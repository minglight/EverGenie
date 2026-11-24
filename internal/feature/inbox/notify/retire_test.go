package notify

import (
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// 通知跟狀態只有一份正本（DEV-49 S2）。時間一律用假時鐘（TST-3）：「3 秒內」＝事件處理完、時鐘沒有往前超過 3 秒就已經收掉。

func registerLinked(h interface {
	RegisterSchema(vault.Spec)
}) {
	h.RegisterSchema(vault.Spec{Kind: "goal", Path: func(id string) string { return "vault/goals/" + id + ".md" }, Fields: []vault.Field{{Name: "state", Type: vault.String}, {Name: "notice", Type: vault.String}}})
	h.RegisterSchema(vault.Spec{Kind: "policy_request", Path: func(id string) string { return "vault/policy/" + id + ".md" }, Fields: []vault.Field{{Name: "status", Type: vault.String}, {Name: "notice", Type: vault.String}}})
}

func retired(t *testing.T, m *Module, id string) bool {
	t.Helper()
	open, err := m.List(ctx, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range open {
		if n.ID == id {
			return false
		}
	}
	n, err := m.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.Answer == nil || n.Answer.Text != RetireReason || n.Answer.Via != sdk.AnswerViaAuto {
		t.Fatalf("收掉要記理由「%s」：%+v", RetireReason, n.Answer)
	}
	return true
}

func handleEvent(t *testing.T, m *Module, topic string, p map[string]any) {
	t.Helper()
	if err := m.handle(ctx, &sdk.BusSubscribeResponse{Topic: topic, Payload: p}); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-374
func TestGoalAndPolicyNoticesRetireWhenSettledElsewhere(t *testing.T) {
	m, h, b, _ := fixture(t)
	registerLinked(h.Vault)
	goal := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認主人的 Goal：省電", Todo: "請確認", Agent: "work", Source: sdk.NotifySourceGoals})
	req := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "權限申請 P-1", Todo: "請審核", Agent: "system", Source: sdk.NotifySourcePolicy})
	other := send(t, m, notice())
	h.WithRecords(
		vault.Record{Kind: "goal", ID: "GOAL-1", Fields: map[string]any{"state": "draft", "notice": "[[" + goal.ID + "]]"}},
		vault.Record{Kind: "policy_request", ID: "P-1", Fields: map[string]any{"status": "pending", "notice": "[[" + req.ID + "]]"}},
	)
	before := m.Unread()
	start := h.Clock.Now()
	// 主人在 Goal 頁確認了、在權限申請頁核准了：事件一到就收掉，訊息頁、未讀一起少。
	r, _ := h.Vault.Get(ctx, "goal", "GOAL-1")
	r.Fields["state"] = "active"
	if _, err := h.Vault.Put(ctx, r, "owner(web)", r.Revision); err != nil {
		t.Fatal(err)
	}
	handleEvent(t, m, "goal.changed", map[string]any{"id": "GOAL-1", "state": "active"})
	q, _ := h.Vault.Get(ctx, "policy_request", "P-1")
	q.Fields["status"] = "applied"
	if _, err := h.Vault.Put(ctx, q, "owner(web)", q.Revision); err != nil {
		t.Fatal(err)
	}
	handleEvent(t, m, "policy.changed", map[string]any{"id": "P-1", "status": "applied"})
	if h.Clock.Now().Sub(start) > 3*time.Second {
		t.Fatal("要在 3 秒內")
	}
	if !retired(t, m, goal.ID) || !retired(t, m, req.ID) {
		t.Fatal("Goal 確認、權限申請不在等主人了，通知要收掉")
	}
	if m.Unread() != before-2 {
		t.Fatalf("未讀要一起少：%d → %d", before, m.Unread())
	}
	if retired(t, m, other.ID) {
		t.Fatal("沒綁東西的通知不能被收")
	}
	// 收掉時發的回答是 auto（goals／policy／tasks 都不把它當主人的回答）。
	answers := b.published("notice.answered")
	if len(answers) != 2 || answers[0].Payload["via"] != "auto" {
		t.Fatalf("收掉要發 via auto 的 notice.answered：%+v", answers)
	}
}

// Guards: REQ-375
func TestSweepRetiresOrphansKeepsSystemNotices(t *testing.T) {
	m, h, _, _ := fixture(t)
	registerLinked(h.Vault)
	// 事件漏掉（重開、舊資料）：Goal 早就不是草稿、權限申請早就核准了，通知還開著。
	done := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認主人的 Goal：舊的", Todo: "請確認", Agent: "work", Source: sdk.NotifySourceGoals})
	live := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認主人的 Goal：還在等", Todo: "請確認", Agent: "work", Source: sdk.NotifySourceGoals})
	gone := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "權限申請 P-9", Todo: "請審核", Agent: "system", Source: sdk.NotifySourcePolicy})
	fresh := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "權限申請 P-10", Todo: "請審核", Agent: "system", Source: sdk.NotifySourcePolicy})
	h.WithRecords(
		vault.Record{Kind: "goal", ID: "GOAL-1", Fields: map[string]any{"state": "withdrawn", "notice": "[[" + done.ID + "]]"}},
		vault.Record{Kind: "goal", ID: "GOAL-2", Fields: map[string]any{"state": "draft", "notice": "[[" + live.ID + "]]"}},
	)
	handleEvent(t, m, "system.alert", map[string]any{"title": "套件「習慣」掛了", "context": "背景"})
	plain := send(t, m, notice())
	h.Clock.Advance(2 * time.Minute) // gone 沒有人指到它、已經過了寬限；fresh 等一下再發
	fresh2 := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "權限申請 P-11", Todo: "請審核", Agent: "system", Source: sdk.NotifySourcePolicy})
	m.Tick(ctx, h.Clock.Now())
	for _, id := range []string{done.ID, gone.ID, fresh.ID} {
		if !retired(t, m, id) {
			t.Fatalf("%s 對應的事不在了，對帳要收掉", id)
		}
	}
	for _, id := range []string{live.ID, plain.ID, fresh2.ID} {
		if retired(t, m, id) {
			t.Fatalf("%s 還在等（或剛發、或沒綁東西），不能收", id)
		}
	}
	open, _ := m.List(ctx, nil, "", false)
	alerts := 0
	for _, n := range open {
		if n.Source == "system.alert" {
			alerts++
		}
	}
	if alerts != 1 {
		t.Fatal("系統級、沒綁東西的提醒不能被對帳收掉")
	}
	// 安全網每 3 秒對一次（只讀記憶體裡的索引）。
	if SweepEvery > 3*time.Second {
		t.Fatal("對帳週期不能超過 3 秒")
	}
}

