package sdk

import "context"

// AgentsAPI 是秘書名冊的管理（evergenie agents …、customize …，Q3）。名冊的正本是 vault/agents/<id>/agent.md（REQ-39）；
// 這裡的寫入都經 agent 模組，不直接寫 vault（API-1）。會改名冊或帳號設定的方法只有主人（by 是 owner(…)）能呼叫；
// AddRule 另外允許該秘書本人（跟 addendum 的寫入規則一樣）。
type AgentsAPI interface {
	// Create 建一位新秘書：id 只能小寫英文、數字、減號（^[a-z][a-z0-9-]{1,30}$）；已存在（含封存過的）回 conflict。
	// 職責（內文）與記憶是空的；建好時 enabled=false，要上線另外 set enabled true。
	Create(ctx context.Context, req *AgentsCreateRequest) (*AgentsCreateResponse, error)
	// Archive 把一位秘書整包封存（設定、職責、附加規則、記憶、日誌、筆記、排程），不刪除（REQ-5）；回傳怎麼還原。
	// session 由呼叫端先停掉。
	Archive(ctx context.Context, req *AgentsArchiveRequest) (*AgentsArchiveResponse, error)
	// Rename 改 id：設定、附加規則、記憶、日誌、筆記、排程（id 前綴與 agent 欄位）一起搬；回傳還提到舊 id 的檔案（不自動改內文）。
	Rename(ctx context.Context, req *AgentsRenameRequest) (*AgentsRenameResponse, error)
	// Assign 換人設：只改 persona 與 former_names（舊的顯示名稱記進去）；記憶與職責不動（REQ-41）。
	Assign(ctx context.Context, req *AgentsAssignRequest) (*AgentsAssignResponse, error)
	// Swap 兩位秘書的人設互換（等於兩次 Assign）；一個失敗另一個不改。
	Swap(ctx context.Context, req *AgentsSwapRequest) (*AgentsSwapResponse, error)
	// SetAccount 改這位用的帳號（可同時改 model、effort）；常駐對話重開後才生效。
	SetAccount(ctx context.Context, req *AgentsSetAccountRequest) (*AgentsSetAccountResponse, error)
	// SetField 改名冊紀錄的單一欄位（白名單見 schema agent 的「可以用 agents set 改的欄位」）。
	SetField(ctx context.Context, req *AgentsSetFieldRequest) (*AgentsSetFieldResponse, error)
	// Link 把每位啟用中的秘書的職責交給各帳號的引擎，寫成平台自己的「點名叫出來」用的檔案。
	// apply=false 只預演（不寫任何檔）；apply=true 才寫，只有主人能做。
	Link(ctx context.Context, req *AgentsLinkRequest) (*AgentsLinkResponse, error)
	// AddRule 把使用者交代的一條新規則附加到這位秘書的附加規則（addendum）尾端（REQ-46）；官方職責不動（REQ-48）。
	// 帶 Section 時加在附加規則裡 `## <Section>` 那一段的最後（沒有就在尾端開一段；DEV-161）。
	AddRule(ctx context.Context, req *AgentsAddRuleRequest) (*AgentsAddRuleResponse, error)
	// Customizations 讀這位秘書的官方職責與附加規則（customize show）。
	Customizations(ctx context.Context, req *AgentsCustomizationsRequest) (*AgentsCustomizationsResponse, error)
	// WriteMemory 整份取代這位秘書的長期記憶；只有本人（by 等於 id）或 system 能寫（AGT-6）。
	WriteMemory(ctx context.Context, req *AgentsWriteMemoryRequest) (*AgentsWriteMemoryResponse, error)
	// WriteNote 新增或整份取代這位秘書自己的做事筆記 vault/agents/<id>/notes/<name>.md（DEV-161）；只有本人（by 等於 id）或 system 能寫。
	// 檔頭（schema、updated_at、by、agent、created_at）由核心補，呼叫端只給內文；新檔 revision 留空，改舊檔要帶讀到的版本
	// （讀走 vault 的 get／list，種類 note、id「<職位>/<名字>」）。
	WriteNote(ctx context.Context, req *AgentsWriteNoteRequest) (*AgentsWriteNoteResponse, error)
	// AppendJournal 在這位秘書今天日誌的尾端追加一行（時間戳記由核心加）；只有本人或 system 能寫。
	AppendJournal(ctx context.Context, req *AgentsAppendJournalRequest) (*AgentsAppendJournalResponse, error)
	// Knowledge 列出這位秘書職責裡的可選知識與現在有沒有勾（DEV-161）；職位沒有可選知識回空清單。
	Knowledge(ctx context.Context, req *AgentsKnowledgeRequest) (*AgentsKnowledgeResponse, error)
	// SetKnowledge 換掉勾的可選知識（整份取代，記在名冊紀錄的 knowledge，並把當下全部可選 id 記進 knowledge_seen）；
	// 主人、system 或擔任這個職位的秘書本人（by 是自己的職位 id）能改，別的秘書不行；不認得的 id 回 invalid_argument。
	SetKnowledge(ctx context.Context, req *AgentsSetKnowledgeRequest) (*AgentsKnowledgeResponse, error)
}

