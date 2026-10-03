package app

import (
	"context"
	"errors"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/ami"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/calls"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/cdr"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/config"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/dashboard"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/exporter"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/httpapi"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/pbx"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/recordings"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/registration"
	"github.com/go-tangra/go-tangra-asterisk/v4/internal/stats"
	"github.com/go-tangra/go-tangra-asterisk/v4/ui"
	authv1 "github.com/go-tangra/go-tangra-auth/sdk/v4/api/proto/auth/v1"
	"github.com/go-tangra/go-tangra-auth/sdk/v4/pkg/authclient"
	freya "github.com/go-tangra/go-tangra/v4"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

type Options struct {
	Freya    []freya.Option
	Verifier httpapi.Verifier
	Checker  httpapi.Checker
}
type App struct {
	Cfg           config.Config
	Freya         *freya.App
	Pools         *pbx.Pools
	HTTP          *httpapi.Server
	Verifier      httpapi.Verifier
	Checker       httpapi.Checker
	Listener      *ami.Listener
	Registry      *calls.Registry
	Registration  *registration.Repository
	Recordings    *recordings.Handler
	Log           *slog.Logger
	wg            sync.WaitGroup
	closeOnce     sync.Once
	admin         *http.Server
	adminListener net.Listener
	streamStop    chan struct{}
	qualityWorker func(context.Context)
	ran           bool
}

func Build(ctx context.Context, c config.Config, o Options) (a *App, err error) {
	if err = c.Validate(); err != nil {
		return nil, err
	}
	a = &App{Cfg: c, Log: slog.New(slog.NewJSONHandler(os.Stderr, nil)), Registry: calls.New(), streamStop: make(chan struct{})}
	built := a
	defer func() {
		if err != nil {
			built.Close()
		}
	}()
	runtimeCfg := c.Config
	runtimeCfg.Admin.Addr = "127.0.0.1:0"
	if a.Freya, err = freya.New(runtimeCfg, o.Freya...); err != nil {
		return nil, errors.New("secure runtime initialization failed")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if a.Pools, err = pbx.Open(ctx, c); err != nil {
		return nil, err
	}
	a.Verifier, a.Checker = o.Verifier, o.Checker
	if a.Verifier == nil || a.Checker == nil {
		conn, e := a.Freya.Client(ctx, "auth")
		if e != nil {
			return nil, errors.New("auth mesh client unavailable")
		}
		if a.Verifier == nil {
			a.Verifier = authclient.New(authclient.Config{Issuer: c.Gateway.Issuer}, authclient.GRPCKeys{Client: authv1.NewKeysClient(conn)}, authclient.GRPCRevocations{Client: authv1.NewSessionsClient(conn)})
		}
		if a.Checker == nil {
			a.Checker = AuthPerms{Client: authv1.NewAuthorizationClient(conn)}
		}
	}
	if c.Binding.RegistrationDSN != "" {
		a.Registration, _ = registration.Open(ctx, c)
		if a.Registration == nil {
			a.Log.Warn("registration unavailable; bootstrap or database recovery required")
		}
	}
	repo := &cdr.Repository{Pools: a.Pools}
	location, _ := time.LoadLocation(c.Binding.Timezone)
	reports := &stats.Repository{CDR: repo, Location: location}
	if c.Binding.RecordingRoot != "" {
		a.Recordings, _ = recordings.New(c.Binding.RecordingRoot, repo)
		if a.Recordings != nil {
			a.Recordings.SourceLocation, _ = time.LoadLocation(c.Binding.SourceTimezone)
		}
		if a.Recordings == nil {
			a.Log.Warn("recordings unavailable")
		}
	}
	var metrics *dashboard.Client
	if c.Binding.MonitoringURL != "" {
		metrics, err = dashboard.New(c.Binding.MonitoringURL, c.Binding.MonitoringDedicated, c.Binding.TenantID, c.Binding.PBXID)
		if err != nil {
			return nil, err
		}
	}
	remote, _ := ui.Remote()
	a.HTTP, err = httpapi.New(httpapi.Deps{Tenant: c.Binding.TenantID, Verifier: a.Verifier, Checker: a.Checker, History: repo, Reports: reports, Registry: a.Registry, Registration: a.Registration, Recordings: a.Recordings, Dashboard: metrics, AMIEnabled: c.AMI.Enabled, StreamLifetime: time.Duration(c.StreamSeconds) * time.Second, Ready: a.ready, Remote: remote, Stop: a.streamStop, RegistrationFresh: func() bool { return a.Listener != nil && a.Listener.RegistrationFresh.Load() }, Capabilities: func(ctx context.Context) map[string]httpapi.Capability {
		ready := a.ready(ctx) == nil
		return map[string]httpapi.Capability{"history": {Available: ready, Fresh: ready}, "cel": {Available: a.Pools.CEL, Fresh: ready}, "quality": {Available: a.Pools.Columns["rtpqos"] || a.Pools.Columns["peerrtpqos"], Fresh: ready}, "names": {Available: a.Pools.Names, Fresh: ready}}
	}})
	if err != nil {
		return nil, err
	}
	a.Freya.HTTP().HandlePrefix("/", a.HTTP.Handler())
	collector := exporter.New(a.Registry)
	collector.SourceLocation, _ = time.LoadLocation(c.Binding.SourceTimezone)
	a.qualityWorker = func(ctx context.Context) { collector.CollectQuality(ctx, repo) }
	if err = a.metrics(collector); err != nil {
		return nil, err
	}
	if c.AMI.Enabled {
		a.Listener = &ami.Listener{Config: c.AMI, Registry: a.Registry, Store: a.Registration, OnError: func() { a.Log.Warn("AMI observation interrupted; retrying") }, OnContact: collector.Contact, OnFrame: collector.Frame, OnSweep: collector.BeginSweep}
	}
	if err = a.buildAdmin(); err != nil {
		return nil, err
	}
	return a, nil
}
func (a *App) ready(ctx context.Context) error {
	if !a.Freya.Ready() {
		return errors.New("identity unavailable")
	}
	e := a.Pools.Ready(ctx)
	return e
}
func (a *App) worker(ctx context.Context, f func(context.Context)) {
	a.wg.Add(1)
	go func() { defer a.wg.Done(); f(ctx) }()
}
func (a *App) Run(ctx context.Context) error {
	a.ran = true
	wctx, cancel := context.WithCancel(ctx)
	a.worker(wctx, func(ctx context.Context) { <-ctx.Done(); close(a.streamStop) })
	if v, ok := a.Verifier.(*authclient.Verifier); ok {
		a.worker(wctx, func(ctx context.Context) {
			for ctx.Err() == nil {
				if e := v.Start(ctx, func(error) { a.Log.Warn("session verification refresh unavailable") }); e == nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		})
	}
	a.worker(wctx, a.register)
	a.worker(wctx, a.seedLoop)
	a.worker(wctx, a.qualityWorker)
	if a.Listener != nil {
		a.worker(wctx, a.Listener.Run)
	}
	a.worker(wctx, func(context.Context) {
		if a.admin.Serve(a.adminListener) != nil {
			cancel()
		}
	})
	err := a.Freya.Run(wctx)
	cancel()
	a.shutdownAdmin()
	a.wg.Wait()
	return err
}
func (a *App) Close() {
	a.closeOnce.Do(func() {
		a.shutdownAdmin()
		if a.Recordings != nil {
			a.Recordings.Close()
		}
		if a.Registration != nil {
			a.Registration.DB.Close()
		}
		if a.Pools != nil {
			a.Pools.Close()
		}
		if a.Freya != nil {
			if !a.ran {
				// The published runtime binds transports in New; Run with an already
				// canceled context drives their lifecycle shutdown before Close.
				cleanup, cancel := context.WithCancel(context.Background())
				cancel()
				_ = a.Freya.Run(cleanup)
			}
			a.Freya.Close()
		}
	})
}
