// internal/base/session/module.go — ai_docs/modules/session.md（REQ-72–81, REQ-30, REQ-142, REQ-143）。
package session

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

type State = sdk.SessionState
type ThreadSummary = *sdk.ThreadSummary
type OpenSpec = *sdk.SessionOpenRequest
type Session = *sdk.Session
type Decision = *sdk.Decision
type EngineEvent = *sdk.EngineEvent
type ApprovalRequest = *sdk.EngineHostRequestApprovalRequest
type ChatMessage = *sdk.ChatMessage

type Sessions interface {
	List(context.Context, string, []State) ([]Session, error)
	Get(context.Context, string) (Session, error)
	Threads(context.Context, string, bool) ([]ThreadSummary, int, error)
	MarkSeen(context.Context, string, string, string) (ThreadSummary, error)
	Open(context.Context, OpenSpec) (Session, error)
	OpenWork(ctx context.Context, id, agent string, by *sdk.Actor) (Session, error) // 開一張單（SEC／DEV）的工作 session（REQ-270、REQ-271）
	Close(context.Context, string) error
	Rotate(context.Context, string) error
	Resolve(context.Context, string, string, Decision, string) (bool, error)
	Command(ctx context.Context, name string, cmd sdk.SessionCommand, arg, by string) (*sdk.SessionCommandNote, error) // 主人的對話指令（SES-59）
	State(string) (State, bool)
	Chat(context.Context, string, string, bool) (<-chan ChatMessage, error)
	Watch(context.Context, string) (<-chan *sdk.SessionWatchResponse, error)
	WatchThreads(context.Context, string) (<-chan *sdk.SessionWatchThreadsResponse, error)
	EngineHost() sdk.EngineHostAPI
	Tick(context.Context, time.Time)
}
type Host interface {
	ReportEvent(context.Context, EngineEvent) error
	RequestApproval(context.Context, ApprovalRequest) (Decision, bool, error)
	GetContext(context.Context, string, string) (string, error)
	CheckAction(context.Context, string, sdk.ActionClass, string, string) (sdk.ActionVerdict, string, error)
}
type EngineAccess interface {
	Start(context.Context, *sdk.EngineStartRequest) (*sdk.EngineStartResponse, error)
	Stop(context.Context, string) error
	Deliver(context.Context, string, string, string) (bool, string, error)
	Transcript(context.Context, string, string, bool) (<-chan *sdk.ChatMessage, error)
	Ask(context.Context, *sdk.EngineAskRequest) (*sdk.EngineAskResponse, error) // 一次性對話（測口吻，P6-5b）
	// AnswerApproval：逾時後在畫面上等的確認，由引擎替主人回答（SES-48、ENG-14）。
	AnswerApproval(ctx context.Context, session string, p *sdk.Pending, d *sdk.Decision, by string) (bool, string, error)
	// Command：主人下的對話指令，引擎直接在 session 上執行（SES-59、ENG-17）。
	Command(ctx context.Context, session string, cmd sdk.SessionCommand, arg, by string) (*sdk.EngineCommandResponse, error)
}
type EngineRegistry interface {
	For(string) (EngineAccess, error)
}
type AgentLookup interface {
	List(context.Context) ([]*sdk.Record, error)
	Resolve(context.Context, string) (string, bool)
	PersonaName(context.Context, string) (string, error)
	Instructions(context.Context, string, string) (string, error)
	// 測口吻（P6-5b、REQ-130）：被測人設的資訊、換人設與說話層的指令、現在生效的說話層。
	ShowPersona(context.Context, string) (sdk.PersonaCard, string, string, error)
	OfficialCardFile(string) (string, bool) // 官方卡在哪：官方資料夾先、沒有才用跟著程式的（報告寫卡片位置用）
	TrialPersona(string, string) (string, string, error)
	TestInstructions(context.Context, string, string, string, string, []string) (string, error) // 最後一個是情境自己帶的梗本（nil＝照家裡的，DEV-164）
	StyleInfo(context.Context) (string, string, bool, []sdk.PersonaStyleLevel, error)
	Context(context.Context, string) (string, bool, error)
}
type NotifierAccess interface {
	Send(context.Context, *sdk.Notification) (*sdk.Notification, error)
	List(context.Context, []sdk.NotificationState, string, bool) ([]sdk.Notification, error)
}
type BusSender interface {
	Get(context.Context, string) (*sdk.Envelope, error)
	Transition(context.Context, string, sdk.Lifecycle, bus.TransitionOpt) error
	Reroute(context.Context, string, string) error
	Publish(context.Context, string, map[string]any) error
	Subscribe(context.Context, ...string) (<-chan *sdk.BusSubscribeResponse, error)
	Send(context.Context, *sdk.Envelope) (*sdk.Envelope, error)
	List(context.Context, string, ...sdk.Lifecycle) ([]*sdk.Envelope, error)
}
type VaultStore interface {
	Get(context.Context, string, string) (vault.Record, error)
	List(context.Context, string, vault.Query) ([]vault.Record, error)
}
type Clock interface{ Now() time.Time }

