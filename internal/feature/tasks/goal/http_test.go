package goal

import (
	"bytes"
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

// 模組實作 RouteProvider，登記的服務名是 sdk 的 goals。
func TestRoutesRegisterGoalsService(t *testing.T) {
	m, _, _, _ := fixture(t)
	var _ app.RouteProvider = m
	got := m.Routes()
	if len(got) != 1 || got[0].Service != "goals" || got[0].Impl == nil {
		t.Fatalf("Routes = %+v", got)
	}
	serve(t, m) // 簽名不符 sdk 介面的話 Mux 會回錯
}

// 每個寫入都要帶操作者：沒帶 → 400、欄位 by、模組沒被叫到。
func TestHTTPWritesRequireActor(t *testing.T) {
	m, _, _, _ := fixture(t)
	h := serve(t, m)
	writes := map[string]func(by *sdk.Actor) any{
		"/api/goals/propose": func(by *sdk.Actor) any {
			return &sdk.GoalsProposeRequest{Criteria: "測試驗收條件", Title: "x", Text: "y", By: by}
		},
		"/api/goals/create": func(by *sdk.Actor) any {
			return &sdk.GoalsCreateRequest{Criteria: "測試驗收條件", Title: "x", Text: "y", By: by}
		},
		"/api/goals/update_planbook": func(by *sdk.Actor) any { return &sdk.GoalsUpdatePlanbookRequest{ID: "GOAL-1", By: by} },
		"/api/goals/done":            func(by *sdk.Actor) any { return &sdk.GoalsDoneRequest{ID: "GOAL-1", Summary: "s", By: by} },
		"/api/goals/withdraw":        func(by *sdk.Actor) any { return &sdk.GoalsWithdrawRequest{ID: "GOAL-1", By: by} },
	}
	for path, build := range writes {
		for _, missing := range []*sdk.Actor{nil, {}, {Name: "  "}} {
			status, body := post(t, h, path, build(missing))
			if e := decodeError(t, body); status != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
				t.Errorf("%s 沒帶 by：%d %s", path, status, body)
			}
		}
	}
	if list, err := m.List(ctx, nil); err != nil || len(list) != 0 {
		t.Fatalf("被擋的寫入不能留下 Goal：%v %v", list, err)
	}
}

// 秘書提案後不能自己收回；主人收回才算；取得、列表、選項都通。
func TestHTTPWithdrawNeedsOwner(t *testing.T) {
	m, _, _, _ := fixture(t)
	h := serve(t, m)
	status, body := post(t, h, "/api/goals/propose", &sdk.GoalsProposeRequest{Criteria: "測試驗收條件", Title: "整理報表", Text: "把三月的報表整理好", By: &sdk.Actor{Name: "code"}})
	var proposed sdk.GoalsProposeResponse
	if err := json.Unmarshal(body, &proposed); err != nil || status != 200 || proposed.Goal.State != sdk.GoalStateDraft || proposed.Goal.Agent != "code" {
		t.Fatalf("propose: %d %s", status, body)
	}
	id := proposed.Goal.ID
	status, body = post(t, h, "/api/goals/withdraw", &sdk.GoalsWithdrawRequest{ID: id, By: &sdk.Actor{Name: "code"}})
	if e := decodeError(t, body); status != 403 || e.Code != sdk.ErrorCodeForbidden || e.Message == "" {
		t.Fatalf("秘書收回了 Goal：%d %s", status, body)
	}
	if got, err := m.Get(ctx, id); err != nil || got.State != sdk.GoalStateDraft {
		t.Fatalf("Goal 被改了：%+v %v", got, err)
	}
	status, body = post(t, h, "/api/goals/withdraw", &sdk.GoalsWithdrawRequest{ID: id, By: &sdk.Actor{Name: "owner(web)"}})
	var withdrawn sdk.GoalsWithdrawResponse
	if err := json.Unmarshal(body, &withdrawn); err != nil || status != 200 || withdrawn.Goal.State != sdk.GoalStateWithdrawn {
		t.Fatalf("主人收回：%d %s", status, body)
	}
	if status, body = post(t, h, "/api/goals/get", &sdk.GoalsGetRequest{ID: id}); status != 200 {
		t.Fatalf("get: %d %s", status, body)
	}
	var listed sdk.GoalsListResponse
	status, body = post(t, h, "/api/goals/list", &sdk.GoalsListRequest{})
	if err := json.Unmarshal(body, &listed); err != nil || status != 200 || len(listed.Goals) != 1 {
		t.Fatalf("list: %d %s", status, body)
	}
}

// 找不到的編號回 404 的白話錯誤。
func TestHTTPGetMissingIs404(t *testing.T) {
	m, _, _, _ := fixture(t)
	status, body := post(t, serve(t, m), "/api/goals/get", &sdk.GoalsGetRequest{ID: "GOAL-404"})
	if e := decodeError(t, body); status != 404 || e.Code != sdk.ErrorCodeNotFound || e.Message == "" {
		t.Fatalf("%d %s", status, body)
	}
}
