// Package photomigrate 是一次性的照片轉換器（DEV-160 S1）：掃現有的圖檔，替每個檔產生一筆 closet.photo 紀錄，
// 把 closet.outfit 的 adopted 字串清單轉成每張的 adopted。
//
// 這是全 repo 唯一保留「舊檔名有意思」的地方（scan.go）：<人設>[@<衣服>][~N].<ext>、同代號 .mp4 與 +k.mp4、full/<代號>.<ext>、
// 隱藏版資料夾（closet.variant 的 dir），與看名字猜隱藏版（adult.go 的 IsAdultDir）。轉換後各處只讀 closet.photo（S2 起），檔名不再帶意思。
//
// 衣櫃的程式每一輪用它補紀錄（Fill：檔案還沒有紀錄指著的才補，只新增、不碰檔案；S2）；cmd/closet-photos 是預演入口。
// 純計算＋只讀檔案：Plan、Fill 都不寫任何東西；寫入是衣櫃的程式（run.fillPhotos）。
package photomigrate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/packages/official/closet/internal/rules"
)

// Kind 是照片紀錄的種類（closet.photo）。
const Kind = "closet.photo"

// 照片的類型（closet.photo 的 type）。
const (
	TypeHalf  = "half"
	TypeFull  = "full"
	TypeVideo = "video"
)

// Input 是轉換要知道的事：衣服目錄、隱藏版、兩個擁有者位置。哪些已經登記了由 Fill 對照 vault 裡的 closet.photo 決定。
type Input struct {
	// Outfits 是 closet.outfit（沒封存的；不合格的呼叫的人先略過）。
	Outfits []rules.Outfit
	// Variants 是 closet.variant（含自己的隱藏版 mine/adult；不看開關，轉換只管檔案在不在）。
	Variants []rules.Variant
	// OfficialRoot 是官方角色資料夾（sdk.OfficialPersonasDir(home)）；MineRoot 是 <家>/personas/mine。
	OfficialRoot, MineRoot string
}

// Photo 是一筆要寫的 closet.photo。
type Photo struct {
	ID      string
	Persona string
	Outfit  string // closet.outfit 編號 <人設>-<衣服 id>
	Type    string // half／full／video
	Order   int
	Of      string // 只給影片：配哪一張照片的 closet.photo 編號
	Clip    int    // 只給影片：這張照片的第幾支（1 起算，就是公開的編號）
	Hidden  bool
	Source  string // official／mine：file 相對於哪個位置
	File    string // 相對於擁有者位置的路徑（用 /）
	Adopted bool
	// Key 是舊的 adopted 寫法（half-2、h1:full、video-1-2…），只給預演與測試看，不寫進紀錄。
	Key string
}

// Fields 是寫進 vault 的欄位（hidden、adopted 一律明寫，官方角色包的 pack_only 看得到值）。
func (p Photo) Fields() map[string]any {
	f := map[string]any{
		"persona": p.Persona, "outfit": p.Outfit, "type": p.Type, "order": p.Order,
		"hidden": p.Hidden, "source": p.Source, "file": p.File, "adopted": p.Adopted,
	}
	if p.Type == TypeVideo {
		f["of"], f["clip"] = p.Of, p.Clip
	}
	return f
}

// OutfitResult 是一套衣服轉出來的結果。
type OutfitResult struct {
	Record  string
	Persona string
	Photos  []Photo  // Plan：照檔案算出來的全部；Fill 之後：這一輪要新寫的
	Kept    []string // Fill 之後：已經有紀錄指著、不重寫的（轉換器的編號）
	// Unmatched 是舊 adopted 清單裡對不到任何一張的字串（列出來，不轉）。
	Unmatched []string
}

// Skipped 是沒有轉的檔：不認得的檔名、接不上前面編號的、隱藏版只留一組時撞號的（UFE-8 第 7 點：不碰，只列出）。
type Skipped struct {
	Path   string // 完整路徑
	Reason string
}

// Result 是整個家的轉換結果。
type Result struct {
	Outfits []OutfitResult // 照編號排
	Skipped []Skipped      // 照路徑排
}

// Count 是要新寫的筆數。
func (r Result) Count() int {
	n := 0
	for _, o := range r.Outfits {
		n += len(o.Photos)
	}
	return n
}

