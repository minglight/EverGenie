package host

import (
	"strings"
	"testing"
)

// TestProcessEnvDropsLayout：背景 serve 不帶別的家的佈局變數（EVERGENIE_STATE、EVERGENIE_MEDIA、EVERGENIE_OFFICIAL_PERSONAS，DEV-145），由 serve 自己照 home.toml 算；
// 跟著程式的公開卡資料夾（EVERGENIE_BUNDLED_PERSONAS，PRS-25）也不帶，serve 開套件時自己給。
func TestProcessEnvDropsLayout(t *testing.T) {
	t.Setenv("EVERGENIE_STATE", "/other/state")
	t.Setenv("EVERGENIE_MEDIA", "/other/media")
	t.Setenv("EVERGENIE_OFFICIAL_PERSONAS", "/other/personas")
	t.Setenv("EVERGENIE_BUNDLED_PERSONAS", "/other/cards")
	s := Service{Spec: ServiceSpec{Home: t.TempDir(), Exe: "/x/evergenie"}}
	for _, v := range s.processEnv() {
		for _, k := range []string{"EVERGENIE_STATE=", "EVERGENIE_MEDIA=", "EVERGENIE_OFFICIAL_PERSONAS=", "EVERGENIE_BUNDLED_PERSONAS="} {
			if strings.HasPrefix(v, k) {
				t.Errorf("不該帶 %s", v)
			}
		}
	}
}
