package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
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

type testRegistry struct{ r *engine.Registry }

func (r testRegistry) For(account string) (EngineAccess, error) { return r.r.For(account) }

type testBus struct {
	mu     sync.Mutex
	events []*sdk.BusSubscribeResponse
	sent   []*sdk.Envelope
}

func (b *testBus) Publish(_ context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, &sdk.BusSubscribeResponse{Topic: topic, Payload: p})
	return nil
}

// published 回到目前為止發過的事件（複本；背景的派送器也會發，直接讀 events 會有資料競爭）。
func (b *testBus) published() []*sdk.BusSubscribeResponse {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*sdk.BusSubscribeResponse(nil), b.events...)
}
func (b *testBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return make(chan *sdk.BusSubscribeResponse), nil
}
func (b *testBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, old := range b.sent {
		if old.IdempotencyKey == e.IdempotencyKey {
			return old, nil
		}
	}
	copy := *e
	b.sent = append(b.sent, &copy)
	return &copy, nil
}
func (b *testBus) List(context.Context, string, ...sdk.Lifecycle) ([]*sdk.Envelope, error) {
	return nil, nil
}

func (b *testBus) Get(context.Context, string) (*sdk.Envelope, error) {
	return nil, platform.NotFound("找不到訊息")
}
func (b *testBus) Transition(context.Context, string, sdk.Lifecycle, bus.TransitionOpt) error {
	return platform.NotFound("找不到訊息")
}
func (b *testBus) Reroute(context.Context, string, string) error {
	return platform.NotFound("找不到訊息")
}

type testNotify struct {
	mu      sync.Mutex
	notices map[string]*sdk.Notification
	sent    chan *sdk.Notification
}

func (n *testNotify) Send(_ context.Context, notice *sdk.Notification) (*sdk.Notification, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	copy := *notice
	copy.ID = fmt.Sprintf("M-%d", len(n.notices)+1)
	copy.State = sdk.NotificationStateUnread
	n.notices[copy.ID] = &copy
	n.sent <- &copy
	return &copy, nil
}
func (n *testNotify) List(_ context.Context, states []sdk.NotificationState, _ string, _ bool) ([]sdk.Notification, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := []sdk.Notification{}
	for _, notice := range n.notices {
		match := len(states) == 0
		for _, state := range states {
			if notice.State == state {
				match = true
			}
		}
		if match {
			out = append(out, *notice)
		}
	}
	return out, nil
}

type fixture struct {
	m *Module
	h *homefixture.Home
	e *fake.Engine
	b *testBus
	n *testNotify
	a *agent.Module
}

func setup(t *testing.T) *fixture { t.Helper(); return setupWith(t, "") }

// setupWith 跟 setup 一樣，另外把 extra 接在 home.toml 後面（例如多一個帳號）。
func setupWith(t *testing.T, extra string) *fixture {
	t.Helper()
	h := homefixture.New(t)
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	must(t, err)
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "home.toml"), append(data, []byte("\n[sessions]\nautostart=false\n"+extra)...), 0600))
	b := &testBus{}
	a := agent.New(agent.Deps{Vault: h.Vault, Bus: b, Clock: h.Clock})
	a.SetHome(h.Dir)
	agents := newTestAgents(a, h.Dir)
	must(t, a.Start(context.Background()))
	must(t, os.MkdirAll(filepath.Join(h.Dir, "personas"), 0700))
	must(t, platform.AtomicWrite(filepath.Join(h.Dir, "personas/test.md"), []byte("---\nname: Person\n---\n測試人設\n"), 0600))
	h.WithRecords(vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": true, "mode": "resident"}, Body: "測試職責"})
	n := &testNotify{notices: map[string]*sdk.Notification{}, sent: make(chan *sdk.Notification, 32)}
	h.Vault.RegisterSchema(goalSpec)
	g := &testGoals{vault: h.Vault, home: h.Dir}
	m := New(Deps{Goals: g, Vault: h.Vault, Bus: b, Notify: n, Agents: agents, Clock: h.Clock})
	tr := newTestTrust()
	m.SetTrust(tr, tr)
	e := fake.New(m.EngineHost())
	m.d.Engine = testRegistry{engine.NewStatic(map[string]sdk.EngineAPI{"main": e, "second": e})}
	m.SetHome(h.Dir)
	must(t, m.start(context.Background(), false))
	t.Cleanup(func() { must(t, m.Stop(context.Background())); must(t, a.Stop(context.Background())) })
	return &fixture{m, h, e, b, n, a}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) open(t *testing.T, name string) {
	t.Helper()
	_, err := f.m.Open(context.Background(), &sdk.SessionOpenRequest{Name: name, Agent: "code"})
	must(t, err)
}
func (f *fixture) event(t *testing.T, kind sdk.EngineEventType) {
	t.Helper()
	must(t, f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: kind, At: f.h.Clock.Now()}))
}
func (f *fixture) state(t *testing.T, want State) {
	t.Helper()
	got, ok := f.m.State("code")
	if !ok || got != want {
		t.Fatalf("state=%s, want %s", got, want)
	}
}
func (f *fixture) thread(t *testing.T) *sdk.ThreadSummary {
	t.Helper()
	rows, _, err := f.m.Threads(context.Background(), "", true)
	must(t, err)
	for _, r := range rows {
		if r.Name == "code" {
			return r
		}
	}
	t.Fatal("missing thread")
	return nil
}
func (f *fixture) tick() { f.m.Tick(context.Background(), f.h.Clock.Now()); f.m.settle() }

