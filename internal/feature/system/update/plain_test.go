package update

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 每一種沒成功的原因都有白話的下一步，原因與下一步都不夾英文（UPD-14）。
func TestEveryFailureHasPlainNextStep(t *testing.T) {
	reasons := []string{msgNoSource, msgManifest, msgNoAsset, msgFetch, msgCut, msgBroken, msgBadPackage, msgTooBig, msgVersionUnknown, msgDisk, msgNoCurrent, msgBuild, msgLostPrev, msgUntouched}
	for _, r := range reasons {
		if r == "" || english(r) {
			t.Errorf("原因要白話：%q", r)
		}
	}
	for msg, next := range nextOfMsg {
		if next == "" || english(next) || english(msg) {
			t.Errorf("%q 的下一步：%q", msg, next)
		}
	}
	for _, msg := range []string{msgNoSource, msgManifest, msgNoAsset, msgFetch, msgCut, msgBroken, msgBadPackage, msgTooBig, msgVersionUnknown, msgDisk, msgNoCurrent} {
		if _, ok := nextOfMsg[msg]; !ok {
			t.Errorf("下載版的原因 %q 沒有自己的下一步", msg)
		}
	}
	for _, next := range []string{nextRetry, nextNetwork, nextDisk, nextAsk, nextSource, nextWait, nextRecheck, nextNothing, nextDirty, nextBusy, nextSkipped, nextRescue, nextRescueDisk} {
		if next == "" || english(next) {
			t.Errorf("下一步要白話：%q", next)
		}
	}
	// 白話錯誤照原因找下一步；英文原文的錯誤請電腦秘書看，原文只進日誌。
	if got := nextOf(plainf(msgFetch, "dial tcp: timeout")); got != nextNetwork {
		t.Errorf("下載不到：%q", got)
	}
	if got := nextOf(errors.New("exit status 2")); got != nextAsk {
		t.Errorf("英文原文：%q", got)
	}
	u := &Upgrader{Home: testHome(t)}
	if got := u.explain("切不到新版", errors.New("fatal: reference is not a tree")); english(got) || !strings.Contains(got, "升級日誌") {
		t.Errorf("英文原文不給主人看：%q", got)
	}
	if got := u.explain("新版檢查沒過", &ErrUpgrade{Message: "服務沒有在跑"}); got != "新版檢查沒過：服務沒有在跑" {
		t.Errorf("白話的原因照接：%q", got)
	}
	if got := sentence("沒成功。", nextRetry); got != "沒成功。"+nextRetry {
		t.Errorf("句子：%q", got)
	}
}

// 外來的錯誤夾著英文原文（重開服務失敗時 launchctl 的輸出、子行程印的「中文：英文」）：給主人看的只留中文那段，整句進升級日誌。
func TestForeignErrorsKeepOnlyPlainPart(t *testing.T) {
	for in, want := range map[string]string{
		"服務沒有在跑": "服務沒有在跑",
		"載入服務失敗：Bootstrap failed: 5: Input/output error": "載入服務失敗",
		"重開失敗: exit status 3":                            "重開失敗",
		"寫不了 /Users/me/Library/LaunchAgents/x.plist":     "",
		"exit status 2": "",
		"Error：讀不到":     "",
	} {
		if got := plainPart(in); got != want {
			t.Errorf("plainPart(%q) = %q，應該是 %q", in, got, want)
		}
	}
	home := testHome(t)
	u := &Upgrader{Home: home}
	got := u.explain("重開舊版失敗", platform.Unavailable("載入服務失敗：Bootstrap failed: 5: Input/output error"))
	if got != "重開舊版失敗：載入服務失敗" || english(got) {
		t.Errorf("重開失敗的原因：%q", got)
	}
	if got := u.explain("重開舊版失敗", platform.Unavailable("寫不了 /Users/me/Library/LaunchAgents/x.plist")); got != "重開舊版失敗（細節記在升級日誌）" {
		t.Errorf("整句都夾英文就不給看：%q", got)
	}
	if got := u.plainTail("migrate-home --apply", "progress\n整理失敗：open vault/x.md: permission denied"); got != "整理失敗" {
		t.Errorf("新版印的最後一行只留中文那段：%q", got)
	}
	data, _ := os.ReadFile(platform.RuntimeDir(home, "logs", "upgrade.log"))
	for _, raw := range []string{"Bootstrap failed: 5", "LaunchAgents/x.plist", "permission denied"} {
		if !strings.Contains(string(data), raw) {
			t.Errorf("英文原文 %q 要進升級日誌：%s", raw, data)
		}
	}
}

// 升級的通知點下去：沒成功的（state 有 outcome）到看板首頁（橫幅有「再試一次」「退回上一版」）；成功的到系統健康的版本與更新卡。
func TestUpgradeNotificationOpensHomeWhenFailed(t *testing.T) {
	home := testHome(t)
	u := &Upgrader{Home: home}
	st := &UpgradeState{Kind: "upgrade", From: "0.1.0", To: "0.2.0"}
	u.setState(st, UpgradePhaseDone, "升級完成")
	if got := notifyURL(home); got != "/settings/health" {
		t.Errorf("成功：%q", got)
	}
	_ = u.untouched(st, "下載不到新版的檔案", nextNetwork)
	if got := notifyURL(home); got != "/" {
		t.Errorf("沒成功：%q", got)
	}
}
