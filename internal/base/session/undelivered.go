package session

import (
	"context"
	"sort"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 總保險（SES-77，DEV-162）：不管原因是什麼，「她待命著、給她的話卻一直送不進去」不能沒有人知道。
// 派送器每一輪看一次：先試一次不會傷到東西的自救（把沒有動靜的那一則放掉），還是不通就發事實事件
// （platform.TopicSessionStuck），要不要通知主人由 notify 決定（法律第 5 條）；健檢用 Stuck 查現況。
// 不自動重開她的對話、不往她的對話打字。

const (
	// StuckAfter：她待命、話卻排了這麼久還沒送進去（或貼上、收到之後沒有動靜）就算卡住。
	StuckAfter = 10 * time.Minute

	// stuckRetryReason：貼上之後一直沒有開始，放回排隊重送一次（這一次補送有自救過就不再重送，rescues）。
	stuckRetryReason = "等了 10 分鐘沒有開始，重新送一次"
	// stuckOrphanReason：記成收到／處理中之後她待命了這麼久都沒有動靜（事件丟了），對話記錄裡也看不到她回話：記中斷。
	stuckOrphanReason = "等了 10 分鐘沒有動靜，不確定有沒有做完"
)

// stuckChange 是這一輪要發的事實事件（放開 dispatchMu 之後才發）。
type stuckChange struct {
	topic   string
	payload map[string]any
}

// stuckEpisode 是一個對話「這一次卡住」的記錄（dispatchMu 底下；只在記憶體）。從第一次自救或第一次報卡住開始，
// 到話真的送進去（有一則被她收下、做完）或排著的話清空為止。
type stuckEpisode struct {
	rescuedAt  time.Time // 第一次自救的時間（之後再自救不重設：不然每放掉一則就又多等一輪，永遠報不出去）
	reportedAt time.Time // 報卡住的時間（零值＝還沒報）
	since      time.Time // 這一次從什麼時候開始等（報出去的「等了多久」）
}

func (ep *stuckEpisode) start() time.Time {
	if ep.rescuedAt.IsZero() || (!ep.reportedAt.IsZero() && ep.reportedAt.Before(ep.rescuedAt)) {
		return ep.reportedAt
	}
	return ep.rescuedAt
}

// watchStuck（dispatch 每一輪，持有 dispatchMu）：照 SES-77 自救、算出現在卡住的對話，回要發的事件與有沒有動過封包。
// rows 跟 expireAdopted 用的是同一份：那邊剛放掉的那一則在這裡還是「adopted 的收到」，這一輪不再動它。
func (m *Module) watchStuck(ctx context.Context, rows []*sdk.Envelope, now time.Time) (out []stuckChange, moved bool) {
	if m.stuckEpisodes == nil {
		m.stuckEpisodes = map[string]*stuckEpisode{}
	}
	bySession := map[string][]*sdk.Envelope{}
	progress := map[string]time.Time{} // 每個對話最近一次「話真的進去了」：她收下（開始事件認領）或做完
	for _, e := range rows {
		if e.Route != sdk.EnvelopeRouteCore {
			continue
		}
		if !terminal(e.Lifecycle) {
			bySession[e.To] = append(bySession[e.To], e)
		}
		_, adopted := adoptedSince(e)
		live := e.Lifecycle == sdk.LifecycleReceived || e.Lifecycle == sdk.LifecycleWorking || e.Lifecycle == sdk.LifecycleBlocked
		if e.Lifecycle == sdk.LifecycleReplied || (live && !adopted) {
			if at := lastStep(e); at.After(progress[e.To]) {
				progress[e.To] = at
			}
		}
	}
	m.stuckMu.Lock()
	previous := m.stuck
	m.stuckMu.Unlock()
	names := map[string]bool{}
	for name := range bySession {
		names[name] = true
	}
	for name := range m.stuckEpisodes {
		names[name] = true
	}
	current := map[string]platform.SessionStuck{}
	for name := range names {
		list := bySession[name]
		ep := m.stuckEpisodes[name]
		if len(list) == 0 || (ep != nil && progress[name].After(ep.start())) {
			delete(m.stuckEpisodes, name) // 排著的話清空了，或有一則真的進去了：這一次結束
			ep = nil
		}
		if len(list) == 0 {
			continue
		}
		reported := ep != nil && !ep.reportedAt.IsZero()
		since, agent, ok := m.settled(name)
		if !ok {
			// 忙著、等主人回答、額度用完、停了、啟動中：各有自己的規則，不開始算；已經報過的不因此算通了（話還排著）。
			if prev, was := previous[name]; was && reported {
				prev.Count = len(list)
				current[name] = prev
			}
			continue
		}
		var gate time.Time // 排著的話裡最早開始等的（只算她待命的這一段）
		count, rescued, head, dialog := 0, false, false, false
		for _, e := range list {
			start := lastStep(e)
			if e.Lifecycle == sdk.LifecycleQueued {
				start = e.CreatedAt // 退避重試每次都寫一筆歷程：排隊的從寄出起算
			}
			if start.Before(since) {
				start = since
			}
			waited := !now.Before(start.Add(StuckAfter))
			_, adopted := adoptedSince(e)
			switch {
			case e.Lifecycle == sdk.LifecycleQueued:
				// 引擎說她的畫面開著確認框（核心不知道有題目在等）：話送不進去多半就是這個，做法不一樣（去回答那個框）。
				if n := len(e.History); n > 0 && e.History[n-1].Detail == dialogHoldReason {
					dialog = true
				}
			case adopted || !waited || m.givingUp[e.ID]:
				head = true // 前面有一則貼上／收到了還在等（adopted 的有自己的期限，SES-76）：後面排著的先不算
			case e.Lifecycle == sdk.LifecycleNotified:
				// 貼上了卻一直沒有開始、也沒有任何事件：放回排隊重送一次（引擎會先查上一次到了沒）；重送過還是這樣就記沒送到。
				rescued = true
				if rescues(e) > 0 {
					if err := m.finish(ctx, e, sdk.LifecycleFailed, notStartedReason, "", false); err != nil {
						m.warn("記錄沒送到失敗", err)
					}
					continue
				}
				if err := m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: stuckRetryReason}); err != nil {
					m.warn("放回排隊失敗", err)
				}
			default:
				// 收到／處理中，她卻待命了這麼久：這一輪的結束事件丟了，不會再來。收掉，後面的話才送得進去。
				rescued = true
				m.dropTurnEnvelope(name, e.ID, now)
				m.giveUp(ctx, e, stuckOrphanReason) // 先查對話記錄她有沒有回這句：有就記已回覆
				continue
			}
			count++
			if gate.IsZero() || start.Before(gate) {
				gate = start
			}
		}
		if rescued {
			moved = true
			if m.log != nil {
				m.log.Warn("給她的話一直沒有動靜，先放掉卡著的那一則再送", "session", name)
			}
			m.markRescued(name, now)
			ep = m.stuckEpisodes[name]
		}
		if count == 0 {
			delete(m.stuckEpisodes, name)
			continue
		}
		if ep != nil && ep.since.IsZero() {
			ep.since = gate
		}
		due := false
		switch {
		case reported:
			due = true // 報過了：到這一次結束前都算卡住
		case ep != nil:
			// 自救過（含 SES-76 的作廢）：從第一次自救起再給一個期限，還有話排著就算卡住（不管前面那則是不是又貼上了）。
			at := ep.rescuedAt
			if since.After(at) {
				at = since
			}
			due = !now.Before(at.Add(StuckAfter))
		default:
			due = !head && !now.Before(gate.Add(StuckAfter))
		}
		if !due || m.agentDisabled(ctx, agent) { // 秘書停用：排著的話由派送器記送不了（SES-58）
			continue
		}
		if ep == nil {
			ep = &stuckEpisode{since: gate}
			m.stuckEpisodes[name] = ep
		}
		if ep.reportedAt.IsZero() {
			ep.reportedAt = now
		}
		s := platform.SessionStuck{Name: name, Agent: agent, Count: count, Since: ep.since, Dialog: dialog}
		current[name] = s
	}
	m.stuckMu.Lock()
	defer m.stuckMu.Unlock()
	agentStuck := map[string]bool{}
	for _, s := range current {
		agentStuck[s.Agent] = true
	}
	if !m.stuckSwept {
		// 核心重開後的第一輪：上一次報過的記在記憶體裡、已經沒了。說一次現在卡著的是哪幾位，訂閱者把其他人的舊提醒收掉。
		m.stuckSwept = true
		agents := make([]string, 0, len(agentStuck))
		for id := range agentStuck {
			agents = append(agents, id)
		}
		sort.Strings(agents)
		out = append(out, stuckChange{platform.TopicSessionUnstuck, map[string]any{"reset": true, "agents": agents}})
	}
	for name, was := range previous {
		if _, still := current[name]; !still {
			out = append(out, stuckChange{platform.TopicSessionUnstuck, map[string]any{"name": name, "agent": was.Agent, "agent_stuck": agentStuck[was.Agent]}})
		}
	}
	for name, s := range current {
		if _, was := previous[name]; !was {
			minutes := int(now.Sub(s.Since) / time.Minute)
			if m.log != nil {
				m.log.Error("給她的話送不進去", "session", name, "count", s.Count, "minutes", minutes)
			}
			out = append(out, stuckChange{platform.TopicSessionStuck, map[string]any{"name": name, "agent": s.Agent, "count": s.Count, "minutes": minutes, "dialog": s.Dialog}})
		}
	}
	m.stuck = current
	return out, moved
}

