// internal/feature/system/update/module.go — ai_docs/modules/system.md（REQ-277, REQ-280, REQ-281）。
package update

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
)

// Deps 是 update 的依賴。升級本身在另一個行程（evergenie upgrade），這個模組只負責「每天檢查、今晚更新」與看板要的現況。
type Deps struct {
	Clock platform.Clock
}

// Module 是升級業務模組：每天檢查新版、到時間叫起「今晚更新」，並回答看板「版本與更新」卡要的現況、檢查、更新、決定。
type Module struct {
	d    Deps
	home string

	mu  sync.Mutex
	log *slog.Logger

	// 測試縫：預設是真的做法。
	findCore func(h *platform.Home, exe string) string
	launch   func(home, exe, tag string) (string, error)
	rescue   func(home, exe string) (string, error) // 換不回去之後再試一次換回（upgrade --rollback）
	check    func(ctx context.Context, h *platform.Home, core string) (*UpgradeCheck, error)
}

// New 不做任何 I/O（MOD-N4）。
func New(d Deps) *Module {
	m := &Module{d: d, findCore: FindInstall, launch: startDetached, rescue: startDetachedRollback}
	m.check = m.realCheck
	return m
}

// Name 是模組名稱。
func (m *Module) Name() string { return "update" }

// SetHome 設定家的資料夾；啟動前呼叫。
func (m *Module) SetHome(dir string) { m.home = dir }

func (m *Module) now() time.Time {
	if m.d.Clock != nil {
		return m.d.Clock.Now()
	}
	return time.Now()
}

func (m *Module) coreDir() (*platform.Home, string) {
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return nil, ""
	}
	exe, _ := os.Executable()
	return h, m.findCore(h, exe)
}

// CoreFound：找得到 EverGenie 程式（原始碼版的 git repo，或下載版的安裝資料夾；FindInstall）。找不到＝這台不是用安裝程式裝的，沒辦法自動升級。
func (m *Module) CoreFound() bool {
	_, core := m.coreDir()
	return core != ""
}

func (m *Module) realCheck(ctx context.Context, h *platform.Home, core string) (*UpgradeCheck, error) {
	u := newUpgrader(h, core, nil)
	if u.Channel == platform.UpdateChannelOff {
		u.Channel = platform.UpdateChannelRelease // 主人按「檢查更新」就是要查：關掉自動檢查不代表不能手動查
	}
	return u.Check(ctx)
}

// Start 只開 log；每分鐘的檢查是 Jobs() 登記的定時工作（2026-11-01 從自己的 ticker 改過來）。
func (m *Module) Start(ctx context.Context) error {
	if h, err := platform.LoadHome(m.home); err == nil {
		l := platform.NewLogger(h, "update")
		m.mu.Lock()
		m.log = l
		m.mu.Unlock()
	}
	return ctx.Err()
}

// Stop 沒有自己的背景工作要收（定時工作由 timer 的 JobRunner 先停）。
func (m *Module) Stop(context.Context) error { return nil }

// Jobs 登記每分鐘一次的檢查：到時間查新版、叫起今晚更新。沙盒家（[runtime] sandbox）不自動檢查、不自動升級。
func (m *Module) Jobs() []app.Job {
	return []app.Job{{Name: "update.check", Every: time.Minute, Run: func(ctx context.Context) error {
		if h, err := platform.LoadHome(m.home); err != nil || h.Runtime.Sandbox {
			return nil
		}
		m.tick(ctx, m.now())
		return nil
	}}}
}

var _ app.JobProvider = (*Module)(nil)

func (m *Module) tick(ctx context.Context, now time.Time) {
	h, core := m.coreDir()
	if h == nil || core == "" || h.Update.Channel == "" || h.Update.Channel == platform.UpdateChannelOff {
		return
	}
	if st, _ := ReadUpgradeState(h.Dir); st != nil && UpgradeRunning(st.Phase) && now.Sub(st.StartedAt) < time.Hour {
		return // 正在升級
	}
	exe, _ := os.Executable()
	// 今晚更新
	if d, _ := ReadUpgradeDecision(h.Dir); d != nil && d.Version != "" && !now.Before(d.At) {
		_ = ClearUpgradeDecision(h.Dir)
		tag := "v" + d.Version
		if chk, _ := ReadUpgradeCheck(h.Dir); chk != nil && chk.Target == d.Version && chk.Tag != "" {
			tag = chk.Tag
		}
		_, _ = m.launch(h.Dir, exe, tag)
		return
	}
	// 每天檢查
	if m.dailyCheckDue(h, now) {
		chk, err := m.check(ctx, h, core)
		if err == nil && chk.Target != "" && !contains(UpgradeNotified(h.Dir), chk.Target) {
			notifyUpgrade(h.Dir, "有新版 "+chk.Target+"："+firstNoteLine(chk.Notes), "到「設定 → 系統健康」可以現在更新、今晚更新或跳過這版。")
			_ = MarkUpgradeNotified(h.Dir, chk.Target)
		}
	}
}

// dailyCheckDue：今天的 check_at 已經過了，而且最後一次檢查是在今天的 check_at 之前。
func (m *Module) dailyCheckDue(h *platform.Home, now time.Time) bool {
	loc := h.Location()
	if loc == nil {
		loc = time.Local
	}
	n := now.In(loc)
	at, err := time.ParseInLocation("15:04", h.Update.CheckAt, loc)
	if err != nil {
		return false
	}
	due := time.Date(n.Year(), n.Month(), n.Day(), at.Hour(), at.Minute(), 0, 0, loc)
	if n.Before(due) {
		return false
	}
	chk, _ := ReadUpgradeCheck(h.Dir)
	return chk == nil || chk.CheckedAt.Before(due)
}

// firstNoteLine 取發版說明的第一句（略過 Markdown 的標題與項目符號）。
func firstNoteLine(notes string) string {
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if line = strings.TrimSpace(strings.TrimLeft(line, "-* ")); line != "" {
			return line
		}
	}
	return "有新東西與修正"
}

func (m *Module) logger() *slog.Logger { m.mu.Lock(); defer m.mu.Unlock(); return m.log }

// 登記方法（app 的登記介面）：Routes 在 http.go，Commands 在 cli.go。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
