package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// loaded 是掃描一個套件資料夾的結果。manifest 為 nil 表示讀不起來（err 是原因，這個套件 rejected）。
type loaded struct {
	id       string
	tier     sdk.Tier
	dir      string
	manifest *Manifest
	err      error
}

// tierOrder 是同一個 id 出現在多層時的優先順序：mine 最優先（fork 就是 mine 蓋掉同 id 的官方套件，D-3）。
var tierOrder = map[sdk.Tier]int{sdk.TierMine: 0, sdk.TierInstalled: 1, sdk.TierOfficial: 2}

// skipPackageDir：隱藏資料夾與備份（名字含 .bak-，備份規則 <原名>.bak-<日期>-<原因>）不是套件，不載入也不看改動。
func skipPackageDir(name string) bool {
	return strings.HasPrefix(name, ".") || strings.Contains(name, ".bak-")
}

// notAPackage：主人的套件資料夾（mine）與別人的套件資料夾（installed）裡，沒有 package.toml 的子資料夾不是套件，
// 是主人自己放的東西（筆記、解開的壓縮檔、工具腳本）：不載入、不列、不算壞掉（DEV-154：不認得的不掃、不碰、不擋升級）。
// 官方套件資料夾是程式的一部分，少了 package.toml 就是壞掉，照舊報。
func notAPackage(dir string, tier sdk.Tier) bool {
	if tier == sdk.TierOfficial {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "package.toml"))
	return os.IsNotExist(err)
}

// scanRoot 掃描 root 底下每個子資料夾（略過隱藏資料夾、備份、一般檔案，以及 mine／installed 裡沒有 package.toml 的資料夾）；
// root 不存在就是空的。
func scanRoot(root string, tier sdk.Tier) []*loaded {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []*loaded
	for _, e := range entries {
		if !e.IsDir() || skipPackageDir(e.Name()) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if notAPackage(dir, tier) {
			continue
		}
		l := &loaded{id: e.Name(), tier: tier, dir: dir}
		l.manifest, l.err = ParseManifest(dir, tier)
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out
}

// resolveTiers 把三層掃描結果合成「每個 id 一個有效的」＋「被蓋掉的」。
// - mine 蓋過同 id 的 installed、official（那就是 fork）；
// - installed 不能蓋掉 official：分享的套件跟官方同 id 時，分享的那個 rejected。
func resolveTiers(all []*loaded) (active, shadowed map[string]*loaded, conflicts []*loaded) {
	active, shadowed = map[string]*loaded{}, map[string]*loaded{}
	sort.SliceStable(all, func(i, j int) bool { return tierOrder[all[i].tier] < tierOrder[all[j].tier] })
	for _, l := range all {
		cur, dup := active[l.id]
		if !dup {
			active[l.id] = l
			continue
		}
		switch {
		case cur.tier == sdk.TierMine:
			shadowed[l.id] = l
		case cur.tier == sdk.TierInstalled && l.tier == sdk.TierOfficial:
			// 官方的優先，分享的那個被擋下。
			cur.err = fmt.Errorf("官方套件已經有同名的「%s」，分享的套件不能蓋掉它", l.id)
			cur.manifest = nil
			active[l.id] = l
			conflicts = append(conflicts, cur)
		default:
			shadowed[l.id] = l
		}
	}
	return active, shadowed, conflicts
}
