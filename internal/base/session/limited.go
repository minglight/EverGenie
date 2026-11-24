package session

import (
	"context"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/sdk"
)

// 額度用完（DEV-47 S4，REQ-378、REQ-379、SES-49）：引擎回報 RATE_LIMITED 時 session 記成 limited，
// 那一輪拿到的話不算收到——退回排隊，恢復後由派送器補送；之後排著的話也等著，不改送別的對話（同一個帳號也用完了）。
// 過了恢復時間記回待命，之後照原本的規則（tickLimited）。

// limitDue：這個 limited 的 session 什麼時候算恢復（引擎說不出恢復時間就從最後一次說額度用完起算 LimitRetryAfter）。
func limitDue(s *sdk.Session) time.Time {
	if !s.LimitedUntil.IsZero() {
		return s.LimitedUntil
	}
	return s.StateSince.Add(LimitRetryAfter)
}

// limitOver：limited 而且已經過了恢復時間。
func limitOver(s *sdk.Session, now time.Time) bool {
	return s != nil && s.State == sdk.SessionStateLimited && !now.Before(limitDue(s))
}

// limitText 是給人看的一句：「額度用完，18:00 恢復」（不是今天就帶日期；說不出時間就只說額度用完）。
func limitText(until, now time.Time, loc *time.Location) string {
	if until.IsZero() {
		return "額度用完"
	}
	u, n := until.In(loc), now.In(loc)
	at := u.Format("15:04")
	if u.Year() != n.Year() || u.YearDay() != n.YearDay() {
		at = u.Format("1/2 15:04")
	}
	return "額度用完，" + at + " 恢復"
}

// requeueLimited：額度用完那一輪拿到的話（這一輪的、同一輪中途補話收進去的、已經貼上還沒開始的）都不算收到：
// 經核心送、她還沒動手的（notified／received）退回排隊（不算一次貼上失敗）；秘書直接傳的、做到一半的記中斷。
// 呼叫時持有 dispatchMu（drainLocked）。
func (m *Module) requeueLimited(ctx context.Context, ev EngineEvent, t *dispatchTurn) error {
	var ids []string
	if t != nil {
		ids = append(ids, t.carried...)
		if t.Envelope != "" {
			ids = append(ids, t.Envelope)
		}
		if len(ids) > 0 && !owner(t.From) {
			// 額度回來後平台可能自己接著做這一輪（對不到封包）：那一輪還是原寄件人觸發的，不算主人打字（法律第 4 條；
			// 同 SES-76 的作廢：恢復時間之後 VoidedKeep 內、只用一次）。
			if m.voided == nil {
				m.voided = map[string]voidedTurn{}
			}
			at := m.d.Clock.Now()
			if ev.Until.After(at) {
				at = ev.Until
			}
			m.voided[ev.Session] = voidedTurn{id: t.Envelope, origin: t.Origin, from: t.From, at: at}
		}
		t.carried = nil
		t.Envelope = ""
		t.ended = ev // 下一輪（恢復後補送的那則）不再把這一輪的話當成中途補話帶著
		delete(m.pendingPeers, t)
		t.Adopted = false
		m.storeTurn(ev.Session, t.turn, false)
	}
	// 經核心送的一次只貼一則：這個 session 還沒結束的（核心重開丟了這一輪的記憶也一樣）都是這一輪的。
	rows, err := m.d.Bus.List(ctx, ev.Session, sdk.LifecycleNotified, sdk.LifecycleReceived, sdk.LifecycleWorking, sdk.LifecycleBlocked)
	if err != nil {
		return err
	}
	for _, e := range rows {
		if e.Route == sdk.EnvelopeRouteCore {
			ids = append(ids, e.ID)
		}
	}
	reason := limitText(ev.Until, m.d.Clock.Now(), m.location()) + "後再送"
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		e, err := m.d.Bus.Get(ctx, id)
		if err != nil || terminal(e.Lifecycle) {
			continue
		}
		// 直接傳的沒辦法重送；已經動手做了一部分的（working／blocked）不重送，免得她做兩次（跟 REQ-371 同一個原則）：記中斷。
		if e.Route == sdk.EnvelopeRouteDirect || e.Lifecycle == sdk.LifecycleWorking || e.Lifecycle == sdk.LifecycleBlocked {
			why := limitText(ev.Until, m.d.Clock.Now(), m.location())
			if e.Lifecycle == sdk.LifecycleWorking || e.Lifecycle == sdk.LifecycleBlocked {
				why += "，做到一半停了"
			}
			if err = m.finish(ctx, e, sdk.LifecycleInterrupted, why, "", false); err != nil {
				return err
			}
			continue
		}
		if err = m.d.Bus.Transition(ctx, e.ID, sdk.LifecycleQueued, bus.TransitionOpt{Reason: reason}); err != nil {
			return err
		}
	}
	return nil
}

// tickLimited（Tick 持有 m.mu）：過了恢復時間的 limited session 記回待命（她的畫面本來就停在輸入框），派送器照常送排著的話；
// 之後全照原本的規則：還是額度用完會再回報一次（從那一刻重新算，REQ-379）。這是引擎給的恢復時間（或說不出時的重試節奏），
// 只決定什麼時候再試一次送話（S5-2）。回傳要不要叫醒派送器。
func (m *Module) tickLimited(ctx context.Context, e *entry, now time.Time) bool {
	if !limitOver(&e.session, now) || e.session.Pending != nil {
		return false
	}
	m.setState(e, sdk.SessionStateIdle, now)
	if err := m.changed(ctx, e); err != nil {
		m.warn("儲存額度恢復失敗", err)
	}
	return true
}

func (m *Module) location() *time.Location {
	if m.config != nil {
		return m.config.Location()
	}
	return time.Local
}
