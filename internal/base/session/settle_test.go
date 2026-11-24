package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// 一輪被打斷（TURN_STARTED interrupted）：這一輪的話和中途補話帶著的上一句都記中斷，不會等下一次正常結束被算成已回覆。
func TestInterruptedTurnInterruptsCarried(t *testing.T) {
	f := dispatchSetup(t)
	first := f.send(t, ownerChat("先整理信件"))
	f.begin(t, first)
	second := f.send(t, ownerChat("順便看一下行事曆"))
	f.tick() // 主人的話在她忙時也送（中途補話）
	f.want(t, second.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: second.Body})
	f.want(t, second.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Interrupted: true, Input: "停"})
	f.want(t, first.ID, sdk.LifecycleInterrupted)
	f.want(t, second.ID, sdk.LifecycleInterrupted)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好"})
	f.want(t, first.ID, sdk.LifecycleInterrupted)
}

// 對話結束（SESSION_ENDED）：收到、處理中的記中斷「她的對話結束了」。
func TestSessionEndedInterruptsInFlight(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("跑測試"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, Activity: "Bash"})
	f.want(t, e.ID, sdk.LifecycleWorking)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeSessionEnded, Reason: "stopped"})
	if got := f.get(t, e.ID); got.Lifecycle != sdk.LifecycleInterrupted || got.Reason != endedReason {
		t.Fatalf("記中斷、附原因：%+v", got)
	}
}

// SES-64：帳號登出讓對話結束時，話上的原因寫明要重新登入（不然主人只看到「她的對話結束了」，不知道要做什麼）。
func TestSessionEndedByLogoutSaysLogInAgain(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("跑測試"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeSessionEnded, Reason: sdk.SessionEndReasonLoggedOut})
	if got := f.get(t, e.ID); got.Lifecycle != sdk.LifecycleInterrupted || got.Reason != loggedOutReason {
		t.Fatalf("記中斷、寫明要重新登入：%+v", got)
	}
}

// 中斷的主人訊息列在泡泡上，主人對這個對話再說一句就不列了（她看到新的話，舊的就不用再提醒）。
func TestInterruptedListedUntilOwnerSpeaksAgain(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("整理一下信件"))
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnFailed, Error: sdk.EngineErrorKindOverloaded})
	if msgs := f.messagesOf(t, "code"); len(msgs) != 1 || msgs[0].ID != e.ID {
		t.Fatalf("中斷的要列出來：%+v", msgs)
	}
	f.h.Clock.Advance(time.Second)
	next := f.send(t, ownerChat("再試一次"))
	f.begin(t, next)
	for _, m := range f.messagesOf(t, "code") {
		if m.ID == e.ID {
			t.Fatalf("主人又說話了，中斷的那則不再列：%+v", m)
		}
	}
}

// 同一個對話較早的那句還在退避重試：後面的先不送，不搶先。
func TestLaterMessageWaitsForEarlierRetry(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "paste"})
	first := f.send(t, ownerChat("第一句"))
	f.tick()
	f.want(t, first.ID, sdk.LifecycleQueued)
	second := f.send(t, ownerChat("第二句"))
	f.tick()
	for _, d := range f.deliveries() {
		if d.EnvelopeID == second.ID {
			t.Fatalf("第一句還在退避，第二句不該先貼：%+v", f.deliveries())
		}
	}
	f.e.Configure(fake.Behavior{Delivered: true})
	f.h.Clock.Advance(time.Second)
	f.tick()
	f.want(t, first.ID, sdk.LifecycleNotified)
	f.want(t, second.ID, sdk.LifecycleQueued)
}

// 秘書傳的話靠閒著的提示收尾、又拿不到她最後一句：記已回覆，但不貼一則空白的回覆給寄件的秘書。
func TestIdleHintSayNoEmptyReply(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{Body: "幫我查一下"})
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint})
	f.want(t, e.ID, sdk.LifecycleReplied)
	rows, err := f.b.List(context.Background(), "work")
	must(t, err)
	for _, r := range rows {
		if r.ReplyTo == e.ID {
			t.Fatalf("沒有回覆文字就不自動回：%+v", r)
		}
	}
}
