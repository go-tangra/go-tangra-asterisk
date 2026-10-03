package recordings

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

type lookup struct{ file string }

func (l lookup) Detail(_ context.Context, tenant, id string) (cdr.Detail, error) {
	if tenant != "t" {
		return cdr.Detail{}, cdr.ErrMissing
	}
	return cdr.Detail{Summary: cdr.Call{Recording: l.file}}, nil
}
func TestRangeAndConfinement(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "audio.wav"), []byte("0123456789"), 0600)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(dir, "escape"))
	for _, tc := range []struct {
		file, rangeHeader, tenant string
		status                    int
		body                      string
	}{{"audio.wav", "bytes=2-4", "t", 206, "234"}, {"audio.wav", "bytes=20-30", "t", 416, ""}, {"../secret", "", "t", 404, ""}, {"escape", "", "t", 404, ""}, {outside, "", "t", 404, ""}, {"audio.wav", "bytes=0-1", "foreign", 404, ""}} {
		h, e := New(dir, lookup{tc.file})
		if e != nil {
			t.Fatal(e)
		}
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("Range", tc.rangeHeader)
		w := httptest.NewRecorder()
		h.Serve(w, req, tc.tenant, "a")
		h.Close()
		if w.Code != tc.status {
			t.Fatalf("%+v: status %d", tc, w.Code)
		}
		if tc.body != "" && w.Body.String() != tc.body {
			t.Fatal("incorrect seek")
		}
		if tc.status == 404 && w.Body.String() == "secret" {
			t.Fatal("escaped root")
		}
	}
}
