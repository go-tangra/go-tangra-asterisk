package support

import (
	"math"
	"sort"
	"time"

	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
)

// Measure is one call's RTP report reduced to its worst direction.
type Measure struct {
	LossPercent *float64 `json:"lossPercent"`
	JitterMs    *float64 `json:"jitterMs"`
	RTTMs       *float64 `json:"rttMs"`
	MOS         *float64 `json:"mos"`
}

func worst(a, b *float64, higherIsWorse bool) *float64 {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case (*a > *b) == higherIsWorse:
		return a
	default:
		return b
	}
}

// MeasureOf reads a raw rtpqos string; nil when it carries no measurement.
func MeasureOf(raw string) *Measure {
	q := cdr.ParseRTPQoS(raw)
	if q == nil {
		return nil
	}
	return &Measure{LossPercent: worst(q.RxLossPercent, q.TxLossPercent, true), JitterMs: worst(q.RxJitterMs, q.TxJitterMs, true), RTTMs: q.RTTMs, MOS: worst(q.RxMOS, q.TxMOS, false)}
}

// FSpread is a distribution of measurements: median and 90th percentile.
type FSpread struct {
	Median *float64 `json:"median"`
	P90    *float64 `json:"p90"`
}

func fspread(v []float64) FSpread {
	if len(v) == 0 {
		return FSpread{}
	}
	sort.Float64s(v)
	q := func(p float64) *float64 {
		i := max(int(math.Ceil(p*float64(len(v))))-1, 0)
		f := math.Round(v[i]*100) / 100
		return &f
	}
	return FSpread{q(0.5), q(0.9)}
}

// QualityGroup aggregates the measured calls of an agent, trunk or phone
// model.
type QualityGroup struct {
	Key    string  `json:"key"`
	Calls  int     `json:"calls"`
	Poor   int     `json:"poor"`
	Loss   FSpread `json:"lossPercent"`
	Jitter FSpread `json:"jitterMs"`
	RTT    FSpread `json:"rttMs"`
	MOS    FSpread `json:"mos"`
}

type group struct {
	calls, poor            int
	loss, jitter, rtt, mos []float64
}

func (g *group) add(m *Measure, poor bool) {
	g.calls++
	if poor {
		g.poor++
	}
	for _, p := range []struct {
		v   *float64
		dst *[]float64
	}{{m.LossPercent, &g.loss}, {m.JitterMs, &g.jitter}, {m.RTTMs, &g.rtt}, {m.MOS, &g.mos}} {
		if p.v != nil {
			*p.dst = append(*p.dst, *p.v)
		}
	}
}

func groups(m map[string]*group) []QualityGroup {
	out := make([]QualityGroup, 0, len(m))
	for k, g := range m {
		out = append(out, QualityGroup{Key: k, Calls: g.calls, Poor: g.poor, Loss: fspread(g.loss), Jitter: fspread(g.jitter), RTT: fspread(g.rtt), MOS: fspread(g.mos)})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Poor*b.Calls != b.Poor*a.Calls { // higher poor share first
			return a.Poor*b.Calls > b.Poor*a.Calls
		}
		if a.Calls != b.Calls {
			return a.Calls > b.Calls
		}
		return a.Key < b.Key
	})
	return out
}

// PoorCall is one call graded poor, for the worst-calls list.
type PoorCall struct {
	LinkedID string    `json:"linkedid"`
	Start    time.Time `json:"start"`
	Type     string    `json:"type"`
	Agent    string    `json:"agent"`
	Trunk    string    `json:"trunk"`
	Device   string    `json:"device"`
	Side     string    `json:"side"`
	Measure
}

// QualityReport is the call quality of a period, per measured side: the
// provider line (trunk side, inbound calls) and the handset (phone side,
// outbound calls) as FreePBX logs them.
type QualityReport struct {
	Trunk    QualityGroup   `json:"trunk"`
	Phone    QualityGroup   `json:"phone"`
	ByTrunk  []QualityGroup `json:"byTrunk"`  // trunk side
	ByAgent  []QualityGroup `json:"byAgent"`  // phone side
	ByDevice []QualityGroup `json:"byDevice"` // phone side, by the phone's user agent at call time
	Poor     []PoorCall     `json:"poorCalls"`
}

// Quality builds the report for calls answered or connected in [from,to).
// device names the phone an extension used at a time ("" when unknown).
func Quality(calls []Call, from, to time.Time, device func(ext string, at time.Time) string) QualityReport {
	trunk, phone := &group{}, &group{}
	byTrunk, byAgent, byDevice := map[string]*group{}, map[string]*group{}, map[string]*group{}
	at := func(m map[string]*group, k string) *group {
		if m[k] == nil {
			m[k] = &group{}
		}
		return m[k]
	}
	r := QualityReport{Poor: []PoorCall{}}
	for _, c := range calls {
		m := MeasureOf(c.Quality)
		if !within(c, from, to) || m == nil {
			continue
		}
		poor := Grade(c.Quality) == "poor"
		pc := PoorCall{LinkedID: c.LinkedID, Start: c.Start, Type: typeOf(c), Agent: c.Agent, Trunk: c.Trunk, Side: c.QualitySide, Measure: *m}
		switch c.QualitySide {
		case QualityTrunk:
			trunk.add(m, poor)
			at(byTrunk, c.Trunk).add(m, poor)
		case QualityPhone:
			phone.add(m, poor)
			at(byAgent, c.Agent).add(m, poor)
			if device != nil {
				pc.Device = device(c.Agent, c.Start)
			}
			name := pc.Device
			if name == "" {
				name = "Unknown"
			}
			at(byDevice, name).add(m, poor)
		default:
			continue
		}
		if poor {
			r.Poor = append(r.Poor, pc)
		}
	}
	r.Trunk = groups(map[string]*group{"trunk": trunk})[0]
	r.Phone = groups(map[string]*group{"phone": phone})[0]
	r.ByTrunk, r.ByAgent, r.ByDevice = groups(byTrunk), groups(byAgent), groups(byDevice)
	sort.Slice(r.Poor, func(i, j int) bool { return r.Poor[i].Start.After(r.Poor[j].Start) })
	if len(r.Poor) > 100 {
		r.Poor = r.Poor[:100]
	}
	return r
}
