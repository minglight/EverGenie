package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.lockFile == nil {
		return platform.Unavailable("資料庫尚未開啟")
	}
	return nil
}
func (m *Module) Get(ctx context.Context, kind, id string) (Record, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return Record{}, err
	}
	e, ok := m.lookup(kind, id)
	path := e.path
	if !ok {
		if sc, exists := m.spec(kind); exists {
			path, _ = m.recordPath(sc, id)
		}
	}
	if m.isInvalid(path) {
		return Record{}, platform.Invalid("record", "檔案格式不正確，請修正後重試")
	}
	if !ok {
		// 家裡沒有：看官方層（官方角色包帶來的紀錄，official.go）。
		if oe, found := m.officialLookup(kind, id); found {
			return cloneRecord(oe.record), nil
		}
		return Record{}, platform.NotFound("找不到這筆資料")
	}
	return cloneRecord(e.record), nil
}
func (m *Module) List(ctx context.Context, kind string, q Query) ([]Record, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	m.indexMu.RLock()
	entries := []entry{}
	for _, e := range sortedEntries(m.index) {
		if !m.invalid[e.path] {
			entries = append(entries, e)
		}
	}
	m.indexMu.RUnlock()
	// 官方層裡、家裡沒有同編號的也算（official.go）；合起來照編號排。
	if extra := m.officialOnly(kind); len(extra) > 0 {
		entries = append(entries, extra...)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].record.ID < entries[j].record.ID })
	}
	result := []Record{}
	for _, e := range entries {
		r := e.record
		if r.Kind != kind || r.Archived && !q.IncludeArchived {
			continue
		}
		matches := true
		for k, v := range q.Where {
			if !matchValue(r.Fields[k], v) {
				matches = false
				break
			}
		}
		if matches {
			result = append(result, cloneRecord(r))
		}
	}
	if q.Page != nil {
		start := 0
		if q.Page.Cursor != "" {
			n, err := strconv.Atoi(q.Page.Cursor)
			if err != nil || n < 0 {
				return nil, platform.Invalid("cursor", "分頁位置不正確")
			}
			start = n
		}
		if start >= len(result) {
			return []Record{}, nil
		}
		result = result[start:]
		if q.Page.Limit < 0 {
			return nil, platform.Invalid("limit", "筆數不可小於零")
		}
		if q.Page.Limit > 0 && int(q.Page.Limit) < len(result) {
			result = result[:q.Page.Limit]
		}
	}
	return result, nil
}
func (m *Module) Put(ctx context.Context, r Record, by, expectedRev string) (Record, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return Record{}, err
	}
	if r.Kind == "journal" {
		return Record{}, platform.Forbidden("日誌只能追加，不能覆寫")
	}
	lock := m.shard(r.Kind, r.ID)
	lock.Lock()
	defer lock.Unlock()
	return m.putLocked(ctx, r, by, expectedRev, "put")
}
func (m *Module) putLocked(ctx context.Context, r Record, by, expectedRev, op string) (Record, error) {
	return m.putAt(ctx, r, by, expectedRev, op, "")
}

