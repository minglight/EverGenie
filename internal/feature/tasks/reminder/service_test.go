package reminder

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

type fakeDeps struct {
	subscribed bool
	events     []map[string]any
	scheduled  []time.Time
	ids        []string
	notices    []*sdk.Notification
	opened     []*sdk.SessionOpenRequest // Sessions.Open
	sent       []*sdk.Envelope           // Bus.Send
	waits      map[string]string         // Sessions.SetRoomWait：待辦 → 對話
	journal    []string                  // Journal.AppendJournalOnce：「秘書：內容」
}

func (f *fakeDeps) AppendJournalOnce(_ context.Context, agent, line string) error {
	f.journal = append(f.journal, agent+"："+line)
	return nil
}

// fakeBus 是 fakeDeps 的 bus 面（Send 跟 Notify 的 Send 同名，所以分開）。
type fakeBus struct{ *fakeDeps }

func (b fakeBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	for _, old := range b.sent {
		if old.IdempotencyKey == e.IdempotencyKey {
			return old, nil
		}
	}
	b.sent = append(b.sent, e)
	return e, nil
}
func (f *fakeDeps) Open(_ context.Context, spec *sdk.SessionOpenRequest) (*sdk.Session, error) {
	f.opened = append(f.opened, spec)
	return &sdk.Session{Name: spec.Name}, nil
}
func (f *fakeDeps) SetRoomWait(key, room string, reason sdk.WaitingReason) {
	if f.waits == nil {
		f.waits = map[string]string{}
	}
	if room == "" {
		delete(f.waits, key)
		return
	}
	f.waits[key] = room
}

func (f *fakeDeps) At(ctx context.Context, id string, at time.Time) error {
	if !f.subscribed {
		return fmt.Errorf("scheduled before subscription")
	}
	f.scheduled = append(f.scheduled, at)
	f.ids = append(f.ids, id)
	return nil
}
func (f *fakeDeps) Publish(ctx context.Context, topic string, p map[string]any) error {
	p["topic"] = topic
	f.events = append(f.events, p)
	return nil
}
func fixture(t *testing.T) (*Module, *homefixture.Home, *fakeDeps) {
	t.Helper()
	h := homefixture.New(t)
	f := &fakeDeps{}
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Bus: fakeBus{f}, Scheduler: f, Notify: f, Sessions: f, Journal: f})
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	h.Vault.RegisterSchema(vault.Spec{Kind: "agent", Path: func(id string) string { return "vault/agents/" + id + "/agent.md" }})
	h.Vault.RegisterSchema(vault.Spec{Kind: "goal", Path: func(id string) string { return "vault/goals/" + id + ".md" }})
	h.WithRecords(vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{}}, vault.Record{Kind: "agent", ID: "work", Fields: map[string]any{"reminder_slot": "work"}}, vault.Record{Kind: "goal", ID: "GOAL-1", Fields: map[string]any{"state": "draft"}})
	m.config.Slots["work"] = map[string]any{"remind": "09:00", "check": "15:00", "label": "工作"}
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	return m, h, f
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func ptr[T any](v T) *T { return &v }
func create(t *testing.T, m *Module, in *sdk.RemindersCreateRequest) Reminder {
	t.Helper()
	if in == nil {
		in = &sdk.RemindersCreateRequest{}
	}
	in.Content = "喝水"
	if in.By == nil {
		in.By = &sdk.Actor{Name: "owner(web)"}
	}
	r, err := m.Create(context.Background(), in)
	must(t, err)
	return r
}

