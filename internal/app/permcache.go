package app

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"sync"
	"time"
)

// permCache remembers allow and deny decisions per tenant, user and permission
// for a short TTL; errors are never cached. Revoked sessions, users and tenants
// are rejected by token verification before any decision is consulted, so the
// TTL only bounds how long a role change in auth takes to apply here.
type permCache struct {
	next httpapi.Checker
	ttl  time.Duration
	max  int
	now  func() time.Time
	mu   sync.Mutex
	m    map[permKey]permEntry
}
type permKey struct{ tenant, user, permission string }
type permEntry struct {
	allowed bool
	until   time.Time
}

func newPermCache(next httpapi.Checker, ttl time.Duration, max int) *permCache {
	return &permCache{next: next, ttl: ttl, max: max, now: time.Now, m: map[permKey]permEntry{}}
}
func (c *permCache) Has(ctx context.Context, tenant, user, permission string) (bool, error) {
	k := permKey{tenant, user, permission}
	c.mu.Lock()
	v, ok := c.m[k]
	c.mu.Unlock()
	if ok && c.now().Before(v.until) {
		return v.allowed, nil
	}
	allowed, e := c.next.Has(ctx, tenant, user, permission)
	if e != nil {
		return false, e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.max {
		now := c.now()
		for key, v := range c.m {
			if !now.Before(v.until) {
				delete(c.m, key)
			}
		}
		for key := range c.m {
			if len(c.m) < c.max {
				break
			}
			delete(c.m, key)
		}
	}
	c.m[k] = permEntry{allowed, c.now().Add(c.ttl)}
	return allowed, nil
}
