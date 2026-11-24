package sdk

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutFallsBackToHomeTOML(t *testing.T) {
	for _, k := range []string{EnvState, EnvMedia, EnvOfficialPersonas} {
		t.Setenv(k, "")
	}
	home, state := t.TempDir(), t.TempDir()
	cfg := "schema = 1\n[web]\nport = 1\n\n# 家的佈局\n[layout]\nversion = 2\nstate = \"" + state + "\"\n"
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := RuntimeDir(home, "sockets"), filepath.Join(state, "runtime", "sockets"); got != want {
		t.Fatalf("RuntimeDir = %s want %s", got, want)
	}
	if got, want := SharesDir(home), filepath.Join(home, "media", "share"); got != want {
		t.Fatalf("SharesDir = %s want %s", got, want)
	}
	if got, want := OfficialPersonasDir(home), filepath.Join(state, "personas"); got != want {
		t.Fatalf("OfficialPersonasDir = %s want %s", got, want)
	}
	v1 := t.TempDir()
	_ = os.WriteFile(filepath.Join(v1, "home.toml"), []byte("schema = 1\n[layoutx]\nversion = 2\n"), 0o600)
	if got := RuntimeDir(v1); got != filepath.Join(v1, "runtime") {
		t.Fatalf("v1 RuntimeDir = %s", got)
	}
}

func TestLayoutOfficialPersonasAtHome(t *testing.T) {
	for _, k := range []string{EnvState, EnvMedia, EnvOfficialPersonas} {
		t.Setenv(k, "")
	}
	home, state := t.TempDir(), t.TempDir()
	cfg := "schema = 2\n[layout]\nversion = 2\nstate = \"" + state + "\" # 註解\nofficial_personas = \"home\"\n"
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := OfficialPersonasDir(home, "avatars"), filepath.Join(home, "personas", "avatars"); got != want {
		t.Fatalf("發行角色包的家官方角色留在家裡：%s want %s", got, want)
	}
	if got, want := RuntimeDir(home), filepath.Join(state, "runtime"); got != want {
		t.Fatalf("RuntimeDir = %s want %s（state 後面有註解也要認得）", got, want)
	}
}

