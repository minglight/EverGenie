package pkg

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// officialGitHome 建一個假的「從 repo 跑的家」：官方資料夾 packages/official 在一個乾淨的 git 工作樹裡。
func officialGitHome(t *testing.T) (home, official string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("沒有 git")
	}
	root := t.TempDir()
	home = filepath.Join(root, "home")
	official = filepath.Join(root, "repo", "packages", "official")
	for _, d := range []string{home, filepath.Join(official, "alpha"), filepath.Join(official, "beta")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{
		filepath.Join(official, "alpha", "package.toml"): "id = \"alpha\"\n",
		filepath.Join(official, "beta", "package.toml"):  "id = \"beta\"\n",
	} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	repo := filepath.Join(root, "repo")
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v：%v %s", args, err, out)
		}
	}
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	return home, official
}

// EditedOfficial：乾淨的 git 工作樹不報；改過、新增檔案的官方套件各回一個 id（排序）。doctor 把它們翻成黃字。
// Guards: REQ-333
func TestEditedOfficial(t *testing.T) {
	home, official := officialGitHome(t)
	if got := EditedOfficial(context.Background(), home); len(got) != 0 {
		t.Fatalf("沒改不報：%v", got)
	}
	if err := os.WriteFile(filepath.Join(official, "beta", "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(official, "alpha", "package.toml"), []byte("id = \"alpha\"\n# 改過\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := EditedOfficial(context.Background(), home)
	if len(got) != 2 || got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("改過的官方套件（排序）：%v", got)
	}
}

// fakeGit 在 PATH 最前面放一支假 git（每次被叫就記一行、印 true），回記號檔的路徑：看 EditedOfficial 有沒有叫 git。
func fakeGit(t *testing.T, root string) string {
	t.Helper()
	bin, mark := filepath.Join(root, "fakebin"), filepath.Join(root, "git-called")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\necho \"$*\" >>'"+mark+"'\necho true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return mark
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// 官方資料夾往上沒有 .git：回空，而且根本不叫 git——乾淨的 Mac 上 /usr/bin/git 是「要先裝開發者工具」的空殼，一叫就跳安裝視窗（PKG-21）。
// 上面有 worktree 的 .git 檔也算在工作樹裡。
func TestEditedOfficialOutsideWorkTreeDoesNotRunGit(t *testing.T) {
	root := t.TempDir()
	if inGitTree(root) {
		t.Skip("暫存資料夾在某個 git repo 底下，驗不了「往上沒有 .git」")
	}
	home, official := filepath.Join(root, "home"), filepath.Join(root, "copy", "packages", "official")
	mkdirs(t, home, filepath.Join(official, "alpha"))
	mark := fakeGit(t, root)
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	if got := EditedOfficial(context.Background(), home); len(got) != 0 {
		t.Fatalf("不在工作樹裡要回空：%v", got)
	}
	if data, err := os.ReadFile(mark); err == nil {
		t.Fatalf("不在工作樹裡不該叫 git，卻叫了：%s", data)
	}
	if err := os.WriteFile(filepath.Join(root, "copy", ".git"), []byte("gitdir: /x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !inGitTree(official) {
		t.Fatal("上面有 .git 檔（worktree）就算在工作樹裡")
	}
}

// 下載版（<app>/versions/<版本>/packages/official，經 current 這個符號連結找到）就算整個放在別的 git repo 底下
// （例如家目錄是放設定檔的 repo）：也不叫 git、不把整包官方套件當成新增的檔報出來（PKG-21）。
func TestEditedOfficialSkipsDownloadInstallInsideRepo(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, ".evergenie", "app")
	ver := filepath.Join(app, "versions", "0.0.1")
	mkdirs(t, filepath.Join(root, ".git"), filepath.Join(ver, "packages", "official", "alpha"), filepath.Join(root, "home"))
	if err := os.WriteFile(filepath.Join(ver, "VERSION"), []byte("0.0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", "0.0.1"), filepath.Join(app, "current")); err != nil {
		t.Fatal(err)
	}
	mark := fakeGit(t, root)
	official := filepath.Join(app, "current", "packages", "official")
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", official)
	if !inGitTree(official) || !inDownloadInstall(official) {
		t.Fatalf("前提：在 repo 底下（%v）、是下載版（%v）", inGitTree(official), inDownloadInstall(official))
	}
	if got := EditedOfficial(context.Background(), filepath.Join(root, "home")); len(got) != 0 {
		t.Fatalf("下載版不報：%v", got)
	}
	if data, err := os.ReadFile(mark); err == nil {
		t.Fatalf("下載版不該叫 git，卻叫了：%s", data)
	}
	if inDownloadInstall(filepath.Join(root, "elsewhere", "packages", "official")) {
		t.Fatal("不在 versions/ 底下的不算下載版")
	}
}

// 官方資料夾是指進 git 工作樹的符號連結：照樣找得到 .git、照樣報改過的（解開符號連結後再往上找）。
func TestEditedOfficialFollowsSymlinkIntoRepo(t *testing.T) {
	home, official := officialGitHome(t)
	link := filepath.Join(t.TempDir(), "official-link")
	if err := os.Symlink(official, link); err != nil {
		t.Fatal(err)
	}
	if inGitTree(filepath.Dir(link)) {
		t.Skip("暫存資料夾在某個 git repo 底下，驗不了「解開連結才找得到」")
	}
	t.Setenv("EVERGENIE_OFFICIAL_PACKAGES_DIR", link)
	if err := os.WriteFile(filepath.Join(official, "beta", "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := EditedOfficial(context.Background(), home); len(got) != 1 || got[0] != "beta" {
		t.Fatalf("經符號連結也要報改過的官方套件：%v", got)
	}
}
