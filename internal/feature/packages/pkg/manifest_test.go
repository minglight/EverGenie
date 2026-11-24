package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// TestManifestAcceptsValid：純資料與附程式的 manifest、選單／頁面／擴充點、版本寫法都讀得進來。
// Guards: REQ-91
func TestManifestAcceptsValid(t *testing.T) {
	h := newHarness(t)
	dir := h.write(sdk.TierMine, "my-habit", dataPkg("my-habit", "0.1.0", `
[[requires]]
id = "base"
version = "^1.2.0"
[[contributes.page]]
id = "main"
title = "習慣"
route = "/habits"
component = "pages/main.tsx"
[[contributes.menu]]
id = "main"
title = "習慣"
icon = "check"
group_hint = "packages"
order = 10
page = "main"
[[extension]]
id = "greeting"
kind = "prompt_append"
description = "開場多說一句"
`), map[string]string{"pages/main.tsx": "export default function Main() { return null }\n"})
	m, err := ParseManifest(dir, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	p := m.Public(sdk.TierMine)
	if p.Kind != sdk.PackageKindData || len(p.Menu) != 1 || len(p.Pages) != 1 || len(p.Extensions) != 1 || p.Requires[0].Version != "^1.2.0" {
		t.Fatalf("Public = %+v", p)
	}
	prog := h.write(sdk.TierMine, "watcher", programPkg("watcher", "on-failure", ""), programFiles("watcher"))
	pm, err := ParseManifest(prog, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	if pm.Binary() != "watcher" || pm.restart() != "on-failure" {
		t.Fatalf("binary=%s restart=%s", pm.Binary(), pm.restart())
	}
	for _, c := range []struct {
		want, version string
		ok            bool
	}{{"", "9.9.9", true}, {"1.2.0", "1.3.0", true}, {"1.2.0", "2.0.0", false}, {"^1.2.0", "1.1.9", false}, {">=1.2.0", "3.0.0", true}, {">=1.2.0", "1.2.0-rc.1", false}} {
		k, err := parseConstraint(c.want)
		if err != nil || k.allows(c.version) != c.ok {
			t.Errorf("constraint %q allows %q = %v，要 %v（%v）", c.want, c.version, k.allows(c.version), c.ok, err)
		}
	}
}

// TestManifestRejectsBadFields：每一種不合格都回 INVALID，並指出是哪個欄位。
// Guards: REQ-91
func TestManifestRejectsBadFields(t *testing.T) {
	ok := dataPkg("x", "1.0.0", "")
	cases := []struct {
		name, field, text string
		tier              sdk.Tier
	}{
		{"unknown field", "color", ok + "color = \"red\"\n", sdk.TierMine},
		{"bad id", "id", strings.Replace(ok, `id = "x"`, `id = "X_1"`, 1), sdk.TierMine},
		{"id != folder", "id", strings.Replace(ok, `id = "x"`, `id = "y"`, 1), sdk.TierMine},
		{"missing name", "name", strings.Replace(ok, `name = "x名"`, `name = ""`, 1), sdk.TierMine},
		{"bad version", "version", strings.Replace(ok, `"1.0.0"`, `"v1"`, 1), sdk.TierMine},
		{"bad kind", "kind", strings.Replace(ok, `"data"`, `"plugin"`, 1), sdk.TierMine},
		{"unknown api", "api", strings.Replace(ok, `"evergenie.v1"`, `"evergenie.v9"`, 1), sdk.TierMine},
		{"data with program", "program", ok + "[program]\nentry = \"cmd/x\"\n", sdk.TierMine},
		{"program without entry", "program.entry", strings.Replace(ok, `"data"`, `"program"`, 1), sdk.TierMine},
		{"entry escapes", "program.entry", strings.Replace(ok, `"data"`, `"program"`, 1) + "[program]\nentry = \"../evil\"\n", sdk.TierMine},
		{"bad restart", "program.restart", strings.Replace(ok, `"data"`, `"program"`, 1) + "[program]\nentry = \"cmd/x\"\nrestart = \"sometimes\"\n", sdk.TierMine},
		{"menu to missing page", "contributes.menu", ok + "[[contributes.menu]]\nid = \"m\"\ntitle = \"t\"\norder = 1\npage = \"nope\"\n", sdk.TierMine},
		{"page file missing", "contributes.page", ok + "[[contributes.page]]\nid = \"p\"\ncomponent = \"pages/none.tsx\"\n", sdk.TierMine},
		{"bad extension kind", "extension", ok + "[[extension]]\nid = \"e\"\nkind = \"magic\"\n", sdk.TierMine},
		{"requires self", "requires", ok + "[[requires]]\nid = \"x\"\n", sdk.TierMine},
		{"requires bad version", "requires", ok + "[[requires]]\nid = \"y\"\nversion = \"latest\"\n", sdk.TierMine},
		{"fork outside mine", "fork", ok + "[fork]\nof = \"x\"\nbase_version = \"1.0.0\"\n", sdk.TierInstalled},
		{"broken toml", "package.toml", "schema = = 1\n", sdk.TierMine},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "x")
			if err := os.MkdirAll(filepath.Join(dir, "cmd", "x"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "package.toml"), []byte(c.text), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := ParseManifest(dir, c.tier)
			pe, isErr := err.(*platform.Err)
			if !isErr || pe.Code != sdk.ErrorCodeInvalid || pe.Field != c.field || pe.Message == "" {
				t.Fatalf("要 INVALID field=%s，實際 %#v", c.field, err)
			}
		})
	}
}

