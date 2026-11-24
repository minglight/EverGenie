package menu

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// fakeRecords 是 show_when 的 cap: 讀的紀錄（`<套件>.capabilities/main`）；fail 設了就每次都回錯。
type fakeRecords struct {
	mu    sync.Mutex
	recs  map[string]sdk.Record // kind + "/" + id
	fail  error
	reads int
}

func (f *fakeRecords) Get(_ context.Context, kind, id string) (sdk.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.fail != nil {
		return sdk.Record{}, f.fail
	}
	r, ok := f.recs[kind+"/"+id]
	if !ok {
		return sdk.Record{}, platform.NotFound("找不到")
	}
	return r, nil
}

func (f *fakeRecords) set(pkg string, supports []any, archived bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.recs[pkg+".capabilities/main"] = sdk.Record{Kind: pkg + ".capabilities", ID: "main", Fields: map[string]any{"supports": supports}, Archived: archived}
}

// capPackages：wardrobe 宣告了 capabilities 種類，能力由測試寫進 fakeRecords；guest 的角色分頁要 wardrobe 有 edit 才列，
// 另一個分頁沒寫 show_when（一直列）。
func capPackages() (wardrobe, guest sdk.Package) {
	wardrobe = demoPackage("wardrobe", sdk.TierOfficial)
	wardrobe.Menu = nil
	wardrobe.Pages = wardrobe.Pages[:1]
	wardrobe.Data = []sdk.DataKindDecl{{Kind: "capabilities", IDs: "given"}}
	guest = demoPackage("guest", sdk.TierMine)
	guest.Menu = nil
	guest.Pages = []sdk.PageDecl{
		{ID: "edit", Title: "修改", Component: "pages/card.tsx", Slot: "core.persona.tab", ShowWhen: "cap:wardrobe.edit"},
		{ID: "clothes", Title: "衣服", Component: "pages/card.tsx", Slot: "core.persona.tab"},
	}
	return wardrobe, guest
}

// 角色頁分頁「API 支援才顯示」（MNU-11、DEV-160 S4）：套件的能力紀錄沒有 edit，宣告 show_when = "cap:wardrobe.edit" 的分頁不在 get_pages；
// 有了就在；能力拿掉、紀錄封存、提供能力的套件關掉，又不在。沒寫 show_when 的分頁一直在。核心不認得套件，只照紀錄的 supports 判斷。
func TestSlotPageShowWhenCapability(t *testing.T) {
	e := newEnv(t)
	wardrobe, guest := capPackages()
	withCatalog(t, e, wardrobe, guest)
	listed := func() (edit, clothes bool) {
		got := pages(t, e)
		_, edit = got["guest.edit"]
		_, clothes = got["guest.clothes"]
		return
	}

	if edit, clothes := listed(); edit || !clothes {
		t.Fatalf("還沒有能力紀錄：修改＝%v（要 false）、衣服＝%v（要 true）", edit, clothes)
	}
	e.recs.set("wardrobe", []any{"mine"}, false)
	if edit, _ := listed(); edit {
		t.Fatal("能力只有 mine：修改分頁不該列")
	}
	e.recs.set("wardrobe", []any{"mine", "edit"}, false)
	edit, clothes := listed()
	if !edit || !clothes {
		t.Fatalf("能力有 edit：修改＝%v、衣服＝%v（都要 true）", edit, clothes)
	}
	if p := pages(t, e)["guest.edit"]; p.Slot != "core.persona.tab" || p.BundleURL == "" || p.Error != "" {
		t.Fatalf("列出來的修改分頁要能載入：%+v", p)
	}
	e.recs.set("wardrobe", []any{"mine", "edit"}, true)
	if edit, _ := listed(); edit {
		t.Fatal("能力紀錄封存了：修改分頁不該列")
	}

	// 提供能力的套件關掉了（紀錄還在）：當沒有。
	e.recs.set("wardrobe", []any{"mine", "edit"}, false)
	withCatalog(t, e, guest)
	if edit, clothes := listed(); edit || !clothes {
		t.Fatalf("wardrobe 關掉：修改＝%v（要 false）、衣服＝%v（要 true）", edit, clothes)
	}

	// 沒宣告 capabilities 種類的套件：不去讀紀錄，一律沒有。
	plain := demoPackage("wardrobe", sdk.TierOfficial)
	plain.Menu, plain.Pages = nil, plain.Pages[:1]
	withCatalog(t, e, plain, guest)
	before := e.recs.reads
	if edit, _ := listed(); edit {
		t.Fatal("沒宣告 capabilities 的套件不該給能力")
	}
	if e.recs.reads != before {
		t.Fatalf("沒宣告 capabilities 的套件不該去讀紀錄（多讀了 %d 次）", e.recs.reads-before)
	}
}

