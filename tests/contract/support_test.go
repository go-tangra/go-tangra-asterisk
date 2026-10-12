package contract_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/support"
)

type supportFake struct{}

func (supportFake) Report(_ context.Context, _ string, from, _ time.Time) ([]support.Call, error) {
	ring, wait := int64(23), int64(41)
	return []support.Call{
		{LinkedID: "a", Start: from.Add(time.Hour), End: from.Add(time.Hour + time.Minute), Kind: support.KindInboundGroup, Group: "600", Outcome: support.OutcomeAnswered, Agent: "49", Ring: &ring, CallerWait: &wait,
			Legs: []support.AgentLeg{{Extension: "49", Outcome: support.LegAnswered, Ring: 23, Talk: 72}, {Extension: "41", Outcome: support.LegDeclined, Ring: 3}}},
		{LinkedID: "b", Start: from.Add(2 * time.Hour), End: from.Add(2*time.Hour + time.Minute), Kind: support.KindInboundGroup, Group: "600", Outcome: support.OutcomeLost, LostReason: support.LostNoAgent, Caller: "0888123456"},
		{LinkedID: "c", Start: from.Add(2*time.Hour + 30*time.Minute), Kind: support.KindOutbound, Outcome: support.OutcomeConnected, Agent: "41", Caller: "+359888123456"},
	}, nil
}
func (supportFake) TeamMembers(context.Context) []string        { return []string{"41", "49"} }
func (supportFake) CallbackWindow() time.Duration               { return 2 * time.Hour }
func (supportFake) Ingested(context.Context) (time.Time, error) { return time.Now(), nil }

func TestSupportRoutes(t *testing.T) {
	unavailable, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}})
	if e != nil {
		t.Fatal(e)
	}
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}, Support: func() httpapi.Support { return supportFake{} }})
	if e != nil {
		t.Fatal(e)
	}
	day := "?from=2026-10-08T00:00:00Z&to=2026-10-09T00:00:00Z"
	for _, tc := range []struct {
		server *httpapi.Server
		url    string
		status int
	}{
		{unavailable, "/support/summary" + day, 503},
		{s, "/support/summary" + day, 200},
		{s, "/support/agents" + day, 200},
		{s, "/support/lost" + day, 200},
		{s, "/support/lost" + day + "&page_size=201", 400},
		{s, "/support/summary?from=2026-01-01T00:00:00Z&to=2026-06-01T00:00:00Z", 400}, // over 92 days
		{s, "/support/summary?from=2026-10-09T00:00:00Z&to=2026-10-08T00:00:00Z", 400},
	} {
		w := httptest.NewRecorder()
		tc.server.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk"+tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d, want %d (%s)", tc.url, w.Code, tc.status, w.Body)
		}
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk/support/lost"+day, nil))
	var lost struct {
		Items []support.Lost `json:"items"`
	}
	if json.Unmarshal(w.Body.Bytes(), &lost) != nil || len(lost.Items) != 1 || lost.Items[0].CallbackBy != "41" {
		t.Fatalf("the lost call was called back by 41 (numbers differ in format only): %s", w.Body)
	}
}
