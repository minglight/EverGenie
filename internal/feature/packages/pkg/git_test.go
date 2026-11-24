package pkg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 套件從 git 網址安裝（REQ-282）：來源是 t.TempDir() 裡的本機 git repo，用 file:// 網址；不連網路。

type gitRepo struct {
	t   *testing.T
	dir string
}

func newGitRepo(t *testing.T) *gitRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("這台沒有 git")
	}
	r := &gitRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	return r
}

func (r *gitRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit 寫檔、commit，tag 不空就打 tag（annotated 與 lightweight 都該能用，這裡用 lightweight）。
func (r *gitRepo) commit(files map[string]string, tag string) {
	r.t.Helper()
	for rel, body := range files {
		p := filepath.Join(r.dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
	r.git("add", "-A")
	r.git("commit", "-q", "-m", "c")
	if tag != "" {
		r.git("tag", tag)
	}
}

func (r *gitRepo) url() string { return "file://" + r.dir }

func notesRepo(t *testing.T, id string, versions ...string) *gitRepo {
	r := newGitRepo(t)
	for _, v := range versions {
		r.commit(map[string]string{"package.toml": dataPkg(id, v, ""), "README.md": "版本 " + v + "\n"}, "v"+v)
	}
	return r
}

// leftovers：packages/installed 底下（含隱藏的暫存）與 runtime/packages 底下有沒有留下東西。
func leftovers(h *harness) []string {
	var out []string
	for _, root := range []string{filepath.Join(h.home.Dir, "packages", "installed"), filepath.Join(h.home.Dir, "runtime", "packages")} {
		items, _ := os.ReadDir(root)
		for _, it := range items {
			out = append(out, filepath.Join(root, it.Name()))
		}
	}
	return out
}

// TestInstallFromGitNeedsConfirm：從網址安裝一定要 confirmed；沒有確認連網路都不碰，也不留暫存。
// Guards: REQ-282
func TestInstallFromGitNeedsConfirm(t *testing.T) {
	r := notesRepo(t, "notes", "1.0.0")
	h := newHarness(t)
	h.start()
	_, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), By: &sdk.Actor{Name: "owner(cli)"}})
	if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "預覽") {
		t.Fatalf("沒確認要 INVALID：%v", err)
	}
	if left := leftovers(h); len(left) != 0 {
		t.Fatalf("不該留下東西：%v", left)
	}
}

// TestInstallFromGitPicksLatestTag：沒指定版本時用最大的 semver tag（v1.10.0 > v1.9.0；預發行 tag 與 tag 之後的 commit 不算）；
// 記下來源 .source.toml、不帶 .git。
// Guards: REQ-282
func TestInstallFromGitPicksLatestTag(t *testing.T) {
	r := notesRepo(t, "notes", "1.0.0", "1.9.0", "1.10.0")
	r.commit(map[string]string{"package.toml": dataPkg("notes", "2.0.0-rc1", "")}, "v2.0.0-rc1")
	r.commit(map[string]string{"package.toml": dataPkg("notes", "9.9.9", "")}, "")
	h := newHarness(t)
	h.start()
	by := &sdk.Actor{Name: "owner(cli)"}
	resp, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Confirmed: true, By: by})
	if err != nil {
		t.Fatal(err)
	}
	p := resp.Package
	if p.Version != "1.10.0" || p.Tier != sdk.TierInstalled || p.Source == nil || p.Source.URL != r.url() || p.Source.Ref != "v1.10.0" {
		t.Fatalf("要裝 v1.10.0 並記來源：%+v %+v", p, p.Source)
	}
	if want := r.git("rev-parse", "v1.10.0"); p.Source.Commit != want {
		t.Fatalf("commit 要是 tag 的那個：%s ≠ %s", p.Source.Commit, want)
	}
	dest := filepath.Join(h.home.Dir, "packages", "installed", "notes")
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		t.Fatal("不該帶 .git")
	}
	if b, _ := os.ReadFile(filepath.Join(dest, ".source.toml")); !strings.Contains(string(b), `ref = "v1.10.0"`) || !strings.Contains(string(b), r.url()) {
		t.Fatalf(".source.toml：%s", b)
	}
	if g := h.get("notes"); g.Source == nil || g.Source.Commit != p.Source.Commit {
		t.Fatalf("Get 也要有來源：%+v", g.Source)
	}
	if left := leftovers(h); len(left) != 1 || filepath.Base(left[0]) != "notes" {
		t.Fatalf("只該留下裝好的套件：%v", left)
	}
}

