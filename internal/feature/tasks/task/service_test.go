package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

type fakeDeps struct {
	subscribed bool
	events     []string
	notices    []*sdk.Notification
	crons      []string
	open       []sdk.Notification // Notify.List 回的「還沒回的通知」
	retired    []string           // Notify.Retire 收掉的「編號：理由」
}

// List 照要的狀態篩（沒標狀態的當成符合，舊測試不用每則都標）。
func (f *fakeDeps) List(_ context.Context, states []sdk.NotificationState, _ string, _ bool) ([]sdk.Notification, error) {
	var out []sdk.Notification
	for _, n := range f.open {
		if n.State == "" || len(states) == 0 || slices.Contains(states, n.State) {
			out = append(out, n)
		}
	}
	return out, nil
}

// Retire 收掉的從 open 拿掉、記在 retired（「編號：理由」）。
func (f *fakeDeps) Retire(_ context.Context, ids []string, reason string) error {
	gone := map[string]bool{}
	for _, id := range ids {
		gone[id] = true
		f.retired = append(f.retired, id+"："+reason)
	}
	var keep []sdk.Notification
	for _, n := range f.open {
		if !gone[n.ID] {
			keep = append(keep, n)
		}
	}
	f.open = keep
	return nil
}

func (f *fakeDeps) Resolve(ctx context.Context, s string) (string, bool) {
	return s, s == "code" || s == "life"
}
func (f *fakeDeps) Publish(ctx context.Context, s string, p map[string]any) error {
	f.events = append(f.events, s)
	return nil
}
func fixture(t *testing.T) (*Module, *homefixture.Home, *fakeDeps) {
	t.Helper()
	h := homefixture.New(t)
	f := &fakeDeps{}
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Agents: f, Bus: f, Notify: f, Scheduler: f})
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	h.Vault.RegisterSchema(vault.Spec{Kind: "goal", Path: func(id string) string { return "vault/goals/" + id + ".md" }})
	// GOAL-1 故意只有 draft 狀態：掛 Goal 只要求存在，不要求 active、不需要主人確認（REQ-35、REQ-36）。
	h.WithRecords(vault.Record{Kind: "goal", ID: "GOAL-1", Fields: map[string]any{"state": "draft"}})
	// GOAL-2 是主人確認過的（進行中）：掛在它底下的單算有主人的驗收條件（法律第 1.4 條）。
	h.WithRecords(vault.Record{Kind: "goal", ID: "GOAL-2", Fields: map[string]any{"state": "active", "confirmed_by": "owner(web)"}})
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m, h, f
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func makeTask(t *testing.T, m *Module, kind sdk.TaskType) Task {
	t.Helper()
	r, err := m.Create(context.Background(), &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "需求", Type: kind, By: &sdk.Actor{Name: "code"}})
	must(t, err)
	return r
}
func submission() *sdk.TasksSubmitRequest {
	return &sdk.TasksSubmitRequest{Result: "成果", Verify: sdk.VerifyVerified, VerifyHow: "測試", Proposed: sdk.TaskStatusDone}
}
func ptr[T any](v T) *T { return &v }
func count(xs []string, s string) int {
	n := 0
	for _, x := range xs {
		if x == s {
			n++
		}
	}
	return n
}

// Guards: REQ-49
func TestCreateGetListUpdate(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	a := makeTask(t, m, sdk.TaskTypeTask)
	b := makeTask(t, m, sdk.TaskTypeTask)
	if a.ID == b.ID {
		t.Fatal("duplicate id")
	}
	_, err := m.Update(ctx, a.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingExternal), Result: ptr("處理中")}, "code")
	must(t, err)
	got, err := m.Get(ctx, a.ID)
	must(t, err)
	if got.Status != sdk.TaskStatusWaitingExternal {
		t.Fatal(got)
	}
	rows, err := m.List(ctx, nil)
	must(t, err)
	if len(rows) != 2 {
		t.Fatal(rows)
	}
}

// Guards: REQ-50
func TestStatusAndTypeEnums(t *testing.T) {
	m, _, _ := fixture(t)
	for _, in := range []*sdk.TasksCreateRequest{{Title: "x", Type: "unknown", By: &sdk.Actor{Name: "code"}}, {Title: "x", Status: "unknown", By: &sdk.Actor{Name: "code"}}} {
		if _, err := m.Create(context.Background(), in); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatal(err)
		}
	}
}

// Guards: REQ-50
func TestTransitionTable(t *testing.T) {
	m, _, _ := fixture(t)
	for _, role := range []string{"user", "agent"} {
		for _, kind := range []string{"task", "ask"} {
			for _, from := range statuses {
				for _, to := range statuses {
					allowed := false
					if !ended(from) {
						if role == "user" {
							allowed = true
						} else {
							allowed = from == to || from == "idea" && to == "doing" || from == "doing" && oneOf(to, "waiting_user", "waiting_external") || oneOf(from, "waiting_user", "waiting_external") && to == "doing" || from == "doing" && kind == "ask" && oneOf(to, "done", "cancelled")
						}
					}
					if got := m.transitions[transition{role, kind, from, to}]; got != allowed {
						t.Fatalf("%s %s %s -> %s: %v", role, kind, from, to, got)
					}
				}
			}
		}
	}
}

