package pkg

import (
	"context"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-118
func TestModuleShape(t *testing.T) {
	m := New(Deps{})
	if got := m.Name(); got != "pkg" {
		t.Fatalf("Name = %q", got)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestPackageCatalog：web 看板的 PackageCatalog 視圖（P6-2a）：Packages() 跟 List 同一份（含 rejected），
// PackageDir 只給有效套件的資料夾。
// Guards: REQ-91
func TestPackageCatalog(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierMine, "good", dataPkg("good", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "broken", "schema = = 1\n", nil)
	if got := h.m.Packages(); got != nil {
		t.Fatalf("還沒啟動不該有套件：%+v", got)
	}
	h.start()
	got := h.m.Packages()
	if len(got) != len(h.list()) || len(got) != 2 {
		t.Fatalf("Packages() 要跟 List 一樣（含 rejected）：%+v", got)
	}
	state := map[string]sdk.PackageState{}
	for _, p := range got {
		state[p.ID] = p.State
	}
	if state["good"] != sdk.PackageStateRunning || state["broken"] != sdk.PackageStateRejected {
		t.Fatalf("狀態 = %v", state)
	}
	if d, ok := h.m.PackageDir("good"); !ok || d != dir {
		t.Fatalf("PackageDir(good) = %q %v，要 %q", d, ok, dir)
	}
	if d, ok := h.m.PackageDir("broken"); ok {
		t.Fatalf("被拒的套件不該有資料夾：%q", d)
	}
	if _, ok := h.m.PackageDir("nope"); ok {
		t.Fatal("不存在的套件不該有資料夾")
	}
}
