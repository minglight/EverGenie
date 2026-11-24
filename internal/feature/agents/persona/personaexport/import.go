package personaexport

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/personapack"
	"github.com/minglight/evergenie/sdk"
)

// ImportOptions 是匯入的參數。
type ImportOptions struct {
	Home   string
	Store  Store
	DryRun bool // 只預覽：驗過整個檔、算出會怎麼做，什麼都不寫
	Force  bool // 你改過的檔與已存在的紀錄也蓋（蓋之前備份）
	Now    time.Time
	// Bundled 是跟著程式的公開卡資料夾（PRS-25）：同代號檢查把它也當官方卡（被這個家同代號的我的卡蓋過的不算，PRS-27）；空＝只看官方位置。
	Bundled string
}

// ImportResult 是匯入（或預覽）的結果。檔案是家底下的相對路徑，紀錄是 `<種類>/<編號>`。
type ImportResult struct {
	DryRun           bool     `json:"dry_run"`
	Personas         []string `json:"personas"`
	ExportedAt       string   `json:"exported_at,omitempty"`
	Added            []string `json:"added,omitempty"`    // 新增的檔
	Replaced         []string `json:"replaced,omitempty"` // 換掉的檔（上次匯入之後沒改過，或 --force）
	Unchanged        int      `json:"unchanged"`          // 已經一樣的檔
	Kept             []string `json:"kept,omitempty"`     // 你改過的，沒換（--force 才換）
	RecordsAdded     []string `json:"records_added,omitempty"`
	RecordsReplaced  []string `json:"records_replaced,omitempty"`
	RecordsUnchanged int      `json:"records_unchanged"`
	RecordsKept      []string `json:"records_kept,omitempty"`    // 已經有、內容不同，沒換（--force 才換）
	RecordsSkipped   []string `json:"records_skipped,omitempty"` // 略過的紀錄：「<種類>/<編號>：原因」
	Backup           string   `json:"backup,omitempty"`          // --force 蓋掉的東西備份在哪（家底下的相對路徑）
}

// Message 是給人看的摘要（一行）；細項由呼叫者列。
func (r *ImportResult) Message() string {
	verb := "匯入了"
	if r.DryRun {
		verb = "預覽（還沒寫）：會匯入"
	}
	s := fmt.Sprintf("%s %s：新增 %d 個檔、換掉 %d 個、一樣的 %d 個；紀錄新增 %d 筆、換掉 %d 筆、一樣的 %d 筆",
		verb, strings.Join(r.Personas, "、"), len(r.Added), len(r.Replaced), r.Unchanged, len(r.RecordsAdded), len(r.RecordsReplaced), r.RecordsUnchanged)
	if n := len(r.Kept) + len(r.RecordsKept); n > 0 {
		s += fmt.Sprintf("；你改過、沒換的 %d 個（加 --force 才會蓋，先備份）", n)
	}
	if n := len(r.RecordsSkipped); n > 0 {
		s += fmt.Sprintf("；略過 %d 筆紀錄", n)
	}
	return s
}

// SDK 換成 API 的回應（personas.import）；指令的 --json 也印這一份，兩邊永遠一樣。
func (r *ImportResult) SDK() *sdk.PersonasImportResponse {
	return &sdk.PersonasImportResponse{
		DryRun: r.DryRun, Personas: r.Personas, ExportedAt: r.ExportedAt,
		Added: r.Added, Replaced: r.Replaced, Unchanged: int32(r.Unchanged), Kept: r.Kept,
		RecordsAdded: r.RecordsAdded, RecordsReplaced: r.RecordsReplaced, RecordsUnchanged: int32(r.RecordsUnchanged),
		RecordsKept: r.RecordsKept, RecordsSkipped: r.RecordsSkipped, Backup: r.Backup,
	}
}

