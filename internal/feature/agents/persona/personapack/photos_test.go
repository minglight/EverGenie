package personapack

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// photoKinds 是 publicOnlyKinds 加上照官方衣櫃宣告的照片紀錄（DEV-160）：file 指著檔（file_field）、隱藏版不跟著走（export_only）、
// 官方角色包只帶官方位置、一般版、已採用的（pack_only），衣服沒進包的照片也不進（pack_parent）。
func photoKinds() []KindInfo {
	str := func(n string, req bool) sdk.DataFieldDecl {
		return sdk.DataFieldDecl{Name: n, Type: "string", Required: req}
	}
	return append(publicOnlyKinds(""), KindInfo{Kind: "closet.photo", Decl: sdk.DataKindDecl{
		Kind: "photo", Title: "照片", IDs: "given", Export: sdk.DataExportByPersona, PersonaField: "persona", PersonaID: sdk.DataPersonaIDPrefix,
		PersonaPrefixed: []string{"outfit", "of"}, PersonaSource: "source",
		PackOnly:   map[string][]string{"source": {"official"}, "hidden": {"false"}, "adopted": {"true"}},
		ExportOnly: map[string][]string{"hidden": {"false"}},
		FileField:  "file", PackParent: &sdk.DataParentDecl{Kind: "outfit", Field: "outfit"},
		Fields: []sdk.DataFieldDecl{
			str("persona", true), str("outfit", true), str("type", true), {Name: "order", Type: "int", Required: true}, str("of", false),
			{Name: "clip", Type: "int"}, {Name: "hidden", Type: "bool", Required: true}, {Name: "adopted", Type: "bool", Required: true},
			{Name: "source", Type: "enum", Enum: []string{"official", "mine"}, Required: true}, str("file", true),
		},
	}})
}

func photo(persona, outfit, file, source string, hidden, adopted bool) map[string]any {
	return map[string]any{"persona": persona, "outfit": outfit, "type": "half", "order": 1, "hidden": hidden, "adopted": adopted, "source": source, "file": file}
}

