package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// stuckEvents 訂閱「話送不進去／通了」兩種事實事件。
func stuckEvents(t *testing.T, f *dispatchFixture) <-chan *sdk.BusSubscribeResponse {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	out := make(chan *sdk.BusSubscribeResponse, 64)
	for _, topic := range []string{platform.TopicSessionStuck, platform.TopicSessionUnstuck} {
		ch, err := f.b.Subscribe(ctx, topic)
		must(t, err)
		go func() {
			for ev := range ch {
				if reset, _ := ev.Payload["reset"].(bool); reset {
					continue // 啟動後第一輪的那一則（TestFirstPassAfterStartTellsWhoIsStillStuck 另外看）
				}
				out <- ev
			}
		}()
	}
	return out
}

func nextStuckEvent(t *testing.T, ch <-chan *sdk.BusSubscribeResponse, topic string) map[string]any {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Topic != topic {
			t.Fatalf("事件是 %s，要 %s：%+v", ev.Topic, topic, ev.Payload)
		}
		return ev.Payload
	case <-time.After(2 * time.Second):
		t.Fatalf("沒等到 %s", topic)
	}
	return nil
}

func noStuckEvent(t *testing.T, ch <-chan *sdk.BusSubscribeResponse) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("不該發事件：%s %+v", ev.Topic, ev.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}

// notStuck：現在沒有對話算卡住（同步查派送器剛算完的結果），也沒有發過事件。
func notStuck(t *testing.T, f *dispatchFixture, ch <-chan *sdk.BusSubscribeResponse) {
	t.Helper()
	if got := f.m.Stuck(context.Background()); len(got) != 0 {
		t.Fatalf("不該算卡住：%+v", got)
	}
	noStuckEvent(t, ch)
}

// tickEvery 每 step 推一次時鐘、跑一輪派送，共 total。
func (f *dispatchFixture) tickEvery(step, total time.Duration) {
	for d := time.Duration(0); d < total; d += step {
		f.h.Clock.Advance(step)
		f.tick()
	}
}

// 她待命著，給她的話卻一直送不進去（這裡是引擎每次都說畫面開著確認框、核心卻不知道有題目在等）：
// 滿 10 分鐘發一次事實事件（哪位秘書、幾則、多久），同一次卡住不重複發；健檢查得到。通了發「通了」；之後再卡才再發。
func TestUndeliverableQueueReportsStuckOnceUntilItClears(t *testing.T) {
	f := dispatchSetup(t)
	events := stuckEvents(t, f)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "dialog"})
	first := f.send(t, ownerChat("第一句"))
	second := f.send(t, ownerChat("第二句"))
	f.tickEvery(time.Minute, StuckAfter-time.Minute)
	notStuck(t, f, events) // 還沒到門檻
	f.tickEvery(time.Minute, time.Minute)
	p := nextStuckEvent(t, events, platform.TopicSessionStuck)
	if p["name"] != "code" || p["agent"] != "code" || p["agent_name"] != "Person" || p["count"] != 2 || p["minutes"] != 10 {
		t.Fatalf("事件要帶哪位秘書、幾則、多久：%+v", p)
	}
	f.tickEvery(time.Minute, 20*time.Minute)
	noStuckEvent(t, events) // 同一次卡住只發一次
	got := f.m.Stuck(context.Background())
	if len(got) != 1 || got[0].Name != "code" || got[0].AgentName != "Person" || got[0].Count != 2 || f.h.Clock.Now().Sub(got[0].Since) < 30*time.Minute {
		t.Fatalf("健檢要查得到現在卡住的：%+v", got)
	}
	f.want(t, first.ID, sdk.LifecycleQueued) // 排著的話不動：不記失敗、不丟
	f.want(t, second.ID, sdk.LifecycleQueued)

	// 通了：第一句送進去、她開始做。
	f.e.Configure(fake.Behavior{Delivered: true})
	f.h.Clock.Advance(5 * time.Second)
	f.untilState(t, first.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: first.Body})
	f.tick()
	p = nextStuckEvent(t, events, platform.TopicSessionUnstuck)
	if p["name"] != "code" || p["agent"] != "code" || p["agent_stuck"] != false {
		t.Fatalf("通了要說是哪位秘書：%+v", p)
	}
	if got := f.m.Stuck(context.Background()); len(got) != 0 {
		t.Fatalf("通了就不在清單上：%+v", got)
	}

	// 之後又卡住：再發一次。
	f.untilState(t, second.ID, sdk.LifecycleNotified) // 第二句在她忙的時候送進去了（中途補話）
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: second.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好"})
	f.want(t, first.ID, sdk.LifecycleReplied)
	f.want(t, second.ID, sdk.LifecycleReplied)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "dialog"})
	third := f.send(t, ownerChat("第三句"))
	f.tickEvery(time.Minute, StuckAfter)
	p = nextStuckEvent(t, events, platform.TopicSessionStuck)
	if p["count"] != 1 {
		t.Fatalf("再卡住時只有第三句：%+v", p)
	}
	f.want(t, third.ID, sdk.LifecycleQueued)
}

