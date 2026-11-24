package goal

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
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
var update = flag.Bool("update", false, "update Goal text fixtures")

type fakeBus struct {
	mu        sync.Mutex
	events    []*sdk.BusSubscribeResponse
	input     chan *sdk.BusSubscribeResponse
	published chan struct{}
}

func (b *fakeBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return b.input, nil
}
func (b *fakeBus) Publish(_ context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, &sdk.BusSubscribeResponse{Topic: topic, Payload: p})
	select {
	case b.published <- struct{}{}:
	default:
	}
	return nil
}
func (b *fakeBus) count() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.events) }

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
func fixture(t *testing.T) (*Module, *homefixture.Home, *fakeNotify, *fakeBus) {
	t.Helper()
	h := homefixture.New(t)
	h.Vault.RegisterSchema(vault.Spec{Kind: "message"})
	n := &fakeNotify{store: h.Vault}
	b := &fakeBus{input: make(chan *sdk.BusSubscribeResponse, 8), published: make(chan struct{}, 32)}
	m := New(Deps{Vault: h.Vault, Notify: n, Bus: b, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return m, h, n, b
}
func propose(t *testing.T, m *Module) Goal {
	t.Helper()
	g, err := m.Propose(ctx, ProposeSpec{Criteria: "測試驗收條件", Title: "測試目標", Text: "保留主人的原話", Agent: "code", By: "code"})
	if err != nil {
		t.Fatal(err)
	}
	return g
}
func respond(t *testing.T, m *Module, g Goal, text, by string) Goal {
	t.Helper()
	p := map[string]any{"id": g.Notice, "text": text, "via": "button"}
	if by != "" {
		p["by"] = by
	}
	if err := m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: p}); err != nil {
		t.Fatal(err)
	}
	out, err := m.Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func active(t *testing.T, m *Module) Goal {
	t.Helper()
	return respond(t, m, propose(t, m), "確認", "owner(test)")
}

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m := New(Deps{})
	if m.Name() != "goal" {
		t.Fatal(m.Name())
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-152
func TestProposeSendsApprove(t *testing.T) {
	m, h, n, b := fixture(t)
	g := propose(t, m)
	if g.State != sdk.GoalStateDraft || g.ID != "GOAL-1" || g.Notice != "M-1" {
		t.Fatal("draft fields")
	}
	notice := n.sent[0]
	if notice.Category != sdk.CategoryApprove || notice.Agent != "code" || notice.By != "code" || notice.Todo == "" || !reflect.DeepEqual(notice.Options, []string{"確認", "不要"}) || !strings.Contains(notice.Context, "「保留主人的原話」") {
		t.Fatal("confirmation notice")
	}
	if b.count() != 1 {
		t.Fatal("missing goal.changed")
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/goals/GOAL-1.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Propose(ctx, ProposeSpec{Criteria: "測試驗收條件", Title: "x", Text: " ", Agent: "code"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal("empty text accepted")
	}
}

// 家的 [ids] goals = "DG-"（工坊）：新 Goal 編號是 DG-1，Mentioned 認 DG-1、不認別的家的 GOAL-1。
func TestGoalUsesHomePrefix(t *testing.T) {
	h := homefixture.NewWith(t, "[ids]\ntasks=\"DEV-\"\ngoals=\"DG-\"\n")
	h.Vault.RegisterSchema(vault.Spec{Kind: "message"})
	b := &fakeBus{input: make(chan *sdk.BusSubscribeResponse, 8), published: make(chan struct{}, 32)}
	m := New(Deps{Vault: h.Vault, Notify: &fakeNotify{store: h.Vault}, Bus: b, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	g, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "工坊", Text: "開發跟日常分開", By: "owner(web)"})
	if err != nil || g.ID != "DG-1" {
		t.Fatalf("編號 %q（要 DG-1）：%v", g.ID, err)
	}
}

// 主人自己開的 Goal 直接是 active：不發確認通知、原文照存、確認的人就是主人（D-39）。
// Guards: REQ-202
func TestOwnerCreatesGoalActive(t *testing.T) {
	m, h, n, b := fixture(t)
	g, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "整理報表", Text: "把三月的報表整理好", By: "owner(web)"})
	if err != nil {
		t.Fatal(err)
	}
	if g.State != sdk.GoalStateActive || g.ID != "GOAL-1" || g.ConfirmedBy != "owner(web)" || !g.ConfirmedAt.Equal(h.Clock.Now()) || g.Notice != "" || g.By != "owner(web)" {
		t.Fatalf("owner goal fields: %+v", g)
	}
	if len(n.sent) != 0 {
		t.Fatal("owner-created goal must not send a confirmation notice")
	}
	if b.count() != 1 {
		t.Fatal("missing goal.changed")
	}
	got, err := m.Get(ctx, g.ID)
	if err != nil || got.Text != "把三月的報表整理好" || got.Title != "整理報表" {
		t.Fatalf("stored goal: %+v %v", got, err)
	}
	// 原文建立後誰都不能改（GOL-2 不變）。
	old, r, err := m.load(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	old.Text = "改過的"
	if _, err := m.save(ctx, old, r, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal("owner goal text was editable")
	}
	// 一建好就算進行中：Planbook 可以寫。
	if _, err := m.UpdatePlanbook(ctx, g.ID, "第一步", "開始", "code"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "x", Text: " ", By: "owner(web)"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal("empty text accepted")
	}
	if _, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: " ", Text: "x", By: "owner(web)"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal("empty title accepted")
	}
}

// 秘書不能直接開 Goal（只能提案、等主人確認）；被拒後什麼都沒寫。
// Guards: REQ-202
func TestAgentCannotCreateGoal(t *testing.T) {
	m, h, n, b := fixture(t)
	for _, by := range []string{"code", "", "owner", "xowner(web)"} {
		_, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "偷開", Text: "秘書自己開的", By: by})
		if platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("by %q: want forbidden, got %v", by, err)
		}
	}
	if len(n.sent) != 0 || b.count() != 0 {
		t.Fatal("a refused create left side effects")
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/goals/GOAL-1.md")); !os.IsNotExist(err) {
		t.Fatalf("a refused create wrote a file: %v", err)
	}
}

