// Package presence tells, live, what every agent is doing: offline,
// available, ringing, on a call, on hold or Do Not Disturb, and which
// callers are waiting. It combines AMI device states (phones, and FreePBX's
// Custom:DND<ext>) with the live call registry.
package presence

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
)

// Agent states.
const (
	Offline   = "offline"
	Available = "available"
	Ringing   = "ringing"
	OnCall    = "on-call"
	OnHold    = "on-hold"
	DND       = "dnd"
)

// device is an endpoint's normalised device state and when it began.
type device struct {
	state string // offline, idle, ringing, busy, hold
	since time.Time
}

// Board keeps the device states AMI reports. Frame is the listener hook.
type Board struct {
	mu      sync.Mutex
	devices map[string]device
	dnd     map[string]bool
	now     func() time.Time
}

func New() *Board {
	return &Board{devices: map[string]device{}, dnd: map[string]bool{}, now: time.Now}
}

func normalise(s string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(s, "_", ""), " ", "")) {
	case "notinuse":
		return "idle"
	case "inuse", "busy", "ringinuse":
		return "busy"
	case "ringing", "ring":
		return "ringing"
	case "onhold":
		return "hold"
	default: // unavailable, invalid, unknown
		return Offline
	}
}

func (b *Board) set(ext, raw string) {
	if ext == "" {
		return
	}
	state := normalise(raw)
	if d, ok := b.devices[ext]; ok && d.state == state {
		return
	}
	b.devices[ext] = device{state: state, since: b.now().UTC()}
}

// Frame applies one AMI frame: DeviceStateChange events (the DeviceStateList
// snapshot and live changes) and the periodic PJSIPShowEndpoints list.
func (b *Board) Frame(m map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch m["Event"] {
	case "DeviceStateChange":
		dev := m["Device"]
		switch {
		case strings.HasPrefix(dev, "PJSIP/"):
			b.set(strings.TrimPrefix(dev, "PJSIP/"), m["State"])
		case strings.HasPrefix(dev, "Custom:DND"):
			b.dnd[strings.TrimPrefix(dev, "Custom:DND")] = normalise(m["State"]) == "busy"
		}
	case "EndpointList":
		id := m["ObjectName"]
		if id == "" {
			id = m["Endpoint"]
		}
		b.set(id, m["DeviceState"])
	}
}

// AgentCall is the call an agent is on (or ringing for).
type AgentCall struct {
	LinkedID  string    `json:"linkedid"`
	Direction string    `json:"direction"` // inbound, outbound, internal
	Peer      string    `json:"peer"`
	Since     time.Time `json:"since"` // talk start, or ring start
}

// Agent is one agent on the board.
type Agent struct {
	Extension string     `json:"extension"`
	Name      string     `json:"name"`
	Member    bool       `json:"member"`
	State     string     `json:"state"`
	Since     *time.Time `json:"since"` // when the device entered its state
	DND       bool       `json:"dnd"`
	Call      *AgentCall `json:"call"`
}

// Waiting is a caller not (yet) talking to anyone.
type Waiting struct {
	LinkedID string    `json:"linkedid"`
	Caller   string    `json:"caller"`
	Stage    string    `json:"stage"` // welcome (announcement), ringing
	Since    time.Time `json:"since"`
	Ringing  []string  `json:"ringing"`
}

// View is the board at one instant.
type View struct {
	Fresh   bool      `json:"fresh"`
	Updated time.Time `json:"updated"`
	Agents  []Agent   `json:"agents"`
	Waiting []Waiting `json:"waiting"`
}

var endpointRe = regexp.MustCompile(`^PJSIP/(.+)-[0-9a-f]+$`)

func endpoint(ch string) string {
	if m := endpointRe.FindStringSubmatch(ch); m != nil {
		return m[1]
	}
	return ""
}
func numeric(s string) bool { _, err := strconv.Atoi(s); return s != "" && err == nil }

