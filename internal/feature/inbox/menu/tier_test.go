package menu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// tierCatalog：官方、分享的（從網址裝，有來源）、自己的各一個沒寫 group_hint 的項目，加上自己的 daily 項目與明說放「未分類」的項目。
func tierCatalog(t *testing.T, e *env) *fakeCatalog {
	t.Helper()
	shared := plainPackage("pond", sdk.TierInstalled, sdk.MenuItemDecl{ID: "home", Title: "池塘", Page: "home"})
	shared.Source = &sdk.PackageSource{URL: "https://github.com/friend/pond.git", Ref: "v1.0.0"}
	return withCatalog(t, e,
		plainPackage("habits", sdk.TierOfficial, sdk.MenuItemDecl{ID: "home", Title: "習慣", Page: "home"}),
		shared,
		plainPackage("garden", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "花園", Page: "home"}),
		plainPackage("diary", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "日記", GroupHint: "daily", Page: "home"}),
		plainPackage("lab", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "實驗", GroupHint: "unplaced", Page: "home"}),
	)
}

// TestMenuTier：選單 pool 與樹的每個項目帶 tier（核心項目算 official）；GetPages 的套件頁帶 tier 與分享來源。
// 沒有 menu.toml 時，自己的與分享的套件沒寫 group_hint 的放預設樹的「我的套件」群組（在 hint 群組後面、未分類前面），
// 官方的與明說 unplaced 的進「未分類」，有 hint 的照 hint；GetMenuPrefs 回同一棵樹。
// Guards: REQ-348, REQ-349
func TestMenuTier(t *testing.T) {
	e := newEnv(t)
	tierCatalog(t, e)
	got := menu(t, e)
	tiers := map[string]sdk.Tier{}
	for _, it := range got.Pool {
		tiers[it.ID] = it.Tier
	}
	want := map[string]sdk.Tier{
		"core.overview": sdk.TierOfficial, "core.chat": sdk.TierOfficial, "core.settings": sdk.TierOfficial,
		"habits.home": sdk.TierOfficial, "pond.home": sdk.TierInstalled, "garden.home": sdk.TierMine, "diary.home": sdk.TierMine, "lab.home": sdk.TierMine,
	}
	for id, tier := range want {
		if tiers[id] != tier {
			t.Errorf("pool %s 的 tier = %q，要 %q", id, tiers[id], tier)
		}
	}
	for _, it := range got.Pool {
		if it.Tier == "" {
			t.Errorf("pool 每個項目都要有 tier：%+v", it)
		}
	}
	// 樹裡的項目也帶（從 pool 抄），群組沒有。
	for _, g := range got.Tree {
		if len(g.Children) > 0 && g.Tier != "" {
			t.Errorf("群組不帶 tier：%+v", g)
		}
		for _, c := range g.Children {
			if c.Tier != want[c.ID] && want[c.ID] != "" {
				t.Errorf("樹 %s 的 tier = %q，要 %q", c.ID, c.Tier, want[c.ID])
			}
		}
	}
	tree := treeOf(got)
	if strings.Join(tree["mine"], ",") != "garden.home,pond.home" {
		t.Fatalf("我的套件 = %v（自己的與分享的、沒寫 hint）", tree["mine"])
	}
	if !strings.HasSuffix(strings.Join(tree["daily"], ","), ",diary.home,core.personas") {
		t.Fatalf("有 hint 的照 hint：daily = %v", tree["daily"])
	}
	if strings.Join(tree["unplaced"], ",") != "habits.home,lab.home" {
		t.Fatalf("官方沒 hint 與明說 unplaced 的進未分類：%v", tree["unplaced"])
	}
	var order []string
	for _, g := range got.Tree {
		order = append(order, g.ID)
	}
	if strings.Join(order, ",") != "daily,mine,unplaced,core.settings" {
		t.Fatalf("樹的順序：%v", order)
	}
	for _, g := range got.Tree {
		if g.ID == "mine" && (g.Title != "我的套件" || g.Icon != "puzzle") {
			t.Fatalf("我的套件群組：%+v", g)
		}
	}
	// 編輯器打開就是同一棵（含「我的套件」，不含「未分類」）。
	prefs := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}")).Prefs
	found := false
	for _, en := range prefs.Tree {
		if en.Group == "mine" {
			found = strings.Join(en.Children, ",") == "garden.home,pond.home" && en.Title == "我的套件"
		}
	}
	if !found {
		t.Fatalf("GetMenuPrefs 的預設樹要有「我的套件」：%+v", prefs.Tree)
	}
	// GetPages：套件頁帶 tier；分享的帶來源網址，其他沒有。
	pg := pages(t, e)
	if p := pg["pond.home"]; p.Tier != sdk.TierInstalled || p.Source != "https://github.com/friend/pond.git" || p.Route != "/p/pond/home" {
		t.Fatalf("pond.home = %+v", p)
	}
	if p := pg["garden.home"]; p.Tier != sdk.TierMine || p.Source != "" {
		t.Fatalf("garden.home = %+v", p)
	}
	if p := pg["habits.home"]; p.Tier != sdk.TierOfficial {
		t.Fatalf("habits.home = %+v", p)
	}
	if p := pg["core.overview"]; p.Tier != "" {
		t.Fatalf("核心頁不是套件，不帶 tier：%+v", p)
	}
}

