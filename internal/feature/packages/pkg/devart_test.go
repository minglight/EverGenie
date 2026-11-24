package pkg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 一般家沒有生圖秘書（DEV-160 S5a，作者 2026-11-15）：生圖職位 art 的樣板只在開發工坊 admin
// （dev_only、選用套件，只有開發家開著它，ADM-39）。這裡用 repo 裡真的官方套件，跟 TestOfficialPackagesDiscovered 一樣。

// officialRepoHarness：官方資料夾指到 repo 的 packages/official；附程式的官方套件由別人監督（不啟動行程）。
func officialRepoHarness(t *testing.T) *harness {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "packages", "official"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	h.m.SetOfficialDir(root)
	h.m.SetExternal(claimAll{})
	h.start()
	return h
}

// claimable 是「職位與秘書」、設定精靈、歡迎頁能列出來加的職位（職位 id → 套件）：
// 啟用中、沒被拒的套件帶來的，跟看板 functions.ts 的 unclaimedFunctions 同一條規則。
func claimable(h *harness) map[string]string {
	out := map[string]string{}
	for _, p := range h.list() {
		if !p.Enabled || p.State == sdk.PackageStateRejected {
			continue
		}
		for _, a := range p.Agents {
			if _, seen := out[a.ID]; !seen {
				out[a.ID] = p.ID
			}
		}
	}
	return out
}

// 一般家（開發工坊沒開）：每個加職位的入口都沒有 art——能加的職位清單、指名同步 secretaries（沒有這個職位）、
// 同步或認領 admin 的 art（已停用）、整包同步 secretaries；主人自己建一個叫 art 的職位也不套官方職責（那是他自己的職位）。
func TestArtNotAddableWithoutDevPackage(t *testing.T) {
	h := officialRepoHarness(t)
	h.m.SetSyncHost(&fakeSync{persona: "builder", duties: map[string]string{}, jobs: map[string][]string{}})
	h.m.SetClaimHost(&fakeClaim{})
	ctx := context.Background()
	owner := &sdk.Actor{Name: s3Owner}

	t.Run("能加的職位清單", func(t *testing.T) {
		got := claimable(h)
		for _, id := range []string{"life", "work", "code", "system"} {
			if got[id] != "secretaries" {
				t.Errorf("一般家要能加 %s（基本職位）：%v", id, got)
			}
		}
		if pkgID, ok := got["art"]; ok {
			t.Errorf("一般家能加的職位不能有 art（來自 %s）", pkgID)
		}
	})
	t.Run("指名同步基本職位的 art", func(t *testing.T) {
		if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "secretaries", Agent: "art", By: owner}); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
			t.Errorf("基本職位已經沒有 art：%v", err)
		}
	})
	t.Run("同步開發工坊的 art", func(t *testing.T) {
		if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "admin", Agent: "art", By: owner}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "停用") {
			t.Errorf("開發工坊沒開就不能同步它的 art：%v", err)
		}
	})
	t.Run("認領通知", func(t *testing.T) {
		if _, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "admin", Agent: "art", By: &sdk.Actor{Name: "code"}}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("開發工坊沒開就不能發 art 的認領通知：%v", err)
		}
	})
	t.Run("整包同步基本職位", func(t *testing.T) {
		whole, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "secretaries", By: owner})
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range whole.Agents {
			if a.ID == "art" {
				t.Errorf("整包同步基本職位不能帶出 art：%+v", whole)
			}
		}
		if len(whole.Optional) != 0 {
			t.Errorf("基本職位沒有選用職位了：%+v", whole.Optional)
		}
	})
	t.Run("自己建的 art 不套官方職責", func(t *testing.T) {
		if ref := h.m.TemplateDutiesRef("art"); ref != "" {
			t.Errorf("一般家自己建的 art 不套開發工坊的官方職責：%q", ref)
		}
		if ref := h.m.TemplateDutiesRef("life"); ref != "secretaries/life" {
			t.Errorf("基本職位照舊套官方職責：%q", ref)
		}
	})
}

