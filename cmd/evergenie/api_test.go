package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-116, REQ-2, REQ-104
func TestRegistryServesAPIOverSocket(t *testing.T) {
	dir := copyFixtureHome(t, false)
	m, err := buildModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	t.Cleanup(func() {
		if err := m.app.Stop(ctx); err != nil {
			t.Error(err)
		}
		if _, err := os.Lstat(filepath.Join(dir, "runtime/sockets/api.sock")); !os.IsNotExist(err) {
			t.Errorf("socket left behind: %v", err)
		}
	})
	if err := m.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	c := sdk.Dial(filepath.Join(dir, "runtime/sockets/api.sock"))
	by := &sdk.Actor{Name: "code"}

	tasks := sdk.NewTasksClient(c)
	created, err := tasks.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "經由 API 開的單", By: by})
	if err != nil || created.Task.ID != "SEC-3" || created.Task.By != "code" {
		t.Fatalf("create: %+v %v", created, err)
	}
	got, err := tasks.Get(ctx, &sdk.TasksGetRequest{ID: "SEC-3"})
	if err != nil || got.Task.Title != "經由 API 開的單" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err = tasks.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "沒有操作者"}); err == nil || err.(*sdk.Error).Field != "by" {
		t.Fatalf("by is required: %v", err)
	}

	reminders := sdk.NewRemindersClient(c)
	one, err := reminders.Get(ctx, &sdk.RemindersGetRequest{ID: "R-1"})
	if err != nil || one.Reminder.ID != "R-1" {
		t.Fatalf("reminders.get: %+v %v", one, err)
	}
	if _, err = reminders.Get(ctx, &sdk.RemindersGetRequest{ID: "R-404"}); err == nil || err.(*sdk.Error).Code != sdk.ErrorCodeNotFound {
		t.Fatalf("missing reminder: %v", err)
	}

	store := sdk.NewVaultClient(c)
	record, err := store.Get(ctx, &sdk.VaultGetRequest{Kind: "task", ID: "SEC-3"})
	if err != nil || record.Record.ID != "SEC-3" {
		t.Fatalf("vault.get: %+v %v", record, err)
	}
	if _, err = store.NextId(ctx, &sdk.VaultNextIdRequest{Kind: "task"}); err == nil || err.(*sdk.Error).Code != sdk.ErrorCodeForbidden {
		t.Fatalf("vault.next_id must be forbidden: %v", err)
	}

	goals := sdk.NewGoalsClient(c)
	proposed, err := goals.Propose(ctx, &sdk.GoalsProposeRequest{Criteria: "測試驗收條件", Title: "整理報表", Text: "把三月的報表整理好", By: by})
	if err != nil || proposed.Goal.State != sdk.GoalStateDraft {
		t.Fatalf("goals.propose: %+v %v", proposed, err)
	}
	if _, err = goals.Withdraw(ctx, &sdk.GoalsWithdrawRequest{ID: proposed.Goal.ID, By: by}); err == nil || err.(*sdk.Error).Code != sdk.ErrorCodeForbidden {
		t.Fatalf("an agent must not withdraw a goal: %v", err)
	}
	if _, err = goals.Create(ctx, &sdk.GoalsCreateRequest{Criteria: "測試驗收條件", Title: "偷開", Text: "秘書自己開的", By: by}); err == nil || err.(*sdk.Error).Code != sdk.ErrorCodeForbidden {
		t.Fatalf("an agent must not create a goal directly: %v", err)
	}
	notices, err := sdk.NewNotifyClient(c).List(ctx, &sdk.NotifyListRequest{})
	if err != nil || notices.Unread < 1 {
		t.Fatalf("the goal confirmation should be an unread notice: %+v %v", notices, err)
	}
	if _, err = sdk.NewSessionClient(c).List(ctx, &sdk.SessionListRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err = sdk.NewBusClient(c).List(ctx, &sdk.BusListRequest{To: "code"}); err != nil {
		t.Fatal(err)
	}
}

// sdk.Routes 列的每個服務的每個方法（含串流）都有人掛在唯一的 HTTP 入口上；不會有哪個方法被搬掉了卻沒人接。
// engine 服務除外：它是核心打引擎套件的方向（套件自己 Serve），核心不掛。
// 只問 mux 這條路徑交給誰（不真的呼叫，沒有副作用）：落到 "/"（sdk.NotFoundHandler）就是沒人掛。
// Guards: REQ-116
func TestEverySDKRouteIsMounted(t *testing.T) {
	m, err := buildModules(copyFixtureHome(t, false))
	if err != nil {
		t.Fatal(err)
	}
	mux, ok := m.api.Handler().(*http.ServeMux)
	if !ok {
		t.Fatalf("HTTP 入口掛載失敗：%T", m.api.Handler())
	}
	services := map[string]int{}
	for _, route := range sdk.Routes {
		if route.Service == "engine" {
			continue
		}
		services[route.Service]++
		req, err := http.NewRequest(http.MethodPost, "http://localhost"+route.Path, strings.NewReader("{}"))
		if err != nil {
			t.Fatal(err)
		}
		if _, pattern := mux.Handler(req); pattern != "" && pattern != "/" {
			continue
		}
		t.Errorf("%s.%s（%s）沒有人掛", route.Service, route.Method, route.Path)
	}
	// 走過的服務要是 sdk 全部的服務（engine 以外）：防止 Routes 被裁掉時測試默默變空。
	for _, svc := range []string{"agents", "bus", "engine_host", "goals", "notify", "package", "personas", "policy", "reminders", "scheduler", "session", "system", "tasks", "vault", "web"} {
		if services[svc] == 0 {
			t.Errorf("sdk.Routes 沒有 %s 服務的方法", svc)
		}
	}
}
