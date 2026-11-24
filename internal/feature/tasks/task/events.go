package task

import (
	"context"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

func (m *Module) subscribe(ctx context.Context) error {
	run, cancel := context.WithCancel(ctx)
	events, err := m.d.Bus.Subscribe(run, "schedule.fire", "notice.answered", "task.changed")
	if err != nil {
		cancel()
		return err
	}
	m.cancel = cancel
	m.done = make(chan struct{})
	done := m.done
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				m.log.Warn("tasks 事件處理中斷")
			}
		}()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if ev == nil {
					continue
				}
				if err := m.handle(run, ev); err != nil && run.Err() == nil {
					m.log.Warn("tasks 事件處理失敗", "error", err)
				}
			}
		}
	}()
	return nil
}
func (m *Module) handle(ctx context.Context, ev *sdk.BusSubscribeResponse) error {
	if ev.Topic == "notice.answered" {
		return m.noticeAnswered(ctx, ev.Payload)
	}
	if ev.Topic == "task.changed" {
		m.workChanged(ctx, ev.Payload)
		return nil
	}
	if ev.Topic != "schedule.fire" || ev.Payload["id"] != "tasks.nudge" {
		return nil
	}
	now := m.d.Clock.Now()
	rows, err := m.DueNudges(ctx, now)
	if err != nil {
		return err
	}
	for _, t := range rows {
		id, err := m.sendWaiting(ctx, t)
		if err != nil {
			return err
		}
		// 催的時候換成新的那則（DEV-156 U-05、TSK-30）：送出新的之後，tasks 自己推過、這張單上還沒回的舊那則收掉，
		// 任務單、訊息頁上同一個問題只有一則。先送後收：送不出去時舊的還在，主人照樣有地方回。
		// 只在這裡收（事件迴圈，不拿著 tasks 的鎖）；剛進等你時沒有舊的（上次離開等你時 notify 已經收掉，closeLinked）。
		if err := m.retireOpen(ctx, t.ID, id, nudgedReason, staleOwn(t)); err != nil && m.log != nil {
			m.log.Warn("收掉催之前的舊通知失敗", "task", t.ID, "error", err)
		}
		if err := m.MarkNudged(ctx, t.ID, now); err != nil {
			return err
		}
	}
	return nil
}

// waitingTodo：TSK-6 推的那則的問句——交付等驗收是「請驗收」，等你決定是單上的 ask。
func waitingTodo(t Task) string {
	if t.Proposed != "" {
		return reviewAsk
	}
	return t.Ask
}

// sendWaiting 推 TSK-6 那則，回它的編號。
func (m *Module) sendWaiting(ctx context.Context, t Task) (string, error) {
	todo := waitingTodo(t)
	var options []string
	// 等你決定（等你、沒有 proposed）：問句是秘書寫的，可能是「要日式還是義式？」，預設的「好／不要」說不通；
	// 只收打字回答（看板另有「去聊」），回答照樣送回進行中（TSK-27、D-57）。
	typed := true
	if t.Proposed != "" { // 交付等驗收（等你＋proposed）：按鈕寫「驗收／退回」，不用預設的「好／不要」（TSK-25）
		options = []string{answerAccept, answerReject}
		typed = false
	}
	n, err := m.d.Notify.Send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: t.Title, Todo: todo, Options: options, Typed: typed, Task: t.ID, Agent: t.Agent})
	if err != nil || n == nil {
		return "", err
	}
	return n.ID, nil
}

// ownNotice：TSK-6 推的那則（sendWaiting）——沒綁對話、沒有來源，只收打字（等你決定）或是「請驗收」。
func ownNotice(n sdk.Notification) bool {
	return n.Room == "" && n.Source == "" && (n.Typed || n.Todo == reviewAsk)
}

// staleOwn：催的時候要換掉的舊那則——tasks 自己推的。等你決定時全部換（問句一樣的、秘書在等你期間改了問句留下的舊問句）；
// 交付等驗收時只換「請驗收」，送驗收前的舊問句照 TSK-25 留著（主人還可以回，只是回「好」不算驗收）。
// 秘書自己綁單發的（有對話、帶選項）不動。
func staleOwn(t Task) func(sdk.Notification) bool {
	return func(n sdk.Notification) bool {
		return ownNotice(n) && (t.Proposed == "" || n.Todo == reviewAsk)
	}
}

// 收掉同一句問句的舊通知時記的理由（回答 auto，看板當成「已經處理了」）。
const (
	nudgedReason   = "又提醒了一次，看新的那則"
	answeredReason = "已在另一則回答"
)

