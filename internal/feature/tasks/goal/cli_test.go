package goal

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
	sdk.GoalsAPI
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
func (f *fakeAPI) Propose(_ context.Context, r *sdk.GoalsProposeRequest) (*sdk.GoalsProposeResponse, error) {
	f.record("propose", r)
	return &sdk.GoalsProposeResponse{Goal: &sdk.Goal{ID: "GOAL-1", Title: r.Title}}, nil
}
func (f *fakeAPI) Get(_ context.Context, r *sdk.GoalsGetRequest) (*sdk.GoalsGetResponse, error) {
	return &sdk.GoalsGetResponse{Goal: &sdk.Goal{ID: r.ID, Title: "整理報表", Text: "把三月的報表整理好", State: sdk.GoalStateActive, Planbook: "先收集資料"}}, nil
}
func (f *fakeAPI) List(_ context.Context, r *sdk.GoalsListRequest) (*sdk.GoalsListResponse, error) {
	f.record("list", r)
	return &sdk.GoalsListResponse{Goals: []sdk.Goal{{ID: "GOAL-1"}}}, nil
}
func (f *fakeAPI) UpdatePlanbook(_ context.Context, r *sdk.GoalsUpdatePlanbookRequest) (*sdk.GoalsUpdatePlanbookResponse, error) {
	f.record("planbook", r)
	return &sdk.GoalsUpdatePlanbookResponse{Goal: &sdk.Goal{ID: r.ID, Planbook: r.Body}}, nil
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
	go func() { done <- sdk.Serve(sctx, sock, map[string]any{"goals": f}) }()
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

func hg(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errb}, Commands(), "用法\n")
	return code, out.String(), errb.String()
}

func mustHG(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errb := hg(t, c, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errb)
	}
	return out
}

func TestCommandsListsAllGoalCommands(t *testing.T) {
	var _ map[string]app.Command = Commands()
	want := []string{"goal propose", "goal show", "goal planbook", "goal list", "goal done", "goal withdraw"}
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

// 旗標名就是請求欄位名；--state 對到 states；show 印原文、狀態、Planbook 三段。
func TestGoalCommandsOnFakes(t *testing.T) {
	c, f := serveFake(t)
	mustHG(t, c, "goal", "propose", "--title", "整理報表", "--text", "把三月的報表整理好")
	if g := f.got("propose").(*sdk.GoalsProposeRequest); g.Title != "整理報表" || g.By.Name != "owner(cli)" {
		t.Fatalf("propose: %+v", g)
	}
	mustHG(t, c, "goal", "list", "--state", "active", "--state", "draft")
	if g := f.got("list").(*sdk.GoalsListRequest); !reflect.DeepEqual(g.States, []sdk.GoalState{"active", "draft"}) {
		t.Fatalf("list: %+v", g)
	}
	out := mustHG(t, c, "goal", "show", "GOAL-1")
	for _, want := range []string{"把三月的報表整理好", "active", "先收集資料"} {
		if !strings.Contains(out, want) {
			t.Fatalf("goal show lacks %q: %s", want, out)
		}
	}
	if strings.Index(out, "把三月的報表整理好") > strings.Index(out, "active") || strings.Index(out, "active") > strings.Index(out, "先收集資料") {
		t.Fatalf("goal show order must be text, state, planbook: %s", out)
	}
}

// planbook 的內文可以來自 --file、標準輸入（--file -）或 --body。
func TestPlanbookBodySources(t *testing.T) {
	c, f := serveFake(t)
	path := filepath.Join(t.TempDir(), "plan.md")
	if err := os.WriteFile(path, []byte("第一步\n第二步\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mustHG(t, c, "goal", "planbook", "GOAL-1", "--file", path, "--note", "改了順序")
	if g := f.got("planbook").(*sdk.GoalsUpdatePlanbookRequest); g.ID != "GOAL-1" || g.Body != "第一步\n第二步\n" || g.Note != "改了順序" {
		t.Fatalf("planbook from file: %+v", g)
	}
	if code, _, errOut := hg(t, c, "從標準輸入", "goal", "planbook", "GOAL-1", "--file", "-", "--note", "n"); code != 0 {
		t.Fatalf("stdin planbook: %d %s", code, errOut)
	}
	if g := f.got("planbook").(*sdk.GoalsUpdatePlanbookRequest); g.Body != "從標準輸入" {
		t.Fatalf("planbook from stdin: %+v", g)
	}
	mustHG(t, c, "goal", "planbook", "GOAL-1", "--body", "直接給", "--note", "n")
	if g := f.got("planbook").(*sdk.GoalsUpdatePlanbookRequest); g.Body != "直接給" {
		t.Fatalf("planbook body: %+v", g)
	}
}

// 用法錯誤：結束碼 2、什麼都不印到 stdout、沒打到 API。
func TestUsageErrorsExitTwo(t *testing.T) {
	c, f := serveFake(t)
	for _, args := range [][]string{
		{"goal", "planbook", "GOAL-1", "--note", "沒有內文"},
		{"goal", "planbook", "GOAL-1", "--file", "x", "--body", "y"},
		{"goal", "show"},
	} {
		code, out, errOut := hg(t, c, "", args...)
		if code != 2 || out != "" || errOut == "" {
			t.Errorf("%v → %d %q %q", args, code, out, errOut)
		}
	}
	if f.got("planbook") != nil {
		t.Fatalf("用法錯誤不該打到 API")
	}
}

func TestTakeFlag(t *testing.T) {
	rest, val, found := takeFlag([]string{"GOAL-1", "--file", "a", "--note=n", "--file=b"}, "file")
	if !found || val != "b" || !reflect.DeepEqual(rest, []string{"GOAL-1", "--note=n"}) {
		t.Fatalf("%v %q %v", rest, val, found)
	}
	rest, _, found = takeFlag([]string{"--", "--file", "x"}, "file")
	if found || !reflect.DeepEqual(rest, []string{"--", "--file", "x"}) {
		t.Fatalf("-- 之後的不取：%v %v", rest, found)
	}
}
