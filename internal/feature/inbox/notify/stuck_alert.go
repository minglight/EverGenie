package notify

import (
	"context"
	"fmt"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 給某位秘書的話送不進去（NTF-68，DEV-162）：session 發事實（platform.TopicSessionStuck：她待命著，話卻排了 10 分鐘以上，
// 核心有安全動作可做的已經先試過），這裡轉成一則提醒。法律第 5 條白名單「要主人介入的事故：自動處理停下來」——核心已經沒有別的
// 安全動作可做（不自動重開對話、不往對話打字），要主人出手。照系統提醒的規則送（NTF-58：要主人回的照偏好與安靜時段推，不標急件）。

// stuckTitle 只說是哪一種事；哪位秘書、幾則、多久寫在內文。同一位秘書還開著一則就不再發（看「標題＋通知的 agent」）。
const stuckTitle = "有話送不進去"

// 下一步寫成不用懂電腦也能照做的步驟（同 NTF-67）：先在看板重開那個對話；不行再到那台 Mac 開「修復」。不要他打指令。
const (
	stuckReopen = "按右上角「⋯」選「重開對話」；重開之後還是送不進去，到那台 Mac 前面，按 ⌘＋空白鍵搜尋「" + platform.RescueName + "」打開它，照畫面做。"
	stuckSteps  = "做法：到看板打開%s，" + stuckReopen
	// 引擎說她的畫面開著確認框：先去回答那個框，不用重開。
	stuckDialogSteps = "她的畫面可能停在一個要你回答的確認框。做法：到看板打開%s，看畫面上有沒有在等你選的框，有就回答它；沒有，或回答之後還是送不進去，" + stuckReopen
)

// payloadInt：事件的數字欄位（同一個行程裡是 int，經 JSON 來回是 float64）。
func payloadInt(p map[string]any, key string) int {
	switch v := p[key].(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// stuckBody：「〈哪一台 的〉〈秘書名〉有 N 則話送不進去，已經等了 M 分鐘。」＋下一步。秘書名是事件帶的現任角色名，沒帶就說「秘書」；
// 不是她的主要對話（對話名跟秘書 id 不一樣）就寫出是哪個對話，主人才不會重開錯；where 是這台的標籤（接了別的家才有）。
func stuckBody(p map[string]any, where string) string {
	who := payloadStr(p, "agent_name")
	if who == "" {
		who = "秘書"
	}
	target := "她的對話"
	if name := payloadStr(p, "name"); name != "" && name != payloadStr(p, "agent") {
		who += "的對話「" + name + "」"
		target = "那個對話"
	}
	if where != "" {
		who = where + " 的" + who
	}
	steps := stuckSteps
	if dialog, _ := p["dialog"].(bool); dialog {
		steps = stuckDialogSteps
	}
	return fmt.Sprintf("%s有 %d 則話送不進去，已經等了 %d 分鐘。", who, payloadInt(p, "count"), payloadInt(p, "minutes")) + fmt.Sprintf(steps, target)
}

// stuckAlert：同一位秘書的這則還沒處理就不再發（同一次卡住只通知一次；通了會收掉，再卡才再發）。呼叫者持有 m.mu。
func (m *Module) stuckAlert(ctx context.Context, p map[string]any) error {
	agent := payloadStr(p, "agent")
	open, err := m.list(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil {
		return err
	}
	for _, n := range open {
		if n.Source == "system.alert" && n.Title == stuckTitle && n.Agent == agent {
			return nil
		}
	}
	_, err = m.send(ctx, &sdk.Notification{Category: sdk.CategoryApprove, Title: stuckTitle, Context: stuckBody(p, m.alertScope().where), Todo: noTodo, Options: []string{"知道了"}, Agent: agent, By: "system", Source: "system.alert"}, false)
	return err
}

// stuckCleared：話送得進去了：這位秘書沒有別的對話還卡著，就把那則收掉（法律第 5 條「問題沒了，通知跟著消失」）。
// 核心啟動後的第一輪（reset）：只留名單上那幾位的，其他的收掉（上次啟動報過、這次已經不卡的）。
func (m *Module) stuckCleared(ctx context.Context, p map[string]any) error {
	if reset, _ := p["reset"].(bool); reset {
		keep := map[string]bool{}
		switch list := p["agents"].(type) {
		case []string:
			for _, id := range list {
				keep[id] = true
			}
		case []any:
			for _, v := range list {
				if id, ok := v.(string); ok {
					keep[id] = true
				}
			}
		}
		return m.retireAlerts(ctx, func(n *sdk.Notification) bool { return n.Title == stuckTitle && !keep[n.Agent] })
	}
	if still, _ := p["agent_stuck"].(bool); still {
		return nil
	}
	agent := payloadStr(p, "agent")
	return m.retireAlerts(ctx, func(n *sdk.Notification) bool { return n.Title == stuckTitle && n.Agent == agent })
}
