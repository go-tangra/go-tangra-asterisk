package httpapi

import "net/http"

func (s *Server) recording(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Recordings == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	s.Deps.Recordings.Serve(w, r, tenant(r), r.PathValue("linkedid"))
}
