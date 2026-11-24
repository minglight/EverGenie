package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/engine"
	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// Tests drive bus notifications explicitly; the real durable mailbox and engine event worker remain in use.
type manualBus struct{ *bus.Module }

func (manualBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return make(chan *sdk.BusSubscribeResponse), nil
}

type dispatchFixture struct {
	m  *Module
	h  *homefixture.Home
	b  *bus.Module
	e  *fake.Engine
	g  *testGoals
	tr *testTrust
}

func dispatchSetup(t *testing.T) *dispatchFixture {
	return dispatchSetupMode(t, false)
}
func dispatchSetupMode(t *testing.T, subscribed bool) *dispatchFixture {
	t.Helper()
	ctx := context.Background()
	h := homefixture.New(t)
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	must(t, err)
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "home.toml"), append(data, []byte("\n[sessions]\nautostart=false\n")...), 0600))
	b := bus.New(bus.Deps{Vault: h.Vault, Clock: h.Clock})
	b.SetHome(h.Dir)
	must(t, b.Start(ctx))
	a := agent.New(agent.Deps{Vault: h.Vault, Bus: manualBus{b}, Clock: h.Clock})
	a.SetHome(h.Dir)
	agents := newTestAgents(a, h.Dir)
	must(t, a.Start(ctx))
	must(t, os.MkdirAll(filepath.Join(h.Dir, "personas"), 0700))
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/test.md"), []byte("---\nname: Person\n---\n測試\n"), 0600))
	for _, id := range []string{"code", "work"} {
		h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": true, "mode": "resident"}})
	}
	n := &testNotify{notices: map[string]*sdk.Notification{}, sent: make(chan *sdk.Notification, 32)}
	h.Vault.RegisterSchema(goalSpec)
	g := &testGoals{vault: h.Vault, home: h.Dir}
	var sessionBus BusSender = manualBus{b}
	if subscribed {
		sessionBus = b
	}
	m := New(Deps{Bus: sessionBus, Vault: h.Vault, Agents: agents, Notify: n, Goals: g, Clock: h.Clock})
	tr := newTestTrust()
	m.SetTrust(tr, tr)
	e := fake.New(m.EngineHost())
	m.d.Engine = testRegistry{engine.NewStatic(map[string]sdk.EngineAPI{"main": e})}
	m.SetHome(h.Dir)
	must(t, m.start(ctx, false))
	f := &dispatchFixture{m, h, b, e, g, tr}
	t.Cleanup(func() { must(t, m.Stop(ctx)); must(t, a.Stop(ctx)); must(t, b.Stop(ctx)) })
	f.open(t, "code", "code", sdk.SessionKindResident)
	f.open(t, "work", "work", sdk.SessionKindResident)
	return f
}
func (f *dispatchFixture) open(t *testing.T, name, id string, kind sdk.SessionKind) {
	t.Helper()
	_, err := f.m.Open(context.Background(), &sdk.SessionOpenRequest{Name: name, Agent: id, Kind: kind})
	must(t, err)
	f.m.drainEvents(context.Background())
}
func (f *dispatchFixture) event(t *testing.T, ev sdk.EngineEvent) {
	t.Helper()
	if ev.Session == "" {
		ev.Session = "code"
	}
	if ev.At.IsZero() {
		ev.At = f.h.Clock.Now()
	}
	must(t, f.m.ReportEvent(context.Background(), &ev))
	f.m.drainEvents(context.Background())
}
func (f *dispatchFixture) send(t *testing.T, e *sdk.Envelope) *sdk.Envelope {
	t.Helper()
	if e.From == "" {
		e.From = "work"
	}
	if e.To == "" {
		e.To = "code"
	}
	if e.Kind == "" {
		e.Kind = sdk.EnvelopeKindSay
	}
	if e.Body == "" {
		e.Body = "請處理這則訊息"
	}
	out, err := f.b.Send(context.Background(), e)
	must(t, err)
	return out
}
func (f *dispatchFixture) get(t *testing.T, id string) *sdk.Envelope {
	t.Helper()
	e, err := f.b.Get(context.Background(), id)
	must(t, err)
	return e
}
func (f *dispatchFixture) want(t *testing.T, id string, state sdk.Lifecycle) {
	t.Helper()
	if e := f.get(t, id); e.Lifecycle != state {
		t.Fatalf("%s lifecycle=%s, want %s; history=%+v", id, e.Lifecycle, state, e.History)
	}
}
func (f *dispatchFixture) tick() { f.m.Tick(context.Background(), f.h.Clock.Now()); f.m.settle() }
func (f *dispatchFixture) deliveries() []*sdk.EngineDeliverRequest {
	var out []*sdk.EngineDeliverRequest
	for _, c := range f.e.Calls() {
		if c.Method == "Deliver" {
			out = append(out, c.Request.(*sdk.EngineDeliverRequest))
		}
	}
	return out
}
func (f *dispatchFixture) echoes(t *testing.T) []*sdk.Envelope {
	t.Helper()
	rows, err := f.b.List(context.Background(), "")
	must(t, err)
	var out []*sdk.Envelope
	for _, e := range rows {
		if e.Kind == sdk.EnvelopeKindEcho {
			out = append(out, e)
		}
	}
	return out
}
func (f *dispatchFixture) begin(t *testing.T, e *sdk.Envelope) {
	t.Helper()
	f.tick()
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: e.Body})
}
func (f *dispatchFixture) direct(t *testing.T) *sdk.Envelope {
	t.Helper()
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped})
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeActivityStarted, DirectMessage: &sdk.DirectMessage{To: "code [hub]", Text: "直接傳話"}})
	rows, err := f.b.List(context.Background(), "code")
	must(t, err)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	return rows[0]
}
func (f *dispatchFixture) peer(t *testing.T) {
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "work", Input: "直接傳話"})
}

