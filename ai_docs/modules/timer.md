# 模組：timer（`internal/base/timer`）
Status: DRAFT — 設計 2026-10-31（骨架重整第 2 步 `base-split`，計畫 `2026-10-31-evergenie-subtraction`），pending author review.

**目的：** 計時：cron 解析與下次時間、時段型排程、一個排程項目到期時該不該觸發（補跑、錯過、單次），加上共用的單一計時迴圈 `Scheduler`（每秒 tick；`Cron`／`At`／`Cancel`／`Next`）。計算的部分沒有 goroutine、不發事件、不讀 vault，每一條規則都能用假時間直接測；迴圈只決定「哪個項目到期了」，之後的事（發事件、額度）交給擁有者提供的 `Handler`。
**種類：** 底層模組（`internal/base/`；沒有 HTTP、沒有指令）

## MUST
- **TMR-1** — `ParseCron(specs, loc)` 把一到多個五欄位 cron（分、時、日、月、週）解析成 `Cron`，時區用 `loc`；空清單或任何一條解析不了回 `invalid`（白話，不帶輸入）。`Cron.Next(after)` 回 `after` 之後最早的一次（多條取最早的），排不出來回零值。
- **TMR-2** — `NextRuns(specs, loc, after, n)` 用同一份 `ParseCron` 算出下 `n` 次（多條合併、由早到晚）。排程服務、遷移報告、排程管理的驗證共用這一份，算出來的時間一定一致。
- **TMR-3** — `Window`（時段型，舊 `windows`）：`Weekday`／`Weekend`（`HH:MM-HH:MM`，`Weekend` 空表示週末沿用 `Weekday`）、`PerDay`、`IdleMin`。`Slots(id, day)` 每天從時段裡挑 `PerDay` 段、每段裡挑一分鐘（用日期與 `id` 當亂數種子，同一天同一個 `id` 結果固定、由早到晚）；`Next(id, after, loc)` 回 `after` 之後最早的觸發點與它的時段結束（最多往後找 8 天）。`ValidWindows` 檢查每段都是 `HH:MM-HH:MM` 而且結束晚於開始，否則 `invalid`。
- **TMR-4** — `Entry` 是一個排程項目的計時狀態（單次 `NewAt`、重複 `NewCron`、時段型 `NewWindow`）。`Due(now, lastTick, catchup, loc)` 判斷 `now` 時有沒有到期：沒到期回 `false` 且不改狀態；到期就回 `Due{At, Fire, Missed, CaughtUp, WindowEnd, Once}` 並把項目推進到下一次。呼叫者據此發事件、記錄觸發（`MarkFired`）、移除單次項目；`timer` 自己不發事件。
- **TMR-5** — cron 項目錯過的時間（`now` 的分鐘晚於排定的分鐘）：`catchup` 為真而且距上次 tick 不超過 6 小時 → 補跑最近一次（`At` 是最近一次、`CaughtUp` 為真）；否則略過。兩種都 `Missed`。同一分鐘已經觸發過（`MarkFired`／`CarryLast`）的不重複觸發，但照樣推進到下一次。
- **TMR-6** — 時段型項目：時段內到期照樣觸發（晚了幾分鐘也送）；`now` 已經過了時段結束就略過這一次（`Missed`、不補），一律排到下一個觸發點（找不到就沒有下一次）。
- **TMR-7** — 單次項目到期就觸發一次，`Once` 為真，補跑與重複判斷不適用。
- **TMR-8** — `Scheduler`（2026-10-31 從 `scheduler` 抽出）：`Cron(ctx, id, specs, CronOpt)`、`At(ctx, id, t)`、`Cancel`（沒有這個項目回 `not_found`）、`Next(id)` 的簽名不變，tasks（每日提醒 cron）、reminders（到期 at）、jobs（使用者排程）共用同一個迴圈。`Start` 讀家的時區後開每秒一次的 tick 迴圈並立刻回傳，`Stop` 等迴圈結束；沒設家（`SetHome`）就不啟動。同一個 id 再登記 `Cron` 會帶走上一次觸發的記錄。
- **TMR-9** — `Tick(ctx, now)` 依 id 排序判斷每個項目是否到期，把結果交給 `Handler`：錯過 → `Missed`；該觸發 → 先問 `Allow`（回 false 就不觸發、不記為已觸發），通過才 `MarkFired` 並 `Fire`；最後 `AfterTick`。單次項目到期時從迴圈移除。Handler 在沒有 Scheduler 鎖的情況下被呼叫（可以回頭呼叫 Scheduler）；沒接 Handler 之前到期的項目留著不被吃掉。
- **TMR-11** — `JobRunner`（`jobs.go`，2026-11-01）跑各模組用 `Jobs()` 登記的程式內建定時工作：實作 `app.JobConsumer`（`ConsumeJobs` 收合併後的清單，`Cron` 寫錯就回錯），`Start` 每項開一條 goroutine 後立刻回傳（`Every`：先等 `First`（零值＝一個 `Every`）、之後每 `Every`；`Cron`：照家的時區算下一次），`Stop` 等全部結束。同一項的 `Run` 是同步跑的，上一次沒跑完就不會有下一次（不疊加）；`Run` 回錯或 panic 只寫 log（`runtime/logs` 的 `jobs-runner`），下一次照跑。沒設家不啟動。registry 把它放在 app 清單最後：其他模組都啟動好才開始跑、關機時最先停。測試 `jobs_test.go` → `TestJobRunnerRunsEveryJob`、`TestJobRunnerRejectsBadCronAndNeedsHome`、`TestJobRunnerNext`。它跟 `Scheduler` 分開：`Scheduler` 管使用者資料（排程、提醒、任務單跟催）的到期，`JobRunner` 管程式內建的週期工作。
- **TMR-10** — `Sync(kind, fn)`：擁有者整批換掉某一種（`Item.Kind`）的項目，其他種類不動；`fn` 拿到現有的、回新的清單，在鎖裡跑，只做計算。`NewCronItem`、`NewWindowItem` 在鎖外組出新項目（jobs 重載用）。

