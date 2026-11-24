package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// 帳號的設定資料夾與 Claude 自己的預設登入（ECL-64，DEV-155 S5）。
//
// Claude Code 沒設 CLAUDE_CONFIG_DIR 時用 ~/.claude（全域設定在 ~/.claude.json），登入資訊記在鑰匙圈的
// 「Claude Code-credentials」；一旦設了 CLAUDE_CONFIG_DIR（就算指的也是 ~/.claude），鑰匙圈的名字會多一段
// 從資料夾算出來的尾碼、全域設定改讀 <資料夾>/.claude.json——也就是另一個帳號，原本的登入看不到。
// 所以「用主人現在登入的那一個」要分兩種：他是帶著 CLAUDE_CONFIG_DIR 登入的，就照樣帶同一個值；他沒帶，
// 開 claude 時也不能帶（engine_config.default_login = true）。

// claudeDefaultDir 是 Claude Code 沒設 CLAUDE_CONFIG_DIR 時用的資料夾（~/.claude）；找不到家目錄回空字串。
func claudeDefaultDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".claude")
}

// usesDefaultLogin：帳號寫了 default_login = true，而且設定資料夾就是 Claude 的預設資料夾——開 claude 時不設 CLAUDE_CONFIG_DIR。
// 設定資料夾改成別的（例如在看板改了帳號資料夾）就照一般帳號，default_login 不算數。
func (c engineConfig) usesDefaultLogin() bool {
	def := claudeDefaultDir()
	return c.DefaultLogin && def != "" && filepath.Clean(c.ConfigDir) == def
}

// withAccountEnv 把環境裡原本的 CLAUDE_CONFIG_DIR 拿掉，再照這個帳號設（預設登入就不設）。
func withAccountEnv(environ []string, c engineConfig) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if strings.HasPrefix(kv, "CLAUDE_CONFIG_DIR=") {
			continue
		}
		out = append(out, kv)
	}
	if !c.usesDefaultLogin() {
		out = append(out, "CLAUDE_CONFIG_DIR="+c.ConfigDir)
	}
	return out
}

// sessionConfigDir 是 hook 行程看到的帳號資料夾：環境有 CLAUDE_CONFIG_DIR 就是它；沒有（預設登入）就是 Claude 的預設 <HOME>/.claude。
func sessionConfigDir(getenv func(string) string) string {
	if d := getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return d
	}
	if home := getenv("HOME"); home != "" {
		return filepath.Join(home, ".claude")
	}
	return ""
}

// AccountDefaults 是新家第一個帳號的 engine_config（`evergenie-engine-claude account-defaults`，ENG-19）：
// 用現在這個環境登入的那一個帳號。有 CLAUDE_CONFIG_DIR 就照它，字串原樣（只展開開頭的 ~；尾端的斜線也不動，
// 鑰匙圈的名字可能是照原字串算的）；沒有就是 Claude 的預設 ~/.claude，並標 default_login，開 claude 時才不會帶
// CLAUDE_CONFIG_DIR（見檔頭）。CLAUDE_CONFIG_DIR 是相對路徑：猜不出主人登入的是哪一個，回錯（init 會請主人到看板填）。
func AccountDefaults(getenv func(string) string) (map[string]any, error) {
	if d := getenv("CLAUDE_CONFIG_DIR"); strings.TrimSpace(d) != "" {
		// 只換開頭的 ~，後面一個字都不動（expandHome 用 filepath.Join，會拿掉尾端的斜線）
		if d == "~" || strings.HasPrefix(d, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				d = home + d[1:]
			}
		}
		if !filepath.IsAbs(d) {
			return nil, fmt.Errorf("CLAUDE_CONFIG_DIR 不是絕對路徑（%s），猜不出登入的是哪個帳號", d)
		}
		return map[string]any{"config_dir": d}, nil
	}
	return map[string]any{"config_dir": "~/.claude", "default_login": true}, nil
}
