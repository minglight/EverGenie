package pkg

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/agent"
	"github.com/minglight/evergenie/sdk"
)

// 分享套件（SEC-302 S5）：export 只帶套件資料夾、掃個資、--git；預覽列出帶來的角色；安裝後角色馬上在角色庫；更新不動設定與資料。

const fairyAvatarPNG = "\x89PNG\r\n\x1a\nfake"

// gardenFeature 用 packages new 建 garden，再放一個角色（personas/fairy.md＋頭像）並宣告 [[contributes.persona]]。
func gardenFeature(t *testing.T, h *harness) string {
	t.Helper()
	if _, err := newPkg(h, "garden", "花園"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(h.home.Dir, "packages", "mine", "garden")
	man, err := os.ReadFile(filepath.Join(dir, "package.toml"))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{
		"package.toml":               string(man) + "\n" + gardenManifestExtra,
		"personas/fairy.md":          fairyCardText,
		"personas/avatars/fairy.png": fairyAvatarPNG,
	})
	h.tick()
	h.tick()
	if p := h.get("garden"); p.State == sdk.PackageStateRejected {
		t.Fatalf("garden 加了角色後被拒：%s", p.Error)
	}
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func export(h *harness, req sdk.PackageExportRequest) (*sdk.PackageExportResponse, error) {
	if req.By == nil {
		req.By = owner
	}
	return h.m.Export(context.Background(), &req)
}

// filesUnder 列出資料夾裡的一般檔案（相對路徑、斜線、排序）。
func filesUnder(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// TestExport：mine 的套件匯出成資料夾（--git 加 .gitignore、不 init）與 .tar.gz（都在 <id>/ 底下），內容一樣、manifest 驗得過；
// 官方回 FORBIDDEN 並說「別人升級就有」、fork 回 FORBIDDEN、分享來的可以匯出（不帶 .source.toml）；輸出位置的檢查。
// Guards: REQ-334, REQ-337
func TestExport(t *testing.T) {
	h := newHarness(t)
	h.write(sdk.TierOfficial, "diary", dataPkg("diary", "1.0.0", ""), nil)
	h.start()
	gardenFeature(t, h)
	out := filepath.Join(t.TempDir(), "share", "garden")
	r, err := export(h, sdk.PackageExportRequest{ID: "garden", Out: out, Git: true})
	if err != nil {
		t.Fatal(err)
	}
	if r.Blocked || len(r.Findings) != 0 || r.Path != out || r.Format != "dir" {
		t.Fatalf("鷹架＋角色不該有疑似個資：%+v", r)
	}
	want := []string{".gitignore", "README.md", "agents/garden/agent.md", "package.toml", "pages/index.tsx", "personas/.gitkeep", "personas/avatars/fairy.png", "personas/fairy.md", "schemas/.gitkeep"}
	if got := filesUnder(t, out); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("匯出的檔案：%v", got)
	}
	if strings.Contains(strings.Join(r.Files, ","), ".gitignore") {
		t.Fatalf("Files 只列套件資料夾裡的檔：%v", r.Files)
	}
	ign, _ := os.ReadFile(filepath.Join(out, ".gitignore"))
	for _, l := range []string{".env", "node_modules/", ".source.toml"} {
		if !strings.Contains(string(ign), l+"\n") {
			t.Errorf(".gitignore 少了 %s：%s", l, ign)
		}
	}
	if exists(filepath.Join(out, ".git")) {
		t.Fatal("--git 不自己 git init")
	}
	if _, err := ParseManifest(out, sdk.TierInstalled); err != nil {
		t.Fatalf("匯出的資料夾要能直接安裝：%v", err)
	}
	if left := dirNames(t, filepath.Dir(out)); strings.Join(left, ",") != "garden" {
		t.Fatalf("不留暫存：%v", left)
	}

	// 壓縮檔：同樣的檔（沒有 .gitignore），全部在 garden/ 底下。
	tgz := filepath.Join(t.TempDir(), "garden.tar.gz")
	if r, err := export(h, sdk.PackageExportRequest{ID: "garden", Out: tgz}); err != nil || r.Format != "tar.gz" {
		t.Fatalf("tar.gz：%v %+v", err, r)
	}
	var names []string
	f, _ := os.Open(tgz)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hd, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(hd.Name, "garden/") {
			t.Fatalf("壓縮檔裡的路徑要在 garden/ 底下：%s", hd.Name)
		}
		if hd.Typeflag == tar.TypeReg {
			names = append(names, strings.TrimPrefix(hd.Name, "garden/"))
		}
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(want[1:], ",") {
		t.Fatalf("壓縮檔內容：%v", names)
	}

	// 預設位置：<家>/runtime/exports/garden-0.1.0，再匯一次加 -2。
	r1, err1 := export(h, sdk.PackageExportRequest{ID: "garden"})
	r2, err2 := export(h, sdk.PackageExportRequest{ID: "garden"})
	if err1 != nil || err2 != nil || r1.Path != filepath.Join(h.home.Dir, "runtime", "exports", "garden-0.1.0") || r2.Path != r1.Path+"-2" {
		t.Fatalf("預設位置：%v %v %+v %+v", err1, err2, r1, r2)
	}

	// 拒絕的情況。
	cases := []struct {
		name string
		req  sdk.PackageExportRequest
		code sdk.ErrorCode
		msg  string
	}{
		{"官方", sdk.PackageExportRequest{ID: "diary"}, sdk.ErrorCodeForbidden, "別人升級"},
		{"沒有", sdk.PackageExportRequest{ID: "nope"}, sdk.ErrorCodeNotFound, "nope"},
		{"相對路徑", sdk.PackageExportRequest{ID: "garden", Out: "share/garden"}, sdk.ErrorCodeInvalid, "完整路徑"},
		{"已存在", sdk.PackageExportRequest{ID: "garden", Out: out}, sdk.ErrorCodeConflict, "已經存在"},
		{"git 配壓縮檔", sdk.PackageExportRequest{ID: "garden", Out: filepath.Join(t.TempDir(), "g.tgz"), Git: true}, sdk.ErrorCodeInvalid, "資料夾"},
		{"放進 packages", sdk.PackageExportRequest{ID: "garden", Out: filepath.Join(h.home.Dir, "packages", "mine", "garden-copy")}, sdk.ErrorCodeInvalid, "packages/"},
		{"放進 vault", sdk.PackageExportRequest{ID: "garden", Out: filepath.Join(h.home.Dir, "vault", "x")}, sdk.ErrorCodeInvalid, "vault/"},
		{"放進自己", sdk.PackageExportRequest{ID: "garden", Out: filepath.Join(h.home.Dir, "packages", "mine", "garden", "out")}, sdk.ErrorCodeInvalid, "套件"},
	}
	for _, c := range cases {
		if _, err := export(h, c.req); codeOf(err) != c.code || !strings.Contains(errMessage(err), c.msg) {
			t.Errorf("%s：要 %s「%s」，實際 %v", c.name, c.code, c.msg, err)
		}
	}
	if exists(filepath.Join(h.home.Dir, "packages", "mine", "garden-copy")) || exists(filepath.Join(h.home.Dir, "packages", "mine", "garden", "out")) {
		t.Fatal("被拒時不能寫任何東西")
	}
	// 空資料夾可以當輸出位置（先建好的空 repo 資料夾）。
	empty := t.TempDir()
	if _, err := export(h, sdk.PackageExportRequest{ID: "garden", Out: empty}); err != nil || !exists(filepath.Join(empty, "package.toml")) {
		t.Fatalf("空資料夾可以用：%v", err)
	}

	// fork：別人裝不起來，拒絕。
	if _, err := h.m.Fork(context.Background(), &sdk.PackageForkRequest{ID: "diary", By: owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := export(h, sdk.PackageExportRequest{ID: "diary"}); codeOf(err) != sdk.ErrorCodeForbidden || !strings.Contains(errMessage(err), "fork") {
		t.Fatalf("fork 不能匯出：%v", err)
	}

	// 分享來的：可以再分享，不帶安裝來源紀錄。
	h.write(sdk.TierInstalled, "notes", dataPkg("notes", "1.2.0", ""), map[string]string{sourceFile: "url = \"file:///x\"\n"})
	h.tick()
	h.tick()
	r, err = export(h, sdk.PackageExportRequest{ID: "notes", Out: filepath.Join(t.TempDir(), "notes")})
	if err != nil || strings.Join(r.Files, ",") != "package.toml" || !strings.Contains(strings.Join(r.Skipped, ","), sourceFile) {
		t.Fatalf("分享來的匯出：%v %+v", err, r)
	}
}

// TestExportExcludesData：絕不帶 vault/pkg/<id>/ 與 runtime/；資料夾裡的 .git、node_modules、fork 基準、暫存、編譯產物不帶並說明；
// 符號連結不跟：指在資料夾裡的略過，指到外面（例如連到自己的資料）整個拒絕並列出、什麼都不寫。
// Guards: REQ-334, REQ-335
func TestExportExcludesData(t *testing.T) {
	h := newHarness(t)
	h.start()
	dir := gardenFeature(t, h)
	const secretData = "我家花園的秘密資料"
	writeFiles(t, h.home.Dir, map[string]string{
		"vault/pkg/garden/plant/p-1.md":   "---\nid: p-1\n---\n" + secretData + "\n",
		"runtime/packages/garden/out.log": secretData + "\n",
	})
	writeFiles(t, dir, map[string]string{
		".git/config":             "[core]\n",
		"node_modules/x/index.js": "module.exports = 1\n",
		".install-ab12/garden/x":  "暫存\n",
		"bin/garden":              "\x7fELF\x02\x01\x01 fake binary",
		"cmd/garden/garden.o":     "obj",
		".DS_Store":               "x",
		"notes/extra.md":          "自己寫的說明\n",
	})
	if err := os.Symlink("README.md", filepath.Join(dir, "readme-link.md")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "garden")
	r, err := export(h, sdk.PackageExportRequest{ID: "garden", Out: out})
	if err != nil {
		t.Fatal(err)
	}
	got := filesUnder(t, out)
	for _, f := range got {
		if strings.HasPrefix(f, ".git/") || strings.HasPrefix(f, "node_modules/") || strings.HasPrefix(f, ".install-") || strings.HasPrefix(f, "bin/") || strings.HasSuffix(f, ".o") || f == ".DS_Store" || f == "readme-link.md" {
			t.Errorf("不該帶：%s", f)
		}
		if b, _ := os.ReadFile(filepath.Join(out, f)); strings.Contains(string(b), secretData) {
			t.Errorf("%s 帶到了使用者的資料", f)
		}
	}
	if !strings.Contains(strings.Join(got, ","), "notes/extra.md") {
		t.Errorf("自己加的檔要帶：%v", got)
	}
	skipped := strings.Join(r.Skipped, "\n")
	for _, s := range []string{".git：", "node_modules：", ".install-ab12：", "bin/garden：編譯產物", "cmd/garden/garden.o：編譯產物", "readme-link.md：符號連結"} {
		if !strings.Contains(skipped, s) {
			t.Errorf("Skipped 要說明 %s：\n%s", s, skipped)
		}
	}

	// 指到資料夾外面的符號連結（連到自己的資料）：整個拒絕、列出、什麼都不寫。
	if err := os.Symlink(filepath.Join(h.home.Dir, "vault", "pkg", "garden"), filepath.Join(dir, "data")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), filepath.Join(dir, "personas", "broken.md")); err != nil {
		t.Fatal(err)
	}
	out2 := filepath.Join(t.TempDir(), "garden")
	_, err = export(h, sdk.PackageExportRequest{ID: "garden", Out: out2, AllowFindings: true})
	if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(errMessage(err), "data") || !strings.Contains(errMessage(err), "personas/broken.md") {
		t.Fatalf("跳出資料夾的符號連結要拒絕並列出：%v", err)
	}
	if exists(out2) || len(dirNames(t, filepath.Dir(out2))) != 0 {
		t.Fatal("被拒時什麼都不寫")
	}
}

// TestExportPrivacyScan：掃到金鑰、家的路徑、email、.env／secrets 檔就停下（Blocked，不寫任何東西），列出檔案與行號，
// 說明只露出開頭幾個字；常見的非個資（範例網域、git@、檔名裡的 @2x、commit 雜湊、網址裡的 /home/）不算；--allow-findings 才照樣匯出。
// Guards: REQ-336
func TestExportPrivacyScan(t *testing.T) {
	h := newHarness(t)
	h.start()
	dir := gardenFeature(t, h)
	const key = "sk-ant-api03-Zq8xLmP2vR7tYc4nWb9KdF3hJs6Ge1Ua"
	writeFiles(t, dir, map[string]string{
		"config.toml":     "# 設定\nname = \"x\"\napi_token = \"" + key + "\"\n",
		"notes/where.md":  "資料在\n/Users/alice/garden 底下\n",
		"notes/mine.md":   "家在 " + h.home.Dir + "/vault\n",
		"notes/about.md":  "聯絡 alice.wang@mailbox.org\n",
		"notes/random.md": "token: Xk9fQ2mZ7pL4vR8tN1cW6yB3hJ5dG0sA\n",
		".env":            "PLACEHOLDER=1\n",
		"secrets/a.txt":   "nothing\n",
		"notes/ok.md": strings.Join([]string{
			"範例 someone@example.com、git@github.com:me/garden.git、icon@2x.png",
			"commit 0123456789abcdef0123456789abcdef01234567",
			"網址 https://example.com/home/page",
			"api_key = \"your-api-key-here\"",
			"~/Documents 相對路徑可以",
		}, "\n") + "\n",
	})
	out := filepath.Join(t.TempDir(), "garden")
	r, err := export(h, sdk.PackageExportRequest{ID: "garden", Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Blocked || r.Path != "" || exists(out) || len(dirNames(t, filepath.Dir(out))) != 0 {
		t.Fatalf("有疑似個資要停下、不寫：%+v", r)
	}
	type key2 struct {
		path string
		line int32
		kind string
	}
	got := map[key2]string{}
	for _, f := range r.Findings {
		got[key2{f.Path, f.Line, f.Kind}] = f.What
		if strings.Contains(f.What, key) || strings.Contains(f.What, "Xk9fQ2mZ7pL4vR8tN1cW6yB3hJ5dG0sA") || strings.Contains(f.What, "alice.wang") {
			t.Errorf("說明不能露出完整內容：%s", f.What)
		}
		if f.Path == "notes/ok.md" {
			t.Errorf("不該算個資：%+v", f)
		}
	}
	for _, w := range []key2{
		{"config.toml", 3, findSecret},
		{"notes/where.md", 2, findHomePath},
		{"notes/mine.md", 1, findHomePath},
		{"notes/about.md", 1, findEmail},
		{"notes/random.md", 1, findSecret},
		{".env", 0, findSecretFile},
		{"secrets/a.txt", 0, findSecretFile},
	} {
		if _, ok := got[w]; !ok {
			t.Errorf("少了 %+v；實際：%+v", w, r.Findings)
		}
	}
	if len(got) != 7 {
		t.Errorf("只該有 7 筆：%+v", r.Findings)
	}

	// 主人看過、同意：--allow-findings 照樣匯出，清單照樣回。
	r, err = export(h, sdk.PackageExportRequest{ID: "garden", Out: out, AllowFindings: true})
	if err != nil || r.Blocked || r.Path != out || len(r.Findings) != 7 || !exists(filepath.Join(out, "config.toml")) || !exists(filepath.Join(out, ".env")) {
		t.Fatalf("--allow-findings：%v %+v", err, r)
	}
}

// TestPreviewListsPersonas：預覽列出帶來的角色——有效代號 <id>.<slug>、卡上的名字、有沒有頭像。
// Guards: REQ-338
func TestPreviewListsPersonas(t *testing.T) {
	h := newHarness(t)
	h.start()
	src := filepath.Join(t.TempDir(), "garden")
	writeFiles(t, src, map[string]string{
		"package.toml":               dataPkg("garden", "1.0.0", gardenManifestExtra+"[[contributes.persona]]\nslug = \"bloom\"\npath = \"personas/bloom.md\"\n"),
		"personas/fairy.md":          fairyCardText,
		"personas/avatars/fairy.png": fairyAvatarPNG,
		"personas/bloom.md":          "+++\nname = \"花開\"\n+++\n舊格式的卡。\n",
	})
	r, err := h.m.Preview(context.Background(), &sdk.PackagePreviewRequest{Source: src})
	if err != nil || len(r.Problems) != 0 {
		t.Fatalf("%v %+v", err, r)
	}
	want := []sdk.PackagePreviewPersona{{ID: "garden.fairy", Slug: "fairy", Name: "Fairy", Avatar: true}, {ID: "garden.bloom", Slug: "bloom", Name: "花開"}}
	if len(r.Personas) != 2 || r.Personas[0] != want[0] || r.Personas[1] != want[1] {
		t.Fatalf("帶來的角色：%+v", r.Personas)
	}
	if exists(filepath.Join(h.home.Dir, "packages", "installed", "garden")) {
		t.Fatal("預覽不安裝")
	}
}

// TestInstallBringsPersonas：從網址（file://）裝一個 export 出來、放上 git 的套件 → 角色庫馬上有 garden.fairy（來自套件、可以指派），
// 不用重開；本機資料夾安裝也一樣。
// Guards: REQ-339
func TestInstallBringsPersonas(t *testing.T) {
	a := newHarness(t)
	a.start()
	gardenFeature(t, a)
	repo := newGitRepo(t)
	if err := os.RemoveAll(repo.dir); err != nil {
		t.Fatal(err)
	}
	if _, err := export(a, sdk.PackageExportRequest{ID: "garden", Out: repo.dir, Git: true}); err != nil {
		t.Fatal(err)
	}
	repo.git("init", "-q", "-b", "main")
	repo.git("add", "-A")
	repo.git("commit", "-q", "-m", "garden 0.1.0")
	repo.git("tag", "v0.1.0")

	for _, src := range []string{repo.url(), repo.dir} {
		b := newHarness(t)
		b.start()
		roster := agent.New(agent.Deps{Vault: b.home.Vault, Clock: b.home.Clock, Bus: quietBus{}})
		roster.SetHome(b.home.Dir)
		if err := roster.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = roster.Stop(context.Background()) })
		roster.SetPersonaSource(stubLib{m: b.m, home: b.home.Dir})
		if _, err := b.m.Install(context.Background(), &sdk.PackageInstallRequest{Source: src, Confirmed: true, By: owner}); err != nil {
			t.Fatalf("%s：%v", src, err)
		}
		cards, err := stubLib{m: b.m, home: b.home.Dir}.Library(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, c := range cards {
			if c.Slug == "garden.fairy" {
				found = c.Source == personaref.SourcePackage && c.Package == "garden" && c.Unassignable == ""
			}
		}
		if !found {
			t.Fatalf("%s：裝好後角色庫要馬上有可指派的 garden.fairy：%+v", src, cards)
		}
		if exists(filepath.Join(b.home.Dir, "vault", "pkg", "garden")) {
			t.Fatalf("%s：B 家的 vault/pkg/garden 不該出現", src)
		}
	}
}

// TestUpgradeKeepsSettingsAndData：從網址裝的套件更新到新版，settings/packages.toml（擴充點的值、啟用）與 vault/pkg/<id>/ 一個位元組都不變。
// Guards: REQ-339
func TestUpgradeKeepsSettingsAndData(t *testing.T) {
	ext := "[[extension]]\nid = \"footer\"\nkind = \"setting\"\ndescription = \"頁尾\"\n"
	r := newGitRepo(t)
	r.commit(map[string]string{"package.toml": dataPkg("notes", "1.0.0", ext)}, "v1.0.0")
	h := newHarness(t)
	h.start()
	ctx := context.Background()
	if _, err := h.m.Install(ctx, &sdk.PackageInstallRequest{Source: r.url(), Confirmed: true, By: owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.SetExtension(ctx, &sdk.PackageSetExtensionRequest{Package: "notes", Extension: "footer", Value: map[string]any{"value": "我的頁尾"}, By: owner}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.SetEnabled(ctx, &sdk.PackageSetEnabledRequest{ID: "notes", Enabled: true, By: owner}); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, h.home.Dir, map[string]string{"vault/pkg/notes/note/n-1.md": "---\nid: n-1\n---\n我的筆記\n"})
	settings := filepath.Join(h.home.Dir, "settings", "packages.toml")
	before, _ := os.ReadFile(settings)
	if !strings.Contains(string(before), "我的頁尾") {
		t.Fatalf("擴充點的值要先存好：%s", before)
	}
	vaultBefore := snapshot(t, filepath.Join(h.home.Dir, "vault", "pkg", "notes"))

	r.commit(map[string]string{"package.toml": dataPkg("notes", "1.1.0", ext), "README.md": "新版\n"}, "v1.1.0")
	if up, err := h.m.Upgrade(ctx, &sdk.PackageUpgradeRequest{ID: "notes", By: owner}); err != nil || up.To != "1.1.0" {
		t.Fatalf("更新：%v %+v", err, up)
	}
	if after, _ := os.ReadFile(settings); string(after) != string(before) {
		t.Fatalf("更新不能動 settings/packages.toml：\n%s\n→\n%s", before, after)
	}
	if after := snapshot(t, filepath.Join(h.home.Dir, "vault", "pkg", "notes")); after != vaultBefore {
		t.Fatalf("更新不能動 vault/pkg/notes：%q → %q", vaultBefore, after)
	}
	if v, err := h.m.ListExtensions(ctx, &sdk.PackageListExtensionsRequest{Package: "notes"}); err != nil || v.Values["notes.footer"] != "我的頁尾" {
		t.Fatalf("擴充點的值還在：%v %+v", err, v)
	}
}

// snapshot 把資料夾的檔案與內容串成一個字串（比對位元組不變）。
func snapshot(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	for _, f := range filesUnder(t, root) {
		data, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		b.WriteString(f + "\x00" + string(data) + "\x00")
	}
	return b.String()
}

// TestExportPrivacyScanUnits：掃描規則的單元檢查（前綴、亂碼、佔位字、路徑邊界、email 例外、二進位檔只看檔名）。
// Guards: REQ-336
func TestExportPrivacyScanUnits(t *testing.T) {
	hit := func(line string) []string {
		var kinds []string
		for _, f := range scanPrivacy("x.md", []byte(line), []string{"/opt/myhome"}) {
			kinds = append(kinds, f.Kind)
		}
		return kinds
	}
	for line, want := range map[string]string{
		"ghp_abcdefghijklmnopqrstuvwxyz0123":          findSecret,
		"AKIAABCDEFGHIJKLMNOP":                        findSecret,
		"-----BEGIN OPENSSH PRIVATE KEY-----":         findSecret,
		`const password = "hunter2hunter2"`:           findSecret,
		"file:///Users/bob/x":                         findHomePath,
		`"/home/carol/notes"`:                         findHomePath,
		"see /opt/myhome/vault":                       findHomePath,
		"mail me: bob.lee@company.co":                 findEmail,
		"https://example.com/home/page":               "",
		"/Users/Shared/x":                             "",
		"src/home/user.ts":                            "",
		"git@github.com:a/b.git":                      "",
		"logo@3x.webp":                                "",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa":  "",
		"handleUserFeatureExportRequestForGarden2026": "",
		"0123456789abcdef0123456789abcdef01234567":    "",
		`token = "${GARDEN_TOKEN}"`:                   "",
		`<input type="password" placeholder="密碼">`:    "",
	} {
		got := strings.Join(hit(line), ",")
		if got != want {
			t.Errorf("%q → %q，要 %q", line, got, want)
		}
	}
	if f := scanPrivacy("avatar.png", []byte("\x89PNG\x00\x00 /Users/bob ghp_abcdefghijklmnopqrstuvwxyz0123"), nil); len(f) != 0 {
		t.Errorf("二進位檔不掃內容：%+v", f)
	}
	if f := scanPrivacy("certs/server.pem", []byte("x"), nil); len(f) != 1 || f[0].Kind != findSecretFile {
		t.Errorf(".pem 檔名要算：%+v", f)
	}
}

// TestUserFeaturesDocHasSharing：慣例文件有「分享」一節（export、--git、個資掃描、不帶資料、怎麼安裝），技能 create-feature 有分享步驟。
// Guards: REQ-340
func TestUserFeaturesDocHasSharing(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "ai_docs", "conventions", "user-features.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "## 分享")
	if i < 0 {
		t.Fatal("文件要有「分享」一節")
	}
	section := doc[i:]
	if j := strings.Index(section[len("## 分享"):], "\n## "); j >= 0 {
		section = section[:len("## 分享")+j]
	}
	for _, want := range []string{"packages export", "--git", "--allow-findings", "vault/pkg/<id>/", "packages install", "<id>.<slug>"} {
		if !strings.Contains(section, want) {
			t.Errorf("「分享」要提到 %q", want)
		}
	}
	found, _ := filepath.Glob(filepath.Join("..", "..", "..", "..", "packages", "official", "engine-*", "skills", "create-feature", "SKILL.md"))
	if len(found) == 0 {
		t.Fatal("技能檔要存在")
	}
	if skill, _ := os.ReadFile(found[0]); !strings.Contains(string(skill), "packages export") || !strings.Contains(string(skill), "--allow-findings") {
		t.Error("技能 create-feature 要有分享步驟（packages export、--allow-findings）")
	}
}
