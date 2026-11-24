package job

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// 指令只認 sdk 的 client，所以測試用 sdk.Serve 在 /tmp 開一個真的 unix socket，再用 cmdline.Run 跑 Commands()。

// fakes 記下收到的請求；night 是夜班清單那一筆 note（vault.get／put 用；nil＝還沒有）。
type fakes struct {
	mu    sync.Mutex
	last  map[string]any
	night *sdk.Record
}

func (f *fakes) record(name string, req any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[name] = req
}

func (f *fakes) got(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[name]
}

type fakeNoteVault struct {
	sdk.VaultAPI
	f *fakes
}

func serveFakes(t *testing.T) (*sdk.Client, *fakes) {
	t.Helper()
	f := &fakes{last: map[string]any{}}
	return serve(t, map[string]any{"vault": fakeNoteVault{f: f}}), f
}

func serve(t *testing.T, services map[string]any) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, services) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(dir)
	})
	waitSocket(t, sock)
	return sdk.Dial(sock)
}

// waitSocket 等到 socket 真的連得上（檔案在 bind 時就出現、listen 之後才收連線）。
func waitSocket(t *testing.T, sock string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never accepted a connection")
}

func run(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, Commands(), "用法：evergenie jobs|night …")
	return code, out.String(), errOut.String()
}

func mustRun(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errOut := run(t, c, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errOut)
	}
	return out
}
