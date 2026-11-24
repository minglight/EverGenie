// Package bus owns envelope mailboxes and asynchronous event subscriptions.
package bus

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

type Bus interface {
	Reroute(context.Context, string, string) error
	Send(context.Context, Envelope) (Envelope, error)
	Get(context.Context, string) (Envelope, error)
	List(context.Context, string, ...Lifecycle) ([]Envelope, error)
	Watch(context.Context, string, string) (<-chan *sdk.BusWatchResponse, error)
	Transition(context.Context, string, Lifecycle, TransitionOpt) error
	Publish(context.Context, string, map[string]any) error
	Subscribe(context.Context, ...string) (<-chan Event, error)
	Sweep(context.Context, time.Time) (int, error)
}
type Lifecycle = sdk.Lifecycle
type Envelope = *sdk.Envelope
type Event = *sdk.BusSubscribeResponse
type TransitionOpt struct{ Reason, LastMessage string }
type VaultStore interface {
	NextID(context.Context, string) (string, error)
	Watch(context.Context, ...string) (<-chan *sdk.VaultWatchResponse, error)
}
type Clock interface{ Now() time.Time }
type Deps struct {
	Vault VaultStore
	Clock Clock
}
type Module struct {
	d       Deps
	life    sync.RWMutex
	home    string
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	boxes   map[string]*mailbox
	subs    map[*eventSub]struct{}
	watches map[*watchSub]struct{}
	idemMu  sync.Mutex
	idem    map[string]string
	log     *slog.Logger
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{d: d, boxes: map[string]*mailbox{}, subs: map[*eventSub]struct{}{}, watches: map[*watchSub]struct{}{}, idem: map[string]string{}}
}
func (m *Module) Name() string       { return "bus" }
func (m *Module) SetHome(dir string) { m.life.Lock(); defer m.life.Unlock(); m.home = dir }
func (m *Module) Start(ctx context.Context) error {
	m.life.Lock()
	defer m.life.Unlock()
	if m.home == "" {
		return nil
	}
	if m.ctx != nil {
		return platform.Conflict("訊息服務已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	h.Clock = m.d.Clock
	m.log = platform.NewLogger(h, "bus")
	if err := os.MkdirAll(platform.RuntimeDir(m.home, "queue"), 0700); err != nil {
		m.log.Error("create queue directory failed", "error", err)
		return platform.Internal("無法建立訊息目錄，請檢查磁碟")
	}
	if err := m.restore(); err != nil {
		return err
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	for _, b := range m.boxes {
		m.runBox(b)
	}
	if m.d.Vault != nil {
		changes, err := m.d.Vault.Watch(m.ctx)
		if err != nil {
			m.cancel()
			m.wg.Wait()
			m.ctx = nil
			return err
		}
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer m.recoverWorker()
			for {
				select {
				case <-m.ctx.Done():
					return
				case change, ok := <-changes:
					if !ok {
						return
					}
					if change == nil || change.Record == nil {
						continue
					}
					by := ""
					if change.By != nil {
						by = change.By.Name
					}
					if err := m.Publish(m.ctx, "vault.changed", map[string]any{"kind": change.Record.Kind, "id": change.Record.ID, "type": string(change.Type), "by": by}); err != nil && m.ctx.Err() == nil {
						m.log.Warn("forward failed")
					}
				}
			}
		}()
	}
	return nil
}
func (m *Module) Stop(ctx context.Context) error {
	m.life.Lock()
	defer m.life.Unlock()
	if m.cancel == nil {
		return nil
	}
	m.cancel()
	// All accepted mailbox requests have completed before the write lock is acquired.
	m.wg.Wait()
	m.mu.Lock()
	m.boxes = map[string]*mailbox{}
	m.subs = map[*eventSub]struct{}{}
	m.watches = map[*watchSub]struct{}{}
	m.mu.Unlock()
	m.ctx = nil
	m.cancel = nil
	return nil
}
func (m *Module) recoverWorker() {
	if recover() != nil && m.log != nil {
		m.log.Error("background worker failed")
	}
}
func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.ctx == nil || m.ctx.Err() != nil {
		return platform.Unavailable("訊息服務尚未啟動")
	}
	return nil
}

var _ Bus = (*Module)(nil)
