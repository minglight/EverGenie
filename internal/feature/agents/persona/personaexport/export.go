package personaexport

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// ExportOptions 是匯出的參數。
type ExportOptions struct {
	Home     string
	Personas []string // 角色代號（官方位置 personas/<代號>.md 或我的 personas/mine/<代號>.md）
	Store    Store
	Now      time.Time // 零值＝現在
	Bundled  string    // 跟著程式的公開卡資料夾（PRS-25；官方位置沒有、我的也沒有同代號的卡才用，PRS-27）；空＝只看官方位置
}

// ExportResult 是匯出的結果。
type ExportResult struct {
	Personas []string `json:"personas"`
	Files    int      `json:"files"`
	Records  int      `json:"records"`
	Bytes    int64    `json:"bytes"`             // 匯出檔的大小
	Kinds    []string `json:"kinds,omitempty"`   // 帶了哪些種類的紀錄（完整名稱）
	Skipped  []string `json:"skipped,omitempty"` // 刻意沒帶的（白話一行一個）
}

// Message 是給人看的摘要。
func (r *ExportResult) Message() string {
	s := fmt.Sprintf("匯出了 %s：%d 個檔案、%d 筆紀錄", strings.Join(r.Personas, "、"), r.Files, r.Records)
	if len(r.Kinds) > 0 {
		s += "（" + strings.Join(r.Kinds, "、") + "）"
	}
	return s
}

type exportFile struct {
	logical string
	local   string
}