// PhotoID 是照片紀錄的編號：<衣服編號>--<類型><第幾張>[-c<第幾支>][-h]。
// 半身 <衣服>--half<n>、全身 <衣服>--full1、影片 <衣服>--video<n>-c<k>（n 是配的那張半身的 order）；隱藏版接 -h。
// 同樣的檔重算一定得到同一個編號（冪等）；編號只是名字，讀的人看欄位，不要拆編號。
func PhotoID(record, typ string, order, clip int, hidden bool) string {
	id := fmt.Sprintf("%s--%s%d", record, typ, order)
	if typ == TypeVideo {
		id += fmt.Sprintf("-c%d", clip)
	}
	if hidden {
		id += "-h"
	}
	return id
}

// root 是一個擁有者位置：檔案的 file 欄位相對於它。
type root struct{ source, dir string }

func (in Input) rootOf(source string) root {
	if source == rules.SourceMine {
		return root{rules.SourceMine, in.MineRoot}
	}
	return root{rules.SourceOfficial, in.OfficialRoot}
}

// variantRoot：隱藏版 dir（相對於 personas/）的實際位置與擁有者。mine/ 開頭的是主人的（相對於 <家>/personas/mine/），
// 其他是官方的（相對於官方角色資料夾）。經過符號連結跑出擁有者位置的不用。
func (in Input) variantRoot(v rules.Variant) (root, string, bool) {
	if !rules.ValidVariantDir(v.Dir) {
		return root{}, "", false
	}
	r, rel := in.rootOf(rules.SourceOfficial), v.Dir
	if first, rest, _ := strings.Cut(v.Dir, "/"); first == "mine" {
		r, rel = in.rootOf(rules.SourceMine), rest // dir = "mine" 就是 personas/mine 本身（同 view 的掃法）
	}
	if r.dir == "" {
		return root{}, "", false
	}
	base, err := filepath.EvalSymlinks(r.dir)
	if err != nil {
		return root{}, "", false
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(base, filepath.FromSlash(rel)))
	if err != nil {
		return root{}, "", false
	}
	if out, err := filepath.Rel(base, dir); err != nil || out == ".." || strings.HasPrefix(out, ".."+string(filepath.Separator)) {
		return root{}, "", false
	}
	return root{r.source, base}, dir, true
}

