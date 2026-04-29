package service

import (
	"context"
	"database/sql"
	"errors"

	kratoserr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	asteriskpb "github.com/go-tangra/go-tangra-asterisk/gen/go/asterisk/service/v1"
	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// CdrService implements AsteriskCdrService.
type CdrService struct {
	asteriskpb.UnimplementedAsteriskCdrServiceServer

	log  *log.Helper
	repo *data.CdrRepo
}

func NewCdrService(ctx *bootstrap.Context, repo *data.CdrRepo) *CdrService {
	return &CdrService{
		log:  ctx.NewLoggerHelper("asterisk/service/cdr"),
		repo: repo,
	}
}

func (s *CdrService) ListCalls(ctx context.Context, req *asteriskpb.ListCallsRequest) (*asteriskpb.ListCallsResponse, error) {
	if req.From == nil || req.To == nil {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from and to are required")
	}
	from := req.From.AsTime()
	to := req.To.AsTime()
	if !from.Before(to) {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from must be before to")
	}

	calls, total, err := s.repo.ListCalls(ctx, data.CallFilter{
		From:        from,
		To:          to,
		Src:         req.Src,
		Dst:         req.Dst,
		Extension:   req.Extension,
		Disposition: dispositionToString(req.Disposition),
		Direction:   req.Direction,
		Page:        int(req.Page),
		PageSize:    int(req.PageSize),
	})
	if err != nil {
		s.log.WithContext(ctx).Errorf("list calls: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.ListCallsResponse{
		Calls: make([]*asteriskpb.Call, 0, len(calls)),
		Total: total,
	}
	for i := range calls {
		out.Calls = append(out.Calls, callToProto(&calls[i]))
	}
	return out, nil
}

func (s *CdrService) GetCall(ctx context.Context, req *asteriskpb.GetCallRequest) (*asteriskpb.GetCallResponse, error) {
	if req.Linkedid == "" {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "linkedid is required")
	}

	summary, legs, timeline, err := s.repo.GetCall(ctx, req.Linkedid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, kratoserr.NotFound("CALL_NOT_FOUND", "call not found")
		}
		s.log.WithContext(ctx).Errorf("get call: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	resp := &asteriskpb.GetCallResponse{
		Summary:  callToProto(summary),
		Legs:     make([]*asteriskpb.CallLeg, 0, len(legs)),
		Timeline: make([]*asteriskpb.CelEvent, 0, len(timeline)),
	}
	for i := range legs {
		resp.Legs = append(resp.Legs, legToProto(&legs[i]))
	}
	for i := range timeline {
		resp.Timeline = append(resp.Timeline, eventToProto(&timeline[i]))
	}
	return resp, nil
}

func callToProto(c *data.CallSummary) *asteriskpb.Call {
	out := &asteriskpb.Call{
		Linkedid:          c.LinkedID,
		Calldate:          timestamppb.New(c.CallDate),
		Src:               c.Src,
		Clid:              c.Clid,
		Cnum:              c.Cnum,
		Cnam:              c.Cnam,
		Dst:               c.Dst,
		Direction:         c.Direction,
		Disposition:       dispositionFromString(c.Disposition),
		DurationSeconds:   c.DurationSeconds,
		BillsecSeconds:    c.BillsecSeconds,
		Did:               c.DID,
		LegCount:          c.LegCount,
		RecordingFile:     c.RecordingFile,
	}
	if c.PickupSeconds != nil {
		out.PickupSeconds = c.PickupSeconds
	}
	if c.AnsweredExtension != "" {
		ext := c.AnsweredExtension
		out.AnsweredExtension = &ext
	}
	if c.OriginatingExtension != "" {
		ext := c.OriginatingExtension
		out.OriginatingExtension = &ext
	}
	return out
}

func legToProto(l *data.CallLeg) *asteriskpb.CallLeg {
	out := &asteriskpb.CallLeg{
		Uniqueid:        l.Uniqueid,
		Calldate:        timestamppb.New(l.CallDate),
		Channel:         l.Channel,
		Dstchannel:      l.Dstchannel,
		Src:             l.Src,
		Dst:             l.Dst,
		Lastapp:         l.Lastapp,
		Lastdata:        l.Lastdata,
		Disposition:     dispositionFromString(l.Disposition),
		DurationSeconds: l.DurationSeconds,
		BillsecSeconds:  l.BillsecSeconds,
		RecordingFile:   l.RecordingFile,
	}
	if l.Extension != "" {
		ext := l.Extension
		out.Extension = &ext
	}
	return out
}

func eventToProto(e *data.CelEvent) *asteriskpb.CelEvent {
	return &asteriskpb.CelEvent{
		EventTime: timestamppb.New(e.EventTime),
		Eventtype: e.EventType,
		Channame:  e.ChanName,
		Uniqueid:  e.Uniqueid,
		Appname:   e.AppName,
		Appdata:   e.AppData,
		CidName:   e.CidName,
		CidNum:    e.CidNum,
		Exten:     e.Exten,
		Context:   e.Context,
	}
}

func dispositionFromString(s string) asteriskpb.Disposition {
	switch s {
	case "ANSWERED":
		return asteriskpb.Disposition_DISPOSITION_ANSWERED
	case "NO ANSWER", "NO_ANSWER":
		return asteriskpb.Disposition_DISPOSITION_NO_ANSWER
	case "BUSY":
		return asteriskpb.Disposition_DISPOSITION_BUSY
	case "FAILED":
		return asteriskpb.Disposition_DISPOSITION_FAILED
	default:
		return asteriskpb.Disposition_DISPOSITION_UNSPECIFIED
	}
}

func dispositionToString(d asteriskpb.Disposition) string {
	switch d {
	case asteriskpb.Disposition_DISPOSITION_ANSWERED:
		return "ANSWERED"
	case asteriskpb.Disposition_DISPOSITION_NO_ANSWER:
		return "NO ANSWER"
	case asteriskpb.Disposition_DISPOSITION_BUSY:
		return "BUSY"
	case asteriskpb.Disposition_DISPOSITION_FAILED:
		return "FAILED"
	default:
		return ""
	}
}