// Guards: REQ-76, REQ-154
func TestOneAtATime(t *testing.T) {
	f := dispatchSetup(t)
	a := f.send(t, &sdk.Envelope{})
	f.h.Clock.Advance(time.Nanosecond)
	b := f.send(t, &sdk.Envelope{})
	f.tick()
	f.tick()
	f.want(t, a.ID, sdk.LifecycleNotified)
	f.want(t, b.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) != 1 {
		t.Fatal(f.deliveries())
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: a.Body})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "完成"})
	f.tick()
	f.want(t, b.ID, sdk.LifecycleNotified)
}

// Guards: REQ-13, REQ-76
func TestDeliverOnlyWhenIdle(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted})
	e := f.send(t, &sdk.Envelope{})
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) != 0 {
		t.Fatal("delivered while busy")
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// Guards: REQ-76
func TestNeverDeliverDuringPending(t *testing.T) {
	f := dispatchSetup(t)
	f.m.mu.Lock()
	f.m.entries["code"].session.Pending = &sdk.Pending{ID: "p"}
	f.m.mu.Unlock()
	e := f.send(t, &sdk.Envelope{})
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) != 0 {
		t.Fatal("delivered during pending")
	}
}

type interceptRegistry struct {
	EngineRegistry
	before func()
}

func (r interceptRegistry) For(a string) (EngineAccess, error) {
	r.before()
	return r.EngineRegistry.For(a)
}

// Guards: REQ-76
func TestRecheckBeforePaste(t *testing.T) {
	f := dispatchSetup(t)
	base := f.m.d.Engine
	f.m.d.Engine = interceptRegistry{base, func() { f.m.mu.Lock(); f.m.entries["code"].session.Pending = &sdk.Pending{ID: "late"}; f.m.mu.Unlock() }}
	e := f.send(t, &sdk.Envelope{})
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	if len(f.deliveries()) != 0 {
		t.Fatal("missed latest pending")
	}
}

// Guards: REQ-154
func TestOwnerQuietHoldsOthers(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	e := f.send(t, &sdk.Envelope{})
	f.tick()
	f.want(t, e.ID, sdk.LifecycleQueued)
	f.h.Clock.Advance(3 * time.Minute)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// Guards: REQ-154
func TestOwnerChatNotHeld(t *testing.T) {
	for _, kind := range []sdk.EnvelopeKind{sdk.EnvelopeKindChat, sdk.EnvelopeKindAnswer} {
		t.Run(string(kind), func(t *testing.T) {
			f := dispatchSetup(t)
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped})
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
			from := "owner(web)"
			if kind == sdk.EnvelopeKindAnswer {
				from = "system"
			}
			e := f.send(t, &sdk.Envelope{Kind: kind, From: from, Notice: "M-1"})
			f.tick()
			f.want(t, e.ID, sdk.LifecycleNotified)
		})
	}
}

