package persona

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// treeList 列出資料夾底下所有檔案與內容（比對「什麼都沒寫」用）。
func treeList(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	must(t, filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		out[strings.TrimPrefix(p, dir)] = string(b)
		return err
	}))
	return out
}

// TestLibraryBundledCards：跟著程式的公開卡（PRS-25）。官方資料夾沒有的，角色庫照樣列得出、讀得到、算官方（唯讀）；
// 同代號官方資料夾那張（角色包同步來的）優先；README、_ 開頭、我的代號格式的檔不算卡；角色庫只讀那個資料夾，不寫。
// Guards: PRS-25
func TestLibraryBundledCards(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	bundled := t.TempDir()
	for name, data := range map[string]string{
		"kaguya.md":    "+++\nname = \"輝夜\"\naka = [\"Kaguya\"]\n+++\n跟著程式的卡\n",
		"athena.md":    "+++\nname = \"程式帶的 Athena\"\n+++\n被官方資料夾那張蓋住\n",
		"README.md":    "不是卡\n",
		"_style.md":    "不是卡\n",
		"my_extra.md":  "+++\nname = \"不是官方代號\"\n+++\n",
		"Upper.md":     "+++\nname = \"大寫\"\n+++\n",
		"notes.txt":    "不是卡\n",
		"wardrobe.png": "不是卡\n",
	} {
		must(t, os.WriteFile(filepath.Join(bundled, name), []byte(data), 0o444))
	}
	before := treeList(t, bundled)
	lib.SetBundledCards(func(home string) string {
		if home != h.Dir {
			t.Errorf("算資料夾要用這個家：%s", home)
		}
		return bundled
	})

	got := map[string]string{}
	for _, c := range lib.cards() {
		got[c.slug] = c.source + "|" + c.name()
	}
	want := map[string]string{"athena": "official|Athena", "new": "official|New", "kaguya": "official|輝夜"}
	if len(got) != len(want) {
		t.Fatalf("角色庫：%v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("角色庫 %s：%q，要 %q（全部：%v）", k, got[k], v, got)
		}
	}
	info, err := lib.Card("kaguya")
	must(t, err)
	if info.Source != personaref.SourceOfficial || info.Record.Fields["name"] != "輝夜" {
		t.Fatalf("名冊讀卡：%+v", info)
	}
	if c := cardOf(t, lib, "kaguya"); c.Name != "輝夜" || c.Source != personaref.SourceOfficial || c.Status != personaref.StatusLive {
		t.Fatalf("show：%+v", c)
	}
	rows, err := lib.Library(ctx)
	must(t, err)
	var slugs []string
	for _, r := range rows {
		slugs = append(slugs, r.Slug)
	}
	sort.Strings(slugs)
	if strings.Join(slugs, ",") != "athena,kaguya,new" {
		t.Fatalf("personas.list：%v", slugs)
	}
	if _, err := lib.mineCard("kaguya"); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("跟著程式的卡也是官方、唯讀：%v", err)
	}
	if _, err := lib.CopyPersona(ctx, "kaguya", "moon", "我的輝夜", nil, "owner(cli)"); err != nil {
		t.Fatalf("複製成我的：%v", err)
	}
	if !strings.Contains(readFile(t, h, "personas/mine/my_moon.md"), "跟著程式的卡") {
		t.Fatal("複製出來的卡要是跟著程式那張的內文")
	}
	if n, ok := lib.introNotice("kaguya"); !ok || n == nil {
		t.Fatal("新角色的招呼也要讀得到跟著程式的卡")
	}

	// 第 2 版的家：角色包同步到狀態資料夾的卡優先；拿掉後回到跟著程式的那張
	state := layoutV2(t, h.Dir)
	must(t, os.WriteFile(filepath.Join(state, "personas", "kaguya.md"), []byte("+++\nname = \"角色包的輝夜\"\n+++\n"), 0o644))
	if c := cardOf(t, lib, "kaguya"); c.Name != "角色包的輝夜" {
		t.Fatalf("官方資料夾的卡要優先：%+v", c)
	}
	must(t, os.Remove(filepath.Join(state, "personas", "kaguya.md")))
	if c := cardOf(t, lib, "kaguya"); c.Name != "輝夜" {
		t.Fatalf("官方資料夾沒有時用跟著程式的：%+v", c)
	}
	after := treeList(t, bundled)
	if len(after) != len(before) {
		t.Fatalf("跟著程式的資料夾不能被寫：%v", after)
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("跟著程式的資料夾不能被寫：%s", k)
		}
	}

	// 給 session 測口吻用的找卡入口：跟 cardPath 同一條規則（sdk.OfficialCardIn）
	if p, ok := lib.OfficialCardFile("kaguya"); !ok || p != filepath.Join(bundled, "kaguya.md") {
		t.Fatalf("OfficialCardFile(kaguya) = %s %v", p, ok)
	}

	// 沒接（舊的組裝、測試）＝只看官方資料夾
	lib.SetBundledCards(nil)
	if _, ok := lib.readCard(personaref.SourceOfficial, "kaguya"); ok {
		t.Fatal("沒接跟著程式的資料夾就不該讀到")
	}
	if _, ok := lib.OfficialCardFile("kaguya"); ok {
		t.Fatal("沒接跟著程式的資料夾，OfficialCardFile 也不該找到")
	}
}

