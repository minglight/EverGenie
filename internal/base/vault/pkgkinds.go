package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// 套件的資料種類（D-28）：套件在 manifest 宣告、核心把它註冊成 vault 的種類 `<套件 id>.<種類>`，
// 檔案放在 vault/pkg/<套件 id>/<種類>/<id>.md。這份檔案管註冊、索引與編號計數；寫入、驗證照共通的流程。

// PackageKind 是套件資料種類的完整名稱（`<套件 id>.<種類>`）。
func PackageKind(pkgID, kind string) string { return pkgID + "." + kind }

// PackagePath 是套件資料的相對路徑（相對於家）。
func PackagePath(pkgID, kind, id string) string {
	return "vault/pkg/" + pkgID + "/" + kind + "/" + id + ".md"
}

// inPackageTree：這個相對於 vault/ 的路徑是不是在 pkg/ 底下。
func (m *Module) inPackageTree(rel string) bool {
	rel = filepath.ToSlash(rel)
	return rel == "pkg" || strings.HasPrefix(rel, "pkg/")
}

// packageKindRegistered：這個種類（含 `.`）現在有沒有註冊。
func (m *Module) packageKindRegistered(kind string) bool {
	if !strings.Contains(kind, ".") {
		return false
	}
	_, ok := m.spec(kind)
	return ok
}

// SetPackageSchemas 把種類以 `<pkgID>.` 開頭的 schema 整批換成 specs（傳空＝這個套件的種類全拿掉），
// 跟 RegisterSchema 共用同一把鎖；換完重建索引裡這些種類：沒註冊的檔案（停用、解除安裝、被拒）留在原地、不進索引。
// specs 的 Kind 必須是 `<pkgID>.<種類>`，不合的略過。
func (m *Module) SetPackageSchemas(pkgID string, specs []Spec) {
	prefix := pkgID + "."
	m.schemaMu.Lock()
	if m.schemas == nil {
		m.schemas = make(map[string]Spec)
	}
	for kind := range m.schemas {
		if strings.HasPrefix(kind, prefix) {
			delete(m.schemas, kind)
		}
	}
	for _, sc := range specs {
		if strings.HasPrefix(sc.Kind, prefix) && len(sc.Kind) > len(prefix) {
			m.schemas[sc.Kind] = sc
		}
	}
	m.schemaMu.Unlock()
	m.reindexPackage(pkgID, specs)
	m.rescanOfficial()
}

// reindexPackage 重建索引裡這個套件的紀錄。沒開啟（還沒 Open）時什麼都不做：Open 的掃描會照當時註冊的種類建。
func (m *Module) reindexPackage(pkgID string, specs []Spec) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if m.lockFile == nil {
		return
	}
	highest := m.rebuildPackageIndex(pkgID, specs)
	m.counterMu.Lock()
	defer m.counterMu.Unlock()
	if m.counters == nil {
		m.counters = map[string]int64{}
	}
	for kind, n := range highest {
		if n > m.counters[kind] {
			m.counters[kind] = n
		}
	}
}

// rebuildPackageIndex 把索引裡這個套件的紀錄清掉、照現在註冊的種類重新讀檔；回 seq 種類目前（含封存）最大的編號。
func (m *Module) rebuildPackageIndex(pkgID string, specs []Spec) map[string]int64 {
	prefix := pkgID + "."
	vaultRoot := filepath.Join(m.home, "vault")
	dir := filepath.Join(vaultRoot, "pkg", pkgID)
	m.indexMu.Lock()
	defer m.indexMu.Unlock()
	for key, e := range m.index {
		if strings.HasPrefix(e.record.Kind, prefix) {
			delete(m.index, key)
		}
	}
	for path := range m.invalid {
		if strings.HasPrefix(path, dir+string(filepath.Separator)) {
			delete(m.invalid, path)
		}
	}
	if len(specs) == 0 {
		return nil
	}
	// 走訪失敗（資料夾不存在、讀不了）就當作這個套件目前沒有資料；之後檔案監看會補上。
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && m.notOursAbs(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			m.invalid[path] = true
			return nil
		}
		// 同一個 id 在正本與封存都有：given 種類索引留正本（封存檔留在原地、不算壞檔，DEV-151）；其他重複標成壞檔，doctor 看得到。
		if err := m.indexFile(vaultRoot, path); err != nil {
			m.invalid[path] = true
		}
		return nil
	})
	highest := map[string]int64{}
	for _, sc := range specs {
		if sc.IDMode != "seq" {
			continue
		}
		for _, e := range m.index {
			if e.record.Kind != sc.Kind {
				continue
			}
			if n, err := strconv.ParseInt(strings.TrimPrefix(e.record.ID, sc.IDPrefix), 10, 64); err == nil && n > highest[sc.Kind] {
				highest[sc.Kind] = n
			}
		}
	}
	return highest
}
