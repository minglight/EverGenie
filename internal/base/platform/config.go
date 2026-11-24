package platform

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// HomePath 是家的設定檔位置。
func HomePath(home string) string { return filepath.Join(home, "home.toml") }

func readHomeRaw(home string) ([]byte, map[string]any, error) {
	data, err := os.ReadFile(HomePath(home))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, NotFound("找不到家的設定，請先建立設定檔")
	}
	if err != nil {
		return nil, nil, Unavailable("無法讀取家的設定，請檢查存取權限")
	}
	raw := map[string]any{}
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return nil, nil, Invalid("home", "設定格式或欄位型別不正確，請檢查設定檔")
	}
	return data, raw, nil
}

func configKey(key string) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(key), ".")
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return nil, Invalid("key", "設定名稱用點分隔，例如 user.quiet_hours")
		}
	}
	return parts, nil
}

// RenderValue 把一個值寫成 TOML 的寫法（表格是多行的 TOML 內容）。
func RenderValue(v any) (string, error) {
	var buf bytes.Buffer
	if table, ok := v.(map[string]any); ok {
		if err := toml.NewEncoder(&buf).Encode(table); err != nil {
			return "", Internal("無法寫出設定值")
		}
		return strings.TrimRight(buf.String(), "\n"), nil
	}
	if err := toml.NewEncoder(&buf).Encode(map[string]any{"v": v}); err != nil {
		return "", Internal("無法寫出設定值")
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(buf.String()), "v =")), nil
}

// GetHomeValue 讀 home.toml 的一個值（key 用點分隔）；key 空回整份檔案的文字。只讀。
func GetHomeValue(home, key string) (value, text string, err error) {
	data, raw, err := readHomeRaw(home)
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(key) == "" {
		return "", string(data), nil
	}
	parts, err := configKey(key)
	if err != nil {
		return "", "", err
	}
	var cur any = raw
	for _, p := range parts {
		table, ok := cur.(map[string]any)
		if !ok {
			return "", "", NotFound("home.toml 沒有 " + key)
		}
		if cur, ok = table[p]; !ok {
			return "", "", NotFound("home.toml 沒有 " + key)
		}
	}
	value, err = RenderValue(cur)
	return value, "", err
}

// parseValue 把一段文字當成 TOML 的值；不是合法的 TOML 值就當成文字。
func parseValue(s string) any {
	holder := map[string]any{}
	if _, err := toml.Decode("v = "+s, &holder); err == nil {
		if v, ok := holder["v"]; ok {
			return v
		}
	}
	return s
}

// SetHomeValue 改 home.toml 的一個值：先把舊檔備份到暫存資料夾的 runtime/backups/home/<時間>.toml，寫完用 LoadHome 重讀驗證，
// 不合格就把舊檔寫回並回錯誤（跟 SetLevels 一樣；註解不保留）。schema 不能改。回傳備份的路徑（第 1 版相對於家、第 2 版絕對路徑）與寫進去的值。
func SetHomeValue(home, key, value string, now time.Time) (backup, rendered string, err error) {
	parts, err := configKey(key)
	if err != nil {
		return "", "", err
	}
	if len(parts) == 1 && parts[0] == "schema" {
		return "", "", Forbidden("設定版本（schema）不能用指令改")
	}
	v := parseValue(value)
	if backup, err = setHomeAny(home, key, v, now); err != nil {
		return backup, "", err
	}
	rendered, err = RenderValue(v)
	return backup, rendered, err
}

// ReadHomeTable 讀 home.toml 成一份表格（只讀；給業務模組讀自己那一段，例如帳號清單）。
func ReadHomeTable(home string) (map[string]any, error) {
	_, raw, err := readHomeRaw(home)
	return raw, err
}

// SetHomeTable 把 home.toml 的 key（點分隔）設成 Go 的值（表格用 map[string]any）：備份、寫完驗證、不合格還原，
// 跟 SetHomeValue 同一套；給業務模組改自己那一段。回傳備份路徑。
func SetHomeTable(home, key string, v any, now time.Time) (backup string, err error) {
	return setHomeAny(home, key, v, now)
}

// setHomeAny 是 SetHomeValue 的本體：值已經是 Go 的值（表格用 map[string]any）。
func setHomeAny(home, key string, v any, now time.Time) (backup string, err error) {
	parts, err := configKey(key)
	if err != nil {
		return "", err
	}
	old, raw, err := readHomeRaw(home)
	if err != nil {
		return "", err
	}
	table := raw
	for _, p := range parts[:len(parts)-1] {
		next, exists := table[p]
		if !exists {
			created := map[string]any{}
			table[p] = created
			table = created
			continue
		}
		t, ok := next.(map[string]any)
		if !ok {
			return "", Invalid("key", p+" 不是表格，不能往下設")
		}
		table = t
	}
	table[parts[len(parts)-1]] = v
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(raw); err != nil {
		return "", Internal("無法寫出家的設定")
	}
	// 備份在暫存資料夾的 backups/home/：在家裡（第 1 版）回相對於家的路徑，在家外面（第 2 版）回絕對路徑。
	abs := RuntimeDir(home, "backups", "home", now.Format("20060102-150405.000000000")+".toml")
	backup = abs
	if rel, err := filepath.Rel(home, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		backup = rel
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return "", Unavailable("無法備份家的設定")
	}
	if err := AtomicWrite(abs, old, 0o600); err != nil {
		return "", Unavailable("無法備份家的設定")
	}
	if err := AtomicWrite(HomePath(home), buf.Bytes(), 0o600); err != nil {
		return "", Unavailable("無法寫入家的設定")
	}
	if _, err := LoadHome(home); err != nil {
		if rerr := AtomicWrite(HomePath(home), old, 0o600); rerr != nil {
			return backup, Internal("新設定不合格，而且還原失敗；舊檔在 " + backup)
		}
		return backup, err
	}
	return backup, nil
}

// FilesMentioning 在家裡的 roots（相對家的檔案或資料夾）找內容提到 word 的 .md／.toml 檔（word 前後不是英數字或減號），
// 跳過 archive／ 資料夾，回傳相對家的路徑（排序過）。只讀。
func FilesMentioning(home, word string, roots ...string) ([]string, error) {
	if strings.TrimSpace(word) == "" {
		return nil, nil
	}
	re, err := regexp.Compile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(word) + `($|[^A-Za-z0-9_-])`)
	if err != nil {
		return nil, Invalid("word", "搜尋字不正確")
	}
	seen := map[string]bool{}
	for _, root := range roots {
		start := filepath.Join(home, filepath.FromSlash(root))
		err := filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() {
				if d.Name() == "archive" {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if ext := filepath.Ext(path); ext != ".md" && ext != ".toml" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			if re.Match(data) {
				if rel, err := filepath.Rel(home, path); err == nil {
					seen[filepath.ToSlash(rel)] = true
				}
			}
			return nil
		})
		if err != nil {
			return nil, Unavailable("無法搜尋家裡的檔案")
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
