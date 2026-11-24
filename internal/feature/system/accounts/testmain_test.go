package accounts

import (
	"os"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 測試自己隔離環境：在秘書的 session 或 shell 裡跑（EVERGENIE_AGENT、EVERGENIE_HOME、EVERGENIE_API_SOCKET… 都設著）
// 不能影響結果，也不能碰到真的家。先把所有 EVERGENIE_*（含改名前的舊名）清掉，個別測試要什麼自己 t.Setenv。
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if sdk.IsProductEnv(name) {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}
