package doctor

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Routes 登記要掛的路由（app.RouteProvider）：sdk 的 system 服務裡只有 doctor 一個方法、web 服務裡只有 get_health 一個方法由這裡實作。
// 網址不變：/api/system/doctor、/api/web/get_health。
func (m *Module) Routes() []app.Mount {
	return []app.Mount{
		{Service: "system", Impl: &service{m: m}, Methods: []string{"doctor"}},
		{Service: "web", Impl: &service{m: m}, Methods: []string{"get_health"}},
	}
}

// service 只有 system 的 doctor 與 web 的 get_health 兩個方法（編譯期檢查簽名跟 sdk.SystemAPI、sdk.WebAPI 一致）。
type service struct{ m *Module }

var _ interface {
	Doctor(context.Context, *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error)
	GetHealth(context.Context, *sdk.WebGetHealthRequest) (*sdk.WebGetHealthResponse, error)
} = (*service)(nil)

func (s *service) GetHealth(ctx context.Context, req *sdk.WebGetHealthRequest) (*sdk.WebGetHealthResponse, error) {
	out, err := s.m.Health(ctx)
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) {
			return nil, se
		}
		return nil, platform.SDKError(err)
	}
	return out, nil
}

func (s *service) Doctor(ctx context.Context, req *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	out, err := s.m.Doctor(ctx, req)
	if err != nil {
		e := platform.SDKError(err)
		if log := s.m.logger(); e.Code == sdk.ErrorCodeInternal && log != nil && !errors.Is(err, context.Canceled) {
			log.Error("request failed", "error", err)
		}
		return nil, e
	}
	return out, nil
}
