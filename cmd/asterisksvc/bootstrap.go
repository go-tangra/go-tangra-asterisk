package main

import (
	"context"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"time"
)

func bootstrap(ctx context.Context, c config.Config) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return registration.Bootstrap(ctx, c)
}
