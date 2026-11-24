package pkg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// TestInstallFromFolder：從本機資料夾安裝到 packages/installed/；同名、跟官方同名、相對路徑、裝到官方層都擋；
// 解除安裝只移除資料夾，vault/pkg/<id>/ 的資料保留；官方與自己的套件不能從這裡解除安裝。
// Guards: REQ-93, REQ-172
func TestInstallFromFolder(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "diary", dataPkg("diary", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "mine-only", dataPkg("mine-only", "1.0.0", ""), nil)
	h.start()
	src := filepath.Join(t.TempDir(), "download-1")
	if err := os.MkdirAll(filepath.Join(src, "cmd", "habit-tracker"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{"package.toml": programPkg("habit-tracker", "", ""), "cmd/habit-tracker/main.go": "package main\n", "README.md": "說明\n"} {
		if err := os.WriteFile(filepath.Join(src, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	by := &sdk.Actor{Name: "owner(web)"}
	r, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: src, By: by})
	if err != nil {
		t.Fatal(err)
	}
	h.drain()
	dest := filepath.Join(h.home.Dir, "packages", "installed", "habit-tracker")
	if r.Package.Tier != sdk.TierInstalled || !isDir(dest) || len(h.procs["habit-tracker"]) != 1 {
		t.Fatalf("安裝：%+v dest=%v procs=%d", r.Package, isDir(dest), len(h.procs["habit-tracker"]))
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "README.md")); string(b) != "說明\n" {
		t.Fatal("要整包複製")
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: src, By: by}); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("同名要 CONFLICT：%v", err)
	}
	off := filepath.Join(t.TempDir(), "diary")
	if err := os.MkdirAll(off, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(off, "package.toml"), []byte(dataPkg("diary", "2.0.0", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: off, By: by}); codeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("跟官方同名要 CONFLICT：%v", err)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: "download-1", By: by}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("相對路徑要 INVALID：%v", err)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: src, Tier: sdk.TierOfficial, By: by}); codeOf(err) != sdk.ErrorCodeForbidden {
		t.Fatalf("裝到官方層要 FORBIDDEN：%v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(h.home.Dir, "packages", "installed")); len(entries) != 1 {
		t.Fatalf("失敗的安裝不能留下暫存資料夾：%v", entries)
	}

	data := filepath.Join(h.home.Dir, "vault", "pkg", "habit-tracker")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "H-1.md"), []byte("---\nschema: 1\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Uninstall(t.Context(), &sdk.PackageUninstallRequest{ID: "habit-tracker", By: by}); err != nil {
		t.Fatal(err)
	}
	if isDir(dest) || !exists(filepath.Join(data, "H-1.md")) || !h.lastProc("habit-tracker").killed {
		t.Fatalf("解除安裝：資料夾要移走、行程收掉、資料保留")
	}
	for _, id := range []string{"diary", "mine-only"} {
		if _, err := h.m.Uninstall(t.Context(), &sdk.PackageUninstallRequest{ID: id, By: by}); codeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%s 不能從這裡解除安裝：%v", id, err)
		}
	}
	if !isDir(filepath.Join(h.official, "diary")) || !isDir(filepath.Join(h.home.Dir, "packages", "mine", "mine-only")) {
		t.Fatal("官方與自己的資料夾不能被動到")
	}
}

// TestExtensionOwnerOnly：宣告 owner_only 的擴充點只有主人本人能改（秘書、系統、沒署名都 FORBIDDEN），讀照常、也看得到宣告；
// 沒宣告的照舊誰都能改。
func TestExtensionOwnerOnly(t *testing.T) {
	h := newHarness(t)
	ext := "[[extension]]\nid = \"secret\"\nkind = \"setting\"\ndefault = \"off\"\nowner_only = true\n" +
		"[[extension]]\nid = \"open\"\nkind = \"setting\"\n"
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", ext), nil)
	h.start()
	set := func(id, by string) error {
		var a *sdk.Actor
		if by != "" {
			a = &sdk.Actor{Name: by}
		}
		_, err := h.m.SetExtension(t.Context(), &sdk.PackageSetExtensionRequest{Package: "notes", Extension: id, Value: map[string]any{"value": "on"}, By: a})
		return err
	}
	for _, by := range []string{"code", "system", "notes", ""} {
		if err := set("secret", by); codeOf(err) != sdk.ErrorCodeForbidden {
			t.Fatalf("%q 改 owner_only 要 FORBIDDEN：%v", by, err)
		}
	}
	if err := set("secret", "owner(web)"); err != nil {
		t.Fatalf("主人照常能改：%v", err)
	}
	if err := set("open", "code"); err != nil {
		t.Fatalf("沒宣告的照舊誰都能改：%v", err)
	}
	l, err := h.m.ListExtensions(t.Context(), &sdk.PackageListExtensionsRequest{Package: "notes"})
	if err != nil || l.Values["notes.secret"] != "on" {
		t.Fatalf("讀照常：%+v %v", l, err)
	}
	for _, p := range l.Extensions {
		if (p.ID == "secret") != p.OwnerOnly {
			t.Fatalf("宣告要帶出來：%+v", p)
		}
	}
}

