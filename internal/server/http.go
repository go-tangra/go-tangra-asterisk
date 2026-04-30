package server

import (
	"io/fs"
	"net/http"
	"os"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-asterisk/cmd/server/assets"
	"github.com/go-tangra/go-tangra-asterisk/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/internal/data"
	"github.com/go-tangra/go-tangra-asterisk/internal/exporter"
)

// NewHTTPServer serves the embedded Module Federation remote (frontend-dist)
// plus a couple of metadata endpoints (health, menus, openapi, descriptor),
// the call-recording streaming endpoint, the live-call SSE stream, and a
// Prometheus /metrics endpoint when AMI is configured.
//
// The admin gateway proxies /modules/asterisk/* to this server, which is how
// the SPA shell loads the federated remote at runtime.
func NewHTTPServer(ctx *bootstrap.Context, mysql *data.MySQLClients, registry *calls.Registry, cfg *data.Config) *kratosHttp.Server {
	l := ctx.NewLoggerHelper("asterisk/http")

	addr := os.Getenv("ASTERISK_HTTP_ADDR")
	if addr == "" {
		addr = "0.0.0.0:9801"
	}

	recordingsBase := os.Getenv("ASTERISK_RECORDINGS_PATH")
	if recordingsBase == "" {
		recordingsBase = "/var/spool/asterisk/monitor"
	}

	// Timeout(0) disables Kratos's per-request context timeout (default
	// 1s). Required for the SSE stream and the recording-download
	// endpoint, both of which legitimately stay open for many minutes.
	// Per-handler deadlines should still be added inline where they
	// make sense (e.g. the recording handler uses a 10s DB lookup
	// timeout before opening the file).
	srv := kratosHttp.NewServer(
		kratosHttp.Address(addr),
		kratosHttp.Timeout(0),
	)

	route := srv.Route("/")

	recordingHandler := NewRecordingHandler(l, mysql, recordingsBase)
	route.GET("/recordings/{linkedid}", recordingHandler.Serve)
	l.Infof("Recording endpoint enabled: base=%s", recordingsBase)

	if registry != nil {
		// Register via HandlePrefix instead of route.GET so the SSE
		// handler bypasses Kratos's request-timeout middleware (default
		// 1s, which would kill the stream before any keepalive fires).
		// Auth is enforced upstream by admin-service before the gateway
		// proxies the request — the module's HTTP server has no auth
		// of its own.
		streamHandler := NewCallStreamHandler(l, registry)
		srv.HandlePrefix("/calls/stream", streamHandler)
		l.Info("Live call SSE stream enabled at /calls/stream")
	}

	// Prometheus /metrics: vendored from menta2k/freepbx-exporter so
	// operators don't need to deploy a second process. Same scrape
	// schema as the standalone exporter.
	//
	// MUST stay unauthenticated. Prometheus scrapes don't carry user
	// credentials, and treating /metrics as a closed endpoint would
	// silently break the dashboards. We register via HandlePrefix to
	// bypass Kratos's middleware chain — DO NOT route this through
	// route.GET, and DO NOT add an auth wrapper here. If you need to
	// restrict access, do it at the network layer (firewall the module
	// port to Prometheus's source IP only).
	if metrics := exporter.Handler(cfg); metrics != nil {
		srv.HandlePrefix("/metrics", metrics)
		l.Info("Prometheus metrics enabled at /metrics (unauthenticated by design — restrict via network ACLs)")
	}

	route.GET("/health", func(c kratosHttp.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	route.GET("/openapi.yaml", func(c kratosHttp.Context) error {
		c.Response().Header().Set("Content-Type", "application/yaml")
		_, err := c.Response().Write(assets.OpenApiData)
		return err
	})

	route.GET("/menus.yaml", func(c kratosHttp.Context) error {
		c.Response().Header().Set("Content-Type", "application/yaml")
		_, err := c.Response().Write(assets.MenusData)
		return err
	})

	route.GET("/proto-descriptor", func(c kratosHttp.Context) error {
		c.Response().Header().Set("Content-Type", "application/octet-stream")
		c.Response().Header().Set("Content-Disposition", "attachment; filename=descriptor.bin")
		_, err := c.Response().Write(assets.DescriptorData)
		return err
	})

	if fsys, err := fs.Sub(assets.FrontendDist, "frontend-dist"); err == nil {
		srv.HandlePrefix("/", http.FileServer(http.FS(fsys)))
		l.Info("Serving embedded frontend assets")
	} else {
		l.Warnf("Failed to load embedded frontend assets: %v", err)
	}

	l.Infof("HTTP server listening on %s", addr)
	return srv
}
