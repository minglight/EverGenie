package goal

import (
	"context"
	"slices"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// criteriaHint 是沒寫驗收條件時的提示（法律第 4 條：Goal 一定要有驗收條件，2026-11-01 作者）。
const criteriaHint = "請寫驗收條件：怎樣算達成（能用指令或畫面確認）"

func (m *Module) Propose(ctx context.Context, s ProposeSpec) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.Text) == "" {
		return nil, platform.Invalid("text", "請填寫主人的原話")
	}
	if strings.TrimSpace(s.Title) == "" {
		return nil, platform.Invalid("title", "請填寫 Goal 標題")
	}
	if strings.TrimSpace(s.Criteria) == "" {
		return nil, platform.Invalid("criteria", criteriaHint)
	}
	if s.Agent == "" {
		s.Agent = s.By
	}
	if s.Agent == "" {
		return nil, platform.Invalid("agent", "請指定負責的秘書")
	}
	if s.By == "" {
		s.By = s.Agent
	}
	id, err := m.d.Vault.NextID(ctx, "goal")
	if err != nil {
		return nil, err
	}
	g := &sdk.Goal{ID: id, Title: s.Title, Text: s.Text, Criteria: strings.TrimSpace(s.Criteria), Agent: s.Agent, State: sdk.GoalStateDraft, PlanbookLog: []sdk.PlanbookEntry{}, CreatedAt: m.d.Clock.Now().In(m.loc)}
	// The schema requires a real notice link even for a draft. The module lock
	// keeps its answer handler from racing the initial Goal write.
	n, err := m.d.Notify.Send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認主人的 Goal：" + s.Title, Context: "「" + s.Text + "」\n驗收條件：" + strings.TrimSpace(s.Criteria), Todo: "請確認主人的 Goal", Options: []string{"確認", "不要"}, Agent: s.Agent, By: s.Agent, Source: sdk.NotifySourceGoals})
	if err != nil {
		return nil, err
	}
	if n == nil || n.ID == "" {
		return nil, platform.Internal("確認通知沒有識別碼")
	}
	g.Notice = n.ID
	g, err = m.save(ctx, g, vault.Record{}, s.By)
	if err != nil {
		return nil, err
	}
	m.publish(ctx, g, s.By)
	return g, nil
}

// Create 是主人自己在看板開 Goal（D-39）：原文是主人親手打的，所以直接 active，不發確認通知。
// 只有主人（by 是 owner(…)）能呼叫；秘書要用 Propose 提案、讓主人確認（D-23）。
func (m *Module) Create(ctx context.Context, s CreateSpec) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if !platform.IsOwner(s.By) {
		return nil, platform.Forbidden("只有主人能直接開 Goal，秘書請用提案讓主人確認")
	}
	if strings.TrimSpace(s.Text) == "" {
		return nil, platform.Invalid("text", "請填寫主人的原話")
	}
	if strings.TrimSpace(s.Title) == "" {
		return nil, platform.Invalid("title", "請填寫 Goal 標題")
	}
	if strings.TrimSpace(s.Criteria) == "" {
		return nil, platform.Invalid("criteria", criteriaHint)
	}
	agent := strings.TrimSpace(s.Agent)
	if agent == "" {
		agent = s.By
	} else if m.d.Agents != nil {
		id, ok := m.d.Agents.Resolve(ctx, agent)
		if !ok {
			return nil, platform.Invalid("agent", "名冊上沒有這位秘書")
		}
		agent = id
	}
	id, err := m.d.Vault.NextID(ctx, "goal")
	if err != nil {
		return nil, err
	}
	now := m.d.Clock.Now().In(m.loc)
	g := &sdk.Goal{ID: id, Title: s.Title, Text: s.Text, Criteria: strings.TrimSpace(s.Criteria), Agent: agent, State: sdk.GoalStateActive, PlanbookLog: []sdk.PlanbookEntry{}, CreatedAt: now, ConfirmedAt: now, ConfirmedBy: s.By}
	g, err = m.save(ctx, g, vault.Record{}, s.By)
	if err != nil {
		return nil, err
	}
	m.publish(ctx, g, s.By)
	if agent != s.By {
		m.tellAgent(ctx, g)
	}
	return g, nil
}