// photoStore：athena 的官方衣服 base、seaside（上架）、next（設計中）、主人自己的 my-24；每張圖一筆照片紀錄。
// base 的第 2 張檔名跟角色代號無關（avatars/pack/renamed.png）；有一張隱藏版、一張沒採用的；avatars/athena~2.png 沒有紀錄指著。
func photoStore(t *testing.T, home string) *memStore {
	t.Helper()
	av := filepath.Join(home, "personas", "avatars")
	write(t, filepath.Join(av, "pack", "renamed.png"), "PNG-renamed")
	write(t, filepath.Join(av, "athena@next.png"), "PNG-next")
	write(t, filepath.Join(av, "secret", "athena.png"), "SECRET-hidden")
	write(t, filepath.Join(av, "athena@raw.png"), "PNG-not-adopted")
	write(t, filepath.Join(home, "personas", "wardrobe", "athena", "plain.png"), "PNG-hidden-in-wardrobe")
	s := newStore(photoKinds())
	s.put(t, "closet.outfit", "athena-base", outfitOf("athena", "base", "active", "official"))
	s.put(t, "closet.outfit", "athena-seaside", outfitOf("athena", "seaside", "active", "official"))
	s.put(t, "closet.outfit", "athena-next", outfitOf("athena", "next", "draft", "official"))
	s.put(t, "closet.outfit", "athena-my-24", outfitOf("athena", "my-24", "active", "mine"))
	s.put(t, "closet.outfit", "venus-base", outfitOf("venus", "base", "active", "official"))
	s.put(t, "closet.photo", "athena-base--half1", photo("athena", "athena-base", "avatars/athena.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half2", photo("athena", "athena-base", "avatars/pack/renamed.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half3", photo("athena", "athena-base", "avatars/athena@raw.png", "official", false, false))
	s.put(t, "closet.photo", "athena-base--full1", photo("athena", "athena-base", "avatars/full/athena.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half1-h", photo("athena", "athena-base", "avatars/secret/athena.png", "official", true, true))
	// 隱藏版的照片放在會整個帶走的衣服資料夾（wardrobe/<代號>/），名字也看不出來：照紀錄的 hidden 一樣不收。
	s.put(t, "closet.photo", "athena-base--half2-h", photo("athena", "athena-base", "wardrobe/athena/plain.png", "official", true, true))
	s.put(t, "closet.photo", "athena-seaside--half1", photo("athena", "athena-seaside", "avatars/athena@seaside.png", "official", false, true))
	s.put(t, "closet.photo", "athena-seaside--video1-c1", photo("athena", "athena-seaside", "avatars/athena@seaside.mp4", "official", false, true))
	s.put(t, "closet.photo", "athena-next--half1", photo("athena", "athena-next", "avatars/athena@next.png", "official", false, true))
	s.put(t, "closet.photo", "athena-my-24--half1", photo("athena", "athena-my-24", "avatars/athena@my-24.png", "mine", false, true))
	s.put(t, "closet.photo", "venus-base--half1", photo("venus", "venus-base", "avatars/venus.png", "official", false, true))
	return s
}

// 建包照照片紀錄帶圖（DEV-160 S2）：file 指到哪就帶哪個檔（檔名跟角色代號無關也帶），不再照檔名認；
// 衣服沒進包（設計中）的照片連紀錄帶圖不收，隱藏版、沒採用、主人自己的照片紀錄不進包、圖也不收；沒有紀錄指著的圖不收、列出來。
func TestPackBuildCarriesFilesByPhotoRecords(t *testing.T) {
	home := sourceHome(t)
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", Store: photoStore(t, home)})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, res.ManifestPath)
	var avatars []string
	for _, f := range paths(m) {
		if strings.HasPrefix(f, "avatars/") {
			avatars = append(avatars, f)
		}
	}
	sort.Strings(avatars)
	if got := strings.Join(avatars, ","); got != "avatars/athena.png,avatars/athena@seaside.mp4,avatars/athena@seaside.png,avatars/full/athena.png,avatars/pack/renamed.png,avatars/venus.png" {
		t.Fatalf("包裡的圖：%s", got)
	}
	var photos []string
	for _, r := range m.Records {
		if r.Kind == "closet.photo" {
			photos = append(photos, r.ID)
		}
	}
	sort.Strings(photos)
	if got := strings.Join(photos, ","); got != "athena-base--full1,athena-base--half1,athena-base--half2,athena-seaside--half1,athena-seaside--video1-c1,venus-base--half1" {
		t.Fatalf("包裡的照片紀錄：%s", got)
	}
	if got := strings.Join(res.HeldFiles, ","); got != "avatars/athena@next.png,avatars/athena@raw.png" {
		t.Errorf("沒帶的紀錄指著的圖：%s", got)
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"avatars/athena~2.png：沒有紀錄指著這個檔",
		"avatars/athena@next.png：是沒帶的紀錄 closet.photo/athena-next--half1 的圖",
		"closet.photo：2 筆不跟著角色走",
		"athena-base--half1-h",
		"wardrobe/athena/plain.png：是不跟著角色走的紀錄 closet.photo/athena-base--half2-h 的圖，不收",
		"上層 closet.outfit（欄位 outfit）有進包",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
	if !strings.Contains(summary(res), "；3 個圖沒有照片紀錄指著、沒收") {
		t.Errorf("摘要要提醒沒登記的圖：%s", summary(res))
	}
	data := read(t, res.ManifestPath)
	for _, bad := range []string{"SECRET", "secret/athena.png", "athena-next--half1", "athena@my-24", "wardrobe/athena/plain.png"} {
		if strings.Contains(data, bad) {
			t.Errorf("manifest 不該有 %s", bad)
		}
	}
}

// 包格式相容（DEV-160 S2，照 persona-pack.md）：新包多帶的照片紀錄，對還沒有 closet.photo 種類的家（舊版的衣櫃）只是略過、列原因，
// 其他紀錄與檔照常同步，不會整包不收；包的 manifest 沒有新的欄位。
func TestPackSyncWithoutPhotoKindSkipsPhotoRecords(t *testing.T) {
	pub := sourceHome(t)
	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", Store: photoStore(t, pub)})
	if err != nil {
		t.Fatal(err)
	}
	user := userHome(t)
	o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
	us := newStore(publicOnlyKinds("")) // 舊版：衣櫃還沒有照片紀錄這種資料
	o.Store = us
	res := mustSync(t, o)
	if res.Status != StatusSynced {
		t.Fatalf("同步：%+v", res)
	}
	added := append([]string(nil), res.RecordsAdded...)
	sort.Strings(added)
	if got := strings.Join(added, ","); got != "closet.outfit/athena-base,closet.outfit/athena-seaside,closet.outfit/venus-base" {
		t.Fatalf("認得的紀錄照常寫：%s", got)
	}
	skipped := strings.Join(res.RecordsSkipped, "\n")
	if !strings.Contains(skipped, "closet.photo/athena-base--half1：closet 套件沒裝或停用") {
		t.Errorf("不認得的種類要列出略過：%v", res.RecordsSkipped)
	}
	if !exists(filepath.Join(user, "personas", "avatars", "pack", "renamed.png")) {
		t.Error("紀錄指著的檔照常放進官方位置")
	}
}

// 家裡有一筆照片紀錄的檔服務讀不懂（vault 不收、列不出來）：不知道它標著哪些圖不能公開，不建包、說是哪個檔。
func TestPackBuildRefusesWithUnreadablePhotoRecord(t *testing.T) {
	home := sourceHome(t)
	s := photoStore(t, home)
	write(t, filepath.Join(home, "vault", "pkg", "closet", "photo", "athena-seaside--half9.md"), "---\nhidden: [壞掉\n---\n")
	out := filepath.Join(t.TempDir(), "pack")
	_, err := Build(BuildOptions{Home: home, Out: out, Version: "2026.11.15", Store: s})
	if err == nil || !strings.Contains(err.Error(), "vault/pkg/closet/photo/athena-seaside--half9.md") || !strings.Contains(err.Error(), "沒有建包") {
		t.Fatalf("要拒絕並說是哪個檔：%v", err)
	}
	if exists(out) {
		t.Error("拒絕時不寫輸出資料夾")
	}
}

// 同步時家裡同一個位置是主人自己的檔（內容不同、沒登記過）：檔不換（你改過的），包裡新指著它的照片紀錄也不寫，
// 不然那筆官方照片紀錄會指著主人自己的那份；其他紀錄照常。
func TestPackSyncSkipsPhotoRecordsOfKeptFiles(t *testing.T) {
	pub := sourceHome(t)
	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", Store: photoStore(t, pub)})
	if err != nil {
		t.Fatal(err)
	}
	user := userHome(t)
	own := filepath.Join(user, "personas", "avatars", "pack", "renamed.png")
	write(t, own, "主人自己的")
	o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
	o.Store = newStore(photoKinds())
	res := mustSync(t, o)
	if res.Status != StatusSynced {
		t.Fatalf("同步：%+v", res)
	}
	if b, _ := os.ReadFile(own); string(b) != "主人自己的" || !slices.Contains(res.UserEdited, "avatars/pack/renamed.png") {
		t.Fatalf("主人自己的檔不換、列在你改過的：%v", res.UserEdited)
	}
	if slices.Contains(res.RecordsAdded, "closet.photo/athena-base--half2") {
		t.Fatal("指著主人自己那份的照片紀錄不能寫")
	}
	want := "closet.photo/athena-base--half2：指著的 personas/avatars/pack/renamed.png 你改過、沒換，紀錄也不收"
	if !slices.Contains(res.RecordsSkipped, want) {
		t.Fatalf("少了「%s」：%v", want, res.RecordsSkipped)
	}
	if !slices.Contains(res.RecordsAdded, "closet.photo/athena-base--half1") {
		t.Fatalf("其他照片紀錄照常寫：%v", res.RecordsAdded)
	}
}

// 紀錄的 file 是資料、不是保證（DEV-160 S2）：經過符號連結的資料夾、不是圖或影片、指到不公開角色的衣服資料夾的，都不進包、列出來。
func TestPackBuildChecksRecordFiles(t *testing.T) {
	home := sourceHome(t)
	s := photoStore(t, home)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "leak.png"), "OUTSIDE")
	if err := os.Symlink(outside, filepath.Join(home, "personas", "avatars", "link")); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, "personas", "avatars", "notes.txt"), "TEXT")
	write(t, filepath.Join(home, "personas", "wardrobe", "secretive", "a.png"), "PRIVATE")
	s.put(t, "closet.photo", "athena-base--half4", photo("athena", "athena-base", "avatars/link/leak.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half5", photo("athena", "athena-base", "avatars/notes.txt", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half6", photo("athena", "athena-base", "wardrobe/secretive/a.png", "official", false, true))
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", Store: s})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(paths(loadManifest(t, res.ManifestPath)), ",")
	for _, bad := range []string{"avatars/link/leak.png", "avatars/notes.txt", "wardrobe/secretive/a.png"} {
		if strings.Contains(got, bad) {
			t.Errorf("不該進包：%s", bad)
		}
	}
	for _, r := range loadManifest(t, res.ManifestPath).Records {
		switch r.ID {
		case "athena-base--half4", "athena-base--half5", "athena-base--half6":
			t.Errorf("檔沒進包的紀錄也不帶：%s", r.ID)
		}
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"closet.photo/athena-base--half4：檔「avatars/link/leak.png」路上有符號連結",
		"closet.photo/athena-base--half5：檔「avatars/notes.txt」不是圖或影片",
		"closet.photo/athena-base--half6：檔「wardrobe/secretive/a.png」不在 avatars/ 或公開角色的 wardrobe/<代號>/ 底下",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
}

// 紀錄只能讓檔更不帶（DEV-160 審查 M1）：公開角色的一般照片紀錄指著不公開角色的隱藏版照片、檔名是不公開角色的圖、
// 換個大小寫指著隱藏版的照片、或封存了的隱藏版紀錄指著的檔，都不進包；那幾筆紀錄的檔沒帶，紀錄也不帶。
func TestPackBuildSkipsOtherPersonasAndHiddenRecordFiles(t *testing.T) {
	home := sourceHome(t)
	s := photoStore(t, home)
	av := filepath.Join(home, "personas", "avatars")
	write(t, filepath.Join(av, "photos", "kaguya-x.png"), "KAGUYA-HIDDEN")
	write(t, filepath.Join(av, "kaguya@secret.png"), "KAGUYA-PRIVATE")
	write(t, filepath.Join(av, "photos", "old-h.png"), "ARCHIVED-HIDDEN")
	s.put(t, "closet.outfit", "kaguya-base", outfitOf("kaguya", "base", "active", "official"))
	s.put(t, "closet.photo", "kaguya-base--u1", photo("kaguya", "kaguya-base", "avatars/photos/kaguya-x.png", "official", true, true))
	s.put(t, "closet.photo", "athena-base--old-h", photo("athena", "athena-base", "avatars/photos/old-h.png", "official", true, true))
	r := s.recs["closet.photo/athena-base--old-h"]
	r.Archived = true
	s.recs["closet.photo/athena-base--old-h"] = r
	s.put(t, "closet.photo", "athena-base--half7", photo("athena", "athena-base", "avatars/photos/kaguya-x.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half8", photo("athena", "athena-base", "avatars/kaguya@secret.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half9", photo("athena", "athena-base", "avatars/SECRET/athena.png", "official", false, true))
	s.put(t, "closet.photo", "athena-base--half10", photo("athena", "athena-base", "avatars/photos/old-h.png", "official", false, true))
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", Store: s})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, res.ManifestPath)
	got := strings.Join(paths(m), ",")
	for _, bad := range []string{"avatars/photos/kaguya-x.png", "avatars/kaguya@secret.png", "SECRET", "secret", "avatars/photos/old-h.png"} {
		if strings.Contains(got, bad) {
			t.Errorf("不該進包：%s（%s）", bad, got)
		}
	}
	for _, r := range m.Records {
		switch r.ID {
		case "athena-base--half7", "athena-base--half8", "athena-base--half9", "athena-base--half10":
			t.Errorf("檔沒帶的紀錄也不帶：%s", r.ID)
		}
	}
	skipped := strings.Join(res.Skipped, "\n")
	for _, want := range []string{
		"avatars/photos/kaguya-x.png：是別的角色（kaguya）的紀錄 closet.photo/kaguya-base--u1 的圖，不收",
		"avatars/kaguya@secret.png：檔名是別的角色（kaguya）的，不收",
		"avatars/SECRET/athena.png：是不跟著角色走的紀錄 closet.photo/athena-base--half1-h 的圖，不收",
		"avatars/photos/old-h.png：是不跟著角色走的紀錄 closet.photo/athena-base--old-h 的圖，不收",
		"closet.photo/athena-base--half7：指著的 personas/avatars/photos/kaguya-x.png 沒帶",
	} {
		if !strings.Contains(skipped, want) {
			t.Errorf("少了「%s」：\n%s", want, skipped)
		}
	}
}

// --no-records 只代表不帶紀錄（DEV-160 審查 M2）：連得到服務照樣照照片紀錄挑圖——只在紀錄上標成隱藏版（檔沒動）、
// 設計中衣服的、沒採用的照片都不進包；包裡沒有紀錄。
func TestPackNoRecordsStillFollowsPhotoRecords(t *testing.T) {
	home := sourceHome(t)
	s := photoStore(t, home)
	s.put(t, "closet.photo", "athena-seaside--half1", photo("athena", "athena-seaside", "avatars/athena@seaside.png", "official", true, true))
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", NoRecords: true, Store: s})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, res.ManifestPath)
	got := strings.Join(paths(m), ",")
	for _, bad := range []string{"avatars/athena@seaside.png", "avatars/athena@next.png", "avatars/athena@raw.png", "secret/athena.png", "wardrobe/athena/plain.png"} {
		if strings.Contains(got, bad) {
			t.Errorf("--no-records 也照紀錄不收：%s（%s）", bad, got)
		}
	}
	if !strings.Contains(got, "avatars/athena@seaside.mp4") || !strings.Contains(got, "avatars/pack/renamed.png") {
		t.Errorf("紀錄說能公開的照收：%s", got)
	}
	if len(m.Records) != 0 || res.Records != 0 || len(res.RecordsHeld) != 0 {
		t.Errorf("--no-records 不帶紀錄：%+v", m.Records)
	}
}

// 連不到服務的 --no-records（DEV-160 審查 M2）：這個家有照紀錄帶圖的種類（從套件設定讀到）就不建，白話說原因；
// 讀不了套件設定、沒給讀法也不建；照片紀錄那類的套件停用或讀不懂時，連得到服務也不建。
func TestPackNoRecordsRefusesOfflineWithFileKinds(t *testing.T) {
	home := sourceHome(t)
	down := photoStore(t, home)
	down.down = true
	withPhotos := func() ([]string, error) { return []string{"closet.photo"}, nil }
	broken := func() ([]string, error) { return nil, errors.New("closet 的 package.toml 讀不懂") }
	for name, o := range map[string]BuildOptions{
		"沒接、有照片紀錄":  {OfflineFileKinds: withPhotos},
		"連不上、有照片紀錄": {Store: down, OfflineFileKinds: withPhotos},
		"套件設定讀不了":   {OfflineFileKinds: broken},
		"不知道套件設定":   {},
	} {
		out := filepath.Join(t.TempDir(), "pack")
		o.Home, o.Out, o.Version, o.NoRecords = home, out, "2026.11.15", true
		_, err := Build(o)
		if platform.CodeOf(err) != sdk.ErrorCodeUnavailable || !strings.Contains(err.Error(), "沒有建包") || !strings.Contains(err.Error(), "evergenie service start") {
			t.Errorf("%s：%v", name, err)
		}
		if name == "沒接、有照片紀錄" && !strings.Contains(err.Error(), "closet.photo") {
			t.Errorf("要說是哪一種紀錄：%v", err)
		}
		if exists(out) {
			t.Errorf("%s：不建包就什麼都不寫", name)
		}
	}
	off := &unreadableStore{memStore: newStore(publicOnlyKinds("")), bad: []UnreadablePackage{{ID: "closet", Why: "停用中",
		Exports: []string{"closet.outfit", "closet.photo"}, Files: []string{"closet.photo"}}}}
	if _, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "p"), Version: "2026.11.15", NoRecords: true, Store: off}); err == nil ||
		!strings.Contains(err.Error(), "closet（停用中；closet.photo）") {
		t.Errorf("照片紀錄讀不到：--no-records 也不建：%v", err)
	}
	unknown := &unreadableStore{memStore: newStore(nil), bad: []UnreadablePackage{{ID: "mine-looks", Why: "被拒：package.toml 讀不懂", Unknown: true}}}
	if _, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "q"), Version: "2026.11.15", NoRecords: true, Store: unknown}); err == nil ||
		!strings.Contains(err.Error(), "mine-looks") {
		t.Errorf("讀不懂宣告的套件：--no-records 也不建：%v", err)
	}
}

