package support

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"strings"
	"time"
	"unicode"
)

//go:embed migrations/*.sql
var migrations embed.FS

// ErrDenied is returned for a tenant other than the binding's.
var ErrDenied = errors.New("support tenant denied")

// Store keeps classified calls in the module-owned database (the one the
// registration history uses), in UTC.
type Store struct {
	DB     *sql.DB
	Tenant string
}

// Migrate creates the support tables when missing.
func (s *Store) Migrate(ctx context.Context) error {
	files, _ := migrations.ReadDir("migrations") // in name order; every statement is idempotent
	for _, f := range files {
		raw, _ := migrations.ReadFile("migrations/" + f.Name())
		for _, q := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(q) == "" {
				continue
			}
			if _, err := s.DB.ExecContext(ctx, q); err != nil {
				return err
			}
		}
	}
	return nil
}

// Cursor is the CEL id up to which calls were ingested (0 before the
// backfill).
func (s *Store) Cursor(ctx context.Context) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, "SELECT cel_id FROM support_ingest WHERE id=1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// CallerKey is the number calls are matched on for callbacks: the last nine
// digits, so 0888…, +359888… and 00359888… agree.
func CallerKey(n string) string {
	var b strings.Builder
	for _, r := range n {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 6 { // an extension, not an external party
		return ""
	}
	if len(d) > 9 {
		d = d[len(d)-9:]
	}
	return d
}

func nullable(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

// Save writes calls (replacing earlier versions of the same linkedid) and
// moves the cursor, in one transaction.
func (s *Store) Save(ctx context.Context, calls []Call, cursor int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, c := range calls {
		if c.LinkedID == "" || c.Kind == KindIgnored {
			continue
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM support_agent_legs WHERE linkedid=?", c.LinkedID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM support_calls WHERE linkedid=?", c.LinkedID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO support_calls (linkedid,started_at,ended_at,kind,grp,outcome,lost_reason,caller,caller_key,agent,trunk,destination,dial_status,announcement_s,ring_s,wait_s,talk_s,setup_s,recorded,quality) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
			c.LinkedID, c.Start.UTC(), c.End.UTC(), c.Kind, c.Group, c.Outcome, c.LostReason, c.Caller, CallerKey(c.Caller), c.Agent, c.Trunk, c.Destination, c.DialStatus,
			nullable(c.Announcement), nullable(c.Ring), nullable(c.CallerWait), nullable(c.Talk), nullable(c.Setup), c.Recorded, c.Quality); err != nil {
			return err
		}
		if len(c.Legs) > 0 { // one statement per call: a group call has 8-13 legs
			args := make([]any, 0, 7*len(c.Legs))
			for i, l := range c.Legs {
				args = append(args, c.LinkedID, i, l.Extension, l.Start.UTC(), l.Outcome, l.Ring, l.Talk)
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO support_agent_legs (linkedid,seq,extension,started_at,outcome,ring_s,talk_s) VALUES "+strings.TrimSuffix(strings.Repeat("(?,?,?,?,?,?,?),", len(c.Legs)), ","), args...); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM support_ingest WHERE id=1"); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO support_ingest (id,cel_id,updated_at) VALUES (1,?,?)", cursor, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// Period loads the calls that started in [from,to), with their agent legs,
// oldest first.
func (s *Store) Period(ctx context.Context, tenant string, from, to time.Time) ([]Call, error) {
	if s == nil || s.DB == nil {
		return nil, errors.New("support store unavailable")
	}
	if tenant == "" || tenant != s.Tenant {
		return nil, ErrDenied
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT linkedid,started_at,ended_at,kind,grp,outcome,lost_reason,caller,agent,trunk,destination,dial_status,announcement_s,ring_s,wait_s,talk_s,setup_s,recorded,quality FROM support_calls WHERE started_at>=? AND started_at<? ORDER BY started_at,linkedid", from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}
	var calls []Call
	index := map[string]int{}
	for rows.Next() {
		var c Call
		var ann, ring, wait, talk, setup sql.NullInt64
		if err = rows.Scan(&c.LinkedID, &c.Start, &c.End, &c.Kind, &c.Group, &c.Outcome, &c.LostReason, &c.Caller, &c.Agent, &c.Trunk, &c.Destination, &c.DialStatus, &ann, &ring, &wait, &talk, &setup, &c.Recorded, &c.Quality); err != nil {
			rows.Close()
			return nil, err
		}
		for _, p := range []struct {
			src sql.NullInt64
			dst **int64
		}{{ann, &c.Announcement}, {ring, &c.Ring}, {wait, &c.CallerWait}, {talk, &c.Talk}, {setup, &c.Setup}} {
			if p.src.Valid {
				v := p.src.Int64
				*p.dst = &v
			}
		}
		index[c.LinkedID] = len(calls)
		calls = append(calls, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// Legs start with (or just after) their call; the margin covers a leg of
	// a call that started just before to.
	rows, err = s.DB.QueryContext(ctx, "SELECT linkedid,extension,started_at,outcome,ring_s,talk_s FROM support_agent_legs WHERE started_at>=? AND started_at<? ORDER BY linkedid,seq", from.UTC(), to.Add(time.Hour).UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var l AgentLeg
		if err = rows.Scan(&id, &l.Extension, &l.Start, &l.Outcome, &l.Ring, &l.Talk); err != nil {
			return nil, err
		}
		if i, ok := index[id]; ok {
			calls[i].Legs = append(calls[i].Legs, l)
		}
	}
	return calls, rows.Err()
}

// Listen is one person opening a call recording.
type Listen struct {
	At       time.Time `json:"at"`
	UserID   string    `json:"userId"`
	LinkedID string    `json:"linkedid"`
}

// listenRepeat: range requests of one playback (seeking, the browser
// re-fetching) within this window count once.
const listenRepeat = 10 * time.Minute

// RecordListen notes that user opened the recording of linkedid.
func (s *Store) RecordListen(ctx context.Context, tenant, user, linkedid string, at time.Time) error {
	if tenant == "" || tenant != s.Tenant {
		return ErrDenied
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM support_listens WHERE linkedid=? AND user_id=? AND listened_at>?", linkedid, user, at.Add(-listenRepeat).UTC()).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, "INSERT INTO support_listens (listened_at,user_id,linkedid) VALUES (?,?,?)", at.UTC(), user, linkedid)
	return err
}

// Listens lists who opened recordings in [from,to) (of one call when
// linkedid is set), newest first, at most 500.
func (s *Store) Listens(ctx context.Context, tenant string, from, to time.Time, linkedid string) ([]Listen, error) {
	if tenant == "" || tenant != s.Tenant {
		return nil, ErrDenied
	}
	q, args := "SELECT listened_at,user_id,linkedid FROM support_listens WHERE listened_at>=? AND listened_at<?", []any{from.UTC(), to.UTC()}
	if linkedid != "" {
		q, args = q+" AND linkedid=?", append(args, linkedid)
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY listened_at DESC, id DESC LIMIT 500", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Listen{}
	for rows.Next() {
		var l Listen
		if err = rows.Scan(&l.At, &l.UserID, &l.LinkedID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
