package sdk

import (
	"context"
	"time"
)

// BackupAPI：整個家的備份與還原（DEV-153，system 塊的 homebackup；指令 `evergenie backup create|status|show|restore`
// 與看板「設定 → 這台電腦 → 備份」共用）。跟 system.backups（改設定前留的小備份）是兩回事。
type BackupAPI interface {
	// GetStatus 回備份的現況：上次備份的時間、自動備份有沒有失敗、現有的備份、上一次還原的結果。只讀。
	GetStatus(ctx context.Context, req *BackupGetStatusRequest) (*BackupGetStatusResponse, error)
	// Create 立刻做一份備份（一個 .egbackup 檔，只放在這台電腦：法律第 7 條，EverGenie 不做跨台備份）；誰都可以叫。
	Create(ctx context.Context, req *BackupCreateRequest) (*BackupCreateResponse, error)
	// Preview 打開一份備份檔看裡面有什麼（整份讀過、驗過檢查碼）；不改任何東西。
	Preview(ctx context.Context, req *BackupPreviewRequest) (*BackupPreviewResponse, error)
	// Restore 用一份備份把整個家換掉：先留一份現況、把備份解開放在旁邊、驗過，然後重開服務，在重開的當下換上去
	// （換不成就維持原樣）。這裡回的時候還沒換完，結果看 GetStatus 的 last_restore。只有主人能叫。
	Restore(ctx context.Context, req *BackupRestoreRequest) (*BackupRestoreResponse, error)
}

// HomeBackupUploadMaxBytes 是看板上傳一個備份檔的上限（2 GB，跟角色匯出檔同一個數字）；更大的用指令給檔案路徑還原。
const HomeBackupUploadMaxBytes int64 = 2 << 30

// 備份的種類（也是放備份的資料夾名）。
const (
	HomeBackupKindDaily         = "daily"          // 以前每天自動的（2026-11-21 起不做，留下的會清掉）
	HomeBackupKindManual        = "manual"         // 主人或秘書按「立刻備份」做的
	HomeBackupKindBeforeRestore = "before-restore" // 還原之前自動留的現況
)

// HomeBackupFile 是這台電腦上的一份備份檔。
type HomeBackupFile struct {
	Name      string    `json:"name,omitempty"`     // 檔名（<家名>-<日期>-<時間>.egbackup）
	Kind      string    `json:"kind,omitempty"`     // daily、manual、before-restore
	Size      int64     `json:"size"`               // 位元組
	CreatedAt time.Time `json:"created_at"`         // 做的時間
	Download  string    `json:"download,omitempty"` // 看板下載用的網址（/home-backup/<種類>/<檔名>）
}

// HomeBackupSection 是備份檔裡的一類東西。
type HomeBackupSection struct {
	ID    string `json:"id,omitempty"`    // settings、vault、personas、packages、media、secrets
	Label string `json:"label,omitempty"` // 白話名稱
	Files int32  `json:"files"`
	Bytes int64  `json:"bytes"`
}

// HomeRestoreResult 是上一次還原（或從備份建家）的結果。
type HomeRestoreResult struct {
	OK               bool      `json:"ok"`
	At               time.Time `json:"at"`                           // 換上去（或放棄）的時間
	BackupCreatedAt  time.Time `json:"backup_created_at"`            // 用的那份備份是什麼時候做的
	BackupHome       string    `json:"backup_home,omitempty"`        // 那份備份的家名
	Message          string    `json:"message,omitempty"`            // 白話結果
	Safety           string    `json:"safety,omitempty"`             // 還原前自動留的那份現況（檔名）
	Todo             []string  `json:"todo,omitempty"`               // 換了電腦之後還要主人做的事（白話，一項一句）
	FromOtherMachine bool      `json:"from_other_machine,omitempty"` // 備份是別台電腦做的
}

type BackupGetStatusRequest struct {
	// Local：這個請求是不是從這台 Mac 本機開的看板來的。看板伺服器照請求算、用戶端帶的不算（board WEB-190）；回應原樣帶回去。
	Local bool `json:"local,omitempty"`
}

