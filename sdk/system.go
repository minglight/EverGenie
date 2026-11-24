package sdk

import (
	"context"
	"time"
)

// SystemAPI：整體健康檢查（evergenie doctor 與看板的健康狀態共用）。
type SystemAPI interface {
	// Doctor 依序跑各 group 的檢查，回一條一條白話的結果；只報告，不修復。
	Doctor(ctx context.Context, req *SystemDoctorRequest) (*SystemDoctorResponse, error)
	// Trust 列出權限的現況：每位秘書的等級、權限來源、最近被擋的動作；只讀設定（Snapshot 只寫 runtime/trust/）。
	Trust(ctx context.Context, req *SystemTrustRequest) (*SystemTrustResponse, error)
	// SetPermissionLevels 改權限等級（settings/trust.toml，先備份）；只有主人（by 是 owner(…)）可以。
	SetPermissionLevels(ctx context.Context, req *SystemSetPermissionLevelsRequest) (*SystemSetPermissionLevelsResponse, error)
	// GetConfig 讀 home.toml：key 空回整份文字，否則回那一個值（TOML 寫法）。只讀。
	GetConfig(ctx context.Context, req *SystemGetConfigRequest) (*SystemGetConfigResponse, error)
	// SetConfig 改 home.toml 的一個值：先備份到 runtime/backups/home/、寫完重讀驗證、不合格就還原；只有主人能改。
	SetConfig(ctx context.Context, req *SystemSetConfigRequest) (*SystemSetConfigResponse, error)
	// GetMode 讀介面模式（home.toml 的 mode：simple 或 full，D-9）；沒寫就是 simple。也回家的身分（env、標籤、電腦代號）。只讀。
	GetMode(ctx context.Context, req *SystemGetModeRequest) (*SystemGetModeResponse, error)
	// SetMode 改介面模式（跟 SetConfig 一樣先備份、原子寫入，改完不需重開）；只有主人能改。
	SetMode(ctx context.Context, req *SystemSetModeRequest) (*SystemSetModeResponse, error)
	// SetChatDisplay 改對話的顯示方式（home.toml 的 chat_display；跟 SetConfig 一樣先備份、原子寫入，改完不需重開）；只有主人能改。
	SetChatDisplay(ctx context.Context, req *SystemSetChatDisplayRequest) (*SystemSetChatDisplayResponse, error)
	// GetWorkMax 讀同時進行的工作對話上限（settings/sessions.toml 的 work_max；沒寫是預設 3）與看板可以改的範圍。只讀。
	GetWorkMax(ctx context.Context, req *SystemGetWorkMaxRequest) (*SystemGetWorkMaxResponse, error)
	// SetWorkMax 改工作對話上限（只動 work_max 那一行、先備份；1～10，改完不需重開）；只有主人能改。
	SetWorkMax(ctx context.Context, req *SystemSetWorkMaxRequest) (*SystemSetWorkMaxResponse, error)
	// Usage 回各帳號的用量與額度（evergenie quota）；只讀，資料由各帳號的引擎提供。
	Usage(ctx context.Context, req *SystemUsageRequest) (*SystemUsageResponse, error)
	// Preflight 問每個帳號的引擎：跑起來需要的東西齊不齊（evergenie preflight 的平台那一段）；只檢查，不安裝。
	Preflight(ctx context.Context, req *SystemPreflightRequest) (*SystemPreflightResponse, error)
	// Backups 列出 runtime/backups/ 底下的備份；prune=true 依保留規則標出會刪哪些（預演），apply=true 才真的刪（只有主人可以）。
	Backups(ctx context.Context, req *SystemBackupsRequest) (*SystemBackupsResponse, error)
	// BackupSweep 回上一次備份掃描的報告（每週的 backup-sweep 排程產生；只出報告、不刪，狀態資料夾 runtime/backups/ 照保留規則清的除外）；
	// run=true 先掃一次再回（主人叫的才會順便照保留規則清 runtime/backups/，秘書叫的只掃）。
	BackupSweep(ctx context.Context, req *SystemBackupSweepRequest) (*SystemBackupSweepResponse, error)
	Accounts(ctx context.Context, req *SystemAccountsRequest) (*SystemAccountsResponse, error)
	// SaveAccount 新增或更新一個 AI 帳號（先備份 home.toml、寫完驗證、不合格還原）；只有主人能做。
	SaveAccount(ctx context.Context, req *SystemSaveAccountRequest) (*SystemSaveAccountResponse, error)
	UpdateStatus(ctx context.Context, req *UpdateStatusRequest) (*UpdateInfo, error)
	// UpdateCheck 現在檢查一次有沒有新版（會連網抓 tag），結果存起來並回現況；只有主人能叫。
	UpdateCheck(ctx context.Context, req *UpdateCheckRequest) (*UpdateInfo, error)
	// UpdateApply 現在升級到檢查到的新版（version 要等於目前檢查到的目標，不然 INVALID）；升級在另一個行程跑，這裡立刻回；只有主人能叫。
	UpdateApply(ctx context.Context, req *UpdateApplyRequest) (*UpdateInfo, error)
	// UpdateDecide 對檢查到的新版做決定：now（現在更新）、tonight（今晚更新）、skip（跳過這版）；只有主人能叫。
	UpdateDecide(ctx context.Context, req *UpdateDecideRequest) (*UpdateInfo, error)
	// UpdateRollback 升級沒成功、也沒換回舊版時（UpdateInfo.Rescue）再試一次換回升級前的版本；在另一個行程跑，這裡立刻回；只有主人能叫。
	UpdateRollback(ctx context.Context, req *UpdateRollbackRequest) (*UpdateInfo, error)
	// Report 產生一份問題回報的內容（版本、系統版本、健檢結果、錯誤的標題與次數；detail 多帶遮過的日誌尾段）。只產生給主人看，不送出任何東西。
	Report(ctx context.Context, req *SystemReportRequest) (*SystemReportResponse, error)
}

