package notify

import (
	"slices"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 訊息分兩道（D-47、NTF-56～58）：秘書跟主人說的話、問的事（agent），系統自動的（system）。
// 規則只在這裡一份；看板照通知帶的 lane 與 badge 分頁、算數字，不自己認秘書 id、不自己算規則。

// defaultSystemAgents：settings/notify.toml 沒寫 [lanes] system_agents 時，**舊檔**（沒寫 lane 的）誰的 done／digest
// 算系統：只有 system——核心自己發通知時的署名（trust、goal 的系統訊息都用它）。各職位秘書的 id（例 admin）核心不認得
// （法律第 3 條）；哪個家要把某位秘書以前的例行報告收進系統那道，自己在 [lanes] 寫（D-47 ③）。
var defaultSystemAgents = []string{"system"}

// routineCategory：舊檔裡 system_agents 發的這幾類算例行報告；要主人同意、主動開場、待辦到期照樣是秘書那道。
func routineCategory(c sdk.Category) bool {
	return c == sdk.CategoryDone || c == sdk.CategoryDigest
}

// validLane：Send 收的 lane 只能是空的、agent 或 system。
func validLane(lane string) error {
	switch lane {
	case "", sdk.NotifyLaneAgent, sdk.NotifyLaneSystem:
		return nil
	}
	return platform.Invalid("lane", "訊息分類只能是 agent 或 system")
}

// sendLane 是 Send 時定的道（NTF-56 ①②④）：系統提醒、套件的通知一律 system；發的人明講的照它；其他 agent。
// 不看是誰發的——秘書（包括 system）做完一件要主人知道的事、Goal 達成請主人確認，都是在跟主人說話（法律第 5 條要傳）；
// 真的自動的報告（每晚測試、升級結果、每週權限摘要）由發的地方自己帶 lane=system。
func sendLane(n *sdk.Notification) string {
	if n.Source == "system.alert" || n.Source == sdk.NotifySourcePackage {
		return sdk.NotifyLaneSystem
	}
	if n.Lane == sdk.NotifyLaneAgent || n.Lane == sdk.NotifyLaneSystem {
		return n.Lane
	}
	return sdk.NotifyLaneAgent
}

// laneOf 回讀出來的這則在哪一道：有存 lane 的照它（Send 時定的）；舊檔沒寫的（D-47 以前的）先照 sendLane，
// 再加一條搬家用的規則 ③：system_agents 裡的秘書發的 done／digest 算 system。呼叫者持有 m.mu。
func (m *Module) laneOf(n *sdk.Notification) string {
	if n.Lane == "" && n.Source != "system.alert" && n.Source != sdk.NotifySourcePackage &&
		routineCategory(n.Category) && slices.Contains(m.systemAgents, n.Agent) {
		return sdk.NotifyLaneSystem
	}
	return sendLane(n)
}

// countsInBadge：這則算不算進選單「訊息」的徽章與未讀數（NTF-57）。秘書那道的未讀都算；
// 系統那道只算還要主人回答的（approve）與發的人標了 urgent 的——這兩種照偏好推手機（NTF-58），推了就要看得到。
// 討論串內提問（inline）只在那個討論串裡，不算（DEV-49 S2）。n.Lane 要先定好（decorate）。
func countsInBadge(n *sdk.Notification) bool {
	if n.Inline || n.State != sdk.NotificationStateUnread {
		return false
	}
	return n.Lane != sdk.NotifyLaneSystem || loudSystem(n)
}

// loudSystem：系統那道裡照樣推、照樣算徽章的——要主人回答的或 urgent 的。
func loudSystem(n *sdk.Notification) bool {
	return n.Urgent || n.Category == sdk.CategoryApprove
}

// quietLane：系統那道不推手機（NTF-58，法律第 5 條「自動事件不傳」），只放看板；
// 還要主人回答的（approve，例：帳號要重新登入）或發的人標了 urgent 的照偏好推。
func quietLane(n *sdk.Notification) bool {
	return n.Lane == sdk.NotifyLaneSystem && !loudSystem(n)
}

// decorate 補上讀出來才有的欄位：lane（舊檔照 laneOf 補）、badge（countsInBadge，看板照它算數字，FEN-N3）、
// delivers_answer（回答要不要等秘書讀到，NTF-59）。
// 呼叫者持有 m.mu。
func (m *Module) decorate(n *sdk.Notification) {
	n.Lane = m.laneOf(n)
	n.Badge = countsInBadge(n)
	n.DeliversAnswer = m.deliversAnswer(n)
}
