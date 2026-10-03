package cdr

import (
	"math"
	"strconv"
	"strings"
)

type RTPQoS struct {
	RxJitterMs    *float64 `json:"rxJitterMs"`
	TxJitterMs    *float64 `json:"txJitterMs"`
	RTTMs         *float64 `json:"rttMs"`
	RxLoss        *float64 `json:"rxLoss"`
	TxLoss        *float64 `json:"txLoss"`
	RxCount       *float64 `json:"rxCount"`
	TxCount       *float64 `json:"txCount"`
	RxLossPercent *float64 `json:"rxLossPercent"`
	TxLossPercent *float64 `json:"txLossPercent"`
	RxMES         *float64 `json:"rxMes"`
	TxMES         *float64 `json:"txMes"`
	RxMOS         *float64 `json:"rxMos"`
	TxMOS         *float64 `json:"txMos"`
	Quality       string   `json:"quality"`
}

func ParseRTPQoS(raw string) *RTPQoS {
	q := &RTPQoS{}
	valid := false
	fields := map[string]**float64{"rxjitter": &q.RxJitterMs, "txjitter": &q.TxJitterMs, "rtt": &q.RTTMs, "lp": &q.RxLoss, "rlp": &q.TxLoss, "rxcount": &q.RxCount, "txcount": &q.TxCount, "rxmes": &q.RxMES, "txmes": &q.TxMES}
	for _, pair := range strings.Split(raw, ";") {
		k, v, ok := strings.Cut(pair, "=")
		p := fields[strings.TrimSpace(k)]
		if !ok || p == nil {
			continue
		}
		n, e := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || ((k == "rxmes" || k == "txmes") && n > 100) {
			continue
		}
		if k == "rxjitter" || k == "txjitter" || k == "rtt" {
			n *= 1000
		}
		*p = &n
		valid = true
	}
	if !valid {
		return nil
	}
	for _, p := range []struct {
		loss, count *float64
		out         **float64
	}{{q.RxLoss, q.RxCount, &q.RxLossPercent}, {q.TxLoss, q.TxCount, &q.TxLossPercent}} {
		if p.loss != nil && p.count != nil && *p.loss+*p.count > 0 {
			n := 100 * *p.loss / (*p.loss + *p.count)
			*p.out = &n
		}
	}
	mos := func(m *float64) *float64 {
		if m == nil || *m == 0 {
			return nil
		}
		r := *m
		n := math.Max(1, math.Min(4.5, 1+0.035*r+r*(r-60)*(100-r)*7e-6))
		return &n
	}
	q.RxMOS = mos(q.RxMES)
	q.TxMOS = mos(q.TxMES)
	worst := 5.0
	for _, v := range []*float64{q.RxMOS, q.TxMOS} {
		if v != nil && *v < worst {
			worst = *v
		}
	}
	switch {
	case worst == 5:
		q.Quality = ""
	case worst >= 4.3:
		q.Quality = "EXCELLENT"
	case worst >= 4:
		q.Quality = "GOOD"
	case worst >= 3.6:
		q.Quality = "FAIR"
	case worst >= 3.1:
		q.Quality = "POOR"
	default:
		q.Quality = "BAD"
	}
	return q
}