type DoctorGroup string

const (
	DoctorGroupConfig      DoctorGroup = "config"
	DoctorGroupSessions    DoctorGroup = "sessions"
	DoctorGroupPackages    DoctorGroup = "packages"
	DoctorGroupPermissions DoctorGroup = "permissions"
	DoctorGroupSchedule    DoctorGroup = "schedule"
	DoctorGroupData        DoctorGroup = "data"
	DoctorGroupPush        DoctorGroup = "push"
)

type DoctorSeverity string

const (
	DoctorSeverityOk    DoctorSeverity = "ok"
	DoctorSeverityWarn  DoctorSeverity = "warn"
	DoctorSeverityError DoctorSeverity = "error"
)

// DoctorCheck 是一條檢查結果。每個 group 至少有一條；沒問題的 group 給一條 ok。
type DoctorCheck struct {
	ID       string         `json:"id,omitempty"` // 穩定的代號，例 data.invalid、permissions.drift
	Group    DoctorGroup    `json:"group,omitempty"`
	Ok       bool           `json:"ok"`
	Severity DoctorSeverity `json:"severity,omitempty"`
	Detail   string         `json:"detail,omitempty"` // 白話說發生什麼事
	Fix      string         `json:"fix,omitempty"`    // 白話說下一步怎麼做；沒有就留空
}

type SystemDoctorRequest struct {
	Groups []DoctorGroup `json:"groups,omitempty"` // 只跑這幾個 group；空表示全部
}

type SystemDoctorResponse struct {
	Checks []DoctorCheck `json:"checks,omitempty"`
}

// AgentPermission 是一位秘書的權限等級與它從哪裡來。
type AgentPermission struct {
	Agent  string          `json:"agent,omitempty"`
	Level  PermissionLevel `json:"level,omitempty"`  // 寫入時空字串表示拿掉這位的單獨設定
	Source string          `json:"source,omitempty"` // agent（單獨設定）／default_level（整個家的預設）／builtin（內建預設）
}

