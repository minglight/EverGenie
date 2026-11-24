// internal/feature/trust/requests/module.go — ai_docs/modules/trust.md（REQ-167、REQ-168）。
package requests

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type VaultStore interface {
	RegisterSchema(vault.Spec)
	NextID(context.Context, string) (string, error)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
}
type Notifier interface {
	Send(context.Context, *sdk.Notification) (*sdk.Notification, error)
}

// Bus 是事件匯流排；Send 把結果訊息送回申請的秘書（走 D-5 的訊息佇列，寄件人一定收到結果）。
type Bus interface {
	Publish(context.Context, string, map[string]any) error
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
}

// Applier 把目前有效的 guidance 交給引擎（產生各帳號的原生設定）。失敗要回錯誤；實作是 engine 模組，由 cmd 組裝時接上。
type Applier interface {
	PushPolicy(context.Context) error
}
type Deps struct {
	Vault  VaultStore
	Notify Notifier
	Bus    Bus
	Engine Applier // 可以是 nil（沒接引擎時 guidance 只存著，不套用）；engine 模組（architecture.md 同層例外 policy→engine）
	Clock  platform.Clock
	Names  Names // 可以是 nil：秘書 id → 對外的名字（通知標題、影響說明寫名字，不寫 id；NTF-61）
}

// Names 查秘書現在對外的名字（底層 agent 的 PersonaName）。查不到就照寫 id。
type Names interface {
	PersonaName(ctx context.Context, agentID string) (string, error)
}

const (
	// MaxPendingPerSender 是每個送件人同時待審的上限，避免被洗版。
	MaxPendingPerSender = 10
	// MaxGuidanceRunes 是 guidance 一句話的長度上限。
	MaxGuidanceRunes = 300
)

type Module struct {
	d          Deps
	mu         sync.Mutex
	home       string
	loc        *time.Location
	log        *slog.Logger
	registered bool
	cancel     context.CancelFunc
	done       chan struct{}
	gen        int64 // 每次有效規則變動加一，進預覽指紋（呼叫端持有 mu）

	// am 只保護 actives：ActiveRules／ActiveGuidance 會在 Decide 持有 mu、呼叫引擎時被引擎回頭讀，所以不能共用 mu。
	am      sync.RWMutex
	actives []sdk.PolicyRequest // 目前套用中的申請
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d, loc: time.UTC}
}
func (m *Module) Name() string       { return "requests" }
func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.home == "" {
		return nil
	}
	if m.cancel != nil {
		return platform.Conflict("權限申請服務已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	h.Clock = m.d.Clock
	m.loc = h.Location()
	m.log = platform.NewLogger(h, "policy")
	if !m.registered {
		m.d.Vault.RegisterSchema(spec())
		m.registered = true
	}
	if err := m.reload(ctx); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	events, err := m.d.Bus.Subscribe(run, "notice.answered")
	if err != nil {
		cancel()
		return err
	}
	m.cancel = cancel
	m.done = make(chan struct{})
	done := m.done
	go func() {
		defer close(done)
		defer func() {
			if recover() != nil {
				m.warn("權限申請事件處理中斷")
			}
		}()
		for {
			select {
			case <-run.Done():
				return
			case ev, ok := <-events:
				if !ok {
					return
				}
				if err := m.handleAnswer(run, ev); err != nil && run.Err() == nil {
					m.warn("無法處理權限申請的通知答案")
				}
			}
		}
	}()
	return nil
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel, done := m.cancel, m.done
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	wait, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	select {
	case <-done:
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
		return nil
	case <-wait.Done():
		return platform.Unavailable("權限申請服務尚未停止，請稍後重試")
	}
}

func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.cancel == nil {
		return platform.Unavailable("權限申請服務尚未啟動")
	}
	return nil
}

func (m *Module) warn(reason string) {
	if m.log != nil {
		m.log.Warn(reason)
	}
}

// reload 從 vault 重建「套用中」的清單（呼叫端持有 m.mu）。
func (m *Module) reload(ctx context.Context) error {
	rs, err := m.d.Vault.List(ctx, kind, vault.Query{})
	if err != nil {
		return err
	}
	var list []sdk.PolicyRequest
	for _, r := range rs {
		if q := decode(r); q.Status == sdk.PolicyStatusApplied {
			list = append(list, *q)
		}
	}
	m.setActives(list)
	return nil
}

// 登記方法（app 的登記介面，實作哪個就登記哪個）：Routes 在 http.go，Commands 在 cli.go；policy 沒有首頁資料、健檢；
// 每分鐘的到期檢查與每週摘要是 Jobs() 登記的定時工作（events.go 的 Tick；2026-11-01 從自己的 ticker 改過來）。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)

// Jobs 登記每分鐘一次的 Tick：到期的權限自動失效、每週一的摘要。
func (m *Module) Jobs() []app.Job {
	return []app.Job{{Name: "policy.tick", Every: time.Minute, Run: func(ctx context.Context) error {
		m.Tick(ctx, m.d.Clock.Now())
		return nil
	}}}
}

var _ app.JobProvider = (*Module)(nil)
