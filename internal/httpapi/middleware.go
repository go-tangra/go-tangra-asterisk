package httpapi

import (
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"net/http"
)

func (s *Server) protect(permission string, recording bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Deps.Verifier == nil {
			writeError(w, r, 401, "UNAUTHENTICATED")
			return
		}
		id, e := s.Deps.Verifier.Verify(r.Context(), authclient.BearerToken(r.Header.Get("Authorization")))
		if e != nil || id.UserID == "" || id.TenantID == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="tangra"`)
			writeError(w, r, 401, "UNAUTHENTICATED")
			return
		}
		if id.TenantID != s.Deps.Tenant || s.Deps.Checker == nil || !s.Deps.Checker.Has(r.Context(), id.TenantID, id.UserID, permission) || (recording && (!s.Deps.Checker.Has(r.Context(), id.TenantID, id.UserID, "calls:read") || (permission != "recordings:read" && !s.Deps.Checker.Has(r.Context(), id.TenantID, id.UserID, "recordings:read")))) {
			writeError(w, r, 403, "FORBIDDEN")
			return
		}
		next.ServeHTTP(w, r.WithContext(authclient.WithIdentity(r.Context(), id)))
	})
}
