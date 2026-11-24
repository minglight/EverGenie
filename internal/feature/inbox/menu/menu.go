// internal/feature/inbox/menu/menu.go — ai_docs/modules/inbox.md。
package menu

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const (
	idOverview  = "core.overview"
	idChat      = "core.chat"
	idMessages  = "core.messages"
	idTasks     = "core.tasks"
	idReminders = "core.reminders"
	idPolicy    = "core.policy"
	idPersonas  = "core.personas"
	idSettings  = "core.settings"

	unplacedGroup = "unplaced"
	// myFeaturesGroup 是預設樹的「我的套件」群組：自己的與分享的套件項目沒寫 group_hint 時放這（REQ-349）。只是預設，使用者可以搬、改名、刪。
	myFeaturesGroup = "mine"
)

// coreItems 是看板固定的 8 個核心項目（tier 算 official，REQ-348）（P4 共通定案 8；權限申請 Q7 加；Goal 在任務單的分頁裡，不是選單項目）；套件項目在 Compute 另外加（P6-2a）。
// 介紹（DEV-117、D-62）帶 section：套件的選單項目宣告同一個 section 就成為「角色」頁的分頁（核心不寫是哪個套件，法律第 3 條）。
var coreItems = []sdk.MenuItem{
	{ID: idOverview, Title: "今天", Icon: "house", Route: "/", Tier: sdk.TierOfficial},
	{ID: idChat, Title: "聊天", Icon: "message-circle", Route: "/chat", Tier: sdk.TierOfficial},
	{ID: idMessages, Title: "訊息", Icon: "bell", Route: "/messages", Tier: sdk.TierOfficial},
	{ID: idTasks, Title: "任務單", Icon: "clipboard-list", Route: "/tasks", Tier: sdk.TierOfficial},
	{ID: idReminders, Title: "待辦", Icon: "alarm-clock", Route: "/reminders", Tier: sdk.TierOfficial},
	{ID: idPolicy, Title: "權限申請", Icon: "shield-check", Route: "/policy", Tier: sdk.TierOfficial},
	{ID: idPersonas, Title: "介紹", Icon: "contact-round", Route: "/personas", Section: "personas", SectionTitle: "角色", Tier: sdk.TierOfficial},
	{ID: idSettings, Title: "設定", Icon: "settings", Route: "/settings", Tier: sdk.TierOfficial},
}

var groupID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// defaultHidden 是預設收到設定的項目（MNU-10）：平常用不到、有事時「今天」的等你處理會帶路。
var defaultHidden = []string{idPolicy}

// tuckable：今天是首頁、設定是收起來的項目的入口，這兩個不能收。
func tuckable(id string) bool { return id != idOverview && id != idSettings }

// defaultPrefs 是沒有 settings/menu.toml 時的預設樹（schemas/menu-prefs.md）；只是個標記與退路：
// effectivePrefs 看到 isDefault 會整個換成 defaultPrefsWith（加上套件項目、介紹照 MNU-8 放），這裡跟沒裝任何套件時的結果一致。
func defaultPrefs() *sdk.MenuPrefs {
	return &sdk.MenuPrefs{Tree: []sdk.MenuPrefEntry{
		{Group: "daily", Title: "日常", Icon: "sun", Children: []string{idOverview, idChat, idReminders, idMessages, idTasks, idPersonas}},
		{Group: "work", Title: "工作", Icon: "briefcase", Children: []string{idPolicy}},
		{Item: idSettings},
	}, Hidden: defaultHidden}
}

// prefsFile 對應 settings/menu.toml。
type prefsFile struct {
	Schema   int          `toml:"schema"`
	Renderer string       `toml:"renderer,omitempty"`
	Entry    []prefsEntry `toml:"entry,omitempty"`
	Hidden   *[]string    `toml:"hidden"` // 沒寫＝預設（defaultHidden）；空清單＝全部放回選單（MNU-10）
}

