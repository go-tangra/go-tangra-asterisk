package cdr

import (
	"testing"
	"time"
)

// The period cache hands every caller its own slice (stats sorts in place),
// keeps at most periodEntries periods and drops expired ones.
func TestPeriodCache(t *testing.T) {
	r := &Repository{}
	k := periodKey{tenant: "t", from: 1, to: 2}
	r.store(k, []Call{{LinkedID: "a"}, {LinkedID: "b"}})

	got, ok := r.cached(k)
	if !ok || len(got) != 2 {
		t.Fatalf("cached = %v %v", got, ok)
	}
	got[0], got[1] = got[1], got[0] // a caller reorders its copy
	again, _ := r.cached(k)
	if again[0].LinkedID != "a" {
		t.Fatal("a caller's reorder leaked into the cache")
	}
	if _, ok := r.cached(periodKey{tenant: "other", from: 1, to: 2}); ok {
		t.Fatal("another tenant hit the cache")
	}

	for i := int64(10); i < 10+2*periodEntries; i++ {
		r.store(periodKey{tenant: "t", from: i, to: i + 1}, nil)
	}
	if len(r.cache) > periodEntries {
		t.Fatalf("cache holds %d periods", len(r.cache))
	}

	r.cache[k] = periodEntry{calls: []Call{{LinkedID: "old"}}, at: time.Now().Add(-periodTTL - time.Second)}
	if _, ok := r.cached(k); ok {
		t.Fatal("expired period served")
	}
}
