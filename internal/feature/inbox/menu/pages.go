// internal/feature/inbox/menu/pages.go — ai_docs/modules/inbox.md。
package menu

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// 看板接套件（P6-2a，ai_docs/modules/inbox.md）：套件頁、slot、選單項目、自訂選單元件。

const (
	slotOverviewCards = "core.overview.cards"
	slotRemindersTop  = "core.reminders.top" // 待辦頁最上面（RMD-25）
	// 角色頁（DEV-143）：一位角色一頁的分頁（套件給標題、拿到角色代號），與角色清單「新增角色」裡的新增方式。
	slotPersonaTab  = "core.persona.tab"
	slotPersonasNew = "core.personas.new"
	slotShellMenu   = "shell.menu"
)

// pageRoute 是套件頁面的標準網址 /p/<套件 id>/<頁面 id>：看板只在這裡組這個字串（選單 pool 與 GetPages 都用它）；
// manifest 的 route 保留未用，不會變成網址（REQ-310、REQ-352）。前端也只在 pkg/routes.ts 組一次（REQ-351）。
func pageRoute(pkgID, pageID string) string { return "/p/" + pkgID + "/" + pageID }

// pageAlias 把套件寫的 route 整理成看板網址（前後空白、結尾斜線去掉，補開頭斜線）；沒寫回空。會蓋住核心網址的 route 在 packages 載入時就擋掉了。
func pageAlias(route string) string {
	r := strings.Trim(strings.TrimSpace(route), "/")
	if r == "" {
		return ""
	}
	return "/" + r
}

// sourceURL 是分享來的套件的來源網址（.source.toml）；本機資料夾裝的、官方、自己的都沒有。
func sourceURL(p sdk.Package) string {
	if p.Source == nil {
		return ""
	}
	return p.Source.URL
}

func pageTitle(pg sdk.PageDecl) string {
	if pg.Title != "" {
		return pg.Title
	}
	return pg.ID
}

// hostPage 是套件放 slot 的頁面：第一個沒有 slot 的頁面。
func hostPage(p sdk.Package) (sdk.PageDecl, bool) {
	for _, pg := range p.Pages {
		if pg.Slot == "" {
			return pg, true
		}
	}
	return sdk.PageDecl{}, false
}

// packageSlots 列出每個 slot 的 id → 它所在的頁面 id（核心的與套件 page_slot 擴充點的），以及各頁面自己的 Slots。
func (m *Module) packageSlots(pkgs []sdk.Package) (hosts map[string]string, perPage map[string][]sdk.PageSlot) {
	hosts = map[string]string{slotOverviewCards: idOverview, slotRemindersTop: idReminders, slotPersonaTab: idPersonas, slotPersonasNew: idPersonas}
	perPage = map[string][]sdk.PageSlot{}
	for _, p := range pkgs {
		host, hasHost := hostPage(p)
		for _, ext := range p.Extensions {
			if ext.Kind != sdk.ExtensionKindPageSlot {
				continue
			}
			if !hasHost {
				m.warnOnce("slot-host:"+p.ID+"."+ext.ID, "套件沒有可以放 slot 的頁面（要有一個沒有 slot 的頁面），略過這個 page_slot 擴充點", "package", p.ID, "extension", ext.ID)
				continue
			}
			slotID := p.ID + "." + ext.ID
			pageID := p.ID + "." + host.ID
			hosts[slotID] = pageID
			perPage[pageID] = append(perPage[pageID], sdk.PageSlot{ID: slotID, Page: pageID})
		}
	}
	return hosts, perPage
}