// Export 把角色打成匯出檔寫進 w（zip：檔案在前、manifest.json 在最後，sha256 是寫進去的那份內容算的）。
// 找不到角色、代號不對回 Invalid；套件的角色（<套件>.<代號>）跟著套件走，不從這裡匯出。
func Export(ctx context.Context, o ExportOptions, w io.Writer) (*ExportResult, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	if len(o.Personas) == 0 {
		return nil, platform.Invalid("persona", "要用 --persona 指定匯出哪位角色（可以給好幾次）")
	}
	// at 是家相對 personas/… 的實際位置：mine/ 在家裡，官方的照家的佈局（第 2 版在狀態資料夾）。
	at := func(parts ...string) string {
		return personapack.HomePath(o.Home, path.Join(append([]string{"personas"}, parts...)...))
	}
	var slugs []string
	seen := map[string]bool{}
	for _, s := range o.Personas {
		s = strings.TrimSpace(s)
		if seen[s] {
			continue
		}
		seen[s] = true
		switch {
		case strings.Contains(s, "."):
			return nil, platform.Invalid("persona", fmt.Sprintf("「%s」是套件帶來的角色，跟著套件走（packages export），不用匯出", s))
		case !personaref.ValidSlug(s):
			return nil, platform.Invalid("persona", fmt.Sprintf("角色代號「%s」格式不對（小寫英文與數字，英文開頭）", s))
		}
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)

	res := &ExportResult{Personas: slugs}
	isSlug := map[string]bool{}
	for _, s := range slugs {
		isSlug[s] = true
	}
	// 全家的「檔 → 紀錄」索引（DEV-160 審查 M1）：紀錄只能讓檔更不帶——全家任何一筆不跟著角色走的、或別的角色的紀錄（封存的也算）
	// 指著的檔，不論怎麼被列進來都不帶。照紀錄帶圖（ix.On）時，跟著資料夾走的紀錄指的資料夾（例如衣櫃的隱藏版）裡的檔也一律不帶（M3）。
	ix, folders, err := exportView(ctx, o.Store, o.Home)
	if err != nil {
		return nil, err
	}
	byRecords := ix.On()
	// known：這個家認得的角色（有卡、或有照片紀錄）。檔名是別的角色的檔，紀錄指著也不帶。
	known := func(s string) bool {
		return personapack.HasCard(o.Home, s) || (o.Bundled != "" && isFile(filepath.Join(o.Bundled, s+".md"))) || ix.Persona(s)
	}
	// guard 是一個檔（家裡的位置 personas/…）不能跟著這幾位走的原因；空＝紀錄與名字都不擋。
	guard := func(rel string) string {
		if why := ix.Blocked(rel, isSlug, false); why != "" {
			return why
		}
		if s := personapack.NameOwner(rel); s != "" && !isSlug[s] && known(s) {
			return "檔名是別的角色（" + s + "）的"
		}
		if d := personapack.UnderDir(strings.TrimPrefix(rel, "personas/"), folders); byRecords && d != "" {
			return "在跟著資料夾走的紀錄指的資料夾 " + d + "/ 裡（例如隱藏版）"
		}
		return ""
	}
	var files []exportFile
	targets := map[string]string{} // 家裡的位置（小寫：macOS 的檔名不分大小寫）→ 匯出檔裡的邏輯路徑：同一個檔只帶一次
	// addFile 收一個檔，回傳它在匯出檔裡的邏輯路徑（剛收或之前收過；之前收過的照當時的寫法，大小寫可能跟這次的不同）；空＝沒帶。
	// byRecord＝紀錄指著的檔（DEV-160）：檔名不再照名字猜是不是隱藏版，但放在看起來是隱藏版的資料夾（level/、mine/adult/ 這類）裡的照樣不帶（M3），
	// 也只帶圖與影片（紀錄的 file 是資料、不是保證：不能指著說明檔、設定這類東西）。
	addFile := func(logical, local string, byRecord bool) string {
		rel, ok := target(logical)
		if !ok {
			return ""
		}
		if had := targets[strings.ToLower(rel)]; had != "" {
			return had
		}
		if why := guard(rel); why != "" {
			res.Skipped = append(res.Skipped, logical+"："+why+"，不帶")
			return ""
		}
		info, err := os.Lstat(local)
		if err != nil || !info.Mode().IsRegular() || !realDirs(o.Home, rel) {
			return ""
		}
		if info.Size() > personapack.MaxFile {
			res.Skipped = append(res.Skipped, logical+"：檔案太大")
			return ""
		}
		inner := strings.TrimPrefix(rel, "personas/")
		if skipped(inner, !byRecord) {
			return ""
		}
		if byRecord && !mediaExt[strings.ToLower(path.Ext(inner))] {
			res.Skipped = append(res.Skipped, logical+"：紀錄指著的不是圖或影片，不帶")
			return ""
		}
		if byRecord && IsAdultPath(path.Dir(inner)) {
			res.Skipped = append(res.Skipped, logical+"：在看起來是隱藏版的資料夾裡，不帶")
			return ""
		}
		targets[strings.ToLower(rel)] = logical
		files = append(files, exportFile{logical, local})
		return logical
	}
	add := func(logical, local string) bool { return addFile(logical, local, false) != "" }
	media := func(dir, prefix, slug string) {
		if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
			return
		}
		items, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, it := range items {
			if !it.IsDir() && personapack.IsPersonaMedia(it.Name(), slug) {
				add(prefix+"/"+it.Name(), filepath.Join(dir, it.Name()))
			}
		}
	}
	// 照紀錄帶圖（DEV-160）：這個家有宣告 file_field 的種類（衣櫃的照片紀錄）時，頭像資料夾只帶跟著角色走的紀錄指著的檔，
	// 加上角色本身的頭像（<代號>.<副檔名>）。一筆這種紀錄都沒有的角色（還沒登記的舊家）照檔名找圖（S7）。
	byName := func(slug string) bool { return !byRecords || !ix.HasRecords(slug) }
	for _, slug := range slugs {
		// 找官方卡的共用入口（PRS-25）：跟著程式的那張被我的同代號卡蓋過時不算官方，匯出的是我的（跟角色庫一樣，PRS-27）
		card, official := sdk.OfficialCardIn(platform.OfficialPersonasDir(o.Home), o.Bundled, mineCardsDir(o.Home), slug)
		switch {
		case official && isFile(card):
			add("cards/"+slug+".md", card)
		case isFile(at("mine", slug+".md")):
			add("extra/mine/"+slug+".md", at("mine", slug+".md"))
		default:
			return nil, platform.Invalid("persona", fmt.Sprintf("找不到角色「%s」的卡（personas/%s.md 或 personas/mine/%s.md）", slug, slug, slug))
		}
		if byName(slug) {
			media(at("avatars"), "avatars", slug)
			media(at("avatars", "full"), "avatars/full", slug)
			media(at("mine", "avatars"), "extra/mine/avatars", slug)
			media(at("mine", "avatars", "full"), "extra/mine/avatars/full", slug)
		} else {
			for ext := range mediaExt {
				add("avatars/"+slug+ext, at("avatars", slug+ext))
				add("extra/mine/avatars/"+slug+ext, at("mine", "avatars", slug+ext))
			}
		}
		wardrobe := at("wardrobe", slug)
		_ = filepath.WalkDir(wardrobe, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(wardrobe, p)
			add("wardrobe/"+slug+"/"+filepath.ToSlash(rel), p)
			return nil
		})
		add("catalog/"+slug+".md", at("catalog", slug+".md"))
		if isFile(at("wardrobe", slug+".toml")) {
			res.Skipped = append(res.Skipped, "personas/wardrobe/"+slug+".toml：製作用的衣櫃設定，不帶")
		}
	}

	var records []Record
	var fileRecords map[string]string
	if byRecords {
		got, err := personapack.CollectRecords(ctx, o.Store, slugs, func(dir string) bool {
			// 跟著資料夾走的紀錄（例如衣櫃的隱藏版）：照紀錄帶圖時那筆紀錄與資料夾裡的圖都不跟著走（M3）；資料夾裡有這幾位的圖才列出來。
			if folderHasMedia(o.Home, dir, slugs) {
				res.Skipped = append(res.Skipped, "personas/"+dir+"：跟著資料夾走的紀錄指的資料夾（例如隱藏版），照紀錄帶圖時紀錄與裡面的圖都不匯出")
			}
			return false
		})
		if err != nil {
			return nil, err
		}
		// 紀錄指著的檔：source = mine 的在 personas/mine/（匯出檔裡是 extra/mine/<file>），其他在官方位置（<file>）。
		// 檔名不是這幾位的（衣櫃新上傳的 photos/<照片編號>.<ext>、改過代號留著舊檔名的圖，DEV-160 S3、審查 M6）
		// 記在 manifest 的 file_records（檔 → 帶著它的紀錄），匯入時照那筆紀錄驗。
		for _, f := range got.Files {
			logical := f.File
			if f.Mine {
				logical = extraPrefix + "mine/" + f.File
			}
			if _, ok := target(logical); !ok || !recordPlace(logical) {
				res.Skipped = append(res.Skipped, f.Key+"：檔「"+logical+"」不在角色的圖的位置（avatars/、wardrobe/、照片資料夾），帶不了")
				continue
			}
			// 記在 file_records 的是匯出檔裡真的那個寫法（同一個檔之前照別的大小寫收過時用那個）
			if got := addFile(logical, at(strings.Split(strings.TrimPrefix(f.Rel(), "personas/"), "/")...), true); got != "" && owner(got, slugs) == "" && fileRecords[got] == "" {
				if fileRecords == nil {
					fileRecords = map[string]string{}
				}
				fileRecords[got] = f.Key
			}
		}
		// 指不到檔、檔被擋下的紀錄也不帶（M6）：不然收件的家會多一筆指著空位置的照片紀錄。
		var dropped []string
		records, dropped = personapack.KeepRecordsWithFiles(got.Records, got.Files, ix.FileKind, func(f personapack.RecordFile) bool { return targets[strings.ToLower(f.Rel())] != "" })
		res.Skipped = append(res.Skipped, got.Skipped...)
		res.Skipped = append(res.Skipped, dropped...)
		res.Kinds = kindsOf(got.Kinds, records)
		// 沒有紀錄指著的圖不帶、列出來（S7，跟建包一樣）：剛放進來、衣櫃還沒登記的圖。
		for _, slug := range slugs {
			if byName(slug) {
				continue
			}
			for _, d := range [][2]string{{"avatars", "avatars"}, {"avatars/full", "avatars/full"}, {"mine/avatars", "extra/mine/avatars"}, {"mine/avatars/full", "extra/mine/avatars/full"}} {
				items, _ := os.ReadDir(at(strings.Split(d[0], "/")...))
				for _, it := range items {
					rel := "personas/" + d[0] + "/" + it.Name()
					if it.IsDir() || !personapack.IsPersonaMedia(it.Name(), slug) || targets[strings.ToLower(rel)] != "" || ix.Pointed(rel) || skipped(it.Name(), false) {
						continue
					}
					res.Skipped = append(res.Skipped, d[1]+"/"+it.Name()+"：沒有紀錄指著這個檔（照紀錄帶圖），不帶")
				}
			}
		}
	} else {
		records, err = exportRecords(ctx, o.Store, o.Home, slugs, res, add)
		if err != nil {
			return nil, err
		}
	}

	// 寫 zip：檔案一邊寫一邊算 sha256（寫進去的就是 manifest 記的那份），最後才寫 manifest。
	cw := &countWriter{w: w}
	zw := zip.NewWriter(cw)
	man := Manifest{Kind: Kind, Schema: Schema, ExportedAt: now.Format(time.RFC3339), Personas: slugs, Files: []personapack.File{}, Records: records, FileRecords: fileRecords}
	sort.Slice(files, func(i, j int) bool { return files[i].logical < files[j].logical })
	for _, f := range files {
		sum, size, err := addZipFile(zw, filesPrefix+f.logical, f.local)
		if err != nil {
			return nil, platform.Unavailable("讀不了 " + homeRel(o.Home, f.local))
		}
		man.Files = append(man.Files, personapack.File{Path: f.logical, SHA256: sum, Size: size})
	}
	if err := man.validate(); err != nil {
		return nil, platform.Internal("匯出檔不合格：" + err.Error())
	}
	data, err := json.MarshalIndent(&man, "", "  ")
	if err != nil {
		return nil, platform.Internal("無法產生 manifest")
	}
	mw, err := zw.CreateHeader(&zip.FileHeader{Name: manifestName, Method: zip.Deflate, Modified: now})
	if err != nil {
		return nil, platform.Unavailable("無法寫入匯出檔")
	}
	if _, err := mw.Write(append(data, '\n')); err != nil {
		return nil, platform.Unavailable("無法寫入匯出檔")
	}
	if err := zw.Close(); err != nil {
		return nil, platform.Unavailable("無法寫入匯出檔")
	}
	res.Files, res.Records, res.Bytes = len(man.Files), len(records), cw.n
	return res, nil
}