// bundledCardSum：匯入的是官方卡 cards/<代號>.md、官方位置沒有這個檔（dst 不存在），而這一版程式帶著同代號的公開卡（PRS-25）時，
// 回程式帶的那張的 sha256；其他情況回空字串（照原本的規則處理）。
func bundledCardSum(o ImportOptions, logical, dst string, dstErr error) string {
	name, ok := strings.CutPrefix(logical, "cards/")
	if !ok || dstErr == nil || o.Bundled == "" {
		return ""
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		return ""
	}
	card, found := sdk.OfficialCardIn(platform.OfficialPersonasDir(o.Home), o.Bundled, mineCardsDir(o.Home), strings.TrimSuffix(name, ".md"))
	if !found || card == dst {
		return ""
	}
	sum, _, err := personapack.SumFile(card)
	if err != nil {
		return ""
	}
	return sum
}

// importRegistry 是 settings/persona-import.toml：匯入時寫進去的每個檔與每筆紀錄的雜湊，
// 用來分辨「上次匯入的、之後沒人改過」（可以換）和「你改過的」（不蓋）。
type importRegistry struct {
	Schema     int               `toml:"schema"`
	ImportedAt string            `toml:"imported_at"`
	Files      map[string]string `toml:"files"`   // 家底下的相對路徑 → sha256
	Records    map[string]string `toml:"records"` // <種類>/<編號> → 內容雜湊
}

func registryPath(home string) string { return filepath.Join(home, "settings", "persona-import.toml") }

func loadRegistry(home string) (*importRegistry, error) {
	r := &importRegistry{Schema: 1}
	data, err := os.ReadFile(registryPath(home))
	if err != nil && !os.IsNotExist(err) {
		return nil, platform.Unavailable("讀不了 settings/persona-import.toml")
	}
	if err == nil {
		if _, err := toml.Decode(string(data), r); err != nil {
			return nil, platform.Invalid("settings/persona-import.toml", "settings/persona-import.toml 的格式不對，請先修正或刪掉它")
		}
	}
	if r.Files == nil {
		r.Files = map[string]string{}
	}
	if r.Records == nil {
		r.Records = map[string]string{}
	}
	return r, nil
}

func saveRegistry(home string, r *importRegistry) error {
	r.Schema = 1
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(r); err != nil {
		return platform.Internal("無法寫入 settings/persona-import.toml")
	}
	if err := os.MkdirAll(filepath.Dir(registryPath(home)), 0o755); err != nil {
		return platform.Unavailable("無法寫入 settings/persona-import.toml")
	}
	return platform.AtomicWrite(registryPath(home), []byte(b.String()), 0o644)
}

// ImportFile 讀一個匯出檔匯入（或預覽）。
func ImportFile(ctx context.Context, o ImportOptions, file string) (*ImportResult, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, platform.Invalid("file", "打不開 "+file)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, platform.Invalid("file", file+" 不是一般的檔案")
	}
	return Import(ctx, o, f, info.Size())
}

type fileJob struct {
	personapack.File
	entry  *zip.File
	rel    string // 家底下的相對路徑
	dst    string
	exists bool
	edited bool
	staged string // 驗過、解開的暫存檔
}

