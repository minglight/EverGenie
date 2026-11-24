package requests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 每週權限摘要（S5 稽核，POL-11，REQ-393）：每週一 09:00（家的時區）之後第一次 Tick，
// 把上一週（上週一 09:00 到這週一 09:00）新套用、撤銷、到期、套用失敗的申請送成一則摘要通知，附上目前套用中的清單。
// 那一週沒有變動就不送。送過的週記在 <家>/runtime/policy/digest-week（policy 只在主機跑），重開 serve 不會重送。
const digestHour = 9

var digestStatuses = []sdk.PolicyStatus{sdk.PolicyStatusApplied, sdk.PolicyStatusRevoked, sdk.PolicyStatusExpired, sdk.PolicyStatusFailed}

// weekStart 是 now 所在那週的週一 digestHour 點（還沒到就是上週一）。
func weekStart(now time.Time) time.Time {
	d := time.Date(now.Year(), now.Month(), now.Day(), digestHour, 0, 0, 0, now.Location())
	d = d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
	if now.Before(d) {
		d = d.AddDate(0, 0, -7)
	}
	return d
}

func (m *Module) digestMark() string {
	return platform.RuntimeDir(m.home, "policy", "digest-week")
}

// digest 呼叫端持有 m.mu。
func (m *Module) digest(ctx context.Context, now time.Time) {
	if m.home == "" || m.d.Notify == nil {
		return
	}
	end := weekStart(now.In(m.loc))
	key := end.Format("2006-01-02")
	if b, err := os.ReadFile(m.digestMark()); err == nil && strings.TrimSpace(string(b)) == key {
		return
	}
	list, err := m.list(ctx, digestStatuses)
	if err != nil {
		m.warn("每週權限摘要讀不到申請")
		return
	}
	start := end.AddDate(0, 0, -7)
	var changed []string
	for i := len(list) - 1; i >= 0; i-- { // list 新的在前；摘要照編號由舊到新
		q := list[i]
		if q.DecidedAt.Before(start) || !q.DecidedAt.Before(end) {
			continue
		}
		changed = append(changed, fmt.Sprintf("%s %s：%s", q.ID, statusText(q.Status), m.titleOf(ctx, &q)))
	}
	if len(changed) > 0 {
		active := m.activeList()
		lines := append([]string{"這週的變動："}, changed...)
		lines = append(lines, fmt.Sprintf("目前套用中 %d 張：", len(active)))
		for _, q := range active {
			lines = append(lines, q.ID+" "+m.titleOf(ctx, &q))
		}
		title := fmt.Sprintf("本週權限變動（%d 筆）", len(changed))
		if _, err := m.d.Notify.Send(ctx, &sdk.Notification{Category: sdk.CategoryDigest, Title: title, Context: strings.Join(lines, "\n"), URL: "/policy", Agent: "system", By: "system", Lane: sdk.NotifyLaneSystem}); err != nil { // 每週自動摘要，系統那道（D-47）
			m.warn("每週權限摘要送不出去，下一分鐘再試")
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(m.digestMark()), 0o755); err == nil {
		err = os.WriteFile(m.digestMark(), []byte(key+"\n"), 0o644)
		if err != nil {
			m.warn("每週權限摘要的紀錄寫不進去")
		}
	}
}

func (m *Module) publish(ctx context.Context, q *sdk.PolicyRequest, by string) {
	if err := m.d.Bus.Publish(ctx, "policy.changed", map[string]any{"id": q.ID, "status": string(q.Status), "kind": string(q.Kind), "by": by}); err != nil {
		m.warn("權限申請事件發布失敗")
	}
}

// Tick 只處理帶 expires_at 的申請單（每分鐘一次；測試可以直接呼叫）：Q7b 起核准不再設到期，只有舊單才會被標成 expired 並拿掉。
// 每週一早上順便送權限摘要（digest.go）。
func (m *Module) Tick(ctx context.Context, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready(ctx) != nil {
		return
	}
	m.digest(ctx, now)
	var due []sdk.PolicyRequest
	next := []sdk.PolicyRequest{}
	guidance := false
	for _, q := range m.activeList() {
		if !q.Permanent && !q.ExpiresAt.IsZero() && !now.Before(q.ExpiresAt) {
			due = append(due, q)
			guidance = guidance || q.Kind == sdk.PolicyKindGuidance
			continue
		}
		next = append(next, q)
	}
	if len(due) == 0 {
		return
	}
	if err := m.swap(ctx, next, guidance); err != nil {
		m.warn("到期的權限拿不掉：引擎沒有回應，下一分鐘再試")
		return
	}
	for _, d := range due {
		r, q, err := m.load(ctx, d.ID)
		if err != nil || q.Status != sdk.PolicyStatusApplied {
			continue
		}
		q.Status = sdk.PolicyStatusExpired
		q.Note = joinNote(q.Note, "到期自動失效")
		if _, err := m.finish(ctx, r, q, "system", "你的權限到期了，已自動失效；還需要請重新申請"); err != nil {
			m.warn("到期的權限申請存不進去")
		}
	}
}

// handleAnswer：主人在通知上按了「核准」或「拒絕」；「晚點再看」不動作，留在待審。
func (m *Module) handleAnswer(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	if ev == nil || ev.Topic != "notice.answered" {
		return nil
	}
	id, _ := ev.Payload["id"].(string)
	text, _ := ev.Payload["text"].(string)
	if id == "" || ev.Payload["via"] == "auto" {
		return nil
	}
	approve := false
	switch strings.TrimSpace(text) {
	case "核准":
		approve = true
	case "拒絕":
	default:
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	pending, err := m.list(ctx, []sdk.PolicyStatus{sdk.PolicyStatusPending})
	if err != nil {
		return err
	}
	for _, q := range pending {
		if q.Notice != id {
			continue
		}
		// 通知只會發給主人，答案就是主人的回合。
		if _, err := m.decide(ctx, &sdk.PolicyDecideRequest{ID: q.ID, Approve: approve, Note: "從通知回答"}, "owner(notice)", false); err != nil {
			return err
		}
	}
	return nil
}