// exportView 讀這個家的紀錄給匯出挑檔用：全家的「檔 → 紀錄」索引（personapack.IndexFiles），與跟著資料夾走的紀錄指的資料夾
// （解開符號連結）。照紀錄帶圖的種類（宣告 file_field）所在的套件停用、被拒、或讀不懂它的設定時不匯出：讀不到它的紀錄就不知道
// 哪些檔被標成不跟著走（DEV-160 審查 M2，憲法「判斷不了就拒絕」）。store 是 nil（測試）回空的。
func exportView(ctx context.Context, store Store, home string) (*personapack.FileIndex, []string, error) {
	if store == nil {
		return nil, nil, nil
	}
	if ul, ok := store.(personapack.UnreadableLister); ok {
		list, err := ul.Unreadable(ctx)
		if err != nil {
			return nil, nil, err
		}
		var bad []string
		for _, p := range list {
			switch {
			case p.Unknown:
				bad = append(bad, fmt.Sprintf("%s（%s，讀不懂它的設定）", p.ID, p.Why))
			case len(p.Files) > 0:
				bad = append(bad, fmt.Sprintf("%s（%s；%s）", p.ID, p.Why, strings.Join(p.Files, "、")))
			}
		}
		if len(bad) > 0 {
			return nil, nil, platform.Conflict("這個家有套件讀不到：" + strings.Join(bad, "、") + "。它們照紀錄決定哪些圖跟著角色走（例如照片的隱藏版），" +
				"讀不到就判斷不了，沒有匯出。先啟用或修好這些套件（evergenie packages list 看原因）再匯出")
		}
	}
	ix, err := personapack.IndexFiles(ctx, store)
	if err != nil {
		return nil, nil, err
	}
	if !ix.On() {
		return ix, nil, nil
	}
	if err := personapack.UnlistedError(home, ix, "匯出"); err != nil {
		return nil, nil, err
	}
	dirs, err := personapack.FolderDirs(ctx, store)
	if err != nil {
		return nil, nil, err
	}
	return ix, personapack.ResolveDirs(home, dirs), nil
}

