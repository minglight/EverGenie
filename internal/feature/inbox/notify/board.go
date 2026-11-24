package notify

import (
	"context"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) Board() []app.BoardPart {
	return []app.BoardPart{
		{Key: "approvals", Load: func(ctx context.Context) (any, error) {
			list, err := m.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
			if err != nil {
				return nil, err
			}
			m.mu.Lock()
			tasks := m.tasks
			m.mu.Unlock()
			var out []sdk.Notification
			for _, n := range list {
				if n.Category != sdk.CategoryApprove || n.Inline {
					continue
				}
				// 掛在「等你」任務單上的通知（請驗收、等你決定、還在等你）：那張單自己就在首頁的 waiting_tasks，
				// 有驗收／退回／去聊；通知再列一次就是同一件事兩張卡、等你處理多算一件（TSK-26）。
				// 只藏任務自己推的那兩種（todo 是「請驗收」或就是單上的問句）；秘書另外問、內容不同的照列，不然那個問題和它的按鈕在首頁就看不到了。
				if n.Task != "" && tasks != nil {
					if t, err := tasks.Get(ctx, n.Task); err == nil && t != nil && t.Status == sdk.TaskStatusWaitingUser &&
						(n.Todo == "請驗收" || n.Todo == t.Ask) {
						continue
					}
				}
				out = append(out, n)
			}
			return out, nil
		}},
		{Key: "unread", Load: func(ctx context.Context) (any, error) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return int32(m.Unread()), nil
		}},
	}
}
