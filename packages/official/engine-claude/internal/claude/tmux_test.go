package claude

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

type runnerCall struct {
	Stdin string
	Args  []string
}

// fakeRunner 取代真的 tmux 執行檔：記錄每次呼叫，並可依指令回錯。
type fakeRunner struct {
	calls []runnerCall
	fail  func(args []string) (string, error)
}

func (r *fakeRunner) run(_ context.Context, stdin string, args ...string) (string, error) {
	r.calls = append(r.calls, runnerCall{stdin, append([]string(nil), args...)})
	if r.fail != nil {
		return r.fail(args)
	}
	return "", nil
}

func newExecTmux(t *testing.T) (*ExecTmux, *fakeRunner) {
	t.Helper()
	r := &fakeRunner{}
	n := 0
	return &ExecTmux{ScriptDir: t.TempDir(), run: r.run, rnd: func() string { n++; return "r" + string(rune('0'+n)) }}, r
}

// Guards: REQ-76
func TestExecTmuxPasteUsesBufferFromStdin(t *testing.T) {
	tm, r := newExecTmux(t)
	text := "雙引號 \"a\" 單引號 'b' $HOME `x` \\ 結尾\n第二行"
	if err := tm.Paste("code", text); err != nil {
		t.Fatal(err)
	}
	want := []runnerCall{
		{text, []string{"-L", "eg", "load-buffer", "-b", "hg-r1", "-"}},
		{"", []string{"-L", "eg", "paste-buffer", "-p", "-d", "-b", "hg-r1", "-t", "=code:"}},
	}
	if !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("貼上的 tmux 指令不對：\n got %q\nwant %q", r.calls, want)
	}
	for _, c := range r.calls {
		for _, a := range c.Args {
			if strings.Contains(a, "雙引號") {
				t.Fatalf("文字不能出現在參數裡（要走 stdin）：%q", c.Args)
			}
		}
	}
}

// Guards: REQ-76
func TestExecTmuxPasteFailureCleansBuffer(t *testing.T) {
	tm, r := newExecTmux(t)
	r.fail = func(args []string) (string, error) {
		if args[2] == "paste-buffer" {
			return "can't find pane", errors.New("exit 1")
		}
		return "", nil
	}
	if err := tm.Paste("code", "x"); err == nil {
		t.Fatal("貼上失敗要回錯")
	}
	last := r.calls[len(r.calls)-1]
	if last.Args[2] != "delete-buffer" || last.Args[4] != "hg-r1" {
		t.Fatalf("失敗後要清掉暫存 buffer：%q", last.Args)
	}
}

