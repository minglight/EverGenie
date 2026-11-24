package run

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestPersonaNameBundledCard：官方角色的顯示名走找官方卡的共用入口（sdk.OfficialCardPath，PRS-25）——官方角色資料夾先，
// 沒有才用跟著程式的公開卡（新裝的家還沒同步角色包時只有那張）；再找主人的；都沒有用代號。
func TestPersonaNameBundledCard(t *testing.T) {
	home, official, bundled := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(sdk.EnvOfficialPersonas, official)
	t.Setenv(sdk.EnvBundledPersonas, bundled)
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bundled, "kaguya.md"), "---\nname: 輝夜\n---\n")
	write(filepath.Join(bundled, "athena.md"), "+++\nname = \"程式帶的雅典娜\"\n+++\n")
	write(filepath.Join(official, "athena.md"), "+++\nname = \"角色包的雅典娜\"\n+++\n")
	write(filepath.Join(home, "personas", "mine", "my_pet.md"), "---\nname: 小寵\n---\n")
	s := &Service{Home: home}
	for slug, want := range map[string]string{"kaguya": "輝夜", "athena": "角色包的雅典娜", "my_pet": "小寵", "nobody": "nobody"} {
		if got := s.personaName(slug); got != want {
			t.Fatalf("personaName(%s) = %q，要 %q", slug, got, want)
		}
	}
	t.Setenv(sdk.EnvBundledPersonas, "")
	if got := s.personaName("kaguya"); got != "kaguya" {
		t.Fatalf("沒有跟著程式的資料夾就只看官方角色資料夾：%q", got)
	}
}

// TestPersonaNameMineBeatsBundledCard：主人有同代號的卡（舊的、不是 my_ 開頭）時跟著程式的公開卡不算（agents 的 PRS-27），
// 顯示名用主人的；官方角色資料夾有的照舊官方優先。
func TestPersonaNameMineBeatsBundledCard(t *testing.T) {
	home, official, bundled := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(sdk.EnvOfficialPersonas, official)
	t.Setenv(sdk.EnvBundledPersonas, bundled)
	write := func(p, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(bundled, "venus.md"), "---\nname: 程式帶的維納斯\n---\n")
	write(filepath.Join(bundled, "athena.md"), "---\nname: 程式帶的雅典娜\n---\n")
	write(filepath.Join(official, "athena.md"), "---\nname: 角色包的雅典娜\n---\n")
	write(filepath.Join(home, "personas", "mine", "venus.md"), "---\nname: 我的維納斯\n---\n")
	write(filepath.Join(home, "personas", "mine", "athena.md"), "---\nname: 我的雅典娜\n---\n")
	s := &Service{Home: home}
	for slug, want := range map[string]string{"venus": "我的維納斯", "athena": "角色包的雅典娜"} {
		if got := s.personaName(slug); got != want {
			t.Fatalf("personaName(%s) = %q，要 %q", slug, got, want)
		}
	}
}
