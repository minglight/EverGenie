package homebackup

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 做備份、準備還原之前先看空間（HBK-10）：估這次要寫多少，目的地剩的不到「估計＋餘裕」就不做，白話說差多少。
// 估計偏大（照沒壓過的大小算）：圖與影片本來就壓過，實際差不多；寧可少做一份，不要把硬碟寫滿。

// spaceMargin 是估計之外要多留的空間：估計的二十分之一再加 64 MB（硬碟不寫到剛好滿）。
const spaceMargin int64 = 64 << 20

func withMargin(est int64) int64 { return est + est/20 + spaceMargin }

// freeBytes 回 dir 所在的磁碟還能寫多少位元組（dir 還沒建就往上找存在的那一層）；ok=false＝問不到，這時不擋。測試會換掉。
var freeBytes = func(dir string) (free int64, ok bool) {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(d, &st); err == nil {
			return int64(st.Bavail) * int64(st.Bsize), true //nolint:unconvert // 兩個欄位的型別各平台不同
		}
		if d == filepath.Dir(d) {
			return 0, false
		}
	}
}

// enoughSpace：dir 那顆磁碟放得下 est（加餘裕）嗎。放不下回還剩多少。
func enoughSpace(dir string, est int64) (free int64, ok bool) {
	free, known := freeBytes(dir)
	if !known {
		return 0, true
	}
	return free, free >= withMargin(est)
}

// sameVolume：兩個位置在不在同一顆磁碟（還沒建的往上找存在的那一層；問不到當成同一顆，空間一起算比較保守）。
func sameVolume(a, b string) bool {
	da, oka := deviceOf(a)
	db, okb := deviceOf(b)
	return !oka || !okb || da == db
}

func deviceOf(path string) (uint64, bool) {
	for d := filepath.Clean(path); ; d = filepath.Dir(d) {
		if info, err := os.Stat(d); err == nil {
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return 0, false
			}
			return uint64(st.Dev), true //nolint:unconvert // 型別各平台不同
		}
		if d == filepath.Dir(d) {
			return 0, false
		}
	}
}

// estimate 估一份備份多大：要收的每個一般檔的大小加起來（沒壓過的大小；讀不到的略過）。
func estimate(list []source) int64 {
	var n int64
	for _, s := range list {
		info, err := os.Lstat(s.path)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if info.Mode().IsRegular() {
				n += info.Size()
			}
			continue
		}
		_ = filepath.WalkDir(s.path, func(_ string, d fs.DirEntry, err error) error { // 估計用：走不完的部分不算
			if err != nil || d.IsDir() || !d.Type().IsRegular() {
				return nil // 估計：讀不到的略過
			}
			if fi, err := d.Info(); err == nil {
				n += fi.Size()
			}
			return nil
		})
	}
	return n
}

// estimateHome 估這個家現在做一份備份多大。
func estimateHome(home string) int64 {
	list, _ := sources(home, layoutOf(home))
	return estimate(list)
}

// layoutOf：這個家的佈局（設定讀得懂用設定的，讀不懂照資料夾猜）。
func layoutOf(home string) platform.Layout {
	if h, err := platform.LoadHome(home); err == nil {
		return h.Paths()
	}
	return platform.LayoutFor(home)
}

// LostLinks 列出「還原之後會不見」的捷徑（符號連結等不是一般檔的東西），寫成家裡的相對路徑（HBK-11）：
// 還原是一塊一塊整個換掉，備份又不收捷徑，所以要換掉的那幾塊裡現有的捷徑換完就沒有了，要主人自己重建。
// 家資料夾最上層的捷徑不在要換的範圍裡，不會不見，不列。
func LostLinks(home string) []string {
	home = filepath.Clean(home)
	list, _ := sources(home, layoutOf(home))
	var out []string
	for _, s := range list {
		if s.file {
			continue
		}
		info, err := os.Lstat(s.path)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(s.path, func(path string, d fs.DirEntry, err error) error { // 列得出來的就列
			if err != nil || d.IsDir() || d.Type().IsRegular() {
				return nil // 讀不到的略過
			}
			if rel, rerr := filepath.Rel(home, path); rerr == nil && filepath.IsLocal(rel) {
				out = append(out, filepath.ToSlash(rel))
			} else {
				out = append(out, path)
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}
