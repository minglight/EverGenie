package claude

import (
	"path/filepath"
	"testing"
)

// TestConfigDirExpandsTilde：帳號的 config_dir 寫 ~/.claude 要變成使用者家目錄下的資料夾，不是 cwd 底下叫「~」的資料夾（DEV-4）。
// Guards: REQ-254
func TestConfigDirExpandsTilde(t *testing.T) {
	user := t.TempDir()
	t.Setenv("HOME", user)
	c, err := parseEngineConfig(map[string]any{"config_dir": " ~/.claude-acme "})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(user, ".claude-acme"); c.ConfigDir != want {
		t.Fatalf("config_dir = %q，應該是 %q", c.ConfigDir, want)
	}
	if got := cardsDir(map[string]any{"config_dir": "~/.claude"}); got != filepath.Join(user, ".claude", "agents") {
		t.Fatalf("cardsDir = %q", got)
	}
	if c, err := parseEngineConfig(map[string]any{"config_dir": "/abs/acct"}); err != nil || c.ConfigDir != "/abs/acct" {
		t.Fatalf("絕對路徑照舊：%q %v", c.ConfigDir, err)
	}
	if _, err := parseEngineConfig(map[string]any{"config_dir": "  "}); err == nil {
		t.Fatal("空白的 config_dir 要報錯")
	}
}