// TestInstallFromGitRef：指定 ref（tag 或分支）就裝那一版；沒有 tag 的 repo 用預設分支；不存在的 ref 不留東西。
// Guards: REQ-282
func TestInstallFromGitRef(t *testing.T) {
	r := notesRepo(t, "notes", "1.0.0", "1.1.0")
	h := newHarness(t)
	h.start()
	by := &sdk.Actor{Name: "owner(cli)"}
	resp, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Ref: "v1.0.0", Confirmed: true, By: by})
	if err != nil || resp.Package.Version != "1.0.0" || resp.Package.Source.Ref != "v1.0.0" {
		t.Fatalf("指定 v1.0.0：%v %+v", err, resp)
	}
	if _, err := h.m.Uninstall(t.Context(), &sdk.PackageUninstallRequest{ID: "notes", By: by}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Ref: "v9.9.9", Confirmed: true, By: by}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("不存在的 ref 要 INVALID：%v", err)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Ref: "--upload-pack=x", Confirmed: true, By: by}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("長得像選項的 ref 要 INVALID：%v", err)
	}
	if left := leftovers(h); len(left) != 0 {
		t.Fatalf("失敗不該留下東西：%v", left)
	}
	noTag := newGitRepo(t)
	noTag.commit(map[string]string{"package.toml": dataPkg("plain", "0.3.0", "")}, "")
	resp, err = h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: noTag.url(), Confirmed: true, By: by})
	if err != nil || resp.Package.Version != "0.3.0" || resp.Package.Source.Ref != "main" {
		t.Fatalf("沒有 tag 就用預設分支：%v %+v", err, resp)
	}
}

// TestInstallFromGitBadManifestLeavesNothing：package.toml 不合格、或最上層沒有 package.toml，回白話錯誤，packages/ 與暫存都不留東西。
// Guards: REQ-282
func TestInstallFromGitBadManifestLeavesNothing(t *testing.T) {
	bad := newGitRepo(t)
	bad.commit(map[string]string{"package.toml": strings.Replace(dataPkg("notes", "1.0.0", ""), "evergenie.v1", "evergenie.v99", 1)}, "v1.0.0")
	none := newGitRepo(t)
	none.commit(map[string]string{"README.md": "沒有 manifest\n"}, "v1.0.0")
	h := newHarness(t)
	h.start()
	for name, r := range map[string]*gitRepo{"bad": bad, "none": none} {
		_, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Confirmed: true, By: &sdk.Actor{Name: "owner(cli)"}})
		if codeOf(err) != sdk.ErrorCodeInvalid {
			t.Fatalf("%s 要 INVALID：%v", name, err)
		}
		if left := leftovers(h); len(left) != 0 {
			t.Fatalf("%s 不該留下東西：%v", name, left)
		}
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: "file:///no/such/repo", Confirmed: true, By: &sdk.Actor{Name: "owner(cli)"}}); err == nil {
		t.Fatal("抓不到的網址要有錯誤")
	}
	if left := leftovers(h); len(left) != 0 {
		t.Fatalf("抓不到也不該留下東西：%v", left)
	}
}

// TestInstallRejectsCredentialsInURL：網址帶帳號密碼（含 https://token@）一律擋，不連線、不記進 .source.toml。
// Guards: REQ-282
func TestInstallRejectsCredentialsInURL(t *testing.T) {
	h := newHarness(t)
	h.start()
	for _, u := range []string{"https://user:secret@example.com/x.git", "https://ghp_token@example.com/x.git", "ssh://user:pw@example.com/x.git"} {
		_, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: u, Confirmed: true, By: &sdk.Actor{Name: "owner(cli)"}})
		if codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "帳號") {
			t.Errorf("%s 要 INVALID（帳號密碼）：%v", u, err)
		}
		if _, err := h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: u}); codeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("預覽 %s 也要擋：%v", u, err)
		}
	}
	if err := checkSourceURL("git@github.com:me/x.git"); err != nil {
		t.Errorf("git@ 的使用者名稱不算帳密：%v", err)
	}
	if err := checkSourceURL("ssh://git@example.com/x.git"); err != nil {
		t.Errorf("ssh 的使用者名稱不算帳密：%v", err)
	}
}

