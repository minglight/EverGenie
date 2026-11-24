package session

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 送出後的排隊原因與重試（REQ-384～387；S5-2 起計時只決定多久再試、畫面寫什麼）。全部用假時鐘、不真的等。

func (f *dispatchFixture) messagesOf(t *testing.T, name string) []sdk.SessionMessage {
	t.Helper()
	s, err := f.m.Get(context.Background(), name)
	must(t, err)
	f.m.WithMessages(context.Background(), s)
	return s.Messages
}

func (f *dispatchFixture) waitOf(t *testing.T, name, id string) sdk.MessageWait {
	t.Helper()
	for _, m := range f.messagesOf(t, name) {
		if m.ID == id {
			return m.Wait
		}
	}
	t.Fatalf("%s 的 messages 裡沒有 %s", name, id)
	return ""
}

func (f *dispatchFixture) startsOf(name string) int {
	n := 0
	for _, c := range f.e.Calls() {
		if c.Method == "Start" && c.Request.(*sdk.EngineStartRequest).Name == name {
			n++
		}
	}
	return n
}

// 對話結束時已貼上、還沒開始的話放回排隊；叫醒後同一個封包 id 再交給引擎，引擎說「已經在她的對話裡」就記收到、不重貼；
// 晚到的開始事件併進這一輪，結束時記已回覆。
func TestRedeliverAlreadyDeliveredAdopted(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
	e := f.send(t, ownerChat("幫我查天氣"))
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeSessionEnded, Reason: "other"})
	f.want(t, e.ID, sdk.LifecycleQueued)
	f.tick() // 叫醒
	f.tick() // 閒著了，同一個 id 再交
	got := f.get(t, e.ID)
	if got.Lifecycle != sdk.LifecycleReceived {
		t.Fatalf("引擎說已經到了要記收到：%+v", got.History)
	}
	if len(f.deliveries()) != 2 {
		t.Fatalf("再交過一次（引擎回 already、不重貼）：%d 次", len(f.deliveries()))
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: e.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "晴天"})
	f.want(t, e.ID, sdk.LifecycleReplied)
}

// 確認框開著：泡泡的原因是「等你回答」，多久都不貼進去、不發提醒。
// Guards: REQ-384
func TestWaitReasonAnswerWhileDialogOpen(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	go func() {
		_, _, _ = f.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "建立檔案"})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if s, _ := f.m.Get(context.Background(), "code"); s != nil && s.Pending != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("等不到確認框")
		}
		time.Sleep(5 * time.Millisecond)
	}
	e := f.send(t, ownerChat("順便看一下信"))
	f.tick()
	f.h.Clock.Advance(10 * time.Second)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	if w := f.waitOf(t, "code", e.ID); w != sdk.MessageWaitAnswer {
		t.Fatalf("確認框開著：原因要是 answer，拿到 %q", w)
	}
	noAlarm(t, alarms)
}

// 引擎還沒準備好（對話在、閒著，卻一直送不進去）：話留在排隊、泡泡不加原因，等多久都不發提醒、不改狀態；引擎好了照送。
func TestEngineNotReadyKeepsQueuedWithoutAlarm(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	ready := false
	inner := f.m.d.Engine.(testRegistry)
	f.m.d.Engine = flakyRegistry{inner: inner, ready: &ready}
	e := f.send(t, ownerChat("哈囉"))
	for i := 0; i < 10; i++ {
		f.tick()
		f.h.Clock.Advance(time.Second)
	}
	if w := f.waitOf(t, "code", e.ID); w != "" {
		t.Fatalf("說不出原因就不寫原因：%q", w)
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
	noAlarm(t, alarms)
	ready = true
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// 引擎說這次沒貼進去（paste）：退避重試中的那句，泡泡的原因是「正在重試」（不是空白、不是紅字），不發提醒；
// 貼進去之後原因就拿掉。
func TestWaitReasonRetryingWhilePasteBacksOff(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "paste"})
	e := f.send(t, ownerChat("幫我訂位"))
	if w := f.waitOf(t, "code", e.ID); w != "" {
		t.Fatalf("還沒試過貼上，不寫重試：%q", w)
	}
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	if w := f.waitOf(t, "code", e.ID); w != sdk.MessageWaitRetrying {
		t.Fatalf("引擎說沒貼進去：原因要是 retrying，拿到 %q", w)
	}
	noAlarm(t, alarms)
	f.e.Configure(fake.Behavior{Delivered: true})
	f.h.Clock.Advance(PasteRetryMax)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	if w := f.waitOf(t, "code", e.ID); w == sdk.MessageWaitRetrying {
		t.Fatal("貼進去之後不再寫重試")
	}
}

