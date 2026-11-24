package personapack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 官方角色包帶跟著公開角色走的紀錄（PKG-59、PRS-14～16、計畫 D-E）：建包照套件宣告收、套 export_omit；
// 同步照角色匯入的規則寫（只新增、上次同步後沒人改過的才換、改過的不蓋、沒裝的套件略過、全部先驗再寫），舊格式照舊。

// memStore 是測試用的套件資料：照 Store 介面記在記憶體。
type memStore struct {
	kinds []KindInfo
	recs  map[string]sdk.Record
	puts  int
	down  bool // 模擬連不到核心
}

func newStore(kinds []KindInfo) *memStore {
	return &memStore{kinds: kinds, recs: map[string]sdk.Record{}}
}

func (m *memStore) Kinds(context.Context) ([]KindInfo, error) {
	if m.down {
		return nil, platform.Unavailable("連不到")
	}
	return m.kinds, nil
}
func (m *memStore) List(_ context.Context, kind string) ([]sdk.Record, error) {
	var out []sdk.Record
	for _, r := range m.recs {
		if r.Kind == kind && !r.Archived {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memStore) ListWithArchived(_ context.Context, kind string) ([]sdk.Record, error) {
	var out []sdk.Record
	for _, r := range m.recs {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memStore) Get(_ context.Context, kind, id string) (*sdk.Record, error) {
	r, ok := m.recs[kind+"/"+id]
	if !ok {
		return nil, nil
	}
	return &r, nil
}
func (m *memStore) Put(_ context.Context, rec sdk.Record, rev string) error {
	key := rec.Kind + "/" + rec.ID
	old, ok := m.recs[key]
	if (ok && old.Revision != rev) || (!ok && rev != "") {
		return fmt.Errorf("revision 不符")
	}
	m.puts++
	fields := map[string]any{"schema": float64(1), "id": rec.ID, "by": "system"}
	for k, v := range rec.Fields {
		fields[k] = v
	}
	rec.Fields, rec.Revision = fields, fmt.Sprintf("r%d", m.puts)
	m.recs[key] = rec
	return nil
}

func (m *memStore) put(t *testing.T, kind, id string, fields map[string]any) {
	t.Helper()
	data, _ := json.Marshal(fields) // 走 JSON 一趟：數字變 float64，跟經 API 讀到的一樣
	var f map[string]any
	_ = json.Unmarshal(data, &f)
	if err := m.Put(context.Background(), sdk.Record{Kind: kind, ID: id, Fields: f}, m.recs[kind+"/"+id].Revision); err != nil {
		t.Fatal(err)
	}
}

// closetKinds 照官方衣櫃的宣告：outfit 跟著角色、variant 跟著資料夾（開關 on 不帶）、unlock 不跟。
func closetKinds() []KindInfo {
	str := func(n string, req bool) sdk.DataFieldDecl {
		return sdk.DataFieldDecl{Name: n, Type: "string", Required: req}
	}
	return []KindInfo{
		{Kind: "closet.outfit", Decl: sdk.DataKindDecl{Kind: "outfit", IDs: "given", Export: sdk.DataExportByPersona, PersonaField: "persona", Fields: []sdk.DataFieldDecl{
			str("persona", true), str("outfit", true), str("name", true), {Name: "cost", Type: "int", Required: true},
		}}},
		{Kind: "closet.variant", Decl: sdk.DataKindDecl{Kind: "variant", IDs: "given", Export: sdk.DataExportByFolder, FolderField: "dir", ExportOmit: []string{"on"}, Fields: []sdk.DataFieldDecl{
			str("name", true), str("dir", true), {Name: "on", Type: "bool"}, {Name: "windows", Type: "list"},
		}}},
		{Kind: "closet.unlock", Decl: sdk.DataKindDecl{Kind: "unlock", IDs: "given", Fields: []sdk.DataFieldDecl{str("persona", true), str("outfit", true)}}},
	}
}

func outfit(persona, name string, cost int) map[string]any {
	return map[string]any{"persona": persona, "outfit": name, "name": name, "cost": cost}
}

// sourceStore 是 sourceHome（公開 athena、venus；kaguya 不公開）的衣櫃紀錄，並放隱藏版的圖。
func sourceStore(t *testing.T, home string) *memStore {
	t.Helper()
	av := filepath.Join(home, "personas", "avatars")
	write(t, filepath.Join(av, "hidden", "athena@seaside.png"), "PNG-hidden-athena")
	write(t, filepath.Join(av, "hidden", "full", "athena.png"), "PNG-hidden-athena-full")
	write(t, filepath.Join(av, "hidden", "kaguya.png"), "NOT-PUBLIC-hidden-kaguya")
	write(t, filepath.Join(av, "night", "kaguya.png"), "NOT-PUBLIC-night-kaguya")
	write(t, filepath.Join(av, "level", "x", "athena.png"), "SECRET-variant")
	s := newStore(closetKinds())
	s.put(t, "closet.outfit", "athena-base", outfit("athena", "base", 0))
	s.put(t, "closet.outfit", "athena-seaside", outfit("athena", "seaside", 1))
	s.put(t, "closet.outfit", "venus-base", outfit("venus", "base", 0))
	s.put(t, "closet.outfit", "kaguya-base", outfit("kaguya", "base", 0))
	s.put(t, "closet.variant", "hidden", map[string]any{"name": "隱藏版", "dir": "avatars/hidden", "on": true, "windows": []any{"22:00-02:00"}})
	s.put(t, "closet.variant", "night", map[string]any{"name": "夜", "dir": "avatars/night"})
	s.put(t, "closet.variant", "secret", map[string]any{"name": "不公開", "dir": "avatars/level/x"})
	s.put(t, "closet.unlock", "athena-seaside", map[string]any{"persona": "athena", "outfit": "seaside"})
	return s
}

func recordKeys(recs []Record) string {
	var keys []string
	for _, r := range recs {
		keys = append(keys, r.Key())
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// 建包照宣告收公開角色的紀錄：只收 by_persona 的衣服目錄，而且只有公開角色的；跟著資料夾走的隱藏版（by_folder）連同資料夾裡的圖
// 一律不進官方角色包（列出原因）；兌換紀錄（closet.unlock）不帶；核心管的欄位不帶。（連不到服務就不建，見 TestPackBuildRefusesWithoutService。）
func TestPackBuildCarriesRecords(t *testing.T) {
	home := sourceHome(t)
	store := sourceStore(t, home)
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, res.ManifestPath)
	if got := recordKeys(m.Records); got != "closet.outfit/athena-base,closet.outfit/athena-seaside,closet.outfit/venus-base" {
		t.Fatalf("包裡的紀錄：%s", got)
	}
	if res.Records != 3 || strings.Join(res.RecordKinds, ",") != "closet.outfit" {
		t.Errorf("結果：%+v", res)
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "closet.variant：跟著資料夾走的紀錄") {
		t.Errorf("要列出隱藏版不進包的原因：%v", res.Skipped)
	}
	for _, r := range m.Records {
		for k := range r.Fields {
			if coreFields[k] {
				t.Errorf("%s 不該帶核心管的欄位 %s", r.Key(), k)
			}
		}
	}
	files := strings.Join(paths(m), ",")
	for _, bad := range []string{"hidden", "night", "kaguya", "level/"} {
		if strings.Contains(files, bad) {
			t.Errorf("不該收 %s：%s", bad, files)
		}
	}
	data := read(t, res.ManifestPath)
	for _, bad := range []string{"closet.variant", "kaguya", "SECRET-variant", "22:00-02:00"} {
		if strings.Contains(data, bad) {
			t.Errorf("manifest 不該有 %s：%s", bad, data)
		}
	}
}

// 連不到這個家的服務（沒接、連不上、套件服務還沒準備好）就不建包，白話說要先啟動服務、或加 --no-records；輸出資料夾什麼都沒寫。
// Guards: PRS-4, PRS-14
func TestPackBuildRefusesWithoutService(t *testing.T) {
	home := sourceHome(t)
	down := sourceStore(t, home)
	down.down = true
	notReady := &notReadyStore{}
	for name, store := range map[string]Store{"沒接": nil, "連不上": down, "套件服務還沒準備好": notReady} {
		out := filepath.Join(t.TempDir(), "pack")
		_, err := Build(BuildOptions{Home: home, Out: out, Version: "2026.11.04", Store: store})
		if platform.CodeOf(err) != sdk.ErrorCodeUnavailable || !strings.Contains(err.Error(), "沒有建包") || !strings.Contains(err.Error(), "--no-records") ||
			!strings.Contains(err.Error(), "evergenie service start") {
			t.Fatalf("%s：%v", name, err)
		}
		if name == "套件服務還沒準備好" && !strings.Contains(err.Error(), "還沒準備好（套件服務還沒啟動）") {
			t.Errorf("服務回的原因要帶上：%v", err)
		}
		if exists(out) {
			t.Errorf("%s：不建包就什麼都不寫：%s", name, out)
		}
	}
}

// noFileKinds 是離線時讀套件設定的結果：這個家沒有照紀錄帶圖（宣告 file_field）的種類。
func noFileKinds() ([]string, error) { return nil, nil }

// notReadyStore 模擬服務在跑、但套件服務還沒準備好（經 API 回 UNAVAILABLE）。
type notReadyStore struct{ memStore }

func (*notReadyStore) Kinds(context.Context) ([]KindInfo, error) {
	return nil, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "套件服務還沒啟動"}
}

// --no-records：明說只要圖與卡。連不到服務照樣建、不帶紀錄並說明；不知道哪些資料夾是隱藏版，衣服資料夾 wardrobe/ 整個不收（頂層頭像照收）。
// 連得上服務時一樣不帶紀錄，但照樣排除隱藏版的資料夾、其他衣服照收。
// Guards: PRS-4, PRS-14
func TestPackBuildNoRecordsFilesOnly(t *testing.T) {
	home := sourceHome(t)
	r, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", NoRecords: true, OfflineFileKinds: noFileKinds})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, r.ManifestPath)
	files := strings.Join(paths(m), ",")
	if r.Records != 0 || len(m.Records) != 0 || !strings.Contains(r.RecordsNote, "--no-records") || strings.Contains(files, "wardrobe/") ||
		!strings.Contains(files, "avatars/athena.png") || !strings.Contains(strings.Join(r.Skipped, "\n"), "wardrobe/：沒有連到這個家的服務") {
		t.Fatalf("連不到服務的 --no-records：%s\n%+v", files, r)
	}
	store := sourceStore(t, home)
	store.put(t, "closet.variant", "moon", map[string]any{"name": "月", "dir": "wardrobe/athena/sub"})
	r2, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", NoRecords: true, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	m2 := loadManifest(t, r2.ManifestPath)
	files = strings.Join(paths(m2), ",")
	if len(m2.Records) != 0 || !strings.Contains(files, "wardrobe/athena/day.png") || strings.Contains(files, "wardrobe/athena/sub/") {
		t.Fatalf("連得上服務的 --no-records：%s", files)
	}
}

// 跟著資料夾走（by_folder）的紀錄指的資料夾（衣櫃的隱藏版等）裡的檔一律不進官方角色包，不管名字有沒有成人字樣、在 avatars/ 底下還是
// wardrobe/<角色>/ 底下、大小寫或頭尾斜線怎麼寫、紀錄封存了沒；核心只看宣告，種類不叫 closet 也一樣。建完的總結列出幾位、幾個檔、幾筆、排除了哪些資料夾。
// Guards: PRS-4, PRS-14
func TestPackBuildExcludesFolderRecordDirs(t *testing.T) {
	home := sourceHome(t)
	pe := filepath.Join(home, "personas")
	write(t, filepath.Join(pe, "wardrobe", "athena", "night", "athena@seaside.png"), "HIDDEN-night") // 名字沒有成人字樣
	write(t, filepath.Join(pe, "wardrobe", "athena", "Moon", "a.png"), "HIDDEN-moon")
	write(t, filepath.Join(pe, "wardrobe", "athena", "gone", "a.png"), "HIDDEN-archived")
	write(t, filepath.Join(pe, "wardrobe", "athena", "nightly.png"), "PNG-nightly") // 名字開頭一樣、不在資料夾裡：照收
	kinds := closetKinds()
	kinds = append(kinds, KindInfo{Kind: "dress.look", Decl: sdk.DataKindDecl{Kind: "look", IDs: "given", Export: sdk.DataExportByFolder, FolderField: "where",
		Fields: []sdk.DataFieldDecl{{Name: "where", Type: "string"}}}})
	store := newStore(kinds)
	store.put(t, "closet.outfit", "athena-base", outfit("athena", "base", 0))
	store.put(t, "closet.variant", "hidden", map[string]any{"name": "隱藏版", "dir": "avatars/hidden"})
	store.put(t, "closet.variant", "night", map[string]any{"name": "夜", "dir": "wardrobe/athena/night"})
	store.put(t, "closet.variant", "gone", map[string]any{"name": "舊的", "dir": "wardrobe/athena/gone"})
	g := store.recs["closet.variant/gone"]
	g.Archived = true
	store.recs["closet.variant/gone"] = g
	store.put(t, "dress.look", "moon", map[string]any{"where": "/Wardrobe/Athena/moon/"})
	write(t, filepath.Join(pe, "avatars", "hidden", "athena@seaside.png"), "HIDDEN-avatars-sub")
	r, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, r.ManifestPath)
	files := strings.Join(paths(m), ",")
	for _, bad := range []string{"night/", "Moon/", "gone/", "hidden"} {
		if strings.Contains(files, bad) {
			t.Errorf("隱藏版的資料夾 %s 不能進包：%s", bad, files)
		}
	}
	for _, want := range []string{"wardrobe/athena/day.png", "wardrobe/athena/nightly.png", "avatars/athena.png"} {
		if !strings.Contains(files, want) {
			t.Errorf("該收的 %s 沒收：%s", want, files)
		}
	}
	for _, f := range m.Files {
		if data := read(t, filepath.Join(filepath.Dir(filepath.Dir(r.ManifestPath)), "f", f.SHA256+filepath.Ext(f.Path))); strings.HasPrefix(data, "HIDDEN") {
			t.Errorf("包裡有隱藏版的內容：%s", f.Path)
		}
	}
	want := "avatars/hidden,wardrobe/athena/gone,wardrobe/athena/moon,wardrobe/athena/night"
	if strings.Join(r.ExcludedDirs, ",") != want {
		t.Errorf("排除的資料夾：%v", r.ExcludedDirs)
	}
	if !strings.Contains(strings.Join(r.Skipped, "\n"), "wardrobe/athena/night/athena@seaside.png：在跟著資料夾走的紀錄指的資料夾 wardrobe/athena/night/ 裡") {
		t.Errorf("要列出沒收的原因：%v", r.Skipped)
	}
	if r.Personas != 2 || r.Records != 1 || !strings.Contains(r.Summary, "2 位角色") || !strings.Contains(r.Summary, fmt.Sprintf("%d 個檔案", r.Files)) ||
		!strings.Contains(r.Summary, "1 筆紀錄") || !strings.Contains(r.Summary, "排除的資料夾") || !strings.Contains(r.Summary, "wardrobe/athena/night") {
		t.Errorf("總結：%q", r.Summary)
	}
	// 收進來的子資料夾（DEV-157）：一個一筆、附幾個檔，頂層的 cards/、avatars/ 不列；隱藏版的資料夾不在裡面。
	var dirs []string
	for _, d := range r.IncludedDirs {
		dirs = append(dirs, fmt.Sprintf("%s=%d", d.Dir, d.Files))
		if d.Files < 1 || d.Dir == "cards" || d.Dir == "avatars" || d.Dir == "." {
			t.Errorf("收進來的子資料夾只列有檔的（頂層的 cards/、avatars/ 不列）：%+v", d)
		}
		for _, bad := range r.ExcludedDirs {
			if d.Dir == bad || strings.HasPrefix(d.Dir, bad+"/") {
				t.Errorf("排除的資料夾 %s 不該列在收進來的：%v", bad, r.IncludedDirs)
			}
		}
	}
	if !strings.Contains(strings.Join(dirs, ","), "wardrobe/athena=") || !strings.Contains(r.Summary, fmt.Sprintf("收進來的子資料夾 %d 個", len(r.IncludedDirs))) {
		t.Errorf("收進來的子資料夾：%v；總結：%q", dirs, r.Summary)
	}
	if r0, _ := buildPack(t, sourceHome(t), "2026.11.04"); !strings.Contains(r0.Summary, "沒有要排除的資料夾") {
		t.Errorf("沒有資料夾紀錄時的總結：%q", r0.Summary)
	}
}

// unreadableStore 是有套件停用或被拒的家：Kinds 不列那些套件（跟 sdk store 一樣），Unreadable 列出來。
type unreadableStore struct {
	*memStore
	bad []UnreadablePackage
}

func (s *unreadableStore) Unreadable(context.Context) ([]UnreadablePackage, error) { return s.bad, nil }

// 衣櫃停用中（或被拒）時讀不到它的衣服目錄與隱藏版的資料夾：帶紀錄的包不建，白話說是哪個套件、怎麼辦；
// --no-records 照建，但衣服資料夾整個不收（不知道隱藏版在哪），名字沒有成人字樣的隱藏版圖也不會進包。
// 停用的套件沒有宣告要跟著角色走的資料就不影響建包；讀不懂設定的套件（不知道宣告了什麼）當成有。
// Guards: PRS-4, PRS-14
func TestPackBuildRefusesWhenExportingPackageUnreadable(t *testing.T) {
	home := sourceHome(t)
	pe := filepath.Join(home, "personas")
	write(t, filepath.Join(pe, "wardrobe", "athena", "night", "athena@seaside.png"), "HIDDEN-night")
	closetOff := UnreadablePackage{ID: "closet", Why: "停用中", Exports: []string{"closet.outfit", "closet.variant"}, Folder: true}
	store := &unreadableStore{memStore: newStore(nil), bad: []UnreadablePackage{closetOff}}
	out := filepath.Join(t.TempDir(), "pack")
	_, err := Build(BuildOptions{Home: home, Out: out, Version: "2026.11.04", Store: store})
	if err == nil || !strings.Contains(err.Error(), "closet（停用中；closet.outfit、closet.variant）") || !strings.Contains(err.Error(), "--no-records") {
		t.Fatalf("衣櫃停用時不建包：%v", err)
	}
	if exists(out) {
		t.Error("沒建包就不寫輸出資料夾")
	}
	r, err := Build(BuildOptions{Home: home, Out: out, Version: "2026.11.04", Store: store, NoRecords: true})
	if err != nil {
		t.Fatal(err)
	}
	files := strings.Join(paths(loadManifest(t, r.ManifestPath)), ",")
	if strings.Contains(files, "wardrobe/") || !strings.Contains(files, "avatars/athena.png") {
		t.Errorf("--no-records：衣服資料夾整個不收、頂層頭像照收：%s", files)
	}
	if !strings.Contains(strings.Join(r.Skipped, "\n"), "wardrobe/：套件 closet 讀不到") {
		t.Errorf("要說衣服資料夾為什麼不收：%v", r.Skipped)
	}
	broken := &unreadableStore{memStore: newStore(nil), bad: []UnreadablePackage{{ID: "mine-looks", Why: "被拒：package.toml 讀不懂", Unknown: true}}}
	if _, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "p"), Version: "2026.11.04", Store: broken}); err == nil || !strings.Contains(err.Error(), "mine-looks") {
		t.Fatalf("讀不懂設定的套件也不建：%v", err)
	}
	quiet := &unreadableStore{memStore: newStore(closetKinds()), bad: []UnreadablePackage{{ID: "admin", Why: "停用中"}}}
	quiet.put(t, "closet.outfit", "athena-base", outfit("athena", "base", 0))
	if r, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "q"), Version: "2026.11.04", Store: quiet}); err != nil || r.Records != 1 {
		t.Fatalf("停用的套件沒有要跟著角色走的資料：照建：%v %+v", err, r)
	}
}

