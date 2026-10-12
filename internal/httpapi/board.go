package httpapi

import (
	"net/http"
)

// liveBoard is what every agent is doing now and who is waiting: the support
// team (when the support reports run) plus anyone else on a call.
func (s *Server) liveBoard(w http.ResponseWriter, r *http.Request) {
	if !s.Deps.AMIEnabled || s.Deps.Registry == nil || s.Deps.Board == nil {
		writeError(w, r, 503, "FEATURE_UNAVAILABLE")
		return
	}
	var team []string
	if s.Deps.Support != nil {
		if svc := s.Deps.Support(); svc != nil {
			team = svc.TeamMembers(r.Context())
		}
	}
	names := map[string]string{}
	if s.Deps.Reports != nil {
		entries, _ := s.Deps.Reports.Directory(r.Context(), tenant(r))
		for _, d := range entries {
			names[d.Extension] = d.Name
		}
	}
	jsonResponse(w, s.Deps.Board.View(s.Deps.Registry.Snapshot(), team, names))
}
