package providers

import (
	"github.com/tx7do/kratos-bootstrap/bootstrap"

	"github.com/go-tangra/go-tangra-asterisk/internal/ami"
	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// NewAMIListener constructs the AMI listener tied to the PJSIP repo. It
// lives in this provider package (not internal/data) to avoid an import
// cycle: internal/ami already imports internal/data for the PJSIPRegEvent
// type and EventSink interface.
func NewAMIListener(ctx *bootstrap.Context, cfg *data.Config, repo *data.PJSIPRegRepo) *ami.Listener {
	return ami.NewListener(ctx, ami.Config{
		Host:           cfg.AMI.Host,
		Port:           cfg.AMI.Port,
		Username:       cfg.AMI.Username,
		Secret:         cfg.AMI.Secret,
		ReconnectDelay: cfg.AMI.ReconnectDelay,
	}, repo)
}
