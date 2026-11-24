package notify

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/inbox/push"
	"github.com/minglight/evergenie/sdk"
)

var ctx = context.Background()
var updateGolden = flag.Bool("update", false, "update notification golden files")

type testBus struct {
	mu       sync.Mutex
	events   []*sdk.BusSubscribeResponse
	incoming chan *sdk.BusSubscribeResponse
	topics   []string
}

func (b *testBus) Publish(_ context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, &sdk.BusSubscribeResponse{Topic: topic, Payload: p})
	return nil
}
func (b *testBus) Subscribe(_ context.Context, topics ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	b.topics = append([]string(nil), topics...)
	if b.incoming == nil {
		b.incoming = make(chan *sdk.BusSubscribeResponse, 16)
	}
	return b.incoming, nil
}
func (b *testBus) published(topic string) []*sdk.BusSubscribeResponse {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*sdk.BusSubscribeResponse
	for _, e := range b.events {
		if e.Topic == topic {
			out = append(out, e)
		}
	}
	return out
}

// testPusher 是注入的推播通道：訂閱端點與金鑰照真的 push.Service，只有 Send 換成記錄（err 不空就回錯）。
type testPusher struct {
	*push.Service
	payloads [][]byte
	err      error
}

func newTestPusher(dir string) *testPusher { return &testPusher{Service: push.New(dir)} }
func (p *testPusher) Send(_ context.Context, _ push.Subscription, b []byte) error {
	p.payloads = append(p.payloads, append([]byte(nil), b...))
	return p.err
}
func fixture(t *testing.T) (*Module, *homefixture.Home, *testBus, *testPusher) {
	t.Helper()
	h := homefixture.New(t)
	b := &testBus{}
	p := newTestPusher(h.Dir)
	m := New(Deps{Vault: h.Vault, Bus: b, Push: p, Clock: h.Clock})
	if err := m.ConsumeNoticeSources(testSources(h.Vault)); err != nil {
		t.Fatal(err)
	}
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	return m, h, b, p
}
func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := platform.AtomicWrite(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func subscriptions(t *testing.T, h *homefixture.Home) {
	t.Helper()
	write(t, filepath.Join(h.Dir, "settings/push.toml"), "[[subscription]]\nendpoint='https://push.invalid'\np256dh='fake'\nauth='fake'\n")
}
func notice() Notice {
	return &sdk.Notification{Category: sdk.CategoryApprove, Title: "確認測試", Todo: "請回答", Agent: "code", Account: "main"}
}
func send(t *testing.T, m *Module, n Notice) Notice {
	t.Helper()
	out, err := m.Send(ctx, n)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func get(t *testing.T, m *Module, id string) Notice {
	t.Helper()
	out, err := m.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func setMode(t *testing.T, m *Module, c sdk.Category, mode sdk.DeliveryMode) {
	t.Helper()
	p := m.Prefs()
	p.Categories[string(c)] = &sdk.CategoryPref{Mode: mode, DigestHours: 3}
	if _, err := m.SetPrefs(ctx, p, "owner(test)"); err != nil {
		t.Fatal(err)
	}
}
func event(t *testing.T, m *Module, topic string, p map[string]any) {
	t.Helper()
	if err := m.handle(ctx, &sdk.BusSubscribeResponse{Topic: topic, Payload: p}); err != nil {
		t.Fatal(err)
	}
}
func answer(t *testing.T, m *Module, id string, via sdk.AnswerVia) Notice {
	t.Helper()
	n, err := m.Answer(ctx, id, "好", via, "owner(test)")
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// vaultTasks 是測試用的 tasks：單的狀態從測試家的 vault 讀（真的 tasks 業務模組也是這樣存）。
type vaultTasks struct{ v *vault.Module }

func (vaultTasks) AddNudge(context.Context, string) error { return nil }
func (t vaultTasks) Get(ctx context.Context, id string) (*sdk.Task, error) {
	r, err := t.v.Get(ctx, "task", id)
	if err != nil {
		return nil, err
	}
	status, _ := r.Fields["status"].(string)
	return &sdk.Task{ID: id, Status: sdk.TaskStatus(status)}, nil
}

func linked(t *testing.T, h *homefixture.Home, kind, id, status string) {
	t.Helper()
	h.Vault.RegisterSchema(vault.Spec{Kind: kind, Fields: []vault.Field{{Name: "status", Type: vault.String}}})
	if _, err := h.Vault.Put(ctx, vault.Record{Kind: kind, ID: id, Fields: map[string]any{"status": status}}, "system", ""); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-23
func TestSendRequiresCategory(t *testing.T) {
	m, _, _, _ := fixture(t)
	n := notice()
	n.Category = ""
	if _, err := m.Send(ctx, n); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("missing category: %v", err)
	}
	for _, c := range categories {
		n := notice()
		n.Category = sdk.Category(c)
		send(t, m, n)
	}
}

// Guards: REQ-24
func TestModesInstantDigestBoardOff(t *testing.T) {
	for _, mode := range []sdk.DeliveryMode{"instant", "digest", "board", "off"} {
		t.Run(string(mode), func(t *testing.T) {
			m, h, _, p := fixture(t)
			subscriptions(t, h)
			setMode(t, m, sdk.CategoryDone, mode)
			n := notice()
			n.Category = sdk.CategoryDone
			out := send(t, m, n)
			if out.Delivery[0].Channel != sdk.ChannelBoard {
				t.Fatal("board missing")
			}
			want := 0
			if mode == "instant" {
				want = 1
			}
			if len(p.payloads) != want {
				t.Fatalf("push count=%d want=%d", len(p.payloads), want)
			}
		})
	}
}

// Guards: REQ-25
func TestPresetsExpand(t *testing.T) {
	expected := map[string][]sdk.DeliveryMode{"quiet": {"instant", "board", "instant", "board", "off"}, "standard": {"instant", "instant", "instant", "digest", "board"}, "active": {"instant", "instant", "instant", "instant", "instant"}}
	for preset, want := range expected {
		p, err := defaults(preset)
		if err != nil {
			t.Fatal(err)
		}
		for i, c := range categories {
			if p.Categories[c].Mode != want[i] {
				t.Fatalf("%s/%s", preset, c)
			}
		}
	}
}

// Guards: REQ-26
func TestApproveCannotBeOff(t *testing.T) {
	m, h, _, _ := fixture(t)
	for _, agentOverride := range []bool{false, true} {
		p := m.Prefs()
		bad := &sdk.CategoryPref{Mode: sdk.DeliveryModeOff}
		if agentOverride {
			p.Agents = []sdk.AgentPref{{Agent: "code", Categories: map[string]*sdk.CategoryPref{"approve": bad}}}
		} else {
			p.Categories["approve"] = bad
		}
		if _, err := m.SetPrefs(ctx, p, "owner(test)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("off accepted: %v", err)
		}
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(h.Dir, "settings/notify.toml"), "[categories.approve]\nmode='off'\n")
	if err := m.Start(ctx); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("start accepted off: %v", err)
	}
}

// Guards: REQ-27
func TestCallerCannotBypassPrefs(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	setMode(t, m, sdk.CategoryApprove, sdk.DeliveryModeBoard)
	n := notice()
	n.Source = "system.alert"
	n.Delivery = []sdk.Delivery{{Channel: sdk.ChannelApp}}
	n.State = sdk.NotificationStateAnswered
	n.Urgent = true
	out := send(t, m, n)
	if out.Source != "" || out.State != sdk.NotificationStateUnread || len(out.Delivery) != 1 || len(p.payloads) != 0 {
		t.Fatal("caller bypassed preferences or internal fields")
	}
}

// Guards: REQ-29
func TestMessageFormat(t *testing.T) {
	m, _, _, _ := fixture(t)
	n := notice()
	n.Context = "測試背景"
	out := send(t, m, n)
	goldenPath := filepath.Join("testdata", "message.golden")
	if *updateGolden {
		write(t, goldenPath, out.Body+"\n")
	}
	data, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSuffix(string(data), "\n")
	if out.Body != want {
		t.Fatalf("format = %q", out.Body)
	}
	n.Title = "[code｜main] 確認測試"
	if got := send(t, m, n).Body; got != want {
		t.Fatalf("duplicate label: %q", got)
	}
}

// 通知文字的開頭：沒有帳號就寫 `[code]`，不寫成 `[code｜]`；背景、請你空的就不留空行；
// 職位和帳號都空時開頭不寫 `[]`；標題貼了 `[code] ` 也拿掉（NTF-62）。
func TestMessageFormatSkipsEmptyParts(t *testing.T) {
	if got := noticeBody("code", "", "要你同意", "確認測試", "", ""); got != "[code] 要你同意｜確認測試" {
		t.Fatalf("no account: %q", got)
	}
	if got := noticeBody("code", "main", "要你同意", "確認測試", "", "請回答"); got != "[code｜main] 要你同意｜確認測試\n請你：請回答" {
		t.Fatalf("no context: %q", got)
	}
	if got := noticeBody("", "", "摘要", "通知摘要", "", ""); got != "摘要｜通知摘要" {
		t.Fatalf("no agent: %q", got)
	}
	m, _, _, _ := fixture(t)
	n := notice()
	n.Category = sdk.CategoryDone
	n.Account = ""
	n.Todo = ""
	n.Title = "[code] 確認測試"
	out := send(t, m, n)
	if out.Body != "[code] 事情做完｜確認測試\n請你：無" || out.Title != "確認測試" {
		t.Fatalf("send: body %q title %q", out.Body, out.Title)
	}
}

// Guards: REQ-30
func TestApproveNeedsTodoAndOptions(t *testing.T) {
	m, _, _, _ := fixture(t)
	n := notice()
	n.Todo = ""
	if _, err := m.Send(ctx, n); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("missing todo: %v", err)
	}
	n = send(t, m, notice())
	if !reflect.DeepEqual(n.Options, []string{"好", "不要"}) {
		t.Fatal(n.Options)
	}
}

// 只收打字回答的通知（NTF-60、D-57）：不補預設的「好／不要」、帶來的選項也清掉；存檔讀回來 typed 還在。
func TestTypedNoticeGetsNoDefaultOptions(t *testing.T) {
	m, _, _, _ := fixture(t)
	n := notice()
	n.Typed = true
	n.Options = []string{"好"}
	got := send(t, m, n)
	if len(got.Options) != 0 || !got.Typed {
		t.Fatalf("typed 通知不能有按鈕：%v typed=%v", got.Options, got.Typed)
	}
	back, err := m.Get(ctx, got.ID)
	if err != nil || !back.Typed || len(back.Options) != 0 {
		t.Fatalf("讀回來：%v %+v", err, back)
	}
}

// Guards: REQ-30
func TestAnswerPublishesEvent(t *testing.T) {
	m, _, b, _ := fixture(t)
	n := send(t, m, notice())
	answer(t, m, n.ID, sdk.AnswerViaButton)
	answer(t, m, n.ID, sdk.AnswerViaButton)
	events := b.published("notice.answered")
	if len(events) != 1 || events[0].Payload["text"] != "好" || events[0].Payload["source"] != "" {
		t.Fatal(events)
	}
	// todo 讓 tasks 認出「請驗收」那則（TSK-25）、收掉同一句的其他則（DEV-156 U-05）；以前漏帶，通知上按「驗收」不會驗收。
	if events[0].Payload["todo"] != n.Todo || n.Todo == "" {
		t.Fatalf("notice.answered 要帶 todo：%v", events[0].Payload)
	}
}

// Guards: REQ-31
func TestImageAndURL(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	n := notice()
	n.Image = "portrait"
	n.URL = "/custom"
	out := send(t, m, n)
	var pushed sdk.Notification
	if err := json.Unmarshal(p.payloads[0], &pushed); err != nil {
		t.Fatal(err)
	}
	stored := get(t, m, out.ID)
	if pushed.Image != n.Image || pushed.URL != n.URL || stored.Image != n.Image || stored.URL != n.URL {
		t.Fatal("image/url lost")
	}
}

// 訊息上的連結按鈕與說話的人（NTF-66，DEV-115 新角色打招呼）：存檔、讀回都帶著；有 speaker 的不管偏好一律推（法律第 5 條白名單），主人設關閉就不推；
// 網址只收站內路徑，外部網址、// 開頭、正規化後會變成別的網站的、沒有字的按鈕都擋下；字和網址都空當作沒有。
func TestSpeakerNoticePushedWithSiteLocalLinkOnly(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	// 主人把「主動開場」設成只放看板也一樣推：新角色來打招呼在法律第 5 條的推播白名單（v1.12）。
	setMode(t, m, sdk.CategoryOpening, sdk.DeliveryModeBoard)
	n := &sdk.Notification{Category: sdk.CategoryOpening, Title: "露娜 來打招呼", Context: "你好", Agent: "system", Speaker: " 露娜 ",
		Image: "/avatars/persona/luna.png", Link: &sdk.NotificationLink{Label: "去角色頁看看", URL: "/personas?card=luna"}}
	out := send(t, m, n)
	stored := get(t, m, out.ID)
	if stored.Link == nil || stored.Link.Label != "去角色頁看看" || stored.Link.URL != "/personas?card=luna" || stored.Speaker != "露娜" || stored.Image != "/avatars/persona/luna.png" {
		t.Fatalf("讀回來要有按鈕與說話的人：%+v", stored)
	}
	if len(stored.Options) != 0 {
		t.Fatalf("連結按鈕不是回答選項：%v", stored.Options)
	}
	if len(p.payloads) != 1 {
		t.Fatalf("新角色來打招呼要推手機：推了 %d 次", len(p.payloads))
	}
	// 一般的 opening 照偏好（只放看板）不推（只有 speaker 的才一律推）。
	send(t, m, &sdk.Notification{Category: sdk.CategoryOpening, Title: "早安", Agent: "life"})
	if len(p.payloads) != 1 {
		t.Fatalf("一般的主動開場照偏好不推：%d", len(p.payloads))
	}
	// 主人把「主動開場」關掉：招呼也不推。
	setMode(t, m, sdk.CategoryOpening, sdk.DeliveryModeOff)
	send(t, m, &sdk.Notification{Category: sdk.CategoryOpening, Title: "露娜 來打招呼", Agent: "system", Speaker: "露娜"})
	if len(p.payloads) != 1 {
		t.Fatalf("主人關掉就不推：%d", len(p.payloads))
	}
	for _, bad := range []sdk.NotificationLink{
		{Label: "外面", URL: "https://evil.example/x"},
		{Label: "外面", URL: "//evil.example/x"},
		{Label: "外面", URL: `/\evil.example`},
		{Label: "外面", URL: "/.//evil.example/x"}, // 瀏覽器正規化後是 //evil.example
		{Label: "外面", URL: "/a/..//evil.example"},
		{Label: "外面", URL: "/a//b"},
		{Label: "", URL: "/settings/personas"},
		{Label: "沒網址", URL: ""},
	} {
		n := notice()
		n.Link = &bad
		if _, err := m.Send(ctx, n); err == nil {
			t.Errorf("%+v 要被擋", bad)
		}
	}
	n2 := notice()
	n2.Link = &sdk.NotificationLink{Label: " ", URL: " "}
	if got := send(t, m, n2); got.Link != nil {
		t.Fatalf("空的按鈕當沒有：%+v", got.Link)
	}
}

// 新角色來打招呼推出去的樣子與例外（NTF-66）：偏好是摘要也即時推、不進摘要桶；推出去帶自我介紹、她的頭像與名字；
// 安靜時段不推；開發家只放看板（正式家已經推過同一位）。
func TestSpeakerNoticePushRules(t *testing.T) {
	hello := func() *sdk.Notification {
		return &sdk.Notification{Category: sdk.CategoryOpening, Title: "露娜 來打招呼", Context: "你好，我是露娜。", Agent: "system",
			Speaker: "露娜", Image: "/avatars/persona/luna.png", Link: &sdk.NotificationLink{Label: "去角色頁看看", URL: "/personas?card=luna"}}
	}
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	setMode(t, m, sdk.CategoryOpening, sdk.DeliveryModeDigest)
	send(t, m, hello())
	if len(p.payloads) != 1 {
		t.Fatalf("偏好是摘要也要即時推：%d", len(p.payloads))
	}
	if len(m.buckets) != 0 {
		t.Fatalf("招呼不進摘要桶：%v", m.buckets)
	}
	var got sdk.Notification
	if err := json.Unmarshal(p.payloads[0], &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "露娜 來打招呼" || got.Context != "你好，我是露娜。" || got.Image != "/avatars/persona/luna.png" || got.Speaker != "露娜" {
		t.Fatalf("推出去要帶標題、自我介紹、頭像與名字：%+v", got)
	}
	h.Clock.Advance(11 * time.Hour) // 進安靜時段
	out := send(t, m, hello())
	if len(p.payloads) != 1 {
		t.Fatalf("安靜時段不推：%d", len(p.payloads))
	}
	for _, d := range out.Delivery {
		if d.Channel == sdk.ChannelApp {
			t.Fatalf("安靜時段只放看板：%+v", out.Delivery)
		}
	}

	dm, dp, _ := runtimeHome(t, `env = "dev"`)
	send(t, dm, hello())
	if len(dp.payloads) != 0 {
		t.Fatalf("開發家的招呼只放看板：推了 %d 次", len(dp.payloads))
	}
}

// Guards: REQ-32
func TestQuietHoursBoardOnly(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	h.Clock.Advance(11 * time.Hour)
	out := send(t, m, notice())
	if len(out.Delivery) != 1 || len(p.payloads) != 0 {
		t.Fatal("quiet hours pushed")
	}
}

// Guards: REQ-32
func TestUrgentException(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	h.Clock.Advance(11 * time.Hour)
	n := notice()
	n.Urgent = true
	send(t, m, n)
	if len(p.payloads) != 1 {
		t.Fatal("urgent not pushed")
	}
}

// Guards: REQ-33
func TestUnreadAndAutoAnswered(t *testing.T) {
	m, h, _, _ := fixture(t)
	linked(t, h, "task", "SEC-1", "waiting_user")
	n := notice()
	n.Task = "SEC-1"
	n = send(t, m, n)
	if m.Unread() != 1 {
		t.Fatal("unread missing")
	}
	event(t, m, "task.changed", map[string]any{"id": "SEC-1", "status": "done"})
	if m.Unread() != 0 || get(t, m, n.ID).Answer.Via != sdk.AnswerViaAuto {
		t.Fatal("not auto answered")
	}
}

// Guards: REQ-33
func TestAutoAnsweredOnlyOnLeave(t *testing.T) {
	for _, kind := range []string{"task", "reminder"} {
		t.Run(kind, func(t *testing.T) {
			m, h, _, _ := fixture(t)
			id, waiting, next := "SEC-1", "waiting_user", "doing"
			if kind == "reminder" {
				id, waiting = "R-1", "asking"
			}
			linked(t, h, kind, id, waiting)
			n := notice()
			if kind == "task" {
				n.Task = id
			} else {
				n.Reminder = id
			}
			n = send(t, m, n)
			event(t, m, kind+".changed", map[string]any{"id": id, "status": waiting, "from": next})
			if get(t, m, n.ID).Answer != nil {
				t.Fatal("answered while still waiting")
			}
			event(t, m, kind+".changed", map[string]any{"id": id, "status": next, "from": waiting})
			if get(t, m, n.ID).Answer == nil {
				t.Fatal("not answered after leaving")
			}
		})
	}
}

// Guards: REQ-34
func TestShareReturnsURL(t *testing.T) {
	m, h, _, _ := fixture(t)
	path := filepath.Join(h.Dir, "fixture.txt")
	write(t, path, "shared bytes\n")
	out, err := m.Share(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "runtime", strings.TrimPrefix(out.URL, "/")))
	if err != nil || string(data) != "shared bytes\n" {
		t.Fatalf("share mismatch: %v", err)
	}
}

// Guards: REQ-34, REQ-120
func TestShareRefusesSecrets(t *testing.T) {
	m, h, _, _ := fixture(t)
	user := t.TempDir()
	t.Setenv("HOME", user)
	secret := filepath.Join(t.TempDir(), "private-data")
	m.config.Secrets.File = secret
	denied := map[string]string{
		"secrets file":    secret,
		"settings folder": filepath.Join(h.Dir, "settings", "trust.toml"),
		"ssh":             filepath.Join(user, ".ssh", "id_ed25519"),
		"gnupg":           filepath.Join(user, ".gnupg", "key"),
		"aws":             filepath.Join(user, ".aws", "credentials"),
		"gh":              filepath.Join(user, ".config", "gh", "hosts.yml"),
		"dotenv":          filepath.Join(t.TempDir(), ".env"),
		"dotenv suffix":   filepath.Join(t.TempDir(), ".env.production"),
		"dotenv upper":    filepath.Join(t.TempDir(), ".ENV"),
	}
	for name, path := range denied {
		write(t, path, "TOKEN=secret-value\n")
		_, err := m.Share(ctx, path)
		if platform.CodeOf(err) != sdk.ErrorCodeForbidden || err.Error() != "這個檔案不能分享" || strings.Contains(err.Error(), path) {
			t.Errorf("%s: want forbidden without the path, got %v", name, err)
		}
	}
	// A symlink that looks harmless must be judged by where it really points.
	link := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.Symlink(denied["ssh"], link); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Share(ctx, link); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Errorf("symlink to a secret must be refused: %v", err)
	}
	// A hard link to the secrets file has a harmless name and path but is the same file.
	hard := filepath.Join(t.TempDir(), "notes2.txt")
	if err := os.Link(secret, hard); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Share(ctx, hard); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Errorf("hard link to the secrets file must be refused: %v", err)
	}
	// On a case-insensitive disk (macOS) a different spelling reaches the same folder.
	upper := filepath.Join(user, ".SSH", "id_ed25519")
	if _, err := os.Stat(upper); err == nil {
		if _, err := m.Share(ctx, upper); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Errorf("another spelling of ~/.ssh must be refused: %v", err)
		}
	}
	if _, err := m.Share(ctx, "notes.txt"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Errorf("a relative path must be refused: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(h.Dir, "runtime", "share"))
	if len(entries) != 0 {
		t.Errorf("nothing may be copied when refused: %v", entries)
	}
	ok := filepath.Join(t.TempDir(), "photo.png")
	write(t, ok, "png")
	if _, err := m.Share(ctx, ok); err != nil {
		t.Errorf("an ordinary file must still be shared: %v", err)
	}
}

// Guards: REQ-34
func TestSharePublicBase(t *testing.T) {
	m, _, _, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "a.txt")
	write(t, path, "x")
	out, err := m.Share(ctx, path)
	if err != nil || out.PublicBase != "" {
		t.Fatalf("no web.public_url → empty PublicBase: %+v %v", out, err)
	}
	m.config.Web.PublicURL = " https://hub.example.ts.net/ "
	out, err = m.Share(ctx, path)
	if err != nil || out.PublicBase != "https://hub.example.ts.net" || !strings.HasPrefix(out.URL, "/share/") {
		t.Fatalf("PublicBase must be the trimmed web.public_url: %+v %v", out, err)
	}
}

