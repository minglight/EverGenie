package requests

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

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeAPI 記下收到的請求，回固定答案；指令只認 sdk 的 client，所以用 sdk.Serve 開一個真的 unix socket。
type fakeAPI struct {
	sdk.PolicyAPI
	mu   sync.Mutex
	last map[string]any
}

func (f *fakeAPI) record(name string, req any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[name] = req
}
func (f *fakeAPI) got(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[name]
}
func (f *fakeAPI) Request(_ context.Context, r *sdk.PolicyRequestRequest) (*sdk.PolicyRequestResponse, error) {
	f.record("request", r)
	return &sdk.PolicyRequestResponse{Request: &sdk.PolicyRequest{ID: "P-1", Status: sdk.PolicyStatusPending}}, nil
}
func (f *fakeAPI) List(_ context.Context, r *sdk.PolicyListRequest) (*sdk.PolicyListResponse, error) {
	f.record("list", r)
	return &sdk.PolicyListResponse{Requests: []sdk.PolicyRequest{{ID: "P-1"}, {ID: "P-2"}}}, nil
}
func (f *fakeAPI) Get(_ context.Context, r *sdk.PolicyGetRequest) (*sdk.PolicyGetResponse, error) {
	return &sdk.PolicyGetResponse{Request: &sdk.PolicyRequest{ID: r.ID, Status: sdk.PolicyStatusPending, Kind: sdk.PolicyKindRule, From: "code", Agent: "code", Action: sdk.ActionClassWrite, Scope: "~/x", Impact: "code 可以改 ~/x", Why: "被擋了", Fingerprint: "fp-1", CreatedAt: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)}}, nil
}
func (f *fakeAPI) Decide(_ context.Context, r *sdk.PolicyDecideRequest) (*sdk.PolicyDecideResponse, error) {
	f.record("decide", r)
	return &sdk.PolicyDecideResponse{Request: &sdk.PolicyRequest{ID: r.ID}}, nil
}
func (f *fakeAPI) Revoke(_ context.Context, r *sdk.PolicyRevokeRequest) (*sdk.PolicyRevokeResponse, error) {
	f.record("revoke", r)
	return &sdk.PolicyRevokeResponse{Request: &sdk.PolicyRequest{ID: r.ID}}, nil
}

func serveFake(t *testing.T) (*sdk.Client, *fakeAPI) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeAPI{last: map[string]any{}}
	sock := filepath.Join(dir, "api.sock")
	sctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(sctx, sock, map[string]any{"policy": f}) }()
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

func hg(t *testing.T, c *sdk.Client, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{Out: &out, Err: &errb}, Commands(), "用法\n")
	return code, out.String(), errb.String()
}

func mustHG(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errb := hg(t, c, args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errb)
	}
	return out
}

func TestCommandsListsAllPolicyCommands(t *testing.T) {
	var _ map[string]app.Command = Commands()
	want := []string{"policy", "policy request", "policy list", "policy show", "policy decide", "policy revoke"}
	got := Commands()
	if len(got) != len(want) {
		t.Fatalf("指令數 %d", len(got))
	}
	for _, name := range want {
		if got[name].Run == nil {
			t.Errorf("缺 %s", name)
		}
	}
}

// 旗標名就是請求欄位名；--status 對到 statuses；操作者來自 $EVERGENIE_AGENT。
func TestFlagsFillRequestFields(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "code")
	c, f := serveFake(t)
	mustHG(t, c, "policy", "request", "--kind", "rule", "--action", "write", "--scope", "~/x", "--why", "被擋了")
	if r := f.got("request").(*sdk.PolicyRequestRequest); r.Kind != sdk.PolicyKindRule || r.Action != sdk.ActionClassWrite || r.Scope != "~/x" || r.Why != "被擋了" || r.By.Name != "code" {
		t.Fatalf("request: %+v", r)
	}
	out := mustHG(t, c, "policy", "list", "--status", "pending", "--status", "applied", "--agent", "code")
	if r := f.got("list").(*sdk.PolicyListRequest); !reflect.DeepEqual(r.Statuses, []sdk.PolicyStatus{"pending", "applied"}) || r.Agent != "code" {
		t.Fatalf("list: %+v", r)
	}
	if strings.Count(out, "\n") != 2 {
		t.Fatalf("list 一行一筆：%q", out)
	}
	mustHG(t, c, "policy", "revoke", "P-1", "--note", "不要了")
	if r := f.got("revoke").(*sdk.PolicyRevokeRequest); r.ID != "P-1" || r.Note != "不要了" || r.By.Name != "code" {
		t.Fatalf("revoke: %+v", r)
	}
}

// decide：一定要明講 --approve 或 --reject；核准要帶指紋。
func TestDecideNeedsExplicitChoice(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	c, f := serveFake(t)
	for _, args := range [][]string{
		{"policy", "decide", "P-1"},
		{"policy", "decide", "P-1", "--approve"},
		{"policy", "decide", "P-1", "--approve", "--reject"},
	} {
		if code, out, errOut := hg(t, c, args...); code != 2 || out != "" || errOut == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errOut)
		}
	}
	if f.got("decide") != nil {
		t.Fatal("用法錯誤不該打到 API")
	}
	mustHG(t, c, "policy", "decide", "P-1", "--approve", "--fingerprint", "fp-1", "--note", "好")
	if r := f.got("decide").(*sdk.PolicyDecideRequest); !r.Approve || r.Fingerprint != "fp-1" || r.Note != "好" || r.By.Name != "owner(cli)" {
		t.Fatalf("approve: %+v", r)
	}
	mustHG(t, c, "policy", "decide", "P-2", "--reject", "--note", "太寬了")
	if r := f.got("decide").(*sdk.PolicyDecideRequest); r.Approve || r.ID != "P-2" || r.Note != "太寬了" {
		t.Fatalf("reject: %+v", r)
	}
}

// show 印白話摘要（含指紋）；--json 印一行 JSON；help 列出所有子指令，不認得的子指令是用法錯誤。
func TestShowAndHelp(t *testing.T) {
	c, _ := serveFake(t)
	out := mustHG(t, c, "policy", "show", "P-1")
	for _, want := range []string{"P-1", "pending", "code", "~/x", "被擋了", "fp-1"} {
		if !strings.Contains(out, want) {
			t.Errorf("show 少了 %q：%s", want, out)
		}
	}
	if js := mustHG(t, c, "policy", "show", "P-1", "--json"); !strings.HasPrefix(js, "{") || strings.Count(js, "\n") != 1 {
		t.Errorf("show --json：%q", js)
	}
	help := mustHG(t, c, "policy", "--help")
	for _, sub := range []string{"request", "list", "show", "decide", "revoke", "--why", "--fingerprint"} {
		if !strings.Contains(help, sub) {
			t.Errorf("help 少了 %s", sub)
		}
	}
	if code, _, errOut := hg(t, c, "policy", "frobnicate"); code != 2 || errOut == "" {
		t.Errorf("不認得的子指令：%d %q", code, errOut)
	}
}
