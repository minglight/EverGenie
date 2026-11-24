package personapack

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// layoutV2 把家改成佈局第 2 版（DEV-145）：home.toml 加 [layout]，官方角色（personas/ 底下 mine/ 與 _ 開頭以外的）搬到狀態資料夾的 personas/。
// 回傳狀態資料夾。
func layoutV2(t *testing.T, home string) string {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(home, "home.toml")
	data, _ := os.ReadFile(path)
	// 第 2 版的家頂層是 schema = 2（舊程式才會拒絕啟動）。
	cfg := regexp.MustCompile(`(?m)^schema\s*=\s*1\s*$`).ReplaceAllString(string(data), "schema = 2")
	if !strings.Contains(cfg, "schema = 2") {
		cfg = "schema = 2\n" + cfg
	}
	cfg += fmt.Sprintf("\n[layout]\nversion = 2\nstate = %q\n", state)
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(home, "personas")
	dst := filepath.Join(state, "personas")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		if e.Name() == "mine" || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		if err := os.Rename(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			t.Fatal(err)
		}
	}
	return state
}

// TestSplitPersonasFollowsLayout：第 1 版全部在 <家>/personas；第 2 版 mine/ 與 _ 開頭的留在家，官方的在狀態資料夾。
func TestSplitPersonasFollowsLayout(t *testing.T) {
	v1 := userHome(t)
	v2 := userHome(t)
	state := layoutV2(t, v2)
	for _, c := range []struct{ home, rel, want string }{
		{v1, "personas/athena.md", filepath.Join(v1, "personas", "athena.md")},
		{v1, "personas/mine/mina.md", filepath.Join(v1, "personas", "mine", "mina.md")},
		{v2, "personas/athena.md", filepath.Join(state, "personas", "athena.md")},
		{v2, "personas/avatars/full/athena.png", filepath.Join(state, "personas", "avatars", "full", "athena.png")},
		{v2, "personas/wardrobe/athena/day.png", filepath.Join(state, "personas", "wardrobe", "athena", "day.png")},
		{v2, "personas/mine/mina.md", filepath.Join(v2, "personas", "mine", "mina.md")},
		{v2, "personas/mine/avatars/mina.png", filepath.Join(v2, "personas", "mine", "avatars", "mina.png")},
		{v2, "personas/_style.md", filepath.Join(v2, "personas", "_style.md")},
		{v2, "settings/x.toml", filepath.Join(v2, "settings", "x.toml")},
	} {
		if got := HomePath(c.home, c.rel); got != c.want {
			t.Errorf("HomePath(%s) = %s，要 %s", c.rel, got, c.want)
		}
	}
}

// TestPackLayoutV2：第 2 版的家建包讀狀態資料夾裡的官方卡（包跟第 1 版一樣）；同步到第 2 版的家，
// 卡、頭像、衣櫃、目錄寫到狀態資料夾的 personas/，家裡的 personas/ 不放官方的東西；暫存與登記檔也在狀態資料夾（同步不寫家裡的檔，DEV-154）。
func TestPackLayoutV2(t *testing.T) {
	pub1 := sourceHome(t)
	r1, _ := buildPack(t, pub1, "2026.10.29")
	pub2 := sourceHome(t)
	layoutV2(t, pub2)
	r2, out := buildPack(t, pub2, "2026.10.29")
	if r1.ManifestSHA256 != r2.ManifestSHA256 || r1.Files != r2.Files {
		t.Fatalf("第 2 版建的包要跟第 1 版一樣：%+v vs %+v", r1, r2)
	}
	ptr := pointer(t, r2, "file://"+out)
	user := userHome(t)
	state := layoutV2(t, user)
	res := mustSync(t, syncOpts(user, ptr))
	if res.Status != StatusSynced || len(res.Installed) != r2.Files {
		t.Fatalf("同步：%+v", res)
	}
	for rel, want := range map[string]string{
		"athena.md":                   "智慧女神 v1",
		"avatars/athena.png":          "PNG-athena-base",
		"avatars/full/athena.png":     "PNG-athena-full",
		"wardrobe/athena/sub/hat.png": "PNG-wardrobe-hat",
		"catalog/athena.md":           "公開版衣服",
	} {
		if got := read(t, filepath.Join(state, "personas", filepath.FromSlash(rel))); !strings.Contains(got, want) {
			t.Errorf("%s：%q", rel, got)
		}
		if exists(filepath.Join(user, "personas", filepath.FromSlash(rel))) {
			t.Errorf("家裡不該有官方的 %s", rel)
		}
	}
	if exists(filepath.Join(user, "settings", "personas-pack.toml")) || !exists(filepath.Join(state, "personas", "pack-registry.toml")) {
		t.Error("官方角色放家外的家，登記檔跟官方角色放一起，不寫家裡的 settings/")
	}
	if exists(filepath.Join(user, "runtime")) {
		t.Error("第 2 版不該在家裡建 runtime/")
	}
	if !hasOfficialCards(user) {
		t.Error("狀態資料夾裡有官方卡")
	}
	if res2 := mustSync(t, syncOpts(user, ptr)); res2.Status != StatusUpToDate {
		t.Fatalf("第二次要已是最新：%+v", res2)
	}
}

