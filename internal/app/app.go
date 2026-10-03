package app

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/go-tangra/go-tangra-lcm/sdk/v4/pkg/lcmidentity"
	freya "github.com/go-tangra/go-tangra/v4"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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
	Log           *slog.Logger
	registration  atomic.Pointer[registration.Repository]
	recordings    atomic.Pointer[recordings.Handler]
	openLate      []func(context.Context) // features Run keeps retrying to open
	wg            sync.WaitGroup
	closeOnce     sync.Once
	meshClose     func() // closes the lcm identity provider (mesh_enroll)
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
	fopts := append([]freya.Option(nil), o.Freya...)
	if c.MeshEnroll.Enabled {
		// Mesh identity: enroll for the module's own SPIFFE SVID over lcm.
		raw, rerr := os.ReadFile(c.MeshEnroll.TokenFile)
		if rerr != nil {
			return nil, errors.New("mesh enroll token unreadable")
		}
		prov, perr := lcmidentity.NewNet(ctx, lcmidentity.NetConfig{
			EnrollURL: c.MeshEnroll.EnrollURL, LCMGRPCTarget: c.MeshEnroll.LCMGRPCTarget,
			TenantID: c.MeshEnroll.TenantID, TrustDomain: c.Config.TrustDomain, ServiceName: c.Config.ServiceName,
			EnrollmentToken: strings.TrimSpace(string(raw)), Insecure: c.MeshEnroll.Insecure, StateFile: c.MeshEnroll.StateFile,
		})
		if perr != nil {
			return nil, fmt.Errorf("mesh enroll: %w", perr)
		}
		a.meshClose = func() { _ = prov.Close() }
		fopts = append(fopts, freya.WithIdentityProvider(prov))
	}
	if a.Freya, err = freya.New(runtimeCfg, fopts...); err != nil {
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
			a.Checker = newPermCache(AuthPerms{Client: authv1.NewAuthorizationClient(conn)}, 30*time.Second, 10000)
		}
	}
	location, err := time.LoadLocation(c.Binding.Timezone)
	if err != nil {
		return nil, errors.New("invalid timezone")
	}
	source, err := time.LoadLocation(c.Binding.SourceTimezone)
	if err != nil {
		return nil, errors.New("invalid source timezone")
	}
	repo := &cdr.Repository{Pools: a.Pools}
	reports := &stats.Repository{CDR: repo, Location: location}
	if c.Binding.RegistrationDSN != "" {
		a.feature(ctx, "registration", a.prune, func(ctx context.Context) error {
			r, e := registration.Open(ctx, c)
			if e != nil {
				return e
			}
			a.registration.Store(r)
			if a.Listener != nil {
				a.Listener.Store.Store(r)
			}
			return nil
		})
	}
	if c.Binding.RecordingRoot != "" {
		a.feature(ctx, "recordings", nil, func(context.Context) error {
			h, e := recordings.New(c.Binding.RecordingRoot, repo)
			if e != nil {
				return e
			}
			h.SourceLocation = source
			a.recordings.Store(h)
			return nil
		})
	}
	var metrics *dashboard.Client
	if c.Binding.MonitoringURL != "" {
		metrics, err = dashboard.New(c.Binding.MonitoringURL, c.Binding.MonitoringDedicated, c.Binding.TenantID, c.Binding.PBXID)
		if err != nil {
			return nil, err
		}
	}
	remote, _ := ui.Remote()
	a.HTTP, err = httpapi.New(httpapi.Deps{Tenant: c.Binding.TenantID, Verifier: a.Verifier, Checker: a.Checker, History: repo, Reports: reports, Registry: a.Registry, Registration: a.registration.Load, Recordings: a.recordings.Load, Dashboard: metrics, AMIEnabled: c.AMI.Enabled, StreamLifetime: time.Duration(c.StreamSeconds) * time.Second, Ready: a.ready, Remote: remote, Stop: a.streamStop, RegistrationFresh: func() bool { return a.Listener != nil && a.Listener.RegistrationFresh.Load() }, Capabilities: func(ctx context.Context) map[string]httpapi.Capability {
		ready := a.ready(ctx) == nil
		return map[string]httpapi.Capability{"history": {Available: ready, Fresh: ready}, "cel": {Available: a.Pools.CEL, Fresh: ready}, "quality": {Available: a.Pools.Columns["rtpqos"] || a.Pools.Columns["peerrtpqos"], Fresh: ready}, "names": {Available: a.Pools.Names, Fresh: ready}}
	}})
	if err != nil {
		return nil, err
	}
	a.Freya.HTTP().HandlePrefix("/", a.HTTP.Handler())
	collector := exporter.New(a.Registry)
	collector.SourceLocation = source
	a.qualityWorker = func(ctx context.Context) { collector.CollectQuality(ctx, repo) }
	if err = a.metrics(collector); err != nil {
		return nil, err
	}
	if c.AMI.Enabled {
		a.Listener = &ami.Listener{Config: c.AMI, Registry: a.Registry, OnError: func() { a.Log.Warn("AMI observation interrupted; retrying") }, OnContact: collector.Contact, OnFrame: collector.Frame, OnSweep: collector.BeginSweep}
		a.Listener.Store.Store(a.registration.Load())
	}
	if err = a.buildAdmin(); err != nil {
		return nil, err
	}
	return a, nil
}

// Registration and Recordings return the feature once it is open, else nil.
func (a *App) Registration() *registration.Repository { return a.registration.Load() }
func (a *App) Recordings() *recordings.Handler        { return a.recordings.Load() }

var retryDelay = time.Second

// feature opens an optional feature now. On failure it logs the reason (the
// open errors carry no DSN or secret) and Run retries with backoff, 1 s
// doubling to 1 min, until it opens; then runs after, if any.
func (a *App) feature(ctx context.Context, name string, after func(context.Context), open func(context.Context) error) {
	e := open(ctx)
	if e == nil {
		return
	}
	a.Log.Warn(name+" unavailable; retrying in the background", "reason", e.Error())
	a.openLate = append(a.openLate, func(ctx context.Context) {
		for delay := retryDelay; ; delay = min(2*delay, time.Minute) {
			if !pause(ctx, delay) {
				return
			}
			attempt, cancel := context.WithTimeout(ctx, 15*time.Second)
			e := open(attempt)
			cancel()
			if e == nil {
				break
			}
			if ctx.Err() == nil {
				a.Log.Warn(name+" still unavailable; retrying", "reason", e.Error())
			}
		}
		a.Log.Info(name + " available")
		if after != nil {
			after(ctx)
		}
	})
}
func (a *App) ready(ctx context.Context) error {
	if !a.Freya.Ready() {
		return errors.New("identity unavailable")
	}
	e := a.Pools.Ready(ctx)
	return e
}

// prune removes registration events past the retention, at start and then
// hourly.
func (a *App) prune(ctx context.Context) {
	if a.Cfg.Binding.RegistrationRetentionDays <= 0 {
		return
	}
	for {
		before := time.Now().AddDate(0, 0, -a.Cfg.Binding.RegistrationRetentionDays)
		if _, err := a.registration.Load().Prune(ctx, before); err != nil && ctx.Err() == nil {
			a.Log.Warn("registration retention prune failed; retrying next hour")
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Hour):
		}
	}
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
	if a.registration.Load() != nil {
		a.worker(wctx, a.prune)
	}
	for _, open := range a.openLate {
		a.worker(wctx, open)
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
		if h := a.recordings.Load(); h != nil {
			h.Close()
		}
		if r := a.registration.Load(); r != nil {
			r.DB.Close()
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
		if a.meshClose != nil {
			a.meshClose()
		}
	})
}
