package sdk

import (
	"context"
	"time"
)

// 這份協定是「AI 平台邊界」（D-21）：跟 AI 平台有關的能力都在這裡切成介面，形狀照第一個引擎（目前唯一做的那個平台）有的能力來切，
// 用 EverGenie 自己的詞彙、不出現平台的專有名詞。留下的空格：engine_config（引擎自己的帳號設定，核心不解讀）、
// features（以後的能力旗標）。不為其他平台的差異做設計；別家的能力長得不一樣時，到時再擴充介面。
type EngineAPI interface {
	// Capabilities 回報引擎的名稱、版本與能力旗標（目前只是資訊，核心不依它改變行為）。
	Capabilities(ctx context.Context, req *EngineCapabilitiesRequest) (*EngineCapabilitiesResponse, error)
	// Start 啟動一個 session，並把核心給的指令交給模型；第一次啟動的對話框由引擎處理或登記成 BootPrompt。
	Start(ctx context.Context, req *EngineStartRequest) (*EngineStartResponse, error)
	Stop(ctx context.Context, req *EngineStopRequest) (*EngineStopResponse, error)
	// Deliver 由派送器把訊息原文送進 session。只在 session 閒著、
	// 沒有等待使用者的事時才會做；做不到回 delivered=false 並說明原因。
	Deliver(ctx context.Context, req *EngineDeliverRequest) (*EngineDeliverResponse, error)
	// Transcript 把引擎自己的對話記錄解析成 EverGenie 的 ChatMessage（不讀畫面）。
	Transcript(ctx context.Context, req *EngineTranscriptRequest, send func(*EngineTranscriptResponse) error) error
	// SetPermission 依政策產生這個平台的原生權限設定（D-7）。產物不准手改。
	SetPermission(ctx context.Context, req *EngineSetPermissionRequest) (*EngineSetPermissionResponse, error)
	// ListBootPrompts／AnswerBootPrompt：啟動時需要人選的對話框，從看板處理。
	ListBootPrompts(ctx context.Context, req *EngineListBootPromptsRequest) (*EngineListBootPromptsResponse, error)
	AnswerBootPrompt(ctx context.Context, req *EngineAnswerBootPromptRequest) (*EngineAnswerBootPromptResponse, error)
	// AnswerApproval：RequestApproval 逾時（decided=false）之後，那件事留在 session 自己的畫面上等；主人從看板回答時，
	// 核心請引擎在那個畫面上替主人回答（ENG-14）。畫面上已經沒有在等的確認回 answered=false、reason=AnswerNone；
	// 引擎不會代答這一種回 reason=AnswerUnsupported。怎麼操作畫面是引擎自己的事（D-21）。
	AnswerApproval(ctx context.Context, req *EngineAnswerApprovalRequest) (*EngineAnswerApprovalResponse, error)
	// Command：主人從看板對這段對話下的指令（清掉重來、整理成摘要、換模型、換思考力度；ENG-17、D-45）。不經訊息佇列、
	// 不寫封包：引擎直接在 session 上用平台自己的做法執行，確認平台收下了才回 done=true。session 不在回 reason="gone"；
	// 畫面上開著要人按的確認框回 CommandDialog；引擎不支援這個指令回 CommandUnsupported、沒有這個選項回 CommandBadArg。指令名稱與參數格式由核心檢查（SES-59），選項認不認得由引擎決定。
	Command(ctx context.Context, req *EngineCommandRequest) (*EngineCommandResponse, error)
	// Usage 回報帳號的用量與額度；warmup=true 時先送一則極小的請求，讓額度視窗從現在起算（REQ-86）。
	Usage(ctx context.Context, req *EngineUsageRequest) (*EngineUsageResponse, error)
	// WriteAgentCards 把秘書名冊寫成這個帳號在平台上「點名叫出來」用的檔案（evergenie agents link，Q3）。
	// DryRun 不寫任何檔，只回報會做什麼；只動引擎自己產生的檔案，不覆蓋別人的檔、不碰帳號本身的設定。
	WriteAgentCards(ctx context.Context, req *EngineWriteAgentCardsRequest) (*EngineWriteAgentCardsResponse, error)
	// Preflight 檢查這個帳號要跑起來需要的東西（平台的程式、登入）；只檢查，不安裝、不改設定（Q4）。
	Preflight(ctx context.Context, req *EnginePreflightRequest) (*EnginePreflightResponse, error)
	// Ask 做一次性對話：把指令與一則訊息交給模型，拿回文字回覆（P6-5b，測口吻用）。不開任何工具、不留 session、
	// 不影響常駐 session、不回報事件。不支援的引擎回 UNAVAILABLE「這個引擎不支援一次性對話」（Capabilities.Features["ask"] 表示支援）。
	Ask(ctx context.Context, req *EngineAskRequest) (*EngineAskResponse, error)
}

