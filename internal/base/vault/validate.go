package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// InvalidRecord 是一筆跟 schema 對不上的紀錄（doctor 的 data 檢查用）。
type InvalidRecord struct {
	Kind   string // 資料種類；讀不出來就是空的
	ID     string
	Path   string // 相對於家的路徑，斜線分隔
	Field  string // 哪個欄位有問題；整個檔案讀不了就是空的
	Reason string // 白話原因
}

// InvalidRecords 用已註冊的 schema 掃全部紀錄（含封存），列出對不上的。
// 啟動時的索引只在「檔案讀不了」時才會標成壞的，欄位對不對要到這裡才檢查；沒有登記 schema 的種類跳過。
// 唯讀，不改索引；跟 Invalid()（只列檔案讀不了的路徑）互補。
func (m *Module) InvalidRecords(ctx context.Context) ([]InvalidRecord, error) {
	m.lifeMu.RLock()
	defer m.lifeMu.RUnlock()
	if err := m.ready(ctx); err != nil {
		return nil, err
	}
	m.indexMu.RLock()
	entries := sortedEntries(m.index)
	broken := make(map[string]bool, len(m.invalid))
	for path := range m.invalid {
		broken[path] = true
	}
	m.indexMu.RUnlock()

	rel := func(path string) string {
		if r, err := filepath.Rel(m.home, path); err == nil {
			return filepath.ToSlash(r)
		}
		return filepath.ToSlash(path)
	}
	var out []InvalidRecord
	for _, e := range entries {
		if broken[e.path] {
			continue // 下面用路徑單獨列
		}
		sc, ok := m.spec(e.record.Kind)
		if !ok {
			continue
		}
		err := sc.validate(e.record, m.prefixes)
		if err == nil {
			continue
		}
		bad := InvalidRecord{Kind: e.record.Kind, ID: e.record.ID, Path: rel(e.path), Reason: "欄位格式或內容不正確"}
		var pe *platform.Err
		if errors.As(err, &pe) {
			bad.Field, bad.Reason = pe.Field, pe.Message
		}
		out = append(out, bad)
	}
	for path := range broken {
		out = append(out, m.explainBroken(path, rel(path)))
	}
	sort.Slice(out, func(i, j int) bool { return strings.Compare(out[i].Path, out[j].Path) < 0 })
	return out, nil
}

// explainBroken 重新讀一次被標成壞的檔案，盡量說明白是哪裡壞：符號連結、讀不出內容、放錯地方，或是欄位對不上 schema
// （外部編輯時監看會直接把欄位不合的檔案標成壞的）。
func (m *Module) explainBroken(path, relPath string) InvalidRecord {
	bad := InvalidRecord{Path: relPath, Reason: "檔案格式不正確，讀不出內容"}
	info, err := os.Lstat(path)
	if err != nil {
		bad.Reason = "檔案讀不到"
		return bad
	}
	if info.Mode()&os.ModeSymlink != 0 {
		bad.Reason = "資料路徑不可以是符號連結"
		return bad
	}
	data, err := os.ReadFile(path)
	if err != nil {
		bad.Reason = "檔案讀不到"
		return bad
	}
	r, _, decodeErr := Decode(data)
	rel, relErr := filepath.Rel(filepath.Join(m.home, "vault"), path)
	if relErr != nil {
		return bad
	}
	r.Kind, r.ID, _ = inferIdentity(rel, r)
	if r.Kind == "" || r.ID == "" {
		bad.Reason = "檔案格式不正確，或放的位置不屬於任何資料種類"
		return bad
	}
	bad.Kind, bad.ID = r.Kind, r.ID
	if decodeErr != nil {
		return bad
	}
	if sc, ok := m.spec(r.Kind); ok {
		if err := sc.validate(r, m.prefixes); err != nil {
			bad.Reason = "欄位格式或內容不正確"
			var pe *platform.Err
			if errors.As(err, &pe) {
				bad.Field, bad.Reason = pe.Field, pe.Message
			}
		}
	}
	return bad
}
