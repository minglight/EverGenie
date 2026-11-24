package requests

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

var ctx = context.Background()

type fakeBus struct {
	mu     sync.Mutex
	events []*sdk.BusSubscribeResponse
	sent   []*sdk.Envelope
	input  chan *sdk.BusSubscribeResponse
}

func (b *fakeBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return b.input, nil
}
func (b *fakeBus) Publish(_ context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, &sdk.BusSubscribeResponse{Topic: topic, Payload: p})
	return nil
}
func (b *fakeBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent = append(b.sent, e)
	return e, nil
}
func (b *fakeBus) messages() []*sdk.Envelope {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*sdk.Envelope(nil), b.sent...)
}

type fakeNotify struct {
	store *vault.Module
	sent  []*sdk.Notification
}

func (n *fakeNotify) Send(ctx context.Context, in *sdk.Notification) (*sdk.Notification, error) {
	id, err := n.store.NextID(ctx, "message")
	if err != nil {
		return nil, err
	}
	if _, err := n.store.Put(ctx, vault.Record{Kind: "message", ID: id, Fields: map[string]any{}}, in.By, ""); err != nil {
		return nil, err
	}
	out := *in
	out.ID = id
	n.sent = append(n.sent, &out)
	return &out, nil
}

// fakeEngine 記錄每次推送時看到的 guidance；fail 是接下來要失敗的次數。
type fakeEngine struct {
	mu     sync.Mutex
	m      *Module
	pushes [][]string
	fail   int
}

func (e *fakeEngine) PushPolicy(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pushes = append(e.pushes, e.m.ActiveGuidance())
	if e.fail > 0 {
		e.fail--
		return platform.Unavailable("寫不進設定檔")
	}
	return nil
}

func fixture(t *testing.T) (*Module, *homefixture.Home, *fakeNotify, *fakeBus, *fakeEngine) {
	t.Helper()
	h := homefixture.New(t)
	h.Vault.RegisterSchema(vault.Spec{Kind: "message"})
	h.Vault.RegisterSchema(vault.Spec{Kind: "agent", Path: func(id string) string { return "vault/agents/" + id + "/agent.md" }})
	for _, id := range []string{"code", "life"} {
		if _, err := h.Vault.Put(ctx, vault.Record{Kind: "agent", ID: id, Fields: map[string]any{}}, "system", ""); err != nil {
			t.Fatal(err)
		}
	}
	n := &fakeNotify{store: h.Vault}
	b := &fakeBus{input: make(chan *sdk.BusSubscribeResponse, 8)}
	e := &fakeEngine{}
	m := New(Deps{Vault: h.Vault, Notify: n, Bus: b, Engine: e, Clock: h.Clock})
	e.m = m
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return m, h, n, b, e
}

func ruleReq(scope string) *sdk.PolicyRequestRequest {
	return &sdk.PolicyRequestRequest{Kind: sdk.PolicyKindRule, Action: sdk.ActionClassWrite, Scope: scope, Why: "改 EverGenie 的檔案被擋了兩次"}
}

