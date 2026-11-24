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
	"github.com/minglight/evergenie/sdk"
)

// fakeClaim 把認領流程接到真的名冊（角色庫、啟用），通知記在記憶體。
type fakeClaim struct {
	roster  *agent.Module
	lib     stubLib
	sent    []*sdk.Notification
	enabled []string
}

func (f *fakeClaim) Personas(ctx context.Context) ([]sdk.PersonaCard, error) {
	return f.lib.Library(ctx)
}
func (f *fakeClaim) Notify(_ context.Context, n *sdk.Notification) (*sdk.Notification, error) {
	cp := *n
	cp.ID = fmt.Sprintf("M-%d", len(f.sent)+1)
	f.sent = append(f.sent, &cp)
	return &cp, nil
}
func (f *fakeClaim) EnableAgent(ctx context.Context, id, by string) error {
	f.enabled = append(f.enabled, id+" by "+by)
	return setAgentField(ctx, f.roster, id, "enabled", "true", by)
}

// claimEnv：s3Env＋套件內角色 garden.fairy（上線）＋接好的 Sync 與認領主機。
func claimEnv(t *testing.T) (*harness, *agent.Module, *fakeClaim) {
	t.Helper()
	h, roster := s3Env(t)
	h.m.SetSyncHost(rosterHost{roster})
	fc := &fakeClaim{roster: roster, lib: stubLib{h.m, h.home.Dir, roster}}
	h.m.SetClaimHost(fc)
	addCard(t, h.m, "garden", "fairy", "花仙子")
	fc.lib.setStatus(t, "garden.fairy", personaref.StatusLive)
	return h, roster, fc
}

func answer(id, text, by string) map[string]any {
	return map[string]any{"id": id, "text": text, "via": "button", "source": sdk.NotifySourcePackage, "by": by}
}

func agentRow(t *testing.T, roster *agent.Module, id string) (agent.Agent, bool) {
	t.Helper()
	rows, err := roster.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range rows {
		if a.ID == id {
			return a, true
		}
	}
	return nil, false
}

// 秘書身分 apply 回 FORBIDDEN、主人身分成功；秘書能請核心發認領通知，但通知本身不建秘書（PKG-12 不放寬）。
// Guards: REQ-341
func TestSyncApplyRequiresOwner(t *testing.T) {
	h, roster, fc := claimEnv(t)
	ctx := context.Background()
	for _, who := range []string{"garden", "code", "", "system", "owner"} {
		_, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Persona: "garden.fairy", Apply: true, By: &sdk.Actor{Name: who}})
		if platform.CodeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%q 套用要 FORBIDDEN：%v", who, err)
		}
	}
	r, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil || !r.Sent || r.Notice != "M-1" {
		t.Fatalf("秘書請發認領通知：%+v %v", r, err)
	}
	if _, ok := agentRow(t, roster, "garden"); ok {
		t.Fatal("發通知不能建秘書")
	}
	if len(fc.enabled) != 0 {
		t.Fatal("發通知不能啟用任何秘書")
	}
	if _, err := h.m.Sync(ctx, &sdk.PackageSyncRequest{ID: "garden", Persona: "garden.fairy", Apply: true, By: &sdk.Actor{Name: s3Owner}}); err != nil {
		t.Fatalf("主人套用：%v", err)
	}
	if a, ok := agentRow(t, roster, "garden"); !ok || a.Fields["persona"] != "garden.fairy" {
		t.Fatalf("主人套用後要有 garden：%+v", a)
	}
}