// TestPreviewShowsProgramAndProblems：預覽回名稱、版本、會不會跑程式、用到的資料；問題（跟官方同名、缺相依、manifest 不合格）列在 Problems；
// 預覽不安裝、不留暫存；已經裝了同一個 id 標 Exists。
// Guards: REQ-282
func TestPreviewShowsProgramAndProblems(t *testing.T) {
	prog := newGitRepo(t)
	prog.commit(map[string]string{
		"package.toml":        programPkg("tracker", "", "\n[[requires]]\nid = \"base\"\nversion = \"^1.0.0\"\n"),
		"cmd/tracker/main.go": "package main\n\nfunc main() {}\n",
	}, "v0.4.0")
	clash := notesRepo(t, "diary", "1.0.0")
	bad := newGitRepo(t)
	bad.commit(map[string]string{"package.toml": "schema = 1\nid = \"x\"\n"}, "v1.0.0")
	h := newHarness(t)
	h.write(sdk.TierOfficial, "diary", dataPkg("diary", "2.0.0", ""), nil)
	dir := h.write(sdk.TierMine, "local", dataPkg("local", "1.0.0", ""), nil)
	h.start()
	// tracker 的 manifest 版本是 1.0.0（programPkg 固定），tag 是 v0.4.0：以 manifest 為準。
	r, err := h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: prog.url()})
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "tracker" || !r.RunsProgram || r.Kind != "program" || r.Ref != "v0.4.0" || len(r.Reads) != 1 || r.Reads[0] != "vault:task" || len(r.Writes) != 1 {
		t.Fatalf("預覽內容：%+v", r)
	}
	if len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "需要套件「base」") {
		t.Fatalf("缺相依要列在問題裡：%v", r.Problems)
	}
	r, err = h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: clash.url()})
	if err != nil || len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "官方") || !r.Exists {
		t.Fatalf("跟官方同名：%v %+v", err, r)
	}
	r, err = h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: bad.url()})
	if err != nil || len(r.Problems) != 1 || r.ID != "" {
		t.Fatalf("manifest 不合格要變成 Problems 而不是錯誤：%v %+v", err, r)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: prog.url(), Confirmed: true, By: &sdk.Actor{Name: "owner(cli)"}}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("有問題的預覽不能裝：%v", err)
	}
	if left := leftovers(h); len(left) != 0 {
		t.Fatalf("預覽與被擋的安裝不該留下東西：%v", left)
	}
	// 本機資料夾也能預覽；已經裝了同一個 id 會標 Exists 並提醒。
	other := filepath.Join(t.TempDir(), "local")
	if err := copyTree(dir, other); err != nil {
		t.Fatal(err)
	}
	r, err = h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: other})
	if err != nil || !r.Exists || len(r.Warnings) != 1 || r.ID != "local" {
		t.Fatalf("本機資料夾預覽：%v %+v", err, r)
	}
	if _, err := h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: "relative/dir"}); codeOf(err) != sdk.ErrorCodeInvalid {
		t.Fatalf("相對路徑要 INVALID：%v", err)
	}
}