// Guards: REQ-51
func TestSubmitRequiresResultVerifyHowProposed(t *testing.T) {
	for _, field := range []string{"result", "verify", "how", "proposed"} {
		t.Run(field, func(t *testing.T) {
			m, _, _ := fixture(t)
			r := makeTask(t, m, sdk.TaskTypeTask)
			s := submission()
			switch field {
			case "result":
				s.Result = ""
			case "verify":
				s.Verify = ""
			case "how":
				s.VerifyHow = ""
			case "proposed":
				s.Proposed = ""
			}
			if _, err := m.Submit(context.Background(), r.ID, s, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
				t.Fatal(err)
			}
		})
	}
}

// Guards: REQ-51
func TestAskClosesWithoutReview(t *testing.T) {
	m, _, _ := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeAsk)
	got, err := m.Close(context.Background(), r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusDone, Result: "回答"}, "code")
	must(t, err)
	if got.Status != sdk.TaskStatusDone || !got.AcceptedAt.IsZero() {
		t.Fatal(got)
	}
}

// Guards: REQ-51
func TestAcceptFillsAcceptedFields(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	r, err = m.Accept(ctx, r.ID, "owner(web)")
	must(t, err)
	if r.AcceptedBy != "owner(web)" || !r.AcceptedAt.Equal(h.Clock.Now()) || !r.ClosedAt.Equal(h.Clock.Now()) {
		t.Fatal(r)
	}
	if _, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusDoing)}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
}

// Guards: REQ-52
func TestIdeasInboxOldestFirst(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a, err := m.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "first", Status: sdk.TaskStatusIdea, By: &sdk.Actor{Name: "owner(web)"}})
	must(t, err)
	h.Clock.Advance(time.Hour)
	_, err = m.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "second", Status: sdk.TaskStatusIdea, By: &sdk.Actor{Name: "code"}})
	must(t, err)
	makeTask(t, m, sdk.TaskTypeTask)
	rows, err := m.List(ctx, &sdk.TasksListRequest{IdeasOnly: true})
	must(t, err)
	if len(rows) != 2 || rows[0].ID != a.ID {
		t.Fatal(rows)
	}
}

// Guards: REQ-53
func TestStaleAfterThreeDays(t *testing.T) {
	m, h, _ := fixture(t)
	makeTask(t, m, sdk.TaskTypeTask)
	h.Clock.Advance(72*time.Hour - time.Minute)
	rows, err := m.Stale(context.Background(), h.Clock.Now())
	must(t, err)
	if len(rows) != 0 {
		t.Fatal(rows)
	}
	h.Clock.Advance(2 * time.Minute)
	rows, err = m.Stale(context.Background(), h.Clock.Now())
	must(t, err)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
}

// Guards: REQ-53
func TestStaleAndReconcileDoNotWrite(t *testing.T) {
	m, h, _ := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	ctx := context.Background()
	before, err := h.Vault.Get(ctx, "task", r.ID)
	must(t, err)
	h.Clock.Advance(4 * 24 * time.Hour)
	_, err = m.Stale(ctx, h.Clock.Now())
	must(t, err)
	rows, err := m.Reconcile(ctx, "code", h.Clock.Now())
	must(t, err)
	after, err := h.Vault.Get(ctx, "task", r.ID)
	must(t, err)
	if len(rows) != 1 || before.Revision != after.Revision {
		t.Fatal("query wrote record")
	}
}

// Guards: REQ-54
func TestWaitingUserRequiresAskAndPublishes(t *testing.T) {
	m, _, f := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	ctx := context.Background()
	p := &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser)}
	if _, err := m.Update(ctx, r.ID, p, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
	p.Ask = ptr("請決定")
	got, err := m.Update(ctx, r.ID, p, "code")
	must(t, err)
	if got.WaitingSince.IsZero() || got.Nudges != 0 || count(f.events, "task.waiting_user") != 1 {
		t.Fatal(got, f.events)
	}
}

// Guards: REQ-54
func TestReviewPublishesAndSetsWaitingSince(t *testing.T) {
	m, h, f := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	got, err := m.Submit(context.Background(), r.ID, submission(), "code")
	must(t, err)
	if !got.WaitingSince.Equal(h.Clock.Now()) || got.Nudges != 0 || count(f.events, "task.review") != 1 {
		t.Fatal(got, f.events)
	}
	// 交付等驗收併進「等你」（2026-11-01）：狀態是 waiting_user、proposed 有值、ask 補「請驗收」。
	if got.Status != sdk.TaskStatusWaitingUser || got.Proposed != sdk.TaskStatusDone || got.Ask != reviewAsk {
		t.Fatal(got)
	}
}

// Guards: REQ-148
func TestDueNudgesEveryThreeDaysMaxThree(t *testing.T) {
	m, h, f := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	n := len(f.events)
	for i := 0; i < 3; i++ {
		h.Clock.Advance(71 * time.Hour)
		rows, err := m.DueNudges(ctx, h.Clock.Now())
		must(t, err)
		if len(rows) != 0 {
			t.Fatal(rows)
		}
		h.Clock.Advance(time.Hour + time.Minute)
		rows, err = m.DueNudges(ctx, h.Clock.Now())
		must(t, err)
		if len(rows) != 1 {
			t.Fatal(rows)
		}
		must(t, m.MarkNudged(ctx, r.ID, h.Clock.Now()))
	}
	h.Clock.Advance(4 * 24 * time.Hour)
	rows, err := m.DueNudges(ctx, h.Clock.Now())
	must(t, err)
	wait, err := m.Waiting(ctx, h.Clock.Now())
	must(t, err)
	if len(rows) != 0 || len(wait) != 1 || len(f.events) != n {
		t.Fatal(rows, wait, f.events)
	}
}

