package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/support"
)

// Support is the support-team reporting feature (internal/support).
type Support interface {
	Report(ctx context.Context, tenant string, from, to time.Time) ([]support.Call, error)
	TeamMembers(ctx context.Context) []string
	CallbackWindow() time.Duration
	Ingested(ctx context.Context) (time.Time, error)
	RecordListen(ctx context.Context, tenant, user, linkedid string) error
	Listens(ctx context.Context, tenant string, from, to time.Time, linkedid string) ([]support.Listen, error)
}

func (s *Server) supportService() Support {
	if s.Deps.Support == nil {
		return nil
	}
	return s.Deps.Support()
}

// supportRange bounds a support report: it is built in memory from every
// call of the period.
const supportRange = 92 * 24 * time.Hour

func (s *Server) supportPeriod(w http.ResponseWriter, r *http.Request) (Support, []support.Call, time.Time, time.Time, bool) {
	svc := s.supportService()
	if svc == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return nil, nil, time.Time{}, time.Time{}, false
	}
	from, to, e := interval(r)
	if e != nil || to.Sub(from) > supportRange {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return nil, nil, from, to, false
	}
	calls, e := svc.Report(r.Context(), tenant(r), from, to)
	if e != nil {
		failure(w, r, e)
		return nil, nil, from, to, false
	}
	return svc, calls, from, to, true
}

func (s *Server) supportSummary(w http.ResponseWriter, r *http.Request) {
	svc, calls, from, to, ok := s.supportPeriod(w, r)
	if !ok {
		return
	}
	var ingested *time.Time
	if t, e := svc.Ingested(r.Context()); e == nil {
		ingested = &t
	}
	jsonResponse(w, struct {
		Timezone string     `json:"timezone"`
		Team     []string   `json:"team"`
		Ingested *time.Time `json:"ingestedAt"`
		support.Summary
	}{s.Deps.Timezone, svc.TeamMembers(r.Context()), ingested, support.Summarize(calls, from, to, svc.CallbackWindow())})
}

func (s *Server) supportAgents(w http.ResponseWriter, r *http.Request) {
	svc, calls, from, to, ok := s.supportPeriod(w, r)
	if !ok {
		return
	}
	names := map[string]string{}
	if s.Deps.Reports != nil {
		entries, _ := s.Deps.Reports.Directory(r.Context(), tenant(r))
		for _, d := range entries {
			names[d.Extension] = d.Name
		}
	}
	jsonResponse(w, map[string]any{"timezone": s.Deps.Timezone, "items": support.Agents(calls, from, to, svc.CallbackWindow(), svc.TeamMembers(r.Context()), names)})
}

func (s *Server) supportLost(w http.ResponseWriter, r *http.Request) {
	page, size, _, _, pe := pagination(r, []string{"start"}, "start")
	if pe != nil {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	svc, calls, from, to, ok := s.supportPeriod(w, r)
	if !ok {
		return
	}
	items := support.LostCalls(calls, from, to, svc.CallbackWindow())
	total := len(items)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	jsonResponse(w, map[string]any{"items": append([]support.Lost{}, items[start:end]...), "total": total, "page": page, "page_size": size})
}

// supportCalls searches the classified calls of a period (support kinds
// only: answered, lost, outbound connected or not), newest first.
func (s *Server) supportCalls(w http.ResponseWriter, r *http.Request) {
	page, size, _, _, pe := pagination(r, []string{"start"}, "start")
	q := r.URL.Query()
	f := support.Filter{Agent: q.Get("agent"), Type: q.Get("type"), Caller: q.Get("caller"), BadQuality: q.Get("quality") == "poor", Recorded: q.Get("recorded") == "true"}
	minTalk, me := strconv.ParseInt(q.Get("min_talk"), 10, 64)
	if q.Get("min_talk") == "" {
		minTalk, me = 0, nil
	}
	f.MinTalk = minTalk
	switch {
	case pe != nil, me != nil, minTalk < 0,
		f.Type != "" && f.Type != support.TypeAnswered && f.Type != support.TypeLost && f.Type != support.TypeOutbound && f.Type != support.TypeNotConnected,
		len(f.Caller) > 64, len(f.Agent) > 32:
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	svc, calls, from, to, ok := s.supportPeriod(w, r)
	if !ok {
		return
	}
	rows := support.Search(calls, from, to, f)
	total := len(rows)
	start := min((page-1)*size, total)
	end := min(start+size, total)
	jsonResponse(w, map[string]any{"timezone": s.Deps.Timezone, "team": svc.TeamMembers(r.Context()), "names": s.names(r), "items": append([]support.Row{}, rows[start:end]...), "total": total, "page": page, "page_size": size})
}

// supportListens is the recording audit: who opened which recording when.
func (s *Server) supportListens(w http.ResponseWriter, r *http.Request) {
	svc := s.supportService()
	if svc == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	from, to, e := interval(r)
	linkedid := r.URL.Query().Get("linkedid")
	if e != nil || to.Sub(from) > supportRange || len(linkedid) > 64 {
		writeError(w, r, 400, "INVALID_ARGUMENT")
		return
	}
	v, e := svc.Listens(r.Context(), tenant(r), from, to, linkedid)
	if e != nil {
		failure(w, r, e)
		return
	}
	jsonResponse(w, map[string]any{"items": v})
}

func (s *Server) names(r *http.Request) map[string]string {
	names := map[string]string{}
	if s.Deps.Reports != nil {
		entries, _ := s.Deps.Reports.Directory(r.Context(), tenant(r))
		for _, d := range entries {
			names[d.Extension] = d.Name
		}
	}
	return names
}
