package notify

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 訊息分兩道（D-47、NTF-56）：Send 時只看來源與發的人有沒有明講，不看是哪位秘書發的。
func TestNoticeLaneRule(t *testing.T) {
	m, _, _, _ := fixture(t)
	cases := []struct {
		name string
		n    *sdk.Notification
		want string
	}{
		{"秘書的事情做完", &sdk.Notification{Category: sdk.CategoryDone, Title: "做完了", Agent: "code"}, sdk.NotifyLaneAgent},
		{"秘書要你同意", &sdk.Notification{Category: sdk.CategoryApprove, Title: "可以嗎", Todo: "回答", Agent: "life"}, sdk.NotifyLaneAgent},
		// 法律第 5 條要傳的：system 坐的 Goal 達成請主人確認、單做完等主人結，都還是秘書在說話。
		{"system 的 Goal 達成", &sdk.Notification{Category: sdk.CategoryDone, Title: "GOAL-3 達成，請確認", Agent: "system"}, sdk.NotifyLaneAgent},
		{"system 的提案摘要", &sdk.Notification{Category: sdk.CategoryDigest, Title: "規矩有提案", Todo: "去看提案", Agent: "system"}, sdk.NotifyLaneAgent},
		{"admin 沒明講也是秘書", &sdk.Notification{Category: sdk.CategoryDone, Title: "DEV-3 做完了", Agent: "admin"}, sdk.NotifyLaneAgent},
		{"腳本明講 system", &sdk.Notification{Category: sdk.CategoryDone, Title: "每晚測試沒過：abc1234（false）", Agent: "admin", Lane: sdk.NotifyLaneSystem}, sdk.NotifyLaneSystem},
		{"system 明講 agent", &sdk.Notification{Category: sdk.CategoryDone, Title: "備份清好了，跟你說一聲", Agent: "system", Lane: sdk.NotifyLaneAgent}, sdk.NotifyLaneAgent},
		{"套件的通知", &sdk.Notification{Category: sdk.CategoryApprove, Title: "有新職位可以認領", Todo: "認領", Agent: "life", Source: sdk.NotifySourcePackage}, sdk.NotifyLaneSystem},
		{"套件的通知不能改道", &sdk.Notification{Category: sdk.CategoryApprove, Title: "認領", Todo: "認領", Agent: "life", Source: sdk.NotifySourcePackage, Lane: sdk.NotifyLaneAgent}, sdk.NotifyLaneSystem},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := send(t, m, c.n)
			if out.Lane != c.want {
				t.Fatalf("lane=%q want %q", out.Lane, c.want)
			}
			if got := get(t, m, out.ID).Lane; got != c.want {
				t.Fatalf("stored lane=%q want %q", got, c.want)
			}
		})
	}
	if _, err := m.Send(ctx, &sdk.Notification{Category: sdk.CategoryDone, Title: "x", Agent: "code", Lane: "loud"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("bad lane err=%v", err)
	}
}

// 系統提醒（system.alert）一律在系統那道，但它要主人回答，所以算進徽章（NTF-56、NTF-57）。
func TestSystemAlertIsSystemLaneButCounted(t *testing.T) {
	m, _, _, _ := fixture(t)
	event(t, m, "system.alert", map[string]any{"title": "備份停了", "context": "三天沒跑", "agent": "system"})
	list, err := m.List(ctx, nil, "", false)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%v err=%v", list, err)
	}
	if list[0].Lane != sdk.NotifyLaneSystem || !list[0].Badge {
		t.Fatalf("alert lane=%q badge=%v", list[0].Lane, list[0].Badge)
	}
	if m.Unread() != 1 {
		t.Fatalf("unanswered alert must count in badge, unread=%d", m.Unread())
	}
}

