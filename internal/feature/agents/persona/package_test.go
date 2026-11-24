package persona

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// 現有角色行為基準：官方、我的、官方蓋住我的、設計中，沒有套件時（以及有套件來源時）都要一樣。
// Guards: REQ-130, REQ-313
func TestPackagePersonaBaselineUnchanged(t *testing.T) {
	m, lib, h, _ := fixture(t)
	ctx := context.Background()
	writeCard(t, h, "personas/mine/my_mina.md", "---\nschema: 1\nname: Mina\nstatus: design\n---\n我的角色。\n")
	writeCard(t, h, "personas/mine/athena.md", "---\nschema: 1\nname: Fake Athena\n---\n被蓋住。\n")
	check := func(label string) {
		t.Helper()
		cards, err := lib.Library(ctx)
		must(t, err)
		var order []string
		for _, c := range cards {
			order = append(order, c.Source+":"+c.Slug+":"+c.Status)
		}
		if got := strings.Join(order, " "); got != "official:athena:live official:new:live mine:athena:live mine:my_mina:design" {
			t.Fatalf("%s：角色庫順序／狀態變了：%s", label, got)
		}
		if p, err := cardRecord(lib, "athena"); err != nil || p.Fields["name"] != "Athena" {
			t.Fatalf("%s：官方優先壞了：%v %v", label, p.Fields, err)
		}
		if err := assign(ctx, m, "code", "my_mina", owner); code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "還在設計中") {
			t.Fatalf("%s：設計中的卡要擋：%v", label, err)
		}
		must(t, assign(ctx, m, "code", "new", owner))
		must(t, assign(ctx, m, "code", "athena", owner))
	}
	check("沒有套件來源")
	lib.SetPackagePersonas(fakePackages{})
	check("有來源但沒有套件")
}

type fakePackages struct{ list []personaref.PackagePersona }

func (f fakePackages) PackagePersonas() []personaref.PackagePersona { return f.list }

const fairyCard = "---\nschema: 1\nname: Fairy\naka: [小仙子]\n---\n我是花園裡的小仙子，說話輕輕的。\n"

func packageFixture(t *testing.T, _ bool) (*agent.Module, *Module, *homefixture.Home, func(string, string, string, bool) personaref.PackagePersona, *fakePackagesMut) {
	t.Helper()
	m, lib, h, _ := fixture(t)
	src := &fakePackagesMut{}
	lib.SetPackagePersonas(src)
	add := func(id, slug, text string, act bool) personaref.PackagePersona {
		rel := "packages/mine/" + id + "/personas/" + slug + ".md"
		writeCard(t, h, rel, text)
		pp := personaref.PackagePersona{Package: id, Slug: slug, Path: filepath.Join(h.Dir, rel), Active: act}
		if !act {
			pp.Reason = "已停用"
		}
		src.list = append(src.list, pp)
		return pp
	}
	return m, lib, h, add, src
}

type fakePackagesMut struct{ list []personaref.PackagePersona }

func (f *fakePackagesMut) PackagePersonas() []personaref.PackagePersona {
	return append([]personaref.PackagePersona(nil), f.list...)
}

// 列出、重名不撞、官方優先不變。
// Guards: REQ-313
func TestPackagePersonaListedWithoutClash(t *testing.T) {
	_, lib, h, add, _ := packageFixture(t, true)
	ctx := context.Background()
	add("garden", "fairy", fairyCard, true)
	add("park", "fairy", strings.Replace(fairyCard, "name: Fairy", "name: ParkFairy", 1), true)
	add("garden", "athena", "---\nschema: 1\nname: GardenAthena\n---\n花園版。\n", true)
	// 我的卡也叫 fairy：不跟套件的撞（代號不同）。
	writeCard(t, h, "personas/mine/fairy.md", "---\nschema: 1\nname: MineFairy\n---\n我的。\n")

	cards, err := lib.Library(ctx)
	must(t, err)
	var order []string
	for _, c := range cards {
		order = append(order, c.Source+":"+c.Slug)
		if c.Source == personaref.SourcePackage && (c.Package == "" || c.Unassignable != "") {
			t.Errorf("套件卡要標來源套件、有效的不標不能指派：%+v", c)
		}
	}
	if got := strings.Join(order, " "); got != "official:athena official:new mine:fairy package:garden.athena package:garden.fairy package:park.fairy" {
		t.Fatalf("官方、我的、套件，各自排序：%s", got)
	}
	if p, err := cardRecord(lib, "athena"); err != nil || p.Fields["name"] != "Athena" {
		t.Fatalf("官方優先不變：%v %v", p.Fields, err)
	}
	if p, err := cardRecord(lib, "fairy"); err != nil || p.Fields["name"] != "MineFairy" {
		t.Fatalf("fairy 還是我的：%v %v", p.Fields, err)
	}
	c, body, raw, err := lib.ShowPersona(ctx, "garden.fairy")
	must(t, err)
	if c.Source != personaref.SourcePackage || c.Package != "garden" || !strings.Contains(body, "小仙子") || !strings.Contains(raw, "schema: 1") {
		t.Fatalf("%+v %q", c, body)
	}
	// 套件的卡不佔名字：新增我的卡叫 Fairy 不會被擋（也不會讓既有卡突然撞名）。
	if _, err := lib.CreatePersona(ctx, "my_mina", "Pixie小精靈", []string{"小仙子"}, owner); err != nil {
		t.Fatalf("套件的名字不算撞名：%v", err)
	}
	problems, err := lib.CheckPersonas(ctx, "")
	must(t, err)
	for _, p := range problems {
		if strings.HasPrefix(p, "athena：") || strings.HasPrefix(p, "fairy：") || strings.HasPrefix(p, "new：") {
			t.Fatalf("既有卡不能因為套件而多出問題：%s", p)
		}
	}
}

