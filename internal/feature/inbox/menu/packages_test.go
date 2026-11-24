package menu

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/sdk"
)

// plainPackage 是只有選單項目的小套件（頁面是 demo 的 home）。
func plainPackage(id string, tier sdk.Tier, items ...sdk.MenuItemDecl) sdk.Package {
	p := demoPackage(id, tier)
	p.Pages = p.Pages[:1]
	p.Menu = items
	return p
}

func poolIDs(m *sdk.Menu) []string {
	var out []string
	for _, it := range m.Pool {
		out = append(out, it.ID)
	}
	return out
}

func treeOf(m *sdk.Menu) map[string][]string {
	out := map[string][]string{}
	for _, g := range m.Tree {
		if len(g.Children) == 0 {
			out[""] = append(out[""], g.ID)
			continue
		}
		out[g.ID] = ids(g.Children)
	}
	return out
}

// Guards: REQ-105, REQ-106
func TestPackageMenuItemsInPool(t *testing.T) {
	e := newEnv(t)
	off := plainPackage("off", sdk.TierMine, sdk.MenuItemDecl{ID: "x", Title: "停用的", Page: "home"})
	off.Enabled, off.State = false, sdk.PackageStateStopped
	rejected := plainPackage("broken", sdk.TierInstalled, sdk.MenuItemDecl{ID: "x", Title: "被拒的", Page: "home"})
	rejected.State, rejected.Error = sdk.PackageStateRejected, "需要套件「z」"
	failed := plainPackage("crashy", sdk.TierMine, sdk.MenuItemDecl{ID: "x", Title: "行程掛了", Icon: "no-such-icon", Page: "home"})
	failed.State = sdk.PackageStateFailed // 附程式的套件行程掛了，頁面照樣顯示
	withCatalog(t, e,
		plainPackage("plain", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "未分類的", Icon: "puzzle", Page: "home", Order: 5}),
		plainPackage("hinted", sdk.TierMine,
			sdk.MenuItemDecl{ID: "w", Title: "工作的", GroupHint: "work", Page: "home", Order: 2},
			sdk.MenuItemDecl{ID: "d", Title: "日常的", GroupHint: "daily", Page: "home", Order: 1},
			sdk.MenuItemDecl{ID: "d0", Title: "日常的先", GroupHint: "daily", Page: "home", Order: 0},
			sdk.MenuItemDecl{ID: "z", Title: "新群組 z", GroupHint: "zoo", Page: "home", Order: 1},
			sdk.MenuItemDecl{ID: "f", Title: "新群組 f", GroupHint: "fit", Page: "home", Order: 1},
			// 指到掛在 slot 的頁面：不是頁面，略過。
			sdk.MenuItemDecl{ID: "slotted", Title: "slot 頁", Page: "card"}),
		off, rejected, failed)
	// hinted 要有 card 頁（slot 頁）才測得到「略過」。
	e.cat.pkgs[1].Pages = append(e.cat.pkgs[1].Pages, sdk.PageDecl{ID: "card", Title: "卡", Component: "pages/card.tsx", Slot: "core.overview.cards"})

	got := menu(t, e)
	pool := poolIDs(got)
	for _, id := range []string{"off.x", "broken.x", "hinted.slotted"} {
		for _, p := range pool {
			if p == id {
				t.Fatalf("%s 不該在 pool：%v", id, pool)
			}
		}
	}
	idx := map[string]int{}
	for i, id := range pool {
		idx[id] = i
	}
	for _, id := range []string{"plain.home", "hinted.w", "hinted.d", "hinted.d0", "hinted.z", "hinted.f", "crashy.x"} {
		if _, ok := idx[id]; !ok {
			t.Fatalf("%s 要在 pool：%v", id, pool)
		}
	}
	if idx["hinted.d0"] > idx["hinted.d"] || idx["hinted.d"] > idx["hinted.w"] || idx["hinted.w"] > idx["plain.home"] {
		t.Fatalf("套件項目要依 order 再依 id 排：%v", pool)
	}
	for _, it := range got.Pool {
		if it.ID == "plain.home" && (it.Route != "/p/plain/home" || it.Title != "未分類的" || it.Icon != "puzzle" || it.Badge != 0) {
			t.Fatalf("plain.home = %+v", it)
		}
	}
	tree := treeOf(got)
	if strings.Join(tree["daily"], ",") != "core.overview,core.chat,core.reminders,core.messages,core.tasks,hinted.d0,hinted.d,core.personas" {
		t.Fatalf("daily = %v", tree["daily"])
	}
	if strings.Join(tree["work"], ",") != "hinted.w" {
		t.Fatalf("work = %v", tree["work"])
	}
	// 其他 hint：在 work 後面、core.settings 前面各自新增群組（依 hint 排序）；自己的套件沒有 hint 的進「我的套件」（REQ-349；官方的進「未分類」見 TestMenuTier）。
	if strings.Join(tree["fit"], ",") != "hinted.f" || strings.Join(tree["zoo"], ",") != "hinted.z" {
		t.Fatalf("新群組：%v", tree)
	}
	if strings.Join(tree["mine"], ",") != "crashy.x,plain.home" || len(tree["unplaced"]) != 0 {
		t.Fatalf("我的套件 = %v，未分類 = %v", tree["mine"], tree["unplaced"])
	}
	var order []string
	for _, g := range got.Tree {
		order = append(order, g.ID)
	}
	if strings.Join(order, ",") != "daily,work,fit,zoo,mine,core.settings" {
		t.Fatalf("樹的順序：%v", order)
	}
	// 圖示：伺服器照 manifest 給，認不得的名字由前端退回預設圖示。
	for _, it := range got.Pool {
		if it.ID == "crashy.x" && it.Icon != "no-such-icon" {
			t.Fatalf("icon = %q", it.Icon)
		}
	}
	// GetMenu 不編譯任何頁面（renderer 除外）。
	if e.as.Builds() != 0 {
		t.Fatalf("GetMenu 編譯了 %d 次", e.as.Builds())
	}
	// 有 menu.toml 時照使用者的樹，沒放進樹的進「未分類」。
	b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{{Group: "mine", Title: "我的", Children: []string{"plain.home", "core.chat"}}}}})
	if w := post(e.h, "/api/web/set_menu_prefs", string(b)); w.Code != 200 {
		t.Fatalf("存選單：%d %s", w.Code, w.Body.String())
	}
	tree = treeOf(menu(t, e))
	if strings.Join(tree["mine"], ",") != "plain.home,core.chat" || !strings.Contains(strings.Join(tree["unplaced"], ","), "hinted.w") {
		t.Fatalf("使用者的樹：%v", tree)
	}
}

