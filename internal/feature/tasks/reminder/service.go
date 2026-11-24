package reminder

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/tasks/attach"
	"github.com/minglight/evergenie/internal/feature/tasks/goal"
	"github.com/minglight/evergenie/sdk"
)

func decode(r vault.Record) (Reminder, error) {
	fields := make(map[string]any, len(r.Fields))
	for k, v := range r.Fields {
		fields[k] = v
	}
	vault.UpgradeLegacyGoal(fields)
	b, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	out := &sdk.Reminder{}
	if err = json.Unmarshal(b, out); err != nil {
		return nil, err
	}
	out.ID = r.ID
	out.Body = r.Body
	out.Archived = r.Archived
	out.UpdatedAt = r.UpdatedAt
	out.Goal = strings.TrimSuffix(strings.TrimPrefix(out.Goal, "[["), "]]")
	return out, nil
}
func (m *Module) Get(ctx context.Context, id string) (Reminder, error) {
	r, err := m.d.Vault.Get(ctx, "reminder", id)
	if err != nil {
		return nil, err
	}
	return m.view(r)
}

// view 是給外面看的待辦：多帶「現在做完能拿幾點」（RMD-26，算出來的、不存）。
func (m *Module) view(r vault.Record) (Reminder, error) {
	out, err := decode(r)
	if err != nil {
		return nil, err
	}
	out.Points = int32(pointsNow(r, m.d.Clock.Now().In(m.config.Location())))
	return out, nil
}

// Snooze 的 until 除了 Go duration、RFC3339、tomorrow、tomorrow-same，還收這兩個（提醒的一鍵回覆，RMD-5）。
const (
	SnoozeLater    = "later"     // 晚點：3 小時後，落在安靜時段就延到安靜時段結束
	SnoozeNotToday = "not-today" // 今天不做：單次的明天同一時間，週期的跳到下一次，不給點
	laterAfter     = 3 * time.Hour
)

func allowed(op, from string) bool {
	switch op {
	case "create":
		return from == ""
	case "doing":
		return from == "todo" || from == "asking"
	case "update", "snooze", "done", "cancel":
		return from == "todo" || from == "doing" || from == "asking" || from == "shelved"
	}
	return false
}
func (m *Module) defaultAt(ctx context.Context, r vault.Record, tomorrow bool) (time.Time, error) {
	slot, _ := m.config.Slots["default"].(string)
	agent, err := m.d.Vault.Get(ctx, "agent", text(r, "owner"))
	if err != nil {
		return time.Time{}, err
	}
	if s := text(agent, "reminder_slot"); s != "" {
		slot = s
	}
	table, ok := m.config.Slots[slot].(map[string]any)
	if !ok {
		return time.Time{}, platform.Invalid("reminder_slot", "提醒時間組不存在")
	}
	key := "check"
	if text(r, "follow") == "remind" {
		key = "remind"
	}
	s, _ := table[key].(string)
	clock, err := time.Parse("15:04", s)
	if err != nil {
		return time.Time{}, platform.Invalid("reminder_slot", "提醒時間格式不正確")
	}
	now := m.d.Clock.Now().In(m.config.Location())
	at := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if tomorrow || at.Before(now) {
		at = at.AddDate(0, 0, 1)
	}
	return m.quiet(now).Defer(at), nil
}

