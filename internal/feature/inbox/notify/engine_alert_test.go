package notify

import (
	"slices"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const (
	turnFailed = platform.TopicSessionTurnFailed
	ended      = platform.TopicSessionEnded
)

// 引擎回報的錯誤只有主人親手能解的才推（法律第 5 條；S5-3）：登入失效、帳單、帳號登出。
func TestOwnerFixableEngineErrorsBecomeOneAlertPerAccount(t *testing.T) {
	m, _, b, _ := fixture(t)
	for _, topic := range []string{turnFailed, ended} {
		if !slices.Contains(b.topics, topic) {
			t.Fatalf("要訂閱 %s：%v", topic, b.topics)
		}
	}
	// 同一個帳號的兩位秘書都登入失效、其中一位又被登出：主人還沒按「知道了」之前只有一則。
	event(t, m, turnFailed, map[string]any{"name": "life", "agent": "life", "agent_name": "露娜", "account": "main", "error": "auth", "last_message": "Invalid API key"})
	event(t, m, turnFailed, map[string]any{"name": "code", "agent": "code", "account": "main", "error": "auth"})
	event(t, m, ended, map[string]any{"name": "code", "agent": "code", "account": "main", "reason": sdk.SessionEndReasonLoggedOut})
	// 另一個帳號的帳單問題是另一件事。
	event(t, m, turnFailed, map[string]any{"name": "work", "agent": "work", "account": "acme", "error": "billing", "last_message": "credit balance too low"})
	ns, err := m.List(ctx, nil, "", false)
	if err != nil || len(ns) != 2 {
		t.Fatalf("同一帳號同一原因只留一則、不同帳號各一則：%v %+v", err, ns)
	}
	var login, bill Notice
	for i := range ns {
		switch {
		case ns[i].Title == "AI 帳號要重新登入" && ns[i].Account == "main":
			login = &ns[i]
		case ns[i].Title == "AI 帳號的帳單或方案有問題" && ns[i].Account == "acme":
			bill = &ns[i]
		}
	}
	if login == nil || login.Source != "system.alert" || login.Category != sdk.CategoryApprove || login.Agent != "life" ||
		!strings.HasPrefix(login.Context, "露娜剛剛沒辦法回你，因為 AI 帳號登入過期了。") || !strings.Contains(login.Context, "重新登入") || !strings.Contains(login.Context, "登入好之前") {
		t.Fatalf("登入失效要寫清楚是哪位秘書、發生什麼、要主人做什麼：%+v", ns)
	}
	// 要做的事已經寫在內文：不另外填「請你」，推播顯示的就是內文開頭那一句（sw.js 的 bodyFor）。
	if login.Todo != noTodo {
		t.Fatalf("不另外填請你：%q", login.Todo)
	}
	// 白話、只講一遍（DEV-156 U-08）：不放平台原話（英文）、不寫內部話、不露秘書代號與對話名（哪位秘書看通知的 agent）。
	for _, banned := range []string{"Invalid API key", "這一輪", "life 的對話", "（life）", "平台說"} {
		if strings.Contains(login.Context, banned) {
			t.Fatalf("內文不能有「%s」：%q", banned, login.Context)
		}
	}
	if strings.Count(login.Context, "登入過期") != 1 {
		t.Fatalf("同一件事只講一遍：%q", login.Context)
	}
	// 事件沒帶角色名：說「秘書」，不拿職位代號頂替。
	if bill == nil || bill.Agent != "work" || !strings.HasPrefix(bill.Context, "秘書剛剛沒辦法回你，因為 AI 帳號的帳單或方案有問題。") || strings.Contains(bill.Context, "credit balance too low") {
		t.Fatalf("帳單問題：%+v", ns)
	}
	// 按了「知道了」之後再發生，才是新的一則。
	answer(t, m, login.ID, sdk.AnswerViaButton)
	event(t, m, ended, map[string]any{"name": "life", "agent": "life", "account": "main", "reason": sdk.SessionEndReasonLoggedOut})
	if ns, _ := m.List(ctx, nil, "", false); len(ns) != 3 {
		t.Fatalf("處理過後又登出要再通知：%d", len(ns))
	}
}

// 平台忙、請求被拒、說不出種類、不認得的種類，以及其他結束原因（關掉、不在了、沒填）都不推：
// 在那個對話裡顯示就好。種類先 Normalize，不認得的當 unknown。
func TestEngineErrorsOwnerCannotFixAreNotPushed(t *testing.T) {
	m, _, b, _ := fixture(t)
	for _, kind := range []string{"overloaded", "rejected", "unknown", "", "quota_exceeded", "AUTH"} {
		event(t, m, turnFailed, map[string]any{"name": "life", "agent": "life", "account": "main", "error": kind, "last_message": "x"})
	}
	for _, reason := range []string{"stopped", "gone", ""} {
		event(t, m, ended, map[string]any{"name": "life", "agent": "life", "account": "main", "reason": reason})
	}
	if ns, _ := m.List(ctx, nil, "", false); len(ns) != 0 {
		t.Fatalf("主人親手解不了的不推：%+v", ns)
	}
	if len(b.published("notice.sent")) != 0 {
		t.Fatal("不能發通知")
	}
}

// 標題只說哪一種事；內文第一句寫出是哪位秘書（法律第 5 條：誰、哪件事）。只有一個家、一個帳號時不寫台名與帳號名（雜訊）；
// 不只一個家才寫是哪一台、不只一個帳號才寫是哪個帳號。
func TestEngineAlertNamesSecretaryAndScopesMachineAccount(t *testing.T) {
	logout := map[string]any{"agent": "life", "agent_name": "露娜", "account": "main", "reason": sdk.SessionEndReasonLoggedOut}
	for _, tc := range []struct {
		scope alertScope
		body  string
	}{
		{alertScope{}, "露娜的對話停了，因為 AI 帳號登出了。要你重新登入這個帳號；登入好之前，用這個帳號的秘書都沒辦法做事。" + reloginSteps},
		{alertScope{where: "amy-mini"}, "露娜的對話停了，因為 AI 帳號登出了。要你在 amy-mini 重新登入這個帳號；登入好之前，用這個帳號的秘書都沒辦法做事。" + reloginSteps},
		{alertScope{nameAccounts: true}, "露娜的對話停了，因為 AI 帳號登出了。要你重新登入 main 帳號；登入好之前，用這個帳號的秘書都沒辦法做事。" + reloginSteps},
		{alertScope{where: "amy-mini", nameAccounts: true}, "露娜的對話停了，因為 AI 帳號登出了。要你在 amy-mini 重新登入 main 帳號；登入好之前，用這個帳號的秘書都沒辦法做事。" + reloginSteps},
	} {
		a, ok := ownerFixable(ended, logout, tc.scope)
		if !ok || a.title != "AI 帳號要重新登入" || a.account != "main" || a.body != tc.body {
			t.Fatalf("%+v：%v %+v", tc.scope, ok, a)
		}
	}
	// 登入過期、帳單：同一種開頭；沒帶帳號時就算家裡有好幾個帳號也只能說「這個帳號」；沒帶角色名說「秘書」，不露職位代號。
	a, ok := ownerFixable(turnFailed, map[string]any{"agent": "code", "error": "auth"}, alertScope{nameAccounts: true})
	if !ok || a.title != "AI 帳號要重新登入" || a.account != "" || a.body != "秘書剛剛沒辦法回你，因為 AI 帳號登入過期了。要你重新登入這個帳號；登入好之前，用這個帳號的秘書都沒辦法做事。"+reloginSteps {
		t.Fatalf("%v %+v", ok, a)
	}
	a, ok = ownerFixable(turnFailed, map[string]any{"agent": "work", "agent_name": "阿媞米斯", "account": "acme", "error": "billing"}, alertScope{where: "amy-office", nameAccounts: true})
	if !ok || a.title != "AI 帳號的帳單或方案有問題" || a.body != "阿媞米斯剛剛沒辦法回你，因為 AI 帳號的帳單或方案有問題。要你去處理 acme 帳號的帳單或方案；處理好之前，用這個帳號的秘書都沒辦法做事。"+billingSteps {
		t.Fatalf("%v %+v", ok, a)
	}
}

// 這個家的設定決定內文寫不寫台名與帳號名：只有一個家、一個帳號不寫；看板連別的家（或被別的看板連、或開發家）寫台名；兩個以上帳號寫帳號名。
func TestAlertScopeFromHomeConfig(t *testing.T) {
	m, _, _, _ := fixture(t)
	base := *m.config
	t.Cleanup(func() { *m.config = base })
	m.config.Web.Sites, m.config.Web.AllowBoards, m.config.Accounts = nil, nil, map[string]any{"spare": map[string]any{}}
	m.config.Runtime.Env = ""
	if got := m.alertScope(); got != (alertScope{}) {
		t.Fatalf("一個家、一個帳號：%+v", got)
	}
	m.config.Accounts = map[string]any{"spare": map[string]any{}, "main": map[string]any{}}
	if got := m.alertScope(); !got.nameAccounts || got.where != "" {
		t.Fatalf("兩個帳號：%+v", got)
	}
	m.config.Web.Sites = []platform.WebSite{{URL: "https://other.example"}}
	if got := m.alertScope(); got.where != m.config.Label() || got.where == "" {
		t.Fatalf("看板還連別的家：%+v（標籤 %q）", got, m.config.Label())
	}
	m.config.Web.Sites, m.config.Web.AllowBoards = nil, []string{"https://board.example"}
	if got := m.alertScope(); got.where == "" {
		t.Fatalf("別的看板連這個家：%+v", got)
	}
}

// 問題沒了，通知跟著消失（法律第 5 條；S5-5）：同一個帳號的對話又正常做完一輪（session.recovered，after＝auth 或 billing）
// 收掉這個帳號「要重新登入」與「帳單」那兩則（一輪做完表示兩樣都好了）；別的帳號好了不收；其他種類的恢復不動任何提醒；主人已經按過的不改。
func TestRecoveredRetiresThatAccountsAlertOnly(t *testing.T) {
	m, _, b, _ := fixture(t)
	if !slices.Contains(b.topics, platform.TopicSessionRecovered) {
		t.Fatalf("要訂閱 %s：%v", platform.TopicSessionRecovered, b.topics)
	}
	open := func() []string {
		t.Helper()
		ns, err := m.List(ctx, nil, "", false)
		if err != nil {
			t.Fatal(err)
		}
		var titles []string
		for _, n := range ns {
			titles = append(titles, n.Account+" "+n.Title)
		}
		return titles
	}
	recovered := func(agent, account, after string) {
		t.Helper()
		event(t, m, platform.TopicSessionRecovered, map[string]any{"name": agent, "agent": agent, "account": account, "after": after})
	}
	has := func(titles []string, want string) bool { return slices.Contains(titles, want) }
	event(t, m, turnFailed, map[string]any{"name": "life", "agent": "life", "account": "main", "error": "auth"})
	event(t, m, turnFailed, map[string]any{"name": "work", "agent": "work", "account": "acme", "error": "auth"})
	event(t, m, turnFailed, map[string]any{"name": "code", "agent": "code", "account": "main", "error": "billing"})
	event(t, m, turnFailed, map[string]any{"name": "art", "agent": "art", "account": "temp", "error": "billing"})
	var tempBill string
	if ns, _ := m.List(ctx, nil, "", false); len(ns) == 4 {
		for _, n := range ns {
			if n.Account == "temp" && n.Title == "AI 帳號的帳單或方案有問題" {
				tempBill = n.ID
			}
		}
	}
	if tempBill == "" {
		t.Fatalf("先有四則：%v", open())
	}
	// 主人先按了 temp 帳單那則「知道了」：之後 temp 恢復也不能改掉他的回答。
	answer(t, m, tempBill, sdk.AnswerViaButton)
	if got := open(); len(got) != 4 {
		t.Fatalf("按過的還在清單（已回答、沒封存）：%v", got)
	}
	// 平台忙過後好了、不認得的種類：都不收。
	recovered("life", "main", "overloaded")
	recovered("life", "main", "AUTH")
	if got := open(); len(got) != 4 {
		t.Fatalf("不相干的恢復不能收提醒：%v", got)
	}
	// 別的帳號（temp）好了：main、acme 的不動；temp 那則主人已經回答過，回答不變。
	recovered("art", "temp", "billing")
	if got := open(); len(got) != 4 {
		t.Fatalf("別的帳號恢復不能收 main、acme 的：%v", got)
	}
	if n, err := m.Get(ctx, tempBill); err != nil || n.Answer == nil || n.Answer.Via != sdk.AnswerViaButton {
		t.Fatalf("主人按過的回答不能被改：%v %+v", err, n)
	}
	// 同帳號的另一位秘書做完一輪也算帳號好了（提醒是照帳號的）：main 的兩則一起收，acme 的留著。
	recovered("code", "main", "auth")
	got := open()
	if len(got) != 2 || has(got, "main AI 帳號要重新登入") || has(got, "main AI 帳號的帳單或方案有問題") || !has(got, "acme AI 帳號要重新登入") {
		t.Fatalf("main 好了，只收它的：%v", got)
	}
	// 收掉的記成自動回答「已經不用回了」並封存（看板顯示「這件已經處理了」）。
	all, err := m.List(ctx, nil, "", true)
	if err != nil {
		t.Fatal(err)
	}
	retired := 0
	for _, n := range all {
		if n.Answer != nil && n.Answer.Via == sdk.AnswerViaAuto && n.Answer.Text == RetireReason {
			retired++
		}
	}
	if retired != 2 {
		t.Fatalf("main 的兩則要記成自動收掉：%+v", all)
	}
	// 收掉之後同帳號又登出，是新的一則。
	event(t, m, ended, map[string]any{"name": "life", "agent": "life", "account": "main", "reason": sdk.SessionEndReasonLoggedOut})
	if got := open(); len(got) != 3 || !has(got, "main AI 帳號要重新登入") {
		t.Fatalf("收掉後再登出要再通知：%v", got)
	}
}

// 升級前發的提醒把台名與帳號寫在標題：升級後那個帳號恢復，舊標題的那則也要收得掉；別的帳號的舊提醒不動。
func TestRecoveredRetiresAlertsSentBeforeUpgrade(t *testing.T) {
	m, _, _, _ := fixture(t)
	label := m.homeLabel()
	old := func(account string) string {
		if label == "" {
			return account + " 帳號要重新登入"
		}
		return label + " 的 " + account + " 帳號要重新登入"
	}
	event(t, m, "system.alert", map[string]any{"title": old("main"), "context": "舊版的內文", "agent": "life"})
	event(t, m, "system.alert", map[string]any{"title": old("acme"), "context": "舊版的內文", "agent": "work"})
	event(t, m, platform.TopicSessionRecovered, map[string]any{"name": "life", "agent": "life", "account": "main", "after": "auth"})
	ns, err := m.List(ctx, nil, "", false)
	if err != nil || len(ns) != 1 || ns[0].Title != old("acme") {
		t.Fatalf("只收 main 的舊提醒：%v %+v", err, ns)
	}
}
