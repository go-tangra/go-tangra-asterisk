package support

import (
	"testing"
	"time"
)

func TestQualityReport(t *testing.T) {
	good := "lp=0;rxcount=500;rlp=0;txcount=500;rxjitter=0.002;txjitter=0.004;rtt=0.020;rxmes=90;txmes=88"
	lossy := "lp=40;rxcount=500;rlp=0;txcount=500;rxjitter=0.002;txjitter=0.001;rtt=0.020;rxmes=90;txmes=90"
	from := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	calls := []Call{
		{LinkedID: "a", Start: from.Add(time.Hour), Kind: KindInboundGroup, Outcome: OutcomeAnswered, Agent: "49", Trunk: "ITD", Quality: good, QualitySide: QualityTrunk},
		{LinkedID: "b", Start: from.Add(2 * time.Hour), Kind: KindInboundGroup, Outcome: OutcomeAnswered, Agent: "41", Trunk: "ITD", Quality: lossy, QualitySide: QualityTrunk},
		{LinkedID: "c", Start: from.Add(3 * time.Hour), Kind: KindOutbound, Outcome: OutcomeConnected, Agent: "45", Trunk: "ITD", Quality: lossy, QualitySide: QualityPhone},
		{LinkedID: "d", Start: from.Add(4 * time.Hour), Kind: KindOutbound, Outcome: OutcomeConnected, Agent: "45", Trunk: "ITD", Quality: good, QualitySide: QualityPhone},
		{LinkedID: "e", Start: from.Add(5 * time.Hour), Kind: KindOutbound, Outcome: OutcomeConnected, Agent: "49", Trunk: "ITD", Quality: good, QualitySide: QualityPhone},
		{LinkedID: "f", Start: from.Add(-time.Hour), Kind: KindOutbound, Outcome: OutcomeConnected, Agent: "49", Quality: lossy, QualitySide: QualityPhone}, // before the period
		{LinkedID: "g", Start: from.Add(time.Hour), Kind: KindInboundGroup, Outcome: OutcomeLost},                                                           // not measured
	}
	device := func(ext string, _ time.Time) string {
		if ext == "45" {
			return "Yealink T46U"
		}
		return ""
	}
	r := Quality(calls, from, from.Add(24*time.Hour), device)
	if r.Trunk.Calls != 2 || r.Trunk.Poor != 1 || r.Phone.Calls != 3 || r.Phone.Poor != 1 {
		t.Fatalf("sides: %+v %+v", r.Trunk, r.Phone)
	}
	if *r.Trunk.Jitter.P90 != 4 || *r.Trunk.Loss.P90 != 7.41 {
		t.Fatalf("worst direction, rounded: jitter %v loss %v", *r.Trunk.Jitter.P90, *r.Trunk.Loss.P90)
	}
	if r.ByAgent[0].Key != "45" || r.ByAgent[0].Poor != 1 || r.ByDevice[0].Key != "Yealink T46U" || r.ByDevice[1].Key != "Unknown" {
		t.Fatalf("worst share first: agents %+v devices %+v", r.ByAgent, r.ByDevice)
	}
	if len(r.Poor) != 2 || r.Poor[0].LinkedID != "c" || r.Poor[0].Device != "Yealink T46U" || r.Poor[1].Side != QualityTrunk {
		t.Fatalf("poor calls newest first: %+v", r.Poor)
	}
}
