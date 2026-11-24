package notify

import (
	"slices"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 給某位秘書的話送不進去（session.stuck）：通知主人一次——哪位秘書、幾則、多久、下一步（重開對話；不行就到那台 Mac 開「修復」）。
// 是要主人回的系統提醒：照偏好推到手機、不標急件；同一位秘書還開著一則就不再發，別位秘書各自一則。
func TestStuckSecretaryAlertsOwnerOncePerSecretary(t *testing.T) {
	m, h, b, p := fixture(t)
	subscriptions(t, h)
	for _, topic := range []string{platform.TopicSessionStuck, platform.TopicSessionUnstuck} {
		if !slices.Contains(b.topics, topic) {
			t.Fatalf("要訂閱 %s：%v", topic, b.topics)
		}
	}
	event(t, m, platform.TopicSessionStuck, map[string]any{"name": "code", "agent": "code", "agent_name": "雅典娜", "count": 3, "minutes": 12})
	event(t, m, platform.TopicSessionStuck, map[string]any{"name": "code-sec-1", "agent": "code", "agent_name": "雅典娜", "count": 1, "minutes": 10}) // 同一位的另一個對話
	event(t, m, platform.TopicSessionStuck, map[string]any{"name": "life", "agent": "life", "count": float64(1), "minutes": float64(10)})          // 沒帶角色名、數字經 JSON 來回
	ns, err := m.List(ctx, nil, "", false)
	if err != nil || len(ns) != 2 {
		t.Fatalf("同一位秘書只留一則、別位各一則：%v %+v", err, ns)
	}
	var code, life Notice
	for i := range ns {
		switch ns[i].Agent {
		case "code":
			code = &ns[i]
		case "life":
			life = &ns[i]
		}
	}
	if code == nil || code.Title != "有話送不進去" || code.Source != "system.alert" || code.Category != sdk.CategoryApprove || code.Lane != sdk.NotifyLaneSystem || code.Urgent ||
		!strings.HasPrefix(code.Context, "雅典娜有 3 則話送不進去，已經等了 12 分鐘。") || !strings.Contains(code.Context, "重開對話") || !strings.Contains(code.Context, platform.RescueName) {
		t.Fatalf("要寫出哪位秘書、幾則、多久、下一步：%+v", code)
	}
	if len(code.Options) != 1 || code.Options[0] != "知道了" || code.Todo != noTodo {
		t.Fatalf("只有「知道了」、不另外填請你：%+v", code)
	}
	if life == nil || !strings.HasPrefix(life.Context, "秘書有 1 則話送不進去，已經等了 10 分鐘。") {
		t.Fatalf("沒帶角色名說「秘書」，不拿職位代號頂替：%+v", life)
	}
	// 不叫主人打指令、不露對話名。
	for _, banned := range []string{"evergenie sessions", "code-sec-1", "`"} {
		if strings.Contains(code.Context, banned) {
			t.Fatalf("內文不能有「%s」：%q", banned, code.Context)
		}
	}
	if len(p.payloads) != 2 {
		t.Fatalf("要主人出手的事故照偏好推到手機（每位一次）：%d", len(p.payloads))
	}
}

// 通了（session.unstuck）那則跟著收掉；這位秘書還有別的對話卡著就先留著；主人按過「知道了」之後再卡才是新的一則。
func TestStuckAlertRetiredWhenQueueMovesAgain(t *testing.T) {
	m, _, _, _ := fixture(t)
	open := func() []Notice {
		t.Helper()
		ns, err := m.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
		if err != nil {
			t.Fatal(err)
		}
		out := []Notice{}
		for i := range ns {
			out = append(out, &ns[i])
		}
		return out
	}
	stuck := func(agent string) {
		t.Helper()
		event(t, m, platform.TopicSessionStuck, map[string]any{"name": agent, "agent": agent, "count": 1, "minutes": 10})
	}
	stuck("code")
	stuck("life")
	event(t, m, platform.TopicSessionUnstuck, map[string]any{"name": "code-sec-1", "agent": "code", "agent_stuck": true})
	if got := open(); len(got) != 2 {
		t.Fatalf("她還有別的對話卡著，先不收：%+v", got)
	}
	event(t, m, platform.TopicSessionUnstuck, map[string]any{"name": "code", "agent": "code", "agent_stuck": false})
	got := open()
	if len(got) != 1 || got[0].Agent != "life" {
		t.Fatalf("通了的那位收掉、別位的不動：%+v", got)
	}
	stuck("code") // 通了之後再卡：再通知
	if got := open(); len(got) != 2 {
		t.Fatalf("通了再卡要再通知：%+v", got)
	}
	for _, n := range open() {
		if n.Agent == "life" {
			answer(t, m, n.ID, sdk.AnswerViaButton)
		}
	}
	stuck("life")
	if got := open(); len(got) != 2 {
		t.Fatalf("主人按過「知道了」之後又收到卡住，是新的一則：%+v", got)
	}
}

// 做法要帶對路：不是她的主要對話就寫出是哪個對話（主人才不會重開錯）；引擎說畫面開著確認框就先請他去回答那個框；
// 看板還連著別的家時寫出是哪一台。
func TestStuckAlertPointsAtTheRightConversationAndFix(t *testing.T) {
	work := stuckBody(map[string]any{"name": "code-sec-1", "agent": "code", "agent_name": "雅典娜", "count": 2, "minutes": 11}, "")
	if !strings.HasPrefix(work, "雅典娜的對話「code-sec-1」有 2 則話送不進去，已經等了 11 分鐘。做法：到看板打開那個對話，按右上角「⋯」選「重開對話」") {
		t.Fatalf("不是主要對話要寫出是哪個：%q", work)
	}
	dialog := stuckBody(map[string]any{"name": "code", "agent": "code", "agent_name": "雅典娜", "count": 1, "minutes": 10, "dialog": true}, "")
	if !strings.Contains(dialog, "確認框") || !strings.Contains(dialog, "有就回答它") || strings.Index(dialog, "回答它") > strings.Index(dialog, "重開對話") {
		t.Fatalf("畫面開著確認框：先請他回答，再說重開：%q", dialog)
	}
	if got := stuckBody(map[string]any{"name": "life", "agent": "life", "agent_name": "輝夜", "count": 1, "minutes": 10}, "amy-mini"); !strings.HasPrefix(got, "amy-mini 的輝夜有 1 則話送不進去") {
		t.Fatalf("看板還連別的家要寫是哪一台：%q", got)
	}
}

// 核心重開後第一輪的名單（session.unstuck 帶 reset）：不在名單上的秘書，上次留下的那則收掉；還卡著的留著、不重發。
func TestStuckAlertsFromBeforeRestartAreRetiredUnlessStillStuck(t *testing.T) {
	m, _, _, _ := fixture(t)
	for _, agent := range []string{"code", "life"} {
		event(t, m, platform.TopicSessionStuck, map[string]any{"name": agent, "agent": agent, "count": 1, "minutes": 10})
	}
	event(t, m, platform.TopicSessionUnstuck, map[string]any{"reset": true, "agents": []any{"life"}}) // 名單經 JSON 來回
	ns, err := m.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false)
	if err != nil || len(ns) != 1 || ns[0].Agent != "life" {
		t.Fatalf("只留還卡著的那位：%v %+v", err, ns)
	}
	kept := ns[0].ID
	event(t, m, platform.TopicSessionStuck, map[string]any{"name": "life", "agent": "life", "count": 1, "minutes": 12})
	if ns, _ = m.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false); len(ns) != 1 || ns[0].ID != kept {
		t.Fatalf("還開著的那則不重發：%+v", ns)
	}
	event(t, m, platform.TopicSessionUnstuck, map[string]any{"reset": true, "agents": []string{}})
	if ns, _ = m.List(ctx, []sdk.NotificationState{sdk.NotificationStateUnread, sdk.NotificationStateRead}, "", false); len(ns) != 0 {
		t.Fatalf("都不卡了全部收掉：%+v", ns)
	}
}
