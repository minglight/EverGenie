package roster

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

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// fakeRoster 是假的名冊管理（Module.ops）：記下收到的呼叫（含操作者），不做名冊的驗證；找不到的 id 回 NotFound。
type fakeRoster struct {
	mu    sync.Mutex
	calls []string
	known map[string]bool
}

func (r *fakeRoster) log(format string, a ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, fmt.Sprintf(format, a...))
}
func (r *fakeRoster) check(id string) error {
	if !r.known[id] {
		return platform.NotFound("沒有這位秘書：" + id)
	}
	return nil
}
func (r *fakeRoster) Create(_ context.Context, id, persona, display, duty string, knowledge []string, by string) (*sdk.Record, error) {
	r.log("create %s %s %s %s by=%s", id, persona, display, duty, by)
	return &sdk.Record{Kind: "agent", ID: id}, nil
}
func (r *fakeRoster) Archive(_ context.Context, id, by string) ([]string, string, error) {
	r.log("archive %s by=%s", id, by)
	return []string{"agent/" + id}, "搬回來", r.check(id)
}
func (r *fakeRoster) Rename(_ context.Context, old, next, display, by string) (*sdk.Record, []string, []string, error) {
	r.log("rename %s %s by=%s", old, next, by)
	return &sdk.Record{Kind: "agent", ID: next}, []string{"memory/" + next}, nil, nil
}
func (r *fakeRoster) Assign(_ context.Context, id, persona, by string) error {
	r.log("assign %s %s by=%s", id, persona, by)
	return r.check(id)
}
func (r *fakeRoster) Swap(_ context.Context, a, b, by string) ([]*sdk.Record, error) {
	r.log("swap %s %s by=%s", a, b, by)
	return []*sdk.Record{{ID: a}, nil, {ID: b}}, nil
}
func (r *fakeRoster) SetAccount(_ context.Context, id, account, model, effort, by string) (*sdk.Record, error) {
	r.log("account %s %s by=%s", id, account, by)
	return &sdk.Record{ID: id}, nil
}
func (r *fakeRoster) SetField(_ context.Context, id, field, value, by string) (*sdk.Record, error) {
	r.log("set %s %s=%s by=%s", id, field, value, by)
	return &sdk.Record{ID: id}, nil
}
func (r *fakeRoster) AddRule(_ context.Context, id, section, rule, by string) (string, error) {
	r.log("rule %s section=%s by=%s", id, section, by)
	return rule + "\n", nil
}
func (r *fakeRoster) Customizations(_ context.Context, id string) (string, string, error) {
	return "職責", "附加", r.check(id)
}
func (r *fakeRoster) Cards(context.Context) ([]sdk.AgentCard, string, error) {
	return []sdk.AgentCard{{ID: "code"}}, "Amy", nil
}
func (r *fakeRoster) WriteMemory(_ context.Context, id, body, by string) error {
	r.log("memory %s by=%s", id, by)
	return nil
}
func (r *fakeRoster) WriteNote(_ context.Context, id, name, body, revision, by string) (string, bool, string, error) {
	r.log("note %s %s rev=%s by=%s", id, name, revision, by)
	return name, revision == "", "rev-1", r.check(id)
}
func (r *fakeRoster) AppendJournal(_ context.Context, id, line string) error {
	r.log("journal %s %s", id, line)
	return nil
}
func (r *fakeRoster) Knowledge(_ context.Context, id string) ([]sdk.AgentKnowledgeBlock, error) {
	return []sdk.AgentKnowledgeBlock{}, r.check(id)
}
func (r *fakeRoster) SetKnowledge(_ context.Context, id string, enabled []string, by string) ([]sdk.AgentKnowledgeBlock, error) {
	r.log("knowledge %s %s by=%s", id, strings.Join(enabled, ","), by)
	return []sdk.AgentKnowledgeBlock{}, nil
}

type fakeRecords struct{}

func (fakeRecords) Get(_ context.Context, kind, id string) (sdk.Record, error) {
	return sdk.Record{Kind: kind, ID: id, Fields: map[string]any{"persona": "新人設"}}, nil
}

type fakeCards struct{ applied []bool }

func (c *fakeCards) WriteAgentCards(_ context.Context, cards []sdk.AgentCard, user string, apply bool) ([]sdk.AgentLinkAccount, error) {
	c.applied = append(c.applied, apply)
	return []sdk.AgentLinkAccount{{Account: "main"}}, nil
}

func fixture(t *testing.T) (*Module, *fakeRoster, *fakeCards) {
	t.Helper()
	r := &fakeRoster{known: map[string]bool{"code": true, "life": true}}
	c := &fakeCards{}
	m := New(Deps{Vault: fakeRecords{}, Cards: c})
	m.ops = r // HTTP 的測試只看轉手：名冊管理換成假的（規則的測試在 manage_test.go）
	return m, r, c
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
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw)))
	return rec.Code, rec.Body.Bytes()
}

