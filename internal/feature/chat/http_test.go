package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// rec 記下收到的呼叫與最後一次的操作者；err 不是 nil 時所有呼叫都回它。
type rec struct {
	mu    sync.Mutex
	calls []string
	by    string
	err   error
}

func (r *rec) rec(format string, a ...any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, fmt.Sprintf(format, a...))
	return r.err
}
func (r *rec) setBy(by string) { r.mu.Lock(); r.by = by; r.mu.Unlock() }
func (r *rec) lastBy() string  { r.mu.Lock(); defer r.mu.Unlock(); return r.by }
func (r *rec) has(prefix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func closed[T any]() <-chan T { ch := make(chan T); close(ch); return ch }

type fakeBus struct {
	*rec
	sent []*sdk.Envelope
}

func (f *fakeBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	f.mu.Lock()
	f.sent = append(f.sent, e)
	f.mu.Unlock()
	return e, f.rec.rec("bus.send %s %s", e.To, e.Kind)
}
func (f *fakeBus) Get(_ context.Context, id string) (*sdk.Envelope, error) {
	return &sdk.Envelope{ID: id}, f.rec.rec("bus.get %s", id)
}
func (f *fakeBus) List(_ context.Context, to string, _ ...sdk.Lifecycle) ([]*sdk.Envelope, error) {
	return nil, f.rec.rec("bus.list %s", to)
}
func (f *fakeBus) Watch(context.Context, string, string) (<-chan *sdk.BusWatchResponse, error) {
	return closed[*sdk.BusWatchResponse](), f.rec.rec("bus.watch")
}
func (f *fakeBus) Publish(_ context.Context, topic string, _ map[string]any) error {
	return f.rec.rec("bus.publish %s", topic)
}
func (f *fakeBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return closed[*sdk.BusSubscribeResponse](), f.rec.rec("bus.subscribe")
}

type fakeTasks struct {
	*rec
	task *sdk.Task
}

func (f *fakeTasks) Get(_ context.Context, id string) (*sdk.Task, error) {
	if err := f.rec.rec("tasks.get %s", id); err != nil {
		return nil, err
	}
	if f.task != nil {
		return f.task, nil
	}
	return &sdk.Task{ID: id, Agent: "code"}, nil
}

type workCall struct{ id, agent, by string }

type fakeSessions struct {
	*rec
	existing *sdk.Session
	applied  bool
	opened   []*sdk.SessionOpenRequest
	works    []workCall
}

func (f *fakeSessions) List(context.Context, string, []sdk.SessionState) ([]*sdk.Session, error) {
	return nil, f.rec.rec("session.list")
}
func (f *fakeSessions) Get(_ context.Context, name string) (*sdk.Session, error) {
	if err := f.rec.rec("session.get %s", name); err != nil {
		return nil, err
	}
	if f.existing == nil {
		return nil, platform.NotFound("找不到這個對話")
	}
	return f.existing, nil
}
func (f *fakeSessions) Open(_ context.Context, spec *sdk.SessionOpenRequest) (*sdk.Session, error) {
	f.mu.Lock()
	f.opened = append(f.opened, spec)
	f.mu.Unlock()
	return &sdk.Session{Name: spec.Name, Agent: spec.Agent, State: sdk.SessionStateStarting}, f.rec.rec("session.open %s", spec.Name)
}
func (f *fakeSessions) OpenWork(_ context.Context, id, agent string, by *sdk.Actor) (*sdk.Session, error) {
	c := workCall{id: id, agent: agent}
	if by != nil {
		c.by = by.Name
	}
	f.mu.Lock()
	f.works = append(f.works, c)
	f.mu.Unlock()
	prefix, num, _ := strings.Cut(id, "-")
	return &sdk.Session{Name: agent + "-" + strings.ToLower(prefix) + "-" + num, Agent: agent, Kind: sdk.SessionKindWork, State: sdk.SessionStateStarting}, f.rec.rec("session.open_work %s", id)
}
func (f *fakeSessions) Close(_ context.Context, name string) error {
	return f.rec.rec("session.close %s", name)
}
func (f *fakeSessions) Rotate(_ context.Context, name string) error {
	return f.rec.rec("session.rotate %s", name)
}
func (f *fakeSessions) Resolve(_ context.Context, name, pending string, d *sdk.Decision, by string) (bool, error) {
	f.setBy(by)
	return f.applied, f.rec.rec("session.resolve %s %s allow=%v", name, pending, d.Allow)
}
func (f *fakeSessions) Chat(_ context.Context, name string, _ string, _ bool) (<-chan *sdk.ChatMessage, error) {
	return closed[*sdk.ChatMessage](), f.rec.rec("session.chat %s", name)
}
func (f *fakeSessions) Watch(context.Context, string) (<-chan *sdk.SessionWatchResponse, error) {
	return closed[*sdk.SessionWatchResponse](), f.rec.rec("session.watch")
}
func (f *fakeSessions) Threads(_ context.Context, agent string, _ bool) ([]*sdk.ThreadSummary, int, error) {
	return []*sdk.ThreadSummary{{Name: "code"}}, 1, f.rec.rec("session.threads %s", agent)
}
func (f *fakeSessions) WatchThreads(context.Context, string) (<-chan *sdk.SessionWatchThreadsResponse, error) {
	return closed[*sdk.SessionWatchThreadsResponse](), f.rec.rec("session.watch_threads")
}
func (f *fakeSessions) MarkSeen(_ context.Context, name, _, by string) (*sdk.ThreadSummary, error) {
	f.setBy(by)
	return &sdk.ThreadSummary{Name: name}, f.rec.rec("session.mark_seen %s", name)
}
func (f *fakeSessions) Command(_ context.Context, name string, cmd sdk.SessionCommand, arg, by string) (*sdk.SessionCommandNote, error) {
	f.setBy(by)
	return &sdk.SessionCommandNote{Command: cmd, Arg: arg, By: by}, f.rec.rec("session.command %s %s %s", name, cmd, arg)
}
func (f *fakeSessions) EngineHost() sdk.EngineHostAPI { return fakeHost{f.rec} }

type fakeHost struct{ *rec }

func (h fakeHost) ReportEvent(context.Context, *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	return &sdk.EngineHostReportEventResponse{}, h.rec.rec("host.report_event")
}
func (h fakeHost) RequestApproval(_ context.Context, r *sdk.EngineHostRequestApprovalRequest) (*sdk.EngineHostRequestApprovalResponse, error) {
	return &sdk.EngineHostRequestApprovalResponse{}, h.rec.rec("host.request_approval max=%d", r.MaxWaitSeconds)
}
func (h fakeHost) GetContext(context.Context, *sdk.EngineHostGetContextRequest) (*sdk.EngineHostGetContextResponse, error) {
	return &sdk.EngineHostGetContextResponse{}, h.rec.rec("host.get_context")
}
func (h fakeHost) CheckAction(context.Context, *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	return &sdk.EngineHostCheckActionResponse{}, h.rec.rec("host.check_action")
}

type stack struct {
	r        *rec
	bus      *fakeBus
	tasks    *fakeTasks
	sessions *fakeSessions
}

func newStack() (*Module, *stack) {
	r := &rec{}
	s := &stack{r: r, bus: &fakeBus{rec: r}, tasks: &fakeTasks{rec: r}, sessions: &fakeSessions{rec: r}}
	return New(Deps{Sessions: s.sessions, Bus: s.bus, Tasks: s.tasks}), s
}

// 用 app 的方式把 Routes() 掛起來，從 HTTP 打進真的模組。
func serve(t *testing.T, m *Module) http.Handler {
	t.Helper()
	mux, err := app.New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	return mux
}

func post(t *testing.T, h http.Handler, path string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rr.Code, rr.Body.Bytes()
}

func decodeError(t *testing.T, raw []byte) sdk.Error {
	t.Helper()
	var e sdk.Error
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatalf("not an error body: %s", raw)
	}
	return e
}

