package settings

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/session"
	"github.com/minglight/evergenie/sdk"
)

// 家的設定與介面模式（system.get_config、set_config、get_mode、set_mode），加上工作對話上限（get_work_max、set_work_max）。

// GetConfig 讀 home.toml（evergenie config get|show）。只讀。
func (s *service) GetConfig(ctx context.Context, req *sdk.SystemGetConfigRequest) (*sdk.SystemGetConfigResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	value, text, err := platform.GetHomeValue(home, req.Key)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SystemGetConfigResponse{Key: req.Key, Value: value, Text: text}, nil
}

// SetConfig 改 home.toml 的一個值（evergenie config set）；只有主人可以。
func (s *service) SetConfig(ctx context.Context, req *sdk.SystemSetConfigRequest) (*sdk.SystemSetConfigResponse, error) {
	who, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if !platform.IsOwner(who) {
		return nil, s.fail(platform.Forbidden("只有主人能改家的設定"))
	}
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	backup, value, err := platform.SetHomeValue(home, req.Key, req.Value, s.m.now())
	if err != nil {
		return nil, s.fail(err)
	}
	if log := s.m.logger(); log != nil {
		log.Info("家的設定已更新", "by", who, "key", req.Key, "backup", backup)
	}
	return &sdk.SystemSetConfigResponse{Key: req.Key, Value: value, Backup: backup}, nil
}

// GetMode 讀介面模式（home.toml 的 mode，沒寫就是 simple），加上家的身分（env、標籤、電腦代號，看板標出是哪個家）
// 與看板連多台要的版本、介面等級、對外網址。只讀。
func (s *service) GetMode(ctx context.Context, req *sdk.SystemGetModeRequest) (*sdk.SystemGetModeResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return nil, s.fail(err)
	}
	return &sdk.SystemGetModeResponse{
		Mode: sdk.HomeMode(h.Mode), ChatDisplay: sdk.ChatDisplay(h.ChatDisplay), Env: sdk.HomeEnv(h.Runtime.Env), Label: h.Label(), Machine: h.Runtime.Machine, UserName: h.User.Name,
		Version: platform.Version(), BoardAPI: sdk.BoardAPILevel, PublicURL: h.PublicBase(),
	}, nil
}

// SetMode 改介面模式：走跟 SetConfig 一樣的備份與驗證；只有主人可以。
func (s *service) SetMode(ctx context.Context, req *sdk.SystemSetModeRequest) (*sdk.SystemSetModeResponse, error) {
	who, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if !platform.IsOwner(who) {
		return nil, s.fail(platform.Forbidden("只有主人能改介面模式"))
	}
	if req.Mode != sdk.HomeModeSimple && req.Mode != sdk.HomeModeFull {
		return nil, s.fail(platform.Invalid("mode", "介面模式只能是 simple 或 full"))
	}
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	backup, _, err := platform.SetHomeValue(home, "mode", `"`+string(req.Mode)+`"`, s.m.now())
	if err != nil {
		return nil, s.fail(err)
	}
	if log := s.m.logger(); log != nil {
		log.Info("介面模式已更新", "by", who, "mode", string(req.Mode), "backup", backup)
	}
	return &sdk.SystemSetModeResponse{Mode: req.Mode, Backup: backup}, nil
}

// SetChatDisplay 改對話的顯示方式：走跟 SetConfig 一樣的備份與驗證；只有主人可以。
func (s *service) SetChatDisplay(ctx context.Context, req *sdk.SystemSetChatDisplayRequest) (*sdk.SystemSetChatDisplayResponse, error) {
	who, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if !platform.IsOwner(who) {
		return nil, s.fail(platform.Forbidden("只有主人能改對話的顯示方式"))
	}
	if req.ChatDisplay != sdk.ChatDisplayDefault && req.ChatDisplay != sdk.ChatDisplayEngineer {
		return nil, s.fail(platform.Invalid("chat_display", "對話的顯示方式只能是 default 或 engineer"))
	}
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	backup, _, err := platform.SetHomeValue(home, "chat_display", `"`+string(req.ChatDisplay)+`"`, s.m.now())
	if err != nil {
		return nil, s.fail(err)
	}
	if log := s.m.logger(); log != nil {
		log.Info("對話的顯示方式已更新", "by", who, "chat_display", string(req.ChatDisplay), "backup", backup)
	}
	return &sdk.SystemSetChatDisplayResponse{ChatDisplay: req.ChatDisplay, Backup: backup}, nil
}

// GetWorkMax 讀同時進行的工作對話上限（settings/sessions.toml 的 work_max，SES-73）；預設值與範圍都來自 session（只定義一次）。只讀。
func (s *service) GetWorkMax(ctx context.Context, req *sdk.SystemGetWorkMaxRequest) (*sdk.SystemGetWorkMaxResponse, error) {
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	n, set, err := session.ReadWorkMax(home)
	if err != nil {
		if log := s.m.logger(); log != nil {
			log.Warn("settings/sessions.toml 讀不懂，工作對話上限照預設", "error", err)
		}
	}
	return &sdk.SystemGetWorkMaxResponse{WorkMax: n, Set: set, Default: session.DefaultWorkMax, Min: session.MinWorkMax, Max: session.MaxWorkMax}, nil
}

// SetWorkMax 改工作對話上限：只動 work_max 那一行（其他內容與註解留著）、先備份；只有主人可以。改完不用重開（開工作對話前每次重讀）。
func (s *service) SetWorkMax(ctx context.Context, req *sdk.SystemSetWorkMaxRequest) (*sdk.SystemSetWorkMaxResponse, error) {
	who, err := platform.ActorName(req.By)
	if err != nil {
		return nil, s.fail(err)
	}
	if !platform.IsOwner(who) {
		return nil, s.fail(platform.Forbidden("只有主人能改同時進行的對話上限"))
	}
	home, err := s.home()
	if err != nil {
		return nil, s.fail(err)
	}
	backup, err := session.WriteWorkMax(home, req.WorkMax, s.m.now())
	if err != nil {
		if log := s.m.logger(); log != nil {
			log.Warn("工作對話上限沒改成", "file", session.WorkSettingsPath(home), "work_max", req.WorkMax, "error", err)
		}
		return nil, s.fail(err)
	}
	if log := s.m.logger(); log != nil {
		log.Info("工作對話上限已更新", "by", who, "work_max", req.WorkMax, "backup", backup)
	}
	return &sdk.SystemSetWorkMaxResponse{WorkMax: req.WorkMax, Backup: backup}, nil
}