// TestExtensionRemovedWarnsNotBreaks：設定引用的擴充點被套件拿掉了 → 值保留、列出警告、套件照常載入。
// Guards: REQ-94
func TestExtensionRemovedWarnsNotBreaks(t *testing.T) {
	h := newHarness(t)
	ext := "[[extension]]\nid = \"greeting\"\nkind = \"prompt_append\"\ndescription = \"開場多說一句\"\ndefault = \"早安\"\n"
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", ext), nil)
	h.start()
	set := func(v map[string]any) error {
		_, err := h.m.SetExtension(t.Context(), &sdk.PackageSetExtensionRequest{Package: "notes", Extension: "greeting", Value: v, By: &sdk.Actor{Name: "owner(web)"}})
		return err
	}
	if err := set(map[string]any{"value": "記得喝水"}); err != nil {
		t.Fatal(err)
	}
	if err := set(map[string]any{"value": 3}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("值不是字串要 INVALID：%v", err)
	}
	if _, err := h.m.SetExtension(t.Context(), &sdk.PackageSetExtensionRequest{Package: "notes", Extension: "nope", Value: map[string]any{"value": "x"}}); codeOf(err) != sdk.ErrorCodeNotFound {
		t.Fatalf("沒有的擴充點要 NOT_FOUND：%v", err)
	}
	l, err := h.m.ListExtensions(t.Context(), &sdk.PackageListExtensionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Extensions) != 1 || l.Extensions[0].Package != "notes" || l.Extensions[0].DefaultValue != "早安" || l.Values["notes.greeting"] != "記得喝水" || len(l.Warnings) != 0 {
		t.Fatalf("ListExtensions = %+v", l)
	}
	// 官方新版拿掉了這個擴充點。
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.1.0", ""), nil)
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if p := h.get("notes"); p.State != sdk.PackageStateRunning || p.Version != "1.1.0" {
		t.Fatalf("擴充點拿掉不能讓套件壞掉：%+v", p)
	}
	l, err = h.m.ListExtensions(t.Context(), &sdk.PackageListExtensionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Extensions) != 0 || l.Values["notes.greeting"] != "記得喝水" || len(l.Warnings) != 1 || !strings.Contains(l.Warnings[0], "notes.greeting") {
		t.Fatalf("要保留值並警告：%+v", l)
	}
	if w := h.m.Warnings(); len(w) != 1 {
		t.Fatalf("Warnings() = %v", w)
	}
	b, _ := os.ReadFile(filepath.Join(h.home.Dir, "settings", "packages.toml"))
	if !strings.Contains(string(b), "記得喝水") {
		t.Fatalf("設定檔的值要保留：%s", b)
	}
}

