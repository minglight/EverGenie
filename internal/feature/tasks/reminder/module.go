// internal/feature/tasks/reminder/module.go — ai_docs/modules/tasks.md（REQ-59, REQ-60, REQ-61, REQ-62, REQ-63, REQ-64, REQ-65, REQ-66, REQ-67, REQ-68, REQ-69, REQ-70, REQ-71, REQ-35, REQ-58）。
package reminder

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type Reminders interface {
	Create(ctx context.Context, in NewReminder) (Reminder, error)
	Update(ctx context.Context, id string, p Patch, by string) (Reminder, error)
	Snooze(ctx context.Context, id, until string, by string) (Reminder, error) // "3h"|"tomorrow"|"tomorrow-same"|"30m"|絕對時間
	Done(ctx context.Context, id, by string) (Reminder, error)
	Doing(ctx context.Context, id, by string) (Reminder, error)
	Cancel(ctx context.Context, id, by string) (Reminder, error)
	List(ctx context.Context, q Query) ([]Reminder, error)
	Get(ctx context.Context, id string) (Reminder, error)
}

type Reminder = *sdk.Reminder
type NewReminder = *sdk.RemindersCreateRequest
type Patch = *sdk.RemindersUpdateRequest
type Query = *sdk.RemindersListRequest
type AgentLookup interface {
	Resolve(ctx context.Context, nameOrAlias string) (string, bool)
}

// JournalWriter 是名冊（底層 agent）的「今天的日誌沒有這一行才寫」：主人完成待辦時寫進負責秘書的日誌（RMD-11）。
type JournalWriter interface {
	AppendJournalOnce(ctx context.Context, agent, line string) error
}
type NotifierAccess interface {
	Send(context.Context, *sdk.Notification) (*sdk.Notification, error)
}
type SchedulerAccess interface {
	At(ctx context.Context, id string, t time.Time) error
}
type BusSender interface {
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	Publish(ctx context.Context, topic string, payload map[string]any) error
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
}

// SessionAccess 是底層 session 的兩個通用能力：開（或接回）一個對話、登記「這個對話在等某件事」（討論串黃點）。
// 待辦要查證（follow: check）時開專屬對話請秘書去查，有專屬對話的待辦沒結束前那個對話顯示「等待辦」。
type SessionAccess interface {
	Open(context.Context, *sdk.SessionOpenRequest) (*sdk.Session, error)
	SetRoomWait(key, room string, reason sdk.WaitingReason)
}
type VaultStore interface {
	RegisterSchema(vault.Spec)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
	NextID(ctx context.Context, kind string) (string, error)
}
type Clock interface{ Now() time.Time }
type Deps struct {
	Agents    AgentLookup
	Notify    NotifierAccess
	Scheduler SchedulerAccess
	Bus       BusSender
	Vault     VaultStore
	Clock     Clock
	Sessions  SessionAccess // 沒接就不開查證對話、不登記等待（測試用）
	Journal   JournalWriter // 沒接就不寫「主人完成」日誌
}
type Module struct {
	log                   *slog.Logger
	cancel                context.CancelFunc
	done                  chan struct{}
	registered            bool
	d                     Deps
	home                  string
	config                *platform.Home
	reaskMinutes, maxAsks int
	checks                map[string]time.Time
	noticed               map[string]time.Time
	mu                    sync.Mutex
}

func New(d Deps) *Module             { return &Module{d: d} }
func (m *Module) Name() string       { return "reminder" }
func (m *Module) SetHome(dir string) { m.home = dir }

// SetSessions 接上 session（查證對話、登記等待）。塊的組裝先建子模組、session 要讀單與 Goal 所以建在塊之後，
// 由 registry 建好 session 再接（跟 tasks 的 SetWork 一樣；在 Start 之前呼叫）。
func (m *Module) SetSessions(s SessionAccess) { m.d.Sessions = s }
func (m *Module) Start(ctx context.Context) error {
	if m.cancel != nil {
		return platform.Conflict("服務已啟動")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.config = h
	h.Clock = m.d.Clock
	m.log = platform.NewLogger(h, "reminders")
	if !m.registered {
		m.d.Vault.RegisterSchema(reminderSpec())
		m.registered = true
	}
	return m.startEvents(ctx)
}
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

// 登記方法（app 的登記介面，實作哪個就登記哪個）：Routes 在 http.go，Commands 在 cli.go，Board 在下面。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.BoardProvider = (*Module)(nil)
)

// Board 登記首頁要的資料段：今天的待辦（總覽的 today_reminders）。前幾天就到期、還沒做完的排在最前面
// （舊的在前），後面是今天的（依時間）；不然逾期的事在首頁看不到（RMD-18）。
func (m *Module) Board() []app.BoardPart {
	return []app.BoardPart{{Key: "today_reminders", Load: func(ctx context.Context) (any, error) {
		today, err := m.List(ctx, &sdk.RemindersListRequest{Query: sdk.ReminderQueryToday})
		if err != nil {
			return nil, err
		}
		overdue, err := m.List(ctx, &sdk.RemindersListRequest{Query: sdk.ReminderQueryOverdue})
		if err != nil {
			return nil, err
		}
		todayRows := values(today)
		seen := map[string]bool{}
		for _, r := range todayRows {
			seen[r.ID] = true
		}
		out := []sdk.Reminder{}
		for _, r := range values(overdue) {
			if !seen[r.ID] {
				out = append(out, r)
			}
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
		sort.SliceStable(todayRows, func(i, j int) bool { return todayRows[i].At.Before(todayRows[j].At) })
		return append(out, todayRows...), nil
	}}}
}
