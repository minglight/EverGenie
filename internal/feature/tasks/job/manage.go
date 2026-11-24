package job

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 排程管理（Q1，`sdk.SchedulerAPI`，由 http.go 掛到 HTTP）：job 的寫入只走排程模組（API-1）。
// 寫入時就把載入時會出問題的設定擋下來（cron 解析不了、時段格式不對、秘書不存在、內文是空的），回 Invalid 白話訊息；
// 寫進 vault 之後，vault.changed 讓排程服務重新載入，不用重啟。

func (m *Module) jobsReady(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.registered {
		return platform.Unavailable("排程服務還沒啟動")
	}
	return nil
}

// ListJobs 列出排程（id 排序）；agent 不是空的只列那位秘書的。
func (m *Module) ListJobs(ctx context.Context, agent string, includeArchived bool) ([]sdk.Job, error) {
	if err := m.jobsReady(ctx); err != nil {
		return nil, err
	}
	q := vault.Query{IncludeArchived: includeArchived}
	if agent != "" {
		q.Where = map[string]string{"agent": agent}
	}
	records, err := m.d.Vault.List(ctx, "job", q)
	if err != nil {
		return nil, err
	}
	out := make([]sdk.Job, 0, len(records))
	now := m.d.Clock.Now()
	for _, r := range records {
		j := toJob(r)
		m.lastRun(j, now)
		if !r.Archived && j.Enabled {
			if at, ok := m.d.Timer.Next(r.ID); ok {
				j.NextRun = &at
			}
		}
		out = append(out, *j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SaveJob 新增一個排程。同一個 id 已經有了（含已移除、封存的）回 Conflict；source 沒寫算 user。
func (m *Module) SaveJob(ctx context.Context, j *sdk.Job, by string) (*sdk.Job, error) {
	if err := m.jobsReady(ctx); err != nil {
		return nil, err
	}
	if j == nil {
		return nil, platform.Invalid("job", "請填寫排程")
	}
	id := strings.TrimSpace(j.ID)
	agent, local, scoped := strings.Cut(id, "/")
	if !scoped {
		agent, local = "", id
	}
	if local == "" || strings.Contains(local, "/") || (scoped && agent == "") {
		return nil, platform.Invalid("id", "排程 id 要寫成 <職位>/<id>，例如 code/nightly")
	}
	if j.Agent != "" && j.Agent != agent {
		return nil, platform.Invalid("agent", "排程 id 開頭的職位要跟 agent 一樣")
	}
	if old, err := m.d.Vault.Get(ctx, "job", id); err == nil {
		if old.Archived {
			return nil, platform.Conflict("這個 id 的排程移除過了（封存著），請換一個 id")
		}
		return nil, platform.Conflict("已經有這個排程，要改請用 jobs set")
	} else if platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return nil, err
	}
	name := strings.TrimSpace(j.Name)
	if name == "" {
		name = local
	}
	source := j.Source
	if source == "" {
		source = "user"
	}
	fields := map[string]any{"name": name, "enabled": j.Enabled, "source": source}
	if agent != "" {
		fields["agent"] = agent
	}
	cron, windows := trimList(j.Cron), trimList(j.Windows)
	switch {
	case len(cron) > 0 && len(windows) > 0:
		return nil, platform.Invalid("cron", "cron 跟時段只能選一種")
	case len(cron) > 0:
		fields["cron"] = cron
	case len(windows) > 0:
		fields["windows"] = windows
		if we := trimList(j.WeekendWindows); len(we) > 0 {
			fields["weekend_windows"] = we
		}
		perDay := j.PerDay
		if perDay == 0 {
			perDay = 1
		}
		fields["per_day"] = perDay
		if j.IdleMin != 0 {
			fields["idle_min"] = j.IdleMin
		}
	default:
		return nil, platform.Invalid("cron", "請給排程時間（cron）或時段（windows）")
	}
	for k, v := range map[string]string{"catchup": j.Catchup, "action": j.Action} {
		if v != "" {
			fields[k] = v
		}
	}

	if j.Silent {
		fields["silent"] = true
	}
	r := vault.Record{Kind: "job", ID: id, Fields: fields, Body: j.Body}
	if err := m.checkJob(ctx, r); err != nil {
		return nil, err
	}
	saved, err := m.d.Vault.Put(ctx, r, by, "")
	if err != nil {
		return nil, err
	}
	return toJob(saved), nil
}

// SetJob 改開關、cron、名稱、內文（沒給的不動；名稱與內文給了不能是空的）。改完還開著的排程要能載入，不然回 Invalid；關掉一定可以。
func (m *Module) SetJob(ctx context.Context, req *sdk.SchedulerSetJobRequest, by string) (*sdk.Job, error) {
	if err := m.jobsReady(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, platform.Invalid("id", "請指定排程")
	}
	r, err := m.liveJob(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	set := map[string]any{}
	if req.Enabled != nil {
		set["enabled"] = *req.Enabled
	}
	if cron := trimList(req.Cron); len(cron) > 0 {
		if r.Fields["windows"] != nil {
			return nil, platform.Invalid("cron", "這是時段型排程，沒有 cron 可以改")
		}
		set["cron"] = cron
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, platform.Invalid("name", "名稱不能是空的")
		}
		set["name"] = name
	}
	if req.Body != nil && strings.TrimSpace(*req.Body) == "" {
		return nil, platform.Invalid("body", "請寫指令內文（要秘書做什麼）")
	}
	if len(set) == 0 && req.Body == nil {
		return nil, platform.Invalid("id", "請至少改一項：enabled、cron、name 或 body")
	}
	next := r
	if req.Body != nil {
		next.Body = *req.Body
	}
	next.Fields = map[string]any{}
	for k, v := range r.Fields {
		next.Fields[k] = v
	}
	for k, v := range set {
		next.Fields[k] = v
	}
	if next.Fields["enabled"] != false {
		if err := m.checkJob(ctx, next); err != nil {
			return nil, err
		}
	}
	// vault 的 Patch 只改欄位、不碰內文；名稱與內文一起改，所以整筆用 Put 寫回（帶讀到的版本，中間被改過回 Conflict）。
	saved, err := m.d.Vault.Put(ctx, next, by, r.Revision)
	if err != nil {
		return nil, err
	}
	return toJob(saved), nil
}

// RemoveJob：自己加的（source: user）先停用、記 removed_at，再封存（不刪除）；遷移或套件帶來的只停用，之後升級也不會被重新打開。
func (m *Module) RemoveJob(ctx context.Context, id, by string) (*sdk.Job, bool, error) {
	if err := m.jobsReady(ctx); err != nil {
		return nil, false, err
	}
	r, err := m.liveJob(ctx, id)
	if err != nil {
		return nil, false, err
	}
	set := map[string]any{"enabled": false}
	user := text(r, "source") == "user"
	if user {
		set["removed_at"] = m.d.Clock.Now().Format(time.RFC3339Nano)
	}
	saved, err := m.d.Vault.Patch(ctx, "job", r.ID, set, nil, by)
	if err != nil {
		return nil, false, err
	}
	if !user {
		return toJob(saved), false, nil
	}
	if err := m.d.Vault.Archive(ctx, "job", r.ID, by); err != nil {
		return nil, false, err
	}
	saved.Archived = true
	return toJob(saved), true, nil
}

// RunJob 立刻跑一次：發 schedule.fire（跟時間到了一樣經派送器送給秘書）；沒做完一樣由封包的結局通知（envelopeDone）。
// 系統級（沒有 agent）的回 Invalid；停用中的也可以手動跑；不算進每日上限。
func (m *Module) RunJob(ctx context.Context, id, by string) (*sdk.Job, time.Time, error) {
	if err := m.jobsReady(ctx); err != nil {
		return nil, time.Time{}, err
	}
	r, err := m.liveJob(ctx, id)
	if err != nil {
		return nil, time.Time{}, err
	}
	agent := text(r, "agent")
	if agent == "" {
		return nil, time.Time{}, platform.Invalid("id", "系統級排程不能手動跑，只有秘書的排程可以")
	}
	action := text(r, "action")
	if action == "" {
		action = "none"
	}
	now := m.d.Clock.Now()
	m.mu.Lock()
	log := m.log
	m.mu.Unlock()
	if err := m.d.Bus.Publish(ctx, "schedule.fire", map[string]any{"id": r.ID, "agent": agent, "kind": "job", "at": now, "action": action}); err != nil {
		return nil, time.Time{}, err
	}
	m.deliverAndNote(ctx, r.ID, agent, now)
	if log != nil {
		log.Info("手動執行排程", "id", r.ID, "by", by)
	}
	return toJob(r), now, nil
}

// liveJob 讀一個還沒移除的排程。
func (m *Module) liveJob(ctx context.Context, id string) (vault.Record, error) {
	if strings.TrimSpace(id) == "" {
		return vault.Record{}, platform.Invalid("id", "請指定排程")
	}
	r, err := m.d.Vault.Get(ctx, "job", id)
	if platform.CodeOf(err) == sdk.ErrorCodeNotFound {
		return vault.Record{}, platform.NotFound("找不到這個排程")
	}
	if err != nil {
		return vault.Record{}, err
	}
	if r.Archived {
		return vault.Record{}, platform.NotFound("這個排程已經移除了")
	}
	return r, nil
}

// checkJob 檢查一筆排程能不能被排程服務載入（跟 reload 會略過的情況一致），不行就回 Invalid 白話訊息。
func (m *Module) checkJob(ctx context.Context, r vault.Record) error {
	if strings.TrimSpace(r.Body) == "" {
		return platform.Invalid("body", "請寫指令內文（要秘書做什麼）")
	}
	if agent := text(r, "agent"); agent != "" {
		if _, err := m.d.Vault.Get(ctx, "agent", agent); platform.CodeOf(err) == sdk.ErrorCodeNotFound {
			return platform.Invalid("agent", "找不到秘書 "+agent)
		} else if err != nil {
			return err
		}
	}
	if r.Fields["windows"] != nil {
		wd, ok1 := stringList(r.Fields["windows"])
		we, ok2 := stringList(r.Fields["weekend_windows"])
		if !ok1 || !ok2 || len(wd) == 0 {
			return platform.Invalid("windows", "請填寫時段，寫成 HH:MM-HH:MM")
		}
		if err := timer.ValidWindows(append(append([]string{}, wd...), we...)); err != nil {
			return err
		}
		if intField(r.Fields["per_day"], 1) < 1 {
			return platform.Invalid("per_day", "一天至少一次")
		}
		if intField(r.Fields["idle_min"], 0) < 0 {
			return platform.Invalid("idle_min", "閒置分鐘數不能小於零")
		}
		return nil
	}
	specs, ok := stringList(r.Fields["cron"])
	if !ok || len(specs) == 0 {
		return platform.Invalid("cron", "請給排程時間（五欄位 cron）")
	}
	if _, err := timer.ParseCron(specs, m.d.Timer.Location()); err != nil {
		return platform.Invalid("cron", "排程格式不正確，請使用五欄位格式，例如 \"0 9 * * 1-5\"")
	}
	return nil
}

func trimList(in []string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func toJob(r vault.Record) *sdk.Job {
	list := func(k string) []string { v, _ := stringList(r.Fields[k]); return v }
	j := &sdk.Job{
		ID: r.ID, Agent: text(r, "agent"), Name: text(r, "name"),
		Cron: list("cron"), Windows: list("windows"), WeekendWindows: list("weekend_windows"),
		PerDay: intField(r.Fields["per_day"], 0), IdleMin: intField(r.Fields["idle_min"], 0),
		Enabled: r.Fields["enabled"] != false, Catchup: text(r, "catchup"), Action: text(r, "action"),
		Silent: r.Fields["silent"] == true, Source: text(r, "source"),
		Body: r.Body, Archived: r.Archived, By: text(r, "by"), UpdatedAt: r.UpdatedAt,
	}
	if at, err := time.Parse(time.RFC3339Nano, text(r, "created_at")); err == nil {
		j.CreatedAt = at
	}
	return j
}

// ScheduledOn 回報這位秘書在 day 那一天（排程服務的時區）有沒有排程要跑（P7-4）：
// 有啟用、沒封存的工作，cron 在那天至少觸發一次，或是時段型（每天都會排）；舊的 sites 欄位忽略（法律第 7 條，排程都在本家跑）。
// session 用它判斷「當天有排程的隨選秘書當天視為常駐」。讀不到資料就回 false（不因此多開 session）。
func (m *Module) ScheduledOn(ctx context.Context, agent string, day time.Time) bool {
	loc := m.d.Timer.Location()
	records, err := m.d.Vault.List(ctx, "job", vault.Query{Where: map[string]string{"agent": agent}})
	if err != nil {
		return false
	}
	d := day.In(loc)
	start := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	for _, r := range records {
		if r.Archived || r.Fields["enabled"] == false {
			continue
		}

		if r.Fields["windows"] != nil {
			return true
		}
		specs, ok := stringList(r.Fields["cron"])
		if !ok || len(specs) == 0 {
			continue
		}
		runs, err := timer.NextRuns(specs, loc, start.Add(-time.Nanosecond), 1)
		if err == nil && len(runs) > 0 && runs[0].Before(end) {
			return true
		}
	}
	return false
}

// 秘書封存、改名時名下的排程（業務模組 agents 經自己 Deps 的小 interface 呼叫；job 的擁有者是 jobs）。
// 排程的 id 是「<秘書>/<名稱>」：照 id 前綴認是誰名下的，跟 agent 欄位無關。不看排程服務起來了沒，跟名冊的其他資料一樣直接讀寫。

// AgentJobIDs 列出這位秘書名下（id 前綴 <秘書>/）還沒封存的排程 id。
func (m *Module) AgentJobIDs(ctx context.Context, agent string) ([]string, error) {
	rows, err := m.d.Vault.List(ctx, "job", vault.Query{})
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if strings.HasPrefix(r.ID, agent+"/") {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

// ArchiveAgentJob 立刻封存一個排程（搬進 archive/，不刪除、不改欄位）；已封存的不再動。
func (m *Module) ArchiveAgentJob(ctx context.Context, id, by string) error {
	return m.d.Vault.ArchiveNow(ctx, "job", id, by)
}

// MoveAgentJob 把排程改成新的 id；有 agent 欄位的一起改成 agent。內文與其他欄位不動。
func (m *Module) MoveAgentJob(ctx context.Context, from, to, agent, by string) error {
	r, err := m.d.Vault.Get(ctx, "job", from)
	if err != nil {
		return err
	}
	set := map[string]any{}
	if _, has := r.Fields["agent"]; has {
		set["agent"] = agent
	}
	_, err = m.d.Vault.Move(ctx, "job", from, to, set, by)
	return err
}
