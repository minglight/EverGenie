package vault

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 官方層（DEV-154，作者 2026-11-12「官方的東西不放家裡、升級不寫家裡」）：家外面一個資料夾，放官方角色包帶來、
// 跟著角色走的套件紀錄（例如官方衣服的目錄）。裡面的排法跟家裡的 vault/ 一樣：<官方層>/pkg/<套件>/<種類>/<編號>.md。
//
//   - 讀：Get、List 把官方層跟家裡的合起來；同一個編號家裡有（含封存的）就用家裡的。
//   - 主人或套件修改官方層的一筆：在家裡寫一份（主人改過的留著，官方之後換新也不蓋它）。官方層的檔本身只有 OfficialPut 會寫。
//   - 只收套件自己給編號的種類（given）；流水號的種類不放官方層（編號是每個家自己發的）。
//   - 官方層的紀錄不進變更紀錄、不能封存；OfficialPut 寫完發變更事件（家裡有同編號的除外），訂閱的套件程式馬上重算（DEV-157）。
//
// 只有「官方角色放家外」的家才有官方層（佈局第 2 版而且沒寫 official_personas = "home"）；其他的家 officialRoot 是空的，行為跟以前一樣。

// OfficialBy 是官方層紀錄的署名。
const OfficialBy = "official"

// officialRootFor 回這個家的官方層資料夾；沒有官方層回空字串。
func officialRootFor(home string) string {
	l := platform.LayoutFor(home)
	if !l.V2 || l.PersonasAtHome {
		return ""
	}
	return l.OfficialPersonas("records")
}

// scanOfficial 重讀官方層（呼叫者不用拿鎖）。種類沒註冊、不是 given、讀不懂、是符號連結的檔略過：官方層壞了不擋家裡的資料。
func (m *Module) scanOfficial() {
	root := m.officialRoot
	next := map[string]entry{}
	if root != "" {
		_ = filepath.WalkDir(filepath.Join(root, "pkg"), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".md" || !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			r, order, err := Decode(data)
			if err != nil {
				return nil
			}
			kind, id, archived := inferIdentity(rel, r)
			sc, ok := m.spec(kind)
			if archived || !ok || sc.IDMode != "given" || !strings.Contains(kind, ".") || sc.checkID(id) != nil {
				return nil
			}
			r.Kind, r.ID = kind, id
			next[recordKey(kind, id)] = entry{r, order, path}
			return nil
		})
	}
	m.officialMu.Lock()
	m.official = next
	m.officialMu.Unlock()
}

// officialLookup 找官方層的一筆（不看家裡有沒有）。
func (m *Module) officialLookup(kind, id string) (entry, bool) {
	m.officialMu.RLock()
	defer m.officialMu.RUnlock()
	e, ok := m.official[recordKey(kind, id)]
	return e, ok
}

// find 是讀一筆的單一入口：家裡的優先（含封存的），沒有才看官方層。fromOfficial＝這筆只在官方層。
func (m *Module) find(kind, id string) (e entry, ok, fromOfficial bool) {
	if e, ok := m.lookup(kind, id); ok {
		return e, true, false
	}
	if e, ok := m.officialLookup(kind, id); ok {
		return e, true, true
	}
	return entry{}, false, false
}

// officialOnly 回官方層裡、家裡沒有同編號的紀錄（某一種；照編號排）。呼叫者不可拿著 indexMu。
func (m *Module) officialOnly(kind string) []entry {
	m.officialMu.RLock()
	var out []entry
	for _, e := range m.official {
		if e.record.Kind == kind {
			out = append(out, e)
		}
	}
	m.officialMu.RUnlock()
	if len(out) == 0 {
		return nil
	}
	m.indexMu.RLock()
	kept := out[:0]
	for _, e := range out {
		if _, shadowed := m.index[recordKey(e.record.Kind, e.record.ID)]; !shadowed {
			kept = append(kept, e)
		}
	}
	m.indexMu.RUnlock()
	sort.Slice(kept, func(i, j int) bool { return kept[i].record.ID < kept[j].record.ID })
	return kept
}

// HasOfficialLayer：這個家有沒有官方層（官方角色放家外的家才有）。
func (m *Module) HasOfficialLayer() bool {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	return m.officialRoot != ""
}

// OfficialGet 讀官方層的一筆（不管家裡有沒有同編號的）；沒有回 false。
func (m *Module) OfficialGet(kind, id string) (Record, bool) {
	e, ok := m.officialLookup(kind, id)
	if !ok {
		return Record{}, false
	}
	return cloneRecord(e.record), true
}

