// internal/feature/chat/module.go — ai_docs/modules/chat.md。
package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Sessions 是底層 session 提供的對話：開關、狀態、串流、授權與問答、討論串；EngineHost 是引擎行程回報事件的入口。
type Sessions interface {
	List(ctx context.Context, agent string, states []sdk.SessionState) ([]*sdk.Session, error)
	Get(ctx context.Context, name string) (*sdk.Session, error)
	Open(ctx context.Context, spec *sdk.SessionOpenRequest) (*sdk.Session, error)
	OpenWork(ctx context.Context, id, agent string, by *sdk.Actor) (*sdk.Session, error) // 一張單（SEC／DEV）的工作 session（REQ-270）
	Close(ctx context.Context, name string) error
	Rotate(ctx context.Context, name string) error
	Resolve(ctx context.Context, name, pendingID string, d *sdk.Decision, by string) (bool, error)
	Chat(ctx context.Context, name, since string, follow bool) (<-chan *sdk.ChatMessage, error)
	Watch(ctx context.Context, agent string) (<-chan *sdk.SessionWatchResponse, error)
	Threads(ctx context.Context, agent string, includeStopped bool) ([]*sdk.ThreadSummary, int, error)
	WatchThreads(ctx context.Context, agent string) (<-chan *sdk.SessionWatchThreadsResponse, error)
	MarkSeen(ctx context.Context, name, uuid, by string) (*sdk.ThreadSummary, error)
	Command(ctx context.Context, name string, cmd sdk.SessionCommand, arg, by string) (*sdk.SessionCommandNote, error) // 主人的對話指令（SES-59）
	EngineHost() sdk.EngineHostAPI
}

// Bus 是底層 bus 的傳話：封包的送、查、等，以及事件的發與訂。
type Bus interface {
	Send(ctx context.Context, e *sdk.Envelope) (*sdk.Envelope, error)
	Get(ctx context.Context, id string) (*sdk.Envelope, error)
	List(ctx context.Context, to string, states ...sdk.Lifecycle) ([]*sdk.Envelope, error)
	Watch(ctx context.Context, to, id string) (<-chan *sdk.BusWatchResponse, error)
	Publish(ctx context.Context, topic string, payload map[string]any) error
	Subscribe(ctx context.Context, topics ...string) (<-chan *sdk.BusSubscribeResponse, error)
}

// Tasks 只用來查一張單：開「<單號>」的工作 session 時決定由誰負責（CHT-2）。任務單是別的業務模組，由 registry 接。
type Tasks interface {
	Get(ctx context.Context, id string) (*sdk.Task, error)
}

// Roster 是健檢 sessions.resident 要的名冊清單（底層 agent 提供，registry 注入）。
type Roster interface {
	List(ctx context.Context) ([]*sdk.Record, error)
}

// observer 收「狀態延遲」的樣本（REQ-390）：引擎事件發生到推進看板串流花了多久。正式是自己的 latencyLog，測試換成假的。
type observer interface {
	Observe(at time.Time, took time.Duration)
}

type Clock interface{ Now() time.Time }

// Deps 的欄位都是使用方（這個套件）定義的小 interface，由 cmd/evergenie/registry.go 注入（MOD-2）。
// Roster、Clock 可以是 nil（健檢不列常駐秘書、用系統時鐘）。
type Deps struct {
	Sessions Sessions
	Bus      Bus
	Tasks    Tasks
	Roster   Roster
	Clock    Clock
}

// Module 沒有自己的資料與背景工作：HTTP 與指令都是底層 session、bus 的門面；Start 只開 logger。
type Module struct {
	d       Deps
	home    string
	log     *slog.Logger
	samples *latencyLog // 狀態延遲的樣本（串流寫、健檢讀）
	latency observer    // 串流把樣本交給誰：預設 samples，測試換掉
	voice   voiceWaits  // 語音問題等秘書簡短回答（CHT-10）
}

func New(d Deps) *Module {
	l := &latencyLog{}
	return &Module{d: d, samples: l, latency: l}
}
func (m *Module) Name() string       { return "chat" }
func (m *Module) SetHome(dir string) { m.home = dir }

func (m *Module) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.home == "" {
		return nil
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.log = platform.NewLogger(h, "chat")
	return nil
}

func (m *Module) Stop(ctx context.Context) error { return nil }

func (m *Module) now() time.Time {
	if m.d.Clock != nil {
		return m.d.Clock.Now()
	}
	return time.Now()
}

// 登記方法：Routes 在 http.go，Checks 在 checks.go，Commands 在 cli.go（套件層級函式）。
var (
	_ app.Module        = (*Module)(nil)
	_ app.RouteProvider = (*Module)(nil)
	_ app.CheckProvider = (*Module)(nil)
)
