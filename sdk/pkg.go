package sdk

import (
	"context"
	"time"
)

// PackageService：套件的註冊、安裝、擴充點、fork（D-2、D-3）。manifest 欄位見 evergenie docs show schemas/package.md。
// 套件 SDK 與核心用同一份協定。
type PackageAPI interface {
	// Hello 是附程式的套件行程啟動後的第一個呼叫：報上 id 與協定版本，核心回它能用的資料路徑與能力。
	Hello(ctx context.Context, req *PackageHelloRequest) (*PackageHelloResponse, error)
	List(ctx context.Context, req *PackageListRequest) (*PackageListResponse, error)
	Get(ctx context.Context, req *PackageGetRequest) (*PackageGetResponse, error)
	// Install 從看板按鈕安裝；Reload 偵測到 packages/ 有改動就重編重啟（REQ-92）。
	Install(ctx context.Context, req *PackageInstallRequest) (*PackageInstallResponse, error)
	Uninstall(ctx context.Context, req *PackageUninstallRequest) (*PackageUninstallResponse, error)
	Reload(ctx context.Context, req *PackageReloadRequest) (*PackageReloadResponse, error)
	SetEnabled(ctx context.Context, req *PackageSetEnabledRequest) (*PackageSetEnabledResponse, error)
	// Fork 把官方套件整包複製進 packages/mine/，記下基準版本（D-3）。
	Fork(ctx context.Context, req *PackageForkRequest) (*PackageForkResponse, error)
	// New 在 packages/mine/<id>/ 建一個空白的使用者套件（資料夾、manifest、一位秘書樣板、一個最小頁面）；只寫自己的層，id 撞官方、已存在、保留字都拒絕（SEC-302、REQ-306）。
	New(ctx context.Context, req *PackageNewRequest) (*PackageNewResponse, error)
	// ListUpdates 列出 fork 的基準版本與官方新版的差異（由 AI 整理成白話清單，逐項問使用者，REQ-95）。
	ListUpdates(ctx context.Context, req *PackageListUpdatesRequest) (*PackageListUpdatesResponse, error)
	ListExtensions(ctx context.Context, req *PackageListExtensionsRequest) (*PackageListExtensionsResponse, error)
	// SetExtension 改一個擴充點的值；擴充點宣告了 owner_only 時只有主人本人（by 是 owner(…)）能改，其他人 FORBIDDEN。
	SetExtension(ctx context.Context, req *PackageSetExtensionRequest) (*PackageSetExtensionResponse, error)
	// InstallGo：附程式的套件需要 Go、這台電腦沒有可以用的 Go 時，秘書先用提問工具問主人一句，主人同意後才呼叫（confirmed 必須是 true）；
	// 下載版的核心把寫死的那一版 Go 從官方下載點下載到程式自己的資料夾（<app>/tools/go，對過 sha256；不用 Homebrew、不用密碼、不改 PATH），
	// 裝好就重編等著 Go 的套件（D-27、REQ-171）。原始碼版不下載（開發機自己裝 Go），已經有 Go 時只重編。
	InstallGo(ctx context.Context, req *PackageInstallGoRequest) (*PackageInstallGoResponse, error)
	// GoStatus 回編譯附程式的套件要用的 Go 現在能不能用（唯讀，不下載）：先看程式自己的工具資料夾、再看 PATH，版本要夠新。
	GoStatus(ctx context.Context, req *PackageGoStatusRequest) (*PackageGoStatusResponse, error)
	// Sync 把套件帶來的秘書職責與排程樣板對到這個家：預設只預演（apply = false），只有主人能套用；已存在的排程不改（D-28、REQ-174）。
	Sync(ctx context.Context, req *PackageSyncRequest) (*PackageSyncResponse, error)
	// Preview 先看一個套件（git 網址或本機資料夾）：抓下來、驗過，回內容摘要，不安裝（REQ-282）。
	Preview(ctx context.Context, req *PackagePreviewRequest) (*PackagePreviewResponse, error)
	// Outdated 列出從網址裝的套件有沒有新版（看來源的 semver tag）。
	Outdated(ctx context.Context, req *PackageOutdatedRequest) (*PackageOutdatedResponse, error)
	// Upgrade 把一個從網址裝的套件換成新版（資料保留；失敗換回舊的）。
	Upgrade(ctx context.Context, req *PackageUpgradeRequest) (*PackageUpgradeResponse, error)
	// Export 把自己的（mine）或分享來的（installed）套件資料夾打包出去分享：只帶資料夾內容，不帶使用者的資料與 runtime；
	// 先掃疑似個資，有就停下列出（allow_findings 才照樣匯出）。官方套件回 FORBIDDEN（SEC-302 S5、REQ-334）。
	Export(ctx context.Context, req *PackageExportRequest) (*PackageExportResponse, error)
	// ClaimNotify 發「這個職位還沒有人負責，建議用 X」的認領通知給主人（SEC-302 S9）；不建秘書、不套用（型別在 pkgclaim.go）。
	ClaimNotify(ctx context.Context, req *PackageClaimNotifyRequest) (*PackageClaimNotifyResponse, error)
}