// settle 等背景裡等引擎的事（開、關、貼上）做完。正式跑的時候沒有人等它們；測試要看結果才等。
func (m *Module) settle() {
	for {
		m.bgMu.Lock()
		n := m.bgCount
		m.bgMu.Unlock()
		m.mu.Lock()
		n += len(m.reading) // 已經認領要讀未讀、背景那一段還沒開始的也算
		m.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(200 * time.Microsecond)
	}
}
func (f *fixture) startRequests() []*sdk.EngineStartRequest {
	out := []*sdk.EngineStartRequest{}
	for _, c := range f.e.Calls() {
		if c.Method == "Start" {
			out = append(out, c.Request.(*sdk.EngineStartRequest))
		}
	}
	return out
}

// beginApproval 開始一個確認，等到 session 上有 Pending 才回（S2 起不建通知，回答走 Resolve）。
func (f *fixture) beginApproval(t *testing.T, req *sdk.EngineHostRequestApprovalRequest) (<-chan approvalResult, *sdk.Pending) {
	t.Helper()
	if req.Session == "" {
		req.Session = "code"
	}
	out := make(chan approvalResult, 1)
	go func() {
		d, decided, err := f.m.RequestApproval(context.Background(), req)
		if err != nil {
			t.Error(err)
		}
		out <- approvalResult{d, decided}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, err := f.m.Get(context.Background(), req.Session); err == nil && s.Pending != nil {
			return out, s.Pending
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pending approval")
	return nil, nil
}

// answer 是主人在看板的確認卡上回答（session.resolve）。
func (f *fixture) answer(t *testing.T, p *sdk.Pending, d *sdk.Decision) {
	t.Helper()
	ok, err := f.m.Resolve(context.Background(), "code", p.ID, d, "owner(web)")
	must(t, err)
	if !ok {
		t.Fatal("answer not applied")
	}
}
func result(t *testing.T, ch <-chan approvalResult) approvalResult {
	t.Helper()
	select {
	case r := <-ch:
		return r
	case <-time.After(3 * time.Second):
		t.Fatal("approval did not finish")
	}
	return approvalResult{}
}

// Guards: REQ-75
func TestStartedBecomesIdle(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.state(t, sdk.SessionStateIdle)
}

// Guards: REQ-72
func TestWatchdogRestartsResident(t *testing.T) {
	f := setup(t)
	f.m.config.Sessions.Autostart = true
	f.tick()
	f.state(t, sdk.SessionStateIdle)
	f.event(t, sdk.EngineEventTypeSessionEnded)
	f.h.Clock.Advance(WatchdogEvery)
	f.tick()
	f.state(t, sdk.SessionStateIdle)
	if len(f.startRequests()) != 2 {
		t.Fatal("resident not reopened")
	}
}

// flakyRegistry 在 ready 之前回「引擎尚未就緒」（serve 剛起來、引擎套件還沒聽 socket 的情況）。
type flakyRegistry struct {
	inner testRegistry
	ready *bool
}

func (r flakyRegistry) For(account string) (EngineAccess, error) {
	if !*r.ready {
		return nil, platform.Unavailable("引擎尚未就緒，請稍後重試")
	}
	return r.inner.For(account)
}

// Guards: REQ-72
func TestAutostartWaitsForEngineQuietly(t *testing.T) {
	f := setup(t)
	var logs bytes.Buffer
	f.m.log = slog.New(slog.NewTextHandler(&logs, nil))
	ready := false
	f.m.d.Engine = flakyRegistry{inner: f.m.d.Engine.(testRegistry), ready: &ready}
	f.m.config.Sessions.Autostart = true
	f.tick()
	if len(f.startRequests()) != 0 || logs.Len() != 0 {
		t.Fatalf("engine not ready must neither start nor warn: %q", logs.String())
	}
	// 引擎一就緒，只要等重試間隔（不是整個 WatchdogEvery）就會開起來。
	ready = true
	f.h.Clock.Advance(EngineRetryEvery)
	f.tick()
	f.state(t, sdk.SessionStateIdle)
	if logs.Len() != 0 {
		t.Fatalf("unexpected log: %q", logs.String())
	}
}

// Guards: REQ-72
func TestAutostartWarnsWhenEngineNeverReady(t *testing.T) {
	f := setup(t)
	var logs bytes.Buffer
	f.m.log = slog.New(slog.NewTextHandler(&logs, nil))
	ready := false
	f.m.d.Engine = flakyRegistry{inner: f.m.d.Engine.(testRegistry), ready: &ready}
	f.m.config.Sessions.Autostart = true
	f.tick()
	f.h.Clock.Advance(EngineReadyGrace)
	f.tick()
	if !strings.Contains(logs.String(), "開啟常駐對話失敗") {
		t.Fatalf("a persistent failure must still warn: %q", logs.String())
	}
}

// Guards: REQ-72
func TestOnlyResidentModeAutostarts(t *testing.T) {
	f := setup(t)
	for id, mode := range map[string]string{"other": "ondemand", "none": "none", "off": "resident"} {
		f.h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": id != "off", "mode": mode}})
	}
	f.tick()
	if len(f.startRequests()) != 0 {
		t.Fatal("autostart false ignored")
	}
	f.m.config.Sessions.Autostart = true
	f.tick()
	if rs := f.startRequests(); len(rs) != 1 || rs[0].Name != "code" {
		t.Fatal(rs)
	}
}

// Guards: REQ-179

// Guards: REQ-73
func TestRemoteAccessNameFollowsPersona(t *testing.T) {
	f := setup(t)
	f.m.config.Accounts["main"].(map[string]any)["remote_access"] = true
	f.m.config.Runtime.Owner, f.m.config.Runtime.Machine = "amy", "home"
	f.open(t, "code")
	if f.startRequests()[0].RemoteAccessName != "Person-amy-home" {
		t.Fatal(f.startRequests())
	}
	must(t, platform.AtomicWrite(filepath.Join(f.h.Dir, "personas/test.md"), []byte("+++\nname='New'\n+++\n"), 0600))
	must(t, f.m.Close(context.Background(), "code"))
	f.open(t, "code")
	if f.startRequests()[1].RemoteAccessName != "New-amy-home" {
		t.Fatal(f.startRequests())
	}
	r := f.startRequests()[1]
	if r.Agent != "code" || r.Cwd != f.h.Dir || r.Instructions == "" {
		t.Fatal(r)
	}
}

// 同一個角色派給兩位秘書：遠端名稱補職位 id，手機上才分得出來（DEV-90）；停用的不算。
func TestRemoteAccessNameSharedPersonaAddsAgent(t *testing.T) {
	for _, tc := range []struct {
		lifeOn bool
		want   string
	}{{false, "Person-amy-home"}, {true, "Person·code-amy-home"}} {
		f := setup(t)
		f.m.config.Accounts["main"].(map[string]any)["remote_access"] = true
		f.m.config.Runtime.Owner, f.m.config.Runtime.Machine = "amy", "home"
		f.h.WithRecords(vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{"persona": "test", "duty": "測試", "enabled": tc.lifeOn, "mode": "none"}})
		f.open(t, "code")
		if got := f.startRequests()[0].RemoteAccessName; got != tc.want {
			t.Fatal(tc.lifeOn, got)
		}
	}
}

// Guards: REQ-74
func TestOpenRoomIdleCloseResume(t *testing.T) {
	f := setup(t)
	f.open(t, "code-topic")
	f.open(t, "code-topic")
	if len(f.startRequests()) != 1 {
		t.Fatal("duplicate start")
	}
	f.h.Clock.Advance(30 * time.Minute)
	f.tick()
	s, err := f.m.Get(context.Background(), "code-topic")
	must(t, err)
	if s.State != sdk.SessionStateStopped || s.EngineSessionID == "" {
		t.Fatal(s)
	}
	f.open(t, "code-topic")
	if f.startRequests()[1].ResumeSessionID != s.EngineSessionID {
		t.Fatal("resume id lost")
	}
}

// Guards: REQ-74
func TestResumeKeepsFolderAndAttachHint(t *testing.T) {
	f := setup(t)
	dir := t.TempDir()
	_, err := f.m.Open(context.Background(), &sdk.SessionOpenRequest{Name: "code-repo", Agent: "code", Cwd: dir})
	must(t, err)
	s, err := f.m.Get(context.Background(), "code-repo")
	must(t, err)
	if s.Cwd != dir || s.Attach != "fake-attach code-repo" {
		t.Fatalf("開啟後要記住資料夾與接上畫面的指令：%+v", s)
	}
	must(t, f.m.Close(context.Background(), "code-repo"))
	must(t, f.m.Stop(context.Background()))
	must(t, f.m.start(context.Background(), false))
	f.open(t, "code-repo")
	if got := f.startRequests()[1]; got.Cwd != dir || got.ResumeSessionID != s.EngineSessionID {
		t.Fatalf("接回要沿用原本的資料夾與 session：%+v", got)
	}
}

// Guards: REQ-75
func TestStateFromEngineEvents(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	data, err := os.ReadFile("testdata/turn.jsonl")
	must(t, err)
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event sdk.EngineEvent
		must(t, json.Unmarshal([]byte(line), &event))
		must(t, f.m.ReportEvent(context.Background(), &event))
		want := sdk.SessionStateBusy
		if event.Type == sdk.EngineEventTypeTurnEnded {
			want = sdk.SessionStateIdle
		}
		f.state(t, want)
	}
	f.h.Clock.Advance(time.Minute)
	f.tick()
	f.state(t, sdk.SessionStateIdle)
}

