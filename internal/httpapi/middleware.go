package httpapi

import (
	"errors"
	"fmt"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"net/http"
)

func (s *Server) protect(permission string, recording bool, next http.Handler) http.Handler {
	required := []string{permission}
	if recording {
		required = append(required, "calls:read")
		if permission != "recordings:read" {
			required = append(required, "recordings:read")
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Deps.Verifier == nil {
			writeError(w, r, 401, "UNAUTHENTICATED")
			return
		}
		id, e := s.Deps.Verifier.Verify(r.Context(), authclient.BearerToken(r.Header.Get("Authorization")))
		if errors.Is(e, authclient.ErrStale) {
			// The revocation feed is unreachable: verification is unavailable,
			// the session is not known to be invalid.
			s.refusals.refused("revocation_feed_stale", e)
			writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		if e != nil || id.UserID == "" || id.TenantID == "" {
			switch {
			case authclient.BearerToken(r.Header.Get("Authorization")) == "":
				s.refusals.refused("no_bearer_token", nil)
			case e != nil:
				s.refusals.refused("token_rejected", e)
			default:
				s.refusals.refused("token_without_user_or_tenant", nil)
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="tangra"`)
			writeError(w, r, 401, "UNAUTHENTICATED")
			return
		}
		if id.TenantID != s.Deps.Tenant || s.Deps.Checker == nil {
			if id.TenantID != s.Deps.Tenant {
				// The module serves one tenant (binding.tenant_id); tenant ids are not secret.
				s.refusals.refused("tenant_not_bound", fmt.Errorf("token tenant %s, module bound to %s", id.TenantID, s.Deps.Tenant))
			}
			writeError(w, r, 403, "FORBIDDEN")
			return
		}
		for _, p := range required {
			ok, e := s.Deps.Checker.Has(r.Context(), id.TenantID, id.UserID, p)
			if e != nil {
				writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
				return
			}
			if !ok {
				writeError(w, r, 403, "FORBIDDEN")
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(authclient.WithIdentity(r.Context(), id)))
	})
}