type Tier string

const (
	TierOfficial  Tier = "official"
	TierInstalled Tier = "installed"
	TierMine      Tier = "mine"
)

type PackageKind string

const (
	PackageKindData    PackageKind = "data"
	PackageKindProgram PackageKind = "program"
)

type PackageState string

const (
	PackageStateStopped  PackageState = "stopped"
	PackageStateBuilding PackageState = "building"
	PackageStateRunning  PackageState = "running"
	PackageStateFailed   PackageState = "failed"   // 只有它自己壞，核心會重啟並通知（REQ-96）
	PackageStateRejected PackageState = "rejected" // api 版本不認得、相依缺少
)

// PackageRemedy 是「這個套件失敗了，可以怎麼補救」的機器可讀代號；秘書與看板依它提出補救（要使用者同意）。
type PackageRemedy string

const (
	PackageRemedyInstallGo PackageRemedy = "install_go" // 這台電腦沒有可以用的 Go：問主人一句，同意後呼叫 InstallGo
)

type ExtensionKind string

const (
	ExtensionKindSetting      ExtensionKind = "setting"
	ExtensionKindPromptAppend ExtensionKind = "prompt_append"
	ExtensionKindPageSlot     ExtensionKind = "page_slot"
)

type Requirement struct {
	ID      string `json:"id,omitempty"`
	Version string `json:"version,omitempty"`
}

type Capabilities struct {
	Read   []string `json:"read,omitempty"`
	Write  []string `json:"write,omitempty"`
	Events []string `json:"events,omitempty"`
}

