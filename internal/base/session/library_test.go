package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/agent/personafixture"
	"github.com/minglight/evergenie/sdk"
)

// testAgents 是 session 看到的名冊：真的名冊加測試角色庫（registry 的 sessionAgents 接的是業務模組 personas；底層的測試不 import 業務模組）。
// 名冊讀人設卡與說話層經角色庫，測口吻要的 ShowPersona、StyleInfo 也由角色庫來。
type testAgents struct {
	*agent.Module
	lib *personafixture.Library
}

func newTestAgents(a *agent.Module, home string) testAgents {
	lib := personafixture.New(home)
	a.SetPersonaSource(lib)
	return testAgents{a, lib}
}

func (t testAgents) ShowPersona(ctx context.Context, slug string) (sdk.PersonaCard, string, string, error) {
	return t.lib.ShowPersona(ctx, slug)
}

func (t testAgents) OfficialCardFile(slug string) (string, bool) { return t.lib.OfficialCardFile(slug) }

func (t testAgents) StyleInfo(ctx context.Context) (string, string, bool, []sdk.PersonaStyleLevel, error) {
	return t.lib.StyleInfo(ctx)
}

// TestToneCardPathBundled：測口吻報告上的卡片位置由角色庫的找卡入口給（官方資料夾先、沒有才是跟著程式的那張，PRS-25）：
// 在家裡寫相對路徑，跟著程式的寫絕對路徑；主人的寫 personas/mine/。測試角色庫接了跟著程式的資料夾也讀得到那張卡。
func TestToneCardPathBundled(t *testing.T) {
	home, bundled := t.TempDir(), t.TempDir()
	must(t, os.MkdirAll(filepath.Join(home, "personas"), 0o700))
	must(t, os.WriteFile(filepath.Join(home, "personas", "test.md"), []byte("---\nname: Person\n---\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(bundled, "kaguya.md"), []byte("---\nname: 輝夜\n---\n"), 0o600))
	lib := personafixture.New(home)
	lib.Bundled = bundled
	for _, c := range []struct {
		card sdk.PersonaCard
		want string
	}{
		{sdk.PersonaCard{Slug: "test", Source: "official"}, "personas/test.md"},
		{sdk.PersonaCard{Slug: "kaguya", Source: "official"}, filepath.Join(bundled, "kaguya.md")},
		{sdk.PersonaCard{Slug: "my_x", Source: "mine"}, "personas/mine/my_x.md"},
	} {
		if got := toneCardPath(home, c.card, lib.OfficialCardFile); got != c.want {
			t.Fatalf("%s：%q，要 %q", c.card.Slug, got, c.want)
		}
	}
	card, _, _, err := lib.ShowPersona(context.Background(), "kaguya")
	if err != nil || card.Name != "輝夜" || card.Source != "official" {
		t.Fatalf("測試角色庫讀跟著程式的卡：%+v %v", card, err)
	}
}