// 模組實作 RouteProvider，登記 bus、session、engine_host 三個 sdk 服務；簽名不符 sdk 介面的話 Mux 會回錯。
func TestRoutesRegisterChatServices(t *testing.T) {
	m, _ := newStack()
	var got []string
	for _, mt := range m.Routes() {
		if mt.Impl == nil {
			t.Fatalf("%s 沒有實作", mt.Service)
		}
		got = append(got, mt.Service)
	}
	if strings.Join(got, ",") != "bus,session,engine_host,web,web" { // web 兩組：啟動提示、語音入口（CHT-9）
		t.Fatalf("Routes = %v", got)
	}
	serve(t, m)
}

// 會寫東西的呼叫都要帶操作者：沒帶 → 400、欄位 by、底層沒被叫到；帶了就把名稱交給底層。
func TestByRequired(t *testing.T) {
	m, s := newStack()
	h := serve(t, m)
	routes := map[string]func(by *sdk.Actor) any{
		"/api/session/resolve": func(by *sdk.Actor) any {
			return &sdk.SessionResolveRequest{Session: "code", PendingID: "P-1", Decision: &sdk.Decision{}, By: by}
		},
		"/api/session/mark_seen": func(by *sdk.Actor) any { return &sdk.SessionMarkSeenRequest{Session: "code", By: by} },
		"/api/session/command": func(by *sdk.Actor) any {
			return &sdk.SessionCommandRequest{Session: "code", Command: sdk.SessionCommandClear, By: by}
		},
		"/api/bus/publish": func(by *sdk.Actor) any { return &sdk.BusPublishRequest{Topic: "x.y", By: by} },
	}
	for path, build := range routes {
		for _, missing := range []*sdk.Actor{nil, {}, {Name: "  "}} {
			status, body := post(t, h, path, build(missing))
			if e := decodeError(t, body); status != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
				t.Errorf("%s without by: %d %s", path, status, body)
			}
		}
	}
	if n := len(s.r.calls); n != 0 {
		t.Fatalf("calls before by was valid: %d %v", n, s.r.calls)
	}
	for path, build := range routes {
		s.r.setBy("")
		if status, body := post(t, h, path, build(&sdk.Actor{Name: "code"})); status != 200 {
			t.Errorf("%s with by: %d %s", path, status, body)
		} else if strings.HasPrefix(path, "/api/session/") && s.r.lastBy() != "code" {
			t.Errorf("%s did not pass the actor name: %q", path, s.r.lastBy())
		}
	}
}

