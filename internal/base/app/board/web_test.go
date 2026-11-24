package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// ---- 測試用的假 api：只做看板會呼叫的那幾個服務，記下收到的 by／from ----

type fakeAPI struct {
	mu          sync.Mutex
	waitingUser int32
	threads     []sdk.ThreadSummary
	notices     []sdk.Notification
	waiting     []sdk.WaitingTask
	reminders   []sdk.Reminder
	sessions    []sdk.Session

	acceptBy   string
	createBy   string // tasks.create 收到的 by
	sendFrom   string
	sendKind   sdk.EnvelopeKind
	sendCalls  int
	remindBy   string
	decideBy   string
	rulesBy    string // 規矩存檔的操作者（S7）
	personaBy  map[string]string
	packSync   *sdk.PersonasPackSyncRequest
	assignBy   string
	calls      int      // 交到 api 的請求數（任何路徑；跨站檢查擋下的不能算進來）
	reached    bool     // 沒掛的服務被打到
	hit        []string // 沒掛的服務被打到的路徑（轉到 api 了的證據）
	levelsBy   string   // system.set_permission_levels 收到的 by
	vaultCalls []string // vault 服務收到的呼叫（方法 種類 by）
	subTopics  []string // bus.subscribe 收到的 topics（逗號連起來）
	menuBy     string   // web.set_menu_prefs 收到的 by

	presenceBy string // notify.presence 收到的 by
	resolveBy  string // notify.resolve_nudge 收到的 by
	pushKeyErr error  // notify.push_key 失敗（金鑰沒設定）
	pushSubs   int    // notify.push_key 回的訂閱數
}

type fakeSession struct {
	sdk.SessionAPI
	f *fakeAPI
}

func (s fakeSession) ListThreads(ctx context.Context, req *sdk.SessionListThreadsRequest) (*sdk.SessionListThreadsResponse, error) {
	return &sdk.SessionListThreadsResponse{Threads: s.f.threads, WaitingUser: s.f.waitingUser}, nil
}
func (s fakeSession) List(ctx context.Context, req *sdk.SessionListRequest) (*sdk.SessionListResponse, error) {
	return &sdk.SessionListResponse{Sessions: s.f.sessions}, nil
}
func (s fakeSession) Watch(ctx context.Context, req *sdk.SessionWatchRequest, send func(*sdk.SessionWatchResponse) error) error {
	return send(&sdk.SessionWatchResponse{Session: &sdk.Session{Name: "code", Agent: "code"}})
}

type fakeNotify struct {
	sdk.NotifyAPI
	f *fakeAPI
}

// List 照真的 notify 的行為依 states 過濾；Unread 是全部未讀數。
func (n fakeNotify) List(ctx context.Context, req *sdk.NotifyListRequest) (*sdk.NotifyListResponse, error) {
	want := map[sdk.NotificationState]bool{}
	for _, s := range req.States {
		want[s] = true
	}
	var out []sdk.Notification
	var unread int32
	for _, x := range n.f.notices {
		if x.State == sdk.NotificationStateUnread {
			unread++
		}
		if len(want) == 0 || want[x.State] {
			out = append(out, x)
		}
	}
	return &sdk.NotifyListResponse{Notifications: out, Unread: unread}, nil
}

func (n fakeNotify) Presence(ctx context.Context, req *sdk.NotifyPresenceRequest) (*sdk.NotifyPresenceResponse, error) {
	n.f.mu.Lock()
	defer n.f.mu.Unlock()
	if req.By != nil {
		n.f.presenceBy = req.By.Name
	}
	return &sdk.NotifyPresenceResponse{}, nil
}
func (n fakeNotify) ResolveNudge(ctx context.Context, req *sdk.NotifyResolveNudgeRequest) (*sdk.NotifyResolveNudgeResponse, error) {
	n.f.mu.Lock()
	defer n.f.mu.Unlock()
	if req.By != nil {
		n.f.resolveBy = req.By.Name
	}
	return &sdk.NotifyResolveNudgeResponse{}, nil
}

func (n fakeNotify) PushKey(ctx context.Context, req *sdk.NotifyPushKeyRequest) (*sdk.NotifyPushKeyResponse, error) {
	if n.f.pushKeyErr != nil {
		return nil, n.f.pushKeyErr
	}
	return &sdk.NotifyPushKeyResponse{PublicKey: "fake-public-key", Subscriptions: n.f.pushSubs}, nil
}

// fakeSystem 假裝 api 的 system.doctor：看板自己的「系統健康」清單只要 push group（真的判斷在 notify.PushStatus，
// 那邊有自己的測試；這裡只驗看板把 push group 的結果原樣當成「推播」一項）；不帶 group 的是設定頁的完整檢查
// （DEV-153，WEB-191），原樣轉過去。帶別的 group 就是看板自己問錯了。
type fakeSystem struct {
	sdk.SystemAPI
	f *fakeAPI
}

func (s fakeSystem) Doctor(ctx context.Context, req *sdk.SystemDoctorRequest) (*sdk.SystemDoctorResponse, error) {
	if len(req.Groups) == 0 {
		return &sdk.SystemDoctorResponse{Checks: []sdk.DoctorCheck{{ID: "config.home", Group: sdk.DoctorGroupConfig, Ok: true, Severity: sdk.DoctorSeverityOk}}}, nil
	}
	if len(req.Groups) != 1 || req.Groups[0] != sdk.DoctorGroupPush {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "看板只該要 push group"}
	}
	check := sdk.DoctorCheck{ID: "push.status", Group: sdk.DoctorGroupPush, Severity: sdk.DoctorSeverityWarn}
	switch {
	case s.f.pushKeyErr != nil:
		check.Detail = "還沒設定推播金鑰"
	case s.f.pushSubs < 1:
		check.Detail = "還沒有裝置開啟推播"
	default:
		check.Ok, check.Severity, check.Detail = true, sdk.DoctorSeverityOk, fmt.Sprintf("%d 台裝置", s.f.pushSubs)
	}
	return &sdk.SystemDoctorResponse{Checks: []sdk.DoctorCheck{check}}, nil
}

func (s fakeSystem) Report(ctx context.Context, req *sdk.SystemReportRequest) (*sdk.SystemReportResponse, error) {
	return &sdk.SystemReportResponse{}, nil
}

func (s fakeSystem) Backups(ctx context.Context, req *sdk.SystemBackupsRequest) (*sdk.SystemBackupsResponse, error) {
	return &sdk.SystemBackupsResponse{Total: 3}, nil
}
func (s fakeSystem) Trust(ctx context.Context, req *sdk.SystemTrustRequest) (*sdk.SystemTrustResponse, error) {
	return &sdk.SystemTrustResponse{DefaultLevel: sdk.PermissionLevelStandard}, nil
}
func (s fakeSystem) Preflight(ctx context.Context, req *sdk.SystemPreflightRequest) (*sdk.SystemPreflightResponse, error) {
	return &sdk.SystemPreflightResponse{}, nil
}
func (s fakeSystem) Usage(ctx context.Context, req *sdk.SystemUsageRequest) (*sdk.SystemUsageResponse, error) {
	return &sdk.SystemUsageResponse{}, nil
}
func (s fakeSystem) SetPermissionLevels(ctx context.Context, req *sdk.SystemSetPermissionLevelsRequest) (*sdk.SystemSetPermissionLevelsResponse, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if req.By != nil {
		s.f.levelsBy = req.By.Name
	}
	return &sdk.SystemSetPermissionLevelsResponse{}, nil
}

type fakeTasks struct {
	sdk.TasksAPI
	f *fakeAPI
}

func (t fakeTasks) Waiting(ctx context.Context, req *sdk.TasksWaitingRequest) (*sdk.TasksWaitingResponse, error) {
	return &sdk.TasksWaitingResponse{Tasks: t.f.waiting}, nil
}
func (t fakeTasks) Accept(ctx context.Context, req *sdk.TasksAcceptRequest) (*sdk.TasksAcceptResponse, error) {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	if req.By != nil {
		t.f.acceptBy = req.By.Name
	}
	return &sdk.TasksAcceptResponse{Task: &sdk.Task{ID: req.ID}}, nil
}

func (t fakeTasks) Create(ctx context.Context, req *sdk.TasksCreateRequest) (*sdk.TasksCreateResponse, error) {
	t.f.mu.Lock()
	defer t.f.mu.Unlock()
	if req.By != nil {
		t.f.createBy = req.By.Name
	}
	return &sdk.TasksCreateResponse{Task: &sdk.Task{ID: "SEC-9", Title: req.Title}}, nil
}

type fakeReminders struct {
	sdk.RemindersAPI
	f *fakeAPI
}

func (r fakeReminders) List(ctx context.Context, req *sdk.RemindersListRequest) (*sdk.RemindersListResponse, error) {
	if req.Query != sdk.ReminderQueryToday {
		return nil, &sdk.Error{Code: sdk.ErrorCodeInvalid, Message: "只假裝今天"}
	}
	return &sdk.RemindersListResponse{Reminders: r.f.reminders}, nil
}
func (r fakeReminders) Create(ctx context.Context, req *sdk.RemindersCreateRequest) (*sdk.RemindersCreateResponse, error) {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	if req.By != nil {
		r.f.remindBy = req.By.Name
	}
	return &sdk.RemindersCreateResponse{Reminder: &sdk.Reminder{ID: "R-1", Content: req.Content}}, nil
}

