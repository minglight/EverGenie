package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// alertHome 起整個家，接上有劇情的假引擎（事件經 api.sock 報回 session，跟真的引擎行程一樣），開好 code 的對話。
type alertHome struct {
	ctx     context.Context
	bus     sdk.BusAPI
	notices sdk.NotifyAPI
}

func startAlertHome(t *testing.T) *alertHome {
	t.Helper()
	dir := doctorHome(t)
	m, err := buildModules(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	t.Cleanup(func() {
		if err := m.app.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if err := m.app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "runtime/sockets/api.sock")
	if err := m.engines.Attach("fake", fake.NewScriptedContext(ctx, sdk.NewEngineHostClient(sdk.Dial(sock)))); err != nil {
		t.Fatal(err)
	}
	c := sdk.Dial(sock)
	if _, err := sdk.NewSessionClient(c).Open(ctx, &sdk.SessionOpenRequest{Agent: "code"}); err != nil {
		t.Fatal(err)
	}
	return &alertHome{ctx: ctx, bus: sdk.NewBusClient(c), notices: sdk.NewNotifyClient(c)}
}

// relogin 數「要重新登入」的通知（含已封存）。
func (h *alertHome) relogin() (n int, err error) {
	got, err := h.notices.List(h.ctx, &sdk.NotifyListRequest{IncludeArchived: true})
	if err != nil {
		return 0, err
	}
	for _, x := range got.Notifications {
		if strings.Contains(x.Title, "要重新登入") {
			n++
		}
	}
	return n, nil
}

// send 主人送一句給 code，等到這句記成中斷，回傳中斷的原因。
func (h *alertHome) send(t *testing.T, body string) string {
	t.Helper()
	sent, err := h.bus.Send(h.ctx, &sdk.BusSendRequest{From: "owner(web)", To: "code", Kind: sdk.EnvelopeKindChat, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	var reason string
	if err := pollUntil(h.ctx, time.Minute, 100*time.Millisecond, body+" 記成中斷", func() (bool, error) {
		got, err := h.bus.Get(h.ctx, &sdk.BusGetRequest{ID: sent.Envelope.ID})
		if err != nil {
			return false, err
		}
		e := got.Envelope
		if e.Lifecycle != sdk.LifecycleInterrupted {
			return false, fmt.Errorf("目前 %s", e.Lifecycle)
		}
		if n := len(e.History); n > 0 {
			reason = e.History[n-1].Detail
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	return reason
}

// wantRelogin：等到「要重新登入」有 want 則，再等一下確定沒有多發。
func (h *alertHome) wantRelogin(t *testing.T, want int) {
	t.Helper()
	if want > 0 {
		if err := pollUntil(h.ctx, 10*time.Second, 100*time.Millisecond, "要重新登入的通知", func() (bool, error) {
			n, err := h.relogin()
			return n >= want, err
		}); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(500 * time.Millisecond) // 通知是另一個訂閱者處理：給它一點時間，確定真的沒有多發
	if n, err := h.relogin(); err != nil || n != want {
		t.Fatalf("「要重新登入」要剛好 %d 則：%d %v", want, n, err)
	}
}

// 引擎報的這一輪出錯（TURN_FAILED）從 session 一路到通知（S5-2＋S5-3）：
// 假引擎「【出錯】」（平台暫時忙不過來）不發任何通知、這句記中斷；「【出錯:auth】」連兩次只有一則「要重新登入」。
func TestTurnFailedAuthNotifiesOnceOverloadedNotAtAll(t *testing.T) {
	h := startAlertHome(t)
	if reason := h.send(t, "請做 【出錯】"); !strings.Contains(reason, "AI 平台暫時忙不過來") {
		t.Errorf("中斷要附白話原因：%q", reason)
	}
	h.wantRelogin(t, 0)
	for i := 0; i < 2; i++ {
		if reason := h.send(t, fmt.Sprintf("請做 【出錯:auth】 第 %d 次", i+1)); !strings.Contains(reason, "AI 帳號登入過期了") {
			t.Errorf("auth 的中斷原因：%q", reason)
		}
	}
	h.wantRelogin(t, 1)
	// 內文第一句寫出是哪位秘書（現任角色的名字，不是職位代號）、發生什麼（法律第 5 條；DEV-156 U-08）。
	got, err := h.notices.List(h.ctx, &sdk.NotifyListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range got.Notifications {
		if x.Title != "AI 帳號要重新登入" {
			continue
		}
		who, rest, ok := strings.Cut(x.Context, "剛剛沒辦法回你，因為 AI 帳號登入過期了。")
		if !ok || who == "" || who == "秘書" || who == "code" || !strings.Contains(rest, "重新登入") {
			t.Fatalf("內文要從「〈秘書名〉剛剛沒辦法回你…」開始：%q", x.Context)
		}
		return
	}
	t.Fatalf("找不到「AI 帳號要重新登入」：%+v", got.Notifications)
}

// 帳號登出（SESSION_ENDED reason＝logged_out）從引擎一路到通知：這句記中斷「AI 帳號登出了，要重新登入」，剛好一則「要重新登入」；
// 對話開回來又登出一次，還是只有一則。
func TestLoggedOutNotifiesOnce(t *testing.T) {
	h := startAlertHome(t)
	for i := 0; i < 2; i++ {
		if reason := h.send(t, fmt.Sprintf("請做 【登出】 第 %d 次", i+1)); !strings.Contains(reason, "AI 帳號登出了，要重新登入") {
			t.Errorf("登出的中斷原因：%q", reason)
		}
		h.wantRelogin(t, 1)
	}
}

// 問題沒了，通知跟著消失（S5-5，NTF-55）：「【出錯:auth】」→ 一則「要重新登入」；同一個對話下一句正常做完
// （session.recovered after＝auth）→ 那則自動收掉（記回答 auto、封存），訊息頁不再有它。
func TestReloginAlertRetiredWhenAccountWorksAgain(t *testing.T) {
	h := startAlertHome(t)
	h.send(t, "請做 【出錯:auth】")
	h.wantRelogin(t, 1)
	sent, err := h.bus.Send(h.ctx, &sdk.BusSendRequest{From: "owner(web)", To: "code", Kind: sdk.EnvelopeKindChat, Body: "登入好了，再試一次"})
	if err != nil {
		t.Fatal(err)
	}
	if err := pollUntil(h.ctx, time.Minute, 100*time.Millisecond, "這句做完、提醒收掉", func() (bool, error) {
		got, err := h.bus.Get(h.ctx, &sdk.BusGetRequest{ID: sent.Envelope.ID})
		if err != nil {
			return false, err
		}
		if got.Envelope.Lifecycle != sdk.LifecycleReplied {
			return false, fmt.Errorf("這句目前 %s", got.Envelope.Lifecycle)
		}
		open, err := h.notices.List(h.ctx, &sdk.NotifyListRequest{})
		if err != nil {
			return false, err
		}
		for _, x := range open.Notifications {
			if strings.Contains(x.Title, "要重新登入") {
				return false, fmt.Errorf("還開著：%s", x.Title)
			}
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	all, err := h.notices.List(h.ctx, &sdk.NotifyListRequest{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all.Notifications {
		if strings.Contains(x.Title, "要重新登入") && (x.Answer == nil || x.Answer.Via != sdk.AnswerViaAuto) {
			t.Fatalf("收掉的要記成自動回答：%+v", x)
		}
	}
}

func pollUntil(ctx context.Context, timeout, every time.Duration, what string, cond func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		ok, err := cond()
		if ok {
			return nil
		}
		last = err
		if time.Now().After(deadline) || ctx.Err() != nil {
			return fmt.Errorf("等不到%s：%v", what, last)
		}
		time.Sleep(every)
	}
}
