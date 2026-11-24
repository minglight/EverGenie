package platform

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// ParseLevel 把文字轉成權限等級；不認得回 false。大小寫與前後空白不計。
func ParseLevel(s string) (sdk.PermissionLevel, bool) {
	switch l := sdk.PermissionLevel(strings.ToLower(strings.TrimSpace(s))); l {
	case sdk.PermissionLevelFull, sdk.PermissionLevelStandard, sdk.PermissionLevelReadOnly:
		return l, true
	}
	return "", false
}

func (t Trust) validateLevels() error {
	if t.DefaultLevel != "" {
		if _, ok := ParseLevel(t.DefaultLevel); !ok {
			return Invalid("default_level", "權限等級必須是 full、standard 或 readonly")
		}
	}
	ids := make([]string, 0, len(t.Agents))
	for id := range t.Agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return Invalid("agent", "職位名稱不可空白")
		}
		if _, ok := ParseLevel(t.Agents[id].Level); !ok {
			return Invalid("agent."+id+".level", "權限等級必須是 full、standard 或 readonly")
		}
	}
	return nil
}

// TrustPath 是權限設定檔的位置。
func TrustPath(home string) string { return filepath.Join(home, "settings", "trust.toml") }
