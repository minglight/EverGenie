package rules

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的路由（app.RouteProvider）：sdk 的 system 服務裡只有 trust、set_permission_levels 兩個方法由這裡實作，
// 其餘（doctor、config、備份…）仍由別人掛。網址不變：/api/system/trust、/api/system/set_permission_levels。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "system", Impl: &service{m: m}, Methods: []string{"trust", "set_permission_levels"}}, {Service: "web", Impl: &service{m: m}, Methods: []string{"get_trust"}}}
}

// service 只有 system 服務的兩個方法（編譯期檢查簽名跟 sdk.SystemAPI 一致）。
type service struct{ m *Module }

var _ interface {
	Trust(context.Context, *sdk.SystemTrustRequest) (*sdk.SystemTrustResponse, error)
	SetPermissionLevels(context.Context, *sdk.SystemSetPermissionLevelsRequest) (*sdk.SystemSetPermissionLevelsResponse, error)
} = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if log := s.m.logger(); out.Code == sdk.ErrorCodeInternal && log != nil && !errors.Is(err, context.Canceled) {
		log.Error("request failed", "error", err)
	}
	return out
}

func (s *service) Trust(ctx context.Context, req *sdk.SystemTrustRequest) (*sdk.SystemTrustResponse, error) {
	out, err := s.m.Show(ctx, req)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) SetPermissionLevels(ctx context.Context, req *sdk.SystemSetPermissionLevelsRequest) (*sdk.SystemSetPermissionLevelsResponse, error) {
	by, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	out, err := s.m.SetLevels(ctx, by, req.DefaultLevel, req.Levels)
	if err != nil {
		return nil, s.fail(err)
	}
	return out, nil
}

func (s *service) GetTrust(ctx context.Context, req *sdk.WebGetTrustRequest) (*sdk.WebGetTrustResponse, error) {
	if s.m.homeDir() == "" {
		return nil, s.fail(platform.Unavailable("看板還沒有家，讀不到權限設定"))
	}
	h, err := platform.LoadHome(s.m.homeDir())
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.WebGetTrustResponse{Policy: Policy(h.Trust)}, nil
}
