package update

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 升級過程放在 <家>/runtime/upgrade/ 的狀態檔（P8 第二輪 S2、REQ-280、REQ-281）。
// 從 internal/base/platform/update.go 搬來（2026-10-31，業務模組 update）；home.toml 的 [update] 設定與家的遷移仍在 platform。

// 升級的 phase（state.json）：依序走，最後是 done、rolled_back 或 failed 其中一個。
const (
	UpgradePhaseFetch      = "fetch"
	UpgradePhaseBackup     = "backup"
	UpgradePhaseBuild      = "build"
	UpgradePhaseMigrate    = "migrate"
	UpgradePhaseRestart    = "restart"
	UpgradePhaseVerify     = "verify"
	UpgradePhaseDone       = "done"
	UpgradePhaseRolledBack = "rolled_back"
	UpgradePhaseFailed     = "failed"
)

// UpgradeRunning 回這個 phase 是不是還在升級中（還沒有結果）。
func UpgradeRunning(phase string) bool {
	switch phase {
	case "", UpgradePhaseDone, UpgradePhaseRolledBack, UpgradePhaseFailed:
		return false
	}
	return true
}

// UpgradeState 是 state.json 的內容（就是 sdk.UpdateState，API 原樣回給看板）。
type UpgradeState = sdk.UpdateState

// UpgradeDir 是升級的狀態資料夾：<家>/runtime/upgrade。
func UpgradeDir(home string) string { return platform.RuntimeDir(home, "upgrade") }

// 檔名（都在 UpgradeDir 底下）。
const (
	UpgradeLockFile     = "lock"
	UpgradeStateFile    = "state.json"
	UpgradeCheckFile    = "check.json"
	UpgradeSkipFile     = "skip.json"
	UpgradeDecisionFile = "decision.json"
	UpgradeHistoryFile  = "history.jsonl"
	UpgradeNotifiedFile = "notified.json"
)

// UpgradeCheck 是 `evergenie upgrade --check` 的結果（也存成 check.json）。Target 空＝沒有新版（Detail 說原因）。
type UpgradeCheck struct {
	Current   string    `json:"current"`
	Target    string    `json:"target"`
	Tag       string    `json:"tag"`
	Notes     string    `json:"notes"`
	Channel   string    `json:"channel"`
	Skipped   []string  `json:"skipped"`
	Detail    string    `json:"detail"`
	CheckedAt time.Time `json:"checked_at"`
}

// UpgradeDecision 是「今晚更新」（decision.json）：到了 at 就自動升到 version。
type UpgradeDecision struct {
	Version  string    `json:"version"`
	Decision string    `json:"decision"`
	At       time.Time `json:"at"`
}

