package task

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

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// fakeAPI 記下收到的請求，回固定答案；指令只認 sdk 的 client，所以用 sdk.Serve 開一個真的 unix socket。
type fakeAPI struct {
	sdk.TasksAPI
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
func (f *fakeAPI) Create(_ context.Context, r *sdk.TasksCreateRequest) (*sdk.TasksCreateResponse, error) {
	f.record("create", r)
	return &sdk.TasksCreateResponse{Task: &sdk.Task{ID: "SEC-1", Title: r.Title}}, nil
}
func (f *fakeAPI) Update(_ context.Context, r *sdk.TasksUpdateRequest) (*sdk.TasksUpdateResponse, error) {
	f.record("update", r)
	return &sdk.TasksUpdateResponse{Task: &sdk.Task{ID: r.ID}}, nil
}
func (f *fakeAPI) Close(_ context.Context, r *sdk.TasksCloseRequest) (*sdk.TasksCloseResponse, error) {
	f.record("close", r)
	return &sdk.TasksCloseResponse{Task: &sdk.Task{ID: r.ID}}, nil
}
func (f *fakeAPI) List(_ context.Context, r *sdk.TasksListRequest) (*sdk.TasksListResponse, error) {
	f.record("list", r)
	if r.Page == nil || r.Page.Cursor == "" {
		return &sdk.TasksListResponse{Tasks: []sdk.Task{{ID: "SEC-1"}}, Page: &sdk.PageResponse{NextCursor: "c2"}}, nil
	}
	return &sdk.TasksListResponse{Tasks: []sdk.Task{{ID: "SEC-2"}}}, nil
}
func (f *fakeAPI) Waiting(_ context.Context, r *sdk.TasksWaitingRequest) (*sdk.TasksWaitingResponse, error) {
	return &sdk.TasksWaitingResponse{Tasks: []sdk.WaitingTask{{}}}, nil
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
	go func() { done <- sdk.Serve(sctx, sock, map[string]any{"tasks": f}) }()
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
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(""), Out: &out, Err: &errb}, Commands(), "用法\n")
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

func TestCommandsListsAllTaskCommands(t *testing.T) {
	var _ map[string]app.Command = Commands()
	want := []string{"tasks add", "tasks set", "tasks submit", "tasks done", "tasks get", "tasks list", "tasks stale", "tasks waiting"}
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

// 旗標名就是請求欄位名；操作者預設 owner(cli)；done 沒給 --status 就是 done；list 跟著分頁游標走完。
func TestTaskCommandsOnFakes(t *testing.T) {
	c, f := serveFake(t)
	mustHG(t, c, "tasks", "add", "--title", "整理報表")
	if g := f.got("create").(*sdk.TasksCreateRequest); g.Title != "整理報表" || g.By.Name != "owner(cli)" {
		t.Fatalf("add: %+v", g)
	}
	mustHG(t, c, "tasks", "set", "SEC-1", "--result", "處理中")
	if g := f.got("update").(*sdk.TasksUpdateRequest); g.ID != "SEC-1" || g.Result == nil || *g.Result != "處理中" {
		t.Fatalf("set: %+v", g)
	}
	mustHG(t, c, "tasks", "set", "SEC-1", "--title", "新標題", "--body", "新需求")
	if g := f.got("update").(*sdk.TasksUpdateRequest); g.Title == nil || *g.Title != "新標題" || g.Body == nil || *g.Body != "新需求" {
		t.Fatalf("set --title --body: %+v", g)
	}
	// 附件（TSK-28）：可重複給，開單是 --attachments，改單是 --add_attachments／--remove_attachments。
	mustHG(t, c, "tasks", "add", "--title", "看圖", "--attachments", "/share/a.png", "--attachments", "/share/b.pdf")
	if g := f.got("create").(*sdk.TasksCreateRequest); strings.Join(g.Attachments, ",") != "/share/a.png,/share/b.pdf" {
		t.Fatalf("add --attachments: %+v", g)
	}
	mustHG(t, c, "tasks", "set", "SEC-1", "--add_attachments", "/share/c.png", "--remove_attachments", "/share/a.png")
	if g := f.got("update").(*sdk.TasksUpdateRequest); strings.Join(g.AddAttachments, ",") != "/share/c.png" || strings.Join(g.RemoveAttachments, ",") != "/share/a.png" {
		t.Fatalf("set --add_attachments: %+v", g)
	}
	mustHG(t, c, "tasks", "done", "SEC-1", "--result", "好了")
	if g := f.got("close").(*sdk.TasksCloseRequest); g.ID != "SEC-1" || g.Status != sdk.TaskStatusDone || g.Result != "好了" {
		t.Fatalf("done: %+v", g)
	}
	mustHG(t, c, "tasks", "done", "SEC-1", "--status", "cancelled")
	if g := f.got("close").(*sdk.TasksCloseRequest); g.Status != sdk.TaskStatusCancelled {
		t.Fatalf("done --status: %+v", g)
	}
	out := mustHG(t, c, "tasks", "list")
	if !strings.Contains(out, "SEC-1") || !strings.Contains(out, "SEC-2") {
		t.Fatalf("list 要跟著游標印完兩頁：%s", out)
	}
	if out = mustHG(t, c, "tasks", "waiting"); out == "" {
		t.Fatal("waiting 沒印東西")
	}
}

// 用法錯誤：結束碼 2、什麼都不印到 stdout、沒打到 API。
func TestUsageErrorsExitTwo(t *testing.T) {
	c, f := serveFake(t)
	for _, args := range [][]string{
		{"tasks", "set"},
		{"tasks", "done"},
		{"tasks", "get"},
	} {
		code, out, errOut := hg(t, c, args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errOut)
		}
	}
	if f.got("update") != nil || f.got("close") != nil {
		t.Fatalf("用法錯誤不該打到 API")
	}
}
