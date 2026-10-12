package httpapi

import (
	"context"
	"net/http"

	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
)

func (s *Server) recording(w http.ResponseWriter, r *http.Request) {
	h := s.recordings()
	if h == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	sw := &statusWriter{ResponseWriter: w}
	h.Serve(sw, r, tenant(r), r.PathValue("linkedid"))
	// Audit: who listened. Playback issues several range requests; the store
	// counts them once. A failed audit write does not stop playback.
	if svc := s.supportService(); svc != nil && (sw.status == 200 || sw.status == 206) {
		id, _ := authclient.FromContext(r.Context())
		if err := svc.RecordListen(context.WithoutCancel(r.Context()), tenant(r), id.UserID, r.PathValue("linkedid")); err != nil && s.Deps.Log != nil {
			s.Deps.Log.Warn("recording listen not recorded", "error", err)
		}
	}
}

// statusWriter remembers the status a handler wrote.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.ResponseWriter.Write(b)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
