package photomigrate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
	"github.com/minglight/evergenie/sdk"
)

// home 是測試用的兩個擁有者位置：官方角色資料夾與 <家>/personas/mine。
type home struct{ official, mine string }

func newHome(t *testing.T) home {
	t.Helper()
	d := t.TempDir()
	return home{official: filepath.Join(d, "official"), mine: filepath.Join(d, "home", "personas", "mine")}
}

func touch(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func outfit(persona, id, state, source string, adopted ...string) rules.Outfit {
	return rules.Outfit{Record: rules.RecordID(persona, id), Persona: persona, Outfit: id, Name: id, State: state, Source: source, Adopted: adopted}
}

func (h home) input(outfits []rules.Outfit, variants ...rules.Variant) Input {
	return Input{Outfits: outfits, Variants: variants, OfficialRoot: h.official, MineRoot: h.mine}
}

// byID 把結果攤平成 編號 → 照片。
func byID(res Result) map[string]Photo {
	out := map[string]Photo{}
	for _, o := range res.Outfits {
		for _, p := range o.Photos {
			out[p.ID] = p
		}
	}
	return out
}

func keys(m map[string]Photo) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return sortStrings(out)
}

func sortStrings(s []string) []string {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	return s
}

// 第一套 base 的檔名就是人設本身；一張半身＋同代號 mp4＋全身照 → 三筆，file 相對於官方角色資料夾、全身照 order = 1。
func TestBaseOutfitOnePhotoEach(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena.png", "avatars/athena.mp4", "avatars/full/athena.webp")
	res := Plan(h.input([]rules.Outfit{outfit("athena", "base", "active", "official")}))
	got := byID(res)
	want := map[string]Photo{
		"athena-base--half1":     {ID: "athena-base--half1", Persona: "athena", Outfit: "athena-base", Type: TypeHalf, Order: 1, Source: "official", File: "avatars/athena.png", Adopted: true, Key: "half-1"},
		"athena-base--video1-c1": {ID: "athena-base--video1-c1", Persona: "athena", Outfit: "athena-base", Type: TypeVideo, Order: 1, Clip: 1, Of: "athena-base--half1", Source: "official", File: "avatars/athena.mp4", Adopted: true, Key: "video-1-1"},
		"athena-base--full1":     {ID: "athena-base--full1", Persona: "athena", Outfit: "athena-base", Type: TypeFull, Order: 1, Source: "official", File: "avatars/full/athena.webp", Adopted: true, Key: "full"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("base：\n%+v\n要\n%+v", got, want)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("沒有不認得的檔：%+v", res.Skipped)
	}
	f := got["athena-base--video1-c1"].Fields()
	if f["of"] != "athena-base--half1" || f["clip"] != 1 || f["hidden"] != false || f["adopted"] != true {
		t.Errorf("影片的欄位：%v", f)
	}
	if _, ok := got["athena-base--half1"].Fields()["of"]; ok {
		t.Error("半身不寫 of、clip")
	}
}

// ~N 多張：order 是掃到的位置；缺號後面的不登記，列在沒登記的檔裡。
func TestNumberedPhotosAndGap(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena@sea.png", "avatars/athena@sea~2.jpg", "avatars/athena@sea~3.png", "avatars/athena@sea~5.png")
	res := Plan(h.input([]rules.Outfit{outfit("athena", "sea", "active", "official")}))
	got := byID(res)
	if strings.Join(keys(got), ",") != "athena-sea--half1,athena-sea--half2,athena-sea--half3" {
		t.Fatalf("~N：%v", keys(got))
	}
	if p := got["athena-sea--half2"]; p.Order != 2 || p.File != "avatars/athena@sea~2.jpg" {
		t.Errorf("第 2 張：%+v", p)
	}
	if len(res.Skipped) != 1 || filepath.Base(res.Skipped[0].Path) != "athena@sea~5.png" {
		t.Errorf("~5 接不上要列出來：%+v", res.Skipped)
	}
}

// +k 多支影片：clip 是公開的編號（第 1 支缺時往前補，同 pub.Photo.Videos）；+k 缺號後面的不登記、列出來。
func TestMultipleVideosWithGap(t *testing.T) {
	h := newHome(t)
	touch(t, h.official,
		"avatars/athena.png", "avatars/athena.mp4", "avatars/athena+2.mp4", "avatars/athena+3.mp4", "avatars/athena+5.mp4",
		"avatars/athena~2.png", "avatars/athena~2+2.mp4")
	res := Plan(h.input([]rules.Outfit{outfit("athena", "base", "draft", "official", "video-1-3", "video-2-1")}))
	got := byID(res)
	for id, file := range map[string]string{
		"athena-base--video1-c1": "avatars/athena.mp4",
		"athena-base--video1-c2": "avatars/athena+2.mp4",
		"athena-base--video1-c3": "avatars/athena+3.mp4",
		"athena-base--video2-c1": "avatars/athena~2+2.mp4", // 第 2 張沒有第 1 支，+2 是公開的第 1 支
	} {
		if p, ok := got[id]; !ok || p.File != file || p.Type != TypeVideo {
			t.Errorf("%s：%+v（要 %s）", id, p, file)
		}
	}
	if got["athena-base--video2-c1"].Of != "athena-base--half2" {
		t.Errorf("影片配第 2 張：%+v", got["athena-base--video2-c1"])
	}
	if len(got) != 6 {
		t.Errorf("2 張半身＋4 支影片：%v", keys(got))
	}
	// video-<n>-<k> 的 k 是公開編號：video-2-1 就是 athena~2+2.mp4。
	for id, want := range map[string]bool{"athena-base--video1-c3": true, "athena-base--video2-c1": true, "athena-base--video1-c1": false, "athena-base--half1": false} {
		if got[id].Adopted != want {
			t.Errorf("%s adopted = %v，要 %v", id, got[id].Adopted, want)
		}
	}
	if len(res.Skipped) != 1 || filepath.Base(res.Skipped[0].Path) != "athena+5.mp4" {
		t.Errorf("+5 接不上要列出來：%+v", res.Skipped)
	}
}

// 一般版與隱藏版同一套都有：隱藏版 hidden = true、編號接 -h、不記是哪一組；file 相對於擁有者位置
// （官方隱藏版 avatars/hidden/… 在官方角色資料夾，自己的隱藏版 mine/adult 在 personas/mine/ 底下：adult/…）。
// 隱藏版只留一組：兩組都有同一張時，排前面的（order 小）登記，後面的列出來。
func TestStandardAndHiddenSameOutfit(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena@sea.png", "avatars/athena@sea~2.png", "avatars/full/athena@sea.png",
		"avatars/hidden/athena@sea~2.png", "avatars/hidden/athena@sea~2.mp4")
	touch(t, h.mine, "adult/athena@sea~2.png", "adult/athena@sea~3.png", "adult/full/athena@sea.png")
	variants := []rules.Variant{
		{ID: "adult", Name: "自己的", Dir: "mine/adult", Order: 2, HasOrder: true},
		{ID: "night", Name: "夜", Dir: "avatars/hidden", Order: 1, HasOrder: true},
	}
	res := Plan(h.input([]rules.Outfit{outfit("athena", "sea", "active", "official")}, variants...))
	got := byID(res)
	want := map[string]struct {
		file, source string
		hidden       bool
	}{
		"athena-sea--half1":       {"avatars/athena@sea.png", "official", false},
		"athena-sea--half2":       {"avatars/athena@sea~2.png", "official", false},
		"athena-sea--full1":       {"avatars/full/athena@sea.png", "official", false},
		"athena-sea--half2-h":     {"avatars/hidden/athena@sea~2.png", "official", true},
		"athena-sea--video2-c1-h": {"avatars/hidden/athena@sea~2.mp4", "official", true},
		"athena-sea--half3-h":     {"adult/athena@sea~3.png", "mine", true},
		"athena-sea--full1-h":     {"adult/full/athena@sea.png", "mine", true},
	}
	if len(got) != len(want) {
		t.Fatalf("筆數：%v", keys(got))
	}
	for id, w := range want {
		p := got[id]
		if p.File != w.file || p.Source != w.source || p.Hidden != w.hidden {
			t.Errorf("%s：%+v，要 %+v", id, p, w)
		}
	}
	if got["athena-sea--video2-c1-h"].Of != "athena-sea--half2-h" {
		t.Errorf("隱藏版的影片配隱藏版那張：%+v", got["athena-sea--video2-c1-h"])
	}
	if len(res.Skipped) != 1 || !strings.HasSuffix(res.Skipped[0].Path, filepath.Join("adult", "athena@sea~2.png")) || !strings.Contains(res.Skipped[0].Reason, "只留一組") {
		t.Errorf("自己的隱藏版第 2 張跟 night 撞號，要列出來：%+v", res.Skipped)
	}
}