type MenuItemDecl struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Icon      string `json:"icon,omitempty"`
	GroupHint string `json:"group_hint,omitempty"` // 建議放哪個群組（只影響預設的選單樹，使用者的樹優先）
	Order     int32  `json:"order"`
	Front     bool   `json:"front,omitempty"` // 預設樹排在「待辦」後面（習慣與健康，DEV-135）；使用者的樹優先
	ShowWhen  string `json:"show_when,omitempty"`
	Page      string `json:"page,omitempty"` // 對應 PageDecl.id
	// Section：同一個 section 的項目在看板合成一個入口（標題 SectionTitle），頁面上方是它們的分頁（例：飲食、運動、身體 → 健康）。
	Section      string `json:"section,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
}

type PageDecl struct {
	ID        string `json:"id,omitempty"`
	Title     string `json:"title,omitempty"`
	Route     string `json:"route,omitempty"`      // 保留未用：頁面網址一律是 /p/<套件 id>/<頁面 id>；寫了也不會變成網址，第一段是核心路由的字會被拒（REQ-352）
	Component string `json:"component,omitempty"`  // pages/*.tsx，核心內嵌 esbuild 編譯（D-11）
	Slot      string `json:"slot,omitempty"`       // 掛在別的頁面的哪個 slot
	SlotFront bool   `json:"slot_front,omitempty"` // 在 slot 裡排最前面（今天頁的習慣卡）
	// Chat：看板寬螢幕時在頁面旁邊放一欄聊天（REQ-235）；ChatAgent 是預設聊天的秘書（職位 id，沒有這位就讓主人選）。
	Chat      bool   `json:"chat"`
	ChatAgent string `json:"chat_agent,omitempty"`
	// Merge：manifest 宣告這頁在看板連多台時可以合併各台的資料（S3 只做宣告；看板照它給 host.sites 留到 S3-10）。
	Merge bool `json:"merge,omitempty"`
	// ShowWhen：掛在 slot 的頁面什麼時候才列出來，寫法同選單項目的 show_when（例 `cap:<套件>.<能力>`，MNU-11）；空＝一直列。
	ShowWhen string `json:"show_when,omitempty"`
}

type ExtensionPoint struct {
	ID           string        `json:"id,omitempty"`
	Kind         ExtensionKind `json:"kind,omitempty"`
	Description  string        `json:"description,omitempty"`
	DefaultValue string        `json:"default_value,omitempty"`
	OwnerOnly    bool          `json:"owner_only,omitempty"` // 宣告了 owner_only：值只有主人本人能改（SetExtension 擋其他人）
	Package      string        `json:"package,omitempty"`    // 這個擴充點是哪個套件的（ListExtensions 填）
	Agent        string        `json:"agent,omitempty"`      // prompt_append：接在哪個職位的指令後面；空＝每一位秘書
}

type ForkInfo struct {
	Of          string    `json:"of,omitempty"`
	BaseVersion string    `json:"base_version,omitempty"`
	ForkedAt    time.Time `json:"forked_at,omitempty"`
}

// DataFieldDecl 是套件資料種類的一個欄位（欄位規格檔 schemas/<種類>.toml 的 [[field]]，D-28）。
type DataFieldDecl struct {
	Name        string   `json:"name,omitempty"`
	Type        string   `json:"type,omitempty"` // string｜int｜number｜bool｜date｜datetime｜enum｜list｜map
	Required    bool     `json:"required"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
}

// AgentDecl 是套件帶來的一個職位樣板（agents/<id>/agent.md 的標頭）：職位 id、一句話職責、顏色、是不是選用。
type AgentDecl struct {
	ID        string `json:"id,omitempty"`
	Duty      string `json:"duty,omitempty"`
	Color     string `json:"color,omitempty"`
	ColorDark string `json:"color_dark,omitempty"`
	Optional  bool   `json:"optional,omitempty"` // 選用職位（標頭 optional: true，DEV-157）：不自動帶出來，主人指名才加
}

