package rules

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// setLevels 改 settings/trust.toml 的權限等級（Q5）：def 非空就改 default_level；levels 逐位設定，Level 空字串＝拿掉單獨設定。
// 其他欄位（repo、approve、deny…）原樣保留，但註解不保留。改之前把舊檔備份到 runtime/backups/trust/，回傳備份的相對路徑
// （原本沒有檔案時是空字串）；寫完重讀驗證，不合格就還原並回錯誤。
func setLevels(home string, def sdk.PermissionLevel, levels []sdk.AgentPermission, now time.Time) (string, error) {
	if def != "" {
		if _, ok := platform.ParseLevel(string(def)); !ok {
			return "", platform.Invalid("default_level", "權限等級必須是 full、standard 或 readonly")
		}
	}
	for _, l := range levels {
		if strings.TrimSpace(l.Agent) == "" {
			return "", platform.Invalid("levels.agent", "請寫是哪一位秘書")
		}
		if _, ok := platform.ParseLevel(string(l.Level)); l.Level != "" && !ok {
			return "", platform.Invalid("levels.level", "權限等級必須是 full、standard 或 readonly")
		}
	}
	path := platform.TrustPath(home)
	old, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", platform.Unavailable("無法讀取權限設定，請檢查存取權限")
	}
	raw := map[string]any{}
	if len(old) > 0 {
		if _, err := toml.Decode(string(old), &raw); err != nil {
			return "", platform.Invalid("trust", "權限設定格式不正確")
		}
	}
	if _, ok := raw["schema"]; !ok {
		raw["schema"] = 1
	}
	if def != "" {
		raw["default_level"], _ = platform.ParseLevel(string(def))
	}
	agents, _ := raw["agent"].(map[string]any)
	if agents == nil {
		agents = map[string]any{}
	}
	for _, l := range levels {
		id := strings.TrimSpace(l.Agent)
		if l.Level == "" {
			delete(agents, id)
			continue
		}
		lv, _ := platform.ParseLevel(string(l.Level))
		table, _ := agents[id].(map[string]any)
		if table == nil {
			table = map[string]any{}
		}
		table["level"] = string(lv)
		agents[id] = table
	}
	if len(agents) == 0 {
		delete(raw, "agent")
	} else {
		raw["agent"] = agents
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(raw); err != nil {
		return "", platform.Internal("無法寫出權限設定")
	}
	backup := ""
	if len(old) > 0 {
		full := platform.RuntimeDir(home, "backups", "trust", now.Format("20060102-150405.000000000")+".toml")
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return "", platform.Unavailable("無法備份權限設定")
		}
		if err := platform.AtomicWrite(full, old, 0o600); err != nil {
			return "", platform.Unavailable("無法備份權限設定")
		}
		backup = homeRel(home, full)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", platform.Unavailable("無法寫入權限設定")
	}
	if err := platform.AtomicWrite(path, buf.Bytes(), 0o600); err != nil {
		return "", platform.Unavailable("無法寫入權限設定")
	}
	if _, err := platform.LoadHome(home); err != nil {
		if len(old) > 0 {
			_ = platform.AtomicWrite(path, old, 0o600)
		} else {
			_ = os.Remove(path)
		}
		return "", err
	}
	return backup, nil
}

// snapshotTrust 把權限來源存成 runtime/trust/snapshots/<時間>.json（evergenie trust snapshot），回傳相對家的路徑（第 2 版是完整路徑），
// 以及存之前最新的一份（沒有或壞掉時是 nil）。
func snapshotTrust(home string, now time.Time, sources []sdk.PermissionSource) (string, []sdk.PermissionSource, error) {
	dir := platform.RuntimeDir(home, "trust", "snapshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", nil, platform.Unavailable("無法建立快照資料夾")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", nil, platform.Unavailable("無法讀取快照資料夾")
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var prev []sdk.PermissionSource
	if len(names) > 0 {
		data, err := os.ReadFile(filepath.Join(dir, names[len(names)-1]))
		if err != nil {
			return "", nil, platform.Unavailable("無法讀取上一份快照")
		}
		var snap struct {
			Sources []sdk.PermissionSource `json:"sources"`
		}
		if json.Unmarshal(data, &snap) == nil {
			prev = snap.Sources
			if prev == nil {
				prev = []sdk.PermissionSource{}
			}
		}
	}
	b, err := json.MarshalIndent(map[string]any{"sources": sources}, "", "  ")
	if err != nil {
		return "", nil, platform.Internal("無法寫出快照")
	}
	name := now.Format("20060102-150405.000000000") + ".json"
	if err := platform.AtomicWrite(filepath.Join(dir, name), b, 0o600); err != nil {
		return "", nil, platform.Unavailable("無法寫入快照")
	}
	return homeRel(home, filepath.Join(dir, name)), prev, nil
}

// homeRel 回傳相對家的路徑；第 2 版的暫存在家外面（狀態資料夾），就回完整路徑。
func homeRel(home, full string) string {
	if r, err := filepath.Rel(home, full); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return r
	}
	return full
}
