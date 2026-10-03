package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	freya "github.com/go-tangra/go-tangra/v4"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestFailedBuildReleasesRuntimeListeners(t *testing.T) {
	lis, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	addr := lis.Addr().String()
	lis.Close()
	c := config.Default()
	c.Gateway.Issuer = "https://auth.example.org"
	c.TrustDomain = "test.local"
	c.Authz.Path = "unused"
	c.Admin.Addr = "127.0.0.1:0"
	c.Server.HTTPAddr = addr
	c.Server.GRPCAddr = "127.0.0.1:0"
	c.Binding.TenantID = "t"
	c.Binding.PBXID = "p"
	c.Binding.CDRDSN = "user:secret@tcp(127.0.0.1:1)/cdr"
	_, e = Build(context.Background(), c, Options{Freya: []freya.Option{freya.WithInsecureLocalDev(), freya.WithAllowAllPolicy()}})
	if e == nil {
		t.Fatal("unavailable history accepted")
	}
	lis, e = net.Listen("tcp", addr)
	if e != nil {
		t.Fatalf("failed build leaked listener: %v", e)
	}
	lis.Close()
}

// An optional feature that fails to open at start is retried in the
// background until it opens, then its follow-up runs.
func TestFeatureOpensLate(t *testing.T) {
	prev := retryDelay
	retryDelay = time.Millisecond
	defer func() { retryDelay = prev }()
	var log bytes.Buffer
	a := &App{Log: slog.New(slog.NewTextHandler(&log, nil))}
	var attempts atomic.Int32
	var available atomic.Bool
	after := make(chan struct{})
	a.feature(context.Background(), "registration", func(context.Context) { close(after) }, func(context.Context) error {
		if attempts.Add(1) < 4 {
			return errors.New("registration unavailable")
		}
		available.Store(true)
		return nil
	})
	if available.Load() || len(a.openLate) != 1 {
		t.Fatal("failed open not scheduled for retry")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.openLate[0](ctx)
	select {
	case <-after:
	case <-time.After(5 * time.Second):
		t.Fatal("feature never opened")
	}
	if !available.Load() || attempts.Load() != 4 {
		t.Fatalf("attempts = %d", attempts.Load())
	}
	if !strings.Contains(log.String(), "registration unavailable") || !strings.Contains(log.String(), "registration available") {
		t.Fatalf("log: %s", log.String())
	}
}
func TestFeatureOpenStopsWithContext(t *testing.T) {
	a := &App{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	a.feature(context.Background(), "recordings", nil, func(context.Context) error { return errors.New("recording root unavailable") })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { a.openLate[0](ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("retry ignored cancellation")
	}
}
