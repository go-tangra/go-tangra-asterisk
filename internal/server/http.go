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
)

// NewHTTPServer serves the embedded Module Federation remote (frontend-dist)
// plus a couple of metadata endpoints (health, menus, openapi, descriptor),
// the call-recording streaming endpoint, and the live-call SSE stream.
//
// The admin gateway proxies /modules/asterisk/* to this server, which is how
// the SPA shell loads the federated remote at runtime.
func NewHTTPServer(ctx *bootstrap.Context, mysql *data.MySQLClients, registry *calls.Registry) *kratosHttp.Server {
	l := ctx.NewLoggerHelper("asterisk/http")

	addr := os.Getenv("ASTERISK_HTTP_ADDR")
	if addr == "" {
		addr = "0.0.0.0:9801"
	}

	recordingsBase := os.Getenv("ASTERISK_RECORDINGS_PATH")
	if recordingsBase == "" {
		recordingsBase = "/var/spool/asterisk/monitor"
	}

	srv := kratosHttp.NewServer(kratosHttp.Address(addr))

	route := srv.Route("/")

	recordingHandler := NewRecordingHandler(l, mysql, recordingsBase)
	route.GET("/recordings/{linkedid}", recordingHandler.Serve)
	l.Infof("Recording endpoint enabled: base=%s", recordingsBase)

	if registry != nil {
		streamHandler := NewCallStreamHandler(l, registry)
		route.GET("/calls/stream", streamHandler.Serve)
		l.Info("Live call SSE stream enabled at /calls/stream")
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
