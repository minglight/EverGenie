package platform

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// 離線改角色代號（evergenie home tidy、evergenie personas promote，DEV-154，PRS-22～24）的紀錄檔位置放在這裡，
// 讓 home relayout 也看得到：還沒收尾的改代號紀錄，退回時要家跟做完那時一樣；這中間搬家或退回佈局會讓它退不回去
// （例：tidy 搬去狀態資料夾的官方卡與圖，退回第 1 版的家看不到；搬家會改寫文字檔裡的路徑、home.toml 也會改），
// 所以 home relayout 搬家與退回前都要先把改代號退回或收尾。

// TidyJournalPath 是 home tidy 的紀錄檔。
func TidyJournalPath(home string) string { return filepath.Join(home, "settings", "home-tidy.json") }

// PromoteJournalDir 是 personas promote 放紀錄檔（<my_名字>.json）的資料夾。
func PromoteJournalDir(home string) string {
	return filepath.Join(home, "settings", "persona-promote")
}

// PendingRecodeJournals 回還沒收尾的改代號紀錄檔（家底下的相對路徑）。
func PendingRecodeJournals(home string) []string {
	var out []string
	if info, err := os.Lstat(TidyJournalPath(home)); err == nil && info.Mode().IsRegular() {
		out = append(out, "settings/home-tidy.json")
	}
	entries, _ := os.ReadDir(PromoteJournalDir(home))
	for _, e := range entries {
		if n := e.Name(); e.Type().IsRegular() && strings.HasSuffix(n, ".json") {
			out = append(out, "settings/persona-promote/"+n)
		}
	}
	sort.Strings(out)
	return out
}

// pendingRecodeProblem 是 home relayout 搬家與退回前的檢查：有還沒收尾的改代號紀錄就回白話原因。
func pendingRecodeProblem(home string) string {
	list := PendingRecodeJournals(home)
	if len(list) == 0 {
		return ""
	}
	return "還有改角色代號的紀錄沒收尾（" + strings.Join(list, "、") + "）：先退回（evergenie home tidy --revert --apply、evergenie personas promote <my_名字> --revert --apply），" +
		"或確定不退了先收尾（同一個指令加 --forget --apply），再搬家或退回佈局"
}

// BackupName 是 <檔>.bak-<YYYYMMDD>-<原因>；同一天已有就加 -HHMM，再撞就加序號（備份規則）。
func BackupName(path string, now time.Time, reason string) string {
	return backupName(path, now, reason)
}
