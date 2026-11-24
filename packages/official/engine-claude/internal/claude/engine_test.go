package claude

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-83
func TestStartBuildsCommandPerAccount(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("code", func(r *sdk.EngineStartRequest) {
		r.Model = "haiku"
		r.Effort = "high"
		r.RemoteAccess = true
		r.RemoteAccessName = "秘書 code"
		r.EngineConfig["tmux_socket"] = "hg-test"
	})
	if c.Socket != "hg-test" || c.Name != "code" || c.Cwd != "/tmp/hg-work" {
		t.Fatalf("tmux 參數不對：%+v", c)
	}
	settings := filepath.Join(v.home, "runtime", "engine-claude", "acct-a", "settings.json")
	want := []string{
		"claude", "--session-id", "00000000-0000-4000-8000-000000000001", "--name", "code",
		"--settings", settings, "--permission-mode", "auto", "--model", "haiku", "--effort", "high",
		"--remote-control", "秘書 code", "--append-system-prompt", "你是 code。\n第二行 \"引號\" 與 $HOME。",
		"--add-dir", filepath.Join(v.home, "runtime", "share"),
	}
	if !reflect.DeepEqual(c.Argv, want) {
		t.Fatalf("啟動指令不對：\n got %q\nwant %q", c.Argv, want)
	}
	if data, err := os.ReadFile(settings); err != nil || !strings.Contains(string(data), `"_generated"`) {
		t.Fatalf("Start 前沒有產生設定檔：%v", err)
	}
	env := envMap(c.Env)
	if env["CLAUDE_CONFIG_DIR"] != v.cfgDir || env["EVERGENIE_SESSION"] != "code" || env["EVERGENIE_AGENT"] != "code" || env["EVERGENIE_HOME"] != v.home {
		t.Fatalf("環境不對：%v", c.Env)
	}
	if env["EVERGENIE_API_SOCKET"] != filepath.Join(v.home, "runtime", "sockets", "api.sock") {
		t.Fatalf("EVERGENIE_API_SOCKET 不對：%v", env["EVERGENIE_API_SOCKET"])
	}
}

// Guards: REQ-83
func TestStartResumeAndHostEndpoint(t *testing.T) {
	v := newEngEnv(t)
	var resp *sdk.EngineStartResponse
	r := v.req("code")
	r.ResumeSessionID = "11111111-2222-4333-8444-555555555555"
	r.HostEndpoint = "/tmp/hg-x/core.sock"
	writeTranscriptFile(t, v.cfgDir, r.Cwd, r.ResumeSessionID, `{"type":"custom-title"}`+"\n"+`{"type":"user"}`+"\n")
	resp, err := v.e.Start(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if resp.EngineSessionID != r.ResumeSessionID || resp.RemoteAccessURL != "" {
		t.Fatalf("回應不對：%+v", resp)
	}
	c := v.tmux.callsOf("start")[0]
	if id, _ := argvValue(c.Argv, "--resume"); id != r.ResumeSessionID || hasFlag(c.Argv, "--session-id") {
		t.Fatalf("接回舊的要用 --resume，不帶 --session-id：%q", c.Argv)
	}
	if envMap(c.Env)["EVERGENIE_API_SOCKET"] != "/tmp/hg-x/core.sock" {
		t.Fatalf("HostEndpoint 沒有進環境：%v", c.Env)
	}
	bad := v.req("code2")
	bad.ResumeSessionID = "../evil"
	if _, err := v.e.Start(context.Background(), bad); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("不合法的 session id 要被拒絕：%v", err)
	}
}

