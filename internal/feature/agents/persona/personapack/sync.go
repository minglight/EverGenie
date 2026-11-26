package personapack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Pointer 是官方套件 `personas` 的 pack.toml：這一版核心用哪份包。url 空＝這一版沒有角色包。
type Pointer struct {
	Version string `toml:"version"`
	URL     string `toml:"url"`
	SHA256  string `toml:"sha256"`
}

// Registry 是 settings/personas-pack.toml：上次同步到哪個版號、每個檔案裝進去時的 sha256（用來分辨「使用者改過」）。
type Registry struct {
	Schema   int               `toml:"schema"`
	Version  string            `toml:"version"`
	SyncedAt string            `toml:"synced_at"`
	Files    map[string]string `toml:"files"` // 邏輯路徑 → sha256
	// Records 是同步寫進來的紀錄（<種類>/<編號> → 欄位＋內文的雜湊，不含 export_omit 的欄位；PKG-59）：分辨「上次同步之後沒人改過」。
	Records map[string]string `toml:"records"`
}

// Pending 是 runtime/personas-pack/pending.json：沒補上的包（沒網路、驗證失敗…）。
type Pending struct {
	Version string `json:"version"`
	Reason  string `json:"reason"`
	At      string `json:"at"`
}

// SyncOptions 是 `evergenie personas pack sync` 的參數。
type SyncOptions struct {
	Home        string
	PointerPath string // pack.toml 的路徑（cmd 層依 EVERGENIE_PERSONAS_PACK_POINTER ＞ 官方套件資料夾決定）
	Force       bool   // 連使用者改過的檔也蓋（蓋之前備份）
	Now         time.Time
	HTTP        *http.Client // 測試可以換；預設逾時 30 秒
	// Store 讀寫這個家的套件資料（包裡有 records 時要；指令行程接 sdk client、serve 接 vault 模組）。包沒有 records 就用不到。
	// 官方角色放家外的家（DEV-154）：紀錄只寫官方層，不寫家裡——Store 要能給官方層（OfficialProvider，serve 那一份才有）；
	// 給不出來（指令行程）就當成這次連不到，檔案照換、紀錄留給 serve 補。
	Store Store
}

// OfficialProvider 是「能給官方層」的 Store：OfficialStore 回只讀寫官方層（家外、唯讀給主人）的那一份；這個家沒有官方層回 nil。
type OfficialProvider interface {
	OfficialStore() Store
}

// recordStoreFor 決定角色包的紀錄寫到哪：官方角色還在家裡的家照舊寫家裡的資料；放家外的家只寫官方層。
func recordStoreFor(home string, s Store) Store {
	if l := platform.LayoutFor(home); !l.V2 || l.PersonasAtHome {
		return s
	}
	if p, ok := s.(OfficialProvider); ok {
		return p.OfficialStore()
	}
	return nil
}

// Sync 的結果狀態。
const (
	StatusSynced      = "synced"       // 換進來了
	StatusUpToDate    = "up_to_date"   // 已是最新
	StatusNoPack      = "no_pack"      // 這一版沒有角色包
	StatusNoDowngrade = "no_downgrade" // 指標比登記的舊，不退版
	StatusSource      = "source"       // 角色包的來源（[admin] public_personas 有寫），不同步
	StatusFailed      = "failed"       // 失敗：什麼都沒換，已記 pending.json
)

