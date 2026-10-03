package httpapi

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"net/http"
)

func (s *Server) calls(w http.ResponseWriter, r *http.Request) {
	from, to, e := interval(r)
	if e != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	page, size, sort, order, e := pagination(r, []string{"start", "src", "dst", "durationSeconds"}, "start")
	q := r.URL.Query()
	direction, disposition := q.Get("direction"), q.Get("disposition")
	if e != nil || (direction != "" && direction != "inbound" && direction != "outbound" && direction != "internal" && direction != "unknown") || (disposition != "" && disposition != "ANSWERED" && disposition != "BUSY" && disposition != "FAILED" && disposition != "NO ANSWER") {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	if s.Deps.History == nil {
		writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	v, e := s.Deps.History.List(r.Context(), tenant(r), cdr.Filter{From: from, To: to, Src: q.Get("src"), Dst: q.Get("dst"), Extension: q.Get("extension"), Direction: direction, Disposition: disposition, Page: page, PageSize: size, Sort: sort, Order: order})
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, v)
}
func (s *Server) callDetail(w http.ResponseWriter, r *http.Request) {
	if s.Deps.History == nil {
		writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	v, e := s.Deps.History.Detail(r.Context(), tenant(r), r.PathValue("linkedid"))
	if e != nil {
		failure(w, r, e)
		return
	}
	if store := s.registration(); store != nil {
		if ok, _ := s.Deps.Checker.Has(r.Context(), tenant(r), user(r), "registration:read"); ok {
			if enrichment, e := cdr.RegistrationAtCall(r.Context(), store, tenant(r), v.Summary); e == nil {
				v.Registration = enrichment
			}
		}
	}
	jsonResponse(w, v)
}
