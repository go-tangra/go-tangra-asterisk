//go:build integration

package integration_test

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"testing"
)

// RestartGap opens a gap only when none is open.
func TestRestartGapKeepsOneOpenGap(t *testing.T) {
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
	if _, e = store.DB.ExecContext(ctx, "DELETE FROM asterisk_observation_gaps"); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if e = store.RestartGap(ctx); e != nil {
			t.Fatal(e)
		}
	}
	var gaps int
	if e = store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM asterisk_observation_gaps").Scan(&gaps); e != nil || gaps != 1 {
		t.Fatalf("gaps=%d %v", gaps, e)
	}
}