// 隱藏版的資料夾是連到 wardrobe/<代號>/ 底下的符號連結時（衣櫃照連結找圖），真正的那個資料夾也不收。
// Guards: PRS-4
func TestPackBuildExcludesSymlinkedFolderRecordDirs(t *testing.T) {
	home := sourceHome(t)
	pe := filepath.Join(home, "personas")
	write(t, filepath.Join(pe, "wardrobe", "athena", "x", "athena@moon.png"), "HIDDEN-real")
	if err := os.Symlink(filepath.Join(pe, "wardrobe", "athena", "x"), filepath.Join(pe, "avatars", "night")); err != nil {
		t.Fatal(err)
	}
	store := newStore(closetKinds())
	store.put(t, "closet.variant", "night", map[string]any{"name": "夜", "dir": "avatars/night"})
	r, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if files := strings.Join(paths(loadManifest(t, r.ManifestPath)), ","); strings.Contains(files, "wardrobe/athena/x/") || !strings.Contains(files, "wardrobe/athena/day.png") {
		t.Errorf("連結指到的資料夾不收、其他衣服照收：%s", files)
	}
	if strings.Join(r.ExcludedDirs, ",") != "avatars/night,wardrobe/athena/x" {
		t.Errorf("排除的資料夾要列出連結指到的位置：%v", r.ExcludedDirs)
	}
}

