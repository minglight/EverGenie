package roster

import (
	"context"
	"errors"
	"sort"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 看板要的名冊資料（sdk 的 web 服務裡 get_roster、get_agents）：已啟用的秘書（人名、頭像）與設定頁「職位與秘書」的全部秘書。
// 人名讀現任人設，頭像網址 /avatars/<id>.png 由看板外殼（base/app/board）送檔。

// Directory 是名冊的唯讀查詢（由 agent 提供）：全部秘書，以及現任人設的人名。
type Directory interface {
	List(ctx context.Context) ([]*sdk.Record, error)
	PersonaName(ctx context.Context, agentID string) (string, error)
}

// Avatars 回某位秘書的動態頭像網址（由 base/app/board 的 Assets 提供，registry 注入）：沒有動態頭像回空字串。
type Avatars interface {
	VideoURL(agentID, persona string) string
	VideoURLs(agentID, persona string) []string // 所有動態（同一張可以有多支，DEV-125）；VideoURL 是第 1 個
	// HasImage：這個人設現在有沒有頭像圖（DEV-157）；沒有時名冊不給 avatar_url，看板直接畫名字、不去抓 404。
	HasImage(persona string) bool
}

// webService 只有 web 服務的 get_roster、get_agents（編譯期檢查簽名跟 sdk.WebAPI 一致）。
type webService struct{ m *Module }

var _ interface {
	GetRoster(context.Context, *sdk.WebGetRosterRequest) (*sdk.WebGetRosterResponse, error)
	GetAgents(context.Context, *sdk.WebGetAgentsRequest) (*sdk.WebGetAgentsResponse, error)
} = (*webService)(nil)

// agentName 讀現任人設的人名；讀不到退回 display_name，再退回職位 id。
func (m *Module) agentName(ctx context.Context, a *sdk.Record) string {
	if m.d.Directory != nil {
		if name, err := m.d.Directory.PersonaName(ctx, a.ID); err == nil && name != "" {
			return name
		}
	}
	if s, _ := a.Fields["display_name"].(string); s != "" {
		return s
	}
	return a.ID
}

// directory 是名冊上全部的秘書（依 id 排序）；讀不到回 unavailable。
func (m *Module) directory(ctx context.Context) ([]*sdk.Record, error) {
	if m.d.Directory == nil {
		return nil, nil
	}
	list, err := m.d.Directory.List(ctx)
	if err != nil {
		if m.log != nil && !errors.Is(err, context.Canceled) {
			m.log.Error("讀名冊失敗", "error", err)
		}
		return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "讀不到秘書名冊，稍後再試"}
	}
	out := make([]*sdk.Record, 0, len(list))
	for _, a := range list {
		if a != nil {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *webService) GetRoster(ctx context.Context, req *sdk.WebGetRosterRequest) (*sdk.WebGetRosterResponse, error) {
	list, err := s.m.directory(ctx)
	if err != nil {
		return nil, err
	}
	out := []sdk.RosterAgent{}
	for _, a := range list {
		if enabled, _ := a.Fields["enabled"].(bool); !enabled {
			continue
		}
		duty, _ := a.Fields["duty"].(string)
		color, _ := a.Fields["color"].(string)
		dark, _ := a.Fields["color_dark"].(string)
		persona, _ := a.Fields["persona"].(string)
		video, videos, avatar := "", []string(nil), "/avatars/"+a.ID+".png"
		if s.m.d.Avatars != nil {
			videos = s.m.d.Avatars.VideoURLs(a.ID, persona)
			if len(videos) > 0 {
				video = videos[0] // video_url 就是第 1 支
			}
			if !s.m.d.Avatars.HasImage(persona) {
				avatar = "" // 沒有圖：不給網址，看板畫名字（DEV-157）
			}
		}
		out = append(out, sdk.RosterAgent{ID: a.ID, Name: s.m.agentName(ctx, a), Duty: duty, Color: color, ColorDark: dark, AvatarURL: avatar, Enabled: true, VideoURL: video, VideoURLs: videos, Persona: persona})
	}
	return &sdk.WebGetRosterResponse{Agents: out}, nil
}

// GetAgents 是設定頁「職位與秘書」的資料：名冊上全部秘書（含停用）加上 home.toml 的帳號名稱。
func (s *webService) GetAgents(ctx context.Context, req *sdk.WebGetAgentsRequest) (*sdk.WebGetAgentsResponse, error) {
	list, err := s.m.directory(ctx)
	if err != nil {
		return nil, err
	}
	str := func(a *sdk.Record, key string) string {
		v, _ := a.Fields[key].(string)
		return v
	}
	out := &sdk.WebGetAgentsResponse{Agents: []sdk.AgentSetting{}}
	for _, a := range list {
		enabled, _ := a.Fields["enabled"].(bool)
		out.Agents = append(out.Agents, sdk.AgentSetting{
			ID: a.ID, Name: s.m.agentName(ctx, a), Duty: str(a, "duty"), Persona: str(a, "persona"),
			Account: str(a, "account"), Model: str(a, "model"), Effort: str(a, "effort"), Enabled: enabled,
		})
	}
	if s.m.home != "" {
		if h, err := platform.LoadHome(s.m.home); err != nil {
			if s.m.log != nil {
				s.m.log.Warn("讀不到 home.toml 的帳號", "error", err)
			}
		} else {
			for name := range h.Accounts {
				if name != "default" {
					out.Accounts = append(out.Accounts, name)
				}
			}
			sort.Strings(out.Accounts)
		}
	}
	return out, nil
}
