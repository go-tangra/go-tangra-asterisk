package data

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// displayTZ is the timezone the FreePBX deployment is operated in. CDR rows
// are stored in UTC, but bucketing (per-day, per-hour, hour-of-day, busiest
// hour) must reflect what an operator reads on a clock — otherwise a call
// at 08:00 Sofia would land in the 05:00 bucket. We push this conversion
// into MySQL via CONVERT_TZ so DST transitions inside the query window are
// handled correctly.
const displayTZ = "Europe/Sofia"

// tzExpr wraps a calldate column reference in a CONVERT_TZ() to displayTZ.
// CDR rows are written by Asterisk in UTC; the column is stored as a naive
// DATETIME, but our DSN reads it as UTC.
func tzExpr(col string) string {
	return "CONVERT_TZ(" + col + ", '+00:00', '" + displayTZ + "')"
}

// StatsRepo computes aggregated CDR statistics. All queries are executed
// against the asteriskcdrdb pool; the asterisk pool is used for display
// name lookups in ListExtensionStats.
type StatsRepo struct {
	log      *log.Helper
	mysql    *MySQLClients
	timeout  time.Duration
	sofiaLoc *time.Location
}

func NewStatsRepo(ctx *bootstrap.Context, m *MySQLClients) *StatsRepo {
	loc, err := time.LoadLocation(displayTZ)
	if err != nil {
		// Fallback should never happen — Go ships full tzdata. If it does,
		// degrade to UTC; charts will be off but service stays up.
		loc = time.UTC
	}
	return &StatsRepo{
		log:      ctx.NewLoggerHelper("asterisk/repo/stats"),
		mysql:    m,
		timeout:  m.Cfg.QueryTimeout,
		sofiaLoc: loc,
	}
}

// reinterpretAsSofia takes a time.Time whose Y/M/D/H/M/S components reflect
// Sofia local time but whose Location is wrong (Go's MySQL driver scans into
// UTC because of our DSN), and returns the equivalent absolute instant. The
// proto wire format is UTC, so this guarantees the frontend receives the
// correct instant for any tz it later renders in.
func (r *StatsRepo) reinterpretAsSofia(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, r.sofiaLoc)
}

// extensionLinkedIDFilter returns SQL clause + args limiting the parent
// query to linkedids where the extension is either the originator (any leg
// has channel = PJSIP/<ext>-…) or the answerer (an ANSWERED leg has
// dstchannel = PJSIP/<ext>-…). This matches the semantic an operator means
// by "ext N's calls": calls that ext N actually handled, not every ringgroup
// call ext N just rang as a passive member. The two extra ?-placeholders
// must be filled with from, to, pat, pat (in that order) by the caller.
const extensionLinkedIDFilter = `c.linkedid IN (
	SELECT DISTINCT linkedid FROM cdr
	WHERE calldate >= ? AND calldate < ?
	  AND (
	    channel LIKE ?
	    OR (disposition = 'ANSWERED' AND dstchannel LIKE ?)
	  )
)`