// 讀不到能力紀錄（不是找不到）、寫法不認得：都不列（fail closed）；同一次請求同一個套件的能力只讀一次。
func TestSlotPageShowWhenFailsClosed(t *testing.T) {
	e := newEnv(t)
	wardrobe, guest := capPackages()
	guest.Pages = append(guest.Pages,
		sdk.PageDecl{ID: "also", Title: "也要 edit", Component: "pages/card.tsx", Slot: "core.overview.cards", ShowWhen: "cap:wardrobe.edit"},
		sdk.PageDecl{ID: "weird", Title: "寫法怪", Component: "pages/card.tsx", Slot: "core.persona.tab", ShowWhen: "cap:wardrobe"},
		sdk.PageDecl{ID: "agent", Title: "看秘書", Component: "pages/card.tsx", Slot: "core.persona.tab", ShowWhen: "agent:code:enabled"},
	)
	withCatalog(t, e, wardrobe, guest)
	e.recs.set("wardrobe", []any{"edit"}, false)
	before := e.recs.reads
	got := pages(t, e)
	for id, want := range map[string]bool{"guest.edit": true, "guest.also": true, "guest.weird": false, "guest.agent": true, "guest.clothes": true} {
		if _, ok := got[id]; ok != want {
			t.Fatalf("%s 列出來＝%v，要 %v", id, ok, want)
		}
	}
	if n := e.recs.reads - before; n != 1 {
		t.Fatalf("同一次請求 wardrobe 的能力要只讀一次，讀了 %d 次", n)
	}

	e.recs.fail = errors.New("磁碟壞了")
	got = pages(t, e)
	if _, ok := got["guest.edit"]; ok {
		t.Fatal("讀不到能力紀錄：修改分頁不該列")
	}
	if _, ok := got["guest.clothes"]; !ok {
		t.Fatal("沒寫 show_when 的分頁不受影響")
	}
}

// 選單項目也認得 cap:（跟 slot 頁面同一個判斷）。
func TestMenuShowWhenCapability(t *testing.T) {
	e := newEnv(t)
	wardrobe, _ := capPackages()
	withCatalog(t, e, wardrobe, plainPackage("gated", sdk.TierMine,
		sdk.MenuItemDecl{ID: "edit", Title: "要 edit", ShowWhen: "cap:wardrobe.edit", Page: "home"},
		sdk.MenuItemDecl{ID: "always", Title: "一直在", Page: "home"}))
	has := func() bool { return strings.Contains(","+strings.Join(poolIDs(menu(t, e)), ",")+",", ",gated.edit,") }
	if has() {
		t.Fatal("沒有能力紀錄：選單項目不該列")
	}
	e.recs.set("wardrobe", []any{"edit"}, false)
	if !has() {
		t.Fatal("有 edit：選單項目要列")
	}
}

func TestParseCap(t *testing.T) {
	for in, want := range map[string][2]string{
		"cap:closet.edit":    {"closet", "edit"},
		"cap:my-pkg.mine":    {"my-pkg", "mine"},
		"cap:closet":         {},
		"cap:.edit":          {},
		"cap:closet.":        {},
		"cap:closet.a.b":     {},
		"cap:closet.edit:x":  {},
		"agent:code:enabled": {},
	} {
		pkg, name, ok := parseCap(in)
		if ok != (want[0] != "") || pkg != want[0] || name != want[1] {
			t.Fatalf("parseCap(%q) = %q %q %v，要 %q", in, pkg, name, ok, want)
		}
	}
}
