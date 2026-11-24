package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// fakeClaude 寫一支假的 claude 執行檔（shell）：把 argv、stdin、CLAUDE_CONFIG_DIR、工作資料夾記進 dir/record，再執行 body。
func fakeClaude(t *testing.T, body string) (bin, record string) {
	t.Helper()
	dir := t.TempDir()
	bin, record = filepath.Join(dir, "claude"), filepath.Join(dir, "record")
	script := "#!/bin/sh\nprintf '%s\\0' \"$@\" > " + record + ".args\n{ printf 'CFG:%s\\nCWD:%s\\n' \"$CLAUDE_CONFIG_DIR\" \"$PWD\"; printf 'STDIN:'; cat; } > " + record + "\n" + body + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, record
}

func askEnv(t *testing.T, body string) (*engEnv, string) {
	t.Helper()
	v := newEngEnv(t)
	bin, record := fakeClaude(t, body)
	v.e.d.ClaudeBin = bin
	v.e.d.Environ = func() []string { return append(append([]string(nil), hostEnviron...), "PATH="+os.Getenv("PATH")) } // 假 claude 是 shell，要找得到 cat、sleep
	return v, record
}

func askReq(v *engEnv) *sdk.EngineAskRequest {
	return &sdk.EngineAskRequest{
		Agent: "life", Cwd: v.home, Account: "acct-a", EngineConfig: map[string]any{"config_dir": v.cfgDir},
		Model: "haiku", Effort: "low", Instructions: "你叫測試員。\n第二行 \"引號\" 與 $HOME。", Message: "哈囉\n第二行",
	}
}

// TestAskArgvNoSessionNoTools：一次性對話用 -p，不留對話、不給工具；不寫 settings、不帶 --settings／--allowedTools／--resume／--name；
// 訊息從 stdin 給，帳號用 CLAUDE_CONFIG_DIR，在秘書的工作資料夾跑；回覆去頭尾空白。
// Guards: REQ-130
func TestAskArgvNoSessionNoTools(t *testing.T) {
	v, record := askEnv(t, "printf '  收到\\n\\n'")
	req := askReq(v)
	got, err := v.e.Ask(context.Background(), req)
	if err != nil || got.Reply != "收到" || got.Model != "haiku" {
		t.Fatalf("回覆：%+v %v", got, err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	argData, err := os.ReadFile(record + ".args")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(argData), "\x00"), "\x00")
	joined := "\x00" + strings.Join(args, "\x00") + "\x00"
	for _, want := range []string{"-p", "--no-session-persistence", "--disallowedTools\x00*", "--model\x00haiku", "--effort\x00low", "--append-system-prompt\x00" + req.Instructions} {
		if !strings.Contains(joined, "\x00"+want+"\x00") {
			t.Errorf("少了 %q：%q", want, args)
		}
	}
	for _, bad := range []string{"--settings", "--allowedTools", "--resume", "--name", "--session-id", "--permission-mode"} {
		if strings.Contains(joined, "\x00"+bad+"\x00") {
			t.Errorf("一次性對話不能帶 %s：%q", bad, args)
		}
	}
	if !strings.Contains(text, "STDIN:哈囉\n第二行") {
		t.Errorf("stdin 沒收到訊息：%q", text)
	}
	if !strings.Contains(text, "CFG:"+v.cfgDir+"\n") {
		t.Errorf("要用這個帳號的資料夾：%q", text)
	}
	if want, _ := filepath.EvalSymlinks(v.home); !strings.Contains(text, "CWD:"+want+"\n") && !strings.Contains(text, "CWD:"+v.home+"\n") {
		t.Errorf("要在秘書的工作資料夾跑：%q", text)
	}
	if entries, _ := os.ReadDir(v.cfgDir); len(entries) != 0 {
		t.Errorf("不能在帳號資料夾寫任何檔案：%v", entries)
	}
	if calls := v.tmux.callsOf("start"); len(calls) != 0 {
		t.Errorf("不能開 tmux session：%v", calls)
	}
	if len(v.host.events) != 0 {
		t.Errorf("不回報事件")
	}
	// 沒給模型、力度不認得、沒有指令：都不帶。
	if _, err := v.e.Ask(context.Background(), &sdk.EngineAskRequest{EngineConfig: req.EngineConfig, Effort: "extreme", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	argData, _ = os.ReadFile(record + ".args")
	for _, bad := range []string{"--model", "--effort", "--append-system-prompt"} {
		if strings.Contains("\x00"+string(argData), "\x00"+bad+"\x00") {
			t.Errorf("空的設定不該帶 %s：%q", bad, argData)
		}
	}
}

// TestAskTimeoutKills：逾時就殺掉行程（連它開的子行程），回 UNAVAILABLE「回覆逾時」。
// Guards: REQ-130
func TestAskTimeoutKills(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "survived")
	v, _ := askEnv(t, "sleep 30; echo done > "+marker)
	req := askReq(v)
	req.TimeoutSeconds = 1
	start := time.Now()
	_, err := v.e.Ask(context.Background(), req)
	if !isCode(err, sdk.ErrorCodeUnavailable) || !strings.Contains(messageOf(err), "回覆逾時") {
		t.Fatalf("逾時要回 UNAVAILABLE「回覆逾時」：%v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("沒有在逾時後馬上結束：%v", time.Since(start))
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("逾時後行程還在跑")
	}
}

// TestAskNonZeroExitPlainError：結束碼不是 0 回 UNAVAILABLE，帶 stderr 的第一行（路徑拿掉）；沒有 stderr 就說結束碼；空回覆也是錯誤。
// Guards: REQ-130
func TestAskNonZeroExitPlainError(t *testing.T) {
	v, _ := askEnv(t, "echo 'Error: not logged in at /Users/someone/.claude-acme/x.json' >&2; echo second >&2; exit 3")
	_, err := v.e.Ask(context.Background(), askReq(v))
	msg := messageOf(err)
	if !isCode(err, sdk.ErrorCodeUnavailable) || !strings.Contains(msg, "not logged in") || strings.Contains(msg, "/Users") || strings.Contains(msg, "second") {
		t.Fatalf("要帶 stderr 第一行、不帶路徑：%v", err)
	}
	v, _ = askEnv(t, "exit 2")
	if _, err := v.e.Ask(context.Background(), askReq(v)); !isCode(err, sdk.ErrorCodeUnavailable) || !strings.Contains(messageOf(err), "2") {
		t.Fatalf("沒有 stderr 要說結束碼：%v", err)
	}
	v, _ = askEnv(t, "true")
	if _, err := v.e.Ask(context.Background(), askReq(v)); !isCode(err, sdk.ErrorCodeUnavailable) {
		t.Fatalf("空回覆要當錯誤：%v", err)
	}
	// 缺 config_dir、沒有訊息：invalid。
	if _, err := v.e.Ask(context.Background(), &sdk.EngineAskRequest{Message: "x"}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("缺 config_dir：%v", err)
	}
	if _, err := v.e.Ask(context.Background(), &sdk.EngineAskRequest{EngineConfig: map[string]any{"config_dir": v.cfgDir}}); !isCode(err, sdk.ErrorCodeInvalid) {
		t.Fatalf("沒有訊息：%v", err)
	}
}

// TestAskCapability：能力旗標 ask 開著（核心只拿來顯示）。
// Guards: REQ-130
func TestAskCapability(t *testing.T) {
	v := newEngEnv(t)
	caps, err := v.e.Capabilities(context.Background(), &sdk.EngineCapabilitiesRequest{})
	if err != nil || !caps.Features["ask"] {
		t.Fatalf("%+v %v", caps, err)
	}
}