// 沒採用的官方照片不公開（DEV-160 審查 M7，衣櫃的 pack_only 已含 adopted = true）：帶紀錄或 --no-records 都一樣，
// 照片放在會整個帶走的衣服資料夾（wardrobe/<代號>/）裡也不收；它的紀錄不進包。
func TestPackBuildHoldsUnadoptedPhotos(t *testing.T) {
	home := sourceHome(t)
	s := photoStore(t, home)
	write(t, filepath.Join(home, "personas", "wardrobe", "athena", "draft.png"), "PNG-not-adopted-in-wardrobe")
	s.put(t, "closet.photo", "athena-base--half11", photo("athena", "athena-base", "wardrobe/athena/draft.png", "official", false, false))
	for _, noRecords := range []bool{false, true} {
		res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.15", Store: s, NoRecords: noRecords})
		if err != nil {
			t.Fatal(err)
		}
		m := loadManifest(t, res.ManifestPath)
		got := strings.Join(paths(m), ",")
		if strings.Contains(got, "avatars/athena@raw.png") || strings.Contains(got, "wardrobe/athena/draft.png") {
			t.Errorf("no-records=%v：沒採用的照片不收：%s", noRecords, got)
		}
		for _, r := range m.Records {
			if r.ID == "athena-base--half3" || r.ID == "athena-base--half11" {
				t.Errorf("no-records=%v：沒採用的照片紀錄不進包：%s", noRecords, r.ID)
			}
		}
		if got := strings.Join(res.HeldFiles, ","); !strings.Contains(got, "avatars/athena@raw.png") || !strings.Contains(got, "wardrobe/athena/draft.png") {
			t.Errorf("no-records=%v：要列出沒收的圖：%s", noRecords, got)
		}
	}
}

