// Package quality is a Prometheus collector that emits RTP-quality
// metrics aggregated from new cdr rows on each scrape. State is held
// in-memory: a high-watermark (lastSeen) advances monotonically, and
// counters/histograms are kept across scrapes so Prometheus can
// rate()/histogram_quantile() over them.
//
// Cardinality budget: only `direction` (rx/tx) and `band` labels.
// Per-extension or per-trunk labels would explode the series count
// on a busy PBX, so they live in CDR queries instead.
package quality

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// repo is the subset of *data.CdrRepo this package needs. Defined as
// an interface so the collector can be tested without a real MySQL.
type repo interface {
	ListLegsWithQoSSince(ctx context.Context, since time.Time) ([]data.CallLeg, error)
}

// Collector emits asterisk_call_* metrics from cdr.rtpqos.
type Collector struct {
	repo    repo
	timeout time.Duration
	logger  *slog.Logger

	mu       sync.Mutex
	lastSeen time.Time

	// Quality band counter — `rate(...[5m])` gives "calls/sec
	// finishing with each quality band over the last 5 minutes".
	callsByBand *prometheus.CounterVec

	// Per-direction histograms — operator can pinpoint which network
	// path has bad quality.
	jitterMs    *prometheus.HistogramVec // labels: direction
	lossPercent *prometheus.HistogramVec // labels: direction
	mosScore    *prometheus.HistogramVec // labels: direction
	rttMs       prometheus.Histogram     // RTT is symmetric

	// Process-wide observability for the collector itself.
	scrapeRows   prometheus.Counter
	scrapeErrors prometheus.Counter
}

// New constructs a Collector. Watermark starts at "now" so the first
// scrape after restart doesn't double-count history that Prometheus
// already remembers from the previous process.
func New(r repo, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.Default()
	}
	const ns = "asterisk"

	return &Collector{
		repo:     r,
		timeout:  10 * time.Second,
		logger:   logger,
		lastSeen: time.Now().UTC(),

		callsByBand: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: "call",
			Name:      "quality_total",
			Help:      "Completed calls bucketed by RTP quality band (excellent/good/fair/poor/bad/unknown). One increment per leg with a populated cdr.rtpqos.",
		}, []string{"band"}),

		jitterMs: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: "call",
			Name:      "rtp_jitter_milliseconds",
			Help:      "RTP jitter per leg from cdr.rtpqos. direction=rx is the receive side; direction=tx is the transmit side as reported by the bridged peer.",
			// Buckets chosen for VoIP norms: <30ms inaudible, 30–50ms
			// noticeable, 50–100ms degraded, >100ms bad.
			Buckets: []float64{1, 5, 10, 20, 30, 50, 100, 200, 500},
		}, []string{"direction"}),

		lossPercent: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: "call",
			Name:      "rtp_loss_percent",
			Help:      "RTP packet loss percentage per leg from cdr.rtpqos. >1% is audible distortion, >5% breaks intelligibility.",
			Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 20, 50},
		}, []string{"direction"}),

		mosScore: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: "call",
			Name:      "rtp_mos_score",
			Help:      "Mean Opinion Score per leg derived from Asterisk's MES via the ITU-T G.107 R-factor → MOS conversion. Range 1.0–4.5; ≥4.0 is good, <3.1 is bad.",
			Buckets:   []float64{1, 2, 2.5, 3, 3.1, 3.6, 4, 4.3, 4.5},
		}, []string{"direction"}),

		rttMs: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: ns,
			Subsystem: "call",
			Name:      "rtp_rtt_milliseconds",
			Help:      "RTP round-trip time per leg from cdr.rtpqos.RTT. >150ms degrades conversational flow, >300ms is borderline unusable.",
			Buckets:   []float64{10, 25, 50, 100, 150, 200, 300, 500, 1000},
		}),

		scrapeRows: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: "call_quality_scrape",
			Name:      "rows_total",
			Help:      "Total number of cdr rows observed by the call-quality collector since process start.",
		}),
		scrapeErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: ns,
			Subsystem: "call_quality_scrape",
			Name:      "errors_total",
			Help:      "Total number of failures while reading cdr rows for the call-quality collector.",
		}),
	}
}

// Describe implements prometheus.Collector.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	c.callsByBand.Describe(ch)
	c.jitterMs.Describe(ch)
	c.lossPercent.Describe(ch)
	c.mosScore.Describe(ch)
	c.rttMs.Describe(ch)
	c.scrapeRows.Describe(ch)
	c.scrapeErrors.Describe(ch)
}

// Collect implements prometheus.Collector. Pulls all new cdr rows
// since the last scrape, observes them into the histograms / counter,
// then emits the current state.
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.repo != nil {
		c.scrapeOnce()
	}

	c.callsByBand.Collect(ch)
	c.jitterMs.Collect(ch)
	c.lossPercent.Collect(ch)
	c.mosScore.Collect(ch)
	c.rttMs.Collect(ch)
	c.scrapeRows.Collect(ch)
	c.scrapeErrors.Collect(ch)
}

func (c *Collector) scrapeOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	legs, err := c.repo.ListLegsWithQoSSince(ctx, c.lastSeen)
	if err != nil {
		c.scrapeErrors.Inc()
		c.logger.Error("call-quality scrape failed", "err", err)
		return
	}
	for i := range legs {
		l := &legs[i]
		c.observe(l)
		// Watermark: largest calldate seen, so the next scrape picks
		// up only newer rows. Equality on calldate is rare enough that
		// the strict-greater-than in SQL is good enough.
		if l.CallDate.After(c.lastSeen) {
			c.lastSeen = l.CallDate
		}
	}
}

func (c *Collector) observe(l *data.CallLeg) {
	c.scrapeRows.Inc()
	q := l.RTPQoS
	if q == nil {
		c.callsByBand.WithLabelValues(string(data.QualityUnknown)).Inc()
		return
	}
	c.callsByBand.WithLabelValues(string(q.Quality)).Inc()

	// Only record histograms when the underlying RTCP report exists
	// (zero values mean "not received"). Histogram buckets aren't
	// meaningful for "no data" — better to keep the count low than
	// to skew percentiles toward zero.
	if q.RxJitterMs > 0 {
		c.jitterMs.WithLabelValues("rx").Observe(q.RxJitterMs)
	}
	if q.TxJitterMs > 0 {
		c.jitterMs.WithLabelValues("tx").Observe(q.TxJitterMs)
	}
	if q.RxLossPercent > 0 || q.RxCount > 0 {
		c.lossPercent.WithLabelValues("rx").Observe(q.RxLossPercent)
	}
	if q.TxLossPercent > 0 || q.TxCount > 0 {
		c.lossPercent.WithLabelValues("tx").Observe(q.TxLossPercent)
	}
	if q.RxMOS > 0 {
		c.mosScore.WithLabelValues("rx").Observe(q.RxMOS)
	}
	if q.TxMOS > 0 {
		c.mosScore.WithLabelValues("tx").Observe(q.TxMOS)
	}
	if q.RTTMs > 0 {
		c.rttMs.Observe(q.RTTMs)
	}
}