type AgentsCreateRequest struct {
	ID          string `json:"id,omitempty"`
	Persona     string `json:"persona,omitempty"`      // 人設 slug（personas/<slug>.md），必填
	DisplayName string `json:"display_name,omitempty"` // 選填
	Duty        string `json:"duty,omitempty"`         // 一句話職責；空的用「（還沒寫）」
	// Knowledge：要勾的可選知識 id（DEV-161）；沒給＝照職責裡每塊的預設，JSON 給 [] ＝全部不勾（Go 用戶端的空切片會被 omitempty 丟掉，要全部不勾請建好後用 SetKnowledge）。不認得的 id 回 invalid_argument。
	Knowledge []string `json:"knowledge,omitempty"`
	By        *Actor   `json:"by,omitempty"`
}

type AgentsCreateResponse struct {
	Agent *Record `json:"agent,omitempty"`
}

type AgentsArchiveRequest struct {
	ID string `json:"id,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type AgentsArchiveResponse struct {
	Archived []string `json:"archived,omitempty"` // 封存的資料，一筆一個「種類/id」
	Restore  string   `json:"restore,omitempty"`  // 白話的還原方式
}

type AgentsRenameRequest struct {
	ID          string `json:"id,omitempty"`
	NewID       string `json:"new_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"` // 選填：同時改顯示名稱
	By          *Actor `json:"by,omitempty"`
}

type AgentsRenameResponse struct {
	Agent    *Record  `json:"agent,omitempty"`
	Moved    []string `json:"moved,omitempty"`    // 搬過去的資料，一筆一個「種類/新 id」
	Mentions []string `json:"mentions,omitempty"` // 內容還提到舊 id 的檔案（相對家的路徑），請人自己看
}

type AgentsAssignRequest struct {
	ID      string `json:"id,omitempty"`
	Persona string `json:"persona,omitempty"`
	By      *Actor `json:"by,omitempty"`
}

type AgentsAssignResponse struct {
	Agent *Record `json:"agent,omitempty"`
}

type AgentsSwapRequest struct {
	A  string `json:"a,omitempty"`
	B  string `json:"b,omitempty"`
	By *Actor `json:"by,omitempty"`
}

type AgentsSwapResponse struct {
	Agents []Record `json:"agents,omitempty"`
}

type AgentsSetAccountRequest struct {
	ID      string `json:"id,omitempty"`
	Account string `json:"account,omitempty"` // home.toml 的 accounts.<名>
	Model   string `json:"model,omitempty"`   // 空表示不改
	Effort  string `json:"effort,omitempty"`  // 空表示不改
	By      *Actor `json:"by,omitempty"`
}

type AgentsSetAccountResponse struct {
	Agent *Record `json:"agent,omitempty"`
}

type AgentsSetFieldRequest struct {
	ID    string `json:"id,omitempty"`
	Field string `json:"field,omitempty"` // 例：display_name、model、effort、enabled、budget.max_runs_per_day
	Value string `json:"value,omitempty"` // 文字；布林寫 true／false、數字寫阿拉伯數字；空字串表示拿掉（只有選填欄位可以）
	By    *Actor `json:"by,omitempty"`
}

type AgentsSetFieldResponse struct {
	Agent *Record `json:"agent,omitempty"`
}

// AgentCard 是一位秘書交給引擎、讓平台能「點名叫出來」的資料（中立詞彙；檔案格式由引擎決定）。
type AgentCard struct {
	ID      string   `json:"id,omitempty"`      // 職位 id，也是檔名
	Name    string   `json:"name,omitempty"`    // 現任的名字（顯示名稱或人設的 name）
	Aliases []string `json:"aliases,omitempty"` // 也會被這樣叫：人設的別名、以前的名字
	Duty    string   `json:"duty,omitempty"`    // 一句話職責
	Body    string   `json:"body,omitempty"`    // 官方職責＋附加規則（附加規則永遠在後面）
}

// AgentCardFile 是引擎對一個檔案要做（或做了）的事。
type AgentCardFile struct {
	Agent   string `json:"agent,omitempty"`
	Path    string `json:"path,omitempty"`
	Action  string `json:"action,omitempty"`  // create、update、same（內容一樣）、skip（已有別人的檔案，不覆蓋）、remove（名冊上已經沒有）
	Summary string `json:"summary,omitempty"` // 一行內容摘要或略過的原因
}

type AgentLinkAccount struct {
	Account string          `json:"account,omitempty"`
	Note    string          `json:"note,omitempty"` // 整個帳號沒處理時的原因（例如引擎沒有在跑）
	Files   []AgentCardFile `json:"files,omitempty"`
}

type AgentsLinkRequest struct {
	Apply bool   `json:"apply,omitempty"`
	By    *Actor `json:"by,omitempty"`
}

type AgentsLinkResponse struct {
	Applied  bool               `json:"applied"`
	Accounts []AgentLinkAccount `json:"accounts,omitempty"`
}

type AgentsAddRuleRequest struct {
	ID      string `json:"id,omitempty"`
	Rule    string `json:"rule,omitempty"`    // 一條規則的原文（可以多行）
	Section string `json:"section,omitempty"` // 選填：加在附加規則裡 `## <section>` 那一段的最後（沒有就在尾端開一段），例「聊到什麼先看什麼」
	By      *Actor `json:"by,omitempty"`
}