// 開發家（開發工坊開著）：art 列出來、標選用（設定精靈、歡迎頁不列），整包同步開發工坊不跟著加、指名才建，建秘書時套官方職責 admin/art。
func TestArtOptionalWhenDevPackageEnabled(t *testing.T) {
	h := officialRepoHarness(t)
	h.m.SetSyncHost(&fakeSync{persona: "builder", duties: map[string]string{}, jobs: map[string][]string{}})
	ctx := context.Background()
	owner := &sdk.Actor{Name: s3Owner}
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "admin", Enabled: true, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}

	t.Run("能加的職位清單有 art、標選用", func(t *testing.T) {
		if got := claimable(h); got["art"] != "admin" {
			t.Errorf("開發家要能加 art：%v", got)
		}
		for _, a := range h.get("admin").Agents {
			if a.ID == "art" && !a.Optional {
				t.Error("開發家的 art 也是選用職位：設定精靈、歡迎頁不列")
			}
		}
	})
	t.Run("建秘書套官方職責", func(t *testing.T) {
		if ref := h.m.TemplateDutiesRef("art"); ref != "admin/art" {
			t.Errorf("開發家建 art 套官方職責 admin/art：%q", ref)
		}
	})
	t.Run("整包同步不跟著加", func(t *testing.T) {
		pre, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "admin", By: owner})
		if err != nil {
			t.Fatal(err)
		}
		if len(pre.Agents) != 0 || strings.Join(pre.Optional, ",") != "art" {
			t.Errorf("整包同步開發工坊不跟著加 art：%+v", pre)
		}
	})
	t.Run("指名才建", func(t *testing.T) {
		named, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "admin", Agent: "art", By: owner})
		if err != nil || len(named.Agents) != 1 || named.Agents[0].ID != "art" || named.Agents[0].Action != "create" {
			t.Errorf("開發家指名 art 才建：%+v %v", named, err)
		}
	})
}

// basicAgents 是基本職位四位都已經有人的名冊（同步 secretaries --apply 時不用建新秘書）。
func basicAgents() []sdk.Record {
	return []sdk.Record{
		{Kind: "agent", ID: "life", Fields: map[string]any{"duty": "生活", "persona": "athena", "enabled": true, "duties_from": "secretaries/life"}},
		{Kind: "agent", ID: "work", Fields: map[string]any{"duty": "工作", "persona": "athena", "enabled": false, "duties_from": "secretaries/work"}},
		{Kind: "agent", ID: "code", Fields: map[string]any{"duty": "寫程式", "persona": "athena", "enabled": false, "duties_from": "secretaries/code"}},
		{Kind: "agent", ID: "system", Fields: map[string]any{"duty": "電腦", "persona": "athena", "enabled": false, "duties_from": "secretaries/system"}},
	}
}

// rosterOn 開一份真的名冊（vault 在 h 的家），職責接 h 的官方樣板（跟 cmd/evergenie/registry.go 一樣），再放進 records。
func rosterOn(t *testing.T, h *harness, records ...sdk.Record) *agent.Module {
	t.Helper()
	ctx := context.Background()
	roster := agent.New(agent.Deps{Vault: h.home.Vault, Clock: h.home.Clock, Bus: quietBus{}})
	roster.SetHome(h.home.Dir)
	roster.SetOfficialDuties(h.m.OfficialDuties)
	if err := roster.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = roster.Stop(ctx) })
	h.home.WithRecords(records...) // 名冊（agent 種類）要先開才寫得進去
	return roster
}