// DataKindDecl 是套件宣告的一種資料（manifest 的 [[contributes.data]]）；完整種類名稱是 <套件 id>.<Kind>。
type DataKindDecl struct {
	Kind      string          `json:"kind,omitempty"`
	Title     string          `json:"title,omitempty"`      // 白話名稱
	IDs       string          `json:"ids,omitempty"`        // seq（核心發編號）｜given（呼叫者給編號）
	Prefix    string          `json:"prefix,omitempty"`     // ids = seq 時的編號開頭，例 M-
	DateField string          `json:"date_field,omitempty"` // 依哪個 date 欄位篩日期（data list --since/--until）；沒寫就用 created_at
	Fields    []DataFieldDecl `json:"fields,omitempty"`
	// 匯出角色時這種資料要不要跟著走（PKG-57）：空＝不跟；by_persona＝PersonaField 的值是被匯出的角色時跟著走；
	// by_folder＝FolderField 是 personas/ 底下的資料夾，裡面有被匯出角色的圖時，這一筆和那些圖一起走。
	Export string `json:"export,omitempty"`
	// PersonaField 是值為角色代號的欄位：export = by_persona 時也是匯出用的欄位（沒寫就是 persona）；
	// 沒有 export 也可以單獨寫（PKG-64），只表示「角色改代號時這個欄位跟著改」。讀的人要先看 Export 再決定拿它做什麼。
	PersonaField string `json:"persona_field,omitempty"`
	FolderField  string `json:"folder_field,omitempty"` // export = by_folder 時的欄位
	// ExportOmit 是匯出時拿掉的欄位（PKG-58）：像開關這類只屬於這個家當下的狀態；匯入時本機已有這筆就保留本機的值。
	ExportOmit []string `json:"export_omit,omitempty"`
	// PackOnly 是「官方角色包只帶哪些」（PKG-68，只配 export = by_persona）：欄位名 → 可以公開的值；每個列出的欄位的值都在清單裡才進官方角色包，
	// 少了那個欄位、值不在清單裡的不帶（建包不收；同步時包裡萬一帶了也不寫）。沒寫＝這種資料公開角色的每一筆都帶。主人點名的角色匯出不看它。
	// PackOnlyNote 是被擋下的紀錄的白話名稱（例「還沒升成官方或還在設計中的衣服」），建包的輸出用；沒寫就用通用的說法。
	PackOnly     map[string][]string `json:"pack_only,omitempty"`
	PackOnlyNote string              `json:"pack_only_note,omitempty"`
	// PackExcept 是「官方角色包不帶哪些」（PKG-71，只配 export = by_persona）：欄位名 → 不公開的值；任何一個列出的欄位的值在清單裡就不進官方角色包
	// （建包不收、同步不寫，例：衣服標了 private = true）；少了那個欄位、值不在清單裡＝不擋。跟 PackOnly 一起看：兩個都過才帶。主人點名的角色匯出不看它。
	// PackExceptNote 是被它擋下的紀錄的白話名稱（例「主人標了不公開的衣服」）；沒寫就用通用的說法。
	PackExcept     map[string][]string `json:"pack_except,omitempty"`
	PackExceptNote string              `json:"pack_except_note,omitempty"`
	// 角色改代號時紀錄怎麼跟著走（PKG-64，evergenie home tidy、personas promote 用）：
	// PersonaID＝exact（編號就是角色代號）或 prefix（編號是 <角色代號>-<其他>），只給 ids = given；
	// PersonaPrefixed＝值是 <角色代號>-<其他> 的字串欄位（例：指到某套衣服編號的欄位）；
	// PersonaSource＝值是 official／mine、說這筆的圖在官方角色資料夾還是主人的 personas/mine/ 的欄位（角色從官方位置收成自己的時改成 mine）。
	PersonaID       string   `json:"persona_id,omitempty"`
	PersonaPrefixed []string `json:"persona_prefixed,omitempty"`
	PersonaSource   string   `json:"persona_source,omitempty"`
	// 紀錄指著檔的種類（DEV-160，只配 export = by_persona）：
	// FileField＝值是這筆的檔的欄位，相對於擁有者的位置——PersonaSource 欄位是 mine 時是主人的 personas/mine/，其他（含沒宣告 PersonaSource）是官方角色資料夾。
	// 角色匯出與官方角色包照它帶檔（指到哪就帶哪個），不再照檔名認圖。
	// ExportOnly＝跟著角色走時（匯出與官方角色包都看）只帶哪些，格式同 PackOnly；不符合的紀錄與它的檔都不帶。
	// PackParent＝上層紀錄：Field 欄位的值是同一個套件 Kind 種類的編號；上層沒進官方角色包時，這筆連同它的檔也不進。
	FileField  string              `json:"file_field,omitempty"`
	ExportOnly map[string][]string `json:"export_only,omitempty"`
	PackParent *DataParentDecl     `json:"pack_parent,omitempty"`
}

// DataParentDecl 是 DataKindDecl.PackParent：Kind 是同一個套件的種類（不含套件 id），Field 是放上層編號的欄位。
type DataParentDecl struct {
	Kind  string `json:"kind"`
	Field string `json:"field"`
}

// DataKindDecl.PersonaID 的值（PKG-64）。
const (
	DataPersonaIDExact  = "exact"
	DataPersonaIDPrefix = "prefix"
)

// DataKindDecl.Export 的值（PKG-57）。
const (
	DataExportByPersona = "by_persona"
	DataExportByFolder  = "by_folder"
)