// 引擎說貼不進去（暫時錯誤）：話留在排隊、照次數退避再試；試了多久都不判送不了、不回音（S5-2：計時只決定多久再試）。
func TestPasteRetryKeepsQueuedWithBackoff(t *testing.T) {
	f := dispatchSetup(t)
	f.e.Configure(fake.Behavior{Delivered: false, Reason: "paste"})
	e := f.send(t, &sdk.Envelope{})
	for i := 0; i < 120; i++ {
		f.tick()
		f.h.Clock.Advance(500 * time.Millisecond)
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
	n := len(f.deliveries())
	// 0.5、1、2、4、8 秒，之後每 10 秒：60 秒裡大約 10 次，不是每一輪都貼。
	if n < 5 || n > 15 {
		t.Fatalf("要退避重試：60 秒貼了 %d 次", n)
	}
	if got := f.get(t, e.ID).Reason; !strings.Contains(got, "paste") {
		t.Fatalf("歷程要寫引擎說的原因：%q", got)
	}
	if len(f.echoes(t)) != 0 {
		t.Fatalf("還在試，不回音：%v", f.echoes(t))
	}
	f.e.Configure(fake.Behavior{Delivered: true})
	f.h.Clock.Advance(PasteRetryMax)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// Guards: REQ-13, REQ-154
func TestGoneResumesThenDelivers(t *testing.T) {
	f := dispatchSetup(t)
	f.open(t, "code-topic", "code", sdk.SessionKindRoom)
	s, err := f.m.Get(context.Background(), "code-topic")
	must(t, err)
	must(t, f.m.Close(context.Background(), s.Name))
	e := f.send(t, &sdk.Envelope{To: s.Name})
	f.tick()
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	var resume string
	for _, c := range f.e.Calls() {
		if c.Method == "Start" {
			r := c.Request.(*sdk.EngineStartRequest)
			if r.Name == s.Name {
				resume = r.ResumeSessionID
			}
		}
	}
	if resume != s.EngineSessionID {
		t.Fatal("resume ID lost", resume, s.EngineSessionID)
	}
}

// Guards: REQ-154
func TestSayClonesAfter5Min(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted})
	e := f.send(t, &sdk.Envelope{})
	f.h.Clock.Advance(5 * time.Minute)
	f.tick()
	if f.get(t, e.ID).To != "code-c1" {
		t.Fatal(f.get(t, e.ID))
	}
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// Guards: REQ-154
func TestCloneCap2(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted})
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, f.send(t, &sdk.Envelope{}).ID)
	}
	f.h.Clock.Advance(5 * time.Minute)
	f.tick()
	count := 0
	for _, id := range ids {
		if f.get(t, id).To == "code" {
			count++
		}
	}
	if count != 1 {
		t.Fatal("clone cap", count)
	}
	for _, name := range []string{"code-c1", "code-c2"} {
		s, err := f.m.Get(context.Background(), name)
		must(t, err)
		if s.Kind != sdk.SessionKindWork || s.Task != "" {
			t.Fatal(s)
		}
	}
}

// Guards: REQ-16
func TestRoutineTempAfter2Min(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted})
	e := f.send(t, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, From: "system", IdempotencyKey: "job:code/night#x"})
	f.h.Clock.Advance(2 * time.Minute)
	f.tick()
	if f.get(t, e.ID).To != "code-job-night" {
		t.Fatal(f.get(t, e.ID))
	}
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// Guards: REQ-16
func TestTempSessionCap4(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted})
	var ids []string
	for i := 0; i < 5; i++ {
		ids = append(ids, f.send(t, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, From: "system", IdempotencyKey: fmt.Sprintf("job:code/j%d#x", i)}).ID)
	}
	f.h.Clock.Advance(2 * time.Minute)
	f.tick()
	left := 0
	for _, id := range ids {
		if f.get(t, id).To == "code" {
			left++
		}
	}
	if left != 1 {
		t.Fatal("temp cap", left)
	}
}

// Guards: REQ-76, REQ-154
func TestPastedTextFormat(t *testing.T) {
	f := dispatchSetup(t)
	cases := []struct {
		e    sdk.Envelope
		want string
	}{{sdk.Envelope{Kind: sdk.EnvelopeKindChat, Body: "原文"}, "原文"}, {sdk.Envelope{Kind: sdk.EnvelopeKindAnswer, Notice: "M-7", Body: "好"}, "［主人回答 M-7］好"}, {sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Body: "［排程 code/j］做事"}, "［排程 code/j］做事"}, {sdk.Envelope{Kind: sdk.EnvelopeKindSay, From: "work", Body: "你好"}, "［Person（work）］你好"}, {sdk.Envelope{Kind: sdk.EnvelopeKindSay, From: "work", Body: "好了", ReplyTo: "x"}, "［Person（work）回覆你］好了"}, {sdk.Envelope{Kind: sdk.EnvelopeKindEcho, Body: "原因"}, "［派送通知］原因"}}
	for _, c := range cases {
		if got := f.m.pastedText(context.Background(), &c.e); got != c.want {
			t.Fatalf("%q != %q", got, c.want)
		}
	}
}