// quiet 是主人的安靜時段。
func (m *Module) quiet(now time.Time) platform.Quiet {
	hours := strings.Split(m.config.User.QuietHours, "-")
	return platform.Quiet{Start: hours[0], End: hours[1], DeferTo: m.config.User.QuietDeferTo, Loc: now.Location()}
}
func (m *Module) ensureAt(ctx context.Context, r *vault.Record) error {
	if text(*r, "at") != "" {
		return nil
	}
	if text(*r, "cycle") != "once" {
		return platform.Invalid("at", "週期待辦必須指定時間")
	}
	if text(*r, "follow") == "none" {
		return nil
	}
	if text(*r, "follow") == "confirm" && strings.HasPrefix(text(*r, "source"), "routine:") {
		r.Fields["at"] = m.d.Clock.Now().In(m.config.Location()).Add(time.Minute).Format(time.RFC3339Nano)
		return nil
	}
	at, err := m.defaultAt(ctx, *r, false)
	if err != nil {
		return err
	}
	r.Fields["at"] = at.Format(time.RFC3339Nano)
	return nil
}
func (m *Module) save(ctx context.Context, r vault.Record, oldAt, oldFollow, by, op string) (Reminder, error) {
	if by == "" {
		return nil, platform.Invalid("by", "請提供操作者")
	}
	vault.UpgradeLegacyGoal(r.Fields)
	if err := m.ensureAt(ctx, &r); err != nil {
		return nil, err
	}
	from := "" // 改之前的狀態，帶在 reminder.changed（notify 用它判斷「離開 asking」，不必自己讀待辦）
	if op != "create" {
		if prev, err := m.d.Vault.Get(ctx, "reminder", r.ID); err == nil {
			from = text(prev, "status")
		}
	}
	saved, err := m.d.Vault.Put(ctx, r, by, r.Revision)
	if err != nil {
		return nil, err
	}
	out, err := m.view(saved)
	if err != nil {
		return nil, err
	}
	if !ended(string(out.Status)) && out.Follow != sdk.FollowNone && !out.At.IsZero() && (op == "create" || oldAt != text(saved, "at") || oldFollow == "none") {
		if err = m.d.Scheduler.At(ctx, out.ID, out.At); err != nil {
			return out, err
		}
	}
	status := string(out.Status)
	if op == "done" {
		status = "done"
	}
	m.markRoomWait(out.ID, out.Room, status)
	if status == "done" && platform.IsOwner(by) {
		m.journalDone(ctx, out)
	}
	err = m.d.Bus.Publish(ctx, "reminder.changed", map[string]any{"id": out.ID, "status": status, "from": from, "owner": out.Owner, "room": out.Room, "by": by})
	return out, err
}
func (m *Module) Create(ctx context.Context, in NewReminder) (Reminder, error) {
	if in == nil || in.By == nil || in.By.Name == "" {
		return nil, platform.Invalid("by", "請提供操作者")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	by := in.By.Name
	owner := in.Owner
	if owner == "" {
		owner = by
		if platform.IsOwner(by) {
			owner = "life"
		}
	}
	follow := string(in.Follow)
	if follow == "" {
		follow = "none"
	}
	difficulty := string(in.Difficulty)
	if difficulty == "" {
		difficulty = string(sdk.DifficultyNormal)
	}
	cycle := string(in.Cycle)
	if cycle == "" {
		cycle = "once"
	}
	r := vault.Record{Kind: "reminder", Fields: map[string]any{"content": in.Content, "owner": owner, "follow": follow, "cycle": cycle, "status": "todo", "room": in.Room, "source": in.Source, "check": in.Check, "difficulty": difficulty}, Body: in.Body}
	if !in.At.IsZero() {
		r.Fields["at"] = in.At.In(m.config.Location()).Format(time.RFC3339Nano)
		m.pastIsNotOverdue(r)
	}
	if !allowed("create", "") {
		return nil, platform.Conflict("無法建立待辦")
	}
	attachments, err := attach.Check(m.home, "attachments", in.Attachments)
	if err != nil {
		return nil, err
	}
	attach.Set(&r, attachments, nil)
	if err := m.ensureAt(ctx, &r); err != nil {
		return nil, err
	}
	id, err := m.d.Vault.NextID(ctx, "reminder")
	if err != nil {
		return nil, err
	}
	r.ID = id
	return m.save(ctx, r, "", "", by, "create")
}
func (m *Module) Update(ctx context.Context, id string, p Patch, by string) (Reminder, error) {
	if p == nil {
		return nil, platform.Invalid("patch", "請提供修改內容")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.d.Vault.Get(ctx, "reminder", id)
	if err != nil {
		return nil, err
	}
	if !allowed("update", text(r, "status")) {
		return nil, platform.Conflict("目前狀態不能修改")
	}
	oldAt, oldFollow := text(r, "at"), text(r, "follow")
	// 動過就算回應了：沒回的次數重算；原本已經不推的，時間還沒到就要重新排上（RMD-8）。
	if current, err := decode(r); err == nil && current.Asks > 0 {
		r.Fields["asks"] = 0
		oldAt = ""
	}
	for k, v := range map[string]*string{"content": p.Content, "owner": p.Owner, "room": p.Room, "check": p.Check} {
		if v != nil {
			r.Fields[k] = *v
		}
	}
	if p.Follow != nil {
		r.Fields["follow"] = string(*p.Follow)
	}
	if p.Cycle != nil {
		r.Fields["cycle"] = string(*p.Cycle)
	}
	if p.Difficulty != nil {
		r.Fields["difficulty"] = string(*p.Difficulty)
	}
	if p.At != nil || p.Cycle != nil {
		delete(r.Fields, "cycle_at") // 主人重新排了：之後從新的時間算
		if platform.IsOwner(by) {
			delete(r.Fields, "due_at") // 主人自己重排：過期從新的時間算；秘書改時間跟延後一樣不重算（RMD-26）
		} else if p.At != nil {
			keepDue(r)
		}
	}
	if p.At != nil {
		if p.At.IsZero() {
			delete(r.Fields, "at")
		} else {
			r.Fields["at"] = p.At.In(m.config.Location()).Format(time.RFC3339Nano)
			m.pastIsNotOverdue(r)
		}
	}
	if p.Body != nil {
		r.Body = strings.TrimSpace(*p.Body)
	}
	attachments, err := attach.Check(m.home, "add_attachments", p.AddAttachments)
	if err != nil {
		return nil, err
	}
	attach.Set(&r, attachments, p.RemoveAttachments)
	vault.UpgradeLegacyGoal(r.Fields)
	if p.Goal != nil {
		if *p.Goal == "" {
			delete(r.Fields, "goal")
		} else {
			// 只要 Goal 存在就能掛（不必是 active、不需主人確認）；goal 資料屬於同一塊，經 goal.CanAttach 直接讀（S8-5）。
			if err := goal.CanAttach(ctx, m.d.Vault, *p.Goal); err != nil {
				return nil, err
			}
			r.Fields["goal"] = "[[" + *p.Goal + "]]"
		}
	}
	return m.save(ctx, r, oldAt, oldFollow, by, "update")
}
func advance(at time.Time, cycle string) time.Time {
	switch cycle {
	case "daily":
		return at.AddDate(0, 0, 1)
	case "weekly":
		return at.AddDate(0, 0, 7)
	case "biweekly":
		return at.AddDate(0, 0, 14)
	case "monthly":
		first := time.Date(at.Year(), at.Month()+1, 1, at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), at.Location())
		last := first.AddDate(0, 1, -1).Day()
		day := at.Day()
		if day > last {
			day = last
		}
		return first.AddDate(0, 0, day-1)
	}
	return at
}

// nextAfter 是週期待辦晚於 limit 的下一次（至少往後一次；中間沒做的那幾次不補）。
func nextAfter(at, limit time.Time, cycle string) time.Time {
	next := advance(at, cycle)
	for !next.After(limit) {
		next = advance(next, cycle)
	}
	return next
}

// lastOfDay 是 t 那天的最後一刻：「今天不做」就要跳過今天剩下的那一次。
func lastOfDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, -1, t.Location())
}

