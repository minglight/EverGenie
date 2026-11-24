package pkg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestTierPrecedence：同一個 id 自己的優先（fork），分享的不能蓋掉官方（分享的被拒、官方照常）。
// Guards: REQ-170
func TestTierPrecedence(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", ""), nil)
	h.write(sdk.TierInstalled, "notes", dataPkg("notes", "1.5.0", ""), nil)
	h.write(sdk.TierMine, "notes", dataPkg("notes", "2.0.0", ""), nil)
	h.write(sdk.TierOfficial, "diary", dataPkg("diary", "1.0.0", ""), nil)
	h.write(sdk.TierInstalled, "diary", dataPkg("diary", "9.0.0", ""), nil)
	h.start()
	if p := h.get("notes"); p.Tier != sdk.TierMine || p.Version != "2.0.0" || p.State != sdk.PackageStateRunning {
		t.Fatalf("notes 要用自己的：%+v", p)
	}
	if p := h.get("diary"); p.Tier != sdk.TierOfficial || p.State != sdk.PackageStateRunning {
		t.Fatalf("diary 要用官方的：%+v", p)
	}
	var rejected []sdk.Package
	for _, p := range h.list() {
		if p.ID == "diary" && p.Tier == sdk.TierInstalled {
			rejected = append(rejected, p)
		}
	}
	if len(rejected) != 1 || rejected[0].State != sdk.PackageStateRejected || !strings.Contains(rejected[0].Error, "不能蓋掉") {
		t.Fatalf("分享的 diary 要被拒並說明：%+v", rejected)
	}
}

// TestBadPackageIsolated：壞掉的 manifest 只讓那一包不載入（白話原因），別的照常、Start 不失敗。
// Guards: REQ-170
func TestBadPackageIsolated(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "broken", "schema = = 1\n", nil)
	h.write(sdk.TierMine, "future", strings.Replace(dataPkg("future", "1.0.0", ""), "evergenie.v1", "evergenie.v9", 1), nil)
	h.write(sdk.TierMine, "good", dataPkg("good", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "nobuild", programPkg("nobuild", "", ""), programFiles("nobuild"))
	h.write(sdk.TierMine, "runs", programPkg("runs", "", ""), programFiles("runs"))
	h.buildErr[h.tierRoot(sdk.TierMine)+"/nobuild"] = "./main.go:3:1: syntax error"
	h.start()
	if p := h.get("broken"); p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, "格式不對") {
		t.Fatalf("broken：%+v", p)
	}
	if p := h.get("future"); p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, "不認得協定版本") {
		t.Fatalf("future：%+v", p)
	}
	if p := h.get("nobuild"); p.State != sdk.PackageStateFailed || !strings.Contains(p.Error, goMsgBuildBroken) || !strings.Contains(p.Error, "syntax error") {
		t.Fatalf("nobuild：%+v", p) // 白話＋原文（給秘書看）
	}
	notified := false
	for _, e := range h.bus.events {
		if e["topic"] == "system.alert" && strings.Contains(fmt.Sprint(e["title"]), "nobuild") {
			notified = true
			if c := fmt.Sprint(e["context"]); !strings.Contains(c, goMsgBuildBroken) || strings.Contains(c, "syntax") {
				t.Fatalf("給主人的通知只放白話、不夾編譯原文：%v", e)
			}
		}
	}
	if !notified {
		t.Fatal("編不過要通知")
	}
	if p := h.get("good"); p.State != sdk.PackageStateRunning {
		t.Fatalf("good 要照常：%+v", p)
	}
	if len(h.procs["runs"]) != 1 || len(h.procs["nobuild"]) != 0 {
		t.Fatalf("runs 要照常啟動：%v", h.procs)
	}
}

// TestMissingDependencyPlainError：相依缺少、版本不符、被停用、成環 → 只有那幾包被拒，原因是白話；依賴它的一路被拒。
// Guards: REQ-170
func TestMissingDependencyPlainError(t *testing.T) {
	h := newHarness(t)
	req := func(id, v string) string { return "[[requires]]\nid = \"" + id + "\"\nversion = \"" + v + "\"\n" }
	h.write(sdk.TierMine, "base", dataPkg("base", "1.4.0", ""), nil)
	h.write(sdk.TierMine, "needs-ghost", dataPkg("needs-ghost", "1.0.0", req("ghost", "")), nil)
	h.write(sdk.TierMine, "on-top", dataPkg("on-top", "1.0.0", req("needs-ghost", "")), nil)
	h.write(sdk.TierMine, "too-new", dataPkg("too-new", "1.0.0", req("base", "2.0.0")), nil)
	h.write(sdk.TierMine, "fine", dataPkg("fine", "1.0.0", req("base", "^1.2.0")), nil)
	h.write(sdk.TierMine, "loop-a", dataPkg("loop-a", "1.0.0", req("loop-b", "")), nil)
	h.write(sdk.TierMine, "loop-b", dataPkg("loop-b", "1.0.0", req("loop-a", "")), nil)
	h.start()
	for id, want := range map[string]string{
		"needs-ghost": "需要套件「ghost」",
		"on-top":      "需要套件「needs-ghost」",
		"too-new":     "需要套件「base」2.0.0，目前裝的是 1.4.0",
		"loop-a":      "相依成環",
		"loop-b":      "相依成環",
	} {
		if p := h.get(id); p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, want) {
			t.Errorf("%s：要被拒並寫「%s」，實際 %+v", id, want, p)
		}
	}
	if p := h.get("fine"); p.State != sdk.PackageStateRunning {
		t.Fatalf("fine 要照常：%+v", p)
	}
	if _, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "base", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if p := h.get("fine"); p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, "已停用") {
		t.Fatalf("base 停用後 fine 要被拒：%+v", p)
	}
}

