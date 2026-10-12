// Package support turns Asterisk CDR/CEL rows into the support-team facts
// described in docs/support-metrics.md: one Call per linkedid and one
// AgentLeg per extension that rang, plus the reports built on them.
package support

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// CDRRow and CELRow are the source rows of one call, as read from the PBX.
type CDRRow struct {
	LinkedID, UniqueID  string
	Sequence            int64
	Start               time.Time
	Channel, DstChannel string
	Src, Dst            string
	DContext, LastApp   string
	Disposition         string
	Duration, Billsec   int64
	Recorded            bool
	RTPQoS, PeerRTPQoS  string
}
type CELRow struct {
	ID                 int64
	Type               string
	Time               time.Time
	CIDNum, Exten      string
	Context, Channel   string
	App, AppData       string
	UniqueID, LinkedID string
	Peer, Extra        string
}

// Routing is the PBX configuration the rules need.
type Routing struct {
	Trunks        map[string]bool   // trunk channel names (PJSIP/<name>-…)
	Announcements map[string]string // announcement id -> ring group it leads to
	GroupTime     map[string]int    // ring group -> ring time in seconds
}

// Call kinds, outcomes and agent-leg outcomes (docs/support-metrics.md).
const (
	KindInboundGroup  = "inbound-group"
	KindInboundDirect = "inbound-direct"
	KindInboundOther  = "inbound-other"
	KindOutbound      = "outbound"
	KindInternal      = "internal"
	KindIgnored       = "ignored"

	OutcomeAnswered       = "answered"
	OutcomeLost           = "lost"
	OutcomeAbandonedEarly = "abandoned-before-group"
	OutcomeConnected      = "connected"
	OutcomeNotConnected   = "not-connected"
	OutcomeUnanswered     = "unanswered"

	LostNoAgent    = "no-agent-available"
	LostCallerLeft = "caller-hung-up-while-ringing"
	LostRangOut    = "rang-out"

	LegAnswered          = "answered"
	LegDeclined          = "declined"
	LegColleagueAnswered = "colleague-answered"
	LegCallerHungUp      = "caller-hung-up"
	LegRangOut           = "rang-out"
)

// Call is one classified call. Durations are whole seconds; nil when the
// measure does not apply.
type Call struct {
	LinkedID     string
	Start, End   time.Time
	Kind         string
	Group        string
	Outcome      string
	LostReason   string
	Caller       string // external party (inbound caller, outbound callee)
	Agent        string // answering (inbound) or dialling (outbound) extension
	Trunk        string
	Destination  string // inbound-other: where the call went
	DialStatus   string
	Announcement *int64
	Ring         *int64 // group: group start to answer or end; direct: agent ring
	CallerWait   *int64 // inbound: call start to answer or end
	Talk         *int64
	Setup        *int64 // outbound: dial to connect, or to give-up
	Recorded     bool
	Quality      string // rtpqos of the agent's leg, raw
	Legs         []AgentLeg
}

// AgentLeg is one extension ringing for one inbound call.
type AgentLeg struct {
	Extension string
	Start     time.Time
	Outcome   string
	Ring      int64 // leg start to answer or hangup
	Talk      int64
}

type channel struct {
	name               string // PJSIP/49-0000e9ce
	endpoint           string // 49
	context            string // of CHAN_START
	start, end, answer time.Time
	answered, ended    bool
	cause              int
	source, dialStatus string
	talk               time.Duration
	bridged            time.Time
}

var (
	endpointRe = regexp.MustCompile(`^[A-Za-z0-9]+/(.+)-[0-9a-f]+$`)
	groupRe    = regexp.MustCompile(`(?:^|/)rg-([0-9]+)-`)
)

// isTrunk tells trunk channels apart: by the configured trunk names, or
// without configuration by a non-numeric endpoint (extensions are numbers).
func (r Routing) isTrunk(ep string) bool {
	if r.Trunks != nil {
		return r.Trunks[ep]
	}
	_, err := strconv.Atoi(ep)
	return ep != "" && err != nil
}

