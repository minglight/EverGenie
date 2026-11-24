package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 新家第一個帳號（ECL-64）：主人帶著 CLAUDE_CONFIG_DIR 登入就照樣帶同一個值（~ 展開）；沒帶就是 ~/.claude＋預設登入。
func TestAccountDefaultsFollowsCurrentLogin(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	got, err := AccountDefaults(getenv)
	if err != nil || got["config_dir"] != "~/.claude" || got["default_login"] != true || len(got) != 2 {
		t.Fatalf("沒有 CLAUDE_CONFIG_DIR：%v %v", got, err)
	}
	// 原字串照樣帶（尾端斜線也留著：鑰匙圈的名字可能照原字串算）
	env["CLAUDE_CONFIG_DIR"] = "/Users/x/.acct-work/"
	if got, err := AccountDefaults(getenv); err != nil || got["config_dir"] != "/Users/x/.acct-work/" || len(got) != 1 {
		t.Fatalf("有 CLAUDE_CONFIG_DIR：%v %v", got, err)
	}
	home, _ := os.UserHomeDir()
	env["CLAUDE_CONFIG_DIR"] = "~/.acct-b"
	if got, err := AccountDefaults(getenv); err != nil || got["config_dir"] != filepath.Join(home, ".acct-b") {
		t.Fatalf("~ 要展開：%v %v", got, err)
	}
	env["CLAUDE_CONFIG_DIR"] = "~/.acct-b/"
	if got, err := AccountDefaults(getenv); err != nil || got["config_dir"] != home+"/.acct-b/" {
		t.Fatalf("~ 展開後尾端斜線也留著：%v %v", got, err)
	}
	// 相對路徑猜不出是哪個帳號：回錯，不默默換成預設登入
	env["CLAUDE_CONFIG_DIR"] = ".acct-rel"
	if got, err := AccountDefaults(getenv); err == nil || got != nil {
		t.Fatalf("相對路徑要回錯：%v", got)
	}
}

// 預設登入的帳號（default_login＝true、資料夾是 ~/.claude）：開 session、一次性對話、暖機都不設 CLAUDE_CONFIG_DIR，
// 外面帶進來的也拿掉（不然 Claude 會換成另一組鑰匙圈的登入）；一般帳號照樣設。資料夾改成別的就不算預設登入。
// 沒寫 config_dir 的預設登入＝~/.claude。
func TestDefaultLoginLeavesConfigDirUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	def := filepath.Join(home, ".claude")
	environ := []string{"PATH=/usr/bin", "CLAUDE_CONFIG_DIR=/from/outside"}
	cfg, err := parseEngineConfig(map[string]any{"config_dir": "~/.claude", "default_login": true})
	if err != nil || !cfg.usesDefaultLogin() || cfg.ConfigDir != def {
		t.Fatalf("預設登入：%+v %v", cfg, err)
	}
	if got := strings.Join(withAccountEnv(environ, cfg), " "); strings.Contains(got, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("預設登入不能帶 CLAUDE_CONFIG_DIR：%s", got)
	}
	cfg, err = parseEngineConfig(map[string]any{"default_login": "true"})
	if err != nil || cfg.ConfigDir != def || !cfg.usesDefaultLogin() {
		t.Fatalf("沒寫資料夾的預設登入是 ~/.claude：%+v %v", cfg, err)
	}
	for _, m := range []map[string]any{
		{"config_dir": "~/.claude"},                       // 沒寫 default_login：照舊帶（作者自己的家都是這樣登入的）
		{"config_dir": "/x/.acct", "default_login": true}, // 資料夾不是預設的：default_login 不算數
	} {
		cfg, err := parseEngineConfig(m)
		if err != nil {
			t.Fatal(err)
		}
		got := withAccountEnv(environ, cfg)
		if cfg.usesDefaultLogin() || got[len(got)-1] != "CLAUDE_CONFIG_DIR="+cfg.ConfigDir || strings.Contains(strings.Join(got, " "), "/from/outside") {
			t.Fatalf("%v：%v", m, got)
		}
	}
}