// 被中斷後又開始的一輪：處理中就是處理中，沒有新的引擎回報時等多久都不變（不再猜「卡住」，S5-2）。
// Guards: REQ-75, REQ-142
func TestInterruptedTurnStaysBusyWithoutEvents(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	must(t, f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeTurnStarted, Interrupted: true}))
	f.h.Clock.Advance(24 * time.Hour)
	f.tick()
	f.state(t, sdk.SessionStateBusy)
	if f.thread(t).Status != sdk.ThreadStatusWorking {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-77
func TestReportEventIsSilentAndFast(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	before := len(f.e.Calls())
	samples := make([]time.Duration, 200)
	for i := range samples {
		start := time.Now()
		err := f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeActivityStarted})
		samples[i] = time.Since(start)
		must(t, err)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	if p99 := samples[197]; p99 >= 50*time.Millisecond {
		t.Fatalf("ReportEvent P99=%s, want <50ms", p99)
	}
	if len(f.e.Calls()) != before || len(f.n.notices) != 0 {
		t.Fatal("event made an external call")
	}
}

// Guards: REQ-77
func TestReportEventDoesNotWaitForDispatcher(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.m.mu.Lock()
	f.m.dispatcher = make(chan EngineEvent)
	f.m.mu.Unlock()
	done := make(chan error, 1)
	go func() {
		done <- f.m.ReportEvent(context.Background(), &sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeActivityStarted})
	}()
	select {
	case err := <-done:
		must(t, err)
	case <-time.After(time.Second):
		t.Fatal("ReportEvent waited for receiver")
	}
}

