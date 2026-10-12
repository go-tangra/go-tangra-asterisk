//go:build integration

package integration_test

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"testing"
	"time"
)

// Registration history pages report the full total, and later pages carry the
// remaining events in order.
func TestRegistrationEventsPaging(t *testing.T) {
	c, _, _ := fixture(t)
	if c.Binding.RegistrationDSN == "" {
		t.Skip("set ASTERISK_FIXTURE_REGISTRATION_DSN to run registration fixture tests")
	}
	ctx := context.Background()
	if e := registration.Bootstrap(ctx, c); e != nil {
		t.Fatal(e)
	}
	store, e := registration.Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	defer store.DB.Close()
	if _, e = store.DB.ExecContext(ctx, "DELETE FROM pjsip_registration_events"); e != nil {
		t.Fatal(e)
	}
	start := time.Now().UTC().Truncate(time.Second).Add(-time.Hour)
	for i := 0; i < 30; i++ {
		if e = store.Append(ctx, registration.Event{Endpoint: "01", Contact: "sip:a", Time: start.Add(time.Duration(i) * time.Second), Status: "Reachable"}); e != nil {
			t.Fatal(e)
		}
	}
	first, total, e := store.Events(ctx, "t", "01", start, start.Add(time.Hour), 1, 25)
	if e != nil || total != 30 || len(first) != 25 || !first[0].Time.Equal(start.Add(29*time.Second)) {
		t.Fatalf("page 1: total=%d len=%d %v", total, len(first), e)
	}
	second, total, e := store.Events(ctx, "t", "01", start, start.Add(time.Hour), 2, 25)
	if e != nil || total != 30 || len(second) != 5 || !second[4].Time.Equal(start) {
		t.Fatalf("page 2: total=%d len=%d %v", total, len(second), e)
	}
}

// The timeline starts from the state before the period (the last event of
// each contact) and splits at the period's events and expiries.
func TestRegistrationTimeline(t *testing.T) {
	c, _, _ := fixture(t)
	if c.Binding.RegistrationDSN == "" {
		t.Skip("set ASTERISK_FIXTURE_REGISTRATION_DSN to run registration fixture tests")
	}
	ctx := context.Background()
	if e := registration.Bootstrap(ctx, c); e != nil {
		t.Fatal(e)
	}
	store, e := registration.Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	defer store.DB.Close()
	for _, q := range []string{"DELETE FROM pjsip_registration_events", "DELETE FROM asterisk_observation_gaps"} {
		if _, e = store.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	from := time.Now().UTC().Truncate(time.Second).Add(-4 * time.Hour)
	for _, ev := range []registration.Event{
		{Endpoint: "01", Contact: "sip:a", Time: from.Add(-time.Hour), Status: "Created"},
		{Endpoint: "01", Contact: "sip:a", Time: from.Add(time.Hour), Status: "Removed"},
		{Endpoint: "01", Contact: "sip:a", Time: from.Add(2 * time.Hour), Status: "Created", Expire: from.Add(3 * time.Hour)},
		{Endpoint: "02", Contact: "sip:b", Time: from, Status: "Created"},
	} {
		if e = store.Append(ctx, ev); e != nil {
			t.Fatal(e)
		}
	}
	tl, e := store.Timeline(ctx, "t", "01", from, from.Add(4*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	want := []string{registration.Online, registration.Offline, registration.Online, registration.Offline}
	if len(tl.Segments) != len(want) {
		t.Fatalf("segments: %+v", tl.Segments)
	}
	for i, s := range tl.Segments {
		if s.State != want[i] || !s.Start.Equal(from.Add(time.Duration(i)*time.Hour)) {
			t.Fatalf("segment %d: %+v", i, s)
		}
	}
	if tl.Summary.Dropouts != 2 || tl.Summary.OnlineSeconds != 7200 {
		t.Fatalf("summary: %+v", tl.Summary)
	}
}