// tellAgent 把主人指定的 Goal 傳給負責的秘書（GOL-12）：routine 封包、走核心派送（她不在線就排隊、照現有方式叫醒），
// 秘書之間的傳話不掛 Goal（2026-11-01 作者）；這則只是告訴她有這個 Goal。Goal 已經存好了，傳不出去只記 WARN、不讓開 Goal 失敗。
func (m *Module) tellAgent(ctx context.Context, g *sdk.Goal) {
	if m.d.Mail == nil {
		return
	}
	body := "［" + g.ID + "］主人開了一個 Goal，指定你負責：「" + g.Title + "」\n要做到什麼：" + g.Text + "\n" +
		criteriaLine(g) +
		"請自己做到達成（要分頭做就開 subagent，不派單給別的秘書）；先把步驟與進度寫進 Planbook（evergenie goal planbook " + g.ID + "），做完用 goal done 回報。"
	_, err := m.d.Mail.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: g.Agent, Body: body, IdempotencyKey: g.ID + "#assigned"})
	if err != nil && m.log != nil {
		m.log.Warn("Goal 沒傳給負責的秘書", "goal", g.ID, "agent", g.Agent, "error", err)
	}
}

// criteriaLine 是傳話與通知裡的「驗收條件」那一行；舊 Goal 沒有就不寫。
func criteriaLine(g *sdk.Goal) string {
	if g.Criteria == "" {
		return ""
	}
	return "驗收條件：" + g.Criteria + "\n"
}

func (m *Module) Get(ctx context.Context, id string) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	g, _, err := m.load(ctx, id)
	return g, err
}
func (m *Module) list(ctx context.Context, states []sdk.GoalState) ([]Goal, error) {
	for _, state := range states {
		if !slices.Contains([]sdk.GoalState{"draft", "active", "done", "withdrawn"}, state) {
			return nil, platform.Invalid("states", "Goal 狀態不正確")
		}
	}
	rs, err := m.d.Vault.List(ctx, "goal", vault.Query{})
	if err != nil {
		return nil, err
	}
	out := []Goal{}
	for _, r := range rs {
		g, err := decode(r)
		if err != nil {
			return nil, err
		}
		if len(states) == 0 || slices.Contains(states, g.State) {
			out = append(out, g)
		}
	}
	return out, nil
}
func (m *Module) List(ctx context.Context, states []sdk.GoalState) ([]Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	return m.list(ctx, states)
}
func (m *Module) UpdatePlanbook(ctx context.Context, id, body, note, by string) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	g, r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if g.State != sdk.GoalStateActive {
		return nil, platform.Conflict("只能修改進行中 Goal 的 Planbook")
	}
	g.Planbook = body
	g.PlanbookLog = append(g.PlanbookLog, sdk.PlanbookEntry{At: m.d.Clock.Now().In(m.loc), By: by, Note: note})
	g, err = m.save(ctx, g, r, by)
	if err != nil {
		return nil, err
	}
	m.publish(ctx, g, by)
	return g, nil
}
func (m *Module) Done(ctx context.Context, id, summary, by string) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	g, r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if g.State != sdk.GoalStateActive {
		return nil, platform.Conflict("只能完成進行中的 Goal")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, platform.Invalid("summary", "請填寫完成說明")
	}
	g.State = sdk.GoalStateDone
	g.Summary = summary
	g.EndedAt = m.d.Clock.Now().In(m.loc)
	g.EndedBy = by
	g, err = m.save(ctx, g, r, by)
	if err != nil {
		return nil, err
	}
	m.publish(ctx, g, by)
	if _, err := m.d.Notify.Send(ctx, &sdk.Notification{Category: sdk.CategoryDone, Title: g.Title, Context: summary, Agent: by, By: by}); err != nil {
		return g, err
	}
	return g, nil
}
func (m *Module) Withdraw(ctx context.Context, id, by string) (Goal, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	if !platform.IsOwner(by) {
		return nil, platform.Forbidden("只有主人能收回 Goal")
	}
	g, r, err := m.load(ctx, id)
	if err != nil {
		return nil, err
	}
	if g.State == sdk.GoalStateWithdrawn {
		return g, nil
	}
	g.State = sdk.GoalStateWithdrawn
	g.EndedAt = m.d.Clock.Now().In(m.loc)
	g.EndedBy = by
	g, err = m.save(ctx, g, r, by)
	if err != nil {
		return nil, err
	}
	m.publish(ctx, g, by)
	return g, nil
}
