package vault

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

type entry struct {
	record Record
	order  []string
	path   string
}

func cloneValue(value any) any {
	if value == nil {
		return nil
	}
	return cloneReflect(reflect.ValueOf(value)).Interface()
}
func cloneReflect(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(cloneReflect(v.Elem()))
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cloneReflect(iter.Value()))
		}
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(cloneReflect(v.Index(i)))
		}
		return out
	default:
		return v
	}
}
func cloneRecord(r Record) Record {
	if r.Fields != nil {
		r.Fields = cloneValue(r.Fields).(map[string]any)
	}
	return r
}
func (m *Module) spec(kind string) (Spec, bool) {
	m.schemaMu.RLock()
	defer m.schemaMu.RUnlock()
	s, ok := m.schemas[kind]
	return s, ok
}
func inferIdentity(rel string, r Record) (string, string, bool) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	archived := false
	for i := 0; i < len(parts); i++ {
		if parts[i] == "archive" && i+1 < len(parts) {
			parts = append(parts[:i], parts[i+2:]...)
			archived = true
			break
		}
	}
	if len(parts) == 4 && parts[0] == "pkg" {
		// 套件的資料（D-28）：pkg/<套件 id>/<種類>/<id>.md → 種類 <套件 id>.<種類>。
		return parts[1] + "." + parts[2], strings.TrimSuffix(parts[3], ".md"), archived
	}
	if len(parts) == 2 {
		// policy：權限申請單的欄位本身就叫 kind（rule／guidance），不能靠檔頭的 kind 認種類，一定要照資料夾（2026-11-01 預演升級時發現重開後申請單不見）。
		kinds := map[string]string{"tasks": "task", "reminders": "reminder", "jobs": "job", "messages": "message", "goals": "goal", "policy": "policy_request"}
		if kind := kinds[parts[0]]; kind != "" {
			return kind, strings.TrimSuffix(parts[1], ".md"), archived
		}
	}
	if len(parts) == 3 && parts[0] == "notes" && strings.HasSuffix(parts[2], ".md") {
		// 主題筆記（DEV-161）：notes/<分類>/<主題>.md → 種類 topic、id <分類>/<主題>；
		// 分類的說明檔 notes/<分類>/<分類>.md（Obsidian 的 folder note）→ 種類 topic_category、id <分類>。
		name := strings.TrimSuffix(parts[2], ".md")
		if name == parts[1] {
			return "topic_category", parts[1], archived
		}
		return "topic", parts[1] + "/" + name, archived
	}
	if len(parts) == 1 && parts[0] == "profile.md" {
		return "profile", "profile", archived
	}
	if len(parts) >= 3 && parts[0] == "agents" {
		agent := parts[1]
		if len(parts) == 3 {
			kind := strings.TrimSuffix(parts[2], ".md")
			switch kind {
			case "agent", "addendum", "memory":
				return kind, agent, archived
			}
		}
		if len(parts) >= 4 {
			kind := parts[2]
			if kind == "jobs" {
				kind = "job"
			}
			if kind == "notes" {
				kind = "note"
			}
			switch kind {
			case "journal", "changelog", "note", "job":
				return kind, agent + "/" + strings.TrimSuffix(strings.Join(parts[3:], "/"), ".md"), archived
			}
		}
	}
	return r.Kind, r.ID, archived
}
func (m *Module) scan() error {
	root := filepath.Join(m.home, "vault")
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if m.notOursAbs(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return platform.Invalid("path", "資料路徑不可以是符號連結")
		}
		return m.indexFile(root, path)
	})
}

// indexFile 讀一個檔案放進索引（呼叫者要自己顧索引的鎖）。套件資料夾（vault/pkg/）底下，
// 種類沒註冊的檔案（停用、解除安裝、被拒的套件）留在原地：不進索引、也不算壞檔（D-28）。
func (m *Module) indexFile(root, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return err
	}
	r, order, err := decodeIndexed(rel, path, data)
	if OwnerNoteFile(rel, r, err) {
		return nil // 主人自己放在 notes/ 的筆記（不是主題筆記格式）
	}
	if err != nil {
		if !m.inPackageTree(rel) {
			m.invalid[path] = true
			return nil
		}
		if kind, _, _ := inferIdentity(rel, r); m.packageKindRegistered(kind) {
			m.invalid[path] = true
		}
		return nil
	}
	kind, id, archived := inferIdentity(rel, r)
	if m.inPackageTree(rel) && !m.packageKindRegistered(kind) {
		return nil
	}
	if kind == "" || id == "" {
		m.invalid[path] = true
		return nil
	}
	key := recordKey(kind, id)
	r.Kind = kind
	r.ID = id
	r.Archived = archived
	next := entry{r, order, path}
	if old, ok := m.index[key]; ok {
		keepNew, dup := prefer(old, next, m.givenIDs(kind))
		if dup {
			return platform.Conflict("資料識別碼重複，請檢查封存與正本")
		}
		if !keepNew {
			return nil
		}
	}
	m.index[key] = next
	return nil
}

