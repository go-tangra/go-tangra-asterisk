package service

import (
	"context"

	kratoserr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	asteriskpb "github.com/go-tangra/go-tangra-asterisk/gen/go/asterisk/service/v1"
	"github.com/go-tangra/go-tangra-asterisk/internal/calls"
)

// LiveCallsService exposes a snapshot of the in-memory call registry
// over gRPC. The streaming companion is the SSE endpoint at
// /calls/stream on the HTTP server — see internal/server/calls_stream.go.
type LiveCallsService struct {
	asteriskpb.UnimplementedAsteriskLiveCallsServiceServer

	log      *log.Helper
	registry *calls.Registry
}

func NewLiveCallsService(ctx *bootstrap.Context, registry *calls.Registry) *LiveCallsService {
	return &LiveCallsService{
		log:      ctx.NewLoggerHelper("asterisk/service/live-calls"),
		registry: registry,
	}
}

func (s *LiveCallsService) ListActiveCalls(ctx context.Context, _ *asteriskpb.ListActiveCallsRequest) (*asteriskpb.ListActiveCallsResponse, error) {
	if s.registry == nil {
		return nil, kratoserr.New(503, "AMI_DISABLED", "live call tracking requires AMI to be configured")
	}
	snap := s.registry.Snapshot()
	out := &asteriskpb.ListActiveCallsResponse{Calls: make([]*asteriskpb.LiveCall, 0, len(snap))}
	for _, c := range snap {
		out.Calls = append(out.Calls, liveCallToProto(&c))
	}
	return out, nil
}

func liveCallToProto(c *calls.Call) *asteriskpb.LiveCall {
	out := &asteriskpb.LiveCall{
		Linkedid:  c.Linkedid,
		StartedAt: timestamppb.New(c.StartedAt),
		UpdatedAt: timestamppb.New(c.UpdatedAt),
		Bridged:   c.Bridged,
		Channels:  make([]*asteriskpb.LiveChannel, 0, len(c.Channels)),
	}
	for i := range c.Channels {
		ch := &c.Channels[i]
		out.Channels = append(out.Channels, &asteriskpb.LiveChannel{
			Uniqueid:          ch.Uniqueid,
			Linkedid:          ch.Linkedid,
			Channel:           ch.Channel,
			ChannelState:      ch.ChannelState,
			ChannelStateDesc:  ch.ChannelStateDesc,
			CallerIdNum:       ch.CallerIDNum,
			CallerIdName:      ch.CallerIDName,
			ConnectedLineNum:  ch.ConnectedLineNum,
			ConnectedLineName: ch.ConnectedLineName,
			Exten:             ch.Exten,
			Context:           ch.Context,
			BridgeId:          ch.BridgeID,
			CreatedAt:         timestamppb.New(ch.CreatedAt),
			UpdatedAt:         timestamppb.New(ch.UpdatedAt),
		})
	}
	return out
}