// 她在忙、額度用完還沒恢復、在等主人回答：話排再久都不算卡住（各有自己的規則）。
func TestQueueBehindBusyLimitedOrWaitingIsNotStuck(t *testing.T) {
	routine := func() *sdk.Envelope {
		return &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, From: "system", Body: "［排程 code/j］做事"}
	}
	// 在忙
	f := dispatchSetup(t)
	events := stuckEvents(t, f)
	f.begin(t, f.send(t, ownerChat("跑很久的事")))
	f.send(t, routine())
	f.tickEvery(time.Minute, time.Hour)
	// 額度用完
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeRateLimited, Until: f.h.Clock.Now().Add(3 * time.Hour)})
	f.tickEvery(time.Minute, time.Hour)
	notStuck(t, f, events)
	// 等主人回答確認
	f = dispatchSetup(t)
	events = stuckEvents(t, f)
	go func() {
		_, _, _ = f.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "跑指令"})
	}()
	if !eventually(2*time.Second, func() bool { s, _ := f.m.Get(context.Background(), "code"); return s != nil && s.Pending != nil }) {
		t.Fatal("確認沒有掛上")
	}
	f.send(t, routine())
	f.tickEvery(time.Minute, ApprovalTimeout-time.Minute)
	noStuckEvent(t, events)
	if got := f.m.Stuck(context.Background()); len(got) != 0 {
		t.Fatalf("不算卡住：%+v", got)
	}
}

// 貼上之後一直沒有開始、也沒有任何事件，她待命了 10 分鐘：核心先自救——放回排隊重送一次；重送後還是這樣就記沒送到
// （可以重送），不無限循環。自救期間不通知。
func TestPastedButNeverStartedIsRetriedOnceThenFailed(t *testing.T) {
	f := dispatchSetup(t)
	events := stuckEvents(t, f)
	e := f.send(t, ownerChat("在嗎"))
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	n := len(f.deliveries())
	f.h.Clock.Advance(StuckAfter - time.Second)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	f.h.Clock.Advance(time.Second)
	f.tick()
	f.untilState(t, e.ID, sdk.LifecycleNotified)
	if got := f.get(t, e.ID); rescues(got) != 1 || len(f.deliveries()) != n+1 {
		t.Fatalf("要放回排隊重送一次：%d → %d %+v", n, len(f.deliveries()), got.History)
	}
	f.h.Clock.Advance(StuckAfter)
	f.tick()
	if got := f.get(t, e.ID); got.Lifecycle != sdk.LifecycleFailed || got.Reason != notStartedReason {
		t.Fatalf("重送過還是沒開始：記沒送到、可以重送：%s %q", got.Lifecycle, got.Reason)
	}
	notStuck(t, f, events)
}

// 記成收到之後這一輪的結束事件丟了（核心記她待命、那一則還掛著）：10 分鐘後記中斷，排在後面的話送得進去。
func TestOrphanedReceivedMessageIsInterruptedSoQueueMoves(t *testing.T) {
	f := dispatchSetup(t)
	events := stuckEvents(t, f)
	e := f.send(t, ownerChat("跑測試"))
	f.begin(t, e)
	f.want(t, e.ID, sdk.LifecycleReceived)
	f.m.mu.Lock() // 她其實已經停下來了，但結束事件沒到派送器
	f.m.setState(f.m.entries["code"], sdk.SessionStateIdle, f.h.Clock.Now())
	f.m.mu.Unlock()
	second := f.send(t, ownerChat("順便看 CI"))
	f.h.Clock.Advance(StuckAfter - time.Second)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleReceived)
	f.want(t, second.ID, sdk.LifecycleQueued)
	f.h.Clock.Advance(time.Second)
	f.tick()
	if got := f.get(t, e.ID); got.Lifecycle != sdk.LifecycleInterrupted || got.Reason != stuckOrphanReason {
		t.Fatalf("掛著的那一則要記中斷並寫原因：%s %q", got.Lifecycle, got.Reason)
	}
	f.untilState(t, second.ID, sdk.LifecycleNotified)
	notStuck(t, f, events)
}

// 重開她的對話（看板的「重開對話」、sessions restart）要救得回來：舊對話掛著的那一則記「對話結束了」，
// 排在後面的話在新對話開好後送進去。
func TestRestartingConversationSettlesHangingMessages(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	second := f.send(t, ownerChat("在嗎"))
	must(t, f.m.Rotate(context.Background(), "code"))
	f.m.drainEvents(context.Background())
	if got := f.get(t, nightly.ID); got.Lifecycle != sdk.LifecycleInterrupted || got.Reason != endedReason {
		t.Fatalf("舊對話掛著的那一則要收掉：%s %q", got.Lifecycle, got.Reason)
	}
	f.untilState(t, second.ID, sdk.LifecycleNotified)
}