// 主人自己的衣服（source = mine）：圖在 <家>/personas/mine/avatars/，file 相對於 personas/mine/。
func TestMineOutfitRelativeToMine(t *testing.T) {
	h := newHome(t)
	touch(t, h.mine, "avatars/my_lin.png", "avatars/full/my_lin.png")
	touch(t, h.official, "avatars/my_lin.png") // 官方位置同名的不算這套（衣服說圖在 mine）
	got := byID(Plan(h.input([]rules.Outfit{outfit("my_lin", "base", "active", "mine")})))
	if p := got["my_lin-base--half1"]; p.Source != "mine" || p.File != "avatars/my_lin.png" {
		t.Errorf("主人的半身：%+v", p)
	}
	if p := got["my_lin-base--full1"]; p.Source != "mine" || p.File != "avatars/full/my_lin.png" {
		t.Errorf("主人的全身：%+v", p)
	}
}

// 設計中的舊採用：有前綴的對隱藏版、沒前綴的對一般版；2026-11-13 以前沒前綴、那張只有隱藏版有圖的算隱藏版（同以前展示間的 piecesOf）；
// 一般版也有那張時沒前綴的只算一般版。對不到的字串列出來。
func TestDraftAdoptedPrefixedAndLegacy(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena@sea.png", "avatars/hidden/athena@sea.png", "avatars/hidden/athena@sea~2.png", "avatars/hidden/full/athena@sea.png")
	night := rules.Variant{ID: "night", Name: "夜", Dir: "avatars/hidden"}
	res := Plan(h.input([]rules.Outfit{outfit("athena", "sea", "draft", "official", "half-1", "night:full", "half-2", "video-9-1")}, night))
	got := byID(res)
	for id, want := range map[string]bool{
		"athena-sea--half1":   true,  // half-1 → 一般版
		"athena-sea--half1-h": false, // 一般版也有第 1 張：沒前綴的不算隱藏版
		"athena-sea--half2-h": true,  // 舊格式 half-2：只有隱藏版有第 2 張
		"athena-sea--full1-h": true,  // night:full
	} {
		if p, ok := got[id]; !ok || p.Adopted != want {
			t.Errorf("%s adopted：%+v，要 %v", id, p, want)
		}
	}
	if u := res.Outfits[0].Unmatched; len(u) != 1 || u[0] != "video-9-1" {
		t.Errorf("對不到的：%v", u)
	}
}

