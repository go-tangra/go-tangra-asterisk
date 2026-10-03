package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/legacy"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/dashboard"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/recordings"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Verifier interface {
	Verify(context.Context, string) (authclient.Identity, error)
}
type Checker interface {
	Has(context.Context, string, string, string) bool
}
type History interface {
	Detail(context.Context, string, string) (cdr.Detail, error)
	List(context.Context, string, cdr.Filter) (cdr.List, error)
}
type Reports interface {
	Overview(context.Context, string, time.Time, time.Time, string) (stats.Overview, error)
	Directory(context.Context, string) ([]stats.DirectoryEntry, error)
	Ringgroup(context.Context, string, string, time.Time, time.Time) (stats.Ringgroup, error)
}
type Deps struct {
	Tenant            string
	Verifier          Verifier
	Checker           Checker
	History           History
	Reports           Reports
	Registry          *calls.Registry
	Registration      *registration.Repository
	Recordings        *recordings.Handler
	Dashboard         *dashboard.Client
	AMIEnabled        bool
	StreamLifetime    time.Duration
	Ready             func(context.Context) error
	Capabilities      func(context.Context) map[string]Capability
	RegistrationFresh func() bool
	Remote            fs.FS
	Stop              <-chan struct{}
}
type Server struct {
	Deps      Deps
	mux       *http.ServeMux
	patterns  map[string]bool
	validator routers.Router
}

func New(d Deps) (*Server, error) {
	s := &Server{Deps: d, mux: http.NewServeMux(), patterns: map[string]bool{}}
	if s.Deps.StreamLifetime <= 0 || s.Deps.StreamLifetime > 300*time.Second {
		s.Deps.StreamLifetime = 240 * time.Second
	}
	doc, e := asteriskmanifest.Load()
	if e != nil {
		return nil, e
	}
	s.validator, e = legacy.NewRouter(doc)
	if e != nil {
		return nil, e
	}
	man, e := asteriskmanifest.Manifest()
	if e != nil {
		return nil, e
	}
	handlers := map[string]http.HandlerFunc{"/capabilities": s.capabilities, "/calls": s.calls, "/calls/{linkedid}": s.callDetail, "/stats/overview": s.overview, "/stats/extensions": s.extensions, "/stats/extensions/{extension}": s.extension, "/stats/ringgroups/{ring_group}": s.ringgroup, "/directory/extensions": s.directory, "/registration/status/{extension}": s.registrationStatus, "/registration/events": s.registrationEvents, "/registration/online": s.registrationOnline, "/live/calls": s.liveSnapshot, "/live/calls/stream": s.liveStream, "/recordings/{linkedid}": s.recording, "/dashboard/query": s.query, "/dashboard/query_range": s.queryRange}
	for _, route := range man.Routes {
		short := strings.TrimPrefix(route.Path, asteriskmanifest.APIPrefix)
		h, ok := handlers[short]
		if !ok {
			return nil, errors.New("manifest handler drift")
		}
		pattern := route.Method + " " + route.Path
		s.patterns[pattern] = true
		s.mux.Handle(pattern, s.protect(route.Permission, short == "/recordings/{linkedid}", s.validate(h)))
	}
	if len(s.patterns) != len(handlers) {
		return nil, errors.New("handler manifest drift")
	}
	if d.Remote != nil {
		s.mux.Handle("GET /ui/", http.StripPrefix("/ui/", http.FileServerFS(d.Remote)))
	}
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if d.Ready == nil || d.Ready(r.Context()) != nil {
			writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
			return
		}
		jsonResponse(w, map[string]string{"status": "ready"})
	})
	return s, nil
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		s.mux.ServeHTTP(w, r)
	})
}
func (s *Server) Patterns() map[string]bool {
	out := map[string]bool{}
	for k, v := range s.patterns {
		out[k] = v
	}
	return out
}
func jsonResponse(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, r *http.Request, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	message := map[string]string{"INVALID_ARGUMENT": "Invalid request parameters", "UNAUTHENTICATED": "Valid platform session required", "FORBIDDEN": "Access denied", "NOT_FOUND": "Record unavailable", "FEATURE_UNAVAILABLE": "Feature unavailable", "DEPENDENCY_UNAVAILABLE": "Dependency unavailable"}[code]
	_ = json.NewEncoder(w).Encode(struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	}{code, message, r.Header.Get("X-Request-Id")})
}
func failure(w http.ResponseWriter, r *http.Request, e error) {
	switch {
	case errors.Is(e, cdr.ErrMissing):
		writeError(w, r, 404, "NOT_FOUND")
	case errors.Is(e, cdr.ErrBound) || errors.Is(e, dashboard.ErrInvalid):
		writeError(w, r, 400, "INVALID_ARGUMENT")
	default:
		writeError(w, r, 503, "DEPENDENCY_UNAVAILABLE")
	}
}
func tenant(r *http.Request) string { id, _ := authclient.FromContext(r.Context()); return id.TenantID }
func interval(r *http.Request) (time.Time, time.Time, error) {
	q := r.URL.Query()
	from, e := time.Parse(time.RFC3339, q.Get("from"))
	if e != nil {
		return from, time.Time{}, e
	}
	to, e := time.Parse(time.RFC3339, q.Get("to"))
	if e != nil || !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return from, to, errors.New("invalid interval")
	}
	return from.UTC(), to.UTC(), nil
}
func at(r *http.Request) (time.Time, error) {
	if r.URL.Query().Get("at") == "" {
		return time.Now().UTC(), nil
	}
	return time.Parse(time.RFC3339, r.URL.Query().Get("at"))
}
func pagination(r *http.Request, allow []string, def string) (page, size int, sort, order string, err error) {
	page, size = 1, 25
	q := r.URL.Query()
	if q.Has("page") {
		page, err = strconv.Atoi(q.Get("page"))
		if err != nil || page < 1 || page > 10000000 {
			return 0, 0, "", "", errors.New("invalid page")
		}
	}
	if q.Has("page_size") {
		size, err = strconv.Atoi(q.Get("page_size"))
		if err != nil || size < 1 || size > 200 {
			return 0, 0, "", "", errors.New("invalid page size")
		}
	}
	sort = q.Get("sort")
	if sort == "" {
		sort = def
	}
	valid := false
	for _, v := range allow {
		if v == sort {
			valid = true
		}
	}
	if !valid {
		return 0, 0, "", "", errors.New("invalid sort")
	}
	order = q.Get("order")
	if order == "" {
		order = "desc"
	}
	if order != "asc" && order != "desc" {
		err = errors.New("invalid order")
	}
	return
}
func bucket(r *http.Request) (string, error) {
	b := r.URL.Query().Get("bucket")
	if b == "" {
		b = "day"
	}
	if b != "hour" && b != "day" && b != "week" {
		return "", errors.New("invalid bucket")
	}
	return b, nil
}

func (s *Server) validate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route, params, e := s.validator.FindRoute(r)
		if e != nil {
			writeError(w, r, 400, "INVALID_ARGUMENT")
			return
		}
		input := &openapi3filter.RequestValidationInput{Request: r, PathParams: params, Route: route, Options: &openapi3filter.Options{AuthenticationFunc: openapi3filter.NoopAuthenticationFunc}}
		if openapi3filter.ValidateRequest(r.Context(), input) != nil {
			writeError(w, r, 400, "INVALID_ARGUMENT")
			return
		}
		next.ServeHTTP(w, r)
	})
}
