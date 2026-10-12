package support

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestSurveySample classifies a whole day exported by tools/phase0/survey.sh
// (SUPPORT_SAMPLE=dir/cdr-<day>.tsv; the cel file is found next to it) and
// prints the outcome counts. It never runs in CI: survey exports hold real
// calls and stay off the repository.
func TestSurveySample(t *testing.T) {
	path := os.Getenv("SUPPORT_SAMPLE")
	if path == "" {
		t.Skip("SUPPORT_SAMPLE not set")
	}
	dir, name := filepath.Split(path)
	tmp := t.TempDir()
	for src, dst := range map[string]string{path: "cdr.tsv", filepath.Join(dir, "cel"+name[3:]): "cel.tsv"} {
		raw, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(tmp, dst), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cdrs, cels := loadGolden(t, tmp)
	byCall := map[string][]CELRow{}
	for _, e := range cels {
		byCall[e.LinkedID] = append(byCall[e.LinkedID], e)
	}
	rowsByCall := map[string][]CDRRow{}
	for _, r := range cdrs {
		rowsByCall[r.LinkedID] = append(rowsByCall[r.LinkedID], r)
	}
	counts := map[string]int{}
	legs, sides, grades := map[string]int{}, map[string]int{}, map[string]int{}
	for id, evs := range byCall {
		c := Classify(goldenRouting, rowsByCall[id], evs)
		counts[c.Kind+" "+c.Outcome+" "+c.LostReason+c.Destination]++
		for _, l := range c.Legs {
			legs[l.Outcome]++
		}
		if c.Outcome == OutcomeAnswered || c.Outcome == OutcomeConnected {
			sides[c.Kind+"/"+c.QualitySide]++
			grades[Grade(c.Quality)]++
		}
	}
	var keys []string
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%5d %s", counts[k], k)
	}
	t.Logf("agent legs: %v", legs)
	t.Logf("quality measured on answered/connected calls: %v, grades %v", sides, grades)
}