// 常駐的對話停了（電腦重開）：主人的話不等 30 秒的節流，馬上開；原因是「正在叫醒她」，不發提醒。
// 秘書之間的話照舊等節流。
// Guards: REQ-386, REQ-384
func TestStoppedMainOpensImmediatelyForOwner(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	ctx := context.Background()
	must(t, f.m.Close(ctx, "code"))
	f.m.mainReopen["code"] = f.h.Clock.Now() // 剛叫過一次（30 秒內）
	before := f.startsOf("code")
	f.h.Clock.Advance(time.Second)
	peer := f.send(t, &sdk.Envelope{})
	f.tick()
	if f.startsOf("code") != before {
		t.Fatal("秘書傳的話照節流，不馬上開")
	}
	if w := f.waitOf(t, "code", peer.ID); w != "" {
		t.Fatalf("秘書傳的話沒有泡泡原因：%q", w)
	}
	f.h.Clock.Advance(time.Second)
	e := f.send(t, ownerChat("醒醒"))
	if w := f.waitOf(t, "code", e.ID); w != sdk.MessageWaitWaking {
		t.Fatalf("停了、會被叫醒：原因是 waking，拿到 %q", w)
	}
	f.h.Clock.Advance(20 * time.Second)
	f.tick()
	if f.startsOf("code") != before+1 {
		t.Fatal("主人的話：不等 30 秒的節流，馬上開")
	}
	noAlarm(t, alarms)
	f.tick() // 假引擎開好就是閒著：接著送（先來的那則先貼，一次一則）
	if len(f.deliveries()) != 1 || f.deliveries()[0].EnvelopeID != peer.ID {
		t.Fatalf("開好就照順序送：%+v", f.deliveries())
	}
}

