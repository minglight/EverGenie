// Package personaexport 是角色的匯出與匯入（DEV-116、D-63）：主人在自己的家把點名的角色（卡、圖、衣櫃資料夾、目錄卡，
// 加上套件宣告要跟著角色走的紀錄）打成一個 `.hgpersona` 檔，對方的主人在他的家匯入。家與家不連線（法律第 7 條）。
// 路徑檢查、sha256、換檔與套件紀錄的收、驗、寫入都跟角色包（personapack）共用；套件紀錄經 Store（指令行程用 sdk client，核心用模組），
// 核心不認得任何套件：要不要帶、怎麼帶只看套件的 `contributes.data` 宣告（PKG-57、PKG-58）。格式見 ai_docs/schemas/persona-pack.md「角色匯出檔」。
package personaexport

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

const (
	// Kind 與 Schema 是 manifest.json 的固定欄位。
	Kind   = "evergenie.persona-export"
	Schema = 1
	// Ext 是匯出檔的副檔名。
	Ext = ".hgpersona"

	manifestName = "manifest.json"
	filesPrefix  = "files/"
	extraPrefix  = "extra/"
	maxManifest  = 16 << 20
	maxFiles     = 20000
	maxTotal     = sdk.PersonaImportMaxBytes // 整個檔解開的上限（暫存解一份、換檔再寫一份）；匯出檔本身與看板上傳也是這個數字（PRS-18）
)

// Manifest 是匯出檔裡的 manifest.json。
type Manifest struct {
	Kind       string             `json:"kind"`
	Schema     int                `json:"schema"`
	ExportedAt string             `json:"exported_at"`
	Personas   []string           `json:"personas"`
	Files      []personapack.File `json:"files"`
	Records    []Record           `json:"records"`
	// FileRecords 是照紀錄帶的檔屬於哪一筆紀錄（邏輯路徑 → <種類>/<編號>，DEV-160 S3）：檔名看不出是誰的（例如衣櫃新上傳的
	// photos/<照片編號>.<ext>）時，靠帶著它的那筆紀錄認是誰的；只列檔名認不出來的。舊的匯出檔沒有這一欄。
	FileRecords map[string]string `json:"file_records,omitempty"`
}

// 紀錄、種類、Store 跟官方角色包共用（personapack/records.go）：收、驗、比對、寫入只有一份。
type (
	Record   = personapack.Record
	KindInfo = personapack.KindInfo
	Store    = personapack.Store
)

var mediaExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".mp4": true}

// target 把匯出檔的邏輯路徑換成家裡的相對路徑：角色包的四種位置（personapack.Target），加上 `extra/<personas 底下的路徑>`。
// extra 只收兩種：使用者自己的角色卡 `extra/mine/<代號>.md`，和圖或影片（png／jpg／jpeg／webp／mp4）；
// 不能是 `_` 開頭的段（`_style.md` 這類全家共用的設定不會被一個匯出檔塞進來）。
func target(logical string) (string, bool) {
	if rel, ok := personapack.Target(logical); ok {
		return rel, true
	}
	if !strings.HasPrefix(logical, extraPrefix) || !personapack.CleanPath(logical) {
		return "", false
	}
	rest := strings.TrimPrefix(logical, extraPrefix)
	for _, seg := range strings.Split(rest, "/") {
		if strings.HasPrefix(seg, "_") {
			return "", false
		}
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[0] == "mine" && strings.HasSuffix(parts[1], ".md") && personaref.ValidSlug(strings.TrimSuffix(parts[1], ".md")) {
		return "personas/" + rest, true
	}
	if len(parts) >= 2 && mediaExt[strings.ToLower(path.Ext(rest))] {
		return "personas/" + rest, true
	}
	return "", false
}

// owner 回傳這個邏輯路徑照名字看屬於哪一位角色（只認 personas 裡的）；不屬於任何一位回空字串。
// 匯出只會產生屬於被匯出角色的檔案，匯入時也照這個擋：一個檔不能夾帶別的角色的東西。名字看不出來的檔要有 file_records 指著
// 匯出檔裡的一筆紀錄（validate 檢查有這一筆；匯入時再照這個家的宣告驗那筆紀錄屬於列出的角色、而且指著這個檔）。
func owner(logical string, personas []string) string {
	parts := strings.Split(logical, "/")
	base := parts[len(parts)-1]
	for _, slug := range personas {
		switch {
		case parts[0] == "cards" && base == slug+".md",
			parts[0] == "catalog" && len(parts) == 2 && base == slug+".md",
			parts[0] == "wardrobe" && len(parts) >= 3 && parts[1] == slug,
			parts[0] == "extra" && len(parts) == 3 && parts[1] == "mine" && base == slug+".md":
			return slug
		case (parts[0] == "avatars" || parts[0] == "extra") && personapack.IsPersonaMedia(base, slug):
			return slug
		}
	}
	return ""
}

// recordPlace：紀錄指著的檔能放的位置（DEV-160 S3、審查 M6）——頭像資料夾（官方的 avatars/…、自己的 extra/mine/avatars/…）、
// 照片資料夾（自己的 extra/mine/photos/<檔>）、衣服資料夾（wardrobe/<代號>/…、extra/mine/wardrobe/<代號>/…）。
// 檔名不是列出的角色的（照片編號、改過代號留著的舊檔名）要有 file_records 指著帶著它的紀錄；匯出時檔名是這個家別的角色的不帶，
// 匯入時檔名是收件家別的角色的也不收（PlanInput.Foreign）。卡、目錄卡、其他 extra/ 底下的位置只照名字認。
func recordPlace(logical string) bool {
	parts := strings.Split(logical, "/")
	switch {
	case parts[0] == "avatars" && len(parts) >= 2, parts[0] == "wardrobe" && len(parts) >= 3:
		return true
	case parts[0] == "extra" && len(parts) >= 3 && parts[1] == "mine":
		return (parts[2] == "avatars" && len(parts) >= 4) || (parts[2] == "photos" && len(parts) == 4) || (parts[2] == "wardrobe" && len(parts) >= 5)
	}
	return false
}

// skipped 是製作用、不跟著走的檔（D-B）：`src/` 底下、`*.locked.*`、`.` 開頭；byName 時名字看起來是自己的隱藏版的也不帶
// （沒有紀錄可讀的檔才照名字猜；紀錄指著的檔照紀錄的 hidden，DEV-160）。rel 是 personas/ 底下的相對路徑。
func skipped(rel string, byName bool) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "src" || strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return (byName && IsAdultPath(rel)) || strings.Contains(path.Base(rel), ".locked.")
}