type prefsEntry struct {
	Item     string   `toml:"item,omitempty"`
	Group    string   `toml:"group,omitempty"`
	Title    string   `toml:"title,omitempty"`
	Icon     string   `toml:"icon,omitempty"`
	Children []string `toml:"children,omitempty"`
}

// loadPrefsState 讀 settings/menu.toml；沒有檔案、讀不了、格式不對都退回預設樹（不讓選單壞掉），格式不對寫一行 WARN。
// 另外回傳是不是用了預設樹（effectivePrefs 要在預設樹裡加上套件項目）。
func (m *Module) loadPrefsState() (prefs *sdk.MenuPrefs, isDefault bool) {
	if m.homeDir() == "" {
		return defaultPrefs(), true
	}
	data, err := os.ReadFile(m.settingsPath("menu.toml"))
	if errors.Is(err, os.ErrNotExist) {
		return defaultPrefs(), true
	}
	if err != nil {
		m.logger().Warn("menu.toml 讀不了，改用預設選單", "error", err)
		return defaultPrefs(), true
	}
	var f prefsFile
	if _, err := toml.Decode(string(data), &f); err != nil || (f.Schema != 0 && f.Schema != 1) {
		m.logger().Warn("menu.toml 格式不對，改用預設選單", "error", err)
		return defaultPrefs(), true
	}
	p := &sdk.MenuPrefs{Renderer: f.Renderer, Hidden: defaultHidden}
	if f.Hidden != nil {
		p.Hidden = *f.Hidden
	}
	for _, e := range f.Entry {
		p.Tree = append(p.Tree, sdk.MenuPrefEntry{Item: e.Item, Group: e.Group, Title: e.Title, Icon: e.Icon, Children: e.Children})
	}
	if len(p.Tree) == 0 {
		p.Tree = defaultPrefs().Tree
		return p, true
	}
	return p, false
}

// effectivePrefs 是畫面上實際用的樹：使用者的 menu.toml，沒有（或只有 renderer、沒有 entry）就是預設樹加上套件項目
// （group_hint）。GetMenu 與 GetMenuPrefs 都呼叫它，編輯器一打開就是畫面上的樣子（P6-2b）。
func (m *Module) effectivePrefs(items []pkgMenuItem) (prefs *sdk.MenuPrefs, isDefault bool) {
	prefs, isDefault = m.loadPrefsState()
	if isDefault {
		renderer, hidden := prefs.Renderer, prefs.Hidden
		prefs = defaultPrefsWith(items)
		prefs.Renderer, prefs.Hidden = renderer, hidden
	}
	return prefs, isDefault
}

