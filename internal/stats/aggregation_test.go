package stats

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"testing"
	"time"
)

func TestLogicalExternalWorkload(t *testing.T) {
	calls := []cdr.Call{{LinkedID: "a", Direction: "inbound", Disposition: "ANSWERED", AnsweredExtension: "01", Talk: 10}, {LinkedID: "b", Direction: "internal", Disposition: "ANSWERED", OriginatingExtension: "01", AnsweredExtension: "02", Talk: 90}}
	r := Aggregate(calls, time.UTC, "day")
	if r.Total != 2 || r.Extensions["01"].WorkloadShare != 1 || r.Extensions["02"].WorkloadShare != 0 {
		t.Fatalf("bad aggregation: %+v", r)
	}
}
func TestDSTHourBuckets(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Sofia")
	a := time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)
	b := a.Add(time.Hour)
	if BucketStart(a, loc, "hour").Equal(BucketStart(b, loc, "hour")) {
		t.Fatal("repeated DST hour collapsed")
	}
	if Aggregate(nil, loc, "day").Total != 0 {
		t.Fatal("empty period")
	}
}
func TestExtensionAnsweredAndMissed(t *testing.T) {
	pickup := 4.0
	cases := []struct {
		name             string
		call             cdr.Call
		ext              string
		answered, missed int
		talk             int64
		pickupN          int
	}{
		{"internal caller", cdr.Call{Direction: "internal", Disposition: "ANSWERED", OriginatingExtension: "01", AnsweredExtension: "02", Talk: 30, Pickup: &pickup}, "01", 1, 0, 30, 0},
		{"internal callee", cdr.Call{Direction: "internal", Disposition: "ANSWERED", OriginatingExtension: "01", AnsweredExtension: "02", Talk: 30, Pickup: &pickup}, "02", 1, 0, 30, 1},
		{"internal unanswered caller", cdr.Call{Direction: "internal", Disposition: "NO ANSWER", OriginatingExtension: "01", Extensions: []string{"01", "02"}}, "01", 0, 1, 0, 0},
		{"internal unanswered callee", cdr.Call{Direction: "internal", Disposition: "NO ANSWER", OriginatingExtension: "01", Extensions: []string{"01", "02"}}, "02", 0, 1, 0, 0},
		{"ring group member that did not answer", cdr.Call{Direction: "inbound", Disposition: "ANSWERED", AnsweredExtension: "02", Extensions: []string{"02", "03"}, Talk: 5}, "03", 0, 1, 0, 0},
		{"outbound caller", cdr.Call{Direction: "outbound", Disposition: "ANSWERED", OriginatingExtension: "01", Talk: 7, Pickup: &pickup}, "01", 1, 0, 7, 1},
		{"inbound answered", cdr.Call{Direction: "inbound", Disposition: "ANSWERED", AnsweredExtension: "02", Talk: 9}, "02", 1, 0, 9, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.call.LinkedID = "x"
			e := Aggregate([]cdr.Call{tc.call}, time.UTC, "day").Extensions[tc.ext]
			if e == nil || e.Answered != tc.answered || e.Missed != tc.missed || e.TalkSeconds != tc.talk || e.pickupN != tc.pickupN {
				t.Fatalf("%+v", e)
			}
		})
	}
}