// 同步到空的家：衣服目錄寫進去（經 Store 的正常寫入）、登記每筆的雜湊；沒有隱藏版的紀錄與圖；再同步一次已是最新、不再寫。
func TestPackSyncWritesRecords(t *testing.T) {
	pub := sourceHome(t)
	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: sourceStore(t, pub)})
	if err != nil {
		t.Fatal(err)
	}
	ptr := pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath)))
	user := userHome(t)
	us := newStore(closetKinds())
	o := syncOpts(user, ptr)
	o.Store = us
	res := mustSync(t, o)
	if res.Status != StatusSynced || strings.Join(res.RecordsAdded, ",") != "closet.outfit/athena-base,closet.outfit/athena-seaside,closet.outfit/venus-base" {
		t.Fatalf("同步：%+v", res)
	}
	if r := us.recs["closet.outfit/athena-seaside"]; r.Fields["cost"] != float64(1) || r.Fields["persona"] != "athena" {
		t.Errorf("衣服目錄：%+v", r.Fields)
	}
	if _, ok := us.recs["closet.variant/hidden"]; ok || exists(filepath.Join(user, "personas", "avatars", "hidden")) {
		t.Error("隱藏版的紀錄與圖不跟著官方角色包來")
	}
	reg, err := LoadRegistry(user)
	if err != nil || len(reg.Records) != 3 || reg.Records["closet.outfit/athena-base"] == "" {
		t.Fatalf("登記紀錄：%v %+v", err, reg.Records)
	}
	if !strings.Contains(res.Message(), "紀錄新增 3 筆") {
		t.Errorf("摘要：%s", res.Message())
	}
	puts := us.puts
	if r := mustSync(t, o); r.Status != StatusUpToDate || us.puts != puts {
		t.Fatalf("再同步一次：%+v（寫了 %d 次）", r, us.puts-puts)
	}
}

