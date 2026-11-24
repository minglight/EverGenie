package overview

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

func approve(id string, st sdk.NotificationState) sdk.Notification {
	return sdk.Notification{ID: id, Category: sdk.CategoryApprove, State: st}
}

// notifyBoard 代替 notify 業務模組的首頁資料段：要你同意的通知（已濾掉已回答、非 approve、討論串內提問）與未讀數。
func notifyBoard(approvals []sdk.Notification, unread int32) []app.BoardPart {
	return []app.BoardPart{
		{Key: "approvals", Load: func(context.Context) (any, error) { return approvals, nil }},
		{Key: "unread", Load: func(context.Context) (any, error) { return unread, nil }},
	}
}

// otherBoard 代替 tasks、chat 的首頁資料段：一張等你處理的任務單、一個等你回覆的討論串（chat 已濾好）。
func otherBoard() []app.BoardPart {
	return []app.BoardPart{
		{Key: "waiting_tasks", Load: func(context.Context) (any, error) {
			return []sdk.WaitingTask{{Task: &sdk.Task{ID: "SEC-7", Status: sdk.TaskStatusWaitingUser, Proposed: sdk.TaskStatusDone}, WaitingDays: 1}}, nil
		}},
		{Key: "waiting_threads", Load: func(context.Context) (any, error) {
			return []sdk.ThreadSummary{{Name: "code", Status: sdk.ThreadStatusWaitingUser}}, nil
		}},
	}
}

// fixture 的首頁資料段可以換：沒給就是 notify 的兩段（兩筆要你同意的通知、2 則未讀）加 tasks、chat 的兩段。
func fixture(parts ...app.BoardPart) *Module {
	if len(parts) == 0 {
		parts = append(notifyBoard([]sdk.Notification{approve("M-1", sdk.NotificationStateUnread), approve("M-2", sdk.NotificationStateRead)}, 2), otherBoard()...)
	}
	m := New(Deps{})
	if err := m.ConsumeBoard(parts); err != nil {
		panic(err)
	}
	return m
}

// 等你處理的總數 ＝ 要你同意的通知 ＋ 等你處理的任務單（overview 自己算）；其他欄位照各段原樣。
// Guards: REQ-109
func TestOverviewCounts(t *testing.T) {
	m := fixture()
	got, err := m.Overview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if got.WaitingTotal != 3 || len(got.Approvals) != 2 || len(got.WaitingTasks) != 1 {
		t.Fatalf("等你處理 = %+v", got)
	}
	if got.Unread != 2 || len(got.WaitingThreads) != 1 || got.WaitingThreads[0].Name != "code" {
		t.Fatalf("overview = %+v", got)
	}
}

// 各業務模組登記的首頁資料段依鍵（總覽回應的 json 名稱）填進對應欄位；不認得的鍵與 waiting_total 在組裝時就回錯。
func TestOverviewFillsBoardParts(t *testing.T) {
	m := fixture(app.BoardPart{Key: "today_reminders", Load: func(context.Context) (any, error) { return []sdk.Reminder{{ID: "R-1", Content: "打給牙醫"}}, nil }})
	got, err := m.Overview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.TodayReminders) != 1 || got.TodayReminders[0].Content != "打給牙醫" || got.WaitingTotal != 0 {
		t.Fatalf("today_reminders = %+v", got)
	}
	for _, key := range []string{"no_such_slot", "waiting_total", ""} {
		if err := New(Deps{}).ConsumeBoard([]app.BoardPart{{Key: key, Load: func(context.Context) (any, error) { return nil, nil }}}); err == nil {
			t.Errorf("%q 要在組裝時回錯", key)
		}
	}
}

// 任何一段讀取失敗或回錯型別，整個總覽失敗；還沒組裝就說核心服務尚未啟動。
func TestOverviewFailsWhenAnyPartFails(t *testing.T) {
	boom := errors.New("boom")
	m := fixture(app.BoardPart{Key: "approvals", Load: func(context.Context) (any, error) { return nil, boom }})
	if _, err := m.Overview(t.Context()); !errors.Is(err, boom) {
		t.Errorf("首頁資料段失敗時 err = %v", err)
	}
	wrong := fixture(app.BoardPart{Key: "unread", Load: func(context.Context) (any, error) { return "三", nil }})
	if _, err := wrong.Overview(t.Context()); err == nil || !strings.Contains(err.Error(), "unread") {
		t.Errorf("型別不對要回錯：%v", err)
	}
	var se *sdk.Error
	if _, err := New(Deps{}).Overview(t.Context()); !errors.As(err, &se) || se.Code != sdk.ErrorCodeUnavailable {
		t.Errorf("沒組裝 err = %v", err)
	}
}

// 登記：只有 web 服務的 get_overview 一個方法（Mount.Methods），沒有指令、不提供首頁資料段；app 收得進去、啟停不出事。
func TestModuleRegistersOnlyGetOverview(t *testing.T) {
	m := fixture()
	mounts := m.Routes()
	if len(mounts) != 1 || mounts[0].Service != "web" || len(mounts[0].Methods) != 1 || mounts[0].Methods[0] != "get_overview" {
		t.Fatalf("Routes = %+v", mounts)
	}
	if _, ok := any(m).(app.BoardProvider); ok {
		t.Fatal("overview 只消費首頁資料段，不提供")
	}
	a := app.New(m)
	if err := a.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func post(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/web/get_overview", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, r)
	return w
}

// 經 app 掛起來的 /api/web/get_overview：內容原樣轉出；來源失敗時 sdk 錯誤原樣、其他錯誤一律 unavailable（白話）；沒接上來源也是 unavailable。
// Guards: REQ-109
func TestGetOverviewRoute(t *testing.T) {
	serve := func(m *Module) http.Handler {
		t.Helper()
		mux, err := app.New(m).Mux()
		if err != nil {
			t.Fatal(err)
		}
		return mux
	}
	w := post(t, serve(fixture()))
	var got sdk.WebGetOverviewResponse
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.WaitingTotal != 3 || got.Unread != 2 || len(got.WaitingTasks) != 1 {
		t.Fatalf("overview：%d %s", w.Code, w.Body.String())
	}
	if w := post(t, serve(New(Deps{}))); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("沒組裝：%d %s", w.Code, w.Body.String())
	}
	notFound := &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "x"}
	fail := func(err error) *Module {
		return fixture(app.BoardPart{Key: "waiting_threads", Load: func(context.Context) (any, error) { return nil, err }})
	}
	if w := post(t, serve(fail(notFound))); w.Code != http.StatusNotFound {
		t.Fatalf("sdk 錯誤要原樣轉出：%d %s", w.Code, w.Body.String())
	}
	if w := post(t, serve(fail(errors.New("boom")))); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("其他錯誤：%d %s", w.Code, w.Body.String())
	}
}