// Guards: REQ-77, REQ-47
func TestGetContextEmptyWhenUnchanged(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	first, err := f.m.GetContext(context.Background(), "code", "")
	must(t, err)
	if first == "" {
		t.Fatal("missing context")
	}
	second, err := f.m.GetContext(context.Background(), "code", "")
	must(t, err)
	if second != "" {
		t.Fatal("context repeated")
	}
}

// Guards: REQ-78
func TestApprovalFlowAllowDeny(t *testing.T) {
	for _, answer := range []string{"允許", "拒絕"} {
		t.Run(answer, func(t *testing.T) {
			f := setup(t)
			f.open(t, "code")
			out, p := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
			f.state(t, sdk.SessionStateWaiting)
			if p.Kind != sdk.PendingKindPermission || p.Summary != "寫入" {
				t.Fatal(p)
			}
			f.answer(t, p, &sdk.Decision{Allow: answer == "允許", Message: answer})
			r := result(t, out)
			if !r.decided || r.decision.Allow != (answer == "允許") {
				t.Fatal(r)
			}
			f.state(t, sdk.SessionStateBusy)
		})
	}
}

// Guards: REQ-78
func TestLateResolveNotApplied(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, _ := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	id := s.Pending.ID
	f.event(t, sdk.EngineEventTypeActivityEnded)
	if result(t, out).decided {
		t.Fatal("external answer ignored")
	}
	ok, err := f.m.Resolve(context.Background(), "code", id, &sdk.Decision{Allow: true}, "owner(web)")
	must(t, err)
	if ok {
		t.Fatal("stale answer accepted")
	}
}

// Guards: REQ-78
func TestApprovalTimeout(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, _ := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	f.h.Clock.Advance(ApprovalTimeout)
	f.tick()
	if result(t, out).decided {
		t.Fatal("timeout decided")
	}
	// 逾時之後留在畫面上等（SES-48）：Pending 還在、標 native。
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil || !s.Pending.Native {
		t.Fatalf("逾時後 Pending 要留著：%+v %v", s.Pending, err)
	}
}

// Guards: REQ-79
func TestQuestionButtons(t *testing.T) {
	for _, q := range []sdk.Question{{Text: "單選", Options: []sdk.QuestionOption{{Label: "A"}, {Label: "B"}}}, {Text: "複選", MultiSelect: true, Options: []sdk.QuestionOption{{Label: "A"}, {Label: "B"}}}, {Text: "自由", AllowFreeText: true}} {
		t.Run(q.Text, func(t *testing.T) {
			f := setup(t)
			f.open(t, "code")
			out, p := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{q}})
			answer := "自由文字"
			if len(q.Options) > 0 {
				answer = "A"
			}
			if q.MultiSelect {
				answer = "A, B"
			}
			f.answer(t, p, &sdk.Decision{Allow: true, Answers: map[string]string{q.Text: answer}})
			r := result(t, out)
			if !r.decided || r.decision.Answers[q.Text] != answer {
				t.Fatal(r)
			}
		})
	}
}

// Guards: REQ-79
func TestMultiQuestionResolvesWhenAllAnswered(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, p := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "one"}, {Text: "two", MultiSelect: true}}})
	if _, err := f.m.Resolve(context.Background(), "code", p.ID, &sdk.Decision{Answers: map[string]string{"one": "A"}}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("只答一題要被擋：%v", err)
	}
	select {
	case <-out:
		t.Fatal("resolved before all answers")
	default:
	}
	f.answer(t, p, &sdk.Decision{Answers: map[string]string{"one": "A", "two": "A, B"}})
	r := result(t, out)
	if !reflect.DeepEqual(r.decision.Answers, map[string]string{"one": "A", "two": "A, B"}) {
		t.Fatal(r)
	}
}

// Guards: REQ-261
func TestAccountChangeRestartsWhenIdle(t *testing.T) {
	f := setupWith(t, "[accounts.second]\nengine=\"test-engine\"\n")
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	f.state(t, sdk.SessionStateIdle)
	f.m.mu.Lock()
	f.m.entries["code"].session.State = sdk.SessionStateBusy
	f.m.mu.Unlock()
	_, err := f.a.PatchAgent(context.Background(), "code", map[string]any{"account": "second"}, nil, "owner(web)")
	must(t, err)
	f.h.Clock.Advance(WatchdogEvery)
	f.tick()
	if n := len(f.startRequests()); n != 1 {
		t.Fatalf("在忙的不能重開，要等做完：Start %d 次", n)
	}
	f.m.mu.Lock()
	f.m.entries["code"].session.State = sdk.SessionStateIdle
	f.m.mu.Unlock()
	f.h.Clock.Advance(WatchdogEvery)
	f.tick()
	rs := f.startRequests()
	if len(rs) != 2 || rs[1].Account != "second" || rs[1].ResumeSessionID != "" {
		t.Fatalf("閒著要換新帳號開新的對話（不接舊的）：%+v", rs)
	}
	f.event(t, sdk.EngineEventTypeSessionStarted)
	f.h.Clock.Advance(WatchdogEvery)
	f.tick()
	if n := len(f.startRequests()); n != 2 {
		t.Fatalf("帳號已經一樣就不要再重開：Start %d 次", n)
	}
	// 手動重開（關了再開）也不能拿舊帳號的對話去接
	_, err = f.a.PatchAgent(context.Background(), "code", map[string]any{"account": "main"}, nil, "owner(web)")
	must(t, err)
	must(t, f.m.Close(context.Background(), "code"))
	f.open(t, "code")
	rs = f.startRequests()
	if len(rs) != 3 || rs[2].Account != "main" || rs[2].ResumeSessionID != "" {
		t.Fatalf("換了帳號再開，不能接另一個帳號的對話：%+v", rs[len(rs)-1])
	}
}

