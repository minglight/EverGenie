package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestPersonasCLIRoundTrip：真的 serve＋fixture 家，走完 new → check（有範本空格）→ 改檔 → check 過 → live → agents assign；
// design 時被擋；官方卡唯讀；強度設定存進 home.toml。
// Guards: REQ-130
func TestPersonasCLIRoundTrip(t *testing.T) {
	dir, sock := startFixtureAPI(t)
	t.Setenv("EVERGENIE_HOME", dir)
	t.Setenv("EVERGENIE_AGENT", "")

	help := hgOK(t, sock, "personas", "--help")
	for _, sub := range []string{"list", "show", "new", "copy", "check", "live", "design", "style", "直接改 personas/mine/"} {
		if !strings.Contains(help, sub) {
			t.Errorf("personas --help 少了 %s", sub)
		}
	}
	list := hgOK(t, sock, "personas", "list")
	if !strings.Contains(list, "builder") || !strings.Contains(list, "官方") || !strings.Contains(list, "坐在 code") {
		t.Fatalf("personas list：\n%s", list)
	}

	// new：設計中；撞名擋下
	hgOK(t, sock, "personas", "new", "hotaru", "--name", "Hotaru", "--aka", "小螢,firefly")
	if code, _, errOut := hg(t, sock, "", "personas", "new", "other", "--name", "builder"); code != 1 || !strings.Contains(errOut, "撞名") {
		t.Fatalf("撞名：%d %q", code, errOut)
	}
	card := filepath.Join(dir, "personas", "mine", "my_hotaru.md")
	if _, err := os.Stat(card); err != nil {
		t.Fatal(err)
	}
	if list = hgOK(t, sock, "personas", "list"); !strings.Contains(list, "設計中") || !strings.Contains(list, "小螢、firefly") {
		t.Fatalf("新卡要出現在 list（設計中）：\n%s", list)
	}

	// 設計中不能指派
	if code, _, errOut := hg(t, sock, "", "agents", "assign", "code", "my_hotaru"); code != 1 || !strings.Contains(errOut, "還在設計中，先上線才能指派") {
		t.Fatalf("設計中指派：%d %q", code, errOut)
	}
	// 範本空格：check 結束碼 1
	if code, out, _ := hg(t, sock, "", "personas", "check", "hotaru"); code != 1 || !strings.Contains(out, "還沒填完") {
		t.Fatalf("check 範本空格：%d %q", code, out)
	}
	// 改檔（編輯內文就是直接改檔案）→ check 過
	raw, err := os.ReadFile(card)
	if err != nil {
		t.Fatal(err)
	}
	head, _, _ := strings.Cut(string(raw), "## 個性")
	if err := os.WriteFile(card, []byte(head+"## 個性\n- 一句話：安靜的夜班同事。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := hgOK(t, sock, "personas", "check", "Hotaru"); !strings.Contains(out, "角色卡沒問題") {
		t.Fatalf("check 過：%q", out)
	}
	if out := hgOK(t, sock, "personas", "show", "firefly"); !strings.Contains(out, "name: Hotaru") || !strings.Contains(out, "安靜的夜班同事") {
		t.Fatalf("show 用別名找：%q", out)
	}

	// live → assign 成功；坐著不能移回設計中
	hgOK(t, sock, "personas", "live", "hotaru")
	hgOK(t, sock, "agents", "assign", "code", "my_hotaru")
	if code, _, errOut := hg(t, sock, "", "personas", "design", "hotaru"); code != 1 || !strings.Contains(errOut, "現在坐在 code") {
		t.Fatalf("坐著不能移回設計中：%d %q", code, errOut)
	}
	hgOK(t, sock, "agents", "assign", "code", "builder")
	hgOK(t, sock, "personas", "design", "hotaru")
	if code, _, errOut := hg(t, sock, "", "agents", "assign", "code", "my_hotaru"); code != 1 || !strings.Contains(errOut, "設計中") {
		t.Fatalf("移回設計中後又擋：%d %q", code, errOut)
	}

	// 官方卡唯讀：live／design 都擋；copy 成我的就能改
	official, _ := os.ReadFile(filepath.Join(dir, "personas", "builder.md"))
	if code, _, errOut := hg(t, sock, "", "personas", "design", "builder"); code != 1 || !strings.Contains(errOut, "唯讀") {
		t.Fatalf("官方卡：%d %q", code, errOut)
	}
	hgOK(t, sock, "personas", "copy", "builder", "builder2", "--name", "Builder Two")
	if after, _ := os.ReadFile(filepath.Join(dir, "personas", "builder.md")); string(after) != string(official) {
		t.Fatal("官方卡被動到")
	}
	if code, _, errOut := hg(t, sock, "", "personas", "copy", "builder", "builder3", "--name", "Builder"); code != 1 || !strings.Contains(errOut, "撞名") {
		t.Fatalf("複製沿用同名：%d %q", code, errOut)
	}

	// 強度：預設 c；設 d 存進 home.toml；秘書不能改
	if out := hgOK(t, sock, "personas", "style"); !strings.Contains(out, "目前的說話層強度：c") {
		t.Fatalf("預設 c：%q", out)
	}
	hgOK(t, sock, "personas", "style", "d")
	if data, _ := os.ReadFile(filepath.Join(dir, "home.toml")); !strings.Contains(string(data), `style = "d"`) {
		t.Fatalf("home.toml：\n%s", data)
	}
	if code, _, errOut := hg(t, sock, "", "personas", "style", "custom"); code != 1 || !strings.Contains(errOut, "_style.md") {
		t.Fatalf("沒有 _style.md 不能選 custom：%d %q", code, errOut)
	}
	t.Setenv("EVERGENIE_AGENT", "code")
	if code, _, errOut := hg(t, sock, "", "personas", "style", "a"); code != 1 || !strings.Contains(errOut, "只有主人") {
		t.Fatalf("秘書改強度：%d %q", code, errOut)
	}
	if code, _, _ := hg(t, sock, "", "personas", "bogus"); code != 2 {
		t.Fatalf("不認得的子指令：%d", code)
	}
}

// TestPersonasServiceFromFeature：真的 serve＋fixture 家，看板用的 personas.* API 由業務模組 feature/agents/persona 掛上
// （list／get_style／pack_status 都答得出來，test 沒接測口吻以外的 session 也不當機）；名冊讀角色卡經它（assign 看得到官方卡）。
// Guards: REQ-130, REQ-283
func TestPersonasServiceFromFeature(t *testing.T) {
	_, sock := startFixtureAPI(t)
	api := sdk.NewPersonasClient(sdk.Dial(sock))
	ctx := context.Background()
	list, err := api.List(ctx, &sdk.PersonasListRequest{})
	if err != nil || len(list.Personas) == 0 {
		t.Fatalf("personas.list：%v %+v", err, list)
	}
	var builder *sdk.PersonaCard
	for i := range list.Personas {
		if list.Personas[i].Slug == "builder" {
			builder = &list.Personas[i]
		}
	}
	if builder == nil || builder.Source != "official" || len(builder.Seats) == 0 {
		t.Fatalf("官方卡 builder 要在，而且名冊有人坐（角色庫向名冊要職位清單）：%+v", builder)
	}
	style, err := api.GetStyle(ctx, &sdk.PersonasGetStyleRequest{})
	if err != nil || style.Level == "" || len(style.Levels) != 5 {
		t.Fatalf("personas.get_style：%v %+v", err, style)
	}
	if _, err := api.PackStatus(ctx, &sdk.PersonasPackStatusRequest{}); err != nil {
		t.Fatalf("personas.pack_status：%v", err)
	}
	if _, err := api.TestResults(ctx, &sdk.PersonasTestResultsRequest{Slug: "builder"}); err != nil {
		t.Fatalf("personas.test_results 要轉給 session：%v", err)
	}
}

// TestPackagePersonaFlowsThroughRealModules：用真的 packages 模組＋真的 personas 模組（registry 接好，不是替身）走完整條：
// 家裡放一個帶角色的套件（packages/mine/garden，宣告角色 fairy）→ 角色庫列出 garden.fairy（來源是套件）→
// `personas new --to-package` 在同一個套件新增 bloom（寫進 package.toml、列表馬上看得到）→ 設計中不能指派、上線後可以指派。
// 取代原本在 packages 測試裡拆成替身（stubLib）的端對端情境；角色庫與 packages 各自的細節在各自的套件測試。
func TestPackagePersonaFlowsThroughRealModules(t *testing.T) {
	manifest := "schema = 1\nid = \"garden\"\nname = \"花園\"\nversion = \"1.0.0\"\ndescription = \"測試用的套件\"\nkind = \"data\"\napi = \"evergenie.v1\"\n" +
		"[[contributes.persona]]\nslug = \"fairy\"\npath = \"personas/fairy.md\"\n"
	card := "---\nschema: 1\nname: Fairy\n---\n我是花園裡的小仙子。\n"
	dir, sock := startFixtureAPIWith(t, func(dir string) {
		pkg := filepath.Join(dir, "packages", "mine", "garden")
		if err := os.MkdirAll(filepath.Join(pkg, "personas"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "package.toml"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "personas", "fairy.md"), []byte(card), 0o644); err != nil {
			t.Fatal(err)
		}
	})
	t.Setenv("EVERGENIE_HOME", dir)
	t.Setenv("EVERGENIE_AGENT", "")
	api := sdk.NewPersonasClient(sdk.Dial(sock))
	ctx := context.Background()

	find := func(slug string) *sdk.PersonaCard {
		t.Helper()
		list, err := api.List(ctx, &sdk.PersonasListRequest{})
		if err != nil {
			t.Fatal(err)
		}
		for i := range list.Personas {
			if list.Personas[i].Slug == slug {
				return &list.Personas[i]
			}
		}
		return nil
	}

	// 裝進來的套件角色出現在角色庫（來源是套件），官方卡照舊在。
	fairy := find("garden.fairy")
	if fairy == nil || fairy.Source != "package" || fairy.Name != "Fairy" {
		t.Fatalf("garden.fairy 要在角色庫（來源 package）：%+v", fairy)
	}
	if find("builder") == nil {
		t.Fatal("官方卡 builder 要還在")
	}
	if out := hgOK(t, sock, "personas", "show", "garden.fairy"); !strings.Contains(out, "我是花園裡的小仙子") {
		t.Fatalf("show 讀得到套件的卡：%q", out)
	}

	// 在同一個套件新增角色：角色庫經 packages 寫卡與宣告，列表馬上看得到。
	hgOK(t, sock, "personas", "new", "bloom", "--name", "Bloom", "--to-package", "garden")
	if got := find("garden.bloom"); got == nil || got.Source != "package" {
		t.Fatalf("新增的 garden.bloom 要出現在角色庫：%+v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "packages", "mine", "garden", "package.toml"))
	if err != nil || !strings.Contains(string(data), `slug = "bloom"`) {
		t.Fatalf("package.toml 要宣告 bloom：%v\n%s", err, data)
	}

	// 新卡是設計中：不能指派；上線之後名冊（經 SetPersonaSource 讀角色庫，再讀 packages 的卡）就能指派。
	if code, _, errOut := hg(t, sock, "", "agents", "assign", "code", "garden.bloom"); code != 1 || !strings.Contains(errOut, "設計中") {
		t.Fatalf("設計中的套件角色不能指派：%d %q", code, errOut)
	}
	hgOK(t, sock, "personas", "live", "garden.bloom")
	hgOK(t, sock, "agents", "assign", "code", "garden.bloom")
	if got := find("garden.bloom"); got == nil || len(got.Seats) == 0 {
		t.Fatalf("指派後角色庫要看得到坐位：%+v", got)
	}
}