// TestCoreConstraint：package.toml 的 core 不符合核心版本 → rejected，白話寫出限制與目前版本；
// 開發版的版本字串（0.2.0-3-gabc1234）當 0.2.0；認不出版本號（短碼）不擋、只警告；core 寫法看不懂的 manifest 不合格。
// Guards: REQ-282
func TestCoreConstraint(t *testing.T) {
	h := newHarness(t)
	h.m.coreVersion = func() string { return "0.2.0-3-gabc1234" }
	h.write(sdk.TierMine, "fits", dataPkg("fits", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "caret", strings.Replace(dataPkg("caret", "1.0.0", ""), "kind =", "core = \"^0.2.0\"\nkind =", 1), nil)
	h.write(sdk.TierMine, "newer", strings.Replace(dataPkg("newer", "1.0.0", ""), "kind =", "core = \">=0.3.0\"\nkind =", 1), nil)
	h.write(sdk.TierMine, "major", strings.Replace(dataPkg("major", "1.0.0", ""), "kind =", "core = \"^1.0.0\"\nkind =", 1), nil)
	h.write(sdk.TierMine, "weird", strings.Replace(dataPkg("weird", "1.0.0", ""), "kind =", "core = \"zzz\"\nkind =", 1), nil)
	h.start()
	for id, wantState := range map[string]sdk.PackageState{"fits": sdk.PackageStateRunning, "caret": sdk.PackageStateRunning, "newer": sdk.PackageStateRejected, "major": sdk.PackageStateRejected, "weird": sdk.PackageStateRejected} {
		if got := h.get(id).State; got != wantState {
			t.Errorf("%s：%s ≠ %s（%s）", id, got, wantState, h.get(id).Error)
		}
	}
	if e := h.get("newer").Error; !strings.Contains(e, "這個套件要 EverGenie >=0.3.0，你現在是 0.2.0-3-gabc1234") {
		t.Fatalf("白話原因：%q", e)
	}
	// 認不出核心版本：不擋、警告。
	h.m.coreVersion = func() string { return "abc1234-dirty" }
	if _, err := h.m.Reload(t.Context(), &sdk.PackageReloadRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := h.get("newer").State; got != sdk.PackageStateRunning {
		t.Fatalf("認不出版本不該擋：%s %s", got, h.get("newer").Error)
	}
	var warned bool
	for _, w := range h.m.Warnings() {
		warned = warned || strings.Contains(w, "newer") || strings.Contains(w, "認不出版本號")
	}
	if !warned {
		t.Fatalf("要有警告：%v", h.m.Warnings())
	}
	// 安裝時就擋（本機資料夾）。
	h.m.coreVersion = func() string { return "0.2.0" }
	src := filepath.Join(t.TempDir(), "late")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "package.toml"), []byte(strings.Replace(dataPkg("late", "1.0.0", ""), "kind =", "core = \">=0.9.0\"\nkind =", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: src, By: &sdk.Actor{Name: "owner(cli)"}}); codeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "你現在是 0.2.0") {
		t.Fatalf("核心版本不符不該裝：%v", err)
	}
	if r, err := h.m.Preview(t.Context(), &sdk.PackagePreviewRequest{Source: src}); err != nil || len(r.Problems) != 1 || r.Core != ">=0.9.0" {
		t.Fatalf("預覽要列問題：%v %+v", err, r)
	}
}

