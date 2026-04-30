package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// DirectoryHandler exposes a tiny read-only endpoint that returns the
// FreePBX extension directory (extension number → display name). Used
// by the dashboard to enrich the per-extension Prometheus metrics with
// operator-recognisable names.
//
// Not modelled in proto / gRPC because:
//   - It's a flat list with no pagination, filtering, or auth scoping.
//   - The data evolves with FreePBX schema versions; gRPC's strict
//     schema would force lockstep proto changes.
//   - The frontend already needs a JSON shape — saves a marshalling hop.
type DirectoryHandler struct {
	log     *log.Helper
	repo    *data.StatsRepo
	timeout time.Duration
}

func NewDirectoryHandler(l *log.Helper, repo *data.StatsRepo) *DirectoryHandler {
	return &DirectoryHandler{
		log:     l,
		repo:    repo,
		timeout: 5 * time.Second,
	}
}

type directoryEntryJSON struct {
	Extension   string `json:"extension"`
	DisplayName string `json:"displayName"`
}

type directoryResponseJSON struct {
	Entries []directoryEntryJSON `json:"entries"`
}

// ServeHTTP handles GET /directory/extensions.
func (h *DirectoryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.repo == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "DB_UNAVAILABLE",
			"directory not available — config db not opened")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	entries, err := h.repo.ListExtensionDirectory(ctx)
	if err != nil {
		h.log.Warnf("list extension directory: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "MYSQL_UNAVAILABLE", err.Error())
		return
	}

	out := directoryResponseJSON{Entries: make([]directoryEntryJSON, 0, len(entries))}
	for _, e := range entries {
		out.Entries = append(out.Entries, directoryEntryJSON{
			Extension:   e.Extension,
			DisplayName: e.DisplayName,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	// Short cache so a busy dashboard reload doesn't hammer MySQL — the
	// directory changes at human-edit speed, not at scrape speed.
	w.Header().Set("Cache-Control", "private, max-age=60")
	_ = json.NewEncoder(w).Encode(out)
}

func writeJSONError(w http.ResponseWriter, code int, reason, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":    code,
		"reason":  reason,
		"message": msg,
	})
}
