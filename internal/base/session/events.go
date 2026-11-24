package session

import (
	"context"
	"time"

	"github.com/minglight/evergenie/sdk"
)

func (m *Module) loadSources(ctx context.Context) error {
	notices, err := m.d.Notify.List(ctx, nil, "", false)
	if err != nil {
		return err
	}
	envelopes, err := m.d.Bus.List(ctx, "")
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notices = map[string]string{}
	m.envelopes = map[string]*sdk.Envelope{}
	for _, n := range notices {

		if n.Room != "" && n.Category == sdk.CategoryApprove && (n.State == sdk.NotificationStateUnread || n.State == sdk.NotificationStateRead) {
			m.notices[n.ID] = n.Room
		}
	}
	for _, e := range envelopes {

		copy := *e
		m.envelopes[e.ID] = &copy
	}
	for _, t := range m.turns {
		if e := m.envelopes[t.Envelope]; e != nil && e.Route == sdk.EnvelopeRouteDirect {
			t.source = sdk.InputSourcePeer
		}
	}
	return nil
}
func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	if ev == nil {
		return nil
	}
	p := ev.Payload
	id := text(p, "id")
	room := text(p, "room")

	switch ev.Topic {
	case "agent.changed":
		// 秘書的設定改了（啟用、模式、帳號）：巡檢、模式、帳號下一次 Tick（1 秒內）就重看，不等 WatchdogEvery（SES-53）。
		m.mu.Lock()
		m.watchdog, m.modesAt, m.accountsAt = time.Time{}, time.Time{}, time.Time{}
		m.mu.Unlock()
		return nil
	case "notice.sent":
		m.mu.Lock()
		if text(p, "category") == string(sdk.CategoryApprove) && room != "" && !m.answered[id] {
			m.notices[id] = room
		}
		m.mu.Unlock()
	case "notice.answered":
		// 工作 session 超過上限的提問：回答是「照開／先排隊／關掉 <單號>」，由 session 自己處理，不送進模型（P7-5）。
		if handled, err := m.handleWorkAnswer(ctx, id, text(p, "text")); handled {
			return err
		}
		m.mu.Lock()
		delete(m.notices, id)
		m.answered[id] = true
		if a := m.approvals[room]; a != nil {
			if q, ok := a.notices[id]; ok {
				answer := text(p, "text")
				d := &sdk.Decision{Allow: answer == "允許", Message: answer}
				if len(a.questions) > 0 {
					a.answers[q] = answer
					if len(a.answers) < len(a.questions) {
						m.mu.Unlock()
						return nil
					}
					d = &sdk.Decision{Answers: a.answers}
				}
				_, err := m.resolve(ctx, room, a.id, d)
				m.mu.Unlock()
				return err
			}
			// RequestApproval reads answered notices once their IDs have been registered.
			m.mu.Unlock()
			return nil
		}
		owned := m.owned[id]
		m.mu.Unlock()
		if owned {
			return nil
		}
		// 要不要轉回發問的對話由 notify 判斷（relay）：自動收掉的、業務模組自己處理的（登記的通知來源）、系統提醒、
		// 沒綁對話的任務／待辦通知都不轉。session 不認得任何來源（2026-11-01 起）。
		if relay, _ := p["relay"].(bool); !relay {
			return nil
		}
		target := room
		if target == "" {
			target = text(p, "agent")
			// 綁在單上、沒綁對話的問句（等你決定，DEV-156 U-05）：那張單有工作 session（在跑、停了都算，停了的派送時會接回）
			// 就送到那裡——秘書多半是在那裡問的；沒有就送給秘書本人。這是預期的：工作 session 被主人關掉或還在排隊（work_max 滿了）時，
			// 答案放在那串，等它開起來才讀到（單同時回到進行中，工作 session 本來就會跟著開，P7-5）；不改送給秘書本人，免得同一件事分兩處講。
			if task := text(p, "task"); task != "" && target != "" && m.ids().IsTask(task) {
				name := workRoom(target, task)
				m.mu.Lock()
				_, known := m.entries[name]
				m.mu.Unlock()
				if known {
					target = name
				}
			}
		}
		if target == "" || target == "system" {
			return nil
		}
		_, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindAnswer, Route: sdk.EnvelopeRouteCore, To: target, From: "system", Body: text(p, "text"), Notice: id, IdempotencyKey: id + "#1"})
		return err
	case "bus.envelope.state":
		e, err := m.d.Bus.Get(ctx, id)
		if err == nil && e != nil {
			m.dispatchMu.Lock()
			err = m.completedEnvelope(ctx, e, false)
			m.dispatchMu.Unlock()
			if err != nil {
				return err
			}
		}
		m.mu.Lock()
		m.envelopes[id] = &sdk.Envelope{ID: id, To: text(p, "to"), Lifecycle: sdk.Lifecycle(text(p, "state"))}
		// 主人的話換了狀態（收到、送不到、重試）：推一次這個 session，看板馬上重抓泡泡，不等輪詢（SES-51）。
		if isOwnerChat(e) {
			if en := m.entries[e.To]; en != nil {
				m.announce(ctx, en)
			}
		}
		m.mu.Unlock()
		m.wakeDelivery()
	}
	return nil
}
