//go:build wireinject
// +build wireinject

//go:generate go run github.com/google/wire/cmd/wire

package providers

import (
	"github.com/google/wire"

	"github.com/go-tangra/go-tangra-asterisk/internal/service"
)

// ProviderSet is the Wire provider set for the service layer.
var ProviderSet = wire.NewSet(
	service.NewCdrService,
	service.NewStatsService,
	service.NewRegistrationService,
)
