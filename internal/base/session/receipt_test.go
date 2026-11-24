package session

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 狀態與送達只照引擎回報的事實走（S5-2）：計時只決定多久再試、畫面寫什麼，不改狀態、不開單、不發通知。
// 情境 A～F 是 DEV-70 的真機情境（ac46aa5a 的 receipt_test.go），改寫成新規則。

// alarmEvents 訂閱核心以前會自己發的三種警示（系統提醒、收掉提醒、掉包事件）；新規則下 session 一則都不該發。
func alarmEvents(t *testing.T, f *dispatchFixture) <-chan *sdk.BusSubscribeResponse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	out := make(chan *sdk.BusSubscribeResponse, 64)
	for _, topic := range []string{"system.alert", "system.alert.cleared", "session.incident"} {
		ch, err := f.b.Subscribe(ctx, topic)
		must(t, err)
		go func() {
			for ev := range ch {
				out <- ev
			}
		}()
	}
	return out
}

func noAlarm(t *testing.T, ch <-chan *sdk.BusSubscribeResponse) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("session 不該自己發警示：%s %+v", ev.Topic, ev.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

func (f *dispatchFixture) notified() int {
	n := f.m.d.Notify.(*testNotify)
	n.mu.Lock()
	defer n.mu.Unlock()
	return len(n.notices)
}

// typed：引擎回報輸入框送出了一句話（主人打的，或派送器貼的）。
func (f *dispatchFixture) typed(t *testing.T, input string) {
	t.Helper()
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: input})
}

// pasteFails：引擎一直說貼不進去（壓縮中的輸入框不收），派送器退避重試幾輪。話始終留在排隊。
func (f *dispatchFixture) pasteFails(t *testing.T, e *sdk.Envelope) {
	t.Helper()
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "paste"})
	for i := 0; i < 6; i++ {
		f.tick()
		f.h.Clock.Advance(time.Second)
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) < 2 {
		t.Fatalf("要重試貼上：%d 次", len(f.deliveries()))
	}
}

// redMessages：泡泡清單上的紅字（沒送到、這一輪沒做完），回封包 id。
func (f *dispatchFixture) redMessages(t *testing.T, name string) []string {
	t.Helper()
	var out []string
	for _, m := range f.messagesOf(t, name) {
		if m.Lifecycle == sdk.LifecycleFailed || m.Lifecycle == sdk.LifecycleInterrupted {
			out = append(out, m.ID)
		}
	}
	return out
}

func (f *dispatchFixture) sessionState(t *testing.T, name string) sdk.SessionState {
	t.Helper()
	st, ok := f.m.State(name)
	if !ok {
		t.Fatalf("沒有 %s 這個對話", name)
	}
	return st
}

// 沒有引擎事件，什麼都不動：處理中、已貼上、直接傳話都放著（秘書的話排久了改送分身是派送的決定，另有測試）；假時鐘走 24 小時，狀態與每則話都跟原本一樣，
// 不開單、不發系統提醒、不發通知、不回音。
func TestNoEngineEventNoChange24h(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	direct := f.direct(t)
	working := f.send(t, ownerChat("跑整套測試"))
	f.begin(t, working)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: "Bash"})
	pasted := f.send(t, ownerChat("跑完跟我說")) // 主人的話在她忙時也貼進去（中途補話），之後就等引擎回報
	f.tick()
	f.want(t, pasted.ID, sdk.LifecycleNotified)
	before := map[string]sdk.Lifecycle{}
	for _, e := range []*sdk.Envelope{working, pasted, direct} {
		before[e.ID] = f.get(t, e.ID).Lifecycle
	}
	states := map[string]sdk.SessionState{"code": f.sessionState(t, "code"), "work": f.sessionState(t, "work")}
	notices := f.notified()
	for i := 0; i < 24*6; i++ {
		f.h.Clock.Advance(10 * time.Minute)
		f.tick()
		f.m.syncWaits(context.Background(), f.h.Clock.Now())
	}
	for id, lc := range before {
		f.want(t, id, lc)
	}
	for name, st := range states {
		if got := f.sessionState(t, name); got != st {
			t.Fatalf("%s 的狀態不能因為時間變：%s → %s", name, st, got)
		}
	}
	if f.sessionState(t, "code") != sdk.SessionStateBusy {
		t.Fatalf("處理中就是處理中：%s", f.sessionState(t, "code"))
	}
	if len(f.echoes(t)) != 0 || f.notified() != notices {
		t.Fatalf("不回音、不發通知：%v %d→%d", f.echoes(t), notices, f.notified())
	}
	if red := f.redMessages(t, "code"); len(red) != 0 {
		t.Fatalf("不能因為時間標紅字：%v", red)
	}
	noAlarm(t, alarms)
}

