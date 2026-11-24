package chat

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

func waitSocket(t *testing.T, sock string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if conn, err := net.Dial("unix", sock); err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never accepted a connection")
}

// run 用這個業務模組自己的指令表跑一個指令（跟 EverGenie 一樣經 cmdline.Run）。
func run(t *testing.T, c *sdk.Client, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, Commands(), "用法\n")
	return code, out.String(), errOut.String()
}

func mustRun(t *testing.T, c *sdk.Client, args ...string) string {
	t.Helper()
	code, out, errOut := run(t, c, "", args...)
	if code != 0 {
		t.Fatalf("%v → %d: %s", args, code, errOut)
	}
	return out
}

func TestCommandsListsAllChatCommands(t *testing.T) {
	var _ map[string]app.Command = Commands()
	want := []string{"sessions list", "sessions opened", "sessions open", "sessions close", "sessions start", "sessions stop", "sessions restart",
		"sessions rotate", "sessions peek", "sessions answer", "sessions attach", "status", "say", "voice answer"}
	got := Commands()
	if len(got) != len(want) {
		t.Fatalf("指令數 %d", len(got))
	}
	for _, name := range want {
		if got[name].Run == nil {
			t.Errorf("缺 %s", name)
		}
	}
}

type fakeVault struct{ sdk.VaultAPI }

func (fakeVault) List(_ context.Context, r *sdk.VaultListRequest) (*sdk.VaultListResponse, error) {
	return &sdk.VaultListResponse{Records: []sdk.Record{{Kind: "agent", ID: "code"}, {Kind: "agent", ID: "life"}}}, nil
}

// sayBus 記下 say 送來的封包。
type sayBus struct {
	sdk.BusAPI
	mu   sync.Mutex
	sent []*sdk.BusSendRequest
}

func (b *sayBus) Send(_ context.Context, r *sdk.BusSendRequest) (*sdk.BusSendResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, r)
	return &sdk.BusSendResponse{Envelope: &sdk.Envelope{ID: "E-1", From: r.From, To: r.To, Body: r.Body}}, nil
}

// say 把字接成一句、種類是 say；操作者來自 $EVERGENIE_AGENT（沒有是 owner(cli)）；缺內容是用法錯誤。
// Guards: REQ-14, REQ-57
func TestSayJoinsWordsAndSendsSayKind(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	bus := &sayBus{}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"bus": bus}) }()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	waitSocket(t, sock)
	c := sdk.Dial(sock)

	t.Setenv("EVERGENIE_AGENT", "life")
	out := mustRun(t, c, "say", "code", "請", "幫我", "看一下")
	req := bus.sent[0]
	if req.To != "code" || req.From != "life" || req.Kind != sdk.EnvelopeKindSay || req.Body != "請 幫我 看一下" {
		t.Fatalf("say request: %+v", req)
	}
	if !strings.Contains(out, `"id":"E-1"`) {
		t.Fatalf("say should print the envelope: %q", out)
	}
	t.Setenv("EVERGENIE_AGENT", "")
	mustRun(t, c, "say", "code", "嗨")
	if bus.sent[1].From != "owner(cli)" {
		t.Fatalf("without EVERGENIE_AGENT the actor is owner(cli): %+v", bus.sent[1])
	}
	if code, _, _ := run(t, c, "", "say", "code"); code != 2 {
		t.Fatalf("say 缺內容要是用法錯誤：%d", code)
	}
}

// fakeSessionStore 是有狀態的假 session 服務：開、關、重開會改記憶體裡的清單，像 fake 引擎接在 session 模組後面。
type fakeSessionStore struct {
	sdk.SessionAPI
	mu       sync.Mutex
	sessions map[string]*sdk.Session
	calls    []string
	resolved *sdk.SessionResolveRequest
}

func (s *fakeSessionStore) log(c string) { s.calls = append(s.calls, c) }

