package sdk

import (
	"context"
)

// WebService：看板外殼需要的資料。選單由伺服器依「已安裝＋已啟用＋有權限」算出完整的一份，
// 前端拿到才畫（D-11、REQ-105；等太久或讀不到時看板先用基本選單，WEB-175）。偏好的檔案格式見 schema menu-prefs。
type WebAPI interface {
	// GetMenu 回完整選單，已套上使用者的排序與隱藏。
	GetMenu(ctx context.Context, req *WebGetMenuRequest) (*WebGetMenuResponse, error)
	// GetPages 回所有已註冊的頁面與 slot（路由表）。
	GetPages(ctx context.Context, req *WebGetPagesRequest) (*WebGetPagesResponse, error)
	GetMenuPrefs(ctx context.Context, req *WebGetMenuPrefsRequest) (*WebGetMenuPrefsResponse, error)
	SetMenuPrefs(ctx context.Context, req *WebSetMenuPrefsRequest) (*WebSetMenuPrefsResponse, error)
	// GetHealth 一般使用者設定頁看的簡單健康狀態（REQ-110、REQ-137 的簡化版）。
	GetHealth(ctx context.Context, req *WebGetHealthRequest) (*WebGetHealthResponse, error)
	// GetRoster 回已啟用的秘書（人名、職責、顏色、頭像網址）；畫面一律顯示人名，不顯示職位 id。
	GetRoster(ctx context.Context, req *WebGetRosterRequest) (*WebGetRosterResponse, error)
	// GetOverview 回總覽頁的「等你處理」：數字由伺服器算好，前端不自己算（P4 共通定案 9）。
	GetOverview(ctx context.Context, req *WebGetOverviewRequest) (*WebGetOverviewResponse, error)
	// GetTrust 回家目前的權限設定（settings/trust.toml，唯讀；設定頁的「權限」分頁）。
	GetTrust(ctx context.Context, req *WebGetTrustRequest) (*WebGetTrustResponse, error)
	// GetAgents 回設定頁「職位與秘書」要的資料：名冊上全部秘書（含停用）加上可選的帳號（P6-2b）。
	GetAgents(ctx context.Context, req *WebGetAgentsRequest) (*WebGetAgentsResponse, error)
	// GetSites 回看板連多台的設定（home.toml 的 [web] owner_login、allow_boards、[[web.sites]]；S3，2026-11-01）。誰都可以讀。
	GetSites(ctx context.Context, req *WebGetSitesRequest) (*WebGetSitesResponse, error)
	// SetSites 整份換掉這個看板還要連的別台（[[web.sites]]）；只有主人。網址整理成 https origin 的寫法，壞值整份擋下、檔案不動。
	SetSites(ctx context.Context, req *WebSetSitesRequest) (*WebSetSitesResponse, error)
	// SetAllowBoards 整份換掉哪些別台的看板可以從瀏覽器呼叫這個家（allow_boards）；只有主人。
	SetAllowBoards(ctx context.Context, req *WebSetAllowBoardsRequest) (*WebSetAllowBoardsResponse, error)
	// SetOwnerLogin 改主人的 tailnet 登入（owner_login；空字串＝不開放別台的看板連進來）；只有主人。
	SetOwnerLogin(ctx context.Context, req *WebSetOwnerLoginRequest) (*WebSetOwnerLoginResponse, error)
	// GetBootPrompts 回卡在啟動對話框（例如「信任這個資料夾」）的秘書；看板在最上面列出來讓主人回答。
	GetBootPrompts(ctx context.Context, req *WebGetBootPromptsRequest) (*WebGetBootPromptsResponse, error)
	// AnswerBootPrompt 回答一個啟動對話框；只有主人，別台看板跨站來一律不准。
	AnswerBootPrompt(ctx context.Context, req *WebAnswerBootPromptRequest) (*WebAnswerBootPromptResponse, error)
	// VoiceSend 主人用語音（手機、手錶的捷徑聽寫）丟一句話：核心修正聽寫錯字、猜要給哪位秘書，再以主人身分傳過去；只有主人。
	VoiceSend(ctx context.Context, req *WebVoiceSendRequest) (*WebVoiceSendResponse, error)
	// VoiceAnswer 秘書回答主人用語音問的問題（一兩句，捷徑會唸出來）；只有收到那句話的秘書能回，主人沒在等了回 delivered=false。
	VoiceAnswer(ctx context.Context, req *WebVoiceAnswerRequest) (*WebVoiceAnswerResponse, error)
	// GetPhone 回「設定 → 手機連線」現在做到哪（DEV-153）：這台的 Tailscale 裝了沒、登入了沒、看板掛上了沒、網址、
	// 推播金鑰好了沒、看板有沒有被 Funnel 開到整個網路。只讀，誰都可以看。
	GetPhone(ctx context.Context, req *WebGetPhoneRequest) (*WebGetPhoneResponse, error)
	// PhoneSetup 是「讓手機連得到」那顆按鈕：等於 `evergenie tailscale setup`，順便把推播金鑰做好（沒有才做）。
	// 只有主人，而且只能在這台 Mac 本機開的看板上按（`local`，看板伺服器照請求算出來、不信用戶端給的）。絕不開 Funnel。
	PhoneSetup(ctx context.Context, req *WebPhoneSetupRequest) (*WebPhoneSetupResponse, error)
	// PhoneCloseFunnel 把看板對整個網路的公開（Tailscale Funnel）關掉；條件同 PhoneSetup。
	PhoneCloseFunnel(ctx context.Context, req *WebPhoneCloseFunnelRequest) (*WebPhoneCloseFunnelResponse, error)
}