// 建議角色的順序：套件自己帶的（沒坐位置的優先）→ 還沒坐位置的上線角色 → 其他上線角色；設計中、被蓋住、不能指派的不算。
// Guards: REQ-342
func TestClaimSuggestPackagePersona(t *testing.T) {
	live := func(slug, src, pkg string, seats ...string) sdk.PersonaCard {
		return sdk.PersonaCard{Slug: slug, Source: src, Package: pkg, Status: "live", Seats: seats}
	}
	cards := []sdk.PersonaCard{
		live("athena", "official", "", "code"),
		live("mina", "mine", ""),
		live("garden.fairy", "package", "garden", "x"),
		live("garden.elf", "package", "garden"),
		live("park.fairy", "package", "park"),
		{Slug: "garden.draft", Source: "package", Package: "garden", Status: "design"},
	}
	cases := []struct {
		name  string
		cards []sdk.PersonaCard
		pkg   string
		want  string
		why   string
	}{
		{"自己帶的、沒坐位置的優先", cards, "garden", "garden.elf", "自己帶的"},
		{"自己帶的都坐了位置也用自己帶的", []sdk.PersonaCard{cards[1], cards[2]}, "garden", "garden.fairy", "自己帶的"},
		{"沒有自己帶的：還沒坐位置的上線角色", cards, "nothing", "mina", "還沒有負責"},
		{"別的職位的角色也算沒坐位置的", []sdk.PersonaCard{cards[0], cards[4]}, "garden", "park.fairy", "還沒有負責"},
		{"都坐了位置：第一位上線角色", []sdk.PersonaCard{cards[0], cards[2]}, "nothing", "athena", "也在別的職位"},
		{"設計中、被蓋住、不能指派的不算", []sdk.PersonaCard{cards[5], {Slug: "s", Status: "live", Shadowed: true}, {Slug: "u", Status: "live", Unassignable: "停用"}}, "garden", "", ""},
	}
	for _, c := range cases {
		got, why := SuggestPersona(c.cards, c.pkg)
		if got != c.want || !strings.Contains(why, c.why) {
			t.Errorf("%s：%q（%s），要 %q", c.name, got, why, c.want)
		}
	}
}

// 認領通知：approve、三個選項、source／claim 由 pkg 標、建議套件自己帶的角色；同一個職位再請不會發第二則。
// Guards: REQ-341, REQ-343, REQ-345
func TestClaimNotifySendsOnce(t *testing.T) {
	h, _, fc := claimEnv(t)
	ctx := context.Background()
	r, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Agent != "garden" || r.Persona != "garden.fairy" || r.PersonaName != "花仙子" || !strings.Contains(r.Reason, "自己帶的") || r.State != sdk.ClaimStateAsked {
		t.Fatalf("回應：%+v", r)
	}
	n := fc.sent[0]
	if n.Category != sdk.CategoryApprove || n.Source != sdk.NotifySourcePackage || n.Claim != "garden" || n.Agent != "code" || n.By != "code" {
		t.Fatalf("通知：%+v", n)
	}
	if strings.Join(n.Options, "|") != "好，用 花仙子|換一個|之後再說" || n.Todo == "" || !strings.Contains(n.Title, "照顧花園") {
		t.Fatalf("選項與文字：%q %q %q", n.Options, n.Title, n.Todo)
	}
	again, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", Agent: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil || again.Sent || again.Notice != "M-1" || again.State != sdk.ClaimStateAsked || again.Detail == "" {
		t.Fatalf("還沒回又請：%+v %v", again, err)
	}
	if len(fc.sent) != 1 {
		t.Fatalf("不能發第二則：%d", len(fc.sent))
	}
	// 狀態記在 settings/packages.toml。
	if s := readFile(t, filepath.Join(h.home.Dir, "settings", "packages.toml")); !strings.Contains(s, "[claims.garden]") || !strings.Contains(s, `notice = "M-1"`) {
		t.Fatalf("settings：\n%s", s)
	}
	// 指定的角色要能指派；沒有能指派的角色就說清楚、不發。
	if _, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", Agent: "nope", By: &sdk.Actor{Name: "code"}}); platform.CodeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有這個職位：%v", err)
	}
	if _, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden"}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("沒有操作者：%v", err)
	}
}