// Guards: REQ-105
func TestShowWhenAgentEnabled(t *testing.T) {
	e := newEnv(t) // 名冊：code、life 啟用，fitness 停用
	withCatalog(t, e, plainPackage("gated", sdk.TierMine,
		sdk.MenuItemDecl{ID: "always", Title: "一直在", Page: "home"},
		sdk.MenuItemDecl{ID: "on", Title: "code 在", ShowWhen: "agent:code:enabled", Page: "home"},
		sdk.MenuItemDecl{ID: "off", Title: "fitness 停用", ShowWhen: "agent:fitness:enabled", Page: "home"},
		sdk.MenuItemDecl{ID: "missing", Title: "沒這位", ShowWhen: "agent:nobody:enabled", Page: "home"},
		sdk.MenuItemDecl{ID: "weird", Title: "寫法怪", ShowWhen: "when it rains", Page: "home"}))
	got := strings.Join(poolIDs(menu(t, e)), ",")
	for id, want := range map[string]bool{"gated.always": true, "gated.on": true, "gated.off": false, "gated.missing": false, "gated.weird": false} {
		if strings.Contains(got, id+",") || strings.HasSuffix(got, id) {
			if !want {
				t.Fatalf("%s 不該顯示：%s", id, got)
			}
		} else if want {
			t.Fatalf("%s 要顯示：%s", id, got)
		}
	}
	// 秘書啟用狀態改變，下一次算選單就跟著變（不快取）。
	e.agents.list[2].Fields["enabled"] = true
	if got := strings.Join(poolIDs(menu(t, e)), ","); !strings.Contains(got, "gated.off") {
		t.Fatalf("fitness 啟用後要顯示：%s", got)
	}
}

