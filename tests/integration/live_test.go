package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type sessions struct{ revoked atomic.Bool }

func (s *sessions) Verify(context.Context, string) (authclient.Identity, error) {
	if s.revoked.Load() {
		return authclient.Identity{}, fmt.Errorf("revoked")
	}
	return authclient.Identity{TenantID: "t", UserID: "u"}, nil
}

type checker struct{}

func (checker) Has(context.Context, string, string, string) (bool, error) { return true, nil }
func TestSnapshotStreamRecoveryAndRevocation(t *testing.T) {
	registry := calls.New()
	registry.Reset(true)
	v := &sessions{}
	shutdown := make(chan struct{})
	handler, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: v, Checker: checker{}, AMIEnabled: true, Registry: registry, StreamLifetime: time.Second, Stop: shutdown})
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(handler.Handler())
	defer srv.Close()
	res, e := srv.Client().Get(srv.URL + "/api/asterisk/live/calls/stream")
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	r := bufio.NewReader(res.Body)
	line, e := r.ReadString('\n')
	if e != nil || line != "event: snapshot\n" {
		t.Fatalf("initial frame %q %v", line, e)
	}
	r.ReadString('\n')
	r.ReadString('\n')
	gen := registry.Snapshot().Generation
	registry.Apply(map[string]string{"Event": "Newchannel", "Uniqueid": "a", "Linkedid": "call"}, gen)
	line, e = r.ReadString('\n')
	if e != nil || line != "event: upsert\n" {
		t.Fatalf("update %q %v", line, e)
	}
	registry.Status(false)
	res.Body.Close()
	registry.Reset(true)
	registry.Apply(map[string]string{"Event": "Newchannel", "Uniqueid": "b", "Linkedid": "new"}, registry.Snapshot().Generation)
	snapshot, e := srv.Client().Get(srv.URL + "/api/asterisk/live/calls")
	if e != nil {
		t.Fatal(e)
	}
	snapshot.Body.Close()
	v.revoked.Store(true)
	denied, e := srv.Client().Get(srv.URL + "/api/asterisk/live/calls/stream")
	if e != nil {
		t.Fatal(e)
	}
	denied.Body.Close()
	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatal("revoked reconnect disclosed data")
	}
	close(shutdown)
}
func TestShutdownCancelsStream(t *testing.T) {
	stop := make(chan struct{})
	s, e := httpapi.New(httpapi.Deps{Tenant: "t", Verifier: &sessions{}, Checker: checker{}, AMIEnabled: true, Registry: calls.New(), Stop: stop})
	if e != nil {
		t.Fatal(e)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	res, e := srv.Client().Get(srv.URL + "/api/asterisk/live/calls/stream")
	if e != nil {
		t.Fatal(e)
	}
	r := bufio.NewReader(res.Body)
	for i := 0; i < 3; i++ {
		r.ReadString('\n')
	}
	close(stop)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, e := r.ReadString('\n')
			if e != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown retained stream")
	}
	res.Body.Close()
}
func TestUnknownRoutesDoNotReturnPBX(t *testing.T) {
	s, _ := httpapi.New(httpapi.Deps{})
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/calls", nil))
	if w.Code != 404 || strings.Contains(w.Body.String(), "linkedid") {
		t.Fatal("legacy public route exists")
	}
}