// WebPhone 是手機連線現在的樣子（DEV-153）。
type WebPhone struct {
	Local       bool   `json:"local"`                  // 這個請求是不是從這台 Mac 本機開的看板來的（是才能按設定與關閉的按鈕）
	Tailscale   string `json:"tailscale,omitempty"`    // missing 沒裝／stopped 裝了沒開／login 還沒登入／ready 裝好登入好了
	Account     string `json:"account,omitempty"`      // 這台 Tailscale 登入的帳號（手機要用同一個）；讀不到是空的
	Mounted     bool   `json:"mounted"`                // 看板已經掛到 Tailscale
	URL         string `json:"url,omitempty"`          // 手機要開的網址（mounted 才有）
	PushReady   bool   `json:"push_ready"`             // 推播金鑰做好了
	Funnel      bool   `json:"funnel"`                 // 看板被 Funnel 開到整個網路
	FunnelURL   string `json:"funnel_url,omitempty"`   // 被公開的網址
	Problem     string `json:"problem,omitempty"`      // 讀不到現況的白話原因
	DownloadURL string `json:"download_url,omitempty"` // Tailscale 官方的 Mac 下載頁
}

type WebGetPhoneRequest struct {
	Local bool   `json:"local"` // 看板伺服器填的；直接呼叫核心（指令）才自己給
	By    *Actor `json:"by,omitempty"`
}

type WebGetPhoneResponse struct {
	Phone WebPhone `json:"phone"`
}

type WebPhoneSetupRequest struct {
	Local bool   `json:"local"`
	By    *Actor `json:"by,omitempty"`
}

type WebPhoneSetupResponse struct {
	OK      bool     `json:"ok"`                 // 看板掛好了
	Reason  string   `json:"reason,omitempty"`   // 沒成功時卡在哪：missing／stopped／login／https（後台還沒開 HTTPS）／other
	Message string   `json:"message,omitempty"`  // 白話：發生什麼、要做什麼
	HelpURL string   `json:"help_url,omitempty"` // 要主人去按的網頁（下載頁、後台開 HTTPS 的那一頁）
	Phone   WebPhone `json:"phone"`              // 做完之後的現況
}

type WebPhoneCloseFunnelRequest struct {
	Local bool   `json:"local"`
	By    *Actor `json:"by,omitempty"`
}

type WebPhoneCloseFunnelResponse struct {
	Phone WebPhone `json:"phone"`
}