type fakeBus struct {
	sdk.BusAPI
	f *fakeAPI
}

func (b fakeBus) Send(ctx context.Context, req *sdk.BusSendRequest) (*sdk.BusSendResponse, error) {
	b.f.mu.Lock()
	defer b.f.mu.Unlock()
	b.f.sendCalls++
	b.f.sendFrom, b.f.sendKind = req.From, req.Kind
	return &sdk.BusSendResponse{Envelope: &sdk.Envelope{ID: "E-1", From: req.From, To: req.To, Kind: req.Kind}}, nil
}

// Subscribe：記下收到的 topics，送一筆就結束（看板的即時更新只拿它當「要重抓」的提示）。
func (b fakeBus) Subscribe(ctx context.Context, req *sdk.BusSubscribeRequest, send func(*sdk.BusSubscribeResponse) error) error {
	b.f.mu.Lock()
	b.f.subTopics = append(b.f.subTopics, strings.Join(req.Topics, ","))
	b.f.mu.Unlock()
	return send(&sdk.BusSubscribeResponse{Topic: "vault.changed", Payload: map[string]any{"kind": "task", "id": "SEC-1"}})
}

type fakePolicy struct {
	sdk.PolicyAPI
	f *fakeAPI
}

func (p fakePolicy) Decide(ctx context.Context, req *sdk.PolicyDecideRequest) (*sdk.PolicyDecideResponse, error) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	if req.By != nil {
		p.f.decideBy = req.By.Name
	}
	return &sdk.PolicyDecideResponse{Request: &sdk.PolicyRequest{ID: req.ID, Status: sdk.PolicyStatusApplied}}, nil
}
func (p fakePolicy) List(ctx context.Context, req *sdk.PolicyListRequest) (*sdk.PolicyListResponse, error) {
	return &sdk.PolicyListResponse{}, nil
}

// fakeRules 記下看板轉過來的規矩存檔是誰送的（規矩頁，S7）。
type fakeRules struct {
	sdk.RulesAPI
	f *fakeAPI
}

func (r fakeRules) Save(ctx context.Context, req *sdk.RulesSaveRequest) (*sdk.RulesSaveResponse, error) {
	r.f.mu.Lock()
	defer r.f.mu.Unlock()
	if req.By != nil {
		r.f.rulesBy = req.By.Name
	}
	return &sdk.RulesSaveResponse{}, nil
}

// fakePersonas 記下看板轉過來的角色庫呼叫（方法 → 操作者）；真的角色庫的行為在 agent 的測試。
type fakePersonas struct {
	sdk.PersonasAPI
	f *fakeAPI
}

func (p fakePersonas) note(method string, by *sdk.Actor) {
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	if p.f.personaBy == nil {
		p.f.personaBy = map[string]string{}
	}
	who := ""
	if by != nil {
		who = by.Name
	}
	p.f.personaBy[method] = who
}
func (p fakePersonas) PackSync(_ context.Context, r *sdk.PersonasPackSyncRequest) (*sdk.PersonasPackSyncResponse, error) {
	p.note("pack_sync", r.By)
	p.f.mu.Lock()
	defer p.f.mu.Unlock()
	p.f.packSync = r
	return &sdk.PersonasPackSyncResponse{Status: "synced"}, nil
}
func (p fakePersonas) List(context.Context, *sdk.PersonasListRequest) (*sdk.PersonasListResponse, error) {
	p.note("list", nil)
	return &sdk.PersonasListResponse{}, nil
}
func (p fakePersonas) Get(context.Context, *sdk.PersonasGetRequest) (*sdk.PersonasGetResponse, error) {
	p.note("get", nil)
	return &sdk.PersonasGetResponse{}, nil
}
func (p fakePersonas) Create(_ context.Context, r *sdk.PersonasCreateRequest) (*sdk.PersonasCreateResponse, error) {
	p.note("create", r.By)
	return &sdk.PersonasCreateResponse{}, nil
}
func (p fakePersonas) Copy(_ context.Context, r *sdk.PersonasCopyRequest) (*sdk.PersonasCopyResponse, error) {
	p.note("copy", r.By)
	return &sdk.PersonasCopyResponse{}, nil
}
func (p fakePersonas) Update(_ context.Context, r *sdk.PersonasUpdateRequest) (*sdk.PersonasUpdateResponse, error) {
	p.note("update", r.By)
	return &sdk.PersonasUpdateResponse{}, nil
}
func (p fakePersonas) SetStatus(_ context.Context, r *sdk.PersonasSetStatusRequest) (*sdk.PersonasSetStatusResponse, error) {
	p.note("set_status", r.By)
	return &sdk.PersonasSetStatusResponse{}, nil
}
func (p fakePersonas) Check(context.Context, *sdk.PersonasCheckRequest) (*sdk.PersonasCheckResponse, error) {
	p.note("check", nil)
	return &sdk.PersonasCheckResponse{}, nil
}
func (p fakePersonas) GetStyle(context.Context, *sdk.PersonasGetStyleRequest) (*sdk.PersonasGetStyleResponse, error) {
	p.note("get_style", nil)
	return &sdk.PersonasGetStyleResponse{}, nil
}
func (p fakePersonas) SetStyle(_ context.Context, r *sdk.PersonasSetStyleRequest) (*sdk.PersonasSetStyleResponse, error) {
	p.note("set_style", r.By)
	return &sdk.PersonasSetStyleResponse{}, nil
}
func (p fakePersonas) Test(context.Context, *sdk.PersonasTestRequest) (*sdk.PersonasTestResponse, error) {
	p.note("test", nil)
	return &sdk.PersonasTestResponse{}, nil
}
func (p fakePersonas) TestResults(context.Context, *sdk.PersonasTestResultsRequest) (*sdk.PersonasTestResultsResponse, error) {
	p.note("test_results", nil)
	return &sdk.PersonasTestResultsResponse{}, nil
}
func (p fakePersonas) Export(_ context.Context, r *sdk.PersonasExportRequest) (*sdk.PersonasExportResponse, error) {
	p.note("export", r.By)
	return &sdk.PersonasExportResponse{}, nil
}
func (p fakePersonas) Import(_ context.Context, r *sdk.PersonasImportRequest) (*sdk.PersonasImportResponse, error) {
	p.note("import", r.By)
	return &sdk.PersonasImportResponse{}, nil
}

// fakeAgentsAPI 接 assign（看板換人設時送來的 by 要是 owner(web)）；set_field、set_account 只記「有轉過來」（寫進 hit）。
type fakeAgentsAPI struct {
	sdk.AgentsAPI
	f *fakeAPI
}

func (a fakeAgentsAPI) SetField(context.Context, *sdk.AgentsSetFieldRequest) (*sdk.AgentsSetFieldResponse, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.hit = append(a.f.hit, "/api/agents/set_field")
	return &sdk.AgentsSetFieldResponse{}, nil
}
func (a fakeAgentsAPI) Knowledge(context.Context, *sdk.AgentsKnowledgeRequest) (*sdk.AgentsKnowledgeResponse, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.hit = append(a.f.hit, "/api/agents/knowledge")
	return &sdk.AgentsKnowledgeResponse{}, nil
}
func (a fakeAgentsAPI) SetKnowledge(_ context.Context, r *sdk.AgentsSetKnowledgeRequest) (*sdk.AgentsKnowledgeResponse, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.hit = append(a.f.hit, "/api/agents/set_knowledge")
	if r.By != nil {
		a.f.hit = append(a.f.hit, "set_knowledge by="+r.By.Name)
	}
	return &sdk.AgentsKnowledgeResponse{}, nil
}
func (a fakeAgentsAPI) SetAccount(context.Context, *sdk.AgentsSetAccountRequest) (*sdk.AgentsSetAccountResponse, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.hit = append(a.f.hit, "/api/agents/set_account")
	return &sdk.AgentsSetAccountResponse{}, nil
}
func (a fakeAgentsAPI) Assign(_ context.Context, r *sdk.AgentsAssignRequest) (*sdk.AgentsAssignResponse, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.assignBy = r.By.Name
	return &sdk.AgentsAssignResponse{}, nil
}

// fakeVault 記下看板轉過來的 vault 呼叫（方法、種類、操作者）；真的 vault 的行為在 vault／api 的測試。
type fakeVault struct {
	sdk.VaultAPI
	f *fakeAPI
}

