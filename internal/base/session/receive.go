package session

import (
	"context"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 送出後 3 秒規則（作者 2026-10-30「最低 3 秒鐘就要更新到最新狀態」，S5 D3 改）：3 秒內看到的是引擎回報的事實
// （她收到了）或排隊的原因（MessageWait）。說不出原因不再判掉包——那只是「還沒有回報」，計時不改狀態（S5-2）。

// isOwnerChat：看板／CLI 上主人說的話（只有這種有 3 秒規則與泡泡）。
func isOwnerChat(e *sdk.Envelope) bool {
	return e != nil && owner(e.From) && e.Kind == sdk.EnvelopeKindChat && e.Route == sdk.EnvelopeRouteCore
}

// messageWait 是主人的這句話（queued／notified）現在說得出的原因；空＝說不出（應該馬上就會收到）。s 是收件的 session（可以是 nil）。
func (m *Module) messageWait(ctx context.Context, e *sdk.Envelope, s *sdk.Session, rows []*sdk.Envelope) sdk.MessageWait {
	if e.Lifecycle != sdk.LifecycleQueued && e.Lifecycle != sdk.LifecycleNotified {
		return ""
	}
	if s == nil {
		if m.wakeable(ctx, e.To) {
			return sdk.MessageWaitWaking // 隨選的秘書還沒開過：派送器正在開（SES-1）
		}
		return ""
	}
	if s.Pending != nil || s.State == sdk.SessionStateWaiting || m.dialogShown(s.Name, m.d.Clock.Now()) {
		return sdk.MessageWaitAnswer // 確認框開著（核心知道的，或引擎貼上時看到的原生確認框，REQ-391）
	}
	switch s.State {
	case sdk.SessionStateLimited:
		// 額度用完（REQ-378）：恢復前排著不算掉包；過了恢復時間 1 秒內就記回待命，照原本的規則（REQ-379）。
		return sdk.MessageWaitLimited
	case sdk.SessionStateStarting:
		return sdk.MessageWaitWaking
	case sdk.SessionStateStopped:
		if s.Name != s.Agent || m.wakeable(ctx, s.Agent) {
			return sdk.MessageWaitWaking // 派送器會把她開回來（SES-40、SES-52）
		}
		return ""
	}
	if e.Lifecycle == sdk.LifecycleQueued && m.retrying(e.ID) {
		return sdk.MessageWaitRetrying // 引擎說上一次沒貼進去，派送器在退避重試同一則（S5-2）
	}
	if s.State == sdk.SessionStateIdle {
		// 她閒著，但同一個對話還有前一句已貼上或收到、還沒結束（例如重試時引擎說「已經在她的對話裡」，額度恢復後
		// 平台自己接著做那一輪，DEV-99）：派送器一次只送一則（active），先不貼這句，原因跟忙著一樣是「等前一句」。
		for _, r := range rows {
			if r.To == e.To && r.ID != e.ID && r.Route == sdk.EnvelopeRouteCore && r.Lifecycle != sdk.LifecycleQueued && !terminal(r.Lifecycle) {
				return sdk.MessageWaitBusy
			}
		}
		return ""
	}
	if s.State != sdk.SessionStateBusy {
		return ""
	}
	if e.Lifecycle == sdk.LifecycleNotified && interjected(e) {
		return sdk.MessageWaitInterject
	}
	for _, r := range rows {
		if r.To == e.To && r.ID != e.ID && r.Route == sdk.EnvelopeRouteCore && r.Lifecycle == sdk.LifecycleNotified {
			return sdk.MessageWaitBusy // 一次只貼一則：等前一句收到（SES-41）
		}
	}
	return ""
}

// wakeable：這位秘書的主要 session 停了的話，派送器（或看板打開聊天頁）會把她開起來（啟用、resident 或 ondemand）。
func (m *Module) wakeable(ctx context.Context, agent string) bool {
	enabled, mode := m.agentMode(ctx, agent)
	return enabled && (mode == "resident" || mode == "ondemand")
}

// NoTargetReason：收件的對話在這台沒有、也叫不起來時，那句話記 failed 的原因（SES-58）。
const NoTargetReason = "這台沒有這個對話"

// cannotWake：name 不是這台叫得起來的秘書——沒有這位秘書（設定讀回 not_found：沒建過、已封存、是從沒開過的專屬對話名稱），
// 或她停用、mode 不是 resident／ondemand。讀設定出別的錯不算（說不準，下一輪再看），所以不直接用 wakeable。
func (m *Module) cannotWake(ctx context.Context, name string) bool {
	r, err := m.d.Vault.Get(ctx, "agent", name)
	if err != nil {
		return platform.CodeOf(err) == sdk.ErrorCodeNotFound
	}
	enabled, _ := r.Fields["enabled"].(bool)
	mode := text(r.Fields, "mode")
	return !enabled || (mode != "resident" && mode != "ondemand")
}

// agentDisabled：這位秘書的設定讀得到而且停用了（not_found 與讀不到不算：房間名稱、說不準的錯照舊）。
func (m *Module) agentDisabled(ctx context.Context, agent string) bool {
	if agent == "" {
		return false
	}
	r, err := m.d.Vault.Get(ctx, "agent", agent)
	if err != nil {
		return false
	}
	enabled, ok := r.Fields["enabled"].(bool)
	return ok && !enabled
}

// failNoTarget：收件的對話不會在這台開起來（SES-58）——這是核心自己知道的事實，不是計時，直接記送不了，
// 不讓它永遠排著。寄件人是秘書時照 SES-20 回音；主人的回答不重送（retryAnswers 看 NoTargetReason）。呼叫時持有 dispatchMu。
func (m *Module) failNoTarget(ctx context.Context, e *sdk.Envelope) {
	if err := m.finish(ctx, e, sdk.LifecycleFailed, NoTargetReason, "", false); err != nil {
		m.warn("記錄送不了失敗", err)
	} else if m.log != nil {
		m.log.Info("收件的對話不會在這台開起來，記成送不了", "session", e.To, "id", e.ID)
	}
}

// retrying：引擎說過這則沒貼進去（pasteRetry 記了次數），還沒成功貼上。
func (m *Module) retrying(id string) bool {
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	return m.tries[id] > 0
}

// holdRetry：這則還在暫時錯誤的退避裡，或收件的畫面開著確認框、還沒到下一次試的時間（REQ-391）。
func (m *Module) holdRetry(e *sdk.Envelope, now time.Time) bool {
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	if at, ok := m.retryAt[e.ID]; ok && now.Before(at) {
		return true
	}
	at, ok := m.dialogAt[e.To]
	return ok && now.Before(at.Add(DialogRetryEvery))
}

// dialogOpen：引擎說她的畫面開著確認框（dialog）。話放回排隊、不算一次沒送到（退避重來）；
// 這段時間泡泡的原因是「她在等你回答上面的問題」（REQ-391）。
func (m *Module) dialogOpen(ctx context.Context, e *sdk.Envelope) {
	now := m.d.Clock.Now()
	m.recvMu.Lock()
	m.dialogAt[e.To] = now
	delete(m.tries, e.ID)
	delete(m.retryAt, e.ID)
	m.recvMu.Unlock()
	if m.log != nil {
		m.log.Info("她的畫面開著確認框，收掉了再送", "session", e.To, "id", e.ID)
	}
	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()
	current, err := m.d.Bus.Get(ctx, e.ID)
	if err == nil && current.Lifecycle == sdk.LifecycleNotified {
		if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: dialogHoldReason}); err != nil {
			m.warn("放回排隊失敗", err)
		}
	}
}

