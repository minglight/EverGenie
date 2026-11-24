package charter

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "rules"。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "rules", Impl: &service{m: m}}}
}

// service 實作 sdk.RulesAPI：驗操作者、呼叫本模組、把錯誤轉成 API 錯誤。誰能寫由模組判斷（主人才能改、秘書只能提案）。
type service struct{ m *Module }

var _ sdk.RulesAPI = (*service)(nil)

func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) List(ctx context.Context, _ *sdk.RulesListRequest) (*sdk.RulesListResponse, error) {
	out, err := s.m.List(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) Get(ctx context.Context, req *sdk.RulesGetRequest) (*sdk.RulesGetResponse, error) {
	out, err := s.m.Get(ctx, req.Doc)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) Save(ctx context.Context, req *sdk.RulesSaveRequest) (*sdk.RulesSaveResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := s.m.Save(ctx, req, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) History(ctx context.Context, req *sdk.RulesHistoryRequest) (*sdk.RulesHistoryResponse, error) {
	list, err := s.m.History(ctx, req.Doc)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RulesHistoryResponse{Versions: list}, nil
}

func (s *service) Version(ctx context.Context, req *sdk.RulesVersionRequest) (*sdk.RulesVersionResponse, error) {
	out, err := s.m.Version(ctx, req.ID, req.Against)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) Restore(ctx context.Context, req *sdk.RulesRestoreRequest) (*sdk.RulesRestoreResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := s.m.Restore(ctx, req, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) Propose(ctx context.Context, req *sdk.RulesProposeRequest) (*sdk.RulesProposeResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	p, err := s.m.Propose(ctx, req, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RulesProposeResponse{Proposal: p}, nil
}

func (s *service) Proposals(ctx context.Context, req *sdk.RulesProposalsRequest) (*sdk.RulesProposalsResponse, error) {
	list, err := s.m.Proposals(ctx, req.Doc, req.Statuses)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.RulesProposalsResponse{Proposals: list}, nil
}

func (s *service) Decide(ctx context.Context, req *sdk.RulesDecideRequest) (*sdk.RulesDecideResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := s.m.Decide(ctx, req, by)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}