// cycleBase 是週期待辦的基準時間：按過晚點之類的，at 只是這一次的時間，下一次要從原本的時間算（RMD-5）。
func (m *Module) cycleBase(r vault.Record, at time.Time) time.Time {
	if base := stamp(r, "cycle_at"); !base.IsZero() {
		return base.In(m.config.Location())
	}
	return at
}

// keepCycle 在只延後這一次之前記下週期基準時間（已經記過就不動；一次性待辦不用）。
func keepCycle(r vault.Record) {
	if text(r, "cycle") == "once" || text(r, "cycle_at") != "" || text(r, "at") == "" {
		return
	}
	r.Fields["cycle_at"] = text(r, "at")
}

// keepDue 在這一次的時間被往後推之前，記下這一次原本該做的時間（RMD-26）：
// 延後、還在做、沒回自動改明天都不會讓過期重算；已經記過就不動。
func keepDue(r vault.Record) {
	if text(r, "due_at") != "" || text(r, "at") == "" {
		return
	}
	r.Fields["due_at"] = text(r, "at")
}

// pastIsNotOverdue：新增或改時間時填了以前的時間（補記忘了的事），過期從寫下去這一刻算，不能往回填刷點（RMD-26，作者 2026-11-12）。
func (m *Module) pastIsNotOverdue(r vault.Record) {
	now := m.d.Clock.Now().In(m.config.Location())
	if text(r, "due_at") == "" && stamp(r, "at").Before(now) {
		r.Fields["due_at"] = now.Format(time.RFC3339Nano)
	}
}

