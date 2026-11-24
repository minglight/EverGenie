package engine

import (
	"context"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 權限變更申請（Q7，D-26）：policy 模組核准後，guidance 經 PushPolicy 寫進每個帳號的原生設定，
// rule 在新開 session 時經 Start 交給引擎。來源是 cmd 組裝時接上的函式（engine 不 import policy）。

// SetGuidanceSource 設定「目前有效的 guidance」來源；SetPermission（握手）、PushPolicy、doctor 的 DryRun 都帶它。
func (m *Module) SetGuidanceSource(src func() []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guidance = src
}

// SetRuleSource 設定「某位秘書目前有效的規則」來源；開 session 時填進 EngineStartRequest.Rules。
func (m *Module) SetRuleSource(src func(agent string) []sdk.PolicyRule) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rules = src
	if m.registry != nil {
		m.registry.mu.Lock()
		m.registry.rules = src
		m.registry.mu.Unlock()
	}
}

// currentPolicy 是 trust.toml 算出的政策加上目前有效的 guidance（呼叫端持有 m.mu）。
func (m *Module) currentPolicy() *sdk.PermissionPolicy {
	if m.policy == nil {
		return nil
	}
	p := *m.policy
	p.Guidance = nil
	if m.guidance != nil {
		p.Guidance = append([]string(nil), m.guidance()...)
	}
	return &p
}

// pushTimeout 是 PushPolicy 對每個帳號的上限。
const pushTimeout = 5 * time.Second

// PushPolicy 把目前的政策（含 guidance）重新推給每個已就緒的帳號；有一個失敗就回錯誤（policy 模組會還原再推一次）。
// 引擎還沒就緒的帳號略過：它握手時會拿到最新的政策。
func (m *Module) PushPolicy(ctx context.Context) error {
	m.mu.Lock()
	r := m.registry
	policy := m.currentPolicy()
	m.mu.Unlock()
	if r == nil {
		return platform.Unavailable("引擎服務尚未啟動")
	}
	type target struct {
		name string
		api  sdk.EngineAPI
	}
	r.mu.RLock()
	var targets []target
	for name, a := range r.accounts {
		if api := r.clients[a.engine]; api != nil {
			targets = append(targets, target{name, api})
		}
	}
	r.mu.RUnlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].name < targets[j].name })
	for _, t := range targets {
		call, cancel := context.WithTimeout(ctx, pushTimeout)
		_, err := t.api.SetPermission(call, &sdk.EngineSetPermissionRequest{Account: t.name, Policy: policy})
		cancel()
		if err != nil {
			return fromSDKError(err)
		}
	}
	return nil
}