// 新版的包：上次同步之後沒人改過的紀錄換新的；你改過的不蓋（--force 才蓋，先備份）。
func TestPackSyncKeepsEditedRecords(t *testing.T) {
	pub := sourceHome(t)
	src := sourceStore(t, pub)
	out := filepath.Join(t.TempDir(), "pack")
	r1, err := Build(BuildOptions{Home: pub, Out: out, Version: "2026.11.04", Store: src})
	if err != nil {
		t.Fatal(err)
	}
	user := userHome(t)
	us := newStore(closetKinds())
	o := syncOpts(user, pointer(t, r1, "file://"+out))
	o.Store = us
	mustSync(t, o)

	// 發行者改了兩筆衣服；使用者自己改了其中一筆。
	src.put(t, "closet.outfit", "athena-base", outfit("athena", "base", 5))
	src.put(t, "closet.outfit", "athena-seaside", outfit("athena", "seaside", 9))
	us.put(t, "closet.outfit", "athena-seaside", map[string]any{"persona": "athena", "outfit": "seaside", "name": "我改的", "cost": 1})
	r2, err := Build(BuildOptions{Home: pub, Out: out, Version: "2026.11.05", Store: src})
	if err != nil {
		t.Fatal(err)
	}
	o.PointerPath = pointer(t, r2, "file://"+out)
	res := mustSync(t, o)
	if strings.Join(res.RecordsKept, ",") != "closet.outfit/athena-seaside" || strings.Join(res.RecordsReplaced, ",") != "closet.outfit/athena-base" {
		t.Fatalf("改過的不蓋、沒改的換：沒換 %v、換 %v", res.RecordsKept, res.RecordsReplaced)
	}
	if us.recs["closet.outfit/athena-seaside"].Fields["name"] != "我改的" || us.recs["closet.outfit/athena-base"].Fields["cost"] != float64(5) {
		t.Fatalf("內容：%v %v", us.recs["closet.outfit/athena-seaside"].Fields, us.recs["closet.outfit/athena-base"].Fields)
	}
	if !strings.Contains(res.Message(), "你改過、沒換的 1 個") {
		t.Errorf("摘要：%s", res.Message())
	}
	// --force（同版號也往下走）：蓋掉改過的那筆，先備份成 JSON。
	o.Force = true
	res = mustSync(t, o)
	if us.recs["closet.outfit/athena-seaside"].Fields["cost"] != float64(9) || res.Backup == "" {
		t.Fatalf("--force 要蓋：%v（備份 %q）", us.recs["closet.outfit/athena-seaside"].Fields, res.Backup)
	}
	var old sdk.Record
	if err := json.Unmarshal([]byte(read(t, filepath.Join(user, filepath.FromSlash(res.Backup), "records", "closet.outfit", "athena-seaside.json"))), &old); err != nil || old.Fields["name"] != "我改的" {
		t.Fatalf("備份：%+v %v", old, err)
	}
}

