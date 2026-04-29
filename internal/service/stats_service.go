package service

import (
	"context"

	kratoserr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/tx7do/kratos-bootstrap/bootstrap"
	"google.golang.org/protobuf/types/known/timestamppb"

	asteriskpb "github.com/go-tangra/go-tangra-asterisk/gen/go/asterisk/service/v1"
	"github.com/go-tangra/go-tangra-asterisk/internal/data"
)

// StatsService implements AsteriskStatsService.
type StatsService struct {
	asteriskpb.UnimplementedAsteriskStatsServiceServer

	log  *log.Helper
	repo *data.StatsRepo
}

func NewStatsService(ctx *bootstrap.Context, repo *data.StatsRepo) *StatsService {
	return &StatsService{
		log:  ctx.NewLoggerHelper("asterisk/service/stats"),
		repo: repo,
	}
}

func (s *StatsService) Overview(ctx context.Context, req *asteriskpb.OverviewRequest) (*asteriskpb.OverviewResponse, error) {
	if req.From == nil || req.To == nil {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from and to are required")
	}
	res, err := s.repo.Overview(ctx, data.OverviewFilter{
		From:   req.From.AsTime(),
		To:     req.To.AsTime(),
		Bucket: bucketFromProto(req.Bucket),
	})
	if err != nil {
		s.log.WithContext(ctx).Errorf("overview: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.OverviewResponse{
		TotalCalls:       res.TotalCalls,
		AnsweredCalls:    res.AnsweredCalls,
		MissedCalls:      res.MissedCalls,
		BusyCalls:        res.BusyCalls,
		FailedCalls:      res.FailedCalls,
		AvgPickupSeconds: res.AvgPickupSeconds,
		AvgTalkSeconds:   res.AvgTalkSeconds,
	}
	if res.TotalCalls > 0 {
		out.AnswerRate = float64(res.AnsweredCalls) / float64(res.TotalCalls)
	}
	for i := range res.Series {
		out.Series = append(out.Series, bucketToProto(&res.Series[i]))
	}
	return out, nil
}

func (s *StatsService) ListExtensionStats(ctx context.Context, req *asteriskpb.ListExtensionStatsRequest) (*asteriskpb.ListExtensionStatsResponse, error) {
	if req.From == nil || req.To == nil {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from and to are required")
	}
	stats, total, err := s.repo.ListExtensionStats(ctx, data.ExtensionStatsFilter{
		From:      req.From.AsTime(),
		To:        req.To.AsTime(),
		Extension: req.Extension,
		Page:      int(req.Page),
		PageSize:  int(req.PageSize),
	})
	if err != nil {
		s.log.WithContext(ctx).Errorf("list extension stats: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.ListExtensionStatsResponse{
		Extensions: make([]*asteriskpb.ExtensionStat, 0, len(stats)),
		Total:      total,
	}
	for i := range stats {
		out.Extensions = append(out.Extensions, extensionStatToProto(&stats[i]))
	}
	return out, nil
}

func (s *StatsService) GetExtensionStats(ctx context.Context, req *asteriskpb.GetExtensionStatsRequest) (*asteriskpb.GetExtensionStatsResponse, error) {
	if req.Extension == "" {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "extension is required")
	}
	if req.From == nil || req.To == nil {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from and to are required")
	}

	res, err := s.repo.GetExtensionDrilldown(ctx, req.Extension, req.From.AsTime(), req.To.AsTime(), bucketFromProto(req.Bucket))
	if err != nil {
		s.log.WithContext(ctx).Errorf("extension drilldown: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.GetExtensionStatsResponse{
		Summary: extensionStatToProto(&res.Summary),
	}
	for i := range res.Series {
		out.Series = append(out.Series, bucketToProto(&res.Series[i]))
	}
	for i := range res.HourOfDay {
		out.HourOfDay = append(out.HourOfDay, bucketToProto(&res.HourOfDay[i]))
	}
	return out, nil
}

func extensionStatToProto(s *data.ExtensionStat) *asteriskpb.ExtensionStat {
	out := &asteriskpb.ExtensionStat{
		Extension:        s.Extension,
		DisplayName:      s.DisplayName,
		TotalCalls:       s.TotalCalls,
		AnsweredCalls:    s.AnsweredCalls,
		MissedCalls:      s.MissedCalls,
		InboundCalls:     s.InboundCalls,
		OutboundCalls:    s.OutboundCalls,
		TotalTalkSeconds: s.TotalTalkSeconds,
		HandledShare:     s.HandledShare,
		AvgPickupSeconds: s.AvgPickupSeconds,
		AvgTalkSeconds:   s.AvgTalkSeconds,
		BusiestHour:      s.BusiestHour,
	}
	if s.TotalCalls > 0 {
		out.MissRate = float64(s.MissedCalls) / float64(s.TotalCalls)
	}
	return out
}

func bucketToProto(b *data.TimeBucketCount) *asteriskpb.TimeBucketCount {
	return &asteriskpb.TimeBucketCount{
		BucketStart: timestamppb.New(b.BucketStart),
		Total:       b.Total,
		Answered:    b.Answered,
		Missed:      b.Missed,
	}
}

func (s *StatsService) RingGroupStats(ctx context.Context, req *asteriskpb.RingGroupStatsRequest) (*asteriskpb.RingGroupStatsResponse, error) {
	if req.RingGroup == "" {
		return nil, kratoserr.BadRequest("INVALID_ARGUMENT", "ring_group is required")
	}
	if req.From == nil || req.To == nil {
		return nil, kratoserr.BadRequest("INVALID_TIME_RANGE", "from and to are required")
	}

	res, err := s.repo.RingGroupStats(ctx, data.RingGroupStatsFilter{
		RingGroup: req.RingGroup,
		From:      req.From.AsTime(),
		To:        req.To.AsTime(),
	})
	if err != nil {
		s.log.WithContext(ctx).Errorf("ring group stats: %v", err)
		return nil, kratoserr.InternalServer("MYSQL_UNAVAILABLE", err.Error())
	}

	out := &asteriskpb.RingGroupStatsResponse{
		RingGroup:    res.RingGroup,
		Total:        res.Total,
		Answered:     res.Answered,
		NoAnswer:     res.NoAnswer,
		AllBusy:      res.AllBusy,
		Failed:       res.Failed,
		MissedCalls:  make([]*asteriskpb.MissedRingGroupCall, 0, len(res.MissedCalls)),
	}
	for i := range res.MissedCalls {
		m := &res.MissedCalls[i]
		out.MissedCalls = append(out.MissedCalls, &asteriskpb.MissedRingGroupCall{
			Linkedid:    m.LinkedID,
			Calldate:    timestamppb.New(m.CallDate),
			Src:         m.Src,
			Clid:        m.Clid,
			Did:         m.DID,
			Disposition: dispositionFromString(m.Disposition),
			RingSeconds: m.RingSeconds,
		})
	}
	return out, nil
}

func bucketFromProto(b asteriskpb.TimeBucket) data.BucketGranularity {
	switch b {
	case asteriskpb.TimeBucket_TIME_BUCKET_HOUR:
		return data.BucketHour
	case asteriskpb.TimeBucket_TIME_BUCKET_DAY:
		return data.BucketDay
	case asteriskpb.TimeBucket_TIME_BUCKET_WEEK:
		return data.BucketWeek
	default:
		return data.BucketNone
	}
}