type Package struct {
	ID           string           `json:"id,omitempty"`
	Name         string           `json:"name,omitempty"`
	Version      string           `json:"version,omitempty"`
	Description  string           `json:"description,omitempty"`
	Kind         PackageKind      `json:"kind,omitempty"`
	Tier         Tier             `json:"tier,omitempty"`
	API          string           `json:"api,omitempty"`
	State        PackageState     `json:"state,omitempty"`
	Enabled      bool             `json:"enabled"`
	Optional     bool             `json:"optional,omitempty"` // 選用套件：沒有明確設定時預設不啟用，設定精靈會問（REQ-176）
	DevOnly      bool             `json:"dev_only,omitempty"` // 只給開發用的家：看板設定頁只在開發家列出來（正式家開著時才列，讓人關掉）
	Avatars      string           `json:"avatars,omitempty"`  // 公開資料夾裡放「現在的頭像」的子資料夾（contributes.avatars，D-29）
	Requires     []Requirement    `json:"requires,omitempty"`
	Capabilities *Capabilities    `json:"capabilities,omitempty"`
	Menu         []MenuItemDecl   `json:"menu,omitempty"`
	Pages        []PageDecl       `json:"pages,omitempty"`
	Data         []DataKindDecl   `json:"data,omitempty"`   // 套件宣告的資料種類（D-28）
	Agents       []AgentDecl      `json:"agents,omitempty"` // 套件帶來的職位樣板（contributes.agent；看板列出還沒人負責的職位，REQ-238）
	Extensions   []ExtensionPoint `json:"extensions,omitempty"`
	Fork         *ForkInfo        `json:"fork,omitempty"`
	Source       *PackageSource   `json:"source,omitempty"` // 從網址裝的：來源（網址、版本、commit）；本機資料夾裝的沒有（REQ-282）
	Error        string           `json:"error,omitempty"`  // FAILED／REJECTED 的白話原因
	Remedy       PackageRemedy    `json:"remedy,omitempty"` // 有可以補救的做法時填（例：install_go），要主人同意才做
}

type PackageHelloRequest struct {
	ID      string `json:"id,omitempty"`
	API     string `json:"api,omitempty"`
	Version string `json:"version,omitempty"`
}

type PackageHelloResponse struct {
	Token   string   `json:"token,omitempty"`    // 之後呼叫其他服務用；只開放 manifest 宣告過的能力（D-19：邊界，不是安全）
	DataDir string   `json:"data_dir,omitempty"` // vault/pkg/<id>/
	Granted []string `json:"granted,omitempty"`
}

type PackageListRequest struct {
	Tier Tier `json:"tier,omitempty"` // UNSPECIFIED 表示全部
}

type PackageListResponse struct {
	Packages []Package `json:"packages,omitempty"`
	Warnings []string  `json:"warnings,omitempty"` // 擴充點不見了、官方有新版等提醒（不影響運作）
}

type PackageGetRequest struct {
	ID string `json:"id,omitempty"`
}

type PackageGetResponse struct {
	Package *Package `json:"package,omitempty"`
}

type PackageInstallRequest struct {
	Source string `json:"source,omitempty"` // 網址（https://、ssh://、git@、file://）或本機資料夾的完整路徑
	Tier   Tier   `json:"tier,omitempty"`
	By     *Actor `json:"by,omitempty"`
	// Ref：從網址裝時指定的版本（tag 或分支）；空＝最大的 semver tag，沒有 tag 就用預設分支（REQ-282）。
	Ref string `json:"ref,omitempty"`
	// Confirmed：從網址裝一定要 true（使用者看過預覽、同意了），本機資料夾不用。
	Confirmed bool `json:"confirmed,omitempty"`
}

type PackageInstallResponse struct {
	Package *Package `json:"package,omitempty"`
}

type PackageUninstallRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type PackageUninstallResponse struct {
}

type PackageReloadRequest struct {
	ID string `json:"id,omitempty"` // 空表示全部
}

type PackageReloadResponse struct {
	Packages []Package `json:"packages,omitempty"`
}