// 上架的整套每張都採用（不看 adopted 清單）；下架的也是（下架前一定上架過）。
func TestActiveOutfitAllAdopted(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena.png", "avatars/athena.mp4", "avatars/athena@old.png")
	res := Plan(h.input([]rules.Outfit{outfit("athena", "base", "active", "official"), outfit("athena", "old", "retired", "official")}))
	for id, p := range byID(res) {
		if !p.Adopted {
			t.Errorf("%s 要已採用", id)
		}
	}
}

// 冪等：同樣輸入跑兩次結果一樣；寫過之後再補（Fill），已經登記的不重寫，主人改過的值不會被蓋掉。
// 退路：封存的編號不能再用，下一次補用 -r2，配這張的影片的 of 跟著換。
func TestIdempotent(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena.png", "avatars/athena.mp4", "avatars/athena~2.png", "avatars/full/athena.png")
	in := h.input([]rules.Outfit{outfit("athena", "base", "draft", "official", "half-1")})
	first, second := Plan(in), Plan(in)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("兩次結果不一樣：\n%+v\n%+v", first, second)
	}
	add := Fill(first, nil, nil)
	if add.Count() != 4 {
		t.Fatalf("第一次補 4 筆：%+v", add)
	}
	var have []sdk.Record
	for _, p := range add.Outfits[0].Photos {
		f := p.Fields()
		if p.ID == "athena-base--half2" {
			f["adopted"] = true // 主人之後把第 2 張採用了：重補不能蓋回 false
		}
		have = append(have, sdk.Record{Kind: Kind, ID: p.ID, Fields: f})
	}
	again := Fill(Plan(in), have, nil)
	if again.Count() != 0 || len(again.Outfits[0].Kept) != 4 {
		t.Fatalf("已經有的不重寫：%+v", again)
	}
	// 封存第 1 張（含它的影片）：重補用 -r2，影片的 of 指新的那筆。
	for i := range have {
		if strings.HasPrefix(have[i].ID, "athena-base--half1") || strings.HasPrefix(have[i].ID, "athena-base--video1") {
			have[i].Archived = true
		}
	}
	got := byID(Fill(Plan(in), have, nil))
	if len(got) != 2 || got["athena-base--half1-r2"].File != "avatars/athena.png" || got["athena-base--video1-c1-r2"].Of != "athena-base--half1-r2" {
		t.Fatalf("封存後重補：%v", keys(got))
	}
	// skip：不補的（例：官方層在家外時官方的照片）。
	if n := Fill(Plan(in), nil, func(Photo) bool { return true }).Count(); n != 0 {
		t.Fatalf("skip 的不補：%d", n)
	}
}

