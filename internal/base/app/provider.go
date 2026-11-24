package app

import (
	"context"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
)

// Module 是組裝框架認得的模組：依 New 的順序 Start、反序 Stop（MOD-3、MOD-4）。
//
// Start 只開背景工作並立刻回傳；Stop 要在 ctx 逾時前結束。
type Module interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// Command 是一個指令的實作；型別與執行環境定義在 cmdline。
type Command = cmdline.Command

// Mount 是要掛到 HTTP 的一個 sdk 服務：Service 是 sdk 的服務名（例 "reminders"），
// Impl 是實作該服務介面的值；App.Mux 交給 sdk.Mount 掛到同一個 http.ServeMux。
//
// Methods 是選用的：sdk 的一個服務（例 "system"）有時由好幾個業務模組分著實作，這時每個業務模組只登記自己那幾個方法
// （網址上的名稱，例 "trust"、"set_permission_levels"），Impl 只要有這幾個方法；空的就是整個服務。
// 同一個服務的同一個方法只能有一個模組登記；整個服務跟任何部分登記也不能並存。
type Mount struct {
	Service string
	Impl    any
	Methods []string
}

// RouteProvider 由有 HTTP 的業務模組實作。
type RouteProvider interface {
	Routes() []Mount
}

// Status 是一項健檢的結果。
type Status int

const (
	// StatusOK 沒事。
	StatusOK Status = iota
	// StatusWarn 提醒：不影響使用，但最好看一下。
	StatusWarn
	// StatusError 錯誤：有東西壞了或不能用。
	StatusError
)

func (s Status) String() string {
	switch s {
	case StatusOK:
		return "OK"
	case StatusWarn:
		return "提醒"
	case StatusError:
		return "錯誤"
	}
	return "未知"
}

// CheckResult 是健檢結果：狀態加一行給人看的白話；沒問題以外的結果可以附下一步怎麼修（Fix，一行白話）。
type CheckResult struct {
	Status  Status
	Message string
	Fix     string
}

// Check 是一個健檢項目，由 doctor 業務模組收集後執行並顯示。
// Name 寫成「群組.項目」（例 "schedule.jobs"、"push.status"）：點前面是 doctor 輸出時的群組，整串是項目編號。
//
// Run 與 RunAll 二選一：一般一項一個結果（Run）；同一項天生有好幾條（每筆壞資料、每個帳號、每位秘書各一條）時用 RunAll，
// 回空的就是這一項沒有要說的（doctor 照樣在整組沒東西時補一條「沒有發現問題」）。
type Check struct {
	Name   string
	Run    func(ctx context.Context) CheckResult
	RunAll func(ctx context.Context) []CheckResult
}

// CheckProvider 由有健檢項目的業務模組實作。
type CheckProvider interface {
	Checks() []Check
}

// CheckConsumer 由收集健檢的模組（doctor）實作：Assemble 把合併後的 Checks() 交給它（名稱不合它的規則就回錯）。
type CheckConsumer interface {
	ConsumeChecks(checks []Check) error
}

// Job 是業務模組內建的定時工作（取代業務模組自己在 Start 開的固定週期 ticker）。Cron（五欄位 cron，家的時區）與
// Every（固定間隔）剛好給一個；First 只配 Every，是啟動後第一次跑之前等多久（零值＝等一個 Every）。
// App 只收集；排進計時、每項一條 goroutine、上一次還沒跑完就略過這一次，是底層 timer 的 JobRunner 的事。
// Run 回的錯誤只寫 log，不影響下一次。
type Job struct {
	Name  string
	Cron  string
	Every time.Duration
	First time.Duration
	Run   func(ctx context.Context) error
}

// JobProvider 由有內建定時工作的業務模組實作。
type JobProvider interface {
	Jobs() []Job
}

// JobConsumer 由負責跑定時工作的模組（timer 的 JobRunner）實作：Assemble 把合併後的 Jobs() 交給它。
type JobConsumer interface {
	ConsumeJobs(jobs []Job) error
}

// BoardPart 是首頁的一段資料。Key 是這段在首頁資料裡的鍵（全域唯一）。
type BoardPart struct {
	Key  string
	Load func(ctx context.Context) (any, error)
}

// BoardProvider 由首頁要顯示資料的業務模組實作。
type BoardProvider interface {
	Board() []BoardPart
}

// BoardConsumer 由彙整首頁的模組（overview）實作：Assemble 把合併後的 Board() 交給它（不認得的 Key 就回錯）。
type BoardConsumer interface {
	ConsumeBoard(parts []BoardPart) error
}
