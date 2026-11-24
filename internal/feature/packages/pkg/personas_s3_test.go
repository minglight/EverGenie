package pkg

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

const s3Owner = "owner(cli)"

// s3Env：一個 mine 套件 garden（有樣板秘書 garden）、官方套件 off、分享套件 inst，加上接好的名冊。
func s3Env(t *testing.T) (*harness, *agent.Module) {
	t.Helper()
	h := newHarness(t)
	agentTpl := map[string]string{"agents/garden/agent.md": "---\nduty: 照顧花園\n---\n你照顧花園。\n"}
	h.write(sdk.TierMine, "garden", dataPkg("garden", "1.0.0", "[[contributes.agent]]\nid = \"garden\"\ntemplate = \"agents/garden/agent.md\"\n"), agentTpl)
	h.write(sdk.TierOfficial, "off", dataPkg("off", "1.0.0", ""), nil)
	h.write(sdk.TierInstalled, "inst", dataPkg("inst", "1.0.0", ""), nil)
	h.start()
	ctx := context.Background()
	roster := agent.New(agent.Deps{Vault: h.home.Vault, Clock: h.home.Clock, Bus: quietBus{}})
	roster.SetHome(h.home.Dir)
	if err := roster.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roster.Stop(ctx) })
	roster.SetPersonaSource(stubLib{m: h.m, home: h.home.Dir})
	if err := os.MkdirAll(filepath.Join(h.home.Dir, "personas", "mine", "avatars"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.home.Dir, "personas", "athena.md"), []byte("---\nname: Athena\n---\n官方\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.home.WithRecords(vault.Record{Kind: "agent", ID: "code", Fields: map[string]any{"duty": "測試", "persona": "athena"}, Body: "職責"})
	return h, roster
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// 套件的寫入介面（角色庫新增、複製去套件時呼叫）只限 mine 層：卡與宣告一起到位、寫前備份；官方與 installed 回 FORBIDDEN、沒有的套件 NOT_FOUND、同代號 CONFLICT。
// 角色庫那一邊（代號格式、只有主人、頭像一起複製、複製的名字）在 internal/feature/agents/persona 的測試。
// Guards: REQ-320, REQ-321, REQ-322
func TestPersonaToPackage(t *testing.T) {
	h, _ := s3Env(t)
	gdir := filepath.Join(h.home.Dir, "packages", "mine", "garden")

	addCard(t, h.m, "garden", "fairy", "Fairy")
	if got := h.m.PackagePersonas(); len(got) != 1 || got[0].Package != "garden" || got[0].Slug != "fairy" || got[0].Tier != "mine" || !got[0].Active {
		t.Fatalf("新卡要出現在套件角色清單：%+v", got)
	}
	if !strings.Contains(readFile(t, filepath.Join(gdir, "package.toml")), "slug = \"fairy\"") {
		t.Fatal("package.toml 要宣告新角色")
	}
	if _, err := ParseManifest(gdir, sdk.TierMine); err != nil {
		t.Fatalf("寫完 manifest 要驗得過：%v", err)
	}
	card := []byte("---\nschema: 1\nname: Fairy\n---\n內文\n")
	if _, err := h.m.AddPackagePersona("garden", "fairy", card); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("同一個套件同代號要 CONFLICT：%v", err)
	}
	if backups, _ := filepath.Glob(filepath.Join(h.home.Dir, "runtime", "backups", "packages", "garden-package.toml-*")); len(backups) == 0 {
		t.Fatal("寫 package.toml 前要備份")
	}

	// 官方、installed、不存在的套件。
	for _, id := range []string{"off", "inst"} {
		before := readFile(t, filepath.Join(h.home.Dir, "packages", "installed", "inst", "package.toml"))
		if _, err := h.m.AddPackagePersona(id, "fairy", card); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%s 要 FORBIDDEN：%v", id, err)
		}
		if readFile(t, filepath.Join(h.home.Dir, "packages", "installed", "inst", "package.toml")) != before {
			t.Fatal("被拒絕不能改檔")
		}
	}
	if _, err := os.Lstat(filepath.Join(h.official, "off", "personas")); err == nil {
		t.Fatal("官方資料夾不能被寫入")
	}
	if _, err := h.m.AddPackagePersona("Bad_Pkg", "fairy", card); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("套件 id 不合格要 Invalid（規則歸 packages）：%v", err)
	}
	if _, err := h.m.AddPackagePersona("nope", "fairy", card); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有這個套件：%v", err)
	}
}