// EngineHostService 由核心實作，引擎套件呼叫：引擎把「平台上發生的事」轉成下面的正規化事件送回來，
// 核心不知道事件在平台上叫什麼。
type EngineHostAPI interface {
	CheckAction(context.Context, *EngineHostCheckActionRequest) (*EngineHostCheckActionResponse, error)
	// ReportEvent 只更新狀態，立刻回；不產生任何要給模型的內容。
	ReportEvent(ctx context.Context, req *EngineHostReportEventRequest) (*EngineHostReportEventResponse, error)
	// RequestApproval 請核心決定一個要使用者確認的事（權限或問題）；擋住直到使用者決定或逾時。
	// 逾時或核心不接手時 decided=false，引擎退回原本的確認方式（由使用者在 session 裡直接按）。
	RequestApproval(ctx context.Context, req *EngineHostRequestApprovalRequest) (*EngineHostRequestApprovalResponse, error)
	// GetContext 取得要附加給模型的內容（現況快照）；沒有新內容回空字串。
	GetContext(ctx context.Context, req *EngineHostGetContextRequest) (*EngineHostGetContextResponse, error)
}

type PermissionMode string

const (
	PermissionModeAsk  PermissionMode = "ask"  // 每次都問
	PermissionModeAuto PermissionMode = "auto" // 由引擎自己判斷是否放行
)

// PermissionLevel 是一位秘書的權限等級（settings/trust.toml 的 level；Q5）。引擎自己決定怎麼對應到平台的原生設定。
type PermissionLevel string

const (
	PermissionLevelFull     PermissionLevel = "full"     // 最高：可以自己改系統與 repo，由引擎自己判斷是否放行
	PermissionLevelStandard PermissionLevel = "standard" // 一般：小事自己做，大事（推送、刪除、執行指令）先問主人
	PermissionLevelReadOnly PermissionLevel = "readonly" // 唯讀：只能看，不能改
)

type EngineCapabilitiesRequest struct {
}

type EngineCapabilitiesResponse struct {
	Engine   string          `json:"engine,omitempty"` // 引擎 id（由引擎套件自己決定；只用於顯示與登記，核心不依此分支）
	Version  string          `json:"version,omitempty"`
	Features map[string]bool `json:"features,omitempty"` // 空格：以後的能力旗標；目前核心不讀
}

type EngineStartRequest struct {
	Name             string          `json:"name,omitempty"` // session 名稱
	Agent            string          `json:"agent,omitempty"`
	Cwd              string          `json:"cwd,omitempty"`
	Account          string          `json:"account,omitempty"`       // 對照 home.toml 的 accounts.<名>
	EngineConfig     map[string]any  `json:"engine_config,omitempty"` // home.toml 的 accounts.<名>.engine_config，核心不解讀，引擎自己看
	Model            string          `json:"model,omitempty"`
	PermissionLevel  PermissionLevel `json:"permission_level,omitempty"` // 這位秘書的權限等級；空表示用引擎自己的預設
	Rules            []PolicyRule    `json:"rules,omitempty"`            // 主人核准過、現在有效的規則（權限變更申請，Q7）；只在開 session 時生效
	Effort           string          `json:"effort,omitempty"`           // 思考力度的提示（低／中／高），引擎自己對應；不支援就忽略
	RemoteAccess     bool            `json:"remote_access"`
	RemoteAccessName string          `json:"remote_access_name,omitempty"`
	Instructions     string          `json:"instructions,omitempty"`      // agent 模組組裝好的指令；引擎用該平台的方式交給模型
	ResumeSessionID  string          `json:"resume_session_id,omitempty"` // 空表示新的 context
	FirstMessage     string          `json:"first_message,omitempty"`
	HostEndpoint     string          `json:"host_endpoint,omitempty"` // 核心的 EngineHostService 位址（unix socket）；引擎用它回報事件
}

