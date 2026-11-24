package push

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
	"sync"
	"sync/atomic"
	"testing"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

const (
	deviceA = "https://push.example/device-a"
	deviceB = "https://push.example/device-b"
)

func newService(t *testing.T, secrets string) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	return New(dir, WithSecretsPath(func() string {
		if secrets == "" {
			return ""
		}
		return filepath.Join(dir, secrets)
	})), dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 訂閱端點：新增、同一個 endpoint 更新不重複、順序保留、檔案 0600。
func TestSubscribeAddsAndUpdates(t *testing.T) {
	s, dir := newService(t, "")
	if subs, err := s.Subscriptions(); err != nil || len(subs) != 0 {
		t.Fatalf("沒有檔案要回空：%v %v", subs, err)
	}
	if n, err := s.Subscribe(Subscription{Endpoint: deviceA, P256dh: "k1_-", Auth: "a1=="}); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := s.Subscribe(Subscription{Endpoint: deviceB, P256dh: "k2", Auth: "a2"}); err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := s.Subscribe(Subscription{Endpoint: deviceA, P256dh: "k9", Auth: "a9"}); err != nil || n != 2 {
		t.Fatalf("同一個 endpoint 要更新、不重複：n=%d err=%v", n, err)
	}
	subs, err := s.Subscriptions()
	if err != nil || len(subs) != 2 || subs[0] != (Subscription{Endpoint: deviceA, P256dh: "k9", Auth: "a9"}) || subs[1].Endpoint != deviceB {
		t.Fatalf("subs = %+v err=%v", subs, err)
	}
	info, err := os.Stat(filepath.Join(dir, "settings", "push.toml"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("push.toml 權限 = %v err=%v", info, err)
	}
}

// 先用空的家建（inbox 塊的組裝），SetHome 之前不讀也不寫工作目錄；SetHome 之後寫進那個家。
func TestEmptyHomeUntilSetHome(t *testing.T) {
	t.Chdir(t.TempDir())
	s := New("")
	if subs, err := s.Subscriptions(); err != nil || len(subs) != 0 {
		t.Fatalf("沒設家當成沒有訂閱：%v %v", subs, err)
	}
	if _, err := s.Subscribe(Subscription{Endpoint: deviceA, P256dh: "k", Auth: "a"}); err == nil {
		t.Fatal("沒設家不能寫")
	}
	if _, err := os.Stat(filepath.Join("settings", "push.toml")); !os.IsNotExist(err) {
		t.Fatalf("不能寫到工作目錄：%v", err)
	}
	dir := t.TempDir()
	s.SetHome(dir)
	if n, err := s.Subscribe(Subscription{Endpoint: deviceA, P256dh: "k", Auth: "a"}); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "settings", "push.toml")); err != nil {
		t.Fatal(err)
	}
}

// 格式不對回 Invalid 並指出欄位，而且不寫檔。
func TestSubscribeRejectsBadFormat(t *testing.T) {
	s, dir := newService(t, "")
	bad := []struct {
		sub   Subscription
		field string
	}{
		{Subscription{"http://push.example/a", "k1", "a1"}, "endpoint"},
		{Subscription{"", "k1", "a1"}, "endpoint"},
		{Subscription{deviceA, "", "a1"}, "p256dh"},
		{Subscription{deviceA, "has space", "a1"}, "p256dh"},
		{Subscription{deviceA, "k1", ""}, "auth"},
		{Subscription{deviceA, "k1", "a+b/c"}, "auth"},
	}
	for _, b := range bad {
		_, err := s.Subscribe(b.sub)
		var pe *platform.Err
		if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeInvalid || pe.Field != b.field {
			t.Fatalf("%+v → %v", b, err)
		}
		if Validate(b.sub) == nil {
			t.Fatalf("Validate 也要擋：%+v", b)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "settings", "push.toml")); !os.IsNotExist(err) {
		t.Fatal("被擋的訂閱不該寫檔")
	}
}

