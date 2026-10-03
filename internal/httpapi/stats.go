package httpapi

import (
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"net/http"
	"sort"
	"strings"
)

func (s *Server) report(w http.ResponseWriter, r *http.Request) (stats.Overview, bool) {
	from, to, e := interval(r)
	b, be := bucket(r)
	if e != nil || be != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return stats.Overview{}, false
	}
	if s.Deps.Reports == nil {
		writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return stats.Overview{}, false
	}
	v, e := s.Deps.Reports.Overview(r.Context(), tenant(r), from, to, b)
	if e != nil {
		failure(w, r, e)
		return v, false
	}
	return v, true
}
func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	v, ok := s.report(w, r)
	if ok {
		jsonResponse(w, v)
	}
}
func (s *Server) extensions(w http.ResponseWriter, r *http.Request) {
	page, size, key, order, e := pagination(r, []string{"extension", "total", "talkSeconds"}, "total")
	if e != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, ok := s.report(w, r)
	if !ok {
		return
	}
	names := map[string]string{}
	entries, _ := s.Deps.Reports.Directory(r.Context(), tenant(r))
	for _, d := range entries {
		names[d.Extension] = d.Name
	}
	items := []*stats.Extension{}
	for id, item := range v.Extensions {
		if filter := r.URL.Query().Get("extension"); filter != "" && id != filter {
			continue
		}
		item.Name = names[id]
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		cmp := strings.Compare(a.Extension, b.Extension)
		switch key {
		case "total":
			if a.Total != b.Total {
				if a.Total < b.Total {
					cmp = -1
				} else {
					cmp = 1
				}
			}
		case "talkSeconds":
			if a.TalkSeconds != b.TalkSeconds {
				if a.TalkSeconds < b.TalkSeconds {
					cmp = -1
				} else {
					cmp = 1
				}
			}
		}
		if order == "desc" {
			return cmp > 0
		}
		return cmp < 0
	})
	total := len(items)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	jsonResponse(w, map[string]any{"timezone": v.Timezone, "items": items[start:end], "total": total, "page": page, "page_size": size, "sort": key, "order": order})
}
func (s *Server) extension(w http.ResponseWriter, r *http.Request) {
	v, ok := s.report(w, r)
	if !ok {
		return
	}
	item := v.Extensions[r.PathValue("extension")]
	if item == nil {
		item = &stats.Extension{Timezone: v.Timezone, Extension: r.PathValue("extension"), Series: []stats.Bucket{}}
	}
	entries, _ := s.Deps.Reports.Directory(r.Context(), tenant(r))
	for _, e := range entries {
		if e.Extension == item.Extension {
			item.Name = e.Name
		}
	}
	jsonResponse(w, item)
}
func (s *Server) ringgroup(w http.ResponseWriter, r *http.Request) {
	from, to, e := interval(r)
	if e != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	if s.Deps.Reports == nil {
		writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
		return
	}
	v, e := s.Deps.Reports.Ringgroup(r.Context(), tenant(r), r.PathValue("ringGroup"), from, to)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, v)
}
