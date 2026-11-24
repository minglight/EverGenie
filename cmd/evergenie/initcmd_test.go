package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// init 建出來的家：home.toml 讀得起來、看板 port 沒被占用、[update] channel 是 release；serve 真的開得起來，
// 看板首頁 200、doctor 沒有紅字（引擎帳號沒登入這種黃字可以）。
// Guards: REQ-279
func TestInitMakesBootableHome(t *testing.T) {
	t.Setenv("EVERGENIE_SITE", "hub")
	t.Setenv("EVERGENIE_BACKUP_DIR", t.TempDir())
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", filepath.Join("..", "..", "packages", "official"))
	// unix socket 的路徑上限很短（macOS 約 100 字元）：t.TempDir() 帶測試名稱太長，改用短名字的暫存資料夾。
	short, err := os.MkdirTemp("", "hgi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(short) })
	home := filepath.Join(short, "h")
	// 安裝程式會在 EverGenie 旁邊編出 evergenie-engine-claude；這裡放一支會直接結束的假檔，讓「找得到引擎」那兩項過關（引擎能不能登入是黃字，不在這裡驗）。
	bin := filepath.Join(short, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// account-defaults 回帳號設定（ENG-19）：init 把它寫進第一個帳號的 engine_config。
	fakeEngine := "#!/bin/sh\nif [ \"$1\" = account-defaults ]; then echo '{\"config_dir\":\"" + filepath.Join(short, "acct") + "\",\"default_login\":false}'; fi\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "evergenie-engine-claude"), []byte(fakeEngine), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EVERGENIE_ENGINE_BIN_DIR", bin)
	var out, errOut bytes.Buffer
	code := initCommand(context.Background(), cmdline.HostRequest{Args: []string{"--home", home, "--name", "測試的人", "--timezone", "Asia/Taipei", "--state", filepath.Join(short, "s")}, Out: &out, Err: &errOut})
	if code != 0 {
		t.Fatalf("init 結束碼 %d：%s", code, errOut.String())
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		t.Fatal(err)
	}
	if h.User.Name != "測試的人" || h.User.Timezone != "Asia/Taipei" || h.UpdateChannel() != platform.UpdateChannelRelease || h.Update.Channel != "release" || h.Web.Port < defaultBoardPort {
		t.Fatalf("home.toml：%+v", h)
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:") || strings.Contains(out.String(), "沒有自動填上") {
		t.Fatalf("要印看板網址、帳號設定填上了就不提醒：%s", out.String())
	}
	main, _ := h.Accounts["main"].(map[string]any)
	ec, _ := main["engine_config"].(map[string]any)
	if main["engine"] != "claude" || ec["config_dir"] != filepath.Join(short, "acct") || ec["default_login"] != false || len(ec) != 2 {
		t.Fatalf("第一個帳號要帶引擎給的帳號設定：%v", h.Accounts)
	}

	mods, err := Build(home)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveApp(ctx, mods, io.Discard, 5*time.Second) }()
	t.Cleanup(func() { cancel(); <-done })

	url := "http://127.0.0.1:" + itoa(h.Web.Port) + "/"
	var status int
	for i := 0; i < 200 && status != 200; i++ {
		if resp, err := http.Get(url); err == nil {
			status = resp.StatusCode
			resp.Body.Close()
		}
		if status != 200 {
			time.Sleep(100 * time.Millisecond)
		}
	}
	if status != 200 {
		t.Fatalf("看板首頁 %s 沒有回 200（%d）", url, status)
	}
	var doc bytes.Buffer
	client := sdk.Dial(platform.RuntimeDir(home, "sockets", "api.sock"))
	features, err := featureCommands()
	if err != nil {
		t.Fatal(err)
	}
	runCLI(ctx, []string{"doctor", "--json", "--home", home}, client, cmdline.IO{Out: &doc, Err: io.Discard}, features)
	var res sdk.SystemDoctorResponse
	if err := json.Unmarshal(bytes.TrimSpace(doc.Bytes()), &res); err != nil {
		t.Fatalf("doctor 輸出：%v\n%s", err, doc.String())
	}
	for _, c := range res.Checks {
		if c.Severity == sdk.DoctorSeverityError {
			t.Errorf("doctor 有紅字：%s（%s）", c.Detail, c.ID)
		}
	}
	if len(res.Checks) == 0 {
		t.Fatal("doctor 沒有任何結果")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// init 只在不存在或空的資料夾建家；有東西就結束 1、什麼都不動；沒給 --home 是用法錯誤（不吃 $EVERGENIE_HOME，免得蓋到現有的家）。
// Guards: REQ-279
func TestInitRefusesNonEmpty(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "我的東西.txt")
	if err := os.WriteFile(marker, []byte("別動"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := initCommand(context.Background(), cmdline.HostRequest{Args: []string{"--home", dir}, Out: &out, Err: &errOut}); code != 1 || !strings.Contains(errOut.String(), "這裡已經有東西了") {
		t.Fatalf("結束碼 %d：%s", code, errOut.String())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("不能動現有的資料夾：%v", entries)
	}
	// 空資料夾可以；建好之後不是空的，再 init 一次被擋。
	empty := filepath.Join(t.TempDir(), "空的")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	if code := initCommand(context.Background(), cmdline.HostRequest{Args: []string{"--home", empty, "--timezone", "UTC", "--state", filepath.Join(t.TempDir(), "state")}, Out: &out, Err: &errOut}); code != 0 {
		t.Fatalf("空資料夾該成功：%d %s", code, errOut.String())
	}
	if code := initCommand(context.Background(), cmdline.HostRequest{Args: []string{"--home", empty}, Out: &out, Err: &errOut}); code != 1 {
		t.Fatalf("第二次該被擋：%d", code)
	}
	t.Setenv("EVERGENIE_HOME", dir)
	if code := initCommand(context.Background(), cmdline.HostRequest{Home: dir, Out: &out, Err: &errOut}); code != 2 {
		t.Fatalf("沒給 --home 是用法錯誤：%d", code)
	}
	// 壞的時區：白話錯誤，不留半個家。
	bad := filepath.Join(t.TempDir(), "x")
	errOut.Reset()
	if code := initCommand(context.Background(), cmdline.HostRequest{Args: []string{"--home", bad, "--timezone", "火星/奧林帕斯"}, Out: &out, Err: &errOut}); code != 1 || !strings.Contains(errOut.String(), "時區") {
		t.Fatalf("%d %s", code, errOut.String())
	}
	if _, err := os.Stat(bad); err == nil {
		t.Fatal("失敗不能留下資料夾")
	}
}

// 引擎回不了帳號設定（找不到執行檔、子指令不認得、印的不是 JSON 物件、值不是字串或布林）：家照樣建好，
// 帳號不寫 engine_config，印一句白話提醒到看板填；不中斷安裝。
func TestInitWithoutEngineAccountDefaults(t *testing.T) {
	cases := []struct{ name, script, why string }{
		{"找不到引擎的執行檔", "", "程式裡少了連 AI 用的那一支"},
		{"引擎不認得這個子指令", "#!/bin/sh\necho 不認得 >&2\nexit 2\n", "沒問到這台電腦登入的是哪個 AI 帳號"},
		{"印的不是物件", "#!/bin/sh\necho '[1,2]'\n", "問到的帳號設定看不懂"},
		{"值不是字串或布林", "#!/bin/sh\necho '{\"config_dir\":{\"x\":1}}'\n", "問到的帳號設定看不懂"},
		{"鍵不是小寫英數底線", "#!/bin/sh\necho '{\"Config-Dir\":\"/x\"}'\n", "問到的帳號設定看不懂"},
	}
	for _, tc := range cases {
		name, script := tc.name, tc.script
		t.Run(name, func(t *testing.T) {
			bin := t.TempDir()
			if script != "" {
				if err := os.WriteFile(filepath.Join(bin, "evergenie-engine-claude"), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("EVERGENIE_ENGINE_BIN_DIR", bin)
			home := filepath.Join(t.TempDir(), "h")
			var out, errOut bytes.Buffer
			code := initCommand(context.Background(), cmdline.HostRequest{Args: []string{"--home", home, "--timezone", "UTC", "--state", filepath.Join(t.TempDir(), "s")}, Out: &out, Err: &errOut})
			if code != 0 || !strings.Contains(out.String(), "沒有自動填上（"+tc.why+"）") || !strings.Contains(out.String(), "「AI 帳號」") || strings.Contains(noteLine(out.String()), "引擎") {
				t.Fatalf("結束碼 %d：%s %s", code, out.String(), errOut.String())
			}
			h, err := platform.LoadHome(home)
			if err != nil {
				t.Fatal(err)
			}
			main, _ := h.Accounts["main"].(map[string]any)
			ec, _ := main["engine_config"].(map[string]any)
			text, _ := os.ReadFile(filepath.Join(home, "home.toml"))
			if main["engine"] != "claude" || len(ec) != 0 || strings.Contains(string(text), "engine_config") {
				t.Fatalf("沒有帳號設定就不寫：%v\n%s", h.Accounts, text)
			}
		})
	}
}

// noteLine 是 init 輸出裡「注意：」開頭的那一行（給主人看的，不能有「引擎」這種內部用語）。
func noteLine(out string) string {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "注意：") {
			return l
		}
	}
	return ""
}
