package notify

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/inbox/push"
	"github.com/minglight/evergenie/sdk"
)

// useSecretsFile 把 home.toml 的 [secrets] 指到 push-secrets.toml（推播通道每次送才讀家的設定，改了下一次就生效）。
func useSecretsFile(t *testing.T, h *homefixture.Home) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.Dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(h.Dir, "home.toml"), string(data)+"\n[secrets]\nfile = \"push-secrets.toml\"\n")
}

// Guards: REQ-28
func TestBoardAndAppDelivery(t *testing.T) {
	h := homefixture.NewShort(t)
	b := &testBus{}
	m := New(Deps{Vault: h.Vault, Bus: b, Push: push.New(h.Dir), Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Stop(ctx); err != nil {
			t.Error(err)
		}
	})
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Header.Get("Authorization") == "" || r.Header.Get("Content-Encoding") != "aes128gcm" || len(data) == 0 {
			t.Error("invalid push request")
		}
		if strings.Contains(string(data), "確認測試") {
			t.Error("unencrypted payload")
		}
		count.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	subKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.RawURLEncoding.EncodeToString(subKey.PublicKey().Bytes())
	auth := base64.RawURLEncoding.EncodeToString([]byte("synthetic-auth-16"))
	write(t, filepath.Join(h.Dir, "settings/push.toml"), fmt.Sprintf("[[subscription]]\nendpoint=%q\np256dh=%q\nauth=%q\n", server.URL, public, auth))
	private, vapidPublic, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	useSecretsFile(t, h)
	write(t, filepath.Join(h.Dir, "push-secrets.toml"), fmt.Sprintf("[ignored]\nanything=42\n[webpush]\nvapid_public_key=%q\nvapid_private_key=%q\nvapid_subject='https://board.example'\n", vapidPublic, private))
	n := send(t, m, notice())
	if len(n.Delivery) != 2 || n.Delivery[0].Result != "ok" || n.Delivery[1].Result != "ok" || count.Load() != 1 {
		t.Fatal("board/app delivery failed")
	}
	// Changing the file affects the very next delivery; no credential cache.
	write(t, filepath.Join(h.Dir, "push-secrets.toml"), "[webpush]\nvapid_public_key='changed'\n")
	n = send(t, m, notice())
	if n.Delivery[1].Result != "failed" || count.Load() != 1 {
		t.Fatal("credentials were cached")
	}
}

// Guards: REQ-120, REQ-28
func TestPushSecretsMissingFailsAppOnly(t *testing.T) {
	cases := []struct {
		name, content  string
		unset, missing bool
	}{
		{name: "unset", unset: true}, {name: "missing", missing: true},
		{name: "missing-field", content: "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_subject='https://board.example'\n"},
		{name: "empty-field", content: "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key=''\nvapid_subject='https://board.example'\n"},
		{name: "broken-toml", content: "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nbroken = [\n"},
		{name: "wrong-subject", content: "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nvapid_subject='invalid-subject-value'\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, h, b, _ := fixture(t)
			subscriptions(t, h)
			if !tc.unset {
				useSecretsFile(t, h)
			}
			if !tc.missing && !tc.unset {
				write(t, filepath.Join(h.Dir, "push-secrets.toml"), tc.content)
			}
			m.d.Push = push.New(h.Dir)
			n := send(t, m, notice())
			if len(n.Delivery) != 2 || n.Delivery[0].Result != "ok" || n.Delivery[1].Result != "failed" || get(t, m, n.ID).State != sdk.NotificationStateUnread {
				t.Fatal("secret failure affected board")
			}
			if len(b.published("system.alert")) != 0 {
				t.Fatal("push failure caused alert loop")
			}
			logPath := filepath.Join(h.Dir, "runtime/logs", h.Clock.Now().Format("2006-01-02")+".jsonl")
			data, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "\"level\":\"WARN\"") != 1 {
				t.Fatal("expected one WARN")
			}
			for _, value := range []string{"synthetic-public-value", "synthetic-private-value", "invalid-subject-value"} {
				if strings.Contains(string(data), value) {
					t.Fatal("secret value leaked")
				}
			}
		})
	}
}

