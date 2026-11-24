package task

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

func decode(r vault.Record) (Task, error) {
	fields := make(map[string]any, len(r.Fields))
	for k, v := range r.Fields {
		fields[k] = v
	}
	vault.UpgradeLegacyGoal(fields)
	upgradeLegacy(fields, "")
	b, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	t := &sdk.Task{}
	if err = json.Unmarshal(b, t); err != nil {
		return nil, err
	}
	t.ID = r.ID
	t.Body = r.Body
	t.Archived = r.Archived
	t.UpdatedAt = r.UpdatedAt
	t.Goal = strings.TrimSuffix(strings.TrimPrefix(t.Goal, "[["), "]]")
	return t, nil
}

// load 讀一張單來改：舊狀態先轉成新狀態（upgradeLegacy），寫回去時檔案就是新的。
func (m *Module) load(ctx context.Context, id string) (vault.Record, error) {
	r, err := m.d.Vault.Get(ctx, "task", id)
	if err == nil {
		upgradeLegacy(r.Fields, "")
	}
	return r, err
}

// inputStatus 把呼叫端給的舊值對到新狀態：failed（無法完成）→ cancelled（不做了）。review、shelved 不收（allow 擋）。
func inputStatus(s string) string {
	if s == "failed" {
		return "cancelled"
	}
	return s
}

// queryStatuses 讓還在用舊值查的呼叫端照樣查得到：review → waiting_user、failed／shelved → cancelled。
func queryStatuses(in []sdk.TaskStatus) []sdk.TaskStatus {
	out := make([]sdk.TaskStatus, 0, len(in))
	for _, s := range in {
		switch s {
		case "review":
			s = sdk.TaskStatusWaitingUser
		case "failed", "shelved":
			s = sdk.TaskStatusCancelled
		}
		out = append(out, s)
	}
	return out
}

func (m *Module) Get(ctx context.Context, id string) (Task, error) {
	r, err := m.d.Vault.Get(ctx, "task", id)
	if err != nil {
		return nil, err
	}
	return decode(r)
}