// 支援 edit 的家（UnadoptLate，DEV-160 審查 S6）：這套已經有照片紀錄之後才補登記的（之後才放進來的舊命名檔）一律還沒採用；
// 第一次轉換（這套一筆都沒有）照舊的規則（上架的整套都採用）；封存的紀錄也算「轉換過了」。不改傳進來的結果。
// 問不到核心時（SkipLate）之後才補的這一輪先不補。
func TestUnadoptLate(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena.png", "avatars/athena@old.png")
	in := h.input([]rules.Outfit{outfit("athena", "base", "active", "official"), outfit("athena", "old", "retired", "official")})
	first := Fill(Plan(in), nil, nil)
	for id, p := range byID(UnadoptLate(first, nil)) {
		if !p.Adopted {
			t.Errorf("第一次轉換照舊規則，%s 要已採用", id)
		}
	}
	var have []sdk.Record
	for _, o := range first.Outfits {
		for _, p := range o.Photos {
			if p.Outfit == "athena-base" {
				have = append(have, sdk.Record{Kind: Kind, ID: p.ID, Fields: p.Fields(), Archived: true})
			}
		}
	}
	touch(t, h.official, "avatars/athena~2.png") // 之後才放進來的第 2 張
	late := Fill(Plan(in), have, nil)
	got := byID(UnadoptLate(late, have))
	if p, ok := got["athena-base--half2"]; !ok || p.Adopted {
		t.Fatalf("轉換過的那套之後才補的要還沒採用：%+v", got)
	}
	if p, ok := got["athena-old--half1"]; !ok || !p.Adopted {
		t.Fatalf("別套第一次轉換照舊：%+v", got)
	}
	if !byID(late)["athena-base--half2"].Adopted {
		t.Fatal("不改傳進來的結果")
	}
	// 問不到核心（SkipLate）：之後才補的這一輪先不補，第一次轉換的照舊。
	skipped := byID(SkipLate(late, have))
	if _, ok := skipped["athena-base--half2"]; ok {
		t.Fatalf("不知道是哪種家時之後才補的先不補：%+v", skipped)
	}
	if p, ok := skipped["athena-old--half1"]; !ok || !p.Adopted {
		t.Fatalf("別套第一次轉換照舊：%+v", skipped)
	}
}

