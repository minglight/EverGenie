package personapack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 跟著角色走的套件紀錄（D-63、PKG-57～59）：角色匯出（personaexport）與官方角色包（Build、Sync）共用同一套收、驗、比對、寫入。
// 核心不認得任何套件：要不要帶、怎麼帶只看套件 `contributes.data` 的宣告（export、persona_field、folder_field、export_omit、pack_only、pack_except）；
// 寫入一律經 Store（指令行程接 sdk client、serve 接 vault 模組），有檔頭、驗證與事件，這裡不碰 vault 的檔案。

// Record 是一筆跟著角色走的套件紀錄：Kind 是完整種類名稱（`<套件>.<種類>`），Fields 不含核心管的欄位與宣告 export_omit 的欄位。
type Record struct {
	Kind   string         `json:"kind"`
	ID     string         `json:"id"`
	Fields map[string]any `json:"fields"`
	Body   string         `json:"body,omitempty"`
}

// Key 是 `<種類>/<編號>`。
func (r Record) Key() string { return r.Kind + "/" + r.ID }

// KindInfo 是這個家現在註冊中（套件有效而且啟用）的一種套件資料：完整名稱與宣告。
type KindInfo struct {
	Kind string
	Decl sdk.DataKindDecl
}

// Store 是收與寫紀錄要的套件資料。寫入一律經 vault 的正常寫入（檔頭、驗證、事件）。
type Store interface {
	// Kinds 列出註冊中的套件資料種類（停用、被拒的不列）。
	Kinds(ctx context.Context) ([]KindInfo, error)
	// List 列出一種資料沒封存的紀錄。
	List(ctx context.Context, kind string) ([]sdk.Record, error)
	// Get 讀一筆（封存的也讀得到）；沒有回 nil、nil。
	Get(ctx context.Context, kind, id string) (*sdk.Record, error)
	// Put 新增（expectedRevision 空）或整筆取代（帶現在的 revision）。
	Put(ctx context.Context, rec sdk.Record, expectedRevision string) error
}

// ArchivedLister 是能連封存的一起列的 Store（可以不實作）：建包找隱藏版資料夾時用，封存了、圖還在資料夾裡的也要排除（PRS-4）。
type ArchivedLister interface {
	ListWithArchived(ctx context.Context, kind string) ([]sdk.Record, error)
}

// UnreadableLister 是能列出「有裝、但這次讀不到紀錄」的套件的 Store（可以不實作；指令行程的 sdk store 有）。
// 建包用它確定沒有悄悄漏掉跟著角色走的紀錄、也沒有漏看隱藏版的資料夾（PRS-4）：Kinds 不列停用、被拒的套件。
type UnreadableLister interface {
	Unreadable(ctx context.Context) ([]UnreadablePackage, error)
}

// UnreadablePackage 是停用中或被拒的套件。Exports 是它宣告要跟著角色走的資料種類（完整名稱，`export` 有值的）；
// Folder 是其中有沒有跟著資料夾走的（by_folder）；Unknown＝套件設定讀不懂，不知道它宣告了什麼。
type UnreadablePackage struct {
	ID      string
	Why     string // 白話：停用中、被拒（原因）
	Exports []string
	Folder  bool
	Unknown bool
	// Files 是其中宣告 file_field 的（照紀錄帶圖的種類，完整名稱）：讀不到它的紀錄就不知道哪些檔被標成不帶，建包與匯出都不做（DEV-160 審查 M2）。
	Files []string
}

const maxRecords = 5000

