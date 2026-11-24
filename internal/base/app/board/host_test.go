package board

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func getHost(h http.Handler, host, target string, headers ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.Host = host
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func hostTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><div id=root></div>")},
		"sw.js":         {Data: []byte("self.skipWaiting()")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
}

// S3-2b：/api 以外的路由（頁面、manifest、sw.js、頭像、分享檔、套件檔）Host 不是 public_url、loopback、*.ts.net 就 403，
// 擋 DNS rebinding（攻擊者的網域改解析到 127.0.0.1，讀分享檔、頭像）；別台看板的 <img> 與 no-cors 探測照常。
func TestCrossSiteHostCheckBlocksRebindingOnPages(t *testing.T) {
	e := newEnv(t, crossWeb)
	png := []byte("\x89PNG\r\n\x1a\nfake")
	write(t, filepath.Join(e.home, "personas", "avatars", "builder.png"), string(png))
	write(t, filepath.Join(e.home, "runtime", "share", "a.png"), string(png))
	e.m.static = hostTestFS()
	paths := []string{"/", "/chat/code", "/manifest.webmanifest", "/sw.js", "/assets/app.js", "/icons/icon-192.png", "/avatars/code.png", "/share/a.png", "/pkg-files/x/y.png", "/pkg-bundles/x.js"}
	for _, host := range []string{"evil.example", "evil.example:18860", "genie-amy-office.example.ts.net.evil.example", "ts.net", "evil.ts.net.example", "127.0.0.1.evil.example", "localhost.evil.example", "[::2]:18860", "::1", "100.64.0.30:18860", "localhost:99999", "evil.tail0000.ts.net"} {
		for _, p := range paths {
			w := getHost(e.h, host, p)
			if w.Code != http.StatusForbidden || strings.Contains(w.Body.String(), "PNG") || w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatalf("Host %q %s：%d %q", host, p, w.Code, w.Body.String())
			}
		}
	}
	if hostKindOf("", webAccess{}) != hostUntrusted {
		t.Fatal("沒有 Host 不能算可信")
	}
	// 會過：public_url 的 host、這台自己的服務網址（*.ts.net；別台看板的 <img src> 頭像與 no-cors 探測 manifest 都是這種）、loopback。
	for _, host := range []string{"genie-amy-office.example.ts.net", "GENIE-AMY-OFFICE.example.ts.net", "my-mac.example.ts.net:443", "127.0.0.1:18860", "localhost:18860", "[::1]:18860", "127.0.0.1"} {
		w := getHost(e.h, host, "/manifest.webmanifest", "Origin", otherBoard, "Sec-Fetch-Mode", "no-cors")
		if w.Code != http.StatusOK {
			t.Fatalf("Host %q 的 manifest（no-cors 探測）：%d %q", host, w.Code, w.Body.String())
		}
		w = getHost(e.h, host, "/avatars/code.png", "Origin", otherBoard, "Sec-Fetch-Mode", "no-cors")
		if w.Code != http.StatusOK || w.Body.String() != string(png) {
			t.Fatalf("Host %q 的頭像（別台看板的 <img>）：%d", host, w.Code)
		}
		if w := getHost(e.h, host, "/share/a.png"); w.Code != http.StatusOK {
			t.Fatalf("Host %q 的分享檔：%d", host, w.Code)
		}
		if w := getHost(e.h, host, "/chat/code"); w.Code != http.StatusOK {
			t.Fatalf("Host %q 的頁面：%d", host, w.Code)
		}
	}
	// /api 與上傳不經過 Host 檢查（瀏覽器的 POST 都帶 Origin，由跨站檢查擋）：curl 不帶 Origin 照舊。
	if w := crossPost(e.h, "/api/web/get_pages", "{}", "Host", "evil.example"); w.Code != http.StatusOK {
		t.Fatalf("不帶 Origin 的 /api：%d", w.Code)
	}
	if w := crossPost(e.h, "/api/web/get_pages", "{}", "Host", "evil.example", "Origin", "http://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("DNS rebinding 的 /api POST：%d", w.Code)
	}
}