// SyncResult 是同步的結果（--json 直接印這個）。
type SyncResult struct {
	Status     string   `json:"status"`
	Version    string   `json:"version,omitempty"`    // 這次處理的包版號（指標的）
	Registered string   `json:"registered,omitempty"` // 同步之後登記的版號
	Installed  []string `json:"installed,omitempty"`  // 換進來的檔（邏輯路徑）
	Unchanged  int      `json:"unchanged"`
	UserEdited []string `json:"user_edited,omitempty"` // 你改過的，沒換
	Backup     string   `json:"backup,omitempty"`      // --force 蓋掉的檔案備份在哪（相對家）
	// NewCards：這次換進來、換之前家裡沒有的官方角色卡代號（DEV-115：新角色來打招呼）。全新安裝的第一次同步（登記是空的、personas/ 也還沒有官方卡）一律空；
	// 本來就有、只是更新內容的卡，或登記過但被刪掉、這次補回來的卡都不算。
	NewCards []string `json:"new_cards,omitempty"`
	// 跟著角色走的紀錄（PKG-59）：<種類>/<編號>；Skipped 附原因（套件沒裝或停用、已封存…）。
	RecordsAdded     []string `json:"records_added,omitempty"`
	RecordsReplaced  []string `json:"records_replaced,omitempty"`
	RecordsUnchanged int      `json:"records_unchanged,omitempty"`
	RecordsKept      []string `json:"records_kept,omitempty"` // 你改過的，沒換（--force 才換）
	RecordsSkipped   []string `json:"records_skipped,omitempty"`
	// RecordsLater：檔案換進來了，包裡的紀錄這次沒寫（服務沒在跑、套件服務還沒準備好，或指令自己同步、寫不了官方層）；serve 很快會再試（PRS-6）。
	RecordsLater bool   `json:"records_later,omitempty"`
	Reason       string `json:"reason,omitempty"` // 失敗的白話原因，或「不退版」之類的說明
}

// NoPackMessage 是這一版沒有角色包（指標的 url 空）時給主人看的話（PRS-25）：卡已經跟著程式，缺的只是圖。
const NoPackMessage = "還沒有公開的角色包：角色的頭像與衣服圖還沒開放下載（角色卡已經跟著程式裝好，沒有圖的地方先用名字的第一個字代替）"

// Message 是給人看的一行。
func (r *SyncResult) Message() string {
	switch r.Status {
	case StatusSynced:
		s := fmt.Sprintf("角色包已同步到 %s：換進來 %d 個檔案，沒變 %d 個", r.Version, len(r.Installed), r.Unchanged)
		if n := len(r.RecordsAdded) + len(r.RecordsReplaced); n > 0 || r.RecordsUnchanged > 0 {
			s += fmt.Sprintf("；紀錄新增 %d 筆、換掉 %d 筆、一樣的 %d 筆", len(r.RecordsAdded), len(r.RecordsReplaced), r.RecordsUnchanged)
		}
		if n := len(r.UserEdited) + len(r.RecordsKept); n > 0 {
			s += fmt.Sprintf("；你改過、沒換的 %d 個（加 --force 才會蓋掉）", n)
		}
		if n := len(r.RecordsSkipped); n > 0 {
			s += fmt.Sprintf("；略過 %d 筆紀錄", n)
		}
		return s
	case StatusUpToDate:
		return "角色包已是最新（" + r.Registered + "）"
	case StatusNoPack:
		return NoPackMessage
	case StatusNoDowngrade:
		return fmt.Sprintf("這一版的角色包（%s）比你已經裝的（%s）舊，不退版，什麼都沒換", r.Version, r.Registered)
	case StatusSource:
		return "這個家是角色包的來源（有寫 [admin] public_personas），角色庫以這裡為準，不同步角色包"
	}
	if len(r.Installed) > 0 {
		return "角色包的檔案換進來了，但還沒同步完：" + r.Reason + "（之後會自動再試）"
	}
	return "角色包沒有同步成功：" + r.Reason + "（什麼都沒換；有網路時會自動再試）"
}

// ReadPointer 讀 pack.toml；檔案不存在或 url 空＝這一版沒有角色包（回 nil）。
func ReadPointer(p string) (*Pointer, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, platform.Unavailable("讀不了角色包指標 pack.toml")
	}
	var ptr Pointer
	if _, err := toml.Decode(string(data), &ptr); err != nil {
		return nil, platform.Invalid("pack.toml", "pack.toml 的格式不對")
	}
	if strings.TrimSpace(ptr.URL) == "" {
		return nil, nil
	}
	return &ptr, nil
}

