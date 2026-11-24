package reminder

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

func fire(t *testing.T, m *Module, id string) {
	t.Helper()
	must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "schedule.fire", Payload: map[string]any{"kind": "at", "id": id}}))
}

// Guards: REQ-60, REQ-63, REQ-67
func TestDueSendsWithoutSession(t *testing.T) {
	m, h, f := fixture(t)
	now := h.Clock.Now()
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: now, Room: "life-test"})
	fire(t, m, r.ID)
	fire(t, m, r.ID)
	if len(f.notices) != 1 || f.notices[0].Reminder != r.ID || f.notices[0].Room != "life-test" || len(f.notices[0].Options) != 3 {
		t.Fatal(f.notices)
	}
	got, err := readReminder(m, r.ID)
	must(t, err)
	if got.Status != sdk.ReminderStatusAsking || !got.SentAt.Equal(now) {
		t.Fatal(got)
	}
	none := create(t, m, nil)
	fire(t, m, none.ID)
	if len(f.notices) != 1 {
		t.Fatal("none sent")
	}
}

// Guards: REQ-63, REQ-69
func TestButtonAnswersApply(t *testing.T) {
	for _, answer := range []string{"做了", "延到明天同一時間", "3 小時後", "取消"} {
		t.Run(answer, func(t *testing.T) {
			m, h, _ := fixture(t)
			now := h.Clock.Now()
			r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: now})
			fire(t, m, r.ID)
			must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"reminder": r.ID, "text": answer}}))
			got, err := readReminder(m, r.ID)
			must(t, err)
			switch answer {
			case "做了":
				if got.Status != sdk.ReminderStatusDone {
					t.Fatal(got)
				}
			case "取消":
				if got.Status != sdk.ReminderStatusCancelled {
					t.Fatal(got)
				}
			case "3 小時後":
				if !got.At.Equal(now.Add(3 * time.Hour)) {
					t.Fatal(got)
				}
			case "延到明天同一時間":
				if !got.At.Equal(now.AddDate(0, 0, 1)) {
					t.Fatal(got)
				}
			}
			if got.By != "owner(web)" {
				t.Fatal(got.By)
			}
		})
	}
}

// Guards: REQ-64
func TestCheckOpensRoomThenFallsBackAfter10Min(t *testing.T) {
	for _, noticed := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "notice"}[noticed], func(t *testing.T) {
			m, h, f := fixture(t)
			now := h.Clock.Now()
			r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowCheck, Check: "查付款", At: now, Room: "life-r-1"})
			fire(t, m, r.ID)
			if len(f.notices) != 0 {
				t.Fatal("premature notice")
			}
			fire(t, m, r.ID) // 同一次到期再觸發一次：不重開、不重送
			if len(f.opened) != 1 || f.opened[0].Name != "life-r-1" || f.opened[0].Agent != "life" || f.opened[0].Kind != sdk.SessionKindRoom || f.opened[0].Topic != r.Content {
				t.Fatal(f.opened)
			}
			if len(f.sent) != 1 || f.sent[0].To != "life-r-1" || f.sent[0].Kind != sdk.EnvelopeKindRoutine || f.sent[0].Body != "［待辦 "+r.ID+"］查付款" {
				t.Fatal(f.sent)
			}
			if f.waits[r.ID] != "life-r-1" {
				t.Fatal("專屬對話要顯示等待辦", f.waits)
			}
			h.Clock.Advance(9 * time.Minute)
			if noticed {
				must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.sent", Payload: map[string]any{"reminder": r.ID}}))
			}
			fire(t, m, r.ID+"#check")
			if len(f.notices) != 0 {
				t.Fatal("early fallback")
			}
			h.Clock.Advance(time.Minute)
			fire(t, m, r.ID+"#check")
			fire(t, m, r.ID+"#check")
			want := 1
			if noticed {
				want = 0
			}
			if len(f.notices) != want {
				t.Fatal(f.notices)
			}
			if want == 1 && f.notices[0].Title != r.Content {
				t.Fatal(f.notices[0])
			}
		})
	}
}

// Guards: REQ-65
func TestDiscussHasChatLink(t *testing.T) {
	m, h, f := fixture(t)
	now := h.Clock.Now()
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowDiscuss, At: now})
	fire(t, m, r.ID)
	n := f.notices[0]
	if n.Category != sdk.CategoryOpening || n.URL != "/chat/life-r-"+strings.TrimPrefix(r.ID, "R-") {
		t.Fatal(n)
	}
}