// TestRepoBundledCardsArePublic：repo 裡跟著程式的公開卡（PRS-26；名單不寫死，照 cards/ 裡有的）每張都是 <代號>.md 的一般檔、
// 讀得起來、有名字、沒有標隱藏版；角色庫接上這個資料夾，新家（官方資料夾是空的）每一位都列得出來、都是官方、名字對。
// Guards: PRS-25, PRS-26
func TestRepoBundledCardsArePublic(t *testing.T) {
	dir := personapack.BundledCardsDir(filepath.Join("..", "..", "..", ".."))
	entries, err := os.ReadDir(dir)
	must(t, err)
	want := map[string]string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue // .DS_Store 這類被 git 忽略、不進發行檔（dist.sh 用 git ls-files 收檔）
		}
		slug, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok || !e.Type().IsRegular() || !personaref.SlugPattern.MatchString(slug) {
			t.Fatalf("cards/ 裡只能放 <代號>.md 的一般檔：%s", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		must(t, err)
		if why := personapack.Excluded(e.Name(), data); why != "" {
			t.Fatalf("%s 不能公開：%s", e.Name(), why)
		}
		r, err := parseCard(data)
		name, _ := r.Fields["name"].(string)
		if err != nil || name == "" {
			t.Fatalf("%s 讀不起來或沒有名字：%v %v", e.Name(), err, r.Fields)
		}
		want[slug] = "official|" + name
	}
	if len(want) == 0 {
		t.Fatal("repo 裡沒有跟著程式的公開卡")
	}

	_, lib, h, _ := fixture(t)
	must(t, os.RemoveAll(filepath.Join(h.Dir, "personas", "athena.md")))
	must(t, os.RemoveAll(filepath.Join(h.Dir, "personas", "new.md")))
	lib.SetBundledCards(func(string) string { return dir })
	got := map[string]string{}
	for _, c := range lib.cards() {
		got[c.slug] = c.source + "|" + c.name()
	}
	if len(got) != len(want) {
		t.Fatalf("角色庫：%v，要 %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("角色庫 %s：%q，要 %q", k, got[k], v)
		}
	}
}

// TestLibraryMineCardBeatsBundledCard：同代號時 官方角色資料夾＞我的卡＞跟著程式的公開卡（PRS-27）。舊家有不是 my_ 開頭、
// 跟公開角色同代號的我的卡（例 venus），換到帶那張公開卡的版本後：角色庫列表、show、名冊讀卡用的都是主人的那張，
// 跟著程式的那張不列、主人的也不標成被蓋住；找官方卡的入口說沒有官方卡。官方資料夾有同代號的卡時照舊官方優先（現有家的行為不變）。
// 主人的卡拿掉，跟著程式的那位就回來。
func TestLibraryMineCardBeatsBundledCard(t *testing.T) {
	_, lib, h, _ := fixture(t)
	ctx := context.Background()
	bundled := t.TempDir()
	must(t, os.WriteFile(filepath.Join(bundled, "venus.md"), []byte("+++\nname = \"程式帶的維納斯\"\n+++\n公開卡\n"), 0o444))
	must(t, os.WriteFile(filepath.Join(bundled, "athena.md"), []byte("+++\nname = \"程式帶的 Athena\"\n+++\n公開卡\n"), 0o444))
	lib.SetBundledCards(func(string) string { return bundled })
	writeCard(t, h, "personas/mine/venus.md", "---\nschema: 1\nname: 我的維納斯\n---\n主人自己的\n")
	writeCard(t, h, "personas/mine/athena.md", "---\nschema: 1\nname: 我的 Athena\n---\n被官方蓋住\n")

	rows, err := lib.Library(ctx)
	must(t, err)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s:%s:%s:%v", r.Source, r.Slug, r.Name, r.Shadowed))
	}
	if want := "official:athena:Athena:false official:new:New:false mine:athena:我的 Athena:true mine:venus:我的維納斯:false"; strings.Join(got, " ") != want {
		t.Fatalf("角色庫列表：\n%s\n要\n%s", strings.Join(got, " "), want)
	}
	if c := cardOf(t, lib, "venus"); c.Source != personaref.SourceMine || c.Name != "我的維納斯" || c.Shadowed {
		t.Fatalf("show venus 要是主人的那張：%+v", c)
	}
	if c := cardOf(t, lib, "athena"); c.Source != personaref.SourceOfficial || c.Name != "Athena" {
		t.Fatalf("官方資料夾有的照舊官方優先：%+v", c)
	}
	info, err := lib.Card("venus")
	must(t, err)
	if info.Source != personaref.SourceMine || info.Record.Fields["name"] != "我的維納斯" {
		t.Fatalf("名冊讀卡要讀主人的那張：%+v", info)
	}
	if info, err = lib.Card("athena"); err != nil || info.Source != personaref.SourceOfficial || info.Record.Fields["name"] != "Athena" {
		t.Fatalf("名冊讀卡：官方資料夾有的照舊官方：%+v %v", info, err)
	}
	if p, ok := lib.OfficialCardFile("venus"); ok {
		t.Fatalf("跟著程式的那張被主人的卡蓋過，不算官方卡：%s", p)
	}
	if problems, err := lib.CheckPersonas(ctx, "venus"); err != nil || len(problems) != 0 {
		t.Fatalf("主人的 venus 沒有被蓋住：%v %v", problems, err)
	}

	must(t, os.Remove(filepath.Join(h.Dir, "personas", "mine", "venus.md")))
	if c := cardOf(t, lib, "venus"); c.Source != personaref.SourceOfficial || c.Name != "程式帶的維納斯" {
		t.Fatalf("主人的卡拿掉，跟著程式的那位就回來：%+v", c)
	}
}

