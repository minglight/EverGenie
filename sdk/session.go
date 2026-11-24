package sdk

import (
	"context"
	"time"
)

// SessionService：常駐 session 的生命週期、討論串狀態、權限／問答的回送、聊天記錄。
// 狀態與轉移見 schema session。事件從哪來、怎麼偵測是引擎的事（sdk/engine.go 的 EngineHostService），
// 這裡完全不出現任何 AI 平台的專有名詞（D-21）。
type SessionAPI interface {
	List(ctx context.Context, req *SessionListRequest) (*SessionListResponse, error)
	Get(ctx context.Context, req *SessionGetRequest) (*SessionGetResponse, error)
	// Open 開一個 session（常駐、任務單／待辦專屬對話、排程臨時 session）。已存在就回現有的。
	Open(ctx context.Context, req *SessionOpenRequest) (*SessionOpenResponse, error)
	Close(ctx context.Context, req *SessionCloseRequest) (*SessionCloseResponse, error)
	// Rotate 重開 session 清掉 context（夜間整理之後，REQ-80）。
	Rotate(ctx context.Context, req *SessionRotateRequest) (*SessionRotateResponse, error)
	// Resolve 由看板或手機的按鈕呼叫：回送權限確認或問題的答案；核心再轉給正在等的引擎。
	Resolve(ctx context.Context, req *SessionResolveRequest) (*SessionResolveResponse, error)
	// Chat 讀聊天記錄（由引擎解析自己的對話記錄，不讀畫面，REQ-81）。since 之後的新訊息會持續串流。
	Chat(ctx context.Context, req *SessionChatRequest, send func(*SessionChatResponse) error) error
	// Watch 訂閱 session 狀態變化（看板的「忙／閒／卡住」）。
	Watch(ctx context.Context, req *SessionWatchRequest, send func(*SessionWatchResponse) error) error
	// ListThreads 回每個討論串的狀態摘要（討論串清單、標題欄、選單徽章用，REQ-142）。
	ListThreads(ctx context.Context, req *SessionListThreadsRequest) (*SessionListThreadsResponse, error)
	WatchThreads(ctx context.Context, req *SessionWatchThreadsRequest, send func(*SessionWatchThreadsResponse) error) error
	// MarkSeen 把這個討論串的已讀位置移到某則訊息（存在伺服器，各裝置一致，REQ-143）。
	MarkSeen(ctx context.Context, req *SessionMarkSeenRequest) (*SessionMarkSeenResponse, error)
	// Command 是主人對這段對話下的指令（看板輸入框的「/」選單，SES-59、D-45）：不當訊息、不進佇列，交給引擎直接執行。
	// 只有主人能下；清掉對話、整理成摘要在她忙或在等確認時拒絕。成功回這次的紀錄（也記在 Session.last_command）。
	Command(ctx context.Context, req *SessionCommandRequest) (*SessionCommandResponse, error)
}

// SessionCommand 是看板能下的對話指令（D-45）。用 EverGenie 自己的詞，平台上怎麼做由引擎決定（D-21）。
type SessionCommand string

const (
	SessionCommandClear   SessionCommand = "clear"   // 清掉這段對話重來（長期記憶與日誌還在）
	SessionCommandCompact SessionCommand = "compact" // 把前面的對話整理成摘要，騰出空間
	SessionCommandModel   SessionCommand = "model"   // 這段對話改用某個模型（arg＝模型名稱）
	SessionCommandEffort  SessionCommand = "effort"  // 改思考力度（arg＝low、medium、high、xhigh、max、auto）
	// SessionCommandInterrupt 叫她停下手上這一輪（跟在她畫面上按停止一樣）；只在她忙著時能下（在等確認要用拒絕），SES-62。
	SessionCommandInterrupt SessionCommand = "interrupt"
)