// packagePages 算出套件頁面的 PageInfo（編譯在這裡發生，指紋沒變就用快取）。
// 掛在 slot 的頁面宣告了 show_when、條件不成立就不列（MNU-11）：看板拿不到它，分頁、卡片都不會出現。
func (m *Module) packagePages(ctx context.Context) []sdk.PageInfo {
	pkgs := m.d.Frontend.VisiblePackages()
	hosts, perPage := m.packageSlots(pkgs)
	when := m.newShowWhen(ctx, pkgs)
	var out []sdk.PageInfo
	for _, p := range pkgs {
		for _, pg := range p.Pages {
			if pg.Slot == slotShellMenu {
				continue // 選單元件不是頁面，見 resolveRenderer
			}
			if pg.Slot != "" && !when.show(p.ID+"."+pg.ID, pg.ShowWhen) {
				continue
			}
			info := sdk.PageInfo{ID: p.ID + "." + pg.ID, Title: pageTitle(pg), Package: p.ID, Slot: pg.Slot, SlotFront: pg.SlotFront, Slots: perPage[p.ID+"."+pg.ID], Chat: pg.Chat, ChatAgent: pg.ChatAgent, Tier: p.Tier, Source: sourceURL(p)}
			if pg.Slot == "" {
				info.Route = pageRoute(p.ID, pg.ID)
				if alias := pageAlias(pg.Route); alias != info.Route {
					info.Alias = alias
				}
			}
			if pg.Slot != "" && hosts[pg.Slot] == "" {
				info.Error = fmt.Sprintf("要掛的位置「%s」不存在", pg.Slot)
				out = append(out, info)
				continue
			}
			hash, err := m.d.Frontend.Bundle(p.ID, pg.ID)
			if err != nil {
				info.Error = err.Error()
			} else {
				info.BundleURL = "/pkg-bundles/" + hash + ".js"
			}
			out = append(out, info)
		}
	}
	return out
}

// pkgMenuItem 是一個套件的選單項目，加上預設樹要用的建議群組與順序。
type pkgMenuItem struct {
	sdk.MenuItem
	hint  string
	order int32
	front bool
}