// 引擎回報這一輪出錯（TURN_FAILED）：她記回待命；這一輪的話記中斷，原因帶錯誤種類（不認得的當 unknown），
// 平台原話存進那則話；聊天的泡泡寫出原因，主人看得到要不要再說一次。不發系統提醒（要主人處理的由通知模組判斷）。
func TestTurnFailedInterruptsWithReason(t *testing.T) {
	for _, tc := range []struct {
		kind sdk.EngineErrorKind
		want string
	}{
		{sdk.EngineErrorKindOverloaded, "AI 平台暫時忙不過來"},
		{sdk.EngineErrorKindAuth, "AI 帳號登入過期了"},
		{"something-new", "出錯了"},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			f := dispatchSetup(t)
			alarms := alarmEvents(t, f)
			e := f.send(t, ownerChat("整理一下信件"))
			f.begin(t, e)
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: "Bash"})
			f.want(t, e.ID, sdk.LifecycleWorking)
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: tc.kind, LastMessage: "API Error: 529 Overloaded"})
			if st := f.sessionState(t, "code"); st != sdk.SessionStateIdle {
				t.Fatalf("出錯之後她停在輸入框：%s", st)
			}
			got := f.get(t, e.ID)
			if got.Lifecycle != sdk.LifecycleInterrupted || !strings.Contains(got.Reason, tc.want) || got.LastMessage != "API Error: 529 Overloaded" {
				t.Fatalf("記中斷、附原因與平台原話：%+v", got)
			}
			msgs := f.messagesOf(t, "code")
			// 泡泡：白話原因只講一遍、不混平台原話；原話另外給（看板收在「詳細」）；種類照 Normalize（看板決定給不給「再說一次」）。
			if len(msgs) != 1 || msgs[0].ID != e.ID || msgs[0].Reason != tc.want || msgs[0].ReasonDetail != "API Error: 529 Overloaded" ||
				msgs[0].ReasonKind != tc.kind.Normalize() || strings.Contains(msgs[0].Reason, "這一輪") {
				t.Fatalf("泡泡要寫出這一輪沒做完的原因：%+v", msgs)
			}
			noAlarm(t, alarms)
			// 之後的話照常送。
			next := f.send(t, ownerChat("再試一次"))
			f.tick()
			f.want(t, next.ID, sdk.LifecycleNotified)
		})
	}
}

// 這一輪沒做完的那句，之後她又做完一輪（主人在終端機直接打字也算，不經派送器）就不再列在 messages：
// 看板的「她這一輪沒做完」紅字跟著收掉，不掛 12 小時（S5-4）。
func TestInterruptedHiddenAfterLaterTurnEnds(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("整理一下信件"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: sdk.EngineErrorKindOverloaded, LastMessage: "API Error: 529 Overloaded"})
	f.want(t, e.ID, sdk.LifecycleInterrupted)
	if msgs := f.messagesOf(t, "code"); len(msgs) != 1 || msgs[0].ID != e.ID {
		t.Fatalf("剛出錯：要列出這一輪沒做完的那句：%+v", msgs)
	}
	f.h.Clock.Advance(time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "終端機直接打的"})
	f.h.Clock.Advance(time.Second)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好了"})
	if msgs := f.messagesOf(t, "code"); len(msgs) != 0 {
		t.Fatalf("之後做完一輪：不再列「沒做完」：%+v", msgs)
	}
}

