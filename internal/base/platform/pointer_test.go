package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// 指標檔：home 可以 ~/ 開頭、相對路徑以 checkout 為準；沒有檔回空字串；寫壞或沒寫 home 回錯。
func TestReadPointer(t *testing.T) {
	dir := t.TempDir()
	if got, err := ReadPointer(dir); err != nil || got != "" {
		t.Fatalf("沒有指標檔：%q %v", got, err)
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, PointerFile), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("home = \"~/evergenie-dev\"\n")
	u, _ := os.UserHomeDir()
	if got, err := ReadPointer(dir); err != nil || got != filepath.Join(u, "evergenie-dev") {
		t.Fatalf("~ 要展開：%q %v", got, err)
	}
	write("home = \"../myhome\"\n")
	if got, err := ReadPointer(dir); err != nil || got != filepath.Join(filepath.Dir(dir), "myhome") {
		t.Fatalf("相對路徑以 checkout 為準：%q %v", got, err)
	}
	for _, bad := range []string{"home = [", "other = 1\n"} {
		write(bad)
		if _, err := ReadPointer(dir); err == nil {
			t.Fatalf("%q 應該回錯", bad)
		}
	}
}

// 從執行檔位置找：<checkout>/bin/evergenie → <checkout>/evergenie.local.toml；符號連結先解開。
func TestPointerHomeFromExecutable(t *testing.T) {
	checkout := t.TempDir()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(checkout, "bin", "evergenie")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, PointerFile), []byte("home = \""+home+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "evergenie")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{exe, link} {
		if got, err := PointerHome(p); err != nil || got != home {
			t.Fatalf("%s：%q %v", p, got, err)
		}
	}
	if got, err := PointerHome(""); err != nil || got != "" {
		t.Fatalf("空的執行檔路徑：%q %v", got, err)
	}
}

// 下載版（<安裝資料夾>/versions/<版本>/bin/evergenie，經過 current 或 ~/.local/bin 的連結叫起來）：指標檔在安裝資料夾，
// 不在版本資料夾裡（換版之後還找得到）；版本資料夾裡就算有一份也不看。versions/ 底下卻是 git checkout 的照原始碼版算。
func TestPointerHomeForDownloadInstall(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	app := filepath.Join(root, "app")
	ver := filepath.Join(app, "versions", "1.2.3")
	if err := os.MkdirAll(filepath.Join(ver, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(ver, "bin", "evergenie")
	for path, body := range map[string]string{
		exe:                             "#!/bin/sh\n",
		filepath.Join(ver, "VERSION"):   "1.2.3\n",
		filepath.Join(app, PointerFile): "home = \"~/hg-download\"\n",
		filepath.Join(ver, PointerFile): "home = \"/wrong/inside-version\"\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join("versions", "1.2.3"), filepath.Join(app, "current")); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "localbin", "evergenie")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(filepath.Join(app, "current", "bin", "evergenie"), link); err != nil {
		t.Fatal(err)
	}
	u, _ := os.UserHomeDir()
	for _, p := range []string{exe, filepath.Join(app, "current", "bin", "evergenie"), link} {
		if got := pointerDir(p); got != app {
			t.Fatalf("%s 的指標檔資料夾：%q，應該是安裝資料夾 %q", p, got, app)
		}
		if got, err := PointerHome(p); err != nil || got != filepath.Join(u, "hg-download") {
			t.Fatalf("%s：%q %v", p, got, err)
		}
	}
	// versions/ 底下是 git checkout（有 .git）：照原始碼版，指標檔在 checkout 裡
	if err := os.Mkdir(filepath.Join(ver, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := pointerDir(exe); got != ver {
		t.Fatalf("有 .git 的照原始碼版：%q", got)
	}
}

// 執行檔旁邊的候選資料夾：照原樣的路徑先；經過符號連結叫起來的，再加解開之後的資料夾（下載版的 current、~/.local/bin 的連結）。
func TestExecutableDirs(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	real := filepath.Join(root, "app", "versions", "1.0.0", "bin")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(real, "evergenie")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", "1.0.0"), filepath.Join(root, "app", "current")); err != nil {
		t.Fatal(err)
	}
	viaCurrent := filepath.Join(root, "app", "current", "bin", "evergenie")
	if got := executableDirs(viaCurrent); len(got) != 2 || got[0] != filepath.Join(root, "app", "current", "bin") || got[1] != real {
		t.Fatalf("經過 current：%v", got)
	}
	link := filepath.Join(root, "localbin", "evergenie")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(viaCurrent, link); err != nil {
		t.Fatal(err)
	}
	if got := executableDirs(link); len(got) != 2 || got[0] != filepath.Dir(link) || got[1] != real {
		t.Fatalf("經過另一個連結：%v", got)
	}
	if got := executableDirs(exe); len(got) != 1 || got[0] != real {
		t.Fatalf("沒有連結就只有一個：%v", got)
	}
	if executableDirs("") != nil {
		t.Fatal("空的回 nil")
	}
}
