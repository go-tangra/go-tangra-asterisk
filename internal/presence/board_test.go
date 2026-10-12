package presence

import (
	"testing"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
)

var t0 = time.Date(2026, 10, 8, 8, 46, 1, 0, time.UTC)

func ch(id, name, state, caller, connected string, created time.Time, bridged *time.Time) calls.Channel {
	return calls.Channel{UniqueID: id, LinkedID: "L", Channel: name, State: state, Caller: caller, Connected: connected, Created: created, Bridged: bridged}
}
func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }
func ptr(t time.Time) *time.Time { return &t }

func board() *Board {
	b := New()
	b.now = func() time.Time { return t0 }
	for ext, state := range map[string]string{"49": "INUSE", "41": "RINGING", "45": "NOT_INUSE", "30": "UNAVAILABLE", "18": "NOT_INUSE"} {
		b.Frame(map[string]string{"Event": "DeviceStateChange", "Device": "PJSIP/" + ext, "State": state})
	}
	b.Frame(map[string]string{"Event": "DeviceStateChange", "Device": "Custom:DND18", "State": "BUSY"})
	b.Frame(map[string]string{"Event": "DeviceStateChange", "Device": "PJSIP/ITD", "State": "NOT_INUSE"})
	return b
}

func byExt(v View) map[string]Agent {
	out := map[string]Agent{}
	for _, a := range v.Agents {
		out[a.Extension] = a
	}
	return out
}

func TestRingingThenAnswered(t *testing.T) {
	b := board()
	ringingCall := calls.Snapshot{Fresh: true, Calls: []calls.Call{{LinkedID: "L", Channels: []calls.Channel{
		ch("L", "PJSIP/ITD-0001", "Up", "0888123456", "", at(0), nil),
		ch("a", "PJSIP/49-0002", "Ringing", "0888123456", "", at(18), nil),
		ch("b", "PJSIP/41-0003", "Ringing", "0888123456", "", at(18), nil),
	}}}}
	v := b.View(ringingCall, []string{"18", "30", "41", "45", "49"}, map[string]string{"49": "Tsvetomila"})
	if len(v.Waiting) != 1 || v.Waiting[0].Stage != "ringing" || len(v.Waiting[0].Ringing) != 2 || v.Waiting[0].Caller != "0888123456" {
		t.Fatalf("waiting: %+v", v.Waiting)
	}
	a := byExt(v)
	if a["49"].State != Ringing || a["49"].Call.Direction != "inbound" || a["49"].Name != "Tsvetomila" {
		t.Fatalf("49 rings for the caller: %+v", a["49"])
	}
	if a["30"].State != Offline || a["45"].State != Available || a["18"].State != DND {
		t.Fatalf("offline/available/dnd: %s %s %s", a["30"].State, a["45"].State, a["18"].State)
	}

	answered := calls.Snapshot{Fresh: true, Calls: []calls.Call{{LinkedID: "L", Channels: []calls.Channel{
		ch("L", "PJSIP/ITD-0001", "Up", "0888123456", "", at(0), ptr(at(24))),
		ch("a", "PJSIP/49-0002", "Up", "0888123456", "", at(18), ptr(at(24))),
	}}}}
	v = b.View(answered, []string{"49"}, nil)
	if len(v.Waiting) != 0 {
		t.Fatalf("an answered caller is not waiting: %+v", v.Waiting)
	}
	if a := byExt(v)["49"]; a.State != OnCall || !a.Call.Since.Equal(at(24)) || a.Call.Peer != "0888123456" {
		t.Fatalf("49 talks since the bridge: %+v %+v", a, a.Call)
	}
}

func TestWelcomeOutboundAndOthers(t *testing.T) {
	b := board()
	live := calls.Snapshot{Fresh: true, Calls: []calls.Call{
		{LinkedID: "W", Channels: []calls.Channel{ch("W", "PJSIP/ITD-0010", "Up", "0899000000", "", at(5), nil)}},
		{LinkedID: "O", Channels: []calls.Channel{
			ch("O", "PJSIP/45-0011", "Ring", "45", "0877111111", at(1), nil),
			ch("o", "PJSIP/ITD-0012", "Ringing", "359", "45", at(2), nil),
		}},
		{LinkedID: "I", Channels: []calls.Channel{
			ch("I", "PJSIP/22-0013", "Up", "22", "36", at(0), ptr(at(3))),
			ch("i", "PJSIP/36-0014", "Up", "36", "22", at(1), ptr(at(3))),
		}},
	}}
	v := b.View(live, []string{"45"}, nil)
	if len(v.Waiting) != 1 || v.Waiting[0].Stage != "welcome" || v.Waiting[0].LinkedID != "W" {
		t.Fatalf("a caller in the announcement waits: %+v", v.Waiting)
	}
	a := byExt(v)
	if a["45"].State != OnCall || a["45"].Call.Direction != "outbound" || a["45"].Call.Peer != "0877111111" {
		t.Fatalf("45 is dialling out: %+v %+v", a["45"], a["45"].Call)
	}
	if a["22"].Member || a["22"].Call.Direction != "internal" || a["22"].Call.Peer != "36" {
		t.Fatalf("non-members on calls are listed: %+v", a["22"])
	}
}

func TestEndpointSweepSeedsAndKeepsSince(t *testing.T) {
	b := New()
	now := t0
	b.now = func() time.Time { return now }
	b.Frame(map[string]string{"Event": "EndpointList", "ObjectName": "49", "DeviceState": "Not in use"})
	now = now.Add(time.Minute)
	b.Frame(map[string]string{"Event": "EndpointList", "ObjectName": "49", "DeviceState": "Not in use"})
	a := byExt(b.View(calls.Snapshot{Fresh: true}, []string{"49", "50"}, nil))
	if a["49"].State != Available || !a["49"].Since.Equal(t0) {
		t.Fatalf("an unchanged state keeps its start: %+v", a["49"])
	}
	if a["50"].State != Offline || a["50"].Since != nil {
		t.Fatalf("an unknown device is offline without a start: %+v", a["50"])
	}
}
