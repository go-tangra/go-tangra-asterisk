package ami

import (
	"bufio"
	"context"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"net"
	"testing"
	"time"
)

func TestFakeServerReconciliationAndCancellation(t *testing.T) {
	lis, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer lis.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, e := lis.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		m, e := ReadFrame(r)
		if e != nil || m["Action"] != "Login" {
			return
		}
		fmt.Fprint(conn, "Asterisk Call Manager/5.0\r\nResponse: Success\r\n\r\n")
		ReadFrame(r)
		ReadFrame(r)
		fmt.Fprint(conn, "Event: CoreShowChannel\r\nUniqueid: a\r\nLinkedid: call\r\nChannel: PJSIP/01-1\r\n\r\nEvent: CoreShowChannelsComplete\r\n\r\nEvent: ContactListComplete\r\n\r\n")
		ReadFrame(r)
	}()
	registry := calls.New()
	l := Listener{Config: config.AMI{Address: lis.Addr().String(), Username: "observer", Secret: "secret"}, Registry: registry}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for !registry.Snapshot().Fresh && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if snapshot := registry.Snapshot(); !snapshot.Fresh || len(snapshot.Calls) != 1 {
		cancel()
		t.Fatalf("snapshot: %+v", snapshot)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("listener did not cancel")
	}
	<-serverDone
}
func TestContactFieldCompatibility(t *testing.T) {
	e := Contact(map[string]string{"EndpointName": "001", "URI": "sip:a", "Status": "NonQualified", "RegExpire": "1791000000"}, time.Now())
	if e.Endpoint != "001" || e.Status != "Unqualified" || e.Expire.IsZero() {
		t.Fatalf("%+v", e)
	}
}

// A quiet PBX (no events for longer than the idle timeout) must keep its
// session: the listener pings and goes on reading instead of reconnecting.
func TestIdleSessionPingsAndStays(t *testing.T) {
	prev := idleTimeout
	idleTimeout = 100 * time.Millisecond
	defer func() { idleTimeout = prev }()
	lis, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer lis.Close()
	logins := make(chan struct{}, 16)
	pings := make(chan struct{}, 64)
	go func() {
		for {
			conn, e := lis.Accept()
			if e != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				r := bufio.NewReader(conn)
				for {
					m, e := ReadFrame(r)
					if e != nil {
						return
					}
					switch m["Action"] {
					case "Login":
						logins <- struct{}{}
						fmt.Fprint(conn, "Asterisk Call Manager/5.0\r\nResponse: Success\r\n\r\n")
					case "CoreShowChannels":
						fmt.Fprint(conn, "Response: Success\r\nActionID: live-snapshot\r\n\r\nEvent: CoreShowChannelsComplete\r\nActionID: live-snapshot\r\n\r\n")
					case "PJSIPShowContacts":
						fmt.Fprint(conn, "Response: Success\r\nActionID: contact-snapshot\r\n\r\nEvent: ContactListComplete\r\nActionID: contact-snapshot\r\n\r\n")
					case "Ping":
						pings <- struct{}{}
						fmt.Fprint(conn, "Response: Success\r\nPing: Pong\r\n\r\n")
					}
				}
			}(conn)
		}
	}()
	registry := calls.New()
	l := Listener{Config: config.AMI{Address: lis.Addr().String(), Username: "observer", Secret: "secret"}, Registry: registry}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	time.Sleep(10 * idleTimeout)
	if n := len(logins); n != 1 {
		t.Fatalf("logins = %d; the idle session was dropped and re-established", n)
	}
	if len(pings) < 3 {
		t.Fatalf("pings = %d; the idle session did not keep pinging", len(pings))
	}
	if !registry.Snapshot().Fresh {
		t.Fatal("live state went stale while the PBX was merely quiet")
	}
}
