package update

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的路由（app.RouteProvider）：sdk 的 system 服務裡只有 update_status、update_check、update_apply、update_decide、
// update_rollback 五個方法由這裡實作（網址不變：/api/system/update_*）。規則與權限（只有主人）都在 Module，這裡只轉手並把錯誤轉成 API 錯誤。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "system", Impl: &service{m: m}, Methods: []string{"update_status", "update_check", "update_apply", "update_decide", "update_rollback"}}}
}

type service struct{ m *Module }

var _ interface {
	UpdateStatus(context.Context, *sdk.UpdateStatusRequest) (*sdk.UpdateInfo, error)
	UpdateCheck(context.Context, *sdk.UpdateCheckRequest) (*sdk.UpdateInfo, error)
	UpdateApply(context.Context, *sdk.UpdateApplyRequest) (*sdk.UpdateInfo, error)
	UpdateDecide(context.Context, *sdk.UpdateDecideRequest) (*sdk.UpdateInfo, error)
	UpdateRollback(context.Context, *sdk.UpdateRollbackRequest) (*sdk.UpdateInfo, error)
} = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if log := s.m.logger(); out.Code == sdk.ErrorCodeInternal && log != nil && !errors.Is(err, context.Canceled) {
		log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) UpdateStatus(ctx context.Context, req *sdk.UpdateStatusRequest) (*sdk.UpdateInfo, error) {
	info, err := s.m.Status(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return info, nil
}

func (s *service) UpdateCheck(ctx context.Context, req *sdk.UpdateCheckRequest) (*sdk.UpdateInfo, error) {
	info, err := s.m.CheckNow(ctx, req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	return info, nil
}

func (s *service) UpdateApply(ctx context.Context, req *sdk.UpdateApplyRequest) (*sdk.UpdateInfo, error) {
	info, err := s.m.Apply(ctx, req.By, req.Version)
	if err != nil {
		return nil, s.fail(err)
	}
	return info, nil
}

func (s *service) UpdateDecide(ctx context.Context, req *sdk.UpdateDecideRequest) (*sdk.UpdateInfo, error) {
	info, err := s.m.Decide(ctx, req.By, req.Version, req.Decision)
	if err != nil {
		return nil, s.fail(err)
	}
	return info, nil
}

func (s *service) UpdateRollback(ctx context.Context, req *sdk.UpdateRollbackRequest) (*sdk.UpdateInfo, error) {
	info, err := s.m.Rollback(ctx, req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	return info, nil
}