// Guards: REQ-59
func TestCreateUpdateSnoozeDoneCancelDoing(t *testing.T) {
	m, h, _ := fixture(t)
	ctx := context.Background()
	r := create(t, m, nil)
	r, err := m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Content: ptr("散步")}, "life")
	must(t, err)
	r, err = m.Doing(ctx, r.ID, "owner(web)")
	must(t, err)
	if r.Status != sdk.ReminderStatusDoing || r.At.Day() != h.Clock.Now().Day()+1 {
		t.Fatal(r)
	}
	r, err = m.Snooze(ctx, r.ID, "3h", "owner(web)")
	must(t, err)
	if r.Status != sdk.ReminderStatusTodo {
		t.Fatal(r)
	}
	r, err = m.Done(ctx, r.ID, "owner(web)")
	must(t, err)
	if r.Status != sdk.ReminderStatusDone || r.ClosedAt.IsZero() {
		t.Fatal(r)
	}
	if _, err = m.Cancel(ctx, r.ID, "life"); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatal(err)
	}
	other := create(t, m, nil)
	other, err = m.Cancel(ctx, other.ID, "owner(web)")
	must(t, err)
	if other.Status != sdk.ReminderStatusCancelled {
		t.Fatal(other)
	}
	for _, from := range []string{"todo", "doing", "asking", "shelved", "done", "cancelled"} {
		for _, op := range []string{"update", "snooze", "done", "cancel", "doing"} {
			r := create(t, m, nil)
			set := map[string]any{"status": from}
			if ended(from) {
				set["closed_at"] = h.Clock.Now().Format(time.RFC3339Nano)
			}
			_, err = h.Vault.Patch(ctx, "reminder", r.ID, set, nil, "system")
			must(t, err)
			if op == "update" {
				_, err = m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Content: ptr("x")}, "life")
			} else {
				_, err = m.change(ctx, r.ID, "life", op, "3h")
			}
			want := !ended(from) && (op != "doing" || from == "todo" || from == "asking")
			if (err == nil) != want {
				t.Fatalf("%s %s: %v", from, op, err)
			}
		}
	}
}

// Guards: REQ-59, REQ-70
func TestGetReadsOneReminder(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	r := create(t, m, nil)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.ID != r.ID || got.Content != "喝水" || got.Status != r.Status {
		t.Fatalf("got %+v want %+v", got, r)
	}
	if _, err = m.Get(ctx, "R-9999"); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
}

// Guards: REQ-60
func TestFollowTypes(t *testing.T) {
	for _, follow := range []sdk.Follow{sdk.FollowNone, sdk.FollowRemind, sdk.FollowConfirm, sdk.FollowCheck, sdk.FollowDiscuss} {
		t.Run(string(follow), func(t *testing.T) {
			m, h, f := fixture(t)
			r := create(t, m, &sdk.RemindersCreateRequest{Follow: follow, At: h.Clock.Now(), Check: "檢查"})
			want := 1
			if follow == sdk.FollowNone {
				want = 0
			}
			if len(f.scheduled) != want || r.Follow != follow {
				t.Fatal(r, f.scheduled)
			}
		})
	}
	m, _, _ := fixture(t)
	if _, err := m.Create(context.Background(), &sdk.RemindersCreateRequest{Content: "x", Follow: sdk.FollowCheck, By: &sdk.Actor{Name: "life"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatal(err)
	}
}

// Guards: REQ-61
func TestCycleRequiresAt(t *testing.T) {
	for _, cycle := range []sdk.Cycle{sdk.CycleDaily, sdk.CycleWeekly, sdk.CycleBiweekly, sdk.CycleMonthly} {
		m, _, _ := fixture(t)
		if _, err := m.Create(context.Background(), &sdk.RemindersCreateRequest{Content: "x", Cycle: cycle, By: &sdk.Actor{Name: "life"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatal(cycle, err)
		}
	}
}

// Guards: REQ-61
func TestWeeklyDoneAdvances(t *testing.T) {
	for _, entry := range []struct {
		cycle sdk.Cycle
		days  int
	}{{sdk.CycleDaily, 1}, {sdk.CycleWeekly, 7}, {sdk.CycleBiweekly, 14}} {
		m, h, _ := fixture(t)
		at := h.Clock.Now()
		r := create(t, m, &sdk.RemindersCreateRequest{Cycle: entry.cycle, At: at})
		got, err := m.Done(context.Background(), r.ID, "owner(web)")
		must(t, err)
		if got.Status != sdk.ReminderStatusTodo || !got.At.Equal(at.AddDate(0, 0, entry.days)) || !got.ClosedAt.IsZero() || got.Asks != 0 {
			t.Fatal(got)
		}
	}
}

// Guards: REQ-61
func TestMonthlyDoneClampsMonthEnd(t *testing.T) {
	for _, year := range []int{2027, 2028} {
		m, h, _ := fixture(t)
		at := time.Date(year, 1, 31, 9, 0, 0, 0, h.Clock.Now().Location())
		r := create(t, m, &sdk.RemindersCreateRequest{Cycle: sdk.CycleMonthly, At: at})
		got, err := m.Done(context.Background(), r.ID, "life")
		must(t, err)
		day := 28
		if year == 2028 {
			day = 29
		}
		if got.At.Month() != time.February || got.At.Day() != day || got.At.Hour() != 9 {
			t.Fatal(got.At)
		}
	}
}

// Guards: REQ-62
func TestDefaultTimeRules(t *testing.T) {
	for _, tc := range []struct {
		owner             string
		follow            sdk.Follow
		hour, minute, day int
		quiet             bool
	}{{"work", sdk.FollowConfirm, 15, 0, 23, false}, {"life", sdk.FollowConfirm, 18, 0, 23, false}, {"life", sdk.FollowRemind, 8, 30, 24, false}, {"work", sdk.FollowRemind, 8, 30, 24, true}, {"life", sdk.FollowNone, 0, 0, 0, false}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			m, _, _ := fixture(t)
			if tc.quiet {
				m.config.Slots["work"].(map[string]any)["remind"] = "23:00"
			}
			r := create(t, m, &sdk.RemindersCreateRequest{Owner: tc.owner, Follow: tc.follow})
			if tc.follow == sdk.FollowNone {
				if !r.At.IsZero() {
					t.Fatal(r.At)
				}
			} else if r.At.Hour() != tc.hour || r.At.Minute() != tc.minute || r.At.Day() != tc.day {
				t.Fatal(r.At)
			}
		})
	}
	m, h, _ := fixture(t)
	explicit := time.Date(2026, 10, 23, 23, 0, 0, 0, h.Clock.Now().Location())
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: explicit})
	if !r.At.Equal(explicit) {
		t.Fatal("explicit time deferred")
	}
}