func code(err error) sdk.ErrorCode { return platform.CodeOf(err) }

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m := New(Deps{})
	if m.Name() != "requests" {
		t.Fatal(m.Name())
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-167
func TestRequestOnlyWritesRequest(t *testing.T) {
	m, _, n, b, e := fixture(t)
	q, err := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if err != nil {
		t.Fatal(err)
	}
	if q.ID != "P-1" || q.Status != sdk.PolicyStatusPending || q.Agent != "code" || q.From != "code" || q.Broad || q.Fingerprint == "" || q.Notice != "M-1" {
		t.Fatalf("request = %+v", q)
	}
	if !strings.Contains(q.Impact, "code") || !strings.Contains(q.Impact, "~/Workspace/amy/evergenie") {
		t.Fatalf("impact = %q", q.Impact)
	}
	if len(n.sent) != 1 || n.sent[0].Category != sdk.CategoryApprove || !reflect.DeepEqual(n.sent[0].Options, []string{"核准", "拒絕", "晚點再看"}) {
		t.Fatalf("notice = %+v", n.sent)
	}
	// 申請前後政策完全相同：沒有規則、沒有 guidance、沒有推給引擎。
	if len(e.pushes) != 0 || m.ActiveRules("code") != nil || m.ActiveGuidance() != nil {
		t.Fatal("request changed the policy")
	}
	list, err := m.List(ctx, []sdk.PolicyStatus{sdk.PolicyStatusPending}, "")
	if err != nil || len(list) != 1 || list[0].ID != "P-1" {
		t.Fatalf("list = %+v %v", list, err)
	}
	if len(b.events) != 1 || b.events[0].Topic != "policy.changed" {
		t.Fatal("missing policy.changed")
	}
}

type fakeNames map[string]string

func (f fakeNames) PersonaName(_ context.Context, id string) (string, error) {
	if n, ok := f[id]; ok {
		return n, nil
	}
	return "", platform.NotFound("找不到 " + id)
}

// 通知標題與影響說明寫秘書的名字，不寫 id（NTF-61）；查不到名字的照寫 id。
func TestRequestNoticeUsesAgentNames(t *testing.T) {
	m, _, n, _, _ := fixture(t)
	m.d.Names = fakeNames{"code": "Builder"}
	q, err := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(q.Impact, "Builder 之後新開") || q.Agent != "code" {
		t.Fatalf("impact = %q agent=%q", q.Impact, q.Agent)
	}
	title := n.sent[len(n.sent)-1].Title
	if !strings.Contains(title, "Builder 送的，Builder 想要") || strings.Contains(title, "code") {
		t.Fatalf("title = %q", title)
	}
	other, err := m.Request(ctx, &sdk.PolicyRequestRequest{Kind: sdk.PolicyKindRule, Action: sdk.ActionClassRead, Scope: "~/Workspace/amy/x", Why: "要讀"}, "life")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(other.Impact, "life 之後新開") {
		t.Fatalf("查不到名字照寫 id：%q", other.Impact)
	}
}

// Guards: REQ-167
func TestRequestValidation(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	cases := map[string]*sdk.PolicyRequestRequest{
		"empty why":        {Kind: sdk.PolicyKindRule, Action: sdk.ActionClassWrite, Scope: "~/x/y"},
		"bad kind":         {Kind: "everything", Why: "x"},
		"send_external":    {Kind: sdk.PolicyKindRule, Action: sdk.ActionClassSendExternal, Why: "x"},
		"relative scope":   {Kind: sdk.PolicyKindRule, Action: sdk.ActionClassRead, Scope: "repo", Why: "x"},
		"dotdot scope":     {Kind: sdk.PolicyKindRule, Action: sdk.ActionClassRead, Scope: "~/a/../b", Why: "x"},
		"glob scope":       {Kind: sdk.PolicyKindRule, Action: sdk.ActionClassRead, Scope: "~/a/*", Why: "x"},
		"unknown agent":    {Kind: sdk.PolicyKindRule, Action: sdk.ActionClassRead, Agent: "ghost", Scope: "~/a/b", Why: "x"},
		"empty guidance":   {Kind: sdk.PolicyKindGuidance, Why: "x"},
		"guidance + scope": {Kind: sdk.PolicyKindGuidance, Text: "可以跑 make ci", Scope: "~/a", Why: "x"},
		"multiline":        {Kind: sdk.PolicyKindGuidance, Text: "a\nb", Why: "x"},
	}
	for name, in := range cases {
		if _, err := m.Request(ctx, in, "code"); code(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// 過寬不拒收，標 broad（作者 2026-10-24：都要問主人）。
	for _, scope := range []string{"", "/", "~", "~/Workspace", "/Users"} {
		q, err := m.Request(ctx, ruleReq(scope), "code")
		if err != nil || !q.Broad || !strings.HasPrefix(q.Impact, "⚠") {
			t.Errorf("scope %q: %+v %v", scope, q, err)
		}
	}
	if q, err := m.Request(ctx, ruleReq("~/Workspace/amy"), "code"); err != nil || q.Broad {
		t.Errorf("narrow scope marked broad: %+v %v", q, err)
	}
}

// Guards: REQ-167
func TestPendingLimitPerSender(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	for i := 0; i < MaxPendingPerSender; i++ {
		if _, err := m.Request(ctx, ruleReq("~/Workspace/amy/a"), "code"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.Request(ctx, ruleReq("~/Workspace/amy/a"), "code"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("11th request: %v", err)
	}
	if _, err := m.Request(ctx, ruleReq("~/Workspace/amy/a"), "life"); err != nil {
		t.Fatalf("other sender blocked: %v", err)
	}
}

// Guards: REQ-168
func TestOnlyOwnerDecidesAndRevokes(t *testing.T) {
	m, _, _, _, e := fixture(t)
	q, err := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if err != nil {
		t.Fatal(err)
	}
	for _, by := range []string{"code", "life", "system", ""} {
		if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint}, by); code(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("decide by %q: %v", by, err)
		}
	}
	got, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint}, "owner(web)")
	if err != nil || got.Status != sdk.PolicyStatusApplied {
		t.Fatalf("owner decide: %+v %v", got, err)
	}
	if _, err := m.Revoke(ctx, q.ID, "", "code"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("revoke by agent: %v", err)
	}
	if len(e.pushes) != 0 {
		t.Fatal("a rule must not rewrite the generated settings")
	}
}

// Guards: REQ-168
func TestApproveRuleTakesEffectForNewSessions(t *testing.T) {
	m, _, _, b, _ := fixture(t)
	q, err := m.Request(ctx, ruleReq("~/Workspace/amy"), "code")
	if err != nil {
		t.Fatal(err)
	}
	// 改後核准：只能改窄。
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint, Scope: "~/Documents"}, "owner(web)"); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("wider scope accepted: %v", err)
	}
	got, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint, Scope: "~/Workspace/amy/evergenie", Note: "只給這個 repo"}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sdk.PolicyStatusApplied || got.Scope != "~/Workspace/amy/evergenie" || !got.Permanent || !got.ExpiresAt.IsZero() || got.DecidedBy != "owner(web)" {
		t.Fatalf("applied = %+v", got)
	}
	if rules := m.ActiveRules("code"); !reflect.DeepEqual(rules, []sdk.PolicyRule{{Agent: "code", Action: sdk.ActionClassWrite, Scope: "~/Workspace/amy/evergenie"}}) {
		t.Fatalf("rules = %+v", rules)
	}
	if m.ActiveRules("life") != nil {
		t.Fatal("rule leaked to another agent")
	}
	msgs := b.messages()
	if len(msgs) != 1 || msgs[0].To != "code" || msgs[0].From != "system" || msgs[0].Kind != sdk.EnvelopeKindEcho || !strings.Contains(msgs[0].Body, "核准") || !strings.Contains(msgs[0].Body, "新開") {
		t.Fatalf("result message = %+v", msgs)
	}
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: false}, "owner(web)"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("decided twice: %v", err)
	}
}

