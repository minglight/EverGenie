// internal/feature/system/charter/module.go — ai_docs/modules/system.md（規矩頁，D-44）。
package charter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// VaultStore 存版本與提案。
type VaultStore interface {
	RegisterSchema(vault.Spec)
	NextID(context.Context, string) (string, error)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
}

// Notifier 送「秘書提案修改規矩」的通知給主人，提案決定後收掉它（notify 模組）。
type Notifier interface {
	Send(context.Context, *sdk.Notification) (*sdk.Notification, error)
	Retire(ctx context.Context, ids []string, reason string) error
}

// Bus 在提案決定後把結果送回提案的秘書（走 D-5 的訊息佇列）。
// 不發自己的事件：看板靠 vault 的 rules_version／rules_proposal 變動更新，沒有別的模組要聽（憲法第 2 條）。
type Bus interface {
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
}

type Deps struct {
	Vault  VaultStore
	Notify Notifier // 可以是 nil（不送通知）
	Bus    Bus      // 可以是 nil（不回報給秘書）
	Clock  platform.Clock
}

const (
	// MaxTextBytes 是一份文件（或提案內容）的大小上限。
	MaxTextBytes = 200 * 1024
	// MaxPendingPerSender 是每位秘書同時待決的提案上限，避免洗版。
	MaxPendingPerSender = 10
	// MaxWhyRunes 是提案理由的長度上限。
	MaxWhyRunes = 2000
	// MaxNoteRunes 是主人寫的備註（存檔、還原、採用或退回）的長度上限：會寫進檔頭、也會傳給秘書。
	MaxNoteRunes = 500
)

type Module struct {
	d          Deps
	mu         sync.Mutex // 寫入（存檔、還原、提案、決定）一次一個：讀檔、比版本、寫檔、留版本要一起做完
	home       string
	loc        *time.Location
	log        *slog.Logger
	started    bool
	registered bool
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d, loc: time.UTC}
}

func (m *Module) Name() string { return "charter" }

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
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	m.loc = h.Location()
	m.log = platform.NewLogger(h, "rules")
	if !m.registered {
		m.d.Vault.RegisterSchema(versionSpec())
		m.d.Vault.RegisterSchema(proposalSpec())
		m.registered = true
	}
	m.started = true
	return nil
}

func (m *Module) Stop(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.started = false
	return nil
}

// ready：呼叫端持有 m.mu。
func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !m.started {
		return platform.Unavailable("規矩服務還沒啟動")
	}
	return nil
}

func (m *Module) warn(msg string) {
	if m.log != nil {
		m.log.Warn(msg)
	}
}

func (m *Module) now() time.Time { return m.d.Clock.Now().In(m.loc) }
