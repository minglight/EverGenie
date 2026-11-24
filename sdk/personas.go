package sdk

import (
	"context"
	"time"
)

// PersonasAPI 是角色庫（人設卡的列出、新增、編輯、檢查、上線／設計中，說話層強度；P6-5a、D-31、REQ-130）。
// 實作在核心的 agent 模組。官方卡（personas/<代號>.md）唯讀；使用者自己的卡放在 personas/mine/<代號>.md。
// 寫入類方法只有主人（by 是 owner(…)）能呼叫，讀取類誰都可以。
type PersonasAPI interface {
	// List 列出每張卡：官方在前、我的在後，各自依代號排。
	List(ctx context.Context, req *PersonasListRequest) (*PersonasListResponse, error)
	// Get 讀一張卡（同一個代號：官方角色資料夾＞我的＞跟著程式的公開卡，PRS-27）：卡片資訊加內文與整份檔案文字。
	Get(ctx context.Context, req *PersonasGetRequest) (*PersonasGetResponse, error)
	// Create 新增一張我的卡（設計中，內文是範本）；代號已存在回 conflict，名字撞名回 invalid。
	Create(ctx context.Context, req *PersonasCreateRequest) (*PersonasCreateResponse, error)
	// Copy 把一張卡複製成新的我的卡（設計中）；名字必填，不能照抄。
	Copy(ctx context.Context, req *PersonasCopyRequest) (*PersonasCopyResponse, error)
	// Update 改我的卡的名字、別名、內文；用檔案修改時間做樂觀鎖，寫入前備份。
	Update(ctx context.Context, req *PersonasUpdateRequest) (*PersonasUpdateResponse, error)
	// SetStatus 把我的卡上線（先檢查）或移回設計中（有秘書坐著不行）。
	SetStatus(ctx context.Context, req *PersonasSetStatusRequest) (*PersonasSetStatusResponse, error)
	// Check 檢查一張卡（沒給代號＝全部）；回問題清單，空的表示沒問題。
	Check(ctx context.Context, req *PersonasCheckRequest) (*PersonasCheckResponse, error)
	// GetStyle 讀目前的說話層強度與各強度的全文。
	GetStyle(ctx context.Context, req *PersonasGetStyleRequest) (*PersonasGetStyleResponse, error)
	// SetStyle 設說話層強度（home.toml 的 [persona] style）。
	SetStyle(ctx context.Context, req *PersonasSetStyleRequest) (*PersonasSetStyleResponse, error)
	// Test 測口吻（P6-5b）：用固定情境、跟常駐一樣的指令，讓這位人設在某個座位上各說一次；立刻回結果檔名，背景跑。
	// 不開工具、不留 session，花的是座位那個帳號的額度；誰都可以叫。
	Test(ctx context.Context, req *PersonasTestRequest) (*PersonasTestResponse, error)
	// TestResults 讀這位人設最近幾次的測試結果（新到舊）。
	TestResults(ctx context.Context, req *PersonasTestResultsRequest) (*PersonasTestResultsResponse, error)
	// PackStatus 角色包的現況：登記的版號、這一版的版號、有沒有待補、上次同步的結果（REQ-283；CLI 為主，看板之後用）。
	PackStatus(ctx context.Context, req *PersonasPackStatusRequest) (*PersonasPackStatusResponse, error)
	// PackSync 請服務照這一版的指標同步角色包（DEV-157、PRS-28）：指令 `personas pack sync`、安裝完的第一次同步與看板的「現在再試」都走這裡，
	// 跟著角色走的紀錄（官方衣服的目錄）由服務寫；服務沒在跑時指令才自己換檔案。誰都可以叫；force 與指定別的指標只有主人。
	// 同步沒成功（沒網路、驗證失敗、紀錄還沒寫）一樣回 200，status 是 failed、reason 說原因。
	PackSync(ctx context.Context, req *PersonasPackSyncRequest) (*PersonasPackSyncResponse, error)
	// Export 把點名的角色打成匯出檔（.hgpersona，D-63、PRS-17）放在家的 runtime/persona-export/<資料夾>/，回看板的下載網址；
	// 跟指令 `personas export` 同一份程式。套件角色回 invalid。只有主人。
	Export(ctx context.Context, req *PersonasExportRequest) (*PersonasExportResponse, error)
	// Import 匯入看板上傳的匯出檔（runtime/persona-import/uploads/<upload>，PRS-18）：dry_run 只預覽、什麼都不寫；
	// 回的摘要跟指令 `personas import --json` 一樣。真的匯入完（或檔案不合格）就刪掉上傳的檔。只有主人。
	Import(ctx context.Context, req *PersonasImportRequest) (*PersonasImportResponse, error)
}

