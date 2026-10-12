package ami

import (
	"bufio"
	"context"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"net"
	"strings"
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
	loginErr := make(chan error, 1)
	go func() {
		defer close(serverDone)
		conn, e := lis.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		m, e := ReadFrame(r)
		// Login carries no Events (some Asterisk builds drop such a
		// socket unanswered); events are switched on by a separate action.
		if e != nil || m["Action"] != "Login" || m["Events"] != "" || m["ActionID"] != "login" {
			loginErr <- fmt.Errorf("login frame %v (%v)", m, e)
			return
		}
		fmt.Fprint(conn, "Asterisk Call Manager/5.0\r\nResponse: Success\r\nActionID: login\r\n\r\n")
		if m, e = ReadFrame(r); e != nil || m["Action"] != "Events" || m["EventMask"] != "on" {
			loginErr <- fmt.Errorf("events frame %v (%v)", m, e)
			return
		}
		loginErr <- nil
		ReadFrame(r) // CoreShowChannels
		ReadFrame(r) // PJSIPShowContacts
		ReadFrame(r) // DeviceStateList
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
	select {
	case e := <-loginErr:
		if e != nil {
			cancel()
			t.Fatal(e)
		}
	default:
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

// A frame split across a pause longer than the read timeout must be resumed,
// not dropped: the session pings and the rest of the frame completes it.
func TestFrameSplitAcrossTimeoutIsKept(t *testing.T) {
	prev := idleTimeout
	idleTimeout = 100 * time.Millisecond
	defer func() { idleTimeout = prev }()
	lis, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer lis.Close()
	logins := make(chan struct{}, 16)
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
						fmt.Fprint(conn, "Response: Success\r\nActionID: live-snapshot\r\n\r\nEvent: CoreShowChannel\r\nUniq")
						time.Sleep(3 * idleTimeout / 2)
						fmt.Fprint(conn, "ueid: a\r\nLinkedid: call\r\nChannel: PJSIP/01-1\r\n\r\nEvent: CoreShowChannelsComplete\r\nActionID: live-snapshot\r\n\r\n")
					case "Ping":
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
	deadline := time.Now().Add(2 * time.Second)
	for !registry.Snapshot().Fresh && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if snapshot := registry.Snapshot(); !snapshot.Fresh || len(snapshot.Calls) != 1 {
		t.Fatalf("split frame lost: %+v", snapshot)
	}
	if n := len(logins); n != 1 {
		t.Fatalf("logins = %d; the paused frame dropped the session", n)
	}
}

// A refused login reaches OnError with AMI's reason (so the log says why),
// never with the secret.
func TestLoginRejectedReportsReason(t *testing.T) {
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
		fmt.Fprint(conn, "Asterisk Call Manager/5.0\r\n")
		if _, e := ReadFrame(bufio.NewReader(conn)); e != nil {
			return
		}
		fmt.Fprint(conn, "Response: Error\r\nActionID: login\r\nMessage: Authentication failed\r\n\r\n")
	}()
	errs := make(chan error, 4)
	l := Listener{Config: config.AMI{Address: lis.Addr().String(), Username: "observer", Secret: "s3cret-value"}, Registry: calls.New(),
		OnError: func(err error) { errs <- err }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go l.Run(ctx)
	select {
	case err := <-errs:
		if err == nil || !strings.Contains(err.Error(), "AMI login rejected: Authentication failed") {
			t.Fatalf("error = %v", err)
		}
		if strings.Contains(err.Error(), "s3cret-value") {
			t.Fatal("error carries the secret")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no error reported")
	}
}

// Asterisk answers PJSIPShowContacts with "No Contacts found" when nothing is
// registered: an empty, complete snapshot, not a failure. Any other error is
// reported with Asterisk's message instead of leaving capture stale silently.
func TestContactSnapshotErrors(t *testing.T) {
	for name, tc := range map[string]struct {
		message string
		wantErr string
	}{
		"no contacts": {"No Contacts found", ""},
		"refused":     {"Permission denied", "AMI contact snapshot rejected: Permission denied"},
	} {
		t.Run(name, func(t *testing.T) {
			lis, e := net.Listen("tcp", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer lis.Close()
			served := make(chan struct{})
			go func() {
				conn, e := lis.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				r := bufio.NewReader(conn)
				fmt.Fprint(conn, "Asterisk Call Manager/11.0.0\r\n")
				for {
					m, e := ReadFrame(r)
					if e != nil {
						return
					}
					switch m["Action"] {
					case "Login":
						fmt.Fprint(conn, "Response: Success\r\nActionID: login\r\nMessage: Authentication accepted\r\n\r\n")
					case "CoreShowChannels":
						fmt.Fprint(conn, "Response: Success\r\nActionID: live-snapshot\r\n\r\nEvent: CoreShowChannelsComplete\r\nActionID: live-snapshot\r\n\r\n")
					case "PJSIPShowContacts":
						fmt.Fprintf(conn, "Response: Error\r\nActionID: contact-snapshot\r\nMessage: %s\r\n\r\n", tc.message)
						close(served)
						<-time.After(300 * time.Millisecond)
						return
					}
				}
			}()
			errs := make(chan error, 8)
			registry := calls.New()
			l := Listener{Config: config.AMI{Address: lis.Addr().String(), Username: "observer", Secret: "secret"}, Registry: registry,
				OnError: func(err error) { errs <- err }}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go l.Run(ctx)
			select {
			case <-served:
			case <-time.After(2 * time.Second):
				t.Fatal("no contact snapshot requested")
			}
			time.Sleep(100 * time.Millisecond)
			var got error
			select {
			case got = <-errs:
			default:
			}
			if tc.wantErr == "" && got != nil {
				t.Fatalf("an empty contact list was reported as a failure: %v", got)
			}
			if tc.wantErr != "" && (got == nil || got.Error() != tc.wantErr) {
				t.Fatalf("error = %v, want %q", got, tc.wantErr)
			}
			if !registry.Snapshot().Fresh {
				t.Fatal("live state lost over a contact snapshot error")
			}
		})
	}
}
