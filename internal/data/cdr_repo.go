package data

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
)

// CdrRepo is read-only access to the asteriskcdrdb database.
type CdrRepo struct {
	log     *log.Helper
	mysql   *MySQLClients
	timeout time.Duration
}

func NewCdrRepo(ctx *bootstrap.Context, m *MySQLClients) *CdrRepo {
	return &CdrRepo{
		log:     ctx.NewLoggerHelper("asterisk/repo/cdr"),
		mysql:   m,
		timeout: m.Cfg.QueryTimeout,
	}
}

// ListCalls returns one row per linkedid matching the filter, paginated and
// sorted newest-first. The returned slice and total count are independent
// queries — total reflects all matching rows, not the current page.
func (r *CdrRepo) ListCalls(ctx context.Context, f CallFilter) ([]CallSummary, int32, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	if !f.From.Before(f.To) {
		return nil, 0, fmt.Errorf("invalid time range: from must be before to")
	}

	where, args := buildLinkedIDFilter(f)

	// Count distinct linkedids matching filter.
	countSQL := `
		SELECT COUNT(*) FROM (
			SELECT c.linkedid
			FROM cdr c
			WHERE ` + where + `
			GROUP BY c.linkedid
			` + buildHavingClause(f) + `
		) t
	`
	var total int64
	if err := r.mysql.Cdr.QueryRowContext(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count calls: %w", err)
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
	offset := page * pageSize

	listSQL := `
		SELECT
			c.linkedid,
			MIN(c.calldate)                                                             AS first_calldate,
			ANY_VALUE(c.src)                                                            AS src,
			ANY_VALUE(c.clid)                                                           AS clid,
			ANY_VALUE(c.cnum)                                                           AS cnum,
			ANY_VALUE(c.cnam)                                                           AS cnam,
			ANY_VALUE(c.dst)                                                            AS dst,
			MAX(c.duration)                                                             AS duration,
			MAX(CASE WHEN c.disposition='ANSWERED' THEN c.billsec ELSE 0 END)           AS billsec,
			CASE
				WHEN SUM(CASE WHEN c.disposition='ANSWERED' THEN 1 ELSE 0 END) > 0 THEN 'ANSWERED'
				WHEN SUM(CASE WHEN c.disposition='BUSY'     THEN 1 ELSE 0 END) > 0 THEN 'BUSY'
				WHEN SUM(CASE WHEN c.disposition='FAILED'   THEN 1 ELSE 0 END) > 0 THEN 'FAILED'
				ELSE 'NO ANSWER'
			END                                                                         AS final_disposition,
			COUNT(*)                                                                    AS leg_count,
			ANY_VALUE(c.did)                                                            AS did,
			ANY_VALUE(c.recordingfile)                                                  AS recordingfile,
			MAX(CASE WHEN c.disposition='ANSWERED' THEN c.dstchannel END)               AS answered_dstchannel,
			MAX(CASE WHEN c.disposition='ANSWERED' THEN c.channel END)                  AS answered_channel,
			(SELECT fl.channel    FROM cdr fl WHERE fl.linkedid = c.linkedid
				ORDER BY fl.calldate ASC, fl.sequence ASC LIMIT 1)                      AS first_channel,
			(SELECT fl.dstchannel FROM cdr fl WHERE fl.linkedid = c.linkedid
				ORDER BY fl.calldate ASC, fl.sequence ASC LIMIT 1)                      AS first_dstchannel,
			(SELECT TIMESTAMPDIFF(SECOND, MIN(cs.eventtime), MIN(ca.eventtime))
				FROM cel cs
				JOIN cel ca ON ca.linkedid = cs.linkedid AND ca.eventtype = 'ANSWER'
				WHERE cs.linkedid = c.linkedid AND cs.eventtype = 'CHAN_START')         AS pickup_seconds
		FROM cdr c
		WHERE ` + where + `
		GROUP BY c.linkedid
		` + buildHavingClause(f) + `
		ORDER BY first_calldate DESC
		LIMIT ? OFFSET ?
	`
	listArgs := append(append([]any{}, args...), pageSize, offset)

	rows, err := r.mysql.Cdr.QueryContext(ctx, listSQL, listArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list calls: %w", err)
	}
	defer rows.Close()

	out := make([]CallSummary, 0, pageSize)
	for rows.Next() {
		var s CallSummary
		var ansDstChan, ansChan, firstChan, firstDstChan sql.NullString
		var pickup sql.NullInt64
		if err := rows.Scan(
			&s.LinkedID, &s.CallDate,
			&s.Src, &s.Clid, &s.Cnum, &s.Cnam, &s.Dst,
			&s.DurationSeconds, &s.BillsecSeconds, &s.Disposition,
			&s.LegCount, &s.DID, &s.RecordingFile,
			&ansDstChan, &ansChan, &firstChan, &firstDstChan, &pickup,
		); err != nil {
			return nil, 0, fmt.Errorf("scan call: %w", err)
		}
		if pickup.Valid {
			p := int32(pickup.Int64)
			s.PickupSeconds = &p
		}
		// Prefer the dstchannel for inbound (answered extension is the receiving side).
		if ext := ExtractExtension(ansDstChan.String); ext != "" {
			s.AnsweredExtension = ext
		} else if ext := ExtractExtension(ansChan.String); ext != "" {
			s.AnsweredExtension = ext
		}
		// Originating extension comes from the first leg's channel: the side that
		// initiated the call. Empty for inbound (where the first channel is the trunk).
		s.OriginatingExtension = ExtractExtension(firstChan.String)
		// Use the first leg's channel pair for direction inference so that
		// unanswered ringgroup calls are still classified.
		s.Direction = inferDirection(s.Src, s.Dst, firstDstChan.String, firstChan.String)
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate calls: %w", err)
	}

	return out, int32(total), nil
}

// GetCall returns the per-leg breakdown and CEL timeline for a single call.
func (r *CdrRepo) GetCall(ctx context.Context, linkedID string) (*CallSummary, []CallLeg, []CelEvent, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	legs, dids, err := r.queryLegs(ctx, linkedID)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(legs) == 0 {
		return nil, nil, nil, sql.ErrNoRows
	}

	timeline, err := r.queryTimeline(ctx, linkedID)
	if err != nil {
		return nil, nil, nil, err
	}

	summary := summarizeLegs(linkedID, legs, timeline)
	for _, d := range dids {
		if d != "" {
			summary.DID = d
			break
		}
	}
	return summary, legs, timeline, nil
}

func (r *CdrRepo) queryLegs(ctx context.Context, linkedID string) ([]CallLeg, []string, error) {
	const q = `
		SELECT uniqueid, calldate, channel, dstchannel, src, dst,
		       lastapp, lastdata, disposition, duration, billsec, recordingfile, did
		FROM cdr
		WHERE linkedid = ?
		ORDER BY calldate ASC, sequence ASC
	`
	rows, err := r.mysql.Cdr.QueryContext(ctx, q, linkedID)
	if err != nil {
		return nil, nil, fmt.Errorf("query legs: %w", err)
	}
	defer rows.Close()

	var legs []CallLeg
	var dids []string
	for rows.Next() {
		var l CallLeg
		var did sql.NullString
		if err := rows.Scan(
			&l.Uniqueid, &l.CallDate, &l.Channel, &l.Dstchannel,
			&l.Src, &l.Dst, &l.Lastapp, &l.Lastdata,
			&l.Disposition, &l.DurationSeconds, &l.BillsecSeconds,
			&l.RecordingFile, &did,
		); err != nil {
			return nil, nil, fmt.Errorf("scan leg: %w", err)
		}
		l.Extension = ExtractExtension(l.Dstchannel)
		if l.Extension == "" {
			l.Extension = ExtractExtension(l.Channel)
		}
		legs = append(legs, l)
		dids = append(dids, did.String)
	}
	return legs, dids, rows.Err()
}

func (r *CdrRepo) queryTimeline(ctx context.Context, linkedID string) ([]CelEvent, error) {
	const q = `
		SELECT eventtime, eventtype, channame, uniqueid, appname, appdata,
		       cid_name, cid_num, exten, context
		FROM cel
		WHERE linkedid = ?
		ORDER BY eventtime ASC, id ASC
	`
	rows, err := r.mysql.Cdr.QueryContext(ctx, q, linkedID)
	if err != nil {
		return nil, fmt.Errorf("query timeline: %w", err)
	}
	defer rows.Close()

	var events []CelEvent
	for rows.Next() {
		var e CelEvent
		var appdata sql.NullString
		if err := rows.Scan(
			&e.EventTime, &e.EventType, &e.ChanName, &e.Uniqueid,
			&e.AppName, &appdata, &e.CidName, &e.CidNum,
			&e.Exten, &e.Context,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		e.AppData = appdata.String
		events = append(events, e)
	}
	return events, rows.Err()
}

// buildLinkedIDFilter generates the WHERE clause shared by ListCalls count
// and list queries. The HAVING clause (post-aggregation) is built separately.
func buildLinkedIDFilter(f CallFilter) (string, []any) {
	var (
		clauses []string
		args    []any
	)
	clauses = append(clauses, "c.calldate >= ? AND c.calldate < ?")
	args = append(args, f.From, f.To)

	if f.Src != "" {
		clauses = append(clauses, "c.src LIKE ?")
		args = append(args, "%"+f.Src+"%")
	}
	if f.Dst != "" {
		clauses = append(clauses, "c.dst LIKE ?")
		args = append(args, "%"+f.Dst+"%")
	}
	if f.Extension != "" {
		// Filter to calls where the extension is the originator or the answerer.
		// The previous "any leg involving the extension" semantic returned every
		// ringgroup call in which this extension was just a member that rang
		// without answering — which is rarely what an operator means by
		// "show me ext N's calls."
		//
		//   - originator (any direction): channel LIKE pat on any leg.
		//     Catches outbound from this ext (PJSIP/<ext>-… on the trunk-side
		//     dial) and internal from this ext.
		//   - answerer (inbound or internal): dstchannel LIKE pat on a leg with
		//     disposition='ANSWERED'. Excludes ringgroup legs that never picked
		//     up.
		clauses = append(clauses, `c.linkedid IN (
			SELECT DISTINCT linkedid FROM cdr
			WHERE calldate >= ? AND calldate < ?
			  AND (
			    channel LIKE ?
			    OR (disposition = 'ANSWERED' AND dstchannel LIKE ?)
			  )
		)`)
		pat := "%/" + f.Extension + "-%"
		args = append(args, f.From, f.To, pat, pat)
	}
	if dirClause := buildDirectionClause(f.Direction); dirClause != "" {
		clauses = append(clauses, dirClause)
	}
	return strings.Join(clauses, " AND "), args
}

// buildDirectionClause filters linkedids by the inferred direction of the
// first leg. The classification mirrors `inferDirection` in Go: an
// "extension-shaped" channel is a tech-prefixed name like PJSIP/45-... or
// Local/600@... where the part after the slash starts with digits/alnum and
// contains at least one digit.
//
// The regex `^[A-Za-z]+/[A-Za-z0-9_]*[0-9][A-Za-z0-9_]*[-@]` matches a
// tech prefix, slash, then alphanumeric/underscore with at least one digit,
// terminated by `-` or `@` (the same boundary `ExtractExtension` uses).
func buildDirectionClause(direction string) string {
	const extPattern = `'^[A-Za-z]+/[A-Za-z0-9_]*[0-9][A-Za-z0-9_]*[-@]'`
	const firstLegChannel = `(SELECT fl.channel    FROM cdr fl WHERE fl.linkedid = c.linkedid
			ORDER BY fl.calldate ASC, fl.sequence ASC LIMIT 1)`
	const firstLegDstChannel = `(SELECT fl.dstchannel FROM cdr fl WHERE fl.linkedid = c.linkedid
			ORDER BY fl.calldate ASC, fl.sequence ASC LIMIT 1)`

	switch strings.ToLower(direction) {
	case "inbound":
		// trunk → extension
		return firstLegChannel + ` NOT REGEXP ` + extPattern + ` AND ` +
			firstLegDstChannel + ` REGEXP ` + extPattern
	case "outbound":
		// extension → trunk
		return firstLegChannel + ` REGEXP ` + extPattern + ` AND ` +
			firstLegDstChannel + ` NOT REGEXP ` + extPattern
	case "internal":
		// extension → extension
		return firstLegChannel + ` REGEXP ` + extPattern + ` AND ` +
			firstLegDstChannel + ` REGEXP ` + extPattern
	default:
		return ""
	}
}

func buildHavingClause(f CallFilter) string {
	if f.Disposition == "" {
		return ""
	}
	switch strings.ToUpper(f.Disposition) {
	case "ANSWERED":
		return "HAVING SUM(c.disposition='ANSWERED') > 0"
	case "BUSY":
		return "HAVING SUM(c.disposition='ANSWERED')=0 AND SUM(c.disposition='BUSY') > 0"
	case "FAILED":
		return "HAVING SUM(c.disposition='ANSWERED')=0 AND SUM(c.disposition='BUSY')=0 AND SUM(c.disposition='FAILED') > 0"
	case "NO ANSWER", "NO_ANSWER":
		return "HAVING SUM(c.disposition='ANSWERED')=0 AND SUM(c.disposition='BUSY')=0 AND SUM(c.disposition='FAILED')=0"
	default:
		return ""
	}
}

// summarizeLegs collapses CallLegs into a CallSummary, using the CEL timeline
// for accurate pickup time.
func summarizeLegs(linkedID string, legs []CallLeg, timeline []CelEvent) *CallSummary {
	s := &CallSummary{
		LinkedID: linkedID,
		LegCount: int32(len(legs)),
	}
	if len(legs) == 0 {
		return s
	}

	s.CallDate = legs[0].CallDate
	s.Src = legs[0].Src
	s.Dst = legs[0].Dst
	s.RecordingFile = legs[0].RecordingFile
	s.OriginatingExtension = ExtractExtension(legs[0].Channel)

	hasAnswered, hasBusy, hasFailed := false, false, false
	for _, l := range legs {
		switch l.Disposition {
		case "ANSWERED":
			hasAnswered = true
			if l.BillsecSeconds > s.BillsecSeconds {
				s.BillsecSeconds = l.BillsecSeconds
			}
			if s.AnsweredExtension == "" {
				s.AnsweredExtension = l.Extension
			}
		case "BUSY":
			hasBusy = true
		case "FAILED":
			hasFailed = true
		}
		if l.DurationSeconds > s.DurationSeconds {
			s.DurationSeconds = l.DurationSeconds
		}
	}
	switch {
	case hasAnswered:
		s.Disposition = "ANSWERED"
	case hasBusy:
		s.Disposition = "BUSY"
	case hasFailed:
		s.Disposition = "FAILED"
	default:
		s.Disposition = "NO ANSWER"
	}

	// Pickup from CEL.
	var firstStart, firstAnswer time.Time
	for _, e := range timeline {
		switch e.EventType {
		case "CHAN_START":
			if firstStart.IsZero() || e.EventTime.Before(firstStart) {
				firstStart = e.EventTime
			}
		case "ANSWER":
			if firstAnswer.IsZero() || e.EventTime.Before(firstAnswer) {
				firstAnswer = e.EventTime
			}
		}
	}
	if !firstStart.IsZero() && !firstAnswer.IsZero() && !firstAnswer.Before(firstStart) {
		secs := int32(firstAnswer.Sub(firstStart) / time.Second)
		s.PickupSeconds = &secs
	}

	s.Direction = inferDirection(s.Src, s.Dst, legs[0].Dstchannel, legs[0].Channel)
	return s
}

// inferDirection guesses call direction from the channel pair. FreePBX
// uses "from-trunk" / trunk-named channels for inbound; bare extensions
// for internal/outbound. This is best-effort.
func inferDirection(src, dst, dstchannel, channel string) string {
	srcExt := ExtractExtension(channel)
	dstExt := ExtractExtension(dstchannel)
	switch {
	case srcExt != "" && dstExt != "" && srcExt != dstExt:
		return "internal"
	case srcExt == "" && dstExt != "":
		return "inbound"
	case srcExt != "" && dstExt == "":
		return "outbound"
	default:
		_, _ = src, dst
		return "unknown"
	}
}