// Guards: REQ-76
func TestExecTmuxCommandsUseExactSessionTarget(t *testing.T) {
	tm, r := newExecTmux(t)
	r.fail = func(args []string) (string, error) {
		if args[2] == "has-session" {
			return "can't find session: code", errors.New("exit 1")
		}
		return "screen text", nil
	}
	if tm.Alive("a.b:c") {
		t.Fatal("has-session 失敗就是不在")
	}
	if err := tm.SendKey("code", "Enter"); err != nil {
		t.Fatal(err)
	}
	if out, err := tm.Capture("code"); err != nil || out != "screen text" {
		t.Fatalf("%q %v", out, err)
	}
	want := [][]string{
		{"-L", "eg", "has-session", "-t", "=a_b_c"},
		{"-L", "eg", "send-keys", "-t", "=code:", "Enter"},
		{"-L", "eg", "capture-pane", "-p", "-t", "=code:"},
	}
	var got [][]string
	for _, c := range r.calls {
		got = append(got, c.Args)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

// Guards: REQ-72
func TestExecTmuxKillMissingSessionIsNotAnError(t *testing.T) {
	tm, r := newExecTmux(t)
	r.fail = func([]string) (string, error) { return "can't find session: code", errors.New("exit 1") }
	if err := tm.Kill("code"); err != nil {
		t.Fatalf("session 不在不算錯：%v", err)
	}
	r.fail = func([]string) (string, error) { return "no server running on /tmp/tmux-1/hg", errors.New("exit 1") }
	if err := tm.Kill("code"); err != nil {
		t.Fatalf("server 不在也不算錯：%v", err)
	}
	r.fail = func([]string) (string, error) { return "permission denied", errors.New("exit 1") }
	if err := tm.Kill("code"); err == nil {
		t.Fatal("其他錯誤要回報")
	}
	if got := r.calls[0].Args; !reflect.DeepEqual(got, []string{"-L", "eg", "kill-session", "-t", "=code"}) {
		t.Fatalf("kill-session 指令：%q", got)
	}
}

// Guards: REQ-83
func TestExecTmuxStartUsesSocketAndRemembersIt(t *testing.T) {
	tm, r := newExecTmux(t)
	if err := tm.Start("hg-test", "code", "/tmp/hg-work", []string{"A=1"}, []string{"claude", "--name", "code"}); err != nil {
		t.Fatal(err)
	}
	args := r.calls[0].Args
	if !reflect.DeepEqual(args[:11], []string{"-L", "hg-test", "new-session", "-d", "-s", "code", "-x", "160", "-y", "50", "-c"}) || args[11] != "/tmp/hg-work" {
		t.Fatalf("new-session 指令：%q", args)
	}
	script := args[len(args)-1]
	if filepath.Dir(script) != tm.ScriptDir {
		t.Fatalf("指令稿要放在 ScriptDir：%s", script)
	}
	_ = tm.Alive("code")
	_ = tm.SendKey("code", "Down")
	_ = tm.Kill("code")
	for _, c := range r.calls[1:] {
		if c.Args[1] != "hg-test" {
			t.Fatalf("同一個 session 後續呼叫要用開它的 socket：%q", c.Args)
		}
	}
	_ = tm.Alive("code")
	if got := r.calls[len(r.calls)-1].Args[1]; got != "eg" {
		t.Fatalf("Kill 之後忘記 socket，回到預設：%s", got)
	}
	if err := tm.Start("hg", "x", "", nil, nil); err == nil {
		t.Fatal("沒有指令要回錯")
	}
	r.fail = func([]string) (string, error) { return "duplicate session: x", errors.New("exit 1") }
	before, _ := os.ReadDir(tm.ScriptDir)
	if err := tm.Start("hg", "x", "", nil, []string{"claude"}); err == nil {
		t.Fatal("new-session 失敗要回錯")
	}
	after, _ := os.ReadDir(tm.ScriptDir)
	if len(after) != len(before) {
		t.Fatalf("開不起來要把指令稿刪掉：%d → %d", len(before), len(after))
	}
}

// 真的拿指令稿用 /bin/sh 跑一次（不是 tmux）：宿主的 CLAUDE*／ANTHROPIC_* 要清掉，我們的環境照原樣進去，
// 超過 tmux 命令列上限的長參數也要完整，跑完指令稿自己刪掉。
// Guards: REQ-83
func TestLaunchScriptScrubsAndPreservesValues(t *testing.T) {
	tm, r := newExecTmux(t)
	special := "it's \"q\" $HOME `x` \\ 結尾\n第二行"
	long := strings.Repeat("長", 20000)
	env := []string{"EVERGENIE_SPECIAL=" + special, "CLAUDE_CONFIG_DIR=/cfg", "NOT VALID=x"}
	argv := []string{"/bin/sh", "-c",
		`printf '%s|%s|%s|%s|%s|' "$EVERGENIE_SPECIAL" "${CLAUDE_FOO-unset}" "${ANTHROPIC_X-unset}" "$CLAUDE_CONFIG_DIR" "${STAY-gone}"; printf '%s' "$1" | wc -c`,
		"_", long}
	if err := tm.Start("hg", "code", "", env, argv); err != nil {
		t.Fatal(err)
	}
	script := r.calls[0].Args[len(r.calls[0].Args)-1]
	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "NOT VALID") {
		t.Fatal("不合法的變數名稱要略過")
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "CLAUDE_FOO=1", "ANTHROPIC_X=1", "STAY=yes")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("指令稿執行失敗：%v", err)
	}
	wantPrefix := special + "|unset|unset|/cfg|yes|"
	if !strings.HasPrefix(string(out), wantPrefix) {
		t.Fatalf("環境不對：\n got %q\nwant 開頭 %q", out, wantPrefix)
	}
	if n := strings.TrimSpace(strings.TrimPrefix(string(out), wantPrefix)); n != "60000" {
		t.Fatalf("長參數要完整（20000 個字＝60000 bytes）：%q", n)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatalf("指令稿跑完要自己刪掉：%v", err)
	}
}

