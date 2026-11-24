package pkg

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

var wordRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

var owner = &sdk.Actor{Name: "owner(cli)"}

func newPkg(h *harness, id, name string) (*sdk.PackageNewResponse, error) {
	return h.m.New(h.t.Context(), &sdk.PackageNewRequest{ID: id, Name: name, By: owner})
}

// dirNames 列出資料夾第一層的名字（含隱藏的），排序。
func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// tree 列出資料夾裡所有檔案與子資料夾（相對路徑、排序），用來比對「什麼都沒動」。
func tree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && p != root {
			rel, _ := filepath.Rel(root, p)
			out = append(out, rel)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// TestPackagesNewScaffold：建 garden → 資料夾與檔案齊全、ParseManifest 無問題、套件載入成功（running、tier mine、頁面與選單與一位秘書樣板）；顯示名稱有引號與中文也安全。
// Guards: REQ-306
func TestPackagesNewScaffold(t *testing.T) {
	h := newHarness(t)
	h.start()
	r, err := newPkg(h, "garden", `我的"花園" \ 測試`)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.home.Dir, "packages", "mine", "garden")
	if r.Dir != "packages/mine/garden" || strings.Join(r.Files, ",") != "README.md,agents/garden/agent.md,package.toml,pages/index.tsx,personas/.gitkeep,schemas/.gitkeep" {
		t.Fatalf("回應：%+v", r)
	}
	for _, rel := range r.Files {
		if !exists(filepath.Join(dir, rel)) {
			t.Errorf("少了 %s", rel)
		}
	}
	for _, rel := range r.Files { // 套件資料夾不放使用者的資料或家的路徑（UFE-N3）
		if b, _ := os.ReadFile(filepath.Join(dir, rel)); strings.Contains(string(b), h.home.Dir) {
			t.Errorf("%s 含有家的路徑", rel)
		}
	}
	man, err := ParseManifest(dir, sdk.TierMine)
	if err != nil {
		t.Fatalf("鷹架的 manifest 要沒問題：%v", err)
	}
	if man.Kind != "data" || man.API != "evergenie.v1" || man.Version != "0.1.0" || man.Name != `我的"花園" \ 測試` || man.Fork != nil {
		t.Fatalf("manifest：%+v", man)
	}
	p := h.get("garden")
	if p.Tier != sdk.TierMine || p.State != sdk.PackageStateRunning || p.Error != "" || p.Name != `我的"花園" \ 測試` {
		t.Fatalf("載入後：%+v", p)
	}
	if len(p.Pages) != 1 || p.Pages[0].ID != "index" || p.Pages[0].Component != "pages/index.tsx" || len(p.Menu) != 1 || p.Menu[0].Page != "index" {
		t.Fatalf("頁面與選單：%+v %+v", p.Pages, p.Menu)
	}
	if len(p.Agents) != 1 || p.Agents[0].ID != "garden" || p.Agents[0].Duty == "" {
		t.Fatalf("秘書樣板：%+v", p.Agents)
	}
	// 名字預設用 id；沒給名字也能建。
	if _, err := newPkg(h, "my-habit", ""); err != nil {
		t.Fatal(err)
	}
	if got := h.get("my-habit").Name; got != "my-habit" {
		t.Fatalf("預設名稱 = %q", got)
	}
	// 只留下 garden 與 my-habit，沒有暫存資料夾。
	if got := dirNames(t, filepath.Join(h.home.Dir, "packages", "mine")); strings.Join(got, ",") != "garden,my-habit" {
		t.Fatalf("mine 層：%v", got)
	}
}