// Guards: REQ-114
func TestSlotPagesListed(t *testing.T) {
	e := newEnv(t)
	host := demoPackage("host", sdk.TierMine)
	host.Pages = []sdk.PageDecl{
		{ID: "cardonly", Title: "只放 slot", Component: "pages/menu.tsx", Slot: "shell.menu"},
		{ID: "main", Title: "主頁", Component: "pages/home.tsx"},
		{ID: "later", Title: "後面的頁", Component: "pages/home.tsx"},
	}
	host.Menu = nil
	host.Extensions = []sdk.ExtensionPoint{
		{ID: "side", Kind: sdk.ExtensionKindPageSlot},
		{ID: "mood", Kind: sdk.ExtensionKindSetting},
		{ID: "top", Kind: sdk.ExtensionKindPageSlot},
	}
	guest := demoPackage("guest", sdk.TierMine)
	guest.Menu = nil
	guest.Pages = []sdk.PageDecl{
		{ID: "widget", Title: "掛件", Component: "pages/card.tsx", Slot: "host.side"},
		{ID: "mine", Title: "掛在今天", Component: "pages/card.tsx", Slot: "core.overview.cards"},
		{ID: "todo", Title: "掛在待辦", Component: "pages/card.tsx", Slot: "core.reminders.top"},
		{ID: "tab", Title: "衣服", Component: "pages/card.tsx", Slot: "core.persona.tab"},
		{ID: "add", Title: "上傳照片建角色", Component: "pages/card.tsx", Slot: "core.personas.new"},
	}
	// 沒有「沒有 slot 的頁面」可以當 host：它的 page_slot 擴充點不列（不是錯誤）。
	hostless := demoPackage("hostless", sdk.TierMine)
	hostless.Menu = nil
	hostless.Pages = []sdk.PageDecl{{ID: "only", Title: "只掛件", Component: "pages/card.tsx", Slot: "core.overview.cards"}}
	hostless.Extensions = []sdk.ExtensionPoint{{ID: "nope", Kind: sdk.ExtensionKindPageSlot}}
	withCatalog(t, e, host, guest, hostless)

	got := pages(t, e)
	if s := got["core.overview"].Slots; len(s) != 1 || s[0].ID != "core.overview.cards" || s[0].Page != "core.overview" {
		t.Fatalf("今天頁的 slot：%+v", s)
	}
	if s := got["core.reminders"].Slots; len(s) != 1 || s[0].ID != "core.reminders.top" || s[0].Page != "core.reminders" {
		t.Fatalf("待辦頁的 slot（RMD-25）：%+v", s)
	}
	if e := got["guest.todo"].Error; e != "" {
		t.Fatalf("掛在待辦頁的不是錯誤：%q", e)
	}
	// 角色頁的兩個 slot（DEV-143）：一位角色一頁的分頁、清單「新增角色」裡的新增方式；套件掛上去不是錯誤，標題照套件給的。
	if s := got["core.personas"].Slots; len(s) != 2 || s[0] != (sdk.PageSlot{ID: "core.persona.tab", Page: "core.personas"}) || s[1] != (sdk.PageSlot{ID: "core.personas.new", Page: "core.personas"}) {
		t.Fatalf("角色頁的 slot：%+v", s)
	}
	for _, id := range []string{"guest.tab", "guest.add"} {
		if p := got[id]; p.Error != "" || p.Route != "" || p.BundleURL == "" {
			t.Fatalf("%s = %+v", id, p)
		}
	}
	if got["guest.tab"].Title != "衣服" {
		t.Fatalf("分頁標題照套件給的：%q", got["guest.tab"].Title)
	}
	// slot 列在套件第一個沒有 slot 的頁面（cardonly 是選單元件，不算）。
	main := got["host.main"]
	if len(main.Slots) != 2 || main.Slots[0] != (sdk.PageSlot{ID: "host.side", Page: "host.main"}) || main.Slots[1] != (sdk.PageSlot{ID: "host.top", Page: "host.main"}) {
		t.Fatalf("host.main.Slots = %+v", main.Slots)
	}
	if len(got["host.later"].Slots) != 0 {
		t.Fatalf("host.later 不該有 slot：%+v", got["host.later"].Slots)
	}
	if _, ok := got["host.cardonly"]; ok {
		t.Fatal("slot = shell.menu 的頁不該出現在 GetPages")
	}
	w := got["guest.widget"]
	if w.Slot != "host.side" || w.Route != "" || w.Error != "" || w.BundleURL == "" {
		t.Fatalf("guest.widget = %+v", w)
	}
	if c := got["guest.mine"]; c.Slot != "core.overview.cards" || c.Route != "" || c.BundleURL == "" {
		t.Fatalf("guest.mine = %+v", c)
	}
	if got["hostless.only"].Error != "" {
		t.Fatalf("hostless.only = %+v", got["hostless.only"])
	}
	for _, p := range got {
		for _, s := range p.Slots {
			if strings.HasPrefix(s.ID, "hostless.") {
				t.Fatalf("沒有 host 頁的擴充點不該列：%+v", s)
			}
		}
	}
}

