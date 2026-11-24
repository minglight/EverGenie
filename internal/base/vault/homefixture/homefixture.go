// Package homefixture builds isolated, synthetic homes for tests.
package homefixture

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const config = `[user]
name="測試使用者"
timezone="Asia/Taipei"
[slots]
default="life"
[slots.life]
label="日常"
remind="08:30"
check="18:00"
[accounts]
default="main"
[accounts.main]
engine="test-engine"
[web]
port=18850
`

type Home struct {
	Dir   string
	Clock *platform.FakeClock
	Vault *vault.Module
	t     testing.TB
}

func safePath(path, userHome string) bool {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	forbidden := filepath.Join(userHome, "."+platform.ProductName) // 這台真的程式與狀態資料夾（~/.evergenie）
	return absolute != forbidden && !strings.HasPrefix(absolute, forbidden+string(filepath.Separator))
}
func New(t testing.TB) *Home {
	t.Helper()
	return newHome(t, t.TempDir(), "")
}

// NewWith 跟 New 一樣，但在 home.toml 後面多加一段設定（例 "[ids]\ntasks=\"DEV-\"\n"）再開資料庫。
func NewWith(t testing.TB, extra string) *Home {
	t.Helper()
	return newHome(t, t.TempDir(), extra)
}

// NewShort builds a home with a short path for local sockets.
func NewShort(t testing.TB) *Home {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	return newHome(t, dir, "")
}

func newHome(t testing.TB, dir, extra string) *Home {
	t.Helper()
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !safePath(dir, userHome) || !safePath(resolved, userHome) {
		t.Fatal("fixture cannot use the real home")
	}
	for _, sub := range []string{"vault/agents/code", "runtime"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	head := ""
	if strings.Contains(extra, "[layout]") && strings.Contains(extra, "version = 2") {
		head = "schema = 2\n" // 佈局第 2 版的家頂層要寫 schema = 2（DEV-145）
	}
	for name, data := range map[string]string{"home.toml": head + config + extra, ".gitignore": "/vault/\n/runtime/\n"} {
		if err := platform.AtomicWrite(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Fatal(err)
	}
	clock := platform.NewFakeClock(time.Date(2026, 10, 23, 12, 0, 0, 0, loc))
	store := vault.New(vault.Deps{Clock: clock})
	if err := store.Open(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return &Home{Dir: dir, Clock: clock, Vault: store, t: t}
}
func (h *Home) WithRecords(records ...sdk.Record) *Home {
	h.t.Helper()
	for _, r := range records {
		if _, err := h.Vault.Put(context.Background(), r, "system", ""); err != nil {
			h.t.Fatal(fmt.Errorf("fixture record: %w", err))
		}
	}
	return h
}

var webPortLine = regexp.MustCompile(`(?ms)(^\[web\][^\[]*?^port\s*=\s*)\d+`)

// FreeWebPort 找一個目前空著的 127.0.0.1 連接埠，寫進 <homeDir>/home.toml 的 [web] port 並回傳。
// 任何會 Start web 的測試都要先呼叫它；固定的 port 會跟正在用的舊看板（8850）或別的測試搶。
func FreeWebPort(t testing.TB, homeDir string) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(homeDir, "home.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !webPortLine.Match(data) {
		t.Fatalf("%s 沒有 [web] port 可以換", path)
	}
	data = webPortLine.ReplaceAll(data, []byte(fmt.Sprintf("${1}%d", port)))
	if err := platform.AtomicWrite(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return port
}
