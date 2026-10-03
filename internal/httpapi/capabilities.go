package httpapi

import "net/http"

type Capability struct {
	Available bool `json:"available"`
	Fresh     bool `json:"fresh"`
}

func (s *Server) capabilities(w http.ResponseWriter, r *http.Request) {
	out := map[string]Capability{}
	if s.Deps.Capabilities != nil {
		out = s.Deps.Capabilities(r.Context())
	}
	captureFresh := false
	if s.Deps.RegistrationFresh != nil {
		captureFresh = s.Deps.RegistrationFresh()
	}
	out["registration"] = Capability{Available: s.registration() != nil, Fresh: captureFresh}
	out["live"] = Capability{Available: s.Deps.AMIEnabled, Fresh: s.Deps.Registry != nil && s.Deps.Registry.Snapshot().Fresh}
	recordings := s.recordings() != nil
	out["recordings"] = Capability{Available: recordings, Fresh: recordings}
	out["monitoring"] = Capability{Available: s.Deps.Dashboard != nil, Fresh: s.Deps.Dashboard != nil}
	jsonResponse(w, out)
}