// 單號的 topic 交給 session 的 OpenWork（組第一句、判斷舊 session 的邏輯在 session 模組）；其他 topic 照舊直接開；找不到的單不開 session。
// Guards: REQ-56, REQ-268, REQ-270
func TestOpenTaskRoomDelegatesToOpenWork(t *testing.T) {
	m, s := newStack()
	h := serve(t, m)
	status, body := post(t, h, "/api/session/open", &sdk.SessionOpenRequest{Agent: "code", Topic: "SEC-7", By: &sdk.Actor{Name: "owner(web)"}})
	if status != 200 {
		t.Fatalf("%d %s", status, body)
	}
	if len(s.sessions.works) != 1 || s.sessions.works[0].id != "SEC-7" || s.sessions.works[0].agent != "code" || s.sessions.works[0].by != "owner(web)" {
		t.Fatalf("works %+v", s.sessions.works)
	}
	if len(s.sessions.opened) != 0 || len(s.bus.sent) != 0 {
		t.Fatalf("不自己開、也不自己組第一句：%v", s.r.calls)
	}

	// 沒給秘書：用單上的負責秘書。
	m3, s3 := newStack()
	s3.tasks.task = &sdk.Task{ID: "SEC-9", Agent: "work"}
	if status, body = post(t, serve(t, m3), "/api/session/open", &sdk.SessionOpenRequest{Topic: "SEC-9"}); status != 200 || len(s3.sessions.works) != 1 || s3.sessions.works[0].agent != "work" {
		t.Fatalf("%d %s %+v", status, body, s3.sessions.works)
	}

	// 其他 topic 直接開。
	m4, s4 := newStack()
	if status, _ = post(t, serve(t, m4), "/api/session/open", &sdk.SessionOpenRequest{Name: "code-c1", Agent: "code", Topic: "整理桌面"}); status != 200 {
		t.Fatal(status)
	}
	if len(s4.bus.sent) != 0 || len(s4.sessions.works) != 0 || s4.sessions.opened[0].Name != "code-c1" {
		t.Fatalf("plain open: %v", s4.r.calls)
	}

	// 找不到的單不開 session。
	m5, s5 := newStack()
	s5.r.err = platform.NotFound("找不到這張單")
	if status, _ = post(t, serve(t, m5), "/api/session/open", &sdk.SessionOpenRequest{Agent: "code", Topic: "SEC-404"}); status != 404 {
		t.Fatal(status)
	}
}

