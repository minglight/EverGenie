package personapack

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 全家的「檔 → 指著它的紀錄」索引（DEV-160 審查 M1）：匯出、建官方角色包都只帶「跟著角色走的紀錄」指著的檔，
// 但同一個檔也可能被別的紀錄指著（隱藏版的、別的角色的、封存了的）。紀錄只能讓檔更不帶、不能讓原本不帶的檔變成要帶：
// 只要有一筆不跟著走、或別的角色的紀錄指著同一個檔，那個檔就不帶。核心只看宣告（file_field、persona_field、export_only、pack_only）。

// FileRef 是一筆紀錄指著一個檔（宣告 file_field 的種類；封存的也算）。
type FileRef struct {
	Key      string // <種類>/<編號>
	Persona  string // 角色欄位的值
	Archived bool
	Exports  bool // 符合 export_only（跟著角色走）
	Packs    bool // 也符合 pack_only（可以進官方角色包；上層 pack_parent 另外看 CollectResult.HeldFiles）
}

// FileIndex 是全家宣告 file_field 的種類的每一筆紀錄（含封存的）指著的檔。key 是家裡的位置（RecordFile.Rel）轉小寫：macOS 的檔名不分大小寫。
type FileIndex struct {
	refs    map[string][]FileRef
	active  map[string]bool // 有沒封存的紀錄的角色
	records map[string]bool // 有紀錄（含封存的）的角色
	kinds   map[string]bool // 宣告 file_field 的種類（完整名稱）
	ids     map[string]bool // 列出來的每一筆（<種類>/<編號>）
}

// IndexFiles 經 store 列出所有宣告 file_field（配 by_persona）的種類的紀錄；Store 有 ArchivedLister 就連封存的一起列。store 是 nil 回空的索引。
func IndexFiles(ctx context.Context, store Store) (*FileIndex, error) {
	ix := &FileIndex{refs: map[string][]FileRef{}, active: map[string]bool{}, records: map[string]bool{}, kinds: map[string]bool{}, ids: map[string]bool{}}
	if store == nil {
		return ix, nil
	}
	kinds, err := store.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range kinds {
		if k.Decl.FileField == "" || k.Decl.Export != sdk.DataExportByPersona {
			continue
		}
		ix.kinds[k.Kind] = true
		list, err := listWithArchived(ctx, store, k.Kind)
		if err != nil {
			return nil, err
		}
		for _, rec := range list {
			ix.ids[k.Kind+"/"+rec.ID] = true
			persona, _ := rec.Fields[k.Decl.PersonaField].(string)
			ix.records[persona] = true
			if !rec.Archived {
				ix.active[persona] = true
			}
			rel, ok := indexRel(k.Decl, rec)
			if !ok {
				continue
			}
			exports := ExportAllows(k.Decl, rec.Fields)
			ix.refs[rel] = append(ix.refs[rel], FileRef{Key: k.Kind + "/" + rec.ID, Persona: persona, Archived: rec.Archived,
				Exports: exports, Packs: exports && PackAllows(k.Decl, rec.Fields)})
		}
	}
	for _, refs := range ix.refs {
		sort.Slice(refs, func(i, j int) bool { return refs[i].Key < refs[j].Key })
	}
	return ix, nil
}

// indexRel 是一筆紀錄指著的檔在索引裡的 key：跟 recordFile 一樣，但路徑不乾淨（多了斜線、. 這類）的也照整理過的位置算
// （寧可多擋；跑出 personas/ 外面的不算）。
func indexRel(decl sdk.DataKindDecl, rec sdk.Record) (string, bool) {
	file, _ := rec.Fields[decl.FileField].(string)
	file = path.Clean(strings.Trim(strings.TrimSpace(file), "/"))
	if file == "." || file == ".." || strings.HasPrefix(file, "../") {
		return "", false
	}
	src := ""
	if decl.PersonaSource != "" {
		src, _ = rec.Fields[decl.PersonaSource].(string)
	}
	return strings.ToLower(RecordFile{Mine: src == "mine", File: file}.Rel()), true
}

func listWithArchived(ctx context.Context, store Store, kind string) ([]sdk.Record, error) {
	if al, ok := store.(ArchivedLister); ok {
		return al.ListWithArchived(ctx, kind)
	}
	return store.List(ctx, kind)
}

