package job

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/sdk"
)

// maintenanceID 是每晚整理的內建定時工作（vault 與 bus 的清道夫）。
const maintenanceID = "system.maintenance"

func (m *Module) publish(ctx context.Context, topic string, payload map[string]any) {
	if m.d.Bus != nil {
		if err := m.d.Bus.Publish(ctx, topic, payload); err != nil {
			m.warn("event publish failed", err)
		}
	}
}

// handle 處理收到的事件：job 資料變了就重新載入；送給秘書的排程封包回覆了就發 schedule.done
// （必要時重開對話），結束在 failed／interrupted 只記 WARN（envelopeDone）。
func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) {
	if ev == nil {
		return
	}
	if ev.Topic == "bus.envelope.state" {
		m.envelopeDone(ctx, ev.Payload) // 不拿 m.mu：要讀封包、叫 session
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.Topic == "vault.changed" && ev.Payload["kind"] == "job" {
		if err := m.reload(ctx, m.d.Clock.Now()); err != nil {
			m.warn("reload jobs failed", err)
		}
	}
}

// dueHandler 是計時迴圈到期之後交給 jobs 的處理（timer.Handler）：發事件、每日額度、每晚整理。
type dueHandler struct{ m *Module }

var _ timer.Handler = dueHandler{}

func (h dueHandler) Missed(ctx context.Context, it *timer.Item, d timer.Due) {
	h.m.publish(ctx, "schedule.missed", map[string]any{"id": it.ID, "at": d.At, "caught_up": d.CaughtUp})
	// 補跑的（d.Fire）接著會觸發，結果由 Fire 記；真的略過的才記「略過」（SCH-16）。
	if it.Kind == jobKind && !d.Fire {
		h.m.noteRun(it.ID, d.At, sdk.JobResultSkipped, missedReason(it.Catchup, it.T.Window() != nil), false)
	}
}

// Allow 先擋每日額度：秘書的 budget.max_runs_per_day（0 或沒設就不限）。
func (h dueHandler) Allow(ctx context.Context, now time.Time, it *timer.Item) bool {
	ok := h.m.budget(ctx, now, it.Agent)
	if !ok && it.Kind == jobKind {
		h.m.noteRun(it.ID, now, sdk.JobResultSkipped, "這位秘書今天的排程次數用完了", false)
	}
	return ok
}

func (h dueHandler) Fire(ctx context.Context, now time.Time, it *timer.Item, d timer.Due) {
	m := h.m
	payload := map[string]any{"id": it.ID, "agent": it.Agent, "kind": it.Kind, "at": d.At}
	if win := it.T.Window(); win != nil {
		payload["window_end"] = d.WindowEnd
		payload["idle_min"] = win.IdleMin
	}
	if it.Kind == jobKind {
		data, _ := it.Data.(jobData)
		payload["action"] = data.action
		payload["body"] = data.body
	}
	m.publish(ctx, "schedule.fire", payload)
	if it.Kind == jobKind {
		if it.Agent != "" {
			m.deliverAndNote(ctx, it.ID, it.Agent, d.At)
		} else {
			m.noteRun(it.ID, d.At, sdk.JobResultFired, "", true)
		}
	}
	if it.ID == maintenanceID {
		m.maintenance(ctx, now)
	}
}

// AfterTick 不做事：排程做完沒有只看引擎回報的事實（封包的結局，envelopeDone），不用計時猜「還沒完成」（S5、D7）。
func (h dueHandler) AfterTick(context.Context, time.Time) {}

func (m *Module) budget(ctx context.Context, now time.Time, agent string) bool {
	if agent == "" || m.d.Vault == nil {
		return true
	}
	r, err := m.d.Vault.Get(ctx, "agent", agent)
	if err != nil {
		m.warn("read agent budget failed", err)
		return true
	}
	budget, _ := r.Fields["budget"].(map[string]any)
	max := intField(budget["max_runs_per_day"], 0)
	m.mu.Lock()
	defer m.mu.Unlock()
	if day := now.In(m.d.Timer.Location()).Format("2006-01-02"); day != m.day {
		m.day = day
		m.counts = map[string]int{}
	}
	if max > 0 && m.counts[agent] >= max {
		m.warn("daily job budget reached", nil)
		return false
	}
	m.counts[agent]++
	return true
}

func (m *Module) maintenance(ctx context.Context, now time.Time) {
	if _, err := m.d.Vault.Sweep(ctx, now, true); err != nil {
		m.warn("vault sweep failed", err)
	}
	if _, err := m.d.Bus.Sweep(ctx, now); err != nil {
		m.warn("bus sweep failed", err)
	}
}

// envelopeKey 是排程封包的冪等鍵「job:<id>#<觸發時間>」：同一次觸發只送一次，回覆時也靠它認出是哪個排程。
func envelopeKey(id string, at time.Time) string { return "job:" + id + "#" + at.Format(time.RFC3339) }

// jobOfEnvelope 從封包的冪等鍵認出是哪個排程；不是排程送的回空字串。
func jobOfEnvelope(e *sdk.Envelope) string {
	if e == nil || e.Kind != sdk.EnvelopeKindRoutine || !strings.HasPrefix(e.IdempotencyKey, "job:") {
		return ""
	}
	id, _, _ := strings.Cut(strings.TrimPrefix(e.IdempotencyKey, "job:"), "#")
	return id
}

// deliverAndNote 送一次排程給秘書並記下來（SCH-16）：先記「還在跑」再送，封包很快就結束（派送器在另一條 goroutine）
// 時結局才對得上這一次；送不出去改記沒跑成；同一次觸發已經送過、封包早就結束了（冪等鍵相同，例如同一秒手動跑兩次），直接記那個結局。
func (m *Module) deliverAndNote(ctx context.Context, id, agent string, at time.Time) {
	m.noteRun(id, at, sdk.JobResultRunning, "", true)
	e, err := m.deliver(ctx, id, agent, at)
	if err != nil {
		m.noteOutcome(id, at, sdk.JobResultFailed, err.Error())
		return
	}
	if e != nil {
		m.noteEnvelope(id, at, string(e.Lifecycle), e)
	}
}

// noteEnvelope 照封包的結局記這一次：replied → ok；failed、interrupted → 沒跑成＋原因；還在路上的不動。
func (m *Module) noteEnvelope(id string, at time.Time, state string, e *sdk.Envelope) {
	switch state {
	case string(sdk.LifecycleReplied):
		m.noteOutcome(id, at, sdk.JobResultOk, "")
	case string(sdk.LifecycleFailed), string(sdk.LifecycleInterrupted):
		m.noteOutcome(id, at, sdk.JobResultFailed, unfinishedReason(state, e))
	}
}

// deliver 把秘書的排程封成 routine 封包送給她（REQ-16）：內文讀排程本身（已經刪掉的不送，不用事件裡的舊內文），
// {{slot 名稱}} 用名冊（Deps.Agents）套好，讀不到 addendum 就用預設內容；同一次觸發（id＋時間）只送一次（bus 的冪等鍵）。
// 什麼時候送進對話、2 分鐘送不進常駐改開臨時 session（SES-10）是 session 派送器的事。
// 回送出（或同一次觸發早就送過）的封包；送不出去回白話的錯（給 deliverAndNote 記原因），細節寫 WARN。
func (m *Module) deliver(ctx context.Context, id, agent string, at time.Time) (*sdk.Envelope, error) {
	r, err := m.d.Vault.Get(ctx, "job", id)
	if err != nil {
		m.warn("讀取排程工作失敗", err)
		return nil, errors.New("讀不到排程內容")
	}
	body := r.Body
	if m.d.Agents != nil {
		if body, err = m.d.Agents.RenderJob(ctx, agent, r.Body); err != nil {
			m.warn("套用排程的 slot 失敗，改用預設內容", err)
		}
	}
	e, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, From: "system", To: agent, Body: "［排程 " + id + "］" + body, IdempotencyKey: envelopeKey(id, at)})
	if err != nil {
		m.warn("送出排程給秘書失敗", err)
		return nil, errors.New("送不出去給秘書")
	}
	return e, nil
}

