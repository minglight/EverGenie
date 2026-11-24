package roster

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
)

// 秘書名冊的健檢（doctor 的 config 群組；2026-11-01 從 doctor 搬來，doctor 只收集）：
// 人設卡與帳號對不對得上、角色來自停用的套件。
// 「事故負責人」（[incidents] owner）的檢查 S5-3 拿掉了：核心不再自動開事故單，設定還讀得到但沒人用（PLT-14）。

// Health 是底層 agent 的名冊檢查：Problems 是紅字（秘書起不來），Warnings 是黃字（照樣起得來）。
type Health interface {
	Problems(ctx context.Context) []string
	Warnings(ctx context.Context) []string
}

// Checks 登記名冊的健檢；沒接 Deps.Health 就不登記。
func (m *Module) Checks() []app.Check {
	if m.d.Health == nil {
		return nil
	}
	return []app.Check{
		{Name: "config.agent", RunAll: m.checkProblems},
		{Name: "config.agent_feature", RunAll: m.checkFeatureWarnings},
	}
}

// checkProblems：人設卡或帳號對不上，每個問題一條紅字；沒有問題給一條綠字。
func (m *Module) checkProblems(ctx context.Context) []app.CheckResult {
	problems := m.d.Health.Problems(ctx)
	if len(problems) == 0 {
		return []app.CheckResult{{Status: app.StatusOK, Message: "秘書的人設卡與帳號都對得上"}}
	}
	out := make([]app.CheckResult, 0, len(problems))
	for _, p := range problems {
		out = append(out, app.CheckResult{Status: app.StatusError, Message: p, Fix: "修正 vault/agents/<秘書>/agent.md，或補上 personas/ 裡的人設卡、home.toml 裡的帳號"})
	}
	return out
}

// checkFeatureWarnings：角色來自停用、被拒或移除的套件，秘書照樣起得來，只黃字（REQ-359）。
func (m *Module) checkFeatureWarnings(ctx context.Context) []app.CheckResult {
	var out []app.CheckResult
	for _, p := range m.d.Health.Warnings(ctx) {
		out = append(out, app.CheckResult{Status: app.StatusWarn, Message: p, Fix: "在設定 → 職位與秘書幫這位秘書換一位角色，或把那個套件開起來、裝回來"})
	}
	return out
}