// Guards: REQ-373
func TestRetireByRoomOwner(t *testing.T) {
	m, _, _, _ := fixture(t)
	old := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "寫入", Todo: "寫入", Options: []string{"允許", "拒絕"}, Room: "code", Agent: "code", By: "system"})
	answered := send(t, m, notice())
	if _, err := m.Answer(ctx, answered.ID, "好", sdk.AnswerViaButton, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if err := m.Retire(ctx, []string{old.ID, answered.ID, "M-999"}, ""); err != nil {
		t.Fatal(err)
	}
	if !retired(t, m, old.ID) {
		t.Fatal("綁對話的舊通知要收掉")
	}
	n, err := m.Get(ctx, answered.ID)
	if err != nil || n.Answer.Text != "好" {
		t.Fatalf("已經回答的不動：%+v %v", n, err)
	}
}

// Guards: REQ-374
func TestInlineAskNotCountedInMessages(t *testing.T) {
	m, _, b, _ := fixture(t)
	before := m.Unread()
	send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "SEC-1 的工作 session 要現在開嗎？", Todo: "照開還是排隊", Options: []string{"照開", "先排隊"}, Room: "code-sec-1", Agent: "code", Inline: true})
	if m.Unread() != before {
		t.Fatalf("名額詢問只在那個討論串裡，不算訊息頁的未讀：%d → %d", before, m.Unread())
	}
	ch, err := m.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first := <-ch; first.Unread != int32(before) {
		t.Fatalf("Watch 的未讀也不算：%d", first.Unread)
	}
	_ = b
}

