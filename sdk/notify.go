package sdk

import (
	"context"
	"time"
)

// NotifyService：給使用者的通知。送不送、什麼時候送由 notify 依偏好決定，呼叫者只回報分類，
// 不能繞過（D-14、REQ-27）。欄位見 schema message（A 類）與 schema notify-prefs。
type NotifyAPI interface {
	Send(ctx context.Context, req *NotifySendRequest) (*NotifySendResponse, error)
	Get(ctx context.Context, req *NotifyGetRequest) (*NotifyGetResponse, error)
	List(ctx context.Context, req *NotifyListRequest) (*NotifyListResponse, error)
	MarkRead(ctx context.Context, req *NotifyMarkReadRequest) (*NotifyMarkReadResponse, error)
	// Archive 使用者在看板刪掉訊息（封存，不真的刪檔）；只有使用者本人（by 以 owner( 開頭）能做。
	// 還沒回答的「要你同意」不封存，放進 Skipped（秘書還在等答案）。
	Archive(ctx context.Context, req *NotifyArchiveRequest) (*NotifyArchiveResponse, error)
	// Answer 使用者按了按鈕或打字回答（REQ-30、REQ-139）：送回發問的 session，或執行訊息帶的動作。
	Answer(ctx context.Context, req *NotifyAnswerRequest) (*NotifyAnswerResponse, error)
	GetPrefs(ctx context.Context, req *NotifyGetPrefsRequest) (*NotifyGetPrefsResponse, error)
	SetPrefs(ctx context.Context, req *NotifySetPrefsRequest) (*NotifySetPrefsResponse, error)
	// Watch 訂閱未讀數與新通知（看板徽章、總覽頁，REQ-109）。
	Watch(ctx context.Context, req *NotifyWatchRequest, send func(*NotifyWatchResponse) error) error
	// Share 把秘書本機的一個檔案複製到可分享的位置，回傳 /share/<名字> 網址（聊天裡的圖片與下載連結，REQ-34）。
	Share(ctx context.Context, req *ShareRequest) (*ShareResponse, error)
	// PushKey 回 App 推播要用的公開金鑰與目前的訂閱數（看板「裝置推播」頁、健康檢查）；金鑰沒設定回 UNAVAILABLE。
	PushKey(ctx context.Context, req *NotifyPushKeyRequest) (*NotifyPushKeyResponse, error)
	// SubscribePush 記下這台裝置的推播訂閱（照 endpoint 新增或更新）；只有使用者本人（by 以 owner( 開頭）能管理。
	SubscribePush(ctx context.Context, req *NotifySubscribePushRequest) (*NotifySubscribePushResponse, error)
	// UnsubscribePush 照 endpoint 刪掉這台裝置的訂閱；沒有這筆也算成功。
	UnsubscribePush(ctx context.Context, req *NotifyUnsubscribePushRequest) (*NotifyUnsubscribePushResponse, error)
	// PushTest 只送一則測試通知給 endpoint 相同的那台裝置，不存檔、不進看板訊息。
	PushTest(ctx context.Context, req *NotifyPushTestRequest) (*NotifyPushTestResponse, error)
	Nudge(ctx context.Context, req *NotifyNudgeRequest) (*NotifyNudgeResponse, error)
	// Presence 看板在前景時每 30 秒回報主人正在看哪一頁（只存記憶體，90 秒過期），回傳目前要在這個畫面顯示的提醒；
	// 只有主人（by 以 owner( 開頭）能報。
	Presence(ctx context.Context, req *NotifyPresenceRequest) (*NotifyPresenceResponse, error)
	// ResolveNudge 主人在頁內卡片或站內提示按「知道了」：其他地方一起收掉（已經推播的用同一個 tag 覆蓋成「已處理」）。
	ResolveNudge(ctx context.Context, req *NotifyResolveNudgeRequest) (*NotifyResolveNudgeResponse, error)
}

// 核心模組自己處理回答的通知（Notification.Source）：回答不送回秘書的對話、不追蹤送達。
const (
	NotifySourceGoals  = "goals"  // 確認主人的 Goal
	NotifySourcePolicy = "policy" // 權限變更申請
)

// 訊息分兩道（Notification.Lane，D-47）：秘書跟主人說的話、問的事走 agent；系統自動的（系統提醒、套件、
// 排程與測試、部署報告）走 system。看板「訊息」分兩個分頁，選單徽章只算 agent 的未讀，外加 system 裡還要主人回答的。
const (
	NotifyLaneAgent  = "agent"
	NotifyLaneSystem = "system"
)