// TestOptionalPackageDefaultsDisabled：manifest 寫 optional = true 的套件，沒有設定記錄時預設停用（不啟用、資料種類不註冊）；
// 依賴它的套件照現有規則被拒；一般套件不受影響。
// Guards: REQ-176
func TestOptionalPackageDefaultsDisabled(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "base", dataPkg("base", "1.0.0", ""), nil)
	h.write(sdk.TierOfficial, "extra", dataPkg("extra", "1.0.0", "optional = true\n[[requires]]\nid = \"base\"\nversion = \"\"\n"), nil)
	h.write(sdk.TierOfficial, "wants-extra", dataPkg("wants-extra", "1.0.0", "[[requires]]\nid = \"extra\"\nversion = \"\"\n"), nil)
	h.start()
	if p := h.get("extra"); p.Enabled || !p.Optional || p.State != sdk.PackageStateStopped {
		t.Fatalf("選用套件預設要停用：%+v", p)
	}
	if p := h.get("base"); !p.Enabled || p.Optional || p.State != sdk.PackageStateRunning {
		t.Fatalf("一般套件照舊啟用：%+v", p)
	}
	if p := h.get("wants-extra"); p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, "已停用") {
		t.Fatalf("依賴選用套件的套件要被拒：%+v", p)
	}
}

// TestSetEnabledOptionalWritesExplicit：選用套件開啟時也要在 settings/packages.toml 寫明確的 true（不能用「刪掉＝預設啟用」）；
// 一般套件開啟仍是刪掉記錄。
// Guards: REQ-176
func TestSetEnabledOptionalWritesExplicit(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "base", dataPkg("base", "1.0.0", ""), nil)
	h.write(sdk.TierOfficial, "extra", dataPkg("extra", "1.0.0", "optional = true\n"), nil)
	h.start()
	ctx := t.Context()
	r, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "extra", Enabled: true})
	if err != nil || !r.Package.Enabled || r.Package.State != sdk.PackageStateRunning {
		t.Fatalf("開啟選用套件：%+v %v", r, err)
	}
	st, err := h.m.loadSettings()
	if v, ok := st.Enabled["extra"]; err != nil || !ok || !v {
		t.Fatalf("要寫明確的 extra = true：%+v %v", st.Enabled, err)
	}
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "extra", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if st, _ = h.m.loadSettings(); st.Enabled["extra"] {
		t.Fatalf("關掉要寫 false：%+v", st.Enabled)
	}
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "base", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "base", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if st, _ = h.m.loadSettings(); len(st.Enabled) != 1 {
		t.Fatalf("一般套件開啟要刪掉記錄，只剩 extra：%+v", st.Enabled)
	}
}

// TestBackupDirSkipped：放在套件旁的備份資料夾（<id>.bak-<日期>-<原因>）不當成套件，不會變成載入失敗擋住升級檢查。
func TestBackupDirSkipped(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierInstalled, "demo-points", dataPkg("demo-points", "0.7.0", ""), nil)
	h.write(sdk.TierInstalled, "demo-points.bak-20261108-0.6.0", dataPkg("demo-points", "0.6.0", ""), nil)
	h.start()
	for _, p := range h.list() {
		if strings.Contains(p.ID, ".bak-") || p.State == sdk.PackageStateRejected {
			t.Fatalf("備份資料夾不該出現在套件清單：%+v", p)
		}
	}
	if p := h.get("demo-points"); p.Version != "0.7.0" || p.State != sdk.PackageStateRunning {
		t.Fatalf("demo-points 要用正本：%+v", p)
	}
}

// TestOwnerFoldersInPackageDirsIgnored：主人的套件資料夾與別人的套件資料夾裡，沒有 package.toml 的子資料夾是主人自己放的東西，
// 不當成套件、不列成載入失敗（DEV-154 第 9 項）；有 package.toml 但寫壞的照舊是壞掉的套件；官方資料夾少了 package.toml 照舊報。
func TestOwnerFoldersInPackageDirsIgnored(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierMine, "garden", dataPkg("garden", "0.1.0", ""), nil)
	for _, tier := range []sdk.Tier{sdk.TierMine, sdk.TierInstalled} {
		dir := filepath.Join(h.tierRoot(tier), "我的筆記")
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "readme.md"), []byte("不是套件"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.write(sdk.TierMine, "broken", "this is not toml [", nil)
	if err := os.MkdirAll(filepath.Join(h.tierRoot(sdk.TierOfficial), "hollow"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.start()
	got := map[string]sdk.PackageState{}
	for _, p := range h.list() {
		got[p.ID] = p.State
	}
	if _, ok := got["我的筆記"]; ok {
		t.Fatalf("主人自己的資料夾不該出現在套件清單：%v", got)
	}
	if got["garden"] != sdk.PackageStateRunning {
		t.Fatalf("garden 要照常：%v", got)
	}
	if got["broken"] != sdk.PackageStateRejected || got["hollow"] != sdk.PackageStateRejected {
		t.Fatalf("寫壞的套件與官方的空資料夾要照舊報：%v", got)
	}
}
