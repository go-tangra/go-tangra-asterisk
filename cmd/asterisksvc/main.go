package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/app"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "preflight" {
		os.Exit(preflightCmd(args[1:], os.Stdout, os.Stderr))
	}
	boot := len(args) > 0 && args[0] == "bootstrap"
	if boot {
		args = args[1:]
	}
	flags := flag.NewFlagSet("asterisksvc", flag.ContinueOnError)
	path := flags.String("config", "configs/dev.yaml", "Configuration file")
	if e := flags.Parse(args); e != nil {
		return e
	}
	c, e := config.Load(*path)
	if e != nil {
		return e
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if boot {
		return bootstrap(ctx, c)
	}
	a, e := app.Build(ctx, c, app.Options{})
	if e != nil {
		return e
	}
	defer a.Close()
	return a.Run(ctx)
}