// UpgradeHistory 是 history.jsonl 的一行：一次升級或退回（Kind 是 upgrade 或 rollback）。
type UpgradeHistory struct {
	At         time.Time `json:"at"`
	Kind       string    `json:"kind"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	FromCommit string    `json:"from_commit"`
	ToCommit   string    `json:"to_commit"`
	OK         bool      `json:"ok"`
	// Migrations 是這次升級跑了哪些資料整理（核心的與套件的 id）；Backup 是升級前那份家的備份放在哪。
	// 有跑過整理的升級，退回時資料要一起還原（DEV-154）；更新前的備份只留最新一份（UPD-16），主人確認（Kind=accept）也不刪。
	Migrations []string `json:"migrations,omitempty"`
	Backup     string   `json:"backup,omitempty"`
	// Install 是這次是哪一種安裝換的版：download（下載版，換 current；沒有 commit）；原始碼版不寫（舊紀錄也沒有）。
	Install string `json:"install,omitempty"`
}

// PendingAcceptance 回「升級好了、還沒退回也沒確認過」的那一筆升級紀錄（DEV-154；不用請主人確認，只是還能退回的那次）：最後一筆成功的升級，後面沒有
// 對它的確認（accept）或退回（rollback）。沒有就回 nil。
func PendingAcceptance(home string) *UpgradeHistory {
	hist := ReadUpgradeHistory(home)
	for i := len(hist) - 1; i >= 0; i-- {
		h := hist[i]
		switch {
		case !h.OK:
			continue
		case h.Kind == "accept" || h.Kind == "rollback":
			return nil
		case h.Kind == "upgrade":
			return &hist[i]
		}
	}
	return nil
}

// ReadUpgradeState 讀 state.json；沒有就回 nil。
func ReadUpgradeState(home string) (*sdk.UpdateState, error) {
	var st sdk.UpdateState
	ok, err := platform.ReadJSONFile(filepath.Join(UpgradeDir(home), UpgradeStateFile), &st)
	if err != nil || !ok {
		return nil, err
	}
	return &st, nil
}

// ReadUpgradeCheck 讀 check.json（最後一次檢查的結果）；沒有就回 nil。
func ReadUpgradeCheck(home string) (*UpgradeCheck, error) {
	var c UpgradeCheck
	ok, err := platform.ReadJSONFile(filepath.Join(UpgradeDir(home), UpgradeCheckFile), &c)
	if err != nil || !ok {
		return nil, err
	}
	return &c, nil
}

// ReadUpgradeSkipped 讀跳過清單 skip.json（版本字串，舊的在前）；沒有就是空的。
func ReadUpgradeSkipped(home string) []string {
	var d struct {
		Skipped []string `json:"skipped"`
	}
	_, _ = platform.ReadJSONFile(filepath.Join(UpgradeDir(home), UpgradeSkipFile), &d)
	return d.Skipped
}

// AddUpgradeSkipped 把一個版本放進跳過清單（已經在就不重複）。
func AddUpgradeSkipped(home, version string) error {
	list := ReadUpgradeSkipped(home)
	for _, v := range list {
		if v == version {
			return nil
		}
	}
	return platform.WriteJSONFile(filepath.Join(UpgradeDir(home), UpgradeSkipFile), map[string]any{"skipped": append(list, version)})
}

// ReadUpgradeDecision 讀 decision.json（今晚更新）；沒有就回 nil。
func ReadUpgradeDecision(home string) (*UpgradeDecision, error) {
	var d UpgradeDecision
	ok, err := platform.ReadJSONFile(filepath.Join(UpgradeDir(home), UpgradeDecisionFile), &d)
	if err != nil || !ok {
		return nil, err
	}
	return &d, nil
}

// AppendUpgradeHistory 在 history.jsonl 加一行。
func AppendUpgradeHistory(home string, h UpgradeHistory) error {
	dir := UpgradeDir(home)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return platform.Unavailable("建不了資料夾 " + dir)
	}
	line, err := json.Marshal(h)
	if err != nil {
		return platform.Internal("寫不出紀錄")
	}
	f, err := os.OpenFile(filepath.Join(dir, UpgradeHistoryFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return platform.Unavailable("寫不了升級紀錄")
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

// ReadUpgradeHistory 讀 history.jsonl（舊的在前）；讀不懂的行略過。
func ReadUpgradeHistory(home string) []UpgradeHistory {
	f, err := os.Open(filepath.Join(UpgradeDir(home), UpgradeHistoryFile))
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []UpgradeHistory
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var h UpgradeHistory
		if json.Unmarshal(sc.Bytes(), &h) == nil && h.Kind != "" {
			out = append(out, h)
		}
	}
	return out
}

// UpgradeNotified 回哪些版本已經通知過（notified.json），MarkUpgradeNotified 加一個。
func UpgradeNotified(home string) []string {
	var d struct {
		Versions []string `json:"versions"`
	}
	_, _ = platform.ReadJSONFile(filepath.Join(UpgradeDir(home), UpgradeNotifiedFile), &d)
	return d.Versions
}

func MarkUpgradeNotified(home, version string) error {
	list := UpgradeNotified(home)
	for _, v := range list {
		if v == version {
			return nil
		}
	}
	return platform.WriteJSONFile(filepath.Join(UpgradeDir(home), UpgradeNotifiedFile), map[string]any{"versions": append(list, version)})
}

// ---- 今晚更新 ------------------------------------------------------------------------------------------

// DecideTonight 記下「今晚更新」：tonightAt（HH:MM）在家的時區的下一個那個時間。
func DecideTonight(home *platform.Home, version string, now time.Time) (time.Time, error) {
	loc := home.Location()
	if loc == nil {
		loc = time.Local
	}
	n := now.In(loc)
	t, err := time.ParseInLocation("15:04", home.Update.TonightAt, loc)
	if err != nil {
		return time.Time{}, platform.Invalid("update.tonight_at", "時間請用 HH:MM")
	}
	at := time.Date(n.Year(), n.Month(), n.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	if !at.After(n) {
		at = at.AddDate(0, 0, 1)
	}
	d := UpgradeDecision{Version: version, Decision: "tonight", At: at}
	return at, platform.WriteJSONFile(filepath.Join(UpgradeDir(home.Dir), UpgradeDecisionFile), d)
}

// ClearUpgradeDecision 拿掉「今晚更新」的決定（沒有也不算錯）。
func ClearUpgradeDecision(home string) error {
	err := os.Remove(filepath.Join(UpgradeDir(home), UpgradeDecisionFile))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return platform.Unavailable("清不掉今晚更新的決定")
	}
	return nil
}