type EngineStartResponse struct {
	EngineSessionID string `json:"engine_session_id,omitempty"` // 引擎的 session id（不透明字串）
	RemoteAccessURL string `json:"remote_access_url,omitempty"`
	Attach          string `json:"attach,omitempty"` // 從終端機接上這個 session 畫面的指令（一行文字）；沒有這個能力就空
	// Model、ModelLabel、Effort：這次開起來實際用的模型、給人看的名字、思考力度（D-46）。引擎說得出才填；
	// 空的欄位核心沿用開的時候的設定（EngineStartRequest 的 model、effort）。
	Model      string `json:"model,omitempty"`
	ModelLabel string `json:"model_label,omitempty"`
	Effort     string `json:"effort,omitempty"`
}

type EngineStopRequest struct {
	Name string `json:"name,omitempty"`
}

type EngineStopResponse struct {
}

type EngineDeliverRequest struct {
	Text       string `json:"text,omitempty"`
	Session    string `json:"session,omitempty"`
	EnvelopeID string `json:"envelope_id,omitempty"`
	// Force：這次一定要重新貼一次，不接受「已經在對話裡」（不回 DeliverAlready）。核心等過那句、對話卻一直沒有接著做時才帶（DEV-162）。
	// 其他檢查（確認框、還留在輸入框就只補 Enter）照舊。
	Force bool `json:"force,omitempty"`
	// Resend：核心以前把這一則貼出去過（現在是重送）。引擎不記得自己貼過什麼（行程重開過）時，只有帶這個才去對話記錄找
	// 「是不是已經貼進去了」；沒帶的是第一次送，一律照常貼。
	Resend bool `json:"resend,omitempty"`
}

type EngineDeliverResponse struct {
	Delivered bool   `json:"delivered"`
	Reason    string `json:"reason,omitempty"` // delivered=false 時：busy、waiting、stopped…；delivered=true 時可以是 DeliverAlready
}

// DeliverAlready：同一個 envelope_id 再送一次是「重查」（REQ-384、ENG-13）：引擎先看上一次的到了沒，
// 已經在對話裡就回 delivered=true、reason=already，不再貼一次。
const DeliverAlready = "already"

type EngineTranscriptRequest struct {
	Session   string `json:"session,omitempty"`
	SinceUuid string `json:"since_uuid,omitempty"`
	Follow    bool   `json:"follow"`
}

type EngineTranscriptResponse struct {
	Message *ChatMessage `json:"message,omitempty"`
}

type RepoPolicy struct {
	Path    string   `json:"path,omitempty"`
	Remote  string   `json:"remote,omitempty"`
	Push    bool     `json:"push"`
	Writers []string `json:"writers,omitempty"` // 可以寫的職位 id
}

// PermissionPolicy 用 EverGenie 自己的詞彙（D-7）；來源是 settings/trust.toml 與 home.toml。
type PermissionPolicy struct {
	Mode  PermissionMode `json:"mode,omitempty"`
	Repos []RepoPolicy   `json:"repos,omitempty"` // 可寫的範圍
	Allow []string       `json:"allow,omitempty"` // 額外放行的動作
	Deny  []string       `json:"deny,omitempty"`  // 這個家不開放的東西
	// Guidance 是主人核准過的「一句話」（權限變更申請的 guidance，Q7）：交給平台的自動判斷，影響這個帳號的所有秘書。
	Guidance []string `json:"guidance,omitempty"`
}

type EngineSetPermissionRequest struct {
	Policy  *PermissionPolicy `json:"policy,omitempty"`
	Account string            `json:"account,omitempty"` // 產生給哪個帳號；空表示全部
	DryRun  bool              `json:"dry_run,omitempty"` // true：不寫任何檔，只比對現有產物跟政策一不一致（doctor 用）
}

type EngineSetPermissionResponse struct {
	Artifacts   []string  `json:"artifacts,omitempty"` // 產生（或更新）的檔案路徑；DryRun 時是空的
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	Drift       []string  `json:"drift,omitempty"` // 現有產物跟政策不一致的地方，一條一句白話；空表示一致（只有 DryRun 會填）
}

type BootPrompt struct {
	ID      string   `json:"id,omitempty"`
	Session string   `json:"session,omitempty"`
	Title   string   `json:"title,omitempty"`
	Choices []string `json:"choices,omitempty"`
}

