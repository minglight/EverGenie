package rules

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 權限的健檢（doctor 的 permissions 群組；2026-11-01 從 doctor 搬來，doctor 只收集）：
// 各帳號引擎產生的權限設定檔跟 settings/trust.toml 一不一致。

// Permissions 是引擎的權限產物比對（engine.CheckPermissions，registry 注入）：帳號 → 不一致的地方（白話）。
type Permissions interface {
	CheckPermissions(ctx context.Context) (map[string][]string, error)
}

// Checks 登記權限的健檢；沒接 Deps.Engine 就沒有。
func (m *Module) Checks() []app.Check {
	if m.d.Engine == nil {
		return nil
	}
	return []app.Check{
		{Name: "permissions.drift", RunAll: func(ctx context.Context) []app.CheckResult { return m.permissions(ctx, false) }},
		{Name: "permissions.check", RunAll: func(ctx context.Context) []app.CheckResult { return m.permissions(ctx, true) }},
	}
}

// permissions 比對一次權限產物：unchecked 為真回「沒辦法檢查」的黃字（引擎沒在跑、比對失敗），否則回不一致的紅字
// （都一致給一條綠字）。兩項各比一次：比對只讀檔，重複一次換來兩個項目編號各管各的。
func (m *Module) permissions(ctx context.Context, unchecked bool) []app.CheckResult {
	result, err := m.d.Engine.CheckPermissions(ctx)
	if err != nil {
		if !unchecked {
			return nil
		}
		msg := "發生內部錯誤"
		if e := platform.SDKError(err); e.Code != sdk.ErrorCodeInternal {
			msg = e.Message
		}
		return []app.CheckResult{{Status: app.StatusWarn, Message: "沒辦法檢查權限產物：" + msg}}
	}
	accounts := make([]string, 0, len(result))
	for name := range result {
		accounts = append(accounts, name)
	}
	sort.Strings(accounts)
	var out []app.CheckResult
	for _, name := range accounts {
		for _, line := range result[name] {
			cannot := line == engine.NotRunning || strings.HasPrefix(line, engine.CheckFailedPrefix)
			switch {
			case cannot && unchecked:
				out = append(out, app.CheckResult{Status: app.StatusWarn, Message: fmt.Sprintf("帳號 %s：%s", name, line), Fix: "引擎起來之後再執行一次 evergenie doctor"})
			case !cannot && !unchecked:
				out = append(out, app.CheckResult{Status: app.StatusError, Message: fmt.Sprintf("帳號 %s：%s", name, line), Fix: "權限設定檔是自動產生的、不要手改；重啟 evergenie serve 會依 settings/trust.toml 重新產生"})
			}
		}
	}
	if unchecked || len(out) > 0 {
		return out
	}
	if len(accounts) == 0 {
		return []app.CheckResult{{Status: app.StatusOK, Message: "沒有設定帳號"}}
	}
	return []app.CheckResult{{Status: app.StatusOK, Message: fmt.Sprintf("%d 個帳號的權限設定檔都跟 settings/trust.toml 一致", len(accounts))}}
}
