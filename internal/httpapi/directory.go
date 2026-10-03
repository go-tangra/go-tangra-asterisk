package httpapi

import "net/http"

func (s *Server) directory(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Reports == nil {
		writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	v, e := s.Deps.Reports.Directory(r.Context(), tenant(r))
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, map[string]any{"items": v})
}