// Guards: REQ-153
func TestOriginTable(t *testing.T) {
	for _, c := range []struct {
		source                 sdk.InputSource
		from, origin, wantFrom string
	}{{sdk.InputSourceTyped, "owner(web)", "owner", "owner(web)"}, {sdk.InputSourceTyped, "system", "owner", "system"}, {sdk.InputSourceTyped, "work", "agent", "work"}, {sdk.InputSourceTyped, "", "owner", "owner(session)"}, {sdk.InputSourcePeer, "work", "agent", "work"}} {
		t.Run(string(c.source)+c.from, func(t *testing.T) {
			f := dispatchSetup(t)
			if c.source == sdk.InputSourceTyped && c.from != "" {
				f.send(t, &sdk.Envelope{From: c.from})
				f.tick()
			}
			f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: c.source, From: c.from})
			f.m.mu.Lock()
			got := f.m.entries["code"].turn
			last := f.m.entries["code"].lastOwner
			f.m.mu.Unlock()
			if got.Origin != c.origin || got.From != c.wantFrom {
				t.Fatal(got)
			}
			if last.IsZero() != (c.source != sdk.InputSourceTyped || c.from != "") {
				t.Fatal("last_owner_at", last)
			}
		})
	}
}

// Guards: REQ-153
func TestSystemTurnInheritsOrigin(t *testing.T) {
	f := dispatchSetup(t)
	f.peer(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceSystem})
	// 還是秘書傳來的那一輪：「一律問你」的動作要問主人。
	v, reason, err := f.m.CheckAction(context.Background(), "code", sdk.ActionClassPush, "push", "")
	must(t, err)
	if v != sdk.ActionVerdictAsk {
		t.Fatal(v, reason)
	}
}

// Guards: REQ-14
func TestDirectMessageRecordedAtStart(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t)
	if e.Route != sdk.EnvelopeRouteDirect || e.To != "code" || e.From != "work" || e.Lifecycle != sdk.LifecycleNotified || !strings.HasSuffix(e.IdempotencyKey, "#1") {
		t.Fatal(e)
	}
	if len(f.deliveries()) != 0 {
		t.Fatal("direct message pasted")
	}
}

// Guards: REQ-14, REQ-151
func TestDirectMessageFailedNoEcho(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t)
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeActivityEnded, DirectMessage: &sdk.DirectMessage{To: "code [hub]", OK: false}})
	f.want(t, e.ID, sdk.LifecycleFailed)
	if len(f.echoes(t)) != 0 {
		t.Fatal(f.echoes(t))
	}
}

// Guards: REQ-14
func TestPeerTurnBeforeEnvelope(t *testing.T) {
	f := dispatchSetup(t)
	f.peer(t)
	e := f.direct(t)
	f.h.Clock.Advance(10 * time.Second)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleReceived)
}

// Guards: REQ-14, REQ-151
func TestDirectReplyMarksReplied(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t)
	f.peer(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, DirectMessage: &sdk.DirectMessage{To: "work", Text: "回覆"}})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityEnded, DirectMessage: &sdk.DirectMessage{To: "work", OK: true}})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.want(t, e.ID, sdk.LifecycleReplied)
	if len(f.echoes(t)) != 0 {
		t.Fatal(f.echoes(t))
	}
	rows, err := f.b.List(context.Background(), "work")
	must(t, err)
	if len(rows) != 1 || rows[0].ReplyTo != e.ID {
		t.Fatal(rows)
	}
}

// Guards: REQ-14, REQ-151
func TestDirectNoReplyEchoes(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t)
	f.peer(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "先等等"})
	f.want(t, e.ID, sdk.LifecycleUnanswered)
	echo := f.echoes(t)
	if len(echo) != 1 || echo[0].To != "work" || !strings.Contains(echo[0].Body, "先等等") {
		t.Fatal(echo)
	}
}

