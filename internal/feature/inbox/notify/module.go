// Package notify implements user notifications; see ai_docs/modules/inbox.md.
package notify

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/feature/inbox/push"
	"github.com/minglight/evergenie/sdk"
)

type Notifier interface {
	Send(context.Context, Notice) (Notice, error)
	Answer(context.Context, string, string, AnswerVia, string) (Notice, error)
	Share(context.Context, string) (Attachment, error)
	Prefs() Prefs
	Unread() int
}
type AnswerVia = sdk.AnswerVia
type Notice = *sdk.Notification
type Attachment = *sdk.ShareResponse
type Prefs = *sdk.NotifyPrefs
type BusSender interface {
	Publish(context.Context, string, map[string]any) error
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
}
type VaultStore interface {
	NextID(context.Context, string) (string, error)
	RegisterSchema(vault.Spec)
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
	Put(context.Context, vault.Record, string, string) (vault.Record, error)
	ArchiveNow(context.Context, string, string, string) error
}
type Clock interface{ Now() time.Time }

// TaskNudges 是「還在等你」提醒要記進任務單的部分（P7-6）：tasks 模組符合（notify 不 import tasks，NTF-N3）。
// TaskNudges 是任務單（tasks 業務模組）給 notify 的兩件事：「還在等你」的提醒記進單的 nudges（P7-6），
// 以及讀單的狀態（每分鐘收掉掛在已結束任務上的訊息，sweepClosedTasks）。notify 不直接讀 task 資料。
type TaskNudges interface {
	AddNudge(ctx context.Context, id string) error
	Get(ctx context.Context, id string) (*sdk.Task, error)
}

// PushChannel 是 Web Push 的傳送通道與訂閱端點（internal/feature/inbox/push 符合）：notify 決定要不要推、推什麼，
// 通道只管送出與管理端點。Send 回的錯誤只有 platform 錯誤是整理過的白話，其他一律不寫進 log、不回給呼叫者。
type PushChannel interface {
	Send(context.Context, push.Subscription, []byte) error
	PublicKey() (string, error)
	Subscriptions() ([]push.Subscription, error)
	Subscribe(push.Subscription) (int, error)
	Unsubscribe(string) (int, error)
}
type Deps struct {
	Bus   BusSender
	Vault VaultStore
	Push  PushChannel
	Clock Clock
}
type digestBucket struct {
	at      time.Time
	hours   int32
	notices []sdk.Notification
}
type Module struct {
	records    map[string]vault.Record
	d          Deps
	mu         sync.Mutex
	home       string
	config     *platform.Home
	prefs      Prefs
	log        *slog.Logger
	registered bool
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	buckets    map[string]*digestBucket
	lastSweep  time.Time // 上次照任務狀態補收訊息（DEV-26）
	// lastOrphanSweep：上次對帳「綁的那件事已經不在」的通知（DEV-49 S2，每 SweepEvery）。
	lastOrphanSweep time.Time
	watchers        map[chan *sdk.NotifyWatchResponse]context.Context
	tasks           TaskNudges
	missingTasks    map[string]bool             // 上一輪 sweepClosedTasks 找不到的單（連兩輪才收訊息）；持 m.mu
	viewers         map[string]viewer           // 看板正在看哪裡（P7-6）：只在記憶體，90 秒過期
	nudges          map[string]*nudgeEntry      // 還沒收掉的提醒（P7-6）：只在記憶體
	sources         map[string]app.NoticeSource // 登記過的通知來源（Start 時從 consumed 建好，之後不變）
	consumed        []app.NoticeSource          // app 組裝時交進來的通知來源（ConsumeNoticeSources）
	// systemAgents：例行報告算系統那道的秘書（settings/notify.toml [lanes] system_agents，D-47；沒寫用 defaultSystemAgents）。
	systemAgents []string
}

// ConsumeNoticeSources 收下各業務模組登記的「自己處理回答的通知來源」（app.NoticeConsumer；app 組裝時、Start 之前呼叫；只有這一份清單）。
// 沒收到就是沒有：所有通知的 source 都清掉、回答一律照一般規則轉回秘書。
func (m *Module) ConsumeNoticeSources(sources []app.NoticeSource) error {
	m.consumed = append([]app.NoticeSource(nil), sources...)
	return nil
}