// TaskAccess 是工作 session 用到的任務單（P7-5、D-33）：讀單的狀態，把 engine_session 記回單上。
type TaskAccess interface {
	Get(ctx context.Context, id string) (*sdk.Task, error)
	Update(ctx context.Context, id string, p *sdk.TasksUpdateRequest, by string) (*sdk.Task, error)
}

// WorkNudger 是工作 session 在 waiting_user 第 50 分鐘「還在等你」的提醒掛勾（D-33）。
// P7-5 沒有設定時用 notify.Send 發一則一般的 approve（會進訊息列表、會推播）；P7-6 換成 notify.Nudge
// （只推手機、不建訊息、遵守安靜時段、依主人有沒有在看決定）。提醒的字由 session 自己組（2026-11-01 從 registry 的
// workNudger 搬回來），notify 的 Nudge 直接接得上，registry 不再轉手。
type WorkNudger interface {
	Nudge(ctx context.Context, req *sdk.NotifyNudgeRequest) (*sdk.Nudge, error)
}

// WorkNudge 是提醒的內容：哪張單、哪個工作 session、等了多久、在等什麼。
type WorkNudge struct {
	Agent, Session, Task, Title, Ask string
	WaitingSince                     time.Time
	URL                              string // 該 session 的聊天頁
}

// GoalsAccess 只用來判斷這一輪的動作要不要問主人；秘書之間的傳話不掛 Goal（2026-11-01 作者）。
type GoalsAccess interface {
	Decide(string, string, sdk.ActionClass) (sdk.ActionVerdict, string)
}
type Deps struct {
	Goals  GoalsAccess
	Engine EngineRegistry
	Agents AgentLookup
	Notify NotifierAccess
	Bus    BusSender
	Vault  VaultStore
	Clock  Clock
	// Tasks、Nudge：工作 session（P7-5）。Nudge 沒給就用 Notify 發一般通知。
	Tasks TaskAccess
	Nudge WorkNudger
}

const (
	// LimitRetryAfter：額度用完但引擎說不出恢復時間時，隔多久先試著補送一則（REQ-379）。
	LimitRetryAfter = 15 * time.Minute
	ApprovalTimeout = 10 * time.Minute
	// QuestionTimeout：選擇題等主人在看板回答多久才交回她的畫面（SES-66，作者 2026-11-03「不能在 genie 這邊回答」）。
	// 選擇題本來就是等主人，她怎樣都卡著；交回畫面後看板只能叫主人去她的對話答，所以等久一點。權限確認照舊 10 分鐘。
	QuestionTimeout = 12 * time.Hour
	WatchdogEvery   = 30 * time.Second
	// EngineReadyGrace 是 serve 剛起來時等引擎套件就緒的寬限：期間開不了常駐不寫 WARN，每 EngineRetryEvery 再試。
	EngineReadyGrace = 2 * time.Minute
	EngineRetryEvery = 2 * time.Second
)

