package sdk

import (
	"context"
	"time"
)

type VaultAPI interface {
	// Get 讀一筆。id 找不到回 ERROR_CODE_NOT_FOUND；已封存的也找得到（連結以 id 解析）。
	Get(ctx context.Context, req *VaultGetRequest) (*VaultGetResponse, error)
	// List 依種類列出，可用欄位過濾。
	List(ctx context.Context, req *VaultListRequest) (*VaultListResponse, error)
	// Put 建立或整筆取代。寫入時依 schema 驗證（REQ-4）；expected_revision 不符回 ERROR_CODE_CONFLICT。
	Put(ctx context.Context, req *VaultPutRequest) (*VaultPutResponse, error)
	// Patch 只改指定的 frontmatter 欄位，內文不動。
	Patch(ctx context.Context, req *VaultPatchRequest) (*VaultPatchResponse, error)
	// Append 在內文尾端追加一行（journal 唯一的寫法，沒有覆寫）。
	Append(ctx context.Context, req *VaultAppendRequest) (*VaultAppendResponse, error)
	// Archive 把一筆搬進 archive/（只封存，不刪除，REQ-5）。
	Archive(ctx context.Context, req *VaultArchiveRequest) (*VaultArchiveResponse, error)
	// NextId 取得下一個識別碼（原子遞增計數器）。
	NextId(ctx context.Context, req *VaultNextIdRequest) (*VaultNextIdResponse, error)
	// Changes 讀變更紀錄（每次寫入追加的那一行，VLT-8）：新到舊、壞行略過、可篩選與分頁；保留最近 30 天。
	Changes(ctx context.Context, req *VaultChangesRequest) (*VaultChangesResponse, error)
	// Watch 訂閱變更（供看板即時更新、scheduler 重算到期）。
	Watch(ctx context.Context, req *VaultWatchRequest, send func(*VaultWatchResponse) error) error
}

// Record 是一筆資料：frontmatter 加內文。kind 例：task、reminder、goal、habit、memory、
// journal、message、session、agent、job、persona。
type Record struct {
	Kind      string         `json:"kind,omitempty"`
	ID        string         `json:"id,omitempty"`       // 檔名等於 id；memory／journal 用路徑當 id（見 schemas）
	Fields    map[string]any `json:"fields,omitempty"`   // YAML frontmatter
	Body      string         `json:"body,omitempty"`     // Markdown 內文
	Revision  string         `json:"revision,omitempty"` // 內容雜湊，樂觀並行控制用
	Archived  bool           `json:"archived"`
	UpdatedAt time.Time      `json:"updated_at,omitempty"`
}

type VaultGetRequest struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id,omitempty"`
}

type VaultGetResponse struct {
	Record *Record `json:"record,omitempty"`
}

type VaultListRequest struct {
	Kind string `json:"kind,omitempty"`
	// 欄位過濾，欄位名對應 frontmatter；值是精確比對，列表欄位是「包含」。
	Where           map[string]string `json:"where,omitempty"`
	IncludeArchived bool              `json:"include_archived"`
	Page            *PageRequest      `json:"page,omitempty"`
}

type VaultListResponse struct {
	Records []Record      `json:"records,omitempty"`
	Page    *PageResponse `json:"page,omitempty"`
}

type VaultPutRequest struct {
	Record           *Record `json:"record,omitempty"`
	By               *Actor  `json:"by,omitempty"`
	ExpectedRevision string  `json:"expected_revision,omitempty"` // 空字串表示「必須是新的」
}

type VaultPutResponse struct {
	Record *Record `json:"record,omitempty"`
}

type VaultPatchRequest struct {
	Kind             string         `json:"kind,omitempty"`
	ID               string         `json:"id,omitempty"`
	Set              map[string]any `json:"set,omitempty"`   // 要改的欄位
	Unset            []string       `json:"unset,omitempty"` // 要拿掉的欄位
	By               *Actor         `json:"by,omitempty"`
	ExpectedRevision string         `json:"expected_revision,omitempty"`
}

type VaultPatchResponse struct {
	Record *Record `json:"record,omitempty"`
}

type VaultAppendRequest struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id,omitempty"`
	Line string `json:"line,omitempty"`
	By   *Actor `json:"by,omitempty"`
}

type VaultAppendResponse struct {
	Revision string `json:"revision,omitempty"`
}

type VaultArchiveRequest struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id,omitempty"`
	By   *Actor `json:"by,omitempty"`
}

type VaultArchiveResponse struct {
}

type VaultNextIdRequest struct {
	Kind string `json:"kind,omitempty"`
}

type VaultNextIdResponse struct {
	ID string `json:"id,omitempty"` // 例 SEC-319
}

// ChangeEntry 是變更紀錄的一行：誰、什麼時候、對哪一筆做了什麼。
type ChangeEntry struct {
	At   time.Time `json:"at,omitempty"`
	By   string    `json:"by,omitempty"`   // owner(web)、owner(cli)、秘書的職位 id、system
	Kind string    `json:"kind,omitempty"` // 資料種類，例 task、reminder、goal、nutrition.meal
	ID   string    `json:"id,omitempty"`
	Op   string    `json:"op,omitempty"` // create（新增）、put（整筆更新）、patch、append、archive、move；舊的紀錄新增也寫 put
}

type VaultChangesRequest struct {
	Since  string `json:"since,omitempty"`  // YYYY-MM-DD（含），依家的時區
	Until  string `json:"until,omitempty"`  // YYYY-MM-DD（含）
	By     string `json:"by,omitempty"`     // 誰；"owner" 表示任何 owner(…)（主人），其他要完全相同
	Kind   string `json:"kind,omitempty"`   // 資料種類
	Limit  int32  `json:"limit,omitempty"`  // 預設 100，最多 500
	Before string `json:"before,omitempty"` // 上一頁回的 next_before
}

type VaultChangesResponse struct {
	Items      []ChangeEntry `json:"items,omitempty"`
	NextBefore string        `json:"next_before,omitempty"` // 空：沒有更早的
}

type VaultWatchRequest struct {
	Kinds []string `json:"kinds,omitempty"` // 空表示全部
}

type ChangeType string

const (
	ChangeTypeCreated  ChangeType = "created"
	ChangeTypeUpdated  ChangeType = "updated"
	ChangeTypeArchived ChangeType = "archived"
)

type VaultWatchResponse struct {
	Type   ChangeType `json:"type,omitempty"`
	Record *Record    `json:"record,omitempty"`
	By     *Actor     `json:"by,omitempty"`
	At     time.Time  `json:"at,omitempty"`
}