type Category string

const (
	CategoryApprove Category = "approve" // 要你同意
	CategoryDone    Category = "done"    // 事情做完
	CategoryDue     Category = "due"     // 待辦到期
	CategoryDigest  Category = "digest"  // 摘要
	CategoryOpening Category = "opening" // 秘書主動開場
)

type DeliveryMode string

const (
	DeliveryModeInstant DeliveryMode = "instant"
	DeliveryModeDigest  DeliveryMode = "digest"
	DeliveryModeBoard   DeliveryMode = "board" // 只放看板
	DeliveryModeOff     DeliveryMode = "off"
)

type Channel string

const (
	ChannelBoard Channel = "board"
	ChannelApp   Channel = "app"
	ChannelLine  Channel = "line"
)

type NotificationState string

const (
	NotificationStateUnread   NotificationState = "unread"
	NotificationStateRead     NotificationState = "read"
	NotificationStateAnswered NotificationState = "answered"
	NotificationStateExpired  NotificationState = "expired"
	NotificationStateFailed   NotificationState = "failed"
)

type AnswerVia string

const (
	AnswerViaButton AnswerVia = "button"
	AnswerViaChat   AnswerVia = "chat"
	AnswerViaAPI    AnswerVia = "api"
	AnswerViaAuto   AnswerVia = "auto"
)

type Delivery struct {
	Channel Channel   `json:"channel,omitempty"`
	At      time.Time `json:"at,omitempty"`
	Result  string    `json:"result,omitempty"`
}

// NotificationLink 是訊息上的一顆連結按鈕（DEV-115）。
type NotificationLink struct {
	Label string `json:"label,omitempty"` // 按鈕上的字
	URL   string `json:"url,omitempty"`   // 站內路徑（/ 開頭，不能是 // 開頭的外部網址）
}

type Answer struct {
	Text        string    `json:"text,omitempty"`
	At          time.Time `json:"at,omitempty"`
	Via         AnswerVia `json:"via,omitempty"`
	DeliveredAt time.Time `json:"delivered_at,omitempty"` // 空：還沒送進發問的 session（REQ-139）
}