func (v fakeVault) note(method, kind string, by *sdk.Actor) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	who := ""
	if by != nil {
		who = by.Name
	}
	v.f.vaultCalls = append(v.f.vaultCalls, method+" "+kind+" "+who)
}
func (v fakeVault) Get(ctx context.Context, req *sdk.VaultGetRequest) (*sdk.VaultGetResponse, error) {
	v.note("get", req.Kind, nil)
	return &sdk.VaultGetResponse{Record: &sdk.Record{Kind: req.Kind, ID: req.ID}}, nil
}
func (v fakeVault) List(ctx context.Context, req *sdk.VaultListRequest) (*sdk.VaultListResponse, error) {
	v.note("list", req.Kind, nil)
	return &sdk.VaultListResponse{}, nil
}
func (v fakeVault) Put(ctx context.Context, req *sdk.VaultPutRequest) (*sdk.VaultPutResponse, error) {
	v.note("put", req.Record.Kind, req.By)
	return &sdk.VaultPutResponse{Record: req.Record}, nil
}
func (v fakeVault) Changes(ctx context.Context, req *sdk.VaultChangesRequest) (*sdk.VaultChangesResponse, error) {
	v.note("changes", req.Kind, nil)
	return &sdk.VaultChangesResponse{}, nil
}
func (v fakeVault) Patch(ctx context.Context, req *sdk.VaultPatchRequest) (*sdk.VaultPatchResponse, error) {
	v.note("patch", req.Kind, req.By)
	return &sdk.VaultPatchResponse{Record: &sdk.Record{Kind: req.Kind, ID: req.ID}}, nil
}
func (v fakeVault) Archive(ctx context.Context, req *sdk.VaultArchiveRequest) (*sdk.VaultArchiveResponse, error) {
	v.note("archive", req.Kind, req.By)
	return &sdk.VaultArchiveResponse{}, nil
}
func (v fakeVault) NextId(ctx context.Context, req *sdk.VaultNextIdRequest) (*sdk.VaultNextIdResponse, error) {
	v.note("next_id", req.Kind, nil)
	return &sdk.VaultNextIdResponse{ID: "N-1"}, nil
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	for name, impl := range map[string]any{
		"session": fakeSession{f: f}, "notify": fakeNotify{f: f}, "tasks": fakeTasks{f: f},
		"reminders": fakeReminders{f: f}, "bus": fakeBus{f: f}, "system": fakeSystem{f: f}, "policy": fakePolicy{f: f}, "vault": fakeVault{f: f}, "personas": fakePersonas{f: f}, "agents": fakeAgentsAPI{f: f}, "web": fakeWeb{f: f},
		"rules": fakeRules{f: f},
	} {
		if err := sdk.Mount(mux, name, impl); err != nil {
			t.Fatal(err)
		}
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.reached = true
		f.hit = append(f.hit, r.URL.Path)
		f.mu.Unlock()
		sdk.NotFoundHandler().ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls++
		f.mu.Unlock()
		mux.ServeHTTP(w, r)
	})
}

// fakeWeb 是 web 服務的假實作：每個方法回空的回應；set_menu_prefs 記下收到的 by。
type fakeWeb struct{ f *fakeAPI }

func (w fakeWeb) GetMenu(context.Context, *sdk.WebGetMenuRequest) (*sdk.WebGetMenuResponse, error) {
	return &sdk.WebGetMenuResponse{}, nil
}
func (w fakeWeb) GetPages(context.Context, *sdk.WebGetPagesRequest) (*sdk.WebGetPagesResponse, error) {
	return &sdk.WebGetPagesResponse{Pages: []sdk.PageInfo{{ID: "core.overview", Title: "今天", Route: "/"}}}, nil
}
func (w fakeWeb) GetMenuPrefs(context.Context, *sdk.WebGetMenuPrefsRequest) (*sdk.WebGetMenuPrefsResponse, error) {
	return &sdk.WebGetMenuPrefsResponse{}, nil
}
func (w fakeWeb) SetMenuPrefs(_ context.Context, r *sdk.WebSetMenuPrefsRequest) (*sdk.WebSetMenuPrefsResponse, error) {
	w.f.mu.Lock()
	defer w.f.mu.Unlock()
	if r.By != nil {
		w.f.menuBy = r.By.Name
	}
	return &sdk.WebSetMenuPrefsResponse{}, nil
}
func (w fakeWeb) GetHealth(context.Context, *sdk.WebGetHealthRequest) (*sdk.WebGetHealthResponse, error) {
	return &sdk.WebGetHealthResponse{}, nil
}
func (w fakeWeb) GetRoster(context.Context, *sdk.WebGetRosterRequest) (*sdk.WebGetRosterResponse, error) {
	return &sdk.WebGetRosterResponse{}, nil
}
func (w fakeWeb) GetOverview(context.Context, *sdk.WebGetOverviewRequest) (*sdk.WebGetOverviewResponse, error) {
	return &sdk.WebGetOverviewResponse{}, nil
}
func (w fakeWeb) GetTrust(context.Context, *sdk.WebGetTrustRequest) (*sdk.WebGetTrustResponse, error) {
	return &sdk.WebGetTrustResponse{}, nil
}
func (w fakeWeb) GetAgents(context.Context, *sdk.WebGetAgentsRequest) (*sdk.WebGetAgentsResponse, error) {
	return &sdk.WebGetAgentsResponse{}, nil
}
func (w fakeWeb) GetSites(context.Context, *sdk.WebGetSitesRequest) (*sdk.WebGetSitesResponse, error) {
	return &sdk.WebGetSitesResponse{}, nil
}
func (w fakeWeb) SetSites(context.Context, *sdk.WebSetSitesRequest) (*sdk.WebSetSitesResponse, error) {
	return &sdk.WebSetSitesResponse{}, nil
}
func (w fakeWeb) SetAllowBoards(context.Context, *sdk.WebSetAllowBoardsRequest) (*sdk.WebSetAllowBoardsResponse, error) {
	return &sdk.WebSetAllowBoardsResponse{}, nil
}
func (w fakeWeb) GetBootPrompts(context.Context, *sdk.WebGetBootPromptsRequest) (*sdk.WebGetBootPromptsResponse, error) {
	return &sdk.WebGetBootPromptsResponse{}, nil
}

func (w fakeWeb) AnswerBootPrompt(context.Context, *sdk.WebAnswerBootPromptRequest) (*sdk.WebAnswerBootPromptResponse, error) {
	return &sdk.WebAnswerBootPromptResponse{}, nil
}

func (w fakeWeb) VoiceAnswer(context.Context, *sdk.WebVoiceAnswerRequest) (*sdk.WebVoiceAnswerResponse, error) {
	return &sdk.WebVoiceAnswerResponse{}, nil
}

func (w fakeWeb) VoiceSend(context.Context, *sdk.WebVoiceSendRequest) (*sdk.WebVoiceSendResponse, error) {
	return &sdk.WebVoiceSendResponse{}, nil
}

func (w fakeWeb) SetOwnerLogin(context.Context, *sdk.WebSetOwnerLoginRequest) (*sdk.WebSetOwnerLoginResponse, error) {
	return &sdk.WebSetOwnerLoginResponse{}, nil
}

// 手機連線的三個方法把收到的 local 原樣回出來（thismac_test.go 看看板伺服器填了什麼）。
func (w fakeWeb) GetPhone(_ context.Context, r *sdk.WebGetPhoneRequest) (*sdk.WebGetPhoneResponse, error) {
	return &sdk.WebGetPhoneResponse{Phone: sdk.WebPhone{Local: r.Local}}, nil
}

func (w fakeWeb) PhoneSetup(_ context.Context, r *sdk.WebPhoneSetupRequest) (*sdk.WebPhoneSetupResponse, error) {
	return &sdk.WebPhoneSetupResponse{OK: true, Phone: sdk.WebPhone{Local: r.Local}}, nil
}

func (w fakeWeb) PhoneCloseFunnel(_ context.Context, r *sdk.WebPhoneCloseFunnelRequest) (*sdk.WebPhoneCloseFunnelResponse, error) {
	return &sdk.WebPhoneCloseFunnelResponse{Phone: sdk.WebPhone{Local: r.Local}}, nil
}

type fakeHandler struct{ h http.Handler }

func (f fakeHandler) Handler() http.Handler { return f.h }

type fakeAgents struct {
	list  []*sdk.Record
	names map[string]string
}

func (f fakeAgents) List(ctx context.Context) ([]*sdk.Record, error) { return f.list, nil }
func (f fakeAgents) PersonaName(ctx context.Context, id string) (string, error) {
	if n, ok := f.names[id]; ok {
		return n, nil
	}
	return "", errors.New("沒有這個人設")
}

func agentRecord(id string, fields map[string]any) *sdk.Record {
	return &sdk.Record{Kind: "agent", ID: id, Fields: fields}
}

const homeTOML = `[user]
name="測試使用者"
timezone="Asia/Taipei"
[slots]
default="life"
[slots.life]
label="日常"
remind="08:30"
check="18:00"
[accounts]
default="main"
[accounts.main]
engine="test-engine"
[web]
port=%d
%s`

// freePort 找一個現在空著的 127.0.0.1 連接埠（web 不能 import homefixture，所以這裡自己做）。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

type env struct {
	m      *Module
	h      http.Handler
	api    *fakeAPI
	home   string
	cat    *fakeCatalog // 套件目錄（可換內容）
	as     *Assets
	agents *fakeAgents
}

// setPackages 換掉套件目錄的內容（Assets 看到的就是新的）。
func (e *env) setPackages(c *fakeCatalog) {
	e.cat.pkgs, e.cat.dirs = c.pkgs, c.dirs
}