// 套件頁面宣告的 route 當成別名：看板打開那個網址就轉到 /p/<套件>/<頁面>（DEV-91）。沒寫、跟正式網址一樣、或掛在 slot 的不給別名。
func TestPageRouteAlias(t *testing.T) {
	e := newEnv(t)
	p := demoPackage("water", sdk.TierMine)
	p.Menu = nil
	p.Pages = []sdk.PageDecl{
		{ID: "log", Title: "喝水", Component: "pages/home.tsx", Route: " /water/ "},
		{ID: "same", Title: "同網址", Component: "pages/home.tsx", Route: "/p/water/same"},
		{ID: "bare", Title: "沒寫", Component: "pages/home.tsx"},
		{ID: "card", Title: "掛件", Component: "pages/card.tsx", Slot: "core.overview.cards", Route: "/water-card"},
	}
	withCatalog(t, e, p)
	got := pages(t, e)
	if g := got["water.log"]; g.Route != "/p/water/log" || g.Alias != "/water" {
		t.Fatalf("water.log = %+v", g)
	}
	for _, id := range []string{"water.same", "water.bare", "water.card"} {
		if got[id].Alias != "" {
			t.Fatalf("%s 不該有別名：%+v", id, got[id])
		}
	}
}

// Guards: REQ-114
func TestSlotMissingHostNotFatal(t *testing.T) {
	e := newEnv(t)
	p := demoPackage("guest", sdk.TierMine)
	p.Pages = append(p.Pages, sdk.PageDecl{ID: "lost", Title: "走丟的", Component: "pages/card.tsx", Slot: "gone.cards"})
	withCatalog(t, e, p)
	got := pages(t, e)
	lost := got["guest.lost"]
	if lost.Error != "要掛的位置「gone.cards」不存在" || lost.BundleURL != "" || lost.Route != "" {
		t.Fatalf("guest.lost = %+v", lost)
	}
	// 其他頁面與選單、核心頁面照常。
	if got["guest.home"].BundleURL == "" || got["guest.card"].BundleURL == "" || got["core.chat"].Route != "/chat" {
		t.Fatalf("其他頁面要照常：%+v", got)
	}
	if len(menu(t, e).Pool) < 8 {
		t.Fatal("選單要照常")
	}
}