// TestOutdatedAndUpgradeKeepsData：從網址裝的套件，來源有新 tag 就列在 Outdated；Upgrade 換成新版、vault/pkg/<id>/ 的資料不動、
// 舊版的備份資料夾清掉；已經最新、不是從網址裝的、官方的都不能更新；來源抓不到放在 Failed。
// Guards: REQ-282
func TestOutdatedAndUpgradeKeepsData(t *testing.T) {
	r := notesRepo(t, "notes", "1.0.0")
	h := newHarness(t)
	h.write(sdk.TierOfficial, "diary", dataPkg("diary", "1.0.0", ""), nil)
	h.write(sdk.TierMine, "handmade", dataPkg("handmade", "1.0.0", ""), nil)
	h.start()
	by := &sdk.Actor{Name: "owner(cli)"}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Confirmed: true, By: by}); err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(h.home.Dir, "vault", "pkg", "notes", "note", "n-1.md")
	if err := os.MkdirAll(filepath.Dir(dataFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dataFile, []byte("---\nid: n-1\n---\n我的筆記\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := h.m.Outdated(t.Context(), &sdk.PackageOutdatedRequest{})
	if err != nil || len(out.Packages) != 0 || len(out.Failed) != 0 {
		t.Fatalf("剛裝好沒有新版：%v %+v", err, out)
	}
	if _, err := h.m.Upgrade(t.Context(), &sdk.PackageUpgradeRequest{ID: "notes", By: by}); codeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "最新") {
		t.Fatalf("已經最新要 CONFLICT：%v", err)
	}
	r.commit(map[string]string{"package.toml": dataPkg("notes", "1.1.0", ""), "README.md": "版本 1.1.0\n"}, "v1.1.0")
	out, err = h.m.Outdated(t.Context(), &sdk.PackageOutdatedRequest{})
	if err != nil || len(out.Packages) != 1 || out.Packages[0].ID != "notes" || out.Packages[0].Current != "1.0.0" || out.Packages[0].Latest != "1.1.0" || out.Packages[0].URL != r.url() {
		t.Fatalf("要列出 1.0.0 → 1.1.0：%v %+v", err, out)
	}
	up, err := h.m.Upgrade(t.Context(), &sdk.PackageUpgradeRequest{ID: "notes", By: by})
	if err != nil || up.From != "1.0.0" || up.To != "1.1.0" || up.Package.Version != "1.1.0" || up.Package.Source.Ref != "v1.1.0" {
		t.Fatalf("更新：%v %+v", err, up)
	}
	if b, _ := os.ReadFile(dataFile); !strings.Contains(string(b), "我的筆記") {
		t.Fatalf("資料要保留：%q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(h.home.Dir, "packages", "installed", "notes", "README.md")); string(b) != "版本 1.1.0\n" {
		t.Fatalf("檔案要換成新版：%q", b)
	}
	if left := leftovers(h); len(left) != 1 {
		t.Fatalf("舊版備份與暫存要清掉：%v", left)
	}
	if out, _ := h.m.Outdated(t.Context(), &sdk.PackageOutdatedRequest{}); len(out.Packages) != 0 {
		t.Fatalf("更新後沒有新版：%+v", out)
	}
	for id, want := range map[string]sdk.ErrorCode{"handmade": sdk.ErrorCodeInvalid, "diary": sdk.ErrorCodeForbidden, "ghost": sdk.ErrorCodeNotFound} {
		if _, err := h.m.Upgrade(t.Context(), &sdk.PackageUpgradeRequest{ID: id, By: by}); codeOf(err) != want {
			t.Errorf("更新 %s 要 %s：%v", id, want, err)
		}
	}
	// 來源不見了：Outdated 放 Failed，不是錯誤。
	if err := os.RemoveAll(r.dir); err != nil {
		t.Fatal(err)
	}
	out, err = h.m.Outdated(t.Context(), &sdk.PackageOutdatedRequest{})
	if err != nil || len(out.Packages) != 0 || len(out.Failed) != 1 || !strings.Contains(out.Failed[0], r.url()) {
		t.Fatalf("抓不到要放 Failed：%v %+v", err, out)
	}
}

// TestUpgradeFailureRestoresOld：新版 rename 進來、重掃之後才被拒（這裡是相依成環）→ 舊版換回來、資料夾與狀態回到更新前，回白話錯誤。
// Guards: REQ-282
func TestUpgradeFailureRestoresOld(t *testing.T) {
	r := notesRepo(t, "alpha", "1.0.0")
	h := newHarness(t)
	h.write(sdk.TierInstalled, "beta", dataPkg("beta", "1.0.0", "\n[[requires]]\nid = \"alpha\"\nversion = \"\"\n"), nil)
	h.start()
	by := &sdk.Actor{Name: "owner(cli)"}
	if _, err := h.m.Install(t.Context(), &sdk.PackageInstallRequest{Source: r.url(), Confirmed: true, By: by}); err != nil {
		t.Fatal(err)
	}
	if h.get("beta").State == sdk.PackageStateRejected {
		t.Fatalf("beta 該正常：%s", h.get("beta").Error)
	}
	// 1.1.0 反過來依賴 beta：兩邊相依成環。
	r.commit(map[string]string{"package.toml": dataPkg("alpha", "1.1.0", "\n[[requires]]\nid = \"beta\"\nversion = \"\"\n")}, "v1.1.0")
	_, err := h.m.Upgrade(t.Context(), &sdk.PackageUpgradeRequest{ID: "alpha", By: by})
	if codeOf(err) != sdk.ErrorCodeConflict || !strings.Contains(err.Error(), "已換回舊版 1.0.0") {
		t.Fatalf("要回「已換回舊版」：%v", err)
	}
	if a := h.get("alpha"); a.Version != "1.0.0" || a.State == sdk.PackageStateRejected {
		t.Fatalf("alpha 要回到 1.0.0 且正常：%+v", a)
	}
	if b := h.get("beta"); b.State == sdk.PackageStateRejected {
		t.Fatalf("beta 要跟著恢復：%s", b.Error)
	}
	if left := leftovers(h); len(left) != 2 {
		t.Fatalf("只剩 alpha 與 beta 兩個資料夾（不留備份、暫存）：%v", left)
	}
	if b, _ := os.ReadFile(filepath.Join(h.home.Dir, "packages", "installed", "alpha", ".source.toml")); !strings.Contains(string(b), "v1.0.0") {
		t.Fatalf("來源要還是 v1.0.0：%s", b)
	}
}