// validatePrefs 擋掉不合規則的選單：最多兩層、項目不重複、群組要有名稱。
func validatePrefs(p *sdk.MenuPrefs) (*sdk.MenuPrefs, error) {
	if p == nil {
		return nil, sdkErr(sdk.ErrorCodeInvalid, "prefs", "請提供選單設定")
	}
	groups := map[string]bool{}
	for _, e := range p.Tree {
		if g := strings.TrimSpace(e.Group); g != "" {
			groups[g] = true
		}
	}
	out := &sdk.MenuPrefs{Renderer: strings.TrimSpace(p.Renderer)}
	if p.Hidden != nil {
		out.Hidden = []string{}
		for _, id := range p.Hidden {
			id = strings.TrimSpace(id)
			if id != "" && tuckable(id) && !slices.Contains(out.Hidden, id) {
				out.Hidden = append(out.Hidden, id)
			}
		}
	}
	seenItems := map[string]bool{}
	seenGroups := map[string]bool{}
	for _, e := range p.Tree {
		item, group := strings.TrimSpace(e.Item), strings.TrimSpace(e.Group)
		switch {
		case (item == "") == (group == ""):
			return nil, sdkErr(sdk.ErrorCodeInvalid, "tree", "每一項要嘛是一個項目、要嘛是一個群組")
		case item != "":
			if len(e.Children) > 0 {
				return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.children", "只有群組可以放項目")
			}
			if groups[item] {
				return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.item", "選單最多兩層，群組不能放在群組裡")
			}
			if seenItems[item] {
				return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.item", "同一個項目只能出現一次："+item)
			}
			seenItems[item] = true
			out.Tree = append(out.Tree, sdk.MenuPrefEntry{Item: item})
		default:
			title := strings.TrimSpace(e.Title)
			if title == "" {
				return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.title", "群組要有名稱")
			}
			if !groupID.MatchString(group) || group == unplacedGroup {
				return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.group", "群組代號請用小寫英文字母開頭的英數字，而且不能叫 "+unplacedGroup)
			}
			if seenGroups[group] {
				return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.group", "群組代號重複："+group)
			}
			seenGroups[group] = true
			children := []string{}
			for _, c := range e.Children {
				c = strings.TrimSpace(c)
				switch {
				case c == "":
					return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.children", "群組裡有空白的項目")
				case groups[c]:
					return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.children", "選單最多兩層，群組裡不能再放群組")
				case seenItems[c]:
					return nil, sdkErr(sdk.ErrorCodeInvalid, "entry.children", "同一個項目只能出現一次："+c)
				}
				seenItems[c] = true
				children = append(children, c)
			}
			out.Tree = append(out.Tree, sdk.MenuPrefEntry{Group: group, Title: title, Icon: strings.TrimSpace(e.Icon), Children: children})
		}
	}
	return out, nil
}

// buildTree 把使用者的樹套在可用項目上：失效項目略過、同一個 section 的項目放在一起（MNU-7）、沒放進樹的進最後的「未分類」群組、
// core.settings 不在樹裡就補在最後（永遠可達）。
func buildTree(pool []sdk.MenuItem, prefs *sdk.MenuPrefs) []sdk.MenuEntry {
	byID := make(map[string]sdk.MenuItem, len(pool))
	for _, it := range pool {
		byID[it.ID] = it
	}
	used := map[string]bool{}
	one := func(id string) (sdk.MenuEntry, bool) {
		it, ok := byID[id]
		if !ok || used[id] {
			return sdk.MenuEntry{}, false
		}
		used[id] = true
		return sdk.MenuEntry{ID: it.ID, Title: it.Title, Icon: it.Icon, Route: it.Route, Badge: it.Badge, Section: it.Section, SectionTitle: it.SectionTitle, Tier: it.Tier}, true
	}
	// leaves 放一個項目；它有 section 時，同一個 section 的項目（含沒放進樹的）在第一次遇到時全部放在這裡，
	// 依 pool 的順序（核心在前、再照套件的 order），看板才合成一個入口、入口連到第一個分頁（MNU-7、D-62）。
	gathered := map[string]bool{}
	leaves := func(id string) []sdk.MenuEntry {
		it, ok := byID[id]
		if !ok || used[id] {
			return nil
		}
		if it.Section == "" {
			l, _ := one(id)
			return []sdk.MenuEntry{l}
		}
		if gathered[it.Section] {
			return nil
		}
		gathered[it.Section] = true
		var out []sdk.MenuEntry
		for _, p := range pool {
			if p.Section != it.Section {
				continue
			}
			if l, ok := one(p.ID); ok {
				out = append(out, l)
			}
		}
		return out
	}
	tree := []sdk.MenuEntry{}
	for _, e := range prefs.Tree {
		if e.Group == "" {
			tree = append(tree, leaves(e.Item)...)
			continue
		}
		g := sdk.MenuEntry{ID: e.Group, Title: e.Title, Icon: e.Icon}
		if g.Title == "" {
			g.Title = e.Group
		}
		for _, c := range e.Children {
			for _, l := range leaves(c) {
				g.Children = append(g.Children, l)
				g.Badge += l.Badge
			}
		}
		if len(g.Children) > 0 {
			tree = append(tree, g)
		}
	}
	var rest []sdk.MenuEntry
	for _, it := range pool {
		if it.ID == idSettings {
			continue
		}
		rest = append(rest, leaves(it.ID)...)
	}
	if len(rest) > 0 {
		g := sdk.MenuEntry{ID: unplacedGroup, Title: "未分類", Icon: "folder", Children: rest}
		for _, l := range rest {
			g.Badge += l.Badge
		}
		tree = append(tree, g)
	}
	if l, ok := one(idSettings); ok {
		tree = append(tree, l)
	}
	return tree
}