// newEnv 起假 api（in-process handler）、暫存的家、一個看板 Module（套件目錄一開始是空的）。
func newEnv(t *testing.T, extraWeb string) *env {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "hgw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	write(t, filepath.Join(home, "home.toml"), fmt.Sprintf(homeTOML, freePort(t), extraWeb))

	api := &fakeAPI{}
	apiHandler := api.handler(t)

	agents := &fakeAgents{
		list: []*sdk.Record{
			agentRecord("code", map[string]any{"duty": "測試開發", "persona": "builder", "enabled": true, "mode": "resident", "color": "#336699", "color_dark": "#99ccff"}),
			agentRecord("life", map[string]any{"duty": "生活", "persona": "helper", "enabled": true, "mode": "resident"}),
			agentRecord("fitness", map[string]any{"duty": "健身", "persona": "coach", "enabled": false, "mode": "resident"}),
		},
		names: map[string]string{"code": "Builder", "life": "Helper", "fitness": "Coach"},
	}
	cat := &fakeCatalog{dirs: map[string]string{}}
	as := NewAssets(cat)
	m := New(Deps{Assets: as, Agents: agents, API: fakeHandler{apiHandler}, Clock: platform.NewFakeClock(time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC))})
	m.SetHome(home)
	return &env{m: m, h: viaLoopback(m.Handler()), api: api, home: home, cat: cat, as: as, agents: agents}
}

// viaLoopback 把 httptest 預設的 Host（example.com）換成 127.0.0.1，當成從本機進來（看板只聽 127.0.0.1）；
// 測 Host 檢查（DNS rebinding）的測試自己指定別的 Host，不受影響——但攻擊者的 Host 不要用 example.com（會被換掉），用 evil.example 這類。
func viaLoopback(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "example.com" || r.Host == "" { // httptest 的預設、http.NewRequest 只給路徑的
			r.Host = "127.0.0.1"
		}
		h.ServeHTTP(w, r)
	})
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func post(h http.Handler, path, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func get(h http.Handler, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, target, nil))
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return v
}

func decodeErr(t *testing.T, w *httptest.ResponseRecorder) sdk.Error {
	t.Helper()
	var e sdk.Error
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("%v: %s", err, w.Body.String())
	}
	return e
}

func approve(id string, state sdk.NotificationState) sdk.Notification {
	return sdk.Notification{ID: id, Category: sdk.CategoryApprove, State: state, Title: "要不要？"}
}

// ---- 身分、白名單、跨站 ----

// Guards: REQ-110
func TestBoardIdentityForced(t *testing.T) {
	e := newEnv(t, "")
	w := post(e.h, "/api/tasks/accept", `{"id":"SEC-1","by":{"name":"life"}}`)
	if w.Code != http.StatusOK || e.api.acceptBy != "owner(web)" {
		t.Fatalf("accept: %d by=%q %s", w.Code, e.api.acceptBy, w.Body.String())
	}
	// 沒有 by（前端不送）也補上；大小寫不同的 by 不能偷渡。
	e.api.acceptBy = ""
	if w := post(e.h, "/api/tasks/accept", `{"id":"SEC-2"}`); w.Code != http.StatusOK || e.api.acceptBy != "owner(web)" {
		t.Fatalf("accept 沒有 by：%d by=%q", w.Code, e.api.acceptBy)
	}
	e.api.acceptBy = ""
	if w := post(e.h, "/api/tasks/accept", `{"id":"SEC-3","BY":{"name":"code"}}`); w.Code != http.StatusOK || e.api.acceptBy != "owner(web)" {
		t.Fatalf("accept 大寫 BY：%d by=%q", w.Code, e.api.acceptBy)
	}
	if w := post(e.h, "/api/reminders/create", `{"content":"買牛奶","by":{"name":"code"}}`); w.Code != http.StatusOK || e.api.remindBy != "owner(web)" {
		t.Fatalf("reminders.create: %d by=%q", w.Code, e.api.remindBy)
	}
	// bus.send：from 一律 owner(web)，只准 chat。
	if w := post(e.h, "/api/bus/send", `{"from":"life","to":"code","kind":"chat","body":"嗨"}`); w.Code != http.StatusOK || e.api.sendFrom != "owner(web)" || e.api.sendKind != sdk.EnvelopeKindChat {
		t.Fatalf("bus.send chat: %d from=%q kind=%q %s", w.Code, e.api.sendFrom, e.api.sendKind, w.Body.String())
	}
	before := e.api.sendCalls
	for _, body := range []string{`{"from":"life","to":"code","kind":"say","body":"偷講"}`, `{"to":"code","KIND":"delegate","body":"x"}`} {
		w := post(e.h, "/api/bus/send", body)
		er := decodeErr(t, w)
		if w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || er.Message != "看板只能送聊天訊息" {
			t.Fatalf("%s → %d %+v", body, w.Code, er)
		}
	}
	if e.api.sendCalls != before {
		t.Fatal("被擋下的訊息不能送到 bus")
	}
	// 不是 JSON 物件的內容換不了身分，一律擋下，不能原樣轉給服務。
	e.api.acceptBy = ""
	for _, body := range []string{`null`, `[{"id":"SEC-4"}]`, `"x"`, `{`} {
		w := post(e.h, "/api/tasks/accept", body)
		if er := decodeErr(t, w); er.Code != sdk.ErrorCodeInvalid || e.api.acceptBy != "" {
			t.Fatalf("%s → %d %+v by=%q", body, w.Code, er, e.api.acceptBy)
		}
	}
	if w := post(e.h, "/api/bus/send", `null`); e.api.sendCalls != before {
		t.Fatalf("bus.send null 不能送到 bus：%d", w.Code)
	}
	// 設定選單：看板來的請求就算塞 by: life 也是 owner(web)（選單業務模組只認 owner(…)，怎麼擋秘書在 internal/feature/inbox/menu 測）。
	b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{}, By: &sdk.Actor{Name: "life"}})
	if w := post(e.h, "/api/web/set_menu_prefs", string(b)); w.Code != http.StatusOK || e.api.menuBy != boardActor {
		t.Fatalf("set_menu_prefs: %d %s by=%q", w.Code, w.Body.String(), e.api.menuBy)
	}
}

// sdk 的 web 服務（選單、頁面、名冊、總覽、健康、權限、設定頁的秘書）整個開放給看板，由核心的各業務模組登記；看板外殼自己不實作任何一個。
// Guards: REQ-111
func TestBoardOpensWebService(t *testing.T) {
	e := newEnv(t, "")
	for _, method := range sdk.ServiceMethods("web") {
		if localAware["web"][method] == localOnly {
			continue // 只准這台 Mac 本機開的看板（手機連線的設定，thismac_test.go 另外測）
		}
		if w := post(e.h, "/api/web/"+method, `{}`); w.Code != http.StatusOK {
			t.Errorf("web.%s：%d %s", method, w.Code, w.Body.String())
		}
	}
	if w := post(e.h, "/api/web/upload_nothing", `{}`); w.Code != http.StatusNotFound {
		t.Errorf("web 服務沒有的方法：%d", w.Code)
	}
	if e.api.reached {
		t.Fatalf("web 的方法不該落到沒掛的路徑：%v", e.api.hit)
	}
}

// 不吵的通知（P7-6）：看板能報在場、按「知道了」，身分一律是主人（自稱秘書也換掉）；送提醒（nudge）不從看板來。
// Guards: REQ-225, REQ-227
func TestBoardPresenceIdentity(t *testing.T) {
	e := newEnv(t, "")
	if w := post(e.h, "/api/notify/presence", `{"client":"c1","path":"/","by":{"name":"code"}}`); w.Code != http.StatusOK || e.api.presenceBy != "owner(web)" {
		t.Fatalf("presence 的 by 要換成 owner(web)：%d %q", w.Code, e.api.presenceBy)
	}
	if w := post(e.h, "/api/notify/resolve_nudge", `{"id":"nudge-1"}`); w.Code != http.StatusOK || e.api.resolveBy != "owner(web)" {
		t.Fatalf("resolve_nudge 的 by 要補上 owner(web)：%d %q", w.Code, e.api.resolveBy)
	}
	if w := post(e.h, "/api/notify/nudge", `{"agent":"code","session":"code-sec-1"}`); w.Code != http.StatusNotFound {
		t.Fatalf("看板不能送提醒：%d", w.Code)
	}
}

// Guards: REQ-104
func TestBoardServiceAllowlist(t *testing.T) {
	e := newEnv(t, "")
	for _, path := range []string{
		"/api/engine_host/check_action", "/api/engine_host/report_event", "/api/vault/get", "/api/vault/watch",
		"/api/engine/deliver", "/api/node/register", "/api/pkg/list", "/api/scheduler/nothing", "/api/session/close", "/api/goals/propose",
		"/api/notify/send", "/api/notify/nudge", "/api/bus/publish", "/api/nobody/nothing", "/api/policy/request",
	} {
		w := post(e.h, path, "{}")
		er := decodeErr(t, w)
		if w.Code != http.StatusNotFound || er.Code != sdk.ErrorCodeNotFound || er.Message != "看板不提供這個服務" {
			t.Fatalf("%s → %d %+v", path, w.Code, er)
		}
	}
	if e.api.reached {
		t.Fatal("沒開放的服務不能轉到 api")
	}
	// 開放的會通。
	for _, path := range []string{"/api/session/list_threads", "/api/notify/list", "/api/tasks/waiting", "/api/web/get_pages", "/api/notify/presence", "/api/notify/resolve_nudge"} {
		if w := post(e.h, path, "{}"); w.Code != http.StatusOK {
			t.Fatalf("%s → %d %s", path, w.Code, w.Body.String())
		}
	}
	// web 底下沒有的方法：404。
	if w := post(e.h, "/api/web/nothing", "{}"); w.Code != http.StatusNotFound {
		t.Fatalf("/api/web/nothing → %d", w.Code)
	}
}

