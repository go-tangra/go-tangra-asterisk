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
