package data

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// PJSIPRegRepo persists and queries the PJSIP registration event log
// (the table this module owns in the tangra DB). When the tangra pool is
// not configured, methods return ErrPJSIPRepoDisabled — callers should
// treat that as "feature off" rather than fatal.
type PJSIPRegRepo struct {
	log     *log.Helper
	db      *sql.DB
	timeout time.Duration
}

// ErrPJSIPRepoDisabled is returned when the AMI/registration feature is not
// configured (no tangra DSN). It lets the gRPC layer surface a friendly
// "not configured" error rather than a generic 500.
var ErrPJSIPRepoDisabled = errors.New("pjsip registration log: tangra DB not configured")

func NewPJSIPRegRepo(ctx *bootstrap.Context, m *MySQLClients) *PJSIPRegRepo {
	r := &PJSIPRegRepo{
		log:     ctx.NewLoggerHelper("asterisk/repo/pjsip_reg"),
		timeout: m.Cfg.QueryTimeout,
	}
	if m != nil && m.Tangra != nil {
		r.db = m.Tangra
	}
	return r
}

// Enabled reports whether the underlying DB pool is available.
func (r *PJSIPRegRepo) Enabled() bool { return r.db != nil }

// Insert appends one event row. ID is populated on success.
func (r *PJSIPRegRepo) Insert(ctx context.Context, e *PJSIPRegEvent) error {
	if r.db == nil {
		return ErrPJSIPRepoDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	const q = `
		INSERT INTO pjsip_registration_events
			(event_time, endpoint, aor, contact_uri, status,
			 user_agent, via_address, reg_expire, rtt_usec)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := r.db.ExecContext(ctx, q,
		e.EventTime.UTC(),
		e.Endpoint,
		e.AOR,
		e.ContactURI,
		string(e.Status),
		e.UserAgent,
		e.ViaAddress,
		nullableTime(e.RegExpire),
		e.RTTMicros,
	)
	if err != nil {
		return fmt.Errorf("insert pjsip reg event: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		e.ID = id
	}
	return nil
}

// GetStatusAt returns the inferred registration state for an endpoint at a
// specific instant. The implementation looks up the most recent prior event;
// callers should bear in mind that AMI is best-effort (gaps in the log are
// possible if the listener was disconnected).
func (r *PJSIPRegRepo) GetStatusAt(ctx context.Context, endpoint string, at time.Time) (*PJSIPRegStatusAt, error) {
	if r.db == nil {
		return nil, ErrPJSIPRepoDisabled
	}
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	const q = `
		SELECT id, event_time, endpoint, aor, contact_uri, status,
		       user_agent, via_address, reg_expire, rtt_usec
		FROM pjsip_registration_events
		WHERE endpoint = ? AND event_time <= ?
		ORDER BY event_time DESC, id DESC
		LIMIT 1
	`
	row := r.db.QueryRowContext(ctx, q, endpoint, at.UTC())

	var e PJSIPRegEvent
	if err := scanEvent(row, &e); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return &PJSIPRegStatusAt{Endpoint: endpoint}, nil
		}
		return nil, fmt.Errorf("status at: %w", err)
	}

	out := &PJSIPRegStatusAt{
		Endpoint:   endpoint,
		Status:     e.Status,
		LastEvent:  &e,
		Registered: e.Status.IsRegistered(),
	}
	// If the latest event implies "registered", but we know its expiry has
	// already passed by `at`, the registration silently lapsed. Asterisk
	// only emits a Removed event when the AMI listener is connected, so we
	// can't rely on that signal alone.
	if out.Registered && e.RegExpire != nil && e.RegExpire.Before(at) {
		out.Registered = false
	}
	return out, nil
}

// ListRegisteredAt returns one row per extension whose most recent event at
// or before `at` left it in a registered state (Created/Updated/Reachable/
// Unqualified) AND, when known, has not yet hit reg_expire.
//
// Implementation: for each endpoint, take its latest event ≤ at via a
// correlated subquery, then keep only the rows whose status implies
// "online". MySQL 8 window functions would be cleaner, but the current
// schema is small enough that the GROUP BY/JOIN form is fine and avoids
// the version dependency.
func (r *PJSIPRegRepo) ListRegisteredAt(ctx context.Context, at time.Time) ([]PJSIPRegisteredEndpoint, error) {
	if r.db == nil {
		return nil, ErrPJSIPRepoDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	const q = `
		SELECT e.endpoint, e.contact_uri, e.user_agent, e.via_address,
		       e.status, e.event_time, e.reg_expire
		FROM pjsip_registration_events e
		INNER JOIN (
			SELECT endpoint, MAX(event_time) AS max_t
			FROM pjsip_registration_events
			WHERE event_time <= ?
			GROUP BY endpoint
		) latest
		  ON latest.endpoint = e.endpoint
		 AND latest.max_t   = e.event_time
		WHERE e.status IN ('Created', 'Updated', 'Reachable', 'Unqualified')
		  AND (e.reg_expire IS NULL OR e.reg_expire > ?)
		ORDER BY CAST(e.endpoint AS UNSIGNED), e.endpoint
	`
	rows, err := r.db.QueryContext(ctx, q, at.UTC(), at.UTC())
	if err != nil {
		return nil, fmt.Errorf("list registered at: %w", err)
	}
	defer rows.Close()

	var out []PJSIPRegisteredEndpoint
	for rows.Next() {
		var (
			ep                                          PJSIPRegisteredEndpoint
			contactURI, userAgent, viaAddr              sql.NullString
			regExpire                                   sql.NullTime
			status                                      string
		)
		if err := rows.Scan(
			&ep.Endpoint, &contactURI, &userAgent, &viaAddr,
			&status, &ep.LastEventTime, &regExpire,
		); err != nil {
			return nil, fmt.Errorf("scan registered: %w", err)
		}
		ep.ContactURI = contactURI.String
		ep.UserAgent = userAgent.String
		ep.ViaAddress = viaAddr.String
		ep.Status = PJSIPRegStatus(status)
		if regExpire.Valid {
			t := regExpire.Time
			ep.RegExpire = &t
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

// ListEvents returns events matching the filter, newest first, paginated.
func (r *PJSIPRegRepo) ListEvents(ctx context.Context, f PJSIPRegEventFilter) ([]PJSIPRegEvent, int32, error) {
	if r.db == nil {
		return nil, 0, ErrPJSIPRepoDisabled
	}
	if !f.From.Before(f.To) {
		return nil, 0, fmt.Errorf("invalid time range: from must be before to")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	page := f.Page
	if page < 0 {
		page = 0
	}
	pageSize := f.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	var (
		clauses []string
		args    []any
	)
	clauses = append(clauses, "event_time >= ? AND event_time < ?")
	args = append(args, f.From.UTC(), f.To.UTC())
	if f.Endpoint != "" {
		clauses = append(clauses, "endpoint LIKE ?")
		args = append(args, "%"+f.Endpoint+"%")
	}
	where := strings.Join(clauses, " AND ")

	var total int64
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM pjsip_registration_events WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count events: %w", err)
	}

	listSQL := `
		SELECT id, event_time, endpoint, aor, contact_uri, status,
		       user_agent, via_address, reg_expire, rtt_usec
		FROM pjsip_registration_events
		WHERE ` + where + `
		ORDER BY event_time DESC, id DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]any{}, args...), pageSize, page*pageSize)

	rows, err := r.db.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	out := make([]PJSIPRegEvent, 0, pageSize)
	for rows.Next() {
		var e PJSIPRegEvent
		if err := scanEvent(rows, &e); err != nil {
			return nil, 0, fmt.Errorf("scan event: %w", err)
		}
		out = append(out, e)
	}
	return out, int32(total), rows.Err()
}

// scanner is the common interface across *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanEvent(s scanner, e *PJSIPRegEvent) error {
	var (
		aor, contactURI, userAgent, viaAddr sql.NullString
		regExpire                           sql.NullTime
		status                              string
	)
	if err := s.Scan(
		&e.ID, &e.EventTime, &e.Endpoint, &aor, &contactURI, &status,
		&userAgent, &viaAddr, &regExpire, &e.RTTMicros,
	); err != nil {
		return err
	}
	e.AOR = aor.String
	e.ContactURI = contactURI.String
	e.Status = PJSIPRegStatus(status)
	e.UserAgent = userAgent.String
	e.ViaAddress = viaAddr.String
	if regExpire.Valid {
		t := regExpire.Time
		e.RegExpire = &t
	}
	return nil
}

func nullableTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t.UTC(), Valid: true}
}