// TestPromptAppends：prompt_append 擴充點的值會變成要接在秘書指令後面的補充（DEV-154）——主人設了用設的、沒設用預設、
// 空的不列；宣告了 agent 的只給那個職位；套件停用就不帶（值留著）；page_slot 沒有值可以設；太長的拒絕。
func TestPromptAppends(t *testing.T) {
	h := newHarness(t)
	ext := "[[extension]]\nid = \"greeting\"\nkind = \"prompt_append\"\ndescription = \"開場多說一句\"\ndefault = \"早安\"\n" +
		"[[extension]]\nid = \"coach\"\nkind = \"prompt_append\"\nagent = \"health\"\n" +
		"[[extension]]\nid = \"top\"\nkind = \"page_slot\"\n" +
		"[[extension]]\nid = \"unit\"\nkind = \"setting\"\ndefault = \"kg\"\n"
	h.write(sdk.TierOfficial, "notes", dataPkg("notes", "1.0.0", ext), nil)
	h.start()
	by := &sdk.Actor{Name: "owner(web)"}
	set := func(id string, v map[string]any) error {
		_, err := h.m.SetExtension(t.Context(), &sdk.PackageSetExtensionRequest{Package: "notes", Extension: id, Value: v, By: by})
		return err
	}
	texts := func(agent string) string {
		var out []string
		for _, p := range h.m.PromptAppends(agent) {
			out = append(out, p.Package+"."+p.Extension+"="+p.Text)
		}
		return strings.Join(out, "|")
	}
	if got := texts("code"); got != "notes.greeting=早安" {
		t.Fatalf("沒設值用預設、setting 與 page_slot 不算：%q", got)
	}
	if err := set("greeting", map[string]any{"value": "  記得喝水\n"}); err != nil {
		t.Fatal(err)
	}
	if err := set("coach", map[string]any{"value": "先問睡得好不好"}); err != nil {
		t.Fatal(err)
	}
	if got := texts("code"); got != "notes.greeting=記得喝水" {
		t.Fatalf("別的職位不帶指定給 health 的那段：%q", got)
	}
	if got := texts("health"); got != "notes.greeting=記得喝水|notes.coach=先問睡得好不好" {
		t.Fatalf("health 兩段都帶、照宣告順序：%q", got)
	}
	if err := set("greeting", map[string]any{"value": ""}); err != nil {
		t.Fatal(err)
	}
	if got := texts("code"); got != "" {
		t.Fatalf("主人清成空字串＝不要這段（不退回預設）：%q", got)
	}
	if err := set("top", map[string]any{"value": "x"}); codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "notes.top") {
		t.Fatalf("page_slot 沒有值可以設，要說明怎麼用：%v", err)
	}
	if err := set("coach", map[string]any{"value": strings.Repeat("長", promptAppendMax)}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("太長要 INVALID：%v", err)
	}
	if _, err := h.m.SetEnabled(t.Context(), &sdk.PackageSetEnabledRequest{ID: "notes", Enabled: false, By: by}); err != nil {
		t.Fatal(err)
	}
	if got := texts("health"); got != "" {
		t.Fatalf("套件停用就不帶：%q", got)
	}
	// agent 只能用在 prompt_append。
	h.write(sdk.TierOfficial, "bad", dataPkg("bad", "1.0.0", "[[extension]]\nid = \"x\"\nkind = \"setting\"\nagent = \"code\"\n"), nil)
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if p := h.get("bad"); p.State != sdk.PackageStateRejected || !strings.Contains(p.Error, "prompt_append") {
		t.Fatalf("setting 寫 agent 要被拒並說明：%+v", p)
	}
}

// 別人分享的套件（DEV-154 審查必修 3）：prompt_append 的預設值不會自動進秘書的指令，主人設了值才帶；
// 預設太長、說明寫多行的套件被拒；設定檔手改成超長的只帶前面一段，健檢提醒。
func TestPromptAppendFromSharedPackage(t *testing.T) {
	h := newHarness(t)
	ext := "[[extension]]\nid = \"greeting\"\nkind = \"prompt_append\"\ndescription = \"開場多說一句\"\ndefault = \"照我說的做\"\n"
	h.write(sdk.TierInstalled, "shared", dataPkg("shared", "1.0.0", ext), nil)
	h.write(sdk.TierMine, "own", dataPkg("own", "1.0.0", ext), nil)
	h.write(sdk.TierInstalled, "longdefault", dataPkg("longdefault", "1.0.0", "[[extension]]\nid = \"g\"\nkind = \"prompt_append\"\ndefault = \""+strings.Repeat("a", promptAppendMax+1)+"\"\n"), nil)
	h.write(sdk.TierInstalled, "multiline", dataPkg("multiline", "1.0.0", "[[extension]]\nid = \"g\"\nkind = \"prompt_append\"\ndescription = \"第一行\\n## 職責\"\n"), nil)
	h.start()
	texts := func() string {
		var out []string
		for _, p := range h.m.PromptAppends("code") {
			out = append(out, p.Package+"="+p.Text)
		}
		return strings.Join(out, "|")
	}
	if got := texts(); got != "own=照我說的做" {
		t.Fatalf("分享來的套件預設值不能自動生效：%q", got)
	}
	for _, id := range []string{"longdefault", "multiline"} {
		if p := h.get(id); p.State != sdk.PackageStateRejected {
			t.Errorf("%s 要被拒：%+v", id, p)
		}
	}
	// 秘書不能替主人把分享來的套件那一段設進指令（可能是被套件的說明帶著走）；主人本人才行。
	if _, err := h.m.SetExtension(t.Context(), &sdk.PackageSetExtensionRequest{Package: "shared", Extension: "greeting", Value: map[string]any{"value": "照套件說的做"}, By: &sdk.Actor{Name: "code"}}); err == nil || !strings.Contains(err.Error(), "只有主人本人能設") {
		t.Fatalf("秘書設分享套件的補充要被擋：%v", err)
	}
	by := &sdk.Actor{Name: "owner(web)"}
	if _, err := h.m.SetExtension(t.Context(), &sdk.PackageSetExtensionRequest{Package: "shared", Extension: "greeting", Value: map[string]any{"value": "記得喝水"}, By: by}); err != nil {
		t.Fatal(err)
	}
	if got := texts(); got != "own=照我說的做|shared=記得喝水" {
		t.Fatalf("主人設了值才帶：%q", got)
	}
	// 手改設定檔成超長：只帶前面那一段（不切在字中間），健檢提醒。
	st, err := h.m.loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	st.Extensions["own.greeting"] = strings.Repeat("月", promptAppendMax)
	if err := h.m.saveSettings(st); err != nil {
		t.Fatal(err)
	}
	for _, p := range h.m.PromptAppends("code") {
		if p.Package == "own" && (len(p.Text) > promptAppendMax || !utf8.ValidString(p.Text)) {
			t.Fatalf("超長要截掉而且不能切壞字：%d", len(p.Text))
		}
	}
	if got := runCheck(t, h.m, "packages.prompt_append"); len(got) != 1 || got[0].Status != app.StatusWarn || !strings.Contains(got[0].Message, "own") {
		t.Fatalf("健檢要提醒：%+v", got)
	}
}

