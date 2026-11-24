package run

import (
	"bytes"
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

// newSceneV2：跟 newScene 一樣，但家是第 2 版（DEV-145）——官方角色卡與頭像搬到狀態資料夾的 personas/，
// 暫存在狀態資料夾的 runtime/；主人的角色仍在 <家>/personas/mine/。
func newSceneV2(t *testing.T) (*scene, string) {
	t.Helper()
	state := t.TempDir()
	sc := newScene(t)
	official := filepath.Join(state, "personas")
	if err := os.MkdirAll(official, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"avatars", "builder.md"} {
		if err := os.Rename(filepath.Join(sc.home, "personas", name), filepath.Join(official, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(sdk.EnvState, state)
	t.Setenv(sdk.EnvMedia, filepath.Join(sc.home, "media"))
	t.Setenv(sdk.EnvOfficialPersonas, official)
	return sc, state
}

// 第 2 版：公開資料夾在狀態資料夾的 runtime/，官方頭像與角色卡從官方角色資料夾讀，mine 的還是家裡的 personas/mine/。
func TestLayoutV2PublicAndAvatarDirs(t *testing.T) {
	sc, state := newSceneV2(t)
	if got, want := sc.svc.publicDir(), filepath.Join(state, "runtime", "packages", "closet", "public"); got != want {
		t.Fatalf("publicDir = %s，要 %s", got, want)
	}
	if got, want := sc.svc.ownerRoot("official"), filepath.Join(state, "personas"); got != want {
		t.Fatalf("官方照片的根 = %s，要 %s", got, want)
	}
	if got, want := sc.svc.ownerRoot("mine"), filepath.Join(sc.home, "personas", "mine"); got != want {
		t.Fatalf("mine 照片的根 = %s，要 %s", got, want)
	}
	pngFile(t, filepath.Join(sc.home, "personas", "mine", "avatars", "pet.png"), 7)
	sc.v.seed(KindOutfit, "pet-base", map[string]any{"persona": "pet", "outfit": "base", "name": "原樣", "cost": 0, "state": "active", "source": "mine"})
	sc.round()
	pub := func(rel string) []byte {
		b, _ := os.ReadFile(filepath.Join(state, "runtime", "packages", "closet", "public", filepath.FromSlash(rel)))
		return b
	}
	if !bytes.Equal(pub("o/builder/base/1.png"), sc.img["builder"]) {
		t.Fatal("官方的圖要從狀態資料夾的 personas/avatars 來、寫到狀態資料夾的公開資料夾")
	}
	if len(pub("o/pet/base/1.png")) == 0 {
		t.Fatal("mine 的圖要從 <家>/personas/mine/avatars 來")
	}
	if _, err := os.Stat(filepath.Join(sc.home, "runtime")); !os.IsNotExist(err) {
		t.Fatalf("第 2 版不該在家裡建 runtime/：%v", err)
	}
	if sc.person()["name"] != "Builder" {
		t.Fatalf("官方角色卡要從官方角色資料夾讀名字：%v", sc.person())
	}
	// 官方層在家外（DEV-160 S2）：官方照片的紀錄屬於官方層、跟角色包走，不寫進家裡（舊包沒帶紀錄就在記憶體裡算）；
	// 主人自己的照片照樣寫進家裡。
	for _, r := range sc.v.records(KindPhoto) {
		if r.Fields["source"] == "official" {
			t.Fatalf("官方層在家外，家裡不該有官方照片紀錄：%v", r)
		}
	}
	if pet := sc.fields(KindPhoto, "pet-base--half1"); pet["source"] != "mine" || pet["file"] != "avatars/pet.png" {
		t.Fatalf("主人的照片要寫紀錄：%v", pet)
	}
}

// 第 2 版：隱藏版 dir 是 mine/ 開頭的在 <家>/personas/ 底下找，其他的在官方角色資料夾找（轉換器）。
func TestLayoutV2VariantRoot(t *testing.T) {
	sc, state := newSceneV2(t)
	dir := filepath.Join(state, "personas", "extra", "set")
	hidden := pngFile(t, filepath.Join(dir, "builder.png"), 40)
	sc.variant(map[string]any{"name": "特別版", "dir": "extra/set", "button": true, "on": true})
	sc.round()
	b, _ := os.ReadFile(filepath.Join(state, "runtime", "packages", "closet", "public", "o", "builder", "base", "h-1.png"))
	if !bytes.Equal(b, hidden) {
		t.Fatal("官方的隱藏版要從官方角色資料夾讀")
	}
}