func writeTranscriptFile(t *testing.T, configDir, cwd, id, body string) {
	t.Helper()
	dir := filepath.Join(configDir, "projects", projectDirName(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 對話檔不在、或只有標題沒有對話時，claude --resume 一開就退出，核心又會再開（DEV-133 每分鐘循環）：要改開新對話，並回報新的 id。
func TestStartResumeWithoutConversationStartsNew(t *testing.T) {
	for name, body := range map[string]string{"沒有檔案": "", "只有標題": `{"type":"custom-title"}` + "\n" + `{"type":"agent-name"}` + "\n"} {
		t.Run(name, func(t *testing.T) {
			v := newEngEnv(t)
			r := v.req("code")
			r.ResumeSessionID = "11111111-2222-4333-8444-555555555555"
			if body != "" {
				writeTranscriptFile(t, v.cfgDir, r.Cwd, r.ResumeSessionID, body)
			}
			resp, err := v.e.Start(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			const fresh = "00000000-0000-4000-8000-000000000001"
			if resp.EngineSessionID != fresh {
				t.Fatalf("要回報新的 id：%+v", resp)
			}
			c := v.tmux.callsOf("start")[0]
			if id, _ := argvValue(c.Argv, "--session-id"); id != fresh || hasFlag(c.Argv, "--resume") {
				t.Fatalf("接不回來就不能帶 --resume：%q", c.Argv)
			}
			if r.ResumeSessionID == "" {
				t.Fatal("不該改動呼叫者的請求")
			}
		})
	}
}

// Guards: REQ-74
func TestStartReturnsAttachCommand(t *testing.T) {
	v := newEngEnv(t)
	r := v.req("code-sec-318")
	r.EngineConfig["tmux_socket"] = "hg-test"
	resp, err := v.e.Start(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if want := "tmux -L hg-test attach -t '=code-sec-318'"; resp.Attach != want {
		t.Fatalf("接上畫面的指令不對：got %q want %q", resp.Attach, want)
	}
}

// Guards: REQ-83
func TestMultipleAccounts(t *testing.T) {
	v := newEngEnv(t)
	cfgB := t.TempDir()
	a := v.start("code", func(r *sdk.EngineStartRequest) { r.Model = "haiku" })
	b := v.start("life", func(r *sdk.EngineStartRequest) {
		r.Account = "acct-b"
		r.Model = "sonnet"
		r.EngineConfig = map[string]any{"config_dir": cfgB, "tmux_socket": "other"}
	})
	if envMap(a.Env)["CLAUDE_CONFIG_DIR"] != v.cfgDir || envMap(b.Env)["CLAUDE_CONFIG_DIR"] != cfgB {
		t.Fatalf("兩個帳號的設定資料夾沒有分開：%v / %v", a.Env, b.Env)
	}
	if m, _ := argvValue(a.Argv, "--model"); m != "haiku" {
		t.Fatalf("a 的模型：%q", m)
	}
	if m, _ := argvValue(b.Argv, "--model"); m != "sonnet" {
		t.Fatalf("b 的模型：%q", m)
	}
	sa, _ := argvValue(a.Argv, "--settings")
	sb, _ := argvValue(b.Argv, "--settings")
	if sa == sb || !strings.Contains(sa, "acct-a") || !strings.Contains(sb, "acct-b") {
		t.Fatalf("設定檔要逐帳號：%s / %s", sa, sb)
	}
	if a.Socket != "eg" || b.Socket != "other" {
		t.Fatalf("tmux socket：%s / %s", a.Socket, b.Socket)
	}
}

// Guards: REQ-83
func TestStartValidation(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	cases := map[string]func(*sdk.EngineStartRequest){
		"沒有名稱":          func(r *sdk.EngineStartRequest) { r.Name = "" },
		"沒有帳號":          func(r *sdk.EngineStartRequest) { r.Account = "" },
		"沒有 config_dir": func(r *sdk.EngineStartRequest) { r.EngineConfig = map[string]any{} },
		"帳號含路徑符號":       func(r *sdk.EngineStartRequest) { r.Account = "../x" },
	}
	for name, mod := range cases {
		r := v.req("code")
		mod(r)
		if _, err := v.e.Start(ctx, r); !isCode(err, sdk.ErrorCodeInvalid) {
			t.Errorf("%s：要回 Invalid，得到 %v", name, err)
		}
	}
	if n := len(v.tmux.callsOf("start")); n != 0 {
		t.Fatalf("驗證失敗不該開 session，開了 %d 次", n)
	}
	// 同名的視窗還活著、又認不出是這個家開的：不動它，回 Conflict（是這個家開的就收掉重開，ECL-68，blockers_test.go）。
	v.tmux.setAlive("code", true)
	v.tmux.foreign = map[string]bool{"code": true}
	if _, err := v.e.Start(ctx, v.req("code")); !isCode(err, sdk.ErrorCodeConflict) {
		t.Fatalf("同名 session 還活著，要回 Conflict：%v", err)
	}
	v.tmux.startErr = os.ErrPermission
	if _, err := v.e.Start(ctx, v.req("other")); !isCode(err, sdk.ErrorCodeUnavailable) {
		t.Fatalf("tmux 開不起來要回 Unavailable：%v", err)
	}
}

// Guards: REQ-83
func TestModelAndEffortRules(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("a", nil)
	if hasFlag(c.Argv, "--model") || hasFlag(c.Argv, "--effort") {
		t.Fatalf("model、effort 空的時候不該帶：%q", c.Argv)
	}
	for _, e := range []string{"low", "medium", "high", "xhigh", "max"} {
		c := v.start("e-"+e, func(r *sdk.EngineStartRequest) { r.Effort = e })
		if got, _ := argvValue(c.Argv, "--effort"); got != e {
			t.Errorf("effort %q 要帶，得到 %q", e, got)
		}
	}
	for _, e := range []string{"High", "turbo", " "} {
		c := v.start("bad-"+strings.TrimSpace(e)+"x", func(r *sdk.EngineStartRequest) { r.Effort = e })
		if hasFlag(c.Argv, "--effort") {
			t.Errorf("effort %q 不在清單內，不該帶：%q", e, c.Argv)
		}
	}
}

// Guards: REQ-76
func TestStartHasNameNoMCPNoExtraPrompt(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("secretary-code", nil)
	if n, _ := argvValue(c.Argv, "--name"); n != "secretary-code" {
		t.Fatalf("--name 要等於 session 名稱：%q", n)
	}
	if hasFlag(c.Argv, "--mcp-config") {
		t.Fatalf("不准帶 --mcp-config：%q", c.Argv)
	}
	prompts := 0
	for _, a := range c.Argv {
		if a == "--append-system-prompt" {
			prompts++
		}
	}
	got, _ := argvValue(c.Argv, "--append-system-prompt")
	if prompts != 1 || got != v.req("x").Instructions {
		t.Fatalf("--append-system-prompt 只能出現一次且等於 Instructions：%d、%q", prompts, got)
	}
	c = v.start("no-instructions", func(r *sdk.EngineStartRequest) { r.Instructions = "" })
	if hasFlag(c.Argv, "--append-system-prompt") {
		t.Fatalf("沒有 Instructions 就不帶：%q", c.Argv)
	}
}

// Guards: REQ-83
func TestStartNativeByDefault(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("native", nil)
	for _, f := range []string{"--setting-sources", "--strict-mcp-config", "--mcp-config"} {
		if hasFlag(c.Argv, f) {
			t.Errorf("原生模式不該帶 %s：%q", f, c.Argv)
		}
	}
	c = v.start("isolated", func(r *sdk.EngineStartRequest) { r.EngineConfig["isolate"] = true })
	if s, _ := argvValue(c.Argv, "--setting-sources"); s != "project" || !hasFlag(c.Argv, "--strict-mcp-config") {
		t.Fatalf("isolate 要帶 --setting-sources project 與 --strict-mcp-config：%q", c.Argv)
	}
	if hasFlag(c.Argv, "--mcp-config") {
		t.Fatalf("隔離也不帶 --mcp-config：%q", c.Argv)
	}
	if last := c.Argv[len(c.Argv)-2]; last != "--add-dir" {
		t.Fatalf("--add-dir 要放最後（它吃後面所有的值）：%q", c.Argv)
	}
}

// Guards: REQ-83
func TestPermissionModeDefaultAuto(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("a", nil)
	if m, _ := argvValue(c.Argv, "--permission-mode"); m != "auto" {
		t.Fatalf("預設要 auto：%q", m)
	}
	c = v.start("b", func(r *sdk.EngineStartRequest) { r.EngineConfig["permission_mode"] = "plan" })
	if m, _ := argvValue(c.Argv, "--permission-mode"); m != "plan" {
		t.Fatalf("設定的模式要照用：%q", m)
	}
}

// Guards: REQ-83
func TestStartScrubsHostEnv(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("code", nil)
	env := envMap(c.Env)
	for _, k := range []string{"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT", "ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "EVERGENIE_ENGINE_SOCKET"} {
		if _, ok := env[k]; ok {
			t.Errorf("繼承來的 %s 要拿掉", k)
		}
	}
	for k := range env {
		if strings.HasPrefix(k, "CLAUDE") && k != "CLAUDE_CONFIG_DIR" || strings.HasPrefix(k, "ANTHROPIC_") {
			t.Errorf("不該留下 %s", k)
		}
	}
	if env["HOME"] != "/Users/tester" || env["TERM"] != "xterm-256color" {
		t.Errorf("一般變數要保留：%v", c.Env)
	}
	if env["EVERGENIE_SESSION"] != "code" || env["EVERGENIE_HOME"] != v.home {
		t.Errorf("EVERGENIE_* 要用這個 session 自己的，不是繼承來的：%v", c.Env)
	}
	if n := countKey(c.Env, "EVERGENIE_SESSION"); n != 1 {
		t.Errorf("EVERGENIE_SESSION 出現 %d 次", n)
	}
}

func countKey(env []string, key string) int {
	n := 0
	for _, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			n++
		}
	}
	return n
}

// Guards: REQ-97, REQ-98
func TestPathHasFrameworkBin(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("code", nil)
	fw := filepath.Join(v.home, "framework", "bin")
	path := envMap(c.Env)["PATH"]
	if !strings.HasPrefix(path, fw+string(os.PathListSeparator)) || !strings.HasSuffix(path, "/usr/local/bin:/usr/bin") {
		t.Fatalf("PATH 要在原本的前面加 framework/bin：%q", path)
	}
	if countKey(c.Env, "PATH") != 1 {
		t.Fatalf("PATH 只能一個：%v", c.Env)
	}
}

// Guards: REQ-82
func TestAttachmentsReadable(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("code", nil)
	share := filepath.Join(v.home, "runtime", "share")
	if d, _ := argvValue(c.Argv, "--add-dir"); d != share {
		t.Fatalf("上傳資料夾要在 --add-dir：%q", c.Argv)
	}
	if info, err := os.Stat(share); err != nil || !info.IsDir() {
		t.Fatalf("上傳資料夾要存在：%v", err)
	}
}

// Guards: REQ-84
func TestAccountSettingsUntouched(t *testing.T) {
	v := newEngEnv(t)
	original := []byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"old-core"}]}]}}`)
	settings := filepath.Join(v.cfgDir, "settings.json")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(original)
	listing := func() []string {
		entries, _ := os.ReadDir(v.cfgDir)
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	filesBefore := listing()
	ctx := context.Background()
	v.start("code", nil)
	v.tmux.setScreen("code", readScreen(t, "remote-control-active.txt"))
	if _, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.e.SetPermission(ctx, &sdk.EngineSetPermissionRequest{Account: "acct-a", Policy: &sdk.PermissionPolicy{Repos: []sdk.RepoPolicy{{Path: "/tmp/x"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(settings)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatalf("帳號的 settings.json 被改了：%v", err)
	}
	if !reflect.DeepEqual(listing(), filesBefore) {
		t.Fatalf("帳號資料夾多了檔案：%v → %v", filesBefore, listing())
	}
	if c := v.tmux.callsOf("start")[0]; !hasFlag(c.Argv, "--settings") {
		t.Fatal("hooks 要經 --settings 載入")
	}
}

// Guards: REQ-76
func TestDeliverPastesOnlyWhenAlive(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	r, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "hello", EnvelopeID: "E-1"})
	if err != nil || r.Delivered || r.Reason != "gone" {
		t.Fatalf("session 不在要回 delivered=false、gone：%+v %v", r, err)
	}
	if got := v.tmux.actions(); len(got) != 0 {
		t.Fatalf("session 不在不能貼也不能按鍵：%+v", got)
	}
	v.tmux.setAlive("code", true)
	r, err = v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "hello", EnvelopeID: "E-2"})
	if err != nil || !r.Delivered {
		t.Fatalf("session 在就要送：%+v %v", r, err)
	}
	var ops []string
	for _, c := range v.tmux.actions() {
		ops = append(ops, c.Op+":"+c.Arg)
	}
	if !reflect.DeepEqual(ops, []string{"paste:hello", "key:Enter"}) {
		t.Fatalf("順序要先貼再按 Enter：%v", ops)
	}
	if _, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "", Text: "x"}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("沒有 session 名稱要 Invalid：%v", err)
	}
	if _, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code"}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("沒有文字要 Invalid：%v", err)
	}
	v.tmux.pasteErr = os.ErrClosed
	r, _ = v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "x"})
	if r.Delivered || r.Reason != "paste" {
		t.Fatalf("貼上失敗要回 delivered=false、paste：%+v", r)
	}
}

// Guards: REQ-76
func TestPasteThenEnterDelay(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	text := "原樣的文字\n第二行"
	r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: text})
	if err != nil || !r.Delivered {
		t.Fatal(r, err)
	}
	pastes, keys := v.tmux.callsOf("paste"), v.tmux.callsOf("key")
	if len(pastes) != 1 || len(keys) != 1 || pastes[0].Arg != text || keys[0].Arg != "Enter" {
		t.Fatalf("要貼一次原文、按一次 Enter：%+v %+v", pastes, keys)
	}
	if gap := keys[0].At.Sub(pastes[0].At); gap < time.Second {
		t.Fatalf("Enter 要在貼上 1 秒之後，只隔了 %v", gap)
	}
	captures := v.tmux.callsOf("capture")
	// 貼上前看一次（確認框已經開著就不貼，ECL-40），送 Enter 前（1 秒之後）再看一次，送 Enter 後看輸入框清空了沒（ECL-42）。
	if len(captures) < 3 || captures[0].At.After(pastes[0].At) || captures[1].At.Before(pastes[0].At.Add(time.Second)) || captures[1].At.After(keys[0].At) || captures[2].At.Before(keys[0].At) {
		t.Fatalf("貼上前、送 Enter 前（在 1 秒之後）、送 Enter 後各看一次畫面：%+v", captures)
	}
}

// Guards: REQ-76
func TestNoEnterWhenDialogAppears(t *testing.T) {
	for _, screen := range []string{"permission-dialog.txt", "hook-ask-dialog.txt", "trust-folder.txt"} {
		v := newEngEnv(t)
		v.tmux.setAlive("code", true)
		v.tmux.setScreen("code", readScreen(t, screen))
		r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "hi"})
		if err != nil || r.Delivered || r.Reason != "dialog" {
			t.Fatalf("%s：有確認框要回 delivered=false、dialog：%+v %v", screen, r, err)
		}
		if len(v.tmux.callsOf("key")) != 0 {
			t.Fatalf("%s：確認框開著不能按任何鍵", screen)
		}
	}
	for _, screen := range []string{"remote-control-active.txt", ""} {
		v := newEngEnv(t)
		v.tmux.setAlive("code", true)
		if screen != "" {
			v.tmux.setScreen("code", readScreen(t, screen))
		}
		r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "hi"})
		if err != nil || !r.Delivered {
			t.Fatalf("%q：沒有確認框要送：%+v %v", screen, r, err)
		}
	}
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.captureErr = os.ErrClosed
	if r, _ := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "hi"}); r.Delivered || len(v.tmux.callsOf("key")) != 0 {
		t.Fatalf("看不到畫面不能送 Enter：%+v", r)
	}
}

// Guards: REQ-76
func TestPasteKeepsSpecialChars(t *testing.T) {
	var lines []string
	for i := 1; i <= 61; i++ {
		lines = append(lines, "第 "+string(rune('0'+i%10))+" 行")
	}
	texts := []string{
		`雙引號 "a" 與 'b' 單引號`,
		"$HOME ${PATH} `date` $(whoami)",
		`反斜線 \n \\ \"`,
		strings.Join(lines, "\n"),
		"［主人回答 M-3］好，\n不要 $1 \\ `x`",
	}
	for _, text := range texts {
		v := newEngEnv(t)
		v.tmux.setAlive("code", true)
		if r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: text}); err != nil || !r.Delivered {
			t.Fatal(r, err)
		}
		if got := v.tmux.callsOf("paste")[0].Arg; got != text {
			t.Fatalf("貼上的文字被改了：\n got %q\nwant %q", got, text)
		}
	}
}

