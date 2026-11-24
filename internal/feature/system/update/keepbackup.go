package update

import (
	"os"
	"path/filepath"
	"strings"
)

// 更新前的備份只留最新一份（UPD-16，2026-11-21 作者定）：每次更新（升級、下載版升級、部署）成功之後，
// runtime/backups/ 最上層更早的 upgrade-*.tgz、deploy-*.tgz 都收掉，只留這次的 keep。
// 只能退回最近一次更新，舊的那幾份用不到，整個家一份一份疊起來很佔空間。
// 更新沒成功（換回去了）不叫它：舊的照樣留著。不刪的例外：換不回去的紀錄（rescue.json）還要用的（UPD-14）；
// `--rollback` 可能還用得到的那份（最近一次整理過資料、還沒確認或退回的升級；能不能退由 Rollback 自己判斷，
// 例：整理過資料之後又部署一次沒整理資料的，之後只把程式換回去，還是能連資料退回）。所以最多兩份。
func keepNewestUpdateBackup(home, keep string) {
	if keep == "" {
		return
	}
	dir := filepath.Dir(keep)
	protect := map[string]bool{filepath.Clean(keep): true}
	for _, p := range RescueBackups(home) {
		protect[p] = true
	}
	if p := PendingAcceptance(home); p != nil && p.Backup != "" && len(p.Migrations) > 0 {
		protect[filepath.Clean(p.Backup)] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !e.Type().IsRegular() || !strings.HasSuffix(name, ".tgz") || !(strings.HasPrefix(name, "upgrade-") || strings.HasPrefix(name, "deploy-")) {
			continue
		}
		path := filepath.Join(dir, name)
		if protect[path] {
			continue
		}
		_ = os.Remove(path) // 刪不掉：下次更新成功再收
	}
}
