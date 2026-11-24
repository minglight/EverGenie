package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

func runCommand(t *testing.T, v *engEnv, name string, cmd sdk.SessionCommand, arg string) *sdk.EngineCommandResponse {
	t.Helper()
	r, err := v.e.Command(context.Background(), &sdk.EngineCommandRequest{Session: name, Command: cmd, Arg: arg, By: &sdk.Actor{Name: "owner(web)"}})
	if err != nil {
		t.Fatalf("Command：%v", err)
	}
	return r
}

func keys(v *engEnv) []string {
	var out []string
	for _, c := range v.tmux.callsOf("key") {
		out = append(out, c.Arg)
	}
	return out
}

// 看板下的指令換成平台的斜線指令原字貼進輸入框（不補空白、不包封包），按 Enter、輸入框清空才算做成。
func TestCommandPastesSlashCommandAndConfirmsInputCleared(t *testing.T) {
	for _, tc := range []struct {
		cmd  sdk.SessionCommand
		arg  string
		want string
	}{
		{sdk.SessionCommandClear, "", "/clear"},
		{sdk.SessionCommandCompact, "", "/compact"},
		{sdk.SessionCommandModel, "opus", "/model opus"},
		{sdk.SessionCommandEffort, "high", "/effort high"},
	} {
		v := newEngEnv(t)
		v.tmux.setAlive("health", true)
		v.tmux.setScreen("health", readScreen(t, "labeled-rule.txt"))
		r := runCommand(t, v, "health", tc.cmd, tc.arg)
		if !r.Done || r.Reason != "" {
			t.Fatalf("%s：要做成：%+v", tc.cmd, r)
		}
		pastes := v.tmux.callsOf("paste")
		if len(pastes) != 1 || pastes[0].Arg != tc.want {
			t.Fatalf("%s：貼上的要是 %q：%+v", tc.cmd, tc.want, pastes)
		}
		if k := keys(v); len(k) != 1 || k[0] != "Enter" {
			t.Fatalf("%s：只按一次 Enter：%v", tc.cmd, k)
		}
	}
}

// 換模型時平台先問「要換嗎」：按 1 同意，問題收掉、輸入框清空才算做成。
func TestCommandModelAnswersSwitchPrompt(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	empty := readScreen(t, "labeled-rule.txt")
	v.tmux.setScreen("health", empty)
	prompt := "Switch model?\nSwitching models mid-conversation re-reads the whole conversation.\n❯ 1. Yes, switch\n  2. No, keep current model\n"
	v.tmux.onKey = func(_, key string) (string, bool) {
		switch key {
		case "Enter":
			return prompt, true
		case "1":
			return empty, true
		}
		return "", false
	}
	r := runCommand(t, v, "health", sdk.SessionCommandModel, "opus")
	if !r.Done {
		t.Fatalf("按了 1 之後要做成：%+v", r)
	}
	if k := keys(v); strings.Join(k, ",") != "Enter,1" {
		t.Fatalf("按鍵要是 Enter 再 1：%v", k)
	}
}

// 畫面上開著要人按的確認框：不貼（指令會被當成回答），回 dialog。
func TestCommandRefusesWhenConfirmDialogOpen(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	v.tmux.setScreen("health", readScreen(t, "permission-dialog.txt"))
	r := runCommand(t, v, "health", sdk.SessionCommandClear, "")
	if r.Done || r.Reason != sdk.CommandDialog {
		t.Fatalf("要回 dialog：%+v", r)
	}
	if len(v.tmux.actions()) != 0 {
		t.Fatalf("不能動她的畫面：%+v", v.tmux.actions())
	}
}