// 這一輪沒做完、平台說不出原因也沒有原話：reason 是空的（看板只寫「她沒回完這一句」，不重複，S5-4）。
// 平台原話不混進白話原因，另外放 detail；種類只有引擎報錯、帳號登出才有（主人叫停、對話結束的空）。
func TestInterruptedReasonDetailAndKindAreSeparate(t *testing.T) {
	e := &sdk.Envelope{Lifecycle: sdk.LifecycleInterrupted}
	if got := failReason(e); got != "" || failDetail(e) != "" || failKind(e) != "" {
		t.Fatalf("說不出原因回空：%q", got)
	}
	e.LastMessage = sdk.EngineNoPlatformMessage
	if failDetail(e) != "" {
		t.Fatalf("平台沒附說明時引擎填的那句不是原話，不給 detail：%q", failDetail(e))
	}
	e.LastMessage = "API Error"
	if failReason(e) != "" || failDetail(e) != "API Error" {
		t.Fatalf("只有平台原話：原因空、原話在 detail：%q %q", failReason(e), failDetail(e))
	}
	e.Reason = authReason
	if failReason(e) != authReason || failDetail(e) != "API Error" || failKind(e) != sdk.EngineErrorKindAuth {
		t.Fatalf("原因與原話分開、種類對得回去：%q %q %q", failReason(e), failDetail(e), failKind(e))
	}
	for reason, want := range map[string]sdk.EngineErrorKind{
		loggedOutReason: sdk.EngineErrorKindAuth, billingReason: sdk.EngineErrorKindBilling, overloadedReason: sdk.EngineErrorKindOverloaded,
		rejectedReason: sdk.EngineErrorKindRejected, unknownReason: sdk.EngineErrorKindUnknown, stoppedReason: "", endedReason: "",
		// 升級前記下的舊字也認得（那些話最多再掛 12 小時）。
		"這一輪出錯了（帳號登入有問題）": sdk.EngineErrorKindAuth, "她的對話結束了（帳號登出了，要重新登入）": sdk.EngineErrorKindAuth,
	} {
		e.Reason = reason
		if got := failKind(e); got != want {
			t.Fatalf("%q 的種類：%q，要 %q", reason, got, want)
		}
	}
	// 不是中斷的不給原話與種類。
	failed := &sdk.Envelope{Lifecycle: sdk.LifecycleFailed, Reason: authReason, LastMessage: "x"}
	if failDetail(failed) != "" || failKind(failed) != "" {
		t.Fatal("送失敗的不帶原話與種類")
	}
}