// retireOpen 收掉這張單上、還沒回、match 說要收的通知（except 那則留著）。同一個問題只該有一則在等主人：
// 不然任務單上一個問題好幾個回答框，回了一個、其他的還開著，單就一直卡在等你（noticeAnswered 看還有別則），
// 主人再回一次秘書會收到重複的回答（DEV-156 U-05、TSK-30）。走 notify 的 Retire（via=auto，不轉話、不再回頭改單）。
func (m *Module) retireOpen(ctx context.Context, taskID, except, reason string, match func(sdk.Notification) bool) error {
	open, err := m.d.Notify.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return err
	}
	var ids []string
	for _, n := range open {
		if n.Task == taskID && n.ID != except && match(n) {
			ids = append(ids, n.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return m.d.Notify.Retire(ctx, ids, reason)
}

// 交付等驗收那則通知的按鈕（TSK-25）。舊通知的預設按鈕「好／不要」照同樣的意思認。
const (
	answerAccept = "驗收"
	answerReject = "退回"
)

// reviewAnswer：主人回了「請驗收」那則通知，照回答驗收或退回（TSK-25）；不能把交付當成一般的回覆送回進行中
// （那樣 proposed 被清掉、沒驗收也沒退回意見，秘書只看到單又回到進行中）。
// 「驗收」「好」→ 通過；「退回」「不要」→ 退回（意見寫白話）；打字回答 → 那句話就是退回意見。
func (m *Module) reviewAnswer(ctx context.Context, t Task, answer, by string) error {
	switch answer {
	case "":
		return nil
	case answerAccept, "好", "通過":
		_, err := m.Accept(ctx, t.ID, by)
		return err
	case answerReject, "不要":
		answer = "主人在通知按了「退回」，沒有寫原因；請到專屬對話問清楚哪裡要改。"
	}
	_, err := m.Reject(ctx, t.ID, answer, by)
	return err
}

// noticeAnswered 是訊息→任務這個方向的連動（DEV-26）：主人在聊天、訊息頁按掉掛在「等你回覆」任務上的最後一則
// 還沒回的訊息，任務就回到進行中，不用等秘書自己記得改狀態。還有別則在等就不動；自動收掉的（via=auto，
// 任務那邊先結案或離開了）不再回頭改任務。
func (m *Module) noticeAnswered(ctx context.Context, p map[string]any) error {
	taskID, _ := p["task"].(string)
	if taskID == "" || p["via"] == string(sdk.AnswerViaAuto) {
		return nil
	}
	t, err := m.Get(ctx, taskID)
	if err != nil || t.Status != sdk.TaskStatusWaitingUser {
		return nil
	}
	if t.Proposed != "" {
		// 只認「請驗收」那則：送驗收前就開著的舊問句（等你→等你送驗收時不會被收掉）回「好」不能當成驗收（TSK-25）。
		if todo, _ := p["todo"].(string); todo != reviewAsk {
			return nil
		}
		answer, _ := p["text"].(string)
		by, _ := p["by"].(string)
		return m.reviewAnswer(ctx, t, strings.TrimSpace(answer), by)
	}
	// 主人回的就是單上那句、而且答案會轉回秘書（relay）：同一句還開著的（舊資料以前催一次就多一則、秘書另外發的同一句）
	// 是同一個問題，一起收掉，不再等它們（TSK-30）。問別的事的不動；答案不會轉回秘書的（例：沒綁對話、帶選項的那則）不收，
	// 留著只收打字的那則，秘書才讀得到主人答了什麼。
	if todo, _ := p["todo"].(string); todo != "" && todo == t.Ask && p["relay"] == true {
		id, _ := p["id"].(string)
		same := func(n sdk.Notification) bool { return n.Todo == todo }
		if err := m.retireOpen(ctx, taskID, id, answeredReason, same); err != nil && m.log != nil {
			m.log.Warn("收掉同一句問句的其他則失敗", "task", taskID, "error", err)
		}
	}
	open, err := m.d.Notify.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return nil
	}
	for _, n := range open {
		if n.Task == taskID {
			return nil
		}
	}
	doing := sdk.TaskStatusDoing
	_, err = m.Update(ctx, taskID, &sdk.TasksUpdateRequest{Status: &doing}, "system")
	return err
}

// workChanged：單的狀態變了，工作 session 跟著（P7-5、REQ-186）。2026-11-01 從 session 搬來（以前 session 自己訂閱 task.changed）。
// 聽自己發的 task.changed（不在寫入的當下直接叫）：session 開工作 session 時要回頭讀這張單，在事件迴圈裡做才不會跟寫入搶鎖。
// 從別的狀態改成 doing 才自動開；新開的單（from 是空的）不算：看板新單是待做，主人按「開始做」才走到這裡（DEV-124）；秘書自己開的本來就在她的對話裡做。
func (m *Module) workChanged(ctx context.Context, p map[string]any) {
	if m.d.Work == nil {
		return
	}
	m.d.Work.RefreshWork()
	str := func(k string) string { v, _ := p[k].(string); return v }
	if from := str("from"); str("status") == string(sdk.TaskStatusDoing) && from != "" && from != string(sdk.TaskStatusDoing) {
		m.d.Work.AutoOpenWork(ctx, str("id"), str("agent"), str("by"))
	}
}