func (s *fakeSessionStore) List(_ context.Context, r *sdk.SessionListRequest) (*sdk.SessionListResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := &sdk.SessionListResponse{}
	for _, v := range s.sessions {
		if r.Agent == "" || v.Agent == r.Agent {
			out.Sessions = append(out.Sessions, *v)
		}
	}
	return out, nil
}
func (s *fakeSessionStore) Get(_ context.Context, r *sdk.SessionGetRequest) (*sdk.SessionGetResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.sessions[r.Name]
	if !ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到這個對話"}
	}
	c := *v
	return &sdk.SessionGetResponse{Session: &c}, nil
}
func (s *fakeSessionStore) Open(_ context.Context, r *sdk.SessionOpenRequest) (*sdk.SessionOpenResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := r.Name
	if name == "" {
		name = r.Agent
	}
	s.log("open " + name + " " + string(r.Kind) + " " + r.Cwd + " " + r.Topic)
	v, ok := s.sessions[name]
	if !ok {
		v = &sdk.Session{Name: name, Agent: r.Agent, Kind: r.Kind, Cwd: r.Cwd, Account: "main"}
		s.sessions[name] = v
	}
	v.State, v.Attach = sdk.SessionStateIdle, "fake-attach "+name
	c := *v
	return &sdk.SessionOpenResponse{Session: &c}, nil
}
func (s *fakeSessionStore) Close(_ context.Context, r *sdk.SessionCloseRequest) (*sdk.SessionCloseResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("close " + r.Name)
	v, ok := s.sessions[r.Name]
	if !ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到這個對話"}
	}
	v.State = sdk.SessionStateStopped
	return &sdk.SessionCloseResponse{}, nil
}
func (s *fakeSessionStore) Rotate(_ context.Context, r *sdk.SessionRotateRequest) (*sdk.SessionRotateResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log("rotate " + r.Name)
	v := s.sessions[r.Name]
	c := *v
	return &sdk.SessionRotateResponse{Session: &c}, nil
}
func (s *fakeSessionStore) Chat(_ context.Context, r *sdk.SessionChatRequest, send func(*sdk.SessionChatResponse) error) error {
	at := time.Date(2026, 10, 25, 9, 0, 0, 0, time.Local)
	for i, m := range []sdk.ChatMessage{{Kind: sdk.ChatKindUser, Text: "早安"}, {Kind: sdk.ChatKindAssistant, Text: "早安，今天有三件事"}, {Kind: sdk.ChatKindToolCall, Tool: "Bash", Text: "ls"}} {
		m.Ts = at.Add(time.Duration(i) * time.Minute)
		if err := send(&sdk.SessionChatResponse{Message: &m}); err != nil {
			return err
		}
	}
	return nil
}
func (s *fakeSessionStore) Resolve(_ context.Context, r *sdk.SessionResolveRequest) (*sdk.SessionResolveResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolved = r
	s.sessions[r.Session].Pending = nil
	return &sdk.SessionResolveResponse{Applied: true}, nil
}

func serveSessions(t *testing.T) (*sdk.Client, *fakeSessionStore) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeSessionStore{sessions: map[string]*sdk.Session{
		"code": {Name: "code", Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionStateBusy, Account: "main"},
		"life": {Name: "life", Agent: "life", Kind: sdk.SessionKindResident, State: sdk.SessionStateWaiting, Account: "main",
			Pending: &sdk.Pending{ID: "p-1", Kind: sdk.PendingKindPermission, Tool: "Bash", Summary: "刪掉暫存檔"}},
	}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"session": store, "vault": fakeVault{}}) }()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	c := sdk.Dial(sock)
	waitSocket(t, sock)
	return c, store
}

