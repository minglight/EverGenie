// internal/feature/tasks/job/module.go — 說明卡 ai_docs/modules/tasks.md（原 scheduler 卡；REQ-15, REQ-16, REQ-17, REQ-18, REQ-19, REQ-20, REQ-21, REQ-22, REQ-159）。
package job

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// BusSender 是 jobs 需要的 bus 能力：發 schedule.fire 等事件、聽 vault.changed 與 bus.envelope.state、
// 把排程內文封成 routine 封包送給秘書（Send）、讀封包看是哪個排程（Get）、每晚清理。
type BusSender interface {
	Publish(context.Context, string, map[string]any) error
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
	Get(context.Context, string) (*sdk.Envelope, error)
	Sweep(context.Context, time.Time) (int, error)
}

// JobRenderer 是名冊（底層 agent）套排程內文 slot 的能力：{{slot 名稱}} 換成這位秘書 addendum 裡的設定（Q1）。
type JobRenderer interface {
	RenderJob(ctx context.Context, agent, body string) (string, error)
}

// SessionRotator 是底層 session 的「重開對話清掉 context」：夜間整理（action: rotate）的封包回覆之後叫它（SES-9）。
type SessionRotator interface {
	Rotate(ctx context.Context, name string) error
}

// VaultStore 是 jobs 需要的 vault 能力。
type VaultStore interface {
	RegisterSchema(vault.Spec)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
	Patch(context.Context, string, string, map[string]any, []string, string) (vault.Record, error)
	Archive(context.Context, string, string, string) error
	ArchiveNow(ctx context.Context, kind, id, by string) error
	Move(ctx context.Context, kind, from, to string, set map[string]any, by string) (vault.Record, error)
	Sweep(context.Context, time.Time, bool) (vault.Report, error)
}

// Timer 是 jobs 需要的計時能力（registry 接 base/timer 的 Scheduler）：jobs 只決定「到期之後要發什麼事件、受不受額度限制」，
// 計時迴圈本身（cron 展開、補跑、單次）在底層 timer，tasks 與 reminders 也共用同一個。
type Timer interface {
	Cron(context.Context, string, []string, timer.CronOpt) error
	Next(string) (time.Time, bool)
	Location() *time.Location
	Sync(kind string, fn func(prev map[string]*timer.Item) map[string]*timer.Item)
	SetHandler(timer.Handler)
}

type Clock interface{ Now() time.Time }

type Deps struct {
	Bus   BusSender
	Vault VaultStore
	Timer Timer
	Clock Clock
	// Agents 套排程內文的 slot；沒接就用原本的內文。Sessions 給夜間整理做完後重開對話；沒接就不重開。
	Agents   JobRenderer
	Sessions SessionRotator
}

// jobKind 是 jobs 放進計時迴圈的項目種類；Sync 只換這一種，不碰 tasks／reminders 登記的項目。
const jobKind = "job"

// jobData 是一個 job 項目帶在 timer.Item.Data 裡的資料。
type jobData struct {
	action string
	body   string // job 的指令內文
}

type Module struct {
	d          Deps
	mu         sync.Mutex
	home       string
	log        *slog.Logger
	counts     map[string]int
	problems   []string // 最近一次 reload 沒辦法排上的工作（Problems）
	day        string
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	registered bool
	// runs 是每個排程最近一次的結果（runs.go，SCH-16）；有自己的鎖，跟 mu 沒有先後關係（拿著 runsMu 不拿別的鎖）。
	runsMu sync.Mutex
	runs   map[string]runState
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d, counts: map[string]int{}}
}
func (m *Module) Name() string       { return "job" }
func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

// SetSessions 接上 session（夜間整理做完重開對話）。塊的組裝先建子模組、session 建在塊之後，由 registry 建好 session 再接（在 Start 之前呼叫）。
func (m *Module) SetSessions(s SessionRotator) { m.mu.Lock(); defer m.mu.Unlock(); m.d.Sessions = s }

func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.home == "" {
		return nil
	}
	if m.cancel != nil {
		return platform.Conflict("排程服務已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.log = platform.NewLogger(h, "jobs")
	m.loadRuns()
	if !m.registered {
		m.d.Vault.RegisterSchema(jobSpec())
		m.registered = true
	}
	m.d.Timer.SetHandler(dueHandler{m})
	if err := m.reload(ctx, m.d.Clock.Now()); err != nil {
		return err
	}
	if err := m.d.Timer.Cron(ctx, maintenanceID, []string{"30 3 * * *"}, timer.CronOpt{}); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	events, err := m.d.Bus.Subscribe(run, "vault.changed", "bus.envelope.state")
	if err != nil {
		cancel()
		return err
	}
	m.cancel = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.recoverWorker()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				m.handle(run, ev)
			}
		}
	}()
	return nil
}

func (m *Module) recoverWorker() {
	if recover() != nil && m.log != nil {
		m.log.Error("jobs worker failed")
	}
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	m.wg.Wait()
	m.mu.Lock()
	m.cancel = nil
	m.mu.Unlock()
	return nil
}

func (m *Module) warn(message string, err error) {
	if m.log != nil {
		m.log.Warn(message, "error", err)
	}
}
