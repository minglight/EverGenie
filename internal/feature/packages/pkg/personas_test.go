package pkg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const gardenManifestExtra = "[[contributes.persona]]\nslug = \"fairy\"\npath = \"personas/fairy.md\"\n"
const fairyCardText = "---\nschema: 1\nname: Fairy\n---\n我是花園裡的小仙子。\n"

func gardenPackage(h *harness, tier sdk.Tier) string {
	return h.write(tier, "garden", dataPkg("garden", "1.0.0", gardenManifestExtra), map[string]string{"personas/fairy.md": fairyCardText})
}

// 套件與角色庫串起來：放一個帶角色的 mine 套件 → 角色庫列出 garden.fairy；停用後不可指派、已指派的秘書保留；移除後不壞。
// Guards: REQ-313, REQ-314, REQ-316
func TestPackagePersonaFixtureEndToEnd(t *testing.T) {
	h := newHarness(t)
	gardenPackage(h, sdk.TierMine)
	h.start()
	ctx := context.Background()
	roster := agent.New(agent.Deps{Vault: h.home.Vault, Clock: h.home.Clock, Bus: quietBus{}})
	roster.SetHome(h.home.Dir)
	lib := stubLib{m: h.m, home: h.home.Dir}
	if err := roster.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roster.Stop(ctx) })
	roster.SetPersonaSource(lib)
	if err := os.MkdirAll(filepath.Join(h.home.Dir, "personas"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.home.Dir, "personas", "athena.md"), []byte("---\nname: Athena\n---\n官方\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.home.WithRecords(vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{"duty": "測試", "persona": "athena"}, Body: "職責"})

	slugs := func() map[string]sdk.PersonaCard {
		cards, err := lib.Library(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]sdk.PersonaCard{}
		for _, c := range cards {
			out[c.Slug] = c
		}
		return out
	}
	c, ok := slugs()["garden.fairy"]
	if !ok || c.Source != personaref.SourcePackage || c.Package != "garden" || c.Unassignable != "" {
		t.Fatalf("角色庫要有 garden.fairy：%+v", c)
	}
	if err := assign(ctx, roster, "code", "garden.fairy", "owner(web)"); err != nil {
		t.Fatal(err)
	}

	// 停用：不可新指派、已指派保留、指令照組。
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "garden", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	c = slugs()["garden.fairy"]
	if c.Unassignable == "" {
		t.Fatalf("停用後標不能指派（坐位保留由下面的指令組裝驗，角色庫列坐位在 feature/agents/persona 的測試）：%+v", c)
	}
	if err := assign(ctx, roster, "code", "athena", "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if err := assign(ctx, roster, "code", "garden.fairy", "owner(web)"); err == nil {
		t.Fatal("停用的套件角色不能指派")
	}
	h.home.WithRecords(vault.Record{Kind: "agent", ID: "plant", Fields: map[string]any{"duty": "測試", "persona": "garden.fairy"}, Body: "職責"})
	ins, err := roster.Instructions(ctx, "plant", "s")
	if err != nil || !strings.Contains(ins, "我是花園裡的小仙子") {
		t.Fatalf("停用後已指派的秘書仍讀原卡：%v", err)
	}
	// 重新啟用：恢復可指派。
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "garden", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := assign(ctx, roster, "code", "garden.fairy", "owner(web)"); err != nil {
		t.Fatalf("啟用後可指派：%v", err)
	}
	// 套件整個移除：角色從清單消失，秘書仍組得出指令。
	if err := os.RemoveAll(filepath.Join(h.home.Dir, "packages", "mine", "garden")); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Reload(ctx, &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := slugs()["garden.fairy"]; ok {
		t.Fatal("套件移除後角色要從清單消失")
	}
	if _, err := roster.Instructions(ctx, "plant", "s2"); err != nil {
		t.Fatalf("套件不見秘書也要起得來：%v", err)
	}
}

// 兩個套件都有 fairy：各自一個有效代號，不撞；被拒的套件角色標不能指派；manifest 的角色代號與重複被擋。
// Guards: REQ-315
func TestPackagePersonaDeclarations(t *testing.T) {
	h := newHarness(t)
	gardenPackage(h, sdk.TierMine)
	h.write(sdk.TierMine, "park", dataPkg("park", "1.0.0", gardenManifestExtra), map[string]string{"personas/fairy.md": fairyCardText})
	h.write(sdk.TierMine, "badslug", dataPkg("badslug", "1.0.0", "[[contributes.persona]]\nslug = \"Fairy-1\"\npath = \"personas/fairy.md\"\n"), map[string]string{"personas/fairy.md": fairyCardText})
	h.write(sdk.TierMine, "dup", dataPkg("dup", "1.0.0", gardenManifestExtra+gardenManifestExtra), map[string]string{"personas/fairy.md": fairyCardText})
	h.write(sdk.TierMine, "escape", dataPkg("escape", "1.0.0", "[[contributes.persona]]\nslug = \"x\"\npath = \"../x.md\"\n"), nil)
	h.start()
	got := map[string]personaref.PackagePersona{}
	for _, p := range h.m.PackagePersonas() {
		got[personaref.PackageCode(p.Package, p.Slug)] = p
	}
	if len(got) != 2 || !got["garden.fairy"].Active || !got["park.fairy"].Active {
		t.Fatalf("只有 garden 與 park 的 fairy：%+v", got)
	}
	for _, id := range []string{"badslug", "dup", "escape"} {
		if p := h.get(id); p.State != sdk.PackageStateRejected {
			t.Fatalf("%s 要被拒：%+v", id, p)
		}
	}
	// 路徑指到套件資料夾外的符號連結：略過，不會讀到外面的檔。
	outside := filepath.Join(t.TempDir(), "o.md")
	if err := os.WriteFile(outside, []byte(fairyCardText), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.home.Dir, "packages", "mine", "garden", "personas")
	if err := os.Remove(filepath.Join(dir, "fairy.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "fairy.md")); err != nil {
		t.Fatal(err)
	}
	for _, p := range h.m.PackagePersonas() {
		if p.Package == "garden" {
			t.Fatalf("符號連結跑出套件資料夾不能算：%+v", p)
		}
	}
}

// quietBus 讓 agent 模組在測試裡不收也不發事件。
type quietBus struct{}

func (quietBus) Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return make(chan *sdk.BusSubscribeResponse), nil
}
func (quietBus) Publish(context.Context, string, map[string]any) error { return nil }