// PermissionSource 是權限來源的一列（evergenie trust scan）。
type PermissionSource struct {
	Scope  string `json:"scope,omitempty"`  // 例：秘書 code、repo、整個家
	Source string `json:"source,omitempty"` // 設定所在的檔案（相對家的路徑）
	Kind   string `json:"kind,omitempty"`   // level、repo、allow、deny、default_level
	Value  string `json:"value,omitempty"`
}

// DeniedAction 是一筆被核心擋下或改成問主人的動作（D-24 的授權判斷）。
type DeniedAction struct {
	At      time.Time     `json:"at,omitempty"`
	Session string        `json:"session,omitempty"`
	Agent   string        `json:"agent,omitempty"`  // 做動作的秘書
	From    string        `json:"from,omitempty"`   // 委派它的秘書
	Origin  string        `json:"origin,omitempty"` // 這一輪的來源（peer…）
	Class   ActionClass   `json:"class,omitempty"`
	Tool    string        `json:"tool,omitempty"`
	Verdict ActionVerdict `json:"verdict,omitempty"`
	Reason  string        `json:"reason,omitempty"`
}

type SystemTrustRequest struct {
	DeniedDays int32 `json:"denied_days,omitempty"` // 被擋紀錄看幾天；0 表示 7 天
	Snapshot   bool  `json:"snapshot,omitempty"`    // true：把現況存一份到 runtime/trust/snapshots/，回傳跟上一份的差異
}

type SystemTrustResponse struct {
	Policy       *PermissionPolicy  `json:"policy,omitempty"`
	DefaultLevel PermissionLevel    `json:"default_level,omitempty"` // 沒有單獨設定、也不在內建名單的秘書拿到的等級
	Agents       []AgentPermission  `json:"agents,omitempty"`        // 名冊上每位秘書＋trust.toml 寫到的
	Sources      []PermissionSource `json:"sources,omitempty"`
	Denied       []DeniedAction     `json:"denied,omitempty"` // 舊的在前
	SnapshotPath string             `json:"snapshot_path,omitempty"`
	Changes      []string           `json:"changes,omitempty"` // 跟上一份快照的差異，一條一句白話；沒有上一份或沒變是空的
}

type SystemSetPermissionLevelsRequest struct {
	DefaultLevel PermissionLevel   `json:"default_level,omitempty"` // 空表示不改
	Levels       []AgentPermission `json:"levels,omitempty"`        // 每位的單獨設定；Level 空字串＝拿掉
	By           *Actor            `json:"by,omitempty"`
}

type SystemSetPermissionLevelsResponse struct {
	Agents []AgentPermission `json:"agents,omitempty"`
	Backup string            `json:"backup,omitempty"` // 改之前的 trust.toml 備份（相對家的路徑）；原本沒有檔案時是空的
}

type SystemGetConfigRequest struct {
	Key string `json:"key,omitempty"` // 用點分隔，例 user.quiet_hours；空表示整份
}

type SystemGetConfigResponse struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"` // 那一個值的 TOML 寫法（字串帶引號、表格是多行）
	Text  string `json:"text,omitempty"`  // key 空時是整份 home.toml
}

type SystemSetConfigRequest struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"` // TOML 的值寫法（"文字"、123、true、[…]）；不是合法 TOML 值就當成文字
	By    *Actor `json:"by,omitempty"`
}

type SystemSetConfigResponse struct {
	Key    string `json:"key,omitempty"`
	Value  string `json:"value,omitempty"`  // 寫進去之後的值（TOML 寫法）
	Backup string `json:"backup,omitempty"` // 改之前的 home.toml 備份（相對家的路徑）
}

// HomeMode 是介面模式：simple 只顯示常用的設定，full 每一項都能細調並顯示進階項目（Goal）。
type HomeMode string

const (
	HomeModeSimple HomeMode = "simple"
	HomeModeFull   HomeMode = "full"
)

// ChatDisplay 是看板對話的顯示方式：default 只顯示「處理中」與三點動畫，engineer 也顯示正在用什麼工具、用了幾個。
type ChatDisplay string

const (
	ChatDisplayDefault  ChatDisplay = "default"
	ChatDisplayEngineer ChatDisplay = "engineer"
)

type SystemGetModeRequest struct {
}

