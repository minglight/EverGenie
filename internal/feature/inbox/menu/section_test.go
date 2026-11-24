package menu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 「角色」頁（DEV-117、D-62）：核心的介紹帶 section，套件的選單項目宣告同一個 section 就成為它的分頁。

// personasPackage 是宣告 section = "personas" 的兩個選單項目（跟官方衣櫃一樣：group_hint daily、order 40／41；核心測試不寫 closet 的名字以外的規則）。
func personasPackage() sdk.Package {
	return plainPackage("closet", sdk.TierOfficial,
		sdk.MenuItemDecl{ID: "wardrobe", Title: "衣櫃", Icon: "shirt", GroupHint: "daily", Page: "home", Order: 40, Section: "personas"},
		sdk.MenuItemDecl{ID: "showroom", Title: "展示間", Icon: "image-plus", GroupHint: "daily", Page: "home", Order: 41, Section: "personas"},
	)
}

func childrenOf(m *sdk.Menu, group string) []string {
	for _, g := range m.Tree {
		if g.ID == group {
			return ids(g.Children)
		}
	}
	return nil
}

// 核心項目 core.personas 在 pool 裡：網址 /personas、section personas、section_title「角色」、名稱「介紹」（頁首已叫「角色」，DEV-156 U-15），在 core.settings 前面。
func TestCorePersonasItemCarriesSection(t *testing.T) {
	e := newEnv(t)
	got := menu(t, e)
	var found *sdk.MenuItem
	for i := range got.Pool {
		if got.Pool[i].ID == "core.personas" {
			found = &got.Pool[i]
		}
	}
	if found == nil {
		t.Fatalf("pool 沒有 core.personas：%v", poolIDs(got))
	}
	if found.Route != "/personas" || found.Section != "personas" || found.SectionTitle != "角色" || found.Title != "介紹" || found.Tier != sdk.TierOfficial {
		t.Fatalf("core.personas = %+v", *found)
	}
	// GetPages 也列出核心頁（標題、網址）。
	if pg := pages(t, e)["core.personas"]; pg.Route != "/personas" || pg.Title != "介紹" {
		t.Fatalf("GetPages core.personas = %+v", pg)
	}
}

// 預設樹：同 section 的套件項目在「日常」時，介紹放在它們前面，三個連在一起（核心在前、再照 order）；
// 沒有同 section 的套件時接在「日常」最後。沒有東西掉進「未分類」。
func TestDefaultTreePlacesCoreSectionBeforePackageItems(t *testing.T) {
	e := newEnv(t)
	got := menu(t, e)
	if d := strings.Join(childrenOf(got, "daily"), ","); d != "core.overview,core.chat,core.reminders,core.messages,core.tasks,core.personas" {
		t.Fatalf("沒有衣櫃時的日常：%s", d)
	}
	withCatalog(t, e, personasPackage(),
		plainPackage("habits", sdk.TierOfficial, sdk.MenuItemDecl{ID: "list", Title: "習慣", GroupHint: "daily", Page: "home", Order: 30}))
	got = menu(t, e)
	want := "core.overview,core.chat,core.reminders,core.messages,core.tasks,habits.list,core.personas,closet.wardrobe,closet.showroom"
	if d := strings.Join(childrenOf(got, "daily"), ","); d != want {
		t.Fatalf("預設樹的日常：%s\n要：%s", d, want)
	}
	if u := childrenOf(got, "unplaced"); len(u) > 0 {
		t.Fatalf("預設樹不該有未分類：%v", u)
	}
	prefs := decode[sdk.WebGetMenuPrefsResponse](t, post(e.h, "/api/web/get_menu_prefs", "{}")).Prefs
	if d := strings.Join(prefs.Tree[0].Children, ","); d != want {
		t.Fatalf("編輯器看到的預設樹要跟選單一樣：%s", d)
	}
}

