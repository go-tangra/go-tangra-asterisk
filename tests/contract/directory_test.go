package contract_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
)

type validIdentity struct{}

func (validIdentity) Verify(context.Context, string) (authclient.Identity, error) {
	return authclient.Identity{UserID: "u", TenantID: "t"}, nil
}

type allow struct{}

func (allow) Has(context.Context, string, string, string) (bool, error) { return true, nil }

type reports struct{}

func (reports) Directory(context.Context, string) ([]stats.DirectoryEntry, error) {
	return []stats.DirectoryEntry{{Extension: "01", Name: "Operator"}}, nil
}

// The extension directory stays; the v3-era statistics (overview,
// extensions, ring groups) are gone in favour of the support reports.
func TestDirectoryAndRetiredStats(t *testing.T) {
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}, Reports: reports{}})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		url    string
		status int
	}{{"/directory/extensions", 200}, {"/stats/overview?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", 404}, {"/stats/extensions", 404}, {"/stats/extensions/01", 404}, {"/stats/ringgroups/600", 404}} {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk"+tc.url, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: %d", tc.url, w.Code)
		}
		if tc.status == 200 && !strings.Contains(w.Body.String(), "Operator") {
			t.Fatalf("%s: %s", tc.url, w.Body.String())
		}
	}
}