// 寫 manifest 失敗會倒回：寫入失敗、寫完驗證不過，package.toml 與卡檔都回到原樣。
// Guards: REQ-322
func TestPersonaToPackageRollback(t *testing.T) {
	h, _ := s3Env(t)
	gdir := filepath.Join(h.home.Dir, "packages", "mine", "garden")
	manPath := filepath.Join(gdir, "package.toml")
	before := readFile(t, manPath)
	for name, writer := range map[string]func(string, []byte, os.FileMode) error{
		"寫入失敗": func(p string, b []byte, m os.FileMode) error {
			if filepath.Base(p) == "package.toml" {
				return fmt.Errorf("磁碟滿了")
			}
			return platform.AtomicWrite(p, b, m)
		},
		"寫完驗證不過": func(p string, b []byte, m os.FileMode) error {
			if filepath.Base(p) == "package.toml" {
				return platform.AtomicWrite(p, []byte("這不是 toml ((("), m)
			}
			return platform.AtomicWrite(p, b, m)
		},
	} {
		h.m.atomicWrite = writer
		if _, err := h.m.AddPackagePersona("garden", "fairy", []byte("---\nschema: 1\nname: Fairy\n---\n")); err == nil {
			t.Fatalf("%s：要回錯", name)
		}
		if got := readFile(t, manPath); got != before {
			t.Fatalf("%s：package.toml 沒倒回：%q", name, got)
		}
		if _, err := os.Lstat(filepath.Join(gdir, "personas", "fairy.md")); err == nil {
			t.Fatalf("%s：卡檔要刪掉", name)
		}
	}
	h.m.atomicWrite = nil
	addCard(t, h.m, "garden", "fairy", "Fairy") // 倒回後還能正常新增
}

// 套件內的卡：設計中不能指派，上線之後可以（名冊經 PersonaSource 讀套件的卡）。官方與 installed 層的卡改不了狀態是角色庫的規則（feature/agents/persona 的測試）。
// Guards: REQ-323
func TestAssignPackagePersona(t *testing.T) {
	h, roster := s3Env(t)
	ctx := context.Background()
	lib := stubLib{m: h.m, home: h.home.Dir}
	addCard(t, h.m, "garden", "fairy", "Fairy")
	if err := assign(ctx, roster, "code", "garden.fairy", s3Owner); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("設計中不能指派：%v", err)
	}
	lib.setStatus(t, "garden.fairy", personaref.StatusLive)
	if err := assign(ctx, roster, "code", "garden.fairy", s3Owner); err != nil {
		t.Fatalf("上線後可指派：%v", err)
	}
}

// 假名冊主機：把 Sync 接到真的 agent 模組，測整條 new → live → sync → agents。
type rosterHost struct{ r *agent.Module }

func (rosterHost) DefaultPersona() string { return "" }
func (h rosterHost) AgentDuties(ctx context.Context, id string) (string, bool, bool, error) {
	rows, err := h.r.List(ctx)
	if err != nil {
		return "", false, false, err
	}
	for _, a := range rows {
		if a.ID == id {
			return a.Body, true, false, nil
		}
	}
	return "", false, false, nil
}
func (h rosterHost) CreateAgent(ctx context.Context, id, persona, duty string, _ []string, by string) error {
	return newAgent(ctx, h.r, id, persona, duty, by)
}
func (h rosterHost) SetAgentField(ctx context.Context, id, field, value, by string) error {
	return setAgentField(ctx, h.r, id, field, value, by)
}
func (h rosterHost) CheckPersona(p string) error { return h.r.CheckAssignable(p) }
func (h rosterHost) WriteDuties(ctx context.Context, id, body, by string) error {
	return h.r.WriteDuties(ctx, id, body, by)
}
func (rosterHost) JobIDs(context.Context, string) ([]string, error) { return nil, nil }
func (rosterHost) SaveJob(context.Context, *sdk.Job, string) error  { return nil }

