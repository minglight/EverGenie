// internal/feature/agents/module.go — 說明卡 ai_docs/modules/agents.md。

// Package agents 是業務塊 agents 的組裝（S8-3）：名冊管理（roster，原 agents）、角色庫（persona，原 personas）
// 與主題筆記（notes，DEV-161）三個子模組。
// 秘書的紀錄本身在底層 agent；這個檔只組裝：規則都在子套件。
package agents

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/feature/agents/notes"
	"github.com/minglight/evergenie/internal/feature/agents/persona"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/internal/feature/agents/roster"
	"github.com/minglight/evergenie/sdk"
)

// Deps 只列塊外的東西（欄位照 roster 原本的；角色庫要的名冊與時鐘跟它共用 Roster、Clock）。
// 角色庫的測口吻（session）、角色包指標（packages）、套件角色（packages）建在塊之後或屬於別的塊，用 setter 接。
type Deps struct {
	Roster    roster.Roster       // 底層 agent：單筆紀錄操作、記憶、日誌（角色庫只用 List）
	Jobs      roster.AgentJobs    // 名下的排程（tasks 塊的 job）：可以是 nil
	Clock     roster.Clock        // 可以是 nil（系統時鐘）
	Vault     roster.RecordReader // 底層 vault
	Mail      roster.Mailer       // 底層 bus：新家第一位秘書的招呼提示（AGS-11）；可以是 nil（就是不送）
	Cards     roster.CardWriter   // 底層 engine
	Directory roster.Directory    // 看板的名冊（底層 agent）：可以是 nil
	Avatars   roster.Avatars      // 看板外殼的動態頭像（base/app/board 的 Assets）：可以是 nil
	Health    roster.Health       // 名冊的健檢（底層 agent）：可以是 nil
	Templates roster.Templates    // 套件帶來的職位樣板（packages 塊）：可以是 nil
	Notes     notes.Notes         // 全家共用的主題筆記（底層 agent，DEV-161）：可以是 nil
}

// Module 是 agents 塊：Start 依 roster → persona → notes（registry 原本的先後；兩個的 Start 都只開 log），Stop 反序；登記合併兩個子模組的。
type Module struct {
	*app.Group
	admin   *roster.Module
	library *persona.Module
}

// New 建兩個子模組；不做事（MOD-N4）。
func New(d Deps) *Module {
	a := roster.New(roster.Deps{Roster: d.Roster, Jobs: d.Jobs, Clock: d.Clock, Vault: d.Vault, Mail: d.Mail, Cards: d.Cards, Directory: d.Directory, Avatars: d.Avatars, Health: d.Health, Templates: d.Templates})
	var images persona.Images
	if d.Avatars != nil {
		images = d.Avatars
	}
	l := persona.New(persona.Deps{Roster: d.Roster, Clock: d.Clock, Images: images})
	return &Module{Group: app.NewGroup("agents", a, l, notes.New(d.Notes)), admin: a, library: l}
}

// SetTester 接上 session 的測口吻（session 建在塊之後；在 Start 之前呼叫）。
func (m *Module) SetTester(t persona.Tester) { m.library.SetTester(t) }

// SetPackPointer 接上角色包指標檔路徑的算法（packages 塊的 PersonasPackPointer）。
func (m *Module) SetPackPointer(f func(home string) string) { m.library.SetPackPointer(f) }

// SetBundledCards 接上跟著程式的公開卡資料夾的算法（packages 塊的 PersonasBundledCards，PRS-25）。
func (m *Module) SetBundledCards(f func(home string) string) { m.library.SetBundledCards(f) }

// SetRecordStore 接上角色包同步寫跟著角色走的紀錄要的套件資料（PKG-59）。
func (m *Module) SetRecordStore(s personapack.Store) { m.library.SetRecordStore(s) }

// SetPackagePersonas 接上套件角色的列出與寫入（packages 塊；角色庫不 import packages）。
func (m *Module) SetPackagePersonas(s persona.PackageSource) { m.library.SetPackagePersonas(s) }

// Admin 是名冊管理子模組（套件同步、認領職位要建秘書、換角色）。
func (m *Module) Admin() *roster.Module { return m.admin }

// Library 是角色庫子模組（底層 agent 的 PersonaSource、session 的角色卡）。
func (m *Module) Library() *persona.Module { return m.library }

// Commands 合併兩個子套件的指令（套件層級）：stateLabel 是 chat 塊的對話狀態白話，packPointer 是 packages 塊的角色包指標，
// bundledCards 是 packages 塊算的跟著程式的公開卡資料夾（PRS-25），fileKinds 是 packages 塊從套件設定讀的照紀錄帶圖的種類（DEV-160）。
func Commands(stateLabel func(sdk.Session) string, packPointer, bundledCards func(home string) string, fileKinds func(home string) ([]string, error)) (map[string]app.Command, error) {
	return cmdline.Merge(roster.Commands(stateLabel), persona.Commands(packPointer, bundledCards, fileKinds), notes.Commands())
}

var _ app.Module = (*Module)(nil)
