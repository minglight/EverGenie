package personapack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// fakeRepo 建一個只有 packages/official/personas/package.toml 的假 repo。
func fakeRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	write(t, filepath.Join(repo, "packages", "official", "personas", "package.toml"), "schema = 1\nid = \"personas\"\n")
	return repo
}

// TestExportCards：公開卡寫進 repo（PRS-26）：只收 public_personas 列的卡（不收圖、不收不公開的、不收標了隱藏版的），
// 一樣的不動、不再公開的拿掉、資料夾裡其他檔不碰；重跑一次什麼都沒變。
// Guards: PRS-26
func TestExportCards(t *testing.T) {
	home := sourceHome(t) // 公開 athena、venus；kaguya 不公開
	repo := fakeRepo(t)
	dir := BundledCardsDir(repo)
	write(t, filepath.Join(dir, "venus.md"), "---\nschema: 1\nname: Venus\n---\n維納斯\n") // 已經一樣
	write(t, filepath.Join(dir, "luna.md"), "舊的、不再公開")
	write(t, filepath.Join(dir, "README.md"), "說明，不碰")

	res, err := ExportCards(CardsOptions{Home: home, Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Written, ",") != "athena" || strings.Join(res.Unchanged, ",") != "venus" || strings.Join(res.Removed, ",") != "luna" || len(res.Skipped) != 0 {
		t.Fatalf("%+v", res)
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "README.md,athena.md,venus.md" {
		t.Fatalf("repo 的卡：%v", names)
	}
	if read(t, filepath.Join(dir, "athena.md")) != read(t, filepath.Join(home, "personas", "athena.md")) {
		t.Fatal("卡要照原樣")
	}
	res, err = ExportCards(CardsOptions{Home: home, Repo: repo})
	if err != nil || len(res.Written)+len(res.Removed) != 0 || len(res.Unchanged) != 2 {
		t.Fatalf("重跑要什麼都沒變：%+v %v", res, err)
	}

	// 自己標了隱藏版的卡不收（列在 skipped），也從 repo 拿掉
	write(t, filepath.Join(home, "personas", "venus.md"), "---\nschema: 1\nname: Venus\nadult: true\n---\n")
	res, err = ExportCards(CardsOptions{Home: home, Repo: repo})
	if err != nil || len(res.Skipped) != 1 || !strings.Contains(res.Skipped[0], "venus") || strings.Join(res.Removed, ",") != "venus" || exists(filepath.Join(dir, "venus.md")) {
		t.Fatalf("標了隱藏版：%+v %v", res, err)
	}
}

// TestExportCardsRefuses：寫了主人名字的卡整個不寫（repo 一個檔都不動）；不是 EverGenie 的 repo、沒有公開角色、少一張卡都回白話的錯。
// Guards: PRS-26
func TestExportCardsRefuses(t *testing.T) {
	home := sourceHome(t)
	repo := fakeRepo(t)
	write(t, filepath.Join(home, "personas", "venus.md"), "---\nname: Venus\n---\n跟 測試 的關係：朋友\n") // homeToml 的 [user] name = "測試"
	_, err := ExportCards(CardsOptions{Home: home, Repo: repo})
	if platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "主人的名字") || exists(BundledCardsDir(repo)) {
		t.Fatalf("寫了主人名字：%v", err)
	}
	if _, err := Build(BuildOptions{Home: home, Out: t.TempDir()}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "主人的名字") {
		t.Fatalf("建包也要擋：%v", err)
	}

	write(t, filepath.Join(home, "personas", "venus.md"), "---\nname: Venus\n---\n維納斯\n")
	if _, err := ExportCards(CardsOptions{Home: home, Repo: t.TempDir()}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "不是 EverGenie 的 repo") {
		t.Fatalf("不是 repo：%v", err)
	}
	if _, err := ExportCards(CardsOptions{Home: userHome(t), Repo: repo}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "public_personas") {
		t.Fatalf("沒有公開角色：%v", err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.Remove(filepath.Join(home, "personas", "venus.md")))
	if _, err := ExportCards(CardsOptions{Home: home, Repo: repo}); platform.CodeOf(err) != sdk.ErrorCodeInvalid || !strings.Contains(err.Error(), "venus") || exists(BundledCardsDir(repo)) {
		t.Fatalf("少一張卡：%v", err)
	}
}

// TestOwnerNameMatch：英文名字照整個字比、不分大小寫（Amygdala 不算），中文直接找；太短或叫「主人」不查。
// Guards: PRS-26
func TestOwnerNameMatch(t *testing.T) {
	h := &platform.Home{}
	for _, c := range []struct {
		name, text string
		hit        bool
	}{
		{"Amy", "跟 Amy 的關係", true},
		{"Amy", "amy：看板掛了", true},
		{"Amy", "Amygdala 研究", false},
		{"小明", "跟小明的關係", true},
		{"主人", "跟主人的關係", false},
		{"A", "A 計畫", false},
		{"", "什麼都行", false},
	} {
		h.User.Name = c.name
		if err := ownerNameIn(h, "x", []byte(c.text)); (err != nil) != c.hit {
			t.Fatalf("%q in %q：%v", c.name, c.text, err)
		}
	}
}

// TestPackBuildSetPointerWritesCards：發版時 build --set-pointer 也把公開卡寫進 repo，跟這一包的卡同一份（PRS-26）。
// Guards: PRS-26
func TestPackBuildSetPointerWritesCards(t *testing.T) {
	home := sourceHome(t)
	repo := fakeRepo(t)
	r, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.13", PointerRepo: repo, URLBase: "https://example.invalid/pack", Store: newStore(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if r.Cards == nil || strings.Join(r.Cards.Written, ",") != "athena,venus" || !exists(filepath.Join(repo, "packages", "official", "personas", "pack.toml")) {
		t.Fatalf("%+v", r.Cards)
	}
	if read(t, filepath.Join(BundledCardsDir(repo), "athena.md")) != read(t, filepath.Join(home, "personas", "athena.md")) || exists(filepath.Join(BundledCardsDir(repo), "kaguya.md")) {
		t.Fatal("repo 的卡要跟包裡同一份、不收不公開的")
	}
}