type entry struct {
	turn                           turn
	lastOwner                      time.Time
	ownerTalk                      time.Time // 主人（寄件人對上是主人）最後一次在這個對話說話；收工作 session 用（DEV-147）
	latestInput                    EngineEvent
	lastAssistant                  *sdk.ChatMessage
	status                         sdk.ThreadStatus
	statusSince                    time.Time
	session                        sdk.Session
	record                         vault.Record
	order                          []string
	title, seen, activity          string
	activitySince, nowSeen, seenAt time.Time
	background                     int32
	idleClose                      time.Duration
	failedKind                     sdk.EngineErrorKind // 上一次 TURN_FAILED 的種類；之後一輪正常結束時發 session.recovered（S5-2）
	messages                       []*sdk.ChatMessage
	ended                          bool
	version                        uint64
	// 工作 session（P7-5）：workAsk＝上限的提問通知編號、nudgedFor＝已提醒過的那一輪等待（計時的起點）、
	// suspended＝主人手動關掉的（單回到 doing 之前不自動接回）、taskStatus＝最近一次看到的單狀態、opened＝最近開起來的時間。
	workAsk, nudgedFor, taskStatus string
	suspended                      bool
	opened                         time.Time
	commanding                     bool // 主人下的對話指令正在等引擎（SES-59，command.go）
	// interrupting：主人叫她停下、正在等引擎（SES-62）；turnStartedAt：這一輪開始的時間（停下之後的閒著提示比它早才算數）。
	interrupting  bool
	turnStartedAt time.Time
}
type approval struct {
	id        string
	deadline  time.Time
	result    chan approvalResult
	questions []sdk.Question
	notices   map[string]string
	answers   map[string]string
	// native：逾時（或引擎不等了）之後交回 session 自己的畫面上等（SES-48，native.go）；answering：正在請引擎代答。
	native, answering bool
}
type approvalResult struct {
	decision Decision
	decided  bool
}
type watch struct {
	ctx   context.Context
	agent string
	ch    chan *sdk.SessionWatchResponse
}
type threadWatch struct {
	ctx   context.Context
	agent string
	ch    chan *sdk.SessionWatchThreadsResponse
	last  map[string]sdk.ThreadSummary
}
type TrustLevels interface {
	LevelFor(platform.Trust, string) sdk.PermissionLevel
}
type DenialRecorder interface {
	AppendDenied(string, sdk.DeniedAction) error
}

type Module struct {
	trustLevels    TrustLevels
	denialRecorder DenialRecorder
	inFlight       map[string]bool
	typedOver      map[string]string    // session → 主人打字蓋過的選擇題編號（SES-70，同一題只收一次）
	mainReopen     map[string]time.Time // 主要 session 上次因為有話要給她而叫醒的時間（openMain）
	rerouting      map[string]bool      // 正在替它另開一個對話、開好要改送過去的封包（fallback；dispatchMu 底下）
	pendingPeers   map[*dispatchTurn]string
	dispatchMu     sync.Mutex
	// stuckEpisodes：話送不進去的每個對話「這一次」的記錄（watchStuck；dispatchMu 底下）。stuck：現在卡住的對話（stuckMu）。
	stuckEpisodes  map[string]*stuckEpisode
	forcePaste     map[string]bool       // 下一次交給引擎要「一定重貼」的封包（SES-76；dispatchMu 底下，只在記憶體）
	givingUp       map[string]bool       // 正在背景查對話記錄、等著收尾的封包（giveUp；dispatchMu 底下）
	voided         map[string]voidedTurn // 每個對話最近一次作廢的「已經在她的對話裡」那一輪（SES-76；dispatchMu 底下）
	bootAt         time.Time             // 對話服務這次啟動的時間（m.mu 底下）：計時從這之後才算
	stuckSwept     bool                  // 這次啟動後看過第一輪了（stuckMu）
	stuckMu        sync.Mutex
	stuck          map[string]platform.SessionStuck
	eventQueue     []EngineEvent
	turns          map[string]*dispatchTurn
	deliveryWake   chan struct{}
	dispatchConfig DispatchConfig
	toneMu         sync.Mutex      // 測口吻（tone.go）
	toneRunning    map[string]bool // 人設代號 → 正在跑
	completed      map[string]bool
	// modes／scheduledOn＝每個 watchdog 週期更新的秘書 mode 與「當天有沒有排程」，Tick 判斷隨選秘書要不要閒置收掉時只看它們。
	modes       map[string]string
	scheduledOn map[string]string
	scheduledFn func(ctx context.Context, agent string, day time.Time) bool
	modesAt     time.Time

	dirty      map[string]bool
	persist    chan struct{}
	writeMu    sync.Mutex
	writeState func(string, []byte) error

	answered            map[string]bool
	workAsks            map[string]string // 上限提問的通知編號 → 工作 session 名稱（P7-5）
	workAt              time.Time         // 上一次對帳工作 session 的時間
	workDirty           bool              // task.changed 來過，下一次 Tick 就對帳
	reading             map[string]bool   // 正在背景讀對話記錄算未讀的對話（refreshUnread；m.mu 底下）
	readFails           map[string]int    // 連續幾次沒讀到（log 降頻用；m.mu 底下）
	d                   Deps
	mu                  sync.Mutex
	operations          sync.Mutex // 開對話前的檢查與登記（名額、種類、記成開機中）；不拿著它等引擎
	locksMu             sync.Mutex
	sessionLocks        map[string]*sync.Mutex // 每個對話一把：開、關、重開（limits.go）
	limits              EngineLimits           // 等引擎的時限（limits.go）
	bgMu                sync.Mutex
	bgCount             int // 背景裡正在等引擎的事（spawn）；測試用它等背景做完
	tickMu              sync.Mutex
	home                string
	config              *platform.Home
	log                 *slog.Logger
	engineWait          time.Time // 第一次因引擎還沒就緒而開不了常駐的時間；成功開起來就清掉
	ctx                 context.Context
	cancel              context.CancelFunc
	wg                  sync.WaitGroup
	entries             map[string]*entry
	approvals           map[string]*approval
	notices             map[string]string
	owned               map[string]bool
	roomWaits           map[string]roomWait // 業務模組登記的「這個對話在等某件事」（SetRoomWait；例：待辦的專屬對話）
	envelopes           map[string]*sdk.Envelope
	watches             map[*watch]struct{}
	threadWatches       map[*threadWatch]struct{}
	dispatcher          chan EngineEvent
	refresh             chan struct{}
	transcriptDirty     map[string]bool
	watchdog, lastFlush time.Time
	accountsAt          time.Time // 上一次帳號巡檢（REQ-261）
	// 對話等待的推播（SES-47，waits.go）：waitPushed＝每個 session 已經推出去的那則（key）；
	// waitWake＝session 有變化就叫醒 waitLoop；waitMu 讓 syncWaits 一次只跑一個；
	// staleAsks＝排隊被取消時要收掉的名額詢問（放開 mu 之後由 retireAsks 收，S5-2）。
	waitMu     sync.Mutex
	waitPushed map[string]pushedWait
	waitWake   chan struct{}
	staleAsks  []string
	// 貼上的重試（REQ-391、S5-2）：tries＝引擎說沒送到幾次（決定退避多久）、retryAt＝下一次可以再試的時間、
	// dialogAt＝引擎上次說這個 session 的畫面開著確認框的時間。只決定多久再試，不判失敗。
	recvMu   sync.Mutex
	tries    map[string]int
	retryAt  map[string]time.Time
	dialogAt map[string]time.Time
}

