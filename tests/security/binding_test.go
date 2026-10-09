package security_test

import (
	"context"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"log/slog"
	"net/http/httptest"
	"strings"
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

// Refusals are logged with a reason and the verifier's detail, once a minute
// per reason; the bearer token never reaches the log.
func TestRefusalsAreLogged(t *testing.T) {
	man, e := asteriskmanifest.Manifest()
	if e != nil || len(man.Routes) == 0 {
		t.Fatal(e)
	}
	route := man.Routes[0]
	call := func(v verifier, bearer string) (int, string) {
		var buf strings.Builder
		s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: v, Checker: checker{allow: true}, Log: slog.New(slog.NewJSONHandler(&buf, nil))})
		if e != nil {
			t.Fatal(e)
		}
		code := 0
		for i := 0; i < 3; i++ {
			req := httptest.NewRequest(route.Method, route.Path, nil)
			if bearer != "" {
				req.Header.Set("Authorization", "Bearer "+bearer)
			}
			w := httptest.NewRecorder()
			s.Handler().ServeHTTP(w, req)
			code = w.Code
		}
		return code, buf.String()
	}
	const secret = "eyJhbGciOiJFZERTQSJ9.secret-token.sig"
	for _, tc := range []struct {
		name, bearer, reason, detail string
		v                            verifier
		status                       int
	}{
		{"wrong issuer", secret, "token_rejected", "token has invalid issuer", verifier{err: errors.New("authclient: unauthenticated\ntoken: token has invalid claims: token has invalid issuer")}, 401},
		{"no token", "", "no_bearer_token", "", verifier{err: authclient.ErrUnauthenticated}, 401},
		{"tenant not bound", secret, "tenant_not_bound", "token tenant other, module bound to t", verifier{id: authclient.Identity{UserID: "u", TenantID: "other"}}, 403},
		{"feed stale", secret, "revocation_feed_stale", "stale", verifier{err: authclient.ErrStale}, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, logged := call(tc.v, tc.bearer)
			if got != tc.status {
				t.Fatalf("status %d, want %d", got, tc.status)
			}
			if n := strings.Count(logged, `"reason":"`+tc.reason+`"`); n != 1 {
				t.Fatalf("reason %s logged %d times (throttled to 1):\n%s", tc.reason, n, logged)
			}
			if !strings.Contains(logged, tc.detail) {
				t.Fatalf("detail %q missing:\n%s", tc.detail, logged)
			}
			if strings.Contains(logged, "secret-token") {
				t.Fatal("the token must never be logged")
			}
		})
	}
}