// 工作 session 的「還在等你」提醒記進任務單（P7-6、D-34）：nudges 加 1，不看每日跟進的上限、不動 last_nudged_at。
// Guards: REQ-223, REQ-148
func TestAddNudgeCountsWithoutFollowCadence(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	for i := 1; i <= 5; i++ { // 超過每日跟進的上限 3 也照記
		must(t, m.AddNudge(ctx, r.ID))
		got, err := m.Get(ctx, r.ID)
		must(t, err)
		if int(got.Nudges) != i || !got.LastNudgedAt.IsZero() {
			t.Fatalf("第 %d 次：nudges=%d last_nudged_at=%v", i, got.Nudges, got.LastNudgedAt)
		}
	}
	if err := m.AddNudge(ctx, "SEC-99999"); err == nil {
		t.Fatal("沒有這張單要回錯誤")
	}
	_ = h
}

// Guards: REQ-148
func TestWaitingOldestFirst(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	a := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Submit(ctx, a.ID, submission(), "code")
	must(t, err)
	h.Clock.Advance(48 * time.Hour)
	b := makeTask(t, m, sdk.TaskTypeTask)
	_, err = m.Submit(ctx, b.ID, submission(), "code")
	must(t, err)
	rows, err := m.Waiting(ctx, h.Clock.Now())
	must(t, err)
	if len(rows) != 2 || rows[0].Task.ID != a.ID || rows[0].WaitingDays != 2 || rows[1].WaitingDays != 0 {
		t.Fatal(rows)
	}
}

// Guards: REQ-149
func TestRejectReturnsToDoingWithNote(t *testing.T) {
	m, _, f := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	got, err := m.Reject(ctx, r.ID, "再驗一次", "owner(web)")
	must(t, err)
	if got.Status != sdk.TaskStatusDoing || got.ReviewNote != "再驗一次" || !got.WaitingSince.IsZero() || count(f.events, "task.rejected") != 1 {
		t.Fatal(got)
	}
	got, err = m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	if got.ReviewNote != "" {
		t.Fatal(got)
	}
}

// Guards: REQ-150
func TestAgentCannotCloseTask(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	for _, s := range []sdk.TaskStatus{sdk.TaskStatusDone, sdk.TaskStatusCancelled, "failed"} {
		if _, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: &s}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatal(err)
		}
		if _, err := m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: s, Result: "x"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatal(err)
		}
	}
	if _, err := m.Accept(ctx, r.ID, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
}

// Guards: REQ-55
func TestTaskPageQueries(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	a := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(ctx, a.ID, &sdk.TasksUpdateRequest{Goal: ptr("GOAL-1"), Agent: ptr("life")}, "code")
	must(t, err)
	makeTask(t, m, sdk.TaskTypeAsk)
	for _, q := range []*sdk.TasksListRequest{{Agent: "life"}, {Goal: "GOAL-1"}, {Statuses: []sdk.TaskStatus{sdk.TaskStatusDoing}, Agent: "life"}} {
		rows, err := m.List(ctx, q)
		must(t, err)
		if len(rows) != 1 || rows[0].ID != a.ID {
			t.Fatal(rows)
		}
	}
}

// Guards: REQ-56
func TestBriefMentionsIDAndReviewNote(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	_, err = m.Reject(ctx, r.ID, "重查", "owner(web)")
	must(t, err)
	s, err := m.Brief(ctx, r.ID)
	must(t, err)
	if !strings.Contains(s, r.ID) || !strings.Contains(s, "重查") || !strings.Contains(s, "需求") {
		t.Fatal(s)
	}
}

// Guards: REQ-57
func TestByRecorded(t *testing.T) {
	m, _, _ := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	if r.By != "code" {
		t.Fatal(r)
	}
	if _, err := m.Create(context.Background(), &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "x", By: &sdk.Actor{Name: "unknown"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
}

// Guards: REQ-6
func TestTaskArchiveTimingRealSpec(t *testing.T) {
	m, h, _ := fixture(t)
	h.Clock.Set(time.Date(2027, 1, 31, 23, 59, 0, 0, h.Clock.Now().Location()))
	r := makeTask(t, m, sdk.TaskTypeAsk)
	ctx := context.Background()
	_, err := m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusDone, Result: "x"}, "code")
	must(t, err)
	report, err := h.Vault.Sweep(ctx, h.Clock.Now(), false)
	must(t, err)
	for _, item := range report.Items {
		if item.Action == "archive" {
			t.Fatal(report)
		}
	}
	h.Clock.Advance(time.Minute)
	_, err = h.Vault.Sweep(ctx, h.Clock.Now(), true)
	must(t, err)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if !got.Archived {
		t.Fatal(got)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/tasks/archive/2027-02", r.ID+".md")); err != nil {
		t.Fatal(err)
	}
	sc := taskSpec()
	now := h.Clock.Now()
	at, ok := sc.ArchiveAt(vault.Record{Fields: map[string]any{"status": "shelved"}, UpdatedAt: now}, now.Location())
	if !ok || !at.Equal(now.AddDate(0, 0, 30)) {
		t.Fatal(at, ok)
	}
}