## NEVER
- **TMR-N1** — 不實作 `Routes()` 或 `Commands()`，不發 bus 事件，不讀寫 vault 或檔案（底層模組）。goroutine 只有 `Scheduler` 的那一個 tick 迴圈與 `JobRunner` 每項工作一條；計算的部分（cron、window、Entry）不開 goroutine。
- **TMR-N2** — 計算的部分不自己讀系統時間：所有「現在」由呼叫者傳進來（`now`、`lastTick`、`after`）。`Scheduler` 的時間來源是注入的 `Clock`。
- **TMR-N3** — 不判斷安靜時段（用 `platform.Quiet`）；不認得 job 內文、額度、`schedule.fire` 這些 `jobs` 的概念（`Item.Agent` 與 `Item.Data` 只是替擁有者帶著，timer 不看）。
- **TMR-N4** — 不 import 業務模組，也不 import 其他核心模組（只用標準庫、`platform` 的錯誤型別與家設定／logger、`robfig/cron`，以及 `app` 的 `Job` 型別與 `JobConsumer` 介面——`JobRunner` 要吃 app 收集到的定時工作）。

## 職責
- cron 的解析與下一次時間、時段型每天挑點、到期判斷（單次、重複、時段型）與補跑規則。

## 不負責
- `schedule.fire`／`schedule.missed` 事件、每日執行上限、排程沒做完記 log（SCH-4；「卡住 20 分鐘的警示」已廢，S5，2026-11-02）、`job` 資料與管理 API、夜班 → `jobs`（業務模組）；安靜時段的計算 → `platform.Quiet`（`notify` 用）；各定時工作要做什麼 → 登記它的模組（`app.Jobs()` 只收集，`JobRunner` 只負責按時呼叫）。