// relTo 是 path 相對於擁有者位置的路徑（用 /）；跑出去回 false。
func relTo(r root, path string) (string, bool) {
	base, err := filepath.EvalSymlinks(r.dir)
	if err != nil {
		base = r.dir
	}
	dir, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(base, filepath.Join(dir, filepath.Base(path)))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// Plan 算出整個家要寫哪些 closet.photo（不寫任何東西）。
func Plan(in Input) Result {
	variants := append([]rules.Variant(nil), in.Variants...)
	rules.SortVariants(variants)
	outfits := append([]rules.Outfit(nil), in.Outfits...)
	sort.Slice(outfits, func(i, j int) bool { return outfits[i].Record < outfits[j].Record })

	claimed := map[string]bool{} // 已經處理過的檔（實際位置）：登記了的、撞號列出來的
	var res Result
	for _, o := range outfits {
		if !rules.ValidPersona(o.Persona) || !rules.ValidOutfit(o.Outfit) || o.Record != rules.RecordID(o.Persona, o.Outfit) {
			continue
		}
		res.Outfits = append(res.Outfits, in.planOutfit(o, preferVariants(o, variants), claimed, &res.Skipped))
	}
	res.Skipped = append(res.Skipped, in.unclaimed(variants, claimed)...)
	sort.Slice(res.Skipped, func(i, j int) bool { return res.Skipped[i].Path < res.Skipped[j].Path })
	return res
}

// planOutfit 轉一套衣服：一般版（衣服的 source 那邊的 avatars/），再來每個隱藏版（hidden = true，不記是哪一組）。
func (in Input) planOutfit(o rules.Outfit, variants []rules.Variant, claimed map[string]bool, skipped *[]Skipped) OutfitResult {
	out := OutfitResult{Record: o.Record, Persona: o.Persona}
	var all []Photo
	ids := map[string]string{} // 編號 → 檔，撞號用
	// 衣服的編號或名字看起來是隱藏版（adult、nsfw、r18、成人、level）：整套的圖都登記成隱藏版（以前角色包、匯出、升成官方看名字擋，S2 起只看 hidden）。
	adultOutfit := IsAdultDir(o.Record) || IsAdultDir(o.Name)
	// photo 登記一個檔；回編號（沒登記回空字串）。
	photo := func(r root, typ string, order, clip int, hidden bool, of, src, key string) string {
		src = actualName(src)
		real := realPath(src)
		if claimed[real] {
			return "" // 同一個檔被兩個地方掃到（兩個隱藏版指同一個資料夾）：只登記第一次
		}
		file, ok := relTo(r, src)
		if !ok {
			return "" // 經過符號連結跑出擁有者位置：不登記，會列在不認得的檔裡
		}
		// 名字看起來是隱藏版的（路徑任一段、衣服編號或名字）也登記成隱藏版：寧可多擋，之後只看 hidden。
		hidden = hidden || adultOutfit || IsAdultDir(file)
		id := PhotoID(o.Record, typ, order, clip, hidden)
		if prev, dup := ids[id]; dup {
			claimed[real] = true
			*skipped = append(*skipped, Skipped{Path: src, Reason: fmt.Sprintf("隱藏版只留一組：跟 %s 是同一張（%s），沒有登記", filepath.Base(prev), id)})
			return ""
		}
		claimed[real] = true
		ids[id] = src
		all = append(all, Photo{ID: id, Persona: o.Persona, Outfit: o.Record, Type: typ, Order: order, Clip: clip, Of: of, Hidden: hidden, Source: r.source, File: file, Key: key})
		return id
	}
	halves := func(r root, list []scanned, hidden bool, prefix string) {
		for _, ph := range list {
			hid := photo(r, TypeHalf, ph.N, 0, hidden, "", ph.Src, prefix+fmt.Sprintf("half-%d", ph.N))
			for k, v := range ph.Videos() {
				if hid == "" {
					if real := realPath(v); !claimed[real] {
						claimed[real] = true
						*skipped = append(*skipped, Skipped{Path: v, Reason: "配的那張照片沒有登記，影片跟著不登記"})
					}
					continue
				}
				photo(r, TypeVideo, ph.N, k+1, hidden, hid, v, prefix+fmt.Sprintf("video-%d-%d", ph.N, k+1))
			}
		}
	}

	if std := in.rootOf(o.Source); std.dir != "" {
		avatars := filepath.Join(std.dir, "avatars")
		halves(std, ScanPhotos(avatars, o.Persona, o.Outfit), false, "")
		if f := ScanFull(avatars, o.Persona, o.Outfit); f.Src != "" {
			photo(std, TypeFull, 1, 0, false, "", f.Src, "full")
		}
	}
	for _, v := range variants {
		r, dir, ok := in.variantRoot(v)
		if !ok {
			continue
		}
		slots := ScanSlots(dir, o.Persona, o.Outfit)
		list := make([]scanned, 0, len(slots))
		for n := 1; n <= MaxPhotos; n++ {
			if ph, ok := slots[n]; ok {
				list = append(list, ph)
			}
		}
		halves(r, list, true, v.ID+":")
		if f := ScanFull(dir, o.Persona, o.Outfit); f.Src != "" {
			photo(r, TypeFull, 1, 0, true, "", f.Src, v.ID+":full")
		}
	}

	out.Unmatched = adopt(o, all)
	out.Photos = all
	return out
}

// adopt 把舊的採用轉成每張的 adopted：上架、下架的整套每張都算採用；設計中的照 adopted 清單
// （half-<n>、full、video-<n>-<k>；隱藏版前面加 <隱藏版編號>:；2026-11-13 以前的舊格式沒有前綴、那一張只有隱藏版有圖時算隱藏版，
// 規則同以前展示間「設計中」的 piecesOf，DEV-160 S5b 拿掉）。回對不到任何一張的字串。
func adopt(o rules.Outfit, all []Photo) []string {
	if o.State != rules.StateDraft {
		for i := range all {
			all[i].Adopted = true
		}
		return nil
	}
	done := map[string]bool{}
	for _, k := range o.Adopted {
		done[k] = true
	}
	std := map[string]bool{}
	for _, p := range all {
		if !p.Hidden {
			std[p.Key] = true
		}
	}
	used := map[string]bool{}
	for i, p := range all {
		if done[p.Key] {
			all[i].Adopted, used[p.Key] = true, true
			continue
		}
		if _, bare, ok := strings.Cut(p.Key, ":"); ok && done[bare] && !std[bare] {
			all[i].Adopted, used[bare] = true, true
		}
	}
	var miss []string
	for _, k := range o.Adopted {
		if !used[k] {
			miss = append(miss, k)
		}
	}
	return miss
}

// 不是照片、本來就不讀的檔（README「其他檔不會被讀」）：不列為不認得。
var ignoredSuffixes = []string{".locked.png", ".live.json", ".voice.mp4"}

var mediaExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".mp4": true}