// 開 session：預設登入的帳號，啟動環境沒有 CLAUDE_CONFIG_DIR；登入檢查的修法不叫人帶 CLAUDE_CONFIG_DIR。
func TestStartAndPreflightWithDefaultLogin(t *testing.T) {
	home := t.TempDir() // ~/.claude 指到暫存的家目錄，不碰真的
	t.Setenv("HOME", home)
	v := newEngEnv(t)
	ec := map[string]any{"config_dir": "~/.claude", "default_login": true}
	c := v.start("life", func(r *sdk.EngineStartRequest) { r.EngineConfig = ec })
	if _, ok := envMap(c.Env)["CLAUDE_CONFIG_DIR"]; ok {
		t.Fatalf("預設登入不設 CLAUDE_CONFIG_DIR：%v", c.Env)
	}
	login := func() sdk.PreflightCheck {
		t.Helper()
		r, err := v.e.Preflight(context.Background(), &sdk.EnginePreflightRequest{Account: "main", EngineConfig: ec})
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range r.Checks {
			if c.ID == "login:main" {
				return c
			}
		}
		t.Fatalf("preflight 沒有帳號 main 的登入檢查：%+v", r.Checks)
		return sdk.PreflightCheck{}
	}
	// 還沒登入：下一步是不用打指令的說法（打開「evergenie 修復」照畫面登入）；Login 是 claude auth login（開瀏覽器登入，不用打字），不帶 CLAUDE_CONFIG_DIR
	if c := login(); c.Ok || strings.Contains(c.Detail, "帳號設定不完整") || !strings.Contains(c.Fix, "「evergenie 修復」") || strings.Contains(c.Fix, "CLAUDE_CONFIG_DIR") || strings.Contains(c.Fix, "終端機") ||
		len(c.Login) != 3 || filepath.Base(c.Login[0]) != "claude" || strings.Join(c.Login[1:], " ") != "auth login" || strings.Contains(strings.Join(c.LoginHint, ""), "/login") {
		t.Fatalf("預設登入、還沒登入：%+v", c)
	}
	// 登入過（Claude 的預設全域設定 ~/.claude.json 有 oauthAccount）：通過，詳情是 ~/.claude
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"a@example.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// 登入過也帶 Login：登入過期時這裡看不出來（只看有沒有登入資料），主人照通知來重新登入時要用
	if c := login(); !c.Ok || !strings.Contains(c.Detail, filepath.Join(home, ".claude")) || len(c.Login) != 3 {
		t.Fatalf("預設登入、登入過：%+v", c)
	}
	// 一般帳號（不是預設登入）：Login 把 claude 開在這個帳號的設定資料夾上
	if got := v.e.loginCommand(engineConfig{ConfigDir: "/x/acct"}); len(got) != 4 || got[0] != "CLAUDE_CONFIG_DIR=/x/acct" || filepath.Base(got[1]) != "claude" || got[2] != "auth" || got[3] != "login" {
		t.Fatalf("一般帳號的登入指令：%v", got)
	}
}

// hook 行程的帳號資料夾：環境有 CLAUDE_CONFIG_DIR 就是它，沒有（預設登入）就是 <HOME>/.claude（對話紀錄才記得到）。
func TestSessionConfigDirFallsBackToClaudeDefault(t *testing.T) {
	env := map[string]string{"HOME": "/Users/t"}
	getenv := func(k string) string { return env[k] }
	if got := sessionConfigDir(getenv); got != "/Users/t/.claude" {
		t.Fatalf("沒有 CLAUDE_CONFIG_DIR：%q", got)
	}
	env["CLAUDE_CONFIG_DIR"] = "/acct"
	if got := sessionConfigDir(getenv); got != "/acct" {
		t.Fatalf("有 CLAUDE_CONFIG_DIR：%q", got)
	}
}
