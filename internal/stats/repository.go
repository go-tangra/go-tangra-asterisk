package stats

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"sort"
	"time"
)

type Bucket struct {
	Start    time.Time `json:"start"`
	Total    int       `json:"total"`
	Answered int       `json:"answered"`
	Missed   int       `json:"missed"`
}
type Extension struct {
	Timezone          string   `json:"timezone"`
	Extension         string   `json:"extension"`
	Name              string   `json:"name"`
	Total             int      `json:"total"`
	Inbound           int      `json:"inbound"`
	Outbound          int      `json:"outbound"`
	Answered          int      `json:"answered"`
	Missed            int      `json:"missed"`
	TalkSeconds       int64    `json:"talkSeconds"`
	MeanTalkSeconds   float64  `json:"meanTalkSeconds"`
	MeanPickupSeconds *float64 `json:"meanPickupSeconds"`
	WorkloadShare     float64  `json:"workloadShare"`
	BusiestHour       int      `json:"busiestHour"`
	HourOfDay         [24]int  `json:"hourOfDay"`
	Series            []Bucket `json:"series"`
	pickupSum         float64
	pickupN           int
	externalTalk      int64
}
type Overview struct {
	Timezone          string                `json:"timezone"`
	Total             int                   `json:"total"`
	Answered          int                   `json:"answered"`
	Missed            int                   `json:"missed"`
	MeanTalkSeconds   float64               `json:"meanTalkSeconds"`
	MeanPickupSeconds *float64              `json:"meanPickupSeconds"`
	Series            []Bucket              `json:"series"`
	Extensions        map[string]*Extension `json:"-"`
}