// Guards: REQ-34, REQ-120
func TestShareLimits(t *testing.T) {
	m, h, _, _ := fixture(t)
	t.Setenv("HOME", t.TempDir())
	if _, err := m.Share(ctx, t.TempDir()); platform.CodeOf(err) != sdk.ErrorCodeInvalid || err.Error() != "只能分享檔案" {
		t.Errorf("a folder must be refused: %v", err)
	}
	if _, err := m.Share(ctx, "/dev/null"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Errorf("a device file must be refused: %v", err)
	}
	if _, err := m.Share(ctx, filepath.Join(t.TempDir(), "missing.txt")); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Errorf("a missing file must be not_found: %v", err)
	}
	big := filepath.Join(t.TempDir(), "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxShareBytes + 1); err != nil { // sparse: Stat sees the size without 25 MiB being written
		t.Fatal(err)
	}
	f.Close()
	if _, err := m.Share(ctx, big); platform.CodeOf(err) != sdk.ErrorCodeInvalid || err.Error() != "檔案太大（上限 25 MB）" {
		t.Errorf("a file over 25 MB must be refused: %v", err)
	}
	exact := filepath.Join(t.TempDir(), "exact.bin")
	f, err = os.Create(exact)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxShareBytes); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if _, err := m.Share(ctx, exact); err != nil {
		t.Errorf("exactly 25 MB is allowed: %v", err)
	}
	// Names keep ASCII word characters and CJK, replace the rest, and stop at 80 characters.
	weird := filepath.Join(t.TempDir(), "報告 (final)#1"+strings.Repeat("x", 100)+".png")
	write(t, weird, "png")
	out, err := m.Share(ctx, weird)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimPrefix(out.URL, "/share/")
	rest := name[strings.Index(name, "-")+1:]
	if !strings.HasPrefix(rest, "報告_") || strings.ContainsAny(rest, " ()#") || len([]rune(rest)) != 80 {
		t.Errorf("sanitized name %q (%d chars)", rest, len([]rune(rest)))
	}
	if out.Mime != "" && !strings.HasPrefix(out.Mime, "image/") && out.Mime != "application/octet-stream" {
		t.Errorf("mime %q", out.Mime)
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "runtime", "share", name)); err != nil {
		t.Error(err)
	}
}