// 舊格式（沒有 records 的包）照舊同步，不用連核心；沒裝衣櫃的家：檔案照常、紀錄略過並列出；
// 包裡有紀錄但連不到核心：檔案照樣換、紀錄這次不寫、版號不登記、記下待補，新角色照樣回報；連得到之後再同步一次補上紀錄。
func TestPackSyncOldFormatAndNoCloset(t *testing.T) {
	pub := sourceHome(t)
	old, oldOut := buildPack(t, pub, "2026.10.29") // 沒給 Store＝舊格式
	if strings.Contains(read(t, old.ManifestPath), "\"records\"") {
		t.Fatal("沒給 Store 的包不寫 records")
	}
	user := userHome(t)
	if r := mustSync(t, syncOpts(user, pointer(t, old, "file://"+oldOut))); r.Status != StatusSynced || len(r.RecordsAdded)+len(r.RecordsSkipped) != 0 {
		t.Fatalf("舊格式：%+v", r)
	}

	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: sourceStore(t, pub)})
	if err != nil {
		t.Fatal(err)
	}
	ptr := pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath)))

	// 包裡有紀錄、沒有 Store（連不到核心）：檔案換進來、紀錄待補。
	down := userHome(t)
	write(t, filepath.Join(down, "personas", "helper.md"), "---\nname: 小幫手\n---\n") // 已經在用的家：這次換進來的卡算新角色
	res, err := Sync(context.Background(), syncOpts(down, ptr))
	if err == nil || res.Status != StatusFailed || !res.RecordsLater || !exists(filepath.Join(down, "personas", "athena.md")) || ReadPending(down) == nil ||
		!strings.Contains(res.Message(), "檔案換進來了") || !strings.Contains(res.Reason, "3 筆紀錄這次沒寫") || strings.Join(res.NewCards, ",") != "athena,venus" {
		t.Fatalf("連不到核心：檔案照換、紀錄待補：%v %+v", err, res)
	}
	if reg, _ := LoadRegistry(down); reg.Version != "" || len(reg.Files) == 0 {
		t.Fatalf("版號不登記、檔案要登記：%+v", reg)
	}
	ds := newStore(closetKinds())
	o0 := syncOpts(down, ptr)
	o0.Store = ds
	if r := mustSync(t, o0); r.Status != StatusSynced || r.RecordsLater || len(r.RecordsAdded) != 3 || len(r.Installed) != 0 || ReadPending(down) != nil {
		t.Fatalf("連得到之後補上紀錄：%+v", r)
	}
	// 套件服務還沒準備好（UNAVAILABLE）也一樣待補，不會把紀錄當成「沒裝」略過。
	late := userHome(t)
	ls := newStore(closetKinds())
	ls.down = true
	o1 := syncOpts(late, ptr)
	o1.Store = ls
	if r, err := Sync(context.Background(), o1); err == nil || !r.RecordsLater || len(r.RecordsSkipped) != 0 || !exists(filepath.Join(late, "personas", "athena.md")) {
		t.Fatalf("還沒準備好：%v %+v", err, r)
	}

	// 沒裝衣櫃：檔案照常，紀錄略過並列出。
	bare := userHome(t)
	o := syncOpts(bare, ptr)
	o.Store = newStore(nil)
	res = mustSync(t, o)
	if res.Status != StatusSynced || len(res.RecordsSkipped) != 3 || !strings.Contains(res.RecordsSkipped[0], "closet 套件沒裝或停用") || !exists(filepath.Join(bare, "personas", "athena.md")) {
		t.Fatalf("沒裝衣櫃：%+v", res)
	}
}

