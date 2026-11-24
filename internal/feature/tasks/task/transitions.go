package task

import (
	"context"

	"github.com/minglight/evergenie/internal/base/platform"
)

type transition struct{ role, kind, from, to string }

// transitionTable 是 schemas/task.md 的轉移表（六種狀態，2026-11-01）。交付等驗收（Submit）是 doing→waiting_user，
// 驗收、退回是 waiting_user→proposed 的結局／doing，都用同一張表檢查；哪個操作能走哪一格由 allow 決定。
func transitionTable() map[transition]bool {
	out := map[transition]bool{}
	add := func(role, kind, from, to string) { out[transition{role, kind, from, to}] = true }
	for _, kind := range []string{"task", "ask"} {
		for _, to := range statuses {
			add("user", kind, "", to)
		}
		for _, to := range []string{"idea", "doing", "waiting_user", "waiting_external"} {
			add("agent", kind, "", to)
		}
		if kind == "ask" {
			add("agent", kind, "", "done")
			add("agent", kind, "", "cancelled")
		}
		for _, from := range statuses {
			if ended(from) {
				continue
			}
			for _, to := range statuses {
				add("user", kind, from, to)
			}
			add("agent", kind, from, from)
		}
		add("agent", kind, "idea", "doing")
		for _, to := range []string{"waiting_user", "waiting_external"} {
			add("agent", kind, "doing", to)
			add("agent", kind, to, "doing")
		}
		if kind == "ask" {
			add("agent", kind, "doing", "done")
			add("agent", kind, "doing", "cancelled")
		}
	}
	return out
}
func (m *Module) role(ctx context.Context, by string) (string, error) {
	if by == "" {
		return "", platform.Invalid("by", "請提供操作者")
	}
	if platform.IsOwner(by) {
		return "user", nil
	}
	if by == "system" {
		return "agent", nil
	}
	if id, ok := m.d.Agents.Resolve(ctx, by); ok && id == by {
		return "agent", nil
	}
	return "", platform.Invalid("by", "請使用職位識別碼或使用者身分")
}
func (m *Module) allow(role, kind, from, to, op string) error {
	if !oneOf(kind, "task", "ask") {
		return platform.Invalid("type", "任務類型不正確")
	}
	if to == "review" {
		return platform.Conflict("請用送驗收操作（submit）交付，單會變成等你處理（要你驗收）")
	}
	if !oneOf(to, statuses...) {
		return platform.Invalid("status", "任務狀態不正確")
	}
	if role == "agent" && kind == "task" && ended(to) {
		return platform.Forbidden("一般任務做完：有主人的驗收條件就照條件驗過用結單（tasks done）自己結，沒有就送驗收（Submit）由使用者結單")
	}
	if op == "submit" && kind != "task" {
		return platform.Conflict("問答類回答完直接結單，不用送驗收")
	}
	if ended(to) && op == "update" {
		return platform.Conflict("請使用結單或通過驗收操作")
	}
	if !m.transitions[transition{role, kind, from, to}] {
		return platform.Conflict("目前狀態不允許這項操作")
	}
	return nil
}
