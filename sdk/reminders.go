package sdk

import (
	"context"
	"time"
)

// RemindersService：主人待辦（schema reminder）。到期的送達由 reminders 與 scheduler 做，
// 提醒、確認、討論是純程式，不經過秘書（REQ-63）。
type RemindersAPI interface {
	Create(ctx context.Context, req *RemindersCreateRequest) (*RemindersCreateResponse, error)
	Get(ctx context.Context, req *RemindersGetRequest) (*RemindersGetResponse, error)
	// List 依預設查詢（今天、逾期、這週…）或自訂條件列出（REQ-70）。
	List(ctx context.Context, req *RemindersListRequest) (*RemindersListResponse, error)
	Update(ctx context.Context, req *RemindersUpdateRequest) (*RemindersUpdateResponse, error)
	// Snooze 延後。until 可以是 preset（"3h"、"tomorrow"、"tomorrow-same"、"30m"）或絕對時間。
	Snooze(ctx context.Context, req *RemindersSnoozeRequest) (*RemindersSnoozeResponse, error)
	Done(ctx context.Context, req *RemindersDoneRequest) (*RemindersDoneResponse, error)
	// Doing 使用者說「還在做」：改進行中，明天同一時間再問。
	Doing(ctx context.Context, req *RemindersDoingRequest) (*RemindersDoingResponse, error)
	Cancel(ctx context.Context, req *RemindersCancelRequest) (*RemindersCancelResponse, error)
}

type Follow string

const (
	FollowNone    Follow = "none"
	FollowRemind  Follow = "remind"
	FollowConfirm Follow = "confirm"
	FollowCheck   Follow = "check"
	FollowDiscuss Follow = "discuss"
)

type Cycle string

const (
	CycleOnce     Cycle = "once"
	CycleDaily    Cycle = "daily"
	CycleWeekly   Cycle = "weekly"
	CycleBiweekly Cycle = "biweekly"
	CycleMonthly  Cycle = "monthly"
)

// Difficulty 是待辦的難度，決定完成時給幾點（RMD-24）：簡單 1、普通 2（預設）、困難 3；只有會集點的跟進方式才算（RMD-26）。
type Difficulty string

const (
	DifficultyEasy   Difficulty = "easy"
	DifficultyNormal Difficulty = "normal"
	DifficultyHard   Difficulty = "hard"
)

type ReminderStatus string

const (
	ReminderStatusTodo      ReminderStatus = "todo"
	ReminderStatusDoing     ReminderStatus = "doing"
	ReminderStatusAsking    ReminderStatus = "asking"
	ReminderStatusShelved   ReminderStatus = "shelved"
	ReminderStatusDone      ReminderStatus = "done"
	ReminderStatusCancelled ReminderStatus = "cancelled"
)

type ReminderQuery string

const (
	ReminderQueryToday         ReminderQuery = "today"
	ReminderQueryOverdue       ReminderQuery = "overdue"
	ReminderQueryThisWeek      ReminderQuery = "this_week"
	ReminderQueryWaiting       ReminderQuery = "waiting" // 等回覆
	ReminderQueryNoTime        ReminderQuery = "no_time"
	ReminderQueryDoneYesterday ReminderQuery = "done_yesterday"
	ReminderQueryUpcoming      ReminderQuery = "upcoming" // 明天 00:00 以後有時間、還沒結束的，由早到晚
)