// PersonaImportMaxBytes 是一個角色匯出檔的上限（2 GB）：看板上傳（/api/web/persona-upload）與匯入（解開的總和）用同一個數字（PRS-18）。
const PersonaImportMaxBytes int64 = 2 << 30

// PersonaCard 是角色庫裡的一張卡。
type PersonaCard struct {
	Slug         string    `json:"slug,omitempty"`
	Name         string    `json:"name,omitempty"`
	Aka          []string  `json:"aka,omitempty"`
	Source       string    `json:"source,omitempty"`       // official（官方，唯讀）、mine（我的）、package（套件帶來的，唯讀；代號是 <套件id>.<slug>）
	Package      string    `json:"package,omitempty"`      // source 是 package 時：來自哪個套件 id
	Unassignable string    `json:"unassignable,omitempty"` // 套件停用或被拒時的白話原因（不能新指派；已經坐著的秘書不受影響）；空＝沒有這個問題
	Status       string    `json:"status,omitempty"`       // live（上線）、design（設計中，不能指派）
	Seats        []string  `json:"seats,omitempty"`        // 現在坐在哪些職位（職位 id）
	Shadowed     bool      `json:"shadowed"`               // 我的卡被同代號的官方卡蓋住
	AvatarURL    string    `json:"avatar_url,omitempty"`   // 有秘書坐著：那位的頭像；沒人坐的卡（官方、我的、套件的都一樣）：/avatars/persona/<代號>.png（DEV-115；我的與套件卡 SEC-12）。人設沒有頭像圖時是空（DEV-157）
	Tagline      string    `json:"tagline,omitempty"`      // 一句介紹：內文「一句話：」那一行（她是什麼樣的人）；卡上沒寫是空的（歡迎頁選角色用，WEB-176）
	UpdatedAt    time.Time `json:"updated_at"`             // 檔案修改時間；update 的 base_updated_at 就帶這個
}

type PersonasListRequest struct{}

type PersonasListResponse struct {
	Personas []PersonaCard `json:"personas,omitempty"`
}

type PersonasGetRequest struct {
	Slug string `json:"slug,omitempty"`
}

type PersonasGetResponse struct {
	Persona *PersonaCard `json:"persona,omitempty"`
	Body    string       `json:"body,omitempty"` // 內文原文
	Raw     string       `json:"raw,omitempty"`  // 整份檔案文字
}

type PersonasCreateRequest struct {
	Slug string   `json:"slug,omitempty"` // 主人給的名字；伺服器記成 my_<名字>
	Name string   `json:"name,omitempty"`
	Aka  []string `json:"aka,omitempty"`
	// Package 不空＝放進這個套件（packages/mine/<id>/personas/<代號>.md，並在 package.toml 宣告）而不是個人的 personas/mine/；
	// 只限 mine 層的套件，官方與 installed 回 forbidden。這時名字可以不給（用代號）。有效代號是 <套件id>.<代號>。
	Package string `json:"package,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type PersonasCreateResponse struct {
	Persona *PersonaCard `json:"persona,omitempty"`
}

type PersonasCopyRequest struct {
	From string   `json:"from,omitempty"` // 來源卡的代號
	Slug string   `json:"slug,omitempty"` // 新卡的代號
	Name string   `json:"name,omitempty"`
	Aka  []string `json:"aka,omitempty"`
	// Package 不空＝複製進這個套件（規則同 PersonasCreateRequest.Package；來源不動；名字、別名、代號沒給就沿用來源的；
	// 來源是我的卡時連頭像一起複製）。
	Package string `json:"package,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type PersonasCopyResponse struct {
	Persona *PersonaCard `json:"persona,omitempty"`
}

type PersonasUpdateRequest struct {
	Slug          string    `json:"slug,omitempty"`
	Name          *string   `json:"name,omitempty"`
	Aka           *[]string `json:"aka,omitempty"`
	Body          *string   `json:"body,omitempty"`
	BaseUpdatedAt time.Time `json:"base_updated_at"` // get／list 回的 updated_at；檔案之後被改過回 conflict
	By            *Actor    `json:"by,omitempty"`
}

type PersonasUpdateResponse struct {
	Persona *PersonaCard `json:"persona,omitempty"`
	Backup  string       `json:"backup,omitempty"` // 改之前的備份（相對家）
}

type PersonasSetStatusRequest struct {
	Slug   string `json:"slug,omitempty"`
	Status string `json:"status,omitempty"` // live、design
	By     *Actor `json:"by,omitempty"`
}