// prefer 決定同一個種類與編號有兩個檔案時索引留哪一個（DEV-151）：自己給編號的套件資料（given，例如用日期當編號）
// 封存後可以再用同一個編號記新的一筆，所以正本和封存可以同時存在——索引留正本，封存的檔案留在 archive/ 不動；
// 兩份都封存就留比較新的。兩份都是正本、或不是 given 的種類（核心發號，不該重複），才是真的重複（dup）。
func prefer(old, next entry, given bool) (keepNew, dup bool) {
	switch {
	case !given, !old.record.Archived && !next.record.Archived:
		return false, true
	case !old.record.Archived:
		return false, false
	case !next.record.Archived:
		return true, false
	default:
		return next.record.UpdatedAt.After(old.record.UpdatedAt), false
	}
}

// givenIDs：這個種類的編號是不是呼叫者自己給的（套件的 given 種類）。
func (m *Module) givenIDs(kind string) bool {
	sc, ok := m.spec(kind)
	return ok && sc.IDMode == "given"
}
func (m *Module) lookup(kind, id string) (entry, bool) {
	m.indexMu.RLock()
	defer m.indexMu.RUnlock()
	e, ok := m.index[recordKey(kind, id)]
	return e, ok
}
func (m *Module) publishEntry(e entry) {
	m.indexMu.Lock()
	defer m.indexMu.Unlock()
	m.index[recordKey(e.record.Kind, e.record.ID)] = e
}
func defaultPath(kind, id string) string {
	if kind == "job" {
		parts := strings.Split(id, "/")
		if len(parts) == 1 {
			return "vault/jobs/" + id + ".md"
		}
		if len(parts) == 2 {
			return "vault/agents/" + parts[0] + "/jobs/" + parts[1] + ".md"
		}
		return ""
	}
	switch kind {
	case "goal":
		return "vault/goals/" + id + ".md"
	case "message":
		return "vault/messages/" + id + ".md"
	case "task":
		return "vault/tasks/" + id + ".md"
	case "reminder":
		return "vault/reminders/" + id + ".md"
	case "agent", "memory", "addendum":
		return "vault/agents/" + id + "/" + kind + ".md"
	case "profile":
		return "vault/profile.md"
	case "journal", "changelog", "note", "job":
		parts := strings.SplitN(id, "/", 2)
		if len(parts) != 2 {
			return ""
		}
		dir := kind
		if kind == "note" {
			dir = "notes"
		}
		return "vault/agents/" + parts[0] + "/" + dir + "/" + parts[1] + ".md"
	default:
		return ""
	}
}
func (m *Module) recordPath(sc Spec, id string) (string, error) {
	if id == "" || filepath.IsAbs(id) || strings.Contains(id, "\\") {
		return "", platform.Invalid("id", "資料識別碼不正確")
	}
	for _, p := range strings.Split(id, "/") {
		if p == "" || p == "." || p == ".." {
			return "", platform.Invalid("id", "資料識別碼不正確")
		}
	}
	rel := defaultPath(sc.Kind, id)
	if sc.Path != nil {
		rel = sc.Path(id)
	}
	clean := filepath.Clean(rel)
	if rel == "" || filepath.IsAbs(rel) || !strings.HasPrefix(filepath.ToSlash(clean), "vault/") {
		return "", platform.Invalid("path", "資料路徑超出資料庫範圍")
	}
	path := filepath.Join(m.home, clean)
	for cur := path; cur != m.home; cur = filepath.Dir(cur) {
		info, err := os.Lstat(cur)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", platform.Invalid("path", "資料路徑不可以是符號連結")
		}
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return path, nil
}
func matchValue(have, want any) bool {
	if reflect.DeepEqual(have, want) {
		return true
	}
	v := reflect.ValueOf(have)
	if v.IsValid() && (v.Kind() == reflect.Slice || v.Kind() == reflect.Array) {
		for i := 0; i < v.Len(); i++ {
			if reflect.DeepEqual(v.Index(i).Interface(), want) {
				return true
			}
		}
	}
	return false
}
func sortedEntries(index map[string]entry) []entry {
	out := make([]entry, 0, len(index))
	for _, e := range index {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].record.ID < out[j].record.ID })
	return out
}