// session 不在回 gone；不認得的指令回 unsupported、不認得的選項回 bad_arg；都不碰畫面。
func TestCommandGoneAndUnsupported(t *testing.T) {
	v := newEngEnv(t)
	if r := runCommand(t, v, "health", sdk.SessionCommandClear, ""); r.Done || r.Reason != "gone" {
		t.Fatalf("session 不在要回 gone：%+v", r)
	}
	v.tmux.setAlive("health", true)
	for _, tc := range []struct {
		cmd  sdk.SessionCommand
		arg  string
		want string
	}{
		{"logout", "", sdk.CommandUnsupported},
		{sdk.SessionCommandModel, "", sdk.CommandBadArg},
		{sdk.SessionCommandModel, "gpt", sdk.CommandBadArg},
		{sdk.SessionCommandModel, "opus; rm", sdk.CommandBadArg},
		{sdk.SessionCommandEffort, "ultra", sdk.CommandBadArg},
		{sdk.SessionCommandClear, "x", sdk.CommandBadArg},
	} {
		if r := runCommand(t, v, "health", tc.cmd, tc.arg); r.Done || r.Reason != tc.want {
			t.Fatalf("%s %q 要回 %s：%+v", tc.cmd, tc.arg, tc.want, r)
		}
	}
	if len(v.tmux.actions()) != 0 {
		t.Fatalf("不能動她的畫面：%+v", v.tmux.actions())
	}
}

// 主人在終端機打了一半的字還在輸入框：不貼（會接成一句），回 typing。
func TestCommandRefusesWhenInputBoxHasTyping(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	v.tmux.setScreen("health", readScreen(t, "labeled-rule-typed.txt"))
	if r := runCommand(t, v, "health", sdk.SessionCommandClear, ""); r.Done || r.Reason != sdk.CommandTyping {
		t.Fatalf("要回 typing：%+v", r)
	}
	if len(v.tmux.callsOf("paste")) != 0 {
		t.Fatal("不能貼")
	}
}

// Enter 被吃掉（字還在輸入框）：5 秒內等不到收下，回 enter，不重按。
func TestCommandNotSubmittedWhenEnterEaten(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	v.tmux.setScreen("health", readScreen(t, "labeled-rule.txt"))
	v.tmux.eatEnter = true
	r := runCommand(t, v, "health", sdk.SessionCommandCompact, "")
	if r.Done || r.Reason != reasonNotSubmitted {
		t.Fatalf("要回 %q：%+v", reasonNotSubmitted, r)
	}
	if k := keys(v); len(k) != 1 {
		t.Fatalf("只按一次 Enter：%v", k)
	}
}

// 清掉重來：hook 把新的對話編號寫進紀錄之後，Command 帶回新的編號，看板讀的對話記錄也換成新的那份（ECL-47）。
func TestCommandClearReturnsNewTranscriptID(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	v.tmux.setScreen("code", readScreen(t, "labeled-rule.txt"))
	const next = "11111111-0000-4000-8000-000000000009"
	v.tmux.onKey = func(name, key string) (string, bool) {
		if key == "Enter" {
			r := readTranscriptRecord(v.home, name)
			r.ID = next
			data, _ := json.Marshal(r)
			if err := os.WriteFile(sessionFilePath(v.home, "transcripts", name), data, 0o644); err != nil {
				t.Error(err)
			}
		}
		return "", false
	}
	r := runCommand(t, v, "code", sdk.SessionCommandClear, "")
	if !r.Done || r.EngineSessionID != next {
		t.Fatalf("要做成並帶回新的編號：%+v", r)
	}
	if s := v.e.transcriptSource("code"); s == nil || s.id != next {
		t.Fatalf("對話記錄要換成新的那份：%+v", s)
	}
}

// 輸入框上緣帶 session 名字的線（「────…──── health ─」）也是輸入框的邊：輸入框只有框裡那一行，不含對話。
func TestInputBoxFindsLabeledTopRule(t *testing.T) {
	typed := readScreen(t, "labeled-rule-typed.txt")
	box, ok := inputBox(typed)
	if !ok || !strings.Contains(box, "/model opus") || strings.Contains(box, "早安") || strings.Contains(box, "健身") {
		t.Fatalf("輸入框要只有框裡那行：ok=%v %q", ok, box)
	}
	if holds, found := boxHolds(readScreen(t, "labeled-rule.txt"), "健身"); !found || holds {
		t.Fatalf("空的輸入框不能把對話區的字算進來：holds=%v found=%v", holds, found)
	}
	if out := outsideBox(typed); strings.Contains(out, "/model opus") || !strings.Contains(out, "早安") {
		t.Fatalf("輸入框外面要是對話區：%q", out)
	}
}

