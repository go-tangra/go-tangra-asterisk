package recordings

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Lookup interface {
	Detail(context.Context, string, string) (cdr.Detail, error)
}
type Handler struct {
	root           *os.Root
	path           string
	lookup         Lookup
	SourceLocation *time.Location
}

func New(path string, lookup Lookup) (*Handler, error) {
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, errors.New("recording root unavailable")
	}
	return &Handler{root: root, path: filepath.Clean(path), lookup: lookup, SourceLocation: time.UTC}, nil
}
func (h *Handler) Close() { h.root.Close() }
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request, tenant, id string) {
	d, e := h.lookup.Detail(r.Context(), tenant, id)
	if e != nil {
		missing(w, r)
		return
	}
	name := d.Summary.Recording
	if filepath.IsAbs(name) {
		name, e = filepath.Rel(h.path, name)
		if e != nil {
			missing(w, r)
			return
		}
	}
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsRune(name, 0) {
		missing(w, r)
		return
	}
	file, e := h.root.Open(name)
	if e != nil && filepath.Base(name) == name {
		loc := h.SourceLocation
		if loc == nil {
			loc = time.UTC
		}
		date := d.Summary.Start.In(loc)
		name = filepath.Join(date.Format("2006/01/02"), name)
		file, e = h.root.Open(name)
	}
	if e != nil {
		missing(w, r)
		return
	}
	defer file.Close()
	st, e := file.Stat()
	if e != nil || !st.Mode().IsRegular() {
		missing(w, r)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Content-Disposition", "inline")
	http.ServeContent(w, r, filepath.Base(name), st.ModTime(), file)
}

func missing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(404)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": "NOT_FOUND", "message": "Recording unavailable", "requestId": r.Header.Get("X-Request-Id")})
}