// Guards: REQ-72
func TestGoneReportsSessionEnded(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.start("code", nil)
	v.start("life", nil)
	v.e.Wait()
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 0 {
		t.Fatalf("都還在，不該回報：%+v", v.host.events)
	}
	v.tmux.setAlive("code", false)
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 1 {
		t.Fatalf("code 掛了要回報一次：%+v", v.host.events)
	}
	ev := v.host.events[0]
	if ev.Type != sdk.EngineEventTypeSessionEnded || ev.Session != "code" || ev.Reason != "gone" {
		t.Fatalf("事件不對：%+v", ev)
	}
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 1 {
		t.Fatalf("同一個 session 不重複回報：%+v", v.host.events)
	}
	// 不是我們開的 session 不管它。
	v.tmux.setAlive("stranger", false)
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 1 {
		t.Fatalf("沒開過的 session 不該被檢查：%+v", v.host.events)
	}
	for _, c := range v.tmux.callsOf("alive") {
		if c.Name == "stranger" {
			t.Fatal("存活檢查只看自己開的 session")
		}
	}
}

// Guards: REQ-72
func TestStopKillsAndUnknownIsNotAnError(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.start("code", nil)
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	if k := v.tmux.callsOf("kill"); len(k) != 1 || k[0].Name != "code" {
		t.Fatalf("Stop 要 Kill：%+v", k)
	}
	if len(v.host.events) != 1 || v.host.events[0].Reason != "stopped" || v.host.events[0].Type != sdk.EngineEventTypeSessionEnded {
		t.Fatalf("Stop 後要回報 session 結束：%+v", v.host.events)
	}
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 1 {
		t.Fatalf("Stop 掉的不該再被報成 gone：%+v", v.host.events)
	}
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "never-started"}); err != nil {
		t.Fatalf("session 不在不算錯：%v", err)
	}
	if len(v.host.events) != 1 {
		t.Fatalf("沒開過的不該回報：%+v", v.host.events)
	}
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("沒名稱要 Invalid：%v", err)
	}
}

