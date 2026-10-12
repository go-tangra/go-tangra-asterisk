package support

import (
	"context"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Batch is how many finished calls one ingest step classifies.
const Batch = 500

// Service ingests finished calls from the PBX into the Store and answers the
// support reports. Run follows the CEL table; the first run backfills the
// whole history, a batch at a time.
type Service struct {
	Source   *Source
	Store    *Store
	Log      *slog.Logger
	Groups   []string // support ring groups; empty: every ring group
	Exclude  []string // extensions (or "201-211" ranges) never in the team
	Callback time.Duration
	Idle     time.Duration // pause when caught up (default 30 s)

	mu      sync.RWMutex
	routing Routing
	team    []string
	loaded  time.Time
}

// refresh reloads routing and team at most every ten minutes.
func (s *Service) refresh(ctx context.Context) {
	s.mu.RLock()
	fresh := time.Since(s.loaded) < 10*time.Minute
	s.mu.RUnlock()
	if fresh {
		return
	}
	r, groups, err := s.Source.Routing(ctx)
	if err != nil && s.Log != nil {
		s.Log.Warn("support routing unavailable; trunks told apart by name", "error", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routing, s.team, s.loaded = r, Team(groups, s.Groups, s.Exclude), time.Now()
}

// Team is the support team: members of the chosen ring groups (all groups
// when none is chosen), minus the excluded extensions, sorted.
func Team(groups Groups, chosen, exclude []string) []string {
	if len(chosen) == 0 {
		for g := range groups {
			chosen = append(chosen, g)
		}
	}
	skip := func(ext string) bool {
		n, err := strconv.Atoi(ext)
		for _, e := range exclude {
			lo, hi, isRange := strings.Cut(e, "-")
			if !isRange {
				if e == ext {
					return true
				}
				continue
			}
			a, e1 := strconv.Atoi(lo)
			b, e2 := strconv.Atoi(hi)
			if err == nil && e1 == nil && e2 == nil && n >= a && n <= b {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	var out []string
	for _, g := range chosen {
		for _, m := range groups[g] {
			if !seen[m] && !skip(m) {
				seen[m] = true
				out = append(out, m)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := strconv.Atoi(out[i])
		b, _ := strconv.Atoi(out[j])
		return a < b
	})
	return out
}

// Step ingests one batch and reports how many calls it read.
func (s *Service) Step(ctx context.Context) (int, error) {
	s.refresh(ctx)
	cursor, err := s.Store.Cursor(ctx)
	if err != nil {
		return 0, err
	}
	ids, last, err := s.Source.Finished(ctx, cursor, Batch)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	cdrs, cels, err := s.Source.Load(ctx, ids)
	if err != nil {
		return 0, err
	}
	s.mu.RLock()
	r := s.routing
	s.mu.RUnlock()
	calls := make([]Call, 0, len(ids))
	for _, id := range ids {
		calls = append(calls, Classify(r, cdrs[id], cels[id]))
	}
	return len(ids), s.Store.Save(ctx, calls, last)
}

// Run ingests until ctx ends: back to back while behind, then every Idle.
func (s *Service) Run(ctx context.Context) {
	idle := s.Idle
	if idle <= 0 {
		idle = 30 * time.Second
	}
	for {
		n, err := s.Step(ctx)
		wait := time.Duration(0)
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			if s.Log != nil {
				s.Log.Warn("support ingest failed; retrying", "error", err)
			}
			wait = 2 * idle
		case n < Batch:
			wait = idle
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		} else if ctx.Err() != nil {
			return
		}
	}
}

// TeamMembers returns the current support team.
func (s *Service) TeamMembers(ctx context.Context) []string {
	s.refresh(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.team...)
}

// Ingested is when the ingest cursor last moved.
func (s *Service) Ingested(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := s.Store.DB.QueryRowContext(ctx, "SELECT updated_at FROM support_ingest WHERE id=1").Scan(&t)
	return t, err
}

// CallbackWindow is how long after a lost call a callback counts.
func (s *Service) CallbackWindow() time.Duration { return s.Callback }

// Report loads [from,to) plus the callback window after it.
func (s *Service) Report(ctx context.Context, tenant string, from, to time.Time) ([]Call, error) {
	return s.Store.Period(ctx, tenant, from, to.Add(s.Callback))
}
