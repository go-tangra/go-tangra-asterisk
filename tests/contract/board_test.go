package contract_test

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/presence"
)

func TestLiveBoard(t *testing.T) {
	off, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}})
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	off.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk/live/board", nil))
	if w.Code != 503 {
		t.Fatalf("without AMI: %d", w.Code)
	}
	board := presence.New()
	board.Frame(map[string]string{"Event": "DeviceStateChange", "Device": "PJSIP/49", "State": "NOT_INUSE"})
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: validIdentity{}, Checker: allow{}, AMIEnabled: true, Registry: calls.New(), Board: board, Support: func() httpapi.Support { return supportFake{} }})
	if e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/asterisk/live/board", nil))
	var v presence.View
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &v) != nil || len(v.Agents) != 2 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	states := map[string]string{}
	for _, a := range v.Agents {
		states[a.Extension] = a.State
	}
	if states["49"] != presence.Available || states["41"] != presence.Offline {
		t.Fatalf("team states: %v", states)
	}
}
