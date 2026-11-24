// internal/feature/agents/persona/module.go — ai_docs/modules/agents.md（REQ-130, REQ-283）。
package persona

import (
	"context"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// Roster 是角色庫向名冊要的：現在有哪些職位、各坐哪個角色（agent 模組實作 List）。
type Roster interface {
	List(ctx context.Context) ([]*sdk.Record, error)
}
type Clock interface{ Now() time.Time }

// Deps 是 registry 注入的依賴（小 interface，定義在使用方）。
type Deps struct {
	Roster Roster
	Clock  Clock
	// Images：這個人設有沒有頭像圖（base/app/board 的 Assets，經 agents 塊的 Avatars 注入）；可以是 nil（照舊一律給網址）。
	Images Images
}

// Images 回某個人設現在有沒有頭像圖：沒有時卡片不給 avatar_url，看板直接畫名字、不去抓 404（DEV-157）。
type Images interface {
	HasImage(slug string) bool
}

// Tester 是 session 的測口吻（tone.go 留在 session；P6-5b）：`personas.test`、`personas.test_results` 轉給它。
type Tester interface {
	TestPersona(ctx context.Context, req *sdk.PersonasTestRequest) (*sdk.PersonasTestResponse, error)
	PersonaTestResults(ctx context.Context, req *sdk.PersonasTestResultsRequest) (*sdk.PersonasTestResultsResponse, error)
}

type packageSourceHolder struct {
	mu  sync.RWMutex
	src PackageSource
}

// Module 是角色庫：人設卡的列出、新增、複製、編輯、檢查、上線／設計中，說話層強度，套件角色，角色包（personapack）。
type Module struct {
	d      Deps
	home   string
	mu     sync.Mutex // 寫卡、改狀態一次一個
	pkgSrc packageSourceHolder

	setMu    sync.RWMutex
	tester   Tester
	pointer  func(home string) string // 角色包指標檔路徑（packages 算，registry 注入）
	bundled  func(home string) string // 跟著程式的公開卡資料夾（PRS-25；packages 算，registry 注入）
	notifier Notifier                 // 新角色打招呼（DEV-115，intro.go）
	records  personapack.Store        // 角色包同步寫跟著角色走的紀錄（PKG-59；組裝接 packages 與 vault）

	pack packGate // 角色包同步一次一個、下一次定期再試的時間（pack_retry.go）
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d}
}

func (m *Module) Name() string { return "persona" }

// SetHome 設定家的位置。
func (m *Module) SetHome(dir string) { m.home = dir }

// Start、Stop 沒有背景工作（角色包的定期再試等是 Jobs() 登記的定時工作）。
func (m *Module) Start(ctx context.Context) error { return ctx.Err() }
func (m *Module) Stop(context.Context) error      { return nil }

// SetTester 接上 session 的測口吻；沒接＝`personas.test` 回 unavailable。
func (m *Module) SetTester(t Tester) { m.setMu.Lock(); m.tester = t; m.setMu.Unlock() }

// SetPackPointer 接上角色包指標檔路徑的算法（packages.PersonasPackPointer）。
func (m *Module) SetPackPointer(f func(home string) string) {
	m.setMu.Lock()
	m.pointer = f
	m.setMu.Unlock()
}

// SetBundledCards 接上「跟著程式的公開卡」資料夾的算法（packages.PersonasBundledCards，PRS-25）；沒接＝只看官方資料夾。
func (m *Module) SetBundledCards(f func(home string) string) {
	m.setMu.Lock()
	m.bundled = f
	m.setMu.Unlock()
}

// bundledDir 是跟著程式的公開卡資料夾；沒接回空。
func (m *Module) bundledDir() string {
	m.setMu.RLock()
	defer m.setMu.RUnlock()
	if m.bundled == nil {
		return ""
	}
	return m.bundled(m.home)
}

// SetRecordStore 接上角色包同步用的套件資料（packages 的種類宣告＋vault 的寫入，組裝接）；沒接＝包裡有紀錄時定期再試會失敗、記下待補。
func (m *Module) SetRecordStore(s personapack.Store) {
	m.setMu.Lock()
	m.records = s
	m.setMu.Unlock()
}

func (m *Module) recordStore() personapack.Store {
	m.setMu.RLock()
	defer m.setMu.RUnlock()
	return m.records
}

func (m *Module) packPointer() string {
	m.setMu.RLock()
	defer m.setMu.RUnlock()
	if m.pointer == nil {
		return ""
	}
	return m.pointer(m.home)
}

// 登記方法：Routes 在 http.go，Commands 在 cli.go。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)

// requireOwner：寫入類方法只有主人（跟名冊的規則一樣）。
func requireOwner(by string) error {
	if platform.IsOwnerOrSystem(by) {
		return nil
	}
	return platform.Forbidden("只有主人能管理秘書名冊")
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func stringList(v any) []string {
	out := []string{}
	switch xs := v.(type) {
	case []string:
		return xs
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func parseCard(data []byte) (vault.Record, error) { return personaref.ParseCard(data) }
