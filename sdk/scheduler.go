package sdk

import (
	"context"
	"time"
)

// Job 是一個排程工作（evergenie docs show schemas/job.md）。ID 是 `<職位>/<id>`，系統級沒有 `/`、也沒有 Agent。
// 一般排程用 Cron；時段型（主動聊天）用 Windows／WeekendWindows／PerDay／IdleMin，這時 Cron 不寫。
type Job struct {
	ID             string    `json:"id,omitempty"`
	Agent          string    `json:"agent,omitempty"`
	Name           string    `json:"name,omitempty"`
	Cron           []string  `json:"cron,omitempty"`
	Windows        []string  `json:"windows,omitempty"`
	WeekendWindows []string  `json:"weekend_windows,omitempty"`
	PerDay         int       `json:"per_day,omitempty"`
	IdleMin        int       `json:"idle_min,omitempty"`
	Enabled        bool      `json:"enabled"`
	Catchup        string    `json:"catchup,omitempty"`
	Action         string    `json:"action,omitempty"`
	Silent         bool      `json:"silent,omitempty"`
	Source         string    `json:"source,omitempty"`
	Body           string    `json:"body,omitempty"`
	Archived       bool      `json:"archived"`
	By             string    `json:"by,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	// NextRun 是下一次觸發的時間；只有 ListJobs 會填，停用、已移除、排不上的沒有。
	NextRun *time.Time `json:"next_run,omitempty"`
	// LastRun 是最近一次到期（或手動跑）的時間，LastResult 是那一次的結果，LastReason 是白話原因（略過、沒跑成時才有）；
	// RunsToday 是家的時區今天真的送出（觸發）幾次，略過的不算。只有 ListJobs 會填，從沒跑過的都沒有（SCH-16）。
	LastRun    *time.Time `json:"last_run,omitempty"`
	LastResult JobResult  `json:"last_result,omitempty"`
	LastReason string     `json:"last_reason,omitempty"`
	RunsToday  int        `json:"runs_today,omitempty"`
}

// JobResult 是排程最近一次的結果（SCH-16）。排程只知道自己送出的封包結局，不知道秘書做得好不好。
type JobResult string

const (
	JobResultOk      JobResult = "ok"      // 秘書回覆了這次的排程封包
	JobResultRunning JobResult = "running" // 送給秘書了，還沒結束
	JobResultFired   JobResult = "fired"   // 系統級排程（沒有秘書）觸發了；結果由處理它的模組負責，排程不知道
	JobResultFailed  JobResult = "failed"  // 沒跑成：送不出去、沒送進對話、做到一半停了
	JobResultSkipped JobResult = "skipped" // 這次略過：錯過時間不補跑、過了時段、今天次數用完
)

// SchedulerAPI 是排程服務（Q1）：job 的寫入只走這裡（API-1），驗證由排程模組負責。
type SchedulerAPI interface {
	ListJobs(context.Context, *SchedulerListJobsRequest) (*SchedulerListJobsResponse, error)
	SaveJob(context.Context, *SchedulerSaveJobRequest) (*SchedulerSaveJobResponse, error)
	SetJob(context.Context, *SchedulerSetJobRequest) (*SchedulerSetJobResponse, error)
	RemoveJob(context.Context, *SchedulerRemoveJobRequest) (*SchedulerRemoveJobResponse, error)
	RunJob(context.Context, *SchedulerRunJobRequest) (*SchedulerRunJobResponse, error)
}
type SchedulerListJobsRequest struct {
	Agent           string `json:"agent,omitempty"`
	IncludeArchived bool   `json:"include_archived,omitempty"`
}
type SchedulerListJobsResponse struct {
	Jobs []Job `json:"jobs,omitempty"`
}

// SchedulerSaveJobRequest 新增一個排程；同一個 id 已經有了（含已移除的）回 conflict。
type SchedulerSaveJobRequest struct {
	Job *Job   `json:"job,omitempty"`
	By  *Actor `json:"by,omitempty"`
}
type SchedulerSaveJobResponse struct {
	Job *Job `json:"job,omitempty"`
}

// SchedulerSetJobRequest 只改使用者能改的幾樣：開關、cron、名稱、內文。沒給的不動；名稱給了不能是空的。
type SchedulerSetJobRequest struct {
	ID      string   `json:"id,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
	Cron    []string `json:"cron,omitempty"`
	By      *Actor   `json:"by,omitempty"`
	Name    *string  `json:"name,omitempty"`
	Body    *string  `json:"body,omitempty"`
}
type SchedulerSetJobResponse struct {
	Job *Job `json:"job,omitempty"`
}

// SchedulerRemoveJobRequest：`source: user` 的封存（archived=true），其他的只停用（enabled=false）。
type SchedulerRemoveJobRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}
type SchedulerRemoveJobResponse struct {
	Job      *Job `json:"job,omitempty"`
	Archived bool `json:"archived"`
}

// SchedulerRunJobRequest 立刻跑一次（發 schedule.fire，跟時間到了一樣）；系統級排程回 invalid。
type SchedulerRunJobRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}
type SchedulerRunJobResponse struct {
	Job *Job      `json:"job,omitempty"`
	At  time.Time `json:"at,omitempty"`
}
