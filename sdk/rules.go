package sdk

import (
	"context"
	"time"
)

// RulesAPI 是規矩頁（D-44）：家的資料夾裡的規矩文件（憲法、宗旨、法律…，哪些檔與位階寫在 home.toml 的 [rules]）。
// 誰都可以讀；只有主人能改（存檔、還原、採用提案），每次改動前後都留版本。秘書只能送修改提案，等主人採用或退回。
type RulesAPI interface {
	// List 列出設定的規矩文件（照位階，最高的在前）；設定有問題時 Docs 是空的、Problem 寫原因。
	List(ctx context.Context, req *RulesListRequest) (*RulesListResponse, error)
	// Get 讀一份文件的全文（檔案或那一段不存在時 Exists=false、Text 空）。
	Get(ctx context.Context, req *RulesGetRequest) (*RulesGetResponse, error)
	// Save 主人改一份文件：Revision 要是讀到的那一版，文件在那之後改過就回 CONFLICT。
	Save(ctx context.Context, req *RulesSaveRequest) (*RulesSaveResponse, error)
	// History 列出一份文件的版本（新的在前，不含全文）。
	History(ctx context.Context, req *RulesHistoryRequest) (*RulesHistoryResponse, error)
	// Version 讀一個版本的全文，和跟另一版（預設前一版）比較的差異。
	Version(ctx context.Context, req *RulesVersionRequest) (*RulesVersionResponse, error)
	// Restore 主人把文件換回某一版（會再留一個新版本，原本的內容也留著）。
	Restore(ctx context.Context, req *RulesRestoreRequest) (*RulesRestoreResponse, error)
	// Propose 秘書送一個修改提案（新全文，或文件裡某一段的新內容＋理由）；不改文件。
	Propose(ctx context.Context, req *RulesProposeRequest) (*RulesProposeResponse, error)
	// Proposals 列出提案（新的在前）；待決的附上「採用後會怎麼改」的差異。
	Proposals(ctx context.Context, req *RulesProposalsRequest) (*RulesProposalsResponse, error)
	// Decide 主人採用（套上去並留版本）或退回一個待決的提案。
	Decide(ctx context.Context, req *RulesDecideRequest) (*RulesDecideResponse, error)
}

// RulesDoc 是一份規矩文件（home.toml [rules] 的一筆）。
type RulesDoc struct {
	ID       string `json:"id,omitempty"`
	Title    string `json:"title,omitempty"`
	Rank     int    `json:"rank,omitempty"`    // 位階：1 最高，照設定的順序
	Path     string `json:"path,omitempty"`    // 設定寫的路徑（相對家的資料夾，或 ~／絕對路徑）
	Section  string `json:"section,omitempty"` // 只取檔案裡這個標題底下的一段；空＝整個檔
	Exists   bool   `json:"exists"`
	Revision string `json:"revision,omitempty"` // 內容雜湊；存檔時帶回來。不存在時是空的
	Pending  int    `json:"pending,omitempty"`  // 待決的修改提案數
	Problem  string `json:"problem,omitempty"`  // 這一份讀不到的原因（list 用；其他份照常列出）
}

type RulesVersionSource string

const (
	RulesVersionSourceSave     RulesVersionSource = "save"     // 主人在看板存檔
	RulesVersionSourceRestore  RulesVersionSource = "restore"  // 主人還原成某一版
	RulesVersionSourceProposal RulesVersionSource = "proposal" // 主人採用秘書的提案
	RulesVersionSourceOutside  RulesVersionSource = "outside"  // 改之前的樣子：第一次經看板改、或在看板外（編輯器、git）改過
)

// RulesVersion 是文件某個時間點的全文（RV-<n>）。
type RulesVersion struct {
	ID           string             `json:"id,omitempty"`
	Doc          string             `json:"doc,omitempty"`
	Title        string             `json:"title,omitempty"` // 當時文件的標題
	Source       RulesVersionSource `json:"source,omitempty"`
	Note         string             `json:"note,omitempty"` // 改了什麼（主人寫的）
	By           string             `json:"by,omitempty"`
	Proposal     string             `json:"proposal,omitempty"`      // 採用的提案 RP-<n>
	RestoredFrom string             `json:"restored_from,omitempty"` // 還原自哪一版
	CreatedAt    time.Time          `json:"created_at,omitempty"`
	Text         string             `json:"text,omitempty"` // 只有 Version 會帶
}

type RulesDiffOp string

const (
	RulesDiffOpSame RulesDiffOp = "same"
	RulesDiffOpAdd  RulesDiffOp = "add"
	RulesDiffOpDel  RulesDiffOp = "del"
)

// RulesDiffLine 是逐行比較的一行。
type RulesDiffLine struct {
	Op   RulesDiffOp `json:"op,omitempty"`
	Text string      `json:"text"`
}

