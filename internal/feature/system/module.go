// internal/feature/system/module.go — 說明卡 ai_docs/modules/system.md。

// Package system 是業務塊 system 的組裝（S8-3）：AI 帳號（accounts）、家的設定（settings）、規矩頁（charter）、
// 健康檢查（doctor）、版本與更新（update）五個子模組，加上只給 update 用的本機函式庫（host）；資料 rules_version、
// rules_proposal 屬於這一塊。這個檔只組裝：規則都在子套件。
package system

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/accounts"
	"github.com/minglight/evergenie/internal/feature/system/backupsweep"
	"github.com/minglight/evergenie/internal/feature/system/charter"
	"github.com/minglight/evergenie/internal/feature/system/doctor"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/report"
	"github.com/minglight/evergenie/internal/feature/system/settings"
	"github.com/minglight/evergenie/internal/feature/system/update"
	"github.com/minglight/evergenie/sdk"
)

// Engine 是底層 engine 給這一塊的：帳號用量（accounts）、跑起來需要的東西齊不齊（settings 的 system.preflight）。可以是 nil。
type Engine interface {
	accounts.Usage
	settings.PreflightSource
}

// PushKeys 是 inbox 塊的推播金鑰（settings 的手機連線用；registry 填）。
type PushKeys = settings.PushKeys

// Deps 只列塊外的東西。doctor 要的 session 是底層 session（建在 tasks、trust、agents 塊之後），所以這一塊要在 session 之後建。
type Deps struct {
	Engine   Engine
	Vault    charter.VaultStore   // 底層 vault：規矩頁的版本與提案
	Notify   charter.Notifier     // inbox 塊的 notify：提案通知主人；可以是 nil
	Bus      charter.Bus          // 底層 bus：提案結果回給秘書；可以是 nil
	Sessions doctor.SessionAccess // 底層 session：看板健康頁列常駐秘書；可以是 nil
	Roster   doctor.Roster        // 底層 agent：同上；可以是 nil
	Clock    platform.Clock
	// Closer 是底層 session 的「把開著的對話全部收掉」：整個家還原前叫（homebackup，HBK-12）；可以是 nil
	Closer homebackup.Sessions
	Push   PushKeys // inbox 塊的推播金鑰：看板「手機連線」順便做好（DEV-153）；可以是空的
	// Events 是底層 bus 的發事件與訂閱：每週的備份掃描聽排程、出錯發 system.alert（backupsweep，DEV-163）；可以是 nil
	Events backupsweep.Events
}

// Module 是 system 塊：Start 依 accounts → settings → charter → doctor → update → homebackup → report → backupsweep，Stop 反序；登記合併各子模組的。
// 塊裡有一個消費者：doctor 收各業務模組的健檢（ConsumeChecks），經 app.Group 轉。
type Module struct {
	*app.Group
	accounts *accounts.Module
	settings *settings.Module
	charter  *charter.Module
	doctor   *doctor.Module
	update   *update.Module
}

// New 建五個子模組；不做事（MOD-N4）。
func New(d Deps) *Module {
	a := accounts.New(accounts.Deps{Usage: d.Engine, Clock: d.Clock})
	s := settings.New(settings.Deps{Engine: d.Engine, Clock: d.Clock, Push: d.Push})
	c := charter.New(charter.Deps{Vault: d.Vault, Notify: d.Notify, Bus: d.Bus, Clock: d.Clock})
	doc := doctor.New(doctor.Deps{Sessions: d.Sessions, Roster: d.Roster})
	u := update.New(update.Deps{Clock: d.Clock})
	// 整個家的備份與還原（DEV-153）；擋「備份比程式新」要知道核心認得哪些資料整理，由 update 給。
	hb := homebackup.New(homebackup.Deps{Notify: d.Notify, Sessions: d.Closer, Clock: d.Clock, KnownMigrations: func(string) []string { return KnownMigrations() }})
	rep := report.New(report.Deps{Doctor: doc, Offline: doctor.OfflineChecks, Clock: d.Clock}) // 回報問題（DEV-153）：只有路由，沒有背景工作
	sweep := backupsweep.New(backupsweep.Deps{Bus: d.Events, Clock: d.Clock})                  // 每週的備份掃描（DEV-163）
	return &Module{Group: app.NewGroup("system", a, s, c, doc, u, hb, rep, sweep), accounts: a, settings: s, charter: c, doctor: doc, update: u}
}

// Doctor 是健康檢查子模組。
func (m *Module) Doctor() *doctor.Module { return m.doctor }

// PrintDoctor 是 doctor 的印法（trust 塊的指令印權限健檢要用；套件層級，指令行程不建 Module）。
func PrintDoctor(e *cmdline.Env, checks []sdk.DoctorCheck) error { return doctor.Print(e, checks) }

// Commands 合併五個子套件的指令（套件層級）：preflight 是 cmd/evergenie 的本機安裝前檢查（settings 的 preflight 指令），
// extra 是 doctor 的額外離線檢查（cmd/evergenie 的 launchd、tailscale，agents 塊角色庫的 OfflineChecks）。
func Commands(preflight cmdline.HostFunc, extra ...doctor.OfflineCheck) (map[string]app.Command, error) {
	collect := func(e *cmdline.Env, home string) []sdk.DoctorCheck { return doctor.Collect(e, home, extra) }
	return cmdline.Merge(accounts.Commands(), settings.Commands(preflight), charter.Commands(), doctor.Commands(extra...), update.Commands(), homebackup.Commands(), report.Commands(collect), backupsweep.Commands())
}

// KnownMigrations 回核心認得的家的資料整理 id：homebackup 拿來擋「備份比程式新」，cmd/evergenie 的「從備份建家」也用同一個規則。
func KnownMigrations() []string {
	out := make([]string, 0, len(update.HomeMigrations))
	for _, m := range update.HomeMigrations {
		out = append(out, m.ID)
	}
	return out
}

var _ app.Module = (*Module)(nil)