// TestPackagesNewRejectsBadIDs：格式錯與保留字被拒（INVALID），訊息是白話、點名那個 id；什麼都沒建。
// Guards: REQ-307, REQ-309
func TestPackagesNewRejectsBadIDs(t *testing.T) {
	h := newHarness(t)
	h.start()
	for _, id := range []string{"", "a", "Garden", "1garden", "-garden", "my_habit", "花園", "has space", "a/b", "..", "x" + strings.Repeat("y", 31)} {
		_, err := newPkg(h, id, "")
		if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "小寫英文") {
			t.Errorf("id=%q 要 INVALID 並說明規則：%v", id, err)
		}
	}
	if _, err := newPkg(h, "x"+strings.Repeat("y", 30), ""); err != nil { // 剛好 31 字
		t.Fatalf("31 字可以：%v", err)
	}
	for _, id := range ReservedIDs {
		_, err := newPkg(h, id, "")
		if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "保留") || !strings.Contains(err.Error(), "「"+id+"」") {
			t.Errorf("保留字 %q 要 INVALID 並說是保留的：%v", id, err)
		}
	}
	for _, name := range []string{strings.Repeat("字", 41), "兩\n行"} {
		if _, err := newPkg(h, "okname", name); codeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("name=%q 要 INVALID：%v", name, err)
		}
	}
	if _, err := h.m.New(t.Context(), &sdk.PackageNewRequest{ID: "nobody"}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有操作者要 INVALID：%v", err)
	}
	if got := dirNames(t, filepath.Join(h.home.Dir, "packages", "mine")); len(got) != 1 || got[0] != "x"+strings.Repeat("y", 30) {
		t.Fatalf("被拒的都不能留東西：%v", got)
	}
}

// TestPackagesNewRejectsExisting：撞官方名、撞已裝的分享套件、撞自己已有的、撞別的套件帶來的職位名，都是 CONFLICT 白話說明；官方層與既有的 mine 一個位元組都不動。
// Guards: REQ-307, REQ-308
func TestPackagesNewRejectsExisting(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", ""), map[string]string{"README.md": "官方\n"})
	h.write(sdk.TierOfficial, "pack", dataPkg("pack", "1.0.0", "[[contributes.agent]]\nid = \"gardener\"\ntemplate = \"agents/gardener/agent.md\"\n"),
		map[string]string{"agents/gardener/agent.md": "---\nduty: 園丁\n---\n你是園丁。\n"})
	h.write(sdk.TierInstalled, "shared", dataPkg("shared", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "mine1", dataPkg("mine1", "1.0.0", ""), map[string]string{"README.md": "我的\n"})
	h.start()
	before := map[string][]string{}
	for _, t2 := range []sdk.Tier{sdk.TierOfficial, sdk.TierInstalled, sdk.TierMine} {
		before[string(t2)] = tree(t, h.tierRoot(t2))
	}
	cases := map[string]string{"notes": "官方套件", "shared": "別人分享", "mine1": "已經存在", "gardener": "套件「pack」帶來的職位"}
	for id, want := range cases {
		_, err := newPkg(h, id, "")
		if codeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), want) {
			t.Errorf("%s 要 CONFLICT 並說「%s」：%v", id, want, err)
		}
	}
	for tier, was := range before {
		if got := tree(t, h.tierRoot(sdk.Tier(tier))); strings.Join(got, ",") != strings.Join(was, ",") {
			t.Errorf("%s 層被動到：%v → %v", tier, was, got)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(h.tierRoot(sdk.TierMine), "mine1", "README.md")); string(b) != "我的\n" {
		t.Fatalf("既有的套件被蓋掉：%q", b)
	}
	// 再建同一個也是 CONFLICT。
	if _, err := newPkg(h, "garden", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := newPkg(h, "garden", ""); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("重複要 CONFLICT：%v", err)
	}
}