## 介面
```go
package timer

type Cron struct{ /* 不匯出 */ }
func ParseCron(specs []string, loc *time.Location) (*Cron, error)
func (c *Cron) Next(after time.Time) time.Time
func NextRuns(specs []string, loc *time.Location, after time.Time, n int) ([]time.Time, error)

type Window struct{ Weekday, Weekend []string; PerDay, IdleMin int }
type Slot struct{ Start, End time.Time }
func (w *Window) Slots(id string, day time.Time) []Slot
func (w *Window) Next(id string, after time.Time, loc *time.Location) (Slot, bool)
func ValidWindows(ws []string) error

type Clock interface{ Now() time.Time }
type CronOpt struct{ Catchup, Agent string }
type Item struct{ ID, Kind, Agent, Catchup string; Data any; T *Entry }
type Handler interface {
    Missed(ctx context.Context, it *Item, d Due)
    Allow(ctx context.Context, now time.Time, it *Item) bool
    Fire(ctx context.Context, now time.Time, it *Item, d Due)
    AfterTick(ctx context.Context, now time.Time)
}
type Scheduler struct{ /* 不匯出 */ }
func NewScheduler(clock Clock) *Scheduler
func (s *Scheduler) SetHome(dir string)
func (s *Scheduler) SetHandler(h Handler)
func (s *Scheduler) Location() *time.Location
func (s *Scheduler) Start(ctx context.Context) error // 讀時區、開每秒一次的 tick

type JobRunner struct{ /* 不匯出 */ }
func NewJobRunner() *JobRunner
func (r *JobRunner) SetHome(dir string)
func (r *JobRunner) ConsumeJobs(jobs []app.Job) error // app.JobConsumer
func (r *JobRunner) Start(ctx context.Context) error   // 每項一條 goroutine
func (r *JobRunner) Stop(ctx context.Context) error
func (s *Scheduler) Stop(ctx context.Context) error
func (s *Scheduler) Cron(ctx context.Context, id string, specs []string, opt CronOpt) error
func (s *Scheduler) At(ctx context.Context, id string, t time.Time) error
func (s *Scheduler) Cancel(ctx context.Context, id string) error
func (s *Scheduler) Next(id string) (time.Time, bool)
func (s *Scheduler) Sync(kind string, fn func(prev map[string]*Item) map[string]*Item)
func (s *Scheduler) Tick(ctx context.Context, now time.Time)
func NewCronItem(id string, specs []string, loc *time.Location, opt CronOpt, now time.Time) (*Item, error)
func NewWindowItem(id string, w *Window, loc *time.Location, agent string, now time.Time) *Item

type Entry struct{ /* 不匯出 */ }
func NewAt(id string, t time.Time) *Entry
func NewCron(id string, c *Cron, now time.Time) *Entry
func NewWindow(id string, w *Window, now time.Time, loc *time.Location) *Entry
func (e *Entry) Next() time.Time
func (e *Entry) Window() *Window
func (e *Entry) CarryLast(prev *Entry)
func (e *Entry) MarkFired(at time.Time)
func (e *Entry) Due(now, lastTick time.Time, catchup bool, loc *time.Location) (Due, bool)
type Due struct{ At time.Time; Fire, Missed, CaughtUp bool; WindowEnd time.Time; Once bool }
```
`Entry` 不是並行安全的，呼叫者自己加鎖（`Scheduler` 在自己的鎖裡用）。

- Evidence: `internal/base/timer/scheduler.go` → `Scheduler`、`Tick`、`Sync`；測試 `internal/base/timer/scheduler_test.go` → `TestSchedulerCronFiresOncePerMinute`、`TestSchedulerAtFiresOnceThenRemoved`、`TestSchedulerAllowDenyDoesNotFire`、`TestSchedulerCatchup`、`TestSchedulerSyncReplacesOnlyOneKind`。
- Evidence: `internal/base/timer/cron.go` → `ParseCron`、`NextRuns`；`window.go` → `Window.Slots`、`Window.Next`、`ValidWindows`；`entry.go` → `Entry.Due`。

## 事件
None。

## 儲存
None（不存任何資料；`Entry` 只在記憶體）。

## 依賴
標準庫、`platform`（錯誤型別、家設定、logger）、`app`（`JobRunner` 吃的 `Job` 型別與 `JobConsumer` 介面）、第三方的 cron 解析函式庫（robfig/cron）。

## 錯誤
`ERROR_CODE_INVALID`（cron 空的或格式不對、時段格式不對），訊息是白話、不帶輸入。

## 不變條件
- 對一個 `* * * * *` 的項目，每過一分鐘恰好到期一次；同一分鐘再問不會再到期。
- 沒到期時 `Due` 不改任何狀態。
- `catchup` 的補跑只補最近一次，睡超過 6 小時一律不補。
- 同一天、同一個 `id`，時段型的觸發點每次算都一樣。
- `Cron.Next` 與 `NextRuns` 的第一筆永遠相同。

## 測試計畫
- `internal/base/timer/timer_test.go`（假時間，不開 goroutine）：`TestNextRunsMergesSpecsInOrder`、`TestParseCronRejectsBadInput`、`TestCronUsesGivenLocation`、`TestWindowSlotsAreStable`、`TestWindowWeekendOverride`、`TestValidWindows`、`TestWindowNextIsAfter`、`TestDueFiresOncePerMinute`、`TestDueSkipsMinuteAlreadyFired`、`TestDueCatchup`、`TestDueOnce`、`TestDueWindow`。
- 接上排程服務後的整體行為（每分鐘觸發、補跑、時段、額度、`schedule.fire` 事件）由 `internal/feature/tasks/job` 的測試守。

## 已知限制
- `app.Jobs()` 的內建定時工作還沒排進 `Scheduler`（目前 `app` 只收集）；`system.maintenance` 由 `jobs` 自己用 `Cron` 登記。
- 設計稿寫的 `timer.Spec`（定時工作的規格）還沒有：`app.Job` 先用自己的 `Name`／`Cron`／`Run`。
- 「安靜時段的計算」不在這裡：它一直是 `platform.Quiet`（SCH-N2），`scheduler` 沒有自己的實作可抽。
