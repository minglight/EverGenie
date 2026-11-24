package backupsweep

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 測試自己隔離環境：在秘書的 session 或 shell 裡跑（EVERGENIE_HOME、EVERGENIE_LAUNCH_AGENTS_DIR… 都設著）不能影響結果。
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sdk.IsProductEnv(name) {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

var taipei = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		panic(err)
	}
	return loc
}()

// testNow 是測試裡的「現在」：2026-11-22（週日）04:30。
var testNow = time.Date(2026, 11, 22, 4, 30, 0, 0, taipei)

// fakeUser 建一個假的使用者家目錄（macOS 的暫存資料夾是符號連結，先解開）。
func fakeUser(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// put 寫一個檔（上層資料夾自動建），最後修改時間設成 mod。
func put(t *testing.T, path string, mod time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("內容"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

// noRefs 是三種「在用」檢查都找不到東西。
func noRefs(Check) ([]Ref, error) { return nil, nil }

// testConfig 是只掃 user/Workspace 與 home 的設定（檢查換成 probe）。
func testConfig(t *testing.T, user, home string, probe func(Check) ([]Ref, error)) Config {
	t.Helper()
	cfg, err := LoadConfig(home, user)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Probe = probe
	cfg.GitFiles = func(string) ([]string, error) { return nil, nil }
	return cfg
}

func byPath(items []sdk.BackupSweepItem, p string) (sdk.BackupSweepItem, bool) {
	for _, it := range items {
		if it.Path == p {
			return it, true
		}
	}
	return sdk.BackupSweepItem{}, false
}

// fakeBus 記下發的事件；Subscribe 回一個測試自己餵的頻道。
type fakeBus struct {
	mu        sync.Mutex
	published []published
	events    chan *sdk.BusSubscribeResponse
}

type published struct {
	topic   string
	payload map[string]any
}

func newFakeBus() *fakeBus { return &fakeBus{events: make(chan *sdk.BusSubscribeResponse, 4)} }

func (b *fakeBus) Publish(_ context.Context, topic string, payload map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, published{topic, payload})
	return nil
}

func (b *fakeBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return b.events, nil
}

func (b *fakeBus) alerts() []published {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []published
	for _, p := range b.published {
		if p.topic == "system.alert" {
			out = append(out, p)
		}
	}
	return out
}

// fire 餵一次排程到了，等背景那一次掃描跑完，回報告。
func fire(t *testing.T, m *Module, b *fakeBus) *sdk.BackupSweepReport {
	t.Helper()
	got := make(chan *sdk.BackupSweepReport, 1)
	m.done = func(r *sdk.BackupSweepReport) { got <- r }
	b.events <- &sdk.BusSubscribeResponse{Topic: "schedule.fire", Payload: map[string]any{"id": "backup-sweep", "action": Action}}
	select {
	case r := <-got:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("排程到了沒有掃")
		return nil
	}
}
