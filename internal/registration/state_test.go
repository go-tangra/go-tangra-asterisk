package registration

import (
	"testing"
	"time"
)

func TestMultipleContactsExpiryAndGap(t *testing.T) {
	at := time.Now().UTC()
	events := []Event{{Time: at, Endpoint: "01", Contact: "sip:a", Status: "Created", Expire: at.Add(time.Hour)}, {Time: at, Endpoint: "01", Contact: "sip:b", Status: "Reachable", Expire: at.Add(2 * time.Hour)}, {Time: at.Add(time.Minute), Endpoint: "01", Contact: "sip:a", Status: "Removed"}}
	s := Evaluate("01", at.Add(90*time.Minute), events, nil)
	if !s.Registered || s.Certainty != "observed" {
		t.Fatalf("bad multi-contact status: %+v", s)
	}
	s = Evaluate("01", at.Add(3*time.Hour), events, nil)
	if s.Registered {
		t.Fatal("expired registration")
	}
	s = Evaluate("01", at.Add(time.Minute), events, []Gap{{Start: at, End: at.Add(time.Hour)}})
	if s.Certainty != "uncertain" {
		t.Fatal("gap hidden")
	}
	if Evaluate("unknown", at, events, nil).Certainty != "unknown" {
		t.Fatal("invented observation")
	}
}
