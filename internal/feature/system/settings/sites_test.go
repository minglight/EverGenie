package settings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func getSites(t *testing.T, h http.Handler) sdk.WebGetSitesResponse {
	t.Helper()
	code, raw := post(t, h, "/api/web/get_sites", &sdk.WebGetSitesRequest{})
	var res sdk.WebGetSitesResponse
	if code != 200 || json.Unmarshal(raw, &res) != nil {
		t.Fatalf("get_sites → %d %s", code, raw)
	}
	return res
}

func readHome(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "home.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// 看板連多台的三個設定只有主人能寫：秘書 403、沒帶身分 400，檔案都不動。
func TestWebSitesSettingsSecretaryCannotWrite(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	before := readHome(t, dir)
	writes := func(by *sdk.Actor) map[string]any {
		return map[string]any{
			"/api/web/set_sites":        &sdk.WebSetSitesRequest{Sites: []sdk.WebSite{{URL: "https://office.example"}}, By: by},
			"/api/web/set_allow_boards": &sdk.WebSetAllowBoardsRequest{AllowBoards: []string{"https://mini.example"}, By: by},
			"/api/web/set_owner_login":  &sdk.WebSetOwnerLoginRequest{OwnerLogin: "amy@example.com", By: by},
		}
	}
	for path, req := range writes(&sdk.Actor{Name: "code"}) {
		if code, raw := post(t, h, path, req); code != http.StatusForbidden {
			t.Errorf("秘書寫 %s 要 403：%d %s", path, code, raw)
		}
	}
	for path, req := range writes(nil) {
		if code, raw := post(t, h, path, req); code != http.StatusBadRequest {
			t.Errorf("沒身分寫 %s 要 400：%d %s", path, code, raw)
		}
	}
	if after := readHome(t, dir); !bytes.Equal(before, after) {
		t.Fatalf("被擋的請求不能改檔：\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime", "backups", "home")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("被擋的請求不該留備份：%v", err)
	}
}

// 主人寫入：先備份、網址整理成 Origin 的寫法寫進 home.toml、get_sites 立刻讀得到、其他設定留著；整份清空也可以。
func TestWebSitesSettingsOwnerWriteNormalizesAndBacksUp(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	code, raw := post(t, h, "/api/web/set_sites", &sdk.WebSetSitesRequest{By: owner, Sites: []sdk.WebSite{
		{URL: "https://Genie-Amy-Office.example.ts.net:443/", Color: " #7c5cff "},
		{URL: "https://other.example:8443"},
	}})
	var sites sdk.WebSetSitesResponse
	if code != 200 || json.Unmarshal(raw, &sites) != nil {
		t.Fatalf("set_sites：%d %s", code, raw)
	}
	want := []sdk.WebSite{{URL: "https://genie-amy-office.example.ts.net", Color: "#7c5cff"}, {URL: "https://other.example:8443"}}
	if len(sites.Sites) != 2 || sites.Sites[0] != want[0] || sites.Sites[1] != want[1] {
		t.Fatalf("set_sites 要回整理過的值：%+v", sites.Sites)
	}
	if _, err := os.Stat(filepath.Join(dir, sites.Backup)); sites.Backup == "" || err != nil {
		t.Fatalf("要先備份：%q %v", sites.Backup, err)
	}
	code, raw = post(t, h, "/api/web/set_allow_boards", &sdk.WebSetAllowBoardsRequest{By: owner, AllowBoards: []string{"https://MINI.example/"}})
	var allow sdk.WebSetAllowBoardsResponse
	if code != 200 || json.Unmarshal(raw, &allow) != nil || len(allow.AllowBoards) != 1 || allow.AllowBoards[0] != "https://mini.example" || allow.Backup == "" {
		t.Fatalf("set_allow_boards：%d %s", code, raw)
	}
	code, raw = post(t, h, "/api/web/set_owner_login", &sdk.WebSetOwnerLoginRequest{By: owner, OwnerLogin: " amy@example.com "})
	var login sdk.WebSetOwnerLoginResponse
	if code != 200 || json.Unmarshal(raw, &login) != nil || login.OwnerLogin != "amy@example.com" || login.Backup == "" {
		t.Fatalf("set_owner_login：%d %s", code, raw)
	}

	got := getSites(t, h)
	if got.OwnerLogin != "amy@example.com" || len(got.AllowBoards) != 1 || len(got.Sites) != 2 || got.Sites[0] != want[0] {
		t.Fatalf("改完要立刻讀得到：%+v", got)
	}
	data := string(readHome(t, dir))
	for _, s := range []string{`url = "https://genie-amy-office.example.ts.net"`, `"https://mini.example"`, `owner_login = "amy@example.com"`, "測試", "port = 18850"} {
		if !strings.Contains(data, s) {
			t.Errorf("home.toml 要有 %s：\n%s", s, data)
		}
	}

	if code, raw := post(t, h, "/api/web/set_sites", &sdk.WebSetSitesRequest{By: owner}); code != 200 {
		t.Fatalf("清空 sites：%d %s", code, raw)
	}
	if code, raw := post(t, h, "/api/web/set_owner_login", &sdk.WebSetOwnerLoginRequest{By: owner}); code != 200 {
		t.Fatalf("拿掉 owner_login：%d %s", code, raw)
	}
	if got := getSites(t, h); len(got.Sites) != 0 || got.OwnerLogin != "" || len(got.AllowBoards) != 1 {
		t.Fatalf("清掉之後：%+v", got)
	}
	if _, err := platform.LoadHome(dir); err != nil {
		t.Fatalf("寫完的 home.toml 要讀得回來：%v", err)
	}
}

// 壞值（不是 https、帶路徑、重複、顏色不對、登入不像 email）整份擋下：400、home.toml 一個字都不變、不留備份。
func TestWebSitesSettingsRejectBadValuesWithoutTouchingHome(t *testing.T) {
	dir, h := settingsHome(t, "", Deps{})
	before := readHome(t, dir)
	for name, c := range map[string]struct {
		path string
		req  any
	}{
		"sites 不是 https":    {"/api/web/set_sites", &sdk.WebSetSitesRequest{By: owner, Sites: []sdk.WebSite{{URL: "http://office.example"}}}},
		"sites 帶路徑":         {"/api/web/set_sites", &sdk.WebSetSitesRequest{By: owner, Sites: []sdk.WebSite{{URL: "https://office.example/board"}}}},
		"sites 整理後重複":       {"/api/web/set_sites", &sdk.WebSetSitesRequest{By: owner, Sites: []sdk.WebSite{{URL: "https://office.example"}, {URL: "https://OFFICE.example:443/"}}}},
		"sites 顏色不對":        {"/api/web/set_sites", &sdk.WebSetSitesRequest{By: owner, Sites: []sdk.WebSite{{URL: "https://office.example", Color: "purple"}}}},
		"allow_boards 萬用字元": {"/api/web/set_allow_boards", &sdk.WebSetAllowBoardsRequest{By: owner, AllowBoards: []string{"https://*.example"}}},
		"allow_boards 好壞混著": {"/api/web/set_allow_boards", &sdk.WebSetAllowBoardsRequest{By: owner, AllowBoards: []string{"https://mini.example", "mini.example"}}},
		"owner_login 不像登入":  {"/api/web/set_owner_login", &sdk.WebSetOwnerLoginRequest{By: owner, OwnerLogin: "amy"}},
		"owner_login 中間有空白": {"/api/web/set_owner_login", &sdk.WebSetOwnerLoginRequest{By: owner, OwnerLogin: "ha nk@example.com"}},
	} {
		code, raw := post(t, h, c.path, c.req)
		var e sdk.Error
		if code != http.StatusBadRequest || json.Unmarshal(raw, &e) != nil || e.Code != sdk.ErrorCodeInvalid || e.Message == "" || !strings.HasPrefix(e.Field, "web.") {
			t.Errorf("%s：要 400 INVALID、白話原因、欄位名同 home.toml（web.…），拿到 %d %s", name, code, raw)
		}
	}
	if after := readHome(t, dir); !bytes.Equal(before, after) {
		t.Fatalf("壞值不能改檔：\n%s", after)
	}
	if _, err := os.Stat(filepath.Join(dir, "runtime", "backups", "home")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("壞值不該留備份：%v", err)
	}
}

// serveHandler 把業務模組的路由掛到 unix socket，讓指令走真的服務（CLI 測試用）。
func serveHandler(t *testing.T, h http.Handler) *sdk.Client {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "api.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()); os.RemoveAll(dir) })
	return sdk.Dial(sock)
}

// web sites|allow|owner 指令走同一套服務：add 已經有的（寫法不同也算）不重複加、不寫檔；rm 沒有的回清楚錯誤；壞網址回伺服器的白話原因。
func TestWebSitesCLI(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	dir, h := settingsHome(t, "", Deps{})
	c := serveHandler(t, h)

	out := mustRun(t, c, "web", "sites", "add", "https://office.example", "--color", "#abc")
	if !strings.Contains(out, "已加入 https://office.example") || !strings.Contains(out, "runtime/backups/home/") {
		t.Fatalf("sites add：%q", out)
	}
	before := readHome(t, dir)
	if out := mustRun(t, c, "web", "sites", "add", "https://OFFICE.example:443/"); !strings.Contains(out, "已經在清單裡") {
		t.Fatalf("重複的 add 要說已經有了：%q", out)
	}
	if after := readHome(t, dir); !bytes.Equal(before, after) {
		t.Fatal("重複的 add 不能寫檔")
	}
	if got := getSites(t, h); len(got.Sites) != 1 || got.Sites[0].Color != "#abc" {
		t.Fatalf("重複的 add 不能多一台：%+v", got.Sites)
	}
	if out := mustRun(t, c, "web", "sites", "list"); !strings.Contains(out, "https://office.example  #abc") {
		t.Fatalf("sites list：%q", out)
	}
	code, _, errOut := runWith(t, c, nil, "web", "sites", "rm", "https://nope.example")
	if code != 1 || !strings.Contains(errOut, "清單裡沒有 https://nope.example") || !strings.Contains(errOut, "web sites list") {
		t.Fatalf("rm 沒有的要回清楚錯誤：%d %q", code, errOut)
	}
	code, _, errOut = runWith(t, c, nil, "web", "sites", "add", "http://office2.example")
	if code != 1 || !strings.Contains(errOut, "https://主機") || strings.Contains(errOut, "命令列是") {
		t.Fatalf("壞網址要回伺服器的白話原因（不要提不存在的旗標）：%d %q", code, errOut)
	}
	if out := mustRun(t, c, "web", "sites", "rm", "https://office.example/"); !strings.Contains(out, "已拿掉") {
		t.Fatalf("sites rm：%q", out)
	}
	if got := getSites(t, h); len(got.Sites) != 0 {
		t.Fatalf("rm 之後：%+v", got.Sites)
	}

	if out := mustRun(t, c, "web", "allow", "add", "https://mini.example"); !strings.Contains(out, "已允許 https://mini.example") || !strings.Contains(out, "還沒設主人的 tailnet 登入") {
		t.Fatalf("allow add：%q", out)
	}
	if out := mustRun(t, c, "web", "allow", "add", "https://Mini.example"); !strings.Contains(out, "已經在清單裡") {
		t.Fatalf("重複的 allow add：%q", out)
	}
	if code, _, errOut := runWith(t, c, nil, "web", "allow", "rm", "https://nope.example"); code != 1 || !strings.Contains(errOut, "清單裡沒有") {
		t.Fatalf("allow rm 沒有的：%d %q", code, errOut)
	}
	if out := mustRun(t, c, "web", "owner", "set", "amy@example.com"); !strings.Contains(out, "amy@example.com") {
		t.Fatalf("owner set：%q", out)
	}
	if out := mustRun(t, c, "web", "allow", "list"); !strings.Contains(out, "https://mini.example") || !strings.Contains(out, "主人的 tailnet 登入：amy@example.com") {
		t.Fatalf("allow list：%q", out)
	}
	if out := mustRun(t, c, "web", "allow", "rm", "https://mini.example"); !strings.Contains(out, "已拿掉") {
		t.Fatalf("allow rm：%q", out)
	}
	if got := getSites(t, h); len(got.AllowBoards) != 0 || got.OwnerLogin != "amy@example.com" {
		t.Fatalf("allow rm 之後：%+v", got)
	}
	if out := mustRun(t, c, "web", "owner", "clear"); !strings.Contains(out, "已拿掉主人的 tailnet 登入") {
		t.Fatalf("owner clear：%q", out)
	}
	if got := getSites(t, h); got.OwnerLogin != "" {
		t.Fatalf("owner clear 之後：%+v", got)
	}
}

// --help 列出子指令與旗標；沒給或不認得的子指令是用法錯誤。
func TestWebSitesCLIHelp(t *testing.T) {
	t.Setenv("EVERGENIE_AGENT", "")
	_, h := settingsHome(t, "", Deps{})
	c := serveHandler(t, h)
	out := mustRun(t, c, "web", "sites", "--help")
	for _, s := range []string{"web sites list", "web sites add <網址> [--color #rrggbb]", "web sites rm <網址>"} {
		if !strings.Contains(out, s) {
			t.Errorf("web sites --help 要列出 %q：\n%s", s, out)
		}
	}
	if out := mustRun(t, c, "web", "sites", "add", "--help"); !strings.Contains(out, "--color") {
		t.Errorf("web sites add --help 要列出旗標：%q", out)
	}
	if out := mustRun(t, c, "web", "allow", "--help"); !strings.Contains(out, "web allow add <網址>") {
		t.Errorf("web allow --help：%q", out)
	}
	if out := mustRun(t, c, "web", "owner", "--help"); !strings.Contains(out, "web owner set <login>") || !strings.Contains(out, "web owner clear") {
		t.Errorf("web owner --help：%q", out)
	}
	if out := mustRun(t, c, "help"); !strings.Contains(out, "web sites|allow|owner") {
		t.Errorf("總覽的 help 要有 web：%q", out)
	}
	for _, args := range [][]string{{"web", "sites"}, {"web", "sites", "wipe"}, {"web", "owner", "set"}, {"web", "allow", "add", "a", "b"}} {
		if code, _, _ := runWith(t, c, nil, args...); code != 2 {
			t.Errorf("%v 要是用法錯誤（2），拿到 %d", args, code)
		}
	}
}