// 收件的對話在這台不會開起來（從沒開過的專屬對話名稱、沒有這位秘書、停用或 mode none 的秘書——含留著舊主要對話的；
// 例如兩個家拆開前寄給別台的話）：這是核心自己知道的事實，派送器同一輪就記送不了（不等計時），不永遠排著；
// 寄件的秘書收到回音（SES-20），主人的回答不會每 30 秒重送一次。存在的房間（停了也算）、啟用的 resident 秘書（主要對話還沒建）照舊排著。
func TestMissingTargetFailsRightAway(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	ctx := context.Background()
	agentRec := func(id string, enabled bool, mode string) vault.Record {
		return vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": enabled, "mode": mode}}
	}
	f.h.WithRecords(agentRec("idle", false, "resident"), agentRec("quiet", true, "none"), agentRec("fresh", true, "resident"), agentRec("old", true, "resident"))
	f.open(t, "old", "old", sdk.SessionKindResident)
	must(t, f.m.Close(ctx, "old"))
	cur, err := f.h.Vault.Get(ctx, "agent", "old")
	must(t, err)
	cur.Fields["enabled"] = false // 停用了，舊的主要對話還留著（停著）
	_, err = f.h.Vault.Put(ctx, cur, "system", cur.Revision)
	must(t, err)
	room := f.send(t, ownerChatTo("code-dev-59", "可以"))
	gone := f.send(t, &sdk.Envelope{To: "gone", From: "system", Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, Body: "［排程 gone/nightly］"})
	off := f.send(t, &sdk.Envelope{To: "idle", Route: sdk.EnvelopeRouteCore, Body: "停用的秘書"})
	none := f.send(t, &sdk.Envelope{To: "quiet", Route: sdk.EnvelopeRouteCore, Body: "mode none 的秘書"})
	stale := f.send(t, &sdk.Envelope{To: "old", From: "system", Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, Body: "［排程 old/nightly］"})
	answer := f.send(t, &sdk.Envelope{To: "code-dev-60", From: "system", Kind: sdk.EnvelopeKindAnswer, Route: sdk.EnvelopeRouteCore, Notice: "N-SES58", Body: "好"})
	f.open(t, "code-room", "code", sdk.SessionKindRoom)
	must(t, f.m.Close(ctx, "code-room"))
	kept := f.send(t, &sdk.Envelope{To: "code-room", Route: sdk.EnvelopeRouteCore, Body: "等你醒"})
	fresh := f.send(t, &sdk.Envelope{To: "fresh", Route: sdk.EnvelopeRouteCore, Body: "新來的秘書"})
	f.tick() // 時鐘沒動：不是計時，是同一輪看到的事實
	for _, e := range []*sdk.Envelope{room, gone, off, none, stale, answer} {
		got := f.get(t, e.ID)
		if got.Lifecycle != sdk.LifecycleFailed || got.Reason != NoTargetReason {
			t.Fatalf("%s 收件人 %s：要記送不了（%s），拿到 %s %q", e.ID, e.To, NoTargetReason, got.Lifecycle, got.Reason)
		}
	}
	for _, e := range []*sdk.Envelope{kept, fresh} {
		if got := f.get(t, e.ID); got.Lifecycle == sdk.LifecycleFailed {
			t.Fatalf("收件人 %s 會開起來，不記送不了：%+v", e.To, got)
		}
	}
	echoed := 0
	for _, e := range f.echoes(t) {
		if e.To == "work" && strings.Contains(e.Body, NoTargetReason) {
			echoed++
		}
	}
	if echoed != 2 {
		t.Fatalf("寄件的秘書（work）寄給停用、mode none 的兩則，各收到一則回音：%d", echoed)
	}
	f.h.Clock.Advance(time.Minute)
	f.tick()
	rows, err := f.b.List(ctx, "")
	must(t, err)
	answers := 0
	for _, e := range rows {
		if e.Kind == sdk.EnvelopeKindAnswer && e.Notice == "N-SES58" {
			answers++
		}
	}
	if answers != 1 {
		t.Fatalf("回答的對話不會開起來，不重送：%d 則", answers)
	}
	noAlarm(t, alarms)
}

func ownerChatTo(to, body string) *sdk.Envelope {
	e := ownerChat(body)
	e.To = to
	return e
}

