// internal/feature/packages/module.go — 說明卡 ai_docs/modules/packages.md。

// Package packages 是業務塊 packages 的組裝（S8-3、S8-5）：套件（子套件 pkg：安裝、重編、擴充點、監督、同步、認領）與
// 通用資料讀寫（子套件 data，sdk 的 vault 服務）。這個檔只組裝：規則都在子套件。
package packages

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/packages/data"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
)

// Vault 是兩個子模組要的資料庫操作（底層 vault）。
type Vault interface {
	pkg.VaultStore
	data.VaultStore
}

// Deps 只列塊外的東西（data 只要 Vault）。引擎監督、同步與認領的宿主由別的塊或 cmd/evergenie 提供、
// 有的建在塊之後，用 setter 接。
type Deps struct {
	Agents pkg.AgentLookup // 底層 agent
	Bus    pkg.BusSender   // 底層 bus
	Vault  Vault
	Clock  platform.Clock
}

// Module 是 packages 塊：Start 依 data → pkg（data 的 Start 只開 log），Stop 反序；登記合併兩個子模組的。
type Module struct {
	*app.Group
	store *pkg.Module
	data  *data.Module
}

// New 建兩個子模組；不做事（MOD-N4）。
func New(d Deps) *Module {
	dt := data.New(data.Deps{Vault: d.Vault})
	s := pkg.New(pkg.Deps{Agents: d.Agents, Bus: d.Bus, Vault: d.Vault, Clock: d.Clock})
	return &Module{Group: app.NewGroup("packages", dt, s), store: s, data: dt}
}

// Store 是套件子模組（看板外殼的前端資源、名冊管理的樣板、角色庫的套件角色都讀它）。
func (m *Module) Store() *pkg.Module { return m.store }

// SetExternal 接上引擎套件的監督（底層 engine；在 Start 之前呼叫，下同）。
func (m *Module) SetExternal(e pkg.ExternalSupervisor) { m.store.SetExternal(e) }

// SetMigrationLog 接上資料轉換的紀錄（主人自己的、分享來的套件載入時做轉換，DEV-154）。
func (m *Module) SetMigrationLog(l pkg.MigrationLog) { m.store.SetMigrationLog(l) }

// SetEngines 接上引擎套件的就緒狀態（packages.engine 健檢）。
func (m *Module) SetEngines(e pkg.EngineStatuses) { m.store.SetEngines(e) }

// SetSyncHost 接上套件同步要的名冊、秘書管理、排程（cmd/evergenie 把別的塊包起來）。
func (m *Module) SetSyncHost(h pkg.SyncHost) { m.store.SetSyncHost(h) }

// SetClaimHost 接上認領職位要的名冊管理、角色庫、通知。
func (m *Module) SetClaimHost(h pkg.ClaimHost) { m.store.SetClaimHost(h) }

// SetOfficialDir 指定官方套件資料夾（測試與開發家）。
func (m *Module) SetOfficialDir(dir string) { m.store.SetOfficialDir(dir) }

// Commands 是塊的指令（只有套件子模組有；套件層級，指令行程不建 Module）。
func Commands() (map[string]app.Command, error) { return cmdline.Merge(pkg.Commands()) }

var _ app.Module = (*Module)(nil)