// 框已經被答掉（登記還沒撤）：不可以把按鍵打進工作中的秘書。
func TestAnswerBootPromptRechecksScreen(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v.start("code", nil)
	v.e.Wait()
	list, err := v.e.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{})
	if err != nil || len(list.Prompts) != 1 {
		t.Fatalf("要登記一個啟動對話框：%+v %v", list, err)
	}
	v.tmux.setScreen("code", "working…")
	p := list.Prompts[0]
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: p.Choices[0]}); !isCode(err, sdk.ErrorCodeNotFound) {
		t.Fatalf("框不在了要 NotFound：%v", err)
	}
	if keys := v.tmux.callsOf("key"); len(keys) != 0 {
		t.Fatalf("不該送按鍵：%v", keys)
	}
}

// Guards: REQ-85
func TestTrustDialogBootPrompt(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v.start("code", nil)
	v.e.Wait()
	list, err := v.e.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{})
	if err != nil || len(list.Prompts) != 1 {
		t.Fatalf("要登記一個啟動對話框：%+v %v", list, err)
	}
	p := list.Prompts[0]
	if p.Session != "code" || p.ID == "" || len(p.Choices) < 2 || p.Title == "" {
		t.Fatalf("對話框內容不對：%+v", p)
	}
	other, _ := v.e.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{Session: "life"})
	if len(other.Prompts) != 0 {
		t.Fatalf("依 session 篩選：%+v", other)
	}
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: "亂選"}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("不認得的選項要 Invalid：%v", err)
	}
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: p.Choices[0]}); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, c := range v.tmux.callsOf("key") {
		keys = append(keys, c.Name+":"+c.Arg)
	}
	if !reflect.DeepEqual(keys, []string{"code:Down", "code:Enter"}) {
		t.Fatalf("信任要送 Down、Enter：%v", keys)
	}
	if again, _ := v.e.ListBootPrompts(ctx, nil); len(again.Prompts) != 0 {
		t.Fatalf("回答後要移除：%+v", again)
	}
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: p.ID, Choice: p.Choices[0]}); !isCode(err, sdk.ErrorCodeNotFound) {
		t.Fatalf("回答過的要 NotFound：%v", err)
	}
	// 選離開：預設選項就是離開，只要 Enter。
	v2 := newEngEnv(t)
	v2.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v2.start("code", nil)
	v2.e.Wait()
	list, _ = v2.e.ListBootPrompts(ctx, nil)
	if _, err := v2.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: list.Prompts[0].ID, Choice: list.Prompts[0].Choices[1]}); err != nil {
		t.Fatal(err)
	}
	if k := v2.tmux.callsOf("key"); len(k) != 1 || k[0].Arg != "Enter" {
		t.Fatalf("離開只送 Enter：%+v", k)
	}
	// session 停掉就清掉它的對話框。
	v3 := newEngEnv(t)
	v3.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v3.start("code", nil)
	v3.e.Wait()
	_, _ = v3.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"})
	if l, _ := v3.e.ListBootPrompts(ctx, nil); len(l.Prompts) != 0 {
		t.Fatalf("Stop 後不該留下對話框：%+v", l)
	}
}

