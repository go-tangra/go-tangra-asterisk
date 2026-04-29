package server

import (
	"io/fs"
	"net/http"
	"os"

	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-asterisk/cmd/server/assets"
)

// NewHTTPServer serves the embedded Module Federation remote (frontend-dist)
// plus a couple of metadata endpoints (health, menus, openapi, descriptor).
//
// The admin gateway proxies /modules/asterisk/* to this server, which is how
// the SPA shell loads the federated remote at runtime.
func NewHTTPServer(ctx *bootstrap.Context) *kratosHttp.Server {
	l := ctx.NewLoggerHelper("asterisk/http")

	addr := os.Getenv("ASTERISK_HTTP_ADDR")
	if addr == "" {
		addr = "0.0.0.0:9801"
	}

	srv := kratosHttp.NewServer(kratosHttp.Address(addr))

	route := srv.Route("/")

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
