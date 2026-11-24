package roster

import (
	"bytes"
	"context"
	"fmt"
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
	code := cmdline.Run(context.Background(), args, c, cmdline.IO{In: strings.NewReader(stdin), Out: &out, Err: &errOut}, Commands(nil), "用法\n")
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

func TestCommandsListsAllAgentCommands(t *testing.T) {
	var _ map[string]app.Command = Commands(nil)
	want := []string{"agents list", "agents new", "agents rm", "agents mv", "agents assign", "agents swap", "agents account", "agents set", "agents link", "agents knowledge",
		"customize add", "customize show", "memory show", "memory add", "memory write", "memory note", "journal add", "journal show", "roster"}
	got := Commands(nil)
	if len(got) != len(want) {
		t.Fatalf("指令數 %d", len(got))
	}
	for _, name := range want {
		if got[name].Run == nil {
			t.Errorf("缺 %s", name)
		}
	}
}

// fakeAgents 是有狀態的假名冊服務：只記下 CLI 送來的請求、改記憶體裡的名冊，不做名冊模組的驗證。
type fakeAgents struct {
	sdk.AgentsAPI
	mu      sync.Mutex
	rows    map[string]*sdk.Record
	calls   []string
	rules   map[string]string
	applied bool
	added   []sdk.AgentsAddRuleRequest // 送出的 AddRule 請求（看 Section 與規則原文分開）
}

func (a *fakeAgents) log(format string, args ...any) {
	a.calls = append(a.calls, fmt.Sprintf(format, args...))
}

func (a *fakeAgents) Create(_ context.Context, r *sdk.AgentsCreateRequest) (*sdk.AgentsCreateResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.rows[r.ID]; ok {
		return nil, &sdk.Error{Code: sdk.ErrorCodeConflict, Message: "已經有這位秘書"}
	}
	a.log("create %s %s by=%s", r.ID, r.Persona, r.By.Name)
	a.rows[r.ID] = &sdk.Record{Kind: "agent", ID: r.ID, Fields: map[string]any{"persona": r.Persona, "display_name": r.DisplayName, "enabled": false}}
	return &sdk.AgentsCreateResponse{Agent: a.rows[r.ID]}, nil
}
func (a *fakeAgents) Archive(_ context.Context, r *sdk.AgentsArchiveRequest) (*sdk.AgentsArchiveResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("archive %s", r.ID)
	delete(a.rows, r.ID)
	return &sdk.AgentsArchiveResponse{Archived: []string{"agent/" + r.ID, "memory/" + r.ID}, Restore: "把 archive 搬回上一層"}, nil
}
func (a *fakeAgents) Rename(_ context.Context, r *sdk.AgentsRenameRequest) (*sdk.AgentsRenameResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("rename %s %s", r.ID, r.NewID)
	row := a.rows[r.ID]
	delete(a.rows, r.ID)
	row.ID = r.NewID
	a.rows[r.NewID] = row
	return &sdk.AgentsRenameResponse{Agent: row, Moved: []string{"agent/" + r.NewID, "memory/" + r.NewID}, Mentions: []string{"home.toml"}}, nil
}
func (a *fakeAgents) Assign(_ context.Context, r *sdk.AgentsAssignRequest) (*sdk.AgentsAssignResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("assign %s %s", r.ID, r.Persona)
	a.rows[r.ID].Fields["persona"] = r.Persona
	return &sdk.AgentsAssignResponse{Agent: a.rows[r.ID]}, nil
}
func (a *fakeAgents) Swap(_ context.Context, r *sdk.AgentsSwapRequest) (*sdk.AgentsSwapResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("swap %s %s", r.A, r.B)
	x, y := a.rows[r.A], a.rows[r.B]
	x.Fields["persona"], y.Fields["persona"] = y.Fields["persona"], x.Fields["persona"]
	return &sdk.AgentsSwapResponse{Agents: []sdk.Record{*x, *y}}, nil
}
func (a *fakeAgents) SetAccount(_ context.Context, r *sdk.AgentsSetAccountRequest) (*sdk.AgentsSetAccountResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("account %s %s %s %s", r.ID, r.Account, r.Model, r.Effort)
	a.rows[r.ID].Fields["account"] = r.Account
	return &sdk.AgentsSetAccountResponse{Agent: a.rows[r.ID]}, nil
}
func (a *fakeAgents) SetField(_ context.Context, r *sdk.AgentsSetFieldRequest) (*sdk.AgentsSetFieldResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("set %s %s=%s", r.ID, r.Field, r.Value)
	a.rows[r.ID].Fields[r.Field] = r.Value
	return &sdk.AgentsSetFieldResponse{Agent: a.rows[r.ID]}, nil
}
func (a *fakeAgents) Knowledge(_ context.Context, r *sdk.AgentsKnowledgeRequest) (*sdk.AgentsKnowledgeResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("knowledge %s", r.Agent)
	if r.Agent != "life" {
		return &sdk.AgentsKnowledgeResponse{Agent: r.Agent, Blocks: []sdk.AgentKnowledgeBlock{}}, nil
	}
	return &sdk.AgentsKnowledgeResponse{Agent: r.Agent, Blocks: []sdk.AgentKnowledgeBlock{{ID: "parenting", Title: "育兒", Summary: "依年齡給建議", Enabled: false}, {ID: "balance", Title: "生活管理", Default: true, Enabled: true}}}, nil
}
func (a *fakeAgents) SetKnowledge(_ context.Context, r *sdk.AgentsSetKnowledgeRequest) (*sdk.AgentsKnowledgeResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.log("set_knowledge %s [%s] by=%s", r.Agent, strings.Join(r.Enabled, ","), r.By.Name)
	return &sdk.AgentsKnowledgeResponse{Agent: r.Agent, Blocks: []sdk.AgentKnowledgeBlock{{ID: "parenting", Title: "育兒", Enabled: true}}}, nil
}
func (a *fakeAgents) Link(_ context.Context, r *sdk.AgentsLinkRequest) (*sdk.AgentsLinkResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applied = a.applied || r.Apply
	return &sdk.AgentsLinkResponse{Applied: r.Apply, Accounts: []sdk.AgentLinkAccount{{Account: "main", Files: []sdk.AgentCardFile{{Agent: "code", Path: "/x/agents/code.md", Action: "create", Summary: "12 行"}, {Agent: "life", Path: "/x/agents/life.md", Action: "skip", Summary: "已有別人的檔案"}}}}}, nil
}
func (a *fakeAgents) AddRule(_ context.Context, r *sdk.AgentsAddRuleRequest) (*sdk.AgentsAddRuleResponse, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.added = append(a.added, *r)
	a.rules[r.ID] += r.Rule + "\n"
	return &sdk.AgentsAddRuleResponse{Addendum: a.rules[r.ID]}, nil
}
func (a *fakeAgents) Customizations(_ context.Context, r *sdk.AgentsCustomizationsRequest) (*sdk.AgentsCustomizationsResponse, error) {
	return &sdk.AgentsCustomizationsResponse{Duties: "官方職責", Addendum: a.rules[r.ID]}, nil
}

// fakeSessionStore 是有狀態的假 session 服務：開、關、重開會改記憶體裡的清單，像 fake 引擎接在 session 模組後面。
type fakeSessionStore struct {
	sdk.SessionAPI
	mu       sync.Mutex
	sessions map[string]*sdk.Session
	calls    []string
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

// Guards: REQ-162, REQ-163
func TestAgentsCLIRoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	ag := &fakeAgents{rows: map[string]*sdk.Record{
		"code": {Kind: "agent", ID: "code", Fields: map[string]any{"persona": "athena", "enabled": true}},
		"life": {Kind: "agent", ID: "life", Fields: map[string]any{"persona": "luna", "enabled": true}},
	}, rules: map[string]string{}}
	store := &fakeSessionStore{sessions: map[string]*sdk.Session{
		"code": {Name: "code", Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle, Account: "main"},
	}}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"agents": ag, "session": store}) }()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	waitSocket(t, sock)
	c := sdk.Dial(sock)
	t.Setenv("EVERGENIE_AGENT", "")

	// new：缺人設是用法錯誤；建好之後提醒還沒啟用。
	if code, _, _ := run(t, c, "", "agents", "new", "scout"); code != 2 {
		t.Fatalf("缺 --persona 要是用法錯誤：%d", code)
	}
	if out := mustRun(t, c, "agents", "new", "scout", "--persona", "athena", "--display", "偵察"); !strings.Contains(out, "已建立 scout") || !strings.Contains(out, "enabled true") {
		t.Fatalf("new：%s", out)
	}
	if code, _, errOut := run(t, c, "", "agents", "new", "scout", "--persona", "athena"); code != 1 || !strings.Contains(errOut, "已經有這位秘書") {
		t.Fatalf("重複建立要回 API 的白話訊息、結束碼 1：%d %q", code, errOut)
	}
	mustRun(t, c, "agents", "set", "scout", "enabled", "true")
	// knowledge：沒給 --enabled 只列出；給了整份換掉（--enabled= 是全部不勾，送空清單）。
	if out := mustRun(t, c, "agents", "knowledge", "life"); !strings.Contains(out, "[ ] parenting") || !strings.Contains(out, "[x] balance") || !strings.Contains(out, "預設勾") {
		t.Fatalf("knowledge 列出：%s", out)
	}
	if out := mustRun(t, c, "agents", "knowledge", "code"); !strings.Contains(out, "沒有可選知識") {
		t.Fatalf("沒有可選知識：%s", out)
	}
	mustRun(t, c, "agents", "knowledge", "life", "--enabled", "parenting, balance")
	mustRun(t, c, "agents", "knowledge", "life", "--enabled=")
	if got := strings.Join(ag.calls, "\n"); !strings.Contains(got, "set_knowledge life [parenting,balance]") || !strings.Contains(got, "set_knowledge life []") {
		t.Fatalf("knowledge --enabled 要整份送出：%s", got)
	}
	mustRun(t, c, "agents", "account", "scout", "second", "--model", "opus", "--effort", "high")
	if out := mustRun(t, c, "agents", "assign", "scout", "luna"); !strings.Contains(out, "記憶與職責沒動") {
		t.Fatalf("assign：%s", out)
	}
	if out := mustRun(t, c, "agents", "swap", "scout", "life"); !strings.Contains(out, "已互換") {
		t.Fatalf("swap：%s", out)
	}
	if ag.rows["scout"].Fields["persona"] != "luna" && ag.rows["life"].Fields["persona"] != "luna" {
		t.Fatalf("互換結果：%+v %+v", ag.rows["scout"], ag.rows["life"])
	}

	// mv：原本在跑的常駐，先停再用新名字重開；列出還提到舊名的檔案。
	out := mustRun(t, c, "agents", "mv", "code", "dev", "--display", "開發")
	if !strings.Contains(out, "home.toml") || !strings.Contains(out, "已用新名字重開") {
		t.Fatalf("mv：%s", out)
	}
	if got := strings.Join(store.calls, ";"); !strings.HasPrefix(got, "close code;open dev resident") {
		t.Fatalf("mv 要先停舊的再開新的：%s", got)
	}

	// rm：先停 session 再封存，印出怎麼還原。
	store.sessions["scout"] = &sdk.Session{Name: "scout", Agent: "scout", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle}
	store.calls = nil
	out = mustRun(t, c, "agents", "rm", "scout")
	if !strings.Contains(out, "要還原：") || strings.Join(store.calls, ";") != "close scout" {
		t.Fatalf("rm：%s %v", out, store.calls)
	}
	if _, still := ag.rows["scout"]; still {
		t.Fatal("scout 應該已封存")
	}

	// 寫入帶 by：沒有 EVERGENIE_AGENT 是 owner(cli)。
	for _, call := range ag.calls {
		if strings.HasPrefix(call, "create") && !strings.HasSuffix(call, "by=owner(cli)") {
			t.Fatalf("by：%s", call)
		}
	}

	// link：預設預演，--apply 才寫。
	out = mustRun(t, c, "agents", "link")
	if ag.applied || !strings.Contains(out, "預演") || !strings.Contains(out, "略過") {
		t.Fatalf("link 預演：%s", out)
	}
	mustRun(t, c, "agents", "link", "--apply")
	if !ag.applied {
		t.Fatal("--apply 要真的送出")
	}

	// customize
	mustRun(t, c, "customize", "add", "life", "先講結論", "再講原因")
	if out := mustRun(t, c, "customize", "show", "life"); !strings.Contains(out, "官方職責") || !strings.Contains(out, "先講結論 再講原因") {
		t.Fatalf("customize show：%s", out)
	}
	// --section 放在 id 後面或規則中間都可以，送出時是 Section，不混進規則原文（DEV-161）。
	if out := mustRun(t, c, "customize", "add", "life", "--section", "聊到什麼先看什麼", "聊到跑步", "先看鞋子"); !strings.Contains(out, "「聊到什麼先看什麼」那一段") {
		t.Fatalf("customize add --section：%s", out)
	}
	mustRun(t, c, "customize", "add", "life", "聊到游泳", "--section=聊到什麼先看什麼", "先看泳鏡")
	ag.mu.Lock()
	sent := ag.added
	ag.mu.Unlock()
	if n := len(sent); n < 3 || sent[n-3].Section != "" || sent[n-3].Rule != "先講結論 再講原因" ||
		sent[n-2].Section != "聊到什麼先看什麼" || sent[n-2].Rule != "聊到跑步 先看鞋子" ||
		sent[n-1].Section != "聊到什麼先看什麼" || sent[n-1].Rule != "聊到游泳 先看泳鏡" {
		t.Fatalf("customize add 送出的請求：%+v", sent)
	}
}

// roster 一行一筆 JSON，列出名冊上的秘書（跨頁照 vault.list 的分頁取完）。
func TestRosterPrintsOneLinePerAgent(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- sdk.Serve(ctx, sock, map[string]any{"vault": rosterVault{}}) }()
	t.Cleanup(func() { cancel(); <-done; os.RemoveAll(dir) })
	waitSocket(t, sock)
	c := sdk.Dial(sock)
	lines := strings.Split(strings.TrimSpace(mustRun(t, c, "roster")), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], `"id":"life"`) {
		t.Fatalf("roster: %q", lines)
	}
	if code, _, _ := run(t, c, "", "roster", "x"); code != 2 {
		t.Fatalf("roster 不收參數：%d", code)
	}
}

type rosterVault struct{ sdk.VaultAPI }

func (rosterVault) List(_ context.Context, r *sdk.VaultListRequest) (*sdk.VaultListResponse, error) {
	if r.Kind != "agent" {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "只列名冊"}
	}
	return &sdk.VaultListResponse{Records: []sdk.Record{{Kind: "agent", ID: "code"}, {Kind: "agent", ID: "life"}}}, nil
}