// Guards: REQ-35, REQ-36
func TestTaskGoalLink(t *testing.T) {
	m, h, _ := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	ctx := context.Background()
	if _, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Goal: ptr("GOAL-999")}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal("不存在的 Goal 應被擋下：", err)
	}
	// 秘書（code）直接掛在尚未 active 的 GOAL-1 上：不需要主人確認。
	got, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Goal: ptr("GOAL-1")}, "code")
	must(t, err)
	raw, err := h.Vault.Get(ctx, "task", r.ID)
	must(t, err)
	if got.Goal != "GOAL-1" || raw.Fields["goal"] != "[[GOAL-1]]" {
		t.Fatal(got, raw)
	}
	if _, has := raw.Fields["group"]; has {
		t.Fatal("不該再寫 group 欄位", raw.Fields)
	}
	// 開單時直接帶 Goal 也行。
	created, err := m.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "掛 Goal 開單", Type: sdk.TaskTypeTask, Goal: "GOAL-1", By: &sdk.Actor{Name: "code"}})
	must(t, err)
	if created.Goal != "GOAL-1" {
		t.Fatal(created)
	}
	if _, err := m.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "壞 Goal", Type: sdk.TaskTypeTask, Goal: "GOAL-404", By: &sdk.Actor{Name: "code"}}); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	// 空字串拿掉 Goal。
	got, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Goal: ptr("")}, "code")
	must(t, err)
	if got.Goal != "" {
		t.Fatal(got)
	}
}

// 簡單模式只影響看板顯示：家是 simple 的時候，掛 Goal、List／Get 回 goal、依 Goal 查、vault 檔案都照常（TSK-11）。
// Guards: REQ-58
func TestSimpleModeKeepsGoalData(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	home, err := platform.LoadHome(h.Dir)
	must(t, err)
	if home.Mode != "simple" {
		t.Fatalf("這個測試要在 simple 的家裡跑：%q", home.Mode)
	}
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Goal: ptr("GOAL-1")}, "code")
	must(t, err)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.Goal != "GOAL-1" {
		t.Fatalf("Get 要照常回 goal：%+v", got)
	}
	list, err := m.List(ctx, &sdk.TasksListRequest{})
	must(t, err)
	found := false
	for _, x := range list {
		if x.ID == r.ID {
			found = x.Goal == "GOAL-1"
		}
	}
	if !found {
		t.Fatalf("List 要照常回 goal：%+v", list)
	}
	byGoal, err := m.List(ctx, &sdk.TasksListRequest{Goal: "GOAL-1"})
	must(t, err)
	if len(byGoal) != 1 || byGoal[0].ID != r.ID {
		t.Fatalf("依 Goal 查照常：%+v", byGoal)
	}
	raw, err := h.Vault.Get(ctx, "task", r.ID)
	must(t, err)
	if raw.Fields["goal"] != "[[GOAL-1]]" {
		t.Fatalf("vault 檔案裡的 goal 原封不動：%+v", raw.Fields)
	}
	// 之後改別的欄位（備註），goal 不會掉。
	_, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Result: ptr("改個備註")}, "code")
	must(t, err)
	raw, err = h.Vault.Get(ctx, "task", r.ID)
	must(t, err)
	if raw.Fields["goal"] != "[[GOAL-1]]" {
		t.Fatalf("改別的欄位 goal 不能掉：%+v", raw.Fields)
	}
}

// Guards: REQ-35
func TestLegacyGroupFieldReadsAsGoal(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	h.WithRecords(
		vault.Record{Kind: "task", ID: "SEC-901", Fields: map[string]any{"title": "舊欄位", "type": "task", "status": "doing", "agent": "code", "group": "[[GOAL-1]]"}},
		vault.Record{Kind: "task", ID: "SEC-902", Fields: map[string]any{"title": "舊設計的 G-1", "type": "task", "status": "doing", "agent": "code", "group": "[[G-1]]"}},
	)
	got, err := m.Get(ctx, "SEC-901")
	must(t, err)
	if got.Goal != "GOAL-1" {
		t.Fatalf("舊的 group 欄位應當成 goal：%+v", got)
	}
	rows, err := m.List(ctx, &sdk.TasksListRequest{Goal: "GOAL-1"})
	must(t, err)
	if len(rows) != 1 || rows[0].ID != "SEC-901" {
		t.Fatal(rows)
	}
	// 不是 GOAL-<n> 形狀的舊值（舊設計的 G-<n>）不會變成 Goal。
	if other, err := m.Get(ctx, "SEC-902"); err != nil || other.Goal != "" {
		t.Fatal(other, err)
	}
	// 下一次寫回就清掉 group。
	_, err = m.Update(ctx, "SEC-901", &sdk.TasksUpdateRequest{Agent: ptr("life")}, "code")
	must(t, err)
	raw, err := h.Vault.Get(ctx, "task", "SEC-901")
	must(t, err)
	if _, has := raw.Fields["group"]; has || raw.Fields["goal"] != "[[GOAL-1]]" {
		t.Fatal(raw.Fields)
	}
}

// Guards: REQ-150
func TestAgentCannotChangeType(t *testing.T) {
	m, _, _ := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	ctx := context.Background()
	p := &sdk.TasksUpdateRequest{Type: ptr(sdk.TaskTypeAsk)}
	if _, err := m.Update(ctx, r.ID, p, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.Type != sdk.TaskTypeTask {
		t.Fatal(got)
	}
	got, err = m.Update(ctx, r.ID, p, "owner(web)")
	must(t, err)
	if got.Type != sdk.TaskTypeAsk {
		t.Fatal(got)
	}
}

// Guards: REQ-50, REQ-150
func TestCreateInitialStatus(t *testing.T) {
	for _, by := range []string{"code", "owner(web)"} {
		for _, kind := range []sdk.TaskType{sdk.TaskTypeTask, sdk.TaskTypeAsk} {
			for _, status := range statuses {
				t.Run(fmt.Sprintf("%s/%s/%s", by, kind, status), func(t *testing.T) {
					m, _, _ := fixture(t)
					_, err := m.Create(context.Background(), &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "x", Type: kind, Status: sdk.TaskStatus(status), Ask: "選擇", Result: "結果", Verify: sdk.VerifyVerified, VerifyHow: "測試", By: &sdk.Actor{Name: by}})
					allowed := by == "owner(web)" || by == "code" && (oneOf(status, "idea", "doing", "waiting_user", "waiting_external") || kind == sdk.TaskTypeAsk && oneOf(status, "done", "cancelled"))
					if (err == nil) != allowed {
						t.Fatalf("allowed %v err %v", allowed, err)
					}
				})
			}
		}
	}
}