// TestMenuUserTreeWins：使用者存過 menu.toml 之後，選單完全照他的樹：新裝的自己的套件不會被自動塞進任何群組（包括他留著的「我的套件」），
// 照舊出現在「未分類」；他刪掉「我的套件」群組，它就不會自己長回來；GetMenuPrefs 回他存的樹原樣。
// Guards: REQ-349, REQ-106
func TestMenuUserTreeWins(t *testing.T) {
	e := newEnv(t)
	c := withCatalog(t, e, plainPackage("garden", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "花園", Page: "home"}))
	save := func(tree []sdk.MenuPrefEntry) {
		t.Helper()
		b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{Tree: tree}})
		if w := post(e.h, "/api/web/set_menu_prefs", string(b)); w.Code != 200 {
			t.Fatalf("存選單：%d %s", w.Code, w.Body.String())
		}
	}
	// 使用者把預設樹存下來（保留「我的套件」，裡面只有 garden），之後又建了一個新套件 pond。
	save([]sdk.MenuPrefEntry{
		{Group: "daily", Title: "日常", Children: []string{"core.overview", "core.chat"}},
		{Group: "mine", Title: "我的套件", Icon: "puzzle", Children: []string{"garden.home"}},
	})
	c.pkgs = append(c.pkgs, plainPackage("pond", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "池塘", Page: "home"}))
	tree := treeOf(menu(t, e))
	if strings.Join(tree["mine"], ",") != "garden.home" {
		t.Fatalf("使用者的「我的套件」不能被自動加東西：%v", tree["mine"])
	}
	if !strings.Contains(","+strings.Join(tree["unplaced"], ",")+",", ",pond.home,") {
		t.Fatalf("新套件照舊在「未分類」：%v", tree["unplaced"])
	}
	prefs := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}")).Prefs
	if len(prefs.Tree) != 2 || strings.Join(prefs.Tree[1].Children, ",") != "garden.home" {
		t.Fatalf("GetMenuPrefs 要回使用者存的樹原樣：%+v", prefs.Tree)
	}
	// 使用者把自己的套件搬進「日常」、刪掉「我的套件」群組：它不會自己長回來。
	save([]sdk.MenuPrefEntry{{Group: "daily", Title: "日常", Children: []string{"core.overview", "garden.home"}}})
	got := menu(t, e)
	for _, g := range got.Tree {
		if g.ID == "mine" {
			t.Fatalf("使用者刪掉的「我的套件」不能長回來：%+v", got.Tree)
		}
	}
	tree = treeOf(got)
	if strings.Join(tree["daily"], ",") != "core.overview,garden.home" || !strings.Contains(strings.Join(tree["unplaced"], ","), "pond.home") {
		t.Fatalf("照使用者的樹：%v", tree)
	}
}

// TestPageCannotShadowCoreRoute：就算套件宣告了蓋住核心網址的 route（manifest 驗證會擋，這裡繞過驗證直接給），
// 看板也只用標準網址 /p/<套件>/<頁面>：GetPages 與選單 pool 都不出現套件宣告的 route，核心項目的網址不變。
// Guards: REQ-352
func TestPageCannotShadowCoreRoute(t *testing.T) {
	e := newEnv(t)
	thief := plainPackage("thief", sdk.TierMine, sdk.MenuItemDecl{ID: "home", Title: "搶網址", Page: "home"})
	thief.Pages[0].Route = "/tasks"
	withCatalog(t, e, thief)
	if p := pages(t, e)["thief.home"]; p.Route != "/p/thief/home" {
		t.Fatalf("套件頁網址只能是標準網址：%+v", p)
	}
	for _, it := range menu(t, e).Pool {
		switch it.ID {
		case "thief.home":
			if it.Route != "/p/thief/home" {
				t.Fatalf("選單項目網址：%+v", it)
			}
		case idTasks:
			if it.Route != "/tasks" {
				t.Fatalf("核心任務單的網址被動到：%+v", it)
			}
		}
	}
}
