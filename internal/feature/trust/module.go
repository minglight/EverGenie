// internal/feature/trust/module.go — 說明卡 ai_docs/modules/trust.md。

// Package trust 是業務塊 trust 的組裝（S8-3）：信任等級與權限設定（rules，原 trust）、權限變更申請（requests，原 policy）
// 兩個子模組；資料 policy_request 屬於這一塊。這個檔只組裝：規則都在子套件。
package trust

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/trust/requests"
	"github.com/minglight/evergenie/internal/feature/trust/rules"
)

// Vault 是兩個子模組要的資料庫操作（底層 vault）。
type Vault interface {
	rules.VaultStore
	requests.VaultStore
}

// Engine 是底層 engine 給這一塊的：權限健檢比對產生的設定（rules）、把核准的 guidance 套進設定（requests）。可以是 nil。
type Engine interface {
	rules.Permissions
	requests.Applier
}

// Deps 只列塊外的東西。
type Deps struct {
	Vault  Vault
	Notify requests.Notifier // inbox 塊的 notify：申請送主人審核
	Bus    requests.Bus      // 底層 bus：結果訊息回給申請的秘書
	Engine Engine
	Clock  platform.Clock
	Names  requests.Names // 底層 agent：秘書 id → 名字（通知與影響說明寫名字）；可以是 nil
}

// Module 是 trust 塊：Start 依 rules → requests（registry 原本的先後），Stop 反序；登記合併兩個子模組的。
// 整塊要在底層 engine 之前啟動：引擎握手時要讀到已核准的 guidance（requests 的 Start 讀進來）。
type Module struct {
	*app.Group
	levels   *rules.Module
	requests *requests.Module
}

// New 建兩個子模組；不做事（MOD-N4）。
func New(d Deps) *Module {
	l := rules.New(rules.Deps{Vault: d.Vault, Engine: d.Engine, Clock: d.Clock})
	r := requests.New(requests.Deps{Vault: d.Vault, Notify: d.Notify, Bus: d.Bus, Engine: d.Engine, Clock: d.Clock, Names: d.Names})
	return &Module{Group: app.NewGroup("trust", l, r), levels: l, requests: r}
}

// Levels 是信任等級子模組（session 的 TrustLevels 與拒絕紀錄）；引擎讀政策用套件層級的 rules.Policy。
func (m *Module) Levels() *rules.Module { return m.levels }

// Requests 是權限申請子模組（引擎的 guidance 與規則來源：ActiveGuidance、ActiveRules）。
func (m *Module) Requests() *requests.Module { return m.requests }

// Commands 合併兩個子套件的指令（套件層級）：printDoctor 是 system 塊 doctor 的印法（trust 指令印權限健檢）。
func Commands(printDoctor rules.DoctorPrinter) (map[string]app.Command, error) {
	return cmdline.Merge(rules.Commands(printDoctor), requests.Commands())
}

var _ app.Module = (*Module)(nil)
