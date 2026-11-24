package goal

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "goals"，實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "goals", Impl: &service{m: m}}}
}

// service 實作 sdk.GoalsAPI：驗操作者、直接呼叫本模組、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.GoalsAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Propose(ctx context.Context, req *sdk.GoalsProposeRequest) (*sdk.GoalsProposeResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	g, err := s.m.Propose(ctx, ProposeSpec{Title: req.Title, Text: req.Text, Criteria: req.Criteria, Agent: req.Agent, By: name})
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsProposeResponse{Goal: g}, nil
}

func (s *service) Create(ctx context.Context, req *sdk.GoalsCreateRequest) (*sdk.GoalsCreateResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	g, err := s.m.Create(ctx, CreateSpec{Title: req.Title, Text: req.Text, Criteria: req.Criteria, Agent: req.Agent, By: name})
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsCreateResponse{Goal: g}, nil
}

func (s *service) Get(ctx context.Context, req *sdk.GoalsGetRequest) (*sdk.GoalsGetResponse, error) {
	g, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsGetResponse{Goal: g}, nil
}

func (s *service) List(ctx context.Context, req *sdk.GoalsListRequest) (*sdk.GoalsListResponse, error) {
	list, err := s.m.List(ctx, req.States)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsListResponse{Goals: values(list)}, nil
}

func (s *service) UpdatePlanbook(ctx context.Context, req *sdk.GoalsUpdatePlanbookRequest) (*sdk.GoalsUpdatePlanbookResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	g, err := s.m.UpdatePlanbook(ctx, req.ID, req.Body, req.Note, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsUpdatePlanbookResponse{Goal: g}, nil
}

func (s *service) Done(ctx context.Context, req *sdk.GoalsDoneRequest) (*sdk.GoalsDoneResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	g, err := s.m.Done(ctx, req.ID, req.Summary, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsDoneResponse{Goal: g}, nil
}

func (s *service) Withdraw(ctx context.Context, req *sdk.GoalsWithdrawRequest) (*sdk.GoalsWithdrawResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	g, err := s.m.Withdraw(ctx, req.ID, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.GoalsWithdrawResponse{Goal: g}, nil
}

func values[T any](in []*T) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		if v != nil {
			out = append(out, *v)
		}
	}
	return out
}