type BackupGetStatusResponse struct {
	Files       []HomeBackupFile   `json:"files,omitempty"`        // 新的在前
	LastAt      time.Time          `json:"last_at"`                // 最新一份備份的時間（零值＝還沒有）
	LastKind    string             `json:"last_kind,omitempty"`    // 最新那份的種類
	AutoError   string             `json:"auto_error,omitempty"`   // 已不用：每天自動備份拿掉了（2026-11-21），固定空的
	AutoErrorAt time.Time          `json:"auto_error_at"`          // 那次是什麼時候
	Restoring   bool               `json:"restoring"`              // 有一份還原在等重開
	LastRestore *HomeRestoreResult `json:"last_restore,omitempty"` // 上一次還原的結果
	KeepCount   int32              `json:"keep_count"`             // 手動做的、還原前留的：最新幾份一定留
	KeepDays    int32              `json:"keep_days"`              // 手動做的、還原前留的：其餘留幾天
	DailyKeep   int32              `json:"daily_keep"`             // 已不用：每天自動備份拿掉了（2026-11-21），固定 0
	FreeBytes   int64              `json:"free_bytes"`             // 放備份的那顆磁碟還剩多少（0＝不知道）
	Local       bool               `json:"local"`                  // 這台 Mac 本機開的看板（是才能上傳備份檔來還原）
}

type BackupCreateRequest struct {
	By *Actor `json:"by,omitempty"`
}

type BackupCreateResponse struct {
	File     HomeBackupFile      `json:"file"`
	Sections []HomeBackupSection `json:"sections,omitempty"`
	Skipped  []string            `json:"skipped,omitempty"` // 沒收進去的（捷徑類的檔），家裡的相對路徑
}

// BackupPreviewRequest 指出要看哪一份：upload（看板上傳的暫存檔名）、kind＋name（這台現有的一份）、path（這台電腦上的檔案路徑，指令用）三選一。
type BackupPreviewRequest struct {
	Upload string `json:"upload,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
}

type BackupPreviewResponse struct {
	CreatedAt    time.Time           `json:"created_at"`
	HomeName     string              `json:"home_name,omitempty"`
	Owner        string              `json:"owner,omitempty"`   // 主人的稱呼
	Machine      string              `json:"machine,omitempty"` // 哪台電腦做的
	AppVersion   string              `json:"app_version,omitempty"`
	Kind         string              `json:"kind,omitempty"`
	Size         int64               `json:"size"`  // 備份檔的大小
	Files        int32               `json:"files"` // 裡面幾個檔
	Bytes        int64               `json:"bytes"` // 解開後的大小
	Sections     []HomeBackupSection `json:"sections,omitempty"`
	OtherMachine bool                `json:"other_machine"`       // 不是這台電腦（或這個位置的家）做的
	Problem      string              `json:"problem,omitempty"`   // 不能還原的原因（例：備份比程式新）；空＝可以還原
	Installed    []string            `json:"installed,omitempty"` // 做備份時裝著的、別人分享的套件（備份不含，還原後要重裝）
	Skipped      []string            `json:"skipped,omitempty"`   // 做備份時沒收進去的（捷徑類的檔）
	// LostLinks 是這個家現在有、還原之後會不見的捷徑（符號連結；家裡的相對路徑）：還原整塊換掉、備份不收捷徑，要主人自己重建。
	LostLinks []string `json:"lost_links,omitempty"`
}

type BackupRestoreRequest struct {
	Upload string `json:"upload,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name,omitempty"`
	Path   string `json:"path,omitempty"`
	By     *Actor `json:"by,omitempty"`
}

type BackupRestoreResponse struct {
	Restarting bool   `json:"restarting"`       // 服務正要重開，重開時換上去
	Safety     string `json:"safety,omitempty"` // 還原前留的那份現況（檔名）
}