// 已經有 art 的家（主人自己建的 art：沒有 duties_from，職責寫在 agent.md 內文）：
// 樣板從 secretaries 搬到 admin 之後，同步基本職位（整包 --apply）不碰 art；開發家同步開發工坊（整包、指名 art，都 --apply）
// 回 same、不寫職責；agent.md 一個位元都不變，開場讀到的職責還是主人寫的那份。用真的名冊與 vault。
func TestExistingArtKeepsDutiesAfterTemplateMoved(t *testing.T) {
	h := officialRepoHarness(t)
	ctx := context.Background()
	const custom = "# art 的職責\n\n## 你主動照顧的事\n- 主人客製：用他自己的生圖流程"
	roster := rosterOn(t, h, append(basicAgents(),
		sdk.Record{Kind: "agent", ID: "art", Fields: map[string]any{"duty": "生圖", "persona": "athena", "enabled": true}, Body: custom})...)
	h.m.SetSyncHost(rosterHost{roster})
	path := filepath.Join(h.home.Dir, "vault", "agents", "art", "agent.md")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := &sdk.Actor{Name: s3Owner}
	same := func(label string) {
		t.Helper()
		if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
			t.Fatalf("%s 之後 art 的 agent.md 變了：\n%s", label, after)
		}
		if _, err := os.Stat(filepath.Join(h.home.Dir, "vault", "agents", "art", "addendum.md")); !os.IsNotExist(err) {
			t.Fatalf("%s 不該替 art 多寫附加規則", label)
		}
	}

	r, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "secretaries", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range r.Agents {
		if a.ID == "art" {
			t.Fatalf("同步基本職位不該再碰 art：%+v", r.Agents)
		}
	}
	same("同步基本職位")

	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "admin", Enabled: true, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []*sdk.PackageSyncRequest{
		{ID: "admin", Apply: true, By: owner},
		{ID: "admin", Agent: "art", Apply: true, By: owner},
	} {
		r, err := h.m.Sync(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Agents) != 1 || r.Agents[0].ID != "art" || r.Agents[0].Action != "same" {
			t.Fatalf("開發工坊同步（agent=%q）：已經有的 art 要是 same：%+v", req.Agent, r.Agents)
		}
		same("同步開發工坊（agent=" + req.Agent + "）")
	}

	rec, err := h.home.Vault.Get(ctx, "agent", "art")
	if err != nil {
		t.Fatal(err)
	}
	if agent.DutiesFrom(rec.Fields) != "" || roster.DutiesOf(rec) != custom {
		t.Fatalf("art 的職責還是主人寫的那份：duties_from=%q 職責=%q", agent.DutiesFrom(rec.Fields), roster.DutiesOf(rec))
	}
}

// DEV-157 部署後到 DEV-160 之間，從看板認領的 art 在名冊上記的是 duties_from: secretaries/art。
// 樣板搬到開發工坊之後她開場照樣讀得到官方職責（不變成「讀不到樣板」），一般家（開發工坊沒開）也一樣；同步基本職位不碰她。
func TestOldSecretariesArtRefStillReadsDuties(t *testing.T) {
	h := officialRepoHarness(t)
	ctx := context.Background()
	roster := rosterOn(t, h, append(basicAgents(),
		sdk.Record{Kind: "agent", ID: "art", Fields: map[string]any{"duty": "生圖", "persona": "athena", "enabled": true, "duties_from": "secretaries/art"}})...)
	h.m.SetSyncHost(rosterHost{roster})
	want, ok := h.m.OfficialDuties("admin/art")
	if !ok || want == "" {
		t.Fatal("開發工坊要帶 art 的樣板")
	}
	read := func() string {
		t.Helper()
		rec, err := h.home.Vault.Get(ctx, "agent", "art")
		if err != nil {
			t.Fatal(err)
		}
		return roster.DutiesOf(rec)
	}
	if got := read(); got != want {
		t.Fatalf("記著 secretaries/art 的 art 要讀到開發工坊那份樣板：%q", got)
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "secretaries", Apply: true, By: &sdk.Actor{Name: s3Owner}}); err != nil {
		t.Fatal(err)
	}
	rec, err := h.home.Vault.Get(ctx, "agent", "art")
	if err != nil {
		t.Fatal(err)
	}
	if agent.DutiesFrom(rec.Fields) != "secretaries/art" || read() != want {
		t.Fatalf("同步基本職位之後 art 不變：duties_from=%q", agent.DutiesFrom(rec.Fields))
	}
}
