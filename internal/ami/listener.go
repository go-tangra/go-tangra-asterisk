package ami

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Listener struct {
	Config            config.AMI
	Registry          *calls.Registry
	Store             *registration.Repository
	OnError           func()
	OnContact         func(registration.Event)
	OnFrame           func(map[string]string)
	OnSweep           func()
	RegistrationFresh atomic.Bool

	// written is the last registration row stored per endpoint+contact; the
	// minute-by-minute contact snapshot only writes what changed (see
	// ShouldStore). Only the session goroutine touches it.
	written map[string]registration.Event
}

// RefreshAhead is how long before the stored expiry of an unchanged, still
// registered contact a refreshed row is written, so stored history never
// shows a continuously registered phone as expired. Contact snapshots run
// every minute, well inside this margin.
const RefreshAhead = 10 * time.Minute

// idleTimeout is how long a read waits for an AMI frame before the session
// pings the PBX to tell a quiet PBX from a dead connection.
var idleTimeout = 5 * time.Second

// ShouldStore reports whether e must be written given the last stored row
// prev of the same endpoint and contact: on any change other than the expiry
// (status, AOR, user agent, address), when the expiry moves earlier or appears
// or disappears, or when the stored expiry is about to lapse and e extends it.
// Repeated identical observations (the periodic PJSIPShowContacts snapshot,
// qualify RTT updates) are not stored again.
func ShouldStore(prev registration.Event, ok bool, e registration.Event, now time.Time) bool {
	switch {
	case !ok:
		return true
	case !strings.EqualFold(prev.Status, e.Status), prev.AOR != e.AOR, prev.UserAgent != e.UserAgent, prev.ViaAddress != e.ViaAddress:
		return true
	case prev.Expire.IsZero() != e.Expire.IsZero(), e.Expire.Before(prev.Expire):
		return true
	case !prev.Expire.IsZero() && prev.Expire.Sub(now) < RefreshAhead && e.Expire.After(prev.Expire):
		return true
	}
	return false
}

// store writes e unless it repeats the last stored row of its contact.
func (l *Listener) store(ctx context.Context, e registration.Event, force bool) error {
	if l.written == nil {
		l.written = map[string]registration.Event{}
	}
	key := e.Endpoint + "\x00" + e.Contact
	prev, ok := l.written[key]
	if !force && !ShouldStore(prev, ok, e, e.Time) {
		return nil
	}
	if err := l.Store.Append(ctx, e); err != nil {
		return err
	}
	l.written[key] = e
	return nil
}