// 寄件人的對話被改過名（外框的 from-name 是顯示名稱「架構師-V3 QQ」，不是核心記的 architect）：
// 名字對不上也要靠內文配到封包，不然收件方明明收到了，封包卡 10 分鐘後被記「送不了」（DEV-96，2026-11-01 17:57 起的四則）。
func TestPeerMatchesByBodyWhenSenderDisplayNameDiffers(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t) // work → code，內文「直接傳話」
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "架構師-V3 QQ", Input: "直接傳話"})
	f.want(t, e.ID, sdk.LifecycleReceived)
	// 配到之後這一輪的寄件人要換成真正的秘書 id：回覆才認得是回覆，授權也看得到是誰委派的。
	f.m.mu.Lock()
	from := f.m.entries["code"].turn.From
	f.m.mu.Unlock()
	if from != "work" {
		t.Fatal("寄件人沒校正", from)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted, DirectMessage: &sdk.DirectMessage{To: "work", Text: "回覆"}})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityEnded, DirectMessage: &sdk.DirectMessage{To: "work", OK: true}})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.want(t, e.ID, sdk.LifecycleReplied)
	if len(f.echoes(t)) != 0 {
		t.Fatal(f.echoes(t))
	}
	rows, err := f.b.List(context.Background(), "work")
	must(t, err)
	if len(rows) != 1 || rows[0].ReplyTo != e.ID {
		t.Fatal("回覆沒掛在原封包上", rows)
	}
	f.h.Clock.Advance(11 * time.Minute)
	f.tick()
	if len(f.echoes(t)) != 0 {
		t.Fatal("已回覆的封包不該再有回音", f.echoes(t))
	}
}

// 名字對不上、內文也不一樣：不能亂配（配錯會把別人的話記成已收到）。
func TestPeerWithUnknownNameAndDifferentBodyNotMatched(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "陌生的對話", Input: "完全不同的另一句話"})
	f.h.Clock.Advance(11 * time.Second)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
}

// 名字對得上、內文被平台改過（找不到內文）：照舊配（原本的行為）。
func TestPeerMatchesByNameWhenBodyDiffers(t *testing.T) {
	f := dispatchSetup(t)
	e := f.direct(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "work", Input: "平台改過的內文"})
	f.want(t, e.ID, sdk.LifecycleReceived)
}

// 傳給不是這個家秘書的對象（引擎自己的子代理、別的 session）不追蹤，也就不會報送不了（DEV-85）。
func TestDirectToUnknownPeerNotTracked(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped})
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeActivityStarted, DirectMessage: &sdk.DirectMessage{To: "adc4013b314b114d0", Text: "補充兩件"}})
	rows, err := f.b.List(context.Background(), "adc4013b314b114d0")
	must(t, err)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
	f.h.Clock.Advance(30 * time.Minute)
	f.tick()
	if len(f.echoes(t)) != 0 {
		t.Fatal(f.echoes(t))
	}
}

// Guards: REQ-151
func TestInterruptedEchoes(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{})
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Interrupted: true})
	f.want(t, e.ID, sdk.LifecycleInterrupted)
	if len(f.echoes(t)) != 1 {
		t.Fatal(f.echoes(t))
	}
}

// Guards: REQ-151
func TestEchoNeverEchoes(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{Kind: sdk.EnvelopeKindEcho, From: "system"})
	f.tick()
	f.h.Clock.Advance(time.Second)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeIdleHint}) // 貼上了她卻閒著：回音本身沒送到
	f.want(t, e.ID, sdk.LifecycleFailed)
	if len(f.echoes(t)) != 1 {
		t.Fatal(f.echoes(t))
	}
}

// Guards: REQ-151
func TestSayReplyRoutedBackOnce(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{})
	f.begin(t, e)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "已完成"})
	rows, err := f.b.List(context.Background(), "work")
	must(t, err)
	if len(rows) != 1 || rows[0].ReplyTo != e.ID || rows[0].Body != "已完成" {
		t.Fatal(rows)
	}
	f.tick()
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "已完成"})
	f.event(t, sdk.EngineEvent{Session: "work", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "收到"})
	rows, err = f.b.List(context.Background(), "code")
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("reply loop", rows)
	}
}

// Guards: REQ-153
func TestCheckActionUsesTurnOrigin(t *testing.T) {
	f := dispatchSetup(t)
	f.peer(t)
	v, _, err := f.m.CheckAction(context.Background(), "code", sdk.ActionClassPush, "push", "")
	must(t, err)
	if v != sdk.ActionVerdictAsk {
		t.Fatal(v)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped})
	v, _, err = f.m.CheckAction(context.Background(), "code", sdk.ActionClassPush, "push", "")
	must(t, err)
	if v != sdk.ActionVerdictAllow {
		t.Fatal(v)
	}
}