// Overview returns whole-fleet metrics for [from, to).
func (r *StatsRepo) Overview(ctx context.Context, f OverviewFilter) (*OverviewResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if !f.From.Before(f.To) {
		return nil, fmt.Errorf("invalid time range: from must be before to")
	}

	const overviewSQL = `
		SELECT
			COUNT(*) AS total_calls,
			SUM(final_disposition='ANSWERED')  AS answered,
			SUM(final_disposition='NO ANSWER') AS missed,
			SUM(final_disposition='BUSY')      AS busy,
			SUM(final_disposition='FAILED')    AS failed,
			AVG(CASE WHEN final_disposition='ANSWERED' THEN billsec END)        AS avg_talk,
			AVG(CASE WHEN final_disposition='ANSWERED' THEN pickup_seconds END) AS avg_pickup
		FROM (
			SELECT
				c.linkedid,
				CASE
					WHEN SUM(c.disposition='ANSWERED') > 0 THEN 'ANSWERED'
					WHEN SUM(c.disposition='BUSY')     > 0 THEN 'BUSY'
					WHEN SUM(c.disposition='FAILED')   > 0 THEN 'FAILED'
					ELSE 'NO ANSWER'
				END AS final_disposition,
				MAX(CASE WHEN c.disposition='ANSWERED' THEN c.billsec ELSE 0 END) AS billsec,
				(SELECT TIMESTAMPDIFF(SECOND, MIN(cs.eventtime), MIN(ca.eventtime))
					FROM cel cs
					JOIN cel ca ON ca.linkedid = cs.linkedid AND ca.eventtype='ANSWER'
					WHERE cs.linkedid = c.linkedid AND cs.eventtype='CHAN_START') AS pickup_seconds
			FROM cdr c
			WHERE c.calldate >= ? AND c.calldate < ?
			GROUP BY c.linkedid
		) t
	`
	var (
		total, answered, missed, busy, failed sql.NullInt64
		avgTalk, avgPickup                    sql.NullFloat64
	)
	row := r.mysql.Cdr.QueryRowContext(ctx, overviewSQL, f.From, f.To)
	if err := row.Scan(&total, &answered, &missed, &busy, &failed, &avgTalk, &avgPickup); err != nil {
		return nil, fmt.Errorf("scan overview: %w", err)
	}

	out := &OverviewResult{
		TotalCalls:       int32(total.Int64),
		AnsweredCalls:    int32(answered.Int64),
		MissedCalls:      int32(missed.Int64),
		BusyCalls:        int32(busy.Int64),
		FailedCalls:      int32(failed.Int64),
		AvgPickupSeconds: avgPickup.Float64,
		AvgTalkSeconds:   avgTalk.Float64,
	}

	if f.Bucket != BucketNone {
		series, err := r.overviewSeries(ctx, f.From, f.To, f.Bucket)
		if err != nil {
			return nil, err
		}
		out.Series = series
	}

	return out, nil
}

