package menu

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// fakeCatalog 是測試用的 PackageCatalog：套件清單由測試給，資料夾是複製出來的 fixture。
type fakeCatalog struct {
	pkgs []sdk.Package
	dirs map[string]string
}

func (c *fakeCatalog) Packages() []sdk.Package { return c.pkgs }
func (c *fakeCatalog) PackageDir(id string) (string, bool) {
	d, ok := c.dirs[id]
	return d, ok
}

// copyFixturePackage 把 testdata/packages/<name> 複製成 <root>/<id>，回新資料夾（測試才改得動它）。
func copyFixturePackage(t *testing.T, name, root, id string) string {
	t.Helper()
	src := filepath.Join("testdata", "packages", name)
	dst := filepath.Join(root, id)
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// demoPackage 是 fixture 套件 demo 的宣告（頁面檔在 testdata/packages/demo/pages/）。
func demoPackage(id string, tier sdk.Tier) sdk.Package {
	return sdk.Package{
		ID: id, Name: id, Tier: tier, Kind: sdk.PackageKindData, State: sdk.PackageStateRunning, Enabled: true,
		Pages: []sdk.PageDecl{
			{ID: "home", Title: "示範頁", Component: "pages/home.tsx"},
			{ID: "card", Title: "示範卡片", Component: "pages/card.tsx", Slot: "core.overview.cards"},
			{ID: "bad", Title: "壞掉的頁面", Component: "pages/bad.tsx"},
		},
		Menu: []sdk.MenuItemDecl{{ID: "home", Title: "示範", Icon: "puzzle", Page: "home"}},
	}
}

// withCatalog 把套件接進 env 的套件目錄；回目錄（資料夾對照）。
func withCatalog(t *testing.T, e *env, pkgs ...sdk.Package) *fakeCatalog {
	t.Helper()
	root := t.TempDir()
	e.cat.pkgs, e.cat.dirs = pkgs, map[string]string{}
	for _, p := range pkgs {
		e.cat.dirs[p.ID] = copyFixturePackage(t, "demo", root, p.ID)
	}
	return e.cat
}

// touch 改檔案內容並把修改時間往後推，免得同一個時間戳讓指紋沒變。
func touch(t *testing.T, path, data string, at time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}
