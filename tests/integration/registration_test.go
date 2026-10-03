package integration_test

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"testing"
	"time"
)

func TestHistoricalOutageRemainsUncertain(t *testing.T) {
	now := time.Now().UTC()
	events := []registration.Event{{ID: 1, Endpoint: "01", Contact: "sip:a", Time: now, Status: "Reachable", Expire: now.Add(time.Hour)}, {ID: 2, Endpoint: "01", Contact: "sip:a", Time: now.Add(10 * time.Minute), Status: "Removed"}}
	gaps := []registration.Gap{{Start: now.Add(time.Minute), End: now.Add(10 * time.Minute)}}
	past := registration.Evaluate("01", now.Add(5*time.Minute), events, gaps)
	current := registration.Evaluate("01", now.Add(11*time.Minute), events, gaps)
	if past.Certainty != "uncertain" || current.Registered || current.Certainty != "observed" {
		t.Fatalf("incorrect history %+v %+v", past, current)
	}
}