// fixture：new --to-package garden fairy → live → sync --apply --persona garden.fairy → 名冊裡 garden 的人設是 garden.fairy。
// Guards: REQ-320, REQ-323, REQ-324
func TestPersonaToPackageFixtureEndToEnd(t *testing.T) {
	h, roster := s3Env(t)
	ctx := context.Background()
	h.m.SetSyncHost(rosterHost{roster})
	addCard(t, h.m, "garden", "fairy", "Fairy")
	stubLib{m: h.m, home: h.home.Dir}.setStatus(t, "garden.fairy", personaref.StatusLive)
	pre, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Persona: "garden.fairy", By: &sdk.Actor{Name: s3Owner}})
	if err != nil || len(pre.Agents) != 1 || pre.Agents[0].Persona != "garden.fairy" || pre.Agents[0].PersonaFrom != "request" {
		t.Fatalf("預演：%+v %v", pre, err)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Persona: "garden.fairy", Apply: true, By: &sdk.Actor{Name: s3Owner}}); err != nil {
		t.Fatal(err)
	}
	rows, err := roster.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range rows {
		if a.ID == "garden" {
			found = true
			if a.Fields["persona"] != "garden.fairy" {
				t.Fatalf("garden 的人設：%v", a.Fields["persona"])
			}
		}
	}
	if !found {
		t.Fatal("名冊要有 garden")
	}
}

const defaultPersonaAgent = "---\nduty: 照顧花園\npersona: %s\n---\n你照顧花園。\n"

func defaultPersonaPkg(h *harness, persona string) {
	h.write(sdk.TierMine, "garden", dataPkg("garden", "1.0.0", "[[contributes.agent]]\nid = \"garden\"\ntemplate = \"agents/garden/agent.md\"\n"),
		map[string]string{"agents/garden/agent.md": fmt.Sprintf(defaultPersonaAgent, persona)})
}

// 套件樣板可寫預設角色：預演顯示來源；沒給 --persona 就用它；給了以主人指定的為準；預設角色不能用就擋；格式錯拒載。
// Guards: REQ-324
func TestSyncDefaultPersona(t *testing.T) {
	h := newHarness(t)
	defaultPersonaPkg(h, "garden.fairy")
	host := &fakeSync{persona: "builder", duties: map[string]string{}, jobs: map[string][]string{}}
	h.m.SetSyncHost(host)
	h.start()
	ctx := context.Background()
	owner := &sdk.Actor{Name: s3Owner}

	pre, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", By: owner})
	if err != nil || pre.Agents[0].Persona != "garden.fairy" || pre.Agents[0].PersonaFrom != "template" {
		t.Fatalf("預演顯示樣板預設：%+v %v", pre, err)
	}
	if len(host.calls) != 0 {
		t.Fatal("預演不能改東西")
	}
	over, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Persona: "athena", By: owner})
	if err != nil || over.Agents[0].Persona != "athena" || over.Agents[0].PersonaFrom != "request" {
		t.Fatalf("--persona 優先：%+v %v", over, err)
	}
	// 沒給 --persona：套用時用樣板預設，仍須主人。
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Apply: true, By: &sdk.Actor{Name: "life"}}); platform.CodeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("秘書不能 apply：%v", err)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Apply: true, By: owner}); err != nil {
		t.Fatal(err)
	}
	if len(host.calls) == 0 || !strings.Contains(host.calls[0], "persona=garden.fairy") {
		t.Fatalf("要用樣板預設的角色建：%v", host.calls)
	}
	// 預設角色不能用：預演就擋，並提示 --persona；給了 --persona 就不卡。
	host2 := &fakeSync{duties: map[string]string{}, jobs: map[string][]string{}, checkErr: platform.Invalid("persona", "還在設計中")}
	h.m.SetSyncHost(host2)
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", By: owner}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "--persona") {
		t.Fatalf("預設角色不能用要擋：%v", err)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Persona: "athena", By: owner}); err != nil {
		t.Fatalf("指定 --persona 不受樣板預設影響：%v", err)
	}
	// 沒有樣板預設、沒有家預設、沒給：套用要求指定人設。
	h3 := newHarness(t)
	h3.write(sdk.TierMine, "garden", dataPkg("garden", "1.0.0", "[[contributes.agent]]\nid = \"garden\"\ntemplate = \"agents/garden/agent.md\"\n"),
		map[string]string{"agents/garden/agent.md": "---\nduty: 花園\n---\n職責\n"})
	h3.m.SetSyncHost(&fakeSync{duties: map[string]string{}, jobs: map[string][]string{}})
	h3.start()
	if _, err := h3.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Apply: true, By: owner}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("都沒有預設要求指定：%v", err)
	}
	// 格式錯：整個套件被拒。
	h4 := newHarness(t)
	defaultPersonaPkg(h4, "Bad Name")
	h4.start()
	if p := h4.get("garden"); p.State != sdk.PackageStateRejected {
		t.Fatalf("persona 格式錯要被拒：%+v", p)
	}
}