// goal 把任務掛到 Goal（REQ-35）：只要 Goal 存在就行（不必是 active），不需要主人確認；
// 空字串表示拿掉。goal 資料屬於同一塊，經 goal.CanAttach 直接讀（S8-5）。
func (m *Module) goal(ctx context.Context, r *vault.Record, id string) error {
	vault.UpgradeLegacyGoal(r.Fields)
	if id == "" {
		delete(r.Fields, "goal")
		return nil
	}
	if err := goal.CanAttach(ctx, m.d.Vault, id); err != nil {
		return err
	}
	r.Fields["goal"] = "[[" + id + "]]"
	return nil
}
func (m *Module) save(ctx context.Context, r vault.Record, from, by, op string) (Task, error) {
	vault.UpgradeLegacyGoal(r.Fields)
	now := m.d.Clock.Now().In(m.loc).Format(time.RFC3339Nano)
	to := text(r, "status")
	// 進入等你：狀態從別的變過來，或送驗收（已經在等你、再交付一次也重新算）。
	entering := waiting(to) && (to != from || op == "submit")
	if entering {
		r.Fields["waiting_since"] = now
		r.Fields["nudges"] = 0
		delete(r.Fields, "last_nudged_at")
	}
	if !waiting(to) {
		delete(r.Fields, "waiting_since")
		delete(r.Fields, "last_nudged_at")
		r.Fields["nudges"] = 0
		if text(r, "ask") == reviewAsk { // 交付等驗收補的那句，離開等你就不用了
			delete(r.Fields, "ask")
		}
	}
	// proposed 只在「交付等驗收」時有意義：不是送驗收而進入等你（要主人決定），或回到沒結束的狀態，就拿掉。
	if op != "submit" && to == "waiting_user" && from != to || !waiting(to) && !ended(to) {
		delete(r.Fields, "proposed")
	}
	if ended(to) {
		r.Fields["closed_at"] = now
		if text(r, "type") == "task" {
			r.Fields["accepted_by"] = by
			r.Fields["accepted_at"] = now
		}
	}
	if op == "submit" {
		delete(r.Fields, "review_note")
	}
	saved, err := m.d.Vault.Put(ctx, r, by, r.Revision)
	if err != nil {
		return nil, err
	}
	t, err := decode(saved)
	if err != nil {
		return nil, err
	}
	if err = m.d.Bus.Publish(ctx, "task.changed", map[string]any{"id": t.ID, "status": string(t.Status), "from": from, "agent": t.Agent, "by": by}); err != nil {
		return t, err
	}
	topic := ""
	p := map[string]any{"id": t.ID, "agent": t.Agent}
	if to == "waiting_user" && from != to {
		topic = "task.waiting_user"
		p["ask"] = t.Ask
	}
	if op == "submit" { // 交付等驗收：事件名照舊（訂閱的人不用改）
		topic = "task.review"
		p["proposed"] = string(t.Proposed)
	}
	if op == "reject" {
		topic = "task.rejected"
	}
	if topic != "" {
		err = m.d.Bus.Publish(ctx, topic, p)
	}
	if err == nil && entering {
		_, err = m.sendWaiting(ctx, t)
	}
	return t, err
}
func (m *Module) Create(ctx context.Context, in NewTask) (Task, error) {
	if in == nil || in.By == nil {
		return nil, platform.Invalid("by", "請提供操作者")
	}
	by := in.By.Name
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kind := string(in.Type)
	if kind == "" {
		kind = "task"
	}
	status := inputStatus(string(in.Status))
	if status == "" {
		status = "doing"
	}
	if err = m.allow(role, kind, "", status, "create"); err != nil {
		return nil, err
	}
	agent := in.Agent
	if agent == "" && role == "agent" {
		agent = by
	}
	r := vault.Record{Kind: "task", Fields: map[string]any{"title": in.Title, "type": kind, "status": status, "agent": agent}, Body: in.Body}
	for k, v := range map[string]string{"ask": in.Ask, "result": in.Result, "verify": string(in.Verify), "verify_how": in.VerifyHow} {
		if v != "" {
			r.Fields[k] = v
		}
	}
	criteria := strings.TrimSpace(in.Criteria)
	if criteria == "" && kind == "task" && role == "agent" {
		return nil, platform.Invalid("criteria", "請寫驗收條件：怎樣算完（能用指令或畫面確認）；主人說過的照抄並加 --criteria_from_owner")
	}
	setCriteria(&r, criteria, role == "user" || in.CriteriaFromOwner)
	if err = m.goal(ctx, &r, in.Goal); err != nil {
		return nil, err
	}
	attachments, err := attach.Check(m.home, "attachments", in.Attachments)
	if err != nil {
		return nil, err
	}
	attach.Set(&r, attachments, nil)
	r.ID, err = m.d.Vault.NextID(ctx, "task")
	if err != nil {
		return nil, err
	}
	return m.save(ctx, r, "", by, "create")
}

// setCriteria 寫驗收條件與「是不是主人給的」；空的就拿掉（主人開單沒寫）。
func setCriteria(r *vault.Record, criteria string, owner bool) {
	if criteria == "" {
		delete(r.Fields, "criteria")
		r.Fields["criteria_owner"] = false
		return
	}
	r.Fields["criteria"] = criteria
	r.Fields["criteria_owner"] = owner
}

// ownerFlag：單上的驗收條件是主人給的（主人寫的，或秘書照抄主人的話並標明）。
func ownerFlag(r vault.Record) bool {
	owner, _ := r.Fields["criteria_owner"].(bool)
	return owner && text(r, "criteria") != ""
}