// 秘書傳來的話就跟打字給她一樣照常做，只有「一律問你」的動作要問主人（2026-11-01 作者）。
// Guards: REQ-153
func TestRelayActsLikeTyping(t *testing.T) {
	f := dispatchSetup(t)
	f.peer(t)
	for class, want := range map[sdk.ActionClass]sdk.ActionVerdict{sdk.ActionClassRead: sdk.ActionVerdictAllow, sdk.ActionClassWrite: sdk.ActionVerdictAllow, sdk.ActionClassPush: sdk.ActionVerdictAsk} {
		v, r, err := f.m.CheckAction(context.Background(), "code", class, "tool", "")
		must(t, err)
		if v != want {
			t.Fatal(class, v, r)
		}
	}
}
func (f *dispatchFixture) goal(t *testing.T) {
	t.Helper()
	f.h.Vault.RegisterSchema(vault.Spec{Kind: "message"})
	f.h.WithRecords(vault.Record{Kind: "message", ID: "M-1", Fields: map[string]any{}})
	f.h.WithRecords(vault.Record{Kind: "goal", ID: "GOAL-1", Fields: map[string]any{"notice": "[[M-1]]", "title": "目的", "text": "主人原文", "state": "active", "agent": "work", "confirmed_by": "owner(web)", "confirmed_at": f.h.Clock.Now().Format(time.RFC3339)}})
}

// 傳話不掛 Goal：內文提到 Goal 編號也不附原文，沒有 Goal 也不加「委派」附註（2026-11-01 作者）。
// Guards: REQ-152
func TestContextNoGoalAttached(t *testing.T) {
	f := dispatchSetup(t)
	f.goal(t)
	f.send(t, &sdk.Envelope{Body: "GOAL-1"})
	f.tick()
	content, err := f.m.GetContext(context.Background(), "code", "GOAL-1")
	must(t, err)
	if strings.Contains(content, "主人原文") || strings.Contains(content, "核心附註") {
		t.Fatal(content)
	}
	g := dispatchSetup(t)
	g.peer(t)
	content, err = g.m.GetContext(context.Background(), "code", "直接傳話")
	must(t, err)
	if strings.Contains(content, "委派") || strings.Contains(content, "Goal") {
		t.Fatal(content)
	}
}

// Guards: REQ-139
func TestAnswerResendUntilReceived(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{Kind: sdk.EnvelopeKindAnswer, From: "system", Notice: "M-1", IdempotencyKey: "M-1#1"})
	must(t, f.b.Transition(context.Background(), e.ID, sdk.LifecycleFailed, bus.TransitionOpt{}))
	f.tick()
	rows, err := f.b.List(context.Background(), "code")
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("early retry")
	}
	f.h.Clock.Advance(30 * time.Second)
	f.tick()
	rows, err = f.b.List(context.Background(), "code")
	must(t, err)
	if len(rows) != 2 || rows[1].IdempotencyKey != "M-1#2" {
		t.Fatal(rows)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: e.Body})
	f.h.Clock.Advance(2 * time.Hour)
	f.tick()
	rows, err = f.b.List(context.Background(), "code")
	must(t, err)
	if len(rows) != 2 {
		t.Fatal("retried after receipt", rows)
	}
}

// Guards: REQ-152, REQ-153
func TestContextAfterTurnAndTurnPersists(t *testing.T) {
	f := dispatchSetup(t)
	f.goal(t)
	e := f.send(t, &sdk.Envelope{Body: "GOAL-1"})
	f.begin(t, e)
	must(t, f.m.Stop(context.Background()))
	raw, err := os.ReadFile(filepath.Join(f.h.Dir, "runtime/sessions/code.md"))
	must(t, err)
	record, _, err := vault.Decode(raw)
	must(t, err)
	fields, ok := record.Fields["turn"].(map[string]any)
	if !ok || fields["origin"] != "agent" || fields["from"] != "work" || fields["envelope"] != e.ID {
		t.Fatal(fields)
	}
	next := New(f.m.d)
	next.SetTrust(f.m.trustLevels, f.m.denialRecorder)
	next.SetHome(f.h.Dir)
	f.m = next
	must(t, next.start(context.Background(), false))
	t.Cleanup(func() { must(t, next.Stop(context.Background())) })
	v, _, err := f.m.CheckAction(context.Background(), "code", sdk.ActionClassPush, "tool", "")
	must(t, err)
	if v != sdk.ActionVerdictAsk {
		t.Fatal("restored turn lost", v)
	}
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "重啟後完成"})
	f.want(t, e.ID, sdk.LifecycleReplied)
}

