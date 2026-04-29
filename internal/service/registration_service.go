package service

import (
	"context"
	"errors"
	"time"

	kratoserr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	asteriskpb "github.com/go-tangra/go-tangra-asterisk/gen/go/asterisk/service/v1"
	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// RegistrationService implements AsteriskRegistrationService.
type RegistrationService struct {
	asteriskpb.UnimplementedAsteriskRegistrationServiceServer

	log  *log.Helper
	repo *data.PJSIPRegRepo
}

func NewRegistrationService(ctx *bootstrap.Context, repo *data.PJSIPRegRepo) *RegistrationService {
	return &RegistrationService{
		log:  ctx.NewLoggerHelper("asterisk/service/registration"),
		repo: repo,
	}
}

func (s *RegistrationService) GetRegistrationStatus(ctx context.Context, req *asteriskpb.GetRegistrationStatusRequest) (*asteriskpb.GetRegistrationStatusResponse, error) {
	if req.Extension == "" {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "extension is required")
	}
	at := time.Now().UTC()
	if req.At != nil {
		at = req.At.AsTime()
	}

	res, err := s.repo.GetStatusAt(ctx, req.Extension, at)
	if err != nil {
		if errors.Is(err, data.ErrPJSIPRepoDisabled) {
			return nil, kratoserr.New(503, "AMI_DISABLED", "PJSIP registration capture is not configured")
		}
		s.log.WithContext(ctx).Errorf("registration status: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.GetRegistrationStatusResponse{
		Extension:  res.Endpoint,
		Registered: res.Registered,
		Status:     regStatusToProto(res.Status),
	}
	if res.LastEvent != nil {
		out.LastEvent = regEventToProto(res.LastEvent)
	}
	return out, nil
}

func (s *RegistrationService) ListRegistrationEvents(ctx context.Context, req *asteriskpb.ListRegistrationEventsRequest) (*asteriskpb.ListRegistrationEventsResponse, error) {
	if req.From == nil || req.To == nil {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from and to are required")
	}
	events, total, err := s.repo.ListEvents(ctx, data.PJSIPRegEventFilter{
		Endpoint: req.Extension,
		From:     req.From.AsTime(),
		To:       req.To.AsTime(),
		Page:     int(req.Page),
		PageSize: int(req.PageSize),
	})
	if err != nil {
		if errors.Is(err, data.ErrPJSIPRepoDisabled) {
			return nil, kratoserr.New(503, "AMI_DISABLED", "PJSIP registration capture is not configured")
		}
		s.log.WithContext(ctx).Errorf("list registration events: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.ListRegistrationEventsResponse{
		Events: make([]*asteriskpb.RegistrationEvent, 0, len(events)),
		Total:  total,
	}
	for i := range events {
		out.Events = append(out.Events, regEventToProto(&events[i]))
	}
	return out, nil
}

func (s *RegistrationService) ListRegisteredAt(ctx context.Context, req *asteriskpb.ListRegisteredAtRequest) (*asteriskpb.ListRegisteredAtResponse, error) {
	at := time.Now().UTC()
	if req.At != nil {
		at = req.At.AsTime()
	}
	endpoints, err := s.repo.ListRegisteredAt(ctx, at)
	if err != nil {
		if errors.Is(err, data.ErrPJSIPRepoDisabled) {
			return nil, kratoserr.New(503, "AMI_DISABLED", "PJSIP registration capture is not configured")
		}
		s.log.WithContext(ctx).Errorf("list registered at: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.ListRegisteredAtResponse{
		At:        timestamppb.New(at),
		Endpoints: make([]*asteriskpb.RegisteredEndpoint, 0, len(endpoints)),
	}
	for i := range endpoints {
		ep := &endpoints[i]
		row := &asteriskpb.RegisteredEndpoint{
			Endpoint:      ep.Endpoint,
			ContactUri:    ep.ContactURI,
			UserAgent:     ep.UserAgent,
			ViaAddress:    ep.ViaAddress,
			Status:        regStatusToProto(ep.Status),
			LastEventTime: timestamppb.New(ep.LastEventTime),
		}
		if ep.RegExpire != nil {
			row.RegExpire = timestamppb.New(*ep.RegExpire)
		}
		out.Endpoints = append(out.Endpoints, row)
	}
	return out, nil
}

func regEventToProto(e *data.PJSIPRegEvent) *asteriskpb.RegistrationEvent {
	out := &asteriskpb.RegistrationEvent{
		Id:         e.ID,
		EventTime:  timestamppb.New(e.EventTime),
		Endpoint:   e.Endpoint,
		Aor:        e.AOR,
		ContactUri: e.ContactURI,
		Status:     regStatusToProto(e.Status),
		UserAgent:  e.UserAgent,
		ViaAddress: e.ViaAddress,
		RttUsec:    e.RTTMicros,
	}
	if e.RegExpire != nil {
		out.RegExpire = timestamppb.New(*e.RegExpire)
	}
	return out
}

func regStatusToProto(s data.PJSIPRegStatus) asteriskpb.RegStatus {
	switch s {
	case data.PJSIPRegCreated:
		return asteriskpb.RegStatus_REG_STATUS_CREATED
	case data.PJSIPRegUpdated:
		return asteriskpb.RegStatus_REG_STATUS_UPDATED
	case data.PJSIPRegReachable:
		return asteriskpb.RegStatus_REG_STATUS_REACHABLE
	case data.PJSIPRegUnreachable:
		return asteriskpb.RegStatus_REG_STATUS_UNREACHABLE
	case data.PJSIPRegRemoved:
		return asteriskpb.RegStatus_REG_STATUS_REMOVED
	case data.PJSIPRegUnknown:
		return asteriskpb.RegStatus_REG_STATUS_UNKNOWN
	case data.PJSIPRegUnqualified:
		return asteriskpb.RegStatus_REG_STATUS_UNQUALIFIED
	default:
		return asteriskpb.RegStatus_REG_STATUS_UNSPECIFIED
	}
}
