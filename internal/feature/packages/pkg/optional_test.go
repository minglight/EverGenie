package pkg

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 選用職位（樣板標頭 optional: true，DEV-157）：不自動帶出來——沒指定職位的 Sync、ClaimNotify 都跳過它，
// 主人指名（--agent、設定頁那張卡）才加。看板那一邊在 web/apps/board 的 functions.spec、ClaimFunctions.spec。

// crewPkg 寫一個帶一般職位 guide、選用職位 painter（連同 painter 的排程）的套件。
func crewPkg(h *harness, painterHeader string) {
	h.write(sdk.TierMine, "crew", dataPkg("crew", "1.0.0", `
[[contributes.agent]]
id = "guide"
template = "agents/guide/agent.md"
[[contributes.agent]]
id = "painter"
template = "agents/painter/agent.md"
[[contributes.job]]
agent = "painter"
id = "sketch"
path = "jobs/sketch.md"
`), map[string]string{
		"agents/guide/agent.md":   "---\nduty: 嚮導\n---\n帶路\n",
		"agents/painter/agent.md": "---\nduty: 畫畫\n" + painterHeader + "---\n畫圖\n",
		"jobs/sketch.md":          weighJob,
	})
}

// Guards: PKG-12
func TestOptionalTemplateHeader(t *testing.T) {
	h := newHarness(t)
	crewPkg(h, "optional: true\n")
	h.start()
	var got []sdk.AgentDecl
	for _, p := range h.list() {
		if p.ID == "crew" {
			got = p.Agents
		}
	}
	if len(got) != 2 || got[0].ID != "guide" || got[0].Optional || got[1].ID != "painter" || !got[1].Optional {
		t.Fatalf("package.list 的 agents 要帶 optional：%+v", got)
	}

	dir := t.TempDir()
	write := func(header string) error {
		p := filepath.Join(dir, "agent.md")
		if err := os.WriteFile(p, []byte("---\nduty: 畫畫\n"+header+"---\n畫圖\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := parseAgentTemplate(p, "painter", "agents/painter/agent.md")
		return err
	}
	if err := write("optional: false\n"); err != nil {
		t.Fatalf("optional: false 也合格：%v", err)
	}
	if err := write("optional: \"yes\"\n"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "optional") {
		t.Fatalf("optional 不是 true／false 要擋：%v", err)
	}
}

// 沒指定職位的同步略過選用職位與它的排程（預演列在 optional）；指名才建。
// Guards: PKG-12
func TestSyncSkipsOptionalUnlessNamed(t *testing.T) {
	h := newHarness(t)
	crewPkg(h, "optional: true\n")
	host := &fakeSync{persona: "builder", duties: map[string]string{}, jobs: map[string][]string{}}
	h.m.SetSyncHost(host)
	h.start()
	ctx := context.Background()
	owner := &sdk.Actor{Name: "owner(cli)"}

	all, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Agents) != 1 || all.Agents[0].ID != "guide" || len(all.Jobs) != 0 || strings.Join(all.Optional, ",") != "painter" {
		t.Fatalf("整包同步：只建 guide、painter 列在 optional：%+v", all)
	}
	if _, ok := host.duties["painter"]; ok {
		t.Fatalf("選用職位不該被建：%v", host.calls)
	}

	named, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Agent: "painter", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(named.Agents) != 1 || named.Agents[0].ID != "painter" || named.Agents[0].Action != "create" || len(named.Jobs) != 1 || len(named.Optional) != 0 {
		t.Fatalf("指名就建（連同排程）：%+v", named)
	}
	if _, ok := host.duties["painter"]; !ok {
		t.Fatalf("指名後要建好：%v", host.calls)
	}

	// 已經在名冊上的選用職位照一般職位走：整包同步會比對職責、補上它的排程，不再說「沒有一起加」。
	h2 := newHarness(t)
	crewPkg(h2, "optional: true\n")
	had := &fakeSync{persona: "builder", duties: map[string]string{"guide": "帶路", "painter": "主人改過的"}, jobs: map[string][]string{}}
	h2.m.SetSyncHost(had)
	h2.start()
	again, err := h2.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Apply: true, By: owner})
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Agents) != 2 || again.Agents[1].ID != "painter" || again.Agents[1].Action != "update" || len(again.Jobs) != 1 || again.Jobs[0].Action != "create" || len(again.Optional) != 0 {
		t.Fatalf("名冊上已經有的選用職位照常同步：%+v", again)
	}

	// 封存過的選用職位也算「還沒有」：整包同步略過、不回衝突；指名才回衝突。
	h3 := newHarness(t)
	crewPkg(h3, "optional: true\n")
	gone := &fakeSync{persona: "builder", duties: map[string]string{}, jobs: map[string][]string{}, archived: map[string]bool{"painter": true}}
	h3.m.SetSyncHost(gone)
	h3.start()
	if r, err := h3.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", By: owner}); err != nil || strings.Join(r.Optional, ",") != "painter" {
		t.Fatalf("封存過的選用職位整包同步略過：%+v %v", r, err)
	}
	if _, err := h3.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Agent: "painter", By: owner}); platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("指名封存過的職位回衝突：%v", err)
	}
}

// 沒指定職位的認領通知不挑選用職位；只剩選用的就不問；指名照樣發。
// Guards: PKG-40
func TestClaimNotifySkipsOptional(t *testing.T) {
	h, _, fc := claimEnv(t)
	ctx := context.Background()
	crewPkg(h, "optional: true\n")
	if _, err := h.m.Reload(ctx, &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	by := &sdk.Actor{Name: "code"}

	r, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "crew", Persona: "garden.fairy", By: by})
	if err != nil || r.Agent != "guide" || !r.Sent {
		t.Fatalf("第一個要問的是 guide：%+v %v", r, err)
	}
	// guide 問過了：再請一次回 guide 的狀態，不會改問 painter。
	again, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "crew", By: by})
	if err != nil || again.Agent != "guide" || again.Sent {
		t.Fatalf("問過的回原狀態、不挑選用的：%+v %v", again, err)
	}
	// guide 有人負責了：只剩選用的 painter，不問。
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "crew", Agent: "guide", Persona: "garden.fairy", Apply: true, By: &sdk.Actor{Name: "owner(cli)"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "crew", By: by}); platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "painter") {
		t.Fatalf("只剩選用職位要回 CONFLICT、說是哪個選用職位沒問：%v", err)
	}
	for _, n := range fc.sent {
		if n.Claim == "painter" {
			t.Fatalf("沒指名就不該問選用職位：%+v", n)
		}
	}
	// 指名就照樣發。
	named, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "crew", Agent: "painter", Persona: "garden.fairy", By: by})
	if err != nil || named.Agent != "painter" || !named.Sent {
		t.Fatalf("指名選用職位照樣發：%+v %v", named, err)
	}
}

// 只帶選用職位的套件：沒指名就不問，說清楚原因。
// Guards: PKG-40
func TestClaimNotifyOnlyOptional(t *testing.T) {
	h, _, _ := claimEnv(t)
	ctx := context.Background()
	h.write(sdk.TierMine, "atelier", dataPkg("atelier", "1.0.0", "[[contributes.agent]]\nid = \"painter\"\ntemplate = \"agents/painter/agent.md\"\n"),
		map[string]string{"agents/painter/agent.md": "---\nduty: 畫畫\noptional: true\n---\n畫圖\n"})
	if _, err := h.m.Reload(ctx, &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	_, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "atelier", By: &sdk.Actor{Name: "code"}})
	if platform.CodeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "選用") {
		t.Fatalf("只有選用職位：%v", err)
	}
}