// 不認得的檔不碰、只列出、不出錯：沒有衣服紀錄的角色、大寫副檔名、不合規則的名字；本來就不讀的檔（.locked.png、.live.json、
// .voice.mp4）、不是圖的檔、子資料夾不列。
func TestUnknownFilesListed(t *testing.T) {
	h := newHome(t)
	touch(t, h.official,
		"avatars/athena.png",
		"avatars/helper.png",          // 沒有衣服紀錄
		"avatars/athena@sea.PNG",      // 大寫副檔名
		"avatars/Athena Final.png",    // 不合規則
		"avatars/full/nobody.png",     // 全身照沒有對應
		"avatars/athena.locked.png",   // 本來就不讀
		"avatars/athena.voice.mp4",    // 本來就不讀
		"avatars/athena.live.json",    // 不是圖
		"avatars/notes.md",            // 不是圖
		"avatars/drafts/athena~9.png", // 子資料夾不往下掃
	)
	res := Plan(h.input([]rules.Outfit{outfit("athena", "base", "active", "official")}))
	if res.Count() != 1 {
		t.Errorf("只登記 athena.png：%v", keys(byID(res)))
	}
	var names []string
	for _, s := range res.Skipped {
		names = append(names, filepath.Base(s.Path))
		if s.Reason == "" {
			t.Errorf("要寫原因：%+v", s)
		}
	}
	if got := strings.Join(sortStrings(names), ","); got != "Athena Final.png,athena@sea.PNG,helper.png,nobody.png" {
		t.Errorf("列出的：%s", got)
	}
}

// 預演讀的檔頭：核心寫出來的那幾種寫法都認（引號、清單、多行文字、布林、整數）。
func TestParseFrontmatter(t *testing.T) {
	text := "---\nschema: 1\nid: athena-sea\nname: \"海邊: 夏天\"\ndesc: |-\n    第一行\n    第二行\nnote: 'it''s'\ncost: 2\non: false\nadopted:\n    - half-1\n    - h1:full\nwindows: [\"12:00-13:00\"]\nempty: []\n---\n內文\n"
	f, err := parseFrontmatter(text)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"schema": 1, "id": "athena-sea", "name": "海邊: 夏天", "desc": "", "note": "it's", "cost": 2, "on": false,
		"adopted": []any{"half-1", "h1:full"}, "windows": []any{"12:00-13:00"}, "empty": []any{}}
	if !reflect.DeepEqual(f, want) {
		t.Errorf("檔頭：\n%#v\n要\n%#v", f, want)
	}
	if _, err := parseFrontmatter("沒有檔頭"); err == nil {
		t.Error("沒有檔頭要錯")
	}
}

// 從 vault 紀錄組 Input：封存的衣服不轉、不合格的列出來。
func TestInputFromRecords(t *testing.T) {
	outfits := []sdk.Record{
		{ID: "athena-base", Fields: map[string]any{"persona": "athena", "outfit": "base", "name": "a", "cost": 0, "state": "active", "source": "official"}},
		{ID: "athena-gone", Archived: true, Fields: map[string]any{"persona": "athena", "outfit": "gone", "name": "a", "cost": 0, "state": "active", "source": "official"}},
		{ID: "athena-bad", Fields: map[string]any{"persona": "athena", "outfit": "bad"}},
	}
	in, bad := InputFromRecords(outfits, nil, "/o", "/m")
	if len(in.Outfits) != 1 || in.Outfits[0].Record != "athena-base" || len(bad) != 1 {
		t.Errorf("Input：%+v bad=%v", in, bad)
	}
}

