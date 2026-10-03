package calls

import (
	"testing"
	"time"
)

func TestHangupOverflowAndGenerations(t *testing.T) {
	r := New()
	r.Reset(true)
	gen := r.Snapshot().Generation
	snapshot, ch, cancel := r.Subscribe()
	defer cancel()
	if !snapshot.Fresh {
		t.Fatal("not fresh")
	}
	r.Apply(map[string]string{"Event": "Newchannel", "Uniqueid": "1", "Linkedid": "a", "Channel": "PJSIP/01-1"}, gen)
	if len(r.Snapshot().Calls) != 1 {
		t.Fatal("missing call")
	}
	r.Reset(false)
	r.Apply(map[string]string{"Event": "Newchannel", "Uniqueid": "2", "Linkedid": "b"}, gen)
	if len(r.Snapshot().Calls) != 0 {
		t.Fatal("accepted stale generation")
	}
	_ = ch
	_, slow, closeSlow := r.Subscribe()
	defer closeSlow()
	for i := 0; i < 100; i++ {
		r.Status(false)
	}
	for range slow {
	}
	r.Reset(true)
	gen = r.Snapshot().Generation
	r.Apply(map[string]string{"Event": "Newchannel", "Uniqueid": "1", "Linkedid": "a"}, gen)
	r.Apply(map[string]string{"Event": "Hangup", "Uniqueid": "1"}, gen)
	if len(r.Snapshot().Calls) != 0 {
		t.Fatal("hangup retained")
	}
	_ = time.Now()
}
