package registration

import (
	"sort"
	"time"
)

// Timeline states.
const (
	Online  = "online"  // at least one contact registered and not expired
	Offline = "offline" // observed, and no contact registered
	Unknown = "unknown" // not observed: before the first event, or in a gap
)

// Segment is a stretch of an extension's timeline with one state. Online
// segments name the contact that was registered (the most recently observed
// one when several were).
type Segment struct {
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	State      string    `json:"state"`
	Contact    string    `json:"contact,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	ViaAddress string    `json:"viaAddress,omitempty"`
}

// Summary totals a timeline. Dropouts counts online → offline changes;
// unknown time is never counted as offline.
type Summary struct {
	OnlineSeconds         int64 `json:"onlineSeconds"`
	OfflineSeconds        int64 `json:"offlineSeconds"`
	UnknownSeconds        int64 `json:"unknownSeconds"`
	Dropouts              int   `json:"dropouts"`
	LongestOfflineSeconds int64 `json:"longestOfflineSeconds"`
}

type Timeline struct {
	Extension string    `json:"extension"`
	From      time.Time `json:"from"`
	To        time.Time `json:"to"`
	Segments  []Segment `json:"segments"`
	Gaps      []Gap     `json:"gaps"`
	Summary   Summary   `json:"summary"`
}

// BuildTimeline turns an extension's registration events into consecutive
// segments covering [from, to). events must hold the last event of every
// contact before from (the state the period starts in) and every event in
// the period; events of other extensions are ignored. A segment changes
// where an event lands, a registration expires, or an observation gap starts
// or ends; adjacent segments with the same state and contact are merged.
func BuildTimeline(ext string, from, to time.Time, input []Event, gaps []Gap) Timeline {
	from, to = from.UTC(), to.UTC()
	t := Timeline{Extension: ext, From: from, To: to, Segments: []Segment{}, Gaps: []Gap{}}
	if !to.After(from) {
		return t
	}
	events := make([]Event, 0, len(input))
	for _, e := range input {
		if e.Endpoint == ext && e.Time.Before(to) {
			events = append(events, e)
		}
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Time.Equal(events[j].Time) {
			return events[i].ID < events[j].ID
		}
		return events[i].Time.Before(events[j].Time)
	})
	for _, g := range gaps {
		if g.Start.Before(to) && (g.End.IsZero() || g.End.After(from)) {
			t.Gaps = append(t.Gaps, g)
		}
	}

	cuts := []time.Time{from, to}
	inside := func(v time.Time) {
		if v.After(from) && v.Before(to) {
			cuts = append(cuts, v)
		}
	}
	for _, e := range events {
		inside(e.Time)
		if !e.Expire.IsZero() {
			inside(e.Expire)
		}
	}
	for _, g := range t.Gaps {
		inside(g.Start)
		if !g.End.IsZero() {
			inside(g.End)
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].Before(cuts[j]) })

	contacts := map[string]Event{}
	next := 0
	for i := 0; i+1 < len(cuts); i++ {
		start, end := cuts[i], cuts[i+1]
		if !end.After(start) {
			continue
		}
		for next < len(events) && !events[next].Time.After(start) {
			contacts[events[next].Contact] = events[next]
			next++
		}
		seg := Segment{Start: start, End: end, State: Unknown}
		if len(contacts) > 0 && !inGap(t.Gaps, start) {
			seg.State = Offline
			var latest *Event
			for _, e := range contacts {
				if registeredAt(e, start) && (latest == nil || e.Time.After(latest.Time) || (e.Time.Equal(latest.Time) && e.ID > latest.ID)) {
					e := e
					latest = &e
				}
			}
			if latest != nil {
				seg.State, seg.Contact, seg.UserAgent, seg.ViaAddress = Online, latest.Contact, latest.UserAgent, latest.ViaAddress
			}
		}
		if n := len(t.Segments); n > 0 {
			last := &t.Segments[n-1]
			if last.State == seg.State && last.Contact == seg.Contact {
				last.End = end
				continue
			}
		}
		t.Segments = append(t.Segments, seg)
	}

	for i, s := range t.Segments {
		d := int64(s.End.Sub(s.Start) / time.Second)
		switch s.State {
		case Online:
			t.Summary.OnlineSeconds += d
		case Offline:
			t.Summary.OfflineSeconds += d
			t.Summary.LongestOfflineSeconds = max(t.Summary.LongestOfflineSeconds, d)
			if i > 0 && t.Segments[i-1].State == Online {
				t.Summary.Dropouts++
			}
		default:
			t.Summary.UnknownSeconds += d
		}
	}
	return t
}

func inGap(gaps []Gap, t time.Time) bool {
	for _, g := range gaps {
		if !t.Before(g.Start) && (g.End.IsZero() || t.Before(g.End)) {
			return true
		}
	}
	return false
}
