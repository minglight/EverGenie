package task

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
)

// Board 登記首頁「等你處理的任務單」（app.BoardProvider，overview 收；2026-11-01 從 overview 經 API 讀 tasks.waiting 改過來）。
// 內容跟 tasks.waiting 一樣（等你回覆、待驗收，等最久的在前）。
func (m *Module) Board() []app.BoardPart {
	return []app.BoardPart{{Key: "waiting_tasks", Load: func(ctx context.Context) (any, error) {
		return m.Waiting(ctx, m.d.Clock.Now())
	}}}
}

var _ app.BoardProvider = (*Module)(nil)