// Guards: REQ-72, REQ-74, REQ-80
func TestSessionsCLIOnFakeEngine(t *testing.T) {
	c, store := serveSessions(t)
	repo := t.TempDir()

	if out := mustRun(t, c, "status"); !strings.Contains(out, "code") || !strings.Contains(out, "忙碌中") || !strings.Contains(out, "等你處理（Bash：刪掉暫存檔）") {
		t.Fatalf("status：\n%s", out)
	}
	if out := mustRun(t, c, "sessions", "list", "--agent", "code"); strings.Contains(out, "life") || !strings.Contains(out, "code") {
		t.Fatalf("list --agent：\n%s", out)
	}

	out := mustRun(t, c, "sessions", "open", "code", "--repo", repo, "--topic", "Fix Login")
	if !strings.Contains(out, "已開 code-fix-login") || !strings.Contains(out, "接上畫面：fake-attach code-fix-login") {
		t.Fatalf("open：\n%s", out)
	}
	if got := store.sessions["code-fix-login"]; got.Kind != sdk.SessionKindRoom || got.Cwd != repo {
		t.Fatalf("open 要帶 room 與資料夾：%+v", got)
	}
	if code, _, errOut := run(t, c, "", "sessions", "open", "code", "--topic", "健身"); code != 2 || !strings.Contains(errOut, "--slug") {
		t.Fatalf("中文主題要求 --slug：%d %s", code, errOut)
	}
	mustRun(t, c, "sessions", "open", "life", "--topic", "健身", "--slug", "fitness")
	if code, _, _ := run(t, c, "", "sessions", "open", "code", "--site", "home"); code != 2 {
		t.Fatal("別台電腦要等 P7")
	}
	if out := mustRun(t, c, "sessions", "opened"); !strings.Contains(out, "code-fix-login") || !strings.Contains(out, "life-fitness") || strings.Contains(out, "code       resident") {
		t.Fatalf("opened 只列專屬對話：\n%s", out)
	}
	if out := mustRun(t, c, "sessions", "attach", "code-fix-login"); out != "fake-attach code-fix-login\n" {
		t.Fatalf("attach：%q", out)
	}
	mustRun(t, c, "sessions", "close", "code-fix-login")
	mustRun(t, c, "sessions", "open", "code", "--resume", "code-fix-login")

	if out := mustRun(t, c, "sessions", "peek", "code", "--lines", "2"); strings.Contains(out, "你：早安\n") || !strings.Contains(out, "秘書：早安，今天有三件事") || !strings.Contains(out, "工具：Bash ls") {
		t.Fatalf("peek 只留最後兩則：\n%s", out)
	}

	if code, _, _ := run(t, c, "", "sessions", "answer", "life"); code != 2 {
		t.Fatal("answer 要選 --allow／--deny／--answer 其中一個")
	}
	if out := mustRun(t, c, "sessions", "answer", "life", "--deny", "--message", "先不要"); !strings.Contains(out, "已回答 life") {
		t.Fatalf("answer：\n%s", out)
	}
	if r := store.resolved; r.PendingID != "p-1" || r.Decision.Allow || r.Decision.Message != "先不要" || r.By.Name != "owner(cli)" {
		t.Fatalf("resolve：%+v %+v", r, r.Decision)
	}
	if code, _, errOut := run(t, c, "", "sessions", "answer", "life", "--allow"); code != 1 || !strings.Contains(errOut, "沒有在等人") {
		t.Fatalf("沒在等時要說明：%d %s", code, errOut)
	}

	mustRun(t, c, "sessions", "stop", "code")
	mustRun(t, c, "sessions", "start", "code")
	mustRun(t, c, "sessions", "restart", "life")
	out = mustRun(t, c, "sessions", "rotate", "--all")
	if !strings.Contains(out, "已重開 code") || !strings.Contains(out, "已重開 life") || strings.Contains(out, "fitness") {
		t.Fatalf("rotate --all 只重開在跑的常駐：\n%s", out)
	}
	want := []string{"close code", "open code resident", "close life", "open life resident", "rotate code", "rotate life"}
	got := strings.Join(store.calls, "|")
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Fatalf("呼叫紀錄缺 %q：%s", w, got)
		}
	}
	if code, _, _ := run(t, c, "", "sessions", "close"); code != 2 {
		t.Fatal("close 要名稱")
	}
}
