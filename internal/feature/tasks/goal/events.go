package goal

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 事件：收 notice.answered（主人確認或不要 draft Goal）、發 goal.changed。
func (m *Module) publish(ctx context.Context, g Goal, by string) {
	if err := m.d.Bus.Publish(ctx, "goal.changed", map[string]any{"id": g.ID, "state": string(g.State), "by": by}); err != nil {
		m.warn("Goal 事件發布失敗")
	}
}

func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	if ev == nil || ev.Topic != "notice.answered" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	id, _ := ev.Payload["id"].(string)
	if id == "" || ev.Payload["via"] == "auto" {
		return nil
	}
	answer, _ := ev.Payload["text"].(string)
	by, _ := ev.Payload["by"].(string)
	if by == "" {
		by = "owner(web)"
	}
	if !platform.IsOwner(by) {
		return platform.Forbidden("只有主人能確認或收回 Goal")
	}
	drafts, err := m.list(ctx, []sdk.GoalState{sdk.GoalStateDraft})
	if err != nil {
		return err
	}
	for _, draft := range drafts {
		if draft.Notice != id {
			continue
		}
		g, r, err := m.load(ctx, draft.ID)
		if err != nil {
			return err
		}
		if g.State != sdk.GoalStateDraft {
			continue
		}
		now := m.d.Clock.Now().In(m.loc)
		if answer == "確認" {
			g.State = sdk.GoalStateActive
			g.ConfirmedAt = now
			g.ConfirmedBy = by
		} else {
			g.State = sdk.GoalStateWithdrawn
			g.EndedAt = now
			g.EndedBy = by
		}
		g, err = m.save(ctx, g, r, by)
		if err != nil {
			return err
		}
		m.publish(ctx, g, by)
	}
	return nil
}