// Guards: REQ-80
func TestRotateAfterNightly(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	must(t, f.m.Rotate(context.Background(), "code"))
	rs := f.startRequests()
	if len(rs) != 2 || rs[1].ResumeSessionID != "" {
		t.Fatal(rs)
	}
}
func (f *fixture) turn(t *testing.T) {
	t.Helper()
	f.e.Configure(fake.Behavior{Delivered: true, Events: []sdk.EngineEvent{{Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped}, {Type: sdk.EngineEventTypeTurnEnded}}, Messages: []sdk.ChatMessage{{Uuid: "a1", Kind: sdk.ChatKindAssistant, Text: "完成", Ts: f.h.Clock.Now()}}})
	_, err := f.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "測試"})
	must(t, err)
	f.m.refreshUnread(context.Background())
	f.m.settle()
}

// Guards: REQ-142
func TestThreadStatusPriority(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.turn(t)
	f.m.mu.Lock()
	e := f.m.entries["code"]
	e.session.State = sdk.SessionStateBusy
	f.m.envelopes["q"] = &sdk.Envelope{To: "code", Lifecycle: sdk.LifecycleQueued}
	e.session.Pending = &sdk.Pending{Kind: sdk.PendingKindPermission}
	f.m.mu.Unlock()
	if got := f.thread(t).Status; got != sdk.ThreadStatusWaitingUser {
		t.Fatal(got)
	}
	f.m.mu.Lock()
	e.session.Pending = nil
	e.session.State = sdk.SessionStateBusy
	f.m.mu.Unlock()
	if f.thread(t).Status != sdk.ThreadStatusWorking {
		t.Fatal(f.thread(t))
	}
	f.m.mu.Lock()
	e.session.State = sdk.SessionStateIdle
	f.m.mu.Unlock()
	if f.thread(t).Status != sdk.ThreadStatusQueued {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-142
func TestStatusTimelineAfterSend(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": "q", "to": "code", "state": "queued"}}))
	if f.thread(t).Status != sdk.ThreadStatusQueued {
		t.Fatal(f.thread(t))
	}
	f.event(t, sdk.EngineEventTypeTurnStarted)
	if f.thread(t).Status != sdk.ThreadStatusWorking {
		t.Fatal(f.thread(t))
	}
	must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "bus.envelope.state", Payload: map[string]any{"id": "q", "to": "code", "state": "replied"}}))
	f.turn(t)
	if f.thread(t).Status != sdk.ThreadStatusDone {
		t.Fatal(f.thread(t))
	}
	_, err := f.m.MarkSeen(context.Background(), "code", "", "owner(web)")
	must(t, err)
	if f.thread(t).Status != sdk.ThreadStatusIdle {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-142
func TestInterruptNotDone(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.turn(t)
	f.event(t, sdk.EngineEventTypeTurnStarted)
	f.h.Clock.Advance(24 * time.Hour)
	f.tick()
	if f.thread(t).Status != sdk.ThreadStatusWorking {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-142
func TestBackgroundTasksShown(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	data, err := os.ReadFile("testdata/background.jsonl")
	must(t, err)
	var ev sdk.EngineEvent
	must(t, json.Unmarshal(data, &ev))
	must(t, f.m.ReportEvent(context.Background(), &ev))
	if f.thread(t).BackgroundTasks != 1 {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-143
func TestWaitingStaysUntilAnswered(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, p := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	_, err := f.m.MarkSeen(context.Background(), "code", "", "owner(web)")
	must(t, err)
	f.event(t, sdk.EngineEventTypeIdleHint)
	if f.thread(t).Status != sdk.ThreadStatusWaitingUser {
		t.Fatal(f.thread(t))
	}
	f.answer(t, p, &sdk.Decision{Allow: true})
	result(t, out)
	if f.thread(t).Status == sdk.ThreadStatusWaitingUser {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-143
func TestApprovalNoticeMakesWaiting(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.sent", Payload: map[string]any{"id": "M-1", "category": "approve", "room": "code"}}))
	if r := f.thread(t); r.Status != sdk.ThreadStatusWaitingUser || !reflect.DeepEqual(r.Waiting, []sdk.WaitingReason{sdk.WaitingReasonApproval}) {
		t.Fatal(r)
	}
	must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "room": "code", "text": "好"}}))
	if f.thread(t).Status == sdk.ThreadStatusWaitingUser {
		t.Fatal(f.thread(t))
	}
}

// Guards: REQ-143
func TestSeenSyncedAcrossDevices(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.turn(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	phone, err := f.m.WatchThreads(ctx, "")
	must(t, err)
	desktop, err := f.m.WatchThreads(ctx, "")
	must(t, err)
	<-phone
	<-desktop
	_, err = f.m.MarkSeen(ctx, "code", "", "owner(phone)")
	must(t, err)
	f.h.Clock.Advance(time.Second)
	f.tick()
	for _, ch := range []<-chan *sdk.SessionWatchThreadsResponse{phone, desktop} {
		select {
		case ev := <-ch:
			if ev.Thread.Unread != 0 || ev.Thread.SeenUuid != "a1" || ev.Thread.Status != sdk.ThreadStatusIdle {
				t.Fatal(ev)
			}
		case <-time.After(time.Second):
			t.Fatal("missing seen update")
		}
	}
	must(t, f.m.Stop(ctx))
	must(t, f.m.start(ctx, false))
	s := f.thread(t)
	if s.SeenUuid != "a1" {
		t.Fatal(s)
	}
}

// Guards: REQ-143
func TestRemoteTypedThreadHasStatus(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.turn(t)
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if s.LastUserAt.IsZero() || f.thread(t).Unread != 1 {
		t.Fatal(s, f.thread(t))
	}
}

// Guards: REQ-143
func TestWatchThreadsCoalesces(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	ch, err := f.m.WatchThreads(context.Background(), "")
	must(t, err)
	<-ch
	f.event(t, sdk.EngineEventTypeTurnStarted)
	f.event(t, sdk.EngineEventTypeActivityStarted)
	f.event(t, sdk.EngineEventTypeActivityEnded)
	f.tick()
	if len(ch) != 1 {
		t.Fatal("expected one coalesced update", len(ch))
	}
	<-ch
	f.tick()
	if len(ch) != 0 {
		t.Fatal("duplicate update")
	}
	f.h.Clock.Advance(time.Second)
	f.tick()
	if len(ch) != 0 {
		t.Fatal("unchanged summary emitted")
	}
}

// Guards: REQ-30, REQ-139
func TestAnswerPacketBackToAsker(t *testing.T) {
	f := setup(t)
	// 要不要轉由 notify 判斷（relay；規則與測試在 notify 的 relaysAnswer）：session 只照 relay 送回 room，沒有 room 就送給秘書。
	for _, p := range []map[string]any{{"id": "M-1", "room": "code-topic", "text": "好", "task": "SEC-1", "relay": true}, {"id": "M-2", "agent": "code", "text": "A, B", "relay": true}, {"id": "M-3", "agent": "code", "text": "知道了", "source": "system.alert", "relay": false}, {"id": "M-4", "agent": "code", "text": "好", "via": "auto", "relay": false}, {"id": "M-5", "agent": "code", "text": "好", "task": "SEC-2"}, {"id": "M-6", "agent": "system", "text": "好", "relay": true}} {
		ev := &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: p}
		must(t, f.m.handle(context.Background(), ev))
		must(t, f.m.handle(context.Background(), ev))
	}
	if len(f.b.sent) != 2 {
		t.Fatal(f.b.sent)
	}
	for i, target := range []string{"code-topic", "code"} {
		e := f.b.sent[i]
		if e.To != target || e.From != "system" || e.Kind != sdk.EnvelopeKindAnswer || e.IdempotencyKey != fmt.Sprintf("M-%d#1", i+1) {
			t.Fatal(e)
		}
	}
	// 綁在單上、沒綁對話的問句（等你決定，DEV-156 U-05）：那張單有工作 session 就送到那裡（停了也算，派送時接回）；沒有就送給秘書本人。
	f.open(t, "code-sec-7")
	must(t, f.m.Close(context.Background(), "code-sec-7"))
	f.b.sent = nil
	for _, p := range []map[string]any{{"id": "M-7", "agent": "code", "task": "SEC-7", "text": "日式", "relay": true}, {"id": "M-8", "agent": "code", "task": "SEC-8", "text": "義式", "relay": true}} {
		must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: p}))
	}
	if len(f.b.sent) != 2 || f.b.sent[0].To != "code-sec-7" || f.b.sent[0].Body != "日式" || f.b.sent[1].To != "code" || f.b.sent[1].Kind != sdk.EnvelopeKindAnswer {
		t.Fatalf("單的問句送到工作 session，沒有才送秘書：%+v", f.b.sent)
	}
}