// TestPromptAppendUntilRecord：until_record 的補充只在那筆紀錄還不在時帶（DEV-157「只講一次」）；紀錄寫了（封存了也算）就不帶、
// 別的補充照帶；讀不到 vault 就不帶（fail closed）；套件停用不帶；寫錯（不是自己的種類、不是 prompt_append、格式不對）套件被拒。
func TestPromptAppendUntilRecord(t *testing.T) {
	h := newHarness(t)
	withVault(h)
	ext := demoNoteDecl + `
[[contributes.data]]
kind = "hello"
schema = "schemas/hello.toml"
ids = "given"

[[extension]]
id = "guide"
kind = "prompt_append"
default = "一直都在"

[[extension]]
id = "intro"
kind = "prompt_append"
default = "只講一次"
until_record = "hello/main"
`
	files := map[string]string{"schemas/note.toml": demoNoteSchema, "schemas/hello.toml": "schema = 1\ntitle = \"打過招呼\"\n[[field]]\nname = \"status\"\ntype = \"string\"\n"}
	h.write(sdk.TierOfficial, "demo", dataPkg("demo", "1.0.0", ext), files)
	h.start()
	texts := func() string {
		var out []string
		for _, p := range h.m.PromptAppends("code") {
			out = append(out, p.Extension+"="+p.Text)
		}
		return strings.Join(out, "|")
	}
	if got := texts(); got != "guide=一直都在|intro=只講一次" {
		t.Fatalf("紀錄還不在：兩段都帶：%q", got)
	}
	ctx := t.Context()
	if _, err := h.home.Vault.Put(ctx, vault.Record{Kind: "demo.hello", ID: "main", Fields: map[string]any{"status": "done"}}, "code", ""); err != nil {
		t.Fatal(err)
	}
	if got := texts(); got != "guide=一直都在" {
		t.Fatalf("紀錄寫了就不帶那一段：%q", got)
	}
	if err := h.home.Vault.ArchiveNow(ctx, "demo.hello", "main", "owner(cli)"); err != nil {
		t.Fatal(err)
	}
	if got := texts(); got != "guide=一直都在" {
		t.Fatalf("封存了也算講過：%q", got)
	}
	h.m.d.Vault = nil
	if got := texts(); got != "guide=一直都在" {
		t.Fatalf("讀不到紀錄就不帶（寧可不講）：%q", got)
	}
	withVault(h)
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "demo", Enabled: false, By: &sdk.Actor{Name: "owner(web)"}}); err != nil {
		t.Fatal(err)
	}
	if got := texts(); got != "" {
		t.Fatalf("套件停用就都不帶：%q", got)
	}

	for name, decl := range map[string]string{
		"別人的種類":            "[[extension]]\nid = \"x\"\nkind = \"prompt_append\"\nuntil_record = \"other/main\"\n",
		"不是 prompt_append": demoNoteDecl + "[[extension]]\nid = \"x\"\nkind = \"setting\"\nuntil_record = \"note/main\"\n",
		"沒有編號":             demoNoteDecl + "[[extension]]\nid = \"x\"\nkind = \"prompt_append\"\nuntil_record = \"note\"\n",
		"編號有斜線":            demoNoteDecl + "[[extension]]\nid = \"x\"\nkind = \"prompt_append\"\nuntil_record = \"note/a/b\"\n",
	} {
		dir := h.write(sdk.TierMine, "bad", dataPkg("bad", "1.0.0", decl), map[string]string{"schemas/note.toml": demoNoteSchema})
		if _, err := ParseManifest(dir, sdk.TierMine); err == nil || !strings.Contains(err.Error(), "until_record") {
			t.Errorf("%s 要被拒並說明：%v", name, err)
		}
	}
}
