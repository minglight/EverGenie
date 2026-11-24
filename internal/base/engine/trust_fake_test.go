package engine

import (
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// testPolicy 是測試用的信任政策來源（registry 正式接的是 trust 業務模組的 Policy；底層的測試不 import 業務模組）。
// 只把 settings/trust.toml 的 approve／deny 動作換成權限詞彙，夠這裡的測試用。
func testPolicy(t platform.Trust) *sdk.PermissionPolicy {
	actions := func(table map[string]any) []string {
		out := []string{}
		switch values := table["actions"].(type) {
		case []string:
			out = append(out, values...)
		case []any:
			for _, v := range values {
				if s, ok := v.(string); ok {
					out = append(out, s)
				}
			}
		}
		return out
	}
	return &sdk.PermissionPolicy{Mode: sdk.PermissionModeAsk, Allow: actions(t.Approve), Deny: actions(t.Deny), Repos: []sdk.RepoPolicy{}}
}