// Guards: REQ-138
func TestApproveDefaultURLOpensMessage(t *testing.T) {
	m, _, _, _ := fixture(t)
	n := send(t, m, notice())
	if n.URL != "/messages/"+n.ID {
		t.Fatal(n.URL)
	}
}

// Guards: REQ-139
func TestChatAnswerEqualsButton(t *testing.T) {
	m, _, b, _ := fixture(t)
	for _, via := range []sdk.AnswerVia{sdk.AnswerViaChat, sdk.AnswerViaButton} {
		n := send(t, m, notice())
		out := answer(t, m, n.ID, via)
		if out.State != sdk.NotificationStateAnswered {
			t.Fatal(out.State)
		}
	}
	es := b.published("notice.answered")
	for _, e := range es {
		if e.Payload["text"] != "好" || e.Payload["agent"] != "code" {
			t.Fatal(e)
		}
	}
	if len(es) != 2 {
		t.Fatal(len(es))
	}
}

// Guards: REQ-139
func TestUndeliveredAnswerRetryThenFail(t *testing.T) {
	m, h, b, _ := fixture(t)
	n := send(t, m, notice())
	answer(t, m, n.ID, sdk.AnswerViaChat)
	h.Clock.Advance(30 * time.Second)
	m.Tick(ctx, h.Clock.Now())
	if get(t, m, n.ID).State != sdk.NotificationStateAnswered {
		t.Fatal("failed too early")
	}
	if len(b.published("notice.answered")) != 1 {
		t.Fatal("notify must leave 30-second resends to session")
	}
	h.Clock.Advance(2 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if get(t, m, n.ID).State != sdk.NotificationStateFailed {
		t.Fatal("not failed at deadline")
	}
	if len(b.published("notice.sent")) != 2 {
		t.Fatal("missing re-answer notice")
	}
	m.Tick(ctx, h.Clock.Now())
	if len(b.published("notice.sent")) != 2 {
		t.Fatal("duplicate failure notice")
	}
}

// Guards: REQ-139
func TestAnswerDeliveredOnReceived(t *testing.T) {
	m, h, _, _ := fixture(t)
	n := send(t, m, notice())
	answer(t, m, n.ID, sdk.AnswerViaChat)
	event(t, m, "bus.envelope.state", map[string]any{"notice": n.ID, "state": "notified"})
	if !get(t, m, n.ID).Answer.DeliveredAt.IsZero() {
		t.Fatal("marked delivered too early")
	}
	event(t, m, "bus.envelope.state", map[string]any{"notice": n.ID, "state": "received"})
	if get(t, m, n.ID).Answer.DeliveredAt.IsZero() {
		t.Fatal("received not recorded")
	}
	h.Clock.Advance(3 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if get(t, m, n.ID).State != sdk.NotificationStateAnswered {
		t.Fatal("delivered answer failed")
	}
}

// Guards: REQ-139
func TestReminderAnswerNotTrackedForDelivery(t *testing.T) {
	m, h, b, _ := fixture(t)
	linked(t, h, "reminder", "R-1", "asking")
	n := notice()
	n.Reminder = "R-1"
	n = send(t, m, n)
	answer(t, m, n.ID, sdk.AnswerViaButton)
	h.Clock.Advance(3 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if get(t, m, n.ID).State != sdk.NotificationStateAnswered || len(b.published("notice.sent")) != 1 {
		t.Fatal("reminder answer incorrectly tracked")
	}
}

// Guards: REQ-30
func TestSystemAlertBecomesApprove(t *testing.T) {
	m, _, b, _ := fixture(t)
	event(t, m, "system.alert", map[string]any{"title": "需要處理", "context": "背景", "agent": "code"})
	ns, err := m.List(ctx, nil, "", false)
	if err != nil || len(ns) != 1 {
		t.Fatalf("%v %d", err, len(ns))
	}
	n := ns[0]
	if n.Category != sdk.CategoryApprove || n.Source != "system.alert" || n.By != "system" || n.Todo != "請看一下" || !reflect.DeepEqual(n.Options, []string{"知道了"}) {
		t.Fatal("alert conversion mismatch")
	}
	answer(t, m, n.ID, sdk.AnswerViaButton)
	if b.published("notice.answered")[0].Payload["source"] != "system.alert" {
		t.Fatal("source missing from event")
	}
}

// Guards: REQ-139
func TestAlertAnswerAfterRestartAndMarkReadNotTracked(t *testing.T) {
	m, h, b, _ := fixture(t)
	event(t, m, "system.alert", map[string]any{"title": "警示", "agent": "code"})
	ns, err := m.List(ctx, nil, "", false)
	if err != nil {
		t.Fatal(err)
	}
	id := ns[0].ID
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.MarkRead(ctx, []string{id}, "owner(test)"); err != nil {
		t.Fatal(err)
	}
	n := answer(t, m, id, sdk.AnswerViaChat)
	if n.Source != "system.alert" || m.tracksAnswer(n) {
		t.Fatal("source lost")
	}
	h.Clock.Advance(3 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if get(t, m, id).State != sdk.NotificationStateAnswered || len(b.published("notice.sent")) != 1 {
		t.Fatal("alert tracked after restart")
	}
}

// Guards: REQ-24
func TestDigestMergesOnTick(t *testing.T) {
	m, h, b, p := fixture(t)
	subscriptions(t, h)
	setMode(t, m, sdk.CategoryDone, sdk.DeliveryModeDigest)
	for _, title := range []string{"甲", "乙"} {
		n := notice()
		n.Category = sdk.CategoryDone
		n.Title = title
		send(t, m, n)
	}
	h.Clock.Advance(2 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if len(p.payloads) != 0 {
		t.Fatal("digest sent early")
	}
	h.Clock.Advance(time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if len(p.payloads) != 1 || len(b.published("notice.sent")) != 3 {
		t.Fatal("digest not merged")
	}
	var n sdk.Notification
	if err := json.Unmarshal(p.payloads[0], &n); err != nil {
		t.Fatal(err)
	}
	if n.Category != sdk.CategoryDigest || !strings.Contains(n.Context, "甲") || !strings.Contains(n.Context, "乙") {
		t.Fatal("missing titles")
	}
	m.Tick(ctx, h.Clock.Now())
	if len(p.payloads) != 1 {
		t.Fatal("digest recurred")
	}
}

// Guards: REQ-24, REQ-25
func TestPrefsRoundTripAndAgentOverride(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	write(t, filepath.Join(h.Dir, "settings/notify.toml"), "[tasks]\nstale_days=7\n[custom]\nkeep='yes'\n")
	prefs := m.Prefs()
	prefs.Tasks = nil // 沒帶 Tasks：[tasks] 原樣保留（REQ-148）
	prefs.Agents = []sdk.AgentPref{{Agent: "code", Categories: map[string]*sdk.CategoryPref{"approve": {Mode: sdk.DeliveryModeBoard}}}}
	if _, err := m.SetPrefs(ctx, prefs, "owner(test)"); err != nil {
		t.Fatal(err)
	}
	send(t, m, notice())
	if len(p.payloads) != 0 {
		t.Fatal("override ignored")
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "settings/notify.toml"))
	if err != nil || !strings.Contains(string(data), "stale_days = 7") || !strings.Contains(string(data), "keep = \"yes\"") {
		t.Fatal("other settings lost")
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	prefs.Tasks = &sdk.TaskFollowPrefs{NudgeAt: "09:00", StaleDays: 7, NudgeEveryDays: 3, NudgeMax: 3}
	if !reflect.DeepEqual(m.Prefs(), prefs) {
		t.Fatal("preferences changed on restart")
	}
}

// Guards: REQ-148
func TestPrefsTasksRoundTrip(t *testing.T) {
	m, h, _, _ := fixture(t)
	if got := m.Prefs().Tasks; got == nil || *got != (sdk.TaskFollowPrefs{NudgeAt: "09:00", StaleDays: 3, NudgeEveryDays: 3, NudgeMax: 3}) {
		t.Fatalf("defaults: %+v", got)
	}
	prefs := m.Prefs()
	prefs.Tasks = &sdk.TaskFollowPrefs{NudgeAt: "08:30", StaleDays: 5, NudgeEveryDays: 2, NudgeMax: 0}
	out, err := m.SetPrefs(ctx, prefs, "owner(test)")
	if err != nil || *out.Tasks != *prefs.Tasks {
		t.Fatal(out, err)
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "settings/notify.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`nudge_at = "08:30"`, "stale_days = 5", "nudge_every_days = 2", "nudge_max = 0"} {
		if !strings.Contains(string(data), want) {
			t.Fatal("missing", want, string(data))
		}
	}
	// 沒帶 Tasks 的請求不動 [tasks]，回傳目前的值。
	other := m.Prefs()
	other.Tasks = nil
	other.QuietHours = "22:00-07:00"
	out, err = m.SetPrefs(ctx, other, "owner(test)")
	if err != nil || out.Tasks == nil || *out.Tasks != *prefs.Tasks {
		t.Fatal(out, err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.Prefs().Tasks; got == nil || *got != *prefs.Tasks {
		t.Fatalf("after restart: %+v", got)
	}
}

// Guards: REQ-148
func TestPrefsTasksValidation(t *testing.T) {
	m, h, _, _ := fixture(t)
	for field, bad := range map[string]sdk.TaskFollowPrefs{
		"tasks.nudge_at":         {NudgeAt: "9:00", StaleDays: 3, NudgeEveryDays: 3, NudgeMax: 3},
		"tasks.stale_days":       {NudgeAt: "09:00", StaleDays: 0, NudgeEveryDays: 3, NudgeMax: 3},
		"tasks.nudge_every_days": {NudgeAt: "09:00", StaleDays: 3, NudgeEveryDays: 31, NudgeMax: 3},
		"tasks.nudge_max":        {NudgeAt: "09:00", StaleDays: 3, NudgeEveryDays: 3, NudgeMax: 11},
	} {
		prefs := m.Prefs()
		bad := bad
		prefs.Tasks = &bad
		_, err := m.SetPrefs(ctx, prefs, "owner(test)")
		var e *platform.Err
		if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !errors.As(err, &e) || e.Field != field {
			t.Fatal(field, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "settings/notify.toml")); err == nil {
		t.Fatal("rejected prefs were written")
	}
}

// Guards: REQ-33, REQ-28
func TestWatchAndMessagePersistence(t *testing.T) {
	m, h, _, _ := fixture(t)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := m.Watch(watchCtx)
	if err != nil {
		t.Fatal(err)
	}
	if (<-ch).Unread != 0 {
		t.Fatal("initial unread")
	}
	n := send(t, m, notice())
	ev := <-ch
	if ev.Unread != 1 || ev.Notification.ID != n.ID {
		t.Fatal("watch missing notification")
	}
	path := filepath.Join(h.Dir, "vault/messages", n.ID+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := vault.Decode(data)
	if err != nil || r.Body != n.Body {
		t.Fatal("notification not persisted")
	}
	cancel()
	m.Tick(ctx, h.Clock.Now())
	if _, ok := <-ch; ok {
		t.Fatal("watch not closed")
	}
}

// Guards: REQ-27
func TestNoForbiddenImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"scheduler", "tasks", "reminders", "session"} {
			forbidden := strings.Join([]string{"github.com/minglight/evergenie/internal", name}, "/")
			if strings.Contains(string(data), "\""+forbidden+"\"") {
				t.Fatalf("forbidden import %s", path)
			}
		}
	}
}

// Guards: REQ-120, REQ-28
func TestInjectedPushErrorDoesNotLeak(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	p.err = errors.New("synthetic-secret-value")
	n := send(t, m, notice())
	if n.Delivery[1].Result != "failed" {
		t.Fatal("push failure not recorded")
	}
	logs, err := filepath.Glob(filepath.Join(h.Dir, "runtime/logs/*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range logs {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "synthetic-secret-value") {
			t.Fatal("push error leaked")
		}
	}
}

// Guards: REQ-30, REQ-33
func TestSubscribedAlertAndWatch(t *testing.T) {
	m, _, b, _ := fixture(t)
	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	ch, err := m.Watch(watchCtx)
	if err != nil {
		t.Fatal(err)
	}
	<-ch
	b.incoming <- &sdk.BusSubscribeResponse{Topic: "system.alert", Payload: map[string]any{"title": "背景事件"}}
	select {
	case ev := <-ch:
		if ev.Notification == nil || ev.Notification.Source != "system.alert" {
			t.Fatal("subscription not wired")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("event handler did not respond")
	}
}

// Guards: REQ-28, REQ-33
func TestListIncludesArchivedOnlyOnRequest(t *testing.T) {
	m, h, _, _ := fixture(t)
	n := send(t, m, notice())
	answer(t, m, n.ID, sdk.AnswerViaButton)
	h.Clock.Advance(31 * 24 * time.Hour)
	if err := h.Vault.Archive(ctx, "message", n.ID, "system"); err != nil {
		t.Fatal(err)
	}
	active, err := m.List(ctx, nil, "code", false)
	if err != nil || len(active) != 0 {
		t.Fatal("archived notice in active list")
	}
	all, err := m.List(ctx, []sdk.NotificationState{sdk.NotificationStateAnswered}, "code", true)
	if err != nil || len(all) != 1 {
		t.Fatal("archive omitted")
	}
}

// Guards: REQ-78, REQ-79, REQ-139
func TestApprovalReceiptPreventsFalseDeliveryFailure(t *testing.T) {
	m, h, b, _ := fixture(t)
	subscribed := false
	for _, topic := range b.topics {
		if topic == "session.approval.resolved" {
			subscribed = true
		}
	}
	if !subscribed {
		t.Fatal("approval receipts not subscribed")
	}
	ids := []string{}
	for range 2 {
		n := notice()
		n.Room = "code"
		n = send(t, m, n)
		answer(t, m, n.ID, sdk.AnswerViaButton)
		ids = append(ids, n.ID)
	}
	event(t, m, "session.approval.resolved", map[string]any{"name": "code", "pending_id": "p1", "notices": ids})
	revisions := map[string]string{}
	for _, id := range ids {
		if get(t, m, id).Answer.DeliveredAt.IsZero() {
			t.Fatal("missing receipt", id)
		}
		r, err := h.Vault.Get(ctx, "message", id)
		if err != nil {
			t.Fatal(err)
		}
		revisions[id] = r.Revision
	}
	h.Clock.Advance(time.Minute)
	event(t, m, "session.approval.resolved", map[string]any{"name": "code", "pending_id": "p1", "notices": []any{ids[0], ids[1]}})
	for _, id := range ids {
		r, err := h.Vault.Get(ctx, "message", id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Revision != revisions[id] {
			t.Fatal("duplicate receipt rewrote notification", id)
		}
	}
	h.Clock.Advance(2 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	for _, id := range ids {
		if get(t, m, id).State != sdk.NotificationStateAnswered {
			t.Fatal("successful approval marked failed", id)
		}
	}
	if len(b.published("notice.sent")) != 2 {
		t.Fatal("successful approval requested another answer")
	}
}

// 簡單模式選預設組只改 preset：檔案裡不寫 [categories.*]，舊的逐類覆寫（含單一秘書另設）跟著清掉；
// 完整模式改一類只留下跟預設組不一樣的那一類。
// Guards: REQ-25
func TestSimplePresetWritesNoCategories(t *testing.T) {
	m, h, _, _ := fixture(t)
	path := filepath.Join(h.Dir, "settings/notify.toml")
	// 完整模式：改「摘要」與單一秘書另設。
	prefs := m.Prefs()
	prefs.Categories["digest"] = &sdk.CategoryPref{Mode: sdk.DeliveryModeBoard, DigestHours: 3}
	prefs.Agents = []sdk.AgentPref{{Agent: "code", Categories: map[string]*sdk.CategoryPref{"done": {Mode: sdk.DeliveryModeBoard, DigestHours: 3}}}}
	if _, err := m.SetPrefs(ctx, prefs, "owner(test)"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "[categories.digest]") || strings.Contains(string(data), "[categories.approve]") || strings.Contains(string(data), "[categories.done]") {
		t.Fatalf("只該留下跟預設組不一樣的分類：\n%s", data)
	}
	if !strings.Contains(string(data), "[agents.code.categories.done]") {
		t.Fatalf("單一秘書另設要留著：\n%s", data)
	}
	// 簡單模式：選「安靜」，不帶任何覆寫。
	out, err := m.SetPrefs(ctx, &sdk.NotifyPrefs{Preset: "quiet", ChannelApp: true, QuietHours: "22:00-07:00"}, "owner(test)")
	if err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "categories") || !strings.Contains(string(data), `preset = "quiet"`) {
		t.Fatalf("簡單模式不該寫 [categories.*]，也不留單一秘書另設：\n%s", data)
	}
	if len(out.Agents) != 0 || out.Categories["done"].Mode != sdk.DeliveryModeBoard || out.Categories["opening"].Mode != sdk.DeliveryModeOff {
		t.Fatalf("回傳要是「安靜」預設組的結果：%+v", out)
	}
	if len(out.Presets) != 3 || out.Presets["quiet"]["opening"].Mode != sdk.DeliveryModeOff {
		t.Fatalf("讀回來的偏好要附預設表：%+v", out.Presets)
	}
	// 重開後一樣。
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.Prefs(); got.Preset != "quiet" || got.Categories["digest"].Mode != sdk.DeliveryModeBoard || len(got.Agents) != 0 {
		t.Fatalf("重開後要是安靜預設組：%+v", got)
	}
	if got, err := m.GetPrefs(ctx); err != nil || len(got.Presets) != 3 {
		t.Fatalf("get 要附預設表：%v %+v", err, got)
	}
	if m.Prefs().Presets != nil {
		t.Fatal("預設表只在回應裡，不進記憶體裡的偏好")
	}
}

// 簡單切到完整：expand 把預設組攤成明確的 [categories.*]，之後預設表變了也不跟著動。
// Guards: REQ-25
func TestExpandOnSwitchToFull(t *testing.T) {
	m, h, _, _ := fixture(t)
	path := filepath.Join(h.Dir, "settings/notify.toml")
	if _, err := m.SetPrefs(ctx, &sdk.NotifyPrefs{Preset: "active", ChannelApp: true}, "owner(test)"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "categories") {
		t.Fatalf("簡單模式還沒展開：\n%s", data)
	}
	out, err := m.SetPrefsExpand(ctx, m.Prefs(), "owner(test)", true)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	for _, c := range categories {
		if !strings.Contains(string(data), "[categories."+c+"]") {
			t.Errorf("展開後要有 [categories.%s]：\n%s", c, data)
		}
		if out.Categories[c].Mode != sdk.DeliveryModeInstant {
			t.Errorf("%s 要是積極組的「即時」：%+v", c, out.Categories[c])
		}
	}
	// 純函式：不動傳入的、每個分類都明確。
	in := &sdk.NotifyPrefs{Preset: "quiet", ChannelApp: true}
	ex, err := ExpandPreset(in)
	if err != nil || len(ex.Categories) != len(categories) || ex.Categories["opening"].Mode != sdk.DeliveryModeOff || in.Categories != nil {
		t.Fatalf("ExpandPreset：%v %+v", err, ex)
	}
	if _, err := ExpandPreset(&sdk.NotifyPrefs{Preset: "nope"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不認得的預設組要 INVALID：%v", err)
	}
	// 重開後還是明確的值。
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := m.Prefs(); got.Categories["done"].Mode != sdk.DeliveryModeInstant || got.Categories["opening"].Mode != sdk.DeliveryModeInstant {
		t.Fatalf("重開後要是展開的值：%+v", got.Categories)
	}
}

// 單一秘書另設：「要你同意」最低只能是「只放看板」（本機核心也擋，不只前端）；別的分類可以關閉，且只影響那位秘書。
// Guards: REQ-25, REQ-26
func TestAgentOverrideRespectsApproveFloor(t *testing.T) {
	m, h, _, p := fixture(t)
	subscriptions(t, h)
	override := func(agent, category string, mode sdk.DeliveryMode) error {
		prefs := m.Prefs()
		prefs.Agents = []sdk.AgentPref{{Agent: agent, Categories: map[string]*sdk.CategoryPref{category: {Mode: mode, DigestHours: 3}}}}
		_, err := m.SetPrefs(ctx, prefs, "owner(test)")
		return err
	}
	if err := override("code", "approve", sdk.DeliveryModeOff); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("單一秘書的 approve 不能關：%v", err)
	}
	if len(m.Prefs().Agents) != 0 {
		t.Fatal("被拒絕的設定不能留下")
	}
	if err := override("code", "approve", sdk.DeliveryModeBoard); err != nil {
		t.Fatalf("approve 可以降到只放看板：%v", err)
	}
	send(t, m, notice()) // code 的 approve → 只放看板，不推手機
	if len(p.payloads) != 0 {
		t.Fatalf("code 的 approve 該只放看板：%v", p.payloads)
	}
	other := notice()
	other.Agent = "life"
	send(t, m, other) // 別的秘書照全域（即時）
	if len(p.payloads) != 1 {
		t.Fatalf("別的秘書不受影響：%d", len(p.payloads))
	}
	if err := override("code", "done", sdk.DeliveryModeOff); err != nil {
		t.Fatalf("其他分類可以關閉：%v", err)
	}
	got := m.Prefs()
	if len(got.Agents) != 1 || got.Agents[0].Categories["done"].Mode != sdk.DeliveryModeOff {
		t.Fatalf("另設要存下來：%+v", got.Agents)
	}
	// 另設的秘書沒有任何分類 → 不記。
	prefs := m.Prefs()
	prefs.Agents = []sdk.AgentPref{{Agent: "code", Categories: map[string]*sdk.CategoryPref{}}}
	if out, err := m.SetPrefs(ctx, prefs, "owner(test)"); err != nil || len(out.Agents) != 0 {
		t.Fatalf("空的另設不記：%v %+v", err, out)
	}
}

// Guards: REQ-180
func TestDevHomeNeverPushes(t *testing.T) {
	build := func(t *testing.T, dev bool) (*Module, *testPusher) {
		t.Helper()
		h := homefixture.New(t)
		if dev {
			data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
			if err != nil {
				t.Fatal(err)
			}
			write(t, filepath.Join(h.Dir, "home.toml"), string(data)+"\n[runtime]\ndev = true\n")
		}
		subscriptions(t, h)
		p := newTestPusher(h.Dir)
		m := New(Deps{Vault: h.Vault, Bus: &testBus{}, Push: p, Clock: h.Clock})
		m.SetHome(h.Dir)
		if err := m.Start(ctx); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = m.Stop(ctx) })
		return m, p
	}
	// 對照組：正式家一樣的通知照常推，證明下面的「沒推」不是因為設定漏了。
	m, p := build(t, false)
	send(t, m, notice())
	if len(p.payloads) != 1 {
		t.Fatalf("正式家應該推播：%d", len(p.payloads))
	}
	m, p = build(t, true)
	urgent := notice()
	urgent.Urgent = true
	out := send(t, m, urgent)
	send(t, m, notice())
	if len(p.payloads) != 0 {
		t.Fatalf("開發家不該推播：%d", len(p.payloads))
	}
	for _, d := range out.Delivery {
		if d.Channel == sdk.ChannelApp {
			t.Fatalf("開發家的投遞紀錄不該有 app 管道：%+v", out.Delivery)
		}
	}
	if err := m.PushTest(ctx, "https://push.invalid", "owner(test)"); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("開發家的測試推播也要擋：%v", err)
	}
	if len(p.payloads) != 0 {
		t.Fatal("測試推播也不該送出")
	}
}

// 家的 [runtime] 接在 fixture 的 home.toml 後面，啟動一個有一台訂閱裝置的 notify。
func runtimeHome(t *testing.T, runtime string) (*Module, *testPusher, *homefixture.Home) {
	t.Helper()
	h := homefixture.New(t)
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(h.Dir, "home.toml"), string(data)+"\n[runtime]\n"+runtime+"\n")
	subscriptions(t, h)
	p := newTestPusher(h.Dir)
	m := New(Deps{Vault: h.Vault, Bus: &testBus{}, Push: p, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	return m, p, h
}

// 新寫法 [runtime] sandbox = true 一樣永遠不推播（舊的 dev = true 由上一個測試守）。
func TestSandboxHomeNeverPushes(t *testing.T) {
	m, p, _ := runtimeHome(t, "sandbox = true")
	urgent := notice()
	urgent.Urgent = true
	send(t, m, urgent)
	if len(p.payloads) != 0 {
		t.Fatalf("沙盒家不該推播：%d", len(p.payloads))
	}
}

// 開發家（env = "dev"，不是沙盒）照常推播，但推出去的標題前加「開發｜」；存檔的標題不變。正式家不加。
func TestDevEnvPushTitleHasPrefix(t *testing.T) {
	title := func(b []byte) string {
		var n struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(b, &n); err != nil {
			t.Fatal(err)
		}
		return n.Title
	}
	m, p, _ := runtimeHome(t, `env = "dev"`)
	n := notice()
	out := send(t, m, n)
	if len(p.payloads) != 1 || title(p.payloads[0]) != "開發｜"+n.Title {
		t.Fatalf("開發家的推播標題要加前綴：%d %q", len(p.payloads), title(p.payloads[len(p.payloads)-1]))
	}
	if out.Title != n.Title {
		t.Fatalf("存檔的標題不變：%q", out.Title)
	}
	m, p, _ = runtimeHome(t, `env = "prod"`)
	send(t, m, n)
	if len(p.payloads) != 1 || title(p.payloads[0]) != n.Title {
		t.Fatalf("正式家不加前綴：%q", title(p.payloads[0]))
	}
}

// 推播內容帶 home＝這台的標籤（看板連多台時開到對的那台）；存檔的通知不寫 home。
func TestPushPayloadCarriesHomeLabel(t *testing.T) {
	m, p, _ := runtimeHome(t, "owner = \"amy\"\nmachine = \"mini\"")
	out := send(t, m, notice())
	var pushed sdk.Notification
	if len(p.payloads) != 1 || json.Unmarshal(p.payloads[0], &pushed) != nil || pushed.Home != "amy-mini" {
		t.Fatalf("推播要帶 home = amy-mini：%d %s", len(p.payloads), p.payloads)
	}
	if out.Home != "" || get(t, m, out.ID).Home != "" {
		t.Fatalf("存檔的通知不寫 home：%q", out.Home)
	}
}

// Guards: REQ-230, REQ-263
func TestArchiveIsOwnerOnlyAndDeletesEverything(t *testing.T) {
	m, _, _, _ := fixture(t)
	done := send(t, m, &sdk.Notification{Category: sdk.CategoryDone, Title: "做完了", Agent: "code", Account: "main"})
	pending := send(t, m, notice())
	answered := send(t, m, notice())
	answer(t, m, answered.ID, sdk.AnswerViaButton)

	// 秘書（不是 owner(…)）不能刪，什麼都不動。
	if _, _, err := m.Archive(ctx, []string{done.ID}, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書刪訊息應該被擋，得到 %v", err)
	}
	// 主人說刪就刪：還沒回答的「要你同意」也一樣（作者 10-28）。
	archived, skipped, err := m.Archive(ctx, []string{done.ID, pending.ID, answered.ID, "M-999"}, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if len(archived) != 3 || len(skipped) != 0 {
		t.Fatalf("archived = %v skipped = %v", archived, skipped)
	}
	active, err := m.List(ctx, nil, "", false)
	if err != nil || len(active) != 0 {
		t.Fatalf("封存後清單應該是空的，得到 %v", active)
	}
	if m.Unread() != 0 {
		t.Fatalf("未讀數 = %d", m.Unread())
	}
	// 再刪一次已經封存的：略過，不算錯。
	if archived, _, err := m.Archive(ctx, []string{done.ID}, "owner(web)"); err != nil || len(archived) != 0 {
		t.Fatalf("重刪：%v %v", archived, err)
	}
}

// Guards: REQ-139
func TestModuleOwnedAnswersAreNotTracked(t *testing.T) {
	m, h, b, _ := fixture(t)
	for _, src := range []string{sdk.NotifySourceGoals, sdk.NotifySourcePolicy} {
		n := notice()
		n.Source = src
		out := send(t, m, n)
		if out.Source != src {
			t.Fatalf("goals／policy 的 source 要留著，得到 %q", out.Source)
		}
		answered := answer(t, m, out.ID, sdk.AnswerViaButton)
		if m.tracksAnswer(answered) {
			t.Fatalf("%s 的回答由模組自己處理，不追蹤送達", src)
		}
	}
	// 過了 2 小時也不會變成「回答尚未送達」再發一則。
	h.Clock.Advance(3 * time.Hour)
	m.Tick(ctx, h.Clock.Now())
	if sent := len(b.published("notice.sent")); sent != 2 {
		t.Fatalf("模組自己處理的回答被當成沒送達：notice.sent = %d", sent)
	}
	for _, p := range b.published("notice.answered") {
		if p.Payload["source"] == "" {
			t.Fatal("notice.answered 要帶 source，session 才知道不用轉送")
		}
	}
}

// Guards: REQ-198

// Guards: REQ-197

// Guards: REQ-220
func TestInlineNoticeNeverPushes(t *testing.T) {
	h := homefixture.New(t)
	subscriptions(t, h)
	p := newTestPusher(h.Dir)
	m := New(Deps{Vault: h.Vault, Bus: &testBus{}, Push: p, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	// 對照組：同樣的通知不帶 Inline 照常推播。
	send(t, m, notice())
	if len(p.payloads) != 1 {
		t.Fatalf("對照組要推播：%d", len(p.payloads))
	}
	// 討論串內提問（工作 session 超過上限，D-33）：進看板、帶著 room 與 inline，但不推播（urgent 也一樣）。
	ask := notice()
	ask.Room, ask.Inline, ask.Urgent = "code-sec-3", true, true
	out := send(t, m, ask)
	if len(p.payloads) != 1 {
		t.Fatalf("討論串內提問不推播：%d", len(p.payloads))
	}
	for _, d := range out.Delivery {
		if d.Channel == sdk.ChannelApp {
			t.Fatalf("投遞紀錄不該有 app：%+v", out.Delivery)
		}
	}
	got, err := m.Get(ctx, out.ID)
	if err != nil || !got.Inline || got.Room != "code-sec-3" {
		t.Fatalf("inline 要存進通知檔：%+v %v", got, err)
	}
}

// Guards: REQ-267
func TestSystemAlertSentOncePerOpenTitle(t *testing.T) {
	m, _, _, _ := fixture(t)
	for i := 0; i < 3; i++ {
		event(t, m, "system.alert", map[string]any{"title": "套件「習慣」掛了", "context": "背景"})
	}
	event(t, m, "system.alert", map[string]any{"title": "套件「衣櫃」掛了", "context": "背景"})
	ns, err := m.List(ctx, nil, "", false)
	if err != nil || len(ns) != 2 {
		t.Fatalf("同一件事沒處理前只留一則：%v %d", err, len(ns))
	}
	// 回答（知道了）之後再發生，才是新的一則。
	for _, n := range ns {
		if n.Title == "套件「習慣」掛了" {
			answer(t, m, n.ID, sdk.AnswerViaButton)
		}
	}
	event(t, m, "system.alert", map[string]any{"title": "套件「習慣」掛了", "context": "背景"})
	if ns, _ := m.List(ctx, nil, "", false); len(ns) != 3 {
		t.Fatalf("處理過後再發生要再通知：%d", len(ns))
	}
}

// 舊資料帶已拿掉的 site 欄位（法律第 7 條之前寫的）也讀得到、回得了。
// Guards: REQ-267
func TestMigratedNoticeWithoutDeliveryCanBeAnswered(t *testing.T) {
	m, h, _, _ := fixture(t)
	now := h.Clock.Now().Format(time.RFC3339)
	r := vault.Record{Kind: "message", ID: "M-77", Fields: map[string]any{
		"schema": 1, "id": "M-77", "created_at": now, "updated_at": now, "by": "system", "category": "approve",
		"title": "AI 瀏覽器一直掛掉", "todo": "請 system 查原因", "options": []any{"現在處理", "不用了"},
		"agent": "system", "site": "office", "delivery": []any{}, "state": "read",
	}}
	if _, err := h.Vault.Put(ctx, r, "system", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Answer(ctx, "M-77", "不用了", sdk.AnswerViaButton, "owner(web)"); err != nil {
		t.Fatalf("舊系統搬來、沒有投遞紀錄的通知要能回答：%v", err)
	}
	if archived, _, err := m.Archive(ctx, []string{"M-77"}, "owner(web)"); err != nil || len(archived) != 1 {
		t.Fatalf("也要能刪：%v %v", archived, err)
	}
}

// Guards: REQ-33
func TestTaskClosedClosesMessagesAnyPath(t *testing.T) {
	for _, status := range []string{"done", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			m, h, _, _ := fixture(t)
			linked(t, h, "task", "SEC-1", "doing")
			n := notice()
			n.Task = "SEC-1"
			n = send(t, m, n)
			event(t, m, "task.changed", map[string]any{"id": "SEC-1", "status": status, "from": "doing"})
			if get(t, m, n.ID).Answer == nil || m.Unread() != 0 {
				t.Fatal("message still open after task closed")
			}
		})
	}
}

// Guards: REQ-33
func TestSweepClosesMessagesOfClosedTasks(t *testing.T) {
	m, h, _, _ := fixture(t)
	linked(t, h, "task", "SEC-1", "doing")
	m.SetTasks(vaultTasks{h.Vault}) // 單的狀態經 tasks 讀
	n := notice()
	n.Task = "SEC-1"
	n = send(t, m, n)
	old, err := h.Vault.Get(ctx, "task", "SEC-1") // 直接改狀態，沒有任何 task.changed 事件
	if err != nil {
		t.Fatal(err)
	}
	old.Fields = map[string]any{"status": "done"}
	if _, err := h.Vault.Put(ctx, old, "system", old.Revision); err != nil {
		t.Fatal(err)
	}
	m.Tick(ctx, h.Clock.Now().Add(2*time.Minute))
	if get(t, m, n.ID).Answer == nil {
		t.Fatal("sweep did not close the message")
	}
}

// 單檔被直接刪掉（找不到這張單）：連兩輪都找不到，掛在上面還沒回的訊息就收掉，不然一直掛著、看板刪不掉；
// 只有一輪找不到（外部工具先刪後寫）不收；查單出別的錯（還沒開好）也不收。
func TestSweepClosesMessagesOfMissingTasks(t *testing.T) {
	m, h, _, _ := fixture(t)
	linked(t, h, "task", "SEC-1", "waiting_user")
	tasks := &stubTasks{err: platform.NotFound("找不到這筆資料")}
	m.SetTasks(tasks)
	n := notice()
	n.Task = "SEC-1"
	n = send(t, m, n)
	now := h.Clock.Now()
	tick := func() { now = now.Add(2 * time.Minute); m.Tick(ctx, now) }

	tick()
	if get(t, m, n.ID).Answer != nil {
		t.Fatal("closed after a single miss")
	}
	tasks.err = platform.Unavailable("還沒開好")
	tick()
	tick()
	if get(t, m, n.ID).Answer != nil {
		t.Fatal("closed on a non-NotFound error")
	}
	tasks.err = platform.NotFound("找不到這筆資料")
	tick()
	if get(t, m, n.ID).Answer != nil {
		t.Fatal("misses must be consecutive")
	}
	tick()
	if get(t, m, n.ID).Answer == nil {
		t.Fatal("sweep did not close the message of a missing task")
	}
}

// stubTasks：查單一律回 err。
type stubTasks struct{ err error }

func (*stubTasks) AddNudge(context.Context, string) error           { return nil }
func (s *stubTasks) Get(context.Context, string) (*sdk.Task, error) { return nil, s.err }

// 首頁「等你處理」（TSK-26）：掛在等你的任務單上的通知不進 approvals（那張單自己在 waiting_tasks，不要同一件事兩張卡、多算一件）；
// 單不在等你、或沒掛單的照列。
func TestBoardApprovalsSkipWaitingTaskNotices(t *testing.T) {
	m, h, _, _ := fixture(t)
	linked(t, h, "task", "SEC-1", "waiting_user")
	if _, err := h.Vault.Put(ctx, vault.Record{Kind: "task", ID: "SEC-2", Fields: map[string]any{"status": "doing"}}, "system", ""); err != nil {
		t.Fatal(err)
	}
	m.SetTasks(vaultTasks{h.Vault})
	waiting, other, plain, asked := notice(), notice(), notice(), notice()
	waiting.Task, other.Task, asked.Task = "SEC-1", "SEC-2", "SEC-1"
	waiting.Todo = "請驗收"
	asked.Todo = "秘書另外問的：要不要順便改標題？" // 跟單上的問句不同：照列，不然首頁看不到這個問題
	send(t, m, waiting)
	keep := []string{send(t, m, other).ID, send(t, m, plain).ID, send(t, m, asked).ID}
	var load func(context.Context) (any, error)
	for _, p := range m.Board() {
		if p.Key == "approvals" {
			load = p.Load
		}
	}
	v, err := load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, n := range v.([]sdk.Notification) {
		got = append(got, n.ID)
	}
	if strings.Join(got, ",") != strings.Join(keep, ",") {
		t.Fatalf("approvals = %v，要 %v", got, keep)
	}
}

// 認領職位的通知（SEC-302 S9）：source＝package 時 claim 留著、存得回來；一般通知帶 claim 一律清掉；
// 回答不追蹤送達，notice.answered 帶回答的人（by），pkg 只認主人。
// Guards: REQ-343
func TestClaimNoticeOwnedByPackage(t *testing.T) {
	m, _, b, _ := fixture(t)
	n := notice()
	n.Source, n.Claim = sdk.NotifySourcePackage, "garden"
	out := send(t, m, n)
	if out.Source != sdk.NotifySourcePackage || get(t, m, out.ID).Claim != "garden" {
		t.Fatalf("認領通知的 source／claim 要留著：%+v", out)
	}
	plain := notice()
	plain.Claim = "garden"
	if got := send(t, m, plain); got.Claim != "" || got.Source != "" {
		t.Fatalf("一般通知不能帶 claim：%+v", got)
	}
	answered, err := m.Answer(ctx, out.ID, "之後再說", sdk.AnswerViaButton, "owner(web)")
	if err != nil {
		t.Fatal(err)
	}
	if m.tracksAnswer(answered) {
		t.Fatal("認領通知的回答由 pkg 處理，不追蹤送達")
	}
	evs := b.published("notice.answered")
	last := evs[len(evs)-1].Payload
	if last["by"] != "owner(web)" || last["source"] != sdk.NotifySourcePackage || last["text"] != "之後再說" {
		t.Fatalf("notice.answered：%v", last)
	}
}
