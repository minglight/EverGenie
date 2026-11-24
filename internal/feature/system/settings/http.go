package settings

import (
	"context"
	"errors"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// systemMethods 是 sdk 的 system 服務裡由這裡實作的方法（網址 /api/system/<名稱> 不變）。
var systemMethods = []string{"get_config", "set_config", "get_mode", "set_mode", "set_chat_display", "get_work_max", "set_work_max", "backups", "preflight"}

// webMethods 是 sdk 的 web 服務裡由這裡實作的方法：看板連多台的設定（sites.go）、手機連線（phone.go）。
var webMethods = []string{"get_sites", "set_sites", "set_allow_boards", "set_owner_login", "get_phone", "phone_setup", "phone_close_funnel"}

// Routes 登記要掛的路由（app.RouteProvider）：system 服務的上面九個方法、web 服務的上面四個方法由這裡實作。
func (m *Module) Routes() []app.Mount {
	impl := &service{m: m}
	return []app.Mount{{Service: "system", Impl: impl, Methods: systemMethods}, {Service: "web", Impl: impl, Methods: webMethods}}
}

// service 是 system 服務的九個方法加 web 服務的四個方法（編譯期檢查簽名跟 sdk.SystemAPI、sdk.WebAPI 一致）。
type service struct{ m *Module }

var _ interface {
	GetConfig(context.Context, *sdk.SystemGetConfigRequest) (*sdk.SystemGetConfigResponse, error)
	SetConfig(context.Context, *sdk.SystemSetConfigRequest) (*sdk.SystemSetConfigResponse, error)
	GetMode(context.Context, *sdk.SystemGetModeRequest) (*sdk.SystemGetModeResponse, error)
	SetMode(context.Context, *sdk.SystemSetModeRequest) (*sdk.SystemSetModeResponse, error)
	SetChatDisplay(context.Context, *sdk.SystemSetChatDisplayRequest) (*sdk.SystemSetChatDisplayResponse, error)
	GetWorkMax(context.Context, *sdk.SystemGetWorkMaxRequest) (*sdk.SystemGetWorkMaxResponse, error)
	SetWorkMax(context.Context, *sdk.SystemSetWorkMaxRequest) (*sdk.SystemSetWorkMaxResponse, error)
	Backups(context.Context, *sdk.SystemBackupsRequest) (*sdk.SystemBackupsResponse, error)
	Preflight(context.Context, *sdk.SystemPreflightRequest) (*sdk.SystemPreflightResponse, error)
	GetSites(context.Context, *sdk.WebGetSitesRequest) (*sdk.WebGetSitesResponse, error)
	SetSites(context.Context, *sdk.WebSetSitesRequest) (*sdk.WebSetSitesResponse, error)
	SetAllowBoards(context.Context, *sdk.WebSetAllowBoardsRequest) (*sdk.WebSetAllowBoardsResponse, error)
	SetOwnerLogin(context.Context, *sdk.WebSetOwnerLoginRequest) (*sdk.WebSetOwnerLoginResponse, error)
	GetPhone(context.Context, *sdk.WebGetPhoneRequest) (*sdk.WebGetPhoneResponse, error)
	PhoneSetup(context.Context, *sdk.WebPhoneSetupRequest) (*sdk.WebPhoneSetupResponse, error)
	PhoneCloseFunnel(context.Context, *sdk.WebPhoneCloseFunnelRequest) (*sdk.WebPhoneCloseFunnelResponse, error)
} = (*service)(nil)

// fail 記下沒預料到的錯誤原因，回對外的錯誤（內部錯誤不外洩原文）。
func (s *service) fail(err error) error {
	out := platform.SDKError(err)
	if log := s.m.logger(); out.Code == sdk.ErrorCodeInternal && log != nil && !errors.Is(err, context.Canceled) {
		log.Error("request failed", "error", err)
	}
	return out
}

// home 回家的路徑；還不知道家在哪裡就回 unavailable。
func (s *service) home() (string, error) {
	home := s.m.homeDir()
	if home == "" {
		return "", platform.Unavailable("還不知道家在哪裡")
	}
	return home, nil
}
