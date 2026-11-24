package persona

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
)

// layoutV2 把家改成佈局第 2 版（DEV-145）：home.toml 加 [layout]，官方角色（personas/ 底下 mine/ 與 _ 開頭以外的）搬到狀態資料夾。
func layoutV2(t *testing.T, home string) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	f, err := os.OpenFile(filepath.Join(home, "home.toml"), os.O_APPEND|os.O_WRONLY, 0)
	must(t, err)
	_, err = fmt.Fprintf(f, "\n[layout]\nversion = 2\nstate = %q\n", state)
	must(t, err)
	f.Close()
	src, dst := filepath.Join(home, "personas"), filepath.Join(state, "personas")
	must(t, os.MkdirAll(dst, 0o755))
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		if e.Name() == "mine" || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		must(t, os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())))
	}
	return state
}

// TestLibraryLayoutV2：第 2 版的家，官方卡在狀態資料夾的 personas/，角色庫列得出、讀得到；我的卡照樣在家裡的 personas/mine/；
// 家裡 personas/ 頂層的 .md 不再算官方卡；改卡的備份放狀態資料夾的暫存。
func TestLibraryLayoutV2(t *testing.T) {
	_, lib, h, _ := fixture(t)
	state := layoutV2(t, h.Dir)
	writeCard(t, h, "personas/mine/mina.md", "---\nschema: 1\nname: Mina\n---\n我的角色。\n")
	writeCard(t, h, "personas/stray.md", "---\nschema: 1\nname: Stray\n---\n第 2 版家裡頂層的卡不算官方。\n")

	got := map[string]string{}
	for _, c := range lib.cards() {
		got[c.slug] = c.source
	}
	if got["athena"] != personaref.SourceOfficial || got["new"] != personaref.SourceOfficial || got["mina"] != personaref.SourceMine {
		t.Fatalf("角色庫：%v", got)
	}
	if _, ok := got["stray"]; ok {
		t.Fatalf("家裡頂層的卡不該列：%v", got)
	}
	p, err := cardRecord(lib, "athena")
	must(t, err)
	if p.Fields["name"] != "Athena" {
		t.Fatal(p.Fields)
	}
	if c := cardOf(t, lib, "athena"); c.Name != "Athena" || c.Source != personaref.SourceOfficial {
		t.Fatalf("show：%+v", c)
	}
	if n, ok := lib.introNotice("athena"); !ok || n == nil {
		t.Fatal("新角色的招呼要讀得到狀態資料夾的官方卡")
	}

	c, ok := lib.readCard(personaref.SourceMine, "mina")
	if !ok {
		t.Fatal("讀不到我的卡")
	}
	rel, err := lib.backupCard(c)
	must(t, err)
	if !strings.HasPrefix(rel, filepath.Join(state, "runtime", "backups", "personas")) {
		t.Fatalf("備份要在狀態資料夾的暫存：%s", rel)
	}
}
