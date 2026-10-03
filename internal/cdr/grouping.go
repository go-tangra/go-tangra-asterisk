package cdr

import (
	"sort"
	"time"
)

type Leg struct {
	LinkedID     string    `json:"linkedid"`
	UniqueID     string    `json:"uniqueid"`
	Sequence     int64     `json:"sequence"`
	Start        time.Time `json:"start"`
	Channel      string    `json:"channel"`
	DstChannel   string    `json:"dstChannel"`
	Src          string    `json:"src"`
	Dst          string    `json:"dst"`
	Disposition  string    `json:"disposition"`
	Duration     int64     `json:"durationSeconds"`
	Talk         int64     `json:"talkSeconds"`
	Recording    string    `json:"-"`
	LocalQuality *RTPQoS   `json:"localQuality,omitempty"`
	PeerQuality  *RTPQoS   `json:"peerQuality,omitempty"`
}
type Event struct {
	Time     time.Time `json:"time"`
	Type     string    `json:"type"`
	Channel  string    `json:"channel"`
	UniqueID string    `json:"uniqueid"`
}
type Call struct {
	LinkedID             string    `json:"linkedid"`
	Start                time.Time `json:"start"`
	Src                  string    `json:"src"`
	Dst                  string    `json:"dst"`
	Direction            string    `json:"direction"`
	Disposition          string    `json:"disposition"`
	Duration             int64     `json:"durationSeconds"`
	Talk                 int64     `json:"talkSeconds"`
	Pickup               *float64  `json:"pickupSeconds"`
	OriginatingExtension string    `json:"originatingExtension"`
	AnsweredExtension    string    `json:"answeredExtension"`
	LegCount             int       `json:"legCount"`
	HasRecording         bool      `json:"hasRecording"`
	Recording            string    `json:"-"`
	Extensions           []string  `json:"extensions"`
	Destinations         []string  `json:"destinations"`
}
type Detail struct {
	Summary          Call    `json:"summary"`
	Legs             []Leg   `json:"legs"`
	Timeline         []Event `json:"timeline"`
	CELAvailable     bool    `json:"celAvailable"`
	QualityAvailable bool    `json:"qualityAvailable"`
	Registration     any     `json:"registration,omitempty"`
}

func Group(input []Leg, events []Event) Call {
	legs := append([]Leg(nil), input...)
	sort.SliceStable(legs, func(i, j int) bool {
		if !legs[i].Start.Equal(legs[j].Start) {
			return legs[i].Start.Before(legs[j].Start)
		}
		if legs[i].Sequence != legs[j].Sequence {
			return legs[i].Sequence < legs[j].Sequence
		}
		if legs[i].UniqueID != legs[j].UniqueID {
			return legs[i].UniqueID < legs[j].UniqueID
		}
		return legs[i].Channel < legs[j].Channel
	})
	c := Call{LegCount: len(legs), Disposition: "NO ANSWER", Direction: "unknown", Extensions: []string{}, Destinations: []string{}}
	if len(legs) == 0 {
		return c
	}
	f := legs[0]
	c.LinkedID = f.LinkedID
	c.Start = f.Start.UTC()
	c.Src = f.Src
	c.Dst = f.Dst
	c.OriginatingExtension = ExtractExtension(f.Channel)
	dstExt := ExtractExtension(f.DstChannel)
	switch {
	case c.OriginatingExtension != "" && dstExt != "" && c.OriginatingExtension != dstExt:
		c.Direction = "internal"
	case c.OriginatingExtension == "" && dstExt != "":
		c.Direction = "inbound"
	case c.OriginatingExtension != "" && dstExt == "":
		c.Direction = "outbound"
	}
	rank := map[string]int{"NO ANSWER": 0, "FAILED": 1, "BUSY": 2, "ANSWERED": 3}
	seen := map[string]bool{}
	dests := map[string]bool{}
	var answered string
	for _, l := range legs {
		if rank[l.Disposition] > rank[c.Disposition] {
			c.Disposition = l.Disposition
		}
		if l.Duration > c.Duration {
			c.Duration = l.Duration
		}
		if l.Disposition == "ANSWERED" {
			if answered == "" {
				answered = l.UniqueID
				c.AnsweredExtension = ExtractExtension(l.DstChannel)
				if c.AnsweredExtension == "" {
					c.AnsweredExtension = ExtractExtension(l.Channel)
				}
			}
			if l.Talk > c.Talk {
				c.Talk = l.Talk
			}
		}
		if c.Recording == "" && l.Recording != "" {
			c.Recording = l.Recording
			c.HasRecording = true
		}
		for _, ext := range []string{ExtractExtension(l.Channel), ExtractExtension(l.DstChannel)} {
			if ext != "" && !seen[ext] {
				seen[ext] = true
				c.Extensions = append(c.Extensions, ext)
			}
		}
		if !dests[l.Dst] {
			dests[l.Dst] = true
			c.Destinations = append(c.Destinations, l.Dst)
		}
	}
	var start, answer time.Time
	for _, e := range events {
		if e.UniqueID != answered {
			continue
		}
		if e.Type == "CHAN_START" && (start.IsZero() || e.Time.Before(start)) {
			start = e.Time
		}
		if e.Type == "ANSWER" && (answer.IsZero() || e.Time.Before(answer)) {
			answer = e.Time
		}
	}
	if !start.IsZero() && !answer.Before(start) && !answer.IsZero() {
		v := answer.Sub(start).Seconds()
		c.Pickup = &v
	}
	return c
}
