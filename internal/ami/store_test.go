package ami

import (
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
)

// The minute-by-minute contact snapshot only stores what changed, yet a
// continuously registered phone never appears expired in stored history.
func TestShouldStore(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	base := registration.Event{Endpoint: "101", Contact: "sip:101@10.0.0.5", AOR: "101", Status: "Reachable", UserAgent: "Yealink", ViaAddress: "10.0.0.5", Expire: now.Add(time.Hour)}
	with := func(f func(*registration.Event)) registration.Event { e := base; f(&e); return e }

	cases := []struct {
		name string
		prev registration.Event
		ok   bool
		e    registration.Event
		want bool
	}{
		{"first observation", registration.Event{}, false, base, true},
		{"identical snapshot", base, true, base, false},
		{"status case only", base, true, with(func(e *registration.Event) { e.Status = "reachable" }), false},
		{"rtt only", base, true, with(func(e *registration.Event) { e.RTT = 4200 }), false},
		{"status change", base, true, with(func(e *registration.Event) { e.Status = "Unreachable" }), true},
		{"user agent change", base, true, with(func(e *registration.Event) { e.UserAgent = "Grandstream" }), true},
		{"address change", base, true, with(func(e *registration.Event) { e.ViaAddress = "10.0.0.9" }), true},
		{"aor change", base, true, with(func(e *registration.Event) { e.AOR = "101b" }), true},
		{"expiry extended far ahead", base, true, with(func(e *registration.Event) { e.Expire = now.Add(2 * time.Hour) }), false},
		{"expiry moved earlier", base, true, with(func(e *registration.Event) { e.Expire = now.Add(30 * time.Minute) }), true},
		{"expiry dropped", base, true, with(func(e *registration.Event) { e.Expire = time.Time{} }), true},
		{"stored expiry about to lapse, extended", with(func(e *registration.Event) { e.Expire = now.Add(5 * time.Minute) }), true, base, true},
		{"stored expiry about to lapse, same", with(func(e *registration.Event) { e.Expire = now.Add(5 * time.Minute) }), true, with(func(e *registration.Event) { e.Expire = now.Add(5 * time.Minute) }), false},
	}
	for _, c := range cases {
		if got := ShouldStore(c.prev, c.ok, c.e, now); got != c.want {
			t.Errorf("%s: ShouldStore = %v, want %v", c.name, got, c.want)
		}
	}

	// A day of minute snapshots of one phone re-registering hourly stores a
	// row about every 50 minutes, not 1440.
	stored, prev, ok := 0, registration.Event{}, false
	for m := 0; m < 24*60; m++ {
		at := now.Add(time.Duration(m) * time.Minute)
		e := base
		e.Time = at
		e.Expire = at.Truncate(time.Hour).Add(time.Hour + 5*time.Minute) // re-registers at each full hour
		if ShouldStore(prev, ok, e, at) {
			stored++
			prev, ok = e, true
		}
		if !at.Before(prev.Expire) {
			t.Fatalf("stored row expired at %s while the phone stayed registered", at)
		}
	}
	if stored > 40 {
		t.Fatalf("stored %d rows for one stable phone in a day", stored)
	}
}
