package registration

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/pbx"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS
var ErrUnavailable = errors.New("registration unavailable")

type Repository struct {
	DB      *sql.DB
	Tenant  string
	PBX     string
	Timeout time.Duration
}

func Open(ctx context.Context, c config.Config) (*Repository, error) {
	db, e := pbx.OpenDB(ctx, c.Binding.RegistrationDSN, "UTC")
	if e != nil {
		return nil, ErrUnavailable
	}
	r := &Repository{DB: db, Tenant: c.Binding.TenantID, PBX: c.Binding.PBXID, Timeout: c.Timeout()}
	if e = r.Ownership(ctx); e != nil {
		db.Close()
		return nil, e
	}
	return r, nil
}
func (r *Repository) Ownership(ctx context.Context) error {
	var tenant, pbx string
	var version int
	e := r.DB.QueryRowContext(ctx, "SELECT tenant_id,pbx_id,version FROM asterisk_ownership WHERE id=1").Scan(&tenant, &pbx, &version)
	if e != nil || tenant != r.Tenant || pbx != r.PBX || version != 1 {
		return errors.New("registration ownership/schema mismatch; bootstrap required")
	}
	return nil
}
func Bootstrap(ctx context.Context, c config.Config) error {
	if c.Binding.RegistrationDSN == "" {
		return errors.New("registration DSN required")
	}
	db, e := pbx.OpenDB(ctx, c.Binding.RegistrationDSN, "UTC")
	if e != nil {
		return ErrUnavailable
	}
	defer db.Close()
	conn, e := db.Conn(ctx)
	if e != nil {
		return ErrUnavailable
	}
	defer conn.Close()
	var lock int
	if conn.QueryRowContext(ctx, "SELECT GET_LOCK('asterisk-bootstrap',10)").Scan(&lock) != nil || lock != 1 {
		return errors.New("registration bootstrap lock unavailable")
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), "SELECT RELEASE_LOCK('asterisk-bootstrap')")
	claimed := false
	var exists int
	if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='asterisk_ownership'").Scan(&exists) != nil {
		return ErrUnavailable
	}
	if exists > 0 {
		var tenant, pbx string
		e = conn.QueryRowContext(ctx, "SELECT tenant_id,pbx_id FROM asterisk_ownership WHERE id=1").Scan(&tenant, &pbx)
		if e != nil && e != sql.ErrNoRows {
			return ErrUnavailable
		}
		if e == nil {
			claimed = true
		}
		if e == nil && (tenant != c.Binding.TenantID || pbx != c.Binding.PBXID) {
			return errors.New("existing registration ownership differs")
		}
	}
	var legacy int
	if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='pjsip_registration_events'").Scan(&legacy) != nil {
		return ErrUnavailable
	}
	if legacy > 0 && !claimed && !c.Binding.AdoptRegistration {
		return errors.New("legacy history requires explicit adopt_registration")
	}
	if legacy > 0 {
		var count int
		if conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='pjsip_registration_events' AND COLUMN_NAME IN ('id','event_time','endpoint','aor','contact_uri','status','user_agent','via_address','reg_expire','rtt_usec')").Scan(&count) != nil || count != 10 {
			return errors.New("legacy registration schema incompatible")
		}
	}
	raw, _ := migrations.ReadFile("migrations/001.sql")
	for _, q := range strings.Split(string(raw), ";") {
		if strings.TrimSpace(q) != "" {
			if _, e = conn.ExecContext(ctx, q); e != nil {
				return errors.New("module-owned migration failed")
			}
		}
	}
	_, e = conn.ExecContext(ctx, "INSERT INTO asterisk_ownership(id,tenant_id,pbx_id,version) VALUES(1,?,?,1) ON DUPLICATE KEY UPDATE version=version", c.Binding.TenantID, c.Binding.PBXID)
	if e != nil {
		return ErrUnavailable
	}
	return nil
}
func (r *Repository) authorized(tenant string) error {
	if r == nil || r.DB == nil {
		return ErrUnavailable
	}
	if tenant == "" || tenant != r.Tenant {
		return errors.New("registration tenant denied")
	}
	return nil
}
func (r *Repository) Append(ctx context.Context, e Event) error {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	var expire any
	if !e.Expire.IsZero() {
		expire = e.Expire.UTC()
	}
	_, err := r.DB.ExecContext(ctx, "INSERT INTO pjsip_registration_events(event_time,endpoint,aor,contact_uri,status,user_agent,via_address,reg_expire,rtt_usec) VALUES(?,?,?,?,?,?,?,?,?)", e.Time.UTC(), e.Endpoint, e.AOR, e.Contact, e.Status, e.UserAgent, e.ViaAddress, expire, e.RTT)
	return err
}