// Guards: REQ-168
func TestApproveNeedsFreshFingerprint(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	a, _ := m.Request(ctx, ruleReq("~/Workspace/amy/a"), "code")
	c, _ := m.Request(ctx, ruleReq("~/Workspace/amy/c"), "code")
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: a.ID, Approve: true}, "owner(web)"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("missing fingerprint: %v", err)
	}
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: a.ID, Approve: true, Fingerprint: a.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	// 有效規則變了（a 套用了），c 在那之前看到的指紋就過期了。
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: c.ID, Approve: true, Fingerprint: c.Fingerprint}, "owner(web)"); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("stale fingerprint: %v", err)
	}
	fresh, err := m.Get(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: c.ID, Approve: true, Fingerprint: fresh.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-168
func TestGuidanceApplyAndRevokeRestores(t *testing.T) {
	m, _, _, _, e := fixture(t)
	q, err := m.Request(ctx, &sdk.PolicyRequestRequest{Kind: sdk.PolicyKindGuidance, Text: "在 ~/Workspace 底下跑 make ci 是主人要的", Why: "make ci 被誤擋三次"}, "code")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Impact, "所有秘書") {
		t.Fatalf("guidance impact must say who is affected: %q", q.Impact)
	}
	before := m.ActiveGuidance()
	got, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Permanent: true, Fingerprint: q.Fingerprint}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sdk.PolicyStatusApplied || !got.Permanent || !got.ExpiresAt.IsZero() {
		t.Fatalf("applied = %+v", got)
	}
	if len(e.pushes) != 1 || !reflect.DeepEqual(e.pushes[0], []string{"在 ~/Workspace 底下跑 make ci 是主人要的"}) {
		t.Fatalf("pushes = %v", e.pushes)
	}
	if _, err := m.Revoke(ctx, q.ID, "不需要了", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if after := m.ActiveGuidance(); !reflect.DeepEqual(after, before) || len(e.pushes) != 2 || len(e.pushes[1]) != 0 {
		t.Fatalf("revoke did not restore: %v %v", after, e.pushes)
	}
	r, _ := m.Get(ctx, q.ID)
	if r.Status != sdk.PolicyStatusRevoked {
		t.Fatalf("status = %s", r.Status)
	}
}

