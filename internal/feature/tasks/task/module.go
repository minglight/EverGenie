// internal/feature/tasks/task/module.go — ai_docs/modules/tasks.md（REQ-49, REQ-50, REQ-51, REQ-52, REQ-53, REQ-54, REQ-55, REQ-56, REQ-57, REQ-35, REQ-58）。
package task

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type Tasks interface {
	Create(ctx context.Context, in NewTask) (Task, error)
	Update(ctx context.Context, id string, p Patch, by string) (Task, error)
	Submit(ctx context.Context, id string, s Submission, by string) (Task, error) // 一般任務送驗收：result、verify、verify_how、proposed、links
	Accept(ctx context.Context, id, by string) (Task, error)                      // 只有使用者
	Archive(ctx context.Context, ids []string, by string) ([]string, error)       // 只有使用者：刪單＝封存（TSK-18）
	Reject(ctx context.Context, id, note, by string) (Task, error)                // 只有使用者
	Close(ctx context.Context, id string, c Closing, by string) (Task, error)     // 問答類或使用者
	Get(ctx context.Context, id string) (Task, error)
	List(ctx context.Context, q Query) ([]Task, error)
	Stale(ctx context.Context, now time.Time) ([]Task, error)                     // 停住的單
	Reconcile(ctx context.Context, agentID string, now time.Time) ([]Task, error) // 今晚要交代的單（不改狀態）
	Waiting(ctx context.Context, now time.Time) ([]WaitingTask, error)            // 等你的單＋等了幾天
	DueNudges(ctx context.Context, now time.Time) ([]Task, error)
	MarkNudged(ctx context.Context, id string, now time.Time) error
	AddNudge(ctx context.Context, id string) error // 工作 session 的「還在等你」提醒：nudges＋1（P7-6）
	Brief(ctx context.Context, id string) (string, error)
}

type Submission = *sdk.TasksSubmitRequest
type WaitingTask = sdk.WaitingTask
type Task = *sdk.Task
type NewTask = *sdk.TasksCreateRequest
type Patch = *sdk.TasksUpdateRequest
type Closing = *sdk.TasksCloseRequest
type Query = *sdk.TasksListRequest
type AgentLookup interface {
	Resolve(ctx context.Context, nameOrAlias string) (string, bool)
}

// NotifierAccess 是 notify 的送通知、列出還沒回的通知（看還有沒有別則掛在這張單上，noticeAnswered），
// 和收掉同一個問題的舊通知（Retire，via=auto；催的時候、主人回了其中一則的時候，retireOpen，TSK-30）。
type NotifierAccess interface {
	Send(context.Context, *sdk.Notification) (*sdk.Notification, error)
	List(ctx context.Context, states []sdk.NotificationState, agent string, archived bool) ([]sdk.Notification, error)
	Retire(ctx context.Context, ids []string, reason string) error
}
type BusSender interface {
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	Publish(ctx context.Context, topic string, payload map[string]any) error
}
type VaultStore interface {
	RegisterSchema(vault.Spec)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
	NextID(ctx context.Context, kind string) (string, error)
	ArchiveNow(ctx context.Context, kind, id, by string) error
}
type Clock interface{ Now() time.Time }
type SchedulerAccess interface {
	Cron(context.Context, string, []string, timer.CronOpt) error
}

// WorkSessions 是底層 session 的工作 session 能力（P7-5、SES-27）：單變成進行中時開那張單的專屬工作 session
// 並送單（AutoOpenWork；已經有、單不是進行中或負責人對不上就不動），單有任何變化時請 session 下一次 Tick 就對帳（RefreshWork）。
type WorkSessions interface {
	AutoOpenWork(ctx context.Context, id, agent, by string)
	RefreshWork()
}

type Deps struct {
	Scheduler SchedulerAccess
	Agents    AgentLookup
	Notify    NotifierAccess
	Bus       BusSender
	Vault     VaultStore
	Clock     Clock
	Work      WorkSessions // session 要讀單（它的 Deps.Tasks 接本模組），所以由 registry 在建好 session 之後用 SetWork 接；沒接就不開工作 session
}
type Module struct {
	log                            *slog.Logger
	cancel                         context.CancelFunc
	done                           chan struct{}
	registered                     bool
	d                              Deps
	home                           string
	loc                            *time.Location
	fmu                            sync.Mutex // 保護下面四個 [tasks] 設定值（REQ-148）
	staleDays, nudgeDays, nudgeMax int
	nudgeAt                        string
	mu                             sync.Mutex
	transitions                    map[transition]bool
}

func New(d Deps) *Module             { return &Module{d: d} }
func (m *Module) Name() string       { return "task" }
func (m *Module) SetHome(dir string) { m.home = dir }

// SetWork 接上 session 的工作 session 能力（session 跟 tasks 互相需要，所以這一邊用 setter；在 Start 之前呼叫）。
func (m *Module) SetWork(w WorkSessions) { m.d.Work = w }
func (m *Module) Stop(ctx context.Context) error {
	if m.cancel == nil {
		return nil
	}
	m.cancel()
	select {
	case <-m.done:
		m.cancel = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// 登記方法（app 的登記介面，實作哪個就登記哪個）：Routes 在 http.go，Commands 在 cli.go；
// 首頁的「等你處理的任務單」由 Board（board.go）登記；每天的跟催由模組自己的計時（events.go，timer.Scheduler 的使用者資料到期），不是 app 的 Jobs()。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
