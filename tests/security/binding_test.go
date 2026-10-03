package security_test

import (
	"context"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"net/http/httptest"
	"testing"
)

type verifier struct {
	id  authclient.Identity
	err error
}

func (v verifier) Verify(context.Context, string) (authclient.Identity, error) { return v.id, v.err }

type checker struct {
	allow bool
	deny  string
	err   error
}

func (c checker) Has(_ context.Context, _, _, permission string) (bool, error) {
	return c.allow && c.deny != permission, c.err
}
func TestEveryRouteFailsClosed(t *testing.T) {
	man, e := asteriskmanifest.Manifest()
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		v      verifier
		c      checker
		status int
	}{{"absent", verifier{err: errors.New("missing")}, checker{allow: true}, 401}, {"revoked", verifier{id: authclient.Identity{UserID: "u", TenantID: "t"}, err: errors.New("revoked")}, checker{allow: true}, 401}, {"foreign", verifier{id: authclient.Identity{UserID: "u", TenantID: "foreign"}}, checker{allow: true}, 403}, {"permission", verifier{id: authclient.Identity{UserID: "u", TenantID: "t"}}, checker{}, 403}, {"auth outage", verifier{id: authclient.Identity{UserID: "u", TenantID: "t"}}, checker{err: errors.New("unavailable")}, 503}, {"revocation feed stale", verifier{err: authclient.ErrStale}, checker{allow: true}, 503}} {
		t.Run(tc.name, func(t *testing.T) {
			s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: tc.v, Checker: tc.c})
			if e != nil {
				t.Fatal(e)
			}
			for _, route := range man.Routes {
				req := httptest.NewRequest(route.Method, route.Path, nil)
				req.Header.Set("Authorization", "Bearer valid")
				req.Header.Set("X-Tenant-ID", "t")
				w := httptest.NewRecorder()
				s.Handler().ServeHTTP(w, req)
				if w.Code != tc.status {
					t.Fatalf("%s: want %d got %d %s", route.Path, tc.status, w.Code, w.Body.String())
				}
			}
		})
	}
}
func TestRecordingRequiresCallsToo(t *testing.T) {
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: verifier{id: authclient.Identity{TenantID: "t", UserID: "u"}}, Checker: checker{allow: true, deny: "calls:read"}})
	if e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("GET", "/api/asterisk/recordings/c", nil)
	req.Header.Set("Range", "bytes=0-1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatal("recording bypassed call permission")
	}
}
