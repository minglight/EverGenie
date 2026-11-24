package platform

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// 看板連多台（S3，2026-11-01）的 [web] 設定：哪些看板可以從瀏覽器呼叫這個家、這個看板還看哪些家。
// 伺服器之間不連線：這些網址只給瀏覽器用（法律第 7 條「每台一個家」）。

// WebSite 是 [[web.sites]] 的一台：看板要連的另一個家。台名、環境、版本一律向那台的 system.get_mode 拿，這裡不寫。
type WebSite struct {
	URL   string `toml:"url"`   // 那台看板的 https origin（例 https://genie-office.example.ts.net）
	Color string `toml:"color"` // 選填，#rgb 或 #rrggbb；沒寫照電腦代號給預設色
}

var (
	ownerLoginPattern = regexp.MustCompile(`^[^\s@]+@[^\s@]+$`)
	siteColorPattern  = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
	// hostnamePattern 是小寫 ASCII 的網域名稱或 IPv4（一段一段用點隔開，每段英數字與中間的連字號）。
	hostnamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)
)

// NormalizeOrigin 把看板網址整理成瀏覽器 Origin 標頭的寫法：https://host[:port]，host 小寫（IPv6 用標準寫法加中括號）、
// 去掉預設的 :443、port 的前導 0 與結尾斜線。不是 https、沒有 host、帶路徑、查詢、片段或帳密，或 host 不是 ASCII 的
// 網域名稱（萬用字元、中文網域這類瀏覽器不會原樣送出的寫法）／合法 IP 的回 false——多寫或寫錯的東西永遠比對不到 Origin。
func NormalizeOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return "", false
	}
	if u.Path != "" && u.Path != "/" {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, ":") {
		ip := net.ParseIP(host)
		if !strings.HasPrefix(u.Host, "[") || strings.Contains(host, "%") || ip == nil {
			return "", false
		}
		host = "[" + ip.String() + "]"
	} else if !hostnamePattern.MatchString(host) {
		return "", false
	}
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return "", false
		}
		if n != 443 {
			host += ":" + strconv.Itoa(n)
		}
	}
	return "https://" + host, true
}

// PublicBase 是 web.public_url 去掉前後空白與結尾斜線（推播連結、get_mode 都用這份）；沒寫是空字串。
func (h *Home) PublicBase() string {
	return strings.TrimRight(strings.TrimSpace(h.Web.PublicURL), "/")
}

// ValidOwnerLogin 判斷 owner_login 像不像 tailnet 登入（email，或 GitHub 登入的 name@github）。
func ValidOwnerLogin(login string) bool { return ownerLoginPattern.MatchString(login) }

// ValidSiteColor 判斷 [[web.sites]] 的 color：空（用預設色）或 #rgb、#rrggbb。
func ValidSiteColor(color string) bool { return color == "" || siteColorPattern.MatchString(color) }

// validateWeb 檢查並正規化 owner_login、allow_boards、sites；網址改成 NormalizeOrigin 的寫法，之後直接拿來跟 Origin 比。
// 規則與訊息在下面三個 Normalize 函式（settings 的 web.set_* 寫檔前也呼叫它們）。
func (h *Home) validateWeb() error {
	var err error
	if h.Web.OwnerLogin, err = NormalizeOwnerLogin(h.Web.OwnerLogin); err != nil {
		return err
	}
	if h.Web.AllowBoards, err = NormalizeAllowBoards(h.Web.AllowBoards); err != nil {
		return err
	}
	h.Web.Sites, err = NormalizeSites(h.Web.Sites)
	return err
}

// NormalizeOwnerLogin 去掉前後空白；有值卻不像 tailnet 登入回 Invalid（web.owner_login）。空＝不開放別的看板連進來。
func NormalizeOwnerLogin(login string) (string, error) {
	login = strings.TrimSpace(login)
	if login != "" && !ValidOwnerLogin(login) {
		return "", Invalid("web.owner_login", "主人的 tailnet 登入要像 email（例 amy@example.com），或不寫（不開放別的看板連進來）")
	}
	return login, nil
}

// NormalizeAllowBoards 把每個網址整理成 NormalizeOrigin 的寫法（回新的一份，不改傳進來的）；
// 整理不了或整理後重複回 Invalid（web.allow_boards）。
func NormalizeAllowBoards(raws []string) ([]string, error) {
	if raws == nil {
		return nil, nil
	}
	out := make([]string, 0, len(raws))
	seen := map[string]bool{}
	for _, raw := range raws {
		origin, ok := NormalizeOrigin(raw)
		if !ok {
			return nil, Invalid("web.allow_boards", "允許的看板要寫 https://主機[:port]，不帶路徑（例 https://genie-mini.example.ts.net）："+raw)
		}
		if seen[origin] {
			return nil, Invalid("web.allow_boards", "允許的看板重複了："+raw)
		}
		seen[origin] = true
		out = append(out, origin)
	}
	return out, nil
}

// NormalizeSites 把每台的 url 整理成 NormalizeOrigin 的寫法、color 去前後空白（回新的一份）；url 整理不了或重複回
// Invalid（web.sites.url），color 不合格回 Invalid（web.sites.color）。
func NormalizeSites(sites []WebSite) ([]WebSite, error) {
	if sites == nil {
		return nil, nil
	}
	out := make([]WebSite, 0, len(sites))
	seen := map[string]bool{}
	for _, site := range sites {
		origin, ok := NormalizeOrigin(site.URL)
		if !ok {
			return nil, Invalid("web.sites.url", "要連的看板網址要寫 https://主機[:port]，不帶路徑（例 https://genie-office.example.ts.net）："+site.URL)
		}
		if seen[origin] {
			return nil, Invalid("web.sites.url", "要連的看板重複了："+site.URL)
		}
		seen[origin] = true
		color := strings.TrimSpace(site.Color)
		if !ValidSiteColor(color) {
			return nil, Invalid("web.sites.color", "顏色請寫 #rgb 或 #rrggbb（例 #7c5cff），或不寫用預設色："+site.Color)
		}
		out = append(out, WebSite{URL: origin, Color: color})
	}
	return out, nil
}