// Guards: REQ-67
func TestRoomRouting(t *testing.T) {
	m, _, _ := fixture(t)
	r := create(t, m, &sdk.RemindersCreateRequest{Room: "life-r-1"})
	got, err := m.Update(context.Background(), r.ID, &sdk.RemindersUpdateRequest{Room: ptr("life-chat")}, "life")
	must(t, err)
	rows, err := m.List(context.Background(), nil)
	must(t, err)
	if got.Room != "life-chat" || len(rows) != 1 || rows[0].Room != "life-chat" {
		t.Fatal(got, rows)
	}
}

// Guards: REQ-69
func TestDoneEventCarriesActor(t *testing.T) {
	m, _, f := fixture(t)
	r := create(t, m, nil)
	_, err := m.Done(context.Background(), r.ID, "owner(web)")
	must(t, err)
	ev := f.events[len(f.events)-1]
	if ev["status"] != "done" || ev["by"] != "owner(web)" || ev["owner"] != "life" || ev["id"] != r.ID || ev["topic"] != "reminder.changed" {
		t.Fatal(ev)
	}
}

// Guards: REQ-69
func TestDoneCycleEventSaysDone(t *testing.T) {
	m, h, f := fixture(t)
	r := create(t, m, &sdk.RemindersCreateRequest{Cycle: sdk.CycleWeekly, At: h.Clock.Now()})
	got, err := m.Done(context.Background(), r.ID, "owner(cli)")
	must(t, err)
	if got.Status != sdk.ReminderStatusTodo || f.events[len(f.events)-1]["status"] != "done" {
		t.Fatal(got, f.events)
	}
}
func seedQueries(t *testing.T, m *Module, h *homefixture.Home) {
	t.Helper()
	now := h.Clock.Now()
	for i, tc := range []struct {
		owner, status string
		at            time.Time
		closed        bool
	}{{"life", "todo", now.Add(time.Hour), false}, {"work", "todo", now.Add(-time.Hour), false}, {"life", "asking", now.AddDate(0, 0, 1), false}, {"life", "todo", time.Time{}, false}, {"life", "done", now.AddDate(0, 0, -1), true}, {"life", "cancelled", now, false}, {"work", "todo", now.AddDate(0, 0, 10), false}} {
		fields := map[string]any{"content": "x", "owner": tc.owner, "status": tc.status}
		if !tc.at.IsZero() {
			fields["at"] = tc.at.Format(time.RFC3339Nano)
		}
		if ended(tc.status) {
			at := now
			if tc.closed {
				at = now.AddDate(0, 0, -1)
			}
			fields["closed_at"] = at.Format(time.RFC3339Nano)
		}
		h.WithRecords(vault.Record{Kind: "reminder", ID: fmt.Sprintf("R-%d", i+1), Fields: fields})
	}
}

