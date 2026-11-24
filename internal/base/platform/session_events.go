package platform

import "time"

// session 照引擎回報發出的事實事件（S5；發的是底層 session，收的是業務模組 notify，兩邊都只認這裡的名字）。
// 只有事實、沒有政策：要不要推給主人由 notify 決定（法律第 5 條）。
const (
	// TopicSessionTurnFailed：引擎報 TURN_FAILED。payload
	//   {name: 對話名, agent: 秘書 id, agent_name: 她現在的角色名（讀不到就沒有）, account: 帳號名,
	//    error: sdk.EngineErrorKind（Normalize 過）, last_message: 平台原話}
	TopicSessionTurnFailed = "session.turn_failed"
	// TopicSessionEnded：引擎報 SESSION_ENDED。payload
	//   {name, agent, agent_name, account, reason: SESSION_ENDED 的 reason（帳號登出是 sdk.SessionEndReasonLoggedOut，引擎照 sdk 的詞彙填）}
	TopicSessionEnded = "session.ended"
	// TopicSessionRecovered：這個對話上一次報過 TURN_FAILED，之後又有一輪正常結束（TURN_ENDED）。payload
	//   {name, agent, account, after: 上一次失敗的種類（sdk.EngineErrorKind；auth／billing 優先記著，帳號登出算 auth）}。
	//   每次失敗之後只發一次（S5-2）；notify 用它收掉這個帳號的「要重新登入／帳單」提醒（NTF-55，S5-5）。
	TopicSessionRecovered = "session.recovered"
	// TopicSessionStuck：給這個對話的話送不進去（SES-77，DEV-162）——她待命著（不是在忙、等主人回答、額度用完、停了），
	//   經核心送的話卻排了 10 分鐘以上；核心有安全動作可做的（放掉沒動靜的那一則重送）已經先試過。payload
	//   {name, agent, agent_name: 她現在的角色名（讀不到就沒有）, count: 幾則話送不進去, minutes: 已經等了幾分鐘,
	//    dialog: 引擎說她的畫面開著確認框（bool）}。
	//   同一次卡住只發一次；通了（TopicSessionUnstuck）之後再卡才再發。
	TopicSessionStuck = "session.stuck"
	// TopicSessionUnstuck：報過 TopicSessionStuck 的對話通了（有一則話真的進去了，或排著的話清空了）。payload
	//   {name, agent, agent_stuck: 這位秘書還有別的對話卡著（bool）}。
	//   核心啟動後的第一輪另外發一次 {reset: true, agents: 現在卡著的秘書 id}：上次啟動報過的已經不記得，
	//   訂閱者把不在名單上的收掉。
	TopicSessionUnstuck = "session.unstuck"
)

// SessionStuck 是一個「話送不進去」的對話（session.Stuck 的回傳；健檢用）。
type SessionStuck struct {
	Name      string    // 對話名
	Agent     string    // 秘書 id
	AgentName string    // 她現在的角色名（讀不到就空）
	Count     int       // 幾則話送不進去
	Since     time.Time // 從什麼時候開始等
	Minutes   int       // 已經等了幾分鐘（session 的時鐘算的）
	Dialog    bool      // 引擎說她的畫面開著確認框
}
