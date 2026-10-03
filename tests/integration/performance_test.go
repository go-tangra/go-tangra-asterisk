//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"os"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestReferencePerformance(t *testing.T) {
	_, p, admin := fixture(t)
	seed(t, admin, 100000)
	r := &cdr.Repository{Pools: p}
	loc, _ := time.LoadLocation("Europe/Sofia")
	reports := &stats.Repository{CDR: r, Location: loc}
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	durations := []time.Duration{}
	var wg sync.WaitGroup
	for viewer := 0; viewer < 10; viewer++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				start := time.Now()
				_, e := r.List(context.Background(), "t", cdr.Filter{From: from, To: from.Add(24 * time.Hour), Page: 1, PageSize: 25, Sort: "start", Order: "desc"})
				if e == nil {
					_, e = reports.Overview(context.Background(), "t", from, from.Add(24*time.Hour), "day")
				}
				elapsed := time.Since(start)
				if e != nil {
					t.Error(e)
				}
				mu.Lock()
				durations = append(durations, elapsed)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95+99)/100-1]
	registry := calls.New()
	registry.Reset(true)
	for i := 0; i < 50; i++ {
		registry.Apply(map[string]string{"Event": "Newchannel", "Uniqueid": fmtID(i), "Linkedid": fmtID(i)}, registry.Snapshot().Generation)
	}
	start := time.Now()
	snapshot := registry.Snapshot()
	snapshotTime := time.Since(start)
	if len(snapshot.Calls) != 50 {
		t.Fatal("incomplete reference snapshot")
	}
	result := map[string]any{"go": runtime.Version(), "arch": runtime.GOARCH, "cpus": runtime.NumCPU(), "legs": 100000, "activeCalls": 50, "viewers": 10, "historyAndReportP95Ms": p95.Milliseconds(), "registrySnapshotMs": snapshotTime.Milliseconds(), "portalReceiptToDisplay": "requires browser acceptance"}
	raw, _ := json.MarshalIndent(result, "", "  ")
	t.Log(string(raw))
	if path := os.Getenv("ASTERISK_BENCHMARK_OUTPUT"); path != "" {
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if p95 > 2*time.Second {
		t.Fatalf("SC-004 p95 %s exceeds 2s", p95)
	}
}
