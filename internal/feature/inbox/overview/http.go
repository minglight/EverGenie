package overview

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的路由（app.RouteProvider）：sdk 的 web 服務裡只有 get_overview 一個方法由這裡實作
// （同一個服務的其他方法由 menu、trust、agents、doctor 各自登記）。網址不變：/api/web/get_overview。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{{Service: "web", Impl: &service{m: m}, Methods: []string{"get_overview"}}}
}

// service 只有 web 服務的 get_overview（編譯期檢查簽名跟 sdk.WebAPI 一致）。
type service struct{ m *Module }

var _ interface {
	GetOverview(context.Context, *sdk.WebGetOverviewRequest) (*sdk.WebGetOverviewResponse, error)
} = (*service)(nil)

// GetOverview 回總覽；失敗時 sdk 錯誤原樣轉出，連不上一律 unavailable（白話）。
func (s *service) GetOverview(ctx context.Context, req *sdk.WebGetOverviewRequest) (*sdk.WebGetOverviewResponse, error) {
	out, err := s.m.Overview(ctx)
	if err != nil {
		return nil, platform.UpstreamError(err)
	}
	return out, nil
}
