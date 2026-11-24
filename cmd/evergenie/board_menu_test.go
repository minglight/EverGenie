package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/feature/packages/pkg"
	"github.com/minglight/evergenie/sdk"
)

// 看板的整條路（packages 建套件 → base/app/board 的套件前端編譯 → menu 的頁面與選單 → 看板外殼轉送與送 bundle），
// 要真的組起來才測得到（業務模組之間不互相 import），所以放在這裡。

func boardPost[T any](t *testing.T, board http.Handler, path string) T {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	board.ServeHTTP(w, req)
	var out T
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%s：%d %s", path, w.Code, w.Body.String())
	}
	return out
}

type boardPages struct {
	Pages []struct {
		ID, Route, Package, Slot, Error string
		BundleURL                       string `json:"bundle_url"`
	}
}

type boardMenu struct {
	Menu struct {
		Pool []struct{ ID, Title, Route string }
	}
}

// packages new garden 之後，看板的頁面清單有 garden.index（路由 /p/garden/index、編得過、bundle 取得到），選單池有 garden.index。
// Guards: REQ-310
func TestPackagesNewPageRoutes(t *testing.T) {
	_, sock, board := startFixtureWebWith(t, nil)
	hgOK(t, sock, "packages", "new", "garden", "--name", "花園")
	var page *struct {
		ID, Route, Package, Slot, Error string
		BundleURL                       string `json:"bundle_url"`
	}
	pages := boardPost[boardPages](t, board, "/api/web/get_pages")
	for i := range pages.Pages {
		if pages.Pages[i].ID == "garden.index" {
			page = &pages.Pages[i]
		}
	}
	if page == nil || page.Error != "" || page.Route != "/p/garden/index" || page.Package != "garden" || page.Slot != "" || !strings.HasPrefix(page.BundleURL, "/pkg-bundles/") {
		t.Fatalf("garden.index = %+v", page)
	}
	w := httptest.NewRecorder()
	board.ServeHTTP(w, httptest.NewRequest(http.MethodGet, page.BundleURL, nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "garden-index") {
		t.Fatalf("bundle → %d %.200s", w.Code, w.Body.String())
	}
	found := false
	for _, it := range boardPost[boardMenu](t, board, "/api/web/get_menu").Menu.Pool {
		if it.ID == "garden.index" {
			found = true
			if it.Route != "/p/garden/index" || !strings.Contains(it.Title, "花園") {
				t.Fatalf("選單項目：%+v", it)
			}
		}
	}
	if !found {
		t.Fatal("選單池沒有 garden.index")
	}
}

// 核心看板的每一個路由第一段（選單裡的核心項目、看板外殼直接處理的網址前綴）都在保留字清單裡，新套件的 id 不可能蓋住它。
// Guards: REQ-309, REQ-312
func TestPackagesNewReservedCoversCoreRoutes(t *testing.T) {
	_, _, board := startFixtureWebWith(t, nil)
	reserved := map[string]bool{}
	for _, id := range pkg.ReservedIDs {
		reserved[id] = true
	}
	coreSeg := map[string]bool{}
	for _, s := range pkg.CoreRouteSegments {
		coreSeg[s] = true
	}
	segs := []string{"api", "avatars", "share", "pkg-bundles", "pkg-files", "admin", "p"} // 看板外殼直接處理的前綴＋套件頁面 /p/
	for _, it := range boardPost[boardMenu](t, board, "/api/web/get_menu").Menu.Pool {
		if strings.HasPrefix(it.ID, "core.") && it.Route != "/" {
			seg := strings.Split(strings.TrimPrefix(it.Route, "/"), "/")[0]
			segs = append(segs, seg)
			// 核心選單項目的網址第一段也要在 CoreRouteSegments（套件頁面的 route 檢查用的那份），例：介紹 /personas（MNU-9）。
			if !coreSeg[seg] {
				t.Errorf("核心選單項目 %s 的網址第一段「%s」要列進 pkg.CoreRouteSegments", it.ID, seg)
			}
		}
	}
	for _, s := range segs {
		if !reserved[s] {
			t.Errorf("核心路由第一段「%s」要列進 pkg.ReservedIDs 與 user-features.md", s)
		}
	}
}

// 自己的一個套件 manifest 壞掉（rejected）時，只有它不見：另一個自己的套件、選單、頁面清單照常，
// 選單照樣回得來、核心項目都在（PKC-8、PKG-9）。
// Guards: REQ-353
func TestBrokenUserFeatureIsolated(t *testing.T) {
	dir, sock, board := startFixtureWebWith(t, nil)
	hgOK(t, sock, "packages", "new", "garden")
	hgOK(t, sock, "packages", "new", "pond")
	if err := os.WriteFile(filepath.Join(dir, "packages", "mine", "pond", "package.toml"), []byte("schema = = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hgOK(t, sock, "packages", "reload")
	list, err := sdk.NewPackageClient(sdk.Dial(sock)).List(t.Context(), &sdk.PackageListRequest{})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]sdk.PackageState{}
	for _, p := range list.Packages {
		states[p.ID] = p.State
	}
	if states["pond"] != sdk.PackageStateRejected || states["garden"] != sdk.PackageStateRunning {
		t.Fatalf("只有壞掉的被拒：%v", states)
	}
	var ids []string
	var mine []string
	got := boardPost[sdk.WebGetMenuResponse](t, board, "/api/web/get_menu").Menu
	for _, it := range got.Pool {
		ids = append(ids, it.ID)
	}
	for _, e := range got.Tree {
		if e.ID == "mine" {
			for _, c := range e.Children {
				mine = append(mine, c.ID)
			}
		}
	}
	pool := "," + strings.Join(ids, ",") + ","
	if strings.Contains(pool, ",pond.") || !strings.Contains(pool, ",garden.index,") || !strings.Contains(pool, ",core.settings,") {
		t.Fatalf("選單 pool：%s", pool)
	}
	if strings.Join(mine, ",") != "garden.index" {
		t.Fatalf("「我的套件」只剩好的那個：%v", mine)
	}
	pages := map[string]sdk.PageInfo{}
	for _, p := range boardPost[sdk.WebGetPagesResponse](t, board, "/api/web/get_pages").Pages {
		pages[p.ID] = p
	}
	if _, ok := pages["pond.index"]; ok {
		t.Fatalf("壞掉的套件不在頁面清單：%v", pages["pond.index"])
	}
	if p := pages["garden.index"]; p.Error != "" || p.BundleURL == "" || p.Tier != sdk.TierMine {
		t.Fatalf("好的套件照常編得過：%+v", p)
	}
}