// 取消訂閱：沒有這筆也算成功（不寫檔）；刪到 0 筆留下空檔。
func TestUnsubscribe(t *testing.T) {
	s, dir := newService(t, "")
	for _, e := range []string{deviceA, deviceB} {
		if _, err := s.Subscribe(Subscription{Endpoint: e, P256dh: "k", Auth: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.Unsubscribe(deviceA); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := s.Unsubscribe("https://push.example/never"); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if n, err := s.Unsubscribe(deviceB); err != nil || n != 0 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings", "push.toml"))
	if err != nil || strings.TrimSpace(string(data)) != "" {
		t.Fatalf("空檔 = %q err=%v", data, err)
	}
	if subs, err := s.Subscriptions(); err != nil || len(subs) != 0 {
		t.Fatalf("subs = %+v err=%v", subs, err)
	}
}

// 壞掉或缺欄位的訂閱檔回 Invalid，不 panic，也不把壞資料當成沒人訂閱。
func TestSubscriptionsRejectsBrokenFile(t *testing.T) {
	for name, content := range map[string]string{
		"broken-toml":   "[[subscription]\nendpoint=",
		"missing-field": "[[subscription]]\nendpoint='https://push.invalid'\np256dh='k'\n",
	} {
		t.Run(name, func(t *testing.T) {
			s, dir := newService(t, "")
			writeFile(t, filepath.Join(dir, "settings", "push.toml"), content)
			if _, err := s.Subscriptions(); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// 同時訂閱多台裝置，讀改寫不能互相蓋掉。
func TestSubscribeConcurrent(t *testing.T) {
	s, _ := newService(t, "")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Subscribe(Subscription{Endpoint: fmt.Sprintf("https://push.example/%d", i), P256dh: "k", Auth: "a"}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if subs, err := s.Subscriptions(); err != nil || len(subs) != 20 {
		t.Fatalf("要有 20 台：%d %v", len(subs), err)
	}
}

// 金鑰檔有問題：公開金鑰與送出都回 platform 錯誤，錯誤不帶金鑰值或欄位內容。
func TestKeyFileProblemsDoNotLeak(t *testing.T) {
	cases := []struct{ name, content string }{
		{"missing-file", ""},
		{"missing-field", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_subject='https://board.example'\n"},
		{"empty-field", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key=''\nvapid_subject='https://board.example'\n"},
		{"broken-toml", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nbroken = [\n"},
		{"wrong-subject", "[webpush]\nvapid_public_key='synthetic-public-value'\nvapid_private_key='synthetic-private-value'\nvapid_subject='invalid-subject-value'\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, dir := newService(t, "secrets.toml")
			if tc.content != "" {
				writeFile(t, filepath.Join(dir, "secrets.toml"), tc.content)
			}
			_, err1 := s.PublicKey()
			err2 := s.Send(context.Background(), Subscription{Endpoint: deviceA, P256dh: "k", Auth: "a"}, []byte("x"))
			for _, err := range []error{err1, err2} {
				var pe *platform.Err
				if !errors.As(err, &pe) {
					t.Fatalf("要回 platform 錯誤：%v", err)
				}
				for _, v := range []string{"synthetic-public-value", "synthetic-private-value", "invalid-subject-value"} {
					if strings.Contains(err.Error(), v) {
						t.Fatalf("%q 外流到錯誤訊息", v)
					}
				}
			}
		})
	}
}

// 沒設定秘密檔（路徑是空的）算「尚未設定」。
func TestSecretsUnset(t *testing.T) {
	s, _ := newService(t, "")
	if _, err := s.PublicKey(); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("err = %v", err)
	}
}

// 預設從家的 home.toml 找秘密檔，而且每次都重讀（改了設定下一次就生效，不快取）。
func TestDefaultSecretsPathComesFromHome(t *testing.T) {
	h := homefixture.New(t)
	dir := h.Dir
	home, err := os.ReadFile(filepath.Join(dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	if _, err := s.PublicKey(); platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
		t.Fatalf("沒設定要回 Unavailable：%v", err)
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "push-secrets.toml"), fmt.Sprintf("[webpush]\nvapid_public_key=%q\nvapid_private_key=%q\nvapid_subject='https://board.example'\n", public, private))
	writeFile(t, filepath.Join(dir, "home.toml"), string(home)+"\n[secrets]\nfile = \"push-secrets.toml\"\n")
	if got, err := s.PublicKey(); err != nil || got != public {
		t.Fatalf("PublicKey = %q err=%v", got, err)
	}
	writeFile(t, filepath.Join(dir, "push-secrets.toml"), "[webpush]\nvapid_public_key='changed'\n")
	if _, err := s.PublicKey(); err == nil {
		t.Fatal("金鑰被快取了")
	}
}

// 真的送一次：打到推播服務的請求有 VAPID 授權、內容加密；服務回非 2xx 算失敗。
func TestSendEncryptsAndChecksStatus(t *testing.T) {
	s, dir := newService(t, "secrets.toml")
	var count atomic.Int32
	status := atomic.Int32{}
	status.Store(http.StatusCreated)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Header.Get("Authorization") == "" || r.Header.Get("Content-Encoding") != "aes128gcm" || len(data) == 0 {
			t.Error("invalid push request")
		}
		if strings.Contains(string(data), "plain-text-marker") {
			t.Error("unencrypted payload")
		}
		count.Add(1)
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	subKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscription{
		Endpoint: server.URL,
		P256dh:   base64.RawURLEncoding.EncodeToString(subKey.PublicKey().Bytes()),
		Auth:     base64.RawURLEncoding.EncodeToString([]byte("synthetic-auth-16")),
	}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "secrets.toml"), fmt.Sprintf("[ignored]\nanything=42\n[webpush]\nvapid_public_key=%q\nvapid_private_key=%q\nvapid_subject='https://board.example'\n", public, private))
	if err := s.Send(context.Background(), sub, []byte("plain-text-marker")); err != nil || count.Load() != 1 {
		t.Fatalf("送出：%v（收到 %d 次）", err, count.Load())
	}
	status.Store(http.StatusGone)
	err = s.Send(context.Background(), sub, []byte("plain-text-marker"))
	var pe *platform.Err
	if !errors.As(err, &pe) || pe.Code != sdk.ErrorCodeUnavailable || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("非 2xx 要回整理過的 Unavailable：%v", err)
	}
	// 訂閱本身壞掉（金鑰不是合法的 base64）：同樣整理過，不帶輸入。
	err = s.Send(context.Background(), Subscription{Endpoint: server.URL, P256dh: "!!bad!!", Auth: "!!bad!!"}, []byte("x"))
	if !errors.As(err, &pe) || strings.Contains(err.Error(), "bad") {
		t.Fatalf("err = %v", err)
	}
}

// 推播服務拒絕時，錯誤帶狀態碼與服務給的原因代碼（Apple 的 JSON reason、FCM 的一行文字），
// 但不帶端點網址或回應裡的其他東西；訂閱失效（410）要說怎麼補救。
func TestSendRejectionCarriesStatusAndReason(t *testing.T) {
	s, dir := newService(t, "secrets.toml")
	var status atomic.Int32
	var body atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(int(status.Load()))
		_, _ = io.WriteString(w, body.Load().(string))
	}))
	defer server.Close()
	subKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sub := Subscription{Endpoint: server.URL, P256dh: base64.RawURLEncoding.EncodeToString(subKey.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString([]byte("synthetic-auth-16"))}
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "secrets.toml"), fmt.Sprintf("[webpush]\nvapid_public_key=%q\nvapid_private_key=%q\nvapid_subject='https://board.example'\n", public, private))
	for _, c := range []struct {
		status     int
		body, want string
		not        []string
	}{
		{403, `{"reason":"BadJwtToken"}`, "HTTP 403 BadJwtToken）：推播服務不認這把金鑰或聯絡方式", nil},
		{410, `{"reason":"Unregistered","extra":"x"}`, "HTTP 410 Unregistered）：收件裝置的訂閱已失效，在那台裝置關閉推播再重新開啟", []string{"extra"}},
		{400, "UnauthorizedRegistration <secret>\nsecond line", "HTTP 400 UnauthorizedRegistration secret）", []string{"second", "<"}},
		{500, "see https://example.com/x", "HTTP 500）", []string{"example"}},
	} {
		status.Store(int32(c.status))
		body.Store(c.body)
		err := s.Send(context.Background(), sub, []byte("x"))
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), server.URL) {
			t.Fatalf("%d：要有「%s」、不帶端點，拿到 %v", c.status, c.want, err)
		}
		for _, n := range c.not {
			if strings.Contains(err.Error(), n) {
				t.Fatalf("%d：不該帶「%s」：%v", c.status, n, err)
			}
		}
	}
}

// 送出去的 VAPID 憑證裡，聯絡方式（sub）正好是設定的那個：mailto: 不重複（重複時 Apple 回 403 BadJwtToken）、https:// 原樣。
func TestSendJWTSubjectMatchesConfig(t *testing.T) {
	for _, subject := range []string{"mailto:owner@example.com", "https://board.example"} {
		s, dir := newService(t, "secrets.toml")
		var got atomic.Value
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization") // vapid t=<jwt>, k=<key>
			jwt, _, _ := strings.Cut(strings.TrimPrefix(auth, "vapid t="), ",")
			parts := strings.Split(jwt, ".")
			if len(parts) == 3 {
				payload, err := base64.RawURLEncoding.DecodeString(parts[1])
				if err == nil {
					var claims struct {
						Sub string `json:"sub"`
					}
					if json.Unmarshal(payload, &claims) == nil {
						got.Store(claims.Sub)
					}
				}
			}
			w.WriteHeader(http.StatusCreated)
		}))
		subKey, err := ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		sub := Subscription{Endpoint: server.URL, P256dh: base64.RawURLEncoding.EncodeToString(subKey.PublicKey().Bytes()), Auth: base64.RawURLEncoding.EncodeToString([]byte("synthetic-auth-16"))}
		private, public, err := webpush.GenerateVAPIDKeys()
		if err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, "secrets.toml"), fmt.Sprintf("[webpush]\nvapid_public_key=%q\nvapid_private_key=%q\nvapid_subject=%q\n", public, private, subject))
		if err := s.Send(context.Background(), sub, []byte("x")); err != nil {
			t.Fatal(err)
		}
		server.Close()
		if v, _ := got.Load().(string); v != subject {
			t.Fatalf("憑證的 sub = %q，應該是 %q", v, subject)
		}
	}
}

// 沒有金鑰時 Send 不會去打網路（先讀金鑰、讀不到就停）。
func TestSendWithoutKeyNeverCallsNetwork(t *testing.T) {
	s, _ := newService(t, "secrets.toml")
	var hit atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Store(true) }))
	defer server.Close()
	if err := s.Send(context.Background(), Subscription{Endpoint: server.URL, P256dh: "k", Auth: "a"}, []byte("x")); err == nil || hit.Load() {
		t.Fatalf("err=%v hit=%v", err, hit.Load())
	}
}
