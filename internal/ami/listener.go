package ami

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// Config controls the listener. Mirror of data.AMIConfig with explicit
// types, decoupled so the package doesn't depend on data internals beyond
// the repo interface.
type Config struct {
	Host           string
	Port           int
	Username       string
	Secret         string
	ReconnectDelay time.Duration
}

// EventSink persists captured events. *data.PJSIPRegRepo satisfies this.
type EventSink interface {
	Insert(ctx context.Context, e *data.PJSIPRegEvent) error
	Enabled() bool
}

// Listener owns the AMI connection, runs the event loop, and writes
// ContactStatus events to the sink.
type Listener struct {
	log     *log.Helper
	cfg     Config
	sink    EventSink
	running atomic.Bool

	mu      sync.Mutex
	conn    net.Conn
	writeMu sync.Mutex
}

// sendAction serialises a write so concurrent goroutines (reconcile, pinger,
// session loop) don't interleave frames on the wire.
func (l *Listener) sendAction(w net.Conn, headers [][2]string) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return writeAction(w, headers)
}

// NewListener constructs a listener. Run starts the loop; Run is a no-op
// when AMI is disabled (host or sink missing).
func NewListener(ctx *bootstrap.Context, cfg Config, sink EventSink) *Listener {
	return &Listener{
		log:  ctx.NewLoggerHelper("asterisk/ami"),
		cfg:  cfg,
		sink: sink,
	}
}

// Enabled reports whether the listener is configured to start.
func (l *Listener) Enabled() bool {
	return l.cfg.Host != "" && l.sink != nil && l.sink.Enabled()
}

// Run blocks until ctx is cancelled. It reconnects on errors with the
// configured backoff. Safe to call exactly once; subsequent calls return
// immediately.
func (l *Listener) Run(ctx context.Context) {
	if !l.Enabled() {
		l.log.Info("AMI listener disabled (no host or no tangra DB)")
		return
	}
	if !l.running.CompareAndSwap(false, true) {
		return
	}
	defer l.running.Store(false)

	delay := l.cfg.ReconnectDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}

	l.log.Infof("AMI listener starting: %s:%d user=%s", l.cfg.Host, l.cfg.Port, l.cfg.Username)
	for {
		if err := l.session(ctx); err != nil && ctx.Err() == nil {
			l.log.Errorf("AMI session error: %v (retrying in %s)", err, delay)
		}
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// Stop closes the active connection. Safe to call multiple times.
func (l *Listener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.conn != nil {
		_ = l.conn.Close()
		l.conn = nil
	}
}

// session handles a single AMI connection lifetime: dial, login, reconcile,
// then read events until the connection drops.
func (l *Listener) session(ctx context.Context) error {
	addr := net.JoinHostPort(l.cfg.Host, strconv.Itoa(l.cfg.Port))
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.conn = nil
		l.mu.Unlock()
		_ = conn.Close()
	}()

	r := bufio.NewReader(conn)
	// Banner. AMI sends a single line "Asterisk Call Manager/X.Y.Z\r\n"
	// with NO terminating blank line. readMessage() would block waiting
	// for the blank line that ends a normal frame, until Asterisk's
	// authtimeout (~30s) fires and closes the socket. We'd then see the
	// banner read "succeed" with the EOF buffered, and the subsequent
	// Login would go onto a half-closed socket — yielding "login: EOF".
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	banner, err := r.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read banner: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	l.log.Infof("AMI banner: %s", strings.TrimRight(banner, "\r\n"))

	// Login — send bare credentials only. Some Asterisk builds drop the
	// socket without responding when the Events parameter contains a
	// class they don't recognise; by leaving Events out of Login we
	// authenticate first, then subscribe with a separate action below.
	l.log.Debugf("AMI -> Login user=%s secret_len=%d", l.cfg.Username, len(l.cfg.Secret))
	if err := l.sendAction(conn, [][2]string{
		{"Action", "Login"},
		{"ActionID", "login-1"},
		{"Username", l.cfg.Username},
		{"Secret", l.cfg.Secret},
	}); err != nil {
		return fmt.Errorf("send login: %w", err)
	}

	// Drain until login response.
	if err := awaitResponse(r, "login-1"); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	l.log.Info("AMI authenticated")

	// Subscribe to event classes. EventMask=on is universally accepted;
	// failures here are logged but don't tear down the session — the
	// listener still works for whatever the manager.conf `read=` line
	// allows by default.
	if err := l.sendAction(conn, [][2]string{
		{"Action", "Events"},
		{"ActionID", "events-1"},
		{"EventMask", "on"},
	}); err != nil {
		l.log.Warnf("send Events action: %v", err)
	}

	// Reconcile current registrations on connect — fills the gap that
	// opens when the listener was offline. Best effort; failures here are
	// logged but don't tear down the session.
	go func() {
		if err := l.reconcile(conn); err != nil && ctx.Err() == nil {
			l.log.Warnf("reconcile failed: %v", err)
		}
	}()

	// Periodic Ping keeps the read loop fed on quiet PBXs so the 120s
	// deadline doesn't spuriously tear down the connection.
	pingCtx, cancelPing := context.WithCancel(ctx)
	defer cancelPing()
	go l.pinger(pingCtx, conn)

	// Event loop.
	for {
		if ctx.Err() != nil {
			return nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		msg, err := readMessage(r)
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}
		l.dispatch(ctx, msg)
	}
}

