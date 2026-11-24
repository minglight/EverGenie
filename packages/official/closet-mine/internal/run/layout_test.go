package run

import (
	"os"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestMain 先清掉家的佈局變數：測試可能在秘書的 session 裡跑（核心會帶 EVERGENIE_STATE 等），其他測試都假設第 1 版的位置。
func TestMain(m *testing.M) {
	for _, k := range sdk.EnvPrefixes(sdk.EnvState, sdk.EnvMedia, sdk.EnvOfficialPersonas, sdk.EnvBundledPersonas) {
		os.Unsetenv(k)
	}
	os.Exit(m.Run())
}