// 已存過 menu.toml 的家（衣櫃、展示間在「日常」、樹裡沒有 core.personas）：介紹跟它們合在同一個群組、排在最前面，
// 不掉進「未分類」；使用者把同 section 的項目分到兩個群組、或順序跟 pool 不同，也一樣在第一次出現的位置照 pool 順序放在一起。
func TestSavedMenuGathersSectionIntoOneEntry(t *testing.T) {
	e := newEnv(t)
	withCatalog(t, e, personasPackage())
	write(t, e.home+"/settings/menu.toml", `schema = 1
[[entry]]
group = "daily"
title = "日常"
children = ["core.overview", "core.chat", "core.reminders", "core.messages", "core.tasks", "closet.wardrobe", "closet.showroom"]
[[entry]]
group = "work"
title = "工作"
children = ["core.policy"]
`)
	got := menu(t, e)
	if d := strings.Join(childrenOf(got, "daily"), ","); d != "core.overview,core.chat,core.reminders,core.messages,core.tasks,core.personas,closet.wardrobe,closet.showroom" {
		t.Fatalf("日常：%s", d)
	}
	if u := childrenOf(got, "unplaced"); len(u) > 0 {
		t.Fatalf("不能多一個沒歸位的入口：%v", u)
	}
	// 分在兩個群組、順序顛倒：第一次遇到的位置（work 的展示間）照 pool 順序放三個，日常那邊不再出現。
	b, _ := json.Marshal(sdk.WebSetMenuPrefsRequest{Prefs: &sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{
		{Group: "work", Title: "工作", Children: []string{"closet.showroom", "core.policy"}},
		{Group: "daily", Title: "日常", Children: []string{"core.overview", "closet.wardrobe", "core.personas"}},
	}}})
	if w := post(e.h, "/api/web/set_menu_prefs", string(b)); w.Code != 200 {
		t.Fatalf("存選單：%d %s", w.Code, w.Body.String())
	}
	got = menu(t, e)
	if w := strings.Join(childrenOf(got, "work"), ","); w != "core.personas,closet.wardrobe,closet.showroom" {
		t.Fatalf("工作：%s", w)
	}
	if d := strings.Join(childrenOf(got, "daily"), ","); d != "core.overview" {
		t.Fatalf("日常：%s", d)
	}
}

// 存過 menu.toml、樹上一個同 section 的項目都沒有（衣櫃停用或沒裝、樹裡也沒寫介紹）：介紹照 WEB-3 進「未分類」，
// 不會自己跑進使用者的群組（menu-prefs.md 的邊界情況，MNU-7）。
func TestSavedMenuWithoutSectionPeersLeavesCoreItemUnplaced(t *testing.T) {
	e := newEnv(t)
	write(t, e.home+"/settings/menu.toml", `schema = 1
[[entry]]
group = "daily"
title = "日常"
children = ["core.overview", "core.chat", "core.reminders", "core.messages", "core.tasks"]
`)
	got := menu(t, e)
	if d := strings.Join(childrenOf(got, "daily"), ","); d != "core.overview,core.chat,core.reminders,core.messages,core.tasks" {
		t.Fatalf("日常不能多出介紹：%s", d)
	}
	if u := strings.Join(childrenOf(got, "unplaced"), ","); !strings.Contains(u, "core.personas") {
		t.Fatalf("介紹要在未分類：%s", u)
	}
}

// MNU-10：預設把權限申請收到設定；存過樹的家也一樣；空清單全部放回；今天與設定收不掉；已存的樹位置留著。
func TestTuckedItems(t *testing.T) {
	has := func(entries []sdk.MenuEntry, id string) bool {
		for _, e := range entries {
			if e.ID == id {
				return true
			}
			for _, c := range e.Children {
				if c.ID == id {
					return true
				}
			}
		}
		return false
	}
	e := newEnv(t)
	got := menu(t, e)
	if has(got.Tree, "core.policy") || len(got.Tucked) != 1 || got.Tucked[0].ID != "core.policy" {
		t.Fatalf("預設要收起權限申請：%+v", got.Tucked)
	}
	write(t, e.home+"/settings/menu.toml", "schema = 1\n[[entry]]\nitem = \"core.policy\"\n[[entry]]\nitem = \"core.chat\"\n")
	got = menu(t, e)
	if has(got.Tree, "core.policy") || len(got.Tucked) != 1 {
		t.Fatalf("存過樹的家也要收：%+v", got.Tree)
	}
	body := `{"prefs":{"tree":[{"item":"core.policy"},{"item":"core.chat"}],"hidden":["core.chat","core.overview","core.settings"]}}`
	if r := post(e.h, "/api/web/set_menu_prefs", body); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	got = menu(t, e)
	if !has(got.Tree, "core.policy") || has(got.Tree, "core.chat") || !has(got.Tree, "core.overview") || !has(got.Tree, "core.settings") {
		t.Fatalf("hidden 沒照存：%+v", got.Tree)
	}
	if r := post(e.h, "/api/web/set_menu_prefs", `{"prefs":{"tree":[{"item":"core.chat"}],"hidden":[]}}`); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if got = menu(t, e); len(got.Tucked) != 0 || !has(got.Tree, "core.policy") {
		t.Fatalf("空清單要全部放回：%+v", got)
	}
}

// 宣告 front 的日常項目（習慣與健康）預設排在待辦後面；存過的樹不動。
func TestDefaultTreeFrontItems(t *testing.T) {
	e := newEnv(t)
	withCatalog(t, e, plainPackage("habits", sdk.TierOfficial, sdk.MenuItemDecl{ID: "list", Title: "習慣", GroupHint: "daily", Page: "home", Order: 30, Front: true}))
	want := "core.overview,core.chat,core.reminders,habits.list,core.messages,core.tasks,core.personas"
	if d := strings.Join(childrenOf(menu(t, e), "daily"), ","); d != want {
		t.Fatalf("日常：%s", d)
	}
}