// 引擎說她閒著（IDLE_HINT）：等於這一輪結束了（漏了結束回報）——收到、處理中的記已回覆，不再掛著；
// 還停在「已貼上」、沒開始的記失敗「這句還沒開始，可以重送」，主人可以重送；比這次貼上還早的提示（晚到的事件）不算。
func TestIdleHintSettlesInFlight(t *testing.T) {
	f := dispatchSetup(t)
	first := f.send(t, ownerChat("一"))
	f.begin(t, first)
	f.want(t, first.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
	f.want(t, first.ID, sdk.LifecycleReplied)

	early := f.h.Clock.Now()
	f.h.Clock.Advance(time.Second)
	second := f.send(t, ownerChat("二"))
	f.tick()
	f.want(t, second.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint, At: early}) // 晚到的提示
	f.want(t, second.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
	got := f.get(t, second.ID)
	if got.Lifecycle != sdk.LifecycleFailed || got.Reason != "這句還沒開始，可以重送" {
		t.Fatalf("閒著還沒開始的話記失敗：%+v", got)
	}
	msgs := f.messagesOf(t, "code")
	if len(msgs) != 1 || msgs[0].ID != second.ID || msgs[0].Lifecycle != sdk.LifecycleFailed {
		t.Fatalf("失敗的那則留在泡泡上給主人重送：%+v", msgs)
	}

	// 在等確認時的「閒著」不收：那一輪還沒停。
	third := f.send(t, ownerChat("三"))
	f.begin(t, third)
	f.m.mu.Lock()
	f.m.entries["code"].session.Pending = &sdk.Pending{ID: "p"}
	f.m.mu.Unlock()
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
	f.want(t, third.ID, sdk.LifecycleReceived)
}

// 核心重開，忘了這一輪是哪一則：引擎報這一輪結束（TURN_ENDED）時，這個對話還掛著收到／處理中的一起記已回覆，
// 不用等計時收尾；別的對話的不動。
func TestTurnEndedSettlesAfterCoreRestart(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("跑測試"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: "Bash"})
	other := f.send(t, &sdk.Envelope{To: "work", From: "owner(web)", Kind: sdk.EnvelopeKindChat, Body: "work 那邊的事"})
	f.tick()
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: other.Body})
	f.want(t, other.ID, sdk.LifecycleReceived)
	f.m.dispatchMu.Lock()
	f.m.turns = map[string]*dispatchTurn{} // 核心重開：記憶體裡的這一輪不見了
	f.m.dispatchMu.Unlock()
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好了"})
	got := f.get(t, e.ID)
	if got.Lifecycle != sdk.LifecycleReplied || got.LastMessage != "好了" {
		t.Fatalf("這一輪結束了，掛著的記已回覆：%+v", got)
	}
	f.want(t, other.ID, sdk.LifecycleReceived)
}

// 秘書直接傳話：寄件方的平台說送出成功（ACTIVITY_ENDED ok=true）就記收到，不等收件方開始處理、也沒有 10 分鐘的判失敗；
// 收件方之後開一輪時照樣配上這則，回覆了就記已回覆。
func TestDirectReceivedOnSendOK(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	e := f.direct(t)
	f.want(t, e.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeActivityEnded, DirectMessage: &sdk.DirectMessage{To: "code [hub]", OK: true}})
	f.want(t, e.ID, sdk.LifecycleReceived)
	f.h.Clock.Advance(time.Hour)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleReceived)
	if len(f.echoes(t)) != 0 {
		t.Fatalf("送到了就不回音：%v", f.echoes(t))
	}
	f.peer(t)
	f.m.mu.Lock()
	from := f.m.entries["code"].turn.From
	f.m.mu.Unlock()
	if from != "work" {
		t.Fatalf("收件方這一輪要配到這則：%q", from)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, DirectMessage: &sdk.DirectMessage{To: "work", Text: "收到"}})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityEnded, DirectMessage: &sdk.DirectMessage{To: "work", OK: true}})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.want(t, e.ID, sdk.LifecycleReplied)
	noAlarm(t, alarms)
}

// 舊版存下的「卡住」（stuck，核心猜的）載入時當待命，等引擎的下一個回報。
func TestStuckStateLoadsAsIdle(t *testing.T) {
	f := dispatchSetup(t)
	must(t, f.m.flushDirty())
	path := filepath.Join(f.h.Dir, "runtime", "sessions", "code.md")
	data, err := os.ReadFile(path)
	must(t, err)
	old := regexp.MustCompile(`(?m)^state: *"?idle"?$`)
	if !old.Match(data) {
		t.Fatalf("狀態檔格式變了：%s", data)
	}
	must(t, platform.AtomicWrite(path, old.ReplaceAll(data, []byte("state: stuck")), 0600))
	f.m.mu.Lock()
	err = f.m.restore()
	f.m.mu.Unlock()
	must(t, err)
	if st := f.sessionState(t, "code"); st != sdk.SessionStateIdle {
		t.Fatalf("stuck 載入要當待命：%s", st)
	}
}