func New(d Deps) *Module {
	if d.Clock == nil {
		d.Clock = platform.SystemClock{}
	}
	return &Module{inFlight: map[string]bool{}, typedOver: map[string]string{}, rerouting: map[string]bool{}, pendingPeers: map[*dispatchTurn]string{}, turns: map[string]*dispatchTurn{}, deliveryWake: make(chan struct{}, 1), dispatchConfig: DefaultDispatchConfig(), limits: DefaultEngineLimits(), reading: map[string]bool{}, readFails: map[string]int{}, sessionLocks: map[string]*sync.Mutex{}, toneRunning: map[string]bool{}, completed: map[string]bool{}, dirty: map[string]bool{}, persist: make(chan struct{}, 1), writeState: func(path string, data []byte) error { return platform.AtomicWrite(path, data, 0600) }, answered: map[string]bool{}, workAsks: map[string]string{}, d: d, entries: map[string]*entry{}, approvals: map[string]*approval{}, notices: map[string]string{}, owned: map[string]bool{}, roomWaits: map[string]roomWait{}, envelopes: map[string]*sdk.Envelope{}, watches: map[*watch]struct{}{}, threadWatches: map[*threadWatch]struct{}{}, dispatcher: make(chan EngineEvent, 256), refresh: make(chan struct{}, 1), transcriptDirty: map[string]bool{}, waitPushed: map[string]pushedWait{}, waitWake: make(chan struct{}, 1), tries: map[string]int{}, retryAt: map[string]time.Time{}, dialogAt: map[string]time.Time{}}
}
func (m *Module) Name() string       { return "session" }
func (m *Module) SetHome(dir string) { m.home = dir }

