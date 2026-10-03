package httpapi

import (
	"encoding/json"
	"fmt"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"net/http"
	"time"
)

func user(r *http.Request) string { id, _ := authclient.FromContext(r.Context()); return id.UserID }
func (s *Server) liveSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.Deps.AMIEnabled || s.Deps.Registry == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	jsonResponse(w, s.Deps.Registry.Snapshot())
}
func (s *Server) liveStream(w http.ResponseWriter, r *http.Request) {
	if !s.Deps.AMIEnabled || s.Deps.Registry == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	ctl := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	send := func(kind string, v any) bool {
		_ = ctl.SetWriteDeadline(time.Now().Add(10 * time.Second))
		raw, e := json.Marshal(v)
		if e != nil {
			return false
		}
		if _, e = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, raw); e != nil {
			return false
		}
		return ctl.Flush() == nil
	}
	snapshot, ch, cancel := s.Deps.Registry.Subscribe()
	defer cancel()
	if !send("snapshot", snapshot) {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	timer := time.NewTimer(s.Deps.StreamLifetime)
	defer timer.Stop()
	for {
		select {
		case <-s.Deps.Stop:
			return
		case <-r.Context().Done():
			return
		case <-timer.C:
			return
		case update, ok := <-ch:
			if !ok {
				return
			}
			if !send(update.Type, update) {
				return
			}
		case <-heartbeat.C:
			id, e := s.Deps.Verifier.Verify(r.Context(), authclient.BearerToken(r.Header.Get("Authorization")))
			if e != nil || id.TenantID != tenant(r) || id.UserID != user(r) {
				return
			}
			if ok, e := s.Deps.Checker.Has(r.Context(), id.TenantID, id.UserID, "live:read"); e != nil || !ok {
				return
			}
			_ = ctl.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, e = fmt.Fprint(w, ": heartbeat\n\n"); e != nil || ctl.Flush() != nil {
				return
			}
		}
	}
}
