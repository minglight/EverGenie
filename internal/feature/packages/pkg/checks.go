package pkg

import (
	"context"
	"fmt"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 套件的健檢（doctor 的 packages 群組；2026-11-01 從 doctor 搬來，doctor 只收集）：
// 引擎套件就緒了沒、使用者的套件載不載得起來、官方套件有沒有被改過。

// EngineStatuses 是引擎套件的狀態（engine 監督它們，packages 只列出；registry 用 SetEngines 接 engine 模組）。
type EngineStatuses interface {
	Statuses() []engine.Status
}

// SetEngines 接上引擎套件的狀態（packages.engine 健檢用）；沒接就不登記那一項。
func (m *Module) SetEngines(e EngineStatuses) { m.mu.Lock(); defer m.mu.Unlock(); m.engines = e }

// Checks 登記套件的健檢。
func (m *Module) Checks() []app.Check {
	m.mu.Lock()
	engines := m.engines
	m.mu.Unlock()
	var out []app.Check
	if engines != nil {
		out = append(out, app.Check{Name: "packages.engine", RunAll: func(context.Context) []app.CheckResult { return engineChecks(engines.Statuses()) }})
	}
	return append(out,
		app.Check{Name: "packages.official_edited", RunAll: m.checkOfficialEdited},
		app.Check{Name: "packages.feature_broken", RunAll: m.checkBrokenFeatures},
		app.Check{Name: "packages.prompt_append", RunAll: m.checkPromptAppends})
}

// engineChecks：不認得的引擎、找不到執行檔是紅字，還沒就緒是黃字；都在跑給一條綠字。
func engineChecks(statuses []engine.Status) []app.CheckResult {
	var out []app.CheckResult
	for _, s := range statuses {
		who := strings.Join(s.Accounts, "、")
		switch {
		case s.Ready:
		case !s.Known:
			out = append(out, app.CheckResult{Status: app.StatusError, Message: fmt.Sprintf("帳號 %s 指定的引擎「%s」這個版本沒有內建", who, s.ID), Fix: "把 home.toml 該帳號的 engine 改成可用的引擎"})
		case !s.Found:
			out = append(out, app.CheckResult{Status: app.StatusError, Message: fmt.Sprintf("找不到引擎「%s」的執行檔（帳號 %s 會連不上）", s.ID, who), Fix: "把引擎執行檔放在 EverGenie 旁邊，或用 EVERGENIE_ENGINE_BIN_DIR 指到放它的資料夾"})
		default:
			out = append(out, app.CheckResult{Status: app.StatusWarn, Message: fmt.Sprintf("引擎「%s」還沒就緒（帳號 %s）", s.ID, who), Fix: "稍等一下再檢查；一直沒好請看 runtime/logs"})
		}
	}
	if len(out) > 0 {
		return out
	}
	if len(statuses) == 0 {
		return []app.CheckResult{{Status: app.StatusOK, Message: "沒有設定需要引擎的帳號"}}
	}
	return []app.CheckResult{{Status: app.StatusOK, Message: fmt.Sprintf("%d 個引擎套件都在跑", len(statuses))}}
}

// checkOfficialEdited：官方套件資料夾被改過（SEC-302 S8，REQ-333），每個一條黃字；沙盒家（[runtime] sandbox）不報。
func (m *Module) checkOfficialEdited(ctx context.Context) []app.CheckResult {
	m.mu.Lock()
	home, edited := m.home, m.editedOfficial
	m.mu.Unlock()
	if home == "" {
		return nil
	}
	if h, err := platform.LoadHome(home); err != nil || h.Runtime.Sandbox {
		return nil
	}
	if edited == nil {
		edited = EditedOfficial
	}
	var out []app.CheckResult
	for _, id := range edited(ctx, home) {
		out = append(out, app.CheckResult{Status: app.StatusWarn, Message: "官方套件「" + id + "」的資料夾被改過（跟 git 上的版本不同）",
			Fix: "官方套件跟著發版，不要直接改；要加東西用 `evergenie packages new`，要改官方行為走擴充點。確定是誤改就還原那個資料夾"})
	}
	return out
}

// checkBrokenFeatures：使用者的套件（自己的 mine、分享的 installed）載不起來（rejected）或行程掛了（failed）時，每個一條黃字（SEC-302 S4、REQ-353）。
// 一個套件壞掉只影響它自己（PKC-8、PKG-9），所以只黃字、不紅字；官方套件不在這裡報（它跟著發版）。拿不到清單就不報（別的檢查照常）。
func (m *Module) checkBrokenFeatures(ctx context.Context) []app.CheckResult {
	res, err := m.List(ctx, &sdk.PackageListRequest{})
	if err != nil || res == nil {
		return nil
	}
	var out []app.CheckResult
	for _, p := range res.Packages {
		if p.Tier != sdk.TierMine && p.Tier != sdk.TierInstalled {
			continue
		}
		if !p.Enabled || (p.State != sdk.PackageStateRejected && p.State != sdk.PackageStateFailed) {
			continue
		}
		who, fix := "你的套件", "照原因修正 packages/mine/"+p.ID+"/ 的 package.toml（或請秘書處理），存檔後會自動重新載入；不用了可以在設定 → 套件停用它"
		if p.Tier == sdk.TierInstalled {
			who, fix = "分享來的套件", "分享來的套件不要自己改：請作者出新版後用 evergenie packages upgrade "+p.ID+" 更新，或在設定 → 套件停用、解除安裝"
		}
		name := p.ID
		if p.Name != "" && p.Name != p.ID {
			name = p.Name + "（" + p.ID + "）"
		}
		detail := who + "「" + name + "」載不起來，其他套件照常"
		if p.Error != "" {
			detail += "：" + p.Error
		}
		out = append(out, app.CheckResult{Status: app.StatusWarn, Message: detail, Fix: fix})
	}
	return out
}

var _ app.CheckProvider = (*Module)(nil)