// packageMenuItems 算出 pool 要加的套件項目（已過濾 slot 頁與 show_when，依 order 再依 id 排）；不編譯任何頁面。
func (m *Module) packageMenuItems(ctx context.Context) []pkgMenuItem {
	pkgs := m.d.Frontend.VisiblePackages()
	if len(pkgs) == 0 {
		return nil
	}
	when := m.newShowWhen(ctx, pkgs)
	var out []pkgMenuItem
	for _, p := range pkgs {
		pages := map[string]sdk.PageDecl{}
		for _, pg := range p.Pages {
			pages[pg.ID] = pg
		}
		for _, mi := range p.Menu {
			id := p.ID + "." + mi.ID
			pg, ok := pages[mi.Page]
			if !ok {
				continue
			}
			if pg.Slot != "" {
				m.warnOnce("menu-slot:"+id, "選單項目指到掛在 slot 的頁面，略過", "item", id, "page", mi.Page, "slot", pg.Slot)
				continue
			}
			if !when.show(id, mi.ShowWhen) {
				continue
			}
			out = append(out, pkgMenuItem{
				MenuItem: sdk.MenuItem{ID: id, Title: mi.Title, Icon: mi.Icon, Route: pageRoute(p.ID, mi.Page), Section: mi.Section, SectionTitle: mi.SectionTitle, Tier: p.Tier},
				hint:     strings.TrimSpace(mi.GroupHint),
				order:    mi.Order,
				front:    mi.Front,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].order != out[j].order {
			return out[i].order < out[j].order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// defaultPrefsWith 是沒有 menu.toml 時的預設樹，加上套件項目：group_hint 是 daily／work 接在那個群組最後；
// 其他字在 work 後面、core.settings 前面新增群組（依 hint 排序）；沒有 hint 的：自己的（mine）與分享的（installed）
// 放「我的套件」群組（在 hint 群組後面），官方的留給「未分類」（SEC-302 S4、REQ-349）。
// 只有預設樹這樣放：使用者存過 menu.toml 就完全照他的樹，新的套件照舊出現在「未分類」（buildTree）。
func defaultPrefsWith(items []pkgMenuItem) *sdk.MenuPrefs {
	// 任務單在「日常」：套件的 group_hint 多半是 daily，接在它後面；宣告 front 的（習慣與健康）排在待辦後面，手機底部列看得到（DEV-135）。
	daily := []string{idOverview, idChat, idReminders}
	for _, it := range items {
		if it.hint == "daily" && it.front {
			daily = append(daily, it.ID)
		}
	}
	daily = append(daily, idMessages, idTasks)
	work := []string{idPolicy}
	extra := map[string][]string{}
	var mine []string
	for _, it := range items {
		switch it.hint {
		case "":
			if it.Tier == sdk.TierMine || it.Tier == sdk.TierInstalled {
				mine = append(mine, it.ID)
			}
		case unplacedGroup:
		case "daily":
			if !it.front {
				daily = append(daily, it.ID)
			}
		case "work":
			work = append(work, it.ID)
		default:
			extra[it.hint] = append(extra[it.hint], it.ID)
		}
	}
	hints := make([]string, 0, len(extra))
	for h := range extra {
		hints = append(hints, h)
	}
	sort.Strings(hints)
	tree := []sdk.MenuPrefEntry{
		{Group: "daily", Title: "日常", Icon: "sun", Children: daily},
		{Group: "work", Title: "工作", Icon: "briefcase", Children: work},
	}
	for _, h := range hints {
		tree = append(tree, sdk.MenuPrefEntry{Group: h, Title: h, Icon: "folder", Children: extra[h]})
	}
	if len(mine) > 0 && extra[myFeaturesGroup] == nil {
		tree = append(tree, sdk.MenuPrefEntry{Group: myFeaturesGroup, Title: "我的套件", Icon: "puzzle", Children: mine})
	} else if len(mine) > 0 {
		// 有套件的 group_hint 剛好叫 mine：併進那個群組，不另開同名群組。
		for i := range tree {
			if tree[i].Group == myFeaturesGroup {
				tree[i].Title, tree[i].Icon = "我的套件", "puzzle"
				tree[i].Children = append(tree[i].Children, mine...)
			}
		}
	}
	placeCoreSections(tree, items)
	// core.settings 不放進樹：buildTree 會把它補在最後，「未分類」才會排在它前面。
	return &sdk.MenuPrefs{Tree: tree}
}

// placeCoreSections 把帶 section 的核心項目（介紹）放進預設樹：同一個 section 有套件項目在樹上，就放在第一個的前面
// （「角色」入口在衣櫃原本的位置）；沒有就接在「日常」最後（MNU-8、D-62）。核心不認套件 id，只看 section。
func placeCoreSections(tree []sdk.MenuPrefEntry, items []pkgMenuItem) {
	sectionOf := map[string]string{}
	for _, it := range items {
		if it.Section != "" {
			sectionOf[it.ID] = it.Section
		}
	}
	for _, core := range coreItems {
		if core.Section == "" {
			continue
		}
		placed := false
		for gi := range tree {
			for ci, c := range tree[gi].Children {
				if sectionOf[c] == core.Section {
					kids := append([]string{}, tree[gi].Children[:ci]...)
					kids = append(kids, core.ID)
					tree[gi].Children = append(kids, tree[gi].Children[ci:]...)
					placed = true
					break
				}
			}
			if placed {
				break
			}
		}
		if !placed {
			tree[0].Children = append(tree[0].Children, core.ID) // tree[0] 一定是「日常」
		}
	}
}

// resolveRenderer 檢查並編譯 menu.toml 的 renderer（`<套件>.<頁面>`）：回編譯後的網址，或給使用者看的白話錯誤。
func (m *Module) resolveRenderer(id string) (bundleURL, errText string) {
	pkgID, pageID, ok := strings.Cut(id, ".")
	if !ok {
		return "", fmt.Sprintf("找不到自訂選單「%s」", id)
	}
	pkg, page, found := m.d.Frontend.FindPage(pkgID, pageID)
	if !found {
		return "", fmt.Sprintf("找不到自訂選單「%s」", id)
	}
	if pkg.Tier != sdk.TierMine {
		return "", fmt.Sprintf("自訂選單「%s」不在自己的套件裡", id)
	}
	if page.Slot != slotShellMenu {
		return "", fmt.Sprintf("自訂選單「%s」不是選單元件（頁面的 slot 要寫 %s）", id, slotShellMenu)
	}
	hash, err := m.d.Frontend.Bundle(pkgID, pageID)
	if err != nil {
		return "", err.Error()
	}
	return "/pkg-bundles/" + hash + ".js", ""
}
