package cdr

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/pbx"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

var ErrMissing = errors.New("call missing")
var ErrBound = errors.New("history query exceeds resource bound; narrow period")

type Filter struct {
	From, To                                                 time.Time
	Src, Dst, Extension, Direction, Disposition, Sort, Order string
	Page, PageSize                                           int
}
type List struct {
	Items    []Call `json:"items"`
	Total    int    `json:"total"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Sort     string `json:"sort"`
	Order    string `json:"order"`
}
type Repository struct {
	Pools *pbx.Pools

	slotsOnce sync.Once
	slots     chan struct{} // bounds concurrent period queries against the PBX

	// Calls of a period are computed once for concurrent identical requests
	// and reused for periodTTL (history pages, statistics and the dashboard
	// of the same period share them).
	flight singleflight.Group
	mu     sync.Mutex
	cache  map[periodKey]periodEntry
}

type periodKey struct {
	tenant   string
	from, to int64
}

type periodEntry struct {
	calls []Call
	at    time.Time
}

const (
	// periodTTL bounds how stale history and statistics can be.
	periodTTL = 30 * time.Second
	// periodEntries bounds the cached periods (memory).
	periodEntries = 4
)

// periodSlots is how many period queries (history, statistics) may run against
// the PBX database at once; further requests wait (within their deadline).
const periodSlots = 4

func (r *Repository) acquire(ctx context.Context) (func(), error) {
	r.slotsOnce.Do(func() { r.slots = make(chan struct{}, periodSlots) })
	select {
	case r.slots <- struct{}{}:
		return func() { <-r.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// periodWhere selects every leg of the calls whose first leg starts in
// [from, to). Candidates come only from legs inside the period (calldate
// index); a call that already had a leg before from is excluded through the
// (linkedid, calldate) index. The former GROUP BY over the whole table scanned
// the complete CDR history on every request.
const periodWhere = "linkedid IN (SELECT w.linkedid FROM (SELECT DISTINCT linkedid FROM cdr WHERE calldate>=? AND calldate<?) w" +
	" WHERE NOT EXISTS (SELECT 1 FROM cdr p WHERE p.linkedid=w.linkedid AND p.calldate<?))"

func (r *Repository) legs(ctx context.Context, where string, args ...any) ([]Leg, error) {
	cols := r.Pools.Columns
	optional := func(name string) string {
		if cols[name] {
			return "COALESCE(`" + name + "`,'')"
		}
		return "''"
	}
	seq := "0"
	if cols["sequence"] {
		seq = "COALESCE(sequence,0)"
	}
	q := "SELECT linkedid,uniqueid," + seq + ",calldate,COALESCE(channel,''),COALESCE(dstchannel,''),COALESCE(src,''),COALESCE(dst,''),COALESCE(disposition,''),COALESCE(duration,0),COALESCE(billsec,0)," + optional("recordingfile") + "," + optional("rtpqos") + "," + optional("peerrtpqos") + " FROM cdr WHERE " + where + " ORDER BY calldate, " + seq + ", uniqueid, channel LIMIT 200001"
	rows, e := r.Pools.CDR.QueryContext(ctx, q, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Leg{}
	for rows.Next() {
		var l Leg
		var local, peer string
		if e = rows.Scan(&l.LinkedID, &l.UniqueID, &l.Sequence, &l.Start, &l.Channel, &l.DstChannel, &l.Src, &l.Dst, &l.Disposition, &l.Duration, &l.Talk, &l.Recording, &local, &peer); e != nil {
			return nil, e
		}
		l.Start = l.Start.UTC()
		l.LocalQuality = ParseRTPQoS(local)
		l.PeerQuality = ParseRTPQoS(peer)
		out = append(out, l)
		if len(out) > 200000 {
			return nil, ErrBound
		}
	}
	return out, rows.Err()
}
func (r *Repository) timeline(ctx context.Context, where string, args ...any) ([]Event, error) {
	out := []Event{}
	if !r.Pools.CEL {
		return out, nil
	}
	rows, e := r.Pools.CDR.QueryContext(ctx, "SELECT eventtime,eventtype,COALESCE(channame,''),uniqueid FROM cel WHERE "+where+" ORDER BY eventtime,uniqueid,eventtype,channame LIMIT 20001", args...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Event
		if e = rows.Scan(&v.Time, &v.Type, &v.Channel, &v.UniqueID); e != nil {
			return nil, e
		}
		v.Time = v.Time.UTC()
		out = append(out, v)
		if len(out) > 20000 {
			return nil, ErrBound
		}
	}
	return out, rows.Err()
}
func (r *Repository) Detail(ctx context.Context, tenant, id string) (Detail, error) {
	if e := r.Pools.Authorize(tenant); e != nil {
		return Detail{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, r.Pools.Timeout)
	defer cancel()
	legs, e := r.legs(ctx, "linkedid=?", id)
	if e != nil {
		return Detail{}, e
	}
	if len(legs) == 0 {
		return Detail{}, ErrMissing
	}
	events, timelineError := r.timeline(ctx, "linkedid=?", id)
	if timelineError != nil {
		events = []Event{}
	}
	return Detail{Summary: Group(legs, events), Legs: legs, Timeline: events, CELAvailable: r.Pools.CEL && timelineError == nil, QualityAvailable: r.Pools.Columns["rtpqos"] || r.Pools.Columns["peerrtpqos"]}, nil
}

// Period returns the logical calls whose first leg starts in [from, to). The
// result is shared: callers get their own slice but must not modify the
// calls' slices.
func (r *Repository) Period(ctx context.Context, tenant string, from, to time.Time) ([]Call, error) {
	if e := r.Pools.Authorize(tenant); e != nil {
		return nil, e
	}
	key := periodKey{tenant: tenant, from: from.UnixNano(), to: to.UnixNano()}
	if calls, ok := r.cached(key); ok {
		return calls, nil
	}
	ch := r.flight.DoChan(fmt.Sprintf("%s|%d|%d", tenant, key.from, key.to), func() (any, error) {
		// Not tied to one caller: a caller that goes away must not fail the
		// others waiting for the same period.
		calls, e := r.period(context.WithoutCancel(ctx), from, to)
		if e == nil {
			r.store(key, calls)
		}
		return calls, e
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return append([]Call(nil), res.Val.([]Call)...), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (r *Repository) cached(key periodKey) ([]Call, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.cache[key]
	if !ok || time.Since(e.at) > periodTTL {
		return nil, false
	}
	return append([]Call(nil), e.calls...), true
}

func (r *Repository) store(key periodKey, calls []Call) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		r.cache = map[periodKey]periodEntry{}
	}
	for k, e := range r.cache { // drop expired entries
		if time.Since(e.at) > periodTTL {
			delete(r.cache, k)
		}
	}
	for len(r.cache) >= periodEntries { // then the oldest
		var oldest periodKey
		first := true
		for k, e := range r.cache {
			if first || e.at.Before(r.cache[oldest].at) {
				oldest, first = k, false
			}
		}
		delete(r.cache, oldest)
	}
	r.cache[key] = periodEntry{calls: calls, at: time.Now()}
}

func (r *Repository) period(ctx context.Context, from, to time.Time) ([]Call, error) {
	ctx, cancel := context.WithTimeout(ctx, r.Pools.Timeout)
	defer cancel()
	release, e := r.acquire(ctx)
	if e != nil {
		return nil, e
	}
	defer release()
	sf, st := from.In(r.sourceLocation()), to.In(r.sourceLocation())
	legs, e := r.legs(ctx, periodWhere, sf, st, sf)
	if e != nil {
		return nil, e
	}
	groups := map[string][]Leg{}
	for _, l := range legs {
		groups[l.LinkedID] = append(groups[l.LinkedID], l)
	}
	calls := make([]Call, 0, len(groups))
	for _, ls := range groups {
		calls = append(calls, Group(ls, nil))
	}
	if r.Pools.CEL && len(calls) > 0 {
		rows, e := r.Pools.CDR.QueryContext(ctx, "SELECT uniqueid,MIN(CASE WHEN eventtype='CHAN_START' THEN eventtime END),MIN(CASE WHEN eventtype='ANSWER' THEN eventtime END) FROM cel WHERE eventtime>=? AND eventtime<? AND eventtype IN ('CHAN_START','ANSWER') GROUP BY uniqueid LIMIT 200001", from.In(r.sourceLocation()), to.Add(24*time.Hour).In(r.sourceLocation()))
		if e == nil {
			defer rows.Close()
			times := map[string][]Event{}
			for rows.Next() {
				var id string
				var start, answer sql.NullTime
				if rows.Scan(&id, &start, &answer) == nil && start.Valid && answer.Valid {
					times[id] = []Event{{UniqueID: id, Type: "CHAN_START", Time: start.Time}, {UniqueID: id, Type: "ANSWER", Time: answer.Time}}
				}
			}
			for i, c := range calls {
				events := []Event{}
				for _, l := range groups[c.LinkedID] {
					events = append(events, times[l.UniqueID]...)
				}
				calls[i] = Group(groups[c.LinkedID], events)
			}
		}
	}
	return calls, nil
}
func (r *Repository) sourceLocation() *time.Location {
	l, _ := time.LoadLocation(r.Pools.Binding.SourceTimezone)
	if l == nil {
		return time.UTC
	}
	return l
}
func (r *Repository) List(ctx context.Context, tenant string, f Filter) (List, error) {
	calls, e := r.Period(ctx, tenant, f.From, f.To)
	if e != nil {
		return List{}, e
	}
	// Filter and sort indexes, not the (large) Call values, and copy out only
	// the requested page.
	idx := make([]int, 0, len(calls))
	for i := range calls {
		c := &calls[i]
		if f.Src != "" && !strings.Contains(c.Src, f.Src) {
			continue
		}
		if f.Dst != "" && !strings.Contains(c.Dst, f.Dst) {
			continue
		}
		if f.Direction != "" && c.Direction != f.Direction {
			continue
		}
		if f.Disposition != "" && c.Disposition != f.Disposition {
			continue
		}
		if f.Extension != "" {
			found := false
			for _, ext := range c.Extensions {
				if ext == f.Extension {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		idx = append(idx, i)
	}
	sort.Slice(idx, func(i, j int) bool {
		a, b := &calls[idx[i]], &calls[idx[j]]
		cmp := 0
		switch f.Sort {
		case "src":
			cmp = strings.Compare(a.Src, b.Src)
		case "dst":
			cmp = strings.Compare(a.Dst, b.Dst)
		case "durationSeconds":
			if a.Duration < b.Duration {
				cmp = -1
			} else if a.Duration > b.Duration {
				cmp = 1
			}
		default:
			if a.Start.Before(b.Start) {
				cmp = -1
			} else if a.Start.After(b.Start) {
				cmp = 1
			}
		}
		if cmp == 0 {
			cmp = strings.Compare(a.LinkedID, b.LinkedID)
		}
		if f.Order == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	total := len(idx)
	start := (f.Page - 1) * f.PageSize
	if start > total {
		start = total
	}
	end := start + f.PageSize
	if end > total {
		end = total
	}
	out := make([]Call, 0, end-start)
	for _, i := range idx[start:end] {
		out = append(out, calls[i])
	}
	return List{Items: out, Total: total, Page: f.Page, PageSize: f.PageSize, Sort: f.Sort, Order: f.Order}, nil
}