func Aggregate(calls []cdr.Call, loc *time.Location, bucket string) Overview {
	o := Overview{Timezone: loc.String(), Series: []Bucket{}, Extensions: map[string]*Extension{}}
	series := map[time.Time]*Bucket{}
	extSeries := map[string]map[time.Time]*Bucket{}
	var talk, external int64
	pickupSum := 0.0
	pickupN := 0
	add := func(m map[time.Time]*Bucket, c cdr.Call) {
		at := BucketStart(c.Start, loc, bucket)
		b := m[at]
		if b == nil {
			b = &Bucket{Start: at}
			m[at] = b
		}
		b.Total++
		if c.Disposition == "ANSWERED" {
			b.Answered++
		} else {
			b.Missed++
		}
	}
	for _, c := range calls {
		o.Total++
		if c.Disposition == "ANSWERED" {
			o.Answered++
			talk += c.Talk
		} else {
			o.Missed++
		}
		if c.Pickup != nil {
			pickupN++
			pickupSum += *c.Pickup
		}
		add(series, c)
		exts := c.Extensions
		if len(exts) == 0 {
			exts = []string{c.OriginatingExtension, c.AnsweredExtension}
		}
		seen := map[string]bool{}
		for _, id := range exts {
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			e := o.Extensions[id]
			if e == nil {
				e = &Extension{Timezone: loc.String(), Extension: id, Series: []Bucket{}}
				o.Extensions[id] = e
				extSeries[id] = map[time.Time]*Bucket{}
			}
			e.Total++
			e.HourOfDay[c.Start.In(loc).Hour()]++
			add(extSeries[id], c)
			if c.Direction == "inbound" {
				e.Inbound++
			}
			if c.Direction == "outbound" {
				e.Outbound++
			}
			// The caller of an answered outbound or internal call sees it answered.
			caller := (c.Direction == "outbound" || c.Direction == "internal") && c.OriginatingExtension == id
			ownsAnswer := c.AnsweredExtension == id || caller
			if c.Disposition == "ANSWERED" && ownsAnswer {
				e.Answered++
				e.TalkSeconds += c.Talk
				if c.Pickup != nil && (c.AnsweredExtension == id || c.Direction == "outbound") {
					e.pickupSum += *c.Pickup
					e.pickupN++
				}
				if c.Direction != "internal" && c.Direction != "unknown" {
					e.externalTalk += c.Talk
					external += c.Talk
				}
			} else {
				e.Missed++
			}
		}
	}
	if o.Answered > 0 {
		o.MeanTalkSeconds = float64(talk) / float64(o.Answered)
	}
	if pickupN > 0 {
		v := pickupSum / float64(pickupN)
		o.MeanPickupSeconds = &v
	}
	o.Series = sortedBuckets(series)
	for id, e := range o.Extensions {
		if e.Answered > 0 {
			e.MeanTalkSeconds = float64(e.TalkSeconds) / float64(e.Answered)
		}
		if e.pickupN > 0 {
			v := e.pickupSum / float64(e.pickupN)
			e.MeanPickupSeconds = &v
		}
		if external > 0 {
			e.WorkloadShare = float64(e.externalTalk) / float64(external)
		}
		for h, n := range e.HourOfDay {
			if n > e.HourOfDay[e.BusiestHour] {
				e.BusiestHour = h
			}
		}
		e.Series = sortedBuckets(extSeries[id])
	}
	return o
}
func sortedBuckets(m map[time.Time]*Bucket) []Bucket {
	out := make([]Bucket, 0, len(m))
	for _, v := range m {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

type Repository struct {
	CDR      *cdr.Repository
	Location *time.Location
}

func (r *Repository) Overview(ctx context.Context, tenant string, from, to time.Time, bucket string) (Overview, error) {
	calls, e := r.CDR.Period(ctx, tenant, from, to)
	if e != nil {
		return Overview{}, e
	}
	return Aggregate(calls, r.Location, bucket), nil
}

type DirectoryEntry struct {
	Extension string `json:"extension"`
	Name      string `json:"name"`
}

func (r *Repository) Directory(ctx context.Context, tenant string) ([]DirectoryEntry, error) {
	if e := r.CDR.Pools.Authorize(tenant); e != nil {
		return nil, e
	}
	out := []DirectoryEntry{}
	if !r.CDR.Pools.Names {
		return out, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.CDR.Pools.Timeout)
	defer cancel()
	rows, e := r.CDR.Pools.Config.QueryContext(ctx, "SELECT extension,COALESCE(name,'') FROM users WHERE extension<>'' ORDER BY extension LIMIT 10001")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v DirectoryEntry
		if e = rows.Scan(&v.Extension, &v.Name); e != nil {
			return nil, e
		}
		out = append(out, v)
		if len(out) > 10000 {
			return nil, cdr.ErrBound
		}
	}
	return out, rows.Err()
}

type Ringgroup struct {
	Total       int        `json:"total"`
	Answered    int        `json:"answered"`
	NoAnswer    int        `json:"noAnswer"`
	Busy        int        `json:"allBusy"`
	Failed      int        `json:"failed"`
	MissedCalls []cdr.Call `json:"missedCalls"`
}

func (r *Repository) Ringgroup(ctx context.Context, tenant, id string, from, to time.Time) (Ringgroup, error) {
	calls, e := r.CDR.Period(ctx, tenant, from, to)
	v := Ringgroup{MissedCalls: []cdr.Call{}}
	if e != nil {
		return v, e
	}
	sort.Slice(calls, func(i, j int) bool { return calls[i].Start.After(calls[j].Start) })
	for _, c := range calls {
		match := false
		for _, dst := range c.Destinations {
			if dst == id {
				match = true
			}
		}
		if !match {
			continue
		}
		v.Total++
		switch c.Disposition {
		case "ANSWERED":
			v.Answered++
		case "BUSY":
			v.Busy++
		case "FAILED":
			v.Failed++
		default:
			v.NoAnswer++
		}
		if c.Disposition != "ANSWERED" && len(v.MissedCalls) < 100 {
			v.MissedCalls = append(v.MissedCalls, c)
		}
	}
	return v, nil
}
