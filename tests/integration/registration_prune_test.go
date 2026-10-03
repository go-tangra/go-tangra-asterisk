//go:build integration

package integration_test

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"testing"
	"time"
)

// Retention removes only events older than the cut-off.
func TestRegistrationPrune(t *testing.T) {
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
	now := time.Now().UTC().Truncate(time.Second)
	for _, at := range []time.Time{now.AddDate(0, 0, -500), now.AddDate(0, 0, -401), now.AddDate(0, 0, -10)} {
		if e = store.Append(ctx, registration.Event{Endpoint: "01", Contact: "sip:a", Time: at, Status: "Reachable"}); e != nil {
			t.Fatal(e)
		}
	}
	n, e := store.Prune(ctx, now.AddDate(0, 0, -400))
	if e != nil || n != 2 {
		t.Fatalf("pruned %d, %v", n, e)
	}
	var left int
	if e = store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM pjsip_registration_events").Scan(&left); e != nil || left != 1 {
		t.Fatalf("left %d, %v", left, e)
	}
}