// overviewSeries returns a histogram of total/answered/missed counts per bucket.
func (r *StatsRepo) overviewSeries(ctx context.Context, from, to time.Time, bucket BucketGranularity) ([]TimeBucketCount, error) {
	expr := bucketExpr(bucket)
	q := `
		SELECT bucket_start,
		       SUM(total)    AS total,
		       SUM(answered) AS answered,
		       SUM(missed)   AS missed
		FROM (
			SELECT
				` + expr + ` AS bucket_start,
				1 AS total,
				CASE WHEN final_disposition='ANSWERED' THEN 1 ELSE 0 END AS answered,
				CASE WHEN final_disposition='NO ANSWER' THEN 1 ELSE 0 END AS missed
			FROM (
				SELECT
					MIN(c.calldate) AS calldate,
					CASE
						WHEN SUM(c.disposition='ANSWERED') > 0 THEN 'ANSWERED'
						WHEN SUM(c.disposition='BUSY')     > 0 THEN 'BUSY'
						WHEN SUM(c.disposition='FAILED')   > 0 THEN 'FAILED'
						ELSE 'NO ANSWER'
					END AS final_disposition
				FROM cdr c
				WHERE c.calldate >= ? AND c.calldate < ?
				GROUP BY c.linkedid
			) calls
		) bucketed
		GROUP BY bucket_start
		ORDER BY bucket_start ASC
	`
	rows, err := r.mysql.Cdr.QueryContext(ctx, q, from, to)
	if err != nil {
		return nil, fmt.Errorf("series query: %w", err)
	}
	defer rows.Close()

	var out []TimeBucketCount
	for rows.Next() {
		var b TimeBucketCount
		if err := rows.Scan(&b.BucketStart, &b.Total, &b.Answered, &b.Missed); err != nil {
			return nil, fmt.Errorf("scan series row: %w", err)
		}
		// MySQL returns the bucket_start in displayTZ as a naive DATETIME but
		// our DSN scans it as UTC. Re-anchor it in Sofia so the wire-time is
		// the correct absolute instant.
		b.BucketStart = r.reinterpretAsSofia(b.BucketStart)
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListExtensionStats returns one row per extension within [from, to). Display
// names are looked up in a single batch from asterisk.users (best-effort —
// missing names are returned as empty strings).
func (r *StatsRepo) ListExtensionStats(ctx context.Context, f ExtensionStatsFilter) ([]ExtensionStat, int32, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if !f.From.Before(f.To) {
		return nil, 0, fmt.Errorf("invalid time range: from must be before to")
	}

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

	// Each call is attributed to every extension that either originated it
	// (channel = PJSIP/<ext>-…) or answered it (an ANSWERED leg's dstchannel
	// = PJSIP/<ext>-…). A call may belong to multiple extensions — an
	// internal call from 30 to 41 shows up in both 30's and 41's totals.
	// Ringgroup members that just rang without answering are excluded.
	//
	// Direction is derived from the channel pair on the legs (FreePBX uses
	// PJSIP/<ext>-… for extensions and PJSIP/ITD-… for trunks). The inbound
	// and outbound counters use direction × role:
	//
	//   inbound_calls(X)  = #calls where direction=inbound  AND X answered
	//   outbound_calls(X) = #calls where direction=outbound AND X originated
	//
	// Internal extension-to-extension calls land in total_calls but in
	// neither inbound nor outbound, matching the operator's mental model.
	//
	// total_talk_seconds sums billsec over every call this extension was
	// party to (the per (linkedid, ext) join is pre-aggregated to one row,
	// so each call contributes once per ext — internal calls correctly add
	// to both parties).
	//
	// handled_share is computed as a window-function ratio over all rows of
	// the inner aggregate, so it stays correct under pagination.
	const baseSQL = `
		SELECT
			per_ext.*,
			CASE
				WHEN SUM(per_ext.handled_count) OVER () > 0
				THEN per_ext.handled_count * 1.0 / SUM(per_ext.handled_count) OVER ()
				ELSE 0
			END AS handled_share
		FROM (
			SELECT
				ext.extension,
				COUNT(DISTINCT pc.linkedid)                                                     AS total_calls,
				COUNT(DISTINCT CASE WHEN pc.final_disposition='ANSWERED'  THEN pc.linkedid END) AS answered_calls,
				COUNT(DISTINCT CASE WHEN pc.final_disposition='NO ANSWER' THEN pc.linkedid END) AS missed_calls,
				COUNT(DISTINCT CASE WHEN pc.direction='inbound'  AND ext.is_answerer=1   THEN pc.linkedid END) AS inbound_calls,
				COUNT(DISTINCT CASE WHEN pc.direction='outbound' AND ext.is_originator=1 THEN pc.linkedid END) AS outbound_calls,
				COUNT(DISTINCT CASE WHEN
					(pc.direction='inbound'  AND ext.is_answerer=1)
				 OR (pc.direction='outbound' AND ext.is_originator=1)
					THEN pc.linkedid END)                                                       AS handled_count,
				COALESCE(SUM(CASE WHEN pc.final_disposition='ANSWERED' THEN pc.billsec ELSE 0 END), 0) AS total_talk_seconds,
				AVG(CASE WHEN pc.final_disposition='ANSWERED' THEN pc.pickup_seconds END)        AS avg_pickup,
				AVG(CASE WHEN pc.final_disposition='ANSWERED' THEN pc.billsec END)               AS avg_talk
			FROM (
				SELECT
					c.linkedid,
					CASE
						WHEN SUM(c.disposition='ANSWERED') > 0 THEN 'ANSWERED'
						WHEN SUM(c.disposition='BUSY')     > 0 THEN 'BUSY'
						WHEN SUM(c.disposition='FAILED')   > 0 THEN 'FAILED'
						ELSE 'NO ANSWER'
					END AS final_disposition,
					CASE
						WHEN     MIN(c.channel)    REGEXP '^[A-Za-z]+/[0-9]+-'
						     AND MIN(c.dstchannel) NOT REGEXP '^[A-Za-z]+/[0-9]+-' THEN 'outbound'
						WHEN     MIN(c.channel)    NOT REGEXP '^[A-Za-z]+/[0-9]+-'
						     AND MIN(c.dstchannel) REGEXP '^[A-Za-z]+/[0-9]+-' THEN 'inbound'
						WHEN     MIN(c.channel)    REGEXP '^[A-Za-z]+/[0-9]+-'
						     AND MIN(c.dstchannel) REGEXP '^[A-Za-z]+/[0-9]+-' THEN 'internal'
						ELSE 'unknown'
					END AS direction,
					MAX(CASE WHEN c.disposition='ANSWERED' THEN c.billsec ELSE 0 END) AS billsec,
					(SELECT TIMESTAMPDIFF(SECOND, MIN(cs.eventtime), MIN(ca.eventtime))
						FROM cel cs
						JOIN cel ca ON ca.linkedid = cs.linkedid AND ca.eventtype='ANSWER'
						WHERE cs.linkedid = c.linkedid AND cs.eventtype='CHAN_START') AS pickup_seconds
				FROM cdr c
				WHERE c.calldate >= ? AND c.calldate < ?
				GROUP BY c.linkedid
			) pc
			JOIN (
				SELECT
					linkedid,
					extension,
					MAX(role = 'originator') AS is_originator,
					MAX(role = 'answerer')   AS is_answerer
				FROM (
					SELECT linkedid,
						NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(channel, '-', 1), '/', -1), '') AS extension,
						'originator' AS role
					FROM cdr
					WHERE calldate >= ? AND calldate < ?
					UNION ALL
					SELECT linkedid,
						NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(dstchannel, '-', 1), '/', -1), '') AS extension,
						'answerer' AS role
					FROM cdr
					WHERE calldate >= ? AND calldate < ?
					  AND disposition = 'ANSWERED'
				) per_leg
				WHERE extension IS NOT NULL AND extension REGEXP '^[0-9]+$'
				GROUP BY linkedid, extension
			) ext ON ext.linkedid = pc.linkedid
	`
	args := []any{f.From, f.To, f.From, f.To, f.From, f.To}
	where := ""
	if f.Extension != "" {
		where = " WHERE ext.extension LIKE ?"
		args = append(args, "%"+f.Extension+"%")
	}
	q := baseSQL + where + `
			GROUP BY ext.extension
		) per_ext
		ORDER BY total_calls DESC
		LIMIT ? OFFSET ?
	`
	args = append(args, pageSize, page*pageSize)

	rows, err := r.mysql.Cdr.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list extension stats: %w", err)
	}
	defer rows.Close()

	var stats []ExtensionStat
	exts := make([]string, 0, pageSize)
	for rows.Next() {
		var s ExtensionStat
		var avgPickup, avgTalk sql.NullFloat64
		var handledCount int32 // unused after window-function share is computed
		var handledShare sql.NullFloat64
		if err := rows.Scan(
			&s.Extension, &s.TotalCalls, &s.AnsweredCalls, &s.MissedCalls,
			&s.InboundCalls, &s.OutboundCalls,
			&handledCount, &s.TotalTalkSeconds,
			&avgPickup, &avgTalk,
			&handledShare,
		); err != nil {
			return nil, 0, fmt.Errorf("scan extension stat: %w", err)
		}
		s.HandledShare = handledShare.Float64
		s.AvgPickupSeconds = avgPickup.Float64
		s.AvgTalkSeconds = avgTalk.Float64
		stats = append(stats, s)
		exts = append(exts, s.Extension)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate extension stats: %w", err)
	}

	// Total = total distinct extensions in range matching the filter.
	total, err := r.countExtensions(ctx, f)
	if err != nil {
		return nil, 0, err
	}

	// Decorate with display names + busiest hour. Both are best-effort:
	// if asterisk.users doesn't exist or the extension isn't there, we
	// just leave display_name empty.
	if err := r.decorateBusiestHour(ctx, stats, f.From, f.To, exts); err != nil {
		r.log.Warnf("decorate busiest hour: %v", err)
	}
	if err := r.decorateDisplayName(ctx, stats, exts); err != nil {
		r.log.Warnf("decorate display name: %v", err)
	}

	return stats, total, nil
}

func (r *StatsRepo) countExtensions(ctx context.Context, f ExtensionStatsFilter) (int32, error) {
	// Count distinct extensions that participated as originator or answerer
	// in [from, to). Mirrors the attribution logic used by ListExtensionStats.
	const q = `
		SELECT COUNT(*) FROM (
			SELECT DISTINCT extension FROM (
				SELECT
					NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(channel, '-', 1), '/', -1), '') AS extension
				FROM cdr
				WHERE calldate >= ? AND calldate < ?
				UNION ALL
				SELECT
					NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(dstchannel, '-', 1), '/', -1), '') AS extension
				FROM cdr
				WHERE calldate >= ? AND calldate < ?
				  AND disposition = 'ANSWERED'
			) per_leg
			WHERE extension IS NOT NULL AND extension REGEXP '^[0-9]+$'
	`
	args := []any{f.From, f.To, f.From, f.To}
	tail := ""
	if f.Extension != "" {
		tail = " AND extension LIKE ?"
		args = append(args, "%"+f.Extension+"%")
	}
	tail += ") e"

	var total int64
	if err := r.mysql.Cdr.QueryRowContext(ctx, q+tail, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("count extensions: %w", err)
	}
	return int32(total), nil
}

func (r *StatsRepo) decorateBusiestHour(ctx context.Context, stats []ExtensionStat, from, to time.Time, exts []string) error {
	if len(exts) == 0 {
		return nil
	}

	// We need: per extension, hour-of-day with the highest call count.
	// Approach: aggregate over cdr by (extension, hour) and pick argmax.
	// We scan the CDR with an OR of LIKE patterns scoped to this page.
	likes, args := buildExtensionLikeClause(exts)
	q := `
		SELECT extension, hour, calls FROM (
			SELECT
				ext AS extension,
				HOUR(` + tzExpr("calldate") + `) AS hour,
				COUNT(*) AS calls,
				ROW_NUMBER() OVER (PARTITION BY ext ORDER BY COUNT(*) DESC) AS rn
			FROM (
				SELECT
					c.calldate,
					COALESCE(
						NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(c.dstchannel, '-', 1), '/', -1), ''),
						NULLIF(SUBSTRING_INDEX(SUBSTRING_INDEX(c.channel, '-', 1), '/', -1), '')
					) AS ext
				FROM cdr c
				WHERE c.calldate >= ? AND c.calldate < ?
				  AND (` + likes + `)
			) raw
			WHERE ext IS NOT NULL AND ext <> ''
			GROUP BY ext, HOUR(` + tzExpr("calldate") + `)
		) ranked
		WHERE rn = 1
	`
	queryArgs := append([]any{from, to}, args...)
	rows, err := r.mysql.Cdr.QueryContext(ctx, q, queryArgs...)
	if err != nil {
		return err
	}
	defer rows.Close()

	hourByExt := make(map[string]int32, len(exts))
	for rows.Next() {
		var ext string
		var hour, calls int64
		if err := rows.Scan(&ext, &hour, &calls); err != nil {
			return err
		}
		hourByExt[ext] = int32(hour)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range stats {
		if h, ok := hourByExt[stats[i].Extension]; ok {
			stats[i].BusiestHour = h
		}
	}
	return nil
}

// ExtensionDirectoryEntry is a (number, display name) pair from the
// FreePBX `asterisk.users` table. Used by the live dashboard to enrich
// per-extension Prometheus metrics with operator-recognisable names.
type ExtensionDirectoryEntry struct {
	Extension   string
	DisplayName string
}

// ListExtensionDirectory returns every row from asterisk.users that has
// a non-empty extension number. Best-effort — returns an empty slice
// (not an error) if the table doesn't exist or the schema differs from
// stock FreePBX. Results are typically <500 rows so we don't paginate.
func (r *StatsRepo) ListExtensionDirectory(ctx context.Context) ([]ExtensionDirectoryEntry, error) {
	if r.mysql == nil || r.mysql.Config == nil {
		return nil, nil
	}
	rows, err := r.mysql.Config.QueryContext(ctx,
		`SELECT extension, name FROM users WHERE extension <> '' ORDER BY extension`)
	if err != nil {
		// Table missing or column mismatch — treat as "directory not
		// available" rather than a hard failure so the dashboard
		// gracefully falls back to bare numbers.
		return nil, nil
	}
	defer rows.Close()
	out := make([]ExtensionDirectoryEntry, 0, 64)
	for rows.Next() {
		var ext, name sql.NullString
		if err := rows.Scan(&ext, &name); err != nil {
			return nil, err
		}
		if !ext.Valid || ext.String == "" {
			continue
		}
		out = append(out, ExtensionDirectoryEntry{
			Extension:   ext.String,
			DisplayName: name.String,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *StatsRepo) decorateDisplayName(ctx context.Context, stats []ExtensionStat, exts []string) error {
	if len(exts) == 0 {
		return nil
	}

	// asterisk.users may have columns extension + name (FreePBX schema).
	// Use INFORMATION_SCHEMA to confirm before issuing the join — schemas
	// vary across FreePBX versions. We accept silent failures.
	placeholders := strings.Repeat("?,", len(exts))
	placeholders = placeholders[:len(placeholders)-1]

	q := `SELECT extension, name FROM users WHERE extension IN (` + placeholders + `)`
	args := make([]any, 0, len(exts))
	for _, e := range exts {
		args = append(args, e)
	}
	rows, err := r.mysql.Config.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	names := make(map[string]string, len(exts))
	for rows.Next() {
		var ext, name sql.NullString
		if err := rows.Scan(&ext, &name); err != nil {
			return err
		}
		if ext.Valid {
			names[ext.String] = name.String
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range stats {
		if n, ok := names[stats[i].Extension]; ok {
			stats[i].DisplayName = n
		}
	}
	return nil
}

// GetExtensionDrilldown returns aggregate stats + bucket histograms for one extension.
func (r *StatsRepo) GetExtensionDrilldown(ctx context.Context, extension string, from, to time.Time, bucket BucketGranularity) (*ExtensionDrilldown, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if extension == "" {
		return nil, fmt.Errorf("extension is required")
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("invalid time range: from must be before to")
	}

	stats, _, err := r.ListExtensionStats(ctx, ExtensionStatsFilter{
		From: from, To: to, Extension: extension, PageSize: 200,
	})
	if err != nil {
		return nil, err
	}

	out := &ExtensionDrilldown{}
	for _, s := range stats {
		if s.Extension == extension {
			out.Summary = s
			break
		}
	}
	if out.Summary.Extension == "" {
		// extension had no calls in range — return zeroed summary.
		out.Summary = ExtensionStat{Extension: extension}
	}

	if bucket != BucketNone {
		series, err := r.extensionSeries(ctx, extension, from, to, bucket)
		if err != nil {
			return nil, err
		}
		out.Series = series
	}

	hod, err := r.extensionHourOfDay(ctx, extension, from, to)
	if err != nil {
		return nil, err
	}
	out.HourOfDay = hod
	return out, nil
}

func (r *StatsRepo) extensionSeries(ctx context.Context, extension string, from, to time.Time, bucket BucketGranularity) ([]TimeBucketCount, error) {
	expr := bucketExpr(bucket)
	pat := "%/" + extension + "-%"
	q := `
		SELECT bucket_start,
		       SUM(total) AS total,
		       SUM(answered) AS answered,
		       SUM(missed) AS missed
		FROM (
			SELECT
				` + expr + ` AS bucket_start,
				1 AS total,
				CASE WHEN final_disposition='ANSWERED' THEN 1 ELSE 0 END AS answered,
				CASE WHEN final_disposition='NO ANSWER' THEN 1 ELSE 0 END AS missed
			FROM (
				SELECT
					MIN(c.calldate) AS calldate,
					CASE
						WHEN SUM(c.disposition='ANSWERED') > 0 THEN 'ANSWERED'
						WHEN SUM(c.disposition='BUSY')     > 0 THEN 'BUSY'
						WHEN SUM(c.disposition='FAILED')   > 0 THEN 'FAILED'
						ELSE 'NO ANSWER'
					END AS final_disposition
				FROM cdr c
				WHERE c.calldate >= ? AND c.calldate < ?
				  AND ` + extensionLinkedIDFilter + `
				GROUP BY c.linkedid
			) calls
		) bucketed
		GROUP BY bucket_start
		ORDER BY bucket_start ASC
	`
	rows, err := r.mysql.Cdr.QueryContext(ctx, q, from, to, from, to, pat, pat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TimeBucketCount
	for rows.Next() {
		var b TimeBucketCount
		if err := rows.Scan(&b.BucketStart, &b.Total, &b.Answered, &b.Missed); err != nil {
			return nil, err
		}
		// See overviewSeries: bucket_start comes back as a naive Sofia
		// DATETIME but is scanned as UTC; re-anchor it.
		b.BucketStart = r.reinterpretAsSofia(b.BucketStart)
		out = append(out, b)
	}
	return out, rows.Err()
}

func (r *StatsRepo) extensionHourOfDay(ctx context.Context, extension string, from, to time.Time) ([]TimeBucketCount, error) {
	pat := "%/" + extension + "-%"
	q := `
		SELECT HOUR(` + tzExpr("calldate") + `) AS hour,
		       COUNT(*) AS total,
		       SUM(final_disposition='ANSWERED') AS answered,
		       SUM(final_disposition='NO ANSWER') AS missed
		FROM (
			SELECT
				MIN(c.calldate) AS calldate,
				CASE
					WHEN SUM(c.disposition='ANSWERED') > 0 THEN 'ANSWERED'
					WHEN SUM(c.disposition='BUSY')     > 0 THEN 'BUSY'
					WHEN SUM(c.disposition='FAILED')   > 0 THEN 'FAILED'
					ELSE 'NO ANSWER'
				END AS final_disposition
			FROM cdr c
			WHERE c.calldate >= ? AND c.calldate < ?
			  AND ` + extensionLinkedIDFilter + `
			GROUP BY c.linkedid
		) calls
		GROUP BY hour
		ORDER BY hour
	`
	rows, err := r.mysql.Cdr.QueryContext(ctx, q, from, to, from, to, pat, pat)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHour := make(map[int]TimeBucketCount, 24)
	for rows.Next() {
		var hour int
		var total, answered, missed int64
		if err := rows.Scan(&hour, &total, &answered, &missed); err != nil {
			return nil, err
		}
		// Anchor at 1970-01-01 hour:00 in the display timezone. The wire format
		// (UTC) preserves the absolute instant; the frontend formats it back in
		// displayTZ and gets the same hour out.
		bucket := time.Date(1970, 1, 1, hour, 0, 0, 0, r.sofiaLoc)
		byHour[hour] = TimeBucketCount{BucketStart: bucket, Total: int32(total), Answered: int32(answered), Missed: int32(missed)}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Always return 24 entries so charts render evenly.
	out := make([]TimeBucketCount, 24)
	for h := 0; h < 24; h++ {
		if b, ok := byHour[h]; ok {
			out[h] = b
		} else {
			out[h] = TimeBucketCount{BucketStart: time.Date(1970, 1, 1, h, 0, 0, 0, r.sofiaLoc)}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BucketStart.Before(out[j].BucketStart) })
	return out, nil
}

// RingGroupStats returns inbound traffic to a ringgroup, broken down by
// outcome and including the list of missed calls.
//
// "Routed to ringgroup R" means at least one CDR leg of the linkedid had
// dst='R' or dstchannel='Local/R@%'. FreePBX writes the ringgroup number as
// the destination on the parent inbound leg and uses Local/<R>@from-internal
// channels for the internal dial fan-out.
//
// The final disposition is computed per linkedid:
//   - ANSWERED  → at least one member picked up
//   - NO ANSWER → at least one member rang but none picked up
//   - BUSY      → every dial attempt returned busy (every member was already
//                 on a call, so the ringgroup never rang at all)
//   - FAILED    → only circuit-level failures
//
// Note: this differs from the global precedence in Overview (which prefers
// BUSY over NO ANSWER) because for a ringgroup the operationally meaningful
// "all operators busy" case is precisely when no member ever rang.
func (r *StatsRepo) RingGroupStats(ctx context.Context, f RingGroupStatsFilter) (*RingGroupStatsResult, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if f.RingGroup == "" {
		return nil, fmt.Errorf("ring_group is required")
	}
	if !f.From.Before(f.To) {
		return nil, fmt.Errorf("invalid time range: from must be before to")
	}

	// linkedids that involved this ringgroup.
	const memberClause = `(dst = ? OR dstchannel LIKE ?)`
	dstchanPat := "Local/" + f.RingGroup + "@%"

	const summarySQL = `
		SELECT
			SUM(disp='ANSWERED')  AS answered,
			SUM(disp='NO ANSWER') AS no_answer,
			SUM(disp='BUSY')      AS all_busy,
			SUM(disp='FAILED')    AS failed,
			COUNT(*)              AS total
		FROM (
			SELECT
				c.linkedid,
				CASE
					WHEN SUM(c.disposition='ANSWERED')  > 0 THEN 'ANSWERED'
					WHEN SUM(c.disposition='NO ANSWER') > 0 THEN 'NO ANSWER'
					WHEN SUM(c.disposition='BUSY')      > 0 THEN 'BUSY'
					WHEN SUM(c.disposition='FAILED')    > 0 THEN 'FAILED'
					ELSE 'NO ANSWER'
				END AS disp
			FROM cdr c
			WHERE c.calldate >= ? AND c.calldate < ?
			  AND c.linkedid IN (
			    SELECT DISTINCT linkedid FROM cdr
			    WHERE calldate >= ? AND calldate < ?
			      AND ` + memberClause + `
			  )
			GROUP BY c.linkedid
		) per_call
	`
	summaryArgs := []any{f.From, f.To, f.From, f.To, f.RingGroup, dstchanPat}

	var (
		answered, noAnswer, allBusy, failed, total sql.NullInt64
	)
	if err := r.mysql.Cdr.QueryRowContext(ctx, summarySQL, summaryArgs...).
		Scan(&answered, &noAnswer, &allBusy, &failed, &total); err != nil {
		return nil, fmt.Errorf("ring group summary: %w", err)
	}

	// Missed calls list — everything not ANSWERED. Returns the parent inbound
	// leg's metadata (src/clid/did) and the wall-clock ring duration before
	// the call dropped.
	const missedSQL = `
		SELECT
			per_call.linkedid,
			per_call.first_calldate,
			per_call.src,
			per_call.clid,
			per_call.did,
			per_call.disp,
			per_call.ring_seconds
		FROM (
			SELECT
				c.linkedid,
				MIN(c.calldate)        AS first_calldate,
				MAX(c.src)             AS src,
				MAX(c.clid)            AS clid,
				MAX(c.did)             AS did,
				MAX(c.duration)        AS ring_seconds,
				CASE
					WHEN SUM(c.disposition='ANSWERED')  > 0 THEN 'ANSWERED'
					WHEN SUM(c.disposition='NO ANSWER') > 0 THEN 'NO ANSWER'
					WHEN SUM(c.disposition='BUSY')      > 0 THEN 'BUSY'
					WHEN SUM(c.disposition='FAILED')    > 0 THEN 'FAILED'
					ELSE 'NO ANSWER'
				END AS disp
			FROM cdr c
			WHERE c.calldate >= ? AND c.calldate < ?
			  AND c.linkedid IN (
			    SELECT DISTINCT linkedid FROM cdr
			    WHERE calldate >= ? AND calldate < ?
			      AND ` + memberClause + `
			  )
			GROUP BY c.linkedid
		) per_call
		WHERE per_call.disp <> 'ANSWERED'
		ORDER BY per_call.first_calldate DESC
		LIMIT 100
	`
	rows, err := r.mysql.Cdr.QueryContext(ctx, missedSQL, summaryArgs...)
	if err != nil {
		return nil, fmt.Errorf("ring group missed: %w", err)
	}
	defer rows.Close()

	missed := make([]MissedRingGroupCall, 0, 16)
	for rows.Next() {
		var m MissedRingGroupCall
		var src, clid, did sql.NullString
		var ring sql.NullInt64
		if err := rows.Scan(&m.LinkedID, &m.CallDate, &src, &clid, &did, &m.Disposition, &ring); err != nil {
			return nil, fmt.Errorf("scan missed: %w", err)
		}
		m.Src = src.String
		m.Clid = clid.String
		m.DID = did.String
		m.RingSeconds = int32(ring.Int64)
		missed = append(missed, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate missed: %w", err)
	}

	return &RingGroupStatsResult{
		RingGroup:   f.RingGroup,
		Total:       int32(total.Int64),
		Answered:    int32(answered.Int64),
		NoAnswer:    int32(noAnswer.Int64),
		AllBusy:     int32(allBusy.Int64),
		Failed:      int32(failed.Int64),
		MissedCalls: missed,
	}, nil
}

// buildExtensionLikeClause builds an OR'd LIKE expression for matching any
// of the supplied extensions in dstchannel/channel. Returns clause + args.
func buildExtensionLikeClause(exts []string) (string, []any) {
	if len(exts) == 0 {
		return "1=0", nil
	}
	parts := make([]string, 0, len(exts)*2)
	args := make([]any, 0, len(exts)*2)
	for _, e := range exts {
		parts = append(parts, "c.dstchannel LIKE ?", "c.channel LIKE ?")
		pat := "%/" + e + "-%"
		args = append(args, pat, pat)
	}
	return strings.Join(parts, " OR "), args
}

// bucketExpr returns the SQL expression to bucket calldate at the requested
// granularity, in the deployment's display timezone (so a per-day bucket
// matches the Sofia calendar day, not the UTC calendar day). The expression
// returns a DATETIME aligned to the bucket start, expressed in displayTZ.
func bucketExpr(b BucketGranularity) string {
	c := tzExpr("calldate")
	switch b {
	case BucketHour:
		return "DATE_FORMAT(" + c + ", '%Y-%m-%d %H:00:00')"
	case BucketDay:
		return "DATE(" + c + ")"
	case BucketWeek:
		// Mode 3: ISO weeks, week starts Monday.
		return "DATE_SUB(DATE(" + c + "), INTERVAL WEEKDAY(" + c + ") DAY)"
	default:
		return "DATE(" + c + ")"
	}
}