// Import 照 D-D 匯入：全部先驗（manifest、每個檔的 sha256 與大小、每筆紀錄的種類與欄位）再寫；
// 檔案只新增與替換、不刪；本機有而內容不同、又不是上次匯入原樣的不蓋（--force 才蓋，先備份）；
// 紀錄已存在同理；套件沒裝或停用的種類略過並列出。預覽（DryRun）驗得一樣完整，但什麼都不寫。
func Import(ctx context.Context, o ImportOptions, r io.ReaderAt, size int64) (*ImportResult, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	bad := func(msg string) error { return platform.Invalid("file", msg+"（什麼都沒寫）") }
	if size > maxTotal {
		return nil, bad(fmt.Sprintf("匯出檔太大了（上限 %d GB）", maxTotal>>30))
	}
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, bad("這不是角色匯出檔（打不開）")
	}
	var manEntry *zip.File
	entries := map[string]*zip.File{}
	for _, e := range zr.File {
		switch {
		case e.Name == manifestName:
			manEntry = e
		case strings.HasPrefix(e.Name, filesPrefix) && !strings.HasSuffix(e.Name, "/"):
			if entries[e.Name] != nil {
				return nil, bad("匯出檔裡「" + e.Name + "」重複了")
			}
			entries[e.Name] = e
		case strings.HasSuffix(e.Name, "/"):
		default:
			return nil, bad("匯出檔裡有不認得的東西「" + e.Name + "」")
		}
	}
	if manEntry == nil {
		return nil, bad("這不是角色匯出檔（沒有 manifest.json）")
	}
	raw, err := readEntry(manEntry, maxManifest)
	if err != nil {
		return nil, bad("匯出檔的 manifest.json 讀不了")
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, bad("匯出檔的 manifest.json 格式不對")
	}
	if err := man.validate(); err != nil {
		return nil, bad(err.Error())
	}
	res := &ImportResult{DryRun: o.DryRun, Personas: man.Personas, ExportedAt: man.ExportedAt}
	listed := map[string]bool{}
	for _, f := range man.Files {
		listed[filesPrefix+f.Path] = true
		if entries[filesPrefix+f.Path] == nil {
			return nil, bad("匯出檔裡少了「" + f.Path + "」")
		}
	}
	for name := range entries {
		if !listed[name] {
			return nil, bad("匯出檔裡的「" + name + "」manifest 沒有列")
		}
	}

	// 同一個代號不能一邊在官方位置、一邊在「我的」：匯進來會悄悄換掉這個家原本那位（坐她的秘書與記在她名下的紀錄也跟著換人），
	// 或匯進來的被蓋住沒有作用。這個家現在用的是哪一張照角色庫的順序（官方資料夾＞我的＞跟著程式的，PRS-27）：
	// 跟著程式的公開卡沒被我的卡蓋過＝這個家的那位是官方的，匯入同代號的「我的」照樣擋（匯進來會蓋過她）；
	// 這個家已經有同代號的我的卡（蓋過跟著程式的那張）＝匯入「我的」是同一個位置，照一般規則（PRS-10）。
	for _, slug := range man.Personas {
		mineHere := sdk.HasMineCard(mineCardsDir(o.Home), slug) // 跟角色庫同一個判斷（捷徑、壞掉的捷徑也算，PRS-27）
		officialDir := platform.OfficialPersonasDir(o.Home)
		card, official := sdk.OfficialCardIn(officialDir, o.Bundled, mineCardsDir(o.Home), slug) // 跟著程式的公開卡也算（PRS-25）
		officialHere := official && isFile(card)
		switch {
		case listed[filesPrefix+"cards/"+slug+".md"] && mineHere:
			return nil, bad(fmt.Sprintf("這個家自己已經有一位「%s」（personas/mine/%s.md），跟匯入的同代號；先把其中一位改代號再匯入", slug, slug))
		case listed[filesPrefix+"extra/mine/"+slug+".md"] && officialHere && filepath.Dir(card) != officialDir:
			return nil, bad(fmt.Sprintf("「%s」是這一版程式帶著的公開角色，跟匯入的「我的」角色同代號；請在原本的家先把那位改成 my_ 開頭的代號（例如 my_%s）再匯出", slug, slug))
		case listed[filesPrefix+"extra/mine/"+slug+".md"] && officialHere:
			return nil, bad(fmt.Sprintf("這個家已經有一位「%s」（personas/%s.md），跟匯入的同代號；先把其中一位改代號再匯入", slug, slug))
		}
	}

	reg, err := loadRegistry(o.Home)
	if err != nil {
		return nil, err
	}

	// 紀錄（跟官方角色包共用 personapack.PlanRecords）：種類要在這個家註冊中、而且這個家的套件也說它跟著角色走；
	// 欄位照這個家的規格先驗；已經有的照同樣的規則；封存過的編號不能重用，連 --force 也不寫。
	// 先算紀錄（只讀）：檔名看不出是誰的檔（file_records）照收了的紀錄驗（DEV-160 S3）。
	if len(man.Records) > 0 && o.Store == nil {
		return nil, platform.Unavailable("讀不到這個家的套件資料（EverGenie 沒在跑？），什麼都沒寫")
	}
	carried, listedSlug := map[string]bool{}, map[string]bool{}
	for _, f := range man.Files {
		if rel, ok := target(f.Path); ok {
			carried[strings.ToLower(rel)] = true
		}
	}
	for _, s := range man.Personas {
		listedSlug[s] = true
	}
	// 這個家的「檔 → 紀錄」索引（DEV-160 第二輪審查）：這個家隱藏版、別的角色、沒寫角色的紀錄指著的位置，帶來的檔不寫（--force 也不寫）、
	// 指著那裡的紀錄不收——匯入不能蓋掉或認走收件家的隱藏版。連不到服務而匯出檔沒有紀錄（只照檔名的舊匯出檔）時沒有索引，照舊。
	here, err := personapack.IndexFiles(ctx, o.Store)
	if err != nil {
		if len(man.Records) > 0 || !personapack.IsUnavailable(err) {
			if personapack.IsUnavailable(err) {
				return nil, platform.Unavailable(personapack.ErrText(err) + "，什麼都沒寫")
			}
			return nil, err
		}
		here = nil
	}
	blockedHere := func(rel string) string { return here.Blocked(rel, listedSlug, false) }
	plan, err := personapack.PlanRecords(ctx, o.Store, personapack.PlanInput{
		Records: man.Records,
		Slugs:   man.Personas,
		FolderOK: func(d string) bool {
			_, ok := target(extraPrefix + d + "/x.png")
			return ok && folderHasFiles(man.Files, d)
		},
		Registry: reg.Records,
		Force:    o.Force,
		// 紀錄指著的檔（DEV-160 審查 M1）：沒帶檔的紀錄不能指著這個家已經有的檔或別筆紀錄的檔；
		// 檔名是這個家另一位角色（不在這個檔案裡）的也不收。
		Carried:  func(rel string) bool { return carried[strings.ToLower(rel)] },
		Occupied: func(rel string) bool { return exists(personapack.HomePath(o.Home, rel)) },
		Foreign: func(rel string) string {
			if s := personapack.NameOwner(rel); s != "" && !listedSlug[s] &&
				(personapack.HasCard(o.Home, s) || (o.Bundled != "" && isFile(filepath.Join(o.Bundled, s+".md")))) {
				return s
			}
			return ""
		},
		Blocked: blockedHere,
	})
	if err != nil {
		var br *personapack.BadRecord
		if errors.As(err, &br) {
			return nil, bad(br.Msg)
		}
		if personapack.IsUnavailable(err) {
			return nil, platform.Unavailable(personapack.ErrText(err) + "，什麼都沒寫")
		}
		return nil, err
	}

	// 檔案：每個先驗 sha256 與大小（真的匯入時同時解到暫存資料夾）。任何一個不對，整個檔都不收。
	var staging string
	if !o.DryRun {
		staging = uniqueDir(platform.RuntimeDir(o.Home, "persona-import", "staging", stamp(now)), exists)
		if err := os.MkdirAll(staging, 0o755); err != nil {
			return nil, platform.Unavailable("無法建立暫存資料夾")
		}
		defer func() {
			// 暫存用完就刪；上面兩層空了也收掉（os.Remove 只刪空資料夾）。
			_ = os.RemoveAll(staging)
			_ = os.Remove(filepath.Dir(staging))
			_ = os.Remove(filepath.Dir(filepath.Dir(staging)))
		}()
	}
	var jobs []fileJob
	var blockedFiles []string            // 這個家的紀錄說不能跟著這次的角色走的位置：帶來的檔不寫
	fileSkipped := map[string][]string{} // 紀錄沒收、連檔也不收的：<種類>/<編號> → 家裡的位置
	for i, f := range man.Files {
		e := entries[filesPrefix+f.Path]
		j := fileJob{File: f, entry: e}
		if staging != "" {
			j.staged = filepath.Join(staging, fmt.Sprintf("%d", i))
		}
		if e.UncompressedSize64 != uint64(f.Size) {
			return nil, bad("匯出檔裡「" + f.Path + "」的大小跟 manifest 寫的不一樣")
		}
		j.rel, _ = target(f.Path)
		if !realDirs(o.Home, j.rel) {
			return nil, bad("家裡 " + path.Dir(j.rel) + " 這一路上有不是一般資料夾的東西（例如符號連結），不往裡面寫")
		}
		if err := verifyEntry(e, f, j.staged); err != nil {
			return nil, bad(err.Error())
		}
		// 檔名看不出是誰的：要是帶著它的那筆紀錄（這個家收了的）照這個家的宣告指著的檔，也不能是這個家別筆紀錄的檔；
		// 紀錄沒收（套件沒裝、這個家說不跟著走、已封存的編號）就連檔也不收，記在那筆紀錄略過的原因後面；
		// 本機那筆改過沒換、指著別的檔的，這個檔算「本機的留著」。
		if key := man.FileRecords[f.Path]; key != "" && owner(f.Path, man.Personas) == "" {
			for _, other := range plan.Taken[strings.ToLower(j.rel)] {
				if other != key {
					return nil, bad(fmt.Sprintf("匯出檔裡的「%s」是這個家紀錄 %s 的檔，不是 %s 的", f.Path, other, key))
				}
			}
			want, ok := plan.Files[key]
			switch {
			case !ok && slices.Contains(plan.Kept, key):
				res.Kept = append(res.Kept, j.rel)
				continue
			case !ok:
				fileSkipped[key] = append(fileSkipped[key], j.rel)
				continue
			case !strings.EqualFold(want, j.rel): // macOS 的檔名不分大小寫
				return nil, bad(fmt.Sprintf("匯出檔裡的「%s」不是紀錄 %s 指著的檔", f.Path, key))
			}
		}
		if why := blockedHere(j.rel); why != "" {
			blockedFiles = append(blockedFiles, j.rel+"："+why+"，帶來的檔不寫（不蓋這個家的檔）")
			continue
		}
		j.dst = personapack.HomePath(o.Home, j.rel) // 官方位置照家的佈局（第 2 版在狀態資料夾），mine/ 在家裡
		local, _, lerr := personapack.SumFile(j.dst)
		if lerr == nil {
			if info, err := os.Lstat(j.dst); err != nil || !info.Mode().IsRegular() {
				return nil, bad("家裡的 " + j.rel + " 不是一般的檔案")
			}
		}
		bundled := bundledCardSum(o, f.Path, j.dst, lerr)
		switch {
		case lerr == nil && local == f.SHA256:
			res.Unchanged++
			reg.Files[j.rel] = f.SHA256
			continue
		case bundled != "" && bundled == f.SHA256:
			// 跟這一版程式帶的公開卡一樣：不抄一份到官方位置（PRS-25；抄了之後換版，新版的卡會被這份舊的蓋住）
			res.Unchanged++
			continue
		case bundled != "" && !o.Force:
			// 跟程式帶的那張不一樣：照「本機已經有、內容不同」算，預設不換；--force 才寫進官方位置（之後換版就是這份優先）
			res.Kept = append(res.Kept, j.rel)
			continue
		case lerr != nil:
			if _, err := os.Lstat(j.dst); err == nil {
				return nil, bad("家裡的 " + j.rel + " 讀不了")
			}
			res.Added = append(res.Added, j.rel)
		case reg.Files[j.rel] != "" && reg.Files[j.rel] == local:
			j.exists = true
			res.Replaced = append(res.Replaced, j.rel)
		default:
			if !o.Force {
				res.Kept = append(res.Kept, j.rel)
				plan.DropFileRecords(j.rel, "這個家已經有、內容不一樣（沒換）")
				continue
			}
			j.exists, j.edited = true, true
			res.Replaced = append(res.Replaced, j.rel)
		}
		jobs = append(jobs, j)
	}

	for i, line := range plan.Skipped {
		key, _, _ := strings.Cut(line, "：")
		if files := fileSkipped[key]; len(files) > 0 {
			plan.Skipped[i] = line + "（它指著的 " + strings.Join(files, "、") + " 也不收）"
		}
	}
	res.RecordsAdded, res.RecordsReplaced, res.RecordsUnchanged = plan.Added, plan.Replaced, plan.Unchanged
	res.RecordsKept, res.RecordsSkipped = plan.Kept, append(plan.Skipped, blockedFiles...)
	sort.Strings(res.Kept)
	if o.DryRun {
		return res, nil
	}

	// 備份要蓋掉的、你改過的東西。
	var backup string
	ensureBackup := func() string {
		if backup == "" {
			backup = uniqueDir(platform.RuntimeDir(o.Home, "backups", "persona-import", stamp(now)), exists)
		}
		return backup
	}
	for _, j := range jobs {
		if j.edited {
			if err := personapack.CopyFile(j.dst, filepath.Join(ensureBackup(), filepath.FromSlash(j.rel))); err != nil {
				return nil, platform.Unavailable("無法備份你改過的 " + j.rel + "（什麼都沒寫）")
			}
		}
	}
	if err := plan.Backup(ensureBackup); err != nil {
		return nil, platform.Unavailable(err.Error() + "（什麼都沒寫）")
	}
	if backup != "" {
		res.Backup = homeRel(o.Home, backup)
	}

	items := make([]personapack.Install, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, personapack.Install{Label: j.rel, Src: j.staged, Dst: j.dst, Exists: j.exists})
	}
	if err := personapack.InstallAll(items); err != nil {
		return nil, platform.Unavailable(err.Error() + "（什麼都沒換）")
	}
	for _, j := range jobs {
		reg.Files[j.rel] = j.SHA256
	}
	// 紀錄經 vault 的正常寫入（檔頭、驗證、事件）。前面已照規格驗過；萬一還是有一筆寫不進去，已經寫的照實說。
	written, failed, werr := plan.Write(ctx, o.Store, reg.Records)
	if werr != nil {
		reg.ImportedAt = now.Format(time.RFC3339)
		_ = saveRegistry(o.Home, reg)
		done := "還沒有紀錄寫進去"
		if len(written) > 0 {
			done = "已經寫進去的紀錄：" + strings.Join(written, "、")
		}
		return nil, platform.Unavailable(fmt.Sprintf("檔案已經換進來，但紀錄 %s 寫不進去（%s）；%s", failed, personapack.ErrText(werr), done))
	}
	reg.ImportedAt = now.Format(time.RFC3339)
	if err := saveRegistry(o.Home, reg); err != nil {
		return nil, err
	}
	return res, nil
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// folderHasFiles：manifest 裡至少有一個檔放在 personas/<dir>/ 或 personas/<dir>/full/（validate 已確定每個檔都屬於列出的角色）。
func folderHasFiles(files []personapack.File, dir string) bool {
	for _, f := range files {
		rel, _ := target(f.Path)
		if d := path.Dir(rel); d == "personas/"+dir || d == "personas/"+dir+"/full" {
			return true
		}
	}
	return false
}