// TestOfficialPackagesDiscovered：repo 裡 packages/official/ 的每一包都照新規則讀得進來，沒有被拒。
// Guards: REQ-91, ADM-39
func TestOfficialPackagesDiscovered(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "official"))
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.m.SetOfficialDir(root)
	h.m.SetExternal(claimAll{}) // 官方引擎由 engine 模組監督，這裡不啟動
	h.start()
	got := map[string]sdk.Package{}
	for _, p := range h.list() {
		got[p.ID] = p
	}
	n := 0
	for _, d := range dirs {
		if !d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			continue
		}
		n++
		p, ok := got[d.Name()]
		if !ok || p.Tier != sdk.TierOfficial || p.State == sdk.PackageStateRejected {
			t.Errorf("官方套件 %s：%+v", d.Name(), p)
		}
	}
	if n == 0 || len(h.procs) != 0 {
		t.Fatalf("找到 %d 包官方套件，啟動了 %d 個行程", n, len(h.procs))
	}
	// 基本職位是四個一般職位，沒有選用的；生圖 art 只在開發工坊 admin（dev_only、選用套件），而且也標選用（ADM-39）。
	optional := map[string]bool{}
	for _, a := range got["secretaries"].Agents {
		optional[a.ID] = a.Optional
	}
	if len(optional) != 4 || optional["life"] || optional["work"] || optional["code"] || optional["system"] {
		t.Errorf("secretaries 的職位：%v", optional)
	}
	admin := got["admin"]
	if !admin.DevOnly || !admin.Optional || len(admin.Agents) != 1 || admin.Agents[0].ID != "art" || !admin.Agents[0].Optional {
		t.Errorf("admin 要是 dev_only 的選用套件、只帶選用的 art：%+v", admin)
	}
}

type claimAll struct{}

func (claimAll) Claim(string) (sdk.PackageState, string, bool) {
	return sdk.PackageStateStopped, "", true
}

// TestManifestMenuSection：選單項目可以宣告 section（同一頁的分頁）與 section_title；格式不對、只有標題都拒絕。
// Guards: REQ-235
func TestManifestMenuSection(t *testing.T) {
	h := newHarness(t)
	page := "[[contributes.page]]\nid = \"main\"\ntitle = \"飲食\"\ncomponent = \"pages/main.tsx\"\n"
	files := map[string]string{"pages/main.tsx": "export default function Main() { return null }\n"}
	dir := h.write(sdk.TierMine, "meals", dataPkg("meals", "0.1.0", page+
		"[[contributes.menu]]\nid = \"main\"\ntitle = \"飲食\"\norder = 1\npage = \"main\"\nsection = \"health\"\nsection_title = \"健康\"\n"), files)
	m, err := ParseManifest(dir, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Public(sdk.TierMine).Menu[0]; got.Section != "health" || got.SectionTitle != "健康" {
		t.Fatalf("menu = %+v", got)
	}
	for name, menu := range map[string]string{
		"bad section":      "section = \"Health!\"\n",
		"title only":       "section_title = \"健康\"\n",
		"section too long": "section = \"h" + strings.Repeat("x", 31) + "\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := h.write(sdk.TierMine, "meals", dataPkg("meals", "0.1.0", page+
				"[[contributes.menu]]\nid = \"main\"\ntitle = \"飲食\"\norder = 1\npage = \"main\"\n"+menu), files)
			_, err := ParseManifest(dir, sdk.TierMine)
			pe, isErr := err.(*platform.Err)
			if !isErr || pe.Code != sdk.ErrorCodeInvalid || pe.Field != "contributes.menu" {
				t.Fatalf("要 INVALID contributes.menu，實際 %#v", err)
			}
		})
	}
}

