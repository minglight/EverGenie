package board

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 看板連多台（S3-2，2026-11-01，計畫 2026-11-01-board-multi-home 設計決定 1）：別台的看板（瀏覽器）直接呼叫這台的 /api/*。
//
//   - Origin 沒有、或等於自己（請求的 Host 或 web.public_url 的 host）：照舊，不查身分。
//   - Origin 在 web.allow_boards：OPTIONS 回 204＋CORS 標頭；其他方法要 Tailscale-User-Login 剛好一個、而且等於 web.owner_login（web.trust_self 開著時，沒帶登入的、來自這台自己的請求也算，WEB-130）
//     （沒設 owner_login＝不開放），不符回 403 但帶 ACAO（前端讀得到原因）；過了的回應（含串流）都帶 ACAO＋Vary。
//   - 其他 Origin（含 `null`、多個 Origin 標頭、不是標準寫法的）：403，不帶 ACAO。
//
// web.trust_self（WEB-130）：這台是 tag 身分的機器（Tailscale 不帶登入標頭）時，從這台自己的瀏覽器連回來的請求，
// 沒帶登入標頭、而且來源位址是這台自己的 tailnet 位址就視為主人。
//
// 檢查一律在交給 handler 之前（瀏覽器沒拿到 ACAO 時請求其實已經送到、執行了，S3-0b 量過；不能靠瀏覽器擋副作用）。
// Tailscale-User-Login 是 Tailscale 服務（serve）轉進來時補上的、用戶端偽造的會被刪掉（S3-0a 量過）；
// 但看板只聽 127.0.0.1，本機程式直接打就能偽造任何標頭（D-24 已知限制，跟 api 的 socket 一樣）。

const (
	tailscaleLoginHeader = "Tailscale-User-Login"
	corsAllowMethods     = "POST"
	corsAllowHeaders     = "Content-Type, X-Requested-With, X-Filename"
	corsMaxAge           = "600" // 秒；沒給時 Chromium 只快取 5 秒，每個請求都要先預檢一次（S3-0b）
)

// webAccess 是跨站檢查要的 [web] 設定。
type webAccess struct {
	publicHost   string          // web.public_url 的 host（小寫、拿掉預設 port）；同源判斷用
	publicScheme string          // web.public_url 的 scheme（小寫；同源要一樣）
	tailnet      string          // public_url 是 *.<tailnet>.ts.net 時的 ".<tailnet>.ts.net"；空的＝任何 *.ts.net
	allow        map[string]bool // web.allow_boards（已經是 platform.NormalizeOrigin 的寫法）
	owner        string          // web.owner_login
	trustSelf    bool            // web.trust_self
}

// accessMaxAge：快取最多用這麼久就重讀一次（保留修改時間的複製工具、trust.toml 這類 home.toml 以外的檔壞掉又修好，也會跟上）。
var accessMaxAge = 30 * time.Second // 測試可改

// accessCache 每個請求看一次 home.toml：同一個檔（inode）、修改時間與大小都沒變、而且讀過不到 accessMaxAge 就用上一次的結果。
type accessCache struct {
	mu       sync.Mutex
	dir      string
	info     os.FileInfo
	loadedAt time.Time
	failed   bool // 上一次讀不進來（只在變成壞掉的那一次寫 WARN）
	cur      webAccess
}

// access 回現在的跨站設定。家的設定讀不到或壞掉時：同源判斷沿用上一次的 public_url，跨站一律不開（不沿用舊的允許清單）。
func (m *Module) access() webAccess {
	dir := m.homeDir()
	c := &m.webCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if dir == "" {
		// 一個一個欄位清（不能整個 *c = accessCache{}：會連拿著的鎖一起蓋掉）。
		c.dir, c.info, c.loadedAt, c.failed, c.cur = "", nil, time.Time{}, false, webAccess{}
		return c.cur
	}
	now := time.Now()
	info, err := os.Stat(filepath.Join(dir, "home.toml"))
	if err == nil && c.info != nil && c.dir == dir && os.SameFile(c.info, info) && info.ModTime().Equal(c.info.ModTime()) &&
		info.Size() == c.info.Size() && now.Sub(c.loadedAt) < accessMaxAge {
		return c.cur
	}
	keep := webAccess{}
	if c.dir == dir {
		keep = webAccess{publicHost: c.cur.publicHost, publicScheme: c.cur.publicScheme, tailnet: c.cur.tailnet}
	}
	c.dir, c.info, c.loadedAt = dir, nil, now
	if err != nil {
		c.cur = keep
		return c.cur
	}
	h, lerr := platform.LoadHome(dir)
	c.info = info
	if lerr != nil {
		if !c.failed {
			m.logger().Warn("家的設定讀不進來，別的看板暫時不能連進來", "error", lerr)
		}
		c.failed, c.cur = true, keep
		return c.cur
	}
	c.failed = false
	next := webAccess{owner: h.Web.OwnerLogin, trustSelf: h.Web.TrustSelf, allow: map[string]bool{}}
	if u, perr := url.Parse(h.PublicBase()); perr == nil && u.Host != "" {
		next.publicScheme = strings.ToLower(u.Scheme)
		next.publicHost = canonicalHost(u.Host, next.publicScheme)
		next.tailnet = tailnetOf(u.Hostname())
	}
	for _, o := range h.Web.AllowBoards {
		next.allow[o] = true
	}
	c.cur = next
	return c.cur
}

// sameOrigin：Origin 是 scheme://host[:port]（不帶帳密、路徑、查詢），而且
//   - 等於 web.public_url 的 scheme 與 host；或
//   - host 等於請求的 Host，而 Host 是 loopback（127.0.0.1、localhost、::1，本機 e2e 與 curl）或 tailnet 的名字（*.ts.net，只認 https）。
//
// 不直接相信任意的 Host：DNS rebinding（攻擊者的網域改解析到 127.0.0.1）時瀏覽器送的 Host 與 Origin 都是攻擊者的網域，
// 照 S3 之前「Origin 的 host 等於 Host 就算同源」會整套放行（資安 review 2026-11-02）。
func sameOrigin(origin string, r *http.Request, a webAccess) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(origin, "#") {
		return false
	}
	if a.publicHost != "" && strings.EqualFold(u.Scheme, a.publicScheme) && canonicalHost(u.Host, a.publicScheme) == a.publicHost {
		return true
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	switch hostKindOf(r.Host, a) {
	case hostLoopback:
		return true
	case hostTailnet:
		return strings.EqualFold(u.Scheme, "https")
	}
	return false
}

type hostKind int

const (
	hostUntrusted hostKind = iota
	hostLoopback           // 127.0.0.1、localhost、::1（本機 e2e、curl、CLI）
	hostTailnet            // *.ts.net（經 Tailscale 服務或機器的 serve 轉進來）
	hostPublic             // 剛好等於 web.public_url 的 host
)

// tailnetName 是 *.ts.net 的小寫 ASCII 網域（一段一段英數字與中間的連字號）。
var tailnetName = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)+ts\.net$`)

// canonicalHost 把 host[:port] 轉小寫，port 是這個 scheme 的預設值（https 443、http 80）就拿掉（瀏覽器送的 Host 與 Origin 不帶預設 port）。
func canonicalHost(host, scheme string) string {
	host = strings.ToLower(host)
	if h, port, err := net.SplitHostPort(host); err == nil && ((scheme == "https" && port == "443") || (scheme == "http" && port == "80")) {
		if strings.Contains(h, ":") {
			return "[" + h + "]"
		}
		return h
	}
	return host
}

// tailnetOf：public_url 的名字是 <機器或服務>.<tailnet>.ts.net 時回 ".<tailnet>.ts.net"（只信同一個 tailnet 的名字）；其他情況回空的（任何 *.ts.net）。
func tailnetOf(name string) string {
	name = strings.ToLower(name)
	labels := strings.Split(name, ".")
	if !tailnetName.MatchString(name) || len(labels) < 4 {
		return ""
	}
	return "." + strings.Join(labels[len(labels)-3:], ".")
}

// hostKindOf 判斷請求的 Host 是哪一種；不是這三種的（攻擊者的網域改解析到 127.0.0.1，DNS rebinding）是 hostUntrusted。
func hostKindOf(host string, a webAccess) hostKind {
	if host == "" {
		return hostUntrusted
	}
	if a.publicHost != "" && canonicalHost(host, a.publicScheme) == a.publicHost {
		return hostPublic
	}
	name := host
	if h, port, err := net.SplitHostPort(host); err == nil {
		if _, perr := strconv.ParseUint(port, 10, 16); perr != nil {
			return hostUntrusted
		}
		name = h
	} else if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		name = host[1 : len(host)-1]
	} else if strings.Contains(host, ":") {
		return hostUntrusted
	}
	if strings.HasPrefix(host, "[") {
		// 中括號只接 IPv6 位址（`[127.0.0.1]`、`[x.example.ts.net]` 不算）。
		if ip := net.ParseIP(name); ip == nil || !strings.Contains(name, ":") {
			return hostUntrusted
		}
	}
	name = strings.ToLower(name)
	switch {
	case name == "127.0.0.1" || name == "localhost" || name == "::1":
		return hostLoopback
	case tailnetName.MatchString(name) && (a.tailnet == "" || strings.HasSuffix(name, a.tailnet)):
		return hostTailnet
	}
	return hostUntrusted
}

// gateHost 在 /api/* 與上傳以外的路由（頁面、manifest、sw.js、頭像、分享檔、套件檔）交給 handler 之前跑：
// Host 不是 public_url 的 host、loopback 或 *.ts.net 就 403（S3-2b，擋 DNS rebinding 讀分享檔與頭像）。
// 不看 Origin：別台看板的 `<img src>` 頭像、no-cors 探測 manifest 送來的 Host 是這台自己的服務網址（*.ts.net），照常通過。
// /api/* 與上傳不經過這裡：瀏覽器的 POST 都帶 Origin，DNS rebinding 的 Origin 不算同源也不在清單，gateCrossSite 已經擋下。
func (m *Module) gateHost(w http.ResponseWriter, r *http.Request) bool {
	if hostKindOf(r.Host, m.access()) != hostUntrusted {
		return true
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusForbidden)
	_, _ = io.WriteString(w, "不接受這個網址：請用看板的網址（web.public_url）打開\n")
	return false
}

// allowedBoard：Origin 是標準寫法（瀏覽器送的樣子，NormalizeOrigin 之後不變）而且在 allow_boards。
// 大寫、`:443`、結尾斜線、`null`、多個值併成一串這類寫法都比對不到。
func allowedBoard(origin string, a webAccess) bool {
	n, ok := platform.NormalizeOrigin(origin)
	return ok && n == origin && a.allow[n]
}

// gateCrossSite 在 /api/* 與上傳交給 handler 之前跑：回 false 表示已經回應（擋下或答完預檢），不能再往下做。
func (m *Module) gateCrossSite(w http.ResponseWriter, r *http.Request) bool {
	origins := r.Header.Values("Origin")
	if len(origins) == 0 {
		return true // curl、CLI、同源的 GET 這類不帶 Origin 的
	}
	a := m.access()
	origin := origins[0]
	if len(origins) == 1 && sameOrigin(origin, r, a) {
		return true
	}
	if len(origins) > 1 || !allowedBoard(origin, a) {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "不接受從別的網站送來的請求"})
		return false
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Add("Vary", "Origin")
	if r.Method == http.MethodOptions {
		h.Set("Access-Control-Allow-Methods", corsAllowMethods)
		h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
		h.Set("Access-Control-Max-Age", corsMaxAge)
		w.WriteHeader(http.StatusNoContent)
		return false
	}
	if a.owner == "" {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "這台還沒設定主人的 tailnet 登入（web.owner_login），不接受別的看板連進來"})
		return false
	}
	logins := r.Header.Values(tailscaleLoginHeader)
	if !(len(logins) == 1 && logins[0] == a.owner) && !(len(logins) == 0 && a.trustSelf && fromThisMachine(r)) {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "這台只接受主人的裝置"})
		return false
	}
	if service, method, ok := splitAPIPath(r.URL.Path); ok && service == "web" && localOnlyWebMethods[method] {
		writeStatusError(w, http.StatusForbidden, &sdk.Error{Code: sdk.ErrorCodeForbidden, Message: "這個設定只能在那台自己的看板改"})
		return false
	}
	return true
}

// localAddrs 回這台機器的網卡位址（測試可換）。
var localAddrs = net.InterfaceAddrs

// tailnetRanges：Tailscale 給節點的位址範圍（CGNAT 100.64.0.0/10、ULA fd7a:115c:a1e0::/48）。
var tailnetRanges = []*net.IPNet{
	{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)},
	{IP: net.ParseIP("fd7a:115c:a1e0::"), Mask: net.CIDRMask(48, 128)},
}

// fromThisMachine（web.trust_self，WEB-130）：請求是這台機器自己送來的——X-Forwarded-For 剛好一個、沒有逗號，
// 是 tailnet 範圍的位址，而且是這台網卡上的位址。tag 身分的機器沒有 Tailscale-User-Login，這是唯一能認出「從這台自己的瀏覽器來」的線索；
// Tailscale serve 會把用戶端帶的 X-Forwarded-For 整個換成連線的真實來源（2026-11-03 實測：偽造的 1.2.3.4 被覆蓋成連線真正的 tailnet 位址），
// 別台機器、Funnel 的來源位址都不會等於這台自己的位址。本機程式直接打看板能偽造任何標頭，跟 D-24 同一個限制。
func fromThisMachine(r *http.Request) bool {
	if len(r.Header.Values("Tailscale-Funnel-Request")) > 0 {
		return false // 公網經 Funnel 進來的，一律不當成自己（縱深防禦；Funnel 本來就不帶身分，WEB-78）
	}
	xff := r.Header.Values("X-Forwarded-For")
	if len(xff) != 1 || strings.Contains(xff[0], ",") {
		return false
	}
	ip := net.ParseIP(xff[0]) // 不 trim：含空白、tab 一律不認
	if ip == nil {
		return false
	}
	inTailnet := false
	for _, n := range tailnetRanges {
		if n.Contains(ip) {
			inTailnet = true
			break
		}
	}
	if !inTailnet {
		return false
	}
	addrs, err := localAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// localOnlyWebMethods：改「誰能連進來」的設定（允許清單、主人的 tailnet 登入）與回答「信任這個資料夾」，別台看板跨站來一律不准（WEB-79，S3-9）。
// 不然放進 allow_boards 的一台就能把自己的信任範圍再往外擴（加更多看板、換主人）。
// 這台「還看哪些台」（set_sites）只影響這台自己的看板畫面，跨站可以改。
// 用跟 serveAPI 同一個 splitAPIPath 判斷，路徑寫法不同（多斜線、大小寫）在 serveAPI 那邊本來就找不到方法。
var localOnlyWebMethods = map[string]bool{"set_allow_boards": true, "set_owner_login": true, "answer_boot_prompt": true}

// board 是 app 的子套件、沒有門面業務模組，自己登記健檢（module-pattern 的例外）。
var _ app.CheckProvider = (*Module)(nil)

// Checks：有允許的看板卻沒設 owner_login，別的看板連進來一律被擋——黃字提醒（照樣能用，只是不開放跨站）。
func (m *Module) Checks() []app.Check {
	return []app.Check{{Name: "config.web_cross_site", RunAll: m.checkCrossSite}}
}

func (m *Module) checkCrossSite(context.Context) []app.CheckResult {
	if msg := crossSiteWarning(m.access()); msg != "" {
		return []app.CheckResult{{Status: app.StatusWarn, Message: msg, Fix: `在 home.toml 的 [web] 加 owner_login = "<你的 tailnet 登入>"，或拿掉 allow_boards`}}
	}
	return nil
}

func crossSiteWarning(a webAccess) string {
	if len(a.allow) > 0 && a.owner == "" {
		return "有允許別的看板連進來（web.allow_boards），但沒設主人的 tailnet 登入（web.owner_login），別的看板一律被擋"
	}
	if a.trustSelf && (len(a.allow) == 0 || a.owner == "") {
		return "web.trust_self 開著，但沒有同時設 web.allow_boards 與 web.owner_login，沒有作用（別的看板一律被擋）"
	}
	return ""
}