// LoadRegistry 讀登記檔；不存在是空的。
func LoadRegistry(home string) (*Registry, error) {
	r := &Registry{Schema: 1, Files: map[string]string{}, Records: map[string]string{}}
	data, err := os.ReadFile(registryPath(home))
	if os.IsNotExist(err) && registryPath(home) != homeRegistryPath(home) {
		// 官方角色剛搬出家的家：登記檔還在 settings/ 那份，讀它（之後寫到家外，家裡那份不動）。
		data, err = os.ReadFile(homeRegistryPath(home))
	}
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, platform.Unavailable("讀不了 settings/personas-pack.toml")
	}
	if _, err := toml.Decode(string(data), r); err != nil {
		return nil, platform.Invalid("settings/personas-pack.toml", "settings/personas-pack.toml 的格式不對，請先修正或刪掉它")
	}
	if r.Files == nil {
		r.Files = map[string]string{}
	}
	if r.Records == nil {
		r.Records = map[string]string{}
	}
	return r, nil
}

func saveRegistry(home string, r *Registry) error {
	r.Schema = 1
	var b strings.Builder
	if err := toml.NewEncoder(&b).Encode(r); err != nil {
		return platform.Internal("無法寫入登記檔")
	}
	if err := os.MkdirAll(filepath.Dir(registryPath(home)), 0o755); err != nil {
		return platform.Unavailable("無法寫入登記檔")
	}
	return platform.AtomicWrite(registryPath(home), []byte(b.String()), 0o644)
}

// ReadPending 讀 pending.json；沒有回 nil。
func ReadPending(home string) *Pending {
	data, err := os.ReadFile(pendingPath(home))
	if err != nil {
		return nil
	}
	var p Pending
	if json.Unmarshal(data, &p) != nil || p.Reason == "" {
		return nil
	}
	return &p
}

func writePending(home string, p Pending) {
	data, _ := json.Marshal(p)
	_ = os.MkdirAll(packDir(home), 0o755)
	_ = platform.AtomicWrite(pendingPath(home), append(data, '\n'), 0o644)
}

func clearPending(home string) { _ = os.Remove(pendingPath(home)) }

func writeLast(home string, r *SyncResult, at time.Time) {
	rec := struct {
		SyncResult
		At string `json:"at"`
	}{*r, at.Format(time.RFC3339)}
	data, _ := json.Marshal(rec)
	_ = os.MkdirAll(packDir(home), 0o755)
	_ = platform.AtomicWrite(lastPath(home), append(data, '\n'), 0o644)
}

// fetcher 讀 manifest 與檔案：file:// 直接讀、https:// 與本機的 http:// 用 net/http（下載來源的規則 PLT-25，跟升級同一條）。
type fetcher struct {
	client *http.Client
}

// open 回傳讀取網址內容的串流；呼叫者關閉。只收下載來源的規則允許的網址（https、本機的 http、file；PLT-25），
// 連線與讀檔是 platform.OpenDownload（跟升級、套件下載 Go 同一支）。
func (f fetcher) open(ctx context.Context, raw string) (io.ReadCloser, error) {
	rc, _, err := platform.OpenDownload(ctx, f.client, raw, 30*time.Second)
	if err == nil {
		return rc, nil
	}
	var de *platform.DownloadError
	errors.As(err, &de)
	switch {
	case de != nil && de.Fault == platform.DownloadNotAllowed:
		return nil, fmt.Errorf("角色包的網址要是 https:// 開頭")
	case de != nil && de.Fault == platform.DownloadUnsafeRedirect:
		return nil, fmt.Errorf("下載的網址把連線轉到不安全的地方，沒有下載")
	case de != nil && de.Fault == platform.DownloadStatus:
		return nil, fmt.Errorf("下載網址回應 %d", de.Status)
	case strings.HasPrefix(raw, "file:"):
		return nil, err
	}
	return nil, fmt.Errorf("連不上下載的網址")
}

func (f fetcher) readAll(ctx context.Context, raw string, limit int64) ([]byte, error) {
	rc, err := f.open(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, fmt.Errorf("下載中斷")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("檔案比預期的大")
	}
	return data, nil
}

