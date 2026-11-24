// internal/base/app/board/module.go — ai_docs/modules/board.md
package board

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// AgentRoster 是看板送頭像要的名冊讀取（由 agent 提供）。
type AgentRoster interface {
	List(ctx context.Context) ([]*sdk.Record, error)
}

// APIServer 是核心 HTTP 入口的 Handler()（app.HTTPServer）：看板把允許的服務轉給它。
type APIServer interface{ Handler() http.Handler }
type Clock interface{ Now() time.Time }

// Deps：API 是核心的路由（行程內直接呼叫，不經 socket）；Assets 是套件前端資源（選單、名冊也用同一份）。
type Deps struct {
	Assets *Assets
	Agents AgentRoster
	API    APIServer
	Clock  Clock
}

// Module 是看板外殼：一個 HTTP 伺服器，沒有自己的 sdk 服務。
type Module struct {
	d      Deps
	static fs.FS // 預設是內嵌的 dist；測試可換

	mu     sync.Mutex
	home   string
	log    *slog.Logger
	server *http.Server
	ln     net.Listener
	done   chan struct{}

	apiReadLimit time.Duration // 讀取的時限；0＝apiReadTimeout（測試才改）

	apiOnce    sync.Once
	voiceMu    sync.Mutex // 簽語音捷徑一次一個（voiceshortcut.go）
	apiHandler http.Handler

	icons appIconCache // 主畫面圖示（appicon.go），版本變了就重產

	webCache accessCache // 跨站檢查要的 [web] 設定（cors.go），每個請求看 home.toml 有沒有改
}

func New(d Deps) *Module {
	if d.Assets == nil {
		d.Assets = NewAssets(nil)
	}
	m := &Module{d: d, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if sub, err := fs.Sub(Dist, "dist"); err == nil {
		m.static = sub
	}
	return m
}
func (m *Module) Name() string { return "board" }
func (m *Module) SetHome(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.home = dir
	m.d.Assets.SetHome(dir)
}
func (m *Module) homeDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.home
}
func (m *Module) logger() *slog.Logger {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.log
}

// Addr 是看板實際在聽的位址；沒有在聽回空字串。
func (m *Module) Addr() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ln == nil {
		return ""
	}
	return m.ln.Addr().String()
}

func (m *Module) now() time.Time {
	if m.d.Clock != nil {
		return m.d.Clock.Now()
	}
	return time.Now()
}

// Start 只聽 127.0.0.1:<web.port>（HTTPS 由 evergenie tailscale setup 接上：Tailscale 服務 svc:<web.service>，沒寫就是機器 serve；D-43）。沒有家就不聽。
func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.home == "" {
		return nil
	}
	if m.server != nil {
		return platform.Conflict("看板已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	if m.d.Clock != nil {
		h.Clock = m.d.Clock
	}
	m.log = platform.NewLogger(h, "board")
	m.d.Assets.SetLogger(m.log)
	if len(h.Web.AllowBoards) > 0 && h.Web.OwnerLogin == "" {
		m.log.Warn("web.allow_boards 有值但沒設 web.owner_login：別的看板連進來一律被擋")
	}
	handler := m.buildHandler()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", h.Web.Port))
	if err != nil {
		m.log.Error("listen failed", "port", h.Web.Port, "error", err)
		return platform.Unavailable(fmt.Sprintf("看板的連接埠 %d 已被占用，請改 home.toml 的 web.port", h.Web.Port))
	}
	// 沒有 WriteTimeout：串流（聊天、通知）要一直開著。
	m.server = &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	m.ln = ln
	m.done = make(chan struct{})
	server, done, log := m.server, m.done, m.log
	go func() {
		defer close(done)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error("serve failed", "error", err)
		}
	}()
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	server, done := m.server, m.done
	m.server, m.done, m.ln = nil, nil, nil
	m.mu.Unlock()
	if server == nil {
		return nil
	}
	// Close（不是 Shutdown）：長連線的串流要跟著核心一起結束。
	err := server.Close()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	return err
}

// Handler 回整個看板的 mux（/api/*＋白名單＋靜態檔），測試可以直接用 httptest。
// 跨站要看的設定（public_url、allow_boards、owner_login）每個請求讀（cors.go），不在這裡算。
func (m *Module) Handler() http.Handler { return m.buildHandler() }
