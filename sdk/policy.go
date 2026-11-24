package sdk

import (
	"context"
	"time"
)

// PolicyAPI 是權限變更申請（Q7，D-26）：秘書被擋住時「提出」申請，主人在看板或通知上審，核心套用；秘書自己不能套用、不能審。
// 這跟 D-7 的 RequestApproval 不同：RequestApproval 管「這一次動作要不要放行」，這裡管「永久改規則」。
type PolicyAPI interface {
	// Request 送一張申請（秘書用）。只寫申請，不改任何設定；why 必填。
	Request(ctx context.Context, req *PolicyRequestRequest) (*PolicyRequestResponse, error)
	// List 列出申請（新的在前）；statuses 空表示全部。
	List(ctx context.Context, req *PolicyListRequest) (*PolicyListResponse, error)
	// Get 讀一張申請（含影響預覽與預覽指紋）。
	Get(ctx context.Context, req *PolicyGetRequest) (*PolicyGetResponse, error)
	// Decide 核准或拒絕（只有主人）：核准會套用，失敗自動還原並把申請標成 failed。
	Decide(ctx context.Context, req *PolicyDecideRequest) (*PolicyDecideResponse, error)
	// Revoke 撤銷一張已套用的申請（只有主人）：回到申請之前。
	Revoke(ctx context.Context, req *PolicyRevokeRequest) (*PolicyRevokeResponse, error)
}

type PolicyKind string

const (
	PolicyKindRule     PolicyKind = "rule"     // 一條精準規則：誰可以做哪一類動作、在哪個範圍
	PolicyKindGuidance PolicyKind = "guidance" // 一句話，告訴平台的自動判斷「這類操作是主人要的」
)

type PolicyStatus string

const (
	PolicyStatusPending  PolicyStatus = "pending"
	PolicyStatusApplied  PolicyStatus = "applied"
	PolicyStatusRejected PolicyStatus = "rejected"
	PolicyStatusFailed   PolicyStatus = "failed"  // 核准了但套用失敗，已還原
	PolicyStatusRevoked  PolicyStatus = "revoked" // 主人撤銷
	PolicyStatusExpired  PolicyStatus = "expired" // 到期自動失效
)

// PolicyRule 是一條已經生效的規則（核心開 session 時交給引擎）：這位秘書可以直接做 Action，範圍是 Scope（資料夾；空表示任何地方）。
type PolicyRule struct {
	Agent  string      `json:"agent,omitempty"`
	Action ActionClass `json:"action,omitempty"`
	Scope  string      `json:"scope,omitempty"`
}

// PolicyRequest 是一張申請單（P-<n>）。
type PolicyRequest struct {
	ID          string       `json:"id,omitempty"`
	From        string       `json:"from,omitempty"` // 誰送的
	Kind        PolicyKind   `json:"kind,omitempty"`
	Agent       string       `json:"agent,omitempty"`  // rule：這條規則給哪位秘書（預設送件人）
	Action      ActionClass  `json:"action,omitempty"` // rule
	Scope       string       `json:"scope,omitempty"`  // rule：資料夾（絕對路徑或 ~/ 開頭）；空表示任何地方
	Text        string       `json:"text,omitempty"`   // guidance：一句話
	Why         string       `json:"why,omitempty"`    // 被擋的情境
	Risk        string       `json:"risk,omitempty"`   // 申請人自述的風險
	Impact      string       `json:"impact,omitempty"` // 核心算的影響預覽：誰多了什麼
	Broad       bool         `json:"broad"`            // 規則過寬：審核畫面用紅字警語
	Status      PolicyStatus `json:"status,omitempty"`
	Permanent   bool         `json:"permanent"` // Q7b 起核准一律 true（舊單可能是 false，到 ExpiresAt 自動失效）
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	ExpiresAt   time.Time    `json:"expires_at,omitempty"` // Q7b 起核准不設到期；只有 Q7b 之前核准的舊單會有
	DecidedAt   time.Time    `json:"decided_at,omitempty"`
	DecidedBy   string       `json:"decided_by,omitempty"`
	Note        string       `json:"note,omitempty"`        // 主人的備註（核准、拒絕、撤銷時）
	Notice      string       `json:"notice,omitempty"`      // 發給主人的通知 id
	Fingerprint string       `json:"fingerprint,omitempty"` // 預覽指紋：核准時帶回來，期間申請或政策變了就要求重看
}

type PolicyRequestRequest struct {
	Kind   PolicyKind  `json:"kind,omitempty"`
	Agent  string      `json:"agent,omitempty"`
	Action ActionClass `json:"action,omitempty"`
	Scope  string      `json:"scope,omitempty"`
	Text   string      `json:"text,omitempty"`
	Why    string      `json:"why,omitempty"`
	Risk   string      `json:"risk,omitempty"`
	By     *Actor      `json:"by,omitempty"`
}

type PolicyRequestResponse struct {
	Request *PolicyRequest `json:"request,omitempty"`
}

type PolicyListRequest struct {
	Statuses []PolicyStatus `json:"statuses,omitempty"`
	Agent    string         `json:"agent,omitempty"` // 只列這位送的或給這位的；空表示全部
}

type PolicyListResponse struct {
	Requests []PolicyRequest `json:"requests,omitempty"`
}

type PolicyGetRequest struct {
	ID string `json:"id,omitempty"`
}

type PolicyGetResponse struct {
	Request *PolicyRequest `json:"request,omitempty"`
}

type PolicyDecideRequest struct {
	ID          string `json:"id,omitempty"`
	Approve     bool   `json:"approve"`
	Permanent   bool   `json:"permanent"` // Q7b 起收下但不影響結果：核准後一律一直有效
	Note        string `json:"note,omitempty"`
	Scope       string `json:"scope,omitempty"`       // 改後核准：換成範圍更窄的資料夾（只有 rule）
	Text        string `json:"text,omitempty"`        // 改後核准：換一句話（只有 guidance）
	Fingerprint string `json:"fingerprint,omitempty"` // 核准時一定要帶：畫面上看到的那一版
	By          *Actor `json:"by,omitempty"`
}

type PolicyDecideResponse struct {
	Request *PolicyRequest `json:"request,omitempty"`
}

type PolicyRevokeRequest struct {
	ID   string `json:"id,omitempty"`
	Note string `json:"note,omitempty"`
	By   *Actor `json:"by,omitempty"`
}

type PolicyRevokeResponse struct {
	Request *PolicyRequest `json:"request,omitempty"`
}
