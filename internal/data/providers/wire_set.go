//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package providers

import (
	"github.com/google/wire"

	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// ProviderSet is the Wire provider set for the data layer.
var ProviderSet = wire.NewSet(
	data.LoadConfig,
	data.NewMySQLClients,
	data.NewCdrRepo,
	data.NewStatsRepo,
	data.NewPJSIPRegRepo,
	data.NewRegistrationClient,
	data.NewPrometheusClient,
	NewCallRegistry,
	NewAMIListener,
)