// validate 檢查 manifest 本身：種類與版本、角色代號、每個檔案（路徑、屬於哪位——照名字，或 file_records 指著檔案裡的一筆紀錄——、
// sha256、大小、不重複、不撞到同一個位置）、每筆紀錄（種類名稱、編號、欄位名稱、不重複）。任何一項不合格整個檔不收。
func (m *Manifest) validate() error {
	if m.Kind != Kind || m.Schema != Schema {
		return fmt.Errorf("這不是 EverGenie 的角色匯出檔（種類或格式版本不對）")
	}
	if len(m.Personas) == 0 {
		return fmt.Errorf("匯出檔裡沒有寫是哪幾位角色")
	}
	seen := map[string]bool{}
	for _, s := range m.Personas {
		if !personaref.ValidSlug(s) || seen[s] {
			return fmt.Errorf("匯出檔裡的角色代號「%s」不對或重複", s)
		}
		seen[s] = true
	}
	recs := map[string]bool{}
	for _, r := range m.Records {
		recs[r.Key()] = true
	}
	paths, targets := map[string]bool{}, map[string]bool{}
	var total int64
	for _, f := range m.Files {
		rel, ok := target(f.Path)
		if !ok {
			return fmt.Errorf("匯出檔裡有不合規定的路徑「%s」", f.Path)
		}
		// 名字認不出來、靠 file_records 的檔只能是圖或影片（avatars/ 底下的位置本身收任何副檔名）
		if owner(f.Path, m.Personas) == "" && (!recordPlace(f.Path) || !recs[m.FileRecords[f.Path]] || !mediaExt[strings.ToLower(path.Ext(f.Path))]) {
			return fmt.Errorf("匯出檔裡的「%s」不屬於檔案裡寫的角色", f.Path)
		}
		if !personapack.IsSHA256(f.SHA256) {
			return fmt.Errorf("匯出檔裡「%s」的 sha256 格式不對", f.Path)
		}
		if f.Size < 0 || f.Size > personapack.MaxFile {
			return fmt.Errorf("匯出檔裡「%s」的大小不合理", f.Path)
		}
		// macOS 的檔名不分大小寫：luna.png 與 luna.PNG 是同一個位置。
		if paths[f.Path] || targets[strings.ToLower(rel)] {
			return fmt.Errorf("匯出檔裡「%s」重複了", f.Path)
		}
		paths[f.Path], targets[strings.ToLower(rel)] = true, true
		total += f.Size
	}
	for p := range m.FileRecords {
		if !paths[p] {
			return fmt.Errorf("匯出檔的 file_records 列了沒帶的檔「%s」", p)
		}
	}
	if total > maxTotal || len(m.Files) > maxFiles {
		return fmt.Errorf("匯出檔解開太大了")
	}
	// 每位角色剛好一張卡：官方位置（cards/）或使用者自己的（extra/mine/），不能兩張都有。
	for _, slug := range m.Personas {
		n := 0
		if paths["cards/"+slug+".md"] {
			n++
		}
		if paths["extra/mine/"+slug+".md"] {
			n++
		}
		if n != 1 {
			return fmt.Errorf("匯出檔裡角色「%s」的卡要剛好一張", slug)
		}
	}
	return personapack.ValidateRecords(m.Records, "匯出檔")
}

// stamp 是備份與暫存資料夾的名字（到秒；同一秒再做一次加 -2、-3）。
func stamp(now time.Time) string { return now.Format("20060102-150405") }

func uniqueDir(base string, exists func(string) bool) string {
	if !exists(base) {
		return base
	}
	for n := 2; ; n++ {
		if p := base + "-" + strconv.Itoa(n); !exists(p) {
			return p
		}
	}
}

// homeRel 把家裡的絕對路徑換回「家底下的相對路徑」（用 /）。
func homeRel(home, p string) string {
	rel, err := filepath.Rel(home, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p // 在家外面（第 2 版的暫存、官方角色）：給完整路徑
	}
	return filepath.ToSlash(rel)
}

// IsAdultPath：personas/ 底下的相對路徑（用 /）是不是自己的隱藏版（personaref.IsAdultPath：level/ 或名字含 adult 這類字）。
// 自己的隱藏版一律不匯出：家設定開著也一樣，只留在這個家。
func IsAdultPath(rel string) bool { return personaref.IsAdultPath(rel) }
