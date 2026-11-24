package reminder

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

// 用 app 的方式把 Routes() 掛起來，從 HTTP 打進真的模組。
func serve(t *testing.T, m *Module) http.Handler {
	t.Helper()
	a := app.New(m)
	mux, err := a.Mux()
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

// 模組實作 RouteProvider，登記的服務名是 sdk 的 reminders。
func TestRoutesRegisterRemindersService(t *testing.T) {
	m, _, _ := fixture(t)
	var _ app.RouteProvider = m
	got := m.Routes()
	if len(got) != 1 || got[0].Service != "reminders" || got[0].Impl == nil {
		t.Fatalf("Routes = %+v", got)
	}
	serve(t, m) // 簽名不符 sdk 介面的話 Mux 會回錯
}

// 每個寫入都要帶操作者：沒帶 → 400、欄位 by、模組沒被叫到。
func TestHTTPWritesRequireActor(t *testing.T) {
	m, _, _ := fixture(t)
	h := serve(t, m)
	writes := map[string]any{
		"/api/reminders/create": &sdk.RemindersCreateRequest{Content: "x"},
		"/api/reminders/update": &sdk.RemindersUpdateRequest{ID: "R-1"},
		"/api/reminders/snooze": &sdk.RemindersSnoozeRequest{ID: "R-1", Until: "3h"},
		"/api/reminders/done":   &sdk.RemindersDoneRequest{ID: "R-1"},
		"/api/reminders/doing":  &sdk.RemindersDoingRequest{ID: "R-1"},
		"/api/reminders/cancel": &sdk.RemindersCancelRequest{ID: "R-1"},
	}
	for path, req := range writes {
		status, body := post(t, h, path, req)
		var e sdk.Error
		if err := json.Unmarshal(body, &e); err != nil || status != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
			t.Errorf("%s 沒帶 by：%d %s", path, status, body)
		}
	}
	if list, err := m.List(t.Context(), nil); err != nil || len(list) != 0 {
		t.Fatalf("被擋的寫入不能留下待辦：%v %v", list, err)
	}
}

// 建立→取得→列表→完成走一輪 HTTP；找不到的編號回 404 的白話錯誤。
func TestHTTPRoundTrip(t *testing.T) {
	m, _, _ := fixture(t)
	h := serve(t, m)
	by := &sdk.Actor{Name: "owner(web)"}
	status, body := post(t, h, "/api/reminders/create", &sdk.RemindersCreateRequest{Content: "喝水", By: by})
	var created sdk.RemindersCreateResponse
	if err := json.Unmarshal(body, &created); err != nil || status != 200 || created.Reminder == nil || created.Reminder.Content != "喝水" {
		t.Fatalf("create：%d %s", status, body)
	}
	id := created.Reminder.ID
	if status, body = post(t, h, "/api/reminders/get", &sdk.RemindersGetRequest{ID: id}); status != 200 {
		t.Fatalf("get：%d %s", status, body)
	}
	var listed sdk.RemindersListResponse
	status, body = post(t, h, "/api/reminders/list", &sdk.RemindersListRequest{})
	if err := json.Unmarshal(body, &listed); err != nil || status != 200 || len(listed.Reminders) != 1 {
		t.Fatalf("list：%d %s", status, body)
	}
	var done sdk.RemindersDoneResponse
	status, body = post(t, h, "/api/reminders/done", &sdk.RemindersDoneRequest{ID: id, By: by})
	if err := json.Unmarshal(body, &done); err != nil || status != 200 || done.Reminder.Status != sdk.ReminderStatusDone {
		t.Fatalf("done：%d %s", status, body)
	}
	status, body = post(t, h, "/api/reminders/get", &sdk.RemindersGetRequest{ID: "R-999"})
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil || status != 404 || e.Code != sdk.ErrorCodeNotFound || e.Message == "" {
		t.Fatalf("找不到：%d %s", status, body)
	}
}

// Board 登記一段首頁資料：今天的待辦，內容是 sdk.Reminder 的值切片。
func TestBoardTodayReminders(t *testing.T) {
	m, h, _ := fixture(t)
	var _ app.BoardProvider = m
	parts := m.Board()
	if len(parts) != 1 || parts[0].Key != "today_reminders" || parts[0].Load == nil {
		t.Fatalf("Board = %+v", parts)
	}
	if _, err := app.New(m).Board(); err != nil {
		t.Fatalf("app 收集首頁資料：%v", err)
	}
	at := h.Clock.Now().Add(time.Hour)
	if _, err := m.Create(t.Context(), &sdk.RemindersCreateRequest{Content: "今天的事", At: at, Follow: sdk.FollowRemind, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(t.Context(), &sdk.RemindersCreateRequest{Content: "下個月的事", At: h.Clock.Now().AddDate(0, 1, 0), Follow: sdk.FollowRemind, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}
	v, err := parts[0].Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, ok := v.([]sdk.Reminder)
	if !ok || len(got) != 1 || got[0].Content != "今天的事" {
		t.Fatalf("今天的待辦 = %#v", v)
	}
	// 前幾天到期、還沒做完的排在最前面（RMD-18）；做完的不列。
	old, err := m.Create(t.Context(), &sdk.RemindersCreateRequest{Content: "前天的事", At: h.Clock.Now().AddDate(0, 0, -2), Follow: sdk.FollowRemind, By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil {
		t.Fatal(err)
	}
	done, err := m.Create(t.Context(), &sdk.RemindersCreateRequest{Content: "昨天做完的", At: h.Clock.Now().AddDate(0, 0, -1), Follow: sdk.FollowRemind, By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Done(t.Context(), done.ID, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	v, _ = parts[0].Load(t.Context())
	got = v.([]sdk.Reminder)
	if len(got) != 2 || got[0].ID != old.ID || got[1].Content != "今天的事" {
		t.Fatalf("逾期的要排最前面、做完的不列：%#v", got)
	}
}
