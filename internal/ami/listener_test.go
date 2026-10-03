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
