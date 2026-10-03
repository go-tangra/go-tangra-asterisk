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
