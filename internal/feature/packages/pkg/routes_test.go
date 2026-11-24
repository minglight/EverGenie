package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// TestPageCannotShadowCoreRoute：頁面的 route 保留未用；寫了會蓋住核心網址（"/"、/tasks、/p/…、/settings/…）就整包 INVALID（field contributes.page，
// 白話說明網址固定是 /p/<套件>/<頁面>）；不撞的寫法（官方套件現有的 /body 之類）照常讀得進來。被拒的只有它自己，別的套件照常。
// Guards: REQ-352
func TestPageCannotShadowCoreRoute(t *testing.T) {
	page := func(route string) string {
		return dataPkg("x", "1.0.0", "[[contributes.page]]\nid = \"main\"\nroute = \""+route+"\"\n")
	}
	for _, route := range []string{"/", "/tasks", "/tasks/T-1", "/p/other/main", "/settings/menu", "chat", "/API", "/pkg-files/x/a.png", "//evil"} {
		t.Run("reject "+route, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "x")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.toml"), []byte(page(route)), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := ParseManifest(dir, sdk.TierMine)
			pe, ok := err.(*platform.Err)
			if !ok || pe.Code != sdk.ErrorCodeInvalid || pe.Field != "contributes.page" || !strings.Contains(pe.Message, "核心網址") || !strings.Contains(pe.Message, "/p/x/main") {
				t.Fatalf("route %q 要 INVALID 並說明網址固定是 /p/x/main：%#v", route, err)
			}
		})
	}
	for _, route := range []string{"", "/body", "/x-home", "/garden/today", "/pages"} {
		dir := filepath.Join(t.TempDir(), "x")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "package.toml"), []byte(page(route)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseManifest(dir, sdk.TierMine); err != nil {
			t.Fatalf("route %q 不撞核心網址，要讀得進來：%v", route, err)
		}
	}

	// 放進家裡：撞核心網址的只有它自己被拒，旁邊的套件照常 running。
	h := newHarness(t)
	h.write(sdk.TierMine, "thief", dataPkg("thief", "1.0.0", "[[contributes.page]]\nid = \"main\"\nroute = \"/tasks\"\n"), nil)
	h.write(sdk.TierMine, "garden", dataPkg("garden", "1.0.0", "[[contributes.page]]\nid = \"main\"\n"), nil)
	h.start()
	got := map[string]sdk.Package{}
	for _, p := range h.list() {
		got[p.ID] = p
	}
	if p := got["thief"]; p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, "核心網址") {
		t.Fatalf("thief 要被拒：%+v", p)
	}
	if p := got["garden"]; p.State != sdk.PackageStateRunning {
		t.Fatalf("garden 要照常：%+v", p)
	}
}

// TestReservedIDsCoverCoreRoutes：核心網址的第一段（CoreRouteSegments，manifest 的 route 檢查用的那份）全部在 ReservedIDs 裡，
// 而且 ReservedIDs 就是從它組出來的（同一份清單，不會兩邊各改各的）。
// Guards: REQ-309, REQ-352
func TestReservedIDsCoverCoreRoutes(t *testing.T) {
	reservedSet := map[string]bool{}
	for _, id := range ReservedIDs {
		reservedSet[id] = true
	}
	for i, seg := range CoreRouteSegments {
		if !reservedSet[seg] || ReservedIDs[i] != seg {
			t.Fatalf("核心網址第一段「%s」要是 ReservedIDs 的第一組", seg)
		}
		if !shadowsCoreRoute("/"+seg) || !shadowsCoreRoute("/"+seg+"/x") {
			t.Fatalf("/%s 要算蓋住核心網址", seg)
		}
	}
	if len(ReservedIDs) <= len(CoreRouteSegments) {
		t.Fatal("ReservedIDs 還要有核心、套件層、預設職位與官方套件的名字")
	}
}
