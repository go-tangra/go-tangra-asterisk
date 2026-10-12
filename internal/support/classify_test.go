package support

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var goldenRouting = Routing{
	Trunks:        map[string]bool{"ITD": true, "Kamailio": true, "elevenlabs": true},
	Announcements: map[string]string{"1": "600"},
	GroupTime:     map[string]int{"600": 300},
}

// readTSV reads a survey export (tools/phase0/survey.sh) into header-keyed rows.
func readTSV(t *testing.T, path string) []map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.Comma, r.LazyQuotes, r.FieldsPerRecord = '\t', true, -1
	recs, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]string
	for _, rec := range recs[1:] {
		m := map[string]string{}
		for i, k := range recs[0] {
			if i < len(rec) {
				m[k] = rec[i]
			}
		}
		out = append(out, m)
	}
	return out
}

func at(t *testing.T, s string) time.Time {
	v, err := time.Parse(time.DateTime, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func loadGolden(t *testing.T, dir string) ([]CDRRow, []CELRow) {
	var cdrs []CDRRow
	for _, m := range readTSV(t, filepath.Join(dir, "cdr.tsv")) {
		cdrs = append(cdrs, CDRRow{LinkedID: m["linkedid"], UniqueID: m["uniqueid"], Sequence: ParseInt(m["sequence"]), Start: at(t, m["calldate"]), Channel: m["channel"], DstChannel: m["dstchannel"], Src: m["src"], Dst: m["dst"], DContext: m["dcontext"], LastApp: m["lastapp"], Disposition: m["disposition"], Duration: ParseInt(m["duration"]), Billsec: ParseInt(m["billsec"]), Recorded: m["recorded"] == "1", RTPQoS: m["rtpqos"], PeerRTPQoS: m["peerrtpqos"]})
	}
	var cels []CELRow
	for _, m := range readTSV(t, filepath.Join(dir, "cel.tsv")) {
		cels = append(cels, CELRow{ID: ParseInt(m["id"]), Type: m["eventtype"], Time: at(t, m["eventtime"]), CIDNum: m["cid_num"], Exten: m["exten"], Context: m["context"], Channel: m["channame"], App: m["appname"], AppData: m["appdata"], UniqueID: m["uniqueid"], LinkedID: m["linkedid"], Peer: m["peer"], Extra: m["extra"]})
	}
	return cdrs, cels
}

// observed renders a Call with the field names expected.json uses.
func observed(c Call) map[string]any {
	m := map[string]any{"kind": c.Kind, "group": c.Group, "outcome": c.Outcome, "lostReason": c.LostReason, "agent": c.Agent, "trunk": c.Trunk, "dialStatus": c.DialStatus, "destination": c.Destination}
	for k, v := range map[string]*int64{"announcementSeconds": c.Announcement, "ringSeconds": c.Ring, "callerWaitSeconds": c.CallerWait, "talkSeconds": c.Talk, "setupSeconds": c.Setup} {
		if v != nil {
			m[k] = float64(*v)
		}
	}
	legs := map[string]any{}
	for _, l := range c.Legs {
		legs[l.Extension] = l.Outcome
	}
	m["legs"] = legs
	return m
}

// TestGolden checks every fixture of docs/support-metrics.md: each key of
// expected.json must match what Classify derives ("reason" is prose).
func TestGolden(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/golden/*")
	if len(dirs) < 8 {
		t.Fatalf("expected the phase 0 fixtures, found %d", len(dirs))
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(dir, "expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			var want map[string]any
			if err = json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			cdrs, cels := loadGolden(t, dir)
			got := observed(Classify(goldenRouting, cdrs, cels))
			for k, w := range want {
				if k == "reason" {
					continue
				}
				if fmt.Sprint(got[k]) != fmt.Sprint(w) {
					t.Errorf("%s: got %v, want %v", k, got[k], w)
				}
			}
		})
	}
}

func TestDeclinedNeedsOwnHangup(t *testing.T) {
	cdrs, cels := loadGolden(t, "testdata/golden/group-caller-hung-up")
	if l := leg(Classify(goldenRouting, cdrs, cels), "41"); l.Outcome != LegDeclined || l.Ring != 4 {
		t.Fatalf("41 declined after 4 s of ringing, got %+v", l)
	}
	for i := range cels { // the same cause from the caller's side is not a decline
		cels[i].Extra = `{"hangupcause":17,"hangupsource":"","dialstatus":""}`
	}
	for _, l := range Classify(goldenRouting, cdrs, cels).Legs {
		if l.Outcome == LegDeclined {
			t.Fatalf("%s declined without hanging up itself", l.Extension)
		}
	}
}

func TestRangOut(t *testing.T) {
	cdrs, cels := loadGolden(t, "testdata/golden/group-caller-hung-up")
	r := goldenRouting
	r.GroupTime = map[string]int{"600": 80}
	c := Classify(r, cdrs, cels)
	if c.LostReason != LostRangOut || leg(c, "40").Outcome != LegRangOut || leg(c, "41").Outcome != LegDeclined {
		t.Fatalf("76 s of an 80 s group is a ring-out: %+v", c)
	}
}

func leg(c Call, ext string) AgentLeg {
	for _, l := range c.Legs {
		if l.Extension == ext {
			return l
		}
	}
	return AgentLeg{}
}