// 帶名字的線要長得剛好像輸入框上緣才算：對話裡的分隔線、表格、兩邊都很長的標題線不算。
func TestIsRuleLabeledOnlyForInputBoxTop(t *testing.T) {
	long := strings.Repeat("─", 40)
	for line, want := range map[string]bool{
		strings.Repeat("─", 3):                      true,
		"  " + long + "  ":                          true,
		long + " health ─":                          true,
		long + " code-dev-12 ───":                   true,
		long + " 健康 ─":                              true,
		"──":                                        false,
		"── 備註 " + long:                             false, // 左邊太短
		long + " 標題 " + long:                        false, // 右邊太長：markdown 標題線
		long + " │ x │ ─":                           false, // 表格
		long + "health─":                            false, // 名字兩邊要有空白
		long + " " + strings.Repeat("名", 41) + " ─": false,
		"⏺ " + long + " health ─":                   false,
	} {
		if got := isRule(line); got != want {
			t.Errorf("isRule(%q) = %v，要 %v", line, got, want)
		}
	}
}

// 訊息開頭是 / 時當純文字送：補一個空白，平台才不會當成斜線指令（ECL-32）。
func TestDeliverEscapesLeadingSlash(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	text := "/clear 這個指令是做什麼的？"
	if r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "health", Text: text}); err != nil || !r.Delivered {
		t.Fatalf("要送出：%+v %v", r, err)
	}
	if got := v.tmux.callsOf("paste"); len(got) != 1 || got[0].Arg != " "+text {
		t.Fatalf("開頭的 / 前面要補空白：%+v", got)
	}
	if safeLeading("看 /tmp") != "看 /tmp" {
		t.Fatal("不是開頭的 / 不動")
	}
}

