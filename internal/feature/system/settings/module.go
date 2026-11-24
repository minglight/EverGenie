// internal/feature/system/settings/module.go — ai_docs/modules/system.md（業務模組；家的設定與模式、備份、安裝前檢查）。
package settings

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// PreflightSource 是引擎那邊的「跑起來需要的東西齊不齊」（registry 傳 engine 模組本身）。
type PreflightSource interface {
	Preflight(ctx context.Context) ([]sdk.PreflightCheck, error)
}

// Deps 都可以是 nil：沒有 Engine 時 system.preflight 回 unavailable，沒有 Clock 用系統時鐘。
// Runner 是跑 tailscale 的（手機連線，phone.go）：nil＝真的執行，測試換成假的；Push 是 inbox 塊的推播金鑰。
type Deps struct {
	Engine PreflightSource
	Clock  platform.Clock
	Runner host.Runner
	Push   PushKeys
}

// Module 是家的設定業務模組：沒有背景工作，Start／Stop 只管 log。
type Module struct {
	d    Deps
	mu   sync.Mutex
	home string
	log  *slog.Logger
}

func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string { return "settings" }

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
	m.log = platform.NewLogger(h, "settings")
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

// 登記方法（app 的登記介面）：Routes 在 http.go，Commands 在 cli.go。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