type Notification struct {
	Source   string   `json:"source,omitempty"` // Empty for ordinary notices; system.alert is assigned only by notify; goals／policy mark their own (NotifySource*).
	ID       string   `json:"id,omitempty"`     // M-<n>
	Category Category `json:"category,omitempty"`
	Title    string   `json:"title,omitempty"`
	Context  string   `json:"context,omitempty"`
	Todo     string   `json:"todo,omitempty"`
	Options  []string `json:"options,omitempty"`
	URL      string   `json:"url,omitempty"`
	Image    string   `json:"image,omitempty"`
	Agent    string   `json:"agent,omitempty"`
	Account  string   `json:"account,omitempty"`
	Room     string   `json:"room,omitempty"`
	Task     string   `json:"task,omitempty"`
	Reminder string   `json:"reminder,omitempty"`
	Urgent   bool     `json:"urgent"`
	// Inline 是討論串內提問（P7-5、D-33）：只進看板與那個討論串（Room），不推播。
	Inline bool `json:"inline,omitempty"`
	// Claim 是認領通知對應的職位 id（SEC-302 S9）：只有 source 是 package 時由 pkg 寫，看板用它開到設定頁那張認領卡。
	Claim string `json:"claim,omitempty"`
	// Speaker：說話的人的名字（DEV-115）：角色本人在說話時（新角色來打招呼），看板寄件人寫她、頭像用 Image，
	// 不用 agent 那位秘書的。notify 也照它一律即時推（只保留給法律第 5 條白名單裡的新角色招呼，NTF-66）。
	// 只由核心模組在 Send 時寫（API 的 Send 不帶）。
	Speaker string `json:"speaker,omitempty"`
	// Link：訊息上的連結按鈕（DEV-115）：看板畫成一顆按鈕，點了開 URL（站內路徑，一定 / 開頭）。跟 Options（回答用的按鈕）不同，
	// 按了不算回答。核心模組與 API 的 Send（套件的程式用，DEV-157）都可以帶；`evergenie notify` 指令不開放。
	Link *NotificationLink `json:"link,omitempty"`
	// Typed：這則只收打字回答（D-57）：notify 不補預設的「好／不要」，看板只給打字回答與「去聊」。
	// 等你決定的任務問句（TSK-27）用它：問題可能是「要日式還是義式？」，按「好」說不通。
	Typed bool `json:"typed,omitempty"`
	// Lane 是這則在哪一道（agent／system，D-47）：由 notify 在 Send 時定（規則見 NTF-56），舊檔沒寫的讀出來時補上。
	Lane string `json:"lane,omitempty"`
	// Badge：這則現在算不算進「訊息」徽章與未讀數（NTF-57）；notify 讀出來時算，不存檔。看板照它數，不自己算規則。
	// 不加 omitempty：false 也要送，看板才分得出「不算」與「舊版的台沒給」。
	Badge bool `json:"badge"`
	// DeliversAnswer：主人回答之後要不要等秘書讀到（NTF-59）；notify 讀出來時算，不存檔。false 的（系統提醒、套件、
	// 綁單或待辦的通知）看板不說「秘書還沒讀到」。不加 omitempty：舊版的台沒給時看板照以前的說法。
	DeliversAnswer bool              `json:"delivers_answer"`
	Delivery       []Delivery        `json:"delivery,omitempty"`
	State          NotificationState `json:"state,omitempty"`
	Answer         *Answer           `json:"answer,omitempty"`
	Body           string            `json:"body,omitempty"` // 固定格式的完整文字，由 notify 產生
	// Home 是發出這則的家的標籤（platform 的 Label()，例 amy-mini）：只填在推播內容裡，看板連多台時用來開到對的那台（S3）；存檔不寫。
	Home      string    `json:"home,omitempty"`
	By        string    `json:"by,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

type NotifySendRequest struct {
	Agent    string   `json:"agent,omitempty"`
	Category Category `json:"category,omitempty"`
	Title    string   `json:"title,omitempty"`
	Context  string   `json:"context,omitempty"`
	Todo     string   `json:"todo,omitempty"` // APPROVE 必填
	Options  []string `json:"options,omitempty"`
	URL      string   `json:"url,omitempty"` // 空：APPROVE 預設直接開到這則訊息（REQ-138）
	Image    string   `json:"image,omitempty"`
	Room     string   `json:"room,omitempty"`
	Task     string   `json:"task,omitempty"`
	Reminder string   `json:"reminder,omitempty"`
	Urgent   bool     `json:"urgent"`
	// Inline：討論串內提問，只進看板與那個討論串，不推播（P7-5、D-33）。
	Inline bool `json:"inline,omitempty"`
	// Lane：發的人明講這則在哪一道（agent／system，D-47）；空的照規則算（NTF-56）。腳本的自動報告帶 system（CLI `--lane system`）。
	Lane string `json:"lane,omitempty"`
	// Link：訊息上的站內連結按鈕（NTF-66）：字與站內路徑都要有，否則回 invalid。套件的程式用它讓主人點下去到對的頁
	// （例：習慣的「到展示間」，DEV-157）；`evergenie notify` 指令不開放。按了不算回答。
	Link *NotificationLink `json:"link,omitempty"`
	By   *Actor            `json:"by,omitempty"`
}

type NotifySendResponse struct {
	Notification *Notification `json:"notification,omitempty"`
}

type NotifyGetRequest struct {
	ID string `json:"id,omitempty"`
}

type NotifyGetResponse struct {
	Notification *Notification `json:"notification,omitempty"`
}

type NotifyListRequest struct {
	States          []NotificationState `json:"states,omitempty"`
	Agent           string              `json:"agent,omitempty"`
	IncludeArchived bool                `json:"include_archived"`
	Page            *PageRequest        `json:"page,omitempty"`
}

type NotifyListResponse struct {
	Notifications []Notification `json:"notifications,omitempty"`
	// Unread：算進徽章的未讀數（D-47 起不含系統那道的例行報告，規則 NTF-57），不是全部的未讀。
	Unread int32         `json:"unread"`
	Page   *PageResponse `json:"page,omitempty"`
}

type NotifyMarkReadRequest struct {
	IDS []string `json:"ids,omitempty"`
}

type NotifyMarkReadResponse struct {
}

type NotifyArchiveRequest struct {
	IDS []string `json:"ids,omitempty"`
	By  *Actor   `json:"by,omitempty"`
}

type NotifyArchiveResponse struct {
	// Archived 這次封存的編號。
	Archived []string `json:"archived,omitempty"`
	// Skipped 沒封存的編號：還沒回答的「要你同意」。
	Skipped []string `json:"skipped,omitempty"`
}

type NotifyAnswerRequest struct {
	ID   string    `json:"id,omitempty"`
	Text string    `json:"text,omitempty"`
	Via  AnswerVia `json:"via,omitempty"`
	By   *Actor    `json:"by,omitempty"`
}

type NotifyAnswerResponse struct {
	Notification *Notification `json:"notification,omitempty"`
	Delivered    bool          `json:"delivered"` // 是否已送進發問的 session
}

type CategoryPref struct {
	Mode        DeliveryMode `json:"mode,omitempty"`
	DigestHours int32        `json:"digest_hours"`
}

type AgentPref struct {
	Agent      string                   `json:"agent,omitempty"`
	Categories map[string]*CategoryPref `json:"categories,omitempty"` // key 是分類名稱：approve、done、due、digest、opening
}

// TaskFollowPrefs 對應 settings/notify.toml 的 [tasks]（任務單跟進，D-22、REQ-148）。
type TaskFollowPrefs struct {
	NudgeAt        string `json:"nudge_at,omitempty"` // HH:MM，依家的時區
	StaleDays      int32  `json:"stale_days"`         // 1–30
	NudgeEveryDays int32  `json:"nudge_every_days"`   // 1–30
	NudgeMax       int32  `json:"nudge_max"`          // 0–10
}

// NotifyPrefs 對應 settings/notify.toml（schema notify-prefs）。
type NotifyPrefs struct {
	Preset            string                   `json:"preset,omitempty"` // quiet、standard、active
	QuietHours        string                   `json:"quiet_hours,omitempty"`
	ChannelApp        bool                     `json:"channel_app"`
	ChannelLine       bool                     `json:"channel_line"`
	ReaskAfterMinutes int32                    `json:"reask_after_minutes"`
	ReaskMaxAsks      int32                    `json:"reask_max_asks"`
	Categories        map[string]*CategoryPref `json:"categories,omitempty"`
	Agents            []AgentPref              `json:"agents,omitempty"`
	Tasks             *TaskFollowPrefs         `json:"tasks,omitempty"` // nil 表示不動 [tasks]
	// Presets 只在讀回來的偏好裡有：三個預設組（quiet、standard、active）每個分類的送法，跟核心用同一份表。
	// 送出去的偏好帶不帶都不影響（忽略）。
	Presets map[string]map[string]*CategoryPref `json:"presets,omitempty"`
}

type NotifyGetPrefsRequest struct {
}

type NotifyGetPrefsResponse struct {
	Prefs *NotifyPrefs `json:"prefs,omitempty"`
}

type NotifySetPrefsRequest struct {
	// Prefs.Categories 裡跟預設組一樣的分類不會寫進檔案（簡單模式選預設組＝不帶覆寫就清掉所有逐類覆寫）。
	Prefs *NotifyPrefs `json:"prefs,omitempty"` // approve 不能設成 off（REQ-26），否則 ERROR_CODE_INVALID
	// Expand 為真：把預設組展開成明確的 [categories.*] 寫進檔案（簡單切到完整模式時用）。
	Expand bool   `json:"expand,omitempty"`
	By     *Actor `json:"by,omitempty"`
}

type NotifySetPrefsResponse struct {
	Prefs *NotifyPrefs `json:"prefs,omitempty"`
}

type NotifyWatchRequest struct {
}

type NotifyWatchResponse struct {
	Notification *Notification `json:"notification,omitempty"`
	// Unread：算進徽章的未讀數（NTF-57），同 NotifyListResponse.Unread。
	Unread int32 `json:"unread"`
	// Nudge 有值：提醒有變動（新的、換了地方、被回答收掉；P7-6），看板收到就重報一次 Presence 拿最新的清單。
	Nudge *Nudge `json:"nudge,omitempty"`
}

// NudgeSurface 是一則提醒現在顯示在哪裡（D-34）。
type NudgeSurface string

const (
	NudgeSurfaceInline NudgeSurface = "inline" // 主人正在看那張單或那個討論串：頁內黃色卡片，2 分鐘沒回或離開頁面改推播
	NudgeSurfaceToast  NudgeSurface = "toast"  // 看板開著但在別頁：站內提示
	NudgeSurfacePush   NudgeSurface = "push"   // 已經推到手機
	NudgeSurfaceNone   NudgeSurface = "none"   // 沒有地方可以顯示（安靜時段、沙盒家、沒有訂閱、偏好不推播）
)

// Nudge 是一則「還在等你」的提醒（只存在 notify 的記憶體裡，不是 M-<n> 訊息）。
type Nudge struct {
	ID      string `json:"id,omitempty"`
	Agent   string `json:"agent,omitempty"`
	Session string `json:"session,omitempty"` // 工作 session 的名稱（聊天頁網址的 session）
	Task    string `json:"task,omitempty"`
	Title   string `json:"title,omitempty"`
	Context string `json:"context,omitempty"` // 背景（任務單的標題）
	Todo    string `json:"todo,omitempty"`
	URL     string `json:"url,omitempty"` // 該 session 的聊天頁
	// Key 有值：綁在對話上的「等你回覆」（選擇題、權限確認，DEV-49 S2）——只推播、不建訊息；同一個 session 可以同時有不同 key 的。
	// 空：工作 session 第 50 分鐘「還在等你」的提醒（P7-6）。
	Key       string       `json:"key,omitempty"`
	Surface   NudgeSurface `json:"surface,omitempty"`
	Resolved  bool         `json:"resolved"` // 已經在某處回答、收掉了（看板收到 Nudge 事件時才有意義）
	CreatedAt time.Time    `json:"created_at,omitempty"`
}

type NotifyNudgeRequest struct {
	Agent   string `json:"agent,omitempty"`
	Session string `json:"session,omitempty"`
	Task    string `json:"task,omitempty"`
	Title   string `json:"title,omitempty"`   // 空：「<單號> 還在等你處理」
	Context string `json:"context,omitempty"` // 背景，例如任務單的標題
	Todo    string `json:"todo,omitempty"`    // 要主人做什麼；空：「請回覆」
	URL     string `json:"url,omitempty"`     // 空：/chat/<session>
	// Key：綁在對話上的等待（例 pending:<確認編號>，DEV-49 S2）；同一個 session、同一個 key 只會有一則。
	Key string `json:"key,omitempty"`
	// Resolve：那件事不在了（答了、在別處答了、session 關了…），收回同一個 session、同一個 key 的那則（推過的用同一個 tag 蓋成「已處理」）。
	Resolve bool   `json:"resolve,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type NotifyNudgeResponse struct {
	Nudge *Nudge `json:"nudge,omitempty"`
}