// 主人按「好」：核心以主人身分 Sync＋啟用，角色是通知裡記下的那位；秘書自己回答、自動回答都不動作。
// Guards: REQ-344
func TestClaimOwnerYesApplies(t *testing.T) {
	h, roster, fc := claimEnv(t)
	ctx := context.Background()
	r, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []map[string]any{
		answer(r.Notice, "好，用 花仙子", "code"),                                                                  // 秘書自己按
		answer(r.Notice, "好，用 花仙子", ""),                                                                      // 來路不明
		{"id": r.Notice, "text": "好", "via": "auto", "source": sdk.NotifySourcePackage, "by": "owner(web)"},  // 自動回答
		{"id": r.Notice, "text": "好", "via": "button", "source": sdk.NotifySourcePolicy, "by": "owner(web)"}, // 別人的通知
	} {
		if err := h.m.handleClaimAnswer(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok := agentRow(t, roster, "garden"); ok || len(fc.enabled) != 0 {
		t.Fatal("不是主人的回答不能建秘書")
	}
	// 主人打字回「好」也算；角色以通知記下的為準（回答的文字不能換角色）。
	if err := h.m.handleClaimAnswer(ctx, answer(r.Notice, "好，用 athena", "owner(web)")); err != nil {
		t.Fatal(err)
	}
	a, ok := agentRow(t, roster, "garden")
	if !ok || a.Fields["persona"] != "garden.fairy" || a.Fields["enabled"] != true {
		t.Fatalf("garden 秘書：%+v", a.Fields)
	}
	if len(fc.enabled) != 1 || fc.enabled[0] != "garden by owner(notice)" {
		t.Fatalf("啟用：%v", fc.enabled)
	}
	again, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", Agent: "garden", By: &sdk.Actor{Name: "code"}})
	if platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("已經有人負責再請要 CONFLICT：%+v %v", again, err)
	}
}

// 「之後再說」記下來、同一個職位不再發；「換一個」也不再發；送出時再驗，角色不能用了就不動並發系統提醒。
// Guards: REQ-345, REQ-344
func TestClaimLaterAndRevalidate(t *testing.T) {
	h, roster, fc := claimEnv(t)
	ctx := context.Background()
	// 第二個職位 park（自己的另一個職位），用來測「換一個」。
	h.write(sdk.TierMine, "park", dataPkg("park", "1.0.0", "[[contributes.agent]]\nid = \"park\"\ntemplate = \"agents/park/agent.md\"\n"),
		map[string]string{"agents/park/agent.md": "---\nduty: 公園巡邏\n---\n你巡公園。\n"})
	if _, err := h.m.Reload(ctx, &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	r, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.handleClaimAnswer(ctx, answer(r.Notice, "之後再說", "owner(web)")); err != nil {
		t.Fatal(err)
	}
	later, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil || later.Sent || later.State != sdk.ClaimStateLater || !strings.Contains(later.Detail, "之後再說") {
		t.Fatalf("之後再說之後：%+v %v", later, err)
	}
	if len(fc.sent) != 1 {
		t.Fatalf("之後再說之後不能再發：%d", len(fc.sent))
	}
	if _, ok := agentRow(t, roster, "garden"); ok {
		t.Fatal("之後再說不建秘書")
	}
	// 主人之後在「好」也沒用了（那則已經不是等回答的狀態）。
	if err := h.m.handleClaimAnswer(ctx, answer(r.Notice, "好，用 花仙子", "owner(web)")); err != nil {
		t.Fatal(err)
	}
	if _, ok := agentRow(t, roster, "garden"); ok {
		t.Fatal("已經說之後再說的那則不能再套用")
	}

	pr, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "park", By: &sdk.Actor{Name: "code"}})
	if err != nil || !pr.Sent || pr.Persona != "garden.fairy" {
		t.Fatalf("park：%+v %v", pr, err)
	}
	// 送出前角色被移回設計中：不動、記失敗、發系統提醒。
	fc.lib.setStatus(t, "garden.fairy", personaref.StatusDesign)
	if err := h.m.handleClaimAnswer(ctx, answer(pr.Notice, "好，用 花仙子", "owner(web)")); err != nil {
		t.Fatal(err)
	}
	if _, ok := agentRow(t, roster, "park"); ok {
		t.Fatal("角色不能用了不能建秘書")
	}
	if alerts := h.bus.alerts(); len(alerts) == 0 || !strings.Contains(alerts[len(alerts)-1], "park") {
		t.Fatalf("要發系統提醒：%v", alerts)
	}
	failed, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "park", By: &sdk.Actor{Name: "code"}})
	if err != nil || failed.Sent || failed.State != sdk.ClaimStateFailed {
		t.Fatalf("失敗後不再問：%+v %v", failed, err)
	}
	data, _ := os.ReadFile(filepath.Join(h.home.Dir, "settings", "packages.toml"))
	if !strings.Contains(string(data), `state = "later"`) || !strings.Contains(string(data), `state = "failed"`) {
		t.Fatalf("settings：\n%s", data)
	}
}

// 「換一個」：記下、不再發；套件沒帶多個職位時不帶 agent 也回那個職位的狀態。
// Guards: REQ-345
func TestClaimChangeNoSecondNotice(t *testing.T) {
	h, _, fc := claimEnv(t)
	ctx := context.Background()
	r, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.handleClaimAnswer(ctx, answer(r.Notice, "換一個", "owner(web)")); err != nil {
		t.Fatal(err)
	}
	again, err := h.m.ClaimNotify(ctx, &sdk.PackageClaimNotifyRequest{ID: "garden", By: &sdk.Actor{Name: "code"}})
	if err != nil || again.Sent || again.State != sdk.ClaimStateChange || len(fc.sent) != 1 {
		t.Fatalf("換一個之後：%+v %v %d", again, err, len(fc.sent))
	}
}