// Guards: REQ-85
func TestOtherScreensAreNotBootPrompts(t *testing.T) {
	for _, screen := range []string{"remote-control-active.txt", "permission-dialog.txt", "hook-ask-dialog.txt", ""} {
		v := newEngEnv(t)
		if screen != "" {
			v.tmux.setScreen("code", readScreen(t, screen))
		}
		v.start("code", nil)
		v.e.Wait()
		if l, _ := v.e.ListBootPrompts(context.Background(), nil); len(l.Prompts) != 0 {
			t.Fatalf("%q 不是啟動對話框：%+v", screen, l.Prompts)
		}
		captures := v.tmux.callsOf("capture")
		if len(captures) < 20 || len(captures) > 22 {
			t.Fatalf("%q：20 秒內每秒看一次，看了 %d 次", screen, len(captures))
		}
		last := captures[len(captures)-1].At
		if last.Sub(fixedNow) < 20*time.Second || last.Sub(fixedNow) > 21*time.Second {
			t.Fatalf("%q：偵測應在 20 秒後停止，最後一次在 %v", screen, last.Sub(fixedNow))
		}
	}
}

// Guards: REQ-85
func TestBootWatchStopsOnceFound(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v.start("code", nil)
	v.e.Wait()
	if n := len(v.tmux.callsOf("capture")); n != 1 {
		t.Fatalf("找到就不再看：看了 %d 次", n)
	}
	if v.clock.sleepCount() != 0 {
		t.Fatal("找到就不用再等")
	}
}