func TestResolveAPISocket(t *testing.T) {
	v1, v2 := t.TempDir(), t.TempDir()
	state := filepath.Join(t.TempDir(), "st")
	if err := os.WriteFile(filepath.Join(v1, "home.toml"), []byte("schema = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v2, "home.toml"), []byte("schema = 2\n[layout]\nversion = 2\nstate = \""+state+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old1 := filepath.Join(v1, "runtime", "sockets", "api.sock")
	if got := ResolveAPISocket(old1); got != old1 {
		t.Errorf("第 1 版的家照舊：%s", got)
	}
	if got, want := ResolveAPISocket(filepath.Join(v2, "runtime", "sockets", "api.sock")), filepath.Join(state, "runtime", "sockets", "api.sock"); got != want {
		t.Errorf("第 2 版的家要改連 %s，得到 %s", want, got)
	}
	if got := ResolveAPISocket("/tmp/x.sock"); got != "/tmp/x.sock" {
		t.Errorf("不是標準位置要原樣回：%s", got)
	}
}

// TestOfficialCardPath：找官方角色卡的共用入口（PRS-25）——官方角色資料夾先，沒有才用跟著程式的那張（EVERGENIE_BUNDLED_PERSONAS）；
// 跟著程式的要是一般檔；官方資料夾裡有同名的（就算是壞掉的捷徑）不往下找；不合格的代號當找不到。
func TestOfficialCardPath(t *testing.T) {
	home, official, bundled := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(EnvOfficialPersonas, official)
	t.Setenv(EnvBundledPersonas, "")
	write := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("---\nname: x\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bundled, "kaguya.md"))
	write(filepath.Join(bundled, "athena.md"))
	write(filepath.Join(official, "athena.md"))
	if err := os.Mkdir(filepath.Join(bundled, "dir.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "nowhere.md"), filepath.Join(official, "broken.md")); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(bundled, "broken.md"))

	if p, ok := OfficialCardPath(home, "kaguya"); ok || p != filepath.Join(official, "kaguya.md") {
		t.Fatalf("沒傳跟著程式的資料夾＝只看官方資料夾：%s %v", p, ok)
	}
	t.Setenv(EnvBundledPersonas, bundled)
	if BundledPersonasDir() != bundled {
		t.Fatal("BundledPersonasDir 要讀 EVERGENIE_BUNDLED_PERSONAS")
	}
	for _, tc := range []struct {
		slug, want string
		ok         bool
	}{
		{"kaguya", filepath.Join(bundled, "kaguya.md"), true},   // 官方資料夾沒有：用跟著程式的
		{"athena", filepath.Join(official, "athena.md"), true},  // 兩邊都有：官方資料夾的先
		{"broken", filepath.Join(official, "broken.md"), true},  // 官方資料夾有同名的捷徑：不換成跟著程式的
		{"dir", filepath.Join(official, "dir.md"), false},       // 跟著程式的那個不是一般檔
		{"nobody", filepath.Join(official, "nobody.md"), false}, // 都沒有
		{"../kaguya", "", false},
		{".hidden", "", false},
		{"", "", false},
	} {
		p, ok := OfficialCardPath(home, tc.slug)
		if ok != tc.ok || p != tc.want { // 代號不合格回空的位置
			t.Fatalf("%q：%s %v，要 %s %v", tc.slug, p, ok, tc.want, tc.ok)
		}
	}
	if p, ok := OfficialCardIn(official, "", "", "kaguya"); ok || p != filepath.Join(official, "kaguya.md") {
		t.Fatalf("OfficialCardIn 的 bundledDir 空＝不看：%s %v", p, ok)
	}
}

// TestOfficialCardMineBeatsBundled：同代號時的順序是 官方角色資料夾＞主人自己的卡（personas/mine/）＞跟著程式的那張——
// 舊家有不是 my_ 開頭、跟公開角色同代號的我的卡時，換到帶那張公開卡的版本，主人的卡不能被蓋掉（跟著程式的那張不算找到）；
// 官方角色資料夾有的照舊官方優先；mineDir 空＝不看主人的卡；主人資料夾裡同名的是資料夾（不是卡）不算、壞掉的捷徑算有、
// 大小寫不同的檔名不算（macOS 檔名不分大小寫）；OfficialCardPath 自己算 <家>/personas/mine。
func TestOfficialCardMineBeatsBundled(t *testing.T) {
	home, official, bundled := t.TempDir(), t.TempDir(), t.TempDir()
	mine := filepath.Join(home, "personas", "mine")
	write := func(p string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, slug := range []string{"venus", "athena", "kaguya", "luna", "nyx", "selene"} {
		write(filepath.Join(bundled, slug+".md"))
	}
	write(filepath.Join(official, "athena.md"))
	write(filepath.Join(mine, "venus.md"))  // 只有跟著程式的＋我的：用我的
	write(filepath.Join(mine, "athena.md")) // 官方資料夾也有：照舊官方
	if err := os.MkdirAll(filepath.Join(mine, "luna.md"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "不存在.md"), filepath.Join(mine, "nyx.md")); err != nil { // 壞掉的捷徑
		t.Fatal(err)
	}
	write(filepath.Join(mine, "Selene.md")) // 大小寫不同：不是 selene 的卡
	for _, tc := range []struct {
		slug, want string
		ok         bool
	}{
		{"venus", filepath.Join(official, "venus.md"), false},  // 被主人的卡蓋過：不算官方卡
		{"athena", filepath.Join(official, "athena.md"), true}, // 官方資料夾＞我的
		{"kaguya", filepath.Join(bundled, "kaguya.md"), true},  // 主人沒有：用跟著程式的
		{"luna", filepath.Join(bundled, "luna.md"), true},      // 主人那邊同名的是資料夾，不是卡
		{"nyx", filepath.Join(official, "nyx.md"), false},      // 主人那邊是壞掉的捷徑：還是主人的，不悄悄換
		{"selene", filepath.Join(bundled, "selene.md"), true},  // 主人那邊是 Selene.md：不是同一張
	} {
		if p, ok := OfficialCardIn(official, bundled, mine, tc.slug); ok != tc.ok || p != tc.want {
			t.Fatalf("OfficialCardIn(%s)：%s %v，要 %s %v", tc.slug, p, ok, tc.want, tc.ok)
		}
	}
	if p, ok := OfficialCardIn(official, bundled, "", "venus"); !ok || p != filepath.Join(bundled, "venus.md") {
		t.Fatalf("mineDir 空＝不看主人的卡：%s %v", p, ok)
	}
	if !HasMineCard(mine, "venus") || HasMineCard(mine, "kaguya") || HasMineCard(filepath.Join(home, "沒有"), "venus") || HasMineCard("", "venus") {
		t.Fatal("HasMineCard：有的算有、沒有的與不存在的資料夾不算、mineDir 空＝不看")
	}
	t.Setenv(EnvOfficialPersonas, official)
	t.Setenv(EnvBundledPersonas, bundled)
	if p, ok := OfficialCardPath(home, "venus"); ok || p != filepath.Join(official, "venus.md") {
		t.Fatalf("OfficialCardPath 要看 <家>/personas/mine：%s %v", p, ok)
	}
	if p, ok := OfficialCardPath(home, "kaguya"); !ok || p != filepath.Join(bundled, "kaguya.md") {
		t.Fatalf("OfficialCardPath(kaguya)：%s %v", p, ok)
	}
}
