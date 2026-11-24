package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// ArchiveNow 跟 Archive 一樣把一筆搬進 archive/<YYYY-MM>/（月份是現在），但不看種類的保存規則（ArchiveAt）。
// 給使用者明確要求封存的操作用（例如 agents rm）；清道夫照舊只用 Archive 的規則。已封存的不再動。
func (m *Module) ArchiveNow(ctx context.Context, kind, id, by string) error {
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
		return platform.NotFound("找不到這筆資料")
	}
	if e.record.Archived {
		return nil
	}
	return m.archiveEntry(e, m.d.Clock.Now(), by)
}

// archiveEntry 把 e 搬進 archive/<at 的月份>/；呼叫者要持有這筆的分片鎖。
func (m *Module) archiveEntry(e entry, at time.Time, by string) error {
	data, err := os.ReadFile(e.path)
	if err != nil || revision(data) != e.record.Revision {
		return platform.Conflict("檔案已被修改，請重新讀取")
	}
	// 同一個月同一個編號封存第二次（DEV-151：自己給編號的資料封存後又記了新的一筆）就放 <月份>-2、-3…，
	// 不蓋掉先封存的那份；索引看 archive/ 底下的資料夾一律只跳過一層，名字不影響辨識。
	month := at.In(m.loc).Format("2006-01")
	dest := ""
	for n := 1; ; n++ {
		dir := month
		if n > 1 {
			dir = fmt.Sprintf("%s-%d", month, n)
		}
		dest = filepath.Join(filepath.Dir(e.path), "archive", dir, filepath.Base(e.path))
		_, err := os.Lstat(dest)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return err
		}
		if n >= 100 {
			return platform.Conflict("封存檔案已存在")
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	if err := platform.Move(e.path, dest); err != nil {
		return err
	}
	e.path = dest
	e.record.Archived = true
	m.publishEntry(e)
	if err := m.changeLog(e.record, by, "archive"); err != nil {
		return err
	}
	m.emitChange(e.record, sdk.ChangeTypeArchived, by)
	return nil
}

// Move 把一筆（還沒封存的）改成新的 id：檔案搬到新 id 的路徑，set 的欄位一起改（例如 agent 欄位），
// 內文與其他欄位原樣保留。新 id 已經有資料（含封存的）回 conflict。日誌也可以搬（內容不改，只改歸屬）。
func (m *Module) Move(ctx context.Context, kind, from, to string, set map[string]any, by string) (Record, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return Record{}, err
	}
	if from == to {
		return Record{}, platform.Invalid("id", "新舊識別碼相同")
	}
	unlock := m.lockPair(kind, from, to)
	defer unlock()
	e, ok := m.lookup(kind, from)
	if !ok {
		return Record{}, platform.NotFound("找不到這筆資料")
	}
	if e.record.Archived {
		return Record{}, platform.Conflict("已封存的資料不能搬")
	}
	if _, taken := m.lookup(kind, to); taken {
		return Record{}, platform.Conflict("新的識別碼已經有資料")
	}
	sc, ok := m.spec(kind)
	if !ok {
		return Record{}, platform.Invalid("kind", "資料種類尚未註冊")
	}
	dest, err := m.recordPath(sc, to)
	if err != nil {
		return Record{}, err
	}
	if _, err := os.Lstat(dest); err == nil {
		return Record{}, platform.Conflict("新的位置已經有檔案")
	} else if !os.IsNotExist(err) {
		return Record{}, err
	}
	disk, err := os.ReadFile(e.path)
	if err != nil || revision(disk) != e.record.Revision {
		return Record{}, platform.Conflict("檔案已被修改，請重新讀取後再試")
	}
	r := cloneRecord(e.record)
	r.ID = to
	for k, v := range set {
		r.Fields[k] = cloneValue(v)
	}
	if _, has := r.Fields["id"]; has {
		r.Fields["id"] = to
	}
	now := m.d.Clock.Now().In(m.loc)
	r.UpdatedAt = now
	r.Fields["updated_at"] = now.Format(time.RFC3339Nano)
	r.Fields["by"] = by

	if err := sc.validate(r, m.prefixes); err != nil {
		return Record{}, err
	}
	data := Encode(r, e.order, sc)
	if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return Record{}, err
	}
	// 先搬再改寫：中途失敗時檔案只會在舊或新的其中一個位置，不會兩份。
	if err := platform.Move(e.path, dest); err != nil {
		return Record{}, err
	}
	if err := platform.AtomicWrite(dest, data, 0600); err != nil {
		return Record{}, err
	}
	r.Revision = revision(data)
	_, order, err := Decode(data)
	if err != nil {
		return Record{}, err
	}
	m.indexMu.Lock()
	delete(m.index, recordKey(kind, from))
	m.indexMu.Unlock()
	m.publishEntry(entry{cloneRecord(r), order, dest})
	old := cloneRecord(e.record)
	if err := m.changeLog(old, by, "move"); err != nil {
		return Record{}, err
	}
	if err := m.changeLog(r, by, "move"); err != nil {
		return Record{}, err
	}
	old.Archived = true
	m.emitChange(old, sdk.ChangeTypeArchived, by)
	m.emitChange(r, sdk.ChangeTypeCreated, by)
	return cloneRecord(r), nil
}

// lockPair 依固定順序拿兩筆的分片鎖（同一把只拿一次），避免互相等待。
func (m *Module) lockPair(kind, a, b string) func() {
	la, lb := m.shard(kind, a), m.shard(kind, b)
	if la == lb {
		la.Lock()
		return la.Unlock
	}
	// 依分片在陣列裡的位置排序，所有呼叫者順序一致。
	first, second := la, lb
	if shardIndex(m, lb) < shardIndex(m, la) {
		first, second = lb, la
	}
	first.Lock()
	second.Lock()
	return func() { second.Unlock(); first.Unlock() }
}

func shardIndex(m *Module, l *sync.Mutex) int {
	for i := range m.shards {
		if &m.shards[i] == l {
			return i
		}
	}
	return -1
}
