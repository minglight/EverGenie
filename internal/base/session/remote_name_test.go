package session

import (
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 遠端存取名稱（手機 App 裡的對話名）是 <角色名>-<owner>-<machine>，開發家（env = dev）再加 @dev；remote_suffix 已退休，寫了也不影響。
func TestRemoteAccessNameUsesHomeLabel(t *testing.T) {
	cases := []struct {
		env, want string
	}{
		{platform.EnvProd, "雅典娜-amy-home"},
		{platform.EnvDev, "雅典娜-amy-home@dev"},
	}
	for _, c := range cases {
		h := &platform.Home{}
		h.Runtime.Env, h.Runtime.Owner, h.Runtime.Machine = c.env, "amy", "home"
		if got := remoteAccessName("雅典娜", h.Label()); got != c.want {
			t.Errorf("env=%s：得到 %q，要 %q", c.env, got, c.want)
		}
	}
	if got := remoteAccessName("雅典娜", ""); got != "雅典娜" {
		t.Errorf("沒有標籤只用人名：%q", got)
	}
}
