package pkg

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// SupportedAPI 是核心認得的套件協定版本；不認得的套件拒載（schemas/package.md `api`）。
var SupportedAPI = map[string]bool{"evergenie.v1": true}

var (
	idPattern      = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?$`)
	// recordIDPattern 是 until_record 的編號：一段，不能有斜線或跳出資料夾。
	recordIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// Manifest 是 package.toml 的內容（schemas/package.md）。層級（tier）由所在資料夾決定，不在這裡。
type Manifest struct {
	Schema       int       `toml:"schema"`
	ID           string    `toml:"id"`
	Name         string    `toml:"name"`
	Version      string    `toml:"version"`
	Description  string    `toml:"description"`
	Kind         string    `toml:"kind"`
	API          string    `toml:"api"`
	Optional     bool      `toml:"optional"` // 選用套件：沒有明確設定時預設不啟用（REQ-176）
	DevOnly      bool      `toml:"dev_only"` // 只給開發用的家：看板設定頁只在開發家（[runtime] env = "dev"）列出來（PKG-56，2026-11-04）
	Core         string    `toml:"core"`     // 要求的核心版本（寫法同 requires[].version）；不符就拒載，認不出核心版本（開發版）不擋（REQ-282）
	Requires     []Require `toml:"requires"`
	Capabilities struct {
		Read   []string `toml:"read"`
		Write  []string `toml:"write"`
		Events []string `toml:"events"`
	} `toml:"capabilities"`
	Program struct {
		Entry   string `toml:"entry"`
		Restart string `toml:"restart"`
	} `toml:"program"`
	Contributes struct {
		Avatars string           `toml:"avatars"` // 公開資料夾裡放「現在的頭像」的子資料夾（<子資料夾>/<人設>.png|.mp4，D-29）
		Agent   []ContribAgent   `toml:"agent"`
		Persona []ContribPersona `toml:"persona"`
		Job     []ContribJob     `toml:"job"`
		Page    []ContribPage    `toml:"page"`
		Menu    []ContribMenu    `toml:"menu"`
		Data    []ContribData    `toml:"data"`
	} `toml:"contributes"`
	Extension []ExtensionDecl `toml:"extension"`
	Migration []MigrationDecl `toml:"migration"` // 套件自己帶的資料轉換（migrate.go，DEV-154）
	Fork      *ForkDecl       `toml:"fork"`

	dataKinds []DataKind      // 驗證時解析好的資料種類（contributes.data，D-28）
	agents    []AgentTemplate // 驗證時解析好的職位樣板（contributes.agent；只有知道資料夾時才有）
}

type Require struct {
	ID      string `toml:"id"`
	Version string `toml:"version"`
}
type ContribAgent struct {
	ID       string `toml:"id"`
	Template string `toml:"template"`
}
type ContribPersona struct {
	Slug string `toml:"slug"`
	Path string `toml:"path"`
}
type ContribJob struct {
	Agent string `toml:"agent"`
	ID    string `toml:"id"`
	Path  string `toml:"path"`
}
type ContribPage struct {
	ID        string `toml:"id"`
	Title     string `toml:"title"`
	Route     string `toml:"route"`
	Component string `toml:"component"`
	Slot      string `toml:"slot"`
	SlotFront bool   `toml:"slot_front"`
	// Chat：看板在頁面旁邊放聊天；ChatAgent 是預設的秘書（職位 id）。
	Chat      bool   `toml:"chat"`
	ChatAgent string `toml:"chat_agent"`
	// Merge：看板連多台時這頁可以把各台的資料合在一起看（S3；S3 只收宣告，官方套件先都不開）。
	Merge bool `toml:"merge"`
	// ShowWhen：只給掛在 slot 的頁面用，條件不成立就不列（寫法同選單的 show_when，由 menu 執行時判斷，MNU-11）。
	ShowWhen string `toml:"show_when"`
}
type ContribMenu struct {
	ID        string `toml:"id"`
	Title     string `toml:"title"`
	Icon      string `toml:"icon"`
	GroupHint string `toml:"group_hint"`
	Front     bool   `toml:"front"`
	Order     int    `toml:"order"`
	ShowWhen  string `toml:"show_when"`
	Page      string `toml:"page"`
	// Section：同一頁的分頁（看板合成一個入口）；SectionTitle 是那個入口的名稱。
	Section      string `toml:"section"`
	SectionTitle string `toml:"section_title"`
}
type ExtensionDecl struct {
	ID          string `toml:"id"`
	Kind        string `toml:"kind"`
	Description string `toml:"description"`
	Default     string `toml:"default"`
	// OwnerOnly：這個擴充點的值只有主人本人能改（秘書、系統、套件寫都被拒；讀照常）。
	OwnerOnly bool `toml:"owner_only"`
	// Agent：只給 prompt_append 用，這一段接在哪個職位的指令後面；空＝每一位秘書都接。
	Agent string `toml:"agent"`
	// UntilRecord：只給 prompt_append 用，寫成「<這個套件的資料種類>/<編號>」；那筆紀錄出現（含封存的）之後就不再帶這一段。
	// 給「只講一次」的話用：記在家的紀錄裡，換秘書、重開都一樣（DEV-157）。
	UntilRecord string `toml:"until_record"`
}
type ForkDecl struct {
	Of          string `toml:"of"`
	BaseVersion string `toml:"base_version"`
	ForkedAt    string `toml:"forked_at"`
}

// Binary 是附程式套件的執行檔名稱：program.entry 的最後一段（cmd/my-tool → my-tool）。
func (m *Manifest) Binary() string { return path.Base(filepath.ToSlash(m.Program.Entry)) }

func (m *Manifest) restart() string {
	if m.Program.Restart == "" {
		return "always"
	}
	return m.Program.Restart
}

// ParseManifest 讀並驗證 dir/package.toml。tier 用來檢查 [fork] 只出現在 mine。
// 錯誤都是白話的 INVALID；呼叫者把它變成這個套件自己的 rejected 狀態，不影響別的套件。
func ParseManifest(dir string, tier sdk.Tier) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "package.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, platform.Invalid("package.toml", "這個資料夾裡沒有 package.toml")
		}
		return nil, platform.Invalid("package.toml", "讀不了 package.toml")
	}
	return parseManifestBytes(data, dir, tier)
}

func parseManifestBytes(data []byte, dir string, tier sdk.Tier) (*Manifest, error) {
	var m Manifest
	md, err := toml.Decode(string(data), &m)
	if err != nil {
		where := ""
		if pe, ok := err.(toml.ParseError); ok && pe.Position.Line > 0 {
			where = fmt.Sprintf("（第 %d 行）", pe.Position.Line)
		}
		return nil, platform.Invalid("package.toml", "package.toml 的格式不對"+where)
	}
	if undec := md.Undecoded(); len(undec) > 0 {
		keys := make([]string, 0, len(undec))
		for _, k := range undec {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		return nil, platform.Invalid(keys[0], "package.toml 有不認得的欄位："+strings.Join(keys, "、"))
	}
	if err := m.validate(dir, tier); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) validate(dir string, tier sdk.Tier) error {
	bad := func(field, msg string) error { return platform.Invalid(field, msg) }
	if m.Schema != 1 {
		return bad("schema", "schema 必須是 1")
	}
	if !idPattern.MatchString(m.ID) {
		return bad("id", "id 要是小寫英文、數字與 -（例：my-habit）")
	}
	if dir != "" && filepath.Base(dir) != m.ID {
		return bad("id", fmt.Sprintf("id「%s」要跟資料夾名稱「%s」一樣", m.ID, filepath.Base(dir)))
	}
	if strings.TrimSpace(m.Name) == "" {
		return bad("name", "要寫顯示名稱 name")
	}
	if !versionPattern.MatchString(m.Version) {
		return bad("version", "version 要是 semver，例：1.2.0")
	}
	if strings.TrimSpace(m.Description) == "" {
		return bad("description", "要用一句話寫 description")
	}
	switch m.Kind {
	case string(sdk.PackageKindData), string(sdk.PackageKindProgram):
	default:
		return bad("kind", "kind 要是 data 或 program")
	}
	if m.API == "" {
		return bad("api", "要寫 api（它講的協定版本），例：evergenie.v1")
	}
	if !SupportedAPI[m.API] {
		return bad("api", fmt.Sprintf("這個核心不認得協定版本「%s」，這個套件不會載入", m.API))
	}
	if _, err := parseConstraint(m.Core); err != nil {
		return bad("core", fmt.Sprintf("core 的版本寫法看不懂：%s（例：^0.2.0 或 >=0.2.0）", m.Core))
	}
	seen := map[string]bool{}
	for _, r := range m.Requires {
		if !idPattern.MatchString(r.ID) {
			return bad("requires", "requires 裡的 id 不合格式")
		}
		if r.ID == m.ID {
			return bad("requires", "套件不能依賴自己")
		}
		if seen[r.ID] {
			return bad("requires", fmt.Sprintf("requires 重複列了「%s」", r.ID))
		}
		seen[r.ID] = true
		if _, err := parseConstraint(r.Version); err != nil {
			return bad("requires", fmt.Sprintf("requires「%s」的版本寫法看不懂：%s", r.ID, r.Version))
		}
	}
	if m.Kind == string(sdk.PackageKindProgram) {
		if m.Program.Entry == "" {
			return bad("program.entry", "附程式的套件要寫 program.entry（Go main package 的路徑）")
		}
		if !safeRel(m.Program.Entry) {
			return bad("program.entry", "program.entry 必須是套件資料夾裡面的相對路徑")
		}
		if dir != "" {
			if info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(m.Program.Entry))); err != nil || !info.IsDir() {
				return bad("program.entry", fmt.Sprintf("找不到 program.entry 指的資料夾「%s」", m.Program.Entry))
			}
		}
		switch m.Program.Restart {
		case "", "always", "on-failure", "never":
		default:
			return bad("program.restart", "program.restart 要是 always、on-failure 或 never")
		}
	} else if m.Program.Entry != "" || m.Program.Restart != "" {
		return bad("program", "純資料的套件（kind = \"data\"）不能有 [program]")
	}
	exists := func(rel string) bool {
		if dir == "" {
			return true
		}
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		return err == nil
	}
	pages := map[string]bool{}
	for _, p := range m.Contributes.Page {
		if p.ID == "" || pages[p.ID] {
			return bad("contributes.page", "頁面 id 不能空白或重複")
		}
		pages[p.ID] = true
		if p.Component != "" && (!safeRel(p.Component) || !exists(p.Component)) {
			return bad("contributes.page", fmt.Sprintf("找不到頁面「%s」的檔案 %s", p.ID, p.Component))
		}
		if p.ChatAgent != "" && (!p.Chat || !agentIDPattern.MatchString(p.ChatAgent)) {
			return bad("contributes.page", fmt.Sprintf("頁面「%s」的 chat_agent 要配 chat = true，而且是職位 id（小寫英文、數字、減號）", p.ID))
		}
		if p.Chat && p.Slot != "" {
			return bad("contributes.page", fmt.Sprintf("頁面「%s」掛在 slot，不能有 chat", p.ID))
		}
		// show_when 只管 slot 頁面要不要列（PKG-69）；有自己網址的頁面要藏就別放選單（選單項目自己有 show_when）。
		// 寫法不在這裡檢查：新核心多認得的寫法，舊核心只會把那頁藏起來，不會整個套件被拒（MNU-11）。
		// 選單元件（slot = shell.menu）不是頁面，不在頁面清單裡，show_when 也不會生效，一樣拒絕。
		if strings.TrimSpace(p.ShowWhen) != "" && (p.Slot == "" || p.Slot == "shell.menu") {
			return bad("contributes.page", fmt.Sprintf("頁面「%s」的 show_when 只能用在掛在 slot 的頁面（shell.menu 除外；要藏選單項目就寫在 [[contributes.menu]] 的 show_when）", p.ID))
		}
		// route 是別名（網址一律 /p/<套件>/<頁面>，REQ-310；看板打開 route 會轉過去，DEV-91）；寫了會蓋住核心網址的字一律拒絕，免得看起來像能搶走 /tasks 這類網址（REQ-352）。
		if shadowsCoreRoute(p.Route) {
			return bad("contributes.page", fmt.Sprintf("頁面「%s」的 route「%s」會蓋住看板的核心網址；套件頁面的網址固定是 /p/%s/%s，route 可以不寫", p.ID, p.Route, m.ID, p.ID))
		}
	}
	menus := map[string]bool{}
	for _, mi := range m.Contributes.Menu {
		if mi.ID == "" || menus[mi.ID] {
			return bad("contributes.menu", "選單項目 id 不能空白或重複")
		}
		menus[mi.ID] = true
		if !pages[mi.Page] {
			return bad("contributes.menu", fmt.Sprintf("選單項目「%s」指到不存在的頁面「%s」", mi.ID, mi.Page))
		}
		if mi.Section != "" && !sectionPattern.MatchString(mi.Section) {
			return bad("contributes.menu", fmt.Sprintf("選單項目「%s」的 section「%s」要是小寫英文、數字、減號（英文開頭，最多 31 個字）", mi.ID, mi.Section))
		}
		if mi.Section == "" && mi.SectionTitle != "" {
			return bad("contributes.menu", fmt.Sprintf("選單項目「%s」有 section_title 卻沒有 section", mi.ID))
		}
	}
	agentIDs := map[string]bool{}
	var agents []AgentTemplate
	for _, a := range m.Contributes.Agent {
		if a.ID == "" || !safeRel(a.Template) || !exists(a.Template) {
			return bad("contributes.agent", fmt.Sprintf("找不到職位「%s」的樣板檔 %s", a.ID, a.Template))
		}
		if !agentIDPattern.MatchString(a.ID) || agentIDs[a.ID] {
			return bad("contributes.agent", fmt.Sprintf("職位 id「%s」要是小寫英文、數字、減號（英文開頭，2 到 31 個字），而且不能重複", a.ID))
		}
		agentIDs[a.ID] = true
		if dir != "" {
			t, err := parseAgentTemplate(filepath.Join(dir, filepath.FromSlash(a.Template)), a.ID, a.Template)
			if err != nil {
				return err
			}
			agents = append(agents, t)
		}
	}
	m.agents = agents
	personaSlugs := map[string]bool{}
	for _, p := range m.Contributes.Persona {
		if p.Slug == "" || !safeRel(p.Path) || !exists(p.Path) {
			return bad("contributes.persona", fmt.Sprintf("找不到角色「%s」的檔案 %s", p.Slug, p.Path))
		}
		if !personaref.SlugPattern.MatchString(p.Slug) || personaSlugs[p.Slug] {
			return bad("contributes.persona", fmt.Sprintf("角色代號「%s」要是小寫英文加數字（英文開頭），而且同一個套件裡不能重複", p.Slug))
		}
		personaSlugs[p.Slug] = true
	}
	for _, j := range m.Contributes.Job {
		if j.ID == "" || j.Agent == "" || !safeRel(j.Path) || !exists(j.Path) {
			return bad("contributes.job", fmt.Sprintf("排程工作「%s」的樣板檔 %s 不存在", j.ID, j.Path))
		}
	}
	if a := m.Contributes.Avatars; a != "" && (m.Kind != string(sdk.PackageKindProgram) || !idPattern.MatchString(a)) {
		return bad("contributes.avatars", "avatars 只給附程式的套件用，值是公開資料夾裡的一個子資料夾名稱（小寫英文、數字與 -）")
	}
	kinds, err := parseDataKinds(m.Contributes.Data, dir)
	if err != nil {
		return err
	}
	m.dataKinds = kinds
	if err := validateMigrations(m.Migration, m.Contributes.Data); err != nil {
		return err
	}
	exts := map[string]bool{}
	for _, e := range m.Extension {
		if !idPattern.MatchString(e.ID) || exts[e.ID] {
			return bad("extension", "擴充點 id 要是小寫英文、數字與 -，而且不能重複")
		}
		exts[e.ID] = true
		switch e.Kind {
		case string(sdk.ExtensionKindSetting), string(sdk.ExtensionKindPromptAppend), string(sdk.ExtensionKindPageSlot):
		default:
			return bad("extension", fmt.Sprintf("擴充點「%s」的 kind 要是 setting、prompt_append 或 page_slot", e.ID))
		}
		if e.Agent != "" && (e.Kind != string(sdk.ExtensionKindPromptAppend) || !idPattern.MatchString(e.Agent)) {
			return bad("extension", fmt.Sprintf("擴充點「%s」的 agent 只能用在 prompt_append，而且要是職位 id（小寫英文、數字與 -）", e.ID))
		}
		// 說明會出現在秘書的指令與設定頁：只能一行、不能太長（不然套件可以靠它塞一大段話，憲法第 10 條）。
		if strings.ContainsAny(e.Description, "\r\n") || len(e.Description) > extensionDescMax {
			return bad("extension", fmt.Sprintf("擴充點「%s」的 description 只能寫一行，而且不超過 %d 個位元組", e.ID, extensionDescMax))
		}
		if e.UntilRecord != "" {
			kind, id, ok := strings.Cut(e.UntilRecord, "/")
			own := false
			for _, d := range m.Contributes.Data {
				own = own || d.Kind == kind
			}
			if e.Kind != string(sdk.ExtensionKindPromptAppend) || !ok || !own || !recordIDPattern.MatchString(id) {
				return bad("extension", fmt.Sprintf("擴充點「%s」的 until_record 只能用在 prompt_append，寫成「<這個套件宣告的資料種類>/<編號>」（例：welcome/main）", e.ID))
			}
		}
		if e.Kind == string(sdk.ExtensionKindPromptAppend) && len(e.Default) > promptAppendMax {
			return bad("extension", fmt.Sprintf("擴充點「%s」的 default 太長了（%d 個位元組，上限 %d）", e.ID, len(e.Default), promptAppendMax))
		}
	}
	if m.Fork != nil {
		if tier != sdk.TierMine {
			return bad("fork", "[fork] 只能出現在 packages/mine/ 的套件")
		}
		if m.Fork.Of == "" || !versionPattern.MatchString(m.Fork.BaseVersion) {
			return bad("fork", "[fork] 要有 of 與 base_version（semver）")
		}
	}
	return nil
}

// safeRel 只接受套件資料夾裡面的相對路徑（不能是絕對路徑，也不能往上跳）。
func safeRel(p string) bool {
	if p == "" || filepath.IsAbs(p) || strings.HasPrefix(p, "/") {
		return false
	}
	clean := path.Clean(filepath.ToSlash(p))
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// Public 把 manifest 變成 API 上的 Package（狀態由呼叫者填）。
func (m *Manifest) Public(tier sdk.Tier) sdk.Package {
	p := sdk.Package{ID: m.ID, Name: m.Name, Version: m.Version, Description: m.Description, Kind: sdk.PackageKind(m.Kind), Tier: tier, API: m.API, Optional: m.Optional, DevOnly: m.DevOnly, Avatars: m.Contributes.Avatars, Enabled: true}
	for _, r := range m.Requires {
		p.Requires = append(p.Requires, sdk.Requirement{ID: r.ID, Version: r.Version})
	}
	if len(m.Capabilities.Read)+len(m.Capabilities.Write)+len(m.Capabilities.Events) > 0 {
		p.Capabilities = &sdk.Capabilities{Read: m.Capabilities.Read, Write: m.Capabilities.Write, Events: m.Capabilities.Events}
	}
	for _, mi := range m.Contributes.Menu {
		p.Menu = append(p.Menu, sdk.MenuItemDecl{ID: mi.ID, Title: mi.Title, Icon: mi.Icon, GroupHint: mi.GroupHint, Front: mi.Front, Order: int32(mi.Order), ShowWhen: mi.ShowWhen, Page: mi.Page, Section: mi.Section, SectionTitle: mi.SectionTitle})
	}
	for _, pg := range m.Contributes.Page {
		p.Pages = append(p.Pages, sdk.PageDecl{ID: pg.ID, Title: pg.Title, Route: pg.Route, Component: pg.Component, Slot: pg.Slot, SlotFront: pg.SlotFront, Chat: pg.Chat, ChatAgent: pg.ChatAgent, Merge: pg.Merge, ShowWhen: strings.TrimSpace(pg.ShowWhen)})
	}
	for _, k := range m.dataKinds {
		p.Data = append(p.Data, k.decl())
	}
	for _, a := range m.agents {
		p.Agents = append(p.Agents, sdk.AgentDecl{ID: a.ID, Duty: a.Duty, Color: a.Color, ColorDark: a.ColorDark, Optional: a.Optional})
	}
	for _, e := range m.Extension {
		p.Extensions = append(p.Extensions, sdk.ExtensionPoint{ID: e.ID, Kind: sdk.ExtensionKind(e.Kind), Description: e.Description, DefaultValue: e.Default, OwnerOnly: e.OwnerOnly, Agent: e.Agent})
	}
	if m.Fork != nil {
		f := &sdk.ForkInfo{Of: m.Fork.Of, BaseVersion: m.Fork.BaseVersion}
		if t, err := parseTime(m.Fork.ForkedAt); err == nil {
			f.ForkedAt = t
		}
		p.Fork = f
	}
	return p
}
