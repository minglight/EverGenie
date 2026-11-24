package notify

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

func post(t *testing.T, h http.Handler, path string, req any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/notify/"+path, bytes.NewReader(raw)))
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
func TestHTTPWritesRequireActor(t *testing.T) {
	m, _, _, _ := fixture(t)
	h := handler(t, m)
	for _, path := range []string{"answer", "archive", "set_prefs", "subscribe_push", "unsubscribe_push", "push_test", "presence", "resolve_nudge"} {
		status, body := post(t, h, path, map[string]any{})
		var e sdk.Error
		if err := json.Unmarshal(body, &e); err != nil || status != 400 || e.Field != "by" {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
}
func TestHTTPRoundTripAndBoard(t *testing.T) {
	m, _, _, _ := fixture(t)
	h := handler(t, m)
	by := &sdk.Actor{Name: "owner(web)"}
	status, body := post(t, h, "send", &sdk.NotifySendRequest{Category: sdk.CategoryApprove, Title: "確認測試", Todo: "請回答", Agent: "code"})
	var sent sdk.NotifySendResponse
	if err := json.Unmarshal(body, &sent); err != nil || status != 200 || sent.Notification == nil {
		t.Fatalf("send: %d %s", status, body)
	}
	id := sent.Notification.ID
	for path, req := range map[string]any{"get": &sdk.NotifyGetRequest{ID: id}, "list": &sdk.NotifyListRequest{}, "get_prefs": &sdk.NotifyGetPrefsRequest{}, "set_prefs": &sdk.NotifySetPrefsRequest{Prefs: m.Prefs(), By: by}, "mark_read": &sdk.NotifyMarkReadRequest{IDS: []string{id}}} {
		if status, body := post(t, h, path, req); status != 200 {
			t.Fatalf("%s: %d %s", path, status, body)
		}
	}
	parts, err := app.New(m).Board()
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]any{}
	for _, part := range parts {
		v, err := part.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		values[part.Key] = v
	}
	if got := values["approvals"].([]sdk.Notification); len(got) != 1 || got[0].ID != id {
		t.Fatalf("approvals: %v", got)
	}
	if values["unread"] != int32(0) {
		t.Fatalf("unread: %v", values["unread"])
	}
	status, body = post(t, h, "answer", &sdk.NotifyAnswerRequest{ID: id, Text: "好", Via: sdk.AnswerViaButton, By: by})
	var answer sdk.NotifyAnswerResponse
	if err := json.Unmarshal(body, &answer); err != nil || status != 200 || answer.Notification.Answer.Text != "好" {
		t.Fatalf("answer: %d %s", status, body)
	}
	if status, body = post(t, h, "archive", &sdk.NotifyArchiveRequest{IDS: []string{id}, By: by}); status != 200 {
		t.Fatalf("archive: %d %s", status, body)
	}
	if status, body = post(t, h, "get", &sdk.NotifyGetRequest{ID: "M-missing"}); status != 404 {
		t.Fatalf("missing: %d %s", status, body)
	}
}

// 套件的程式經 API 送通知可以帶站內連結按鈕（NTF-66、DEV-157：習慣的「到展示間」）：存得進去、讀回來有；
// 外部網址、// 開頭的照樣擋（跟核心模組寫的同一套檢查）。
func TestHTTPSendCarriesSiteLocalLink(t *testing.T) {
	m, _, _, _ := fixture(t)
	h := handler(t, m)
	status, body := post(t, h, "send", &sdk.NotifySendRequest{Category: sdk.CategoryDone, Title: "送你 1 點", Agent: "life",
		Link: &sdk.NotificationLink{Label: "到展示間", URL: "/p/closet/showroom"}})
	var sent sdk.NotifySendResponse
	if err := json.Unmarshal(body, &sent); err != nil || status != 200 || sent.Notification == nil {
		t.Fatalf("send: %d %s", status, body)
	}
	got := get(t, m, sent.Notification.ID)
	if got.Link == nil || got.Link.Label != "到展示間" || got.Link.URL != "/p/closet/showroom" {
		t.Fatalf("讀回來要有按鈕：%+v", got.Link)
	}
	for _, bad := range []sdk.NotificationLink{
		{Label: "外面", URL: "https://evil.example/x"},
		{Label: "外面", URL: "//evil.example/x"},
		{Label: "外面", URL: "/a/../b"},
		{Label: "外面", URL: `/a\b`},
		{Label: "外面", URL: "/a b"},
		{Label: "只有字"},
		{URL: "/p/habits/list"},
	} {
		status, body := post(t, h, "send", &sdk.NotifySendRequest{Category: sdk.CategoryDone, Title: "x", Agent: "life", Link: &bad})
		if status != 400 {
			t.Errorf("%+v 要被擋：%d %s", bad, status, body)
		}
	}
}

func TestHTTPUnavailableAndChecks(t *testing.T) {
	m := New(Deps{})
	h := handler(t, m)
	if status, body := post(t, h, "list", &sdk.NotifyListRequest{}); status != 503 {
		t.Fatalf("not started: %d %s", status, body)
	}
	checks, err := app.New(m).Checks()
	if err != nil || len(checks) != 1 {
		t.Fatalf("checks: %v %v", checks, err)
	}
	if got := checks[0].Run(t.Context()); got.Status != app.StatusWarn {
		t.Fatalf("check: %+v", got)
	}
}
