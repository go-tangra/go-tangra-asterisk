// Package calls maintains an in-memory model of currently-active calls,
// fed by AMI events from the AMI listener. The registry is the single
// source of truth for both the gRPC ListActiveCalls snapshot endpoint
// and the SSE delta stream.
//
// Design notes:
//   - Indexed by Uniqueid (channel) and Linkedid (logical call). One
//     logical call may have many channels (bridged conversation,
//     conference, ring-all).
//   - All public methods are safe for concurrent use; the AMI listener
//     calls them from its single read goroutine, while the SSE handler
//     and gRPC service read snapshots from request goroutines.
//   - Subscribers receive a copy of every Event; a slow subscriber
//     gets dropped (channel closed) so a stalled browser tab can't
//     pin AMI events in memory forever.
package calls

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// EventType discriminates Event payloads.
type EventType string

const (
	EventCallStarted EventType = "call.started"
	EventCallUpdated EventType = "call.updated"
	EventCallEnded   EventType = "call.ended"
)

// Channel is a single AMI Channel (a call leg). Field names mirror AMI
// header names so the mapping from event to struct is obvious.
type Channel struct {
	Uniqueid          string    `json:"uniqueid"`
	Linkedid          string    `json:"linkedid"`
	Channel           string    `json:"channel"`
	ChannelState      string    `json:"channelState"`
	ChannelStateDesc  string    `json:"channelStateDesc"`
	CallerIDNum       string    `json:"callerIdNum"`
	CallerIDName      string    `json:"callerIdName"`
	ConnectedLineNum  string    `json:"connectedLineNum"`
	ConnectedLineName string    `json:"connectedLineName"`
	Exten             string    `json:"exten"`
	Context           string    `json:"context"`
	BridgeID          string    `json:"bridgeId"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// Call is the user-facing roll-up of one or more channels sharing a
// Linkedid. Channels is sorted by CreatedAt (oldest first) so the
// originating leg is always element 0.
type Call struct {
	Linkedid  string    `json:"linkedid"`
	Channels  []Channel `json:"channels"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Bridged is true when at least two of the call's channels are
	// currently in a shared bridge — i.e. a conversation is in progress.
	Bridged bool `json:"bridged"`
}

// Event is what subscribers receive. EndedCall is set only for EventCallEnded.
type Event struct {
	Type EventType `json:"type"`
	Call *Call     `json:"call,omitempty"`
	// EndedCall carries the final state at hangup; useful for the UI
	// to flash the row before removing it.
	EndedCall *Call `json:"endedCall,omitempty"`
	At        time.Time `json:"at"`
}

// subBufferSize bounds per-subscriber backlog; one slow consumer can't
// stall AMI ingestion. 64 is enough for ~30s of typical activity.
const subBufferSize = 64

type subscriber struct {
	id uint64
	ch chan Event
}

// Registry is the live call store.
type Registry struct {
	mu       sync.RWMutex
	channels map[string]*Channel  // Uniqueid → channel
	byCall   map[string][]string  // Linkedid → []Uniqueid

	subMu       sync.RWMutex
	subs        map[uint64]*subscriber
	nextSubID   atomic.Uint64
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		channels: make(map[string]*Channel),
		byCall:   make(map[string][]string),
		subs:     make(map[uint64]*subscriber),
	}
}

// Subscribe registers a consumer of Events. The returned channel is
// closed when the subscriber is dropped (either via Unsubscribe or
// because it fell behind subBufferSize). The returned id is the handle
// to pass to Unsubscribe.
func (r *Registry) Subscribe() (uint64, <-chan Event) {
	id := r.nextSubID.Add(1)
	s := &subscriber{id: id, ch: make(chan Event, subBufferSize)}
	r.subMu.Lock()
	r.subs[id] = s
	r.subMu.Unlock()
	return id, s.ch
}

// Unsubscribe removes a subscriber and closes its channel. Idempotent.
func (r *Registry) Unsubscribe(id uint64) {
	r.subMu.Lock()
	if s, ok := r.subs[id]; ok {
		delete(r.subs, id)
		close(s.ch)
	}
	r.subMu.Unlock()
}

// publish fans out an event to all current subscribers. A subscriber
// whose buffer is full is dropped — the SSE handler observes the
// closed channel and tears down its HTTP response.
func (r *Registry) publish(ev Event) {
	r.subMu.RLock()
	dead := make([]uint64, 0)
	for id, s := range r.subs {
		select {
		case s.ch <- ev:
		default:
			dead = append(dead, id)
		}
	}
	r.subMu.RUnlock()
	for _, id := range dead {
		r.Unsubscribe(id)
	}
}

