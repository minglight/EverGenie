package personapack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// BuildOptions 是 `evergenie personas pack build` 的參數。
type BuildOptions struct {
	Home    string
	Out     string    // 輸出資料夾（會建 m/ 與 f/）
	Version string    // 空＝今天的日期（同一個輸出資料夾已有就加 .2、.3…）
	Now     time.Time // 零值＝現在
	// 兩個一起給：把 pack.toml 寫進那個 repo 的 packages/official/personas/（不 commit）。
	PointerRepo string
	URLBase     string // 包放的網址（https:// 或 file://）；manifest 的網址是 <URLBase>/m/<版號>.json
	// Store 讀這個家的套件資料（經這個家的服務）：照宣告收跟著公開角色走的紀錄（PKG-59），也找出跟著資料夾走的紀錄指的資料夾
	// （例如衣櫃的隱藏版），那些資料夾裡的檔一律不收（PRS-4）。nil 或連不到＝不建、說要先啟動服務，除非 NoRecords。
	Store Store
	// NoRecords：明說只要圖與卡（--no-records）：不帶紀錄。連得到服務照樣讀紀錄決定帶哪些檔（照片紀錄標成隱藏版、設計中、沒採用的檔不收，
	// DEV-160 審查 M2）；連不到時不知道哪些資料夾是隱藏版，wardrobe/ 整個不收，而且這個家有照紀錄帶圖的種類（OfflineFileKinds 有回）就不建。
	NoRecords bool
	// OfflineFileKinds：連不到服務時，從套件設定（package.toml）讀這個家宣告 file_field 的種類（完整名稱）。
	// 有回種類、回錯誤或是 nil（不知道）＝離線 --no-records 也不建：哪些圖能公開只標在紀錄上，讀不到紀錄就判斷不了。
	OfflineFileKinds func() ([]string, error)
	Ctx              context.Context // 零值＝context.Background()
}

// BuildResult 是建包的結果。
type BuildResult struct {
	Version        string   `json:"version"`
	ManifestPath   string   `json:"manifest_path"`
	ManifestSHA256 string   `json:"manifest_sha256"`
	Files          int      `json:"files"`
	Skipped        []string `json:"skipped,omitempty"` // 刻意不收的（白話一行一個）
	PointerFile    string   `json:"pointer_file,omitempty"`
	Records        int      `json:"records"`                // 帶了幾筆跟著角色走的紀錄
	RecordKinds    []string `json:"record_kinds,omitempty"` // 哪些種類（完整名稱）
	RecordsNote    string   `json:"records_note,omitempty"` // 沒帶紀錄的原因（--no-records）
	// RecordsHeld：套件宣告「官方角色包只帶哪些」（pack_only，PKG-68）或「不帶哪些」（pack_except，PKG-71）擋下、沒帶的公開角色紀錄（例如還沒升成官方的衣服、主人標了不公開的衣服），一種一筆（兩種都有分兩筆）、列編號。
	RecordsHeld []HeldRecords `json:"records_held,omitempty"`
	// HeldFiles：因為是 RecordsHeld 那些紀錄的圖（檔名 <角色代號>@<其他>…、wardrobe/<角色代號>/<其他>/）而沒收的檔（包裡的邏輯路徑）。
	HeldFiles []string     `json:"held_files,omitempty"`
	Cards     *CardsResult `json:"cards,omitempty"` // --set-pointer 時一起寫進 repo 的公開卡（PRS-26）
	Personas  int          `json:"personas"`        // 包裡有幾位角色（卡的張數）
	// ExcludedDirs：跟著資料夾走的紀錄指的資料夾（personas/ 底下；例如衣櫃的隱藏版），裡面的檔一個都沒收（PRS-4）。
	ExcludedDirs []string `json:"excluded_dirs,omitempty"`
	// IncludedDirs：收進來的檔落在哪些子資料夾（包裡的邏輯路徑，例 wardrobe/<代號>/full；頂層的 cards/、avatars/ 不列），
	// 每個附幾個檔。給發行的人一眼看出有沒有不該帶的資料夾（DEV-157）。
	IncludedDirs []IncludedDir `json:"included_dirs,omitempty"`
	Summary      string        `json:"summary"` // 建完的一行總結：幾位角色、幾個檔、幾筆紀錄、收了幾個子資料夾、排除了哪些資料夾
	dropWhy      string        // 衣服資料夾整個不收的原因（空＝沒連到服務）
	unlisted     int           // 照紀錄帶圖時，頭像資料夾裡沒有紀錄指著、沒收的檔數（摘要提醒用）
}

// IncludedDir 是收進包裡的一個子資料夾與裡面收了幾個檔（只算直接在這個資料夾裡的）。
type IncludedDir struct {
	Dir   string `json:"dir"`
	Files int    `json:"files"`
}