type NotifyPresenceRequest struct {
	Client string `json:"client,omitempty"` // 這個瀏覽器分頁的隨機編號（同一個人可以同時開手機與桌機）
	Path   string `json:"path,omitempty"`   // 目前的看板路徑，例 /tasks/SEC-12
	Thread string `json:"thread,omitempty"` // 正在看的討論串（session 名稱）；沒有就空
	Away   bool   `json:"away"`             // 分頁被切到背景或要關了：馬上清掉，不等 90 秒過期
	By     *Actor `json:"by,omitempty"`
}

type NotifyPresenceResponse struct {
	Nudges []Nudge `json:"nudges,omitempty"` // 還沒收掉、而且要顯示在看板上的（inline、toast），新的在前
}

type NotifyResolveNudgeRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type NotifyResolveNudgeResponse struct {
}

type ShareRequest struct {
	Path  string `json:"path,omitempty"` // 秘書的檔案路徑（本機）
	Agent string `json:"agent,omitempty"`
}

type ShareResponse struct {
	URL  string `json:"url,omitempty"`
	Mime string `json:"mime,omitempty"`
	// PublicBase 是 home.toml 的 web.public_url（去掉結尾的 /）；沒設就空。CLI 用它印完整網址：PublicBase + URL。
	PublicBase string `json:"public_base,omitempty"`
}

type NotifyPushKeyRequest struct {
}

type NotifyPushKeyResponse struct {
	PublicKey     string `json:"public_key,omitempty"`
	Subscriptions int    `json:"subscriptions"`
}

type NotifySubscribePushRequest struct {
	Endpoint string `json:"endpoint,omitempty"`
	P256dh   string `json:"p256dh,omitempty"`
	Auth     string `json:"auth,omitempty"`
	By       *Actor `json:"by,omitempty"`
}

type NotifySubscribePushResponse struct {
	Subscriptions int `json:"subscriptions"`
}

type NotifyUnsubscribePushRequest struct {
	Endpoint string `json:"endpoint,omitempty"`
	By       *Actor `json:"by,omitempty"`
}

type NotifyUnsubscribePushResponse struct {
	Subscriptions int `json:"subscriptions"`
}

type NotifyPushTestRequest struct {
	Endpoint string `json:"endpoint,omitempty"`
	By       *Actor `json:"by,omitempty"`
}

type NotifyPushTestResponse struct {
}
