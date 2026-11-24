// internal/feature/tasks/goal/module.go — ai_docs/modules/tasks.md (REQ-152, REQ-153).
package goal

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type Goal = *sdk.Goal
type ProposeSpec struct {
	Title, Text, Criteria string
	Agent, By             string
}

// CreateSpec 是主人在看板直接開 Goal 的內容（D-39）；By 一定要是主人。
// Agent 是負責的秘書（職位 id 或名字）；空的＝不指定（agent 記成 By）。
type CreateSpec struct {
	Title, Text, Criteria string
	Agent                 string
	By                    string
}
type Goals interface {
	Propose(context.Context, ProposeSpec) (Goal, error)
	Create(context.Context, CreateSpec) (Goal, error)
	Get(context.Context, string) (Goal, error)
	List(context.Context, []sdk.GoalState) ([]Goal, error)
	UpdatePlanbook(context.Context, string, string, string, string) (Goal, error)
	Done(context.Context, string, string, string) (Goal, error)
	Withdraw(context.Context, string, string) (Goal, error)
	Decide(string, string, sdk.ActionClass) (sdk.ActionVerdict, string)
}
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
type Bus interface {
	Publish(context.Context, string, map[string]any) error
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
}
type Clock interface{ Now() time.Time }

// AgentLookup 把名字或別名對到名冊上的職位 id（主人開 Goal 指定負責秘書時用；沒接就不檢查）。
type AgentLookup interface {
	Resolve(ctx context.Context, nameOrAlias string) (string, bool)
}

// Mailer 是 bus 的送封包能力：主人開 Goal 指定負責秘書時，傳一則話給她（照現有的排隊、派送與叫醒；沒接就不傳）。
type Mailer interface {
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
}
type Deps struct {
	Vault  VaultStore
	Notify Notifier
	Bus    Bus
	Clock  Clock
	Agents AgentLookup
	Mail   Mailer
}
type Module struct {
	d          Deps
	mu         sync.Mutex
	home       string
	loc        *time.Location
	log        *slog.Logger
	alwaysAsk  []sdk.ActionClass
	registered bool
	cancel     context.CancelFunc
	done       chan struct{}
}

func defaultPolicy() []sdk.ActionClass {
	return []sdk.ActionClass{sdk.ActionClassPush, sdk.ActionClassDelete, sdk.ActionClassSendExternal, sdk.ActionClassSpend, sdk.ActionClassSettings}
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d, loc: time.UTC, alwaysAsk: defaultPolicy()}
}
func (m *Module) Name() string       { return "goal" }
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
		return platform.Conflict("Goal 服務已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	h.Clock = m.d.Clock
	m.loc = h.Location()
	m.log = platform.NewLogger(h, "goals")
	cfg := struct {
		AlwaysAsk []sdk.ActionClass `toml:"always_ask"`
	}{AlwaysAsk: defaultPolicy()}
	data, err := os.ReadFile(filepath.Join(m.home, "settings/goals.toml"))
	if err != nil && !os.IsNotExist(err) {
		return platform.Unavailable("無法讀取 Goal 授權設定")
	}
	if err == nil {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return platform.Invalid("always_ask", "Goal 授權設定格式不正確")
		}
	}
	for _, class := range cfg.AlwaysAsk {
		if !validClass(class) {
			return platform.Invalid("always_ask", "授權動作分類不正確")
		}
	}
	m.alwaysAsk = append([]sdk.ActionClass(nil), cfg.AlwaysAsk...)
	if !m.registered {
		m.d.Vault.RegisterSchema(goalSpec())
		m.registered = true
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
				m.warn("Goal 事件處理失敗")
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
				if err := m.handle(run, ev); err != nil && run.Err() == nil {
					m.warn("無法處理 Goal 確認")
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
		return platform.Unavailable("Goal 服務尚未停止，請稍後重試")
	}
}
func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.cancel == nil {
		return platform.Unavailable("Goal 服務尚未啟動")
	}
	return nil
}
func (m *Module) warn(reason string) {
	if m.log != nil {
		m.log.Warn(reason)
	}
}

var _ Goals = (*Module)(nil)

// 登記方法（app 的登記介面，實作哪個就登記哪個）：Routes 在 http.go，Commands 在 cli.go；goals 沒有首頁資料、健檢、定時工作。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
)