// putAt 是 putLocked 的完整版：archiveMonth 不是空字串時，新的一筆直接寫進 archive/<archiveMonth>/（只給遷移匯入用）。
func (m *Module) putAt(ctx context.Context, r Record, by, expectedRev, op, archiveMonth string) (Record, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	sc, ok := m.spec(r.Kind)
	if !ok {
		return Record{}, platform.Invalid("kind", "資料種類尚未註冊")
	}
	if err := sc.checkID(r.ID); err != nil {
		return Record{}, err
	}
	r = cloneRecord(r)
	if r.Fields == nil {
		r.Fields = map[string]any{}
	}
	e, exists := m.lookup(r.Kind, r.ID)
	if exists && e.record.Archived && sc.IDMode != "" && archiveMonth == "" {
		// 套件資料的封存紀錄不再改（DEV-151：以前 data set 回成功卻改到 archive/ 裡那筆）。
		if expectedRev != "" {
			return Record{}, platform.NotFound("找不到這筆資料（這個編號已封存；要重新記一筆請用新增）")
		}
		// given 種類新增一筆、而這個編號只剩封存的紀錄：封存的留在 archive/ 不動，新的寫回正本位置。
		if sc.IDMode == "given" {
			e, exists = entry{}, false
		}
	}
	// 這個編號只在官方層：對呼叫者來說它是「已經有的一筆」（新增要擋、修改要帶它的 revision），
	// 寫的時候在家裡寫一份——主人改過的留在家裡，官方層那個檔不動。
	official, inOfficial := entry{}, false
	if !exists {
		official, inOfficial = m.officialLookup(r.Kind, r.ID)
	}
	switch {
	case (exists || inOfficial) && expectedRev == "":
		return Record{}, platform.Conflict("這個編號已經有資料了；要改內容請用修改，不是新增")
	case inOfficial && official.record.Revision != expectedRev:
		return Record{}, platform.Conflict("資料已更新，請重新讀取後再試")
	case !exists && !inOfficial && expectedRev != "", exists && e.record.Revision != expectedRev:
		return Record{}, platform.Conflict("資料已更新，請重新讀取後再試")
	}
	if inOfficial {
		if v, ok := official.record.Fields["created_at"]; ok {
			if _, has := r.Fields["created_at"]; !has {
				r.Fields["created_at"] = v
			}
		}
	}
	if !exists && !inOfficial && op == "put" {
		// 變更紀錄分得出「新增」和「整筆更新」（設定→紀錄用）。
		op = "create"
	}
	path, err := m.recordPath(sc, r.ID)
	if err != nil {
		return Record{}, err
	}
	if exists {
		path = e.path
	} else if archiveMonth != "" {
		path = filepath.Join(filepath.Dir(path), "archive", archiveMonth, filepath.Base(path))
	}
	disk, err := os.ReadFile(path)
	if exists {
		if err != nil || revision(disk) != e.record.Revision {
			return Record{}, platform.Conflict("檔案已被修改，請重新讀取後再試")
		}
	} else if err == nil {
		return Record{}, platform.Conflict("檔案已存在，請重新讀取")
	} else if !errors.Is(err, os.ErrNotExist) {
		return Record{}, err
	}
	now := m.d.Clock.Now().In(m.loc)
	r.UpdatedAt = now
	r.Archived = exists && e.record.Archived || !exists && archiveMonth != ""
	r.Fields["updated_at"] = now.Format(time.RFC3339Nano)
	r.Fields["by"] = by

	if !exists {
		if _, ok := r.Fields["created_at"]; !ok {
			r.Fields["created_at"] = now.Format(time.RFC3339Nano)
		}
		r.Fields["schema"] = 1
	}
	switch r.Kind {
	case "memory", "journal", "addendum", "profile", "note", "changelog", "topic", "topic_category":
	default:
		r.Fields["id"] = r.ID
	}
	for _, f := range sc.Fields {
		if _, ok := r.Fields[f.Name]; !ok && f.Default != nil {
			r.Fields[f.Name] = cloneValue(f.Default)
		}
	}
	if err := sc.validate(r, m.prefixes); err != nil {
		return Record{}, err
	}
	for _, f := range sc.Fields {
		if f.Type != Link {
			continue
		}
		value, ok := r.Fields[f.Name].(string)
		if !ok || value == "" {
			continue
		}
		id, ok := linkID(value, f.LinkKind, m.prefixes)
		if !ok {
			return Record{}, platform.Invalid(f.Name, "連結格式不正確")
		}
		if _, ok, _ := m.find(f.LinkKind, id); !ok {
			return Record{}, platform.Invalid(f.Name, "連結指向的資料不存在")
		}
	}
	data := Encode(r, e.order, sc)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return Record{}, err
	}
	if err := platform.AtomicWrite(path, data, 0600); err != nil {
		return Record{}, err
	}
	r.Revision = revision(data)
	_, order, err := Decode(data)
	if err != nil {
		return Record{}, err
	}
	m.publishEntry(entry{cloneRecord(r), order, path})
	if err := m.changeLog(r, by, op); err != nil {
		return Record{}, err
	}
	typ := sdk.ChangeTypeCreated
	if exists {
		typ = sdk.ChangeTypeUpdated
	}
	m.emitChange(r, typ, by)
	return cloneRecord(r), nil
}
func (m *Module) Patch(ctx context.Context, kind, id string, set map[string]any, unset []string, by string) (Record, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return Record{}, err
	}
	if kind == "journal" {
		return Record{}, platform.Forbidden("日誌只能追加，不能修改")
	}
	lock := m.shard(kind, id)
	lock.Lock()
	defer lock.Unlock()
	e, ok, _ := m.find(kind, id) // 只在官方層的一筆：改了就在家裡寫一份（putAt）
	if !ok {
		return Record{}, platform.NotFound("找不到這筆資料")
	}
	r := cloneRecord(e.record)
	for k, v := range set {
		r.Fields[k] = cloneValue(v)
	}
	for _, k := range unset {
		delete(r.Fields, k)
	}
	return m.putLocked(ctx, r, by, e.record.Revision, "patch")
}
func (m *Module) Append(ctx context.Context, kind, id, line, by string) error {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	if kind != "journal" {
		return platform.Forbidden("這個種類不支援追加日誌")
	}
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		return platform.Invalid("id", "請指定職位與日誌日期")
	}
	if _, err := time.Parse("2006-01-02", parts[1]); err != nil {
		return platform.Invalid("date", "日誌日期不正確")
	}
	if strings.ContainsAny(line, "\r\n") {
		return platform.Invalid("line", "每次只能追加一行日誌")
	}
	lock := m.shard(kind, id)
	lock.Lock()
	defer lock.Unlock()
	e, exists := m.lookup(kind, id)
	r := cloneRecord(e.record)
	if !exists {
		r = Record{Kind: kind, ID: id, Fields: map[string]any{"agent": parts[0], "date": parts[1]}}
	}
	if r.Body != "" {
		r.Body += "\n"
	}
	r.Body += line
	_, err := m.putLocked(ctx, r, by, e.record.Revision, "append")
	return err
}
func (m *Module) Archive(ctx context.Context, kind, id, by string) error {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return err
	}
	lock := m.shard(kind, id)
	lock.Lock()
	defer lock.Unlock()
	e, ok := m.lookup(kind, id)
	if !ok {
		if _, official := m.officialLookup(kind, id); official {
			return platform.Forbidden("這筆是官方提供的，不能封存")
		}
		return platform.NotFound("找不到這筆資料")
	}
	if e.record.Archived {
		return nil
	}
	sc, ok := m.spec(kind)
	if !ok || sc.ArchiveAt == nil {
		return platform.Invalid("kind", "資料種類未設定封存規則")
	}
	at, ok := sc.ArchiveAt(e.record, m.loc)
	if !ok {
		return platform.Conflict("這筆資料尚未符合封存條件")
	}
	return m.archiveEntry(e, at, by)
}
