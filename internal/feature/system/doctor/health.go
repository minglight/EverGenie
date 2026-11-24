package doctor

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 看板設定頁「系統健康」的簡單版（sdk 的 `web.get_health`，REQ-110）：看板、核心服務、推播、每位常駐秘書各一項。
// 它跟 `system.doctor` 同屬健檢，所以由 doctor 登記（Mount.Methods 只掛這個方法）；推播一項直接用 doctor 自己的 push 群組（D-14），不複製判斷。

// Roster 是健康頁要的名冊讀取（由 agent 提供，registry 注入）。
type Roster interface {
	List(ctx context.Context) ([]*sdk.Record, error)
	PersonaName(ctx context.Context, agentID string) (string, error)
}

// sessionPhrase 把常駐 session 的狀態換成白話；ok 為 false 代表要你注意。
func sessionPhrase(s sdk.SessionState) (string, bool) {
	switch s {
	case sdk.SessionStateIdle:
		return "待命中", true
	case sdk.SessionStateBusy:
		return "處理中", true
	case sdk.SessionStateWaiting:
		return "在等你處理", true
	case sdk.SessionStateStarting:
		return "啟動中", true
	case sdk.SessionStateLimited:
		return "額度用完，等恢復", true // 不是壞掉，恢復後自己補送（REQ-378）
	case sdk.SessionStateStopped, "":
		return "沒有在跑", false
	default:
		// 不認得的狀態（例如 S5 拿掉的 stuck）不猜它是好是壞，也不把代碼秀給主人；要你看一下。
		return "狀態不明", false
	}
}

// stuckLister 是 session「額外有」的查詢：現在話送不進去的對話（SES-77，DEV-162）。沒有就不看。
type stuckLister interface {
	Stuck(ctx context.Context) []platform.SessionStuck
}

// Health 是 `web.get_health` 的實作。
func (m *Module) Health(ctx context.Context) (*sdk.WebGetHealthResponse, error) {
	items := []sdk.HealthItem{{Name: "看板", Ok: true, Detail: "看板運作中"}}
	var sessions []*sdk.Session
	coreOK := false
	if m.d.Sessions != nil {
		if list, err := m.d.Sessions.List(ctx, "", nil); err == nil {
			coreOK, sessions = true, list
		}
	}
	if coreOK {
		items = append(items, sdk.HealthItem{Name: "核心服務", Ok: true, Detail: "核心服務運作中"})
	} else {
		items = append(items, sdk.HealthItem{Name: "核心服務", Ok: false, Detail: "連不到核心服務"})
	}
	items = append(items, m.pushHealth(ctx, coreOK))
	if m.d.Roster == nil {
		return &sdk.WebGetHealthResponse{Items: items}, nil
	}
	list, err := m.d.Roster.List(ctx)
	if err != nil {
		if log := m.logger(); log != nil && !errors.Is(err, context.Canceled) {
			log.Error("讀名冊失敗", "error", err)
		}
		return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "讀不到秘書名冊，稍後再試"}
	}
	agents := make([]*sdk.Record, 0, len(list))
	for _, a := range list {
		if a != nil {
			agents = append(agents, a)
		}
	}
	sort.Slice(agents, func(i, j int) bool { return agents[i].ID < agents[j].ID })
	// 話送不進去的秘書（她任何一個對話）：狀態寫「待命中」也不算正常（DCT-10）。
	stuck := map[string]platform.SessionStuck{}
	if sl, ok := m.d.Sessions.(stuckLister); ok && coreOK {
		for _, s := range sl.Stuck(ctx) {
			if cur, seen := stuck[s.Agent]; seen {
				s.Count += cur.Count
				if cur.Minutes > s.Minutes {
					s.Minutes = cur.Minutes
				}
			}
			stuck[s.Agent] = s
		}
	}
	for _, a := range agents {
		enabled, _ := a.Fields["enabled"].(bool)
		if mode, _ := a.Fields["mode"].(string); !enabled || (mode != "" && mode != "resident") {
			continue
		}
		item := sdk.HealthItem{Name: m.agentName(ctx, a)}
		if !coreOK {
			item.Detail = "核心服務連不上，無法確認"
			items = append(items, item)
			continue
		}
		state := sdk.SessionStateStopped
		for _, s := range sessions {
			if s != nil && s.Agent == a.ID && s.Kind == sdk.SessionKindResident {
				state = s.State
				break
			}
		}
		item.Detail, item.Ok = sessionPhrase(state)
		if s, ok := stuck[a.ID]; ok {
			item.Ok, item.Detail = false, fmt.Sprintf("有 %d 則話送不進去，已經等了 %d 分鐘；請重開她的對話", s.Count, s.Minutes)
		}
		items = append(items, item)
	}
	return &sdk.WebGetHealthResponse{Items: items}, nil
}

// agentName 讀現任人設的人名；讀不到退回 display_name，再退回職位 id。
func (m *Module) agentName(ctx context.Context, a *sdk.Record) string {
	if name, err := m.d.Roster.PersonaName(ctx, a.ID); err == nil && name != "" {
		return name
	}
	if s, _ := a.Fields["display_name"].(string); s != "" {
		return s
	}
	return a.ID
}

// pushHealth 是健康檢查的「推播」一項（D-14：金鑰設定好而且至少一台裝置開啟才算 ok）。
func (m *Module) pushHealth(ctx context.Context, coreOK bool) sdk.HealthItem {
	item := sdk.HealthItem{Name: "推播"}
	if !coreOK {
		item.Detail = "核心服務連不上，無法確認"
		return item
	}
	res, err := m.Doctor(ctx, &sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{sdk.DoctorGroupPush}})
	if err != nil || len(res.Checks) == 0 {
		item.Detail = "核心服務連不上，無法確認"
		return item
	}
	item.Ok, item.Detail = res.Checks[0].Ok, res.Checks[0].Detail
	return item
}
