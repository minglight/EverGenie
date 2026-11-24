package chat

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

// Board 登記首頁「等你回覆的討論串」（app.BoardProvider，overview 收；2026-11-01 從 overview 經 API 讀 session.list_threads 改過來）：
// 所有秘書、不含已停的討論串裡，狀態是 waiting_user 的。沒接 Sessions 就不登記。
func (m *Module) Board() []app.BoardPart {
	if m.d.Sessions == nil {
		return nil
	}
	return []app.BoardPart{{Key: "waiting_threads", Load: func(ctx context.Context) (any, error) {
		list, _, err := m.d.Sessions.Threads(ctx, "", false)
		if err != nil {
			return nil, err
		}
		var out []sdk.ThreadSummary
		for _, t := range list {
			if t != nil && t.Status == sdk.ThreadStatusWaitingUser {
				out = append(out, *t)
			}
		}
		return out, nil
	}}}
}

var _ app.BoardProvider = (*Module)(nil)