func endpoint(ch string) string {
	if m := endpointRe.FindStringSubmatch(ch); m != nil {
		return m[1]
	}
	return ""
}
func secs(d time.Duration) *int64 { s := int64(d.Round(time.Second) / time.Second); return &s }

// Classify applies the rules to one call's rows. Events must all share one
// linkedid; CDR rows may be empty (CDR does not log every call).
func Classify(r Routing, cdrs []CDRRow, events []CELRow) Call {
	evs := append([]CELRow(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool {
		if !evs[i].Time.Equal(evs[j].Time) {
			return evs[i].Time.Before(evs[j].Time)
		}
		return evs[i].ID < evs[j].ID
	})
	c := Call{Kind: KindIgnored}
	if len(evs) == 0 {
		return c
	}
	c.LinkedID, c.Start, c.End = evs[0].LinkedID, evs[0].Time, evs[len(evs)-1].Time
	chans := map[string]*channel{}
	var order []*channel
	var groupAt time.Time
	lastContext := ""
	for _, e := range evs {
		ch := chans[e.Channel]
		if ch == nil && e.Channel != "" {
			ch = &channel{name: e.Channel, endpoint: endpoint(e.Channel), start: e.Time}
			chans[e.Channel] = ch
			order = append(order, ch)
		}
		switch e.Type {
		case "CHAN_START":
			ch.start, ch.context = e.Time, e.Context
		case "ANSWER":
			if !ch.answered {
				ch.answered, ch.answer = true, e.Time
			}
		case "BRIDGE_ENTER":
			ch.bridged = e.Time
		case "BRIDGE_EXIT":
			if !ch.bridged.IsZero() {
				ch.talk += e.Time.Sub(ch.bridged)
				ch.bridged = time.Time{}
			}
		case "HANGUP":
			ch.ended, ch.end = true, e.Time
			var x struct {
				Cause      int    `json:"hangupcause"`
				Source     string `json:"hangupsource"`
				DialStatus string `json:"dialstatus"`
			}
			if json.Unmarshal([]byte(e.Extra), &x) == nil {
				ch.cause, ch.source, ch.dialStatus = x.Cause, x.Source, x.DialStatus
			}
		case "APP_START":
			if m := groupRe.FindStringSubmatch(e.AppData); m != nil && strings.EqualFold(e.App, "MixMonitor") && c.Group == "" {
				c.Group, groupAt = m[1], e.Time
			}
		}
		if e.Context != "" && !strings.HasPrefix(e.Context, "macro-") && !strings.HasPrefix(e.Context, "sub-") && !strings.HasPrefix(e.Context, "func-") {
			lastContext = e.Context // where the call got to, ignoring subroutines
		}
	}
	for _, ch := range order {
		if !ch.bridged.IsZero() && ch.ended {
			ch.talk += ch.end.Sub(ch.bridged)
		}
	}
	first := order[0]
	isTrunk := r.isTrunk
	for _, row := range cdrs {
		c.Recorded = c.Recorded || row.Recorded
		if c.Group == "" && row.DContext == "ext-group" && row.Dst != "" {
			c.Group, groupAt = row.Dst, row.Start
		}
	}
	if isTrunk(first.endpoint) {
		inbound(&c, r, first, order, groupAt, lastContext, cdrs)
		return c
	}
	var trunk *channel
	others := 0
	for _, ch := range order[1:] {
		if isTrunk(ch.endpoint) && trunk == nil {
			trunk = ch
		}
		if ch.endpoint != first.endpoint {
			others++
		}
	}
	switch {
	case trunk != nil:
		outbound(&c, first, trunk, cdrs)
	case others > 0:
		c.Kind, c.Agent = KindInternal, first.endpoint
	}
	return c
}

func inbound(c *Call, r Routing, caller *channel, order []*channel, groupAt time.Time, lastContext string, cdrs []CDRRow) {
	c.Trunk = caller.endpoint
	for _, row := range cdrs {
		if row.Channel == caller.name && row.Src != "" {
			c.Caller = row.Src
			break
		}
	}
	var legs []*channel
	var answered *channel
	for _, ch := range order[1:] {
		if r.isTrunk(ch.endpoint) || ch.endpoint == "" || ch.context != "from-internal" {
			continue
		}
		legs = append(legs, ch)
		if ch.answered && answered == nil {
			answered = ch
		}
	}
	end := c.End
	if caller.ended {
		end = caller.end
	}
	if c.Group == "" && strings.HasPrefix(lastContext, "app-announcement-") {
		c.Group = r.Announcements[strings.TrimPrefix(lastContext, "app-announcement-")]
	}
	if c.Group == "" && len(legs) == 0 {
		c.Kind, c.Destination = KindInboundOther, strings.TrimPrefix(strings.TrimPrefix(lastContext, "ext-"), "app-")
		if strings.HasPrefix(lastContext, "ext-fax") {
			c.Destination = "fax"
		}
		return
	}
	if c.Group == "" {
		c.Kind = KindInboundDirect
	} else {
		c.Kind = KindInboundGroup
	}
	if groupAt.IsZero() && len(legs) > 0 {
		groupAt = legs[0].start
	}
	if c.Kind == KindInboundGroup {
		if groupAt.IsZero() {
			c.Outcome = OutcomeAbandonedEarly
			c.Announcement = secs(end.Sub(c.Start))
			return
		}
		c.Announcement = secs(groupAt.Sub(c.Start))
	}
	if answered != nil {
		c.Outcome, c.Agent = OutcomeAnswered, answered.endpoint
		c.CallerWait = secs(answered.answer.Sub(c.Start))
		c.Talk = secs(answered.talk)
		if c.Kind == KindInboundGroup {
			c.Ring = secs(answered.answer.Sub(groupAt))
		} else {
			c.Ring = secs(answered.answer.Sub(answered.start))
		}
		for _, row := range cdrs {
			if row.DstChannel == answered.name {
				c.Quality = row.RTPQoS
			}
		}
	} else {
		c.CallerWait = secs(end.Sub(c.Start))
		if c.Kind == KindInboundGroup {
			c.Outcome = OutcomeLost
			rang := end.Sub(groupAt)
			if len(legs) == 0 {
				rang = 0 // the group was reached but no phone rang
			}
			c.Ring = secs(rang)
			switch {
			case len(legs) == 0:
				c.LostReason = LostNoAgent
			case r.GroupTime[c.Group] > 0 && rang >= time.Duration(r.GroupTime[c.Group]-5)*time.Second:
				c.LostReason = LostRangOut
			default:
				c.LostReason = LostCallerLeft
			}
		} else {
			c.Outcome = OutcomeUnanswered
		}
	}
	for _, ch := range legs {
		l := AgentLeg{Extension: ch.endpoint, Start: ch.start}
		stop := ch.end
		if !ch.ended {
			stop = end
		}
		switch {
		case ch == answered:
			l.Outcome, stop = LegAnswered, ch.answer
			l.Talk = *secs(ch.talk)
		case ch.cause == 17 && ch.source == ch.name:
			l.Outcome = LegDeclined
		case answered != nil:
			l.Outcome = LegColleagueAnswered
		case c.LostReason == LostRangOut:
			l.Outcome = LegRangOut
		default:
			l.Outcome = LegCallerHungUp
		}
		l.Ring = *secs(stop.Sub(ch.start))
		c.Legs = append(c.Legs, l)
	}
}

func outbound(c *Call, agent, trunk *channel, cdrs []CDRRow) {
	c.Kind, c.Agent, c.Trunk, c.DialStatus = KindOutbound, agent.endpoint, trunk.endpoint, agent.dialStatus
	for _, row := range cdrs {
		if row.Channel == agent.name && row.Dst != "" {
			c.Caller = row.Dst
			if row.DstChannel == trunk.name {
				c.Quality = row.RTPQoS
			}
		}
	}
	if trunk.answered {
		c.Outcome = OutcomeConnected
		c.Setup = secs(trunk.answer.Sub(agent.start))
		c.Talk = secs(agent.talk)
		return
	}
	c.Outcome = OutcomeNotConnected
	stop := c.End
	if agent.ended {
		stop = agent.end
	}
	c.Setup = secs(stop.Sub(agent.start))
}

// ParseInt is strconv.ParseInt for source columns that may be blank.
func ParseInt(s string) int64 { n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64); return n }