// 讀出來的每則帶 delivers_answer（NTF-59）：秘書問的、綁對話的要等她讀到；系統提醒、套件的回答了就算數（綁單的見 tracksAnswer），
// 看板才不會對它們說「秘書還沒讀到…超過會再通知你重答」。這欄不存檔。
func TestDeliversAnswerOnlyForSecretaryNotices(t *testing.T) {
	m, _, _, _ := fixture(t)
	event(t, m, "system.alert", map[string]any{"title": "帳號要重新登入", "context": "登出了", "agent": "system"})
	asked := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "要不要訂位", Todo: "回答", Agent: "life"})
	room := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "對話裡問的", Todo: "回答", Agent: "code", Room: "code"})
	pkg := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "認領職位", Todo: "認領", Agent: "life", Source: sdk.NotifySourcePackage})
	list, err := m.List(ctx, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{asked.ID: true, room.ID: true, pkg.ID: false}
	for _, n := range list {
		if n.Source == "system.alert" {
			want[n.ID] = false
		}
	}
	for _, n := range list {
		if n.DeliversAnswer != want[n.ID] {
			t.Errorf("%s %q delivers_answer=%v want %v", n.ID, n.Title, n.DeliversAnswer, want[n.ID])
		}
	}
	answered := answer(t, m, asked.ID, sdk.AnswerViaButton)
	if !get(t, m, answered.ID).DeliversAnswer {
		t.Fatal("answered secretary notice lost delivers_answer")
	}
	r, err := m.d.Vault.Get(ctx, "message", asked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, stored := r.Fields["delivers_answer"]; stored {
		t.Fatal("delivers_answer must not be stored")
	}
}

// 徽章只算秘書那道的未讀，加上系統那道要主人回答的與 urgent 的（NTF-57）；每則帶的 badge 跟總數一致，Watch 送的也是同一個數。
func TestBadgeCountsAgentLaneAndPendingSystem(t *testing.T) {
	m, _, _, _ := fixture(t)
	for range 5 {
		send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "每晚測試沒過：deadbee（false）", Agent: "admin", Lane: sdk.NotifyLaneSystem})
	}
	if m.Unread() != 0 {
		t.Fatalf("routine system notices must not count, unread=%d", m.Unread())
	}
	send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "做完了", Agent: "code"})
	pending := send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "要重新登入", Todo: "去登入", Agent: "admin", Lane: sdk.NotifyLaneSystem})
	send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "部署壞了", Agent: "admin", Lane: sdk.NotifyLaneSystem, Urgent: true})
	if m.Unread() != 3 {
		t.Fatalf("unread=%d want 3 (agent + pending system + urgent system)", m.Unread())
	}
	list, err := m.List(ctx, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	badged := 0
	for _, n := range list {
		if n.Badge {
			badged++
		}
	}
	if badged != 3 {
		t.Fatalf("notices carrying badge=%d want 3", badged)
	}
	answer(t, m, pending.ID, sdk.AnswerViaButton)
	if got := get(t, m, pending.ID); got.Badge {
		t.Fatal("answered system notice still says badge")
	}
	if m.Unread() != 2 {
		t.Fatalf("answered system notice still counted, unread=%d", m.Unread())
	}
	ch, err := m.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if first := <-ch; first.Unread != 2 {
		t.Fatalf("watch unread=%d want 2", first.Unread)
	}
}

// 系統那道的例行報告不推手機、也不進摘要桶；要主人回答的與 urgent 照偏好推（NTF-58）。
// system 坐的 Goal 達成這種秘書那道的 done 照樣推（法律第 5 條）。
func TestSystemLaneDoesNotPushUnlessAnswerOrUrgent(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	setMode(t, m, sdk.CategoryDone, sdk.DeliveryModeInstant)
	out := send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "每晚測試沒過：abc1234（false）", Agent: "admin", Lane: sdk.NotifyLaneSystem})
	if len(p.payloads) != 0 {
		t.Fatalf("routine system notice pushed %d times", len(p.payloads))
	}
	for _, d := range out.Delivery {
		if d.Channel != sdk.ChannelBoard {
			t.Fatalf("delivery %v: system routine must stay on the board", d)
		}
	}
	send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "GOAL-3 達成，請確認", Agent: "system"})
	if len(p.payloads) != 1 {
		t.Fatalf("system's Goal done push=%d want 1", len(p.payloads))
	}
	send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "部署壞了", Agent: "admin", Lane: sdk.NotifyLaneSystem, Urgent: true})
	send(t, m, &sdk.Notification{Category: sdk.CategoryApprove, Title: "要重新登入", Todo: "去登入", Agent: "admin", Lane: sdk.NotifyLaneSystem})
	if len(p.payloads) != 3 {
		t.Fatalf("urgent / approve system push=%d want 3", len(p.payloads))
	}
	setMode(t, m, sdk.CategoryDigest, sdk.DeliveryModeDigest)
	send(t, m, &sdk.Notification{Category: sdk.CategoryDigest, Title: "權限申請摘要", Agent: "system", Lane: sdk.NotifyLaneSystem})
	if len(m.buckets) != 0 {
		t.Fatalf("system digest went into a digest bucket: %v", m.buckets)
	}
}