// 授權回答：帶 by 與決定才轉給 session；沒有決定回 400（欄位 decision）。
// Guards: REQ-30
func TestResolveWiring(t *testing.T) {
	m, s := newStack()
	s.sessions.applied = true
	h := serve(t, m)
	status, body := post(t, h, "/api/session/resolve", &sdk.SessionResolveRequest{Session: "code", PendingID: "P-1", Decision: &sdk.Decision{Allow: true}, By: &sdk.Actor{Name: "owner(web)"}})
	var resolved sdk.SessionResolveResponse
	if err := json.Unmarshal(body, &resolved); err != nil || status != 200 || !resolved.Applied {
		t.Fatalf("%d %s", status, body)
	}
	if !s.r.has("session.resolve code P-1 allow=true") || s.r.lastBy() != "owner(web)" {
		t.Fatalf("resolve not wired: %v", s.r.calls)
	}
	if status, body = post(t, h, "/api/session/resolve", &sdk.SessionResolveRequest{Session: "code", PendingID: "P-1", By: &sdk.Actor{Name: "owner(web)"}}); status != 400 || decodeError(t, body).Field != "decision" {
		t.Fatalf("missing decision: %d %s", status, body)
	}
}

// 一般呼叫與串流都原樣轉給底層；串流在底層的 channel 關了就結束；錯誤轉成白話的 API 錯誤。
func TestAdaptersPassThrough(t *testing.T) {
	m, s := newStack()
	s.sessions.existing = &sdk.Session{Name: "code"}
	h := serve(t, m)
	for path, req := range map[string]any{
		"/api/bus/send":                     &sdk.BusSendRequest{From: "owner(web)", To: "code", Kind: sdk.EnvelopeKindChat, Body: "嗨"},
		"/api/bus/get":                      &sdk.BusGetRequest{ID: "e1"},
		"/api/bus/list":                     &sdk.BusListRequest{To: "code"},
		"/api/session/list":                 &sdk.SessionListRequest{},
		"/api/session/get":                  &sdk.SessionGetRequest{Name: "code"},
		"/api/session/close":                &sdk.SessionCloseRequest{Name: "code"},
		"/api/session/rotate":               &sdk.SessionRotateRequest{Name: "code"},
		"/api/engine_host/request_approval": &sdk.EngineHostRequestApprovalRequest{MaxWaitSeconds: 1},
		"/api/bus/watch":                    &sdk.BusWatchRequest{},
		"/api/bus/subscribe":                &sdk.BusSubscribeRequest{Topics: []string{"task.*"}},
		"/api/session/chat":                 &sdk.SessionChatRequest{Session: "code"},
		"/api/session/watch":                &sdk.SessionWatchRequest{},
		"/api/session/watch_threads":        &sdk.SessionWatchThreadsRequest{},
	} {
		if status, body := post(t, h, path, req); status != 200 {
			t.Errorf("%s: %d %s", path, status, body)
		}
	}
	for _, want := range []string{"bus.send code chat", "bus.get e1", "session.rotate code", "host.request_approval max=1", "session.watch"} {
		if !s.r.has(want) {
			t.Errorf("missing call %q in %v", want, s.r.calls)
		}
	}
	var threads sdk.SessionListThreadsResponse
	_, body := post(t, h, "/api/session/list_threads", &sdk.SessionListThreadsRequest{})
	if err := json.Unmarshal(body, &threads); err != nil || threads.WaitingUser != 1 || len(threads.Threads) != 1 {
		t.Fatalf("list_threads: %s", body)
	}
	m2, _ := newStack()
	status, body := post(t, serve(t, m2), "/api/session/get", &sdk.SessionGetRequest{Name: "nobody"})
	if e := decodeError(t, body); status != 404 || e.Code != sdk.ErrorCodeNotFound || e.Message == "" {
		t.Fatalf("找不到：%d %s", status, body)
	}
}