// SetScheduledToday 指定「這位秘書今天有沒有排程在這台跑」怎麼算（scheduler.ScheduledOn；session 不 import scheduler）。
// 隨選（ondemand）的秘書當天有排程就當天視為常駐（SES-1）；沒指定就都當沒有。
func (m *Module) SetScheduledToday(f func(ctx context.Context, agent string, day time.Time) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scheduledFn = f
}
func (m *Module) Start(ctx context.Context) error { return m.start(ctx, true) }
func (m *Module) start(ctx context.Context, ticker bool) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return platform.Conflict("對話服務已啟動")
	}
	if m.trustLevels == nil || m.denialRecorder == nil {
		m.mu.Unlock()
		return platform.Unavailable("信任能力尚未接上，無法啟動對話")
	}
	m.bootAt = m.d.Clock.Now()
	if m.home == "" {
		m.mu.Unlock()
		return nil
	}
	h, err := platform.LoadHome(m.home)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	h.Clock = m.d.Clock
	m.config = h
	m.log = platform.NewLogger(h, "session")
	m.modes, m.scheduledOn = map[string]string{}, map[string]string{}
	m.recoverToneRuns()
	if err = m.restore(); err != nil {
		m.mu.Unlock()
		return err
	}
	run, cancel := context.WithCancel(ctx)
	m.ctx = run
	m.cancel = cancel
	m.watchdog = m.d.Clock.Now().Add(-WatchdogEvery)
	m.mu.Unlock()
	events, err := m.d.Bus.Subscribe(run, "notice.sent", "notice.answered", "bus.envelope.state", "agent.changed")
	if err != nil {
		cancel()
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
		return err
	}
	if err = m.loadSources(run); err != nil {

		cancel()
		m.mu.Lock()
		m.cancel = nil
		m.mu.Unlock()
		return err

	}
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
					m.warn("處理對話事件失敗", err)
				}
			}
		}
	}()
	dispatch := m.dispatcher
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.recoverWorker()
		for {
			select {
			case <-run.Done():
				return
			case <-dispatch:
				m.drainEvents(run)
			}
		}
	}()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer m.recoverWorker()
		for {
			select {
			case <-run.Done():
				return
			case <-m.refresh:
				m.refreshUnread(run)
			}
		}
	}()
	m.wg.Add(1)
	go m.waitLoop(run)
	m.wg.Add(1)
	go m.deliveryLoop(run)
	m.wg.Add(1)
	go m.persistLoop(run)
	if ticker {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			defer m.recoverWorker()
			t := time.NewTicker(time.Second)
			defer t.Stop()
			m.Tick(run, m.d.Clock.Now())
			for {
				select {
				case <-run.Done():
					return
				case <-t.C:
					m.Tick(run, m.d.Clock.Now())
				}
			}
		}()
	}
	return nil
}
func (m *Module) Stop(ctx context.Context) error {
	m.mu.Lock()
	cancel := m.cancel
	if cancel != nil {
		cancel() // 在 m.mu 底下取消：spawn 看得到「正在停」，不會在我們開始等之後才加一筆
	}
	m.mu.Unlock()
	if cancel == nil {
		return nil
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return platform.Unavailable("對話服務尚未停止，請稍後重試")
	}
	m.drainEvents(ctx)
	if err := m.flushDirty(); err != nil {
		m.warn("停止時寫入對話狀態失敗", err)
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, a := range m.approvals {
		select { // 交回畫面的確認（SES-48）已經送過結果了
		case a.result <- approvalResult{}:
		default:
		}
		delete(m.approvals, name)
	}
	for w := range m.watches {
		close(w.ch)
		delete(m.watches, w)
	}
	for w := range m.threadWatches {
		close(w.ch)
		delete(m.threadWatches, w)
	}
	m.cancel = nil
	return nil
}
func (m *Module) warn(msg string, err error) {
	if m.log != nil {
		m.log.Warn(msg, "error", err)
	}
}
func (m *Module) recoverWorker() {
	if recover() != nil {
		m.warn("對話背景工作失敗", platform.Internal("背景工作中斷"))
	}
}
func (m *Module) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.cancel == nil || m.ctx.Err() != nil {
		return platform.Unavailable("對話服務尚未啟動")
	}
	return nil
}
func (m *Module) publish(ctx context.Context, topic string, p map[string]any) {
	if err := m.d.Bus.Publish(ctx, topic, p); err != nil {
		m.warn("對話事件發布失敗", err)
	}
}

var _ Sessions = (*Module)(nil)
var _ Host = (*Module)(nil)

// SetTrust 接上等級判斷與拒絕紀錄；啟動前必須設定。
func (m *Module) SetTrust(levels TrustLevels, recorder DenialRecorder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trustLevels, m.denialRecorder = levels, recorder
}
