package sdk

import (
	"context"
	"time"
)

// TasksService：任務單（schema task）。
type TasksAPI interface {
	Waiting(ctx context.Context, req *TasksWaitingRequest) (*TasksWaitingResponse, error)
	Stale(ctx context.Context, req *TasksStaleRequest) (*TasksStaleResponse, error)
	Reject(ctx context.Context, req *TasksRejectRequest) (*TasksRejectResponse, error)
	Accept(ctx context.Context, req *TasksAcceptRequest) (*TasksAcceptResponse, error)
	// Archive 主人在看板刪單：沒結束的先記成 cancelled，再搬進 archive（只封存、不刪檔，TSK-18）。
	Archive(ctx context.Context, req *TasksArchiveRequest) (*TasksArchiveResponse, error)
	Submit(ctx context.Context, req *TasksSubmitRequest) (*TasksSubmitResponse, error)
	Create(ctx context.Context, req *TasksCreateRequest) (*TasksCreateResponse, error)
	Get(ctx context.Context, req *TasksGetRequest) (*TasksGetResponse, error)
	List(ctx context.Context, req *TasksListRequest) (*TasksListResponse, error)
	// Update 改狀態、結果、Goal 等。改成 waiting_user（等你）時 tasks 會要求 notify 推一則 approve（REQ-54）。
	Update(ctx context.Context, req *TasksUpdateRequest) (*TasksUpdateResponse, error)
	// Close 結單：缺 result／verify／verify_how 就拒絕（REQ-51）。秘書只能結自己負責、有主人驗收條件、驗過（verified）的一般任務成 done（法律第 1.4 條）。
	Close(ctx context.Context, req *TasksCloseRequest) (*TasksCloseResponse, error)
	// Reconcile 夜間對帳：列出這位秘書超過 24 小時沒更新的 doing 單，不改狀態（REQ-53、D-22）。
	Reconcile(ctx context.Context, req *TasksReconcileRequest) (*TasksReconcileResponse, error)
}

type TaskType string

const (
	TaskTypeTask TaskType = "task"
	TaskTypeAsk  TaskType = "ask"
)

type TaskStatus string

// 任務單的六種狀態（2026-11-01 從九種收成六種，設計 design-goal-task.md）：待做、進行中、等你、等外部、完成、不做了。
// 「待做」的值沿用舊的 idea（升級後退回舊版也讀得懂）；交付等主人驗收是「等你」且 proposed 有值（原本的 review）。
// 舊資料的 review、failed、shelved 由 tasks 在讀取與家的遷移時轉成新狀態，API 不會再出現。
const (
	TaskStatusIdea            TaskStatus = "idea" // 待做（含原本的「想法」）
	TaskStatusDoing           TaskStatus = "doing"
	TaskStatusWaitingUser     TaskStatus = "waiting_user" // 等你：要主人決定（ask）或驗收（proposed 有值）
	TaskStatusWaitingExternal TaskStatus = "waiting_external"
	TaskStatusDone            TaskStatus = "done"
	TaskStatusCancelled       TaskStatus = "cancelled" // 不做了（原本的無法完成、擱置、取消；原因寫在 result）
)

type Verify string

const (
	VerifyVerified     Verify = "verified"
	VerifyUnverifiable Verify = "unverifiable"
)

