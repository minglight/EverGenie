package board

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/sdk"
)

// 看板連多台（S3-2）：別台看板（瀏覽器）跨站呼叫這台的 /api/*。
const (
	otherBoard = "https://genie-amy-mini.example.ts.net"
	ownerLogin = "owner@example.com"
	crossWeb   = `public_url="https://genie-amy-office.example.ts.net"` + "\n" +
		`allow_boards=["` + otherBoard + `"]` + "\n" +
		`owner_login="` + ownerLogin + `"` + "\n"
)

func preflight(h http.Handler, path, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodOptions, path, nil)
	r.Header.Set("Origin", origin)
	r.Header.Set("Access-Control-Request-Method", "POST")
	r.Header.Set("Access-Control-Request-Headers", "content-type,x-filename,x-requested-with")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// crossPost 送一個跨站 POST：headers 用 Add（可以重複同一個標頭）。
func crossPost(h http.Handler, path, body string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
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

func hasVaryOrigin(h http.Header) bool {
	for _, v := range h.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Origin") {
				return true
			}
		}
	}
	return false
}

// rewriteWeb 換掉 home.toml 的 [web] 額外設定（port 不變），修改時間往後推一秒，確定看得出檔案變了。
func rewriteWeb(t *testing.T, e *env, extra string) {
	t.Helper()
	file := filepath.Join(e.home, "home.toml")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^port=(\d+)$`).FindSubmatch(raw)
	if m == nil {
		t.Fatalf("home.toml 找不到 port：%s", raw)
	}
	port, _ := strconv.Atoi(string(m[1]))
	info, _ := os.Stat(file)
	write(t, file, fmt.Sprintf(homeTOML, port, extra))
	later := info.ModTime().Add(time.Second)
	if err := os.Chtimes(file, later, later); err != nil {
		t.Fatal(err)
	}
}

func TestCORSPreflightAllowedOriginAnswers204(t *testing.T) {
	e := newEnv(t, crossWeb)
	for _, path := range []string{"/api/tasks/accept", "/api/session/watch", uploadPath} {
		w := preflight(e.h, path, otherBoard)
		h := w.Header()
		if w.Code != http.StatusNoContent || h.Get("Access-Control-Allow-Origin") != otherBoard || !hasVaryOrigin(h) {
			t.Fatalf("%s 預檢：%d %v", path, w.Code, h)
		}
		if h.Get("Access-Control-Allow-Methods") != "POST" || h.Get("Access-Control-Max-Age") != "600" {
			t.Fatalf("%s 預檢的方法與快取：%v", path, h)
		}
		allowHeaders := strings.ToLower(h.Get("Access-Control-Allow-Headers"))
		for _, want := range []string{"content-type", "x-requested-with", "x-filename"} {
			if !strings.Contains(allowHeaders, want) {
				t.Fatalf("%s 的 Allow-Headers 少了 %s：%q", path, want, allowHeaders)
			}
		}
		if h.Get("Access-Control-Allow-Credentials") != "" {
			t.Fatalf("不開 Allow-Credentials：%v", h)
		}
	}
	if e.api.calls != 0 {
		t.Fatalf("預檢不能交給 api：%d 次", e.api.calls)
	}
}

func TestCORSPreflightUnlistedOriginRejectedWithoutACAO(t *testing.T) {
	e := newEnv(t, crossWeb)
	for _, origin := range []string{"https://evil.example", "null", otherBoard + ".evil.example", "http://genie-amy-mini.example.ts.net"} {
		w := preflight(e.h, "/api/tasks/accept", origin)
		if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s 預檢：%d %+v %v", origin, w.Code, er, w.Header())
		}
	}
}

func TestCrossSiteIdentityWrongLoginRejectedWithReadableBody(t *testing.T) {
	e := newEnv(t, crossWeb)
	for name, headers := range map[string][]string{
		"別人的登入":        {"Origin", otherBoard, tailscaleLoginHeader, "someone@example.com"},
		"沒有登入（tag 身分）": {"Origin", otherBoard},
		"大小寫不同":        {"Origin", otherBoard, tailscaleLoginHeader, strings.ToUpper(ownerLogin)},
		"兩個登入標頭":       {"Origin", otherBoard, tailscaleLoginHeader, "someone@example.com", tailscaleLoginHeader, ownerLogin},
		"前後空白":         {"Origin", otherBoard, tailscaleLoginHeader, " " + ownerLogin},
	} {
		w := crossPost(e.h, "/api/tasks/accept", `{"id":"SEC-1"}`, headers...)
		er := decodeErr(t, w)
		if w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || er.Message != "這台只接受主人的裝置" {
			t.Fatalf("%s：%d %+v", name, w.Code, er)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != otherBoard || !hasVaryOrigin(w.Header()) {
			t.Fatalf("%s：403 要帶 ACAO 讓前端讀得到原因：%v", name, w.Header())
		}
	}
	if e.api.calls != 0 {
		t.Fatalf("身分不對不能交給 api：%d 次", e.api.calls)
	}
}

func TestCrossSiteIdentityRequiresOwnerLoginSetting(t *testing.T) {
	e := newEnv(t, `allow_boards=["`+otherBoard+`"]`+"\n")
	w := crossPost(e.h, "/api/tasks/accept", `{"id":"SEC-1"}`, "Origin", otherBoard, tailscaleLoginHeader, ownerLogin)
	er := decodeErr(t, w)
	if w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || !strings.Contains(er.Message, "web.owner_login") || w.Header().Get("Access-Control-Allow-Origin") != otherBoard {
		t.Fatalf("沒設 owner_login：%d %+v %v", w.Code, er, w.Header())
	}
	if e.api.calls != 0 {
		t.Fatalf("沒設 owner_login 不能交給 api：%d 次", e.api.calls)
	}
}

func TestCrossSiteIdentityOwnerPassesWithACAOAndVary(t *testing.T) {
	e := newEnv(t, crossWeb)
	w := crossPost(e.h, "/api/tasks/accept", `{"id":"SEC-1","by":{"name":"life"}}`, "Origin", otherBoard, tailscaleLoginHeader, ownerLogin)
	if w.Code != http.StatusOK || e.api.acceptBy != boardActor {
		t.Fatalf("主人的裝置：%d by=%q %s", w.Code, e.api.acceptBy, w.Body.String())
	}
	if w.Header().Get("Access-Control-Allow-Origin") != otherBoard || !hasVaryOrigin(w.Header()) || w.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("成功的回應要帶 ACAO＋Vary、不帶 Credentials：%v", w.Header())
	}
	// 過了跨站檢查，後面的規則照舊：白名單外 404、不是 JSON 415，回應一樣帶 ACAO。
	if w := crossPost(e.h, "/api/engine/list", `{}`, "Origin", otherBoard, tailscaleLoginHeader, ownerLogin); w.Code != http.StatusNotFound || w.Header().Get("Access-Control-Allow-Origin") != otherBoard {
		t.Fatalf("白名單外：%d %v", w.Code, w.Header())
	}
}

// 改「誰能連進來」（允許清單、主人的登入）只能在那台自己的看板做：別台看板就算是主人的裝置也 403（帶 ACAO 讓前端讀得到原因）、不交給 api；
// 「還看哪些台」（set_sites）跨站照樣可以改；同源與不帶 Origin（CLI、curl）照舊可以改信任設定（WEB-79）。
func TestCrossSiteCannotChangeTrustSettings(t *testing.T) {
	e := newEnv(t, crossWeb)
	for _, c := range []struct{ path, body string }{
		{"/api/web/set_allow_boards", `{"allow_boards":["https://evil.example"]}`},
		{"/api/web/set_owner_login", `{"owner_login":"evil@example.com"}`},
		{"/api/web/answer_boot_prompt", `{"session":"s","id":"s#trust","choice":"信任這個資料夾"}`},
	} {
		w := crossPost(e.h, c.path, c.body, "Origin", otherBoard, tailscaleLoginHeader, ownerLogin)
		er := decodeErr(t, w)
		if w.Code != http.StatusForbidden || er.Code != sdk.ErrorCodeForbidden || er.Message != "這個設定只能在那台自己的看板改" {
			t.Fatalf("%s 跨站：%d %+v", c.path, w.Code, er)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != otherBoard || !hasVaryOrigin(w.Header()) {
			t.Fatalf("%s：403 要帶 ACAO 讓前端讀得到原因：%v", c.path, w.Header())
		}
		// 預檢照樣 204（瀏覽器才送得出 POST、讀得到上面那句原因）。
		if w := preflight(e.h, c.path, otherBoard); w.Code != http.StatusNoContent {
			t.Fatalf("%s 預檢：%d", c.path, w.Code)
		}
	}
	if e.api.calls != 0 {
		t.Fatalf("跨站改信任設定不能交給 api：%d 次", e.api.calls)
	}
	if w := crossPost(e.h, "/api/web/set_sites", `{"sites":[]}`, "Origin", otherBoard, tailscaleLoginHeader, ownerLogin); w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != otherBoard {
		t.Fatalf("跨站改台清單要可以：%d %s", w.Code, w.Body.String())
	}
	for _, c := range [][]string{
		{"Origin", "https://genie-amy-office.example.ts.net"},
		{"Host", "127.0.0.1:18860", "Origin", "http://127.0.0.1:18860"},
		{},
	} {
		for _, path := range []string{"/api/web/set_allow_boards", "/api/web/set_owner_login"} {
			if w := crossPost(e.h, path, `{}`, c...); w.Code != http.StatusOK {
				t.Fatalf("同源 %q 改 %s：%d %s", c, path, w.Code, w.Body.String())
			}
		}
	}
}

func TestCORSStreamResponseCarriesACAO(t *testing.T) {
	e := newEnv(t, crossWeb)
	srv := httptest.NewServer(e.h)
	defer srv.Close()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/session/watch", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", otherBoard)
	req.Header.Set(tailscaleLoginHeader, ownerLogin)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") || !strings.Contains(string(body), `data: {"session"`) {
		t.Fatalf("串流：%d %v %s", res.StatusCode, res.Header, body)
	}
	if res.Header.Get("Access-Control-Allow-Origin") != otherBoard || !hasVaryOrigin(res.Header) {
		t.Fatalf("串流回應要帶 ACAO＋Vary：%v", res.Header)
	}
}

// 不在清單（含長得像的寫法）一律在交給 api 之前擋掉，回應不帶 ACAO；主人的登入也救不了。
func TestCrossSiteUnlistedOriginPostRejectedBeforeHandler(t *testing.T) {
	e := newEnv(t, crossWeb)
	for _, origins := range [][]string{
		{"https://evil.example"},
		{"null"},
		{otherBoard + ".evil.example"},
		{"http://genie-amy-mini.example.ts.net"},  // http 不是 https
		{"https://GENIE-AMY-MINI.example.ts.net"}, // 瀏覽器不會送大寫
		{otherBoard + ":443"},
		{otherBoard + ":8443"},
		{otherBoard + "/"},
		{otherBoard + ", https://evil.example"},
		{"https://evil.example", otherBoard}, // 兩個 Origin 標頭
		{otherBoard, "https://evil.example"},
		{"https://evil@genie-amy-office.example.ts.net"}, // 帳密＋自己的 host
	} {
		headers := []string{tailscaleLoginHeader, ownerLogin}
		for _, o := range origins {
			headers = append(headers, "Origin", o)
		}
		w := crossPost(e.h, "/api/tasks/accept", `{"id":"SEC-1"}`, headers...)
		if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Message != "不接受從別的網站送來的請求" || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%q：%d %+v %v", origins, w.Code, er, w.Header())
		}
	}
	if e.api.calls != 0 {
		t.Fatalf("被擋的請求不能交給 api：%d 次", e.api.calls)
	}
}

func TestCrossSiteSameOriginNeedsNoIdentity(t *testing.T) {
	e := newEnv(t, crossWeb)
	// public_url（office 的網址）、loopback 的 Host（本機 e2e、curl）、tailnet 名字的 Host（https）、沒有 Origin：不查身分、不帶 ACAO。
	for _, c := range [][]string{
		{"Origin", "https://genie-amy-office.example.ts.net"},
		{"Host", "127.0.0.1:18860", "Origin", "http://127.0.0.1:18860"},
		{"Host", "localhost:18860", "Origin", "http://localhost:18860"},
		{"Host", "[::1]:18860", "Origin", "http://[::1]:18860"},
		{"Host", "my-mac.example.ts.net", "Origin", "https://my-mac.example.ts.net"},
		{},
	} {
		w := crossPost(e.h, "/api/web/get_pages", "{}", c...)
		if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("同源 %q：%d %v", c, w.Code, w.Header())
		}
	}
	before := e.api.calls
	// 不算同源：DNS rebinding（Host 與 Origin 都是攻擊者的網域）、public_url 的 host 換成 http、tailnet 名字用 http、loopback 別的 port。
	for _, c := range [][]string{
		{"Host", "evil.example:18860", "Origin", "http://evil.example:18860"},
		{"Origin", "http://genie-amy-office.example.ts.net"},
		{"Host", "my-mac.example.ts.net", "Origin", "http://my-mac.example.ts.net"},
		{"Host", "127.0.0.1:18860", "Origin", "http://127.0.0.1:9999"},
	} {
		w := crossPost(e.h, "/api/web/get_pages", "{}", c...)
		if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("不是同源 %q：%d %v", c, w.Code, w.Header())
		}
	}
	if e.api.calls != before {
		t.Fatalf("不是同源的請求不能交給 api：多了 %d 次", e.api.calls-before)
	}
	// 同源的預檢不是 CORS：照舊交給 /api 的規則（415）。
	r := httptest.NewRequest(http.MethodOptions, "/api/web/get_pages", nil)
	r.Host = "127.0.0.1:18860"
	r.Header.Set("Origin", "http://127.0.0.1:18860")
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("同源 OPTIONS：%d %v", w.Code, w.Header())
	}
}

func TestCrossSiteSettingsReloadWithoutRestart(t *testing.T) {
	e := newEnv(t, `public_url="https://genie-amy-office.example.ts.net"`+"\n")
	call := func() *httptest.ResponseRecorder {
		return crossPost(e.h, "/api/web/get_pages", "{}", "Origin", otherBoard, tailscaleLoginHeader, ownerLogin)
	}
	if w := call(); w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("還沒允許：%d %v", w.Code, w.Header())
	}
	rewriteWeb(t, e, crossWeb)
	if w := call(); w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != otherBoard {
		t.Fatalf("加進 allow_boards 之後（沒重開）：%d %v %s", w.Code, w.Header(), w.Body.String())
	}
	rewriteWeb(t, e, `public_url="https://genie-amy-office.example.ts.net"`+"\n"+`allow_boards=["`+otherBoard+`"]`+"\n"+`owner_login="other@example.com"`+"\n")
	if w := call(); w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != otherBoard {
		t.Fatalf("改了 owner_login 之後：%d %v", w.Code, w.Header())
	}
	// 設定壞掉：跨站一律不開（不沿用舊的允許清單），同源（public_url）照常。
	rewriteWeb(t, e, crossWeb+"allow_boards = 3\n")
	if w := call(); w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("設定壞掉時跨站要擋：%d %v", w.Code, w.Header())
	}
	if w := crossPost(e.h, "/api/web/get_pages", "{}", "Origin", "https://genie-amy-office.example.ts.net"); w.Code != http.StatusOK {
		t.Fatalf("設定壞掉時同源（public_url）照常：%d", w.Code)
	}
	rewriteWeb(t, e, crossWeb)
	if w := call(); w.Code != http.StatusOK {
		t.Fatalf("修好之後：%d %s", w.Code, w.Body.String())
	}
}

func TestCrossSiteUploadSameRules(t *testing.T) {
	e := newEnv(t, crossWeb)
	shared := filepath.Join(e.home, "runtime", "share")
	if w := preflight(e.h, uploadPath, otherBoard); w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "X-Filename") {
		t.Fatalf("上傳預檢：%d %v", w.Code, w.Header())
	}
	w := upload(e.h, []byte("x"), "Origin", otherBoard, tailscaleLoginHeader, "someone@example.com")
	if er := decodeErr(t, w); w.Code != http.StatusForbidden || er.Message != "這台只接受主人的裝置" || w.Header().Get("Access-Control-Allow-Origin") != otherBoard {
		t.Fatalf("上傳、別人的登入：%d %+v %v", w.Code, er, w.Header())
	}
	w = upload(e.h, []byte("x"), "Origin", "https://evil.example", tailscaleLoginHeader, ownerLogin)
	if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("上傳、不在清單：%d %v", w.Code, w.Header())
	}
	if entries, _ := os.ReadDir(shared); len(entries) != 0 {
		t.Fatalf("被擋的上傳不該留檔案，共 %d 個", len(entries))
	}
	w = upload(e.h, []byte("x"), "Origin", otherBoard, tailscaleLoginHeader, ownerLogin, "X-Filename", "a.png")
	if w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != otherBoard || !hasVaryOrigin(w.Header()) {
		t.Fatalf("上傳、主人的裝置：%d %v %s", w.Code, w.Header(), w.Body.String())
	}
}

func TestCrossSiteDoctorWarnsWithoutOwnerLogin(t *testing.T) {
	run := func(extra string) []app.CheckResult {
		e := newEnv(t, extra)
		cs := e.m.Checks()
		if len(cs) != 1 || cs[0].Name != "config.web_cross_site" || cs[0].RunAll == nil {
			t.Fatalf("健檢登記：%+v", cs)
		}
		return cs[0].RunAll(context.Background())
	}
	got := run(`allow_boards=["` + otherBoard + `"]` + "\n")
	if len(got) != 1 || got[0].Status != app.StatusWarn || !strings.Contains(got[0].Message, "web.owner_login") {
		t.Fatalf("有 allow_boards 沒 owner_login 要黃字：%+v", got)
	}
	if got := run(crossWeb); len(got) != 0 {
		t.Fatalf("都設了不報：%+v", got)
	}
	if got := run(""); len(got) != 0 {
		t.Fatalf("沒開跨站不報：%+v", got)
	}
}

// home.toml 沒動、別的設定檔（trust.toml）壞掉又修好：最多 accessMaxAge 之後跟上，不會一直關著。
func TestCrossSiteSettingsRecoverAfterOtherFileFixed(t *testing.T) {
	e := newEnv(t, crossWeb)
	call := func() int {
		return crossPost(e.h, "/api/web/get_pages", "{}", "Origin", otherBoard, tailscaleLoginHeader, ownerLogin).Code
	}
	if c := call(); c != http.StatusOK {
		t.Fatalf("一開始：%d", c)
	}
	old := accessMaxAge
	accessMaxAge = 0
	t.Cleanup(func() { accessMaxAge = old })
	trust := filepath.Join(e.home, "settings", "trust.toml")
	write(t, trust, "這不是 toml = = =")
	if c := call(); c != http.StatusForbidden {
		t.Fatalf("trust.toml 壞掉時跨站要關：%d", c)
	}
	if err := os.Remove(trust); err != nil {
		t.Fatal(err)
	}
	if c := call(); c != http.StatusOK {
		t.Fatalf("修好之後要恢復：%d", c)
	}
}

// web.trust_self（WEB-130）：tag 身分的機器沒有 Tailscale-User-Login，從這台自己的瀏覽器開別台看板連回來要能過；
// 預設關；只認「剛好一個、沒逗號、不含空白、這台網卡上的 tailnet 位址」；帶了別人的登入就照登入判斷、不退回來源位址。
func TestCrossSiteTrustSelfAcceptsOnlyThisMachinesOwnTailnetAddress(t *testing.T) {
	const self, self6 = "100.64.0.10", "fd7a:115c:a1e0::10"
	nic := func(cidrs ...string) func() ([]net.Addr, error) {
		return func() ([]net.Addr, error) {
			var out []net.Addr
			for _, c := range cidrs {
				ip, n, _ := net.ParseCIDR(c)
				n.IP = ip
				out = append(out, n)
			}
			return out, nil
		}
	}
	old := localAddrs
	t.Cleanup(func() { localAddrs = old })
	// 網卡上有 tailnet 位址，也有一般的內網、本機迴路、docker 位址（這些都不是 tailnet，不能當成「自己」）。
	localAddrs = nic(self+"/32", self6+"/128", "192.168.1.5/24", "127.0.0.1/8", "172.17.0.1/16", "10.0.0.7/8", "::1/128")

	post := func(e *env, path string, headers ...string) int {
		return crossPost(e.h, path, `{"id":"SEC-1","by":{"name":"life"}}`, append([]string{"Origin", otherBoard}, headers...)...).Code
	}
	const accept = "/api/tasks/accept"
	off := newEnv(t, crossWeb)
	if c := post(off, accept, "X-Forwarded-For", self); c != http.StatusForbidden {
		t.Fatalf("預設關：來源是自己也要擋：%d", c)
	}
	if c := post(off, accept, tailscaleLoginHeader, ownerLogin); c != http.StatusOK {
		t.Fatalf("預設關：主人的登入照舊能過：%d", c)
	}
	on := newEnv(t, crossWeb+"trust_self=true\n")
	for name, h := range map[string][]string{
		"這台自己（IPv4）":             {"X-Forwarded-For", self},
		"這台自己（IPv6）":             {"X-Forwarded-For", self6},
		"主人的登入（trust_self 開著照舊）": {tailscaleLoginHeader, ownerLogin},
	} {
		if c := post(on, accept, h...); c != http.StatusOK {
			t.Fatalf("%s：要過，得到 %d", name, c)
		}
	}
	if w := upload(on.h, []byte("x"), "Origin", otherBoard, "X-Forwarded-For", self, "X-Filename", "a.png"); w.Code != http.StatusOK {
		t.Fatalf("上傳走同一個閘門，這台自己的請求要過：%d %s", w.Code, w.Body.String())
	}
	if w := upload(off.h, []byte("x"), "Origin", otherBoard, "X-Forwarded-For", self, "X-Filename", "a.png"); w.Code != http.StatusForbidden {
		t.Fatalf("上傳、預設關：要擋：%d", w.Code)
	}
	for name, headers := range map[string][]string{
		"別台的 tailnet 位址":       {"X-Forwarded-For", "100.64.0.20"},
		"不在網卡上的位址":             {"X-Forwarded-For", "1.2.3.4"},
		"網卡上的內網位址（不是 tailnet）": {"X-Forwarded-For", "192.168.1.5"},
		"網卡上的本機迴路":             {"X-Forwarded-For", "127.0.0.1"},
		"網卡上的 docker 位址":       {"X-Forwarded-For", "172.17.0.1"},
		"網卡上的 IPv6 迴路":         {"X-Forwarded-For", "::1"},
		"沒有來源位址":               {},
		"空的來源位址":               {"X-Forwarded-For", ""},
		"兩個標頭":                 {"X-Forwarded-For", "1.2.3.4", "X-Forwarded-For", self},
		"逗號串（前面偽造）":            {"X-Forwarded-For", "1.2.3.4, " + self},
		"逗號串（自己在前）":            {"X-Forwarded-For", self + ", 1.2.3.4"},
		"不是位址":                 {"X-Forwarded-For", "localhost"},
		"前後有空白":                {"X-Forwarded-For", " " + self},
		"結尾有空白":                {"X-Forwarded-For", self + " "},
		"帶 port":               {"X-Forwarded-For", self + ":443"},
		"帶 zone":               {"X-Forwarded-For", self6 + "%utun4"},
		"經過 Funnel":            {"X-Forwarded-For", self, "Tailscale-Funnel-Request", "?1"},
		"帶了別人的登入，不退回來源位址":      {"X-Forwarded-For", self, tailscaleLoginHeader, "someone@example.com"},
	} {
		if c := post(on, accept, headers...); c != http.StatusForbidden {
			t.Fatalf("%s：要 403，得到 %d", name, c)
		}
	}
	// 讀不到網卡位址：擋下、不能 panic。
	localAddrs = func() ([]net.Addr, error) { return nil, fmt.Errorf("no interfaces") }
	if c := post(on, accept, "X-Forwarded-For", self); c != http.StatusForbidden {
		t.Fatalf("讀不到網卡：要擋：%d", c)
	}
	localAddrs = nic(self + "/32")
	// 沒設主人登入就不開放跨站，trust_self 不能繞過。
	noOwner := newEnv(t, `public_url="https://genie-amy-office.example.ts.net"`+"\n"+`allow_boards=["`+otherBoard+`"]`+"\ntrust_self=true\n")
	if c := post(noOwner, accept, "X-Forwarded-For", self); c != http.StatusForbidden {
		t.Fatalf("沒設 owner_login：trust_self 不能繞過：%d", c)
	}
	// 改信任設定照樣只能在那台自己的看板改，trust_self 的請求也一樣（WEB-79）。
	for _, path := range []string{"/api/web/set_owner_login", "/api/web/set_allow_boards"} {
		before := on.api.calls
		if c := post(on, path, "X-Forwarded-For", self); c != http.StatusForbidden || on.api.calls != before {
			t.Fatalf("%s：trust_self 的跨站請求也不能改信任設定：%d，api 多了 %d 次", path, c, on.api.calls-before)
		}
	}
}

// trust_self 開了卻沒有同時設 allow_boards 與 owner_login：沒有作用，doctor 黃字提醒。
func TestCrossSiteDoctorWarnsTrustSelfWithoutAllowOrOwner(t *testing.T) {
	if crossSiteWarning(webAccess{trustSelf: true}) == "" || crossSiteWarning(webAccess{trustSelf: true, owner: "a@b"}) == "" ||
		crossSiteWarning(webAccess{trustSelf: true, allow: map[string]bool{"https://x.example.ts.net": true}}) == "" {
		t.Fatal("trust_self 缺 allow_boards 或 owner_login 要提醒")
	}
	if w := crossSiteWarning(webAccess{trustSelf: true, owner: "a@b", allow: map[string]bool{"https://x.example.ts.net": true}}); w != "" {
		t.Fatalf("三樣都設了不用提醒：%q", w)
	}
}