// Guards: REQ-152
func TestConfirmActivates(t *testing.T) {
	m, h, _, b := fixture(t)
	g := propose(t, m)
	g = respond(t, m, g, "確認", "")
	if g.State != sdk.GoalStateActive || g.ConfirmedBy != "owner(web)" || !g.ConfirmedAt.Equal(h.Clock.Now()) {
		t.Fatal("confirmation fields")
	}
	count := b.count()
	respond(t, m, g, "不要", "owner(test)")
	if b.count() != count {
		t.Fatal("duplicate answer changed active goal")
	}
}

// Guards: REQ-152
func TestDeclineWithdraws(t *testing.T) {
	m, h, _, _ := fixture(t)
	g := respond(t, m, propose(t, m), "不要", "owner(test)")
	if g.State != sdk.GoalStateWithdrawn || g.EndedBy != "owner(test)" || !g.EndedAt.Equal(h.Clock.Now()) {
		t.Fatal("decline fields")
	}
}

// Guards: REQ-152
func TestTextImmutable(t *testing.T) {
	m, h, _, _ := fixture(t)
	g := active(t, m)
	path := filepath.Join(h.Dir, "vault/goals", g.ID+".md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"title", "text"} {
		old, r, err := m.load(ctx, g.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch name {
		case "title":
			old.Title = "changed"
		case "text":
			old.Text = "changed"
		}
		if _, err := m.save(ctx, old, r, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%s mutation allowed", name)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("immutable write changed file")
		}
	}
	request := reflect.TypeOf(sdk.GoalsUpdatePlanbookRequest{})
	for _, name := range []string{"Title", "Text", "Grants"} { // Grants 已拿掉（DEV-95），仍守住 planbook API 不能帶原文欄位
		if _, ok := request.FieldByName(name); ok {
			t.Fatal("planbook API can change original")
		}
	}
}

// Guards: REQ-152
func TestPlanbookLogged(t *testing.T) {
	m, h, _, _ := fixture(t)
	draft := propose(t, m)
	if _, err := m.UpdatePlanbook(ctx, draft.ID, "x", "n", "code"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal("draft planbook changed")
	}
	g := respond(t, m, draft, "確認", "owner(test)")
	for i, by := range []string{"code", "work"} {
		h.Clock.Advance(time.Minute)
		out, err := m.UpdatePlanbook(ctx, g.ID, "新的 Planbook", by+" 接手", by)
		if err != nil {
			t.Fatal(err)
		}
		if len(out.PlanbookLog) != i+1 || out.PlanbookLog[i].By != by || !out.PlanbookLog[i].At.Equal(h.Clock.Now()) || out.Text != g.Text || out.Title != g.Title {
			t.Fatal("planbook log or original incorrect")
		}
	}
}

// Guards: REQ-152
func TestOnlyOwnerWithdraws(t *testing.T) {
	m, _, _, _ := fixture(t)
	g := active(t, m)
	if _, err := m.Withdraw(ctx, g.ID, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal("agent withdrew")
	}
	out, err := m.Withdraw(ctx, g.ID, "owner(web)")
	if err != nil || out.State != sdk.GoalStateWithdrawn {
		t.Fatal(err)
	}
}

// 秘書傳來的話就跟打字給她一樣：照常做，只有「一律問你」的動作要問主人；傳話不掛 Goal（2026-11-01 作者）。
// Guards: REQ-153
func TestDecideTable(t *testing.T) {
	m := New(Deps{})
	classes := []sdk.ActionClass{"read", "write", "commit", "push", "delete", "send_external", "spend", "settings"}
	for _, origin := range []string{"owner", "agent", ""} {
		for _, class := range classes {
			t.Run(origin+"/"+string(class), func(t *testing.T) {
				alwaysAsk := class != "read" && class != "write" && class != "commit"
				want := sdk.ActionVerdictAllow
				if origin == "agent" && alwaysAsk {
					want = sdk.ActionVerdictAsk
				}
				if origin == "" && class != "read" {
					want = sdk.ActionVerdictDeny
				}
				got, reason := m.Decide(origin, "work", class)
				if got != want {
					t.Fatalf("%s != %s", got, want)
				}
				if got == sdk.ActionVerdictAllow && reason != "" {
					t.Fatal("allow includes reason")
				}
				if got == sdk.ActionVerdictDeny && !strings.Contains(reason, "這個限制只針對這一則") {
					t.Fatal("deny lacks scope")
				}
			})
		}
	}
}

// settings/goals.toml 的 always_ask 仍然決定哪些動作要問主人（授權範圍拿掉後只剩這一個來源，DEV-95）。
// Guards: REQ-153
func TestConfiguredAlwaysAskDrivesDecide(t *testing.T) {
	m, h, _, _ := fixture(t)
	if got, _ := m.Decide("agent", "work", sdk.ActionClassWrite); got != sdk.ActionVerdictAllow {
		t.Fatal("write should be allowed by default")
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.Dir, "settings"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := platform.AtomicWrite(filepath.Join(h.Dir, "settings/goals.toml"), []byte("always_ask=['write']\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := m.Decide("agent", "work", sdk.ActionClassWrite); got != sdk.ActionVerdictAsk {
		t.Fatal("configured always_ask ignored")
	}
}

// 舊的 Goal 檔帶 grants（DEV-95 之前存的）：讀得開、其他欄位照讀、原文不變；Planbook 照寫。
func TestLegacyGoalWithGrantsReads(t *testing.T) {
	m, h, _, _ := fixture(t)
	// 直接用 vault 寫一筆帶 grants 的舊紀錄（目前的 schema 已沒有這個欄位）。
	if _, err := m.d.Vault.Put(ctx, vault.Record{Kind: "goal", ID: "GOAL-9", Fields: map[string]any{
		"title": "舊目標", "text": "舊的原話", "grants": []string{"push", "delete"}, "agent": "code", "state": "active",
		"planbook_log": []any{}, "confirmed_by": "owner(web)", "confirmed_at": h.Clock.Now().Format(time.RFC3339Nano),
	}}, "owner(web)", ""); err != nil {
		t.Fatalf("legacy put: %v", err)
	}
	g, err := m.Get(ctx, "GOAL-9")
	if err != nil || g.Title != "舊目標" || g.Text != "舊的原話" || g.Agent != "code" {
		t.Fatalf("legacy goal: %+v %v", g, err)
	}
	if _, err := m.UpdatePlanbook(ctx, "GOAL-9", "第一步", "接手", "code"); err != nil {
		t.Fatalf("planbook on legacy goal: %v", err)
	}
}

// Guards: REQ-152
func TestDoneAndRestart(t *testing.T) {
	m, _, n, _ := fixture(t)
	g := active(t, m)
	if _, err := m.UpdatePlanbook(ctx, g.ID, "進度", "紀錄", "code"); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	g, err := m.Get(ctx, g.ID)
	if err != nil || g.Planbook != "進度" || len(g.PlanbookLog) != 1 {
		t.Fatal("restart lost goal")
	}
	g, err = m.Done(ctx, g.ID, "完成說明", "code")
	if err != nil || g.State != sdk.GoalStateDone || g.Summary != "完成說明" {
		t.Fatal(err)
	}
	last := n.sent[len(n.sent)-1]
	if last.Category != sdk.CategoryDone || last.Context != "完成說明" || last.Agent != "code" {
		t.Fatal("done notice missing")
	}
	if _, err := m.Done(ctx, g.ID, "重複", "code"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal("duplicate done accepted")
	}
}

// Guards: REQ-152
func TestAnswerSubscription(t *testing.T) {
	m, _, _, b := fixture(t)
	g := propose(t, m)
	<-b.published
	b.input <- &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": g.Notice, "text": "確認", "by": "owner(test)"}}
	select {
	case <-b.published:
	case <-time.After(5 * time.Second):
		t.Fatal("answer subscription timed out")
	}
	out, err := m.Get(ctx, g.ID)
	if err != nil || out.State != sdk.GoalStateActive {
		t.Fatal("subscription not applied")
	}
}

// Guards: REQ-37
func TestGoalArchivedAfter30DaysEnded(t *testing.T) {
	m, h, _, _ := fixture(t)
	archived := func() []vault.SweepItem {
		rep, err := h.Vault.Sweep(ctx, h.Clock.Now(), true)
		if err != nil {
			t.Fatal(err)
		}
		var out []vault.SweepItem
		for _, it := range rep.Items {
			if it.Action == "archive" {
				out = append(out, it)
			}
		}
		return out
	}
	g := active(t, m)
	// active 的 Goal 再久都不會被封存。
	h.Clock.Advance(90 * 24 * time.Hour)
	if got := archived(); len(got) != 0 {
		t.Fatalf("active Goal 不該被封存：%+v", got)
	}
	if _, err := m.Done(ctx, g.ID, "完成說明", "code"); err != nil {
		t.Fatal(err)
	}
	h.Clock.Advance(29 * 24 * time.Hour)
	if got := archived(); len(got) != 0 {
		t.Fatalf("第 29 天還不該封存：%+v", got)
	}
	h.Clock.Advance(2 * 24 * time.Hour)
	if got := archived(); len(got) != 1 || got[0].ID != g.ID {
		t.Fatalf("結束滿 30 天應封存：%+v", got)
	}
	// 封存後，任務與待辦的 goal 連結仍可解析（Get 找得到封存的 Goal）。
	if got, err := m.Get(ctx, g.ID); err != nil || got.State != sdk.GoalStateDone {
		t.Fatalf("封存後 Goal 仍要讀得到：%+v %v", got, err)
	}
}

type fakeRoster map[string]string

func (r fakeRoster) Resolve(_ context.Context, name string) (string, bool) {
	id, ok := r[name]
	return id, ok
}

type fakeMail struct {
	sent []*sdk.Envelope
	err  error
}

func (f *fakeMail) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.sent = append(f.sent, e)
	return e, nil
}

// 主人開 Goal 時可以指定負責的秘書（表單四欄之一，2026-11-01）：名字對到名冊 id；名冊上沒有回 invalid；沒指定記成主人。
// 指定了就傳一則 routine 給她（GOL-12）：內文帶 GOAL-n、標題、要做到什麼；沒指定不傳；傳不出去 Goal 照樣開好。
func TestOwnerCreateGoalWithAgent(t *testing.T) {
	m, _, _, _ := fixture(t)
	mail := &fakeMail{}
	m.d.Agents = fakeRoster{"code": "code", "Athena": "code"}
	m.d.Mail = mail
	g, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "合併", Text: "把測試家併進正式家", Agent: "Athena", By: "owner(web)"})
	if err != nil || g.Agent != "code" {
		t.Fatalf("agent: %+v %v", g, err)
	}
	if len(mail.sent) != 1 {
		t.Fatalf("sent: %d", len(mail.sent))
	}
	e := mail.sent[0]
	if e.To != "code" || e.Kind != sdk.EnvelopeKindRoutine || e.Route != sdk.EnvelopeRouteCore || e.IdempotencyKey != g.ID+"#assigned" ||
		!strings.Contains(e.Body, g.ID) || !strings.Contains(e.Body, "合併") || !strings.Contains(e.Body, "把測試家併進正式家") || strings.Contains(e.Body, "授權") {
		t.Fatalf("envelope: %+v", e)
	}
	if _, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "x", Text: "y", Agent: "nobody", By: "owner(web)"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("unknown agent: %v", err)
	}
	g, err = m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "x", Text: "y", Agent: " ", By: "owner(web)"})
	if err != nil || g.Agent != "owner(web)" || len(mail.sent) != 1 {
		t.Fatalf("no agent: %+v %v sent=%d", g, err, len(mail.sent))
	}
	mail.err = errors.New("bus down")
	if g, err := m.Create(ctx, CreateSpec{Criteria: "測試驗收條件", Title: "y", Text: "z", Agent: "code", By: "owner(web)"}); err != nil || g.Agent != "code" {
		t.Fatalf("send failure must not fail create: %+v %v", g, err)
	}
}

// 法律第 4 條（2026-11-01）：Goal 一定要有驗收條件，開了之後跟原文一樣不能改。
func TestGoalNeedsCriteriaAndKeepsIt(t *testing.T) {
	m, _, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, CreateSpec{Title: "x", Text: "y", Criteria: " ", By: "owner(web)"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("主人開 Goal 沒寫驗收條件應該擋：%v", err)
	}
	if _, err := m.Propose(ctx, ProposeSpec{Title: "x", Text: "y", Agent: "code", By: "code"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("代寫 Goal 沒寫驗收條件應該擋：%v", err)
	}
	g, err := m.Create(ctx, CreateSpec{Title: "整理報表", Text: "把三月的報表整理好", Criteria: "三月每一天都有一列", By: "owner(web)"})
	if err != nil || g.Criteria != "三月每一天都有一列" {
		t.Fatalf("驗收條件要存起來：%+v %v", g, err)
	}
	_, r, err := m.load(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	g.Criteria = "改寬"
	if _, err := m.save(ctx, g, r, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("驗收條件開了之後不能改：%v", err)
	}
}