// 模組實作 RouteProvider：整個 sdk 的 agents 服務，加上 web 服務裡看板名冊的兩個方法（get_roster、get_agents）；
// 簽名不符 sdk 介面的話 Mux 會回錯。
func TestRoutesRegisterAgentsService(t *testing.T) {
	m, _, _ := fixture(t)
	got := m.Routes()
	if len(got) != 2 || got[0].Service != "agents" || got[0].Impl == nil || len(got[0].Methods) != 0 ||
		got[1].Service != "web" || strings.Join(got[1].Methods, ",") != "get_roster,get_agents" {
		t.Fatalf("Routes = %+v", got)
	}
	serve(t, m)
}

// 每個寫入都要帶操作者：沒帶 → 400、欄位 by、名冊沒被叫到。
func TestHTTPWritesRequireActor(t *testing.T) {
	m, r, _ := fixture(t)
	h := serve(t, m)
	writes := map[string]any{
		"/api/agents/create":         &sdk.AgentsCreateRequest{ID: "x", Persona: "p"},
		"/api/agents/archive":        &sdk.AgentsArchiveRequest{ID: "code"},
		"/api/agents/rename":         &sdk.AgentsRenameRequest{ID: "code", NewID: "dev"},
		"/api/agents/assign":         &sdk.AgentsAssignRequest{ID: "code", Persona: "p"},
		"/api/agents/swap":           &sdk.AgentsSwapRequest{A: "code", B: "life"},
		"/api/agents/set_account":    &sdk.AgentsSetAccountRequest{ID: "code", Account: "main"},
		"/api/agents/set_field":      &sdk.AgentsSetFieldRequest{ID: "code", Field: "duty", Value: "x"},
		"/api/agents/add_rule":       &sdk.AgentsAddRuleRequest{ID: "code", Rule: "x"},
		"/api/agents/write_memory":   &sdk.AgentsWriteMemoryRequest{ID: "code", Body: "x"},
		"/api/agents/write_note":     &sdk.AgentsWriteNoteRequest{ID: "code", Name: "n", Body: "x"},
		"/api/agents/append_journal": &sdk.AgentsAppendJournalRequest{ID: "code", Line: "x"},
		"/api/agents/link":           &sdk.AgentsLinkRequest{Apply: true},
	}
	for path, req := range writes {
		status, body := post(t, h, path, req)
		var e sdk.Error
		if err := json.Unmarshal(body, &e); err != nil || status != 400 || e.Code != sdk.ErrorCodeInvalid || e.Field != "by" {
			t.Errorf("%s 沒帶 by：%d %s", path, status, body)
		}
	}
	if len(r.calls) != 0 {
		t.Fatalf("被擋的寫入不能呼叫名冊：%v", r.calls)
	}
}

// 寫入把操作者名稱交給名冊；assign 之後回的是名冊上的新紀錄；swap 略過 nil。
func TestHTTPRoundTrip(t *testing.T) {
	m, r, _ := fixture(t)
	h := serve(t, m)
	by := &sdk.Actor{Name: "owner(web)"}
	if status, body := post(t, h, "/api/agents/create", &sdk.AgentsCreateRequest{ID: "scout", Persona: "athena", DisplayName: "偵察", Duty: "巡邏", By: by}); status != 200 {
		t.Fatalf("create：%d %s", status, body)
	}
	var assigned sdk.AgentsAssignResponse
	status, body := post(t, h, "/api/agents/assign", &sdk.AgentsAssignRequest{ID: "code", Persona: "新人設", By: by})
	if err := json.Unmarshal(body, &assigned); err != nil || status != 200 || assigned.Agent == nil || assigned.Agent.Fields["persona"] != "新人設" {
		t.Fatalf("assign：%d %s", status, body)
	}
	var swapped sdk.AgentsSwapResponse
	status, body = post(t, h, "/api/agents/swap", &sdk.AgentsSwapRequest{A: "code", B: "life", By: by})
	if err := json.Unmarshal(body, &swapped); err != nil || status != 200 || len(swapped.Agents) != 2 {
		t.Fatalf("swap：%d %s", status, body)
	}
	if status, body := post(t, h, "/api/agents/add_rule", &sdk.AgentsAddRuleRequest{ID: "code", Section: "聊到什麼先看什麼", Rule: "聊到跑步先看鞋子", By: by}); status != 200 {
		t.Fatalf("add_rule：%d %s", status, body)
	}
	got := strings.Join(r.calls, ";")
	if !strings.Contains(got, "rule code section=聊到什麼先看什麼 by=owner(web)") {
		t.Fatalf("add_rule 要把小節交給名冊（DEV-161）：%s", got)
	}
	if !strings.Contains(got, "create scout athena 偵察 巡邏 by=owner(web)") || !strings.Contains(got, "assign code 新人設 by=owner(web)") {
		t.Fatalf("名冊收到：%s", got)
	}
}