// envelopeDone：排程送給秘書的封包結束了（SCH-4）。
// 回覆了（replied）→ 發 schedule.done；夜間整理（action: rotate）再請 session 重開那個對話清掉 context（SES-9）。封包改送到臨時 session 的，重開的是臨時那個。
// 沒做完（failed：沒送進去；interrupted：做到一半斷了）→ 只記一行 WARN，不發事件、不推給主人：
// 一次沒做完多半是主人按了中斷、額度用完、平台忙，原因已經寫在那位秘書的對話裡；法律第 5 條「自己處理掉的錯誤、
// 自動事件」不傳。沒有人收的事件不留（code 2026-11-02 定）。
func (m *Module) envelopeDone(ctx context.Context, p map[string]any) {
	state, _ := p["state"].(string)
	if p["kind"] != string(sdk.EnvelopeKindRoutine) || state != string(sdk.LifecycleReplied) && state != string(sdk.LifecycleFailed) && state != string(sdk.LifecycleInterrupted) {
		return
	}
	id, _ := p["id"].(string)
	e, err := m.d.Bus.Get(ctx, id)
	if err != nil {
		return
	}
	job := jobOfEnvelope(e)
	if job == "" {
		return
	}
	if at, ok := envelopeTime(e); ok {
		m.noteEnvelope(job, at, state, e)
	}
	if state != string(sdk.LifecycleReplied) {
		if m.log != nil {
			m.log.Warn("排程這次沒做完", "id", job, "agent", e.To, "state", state, "reason", e.Reason)
		}
		return
	}
	m.publish(ctx, "schedule.done", map[string]any{"id": job})
	r, err := m.d.Vault.Get(ctx, "job", job)
	if err != nil {
		m.warn("讀取排程工作失敗", err)
		return
	}
	if text(r, "action") == "rotate" && m.d.Sessions != nil {
		if err := m.d.Sessions.Rotate(ctx, e.To); err != nil {
			m.warn("夜間整理後重開對話失敗", err)
		}
	}
}
