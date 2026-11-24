package job

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "scheduler"（sdk 的 SchedulerAPI），實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "scheduler", Impl: &service{m: m}}}
}

// service 實作 sdk.SchedulerAPI：驗操作者、直接呼叫本模組、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.SchedulerAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) ListJobs(ctx context.Context, req *sdk.SchedulerListJobsRequest) (*sdk.SchedulerListJobsResponse, error) {
	jobs, err := s.m.ListJobs(ctx, req.Agent, req.IncludeArchived)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SchedulerListJobsResponse{Jobs: jobs}, nil
}

func (s *service) SaveJob(ctx context.Context, req *sdk.SchedulerSaveJobRequest) (*sdk.SchedulerSaveJobResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	j, err := s.m.SaveJob(ctx, req.Job, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SchedulerSaveJobResponse{Job: j}, nil
}

func (s *service) SetJob(ctx context.Context, req *sdk.SchedulerSetJobRequest) (*sdk.SchedulerSetJobResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	j, err := s.m.SetJob(ctx, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SchedulerSetJobResponse{Job: j}, nil
}

func (s *service) RemoveJob(ctx context.Context, req *sdk.SchedulerRemoveJobRequest) (*sdk.SchedulerRemoveJobResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	j, archived, err := s.m.RemoveJob(ctx, req.ID, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SchedulerRemoveJobResponse{Job: j, Archived: archived}, nil
}

func (s *service) RunJob(ctx context.Context, req *sdk.SchedulerRunJobRequest) (*sdk.SchedulerRunJobResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	j, at, err := s.m.RunJob(ctx, req.ID, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SchedulerRunJobResponse{Job: j, At: at}, nil
}