// SessionCommandNote 是一次成功的對話指令紀錄（看板在這一串畫一行「你下了指令：…」）。
type SessionCommandNote struct {
	Command SessionCommand `json:"command,omitempty"`
	Arg     string         `json:"arg,omitempty"`
	At      time.Time      `json:"at,omitempty"`
	By      string         `json:"by,omitempty"`
}

type SessionCommandRequest struct {
	Session string         `json:"session,omitempty"`
	Command SessionCommand `json:"command,omitempty"`
	Arg     string         `json:"arg,omitempty"`
	By      *Actor         `json:"by,omitempty"`
}

type SessionCommandResponse struct {
	Note *SessionCommandNote `json:"note,omitempty"`
}

type SessionKind string

const (
	SessionKindResident SessionKind = "resident"
	SessionKindRoom     SessionKind = "room"
	SessionKindTemp     SessionKind = "temp"
	// SessionKindClone 已併進 SessionKindWork（P7-5、D-33）：新的程式不再開 clone；留著讀舊的狀態檔與舊的 API 呼叫。
	SessionKindClone SessionKind = "clone"
	// SessionKindWork 是工作 session（P7-5、D-33）：一張任務單一個（Task 有單號，生命週期跟著單的狀態），
	// 或秘書傳話排太久時開的同一位秘書的另一個 session（Task 空，閒置就收掉）。
	SessionKindWork SessionKind = "work"
)

type SessionState string

const (
	SessionStateStopped  SessionState = "stopped"
	SessionStateStarting SessionState = "starting"
	SessionStateIdle     SessionState = "idle"
	SessionStateBusy     SessionState = "busy"
	SessionStateWaiting  SessionState = "waiting" // 卡在確認
	// 舊版的 "stuck"（疑似被中斷）2026-11-02 拿掉（S5）：核心不再猜卡住，狀態檔讀到當 idle，看板遇到畫灰。
	// SessionStateLimited：帳號額度用完（引擎回報 RATE_LIMITED，REQ-378）；不是卡住。LimitedUntil 是恢復時間（零值＝不知道）。
	SessionStateLimited SessionState = "limited"
)

type PendingKind string

const (
	PendingKindPermission PendingKind = "permission" // 要不要允許某個動作
	PendingKindQuestion   PendingKind = "question"   // 模型要問使用者問題
)

type QuestionOption struct {
	Label       string `json:"label,omitempty"`
	Description string `json:"description,omitempty"`
}

// Question 用 EverGenie 自己的詞彙描述一個問題；引擎負責轉成該平台的問答格式、再把答案轉回去。
type Question struct {
	Header        string           `json:"header,omitempty"` // 短標題
	Text          string           `json:"text,omitempty"`   // 題目全文（也是 Decision.answers 的 key）
	Options       []QuestionOption `json:"options,omitempty"`
	MultiSelect   bool             `json:"multi_select"`
	AllowFreeText bool             `json:"allow_free_text"`
}

// Pending 是 session 正在等使用者的那件事。
type Pending struct {
	Kind      PendingKind `json:"kind,omitempty"`
	ID        string      `json:"id,omitempty"`        // 核心產生，用來對應 Resolve
	Tool      string      `json:"tool,omitempty"`      // PERMISSION：動作名稱
	Summary   string      `json:"summary,omitempty"`   // PERMISSION：用白話說明要做什麼；QUESTION：問之前她說的那段話（可空）
	Questions []Question  `json:"questions,omitempty"` // QUESTION：題目
	Since     time.Time   `json:"since,omitempty"`
	// Native：核心等到逾時沒接手，這件事回到 session 自己的畫面上等（終端機或遠端存取上直接按）。
	// 看板照樣顯示、照樣可以回答——由引擎在那個畫面上替主人回答（EngineAPI.AnswerApproval，SES-48）。
	Native bool `json:"native,omitempty"`
}

// RemoteAccess：讓使用者用手機或別的裝置直接連進這個 session（引擎有 remote_access 能力才有）。
type RemoteAccess struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"` // <人名>-<機器>
	URL     string `json:"url,omitempty"`
}

