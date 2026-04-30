// Package exporter wires the vendored freepbx-exporter collectors
// (internal/exporter/collector + internal/exporter/ami) to the
// asterisk module's runtime. Reuses the same AMI host/port/credentials
// that the live-call AMI listener uses, so operators only configure
// AMI access in one place.
//
// The exposed /metrics endpoint is meant to be scraped by an external
// Prometheus instance — same data the standalone freepbx-exporter
// would have served, just colocated in this module.
package exporter

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/go-tangra/go-tangra-asterisk/internal/data"
	"github.com/go-tangra/go-tangra-asterisk/internal/exporter/ami"
	"github.com/go-tangra/go-tangra-asterisk/internal/exporter/collector"
)

// Handler returns an http.Handler that serves Prometheus metrics, or
// nil when AMI is not configured (in which case the /metrics endpoint
// should not be registered at all).
//
// Each scrape opens a fresh AMI connection — the standard
// "scrape and disconnect" pattern. This is independent of the
// long-lived event-stream listener used by the live-calls feature;
// Asterisk allows many concurrent AMI sessions for the same user.
func Handler(cfg *data.Config) http.Handler {
	if cfg == nil || cfg.AMI.Host == "" {
		return nil
	}
	addr := net.JoinHostPort(cfg.AMI.Host, strconv.Itoa(amiPort(cfg.AMI.Port)))
	c := collector.New(
		ami.Config{
			Address:  addr,
			Username: cfg.AMI.Username,
			Secret:   cfg.AMI.Secret,
			// MUST be set explicitly. The collector uses cfg.Timeout
			// for the WHOLE-scrape deadline (context.WithTimeout in
			// Collector.Collect); the ami.Dial fallback to 10s only
			// kicks in inside Dial itself, which never runs because
			// the context is already expired. Leaving this at 0 makes
			// every scrape fail with "i/o timeout" in microseconds.
			Timeout: 10 * time.Second,
		},
		collector.ScrapeOptions{},
		slog.Default(),
		nil,
	)

	// Dedicated registry so this collector's metrics aren't mixed with
	// process / Go runtime metrics from the host process — a Prometheus
	// scrape against the asterisk module should look identical to what
	// the standalone freepbx-exporter would have served.
	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		// Falling back to nil disables the endpoint cleanly; a
		// duplicate-register error here would only happen during a
		// programming mistake during refactor.
		return nil
	}
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog:      slogErrorLogAdapter{},
		ErrorHandling: promhttp.ContinueOnError,
	})
}

func amiPort(p int) int {
	if p > 0 {
		return p
	}
	return 5038
}

// slogErrorLogAdapter satisfies promhttp.HandlerOpts.ErrorLog without
// pulling in the legacy `log` package.
type slogErrorLogAdapter struct{}

func (slogErrorLogAdapter) Println(v ...interface{}) {
	slog.Default().Error(fmt.Sprint(v...))
}