// Prune deletes registration events older than before, in batches so no
// long-running delete holds locks; it returns the number of rows removed.
// Observation gaps are kept (they are few and mark uncertainty).
func (r *Repository) Prune(ctx context.Context, before time.Time) (int64, error) {
	var total int64
	for ctx.Err() == nil {
		qctx, cancel := context.WithTimeout(ctx, r.Timeout)
		res, err := r.DB.ExecContext(qctx, "DELETE FROM pjsip_registration_events WHERE event_time<? ORDER BY event_time LIMIT 5000", before.UTC())
		cancel()
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 5000 {
			return total, nil
		}
	}
	return total, ctx.Err()
}
func (r *Repository) BeginGap(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	_, e := r.DB.ExecContext(ctx, "INSERT INTO asterisk_observation_gaps(started_at) SELECT UTC_TIMESTAMP(3) WHERE NOT EXISTS (SELECT 1 FROM asterisk_observation_gaps WHERE ended_at IS NULL)")
	return e
}
func (r *Repository) RecoverGap(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	_, e := r.DB.ExecContext(ctx, "UPDATE asterisk_observation_gaps SET ended_at=UTC_TIMESTAMP(3) WHERE ended_at IS NULL")
	return e
}
func (r *Repository) RestartGap(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	_, e := r.DB.ExecContext(ctx, "INSERT INTO asterisk_observation_gaps(started_at) SELECT COALESCE(MAX(event_time),UTC_TIMESTAMP(3)) FROM pjsip_registration_events WHERE NOT EXISTS(SELECT 1 FROM asterisk_observation_gaps WHERE ended_at IS NULL)")
	return e
}
func scan(rows *sql.Rows) ([]Event, error) {
	out := []Event{}
	for rows.Next() {
		var e Event
		var expiry sql.NullTime
		if err := rows.Scan(&e.ID, &e.Time, &e.Endpoint, &e.AOR, &e.Contact, &e.Status, &e.UserAgent, &e.ViaAddress, &expiry, &e.RTT); err != nil {
			return nil, err
		}
		e.Time = e.Time.UTC()
		if expiry.Valid {
			e.Expire = expiry.Time.UTC()
		}
		out = append(out, e)
		if len(out) > 20000 {
			return nil, errors.New("registration query bound exceeded")
		}
	}
	return out, rows.Err()
}

const eventColumns = "id,event_time,endpoint,aor,contact_uri,status,user_agent,via_address,reg_expire,rtt_usec"

func (r *Repository) At(ctx context.Context, tenant, extension string, at time.Time) ([]Status, []Gap, error) {
	if e := r.authorized(tenant); e != nil {
		return nil, nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	where := "event_time<=?"
	args := []any{at}
	if extension != "" {
		where += " AND endpoint=?"
		args = append(args, extension)
	}
	rows, e := r.DB.QueryContext(ctx, "SELECT "+eventColumns+" FROM (SELECT "+eventColumns+",ROW_NUMBER() OVER (PARTITION BY endpoint,contact_uri ORDER BY event_time DESC,id DESC) AS rn FROM pjsip_registration_events WHERE "+where+") latest WHERE rn=1 ORDER BY event_time,id LIMIT 20001", args...)
	if e != nil {
		return nil, nil, e
	}
	events, e := scan(rows)
	rows.Close()
	if e != nil {
		return nil, nil, e
	}
	grows, e := r.DB.QueryContext(ctx, "SELECT started_at,ended_at FROM asterisk_observation_gaps WHERE started_at<=? AND (ended_at IS NULL OR ended_at>?) ORDER BY started_at LIMIT 1000", at, at)
	if e != nil {
		return nil, nil, e
	}
	defer grows.Close()
	gaps := []Gap{}
	for grows.Next() {
		var g Gap
		var end sql.NullTime
		if e = grows.Scan(&g.Start, &end); e != nil {
			return nil, nil, e
		}
		if end.Valid {
			g.End = end.Time
		}
		gaps = append(gaps, g)
	}
	if grows.Err() != nil {
		return nil, nil, grows.Err()
	}
	perEndpoint := map[string][]Event{}
	for _, e := range events {
		perEndpoint[e.Endpoint] = append(perEndpoint[e.Endpoint], e)
	}
	ids := map[string]bool{}
	if extension != "" {
		ids[extension] = true
	}
	for _, v := range events {
		ids[v.Endpoint] = true
	}
	out := []Status{}
	for id := range ids {
		out = append(out, Evaluate(id, at, perEndpoint[id], gaps))
	}
	return out, gaps, nil
}
func (r *Repository) Events(ctx context.Context, tenant, extension string, from, to time.Time, page, size int) ([]Event, int, error) {
	if e := r.authorized(tenant); e != nil {
		return nil, 0, e
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	where := "event_time>=? AND event_time<?"
	args := []any{from, to}
	if extension != "" {
		where += " AND endpoint=?"
		args = append(args, extension)
	}
	var total int
	if e := r.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM pjsip_registration_events WHERE "+where, args...).Scan(&total); e != nil {
		return nil, 0, e
	}
	args = append(args, size, (page-1)*size)
	rows, e := r.DB.QueryContext(ctx, "SELECT "+eventColumns+" FROM pjsip_registration_events WHERE "+where+" ORDER BY event_time DESC,id DESC LIMIT ? OFFSET ?", args...)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	events, e := scan(rows)
	return events, total, e
}

// Contacts returns the last observation per contact, including removed contacts.
// Reconciliation must not collapse multiple contacts to one endpoint status.
func (r *Repository) Contacts(ctx context.Context) ([]Event, error) {
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	rows, e := r.DB.QueryContext(ctx, "SELECT "+eventColumns+" FROM (SELECT "+eventColumns+",ROW_NUMBER() OVER (PARTITION BY endpoint,contact_uri ORDER BY event_time DESC,id DESC) AS rn FROM pjsip_registration_events) latest WHERE rn=1 ORDER BY endpoint,contact_uri LIMIT 20001")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	return scan(rows)
}