type Session struct {
	Name    string      `json:"name,omitempty"`
	Agent   string      `json:"agent,omitempty"`
	Kind    SessionKind `json:"kind,omitempty"`
	Engine  string      `json:"engine,omitempty"`
	Account string      `json:"account,omitempty"`
	// Model 是這段對話現在用的模型（D-46）：引擎說得出的、主人用指令換的，或開的時候的設定；空＝不知道。核心只存不解讀。
	Model           string       `json:"model,omitempty"`
	State           SessionState `json:"state,omitempty"`
	StateSince      time.Time    `json:"state_since,omitempty"`
	Pending         *Pending     `json:"pending,omitempty"`
	EngineSessionID string       `json:"engine_session_id,omitempty"`
	// reserved 12;                     // 原本的欄位 12：引擎自己的記錄位置不進核心
	RemoteAccess  *RemoteAccess `json:"remote_access,omitempty"`
	LastEvent     string        `json:"last_event,omitempty"`
	LastEventAt   time.Time     `json:"last_event_at,omitempty"`
	LastTurnEndAt time.Time     `json:"last_turn_end_at,omitempty"`
	LastUserAt    time.Time     `json:"last_user_at,omitempty"`
	// Cwd 是這個 session 開在哪個資料夾（重新接回時沿用）。
	Cwd string `json:"cwd,omitempty"`
	// Attach 是從終端機接上這個 session 畫面的指令（引擎提供的一行文字，核心只存不解讀）；引擎沒有這個能力就空。
	Attach string `json:"attach,omitempty"`
	// Task 是工作 session 對應的任務單號（SEC-<n>，P7-5）；其他 session 空。
	Task string `json:"task,omitempty"`
	// QueuedAt 不是空的表示這個工作 session 在排隊等名額（每台 doing 的工作 session 超過 work_max，D-33）；開起來就清掉。
	QueuedAt time.Time `json:"queued_at,omitempty"`
	// LimitedUntil：state 是 limited 時，額度什麼時候恢復（引擎說的；零值＝不知道）。離開 limited 就清掉（REQ-378）。
	LimitedUntil time.Time        `json:"limited_until,omitempty"`
	Messages     []SessionMessage `json:"messages,omitempty"`
	// LastCommand 是主人最近一次成功下的對話指令（SES-59）；看板在這一串畫一行紀錄。
	LastCommand *SessionCommandNote `json:"last_command,omitempty"`
	// ModelLabel 是 model 給人看的名字（引擎給的，例如「X 9」；空＝引擎沒給，看板顯示 model 原字）。D-46
	ModelLabel string `json:"model_label,omitempty"`
	// Effort 是這段對話現在的思考力度（low、medium、high、xhigh、max、auto；空＝不知道）。來源同 Model（D-46）。
	Effort string `json:"effort,omitempty"`
}

