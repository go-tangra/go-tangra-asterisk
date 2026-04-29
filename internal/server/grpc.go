package server

import (
	"context"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	kratosmd "github.com/go-kratos/kratos/v2/middleware/metadata"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-common/middleware/audit"
	"github.com/go-tangra/go-tangra-common/middleware/mtls"
	"github.com/go-tangra/go-tangra-common/viewer"

	asteriskpb "github.com/go-tangra/go-tangra-asterisk/gen/go/asterisk/service/v1"
	"github.com/go-tangra/go-tangra-asterisk/internal/cert"
	"github.com/go-tangra/go-tangra-asterisk/internal/service"
)

func systemViewerMiddleware() middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			ctx = viewer.NewSystemViewerContext(ctx)
			return handler(ctx, req)
		}
	}
}

// NewGRPCServer wires up the asterisk gRPC server with the platform's standard
// middleware stack. Health checks bypass mTLS so liveness probes can reach
// the server without a client cert.
func NewGRPCServer(
	ctx *bootstrap.Context,
	certManager *cert.CertManager,
	cdrSvc *service.CdrService,
	statsSvc *service.StatsService,
	regSvc *service.RegistrationService,
) *grpc.Server {
	cfg := ctx.GetConfig()
	l := ctx.NewLoggerHelper("asterisk/grpc")

	var opts []grpc.ServerOption

	if cfg.Server != nil && cfg.Server.Grpc != nil {
		if cfg.Server.Grpc.Network != "" {
			opts = append(opts, grpc.Network(cfg.Server.Grpc.Network))
		}
		if cfg.Server.Grpc.Addr != "" {
			opts = append(opts, grpc.Address(cfg.Server.Grpc.Addr))
		}
		if cfg.Server.Grpc.Timeout != nil {
			opts = append(opts, grpc.Timeout(cfg.Server.Grpc.Timeout.AsDuration()))
		}
	}

	if certManager != nil && certManager.IsTLSEnabled() {
		tlsConfig, err := certManager.GetServerTLSConfig()
		if err != nil {
			l.Warnf("Failed to get TLS config, running without TLS: %v", err)
		} else {
			opts = append(opts, grpc.TLSConfig(tlsConfig))
			l.Info("gRPC server configured with mTLS")
		}
	} else {
		l.Warn("TLS not enabled, running without mTLS")
	}

	var ms []middleware.Middleware
	ms = append(ms, recovery.Recovery())
	ms = append(ms, systemViewerMiddleware())
	ms = append(ms, tracing.Server())
	ms = append(ms, kratosmd.Server())
	ms = append(ms, logging.Server(ctx.GetLogger()))

	if certManager != nil && certManager.IsTLSEnabled() {
		ms = append(ms, mtls.MTLSMiddleware(
			ctx.GetLogger(),
			mtls.WithPublicEndpoints(
				"/grpc.health.v1.Health/Check",
				"/grpc.health.v1.Health/Watch",
			),
		))
	}

	ms = append(ms, audit.Server(
		ctx.GetLogger(),
		audit.WithServiceName("asterisk-service"),
		audit.WithSkipOperations(
			"/grpc.health.v1.Health/Check",
			"/grpc.health.v1.Health/Watch",
		),
	))

	ms = append(ms, protoValidator())

	opts = append(opts, grpc.Middleware(ms...))

	srv := grpc.NewServer(opts...)

	asteriskpb.RegisterAsteriskCdrServiceServer(srv, cdrSvc)
	asteriskpb.RegisterAsteriskStatsServiceServer(srv, statsSvc)
	asteriskpb.RegisterAsteriskRegistrationServiceServer(srv, regSvc)

	return srv
}