// Guards: REQ-140
func TestRendererMustBeMine(t *testing.T) {
	e := newEnv(t)
	shellMenu := func(id string, tier sdk.Tier) sdk.Package {
		p := demoPackage(id, tier)
		p.Menu = nil
		p.Pages = []sdk.PageDecl{{ID: "nav", Title: "選單", Component: "pages/menu.tsx", Slot: "shell.menu"}, {ID: "plain", Title: "一般頁", Component: "pages/home.tsx"}}
		return p
	}
	withCatalog(t, e, shellMenu("official-nav", sdk.TierOfficial), shellMenu("shared-nav", sdk.TierInstalled), shellMenu("mine-nav", sdk.TierMine))
	setRenderer := func(id string) *sdk.Menu {
		t.Helper()
		b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{Renderer: id, Tree: []sdk.MenuPrefEntry{{Item: "core.settings"}}}})
		if w := post(e.h, "/api/web/set_menu_prefs", string(b)); w.Code != 200 {
			t.Fatalf("存選單：%d %s", w.Code, w.Body.String())
		}
		return menu(t, e)
	}
	for _, id := range []string{"official-nav.nav", "shared-nav.nav"} {
		m := setRenderer(id)
		if m.Renderer != id || m.RendererBundleURL != "" || m.RendererError != "自訂選單「"+id+"」不在自己的套件裡" {
			t.Fatalf("%s：%+v", id, m)
		}
	}
	for _, id := range []string{"nobody.nav", "mine-nav.gone", "nodot"} {
		m := setRenderer(id)
		if m.RendererBundleURL != "" || m.RendererError != "找不到自訂選單「"+id+"」" {
			t.Fatalf("%s：%+v", id, m)
		}
	}
	if m := setRenderer("mine-nav.plain"); m.RendererBundleURL != "" || !strings.Contains(m.RendererError, "shell.menu") {
		t.Fatalf("不是選單元件：%+v", m)
	}
	m := setRenderer("mine-nav.nav")
	if m.Renderer != "mine-nav.nav" || m.RendererError != "" || !strings.HasPrefix(m.RendererBundleURL, "/pkg-bundles/") {
		t.Fatalf("mine 的選單元件：%+v", m)
	}
	if w := get(e.h, m.RendererBundleURL); w.Code != 200 {
		t.Fatalf("renderer bundle → %d", w.Code)
	}
	// 沒設 renderer：三個欄位都空。
	if m := setRenderer(""); m.Renderer != "" || m.RendererBundleURL != "" || m.RendererError != "" {
		t.Fatalf("沒設 renderer：%+v", m)
	}
}

// Guards: REQ-140
func TestRendererCompileErrorReported(t *testing.T) {
	e := newEnv(t)
	p := demoPackage("custom", sdk.TierMine)
	p.Menu = nil
	p.Pages = []sdk.PageDecl{{ID: "nav", Title: "我的選單", Component: "pages/bad.tsx", Slot: "shell.menu"}}
	withCatalog(t, e, p)
	body, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{Renderer: "custom.nav", Tree: []sdk.MenuPrefEntry{{Item: "core.settings"}}}})
	if w := post(e.h, "/api/web/set_menu_prefs", string(body)); w.Code != 200 {
		t.Fatalf("存選單：%d %s", w.Code, w.Body.String())
	}
	m := menu(t, e)
	if m.Renderer != "custom.nav" || m.RendererBundleURL != "" || !strings.HasPrefix(m.RendererError, "頁面「我的選單」編不過：") || !strings.Contains(m.RendererError, "套件頁面只能 import") {
		t.Fatalf("編不過的選單元件：%+v", m)
	}
	// 選單本身照常可用：樹跟 pool 都在。
	if len(m.Tree) == 0 || len(m.Pool) < 7 {
		t.Fatalf("選單要照常：%+v", m)
	}
}