// 看板的 vault 只給套件的資料種類（種類名稱含 `.`）：讀寫都通，寫入的 by 換成 owner(web)；核心種類（任務、待辦…）、沒帶種類、
// put 的 record.kind 藏在別處、watch 與 append 都是 404、不轉給 api。
// Guards: REQ-173
func TestBoardVaultOnlyPackageKinds(t *testing.T) {
	e := newEnv(t, "")
	ok := map[string]string{
		"/api/vault/list":    `{"kind":"demo.note"}`,
		"/api/vault/get":     `{"kind":"demo.note","id":"N-1"}`,
		"/api/vault/put":     `{"record":{"kind":"demo.note","id":"N-1","fields":{"text":"x"}},"by":{"name":"code"}}`,
		"/api/vault/patch":   `{"kind":"demo.note","id":"N-1","set":{"text":"y"},"by":{"name":"life"}}`,
		"/api/vault/archive": `{"kind":"demo.note","id":"N-1"}`,
		"/api/vault/next_id": `{"kind":"demo.note"}`,
	}
	for path, body := range ok {
		if w := post(e.h, path, body); w.Code != http.StatusOK {
			t.Fatalf("%s → %d %s", path, w.Code, w.Body.String())
		}
	}
	e.api.mu.Lock()
	calls := strings.Join(e.api.vaultCalls, "|")
	e.api.mu.Unlock()
	for _, want := range []string{"put demo.note owner(web)", "patch demo.note owner(web)", "archive demo.note owner(web)", "list demo.note", "get demo.note", "next_id demo.note"} {
		if !strings.Contains(calls, want) {
			t.Errorf("少了「%s」（收到：%s）", want, calls)
		}
	}
	before := len(e.api.vaultCalls)
	denied := map[string]string{
		"/api/vault/list":    `{"kind":"task"}`,
		"/api/vault/get":     `{"kind":"agent","id":"code"}`,
		"/api/vault/put":     `{"record":{"kind":"task","id":"SEC-1"}}`,
		"/api/vault/put ":    `{"kind":"demo.note","record":{"kind":"task","id":"SEC-1"}}`, // 外層的 kind 不算，看 record.kind
		"/api/vault/patch":   `{"kind":"job","id":"code/x","set":{"enabled":false}}`,
		"/api/vault/archive": `{"kind":"reminder","id":"R-1"}`,
		"/api/vault/next_id": `{"kind":"task"}`,
		"/api/vault/watch":   `{"kinds":["demo.note"]}`,
		"/api/vault/append":  `{"kind":"demo.note","id":"N-1","line":"x"}`,
	}
	for path, body := range denied {
		w := post(e.h, strings.TrimSpace(path), body)
		er := decodeErr(t, w)
		if w.Code != http.StatusNotFound || er.Message != "看板不提供這個服務" {
			t.Errorf("%s %s → %d %+v", path, body, w.Code, er)
		}
	}
	// 沒帶種類、壞掉的 JSON 也不給。
	for _, body := range []string{`{}`, `{"kind":""}`, `{`, `null`} {
		if w := post(e.h, "/api/vault/list", body); w.Code == http.StatusOK {
			t.Errorf("list %s 不該通", body)
		}
	}
	if len(e.api.vaultCalls) != before {
		t.Fatalf("被擋的請求不能轉給 api：%v", e.api.vaultCalls[before:])
	}
}

// 看板 vault 的兩個例外（P6-6）：變更紀錄（changes，只讀、不看種類）和夜班清單那一筆 note 的 get／put；
// 其他 note、夜班清單的 patch／archive 一樣擋。
// Guards: REQ-111
func TestBoardVaultExceptions(t *testing.T) {
	e := newEnv(t, "")
	for path, body := range map[string]string{
		"/api/vault/changes": `{"limit":5,"by":"owner"}`, // by 是篩選，不能被換成身分
		"/api/vault/get":     `{"kind":"note","id":"system/night-queue"}`,
		"/api/vault/put":     `{"record":{"kind":"note","id":"system/night-queue","body":"x"},"by":{"name":"code"}}`,
	} {
		if w := post(e.h, path, body); w.Code != http.StatusOK {
			t.Errorf("%s → %d %s", path, w.Code, w.Body.String())
		}
	}
	e.api.mu.Lock()
	calls := strings.Join(e.api.vaultCalls, "|")
	e.api.mu.Unlock()
	for _, want := range []string{"changes", "get note", "put note owner(web)"} {
		if !strings.Contains(calls, want) {
			t.Errorf("少了「%s」（收到：%s）", want, calls)
		}
	}
	before := len(e.api.vaultCalls)
	for path, body := range map[string]string{
		"/api/vault/get":     `{"kind":"note","id":"code/other"}`,
		"/api/vault/put":     `{"record":{"kind":"note","id":"code/other"}}`,
		"/api/vault/patch":   `{"kind":"note","id":"system/night-queue","set":{"x":1}}`,
		"/api/vault/archive": `{"kind":"note","id":"system/night-queue"}`,
		"/api/vault/list":    `{"kind":"note"}`,
	} {
		w := post(e.h, path, body)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s %s 要擋：%d", path, body, w.Code)
		}
	}
	if len(e.api.vaultCalls) != before {
		t.Fatalf("被擋的請求不能轉給 api：%v", e.api.vaultCalls[before:])
	}
}

// TestBoardAllowsSettingsServices：設定頁補齊（P6-2b）新開的方法會轉給 api（by 換成看板身分）；
// package.hello 仍是 404、不轉；system.backups 帶 apply 由代理直接回 403，不轉。
// Guards: REQ-111, REQ-238
func TestBoardAllowsSettingsServices(t *testing.T) {
	e := newEnv(t, "")
	// package／agents 在假 api 沒掛：打到 api 的「/」就代表有轉過去。
	forwarded := []string{
		"/api/package/list", "/api/package/get", "/api/package/install", "/api/package/uninstall", "/api/package/set_enabled",
		"/api/package/reload", "/api/package/list_extensions", "/api/package/set_extension", "/api/package/install_go",
		"/api/package/fork", "/api/package/list_updates", "/api/package/sync", "/api/package/preview", "/api/package/outdated", "/api/package/upgrade", "/api/agents/set_field", "/api/agents/set_account",
		"/api/agents/knowledge", "/api/agents/set_knowledge",
	}
	for _, path := range forwarded {
		post(e.h, path, "{}")
	}
	e.api.mu.Lock()
	hit := strings.Join(e.api.hit, ",")
	e.api.mu.Unlock()
	for _, path := range forwarded {
		if !strings.Contains(hit+",", path+",") {
			t.Fatalf("%s 沒有轉給 api（打到的：%s）", path, hit)
		}
	}
	// 勾可選知識的身分一律換成看板（主人），由名冊判斷只有主人能改（DEV-161）。
	if !strings.Contains(hit, "set_knowledge by="+boardActor) {
		t.Fatalf("set_knowledge 的 by 要換成看板身分：%s", hit)
	}
	// system 的幾個：掛了假的，會通；set_permission_levels 的 by 一律換成 owner(web)。
	// doctor、report 是「設定 → 系統健康」的完整檢查與回報問題（DEV-153，WEB-191）：只讀、只產生內容。
	for _, path := range []string{"/api/system/trust", "/api/system/preflight", "/api/system/usage", "/api/system/backups", "/api/system/doctor", "/api/system/report"} {
		if w := post(e.h, path, "{}"); w.Code != http.StatusOK {
			t.Fatalf("%s → %d %s", path, w.Code, w.Body.String())
		}
	}
	if w := post(e.h, "/api/system/set_permission_levels", `{"default_level":"standard","by":{"name":"code"}}`); w.Code != http.StatusOK || e.api.levelsBy != "owner(web)" {
		t.Fatalf("set_permission_levels → %d by=%q", w.Code, e.api.levelsBy)
	}
	// 沒開的：package.hello、system.get_config／set_config、node.register 都是 404、不轉。
	e.api.mu.Lock()
	e.api.hit = nil
	e.api.mu.Unlock()
	for _, path := range []string{"/api/node/list_servers", "/api/system/servers", "/api/system/save_server", "/api/system/set_hub", "/api/package/hello", "/api/system/get_config", "/api/system/set_config", "/api/node/register", "/api/agents/archive"} {
		w := post(e.h, path, "{}")
		if er := decodeErr(t, w); w.Code != http.StatusNotFound || er.Message != "看板不提供這個服務" {
			t.Fatalf("%s → %d %+v", path, w.Code, er)
		}
	}
	// 備份只能看：帶 apply 的 403（含大小寫不同的 key），不帶或 false 照常。
	for _, body := range []string{`{"prune":true,"apply":true}`, `{"Apply":true}`} {
		w := post(e.h, "/api/system/backups", body)
		if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || er.Message != "看板不能刪備份；要清理，跟任何一位秘書說「幫我清理舊的設定備份」" {
			t.Fatalf("backups %s → %d %+v", body, w.Code, er)
		}
	}
	if w := post(e.h, "/api/system/backups", `{"prune":true,"apply":false}`); w.Code != http.StatusOK {
		t.Fatalf("backups apply=false → %d %s", w.Code, w.Body.String())
	}
	e.api.mu.Lock()
	defer e.api.mu.Unlock()
	if len(e.api.hit) != 0 {
		t.Fatalf("沒開放的方法不能轉到 api：%v", e.api.hit)
	}
}

