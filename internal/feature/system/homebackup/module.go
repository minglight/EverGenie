// internal/feature/system/homebackup/module.go — ai_docs/modules/system.md（system/homebackup：整個家的備份、還原、從備份建家）。
package homebackup

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Notifier 是 inbox 塊的 notify：還原的結果要讓主人知道。
type Notifier interface {
	Send(context.Context, *sdk.Notification) (*sdk.Notification, error)
}

// Sessions 是底層 session 的「把這個家開著的秘書對話全部收掉」（SES-75）：還原要換上去之前叫，
// 不然重開之後她們還帶著還原前的內容繼續做事。
type Sessions interface {
	CloseAll(ctx context.Context) (closed int, err error)
}

// Deps 都可以是 nil。
type Deps struct {
	Notify   Notifier
	Sessions Sessions
	Clock    platform.Clock
	// KnownMigrations 回現在的程式認得的家的資料整理（核心的）；拿來擋「備份比程式新」。nil＝不檢查。
	KnownMigrations func(home string) []string
	// Restart 讓服務重開（還原準備好之後叫）；nil＝RequestRestart（請 serve 收工後原地重開）。測試換掉它。
	Restart func()
}

// Module 是整個家的備份與還原。
type Module struct {
	d    Deps
	mu   sync.Mutex
	home string
	log  *slog.Logger
	// work 一次只讓一件大事做：做備份、準備還原。
	work sync.Mutex
}

func New(d Deps) *Module { return &Module{d: d} }

func (m *Module) Name() string { return "homebackup" }

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
	m.log = platform.NewLogger(h, "homebackup")
	m.mu.Unlock()
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

func (m *Module) homeDir() string { m.mu.Lock(); defer m.mu.Unlock(); return m.home }

func (m *Module) logger() *slog.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return m.log
}

func (m *Module) now() time.Time {
	if m.d.Clock != nil {
		return m.d.Clock.Now()
	}
	return platform.SystemClock{}.Now()
}

func (m *Module) known(home string) []string {
	if m.d.KnownMigrations == nil {
		return nil
	}
	return m.d.KnownMigrations(home)
}

// restartRequested：還原準備好了，serve 收工後要原地重開（重開時換上去）。
var restartRequested atomic.Bool

// RestartRequested 給 cmd/evergenie 的 serve 看：這次收工是不是為了還原要重開。
func RestartRequested() bool { return restartRequested.Load() }

// RequestRestart 記下「要重開」，過一下（讓還原的回應先送出去）請自己這個行程收工；serve 收完工看到記號就原地重開。
func RequestRestart() {
	restartRequested.Store(true)
	go func() {
		time.Sleep(700 * time.Millisecond)
		if p, err := os.FindProcess(os.Getpid()); err == nil {
			_ = p.Signal(syscall.SIGTERM) // 送不出去：還原會等到下次服務重開才換上去，看板顯示「等重開」
		}
	}()
}

// closeSessionsTimeout：還原前收對話最多等這麼久（引擎不回也要繼續重開，不能卡住還原）。
const closeSessionsTimeout = 30 * time.Second

// closeSessions 在服務為了還原重開之前，把這個家開著的秘書對話全部收掉（HBK-12）。收不掉的只記日誌、照樣重開：
// 換上去之後的家是好的，只是那幾段對話的內容還是還原前的（已知限制）。
func (m *Module) closeSessions(ctx context.Context) {
	if m.d.Sessions == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), closeSessionsTimeout)
	defer cancel()
	n, err := m.d.Sessions.CloseAll(ctx)
	if err != nil {
		m.logger().Warn("sessions not all closed before restore", "closed", n, "error", err)
		return
	}
	m.logger().Info("sessions closed before restore", "closed", n)
}

func (m *Module) restart() {
	if m.d.Restart != nil {
		m.d.Restart()
		return
	}
	RequestRestart()
}

// Jobs：每半小時通知還原結果、收上傳暫存、清舊備份（daily.go 的 tick）。
func (m *Module) Jobs() []app.Job {
	return []app.Job{{Name: "homebackup.tick", Every: 30 * time.Minute, First: 2 * time.Minute, Run: func(ctx context.Context) error {
		m.tick(ctx, m.now())
		return nil
	}}}
}

var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.JobProvider   = (*Module)(nil)
)
