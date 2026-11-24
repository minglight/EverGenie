package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 測試自己隔離環境：在秘書的 session 或 shell 裡跑（EVERGENIE_AGENT、EVERGENIE_HOME、EVERGENIE_API_SOCKET… 都設著）
// 不能影響結果，也不能碰到真的家。先把所有 EVERGENIE_*（含改名前的舊名）清掉（EVERGENIE_IT_* 是手動跑真實 Claude 整合測試時
// 明講的輸入，留著），個別測試要什麼自己 t.Setenv。
func TestMain(m *testing.M) {
	scrubHGEnv()
	// 測試不碰真的 ~/Library/LaunchAgents（doctor 會看 serve 的服務有沒有裝）：指到一個不存在的資料夾。
	os.Setenv("EVERGENIE_LAUNCH_AGENTS_DIR", filepath.Join(os.TempDir(), "hg-test-launch-agents-none"))
	os.Exit(m.Run())
}

func scrubHGEnv() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sdk.IsProductEnv(name) && !strings.HasPrefix(sdk.CurrentEnvName(name), "EVERGENIE_IT_") {
			os.Unsetenv(name)
		}
	}
}