// unclaimed 列出圖資料夾裡沒有被任何一套衣服登記的圖檔與影片：官方與主人的 avatars/、avatars/full/，每個隱藏版資料夾與它的 full/。
// 子資料夾不往下掃（隱藏版資料夾自己會掃）；不是圖或影片的檔、本來就不讀的檔不列。
func (in Input) unclaimed(variants []rules.Variant, claimed map[string]bool) []Skipped {
	var dirs []string
	for _, src := range []string{rules.SourceOfficial, rules.SourceMine} {
		r := in.rootOf(src)
		if r.dir == "" {
			continue
		}
		dirs = append(dirs, filepath.Join(r.dir, "avatars"), filepath.Join(r.dir, "avatars", "full"))
	}
	for _, v := range variants {
		if _, dir, ok := in.variantRoot(v); ok {
			dirs = append(dirs, dir, filepath.Join(dir, "full"))
		}
	}
	seen := map[string]bool{}
	var out []Skipped
	for _, d := range dirs {
		real, err := filepath.EvalSymlinks(d)
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if !e.Type().IsRegular() || strings.HasPrefix(name, ".") || !mediaExts[strings.ToLower(filepath.Ext(name))] || hasIgnoredSuffix(name) {
				continue
			}
			path := filepath.Join(d, name)
			if claimed[realPath(path)] {
				continue
			}
			out = append(out, Skipped{Path: path, Reason: "檔名規則不認得、接不上前面的編號，或沒有對應的衣服紀錄"})
		}
	}
	return out
}

func hasIgnoredSuffix(name string) bool {
	lower := strings.ToLower(name)
	for _, s := range ignoredSuffixes {
		if strings.HasSuffix(lower, s) {
			return true
		}
	}
	return false
}

// actualName：大小寫不分的檔案系統（macOS）上，掃圖用小寫副檔名也找得到 athena@sea.PNG；登記時換成資料夾裡真正的檔名，
// file 欄位才跟實際檔名一樣（換到分大小寫的系統照樣找得到），也不會同一個檔又被列成不認得。
func actualName(path string) string {
	dir, base := filepath.Dir(path), filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path
	}
	for _, e := range entries {
		if e.Name() == base {
			return path
		}
	}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), base) {
			return filepath.Join(dir, e.Name())
		}
	}
	return path
}

// preferVariants 是這套衣服撞號時誰先登記（隱藏版只留一組，作者 2026-11-15）：舊 adopted 清單提到的那組（主人看過、採用過的）先，
// 再來開著的（畫面上看得到的那組，同 view 的 applyVariant），其餘照 SortVariants。
func preferVariants(o rules.Outfit, sorted []rules.Variant) []rules.Variant {
	named := map[string]bool{}
	for _, k := range o.Adopted {
		if id, _, ok := strings.Cut(k, ":"); ok {
			named[id] = true
		}
	}
	rank := func(v rules.Variant) int {
		switch {
		case named[v.ID]:
			return 0
		case v.On:
			return 1
		}
		return 2
	}
	out := append([]rules.Variant(nil), sorted...)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// realPath 是檔案的實際位置（經過符號連結的解開，例 macOS 的 /var → /private/var）；解不開就原樣。登記與比對都用它。
func realPath(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return path
}
