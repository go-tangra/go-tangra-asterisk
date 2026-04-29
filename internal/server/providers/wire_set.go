//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package providers

import (
	"github.com/google/wire"

	"github.com/go-tangra/go-tangra-asterisk/internal/cert"
	"github.com/go-tangra/go-tangra-asterisk/internal/server"
)

var ProviderSet = wire.NewSet(
	cert.NewCertManager,
	server.NewGRPCServer,
	server.NewHTTPServer,
)
