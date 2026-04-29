package data

import (
	"os"

	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-common/registration"
)

// NewRegistrationClient creates a registration client connected to admin-service.
// This is created early (during Wire DI) so its connection is available for
// the registration helper. Asterisk doesn't call other modules — no
// ModuleDialer needed.
func NewRegistrationClient(ctx *bootstrap.Context) (*registration.Client, error) {
	adminEndpoint := os.Getenv("ADMIN_GRPC_ENDPOINT")
	if adminEndpoint == "" {
		return nil, nil
	}
	cfg := &registration.Config{
		AdminEndpoint: adminEndpoint,
		MaxRetries:    60,
	}
	return registration.NewClient(ctx.GetLogger(), cfg)
}