var _ app.NoticeConsumer = (*Module)(nil)

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	p, err := defaults("standard")
	if err != nil {
		panic(err)
	}
	return &Module{records: map[string]vault.Record{}, d: d, prefs: p, systemAgents: defaultSystemAgents, buckets: map[string]*digestBucket{}, watchers: map[chan *sdk.NotifyWatchResponse]context.Context{}, viewers: map[string]viewer{}, nudges: map[string]*nudgeEntry{}}
}
func (m *Module) Name() string       { return "notify" }
func (m *Module) SetHome(dir string) { m.mu.Lock(); defer m.mu.Unlock(); m.home = dir }

// SetTasks 接上任務單的提醒計數（P7-6）：任務單比通知晚建，所以用 setter。沒接就不記次數。
func (m *Module) SetTasks(t TaskNudges) { m.mu.Lock(); defer m.mu.Unlock(); m.tasks = t }

// Start 訂閱事件；每秒的 Tick（到期的提醒、還在等你、安靜時段結束補送）是 Jobs() 登記的定時工作（2026-11-01 從自己的 ticker 改過來）。
func (m *Module) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	// 沒有推播通道就是組裝錯了：啟動就報錯，不默默當沒人訂閱（2026-10-31）。
	if m.d.Push == nil {
		return errors.New("通知模組沒有接上推播通道（Deps.Push），請檢查組裝")
	}
	if m.home == "" {
		return nil
	}
	if m.cancel != nil {
		return platform.Conflict("通知服務已啟動")
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		return err
	}
	h.Clock = m.d.Clock
	m.config = h
	m.log = platform.NewLogger(h, "notify")
	if err := m.loadPrefs(); err != nil {
		return err
	}
	if !m.registered {
		m.d.Vault.RegisterSchema(messageSpec())
		m.registered = true
	}
	m.sources = map[string]app.NoticeSource{}
	topics := []string{"system.alert", "task.changed", "reminder.changed", "bus.envelope.state", "session.approval.resolved", platform.TopicSessionTurnFailed, platform.TopicSessionEnded, platform.TopicSessionRecovered, platform.TopicSessionStuck, platform.TopicSessionUnstuck}
	for _, s := range m.consumed {
		m.sources[s.Name] = s
		if s.Changed != "" && !slices.Contains(topics, s.Changed) {
			topics = append(topics, s.Changed)
		}
	}
	run, cancel := context.WithCancel(ctx)
	events, err := m.d.Bus.Subscribe(run, topics...)
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
				if err := m.handle(run, ev); err != nil {
					m.warn("處理通知事件失敗")
				}
			}
		}
	}()
	return nil
}

// Jobs 登記每秒一次的 Tick；還沒啟動時跑是空操作。
func (m *Module) Jobs() []app.Job {
	return []app.Job{{Name: "notify.tick", Every: time.Second, Run: func(ctx context.Context) error {
		m.Tick(ctx, m.d.Clock.Now())
		return nil
	}}}
}

var _ app.JobProvider = (*Module)(nil)

func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	m.mu.Unlock()
	if cancel != nil {
		cancel()
		m.wg.Wait()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancel = nil
	for ch := range m.watchers {
		close(ch)
		delete(m.watchers, ch)
	}
	return nil
}
func (m *Module) recoverWorker() {
	if recover() != nil {
		m.warn("通知背景工作失敗")
	}
}
func (m *Module) warn(reason string) {
	if m.log != nil {
		m.log.Warn(reason)
	}
}
func (m *Module) publish(ctx context.Context, topic string, payload map[string]any) {
	if err := m.d.Bus.Publish(ctx, topic, payload); err != nil {
		m.warn("通知事件發布失敗")
	}
}
func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.cancel == nil {
		return platform.Unavailable("通知服務尚未啟動")
	}
	return nil
}

var _ Notifier = (*Module)(nil)
