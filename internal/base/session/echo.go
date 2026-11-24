package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/sdk"
)

func terminal(s sdk.Lifecycle) bool {
	return s == sdk.LifecycleReplied || s == sdk.LifecycleUnanswered || s == sdk.LifecycleFailed || s == sdk.LifecycleInterrupted
}
func (m *Module) person(ctx context.Context, id string) string {
	id = m.agentID(ctx, id)
	if name, err := m.d.Agents.PersonaName(ctx, id); err == nil {
		return name
	}
	return id
}
func (m *Module) finish(ctx context.Context, e *sdk.Envelope, state sdk.Lifecycle, reason, last string, noEcho bool) error {
	if terminal(e.Lifecycle) {
		return nil
	}
	if err := m.d.Bus.Transition(ctx, e.ID, state, bus.TransitionOpt{Reason: reason, LastMessage: last}); err != nil {
		return err
	}
	next, err := m.d.Bus.Get(ctx, e.ID)
	if err != nil {
		return err
	}
	return m.completedEnvelope(ctx, next, noEcho)
}
func (m *Module) completedEnvelope(ctx context.Context, e *sdk.Envelope, noEcho bool) error {
	if !terminal(e.Lifecycle) || m.completed[e.ID] {
		return nil
	}
	if e.Lifecycle == sdk.LifecycleReplied {
		// 排程的封包回覆了：schedule.done 與夜間整理的重開對話由 jobs 業務模組聽 bus.envelope.state 自己做（SES-10、SES-9）。
		// 沒有回覆文字（例如閒著的提示收尾、拿不到她最後一句）就不自動回，不貼一則空白的話給寄件的秘書。
		if e.Route == sdk.EnvelopeRouteCore && e.Kind == sdk.EnvelopeKindSay && e.ReplyTo == "" && !owner(e.From) && e.From != "system" && strings.TrimSpace(e.LastMessage) != "" {
			_, err := m.d.Bus.Send(ctx, &sdk.Envelope{Route: sdk.EnvelopeRouteCore, Kind: sdk.EnvelopeKindSay, From: m.agentID(ctx, e.To), To: m.agentID(ctx, e.From), Body: e.LastMessage, ReplyTo: e.ID, IdempotencyKey: "reply:" + e.ID})
			if err != nil {
				return err
			}
		}
	} else if e.Kind == sdk.EnvelopeKindEcho {
		if m.log != nil {
			m.log.Warn("派送回音未完成", "id", e.ID)
		}
	} else if !noEcho && !(e.Route == sdk.EnvelopeRouteDirect && e.Reason == "對方不在") && e.ReplyTo == "" && e.From != "system" && !owner(e.From) {
		name := m.person(ctx, e.To)
		var body string
		switch e.Lifecycle {
		case sdk.LifecycleUnanswered:
			body = fmt.Sprintf("%s 收到了你的訊息，但沒有回覆。它最後說：「%s」", name, prefix(e.LastMessage, 200))
		case sdk.LifecycleInterrupted:
			why := ""
			if r := failReason(e); r != "" {
				why = "（" + prefix(r, 200) + "）"
			}
			body = fmt.Sprintf("%s 處理到一半被中斷了%s。原文：「%s」", name, why, prefix(e.Body, 200))
		case sdk.LifecycleFailed:
			body = fmt.Sprintf("送不了給 %s（%s）。原文：「%s」", name, e.Reason, prefix(e.Body, 200))
		}
		_, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindEcho, Route: sdk.EnvelopeRouteCore, From: "system", To: m.agentID(ctx, e.From), Body: body, IdempotencyKey: "echo:" + e.ID})
		if err != nil {
			return err
		}
	}
	m.completed[e.ID] = true
	return nil
}
func (m *Module) retryAnswers(ctx context.Context, rows []*sdk.Envelope, now time.Time) error {
	type answerGroup struct {
		first    time.Time
		last     *sdk.Envelope
		count    int
		received bool
	}
	groups := map[string]*answerGroup{}
	for _, e := range rows {
		if e.Kind != sdk.EnvelopeKindAnswer || e.Notice == "" {
			continue
		}
		g := groups[e.Notice]
		if g == nil {
			g = &answerGroup{first: e.CreatedAt}
			groups[e.Notice] = g
		}
		g.count++
		if e.CreatedAt.Before(g.first) {
			g.first = e.CreatedAt
		}
		if g.last == nil || g.last.CreatedAt.Before(e.CreatedAt) || (g.last.CreatedAt.Equal(e.CreatedAt) && g.last.ID < e.ID) {
			g.last = e
		}
		for _, h := range e.History {
			if h.State == sdk.LifecycleReceived {
				g.received = true
			}
		}
	}
	for id, g := range groups {
		e := g.last
		// 收件的對話在這台沒有、也叫不起來（SES-58）：再送也一樣，不重送。
		if g.received || e.Lifecycle != sdk.LifecycleFailed || e.Reason == NoTargetReason || !now.Before(g.first.Add(2*time.Hour)) || now.Before(lastStep(e).Add(30*time.Second)) {
			continue
		}
		_, err := m.d.Bus.Send(ctx, &sdk.Envelope{Kind: sdk.EnvelopeKindAnswer, Route: sdk.EnvelopeRouteCore, From: "system", To: e.To, Body: e.Body, Notice: id, IdempotencyKey: fmt.Sprintf("%s#%d", id, g.count+1)})
		if err != nil {
			return err
		}
	}
	return nil
}

// jobID 認出排程送來的 routine 封包（冪等鍵「job:<id>#<觸發時間>」，jobs 業務模組送的；SES-10 的臨時 session 用它取名與判斷）。
func jobID(e *sdk.Envelope) string {
	if e.Kind != sdk.EnvelopeKindRoutine || !strings.HasPrefix(e.IdempotencyKey, "job:") {
		return ""
	}
	id, _, _ := strings.Cut(strings.TrimPrefix(e.IdempotencyKey, "job:"), "#")
	return id
}
