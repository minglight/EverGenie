package requests

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的 sdk 服務（app.RouteProvider）：服務名 "policy"，實作是下面的 service。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "policy", Impl: &service{m: m}}}
}

// service 實作 sdk.PolicyAPI：驗操作者、直接呼叫本模組、把錯誤轉成 API 錯誤。
type service struct{ m *Module }

var _ sdk.PolicyAPI = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if out.Code == sdk.ErrorCodeInternal && s.m.log != nil && !errors.Is(err, context.Canceled) {
		s.m.log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Request(ctx context.Context, req *sdk.PolicyRequestRequest) (*sdk.PolicyRequestResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	q, err := s.m.Request(ctx, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PolicyRequestResponse{Request: q}, nil
}

func (s *service) List(ctx context.Context, req *sdk.PolicyListRequest) (*sdk.PolicyListResponse, error) {
	list, err := s.m.List(ctx, req.Statuses, req.Agent)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PolicyListResponse{Requests: list}, nil
}

func (s *service) Get(ctx context.Context, req *sdk.PolicyGetRequest) (*sdk.PolicyGetResponse, error) {
	q, err := s.m.Get(ctx, req.ID)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PolicyGetResponse{Request: q}, nil
}

// Decide 只有主人（by 以 owner( 開頭）能呼叫；秘書回合 403（模組裡判斷，REQ-168）。
func (s *service) Decide(ctx context.Context, req *sdk.PolicyDecideRequest) (*sdk.PolicyDecideResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	q, err := s.m.Decide(ctx, req, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PolicyDecideResponse{Request: q}, nil
}

func (s *service) Revoke(ctx context.Context, req *sdk.PolicyRevokeRequest) (*sdk.PolicyRevokeResponse, error) {
	name, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	q, err := s.m.Revoke(ctx, req.ID, req.Note, name)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.PolicyRevokeResponse{Request: q}, nil
}