// Unlisted 是家裡 vault 裡這些種類的紀錄檔（vault/pkg/<套件>/<種類>/<編號>.md），服務卻沒列出來的（檔頭讀不懂、不合規格，vault 不收）：
// 不知道它們標著哪些圖不能帶（例如一筆手改壞了的隱藏版照片紀錄），建包與匯出就不做（DEV-160 第二輪審查，判斷不了就拒絕）。回家底下的相對路徑，排序過。
func (ix *FileIndex) Unlisted(home string) []string {
	if ix == nil {
		return nil
	}
	var out []string
	for kind := range ix.kinds {
		pkgID, name, ok := strings.Cut(kind, ".")
		if !ok {
			continue
		}
		entries, _ := os.ReadDir(filepath.Join(home, "vault", "pkg", pkgID, name))
		for _, e := range entries {
			id, isMD := strings.CutSuffix(e.Name(), ".md")
			if !isMD || e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.Contains(e.Name(), ".bak-") || ix.ids[kind+"/"+id] {
				continue
			}
			out = append(out, "vault/pkg/"+pkgID+"/"+name+"/"+e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// UnlistedError 是有 Unlisted 的紀錄時給人看的錯誤（what＝「建包」「匯出」）；沒有回 nil。
func UnlistedError(home string, ix *FileIndex, what string) error {
	bad := ix.Unlisted(home)
	if len(bad) == 0 {
		return nil
	}
	if len(bad) > 5 {
		bad = append(bad[:5], fmt.Sprintf("…共 %d 筆", len(bad)))
	}
	return platform.Conflict("這個家有讀不懂的照片紀錄：" + strings.Join(bad, "、") + "。它們可能標著哪些圖不能帶（例如隱藏版），讀不懂就判斷不了，沒有" + what +
		"。先修好或刪掉這些檔（evergenie doctor 看原因）再" + what)
}

// On：這個家有宣告 file_field 的種類（有的話照紀錄帶圖）。
func (ix *FileIndex) On() bool { return ix != nil && len(ix.kinds) > 0 }

// FileKind：kind 是宣告 file_field 的種類。
func (ix *FileIndex) FileKind(kind string) bool { return ix != nil && ix.kinds[kind] }

// HasRecords：這位角色在這些種類有紀錄（含封存的）。一筆都沒有的角色照檔名找圖（還沒登記的舊家，S7）。
func (ix *FileIndex) HasRecords(slug string) bool { return ix != nil && ix.records[slug] }

// Persona：這位角色有還在的紀錄（給「檔名是不是別的角色的」用；只有封存紀錄的舊代號不算，例如改過代號之後）。
func (ix *FileIndex) Persona(slug string) bool { return ix != nil && ix.active[slug] }

// Pointed：有紀錄指著這個檔（rel 是家裡的位置，不分大小寫）。
func (ix *FileIndex) Pointed(rel string) bool {
	return ix != nil && len(ix.refs[strings.ToLower(rel)]) > 0
}

// Blocked：rel（家裡的位置 personas/…）不能跟著 slugs 這幾位走的原因；空＝紀錄不擋。有別的角色的紀錄、不跟著角色走（export_only）的紀錄
// 指著它就擋，封存的也算；pack＝建官方角色包，不符合 pack_only 的也擋。原因接在檔的路徑後面（例「：是不跟著角色走的紀錄 … 的圖」）。
func (ix *FileIndex) Blocked(rel string, isSlug map[string]bool, pack bool) string {
	if ix == nil {
		return ""
	}
	for _, r := range ix.refs[strings.ToLower(rel)] {
		switch {
		case r.Persona == "":
			return "是沒寫角色的紀錄 " + r.Key + " 的圖"
		case !isSlug[r.Persona]:
			return "是別的角色（" + r.Persona + "）的紀錄 " + r.Key + " 的圖"
		case !r.Exports:
			return "是不跟著角色走的紀錄 " + r.Key + " 的圖"
		case pack && !r.Packs:
			return "是不進官方角色包的紀錄 " + r.Key + " 的圖"
		}
	}
	return ""
}

// NameOwner 是家裡的位置（personas/… 或 personas/mine/…）照名字看是哪位角色的：avatars/ 底下檔名是 <代號>@…、<代號>~…、<代號>+…、<代號>.<副檔名>
// 的 <代號>（轉小寫、要是角色代號的樣子），wardrobe/<代號>/… 的 <代號>；看不出來（例如照片資料夾 photos/<照片編號>.<ext>）回空。
// 檔名本身不讓檔變成要帶，只用來擋：一筆紀錄不能把檔名是別的角色的檔帶走。
func NameOwner(rel string) string {
	rest, _ := strings.CutPrefix(filepath.ToSlash(rel), "personas/")
	if r, ok := strings.CutPrefix(rest, "mine/"); ok {
		rest = r
	}
	parts := strings.Split(rest, "/")
	switch {
	case len(parts) >= 3 && parts[0] == "wardrobe":
		if s := strings.ToLower(parts[1]); personaref.ValidSlug(s) {
			return s
		}
	case len(parts) >= 2 && parts[0] == "avatars":
		base := parts[len(parts)-1]
		if i := strings.IndexAny(base, "@~.+"); i > 0 {
			if s := strings.ToLower(base[:i]); personaref.ValidSlug(s) {
				return s
			}
		}
	}
	return ""
}

// HasCard：這個家有 slug 這位角色的卡（官方位置 personas/<代號>.md 或我的 personas/mine/<代號>.md；捷徑也算）。
func HasCard(home, slug string) bool {
	for _, p := range []string{filepath.Join(platform.OfficialPersonasDir(home), slug+".md"), HomePath(home, "personas/mine/"+slug+".md")} {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}

// ResolveDirs 把要排除的資料夾（personas/ 底下的相對路徑）解開符號連結再列一次（小寫、排序不重複）：衣櫃照符號連結找圖，
// 隱藏版的資料夾若是連到別的資料夾（例 wardrobe/<代號>/ 底下）的連結，真正的那個資料夾也不收（PRS-4）。
// 解開後在官方角色資料夾或家裡的 personas/ 底下才列（相對於那裡）；解不開、連到外面的只留原本的。
func ResolveDirs(home string, dirs []string) []string {
	var roots []string
	for _, r := range []string{platform.OfficialPersonasDir(home), filepath.Join(home, "personas")} {
		if real, err := filepath.EvalSymlinks(r); err == nil {
			roots = append(roots, real)
		} else {
			roots = append(roots, r)
		}
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		seen[d] = true
		real, err := filepath.EvalSymlinks(HomePath(home, "personas/"+d))
		if err != nil {
			continue
		}
		for _, root := range roots {
			rel, err := filepath.Rel(root, real)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			seen[strings.ToLower(filepath.ToSlash(rel))] = true
			break
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// UnderDir 回 rel（personas/ 底下的相對路徑）落在 dirs 的哪一個資料夾裡；不在回空。比的時候不分大小寫（macOS 的磁碟不分）。
func UnderDir(rel string, dirs []string) string { return underDir(rel, dirs) }

// UnreadableOf 從套件清單列出停用中與被拒的套件，和它們宣告要跟著角色走的資料（UnreadableLister 的共用做法：指令行程的 sdk store 與 serve 的 store）。
// 被拒而且連套件設定都讀不懂的（沒有種類、沒有宣告）算 Unknown。
func UnreadableOf(pkgs []sdk.Package) []UnreadablePackage {
	var out []UnreadablePackage
	for _, p := range pkgs {
		rejected := p.State == sdk.PackageStateRejected
		if !rejected && p.Enabled {
			continue
		}
		u := UnreadablePackage{ID: p.ID, Why: "停用中"}
		if rejected {
			u.Why = "被拒：" + p.Error
			u.Unknown = p.Kind == "" && len(p.Data) == 0
		}
		for _, d := range p.Data {
			if d.Export == "" {
				continue
			}
			u.Exports = append(u.Exports, p.ID+"."+d.Kind)
			u.Folder = u.Folder || d.Export == sdk.DataExportByFolder
			if d.FileField != "" {
				u.Files = append(u.Files, p.ID+"."+d.Kind)
			}
		}
		out = append(out, u)
	}
	return out
}

// KeepRecordsWithFiles 拿掉「宣告 file_field、但指著的檔沒帶」的紀錄（DEV-160 審查 M6）：檔被擋下、不在、路徑不對的，紀錄也不帶，
// 不然收件的家會多一筆指著空位置（或指著收件家自己的檔）的紀錄。files 是 CollectRecords 的 Files；carried 說那個檔有沒有帶。
// 回留下的紀錄與拿掉的原因（白話一行一個）。
func KeepRecordsWithFiles(recs []Record, files []RecordFile, fileKind func(kind string) bool, carried func(RecordFile) bool) ([]Record, []string) {
	byKey := map[string]RecordFile{}
	for _, f := range files {
		byKey[f.Key] = f
	}
	out := make([]Record, 0, len(recs))
	var dropped []string
	for _, r := range recs {
		if !fileKind(r.Kind) {
			out = append(out, r)
			continue
		}
		f, ok := byKey[r.Key()]
		switch {
		case !ok:
			dropped = append(dropped, r.Key()+"：沒有指著能帶的檔（檔的欄位是空的或路徑不對），紀錄也不帶")
		case !carried(f):
			dropped = append(dropped, r.Key()+"：指著的 "+f.Rel()+" 沒帶（見上面的原因，或檔不在），紀錄也不帶")
		default:
			out = append(out, r)
		}
	}
	return out, dropped
}