type RulesProposalStatus string

const (
	RulesProposalStatusPending  RulesProposalStatus = "pending"
	RulesProposalStatusAccepted RulesProposalStatus = "accepted"
	RulesProposalStatusRejected RulesProposalStatus = "rejected"
)

// RulesProposal 是秘書的修改提案（RP-<n>）。
type RulesProposal struct {
	ID        string              `json:"id,omitempty"`
	Doc       string              `json:"doc,omitempty"`
	Section   string              `json:"section,omitempty"` // 改文件裡的哪一段（標題）；空＝整份換掉
	From      string              `json:"from,omitempty"`
	Why       string              `json:"why,omitempty"`
	Text      string              `json:"text,omitempty"` // 新的全文，或那一段的新內容
	Status    RulesProposalStatus `json:"status,omitempty"`
	Base      string              `json:"base,omitempty"`  // 提案時文件的 Revision
	Stale     bool                `json:"stale,omitempty"` // 提案之後文件改過了：不能採用，要退回請秘書照新版重送
	CreatedAt time.Time           `json:"created_at,omitempty"`
	DecidedAt time.Time           `json:"decided_at,omitempty"`
	DecidedBy string              `json:"decided_by,omitempty"`
	Note      string              `json:"note,omitempty"`    // 主人採用或退回時寫的
	Notice    string              `json:"notice,omitempty"`  // 給主人的通知 M-<n>
	Version   string              `json:"version,omitempty"` // 採用後留下的版本 RV-<n>
	Diff      []RulesDiffLine     `json:"diff,omitempty"`    // 只有待決的：現在的文件 → 採用後
}

type RulesListRequest struct{}

type RulesListResponse struct {
	Docs    []RulesDoc `json:"docs,omitempty"`
	Problem string     `json:"problem,omitempty"`
}

type RulesGetRequest struct {
	Doc string `json:"doc,omitempty"`
}

type RulesGetResponse struct {
	Doc  *RulesDoc `json:"doc,omitempty"`
	Text string    `json:"text,omitempty"`
}

type RulesSaveRequest struct {
	Doc      string `json:"doc,omitempty"`
	Text     string `json:"text,omitempty"`
	Revision string `json:"revision,omitempty"` // 讀到的那一版；文件原本不存在時是空的
	Note     string `json:"note,omitempty"`
	By       *Actor `json:"by,omitempty"`
}

type RulesSaveResponse struct {
	Doc     *RulesDoc     `json:"doc,omitempty"`
	Version *RulesVersion `json:"version,omitempty"` // 內容沒變時是空的
}

type RulesHistoryRequest struct {
	Doc string `json:"doc,omitempty"`
}

type RulesHistoryResponse struct {
	Versions []RulesVersion `json:"versions,omitempty"`
}

type RulesVersionRequest struct {
	ID      string `json:"id,omitempty"`
	Against string `json:"against,omitempty"` // 跟哪一版比：空＝前一版、current＝現在的文件、RV-<n>
}

type RulesVersionResponse struct {
	Version *RulesVersion   `json:"version,omitempty"`
	Against string          `json:"against,omitempty"` // 實際比的那一版（RV-<n>、current，或空＝這是第一版）
	Diff    []RulesDiffLine `json:"diff,omitempty"`    // Against → 這一版
}

type RulesRestoreRequest struct {
	ID       string `json:"id,omitempty"`
	Revision string `json:"revision,omitempty"` // 讀到的現在那一版；有帶就檢查
	Note     string `json:"note,omitempty"`
	By       *Actor `json:"by,omitempty"`
}

type RulesRestoreResponse struct {
	Doc     *RulesDoc     `json:"doc,omitempty"`
	Version *RulesVersion `json:"version,omitempty"`
}

type RulesProposeRequest struct {
	Doc     string `json:"doc,omitempty"`
	Section string `json:"section,omitempty"`
	Text    string `json:"text,omitempty"`
	Why     string `json:"why,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type RulesProposeResponse struct {
	Proposal *RulesProposal `json:"proposal,omitempty"`
}

type RulesProposalsRequest struct {
	Doc      string                `json:"doc,omitempty"`
	Statuses []RulesProposalStatus `json:"statuses,omitempty"` // 空＝全部
}

type RulesProposalsResponse struct {
	Proposals []RulesProposal `json:"proposals,omitempty"`
}

type RulesDecideRequest struct {
	ID     string `json:"id,omitempty"`
	Accept bool   `json:"accept"`
	Note   string `json:"note,omitempty"`
	By     *Actor `json:"by,omitempty"`
}

type RulesDecideResponse struct {
	Proposal *RulesProposal `json:"proposal,omitempty"`
	Version  *RulesVersion  `json:"version,omitempty"` // 採用時留下的版本
}
