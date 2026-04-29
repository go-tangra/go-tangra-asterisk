package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/go-tangra/go-tangra-asterisk/internal/calls"
)

// CallStreamHandler bridges the calls.Registry to a Server-Sent Events
// HTTP response. Each subscriber gets its own goroutine; the registry
// drops slow consumers automatically.
//
// The endpoint is exposed on the asterisk module's HTTP server and
// reached from the browser via the admin gateway's /modules/asterisk/
// reverse proxy. Go's httputil.ReverseProxy auto-detects
// text/event-stream and switches to streaming-flush mode (Go 1.19+),
// so no proxy-side change is required.
//
// IMPORTANT: implements http.Handler directly (not the Kratos Context
// signature) so it can be registered via srv.HandlePrefix, which
// bypasses Kratos's middleware chain. The Kratos HTTP server has a
// default 1-second request timeout that would otherwise kill the
// stream before the first keepalive fires. Auth is still enforced
// upstream by admin-service before the gateway proxies the request.
type CallStreamHandler struct {
	log      *log.Helper
	registry *calls.Registry
}

func NewCallStreamHandler(l *log.Helper, registry *calls.Registry) *CallStreamHandler {
	return &CallStreamHandler{log: l, registry: registry}
}

// ServeHTTP implements http.Handler.
func (h *CallStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.registry == nil {
		http.Error(w, `{"code":503,"reason":"AMI_DISABLED","message":"live call stream requires AMI to be configured"}`, http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"code":500,"reason":"STREAMING_UNSUPPORTED","message":"response writer does not support streaming"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, no-transform")
	w.Header().Set("Connection", "keep-alive")
	// X-Accel-Buffering disables nginx response buffering for this
	// stream; harmless if no nginx is in front.
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Initial snapshot frame so the client doesn't need a separate REST
	// round-trip before the first event arrives.
	snap := h.registry.Snapshot()
	if err := writeEvent(w, "snapshot", snap); err != nil {
		return // client gone before first write
	}
	flusher.Flush()

	id, ch := h.registry.Subscribe()
	defer h.registry.Unsubscribe(id)

	// Periodic comment as a keepalive — many reverse proxies close
	// idle text/event-stream connections after 30–60s of silence.
	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				// Subscriber dropped (slow consumer). Tear down so the
				// browser EventSource reconnects fresh.
				return
			}
			if err := writeEvent(w, string(ev.Type), ev); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeEvent serialises one SSE frame. The Event: line carries the
// event type so the browser can subscribe to specific types via
// EventSource.addEventListener('call.started', ...).
func writeEvent(w http.ResponseWriter, eventType string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, body); err != nil {
		return err
	}
	return nil
}