type EngineListBootPromptsRequest struct {
	Session string `json:"session,omitempty"` // 空表示全部
}

type EngineListBootPromptsResponse struct {
	Prompts []BootPrompt `json:"prompts,omitempty"`
}

type EngineAnswerBootPromptRequest struct {
	ID     string `json:"id,omitempty"`
	Choice string `json:"choice,omitempty"`
	By     *Actor `json:"by,omitempty"`
}

type EngineAnswerBootPromptResponse struct {
}

type EngineAnswerApprovalRequest struct {
	Session  string    `json:"session,omitempty"`
	Pending  *Pending  `json:"pending,omitempty"`  // 在等的那件事（種類、動作、摘要、題目）
	Decision *Decision `json:"decision,omitempty"` // 主人的決定（允許／拒絕＋理由、各題答案）
	By       *Actor    `json:"by,omitempty"`
}

type EngineAnswerApprovalResponse struct {
	Answered bool   `json:"answered"`
	Reason   string `json:"reason,omitempty"` // answered=false 時：AnswerNone、AnswerUnsupported、gone（session 不在）、keys（操作不了畫面）
}

// AnswerApproval 的 reason（ENG-14）。
const (
	AnswerNone        = "none"        // 畫面上已經沒有在等的確認（有人在別處答了，或被中斷了）
	AnswerUnsupported = "unsupported" // 這一種確認引擎還不會在畫面上代答
)

// EngineCommandRequest 是主人下的對話指令（ENG-17）：Command 是 SessionCommand 的值，Arg 是參數
// （model：模型名稱；effort：思考力度；clear、compact 沒有參數）。核心送來之前已經檢查過白名單與參數。
type EngineCommandRequest struct {
	Session string         `json:"session,omitempty"`
	Command SessionCommand `json:"command,omitempty"`
	Arg     string         `json:"arg,omitempty"`
	By      *Actor         `json:"by,omitempty"`
}

type EngineCommandResponse struct {
	Done bool `json:"done"`
	// Reason：done=false 時：CommandUnsupported、CommandBadArg、CommandDialog、CommandTyping、gone（session 不在）；
	// 其他（paste、capture、enter…）是沒送進去、或送了確認不到平台收下（核心不重試，交回給主人）。
	Reason string `json:"reason,omitempty"`
	// EngineSessionID：清掉重來之後平台換了新的對話編號時帶回來（核心記成 Session.engine_session_id，之後接回用新的）。
	EngineSessionID string `json:"engine_session_id,omitempty"`
	// Model、ModelLabel、Effort：換模型、換力度做成之後，引擎說得出的現在的值（D-46）。空的欄位核心用這次的參數（arg）；
	// model 換了而 ModelLabel 空，就清掉舊的名字（看板顯示 model 原字）。
	Model      string `json:"model,omitempty"`
	ModelLabel string `json:"model_label,omitempty"`
	Effort     string `json:"effort,omitempty"`
}

// Command 的 reason（ENG-17）。
const (
	CommandUnsupported = "unsupported" // 這個引擎不支援這個指令
	CommandDialog      = "dialog"      // 畫面上開著要人按的確認框：先回答再下指令
	CommandBadArg      = "bad_arg"     // 這個引擎沒有這個選項（模型名稱、力度等級由引擎認，核心只檢查格式）
	CommandTyping      = "typing"      // 她的輸入框裡有打到一半的字：不動它
)

type EngineUsageRequest struct {
	Account      string         `json:"account,omitempty"`
	EngineConfig map[string]any `json:"engine_config,omitempty"` // home.toml 的 accounts.<名>.engine_config，核心不解讀
	Warmup       bool           `json:"warmup,omitempty"`        // true：先送一則極小的請求讓額度視窗從現在起算，再回報用量（REQ-86）
}

type EngineUsageResponse struct {
	Account    string        `json:"account,omitempty"`
	UsedRatio  float64       `json:"used_ratio"` // 0–1，目前週期已用比例（各視窗取最高）
	ResetsAt   time.Time     `json:"resets_at,omitempty"`
	Windows    []UsageWindow `json:"windows,omitempty"`     // 各額度視窗；沒有資料是空的
	MeasuredAt time.Time     `json:"measured_at,omitempty"` // 這份用量資料的時間；零值表示沒有資料
	Warmed     bool          `json:"warmed,omitempty"`      // warmup=true 時：暖機請求成功
}