// 會進包的官方衣服一筆照片紀錄都沒有（DEV-160 收尾：剛放好衣服與圖、衣櫃還沒登記就建包）：建包只照照片紀錄帶圖，
// 這幾套的圖一張也帶不走、衣服卻會進包，所以不建、說是哪幾套與怎麼辦，輸出資料夾什麼都不寫。只有封存的照片紀錄也算沒有；
// 沒進包的衣服（設計中、主人自己的）沒有照片紀錄不擋；--no-records 不帶衣服、照建；登記好（有一筆照片紀錄，就算沒採用）就照建。
func TestPackBuildRefusesPackedOutfitWithoutPhotoRecords(t *testing.T) {
	home := sourceHome(t)
	s := photoStore(t, home)
	write(t, filepath.Join(home, "personas", "avatars", "athena@new.png"), "PNG-athena-new")
	s.put(t, "closet.outfit", "athena-new", outfitOf("athena", "new", "active", "official"))
	s.put(t, "closet.outfit", "venus-sail", outfitOf("venus", "sail", "retired", "official"))
	s.put(t, "closet.photo", "venus-sail--half1", photo("venus", "venus-sail", "avatars/venus@sail.png", "official", false, true))
	r := s.recs["closet.photo/venus-sail--half1"]
	r.Archived = true
	s.recs["closet.photo/venus-sail--half1"] = r
	s.put(t, "closet.outfit", "athena-wip", outfitOf("athena", "wip", "draft", "official"))
	s.put(t, "closet.outfit", "athena-my-50", outfitOf("athena", "my-50", "active", "mine"))

	out := filepath.Join(t.TempDir(), "pack")
	_, err := Build(BuildOptions{Home: home, Out: out, Version: "2026.11.15", Store: s})
	if platform.CodeOf(err) != sdk.ErrorCodeConflict {
		t.Fatalf("要拒絕（CONFLICT）：%v", err)
	}
	msg := err.Error()
	for _, want := range []string{"沒有建包", "2 筆會進官方角色包的「衣服」（closet.outfit）還沒有任何一筆「照片」（closet.photo）：athena-new、venus-sail", "evergenie data list closet.photo"} {
		if !strings.Contains(msg, want) {
			t.Errorf("少了「%s」：%s", want, msg)
		}
	}
	for _, bad := range []string{"athena-wip", "athena-my-50", "official_personas"} {
		if strings.Contains(msg, bad) {
			t.Errorf("不該提 %s：%s", bad, msg)
		}
	}
	if exists(out) {
		t.Error("拒絕時不寫輸出資料夾")
	}

	if _, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "p"), Version: "2026.11.15", Store: s, NoRecords: true}); err != nil {
		t.Errorf("--no-records 不帶衣服、照建：%v", err)
	}

	s.put(t, "closet.photo", "athena-new--half1", photo("athena", "athena-new", "avatars/athena@new.png", "official", false, false))
	s.put(t, "closet.photo", "venus-sail--half1-r2", photo("venus", "venus-sail", "avatars/venus@sail.png", "official", false, true))
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "q"), Version: "2026.11.15", Store: s})
	if err != nil {
		t.Fatalf("登記好了就照建：%v", err)
	}
	if !slices.Contains(res.HeldFiles, "avatars/athena@new.png") {
		t.Errorf("沒採用的照片照樣不帶：%v", res.HeldFiles)
	}
}