// RMD-8：沒回不自動擱置；當天不再推，隔天同一時間最多再問到上限，之後留著不推，主人一碰就恢復。
func TestNoReplyNeverShelvesAndStopsPushing(t *testing.T) {
	m, h, f := fixture(t)
	now := h.Clock.Now()
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: now})
	for i := 1; i <= 3; i++ {
		fire(t, m, r.ID)
		h.Clock.Advance(30 * time.Minute)
		fire(t, m, r.ID+"#noreply")
		got, err := readReminder(m, r.ID)
		must(t, err)
		if int(got.Asks) != i || got.Status != sdk.ReminderStatusTodo {
			t.Fatal(got)
		}
		if i < 3 {
			if got.At.In(h.Clock.Now().Location()).Format(time.DateOnly) == h.Clock.Now().Format(time.DateOnly) {
				t.Fatalf("同一天不能再推：%v", got.At)
			}
			h.Clock.Advance(got.At.Sub(h.Clock.Now()))
		}
	}
	h.Clock.Advance(48 * time.Hour)
	fire(t, m, r.ID)
	if len(f.notices) != 3 {
		t.Fatal("上限之後不再推", len(f.notices))
	}
	if _, err := m.Snooze(context.Background(), r.ID, SnoozeLater, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	got, err := readReminder(m, r.ID)
	must(t, err)
	if got.Asks != 0 {
		t.Fatal("主人回了要重算", got.Asks)
	}
	h.Clock.Advance(got.At.Sub(h.Clock.Now()))
	fire(t, m, r.ID)
	if len(f.notices) != 4 {
		t.Fatal("主人碰過要恢復", len(f.notices))
	}
}

// RMD-5：通知只有三顆鈕。
func TestDueNoticeHasThreeOptions(t *testing.T) {
	m, h, f := fixture(t)
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: h.Clock.Now()})
	fire(t, m, r.ID)
	if got := strings.Join(f.notices[0].Options, ","); got != "做了,晚點,今天不做" {
		t.Fatal(got)
	}
}

// RMD-5：今天不做：單次的明天同一時間（逾期很久也一樣），週期的跳到下一次；都不給點。
func TestNotTodayAnswer(t *testing.T) {
	answer := func(t *testing.T, m *Module, id string) {
		must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"reminder": id, "text": AnswerNotToday}}))
	}
	t.Run("once", func(t *testing.T) {
		m, h, _ := fixture(t)
		at := h.Clock.Now().AddDate(0, 0, -5)
		r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: at})
		answer(t, m, r.ID)
		got, err := readReminder(m, r.ID)
		must(t, err)
		if !got.At.Equal(at.AddDate(0, 0, 6)) || got.Status != sdk.ReminderStatusTodo || got.DonePoints != 0 {
			t.Fatal(got)
		}
	})
	t.Run("weekly", func(t *testing.T) {
		m, h, _ := fixture(t)
		at := h.Clock.Now()
		r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, Cycle: sdk.CycleWeekly, At: at})
		answer(t, m, r.ID)
		got, err := readReminder(m, r.ID)
		must(t, err)
		if !got.At.Equal(at.AddDate(0, 0, 7)) || got.DonePoints != 0 {
			t.Fatal(got)
		}
	})
}

// RMD-5：晚點是 3 小時後，落在安靜時段就延到結束。
func TestLaterAnswer(t *testing.T) {
	m, h, _ := fixture(t)
	now := h.Clock.Now()
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: now})
	must(t, m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"reminder": r.ID, "text": AnswerLater}}))
	got, err := readReminder(m, r.ID)
	must(t, err)
	want := m.quiet(now).Defer(now.Add(3 * time.Hour))
	if !got.At.Equal(want) {
		t.Fatal(got.At, want)
	}
}