// Guards: REQ-168
func TestApplyFailureRollsBack(t *testing.T) {
	m, _, _, b, e := fixture(t)
	q, _ := m.Request(ctx, &sdk.PolicyRequestRequest{Kind: sdk.PolicyKindGuidance, Text: "可以讀 ~/Downloads 的檔案", Why: "讀附件被擋"}, "life")
	e.fail = 1
	got, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != sdk.PolicyStatusFailed || !strings.Contains(got.Note, "寫不進設定檔") {
		t.Fatalf("failed = %+v", got)
	}
	// 推了兩次：一次失敗、一次還原成原本（空的）。
	if len(e.pushes) != 2 || len(e.pushes[1]) != 0 || m.ActiveGuidance() != nil {
		t.Fatalf("not restored: %v", e.pushes)
	}
	if msgs := b.messages(); len(msgs) != 1 || msgs[0].To != "life" || !strings.Contains(msgs[0].Body, "失敗") {
		t.Fatalf("result message = %+v", msgs)
	}
}

// Guards: REQ-168
func TestApprovedNeverExpires(t *testing.T) {
	m, h, _, b, _ := fixture(t)
	q, _ := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	before := len(b.messages())
	m.Tick(ctx, h.Clock.Now().Add(365*24*time.Hour))
	if len(m.ActiveRules("code")) != 1 {
		t.Fatal("approved grant expired")
	}
	r, _ := m.Get(ctx, q.ID)
	if r.Status != sdk.PolicyStatusApplied || !r.Permanent || !r.ExpiresAt.IsZero() {
		t.Fatalf("request = %+v", r)
	}
	if msgs := b.messages(); len(msgs) != before {
		t.Fatalf("extra messages after tick: %+v", msgs)
	}
}

// Guards: REQ-168
func TestAppliedSurvivesRestart(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	q, _ := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: true, Fingerprint: q.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.setActives(nil)
	m.mu.Unlock()
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if len(m.ActiveRules("code")) != 1 {
		t.Fatal("applied grant lost after restart")
	}
}