// Guards: REQ-14, REQ-151
func TestDelayedPeerEndsBeforeMatch(t *testing.T) {
	f := dispatchSetup(t)
	f.peer(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded, LastMessage: "稍後再看"})
	f.h.Clock.Advance(time.Second)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "主人新交代"})
	e := f.direct(t)
	f.h.Clock.Advance(9 * time.Second)
	f.tick()
	f.want(t, e.ID, sdk.LifecycleUnanswered)
	if len(f.echoes(t)) != 1 {
		t.Fatal(f.echoes(t))
	}
	v, _, err := f.m.CheckAction(context.Background(), "code", sdk.ActionClassWrite, "tool", "")
	must(t, err)
	if v != sdk.ActionVerdictAllow {
		t.Fatal("old peer replaced current owner", v)
	}
}

// Guards: REQ-76, REQ-153
func TestBusSubscriptionDeliversAndChecksAction(t *testing.T) {
	f := dispatchSetupMode(t, true)
	f.e.Configure(fake.Behavior{Delivered: true, Events: []sdk.EngineEvent{{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "委派"}, {Type: sdk.EngineEventTypeActivityStarted}, {Type: sdk.EngineEventTypeTurnEnded, LastMessage: "查到了"}}, Actions: []sdk.EngineHostCheckActionRequest{{Class: sdk.ActionClassWrite}}})
	ch, err := f.b.Subscribe(context.Background(), "bus.envelope.state")
	must(t, err)
	e := f.send(t, &sdk.Envelope{Body: "委派"})
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Payload["id"] == e.ID && ev.Payload["state"] == "replied" {
				results := f.e.Results()
				if len(results.Actions) == 0 || results.Actions[0].Verdict != sdk.ActionVerdictAllow {
					t.Fatal(results)
				}
				return
			}
		case <-timeout:
			t.Fatal("subscription did not finish delivery")
		}
	}
}

