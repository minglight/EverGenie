// internal/feature/trust/rules/module.go — ai_docs/modules/trust.md（業務模組；信任等級，Q5／Q7）。
package rules

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// VaultStore 只用來列名冊上的秘書（每位秘書都要有一個等級）。
type VaultStore interface {
	List(ctx context.Context, kind string, q vault.Query) ([]vault.Record, error)
}

// Deps 都可以是 nil：沒有 Vault 就只列 trust.toml 寫到的職位，沒有 Engine 就沒有權限的健檢，沒有 Clock 用系統時鐘。
type Deps struct {
	Vault  VaultStore
	Engine Permissions
	Clock  platform.Clock
}

// Module 是信任等級業務模組：沒有背景工作，Start／Stop 只管 log。
type Module struct {
	d    Deps
	mu   sync.Mutex
	home string
	log  *slog.Logger
}

func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string { return "rules" }

func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	home := m.homeDir()
	if home == "" {
		return nil
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.log = platform.NewLogger(h, "trust")
	m.mu.Unlock()
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) homeDir() string { m.mu.Lock(); defer m.mu.Unlock(); return m.home }

func (m *Module) logger() *slog.Logger { m.mu.Lock(); defer m.mu.Unlock(); return m.log }

func (m *Module) now() time.Time {
	if m.d.Clock != nil {
		return m.d.Clock.Now()
	}
	return platform.SystemClock{}.Now()
}

// 登記方法（app 的登記介面）：Routes 在 http.go，Checks 在 checks.go，Commands 在 cli.go。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.CheckProvider = (*Module)(nil)
)

func (*Module) LevelFor(t platform.Trust, agent string) sdk.PermissionLevel {
	return LevelFor(t, agent)
}
func (*Module) AppendDenied(home string, d sdk.DeniedAction) error { return AppendDenied(home, d) }