type PersonasSetStatusResponse struct {
	Persona *PersonaCard `json:"persona,omitempty"`
}

type PersonasCheckRequest struct {
	Slug string `json:"slug,omitempty"` // 空＝全部
}

type PersonasCheckResponse struct {
	Problems []string `json:"problems,omitempty"` // 一條一句白話，開頭是代號
}

// PersonaStyleLevel 是一個說話層強度選項與它的全文。
type PersonaStyleLevel struct {
	Level  string `json:"level,omitempty"` // a、b、c、d、custom
	Text   string `json:"text,omitempty"`
	Exists bool   `json:"exists"` // custom 才有意義：personas/_style.md 在不在
}

type PersonasGetStyleRequest struct{}

type PersonasGetStyleResponse struct {
	Level        string              `json:"level,omitempty"` // 目前生效的強度
	Text         string              `json:"text,omitempty"`  // 目前生效的全文
	CustomExists bool                `json:"custom_exists"`
	Levels       []PersonaStyleLevel `json:"levels,omitempty"` // 五個選項各自的全文
}

type PersonasSetStyleRequest struct {
	Level string `json:"level,omitempty"` // a、b、c、d、custom
	By    *Actor `json:"by,omitempty"`
}

type PersonasSetStyleResponse struct {
	Level        string `json:"level,omitempty"`
	Text         string `json:"text,omitempty"`
	CustomExists bool   `json:"custom_exists"`
	Backup       string `json:"backup,omitempty"`
}

type PersonasTestRequest struct {
	Slug  string   `json:"slug,omitempty"`
	Seat  string   `json:"seat,omitempty"`  // 在哪一位秘書的位置測；空＝坐這位人設、編號最小的啟用秘書
	Only  []string `json:"only,omitempty"`  // 只跑這些情境 id；空＝全部
	Model string   `json:"model,omitempty"` // 空＝座位的模型
	Card  string   `json:"card,omitempty"`  // 草稿卡的全文（看板編輯中還沒存的內容）；空＝讀存好的卡
	Style string   `json:"style,omitempty"` // a、b、c、d、custom；空＝目前設定
}

type PersonasTestResponse struct {
	Run       string `json:"run,omitempty"` // 結果檔名（<時間>.md）
	Scenarios int32  `json:"scenarios"`     // 這次要跑幾個情境
}

type PersonasTestResultsRequest struct {
	Slug  string `json:"slug,omitempty"`
	Limit int32  `json:"limit"` // 0＝10
}

// PersonaTestRun 是一次測口吻的結果。
type PersonaTestRun struct {
	Run      string    `json:"run,omitempty"`
	Status   string    `json:"status,omitempty"` // running、done、failed
	At       time.Time `json:"at"`
	Seat     string    `json:"seat,omitempty"`
	Model    string    `json:"model,omitempty"`
	Style    string    `json:"style,omitempty"`  // 說話層等級：a、b、c、d、custom
	Reason   string    `json:"reason,omitempty"` // failed 的原因
	Checks   string    `json:"checks,omitempty"` // 情境自己帶的檢查（例：提梗次數）的結果，例「通過 2／3（沒過：quip_tired）」；沒有檢查就是空的
	Markdown string    `json:"markdown,omitempty"`
}

type PersonasTestResultsResponse struct {
	Runs []PersonaTestRun `json:"runs,omitempty"`
}

type PersonasPackStatusRequest struct{}

// PersonaPackPending 是沒補上的角色包。
type PersonaPackPending struct {
	Version string `json:"version,omitempty"`
	Reason  string `json:"reason,omitempty"` // 白話原因
	At      string `json:"at,omitempty"`
}

// PersonaPackLast 是上次同步的結果。
type PersonaPackLast struct {
	Status     string   `json:"status,omitempty"` // synced｜up_to_date｜no_pack｜no_downgrade｜source｜failed
	Version    string   `json:"version,omitempty"`
	Installed  int32    `json:"installed"`
	Unchanged  int32    `json:"unchanged"`
	UserEdited []string `json:"user_edited,omitempty"` // 你改過、沒換的檔
	Reason     string   `json:"reason,omitempty"`
	At         string   `json:"at,omitempty"`
	// RecordsLater：檔案換進來了、跟著角色走的紀錄（官方衣服的目錄）還沒寫，服務很快會再試（DEV-157）。
	RecordsLater bool   `json:"records_later,omitempty"`
	Message      string `json:"message,omitempty"` // 給人看的一行（跟指令 `personas pack sync` 印的一樣）
}

