package vault

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
)

// 這個檔的兩個方法只給遷移工具用（P5-S2，REQ-121）：
// 舊系統已經封存的資料要直接進 archive/<YYYY-MM>/，計數器要接續舊的最大號。
// 其他程式不要呼叫；一般封存走 Archive 與清道夫（sweep.go）。

var archiveMonthPattern = regexp.MustCompile(`^[0-9]{4}-(0[1-9]|1[0-2])$`)

// ImportArchived 把一筆新的資料直接寫進 archive/<month>/（month 是 YYYY-MM）。
// 跟 Put 一樣驗證 schema、寫變更紀錄；這一筆必須是新的（已存在回 ERROR_CODE_CONFLICT）。
// 之後要更新它，用一般的 Put（原地更新，仍在封存資料夾）。
func (m *Module) ImportArchived(ctx context.Context, r Record, month, by string) (Record, error) {
	if !archiveMonthPattern.MatchString(month) {
		return Record{}, platform.Invalid("month", "封存月份請用 YYYY-MM")
	}
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return Record{}, err
	}
	if r.Kind == "journal" {
		return Record{}, platform.Forbidden("日誌只能追加，不能直接匯入封存")
	}
	lock := m.shard(r.Kind, r.ID)
	lock.Lock()
	defer lock.Unlock()
	if _, exists := m.lookup(r.Kind, r.ID); exists {
		return Record{}, platform.Conflict("這筆資料已經存在，無法直接匯入封存")
	}
	return m.putAt(ctx, r, by, "", "import-archived", month)
}

// EnsureCounter 讓 kind 的計數器至少是 atLeast（下一個 NextID 會是 atLeast+1）。
// 計數器已經大於或等於時什麼都不寫，回傳 false；有改才回傳 true。
func (m *Module) EnsureCounter(ctx context.Context, kind string, atLeast int64) (bool, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return false, err
	}
	if _, ok := m.prefixes[kind]; !ok {
		return false, platform.Invalid("kind", "這個種類沒有遞增識別碼")
	}
	if atLeast < 0 {
		return false, platform.Invalid("counter", "計數器不可小於零")
	}
	lock, _ := m.counterLocks.LoadOrStore(kind, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	m.counterMu.Lock()
	defer m.counterMu.Unlock()
	if m.counters[kind] >= atLeast {
		return false, nil
	}
	next := map[string]int64{}
	for k, v := range m.counters {
		next[k] = v
	}
	next[kind] = atLeast
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(next); err != nil {
		return false, err
	}
	path := filepath.Join(m.home, "vault", ".counters.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return false, err
	}
	if err := platform.AtomicWrite(path, buf.Bytes(), 0600); err != nil {
		return false, err
	}
	m.counters = next
	return true, nil
}