// tomorrowSameClock 是明天、跟 at 同一個時分的時間（at 可能已經逾期好幾天，所以從 now 算）。
func tomorrowSameClock(at, now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day()+1, at.Hour(), at.Minute(), at.Second(), at.Nanosecond(), now.Location())
}

// notToday 算「今天先不做」之後下一次該提的時間（主人按「今天不做」與沒回都用它，RMD-5、RMD-8）。
func (m *Module) notToday(ctx context.Context, r vault.Record, at, now time.Time) (time.Time, error) {
	if at.IsZero() {
		return m.defaultAt(ctx, r, true)
	}
	if cycle := text(r, "cycle"); cycle != "once" {
		base, end := m.cycleBase(r, at), lastOfDay(now)
		delete(r.Fields, "cycle_at")
		delete(r.Fields, "due_at") // 這一次跳過了，下一次從頭算過期
		if base.After(end) {       // 下一次本來就不在今天
			return base, nil
		}
		return nextAfter(base, end, cycle), nil
	}
	return tomorrowSameClock(at, now), nil
}

// collects：只有「問我做了沒」「秘書幫我確認」的待辦會集點（RMD-26，DEV-149 作者 2026-11-12 定）。
func collects(follow string) bool {
	return follow == string(sdk.FollowConfirm) || follow == string(sdk.FollowCheck)
}

// difficultyPoints：難度換成點數（RMD-24）；沒寫難度算普通。
func difficultyPoints(difficulty string) int {
	switch sdk.Difficulty(difficulty) {
	case sdk.DifficultyEasy:
		return 1
	case sdk.DifficultyHard:
		return 3
	}
	return 2
}

// overdueWeeks：這一次過期滿幾週（從 due_at 算，沒有就從 at）；沒過期是 0（RMD-26）。
func overdueWeeks(r vault.Record, now time.Time) int {
	due := stamp(r, "due_at")
	if due.IsZero() {
		due = stamp(r, "at")
	}
	if due.IsZero() || !now.After(due) {
		return 0
	}
	return int(now.Sub(due) / (7 * 24 * time.Hour))
}

// donePoints：現在做完這一筆值幾點＝難度點數＋過期每滿一週 1 點；不集點的跟進方式是 0（RMD-24、RMD-26）。
func donePoints(r vault.Record, now time.Time) int {
	if !collects(text(r, "follow")) {
		return 0
	}
	return difficultyPoints(text(r, "difficulty")) + overdueWeeks(r, now)
}

// givenToday：這一筆今天已經給過點（同一筆同一天只給一次）。
func givenToday(r vault.Record, now time.Time) bool {
	last := stamp(r, "done_at")
	return !last.IsZero() && last.In(now.Location()).Format(time.DateOnly) == now.Format(time.DateOnly)
}

