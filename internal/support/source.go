package support

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

// Source reads finished calls and the routing configuration from the PBX
// databases (read only). CDR may be nil-safe columns: every optional column
// is probed once.
type Source struct {
	CDR    *sql.DB // asteriskcdrdb: cdr and cel
	Config *sql.DB // FreePBX config database; nil: routing falls back to channel names
	cdrCol map[string]bool
}

// ErrNoCEL is returned when the CEL table lacks what the rules read.
var ErrNoCEL = errors.New("support: CEL table lacks id/context/appname/appdata/extra")

// celColumns are required: the rules read hangup causes (extra), contexts and
// the group recording (appname/appdata); id is the ingest cursor.
var celColumns = []string{"id", "eventtype", "eventtime", "context", "channame", "appname", "appdata", "uniqueid", "linkedid", "extra"}

// Probe checks the CEL columns and records which optional CDR columns exist.
func (s *Source) Probe(ctx context.Context) error {
	have, err := columns(ctx, s.CDR, "cel")
	if err != nil {
		return err
	}
	for _, c := range celColumns {
		if !have[c] {
			return ErrNoCEL
		}
	}
	s.cdrCol, err = columns(ctx, s.CDR, "cdr")
	return err
}

func columns(ctx context.Context, db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var c string
		if err = rows.Scan(&c); err != nil {
			return nil, err
		}
		out[strings.ToLower(c)] = true
	}
	return out, rows.Err()
}

// Finished returns the linkedids whose LINKEDID_END has a CEL id above
// after (oldest first, at most limit) and the highest such id.
func (s *Source) Finished(ctx context.Context, after int64, limit int) ([]string, int64, error) {
	rows, err := s.CDR.QueryContext(ctx, "SELECT id,linkedid FROM cel WHERE id>? AND eventtype='LINKEDID_END' ORDER BY id LIMIT ?", after, limit)
	if err != nil {
		return nil, after, err
	}
	defer rows.Close()
	var ids []string
	last := after
	for rows.Next() {
		var id int64
		var link string
		if err = rows.Scan(&id, &link); err != nil {
			return nil, after, err
		}
		ids, last = append(ids, link), id
	}
	return ids, last, rows.Err()
}

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }
func anys(ids []string) []any {
	out := make([]any, len(ids))
	for i, v := range ids {
		out[i] = v
	}
	return out
}

// Load reads the CDR and CEL rows of the given calls, grouped by linkedid.
func (s *Source) Load(ctx context.Context, ids []string) (map[string][]CDRRow, map[string][]CELRow, error) {
	cdrs, cels := map[string][]CDRRow{}, map[string][]CELRow{}
	if len(ids) == 0 {
		return cdrs, cels, nil
	}
	opt := func(col string) string {
		if s.cdrCol[col] {
			return "COALESCE(" + col + ",'')"
		}
		return "''"
	}
	seq := "0"
	if s.cdrCol["sequence"] {
		seq = "COALESCE(sequence,0)"
	}
	q := "SELECT linkedid,uniqueid," + seq + ",calldate,COALESCE(channel,''),COALESCE(dstchannel,''),COALESCE(src,''),COALESCE(dst,'')," + opt("dcontext") + "," + opt("lastapp") + ",COALESCE(disposition,''),COALESCE(duration,0),COALESCE(billsec,0)," + opt("recordingfile") + "," + opt("rtpqos") + "," + opt("peerrtpqos") + " FROM cdr WHERE linkedid IN (" + placeholders(len(ids)) + ")"
	rows, err := s.CDR.QueryContext(ctx, q, anys(ids)...)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var r CDRRow
		var rec string
		if err = rows.Scan(&r.LinkedID, &r.UniqueID, &r.Sequence, &r.Start, &r.Channel, &r.DstChannel, &r.Src, &r.Dst, &r.DContext, &r.LastApp, &r.Disposition, &r.Duration, &r.Billsec, &rec, &r.RTPQoS, &r.PeerRTPQoS); err != nil {
			rows.Close()
			return nil, nil, err
		}
		r.Recorded = rec != ""
		cdrs[r.LinkedID] = append(cdrs[r.LinkedID], r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = s.CDR.QueryContext(ctx, "SELECT id,eventtype,eventtime,COALESCE(context,''),COALESCE(channame,''),COALESCE(appname,''),COALESCE(appdata,''),COALESCE(uniqueid,''),linkedid,COALESCE(extra,'') FROM cel WHERE linkedid IN ("+placeholders(len(ids))+") ORDER BY id", anys(ids)...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e CELRow
		if err = rows.Scan(&e.ID, &e.Type, &e.Time, &e.Context, &e.Channel, &e.App, &e.AppData, &e.UniqueID, &e.LinkedID, &e.Extra); err != nil {
			return nil, nil, err
		}
		cels[e.LinkedID] = append(cels[e.LinkedID], e)
	}
	return cdrs, cels, rows.Err()
}

var destRe = regexp.MustCompile(`^ext-group,([0-9]+),`)

// Groups maps a ring group to its member extensions (external numbers
// dropped).
type Groups map[string][]string

// Routing reads trunks, announcements and ring groups from the FreePBX
// configuration. Without a configuration database it returns an empty
// Routing: trunks are then told apart by their non-numeric names.
func (s *Source) Routing(ctx context.Context) (Routing, Groups, error) {
	r := Routing{Announcements: map[string]string{}, GroupTime: map[string]int{}}
	groups := Groups{}
	if s.Config == nil {
		return r, groups, nil
	}
	r.Trunks = map[string]bool{}
	rows, err := s.Config.QueryContext(ctx, "SELECT channelid FROM trunks")
	if err != nil {
		return r, groups, err
	}
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && name != "" {
			r.Trunks[name] = true
		}
	}
	rows.Close()
	rows, err = s.Config.QueryContext(ctx, "SELECT grpnum,COALESCE(grptime,0),COALESCE(grplist,'') FROM ringgroups")
	if err != nil {
		return r, groups, err
	}
	for rows.Next() {
		var num, list string
		var t int
		if rows.Scan(&num, &t, &list) != nil {
			continue
		}
		r.GroupTime[num] = t
		for _, m := range strings.Split(list, "-") {
			if _, e := strconv.Atoi(m); e == nil { // "0888…#" members are external numbers
				groups[num] = append(groups[num], m)
			}
		}
	}
	rows.Close()
	// Announcements are optional (module may be absent).
	if rows, err = s.Config.QueryContext(ctx, "SELECT announcement_id,COALESCE(post_dest,'') FROM announcement"); err == nil {
		for rows.Next() {
			var id, dest string
			if rows.Scan(&id, &dest) == nil {
				if m := destRe.FindStringSubmatch(dest); m != nil {
					r.Announcements[id] = m[1]
				}
			}
		}
		rows.Close()
	}
	return r, groups, nil
}