// Guards: REQ-81
func TestTranscriptSinceAndFollow(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	id := "00000000-0000-4000-8000-000000000001"
	dir := filepath.Join(v.cfgDir, "projects", projectDirName("/tmp/hg-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, id+".jsonl")
	line := func(uuid, text string) string {
		return `{"type":"user","uuid":"` + uuid + `","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"` + text + `"}}` + "\n"
	}
	if err := os.WriteFile(path, []byte(line("u1", "一")+line("u2", "二")), 0o644); err != nil {
		t.Fatal(err)
	}
	collect := func(req *sdk.EngineTranscriptRequest, ctx context.Context) []string {
		var got []string
		err := v.e.Transcript(ctx, req, func(r *sdk.EngineTranscriptResponse) error {
			got = append(got, r.Message.Uuid+":"+r.Message.Text)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	ctx := context.Background()
	if got := collect(&sdk.EngineTranscriptRequest{Session: "code"}, ctx); !reflect.DeepEqual(got, []string{"u1:一", "u2:二"}) {
		t.Fatalf("從頭：%v", got)
	}
	if got := collect(&sdk.EngineTranscriptRequest{Session: "code", SinceUuid: "u1"}, ctx); !reflect.DeepEqual(got, []string{"u2:二"}) {
		t.Fatalf("since：%v", got)
	}
	if got := collect(&sdk.EngineTranscriptRequest{Session: "code", SinceUuid: "u2"}, ctx); len(got) != 0 {
		t.Fatalf("since 最後一則：%v", got)
	}
	if got := collect(&sdk.EngineTranscriptRequest{Session: "code", SinceUuid: "lost"}, ctx); len(got) != 2 {
		t.Fatalf("游標找不到就從頭送：%v", got)
	}
	// Follow：第一輪 EOF 之後（Sleeper 被呼叫）才寫入新的一行，其中一行寫到一半。
	followCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	step := 0
	v.e.d.Sleeper = func(d time.Duration) {
		step++
		f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		defer f.Close()
		switch step {
		case 1:
			f.WriteString(line("u3", "三"))
			f.WriteString(`{"type":"user","uuid":"u4","timest`) // 寫到一半
		case 2:
			f.WriteString(`amp":"2026-10-24T10:00:01Z","message":{"role":"user","content":"四"}}` + "\n")
		default:
			cancel()
		}
	}
	got := collect(&sdk.EngineTranscriptRequest{Session: "code", SinceUuid: "u2", Follow: true}, followCtx)
	if !reflect.DeepEqual(got, []string{"u3:三", "u4:四"}) {
		t.Fatalf("follow 要等到整行寫完再送，且不重複：%v", got)
	}
	if err := v.e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "nobody"}, func(*sdk.EngineTranscriptResponse) error { return nil }); !isCode(err, sdk.ErrorCodeNotFound) {
		t.Fatalf("沒有這個 session 要 NotFound：%v", err)
	}
}

// DEV-22：停掉的 session（或 engine 重開之後）對話記錄還在磁碟上，要讀得到；沒開過的才是 NotFound。
// Guards: REQ-286
func TestTranscriptReadableAfterStop(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	id := "00000000-0000-4000-8000-000000000001"
	dir := filepath.Join(v.cfgDir, "projects", projectDirName("/tmp/hg-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"一"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	read := func(e *Engine) (int, error) {
		n := 0
		err := e.Transcript(context.Background(), &sdk.EngineTranscriptRequest{Session: "code"}, func(*sdk.EngineTranscriptResponse) error { n++; return nil })
		return n, err
	}
	if _, err := v.e.Stop(context.Background(), &sdk.EngineStopRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	if n, err := read(v.e); err != nil || n != 1 {
		t.Fatalf("停掉的 session 要讀得到對話記錄：%d %v", n, err)
	}
	// engine 重開：記憶是空的，只剩磁碟上記的位置。
	v.e.mu.Lock()
	v.e.sessions = map[string]*session{}
	v.e.mu.Unlock()
	if n, err := read(v.e); err != nil || n != 1 {
		t.Fatalf("engine 重開後要讀得到對話記錄：%d %v", n, err)
	}
}

// DEV-24：部署重開 engine 時已經在跑的 session 沒有對話檔位置紀錄，對話框不能空白：
// 退路讀核心記的 runtime/sessions/<名稱>.md 的 engine_session_id 與 cwd，再到這個家用過的帳號資料夾的 projects 找。
// Guards: REQ-286
func TestTranscriptFallsBackToSessionFile(t *testing.T) {
	v := newEngEnv(t)
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	id := "00000000-0000-4000-8000-0000000000aa"
	cwd := "/tmp/hg-running"
	dir := filepath.Join(userHome, ".claude-acme", "projects", projectDirName(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"一"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	sdir := filepath.Join(v.e.d.Home, "runtime", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nengine_session_id: " + id + "\ncwd: " + cwd + "\n---\n"
	if err := os.WriteFile(filepath.Join(sdir, "running.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	// 這個家用過 .claude-acme（別的 session 的位置紀錄裡有）。
	rememberConfigDir(t, v, "other", filepath.Join(userHome, ".claude-acme"))
	n := 0
	err := v.e.Transcript(context.Background(), &sdk.EngineTranscriptRequest{Session: "running"}, func(*sdk.EngineTranscriptResponse) error { n++; return nil })
	if err != nil || n != 1 {
		t.Fatalf("沒有位置紀錄也要從核心的 session 檔找到對話：%d %v", n, err)
	}
}

// rememberConfigDir 在這個家記一筆別的 session 的對話檔位置紀錄，等於這個家用過那個帳號資料夾。
func rememberConfigDir(t *testing.T, v *engEnv, session, configDir string) {
	t.Helper()
	data, _ := json.Marshal(transcriptRecord{ID: "00000000-0000-4000-8000-0000000000bb", Cwd: "/tmp/hg-other", ConfigDir: configDir})
	p := sessionFilePath(v.e.d.Home, "transcripts", session)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 退路只找這個家用過的帳號資料夾：同一台電腦上沒設定的 .claude-other（例如另一個家的帳號）裡就算有同編號的對話也不讀（SEC-373）。
func TestTranscriptFallbackSkipsUnconfiguredAccountDirs(t *testing.T) {
	v := newEngEnv(t)
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	id := "00000000-0000-4000-8000-0000000000cc"
	cwd := "/tmp/hg-running"
	dir := filepath.Join(userHome, ".claude-other", "projects", projectDirName(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := `{"type":"user","uuid":"u1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"別人的話"}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	sdir := filepath.Join(v.e.d.Home, "runtime", "sessions")
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nengine_session_id: " + id + "\ncwd: " + cwd + "\n---\n"
	if err := os.WriteFile(filepath.Join(sdir, "running.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	rememberConfigDir(t, v, "other", filepath.Join(userHome, ".claude-mine"))
	err := v.e.Transcript(context.Background(), &sdk.EngineTranscriptRequest{Session: "running"}, func(*sdk.EngineTranscriptResponse) error {
		t.Fatal("不能讀到沒設定的帳號資料夾裡的對話")
		return nil
	})
	if !isCode(err, sdk.ErrorCodeNotFound) {
		t.Fatalf("找不到要回 NotFound：%v", err)
	}
}

// Guards: REQ-81
func TestTranscriptFoundThroughSymlinkedCwd(t *testing.T) {
	v := newEngEnv(t)
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("不能建連結")
	}
	v.start("code", func(r *sdk.EngineStartRequest) { r.Cwd = link })
	realResolved, _ := filepath.EvalSymlinks(real)
	dir := filepath.Join(v.cfgDir, "projects", projectDirName(realResolved))
	_ = os.MkdirAll(dir, 0o755)
	data := `{"type":"assistant","uuid":"a1","timestamp":"2026-10-24T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"好"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "00000000-0000-4000-8000-000000000001.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := v.e.Transcript(context.Background(), &sdk.EngineTranscriptRequest{Session: "code"}, func(*sdk.EngineTranscriptResponse) error { n++; return nil }); err != nil || n != 1 {
		t.Fatalf("連結的資料夾要找得到 transcript：%d %v", n, err)
	}
}

// Guards: REQ-81
func TestProjectDirName(t *testing.T) {
	if got := projectDirName("/tmp/hg-s0c/repo_1.x"); got != "-tmp-hg-s0c-repo-1-x" {
		t.Fatalf("得到 %q", got)
	}
}

// Guards: REQ-86
func TestUsageNeedsConfigDirAndCapabilities(t *testing.T) {
	v := newEngEnv(t)
	if _, err := v.e.Usage(context.Background(), &sdk.EngineUsageRequest{}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("沒有 config_dir 要回 Invalid：%v", err)
	}
	caps, err := v.e.Capabilities(context.Background(), &sdk.EngineCapabilitiesRequest{})
	if err != nil || caps.Engine != "claude" || caps.Version == "" {
		t.Fatalf("%+v %v", caps, err)
	}
}

// Guards: REQ-76
func TestFirstMessageIgnored(t *testing.T) {
	v := newEngEnv(t)
	c := v.start("code", func(r *sdk.EngineStartRequest) { r.FirstMessage = "第一則：請做 X" })
	for _, a := range c.Argv {
		if strings.Contains(a, "第一則") {
			t.Fatalf("FirstMessage 要忽略，核心經派送器送：%q", c.Argv)
		}
	}
	if len(v.tmux.callsOf("paste")) != 0 {
		t.Fatal("Start 不貼任何東西")
	}
}

// 能力旗標 resume 開著：關掉的 session 可以用 --resume 接回（P7-5、D-33）；核心只拿來顯示。
// Guards: REQ-219
func TestResumeCapability(t *testing.T) {
	v := newEngEnv(t)
	caps, err := v.e.Capabilities(context.Background(), &sdk.EngineCapabilitiesRequest{})
	if err != nil || !caps.Features["resume"] {
		t.Fatalf("%+v %v", caps, err)
	}
}

// 核心重查（同一個封包 id 再交來，ECL-39、ECL-42）：上一次其實收下了就回 already、不重貼；還留在輸入框只補 Enter；
// 貼上丟了才重貼。
// Guards: REQ-385
func TestDeliverRecheckDoesNotPasteTwice(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	ctx := context.Background()
	path := v.transcriptFile(t)
	ops := func() []string {
		var out []string
		for _, c := range v.tmux.actions() {
			if c.Op == "paste" || c.Op == "key" {
				out = append(out, c.Op+":"+c.Arg)
			}
		}
		return out
	}
	deliver := func(id, text string) *sdk.EngineDeliverResponse {
		t.Helper()
		r, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: text, EnvelopeID: id})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// 1. 貼上丟了：第一次按了 Enter 也沒看到送出 → 回 enter；重查時輸入框裡沒有 → 重貼，這次送出了。
	v.tmux.dropPaste = true
	if r := deliver("E-1", "第一句話"); r.Delivered || r.Reason != "enter" {
		t.Fatalf("貼上丟了不能說送到：%+v", r)
	}
	v.tmux.dropPaste = false
	if r := deliver("E-1", "第一句話"); !r.Delivered || r.Reason != "" {
		t.Fatalf("沒看到要重貼並送出：%+v", r)
	}
	if got := ops(); !reflect.DeepEqual(got, []string{"paste:第一句話", "key:Enter", "paste:第一句話", "key:Enter"}) {
		t.Fatalf("沒看到就重貼一次：%v", got)
	}
	// 2. Enter 被吃掉（字還在輸入框）→ 回 enter；重查只補 Enter，輸入框清空才算送到。
	v.tmux.eatEnter = true
	if r := deliver("E-2", "第二句話：請幫我看看"); r.Delivered || r.Reason != "enter" {
		t.Fatalf("Enter 被吃掉不能說送到：%+v", r)
	}
	v.tmux.eatEnter = false
	before := len(ops())
	if r := deliver("E-2", "第二句話：請幫我看看"); !r.Delivered || r.Reason != "" {
		t.Fatalf("在輸入框裡：補 Enter：%+v", r)
	}
	if got := ops()[before:]; !reflect.DeepEqual(got, []string{"key:Enter"}) {
		t.Fatalf("在輸入框裡只補 Enter、不重貼：%v", got)
	}
	// 3. 對話記錄裡已經有（核心又交來一次）→ already、什麼都不按。
	deliver("E-3", "第三句")
	ts := v.clock.Now().UTC().Format(time.RFC3339Nano)
	line := `{"type":"user","uuid":"u3","timestamp":"` + ts + `","message":{"role":"user","content":"第三句"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	before = len(ops())
	if r := deliver("E-3", "第三句"); !r.Delivered || r.Reason != sdk.DeliverAlready {
		t.Fatalf("對話記錄有了要回 already：%+v", r)
	}
	if got := ops()[before:]; len(got) != 0 {
		t.Fatalf("已經到了不能再貼或按鍵：%v", got)
	}
	// 3b. 核心帶 Force（等過那句、她一直沒有接著做）：不回 already，照常再貼一次。
	before = len(ops())
	forced, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "第三句", EnvelopeID: "E-3", Force: true})
	if err != nil || forced.Reason == sdk.DeliverAlready {
		t.Fatalf("帶 Force 不能回 already：%+v %v", forced, err)
	}
	if got := ops()[before:]; !reflect.DeepEqual(got, []string{"paste:第三句", "key:Enter"}) {
		t.Fatalf("帶 Force 要真的重貼：%v", got)
	}
	// 3c. 引擎重開過、不記得貼過什麼：對話記錄最近已經有這句就照重查處理（already），不貼第二次。
	v.e.mu.Lock()
	delete(v.e.delivered, "code")
	v.e.mu.Unlock()
	before = len(ops())
	resent, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "第三句", EnvelopeID: "E-3", Resend: true})
	if err != nil || !resent.Delivered || resent.Reason != sdk.DeliverAlready {
		t.Fatalf("不記得貼過、核心說是重送、對話記錄裡有：要回 already：%+v %v", resent, err)
	}
	if got := ops()[before:]; len(got) != 0 {
		t.Fatalf("對話記錄裡已經有就不能再貼：%v", got)
	}
	// 3d. 一樣不記得，但核心沒說是重送（第一次送的新話，只是開頭跟對話裡的舊話一樣）：照常貼，不能當成已經到了。
	v.e.mu.Lock()
	delete(v.e.delivered, "code")
	v.e.mu.Unlock()
	before = len(ops())
	if r := deliver("E-3-new", "第三句"); r.Reason == sdk.DeliverAlready {
		t.Fatalf("第一次送的話不能因為對話裡有一樣的舊話就說已經到了：%+v", r)
	}
	if got := ops()[before:]; !reflect.DeepEqual(got, []string{"paste:第三句", "key:Enter"}) {
		t.Fatalf("新話要照常貼：%v", got)
	}
	v.e.mu.Lock()
	delete(v.e.delivered, "code")
	v.e.mu.Unlock()
	// 4. 平台已經觸發這句的 UserPromptSubmit（turns 檔）→ 也是 already。
	v.tmux.typing = false // 畫面看不出來
	if r := deliver("E-4", "第四句：只有 turns 檔知道"); r.Delivered {
		t.Fatalf("什麼都沒看到不能說送到：%+v", r)
	}
	v.writeTurn(t, "第四句：只有 turns 檔知道", v.clock.Now())
	before = len(ops())
	if r := deliver("E-4", "第四句：只有 turns 檔知道"); !r.Delivered || r.Reason != sdk.DeliverAlready {
		t.Fatalf("turns 檔有了要回 already：%+v", r)
	}
	if got := ops()[before:]; len(got) != 0 {
		t.Fatalf("已經到了不能再貼或按鍵：%v", got)
	}
	// 5. 不同的封包照常貼（不會被當成重查）。
	v.tmux.typing = true
	before = len(ops())
	deliver("E-5", "第三句")
	if got := ops()[before:]; len(got) != 2 {
		t.Fatalf("新的封包照常貼：%v", got)
	}
}

// 貼上＋Enter 之後引擎自己確認送出才回 delivered（ECL-42，S5 D4）：輸入框清空（真機畫面樣本）、UserPromptSubmit 的 turns 檔、
// 對話記錄，任一個對上就算；都沒有就在 2 秒後回 enter（沿用既有碼），引擎自己不重貼、不重按。
func TestDeliverConfirmsSubmitBeforeDelivered(t *testing.T) {
	ctx := context.Background()
	count := func(v *engEnv, op string) int { return len(v.tmux.callsOf(op)) }

	// 真機畫面樣本：貼上後字在輸入框，Enter 後輸入框清空 → delivered。
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.setScreen("code", readScreen(t, "remote-control-active.txt"))
	if r, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "幫我看一下今天的單", EnvelopeID: "E-1"}); err != nil || !r.Delivered {
		t.Fatalf("輸入框清空要算送到：%+v %v", r, err)
	}

	// 畫面看不出來、也沒有任何回報 → 2 秒後回 enter，只貼一次、按一次。
	v = newEngEnv(t)
	v.start("code", nil)
	v.transcriptFile(t)
	v.tmux.typing = false
	start := v.clock.Now()
	r, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "沒人收", EnvelopeID: "E-1"})
	if err != nil || r.Delivered || r.Reason != "enter" {
		t.Fatalf("沒確認送出要回 delivered=false、enter：%+v %v", r, err)
	}
	if waited := v.clock.Now().Sub(start); waited < pasteEnterDelay+submitConfirmWithin {
		t.Fatalf("要等滿確認時間才放棄：只等了 %v", waited)
	}
	if count(v, "paste") != 1 || count(v, "key") != 1 {
		t.Fatalf("引擎自己不重試：paste=%d key=%d", count(v, "paste"), count(v, "key"))
	}

	// Enter 被吃掉（字還在輸入框）→ enter。
	v = newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.eatEnter = true
	if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "還在框裡"}); r.Delivered || r.Reason != "enter" {
		t.Fatalf("字還在輸入框不能說送到：%+v", r)
	}

	// 貼上丟了（看得到輸入框、裡面沒有這句）：不按 Enter（主人打了一半的字不能被送出去），回 paste 讓核心再交來。
	// 短句「test」不能被空輸入框的提示字（Try "write a test …"）當成還在框裡。
	for _, text := range []string{"丟了", "test"} {
		v = newEngEnv(t)
		v.tmux.setAlive("code", true)
		v.tmux.setScreen("code", readScreen(t, "remote-control-active.txt"))
		v.tmux.dropPaste = true
		if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: text}); r.Delivered || r.Reason != "paste" {
			t.Fatalf("%q：貼上丟了要回 paste：%+v", text, r)
		}
		if count(v, "key") != 0 {
			t.Fatalf("%q：貼上丟了不能按 Enter", text)
		}
	}

	// 她忙著（插話）：Enter 後那句排到輸入框外面、平台的回報還沒到 → 輸入框清空就算送到；
	// 核心再交來同一個封包（重查）時，輸入框空、對話區看得到這句 → 不重貼不按，回 enter 讓核心晚點再查。
	v = newEngEnv(t)
	v.start("code", nil)
	v.transcriptFile(t)
	v.tmux.setScreen("code", readScreen(t, "remote-control-active.txt"))
	v.tmux.echo = true
	if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "插話：先停一下", EnvelopeID: "E-9"}); !r.Delivered {
		t.Fatalf("插話時輸入框清空要算送到：%+v", r)
	}
	pastes, keys := count(v, "paste"), count(v, "key")
	if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "插話：先停一下", EnvelopeID: "E-9"}); r.Delivered || r.Reason != "enter" {
		t.Fatalf("對話區看得到、回報還沒到：要回 enter、不重貼：%+v", r)
	}
	if count(v, "paste") != pastes || count(v, "key") != keys {
		t.Fatalf("對話區看得到這句時重查不能重貼也不能按：paste %d→%d key %d→%d", pastes, count(v, "paste"), keys, count(v, "key"))
	}

	// 引擎要關（ctx 取消）：不空等滿確認時間。
	v = newEngEnv(t)
	v.tmux.setAlive("code", true)
	v.tmux.typing = false
	cctx, cancel := context.WithCancel(ctx)
	v.tmux.onKey = func(name, key string) (string, bool) { cancel(); return "", false }
	start = v.clock.Now()
	if r, _ := v.e.Deliver(cctx, &sdk.EngineDeliverRequest{Session: "code", Text: "要關了"}); r.Delivered {
		t.Fatalf("沒確認送出不能說送到：%+v", r)
	}
	if waited := v.clock.Now().Sub(start); waited >= pasteEnterDelay+submitConfirmWithin {
		t.Fatalf("ctx 取消後不該等滿：等了 %v", waited)
	}

	// 畫面看不出來，但按 Enter 之後平台觸發了這句的 UserPromptSubmit（turns 檔）→ delivered。
	v = newEngEnv(t)
	v.start("code", nil)
	v.transcriptFile(t)
	v.tmux.typing = false
	v.tmux.onKey = func(name, key string) (string, bool) {
		v.writeTurn(t, "［主人］turns 檔對上\n第二行", v.clock.Now())
		return "", false
	}
	if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "［主人］turns 檔對上\n第二行"}); !r.Delivered || r.Reason != "" {
		t.Fatalf("UserPromptSubmit 對上要算送到：%+v", r)
	}

	// turns 檔是這次貼上之前的、或是別句話 → 不算。
	for _, tc := range []struct {
		name string
		head string
		at   time.Duration
	}{{"貼上之前的", "舊的那一句", -time.Minute}, {"別句話", "別人的話", 0}} {
		v = newEngEnv(t)
		v.start("code", nil)
		v.transcriptFile(t)
		v.tmux.typing = false
		text := "舊的那一句"
		if tc.at == 0 {
			text = "這一句"
		}
		head, at := tc.head, tc.at
		v.tmux.onKey = func(name, key string) (string, bool) {
			v.writeTurn(t, head, v.clock.Now().Add(at))
			return "", false
		}
		if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: text}); r.Delivered {
			t.Fatalf("%s的 turns 檔不算：%+v", tc.name, r)
		}
	}

	// 畫面看不出來，但對話記錄有了 → delivered。
	v = newEngEnv(t)
	v.start("code", nil)
	path := v.transcriptFile(t)
	v.tmux.typing = false
	v.tmux.onKey = func(name, key string) (string, bool) {
		ts := v.clock.Now().UTC().Format(time.RFC3339Nano)
		_ = os.WriteFile(path, []byte(`{"type":"user","uuid":"u1","timestamp":"`+ts+`","message":{"role":"user","content":"對話記錄對上"}}`+"\n"), 0o644)
		return "", false
	}
	if r, _ := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "對話記錄對上"}); !r.Delivered {
		t.Fatalf("對話記錄對上要算送到：%+v", r)
	}
}

// 輸入框（最後兩條橫線中間）在真機畫面樣本上找得到；空的輸入框只有提示字。
func TestDeliverInputBoxOnRealScreen(t *testing.T) {
	box, ok := inputBox(readScreen(t, "remote-control-active.txt"))
	if !ok || strings.TrimSpace(box) != "" {
		t.Fatalf("樣本的空輸入框（提示字拿掉）：%q %v", box, ok)
	}
	if holdsText(box, "幫我看一下今天的單") || holdsText(box, "test") {
		t.Fatal("空的輸入框不含這句，提示字也不算")
	}
	if box, _ := inputBox(withBox(readScreen(t, "remote-control-active.txt"), "幫我看一下今天的單")); !holdsText(box, "幫我看一下今天的單") {
		t.Fatalf("貼上後的輸入框要看得到這句：%q", box)
	}
	if _, ok := inputBox("沒有橫線的畫面\n第二行"); ok {
		t.Fatal("沒有橫線就是找不到輸入框")
	}
	if !holdsText("❯ [Pasted text #1 +40 lines]", "很長的一段\n…") {
		t.Fatal("長文字收成 [Pasted text] 也算在框裡")
	}
}

// 確認框已經開著：不貼也不按（貼進去的字可能被當成回答），回 dialog（ECL-40）。
// Guards: REQ-391, REQ-76
func TestDeliverRefusesWhenDialogAlreadyOpen(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("code", true)
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "spike", "screens", "permission-dialog.txt"))
	if err != nil {
		t.Fatal(err)
	}
	v.tmux.setScreen("code", string(data))
	r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "code", Text: "在嗎", EnvelopeID: "E-1"})
	if err != nil || r.Delivered || r.Reason != "dialog" {
		t.Fatalf("確認框開著要回 dialog：%+v %v", r, err)
	}
	if got := v.tmux.actions(); len(got) != 0 {
		t.Fatalf("確認框開著不能貼也不能按：%+v", got)
	}
}