// UsageWindowKind 是額度視窗的種類。
type UsageWindowKind string

const (
	UsageWindowKindShort  UsageWindowKind = "short"  // 幾個小時的滾動視窗
	UsageWindowKindWeekly UsageWindowKind = "weekly" // 每週固定時間重置
)

// UsageWindow 是一個額度視窗的用量。引擎已經把「重置時間過了」換算好：過了就是 0、每週的下一次重置往後推。
type UsageWindow struct {
	Kind      UsageWindowKind `json:"kind,omitempty"`
	Hours     int32           `json:"hours"`      // 視窗長度（小時）
	UsedRatio float64         `json:"used_ratio"` // 0–1
	ResetsAt  time.Time       `json:"resets_at,omitempty"`
	Expired   bool            `json:"expired,omitempty"` // 資料上的重置時間已過，用量當 0（滾動視窗要等下一則訊息才開新的）
	Rolled    bool            `json:"rolled,omitempty"`  // 資料是舊的，重置時間是推算出來的（每週視窗）
}

type EnginePreflightRequest struct {
	Account      string         `json:"account,omitempty"`
	EngineConfig map[string]any `json:"engine_config,omitempty"`
}

type EnginePreflightResponse struct {
	Checks []PreflightCheck `json:"checks,omitempty"`
}

// PreflightCheck 是安裝前檢查的一項（evergenie preflight）。
type PreflightCheck struct {
	ID      string   `json:"id,omitempty"`   // 穩定的代號；同一個 id 在多個帳號重複時只列一次
	Name    string   `json:"name,omitempty"` // 給人看的名稱，例「git」「帳號 acme 已登入」
	Ok      bool     `json:"ok"`
	Detail  string   `json:"detail,omitempty"`  // 白話說現況
	Fix     string   `json:"fix,omitempty"`     // 白話說怎麼修；沒問題就空
	Install []string `json:"install,omitempty"` // 不需要密碼的安裝指令（CLI 只接受 brew install …）；--fix 才會跑
	// Login：帳號登入那一項才有。主人要在終端機親手（重新）登入時跑的指令：前面幾個 KEY=值 是環境變數，後面是程式與參數。
	// 只給「修復」小工具用（evergenie rescue --ask，主人選了才跑）；帳密在引擎自己的畫面輸入，EverGenie 不經手。
	Login []string `json:"login,omitempty"`
	// LoginHint：跑 Login 之前給主人看的幾行白話（畫面出來之後要做什麼、怎麼回來），一行一步。
	LoginHint []string `json:"login_hint,omitempty"`
}

// EngineAskRequest 是一次性對話（Ask）的請求。
type EngineAskRequest struct {
	Agent          string         `json:"agent,omitempty"`
	Cwd            string         `json:"cwd,omitempty"` // 秘書的工作資料夾；空表示家
	Account        string         `json:"account,omitempty"`
	EngineConfig   map[string]any `json:"engine_config,omitempty"` // home.toml 的 accounts.<名>.engine_config，核心不解讀
	Model          string         `json:"model,omitempty"`
	Effort         string         `json:"effort,omitempty"`       // 思考力度的提示，引擎自己對應；不支援就忽略
	Instructions   string         `json:"instructions,omitempty"` // agent 模組組裝好的指令
	Message        string         `json:"message,omitempty"`      // 使用者這一輪的訊息
	TimeoutSeconds int32          `json:"timeout_seconds"`        // 0＝600
}

type EngineAskResponse struct {
	Reply string `json:"reply,omitempty"` // 模型的文字回覆
	Model string `json:"model,omitempty"` // 實際用的模型；拿得到才填
}

type EngineWriteAgentCardsRequest struct {
	Account      string         `json:"account,omitempty"`
	EngineConfig map[string]any `json:"engine_config,omitempty"` // home.toml 的 accounts.<名>.engine_config，核心不解讀
	UserName     string         `json:"user_name,omitempty"`     // 使用者稱呼（home.toml 的 user.name），給說明文字用
	Cards        []AgentCard    `json:"cards,omitempty"`         // 名冊上啟用中的秘書；不在裡面的、引擎以前產生的檔案要拿掉
	DryRun       bool           `json:"dry_run,omitempty"`
}

