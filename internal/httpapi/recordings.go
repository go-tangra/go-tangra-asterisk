package httpapi

import "net/http"

func (s *Server) recording(w http.ResponseWriter, r *http.Request) {
	h := s.recordings()
	if h == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	h.Serve(w, r, tenant(r), r.PathValue("linkedid"))
}
