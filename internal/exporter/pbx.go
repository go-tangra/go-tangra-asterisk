package exporter

import (
	"context"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type point struct {
	Value  float64
	Labels map[string]string
	At     time.Time
}

var pbxMetrics = []string{"asterisk_info", "asterisk_uptime_seconds", "asterisk_last_reload_seconds", "asterisk_current_calls", "asterisk_sip_peer_up", "asterisk_sip_peer_latency_milliseconds", "asterisk_sip_peers", "asterisk_pjsip_endpoint_up", "asterisk_pjsip_endpoints", "asterisk_queue_callers", "asterisk_queue_completed_calls", "asterisk_queue_abandoned_calls", "asterisk_queue_members", "asterisk_scrape_duration_seconds"}
var latency = regexp.MustCompile(`\((\d+)\s*ms\)`)

func (c *Collector) BeginSweep() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sweepStart = time.Now()
	c.pending = map[string][]map[string]string{}
}
func (c *Collector) Frame(m map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	put := func(name string, v float64, labels map[string]string) {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return
		}
		c.pbx[name] = append(c.pbx[name], point{v, labels, now})
		if len(c.pbx[name]) > 10000 {
			c.pbx[name] = c.pbx[name][len(c.pbx[name])-10000:]
		}
	}
	number := func(name, raw string, labels map[string]string) {
		if v, e := strconv.ParseFloat(raw, 64); e == nil {
			put(name, v, labels)
		}
	}
	switch m["ActionID"] {
	case "metrics-CoreSettings":
		if m["Response"] == "Success" {
			c.pbx["asterisk_info"] = nil
			version := m["AsteriskVersion"]
			if version == "" {
				version = m["Version"]
			}
			put("asterisk_info", 1, map[string]string{"version": version, "system_name": m["SystemName"]})
		}
	case "metrics-CoreStatus":
		if m["Response"] == "Success" {
			for _, n := range []string{"asterisk_current_calls", "asterisk_uptime_seconds", "asterisk_last_reload_seconds"} {
				c.pbx[n] = nil
			}
			number("asterisk_current_calls", m["CoreCurrentCalls"], nil)
			loc := c.SourceLocation
			if loc == nil {
				loc = time.UTC
			}
			for field, name := range map[string]string{"CoreStartup": "asterisk_uptime_seconds", "CoreReload": "asterisk_last_reload_seconds"} {
				if at, e := time.ParseInLocation("2006-01-02 15:04:05", m[field+"Date"]+" "+m[field+"Time"], loc); e == nil && !at.After(now) {
					put(name, now.Sub(at).Seconds(), nil)
				}
			}
		}
	}
	kind := m["Event"]
	phase := ""
	switch kind {
	case "EndpointList":
		phase = "endpoints"
	case "PeerEntry":
		phase = "peers"
	case "QueueParams", "QueueMember":
		phase = "queues"
	}
	if phase != "" && len(c.pending[phase]) < 10000 {
		copy := map[string]string{}
		for k, v := range m {
			copy[k] = v
		}
		c.pending[phase] = append(c.pending[phase], copy)
		return
	}
	switch kind {
	case "EndpointListComplete":
		for _, n := range []string{"asterisk_pjsip_endpoint_up", "asterisk_pjsip_endpoints"} {
			c.pbx[n] = nil
		}
		counts := map[string]int{}
		for _, e := range c.pending["endpoints"] {
			id := e["ObjectName"]
			if id == "" {
				id = e["Endpoint"]
			}
			state := e["DeviceState"]
			kind := "trunk"
			numeric := id != ""
			for _, r := range id {
				if r < '0' || r > '9' {
					numeric = false
				}
			}
			if e["OutboundAuths"] == "" && (e["Auths"] != "" || numeric) {
				kind = "extension"
			}
			up := 0.0
			switch state {
			case "Not in use", "In use", "Busy", "Ringing", "Ring", "On Hold":
				up = 1
			}
			put("asterisk_pjsip_endpoint_up", up, map[string]string{"endpoint": id, "extension": id, "device_state": state, "kind": kind})
			counts[state+"\x00"+kind]++
		}
		for key, n := range counts {
			state, kind, _ := strings.Cut(key, "\x00")
			put("asterisk_pjsip_endpoints", float64(n), map[string]string{"device_state": state, "kind": kind})
		}
		delete(c.pending, "endpoints")
	case "PeerlistComplete":
		for _, n := range []string{"asterisk_sip_peer_up", "asterisk_sip_peer_latency_milliseconds", "asterisk_sip_peers"} {
			c.pbx[n] = nil
		}
		counts := map[string]int{}
		for _, e := range c.pending["peers"] {
			peer := e["ObjectName"]
			if peer == "" {
				peer = e["Name"]
			}
			status := strings.ToUpper(strings.Fields(e["Status"] + " UNKNOWN")[0])
			up := 0.0
			if status == "OK" {
				up = 1
			}
			put("asterisk_sip_peer_up", up, map[string]string{"peer": peer, "status": status})
			if m := latency.FindStringSubmatch(e["Status"]); len(m) == 2 {
				number("asterisk_sip_peer_latency_milliseconds", m[1], map[string]string{"peer": peer})
			}
			counts[status]++
		}
		for status, n := range counts {
			put("asterisk_sip_peers", float64(n), map[string]string{"status": status})
		}
		delete(c.pending, "peers")
	case "QueueStatusComplete":
		for _, n := range []string{"asterisk_queue_callers", "asterisk_queue_completed_calls", "asterisk_queue_abandoned_calls", "asterisk_queue_members"} {
			c.pbx[n] = nil
		}
		counts := map[string]int{}
		statuses := map[string]string{"0": "unknown", "1": "not_in_use", "2": "in_use", "3": "busy", "4": "invalid", "5": "unavailable", "6": "ringing", "7": "ringinuse", "8": "on_hold"}
		for _, e := range c.pending["queues"] {
			q := e["Queue"]
			if e["Event"] == "QueueParams" {
				for field, name := range map[string]string{"Calls": "asterisk_queue_callers", "Completed": "asterisk_queue_completed_calls", "Abandoned": "asterisk_queue_abandoned_calls"} {
					number(name, e[field], map[string]string{"queue": q})
				}
			} else {
				status := statuses[e["Status"]]
				if status == "" {
					status = "unknown"
				}
				counts[q+"\x00"+status]++
			}
		}
		for key, n := range counts {
			q, status, _ := strings.Cut(key, "\x00")
			put("asterisk_queue_members", float64(n), map[string]string{"queue": q, "status": status})
		}
		delete(c.pending, "queues")
		c.pbx["asterisk_scrape_duration_seconds"] = nil
		put("asterisk_scrape_duration_seconds", time.Since(c.sweepStart).Seconds(), nil)
	}
}
func (c *Collector) RegisterPBX(m metric.Meter) (metric.Registration, error) {
	gauges := map[string]metric.Float64ObservableGauge{}
	instruments := []metric.Observable{}
	for _, name := range pbxMetrics {
		g, e := m.Float64ObservableGauge(name)
		if e != nil {
			return nil, e
		}
		gauges[name] = g
		instruments = append(instruments, g)
	}
	up, e := m.Int64ObservableGauge("asterisk_up")
	if e != nil {
		return nil, e
	}
	active, e := m.Int64ObservableGauge("asterisk_calls_active")
	if e != nil {
		return nil, e
	}
	channels, e := m.Int64ObservableGauge("asterisk_channels_active")
	if e != nil {
		return nil, e
	}
	byState, e := m.Int64ObservableGauge("asterisk_channels_by_state")
	if e != nil {
		return nil, e
	}
	rtt, e := m.Float64ObservableGauge("asterisk_pjsip_contact_rtt_milliseconds")
	if e != nil {
		return nil, e
	}
	instruments = append(instruments, up, active, channels, byState, rtt)
	return m.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		s := c.Registry.Snapshot()
		n := int64(0)
		if s.Fresh {
			n = 1
		}
		o.ObserveInt64(up, n)
		o.ObserveInt64(active, int64(len(s.Calls)))
		total := 0
		states := map[string]int{}
		for _, call := range s.Calls {
			for _, ch := range call.Channels {
				total++
				states[ch.State]++
			}
		}
		o.ObserveInt64(channels, int64(total))
		for state, n := range states {
			o.ObserveInt64(byState, int64(n), metric.WithAttributes(attribute.String("state", state)))
		}
		if !s.Fresh {
			return nil
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		for name, points := range c.pbx {
			for _, p := range points {
				if time.Since(p.At) > 2*time.Minute {
					continue
				}
				attrs := []attribute.KeyValue{}
				for k, v := range p.Labels {
					attrs = append(attrs, attribute.String(k, v))
				}
				o.ObserveFloat64(gauges[name], p.Value, metric.WithAttributes(attrs...))
			}
		}
		for _, e := range c.contacts {
			if e.RTT > 0 {
				o.ObserveFloat64(rtt, float64(e.RTT)/1000, metric.WithAttributes(attribute.String("endpoint", e.Endpoint), attribute.String("aor", e.AOR)))
			}
		}
		return nil
	}, instruments...)
}
