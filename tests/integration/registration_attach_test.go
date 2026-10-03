//go:build integration

package integration_test

import (
	"bufio"
	"context"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/ami"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"net"
	"sync"
	"testing"
	"time"
)

// Registration storage that opens while an AMI session is running starts
// capturing from the next contact snapshot, and only then closes its gap.
func TestRegistrationAttachedMidSession(t *testing.T) {
	c, _, _ := fixture(t)
	if c.Binding.RegistrationDSN == "" {
		t.Skip("set ASTERISK_FIXTURE_REGISTRATION_DSN to run registration fixture tests")
	}
	ctx := context.Background()
	if e := registration.Bootstrap(ctx, c); e != nil {
		t.Fatal(e)
	}
	store, e := registration.Open(ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	defer store.DB.Close()
	for _, q := range []string{"DELETE FROM pjsip_registration_events", "DELETE FROM asterisk_observation_gaps"} {
		if _, e = store.DB.ExecContext(ctx, q); e != nil {
			t.Fatal(e)
		}
	}
	lis, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer lis.Close()
	go func() {
		conn, e := lis.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		var mu sync.Mutex
		write := func(s string) { mu.Lock(); fmt.Fprint(conn, s); mu.Unlock() }
		stop := make(chan struct{})
		defer close(stop)
		go func() {
			for {
				select {
				case <-stop:
					return
				case <-time.After(20 * time.Millisecond):
					write("Event: VarSet\r\nVariable: tick\r\n\r\n")
				}
			}
		}()
		r := bufio.NewReader(conn)
		for {
			m, e := ami.ReadFrame(r)
			if e != nil {
				return
			}
			switch m["Action"] {
			case "Login":
				write("Asterisk Call Manager/5.0\r\nResponse: Success\r\n\r\n")
			case "CoreShowChannels":
				write("Response: Success\r\nActionID: live-snapshot\r\n\r\nEvent: CoreShowChannelsComplete\r\nActionID: live-snapshot\r\n\r\n")
			case "PJSIPShowContacts":
				exp := time.Now().Add(time.Hour).Unix()
				write(fmt.Sprintf("Response: Success\r\nActionID: contact-snapshot\r\n\r\nEvent: ContactList\r\nEndpoint: 01\r\nAor: 01\r\nUri: sip:01@10.0.0.1\r\nStatus: Reachable\r\nRegExpire: %d\r\n\r\nEvent: ContactListComplete\r\nActionID: contact-snapshot\r\n\r\n", exp))
			}
		}
	}()
	registry := calls.New()
	l := &ami.Listener{Config: config.AMI{Address: lis.Addr().String(), Username: "observer", Secret: "secret"}, Registry: registry}
	lctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { l.Run(lctx); close(done) }()
	defer func() { cancel(); <-done }()
	wait := func(what string, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !ok() {
			if time.Now().After(deadline) {
				t.Fatal(what)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	wait("live snapshot", func() bool { return registry.Snapshot().Fresh })
	if l.RegistrationFresh.Load() {
		t.Fatal("registration fresh without storage")
	}
	l.Store.Store(store)
	wait("registration capture after attach", l.RegistrationFresh.Load)
	var events, open, gaps int
	if e = store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM pjsip_registration_events WHERE endpoint='01'").Scan(&events); e != nil {
		t.Fatal(e)
	}
	if e = store.DB.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(ended_at IS NULL),0) FROM asterisk_observation_gaps").Scan(&gaps, &open); e != nil {
		t.Fatal(e)
	}
	if events != 1 || gaps != 1 || open != 0 {
		t.Fatalf("events=%d gaps=%d open=%d", events, gaps, open)
	}
}