// dialogShown：最近一次貼上時引擎說這個 session 的畫面開著確認框（還沒有成功貼上過）。
func (m *Module) dialogShown(name string, now time.Time) bool {
	m.recvMu.Lock()
	defer m.recvMu.Unlock()
	at, ok := m.dialogAt[name]
	return ok && now.Before(at.Add(3*DialogRetryEvery))
}

// adoptDelivered：重試時引擎說「這句已經在她的對話裡」（sdk.DeliverAlready）——貼上了，只是開始處理的事件還沒到
// （或到不了）。記成收到，並把它當成這一輪的話：晚到的開始事件照中途補話的規則併進下一輪（SES-41），一輪結束時一起記已回覆。
func (m *Module) adoptDelivered(ctx context.Context, e *sdk.Envelope) {
	m.dispatchMu.Lock()
	defer m.dispatchMu.Unlock()
	current, err := m.d.Bus.Get(ctx, e.ID)
	if err != nil || current.Lifecycle != sdk.LifecycleNotified {
		return
	}
	if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleReceived, bus.TransitionOpt{Reason: adoptedReason}); err != nil {
		m.warn("記錄已收到失敗", err)
		return
	}
	if t := m.turns[e.To]; t != nil && t.ended == nil {
		t.carried = append(t.carried, e.ID) // 她正在跑的這一輪（中途補話被收進去）：跟著這一輪結束
		return
	}
	origin := "owner"
	if !owner(e.From) && e.From != "system" {
		origin = "agent"
	}
	t := &dispatchTurn{turn: turn{Origin: origin, From: e.From, StartedAt: m.d.Clock.Now()}, source: sdk.InputSourceTyped, input: e.Body}
	t.Envelope = e.ID
	t.Adopted = true
	m.turns[e.To] = t
	m.storeTurn(e.To, t.turn, false)
	if owner(e.From) {
		m.ownerSaid(e.To, t.StartedAt)
	}
}