// Guards: REQ-81
func TestChatStream(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.turn(t)
	ch, err := f.m.Chat(context.Background(), "code", "", false)
	must(t, err)
	found := false
	for msg := range ch {
		if msg.Uuid == "a1" {
			found = true
		}
	}
	if !found {
		t.Fatal("assistant missing")
	}
	ctx, cancel := context.WithCancel(context.Background())
	follow, err := f.m.Chat(ctx, "code", "a1", true)
	must(t, err)
	cancel()
	select {
	case _, ok := <-follow:
		if ok {
			t.Fatal("unexpected message")
		}
	case <-time.After(time.Second):
		t.Fatal("stream did not cancel")
	}
}

// Guards: REQ-74, REQ-67
func TestRoomWaitShowsOnThread(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.m.SetRoomWait("R-1", "code", sdk.WaitingReasonReminder)
	f.m.SetRoomWait("R-2", "code", sdk.WaitingReasonReminder) // 同一個原因只列一次
	if r := f.thread(t); !reflect.DeepEqual(r.Waiting, []sdk.WaitingReason{sdk.WaitingReasonReminder}) {
		t.Fatal(r.Waiting)
	}
	f.m.SetRoomWait("R-1", "", sdk.WaitingReasonReminder)
	f.m.SetRoomWait("R-2", "", sdk.WaitingReasonReminder)
	if r := f.thread(t); len(r.Waiting) != 0 {
		t.Fatal(r.Waiting)
	}
}