// download 把 raw 存成 dst（先寫 .part，驗 sha256 才改名）；已經有驗過的就不重抓。
func (f fetcher) download(ctx context.Context, raw, dst, wantSHA string) error {
	if sum, _, err := SumFile(dst); err == nil && sum == wantSHA {
		return nil
	}
	rc, err := f.open(ctx, raw)
	if err != nil {
		return err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("無法建立暫存資料夾")
	}
	part := dst + ".part"
	out, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("無法寫入暫存檔")
	}
	n, err := io.Copy(out, io.LimitReader(rc, MaxFile+1))
	cerr := out.Close()
	if err != nil || cerr != nil {
		_ = os.Remove(part)
		return fmt.Errorf("下載中斷")
	}
	if n > MaxFile {
		_ = os.Remove(part)
		return fmt.Errorf("檔案比預期的大")
	}
	if sum, _, err := SumFile(part); err != nil || sum != wantSHA {
		_ = os.Remove(part)
		return fmt.Errorf("下載的檔案內容跟 manifest 寫的不一樣（sha256 對不上）")
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("無法存下載的檔案")
	}
	return nil
}

// Sync 照指標同步角色包。失敗時什麼都不換、登記不變、寫 pending.json，回 StatusFailed 的結果與同一個錯誤。
func Sync(ctx context.Context, o SyncOptions) (*SyncResult, error) {
	now := o.Now
	if now.IsZero() {
		now = time.Now()
	}
	h, err := platform.LoadHome(o.Home)
	if err != nil {
		return nil, err
	}
	if h.PersonaPackSource() {
		return &SyncResult{Status: StatusSource}, nil
	}
	ptr, err := ReadPointer(o.PointerPath)
	if err != nil {
		return nil, err
	}
	if ptr == nil {
		return &SyncResult{Status: StatusNoPack}, nil
	}
	reg, err := LoadRegistry(o.Home)
	if err != nil {
		return nil, err
	}
	// 全新安裝：登記是空的、而且 personas/ 底下一張官方卡都還沒有——這時所有角色都是「新」的，不打招呼。
	// 已經在用、只是從沒同步過角色包的家（卡是發版或搬家放進來的）不算全新，真的沒有的角色照樣打招呼。
	firstSync := reg.Version == "" && len(reg.Files) == 0 && !hasOfficialCards(o.Home)
	res := &SyncResult{Version: ptr.Version, Registered: reg.Version}
	if _, ok := ParseVersion(ptr.Version); !ok || !shaPattern.MatchString(ptr.SHA256) {
		return failSync(o.Home, res, ptr.Version, "pack.toml 的版號或 sha256 格式不對", now)
	}
	if reg.Version != "" {
		cmp, _ := CompareVersions(ptr.Version, reg.Version)
		// 同版號時 --force 還是要往下走：同步完提示「你改過的沒換（加 --force 才蓋）」，加了卻回「已是最新」就蓋不掉了（DEV-35）。
		if cmp == 0 && !o.Force {
			clearPending(o.Home)
			res.Status = StatusUpToDate
			return res, nil
		}
		if cmp < 0 {
			if p := ReadPending(o.Home); p != nil {
				if c, ok := CompareVersions(p.Version, reg.Version); ok && c <= 0 {
					clearPending(o.Home)
				}
			}
			res.Status = StatusNoDowngrade
			return res, nil
		}
	}
	fetch := fetcher{client: o.HTTP}
	raw, err := fetch.readAll(ctx, ptr.URL, maxManifest)
	if err != nil {
		return failSync(o.Home, res, ptr.Version, "抓不到角色包的清單（"+err.Error()+"）", now)
	}
	if SumBytes(raw) != ptr.SHA256 {
		return failSync(o.Home, res, ptr.Version, "角色包清單的內容跟這一版核心記的不一樣（sha256 對不上）", now)
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return failSync(o.Home, res, ptr.Version, "角色包清單的格式不對", now)
	}
	if err := man.Validate(); err != nil {
		return failSync(o.Home, res, ptr.Version, err.Error(), now)
	}
	if man.Version != ptr.Version {
		return failSync(o.Home, res, ptr.Version, "角色包清單的版號跟核心記的不一樣", now)
	}

	// 規劃：每個檔案是「已經是這份」「要換進來」還是「你改過的」。
	// 沒有檔案或本機沒被改過（跟登記一樣）而包有新內容 → 換；本機跟登記不一樣（你改過，或原本就不是包裡來的）→ 不蓋，除非 --force。
	type job struct {
		File
		dst    string // 家裡的絕對路徑
		exists bool
		edited bool
		fresh  bool // 換之前家裡沒有、也沒登記過的官方卡（新角色）
	}
	var jobs []job
	for _, f := range man.Files {
		rel, _ := Target(f.Path)
		dst := HomePath(o.Home, rel) // 官方角色的位置照家的佈局（第 2 版在狀態資料夾）
		old := reg.Files[f.Path]
		local, _, lerr := SumFile(dst)
		exists := lerr == nil
		switch {
		case exists && local == f.SHA256:
			reg.Files[f.Path] = f.SHA256 // 本機已經是這份：只登記
			res.Unchanged++
		case !exists:
			jobs = append(jobs, job{File: f, dst: dst, fresh: !firstSync && old == "" && cardSlug(f.Path) != ""})
		case old != "" && local == old:
			jobs = append(jobs, job{File: f, dst: dst, exists: true})
		default:
			res.UserEdited = append(res.UserEdited, f.Path)
			if o.Force {
				jobs = append(jobs, job{File: f, dst: dst, exists: true, edited: true})
			}
		}
	}
	sort.Strings(res.UserEdited)

	// 紀錄（PKG-59、D-E）：只收 by_persona 的（跟著資料夾走的略過）；跟角色匯入同一套規則（PlanRecords）——只新增、上次同步之後沒人改過的才換、改過的不蓋（--force 才蓋，先備份）、
	// 這個家沒裝或停用的套件略過；任何一筆不合格整包不換。舊的包沒有 records，什麼都不做、也不用連核心。
	var slugs []string
	for _, f := range man.Files {
		if s := cardSlug(f.Path); s != "" {
			slugs = append(slugs, s)
		}
	}
	store := recordStoreFor(o.Home, o.Store)
	inPack := map[string]bool{}
	for _, f := range man.Files {
		if rel, ok := Target(f.Path); ok {
			inPack[strings.ToLower(rel)] = true
		}
	}
	plan, err := PlanRecords(ctx, store, PlanInput{
		Records:     man.Records,
		Slugs:       slugs,
		Registry:    reg.Records,
		Force:       o.Force,
		PersonaOnly: true,
		// 包裡沒帶檔的照片紀錄不能指著這個家已經有的檔或別筆紀錄的檔（DEV-160 審查 M1）
		Carried:  func(rel string) bool { return inPack[strings.ToLower(rel)] },
		Occupied: func(rel string) bool { _, err := os.Lstat(HomePath(o.Home, rel)); return err == nil },
	})
	// 連不到這個家的核心（serve 沒在跑、還沒準備好、指令行程沒接）：檔案照樣換，紀錄這次不寫、版號不登記、記下待補，
	// 下一次同步（serve 的定期再試）接著寫。紀錄不合格（不屬於包裡的角色、欄位不合規格）才整包不換。
	recordsLater := ""
	if err != nil {
		if !IsUnavailable(err) {
			return failSync(o.Home, res, ptr.Version, "角色包的紀錄："+ErrText(err), now)
		}
		recordsLater, plan = ErrText(err), &RecordPlan{}
	}
	if !o.Force {
		// 你改過、沒換的檔：包裡新指著它的紀錄也不寫（紀錄會指著你自己的那份，DEV-160 第二輪審查）
		for _, p := range res.UserEdited {
			if rel, ok := Target(p); ok {
				plan.DropFileRecords(rel, "你改過、沒換")
			}
		}
	}
	if len(plan.Dropped) > 0 {
		// 照這個家的公開條件沒收的紀錄（PKG-68、PKG-71；含上層沒收的照片）指著的檔也不放（舊的包、別人做的包萬一帶了）。
		dropped := map[string]bool{}
		for _, rel := range plan.Dropped {
			dropped[strings.ToLower(rel)] = true
		}
		jobs = slices.DeleteFunc(jobs, func(j job) bool {
			rel, ok := Target(j.Path)
			if ok && dropped[strings.ToLower(rel)] {
				plan.Skipped = append(plan.Skipped, j.Path+"：是這個家不從官方角色包收的紀錄的圖，不放")
				return true
			}
			return false
		})
	}
	res.RecordsAdded, res.RecordsReplaced, res.RecordsUnchanged = plan.Added, plan.Replaced, plan.Unchanged
	res.RecordsKept, res.RecordsSkipped = plan.Kept, plan.Skipped

	idx := strings.LastIndex(ptr.URL, "/m/")
	if idx < 0 {
		return failSync(o.Home, res, ptr.Version, "pack.toml 的 url 要是 <包的位置>/m/<版號>.json", now)
	}
	base := ptr.URL[:idx+1]

	// 下載到快取（驗 sha256）；任何一個失敗就整批不換。
	cache := cacheDir(o.Home)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return failSync(o.Home, res, ptr.Version, "無法建立暫存資料夾", now)
	}
	for _, j := range jobs {
		if err := fetch.download(ctx, base+"f/"+j.SHA256+filepath.Ext(j.Path), filepath.Join(cache, j.SHA256), j.SHA256); err != nil {
			return failSync(o.Home, res, ptr.Version, "下載「"+j.Path+"」失敗（"+err.Error()+"）", now)
		}
	}

	// 備份要蓋掉的、使用者改過的檔案與紀錄（紀錄放 records/<種類>/<編號>.json）。
	var backupRoot string
	ensure := func() string {
		if backupRoot == "" {
			backupRoot = platform.RuntimeDir(o.Home, "backups", "personas-pack", now.Format("20060102-150405"))
		}
		return backupRoot
	}
	for _, j := range jobs {
		if !j.edited {
			continue
		}
		rel, _ := Target(j.Path) // 備份裡照家相對的 personas/… 放（兩版一樣）
		if err := CopyFile(j.dst, filepath.Join(ensure(), filepath.FromSlash(rel))); err != nil {
			return failSync(o.Home, res, ptr.Version, "無法備份你改過的檔案「"+j.Path+"」", now)
		}
	}
	if err := plan.Backup(ensure); err != nil {
		return failSync(o.Home, res, ptr.Version, err.Error(), now)
	}
	if backupRoot != "" {
		res.Backup = backupRoot // 第 2 版在家外面：給完整路徑
		if r, err := filepath.Rel(o.Home, backupRoot); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			res.Backup = r
		}
	}

	// 全部到齊才一次換進來（InstallAll：先寫暫存檔再改名，任何一步失敗就倒回去）。
	items := make([]Install, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, Install{Label: j.Path, Src: filepath.Join(cache, j.SHA256), Dst: j.dst, Exists: j.exists})
	}
	if err := InstallAll(items); err != nil {
		return failSync(o.Home, res, ptr.Version, err.Error(), now)
	}
	for _, j := range jobs {
		reg.Files[j.Path] = j.SHA256
		res.Installed = append(res.Installed, j.Path)
		if j.fresh {
			res.NewCards = append(res.NewCards, cardSlug(j.Path))
		}
	}
	sort.Strings(res.NewCards)
	// 紀錄經 vault 的正常寫入。萬一有一筆寫不進去：檔案與已寫的紀錄照樣登記、版號不登記（之後再試會接著寫），照實說。
	// new_cards 照樣回報：卡已經換進來，下一次同步不會再算它新（DEV-115 的招呼要這次排）。
	if recordsLater != "" {
		reg.SyncedAt = now.Format(time.RFC3339)
		_ = saveRegistry(o.Home, reg)
		res.Status, res.RecordsLater = StatusFailed, true
		res.Reason = fmt.Sprintf("包裡的 %d 筆紀錄這次沒寫：%s", len(man.Records), recordsLater)
		writePending(o.Home, Pending{Version: ptr.Version, Reason: res.Reason, At: now.Format(time.RFC3339)})
		writeLast(o.Home, res, now)
		return res, errors.New(res.Message())
	}
	written, failed, werr := plan.Write(ctx, store, reg.Records)
	if werr != nil {
		reg.SyncedAt = now.Format(time.RFC3339)
		_ = saveRegistry(o.Home, reg)
		done := "還沒有紀錄寫進去"
		if len(written) > 0 {
			done = "已經寫進去的紀錄：" + strings.Join(written, "、")
		}
		res.Status, res.Reason = StatusFailed, fmt.Sprintf("紀錄 %s 寫不進去（%s）；%s", failed, ErrText(werr), done)
		res.RecordsLater = IsUnavailable(werr) // 服務或套件服務暫時不行：serve 很快再試（PRS-6）；格式不對這類的照舊 6 小時
		writePending(o.Home, Pending{Version: ptr.Version, Reason: res.Reason, At: now.Format(time.RFC3339)})
		writeLast(o.Home, res, now)
		return res, errors.New(res.Message())
	}
	reg.Version, reg.SyncedAt = ptr.Version, now.Format(time.RFC3339)
	if err := saveRegistry(o.Home, reg); err != nil {
		return failSync(o.Home, res, ptr.Version, "檔案換進來了，但無法更新登記檔", now)
	}
	clearPending(o.Home)
	res.Status, res.Registered = StatusSynced, reg.Version
	writeLast(o.Home, res, now)
	return res, nil
}