// Guards: REQ-70
func TestQueries(t *testing.T) {
	m, h, _ := fixture(t)
	seedQueries(t, m, h)
	for _, tc := range []struct {
		q    sdk.ReminderQuery
		want int
	}{{sdk.ReminderQueryToday, 2}, {sdk.ReminderQueryOverdue, 1}, {sdk.ReminderQueryThisWeek, 3}, {sdk.ReminderQueryWaiting, 1}, {sdk.ReminderQueryNoTime, 1}, {sdk.ReminderQueryDoneYesterday, 1}} {
		rows, err := m.List(context.Background(), &sdk.RemindersListRequest{Query: tc.q})
		must(t, err)
		if len(rows) != tc.want {
			t.Fatalf("%s: got %d want %d", tc.q, len(rows), tc.want)
		}
	}
}

// Guards: REQ-71
func TestReminderPageQueries(t *testing.T) {
	m, h, _ := fixture(t)
	seedQueries(t, m, h)
	ctx := context.Background()
	raw, err := h.Vault.List(ctx, "reminder", vault.Query{})
	must(t, err)
	for _, owner := range []string{"life", "work"} {
		for _, status := range []sdk.ReminderStatus{sdk.ReminderStatusTodo, sdk.ReminderStatusDone, sdk.ReminderStatusAsking} {
			want := 0
			for _, r := range raw {
				if text(r, "owner") == owner && text(r, "status") == string(status) {
					want++
				}
			}
			rows, err := m.List(ctx, &sdk.RemindersListRequest{Owner: owner, Statuses: []sdk.ReminderStatus{status}})
			must(t, err)
			if len(rows) != want {
				t.Fatal(owner, status, len(rows), want)
			}
		}
	}

	expectedIDs := map[sdk.ReminderQuery][]string{
		sdk.ReminderQueryToday: {"R-1", "R-2"}, sdk.ReminderQueryOverdue: {"R-2"}, sdk.ReminderQueryThisWeek: {"R-1", "R-2", "R-3"}, sdk.ReminderQueryWaiting: {"R-3"}, sdk.ReminderQueryNoTime: {"R-4"}, sdk.ReminderQueryDoneYesterday: {"R-5"},
	}
	for q, ids := range expectedIDs {
		for _, owner := range []string{"life", "work"} {
			want := 0
			for _, r := range raw {
				for _, id := range ids {
					if r.ID == id && text(r, "owner") == owner {
						want++
					}
				}
			}
			got, err := m.List(ctx, &sdk.RemindersListRequest{Query: q, Owner: owner})
			must(t, err)
			if len(got) != want {
				t.Fatal(q, owner, len(got), want)
			}
		}
	}

}

// Guards: REQ-35, REQ-36
func TestReminderGoalLink(t *testing.T) {
	m, h, _ := fixture(t)
	r := create(t, m, nil)
	ctx := context.Background()
	if _, err := m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Goal: ptr("GOAL-999")}, "life"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatal(err)
	}
	// 秘書直接掛在尚未 active 的 GOAL-1 上：不需要主人確認。
	got, err := m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Goal: ptr("GOAL-1")}, "life")
	must(t, err)
	raw, err := h.Vault.Get(ctx, "reminder", r.ID)
	must(t, err)
	if got.Goal != "GOAL-1" || raw.Fields["goal"] != "[[GOAL-1]]" {
		t.Fatal(got, raw)
	}
	if _, has := raw.Fields["group"]; has {
		t.Fatal("不該再寫 group 欄位", raw.Fields)
	}
	got, err = m.Update(ctx, r.ID, &sdk.RemindersUpdateRequest{Goal: ptr("")}, "life")
	must(t, err)
	if got.Goal != "" {
		t.Fatal(got)
	}
}

