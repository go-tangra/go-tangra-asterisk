package app

import (
	"context"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/pkg/asteriskmanifest"
	authv1 "github.com/go-tangra/go-tangra-auth/sdk/v4/api/proto/auth/v1"
	"github.com/go-tangra/go-tangra-portal/sdk/v4/pkg/gatewayclient"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"time"
)

type AuthPerms struct{ Client authv1.AuthorizationClient }

var errAuthUnavailable = errors.New("authorization unavailable")

// Has asks auth for a decision. Rejections of the request itself are denials;
// any other failure (transport, timeout, auth down) is an error, never a deny.
func (p AuthPerms) Has(ctx context.Context, tenant, user, permission string) (bool, error) {
	resource, action, ok := strings.Cut(permission, ":")
	if !ok {
		return false, nil
	}
	if p.Client == nil {
		return false, errAuthUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	res, e := p.Client.Check(ctx, &authv1.CheckRequest{TenantId: tenant, UserId: user, Resource: resource, Action: action})
	switch status.Code(e) {
	case codes.OK:
		return res.GetAllowed(), nil
	case codes.InvalidArgument, codes.NotFound, codes.PermissionDenied:
		return false, nil
	}
	return false, errAuthUnavailable
}
func pause(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
func (a *App) seedLoop(ctx context.Context) {
	for ctx.Err() == nil {
		attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
		conn, e := a.Freya.Client(attempt, "auth")
		if e == nil {
			_, e = asteriskmanifest.Registration().Register(attempt, conn, a.Log)
		}
		cancel()
		delay := 5 * time.Minute
		if e != nil {
			a.Log.Warn("auth declarations unavailable; retrying")
			delay = 5 * time.Second
		}
		if !pause(ctx, delay) {
			return
		}
	}
}
func (a *App) register(ctx context.Context) {
	man, e := asteriskmanifest.Manifest()
	if e != nil {
		return
	}
	for ctx.Err() == nil {
		if a.ready(ctx) != nil {
			if !pause(ctx, time.Second) {
				return
			}
			continue
		}
		ep, e := a.Freya.HTTP().Endpoint()
		if e != nil {
			if !pause(ctx, time.Second) {
				return
			}
			continue
		}
		attempt, cancel := context.WithTimeout(ctx, 5*time.Second)
		conn, e := a.Freya.Client(attempt, a.Cfg.Gateway.Service)
		cancel()
		if e != nil {
			if !pause(ctx, 2*time.Second) {
				return
			}
			continue
		}
		client, e := gatewayclient.New(conn, gatewayclient.Options{Manifest: man, HTTPURL: "https://" + ep.Host, Logger: a.Log})
		if e != nil {
			return
		}
		leaseCtx, stop := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-leaseCtx.Done():
					return
				case <-ticker.C:
					if a.ready(leaseCtx) != nil {
						stop()
						return
					}
				}
			}
		}()
		_ = client.Run(leaseCtx)
		stop()
		<-done
		if !pause(ctx, 2*time.Second) {
			return
		}
	}
}
