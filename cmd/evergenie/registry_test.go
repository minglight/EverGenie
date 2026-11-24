package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/session"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/inbox/notify"
	"github.com/minglight/evergenie/sdk"
)

type fakeModule struct {
	name  string
	calls *[]string
	start func() error
	stop  func(context.Context) error
}

func (m fakeModule) Name() string { return m.name }
func (m fakeModule) Start(context.Context) error {
	*m.calls = append(*m.calls, "start "+m.name)
	if m.start != nil {
		return m.start()
	}
	return nil
}
func (m fakeModule) Stop(ctx context.Context) error {
	if m.stop != nil {
		return m.stop(ctx)
	}
	*m.calls = append(*m.calls, "stop "+m.name)
	return nil
}

// serveApp 依序啟動、ctx 結束後反序停止，並印出 start／stop 的模組名稱；啟停與回滾本身由 app 的測試守（APP-2、APP-3）。
// Guards: REQ-118
func TestStartStopOrder(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var calls []string
	a := app.New(fakeModule{name: "a", calls: &calls}, fakeModule{name: "b", calls: &calls}, fakeModule{name: "c", calls: &calls, start: func() error { cancel(); return nil }})
	var out bytes.Buffer
	if err := serveApp(ctx, a, &out, time.Second); err != nil {
		t.Fatal(err)
	}
	want := []string{"start a", "start b", "start c", "stop c", "stop b", "stop a"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("got %v want %v", calls, want)
	}
	if out.String() != "start a\nstart b\nstart c\nstop c\nstop b\nstop a\n" {
		t.Fatal(out.String())
	}
}

// 真的組裝出來的模組清單與順序（啟動順序，反序停止），一塊一格：底層在前；trust 在 engine 前、tasks 在 session 前、
// packages 在 engine 與 session 後；gateway（api）在各業務模組之後，看板（board）、定時工作的執行器最後。
// 每塊裡子模組的順序由各塊自己的測試守。清單由 app 管，順序變動要有意識地改這裡。
// Guards: REQ-118
func TestRegistryModuleOrder(t *testing.T) {
	actual, err := Build(copyFixtureHome(t, false))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range actual.Modules() {
		names = append(names, m.Name())
	}
	expected := []string{"platform", "vault", "bus", "timer", "agent", "inbox", "agents", "tasks", "trust", "system", "engine", "session", "chat", "packages", "api", "board", "jobs-runner"}
	if !reflect.DeepEqual(names, expected) {
		t.Fatalf("registry order = %v", names)
	}
}

// 啟動失敗：已啟動的反序停掉，後面的不啟動，回的錯誤講出是哪個模組（app 的 APP-2，這裡確認 serveApp 把錯誤原樣回）。
// Guards: REQ-118
func TestServeAppStartFailureRollsBack(t *testing.T) {
	var calls []string
	failure := errors.New("start failed")
	a := app.New(fakeModule{name: "a", calls: &calls}, fakeModule{name: "b", calls: &calls}, fakeModule{name: "c", calls: &calls, start: func() error { return failure }}, fakeModule{name: "d", calls: &calls})
	err := serveApp(context.Background(), a, io.Discard, time.Second)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), "c") {
		t.Fatalf("got %v", err)
	}
	want := []string{"start a", "start b", "start c", "stop b", "stop a"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("got %v want %v", calls, want)
	}
}

// copyFixtureHome copies the fixture home under /tmp: the api socket path must stay under the 104 character limit.
func copyFixtureHome(t *testing.T, empty bool) string {
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
	if empty {
		data, err := os.ReadFile("testdata/home/home.toml")
		if err != nil {
			t.Fatal(err)
		}
		if err = platform.AtomicWrite(filepath.Join(dir, "home.toml"), data, 0600); err != nil {
			t.Fatal(err)
		}
	} else if err := os.CopyFS(dir, os.DirFS("testdata/home")); err != nil {
		t.Fatal(err)
	}
	// web 會真的聽 TCP：換成臨時找的空 port，不跟舊看板（8850）或別的測試搶。
	homefixture.FreeWebPort(t, dir)
	return dir
}