// Guards: REQ-72, REQ-74
func TestSessionSettingsAndStateRoundTrip(t *testing.T) {
	f := setup(t)
	f.open(t, "code-topic")
	s, err := f.m.Get(context.Background(), "code-topic")
	must(t, err)
	path := filepath.Join(f.h.Dir, "runtime/sessions/code-topic.md")
	data, err := os.ReadFile(path)
	must(t, err)
	r, order, err := vault.Decode(data)
	must(t, err)
	r.Fields["extra"] = "keep"
	r.Body = "body\n"
	must(t, platform.AtomicWrite(path, vault.Encode(r, order, vault.Spec{}), 0600))
	must(t, f.m.Stop(context.Background()))
	must(t, f.m.start(context.Background(), false))
	must(t, f.m.Close(context.Background(), "code-topic"))
	f.open(t, "code-topic")
	if f.startRequests()[1].ResumeSessionID != s.EngineSessionID {
		t.Fatal("resume lost after restart")
	}
	data, err = os.ReadFile(path)
	must(t, err)
	r, _, err = vault.Decode(data)
	must(t, err)
	if r.Fields["extra"] != "keep" || r.Body != "body" {
		t.Fatal(r)
	}
	h, err := platform.LoadHome(f.h.Dir)
	must(t, err)
	if h.Sessions.Autostart || h.Sessions.Workdir != f.h.Dir {
		t.Fatal(h.Sessions)
	}
}

func (f *fixture) receipts() []*sdk.BusSubscribeResponse {
	f.m.mu.Lock()
	defer f.m.mu.Unlock()
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	out := []*sdk.BusSubscribeResponse{}
	for _, ev := range f.b.events {
		if ev.Topic == "session.approval.resolved" {
			out = append(out, ev)
		}
	}
	return out
}

// Guards: REQ-78, REQ-79, REQ-139
func TestApprovalReceiptOnlyAfterAllAnswers(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, p := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "one"}, {Text: "two"}}})
	id := p.ID
	if _, err := f.m.Resolve(context.Background(), "code", id, &sdk.Decision{Answers: map[string]string{"one": "A"}}, "owner(web)"); err == nil || len(f.receipts()) != 0 {
		t.Fatal("partial answers emitted receipt")
	}
	f.answer(t, p, &sdk.Decision{Answers: map[string]string{"one": "A", "two": "B"}})
	if !result(t, out).decided {
		t.Fatal("not decided")
	}
	evs := f.receipts()
	if len(evs) != 1 {
		t.Fatal("expected one receipt", evs)
	}
	// S2 起確認不建通知（只推播），回執的 notices 是空的。
	if evs[0].Payload["name"] != "code" || evs[0].Payload["pending_id"] != id || len(evs[0].Payload["notices"].([]string)) != 0 {
		t.Fatal(evs[0])
	}
	applied, err := f.m.Resolve(context.Background(), "code", id, &sdk.Decision{}, "owner(web)")
	must(t, err)
	if applied || len(f.receipts()) != 1 {
		t.Fatal("late resolve emitted receipt")
	}
}

// 過期之後看板回答交給引擎在畫面上代答（SES-48）；畫面上已經沒有確認框（引擎回 none）就不算、不發回執。
// Guards: REQ-78, REQ-139, REQ-377
func TestExpiredResolveNeverEmitsReceipt(t *testing.T) {
	f := setup(t)
	f.e.Configure(fake.Behavior{Delivered: true, AnswerReason: sdk.AnswerNone})
	f.open(t, "code")
	out, _ := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	f.h.Clock.Advance(ApprovalTimeout)
	applied, err := f.m.Resolve(context.Background(), "code", s.Pending.ID, &sdk.Decision{Allow: true}, "owner(web)")
	must(t, err)
	if applied || result(t, out).decided || len(f.receipts()) != 0 {
		t.Fatal("expired confirmation succeeded")
	}
}

// Guards: REQ-139, REQ-343
func TestModuleOwnedAnswersStayOutOfAgentThreads(t *testing.T) {
	f := setup(t)
	// 業務模組自己處理的回答（登記的通知來源），notify 發的 relay 是 false：session 不看 source，只看 relay。
	for _, p := range []map[string]any{{"id": "M-1", "agent": "work", "text": "不要", "source": sdk.NotifySourceGoals, "relay": false}, {"id": "M-2", "agent": "system", "text": "核准", "source": sdk.NotifySourcePolicy, "relay": false}, {"id": "M-3", "agent": "code", "text": "之後再說", "source": sdk.NotifySourcePackage, "relay": false}} {
		must(t, f.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: p}))
	}
	if len(f.b.sent) != 0 {
		t.Fatalf("Goal 確認與權限申請的回答不該轉進秘書的對話：%v", f.b.sent)
	}
}

// Guards: REQ-256
func TestCloseStoppedStillStopsEngine(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.event(t, sdk.EngineEventTypeSessionStarted)
	// 重開時舊的那一次晚到的結束事件把剛開好的記成停了（DEV-10）；引擎那邊其實還開著。
	f.event(t, sdk.EngineEventTypeSessionEnded)
	f.state(t, sdk.SessionStateStopped)
	must(t, f.m.Close(context.Background(), "code"))
	stops := 0
	for _, c := range f.e.Calls() {
		if c.Method == "Stop" && c.Request.(*sdk.EngineStopRequest).Name == "code" {
			stops++
		}
	}
	if stops != 1 {
		t.Fatalf("核心記成停了也要叫引擎關一次：Stop 呼叫 %d 次", stops)
	}
	f.state(t, sdk.SessionStateStopped)
	f.open(t, "code")
	if len(f.startRequests()) != 2 {
		t.Fatal("關掉之後要開得起來")
	}
}

