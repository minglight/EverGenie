package sdk

import (
	"context"
	"time"
)

type GoalState string

const (
	GoalStateDraft     GoalState = "draft"
	GoalStateActive    GoalState = "active"
	GoalStateDone      GoalState = "done"
	GoalStateWithdrawn GoalState = "withdrawn"
)

type ActionClass string

const (
	ActionClassRead         ActionClass = "read"
	ActionClassWrite        ActionClass = "write"
	ActionClassCommit       ActionClass = "commit"
	ActionClassPush         ActionClass = "push"
	ActionClassDelete       ActionClass = "delete"
	ActionClassSendExternal ActionClass = "send_external"
	ActionClassSpend        ActionClass = "spend"
	ActionClassSettings     ActionClass = "settings"
)

type ActionVerdict string

const (
	ActionVerdictAllow ActionVerdict = "allow"
	ActionVerdictDeny  ActionVerdict = "deny"
	ActionVerdictAsk   ActionVerdict = "ask"
)

type PlanbookEntry struct {
	At   time.Time `json:"at,omitempty"`
	By   string    `json:"by,omitempty"`
	Note string    `json:"note,omitempty"`
}
type Goal struct {
	ID          string          `json:"id,omitempty"`
	Title       string          `json:"title,omitempty"`
	Text        string          `json:"text,omitempty"`
	Criteria    string          `json:"criteria,omitempty"` // 驗收條件：怎樣算達成（主人給的，跟原文一樣生效後不能改；2026-11-01 法律第 4 條）
	Agent       string          `json:"agent,omitempty"`
	State       GoalState       `json:"state,omitempty"`
	Notice      string          `json:"notice,omitempty"`
	ConfirmedAt time.Time       `json:"confirmed_at,omitempty"`
	ConfirmedBy string          `json:"confirmed_by,omitempty"`
	EndedAt     time.Time       `json:"ended_at,omitempty"`
	EndedBy     string          `json:"ended_by,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Planbook    string          `json:"planbook,omitempty"`
	PlanbookLog []PlanbookEntry `json:"planbook_log,omitempty"`
	By          string          `json:"by,omitempty"`
	CreatedAt   time.Time       `json:"created_at,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at,omitempty"`
}
type GoalsAPI interface {
	Propose(context.Context, *GoalsProposeRequest) (*GoalsProposeResponse, error)
	Create(context.Context, *GoalsCreateRequest) (*GoalsCreateResponse, error)
	Get(context.Context, *GoalsGetRequest) (*GoalsGetResponse, error)
	List(context.Context, *GoalsListRequest) (*GoalsListResponse, error)
	UpdatePlanbook(context.Context, *GoalsUpdatePlanbookRequest) (*GoalsUpdatePlanbookResponse, error)
	Done(context.Context, *GoalsDoneRequest) (*GoalsDoneResponse, error)
	Withdraw(context.Context, *GoalsWithdrawRequest) (*GoalsWithdrawResponse, error)
}
type GoalsProposeRequest struct {
	Title    string `json:"title,omitempty"`
	Text     string `json:"text,omitempty"`
	Criteria string `json:"criteria,omitempty"` // 必填：驗收條件（怎樣算達成）
	Agent    string `json:"agent,omitempty"`
	By       *Actor `json:"by,omitempty"`
}
type GoalsProposeResponse struct {
	Goal *Goal `json:"goal,omitempty"`
}

// GoalsCreateRequest 是主人直接開 Goal（D-39）：只有 by 是 owner(…) 才行，建好就是 active。
// Agent 是負責的秘書（職位 id 或名字，2026-11-01 加）；空的＝不指定，由接力的秘書自己判斷。
type GoalsCreateRequest struct {
	Title    string `json:"title,omitempty"`
	Text     string `json:"text,omitempty"`
	Criteria string `json:"criteria,omitempty"` // 必填：驗收條件（怎樣算達成）
	Agent    string `json:"agent,omitempty"`
	By       *Actor `json:"by,omitempty"`
}
type GoalsCreateResponse struct {
	Goal *Goal `json:"goal,omitempty"`
}

type GoalsGetRequest struct {
	ID string `json:"id,omitempty"`
}
type GoalsGetResponse struct {
	Goal *Goal `json:"goal,omitempty"`
}
type GoalsListRequest struct {
	States []GoalState `json:"states,omitempty"`
}
type GoalsListResponse struct {
	Goals []Goal `json:"goals,omitempty"`
}
type GoalsUpdatePlanbookRequest struct {
	ID   string `json:"id,omitempty"`
	Body string `json:"body,omitempty"`
	Note string `json:"note,omitempty"`
	By   *Actor `json:"by,omitempty"`
}
type GoalsUpdatePlanbookResponse struct {
	Goal *Goal `json:"goal,omitempty"`
}
type GoalsDoneRequest struct {
	ID      string `json:"id,omitempty"`
	Summary string `json:"summary,omitempty"`
	By      *Actor `json:"by,omitempty"`
}
type GoalsDoneResponse struct {
	Goal *Goal `json:"goal,omitempty"`
}
type GoalsWithdrawRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}
type GoalsWithdrawResponse struct {
	Goal *Goal `json:"goal,omitempty"`
}
