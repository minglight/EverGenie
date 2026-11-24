package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

func ownerChat(body string) *sdk.Envelope {
	return &sdk.Envelope{From: "owner(web)", Kind: sdk.EnvelopeKindChat, Body: body}
}

// 電腦重開後核心還記得「待命」：貼上時引擎說不在 → 記成停了、訊息放回排隊（不算失敗）、馬上把她開回來、閒著後再送。
// Guards: REQ-300
func TestGoneMarksStoppedReopensThenDelivers(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "gone"})
	e := f.send(t, ownerChat("我現在要做卷腹這台喔"))
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	if st, _ := f.m.State("code"); st != sdk.SessionStateStopped {
		t.Fatalf("引擎說不在要記成停了：%s", st)
	}
	starts := func() int {
		n := 0
		for _, c := range f.e.Calls() {
			if c.Method == "Start" && c.Request.(*sdk.EngineStartRequest).Name == "code" {
				n++
			}
		}
		return n
	}
	before := starts()
	f.e.Configure(fake.Behavior{Delivered: true})
	f.tick()
	if starts() != before+1 {
		t.Fatalf("常駐秘書有話要給她要馬上開回來（Start %d→%d）", before, starts())
	}
	f.m.drainEvents(context.Background())
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	if got := f.get(t, e.ID); got.Attempts > 2 {
		t.Fatalf("不在的那次不能把次數用光：%+v", got)
	}
}

// 開不起來時不要每秒重試。
// Guards: REQ-300
func TestReopenThrottled(t *testing.T) {
	f := dispatchSetup(t)
	must(t, f.m.Close(context.Background(), "code"))
	f.send(t, ownerChat("在嗎"))
	count := func() int {
		n := 0
		for _, c := range f.e.Calls() {
			if c.Method == "Start" && c.Request.(*sdk.EngineStartRequest).Name == "code" {
				n++
			}
		}
		return n
	}
	f.e.Configure(fake.Behavior{Delivered: true})
	f.tick()
	first := count()
	must(t, f.m.Close(context.Background(), "code"))
	f.h.Clock.Advance(time.Second)
	f.tick()
	if count() != first {
		t.Fatalf("30 秒內不重開：%d→%d", first, count())
	}
	f.h.Clock.Advance(MainReopenEvery)
	f.tick()
	if count() != first+1 {
		t.Fatalf("過了 30 秒要再試：%d→%d", first, count())
	}
}

// 主人的話在秘書忙時也送進去並記「中途補話」；別人的話照舊等閒著；一次只貼一則。
// Guards: REQ-301
func TestOwnerInterjectsWhileBusy(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "手上的事"})
	say := f.send(t, &sdk.Envelope{})
	a := f.send(t, ownerChat("這台要注意什麼"))
	b := f.send(t, ownerChat("還有一件"))
	f.tick()
	f.want(t, a.ID, sdk.LifecycleNotified)
	f.want(t, say.ID, sdk.LifecycleQueued)
	f.want(t, b.ID, sdk.LifecycleQueued)
	h := f.get(t, a.ID).History
	if h[len(h)-1].Detail != interjectReason {
		t.Fatalf("要記中途補話：%+v", h)
	}
	// 有確認框時不中途補話（Enter 會被當成允許，D-5）。
	f2 := dispatchSetup(t)
	f2.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped})
	go func() {
		_, _, _ = f2.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Summary: "跑指令"})
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s, _ := f2.m.Get(context.Background(), "code")
		if s.Pending != nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	c := f2.send(t, ownerChat("補一句"))
	f2.tick()
	f2.want(t, c.ID, sdk.LifecycleQueued)
}

// 中途補話在同一輪裡被收進去：兩則都跟著那一輪結束記成已回覆。
// Guards: REQ-301
func TestInterjectedTurnRepliesBoth(t *testing.T) {
	f := dispatchSetup(t)
	first := f.send(t, ownerChat("我現在要做卷腹這台喔"))
	f.begin(t, first)
	f.want(t, first.ID, sdk.LifecycleReceived)
	second := f.send(t, ownerChat("這台要注意什麼"))
	f.tick()
	f.want(t, second.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: second.Body})
	f.want(t, second.ID, sdk.LifecycleReceived)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "兩件都回了"})
	f.want(t, first.ID, sdk.LifecycleReplied)
	f.want(t, second.ID, sdk.LifecycleReplied)
}

// 剛閒下來的幾秒不重貼「已送出還沒收到」的。
// Guards: REQ-301
func TestNoRepasteRightAfterIdle(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "手上的事"})
	a := f.send(t, ownerChat("中途送進去的"))
	f.tick()
	f.want(t, a.ID, sdk.LifecycleNotified)
	f.h.Clock.Advance(2 * time.Minute)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.tick()
	f.want(t, a.ID, sdk.LifecycleNotified)
	if n := len(f.deliveries()); n != 1 {
		t.Fatalf("剛閒下來不重貼：%d", n)
	}
}

// Session.messages：還沒結束的、主人送失敗沒重送的；重送過或回覆過的不列。
// Guards: REQ-302
func TestWithMessages(t *testing.T) {
	f := dispatchSetup(t)
	ctx := context.Background()
	failed := f.send(t, ownerChat("沒送到的"))
	f.tick()
	f.h.Clock.Advance(time.Second)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint}) // 貼上了她卻閒著：這句沒開始
	f.want(t, failed.ID, sdk.LifecycleFailed)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "手上的事"})
	queued := f.send(t, &sdk.Envelope{Body: "排隊中的傳話"})
	s, err := f.m.Get(ctx, "code")
	must(t, err)
	f.m.WithMessages(ctx, s)
	ids := map[string]sdk.SessionMessage{}
	for _, m := range s.Messages {
		ids[m.ID] = m
	}
	if m, ok := ids[failed.ID]; !ok || m.Lifecycle != sdk.LifecycleFailed || m.Reason == "" {
		t.Fatalf("送失敗的要列、帶白話原因：%+v", s.Messages)
	}
	if _, ok := ids[queued.ID]; !ok {
		t.Fatalf("排隊中的要列：%+v", s.Messages)
	}
	f.send(t, ownerChat("沒送到的"))
	s, err = f.m.Get(ctx, "code")
	must(t, err)
	f.m.WithMessages(ctx, s)
	for _, m := range s.Messages {
		if m.ID == failed.ID {
			t.Fatalf("重送過的失敗不再列：%+v", s.Messages)
		}
	}
	f.h.Clock.Advance(FailedShownFor + time.Minute)
	s, _ = f.m.Get(ctx, "work")
	f.m.WithMessages(ctx, s)
	if len(s.Messages) != 0 {
		t.Fatalf("別的 session 的不列：%+v", s.Messages)
	}
}

// Guards: REQ-318

// 2026-11-03 以前的封包歷程記的是舊詞：讀的時候照樣當成主人在她忙時送到的。
func TestInterjectedReadsLegacyDetail(t *testing.T) {
	for _, d := range []string{interjectReason, legacyInterjectReason} {
		e := &sdk.Envelope{History: []sdk.LifecycleStep{{State: sdk.LifecycleNotified, Detail: d}}}
		if !interjected(e) {
			t.Fatalf("認不得 %q", d)
		}
	}
}
