package session

import (
	"context"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// FailedShownFor：主人送失敗的話在 Session.Messages 留多久（之後就只在訊息歷程裡）。
const FailedShownFor = 12 * time.Hour

// WithMessages 把送進這些 session、還沒結束的話填進 Messages（主人的話另外帶 Wait：還沒收到的原因，SES-51）（看板退出再進來畫回「還沒讀到／沒送到」，REQ-302）：
// 派送器要處理的（core）都列，給看板畫回還沒收到的話；主人的 chat 送失敗的、這一輪沒做完（interrupted，附原因）的，
// 12 小時內、之後主人沒再送同樣內容（失敗）或沒再對這個對話說話（中斷）的也列（S5-2）。
func (m *Module) WithMessages(ctx context.Context, list ...*sdk.Session) {
	if len(list) == 0 {
		return
	}
	rows, err := m.d.Bus.List(ctx, "")
	if err != nil {
		return
	}
	want := map[string]*sdk.Session{}
	for _, s := range list {
		if s != nil {
			want[s.Name] = s
		}
	}
	now := m.d.Clock.Now()
	for i, e := range rows {
		s := want[e.To]
		if s == nil || e.Route != sdk.EnvelopeRouteCore {
			continue
		}
		switch {
		case e.Lifecycle == sdk.LifecycleFailed:
			if !owner(e.From) || e.Kind != sdk.EnvelopeKindChat || now.Sub(e.CreatedAt) > FailedShownFor || resent(rows[i+1:], e) {
				continue
			}
		case e.Lifecycle == sdk.LifecycleInterrupted:
			if !owner(e.From) || e.Kind != sdk.EnvelopeKindChat || now.Sub(e.CreatedAt) > FailedShownFor || spokeAfter(rows[i+1:], e) || s.LastTurnEndAt.After(lastStep(e)) {
				continue // 之後她又做完過一輪（含主人在終端機直接打的）：「沒做完」不用再掛著（S5-4）
			}
		case terminal(e.Lifecycle):
			continue
		}
		msg := sdk.SessionMessage{
			ID: messageID(e), From: e.From, Body: e.Body, Lifecycle: e.Lifecycle, Reason: failReason(e),
			ReasonDetail: failDetail(e), ReasonKind: failKind(e),
			Interject: interjected(e), CreatedAt: e.CreatedAt,
		}
		if isOwnerChat(e) {
			msg.Wait = m.messageWait(ctx, e, s, rows) // 還沒收到：伺服器說得出的原因（REQ-384）
			if msg.Wait == sdk.MessageWaitLimited {
				msg.WaitUntil = s.LimitedUntil // 「額度用完，18:00 恢復，到時就送」（REQ-378）
			}
		}
		s.Messages = append(s.Messages, msg)
	}
}

func messageID(e *sdk.Envelope) string {

	return e.ID
}

// resent：失敗那則之後，主人又送了同樣內容給同一個 session（看板的「重送」）。
func resent(later []*sdk.Envelope, e *sdk.Envelope) bool {
	for _, n := range later {
		if n.To == e.To && n.Body == e.Body && owner(n.From) {
			return true
		}
	}
	return false
}

// spokeAfter：這一輪沒做完之後主人又對同一個對話說了話（不管內容）——「沒做完」的提示就不用再掛著。
// 中斷之前就排著的不算。
func spokeAfter(later []*sdk.Envelope, e *sdk.Envelope) bool {
	for _, n := range later {
		if n.To == e.To && owner(n.From) && n.Kind == sdk.EnvelopeKindChat && n.CreatedAt.After(lastStep(e)) {
			return true
		}
	}
	return false
}

func interjected(e *sdk.Envelope) bool {
	for _, h := range e.History {
		if h.State == sdk.LifecycleNotified && (h.Detail == interjectReason || h.Detail == legacyInterjectReason) {
			return true
		}
	}
	return false
}

// failReason 把派送器記的原因翻成主人看得懂的話：失敗的照記下的原因；這一輪沒做完的只給白話原因（說不出就空），
// 平台原話另外放 failDetail（看板收在「詳細」，DEV-156）；其他回空。
func failReason(e *sdk.Envelope) string {
	switch e.Lifecycle {
	case sdk.LifecycleFailed:
		if e.Reason == "" {
			return "沒送到"
		}
		return e.Reason
	case sdk.LifecycleInterrupted:
		return e.Reason
	}
	return ""
}

// failDetail：這一輪沒做完時平台說的原話（turn_failed 的 last_message，S5-2）；跟白話原因一樣、不是中斷的、
// 或平台根本沒附說明（引擎填的固定一句，不是原話）都回空。
func failDetail(e *sdk.Envelope) string {
	if e.Lifecycle != sdk.LifecycleInterrupted || e.LastMessage == e.Reason || e.LastMessage == sdk.EngineNoPlatformMessage {
		return ""
	}
	return e.LastMessage
}

// failKind：這一輪沒做完是哪一種錯（只有引擎報錯、帳號登出的才有；主人叫停、對話結束的回空）。
func failKind(e *sdk.Envelope) sdk.EngineErrorKind {
	if e.Lifecycle != sdk.LifecycleInterrupted {
		return ""
	}
	return reasonKindOf(e.Reason)
}