// RMD-24：難度給點；取消不給；週期每次完成都給，同一天連按只給一次；沒有 done_points 的舊待辦從 0 算。
func TestDonePoints(t *testing.T) {
	ctx := context.Background()
	for diff, want := range map[sdk.Difficulty]int32{sdk.DifficultyEasy: 1, "": 2, sdk.DifficultyNormal: 2, sdk.DifficultyHard: 3} {
		m, h, _ := fixture(t)
		r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, At: h.Clock.Now(), Difficulty: diff})
		got, err := m.Done(ctx, r.ID, "owner(web)")
		must(t, err)
		if got.DonePoints != want || got.DoneAt.IsZero() {
			t.Fatal(diff, got.DonePoints)
		}
	}
	m, h, _ := fixture(t)
	c := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: h.Clock.Now()})
	got, err := m.Cancel(ctx, c.ID, "owner(web)")
	must(t, err)
	if got.DonePoints != 0 {
		t.Fatal(got)
	}
	d := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowCheck, Check: "看一下", Cycle: sdk.CycleDaily, At: h.Clock.Now()})
	_, err = m.Done(ctx, d.ID, "owner(web)")
	must(t, err)
	got, err = m.Done(ctx, d.ID, "owner(web)")
	must(t, err)
	if got.DonePoints != 2 {
		t.Fatal("同一天只給一次", got.DonePoints)
	}
	h.Clock.Advance(24 * time.Hour)
	got, err = m.Done(ctx, d.ID, "owner(web)")
	must(t, err)
	if got.DonePoints != 4 {
		t.Fatal("隔天再給", got.DonePoints)
	}
	if !got.At.After(h.Clock.Now()) {
		t.Fatal("下一次要在未來", got.At)
	}
}

// RMD-26（DEV-149）：只有「問我做了沒」「秘書幫我確認」集點；過期每滿一週多 1 點，延後、還在做不會讓過期重算；看板拿得到現在做完幾點。
func TestPointsOnlyConfirmAndOverdue(t *testing.T) {
	ctx := context.Background()
	m, h, _ := fixture(t)
	for _, f := range []sdk.Follow{sdk.FollowNone, sdk.FollowRemind, sdk.FollowDiscuss} {
		r := create(t, m, &sdk.RemindersCreateRequest{Follow: f, At: h.Clock.Now(), Difficulty: sdk.DifficultyHard})
		if r.Points != 0 {
			t.Fatal(f, "不集點的標籤要是 0", r.Points)
		}
		got, err := m.Done(ctx, r.ID, "owner(web)")
		must(t, err)
		if got.DonePoints != 0 {
			t.Fatal(f, "不集點", got.DonePoints)
		}
	}
	past := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, At: h.Clock.Now().Add(-15 * 24 * time.Hour)})
	if past.Points != 2 {
		t.Fatal("往回填的時間不算過期", past.Points)
	}
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, At: h.Clock.Now()})
	h.Clock.Advance(15 * 24 * time.Hour)
	if got, _ := m.Get(ctx, r.ID); got.Points != 4 {
		t.Fatal("過期兩週：普通 2＋2", got.Points)
	}
	if got, _ := m.Get(ctx, past.ID); got.Points != 4 {
		t.Fatal("補記的從寫下去那刻算，也過了兩週", got.Points)
	}
	_, err := m.Snooze(ctx, r.ID, "later", "owner(web)")
	must(t, err)
	_, err = m.Doing(ctx, r.ID, "owner(web)")
	must(t, err)
	h.Clock.Advance(7 * 24 * time.Hour)
	got, err := m.Get(ctx, r.ID)
	must(t, err)
	if got.Points != 5 {
		t.Fatal("延後、還在做不重算過期，再過一週多 1", got.Points)
	}
	got, err = m.Done(ctx, r.ID, "owner(web)")
	must(t, err)
	if got.DonePoints != 5 || got.Points != 0 {
		t.Fatal("做完拿到 5、標籤歸 0", got.DonePoints, got.Points)
	}
	// 秘書改時間跟延後一樣：過期不重算。
	s := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, At: h.Clock.Now()})
	h.Clock.Advance(8 * 24 * time.Hour)
	later := h.Clock.Now().Add(24 * time.Hour)
	got, err = m.Update(ctx, s.ID, &sdk.RemindersUpdateRequest{At: &later}, "life")
	must(t, err)
	if got.Points != 3 {
		t.Fatal("秘書改時間不重算過期", got.Points)
	}
	// 主人自己改時間：過期從新的時間算。
	e := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowCheck, Check: "看", At: h.Clock.Now(), Difficulty: sdk.DifficultyEasy})
	h.Clock.Advance(8 * 24 * time.Hour)
	_, err = m.Snooze(ctx, e.ID, "later", "owner(web)")
	must(t, err)
	at := h.Clock.Now()
	got, err = m.Update(ctx, e.ID, &sdk.RemindersUpdateRequest{At: &at}, "owner(web)")
	must(t, err)
	if got.Points != 1 {
		t.Fatal("改了時間就不算過期", got.Points)
	}
	// 週期待辦今天不做：跳到下一次，過期從頭算。
	c := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, Cycle: sdk.CycleWeekly, At: h.Clock.Now().Add(time.Hour)})
	rec, err := m.d.Vault.Get(ctx, "reminder", c.ID) // 直接寫成十天前（模擬一直沒做），不經過新增的往回填檢查
	must(t, err)
	rec.Fields["at"] = h.Clock.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339Nano)
	_, err = m.d.Vault.Put(ctx, rec, "test", rec.Revision)
	must(t, err)
	c, err = m.Get(ctx, c.ID)
	must(t, err)
	if c.Points != 3 {
		t.Fatal("週期過期一週", c.Points)
	}
	got, err = m.Snooze(ctx, c.ID, SnoozeNotToday, "owner(web)")
	must(t, err)
	if got.Points != 2 {
		t.Fatal("跳到下一次，不算過期", got.Points, got.At)
	}
}

