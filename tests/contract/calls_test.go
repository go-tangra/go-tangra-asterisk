package contract_test

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"testing"
	"time"
)

func TestGroupingAnswerTiesAndPickup(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	legs := []cdr.Leg{{LinkedID: "a", UniqueID: "2", Start: start, Src: "1234", Dst: "600", Channel: "PJSIP/trunk-1", DstChannel: "PJSIP/002-1", Disposition: "BUSY", Duration: 5}, {LinkedID: "a", UniqueID: "1", Start: start, Src: "1234", Dst: "600", Channel: "PJSIP/trunk-2", DstChannel: "PJSIP/001-2", Disposition: "ANSWERED", Duration: 10, Talk: 7}}
	events := []cdr.Event{{UniqueID: "1", Type: "CHAN_START", Time: start}, {UniqueID: "1", Type: "ANSWER", Time: start.Add(3 * time.Second)}}
	c := cdr.Group(legs, events)
	if c.LinkedID != "a" || c.Disposition != "ANSWERED" || c.AnsweredExtension != "001" || c.Direction != "inbound" || c.Pickup == nil || *c.Pickup != 3 || c.LegCount != 2 {
		t.Fatalf("bad summary: %+v", c)
	}
	if legs[0].UniqueID != "2" {
		t.Fatal("group mutated input")
	}
}
func TestMissingCELAndUnknownChannel(t *testing.T) {
	c := cdr.Group([]cdr.Leg{{LinkedID: "b", Channel: "unknown", Disposition: "NO ANSWER"}}, nil)
	if c.Pickup != nil || c.Direction != "unknown" {
		t.Fatalf("fabricated data: %+v", c)
	}
	if cdr.ExtractExtension("Local/001@from-internal-1;1") != "001" {
		t.Fatal("leading zeros lost")
	}
}