// Guards: REQ-373
func TestConversationWaitPushesOnlyAndRetracts(t *testing.T) {
	m, h, b, p, ft := nudgeFixture(t)
	before := m.Unread()
	req := &sdk.NotifyNudgeRequest{Agent: "code", Session: "code", Key: "pending:P1", Title: "Athena 要你確認", Context: "git push", Todo: "允許或拒絕", Task: "SEC-1"}
	n := nudge(t, m, req)
	if n.Surface != sdk.NudgeSurfacePush || len(p.payloads) != 1 || n.Key != "pending:P1" {
		t.Fatalf("沒人在看就推播：%+v、%d 次", n, len(p.payloads))
	}
	list, _ := m.List(ctx, nil, "", true)
	if len(list) != 0 || m.Unread() != before || len(b.published("notice.sent")) != 0 {
		t.Fatal("綁對話的等待不建訊息")
	}
	if ft.count("SEC-1") != 0 {
		t.Fatal("對話等待不算任務單的跟進次數")
	}
	if got := pushed(p)[0]; got.URL != "/chat/code" {
		t.Fatalf("推播點下去開那個對話：%+v", got)
	}
	// 同一個 session 另一題（不同 key）是另一則；同 key 再送回同一則。
	second := nudge(t, m, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code", Key: "pending:P2", Todo: "回答這一題"})
	if second.ID == n.ID || len(p.payloads) != 2 {
		t.Fatal("不同的題目各一則")
	}
	if again := nudge(t, m, req); again.ID != n.ID || len(p.payloads) != 2 {
		t.Fatal("同 key 不重推")
	}
	// 主人在那個對話打字不算回答那一題（題目還開著）。
	handleEvent(t, m, "bus.envelope.state", map[string]any{"state": "queued", "kind": "chat", "from": "owner(web)", "to": "code"})
	if len(p.payloads) != 2 {
		t.Fatal("打字不收回對話等待")
	}
	// 那件事不在了：收回，不再補推（DEV-123）；3 秒內（沒有等時鐘）。
	start := h.Clock.Now()
	if _, err := m.Nudge(ctx, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code", Key: "pending:P1", Resolve: true}); err != nil {
		t.Fatal(err)
	}
	all := pushed(p)
	if len(all) != 2 || h.Clock.Now().Sub(start) > 3*time.Second {
		t.Fatalf("收回不再補推：%+v", all)
	}
	// 真的收掉了：同一題再問一次是新的一則，會再推。
	if again := nudge(t, m, req); again.ID == n.ID || len(p.payloads) != 3 {
		t.Fatalf("收回後同一題要能再提醒：%+v、%d", again, len(p.payloads))
	}
	if all[1].Nudge.Tag == all[0].Nudge.Tag {
		t.Fatal("不同題目的 tag 要不同，收一題不能蓋到另一題")
	}
	// 另一個對話的等待：網址指到它的聊天頁，收回也認得。
	remote, err := m.Nudge(ctx, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code-c1", Key: "pending:P3", Todo: "回答這一題"})
	if err != nil || remote.URL != "/chat/code-c1" {
		t.Fatalf("網址要指到那個對話的聊天頁：%+v %v", remote, err)
	}
	if _, err := m.Nudge(ctx, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code-c1", Key: "pending:P3", Resolve: true}); err != nil {
		t.Fatal(err)
	}
	if again := nudge(t, m, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code-c1", Key: "pending:P3", Todo: "回答這一題"}); again.ID == remote.ID {
		t.Fatal("另一個對話的也要收回")
	}
}