// realDirs：rel（家底下的相對路徑，personas/ 開頭）在實際的根（personapack.SplitPersonas）底下一路上已經存在的每一層
// 都是一般資料夾（不是符號連結），寫進去才不會跑到別處。
func realDirs(home, rel string) bool {
	p, rest := personapack.SplitPersonas(home, rel)
	dir := path.Dir(rest)
	if dir == "." {
		return true
	}
	for _, seg := range strings.Split(dir, "/") {
		p = filepath.Join(p, seg)
		info, err := os.Lstat(p)
		if os.IsNotExist(err) {
			return true
		}
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

func readEntry(e *zip.File, limit int64) ([]byte, error) {
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("too large")
	}
	return data, nil
}

// verifyEntry 讀一個檔、比對大小與 sha256；staged 不空就同時寫到那裡（驗不過就刪掉）。
func verifyEntry(e *zip.File, f personapack.File, staged string) error {
	rc, err := e.Open()
	if err != nil {
		return fmt.Errorf("匯出檔裡的「%s」讀不了", f.Path)
	}
	defer rc.Close()
	h := sha256.New()
	var w io.Writer = h
	var out *os.File
	if staged != "" {
		if out, err = os.Create(staged); err != nil {
			return fmt.Errorf("無法解開「%s」", f.Path)
		}
		defer out.Close()
		w = io.MultiWriter(h, out)
	}
	n, err := io.Copy(w, io.LimitReader(rc, f.Size+1))
	if err != nil {
		return fmt.Errorf("匯出檔裡的「%s」壞了", f.Path)
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("匯出檔裡「%s」的內容跟 manifest 寫的不一樣（sha256 對不上）", f.Path)
	}
	return nil
}