// pinger sends Action: Ping every 30s. Failures terminate the goroutine; the
// next read on the session loop will then fail and trigger reconnect.
func (l *Listener) pinger(ctx context.Context, conn net.Conn) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := l.sendAction(conn, [][2]string{
				{"Action", "Ping"},
				{"ActionID", "ping"},
			}); err != nil {
				return
			}
		}
	}
}

// reconcile asks Asterisk for the current contact list and synthesises an
// event per contact so a "registered now" snapshot lands in the log even if
// no live ContactStatus event fired during connect.
func (l *Listener) reconcile(w net.Conn) error {
	// PJSIPShowContacts returns ContactList events followed by
	// ContactListComplete. Because the read loop is the single consumer of
	// the connection, we can't pull events here ourselves — instead we just
	// trigger the action and let dispatch() handle ContactList rows as
	// regular events (treating them like ContactStatus with status=Reachable).
	return l.sendAction(w, [][2]string{
		{"Action", "PJSIPShowContacts"},
		{"ActionID", "reconcile-1"},
	})
}

// dispatch routes a single AMI message. We persist ContactStatus events
// directly, and treat ContactList (response to PJSIPShowContacts) as
// reconcile-time snapshots.
func (l *Listener) dispatch(ctx context.Context, msg *Message) {
	t := msg.Type()
	if t != "" {
		l.log.Infof("AMI frame: type=%s endpoint=%s status=%s aor=%s", t,
			msg.Get("EndpointName"), msg.Get("ContactStatus"), msg.Get("AOR"))
	}
	switch t {
	case "ContactStatus":
		l.persist(ctx, parseContactStatus(msg))
	case "ContactList":
		l.persist(ctx, parseContactList(msg))
	}
}

func (l *Listener) persist(ctx context.Context, e *data.PJSIPRegEvent) {
	if e == nil || e.Endpoint == "" {
		return
	}
	if err := l.sink.Insert(ctx, e); err != nil {
		l.log.Warnf("persist event %s/%s: %v", e.Endpoint, e.Status, err)
	}
}

// awaitResponse pulls messages from the reader until one matches the given
// ActionID and is a Response. Returns an error if the response is not
// "Success".
func awaitResponse(r *bufio.Reader, actionID string) error {
	for range 32 {
		msg, err := readMessage(r)
		if err != nil {
			return err
		}
		if msg.Get("ActionID") != actionID {
			continue
		}
		if strings.EqualFold(msg.Get("Response"), "Success") {
			return nil
		}
		return fmt.Errorf("response: %s — %s", msg.Get("Response"), msg.Get("Message"))
	}
	return fmt.Errorf("no response for ActionID=%s", actionID)
}

// parseContactStatus maps an AMI ContactStatus event to our row type. See
// the package doc for field semantics.
func parseContactStatus(m *Message) *data.PJSIPRegEvent {
	e := &data.PJSIPRegEvent{
		EventTime:  time.Now().UTC(),
		Endpoint:   m.Get("EndpointName"),
		AOR:        m.Get("AOR"),
		ContactURI: m.Get("URI"),
		Status:     data.PJSIPRegStatus(m.Get("ContactStatus")),
		UserAgent:  m.Get("UserAgent"),
		ViaAddress: m.Get("ViaAddress"),
	}
	if rtt, err := strconv.ParseInt(m.Get("RoundtripUsec"), 10, 64); err == nil {
		e.RTTMicros = rtt
	}
	if exp, err := strconv.ParseInt(m.Get("RegExpire"), 10, 64); err == nil && exp > 0 {
		t := time.Unix(exp, 0).UTC()
		e.RegExpire = &t
	}
	return e
}

// parseContactList maps a ContactList row (emitted in response to
// PJSIPShowContacts) to a synthetic Reachable/Unreachable event. The row
// has fewer fields than ContactStatus — notably no ContactStatus enum, only
// a Status value like "Reachable", "Unreachable", "NonQualified".
func parseContactList(m *Message) *data.PJSIPRegEvent {
	endpoint := m.Get("EndpointName")
	if endpoint == "" {
		// Some Asterisk versions name the field "Endpoint" instead.
		endpoint = m.Get("Endpoint")
	}
	if endpoint == "" {
		return nil
	}
	status := data.PJSIPRegStatus(m.Get("Status"))
	if status == "" {
		status = data.PJSIPRegReachable
	}
	e := &data.PJSIPRegEvent{
		EventTime:  time.Now().UTC(),
		Endpoint:   endpoint,
		AOR:        m.Get("AOR"),
		ContactURI: m.Get("URI"),
		Status:     status,
		UserAgent:  m.Get("UserAgent"),
		ViaAddress: m.Get("ViaAddress"),
	}
	if rtt, err := strconv.ParseInt(m.Get("RoundtripUsec"), 10, 64); err == nil {
		e.RTTMicros = rtt
	}
	if exp, err := strconv.ParseInt(m.Get("RegExpire"), 10, 64); err == nil && exp > 0 {
		t := time.Unix(exp, 0).UTC()
		e.RegExpire = &t
	}
	return e
}