// 通知前先自救一次：核心剛放掉掛著的那一則（這裡是 adopted 到期重送），從那時起再滿 10 分鐘還是送不進去才報卡住；
// 報出去的「等了多久」仍從最早那則開始等算。
func TestStuckIsReportedOnlyAfterSelfRescueHadItsChance(t *testing.T) {
	f, nightly := adoptedAfterLimit(t)
	events := stuckEvents(t, f)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "dialog"}) // 重送時貼不進去了
	f.tickEvery(time.Minute, AdoptSettleAfter)                       // 到期：作廢、退回排隊（自救）
	if got := f.get(t, nightly.ID); rescues(got) != 1 {
		t.Fatalf("要先作廢重送：%+v", got.History)
	}
	f.tickEvery(time.Minute, StuckAfter-time.Minute)
	notStuck(t, f, events) // 她已經待命超過 10 分鐘，但自救之後還沒滿
	f.tickEvery(time.Minute, time.Minute)
	p := nextStuckEvent(t, events, platform.TopicSessionStuck)
	if p["count"] != 1 || p["minutes"] != 13 {
		t.Fatalf("自救後還是不通才報，等了多久從頭算：%+v", p)
	}
}

// 對話死了（每一句都貼得進去、卻從來不開始）而且排著好幾句：核心一則一則放掉重送的期間不能先報、收回、然後就不再報。
// 從第一次自救起再滿 10 分鐘還有話排著就報一次，之後自救不把它收回；排著的話都記成沒送到之後才算結束。
func TestDeadConversationWithQueueIsReportedOnceAndNotWithdrawn(t *testing.T) {
	f := dispatchSetup(t)
	events := stuckEvents(t, f)
	first := f.send(t, ownerChat("第一句"))
	f.tick()
	f.want(t, first.ID, sdk.LifecycleNotified)
	for _, body := range []string{"第二句", "第三句", "第四句"} {
		f.send(t, ownerChat(body))
	}
	f.tickEvery(time.Minute, 2*StuckAfter-time.Minute)
	notStuck(t, f, events) // 第 10 分鐘先自救（放掉第一句重送），從那時起還沒滿 10 分鐘
	f.tickEvery(time.Minute, time.Minute)
	p := nextStuckEvent(t, events, platform.TopicSessionStuck)
	if p["name"] != "code" || p["minutes"] != 20 {
		t.Fatalf("自救過還是不通要報，等了多久從第一句算：%+v", p)
	}
	f.tickEvery(time.Minute, 25*time.Minute) // 核心繼續一則一則放掉：不收回、不重報
	noStuckEvent(t, events)
	if got := f.m.Stuck(context.Background()); len(got) != 1 || got[0].Minutes != 45 {
		t.Fatalf("還有話排著就一直算卡住，等候時間不重新算：%+v", got)
	}
	if got := f.get(t, first.ID); got.Lifecycle != sdk.LifecycleFailed {
		t.Fatalf("第一句重送過還是沒開始，要記沒送到：%s", got.Lifecycle)
	}
}

// 卡住期間主人直接在她的畫面打了一輪（她忙一下又待命）：排著的話還是沒進去，不算通了、不收回提醒，
// 也不在 10 分鐘後當成新的一次再報；等候時間照原本的算。
func TestOwnerTypingWhileStuckNeitherClearsNorRepeatsTheReport(t *testing.T) {
	f := dispatchSetup(t)
	events := stuckEvents(t, f)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "dialog"})
	f.send(t, ownerChat("第一句"))
	f.send(t, ownerChat("第二句"))
	f.tickEvery(time.Minute, StuckAfter)
	if p := nextStuckEvent(t, events, platform.TopicSessionStuck); p["dialog"] != true {
		t.Fatalf("引擎說畫面開著確認框，事件要帶著：%+v", p)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "你還好嗎"})
	f.tickEvery(time.Minute, 3*time.Minute)
	if got := f.m.Stuck(context.Background()); len(got) != 1 || got[0].Count != 2 {
		t.Fatalf("她在忙的這一段，排著的話還是沒進去：%+v", got)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "還好"})
	f.tickEvery(time.Minute, 2*StuckAfter)
	noStuckEvent(t, events)
	if got := f.m.Stuck(context.Background()); len(got) != 1 || got[0].Minutes != 33 || !got[0].Dialog {
		t.Fatalf("同一次卡住：等候時間照原本的算：%+v", got)
	}
}

// 核心重開後上一次報過誰已經不記得：第一輪說一次現在卡著的是哪幾位秘書（沒有就是空的），訂閱者把其他人的舊提醒收掉。
func TestFirstPassAfterStartTellsWhoIsStillStuck(t *testing.T) {
	f := dispatchSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := f.b.Subscribe(ctx, platform.TopicSessionUnstuck)
	must(t, err)
	f.m.stuckMu.Lock()
	f.m.stuckSwept = false // 重新啟動
	f.m.stuckMu.Unlock()
	f.tick()
	select {
	case ev := <-ch:
		agents, _ := ev.Payload["agents"].([]string)
		if ev.Payload["reset"] != true || len(agents) != 0 {
			t.Fatalf("第一輪要說現在卡著的名單：%+v", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("啟動後第一輪沒有說現在卡著的名單")
	}
	f.tick()
	select {
	case ev := <-ch:
		t.Fatalf("只有第一輪說：%+v", ev.Payload)
	case <-time.After(100 * time.Millisecond):
	}
}
