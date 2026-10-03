package calls

import (
	"sort"
	"sync"
	"time"
)

type Channel struct {
	UniqueID  string    `json:"uniqueid"`
	LinkedID  string    `json:"linkedid"`
	Channel   string    `json:"channel"`
	State     string    `json:"state"`
	Caller    string    `json:"caller"`
	Connected string    `json:"connected"`
	Bridge    string    `json:"bridge"`
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
}
type Call struct {
	LinkedID string    `json:"linkedid"`
	Channels []Channel `json:"channels"`
}
type Snapshot struct {
	Calls      []Call    `json:"calls"`
	Generation uint64    `json:"generation"`
	Fresh      bool      `json:"fresh"`
	Updated    time.Time `json:"updated"`
}
type Update struct {
	Type       string `json:"type"`
	Call       *Call  `json:"call,omitempty"`
	LinkedID   string `json:"linkedid,omitempty"`
	Generation uint64 `json:"generation"`
	Fresh      bool   `json:"fresh"`
}
type Registry struct {
	mu          sync.Mutex
	channels    map[string]Channel
	subscribers map[uint64]chan Update
	next        uint64
	generation  uint64
	fresh       bool
	updated     time.Time
}

func New() *Registry {
	return &Registry{channels: map[string]Channel{}, subscribers: map[uint64]chan Update{}}
}
func (r *Registry) emit(v Update) {
	for id, ch := range r.subscribers {
		select {
		case ch <- v:
		default:
			close(ch)
			delete(r.subscribers, id)
		}
	}
}
func (r *Registry) Reset(fresh bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels = map[string]Channel{}
	r.generation++
	r.fresh = fresh
	r.updated = time.Now().UTC()
	for id, ch := range r.subscribers {
		close(ch)
		delete(r.subscribers, id)
	}
}
func (r *Registry) Status(fresh bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fresh = fresh
	r.updated = time.Now().UTC()
	r.emit(Update{Type: "status", Generation: r.generation, Fresh: fresh})
}
func (r *Registry) call(id string) Call {
	c := Call{LinkedID: id, Channels: []Channel{}}
	for _, ch := range r.channels {
		if ch.LinkedID == id {
			c.Channels = append(c.Channels, ch)
		}
	}
	sort.Slice(c.Channels, func(i, j int) bool { return c.Channels[i].UniqueID < c.Channels[j].UniqueID })
	return c
}
func (r *Registry) snapshot() Snapshot {
	s := Snapshot{Calls: []Call{}, Generation: r.generation, Fresh: r.fresh, Updated: r.updated}
	grouped := map[string][]Channel{}
	for _, ch := range r.channels {
		grouped[ch.LinkedID] = append(grouped[ch.LinkedID], ch)
	}
	for id, channels := range grouped {
		sort.Slice(channels, func(i, j int) bool { return channels[i].UniqueID < channels[j].UniqueID })
		s.Calls = append(s.Calls, Call{LinkedID: id, Channels: channels})
	}
	sort.Slice(s.Calls, func(i, j int) bool { return s.Calls[i].LinkedID < s.Calls[j].LinkedID })
	return s
}

func (r *Registry) Snapshot() Snapshot { r.mu.Lock(); defer r.mu.Unlock(); return r.snapshot() }
func (r *Registry) Subscribe() (Snapshot, <-chan Update, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.next
	r.next++
	ch := make(chan Update, 32)
	r.subscribers[id] = ch
	return r.snapshot(), ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if c, ok := r.subscribers[id]; ok {
			close(c)
			delete(r.subscribers, id)
		}
	}
}
func (r *Registry) Apply(m map[string]string, generation uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if generation != r.generation {
		return
	}
	id := m["Uniqueid"]
	if id == "" {
		return
	}
	ch, exists := r.channels[id]
	kind := m["Event"]
	if kind == "Hangup" {
		if !exists {
			return
		}
		delete(r.channels, id)
		c := r.call(ch.LinkedID)
		u := Update{Type: "remove", LinkedID: ch.LinkedID, Generation: r.generation, Fresh: r.fresh}
		if len(c.Channels) > 0 {
			u.Type = "upsert"
			u.Call = &c
		}
		r.emit(u)
		return
	}
	switch kind {
	case "Newchannel", "Newstate", "NewCallerid", "NewConnectedLine", "BridgeEnter", "BridgeLeave", "CoreShowChannel":
	default:
		return
	}
	if !exists {
		if len(r.channels) >= 10000 {
			r.fresh = false
			return
		}
		ch = Channel{UniqueID: id, LinkedID: m["Linkedid"], Created: time.Now().UTC()}
		if ch.LinkedID == "" {
			ch.LinkedID = id
		}
	}
	for key, p := range map[string]*string{"Channel": &ch.Channel, "ChannelStateDesc": &ch.State, "CallerIDNum": &ch.Caller, "ConnectedLineNum": &ch.Connected, "BridgeUniqueid": &ch.Bridge} {
		if v, ok := m[key]; ok {
			*p = v
		}
	}
	if kind == "BridgeLeave" {
		ch.Bridge = ""
	}
	ch.Updated = time.Now().UTC()
	r.updated = ch.Updated
	r.channels[id] = ch
	c := r.call(ch.LinkedID)
	r.emit(Update{Type: "upsert", Call: &c, LinkedID: ch.LinkedID, Generation: r.generation, Fresh: r.fresh})
}
