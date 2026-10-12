package registration

import (
	"sort"
	"strings"
	"time"
)

type Event struct {
	ID         int64     `json:"id"`
	Time       time.Time `json:"time"`
	Endpoint   string    `json:"endpoint"`
	AOR        string    `json:"aor"`
	Contact    string    `json:"contact"`
	Status     string    `json:"status"`
	UserAgent  string    `json:"userAgent"`
	ViaAddress string    `json:"viaAddress"`
	Expire     time.Time `json:"expire"`
	RTT        int64     `json:"rttUsec"`
}
type Gap struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}
type Status struct {
	Extension  string     `json:"extension"`
	At         time.Time  `json:"at"`
	Status     string     `json:"status"`
	Registered bool       `json:"registered"`
	Certainty  string     `json:"certainty"`
	LastEvent  *Event     `json:"lastEvent"`
	ExpiresAt  *time.Time `json:"expiresAt"`
}

func Evaluate(ext string, at time.Time, input []Event, gaps []Gap) Status {
	events := append([]Event(nil), input...)
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Time.Equal(events[j].Time) {
			return events[i].ID < events[j].ID
		}
		return events[i].Time.Before(events[j].Time)
	})
	s := Status{Extension: ext, At: at.UTC(), Status: "unknown", Certainty: "unknown"}
	contacts := map[string]Event{}
	for _, e := range events {
		if e.Endpoint != ext || e.Time.After(at) {
			continue
		}
		e := e
		contacts[e.Contact] = e
		s.LastEvent = &e
		s.Status = e.Status
		s.Certainty = "observed"
	}
	for _, e := range contacts {
		if registeredAt(e, at) {
			s.Registered = true
			if !e.Expire.IsZero() && (s.ExpiresAt == nil || e.Expire.After(*s.ExpiresAt)) {
				v := e.Expire
				s.ExpiresAt = &v
			}
		}
	}
	for _, g := range gaps {
		if !at.Before(g.Start) && (g.End.IsZero() || at.Before(g.End)) {
			s.Certainty = "uncertain"
		}
	}
	if !s.Registered && s.LastEvent != nil && !s.LastEvent.Expire.IsZero() && !at.Before(s.LastEvent.Expire) {
		s.Status = "expired"
	}
	return s
}

// registeredAt reports whether a contact's last observation e means it was
// registered at t: a registering or reachable status that has not expired.
func registeredAt(e Event, t time.Time) bool {
	switch strings.ToLower(e.Status) {
	case "created", "updated", "reachable", "unqualified", "avail", "available":
		return e.Expire.IsZero() || t.Before(e.Expire)
	}
	return false
}
