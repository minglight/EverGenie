package notify

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

type fakes struct {
	mu          sync.Mutex
	last        map[string]any
	fail        *sdk.Error
	sharePublic string
}

func (f *fakes) record(n string, r any) *sdk.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[n] = r
	return f.fail
}
func (f *fakes) got(n string) any { f.mu.Lock(); defer f.mu.Unlock(); return f.last[n] }

type fakeNotify struct {
	sdk.NotifyAPI
	f *fakes
}

func (s fakeNotify) Send(_ context.Context, r *sdk.NotifySendRequest) (*sdk.NotifySendResponse, error) {
	s.f.record("notify.send", r)
	return &sdk.NotifySendResponse{Notification: &sdk.Notification{ID: "M-9", Title: r.Title}}, nil
}

func (s fakeNotify) Share(_ context.Context, r *sdk.ShareRequest) (*sdk.ShareResponse, error) {
	if err := s.f.record("notify.share", r); err != nil {
		return nil, err
	}
	return &sdk.ShareResponse{URL: "/share/01X-" + filepath.Base(r.Path), Mime: "image/png", PublicBase: s.f.sharePublic}, nil
}
func serveFakes(t *testing.T) (*sdk.Client, *fakes) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakes{last: map[string]any{}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"notify": fakeNotify{f: f}}) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		_ = os.RemoveAll(dir)
	})
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return sdk.Dial(sock), f
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never accepted a connection")
	return nil, nil
}

func run(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{Out: &out, Err: &errb}, Commands(), "用法\n")
	return code, out.String(), errb.String()
}

func mustRun(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errb := run(t, c, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errb)
	}
	return out
}

func TestNotifyOptionsSplitOnBar(t *testing.T) {
	c, f := serveFakes(t)
	mustRun(t, c, "notify", "--agent", "code", "--category", "approve", "--title", "要不要？", "--todo", "決定", "--options", "好|不要", "--urgent")
	n := f.got("notify.send").(*sdk.NotifySendRequest)
	if n.Agent != "code" || n.Category != sdk.CategoryApprove || n.Title != "要不要？" || !n.Urgent || !reflect.DeepEqual(n.Options, []string{"好", "不要"}) {
		t.Fatalf("notify request: %+v", n)
	}
}

// 腳本的自動報告用 --lane system 標成系統那道（D-47，scripts/release.sh）。
func TestNotifyLaneFlag(t *testing.T) {
	c, f := serveFakes(t)
	mustRun(t, c, "notify", "--agent", "admin", "--category", "done", "--lane", "system", "--title", "每晚測試沒過")
	if n := f.got("notify.send").(*sdk.NotifySendRequest); n.Lane != sdk.NotifyLaneSystem {
		t.Fatalf("--lane not passed: %+v", n)
	}
}

func TestNotifyAgentDefaultsToCaller(t *testing.T) {
	c, f := serveFakes(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	mustRun(t, c, "notify", "--category", "done", "--title", "好了")
	if n := f.got("notify.send").(*sdk.NotifySendRequest); n.Agent != "life" {
		t.Fatalf("notify agent must default to EVERGENIE_AGENT: %+v", n)
	}
	mustRun(t, c, "notify", "--agent", "code", "--category", "done", "--title", "好了")
	if n := f.got("notify.send").(*sdk.NotifySendRequest); n.Agent != "code" {
		t.Fatalf("--agent must win: %+v", n)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	mustRun(t, c, "notify", "--category", "done", "--title", "好了")
	if n := f.got("notify.send").(*sdk.NotifySendRequest); n.Agent != "" {
		t.Fatalf("owner(cli) is not an agent: %+v", n)
	}
}

func TestShareCommandPrintsURL(t *testing.T) {
	c, f := serveFakes(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	dir := t.TempDir()
	t.Chdir(dir)
	out := mustRun(t, c, "share", "pic.png")
	req := f.got("notify.share").(*sdk.ShareRequest)
	if out != "/share/01X-pic.png\n" || req.Agent != "life" || !filepath.IsAbs(req.Path) || filepath.Base(req.Path) != "pic.png" {
		t.Fatalf("share: out %q request %+v", out, req)
	}
	mustRun(t, c, "share", "--agent", "code", "/tmp/a.png")
	if req := f.got("notify.share").(*sdk.ShareRequest); req.Agent != "code" || req.Path != "/tmp/a.png" {
		t.Fatalf("--agent must win: %+v", req)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	mustRun(t, c, "share", "/tmp/a.png")
	if req := f.got("notify.share").(*sdk.ShareRequest); req.Agent != "" {
		t.Fatalf("owner(cli) is not an agent: %+v", req)
	}

	f.sharePublic = "https://hub.example.ts.net"
	if out := mustRun(t, c, "share", "/tmp/a.png"); out != "https://hub.example.ts.net/share/01X-a.png\n" {
		t.Fatalf("public_url must prefix the URL: %q", out)
	}
	f.sharePublic = ""

	for _, args := range [][]string{{"share"}, {"share", "a", "b"}, {"share", "--nope", "x", "a"}} {
		if code, out, errOut := run(t, c, "", args...); code != 2 || out != "" || errOut == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errOut)
		}
	}
	f.fail = &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "這個檔案不能分享"}
	if code, out, errOut := run(t, c, "", "share", "/tmp/a.png"); code != 1 || out != "" || !strings.HasPrefix(errOut, "這個檔案不能分享") {
		t.Errorf("API error must print its message: %d %q %q", code, out, errOut)
	}
}