type SystemGetModeResponse struct {
	Mode HomeMode `json:"mode,omitempty"`
	// 對話的顯示方式（home.toml chat_display）；舊版那台沒有這個欄位，看板當成 default。
	ChatDisplay ChatDisplay `json:"chat_display,omitempty"`
	// 家的身分（home.toml [runtime]，2026-11-01）：看板用來標出這是哪個家。
	Env      HomeEnv `json:"env,omitempty"`       // prod（正式家）或 dev（開發家）
	Label    string  `json:"label,omitempty"`     // <owner>-<machine>，開發家加 @dev（例 amy-home、amy-home@dev）
	Machine  string  `json:"machine,omitempty"`   // 電腦代號（例 home、office）
	UserName string  `json:"user_name,omitempty"` // home.toml user.name：看板的問候語與標題用
	// 看板連多台（S3）：別台的看板拿這三個值判斷能不能連、怎麼標。
	Version   string `json:"version,omitempty"`    // 目前執行的版本（跟 system.update_status 的 current 同來源）
	BoardAPI  int    `json:"board_api,omitempty"`  // 這台提供的看板介面等級（BoardAPILevel）；0＝S3 之前的版本
	PublicURL string `json:"public_url,omitempty"` // home.toml web.public_url（去掉結尾斜線）；沒寫是空
}

// BoardAPILevel 是看板跨台呼叫的介面等級：看板要的等級比那台的 board_api 高，就只在那台的區塊提示「版本太舊」。
// 只在看板需要的介面有不相容的增加時往上加（1＝S3 看板連多台）。
const BoardAPILevel = 1

// HomeEnv 是家的身分：prod＝正式家、dev＝開發家。主要拿來標記（看板、對話名、通知標題）；
// 依 env 改變行為的例外（清單的正本在 platform 卡 PLT-20）：AGT-35（秘書的共用規則）、PKG-56（看板設定頁列不列 dev_only 套件）、
// 官方套件 closet-mine 的 CMI-14（衣服「升成官方」只在開發家能做）、closet 的 CLO-27／CLO-29（開發家不遮沒兌換的衣服、標哪些可以升成官方）、
// CLO-32（開發家多能力 edit，寫在 closet.capabilities，頁面與核心的 show_when 只看能力）。
type HomeEnv string

const (
	HomeEnvProd HomeEnv = "prod"
	HomeEnvDev  HomeEnv = "dev"
)

type SystemSetModeRequest struct {
	Mode HomeMode `json:"mode,omitempty"` // simple 或 full
	By   *Actor   `json:"by,omitempty"`
}

type SystemSetModeResponse struct {
	Mode   HomeMode `json:"mode,omitempty"`
	Backup string   `json:"backup,omitempty"` // 改之前的 home.toml 備份（相對家的路徑）
}

type SystemSetChatDisplayRequest struct {
	ChatDisplay ChatDisplay `json:"chat_display,omitempty"` // default 或 engineer
	By          *Actor      `json:"by,omitempty"`
}

type SystemSetChatDisplayResponse struct {
	ChatDisplay ChatDisplay `json:"chat_display,omitempty"`
	Backup      string      `json:"backup,omitempty"` // 改之前的 home.toml 備份（相對家的路徑）
}

type SystemGetWorkMaxRequest struct{}

type SystemGetWorkMaxResponse struct {
	WorkMax int  `json:"work_max,omitempty"` // 現在生效的上限（沒寫或不合理就是 default）
	Set     bool `json:"set,omitempty"`      // 檔裡有寫一個合理的值
	Default int  `json:"default,omitempty"`  // 沒寫時的值（3）
	Min     int  `json:"min,omitempty"`      // 看板可以改的範圍（1～10）
	Max     int  `json:"max,omitempty"`
}

type SystemSetWorkMaxRequest struct {
	WorkMax int    `json:"work_max,omitempty"` // 1～10
	By      *Actor `json:"by,omitempty"`
}

type SystemSetWorkMaxResponse struct {
	WorkMax int    `json:"work_max,omitempty"`
	Backup  string `json:"backup,omitempty"` // 改之前的 settings/sessions.toml 備份（沒有原檔就是空）
}

