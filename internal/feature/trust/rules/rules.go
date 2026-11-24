package rules

import (
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// BuiltinLevel 是沒有任何設定時每位秘書拿到的等級：全部最高權限（作者 2026-10-25 定案）。
const BuiltinLevel = sdk.PermissionLevelFull

// LevelFor 回傳某個職位的權限等級：先看 [agent.<id>]，其次 default_level，最後是內建預設
// （BuiltinLevel）。永遠回傳一個有效的等級（載入時已驗證）。
func LevelFor(t platform.Trust, agent string) sdk.PermissionLevel {
	l, _ := LevelSource(t, agent)
	return l
}

// LevelSource 同 LevelFor，另外回傳等級從哪裡來：agent（單獨設定）、default_level、builtin。
func LevelSource(t platform.Trust, agent string) (sdk.PermissionLevel, string) {
	if a, ok := t.Agents[agent]; ok {
		if l, ok := platform.ParseLevel(a.Level); ok {
			return l, "agent"
		}
	}
	if l, ok := platform.ParseLevel(t.DefaultLevel); ok {
		return l, "default_level"
	}
	return BuiltinLevel, "builtin"
}

// DefaultFor 是沒有單獨設定的秘書拿到的等級。
func DefaultFor(t platform.Trust) sdk.PermissionLevel {
	if l, ok := platform.ParseLevel(t.DefaultLevel); ok {
		return l
	}
	return BuiltinLevel
}

// Policy 把 settings/trust.toml 換成 EverGenie 自己的權限詞彙（engine 產生設定檔、看板「權限」分頁共用）。
func Policy(t platform.Trust) *sdk.PermissionPolicy {
	actions := func(table map[string]any) []string {
		out := []string{}
		switch values := table["actions"].(type) {
		case []string:
			out = append(out, values...)
		case []any:
			for _, v := range values {
				out = append(out, v.(string))
			}
		}
		return out
	}
	p := &sdk.PermissionPolicy{Mode: sdk.PermissionModeAsk, Allow: actions(t.Approve), Deny: actions(t.Deny), Repos: []sdk.RepoPolicy{}}
	for _, r := range t.Repos {
		p.Repos = append(p.Repos, sdk.RepoPolicy{Path: r.Path, Remote: r.Remote, Push: *r.Push, Writers: r.Writers})
	}
	return p
}