// Guards: REQ-78, REQ-154
func TestEnvelopeBlockedUntilResolve(t *testing.T) {
	f := dispatchSetup(t)
	e := f.send(t, &sdk.Envelope{})
	f.begin(t, e)
	done := make(chan approvalResult, 1)
	go func() {
		d, decided, err := f.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
		if err != nil {
			t.Error(err)
		}
		done <- approvalResult{d, decided}
	}()
	deadline := time.Now().Add(time.Second)
	for {
		if s, _ := f.m.Get(context.Background(), "code"); s != nil && s.Pending != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.m.drainEvents(context.Background())
	f.want(t, e.ID, sdk.LifecycleBlocked)
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	applied, err := f.m.Resolve(context.Background(), "code", s.Pending.ID, &sdk.Decision{Allow: true}, "owner(web)")
	must(t, err)
	if !applied {
		t.Fatal("resolve not applied")
	}
	result(t, done)
	f.m.drainEvents(context.Background())
	f.want(t, e.ID, sdk.LifecycleWorking)
}

// Guards: REQ-14, REQ-154
func TestDirectTargetDoesNotMatchAnotherSessionOfSameAgent(t *testing.T) {
	f := dispatchSetup(t)
	f.open(t, "code-c1", "code", sdk.SessionKindWork)
	e := f.send(t, &sdk.Envelope{Route: sdk.EnvelopeRouteDirect, To: "code-c1", Body: "指定分身"})
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "work", Input: "指定分身"})
	f.want(t, e.ID, sdk.LifecycleNotified)
	f.event(t, sdk.EngineEvent{Session: "code-c1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourcePeer, From: "work", Input: "指定分身"})
	f.want(t, e.ID, sdk.LifecycleReceived)
}

// Guards: REQ-153
func TestDeniedActionsRecorded(t *testing.T) {
	f := dispatchSetup(t)
	f.peer(t)
	if _, _, err := f.m.CheckAction(context.Background(), "code", sdk.ActionClassRead, "Read", ""); err != nil {
		t.Fatal(err)
	}
	v, _, err := f.m.CheckAction(context.Background(), "code", sdk.ActionClassPush, "Bash", "")
	must(t, err)
	if v != sdk.ActionVerdictAsk {
		t.Fatal(v)
	}
	got := f.tr.Denied()
	if len(got) != 1 || got[0].Agent != "code" || got[0].Class != sdk.ActionClassPush || got[0].Verdict != sdk.ActionVerdictAsk || got[0].Tool != "Bash" || got[0].Reason == "" {
		t.Fatalf("只記被擋的那一筆：%+v", got)
	}
}

// Guards: REQ-84
func TestOpenPassesPermissionLevel(t *testing.T) {
	f := dispatchSetup(t)
	levels := func() map[string]sdk.PermissionLevel {
		out := map[string]sdk.PermissionLevel{}
		for _, c := range f.e.Calls() {
			if r, ok := c.Request.(*sdk.EngineStartRequest); ok && c.Method == "Start" {
				out[r.Name] = r.PermissionLevel
			}
		}
		return out
	}
	if got := levels(); got["code"] != sdk.PermissionLevelFull || got["work"] != sdk.PermissionLevelFull {
		t.Fatalf("沒有 trust.toml 時用內建預設：%v", got)
	}
	// 改了等級不用重啟 serve：下一個開的 session 就用新的。
	must(t, os.MkdirAll(filepath.Dir(platform.TrustPath(f.h.Dir)), 0o700))
	must(t, platform.AtomicWrite(platform.TrustPath(f.h.Dir), []byte("schema=1\n[agent.work]\nlevel='readonly'\n"), 0o600))
	f.open(t, "work-tmp", "work", sdk.SessionKindTemp)
	if got := levels(); got["work-tmp"] != sdk.PermissionLevelReadOnly {
		t.Fatalf("要重讀 trust.toml：%v", got)
	}
}

// Guards: REQ-72, REQ-76
func TestOpenFirstMessageGoesThroughDispatcher(t *testing.T) {
	f := dispatchSetup(t)
	_, err := f.m.Open(context.Background(), &sdk.SessionOpenRequest{Name: "code-first", Agent: "code", FirstMessage: "回我一個字：好"})
	must(t, err)
	rows, err := f.b.List(context.Background(), "code-first")
	must(t, err)
	if len(rows) != 1 || rows[0].Kind != sdk.EnvelopeKindChat || rows[0].Body != "回我一個字：好" {
		t.Fatalf("第一句要排進派送器：%+v", rows)
	}
	f.event(t, sdk.EngineEvent{Session: "code-first", Type: sdk.EngineEventTypeSessionStarted})
	f.tick()
	f.want(t, rows[0].ID, sdk.LifecycleNotified)
}

func TestPermissionLevelFallsBackToStartupConfig(t *testing.T) {
	f := dispatchSetup(t)
	f.m.config.Trust.DefaultLevel = "standard"
	must(t, os.MkdirAll(filepath.Dir(platform.TrustPath(f.h.Dir)), 0o700))
	must(t, platform.AtomicWrite(platform.TrustPath(f.h.Dir), []byte("[[repo]"), 0o600))
	if got := f.m.permissionLevel("code"); got != sdk.PermissionLevelStandard {
		t.Fatalf("壞設定應使用啟動設定，得到 %s", got)
	}
}

// 停了的非常駐對話有話要給它、平台卻開不起來（說同名的還在跑）：MainReopenEvery 內只叫一次，不是每個派送週期都重開、
// 每次記一行（2026-11-05 曾經一天半洗了 5.8 萬行「接回對話失敗」，DEV-153）。過了節流才再試；主人的新一句馬上試。
// Guards: SES-74
func TestStoppedSideSessionReopenIsThrottled(t *testing.T) {
	f := dispatchSetup(t)
	f.open(t, "code-tmp", "code", sdk.SessionKindTemp)
	must(t, f.m.Close(context.Background(), "code-tmp"))
	f.e.Configure(fake.Behavior{StartError: "這個 session 已經在執行"})
	before := f.startsOf("code-tmp")
	f.m.mainReopen["code-tmp"] = f.h.Clock.Now() // 剛叫過一次
	f.h.Clock.Advance(time.Second)
	e := ownerChat("還在嗎")
	e.To = "code-tmp"
	f.send(t, e)
	f.tickFor(2 * time.Second)
	if got := f.startsOf("code-tmp") - before; got != 1 {
		t.Fatalf("主人的新一句不等節流、馬上叫一次，叫了 %d 次", got)
	}
	for i := 0; i < 10; i++ { // 20 秒、十個派送週期
		f.tickFor(2 * time.Second)
	}
	if got := f.startsOf("code-tmp") - before; got != 1 {
		t.Fatalf("開不起來：節流內不再叫，共叫了 %d 次", got)
	}
	f.tickFor(MainReopenEvery)
	if got := f.startsOf("code-tmp") - before; got != 2 {
		t.Fatalf("過了節流再試一次，共叫了 %d 次", got)
	}
}