// AccountUsage 是一個帳號的用量（evergenie quota）；資料由該帳號的引擎提供。
type AccountUsage struct {
	Account    string        `json:"account,omitempty"`
	UsedRatio  float64       `json:"used_ratio"` // 0–1，各視窗取最高
	ResetsAt   time.Time     `json:"resets_at,omitempty"`
	Windows    []UsageWindow `json:"windows,omitempty"`
	MeasuredAt time.Time     `json:"measured_at,omitempty"` // 零值表示沒有資料
	Note       string        `json:"note,omitempty"`        // 沒有資料或引擎沒在跑時的原因
}

type SystemUsageRequest struct {
	Account string `json:"account,omitempty"` // 空表示全部帳號
}

type SystemUsageResponse struct {
	Accounts []AccountUsage `json:"accounts,omitempty"`
}

type SystemPreflightRequest struct{}

type SystemPreflightResponse struct {
	Checks []PreflightCheck `json:"checks,omitempty"` // 各帳號引擎的檢查，同一個 id 只列一次
}

// BackupFile 是 runtime/backups/ 底下的一個備份（檔案或資料夾）。
type BackupFile struct {
	Path    string    `json:"path,omitempty"`  // 相對家
	Group   string    `json:"group,omitempty"` // 第一層資料夾名（trust、home、…）；直接放在最上層的是「（最上層）」
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time,omitempty"`
	Prune   bool      `json:"prune,omitempty"` // 依保留規則會（或已經）刪掉
}

type SystemBackupsRequest struct {
	Prune bool   `json:"prune,omitempty"` // true：依保留規則標出要刪的（預演）
	Apply bool   `json:"apply,omitempty"` // 搭配 prune：真的刪；只有主人可以
	Keep  int32  `json:"keep,omitempty"`  // 每一組至少保留最新幾個；0 表示 10
	Days  int32  `json:"days,omitempty"`  // 幾天內的都保留；0 表示 30
	By    *Actor `json:"by,omitempty"`
}

type SystemBackupsResponse struct {
	Files   []BackupFile `json:"files,omitempty"`
	Total   int64        `json:"total"`   // 所有備份的大小（位元組）
	Pruned  int32        `json:"pruned"`  // 標出（或已刪）幾個
	Freed   int64        `json:"freed"`   // 標出（或已刪）的大小
	Applied bool         `json:"applied"` // 真的刪了
}

// 備份掃描的項目狀態（BackupSweepItem.State）。
const (
	BackupSweepExpired   = "expired"   // 已到期、查過沒有東西用到它：可以刪（主人或電腦秘書看過再刪）
	BackupSweepInUse     = "in_use"    // 已到期，但有開機服務、執行中的程式或終端機視窗用到這個路徑或它的上層資料夾：在用，不能刪
	BackupSweepUnchecked = "unchecked" // 已到期，但有一種檢查沒做成：不知道有沒有在用，不能刪
	BackupSweepMisnamed  = "misnamed"  // 名字不合規（沒日期、沒原因、格式錯）
	BackupSweepSkipped   = "skipped"   // 例外：不算備份、不列
)

// BackupSweepItem 是備份掃描找到的一個檔案或資料夾。
type BackupSweepItem struct {
	Path    string    `json:"path,omitempty"`    // 位置（使用者家目錄底下的寫成 ~/…）
	Dir     bool      `json:"dir,omitempty"`     // 是資料夾
	Size    int64     `json:"size"`              // 位元組（資料夾是裡面全部加起來）
	Date    string    `json:"date,omitempty"`    // 名字裡的日期（YYYY-MM-DD）；名字沒有就是最後修改的日期
	Expires string    `json:"expires,omitempty"` // 合規的：留到哪一天（YYYY-MM-DD）
	ModTime time.Time `json:"mod_time"`          // 最後修改時間
	State   string    `json:"state,omitempty"`   // BackupSweep* 其中一個
	Checked string    `json:"checked,omitempty"` // 已到期的：查了哪幾種、結果（例「查過：沒有開機服務、執行中的程式、終端機視窗用到它」）
	UsedBy  string    `json:"used_by,omitempty"` // 在用：哪一種檢查、誰指到
	Problem string    `json:"problem,omitempty"` // 不合規：哪裡不對
	Suggest string    `json:"suggest,omitempty"` // 不合規：建議怎麼做
	Reason  string    `json:"reason,omitempty"`  // 例外：照哪一條略過
}