// A 壓縮中：引擎一直說貼不進去，派送器照退避一直試（不放棄、不標紅字）；其實那句排進了輸入框，
// 壓縮完引擎回報開始（帶原文）就記收到，結束記已回覆，泡泡清單乾淨。
func TestReceiptCompactingPasteFailsThenEngineTakesIt(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("hello are you still there ?"))
	f.pasteFails(t, e)
	if red := f.redMessages(t, "code"); len(red) != 0 {
		t.Fatalf("還在試，不標紅字：%v", red)
	}
	f.h.Clock.Advance(40 * time.Second) // 壓縮完
	f.typed(t, e.Body)
	f.want(t, e.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "在"})
	f.want(t, e.ID, sdk.LifecycleReplied)
	if msgs := f.messagesOf(t, "code"); len(msgs) != 0 {
		t.Fatalf("回完的話不再掛在泡泡清單：%+v", msgs)
	}
}

// B 卡在選擇題時打純文字（DEV-56 ③）：畫面原因是「她在等你回答上面的問題」，不發提醒、話留在排隊。
func TestReceiptPlainTextWhilePendingShowsAnswerNotIncident(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	f.m.mu.Lock()
	f.m.entries["code"].session.Pending = &sdk.Pending{ID: "p"}
	f.m.mu.Unlock()
	e := f.send(t, ownerChat("可以"))
	for i := 0; i < 6; i++ {
		f.tick()
		f.h.Clock.Advance(time.Second)
	}
	if got := f.waitOf(t, "code", e.ID); got != sdk.MessageWaitAnswer {
		t.Fatalf("原因要是等你回答：%q", got)
	}
	noAlarm(t, alarms)
	f.want(t, e.ID, sdk.LifecycleQueued)
}

// C 額度用完再恢復（DEV-56 ④）：收到→退回排隊→恢復後重送再收到；之後平台只說閒著（沒有 Stop）就當這一輪結束，
// 不判狀態掉了、不標紅字、不發提醒。
func TestReceiptRateLimitedResendIsNotLostState(t *testing.T) {
	f, first, until := limitedSetup(t)
	alarms := alarmEvents(t, f)
	f.h.Clock.Advance(until.Sub(f.h.Clock.Now()))
	f.tick()
	f.tick()
	f.want(t, first.ID, sdk.LifecycleNotified)
	f.typed(t, first.Body)
	f.want(t, first.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
	f.want(t, first.ID, sdk.LifecycleReplied)
	noAlarm(t, alarms)
	if red := f.redMessages(t, "code"); len(red) != 0 {
		t.Fatalf("重送成功之後不該有紅字：%v", red)
	}
}

// D serve 重啟當下有話在排隊（DEV-56 ①）：核心還沒把 session 載回來的空窗，不發提醒；載回來照送、照常收到與回覆。
func TestReceiptRestartGapIsNotNotDelivered(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	e := f.send(t, ownerChat("發版好了嗎"))
	f.h.Clock.Advance(10 * time.Second) // serve 停著的這段時間
	f.m.mu.Lock()
	delete(f.m.entries, "code") // 重開後 session 還沒載回來
	f.m.mu.Unlock()
	f.tick()
	noAlarm(t, alarms)
	f.open(t, "code", "code", sdk.SessionKindResident) // 載回來、引擎就緒
	f.tick()
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	f.typed(t, e.Body)
	f.want(t, e.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好了"})
	f.want(t, e.ID, sdk.LifecycleReplied)
	noAlarm(t, alarms)
}

// E 連送兩句、第一句貼上一直失敗：引擎照順序收進去；每一句對到自己的封包，不互相頂替。
func TestReceiptTwoMessagesFirstPasteFailsKeepsOrder(t *testing.T) {
	f := dispatchSetup(t)
	first := f.send(t, ownerChat("hello are you still there ?"))
	f.pasteFails(t, first)
	f.h.Clock.Advance(time.Second)
	second := f.send(t, ownerChat("應該是Purpose對吧"))
	var order []string
	for _, m := range f.messagesOf(t, "code") {
		order = append(order, m.ID)
	}
	if len(order) != 2 || order[0] != first.ID || order[1] != second.ID {
		t.Fatalf("泡泡照送出時間排：%v want [%s %s]", order, first.ID, second.ID)
	}
	f.typed(t, first.Body) // 其實排在輸入框的第一句
	f.want(t, first.ID, sdk.LifecycleReceived)
	if lc := f.get(t, second.ID).Lifecycle; lc != sdk.LifecycleQueued && lc != sdk.LifecycleNotified {
		t.Fatalf("第二句還沒被引擎收進去，不能被第一句的事件頂替：%s", lc)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "在"})
	f.want(t, first.ID, sdk.LifecycleReplied)
	f.e.Configure(fake.Behavior{Delivered: true})
	f.h.Clock.Advance(PasteRetryMax)
	f.tick()
	f.want(t, second.ID, sdk.LifecycleNotified)
	f.typed(t, second.Body)
	f.want(t, second.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "對"})
	f.want(t, second.ID, sdk.LifecycleReplied)
	if red := f.redMessages(t, "code"); len(red) != 0 {
		t.Fatalf("兩句都回完，不該有紅字：%v", red)
	}
}