// 名冊的錯誤轉成 API 的白話錯誤（404 找不到）。
func TestHTTPMapsErrors(t *testing.T) {
	m, _, _ := fixture(t)
	h := serve(t, m)
	status, body := post(t, h, "/api/agents/archive", &sdk.AgentsArchiveRequest{ID: "nobody", By: &sdk.Actor{Name: "owner(web)"}})
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil || status != 404 || e.Code != sdk.ErrorCodeNotFound || !strings.Contains(e.Message, "nobody") {
		t.Fatalf("找不到：%d %s", status, body)
	}
	status, body = post(t, h, "/api/agents/customizations", &sdk.AgentsCustomizationsRequest{ID: "nobody"})
	if err := json.Unmarshal(body, &e); err != nil || status != 404 {
		t.Fatalf("customizations 找不到：%d %s", status, body)
	}
}

// link：預演誰都可以；真的寫（apply）只有主人，秘書要 403，而且引擎沒被叫到。
func TestHTTPLinkApplyOwnerOnly(t *testing.T) {
	m, _, c := fixture(t)
	h := serve(t, m)
	if status, body := post(t, h, "/api/agents/link", &sdk.AgentsLinkRequest{By: &sdk.Actor{Name: "code"}}); status != 200 {
		t.Fatalf("秘書預演：%d %s", status, body)
	}
	status, body := post(t, h, "/api/agents/link", &sdk.AgentsLinkRequest{Apply: true, By: &sdk.Actor{Name: "code"}})
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil || status != 403 || e.Code != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書 apply：%d %s", status, body)
	}
	if len(c.applied) != 1 || c.applied[0] {
		t.Fatalf("被擋的 apply 不能寫：%v", c.applied)
	}
	var linked sdk.AgentsLinkResponse
	status, body = post(t, h, "/api/agents/link", &sdk.AgentsLinkRequest{Apply: true, By: &sdk.Actor{Name: "owner(cli)"}})
	if err := json.Unmarshal(body, &linked); err != nil || status != 200 || !linked.Applied || len(linked.Accounts) != 1 {
		t.Fatalf("主人 apply：%d %s", status, body)
	}
}

// 日誌只能追加：只有本人或 system 寫自己的，一次一行、不能空白。
func TestHTTPAppendJournalRules(t *testing.T) {
	m, r, _ := fixture(t)
	h := serve(t, m)
	cases := []struct {
		by     string
		line   string
		status int
	}{
		{"code", "修好了", 200},
		{"system", "夜班", 200},
		{"life", "寫別人的", 403},
		{"owner(cli)", "主人也不能寫秘書的日誌", 403},
		{"code", "兩行\n不行", 400},
		{"code", "  ", 400},
	}
	for _, c := range cases {
		if status, body := post(t, h, "/api/agents/append_journal", &sdk.AgentsAppendJournalRequest{ID: "code", Line: c.line, By: &sdk.Actor{Name: c.by}}); status != c.status {
			t.Errorf("%s %q：%d %s", c.by, c.line, status, body)
		}
	}
	if len(r.calls) != 2 {
		t.Fatalf("只有合法的兩筆會寫：%v", r.calls)
	}
}

// 做事筆記：操作者交給名冊（權限與名字由底層判斷）；回實際用的名字、是不是新建、版本。
func TestHTTPWriteNote(t *testing.T) {
	m, r, _ := fixture(t)
	h := serve(t, m)
	status, body := post(t, h, "/api/agents/write_note", &sdk.AgentsWriteNoteRequest{ID: "code", Name: "計畫", Body: "x\n", By: &sdk.Actor{Name: "code"}})
	var out sdk.AgentsWriteNoteResponse
	if err := json.Unmarshal(body, &out); err != nil || status != 200 || out.Name != "計畫" || !out.Created || out.Revision != "rev-1" {
		t.Fatalf("write_note：%d %s", status, body)
	}
	if len(r.calls) != 1 || r.calls[0] != "note code 計畫 rev= by=code" {
		t.Fatalf("要把操作者交給名冊：%v", r.calls)
	}
}

// 底層沒接上就回 unavailable，不 panic。
func TestHTTPUnavailableWithoutRoster(t *testing.T) {
	h := serve(t, New(Deps{}))
	status, body := post(t, h, "/api/agents/create", &sdk.AgentsCreateRequest{ID: "x", Persona: "p", By: &sdk.Actor{Name: "owner(cli)"}})
	var e sdk.Error
	if err := json.Unmarshal(body, &e); err != nil || status != 503 || e.Code != sdk.ErrorCodeUnavailable {
		t.Fatalf("沒接名冊：%d %s", status, body)
	}
}
