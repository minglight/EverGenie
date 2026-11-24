package engine

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 這兩句是 CheckPermissions 在「沒辦法比對」時放進結果的白話（不是產物不一致）；doctor 依它們分辨 warn 與 error。
const (
	NotRunning        = "這個帳號的引擎沒有在跑，無法檢查"
	CheckFailedPrefix = "無法檢查："
)

// permissionCheckTimeout 是對每個帳號送 DryRun 的上限，引擎卡住不會拖住整個 doctor。
const permissionCheckTimeout = 3 * time.Second

// CheckPermissions 對每個設定過的帳號送 SetPermission(DryRun)：不寫任何檔，只比對引擎的權限產物跟目前政策。
// 回傳「帳號 → 不一致的地方」；空串列表示一致。帳號的引擎沒在跑，該帳號放 NotRunning（doctor 只報告，不替它啟動）。
func (m *Module) CheckPermissions(ctx context.Context) (map[string][]string, error) {
	m.mu.Lock()
	r, policy := m.registry, m.currentPolicy()
	m.mu.Unlock()
	if r == nil {
		return nil, platform.Unavailable("引擎服務尚未啟動")
	}
	type target struct {
		name string
		api  sdk.EngineAPI
	}
	r.mu.RLock()
	targets := make([]target, 0, len(r.accounts))
	for name, a := range r.accounts {
		targets = append(targets, target{name, r.clients[a.engine]})
	}
	r.mu.RUnlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	out := make(map[string][]string, len(targets))
	for _, t := range targets {
		if t.api == nil {
			out[t.name] = []string{NotRunning}
			continue
		}
		call, cancel := context.WithTimeout(ctx, permissionCheckTimeout)
		resp, err := t.api.SetPermission(call, &sdk.EngineSetPermissionRequest{Account: t.name, Policy: policy, DryRun: true})
		cancel()
		switch {
		case ctx.Err() != nil:
			return nil, ctx.Err()
		case err != nil:
			out[t.name] = []string{CheckFailedPrefix + failureText(err)}
		case resp == nil || len(resp.Drift) == 0:
			out[t.name] = []string{}
		default:
			out[t.name] = append([]string(nil), resp.Drift...)
		}
	}
	return out, nil
}

// failureText 只取引擎回的白話 message；沒有就用一句通用的，不帶內部細節。
func failureText(err error) string {
	var e *sdk.Error
	if errors.As(err, &e) && e.Message != "" {
		return e.Message
	}
	return "引擎沒有回應"
}

// Status 是一個引擎套件（以引擎 id 為單位）目前的狀況，給 doctor 的 packages 檢查。
type Status struct {
	ID       string
	Accounts []string // 用這個引擎的帳號
	Known    bool     // 在內建登記表裡
	Found    bool     // 找得到執行檔（或已有人接上，例如測試）
	Ready    bool     // 已經通過握手、可以接受呼叫
}

// Statuses 依 id 排序回傳每個帳號設定用到的引擎。
func (m *Module) Statuses() []Status {
	m.mu.Lock()
	r := m.registry
	missing := map[string]bool{}
	for id := range m.missing {
		missing[id] = true
	}
	m.mu.Unlock()
	if r == nil {
		return nil
	}
	binaries := registry.Executables()
	r.mu.RLock()
	defer r.mu.RUnlock()
	byID := map[string]*Status{}
	for name, a := range r.accounts {
		s := byID[a.engine]
		if s == nil {
			_, known := binaries[a.engine]
			s = &Status{ID: a.engine, Known: known, Ready: r.clients[a.engine] != nil}
			s.Found = s.Ready || (known && !missing[a.engine])
			byID[a.engine] = s
		}
		s.Accounts = append(s.Accounts, name)
	}
	out := make([]Status, 0, len(byID))
	for _, s := range byID {
		sort.Strings(s.Accounts)
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Attach 把一個引擎 id 直接接到一個已經在跑的 EngineAPI（不經行程監督）。給測試與組裝用；要在 Start 之後呼叫。
func (m *Module) Attach(engineID string, api sdk.EngineAPI) error {
	m.mu.Lock()
	r := m.registry
	m.mu.Unlock()
	if r == nil {
		return platform.Unavailable("引擎服務尚未啟動")
	}
	r.mu.Lock()
	r.clients[engineID] = api
	r.mu.Unlock()
	return nil
}
