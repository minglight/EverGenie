package personaexport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// layoutV2 把家改成佈局第 2 版（DEV-145）：home.toml 加 [layout]，官方角色（personas/ 底下 mine/ 與 _ 開頭以外的）搬到狀態資料夾。
func layoutV2(t *testing.T, home string) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	f, err := os.OpenFile(filepath.Join(home, "home.toml"), os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(f, "\n[layout]\nversion = 2\nstate = %q\n", state); err != nil {
		t.Fatal(err)
	}
	f.Close()
	src, dst := filepath.Join(home, "personas"), filepath.Join(state, "personas")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		if e.Name() == "mine" || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		if err := os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

// TestExportImportLayoutV2：第 2 版的家匯出跟第 1 版一樣的檔；匯入第 2 版的家時官方位置的檔寫到狀態資料夾、我的寫到家裡的 personas/mine/，
// 暫存與備份在狀態資料夾。
func TestExportImportLayoutV2(t *testing.T) {
	a1, s1 := homeA(t)
	want := strings.Join(zipNames(t, exportTo(t, a1, s1, "luna", "myhelper")), "\n")
	a2, s2 := homeA(t)
	layoutV2(t, a2)
	data := exportTo(t, a2, s2, "luna", "myhelper")
	if got := strings.Join(zipNames(t, data), "\n"); got != want {
		t.Fatalf("第 2 版匯出的檔要跟第 1 版一樣：\n%s\n---\n%s", got, want)
	}

	b := t.TempDir()
	state := layoutV2(t, b)
	sb := &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}
	if _, err := importBytes(b, sb, data, false, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"luna.md", "avatars/luna.png", "avatars/full/luna@sea.png", "wardrobe/luna/look.md", "catalog/luna.md", "avatars/hidden/luna@sea.png"} {
		if !fileExists(filepath.Join(state, "personas", filepath.FromSlash(f))) || fileExists(filepath.Join(b, "personas", filepath.FromSlash(f))) {
			t.Errorf("官方位置的 %s 要在狀態資料夾", f)
		}
	}
	for _, f := range []string{"mine/myhelper.md", "mine/avatars/myhelper.png", "mine/avatars/luna@my-3.png"} {
		if !fileExists(filepath.Join(b, "personas", filepath.FromSlash(f))) {
			t.Errorf("我的 %s 要在家裡", f)
		}
	}
	if fileExists(filepath.Join(b, "runtime")) {
		t.Error("第 2 版不該在家裡建 runtime/")
	}
	// 同代號一邊官方、一邊我的：第 2 版也擋（官方卡在狀態資料夾）。
	c := t.TempDir()
	layoutV2(t, c)
	writeFile(t, filepath.Join(c, "personas", "mine", "luna.md"), "---\nschema: 1\nname: 我的露娜\n---\n")
	if _, err := importBytes(c, &memStore{kinds: closetKinds(), recs: map[string]sdk.Record{}}, data, true, false); err == nil {
		t.Fatal("同代號在我的位置要擋")
	}
}