type Task struct {
	Ask          string     `json:"ask,omitempty"`
	WaitingSince time.Time  `json:"waiting_since,omitempty"`
	Nudges       int32      `json:"nudges"`
	LastNudgedAt time.Time  `json:"last_nudged_at,omitempty"`
	Proposed     TaskStatus `json:"proposed,omitempty"` // 交付等驗收時秘書建議的結局（done 或 cancelled）；有值且 status 是 waiting_user＝等你驗收
	ReviewNote   string     `json:"review_note,omitempty"`
	AcceptedBy   string     `json:"accepted_by,omitempty"`
	AcceptedAt   time.Time  `json:"accepted_at,omitempty"`
	ID           string     `json:"id,omitempty"` // SEC-<n>
	Title        string     `json:"title,omitempty"`
	Type         TaskType   `json:"type,omitempty"`
	Agent        string     `json:"agent,omitempty"` // 職位 id；想法可以空
	Status       TaskStatus `json:"status,omitempty"`
	Result       string     `json:"result,omitempty"`
	Verify       Verify     `json:"verify,omitempty"`
	VerifyHow    string     `json:"verify_how,omitempty"`
	Links        []string   `json:"links,omitempty"`
	// 附件（TSK-28、D-61）：`/share/<名字>` 連結（看板上傳或 `evergenie share` 給的），檔案在 `<家>/runtime/share/<名字>`；照加入的順序。
	Attachments []string `json:"attachments,omitempty"`
	Goal        string   `json:"goal,omitempty"` // GOAL-<n>，選填（原本的 group，作者 2026-10-25 改名）
	// 驗收條件（法律第 1.4 條，2026-11-01）：一般任務一定要有。CriteriaOwner＝條件是主人給的（主人寫的，或秘書照抄主人的話）；
	// 有主人的條件（或掛了 Goal，Goal 的驗收條件也算主人的），負責的秘書照條件驗過就能自己結單，不然送驗收等主人結。
	Criteria      string `json:"criteria,omitempty"`
	CriteriaOwner bool   `json:"criteria_owner,omitempty"`
	// 工作 session（P7-5、D-33）：引擎的 session id（關掉後用引擎的接續（resume）接回）。
	EngineSession string    `json:"engine_session,omitempty"`
	Body          string    `json:"body,omitempty"`
	By            string    `json:"by,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
	ClosedAt      time.Time `json:"closed_at,omitempty"`
	Archived      bool      `json:"archived"`
}

type TasksCreateRequest struct {
	Ask       string     `json:"ask,omitempty"`
	Goal      string     `json:"goal,omitempty"`
	Title     string     `json:"title,omitempty"`
	Type      TaskType   `json:"type,omitempty"`
	Agent     string     `json:"agent,omitempty"`
	Status    TaskStatus `json:"status,omitempty"` // 預設 doing；問答查完一次結掉可以直接給 done 並附結果
	Result    string     `json:"result,omitempty"`
	Verify    Verify     `json:"verify,omitempty"`
	VerifyHow string     `json:"verify_how,omitempty"`
	Body      string     `json:"body,omitempty"`
	// 附件：`/share/<名字>` 連結，檔案要已經在這台的分享資料夾（看板上傳，或先 `evergenie share <路徑>`）。
	Attachments []string `json:"attachments,omitempty"`
	// 驗收條件：秘書開一般任務一定要寫；主人開單可以不寫（接單的秘書送出前補上，那條算秘書寫的）。
	Criteria string `json:"criteria,omitempty"`
	// 秘書照抄主人說的驗收條件時設 true（主人開單時一律算主人的，不用設）。
	CriteriaFromOwner bool   `json:"criteria_from_owner,omitempty"`
	By                *Actor `json:"by,omitempty"`
}

type TasksCreateResponse struct {
	Task *Task `json:"task,omitempty"`
}

type TasksGetRequest struct {
	ID string `json:"id,omitempty"`
}

type TasksGetResponse struct {
	Task *Task `json:"task,omitempty"`
}

type TasksListRequest struct {
	Agent           string       `json:"agent,omitempty"` // 空表示全部
	Statuses        []TaskStatus `json:"statuses,omitempty"`
	IdeasOnly       bool         `json:"ideas_only"` // 待做收件匣（status idea），開單日舊的在前（REQ-52）
	IncludeArchived bool         `json:"include_archived"`
	Goal            string       `json:"goal,omitempty"`
	Page            *PageRequest `json:"page,omitempty"`
}

type TasksListResponse struct {
	Tasks []Task        `json:"tasks,omitempty"`
	Page  *PageResponse `json:"page,omitempty"`
}

type TasksUpdateRequest struct {
	Ask *string `json:"ask,omitempty"`
	ID  string  `json:"id,omitempty"`
	// 改標題與需求（TSK-29，只有主人能改）：沒給＝不動；標題不能改成空白，需求給空字串＝清掉。
	Title  *string     `json:"title,omitempty"`
	Body   *string     `json:"body,omitempty"`
	Status *TaskStatus `json:"status,omitempty"`
	Result *string     `json:"result,omitempty"`
	Agent  *string     `json:"agent,omitempty"`
	Type   *TaskType   `json:"type,omitempty"`
	Goal   *string     `json:"goal,omitempty"` // 空字串表示拿掉 Goal
	// 工作 session（P7-5）：空字串表示拿掉。
	EngineSession *string  `json:"engine_session,omitempty"`
	AddLinks      []string `json:"add_links,omitempty"`
	// 加附件（`/share/<名字>`，同 TasksCreateRequest.attachments；已經在單上的略過）與拿掉附件（只從單上拿掉，檔案不刪）。
	AddAttachments    []string `json:"add_attachments,omitempty"`
	RemoveAttachments []string `json:"remove_attachments,omitempty"`
	// 改驗收條件：主人改的算主人的；秘書改的算秘書的，除非同時給 criteria_from_owner（照抄主人的話）。
	Criteria          *string `json:"criteria,omitempty"`
	CriteriaFromOwner bool    `json:"criteria_from_owner,omitempty"`
	By                *Actor  `json:"by,omitempty"`
}

type TasksUpdateResponse struct {
	Task *Task `json:"task,omitempty"`
}

type TasksCloseRequest struct {
	ID        string     `json:"id,omitempty"`
	Status    TaskStatus `json:"status,omitempty"` // done 或 cancelled（不做了，原因寫在 result）
	Result    string     `json:"result,omitempty"`
	Verify    Verify     `json:"verify,omitempty"`     // DONE 必填
	VerifyHow string     `json:"verify_how,omitempty"` // DONE 必填
	AddLinks  []string   `json:"add_links,omitempty"`
	By        *Actor     `json:"by,omitempty"`
}

type TasksCloseResponse struct {
	Task *Task `json:"task,omitempty"`
}

type TasksReconcileRequest struct {
	Agent string `json:"agent,omitempty"` // 空表示全部
}

type TasksReconcileResponse struct {
	Tasks []Task `json:"tasks,omitempty"`
	// 已停用（D-22 起不自動擱置），永遠是空的。
	ShelvedIDS []string `json:"shelved_ids,omitempty"`
}

type TasksSubmitRequest struct {
	ID        string     `json:"id,omitempty"`
	Result    string     `json:"result,omitempty"`
	Verify    Verify     `json:"verify,omitempty"`
	VerifyHow string     `json:"verify_how,omitempty"`
	Proposed  TaskStatus `json:"proposed,omitempty"` // done 或 cancelled
	AddLinks  []string   `json:"add_links,omitempty"`
	By        *Actor     `json:"by,omitempty"`
}
type TasksSubmitResponse struct {
	Task *Task `json:"task,omitempty"`
}
type TasksAcceptRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}
type TasksAcceptResponse struct {
	Task *Task `json:"task,omitempty"`
}
type TasksArchiveRequest struct {
	IDS []string `json:"ids,omitempty"`
	By  *Actor   `json:"by,omitempty"`
}
type TasksArchiveResponse struct {
	Archived []string `json:"archived,omitempty"`
}
type TasksRejectRequest struct {
	ID   string `json:"id,omitempty"`
	Note string `json:"note,omitempty"`
	By   *Actor `json:"by,omitempty"`
}
type TasksRejectResponse struct {
	Task *Task `json:"task,omitempty"`
}
type TasksStaleRequest struct{} // 時間由伺服器的 Clock 決定
type TasksStaleResponse struct {
	Tasks []Task `json:"tasks,omitempty"`
}
type TasksWaitingRequest struct{} // 同上
type TasksWaitingResponse struct {
	Tasks []WaitingTask `json:"tasks,omitempty"`
}

// WaitingTask 是等使用者的單與等了幾天。
type WaitingTask struct {
	Task        *Task `json:"task,omitempty"`
	WaitingDays int32 `json:"waiting_days"` // floor((now - waiting_since) / 24h)，最小 0
}
type Proposed = TaskStatus
