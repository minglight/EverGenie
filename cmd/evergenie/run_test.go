package main

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

// fakes 記下收到的請求；沒有覆寫的方法直接呼叫會 panic（內嵌的 interface 是 nil）。
type fakes struct {
	mu   sync.Mutex
	last map[string]any
	fail *sdk.Error
}

func (f *fakes) record(name string, req any) *sdk.Error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last[name] = req
	return f.fail
}

func (f *fakes) got(name string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last[name]
}

type fakeTasks struct {
	sdk.TasksAPI
	f *fakes
}

func (s fakeTasks) Create(_ context.Context, r *sdk.TasksCreateRequest) (*sdk.TasksCreateResponse, error) {
	if err := s.f.record("tasks.create", r); err != nil {
		return nil, err
	}
	return &sdk.TasksCreateResponse{Task: &sdk.Task{ID: "SEC-9", Title: r.Title}}, nil
}

func (s fakeTasks) Update(_ context.Context, r *sdk.TasksUpdateRequest) (*sdk.TasksUpdateResponse, error) {
	s.f.record("tasks.update", r)
	return &sdk.TasksUpdateResponse{Task: &sdk.Task{ID: r.ID}}, nil
}

func (s fakeTasks) Close(_ context.Context, r *sdk.TasksCloseRequest) (*sdk.TasksCloseResponse, error) {
	s.f.record("tasks.close", r)
	return &sdk.TasksCloseResponse{Task: &sdk.Task{ID: r.ID}}, nil
}

func (s fakeTasks) Get(_ context.Context, r *sdk.TasksGetRequest) (*sdk.TasksGetResponse, error) {
	if err := s.f.record("tasks.get", r); err != nil {
		return nil, err
	}
	return &sdk.TasksGetResponse{Task: &sdk.Task{ID: r.ID}}, nil
}

func (s fakeTasks) List(_ context.Context, r *sdk.TasksListRequest) (*sdk.TasksListResponse, error) {
	s.f.record("tasks.list", r)
	if r.Page == nil {
		return &sdk.TasksListResponse{Tasks: []sdk.Task{{ID: "SEC-1"}, {ID: "SEC-2"}}, Page: &sdk.PageResponse{NextCursor: "p2"}}, nil
	}
	return &sdk.TasksListResponse{Tasks: []sdk.Task{{ID: "SEC-3"}}}, nil
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
	go func() {
		done <- sdk.Serve(ctx, sock, map[string]any{
			"tasks": fakeTasks{f: f},
		})
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	waitSocket(t, sock)
	return sdk.Dial(sock), f
}

// waitSocket 等到 socket 真的連得上：檔案在 bind 時就出現、listen 之後才收連線，
// 只看檔案在不在的話，負載高時第一個請求會被拒絕（TestDoctorJSON 曾因此時好時壞）。
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

// run 帶上測試用的業務模組的指令表（testFeatureCommands）：cli 不能 import 業務模組，所以通用行為的測試用一份形狀相同的 tasks 指令。
func run(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runCLI(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, testFeatureCommands())
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

// Guards: REQ-49, REQ-59
func TestFlagsFillRequestFields(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "code")
	c, f := serveFakes(t)

	out := mustRun(t, c, "tasks", "add", "--title", "整理 <報表> & 備份", "--agent", "code", "--type", "task", "--status", "doing", "--body", "內文")
	if !strings.Contains(out, "整理 <報表> & 備份") || strings.Count(out, "\n") != 1 {
		t.Fatalf("add should print one unescaped JSON line: %q", out)
	}
	if got := f.got("tasks.create").(*sdk.TasksCreateRequest); got.Title != "整理 <報表> & 備份" || got.Agent != "code" || got.Type != sdk.TaskTypeTask || got.Status != sdk.TaskStatusDoing || got.Body != "內文" || got.By.Name != "code" {
		t.Fatalf("create request: %+v", got)
	}

	mustRun(t, c, "tasks", "set", "SEC-9", "--status", "waiting_user", "--goal", "", "--add_links", "a", "--add_links=b")
	up := f.got("tasks.update").(*sdk.TasksUpdateRequest)
	if up.ID != "SEC-9" || up.Status == nil || *up.Status != sdk.TaskStatusWaitingUser || up.Goal == nil || *up.Goal != "" || up.Result != nil || !reflect.DeepEqual(up.AddLinks, []string{"a", "b"}) {
		t.Fatalf("update request: %+v", up)
	}

	mustRun(t, c, "tasks", "done", "SEC-9", "--result", "好了", "--verify", "verified", "--verify_how", "跑過測試")
	if cl := f.got("tasks.close").(*sdk.TasksCloseRequest); cl.Status != sdk.TaskStatusDone || cl.Verify != sdk.VerifyVerified || cl.VerifyHow != "跑過測試" || cl.Result != "好了" {
		t.Fatalf("close request: %+v", cl)
	}
	mustRun(t, c, "tasks", "done", "SEC-9", "--status", "cancelled", "--result", "不做了")
	if cl := f.got("tasks.close").(*sdk.TasksCloseRequest); cl.Status != sdk.TaskStatusCancelled {
		t.Fatalf("explicit status must win: %+v", cl)
	}
}

// Guards: REQ-49, REQ-116
func TestListsPrintOneLinePerItemAcrossPages(t *testing.T) {
	c, _ := serveFakes(t)
	lines := strings.Split(strings.TrimSpace(mustRun(t, c, "tasks", "list", "--agent", "code")), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "SEC-1") || !strings.Contains(lines[2], "SEC-3") {
		t.Fatalf("tasks list: %q", lines)
	}
}

// Guards: REQ-23

// Guards: REQ-23

// Guards: REQ-57
func TestByFromEnv(t *testing.T) {
	c, f := serveFakes(t)
	t.Setenv("EVERGENIE_AGENT", "life")
	mustRun(t, c, "tasks", "add", "--title", "x")
	if f.got("tasks.create").(*sdk.TasksCreateRequest).By.Name != "life" {
		t.Fatalf("EVERGENIE_AGENT must be the actor: %+v", f.got("tasks.create"))
	}
	t.Setenv("EVERGENIE_AGENT", "")
	mustRun(t, c, "tasks", "add", "--title", "x")
	if f.got("tasks.create").(*sdk.TasksCreateRequest).By.Name != "owner(cli)" {
		t.Fatalf("without EVERGENIE_AGENT the actor is owner(cli): %+v", f.got("tasks.create"))
	}
}

// Guards: REQ-116
func TestUsageErrorsExitTwo(t *testing.T) {
	c, f := serveFakes(t)
	cases := [][]string{
		{"tasks"},
		{"tasks", "frobnicate"},
		{"frobnicate"},
		{"tasks", "get"},
		{"tasks", "get", "SEC-1", "SEC-2"},
		{"tasks", "get", "SEC-1", "--id", "SEC-2"},
		{"tasks", "add", "--nope", "x"},
		{"tasks", "add", "--title"},
		{"tasks", "add", "stray"},
		{"tasks", "add", "--by", "x"},
		{"roster", "x"},
	}
	for _, args := range cases {
		code, out, errOut := run(t, c, "", args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errOut)
		}
	}
	for _, name := range []string{"migrate", "cutover", "rollback", "delegate"} {
		code, _, errOut := run(t, c, "", name)
		if code != 2 || !strings.Contains(errOut, "還沒有這個指令") {
			t.Errorf("%s → %d %q", name, code, errOut)
		}
	}
	if code, out, _ := run(t, c, ""); code != 0 || !strings.Contains(out, "tasks add") {
		t.Errorf("no args should print usage: %d %q", code, out)
	}
	if f.got("tasks.create") != nil {
		t.Error("a usage error must not reach the API")
	}
}