// folderHasMedia：personas/<dir>（與底下的 full/）有這幾位角色的圖（檔名照角色代號）。
func folderHasMedia(home, dir string, slugs []string) bool {
	for _, sub := range []string{"", "full"} {
		items, err := os.ReadDir(personapack.HomePath(home, path.Join("personas", dir, sub)))
		if err != nil {
			continue
		}
		for _, it := range items {
			for _, s := range slugs {
				if !it.IsDir() && personapack.IsPersonaMedia(it.Name(), s) {
					return true
				}
			}
		}
	}
	return false
}

// kindsOf 是 kinds 裡還有紀錄留在 records 的種類（順序照 kinds）。
func kindsOf(kinds []string, records []Record) []string {
	has := map[string]bool{}
	for _, r := range records {
		has[r.Kind] = true
	}
	var out []string
	for _, k := range kinds {
		if has[k] {
			out = append(out, k)
		}
	}
	return out
}

// exportRecords 照套件的宣告（PKG-57、PKG-58）收紀錄（這個家沒有照紀錄帶圖的種類時）：by_folder 時把資料夾裡被匯出角色的圖一起帶（extra/<資料夾>/…）；
// 名字看起來是隱藏版的資料夾不帶。
func exportRecords(ctx context.Context, store Store, home string, slugs []string, res *ExportResult, add func(logical, local string) bool) ([]Record, error) {
	folder := func(dir string) bool {
		if IsAdultPath(dir) {
			res.Skipped = append(res.Skipped, "personas/"+dir+"：自己的隱藏版不匯出")
			return false
		}
		found := false
		for _, sub := range []string{"", "full"} {
			d := personapack.HomePath(home, path.Join("personas", dir, sub))
			items, err := os.ReadDir(d)
			if err != nil {
				continue
			}
			for _, it := range items {
				for _, s := range slugs {
					if !it.IsDir() && personapack.IsPersonaMedia(it.Name(), s) && add(extraPrefix+path.Join(dir, sub, it.Name()), filepath.Join(d, it.Name())) {
						found = true
					}
				}
			}
		}
		return found
	}
	got, err := personapack.CollectRecords(ctx, store, slugs, folder)
	if err != nil {
		return nil, err
	}
	res.Kinds = got.Kinds
	res.Skipped = append(res.Skipped, got.Skipped...)
	return got.Records, nil
}