// Guards: REQ-169
func TestNoticeAnswerDecides(t *testing.T) {
	m, _, _, _, _ := fixture(t)
	a, _ := m.Request(ctx, ruleReq("~/Workspace/amy/a"), "code")
	c, _ := m.Request(ctx, ruleReq("~/Workspace/amy/c"), "code")
	d, _ := m.Request(ctx, ruleReq("~/Workspace/amy/d"), "code")
	for _, ev := range []struct{ id, text, via string }{{a.Notice, "核准", "button"}, {c.Notice, "拒絕", "button"}, {d.Notice, "晚點再看", "button"}, {d.Notice, "核准", "auto"}} {
		if err := m.handleAnswer(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": ev.id, "text": ev.text, "via": ev.via}}); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]sdk.PolicyStatus{a.ID: sdk.PolicyStatusApplied, c.ID: sdk.PolicyStatusRejected, d.ID: sdk.PolicyStatusPending}
	for id, s := range want {
		got, err := m.Get(ctx, id)
		if err != nil || got.Status != s {
			t.Errorf("%s = %v %v", id, got, err)
		}
		if s != sdk.PolicyStatusPending && got.DecidedBy != "owner(notice)" {
			t.Errorf("%s decided by %q", id, got.DecidedBy)
		}
	}
}

// Guards: REQ-167
func TestNotStartedIsUnavailable(t *testing.T) {
	m := New(Deps{})
	if _, err := m.Request(ctx, ruleReq("~/a/b"), "code"); code(err) != sdk.ErrorCodeUnavailable {
		t.Fatal(err)
	}
	var pe *platform.Err
	_, err := m.List(ctx, nil, "")
	if !errors.As(err, &pe) {
		t.Fatal(err)
	}
}

func digests(n *fakeNotify) []*sdk.Notification {
	var out []*sdk.Notification
	for _, s := range n.sent {
		if s.Category == sdk.CategoryDigest {
			out = append(out, s)
		}
	}
	return out
}

// Guards: REQ-393
func TestWeeklyDigestListsGrantChanges(t *testing.T) {
	m, h, n, _, _ := fixture(t) // 時鐘在 2026-10-23（週五）12:00
	keep, _ := m.Request(ctx, ruleReq("~/Workspace/amy/evergenie"), "code")
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: keep.ID, Approve: true, Fingerprint: keep.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	gone, _ := m.Request(ctx, &sdk.PolicyRequestRequest{Kind: sdk.PolicyKindGuidance, Text: "在 ~/Workspace 底下跑 make ci 是主人要的", Why: "被擋兩次"}, "code")
	if _, err := m.Decide(ctx, &sdk.PolicyDecideRequest{ID: gone.ID, Approve: true, Fingerprint: gone.Fingerprint}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Revoke(ctx, gone.ID, "", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	loc := h.Clock.Now().Location()
	m.Tick(ctx, time.Date(2026, 10, 26, 8, 59, 0, 0, loc))
	if len(digests(n)) != 0 {
		t.Fatal("週一 09:00 前不該送")
	}
	m.Tick(ctx, time.Date(2026, 10, 26, 9, 0, 0, 0, loc))
	got := digests(n)
	if len(got) != 1 {
		t.Fatalf("週一 09:00 要送一則摘要，got %d", len(got))
	}
	for _, want := range []string{keep.ID + " 已套用", gone.ID + " 已撤銷", "目前套用中 1 張"} {
		if !strings.Contains(got[0].Context, want) {
			t.Fatalf("摘要少了 %q：\n%s", want, got[0].Context)
		}
	}
	// 同一週不重送（重開也一樣：紀錄在檔案裡）。
	m.Tick(ctx, time.Date(2026, 10, 27, 10, 0, 0, 0, loc))
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	m.Tick(ctx, time.Date(2026, 10, 28, 10, 0, 0, 0, loc))
	// 下一週沒有變動：不送。
	m.Tick(ctx, time.Date(2026, 11, 2, 9, 30, 0, 0, loc))
	if len(digests(n)) != 1 {
		t.Fatalf("同一週或沒有變動不該再送，got %d", len(digests(n)))
	}
}

// 每分鐘的 Tick 是 Jobs() 登記的定時工作（timer 的 JobRunner 跑）；還沒啟動時跑也不出事。
// Guards: REQ-168
func TestJobsTickEveryMinute(t *testing.T) {
	jobs := New(Deps{Clock: platform.SystemClock{}}).Jobs()
	if len(jobs) != 1 || jobs[0].Name != "policy.tick" || jobs[0].Every != time.Minute {
		t.Fatalf("%+v", jobs)
	}
	if err := jobs[0].Run(ctx); err != nil {
		t.Fatal(err)
	}
}