// TestDefaultPrefsIncludePackages：沒有 menu.toml 時 GetMenuPrefs 回的預設樹跟畫面上的是同一份算法：
// 含套件 group_hint 群組、不含「未分類」、最後有 core.settings；存檔之後回使用者的樹。
// （plain 是官方層：沒有 hint 的官方項目留給「未分類」；自己的進「我的套件」見 TestMenuTier。）
// Guards: REQ-141
func TestDefaultPrefsIncludePackages(t *testing.T) {
	e := newEnv(t)
	withCatalog(t, e,
		plainPackage("plain", sdk.TierOfficial, sdk.MenuItemDecl{ID: "home", Title: "未分類的", Page: "home", Order: 5}),
		plainPackage("hinted", sdk.TierMine,
			sdk.MenuItemDecl{ID: "w", Title: "工作的", GroupHint: "work", Page: "home", Order: 2},
			sdk.MenuItemDecl{ID: "d", Title: "日常的", GroupHint: "daily", Page: "home", Order: 1},
			sdk.MenuItemDecl{ID: "z", Title: "新群組", GroupHint: "zoo", Page: "home", Order: 1}))
	got := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}")).Prefs
	byGroup := map[string][]string{}
	var order []string
	for _, en := range got.Tree {
		if en.Group == "" {
			order = append(order, en.Item)
			continue
		}
		order = append(order, en.Group)
		byGroup[en.Group] = en.Children
	}
	if strings.Join(order, ",") != "daily,work,zoo,core.settings" {
		t.Fatalf("預設樹的順序：%v（不含「未分類」、最後是 core.settings）", order)
	}
	if strings.Join(byGroup["daily"], ",") != "core.overview,core.chat,core.reminders,core.messages,core.tasks,hinted.d,core.personas" ||
		strings.Join(byGroup["work"], ",") != "core.policy,hinted.w" || strings.Join(byGroup["zoo"], ",") != "hinted.z" {
		t.Fatalf("群組內容：%v", byGroup)
	}
	for _, en := range got.Tree {
		if en.Group == "unplaced" || strings.Contains(strings.Join(en.Children, ","), "plain.home") {
			t.Fatalf("沒有 group_hint 的項目留給「未分類」，不在預設樹裡：%+v", en)
		}
	}
	// 跟 GetMenu 畫出來的群組一致（GetMenu 另有「未分類」，裡面是沒放進樹的項目）。
	tree := treeOf(menu(t, e))
	for g, children := range byGroup {
		// 權限申請預設收到設定（MNU-10）：位置留在預設樹，選單不畫。
		children = slices.DeleteFunc(slices.Clone(children), func(id string) bool { return id == "core.policy" })
		if strings.Join(tree[g], ",") != strings.Join(children, ",") {
			t.Fatalf("群組 %s：選單 %v ≠ 預設樹 %v", g, tree[g], children)
		}
	}
	if strings.Join(tree["unplaced"], ",") != "plain.home" {
		t.Fatalf("未分類 = %v", tree["unplaced"])
	}
	// 只有 renderer、沒有 entry 的 menu.toml 仍然回預設樹（連同 renderer）。
	write(t, e.home+"/settings/menu.toml", "schema = 1\nrenderer = \"demo.menu\"\n")
	again := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}")).Prefs
	if again.Renderer != "demo.menu" || len(again.Tree) != len(got.Tree) {
		t.Fatalf("只有 renderer：%+v", again)
	}
}

// officialMenuPackages 讀 packages/official/*/package.toml 的 [[contributes.menu]]（只取選單項目，忽略 show_when），
// 組成跟正式家裝了全部官方套件時一樣的選單宣告。
func officialMenuPackages(t *testing.T) []sdk.Package {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "packages", "official", "*", "package.toml"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到官方套件的 package.toml：%v %v", files, err)
	}
	var out []sdk.Package
	for _, f := range files {
		var doc struct {
			Contributes struct {
				Menu []struct {
					ID        string `toml:"id"`
					Title     string `toml:"title"`
					Icon      string `toml:"icon"`
					GroupHint string `toml:"group_hint"`
					Order     int32  `toml:"order"`
					Page      string `toml:"page"`
					Front     bool   `toml:"front"`
				} `toml:"menu"`
			} `toml:"contributes"`
		}
		if _, err := toml.DecodeFile(f, &doc); err != nil {
			t.Fatal(err)
		}
		var items []sdk.MenuItemDecl
		for _, m := range doc.Contributes.Menu {
			items = append(items, sdk.MenuItemDecl{ID: m.ID, Title: m.Title, Icon: m.Icon, GroupHint: m.GroupHint, Order: m.Order, Page: "home", Front: m.Front})
		}
		if len(items) > 0 {
			out = append(out, plainPackage(filepath.Base(filepath.Dir(f)), sdk.TierInstalled, items...))
		}
	}
	return out
}