// Guards: REQ-72
func TestServeMountsEngineOverSocket(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "hg-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	cfg, err := ServeConfigFromEnv(func(k string) string {
		return map[string]string{"EVERGENIE_HOME": home}[k]
	}, "/opt/hg/evergenie-engine-claude")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EngineSocket != filepath.Join(home, "runtime", "sockets", "engine-claude.sock") || cfg.APISocket != filepath.Join(home, "runtime", "sockets", "api.sock") {
		t.Fatalf("socket 預設路徑：%+v", cfg)
	}
	clock := newFakeClock()
	tm := newFakeTmux(clock)
	cfg.Tmux = tm
	cfg.AliveEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg) }()
	client := sdk.NewEngineClient(sdk.Dial(cfg.EngineSocket))
	var caps *sdk.EngineCapabilitiesResponse
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			cancel()
			if err != nil && strings.Contains(err.Error(), "operation not permitted") {
				t.Skipf("這個環境不能 bind unix socket：%v", err)
			}
			t.Fatalf("serve 提早結束：%v", err)
		default:
		}
		if caps, err = client.Capabilities(ctx, &sdk.EngineCapabilitiesRequest{}); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || caps == nil || caps.Engine != "claude" {
		cancel()
		t.Fatalf("Capabilities：%+v %v", caps, err)
	}
	// 透過 socket 呼叫 Start 與 Usage，確認方法簽名與錯誤碼經 sdk 的傳輸都對。
	cfgDir := t.TempDir()
	if _, err := client.Start(ctx, &sdk.EngineStartRequest{Name: "code", Account: "a", Cwd: "/tmp", EngineConfig: map[string]any{"config_dir": cfgDir}}); err != nil {
		cancel()
		t.Fatalf("Start：%v", err)
	}
	// 用量走同一條 socket：沒有快照也要正常回（空的視窗），不是錯誤。
	if r, err := client.Usage(ctx, &sdk.EngineUsageRequest{Account: "a", EngineConfig: map[string]any{"config_dir": cfgDir, "monitor_dir": t.TempDir()}}); err != nil || len(r.Windows) != 0 {
		t.Fatalf("Usage：%+v %v", r, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("結束時：%v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ctx 結束後 serve 要結束")
	}
	if _, err := os.Stat(cfg.EngineSocket); !os.IsNotExist(err) {
		t.Fatalf("結束後 socket 要刪掉：%v", err)
	}
	if _, err := ServeConfigFromEnv(func(string) string { return "" }, "x"); err == nil {
		t.Fatal("沒有 EVERGENIE_HOME 要回錯")
	}
}

// TestLaunchScriptLoadsEnvFile：帳號設了 env_file（例 ~/.secret.sh）時，啟動腳本先載入它，秘書拿到跟終端機一樣的 token；
// 那個檔蓋不掉帳號與 session 的變數，CLAUDE*／ANTHROPIC_* 照樣清掉；檔案壞了或不見了，session 照樣開（DEV-5）。
// Guards: REQ-255
func TestLaunchScriptLoadsEnvFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "secret's.sh")
	if err := os.WriteFile(good, []byte("export EVERGENIE_TEST_JIRA_URL=https://jira.example\nEVERGENIE_TEST_PLAIN='沒寫 export 也帶'\nexport ANTHROPIC_API_KEY=bad\nexport CLAUDE_CONFIG_DIR=/evil\nexport EVERGENIE_AGENT=evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(dir, "broken.sh")
	if err := os.WriteFile(broken, []byte("export EVERGENIE_TEST_JIRA_URL=x\nif then fi (\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := func(file string) string {
		t.Helper()
		tm, r := newExecTmux(t)
		env := []string{envFileVar + "=" + file, "CLAUDE_CONFIG_DIR=/acct", "EVERGENIE_AGENT=work"}
		argv := []string{"/bin/sh", "-c", `printf '%s|%s|%s|%s|%s' "${EVERGENIE_TEST_JIRA_URL-unset}" "${EVERGENIE_TEST_PLAIN-unset}" "${ANTHROPIC_API_KEY-unset}" "$CLAUDE_CONFIG_DIR" "$EVERGENIE_AGENT"`}
		if err := tm.Start("hg", "work", "", env, argv); err != nil {
			t.Fatal(err)
		}
		script := r.calls[0].Args[len(r.calls[0].Args)-1]
		out, err := exec.Command("/bin/sh", script).Output()
		if err != nil {
			t.Fatalf("指令稿執行失敗：%v", err)
		}
		return string(out)
	}
	if got := run(good); got != "https://jira.example|沒寫 export 也帶|unset|/acct|work" {
		t.Fatalf("載入 env_file：%q", got)
	}
	for _, f := range []string{broken, filepath.Join(dir, "missing.sh")} {
		if got := run(f); got != "unset|unset|unset|/acct|work" {
			t.Fatalf("%s 壞了或不見要略過、照樣開：%q", filepath.Base(f), got)
		}
	}
	// 帳號設定：~/ 展開；相對路徑不收。
	t.Setenv("HOME", dir)
	if c, err := parseEngineConfig(map[string]any{"config_dir": "/acct", "env_file": "~/.secret.sh"}); err != nil || c.EnvFile != filepath.Join(dir, ".secret.sh") {
		t.Fatalf("env_file 展開：%q %v", c.EnvFile, err)
	}
	if _, err := parseEngineConfig(map[string]any{"config_dir": "/acct", "env_file": "secret.sh"}); err == nil {
		t.Fatal("相對路徑的 env_file 要報錯")
	}
}