// Guards: REQ-116
func TestErrorShowsPlainMessage(t *testing.T) {
	c, f := serveFakes(t)
	f.mu.Lock()
	f.fail = &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到這張單，請確認編號"}
	f.mu.Unlock()
	code, out, errOut := run(t, c, "", "tasks", "get", "SEC-404")
	if code != 1 || out != "" || errOut != "找不到這張單，請確認編號\n" {
		t.Fatalf("api error: %d %q %q", code, out, errOut)
	}

	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	code, _, errOut = run(t, sdk.Dial(filepath.Join(dir, "none.sock")), "", "tasks", "get", "SEC-1")
	if code != 1 || errOut != "連不到 EverGenie，請確認 serve 在跑\n" {
		t.Fatalf("serve not running: %d %q", code, errOut)
	}

	code, _, errOut = run(t, nil, "", "tasks", "get", "SEC-1")
	if code != 1 || !strings.HasPrefix(errOut, "連不到 EverGenie") || !strings.Contains(errOut, "EVERGENIE_API_SOCKET") || strings.Count(errOut, "\n") != 1 {
		t.Fatalf("no socket known: %d %q", code, errOut)
	}
}

// Guards: REQ-34

// testFeatureCommands 模擬業務模組登記進來的指令表：形狀跟 internal/feature/tasks/task 的 Commands() 一樣
// （ID 位置參數、預設狀態、分頁、旗標填欄位），用來測 runCLI 通用的旗標、用法、錯誤行為（不依賴真的業務模組指令）。
// 真的 tasks 指令的測試在 internal/feature/tasks/task/cli_test.go。
func testFeatureCommands() map[string]app.Command {
	api := func(e *cmdline.Env) sdk.TasksAPI { return sdk.NewTasksClient(e.Client) }
	return map[string]app.Command{
		"tasks add": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{}, api(e).Create, func(r *sdk.TasksCreateResponse) any { return r.Task })
		}},
		"tasks set": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Update, func(r *sdk.TasksUpdateResponse) any { return r.Task })
		}},
		"tasks done": {Run: func(e *cmdline.Env, a []string) error {
			p, err := cmdline.Prepare[sdk.TasksCloseRequest](e, a, cmdline.Spec{ID: true})
			if err != nil {
				return err
			}
			if !p.Set["status"] {
				p.Req.Status = sdk.TaskStatusDone
			}
			return cmdline.Send(e, p, api(e).Close, func(r *sdk.TasksCloseResponse) any { return r.Task })
		}},
		"tasks get": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Get, func(r *sdk.TasksGetResponse) any { return r.Task })
		}},
		"tasks list": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.Each(e, a, cmdline.Spec{}, api(e).List,
				func(r *sdk.TasksListRequest, cursor string) { r.Page = &sdk.PageRequest{Cursor: cursor} },
				func(r *sdk.TasksListResponse) ([]sdk.Task, string) { return r.Tasks, cmdline.NextCursor(r.Page) })
		}},
	}
}