// TestDefaultTreeKeepsTasksInBar：裝了全部官方套件、沒有 menu.toml 時，攤平後前 5 項仍是今天、聊天、待辦、訊息、任務單
// （手機底部列的 5 格＋「更多」），套件項目全部排在後面（F6：作者試用時，任務單被「飲食」擠進「更多」）。
// Guards: REQ-201
func TestDefaultTreeKeepsTasksInBar(t *testing.T) {
	e := newEnv(t)
	pkgs := officialMenuPackages(t)
	if len(pkgs) < 4 {
		t.Fatalf("官方套件的選單項目太少，測試沒意義：%d 個套件", len(pkgs))
	}
	withCatalog(t, e, pkgs...)
	var flat []string
	for _, entry := range menu(t, e).Tree {
		if len(entry.Children) == 0 {
			flat = append(flat, entry.ID)
			continue
		}
		flat = append(flat, ids(entry.Children)...)
	}
	if len(flat) <= 6 {
		t.Fatalf("攤平後只有 %d 項，底部列不會有「更多」：%v", len(flat), flat)
	}
	if got := strings.Join(flat[:5], ","); got != "core.overview,core.chat,core.reminders,habits.list,nutrition.log" {
		t.Fatalf("底部列前 5 格：%s（全部：%v）", got, flat)
	}
}

// TestMenuSectionPassedThrough：套件宣告的 section／section_title 原樣放進 pool 與樹的項目（看板依它合成一個入口）；頁面的 chat 放進 GetPages。
// Guards: REQ-235
func TestMenuSectionPassedThrough(t *testing.T) {
	e := newEnv(t)
	withCatalog(t, e,
		plainPackage("meals", sdk.TierMine, sdk.MenuItemDecl{ID: "log", Title: "飲食", GroupHint: "daily", Page: "home", Order: 1, Section: "health", SectionTitle: "健康"}),
		plainPackage("gym", sdk.TierMine,
			sdk.MenuItemDecl{ID: "log", Title: "運動", GroupHint: "daily", Page: "home", Order: 2, Section: "health", SectionTitle: "健康"},
			sdk.MenuItemDecl{ID: "solo", Title: "自己一頁", GroupHint: "daily", Page: "home", Order: 3}),
	)
	// 頁面的 chat／chat_agent 原樣放進 GetPages（看板依它在旁邊放聊天）。
	e.cat.pkgs[0].Pages[0].Chat = true
	e.cat.pkgs[0].Pages[0].ChatAgent = "nutrition"
	if pg := pages(t, e)["meals.home"]; !pg.Chat || pg.ChatAgent != "nutrition" {
		t.Fatalf("meals.home = %+v", pg)
	}
	if pg := pages(t, e)["gym.home"]; pg.Chat || pg.ChatAgent != "" {
		t.Fatalf("gym.home = %+v", pg)
	}
	got := menu(t, e)
	sections := map[string]string{}
	for _, it := range got.Pool {
		sections[it.ID] = it.Section + "/" + it.SectionTitle
	}
	if sections["meals.log"] != "health/健康" || sections["gym.log"] != "health/健康" || sections["gym.solo"] != "/" {
		t.Fatalf("pool sections = %v", sections)
	}
	for _, g := range got.Tree {
		for _, c := range g.Children {
			if c.ID == "gym.log" && (c.Section != "health" || c.SectionTitle != "健康") {
				t.Fatalf("tree entry = %+v", c)
			}
			if c.ID == "core.chat" && c.Section != "" {
				t.Fatalf("核心項目不該有 section：%+v", c)
			}
		}
	}
}