// addZipFile 把 local 寫成 zip 裡的 name（圖與影片已經壓過就不再壓），回傳寫進去的內容的 sha256 與大小。
func addZipFile(zw *zip.Writer, name, local string) (string, int64, error) {
	f, err := os.Open(local)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	method := zip.Deflate
	if mediaExt[strings.ToLower(filepath.Ext(local))] {
		method = zip.Store
	}
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(f, personapack.MaxFile+1))
	if err != nil {
		return "", 0, err
	}
	if n > personapack.MaxFile {
		return "", 0, fmt.Errorf("too large")
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func isFile(p string) bool { i, err := os.Lstat(p); return err == nil && i.Mode().IsRegular() }

// mineCardsDir 是主人自己的角色卡資料夾（永遠在家裡的 personas/mine/）：找官方卡時給 sdk.OfficialCardIn，
// 同代號的我的卡蓋過跟著程式的公開卡（PRS-27）。
func mineCardsDir(home string) string { return personapack.HomePath(home, "personas/mine") }

// ExportToFile 匯出到 out（沒有副檔名就補 .hgpersona）：先寫同資料夾的暫存檔，完成才改名；已經有這個檔回 Conflict。
func ExportToFile(ctx context.Context, o ExportOptions, out string) (*ExportResult, string, error) {
	if strings.TrimSpace(out) == "" {
		return nil, "", platform.Invalid("out", "要用 --out 指定匯出到哪個檔（例：--out ~/Desktop/luna.hgpersona）")
	}
	if filepath.Ext(out) == "" {
		out += Ext
	}
	abs, err := filepath.Abs(out)
	if err != nil {
		return nil, "", platform.Invalid("out", "--out 的路徑不對")
	}
	if _, err := os.Lstat(abs); err == nil {
		return nil, "", platform.Conflict("已經有 " + abs + "，換一個檔名")
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return nil, "", platform.Unavailable("無法建立資料夾 " + filepath.Dir(abs))
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), "."+filepath.Base(abs)+".*.part")
	if err != nil {
		return nil, "", platform.Unavailable("無法寫入 " + abs)
	}
	defer os.Remove(tmp.Name())
	res, err := Export(ctx, o, tmp)
	if cerr := tmp.Close(); err == nil && cerr != nil {
		err = platform.Unavailable("無法寫入 " + abs)
	}
	if err != nil {
		return nil, "", err
	}
	if err := os.Rename(tmp.Name(), abs); err != nil {
		return nil, "", platform.Unavailable("無法寫入 " + abs)
	}
	return res, abs, nil
}