// 指派、建秘書、check 都接受套件內代號；組裝指令讀得到卡的內容；套件的卡唯讀。
// Guards: REQ-314
func TestPackagePersonaAssignCheckInstructions(t *testing.T) {
	m, lib, _, add, _ := packageFixture(t, true)
	ctx := context.Background()
	add("garden", "fairy", fairyCard, true)
	must(t, assign(ctx, m, "code", "garden.fairy", owner))
	if err := newAgent(ctx, m, "plant", "garden.fairy", "", owner); err != nil {
		t.Fatalf("建秘書：%v", err)
	}
	ins, err := m.Instructions(ctx, "code", "s1")
	must(t, err)
	if !strings.Contains(ins, "我是花園裡的小仙子") {
		t.Fatalf("組裝指令要含套件角色的內容")
	}
	if n, err := m.PersonaName(ctx, "code"); err != nil || n != "Fairy" {
		t.Fatalf("%q %v", n, err)
	}
	if id, ok := m.Resolve(ctx, "小仙子"); !ok || (id != "code" && id != "plant") {
		t.Fatalf("別名要能找到：%q %v", id, ok)
	}
	ps, err := lib.CheckPersonas(ctx, "garden.fairy")
	must(t, err)
	if len(ps) != 0 {
		t.Fatal(ps)
	}
	if _, err := lib.CheckPersonas(ctx, "garden.ghost"); code(err) != sdk.ErrorCodeNotFound {
		t.Fatal(err)
	}
	// 沒這張卡、格式不對的代號：指派擋掉。
	for _, bad := range []string{"garden.ghost", "garden.", ".fairy", "garden.fairy.x", "Garden.fairy"} {
		if err := assign(ctx, m, "life", bad, owner); code(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%q 要 invalid：%v", bad, err)
		}
	}
	// 唯讀：update／set_status 回 forbidden；複製成我的可以。
	if _, _, err := lib.UpdatePersona(ctx, "garden.fairy", ptr("X"), nil, nil, time.Now(), owner); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("唯讀：%v", err)
	}
	if _, err := lib.SetPersonaStatus(ctx, "garden.fairy", personaref.StatusDesign, owner); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("唯讀：%v", err)
	}
	if _, err := lib.CopyPersona(ctx, "garden.fairy", "fairycopy", "FairyCopy", nil, owner); err != nil {
		t.Fatalf("複製：%v", err)
	}
	// design 狀態沿用：套件的卡寫 status: design 就不能指派。
	add("garden", "wip", "---\nschema: 1\nname: Wip\nstatus: design\n---\n還在畫。\n", true)
	if err := assign(ctx, m, "life", "garden.wip", owner); code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "還在設計中") {
		t.Fatalf("design：%v", err)
	}
}

func ptr[T any](v T) *T { return &v }