// Guards: REQ-118
func TestServeFixtureHome(t *testing.T) {
	for _, empty := range []bool{false, true} {
		name := "fixture"
		if empty {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			dir := copyFixtureHome(t, empty)
			m, err := buildModules(dir)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(filepath.Join(dir, "runtime/vault.lock")); !os.IsNotExist(err) {
				t.Fatalf("Build opened vault: %v", err)
			}
			ctx := context.Background()
			t.Cleanup(func() {
				if err := m.app.Stop(ctx); err != nil {
					t.Error(err)
				}
			})
			if err = m.app.Start(ctx); err != nil {
				t.Fatal(err)
			}
			tasks, err := m.tasks.List(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			reminders, err := m.reminders.List(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			agents, err := m.agent.List(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := 2
			if empty {
				want = 0
			}
			if len(tasks) != want || len(reminders) != want || len(agents) != want {
				t.Fatalf("counts tasks=%d reminders=%d agents=%d want=%d", len(tasks), len(reminders), len(agents), want)
			}
			if !empty {
				r, err := m.tasks.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "組裝寫入測試", By: &sdk.Actor{Name: "code"}})
				if err != nil {
					t.Fatal(err)
				}
				if r.ID != "SEC-3" || r.CreatedAt.IsZero() || r.UpdatedAt.IsZero() {
					t.Fatalf("real counter/clock missing: %+v", r)
				}
				if id, ok := m.agent.Resolve(ctx, "Builder"); !ok || id != "code" {
					t.Fatalf("roster resolve: %s %v", id, ok)
				}
			}
		})
	}
}

// Guards: REQ-15, REQ-18, REQ-118
func TestFixtureHomeForIntegration(t *testing.T) {
	dir := copyFixtureHome(t, false)
	home, err := platform.LoadHome(dir)
	if err != nil {
		t.Fatal(err)
	}
	if home.Sessions.Autostart {
		t.Fatal("fixture home must not autostart sessions: only the integration test turns it on")
	}
	m, err := buildModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := m.app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	if err = m.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ping, err := m.vault.Get(ctx, "job", "code/ping")
	if err != nil {
		t.Fatal(err)
	}
	cron, _ := ping.Fields["cron"].([]any)
	if ping.Fields["enabled"] != false || ping.Fields["agent"] != "code" || len(cron) != 1 || cron[0] != "* * * * *" {
		t.Fatalf("ping job: %+v", ping.Fields)
	}
}

// Q6：任務群組併進 Goal，groups 模組整個拿掉。組裝、程式資料夾、架構文件三邊要一起沒有它。
// Guards: REQ-35, REQ-118
func TestGroupsModuleRemoved(t *testing.T) {
	actual, err := Build(copyFixtureHome(t, false))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range actual.Modules() {
		if m.Name() == "groups" {
			t.Fatal("registry 不該再組裝 groups")
		}
	}
	if _, err := os.Stat(filepath.Join("..", "..", "internal", "groups")); !os.IsNotExist(err) {
		t.Fatalf("internal/groups 應該已刪除：%v", err)
	}
	arch, err := os.ReadFile(filepath.Join("..", "..", "ai_docs", "architecture.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"| `groups` |", "| `Groups` |", "`groups`、"} {
		if strings.Contains(string(arch), banned) {
			t.Errorf("architecture.md 還留著 groups：%q", banned)
		}
	}
	for _, retired := range []string{"modules/groups.md", "schemas/group.md"} {
		if _, err := os.Stat(filepath.Join("..", "..", "ai_docs", retired)); !os.IsNotExist(err) {
			t.Errorf("%s 應該已退役：%v", retired, err)
		}
	}
}

// 工作 session 在 waiting_user 第 50 分鐘的提醒走 notify.Nudge（不是一般的 approve 通知，P7-6）；registry 直接把 notify 交給 session，
// 提醒的字由 session 組（session 的 work_test 驗內容）。
// Guards: REQ-223
var _ session.WorkNudger = (*notify.Module)(nil)