type AgentsAddRuleResponse struct {
	Addendum string `json:"addendum,omitempty"` // 寫完之後整份附加規則
}

type AgentsCustomizationsRequest struct {
	ID string `json:"id,omitempty"`
}

type AgentsCustomizationsResponse struct {
	Duties   string `json:"duties,omitempty"`   // 官方職責（只讀）
	Addendum string `json:"addendum,omitempty"` // 使用者加的規則
}

type AgentsWriteMemoryRequest struct {
	ID   string `json:"id,omitempty"`
	Body string `json:"body,omitempty"` // 新的整份記憶
	By   *Actor `json:"by,omitempty"`
}

type AgentsWriteMemoryResponse struct {
}

type AgentsWriteNoteRequest struct {
	ID       string `json:"id,omitempty"`       // 職位 id
	Name     string `json:"name,omitempty"`     // 筆記名字，就是檔名（中文可、不含 /）
	Body     string `json:"body,omitempty"`     // 新的整份內文（不含檔頭）
	Revision string `json:"revision,omitempty"` // 改舊檔要帶現在的版本；新檔留空
	By       *Actor `json:"by,omitempty"`
}

type AgentsWriteNoteResponse struct {
	Name     string `json:"name,omitempty"`     // 實際用的名字（去掉前後空白與 .md）
	Created  bool   `json:"created"`            // 是不是新建的
	Revision string `json:"revision,omitempty"` // 寫完的版本
}

type AgentsAppendJournalRequest struct {
	ID   string `json:"id,omitempty"`
	Line string `json:"line,omitempty"` // 一行，不含換行
	By   *Actor `json:"by,omitempty"`
}

type AgentsAppendJournalResponse struct {
}

// AgentKnowledgeBlock 是職責裡的一塊可選知識（`## 可選知識` 底下的 `### <標題> {#<id>}`，DEV-161）。
type AgentKnowledgeBlock struct {
	ID      string `json:"id,omitempty"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"` // 一句說明
	Default bool   `json:"default"`           // 職責寫的預設：勾或不勾
	Enabled bool   `json:"enabled"`           // 這位秘書現在有沒有勾（主人選過照他選的，沒選過照預設）
}

type AgentsKnowledgeRequest struct {
	Agent string `json:"agent,omitempty"` // 職位 id
}

type AgentsKnowledgeResponse struct {
	Agent  string                `json:"agent,omitempty"`
	Blocks []AgentKnowledgeBlock `json:"blocks"`
}

type AgentsSetKnowledgeRequest struct {
	Agent   string   `json:"agent,omitempty"`
	Enabled []string `json:"enabled"` // 要勾的 id（整份取代；空清單＝全部不勾）
	By      *Actor   `json:"by,omitempty"`
}
