package board

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// phonePost 送一個手機連線的請求：remote 是連線來源（看板只聽本機，真的請求都是 127.0.0.1），headers 同 crossPost。
func phonePost(h http.Handler, path, body, remote string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = remote
	r.Host = "127.0.0.1:8850"
	for i := 0; i+1 < len(headers); i += 2 {
		if headers[i] == "Host" {
			r.Host = headers[i+1]
			continue
		}
		r.Header.Add(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const (
	loopback   = "127.0.0.1:50123"
	tailnetURL = "genie-amy-office.example.ts.net"
)

// notThisMac 是「不是這台 Mac 本機開的看板」的各種請求（連線都來自 127.0.0.1：經 Tailscale 轉進來的就是這樣）。
var notThisMac = map[string][]string{
	"手機或別台經 Tailscale（個人帳號）":       {"Host", tailnetURL, "Origin", "https://" + tailnetURL, "X-Forwarded-For", "100.101.102.103", "X-Forwarded-Host", tailnetURL, "X-Forwarded-Proto", "https", tailscaleLoginHeader, ownerLogin},
	"別台經 Tailscale（tag 身分，沒有登入標頭）": {"Host", tailnetURL, "Origin", "https://" + tailnetURL, "X-Forwarded-For", "100.101.102.103"},
	"經 Tailscale、但把 Host 偽造成本機":    {"Host", "127.0.0.1:8850", "X-Forwarded-For", "100.101.102.103"},
	"偽造 Host 與 Origin，轉送標頭還在":      {"Host", "localhost:8850", "Origin", "http://localhost:8850", "X-Forwarded-For", "100.101.102.103", "X-Forwarded-Proto", "https"},
	"經 Funnel（公網）":                 {"Host", "127.0.0.1:8850", "Tailscale-Funnel-Request", "?1"},
	"只帶 Tailscale 的登入標頭":           {"Host", "127.0.0.1:8850", tailscaleLoginHeader, ownerLogin},
	"別的代理（Forwarded）":              {"Host", "127.0.0.1:8850", "Forwarded", "for=203.0.113.9"},
	"別的代理（Via）":                    {"Host", "127.0.0.1:8850", "Via", "1.1 proxy"},
	"別的代理（X-Real-Ip）":              {"Host", "127.0.0.1:8850", "X-Real-Ip", "203.0.113.9"},
	"看板的網址、沒有轉送標頭":                 {"Host", tailnetURL},
	"瀏覽器說是別的網站送的":                  {"Host", "127.0.0.1:8850", "Origin", "http://127.0.0.1:8850", "Sec-Fetch-Site", "cross-site"},
	"別台看板跨站、帶主人登入":                 {"Host", "genie-amy-office.example.ts.net", "Origin", otherBoard, tailscaleLoginHeader, ownerLogin, "X-Forwarded-For", "100.101.102.103"},
}

// 「讓手機連得到」會動這台電腦的 Tailscale：不是這台 Mac 本機開的看板一律 403，而且不交給核心。
// Guards: WEB-187
func TestPhoneSetupOnlyFromThisMac(t *testing.T) {
	e := newEnv(t, crossWeb)
	for name, headers := range notThisMac {
		w := phonePost(e.h, "/api/web/phone_setup", `{"local":true}`, loopback, headers...)
		er := decodeErr(t, w)
		if w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || er.Message != thisMacOnlyMessage {
			t.Errorf("%s：要 403「只能在那台 Mac 上按」，得到 %d %+v", name, w.Code, er)
		}
	}
	// 連線不是來自本機（看板不聽區網；萬一哪天聽了也不算本機）。
	if w := phonePost(e.h, "/api/web/phone_setup", `{"local":true}`, "192.168.1.20:40000"); w.Code != http.StatusForbidden {
		t.Errorf("區網來的要 403，得到 %d", w.Code)
	}
	// 這台 Mac 上別的埠的網頁（Origin 是本機、但跟看板的 Host 不同埠）不算本機開的看板。
	for name, headers := range map[string][]string{
		"本機別的埠":           {"Host", "127.0.0.1:8850", "Origin", "http://127.0.0.1:3000"},
		"localhost 對 127": {"Host", "127.0.0.1:8850", "Origin", "http://localhost:8850"},
		"Origin 沒寫埠":      {"Host", "127.0.0.1:8850", "Origin", "http://127.0.0.1"},
	} {
		if w := phonePost(e.h, "/api/web/phone_setup", `{"local":true}`, loopback, headers...); w.Code != http.StatusForbidden {
			t.Errorf("%s：Origin 跟 Host 不一樣要 403，得到 %d %s", name, w.Code, w.Body.String())
		}
	}
	if e.api.calls != 0 {
		t.Fatalf("被擋的請求不能交給核心：%d 次", e.api.calls)
	}
}

// 「關掉對外公開」是變安全的動作，主人從哪裡都能關（手機、別台）：不擋，只是 local 照伺服器算的填。
// Guards: WEB-187
func TestPhoneCloseFunnelWorksFromAnywhere(t *testing.T) {
	e := newEnv(t, crossWeb)
	for name, headers := range notThisMac {
		if strings.Contains(name, "別台看板跨站") || strings.Contains(name, "別的網站") {
			continue // 這兩種由跨站檢查另外處理
		}
		w := phonePost(e.h, "/api/web/phone_close_funnel", `{"local":true}`, loopback, headers...)
		var res sdk.WebPhoneCloseFunnelResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil || res.Phone.Local {
			t.Errorf("%s：要放行、local 是伺服器算的 false，得到 %d %s", name, w.Code, w.Body.String())
		}
	}
}

// 這台 Mac 自己的瀏覽器（或 curl）直接打本機看板：放行，交給核心的請求帶 local=true。
func TestPhoneSetupAllowedOnThisMac(t *testing.T) {
	e := newEnv(t, crossWeb)
	for name, headers := range map[string][]string{
		"瀏覽器（同源）":         {"Host", "127.0.0.1:8850", "Origin", "http://127.0.0.1:8850", "Sec-Fetch-Site", "same-origin"},
		"瀏覽器用 localhost":  {"Host", "localhost:8850", "Origin", "http://localhost:8850"},
		"curl（沒有 Origin）": {"Host", "127.0.0.1:8850"},
	} {
		w := phonePost(e.h, "/api/web/phone_setup", `{"local":false}`, loopback, headers...)
		var res sdk.WebPhoneSetupResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil || !res.Phone.Local {
			t.Errorf("%s：要放行而且 local=true，得到 %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := phonePost(e.h, "/api/web/phone_close_funnel", `{}`, "[::1]:50123", "Host", "[::1]:8850"); w.Code != http.StatusOK {
		t.Errorf("IPv6 的本機：%d %s", w.Code, w.Body.String())
	}
}

// 看狀態誰都可以，但 local 是伺服器算的：用戶端自己寫 local=true（含大小寫不同的 key）不算數。
func TestPhoneStatusLocalIsDecidedByServer(t *testing.T) {
	e := newEnv(t, crossWeb)
	local := func(w *httptest.ResponseRecorder) bool {
		t.Helper()
		var res sdk.WebGetPhoneResponse
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &res) != nil {
			t.Fatalf("get_phone：%d %s", w.Code, w.Body.String())
		}
		return res.Phone.Local
	}
	for name, headers := range notThisMac {
		if strings.Contains(name, "別台看板跨站") || strings.Contains(name, "別的網站") {
			continue // 這兩種由跨站檢查另外處理
		}
		for _, body := range []string{`{"local":true}`, `{"LOCAL":true}`, `{"Local":true,"local":true}`} {
			if local(phonePost(e.h, "/api/web/get_phone", body, loopback, headers...)) {
				t.Errorf("%s／%s：不是本機卻被當成本機", name, body)
			}
		}
	}
	if !local(phonePost(e.h, "/api/web/get_phone", `{}`, loopback, "Origin", "http://127.0.0.1:8850")) {
		t.Error("本機的看板要是 local=true")
	}
}
