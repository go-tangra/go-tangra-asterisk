package registration

import (
	"testing"
	"time"
)

func TestBuildTimeline(t *testing.T) {
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	h := func(n float64) time.Time { return day.Add(time.Duration(n * float64(time.Hour))) }
	events := []Event{
		// Registered the evening before, expiring at 01:00 without a refresh.
		{ID: 1, Time: h(-2), Endpoint: "49", Contact: "sip:a", Status: "Created", Expire: h(1)},
		{ID: 2, Time: h(8), Endpoint: "49", Contact: "sip:a", Status: "Created", UserAgent: "Yealink", ViaAddress: "192.168.16.4"},
		{ID: 3, Time: h(12), Endpoint: "49", Contact: "sip:a", Status: "Unreachable"},
		{ID: 4, Time: h(12.5), Endpoint: "49", Contact: "sip:a", Status: "Reachable", UserAgent: "Yealink"},
		{ID: 5, Time: h(18), Endpoint: "49", Contact: "sip:a", Status: "Removed"},
		{ID: 6, Time: h(9), Endpoint: "54", Contact: "sip:other", Status: "Created"},
	}
	gaps := []Gap{{Start: h(20), End: h(21)}}
	tl := BuildTimeline("49", day, h(24), events, gaps)
	want := []struct {
		from, to float64
		state    string
	}{{0, 1, Online}, {1, 8, Offline}, {8, 12, Online}, {12, 12.5, Offline}, {12.5, 18, Online}, {18, 20, Offline}, {20, 21, Unknown}, {21, 24, Offline}}
	if len(tl.Segments) != len(want) {
		t.Fatalf("segments: %+v", tl.Segments)
	}
	for i, w := range want {
		s := tl.Segments[i]
		if !s.Start.Equal(h(w.from)) || !s.End.Equal(h(w.to)) || s.State != w.state {
			t.Fatalf("segment %d = %s %s..%s, want %s %v..%v", i, s.State, s.Start, s.End, w.state, w.from, w.to)
		}
	}
	if s := tl.Segments[2]; s.Contact != "sip:a" || s.UserAgent != "Yealink" || s.ViaAddress != "192.168.16.4" {
		t.Fatalf("online segment lost its contact: %+v", s)
	}
	sum := tl.Summary
	if sum.OnlineSeconds != 37800 || sum.OfflineSeconds != 45000 || sum.UnknownSeconds != 3600 {
		t.Fatalf("summary totals: %+v", sum)
	}
	if sum.Dropouts != 3 || sum.LongestOfflineSeconds != 7*3600 {
		t.Fatalf("summary dropouts/longest: %+v", sum)
	}
	if len(tl.Gaps) != 1 {
		t.Fatalf("gaps: %+v", tl.Gaps)
	}
}

func TestBuildTimelineUnobservedAndOpenGap(t *testing.T) {
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	tl := BuildTimeline("49", day, day.Add(4*time.Hour), []Event{{ID: 1, Time: day.Add(time.Hour), Endpoint: "49", Contact: "sip:a", Status: "Reachable"}}, []Gap{{Start: day.Add(3 * time.Hour)}})
	got := []string{}
	for _, s := range tl.Segments {
		got = append(got, s.State)
	}
	// Nothing known before the first event; the open gap runs to the end.
	if len(got) != 3 || got[0] != Unknown || got[1] != Online || got[2] != Unknown || !tl.Segments[2].End.Equal(day.Add(4*time.Hour)) {
		t.Fatalf("segments: %+v", tl.Segments)
	}
	if tl.Summary.Dropouts != 0 || tl.Summary.OfflineSeconds != 0 {
		t.Fatalf("unknown counted as offline: %+v", tl.Summary)
	}
}

func TestBuildTimelineMultipleContacts(t *testing.T) {
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	events := []Event{
		{ID: 1, Time: day, Endpoint: "49", Contact: "sip:desk", Status: "Created"},
		{ID: 2, Time: day.Add(time.Hour), Endpoint: "49", Contact: "sip:softphone", Status: "Created"},
		{ID: 3, Time: day.Add(2 * time.Hour), Endpoint: "49", Contact: "sip:softphone", Status: "Removed"},
	}
	tl := BuildTimeline("49", day, day.Add(3*time.Hour), events, nil)
	// Online throughout: the desk phone stays registered while the softphone
	// comes and goes; the newest registered contact names each stretch.
	if tl.Summary.OnlineSeconds != 3*3600 || tl.Summary.Dropouts != 0 || len(tl.Segments) != 3 || tl.Segments[1].Contact != "sip:softphone" || tl.Segments[2].Contact != "sip:desk" {
		t.Fatalf("timeline: %+v", tl)
	}
}

func TestBuildTimelineEmptyPeriod(t *testing.T) {
	now := time.Now()
	if tl := BuildTimeline("49", now, now, nil, nil); len(tl.Segments) != 0 || tl.Segments == nil || tl.Gaps == nil {
		t.Fatalf("empty period: %+v", tl)
	}
}
