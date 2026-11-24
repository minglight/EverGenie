package accounts

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的路由（app.RouteProvider）：sdk 的 system 服務裡只有 accounts、save_account、usage 三個方法由這裡實作，
// 其餘（doctor、config、備份…）仍由別人掛。網址不變：/api/system/accounts、save_account、usage。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "system", Impl: &service{m: m}, Methods: []string{"accounts", "save_account", "usage"}}}
}

// service 只有 system 服務的三個方法（編譯期檢查簽名跟 sdk.SystemAPI 一致）。
type service struct{ m *Module }

var _ interface {
	Accounts(context.Context, *sdk.SystemAccountsRequest) (*sdk.SystemAccountsResponse, error)
	SaveAccount(context.Context, *sdk.SystemSaveAccountRequest) (*sdk.SystemSaveAccountResponse, error)
	Usage(context.Context, *sdk.SystemUsageRequest) (*sdk.SystemUsageResponse, error)
} = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if log := s.m.logger(); out.Code == sdk.ErrorCodeInternal && log != nil && !errors.Is(err, context.Canceled) {
		log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Accounts(ctx context.Context, req *sdk.SystemAccountsRequest) (*sdk.SystemAccountsResponse, error) {
	out, err := s.m.Show(ctx)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) SaveAccount(ctx context.Context, req *sdk.SystemSaveAccountRequest) (*sdk.SystemSaveAccountResponse, error) {
	out, err := s.m.Upsert(ctx, req.By, req.Account, req.MakeDefault)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) Usage(ctx context.Context, req *sdk.SystemUsageRequest) (*sdk.SystemUsageResponse, error) {
	out, err := s.m.Usages(ctx, req.Account)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}