func (b *fakeDeps) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	b.subscribed = true
	return nil, nil
}

func (f *fakeDeps) Send(ctx context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	f.notices = append(f.notices, n)
	out := *n // 回的那份帶編號（像 notify 一樣）；記下來的照原樣，比對內容的測試不用管編號
	out.ID = fmt.Sprintf("N-%d", len(f.notices))
	return &out, nil
}
func (f *fakeDeps) Cron(ctx context.Context, id string, specs []string, opt timer.CronOpt) error {
	if !f.subscribed {
		return fmt.Errorf("scheduled before subscription")
	}
	f.crons = append(f.crons, specs...)
	return nil
}

// Guards: REQ-148
func TestFollowConfigReloads(t *testing.T) {
	m, h, f := fixture(t)
	ctx := context.Background()
	makeTask(t, m, sdk.TaskTypeTask)
	h.Clock.Advance(2*24*time.Hour + time.Hour)
	rows, err := m.Stale(ctx, h.Clock.Now())
	must(t, err)
	if len(rows) != 0 {
		t.Fatal("default stale_days is 3", rows)
	}
	// 看板改了 [tasks]：下一次檢查就用新值，不用重開。
	path := filepath.Join(h.Dir, "settings/notify.toml")
	must(t, os.MkdirAll(filepath.Dir(path), 0o700))
	must(t, os.WriteFile(path, []byte("[tasks]\nstale_days = 2\nnudge_at = \"08:30\"\n"), 0o600))
	rows, err = m.Stale(ctx, h.Clock.Now())
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("new stale_days not used", rows)
	}
	if len(f.crons) == 0 || f.crons[len(f.crons)-1] != "30 8 * * *" {
		t.Fatal("nudge_at change did not re-register the schedule", f.crons)
	}
	// 壞檔：沿用上一次的值，不回錯。
	must(t, os.WriteFile(path, []byte("[tasks\nstale_days = "), 0o600))
	rows, err = m.Stale(ctx, h.Clock.Now())
	must(t, err)
	if len(rows) != 1 {
		t.Fatal("bad file should keep the last values", rows)
	}
	// Start 遇到壞檔照舊回錯。
	m2 := New(Deps{Vault: h.Vault, Clock: h.Clock, Agents: f, Bus: f, Notify: f, Scheduler: f})
	m2.SetHome(h.Dir)
	if err := m2.Start(ctx); err == nil {
		t.Fatal("Start should fail on a bad file")
	}
}

// 任務單記著工作 session 的引擎 session id（P7-5、D-33）：秘書可以寫、空字串拿掉、不影響狀態與跟進。
// Guards: REQ-217
func TestTaskWorkSessionFields(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	created, err := m.Create(ctx, &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "開工作對話", Type: sdk.TaskTypeTask, By: &sdk.Actor{Name: "code"}})
	must(t, err)
	if created.EngineSession != "" {
		t.Fatal(created)
	}
	got, err := m.Update(ctx, created.ID, &sdk.TasksUpdateRequest{EngineSession: ptr("11111111-2222-4333-8444-555555555555")}, "code")
	must(t, err)
	raw, err := h.Vault.Get(ctx, "task", created.ID)
	must(t, err)
	if got.EngineSession != "11111111-2222-4333-8444-555555555555" || got.Status != sdk.TaskStatusDoing || raw.Fields["engine_session"] != got.EngineSession {
		t.Fatal(got, raw.Fields)
	}
	// 重新讀回來還在。
	again, err := m.Get(ctx, created.ID)
	must(t, err)
	if again.EngineSession != got.EngineSession {
		t.Fatal(again)
	}
	// 空字串拿掉。
	got, err = m.Update(ctx, created.ID, &sdk.TasksUpdateRequest{EngineSession: ptr("")}, "code")
	must(t, err)
	raw, _ = h.Vault.Get(ctx, "task", created.ID)
	if got.EngineSession != "" || raw.Fields["engine_session"] != nil {
		t.Fatal(got, raw.Fields)
	}
}

// Guards: REQ-262
func TestArchiveIsOwnerOnlyAndCancelsOpenTasks(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	open := makeTask(t, m, sdk.TaskTypeTask)
	other := makeTask(t, m, sdk.TaskTypeTask)

	if _, err := m.Archive(ctx, []string{open.ID}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書刪任務應該被擋，得到 %v", err)
	}
	got, err := m.Archive(ctx, []string{open.ID, "SEC-999"}, "owner(web)")
	must(t, err)
	if len(got) != 1 || got[0] != open.ID {
		t.Fatalf("archived = %v（找不到的略過）", got)
	}
	list, err := m.List(ctx, &sdk.TasksListRequest{})
	must(t, err)
	if len(list) != 1 || list[0].ID != other.ID {
		t.Fatalf("刪掉的單不該還在清單裡：%v", list)
	}
	// 再刪一次：略過，不算錯。
	if got, err = m.Archive(ctx, []string{open.ID}, "owner(web)"); err != nil || len(got) != 0 {
		t.Fatalf("重刪：%v %v", got, err)
	}
}

