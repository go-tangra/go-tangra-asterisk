package support

import (
	"math"
	"testing"
)

func TestWorkSharesCountCallsAndTalk(t *testing.T) {
	long := &AgentStat{Extension: "41", Member: true} // one two-hour call
	long.Inbound.Answered, long.Inbound.Talk = 1, 7200
	busy := &AgentStat{Extension: "49", Member: true} // 29 short calls, most of them outbound
	busy.Inbound.Answered, busy.Inbound.Talk = 4, 600
	busy.Inbound.DirectAnswered, busy.Inbound.DirectTalk = 5, 600
	busy.Outbound.Connected, busy.Outbound.Talk = 20, 2400
	other := &AgentStat{Extension: "201"} // not in the team: measured, not counted
	other.Outbound.Connected, other.Outbound.Talk = 30, 9000
	workShares([]*AgentStat{long, busy, other})
	for _, c := range []struct {
		a    *AgentStat
		want float64
	}{{long, (1.0/30 + 7200.0/10800) / 2}, {busy, (29.0/30 + 3600.0/10800) / 2}, {other, (30.0/30 + 9000.0/10800) / 2}} {
		if math.Abs(c.a.Share-c.want) > 1e-9 {
			t.Errorf("%s: share %v, want %v", c.a.Extension, c.a.Share, c.want)
		}
	}
	if math.Abs(long.Share+busy.Share-1) > 1e-9 {
		t.Errorf("members' shares add up to %v", long.Share+busy.Share)
	}
}

func TestWorkSharesWithoutTalk(t *testing.T) {
	a := &AgentStat{Member: true}
	a.Outbound.Connected = 1
	b := &AgentStat{Member: true}
	workShares([]*AgentStat{a, b, {}})
	if a.Share != 1 || b.Share != 0 {
		t.Fatalf("shares %v %v", a.Share, b.Share)
	}
	workShares([]*AgentStat{{Member: true}}) // an idle team: no division by zero
}
