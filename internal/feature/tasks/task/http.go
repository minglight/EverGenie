package task

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "tasks"，實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "tasks", Impl: &service{m: m}}}
}

// service 實作 sdk.TasksAPI：驗操作者、直接呼叫本模組、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.TasksAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Create(ctx context.Context, req *sdk.TasksCreateRequest) (*sdk.TasksCreateResponse, error) {
	if _, err := platform.ActorName(req.By); err != nil {
		return nil, s.fail(err)
	}
	t, err := s.m.Create(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksCreateResponse{Task: t}, nil
}
func (s *service) Get(ctx context.Context, req *sdk.TasksGetRequest) (*sdk.TasksGetResponse, error) {
	t, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksGetResponse{Task: t}, nil
}
func (s *service) List(ctx context.Context, req *sdk.TasksListRequest) (*sdk.TasksListResponse, error) {
	list, err := s.m.List(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksListResponse{Tasks: values(list)}, nil
}
func (s *service) Update(ctx context.Context, req *sdk.TasksUpdateRequest) (*sdk.TasksUpdateResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	t, err := s.m.Update(ctx, req.ID, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksUpdateResponse{Task: t}, nil
}
func (s *service) Submit(ctx context.Context, req *sdk.TasksSubmitRequest) (*sdk.TasksSubmitResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	t, err := s.m.Submit(ctx, req.ID, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksSubmitResponse{Task: t}, nil
}
func (s *service) Accept(ctx context.Context, req *sdk.TasksAcceptRequest) (*sdk.TasksAcceptResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	t, err := s.m.Accept(ctx, req.ID, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksAcceptResponse{Task: t}, nil
}
func (s *service) Archive(ctx context.Context, req *sdk.TasksArchiveRequest) (*sdk.TasksArchiveResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	archived, err := s.m.Archive(ctx, req.IDS, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksArchiveResponse{Archived: archived}, nil
}
func (s *service) Reject(ctx context.Context, req *sdk.TasksRejectRequest) (*sdk.TasksRejectResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	t, err := s.m.Reject(ctx, req.ID, req.Note, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksRejectResponse{Task: t}, nil
}
func (s *service) Close(ctx context.Context, req *sdk.TasksCloseRequest) (*sdk.TasksCloseResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	t, err := s.m.Close(ctx, req.ID, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksCloseResponse{Task: t}, nil
}
func (s *service) Reconcile(ctx context.Context, req *sdk.TasksReconcileRequest) (*sdk.TasksReconcileResponse, error) {
	list, err := s.m.Reconcile(ctx, req.Agent, s.m.d.Clock.Now())
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksReconcileResponse{Tasks: values(list)}, nil
}
func (s *service) Stale(ctx context.Context, req *sdk.TasksStaleRequest) (*sdk.TasksStaleResponse, error) {
	list, err := s.m.Stale(ctx, s.m.d.Clock.Now())
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksStaleResponse{Tasks: values(list)}, nil
}
func (s *service) Waiting(ctx context.Context, req *sdk.TasksWaitingRequest) (*sdk.TasksWaitingResponse, error) {
	list, err := s.m.Waiting(ctx, s.m.d.Clock.Now())
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.TasksWaitingResponse{Tasks: list}, nil
}

func values(in []Task) []sdk.Task {
	out := make([]sdk.Task, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}
