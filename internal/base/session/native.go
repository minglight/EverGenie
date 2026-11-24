package session

import (
	"context"
	"fmt"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// handOff 在 mu 底下呼叫：把還在等核心的確認交回 session 自己的畫面（Pending 標 Native、留著）。
func (m *Module) handOff(ctx context.Context, e *entry, a *approval) {
	if a.native {
		return
	}
	a.native = true
	select {
	case a.result <- approvalResult{}: // 引擎拿到 decided=false，退回自己的確認畫面
	default:
	}
	if p := e.session.Pending; p != nil && p.ID == a.id {
		copy := *p
		copy.Native = true
		e.session.Pending = &copy
	}
	if err := m.changed(ctx, e); err != nil {
		m.warn("儲存確認逾時失敗", err)
	}
}

// dropNative 在 mu 底下呼叫：引擎有新的事件＝畫面上那個確認已經有人答了（或被中斷了），收掉 Native 的 Pending。
// 還在等核心回答的（不是 Native）不動：那時引擎的事件不代表確認收掉了。
func (m *Module) dropNative(e *entry) bool {
	a := m.approvals[e.session.Name]
	if a == nil || !a.native {
		return false
	}
	delete(m.approvals, e.session.Name)
	e.session.Pending = nil
	return true
}

// restoreNative：serve 重開時讀回來的 Pending——等它的那一次 RequestApproval 已經不在了，確認一定是在畫面上等，當成 Native 接回來。
func (m *Module) restoreNative(e *entry) {
	p := e.session.Pending
	if p == nil || p.ID == "" || e.session.State == sdk.SessionStateStopped {
		return
	}
	copy := *p
	copy.Native = true
	e.session.Pending = &copy
	m.approvals[e.session.Name] = &approval{id: copy.ID, native: true, result: make(chan approvalResult, 1), questions: append([]sdk.Question(nil), copy.Questions...), notices: map[string]string{}, answers: map[string]string{}}
}

// resolveNative：主人回答一個在畫面上等的確認（或已經過了期限、Tick 還沒收的），由引擎在畫面上替他回答。
// handled=false 表示不是這種確認，照一般的 resolve 走。呼叫時不能拿著 mu（要等引擎）。
func (m *Module) resolveNative(ctx context.Context, name, id string, d Decision, by string) (handled, applied bool, err error) {
	m.mu.Lock()
	if err := m.ready(ctx); err != nil {
		m.mu.Unlock()
		return true, false, err
	}
	e, a := m.entries[name], m.approvals[name]
	if e == nil || a == nil || a.id != id || e.session.Pending == nil || e.session.Pending.ID != id {
		m.mu.Unlock()
		return false, false, nil
	}
	if !a.native {
		if m.d.Clock.Now().Before(a.deadline) {
			m.mu.Unlock()
			return false, false, nil
		}
		m.handOff(ctx, e, a) // 過了期限、Tick 還沒收：先交回畫面，再照畫面上的確認代答
	}
	if d == nil {
		m.mu.Unlock()
		return true, false, platform.Invalid("decision", "請提供決定")
	}
	for _, q := range a.questions {
		if _, ok := d.Answers[q.Text]; !ok {
			m.mu.Unlock()
			return true, false, platform.Invalid("answers", "請回答全部問題")
		}
	}
	if a.answering {
		m.mu.Unlock()
		return true, false, platform.Conflict("正在替你回答這一題")
	}
	a.answering = true
	pending := *e.session.Pending
	account := e.session.Account
	m.mu.Unlock()

	answered, reason := false, ""
	eng, err := m.d.Engine.For(account)
	if err == nil {
		call, cancel := context.WithTimeout(ctx, m.limits.Answer)
		answered, reason, err = eng.AnswerApproval(call, name, &pending, d, by)
		if err != nil && gaveUp(call, ctx) {
			// 不知道按下去了沒：這一題留著。真的按到了，她接下來的事件會把它收掉（SES-48）。
			err = silent("替你在秘書的畫面上回答", m.limits.Answer, "這一題先留著；過一下再試，或直接到秘書的畫面上回答。")
		}
		cancel()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	a.answering = false
	if err != nil {
		return true, false, err
	}
	current := m.approvals[name] == a // 等引擎的時候，它按下去引起的事件可能已經先把 Pending 收掉了
	switch {
	case answered:
		if current {
			delete(m.approvals, name)
			e.session.Pending = nil
			m.setState(e, sdk.SessionStateBusy, m.d.Clock.Now())
			if err := m.changed(ctx, e); err != nil {
				return true, false, err
			}
		}
		m.enqueueApproval(name, "approval_resolved")
		m.publish(ctx, "session.approval.resolved", map[string]any{"name": name, "pending_id": id, "notices": []string{}})
		return true, true, nil
	case reason == sdk.AnswerNone || reason == "gone":
		// 畫面上已經沒有這個確認：有人在別處答了、被中斷了，或 session 不在了。收掉；看板顯示「已經在別處回答了」。
		if current {
			delete(m.approvals, name)
			e.session.Pending = nil
			if reason == sdk.AnswerNone && e.session.State == sdk.SessionStateWaiting {
				m.setState(e, sdk.SessionStateIdle, m.d.Clock.Now())
			}
			if err := m.changed(ctx, e); err != nil {
				return true, false, err
			}
		}
		return true, false, nil
	case reason == sdk.AnswerUnsupported:
		return true, false, platform.Unavailable("這一題看板代答不了，請在秘書的對話裡回答（手機或網頁都行）")
	default:
		return true, false, platform.Unavailable(fmt.Sprintf("沒辦法在秘書的畫面上回答（%s），請在秘書的對話裡回答（手機或網頁都行）", reason))
	}
}