// Guards: REQ-33
func TestAnsweredLastMessageMovesTaskBackToDoing(t *testing.T) {
	m, _, f := fixture(t)
	tk := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(context.Background(), tk.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("要不要？")}, "code")
	must(t, err)
	f.open = []sdk.Notification{{ID: "M-2", Task: tk.ID, State: sdk.NotificationStateUnread}, {ID: "M-9", Task: "SEC-999", State: sdk.NotificationStateUnread}}
	status := func() sdk.TaskStatus {
		got, err := m.Get(context.Background(), tk.ID)
		must(t, err)
		return got.Status
	}
	// 還有一則在等：不動。
	must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "task": tk.ID, "via": "button"}}))
	if status() != sdk.TaskStatusWaitingUser {
		t.Fatal("task moved while another message is still open")
	}
	// 最後一則答完：回到進行中。
	f.open = f.open[1:] // M-2 也答了；只剩別張單的
	must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-2", "task": tk.ID, "via": "chat"}}))
	if status() != sdk.TaskStatusDoing {
		t.Fatalf("task not back to doing: %s", status())
	}
}

// 「請驗收」那則通知（TSK-25）：按鈕是驗收／退回；按「驗收」（舊通知的「好」）就通過，按「退回」或打字就退回並留意見，
// 不會像一般回覆那樣把單送回進行中、清掉 proposed。
func TestAnsweringReviewNoticeAcceptsOrRejects(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		answer, status, note string
	}{
		{"驗收", "done", ""},
		{"好", "done", ""},
		{"退回", "doing", "主人在通知按了「退回」"},
		{"不要", "doing", "主人在通知按了「退回」"},
		{"按鈕顏色不對", "doing", "按鈕顏色不對"},
	} {
		m, _, f := fixture(t)
		tk := makeTask(t, m, sdk.TaskTypeTask)
		_, err := m.Submit(ctx, tk.ID, submission(), "code")
		must(t, err)
		last := f.notices[len(f.notices)-1]
		if last.Todo != reviewAsk || strings.Join(last.Options, "/") != "驗收/退回" {
			t.Fatalf("請驗收的通知：todo=%q options=%v", last.Todo, last.Options)
		}
		must(t, m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "task": tk.ID, "via": "button", "text": c.answer, "by": "owner(web)", "todo": reviewAsk}}))
		got, err := m.Get(ctx, tk.ID)
		must(t, err)
		if string(got.Status) != c.status || !strings.Contains(got.ReviewNote, c.note) {
			t.Fatalf("回答 %q：status=%s note=%q", c.answer, got.Status, got.ReviewNote)
		}
		if c.status == "done" && got.AcceptedBy != "owner(web)" {
			t.Fatalf("回答 %q：accepted_by=%q", c.answer, got.AcceptedBy)
		}
	}
}

// 送驗收前就開著的舊問句（等你→等你送驗收不會收掉它）回「好」：不是驗收，單照樣等驗收、proposed 還在（TSK-25）。
func TestAnsweringOldQuestionDoesNotAcceptReview(t *testing.T) {
	ctx := context.Background()
	m, _, _ := fixture(t)
	tk := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(ctx, tk.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("要日式還是義式？")}, "code")
	must(t, err)
	_, err = m.Submit(ctx, tk.ID, submission(), "code")
	must(t, err)
	must(t, m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "task": tk.ID, "via": "button", "text": "好", "by": "owner(web)", "todo": "要日式還是義式？"}}))
	got, err := m.Get(ctx, tk.ID)
	must(t, err)
	if got.Status != sdk.TaskStatusWaitingUser || got.Proposed == "" {
		t.Fatalf("舊問句的回答不能結案或退回：status=%s proposed=%q", got.Status, got.Proposed)
	}
}

// 等你決定的問句（等你、沒有 proposed，TSK-27）：通知只收打字回答，不給預設的「好／不要」（問題可能是「要日式還是義式？」）；
// 請驗收那則照舊有「驗收／退回」按鈕（TSK-25）。打字回答了單照樣回進行中。
func TestWaitingDecisionNoticeIsTypedOnly(t *testing.T) {
	ctx := context.Background()
	m, _, f := fixture(t)
	tk := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(ctx, tk.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("要日式還是義式？")}, "code")
	must(t, err)
	ask := f.notices[len(f.notices)-1]
	if !ask.Typed || len(ask.Options) != 0 || ask.Todo != "要日式還是義式？" {
		t.Fatalf("等你決定的通知要只收打字：typed=%v options=%v todo=%q", ask.Typed, ask.Options, ask.Todo)
	}
	must(t, m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "task": tk.ID, "via": "chat", "text": "日式", "by": "owner(web)", "todo": "要日式還是義式？"}}))
	got, err := m.Get(ctx, tk.ID)
	must(t, err)
	if got.Status != sdk.TaskStatusDoing {
		t.Fatalf("打字回答後要回進行中：%s", got.Status)
	}
	_, err = m.Submit(ctx, tk.ID, submission(), "code")
	must(t, err)
	review := f.notices[len(f.notices)-1]
	if review.Typed || strings.Join(review.Options, "/") != "驗收/退回" {
		t.Fatalf("請驗收那則照舊有按鈕：typed=%v options=%v", review.Typed, review.Options)
	}
}

