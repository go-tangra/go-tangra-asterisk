package support

import (
	"sort"
	"time"
)

// Spread is a distribution of seconds: median and 90th percentile (nil when
// there is no sample).
type Spread struct {
	Count  int      `json:"count"`
	Median *float64 `json:"median"`
	P90    *float64 `json:"p90"`
}

func spread(v []int64) Spread {
	s := Spread{Count: len(v)}
	if len(v) == 0 {
		return s
	}
	sorted := append([]int64(nil), v...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	q := func(p float64) *float64 { // nearest rank
		i := int(p*float64(len(sorted))+0.999999) - 1
		if i < 0 {
			i = 0
		}
		f := float64(sorted[i])
		return &f
	}
	s.Median, s.P90 = q(0.5), q(0.9)
	return s
}

// Summary is the team and caller view of a period.
type Summary struct {
	Inbound struct {
		Calls          int            `json:"calls"`
		Answered       int            `json:"answered"`
		AbandonedEarly int            `json:"abandonedBeforeGroup"`
		Lost           int            `json:"lost"`
		LostBy         map[string]int `json:"lostByReason"`
		CalledBack     int            `json:"calledBack"`
		Wait           Spread         `json:"waitSeconds"` // group start to answer
		Announcement   Spread         `json:"announcementSeconds"`
		Talk           int64          `json:"talkSeconds"`
		Direct         int            `json:"directCalls"`
		DirectAnswered int            `json:"directAnswered"`
	} `json:"inbound"`
	Outbound struct {
		Attempts  int   `json:"attempts"`
		Connected int   `json:"connected"`
		Talk      int64 `json:"talkSeconds"`
	} `json:"outbound"`
}

// AgentStat is one agent's work in a period.
type AgentStat struct {
	Extension string `json:"extension"`
	Name      string `json:"name"`
	Member    bool   `json:"member"` // in the support team
	Inbound   struct {
		Offered           int     `json:"offered"`
		Answered          int     `json:"answered"`
		Declined          int     `json:"declined"`
		ColleagueAnswered int     `json:"colleagueAnswered"`
		RangWhileLost     int     `json:"rangWhileLost"`
		AnswerShare       float64 `json:"answerShare"` // of the team's answered group calls
		TimeToAnswer      Spread  `json:"timeToAnswerSeconds"`
		Talk              int64   `json:"talkSeconds"`
		DirectAnswered    int     `json:"directAnswered"`
		DirectTalk        int64   `json:"directTalkSeconds"`
	} `json:"inbound"`
	Outbound struct {
		Attempts  int   `json:"attempts"`
		Connected int   `json:"connected"`
		Talk      int64 `json:"talkSeconds"`
		Callbacks int   `json:"callbacks"`
	} `json:"outbound"`
}

// Lost is a lost (or announcement-abandoned) inbound group call and whether
// someone called the caller back.
type Lost struct {
	LinkedID    string     `json:"linkedid"`
	Start       time.Time  `json:"start"`
	Caller      string     `json:"caller"`
	Reason      string     `json:"reason"`
	Outcome     string     `json:"outcome"`
	WaitSeconds *int64     `json:"waitSeconds"`
	Rang        []string   `json:"rang"`
	CallbackAt  *time.Time `json:"callbackAt"`
	CallbackBy  string     `json:"callbackBy"`
}

func add(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// callbacks finds, for each lost call of calls, the first connected
// outbound call to the same number within window after it ended. calls must
// also cover the window after the period (Period(from, to+window)).
func callbacks(calls []Call, window time.Duration) map[string]*Call {
	byKey := map[string][]*Call{}
	for i := range calls {
		c := &calls[i]
		if c.Kind == KindOutbound && c.Outcome == OutcomeConnected {
			if k := CallerKey(c.Caller); k != "" {
				byKey[k] = append(byKey[k], c)
			}
		}
	}
	out := map[string]*Call{}
	for i := range calls {
		c := &calls[i]
		if c.Kind != KindInboundGroup || (c.Outcome != OutcomeLost && c.Outcome != OutcomeAbandonedEarly) {
			continue
		}
		for _, o := range byKey[CallerKey(c.Caller)] { // oldest first
			if !o.Start.Before(c.End) && o.Start.Sub(c.End) <= window {
				out[c.LinkedID] = o
				break
			}
		}
	}
	return out
}

func within(c Call, from, to time.Time) bool { return !c.Start.Before(from) && c.Start.Before(to) }

// Summarize builds the team summary of the calls that started in [from,to).
func Summarize(calls []Call, from, to time.Time, window time.Duration) Summary {
	var s Summary
	s.Inbound.LostBy = map[string]int{}
	back := callbacks(calls, window)
	var wait, ann []int64
	for _, c := range calls {
		if !within(c, from, to) {
			continue
		}
		switch c.Kind {
		case KindInboundGroup:
			s.Inbound.Calls++
			if c.Announcement != nil {
				ann = append(ann, *c.Announcement)
			}
			switch c.Outcome {
			case OutcomeAnswered:
				s.Inbound.Answered++
				s.Inbound.Talk += add(c.Talk)
				if c.Ring != nil {
					wait = append(wait, *c.Ring)
				}
			case OutcomeAbandonedEarly:
				s.Inbound.AbandonedEarly++
			case OutcomeLost:
				s.Inbound.Lost++
				s.Inbound.LostBy[c.LostReason]++
			}
			if back[c.LinkedID] != nil {
				s.Inbound.CalledBack++
			}
		case KindInboundDirect:
			s.Inbound.Direct++
			if c.Outcome == OutcomeAnswered {
				s.Inbound.DirectAnswered++
			}
		case KindOutbound:
			s.Outbound.Attempts++
			if c.Outcome == OutcomeConnected {
				s.Outbound.Connected++
				s.Outbound.Talk += add(c.Talk)
			}
		}
	}
	s.Inbound.Wait, s.Inbound.Announcement = spread(wait), spread(ann)
	return s
}

// Agents builds per-agent statistics for [from,to). Every team member is
// listed (with zeros when idle); other extensions appear when they took part.
func Agents(calls []Call, from, to time.Time, window time.Duration, team []string, names map[string]string) []*AgentStat {
	stats := map[string]*AgentStat{}
	get := func(ext string) *AgentStat {
		a := stats[ext]
		if a == nil {
			a = &AgentStat{Extension: ext, Name: names[ext]}
			stats[ext] = a
		}
		return a
	}
	for _, ext := range team {
		get(ext).Member = true
	}
	rings := map[string][]int64{}
	answered := 0
	back := callbacks(calls, window)
	for _, c := range calls {
		if !within(c, from, to) {
			continue
		}
		if o := back[c.LinkedID]; o != nil && o.Agent != "" { // credited for the period of the lost call
			get(o.Agent).Outbound.Callbacks++
		}
		switch c.Kind {
		case KindInboundGroup:
			if c.Outcome == OutcomeAnswered {
				answered++
			}
			for _, l := range c.Legs {
				a := get(l.Extension)
				a.Inbound.Offered++
				switch l.Outcome {
				case LegAnswered:
					a.Inbound.Answered++
					a.Inbound.Talk += l.Talk
					rings[l.Extension] = append(rings[l.Extension], l.Ring)
				case LegDeclined:
					a.Inbound.Declined++
				case LegColleagueAnswered:
					a.Inbound.ColleagueAnswered++
				default:
					a.Inbound.RangWhileLost++
				}
			}
		case KindInboundDirect:
			if c.Outcome == OutcomeAnswered && c.Agent != "" {
				a := get(c.Agent)
				a.Inbound.DirectAnswered++
				a.Inbound.DirectTalk += add(c.Talk)
			}
		case KindOutbound:
			if c.Agent == "" {
				continue
			}
			a := get(c.Agent)
			a.Outbound.Attempts++
			if c.Outcome == OutcomeConnected {
				a.Outbound.Connected++
				a.Outbound.Talk += add(c.Talk)
			}
		}
	}
	out := make([]*AgentStat, 0, len(stats))
	for ext, a := range stats {
		a.Inbound.TimeToAnswer = spread(rings[ext])
		if answered > 0 {
			a.Inbound.AnswerShare = float64(a.Inbound.Answered) / float64(answered)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Inbound.Answered != out[j].Inbound.Answered {
			return out[i].Inbound.Answered > out[j].Inbound.Answered
		}
		return out[i].Extension < out[j].Extension
	})
	return out
}

// LostCalls lists the lost and announcement-abandoned group calls of
// [from,to), newest first, with their callback.
func LostCalls(calls []Call, from, to time.Time, window time.Duration) []Lost {
	back := callbacks(calls, window)
	var out []Lost
	for _, c := range calls {
		if !within(c, from, to) || c.Kind != KindInboundGroup || (c.Outcome != OutcomeLost && c.Outcome != OutcomeAbandonedEarly) {
			continue
		}
		l := Lost{LinkedID: c.LinkedID, Start: c.Start, Caller: c.Caller, Reason: c.LostReason, Outcome: c.Outcome, WaitSeconds: c.CallerWait, Rang: []string{}}
		for _, leg := range c.Legs {
			l.Rang = append(l.Rang, leg.Extension)
		}
		if o := back[c.LinkedID]; o != nil {
			t := o.Start
			l.CallbackAt, l.CallbackBy = &t, o.Agent
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.After(out[j].Start) })
	return out
}