// 選擇題等主人在看板回答 12 小時才交回她的畫面；權限確認照舊 10 分鐘（SES-66，作者 2026-11-03「不能在 genie 這邊回答」）。
func TestQuestionWaitsLongerThanPermission(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, _ := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "選哪個", Options: []sdk.QuestionOption{{Label: "A"}, {Label: "B"}}}}})
	f.h.Clock.Advance(ApprovalTimeout + time.Minute)
	f.tick()
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil || s.Pending.Native {
		t.Fatalf("10 分鐘後選擇題還要在看板等，不能交回畫面：%+v %v", s.Pending, err)
	}
	f.h.Clock.Advance(QuestionTimeout)
	f.tick()
	if result(t, out).decided {
		t.Fatal("逾時不能算有決定")
	}
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil || !s.Pending.Native {
		t.Fatalf("12 小時後交回她的畫面：%+v %v", s.Pending, err)
	}
}

// 選擇題還在看板等（12 小時內）時主人直接在她的對話裡答了：引擎那邊還在等，但那一步做完（activity_ended）
// 核心就收掉確認、引擎拿到沒決定（SES-66 等得久之後，看板不能一直掛著答過的題目）。
func TestQuestionAnsweredOnHerScreenClearsBoardPending(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	out, _ := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "選哪個", Options: []sdk.QuestionOption{{Label: "A"}, {Label: "B"}}}}})
	f.h.Clock.Advance(time.Hour)
	f.event(t, sdk.EngineEventTypeActivityEnded)
	if result(t, out).decided {
		t.Fatal("在她畫面上答的不能算看板的決定")
	}
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending != nil {
		t.Fatalf("那一步做完要收掉確認：%+v %v", s.Pending, err)
	}
}

// 核心要停（部署、重開）時還在等的選擇題：回 unavailable（引擎等核心回來再問），不回「沒決定」；
// 重開後那題當 native 接回，引擎再問同一題會取代它、看板照樣能答（SES-66）。
func TestQuestionWaitingWhenCoreStopsAsksEngineToRetry(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	q := &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "選哪個", Options: []sdk.QuestionOption{{Label: "A"}, {Label: "B"}}}}}
	errs := make(chan error, 1)
	go func() {
		_, _, err := f.m.RequestApproval(context.Background(), q)
		errs <- err
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if s, err := f.m.Get(context.Background(), "code"); err == nil && s.Pending != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending approval")
		}
		time.Sleep(5 * time.Millisecond)
	}
	must(t, f.m.Stop(context.Background()))
	select {
	case err := <-errs:
		var pe *platform.Err
		if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeUnavailable {
			t.Fatalf("核心要停時要回 unavailable，實際 %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("核心停了，等待還沒結束")
	}
	must(t, f.m.start(context.Background(), false))
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil || !s.Pending.Native {
		t.Fatalf("重開後那題要當 native 接回：%+v %v", s, err)
	}
	out := make(chan approvalResult, 1)
	go func() {
		d, decided, err := f.m.RequestApproval(context.Background(), q)
		if err != nil {
			t.Error(err)
		}
		out <- approvalResult{d, decided}
	}()
	var p *sdk.Pending
	for deadline := time.Now().Add(3 * time.Second); p == nil; time.Sleep(5 * time.Millisecond) {
		if s, err := f.m.Get(context.Background(), "code"); err == nil && s.Pending != nil && !s.Pending.Native {
			p = s.Pending // 引擎再問的那題取代 native、在看板等
		}
		if time.Now().After(deadline) {
			t.Fatal("引擎再問的那題沒有取代 native")
		}
	}
	f.answer(t, p, &sdk.Decision{Allow: true, Answers: map[string]string{"選哪個": "B"}})
	if r := result(t, out); !r.decided || r.decision.Answers["選哪個"] != "B" {
		t.Fatalf("看板答的要傳回去：%+v", r)
	}
}

// 引擎那邊的請求被取消（部署時請求的 ctx 跟著停機先取消、核心的還沒）：一樣回 unavailable，確認留在畫面上（SES-66）。
func TestApprovalRequestCanceledReturnsUnavailable(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	ctx, cancel := context.WithCancel(context.Background())
	errs := make(chan error, 1)
	go func() {
		_, _, err := f.m.RequestApproval(ctx, &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "選哪個", Options: []sdk.QuestionOption{{Label: "A"}}}}})
		errs <- err
	}()
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if s, err := f.m.Get(context.Background(), "code"); err == nil && s.Pending != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending approval")
		}
	}
	cancel()
	var pe *platform.Err
	if err := <-errs; !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeUnavailable {
		t.Fatalf("要回 unavailable，實際 %v", err)
	}
	if s, _ := f.m.Get(context.Background(), "code"); s.Pending == nil || !s.Pending.Native {
		t.Fatalf("確認要留在畫面上：%+v", s.Pending)
	}
}

// 看板正在請引擎代按畫面上那個確認時，引擎又問一次：不取代（回 conflict），免得代按完多一張按過的卡（SES-66）。
func TestApprovalDoesNotReplaceNativeBeingAnswered(t *testing.T) {
	f := setup(t)
	f.open(t, "code")
	f.m.mu.Lock()
	e := f.m.entries["code"]
	e.session.Pending = &sdk.Pending{ID: "old", Kind: sdk.PendingKindPermission, Summary: "寫入"}
	f.m.restoreNative(e)
	f.m.approvals["code"].answering = true
	f.m.mu.Unlock()
	_, _, err := f.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	var pe *platform.Err
	if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeConflict {
		t.Fatalf("要回 conflict，實際 %v", err)
	}
	if s, _ := f.m.Get(context.Background(), "code"); s.Pending == nil || s.Pending.ID != "old" {
		t.Fatalf("畫面上那個要留著：%+v", s.Pending)
	}
}