// Snapshot returns the current set of calls, sorted by StartedAt asc.
// Safe to call from any goroutine; the returned slice and Channels
// slices are independent copies.
func (r *Registry) Snapshot() []Call {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Call, 0, len(r.byCall))
	for linkedid := range r.byCall {
		if c, ok := r.callLocked(linkedid); ok {
			out = append(out, *c)
		}
	}
	// Stable order: oldest call first so the UI's row order is
	// predictable across snapshots.
	sortByStartedAt(out)
	return out
}

// callLocked builds a Call view from the underlying channel set. r.mu
// must be held (RLock or Lock) by the caller.
func (r *Registry) callLocked(linkedid string) (*Call, bool) {
	uniqueids, ok := r.byCall[linkedid]
	if !ok || len(uniqueids) == 0 {
		return nil, false
	}
	chans := make([]Channel, 0, len(uniqueids))
	earliest := time.Time{}
	latest := time.Time{}
	bridgeIDs := make(map[string]int, 2)
	for _, uid := range uniqueids {
		ch, exists := r.channels[uid]
		if !exists {
			continue
		}
		chans = append(chans, *ch)
		if earliest.IsZero() || ch.CreatedAt.Before(earliest) {
			earliest = ch.CreatedAt
		}
		if ch.UpdatedAt.After(latest) {
			latest = ch.UpdatedAt
		}
		if ch.BridgeID != "" {
			bridgeIDs[ch.BridgeID]++
		}
	}
	if len(chans) == 0 {
		return nil, false
	}
	sortChannelsByCreatedAt(chans)
	bridged := false
	for _, n := range bridgeIDs {
		if n >= 2 {
			bridged = true
			break
		}
	}
	return &Call{
		Linkedid:  linkedid,
		Channels:  chans,
		StartedAt: earliest,
		UpdatedAt: latest,
		Bridged:   bridged,
	}, true
}