// TestManifestPageChat：頁面可以宣告 chat（看板在旁邊放聊天）與預設秘書 chat_agent；只有 chat_agent、格式不對、掛在 slot 的都拒絕。
// Guards: REQ-235
func TestManifestPageChat(t *testing.T) {
	h := newHarness(t)
	files := map[string]string{"pages/main.tsx": "export default function Main() { return null }\n"}
	page := func(extra string) string {
		return "[[contributes.page]]\nid = \"main\"\ntitle = \"飲食\"\ncomponent = \"pages/main.tsx\"\n" + extra
	}
	dir := h.write(sdk.TierMine, "meals", dataPkg("meals", "0.1.0", page("chat = true\nchat_agent = \"nutrition\"\n")), files)
	m, err := ParseManifest(dir, sdk.TierMine)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Public(sdk.TierMine).Pages[0]; !got.Chat || got.ChatAgent != "nutrition" {
		t.Fatalf("page = %+v", got)
	}
	for name, extra := range map[string]string{
		"agent without chat": "chat_agent = \"nutrition\"\n",
		"bad agent":          "chat = true\nchat_agent = \"Nutrition!\"\n",
		"chat on slot":       "chat = true\nslot = \"core.overview.cards\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := h.write(sdk.TierMine, "meals", dataPkg("meals", "0.1.0", page(extra)), files)
			_, err := ParseManifest(dir, sdk.TierMine)
			pe, isErr := err.(*platform.Err)
			if !isErr || pe.Code != sdk.ErrorCodeInvalid || pe.Field != "contributes.page" {
				t.Fatalf("要 INVALID contributes.page，實際 %#v", err)
			}
		})
	}
}

// 頁面可以宣告 merge（看板連多台時可合併各台資料）；沒寫就是 false，原樣傳到 PageDecl。
func TestManifestPageMergeDeclaration(t *testing.T) {
	h := newHarness(t)
	files := map[string]string{"pages/main.tsx": "export default function Main() { return null }\n"}
	for extra, want := range map[string]bool{"merge = true\n": true, "": false} {
		dir := h.write(sdk.TierMine, "meals", dataPkg("meals", "0.1.0", "[[contributes.page]]\nid = \"main\"\ntitle = \"飲食\"\ncomponent = \"pages/main.tsx\"\n"+extra), files)
		m, err := ParseManifest(dir, sdk.TierMine)
		if err != nil {
			t.Fatal(err)
		}
		if got := m.Public(sdk.TierMine).Pages[0].Merge; got != want {
			t.Errorf("%q：merge = %v，應該是 %v", extra, got, want)
		}
	}
}

// slot 頁面可以宣告 show_when（API 支援才列，MNU-11）：原樣（去掉前後空白）傳到 PageDecl；沒有 slot 的頁面寫了就拒絕（PKG-69）。
// 寫法本身不在這裡檢查（新核心多認得的寫法在舊核心只會藏起那頁，不會整個套件被拒）。
func TestManifestPageShowWhen(t *testing.T) {
	h := newHarness(t)
	files := map[string]string{"pages/main.tsx": "export default function Main() { return null }\n"}
	page := func(extra string) string {
		return "[[contributes.page]]\nid = \"main\"\ntitle = \"修改\"\ncomponent = \"pages/main.tsx\"\n" + extra
	}
	for extra, want := range map[string]string{
		"slot = \"core.persona.tab\"\nshow_when = \" cap:closet.edit \"\n": "cap:closet.edit",
		"slot = \"core.persona.tab\"\nshow_when = \"someday maybe\"\n":     "someday maybe",
		"slot = \"core.persona.tab\"\n":                                    "",
	} {
		dir := h.write(sdk.TierMine, "studio", dataPkg("studio", "0.1.0", page(extra)), files)
		m, err := ParseManifest(dir, sdk.TierMine)
		if err != nil {
			t.Fatalf("%q：%v", extra, err)
		}
		if got := m.Public(sdk.TierMine).Pages[0].ShowWhen; got != want {
			t.Errorf("%q：show_when = %q，應該是 %q", extra, got, want)
		}
	}
	// 沒有 slot 的頁面、選單元件（shell.menu）寫 show_when 不會生效：拒絕。
	for _, extra := range []string{"show_when = \"cap:closet.edit\"\n", "slot = \"shell.menu\"\nshow_when = \"cap:closet.edit\"\n"} {
		dir := h.write(sdk.TierMine, "studio", dataPkg("studio", "0.1.0", page(extra)), files)
		_, err := ParseManifest(dir, sdk.TierMine)
		pe, isErr := err.(*platform.Err)
		if !isErr || pe.Code != sdk.ErrorCodeInvalid || pe.Field != "contributes.page" || !strings.Contains(pe.Message, "show_when") {
			t.Fatalf("%q：要 INVALID contributes.page，實際 %#v", extra, err)
		}
	}
}

// 套件 id 的規則歸 packages；角色代號 <套件id>.<slug> 裡那一段（personaref 拆代號用）要跟它一致。
func TestPersonaCodeAcceptsPackageIDs(t *testing.T) {
	for _, id := range []string{"garden", "garden-2", "a1-b2-c3", "Garden", "garden-", "-garden", "gar.den", "2garden", "garden--x", ""} {
		if got, want := personaref.IsPackageCode(personaref.PackageCode(id, "fairy")), idPattern.MatchString(id); got != want {
			t.Errorf("%q：代號認 %v，套件 id 認 %v", id, got, want)
		}
	}
}
