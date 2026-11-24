// internal/feature/inbox/module.go — 說明卡 ai_docs/modules/inbox.md。

// Package inbox 是業務塊 inbox 的組裝（S8-3）：通知（notify）、首頁彙整（overview）、選單（menu）三個子模組，
// 加上只給 notify 用的推播通道（push）；資料 message 屬於這一塊。這個檔只組裝：規則都在子套件。
package inbox

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/feature/inbox/menu"
	"github.com/minglight/evergenie/internal/feature/inbox/notify"
	"github.com/minglight/evergenie/internal/feature/inbox/overview"
	"github.com/minglight/evergenie/internal/feature/inbox/push"
)

// Deps 只列塊外的東西；塊內的（notify 的推播通道、menu 的徽章數字從 overview 來）在 New 裡接。
// tasks 塊反過來要 notify 送通知，所以任務單不在這裡：建好 tasks 之後用 SetTasks 接。
type Deps struct {
	Bus      notify.BusSender
	Vault    notify.VaultStore
	Clock    notify.Clock
	Frontend menu.Frontend // 看板外殼的套件前端資源（base/app/board 的 Assets）；可以是 nil
	Roster   menu.Roster   // 名冊（底層 agent），選單的 show_when 用；可以是 nil
}

// Module 是 inbox 塊：Start 依 notify → overview → menu（registry 原本的先後），Stop 反序；登記合併三個子模組的。
// 塊裡有兩個消費者：notify 收各業務模組的通知來源（ConsumeNoticeSources），overview 收首頁資料段（ConsumeBoard），都經 app.Group 轉。
type Module struct {
	*app.Group
	push     *push.Service
	notify   *notify.Module
	overview *overview.Module
	menu     *menu.Module
}

// New 建三個子模組與推播通道；不做事（MOD-N4）。推播通道的家在 SetHome 時才給。
func New(d Deps) *Module {
	p := push.New("")
	n := notify.New(notify.Deps{Bus: d.Bus, Vault: d.Vault, Push: p, Clock: d.Clock})
	o := overview.New(overview.Deps{})
	mn := menu.New(menu.Deps{Frontend: d.Frontend, Roster: d.Roster, Records: d.Vault, Overview: o})
	return &Module{Group: app.NewGroup("inbox", n, o, mn), push: p, notify: n, overview: o, menu: mn}
}

// SetHome 交給子模組與推播通道。
func (m *Module) SetHome(dir string) {
	m.push.SetHome(dir)
	m.Group.SetHome(dir)
}

// SetTasks 接上任務單（notify 把「還在等你」記進單的 nudges、收掉已結束任務的訊息；在 Start 之前呼叫）。
func (m *Module) SetTasks(t notify.TaskNudges) { m.notify.SetTasks(t) }

// Notices 是通知子模組（各業務模組送通知、session 的「還在等你」）。
func (m *Module) Notices() *notify.Module { return m.notify }

// Overview 是首頁彙整子模組。
func (m *Module) Overview() *overview.Module { return m.overview }

// Menu 是選單子模組。
func (m *Module) Menu() *menu.Module { return m.menu }

// EnsurePushKeys：這個家沒有推播金鑰就做一組（回這次有沒有新做）；PushKeysReady：這個家有沒有金鑰。
// 套件層級（不看 Module 的狀態）：system 塊的「手機連線」經 registry 用（DEV-153），跟 `evergenie push init` 同一份。
func EnsurePushKeys(home string) (bool, error) {
	_, made, err := notify.EnsurePushKeys(home, "", false)
	return made, err
}

func PushKeysReady(home string) bool { return notify.PushKeysReady(home) }

// Commands 合併子套件的指令（只有 notify 有；套件層級，指令行程不建 Module）。
func Commands() (map[string]app.Command, error) {
	return cmdline.Merge(notify.Commands())
}

var _ app.Module = (*Module)(nil)