// Guards: REQ-35
func TestLegacyGroupFieldReadsAsGoal(t *testing.T) {
	m, h, _ := fixture(t)
	r := create(t, m, nil)
	ctx := context.Background()
	raw, err := h.Vault.Get(ctx, "reminder", r.ID)
	must(t, err)
	raw.Fields["group"] = "[[GOAL-1]]"
	_, err = h.Vault.Put(ctx, raw, "life", raw.Revision)
	must(t, err)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.Goal != "GOAL-1" {
		t.Fatalf("舊的 group 欄位應當成 goal：%+v", got)
	}
}

// 法律第 7 條之前寫的待辦帶 site 欄位：讀得到、改得了，site 不影響任何事。
func TestLegacySiteFieldIgnored(t *testing.T) {
	m, h, _ := fixture(t)
	r := create(t, m, nil)
	ctx := context.Background()
	raw, err := h.Vault.Get(ctx, "reminder", r.ID)
	must(t, err)
	raw.Fields["site"] = "office"
	_, err = h.Vault.Put(ctx, raw, "life", raw.Revision)
	must(t, err)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.ID != r.ID || got.Content != r.Content {
		t.Fatalf("帶舊 site 的待辦要讀得到：%+v", got)
	}
	done, err := m.Done(ctx, r.ID, "life")
	must(t, err)
	if done.Status != sdk.ReminderStatusDone {
		t.Fatalf("帶舊 site 的待辦要改得了：%+v", done)
	}
}

// Guards: REQ-59
func TestSnoozeValues(t *testing.T) {
	for _, tc := range []struct {
		until string
		hours int
	}{{"3h", 3}, {"30m", 0}, {"tomorrow", 30}, {"tomorrow-same", 25}, {"2026-10-24T14:00:00+08:00", 26}} {
		t.Run(tc.until, func(t *testing.T) {
			m, h, _ := fixture(t)
			now := h.Clock.Now()
			r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, At: now.Add(time.Hour)})
			got, err := m.Snooze(context.Background(), r.ID, tc.until, "life")
			must(t, err)
			want := now.Add(time.Duration(tc.hours) * time.Hour)
			if tc.until == "30m" {
				want = now.Add(30 * time.Minute)
			}
			if !got.At.Equal(want) {
				t.Fatal(got.At, want)
			}
		})
	}
}

func (b *fakeDeps) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	b.subscribed = true
	return nil, nil
}

func (f *fakeDeps) Send(ctx context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	f.notices = append(f.notices, n)
	return n, nil
}

// Guards: REQ-59
func TestQueryUpcoming(t *testing.T) {
	m, h, _ := fixture(t)
	seedQueries(t, m, h)
	now := h.Clock.Now()
	nextMonth := now.AddDate(0, 1, 0)
	h.WithRecords(
		vault.Record{Kind: "reminder", ID: "R-8", Fields: map[string]any{"content": "x", "owner": "life", "status": "todo", "at": nextMonth.Format(time.RFC3339Nano)}},
		vault.Record{Kind: "reminder", ID: "R-9", Fields: map[string]any{"content": "x", "owner": "life", "status": "done", "at": nextMonth.Format(time.RFC3339Nano), "closed_at": now.Format(time.RFC3339Nano)}},
	)
	rows, err := m.List(context.Background(), &sdk.RemindersListRequest{Query: sdk.ReminderQueryUpcoming})
	must(t, err)
	got := []string{}
	for _, r := range rows {
		got = append(got, r.ID)
	}
	// 明天（R-3）、10 天後（R-7）、下個月（R-8）在，由早到晚；今天（R-1、R-2、R-6）、沒時間（R-4）、已結束（R-5、R-9）不在。
	if strings.Join(got, ",") != "R-3,R-7,R-8" {
		t.Fatal(got)
	}
}
