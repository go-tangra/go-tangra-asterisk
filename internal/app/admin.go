package app

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/go-tangra/go-tangra/v4/transport"
	"github.com/go-tangra/go-tangra/v4/transport/tlsconf"
	"net"
	"net/http"
	"time"
)

// The application admin listener includes source readiness. Freya's own
// operations listener remains on an ephemeral loopback port for runtime health.
func (a *App) buildAdmin() error {
	lis, e := net.Listen("tcp", a.Cfg.Admin.Addr)
	if e != nil {
		return errors.New("admin listener unavailable")
	}
	a.adminListener = lis
	host, _, _ := net.SplitHostPort(lis.Addr().String())
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		tlsCfg, e := tlsconf.ServerConfig(a.Freya.Provider(), transport.TLSOptions(a.Freya))
		if e != nil {
			lis.Close()
			return errors.New("admin mTLS unavailable")
		}
		a.adminListener = tls.NewListener(lis, tlsCfg)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if a.ready(r.Context()) != nil {
			http.Error(w, "not ready", 503)
			return
		}
		w.Write([]byte("ready\n"))
	})
	mux.Handle("GET /metrics", a.Freya.Metrics().Handler())
	a.admin = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	return nil
}
func (a *App) shutdownAdmin() {
	if a.admin != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = a.admin.Shutdown(ctx)
	}
	if a.adminListener != nil {
		_ = a.adminListener.Close()
	}
}