// markRescued 記下剛替這個對話自救過（持有 dispatchMu）；同一次卡住只記第一次。
func (m *Module) markRescued(name string, now time.Time) {
	if m.stuckEpisodes == nil {
		m.stuckEpisodes = map[string]*stuckEpisode{}
	}
	ep := m.stuckEpisodes[name]
	if ep == nil {
		ep = &stuckEpisode{}
		m.stuckEpisodes[name] = ep
	}
	if ep.rescuedAt.IsZero() {
		ep.rescuedAt = now
	}
}

// publishStuck 發 watchStuck 算出來的事件（不持有 dispatchMu；卡住的帶上她現在的角色名，通知要寫出是誰）。
func (m *Module) publishStuck(ctx context.Context, changes []stuckChange) {
	for _, c := range changes {
		if agent, _ := c.payload["agent"].(string); agent != "" && c.topic == platform.TopicSessionStuck && m.d.Agents != nil {
			if name, err := m.d.Agents.PersonaName(ctx, agent); err == nil && name != "" {
				c.payload["agent_name"] = name
			}
		}
		m.publish(ctx, c.topic, c.payload)
	}
}

// Stuck 回現在話送不進去的對話（SES-77；健檢 sessions.stuck 與看板「系統健康」用），照對話名排。
func (m *Module) Stuck(ctx context.Context) []platform.SessionStuck {
	m.stuckMu.Lock()
	out := make([]platform.SessionStuck, 0, len(m.stuck))
	for _, s := range m.stuck {
		out = append(out, s)
	}
	m.stuckMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	now := m.d.Clock.Now()
	for i := range out {
		out[i].Minutes = int(now.Sub(out[i].Since) / time.Minute)
		if m.d.Agents != nil && out[i].Agent != "" {
			if name, err := m.d.Agents.PersonaName(ctx, out[i].Agent); err == nil {
				out[i].AgentName = name
			}
		}
	}
	return out
}
