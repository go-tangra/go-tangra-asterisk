package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// RecordingHandler streams call recordings from the FreePBX recordings
// directory mounted into this container. The endpoint resolves the file
// from cdr.recordingfile + cdr.calldate so the path can never be supplied
// by the client — only the linkedid is.
//
// Security guards:
//  - The `recordingfile` column is treated as an untrusted relative path.
//    Absolute paths and ".." segments are rejected outright.
//  - The final resolved path must remain inside the configured base dir
//    (ASTERISK_RECORDINGS_PATH). filepath.Rel verifies this after symlink
//    resolution.
type RecordingHandler struct {
	log      *log.Helper
	cdr      *sql.DB
	basePath string
	timeout  time.Duration
}

// NewRecordingHandler returns a handler bound to the given pool and base
// directory. When basePath is empty the handler returns 404 for every
// request — the operator simply hasn't mounted the recordings volume.
func NewRecordingHandler(l *log.Helper, mysql *data.MySQLClients, basePath string) *RecordingHandler {
	h := &RecordingHandler{
		log:      l,
		basePath: filepath.Clean(basePath),
		timeout:  10 * time.Second,
	}
	if mysql != nil {
		h.cdr = mysql.Cdr
		if mysql.Cfg != nil && mysql.Cfg.QueryTimeout > 0 {
			h.timeout = mysql.Cfg.QueryTimeout
		}
	}
	return h
}

// Serve handles GET /recordings/{linkedid}.
func (h *RecordingHandler) Serve(c kratosHttp.Context) error {
	if h.basePath == "" || h.basePath == "." {
		return writeError(c, http.StatusNotFound, "RECORDING_DISABLED",
			"recordings base path not configured (set ASTERISK_RECORDINGS_PATH)")
	}
	if h.cdr == nil {
		return writeError(c, http.StatusServiceUnavailable, "DB_UNAVAILABLE", "cdr db not available")
	}

	linkedid := strings.TrimSpace(c.Vars().Get("linkedid"))
	if linkedid == "" {
		return writeError(c, http.StatusBadRequest, "MISSING_LINKEDID", "linkedid is required")
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), h.timeout)
	defer cancel()

	const q = `
		SELECT recordingfile, calldate
		FROM cdr
		WHERE linkedid = ? AND recordingfile <> ''
		ORDER BY calldate ASC, sequence ASC
		LIMIT 1
	`
	var (
		recname  sql.NullString
		calldate time.Time
	)
	if err := h.cdr.QueryRowContext(ctx, q, linkedid).Scan(&recname, &calldate); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return writeError(c, http.StatusNotFound, "RECORDING_NOT_FOUND", "no recording for this call")
		}
		h.log.Errorf("recording lookup: %v", err)
		return writeError(c, http.StatusInternalServerError, "MYSQL_UNAVAILABLE", err.Error())
	}
	if !recname.Valid || recname.String == "" {
		return writeError(c, http.StatusNotFound, "RECORDING_NOT_FOUND", "no recording for this call")
	}

	fullPath, err := h.resolvePath(recname.String, calldate)
	if err != nil {
		h.log.Warnf("resolve recording %q (linkedid=%s): %v", recname.String, linkedid, err)
		return writeError(c, http.StatusNotFound, "RECORDING_NOT_FOUND", "recording file not accessible")
	}

	f, err := os.Open(fullPath)
	if err != nil {
		h.log.Warnf("open recording %s: %v", fullPath, err)
		return writeError(c, http.StatusNotFound, "RECORDING_NOT_FOUND", "recording file not accessible")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || stat.IsDir() {
		return writeError(c, http.StatusNotFound, "RECORDING_NOT_FOUND", "recording file not accessible")
	}

	w := c.Response()
	name := filepath.Base(fullPath)
	w.Header().Set("Content-Type", mimeForExtension(filepath.Ext(name)))
	w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Accept-Ranges", "bytes")

	// http.ServeContent handles Range requests, ETag and Last-Modified
	// based on stat.ModTime().
	http.ServeContent(w, c.Request(), name, stat.ModTime(), f)
	return nil
}

// resolvePath turns a (recordingfile, calldate) pair into a verified
// absolute path under basePath. FreePBX stores recordings in
// `<base>/YYYY/MM/DD/<filename>` (Sofia local date). The recordingfile
// column may contain just the filename, or the relative path including
// the date subdir, depending on the deployment — both are accepted.
func (h *RecordingHandler) resolvePath(name string, calldate time.Time) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("absolute paths are rejected")
	}
	clean := filepath.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("path traversal rejected")
	}

	candidates := []string{
		filepath.Join(h.basePath, clean),
	}
	// Standard FreePBX layout: monitor/YYYY/MM/DD/<file>. calldate is
	// in UTC from the DSN — FreePBX writes recordings in the asterisk
	// system timezone. We try UTC date first (matches `loc=UTC` DSN);
	// callers in non-UTC deployments can still ship an absolute relative
	// path in recordingfile.
	if calldate.Year() > 1970 && !strings.ContainsRune(clean, '/') {
		sub := fmt.Sprintf("%04d/%02d/%02d", calldate.Year(), int(calldate.Month()), calldate.Day())
		candidates = append(candidates, filepath.Join(h.basePath, sub, clean))
	}

	baseAbs, err := filepath.Abs(h.basePath)
	if err != nil {
		return "", err
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		// Resolve symlinks before the boundary check so a malicious
		// symlink can't escape basePath.
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			resolved = abs
		}
		rel, err := filepath.Rel(baseAbs, resolved)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", fmt.Errorf("path escapes base dir")
		}
		return resolved, nil
	}
	return "", fmt.Errorf("file not found under %s", h.basePath)
}

func mimeForExtension(ext string) string {
	switch strings.ToLower(ext) {
	case ".wav":
		return "audio/wav"
	case ".mp3":
		return "audio/mpeg"
	case ".ogg", ".oga":
		return "audio/ogg"
	case ".gsm":
		return "audio/x-gsm"
	case ".m4a":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	default:
		return "application/octet-stream"
	}
}

func writeError(c kratosHttp.Context, code int, key, msg string) error {
	w := c.Response()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, err := w.Write([]byte(fmt.Sprintf(`{"code":%d,"reason":%q,"message":%q}`, code, key, msg)))
	return err
}
