package agent

import (
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 共用規則有「出事時的急救」（DEV-153，AGT-44）：每位秘書都讀得到，家裡沒有電腦秘書也有人會處理。
// 守著五種狀況各有一條、用的指令都在、修復入口的名字跟程式一致（platform.RescueName）、指到的說明文件是隨包的那一份。
func TestSharedRulesHaveFirstAid(t *testing.T) {
	for _, want := range []string{
		"出事時的急救", "這個家不一定有電腦秘書",
		"看板有紅字", "某位秘書不回話", "額度用完：這是主人那個 AI 訂閱的用量上限，不是壞掉", "登入掉了", "升級後怪怪的",
		"`evergenie doctor`", "`evergenie status`", "`evergenie sessions restart <職位>`", "`evergenie quota`",
		"`evergenie upgrade --report`", "`evergenie upgrade --rollback`", "`evergenie report`",
		"`evergenie docs show guides/billing-and-accounts.md`",
		"「" + platform.RescueName + "」",
	} {
		if !strings.Contains(sharedRules, want) {
			t.Errorf("共用規則的急救段少了 %q", want)
		}
	}
}
