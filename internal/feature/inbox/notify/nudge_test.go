package notify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// 不吵的通知（P7-6、D-34）的測試。時間一律用假時鐘（TST-3）；推播用假的轉接器，不碰真的推播服務。

type fakeTasks struct {
	mu     sync.Mutex
	counts map[string]int
}

func (f *fakeTasks) AddNudge(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.counts == nil {
		f.counts = map[string]int{}
	}
	f.counts[id]++
	return nil
}
func (f *fakeTasks) Get(context.Context, string) (*sdk.Task, error) {
	return nil, platform.NotFound("找不到這張單")
}
func (f *fakeTasks) count(id string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.counts[id] }

// nudgeFixture：有推播訂閱、有任務單計數器的 notify。
func nudgeFixture(t *testing.T) (*Module, *homefixture.Home, *testBus, *testPusher, *fakeTasks) {
	t.Helper()
	m, h, b, p := fixture(t)
	subscriptions(t, h)
	ft := &fakeTasks{}
	m.SetTasks(ft)
	return m, h, b, p, ft
}

func nudgeReq() *sdk.NotifyNudgeRequest {
	return &sdk.NotifyNudgeRequest{Agent: "code", Session: "code-sec-12", Task: "SEC-12", Context: "把通知做安靜", Todo: "請確認要不要上線"}
}
func nudge(t *testing.T, m *Module, req *sdk.NotifyNudgeRequest) *sdk.Nudge {
	t.Helper()
	n, err := m.Nudge(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func present(t *testing.T, m *Module, client, path, thread string) *sdk.NotifyPresenceResponse {
	t.Helper()
	out, err := m.Presence(ctx, &sdk.NotifyPresenceRequest{Client: client, Path: path, Thread: thread}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func pushed(p *testPusher) []nudgePayload {
	var out []nudgePayload
	for _, b := range p.payloads {
		var n nudgePayload
		if err := json.Unmarshal(b, &n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// Guards: REQ-223
func TestNudgeNoMessage(t *testing.T) {
	m, _, b, p, ft := nudgeFixture(t)
	before := m.Unread()
	n := nudge(t, m, nudgeReq())
	// 只推手機：沒人在看板，直接推播；不建 M-<n>（訊息列表沒有、未讀數沒變、也沒有 notice.sent）。
	if n.Surface != sdk.NudgeSurfacePush || len(p.payloads) != 1 {
		t.Fatalf("沒人在看板應該直接推播：%+v、推播 %d 次", n, len(p.payloads))
	}
	list, err := m.List(ctx, nil, "", true)
	if err != nil || len(list) != 0 || m.Unread() != before {
		t.Fatalf("提醒不該建訊息：%d 則 %v、未讀 %d→%d", len(list), err, before, m.Unread())
	}
	if len(b.published("notice.sent")) != 0 {
		t.Fatal("提醒不該發 notice.sent")
	}
	// 點下去開那個 session 的聊天頁；內容寫清楚單號、背景、要做什麼。
	got := pushed(p)[0]
	if got.URL != "/chat/code-sec-12" || n.URL != got.URL {
		t.Fatalf("推播要指到 session 的聊天頁：%+v", got)
	}
	if !strings.Contains(got.Title, "SEC-12") || !strings.Contains(got.Todo, "把通知做安靜") || !strings.Contains(got.Todo, "請確認要不要上線") {
		t.Fatalf("推播內容要有單號、標題、要做什麼：%+v", got)
	}
	if got.Nudge.Tag == "" {
		t.Fatalf("推播要帶 tag：%+v", got.Nudge)
	}
	// 任務單的 nudges ＋1；同一個 session 再提醒一次回既有的那則，不重推、不重複計數。
	if ft.count("SEC-12") != 1 {
		t.Fatalf("nudges 要 +1：%d", ft.count("SEC-12"))
	}
	again := nudge(t, m, nudgeReq())
	if again.ID != n.ID || len(p.payloads) != 1 || ft.count("SEC-12") != 1 {
		t.Fatalf("同一個 session 不重複提醒：%+v、推播 %d、計數 %d", again, len(p.payloads), ft.count("SEC-12"))
	}
	// 另一個 session：另一則提醒，網址指到它自己的聊天頁。
	other := nudgeReq()
	other.Session, other.Task = "code-sec-13", "SEC-13"
	second, err := m.Nudge(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == n.ID || second.URL != "/chat/code-sec-13" {
		t.Fatalf("另一個 session 另一則、指到它的聊天頁：%+v", second)
	}
}

// Guards: REQ-224
func TestNudgeRespectsQuiet(t *testing.T) {
	m, h, _, p, ft := nudgeFixture(t)
	// 對照組：非安靜時段照推（證明下面的「沒推」不是因為設定漏了）。
	nudge(t, m, nudgeReq())
	if len(p.payloads) != 1 {
		t.Fatalf("對照組要推播：%d", len(p.payloads))
	}
	// 安靜時段：不推、不算一次提醒、任務單不加。
	h.Clock.Advance(11 * time.Hour)
	req := nudgeReq()
	req.Session, req.Task = "code-sec-20", "SEC-20"
	n := nudge(t, m, req)
	if n.Surface != sdk.NudgeSurfaceNone || len(p.payloads) != 1 || ft.count("SEC-20") != 0 {
		t.Fatalf("安靜時段不推：%+v、推播 %d、計數 %d", n, len(p.payloads), ft.count("SEC-20"))
	}
	// 沒有 urgent 這回事：提醒的請求沒有可以繞過安靜時段的欄位。
	// 偏好把「要你同意」降到只放看板時也不推。
	h.Clock.Advance(-11 * time.Hour)
	setMode(t, m, sdk.CategoryApprove, sdk.DeliveryModeBoard)
	req2 := nudgeReq()
	req2.Session, req2.Task = "code-sec-21", "SEC-21"
	if n := nudge(t, m, req2); n.Surface != sdk.NudgeSurfaceNone || len(p.payloads) != 1 {
		t.Fatalf("偏好只放看板時不推：%+v、%d", n, len(p.payloads))
	}
	// 安靜時段內主人正在看那個討論串：頁內卡片照出（安靜時段只管推播）。
	h.Clock.Advance(11 * time.Hour)
	present(t, m, "c1", "/chat/code-sec-22", "code-sec-22")
	req3 := nudgeReq()
	req3.Session, req3.Task = "code-sec-22", "SEC-22"
	if n := nudge(t, m, req3); n.Surface != sdk.NudgeSurfaceInline || ft.count("SEC-22") != 1 {
		t.Fatalf("安靜時段也可以顯示在看的頁面：%+v、計數 %d", n, ft.count("SEC-22"))
	}
}

// Guards: REQ-225
func TestPresenceSuppressesPush(t *testing.T) {
	m, h, _, p, _ := nudgeFixture(t)
	// 只有主人能回報在場。
	if _, err := m.Presence(ctx, &sdk.NotifyPresenceRequest{Client: "c1", Path: "/"}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能回報在場：%v", err)
	}
	// 主人正在看那個討論串 → 頁內卡片，不推播。
	present(t, m, "c1", "/chat/code-sec-12", "code-sec-12")
	n := nudge(t, m, nudgeReq())
	if n.Surface != sdk.NudgeSurfaceInline || len(p.payloads) != 0 {
		t.Fatalf("在看那個討論串不推播：%+v、%d", n, len(p.payloads))
	}
	resp := present(t, m, "c1", "/chat/code-sec-12", "code-sec-12")
	if len(resp.Nudges) != 1 || resp.Nudges[0].ID != n.ID {
		t.Fatalf("看板要拿到這則提醒：%+v", resp.Nudges)
	}
	// 看那張單的頁面也算。
	m2, _, _, p2, _ := nudgeFixture(t)
	present(t, m2, "c1", "/tasks/SEC-12", "")
	if n := nudge(t, m2, nudgeReq()); n.Surface != sdk.NudgeSurfaceInline || len(p2.payloads) != 0 {
		t.Fatalf("在看那張單的頁面也是頁內卡片：%+v", n)
	}
	// 看板開著、在別頁 → 站內提示，不推播。
	m3, _, _, p3, _ := nudgeFixture(t)
	present(t, m3, "c1", "/reminders", "")
	if n := nudge(t, m3, nudgeReq()); n.Surface != sdk.NudgeSurfaceToast || len(p3.payloads) != 0 {
		t.Fatalf("在別頁是站內提示：%+v、%d", n, len(p3.payloads))
	}
	// 90 秒沒報就當不在：直接推播。
	m4, h4, _, p4, _ := nudgeFixture(t)
	present(t, m4, "c1", "/chat/code-sec-12", "code-sec-12")
	h4.Clock.Advance(PresenceTTL)
	if n := nudge(t, m4, nudgeReq()); n.Surface != sdk.NudgeSurfacePush || len(p4.payloads) != 1 {
		t.Fatalf("過期的在場不算：%+v、%d", n, len(p4.payloads))
	}
	// 分頁切到背景（away）：馬上不算在場。
	m5, _, _, p5, _ := nudgeFixture(t)
	present(t, m5, "c1", "/chat/code-sec-12", "code-sec-12")
	if _, err := m5.Presence(ctx, &sdk.NotifyPresenceRequest{Client: "c1", Away: true}, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if n := nudge(t, m5, nudgeReq()); n.Surface != sdk.NudgeSurfacePush || len(p5.payloads) != 1 {
		t.Fatalf("切到背景不算在場：%+v", n)
	}
	// 兩個裝置：手機在別頁、桌機在看那個討論串 → 以在看的為準（頁內卡片）。
	m6, _, _, p6, _ := nudgeFixture(t)
	present(t, m6, "phone", "/", "")
	present(t, m6, "desk", "/chat/code-sec-12", "code-sec-12")
	if n := nudge(t, m6, nudgeReq()); n.Surface != sdk.NudgeSurfaceInline || len(p6.payloads) != 0 {
		t.Fatalf("任一台在看就是頁內卡片：%+v", n)
	}
	_ = h
	_ = p
}

// Guards: REQ-226
func TestInlineCardEscalatesAfter2m(t *testing.T) {
	// 滿 2 分鐘沒回 → 改推播。
	m, h, _, p, _ := nudgeFixture(t)
	present(t, m, "c1", "/chat/code-sec-12", "code-sec-12")
	n := nudge(t, m, nudgeReq())
	h.Clock.Advance(InlineEscalateAfter - time.Second)
	present(t, m, "c1", "/chat/code-sec-12", "code-sec-12") // 還在看，還沒到 2 分鐘
	m.Tick(ctx, h.Clock.Now())
	if len(p.payloads) != 0 {
		t.Fatalf("2 分鐘以前不推：%d", len(p.payloads))
	}
	h.Clock.Advance(time.Second)
	present(t, m, "c1", "/chat/code-sec-12", "code-sec-12") // 就算還一直在看
	m.Tick(ctx, h.Clock.Now())
	if len(p.payloads) != 1 {
		t.Fatalf("滿 2 分鐘要改推播：%d", len(p.payloads))
	}
	if resp := present(t, m, "c1", "/chat/code-sec-12", "code-sec-12"); len(resp.Nudges) != 0 {
		t.Fatalf("改推播後頁內卡片要收掉：%+v", resp.Nudges)
	}
	m.Tick(ctx, h.Clock.Now())
	if len(p.payloads) != 1 {
		t.Fatalf("只推一次：%d", len(p.payloads))
	}
	_ = n

	// 離開頁面 → 馬上改推播（不用等 2 分鐘）。
	m2, h2, _, p2, _ := nudgeFixture(t)
	present(t, m2, "c1", "/chat/code-sec-12", "code-sec-12")
	nudge(t, m2, nudgeReq())
	h2.Clock.Advance(20 * time.Second)
	present(t, m2, "c1", "/reminders", "")
	if len(p2.payloads) != 1 {
		t.Fatalf("離開頁面要馬上改推播：%d", len(p2.payloads))
	}
	// 分頁關掉（沒再報，90 秒過期）也是離開。
	m3, h3, _, p3, _ := nudgeFixture(t)
	present(t, m3, "c1", "/chat/code-sec-12", "code-sec-12")
	nudge(t, m3, nudgeReq())
	h3.Clock.Advance(PresenceTTL)
	m3.Tick(ctx, h3.Clock.Now())
	if len(p3.payloads) != 1 {
		t.Fatalf("分頁消失要改推播：%d", len(p3.payloads))
	}
	// 升級時剛好進安靜時段：不推，也不會留著卡片。
	m4, h4, _, p4, _ := nudgeFixture(t)
	h4.Clock.Advance(10*time.Hour + 59*time.Minute) // 快要進安靜時段
	present(t, m4, "c1", "/chat/code-sec-12", "code-sec-12")
	nudge(t, m4, nudgeReq())
	h4.Clock.Advance(InlineEscalateAfter)
	present(t, m4, "c1", "/chat/code-sec-12", "code-sec-12")
	m4.Tick(ctx, h4.Clock.Now())
	if len(p4.payloads) != 0 {
		t.Fatalf("安靜時段升級也不推：%d", len(p4.payloads))
	}
	// 站內提示：看板整個沒人看了（分頁關了）才改推播；人還在看板就不因時間改推。
	m5, h5, _, p5, _ := nudgeFixture(t)
	present(t, m5, "c1", "/reminders", "")
	nudge(t, m5, nudgeReq())
	h5.Clock.Advance(60 * time.Second)
	present(t, m5, "c1", "/reminders", "")
	m5.Tick(ctx, h5.Clock.Now())
	if len(p5.payloads) != 0 {
		t.Fatalf("人還在看板，站內提示不因時間改推：%d", len(p5.payloads))
	}
	h5.Clock.Advance(PresenceTTL)
	m5.Tick(ctx, h5.Clock.Now())
	if len(p5.payloads) != 1 {
		t.Fatalf("看板沒人看了，站內提示改推播：%d", len(p5.payloads))
	}
}

// Guards: REQ-227
func TestAnswerRetractsEverywhere(t *testing.T) {
	watching := func(m *Module) <-chan *sdk.NotifyWatchResponse {
		ch, err := m.Watch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		<-ch // 一開始的未讀數
		return ch
	}
	resolvedEvent := func(t *testing.T, ch <-chan *sdk.NotifyWatchResponse) {
		t.Helper()
		for {
			select {
			case r := <-ch:
				if r.Nudge != nil && r.Nudge.Resolved {
					return
				}
			default:
				t.Fatal("看板沒有收到「已收掉」的事件")
			}
		}
	}

	// 主人按「知道了」：頁內卡片收掉；已經推播的不再補推「已處理」（DEV-123）。
	m, _, _, p, _ := nudgeFixture(t)
	ch := watching(m)
	n := nudge(t, m, nudgeReq()) // 沒人在看 → 推播
	if len(p.payloads) != 1 {
		t.Fatal("先要推出去")
	}
	if err := m.ResolveNudge(ctx, n.ID, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("只有主人能收掉：%v", err)
	}
	if err := m.ResolveNudge(ctx, n.ID, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	resolvedEvent(t, ch)
	if got := pushed(p); len(got) != 1 {
		t.Fatalf("收掉時不再推任何東西：%+v", got)
	}
	if err := m.ResolveNudge(ctx, n.ID, "owner(web)"); err != nil || len(p.payloads) != 1 {
		t.Fatalf("重複收掉是空操作：%v、%d", err, len(p.payloads))
	}

	// 單離開 waiting_user（回答、驗收、改狀態）→ 收掉；站內的卡片也一起收（頁面下一次拿到的清單是空的）。
	m2, _, _, p2, _ := nudgeFixture(t)
	present(t, m2, "c1", "/chat/code-sec-12", "code-sec-12")
	ch2 := watching(m2)
	nudge(t, m2, nudgeReq())
	event(t, m2, "task.changed", map[string]any{"id": "SEC-12", "status": "waiting_user", "from": "doing"})
	if resp := present(t, m2, "c1", "/chat/code-sec-12", "code-sec-12"); len(resp.Nudges) != 1 {
		t.Fatalf("還在等的不收：%+v", resp.Nudges)
	}
	event(t, m2, "task.changed", map[string]any{"id": "SEC-12", "status": "doing", "from": "waiting_user"})
	resolvedEvent(t, ch2)
	if resp := present(t, m2, "c1", "/chat/code-sec-12", "code-sec-12"); len(resp.Nudges) != 0 {
		t.Fatalf("單回到進行中，卡片要收：%+v", resp.Nudges)
	}
	if len(p2.payloads) != 0 {
		t.Fatal("收掉時不再推任何東西")
	}

	// 主人在那個 session 的討論串回話（任一處回答）→ 收掉，不再補推；別的 session 的回話不影響。
	m3, _, _, p3, _ := nudgeFixture(t)
	nudge(t, m3, nudgeReq())
	event(t, m3, "bus.envelope.state", map[string]any{"id": "e1", "kind": "chat", "from": "owner(web)", "to": "code-sec-99", "state": "queued"})
	if len(p3.payloads) != 1 {
		t.Fatalf("別的 session 的回話不該收掉：%d", len(p3.payloads))
	}
	event(t, m3, "bus.envelope.state", map[string]any{"id": "e2", "kind": "chat", "from": "code", "to": "code-sec-12", "state": "queued"})
	if len(p3.payloads) != 1 {
		t.Fatalf("秘書自己的訊息不算回答：%d", len(p3.payloads))
	}
	// 更早的訊息之後才走到「已回覆」等狀態：不是這次的回答。
	event(t, m3, "bus.envelope.state", map[string]any{"id": "e0", "kind": "chat", "from": "owner(web)", "to": "code-sec-12", "state": "replied"})
	if len(p3.payloads) != 1 {
		t.Fatalf("舊訊息的狀態變化不收新的提醒：%d", len(p3.payloads))
	}
	event(t, m3, "bus.envelope.state", map[string]any{"id": "e3", "kind": "chat", "from": "owner(web)", "to": "code-sec-12", "state": "queued"})
	if got3 := pushed(p3); len(got3) != 1 {
		t.Fatalf("主人回話後不再補推：%+v", got3)
	}
	// 收掉之後同一個 session 可以再被提醒（新的一輪等待）；回既有那則就不會推，所以推播數加 1 代表真的收掉了。
	if n := nudge(t, m3, nudgeReq()); n.ID == "" || len(p3.payloads) != 2 {
		t.Fatalf("新的一輪等待要能再提醒：%+v、%d", n, len(p3.payloads))
	}
}

// Guards: REQ-224
func TestNudgeNeverPushesFromDevHome(t *testing.T) {
	h := homefixture.New(t)
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(h.Dir, "home.toml"), string(data)+"\n[runtime]\ndev = true\n")
	subscriptions(t, h)
	p := newTestPusher(h.Dir)
	m := New(Deps{Vault: h.Vault, Bus: &testBus{}, Push: p, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	if n := nudge(t, m, nudgeReq()); n.Surface != sdk.NudgeSurfaceNone || len(p.payloads) != 0 {
		t.Fatalf("開發家不推播：%+v、%d", n, len(p.payloads))
	}
}

// Guards: REQ-223
func TestNudgeValidates(t *testing.T) {
	m, _, _, _, _ := nudgeFixture(t)
	for _, req := range []*sdk.NotifyNudgeRequest{{Session: "code-sec-1"}, {Agent: "code"}, nil} {
		if _, err := m.Nudge(ctx, req); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("缺欄位要被拒絕：%+v %v", req, err)
		}
	}
	for _, c := range []string{"", strings.Repeat("x", 65)} {
		if _, err := m.Presence(ctx, &sdk.NotifyPresenceRequest{Client: c, Path: "/"}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("分頁編號不合要被拒絕：%q %v", c, err)
		}
	}
	// 外部網址、不是站內路徑的 url 換成預設的聊天頁。
	n := nudge(t, m, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code-sec-5", URL: "https://evil.example/x"})
	if n.URL != "/chat/code-sec-5" {
		t.Fatalf("url 只能是站內路徑：%q", n.URL)
	}
}
