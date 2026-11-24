package task

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-148
func TestNudgeSendsAndMarks(t *testing.T) {
	m, h, f := fixture(t)
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(context.Background(), r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("選哪個？")}, "code")
	must(t, err)
	if len(f.crons) != 1 || f.crons[0] != "0 9 * * *" {
		t.Fatal(f.crons)
	}
	f.notices = nil
	h.Clock.Advance(72*time.Hour + time.Minute)
	ev := &sdk.BusSubscribeResponse{Topic: "schedule.fire", Payload: map[string]any{"id": "tasks.nudge", "kind": "cron"}}
	must(t, m.handle(context.Background(), ev))
	must(t, m.handle(context.Background(), ev))
	if len(f.notices) != 1 || f.notices[0].Task != r.ID || f.notices[0].Todo != "選哪個？" || f.notices[0].Agent != "code" {
		t.Fatal(f.notices)
	}
	got, err := m.Get(context.Background(), r.ID)
	must(t, err)
	if got.Nudges != 1 || !got.LastNudgedAt.Equal(h.Clock.Now()) {
		t.Fatal(got)
	}
}

// Guards: REQ-54
func TestEnterWaitingPushesOnce(t *testing.T) {
	m, _, f := fixture(t)
	ctx := context.Background()
	r := makeTask(t, m, sdk.TaskTypeTask)
	_, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("選哪個？")}, "code")
	must(t, err)
	_, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Result: ptr("補充資訊")}, "code")
	must(t, err)
	if len(f.notices) != 1 || f.notices[0].Category != sdk.CategoryApprove {
		t.Fatal(f.notices)
	}
	_, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusDoing)}, "code")
	must(t, err)
	_, err = m.Submit(ctx, r.ID, submission(), "code")
	must(t, err)
	_, err = m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Result: ptr("補充成果")}, "code")
	must(t, err)
	if len(f.notices) != 2 || f.notices[1].Todo != "請驗收" {
		t.Fatal(f.notices)
	}
}

type fakeWork struct {
	opened    []string
	refreshed int
}

func (w *fakeWork) AutoOpenWork(_ context.Context, id, agent, by string) {
	w.opened = append(w.opened, id+"/"+agent+"/"+by)
}
func (w *fakeWork) RefreshWork() { w.refreshed++ }

// Guards: REQ-217, REQ-186
// 單從別的狀態改成 doing 才請 session 開工作 session；新開的單（from 是空的）、doing → doing、改成別的狀態都不開，
// 但每次變化都請 session 對帳（P7-5、REQ-186；2026-11-01 從 session 搬來）。
func TestDoingOpensWorkSession(t *testing.T) {
	m, _, _ := fixture(t)
	w := &fakeWork{}
	m.SetWork(w)
	changed := func(status, from, by string) {
		must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "task.changed", Payload: map[string]any{"id": "SEC-2", "status": status, "from": from, "agent": "code", "by": by}}))
	}
	changed("doing", "idea", "code")
	changed("doing", "", "owner(web)")
	changed("doing", "doing", "code")
	changed("waiting_user", "doing", "code")
	if len(w.opened) != 1 || w.opened[0] != "SEC-2/code/code" || w.refreshed != 4 {
		t.Fatalf("開 %v、對帳 %d 次", w.opened, w.refreshed)
	}
}

// 催的時候送出新的一則之後，tasks 自己推過、這張單上還沒回的舊那則收掉（via=auto 的 Retire），只留新的（DEV-156 U-05、TSK-30）。
func TestNudgeRetiresOlderSameQuestion(t *testing.T) {
	ctx := context.Background()
	nudge := func(t *testing.T, m *Module, h *homefixture.Home) {
		t.Helper()
		h.Clock.Advance(72*time.Hour + time.Minute)
		must(t, m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "schedule.fire", Payload: map[string]any{"id": "tasks.nudge", "kind": "cron"}}))
	}

	// 等你決定：同一句的舊那則、秘書在等你期間改了問句留下的舊問句都換掉；秘書自己綁單發的（有對話、帶選項）、別張單的不動。
	t.Run("等你決定", func(t *testing.T) {
		m, h, f := fixture(t)
		r := makeTask(t, m, sdk.TaskTypeTask)
		_, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("選哪個？")}, "code")
		must(t, err)
		f.open = []sdk.Notification{
			{ID: "M-1", Task: r.ID, Todo: "選哪個？", Typed: true, State: sdk.NotificationStateUnread},
			{ID: "M-2", Task: r.ID, Todo: "改之前的問句", Typed: true, State: sdk.NotificationStateUnread},
			{ID: "M-3", Task: "SEC-999", Todo: "選哪個？", Typed: true, State: sdk.NotificationStateUnread},
			{ID: "M-4", Task: r.ID, Todo: "選哪個？", Room: "code", Options: []string{"A", "B"}, State: sdk.NotificationStateUnread},
		}
		f.notices = nil
		nudge(t, m, h)
		if len(f.notices) != 1 || f.notices[0].Todo != "選哪個？" {
			t.Fatalf("送新的一則：%v", f.notices)
		}
		want := []string{"M-1：" + nudgedReason, "M-2：" + nudgedReason}
		if !slices.Equal(f.retired, want) {
			t.Fatalf("收掉的是 tasks 自己推的、這張單的舊那則：%v", f.retired)
		}
	})

	// 交付等驗收：只換「請驗收」，送驗收前的舊問句照 TSK-25 留著。
	t.Run("交付等驗收", func(t *testing.T) {
		m, h, f := fixture(t)
		r := makeTask(t, m, sdk.TaskTypeTask)
		_, err := m.Submit(ctx, r.ID, submission(), "code")
		must(t, err)
		f.open = []sdk.Notification{
			{ID: "M-1", Task: r.ID, Todo: reviewAsk, Options: []string{answerAccept, answerReject}, State: sdk.NotificationStateUnread},
			{ID: "M-2", Task: r.ID, Todo: "送驗收前的問句", Typed: true, State: sdk.NotificationStateUnread},
		}
		f.notices = nil
		nudge(t, m, h)
		if len(f.notices) != 1 || f.notices[0].Todo != reviewAsk {
			t.Fatalf("送新的「請驗收」：%v", f.notices)
		}
		if !slices.Equal(f.retired, []string{"M-1：" + nudgedReason}) {
			t.Fatalf("只換「請驗收」：%v", f.retired)
		}
	})
}