// 清掉重來（SessionEnd reason=clear、SessionStart source=clear）：不回報對話結束；新的對話編號寫進紀錄，
// 工作資料夾與帳號資料夾沿用原本的（ECL-47）。
func TestHookClearSwitchesTranscriptRecord(t *testing.T) {
	e := newHookEnv(t)
	old := transcriptRecord{ID: "00000000-0000-4000-8000-000000000001", Cwd: "/tmp/hg-work", ConfigDir: "/Users/tester/.claude-acct"}
	data, _ := json.Marshal(old)
	if err := writeFileAtomic(sessionFilePath(e.home, "transcripts", "code"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code := e.run("SessionEnd", []byte(`{"hook_event_name":"SessionEnd","reason":"clear"}`)); code != 0 {
		t.Fatal(code)
	}
	if len(e.host.events) != 0 {
		t.Fatalf("清掉重來不是對話結束，不回報：%+v", e.host.events)
	}
	const next = "22222222-0000-4000-8000-000000000002"
	if _, code := e.run("SessionStart", []byte(`{"hook_event_name":"SessionStart","source":"clear","session_id":"`+next+`","cwd":"/elsewhere"}`)); code != 0 {
		t.Fatal(code)
	}
	r := readTranscriptRecord(e.home, "code")
	if r == nil || r.ID != next || r.Cwd != old.Cwd || r.ConfigDir != old.ConfigDir {
		t.Fatalf("紀錄要換成新的編號、其他沿用：%+v", r)
	}
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeSessionStarted {
		t.Fatalf("SessionStart 照常回報：%+v", e.host.events)
	}
	// 一般的 SessionStart 不動紀錄。
	if _, code := e.run("SessionStart", []byte(`{"hook_event_name":"SessionStart","source":"resume","session_id":"33333333-0000-4000-8000-000000000003"}`)); code != 0 {
		t.Fatal(code)
	}
	if r := readTranscriptRecord(e.home, "code"); r.ID != next {
		t.Fatalf("resume 不改紀錄：%+v", r)
	}
}

// 只有畫面最下面真的開著「Switch model?」＋編號選項才按 1；對話裡講到這句英文不算（不然「1」會打進空的輸入框）。
func TestSwitchPromptOnlyWhenDialogAtBottom(t *testing.T) {
	dialog := "Switch model?\nSwitching models mid-conversation re-reads the whole conversation.\n❯ 1. Yes, switch\n  2. No, keep current model\n"
	if !hasSwitchPrompt(dialog) {
		t.Fatal("確認題要認得")
	}
	rule := strings.Repeat("─", 80)
	talk := "● 換模型時 Claude Code 會問「Switch model?」，選 1 就換。\n  1. 先打 /model\n  2. 再按 Enter\n" +
		strings.Repeat("\n", 3) + rule + "\n❯ \n" + rule + "\n  ? for shortcuts\n"
	if hasSwitchPrompt(talk) {
		t.Fatal("對話裡講到這句不算確認題")
	}
	old := "Switch model?\n❯ 1. Yes, switch\n" + strings.Repeat("● 之後的對話\n", 20) + rule + "\n❯ \n" + rule + "\n"
	if hasSwitchPrompt(old) {
		t.Fatal("捲上去的舊確認題不算")
	}
}

// 跟著看對話記錄時主人清掉重來（ECL-47）：紀錄換成新的編號之後，從新檔開頭送，舊檔後面的不再送。
func TestTranscriptFollowSwitchesFileAfterClear(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	oldID := "00000000-0000-4000-8000-000000000001"
	newID := "22222222-0000-4000-8000-000000000002"
	dir := filepath.Join(v.cfgDir, "projects", projectDirName("/tmp/hg-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := func(uuid, text string) string {
		return `{"type":"user","uuid":"` + uuid + `","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"` + text + `"}}` + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, oldID+".jsonl"), []byte(line("u1", "一")+line("u2", "二")), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	step := 0
	v.e.d.Sleeper = func(time.Duration) {
		step++
		switch step {
		case 1:
			// 平台清掉重來：新的檔、紀錄換編號（hook 做的事）。
			_ = os.WriteFile(filepath.Join(dir, newID+".jsonl"), []byte(line("n1", "新的")), 0o644)
			r := readTranscriptRecord(v.e.d.Home, "code")
			r.ID = newID
			data, _ := json.Marshal(r)
			_ = writeFileAtomic(sessionFilePath(v.e.d.Home, "transcripts", "code"), data, 0o644)
		default:
			cancel()
		}
	}
	var got []string
	err := v.e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "code", SinceUuid: "u1", Follow: true}, func(r *sdk.EngineTranscriptResponse) error {
		got = append(got, r.Message.Uuid)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"u2", "n1"}) {
		t.Fatalf("清掉之後要從新檔開頭接著送：%v", got)
	}
}

// 叫她停下（ECL-52）：在她畫面上按一次 Esc、不貼字，然後回報閒著；session 不在回 gone。
func TestCommandInterruptPressesEscapeAndReportsIdle(t *testing.T) {
	v := newEngEnv(t)
	if r := runCommand(t, v, "health", sdk.SessionCommandInterrupt, ""); r.Done || r.Reason != "gone" {
		t.Fatalf("session 不在要回 gone：%+v", r)
	}
	v.tmux.setAlive("health", true)
	if r := runCommand(t, v, "health", sdk.SessionCommandInterrupt, ""); !r.Done {
		t.Fatalf("要做成：%+v", r)
	}
	acts := v.tmux.actions()
	if len(acts) != 1 || acts[0].Op != "key" || acts[0].Arg != "Escape" {
		t.Fatalf("只按一次 Esc：%+v", acts)
	}
	v.host.mu.Lock()
	defer v.host.mu.Unlock()
	if n := len(v.host.events); n != 1 || v.host.events[0].Type != sdk.EngineEventTypeIdleHint || v.host.events[0].Session != "health" || !v.host.events[0].Interrupted || !v.host.events[0].At.Equal(acts[0].At) {
		t.Fatalf("要回報閒著：%+v", v.host.events)
	}
}
