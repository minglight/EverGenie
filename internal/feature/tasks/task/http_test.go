package task

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

// 用 app 的方式把 Routes() 掛起來，從 HTTP 打進真的模組。
func serve(t *testing.T, m *Module) http.Handler {
	t.Helper()
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func post(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rec.Code, rec.Body.Bytes()
}

func decodeError(t *testing.T, body []byte) sdk.Error {
	t.Helper()
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("不是錯誤本體：%s", body)
	}
	return e
}

// 模組實作 RouteProvider，登記的服務名是 sdk 的 tasks。
func TestRoutesRegisterTasksService(t *testing.T) {
	m, _, _ := fixture(t)
	var _ app.RouteProvider = m
	got := m.Routes()
	if len(got) != 1 || got[0].Service != "tasks" || got[0].Impl == nil {
		t.Fatalf("Routes = %+v", got)
	}
	serve(t, m) // 簽名不符 sdk 介面的話 Mux 會回錯
}

// 每個寫入都要帶操作者：沒帶 → 400、欄位 by、模組沒被叫到（沒有留下單）。
func TestHTTPWritesRequireActor(t *testing.T) {
	m, _, _ := fixture(t)
	h := serve(t, m)
	writes := map[string]func(by *sdk.Actor) any{
		"/api/tasks/create": func(by *sdk.Actor) any {
			return &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "x", By: by}
		},
		"/api/tasks/update":  func(by *sdk.Actor) any { return &sdk.TasksUpdateRequest{ID: "SEC-1", By: by} },
		"/api/tasks/submit":  func(by *sdk.Actor) any { return &sdk.TasksSubmitRequest{ID: "SEC-1", By: by} },
		"/api/tasks/accept":  func(by *sdk.Actor) any { return &sdk.TasksAcceptRequest{ID: "SEC-1", By: by} },
		"/api/tasks/archive": func(by *sdk.Actor) any { return &sdk.TasksArchiveRequest{IDS: []string{"SEC-1"}, By: by} },
		"/api/tasks/reject":  func(by *sdk.Actor) any { return &sdk.TasksRejectRequest{ID: "SEC-1", By: by} },
		"/api/tasks/close":   func(by *sdk.Actor) any { return &sdk.TasksCloseRequest{ID: "SEC-1", By: by} },
	}
	for path, build := range writes {
		for _, missing := range []*sdk.Actor{nil, {}, {Name: "  "}} {
			status, body := post(t, h, path, build(missing))
			if e := decodeError(t, body); status != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
				t.Errorf("%s 沒帶 by：%d %s", path, status, body)
			}
		}
	}
	if list, err := m.List(context.Background(), nil); err != nil || len(list) != 0 {
		t.Fatalf("被擋的寫入不能留下單：%v %v", list, err)
	}
}

// 開單、取得、列表、更新、停住的單、等你的單都走得通；秘書不能自己驗收，錯誤對應 403；沒有的單 404。
func TestHTTPRoundTripAndErrors(t *testing.T) {
	m, _, _ := fixture(t)
	h := serve(t, m)
	code := &sdk.Actor{Name: "code"}
	status, body := post(t, h, "/api/tasks/create", &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "整理報表", By: code})
	var created sdk.TasksCreateResponse
	if err := json.Unmarshal(body, &created); err != nil || status != 200 || created.Task == nil || created.Task.Title != "整理報表" {
		t.Fatalf("create: %d %s", status, body)
	}
	id := created.Task.ID
	if status, body = post(t, h, "/api/tasks/get", &sdk.TasksGetRequest{ID: id}); status != 200 {
		t.Fatalf("get: %d %s", status, body)
	}
	var listed sdk.TasksListResponse
	status, body = post(t, h, "/api/tasks/list", &sdk.TasksListRequest{})
	if err := json.Unmarshal(body, &listed); err != nil || status != 200 || len(listed.Tasks) != 1 {
		t.Fatalf("list: %d %s", status, body)
	}
	status, body = post(t, h, "/api/tasks/update", &sdk.TasksUpdateRequest{ID: id, Result: ptr("處理中"), By: code})
	if status != 200 {
		t.Fatalf("update: %d %s", status, body)
	}
	for _, path := range []string{"/api/tasks/stale", "/api/tasks/waiting"} {
		var req any = &sdk.TasksStaleRequest{}
		if path == "/api/tasks/waiting" {
			req = &sdk.TasksWaitingRequest{}
		}
		if status, body = post(t, h, path, req); status != 200 {
			t.Fatalf("%s: %d %s", path, status, body)
		}
	}
	if status, body = post(t, h, "/api/tasks/reconcile", &sdk.TasksReconcileRequest{Agent: "code"}); status != 200 {
		t.Fatalf("reconcile: %d %s", status, body)
	}
	status, body = post(t, h, "/api/tasks/accept", &sdk.TasksAcceptRequest{ID: id, By: code})
	if e := decodeError(t, body); status != 403 || e.Code != sdk.ErrorCodeForbidden || e.Message == "" {
		t.Fatalf("秘書驗收了單：%d %s", status, body)
	}
	status, body = post(t, h, "/api/tasks/get", &sdk.TasksGetRequest{ID: "SEC-999"})
	if e := decodeError(t, body); status != 404 || e.Code != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有的單：%d %s", status, body)
	}
}