// 隱藏版只留一組時誰留下：舊 adopted 提到的那組（主人採用過的）先，再來開著的，最後照順序；採用跟著留下的那張。
func TestHiddenCollisionPrefersAdopted(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena@sea.png", "avatars/a/athena@sea~2.png", "avatars/b/athena@sea~2.png", "avatars/c/athena@sea~3.png", "avatars/d/athena@sea~3.png")
	variants := []rules.Variant{
		{ID: "a", Name: "a", Dir: "avatars/a", Order: 1, HasOrder: true},
		{ID: "b", Name: "b", Dir: "avatars/b", Order: 2, HasOrder: true},
		{ID: "c", Name: "c", Dir: "avatars/c", Order: 3, HasOrder: true},
		{ID: "d", Name: "d", Dir: "avatars/d", Order: 4, HasOrder: true, On: true},
	}
	res := Plan(h.input([]rules.Outfit{outfit("athena", "sea", "draft", "official", "b:half-2")}, variants...))
	got := byID(res)
	if p := got["athena-sea--half2-h"]; p.File != "avatars/b/athena@sea~2.png" || !p.Adopted {
		t.Errorf("第 2 張要留 adopted 提到的 b：%+v", p)
	}
	if p := got["athena-sea--half3-h"]; p.File != "avatars/d/athena@sea~3.png" {
		t.Errorf("第 3 張要留開著的 d：%+v", p)
	}
	if len(res.Outfits[0].Unmatched) != 0 || len(res.Skipped) != 2 {
		t.Errorf("採用要對到、撞號的兩張列出來：%v %+v", res.Outfits[0].Unmatched, res.Skipped)
	}
}

// 大小寫不分的檔案系統（macOS 預設）：athena@sea.PNG 掃得到，file 寫真正的檔名，也不會再被列成不認得。分大小寫的系統跳過。
func TestCaseInsensitiveName(t *testing.T) {
	h := newHome(t)
	touch(t, h.official, "avatars/athena@sea.PNG")
	if _, err := os.Stat(filepath.Join(h.official, "avatars", "athena@sea.png")); err != nil {
		t.Skip("檔案系統分大小寫")
	}
	res := Plan(h.input([]rules.Outfit{outfit("athena", "sea", "active", "official")}))
	if p := byID(res)["athena-sea--half1"]; p.File != "avatars/athena@sea.PNG" {
		t.Errorf("file 要是真正的檔名：%+v", p)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("登記了就不列：%+v", res.Skipped)
	}
}

// 預演讀紀錄：家裡的 vault 加官方層 <官方角色資料夾>/records/（VLT-19），同編號家裡的優先；封存的帶 Archived。
func TestReadKindFilesOfficialLayer(t *testing.T) {
	d := t.TempDir()
	homeDir, official := filepath.Join(d, "home"), filepath.Join(d, "official")
	rec := func(id, name string) string { return "---\nid: " + id + "\nname: " + name + "\n---\n" }
	write := func(path, text string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(homeDir, "vault/pkg/closet/outfit/athena-base.md"), rec("athena-base", "家裡"))
	write(filepath.Join(homeDir, "vault/pkg/closet/outfit/archive/2026-11/athena-old.md"), rec("athena-old", "舊"))
	write(filepath.Join(official, "records/pkg/closet/outfit/athena-base.md"), rec("athena-base", "官方"))
	write(filepath.Join(official, "records/pkg/closet/outfit/athena-sea.md"), rec("athena-sea", "官方"))
	recs, bad, err := ReadKindFiles(homeDir, official, "outfit")
	if err != nil || len(bad) != 0 {
		t.Fatal(err, bad)
	}
	got := map[string]string{}
	for _, r := range recs {
		got[r.ID] = r.Fields["name"].(string)
		if r.ID == "athena-old" && !r.Archived {
			t.Error("archive/ 底下的是封存的")
		}
	}
	if !reflect.DeepEqual(got, map[string]string{"athena-base": "家裡", "athena-old": "舊", "athena-sea": "官方"}) {
		t.Errorf("合併：%v", got)
	}
}
