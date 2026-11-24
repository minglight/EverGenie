// internal/feature/inbox/menu/module.go — 說明卡 ai_docs/modules/inbox.md。
package menu

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/inbox/overview"
	"github.com/minglight/evergenie/sdk"
)

// Frontend 是選單算套件項目與頁面要的套件前端資源（由 base/app/board 的 Assets 提供，registry 注入）。
type Frontend interface {
	VisiblePackages() []sdk.Package                                  // 啟用而且沒被拒的套件
	FindPage(pkgID, pageID string) (sdk.Package, sdk.PageDecl, bool) // 在看得到的套件裡找頁面
	Bundle(pkgID, pageID string) (hash string, err error)            // 編譯頁面（指紋沒變用快取），回產物 hash；錯誤是白話
}

// Roster 是 show_when（`agent:<職位 id>:enabled`）要的名冊讀取。
type Roster interface {
	List(ctx context.Context) ([]*sdk.Record, error)
}

// Records 是 show_when（`cap:<套件>.<能力>`）要的紀錄讀取：讀 `<套件>.capabilities/main`（含封存的；找不到回 NOT_FOUND，MNU-11）。
type Records interface {
	Get(ctx context.Context, kind, id string) (sdk.Record, error)
}

// Deps 的欄位都可以是 nil：沒接 Overview 說「核心服務尚未啟動」；沒接 Frontend 就沒有套件項目與頁面；沒接 Roster、Records 時 show_when 要看它們的項目不顯示。
// Overview 是同塊的首頁彙整（徽章的數字都從它來），塊的 New 直接接，不另定介面（S8-5）。
type Deps struct {
	Frontend Frontend
	Roster   Roster
	Records  Records
	Overview *overview.Module
}

// Module 是看板的選單：核心項目、套件項目與頁面、使用者自己排的樹（settings/menu.toml）。
// HTTP 在 http.go（sdk 的 web 服務裡 get_menu、get_pages、get_menu_prefs、set_menu_prefs）。
type Module struct {
	d Deps

	mu   sync.Mutex
	home string
	log  *slog.Logger

	prefsMu sync.Mutex
	warned  sync.Map // 只警告一次的訊息 key
}

// New 不做任何 I/O（MOD-N4）。
func New(d Deps) *Module {
	if d.Frontend == nil {
		d.Frontend = noFrontend{}
	}
	return &Module{d: d, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func (m *Module) Name() string { return "menu" }

func (m *Module) SetHome(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.home = dir
}

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
	l := platform.NewLogger(h, "menu")
	m.mu.Lock()
	m.log = l
	m.mu.Unlock()
	return nil
}

func (m *Module) Stop(context.Context) error { return nil }

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

func (m *Module) settingsPath(name string) string {
	return filepath.Join(m.homeDir(), "settings", name)
}

// warnOnce 同一個 key 只寫一次 WARN，免得每次重算選單都洗版。
func (m *Module) warnOnce(key, msg string, args ...any) {
	if _, loaded := m.warned.LoadOrStore(key, true); !loaded {
		m.logger().Warn(msg, args...)
	}
}

// badges 向首頁彙整拿徽章的數字；失敗時 sdk 錯誤原樣轉出、連不上一律 unavailable。
func (m *Module) badges(ctx context.Context) (*sdk.WebGetOverviewResponse, error) {
	if m.d.Overview == nil {
		return nil, sdkErr(sdk.ErrorCodeUnavailable, "", "核心服務尚未啟動")
	}
	out, err := m.d.Overview.Overview(ctx)
	if err != nil {
		return nil, platform.UpstreamError(err)
	}
	return out, nil
}

// agents 是名冊上全部的秘書；讀不到回 unavailable（呼叫的人決定要不要當作沒有）。
func (m *Module) agents(ctx context.Context) ([]*sdk.Record, error) {
	if m.d.Roster == nil {
		return nil, nil
	}
	list, err := m.d.Roster.List(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			m.logger().Error("讀名冊失敗", "error", err)
		}
		return nil, sdkErr(sdk.ErrorCodeUnavailable, "", "讀不到秘書名冊，稍後再試")
	}
	out := make([]*sdk.Record, 0, len(list))
	for _, a := range list {
		if a != nil {
			out = append(out, a)
		}
	}
	return out, nil
}

func sdkErr(code sdk.ErrorCode, field, msg string) error {
	return &sdk.Error{Code: code, Field: field, Message: msg}
}

// noFrontend 是沒有接套件前端時的樣子：沒有套件。
type noFrontend struct{}

func (noFrontend) VisiblePackages() []sdk.Package { return nil }
func (noFrontend) FindPage(string, string) (sdk.Package, sdk.PageDecl, bool) {
	return sdk.Package{}, sdk.PageDecl{}, false
}
func (noFrontend) Bundle(string, string) (string, error) {
	return "", errors.New("沒有接上套件前端")
}

var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