// BackupSweepReport 是一次備份掃描的結果。
type BackupSweepReport struct {
	At           time.Time         `json:"at"`                 // 開始掃的時間
	TookMs       int64             `json:"took_ms"`            // 花了多久
	By           string            `json:"by,omitempty"`       // 誰叫的（排程是 system）
	Roots        []string          `json:"roots,omitempty"`    // 掃了哪幾處
	Checks       []string          `json:"checks,omitempty"`   // 「在用」查了哪幾種（給主人看的白話）
	Expired      []BackupSweepItem `json:"expired,omitempty"`  // 已到期（可刪、在用、沒查完）
	Misnamed     []BackupSweepItem `json:"misnamed,omitempty"` // 名字不合規
	Current      int32             `json:"current"`            // 期限內的數量
	CurrentBytes int64             `json:"current_bytes"`      // 期限內的大小
	Skipped      []BackupSweepItem `json:"skipped,omitempty"`  // 略過的例外
	Pruned       int32             `json:"pruned"`             // 這次照保留規則清掉的 runtime/backups/ 舊備份數
	PrunedBytes  int64             `json:"pruned_bytes"`       // 清掉的大小
	Notes        []string          `json:"notes,omitempty"`    // 不影響結果的提醒（例：哪個資料夾的 git 讀不到）
	Errors       []string          `json:"errors,omitempty"`   // 出錯的地方（有的話也會發系統通知）
	File         string            `json:"file,omitempty"`     // Markdown 報告檔的位置
}

type SystemBackupSweepRequest struct {
	Run bool   `json:"run,omitempty"` // true：現在掃一次再回
	By  *Actor `json:"by,omitempty"`
}

type SystemBackupSweepResponse struct {
	Report  *BackupSweepReport `json:"report,omitempty"` // 還沒掃過是空的
	Running bool               `json:"running"`          // 另一次掃描還在跑（run=true 時不會再開一次）
}

// UpdateState 是升級進行到哪裡（<家>/runtime/upgrade/state.json）。Phase：fetch、backup、build、migrate、restart、verify（進行中），
// 最後是 done（成功）、rolled_back（沒成功、已換回舊版）或 failed（沒成功）。沒成功時舊版是不是照常在跑看 Outcome，不看 Phase
// （failed 也可能是「換版之前就停了、舊版照常在跑」）；Outcome 是空的（這個欄位之前的紀錄）才照 Phase 猜。
type UpdateState struct {
	Phase      string    `json:"phase,omitempty"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	Reason     string    `json:"reason,omitempty"` // 沒成功時的白話原因（不夾英文；英文細節只在升級日誌）
	OK         bool      `json:"ok"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
	// Kind 是哪一種動作：upgrade（升級，空的也是）、rollback（手動退回上一版）、rescue（換不回去之後再試一次換回）。
	Kind string `json:"kind,omitempty"`
	// Outcome 是沒成功時的結果：untouched（換版之前就停了，舊版照常在跑）、rolled_back（已換回舊版）、stranded（也沒換回，要處理）。
	Outcome string `json:"outcome,omitempty"`
	// Restored：換回時資料也用升級前的備份還原了（整理過資料才會）。
	Restored bool `json:"restored,omitempty"`
	// Next 是沒成功時給主人的一個下一步（白話一句）。
	Next string `json:"next,omitempty"`
	// Log 是英文細節寫在哪個日誌（給秘書查）。
	Log string `json:"log,omitempty"`
}