// TestPackagesNewLeavesNothingOnFailure：鷹架驗不過（模擬）→ 回白話錯誤、packages/mine/ 沒有半成品也沒有暫存資料夾；先驗過才 rename。
// Guards: REQ-308
func TestPackagesNewLeavesNothingOnFailure(t *testing.T) {
	h := newHarness(t)
	h.start()
	orig := scaffoldFilesFn
	t.Cleanup(func() { scaffoldFilesFn = orig })
	scaffoldFilesFn = func(id, name string) map[string]string {
		f := orig(id, name)
		f["package.toml"] = strings.Replace(f["package.toml"], `kind = "data"`, `kind = "weird"`, 1)
		return f
	}
	_, err := newPkg(h, "garden", "")
	if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("要 INVALID：%v", err)
	}
	if got := dirNames(t, filepath.Join(h.home.Dir, "packages", "mine")); len(got) != 0 {
		t.Fatalf("失敗不能留東西：%v", got)
	}
	if _, err := h.m.Get(t.Context(), &sdk.PackageGetRequest{ID: "garden"}); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("不該有 garden：%v", err)
	}
}

// TestPackagesNewAgentTemplateParses：agent.md 範本有 duty 與內文（sync 預演看得到新增一位秘書），名字裡的特殊字元不會弄壞標頭。
// Guards: REQ-306
func TestPackagesNewAgentTemplateParses(t *testing.T) {
	h := newHarness(t)
	h.start()
	if _, err := newPkg(h, "garden", `花園: "a" #1`); err != nil {
		t.Fatal(err)
	}
	t1, err := parseAgentTemplate(filepath.Join(h.tierRoot(sdk.TierMine), "garden", "agents", "garden", "agent.md"), "garden", "agents/garden/agent.md")
	if err != nil || !strings.Contains(t1.Duty, `花園: "a" #1`) || strings.TrimSpace(t1.Body) == "" {
		t.Fatalf("範本：%+v %v", t1, err)
	}
}

// TestReservedIDsListedInDocs：程式的保留字清單跟 ai_docs/conventions/user-features.md 一致（兩邊都要列、沒有多的）；清單排序且不重複。
// Guards: REQ-309, REQ-312
func TestReservedIDsListedInDocs(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ai_docs", "conventions", "user-features.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	start := strings.Index(doc, "<!-- reserved-ids:start -->")
	end := strings.Index(doc, "<!-- reserved-ids:end -->")
	if start < 0 || end < start {
		t.Fatal("文件要有 reserved-ids 區段標記")
	}
	var listed []string
	for _, f := range strings.FieldsFunc(doc[start:end], func(r rune) bool { return r == '`' }) {
		if wordRe.MatchString(f) {
			listed = append(listed, f)
		}
	}
	want := append([]string(nil), ReservedIDs...)
	sort.Strings(want)
	sort.Strings(listed)
	if strings.Join(want, ",") != strings.Join(listed, ",") {
		t.Fatalf("保留字清單不一致\n程式：%v\n文件：%v", want, listed)
	}
	for i := 1; i < len(want); i++ {
		if want[i] == want[i-1] {
			t.Fatalf("保留字重複：%s", want[i])
		}
	}
	idx, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ai_docs", "INDEX.md"))
	if err != nil || !strings.Contains(string(idx), "conventions/user-features.md") {
		t.Fatalf("INDEX.md 要登記 user-features.md：%v", err)
	}
}

// TestUserFeaturesDocPointsSecretariesToSkill：慣例文件有「給秘書」一節，指到技能 create-feature 與共用規則，技能檔真的存在（SEC-302 S8）。
// Guards: REQ-331
func TestUserFeaturesDocPointsSecretariesToSkill(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ai_docs", "conventions", "user-features.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "## 給秘書")
	if i < 0 {
		t.Fatal("文件要有「給秘書」一節")
	}
	section := doc[i:]
	for _, want := range []string{"create-feature", "rules/shared.md", "packages new"} {
		if !strings.Contains(section, want) {
			t.Errorf("「給秘書」要提到 %q", want)
		}
	}
	// 技能放在引擎套件裡（資料夾名含平台名，D-21 不讓這裡寫死），用萬用字元找。
	if found, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "packages", "official", "engine-*", "skills", "create-feature", "SKILL.md")); len(found) == 0 {
		t.Error("技能檔要存在")
	}
}
