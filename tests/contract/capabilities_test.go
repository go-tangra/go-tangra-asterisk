package contract_test

import (
	"encoding/json"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/recordings"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// Registration and recordings opened after start become available without a
// restart, safely under concurrent requests.
func TestFeaturesBecomeAvailableLate(t *testing.T) {
	var store atomic.Pointer[registration.Repository]
	var files atomic.Pointer[recordings.Handler]
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}, Registration: store.Load, Recordings: files.Load})
	if e != nil {
		t.Fatal(e)
	}
	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	available := func() (bool, bool) {
		var out map[string]json.RawMessage
		if e := json.Unmarshal(get("/api/asterisk/capabilities").Body.Bytes(), &out); e != nil {
			t.Error(e)
		}
		var reg, rec httpapi.Capability
		_ = json.Unmarshal(out["registration"], &reg)
		_ = json.Unmarshal(out["recordings"], &rec)
		return reg.Available, rec.Available
	}
	if reg, rec := available(); reg || rec {
		t.Fatal("unopened features reported available")
	}
	if w := get("/api/asterisk/registration/online"); w.Code != 503 {
		t.Fatalf("registration before open: %d", w.Code)
	}
	h, e := recordings.New(t.TempDir(), nil)
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); available() }()
	}
	store.Store(&registration.Repository{Tenant: "t"})
	files.Store(h)
	wg.Wait()
	if reg, rec := available(); !reg || !rec {
		t.Fatal("late-opened features not reported available")
	}
}
