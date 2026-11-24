package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// TestMain 先清掉家的佈局變數：測試可能在秘書的 session 裡跑（核心會帶 EVERGENIE_STATE 等），其他測試都假設第 1 版的位置。
func TestMain(m *testing.M) {
	for _, k := range sdk.EnvPrefixes(sdk.EnvState, sdk.EnvMedia, sdk.EnvOfficialPersonas, sdk.EnvBundledPersonas) {
		os.Unsetenv(k)
	}
	os.Exit(m.Run())
}

// 第 2 版的家（DEV-145）：附件從 <家>/media/share/ 讀（不再是 runtime/share/），官方角色卡在官方角色資料夾找；
// 主人的角色仍寫 <家>/personas/mine/。
func TestLayoutV2ShareAndOfficialCard(t *testing.T) {
	home, state := t.TempDir(), t.TempDir()
	t.Setenv(sdk.EnvState, state)
	t.Setenv(sdk.EnvMedia, filepath.Join(home, "media"))
	t.Setenv(sdk.EnvOfficialPersonas, filepath.Join(state, "personas"))
	s := New(home)
	mustWrite(t, filepath.Join(home, "media", "share", "ok.png"), []byte("v2"))
	mustWrite(t, filepath.Join(home, "runtime", "share", "old.png"), []byte("v1"))
	if data, err := s.ReadUpload("ok.png"); err != nil || string(data) != "v2" {
		t.Fatalf("第 2 版要從 media/share 讀：%q %v", data, err)
	}
	if _, err := s.ReadUpload("old.png"); err == nil {
		t.Fatal("第 2 版不該再讀 runtime/share")
	}
	mustWrite(t, filepath.Join(state, "personas", "athena.md"), []byte("x"))
	mustWrite(t, filepath.Join(home, "personas", "zed.md"), []byte("x"))
	if !s.OfficialCardExists("athena") || s.OfficialCardExists("zed") {
		t.Fatal("官方角色卡要在官方角色資料夾找")
	}
	if got, want := s.mineDir(), filepath.Join(home, "personas", "mine"); got != want {
		t.Fatalf("mineDir = %s，要 %s", got, want)
	}
}

// TestOfficialCardExistsBundled：官方角色卡走找官方卡的共用入口（sdk.OfficialCardPath，PRS-25）——新裝的家官方角色資料夾是空的，
// 跟著程式的公開卡也算官方角色（替她加衣服、新角色不能撞她的代號）；兩邊都沒有才不算。
func TestOfficialCardExistsBundled(t *testing.T) {
	home, official, bundled := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(sdk.EnvOfficialPersonas, official)
	t.Setenv(sdk.EnvBundledPersonas, bundled)
	mustWrite(t, filepath.Join(bundled, "kaguya.md"), []byte("x"))
	mustWrite(t, filepath.Join(official, "athena.md"), []byte("x"))
	s := New(home)
	if !s.OfficialCardExists("kaguya") || !s.OfficialCardExists("athena") || s.OfficialCardExists("zed") {
		t.Fatal("官方角色資料夾與跟著程式的公開卡都要算官方角色")
	}
	t.Setenv(sdk.EnvBundledPersonas, "")
	if s.OfficialCardExists("kaguya") {
		t.Fatal("沒有跟著程式的資料夾就只看官方角色資料夾")
	}
}

// TestOfficialCardExistsMineBeatsBundled：主人自己有同代號的卡（personas/mine/<代號>.md，舊的、不是 my_ 開頭）時，
// 跟著程式的公開卡不算官方角色——這個代號在這個家是主人的（agents 的 PRS-27）；官方角色資料夾有的照舊算官方。
func TestOfficialCardExistsMineBeatsBundled(t *testing.T) {
	home, official, bundled := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv(sdk.EnvOfficialPersonas, official)
	t.Setenv(sdk.EnvBundledPersonas, bundled)
	mustWrite(t, filepath.Join(bundled, "venus.md"), []byte("x"))
	mustWrite(t, filepath.Join(official, "athena.md"), []byte("x"))
	mustWrite(t, filepath.Join(home, "personas", "mine", "venus.md"), []byte("x"))
	mustWrite(t, filepath.Join(home, "personas", "mine", "athena.md"), []byte("x"))
	s := New(home)
	if s.OfficialCardExists("venus") {
		t.Fatal("主人的 venus 蓋過程式帶的那張：不算官方角色")
	}
	if !s.OfficialCardExists("athena") {
		t.Fatal("官方角色資料夾有的照舊算官方角色")
	}
}
