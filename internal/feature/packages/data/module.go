// internal/feature/packages/data/module.go — ai_docs/modules/packages.md（業務模組；套件與看板的通用資料讀寫 API）。
package data

import (
	"context"
	"log/slog"
	"sync"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// VaultStore 是 data 用到的 vault 能力（registry 傳 vault 模組本身）。
type VaultStore interface {
	Get(ctx context.Context, kind, id string) (vault.Record, error)
	List(ctx context.Context, kind string, q vault.Query) ([]vault.Record, error)
	Watch(ctx context.Context, kinds ...string) (<-chan vault.Change, error)
	// 套件的資料種類（種類名稱含 `.`，D-28）可以經 API 寫；核心的種類一律走各自的模組（API-1）。
	Put(ctx context.Context, r vault.Record, by, expectedRev string) (vault.Record, error)
	Patch(ctx context.Context, kind, id string, set map[string]any, unset []string, by string) (vault.Record, error)
	ArchiveNow(ctx context.Context, kind, id, by string) error
	NextID(ctx context.Context, kind string) (string, error)
	// Changes 讀變更紀錄（VLT-8 寫的那一份）：新到舊、可篩選、分頁。
	Changes(ctx context.Context, q vault.ChangesQuery) ([]sdk.ChangeEntry, string, error)
}

// Deps：沒有 Vault 時每個請求回 unavailable（測試與單獨起來的情況）。
type Deps struct {
	Vault VaultStore
}

// Module 是資料讀寫業務模組：沒有背景工作，Start／Stop 只管 log。
type Module struct {
	d    Deps
	mu   sync.Mutex
	home string
	log  *slog.Logger
}

func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string { return "data" }

func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	home := m.home
	m.mu.Unlock()
	if home == "" {
		return nil
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.log = platform.NewLogger(h, "data")
	m.mu.Unlock()
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) logger() *slog.Logger { m.mu.Lock(); defer m.mu.Unlock(); return m.log }

// 登記方法（app 的登記介面）：Routes 在 http.go，Checks 在 checks.go。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.CheckProvider = (*Module)(nil)
)