// 看板開任務單（REQ-186）與排程頁（REQ-18、REQ-159）：tasks.create 通，by 一律 owner(web)；scheduler 的五個方法會轉給 api。
// Guards: REQ-186, REQ-159
func TestBoardAllowsTaskCreateAndScheduler(t *testing.T) {
	e := newEnv(t, "")
	w := post(e.h, "/api/tasks/create", `{"title":"整理桌面","agent":"code","by":{"name":"code"}}`)
	e.api.mu.Lock()
	by := e.api.createBy
	e.api.mu.Unlock()
	if w.Code != http.StatusOK || by != "owner(web)" {
		t.Fatalf("tasks.create → %d by=%q %s", w.Code, by, w.Body.String())
	}
	// scheduler 在假 api 沒掛：打到 api 的「/」就代表有轉過去。
	forwarded := []string{"/api/scheduler/list_jobs", "/api/scheduler/save_job", "/api/scheduler/set_job", "/api/scheduler/remove_job", "/api/scheduler/run_job"}
	for _, path := range forwarded {
		post(e.h, path, "{}")
	}
	e.api.mu.Lock()
	hit := strings.Join(e.api.hit, ",")
	e.api.mu.Unlock()
	for _, path := range forwarded {
		if !strings.Contains(hit+",", path+",") {
			t.Fatalf("%s 沒有轉給 api（打到的：%s）", path, hit)
		}
	}
}

// 看板可以開 Goal（D-39、REQ-202）：goals.create 轉給 api（by 一律換成 owner(web)，由 forceIdentity 保證）；
// 秘書提案的 propose、改 Planbook 的 update_planbook、done 不從看板來，仍是 404。
// Guards: REQ-202
func TestBoardAllowsGoalCreate(t *testing.T) {
	e := newEnv(t, "")
	post(e.h, "/api/goals/create", `{"title":"整理報表","text":"x","by":{"name":"code"}}`)
	if w := post(e.h, "/api/goals/options", `{}`); w.Code != http.StatusNotFound {
		t.Fatalf("goals.options 已拿掉（DEV-95）→ %d", w.Code)
	}
	for _, method := range []string{"propose", "update_planbook", "done"} {
		if w := post(e.h, "/api/goals/"+method, `{}`); w.Code != http.StatusNotFound {
			t.Fatalf("goals.%s → %d，看板不該開這個", method, w.Code)
		}
	}
	e.api.mu.Lock()
	hit := strings.Join(e.api.hit, ",")
	e.api.mu.Unlock()
	if !strings.Contains(hit, "/api/goals/create") || strings.Contains(hit, "/api/goals/propose") {
		t.Fatalf("打到 api 的路徑：%s", hit)
	}
}

// TestBoardPolicyProxy：看板的權限申請頁能列、能審；審的人一律換成 owner(web)（主人），送申請不從看板來。
// Guards: REQ-169
func TestBoardPolicyProxy(t *testing.T) {
	e := newEnv(t, "")
	if w := post(e.h, "/api/policy/list", `{"statuses":["pending"]}`); w.Code != http.StatusOK {
		t.Fatalf("policy.list → %d %s", w.Code, w.Body.String())
	}
	if w := post(e.h, "/api/policy/decide", `{"id":"P-1","approve":true,"fingerprint":"abc","by":{"name":"code"}}`); w.Code != http.StatusOK || e.api.decideBy != "owner(web)" {
		t.Fatalf("policy.decide → %d by=%q", w.Code, e.api.decideBy)
	}
}

// 規矩頁（S7、D-44）：看板存檔的身分一律換成 owner(web)（主人）；送修改提案（propose）只給秘書，看板不轉。
func TestBoardRulesSaveIsOwnerAndProposeIsNotProxied(t *testing.T) {
	e := newEnv(t, "")
	if w := post(e.h, "/api/rules/save", `{"doc":"law","text":"x","by":{"name":"code"}}`); w.Code != http.StatusOK || e.api.rulesBy != "owner(web)" {
		t.Fatalf("rules.save → %d by=%q", w.Code, e.api.rulesBy)
	}
	w := post(e.h, "/api/rules/propose", `{"doc":"law","text":"x","why":"y","by":{"name":"code"}}`)
	if er := decodeErr(t, w); w.Code != http.StatusNotFound || er.Code != sdk.ErrorCodeNotFound {
		t.Fatalf("rules.propose 不從看板來：%d %+v", w.Code, er)
	}
}

// TestBoardAllowsPersonas：角色頁的介紹用得到的十三個方法（含測口吻的 test、test_results，匯出匯入的 export、import〔WEB-151〕）、
// 衣櫃的「現在再試」（pack_sync，拿掉指標與 force）與換人設（agents.assign）會轉給 api，寫入的 by 一律換成 owner(web)；
// agents 的其他管理方法（archive、swap…）仍是 404。
// Guards: REQ-130, REQ-111, PRS-28
func TestBoardAllowsPersonas(t *testing.T) {
	e := newEnv(t, "")
	for _, method := range []string{"list", "get", "create", "copy", "update", "set_status", "check", "get_style", "set_style", "test", "test_results", "export", "import"} {
		body := `{"slug":"x","by":{"name":"code"}}`
		if w := post(e.h, "/api/personas/"+method, body); w.Code != http.StatusOK {
			t.Fatalf("personas.%s → %d %s", method, w.Code, w.Body.String())
		}
	}
	e.api.mu.Lock()
	for _, method := range []string{"create", "copy", "update", "set_status", "set_style", "export", "import"} {
		if got := e.api.personaBy[method]; got != "owner(web)" {
			t.Errorf("personas.%s 的 by = %q，要換成 owner(web)", method, got)
		}
	}
	e.api.mu.Unlock()
	// 「現在再試」（DEV-157）：by 換成 owner(web)、指標檔路徑與 force（大小寫不同的寫法也算）拿掉：一律用服務自己的指標、不蓋掉主人改過的。
	if w := post(e.h, "/api/personas/pack_sync", `{"pointer":"/tmp/evil.toml","Pointer":"/tmp/evil2.toml","force":true,"Force":true,"by":{"name":"code"}}`); w.Code != http.StatusOK {
		t.Fatalf("personas.pack_sync → %d %s", w.Code, w.Body.String())
	}
	e.api.mu.Lock()
	if got := e.api.packSync; got == nil || got.Pointer != "" || got.Force || got.By == nil || got.By.Name != "owner(web)" {
		t.Errorf("personas.pack_sync 要換身分、拿掉指標與 force：%+v", got)
	}
	e.api.mu.Unlock()
	if w := post(e.h, "/api/agents/assign", `{"id":"code","persona":"x","by":{"name":"code"}}`); w.Code != http.StatusOK || e.api.assignBy != "owner(web)" {
		t.Fatalf("agents.assign → %d by=%q", w.Code, e.api.assignBy)
	}
	for _, path := range []string{"/api/agents/archive", "/api/agents/swap", "/api/agents/rename", "/api/personas/delete"} {
		w := post(e.h, path, "{}")
		if er := decodeErr(t, w); w.Code != http.StatusNotFound || er.Message != "看板不提供這個服務" {
			t.Fatalf("%s → %d %+v", path, w.Code, er)
		}
	}
}

