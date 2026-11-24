package homebackup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 保留規則（HBK-3）：手動做的、還原前自動留的最新 KeepCount 份一定留，其餘超過 KeepDays 天的刪。
// 以前每天自動的（daily/）已經不做了（2026-11-21），留下來的只留最新一份（完整的家備份，更新前那份不收大檔）。
// 備份只放在這台電腦（法律第 7 條：備份由主人自己做，EverGenie 不做跨台備份；要另外存一份，主人自己下載）。
// 以前試做過的「第二個位置」（settings/backup.toml 的 copy_dir）已拿掉：那個檔還在的話沒有人讀，不報錯。
const (
	KeepCount = 7
	KeepDays  = 30
)

// keepRule 回某一種備份留幾份、其餘留幾天（days 是 0＝其餘都不留）。
func keepRule(kind string) (keep, days int) {
	if kind == sdk.HomeBackupKindDaily {
		return 1, 0
	}
	return KeepCount, KeepDays
}

// List 列出這台電腦上的備份（新的在前）。
func List(home string) []sdk.HomeBackupFile {
	var out []sdk.HomeBackupFile
	for _, kind := range platform.HomeBackupKinds {
		for _, f := range listDir(platform.HomeBackupDir(home, kind)) {
			f.Kind = kind
			f.Download = "/home-backup/" + kind + "/" + f.Name
			out = append(out, f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// listDir 列一個資料夾裡的備份檔（新的在前；不存在＝空的）。時間用檔案的修改時間（做好時設成做的時間）。
func listDir(dir string) []sdk.HomeBackupFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []sdk.HomeBackupFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !platform.IsHomeBackupName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, sdk.HomeBackupFile{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime().UTC()})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// PlanPrune 算出該刪哪些：新的在前排好的清單裡，前 keep 份一定留；其餘超過 days 天的刪（days 是 0＝其餘全刪）。
func PlanPrune(list []sdk.HomeBackupFile, keep, days int, now time.Time) []sdk.HomeBackupFile {
	cutoff := now.AddDate(0, 0, -days)
	var out []sdk.HomeBackupFile
	for i, f := range list {
		if i >= keep && (days <= 0 || f.CreatedAt.Before(cutoff)) {
			out = append(out, f)
		}
	}
	return out
}

// Prune 照保留規則清這台電腦上的備份，也收掉做到一半留下的 .part；回刪了幾個。
func Prune(home string, now time.Time) int {
	n := 0
	for _, kind := range platform.HomeBackupKinds {
		dir := platform.HomeBackupDir(home, kind)
		keep, days := keepRule(kind)
		for _, f := range PlanPrune(listDir(dir), keep, days, now) {
			if os.Remove(filepath.Join(dir, f.Name)) == nil {
				n++
			}
		}
		cleanParts(dir, now)
	}
	return n
}

// cleanParts 收掉做到一半的暫存檔（.<檔名>.part）：超過一天的才收。
func cleanParts(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".part") || !platform.HasHomeBackupExt(strings.TrimSuffix(name, ".part")) {
			continue
		}
		if info, err := e.Info(); err == nil && now.Sub(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(dir, name)) // 收不掉下次再收
		}
	}
}

// cleanUploads 收掉看板上傳後超過一天沒用的備份檔。
func cleanUploads(home string, now time.Time) {
	dir := platform.HomeBackupUploadDir(home)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if now.Sub(info.ModTime()) > 24*time.Hour {
			_ = os.Remove(filepath.Join(dir, e.Name())) // 刪不掉：下一輪再試
		}
	}
}
