package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// HTTPServer 是核心唯一的 HTTP 入口（API-6）：只在家的暫存資料夾 `runtime/sockets/api.sock` 這個 unix socket 上接請求，
// 把各業務模組登記的路由（Mount）掛在同一個 mux，找不到的路徑回 sdk.NotFoundHandler。
// 它本身沒有任何服務；看板（web）經 Handler() 在行程內直接用同一組路由。
type HTTPServer struct {
	d      HTTPDeps
	mu     sync.Mutex
	home   string
	mounts []Mount
	log    *slog.Logger
	server *http.Server
	ln     net.Listener
	done   chan struct{}
}

// HTTPDeps：Clock 可以是 nil（用系統時鐘）；測試用假時鐘。
type HTTPDeps struct {
	Clock interface{ Now() time.Time }
}

// NewHTTPServer 不做任何 I/O（MOD-N4）。
func NewHTTPServer(d HTTPDeps) *HTTPServer { return &HTTPServer{d: d} }

// Name 是模組名稱，也是 log 的模組名（runtime/logs 裡的 "api"）。
func (s *HTTPServer) Name() string { return "api" }

// SetHome 設定家的資料夾；啟動前呼叫。沒設定＝Start 什麼都不做。
func (s *HTTPServer) SetHome(dir string) { s.mu.Lock(); defer s.mu.Unlock(); s.home = dir }

// SetMounts 接上要掛的路由：registry 用 (*App).Mounts() 收集後交進來（服務名重複在那裡就擋下）；啟動前呼叫。
func (s *HTTPServer) SetMounts(mounts []Mount) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mounts = append([]Mount(nil), mounts...)
}

// maxSocketPath：macOS 的 sockaddr_un 路徑上限是 104 位元組（Linux 108），含結尾 NUL 後剛好放得下。
const maxSocketPath = 103

// SocketPath 是家的 API socket 路徑。
func SocketPath(home string) string { return platform.RuntimeDir(home, "sockets", "api.sock") }

// Start 開 socket 並開始服務；已經啟動回 Conflict，家的路徑太長回 Unavailable（白話）。
func (s *HTTPServer) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.home == "" {
		return nil
	}
	if s.server != nil {
		return platform.Conflict("API 服務已啟動")
	}
	h, err := platform.LoadHome(s.home)
	if err != nil {
		return err
	}
	if s.d.Clock != nil {
		h.Clock = s.d.Clock
	}
	s.log = platform.NewLogger(h, "api")
	mux, err := s.mux()
	if err != nil {
		s.log.Error("mount services failed", "error", err)
		return platform.Internal("API 服務無法掛載")
	}
	sock := SocketPath(s.home)
	if len(sock) > maxSocketPath {
		s.log.Error("socket path too long", "path", sock, "length", len(sock))
		return platform.Unavailable(fmt.Sprintf("家的路徑太長（unix socket 路徑上限約 %d 字元，現在 %d）；請把家放到比較短的資料夾", maxSocketPath, len(sock)))
	}
	ln, err := listenUnix(sock)
	if err != nil {
		s.log.Error("listen failed", "error", err)
		return platform.Unavailable("API 服務無法開始接收請求，請檢查家目錄")
	}
	// 不設 WriteTimeout：核准請求可以合理地等 600 秒。
	s.server = &http.Server{Handler: mux, BaseContext: func(net.Listener) context.Context { return ctx }}
	s.ln = ln
	s.done = make(chan struct{})
	server, done, log := s.server, s.done, s.log
	go func() {
		defer close(done)
		if err := server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Error("serve failed", "error", err)
		}
	}()
	return nil
}

// Stop 關掉伺服器並刪掉 socket 檔；沒啟動過或已經停掉就什麼都不做。
func (s *HTTPServer) Stop(ctx context.Context) error {
	s.mu.Lock()
	server, done, ln, home := s.server, s.done, s.ln, s.home
	s.server, s.done, s.ln = nil, nil, nil
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	// Close（不是 Shutdown）：長連線的串流要在核心停止時結束。
	err := server.Close()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}
	if cerr := removeSocket(SocketPath(home), ln); err == nil {
		err = cerr
	}
	return err
}

// Handler 回掛好的路由，給自己服務的人用（看板在行程內直接用，不經 socket）；掛載失敗回一律 500 的 handler。
func (s *HTTPServer) Handler() http.Handler {
	s.mu.Lock()
	defer s.mu.Unlock()
	mux, err := s.mux()
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "內部錯誤", http.StatusInternalServerError)
		})
	}
	return mux
}

// mux 把 mounts 掛到同一個 http.ServeMux，加上 "/" 的找不到處理。呼叫時要持有 s.mu。
func (s *HTTPServer) mux() (*http.ServeMux, error) {
	mux := http.NewServeMux()
	for _, mt := range s.mounts {
		if err := sdk.MountMethods(mux, mt.Service, mt.Impl, mt.Methods); err != nil {
			return nil, fmt.Errorf("掛載服務 %s：%w", mt.Service, err)
		}
	}
	mux.Handle("/", sdk.NotFoundHandler())
	return mux, nil
}

// listenUnix 換掉殘留的 socket 檔，只在本機 socket 上監聽（不是 socket 的檔案不動）。
func listenUnix(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("socket path is not a socket")
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return net.Listen("unix", path)
}

// removeSocket 關掉 listener 並確認 socket 檔不在了。
func removeSocket(path string, ln net.Listener) error {
	var err error
	if ln != nil {
		// 伺服器已經關過了；第二次 Close 只會回報這件事。
		_ = ln.Close()
	}
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		err = rmErr
	}
	return err
}

var _ Module = (*HTTPServer)(nil)
