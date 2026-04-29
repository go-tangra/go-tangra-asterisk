package service

import (
	"context"
	"time"

	kratoserr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	asteriskpb "github.com/go-tangra/go-tangra-asterisk/gen/go/asterisk/service/v1"
	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// DashboardService is a thin Prometheus proxy for the freepbx-exporter
// metrics. The frontend issues PromQL through these RPCs; this service
// keeps Prometheus out of the browser's reach.
type DashboardService struct {
	asteriskpb.UnimplementedAsteriskDashboardServiceServer

	log    *log.Helper
	client *data.PrometheusClient
}

func NewDashboardService(ctx *bootstrap.Context, client *data.PrometheusClient) *DashboardService {
	return &DashboardService{
		log:    ctx.NewLoggerHelper("asterisk/service/dashboard"),
		client: client,
	}
}

func (s *DashboardService) InstantQuery(ctx context.Context, req *asteriskpb.InstantQueryRequest) (*asteriskpb.InstantQueryResponse, error) {
	if s.client == nil {
		return nil, kratoserr.New(503, "PROMETHEUS_DISABLED", "prometheus url is not configured")
	}
	if req.Query == "" {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "query is required")
	}

	var ts time.Time
	if req.Time != nil {
		ts = req.Time.AsTime()
	}
	series, err := s.client.Query(ctx, req.Query, ts)
	if err != nil {
		s.log.WithContext(ctx).Errorf("instant query %q: %v", req.Query, err)
		return nil, kratoserr.InternalServer("PROMETHEUS_QUERY_FAILED", err.Error())
	}

	out := &asteriskpb.InstantQueryResponse{Series: make([]*asteriskpb.InstantSample, 0, len(series))}
	for _, s := range series {
		out.Series = append(out.Series, &asteriskpb.InstantSample{
			Labels:    s.Labels,
			Timestamp: timestamppb.New(s.Sample.Time),
			Value:     s.Sample.Value,
			HasValue:  s.Sample.HasValue,
		})
	}
	return out, nil
}

func (s *DashboardService) RangeQuery(ctx context.Context, req *asteriskpb.RangeQueryRequest) (*asteriskpb.RangeQueryResponse, error) {
	if s.client == nil {
		return nil, kratoserr.New(503, "PROMETHEUS_DISABLED", "prometheus url is not configured")
	}
	if req.Query == "" {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "query is required")
	}
	if req.Start == nil || req.End == nil {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "start and end are required")
	}
	if req.StepSeconds <= 0 {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "step_seconds must be positive")
	}

	step := time.Duration(req.StepSeconds) * time.Second
	series, err := s.client.QueryRange(ctx, req.Query, req.Start.AsTime(), req.End.AsTime(), step)
	if err != nil {
		s.log.WithContext(ctx).Errorf("range query %q: %v", req.Query, err)
		return nil, kratoserr.InternalServer("PROMETHEUS_QUERY_FAILED", err.Error())
	}

	out := &asteriskpb.RangeQueryResponse{Series: make([]*asteriskpb.RangeSeries, 0, len(series))}
	for _, s := range series {
		ts := make([]*timestamppb.Timestamp, 0, len(s.Samples))
		vals := make([]float64, 0, len(s.Samples))
		for _, sample := range s.Samples {
			ts = append(ts, timestamppb.New(sample.Time))
			vals = append(vals, sample.Value)
		}
		out.Series = append(out.Series, &asteriskpb.RangeSeries{
			Labels:     s.Labels,
			Timestamps: ts,
			Values:     vals,
		})
	}
	return out, nil
}