// 主人回了單上那句、答案會轉回秘書（relay）：同一句的其他則（舊資料以前催一次就多一則）一起收掉，單回到進行中，不用每則都回（TSK-30）。
func TestAnsweringOneOfDuplicateAsksRetiresTheRest(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*Module, *fakeDeps, string) {
		t.Helper()
		m, _, f := fixture(t)
		r := makeTask(t, m, sdk.TaskTypeTask)
		_, err := m.Update(ctx, r.ID, &sdk.TasksUpdateRequest{Status: ptr(sdk.TaskStatusWaitingUser), Ask: ptr("選哪個？")}, "code")
		must(t, err)
		return m, f, r.ID
	}
	answer := func(t *testing.T, m *Module, task, todo string, relay bool) {
		t.Helper()
		must(t, m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": "M-1", "task": task, "via": "chat", "text": "A", "by": "owner(web)", "todo": todo, "relay": relay}}))
	}
	status := func(t *testing.T, m *Module, id string) sdk.TaskStatus {
		t.Helper()
		got, err := m.Get(ctx, id)
		must(t, err)
		return got.Status
	}
	answered := sdk.Notification{ID: "M-1", Todo: "選哪個？", Typed: true, State: sdk.NotificationStateAnswered}

	t.Run("同一句的另一則收掉、回的那則不動，單回進行中", func(t *testing.T) {
		m, f, id := setup(t)
		mine := answered
		mine.Task = id
		f.open = []sdk.Notification{mine, {ID: "M-2", Task: id, Todo: "選哪個？", Typed: true, State: sdk.NotificationStateUnread}}
		answer(t, m, id, "選哪個？", true)
		if !slices.Equal(f.retired, []string{"M-2：" + answeredReason}) {
			t.Fatalf("只收同一句的另一則：%v", f.retired)
		}
		if got := status(t, m, id); got != sdk.TaskStatusDoing {
			t.Fatalf("同一個問題回過了，單要回進行中：%s", got)
		}
	})

	// 秘書另外問的、問句剛好一樣（都是「請回覆」，不是單上那句）：是別題，留著，單照樣等你。
	t.Run("不是單上那句不收", func(t *testing.T) {
		m, f, id := setup(t)
		f.open = []sdk.Notification{{ID: "M-2", Task: id, Todo: "請回覆", Room: "code", State: sdk.NotificationStateUnread}}
		answer(t, m, id, "請回覆", true)
		if len(f.retired) != 0 || status(t, m, id) != sdk.TaskStatusWaitingUser {
			t.Fatalf("別題不收、單照樣等你：retired=%v status=%s", f.retired, status(t, m, id))
		}
	})

	// 答案不會轉回秘書（relay=false）：只收打字的那則留著，秘書才讀得到主人答了什麼。
	t.Run("答案不轉回秘書就不收", func(t *testing.T) {
		m, f, id := setup(t)
		f.open = []sdk.Notification{{ID: "M-2", Task: id, Todo: "選哪個？", Typed: true, State: sdk.NotificationStateUnread}}
		answer(t, m, id, "選哪個？", false)
		if len(f.retired) != 0 || status(t, m, id) != sdk.TaskStatusWaitingUser {
			t.Fatalf("不轉話的回答不收會轉話的那則：retired=%v status=%s", f.retired, status(t, m, id))
		}
	})
}
