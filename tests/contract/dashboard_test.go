package contract_test

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOptionalRoutesExplicitlyUnavailable(t *testing.T) {
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}})
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/dashboard/query?query=up", "/dashboard/query_range?query=up&start=2026-01-01T00:00:00Z&end=2026-01-02T00:00:00Z&stepSeconds=60", "/registration/status/01", "/registration/events?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", "/registration/online", "/recordings/a", "/live/calls", "/live/calls/stream"} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk"+path, nil))
		if w.Code != 503 || !strings.Contains(w.Body.String(), "FEATURE_UNAVAILABLE") {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
	}
}