// Guards: REQ-104
func TestCrossOriginRejected(t *testing.T) {
	e := newEnv(t, `public_url="https://hg.example.ts.net"`+"\n")
	post1 := func(headers ...string) *httptest.ResponseRecorder {
		return post(e.h, "/api/web/get_pages", "{}", headers...)
	}
	w := post1("Origin", "https://evil.example")
	if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || er.Message != "不接受從別的網站送來的請求" {
		t.Fatalf("跨站：%d %+v", w.Code, er)
	}
	if w := post1("Origin", "null"); w.Code != http.StatusForbidden {
		t.Fatalf("Origin: null → %d", w.Code)
	}
	// 別的網域的 Origin（Host 是 127.0.0.1）不算同源；Host 與 Origin 都是攻擊者網域（DNS rebinding）的在 cors_test.go。
	if w := post1("Origin", "http://example.com"); w.Code != http.StatusForbidden {
		t.Fatalf("別的網域的 Origin → %d", w.Code)
	}
	// public_url 的 host 放行；沒有 Origin 也放行（curl、CLI）；loopback 的 Host 見 cors_test.go。
	for _, origin := range []string{"https://hg.example.ts.net", ""} {
		headers := []string{}
		if origin != "" {
			headers = []string{"Origin", origin}
		}
		if w := post1(headers...); w.Code != http.StatusOK {
			t.Fatalf("Origin %q → %d", origin, w.Code)
		}
	}
	// 同一台機器的別的 port 是另一個 Origin（請求的 Host 是 127.0.0.1，見 viaLoopback）。
	if w := post1("Origin", "http://127.0.0.1:9999"); w.Code != http.StatusForbidden {
		t.Fatalf("別的 port → %d", w.Code)
	}
	// 非 JSON 或非 POST：415。
	r := httptest.NewRequest(http.MethodPost, "/api/web/get_pages", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain → %d", rec.Code)
	}
	if rec := get(e.h, "/api/web/get_pages"); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("GET → %d", rec.Code)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/web/get_pages", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("帶 charset 的 JSON → %d", rec.Code)
	}
}

// Guards: REQ-142
func TestStreamsPassThrough(t *testing.T) {
	e := newEnv(t, "")
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	res, err := http.Post(srv.URL+"/api/session/watch", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") || !strings.Contains(string(body), `data: {"session"`) {
		t.Fatalf("%d %s %s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
}

// ---- 頭像、分享檔、靜態檔 ----

// Guards: REQ-112
func TestAvatarAndShareFiles(t *testing.T) {
	e := newEnv(t, "")
	png := []byte("\x89PNG\r\n\x1a\nfake")
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.png"), string(png))
	write(t, filepath.Join(e.home, "personas", "avatars", "secret.png"), "不該被讀到")
	write(t, filepath.Join(e.home, "runtime", "share", "a.png"), string(png))
	write(t, filepath.Join(e.home, "runtime", "share", "report.txt"), "報告")
	write(t, filepath.Join(e.home, "runtime", "share", "page.svg"), "<svg onload=alert(1)/>")
	write(t, filepath.Join(e.home, "outside.txt"), "家裡別的檔案")
	if err := os.Symlink(filepath.Join(e.home, "outside.txt"), filepath.Join(e.home, "runtime", "share", "link.png")); err != nil {
		t.Fatal(err)
	}

	w := get(e.h, "/avatars/code.png")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), png) || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("avatar: %d %v", w.Code, w.Header())
	}
	for _, p := range []string{
		"/avatars/nobody.png",          // 沒有這位
		"/avatars/life.png",            // 有這位，但人設沒有圖
		"/avatars/..%2f..%2fhome.toml", // 路徑穿越
		"/avatars/../home.toml",
		"/avatars/code.png/x",
		"/avatars/CODE.png",
		"/avatars/code",
		"/avatars/.png",
	} {
		if w := get(e.h, p); w.Code != http.StatusNotFound {
			t.Fatalf("%s → %d", p, w.Code)
		}
	}

	w = get(e.h, "/share/a.png")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Content-Disposition") != "inline" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("share png: %d %v", w.Code, w.Header())
	}
	for _, name := range []string{"report.txt", "page.svg"} {
		w = get(e.h, "/share/"+name)
		if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment") || w.Header().Get("Content-Type") != "application/octet-stream" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("share %s: %d %v", name, w.Code, w.Header())
		}
	}
	for _, p := range []string{
		"/share/..%2fhome.toml", "/share/../home.toml", "/share/..%2foutside.txt", "/share/%2e%2e%2foutside.txt",
		"/share/a%5Cb", "/share/sub/x.png", "/share/", "/share/missing.png", "/share/link.png", "/share/.hidden", "/share/%2e%2e",
	} {
		if w := get(e.h, p); w.Code != http.StatusNotFound {
			t.Fatalf("%s → %d %q", p, w.Code, w.Body.String())
		}
	}
}

// Guards: REQ-156
func TestAvatarVideoRange(t *testing.T) {
	e := newEnv(t, "")
	data := "0123456789abcdefghij"
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.mp4"), data)
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.gif"), "gif")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/avatars/code.mp4", nil)
	r.Header.Set("Range", "bytes=0-9")
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusPartialContent || w.Header().Get("Content-Range") != "bytes 0-9/20" || w.Header().Get("Content-Type") != "video/mp4" || w.Body.String() != data[:10] {
		t.Fatalf("range: %d %v %q", w.Code, w.Header(), w.Body.String())
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("缺 nosniff：%v", w.Header())
	}
	if w := get(e.h, "/avatars/code.mp4"); w.Code != http.StatusOK || w.Header().Get("Accept-Ranges") != "bytes" || w.Body.String() != data {
		t.Fatalf("整份：%d %v", w.Code, w.Header())
	}
	for _, p := range []string{"/avatars/code.gif", "/avatars/code.webm", "/avatars/life.mp4", "/avatars/code.MP4"} {
		if w := get(e.h, p); w.Code != http.StatusNotFound {
			t.Fatalf("%s → %d", p, w.Code)
		}
	}
}

// Guards: REQ-156
func TestShareVideoInline(t *testing.T) {
	e := newEnv(t, "")
	write(t, filepath.Join(e.home, "runtime", "share", "clip.mp4"), "mp4")
	write(t, filepath.Join(e.home, "runtime", "share", "clip.webm"), "webm")
	write(t, filepath.Join(e.home, "runtime", "share", "clip.MOV"), "mov")
	for name, typ := range map[string]string{"clip.mp4": "video/mp4", "clip.webm": "video/webm", "clip.MOV": "video/quicktime"} {
		w := get(e.h, "/share/"+name)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != typ || w.Header().Get("Content-Disposition") != "inline" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s: %d %v", name, w.Code, w.Header())
		}
	}
}

