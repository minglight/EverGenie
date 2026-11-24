package platform

import (
	"errors"
	"reflect"
	"testing"
)

// 看板連多台的 [web] 設定：讀得進來、網址整理成 Origin 的寫法（之後直接跟 Origin 標頭比）。
func TestWebMultiBoardSettingsLoadAndNormalize(t *testing.T) {
	h, err := LoadHome(configDir(t, homeConfig+`owner_login = " amy@example.com "
allow_boards = ["https://Genie-Amy-Mini.example.ts.net/", "https://dev.example.ts.net:8443", "https://x.example:443"]
[[web.sites]]
url = "https://genie-amy-office.example.ts.net/"
color = "#7c5cff"
[[web.sites]]
url = "https://other.example:8443"
`))
	if err != nil {
		t.Fatal(err)
	}
	if h.Web.OwnerLogin != "amy@example.com" {
		t.Errorf("owner_login：%q", h.Web.OwnerLogin)
	}
	wantBoards := []string{"https://genie-amy-mini.example.ts.net", "https://dev.example.ts.net:8443", "https://x.example"}
	if !reflect.DeepEqual(h.Web.AllowBoards, wantBoards) {
		t.Errorf("allow_boards：%q，應該是 %q", h.Web.AllowBoards, wantBoards)
	}
	wantSites := []WebSite{{URL: "https://genie-amy-office.example.ts.net", Color: "#7c5cff"}, {URL: "https://other.example:8443"}}
	if !reflect.DeepEqual(h.Web.Sites, wantSites) {
		t.Errorf("sites：%+v，應該是 %+v", h.Web.Sites, wantSites)
	}
}

// 沒寫這三個欄位的舊家照常讀得進來（沒寫 owner_login＝不開放跨站，不是錯）。
func TestWebMultiBoardSettingsAreOptional(t *testing.T) {
	h, err := LoadHome(configDir(t, homeConfig))
	if err != nil {
		t.Fatal(err)
	}
	if h.Web.OwnerLogin != "" || len(h.Web.AllowBoards) != 0 || len(h.Web.Sites) != 0 {
		t.Fatalf("沒寫應該都是空的：%+v", h.Web)
	}
}

// 壞設定照 home 驗證的方式回 Invalid 並指出哪個欄位：allow_boards 只收 https origin（不帶路徑、查詢、帳密），
// sites.url 一樣、color 只收 #rgb／#rrggbb，owner_login 要像 email。
func TestWebMultiBoardSettingsRejectBadValues(t *testing.T) {
	for extra, field := range map[string]string{
		"allow_boards = [\"http://mini.example\"]\n":                            "web.allow_boards",
		"allow_boards = [\"https://mini.example/board\"]\n":                     "web.allow_boards",
		"allow_boards = [\"https://mini.example/?a=1\"]\n":                      "web.allow_boards",
		"allow_boards = [\"https://mini.example/#x\"]\n":                        "web.allow_boards",
		"allow_boards = [\"https://u:p@mini.example\"]\n":                       "web.allow_boards",
		"allow_boards = [\"https://mini.example:0\"]\n":                         "web.allow_boards",
		"allow_boards = [\"mini.example\"]\n":                                   "web.allow_boards",
		"allow_boards = [\"https://\"]\n":                                       "web.allow_boards",
		"allow_boards = [\"https://a.example\", \"https://A.example/\"]\n":      "web.allow_boards",
		"owner_login = \"amy\"\n":                                               "web.owner_login",
		"owner_login = \"amy @example.com\"\n":                                  "web.owner_login",
		"allow_boards = [\"https://::1\"]\n":                                    "web.allow_boards",
		"allow_boards = [\"https://a.example:443:443\"]\n":                      "web.allow_boards",
		"allow_boards = [\"https://*.example\"]\n":                              "web.allow_boards",
		"allow_boards = [\"https://例子.com\"]\n":                                 "web.allow_boards",
		"allow_boards = [\"https://[fe80::1%25en0]\"]\n":                        "web.allow_boards",
		"allow_boards = [\"https://x.example#\"]\n":                             "web.allow_boards",
		"[[web.sites]]\nurl = \"http://office.example\"\n":                      "web.sites.url",
		"[[web.sites]]\nurl = \"https://office.example/x\"\n":                   "web.sites.url",
		"[[web.sites]]\nurl = \"\"\n":                                           "web.sites.url",
		"[[web.sites]]\nurl = \"https://office.example\"\ncolor = \"red\"\n":    "web.sites.color",
		"[[web.sites]]\nurl = \"https://office.example\"\ncolor = \"#12345\"\n": "web.sites.color",
	} {
		_, err := LoadHome(configDir(t, homeConfig+extra))
		var e *Err
		if !errors.As(err, &e) || e.Field != field {
			t.Errorf("%q：要指出 %s，得到 %v", extra, field, err)
		}
	}
}

// GitHub 登入的 tailnet 身分是 name@github，沒有點也要收。
func TestOwnerLoginAcceptsGitHubStyleLogin(t *testing.T) {
	for _, login := range []string{"amy@example.com", "amy@github"} {
		if !ValidOwnerLogin(login) {
			t.Errorf("%q 應該合格", login)
		}
	}
}

// IPv6 寫成標準格式、port 去掉前導 0（:0443 就是 :443），整理後的結果跟瀏覽器送的 Origin 一樣。
func TestNormalizeOriginMatchesBrowserSerialization(t *testing.T) {
	for in, want := range map[string]string{
		"https://[::1]": "https://[::1]", "https://[::1]:8443/": "https://[::1]:8443", "https://[0:0::1]": "https://[::1]",
		"https://a.example:0443": "https://a.example", "https://a.example:08443": "https://a.example:8443",
	} {
		if got, ok := NormalizeOrigin(in); !ok || got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v，應該是 %q", in, got, ok, want)
		}
	}
}