// hostKindOf 的邊界：大小寫、結尾點、空 port、超過範圍的 port、中括號只接 IPv6、別的 tailnet、自訂網域的預設 port。
func TestCrossSiteHostKindTable(t *testing.T) {
	tail := webAccess{publicHost: "genie-amy-office.example.ts.net", publicScheme: "https", tailnet: tailnetOf("genie-amy-office.example.ts.net")}
	custom := webAccess{publicHost: canonicalHost("Board.Example.com:443", "https"), publicScheme: "https"}
	cases := []struct {
		host string
		a    webAccess
		want hostKind
	}{
		{"[::1]", webAccess{}, hostLoopback},
		{"[::1]:18860", webAccess{}, hostLoopback},
		{"LOCALHOST", webAccess{}, hostLoopback},
		{"localhost:65535", webAccess{}, hostLoopback},
		{"localhost.", webAccess{}, hostUntrusted},
		{"x.ts.net.", webAccess{}, hostUntrusted},
		{"localhost:", webAccess{}, hostUntrusted},
		{"localhost:65536", webAccess{}, hostUntrusted},
		{"[127.0.0.1]", webAccess{}, hostUntrusted},
		{"[127.0.0.1]:80", webAccess{}, hostUntrusted},
		{"[evil.ts.net]", webAccess{}, hostUntrusted},
		{"[evil.ts.net]:443", webAccess{}, hostUntrusted},
		{"[::2]", webAccess{}, hostUntrusted},
		{"anyone.tail0000.ts.net", webAccess{}, hostTailnet}, // 沒設 public_url：任何 *.ts.net
		{"my-mini.example.ts.net", tail, hostTailnet},
		{"MY-MINI.EXAMPLE.TS.NET:443", tail, hostTailnet},
		{"genie-amy-office.example.ts.net:443", tail, hostPublic},
		{"evil.tail0000.ts.net", tail, hostUntrusted}, // 別的 tailnet（Funnel 名字查得到）
		{"example.ts.net", tail, hostUntrusted},
		{"board.example.com", custom, hostPublic},
		{"BOARD.example.com:443", custom, hostPublic},
		{"board.example.com:8443", custom, hostUntrusted},
		{"other.example.com", custom, hostUntrusted},
	}
	for _, c := range cases {
		if got := hostKindOf(c.host, c.a); got != c.want {
			t.Errorf("hostKindOf(%q)=%d，要 %d", c.host, got, c.want)
		}
	}
	if custom.publicHost != "board.example.com" {
		t.Fatalf("public_url 的 host 沒正規化：%q", custom.publicHost)
	}
	if tailnetOf("example.ts.net") != "" || tailnetOf("board.example.com") != "" {
		t.Fatal("不是 <名字>.<tailnet>.ts.net 的不收窄")
	}
}

// public_url 是自訂網域（不是 *.ts.net）時：那個網域能開（設定帶 :443 也一樣），別的網域不行。
func TestCrossSiteHostCheckCustomDomain(t *testing.T) {
	e := newEnv(t, `public_url="https://Board.Example.com:443"`+"\n")
	e.m.static = hostTestFS()
	for host, want := range map[string]int{"board.example.com": http.StatusOK, "board.example.com:443": http.StatusOK, "other.example.com": http.StatusForbidden, "evil.tail0000.ts.net": http.StatusOK} {
		if w := getHost(e.h, host, "/"); w.Code != want {
			t.Fatalf("Host %q：%d，要 %d", host, w.Code, want)
		}
	}
	// 同源：Origin 等於 public_url（沒帶預設 port）的 POST 不用身分。
	if w := crossPost(e.h, "/api/web/get_pages", "{}", "Host", "board.example.com", "Origin", "https://board.example.com"); w.Code != http.StatusOK {
		t.Fatalf("自訂網域的同源 POST：%d %q", w.Code, w.Body.String())
	}
}

// 沒設 public_url 的家：服務網址（*.ts.net）與 loopback 照樣能開看板，別的網域不行。
func TestCrossSiteHostCheckWithoutPublicURL(t *testing.T) {
	e := newEnv(t, "")
	e.m.static = hostTestFS()
	for host, want := range map[string]int{"genie-amy-mini.example.ts.net": http.StatusOK, "127.0.0.1:18860": http.StatusOK, "genie-amy-mini.example": http.StatusForbidden} {
		if w := getHost(e.h, host, "/"); w.Code != want {
			t.Fatalf("Host %q：%d，要 %d", host, w.Code, want)
		}
	}
}