// 套件停用或被拒：角色不可指派，已經指派的秘書不壞；套件整個不見：用暫代卡，起得來，doctor 報。
// Guards: REQ-316, REQ-359
func TestPackagePersonaInactiveKeepsExistingSeat(t *testing.T) {
	m, lib, h, add, src := packageFixture(t, true)
	ctx := context.Background()
	add("garden", "fairy", fairyCard, true)
	must(t, assign(ctx, m, "code", "garden.fairy", owner))
	// 停用
	src.list[0].Active, src.list[0].Reason = false, "已停用"
	cards, err := lib.Library(ctx)
	must(t, err)
	var got sdk.PersonaCard
	for _, c := range cards {
		if c.Slug == "garden.fairy" {
			got = c
		}
	}
	if got.Unassignable == "" || strings.Join(got.Seats, ",") != "code" {
		t.Fatalf("標不能指派、仍顯示坐著：%+v", got)
	}
	if err := assign(ctx, m, "life", "garden.fairy", owner); code(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "garden") {
		t.Fatalf("不可新指派：%v", err)
	}
	if err := newAgent(ctx, m, "plant", "garden.fairy", "", owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不可建秘書：%v", err)
	}
	// 已經指派的：原值還在、重新指派同一位（原值）不報錯、指令照樣組得出來並含原卡內容。
	if p, _ := record(t, h, "agent", "code").Fields["persona"].(string); p != "garden.fairy" {
		t.Fatal(p)
	}
	ins, err := m.Instructions(ctx, "code", "s1")
	must(t, err)
	if !strings.Contains(ins, "我是花園裡的小仙子") {
		t.Fatal("停用後仍讀原卡")
	}
	if joined := strings.Join(m.Warnings(ctx), "\n"); !strings.Contains(joined, "garden") || !strings.Contains(joined, "已停用") {
		t.Fatalf("doctor 要警告：%q", joined)
	}
	if p := m.Problems(ctx); len(p) != 0 {
		t.Fatalf("套件停用只黃字、不紅字：%q", p)
	}
	// 整個套件不見：秘書照樣組得出指令（暫代卡），doctor 報。
	src.list = nil
	ins, err = m.Instructions(ctx, "code", "s2")
	must(t, err)
	if !strings.Contains(ins, "garden.fairy") || !strings.Contains(ins, "暫時沒有個性設定") {
		t.Fatalf("套件不見要有暫代人設：%q", ins)
	}
	if _, err := m.PersonaName(ctx, "code"); err != nil {
		t.Fatal(err)
	}
	if joined := strings.Join(m.Warnings(ctx), "\n"); !strings.Contains(joined, "套件不在了") {
		t.Fatalf("doctor：%q", joined)
	}
	if p := m.Problems(ctx); len(p) != 0 {
		t.Fatalf("套件不見只黃字、不紅字：%q", p)
	}
	// 換回有效角色就恢復正常。
	must(t, assign(ctx, m, "code", "athena", owner))
}

// fakeWriter 是 packages 的寫入介面的替身：把卡寫進 packages/mine/<id>/personas/<slug>.md，並加進套件角色清單；
// 層級不是 mine 的套件回 Forbidden、沒有的套件回 NotFound、同代號回 Conflict（跟真的 AddPackagePersona 一樣的錯誤碼）。
type fakeWriter struct {
	*fakePackagesMut
	home  string
	tiers map[string]string // 套件 id → 層級
}

func (w *fakeWriter) AddPackagePersona(pkgID, slug string, card []byte) (string, error) {
	if !personaref.IsPackageCode(personaref.PackageCode(pkgID, "x")) { // 跟 packages 一樣：套件 id 不合格是 Invalid
		return "", platform.Invalid("package", "套件 id 不正確")
	}
	tier, ok := w.tiers[pkgID]
	if !ok {
		return "", platform.NotFound("沒有這個套件：" + pkgID)
	}
	if tier != "mine" {
		return "", platform.Forbidden("只有自己的套件能新增角色")
	}
	dir := filepath.Join(w.home, "packages", "mine", pkgID)
	file := filepath.Join(dir, "personas", slug+".md")
	if _, err := os.Lstat(file); err == nil {
		return "", platform.Conflict("已經有角色 " + slug)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", err
	}
	if err := platform.AtomicWrite(file, card, 0o644); err != nil {
		return "", err
	}
	w.list = append(w.list, personaref.PackagePersona{Package: pkgID, Slug: slug, Path: file, Tier: tier, Active: true})
	return dir, nil
}

func writerFixture(t *testing.T) (*agent.Module, *Module, *homefixture.Home) {
	t.Helper()
	m, lib, h, _ := fixture(t)
	w := &fakeWriter{fakePackagesMut: &fakePackagesMut{}, home: h.Dir, tiers: map[string]string{"garden": "mine", "off": "official", "inst": "installed"}}
	lib.SetPackagePersonas(w)
	return m, lib, h
}