// officialStore 是測試用、能給官方層的 Store：家裡的資料與官方層各一份。
type officialStore struct {
	*memStore
	official *memStore
}

func (o officialStore) OfficialStore() Store { return o.official }

// treeSum 回資料夾裡每個檔的相對路徑與內容雜湊（排序過），用來比對「一個檔都沒動」。
func treeSum(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			fmt.Fprintf(&b, "%s/\n", rel)
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s %x\n", rel, sha256.Sum256(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestPackSyncLeavesHomeUntouched（DEV-154 第 1 件，作者定的檢查）：官方角色放家外的家，角色包同步前後（第一次、換新版、
// 帶紀錄）家裡的檔案清單與每個檔的雜湊完全相同；官方的卡、圖、衣服目錄紀錄、登記檔都在家外。主人自己的角色原樣。
// 舊的家（登記檔在 settings/）搬出去之後第一次同步讀得到舊登記，不會把每個檔當成新的。
func TestPackSyncLeavesHomeUntouched(t *testing.T) {
	pub := sourceHome(t)
	out := filepath.Join(t.TempDir(), "pack")
	src := sourceStore(t, pub)
	r1, err := Build(BuildOptions{Home: pub, Out: out, Version: "2026.11.04", Store: src})
	if err != nil {
		t.Fatal(err)
	}
	user := userHome(t)
	state := layoutV2(t, user)
	mine := filepath.Join(user, "personas", "mine", "my_cat.md")
	if err := os.MkdirAll(filepath.Dir(mine), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mine, []byte("---\nschema: 1\nname: \"我的貓\"\n---\n自己做的角色。\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := treeSum(t, user)
	us := officialStore{newStore(closetKinds()), newStore(closetKinds())}
	o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
	o.Store = us
	res := mustSync(t, o)
	if res.Status != StatusSynced || len(res.RecordsAdded) != 3 {
		t.Fatalf("同步：%+v", res)
	}
	if got := treeSum(t, user); got != before {
		t.Fatalf("同步不能動家裡的檔。\n前：\n%s\n後：\n%s", before, got)
	}
	if len(us.memStore.recs) != 0 || len(us.official.recs) != 3 {
		t.Fatalf("衣服目錄只寫官方層：家裡 %d 筆、官方層 %d 筆", len(us.memStore.recs), len(us.official.recs))
	}
	if !exists(filepath.Join(state, "personas", "athena.md")) || !exists(filepath.Join(state, "personas", "pack-registry.toml")) {
		t.Error("官方卡與登記檔要在家外")
	}
	// 換新版再同步：一樣不動家裡。
	write(t, filepath.Join(pub, "personas", "athena.md"), read(t, filepath.Join(pub, "personas", "athena.md"))+"\n新版多一句\n")
	r2, err := Build(BuildOptions{Home: pub, Out: out, Version: "2026.11.05", Store: src})
	if err != nil {
		t.Fatal(err)
	}
	o = syncOpts(user, pointer(t, r2, "file://"+filepath.Dir(filepath.Dir(r2.ManifestPath))))
	o.Store = us
	if res := mustSync(t, o); res.Status != StatusSynced {
		t.Fatalf("新版同步：%+v", res)
	}
	if !strings.Contains(read(t, filepath.Join(state, "personas", "athena.md")), "新版多一句") {
		t.Error("官方卡要換成新版")
	}
	if got := treeSum(t, user); got != before {
		t.Fatalf("新版同步不能動家裡的檔。\n前：\n%s\n後：\n%s", before, got)
	}
	// 指令行程給不出官方層：檔案照換、紀錄留給 serve 補，家裡照樣不動。
	user2 := userHome(t)
	layoutV2(t, user2)
	before2 := treeSum(t, user2)
	o = syncOpts(user2, pointer(t, r2, "file://"+filepath.Dir(filepath.Dir(r2.ManifestPath))))
	plain := newStore(closetKinds())
	o.Store = plain
	_, _ = Sync(context.Background(), o)
	if len(plain.recs) != 0 || treeSum(t, user2) != before2 {
		t.Fatalf("給不出官方層時不能把官方紀錄寫進家裡（寫了 %d 筆）", len(plain.recs))
	}
}

// TestRegistryFallsBackToHome：官方角色剛搬出家的家，登記檔還在 settings/：讀得到，之後寫在家外、家裡那份不動。
func TestRegistryFallsBackToHome(t *testing.T) {
	user := userHome(t)
	state := layoutV2(t, user)
	old := filepath.Join(user, "settings", "personas-pack.toml")
	write(t, old, "schema = 1\nversion = \"2026.10.29\"\n\n[files]\n\"personas/athena.md\" = \"abc\"\n")
	reg, err := LoadRegistry(user)
	if err != nil || reg.Version != "2026.10.29" || reg.Files["personas/athena.md"] != "abc" {
		t.Fatalf("要讀得到 settings/ 那份舊登記：%v %+v", err, reg)
	}
	sum := read(t, old)
	reg.Version = "2026.11.05"
	if err := saveRegistry(user, reg); err != nil {
		t.Fatal(err)
	}
	if read(t, old) != sum || !strings.Contains(read(t, filepath.Join(state, "personas", "pack-registry.toml")), "2026.11.05") {
		t.Error("新登記寫在家外，家裡那份不動")
	}
	if reg2, _ := LoadRegistry(user); reg2.Version != "2026.11.05" {
		t.Errorf("之後讀家外那份：%+v", reg2)
	}
}

// TestDroppedOutfitStays（DEV-154 第 12 件，作者 2026-11-12「已發佈的官方衣服代號不改不刪，官方不再提供時已兌換的照樣留著」）：
// 主人兌換過一件官方衣服、又自己加了衣服與隱藏版；官方新版的角色包拿掉那件衣服。同步之後：那件衣服的目錄紀錄與圖都還在
// （衣櫃、展示間照樣讀得到）、兌換紀錄與自己加的衣服原樣、`personas/mine/` 的檔一個都沒動。兩種佈局都一樣。
func TestDroppedOutfitStays(t *testing.T) {
	for _, outside := range []bool{false, true} {
		name := "官方角色在家裡"
		if outside {
			name = "官方角色在家外"
		}
		t.Run(name, func(t *testing.T) {
			pub := sourceHome(t)
			out := filepath.Join(t.TempDir(), "pack")
			src := sourceStore(t, pub)
			r1, err := Build(BuildOptions{Home: pub, Out: out, Version: "2026.11.04", Store: src})
			if err != nil {
				t.Fatal(err)
			}
			user := userHome(t)
			if outside {
				layoutV2(t, user)
			}
			homeRecs, officialRecs := newStore(closetKinds()), newStore(closetKinds())
			var store Store = homeRecs
			catalog := homeRecs // 官方衣服目錄寫到哪一份
			if outside {
				store, catalog = officialStore{homeRecs, officialRecs}, officialRecs
			}
			o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
			o.Store = store
			if res := mustSync(t, o); res.Status != StatusSynced {
				t.Fatalf("第一次同步：%+v", res)
			}
			// 主人的東西：兌換紀錄、自己加的衣服、自己的圖與隱藏版。
			homeRecs.put(t, "closet.unlock", "athena-seaside", map[string]any{"persona": "athena", "outfit": "seaside"})
			homeRecs.put(t, "closet.outfit", "athena-my-1", outfit("athena", "my-1", 0))
			write(t, filepath.Join(user, "personas", "mine", "avatars", "athena@my-1.png"), "PNG-我自己加的")
			write(t, filepath.Join(user, "personas", "mine", "adult", "athena.png"), "PNG-隱藏版")
			mineBefore := treeSum(t, filepath.Join(user, "personas", "mine"))
			img := HomePath(user, "personas/avatars/athena@seaside.png")
			if read(t, img) != "PNG-athena-seaside" {
				t.Fatal("第一次同步要帶來 seaside 的圖")
			}
			// 官方新版不再提供 seaside：紀錄與圖都從來源拿掉。
			delete(src.recs, "closet.outfit/athena-seaside")
			for _, f := range []string{"athena@seaside.png", "athena@seaside.mp4"} {
				if err := os.Remove(filepath.Join(pub, "personas", "avatars", f)); err != nil {
					t.Fatal(err)
				}
			}
			r2, err := Build(BuildOptions{Home: pub, Out: out, Version: "2026.11.05", Store: src})
			if err != nil {
				t.Fatal(err)
			}
			if keys := recordKeys(loadManifest(t, r2.ManifestPath).Records); strings.Contains(keys, "athena-seaside") {
				t.Fatalf("新版的包不該再帶 seaside：%s", keys)
			}
			o = syncOpts(user, pointer(t, r2, "file://"+filepath.Dir(filepath.Dir(r2.ManifestPath))))
			o.Store = store
			if res := mustSync(t, o); res.Status != StatusSynced && res.Status != StatusUpToDate {
				t.Fatalf("新版同步：%+v", res)
			}
			if r, ok := catalog.recs["closet.outfit/athena-seaside"]; !ok || r.Fields["outfit"] != "seaside" {
				t.Error("官方不再提供的衣服，目錄紀錄要留著（代號不改不刪）")
			}
			if read(t, img) != "PNG-athena-seaside" {
				t.Error("官方不再提供的衣服，圖要留著")
			}
			if _, ok := homeRecs.recs["closet.unlock/athena-seaside"]; !ok {
				t.Error("兌換紀錄是主人的資料，要留著")
			}
			if r, ok := homeRecs.recs["closet.outfit/athena-my-1"]; !ok || r.Fields["outfit"] != "my-1" {
				t.Error("主人自己加的衣服要留著")
			}
			if got := treeSum(t, filepath.Join(user, "personas", "mine")); got != mineBefore {
				t.Errorf("personas/mine/ 的檔不能動。\n前：\n%s\n後：\n%s", mineBefore, got)
			}
		})
	}
}