// wake：主人打開聊天頁時叫醒她（REQ-386）。只對主要 session；秘書不能叫醒（停用、不在這台、mode none）或已經開著的回現在的樣子。
func (m *Module) wake(ctx context.Context, spec OpenSpec) (Session, error) {
	name := spec.Name
	if name == "" {
		name = spec.Agent
	}
	agent := spec.Agent
	if agent == "" {
		agent = name
	}
	current, err := m.Get(ctx, name)
	if name != agent || !m.wakeable(ctx, agent) || (err == nil && current.State != sdk.SessionStateStopped) {
		return current, err
	}
	if err != nil && platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		return nil, err
	}
	return m.Open(ctx, &sdk.SessionOpenRequest{Name: agent, Agent: agent, Kind: sdk.SessionKindResident})
}

func (m *Module) ReceiveLatency(ctx context.Context) (count, over int, p50, p90 time.Duration, err error) {
	rows, err := m.d.Bus.List(ctx, "")
	if err != nil {
		return 0, 0, 0, 0, err
	}
	now := m.d.Clock.Now()
	loc := time.Local
	if m.config != nil {
		loc = m.config.Location()
	}
	y, mo, d := now.In(loc).Date()
	day := time.Date(y, mo, d, 0, 0, 0, 0, loc)
	var took []time.Duration
	for _, e := range rows {
		if !isOwnerChat(e) || e.CreatedAt.Before(day) {
			continue
		}
		count++
		at, ok := receivedAt(e)
		switch {
		case ok:
			lat := at.Sub(e.CreatedAt)
			took = append(took, lat)
			if lat > sdk.ReceiveWithin {
				over++
			}
		case terminal(e.Lifecycle) || now.Sub(e.CreatedAt) > sdk.ReceiveWithin:
			over++
		}
	}
	if len(took) > 0 {
		sort.Slice(took, func(i, j int) bool { return took[i] < took[j] })
		p50, p90 = percentile(took, 50), percentile(took, 90)
	}
	return count, over, p50, p90, nil
}

func receivedAt(e *sdk.Envelope) (time.Time, bool) {
	for _, h := range e.History {
		if h.State == sdk.LifecycleReceived {
			return h.At, true
		}
	}
	return time.Time{}, false
}

// percentile：排好序的資料取第 p 百分位（最近序位法：ceil(p/100·n) 那一個）。
func percentile(sorted []time.Duration, p int) time.Duration {
	i := (p*len(sorted) + 99) / 100
	if i < 1 {
		i = 1
	}
	return sorted[i-1]
}

// forgetTries 清掉已經不在排隊的封包的重試記錄（tries／retryAt 是所有封包都有，不只主人的話）。
func (m *Module) forgetTries(ctx context.Context) {
	m.recvMu.Lock()
	ids := make([]string, 0, len(m.retryAt))
	for id := range m.retryAt {
		ids = append(ids, id)
	}
	for id := range m.tries {
		if _, ok := m.retryAt[id]; !ok {
			ids = append(ids, id)
		}
	}
	m.recvMu.Unlock()
	for _, id := range ids {
		if e, err := m.d.Bus.Get(ctx, id); err == nil && (e.Lifecycle == sdk.LifecycleQueued || e.Lifecycle == sdk.LifecycleNotified) {
			continue
		}
		m.recvMu.Lock()
		delete(m.tries, id)
		delete(m.retryAt, id)
		m.recvMu.Unlock()
	}
}