// 底層 session 沒接上時，引擎回報回 unavailable，不 panic。
func TestHostUnavailableWithoutSessions(t *testing.T) {
	h := serve(t, New(Deps{}))
	status, body := post(t, h, "/api/engine_host/get_context", &sdk.EngineHostGetContextRequest{})
	if e := decodeError(t, body); status != 503 || e.Code != sdk.ErrorCodeUnavailable {
		t.Fatalf("沒接 session：%d %s", status, body)
	}
}

type sampleLatency struct{ took []time.Duration }

func (l *sampleLatency) Observe(_ time.Time, took time.Duration) { l.took = append(l.took, took) }

// 狀態延遲：連上時的現況不算；事件時間往前走才算一筆（同一個事件的後續推送不重複算）；每個 session 分開算；樣本交給 m.latency（正式是自己的 latencyLog）。
// Guards: REQ-390
func TestStateLatencyFromEngineEventToStream(t *testing.T) {
	clock := platform.NewFakeClock(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	l := &sampleLatency{}
	m := New(Deps{Clock: clock})
	m.latency = l
	st := m.latencyStream()
	t0 := clock.Now()
	st.sent(&sdk.Session{Name: "code", LastEventAt: t0.Add(-time.Hour)}) // 連上時的現況
	st.sent(&sdk.Session{Name: "work", LastEventAt: t0.Add(-time.Hour)}) // 另一個 session 的現況也只記下
	if len(l.took) != 0 {
		t.Fatalf("連上時的現況不算：%v", l.took)
	}
	for i, took := range []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 5 * time.Second} {
		clock.Advance(time.Minute)
		ev := clock.Now().Add(-took)
		st.sent(&sdk.Session{Name: "code", LastEventAt: ev})
		st.sent(&sdk.Session{Name: "code", LastEventAt: ev}) // 同一個事件之後的變化不重複算
		if len(l.took) != i+1 || l.took[i] != took {
			t.Fatalf("第 %d 個事件：%v", i+1, l.took)
		}
	}
	clock.Advance(time.Minute)
	st.sent(&sdk.Session{Name: "code", LastEventAt: clock.Now().Add(50 * time.Millisecond)}) // 事件時間比現在晚一點（時鐘誤差）
	if len(l.took) != 4 || l.took[3] >= 0 {
		t.Fatalf("負的原樣交出（api 那邊當 0）：%v", l.took)
	}
	// 沒有收樣本的就什麼都不量，也不 panic。
	none := New(Deps{})
	none.latency = nil
	none.latencyStream().sent(&sdk.Session{Name: "code", LastEventAt: t0})
}