// 週期待辦逾期很久才做：下一次跳到未來，不補中間幾次。
func TestCycleDoneSkipsMissedOccurrences(t *testing.T) {
	m, h, _ := fixture(t)
	at := h.Clock.Now().AddDate(0, 0, -3)
	r := create(t, m, &sdk.RemindersCreateRequest{Cycle: sdk.CycleDaily, Follow: sdk.FollowRemind, At: at})
	got, err := m.Done(context.Background(), r.ID, "owner(web)")
	must(t, err)
	if !got.At.Equal(at.AddDate(0, 0, 4)) {
		t.Fatal(got.At)
	}
}

// Guards: REQ-68
func TestRoutineConfirmSource(t *testing.T) {
	m, h, f := fixture(t)
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowConfirm, Source: "routine:life-morning"})
	if r.At.Sub(h.Clock.Now()) > time.Minute {
		t.Fatal(r.At)
	}
	h.Clock.Advance(time.Minute)
	fire(t, m, r.ID)
	if len(f.notices) != 1 || !strings.Contains(f.notices[0].Title, "做了嗎") {
		t.Fatal(f.notices)
	}
	got, err := readReminder(m, r.ID)
	must(t, err)
	if got.Source != r.Source {
		t.Fatal(got.Source)
	}
}

// Guards: REQ-63, REQ-66
func TestRestartReRegistersAt(t *testing.T) {
	m, h, f := fixture(t)
	now := h.Clock.Now()
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: now})
	fire(t, m, r.ID)
	must(t, m.Stop(context.Background()))
	f.subscribed = false
	f.ids = nil
	f.scheduled = nil
	must(t, m.Start(context.Background()))
	if len(f.ids) != 2 || f.ids[0] != r.ID || f.ids[1] != r.ID+"#noreply" {
		t.Fatal(f.ids)
	}
	fire(t, m, r.ID)
	if len(f.notices) != 1 {
		t.Fatal("duplicate after restart")
	}
}

func readReminder(m *Module, id string) (Reminder, error) {
	r, err := m.d.Vault.Get(context.Background(), "reminder", id)
	if err != nil {
		return nil, err
	}
	return decode(r)
}

// Guards: REQ-64
// 沒指定對話的查證待辦用「<owner>-r-<編號>」開；做完之後那個對話不再顯示等待辦。
func TestCheckDefaultRoomAndWaitClears(t *testing.T) {
	m, h, f := fixture(t)
	r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowCheck, Check: "查付款", At: h.Clock.Now()})
	fire(t, m, r.ID)
	room := "life-r-" + strings.TrimPrefix(r.ID, "R-")
	if len(f.opened) != 1 || f.opened[0].Name != room || f.waits[r.ID] != room {
		t.Fatal(f.opened, f.waits)
	}
	_, err := m.Done(context.Background(), r.ID, "owner(web)")
	must(t, err)
	if _, ok := f.waits[r.ID]; ok {
		t.Fatal("做完還在等", f.waits)
	}
}

// 重啟時把有專屬對話、還沒結束的待辦整批登記給 session。
func TestStartRegistersRoomWaits(t *testing.T) {
	m, _, f := fixture(t)
	create(t, m, &sdk.RemindersCreateRequest{Room: "life-talk"})
	must(t, m.Stop(context.Background()))
	f.waits = nil
	must(t, m.Start(context.Background()))
	if len(f.waits) != 1 {
		t.Fatal(f.waits)
	}
}