// Guards: REQ-33
func TestAutoAnsweredDoesNotTouchTask(t *testing.T) {
	m, _, _ := fixture(t)
	tk := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(context.Background(), tk.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("要不要？")}, "code")
	must(t, err)
	must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "task": tk.ID, "via": "auto"}}))
	got, _ := m.Get(context.Background(), tk.ID)
	if got.Status != sdk.TaskStatusWaitingUser {
		t.Fatal("auto answer must not move the task")
	}
}

// 家的 [ids] 設了 tasks = "DEV-"、goals = "DG-"（工坊）：開新單得到 DEV-1，掛 Goal 照 DG- 認，舊字首 GOAL- 擋掉。
func TestCreateUsesHomeTaskPrefix(t *testing.T) {
	h := homefixture.NewWith(t, "[ids]\ntasks=\"DEV-\"\ngoals=\"DG-\"\n")
	f := &fakeDeps{}
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Agents: f, Bus: f, Notify: f, Scheduler: f})
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	h.Vault.RegisterSchema(vault.Spec{Kind: "goal", Path: func(id string) string { return "vault/goals/" + id + ".md" }})
	h.WithRecords(vault.Record{Kind: "goal", ID: "DG-1", Fields: map[string]any{"state": "draft"}})
	r, err := m.Create(context.Background(), &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "工坊的單", Type: sdk.TaskTypeTask, Goal: "DG-1", By: &sdk.Actor{Name: "code"}})
	must(t, err)
	if r.ID != "DEV-1" || r.Goal != "DG-1" {
		t.Fatalf("編號 %q、Goal %q；要 DEV-1、DG-1", r.ID, r.Goal)
	}
	if _, err := m.Create(context.Background(), &sdk.TasksCreateRequest{Criteria: "測試驗收條件", Title: "掛錯字首", Type: sdk.TaskTypeTask, Goal: "GOAL-1", By: &sdk.Actor{Name: "code"}}); err == nil {
		t.Fatal("GOAL-1 不是這個家的 Goal 字首，應該擋")
	}
}

// 法律第 1.4 條（2026-11-01）：一般任務一定要有驗收條件。秘書開單要寫；主人開單可以不寫，但送驗收前一定要補上。
func TestTaskNeedsCriteriaBeforeSubmit(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	if _, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "沒寫條件", By: &sdk.Actor{Name: "code"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || fieldOf(err) != "criteria" {
		t.Fatalf("秘書開單沒寫驗收條件應該擋：%v", err)
	}
	if _, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "問答", Type: sdk.TaskTypeAsk, By: &sdk.Actor{Name: "code"}}); err != nil {
		t.Fatalf("問答類不用驗收條件：%v", err)
	}
	r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "主人一行開單", Agent: "code", By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil {
		t.Fatal(err)
	}
	sub := &sdk.TasksSubmitRequest{Result: "做好了", Verify: sdk.VerifyVerified, VerifyHow: "跑過", Proposed: sdk.TaskStatusDone}
	if _, err = m.Submit(ctx, r.ID, sub, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || fieldOf(err) != "criteria" {
		t.Fatalf("沒有驗收條件不能送驗收：%v", err)
	}
	got, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Criteria: ptr("畫面看得到新按鈕")}, "code")
	if err != nil || got.Criteria != "畫面看得到新按鈕" || got.CriteriaOwner {
		t.Fatalf("秘書補的條件算秘書的：%+v %v", got, err)
	}
	if _, err = m.Submit(ctx, r.ID, sub, "code"); err != nil {
		t.Fatal(err)
	}
}

// 法律第 1.4 條：有主人的驗收條件，負責的秘書照條件驗過就自己結；別的秘書、沒驗過、不做了都不行。
func TestAgentClosesTaskWithOwnerCriteria(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "主人給條件", Agent: "code", Criteria: "make ci 過", By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil || !r.CriteriaOwner {
		t.Fatalf("主人寫的條件算主人的：%+v %v", r, err)
	}
	ok := &sdk.TasksCloseRequest{Status: sdk.TaskStatusDone, Result: "改好了", Verify: sdk.VerifyVerified, VerifyHow: "跑了 make ci，全過"}
	if _, err = m.Close(ctx, r.ID, ok, "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不是負責的秘書不能結：%v", err)
	}
	if _, err = m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusDone, Result: "改好了", Verify: sdk.VerifyUnverifiable, VerifyHow: "沒辦法跑"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒驗過不能自己結：%v", err)
	}
	if _, err = m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusCancelled, Result: "不做"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("不做了只有主人決定：%v", err)
	}
	done, err := m.Close(ctx, r.ID, ok, "code")
	if err != nil || done.Status != sdk.TaskStatusDone || done.AcceptedBy != "code" || done.ClosedAt.IsZero() {
		t.Fatalf("負責的秘書照條件驗過應該能結：%+v %v", done, err)
	}
}