// 官方角色放在家外的家（第 2 版、沒寫 official_personas = "home"）拒絕時改說：衣櫃不替官方的圖寫照片紀錄、等多久都登記不到，
// 這個家不能當發行角色包的家、到官方角色留在家裡的家建，不叫人去等登記；官方角色在家裡的家照常說等登記好再建。
func TestPackBareRefusalExplainsOfficialOutsideHome(t *testing.T) {
	bare := []BareRecords{{Kind: "closet.outfit", Title: "衣服", Child: "closet.photo", ChildTitle: "照片", IDs: []string{"kaguya-sea"}}}
	for layout, want := range map[string]bool{
		"": false,
		"[layout]\nversion = 2\nstate = \"%s\"\n":                               true,
		"[layout]\nversion = 2\nstate = \"%s\"\nofficial_personas = \"home\"\n": false,
	} {
		home := t.TempDir()
		body := homeToml
		if layout != "" {
			body = "schema = 2\n" + homeToml + strings.Replace(layout, "%s", filepath.Join(t.TempDir(), "state"), 1)
		}
		write(t, filepath.Join(home, "home.toml"), body)
		msg := errBare(home, bare).Error()
		if got := strings.Contains(msg, "這個家不能當發行角色包的家"); got != want {
			t.Errorf("佈局 %q：說不說這個家不能發行要是 %v：%s", layout, want, msg)
		}
		if got := strings.Contains(msg, "evergenie data list closet.photo"); got == want {
			t.Errorf("佈局 %q：叫人等登記要是 %v：%s", layout, !want, msg)
		}
		if !strings.Contains(msg, "kaguya-sea") {
			t.Errorf("要說是哪一套：%s", msg)
		}
	}
}