// ApplyNewchannel registers a new channel (or refreshes an existing
// one — Asterisk occasionally re-emits Newchannel for the same Uniqueid
// on resync).
func (r *Registry) ApplyNewchannel(c Channel) {
	if c.Uniqueid == "" {
		return
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	if c.Linkedid == "" {
		c.Linkedid = c.Uniqueid
	}

	r.mu.Lock()
	_, existed := r.channels[c.Uniqueid]
	r.channels[c.Uniqueid] = &c
	if !channelInCall(r.byCall[c.Linkedid], c.Uniqueid) {
		r.byCall[c.Linkedid] = append(r.byCall[c.Linkedid], c.Uniqueid)
	}
	call, _ := r.callLocked(c.Linkedid)
	r.mu.Unlock()

	if call == nil {
		return
	}
	t := EventCallStarted
	if existed || len(call.Channels) > 1 {
		t = EventCallUpdated
	}
	r.publish(Event{Type: t, Call: call, At: now})
}

// ApplyNewstate updates a channel's state (Ringing → Up, etc.).
func (r *Registry) ApplyNewstate(uniqueid, state, stateDesc string) {
	if uniqueid == "" {
		return
	}
	now := time.Now().UTC()
	r.mu.Lock()
	ch, ok := r.channels[uniqueid]
	if !ok {
		r.mu.Unlock()
		return
	}
	if state != "" {
		ch.ChannelState = state
	}
	if stateDesc != "" {
		ch.ChannelStateDesc = stateDesc
	}
	ch.UpdatedAt = now
	call, _ := r.callLocked(ch.Linkedid)
	r.mu.Unlock()

	if call != nil {
		r.publish(Event{Type: EventCallUpdated, Call: call, At: now})
	}
}

// ApplyCallerID updates the CallerID fields. Inbound calls that land on
// a ring group via an announcement only get a real CallerIDName once
// the dialplan progresses past the Playback() — until then the field
// is Asterisk's literal sentinel "<unknown>", which we normalise to
// empty so the UI doesn't show "(<unknown>)" forever.
func (r *Registry) ApplyCallerID(uniqueid, num, name string) {
	r.mutateChannel(uniqueid, func(ch *Channel) {
		if v := normaliseUnknown(num); v != "" {
			ch.CallerIDNum = v
		}
		if v := normaliseUnknown(name); v != "" {
			ch.CallerIDName = v
		}
	})
}

// ApplyConnectedLine updates the ConnectedLine fields. Same
// normalisation as ApplyCallerID.
func (r *Registry) ApplyConnectedLine(uniqueid, num, name string) {
	r.mutateChannel(uniqueid, func(ch *Channel) {
		if v := normaliseUnknown(num); v != "" {
			ch.ConnectedLineNum = v
		}
		if v := normaliseUnknown(name); v != "" {
			ch.ConnectedLineName = v
		}
	})
}

// ApplyExtension updates the Exten/Context fields. Fired by NewExten as
// the channel walks the dialplan (from-trunk → app-announcement-X →
// ext-group-X), giving the UI a more useful "to" value over time.
func (r *Registry) ApplyExtension(uniqueid, exten, context string) {
	r.mutateChannel(uniqueid, func(ch *Channel) {
		if exten != "" {
			ch.Exten = exten
		}
		if context != "" {
			ch.Context = context
		}
	})
}

// mutateChannel runs fn under the write lock and emits a call.updated
// event if the channel exists.
func (r *Registry) mutateChannel(uniqueid string, fn func(*Channel)) {
	if uniqueid == "" {
		return
	}
	now := time.Now().UTC()
	r.mu.Lock()
	ch, ok := r.channels[uniqueid]
	if !ok {
		r.mu.Unlock()
		return
	}
	fn(ch)
	ch.UpdatedAt = now
	call, _ := r.callLocked(ch.Linkedid)
	r.mu.Unlock()
	if call != nil {
		r.publish(Event{Type: EventCallUpdated, Call: call, At: now})
	}
}

// normaliseUnknown maps Asterisk's "no value" sentinels to "" so they
// don't reach the UI. Asterisk reports unknown caller info as literal
// "<unknown>" or "<no name>" / "<not provided>" depending on chan
// driver and version.
func normaliseUnknown(s string) string {
	switch s {
	case "", "<unknown>", "<no name>", "<not provided>":
		return ""
	}
	return s
}

// ApplyBridgeEnter / ApplyBridgeLeave update bridge membership.
func (r *Registry) ApplyBridgeEnter(uniqueid, bridgeID string) {
	r.updateBridge(uniqueid, bridgeID)
}
func (r *Registry) ApplyBridgeLeave(uniqueid string) {
	r.updateBridge(uniqueid, "")
}

func (r *Registry) updateBridge(uniqueid, bridgeID string) {
	if uniqueid == "" {
		return
	}
	now := time.Now().UTC()
	r.mu.Lock()
	ch, ok := r.channels[uniqueid]
	if !ok {
		r.mu.Unlock()
		return
	}
	ch.BridgeID = bridgeID
	ch.UpdatedAt = now
	call, _ := r.callLocked(ch.Linkedid)
	r.mu.Unlock()
	if call != nil {
		r.publish(Event{Type: EventCallUpdated, Call: call, At: now})
	}
}

// ApplyHangup removes a channel. When the last channel for a Linkedid
// hangs up, the call is removed and a call.ended event fires.
func (r *Registry) ApplyHangup(uniqueid string) {
	if uniqueid == "" {
		return
	}
	now := time.Now().UTC()
	r.mu.Lock()
	ch, ok := r.channels[uniqueid]
	if !ok {
		r.mu.Unlock()
		return
	}
	linkedid := ch.Linkedid
	delete(r.channels, uniqueid)
	r.byCall[linkedid] = removeString(r.byCall[linkedid], uniqueid)

	var endedCall *Call
	var updatedCall *Call
	if len(r.byCall[linkedid]) == 0 {
		delete(r.byCall, linkedid)
		// Synthesise a one-channel "ended" call so the UI has the
		// final state to flash in the row before removal.
		ec := Call{
			Linkedid:  linkedid,
			Channels:  []Channel{*ch},
			StartedAt: ch.CreatedAt,
			UpdatedAt: now,
		}
		endedCall = &ec
	} else {
		updatedCall, _ = r.callLocked(linkedid)
	}
	r.mu.Unlock()

	if endedCall != nil {
		r.publish(Event{Type: EventCallEnded, EndedCall: endedCall, At: now})
	} else if updatedCall != nil {
		r.publish(Event{Type: EventCallUpdated, Call: updatedCall, At: now})
	}
}

// Reset wipes the registry. Used on AMI reconnect before re-seeding so
// stale channels from a dropped session don't linger forever.
func (r *Registry) Reset() {
	r.mu.Lock()
	r.channels = make(map[string]*Channel)
	r.byCall = make(map[string][]string)
	r.mu.Unlock()
}

// Helpers.

func channelInCall(uids []string, uid string) bool {
	return slices.Contains(uids, uid)
}

func removeString(s []string, v string) []string {
	for i, x := range s {
		if x == v {
			return append(s[:i], s[i+1:]...)
		}
	}
	return s
}

func sortByStartedAt(in []Call) {
	// Tiny n; insertion sort beats sort.Slice's overhead and stays
	// stable without extra closures.
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].StartedAt.Before(in[j-1].StartedAt); j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}

func sortChannelsByCreatedAt(in []Channel) {
	for i := 1; i < len(in); i++ {
		for j := i; j > 0 && in[j].CreatedAt.Before(in[j-1].CreatedAt); j-- {
			in[j], in[j-1] = in[j-1], in[j]
		}
	}
}