// 秘書自己寫的條件不算主人的；照抄主人的話（criteria_from_owner）或掛了 Goal 才算。
func TestAgentCriteriaCountsOnlyFromOwnerOrGoal(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	ok := &sdk.TasksCloseRequest{Status: sdk.TaskStatusDone, Result: "好了", Verify: sdk.VerifyVerified, VerifyHow: "照條件看過"}
	own, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "自己寫條件", Criteria: "我覺得好", By: &sdk.Actor{Name: "code"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Close(ctx, own.ID, ok, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書自己寫的條件不能自己結：%v", err)
	}
	// 草稿 Goal（主人還沒確認）不算：不然秘書自己提一個 Goal 掛上去就能自己結。
	if _, err = m.Update(ctx, own.ID, &sdk.TasksUpdateRequest{Goal: ptr("GOAL-1")}, "code"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Close(ctx, own.ID, ok, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("掛草稿 Goal 不能自己結：%v", err)
	}
	if _, err = m.Update(ctx, own.ID, &sdk.TasksUpdateRequest{Goal: ptr("GOAL-2")}, "code"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Close(ctx, own.ID, ok, "code"); err != nil {
		t.Fatalf("掛了主人確認過的 Goal，照 Goal 的驗收條件可以自己結：%v", err)
	}
	copied, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "照抄主人", Criteria: "主人說：看板有新欄位", CriteriaFromOwner: true, By: &sdk.Actor{Name: "code"}})
	if err != nil || !copied.CriteriaOwner {
		t.Fatalf("照抄主人的條件算主人的：%+v %v", copied, err)
	}
	if _, err = m.Update(ctx, copied.ID, &sdk.TasksUpdateRequest{Criteria: ptr("我改寬一點"), CriteriaFromOwner: true}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("主人的條件秘書不能改（帶 criteria_from_owner 也一樣）：%v", err)
	}
	if _, err = m.Update(ctx, copied.ID, &sdk.TasksUpdateRequest{Criteria: ptr("主人說：看板有新欄位")}, "code"); err != nil {
		t.Fatalf("照原樣再送一次不算改：%v", err)
	}
	// 秘書換負責人：主人的條件降成秘書的，新負責人做完要送驗收。
	moved, err := m.Update(ctx, copied.ID, &sdk.TasksUpdateRequest{Agent: ptr("life")}, "code")
	if err != nil || moved.CriteriaOwner {
		t.Fatalf("秘書換負責人後條件應該降成秘書的：%+v %v", moved, err)
	}
	if _, err = m.Close(ctx, copied.ID, ok, "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("換過負責人的單不能自己結：%v", err)
	}
	if _, err = m.Update(ctx, copied.ID, &sdk.TasksUpdateRequest{CriteriaFromOwner: true}, "code"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("只標主人的、不給條件內容應該擋：%v", err)
	}
}

// 秘書自己結單只能從進行中或等外部：不跳過主人的「等你」，也不把還沒開工的待做直接結掉。
func TestAgentSelfCloseOnlyFromDoingOrWaitingExternal(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	ok := &sdk.TasksCloseRequest{Status: sdk.TaskStatusDone, Result: "好了", Verify: sdk.VerifyVerified, VerifyHow: "照條件看過"}
	for _, st := range []sdk.TaskStatus{sdk.TaskStatusIdea, sdk.TaskStatusWaitingUser} {
		r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "主人開的", Agent: "code", Criteria: "看板有新欄位", Status: st, Ask: "要哪一種？", By: &sdk.Actor{Name: "owner(web)"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = m.Close(ctx, r.ID, ok, "code"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
			t.Fatalf("%s 的單不能自己結：%v", st, err)
		}
	}
	r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "等外部", Agent: "code", Criteria: "CI 綠", Status: sdk.TaskStatusWaitingExternal, By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.Close(ctx, r.ID, ok, "code"); err != nil {
		t.Fatalf("等外部的單照條件驗過可以自己結：%v", err)
	}
}

func fieldOf(err error) string {
	var e *platform.Err
	if errors.As(err, &e) {
		return e.Field
	}
	return ""
}

// TSK-29：標題與需求主人可以改、秘書不行；沒給就不動，標題不能空白，結束的單不能改；主人改驗收條件算主人的。
func TestUpdateTitleBodyAndCriteria(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r, err := m.Create(ctx, &sdk.TasksCreateRequest{Title: "原標題", Body: "原需求", Agent: "code", Criteria: "秘書寫的條件", By: &sdk.Actor{Name: "code"}})
	if err != nil || r.CriteriaOwner {
		t.Fatalf("秘書開單：%+v %v", r, err)
	}
	title, body := " 新標題 ", " 新需求\n第二行 "
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Title: &title, Body: &body}, "owner(web)")
	if err != nil || r.Title != "新標題" || r.Body != "新需求\n第二行" {
		t.Fatalf("改標題與需求（去頭尾空白）：%+v %v", r, err)
	}
	if got, err := m.Get(ctx, r.ID); err != nil || got.Title != "新標題" || got.Body != "新需求\n第二行" {
		t.Fatalf("讀回來要是新的：%+v %v", got, err)
	}
	if _, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Title: &title}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能改標題：%v", err)
	}
	if _, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Body: &body}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能改需求：%v", err)
	}
	blank := "  "
	if _, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Title: &blank}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("標題不能空白：%v", err)
	}
	result := "處理中"
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Result: &result}, "owner(web)")
	if err != nil || r.Title != "新標題" || r.Body != "新需求\n第二行" {
		t.Fatalf("沒給就不動：%+v %v", r, err)
	}
	criteria := "主人改的條件"
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Criteria: &criteria}, "owner(web)")
	if err != nil || r.Criteria != criteria || !r.CriteriaOwner {
		t.Fatalf("主人改的條件算主人的：%+v %v", r, err)
	}
	empty := ""
	r, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Body: &empty}, "owner(web)")
	if err != nil || r.Body != "" {
		t.Fatalf("需求給空字串＝清掉：%+v %v", r, err)
	}
	if _, err = m.Close(ctx, r.ID, &sdk.TasksCloseRequest{Status: sdk.TaskStatusCancelled, Result: "不用了"}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Title: &title}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("結束的單不能改：%v", err)
	}
}
