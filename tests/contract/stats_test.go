package contract_test

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"net/http/httptest"
	"testing"
	"time"
)

type validIdentity struct{}

func (validIdentity) Verify(context.Context, string) (authclient.Identity, error) {
	return authclient.Identity{UserID: "u", TenantID: "t"}, nil
}

type allow struct{}

func (allow) Has(context.Context, string, string, string) bool { return true }

type reports struct{}

func (reports) Overview(context.Context, string, time.Time, time.Time, string) (stats.Overview, error) {
	return stats.Overview{Total: 3, Answered: 1, Missed: 2, Series: []stats.Bucket{}, Extensions: map[string]*stats.Extension{"01": {Extension: "01", Total: 3}}}, nil
}
func (reports) Directory(context.Context, string) ([]stats.DirectoryEntry, error) {
	return []stats.DirectoryEntry{{Extension: "01", Name: "Operator"}}, nil
}
func (reports) Ringgroup(context.Context, string, string, time.Time, time.Time) (stats.Ringgroup, error) {
	return stats.Ringgroup{Total: 3, Answered: 1, NoAnswer: 1, Busy: 1}, nil
}
func TestStatsAndListBounds(t *testing.T) {
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}, Reports: reports{}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		url    string
		status int
	}{{"/stats/overview?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", 200}, {"/stats/extensions?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", 200}, {"/stats/extensions?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&page_size=201", 400}, {"/stats/overview?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z", 400}, {"/stats/overview?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z&bucket=invalid", 400}} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk"+tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.url, w.Code)
		}
	}
}