// pointsNow：看板標籤用，現在按「做了」會拿幾點；已經結束或今天給過是 0（RMD-26）。
func pointsNow(r vault.Record, now time.Time) int {
	if ended(text(r, "status")) || givenToday(r, now) {
		return 0
	}
	return donePoints(r, now)
}

// award 記一次完成的點數（RMD-24）。同一筆同一天只給一次：週期待辦完成後會回到 todo，連按不能多拿。
func award(r *vault.Record, now time.Time) {
	if !givenToday(*r, now) {
		n, _ := r.Fields["done_points"].(int)
		if v, ok := r.Fields["done_points"].(int64); ok {
			n = int(v)
		}
		if v, ok := r.Fields["done_points"].(float64); ok {
			n = int(v)
		}
		r.Fields["done_points"] = n + donePoints(*r, now)
	}
	r.Fields["done_at"] = now.Format(time.RFC3339Nano)
}

func (m *Module) change(ctx context.Context, id, by, op, until string) (Reminder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.d.Vault.Get(ctx, "reminder", id)
	if err != nil {
		return nil, err
	}
	if !allowed(op, text(r, "status")) {
		return nil, platform.Conflict("目前狀態不允許這項操作")
	}
	oldAt, oldFollow := text(r, "at"), text(r, "follow")
	at := stamp(r, "at").In(m.config.Location())
	now := m.d.Clock.Now().In(m.config.Location())
	switch op {
	case "cancel":
		r.Fields["status"] = "cancelled"
		r.Fields["closed_at"] = now.Format(time.RFC3339Nano)
	case "done":
		award(&r, now)
		if text(r, "cycle") == "once" {
			r.Fields["status"] = "done"
			r.Fields["closed_at"] = now.Format(time.RFC3339Nano)
		} else {
			if at.IsZero() {
				return nil, platform.Invalid("at", "週期待辦缺少時間")
			}
			r.Fields["at"] = nextAfter(m.cycleBase(r, at), now, text(r, "cycle")).Format(time.RFC3339Nano)
			delete(r.Fields, "cycle_at")
			delete(r.Fields, "due_at")
			r.Fields["status"] = "todo"
			r.Fields["asks"] = 0
			delete(r.Fields, "closed_at")
		}
	case "doing":
		r.Fields["asks"] = 0 // 主人回了：沒回的次數重算（RMD-8）
		keepCycle(r)
		keepDue(r)
		if at.IsZero() {
			at, err = m.defaultAt(ctx, r, true)
		} else {
			at = at.Add(24 * time.Hour)
		}
		if err != nil {
			return nil, err
		}
		r.Fields["at"] = at.Format(time.RFC3339Nano)
		r.Fields["status"] = "doing"
	case "snooze":
		r.Fields["asks"] = 0 // 主人回了：沒回的次數重算（RMD-8）
		if until != SnoozeNotToday {
			keepCycle(r)
		}
		keepDue(r)
		switch until {
		case SnoozeLater:
			at = m.quiet(now).Defer(now.Add(laterAfter))
		case SnoozeNotToday:
			at, err = m.notToday(ctx, r, at, now)
		case "tomorrow":
			at, err = m.defaultAt(ctx, r, true)
		case "tomorrow-same":
			if at.IsZero() {
				at, err = m.defaultAt(ctx, r, true)
			} else {
				at = at.Add(24 * time.Hour)
			}
		default:
			if duration, e := time.ParseDuration(until); e == nil {
				at = now.Add(duration)
			} else {
				at, err = time.Parse(time.RFC3339, until)
			}
		}
		if err != nil {
			return nil, platform.Invalid("until", "請提供有效的延後時間")
		}
		r.Fields["at"] = at.In(m.config.Location()).Format(time.RFC3339Nano)
		r.Fields["status"] = "todo"
	}
	return m.save(ctx, r, oldAt, oldFollow, by, op)
}
func (m *Module) Snooze(ctx context.Context, id, until, by string) (Reminder, error) {
	return m.change(ctx, id, by, "snooze", until)
}
func (m *Module) Done(ctx context.Context, id, by string) (Reminder, error) {
	return m.change(ctx, id, by, "done", "")
}
func (m *Module) Doing(ctx context.Context, id, by string) (Reminder, error) {
	return m.change(ctx, id, by, "doing", "")
}
func (m *Module) Cancel(ctx context.Context, id, by string) (Reminder, error) {
	return m.change(ctx, id, by, "cancel", "")
}