// Guards: REQ-120, REQ-28
func TestNoSubscriptionDoesNotInvokePusher(t *testing.T) {
	m, _, _, p := fixture(t)
	n := send(t, m, notice())
	if len(n.Delivery) != 1 || len(p.payloads) != 0 {
		t.Fatal("no subscription should never invoke credential-reading adapter")
	}
}

// capturePusher 記下每一次被要求推給哪個 endpoint、內容是什麼。
type capturePusher struct {
	*push.Service
	endpoints []string
	payloads  [][]byte
	err       error
}

func (p *capturePusher) Send(_ context.Context, sub push.Subscription, b []byte) error {
	p.endpoints = append(p.endpoints, sub.Endpoint)
	p.payloads = append(p.payloads, append([]byte(nil), b...))
	return p.err
}

const (
	owner   = "owner(web)"
	deviceA = "https://push.example/device-a"
	deviceB = "https://push.example/device-b"
)

func readLog(t *testing.T, h *homefixture.Home) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.Dir, "runtime/logs", h.Clock.Now().Format("2006-01-02")+".jsonl"))
	if err != nil {
		return ""
	}
	return string(data)
}

// Guards: REQ-120
func TestPushKeyMissing(t *testing.T) {
	cases := []struct{ name, content string }{
		{"unset", ""},
		{"missing-file", "gone.toml"},
		{"missing-private", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_subject='https://board.example'\n"},
		{"wrong-subject", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nvapid_subject='invalid-subject-value'\n"},
		{"broken-toml", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nbroken = [\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, h, _, _ := fixture(t)
			if tc.name != "unset" {
				useSecretsFile(t, h)
				if tc.name != "missing-file" {
					write(t, filepath.Join(h.Dir, "push-secrets.toml"), tc.content)
				}
			}
			_, err := m.PushKey(ctx)
			if platform.CodeOf(err) != sdk.ErrorCodeUnavailable || err.Error() != "推播金鑰還沒設定" {
				t.Fatalf("err = %v", err)
			}
			logs := readLog(t, h)
			for _, v := range []string{"synthetic-public-value", "synthetic-private-value", "invalid-subject-value", "vapid_"} {
				if strings.Contains(err.Error(), v) || strings.Contains(logs, v) {
					t.Fatalf("%q 不該出現在錯誤或 log", v)
				}
			}
		})
	}
}

// Guards: REQ-120
func TestPushKeyReturnsPublicKeyAndCount(t *testing.T) {
	m, h, _, _ := fixture(t)
	useSecretsFile(t, h)
	write(t, filepath.Join(h.Dir, "push-secrets.toml"), "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nvapid_subject='https://board.example'\n")
	got, err := m.PushKey(ctx)
	if err != nil || got.PublicKey != "synthetic-public-value" || got.Subscriptions != 0 {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
	if _, err := m.SubscribePush(ctx, deviceA, "k1", "a1", owner); err != nil {
		t.Fatal(err)
	}
	if got, err = m.PushKey(ctx); err != nil || got.Subscriptions != 1 {
		t.Fatalf("got = %+v, err = %v", got, err)
	}
	// 沒有快取：換掉檔案，下一次就讀新的。
	write(t, filepath.Join(h.Dir, "push-secrets.toml"), "[webpush]\nvapid_public_key='changed'\n")
	if _, err := m.PushKey(ctx); err == nil {
		t.Fatal("金鑰被快取了")
	}
	if strings.Contains(readLog(t, h), "synthetic-private-value") {
		t.Fatal("私密金鑰寫進了 log")
	}
}

// Guards: REQ-28
func TestSubscribePushOwnerOnly(t *testing.T) {
	m, h, _, _ := fixture(t)
	for _, by := range []string{"code", "", "system", "someone(amy)"} {
		if _, err := m.SubscribePush(ctx, deviceA, "k1", "a1", by); platform.CodeOf(err) != sdk.ErrorCodeForbidden || err.Error() != "只有主人能管理推播裝置" {
			t.Fatalf("by=%q 應該被擋：%v", by, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "settings/push.toml")); !os.IsNotExist(err) {
		t.Fatal("被擋的訂閱不該寫檔")
	}
	bad := []struct{ endpoint, p256dh, auth, field string }{
		{"http://push.example/a", "k1", "a1", "endpoint"},
		{"", "k1", "a1", "endpoint"},
		{deviceA, "", "a1", "p256dh"},
		{deviceA, "has space", "a1", "p256dh"},
		{deviceA, "k1", "", "auth"},
		{deviceA, "k1", "a+b/c", "auth"},
	}
	for _, b := range bad {
		_, err := m.SubscribePush(ctx, b.endpoint, b.p256dh, b.auth, owner)
		var pe *platform.Err
		if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeInvalid || pe.Field != b.field {
			t.Fatalf("%+v → %v", b, err)
		}
	}
	// 成功：第一台、第二台、同一台更新（不重複）。
	if n, err := m.SubscribePush(ctx, deviceA, "k1_-", "a1==", owner); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := m.SubscribePush(ctx, deviceB, "k2", "a2", "owner(api)"); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := m.SubscribePush(ctx, deviceA, "k9", "a9", owner); err != nil || n != 2 {
		t.Fatalf("同一個 endpoint 要更新、不重複：n=%d err=%v", n, err)
	}
	subs, err := push.New(h.Dir).Subscriptions()
	if err != nil || len(subs) != 2 || subs[0] != (push.Subscription{Endpoint: deviceA, P256dh: "k9", Auth: "a9"}) || subs[1].Endpoint != deviceB {
		t.Fatalf("subs = %+v err=%v", subs, err)
	}
	info, err := os.Stat(filepath.Join(h.Dir, "settings/push.toml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("push.toml 權限 = %v err=%v", info, err)
	}
}

// Guards: REQ-28
func TestUnsubscribePush(t *testing.T) {
	m, h, _, _ := fixture(t)
	for _, e := range []string{deviceA, deviceB} {
		if _, err := m.SubscribePush(ctx, e, "k", "a", owner); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := m.UnsubscribePush(ctx, deviceA, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能刪裝置：%v", err)
	}
	if n, err := m.UnsubscribePush(ctx, deviceA, owner); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	// 沒有這一筆也算成功。
	if n, err := m.UnsubscribePush(ctx, "https://push.example/never", owner); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	subs, _ := push.New(h.Dir).Subscriptions()
	if len(subs) != 1 || subs[0].Endpoint != deviceB {
		t.Fatalf("subs = %+v", subs)
	}
	// 刪到 0 筆：檔案留著、是空的，之後送通知照常（沒訂閱就不推）。
	if n, err := m.UnsubscribePush(ctx, deviceB, owner); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	data, err := os.ReadFile(filepath.Join(h.Dir, "settings/push.toml"))
	if err != nil || strings.TrimSpace(string(data)) != "" {
		t.Fatalf("空檔 = %q err=%v", data, err)
	}
	if subs, err := push.New(h.Dir).Subscriptions(); err != nil || len(subs) != 0 {
		t.Fatalf("subs = %+v err=%v", subs, err)
	}
}

// Guards: REQ-28
func TestPushTestOnlyThatDevice(t *testing.T) {
	m, h, b, _ := fixture(t)
	p := &capturePusher{Service: push.New(h.Dir)}
	m.d.Push = p
	for _, e := range []string{deviceA, deviceB} {
		if _, err := m.SubscribePush(ctx, e, "k", "a", owner); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.PushTest(ctx, deviceB, "code"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能送測試：%v", err)
	}
	if err := m.PushTest(ctx, "https://push.example/never", owner); platform.CodeOf(err) != sdk.ErrorCodeNotFound || err.Error() != "這台裝置還沒開啟推播" {
		t.Fatalf("沒訂閱的裝置：%v", err)
	}
	if len(p.endpoints) != 0 {
		t.Fatal("被擋或找不到的測試不該推給任何人")
	}
	if err := m.PushTest(ctx, deviceB, owner); err != nil {
		t.Fatal(err)
	}
	if len(p.endpoints) != 1 || p.endpoints[0] != deviceB {
		t.Fatalf("只該推給 B：%v", p.endpoints)
	}
	var n sdk.Notification
	if err := json.Unmarshal(p.payloads[0], &n); err != nil || n.ID != "test" || n.Category != sdk.CategoryDone || n.Title != "測試通知" || n.Context != "這台裝置收得到推播" {
		t.Fatalf("payload = %s err=%v", p.payloads[0], err)
	}
	// 不存檔、不進看板訊息、不發事件。
	list, err := m.List(ctx, nil, "", true)
	if err != nil || len(list) != 0 {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if m.Unread() != 0 || len(b.published("notice.sent")) != 0 {
		t.Fatal("測試通知不該算未讀或發 notice.sent")
	}
	if _, err := os.Stat(filepath.Join(h.Dir, "vault/messages")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(h.Dir, "vault/messages"))
		if len(entries) != 0 {
			t.Fatalf("不該有訊息檔：%v", entries)
		}
	}
	// 推播端失敗：注入的轉接器的錯誤文字不外流。
	p.err = errors.New("secret-detail-from-adapter")
	err = m.PushTest(ctx, deviceA, owner)
	if platform.CodeOf(err) != sdk.ErrorCodeUnavailable || err.Error() != "推播送出失敗" || strings.Contains(err.Error(), "secret-detail") {
		t.Fatalf("err = %v", err)
	}
}

// Guards: REQ-117, REQ-28
func TestPushStatusJudgment(t *testing.T) {
	m, h, _, _ := fixture(t)
	if s := m.PushStatus(ctx); s.Ok || s.Detail != "還沒設定推播金鑰" || s.Fix == "" {
		t.Fatalf("沒金鑰：%+v", s)
	}
	useSecretsFile(t, h)
	write(t, filepath.Join(h.Dir, "push-secrets.toml"), "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nvapid_subject='https://board.example'\n")
	if s := m.PushStatus(ctx); s.Ok || s.Detail != "還沒有裝置開啟推播" {
		t.Fatalf("沒裝置：%+v", s)
	}
	for _, d := range []string{deviceA, deviceB} {
		if _, err := m.SubscribePush(ctx, d, "k1", "a1", owner); err != nil {
			t.Fatal(err)
		}
	}
	s := m.PushStatus(ctx)
	if !s.Ok || s.Detail != "2 台裝置" || s.Fix != "" {
		t.Fatalf("兩台裝置：%+v", s)
	}
	if strings.Contains(fmt.Sprint(s), "synthetic") || strings.Contains(fmt.Sprint(s), "push.example") {
		t.Fatalf("狀態不能帶金鑰或裝置網址：%+v", s)
	}
}

// 每一條送 Web Push 的路（一般通知、測試推播、提醒、綁對話的等待）
// payload 都帶 home＝這台的標籤，url 維持站內相對路徑（看板連多台時 service worker 照 home 換成那台的網址，S3）。
func TestEveryPushPayloadCarriesHomeLabelAndRelativeURL(t *testing.T) {
	m, p, _ := runtimeHome(t, "owner = \"amy\"\nmachine = \"office\"")
	m.SetTasks(&fakeTasks{})
	kinds := []string{}
	expect := func(kind string) {
		t.Helper()
		kinds = append(kinds, kind)
		if len(p.payloads) != len(kinds) {
			t.Fatalf("%s 要推一則（應有 %d 則，實際 %d 則）", kind, len(kinds), len(p.payloads))
		}
		last := p.payloads[len(p.payloads)-1]
		var raw map[string]any
		if err := json.Unmarshal(last, &raw); err != nil {
			t.Fatal(err)
		}
		if raw["home"] != "amy-office" {
			t.Fatalf("%s 的推播要帶 home = amy-office：%s", kind, last)
		}
		if url, _ := raw["url"].(string); url != "" && !strings.HasPrefix(url, "/") {
			t.Fatalf("%s 的 url 要是站內相對路徑：%q", kind, url)
		}
	}

	send(t, m, notice())
	expect("一般通知")
	if err := m.PushTest(ctx, "https://push.invalid", owner); err != nil {
		t.Fatal(err)
	}
	expect("測試推播")
	if _, err := m.Nudge(ctx, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code-sec-12", Task: "SEC-12", Todo: "請確認"}); err != nil {
		t.Fatal(err)
	}
	expect("提醒")
	if _, err := m.Nudge(ctx, &sdk.NotifyNudgeRequest{Agent: "code", Session: "code", Key: "pending:P1", Todo: "允許或拒絕"}); err != nil {
		t.Fatal(err)
	}
	expect("對話等待")
}
