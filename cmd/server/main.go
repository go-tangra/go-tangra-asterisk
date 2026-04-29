package main

import (
	"context"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	kratosHttp "github.com/go-kratos/kratos/v2/transport/http"

	conf "github.com/tx7do/kratos-bootstrap/api/gen/go/conf/v1"
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-asterisk/cmd/server/assets"
	"github.com/go-tangra/go-tangra-asterisk/internal/ami"
	"github.com/go-tangra/go-tangra-common/registration"
	"github.com/go-tangra/go-tangra-common/service"
)

var (
	moduleID    = "asterisk"
	moduleName  = "Asterisk"
	version     = "1.0.0"
	description = "FreePBX call detail records and statistics"
)

var globalRegHelper *registration.RegistrationHelper
var globalAMICancel context.CancelFunc

func newApp(
	ctx *bootstrap.Context,
	gs *grpc.Server,
	hs *kratosHttp.Server,
	regClient *registration.Client,
	amiListener *ami.Listener,
) *kratos.App {
	if regClient != nil {
		regClient.SetConfig(&registration.Config{
			ModuleID:         moduleID,
			ModuleName:       moduleName,
			Version:          version,
			Description:      description,
			GRPCEndpoint:     registration.GetGRPCAdvertiseAddr(ctx, "0.0.0.0:9800"),
			FrontendEntryUrl: registration.GetEnvOrDefault("FRONTEND_ENTRY_URL", ""),
			HttpEndpoint:     registration.GetEnvOrDefault("HTTP_ADVERTISE_ADDR", ""),
			OpenapiSpec:      assets.OpenApiData,
			ProtoDescriptor:  assets.DescriptorData,
			MenusYaml:        assets.MenusData,
		})
		globalRegHelper = registration.StartRegistrationWithClient(ctx.GetLogger(), regClient)
	}

	// AMI listener runs in the background for the lifetime of the app.
	// Listener.Run is a no-op when AMI is disabled.
	if amiListener != nil {
		amiCtx, cancel := context.WithCancel(context.Background())
		globalAMICancel = cancel
		go amiListener.Run(amiCtx)
	}

	return bootstrap.NewApp(ctx, gs, hs)
}

func runApp() error {
	ctx := bootstrap.NewContext(
		context.Background(),
		&conf.AppInfo{
			Project: service.Project,
			AppId:   "asterisk.service",
			Version: version,
		},
	)

	defer func() {
		if globalRegHelper != nil {
			globalRegHelper.Stop()
		}
		if globalAMICancel != nil {
			globalAMICancel()
		}
	}()

	return bootstrap.RunApp(ctx, initApp)
}

func main() {
	if err := runApp(); err != nil {
		panic(err)
	}
}
