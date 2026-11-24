package sdk

import "context"

// 認領職位的通知（SEC-302 S9、REQ-341～REQ-347）：建好套件後，秘書請核心發一則「要你同意」，
// 選項「好，用 X｜換一個｜之後再說」。主人按「好」由核心以主人身分套用（package.sync＋啟用）；秘書自己不能套用。

// NotifySourcePackage 標「認領職位」的通知：只有 pkg 模組在 Send 時標（API 的 Send 不帶 source，秘書標不了）；
// 回答由 pkg 處理，不送回秘書的對話。
const NotifySourcePackage = "package"

// ClaimState 是一個職位的認領通知走到哪裡（存在 settings/packages.toml 的 [claims.<職位>]）。
type ClaimState string

const (
	ClaimStateAsked  ClaimState = "asked"  // 通知發出去了、主人還沒回
	ClaimStateLater  ClaimState = "later"  // 主人按「之後再說」：同一個職位不再發通知
	ClaimStateChange ClaimState = "change" // 主人按「換一個」（或打了別的字）：他自己到設定頁那張卡挑，不再發通知
	ClaimStateDone   ClaimState = "done"   // 主人按「好」，核心已經建好秘書並啟用
	ClaimStateFailed ClaimState = "failed" // 主人按「好」，但套用失敗（原因在 Detail）；不再發通知，設定頁那張卡照常
)

type PackageClaimNotifyRequest struct {
	ID string `json:"id,omitempty"` // 套件 id
	// Agent：要認領的職位 id（空＝這個套件第一個還沒有人負責的職位，選用職位除外，DEV-157）。
	Agent string `json:"agent,omitempty"`
	// Persona：建議的角色（空＝核心照規則挑：套件自己帶的上線角色 → 還沒坐位置的上線角色 → 其他上線角色）。
	Persona string `json:"persona,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type PackageClaimNotifyResponse struct {
	Package     string     `json:"package,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	Persona     string     `json:"persona,omitempty"`      // 建議的角色代號
	PersonaName string     `json:"persona_name,omitempty"` // 角色的名字（通知按鈕上寫的）
	Reason      string     `json:"reason,omitempty"`       // 為什麼建議她（白話）
	Notice      string     `json:"notice,omitempty"`       // 認領通知 M-<n>（這次發的，或之前發過的）
	State       ClaimState `json:"state,omitempty"`
	Sent        bool       `json:"sent"`             // 這次有沒有發新的通知（之前問過、主人說之後再說都不發）
	Detail      string     `json:"detail,omitempty"` // 沒發時的白話原因
}

func (c *packageClient) ClaimNotify(ctx context.Context, req *PackageClaimNotifyRequest) (*PackageClaimNotifyResponse, error) {
	return Invoke[PackageClaimNotifyRequest, PackageClaimNotifyResponse](ctx, c.c, "/api/package/claim_notify", req)
}