// TestDoctorWarnsMineSlugClash：doctor 的離線檢查（PRS-27）——我的卡跟跟著程式的公開卡、或跟官方角色資料夾的卡同代號，各一條 warn，
// 白話說角色庫用的是哪一張、建議改成 my_ 開頭（不自動改，檔案都不動）；my_ 開頭的、官方沒有的、同名的資料夾都不算；沒撞名就沒有這條。
func TestDoctorWarnsMineSlugClash(t *testing.T) {
	h := homefixture.New(t)
	bundled := t.TempDir()
	for _, slug := range []string{"venus", "athena", "luna"} {
		must(t, os.WriteFile(filepath.Join(bundled, slug+".md"), []byte("+++\nname = \"公開\"\n+++\n"), 0o444))
	}
	writeCard(t, h, "personas/athena.md", "---\nname: Athena\n---\n")
	writeCard(t, h, "personas/mine/my_venus.md", "---\nname: 我的\n---\n")
	writeCard(t, h, "personas/mine/kaguya.md", "---\nname: 我的輝夜\n---\n")
	must(t, os.MkdirAll(filepath.Join(h.Dir, "personas", "mine", "luna.md"), 0o755))
	checks := SlugClashChecks(func(home string) string {
		if home != h.Dir {
			t.Errorf("算資料夾要用這個家：%s", home)
		}
		return bundled
	})
	if got := checks(context.Background(), h.Dir); len(got) != 0 {
		t.Fatalf("沒撞名不該有這條：%+v", got)
	}

	writeCard(t, h, "personas/mine/venus.md", "---\nname: 我的維納斯\n---\n")
	writeCard(t, h, "personas/mine/athena.md", "---\nname: 我的 Athena\n---\n")
	before := treeList(t, h.Dir)
	got := checks(context.Background(), h.Dir)
	if len(got) != 2 {
		t.Fatalf("撞名的兩張各一條：%+v", got)
	}
	for _, c := range got {
		if c.ID != "personas.slug_clash" || c.Severity != sdk.DoctorSeverityWarn || !strings.Contains(c.Fix, "my_") {
			t.Fatalf("要是 warn、建議改成 my_ 開頭：%+v", c)
		}
	}
	if a := got[0]; !strings.Contains(a.Detail, "personas/mine/athena.md") || !strings.Contains(a.Detail, "用的是官方那張") || !strings.Contains(a.Fix, "personas/mine/my_athena.md") ||
		!strings.Contains(a.Fix, "personas/mine/avatars/") || strings.Contains(a.Fix, "home tidy") { // home tidy 不改這種，不能叫主人去跑
		t.Fatalf("跟官方資料夾的卡同代號：%+v", a)
	}
	if v := got[1]; !strings.Contains(v.Detail, "personas/mine/venus.md") || !strings.Contains(v.Detail, "程式帶的公開角色") || !strings.Contains(v.Detail, "用的是你的這張") ||
		!strings.Contains(v.Fix, "`evergenie service stop`") || !strings.Contains(v.Fix, "`evergenie home tidy`") || !strings.Contains(v.Fix, "`--apply`") ||
		!strings.Contains(v.Fix, "`evergenie service start`") || !strings.Contains(v.Fix, "my_venus") {
		t.Fatalf("跟程式帶的公開卡同代號：%+v", v)
	}
	after := treeList(t, h.Dir)
	if len(after) != len(before) {
		t.Fatal("doctor 不自動改，家裡的檔不動")
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("doctor 不自動改：%s 變了", k)
		}
	}
	if got := SlugClashChecks(nil)(context.Background(), h.Dir); len(got) != 1 || !strings.Contains(got[0].Detail, "athena") {
		t.Fatalf("沒接跟著程式的資料夾＝只比官方角色資料夾：%+v", got)
	}
	if got := checks(context.Background(), ""); got != nil {
		t.Fatalf("沒有家不檢查：%+v", got)
	}
}
