package settings

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/update"
)

// 備份的整理（Q4，2026-11-01 從 platform 搬來，只有 settings 用；`evergenie backup list|prune`）：各個業務模組改設定前都會把舊檔備份到 runtime/backups/<組>/<時間>.<副檔名>，
// 遷移與升級的整包備份放在 runtime/backups/ 最上層（legacy-<時間>.tar.gz）。這裡只列出與依保留規則刪除，不產生備份。

// TopLevelGroup 是直接放在 runtime/backups/ 最上層、沒有資料夾可以分組的備份所屬的組。
const TopLevelGroup = "（最上層）"

// 預設的保留規則：每一組至少留最新 10 個，30 天內的都留。
const (
	DefaultBackupKeep = 10
	DefaultBackupDays = 30
)

// Backup 是 runtime/backups/ 底下的一個備份（檔案，或組資料夾底下的一個子資料夾）。
type Backup struct {
	Path    string // 相對家（第 2 版相對狀態資料夾；兩版都是 runtime/backups/…）
	Group   string
	Size    int64
	ModTime time.Time
}

var stampSuffix = regexp.MustCompile(`^(.*?)-\d`)

func backupGroup(rel string) string {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) > 1 {
		return parts[0]
	}
	if m := stampSuffix.FindStringSubmatch(parts[0]); m != nil && m[1] != "" {
		return m[1]
	}
	return TopLevelGroup
}

func entrySize(path string, info fs.FileInfo) int64 {
	if !info.IsDir() {
		return info.Size()
	}
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, e := d.Info(); e == nil {
				total += i.Size()
			}
		}
		return nil
	})
	return total
}

// ListBackups 列出 runtime/backups/ 底下所有備份（新的在前）；資料夾不存在是空的。只讀。
func ListBackups(home string) ([]Backup, error) {
	root, base := backupsRoot(home)
	top, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, platform.Unavailable("讀不到備份資料夾")
	}
	var out []Backup
	add := func(path string) {
		info, err := os.Lstat(path)
		if err != nil {
			return
		}
		rel, _ := filepath.Rel(base, path)
		out = append(out, Backup{Path: rel, Group: backupGroup(strings.TrimPrefix(rel, filepath.Join("runtime", "backups")+string(filepath.Separator))), Size: entrySize(path, info), ModTime: info.ModTime()})
	}
	for _, en := range top {
		path := filepath.Join(root, en.Name())
		// 資料夾是「組」，備份是它底下的東西；最上層的檔案自己就是備份。
		if en.IsDir() {
			kids, err := os.ReadDir(path)
			if err != nil {
				continue
			}
			for _, k := range kids {
				add(filepath.Join(path, k.Name()))
			}
			continue
		}
		add(path)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// backupsRoot 回傳備份資料夾與 Backup.Path 的基準（runtime/ 的上一層：第 1 版是家，第 2 版是狀態資料夾）。
func backupsRoot(home string) (dir, base string) {
	dir = platform.RuntimeDir(home, "backups")
	return dir, filepath.Dir(filepath.Dir(dir))
}

// PlanPrune 依保留規則標出該刪的：每一組（新的在前）前 keep 個一定留，其餘只留 days 天內的。keep、days 是 0 用預設。
func PlanPrune(list []Backup, keep, days int, now time.Time) map[string]bool {
	if keep <= 0 {
		keep = DefaultBackupKeep
	}
	if days <= 0 {
		days = DefaultBackupDays
	}
	cutoff := now.Add(-time.Duration(days) * 24 * time.Hour)
	seen := map[string]int{}
	out := map[string]bool{}
	for _, b := range list { // list 已經新的在前
		seen[b.Group]++
		if seen[b.Group] > keep && b.ModTime.Before(cutoff) {
			out[b.Path] = true
		}
	}
	return out
}

// keepRescueBackups 把「換不回去的紀錄」（update 的 rescue.json）還要用的備份從要刪的清單拿掉：升級前的備份、換回前留的那份
// 不管多舊都留著，等換回成功（紀錄刪掉）之後才照一般的規則整理（system 卡 UPD-14）。
func keepRescueBackups(home string, plan map[string]bool) {
	_, base := backupsRoot(home)
	for _, p := range update.RescueBackups(home) {
		if rel, err := filepath.Rel(base, p); err == nil {
			delete(plan, rel)
		}
	}
}

// DeleteBackups 真的刪掉這些備份。路徑一定要在 runtime/backups/ 底下（不接受 .. 或別處的路徑），刪不掉的回錯誤、前面已刪的不還原。
func DeleteBackups(home string, paths []string) error {
	dir, base := backupsRoot(home)
	root := dir + string(filepath.Separator)
	for _, rel := range paths {
		abs := filepath.Join(base, filepath.Clean(rel))
		if !strings.HasPrefix(abs, root) {
			return platform.Forbidden("只能刪 runtime/backups/ 底下的備份")
		}
		if err := os.RemoveAll(abs); err != nil {
			return platform.Unavailable("刪不掉備份 " + rel)
		}
	}
	return nil
}

// AutoPrune 照預設的保留規則（每組最新 DefaultBackupKeep 個一定留、其餘只留 DefaultBackupDays 天內；換不回去的紀錄還要用的不管多舊都留）
// 真的刪掉 runtime/backups/ 的舊備份，回刪掉的那些。給每週的備份掃描用（system/backupsweep，BSW-5）；跟 `backup prune --apply` 同一套規則。
// 只動 runtime/backups/：整個家的備份（homebackup，HBK-3）在別的資料夾、有自己的規則，這裡不碰。
func AutoPrune(home string, now time.Time) ([]Backup, error) {
	list, err := ListBackups(home)
	if err != nil {
		return nil, err
	}
	plan := PlanPrune(list, 0, 0, now)
	keepRescueBackups(home, plan)
	var doomed []Backup
	var paths []string
	for _, b := range list {
		if plan[b.Path] {
			doomed = append(doomed, b)
			paths = append(paths, b.Path)
		}
	}
	if len(paths) == 0 {
		return nil, nil
	}
	if err := DeleteBackups(home, paths); err != nil {
		return nil, err
	}
	return doomed, nil
}
