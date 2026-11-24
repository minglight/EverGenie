package update

import (
	"errors"
	"strings"
	"unicode"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 升級沒成功時給主人看的話（DEV-155 S6，system 卡 UPD-14）：原始碼版與下載版同一套。
// 每一種失敗都是「一句白話原因（state.json 的 reason）＋一個下一步（next）」，不夾英文；英文細節（錯誤原文、網址、
// 新版印出來的東西）只寫進升級日誌 runtime/logs/upgrade.log（detail）。結果分三種（outcome）：
//   - untouched：換版之前就停了，舊版照常在跑（下載、備份、版本清單…沒過；下載版試跑資料整理沒過也是）。
//   - rolled_back：換過了、已換回舊版（資料整理過就用備份還原，restored）。
//   - stranded：也沒換回，要處理：記下 rescue.json，看板首頁的「退回上一版」（update_rollback → upgrade --rollback）再試一次換回。

// 下一步（一句；給主人看）。
const (
	nextRetry   = "可以再更新一次；一直不行再請秘書看"
	nextNetwork = "等網路正常了再更新一次"
	nextDisk    = "清出一些硬碟空間後再更新一次"
	nextAsk     = "請秘書幫忙看（細節記在升級日誌）"
	nextSource  = "請秘書幫忙設定從哪裡下載新版"
	nextWait    = "這一版先不用，等下一版出來再更新"
	nextRecheck = "重新檢查一次更新，再按「現在更新」"
	nextNothing = "現在不用做什麼"
	nextDirty   = "先把程式資料夾裡沒提交的改動提交或收起來，再試一次"
	nextBusy    = "等現在這一個做完再試"
	nextSkipped = "這一版先跳過，已經請秘書查原因；你不用做什麼，舊版照常可以用"
	nextRescue  = "到看板首頁按「退回上一版」再試一次換回；還是不行就請秘書處理"
	// 「退回上一版」時留不下換回前的資料備份（硬碟滿了之類）：什麼都沒動。
	nextRescueDisk = "清出一些硬碟空間後再按一次「退回上一版」；還是不行就請秘書處理"
	// 換不回去的紀錄（rescue.json）指著的東西不能用（備份不見了、讀不開，或紀錄被改過）：什麼都沒動，要人處理。
	nextRescueBroken = "請秘書幫忙處理（細節記在升級日誌）；在那之前不要刪升級前的備份"
	// --to 指了比現在舊的版本：升級不換成舊版，要回去走「退回上一版」。
	nextDowngrade = "要回到上一版，請跟秘書說「退回上一版」（或在看板首頁按「退回上一版」）"
	// 有一次沒換回的升級還沒處理（rescue.json）：先處理它，才能確認升級（確認會收掉舊版的程式）。
	nextRescueFirst = "先到看板首頁按「退回上一版」把沒換回的那次處理好；還是不行就請秘書處理"
)

// nextOfMsg：下載版的白話原因（msg*）各自的下一步；沒列到的用 nextRetry。
var nextOfMsg = map[string]string{
	msgNoSource:       nextSource,
	msgManifest:       nextNetwork,
	msgNoAsset:        nextWait,
	msgFetch:          nextNetwork,
	msgCut:            nextNetwork,
	msgBroken:         nextRetry,
	msgBadPackage:     nextWait,
	msgTooBig:         nextWait,
	msgVersionUnknown: nextAsk,
	msgDisk:           nextDisk,
	msgNoCurrent:      nextAsk,
}

// 原始碼版才有的白話原因（英文細節進升級日誌）。
const (
	msgBuild    = "編譯新版失敗"
	msgLostPrev = "舊版的執行檔不見了"
)

// nextOf 回錯誤的下一步：白話錯誤（plainErr）照它的原因找；其他的請秘書看。
func nextOf(err error) string {
	var pe *plainErr
	if errors.As(err, &pe) {
		if n := nextOfMsg[pe.plain]; n != "" {
			return n
		}
		return nextRetry
	}
	return nextAsk
}

// sentence 把原因與下一步接成一句（中間一個句號）。
func sentence(reason, next string) string {
	reason = strings.TrimRight(strings.TrimSpace(reason), "。")
	if next == "" {
		return reason
	}
	return reason + "。" + next
}

// hasLatin：有英文字母（給主人的話不夾英文）。
func hasLatin(s string) bool {
	for _, r := range s {
		if r < 0x80 && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// plainPart 是外來的錯誤（不是這裡寫的白話）裡可以給主人看的那段：外來的錯誤就算有中文，也常是「中文前綴：英文原文」
// （例如重開服務失敗時 launchctl 的輸出「載入服務失敗：Bootstrap failed: 5: Input/output error」）。
// 沒有英文字母就整句照用；有的話只留第一個冒號前面那段（那段要有中文、不夾英文），不然回空字串。整句由呼叫的人寫進升級日誌。
func plainPart(s string) string {
	s = strings.TrimSpace(s)
	if !hasHan(s) {
		return ""
	}
	if !hasLatin(s) {
		return s
	}
	if i := strings.IndexAny(s, "：:"); i > 0 {
		if head := strings.TrimSpace(s[:i]); hasHan(head) && !hasLatin(head) {
			return head
		}
	}
	return ""
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

// logPath 是升級日誌（英文細節在這裡）。
func (u *Upgrader) logPath() string { return platform.RuntimeDir(u.Home, "logs", "upgrade.log") }

// logNote 是通知最後那一句：細節寫在哪裡（給秘書查）。
func (u *Upgrader) logNote() string { return "（細節記在升級日誌：" + u.logPath() + "）" }

// end 記下沒成功的結果（state.json：phase、outcome、原因 reason、下一步、日誌位置），回帶結束碼的錯誤；錯誤訊息是 msg（給命令列看的
// 整句，含結果）加下一步。不發通知（要不要發由呼叫的人決定：換回、換不回去才發；換版之前就停的，看板首頁有橫幅）。
func (u *Upgrader) end(st *UpgradeState, code int, phase, outcome, reason, msg, next string) error {
	st.Outcome, st.Next, st.Log = outcome, next, u.logPath()
	u.setState(st, phase, reason)
	return upgradeFail(code, "%s", sentence(msg, next))
}

// untouched 是「換版之前就停了」：舊版照常在跑，結束碼 1。
func (u *Upgrader) untouched(st *UpgradeState, reason, next string) error {
	return u.end(st, UpgradeExitFailed, UpgradePhaseFailed, sdk.UpdateOutcomeUntouched, reason, reason, next)
}

// failMsg 是沒有 state 可寫（還沒開始）時的錯誤：原因加下一步。
func failMsg(code int, reason, next string) error {
	return upgradeFail(code, "%s", sentence(reason, next))
}
