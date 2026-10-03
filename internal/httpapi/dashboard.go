package httpapi

import (
	"net/http"
	"strconv"
	"time"
)

func (s *Server) query(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Dashboard == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	q := r.URL.Query()
	at := time.Now().UTC()
	if q.Has("time") {
		var e error
		at, e = time.Parse(time.RFC3339, q.Get("time"))
		if e != nil {
			writeError(w, r, 400, "INVALID_ARGUMENT")
			return
		}
	}
	v, e := s.Deps.Dashboard.Instant(r.Context(), tenant(r), q.Get("query"), at)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, map[string]any{"items": v})
}
func (s *Server) queryRange(w http.ResponseWriter, r *http.Request) {
	if s.Deps.Dashboard == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	q := r.URL.Query()
	start, e := time.Parse(time.RFC3339, q.Get("start"))
	end, ee := time.Parse(time.RFC3339, q.Get("end"))
	step, se := strconv.Atoi(q.Get("stepSeconds"))
	if e != nil || ee != nil || se != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, e := s.Deps.Dashboard.Range(r.Context(), tenant(r), q.Get("query"), start, end, step)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, map[string]any{"items": v})
}
