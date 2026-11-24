package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

// fakePersonas 是記憶體裡的 PersonaSource：不碰 personas/ 資料夾。
type fakePersonas struct {
	cards map[string]PersonaCardInfo
	level string
	style string
	exist bool
	asked []string
}

func (f *fakePersonas) Card(slug string) (PersonaCardInfo, error) {
	f.asked = append(f.asked, slug)
	c, ok := f.cards[slug]
	if !ok {
		return PersonaCardInfo{}, platform.Invalid("persona", "人設卡不存在或無法讀取")
	}
	return c, nil
}

func (f *fakePersonas) Style() (string, string, bool, error) { return f.level, f.style, f.exist, nil }

func fakeCard(name, body, source string) PersonaCardInfo {
	return PersonaCardInfo{Source: source, Record: vault.Record{Fields: map[string]any{"name": name}, Body: body}}
}

// 名冊讀角色卡與說話層只經 PersonaSource：接一個假的，家裡沒有任何 personas/ 檔案也照常運作。
func TestRosterUsesInjectedPersonaSource(t *testing.T) {
	h := homefixture.New(t)
	fake := &fakePersonas{
		cards: map[string]PersonaCardInfo{
			"ghost": fakeCard("幽靈", "假來源的人設內文", personaref.SourceOfficial),
			"wip":   fakeCard("草稿", "還在設計", personaref.SourceMine),
		},
		level: "d", style: "假來源的說話層", exist: true,
	}
	wip := fake.cards["wip"]
	wip.Record.Fields["status"], wip.Status = personaref.StatusDesign, personaref.StatusDesign // 角色庫判斷好放進 Status
	fake.cards["wip"] = wip
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Bus: &recordingBus{}})
	m.SetPersonaSource(fake)
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	h.WithRecords(vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{"duty": "測試職責", "persona": "ghost"}, Body: "官方職責"})
	ctx := context.Background()

	if name, err := m.PersonaName(ctx, "code"); err != nil || name != "幽靈" {
		t.Fatalf("PersonaName = %q, %v", name, err)
	}
	text, err := m.Instructions(ctx, "code", "s1")
	must(t, err)
	for _, want := range []string{"假來源的人設內文", "假來源的說話層"} {
		if !strings.Contains(text, want) {
			t.Fatalf("Instructions 沒有 %q", want)
		}
	}
	if err := m.CheckAssignable("wip"); sdk.ErrorCode(platform.CodeOf(err)) != sdk.ErrorCodeInvalid {
		t.Fatalf("設計中的卡要擋，得到 %v", err)
	}
	if err := m.CheckAssignable("nobody"); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不存在的卡要擋，得到 %v", err)
	}
	if len(fake.asked) == 0 {
		t.Fatal("名冊沒有經過假來源")
	}

	// doctor：坐著設計中的卡、說話層設 custom 但檔案不在，都從假來源判斷。
	h.WithRecords(vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{"duty": "x", "persona": "wip"}, Body: "官方職責"})
	fake.level, fake.exist = "custom", false
	joined := strings.Join(m.Problems(ctx), "\n")
	for _, want := range []string{"在設計中", "custom"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Problems 沒有 %q：%s", want, joined)
		}
	}
}

// 套件角色停用：假來源只要回 Inactive，名冊就擋新指派、doctor 只給黃字。
func TestRosterPersonaSourceInactivePackage(t *testing.T) {
	h := homefixture.New(t)
	pkg := fakeCard("仙子", "套件角色", personaref.SourcePackage)
	pkg.PackageID, pkg.Inactive = "garden", "已停用"
	fake := &fakePersonas{cards: map[string]PersonaCardInfo{"garden.fairy": pkg, "plain": fakeCard("平實", "x", personaref.SourceOfficial)}, level: "c", style: "c", exist: false}
	m := New(Deps{Vault: h.Vault, Clock: h.Clock, Bus: &recordingBus{}})
	m.SetPersonaSource(fake)
	m.SetHome(h.Dir)
	must(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Stop(context.Background()) })
	h.WithRecords(vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{"duty": "x", "persona": "garden.fairy"}, Body: "官方職責"})
	h.WithRecords(vault.Record{Kind: "agent", ID: "life", Fields: map[string]any{"duty": "x", "persona": "plain"}, Body: "官方職責"})
	ctx := context.Background()

	err := m.CheckAssignable("garden.fairy")
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "garden") {
		t.Fatalf("停用的套件角色要擋並說明套件：%v", err)
	}
	if w := strings.Join(m.Warnings(ctx), "\n"); !strings.Contains(w, "garden") {
		t.Fatalf("Warnings = %q", w)
	}
	for _, p := range m.Problems(ctx) {
		if strings.Contains(p, "garden.fairy") {
			t.Fatalf("停用的套件角色不該是紅字：%s", p)
		}
	}
}