// 主人打開聊天頁：停了的主要 session 先叫醒；開著的、不是主要 session、秘書停用的不動。
// Guards: REQ-386
func TestWakeOnChatOpen(t *testing.T) {
	f := dispatchSetup(t)
	ctx := context.Background()
	must(t, f.m.Close(ctx, "code"))
	before := f.startsOf("code")
	s, err := f.m.Open(ctx, &sdk.SessionOpenRequest{Name: "code", Agent: "code", Wake: true})
	must(t, err)
	if f.startsOf("code") != before+1 || s.State == sdk.SessionStateStopped {
		t.Fatalf("停了的主要 session 要叫醒（看板寫「正在叫醒她」）：%+v", s)
	}
	f.m.drainEvents(ctx)
	if _, err = f.m.Open(ctx, &sdk.SessionOpenRequest{Name: "code", Agent: "code", Wake: true}); err != nil || f.startsOf("code") != before+1 {
		t.Fatalf("開著的不重開：%v", err)
	}
	f.open(t, "code-room", "code", sdk.SessionKindRoom)
	must(t, f.m.Close(ctx, "code-room"))
	if s, err = f.m.Open(ctx, &sdk.SessionOpenRequest{Name: "code-room", Agent: "code", Wake: true}); err != nil || s.State != sdk.SessionStateStopped || f.startsOf("code-room") != 1 {
		t.Fatalf("不是主要 session 不叫醒：%v %+v", err, s)
	}
	f.h.WithRecords(vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": true, "mode": "none"}})
	f.open(t, "life", "life", sdk.SessionKindResident)
	must(t, f.m.Close(ctx, "life"))
	if s, err = f.m.Open(ctx, &sdk.SessionOpenRequest{Name: "life", Agent: "life", Wake: true}); err != nil || s.State != sdk.SessionStateStopped || f.startsOf("life") != 1 {
		t.Fatalf("mode none 的秘書不叫醒：%v %+v", err, s)
	}
}

// 工作對話滿了 work_max 在排隊：主人對它說話就直接開，不受名額限制。
// Guards: REQ-386
func TestOwnerOpensQueuedWorkBeyondLimit(t *testing.T) {
	w := workSetup(t, 1, 2)
	w.openAs(t, "SEC-1", "code")
	s := w.openAs(t, "SEC-2", "code")
	if s.QueuedAt.IsZero() {
		t.Fatalf("超過上限要排隊：%+v", s)
	}
	e := w.send(t, &sdk.Envelope{To: "code-sec-2", From: "owner(web)", Kind: sdk.EnvelopeKindChat, Body: "先做這張"})
	if wait := w.waitOf(t, "code-sec-2", e.ID); wait != sdk.MessageWaitWaking {
		t.Fatalf("排隊中、主人說話會開：原因 waking，拿到 %q", wait)
	}
	w.tick()
	if len(w.starts("code-sec-2")) != 1 || !w.sessionOf(t, "code-sec-2").QueuedAt.IsZero() {
		t.Fatalf("主人說話：不看上限直接開：%+v", w.sessionOf(t, "code-sec-2"))
	}
}

// 今天主人的話「送出→收到」：p50／p90 與超過 3 秒（含沒送到）的則數；秘書之間的話不算。
// Guards: REQ-387
func TestReceiveLatency(t *testing.T) {
	f := dispatchSetup(t)
	ctx := context.Background()
	fast := f.send(t, ownerChat("一"))
	f.tick()
	f.h.Clock.Advance(time.Second)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: fast.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	slow := f.send(t, ownerChat("二"))
	f.tick()
	f.h.Clock.Advance(5 * time.Second)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: slow.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.send(t, &sdk.Envelope{}) // 秘書之間的話不算
	lost := f.send(t, &sdk.Envelope{From: "owner(web)", Kind: sdk.EnvelopeKindChat, Body: "三", To: "nobody"})
	f.h.Clock.Advance(4 * time.Second)
	count, over, p50, p90, err := f.m.ReceiveLatency(ctx)
	must(t, err)
	if count != 3 || over != 2 || p50 != time.Second || p90 != 5*time.Second {
		t.Fatalf("count=%d over=%d p50=%v p90=%v（%s 沒收到算超過）", count, over, p50, p90, lost.ID)
	}
}

// 引擎回 dialog（她的畫面開著原生確認框，貼不進去）：不算失敗、不發提醒，原因是「等你回答」，確認框收掉就送。
// Guards: REQ-391, REQ-384
func TestDeliverDialogWaitsForAnswer(t *testing.T) {
	f := dispatchSetup(t)
	alarms := alarmEvents(t, f)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "dialog"})
	e := f.send(t, ownerChat("在嗎"))
	for i := 0; i < 10; i++ {
		f.tick()
		f.h.Clock.Advance(time.Second)
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
	if w := f.waitOf(t, "code", e.ID); w != sdk.MessageWaitAnswer {
		t.Fatalf("確認框開著：原因是 answer，拿到 %q", w)
	}
	if h := f.get(t, e.ID).History; h[len(h)-1].Detail != "她的畫面開著確認框，等你回答後再送" {
		t.Fatalf("原因要寫進歷程：%+v", h)
	}
	f.tick() // 迴圈最後撥過 1 秒，這輪可以再試一次
	n := len(f.deliveries())
	f.tick()
	f.tick()
	if len(f.deliveries()) != n || n > 11 {
		t.Fatalf("確認框開著時 1 秒最多試一次，不是每輪都試：%d → %d", n, len(f.deliveries()))
	}
	noAlarm(t, alarms)
	f.e.Configure(fake.Behavior{Delivered: true}) // 確認框收掉了
	f.h.Clock.Advance(DialogRetryEvery)           // 1 秒內再試就送出
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// 暫時的錯誤（paste／capture／enter）：退避重試，不會馬上重貼；試多久都留在排隊、不記失敗、不發提醒；每次的原因寫進歷程。
func TestDeliverTransientErrorsBackOff(t *testing.T) {
	for _, reason := range []string{"paste", "capture", "enter"} {
		t.Run(reason, func(t *testing.T) {
			f := dispatchSetup(t)
			alarms := alarmEvents(t, f)
			f.e.Configure(fake.Behavior{Delivered: false, Reason: reason})
			e := f.send(t, ownerChat("在嗎"))
			f.tick()
			f.tick() // 同一刻再跑一輪：還在退避，不重貼
			if n := len(f.deliveries()); n != 1 {
				t.Fatalf("退避中不能馬上重貼：%d 次", n)
			}
			for i := 0; i < 20; i++ {
				f.h.Clock.Advance(500 * time.Millisecond)
				f.tick()
				f.want(t, e.ID, sdk.LifecycleQueued)
			}
			got := f.get(t, e.ID)
			found := false
			for _, h := range got.History {
				found = found || strings.Contains(h.Detail, "（"+reason+"）")
			}
			if !found || len(f.deliveries()) < 3 {
				t.Fatalf("要退避重試幾次，原因寫進歷程：%d 次 %+v", len(f.deliveries()), got.History)
			}
			if len(f.echoes(t)) != 0 {
				t.Fatalf("還在試，不回音：%v", f.echoes(t))
			}
			noAlarm(t, alarms)
		})
	}
}

// 引擎說 session 不在（gone，舊的假引擎回 stopped）：記成停了、叫醒、閒著後再送，不算失敗。
// Guards: REQ-391, REQ-300
func TestDeliverGoneOrStoppedWakesThenDelivers(t *testing.T) {
	for _, reason := range []string{"gone", "stopped"} {
		t.Run(reason, func(t *testing.T) {
			f := dispatchSetup(t)
			before := f.startsOf("code")
			f.e.Configure(fake.Behavior{Delivered: false, Reason: reason})
			e := f.send(t, ownerChat("醒醒"))
			f.tick()
			f.want(t, e.ID, sdk.LifecycleQueued)
			if st, _ := f.m.State("code"); st != sdk.SessionStateStopped {
				t.Fatalf("要記成停了：%s", st)
			}
			if w := f.waitOf(t, "code", e.ID); w != sdk.MessageWaitWaking {
				t.Fatalf("原因是正在叫醒：%q", w)
			}
			f.e.Configure(fake.Behavior{Delivered: true})
			f.tick() // 叫醒
			f.tick() // 閒著了，送
			if f.startsOf("code") != before+1 {
				t.Fatal("要叫醒她")
			}
			f.want(t, e.ID, sdk.LifecycleNotified)
		})
	}
}

// 秘書停用後，她留下的房間與工作對話（開著或停著）不再收話：寄給它們的話同一輪記送不了，不叫起來、不貼進去（SES-58）。
// 2026-11-07 實際發生過：停用一位秘書後，一則寄給她工作對話的話把對話一直叫起來重試。
func TestDisabledAgentRoomsFail(t *testing.T) {
	f := dispatchSetup(t)
	ctx := context.Background()
	f.h.WithRecords(vault.Record{Kind: "agent", ID: "art", Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": false, "mode": "resident"}})
	f.open(t, "art-room", "art", sdk.SessionKindRoom)
	f.open(t, "art-old", "art", sdk.SessionKindRoom)
	must(t, f.m.Close(ctx, "art-old"))
	before := f.startsOf("art-old")
	idle := f.send(t, ownerChatTo("art-room", "還在嗎"))
	stopped := f.send(t, ownerChatTo("art-old", "醒醒"))
	f.tick()
	for _, e := range []*sdk.Envelope{idle, stopped} {
		if got := f.get(t, e.ID); got.Lifecycle != sdk.LifecycleFailed || got.Reason != NoTargetReason {
			t.Fatalf("%s：停用秘書的對話要記送不了，拿到 %s %q", e.To, got.Lifecycle, got.Reason)
		}
	}
	if len(f.deliveries()) != 0 || f.startsOf("art-old") != before {
		t.Fatalf("不貼進去、不叫起來：%d 則，開了 %d 次", len(f.deliveries()), f.startsOf("art-old")-before)
	}
}

// 貼進去的文字拿掉看不見的控制字元（換行、tab 留著）：夾帶 ESC 的話貼上後畫面上看不到，引擎永遠對不上（SES-68）。
func TestPastedTextDropsControlChars(t *testing.T) {
	if got := printable("\x1b我讓開發家\r一起\t處理\x07\u0085\r\n好😊"); got != "我讓開發家\n一起\t處理\n好😊" {
		t.Fatalf("拿到 %q", got)
	}
}

// 整句只有控制字元：清完沒東西可貼，同一輪記送不了，不貼、不無限重試（SES-68）。
func TestBlankAfterCleanFails(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, ownerChat("\x1b\x07"))
	f.tick()
	if got := f.get(t, e.ID); got.Lifecycle != sdk.LifecycleFailed || got.Reason != BlankBodyReason {
		t.Fatalf("要記送不了（%s），拿到 %s %q", BlankBodyReason, got.Lifecycle, got.Reason)
	}
	if len(f.deliveries()) != 0 {
		t.Fatalf("不貼：%d 則", len(f.deliveries()))
	}
}

// 同一則話一直貼不進去（2026-11-14：重試上千次、log 每 12 秒一行）：log 只記前幾次，之後每 PasteLogEvery 次一行；
// 重試照舊（不因為少記就少試），貼進去時補一行收尾。
func TestPasteRetryLogIsThrottled(t *testing.T) {
	f := dispatchSetup(t)
	var logs bytes.Buffer
	f.m.log = slog.New(slog.NewTextHandler(&logs, nil))
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "paste"})
	e := f.send(t, ownerChat("在嗎"))
	const tries = 2*PasteLogEvery + 5
	for len(f.deliveries()) < tries {
		f.h.Clock.Advance(PasteRetryMax)
		f.tick()
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
	if got, want := strings.Count(logs.String(), "貼上沒成功"), PasteLogFirst+2; got != want {
		t.Fatalf("重試 %d 次，log 要 %d 行（前 %d 次＋每 %d 次一行），得到 %d 行", tries, want, PasteLogFirst, PasteLogEvery, got)
	}
	if !strings.Contains(logs.String(), "try=60") {
		t.Fatalf("降頻後那一行要帶累計次數：%s", logs.String())
	}
	f.e.Configure(fake.Behavior{Delivered: true})
	f.h.Clock.Advance(PasteRetryMax)
	f.tick()
	if strings.Count(logs.String(), "貼上恢復了") != 1 {
		t.Fatalf("貼進去了要補一行收尾：%s", logs.String())
	}
}