type MenuItem struct {
	ID    string `json:"id,omitempty"` // core.chat 或 <套件 id>.<項目 id>
	Title string `json:"title,omitempty"`
	Icon  string `json:"icon,omitempty"`
	Route string `json:"route,omitempty"`
	Badge int32  `json:"badge"` // 例：未讀數、等你處理的數量（REQ-109）
	// Section／SectionTitle：套件宣告的「同一頁」（MenuItemDecl.Section）；核心項目沒有。
	Section      string `json:"section,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
	// Tier：項目來自哪一層（核心項目算 official；installed 在畫面上叫「分享的」）；看板只拿來顯示標籤，放哪裡由使用者的樹決定（SEC-302 S4、REQ-348）。
	Tier Tier `json:"tier,omitempty"`
}

type MenuEntry struct {
	ID       string      `json:"id,omitempty"` // 項目 id（core.chat、<套件 id>.<項目 id>）或群組 id
	Title    string      `json:"title,omitempty"`
	Icon     string      `json:"icon,omitempty"`
	Route    string      `json:"route,omitempty"` // 群組沒有
	Badge    int32       `json:"badge"`
	Children []MenuEntry `json:"children,omitempty"` // 只有群組有
	// 同 MenuItem：看板把同一個群組裡 section 相同的項目合成一個入口。
	Section      string `json:"section,omitempty"`
	SectionTitle string `json:"section_title,omitempty"`
	Tier         Tier   `json:"tier,omitempty"` // 同 MenuItem.Tier；群組沒有
}

// Menu 是伺服器依「已安裝＋已啟用＋有權限」算好的完整結果（D-11）：沒有權限的項目根本不在裡面。
type Menu struct {
	Tree              []MenuEntry `json:"tree,omitempty"`                // 使用者的樹（已套上使用者的組合；沒放進樹的項目在最後的「未分類」群組）
	Pool              []MenuItem  `json:"pool,omitempty"`                // 全部可用的項目（給自訂呈現元件用，它可以不照 tree 排）
	Tucked            []MenuItem  `json:"tucked,omitempty"`              // 收到設定的項目（MNU-10）：不在 tree，列在設定頁
	Renderer          string      `json:"renderer,omitempty"`            // 空：用預設呈現元件；否則是使用者自訂元件的 id
	RendererBundleURL string      `json:"renderer_bundle_url,omitempty"` // 自訂元件編譯後的 JS（renderer 非空才有）
	RendererError     string      `json:"renderer_error,omitempty"`      // 自訂元件找不到、不在自己的套件裡或編不過的白話原因（看板退回預設元件並提醒）
}

type WebGetMenuRequest struct {
}

type WebGetMenuResponse struct {
	Menu *Menu `json:"menu,omitempty"`
}

type PageSlot struct {
	ID   string `json:"id,omitempty"`
	Page string `json:"page,omitempty"` // 這個 slot 屬於哪個頁面
}

type PageInfo struct {
	ID        string     `json:"id,omitempty"`
	Title     string     `json:"title,omitempty"`
	Route     string     `json:"route,omitempty"`
	BundleURL string     `json:"bundle_url,omitempty"` // esbuild 編譯後的 JS
	Package   string     `json:"package,omitempty"`    // 來自哪個套件；核心頁面為空
	Slots     []PageSlot `json:"slots,omitempty"`
	Slot      string     `json:"slot,omitempty"`       // 這個頁面掛在哪個 slot
	SlotFront bool       `json:"slot_front,omitempty"` // 在 slot 裡排最前面
	Error     string     `json:"error,omitempty"`      // 編不過或要掛的位置不存在時的白話原因（有值就沒有 bundle_url）
	Chat      bool       `json:"chat"`                 // 同 PageDecl.Chat
	ChatAgent string     `json:"chat_agent,omitempty"` // 同 PageDecl.ChatAgent
	// Tier：套件在哪一層（套件頁才有；看板在標題旁標「我的」「分享的」，官方不標）；Source：分享的套件從網址裝時的來源網址（.source.toml），沒有就空（SEC-302 S4、REQ-348）。
	Tier   Tier   `json:"tier,omitempty"`
	Source string `json:"source,omitempty"`
	// Alias：套件 package.toml 寫的 route（例 /water）；看板打開這個網址時轉到 Route（DEV-91）。沒寫或跟 Route 一樣就空。
	Alias string `json:"alias,omitempty"`
}

type WebGetPagesRequest struct {
}

type WebGetPagesResponse struct {
	Pages []PageInfo `json:"pages,omitempty"`
}

// MenuPrefs 對應 settings/menu.toml（schema menu-prefs）。
type MenuPrefs struct {
	Tree     []MenuPrefEntry `json:"tree,omitempty"`
	Renderer string          `json:"renderer,omitempty"` // 空：預設呈現元件
	// Hidden 是收到設定的項目 id（MNU-10）。請求不帶＝用預設；帶空清單＝全部放回選單。
	Hidden []string `json:"hidden,omitempty"`
}

type MenuPrefEntry struct {
	Item     string   `json:"item,omitempty"`  // 葉：項目 id（與 group 二選一）
	Group    string   `json:"group,omitempty"` // 群組 id
	Title    string   `json:"title,omitempty"` // 群組名稱
	Icon     string   `json:"icon,omitempty"`
	Children []string `json:"children,omitempty"` // 群組底下的項目 id；不能是群組
}

type WebGetMenuPrefsRequest struct {
}

type WebGetMenuPrefsResponse struct {
	Prefs *MenuPrefs `json:"prefs,omitempty"`
}

type WebSetMenuPrefsRequest struct {
	Prefs *MenuPrefs `json:"prefs,omitempty"`
	By    *Actor     `json:"by,omitempty"`
}

type WebSetMenuPrefsResponse struct {
	Prefs *MenuPrefs `json:"prefs,omitempty"`
}

type WebGetHealthRequest struct {
}

type HealthItem struct {
	Name   string `json:"name,omitempty"` // 例 serve、AI 帳號、套件
	Ok     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type WebGetHealthResponse struct {
	Items []HealthItem `json:"items,omitempty"`
}

// RosterAgent 是看板上的一位秘書。Name 是現任人設的人名；AvatarURL 是 /avatars/<id>.png，只有這位的人設現在有頭像圖才給
// （DEV-157；沒有圖是空的，前端顯示人名前兩個字的圓、不去抓圖）。VideoURL 只有人設的預設影片存在時才有，是 /avatars/<id>.mp4。
// VideoURLs 是所有動態頭像（DEV-125，同一張照片最多 6 支，/avatars/<id>.mp4、/avatars/<id>+2.mp4…），前端每次隨機挑一支播；VideoURL 等於它的第 1 個。
// Persona 是人設代號（看板把同一位角色坐的幾個職位合成一張）。
type RosterAgent struct {
	ID        string   `json:"id,omitempty"`
	Name      string   `json:"name,omitempty"`
	Duty      string   `json:"duty,omitempty"`
	Color     string   `json:"color,omitempty"`
	ColorDark string   `json:"color_dark,omitempty"`
	AvatarURL string   `json:"avatar_url,omitempty"`
	Enabled   bool     `json:"enabled"`
	VideoURL  string   `json:"video_url,omitempty"`
	VideoURLs []string `json:"video_urls,omitempty"`
	Persona   string   `json:"persona,omitempty"`
}

type WebGetRosterRequest struct {
}

type WebGetRosterResponse struct {
	Agents []RosterAgent `json:"agents,omitempty"`
}

type WebGetOverviewRequest struct {
}

// WebGetOverviewResponse：WaitingTotal ＝「要你同意」而且還沒回答的通知數（Approvals 的筆數）
// 加上等你處理的任務單數（WaitingTasks 的筆數，等你決定＋等你驗收）。
type WebGetOverviewResponse struct {
	WaitingTotal   int32           `json:"waiting_total"`
	Approvals      []Notification  `json:"approvals,omitempty"`
	WaitingTasks   []WaitingTask   `json:"waiting_tasks,omitempty"`
	WaitingThreads []ThreadSummary `json:"waiting_threads,omitempty"`
	// Unread：算進徽章的未讀數（NTF-57），同 NotifyListResponse.Unread。
	Unread         int32      `json:"unread"`
	TodayReminders []Reminder `json:"today_reminders,omitempty"`
}

type WebGetTrustRequest struct {
}

type WebGetTrustResponse struct {
	Policy *PermissionPolicy `json:"policy,omitempty"`
}

type WebGetAgentsRequest struct{}

// AgentSetting 是設定頁的一位秘書；Name 是現任人設的人名。
type AgentSetting struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Duty    string `json:"duty,omitempty"`
	Persona string `json:"persona,omitempty"`
	Account string `json:"account,omitempty"`
	Model   string `json:"model,omitempty"`
	Effort  string `json:"effort,omitempty"`
	Enabled bool   `json:"enabled"`
}

type WebGetAgentsResponse struct {
	Agents   []AgentSetting `json:"agents,omitempty"`   // 名冊上全部（含停用、不含封存），依 id 排
	Accounts []string       `json:"accounts,omitempty"` // home.toml 的帳號名稱，排序
}

// WebSite 是看板還要連的另一台（home.toml 的 [[web.sites]]）。台名、環境、版本向那台的 system.get_mode 拿，這裡不存。
type WebSite struct {
	URL   string `json:"url,omitempty"`   // 那台看板的 https origin（https://主機[:port]，不帶路徑）
	Color string `json:"color,omitempty"` // #rgb 或 #rrggbb；空＝照電腦代號給預設色
}

type WebGetSitesRequest struct{}

// WebGetSitesResponse 是整理過的值：網址一律是 Origin 的寫法（小寫、沒有 :443 與結尾斜線）。
type WebGetSitesResponse struct {
	OwnerLogin  string    `json:"owner_login,omitempty"`  // 主人的 tailnet 登入；空＝不開放別台的看板連進來
	AllowBoards []string  `json:"allow_boards,omitempty"` // 可以從瀏覽器呼叫這個家的別台看板
	Sites       []WebSite `json:"sites,omitempty"`        // 這個看板還要連的別台
}

type WebSetSitesRequest struct {
	Sites []WebSite `json:"sites,omitempty"` // 整份；空＝清掉
	By    *Actor    `json:"by,omitempty"`
}

type WebSetSitesResponse struct {
	Sites  []WebSite `json:"sites,omitempty"`  // 寫進去、整理過的那一份
	Backup string    `json:"backup,omitempty"` // 舊 home.toml 的備份（相對家的路徑）
}

type WebSetAllowBoardsRequest struct {
	AllowBoards []string `json:"allow_boards,omitempty"` // 整份；空＝清掉
	By          *Actor   `json:"by,omitempty"`
}

type WebSetAllowBoardsResponse struct {
	AllowBoards []string `json:"allow_boards,omitempty"`
	Backup      string   `json:"backup,omitempty"`
}

type WebBootPrompt struct {
	ID        string   `json:"id,omitempty"`
	Session   string   `json:"session,omitempty"`
	AgentName string   `json:"agent_name,omitempty"` // 畫面顯示的人名；找不到秘書時空白
	Title     string   `json:"title,omitempty"`
	Choices   []string `json:"choices,omitempty"`
}

type WebGetBootPromptsRequest struct{}

type WebGetBootPromptsResponse struct {
	Prompts []WebBootPrompt `json:"prompts,omitempty"`
}

type WebAnswerBootPromptRequest struct {
	Session string `json:"session,omitempty"`
	ID      string `json:"id,omitempty"`
	Choice  string `json:"choice,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type WebAnswerBootPromptResponse struct{}

type WebVoiceSendRequest struct {
	Text string `json:"text,omitempty"` // 聽寫出來的原文
	To   string `json:"to,omitempty"`   // 指定給誰（職位 id 或秘書名字）；空＝核心依內容猜
	// NoWait：是問題也不等秘書回答，送出就回。預設（false）是問題就等秘書的簡短回答（等多久由核心定）。
	NoWait bool   `json:"no_wait,omitempty"`
	By     *Actor `json:"by,omitempty"`
}

type WebVoiceAnswerRequest struct {
	Ask  string `json:"ask,omitempty"`  // 語音問題的代號（傳話內文裡的 V- 開頭那串）
	Text string `json:"text,omitempty"` // 一兩句口語回答
	By   *Actor `json:"by,omitempty"`
}

type WebVoiceAnswerResponse struct {
	Delivered bool   `json:"delivered"`       // 主人還在等，已經交給捷徑念出來
	Reply     string `json:"reply,omitempty"` // 給秘書看的一句話
}

type WebVoiceSendResponse struct {
	Agent     string `json:"agent,omitempty"`      // 收到的秘書（職位 id）
	AgentName string `json:"agent_name,omitempty"` // 畫面與手錶顯示的人名
	Text      string `json:"text,omitempty"`       // 實際送出的內容（修正過聽寫錯字）
	Guessed   bool   `json:"guessed"`              // 沒有指名、由核心猜的
	Sure      bool   `json:"sure"`                 // 猜得有把握（指名的一律 true）
	Envelope  string `json:"envelope,omitempty"`   // 傳話封包 id
	Reply     string `json:"reply,omitempty"`      // 給捷徑顯示的一句話
	Speak     string `json:"speak,omitempty"`      // 給捷徑念出來的一句話（有回答就是回答，沒有就是「交給某某了」）
	Question  bool   `json:"question"`             // 判斷是在問問題（會等秘書回答）
	Answered  bool   `json:"answered"`             // 秘書在時間內回答了
}

type WebSetOwnerLoginRequest struct {
	OwnerLogin string `json:"owner_login,omitempty"` // 空＝拿掉
	By         *Actor `json:"by,omitempty"`
}

type WebSetOwnerLoginResponse struct {
	OwnerLogin string `json:"owner_login,omitempty"`
	Backup     string `json:"backup,omitempty"`
}