// 摘要桶裡都是可以推的，送出的「通知摘要」在秘書那道，照樣推、算徽章（NTF-58）。
func TestDigestSummaryStaysInAgentLane(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	setMode(t, m, sdk.CategoryDone, sdk.DeliveryModeDigest)
	send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "備份清好了", Agent: "system"})
	m.Tick(ctx, h.Clock.Now().Add(48*time.Hour))
	list, err := m.List(ctx, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range list {
		if n.Title == "通知摘要" {
			if n.Lane != sdk.NotifyLaneAgent || len(p.payloads) == 0 {
				t.Fatalf("summary lane=%q pushes=%d", n.Lane, len(p.payloads))
			}
			return
		}
	}
	t.Fatalf("no summary sent: %v", list)
}

// 舊檔沒有 lane：讀出來時補上。預設只有 system 的例行報告算系統；別位秘書（例 admin）以前的例行報告
// 要家自己在 settings/notify.toml [lanes] system_agents 寫，SetPrefs 不會把它洗掉（D-47 ③）。
func TestOldNoticeWithoutLaneGetsDerived(t *testing.T) {
	m, h, _, _ := fixture(t)
	now := h.Clock.Now().Format(time.RFC3339)
	put := func(id, agent, title string) {
		r := vault.Record{Kind: "message", ID: id, Fields: map[string]any{
			"schema": 1, "id": id, "created_at": now, "updated_at": now, "by": agent, "category": "done",
			"title": title, "todo": "無", "agent": agent, "delivery": []any{}, "state": "unread",
		}}
		if _, err := h.Vault.Put(ctx, r, agent, ""); err != nil {
			t.Fatal(err)
		}
	}
	put("M-900", "admin", "每晚測試沒過：1234abcd（false）")
	put("M-901", "system", "備份清好了")
	if got := get(t, m, "M-901").Lane; got != sdk.NotifyLaneSystem {
		t.Fatalf("old system routine lane=%q", got)
	}
	if got := get(t, m, "M-900").Lane; got != sdk.NotifyLaneAgent {
		t.Fatalf("core must not know admin by default, lane=%q", got)
	}
	if m.Unread() != 1 {
		t.Fatalf("unread=%d want 1 (old admin notice only)", m.Unread())
	}
	write(t, filepath.Join(h.Dir, "settings/notify.toml"), "schema = 1\npreset = \"standard\"\n[lanes]\nsystem_agents = [\"system\", \"admin\"]\n")
	reload := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if err := m.loadPrefs(); err != nil {
			t.Fatal(err)
		}
	}
	reload()
	if got := get(t, m, "M-900").Lane; got != sdk.NotifyLaneSystem {
		t.Fatalf("configured old admin notice lane=%q", got)
	}
	if m.Unread() != 0 {
		t.Fatalf("old routine notices counted, unread=%d", m.Unread())
	}
	// 新發的不吃 [lanes]：admin 沒明講就是秘書那道。
	if got := send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "DEV-3 做完了", Agent: "admin"}).Lane; got != sdk.NotifyLaneAgent {
		t.Fatalf("new admin notice lane=%q", got)
	}
	if _, err := m.SetPrefs(ctx, m.Prefs(), "owner(test)"); err != nil {
		t.Fatal(err)
	}
	reload()
	put("M-902", "admin", "每晚測試沒過：5678abcd（false）")
	if got := get(t, m, "M-902").Lane; got != sdk.NotifyLaneSystem {
		t.Fatalf("SetPrefs dropped [lanes], lane=%q", got)
	}
}