type PackageSetEnabledRequest struct {
	ID      string `json:"id,omitempty"`
	Enabled bool   `json:"enabled"`
	By      *Actor `json:"by,omitempty"`
}

type PackageSetEnabledResponse struct {
	Package *Package `json:"package,omitempty"`
}

type PackageForkRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type PackageForkResponse struct {
	Package *Package `json:"package,omitempty"`
}

// PackageNewRequest：id 要是 ^[a-z][a-z0-9-]{1,30}$、不能是保留字、不能跟官方套件或已有的套件同名；name 空白就用 id。
type PackageNewRequest struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"` // 顯示名稱（選填）
	By   *Actor `json:"by,omitempty"`
}

// PackageNewResponse：Dir 是家裡的相對路徑（packages/mine/<id>）；Files 是建好的檔案（相對 Dir，排序過）。
type PackageNewResponse struct {
	Package *Package `json:"package,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Files   []string `json:"files,omitempty"`
}

type UpdateItem struct {
	Path              string `json:"path,omitempty"`    // 套件裡的哪個檔（相對路徑）
	Summary           string `json:"summary,omitempty"` // 白話說明這一項改了什麼
	Diff              string `json:"diff,omitempty"`
	ConflictsWithFork bool   `json:"conflicts_with_fork"`
}

type PackageListUpdatesRequest struct {
	ID string `json:"id,omitempty"` // fork 的套件 id
}

type PackageListUpdatesResponse struct {
	BaseVersion   string       `json:"base_version,omitempty"`
	LatestVersion string       `json:"latest_version,omitempty"`
	Items         []UpdateItem `json:"items,omitempty"`
	ReportPath    string       `json:"report_path,omitempty"` // 寫給 AI 讀的差異清單檔（相對於家）；沒有差異時空白
}

type PackageListExtensionsRequest struct {
	Package string `json:"package,omitempty"` // 空表示全部
}

type PackageListExtensionsResponse struct {
	Extensions []ExtensionPoint  `json:"extensions,omitempty"`
	Values     map[string]string `json:"values,omitempty"`   // key 是 <套件 id>.<擴充點 id>
	Warnings   []string          `json:"warnings,omitempty"` // 設定引用了已經不存在的擴充點（值保留、沒有作用）
}

type PackageSetExtensionRequest struct {
	Package   string         `json:"package,omitempty"`
	Extension string         `json:"extension,omitempty"`
	Value     map[string]any `json:"value,omitempty"`
	By        *Actor         `json:"by,omitempty"`
}

type PackageSetExtensionResponse struct {
}

type PackageInstallGoRequest struct {
	Confirmed bool   `json:"confirmed"` // 使用者已經明確同意安裝（秘書在對話裡問過、或主人在看板按了）
	By        *Actor `json:"by,omitempty"`
}

type PackageInstallGoResponse struct {
	Command  string    `json:"command,omitempty"`  // 這次做了什麼（白話，例「下載 Go go1.27.4 到程式自己的資料夾」；已經有 Go、沒有下載時空白）
	Packages []Package `json:"packages,omitempty"` // 原本在等 Go、已經排進重編的套件
	Version  string    `json:"version,omitempty"`  // 現在要用的 Go 的版本（例 go1.27.4）
}

type PackageGoStatusRequest struct {
}

// PackageGoStatusResponse：編譯附程式的套件要用的 Go（packages PKG-65）。usable 是 false 時 detail 是白話原因；
// installable 是 true 時 ask 是秘書可以照著問主人的一句話，主人同意才呼叫 InstallGo。
type PackageGoStatusResponse struct {
	Usable      bool   `json:"usable"`            // 找得到能用、版本夠新的 Go
	Source      string `json:"source,omitempty"`  // 用的是哪裡的：tools（程式自己的工具資料夾）、path（這台電腦本來就有的）
	Path        string `json:"path,omitempty"`    // 那支 go 的位置
	Version     string `json:"version,omitempty"` // 那支 go 的版本（例 go1.27.4）
	Need        string `json:"need,omitempty"`    // 最低要的版本（例 go1.24）
	Installable bool   `json:"installable"`       // InstallGo 能幫忙下載（下載版、這台晶片有官方的包）
	Offer       string `json:"offer,omitempty"`   // InstallGo 會下載的版本
	SizeMB      int    `json:"size_mb,omitempty"` // 下載大約多大（MB）
	Detail      string `json:"detail,omitempty"`  // 白話說明：為什麼不能用、為什麼不能幫忙下載
	Ask         string `json:"ask,omitempty"`     // 不能用而且能幫忙下載時，問主人的那一句
}

type PackageSyncRequest struct {
	ID    string `json:"id,omitempty"`
	Apply bool   `json:"apply"` // false＝只預演；true＝真的套用（只有主人）
	// Agent：只同步這一個職位的樣板與它的排程（空＝套件全部）；套件沒有這個職位回 not_found（REQ-238）。
	Agent string `json:"agent,omitempty"`
	// Persona：要新增的秘書用這個人設（人設 slug；空＝home.toml 的 defaults.persona）。已經在名冊上的不換人設。
	Persona string `json:"persona,omitempty"`
	// Knowledge：新增的秘書要勾的可選知識 id（DEV-161，要同時指定 agent）；沒給＝照職責的預設，JSON 給 [] ＝全部不勾（Go 空切片會被 omitempty 丟掉）。已經在名冊上的不改（之後用 agents.set_knowledge）。
	Knowledge []string `json:"knowledge,omitempty"`
	By        *Actor   `json:"by,omitempty"`
}

// SyncAgent 是套件樣板裡的一位秘書：不存在＝create、職責不同＝update、相同＝same；Added／Removed 是職責逐行差異的增刪行數。
type SyncAgent struct {
	ID      string `json:"id,omitempty"`
	Action  string `json:"action,omitempty"` // create｜update｜same
	Added   int32  `json:"added"`
	Removed int32  `json:"removed"`
	// Persona 是 create 時這位新秘書會用的角色（預演也看得到）：--persona 指定的 > 樣板 agents/<id>/agent.md 的預設 persona > home.toml 的預設人設；
	// PersonaFrom 說是哪一個來源（request｜template｜home）。都沒有時兩個都空（套用會要求先指定人設）。
	Persona     string `json:"persona,omitempty"`
	PersonaFrom string `json:"persona_from,omitempty"`
}

// SyncJob 是套件樣板裡的一個排程：該秘書沒有同 id 的排程＝create，有＝exists（不改）。
type SyncJob struct {
	Agent  string `json:"agent,omitempty"`
	ID     string `json:"id,omitempty"`
	Action string `json:"action,omitempty"` // create｜exists
}

type PackageSyncResponse struct {
	Agents   []SyncAgent `json:"agents,omitempty"`
	Jobs     []SyncJob   `json:"jobs,omitempty"`
	Applied  bool        `json:"applied"`
	Optional []string    `json:"optional,omitempty"` // 沒指定職位時略過的選用職位（名冊上還沒有的，DEV-157）；要加就指名 --agent
}

// PackageSource 是從網址裝的套件的來源（安裝時記在套件資料夾的 .source.toml，REQ-282）。
type PackageSource struct {
	URL         string    `json:"url,omitempty"`
	Ref         string    `json:"ref,omitempty"`    // 裝的是哪個 tag 或分支
	Commit      string    `json:"commit,omitempty"` // 實際抓到的 commit
	InstalledAt time.Time `json:"installed_at"`
}

type PackagePreviewRequest struct {
	Source string `json:"source,omitempty"` // git 網址或本機資料夾（完整路徑）
	Ref    string `json:"ref,omitempty"`
}

// PackagePreviewResponse 是裝之前先看的內容摘要：抓下來、驗過，不安裝。
type PackagePreviewResponse struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Kind        string   `json:"kind,omitempty"`
	RunsProgram bool     `json:"runs_program"` // kind == program：會在你的電腦上編譯並執行
	Agents      []string `json:"agents,omitempty"`
	Pages       []string `json:"pages,omitempty"`
	Jobs        []string `json:"jobs,omitempty"`
	DataKinds   []string `json:"data_kinds,omitempty"`
	Reads       []string `json:"reads,omitempty"`
	Writes      []string `json:"writes,omitempty"`
	Core        string   `json:"core,omitempty"`     // 要求的核心版本
	Ref         string   `json:"ref,omitempty"`      // 實際抓的版本
	Commit      string   `json:"commit,omitempty"`   // 實際抓的 commit
	Problems    []string `json:"problems,omitempty"` // 擋住安裝的問題（白話）；空＝可以裝
	Warnings    []string `json:"warnings,omitempty"`
	Exists      bool     `json:"exists"` // 已經裝了同一個 id
	// Personas 是這個套件帶來的角色（[[contributes.persona]]，SEC-302 S5、REQ-338）。
	Personas []PackagePreviewPersona `json:"personas,omitempty"`
}

type PackageOutdatedRequest struct{}

type PackageOutdated struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Current string `json:"current,omitempty"`
	Latest  string `json:"latest,omitempty"`
	URL     string `json:"url,omitempty"`
}

type PackageOutdatedResponse struct {
	Packages []PackageOutdated `json:"packages,omitempty"`
	Failed   []string          `json:"failed,omitempty"` // 抓不到的網址（白話）
}

type PackageUpgradeRequest struct {
	ID  string `json:"id,omitempty"`
	Ref string `json:"ref,omitempty"` // 空＝來源最新的 semver tag
	By  *Actor `json:"by,omitempty"`
}

type PackageUpgradeResponse struct {
	Package *Package `json:"package,omitempty"`
	From    string   `json:"from,omitempty"`
	To      string   `json:"to,omitempty"`
}

// ---- SEC-302 S5：分享套件（export）與預覽帶來的角色 ----

// PackagePreviewPersona 是預覽裡的一個角色：ID 是有效代號 <套件 id>.<slug>（例 garden.fairy）。
type PackagePreviewPersona struct {
	ID     string `json:"id,omitempty"`
	Slug   string `json:"slug,omitempty"`
	Name   string `json:"name,omitempty"` // 卡的 name；讀不出來空白
	Avatar bool   `json:"avatar"`         // 有 personas/avatars/<slug>.png 或 .mp4
}

// PackageExportRequest：Out 是絕對路徑，結尾 .tar.gz／.tgz 打成壓縮檔，其他當資料夾（不能已存在，空資料夾可以）；
// 空＝<家>/runtime/exports/<id>-<version>/。Git 產生可以 git init && git push 的資料夾（加 .gitignore，不跑 git；只限資料夾）。
// AllowFindings：掃到疑似個資仍照樣匯出（主人看過清單、確定沒問題才給）。
type PackageExportRequest struct {
	ID            string `json:"id,omitempty"`
	Out           string `json:"out,omitempty"`
	Git           bool   `json:"git"`
	AllowFindings bool   `json:"allow_findings"`
	By            *Actor `json:"by,omitempty"`
}

// PackageExportFinding 是一筆疑似個資：哪個檔第幾行（0＝檔名本身）、哪一類、白話說明（不含完整內容）。
type PackageExportFinding struct {
	Path string `json:"path,omitempty"` // 相對套件資料夾
	Line int32  `json:"line"`
	Kind string `json:"kind,omitempty"` // secret｜secret_file｜home_path｜email
	What string `json:"what,omitempty"`
}

// PackageExportResponse：Blocked＝掃到疑似個資而且沒有 allow_findings，什麼都沒寫（Path 空白）。
type PackageExportResponse struct {
	Path     string                 `json:"path,omitempty"`    // 匯出到哪（絕對路徑）
	Format   string                 `json:"format,omitempty"`  // dir｜tar.gz
	Files    []string               `json:"files,omitempty"`   // 匯出（或會匯出）的檔案，相對套件資料夾、排序過
	Skipped  []string               `json:"skipped,omitempty"` // 沒帶的東西與原因（白話）
	Findings []PackageExportFinding `json:"findings,omitempty"`
	Blocked  bool                   `json:"blocked"`
}