// Guards: REQ-107
func TestSPAFallback(t *testing.T) {
	e := newEnv(t, "")
	e.m.static = fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><div id=root></div>")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		"sw.js":                {Data: []byte("self.skipWaiting()")},
		"manifest.webmanifest": {Data: []byte(`{"name":"x"}`)},
		"assets/app-123.js":    {Data: []byte("console.log(1)")},
	}
	h := viaLoopback(e.m.Handler())
	for _, p := range []string{"/", "/chat", "/chat/code-1", "/messages/M-3", "/tasks/SEC-12", "/reminders?view=overdue", "/settings", "/settings/notify", "/no/such/page", "/index.html"} {
		w := get(h, p)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<div id=root>`) || w.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s → %d %v %q", p, w.Code, w.Header(), w.Body.String())
		}
	}
	if w := get(h, "/assets/missing.js"); w.Code != http.StatusNotFound {
		t.Fatalf("不存在的 asset 要 404：%d", w.Code)
	}
	w := get(h, "/assets/app-123.js")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset: %d %v", w.Code, w.Header())
	}
	w = get(h, "/sw.js")
	if w.Code != http.StatusOK || w.Header().Get("Service-Worker-Allowed") != "/" || w.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("sw.js: %d %v", w.Code, w.Header())
	}
	w = get(h, "/manifest.webmanifest")
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-cache" || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/manifest+json") {
		t.Fatalf("manifest: %d %v", w.Code, w.Header())
	}
	if w := get(h, "/favicon.svg"); w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "image/svg+xml") {
		t.Fatalf("favicon: %d %v", w.Code, w.Header())
	}
	// 非 GET 的靜態路徑：405，不是 index.html。
	r := httptest.NewRequest(http.MethodPost, "/chat", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /chat → %d", rec.Code)
	}
}

// Guards: REQ-107
func TestEmbeddedDistServesIndex(t *testing.T) {
	m := New(Deps{})
	w := get(viaLoopback(m.Handler()), "/chat/abc")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `<div id="root">`) {
		t.Fatalf("內嵌的 dist：%d %q", w.Code, w.Body.String())
	}
}

// Guards: REQ-108
func TestOldTabLinkIs404(t *testing.T) {
	e := newEnv(t, "")
	for _, p := range []string{"/?tab=chat&room=code", "/?tab=tasks", "/?room=x&tab=home"} {
		w := get(e.h, p)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "這是舊看板的連結，新看板不支援；請從選單進入") || !strings.Contains(w.Body.String(), `href="/"`) || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s → %d %q", p, w.Code, w.Body.String())
		}
	}
	if w := get(e.h, "/"); w.Code != http.StatusOK {
		t.Fatalf("/ → %d", w.Code)
	}
	if w := get(e.h, "/?room=x"); w.Code != http.StatusOK {
		t.Fatalf("沒有 tab 參數就正常：%d", w.Code)
	}
}

// ---- 真的監聽 ----

// Guards: REQ-104
func TestListensOnLoopbackOnly(t *testing.T) {
	e := newEnv(t, "")
	ctx := context.Background()
	if err := e.m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	stopped := false
	defer func() {
		if !stopped {
			_ = e.m.Stop(ctx)
		}
	}()
	addr := e.m.Addr()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() || host != "127.0.0.1" {
		t.Fatalf("要只聽 127.0.0.1，實際是 %s", addr)
	}
	home, err := platform.LoadHome(e.home)
	if err != nil || fmt.Sprint(home.Web.Port) != port {
		t.Fatalf("port = %s, home.toml = %d (%v)", port, home.Web.Port, err)
	}
	res, err := http.Post("http://"+addr+"/api/web/get_pages", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "core.overview") {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, err = http.Get("http://" + addr + "/chat"); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("SPA: %v %v", res, err)
	}
	_ = res.Body.Close()
	if err := e.m.Start(ctx); err == nil || platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("重複 Start 要回 conflict：%v", err)
	}
	if err := e.m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	stopped = true
	if c, err := net.Dial("tcp", addr); err == nil {
		_ = c.Close()
		t.Fatal("Stop 之後不該還在聽")
	}
	if e.m.Addr() != "" {
		t.Fatalf("Addr = %q", e.m.Addr())
	}
}

// Guards: REQ-104
func TestPortInUseFails(t *testing.T) {
	e := newEnv(t, "")
	home, err := platform.LoadHome(e.home)
	if err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", home.Web.Port))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = busy.Close() }()
	err = e.m.Start(context.Background())
	want := fmt.Sprintf("看板的連接埠 %d 已被占用，請改 home.toml 的 web.port", home.Web.Port)
	var pe *platform.Err
	if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeUnavailable || pe.Message != want {
		t.Fatalf("err = %v", err)
	}
	if e.m.Addr() != "" {
		t.Fatal("沒聽成功就不該有位址")
	}
	if err := e.m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-104
func TestNoHomeDoesNotListen(t *testing.T) {
	m := New(Deps{})
	if err := m.Start(context.Background()); err != nil || m.Addr() != "" {
		t.Fatalf("沒有家就不聽：%v %q", err, m.Addr())
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// ---- 聊天附件上傳（REQ-82） ----

func upload(h http.Handler, body []byte, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/web/upload", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/octet-stream")
	r.Header.Set("X-Requested-With", "evergenie")
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i+1] == "" {
			r.Header.Del(headers[i])
		} else {
			r.Header.Set(headers[i], headers[i+1])
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// Guards: REQ-82
func TestUploadStoresInShare(t *testing.T) {
	e := newEnv(t, "")
	png := []byte("\x89PNG\r\n\x1a\nfake")
	w := upload(e.h, png, "X-Filename", url.PathEscape("早餐 照片(1).png"))
	res := decode[struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Size int    `json:"size"`
	}](t, w)
	if !regexp.MustCompile(`^20261024-090000-[0-9a-f]{4}-早餐_照片_1_.png$`).MatchString(res.Name) {
		t.Fatalf("存檔名：%q", res.Name)
	}
	if res.URL != "/share/"+res.Name || res.Size != len(png) {
		t.Fatalf("回應：%+v", res)
	}
	file := filepath.Join(e.home, "runtime", "share", res.Name)
	got, err := os.ReadFile(file)
	if err != nil || !bytes.Equal(got, png) {
		t.Fatalf("檔案內容：%v %q", err, got)
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
		t.Fatalf("檔案權限：%v", info.Mode())
	}
	if info, _ := os.Stat(filepath.Dir(file)); info.Mode().Perm() != 0o700 {
		t.Fatalf("資料夾權限：%v", info.Mode())
	}
	// 存進去的檔案馬上能從 /share 取回（圖片 inline）。
	back := get(e.h, res.URL)
	if back.Code != http.StatusOK || back.Header().Get("Content-Type") != "image/png" || !bytes.Equal(back.Body.Bytes(), png) {
		t.Fatalf("/share 取回：%d %v", back.Code, back.Header())
	}
	// 檔名處理：資料夾被去掉、連續的點不會讓 /share 打不開、沒檔名叫 file、其他類型照存（下載）。
	for header, want := range map[string]string{
		url.PathEscape("../../etc/passwd"): "-passwd",
		url.PathEscape("a..b.pdf"):         "-a.b.pdf",
		"":                                 "-file",
	} {
		w := upload(e.h, []byte("x"), "X-Filename", header)
		r := decode[struct {
			Name string `json:"name"`
		}](t, w)
		if !strings.HasSuffix(r.Name, want) {
			t.Fatalf("X-Filename %q → %q，要以 %q 結尾", header, r.Name, want)
		}
		if g := get(e.h, "/share/"+r.Name); g.Code != http.StatusOK {
			t.Fatalf("%q 存了卻打不開：%d", r.Name, g.Code)
		}
	}
}

// Guards: REQ-82
func TestUploadRejectsCrossSiteAndOversize(t *testing.T) {
	e := newEnv(t, `public_url="https://hg.example.ts.net"`+"\n")
	shared := filepath.Join(e.home, "runtime", "share")
	cases := []struct {
		name    string
		w       *httptest.ResponseRecorder
		status  int
		message string
	}{
		{"跨站", upload(e.h, []byte("x"), "Origin", "https://evil.example"), http.StatusForbidden, "不接受從別的網站送來的請求"},
		{"沒帶 X-Requested-With", upload(e.h, []byte("x"), "X-Requested-With", ""), http.StatusForbidden, "缺少必要的標頭，請從看板上傳"},
		{"標頭值不對", upload(e.h, []byte("x"), "X-Requested-With", "fetch"), http.StatusForbidden, "缺少必要的標頭，請從看板上傳"},
		{"空檔", upload(e.h, nil), http.StatusBadRequest, "檔案是空的"},
		{"超過 20 MB", upload(e.h, make([]byte, 20<<20+1)), http.StatusRequestEntityTooLarge, "檔案太大（上限 20 MB）"},
	}
	for _, c := range cases {
		if er := decodeErr(t, c.w); c.w.Code != c.status || er.Message != c.message {
			t.Fatalf("%s：%d %+v", c.name, c.w.Code, er)
		}
	}
	if entries, _ := os.ReadDir(shared); len(entries) != 0 {
		t.Fatalf("被擋的上傳不該留檔案，共 %d 個", len(entries))
	}
	// 剛好 20 MB 可以；同源與 public_url 的 Origin 放行；GET 不行。
	if w := upload(e.h, make([]byte, 20<<20)); w.Code != http.StatusOK {
		t.Fatalf("20 MB 整：%d %s", w.Code, w.Body.String())
	}
	if w := upload(e.h, []byte("x"), "Origin", "https://hg.example.ts.net"); w.Code != http.StatusOK {
		t.Fatalf("public_url 的 Origin：%d", w.Code)
	}
	if w := get(e.h, "/api/web/upload"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET：%d", w.Code)
	}
}

// 看板訂 bus 事件只限 vault.changed（知道哪種資料變了，好重抓）；
// 空的（＝全部）、萬用字元、其他事件、大小寫不同的重複 key 偷渡都擋，不轉給 api。
// Guards: REQ-389
func TestBoardBusSubscribeOnlyChangeTopics(t *testing.T) {
	e := newEnv(t, "")
	for _, body := range []string{`{"topics":["vault.changed"]}`} {
		if w := post(e.h, "/api/bus/subscribe", body); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "vault.changed") {
			t.Fatalf("%s → %d %s", body, w.Code, w.Body.String())
		}
	}
	before := len(e.api.subTopics)
	for _, body := range []string{
		`{}`, `{"topics":[]}`, `{"topics":["*"]}`, `{"topics":["vault.*"]}`, `{"topics":["system.alert"]}`,
		`{"topics":["vault.changed","bus.envelope.state"]}`, `{"topics":["system.alert"],"Topics":["vault.changed"]}`,
		`{"Topics":["vault.changed"],"topics":["system.alert"]}`, `{`, `null`,
	} {
		w := post(e.h, "/api/bus/subscribe", body)
		if w.Code == http.StatusOK {
			t.Errorf("%s 不該通：%s", body, w.Body.String())
		}
	}
	if got := e.api.subTopics[before:]; len(got) != 0 {
		t.Fatalf("被擋的訂閱不能轉給 api：%v", got)
	}
}

// DEV-145：第 2 版的家，附件存在 <家>/media/share/、從那裡伺服；官方頭像讀狀態資料夾的 personas/avatars/。
func TestUploadAndAvatarFollowLayoutV2(t *testing.T) {
	state := t.TempDir()
	e := newEnv(t, "\n[layout]\nversion = 2\nstate = \""+state+"\"\n")
	png := []byte("\x89PNG\r\n\x1a\nfake")
	res := decode[struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}](t, upload(e.h, png, "X-Filename", "a.png"))
	if got, err := os.ReadFile(filepath.Join(e.home, "media", "share", res.Name)); err != nil || !bytes.Equal(got, png) {
		t.Fatalf("附件要在 media/share：%v", err)
	}
	if back := get(e.h, res.URL); back.Code != http.StatusOK || !bytes.Equal(back.Body.Bytes(), png) {
		t.Fatalf("/share 取回：%d", back.Code)
	}
	write(t, filepath.Join(state, "personas", "avatars", "builder.png"), string(png))
	if w := get(e.h, "/avatars/code.png"); w.Code != http.StatusOK || !bytes.Equal(w.Body.Bytes(), png) {
		t.Fatalf("官方頭像要讀狀態資料夾：%d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(e.home, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("家裡不該有 runtime/：%v", err)
	}
}