type EngineWriteAgentCardsResponse struct {
	Files []AgentCardFile `json:"files,omitempty"`
	Note  string          `json:"note,omitempty"` // 整個帳號沒處理時的原因（例如帳號沒有設定位置）
}

type EngineEventType string

const (
	EngineEventTypeSessionStarted  EngineEventType = "session_started"
	EngineEventTypeSessionEnded    EngineEventType = "session_ended"
	EngineEventTypeTurnStarted     EngineEventType = "turn_started"     // 使用者（或核心）送了一則輸入，模型開始處理（必備）
	EngineEventTypeTurnEnded       EngineEventType = "turn_ended"       // 模型這一回合說完了（必備）
	EngineEventTypeActivityStarted EngineEventType = "activity_started" // 開始做一件事（用工具、跑指令…），activity 說明是什麼
	EngineEventTypeActivityEnded   EngineEventType = "activity_ended"
	EngineEventTypeIdleHint        EngineEventType = "idle_hint" // 閒置一段時間的提示
	// EngineEventTypeRateLimited：帳號的額度用完了，這一輪什麼都沒做（REQ-378）；Until 是平台說的恢復時間（認不出就空）。
	// 恢復後照常 TURN_STARTED／TURN_ENDED。
	EngineEventTypeRateLimited EngineEventType = "rate_limited"
	// EngineEventTypeTurnFailed：這一輪因為平台的錯誤沒做完就停了（不會再有這一輪的 TURN_ENDED；S5）。
	// Error 是錯誤種類（EngineErrorKind），LastMessage 是平台說的原話（給主人看，核心不解讀）。額度用完不走這個，照舊報 RATE_LIMITED。
	EngineEventTypeTurnFailed EngineEventType = "turn_failed"
)

// EngineErrorKind 是 TURN_FAILED 的錯誤種類，用 EverGenie 自己的詞彙；平台的錯誤怎麼歸到這幾種由引擎決定。
// 以後可能多出新的種類：核心與看板遇到不認得的值一律當 unknown。
type EngineErrorKind string

const (
	EngineErrorKindOverloaded EngineErrorKind = "overloaded" // 平台暫時忙不過來或暫時壞了：過一下再說一次通常就好
	EngineErrorKindAuth       EngineErrorKind = "auth"       // 登入失效或帳號不能用：要主人重新登入或處理帳號
	EngineErrorKindBilling    EngineErrorKind = "billing"    // 帳單或方案的問題：要主人處理
	EngineErrorKindRejected   EngineErrorKind = "rejected"   // 平台不收這一輪的請求（例如對話太長）：再說一次多半一樣，要先整理對話
	EngineErrorKindUnknown    EngineErrorKind = "unknown"    // 其他、說不出種類
)

// EngineNoPlatformMessage：平台出錯卻沒附任何說明時，引擎在 TURN_FAILED 的 LastMessage 填的固定一句
// （LastMessage 不能空，ENG-7）。它不是平台的原話：核心不把它當原話給主人看（看板「詳細」不顯示），
// 發生什麼事由錯誤種類的白話原因講，同一件事只講一遍（DEV-156 U-08）。
const EngineNoPlatformMessage = "AI 平台沒有附說明"

// Normalize 回列出來的種類；不認得的（含空的）一律回 unknown。核心與看板判斷種類前先過這個。
func (k EngineErrorKind) Normalize() EngineErrorKind {
	switch k {
	case EngineErrorKindOverloaded, EngineErrorKindAuth, EngineErrorKindBilling, EngineErrorKindRejected, EngineErrorKindUnknown:
		return k
	}
	return EngineErrorKindUnknown
}

// SessionEndReasonLoggedOut 是 SESSION_ENDED 的 reason「這個帳號登出了」（S5）：要主人重新登入，核心據此提醒主人。
// 平台自己的說法由引擎換成它；其他原因（stopped、gone、平台的說法）照填，核心不解讀。
const SessionEndReasonLoggedOut = "logged_out"