// hasOfficialCards：官方角色資料夾（第 1 版 <家>/personas/）頂層有沒有任何一張官方卡（*.md 一般檔；mine/ 是子資料夾，不算）。
func hasOfficialCards(home string) bool {
	entries, err := os.ReadDir(platform.OfficialPersonasDir(home))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".md") && !strings.HasPrefix(e.Name(), ".") {
			return true
		}
	}
	return false
}

// cardSlug 回邏輯路徑 cards/<代號>.md 的代號；不是角色卡回空字串。
func cardSlug(p string) string {
	rest, ok := strings.CutPrefix(p, "cards/")
	if !ok || strings.Contains(rest, "/") {
		return ""
	}
	slug, ok := strings.CutSuffix(rest, ".md")
	if !ok || slug == "" {
		return ""
	}
	return slug
}

func failSync(home string, res *SyncResult, version, reason string, now time.Time) (*SyncResult, error) {
	res.Status, res.Reason = StatusFailed, reason
	res.Installed, res.Backup, res.NewCards = nil, "", nil
	writePending(home, Pending{Version: version, Reason: reason, At: now.Format(time.RFC3339)})
	writeLast(home, res, now)
	return res, errors.New(res.Message())
}

// RegisteredCards 回角色包登記檔裡有的官方角色代號（登記檔記的是包裡的邏輯路徑：cards/<代號>.md 的那些）與這個家有沒有登記檔
// （家外那份或 settings/ 那份）；evergenie home tidy 用它分辨官方位置的卡是不是官方的。讀不懂回錯誤。
func RegisteredCards(home string) (map[string]bool, bool, error) {
	found := false
	for _, p := range []string{registryPath(home), homeRegistryPath(home)} {
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			found = true
		}
	}
	codes := map[string]bool{}
	if !found {
		return codes, false, nil
	}
	reg, err := LoadRegistry(home)
	if err != nil {
		return nil, true, err
	}
	for p := range reg.Files {
		if slug := cardSlug(p); slug != "" && slugPattern.MatchString(slug) {
			codes[slug] = true
		}
	}
	return codes, true, nil
}
