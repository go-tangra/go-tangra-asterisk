package support

import (
	"testing"
	"time"
)

func TestGrade(t *testing.T) {
	for raw, want := range map[string]string{
		"": "",
		"ssrc=1;themssrc=2;lp=0;rxjitter=0.002;rxcount=500;txjitter=0.001;txcount=500;rlp=0;rtt=0.020;rxmes=90;txmes=90": "good",
		"lp=40;rxcount=500;rlp=0;txcount=500;rxjitter=0.002;txjitter=0.001;rtt=0.020;rxmes=90;txmes=90":                  "poor", // 7.4 % loss
		"lp=0;rxcount=500;rlp=0;txcount=500;rxjitter=0.045;txjitter=0.001;rtt=0.020;rxmes=90;txmes=90":                   "poor", // 45 ms jitter
		"lp=0;rxcount=500;rlp=0;txcount=500;rxjitter=0.002;txjitter=0.001;rtt=0.020;rxmes=75;txmes=90":                   "fair",
	} {
		if got := Grade(raw); got != want {
			t.Errorf("%q: %q, want %q", raw, got, want)
		}
	}
}

func TestSearch(t *testing.T) {
	d := func(s int64) *int64 { return &s }
	from := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	calls := []Call{
		{LinkedID: "a", Start: from.Add(time.Hour), Kind: KindInboundGroup, Outcome: OutcomeAnswered, Agent: "49", Caller: "0888123456", Talk: d(400), Recorded: true, Legs: []AgentLeg{{Extension: "49"}, {Extension: "41"}}},
		{LinkedID: "b", Start: from.Add(2 * time.Hour), Kind: KindInboundGroup, Outcome: OutcomeLost, LostReason: LostCallerLeft, Caller: "0899000000", Legs: []AgentLeg{{Extension: "41"}}},
		{LinkedID: "c", Start: from.Add(3 * time.Hour), Kind: KindOutbound, Outcome: OutcomeConnected, Agent: "41", Caller: "+359888123456", Talk: d(30)},
		{LinkedID: "d", Start: from.Add(4 * time.Hour), Kind: KindInternal, Agent: "41"},
		{LinkedID: "e", Start: from.Add(-time.Hour), Kind: KindOutbound, Outcome: OutcomeNotConnected, Agent: "41"},
	}
	to := from.Add(24 * time.Hour)
	ids := func(f Filter) (out string) {
		for _, r := range Search(calls, from, to, f) {
			out += r.LinkedID
		}
		return
	}
	for name, tc := range map[string]struct {
		f    Filter
		want string
	}{
		"all support calls, newest first": {Filter{}, "cba"},
		"agent took, dialled or rang":     {Filter{Agent: "41"}, "cba"},
		"agent 49":                        {Filter{Agent: "49"}, "a"},
		"lost only":                       {Filter{Type: TypeLost}, "b"},
		"caller digits in any format":     {Filter{Caller: "888 123"}, "ca"},
		"at least five minutes":           {Filter{MinTalk: 300}, "a"},
		"recorded":                        {Filter{Recorded: true}, "a"},
	} {
		if got := ids(tc.f); got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}