type Reminder struct {
	ID      string         `json:"id,omitempty"` // R-<n>
	Content string         `json:"content,omitempty"`
	Follow  Follow         `json:"follow,omitempty"`
	Cycle   Cycle          `json:"cycle,omitempty"`
	At      time.Time      `json:"at,omitempty"`
	Owner   string         `json:"owner,omitempty"` // 職位 id
	Room    string         `json:"room,omitempty"`
	Status  ReminderStatus `json:"status,omitempty"`
	Check   string         `json:"check,omitempty"`
	Source  string         `json:"source,omitempty"`
	Asks    int32          `json:"asks"`
	SentAt  time.Time      `json:"sent_at,omitempty"`
	Goal    string         `json:"goal,omitempty"`
	Body    string         `json:"body,omitempty"` // 描述（Markdown）
	// 難度與累積點數（RMD-24）：done_points 只增不減，習慣套件讀它算衣櫃點數；done_at 是最近一次完成的時間。
	Difficulty Difficulty `json:"difficulty,omitempty"`
	DonePoints int32      `json:"done_points"`
	DoneAt     time.Time  `json:"done_at,omitempty"`
	// 過期從哪一刻算（RMD-26）：這一次被延後前原本該做的時間；空＝跟 at 一樣。
	DueAt time.Time `json:"due_at,omitempty"`
	// 現在按「做了」會拿幾點（RMD-26，算出來的、不存）：只有「問我做了沒」「秘書幫我確認」集點，難度點數＋過期每滿一週 1 點；結束了或今天給過是 0。
	Points int32 `json:"points"`
	// 附件（RMD-23、D-61，跟任務單同一套）：`/share/<名字>` 連結，檔案在 `<家>/runtime/share/<名字>`。
	Attachments []string  `json:"attachments,omitempty"`
	By          string    `json:"by,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
	ClosedAt    time.Time `json:"closed_at,omitempty"`
	Archived    bool      `json:"archived"`
}

type RemindersCreateRequest struct {
	Content string    `json:"content,omitempty"`
	Follow  Follow    `json:"follow,omitempty"`
	Cycle   Cycle     `json:"cycle,omitempty"`
	At      time.Time `json:"at,omitempty"` // 空：套用預設時間規則（REQ-62）
	Owner   string    `json:"owner,omitempty"`
	Room    string    `json:"room,omitempty"`
	Check   string    `json:"check,omitempty"`
	Source  string    `json:"source,omitempty"`
	Body    string    `json:"body,omitempty"` // 描述
	// 難度（RMD-24）：空＝普通。
	Difficulty Difficulty `json:"difficulty,omitempty"`
	// 附件（RMD-23）：`/share/<名字>` 連結（先上傳或 `evergenie share`），要是這台分享資料夾裡真的有的檔。
	Attachments []string `json:"attachments,omitempty"`
	By          *Actor   `json:"by,omitempty"`
}

type RemindersCreateResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}

type RemindersGetRequest struct {
	ID string `json:"id,omitempty"`
}

type RemindersGetResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}

type RemindersListRequest struct {
	Query           ReminderQuery    `json:"query,omitempty"` // UNSPECIFIED 表示用下面的條件
	Owner           string           `json:"owner,omitempty"`
	Statuses        []ReminderStatus `json:"statuses,omitempty"`
	IncludeArchived bool             `json:"include_archived"`
	Page            *PageRequest     `json:"page,omitempty"`
}

type RemindersListResponse struct {
	Reminders []Reminder    `json:"reminders,omitempty"`
	Page      *PageResponse `json:"page,omitempty"`
}

type RemindersUpdateRequest struct {
	ID      string     `json:"id,omitempty"`
	Content *string    `json:"content,omitempty"`
	Follow  *Follow    `json:"follow,omitempty"`
	Cycle   *Cycle     `json:"cycle,omitempty"`
	At      *time.Time `json:"at,omitempty"`
	Owner   *string    `json:"owner,omitempty"`
	Room    *string    `json:"room,omitempty"`
	Check   *string    `json:"check,omitempty"`
	Goal    *string    `json:"goal,omitempty"`
	Body    *string    `json:"body,omitempty"` // 描述；空字串清掉
	// 難度（RMD-24）：只影響之後的完成，已經給的點不變。
	Difficulty *Difficulty `json:"difficulty,omitempty"`
	// 附件（RMD-23）：加在後面（已經有的略過）／從單上拿掉（檔案不刪）。
	AddAttachments    []string `json:"add_attachments,omitempty"`
	RemoveAttachments []string `json:"remove_attachments,omitempty"`
	By                *Actor   `json:"by,omitempty"`
}

type RemindersUpdateResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}

type RemindersSnoozeRequest struct {
	ID    string `json:"id,omitempty"`
	Until string `json:"until,omitempty"`
	By    *Actor `json:"by,omitempty"`
}

type RemindersSnoozeResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}

type RemindersDoneRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type RemindersDoneResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}

type RemindersDoingRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type RemindersDoingResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}

type RemindersCancelRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type RemindersCancelResponse struct {
	Reminder *Reminder `json:"reminder,omitempty"`
}