// ownerCriteria：單上有主人的驗收條件，或單掛在主人確認過的 Goal（進行中或已達成；Goal 的驗收條件是主人給的，法律第 1.4 條）。
// 草稿（主人還沒確認）與收回的 Goal 不算，免得秘書自己提一個 Goal 掛上去就能自己結單。
func (m *Module) ownerCriteria(ctx context.Context, r vault.Record) (bool, error) {
	if text(r, "criteria") == "" {
		return false, nil
	}
	if ownerFlag(r) {
		return true, nil
	}
	vault.UpgradeLegacyGoal(r.Fields)
	id := strings.TrimSuffix(strings.TrimPrefix(text(r, "goal"), "[["), "]]")
	if id == "" {
		return false, nil
	}
	g, err := goal.Read(ctx, m.d.Vault, id)
	if err != nil {
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			return false, nil
		}
		return false, err
	}
	return g.ConfirmedBy != "" && (g.State == sdk.GoalStateActive || g.State == sdk.GoalStateDone), nil
}

func addLinks(r *vault.Record, links []string) {
	if len(links) == 0 {
		return
	}
	r.Fields["links"] = append(attach.StringList(*r, "links"), links...)
}
func (m *Module) Update(ctx context.Context, id string, p Patch, by string) (Task, error) {
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, platform.Invalid("patch", "請提供修改內容")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	from := text(r, "status")
	kind := text(r, "type")
	if p.Type != nil {
		if role != "user" {
			return nil, platform.Forbidden("類型建立後只有使用者能改")
		}
		if ended(from) {
			return nil, platform.Conflict("已結束的任務不能改類型")
		}
		kind = string(*p.Type)
		r.Fields["type"] = kind
	}
	to := from
	if p.Status != nil {
		to = inputStatus(string(*p.Status))
	}
	if err = m.allow(role, kind, from, to, "update"); err != nil {
		return nil, err
	}
	r.Fields["status"] = to
	// 標題與需求只有主人能改：驗收條件可能就寫在裡面，秘書改了等於繞過法律第 1.4 條。
	if role != "user" && (p.Title != nil || p.Body != nil) {
		return nil, platform.Forbidden("標題與需求只有主人能改；覺得要改就問主人")
	}
	if p.Title != nil {
		title := strings.TrimSpace(*p.Title)
		if title == "" {
			return nil, platform.Invalid("title", "標題不能空白")
		}
		r.Fields["title"] = title
	}
	if p.Body != nil {
		r.Body = strings.TrimSpace(*p.Body)
	}
	if p.Ask != nil {
		r.Fields["ask"] = *p.Ask
		if to == "waiting_user" { // 改問主人一件事：不再是交付等驗收
			delete(r.Fields, "proposed")
		}
	}
	if p.Result != nil {
		r.Fields["result"] = *p.Result
	}
	if p.Agent != nil {
		// 秘書換負責人（含換成自己）：主人的驗收條件是給原本那位的，降成秘書的，做完等主人結（法律第 1.4 條）。
		if role == "agent" && *p.Agent != text(r, "agent") && ownerFlag(r) {
			r.Fields["criteria_owner"] = false
		}
		r.Fields["agent"] = *p.Agent
	}
	// 工作 session 的記錄（P7-5）：空字串表示拿掉。
	if v := p.EngineSession; v != nil {
		if *v == "" {
			delete(r.Fields, "engine_session")
		} else {
			r.Fields["engine_session"] = *v
		}
	}
	if p.Criteria != nil {
		c := strings.TrimSpace(*p.Criteria)
		if c == "" && kind == "task" && role == "agent" {
			return nil, platform.Invalid("criteria", "驗收條件不能拿掉")
		}
		// 主人給的驗收條件只有主人能改；秘書照原樣再送一次不算改。
		if role == "agent" && ownerFlag(r) {
			if c != text(r, "criteria") {
				return nil, platform.Forbidden("主人給的驗收條件秘書不能改；覺得要改就問主人")
			}
		} else {
			setCriteria(&r, c, role == "user" || p.CriteriaFromOwner)
		}
	} else if p.CriteriaFromOwner {
		return nil, platform.Invalid("criteria", "標成主人的驗收條件時要一起給條件內容（--criteria）")
	}
	vault.UpgradeLegacyGoal(r.Fields)
	if p.Goal != nil {
		if err = m.goal(ctx, &r, *p.Goal); err != nil {
			return nil, err
		}
	}
	addLinks(&r, p.AddLinks)
	attachments, err := attach.Check(m.home, "add_attachments", p.AddAttachments)
	if err != nil {
		return nil, err
	}
	attach.Set(&r, attachments, p.RemoveAttachments)
	return m.save(ctx, r, from, by, "update")
}
func (m *Module) Submit(ctx context.Context, id string, s Submission, by string) (Task, error) {
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	if s == nil {
		return nil, platform.Invalid("submission", "請提供驗收內容")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	from := text(r, "status")
	if err = m.allow(role, text(r, "type"), from, "waiting_user", "submit"); err != nil {
		return nil, err
	}
	if text(r, "criteria") == "" {
		return nil, platform.Invalid("criteria", "請先在單上寫驗收條件再送驗收（evergenie tasks set "+id+" --criteria …）")
	}
	proposed := inputStatus(string(s.Proposed))
	if proposed == "" {
		return nil, platform.Invalid("proposed", "請填寫必要欄位")
	}
	if !oneOf(proposed, "done", "cancelled") {
		return nil, platform.Invalid("proposed", "建議的結局只能是完成（done）或不做了（cancelled）")
	}
	// 交付等驗收＝等你＋proposed（原本的 review，2026-11-01 併進等你）。
	r.Fields["status"] = "waiting_user"
	r.Fields["ask"] = reviewAsk
	r.Fields["result"] = s.Result
	r.Fields["verify"] = string(s.Verify)
	r.Fields["verify_how"] = s.VerifyHow
	r.Fields["proposed"] = proposed
	addLinks(&r, s.AddLinks)
	return m.save(ctx, r, from, by, "submit")
}
func (m *Module) Accept(ctx context.Context, id, by string) (Task, error) {
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	if role != "user" {
		return nil, platform.Forbidden("只有使用者能通過驗收")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !delivered(r) {
		return nil, platform.Conflict("只有待驗收的任務能通過")
	}
	to := text(r, "proposed")
	if err = m.allow(role, text(r, "type"), "waiting_user", to, "accept"); err != nil {
		return nil, err
	}
	r.Fields["status"] = to
	return m.save(ctx, r, "waiting_user", by, "accept")
}

// Archive 是主人在看板的「刪除」：沒結束的單先記成 cancelled（closed_at 照 save），
// 再用 ArchiveNow 搬進 archive；不看保存規則、不刪檔（TSK-18）。找不到或已經封存的略過。
func (m *Module) Archive(ctx context.Context, ids []string, by string) ([]string, error) {
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	if role != "user" {
		return nil, platform.Forbidden("只有使用者能刪任務")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var done []string
	for _, id := range ids {
		r, err := m.load(ctx, id)
		if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			continue
		}
		if err != nil {
			return done, err
		}
		if r.Archived {
			continue
		}
		if from := text(r, "status"); !ended(from) {
			r.Fields["status"] = "cancelled"
			if _, err = m.save(ctx, r, from, by, "archive"); err != nil {
				return done, err
			}
		}
		if err = m.d.Vault.ArchiveNow(ctx, "task", id, by); err != nil {
			return done, err
		}
		done = append(done, id)
	}
	return done, nil
}
func (m *Module) Reject(ctx context.Context, id, note, by string) (Task, error) {
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	if role != "user" {
		return nil, platform.Forbidden("只有使用者能退回驗收")
	}
	if strings.TrimSpace(note) == "" {
		return nil, platform.Invalid("note", "請填寫退回意見")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if !delivered(r) {
		return nil, platform.Conflict("只有待驗收的任務能退回")
	}
	if err = m.allow(role, text(r, "type"), "waiting_user", "doing", "reject"); err != nil {
		return nil, err
	}
	r.Fields["status"] = "doing"
	r.Fields["review_note"] = note
	return m.save(ctx, r, "waiting_user", by, "reject")
}
func (m *Module) Close(ctx context.Context, id string, c Closing, by string) (Task, error) {
	role, err := m.role(ctx, by)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, platform.Invalid("closing", "請提供結單內容")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	from := text(r, "status")
	to := inputStatus(string(c.Status))
	if role == "agent" && text(r, "type") == "task" && to == "done" {
		// 法律第 1.4 條（2026-11-01）：有主人的驗收條件，負責的秘書照條件驗過就自己結。
		owner, err := m.ownerCriteria(ctx, r)
		if err != nil {
			return nil, err
		}
		if err = selfClose(r, c, by, owner); err != nil {
			return nil, err
		}
	} else if err = m.allow(role, text(r, "type"), from, to, "close"); err != nil {
		return nil, err
	}
	if !ended(to) {
		return nil, platform.Conflict("結單必須選擇結束狀態")
	}
	if role == "agent" && to == "cancelled" && strings.TrimSpace(c.Result) == "" {
		return nil, platform.Invalid("result", "不做了要在結果寫原因")
	}
	r.Fields["status"] = to
	for k, v := range map[string]string{"result": c.Result, "verify": string(c.Verify), "verify_how": c.VerifyHow} {
		if v != "" {
			r.Fields[k] = v
		} else {
			delete(r.Fields, k)
		}
	}
	addLinks(&r, c.AddLinks)
	return m.save(ctx, r, from, by, "close")
}

// selfClose 檢查秘書能不能自己結這張一般任務（法律第 1.4 條）：是她負責的、在進行中或等外部（不跳過主人的「等你」與還沒開工的待做）、
// 驗收條件是主人給的、照條件驗過並寫了結果與驗證方式。
func selfClose(r vault.Record, c Closing, by string, owner bool) error {
	if text(r, "agent") != by {
		return platform.Forbidden("只有負責這張單的秘書能自己結單")
	}
	if st := text(r, "status"); st != "doing" && st != "waiting_external" {
		return platform.Conflict("只有進行中或等外部的單能自己結；等你處理的單等主人回，待做的先改成進行中")
	}
	if !owner {
		return platform.Forbidden("這張單沒有主人給的驗收條件，做完請送驗收（Submit），由主人結單")
	}
	if strings.TrimSpace(c.Result) == "" || strings.TrimSpace(c.VerifyHow) == "" {
		return platform.Invalid("result", "自己結單要寫結果（result）與怎麼照驗收條件驗的（verify_how）")
	}
	if c.Verify != sdk.VerifyVerified {
		return platform.Invalid("verify", "照驗收條件驗過（verify=verified）才能自己結；驗不了就送驗收")
	}
	return nil
}

func (m *Module) List(ctx context.Context, q Query) ([]Task, error) {
	if q == nil {
		q = &sdk.TasksListRequest{}
	}
	rows, err := m.d.Vault.List(ctx, "task", vault.Query{IncludeArchived: q.IncludeArchived})
	if err != nil {
		return nil, err
	}
	out := []Task{}
	want := queryStatuses(q.Statuses)
	for _, r := range rows {
		t, err := decode(r)
		if err != nil {
			return nil, err
		}
		if q.Agent != "" && t.Agent != q.Agent || q.Goal != "" && t.Goal != q.Goal || q.IdeasOnly && t.Status != sdk.TaskStatusIdea {
			continue
		}
		if len(want) > 0 {
			found := false
			for _, s := range want {
				if t.Status == s {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		out = append(out, t)
	}
	if q.IdeasOnly {
		sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
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
			return []Task{}, nil
		}
		out = out[start:]
		if q.Page.Limit > 0 && int(q.Page.Limit) < len(out) {
			out = out[:q.Page.Limit]
		}
	}
	return out, nil
}
func (m *Module) old(ctx context.Context, agent string, now time.Time, days time.Duration, states ...sdk.TaskStatus) ([]Task, error) {
	rows, err := m.List(ctx, &sdk.TasksListRequest{Agent: agent, Statuses: states})
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for _, r := range rows {
		if now.Sub(r.UpdatedAt) > days {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt.Before(out[j].UpdatedAt) })
	return out, nil
}
func (m *Module) Stale(ctx context.Context, now time.Time) ([]Task, error) {
	c := m.loadFollow(ctx)
	return m.old(ctx, "", now, time.Duration(c.stale)*24*time.Hour, sdk.TaskStatusDoing, sdk.TaskStatusWaitingExternal)
}
func (m *Module) Reconcile(ctx context.Context, id string, now time.Time) ([]Task, error) {
	return m.old(ctx, id, now, 24*time.Hour, sdk.TaskStatusDoing)
}
func (m *Module) Waiting(ctx context.Context, now time.Time) ([]WaitingTask, error) {
	m.loadFollow(ctx)
	rows, err := m.List(ctx, &sdk.TasksListRequest{Statuses: []sdk.TaskStatus{sdk.TaskStatusWaitingUser}})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].WaitingSince.Before(rows[j].WaitingSince) })
	out := []WaitingTask{}
	for _, t := range rows {
		days := int32(now.Sub(t.WaitingSince) / (24 * time.Hour))
		if days < 0 {
			days = 0
		}
		out = append(out, WaitingTask{Task: t, WaitingDays: days})
	}
	return out, nil
}
func (m *Module) DueNudges(ctx context.Context, now time.Time) ([]Task, error) {
	c := m.loadFollow(ctx)
	rows, err := m.Waiting(ctx, now)
	if err != nil {
		return nil, err
	}
	out := []Task{}
	for _, r := range rows {
		t := r.Task
		at := t.WaitingSince
		if !t.LastNudgedAt.IsZero() {
			at = t.LastNudgedAt
		}
		if int(t.Nudges) < c.max && now.Sub(at) > time.Duration(c.every)*24*time.Hour {
			out = append(out, t)
		}
	}
	return out, nil
}
func (m *Module) MarkNudged(ctx context.Context, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	if !waiting(text(r, "status")) {
		return platform.Conflict("只有等待使用者的任務能記錄跟進")
	}
	t, err := decode(r)
	if err != nil {
		return err
	}
	m.fmu.Lock()
	max := m.nudgeMax
	m.fmu.Unlock()
	if int(t.Nudges) >= max {
		return platform.Conflict("已達跟進次數上限")
	}
	r.Fields["nudges"] = t.Nudges + 1
	r.Fields["last_nudged_at"] = now.In(m.loc).Format(time.RFC3339Nano)
	_, err = m.d.Vault.Put(ctx, r, "system", r.Revision)
	return err
}

// AddNudge 把任務單的 nudges 加 1：工作 session 的「還在等你」提醒推出去（或顯示給主人）時記一次（P7-6、D-34）。
// 跟 MarkNudged（每日跟進）不同：不看跟進上限、不動 last_nudged_at（那是每日跟進的節奏）、單不在等待狀態也照記。
func (m *Module) AddNudge(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, err := m.load(ctx, id)
	if err != nil {
		return err
	}
	t, err := decode(r)
	if err != nil {
		return err
	}
	r.Fields["nudges"] = t.Nudges + 1
	_, err = m.d.Vault.Put(ctx, r, "system", r.Revision)
	return err
}
func (m *Module) Brief(ctx context.Context, id string) (string, error) {
	t, err := m.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return BriefText(t), nil
}

func BriefText(t *sdk.Task) string {
	s := t.ID + "：" + t.Title + "\n" + t.Body + attach.Lines(t.Attachments)
	if t.ReviewNote != "" {
		s += "\n退回意見：" + t.ReviewNote
	}
	return s
}