// F 原文比對：對不上的輸入不能亂認；只剩一則已貼上時照舊收（輸入框會改寫內容）；空白差異要容忍。
func TestReceiptMatchingByBody(t *testing.T) {
	t.Run("對不上原文的輸入是主人在終端機打的，不認領貼不進去的話", func(t *testing.T) {
		f := dispatchSetup(t)
		e := f.send(t, ownerChat("hello are you still there ?"))
		f.pasteFails(t, e)
		f.typed(t, "ls -la")
		f.want(t, e.ID, sdk.LifecycleQueued)
	})
	t.Run("只剩一則已貼上時，原文不同照舊當成它", func(t *testing.T) {
		f := dispatchSetup(t)
		e := f.send(t, ownerChat("看一下 [Image #1]"))
		f.tick()
		f.want(t, e.ID, sdk.LifecycleNotified)
		f.typed(t, "看一下")
		f.want(t, e.ID, sdk.LifecycleReceived)
	})
	t.Run("空白與換行的差異不影響對應", func(t *testing.T) {
		f := dispatchSetup(t)
		e := f.send(t, ownerChat("hello  are you\nstill there ?"))
		f.pasteFails(t, e)
		f.typed(t, "hello are you still there ?")
		f.want(t, e.ID, sdk.LifecycleReceived)
	})
}

// 主人叫她停下（SES-62）：引擎回報帶 interrupted 的閒著，這一輪的話記中斷「你叫她停下了」，
// 不記成回覆（別的秘書傳來的話不會收到她說到一半的那句）。
func TestInterruptedIdleHintInterruptsTurn(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{To: "code", Route: sdk.EnvelopeRouteCore, Kind: sdk.EnvelopeKindChat, Body: "幫我查一下"})
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: "Bash"})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint, Interrupted: true})
	got := f.get(t, e.ID)
	if got.Lifecycle != sdk.LifecycleInterrupted || got.Reason != stoppedReason {
		t.Fatalf("記中斷、原因寫你叫她停下了：%+v", got)
	}
	if st := f.sessionState(t, "code"); st != sdk.SessionStateIdle {
		t.Fatalf("停下之後回到待命：%s", st)
	}
}

// 停下之後她又開始了一輪（例：輸入框裡排著的話接著送進去），比那一輪早的閒著提示不算數（SES-62）。
func TestStaleInterruptedIdleHintIgnored(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("整理一下信件"))
	pressed := f.h.Clock.Now()
	f.h.Clock.Advance(time.Second)
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint, Interrupted: true, At: pressed})
	if got := f.get(t, e.ID); got.Lifecycle == sdk.LifecycleInterrupted || got.Lifecycle == sdk.LifecycleReplied {
		t.Fatalf("新的這一輪不能被收掉：%+v", got)
	}
	if st := f.sessionState(t, "code"); st != sdk.SessionStateBusy {
		t.Fatalf("她還在忙：%s", st)
	}
}
