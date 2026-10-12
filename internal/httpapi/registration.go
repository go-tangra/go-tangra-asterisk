package httpapi

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"net/http"
	"sort"
	"time"
)

func (s *Server) registrationStatus(w http.ResponseWriter, r *http.Request) {
	store := s.registration()
	if store == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	when, e := at(r)
	if e != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, gaps, e := store.At(r.Context(), tenant(r), r.PathValue("extension"), when)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, map[string]any{"status": v[0], "gaps": gaps})
}
func (s *Server) registrationOnline(w http.ResponseWriter, r *http.Request) {
	store := s.registration()
	if store == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	when, e := at(r)
	if e != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, gaps, e := store.At(r.Context(), tenant(r), "", when)
	if e != nil {
		failure(w, r, e)
		return
	}
	online := []registration.Status{}
	for _, status := range v {
		if status.Registered {
			online = append(online, status)
		}
	}
	sort.Slice(online, func(i, j int) bool { return online[i].Extension < online[j].Extension })
	jsonResponse(w, map[string]any{"items": online, "gaps": gaps, "uncertain": len(gaps) > 0})
}
func (s *Server) registrationEvents(w http.ResponseWriter, r *http.Request) {
	store := s.registration()
	if store == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	from, to, e := interval(r)
	page, size, sort, order, pe := pagination(r, []string{"time"}, "time")
	if e != nil || pe != nil || order != "desc" {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, total, e := store.Events(r.Context(), tenant(r), r.URL.Query().Get("extension"), from, to, page, size)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, map[string]any{"items": v, "total": total, "page": page, "page_size": size, "sort": sort, "order": order})
}

// registrationTimeline answers when an extension was online during a period:
// its registration states as consecutive segments (at most 31 days, so the
// event bound holds for chatty phones).
func (s *Server) registrationTimeline(w http.ResponseWriter, r *http.Request) {
	store := s.registration()
	if store == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	from, to, e := interval(r)
	if e != nil || to.Sub(from) > 31*24*time.Hour {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, e := store.Timeline(r.Context(), tenant(r), r.PathValue("extension"), from, to)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, v)
}