// 新增與複製都能去套件（只限 mine 層）：卡到位、設計中，名字沒給用代號；代號格式、只有主人、同代號衝突、官方與 installed 層的套件、沒有的套件都擋；複製時來源不動、我的角色連頭像一起過去。
// Guards: REQ-320, REQ-321, REQ-322
func TestPersonaToPackage(t *testing.T) {
	_, lib, h := writerFixture(t)
	ctx := context.Background()
	card, err := lib.CreatePersonaIn(ctx, "garden", "fairy", "", nil, owner)
	must(t, err)
	if card.Slug != "garden.fairy" || card.Source != personaref.SourcePackage || card.Status != personaref.StatusDesign || card.Package != "garden" || card.Name != "Fairy" {
		t.Fatalf("新卡：%+v", card)
	}
	if _, err := lib.CreatePersonaIn(ctx, "garden", "fairy", "", nil, owner); code(err) != sdk.ErrorCodeConflict {
		t.Fatalf("同一個套件同代號要 CONFLICT：%v", err)
	}
	if _, err := lib.CreatePersonaIn(ctx, "garden", "Bad Slug", "", nil, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("代號格式：%v", err)
	}
	if _, err := lib.CreatePersonaIn(ctx, "Bad_Pkg", "x", "", nil, owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("套件 id 格式：%v", err)
	}
	if _, err := lib.CreatePersonaIn(ctx, "garden", "x", "", nil, "life"); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("只有主人：%v", err)
	}
	for _, id := range []string{"off", "inst"} {
		if _, err := lib.CreatePersonaIn(ctx, id, "fairy", "", nil, owner); code(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%s 要 FORBIDDEN：%v", id, err)
		}
		if _, err := lib.CopyPersonaIn(ctx, id, "athena", "", "", nil, owner); code(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%s 複製要 FORBIDDEN：%v", id, err)
		}
	}
	if _, err := lib.CreatePersonaIn(ctx, "nope", "fairy", "", nil, owner); code(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有這個套件：%v", err)
	}

	// 複製我的角色進套件：來源與頭像都不動，頭像複製過去。
	_, err = lib.CreatePersona(ctx, "my_mina", "Mina", []string{"小米"}, owner)
	must(t, err)
	writeCard(t, h, "personas/mine/avatars/my_mina.png", "PNG")
	srcCard := readFile(t, h, "personas/mine/my_mina.md")
	c2, err := lib.CopyPersonaIn(ctx, "garden", "my_mina", "", "", nil, owner)
	must(t, err)
	if c2.Slug != "garden.mina" || c2.Name != "Mina" || len(c2.Aka) != 1 || c2.Status != personaref.StatusDesign {
		t.Fatalf("複製：%+v", c2)
	}
	if readFile(t, h, "personas/mine/my_mina.md") != srcCard || readFile(t, h, "personas/mine/avatars/my_mina.png") != "PNG" {
		t.Fatal("來源不動")
	}
	if readFile(t, h, "packages/mine/garden/personas/avatars/mina.png") != "PNG" {
		t.Fatal("頭像要複製進套件")
	}
	if c3, err := lib.CopyPersonaIn(ctx, "garden", "my_mina", "mina2", "", nil, owner); err != nil || c3.Slug != "garden.mina2" {
		t.Fatalf("--slug：%+v %v", c3, err)
	}
}

// 套件內的卡：設計中不能指派，上線之後可以；有秘書坐著不能移回設計中；官方與 installed 層的套件的卡改不了狀態。
// Guards: REQ-323
func TestPackagePersonaStatusAndAssign(t *testing.T) {
	m, lib, _ := writerFixture(t)
	ctx := context.Background()
	_, err := lib.CreatePersonaIn(ctx, "garden", "fairy", "", nil, owner)
	must(t, err)
	if err := assign(ctx, m, "code", "garden.fairy", owner); code(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("設計中不能指派：%v", err)
	}
	c, err := lib.SetPersonaStatus(ctx, "garden.fairy", personaref.StatusLive, owner)
	if err != nil || c.Status != personaref.StatusLive || c.Source != personaref.SourcePackage {
		t.Fatalf("live：%+v %v", c, err)
	}
	must(t, assign(ctx, m, "code", "garden.fairy", owner))
	if _, err := lib.SetPersonaStatus(ctx, "garden.fairy", personaref.StatusDesign, owner); err == nil {
		t.Fatal("有秘書坐著不能移回設計中")
	}
	// installed 層的卡：不能改狀態。
	w := lib.pkgSrc.src.(*fakeWriter)
	w.list = append(w.list, personaref.PackagePersona{Package: "shared", Slug: "fairy", Path: writeFairy(t, lib), Tier: "installed", Active: true})
	if _, err := lib.SetPersonaStatus(ctx, "shared.fairy", personaref.StatusDesign, owner); code(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("installed 的卡唯讀：%v", err)
	}
}

func writeFairy(t *testing.T, lib *Module) string {
	t.Helper()
	p := filepath.Join(lib.home, "packages", "installed", "shared", "personas", "fairy.md")
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, []byte(fairyCard), 0o644))
	return p
}
