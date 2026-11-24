package app

import (
	"context"
	"go/parser"
	"go/token"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// echoTasks 只有 tasks 服務的 get；其他方法呼叫會 panic（沒被掛上就不會被打到）。
type echoTasks struct{ sdk.TasksAPI }

func (echoTasks) Get(_ context.Context, req *sdk.TasksGetRequest) (*sdk.TasksGetResponse, error) {
	return &sdk.TasksGetResponse{Task: &sdk.Task{ID: req.ID}}, nil
}

func status(c *sdk.Client, path string) (int, error) {
	req, err := http.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader(`{"id":"SEC-1"}`))
	if err != nil {
		return 0, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0, err
	}
	return resp.StatusCode, resp.Body.Close()
}

// 只在家的 unix socket 上接請求：殘留的 socket 檔會被換掉、啟動第二次回 Conflict、停掉後 socket 檔不在、
// 不是 socket 的檔案絕不刪；掛上的服務有回應、沒掛的路徑是 404。
// Guards: REQ-104
func TestHTTPServerListensOnlyOnUnixSocket(t *testing.T) {
	h := homefixture.NewShort(t)
	path := SocketPath(h.Dir)
	// 上次當掉留下的 socket 檔會被換掉。
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}
	s := NewHTTPServer(HTTPDeps{})
	s.SetHome(h.Dir)
	s.SetMounts([]Mount{{Service: "tasks", Impl: echoTasks{}, Methods: []string{"get"}}})
	ctx := context.Background()
	if err := s.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := s.ln.Addr().Network(); got != "unix" || s.ln.Addr().String() != path {
		t.Fatalf("listens on %s %s", got, s.ln.Addr())
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket missing: %v", err)
	}
	if err := s.Start(ctx); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("second start: %v", err)
	}
	c := sdk.Dial(path)
	if code, err := status(c, "/api/tasks/get"); err != nil || code != http.StatusOK {
		t.Fatalf("掛上的服務要有回應：%d %v", code, err)
	}
	if code, err := status(c, "/api/tasks/list"); err != nil || code != http.StatusNotFound {
		t.Fatalf("沒登記的方法要 404：%d %v", code, err)
	}
	if code, err := status(c, "/api/nothing/here"); err != nil || code != http.StatusNotFound {
		t.Fatalf("沒有的路徑要 404：%d %v", code, err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("socket not removed: %v", err)
	}
	if err := s.Stop(ctx); err != nil {
		t.Fatalf("stop twice: %v", err)
	}
	// 不是 socket 的檔案絕不刪。
	if err := os.WriteFile(path, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(ctx); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("regular file: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("regular file was removed")
	}
}

// 沒設家就什麼都不做；掛載失敗（服務名重複的方法、不存在的方法）Start 說「無法掛載」，Handler 回 500。
func TestHTTPServerMountFailure(t *testing.T) {
	if err := NewHTTPServer(HTTPDeps{}).Start(context.Background()); err != nil {
		t.Fatalf("沒設家：%v", err)
	}
	h := homefixture.NewShort(t)
	s := NewHTTPServer(HTTPDeps{})
	s.SetHome(h.Dir)
	s.SetMounts([]Mount{{Service: "tasks", Impl: echoTasks{}, Methods: []string{"no_such"}}})
	if err := s.Start(context.Background()); platform.CodeOf(err) != sdk.ErrorCodeInternal {
		t.Fatalf("掛載失敗：%v", err)
	}
	rec := &recorder{}
	s.Handler().ServeHTTP(rec, mustRequest(t, "/api/tasks/get"))
	if rec.code != http.StatusInternalServerError {
		t.Fatalf("Handler 要 500：%d", rec.code)
	}
}

type recorder struct {
	code int
	hdr  http.Header
}

func (r *recorder) Header() http.Header {
	if r.hdr == nil {
		r.hdr = http.Header{}
	}
	return r.hdr
}
func (r *recorder) Write(b []byte) (int, error) { return len(b), nil }
func (r *recorder) WriteHeader(code int)        { r.code = code }

func mustRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://localhost"+path, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// 組裝框架自己不碰檔案：只有 httpserver.go 可以用 os（刪 socket）；沒有 TCP 監聽。
// Guards: REQ-2
func TestOnlyHTTPServerTouchesFiles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if (p == "os" && name != "httpserver.go") || p == "io/ioutil" || p == "os/exec" {
				t.Errorf("%s imports %s; only httpserver.go may use os (to remove the socket)", name, p)
			}
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), `"tcp`) || strings.Contains(string(data), "ListenAndServe") {
			t.Errorf("%s opens a TCP listener", name)
		}
		checked++
	}
	if checked < 4 {
		t.Fatalf("only checked %d files", checked)
	}
}

// DEV-145：第 2 版的家，API socket 在狀態資料夾的 runtime/sockets/。
func TestSocketPathLayoutV2(t *testing.T) {
	home, state := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("[layout]\nversion = 2\nstate = \""+state+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := SocketPath(home); got != filepath.Join(state, "runtime", "sockets", "api.sock") {
		t.Fatal(got)
	}
	if got := SocketPath(t.TempDir()); !strings.HasSuffix(got, filepath.Join("runtime", "sockets", "api.sock")) {
		t.Fatal("第 1 版照舊在家裡：", got)
	}
}