func (m *Module) List(ctx context.Context, q Query) ([]Reminder, error) {
	if q == nil {
		q = &sdk.RemindersListRequest{}
	}
	rows, err := m.d.Vault.List(ctx, "reminder", vault.Query{IncludeArchived: q.IncludeArchived})
	if err != nil {
		return nil, err
	}
	now := m.d.Clock.Now().In(m.config.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	week := today.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	out := []Reminder{}
	for _, r := range rows {
		v, err := m.view(r)
		if err != nil {
			return nil, err
		}
		if q.Owner != "" && v.Owner != q.Owner {
			continue
		}
		if len(q.Statuses) > 0 {
			found := false
			for _, s := range q.Statuses {
				if v.Status == s {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		active := !ended(string(v.Status))
		has := !v.At.IsZero()
		match := true
		switch q.Query {
		case "":
		case sdk.ReminderQueryToday:
			match = active && has && !v.At.Before(today) && v.At.Before(today.AddDate(0, 0, 1))
		case sdk.ReminderQueryOverdue:
			match = active && has && v.At.Before(now)
		case sdk.ReminderQueryThisWeek:
			match = active && has && !v.At.Before(week) && v.At.Before(week.AddDate(0, 0, 7))
		case sdk.ReminderQueryWaiting:
			match = v.Status == sdk.ReminderStatusAsking
		case sdk.ReminderQueryNoTime:
			match = active && !has
		case sdk.ReminderQueryDoneYesterday:
			match = v.Status == sdk.ReminderStatusDone && !v.ClosedAt.Before(today.AddDate(0, 0, -1)) && v.ClosedAt.Before(today)
		case sdk.ReminderQueryUpcoming:
			match = active && has && !v.At.Before(today.AddDate(0, 0, 1))
		default:
			return nil, platform.Invalid("query", "查詢種類不正確")
		}
		if match {
			out = append(out, v)
		}
	}
	if q.Query == sdk.ReminderQueryUpcoming {
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	}
	if q.Page != nil {
		start := 0
		if q.Page.Cursor != "" {
			start, err = strconv.Atoi(q.Page.Cursor)
			if err != nil || start < 0 {
				return nil, platform.Invalid("cursor", "分頁位置不正確")
			}
		}
		if q.Page.Limit < 0 {
			return nil, platform.Invalid("limit", "筆數不可小於零")
		}
		if start >= len(out) {
			return []Reminder{}, nil
		}
		out = out[start:]
		if q.Page.Limit > 0 && int(q.Page.Limit) < len(out) {
			out = out[:q.Page.Limit]
		}
	}
	return out, nil
}

// journalDone：主人完成待辦，在負責秘書今天的日誌記一行「主人完成：<內容>（R-n）」，同一天同一則只記一次（RMD-11）。
// 2026-11-01 從名冊搬來（以前名冊聽 reminder.changed 自己讀待辦）；寫不進去只記 log，不影響完成。
func (m *Module) journalDone(ctx context.Context, r Reminder) {
	if m.d.Journal == nil || r.Owner == "" {
		return
	}
	if err := m.d.Journal.AppendJournalOnce(ctx, r.Owner, "主人完成："+r.Content+"（"+r.ID+"）"); err != nil && m.log != nil {
		m.log.Warn("寫主人完成日誌失敗", "error", err)
	}
}