// 包裡帶了跟著資料夾走的紀錄（例如舊版做出來的包）：同步略過並列原因，其他照常。
func TestPackSyncSkipsFolderRecords(t *testing.T) {
	pub := sourceHome(t)
	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: sourceStore(t, pub)})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, r1.ManifestPath)
	m.Records = append(m.Records, Record{Kind: "closet.variant", ID: "hidden", Fields: map[string]any{"name": "隱藏版", "dir": "avatars", "windows": []any{"22:00-02:00"}}})
	data, _ := json.MarshalIndent(&m, "", "  ")
	if err := os.WriteFile(r1.ManifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	r1.ManifestSHA256 = SumBytes(data)
	user := userHome(t)
	o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
	us := newStore(closetKinds())
	o.Store = us
	res := mustSync(t, o)
	if len(res.RecordsAdded) != 3 || len(res.RecordsSkipped) != 1 || !strings.Contains(res.RecordsSkipped[0], "closet.variant/hidden：跟著資料夾走的紀錄不從官方角色包收") {
		t.Fatalf("略過跟著資料夾走的紀錄：%+v", res)
	}
	if _, ok := us.recs["closet.variant/hidden"]; ok {
		t.Fatal("不該寫隱藏版")
	}
}

// 包裡的紀錄不屬於包裡的角色（或欄位不合這個家的規格）：整包不換、檔案一個都不寫。
func TestPackSyncBadRecordChangesNothing(t *testing.T) {
	pub := sourceHome(t)
	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.04", Store: sourceStore(t, pub)})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, r1.ManifestPath)
	m.Records = append(m.Records, Record{Kind: "closet.outfit", ID: "kaguya-base", Fields: map[string]any{"persona": "kaguya", "outfit": "base", "name": "x", "cost": float64(0)}})
	data, _ := json.MarshalIndent(&m, "", "  ")
	if err := os.WriteFile(r1.ManifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	r1.ManifestSHA256 = SumBytes(data)
	user := userHome(t)
	o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
	us := newStore(closetKinds())
	o.Store = us
	res, err := Sync(context.Background(), o)
	if err == nil || res.Status != StatusFailed || !strings.Contains(res.Reason, "不屬於") || exists(filepath.Join(user, "personas", "athena.md")) || us.puts != 0 {
		t.Fatalf("不屬於包裡角色的紀錄要整包不換：%v %+v", err, res)
	}
}

// publicOnlyKinds 照官方衣櫃現在的宣告（PKG-68）：outfit 跟著角色，官方角色包只帶 source 是 official、state 是 active 或 retired 的；
// look 是另一種跟著角色走、沒寫 pack_only 的資料（行為照舊：公開角色的每一筆都帶）。
func publicOnlyKinds(note string) []KindInfo {
	str := func(n string, req bool) sdk.DataFieldDecl {
		return sdk.DataFieldDecl{Name: n, Type: "string", Required: req}
	}
	return []KindInfo{
		{Kind: "closet.outfit", Decl: sdk.DataKindDecl{Kind: "outfit", Title: "衣服", IDs: "given", Export: sdk.DataExportByPersona, PersonaField: "persona", PersonaID: sdk.DataPersonaIDPrefix,
			PackOnly: map[string][]string{"source": {"official"}, "state": {"active", "retired"}}, PackOnlyNote: note,
			Fields: []sdk.DataFieldDecl{
				str("persona", true), str("outfit", true), str("name", true), {Name: "cost", Type: "int", Required: true},
				{Name: "state", Type: "enum", Enum: []string{"active", "draft", "retired"}, Required: true},
				{Name: "source", Type: "enum", Enum: []string{"official", "mine"}, Required: true},
			}}},
		{Kind: "notes.look", Decl: sdk.DataKindDecl{Kind: "look", Title: "造型筆記", IDs: "given", Export: sdk.DataExportByPersona, PersonaField: "persona", Fields: []sdk.DataFieldDecl{
			str("persona", true), str("source", false), str("state", false),
		}}},
	}
}

func outfitOf(persona, name, state, source string) map[string]any {
	return map[string]any{"persona": persona, "outfit": name, "name": name, "cost": 0, "state": state, "source": source}
}

// publicOnlyStore：公開角色 athena 有官方的三套（已採用、下架、設計中）、主人自己的兩套（已採用、設計中）；kaguya 不公開。
func publicOnlyStore(t *testing.T, note string) *memStore {
	t.Helper()
	s := newStore(publicOnlyKinds(note))
	s.put(t, "closet.outfit", "athena-base", outfitOf("athena", "base", "active", "official"))
	s.put(t, "closet.outfit", "athena-old", outfitOf("athena", "old", "retired", "official"))
	s.put(t, "closet.outfit", "athena-next", outfitOf("athena", "next", "draft", "official"))
	s.put(t, "closet.outfit", "athena-my-24", outfitOf("athena", "my-24", "active", "mine"))
	s.put(t, "closet.outfit", "athena-my-38", outfitOf("athena", "my-38", "draft", "mine"))
	s.put(t, "closet.outfit", "kaguya-my-1", outfitOf("kaguya", "my-1", "active", "mine"))
	s.put(t, "notes.look", "athena-a", map[string]any{"persona": "athena", "source": "mine", "state": "draft"})
	return s
}

// 官方角色包只帶套件宣告可以公開的紀錄（PKG-68、PRS-29；作者 2026-11-14：採用只代表主人要穿，升成官方的才可以公開）：
// 主人自己的衣服（source = mine）與設計中的（state = draft）不進包，連名稱都不在 manifest 裡；結果的 RecordsHeld、Skipped 與總結那一行
// 用套件寫的白話說幾筆沒帶、列編號。沒宣告 pack_only 的種類照舊每一筆都帶，欄位剛好叫 source／state 也一樣（核心不認得欄位的意思）。
func TestPackBuildHoldsRecordsNotDeclaredPublic(t *testing.T) {
	home := sourceHome(t)
	// 沒帶的紀錄的圖也不收：athena-next 是官方來源、還在設計中，圖已經放在官方位置。檔名只是開頭像的（athena@nextgen）不是它的圖，照收。
	av := filepath.Join(home, "personas", "avatars")
	for _, f := range []string{"athena@next.png", "athena@next~2.png", "athena@next+1.mp4", "full/athena@next.png", "athena@nextgen.png"} {
		write(t, filepath.Join(av, filepath.FromSlash(f)), "PNG-"+f)
	}
	write(t, filepath.Join(home, "personas", "wardrobe", "athena", "next", "1.png"), "PNG-wardrobe-next")
	res, err := Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.14", Store: publicOnlyStore(t, "還沒升成官方或還在設計中的衣服")})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, res.ManifestPath)
	files := paths(m)
	sort.Strings(files)
	if got := strings.Join(res.HeldFiles, ","); got != "avatars/athena@next+1.mp4,avatars/athena@next.png,avatars/athena@next~2.png,avatars/full/athena@next.png,wardrobe/athena/next/1.png" {
		t.Errorf("沒帶的紀錄的圖也不收：%s", got)
	}
	for _, f := range files {
		if strings.Contains(f, "@next.") || strings.Contains(f, "@next~") || strings.Contains(f, "@next+") || strings.HasPrefix(f, "wardrobe/athena/next/") {
			t.Errorf("沒帶的紀錄的圖不能在包裡：%s", f)
		}
	}
	if !strings.Contains(strings.Join(files, ","), "avatars/athena@nextgen.png") {
		t.Errorf("只是檔名開頭像的照收：%v", files)
	}
	if !strings.Contains(strings.Join(res.Skipped, "\n"), "avatars/athena@next.png：是沒帶的紀錄 closet.outfit/athena-next 的圖，不進官方角色包") || !strings.Contains(res.Summary, "（它們的圖 5 個檔也沒收）") {
		t.Errorf("要說圖為什麼沒收：%v\n%s", res.Skipped, res.Summary)
	}
	if got := recordKeys(m.Records); got != "closet.outfit/athena-base,closet.outfit/athena-old,notes.look/athena-a" {
		t.Fatalf("包裡的紀錄：%s", got)
	}
	data := read(t, res.ManifestPath)
	for _, bad := range []string{"my-24", "my-38", "athena-next", "@next.", "kaguya"} {
		if strings.Contains(data, bad) {
			t.Errorf("manifest 不該有 %s：%s", bad, data)
		}
	}
	if len(res.RecordsHeld) != 1 || res.RecordsHeld[0].Kind != "closet.outfit" || strings.Join(res.RecordsHeld[0].IDs, ",") != "athena-my-24,athena-my-38,athena-next" {
		t.Fatalf("沒帶的要列出來（不公開的角色 kaguya 的不算）：%+v", res.RecordsHeld)
	}
	want := "closet.outfit：3 筆還沒升成官方或還在設計中的衣服沒帶（套件宣告官方角色包只帶 source 是 official、state 是 active／retired 的）：athena-my-24、athena-my-38、athena-next"
	if !strings.Contains(strings.Join(res.Skipped, "\n"), want) {
		t.Errorf("skipped 要白話說幾筆沒帶：%v", res.Skipped)
	}
	if !strings.Contains(res.Summary, "3 筆紀錄（3 筆還沒升成官方或還在設計中的衣服沒帶：athena-my-24、athena-my-38、athena-next）") {
		t.Errorf("總結那一行要說幾筆沒帶：%s", res.Summary)
	}

	// 套件沒寫白話說明：用種類的名稱說。
	res, err = Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.14", Store: publicOnlyStore(t, "")})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Summary, "（3 筆不在公開條件裡的「衣服」沒帶：") {
		t.Errorf("沒有白話說明時的總結：%s", res.Summary)
	}

	// 全部都可以公開：沒有 RecordsHeld，總結照舊。
	s := newStore(publicOnlyKinds(""))
	s.put(t, "closet.outfit", "athena-base", outfitOf("athena", "base", "active", "official"))
	res, err = Build(BuildOptions{Home: home, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.14", Store: s})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RecordsHeld) != 0 || !strings.Contains(res.Summary, "1 筆紀錄；") {
		t.Errorf("都可以公開時：%+v", res)
	}
}

// PackAllows 只比宣告寫的欄位與值：每個列出的欄位都要符合；少了欄位、值不是字串都算不可以（判斷不了就不公開）；沒宣告就都可以。
func TestPackAllows(t *testing.T) {
	decl := publicOnlyKinds("")[0].Decl
	cases := []struct {
		fields map[string]any
		want   bool
	}{
		{map[string]any{"source": "official", "state": "active"}, true},
		{map[string]any{"source": "official", "state": "retired"}, true},
		{map[string]any{"source": "mine", "state": "active"}, false},
		{map[string]any{"source": "official", "state": "draft"}, false},
		{map[string]any{"source": "official"}, false},
		{map[string]any{"source": "official", "state": 1.0}, false},
		{map[string]any{"source": "official", "state": "review"}, false},
		{map[string]any{}, false},
	}
	for _, c := range cases {
		if got := PackAllows(decl, c.fields); got != c.want {
			t.Errorf("%v：%v，要 %v", c.fields, got, c.want)
		}
	}
	if !PackAllows(sdk.DataKindDecl{}, map[string]any{"source": "mine"}) {
		t.Error("沒宣告 pack_only 就都可以")
	}
}

// bool 欄位的 pack_only（DEV-160：closet.photo 的 hidden = false 才進官方角色包）：bool 值轉成 "true"／"false" 再比；少了欄位、值不在清單裡
// （例如 "maybe"）不算。字串照原樣比，所以字串 "false" 跟 bool false 一樣算（欄位型別由 vault 的寫入驗，這裡不分）。
func TestPackAllowsBoolField(t *testing.T) {
	decl := sdk.DataKindDecl{PackOnly: map[string][]string{"source": {"official"}, "hidden": {"false"}}}
	cases := []struct {
		fields map[string]any
		want   bool
	}{
		{map[string]any{"source": "official", "hidden": false}, true},
		{map[string]any{"source": "official", "hidden": true}, false},
		{map[string]any{"source": "official"}, false},
		{map[string]any{"source": "official", "hidden": "maybe"}, false},
		{map[string]any{"source": "official", "hidden": "false"}, true},
		{map[string]any{"source": "mine", "hidden": false}, false},
	}
	for _, c := range cases {
		if got := PackAllows(decl, c.fields); got != c.want {
			t.Errorf("%v：%v，要 %v", c.fields, got, c.want)
		}
	}
}

// 同步端也防一手（PKG-68、PRS-29）：包裡萬一帶了不在這個家宣告的公開條件裡的紀錄（舊版程式建的包、別人做的包），略過並列原因，其他照常寫；
// 這個家的套件沒宣告 pack_only 的種類照舊收。
func TestPackSyncSkipsRecordsNotDeclaredPublic(t *testing.T) {
	pub := sourceHome(t)
	r1, err := Build(BuildOptions{Home: pub, Out: filepath.Join(t.TempDir(), "pack"), Version: "2026.11.14", Store: publicOnlyStore(t, "")})
	if err != nil {
		t.Fatal(err)
	}
	m := loadManifest(t, r1.ManifestPath)
	// athena-odd 不在公開條件裡、欄位也不合這個家的規格：照樣只是略過，不會讓整包不換（先看公開條件、再驗欄位）。
	odd := outfitOf("athena", "odd", "draft", "mine")
	odd["nobody_knows"] = "x"
	for id, f := range map[string]map[string]any{"athena-my-24": outfitOf("athena", "my-24", "active", "mine"), "athena-next": outfitOf("athena", "next", "draft", "official"), "athena-odd": odd} {
		f["cost"] = float64(0)
		m.Records = append(m.Records, Record{Kind: "closet.outfit", ID: id, Fields: f})
	}
	data, _ := json.MarshalIndent(&m, "", "  ")
	if err := os.WriteFile(r1.ManifestPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	r1.ManifestSHA256 = SumBytes(data)
	user := userHome(t)
	o := syncOpts(user, pointer(t, r1, "file://"+filepath.Dir(filepath.Dir(r1.ManifestPath))))
	us := newStore(publicOnlyKinds(""))
	o.Store = us
	res := mustSync(t, o)
	added := append([]string(nil), res.RecordsAdded...)
	sort.Strings(added)
	if strings.Join(added, ",") != "closet.outfit/athena-base,closet.outfit/athena-old,notes.look/athena-a" {
		t.Fatalf("可以公開的照常寫：%+v", res)
	}
	skipped := strings.Join(res.RecordsSkipped, "\n")
	for _, want := range []string{"closet.outfit/athena-my-24：不在公開條件裡的「衣服」，這個家的 closet 套件說不從官方角色包收", "closet.outfit/athena-next：", "closet.outfit/athena-odd："} {
		if !strings.Contains(skipped, want) {
			t.Errorf("要列出略過的「%s」：%v", want, res.RecordsSkipped)
		}
	}
	for _, key := range []string{"closet.outfit/athena-my-24", "closet.outfit/athena-next", "closet.outfit/athena-odd"} {
		if _, ok := us.recs[key]; ok {
			t.Errorf("不該寫 %s", key)
		}
	}
}