type PersonasPackStatusResponse struct {
	Registered string              `json:"registered,omitempty"` // 登記的版號；空＝還沒同步過
	Latest     string              `json:"latest,omitempty"`     // 這一版核心指標的版號；空＝這一版沒有角色包
	Source     bool                `json:"source"`               // 角色包的來源（[admin] public_personas 有寫），不同步
	Pending    *PersonaPackPending `json:"pending,omitempty"`
	Last       *PersonaPackLast    `json:"last,omitempty"`
}

// PersonasPackSyncRequest 請服務同步角色包。
type PersonasPackSyncRequest struct {
	Force bool `json:"force,omitempty"` // 連你改過的檔與紀錄也蓋（蓋之前備份）；只有主人
	// Pointer 是指令那邊算出來的指標檔 pack.toml 的路徑（同一台電腦）；空＝用服務自己的。跟服務自己的不一樣時只有主人能指定。
	Pointer string `json:"pointer,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

// PersonasPackSyncResponse 是同步的結果，欄位跟指令 `personas pack sync --json` 一樣，多一行 message。
type PersonasPackSyncResponse struct {
	Status           string   `json:"status"` // synced｜up_to_date｜no_pack｜no_downgrade｜source｜failed
	Version          string   `json:"version,omitempty"`
	Registered       string   `json:"registered,omitempty"`
	Installed        []string `json:"installed,omitempty"`
	Unchanged        int32    `json:"unchanged"`
	UserEdited       []string `json:"user_edited,omitempty"`
	Backup           string   `json:"backup,omitempty"`
	NewCards         []string `json:"new_cards,omitempty"`
	RecordsAdded     []string `json:"records_added,omitempty"`
	RecordsReplaced  []string `json:"records_replaced,omitempty"`
	RecordsUnchanged int32    `json:"records_unchanged,omitempty"`
	RecordsKept      []string `json:"records_kept,omitempty"`
	RecordsSkipped   []string `json:"records_skipped,omitempty"`
	RecordsLater     bool     `json:"records_later,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	Message          string   `json:"message"` // 給人看的一行
}

type PersonasExportRequest struct {
	Personas []string `json:"personas,omitempty"` // 角色代號（官方位置或我的；套件角色不行）
	By       *Actor   `json:"by,omitempty"`
}

// PersonasExportResponse 是匯出的結果；檔案在看板 GET Download 一次後刪掉（放超過一小時沒拿的，下次匯出或匯入時清掉）。
type PersonasExportResponse struct {
	Personas []string `json:"personas,omitempty"`
	Files    int32    `json:"files"`
	Records  int32    `json:"records"`
	Bytes    int64    `json:"bytes"`              // 匯出檔的大小
	Kinds    []string `json:"kinds,omitempty"`    // 帶了哪些種類的紀錄（完整名稱）
	Skipped  []string `json:"skipped,omitempty"`  // 刻意沒帶的（白話一行一個）
	Filename string   `json:"filename,omitempty"` // 下載時的檔名：<名字或代號>.hgpersona
	Download string   `json:"download,omitempty"` // 看板的下載網址：/persona-export/<資料夾>/<檔名>（檔名 URL 編碼過）
}

type PersonasImportRequest struct {
	Upload string `json:"upload,omitempty"` // 看板上傳回的 name（runtime/persona-import/uploads/ 底下）
	DryRun bool   `json:"dry_run,omitempty"`
	Force  bool   `json:"force,omitempty"` // 你改過的檔與紀錄也蓋（先備份）
	By     *Actor `json:"by,omitempty"`
}

// PersonasImportResponse 跟指令 `personas import --json` 一樣：檔案是家底下的相對路徑，紀錄是 `<種類>/<編號>`。
type PersonasImportResponse struct {
	DryRun           bool     `json:"dry_run"`
	Personas         []string `json:"personas"`
	ExportedAt       string   `json:"exported_at,omitempty"`
	Added            []string `json:"added,omitempty"`    // 新增的檔
	Replaced         []string `json:"replaced,omitempty"` // 換掉的檔
	Unchanged        int32    `json:"unchanged"`          // 已經一樣的檔
	Kept             []string `json:"kept,omitempty"`     // 你改過的，沒換（force 才換）
	RecordsAdded     []string `json:"records_added,omitempty"`
	RecordsReplaced  []string `json:"records_replaced,omitempty"`
	RecordsUnchanged int32    `json:"records_unchanged"`
	RecordsKept      []string `json:"records_kept,omitempty"`    // 已經有、內容不同，沒換（force 才換）
	RecordsSkipped   []string `json:"records_skipped,omitempty"` // 略過的紀錄：「<種類>/<編號>：原因」
	Backup           string   `json:"backup,omitempty"`          // force 蓋掉的東西備份在哪（家底下的相對路徑）
}
