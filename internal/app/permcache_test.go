package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

type countingChecker struct {
	calls   int
	allowed map[string]bool
	err     error
}

func (c *countingChecker) Has(_ context.Context, _, _, permission string) (bool, error) {
	c.calls++
	return c.allowed[permission], c.err
}

func TestPermCacheHitsAndExpires(t *testing.T) {
	next := &countingChecker{allowed: map[string]bool{"calls:read": true}}
	now := time.Unix(1000, 0)
	c := newPermCache(next, 30*time.Second, 100)
	c.now = func() time.Time { return now }
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if ok, e := c.Has(ctx, "t", "u", "calls:read"); !ok || e != nil {
			t.Fatal(ok, e)
		}
		if ok, e := c.Has(ctx, "t", "u", "live:read"); ok || e != nil {
			t.Fatal("denial", ok, e)
		}
	}
	if next.calls != 2 {
		t.Fatalf("calls = %d; decisions were not cached", next.calls)
	}
	if ok, _ := c.Has(ctx, "t", "other", "calls:read"); !ok || next.calls != 3 {
		t.Fatal("decision shared across users")
	}
	now = now.Add(30 * time.Second)
	next.allowed["calls:read"] = false
	if ok, _ := c.Has(ctx, "t", "u", "calls:read"); ok || next.calls != 4 {
		t.Fatalf("expired decision reused: calls=%d", next.calls)
	}
}
func TestPermCacheNeverCachesErrors(t *testing.T) {
	next := &countingChecker{allowed: map[string]bool{"calls:read": true}, err: errors.New("auth down")}
	c := newPermCache(next, 30*time.Second, 100)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, e := c.Has(ctx, "t", "u", "calls:read"); e == nil {
			t.Fatal("outage hidden")
		}
	}
	next.err = nil
	if ok, e := c.Has(ctx, "t", "u", "calls:read"); !ok || e != nil || next.calls != 3 {
		t.Fatalf("error was cached: calls=%d", next.calls)
	}
}
func TestPermCacheIsBounded(t *testing.T) {
	next := &countingChecker{allowed: map[string]bool{"calls:read": true}}
	c := newPermCache(next, time.Minute, 10)
	for i := 0; i < 100; i++ {
		_, _ = c.Has(context.Background(), "t", string(rune('a'+i)), "calls:read")
	}
	if len(c.m) > 10 {
		t.Fatalf("cache grew to %d", len(c.m))
	}
}