// View builds the board: the team (with names) plus any other extension
// that is on a call, from the device states and the live calls.
func (b *Board) View(live calls.Snapshot, team []string, names map[string]string) View {
	b.mu.Lock()
	devices := make(map[string]device, len(b.devices))
	for k, v := range b.devices {
		devices[k] = v
	}
	dnd := make(map[string]bool, len(b.dnd))
	for k, v := range b.dnd {
		dnd[k] = v
	}
	b.mu.Unlock()

	v := View{Fresh: live.Fresh && len(devices) > 0, Updated: live.Updated, Agents: []Agent{}, Waiting: []Waiting{}}
	agentCalls := map[string]*AgentCall{}
	ringing, talking := map[string]bool{}, map[string]bool{}
	for _, c := range live.Calls {
		chans := append([]calls.Channel(nil), c.Channels...)
		sort.Slice(chans, func(i, j int) bool { return chans[i].Created.Before(chans[j].Created) })
		var trunk *calls.Channel
		var agents []calls.Channel
		for i := range chans {
			ep := endpoint(chans[i].Channel)
			switch {
			case numeric(ep):
				agents = append(agents, chans[i])
			case ep != "" && trunk == nil:
				trunk = &chans[i]
			}
		}
		answered := false
		var rang []string
		for _, ch := range agents {
			ext := endpoint(ch.Channel)
			ac := &AgentCall{LinkedID: c.LinkedID, Since: ch.Created}
			switch {
			case trunk != nil && ch.Created.Before(trunk.Created):
				ac.Direction, ac.Peer = "outbound", ch.Connected
			case trunk != nil:
				ac.Direction, ac.Peer = "inbound", trunk.Caller
			default:
				ac.Direction = "internal"
				for _, o := range agents {
					if o.UniqueID != ch.UniqueID {
						ac.Peer = endpoint(o.Channel)
					}
				}
			}
			if ch.Bridged != nil {
				ac.Since = *ch.Bridged
				answered = true
				talking[ext] = true
			} else if strings.HasPrefix(ch.State, "Ring") && ac.Direction != "outbound" {
				ringing[ext] = true
				rang = append(rang, ext)
			}
			if prev := agentCalls[ext]; prev == nil || ch.Bridged != nil {
				agentCalls[ext] = ac
			}
		}
		if trunk != nil && (len(agents) == 0 || !agents[0].Created.Before(trunk.Created)) && !answered {
			w := Waiting{LinkedID: c.LinkedID, Caller: trunk.Caller, Stage: "welcome", Since: trunk.Created, Ringing: []string{}}
			if len(rang) > 0 {
				w.Stage, w.Ringing = "ringing", rang
			}
			v.Waiting = append(v.Waiting, w)
		}
	}
	sort.Slice(v.Waiting, func(i, j int) bool { return v.Waiting[i].Since.Before(v.Waiting[j].Since) })

	listed := map[string]bool{}
	add := func(ext string, member bool) {
		if listed[ext] {
			return
		}
		listed[ext] = true
		a := Agent{Extension: ext, Name: names[ext], Member: member, DND: dnd[ext], State: Offline}
		d, known := devices[ext]
		if known {
			t := d.since
			a.Since = &t
		}
		call := agentCalls[ext]
		switch {
		case known && d.state == Offline:
			a.State = Offline
		case call != nil && (talking[ext] || !ringing[ext]): // talking, or dialling out
			a.State, a.Call = OnCall, call
			if d.state == "hold" {
				a.State = OnHold
			}
		case call != nil:
			a.State, a.Call = Ringing, call
		case a.DND:
			a.State = DND
		case d.state == "busy" || d.state == "hold": // a call the registry has not seen
			a.State = OnCall
		case d.state == "ringing":
			a.State = Ringing
		case known:
			a.State = Available
		}
		v.Agents = append(v.Agents, a)
	}
	for _, ext := range team {
		add(ext, true)
	}
	for ext := range agentCalls {
		add(ext, false)
	}
	return v
}
