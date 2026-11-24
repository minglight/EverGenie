package session

import (
	"context"

	"github.com/minglight/evergenie/sdk"
)

// AnswerNotice 將通知答案交給本機等待中的確認。
func (m *Module) AnswerNotice(ctx context.Context, session, noticeID, answer string) (bool, error) {
	if handled, err := m.handleWorkAnswer(ctx, noticeID, answer); handled {
		return err == nil, err // 工作 session 超過上限的提問（P7-5）
	}
	m.mu.Lock()
	a := m.approvals[session]
	known := false
	if a != nil {
		_, known = a.notices[noticeID]
	}
	m.mu.Unlock()
	if !known {
		return false, nil
	}
	err := m.handle(ctx, &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": noticeID, "room": session, "text": answer, "via": "api"}})
	return err == nil, err
}

func (m *Module) ResolveNotices(ctx context.Context, name, id string, d Decision, by string) (bool, []string, error) {
	if handled, applied, err := m.resolveNative(ctx, name, id, d, by); handled {
		return applied, nil, err // 在畫面上等的確認沒有通知（SES-47、SES-48）
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var notices []string
	if a := m.approvals[name]; a != nil && a.id == id {
		for n := range a.notices {
			notices = append(notices, n)
		}
	}
	applied, err := m.resolve(ctx, name, id, d)
	if !applied {
		notices = nil
	}
	return applied, notices, err
}