// OfficialList 列官方層某一種的全部紀錄（不管家裡有沒有同編號的；照編號排）。
func (m *Module) OfficialList(kind string) []Record {
	m.officialMu.RLock()
	defer m.officialMu.RUnlock()
	var out []Record
	for _, e := range m.official {
		if e.record.Kind == kind {
			out = append(out, cloneRecord(e.record))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// OfficialPut 把一筆寫進官方層（新增或整筆換掉；只給角色包同步用）。種類要註冊中而且是 given；欄位照規格驗。
// 家裡的檔一個都不動、不進變更紀錄；家裡有同編號的（主人改過的那份）照樣蓋過它。
// 寫完發一個變更事件給訂閱的（VLT-19；衣櫃等套件的程式照它馬上重算，不用等定期重掃）；家裡有同編號的就不發（大家看到的還是家裡那份，沒變）。
func (m *Module) OfficialPut(ctx context.Context, r Record) (Record, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return Record{}, err
	}
	if m.officialRoot == "" {
		return Record{}, platform.Invalid("official", "這個家的官方角色放在家裡，沒有官方層")
	}
	sc, ok := m.spec(r.Kind)
	if !ok {
		return Record{}, platform.Invalid("kind", "資料種類尚未註冊")
	}
	pkgID, name, isPkg := strings.Cut(r.Kind, ".")
	if !isPkg || sc.IDMode != "given" {
		return Record{}, platform.Invalid("kind", "官方層只收套件自己給編號的資料")
	}
	if err := sc.checkID(r.ID); err != nil {
		return Record{}, err
	}
	if strings.ContainsAny(r.ID, "/\\") {
		return Record{}, platform.Invalid("id", "資料識別碼不正確")
	}
	lock := m.shard(r.Kind, r.ID)
	lock.Lock()
	defer lock.Unlock()
	r = cloneRecord(r)
	if r.Fields == nil {
		r.Fields = map[string]any{}
	}
	old, exists := m.officialLookup(r.Kind, r.ID)
	now := m.d.Clock.Now().In(m.loc)
	r.UpdatedAt, r.Archived = now, false
	r.Fields["updated_at"] = now.Format(time.RFC3339Nano)
	r.Fields["by"] = OfficialBy
	r.Fields["schema"] = 1
	r.Fields["id"] = r.ID
	if exists {
		if v, ok := old.record.Fields["created_at"]; ok {
			r.Fields["created_at"] = v
		}
	}
	if _, ok := r.Fields["created_at"]; !ok {
		r.Fields["created_at"] = now.Format(time.RFC3339Nano)
	}
	for _, f := range sc.Fields {
		if _, ok := r.Fields[f.Name]; !ok && f.Default != nil {
			r.Fields[f.Name] = cloneValue(f.Default)
		}
	}
	if err := sc.validate(r, m.prefixes); err != nil {
		return Record{}, err
	}
	path := filepath.Join(m.officialRoot, "pkg", pkgID, name, r.ID+".md")
	for cur := filepath.Dir(path); len(cur) > len(m.officialRoot); cur = filepath.Dir(cur) {
		if info, err := os.Lstat(cur); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return Record{}, platform.Invalid("path", "資料路徑不可以是符號連結")
		}
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return Record{}, platform.Invalid("path", "資料路徑不可以是符號連結")
	}
	data := Encode(r, old.order, sc)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Record{}, err
	}
	if err := platform.AtomicWrite(path, data, 0o600); err != nil {
		return Record{}, err
	}
	r.Revision = revision(data)
	_, order, err := Decode(data)
	if err != nil {
		return Record{}, err
	}
	m.officialMu.Lock()
	if m.official == nil {
		m.official = map[string]entry{}
	}
	m.official[recordKey(r.Kind, r.ID)] = entry{cloneRecord(r), order, path}
	m.officialMu.Unlock()
	m.indexMu.RLock()
	_, shadowed := m.index[recordKey(r.Kind, r.ID)]
	m.indexMu.RUnlock()
	if !shadowed {
		typ := sdk.ChangeTypeCreated
		if exists {
			typ = sdk.ChangeTypeUpdated
		}
		m.emitChange(r, typ, OfficialBy)
	}
	return cloneRecord(r), nil
}

// rescanOfficial 在套件的種類換過之後重讀官方層（官方層只認註冊中的種類）。還沒開啟時不做：Open 會掃。
func (m *Module) rescanOfficial() {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if m.lockFile == nil || m.officialRoot == "" {
		return
	}
	m.scanOfficial()
}
