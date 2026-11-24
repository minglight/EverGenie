package update

import (
	"context"
	"os"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 版本與更新（P8 第二輪 S2、REQ-280、REQ-281）：看板「設定 → 系統健康」的「版本與更新」卡用。
// 讀的部分（現況）只讀 <家>/runtime/upgrade/ 底下的檔案；動手的部分（連網檢查、叫起升級行程）在這裡做。
// 升級本身在另一個行程跑（evergenie upgrade --apply），所以 Apply 立刻回、不等升級做完。
// 這些方法由 api 的 system 服務（sdk 的 system.update_*）轉過來；sdk 目前沒有獨立的 update 服務，見 module-pattern.md 的「踩過的坑」。

const noCoreDetail = "這台不是用安裝程式裝的，沒辦法自動升級"

func (m *Module) loadHome() (*platform.Home, error) {
	if m.home == "" {
		return nil, platform.Unavailable("還不知道家在哪裡")
	}
	return platform.LoadHome(m.home)
}

func (m *Module) owner(actor *sdk.Actor, what string) error {
	who, err := platform.ActorName(actor)
	if err != nil {
		return err
	}
	if !platform.IsOwner(who) {
		return platform.Forbidden("只有主人能" + what)
	}
	return nil
}

// info 把檔案裡的現況整理成 UpdateInfo。
func (m *Module) info(h *platform.Home) (*sdk.UpdateInfo, error) {
	info := &sdk.UpdateInfo{Current: platform.Version(), Channel: h.UpdateChannel(), TonightAt: h.Update.TonightAt, Skipped: ReadUpgradeSkipped(h.Dir)}
	info.CoreFound = m.CoreFound()
	chk, err := ReadUpgradeCheck(h.Dir)
	if err != nil {
		return nil, err
	}
	if chk != nil {
		info.Target, info.Tag, info.Notes, info.Detail, info.CheckedAt = chk.Target, chk.Tag, chk.Notes, chk.Detail, chk.CheckedAt
		// 檢查之後才跳過或升級過的版本：不再當成新版
		if info.Target != "" && (contains(info.Skipped, info.Target) || info.Target == info.Current) {
			info.Target, info.Tag, info.Notes = "", "", ""
			if info.Detail == "" {
				info.Detail = "已經是最新版"
			}
		}
	}
	if !info.CoreFound && info.Detail == "" {
		info.Detail = noCoreDetail
	}
	st, err := ReadUpgradeState(h.Dir)
	if err != nil {
		return nil, err
	}
	info.State = st
	info.Rescue = readRescue(h.Dir) != nil
	d, err := ReadUpgradeDecision(h.Dir)
	if err != nil {
		return nil, err
	}
	if d != nil && d.Version != "" {
		info.Decision, info.DecisionAt = d.Decision, d.At
	}
	return info, nil
}

// Status 回現況（誰都可以看）。
func (m *Module) Status(ctx context.Context) (*sdk.UpdateInfo, error) {
	h, err := m.loadHome()
	if err != nil {
		return nil, err
	}
	return m.info(h)
}

// CheckNow 連網檢查一次有沒有新版（只有主人能做），結果寫進 check.json。
func (m *Module) CheckNow(ctx context.Context, by *sdk.Actor) (*sdk.UpdateInfo, error) {
	if err := m.owner(by, "檢查更新"); err != nil {
		return nil, err
	}
	h, err := m.loadHome()
	if err != nil {
		return nil, err
	}
	exe, _ := os.Executable()
	core := m.findCore(h, exe)
	if core == "" {
		return nil, platform.Unavailable(noCoreDetail)
	}
	if _, err := m.check(ctx, h, core); err != nil {
		return nil, err
	}
	return m.info(h)
}

// target 找「目前檢查到的新版」：version 要等於它，不然 INVALID（畫面上的版本過期了，請重新檢查）。
func (m *Module) target(h *platform.Home, version string) (*UpgradeCheck, error) {
	chk, err := ReadUpgradeCheck(h.Dir)
	if err != nil {
		return nil, err
	}
	if chk == nil || chk.Target == "" {
		return nil, platform.Invalid("version", "現在沒有新版可以更新；先檢查一次更新")
	}
	if version != chk.Target {
		return nil, platform.Invalid("version", "要更新的版本（"+version+"）不是現在檢查到的新版（"+chk.Target+"），請重新整理再試")
	}
	return chk, nil
}

// Apply 叫起一個脫離的升級行程升到目前檢查到的新版（version 必須等於它），立刻回（只有主人能做）。
func (m *Module) Apply(ctx context.Context, by *sdk.Actor, version string) (*sdk.UpdateInfo, error) {
	if err := m.owner(by, "更新 EverGenie"); err != nil {
		return nil, err
	}
	h, err := m.loadHome()
	if err != nil {
		return nil, err
	}
	chk, err := m.target(h, version)
	if err != nil {
		return nil, err
	}
	if err := m.applyNow(h, chk); err != nil {
		return nil, err
	}
	return m.info(h)
}

func (m *Module) applyNow(h *platform.Home, chk *UpgradeCheck) error {
	exe, _ := os.Executable()
	if m.findCore(h, exe) == "" {
		return platform.Unavailable(noCoreDetail)
	}
	if st, _ := ReadUpgradeState(h.Dir); st != nil && UpgradeRunning(st.Phase) && time.Since(st.StartedAt) < time.Hour {
		return platform.Conflict("已經有一個升級在進行了")
	}
	if exe == "" {
		return platform.Unavailable("找不到 EverGenie 執行檔")
	}
	_, err := m.launch(m.home, exe, chk.Tag)
	return err
}

// Rollback 是看板首頁的「退回上一版」：上一次升級沒成功、也沒換回（rescue.json）時，叫起一個脫離的 `upgrade --rollback`
// 再試一次換回升級前的版本，立刻回（只有主人能做）。沒有換不回去的升級就 INVALID（不拿它去退別次升級）。
func (m *Module) Rollback(ctx context.Context, by *sdk.Actor) (*sdk.UpdateInfo, error) {
	if err := m.owner(by, "換回舊版"); err != nil {
		return nil, err
	}
	h, err := m.loadHome()
	if err != nil {
		return nil, err
	}
	if readRescue(h.Dir) == nil {
		return nil, platform.Invalid("rescue", "現在沒有要換回的升級")
	}
	exe, _ := os.Executable()
	if exe == "" {
		return nil, platform.Unavailable("找不到 EverGenie 執行檔")
	}
	if m.findCore(h, exe) == "" {
		return nil, platform.Unavailable(noCoreDetail)
	}
	if st, _ := ReadUpgradeState(h.Dir); st != nil && UpgradeRunning(st.Phase) && time.Since(st.StartedAt) < time.Hour {
		return nil, platform.Conflict("已經有一個升級在進行了")
	}
	if _, err := m.rescue(h.Dir, exe); err != nil {
		return nil, err
	}
	return m.info(h)
}

// Decide 記下主人對新版的決定：now（現在更新）、tonight（今晚更新）、skip（跳過這版）（只有主人能做）。
func (m *Module) Decide(ctx context.Context, by *sdk.Actor, version, decision string) (*sdk.UpdateInfo, error) {
	if err := m.owner(by, "決定要不要更新"); err != nil {
		return nil, err
	}
	h, err := m.loadHome()
	if err != nil {
		return nil, err
	}
	chk, err := m.target(h, version)
	if err != nil {
		return nil, err
	}
	switch decision {
	case "now":
		err = m.applyNow(h, chk)
	case "tonight":
		_, err = DecideTonight(h, chk.Target, m.now())
	case "skip":
		err = AddUpgradeSkipped(h.Dir, chk.Target)
		if err == nil {
			err = ClearUpgradeDecision(h.Dir)
		}
	default:
		err = platform.Invalid("decision", "決定只能是 now（現在更新）、tonight（今晚更新）或 skip（跳過這版）")
	}
	if err != nil {
		return nil, err
	}
	return m.info(h)
}