func (l *Listener) Run(ctx context.Context) {
	backoff := time.Second
	if l.Store != nil {
		_ = l.Store.RestartGap(ctx)
	}
	for ctx.Err() == nil {
		started := time.Now()
		l.RegistrationFresh.Store(false)
		_ = l.session(ctx)
		l.RegistrationFresh.Store(false)
		l.Registry.Status(false)
		if l.Store != nil {
			_ = l.Store.BeginGap(ctx)
		}
		if l.OnError != nil {
			l.OnError()
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}
func (l *Listener) session(ctx context.Context) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	var conn net.Conn
	var err error
	if l.Config.TLS {
		td := tls.Dialer{NetDialer: &d, Config: &tls.Config{MinVersion: tls.VersionTLS12}}
		conn, err = td.DialContext(ctx, "tcp", l.Config.Address)
	} else {
		conn, err = d.DialContext(ctx, "tcp", l.Config.Address)
	}
	if err != nil {
		return errors.New("AMI unavailable")
	}
	defer conn.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	r := NewFrameReader(bufio.NewReaderSize(conn, 65536))
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if err = WriteAction(conn, "Login", map[string]string{"Username": l.Config.Username, "Secret": l.Config.Secret, "Events": "on"}); err != nil {
		return err
	}
	m, err := r.Next()
	if err != nil || m["Response"] != "Success" {
		return errors.New("AMI login rejected")
	}
	l.Registry.Reset(false)
	gen := l.Registry.Snapshot().Generation
	if err = WriteAction(conn, "CoreShowChannels", map[string]string{"ActionID": "live-snapshot"}); err != nil {
		return err
	}
	if err = WriteAction(conn, "PJSIPShowContacts", map[string]string{"ActionID": "contact-snapshot"}); err != nil {
		return err
	}

	sweep := func() error {
		if l.OnFrame == nil {
			return nil
		}
		if l.OnSweep != nil {
			l.OnSweep()
		}
		for _, action := range []string{"CoreSettings", "CoreStatus", "PJSIPShowEndpoints", "SIPpeers", "QueueStatus"} {
			if e := WriteAction(conn, action, map[string]string{"ActionID": "metrics-" + action}); e != nil {
				return e
			}
		}
		return nil
	}
	if err = sweep(); err != nil {
		return err
	}
	liveReady, contactsReady := false, false
	contacts := map[string]bool{}
	captureHealthy := true
	lastSnapshot := time.Now()
	failedCapture := func() {
		captureHealthy = false
		l.RegistrationFresh.Store(false)
		if l.Store != nil {
			_ = l.Store.RestartGap(ctx)
		}
		if l.OnError != nil {
			l.OnError()
		}
	}
	for ctx.Err() == nil {
		conn.SetDeadline(time.Now().Add(idleTimeout))
		m, err = r.Next()
		if err != nil {
			if n, ok := err.(net.Error); ok && n.Timeout() {
				// The expired read deadline also fails writes: renew it before
				// pinging, or every idle period drops the session.
				conn.SetDeadline(time.Now().Add(idleTimeout))
				if WriteAction(conn, "Ping", nil) != nil {
					return err
				}
				if time.Since(lastSnapshot) > time.Minute {
					contacts = map[string]bool{}
					contactsReady = false
					lastSnapshot = time.Now()
					if WriteAction(conn, "PJSIPShowContacts", map[string]string{"ActionID": "contact-snapshot"}) != nil {
						return err
					}
				}
				// A frame cut by the deadline resumes where it stopped.
				m, err = r.Next()
			}
			if err != nil {
				return err
			}
		}
		if time.Since(lastSnapshot) > time.Minute {
			contacts = map[string]bool{}
			contactsReady = false
			lastSnapshot = time.Now()
			if e := WriteAction(conn, "PJSIPShowContacts", map[string]string{"ActionID": "contact-snapshot"}); e != nil {
				return e
			}
			if e := sweep(); e != nil {
				return e
			}
		}
		if l.OnFrame != nil {
			l.OnFrame(m)
		}
		l.Registry.Apply(m, gen)
		switch m["Event"] {
		case "CoreShowChannelsComplete":
			liveReady = true
			l.Registry.Status(true)
		case "ContactStatus", "ContactList":
			e := Contact(m, time.Now().UTC())
			if e.Endpoint != "" {
				if m["Event"] == "ContactList" {
					contacts[e.Endpoint+"\x00"+e.Contact] = true
				}
				if l.OnContact != nil {
					l.OnContact(e)
				}
				if l.Store != nil && l.store(ctx, e, false) != nil {
					failedCapture()
				}
			}
		case "ContactListComplete":
			contactsReady = true
			if l.Store != nil {
				prior, e := l.Store.Contacts(ctx)
				if e != nil {
					failedCapture()
					break
				}
				good := true
				for _, contact := range prior {
					state := registration.Evaluate(contact.Endpoint, time.Now().UTC(), []registration.Event{contact}, nil)
					if state.Registered && !contacts[contact.Endpoint+"\x00"+contact.Contact] {
						contact.Time = time.Now().UTC()
						contact.Status = "Removed"
						if l.store(ctx, contact, true) != nil {
							good = false
							failedCapture()
						}
					}
				}
				if good {
					if !captureHealthy {
						_ = l.Store.RestartGap(ctx)
					}
					if l.Store.RecoverGap(ctx) != nil {
						failedCapture()
					} else {
						captureHealthy = true
						l.RegistrationFresh.Store(true)
					}
				}
			}
		}
		if (!liveReady || !contactsReady) && m["Response"] == "Error" && m["ActionID"] == "live-snapshot" {
			return errors.New("AMI live reconciliation rejected")
		}
	}

	return ctx.Err()
}
func Contact(m map[string]string, now time.Time) registration.Event {
	e := registration.Event{Time: now, Endpoint: m["EndpointName"], AOR: m["AOR"], Contact: m["URI"], Status: m["ContactStatus"], UserAgent: m["UserAgent"], ViaAddress: m["ViaAddress"]}
	if e.Endpoint == "" {
		e.Endpoint = m["Endpoint"]
	}
	if e.Endpoint == "" {
		e.Endpoint = m["ObjectName"]
	}
	if e.AOR == "" {
		e.AOR = m["Aor"]
	}
	if e.Contact == "" {
		e.Contact = m["Contact"]
	}
	if e.Status == "" {
		switch strings.ToLower(m["Status"]) {
		case "avail", "available", "reachable":
			e.Status = "Reachable"
		case "unavail", "unavailable", "unreachable":
			e.Status = "Unreachable"
		case "nonqual", "nonqualified", "unqualified":
			e.Status = "Unqualified"
		default:
			e.Status = "Unknown"
		}
	}
	e.RTT, _ = strconv.ParseInt(m["RoundtripUsec"], 10, 64)
	if exp, _ := strconv.ParseInt(m["RegExpire"], 10, 64); exp > 0 {
		e.Expire = time.Unix(exp, 0).UTC()
	}
	return e
}
