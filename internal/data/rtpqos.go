package data

import (
	"strconv"
	"strings"
)

// RTPQoS is a parsed view of Asterisk's CDR rtpqos column. Asterisk
// emits a semicolon-delimited key=value string with raw counters; we
// extract the operator-relevant fields and compute a MOS estimate.
//
// Example raw value:
//   ssrc=894967153;themssrc=3084395988;lp=1;rxjitter=0.002375;
//   rxcount=1808;txjitter=0.000625;txcount=1828;rlp=0;
//   rtt=0.017517;rxmes=87.921944;txmes=87.805427
//
// Field semantics (from Asterisk's rtp_engine.c):
//   - lp    : packets we lost on receive (sender's perspective)
//   - rlp   : packets the remote lost (their reports back to us)
//   - rxjit : jitter on the receive stream, seconds (RFC 3550)
//   - txjit : jitter on the transmit stream, seconds
//   - rxcount/txcount : packets received / sent
//   - rtt   : round-trip time in seconds (RTCP-derived)
//   - rxmes/txmes : Asterisk's MES (Media Experience Score), an
//     R-factor-style 0–100 quality estimate for each direction.
//     Higher = better. Empty/absent when RTCP wasn't received from
//     the peer.
type RTPQoS struct {
	Raw string

	// Lossy-path stats (operator units: ms / count).
	RxJitterMs    float64
	TxJitterMs    float64
	RTTMs         float64
	RxLoss        int64
	TxLoss        int64
	RxCount       int64
	TxCount       int64
	RxLossPercent float64
	TxLossPercent float64

	// Quality estimates. *Mes are the raw 0–100 values from Asterisk;
	// *MOS are derived 1.0–4.5 scores using the standard R-factor →
	// MOS conversion. Both directions are kept because asymmetric
	// networks regularly report wildly different rx vs tx scores.
	RxMes float64
	TxMes float64
	RxMOS float64
	TxMOS float64

	// Quality is the overall band — picks the worst of rx/tx, since
	// any direction being bad makes the call bad. Empty when neither
	// side reported a Mes value.
	Quality QualityBand
}

// QualityBand is a coarse, operator-friendly quality label.
type QualityBand string

const (
	QualityUnknown   QualityBand = ""
	QualityExcellent QualityBand = "EXCELLENT"
	QualityGood      QualityBand = "GOOD"
	QualityFair      QualityBand = "FAIR"
	QualityPoor      QualityBand = "POOR"
	QualityBad       QualityBand = "BAD"
)

// ParseRTPQoS turns the semicolon-key=value string into a struct.
// Returns nil when the input is empty or unparseable. Unknown keys
// are silently ignored — Asterisk versions occasionally add or rename
// fields and we don't want a single new key to make us drop the row.
func ParseRTPQoS(raw string) *RTPQoS {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	q := &RTPQoS{Raw: raw}
	for _, pair := range strings.Split(raw, ";") {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(pair[:eq])
		val := strings.TrimSpace(pair[eq+1:])
		if val == "" {
			continue
		}
		switch key {
		case "lp":
			q.RxLoss, _ = strconv.ParseInt(val, 10, 64)
		case "rlp":
			q.TxLoss, _ = strconv.ParseInt(val, 10, 64)
		case "rxjitter":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				q.RxJitterMs = v * 1000
			}
		case "txjitter":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				q.TxJitterMs = v * 1000
			}
		case "rxcount":
			q.RxCount, _ = strconv.ParseInt(val, 10, 64)
		case "txcount":
			q.TxCount, _ = strconv.ParseInt(val, 10, 64)
		case "rtt":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				q.RTTMs = v * 1000
			}
		case "rxmes":
			q.RxMes, _ = strconv.ParseFloat(val, 64)
		case "txmes":
			q.TxMes, _ = strconv.ParseFloat(val, 64)
		}
	}

	// Loss percentages — saturate the denominator so a brief silent
	// stream with 0 packets doesn't blow up to NaN.
	if total := q.RxCount + q.RxLoss; total > 0 {
		q.RxLossPercent = 100 * float64(q.RxLoss) / float64(total)
	}
	if total := q.TxCount + q.TxLoss; total > 0 {
		q.TxLossPercent = 100 * float64(q.TxLoss) / float64(total)
	}

	q.RxMOS = mesToMOS(q.RxMes)
	q.TxMOS = mesToMOS(q.TxMes)
	q.Quality = quality(q.RxMOS, q.TxMOS)
	return q
}

// mesToMOS converts Asterisk's 0–100 MES (effectively an R-factor) to
// a standard 1.0–4.5 MOS using the ITU-T G.107 E-Model approximation.
// Returns 0 when mes is 0 (no RTCP report received).
func mesToMOS(mes float64) float64 {
	if mes <= 0 {
		return 0
	}
	if mes >= 100 {
		return 4.5
	}
	if mes <= 6.5 {
		return 1.0
	}
	mos := 1 + 0.035*mes + mes*(mes-60)*(100-mes)*7e-6
	if mos < 1.0 {
		return 1.0
	}
	if mos > 4.5 {
		return 4.5
	}
	return mos
}

// quality picks the worst of the two directional MOS scores and maps
// to a band. Worst-of is correct because the operator and caller hear
// different streams — if either direction is bad, someone heard a
// bad call.
func quality(rx, tx float64) QualityBand {
	worst := rx
	if tx > 0 && (worst == 0 || tx < worst) {
		worst = tx
	}
	if worst == 0 {
		return QualityUnknown
	}
	switch {
	case worst >= 4.3:
		return QualityExcellent
	case worst >= 4.0:
		return QualityGood
	case worst >= 3.6:
		return QualityFair
	case worst >= 3.1:
		return QualityPoor
	default:
		return QualityBad
	}
}