// includedDirs 把收進來的檔照所在的資料夾歸類（照名字排）：wardrobe/<代號>、avatars/full…都列；
// 只有頂層的 cards/、avatars/ 不列（每一包都有）。其他頂層資料夾萬一出現也列出來，讓發行的人看得到。
func includedDirs(files []File) []IncludedDir {
	count := map[string]int{}
	for _, f := range files {
		if dir := path.Dir(f.Path); dir != "." && dir != "cards" && dir != "avatars" {
			count[dir]++
		}
	}
	out := make([]IncludedDir, 0, len(count))
	for dir, n := range count {
		out = append(out, IncludedDir{Dir: dir, Files: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// errNoService 是建包連不到這個家的服務（或服務還沒準備好）時的白話說明（PRS-4）。err 是服務回的錯誤時把它的話帶上。
func errNoService(err error) error {
	head := "連不到這個家的 EverGenie 服務"
	var se *sdk.Error
	if errors.As(err, &se) && se.Message != "" {
		head = "這個家的 EverGenie 服務還沒準備好（" + se.Message + "）"
	}
	return platform.Unavailable(head + "，沒有建包：角色包要經服務讀跟著角色走的紀錄（例如衣服的目錄），也要知道哪些資料夾是隱藏版、不能收。" +
		"先啟動這個家的服務（evergenie service start --home <家>）再建一次；只要圖與卡、不帶紀錄的話加 --no-records")
}

var (
	allowedAvatarExt = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".mp4": true}
	// 一律不收的目錄名稱（任何一層）：隱藏版（level/）、舊風格、使用者自己的、製作原始檔。
	excludedDirs = map[string]bool{"level": true, "old_school": true, "mine": true, "src": true}
	adultMark    = regexp.MustCompile(`(?im)^\s*(adult|nsfw|r18)\s*[:=]\s*"?true|^\s*level\s*[:=]\s*"?(adult|成人)`)
)

// Excluded 回傳這個檔案「一律不收」的原因；空字串＝可以收。rel 是 personas/ 底下的相對路徑，data 是檔案開頭的內容（可以是 nil）。
// 規則（REQ-135）：路徑有 level/、old_school/、mine/、src/；`*.locked.*`、`*.live.json`、`*.voice.*`；卡片或目錄檔自己標了隱藏版（adult: true、level: adult）；
// 名字有 adult／nsfw／r18／成人（只用在沒有紀錄可讀的檔：紀錄指著的檔照紀錄的 hidden，見 excludedFixed，DEV-160）。
func Excluded(rel string, data []byte) string {
	if why := excludedFixed(rel, data); why != "" {
		return why
	}
	if personaref.IsAdultPath(rel) { // level/ 已在上面擋；跟角色匯出同一份規則
		return "名稱看起來是隱藏版"
	}
	return ""
}

// excludedFixed 是 Excluded 裡不看名字猜隱藏版的那幾條（紀錄指著的檔用，DEV-160）：不公開的資料夾、鎖定版、動態設定、語音、自己標了隱藏版。
func excludedFixed(rel string, data []byte) string {
	for _, seg := range strings.Split(rel, "/") {
		if excludedDirs[seg] {
			return "在 " + seg + "/ 底下（不公開）"
		}
	}
	base := path.Base(rel)
	switch {
	case strings.Contains(base, ".locked."):
		return "鎖定版（.locked）"
	case strings.HasSuffix(base, ".live.json"):
		return "動態設定（.live.json）"
	case strings.Contains(base, ".voice."):
		return "語音（.voice）"
	}
	if len(data) > 0 && adultMark.Match(data) {
		return "自己標了隱藏版"
	}
	return ""
}

type candidate struct {
	logical string // 包裡的邏輯路徑
	local   string // 家裡的實際路徑
	record  bool   // 是帶了的紀錄指著的檔（file_field，DEV-160）：照紀錄收，不再照名字猜是不是隱藏版
}

// Build 建角色包。`[admin] public_personas` 沒寫＝不建（Invalid，說明怎麼設）。
func Build(o BuildOptions) (*BuildResult, error) {
	h, err := platform.LoadHome(o.Home)
	if err != nil {
		return nil, err
	}
	slugs, err := publicSlugs(h, "建角色包")
	if err != nil {
		return nil, err
	}
	if o.Out == "" {
		return nil, platform.Invalid("out", "要用 --out 指定輸出的資料夾")
	}
	if (o.PointerRepo == "") != (o.URLBase == "") {
		return nil, platform.Invalid("set-pointer", "--set-pointer 和 --url-base 要一起給")
	}
	// 發行的網址照下載來源的規則（PLT-25），而且不收 http（連本機的也不收：指標檔會發給別人）
	if o.URLBase != "" && (!platform.DownloadURLAllowed(o.URLBase) || strings.HasPrefix(o.URLBase, "http://")) {
		return nil, platform.Invalid("url-base", "--url-base 要是 https:// 或 file:// 開頭的網址")
	}
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	version, err := pickVersion(o.Out, o.Version, now)
	if err != nil {
		return nil, err
	}
	// 什麼都還沒寫之前先讀公開卡、照同一套規則檢查（一般檔、大小、主人的名字；PRS-26）：包裡的卡與程式帶的卡用同一份
	pubCards, pubSkipped, err := readPublicCards(h, o.Home, slugs)
	if err != nil {
		return nil, err
	}

	res := &BuildResult{Version: version}
	var cands, avatars []candidate
	personas := platform.OfficialPersonasDir(o.Home) // 公開角色是官方位置的卡（第 2 版在狀態資料夾）
	for _, slug := range slugs {
		card := filepath.Join(personas, slug+".md") // readPublicCards 已確認是一般檔
		cands = append(cands, candidate{logical: "cards/" + slug + ".md", local: card})
		avatars = append(avatars, avatarCandidates(filepath.Join(personas, "avatars"), "avatars", slug)...)
		avatars = append(avatars, avatarCandidates(filepath.Join(personas, "avatars", "full"), "avatars/full", slug)...)
		cands = append(cands, treeCandidates(filepath.Join(personas, "wardrobe", slug), "wardrobe/"+slug)...)
		if cat := filepath.Join(personas, "catalog", slug+".md"); isFile(cat) {
			cands = append(cands, candidate{logical: "catalog/" + slug + ".md", local: cat})
		}
		if isFile(filepath.Join(personas, "wardrobe", slug+".toml")) {
			res.Skipped = append(res.Skipped, "personas/wardrobe/"+slug+".toml：製作用的衣櫃設定（含提示詞），不收")
		}
	}
	v, err := readStore(o, slugs, res)
	if err != nil {
		return nil, err
	}
	public := map[string]bool{}
	for _, s := range slugs {
		public[s] = true
	}
	var records []Record
	var heldMedia []HeldMedia
	heldFile := map[string]string{} // 被擋下的紀錄指的檔（包裡的邏輯路徑，小寫：macOS 的檔名不分大小寫）→ 那筆紀錄
	if v.got != nil {
		records, heldMedia = v.got.Records, v.got.HeldMedia
		for _, f := range v.got.HeldFiles {
			if !f.Mine {
				heldFile[strings.ToLower(f.File)] = f.Key
			}
		}
	}
	// blocked：紀錄只能讓檔更不帶（DEV-160 審查 M1）——全家任何一筆不跟著角色走、不進官方角色包、或別的角色的紀錄指著這個檔，
	// 或檔名是別的角色（這個家有她的卡或紀錄）的，不論是誰的紀錄把它列進來都不收。
	blocked := func(logical string) string {
		if why := v.ix.Blocked("personas/"+logical, public, true); why != "" {
			return why + "，不收"
		}
		if s := NameOwner("personas/" + logical); s != "" && !public[s] && (HasCard(o.Home, s) || v.ix.Persona(s)) {
			return "檔名是別的角色（" + s + "）的，不收"
		}
		return ""
	}
	if v.ix.On() {
		// 照紀錄帶圖（DEV-160）：頭像資料夾裡只收帶了的紀錄指著的檔，加上角色本身的頭像 avatars/<代號>.<副檔名>（核心的角色圖）；
		// 被擋下的紀錄指著的檔不收（PRS-29），沒有紀錄指著的列出來不收（不再照檔名猜是哪一套、是不是隱藏版）。
		cands = append(cands, recordCandidates(personas, slugs, v.got.Files, res)...)
		listed := map[string]bool{}
		for _, c := range cands {
			listed[strings.ToLower(c.logical)] = true
		}
		for _, c := range avatars {
			if listed[strings.ToLower(c.logical)] || v.ix.Pointed("personas/"+c.logical) || strings.HasSuffix(c.logical, ".live.json") {
				continue
			}
			res.Skipped = append(res.Skipped, c.logical+"：沒有紀錄指著這個檔（建包照紀錄帶圖），不收")
			res.unlisted++
		}
		for logical, key := range heldFile {
			if isFile(filepath.Join(personas, filepath.FromSlash(logical))) {
				res.Skipped = append(res.Skipped, logical+"：是沒帶的紀錄 "+key+" 的圖，不進官方角色包")
				res.HeldFiles = append(res.HeldFiles, logical)
			}
		}
	} else {
		cands = append(cands, avatars...)
	}
	hidden := resolvedDirs(personas, v.hidden)
	res.ExcludedDirs = hidden
	if !v.known {
		// 不知道哪些資料夾是隱藏版：只收頂層的頭像（名字照角色代號），往下找的衣服資料夾整個不收。
		kept, dropped := cands[:0], false
		for _, c := range cands {
			if strings.HasPrefix(c.logical, "wardrobe/") {
				dropped = true
				continue
			}
			kept = append(kept, c)
		}
		cands = kept
		if dropped {
			why := res.dropWhy
			if why == "" {
				why = "沒有連到這個家的服務"
			}
			res.Skipped = append(res.Skipped, "wardrobe/："+why+"，不知道哪些資料夾是隱藏版，衣服資料夾整個不收")
		}
	}

	if err := os.MkdirAll(filepath.Join(o.Out, "f"), 0o755); err != nil {
		return nil, platform.Unavailable("無法建立輸出資料夾")
	}
	if err := os.MkdirAll(filepath.Join(o.Out, "m"), 0o755); err != nil {
		return nil, platform.Unavailable("無法建立輸出資料夾")
	}
	var files []File
	seen := map[string]bool{}
	for _, c := range cands {
		low := strings.ToLower(c.logical)
		if seen[low] {
			continue
		}
		seen[low] = true
		if heldFile[low] != "" {
			continue // 上面已經列了
		}
		if why := blocked(c.logical); why != "" {
			res.Skipped = append(res.Skipped, c.logical+"："+why)
			continue
		}
		if why := acceptable(o.Home, c, hidden); why != "" {
			res.Skipped = append(res.Skipped, c.logical+"："+why)
			continue
		}
		// 被套件的 pack_only、pack_except 擋下的紀錄，它的圖也不收（PRS-29）：紀錄不公開、圖卻在包裡，等於照樣公開。
		// 紀錄指著的檔照紀錄認（上面），這裡只認沒有 file_field 的種類照命名的圖。
		if i := slices.IndexFunc(heldMedia, func(h HeldMedia) bool { return !c.record && h.Matches(c.logical) }); i >= 0 {
			res.Skipped = append(res.Skipped, c.logical+"：是沒帶的紀錄 "+heldMedia[i].Key+" 的圖，不進官方角色包")
			res.HeldFiles = append(res.HeldFiles, c.logical)
			continue
		}
		sum, size, err := SumFile(c.local)
		if err != nil {
			return nil, platform.Unavailable("讀不了 " + c.logical)
		}
		dst := filepath.Join(o.Out, "f", sum+filepath.Ext(c.logical))
		if !isFile(dst) {
			if err := CopyFile(c.local, dst); err != nil {
				return nil, platform.Unavailable("無法寫入 " + dst)
			}
		}
		files = append(files, File{Path: c.logical, SHA256: sum, Size: size})
	}
	if len(files) == 0 {
		return nil, platform.Invalid("admin.public_personas", "沒有任何可以公開的檔案")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	sort.Strings(res.HeldFiles)
	if v.got != nil && len(records) > 0 {
		// 紀錄指著的檔沒進包（被擋、不在、不合規定）：那筆紀錄也不帶，不然收件的家會有一筆指著空位置（或指著自己家的檔）的照片紀錄。
		inPack := map[string]bool{}
		for _, f := range files {
			inPack[strings.ToLower(f.Path)] = true
		}
		var dropped []string
		records, dropped = KeepRecordsWithFiles(records, v.got.Files, v.ix.FileKind, func(f RecordFile) bool { return !f.Mine && inPack[strings.ToLower(f.File)] })
		res.Skipped = append(res.Skipped, dropped...)
	}
	m := Manifest{Kind: Kind, Schema: Schema, Version: version, Files: files, Records: records}
	if err := m.Validate(); err != nil {
		return nil, platform.Invalid("pack", err.Error())
	}
	data, err := json.MarshalIndent(&m, "", "  ")
	if err != nil {
		return nil, platform.Internal("無法產生 manifest")
	}
	data = append(data, '\n')
	res.ManifestPath = filepath.Join(o.Out, "m", version+".json")
	if err := platform.AtomicWrite(res.ManifestPath, data, 0o644); err != nil {
		return nil, platform.Unavailable("無法寫入 manifest")
	}
	res.ManifestSHA256, res.Files, res.Records = SumBytes(data), len(files), len(records)
	for _, f := range files {
		if cardSlug(f.Path) != "" {
			res.Personas++
		}
	}
	res.IncludedDirs = includedDirs(files)
	res.Summary = summary(res)
	if o.PointerRepo != "" {
		// 同一次發版：程式帶的公開卡跟這一包的卡是同一份（PRS-26）
		if res.Cards, err = writeBundledCards(BundledCardsDir(o.PointerRepo), pubCards); err != nil {
			return nil, err
		}
		res.Cards.Skipped = pubSkipped
		dir := filepath.Join(o.PointerRepo, "packages", "official", "personas")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, platform.Unavailable("無法建立 " + dir)
		}
		body := fmt.Sprintf("# 這一版核心用的角色包（personas pack build --set-pointer 寫的；url 空＝這一版沒有角色包）\nversion = %q\nurl = %q\nsha256 = %q\n",
			version, strings.TrimRight(o.URLBase, "/")+"/m/"+version+".json", res.ManifestSHA256)
		res.PointerFile = filepath.Join(dir, "pack.toml")
		if err := platform.AtomicWrite(res.PointerFile, []byte(body), 0o644); err != nil {
			return nil, platform.Unavailable("無法寫入 pack.toml")
		}
	}
	return res, nil
}

// storeView 是建包從這個家的服務讀到的：got 是跟著公開角色走的紀錄（--no-records 時只用來決定帶哪些檔，紀錄本身不帶）；
// ix 是全家的「檔 → 紀錄」索引；hidden 是跟著資料夾走的紀錄指的資料夾；known＝知道有哪些這種資料夾。離線 --no-records 時都是空的。
type storeView struct {
	got    *CollectResult
	ix     *FileIndex
	hidden []string
	known  bool
}

// readStore 經這個家的服務讀建包要的套件資料：跟著公開角色走的紀錄（只收 by_persona 的；共用 CollectRecords，套 export_omit、pack_only），
// 全家紀錄指著的檔（IndexFiles，DEV-160 審查 M1），與跟著資料夾走（by_folder）的紀錄指的資料夾（hidden；那些資料夾連同圖一律不進官方角色包，PKG-59）。
// --no-records 只代表「不帶紀錄」：連得到服務照樣讀紀錄決定帶哪些檔（M2）。連不到服務：沒有 NoRecords 就不建（errNoService）；
// 有 NoRecords 而這個家有照紀錄帶圖的種類（OfflineFileKinds）也不建，沒有的話只收圖與卡、known 是 false。
func readStore(o BuildOptions, slugs []string, res *BuildResult) (*storeView, error) {
	ctx := o.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if o.NoRecords {
		res.RecordsNote = "加了 --no-records：這一包只有圖與卡，沒帶跟著角色走的套件紀錄"
	}
	offline := func(cause error) (*storeView, error) {
		if !o.NoRecords {
			return nil, errNoService(cause)
		}
		if err := offlineFileKinds(o); err != nil {
			return nil, err
		}
		return &storeView{}, nil
	}
	if o.Store == nil {
		return offline(nil)
	}
	v := &storeView{}
	var err error
	if v.hidden, err = FolderDirs(ctx, o.Store); err == nil {
		v.known, err = checkUnreadable(ctx, o, res)
	}
	if err == nil {
		v.ix, err = IndexFiles(ctx, o.Store)
	}
	if err == nil && v.ix.On() {
		err = UnlistedError(o.Home, v.ix, "建包")
	}
	if err == nil {
		v.got, err = CollectRecords(ctx, o.Store, slugs, nil)
	}
	if err != nil {
		if IsUnavailable(err) {
			return offline(err)
		}
		return nil, err
	}
	if o.NoRecords {
		v.got.Records, v.got.Kinds, v.got.Held, v.got.Bare = nil, nil, nil, nil // 紀錄不帶；檔照紀錄挑（Files、HeldFiles、HeldMedia）
		return v, nil
	}
	if len(v.got.Bare) > 0 {
		return nil, errBare(o.Home, v.got.Bare)
	}
	res.RecordKinds, res.RecordsHeld = v.got.Kinds, v.got.Held
	res.Skipped = append(res.Skipped, v.got.Skipped...)
	return v, nil
}

// errBare：會進官方角色包的上層紀錄（例如一套官方衣服）一筆照紀錄帶檔的下層（照片紀錄）都沒有就不建（DEV-160 收尾）。
// 建包只照紀錄帶檔：沒有紀錄的那幾筆一個檔也帶不走，包裡卻有它們，收件的家只看到沒有圖的衣服，而且只在摘要提一句，等於靜靜少帶。
// 核心分不出是「套件還沒替剛放進來的圖登記」還是「本來就沒有圖」，判斷不了就拒絕，說是哪幾筆、怎麼辦。
// 官方角色放在家外（第 2 版、沒寫 official_personas = "home"）的家，套件不在家裡替官方的檔寫紀錄（會蓋住官方層，VLT-19），
// 等多久都登記不到，改說這個家不能當發行角色包的家；只在 home.toml 加那一行不夠（官方的檔還在狀態資料夾，加了反而找不到）。
func errBare(home string, bare []BareRecords) error {
	var parts, kinds []string
	for _, b := range bare {
		parts = append(parts, fmt.Sprintf("%d 筆會進官方角色包的「%s」（%s）還沒有任何一筆「%s」（%s）：%s", len(b.IDs), b.Title, b.Kind, b.ChildTitle, b.Child, strings.Join(b.IDs, "、")))
		kinds = append(kinds, b.Child)
	}
	kinds = dedupe(kinds)
	msg := strings.Join(parts, "；") + "，沒有建包。建包只照 " + strings.Join(kinds, "、") +
		" 的紀錄帶檔，這幾筆一個檔也帶不走，包裡卻會有它們（收件的家只看到沒有圖的「" + bare[0].Title + "」）。"
	if l := platform.LayoutFor(home); l.V2 && !l.PersonasAtHome {
		return platform.Conflict(msg + "這個家的官方角色放在家外（狀態資料夾），套件不會替官方的檔寫紀錄（會蓋住官方層），等多久都登記不到：" +
			"這個家不能當發行角色包的家，到官方角色留在家裡的家建（例如開發家）。不要只在這個家的 home.toml 加 official_personas = \"home\"：官方的檔還在狀態資料夾，加了反而找不到官方角色")
	}
	return platform.Conflict(msg + "剛放進來的圖等套件登記好再建（服務在跑時套件下一輪就會補登記；直接把檔複製進來的要等它定期重掃，或重開服務），用 evergenie data list " +
		kinds[0] + " 看登記好了沒；本來就沒有圖的，先改成不公開（例如設計中）或封存再建")
}

// offlineFileKinds：連不到服務的 --no-records 能不能建——這個家宣告 file_field 的種類（照片紀錄那類）把「哪些圖能公開」標在紀錄上（隱藏版、設計中、沒採用），
// 讀不到紀錄就只能照檔名猜，會把只靠紀錄標成不公開的圖帶進包（DEV-160 審查 M2）。有這種種類、讀不了套件設定、或沒給讀法，都不建。
func offlineFileKinds(o BuildOptions) error {
	const next = "先啟動這個家的服務（evergenie service start --home <家>）再建一次"
	if o.OfflineFileKinds == nil {
		return platform.Unavailable("連不到這個家的 EverGenie 服務，也沒辦法讀這個家的套件設定，沒有建包：不知道有沒有照片紀錄標著哪些圖不能公開。" + next)
	}
	kinds, err := o.OfflineFileKinds()
	if err != nil {
		return platform.Unavailable("連不到這個家的 EverGenie 服務，套件設定也讀不完整（" + err.Error() + "），沒有建包：不知道有沒有照片紀錄標著哪些圖不能公開。" + next)
	}
	if len(kinds) > 0 {
		return platform.Unavailable("連不到這個家的 EverGenie 服務，沒有建包：這個家的 " + strings.Join(kinds, "、") +
			" 照紀錄決定哪些圖能公開（隱藏版、設計中、沒採用的照片只標在紀錄上），加了 --no-records 也要讀得到這些紀錄。" + next)
	}
	return nil
}

// checkUnreadable：停用中或被拒的套件宣告了跟著角色走的資料（或讀不懂它的宣告）時，這次讀不到它的紀錄與資料夾（Kinds 不列它）。
// 帶紀錄的包就不建：會悄悄少了它的紀錄，也不知道它的隱藏版資料夾在哪（憲法「判斷不了就拒絕」）；--no-records 只是衣服資料夾整個不收。
// 回 known＝隱藏版的資料夾都知道了。Store 沒實作 UnreadableLister 就當沒有讀不到的。
func checkUnreadable(ctx context.Context, o BuildOptions, res *BuildResult) (bool, error) {
	ul, ok := o.Store.(UnreadableLister)
	if !ok {
		return true, nil
	}
	list, err := ul.Unreadable(ctx)
	if err != nil {
		return false, err
	}
	var block, folder, files []string
	for _, p := range list {
		switch {
		case p.Unknown:
			block = append(block, fmt.Sprintf("%s（%s，讀不懂它的設定）", p.ID, p.Why))
			folder = append(folder, p.ID)
			files = append(files, fmt.Sprintf("%s（%s，讀不懂它的設定）", p.ID, p.Why))
		case len(p.Exports) > 0:
			block = append(block, fmt.Sprintf("%s（%s；%s）", p.ID, p.Why, strings.Join(p.Exports, "、")))
			if p.Folder {
				folder = append(folder, p.ID)
			}
			if len(p.Files) > 0 {
				files = append(files, fmt.Sprintf("%s（%s；%s）", p.ID, p.Why, strings.Join(p.Files, "、")))
			}
		}
	}
	if len(block) == 0 {
		return true, nil
	}
	if !o.NoRecords {
		return false, platform.Conflict("這個家有套件讀不到：" + strings.Join(block, "、") + "。它們的紀錄要跟著角色進角色包（例如衣服的目錄），" +
			"讀不到就會少了，也不知道它們的隱藏版資料夾在哪，沒有建包。先啟用或修好這些套件（evergenie packages list 看原因）再建；只要圖與卡、不帶紀錄的話加 --no-records")
	}
	if len(files) > 0 {
		// --no-records 也照紀錄挑圖（M2）：照片紀錄那類讀不到（或讀不懂套件宣告了什麼），就不知道哪些圖被標成不能公開。
		return false, platform.Conflict("這個家有套件讀不到：" + strings.Join(files, "、") + "。它們照紀錄決定哪些圖能公開（例如照片的隱藏版、沒採用），" +
			"讀不到就判斷不了，加了 --no-records 也沒有建包。先啟用或修好這些套件（evergenie packages list 看原因）再建")
	}
	if len(folder) == 0 {
		return true, nil
	}
	res.dropWhy = "套件 " + strings.Join(folder, "、") + " 讀不到"
	return false, nil
}

// resolvedDirs 把要排除的資料夾解開符號連結再列一次（personas/ 底下的相對路徑，小寫、排序不重複）：衣櫃照符號連結找圖，
// 隱藏版的資料夾若是連到 wardrobe/<代號>/ 底下的連結，真正的那個資料夾也不收（PRS-4）。解不開、連到 personas/ 外面的只留原本的。
func resolvedDirs(personas string, dirs []string) []string {
	root, err := filepath.EvalSymlinks(personas)
	if err != nil {
		root = personas
	}
	seen := map[string]bool{}
	for _, d := range dirs {
		seen[d] = true
		real, err := filepath.EvalSymlinks(filepath.Join(personas, filepath.FromSlash(d)))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, real)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		seen[strings.ToLower(filepath.ToSlash(rel))] = true
	}
	out := make([]string, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Strings(out)
	return out
}

// summary 是建完印的一行（PRS-4）：幾位角色、幾個檔、幾筆紀錄（幾筆被套件的 pack_only 擋下沒帶）、排除了哪些資料夾、收了幾個子資料夾（指令在下面一個一行列出）。
func summary(r *BuildResult) string {
	s := fmt.Sprintf("角色包 %s：%d 位角色、%d 個檔案、%d 筆紀錄", r.Version, r.Personas, r.Files, r.Records)
	for _, h := range r.RecordsHeld {
		s += fmt.Sprintf("（%d 筆%s沒帶：%s）", len(h.IDs), h.What, strings.Join(h.IDs, "、"))
	}
	if n := len(r.HeldFiles); n > 0 {
		s += fmt.Sprintf("（它們的圖 %d 個檔也沒收）", n)
	}
	if r.unlisted > 0 {
		s += fmt.Sprintf("；%d 個圖沒有照片紀錄指著、沒收（剛放進來的圖等衣櫃跑一輪登記好再建包）", r.unlisted)
	}
	if len(r.ExcludedDirs) > 0 {
		s += "；排除的資料夾（跟著資料夾走的紀錄指的，例如隱藏版）：" + strings.Join(r.ExcludedDirs, "、")
	} else {
		s += "；沒有要排除的資料夾"
	}
	if n := len(r.IncludedDirs); n > 0 {
		return s + fmt.Sprintf("；收進來的子資料夾 %d 個", n)
	}
	return s + "；沒有收子資料夾"
}

// underDir 回 rel（personas/ 底下的相對路徑）落在 dirs 的哪一個資料夾裡；不在回空。比的時候不分大小寫（macOS 的磁碟不分）。
func underDir(rel string, dirs []string) string {
	low := strings.ToLower(rel)
	for _, d := range dirs {
		if d == "." || low == d || strings.HasPrefix(low, d+"/") {
			return d
		}
	}
	return ""
}

// acceptable 回傳這個候選檔「不收」的原因；空字串＝可以收（一般檔案、不太大、不在「不收什麼」裡）。
// 在跟著資料夾走的紀錄指的資料夾（hidden）裡的也不收。
func acceptable(home string, c candidate, hidden []string) string {
	rel, _ := filepath.Rel(platform.OfficialPersonasDir(home), c.local)
	rel = filepath.ToSlash(rel)
	if d := underDir(rel, hidden); d != "" {
		return "在跟著資料夾走的紀錄指的資料夾 " + d + "/ 裡（例如隱藏版），不進官方角色包"
	}
	info, err := os.Lstat(c.local)
	if err != nil || !info.Mode().IsRegular() {
		return "不是一般檔案"
	}
	if info.Size() > MaxFile {
		return "檔案太大"
	}
	if c.record {
		// 紀錄說了是一般版的照片：檔名不再照名字猜（DEV-160）；但放在看起來是隱藏版的資料夾（level/、名字有 adult 這類）裡的照樣不收：
		// 紀錄只能讓檔更不帶（DEV-160 審查 M3）。
		if personaref.IsAdultPath(path.Dir(rel)) {
			return "在看起來是隱藏版的資料夾裡"
		}
		return excludedFixed(rel, readHead(c.local))
	}
	return Excluded(rel, readHead(c.local))
}

// recordCandidates 是照紀錄要收的頭像資料夾的檔（DEV-160）：帶了的紀錄指著的檔（在官方角色資料夾的；在 personas/mine/ 的不進包、列出來），
// 加上每位角色本身的頭像 avatars/<代號>.<副檔名>（核心的角色圖，沒有紀錄也收）。紀錄的 file 要在 avatars/ 或 wardrobe/<代號>/ 底下。
func recordCandidates(personas string, slugs []string, files []RecordFile, res *BuildResult) []candidate {
	var out []candidate
	public := map[string]bool{}
	for _, s := range slugs {
		public[s] = true
	}
	for _, f := range files {
		parts := strings.Split(f.File, "/")
		_, ok := Target(f.File)
		// 紀錄的 file 是資料、不是保證（任何套件都寫得進去）：要在 avatars/ 或公開角色的 wardrobe/<代號>/ 底下、是圖或影片、
		// 路上的資料夾都不是符號連結（不從官方位置外面帶東西進公開的包）。
		switch {
		case f.Mine:
			res.Skipped = append(res.Skipped, f.Key+"：檔在 personas/mine/（主人自己的），不進官方角色包")
			continue
		case !ok || (parts[0] != "avatars" && parts[0] != "wardrobe") || (parts[0] == "wardrobe" && !public[parts[1]]):
			res.Skipped = append(res.Skipped, f.Key+"：檔「"+f.File+"」不在 avatars/ 或公開角色的 wardrobe/<代號>/ 底下，不收")
			continue
		case !allowedAvatarExt[strings.ToLower(path.Ext(f.File))]:
			res.Skipped = append(res.Skipped, f.Key+"：檔「"+f.File+"」不是圖或影片（png、jpg、jpeg、webp、mp4），不收")
			continue
		case !plainDirs(personas, f.File):
			res.Skipped = append(res.Skipped, f.Key+"：檔「"+f.File+"」路上有符號連結或不是資料夾的段，不收")
			continue
		}
		out = append(out, candidate{logical: f.File, local: filepath.Join(personas, filepath.FromSlash(f.File)), record: true})
	}
	for _, slug := range slugs {
		for ext := range allowedAvatarExt {
			if p := filepath.Join(personas, "avatars", slug+ext); isFile(p) {
				out = append(out, candidate{logical: "avatars/" + slug + ext, local: p})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].logical < out[j].logical })
	return out
}

func pickVersion(out, want string, now time.Time) (string, error) {
	exists := func(v string) bool { return isFile(filepath.Join(out, "m", v+".json")) }
	if want != "" {
		if _, ok := ParseVersion(want); !ok {
			return "", platform.Invalid("version", "版號格式要像 2026.10.29 或 2026.10.29.2")
		}
		if exists(want) {
			return "", platform.Conflict("輸出資料夾裡已經有版號 " + want + "，換一個版號")
		}
		return want, nil
	}
	v := now.Format("2006.01.02")
	for n := 2; exists(v); n++ {
		v = fmt.Sprintf("%s.%d", now.Format("2006.01.02"), n)
	}
	return v, nil
}

// avatarCandidates：dir 底下（不往下找）名字是 <slug> 或 <slug>@…、<slug>~…、<slug>.… 的圖與影片。
func avatarCandidates(dir, prefix, slug string) []candidate {
	items, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []candidate
	for _, it := range items {
		name := it.Name()
		if it.IsDir() || !strings.HasPrefix(name, slug) || strings.HasPrefix(name, ".") {
			continue
		}
		rest := name[len(slug):]
		if rest == "" || !strings.ContainsRune("@~.+", rune(rest[0])) || (!allowedAvatarExt[strings.ToLower(filepath.Ext(name))] && !strings.HasSuffix(name, ".live.json")) {
			continue
		}
		out = append(out, candidate{logical: prefix + "/" + name, local: filepath.Join(dir, name)})
	}
	return out
}

// plainDirs：root 底下 rel（用 /）路上的每一段資料夾都是真的資料夾（不是符號連結）；檔案本身由 acceptable 看。
func plainDirs(root, rel string) bool {
	p := root
	segs := strings.Split(rel, "/")
	for _, seg := range segs[:len(segs)-1] {
		p = filepath.Join(p, seg)
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() {
			return false
		}
	}
	return true
}

// treeCandidates：dir 底下所有一般檔案（含子資料夾；排除規則在 Build 一個一個套）。
func treeCandidates(dir, prefix string) []candidate {
	var out []candidate
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		out = append(out, candidate{logical: prefix + "/" + filepath.ToSlash(rel), local: p})
		return nil
	})
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func isFile(p string) bool { i, err := os.Stat(p); return err == nil && i.Mode().IsRegular() }

// readHead 讀檔案開頭 4KB（判斷文字檔有沒有標隱藏版；二進位檔讀了也無害）。
func readHead(p string) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	return buf[:n]
}
