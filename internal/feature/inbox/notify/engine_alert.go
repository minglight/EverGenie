package notify

import (
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 引擎回報、只有主人親手能解的錯誤（S5-3；法律第 5 條白名單「要主人介入的事故：帳號登出…」）。
// session 只把引擎事件的事實發出來（platform.TopicSessionTurnFailed／TopicSessionEnded：哪個對話、哪位秘書、哪個帳號、什麼種類），
// 要不要推給主人由這裡決定：登入失效（turn_failed 的 auth）、帳單（billing）、帳號登出（session_ended 的 logged_out）才推；
// 平台忙、請求被拒、說不出種類的，在那個對話裡顯示就好（session 記 interrupted 附原話），不推。

// 標題只說是哪一種事（DEV-156 U-08）：哪位秘書、哪台、哪個帳號寫在內文。同一個帳號的同一種事只留一則：
// 去重看「標題＋通知的 account 欄位」（NTF-43），不靠把帳號寫進標題。
const (
	reloginTitle = "AI 帳號要重新登入"
	billingTitle = "AI 帳號的帳單或方案有問題"
)

func payloadStr(p map[string]any, key string) string {
	s, _ := p[key].(string)
	return strings.TrimSpace(s)
}

// alertScope：內文要不要寫出是哪一台、哪個帳號。只有一個家、一個帳號的主人，台名與帳號名是雜訊（DEV-156 U-07）；
// 看板連著好幾個家（或這是開發家）才寫台名，這個家有兩個以上的帳號才寫帳號名。
type alertScope struct {
	where        string // 這台的標籤（platform.Home.Label()）；空＝不寫是哪一台
	nameAccounts bool   // 內文寫出帳號名
}

// alertScope 照這個家的設定決定：有 [[web.sites]]（這個看板還看別的家）、allow_boards（別的看板看這個家）或開發家＝不只一個家。
func (m *Module) alertScope() alertScope {
	if m.config == nil {
		return alertScope{}
	}
	var s alertScope
	if len(m.config.Web.Sites) > 0 || len(m.config.Web.AllowBoards) > 0 || m.config.IsDev() {
		s.where = m.config.Label()
	}
	s.nameAccounts = len(m.config.Accounts) > 1
	return s
}

// legacyTitles：升級前發的提醒把台名與帳號寫在標題（「amy-mini 的 main 帳號要重新登入」）。帳號恢復時這些舊標題也要收得掉，
// 不然升級當下還開著的那則永遠留著。where 是這台的標籤。
func legacyTitles(p map[string]any, where string) []string {
	agent, account := payloadStr(p, "agent"), payloadStr(p, "account")
	if agent == "" {
		agent = "秘書"
	}
	acct := agent + " 用的帳號"
	if account != "" {
		acct = account + " 帳號"
	}
	if where != "" {
		acct = where + " 的 " + acct
	}
	return []string{acct + "要重新登入", acct + "的帳單或方案有問題"}
}

// recoveredFromOwnerFixable：session.recovered（這個對話上一輪出錯、之後又有一輪正常做完）是不是代表那個帳號又能用了。
// after 先 Normalize：只有 auth、billing（推過提醒的那兩種）才算；算的話這個帳號「要重新登入」（含登出那則）與「帳單」兩則一起收
// （一輪正常做完，表示登入與帳單都沒問題了；法律第 5 條「問題沒了，通知跟著消失」）。其他種類本來就沒推，不收。
func recoveredFromOwnerFixable(p map[string]any) bool {
	switch sdk.EngineErrorKind(payloadStr(p, "after")).Normalize() {
	case sdk.EngineErrorKindAuth, sdk.EngineErrorKindBilling:
		return true
	}
	return false
}

// engineAlert：一則「只有主人親手能解」的提醒。account 是出事的帳號（去重與收掉都照「標題＋帳號」）。
type engineAlert struct {
	title, body, account string
}

// 下一步寫成不用懂電腦也能照做的步驟（DEV-153 B3，NTF-67）：不叫他去找電腦秘書（這個家不一定有）、不要他打指令。
// 重新登入只能在那台 Mac 前面做，入口是安裝時放好的「修復」小工具（platform.RescueName；點兩下，照畫面選重新登入）。
// 帳單只能到主人訂閱的那個 AI 的官網處理；核心不寫是哪一家（D-21）、不寫金額與方案名（會變）。
const (
	reloginSteps = "做法：到那台 Mac 前面，按 ⌘＋空白鍵搜尋「" + platform.RescueName + "」打開它，照畫面選「重新登入 AI 帳號」；登入好，回來再說一次話就會接著做。"
	billingSteps = "做法：用瀏覽器到你訂閱這個 AI 帳號的官網登入，在帳號設定裡訂閱與付款的地方，看是不是付款沒過或訂閱到期；處理好，回來再說一次話就會接著做。"
)

// ownerFixable 判斷這個引擎事件是不是只有主人親手能解的；是就回提醒。
// 標題只看原因（登入失效和登出都是「AI 帳號要重新登入」）：同一個帳號好幾位秘書一起出錯，主人還沒按「知道了」之前只會有一則。
// 內文第一句寫出是哪位秘書、發生什麼（法律第 5 條：誰、哪件事；DEV-156 U-08）：「〈秘書名〉剛剛沒辦法回你，因為 AI 帳號登入過期了。」
// 秘書名是 session 事件帶的現任角色名（agent_name）；沒帶就說「秘書」，不拿職位代號頂替。接著一句下一步；
// 不只一個家或不只一個帳號時才寫出是哪一台、哪個帳號（alertScope）。白話、同一件事只講一遍：不寫「這一輪沒做完」這種內部話、
// 不放對話名，也不放平台原話（多半是英文；原話在那個對話裡那一句的「詳細」）。推播的字就是內文開頭這一句。
func ownerFixable(topic string, p map[string]any, scope alertScope) (engineAlert, bool) {
	str := func(key string) string { return payloadStr(p, key) }
	who := str("agent_name")
	if who == "" {
		who = "秘書"
	}
	account := str("account")
	acct, at := accountPhrase(scope, account), ""
	if scope.where != "" {
		at = "在 " + scope.where + " "
	}
	relogin := "要你" + at + "重新登入" + acct + "；登入好之前，用這個帳號的秘書都沒辦法做事。" + reloginSteps
	billing := "要你去處理" + acct + "的帳單或方案；處理好之前，用這個帳號的秘書都沒辦法做事。" + billingSteps
	switch topic {
	case platform.TopicSessionTurnFailed:
		switch sdk.EngineErrorKind(str("error")).Normalize() {
		case sdk.EngineErrorKindAuth:
			return engineAlert{reloginTitle, who + "剛剛沒辦法回你，因為 AI 帳號登入過期了。" + relogin, account}, true
		case sdk.EngineErrorKindBilling:
			return engineAlert{billingTitle, who + "剛剛沒辦法回你，因為 AI 帳號的帳單或方案有問題。" + billing, account}, true
		}
	case platform.TopicSessionEnded:
		if str("reason") == sdk.SessionEndReasonLoggedOut {
			return engineAlert{reloginTitle, who + "的對話停了，因為 AI 帳號登出了。" + relogin, account}, true
		}
	}
	return engineAlert{}, false
}

// accountPhrase：內文怎麼叫出事的帳號：這個家只有一個帳號（或事件沒帶帳號）就說「這個帳號」，不只一個才寫名字。
func accountPhrase(scope alertScope, account string) string {
	if scope.nameAccounts && account != "" {
		return " " + account + " 帳號"
	}
	return "這個帳號"
}
