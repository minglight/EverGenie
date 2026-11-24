// internal/feature/system/accounts/module.go — ai_docs/modules/system.md（業務模組；AI 帳號設定與用量）。
package accounts

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Usage 是「問各帳號用量」的能力（engine 實作，registry 接上）。account 空字串＝全部。
type Usage interface {
	Usages(ctx context.Context, account string) ([]sdk.AccountUsage, error)
}

// Deps 都可以是 nil：沒有 Usage 時查用量回「引擎服務還沒接上」，沒有 Clock 用系統時鐘。
type Deps struct {
	Usage Usage
	Clock platform.Clock
}

// Module 是 AI 帳號業務模組：沒有背景工作，Start／Stop 只管 log。
type Module struct {
	d    Deps
	mu   sync.Mutex
	home string
	log  *slog.Logger
}

func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string { return "accounts" }

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
	m.log = platform.NewLogger(h, "accounts")
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