// EngineEvent 是正規化的事件；原始的平台資料留在引擎裡，不進核心。
type EngineEvent struct {
	Input  string      `json:"input,omitempty"`
	Source InputSource `json:"source,omitempty"`
	From   string      `json:"from,omitempty"`
	// Interrupted：TURN_STARTED 帶的是「上一輪被中斷了」；IDLE_HINT 帶的是「主人叫她停下了」（session.command interrupt，At＝按停止的時間，SES-62）。
	Interrupted     bool            `json:"interrupted,omitempty"`
	DirectMessage   *DirectMessage  `json:"direct_message,omitempty"`
	Session         string          `json:"session,omitempty"`
	Type            EngineEventType `json:"type,omitempty"`
	At              time.Time       `json:"at,omitempty"`
	Activity        string          `json:"activity,omitempty"`     // ACTIVITY_*：做的是什麼（工具或動作名稱）
	BackgroundTasks int32           `json:"background_tasks"`       // TURN_ENDED：回合結束後還在背景跑的工作數
	LastMessage     string          `json:"last_message,omitempty"` // TURN_ENDED：模型最後說的話（核心只截前 80 字做預覽）；TURN_FAILED：平台說的原話
	Reason          string          `json:"reason,omitempty"`       // SESSION_ENDED：原因（帳號登出用 SessionEndReasonLoggedOut）
	Until           time.Time       `json:"until,omitempty"`        // RATE_LIMITED：額度什麼時候恢復（認不出就是零值）
	Error           EngineErrorKind `json:"error,omitempty"`        // TURN_FAILED：錯誤種類（不認得的值當 unknown）
	// Model、ModelLabel、Effort：引擎看到的這段對話現在用的模型、給人看的名字、思考力度（任何事件都可以帶，D-46）。
	// 空＝這次沒看到，核心不改；Model 有值時 ModelLabel 跟著它換（空就清掉舊的名字）。
	Model      string `json:"model,omitempty"`
	ModelLabel string `json:"model_label,omitempty"`
	Effort     string `json:"effort,omitempty"`
	// EngineSessionID：SESSION_STARTED 時平台換了新的對話編號（主人在她畫面上清掉重來）；之後接回用這個。空＝沒換（SES-61）。
	EngineSessionID string `json:"engine_session_id,omitempty"`
}

type EngineHostReportEventRequest struct {
	Event *EngineEvent `json:"event,omitempty"`
}

type EngineHostReportEventResponse struct {
}

type ApprovalKind string

const (
	ApprovalKindPermission ApprovalKind = "permission" // 要不要允許某個動作
	ApprovalKindQuestion   ApprovalKind = "question"   // 模型要問使用者問題
)

type EngineHostRequestApprovalRequest struct {
	Session        string       `json:"session,omitempty"`
	Kind           ApprovalKind `json:"kind,omitempty"`
	Tool           string       `json:"tool,omitempty"`      // PERMISSION：要做的動作名稱
	Summary        string       `json:"summary,omitempty"`   // PERMISSION：用白話說明要做什麼；QUESTION：問之前她說的那段話（可空）。都是給使用者看
	Questions      []Question   `json:"questions,omitempty"` // QUESTION：題目
	MaxWaitSeconds int32        `json:"max_wait_seconds"`    // 引擎能等多久；核心不會超過
}

type EngineHostRequestApprovalResponse struct {
	Decided  bool      `json:"decided"` // false：逾時或核心不接手
	Decision *Decision `json:"decision,omitempty"`
}

type EngineHostGetContextRequest struct {
	Input   string `json:"input,omitempty"`
	Session string `json:"session,omitempty"`
}

type EngineHostGetContextResponse struct {
	Text string `json:"text,omitempty"` // 空：沒有新內容，引擎什麼都不要附加
}

type InputSource string

const (
	InputSourceTyped  InputSource = "typed"
	InputSourcePeer   InputSource = "peer"
	InputSourceSystem InputSource = "system"
)

type DirectMessage struct {
	To   string `json:"to,omitempty"`
	Text string `json:"text,omitempty"`
	OK   bool   `json:"ok,omitempty"`
}
type EngineHostCheckActionRequest struct {
	Session string      `json:"session,omitempty"`
	Class   ActionClass `json:"class,omitempty"`
	Tool    string      `json:"tool,omitempty"`
	Summary string      `json:"summary,omitempty"`
}
type EngineHostCheckActionResponse struct {
	Verdict ActionVerdict `json:"verdict,omitempty"`
	Reason  string        `json:"reason,omitempty"`
}