// Compute 一次算好完整的選單（樹＋可用項目＋徽章），前端拿到才畫（MNU-1；等太久或讀不到時看板先用基本選單，WEB-175）。
// 徽章的數字都從首頁彙整（overview）來，跟首頁「等你處理」是同一份，只算一次。
func (m *Module) Compute(ctx context.Context) (*sdk.Menu, error) {
	data, err := m.badges(ctx)
	if err != nil {
		return nil, err
	}
	pool := make([]sdk.MenuItem, len(coreItems))
	copy(pool, coreItems)
	for i := range pool {
		switch pool[i].ID {
		case idOverview:
			pool[i].Badge = data.WaitingTotal
		case idChat:
			pool[i].Badge = int32(len(data.WaitingThreads))
		case idMessages:
			pool[i].Badge = data.Unread
		case idTasks:
			pool[i].Badge = int32(len(data.WaitingTasks))
		}
	}
	items := m.packageMenuItems(ctx)
	for _, it := range items {
		pool = append(pool, it.MenuItem)
	}
	prefs, _ := m.effectivePrefs(items)
	renderer := prefs.Renderer
	placed := make([]sdk.MenuItem, 0, len(pool))
	var tucked []sdk.MenuItem
	for _, it := range pool {
		if tuckable(it.ID) && slices.Contains(prefs.Hidden, it.ID) {
			tucked = append(tucked, it)
		} else {
			placed = append(placed, it)
		}
	}
	menu := &sdk.Menu{Tree: buildTree(placed, prefs), Pool: pool, Tucked: tucked}
	if renderer != "" {
		menu.Renderer = renderer
		menu.RendererBundleURL, menu.RendererError = m.resolveRenderer(renderer)
	}
	return menu, nil
}

// savePrefs 驗證後以原子寫入存成 settings/menu.toml。
func (m *Module) savePrefs(p *sdk.MenuPrefs) (*sdk.MenuPrefs, error) {
	if m.homeDir() == "" {
		return nil, sdkErr(sdk.ErrorCodeUnavailable, "", "看板還沒有家，無法儲存選單")
	}
	clean, err := validatePrefs(p)
	if err != nil {
		return nil, err
	}
	f := prefsFile{Schema: 1, Renderer: clean.Renderer}
	if clean.Hidden != nil {
		f.Hidden = &clean.Hidden
	}
	for _, e := range clean.Tree {
		f.Entry = append(f.Entry, prefsEntry{Item: e.Item, Group: e.Group, Title: e.Title, Icon: e.Icon, Children: e.Children})
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(f); err != nil {
		m.logger().Error("menu.toml 編碼失敗", "error", err)
		return nil, sdkErr(sdk.ErrorCodeInternal, "", "內部錯誤")
	}
	m.prefsMu.Lock()
	defer m.prefsMu.Unlock()
	path := m.settingsPath("menu.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		m.logger().Error("建立 settings 資料夾失敗", "error", err)
		return nil, sdkErr(sdk.ErrorCodeUnavailable, "", "無法儲存選單，請檢查家的存取權限")
	}
	if err := platform.AtomicWrite(path, buf.Bytes(), 0o600); err != nil {
		m.logger().Error("儲存 menu.toml 失敗", "error", err)
		return nil, sdkErr(sdk.ErrorCodeUnavailable, "", "無法儲存選單，請檢查家的存取權限")
	}
	return clean, nil
}