// Guards: REQ-69
// 主人完成待辦：負責秘書今天的日誌記「主人完成：<內容>（R-n）」；秘書自己完成的不記（RMD-11）。
// reminder.changed 帶改之前的狀態 from（notify 用它判斷離開 asking，不必讀待辦）。
func TestOwnerDoneWritesJournalAndChangedHasFrom(t *testing.T) {
	m, _, f := fixture(t)
	ctx := context.Background()
	a := create(t, m, nil)
	_, err := m.Done(ctx, a.ID, "owner(web)")
	must(t, err)
	b := create(t, m, nil)
	_, err = m.Done(ctx, b.ID, "life")
	must(t, err)
	if len(f.journal) != 1 || f.journal[0] != "life：主人完成：喝水（"+a.ID+"）" {
		t.Fatal(f.journal)
	}
	last := f.events[len(f.events)-1]
	if last["topic"] != "reminder.changed" || last["status"] != "done" || last["from"] != "todo" {
		t.Fatal(last)
	}
}

// 週期待辦按「晚點」只動這一次：下一次還是原本的時間（RMD-5）。
func TestLaterKeepsCycleClock(t *testing.T) {
	ctx := context.Background()
	m, h, _ := fixture(t)
	at := h.Clock.Now().Add(time.Hour)
	r := create(t, m, &sdk.RemindersCreateRequest{Cycle: sdk.CycleDaily, Follow: sdk.FollowRemind, At: at})
	h.Clock.Advance(time.Hour)
	_, err := m.Snooze(ctx, r.ID, SnoozeLater, "owner(web)")
	must(t, err)
	_, err = m.Snooze(ctx, r.ID, SnoozeLater, "owner(web)")
	must(t, err)
	got, err := m.Done(ctx, r.ID, "owner(web)")
	must(t, err)
	if !got.At.Equal(at.AddDate(0, 0, 1)) {
		t.Fatal(got.At, at)
	}
	_, err = m.Snooze(ctx, r.ID, SnoozeLater, "owner(web)")
	must(t, err)
	got, err = m.Snooze(ctx, r.ID, SnoozeNotToday, "owner(web)")
	must(t, err)
	if !got.At.Equal(at.AddDate(0, 0, 1)) {
		t.Fatal(got.At, at)
	}
}

// 逾期的週期待辦按「做了」，今天還沒到的那一次照常；按「今天不做」才跳過今天（RMD-5）。
func TestDoneKeepsTodaysOccurrence(t *testing.T) {
	ctx := context.Background()
	m, h, _ := fixture(t)
	at := h.Clock.Now().Add(time.Minute).AddDate(0, 0, -1)
	r := create(t, m, &sdk.RemindersCreateRequest{Cycle: sdk.CycleDaily, Follow: sdk.FollowRemind, At: at})
	got, err := m.Done(ctx, r.ID, "owner(web)")
	must(t, err)
	if !got.At.Equal(at.AddDate(0, 0, 1)) {
		t.Fatal(got.At, at)
	}
	s := create(t, m, &sdk.RemindersCreateRequest{Cycle: sdk.CycleDaily, Follow: sdk.FollowRemind, At: at})
	got, err = m.Snooze(ctx, s.ID, SnoozeNotToday, "owner(web)")
	must(t, err)
	if !got.At.After(at.AddDate(0, 0, 1)) {
		t.Fatal(got.At, at)
	}
}

// 主人碰過（還在做、改內容）沒回的次數就歸零，不會停在不推的狀態（RMD-8）。
func TestAnyOwnerActionResetsAsks(t *testing.T) {
	ctx := context.Background()
	m, h, _ := fixture(t)
	for _, act := range []func(id string) (Reminder, error){
		func(id string) (Reminder, error) { return m.Doing(ctx, id, "owner(web)") },
		func(id string) (Reminder, error) {
			content := "改過"
			return m.Update(ctx, id, &sdk.RemindersUpdateRequest{Content: &content}, "owner(web)")
		},
	} {
		r := create(t, m, &sdk.RemindersCreateRequest{Follow: sdk.FollowRemind, At: h.Clock.Now()})
		rec, err := m.d.Vault.Get(ctx, "reminder", r.ID)
		must(t, err)
		rec.Fields["asks"] = m.maxAsks
		_, err = m.d.Vault.Put(ctx, rec, "system", rec.Revision)
		must(t, err)
		got, err := act(r.ID)
		must(t, err)
		if got.Asks != 0 {
			t.Fatal(got.Asks)
		}
	}
}
