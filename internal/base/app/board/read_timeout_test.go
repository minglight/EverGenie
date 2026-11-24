package board

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 後面的服務卡住時用的假 api：路徑在 stuck 裡的一直不回（等到測試結束或請求被取消），其他照常回 {"ok":true}。
func stuckAPI(t *testing.T, stuck ...string) (http.Handler, chan string) {
	t.Helper()
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	cancelled := make(chan string, 8)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range stuck {
			if r.URL.Path == p {
				select {
				case <-release:
				case <-r.Context().Done():
					cancelled <- r.URL.Path
				}
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), cancelled
}

func newStuckModule(t *testing.T, api http.Handler, limit time.Duration) http.Handler {
	t.Helper()
	home, err := os.MkdirTemp("/tmp", "hgw-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	write(t, filepath.Join(home, "home.toml"), fmt.Sprintf(homeTOML, freePort(t), ""))
	m := New(Deps{API: fakeHandler{api}})
	m.SetHome(home)
	m.apiReadLimit = limit
	return viaLoopback(m.Handler())
}

// 首頁要的讀取（選單、總覽…）後面卡住：看板在時限內回白話的 503（unavailable），不讓瀏覽器一直等；
// 卡住的那個請求的 context 也被取消。其他讀取照常。
func TestStuckReadAnswersPlainTimeout(t *testing.T) {
	api, cancelled := stuckAPI(t, "/api/web/get_menu")
	h := newStuckModule(t, api, 50*time.Millisecond)

	started := time.Now()
	w := post(h, "/api/web/get_menu", "{}")
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("等了 %s 才回", took)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("狀態碼 %d，要 503：%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q，要 JSON", ct)
	}
	var e sdk.Error
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("回應不是 sdk.Error：%v %q", err, w.Body.String())
	}
	if e.Code != sdk.ErrorCodeUnavailable || !strings.Contains(e.Message, "等太久") {
		t.Fatalf("錯誤 %+v，要白話的 unavailable", e)
	}
	select {
	case p := <-cancelled:
		if p != "/api/web/get_menu" {
			t.Fatalf("被取消的是 %s", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("卡住的請求沒有被取消")
	}

	// 別的讀取照常：另開一個時限夠長的，機器忙的時候不會剛好踩到上面那個很短的時限。
	other := newStuckModule(t, api, 30*time.Second)
	if w := post(other, "/api/web/get_overview", "{}"); w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"ok":true}` {
		t.Fatalf("別的讀取：%d %q", w.Code, w.Body.String())
	}
}

// 時限只管「該很快回來的讀取」：會改東西的（中途切斷不知道做了沒）、串流、本來就久的讀取都不設。
func TestQuickReadOnlyCoversReads(t *testing.T) {
	for _, c := range []struct {
		service, method string
		want            bool
	}{
		{"web", "get_menu", true},
		{"web", "get_overview", true},
		{"system", "get_mode", true},
		{"session", "list_threads", true},
		{"session", "list", true},
		{"tasks", "get", true},
		{"notify", "list", true},
		{"web", "get_health", false},
		{"package", "list_updates", false},
		{"session", "watch_threads", false},
		{"session", "chat", false},
		{"notify", "watch", false},
		{"bus", "subscribe", false},
		{"tasks", "create", false},
		{"notify", "answer", false},
		{"system", "set_mode", false},
		{"system", "update_apply", false},
		{"session", "getaway", false},
	} {
		if got := quickRead(c.service, c.method); got != c.want {
			t.Errorf("quickRead(%s, %s) = %v，要 %v", c.service, c.method, got, c.want)
		}
	}
}

// 會改東西的請求後面慢：看板照等，不會在讀取的時限切斷。
// 用真的時間（http.TimeoutHandler 沒有可以換的時鐘）：後面慢 150ms、時限 30ms，差距夠大。
func TestSlowWriteIsNotCutOff(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	h := newStuckModule(t, api, 30*time.Millisecond)
	if w := post(h, "/api/tasks/create", `{"title":"x"}`); w.Code != http.StatusOK {
		t.Fatalf("寫入被切斷：%d %q", w.Code, w.Body.String())
	}
}