var (
	recordKindPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`)
	recordIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	fieldNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	// coreFields 是 vault 自己管的欄位：匯出不帶、匯入不收。
	coreFields = map[string]bool{"schema": true, "id": true, "created_at": true, "updated_at": true, "by": true, "kind": true, "archived_at": true}
)

// ValidateRecords 檢查一串紀錄本身：數量、種類名稱、編號、欄位名稱（不能是核心管的）、不重複。where 是錯誤訊息裡的「匯出檔」「角色包」。
func ValidateRecords(recs []Record, where string) error {
	if len(recs) > maxRecords {
		return fmt.Errorf("%s裡的紀錄太多了", where)
	}
	keys := map[string]bool{}
	for _, r := range recs {
		if !recordKindPattern.MatchString(r.Kind) || !recordIDPattern.MatchString(r.ID) {
			return fmt.Errorf("%s裡的紀錄「%s/%s」種類或編號不對", where, r.Kind, r.ID)
		}
		if keys[r.Key()] {
			return fmt.Errorf("%s裡的紀錄「%s」重複了", where, r.Key())
		}
		keys[r.Key()] = true
		for name := range r.Fields {
			if !fieldNamePattern.MatchString(name) || coreFields[name] {
				return fmt.Errorf("%s裡的紀錄「%s」有不能匯入的欄位「%s」", where, r.Key(), name)
			}
		}
	}
	return nil
}

// CollectResult 是 CollectRecords 收到的。
type CollectResult struct {
	Records []Record
	Kinds   []string // 帶了哪些種類（完整名稱）
	Skipped []string // 刻意沒帶的（白話一行一個）
	// Held 是套件宣告「官方角色包只帶哪些」（pack_only，PKG-68）擋下的：公開角色的紀錄，但不在可以公開的值裡（例如還沒升成官方的衣服）。
	// 一種一筆、照種類排；只有建官方角色包（folder 是 nil）才會有。
	Held []HeldRecords
	// HeldMedia 是被擋下的紀錄照宣告認得出來的圖：種類宣告編號是 <角色代號>-<其他>（persona_id = "prefix"，PKG-64）時，
	// 核心自己的圖檔命名 <角色代號>@<其他>… 就是它的圖。建包把這些圖也排除（PRS-29）；沒宣告 persona_id 的種類認不出來，不列。
	// 宣告 file_field 的種類不列（它的檔照紀錄認，在 HeldFiles）。
	HeldMedia []HeldMedia
	// 紀錄指著的檔（宣告 file_field 的種類，DEV-160）：Files 是帶了的紀錄指的檔，HeldFiles 是被擋下的紀錄指的檔（建包連檔也不收）；
	// NotExportedFiles 是不跟著角色走（export_only）的紀錄指的檔：建包與匯出都不收，就算放在會整個帶走的位置（wardrobe/<代號>/）也一樣。
	// FileKinds＝這個家有宣告 file_field 的種類（有的話建包與匯出照紀錄帶圖，不再照檔名認）。
	Files            []RecordFile
	HeldFiles        []RecordFile
	NotExportedFiles []RecordFile
	FileKinds        bool
	// Bare 是進了官方角色包、可是一筆下層紀錄都沒有的上層紀錄（DEV-160 收尾）：下層是宣告 file_field、pack_parent 指著它的種類
	// （例如一套官方衣服一筆照片紀錄都沒有）。照紀錄帶檔，這幾筆的檔一個也帶不走——通常是套件還沒替剛放進來的圖登記。
	// 一種下層一筆；只有建官方角色包（folder 是 nil）才會有。下層的紀錄不管跟不跟著走、進不進包都算（有登記過），封存的不算。
	Bare []BareRecords
}

// BareRecords 是一種下層資料（宣告 file_field 與 pack_parent）一筆都沒指著的、進了包的上層紀錄。
type BareRecords struct {
	Kind       string   // 上層種類（完整名稱），例 closet.outfit
	Title      string   // 上層種類的名稱（宣告的 title；沒寫是 Kind）
	Child      string   // 下層種類（完整名稱），例 closet.photo
	ChildTitle string   // 下層種類的名稱
	IDs        []string // 上層編號，排序過
}

// kindTitle 是一種資料給人看的名稱：宣告的 title，沒寫用完整名稱。
func kindTitle(kind string, decl sdk.DataKindDecl) string {
	if decl.Title != "" {
		return decl.Title
	}
	return kind
}

// RecordFile 是一筆紀錄指著的檔（宣告 file_field 的種類）：Mine＝檔在主人的 personas/mine/（persona_source 欄位是 mine），
// 不然在官方角色資料夾；File 是相對於那裡的路徑（用 /，整理過，CleanPath 不過的不列）；Key 是 <種類>/<編號>。
type RecordFile struct {
	Key  string
	Mine bool
	File string
}

// Rel 是這個檔在家裡的位置（personas/… 或 personas/mine/…，實際位置用 HomePath 換）。
func (f RecordFile) Rel() string {
	if f.Mine {
		return "personas/mine/" + f.File
	}
	return "personas/" + f.File
}

// recordFile 照宣告讀一筆紀錄指著的檔；沒宣告 file_field、欄位是空的或路徑不乾淨回 false。
func recordFile(kind string, decl sdk.DataKindDecl, rec sdk.Record) (RecordFile, bool) {
	if decl.FileField == "" {
		return RecordFile{}, false
	}
	file, _ := rec.Fields[decl.FileField].(string)
	if file == "" || !CleanPath(file) {
		return RecordFile{}, false
	}
	src := ""
	if decl.PersonaSource != "" {
		src, _ = rec.Fields[decl.PersonaSource].(string)
	}
	return RecordFile{Key: kind + "/" + rec.ID, Mine: src == "mine", File: file}, true
}

// ExportAllows：這筆紀錄符不符合宣告的 export_only（DEV-160）——比法同 PackAllows；沒宣告＝都可以。匯出與官方角色包都看。
func ExportAllows(decl sdk.DataKindDecl, fields map[string]any) bool {
	return PackAllows(sdk.DataKindDecl{PackOnly: decl.ExportOnly}, fields)
}

// HeldMedia 是一筆被 pack_only 擋下的紀錄的圖怎麼認：Slug 是角色代號，Rest 是編號裡角色代號後面那一段；Key 是 <種類>/<編號>。
type HeldMedia struct{ Slug, Rest, Key string }

// Matches：包裡的邏輯路徑是不是這筆紀錄的圖——檔名是 <角色代號>@<其他>（後面接 ~N、+k 或副檔名），或在 wardrobe/<角色代號>/<其他>/ 底下。
func (h HeldMedia) Matches(logical string) bool {
	if strings.HasPrefix(logical, "wardrobe/"+h.Slug+"/"+h.Rest+"/") {
		return true
	}
	base, stem := path.Base(logical), h.Slug+"@"+h.Rest
	if !strings.HasPrefix(base, stem) {
		return false
	}
	rest := base[len(stem):]
	return rest == "" || strings.ContainsRune("~.+", rune(rest[0]))
}

// HeldRecords 是一種資料被 pack_only（或 pack_except，PKG-71）擋下、沒進官方角色包的紀錄。同一種資料兩種都有時分兩筆。
type HeldRecords struct {
	Kind string   `json:"kind"`           // 完整種類名稱
	What string   `json:"what"`           // 白話：這些是什麼（套件的 pack_only_note／pack_except_note；沒寫是通用的說法）
	Rule string   `json:"rule,omitempty"` // 套件宣告的條件（例：source 是 official、state 是 active／retired；pack_except 的是 private 是 true）
	IDs  []string `json:"ids"`            // 編號，排序過
	// Except＝是 pack_except 擋下的（Rule 是「不帶哪些」）；否則是 pack_only 或上層沒進包（Rule 是「只帶哪些」）。
	Except bool `json:"except,omitempty"`
}

// Line 是給人看的一行：幾筆什麼沒帶、編號。
func (h HeldRecords) Line() string {
	verb := "只帶"
	if h.Except {
		verb = "不帶"
	}
	return fmt.Sprintf("%s：%d 筆%s沒帶（套件宣告官方角色包%s %s 的）：%s", h.Kind, len(h.IDs), h.What, verb, h.Rule, strings.Join(h.IDs, "、"))
}

// fieldText 是比 pack_only／pack_except 用的欄位值：字串照原樣、bool 轉成 "true"／"false"（DEV-160）；其他型別回 false。
func fieldText(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}

// PackAllows：這筆紀錄可不可以進官方角色包——符合宣告的 pack_only（PKG-68：每個列出的欄位的值都在可以公開的清單裡），
// 而且沒被 pack_except 擋下（PKG-71，見 PackExcepts）。
// pack_only 沒宣告＝都可以；少了欄位、值不是字串或 bool、不在清單裡＝不可以（判斷不了就不公開）。核心只比宣告寫的欄位與值，不認得它們的意思。
func PackAllows(decl sdk.DataKindDecl, fields map[string]any) bool {
	for name, allowed := range decl.PackOnly {
		v, ok := fieldText(fields[name])
		if !ok || !slices.Contains(allowed, v) {
			return false
		}
	}
	return !PackExcepts(decl, fields)
}

// PackExcepts：這筆紀錄是不是被宣告的 pack_except（PKG-71）擋下——任何一個列出的欄位的值在不公開的清單裡。
// 欄位沒填＝不擋（例：衣服沒寫 private 就照 pack_only 決定）；有值但不是字串或 bool（判斷不了）＝擋。沒宣告＝都不擋。
func PackExcepts(decl sdk.DataKindDecl, fields map[string]any) bool {
	for name, blocked := range decl.PackExcept {
		raw, ok := fields[name]
		if !ok || raw == nil {
			continue
		}
		if v, ok := fieldText(raw); !ok || slices.Contains(blocked, v) {
			return true
		}
	}
	return false
}

// packRule 把「欄位名 → 值」（pack_only、pack_except、export_only）寫成一句（欄位照名字排）：`source 是 official、state 是 active／retired`。
func packRule(m map[string][]string) string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, name+" 是 "+strings.Join(m[name], "／"))
	}
	return strings.Join(parts, "、")
}

// heldWhat 是被擋下的紀錄的白話名稱。except＝pack_except 擋下的：套件寫的 pack_except_note，沒寫就說「主人標了不公開的「<種類的名稱>」」；
// 其他：套件寫的 pack_only_note，沒寫就說「不在公開條件裡的「<種類的名稱>」」。
func heldWhat(kind string, decl sdk.DataKindDecl, except bool) string {
	title := decl.Title
	if title == "" {
		title = kind
	}
	if except {
		if decl.PackExceptNote != "" {
			return decl.PackExceptNote
		}
		return "主人標了不公開的「" + title + "」"
	}
	if decl.PackOnlyNote != "" {
		return decl.PackOnlyNote
	}
	return "不在公開條件裡的「" + title + "」"
}

// CollectRecords 照套件的宣告收 slugs 這幾位角色的紀錄：by_persona 看角色欄位；by_folder 呼叫 folder（資料夾是 personas/ 底下的相對路徑），
// 它把資料夾裡這幾位的圖收進去、回傳有沒有收到，有才帶這一筆。folder 是 nil＝建官方角色包：by_folder 的種類整個不收（PKG-59），每種列一行原因；
// by_persona 的種類再照宣告的 pack_only、pack_except 濾（PKG-68、PKG-71：不在可以公開的值裡、或在不公開的值裡的不帶，記在 Held 與 Skipped）。主人點名的角色匯出（folder 不是 nil）不看 pack_only。
// 宣告 export_omit 的欄位與核心管的欄位都拿掉。store 是 nil 時什麼都不收。
func CollectRecords(ctx context.Context, store Store, slugs []string, folder func(dir string) bool) (*CollectResult, error) {
	res := &CollectResult{Records: []Record{}}
	if store == nil {
		return res, nil
	}
	kinds, err := store.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	// 有上層（pack_parent）的種類排在後面：先知道上層哪幾筆進了包。
	sort.Slice(kinds, func(i, j int) bool {
		if pi, pj := kinds[i].Decl.PackParent != nil, kinds[j].Decl.PackParent != nil; pi != pj {
			return pj
		}
		return kinds[i].Kind < kinds[j].Kind
	})
	isSlug := map[string]bool{}
	for _, s := range slugs {
		isSlug[s] = true
	}
	packed := map[string]bool{}        // 進了這次的 <種類>/<編號>（給 pack_parent 查）
	packedIDs := map[string][]string{} // 種類 → 進了這次的編號（照編號排；給 Bare 查）
	titles := map[string]string{}
	for _, k := range kinds {
		titles[k.Kind] = kindTitle(k.Kind, k.Decl)
	}
	for _, k := range kinds {
		if k.Decl.Export == "" {
			continue
		}
		if k.Decl.Export == sdk.DataExportByPersona && k.Decl.FileField != "" {
			res.FileKinds = true
		}
		parentKind, parentField := "", ""
		if p := k.Decl.PackParent; p != nil && folder == nil {
			parentKind, parentField = packageOf(k.Kind)+"."+p.Kind, p.Field
		}
		if k.Decl.Export == sdk.DataExportByFolder && folder == nil {
			res.Skipped = append(res.Skipped, k.Kind+"：跟著資料夾走的紀錄（使用者自己定義的圖組）不進官方角色包，只有主人點名的角色匯出才帶")
			continue
		}
		list, err := store.List(ctx, k.Kind)
		if folder == nil && len(k.Decl.PackExcept) > 0 {
			list, err = listWithArchived(ctx, store, k.Kind) // 封存的、主人標了不公開的，它的圖也要擋（下面）
		}
		if err != nil {
			return nil, err
		}
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		n := 0
		var held, heldExcept, notExported []string
		children := map[string]int{} // 上層編號 → 這種下層有幾筆（沒封存的；不管跟不跟著走）
		for _, rec := range list {
			if !recordIDPattern.MatchString(rec.ID) {
				continue
			}
			if rec.Archived {
				// 封存的紀錄不帶；但主人標了不公開的（pack_except）它的圖照樣不收（不然 wardrobe/<代號>/<衣服>/ 的檔會照資料夾被收進包）。
				if s, _ := rec.Fields[k.Decl.PersonaField].(string); folder == nil && k.Decl.Export == sdk.DataExportByPersona && isSlug[s] && PackExcepts(k.Decl, rec.Fields) {
					if f, ok := recordFile(k.Kind, k.Decl, rec); ok {
						res.HeldFiles = append(res.HeldFiles, f)
					} else if k.Decl.FileField == "" && k.Decl.PersonaID == sdk.DataPersonaIDPrefix {
						if rest, ok := strings.CutPrefix(rec.ID, s+"-"); ok && rest != "" {
							res.HeldMedia = append(res.HeldMedia, HeldMedia{Slug: s, Rest: rest, Key: k.Kind + "/" + rec.ID})
						}
					}
				}
				continue
			}
			if parentKind != "" {
				if parent, _ := rec.Fields[parentField].(string); parent != "" {
					children[parent]++
				}
			}
			switch k.Decl.Export {
			case sdk.DataExportByPersona:
				if s, _ := rec.Fields[k.Decl.PersonaField].(string); !isSlug[s] {
					continue
				}
				if !ExportAllows(k.Decl, rec.Fields) {
					notExported = append(notExported, rec.ID) // 不跟著角色走：紀錄與檔都不帶（不算在 Held：匯出也不帶）
					if f, ok := recordFile(k.Kind, k.Decl, rec); ok {
						res.NotExportedFiles = append(res.NotExportedFiles, f)
					}
					continue
				}
				parent, _ := rec.Fields[parentField].(string)
				if folder == nil && (!PackAllows(k.Decl, rec.Fields) || (parentKind != "" && !packed[parentKind+"/"+parent])) {
					if PackExcepts(k.Decl, rec.Fields) {
						heldExcept = append(heldExcept, rec.ID) // 主人標了不公開（pack_except）：輸出分開說
					} else {
						held = append(held, rec.ID)
					}
					if f, ok := recordFile(k.Kind, k.Decl, rec); ok {
						res.HeldFiles = append(res.HeldFiles, f)
					} else if s, _ := rec.Fields[k.Decl.PersonaField].(string); k.Decl.FileField == "" && k.Decl.PersonaID == sdk.DataPersonaIDPrefix {
						if rest, ok := strings.CutPrefix(rec.ID, s+"-"); ok && rest != "" {
							res.HeldMedia = append(res.HeldMedia, HeldMedia{Slug: s, Rest: rest, Key: k.Kind + "/" + rec.ID})
						}
					}
					continue
				}
				if f, ok := recordFile(k.Kind, k.Decl, rec); ok {
					res.Files = append(res.Files, f)
				}
			case sdk.DataExportByFolder:
				dir, _ := rec.Fields[k.Decl.FolderField].(string)
				dir = strings.Trim(dir, "/")
				if dir == "" || !CleanPath(dir) {
					res.Skipped = append(res.Skipped, fmt.Sprintf("%s/%s：資料夾「%s」不對，沒帶", k.Kind, rec.ID, dir))
					continue
				}
				if !folder(dir) {
					continue
				}
			default:
				continue
			}
			omit := k.Decl.ExportOmit
			if folder == nil && len(k.Decl.PackExcept) > 0 {
				// 進官方角色包的一定是不擋的值：pack_except 的欄位不帶（收件的家沒這欄＝不擋；還沒升級、規格不認得這欄的家也不會整包不收，PKG-71）。
				omit = append(slices.Clone(omit), slices.Sorted(maps.Keys(k.Decl.PackExcept))...)
			}
			res.Records = append(res.Records, Record{Kind: k.Kind, ID: rec.ID, Fields: exportFields(rec.Fields, omit), Body: rec.Body})
			packed[k.Kind+"/"+rec.ID] = true
			packedIDs[k.Kind] = append(packedIDs[k.Kind], rec.ID)
			n++
		}
		if n > 0 {
			res.Kinds = append(res.Kinds, k.Kind)
		}
		if parentKind != "" && k.Decl.FileField != "" && k.Decl.Export == sdk.DataExportByPersona {
			// 進了包的上層，這種照紀錄帶檔的下層一筆都沒有：它的檔一個也帶不走（建包不建，見 errBare）。
			var bare []string
			for _, id := range packedIDs[parentKind] {
				if children[id] == 0 {
					bare = append(bare, id)
				}
			}
			if len(bare) > 0 {
				res.Bare = append(res.Bare, BareRecords{Kind: parentKind, Title: titles[parentKind], Child: k.Kind, ChildTitle: titles[k.Kind], IDs: bare})
			}
		}
		if len(notExported) > 0 {
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s：%d 筆不跟著角色走（套件宣告只帶 %s 的），紀錄與檔都沒帶：%s",
				k.Kind, len(notExported), packRule(k.Decl.ExportOnly), strings.Join(notExported, "、")))
		}
		if len(held) > 0 {
			rule := packRule(k.Decl.PackOnly)
			if parentKind != "" {
				rule = strings.TrimPrefix(rule+"、上層 "+parentKind+"（欄位 "+parentField+"）有進包", "、")
			}
			h := HeldRecords{Kind: k.Kind, What: heldWhat(k.Kind, k.Decl, false), Rule: rule, IDs: held}
			res.Held = append(res.Held, h)
			res.Skipped = append(res.Skipped, h.Line())
		}
		if len(heldExcept) > 0 {
			h := HeldRecords{Kind: k.Kind, What: heldWhat(k.Kind, k.Decl, true), Rule: packRule(k.Decl.PackExcept), IDs: heldExcept, Except: true}
			res.Held = append(res.Held, h)
			res.Skipped = append(res.Skipped, h.Line())
		}
	}
	return res, nil
}

// FolderDirs 列出這個家所有跟著資料夾走（by_folder）的紀錄指的資料夾：personas/ 底下的相對路徑，小寫、整理過（path.Clean、去掉頭尾的 /）、排序不重複。
// Store 有 ArchivedLister 就連封存的一起算。建官方角色包時這些資料夾裡的檔一律不收（PRS-4）：那是使用者自己定義的圖組（例如衣櫃的隱藏版），
// 可能放不公開的版本；核心只看宣告（export、folder_field），不認得是哪個套件。資料夾欄位是空的不算（沒指任何資料夾）。
func FolderDirs(ctx context.Context, store Store) ([]string, error) {
	kinds, err := store.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		if k.Decl.Export != sdk.DataExportByFolder || k.Decl.FolderField == "" {
			continue
		}
		var list []sdk.Record
		if al, ok := store.(ArchivedLister); ok {
			list, err = al.ListWithArchived(ctx, k.Kind)
		} else {
			list, err = store.List(ctx, k.Kind)
		}
		if err != nil {
			return nil, err
		}
		for _, rec := range list {
			dir, _ := rec.Fields[k.Decl.FolderField].(string)
			if dir = strings.Trim(strings.TrimSpace(dir), "/"); dir == "" {
				continue
			}
			seen[strings.ToLower(path.Clean(dir))] = true
		}
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out, nil
}

// exportFields 拿掉核心管的欄位、值是 null 的、與 omit 列的欄位；回新的 map。
func exportFields(fields map[string]any, omit []string) map[string]any {
	drop := map[string]bool{}
	for _, n := range omit {
		drop[n] = true
	}
	out := map[string]any{}
	for k, v := range fields {
		if !coreFields[k] && !drop[k] && v != nil {
			out[k] = v
		}
	}
	return out
}

// RecordHash 是一筆紀錄內容（套件的欄位＋內文，不含核心管的與 omit 列的欄位）的雜湊：分辨「一樣」「上次寫進來之後沒人改過」。
func RecordHash(fields map[string]any, body string, omit []string) string {
	data, _ := json.Marshal(struct {
		Fields map[string]any `json:"fields"`
		Body   string         `json:"body"`
	}{exportFields(fields, omit), strings.TrimSpace(body)})
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// BadRecord 是檔案（匯出檔或角色包）裡的紀錄本身不合格：呼叫者整個不收，說「什麼都沒寫」。
type BadRecord struct{ Msg string }

func (e *BadRecord) Error() string { return e.Msg }

func badRecord(format string, a ...any) error { return &BadRecord{Msg: fmt.Sprintf(format, a...)} }

// PlanInput 是 PlanRecords 的參數。
type PlanInput struct {
	Records []Record
	Slugs   []string // 這個檔案帶的角色：by_persona 的角色欄位要是其中一位
	// FolderOK：by_folder 的資料夾（personas/ 底下）合規定、而且這個檔案帶了那個資料夾裡她的圖。
	FolderOK func(dir string) bool
	// Registry 是上一次寫進來的每筆紀錄的雜湊（<種類>/<編號> → RecordHash）；跟本機一樣＝沒人改過，可以換。
	Registry map[string]string
	Force    bool // 改過的也換（換之前 Backup）
	// PersonaOnly：只收 by_persona 的紀錄，by_folder 的略過並列原因（官方角色包，PKG-59；舊的包萬一帶了也不寫）；
	// 這個家的套件宣告了 pack_only（PKG-68）或 pack_except（PKG-71）時，不在可以公開的值裡、或在不公開的值裡的也略過並列原因（舊的包、別人建的包萬一帶了也不寫）。
	PersonaOnly bool
	// 紀錄指著的檔（宣告 file_field 的種類，DEV-160 審查 M1）：Carried＝檔案（匯出檔、角色包）帶了家裡這個位置（personas/…）的檔；
	// Occupied＝這個家這個位置已經有東西；Foreign 回這個位置照檔名是這個家哪一位別的角色的（空＝不是）。有給 Carried 時，
	// 沒帶檔的紀錄不能指著這個家已經有的檔或別筆紀錄的檔（不然一筆手改的紀錄就能把收件家的隱藏版指成要跟著走的）：略過並列原因。
	Carried  func(rel string) bool
	Occupied func(rel string) bool
	Foreign  func(rel string) string
	// Blocked 回這個家的紀錄說這個位置不能跟著這次的角色走的原因（FileIndex.Blocked：隱藏版、別的角色、沒寫角色的紀錄的檔）；
	// 空＝不擋。帶來的紀錄指著這種位置就不收，帶了檔也一樣（不然一筆一般的紀錄就把收件家的隱藏版認成要跟著走的，DEV-160 第二輪審查）。
	Blocked func(rel string) string
}

// RecordPlan 是驗過、算好的紀錄寫入計畫。Added、Replaced、Kept、Skipped 是 `<種類>/<編號>`（Skipped 附原因）。
type RecordPlan struct {
	Added     []string
	Replaced  []string
	Unchanged int
	Kept      []string
	Skipped   []string
	// Files 是收了的紀錄（新增、換掉、一樣的；你改過沒換的只在本機那筆也指著同一個檔時）照這個家的宣告（file_field）指著的檔：
	// <種類>/<編號> → 家裡的位置（RecordFile.Rel）。匯入時檔名看不出是誰的檔（file_records，DEV-160 S3）照這個驗：紀錄沒收的，它的檔也不收。
	Files map[string]string
	// Taken 是這個家的紀錄（宣告 file_field 的每一種、封存的也算）指著的檔：家裡的位置（小寫）→ 指著它的 <種類>/<編號>（排序過）。
	// 匯入時檔名看不出是誰的檔不能是這個家別筆紀錄的檔（不然一筆自己的紀錄就能蓋掉別的角色的照片）。
	Taken map[string][]string
	// Dropped 是官方角色包裡因為這個家的公開條件（pack_only、pack_except）或上層沒收（pack_parent）而略過的紀錄指著的檔：
	// 家裡的位置（RecordFile.Rel）。同步不寫這些檔（舊版程式建的包、別人做的包萬一帶了，紀錄不收、圖也不放，PKG-71）。
	Dropped []string
	jobs    []recordJob
	same    map[string]string // 已經一樣的：登記它的雜湊
}

type recordJob struct {
	Record
	decl   sdk.DataKindDecl
	omit   []string
	rev    string      // 已存在那筆的 revision（取代時帶）
	old    *sdk.Record // 已存在的那筆
	edited bool
}

// PlanRecords 照 D-D 驗每一筆、算出怎麼寫（還沒寫）：種類要在這個家註冊中、而且這個家的套件也說它跟著角色走（沒裝、停用、沒宣告的略過並列出）；
// 紀錄要屬於檔案帶的角色、欄位照這個家的規格（不合格回 *BadRecord，整個不收）；已經有的：一樣的不寫、上一次寫進來之後沒人改過的換、
// 改過的不蓋（Force 才蓋）、封存過的編號不重用（連 Force 也不寫）。這個家宣告 export_omit 的欄位：檔案裡帶了也不收，換的時候保留本機的值。
func PlanRecords(ctx context.Context, store Store, in PlanInput) (*RecordPlan, error) {
	p := &RecordPlan{same: map[string]string{}, Files: map[string]string{}, Taken: map[string][]string{}}
	if len(in.Records) == 0 {
		return p, nil
	}
	if store == nil {
		return nil, platform.Unavailable("讀不到這個家的套件資料（EverGenie 沒在跑？）")
	}
	list, err := store.Kinds(ctx)
	if err != nil {
		return nil, err
	}
	kinds := map[string]sdk.DataKindDecl{}
	for _, k := range list {
		kinds[k.Kind] = k.Decl
	}
	isSlug := map[string]bool{}
	for _, s := range in.Slugs {
		isSlug[s] = true
	}
	// 官方角色包：先找出照這個家的公開條件不收的紀錄，下層（pack_parent）指著它們的也不收，免得留下沒有衣服的照片。
	held := map[string]bool{}
	if in.PersonaOnly {
		for _, rec := range in.Records {
			if decl, ok := kinds[rec.Kind]; ok && decl.Export == sdk.DataExportByPersona && !PackAllows(decl, exportFields(rec.Fields, decl.ExportOmit)) {
				held[rec.Key()] = true
			}
		}
	}
	drop := func(rec Record, decl sdk.DataKindDecl) {
		if f, ok := recordFile(rec.Kind, decl, sdk.Record{Kind: rec.Kind, ID: rec.ID, Fields: rec.Fields}); ok {
			p.Dropped = append(p.Dropped, f.Rel())
		}
	}
	var jobs []recordJob
	for _, rec := range in.Records {
		decl, ok := kinds[rec.Kind]
		switch {
		case !ok:
			p.Skipped = append(p.Skipped, rec.Key()+"："+packageOf(rec.Kind)+" 套件沒裝或停用")
			continue
		case decl.Export == "":
			p.Skipped = append(p.Skipped, rec.Key()+"：這個家的 "+packageOf(rec.Kind)+" 套件沒說這種資料能跟著角色匯入")
			continue
		}
		rec.Fields = exportFields(rec.Fields, decl.ExportOmit)
		switch decl.Export {
		case sdk.DataExportByPersona:
			if s, _ := rec.Fields[decl.PersonaField].(string); !isSlug[s] {
				return nil, badRecord("紀錄「%s」不屬於這次的角色", rec.Key())
			}
			if !ExportAllows(decl, rec.Fields) { // 匯出與建包本來就不帶；手改過的檔帶了也不收（DEV-160：隱藏版的照片紀錄）
				p.Skipped = append(p.Skipped, rec.Key()+"：這個家的 "+packageOf(rec.Kind)+" 套件說這種紀錄不跟著角色走")
				continue
			}
			if in.PersonaOnly && !PackAllows(decl, rec.Fields) {
				p.Skipped = append(p.Skipped, rec.Key()+"："+heldWhat(rec.Kind, decl, PackExcepts(decl, rec.Fields))+"，這個家的 "+packageOf(rec.Kind)+" 套件說不從官方角色包收")
				drop(rec, decl)
				continue
			}
			if pp := decl.PackParent; in.PersonaOnly && pp != nil {
				parent, _ := rec.Fields[pp.Field].(string)
				if key := packageOf(rec.Kind) + "." + pp.Kind + "/" + parent; held[key] {
					p.Skipped = append(p.Skipped, rec.Key()+"：上層 "+key+" 這個家不從官方角色包收，這筆與它的圖也不收")
					drop(rec, decl)
					continue
				}
			}
		case sdk.DataExportByFolder:
			if in.PersonaOnly {
				p.Skipped = append(p.Skipped, rec.Key()+"：跟著資料夾走的紀錄不從官方角色包收")
				continue
			}
			// 跟收的時候同一個規則：資料夾裡要有檔案裡帶來的、列出角色的圖，這筆才是跟著她來的。
			d, _ := rec.Fields[decl.FolderField].(string)
			d = strings.Trim(d, "/")
			if d == "" || !CleanPath(d) || in.FolderOK == nil || !in.FolderOK(d) {
				return nil, badRecord("紀錄「%s」的資料夾不對，或檔案裡沒有帶那個資料夾裡她的圖", rec.Key())
			}
		default:
			p.Skipped = append(p.Skipped, rec.Key()+"：這個家的 "+packageOf(rec.Kind)+" 套件的匯出方式不認得")
			continue
		}
		if err := checkFields(decl, rec); err != nil {
			return nil, err
		}
		if f, ok := recordFile(rec.Kind, decl, sdk.Record{Kind: rec.Kind, ID: rec.ID, Fields: rec.Fields}); ok && decl.Export == sdk.DataExportByPersona {
			p.Files[rec.Key()] = f.Rel()
		}
		jobs = append(jobs, recordJob{Record: rec, decl: decl, omit: decl.ExportOmit})
	}
	if len(p.Files) > 0 {
		for _, k := range list {
			if k.Decl.FileField == "" || k.Decl.Export != sdk.DataExportByPersona {
				continue
			}
			recs, err := listWithArchived(ctx, store, k.Kind)
			if err != nil {
				return nil, err
			}
			for _, r := range recs {
				if rel, ok := indexRel(k.Decl, r); ok {
					p.Taken[rel] = append(p.Taken[rel], k.Kind+"/"+r.ID)
				}
			}
		}
		for _, keys := range p.Taken {
			sort.Strings(keys)
		}
		jobs = p.dropUnsafeFiles(jobs, in)
	}
	for _, j := range jobs {
		old, err := store.Get(ctx, j.Kind, j.ID)
		if err != nil {
			return nil, err
		}
		h := RecordHash(j.Fields, j.Body, j.omit)
		switch {
		case old == nil:
			p.Added = append(p.Added, j.Key())
			p.jobs = append(p.jobs, j)
			continue
		case old.Archived:
			p.Skipped = append(p.Skipped, j.Key()+"：這個家已經封存了這一筆，編號不能重用")
			delete(p.Files, j.Key())
			continue
		}
		local := RecordHash(old.Fields, old.Body, j.omit)
		j.rev, j.old = old.Revision, old
		switch {
		case local == h:
			p.Unchanged++
			p.same[j.Key()] = h
		case in.Registry[j.Key()] != "" && in.Registry[j.Key()] == local:
			p.Replaced = append(p.Replaced, j.Key())
			p.jobs = append(p.jobs, j)
		case in.Force:
			j.edited = true
			p.Replaced = append(p.Replaced, j.Key())
			p.jobs = append(p.jobs, j)
		default:
			p.Kept = append(p.Kept, j.Key())
			// 本機那筆改過、沒換：它指著別的檔的話，檔案裡帶來的那個檔也不收（家裡沒有紀錄指著它）。
			if want, ok := p.Files[j.Key()]; ok {
				if f, ok := recordFile(j.Kind, j.decl, *old); !ok || f.Rel() != want {
					delete(p.Files, j.Key())
				}
			}
		}
	}
	sort.Strings(p.Kept)
	return p, nil
}

// dropUnsafeFiles 拿掉指著不該指的檔的紀錄（略過並列原因，它的檔也不收）：照檔名是這個家別的角色的檔；
// 有給 Carried 時，檔案沒帶檔、卻指著這個家別筆紀錄的檔或這個家已經有的檔（自己那筆本來就指著它的不算）。
func (p *RecordPlan) dropUnsafeFiles(jobs []recordJob, in PlanInput) []recordJob {
	out := jobs[:0]
	for _, j := range jobs {
		rel, ok := p.Files[j.Key()]
		if !ok {
			out = append(out, j)
			continue
		}
		why := ""
		var others []string
		mine := false
		for _, k := range p.Taken[strings.ToLower(rel)] {
			if k == j.Key() {
				mine = true
			} else {
				others = append(others, k)
			}
		}
		switch {
		case in.Foreign != nil && in.Foreign(rel) != "":
			why = "指著的 " + rel + " 照檔名是這個家另一位角色（" + in.Foreign(rel) + "）的，不收"
		case in.Blocked != nil && in.Blocked(rel) != "":
			why = "指著的 " + rel + " " + in.Blocked(rel) + "，不收"
		case in.Carried == nil || in.Carried(rel):
		case len(others) > 0:
			why = "指著這個家紀錄 " + strings.Join(others, "、") + " 的檔 " + rel + "（檔案裡沒帶這個檔），不收"
		case !mine && in.Occupied != nil && in.Occupied(rel):
			why = "指著這個家已經有的 " + rel + "（檔案裡沒帶這個檔），不收"
		}
		if why == "" {
			out = append(out, j)
			continue
		}
		p.Skipped = append(p.Skipped, j.Key()+"："+why)
		delete(p.Files, j.Key())
	}
	return out
}

// DropFileRecords：帶來的檔沒寫進 rel（這個家的檔內容不一樣、留著），要寫的紀錄裡新指著 rel 的也不寫——
// 不然那筆紀錄指著的是這個家原本的檔（主人自己的、或別的照片），之後的匯出、建包會把它當成那筆紀錄的照片帶走（DEV-160 第二輪審查）。
// 本機那筆原本就指著 rel 的照樣換（指著的檔沒變）。回拿掉的 <種類>/<編號>，略過的原因記在 Skipped。
func (p *RecordPlan) DropFileRecords(rel, why string) []string {
	low := strings.ToLower(rel)
	var dropped []string
	kept := p.jobs[:0]
	for _, j := range p.jobs {
		want, ok := p.Files[j.Key()]
		if !ok || strings.ToLower(want) != low {
			kept = append(kept, j)
			continue
		}
		if j.old != nil {
			if f, ok := recordFile(j.Kind, j.decl, *j.old); ok && strings.ToLower(f.Rel()) == low {
				kept = append(kept, j)
				continue
			}
		}
		dropped = append(dropped, j.Key())
		p.Skipped = append(p.Skipped, j.Key()+"：指著的 "+rel+" "+why+"，紀錄也不收")
		delete(p.Files, j.Key())
	}
	p.jobs = kept
	if len(dropped) > 0 {
		p.Added = slices.DeleteFunc(p.Added, func(k string) bool { return slices.Contains(dropped, k) })
		p.Replaced = slices.DeleteFunc(p.Replaced, func(k string) bool { return slices.Contains(dropped, k) })
	}
	return dropped
}

// Backup 把要蓋掉的、改過的紀錄存成 JSON：<dir>/records/<種類>/<編號>.json。dir 由 ensure 給（第一次用到才建，沒有要備份的就不建）。
func (p *RecordPlan) Backup(ensure func() string) error {
	for _, j := range p.jobs {
		if !j.edited {
			continue
		}
		data, _ := json.MarshalIndent(j.old, "", "  ")
		f := filepath.Join(ensure(), "records", j.Kind, j.ID+".json")
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
			return fmt.Errorf("無法備份紀錄 %s", j.Key())
		}
		if err := platform.AtomicWrite(f, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("無法備份紀錄 %s", j.Key())
		}
	}
	return nil
}

// Write 照計畫寫進 store，並把寫進去的與已經一樣的登記到 reg（<種類>/<編號> → RecordHash）。取代的時候，宣告 export_omit 的欄位保留本機的值。
// 萬一有一筆寫不進去，回傳已經寫進去的（written）與那個錯誤；呼叫者照實說，已寫的照樣登記。
func (p *RecordPlan) Write(ctx context.Context, store Store, reg map[string]string) (written []string, failed string, err error) {
	for k, h := range p.same {
		reg[k] = h
	}
	for _, j := range p.jobs {
		fields := map[string]any{}
		for k, v := range j.Fields {
			fields[k] = v
		}
		if j.old != nil {
			for _, n := range j.omit {
				if v, ok := j.old.Fields[n]; ok && v != nil {
					fields[n] = v
				}
			}
		}
		if err := store.Put(ctx, sdk.Record{Kind: j.Kind, ID: j.ID, Fields: fields, Body: j.Body}, j.rev); err != nil {
			return written, j.Key(), err
		}
		written = append(written, j.Key())
		reg[j.Key()] = RecordHash(fields, j.Body, j.omit)
	}
	return written, "", nil
}

// ErrText 是錯誤給人看的那一句（platform.Err 只取訊息）。
// IsUnavailable：連不到或還沒準備好（platform 的錯誤碼，或經 API 回來的 sdk 錯誤碼）。
func IsUnavailable(err error) bool {
	if platform.CodeOf(err) == sdk.ErrorCodeUnavailable {
		return true
	}
	var se *sdk.Error
	return errors.As(err, &se) && se.Code == sdk.ErrorCodeUnavailable
}

func ErrText(err error) string {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		return pe.Message
	}
	return err.Error()
}

func packageOf(kind string) string { p, _, _ := strings.Cut(kind, "."); return p }

// checkFields 照這個家的欄位規格先驗一筆紀錄（不認得的欄位、缺必填、型別、enum），全部驗過才開始寫（D-D）。
// 這裡只擋明顯不合的；寫入時 vault 會再照規格驗一次。
func checkFields(decl sdk.DataKindDecl, r Record) error {
	known := map[string]sdk.DataFieldDecl{}
	for _, f := range decl.Fields {
		known[f.Name] = f
	}
	for name, v := range r.Fields {
		f, ok := known[name]
		if !ok {
			return badRecord("紀錄「%s」的欄位「%s」這個家的套件不認得", r.Key(), name)
		}
		if v == nil {
			continue
		}
		if !valueFits(f, v) {
			return badRecord("紀錄「%s」的欄位「%s」型別不對（要 %s）", r.Key(), name, f.Type)
		}
	}
	for _, f := range decl.Fields {
		if v, ok := r.Fields[f.Name]; f.Required && (!ok || v == nil || v == "") {
			return badRecord("紀錄「%s」少了必填的欄位「%s」", r.Key(), f.Name)
		}
	}
	return nil
}

func valueFits(f sdk.DataFieldDecl, v any) bool {
	switch f.Type {
	case "string":
		_, ok := v.(string)
		return ok
	case "int":
		n, ok := v.(float64)
		return ok && n == float64(int64(n))
	case "number":
		_, ok := v.(float64)
		return ok
	case "bool":
		_, ok := v.(bool)
		return ok
	case "date":
		s, ok := v.(string)
		_, err := time.Parse("2006-01-02", s)
		return ok && err == nil
	case "datetime":
		s, ok := v.(string)
		_, err := time.Parse(time.RFC3339, s)
		return ok && err == nil
	case "enum":
		s, ok := v.(string)
		for _, e := range f.Enum {
			if ok && s == e {
				return true
			}
		}
		return false
	case "list":
		_, ok := v.([]any)
		return ok
	case "map":
		_, ok := v.(map[string]any)
		return ok
	}
	return true
}