// SessionMessage 是 Session.Messages 的一則。
type SessionMessage struct {
	ID        string    `json:"id,omitempty"`
	From      string    `json:"from,omitempty"`
	Body      string    `json:"body,omitempty"`
	Lifecycle Lifecycle `json:"lifecycle,omitempty"`
	// Reason：failed 是送不到的原因（白話）；interrupted 是這一輪沒做完的白話原因（SES-57，說不出就空）；其他狀態空。
	Reason string `json:"reason,omitempty"`
	// ReasonDetail：interrupted 時平台說的原話（多半是英文；看板收在「詳細」，不直接放畫面，DEV-156）；沒有就空。
	ReasonDetail string `json:"reason_detail,omitempty"`
	// ReasonKind：interrupted 是引擎報錯或帳號登出時的錯誤種類（auth、billing 要主人處理，看板不給「再說一次」）；其他空。
	ReasonKind EngineErrorKind `json:"reason_kind,omitempty"`
	// Interject 表示秘書在忙的時候送進去的（主人中途補話，SES-41）：她做完手上那一步才會看到。
	Interject bool      `json:"interject,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	// Wait 是主人的話還沒收到時，伺服器說得出的原因（送出後 3 秒規則，REQ-384）；空＝還在正常的 3 秒內、已經收到，或說不出原因（不猜，S5）。
	Wait MessageWait `json:"wait,omitempty"`
	// WaitUntil：wait 是 limited 時，她的額度什麼時候恢復（零值＝不知道，REQ-378）。
	WaitUntil time.Time `json:"wait_until,omitempty"`
}

// MessageWait 是主人的話還沒收到的原因（REQ-384）。
type MessageWait string

const (
	MessageWaitAnswer    MessageWait = "answer"    // 她在等你回答上面的問題（確認框開著），這句答完就送
	MessageWaitWaking    MessageWait = "waking"    // 她的對話停了或正在開，正在叫醒她
	MessageWaitBusy      MessageWait = "busy"      // 她正在收前一句（一次只貼一則），接著就送
	MessageWaitInterject MessageWait = "interject" // 她在忙，這句已經送達了
	MessageWaitLimited   MessageWait = "limited"   // 她的帳號額度用完，wait_until 恢復後就送（REQ-378）
	// MessageWaitRetrying：引擎說上一次沒貼進她的對話（paste／capture／enter），派送器在退避重試同一則（S5-2、SES-51）。
	MessageWaitRetrying MessageWait = "retrying"
	// 舊版的 "late"（3 秒說不出原因＝掉包）2026-11-02 拿掉（S5）：說不出原因就不給 wait，看板不猜。
)

// ReceiveWithin 是主人送出後一定要有交代的時間（作者 2026-10-30「最低 3 秒鐘就要更新到最新狀態」，REQ-384）。
const ReceiveWithin = 3 * time.Second

type SessionListRequest struct {
	Agent  string         `json:"agent,omitempty"`
	States []SessionState `json:"states,omitempty"`
}

type SessionListResponse struct {
	Sessions []Session `json:"sessions,omitempty"`
}

type SessionGetRequest struct {
	Name string `json:"name,omitempty"`
}

type SessionGetResponse struct {
	Session *Session `json:"session,omitempty"`
}

type SessionOpenRequest struct {
	Name         string      `json:"name,omitempty"` // 空表示常駐：用 agent 當名稱
	Agent        string      `json:"agent,omitempty"`
	Kind         SessionKind `json:"kind,omitempty"`
	Cwd          string      `json:"cwd,omitempty"`   // 專屬對話指定的 repo 或資料夾
	Topic        string      `json:"topic,omitempty"` // 任務單或待辦對話：第一句帶單號
	FirstMessage string      `json:"first_message,omitempty"`
	// By 是誰要開的（P7-5）：工作 session 超過上限時，主人開的才在討論串裡問，其他的直接排隊（D-33）。看板代理換成 owner(web)。
	By *Actor `json:"by,omitempty"`
	// Wake：主人打開聊天頁時叫醒她（REQ-386）。只對主要 session（name＝agent），而且秘書啟用、在這台、mode 是
	// resident 或 ondemand 才開；已經開著或不能叫醒的回現在的樣子（沒有記錄回 not_found），不報錯。
	Wake bool `json:"wake,omitempty"`
}

type SessionOpenResponse struct {
	Session *Session `json:"session,omitempty"`
}

type SessionCloseRequest struct {
	Name string `json:"name,omitempty"`
}

type SessionCloseResponse struct {
}

type SessionRotateRequest struct {
	Name string `json:"name,omitempty"`
}

type SessionRotateResponse struct {
	Session *Session `json:"session,omitempty"`
}

// Decision 是使用者對一個 Pending 的決定。權限用 allow／message；問題用 answers（key 是 Question.text，
// 單選＝選項 label、複選＝逗號加空格串起、自由輸入＝字串）。
type Decision struct {
	Allow   bool              `json:"allow"`
	Message string            `json:"message,omitempty"` // 拒絕的理由，會傳給模型
	Answers map[string]string `json:"answers,omitempty"`
}

type SessionResolveRequest struct {
	Session   string    `json:"session,omitempty"`
	PendingID string    `json:"pending_id,omitempty"`
	Decision  *Decision `json:"decision,omitempty"`
	By        *Actor    `json:"by,omitempty"`
}

type SessionResolveResponse struct {
	Applied bool `json:"applied"` // false：已經被人在別處先按了，或已不在等
}

type ChatKind string

const (
	ChatKindUser       ChatKind = "user"
	ChatKindAssistant  ChatKind = "assistant"
	ChatKindToolCall   ChatKind = "tool_call"
	ChatKindToolResult ChatKind = "tool_result"
	ChatKindInterrupt  ChatKind = "interrupt"
	// ChatKindNotice：平台自己塞進對話、不是人說的話（載入技能、額度通知、自己排的喚醒…）；Text 是引擎寫好的一行字。
	ChatKindNotice ChatKind = "notice"
)

// ChatMessage 是一則對話（攤平格式）。引擎內部用來通知模型的字串與非對話的記錄已被濾掉。
type ChatMessage struct {
	Uuid        string    `json:"uuid,omitempty"`
	Ts          time.Time `json:"ts,omitempty"`
	Kind        ChatKind  `json:"kind,omitempty"`
	Text        string    `json:"text,omitempty"`
	Tool        string    `json:"tool,omitempty"`
	ToolLabel   string    `json:"tool_label,omitempty"` // tool_call 給主人看的白話（例「讀檔」），由引擎翻（同 activity，SES-63、SES-65）；舊資料沒有，看板退回 tool
	CallID      string    `json:"call_id,omitempty"`    // 工具呼叫與結果的配對 id
	IsError     bool      `json:"is_error"`
	FromSession string    `json:"from_session,omitempty"` // 跨 session 訊息解封裝後的寄件者
	// Draft：秘書還在打、還沒寫進對話記錄的那段話（只在 follow 時送；沒有 uuid，Text 空＝收掉）。
	// 下一則正式的訊息到了就換掉，不算已讀、不進未讀（DEV-150）。
	Draft bool `json:"draft,omitempty"`
}

type SessionChatRequest struct {
	Session   string `json:"session,omitempty"`
	SinceUuid string `json:"since_uuid,omitempty"` // 空表示從頭
	Follow    bool   `json:"follow"`
}

type SessionChatResponse struct {
	Message *ChatMessage `json:"message,omitempty"`
}

type SessionWatchRequest struct {
	Agent string `json:"agent,omitempty"` // 空表示全部
}

type SessionWatchResponse struct {
	Session *Session `json:"session,omitempty"`
}

// ThreadStatus 是討論串「現在最該讓人知道的一件事」。同時成立多個時依下面的優先順序取一個
// （WAITING_USER > LIMITED > WORKING > QUEUED > DONE > STARTING > IDLE > STOPPED）；
// 其餘資訊放在 ThreadSummary 的其他欄位。
type ThreadStatus string

const (
	ThreadStatusStopped     ThreadStatus = "stopped" // 沒在跑
	ThreadStatusStarting    ThreadStatus = "starting"
	ThreadStatusIdle        ThreadStatus = "idle"         // 閒著，而且你都看過了
	ThreadStatusQueued      ThreadStatus = "queued"       // 你的訊息還沒被秘書收到（排隊或已通知）；或工作 session 排隊等名額（waiting 帶 work_queue，D6）
	ThreadStatusWorking     ThreadStatus = "working"      // AI 處理中，activity 說明正在做什麼
	ThreadStatusWaitingUser ThreadStatus = "waiting_user" // 要你回覆（黃點）
	ThreadStatusDone        ThreadStatus = "done"         // AI 回合結束，你還沒看（完成標記）
	// 舊版的 "stuck"（可能被中斷）2026-11-02 拿掉（S5）：看板遇到舊版那台送來的畫灰。
	// ThreadStatusLimited：她的帳號額度用完，等恢復（REQ-378）；看板灰色「額度用完，HH:MM 恢復」（limited_until）。不算卡住、不算要你回覆。
	ThreadStatusLimited ThreadStatus = "limited"
)

// WaitingReason 說明 WAITING_USER 是在等什麼；可以同時有多個。
type WaitingReason string

const (
	WaitingReasonPermission WaitingReason = "permission" // 要不要允許某個工具
	WaitingReasonQuestion   WaitingReason = "question"   // 模型的問題
	WaitingReasonApproval   WaitingReason = "approval"   // 這個討論串有一則還沒回的「要你同意」通知
	WaitingReasonReminder   WaitingReason = "reminder"   // 這個討論串是某則待辦的討論／確認，還沒回
	// WaitingReasonWorkQueue：這張單的工作 session 在排隊等名額（P7-5、D-33）。不是在等主人：
	// 只有這個原因時討論串是 QUEUED、灰色、不算進 waiting_user（S5 D6，2026-11-02；舊版是 WAITING_USER）。
	WaitingReasonWorkQueue WaitingReason = "work_queue"
)

type ThreadSummary struct {
	Name            string          `json:"name,omitempty"` // session 名稱
	Agent           string          `json:"agent,omitempty"`
	Title           string          `json:"title,omitempty"` // 討論串標題（人看得懂的）
	Status          ThreadStatus    `json:"status,omitempty"`
	Waiting         []WaitingReason `json:"waiting,omitempty"`
	Activity        string          `json:"activity,omitempty"` // WORKING 時：正在跑的工具或「思考中」；其他狀態空
	StatusSince     time.Time       `json:"status_since,omitempty"`
	Unread          int32           `json:"unread"`                 // 已讀位置之後 AI 新說的訊息數
	Queued          int32           `json:"queued"`                 // 你送出、還沒被收的訊息數
	BackgroundTasks int32           `json:"background_tasks"`       // AI 回合結束後還在背景跑的工作數（來自引擎的 TURN_ENDED 事件）
	LastPreview     string          `json:"last_preview,omitempty"` // 最後一則 AI 訊息的前 80 字（只用於清單，不含工具輸出）
	LastAssistantAt time.Time       `json:"last_assistant_at,omitempty"`
	SeenUuid        string          `json:"seen_uuid,omitempty"` // 已讀位置
	// 工作 session（P7-5）：種類、單號，以及上一輪結束的時間（看板算 hot cache 倒數：上一輪結束起 60 分鐘）。
	Kind          SessionKind `json:"kind,omitempty"`
	Task          string      `json:"task,omitempty"`
	LastTurnEndAt time.Time   `json:"last_turn_end_at,omitempty"`
	// LimitedUntil：status 是 limited 時，額度什麼時候恢復（零值＝不知道，REQ-378）。
	LimitedUntil time.Time `json:"limited_until,omitempty"`
}

type SessionListThreadsRequest struct {
	Agent          string `json:"agent,omitempty"` // 空表示全部
	IncludeStopped bool   `json:"include_stopped"`
}

type SessionListThreadsResponse struct {
	Threads     []ThreadSummary `json:"threads,omitempty"`
	WaitingUser int32           `json:"waiting_user"` // 狀態是 WAITING_USER 的串數（選單徽章、分頁標題、PWA 徽章用）
}

type SessionWatchThreadsRequest struct {
	Agent string `json:"agent,omitempty"`
}

type SessionWatchThreadsResponse struct {
	Thread      *ThreadSummary `json:"thread,omitempty"`
	WaitingUser int32          `json:"waiting_user"`
}

type SessionMarkSeenRequest struct {
	Session string `json:"session,omitempty"`
	Uuid    string `json:"uuid,omitempty"` // 看到哪一則；空表示最新一則
	By      *Actor `json:"by,omitempty"`
}

type SessionMarkSeenResponse struct {
	Thread *ThreadSummary `json:"thread,omitempty"`
}
