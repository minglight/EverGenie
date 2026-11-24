package main

import (
	"bytes"
	"strings"
	"testing"
)

// Guards: REQ-77
func TestHookNeverFailsAndServeNeedsHome(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"hook"}, strings.NewReader(""), &out, &errOut); code != 0 || out.Len() != 0 {
		t.Fatalf("hook 沒給事件也要 exit 0 且不印：%d %q", code, out.String())
	}
	t.Setenv("EVERGENIE_HOME", "")
	t.Setenv("EVERGENIE_ENGINE_SOCKET", "")
	if code := run([]string{"serve"}, strings.NewReader(""), &out, &errOut); code != 1 || !strings.Contains(errOut.String(), "EVERGENIE_HOME") {
		t.Fatalf("serve 沒有 EVERGENIE_HOME 要失敗並說明：%d %q", code, errOut.String())
	}
	if code := run([]string{"nope"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Fatalf("不認得的子指令：%d", code)
	}
}

// account-defaults（ENG-19、ECL-64）：印一個 JSON 物件，是新家第一個帳號的 engine_config；沒有 CLAUDE_CONFIG_DIR 時是預設登入。
func TestAccountDefaultsPrintsEngineConfig(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	var out, errOut bytes.Buffer
	if code := run([]string{"account-defaults"}, strings.NewReader(""), &out, &errOut); code != 0 {
		t.Fatalf("結束碼 %d：%s", code, errOut.String())
	}
	if got := strings.TrimSpace(out.String()); got != `{"config_dir":"~/.claude","default_login":true}` {
		t.Fatalf("沒有 CLAUDE_CONFIG_DIR：%s", got)
	}
	out.Reset()
	t.Setenv("CLAUDE_CONFIG_DIR", "/Users/someone/.claude-work")
	if code := run([]string{"account-defaults"}, strings.NewReader(""), &out, &errOut); code != 0 || strings.TrimSpace(out.String()) != `{"config_dir":"/Users/someone/.claude-work"}` {
		t.Fatalf("有 CLAUDE_CONFIG_DIR 就照它：%d %s", code, out.String())
	}
	out.Reset()
	t.Setenv("CLAUDE_CONFIG_DIR", "relative/dir")
	if code := run([]string{"account-defaults"}, strings.NewReader(""), &out, &errOut); code == 0 || out.Len() != 0 {
		t.Fatalf("相對路徑要非 0、不印設定：%d %s", code, out.String())
	}
}
