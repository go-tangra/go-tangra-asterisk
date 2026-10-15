package support

import (
	"sort"
	"strings"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
)

// Search types: what a call was, as the call search offers it.
const (
	TypeAnswered     = "answered"      // inbound, answered by an agent
	TypeLost         = "lost"          // inbound group call nobody took (incl. hung up in the welcome)
	TypeOutbound     = "outbound"      // outbound, connected
	TypeNotConnected = "not-connected" // outbound, not connected
)

// Filter narrows the call search. Zero values do not filter.
type Filter struct {
	Agent      string // answered or dialled by, or rang for
	Type       string
	Caller     string // digits contained in the external number
	MinTalk    int64
	BadQuality bool
	Recorded   bool
}

// Row is one call in the search.
type Row struct {
	LinkedID    string    `json:"linkedid"`
	Start       time.Time `json:"start"`
	Type        string    `json:"type"`
	Kind        string    `json:"kind"`
	Outcome     string    `json:"outcome"`
	LostReason  string    `json:"lostReason"`
	Agent       string    `json:"agent"`
	Caller      string    `json:"caller"`
	Wait        *int64    `json:"waitSeconds"`
	Talk        *int64    `json:"talkSeconds"`
	Rang        []string  `json:"rang"`
	Recorded    bool      `json:"recorded"`
	Quality     string    `json:"quality"`     // good, fair, poor or "" when not measured
	QualitySide string    `json:"qualitySide"` // trunk or phone: the leg measured
}

// Grade rates a leg's RTP report as docs/support-metrics.md defines "bad":
// loss over 3 %, jitter over 30 ms or round trip over 300 ms, or a poor MOS.
func Grade(raw string) string {
	q := cdr.ParseRTPQoS(raw)
	if q == nil {
		return ""
	}
	over := func(v *float64, limit float64) bool { return v != nil && *v > limit }
	switch {
	case q.Quality == "POOR" || q.Quality == "BAD",
		over(q.RxLossPercent, 3), over(q.TxLossPercent, 3),
		over(q.RxJitterMs, 30), over(q.TxJitterMs, 30), over(q.RTTMs, 300):
		return "poor"
	case q.Quality == "FAIR":
		return "fair"
	default:
		return "good"
	}
}

func typeOf(c Call) string {
	switch {
	case (c.Kind == KindInboundGroup || c.Kind == KindInboundDirect) && c.Outcome == OutcomeAnswered:
		return TypeAnswered
	case c.Kind == KindInboundGroup:
		return TypeLost
	case c.Kind == KindOutbound && c.Outcome == OutcomeConnected:
		return TypeOutbound
	case c.Kind == KindOutbound:
		return TypeNotConnected
	}
	return ""
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Search returns the support calls of [from,to) matching f, newest first.
// Internal and other calls are not part of it.
func Search(calls []Call, from, to time.Time, f Filter) []Row {
	want := digits(f.Caller)
	var out []Row
	for _, c := range calls {
		t := typeOf(c)
		if !within(c, from, to) || t == "" || (f.Type != "" && f.Type != t) {
			continue
		}
		r := Row{LinkedID: c.LinkedID, Start: c.Start, Type: t, Kind: c.Kind, Outcome: c.Outcome, LostReason: c.LostReason, Agent: c.Agent, Caller: c.Caller, Wait: c.CallerWait, Talk: c.Talk, Recorded: c.Recorded, Quality: Grade(c.Quality), QualitySide: c.QualitySide, Rang: []string{}}
		// The agent's calls are those they took or dialled. Ringing alone
		// does not count: a group call rings every member of the group.
		involved := f.Agent == "" || c.Agent == f.Agent
		for _, l := range c.Legs {
			r.Rang = append(r.Rang, l.Extension)
			involved = involved || (l.Extension == f.Agent && l.Outcome == LegAnswered)
		}
		switch {
		case !involved,
			want != "" && !strings.Contains(digits(c.Caller), want),
			f.MinTalk > 0 && (c.Talk == nil || *c.Talk < f.MinTalk),
			f.BadQuality && r.Quality != "poor",
			f.Recorded && !c.Recorded:
			continue
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	return out
}