// UpdateState.Kind 與 UpdateState.Outcome 的值。
const (
	UpdateKindUpgrade  = "upgrade"
	UpdateKindRollback = "rollback"
	UpdateKindRescue   = "rescue"

	UpdateOutcomeUntouched  = "untouched"
	UpdateOutcomeRolledBack = "rolled_back"
	UpdateOutcomeStranded   = "stranded"
)

// UpdateInfo 是「版本與更新」的現況。Target 空＝沒有新版（Detail 說原因，例如「這版你跳過了」）。
type UpdateInfo struct {
	Current    string       `json:"current,omitempty"` // 現在跑的核心版本
	Target     string       `json:"target,omitempty"`  // 有新版時的版本
	Tag        string       `json:"tag,omitempty"`     // 新版的 git tag
	Notes      string       `json:"notes,omitempty"`   // 新版的發版說明（Markdown）
	Channel    string       `json:"channel,omitempty"` // release（只升正式版）、safe（也升測試過的）、off（不自動檢查）
	Detail     string       `json:"detail,omitempty"`  // 白話：沒有新版的原因、抓不到的原因
	CoreFound  bool         `json:"core_found"`        // false＝這台不是用安裝程式裝的，沒辦法自動升級
	Skipped    []string     `json:"skipped,omitempty"`
	Decision   string       `json:"decision,omitempty"` // 目前的決定：tonight（今晚更新）；沒有是空的
	DecisionAt time.Time    `json:"decision_at,omitempty"`
	State      *UpdateState `json:"state,omitempty"`
	CheckedAt  time.Time    `json:"checked_at,omitempty"`
	TonightAt  string       `json:"tonight_at,omitempty"` // 「今晚更新」幾點動手（HH:MM）
	// Rescue：上一次升級沒成功、也沒換回（換不回去），可以用 update_rollback 再試一次換回升級前的版本。
	Rescue bool `json:"rescue,omitempty"`
}

type UpdateStatusRequest struct{}

type UpdateCheckRequest struct {
	By *Actor `json:"by,omitempty"`
}

type UpdateApplyRequest struct {
	Version string `json:"version,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type UpdateDecideRequest struct {
	Version  string `json:"version,omitempty"`
	Decision string `json:"decision,omitempty"` // now、tonight、skip
	By       *Actor `json:"by,omitempty"`
}

type UpdateRollbackRequest struct {
	By *Actor `json:"by,omitempty"`
}

// Account 是 home.toml 的一個 AI 帳號。
type Account struct {
	Name         string `json:"name,omitempty"`
	Engine       string `json:"engine,omitempty"`
	ConfigDir    string `json:"config_dir,omitempty"` // 這個帳號的登入資料夾（例 ~/.acct-work）
	EnvFile      string `json:"env_file,omitempty"`   // 啟動前要載入的環境檔（可空）
	RemoteAccess bool   `json:"remote_access"`
	UsageWarmup  bool   `json:"usage_warmup"`
	Default      bool   `json:"default"`
}

type SystemAccountsRequest struct{}

type SystemAccountsResponse struct {
	Accounts []Account `json:"accounts,omitempty"`
	Engines  []string  `json:"engines,omitempty"` // 可以選的引擎 id（看板新增帳號的選單）
}

type SystemSaveAccountRequest struct {
	Account     Account `json:"account"`
	MakeDefault bool    `json:"make_default"`
	By          *Actor  `json:"by,omitempty"`
}

type SystemSaveAccountResponse struct {
	Backup string `json:"backup,omitempty"` // 改之前的 home.toml 備份（相對家的路徑）
}

type SystemReportRequest struct {
	Detail bool `json:"detail,omitempty"` // true：多帶遮過的日誌尾段（只留狀況欄位）
}

// SystemReportResponse 是一份回報包：已盡量拿掉私人內容（訊息內文、帳號名、電腦名、家目錄路徑…），送出前主人自己再看一遍。
type SystemReportResponse struct {
	Title     string `json:"title,omitempty"`
	Body      string `json:"body,omitempty"`       // 給人看的 Markdown
	IssueLink string `json:"issue_link,omitempty"` // 帶好標題與內文的回報頁網址；還沒設定回報頁就是空的（看板只給「複製內容」）
}
