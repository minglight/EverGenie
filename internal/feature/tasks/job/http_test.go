package job

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

func post(t *testing.T, h http.Handler, path string, req any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/scheduler/"+path, bytes.NewReader(raw)))
	return w.Code, w.Body.Bytes()
}

func handler(t *testing.T, m *Module) http.Handler {
	t.Helper()
	h, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// 四個會寫入的方法沒帶操作者（by）一律 400，不會動到 vault。
func TestHTTPWritesRequireActor(t *testing.T) {
	m, s := setupJobStore(t)
	h := handler(t, m.Module)
	for _, path := range []string{"save_job", "set_job", "remove_job", "run_job"} {
		status, body := post(t, h, path, map[string]any{})
		var e sdk.Error
		if err := json.Unmarshal(body, &e); err != nil || status != 400 || e.Field != "by" {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	if s.puts != 0 || s.patches != 0 {
		t.Fatal("沒帶 by 不能寫")
	}
}

// 一輪來回：list → save → set → run → remove，操作者都帶到 vault；錯誤對照白話。
func TestHTTPRoundTripAndErrors(t *testing.T) {
	m, s := setupJobStore(t)
	h := handler(t, m.Module)
	by := &sdk.Actor{Name: "owner(web)"}

	status, body := post(t, h, "list_jobs", &sdk.SchedulerListJobsRequest{})
	var list sdk.SchedulerListJobsResponse
	if err := json.Unmarshal(body, &list); err != nil || status != 200 || len(list.Jobs) != 1 || list.Jobs[0].NextRun == nil {
		t.Fatalf("list_jobs: %d %s", status, body)
	}

	status, body = post(t, h, "save_job", &sdk.SchedulerSaveJobRequest{By: by, Job: &sdk.Job{ID: "code/new", Name: "新的", Cron: []string{"0 8 * * *"}, Enabled: true, Body: "做事"}})
	if status != 200 {
		t.Fatalf("save_job: %d %s", status, body)
	}
	if got := s.jobs["code/new"]; got.Fields["by"] == nil && got.Body != "做事" {
		t.Fatalf("沒存進 vault：%+v", got)
	}
	// 同一個 id 再存 → 409；壞 cron → 400 且欄位是 cron。
	if status, body = post(t, h, "save_job", &sdk.SchedulerSaveJobRequest{By: by, Job: &sdk.Job{ID: "code/new", Cron: []string{"0 8 * * *"}, Body: "x"}}); status != 409 {
		t.Fatalf("重複新增要 409：%d %s", status, body)
	}
	status, body = post(t, h, "save_job", &sdk.SchedulerSaveJobRequest{By: by, Job: &sdk.Job{ID: "code/bad", Cron: []string{"nope"}, Body: "x"}})
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil || status != 400 || e.Field != "cron" {
		t.Fatalf("壞 cron：%d %s", status, body)
	}

	off := false
	if status, body = post(t, h, "set_job", &sdk.SchedulerSetJobRequest{By: by, ID: "code/ping", Enabled: &off}); status != 200 {
		t.Fatalf("set_job: %d %s", status, body)
	}
	if status, body = post(t, h, "run_job", &sdk.SchedulerRunJobRequest{By: by, ID: "code/ping"}); status != 200 {
		t.Fatalf("run_job: %d %s", status, body)
	}
	if status, body = post(t, h, "run_job", &sdk.SchedulerRunJobRequest{By: by, ID: "code/missing"}); status != 404 {
		t.Fatalf("找不到要 404：%d %s", status, body)
	}
	if status, body = post(t, h, "remove_job", &sdk.SchedulerRemoveJobRequest{By: by, ID: "code/ping"}); status != 200 {
		t.Fatalf("remove_job: %d %s", status, body)
	}
}

// 模組還沒啟動（沒讀到家）時，管理 API 回 503，不是 500。
func TestHTTPUnavailableBeforeStart(t *testing.T) {
	h := homefixture.New(t)
	m := New(Deps{Clock: h.Clock})
	status, body := post(t, handler(t, m), "list_jobs", &sdk.SchedulerListJobsRequest{})
	if status != 503 {
		t.Fatalf("還沒啟動要 503：%d %s", status, body)
	}
}

// Routes 只登記一個服務 "scheduler"（sdk 的 SchedulerAPI）。
func TestRoutesMountsSchedulerService(t *testing.T) {
	routes := New(Deps{}).Routes()
	if len(routes) != 1 || routes[0].Service != "scheduler" {
		t.Fatalf("%+v", routes)
	}
}
