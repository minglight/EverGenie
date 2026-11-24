# 模組：engine（`internal/base/engine`）
Status: DRAFT — 設計 2026-10-23（P0-S7；同日依 D-21 改寫）；2026-10-24 依新傳話架構補介面（D-5、D-24，P3 放行）。

**目的：** L5 的「AI 平台邊界」：只有介面。凡是跟 AI 平台有關的事（啟動、通知有訊息、事件、權限與問答、對話記錄、遠端存取、用量、帳號設定）都在這裡切成中立的 interface，形狀照 Claude 有的能力來切（D-6、D-7、D-21）。
**種類：** 底層模組（`internal/base/`；沒有 HTTP、沒有指令，只給別人呼叫）

## MUST
- **ENG-1** — 核心只認 `EngineAPI`（核心呼叫引擎）與 `EngineHostAPI`（引擎呼叫核心）這兩個介面；平台的差異（終端機怎麼開、事件從哪來、記錄檔格式、權限設定、遠端存取的做法）全在引擎套件裡。
- **ENG-2** — 介面的名稱與型別只用 EverGenie 自己的詞彙（`EngineEvent`、`Question`、`Decision`、`PermissionPolicy`…），核心的 `internal/` 與 `sdk/` 不出現任何平台的專有名詞；lint 擋。
- **ENG-3** — 依 `home.toml` 的 `accounts.<名>.engine` 選引擎，把呼叫轉給該引擎套件的 `EngineAPI`；引擎未知回 `ERROR_CODE_NOT_FOUND`。
- **ENG-4** — 引擎自己的帳號設定整包不透明地傳過去（`StartRequest.engine_config`），核心不解讀；`Capabilities.features` 是留給以後的旗標，核心目前不讀（這兩個是「空格」，D-21）。
- **ENG-9** — 用量與額度（Q4，REQ-86）：`Usages(account)` 問每個帳號的引擎（`Usage`，帶該帳號的 `engine_config`），沒有資料或引擎沒在跑放說明、不當成錯誤；`Preflight()` 問每個帳號的引擎跑起來需要的東西，同一個 id 只列一次；`Warmup()` 對 `accounts.<名>.usage_warmup = true` 的帳號送暖機請求，失敗發 `system.alert`；`LimitWatch(now)` 記下用量 ≥ 98% 的視窗與重置時間，全部過了重置才發一則「額度開通了」（`system.alert`），安靜時段先留著、重置超過 12 小時才處理就不發，狀態在 `runtime/engine/limit-watch.json`。後兩者由排程的系統級工作觸發（`job.action` 是 `usage_warmup`／`limit_watch`，訂閱 `schedule.fire`）。
- **ENG-12** — 接續（`resume`，P7-5、D-33）：`EngineStartRequest.ResumeSessionID` 有值就接回那個引擎 session（對話沿用），`EngineStartResponse.EngineSessionID` 回同一個 id；接不回來（平台的對話不見了）時引擎開新對話、回新的 id，不回錯誤也不開一個會立刻退出的 session（核心比對 id 處理，SES-67）；引擎在 `Capabilities.features["resume"] = true` 表示支援（核心不靠它分支，給 doctor 與看板顯示；ENG-4 的「空格」之一）。id 格式不對回 `ERROR_CODE_INVALID`。fake 引擎接回時回傳同一個 id。工作 session 關掉再接回靠它。
- **ENG-13** — `delivered=true`＝平台已經收下這句（引擎在 `Deliver` 裡自己確認送出，S5-1；核心不再重查，S5-2）；「她收到了、開始處理」照舊只看之後的 `TURN_STARTED`。`delivered=false` 時核心退避再試，用**同一個** `envelope_id`。重查（DEV-50 S7）：同一個 `envelope_id` 再 `Deliver` 一次表示「上一次可能貼了但沒確認」：引擎先看上一次的到了沒，已經在對話裡就回 `delivered=true`、`reason` 是 `sdk.DeliverAlready`（`already`），不再送一次；沒到才照常送。不同的 `envelope_id` 照常送。fake 引擎 `Delivered: true` 馬上回，語意同上（記進對話＝平台收下）；`Behavior.Recheck` 模擬「其實已經到了」。請求帶 `force`（`EngineDeliverRequest.Force`，DEV-162，只加不刪）＝這次一定重貼、不回 `already`：核心等過那句、對話一直沒有接著做時才帶（SES-76）；不認得這個欄位的引擎照舊重查。`resend`（`EngineDeliverRequest.Resend`，只加不刪）＝核心以前把這一則貼出去過：引擎自己不記得貼過什麼時，只有帶這個才去對話記錄找它在不在；沒帶的一律照常送。核心經轉接層的 `DeliverWith` 帶這兩個。
- **ENG-14** — 替主人在畫面上回答（DEV-46 S3，SES-48）：`AnswerApproval(session, pending, decision, by)`：核心逾時沒接手、確認回到 session 自己的畫面上等之後，主人在看板回答，引擎在那個畫面上替他回答。回 `answered=true`＝按下去了；`answered=false` 帶 `reason`：`sdk.AnswerNone`（`none`，畫面上已經沒有確認框）、`sdk.AnswerUnsupported`（`unsupported`，這種確認引擎不會代答）、`gone`（session 不在了），其他是畫面讀不到、按鍵送不出。拒絕的理由（`decision.message`）原生畫面帶不過去，不保證送到。fake 引擎記錄呼叫，`Behavior.AnswerReason` 空＝答成功。
- **ENG-15** — 一輪出錯（S5-0，計畫 `2026-11-02-status-from-engine`）：這一輪因為平台的錯誤沒做完就停了（不是額度用完；額度用完照 ENG-8 報 `RATE_LIMITED`），引擎報 `TURN_FAILED`：`error` 是錯誤種類 `sdk.EngineErrorKind`——`overloaded`（平台暫時忙或暫時壞，過一下再說一次通常就好）、`auth`（登入失效或帳號不能用，要主人處理）、`billing`（帳單或方案，要主人處理）、`rejected`（平台不收這一輪的請求，例如對話太長，再說一次多半一樣）、`unknown`（其他）；`last_message` 是平台說的原話（給主人看，核心不解讀；沒有原話時引擎填 `sdk.EngineNoPlatformMessage` 這固定一句，核心不把它當原話顯示）。這一輪之後**不會再有** `TURN_ENDED`，`TURN_FAILED` 本身就是這一輪的結束。平台的錯誤怎麼歸到這幾種由引擎決定；種類以後可能變多，核心與看板遇到不認得的值一律當 `unknown`（判斷前先過 `EngineErrorKind.Normalize()`，不要各寫一份清單）。核心收到之後怎麼處理（狀態、那一輪的話、要不要通知主人）在 `session` 與 `inbox` 的卡（S5-2、S5-3）。fake 腳本引擎：文字含「【出錯】」報 `overloaded`、「【出錯:<種類>】」（全形冒號也可以）報指定的種類（不檢查認不認得）。
- **ENG-16** — 對話結束的原因（S5，2026-11-02）：`SESSION_ENDED` 的 `reason` 是一句短的原因；核心只看得懂 `sdk.SessionEndReasonLoggedOut`（`logged_out`：這個帳號登出了，要主人重新登入，`notify` 據此提醒主人，NTF-54），平台自己的登出說法由引擎換成它（法律第 3 條）。其他原因（`stopped`：核心叫停、`gone`：重開後找不到、平台自己的說法）照填，核心不解讀。
- **ENG-17** — 對話指令（DEV-100，D-45）：`Command(session, command, arg, by)`，`command` 是中立的 `SessionCommand`（`clear` 清掉這段對話從頭開始、`compact` 整理成摘要、`model` 換模型、`effort` 換思考力度；後兩個帶 `arg`）。引擎把它翻成平台自己的指令送進去、確認平台收下才回 `done=true`；做不到回 `done=false` 與 `reason`：`gone`（session 不在）、`dialog`（`CommandDialog`，畫面上有確認框開著）、`typing`（`CommandTyping`，輸入框裡有打到一半的字）、`unsupported`（`CommandUnsupported`，這個平台沒有這個指令）、`bad_arg`（`CommandBadArg`，沒有這個模型或力度；選項由引擎認，核心只檢查格式）、其他字串是沒送進去（核心不給主人看原因碼）。`clear` 之後平台換了新的對話 id 時放在 `engine_session_id` 回來（核心換掉接續用的 id）。不是訊息：不產生 `TURN_STARTED`、不算使用者訊息。轉接層補 `by`；回 nil 當 `unavailable`「引擎沒有回覆」。fake 引擎記下呼叫，`Behavior.CommandReason` 不空就回 `done=false` 帶那個原因；`Behavior.CommandSessionID` 是 `clear` 做成時帶回的新編號。測試 `TestCommandThroughAdapter`。
- **ENG-18** — 現在用的模型與力度（D-46）：`EngineStartResponse`、`EngineCommandResponse`、`EngineEvent` 都可以帶 `model`（引擎認得的模型代號或別名）、`model_label`（給人看的名字）、`effort`（力度的詞）。說得出才帶，說不出就空著（核心沿用原本的，SES-60）；帶了 `model` 時 `model_label` 跟著它（沒給＝沒有名字）。解析平台的資料在引擎套件，核心不認得任何模型名（法律第 3 條）。fake 引擎：開的時候照請求的 `model`、`effort`，`model`／`effort` 指令做成了換掉並在結果帶回，`Scripted` 的 `TURN_ENDED` 帶現在的值；名字是「Fake ＋別名首字大寫」（`fake.ModelLabel`，例 `opus` →「Fake Opus」），`Behavior.Unlabeled` 時不給名字。
- **ENG-5** — `SetPermission` 的參數用 EverGenie 自己的詞彙（`PermissionPolicy`），由引擎實作並產生它那個平台的原生設定；產物不准手改（檔頭註明、`doctor` 偵測被改過）。
- **ENG-6** — 上線前，引擎套件是 `serve` 依內建清單啟動的獨立行程（沒有 `pkg` 模組）；P6 之後交給 `pkg` 管理。
- **ENG-7** — 介面有一套一致性測試（`internal/base/engine/conformance`）：對 fake 引擎與 `engine-claude` 跑同一組案例（啟動、送達（文字原樣到）、事件序列（含別的 session 傳來的輸入與直接傳話）、權限確認、問題、動作檢查、對話記錄、停止）。另有 `TurnFailed` 案例（ENG-15：先 `TURN_STARTED` 再 `TURN_FAILED`、`error` 是列出來的種類、`last_message` 不空、這一輪沒有 `TURN_ENDED`），要呼叫端給「讓引擎出錯的文字」，所以只對 fake 引擎跑（經真的 unix socket 傳輸）；真的平台沒辦法隨時造出錯誤，由引擎套件自己的測試用錄下來的樣本守。
- **ENG-8** — 介面帶得動傳話與授權要的資訊，全部用中立詞彙：`Deliver` 帶要送進去的完整文字；`TURN_STARTED` 帶這一輪的輸入與來源（`typed`／`peer`／`system`）、`peer` 的對方名稱、上一輪是否被中斷；`ACTIVITY_STARTED` 可帶「要直接傳話給 X」、`ACTIVITY_ENDED` 再帶成功與否；帳號額度用完時回報 `RATE_LIMITED`（帶平台說的恢復時間 `until`，認不出就空；恢復後照常 `TURN_STARTED`／`TURN_ENDED`，DEV-47 S4、REQ-378）；其他平台錯誤讓一輪停下來時回報 `TURN_FAILED`（ENG-15）；`GetContext` 帶輸入；`CheckAction` 讓引擎在動手前問核心（動作分類 `ActionClass`，回 `allow`／`deny`／`ask`）。
- **ENG-10** — 權限變更申請（Q7，D-26）：`SetGuidanceSource(func() []string)` 與 `SetRuleSource(func(agent string) []sdk.PolicyRule)` 由 `cmd` 組裝時接 `policy.ActiveGuidance`／`ActiveRules`（engine 不 import policy）。握手時的 `SetPermission`、`PushPolicy`、doctor 的 `CheckPermissions`（DryRun）都帶「trust.toml 算出的政策＋目前的 guidance」（`currentPolicy`，不改 trust 那份）；`PushPolicy` 對每個已就緒的帳號重推一次，有一個失敗就回錯誤（還沒就緒的略過，握手時會拿到最新的）。`For(account).Start` 依 `Agent` 填 `Rules`。
- **ENG-11** — 一次性對話 `Ask`（P6-5b，測口吻用）：請求帶 `Agent`、`Cwd`、`Account`、`EngineConfig`、`Model`、`Effort`、`Instructions`、`Message`、`TimeoutSeconds`（0＝600），回 `Reply`（模型的文字）與實際用的 `Model`（拿得到才填）。語意：**不開任何工具、不留 session、不影響常駐 session、不回報事件**；轉接層補上帳號與帳號設定（跟 `Start` 一樣）。引擎不支援就回 `ERROR_CODE_UNAVAILABLE`「這個引擎不支援一次性對話」；`Capabilities.features["ask"] = true` 表示支援（核心不靠它分支，只給 doctor 與看板顯示）。逾時回 `ERROR_CODE_UNAVAILABLE`「回覆逾時」。fake 引擎回 `"[fake] " + Message`（或 `Behavior.AskReply`），可設延遲（`AskDelayMillis`）、一律失敗（`AskError`）、訊息含某段文字才失敗（`AskFailOn`）。一致性測試多一個 Ask 案例（引擎有 `ask` 旗標才跑：回非空文字、不回報事件）。
- **ENG-19** — 新家第一個帳號的帳號設定（DEV-155 S5，2026-11-13）：`evergenie init` 執行預設引擎（`registry.Default`）的執行檔 `<執行檔> account-defaults`（照 `FindBinary` 找；10 秒內），讀它印的一個 JSON 物件（引擎的子行程抓著輸出不放時最多再等 2 秒，init 不會卡住），原樣寫成 `home.toml` 的 `[accounts.main.engine_config]`。核心不解讀內容（ENG-4、法律第 3 條），只收鍵是小寫英數與底線、值是字串或布林的物件。「用這台現在登入的那個帳號」怎麼寫由引擎決定（Claude 引擎見 engine-claude ECL-64）。找不到執行檔、結束碼不是 0、印的不是這種物件：不寫 `engine_config`，`init` 照樣成功，印一行「注意：帳號的設定資料夾沒有自動填上（<原因>），第一次開秘書前請到看板「設定 → 電腦」的「AI 帳號」填。」（安裝程式把「注意：」那行轉給主人）；原因是給主人看的白話（「程式裡少了連 AI 用的那一支」「沒問到這台電腦登入的是哪個 AI 帳號」「問到的帳號設定看不懂」），不寫「引擎」這種內部用語。引擎套件不認得這個子指令時照它自己的用法錯誤結束（非 0），就是上面那種情況。Evidence: `internal/base/engine/account_defaults.go` → `AccountDefaults`；`account_defaults_test.go` → `TestAccountDefaultsReadsEngineOutput`；`cmd/evergenie/initcmd.go` → `engineAccountDefaults`（寫成 TOML）；`initcmd_test.go` → `TestInitMakesBootableHome`、`TestInitWithoutEngineAccountDefaults`。

## NEVER
- **ENG-N1** — 核心裡任何檔案不得出現依引擎名稱分支的程式（`if engine == "…"`、`switch` 到引擎字串）；lint 以 grep 擋。
- **ENG-N2** — 不為其他平台的差異做設計：不做能力協商、降級、多種投遞模式；別家的能力長得不一樣時，到時再擴充介面（D-21）。
- **ENG-N3** — 引擎套件不得 import `internal/`，只能透過 `sdk`。

## 職責
- 引擎登記表（引擎 id → 套件行程的連線）、轉接 `EngineAPI` 的每個方法、`EngineHostAPI` 的伺服器端轉給 `session`（含 `CheckAction`）、一致性測試。

## 不負責
- session 的狀態與通知時機 → `session`；平台的任何細節 → 引擎套件（目前是 `engine-claude`）。

## 介面
API：`sdk/engine.go` 的 `EngineAPI`（引擎實作，核心是 client）與 `EngineHostAPI`（核心實作，引擎是 client）。Go：
```go
package engine

type Engine interface {
    Capabilities(ctx context.Context) (Capabilities, error)
    Start(ctx context.Context, s StartSpec) (Started, error)
    Stop(ctx context.Context, name string) error
    Deliver(ctx context.Context, session, envelopeID, text string) (delivered bool, reason string, err error)
    Transcript(ctx context.Context, session, sinceUUID string, follow bool) (<-chan ChatMessage, error)
    SetPermission(ctx context.Context, p PermissionPolicy, account string) ([]string, error)
    BootPrompts(ctx context.Context, session string) ([]BootPrompt, error)
    AnswerBootPrompt(ctx context.Context, id, choice, by string) error
    Usage(ctx context.Context, account string) (Usage, error)
    WriteAgentCards(ctx context.Context, cards []AgentCard, dryRun bool) ([]AgentCardFile, error) // Q3：agents link
    Preflight(ctx context.Context, account string) ([]PreflightCheck, error) // Q4：程式與登入
    Ask(ctx context.Context, s AskSpec) (Asked, error) // P6-5b：一次性對話（ENG-11）；Account／EngineConfig 由轉接層填
    AnswerApproval(ctx context.Context, session string, p *sdk.Pending, d *sdk.Decision, by string) (answered bool, reason string, err error) // DEV-46 S3（ENG-14）
    Command(ctx context.Context, session string, cmd sdk.SessionCommand, arg, by string) (*sdk.EngineCommandResponse, error) // DEV-100（ENG-17）
}
type Registry struct { /* 登記表，內部同步保護 */ }
func NewStatic(accounts map[string]sdk.EngineAPI) *Registry
func (r *Registry) For(account string) (Engine, error)
// 新家第一個帳號的 engine_config（ENG-19）：問不到回 nil 與給主人看的白話原因
func AccountDefaults(ctx context.Context, engineID string) (map[string]any, string)
```

`Started`（`sdk.EngineStartResponse`）除了 `engine_session_id`、`remote_access_url`，Q2 起多一個 `attach`：從終端機接上畫面的一行指令（平台的寫法由引擎決定，核心只存在 session 上、CLI 原樣印出；引擎沒有這個能力就空）。Q5 起 `EngineStartRequest` 多 `permission_level`（每位秘書的權限等級，引擎自己對應原生設定）。

### P1 骨架對照
- Evidence: `internal/base/engine/module.go` → `Module`、`Deps`；形狀測試 `internal/base/engine/module_test.go` → `TestModuleShape`（REQ-118）。
- P3 已實作帳號登記、EngineAPI 轉接、SetHome、引擎行程監督與 Tick；socket 與 Capabilities 就緒後，每個帳號設定一次權限。缺執行檔不阻止核心啟動，行程失敗最多重啟五次。NewStatic 以帳號名對應測試 API；Module.For 則依 home.toml 選引擎。
- fake 引擎可設定投遞結果、事件、確認、問題、動作檢查及對話內容，記錄呼叫與 host 回應。一致性測試由 conformance.NewHost 記錄反向呼叫、Run 檢查正向呼叫與事件；真引擎測試要提供帳號／引擎設定與 HostEndpoint，使用測試工作目錄。
- API 資料型別直接使用 SDK 的型別別名（實際對照見 module.go）；模組與 API 共用同一份資料，不另做型別轉換（D-5、MOD-N5）。

### P4 實作對照（S3：給 e2e 用的假引擎執行檔）
- Evidence: `internal/base/engine/fake/cmd/evergenie-engine-fake/main.go`（入口，`serve`）、`internal/base/engine/fake/script.go`（`NewScripted`、`NewScriptedContext`，腳本引擎）；`internal/base/engine/registry/builtins.go` 多一筆 `"fake": "evergenie-engine-fake"`；測試 `internal/base/engine/fake/script_test.go`。正式安裝不會有這個執行檔，帳號沒用到 `fake` 引擎就不會被啟動。
- 入口照 `engine-claude` 的做法：`engine` 模組用 `<執行檔> serve` 啟動，環境變數 `EVERGENIE_HOME`、`EVERGENIE_API_SOCKET`、`EVERGENIE_ENGINE_SOCKET`（`engine-fake.sock`）；建 `sdk.NewEngineHostClient(sdk.Dial(EVERGENIE_API_SOCKET))`，`sdk.Serve(ctx, EVERGENIE_ENGINE_SOCKET, {"engine": 腳本引擎})`，SIGINT／SIGTERM 結束。
- 腳本引擎嵌入共用的 `fake.Engine`（訊息儲存、`Transcript`、`Start`／`Stop` 事件），只換 `Deliver`：馬上回 `delivered`，劇情在背景跑（同一個 session 一輪一輪排隊）：`TURN_STARTED`（`input`＝文字、`source: typed`）→ 記一則 `user` 訊息 → `ACTIVITY_STARTED`（`activity: "Read"`；對話記錄的 `tool_call` 帶 `tool_label: "讀檔"`，SES-65）→ 300ms（文字含「【慢】」改 3 秒）→ `ACTIVITY_ENDED` → 記一則 `assistant`「收到：<文字>」→ `TURN_ENDED`（`last_message` 同上）。文字含「【權限】」：回覆前 `RequestApproval`（`permission`、`tool: "Bash"`、`summary: "建立檔案 e2e.txt"`），回「已允許」或「已拒絕：<理由>」；含「【選擇】」：`RequestApproval`（`question`，一題「要哪一個？」選項 A、B），回「你選了 <答案>」；含「【選說明】」：一題選項帶說明、可自己打字；含「【多題】」：一次問三題（第一題可多選、說明很長，WEB-122）；含「【出錯】」（或「【出錯:<種類>】」，全形冒號也可以）：記完 `user` 訊息就報 `TURN_FAILED`（`error: overloaded` 或指定的種類、`last_message: "假引擎：這一輪出錯了"`），不做事、沒有 `TURN_ENDED`（ENG-15）。文字含「【多工具】」：讀檔之後再用跑指令 12 次、找內容 2 次、讀檔 1 次、一個沒有白話的 `OldTool`（共 17 個工具，看板測合併與退回，WEB-125、WEB-126；`TestScriptedToolCallsCarryPlainLabel`）。文字含「【登出】」：記完 `user` 訊息就報 `SESSION_ENDED`（`reason: sdk.SessionEndReasonLoggedOut`），session 跟著不在（之後的 `Deliver` 回 `gone`，ENG-16；S5-2，`TestScriptedLogoutEndsSession`）。文字含「【不回】」：從這一句起引擎整個不回應（`Engine.Stick`）——這一句的 `Deliver` 與之後每個呼叫都卡著，等到呼叫的人自己放棄（ctx 結束）才回錯誤；模擬引擎行程還在、socket 接得上、就是不回，測核心與看板不會跟著卡（WEB-175，`TestScriptedStuckHoldsEveryCall`、e2e `engine-stuck.spec.ts`）。文字含「【這位不回】」：只有收到這句的那個 session 不回應（`Engine.StickSession`）——她的 `Deliver` 與之後對她的 `Start`、`Stop`、`Transcript`、`ListBootPrompts`、`AnswerApproval`、`Command` 都卡到呼叫的人放棄，別的 session 照常；測一位卡住不拖累別位（SES-72，`TestScriptedStuckOneHoldsOnlyThatSession`、e2e `secretary-stuck.spec.ts`）。Go 測試另外可用 `Engine.SlowSession(session, d)`（她的每個呼叫拖 `d` 才回 `unavailable`）與 `Engine.LandLate(session, on)`（`Deliver` 等呼叫的人放棄之後才真的記進對話、回錯誤；配 `Behavior.Recheck` 測「核心放手後其實送到了」不會貼成兩則），`TestSlowSessionAndLateLanding`。`Start` 之後 session 從 `SESSION_STARTED` 直接是 `idle`，不需要補 `TURN_ENDED`。
### P5 實作對照（S5：doctor 要的檢查）
- Evidence: `sdk/engine.go` → `EngineSetPermissionRequest.DryRun`、`EngineSetPermissionResponse.Drift`；`internal/base/engine/doctor.go` → `Module.CheckPermissions`、`Module.Statuses`、`Module.Attach`、`FindBinary`；`internal/base/engine/fake/engine.go` → `Behavior.Drift`；`internal/base/engine/conformance/conformance.go`（`Run` 多一個 DryRun 案例）；測試 `internal/base/engine/doctor_test.go` → `TestCheckPermissionsDryRunReportsDrift`、`TestStatusesAndAttach`（REQ-117、REQ-84）。
- `DryRun`：引擎**不寫任何檔**，只拿現有的產物跟目前政策比對，不一致的地方一條一句白話放進 `Drift`（空＝一致）；`Artifacts` 留空。一致性測試（TST-11）要求：DryRun 不能回報寫了檔。
- `Module.CheckPermissions(ctx) (map[帳號][]string, error)`：對每個設定過的帳號送 `SetPermission(DryRun)`（每個帳號 3 秒上限）；空串列＝一致；該帳號的引擎沒在跑，值是 `NotRunning`（「這個帳號的引擎沒有在跑，無法檢查」）；呼叫失敗是 `CheckFailedPrefix`＋引擎回的白話。doctor 只報告，不替引擎啟動。
- `Module.Statuses()` 依引擎 id 回 `Known`（內建登記表有）、`Found`（執行檔找得到）、`Ready`（已握手）與用它的帳號；`Attach(id, api)` 把一個已經在跑的 `EngineAPI` 直接接到引擎 id 上（測試與組裝用，不經行程監督）。


## 事件
None.（事件由引擎經 `EngineHostAPI.ReportEvent` 送進 `session`，不經過這個模組。）

## 儲存
None.（引擎的產物在 `runtime/engine-<id>/`，屬引擎套件。）

## 依賴
- 啟動前由 registry 呼叫 `SetTrustSource(trust.Policy)`；未接來源時啟動回錯。底層只消費政策，不判定信任規則。
`platform`（import）。事件由 registry 接上 bus 的發布函式，不 import。

## 錯誤
`ERROR_CODE_NOT_FOUND`（沒有這個引擎）、`ERROR_CODE_UNAVAILABLE`（引擎套件行程不在）、其餘原樣轉回引擎的錯誤碼。

## 不變條件
- `Usages` 對沒有資料的帳號說明原因、引擎回錯誤放進說明、引擎沒在跑回 `NotRunning`；`Warmup` 只動標了的帳號，失敗發通知；`LimitWatch` 重置前不通知、重置後只通知一次、安靜時段與過期都不通知。
- `Ask` 不開 session、不回報事件、轉接層補上帳號；引擎的失敗原樣轉成平台錯誤（fake 引擎與轉接層的測試；`engine-claude` 的 argv 不含 `--settings`／`--allowedTools`／`--resume`／`--name`）。
- `Preflight` 兩個帳號共用同一個引擎時，同一個 id 的檢查只列一次。
- `internal/` 底下（除 `internal/base/engine/registry` 的設定對照表之外）沒有任何引擎 id 字串，也沒有平台專有名詞。
- `sdk/*.go` 沒有平台專有名詞（hook、tmux、MCP 及各家名稱等，詞表在 lint 設定）。
- 換掉引擎實作（用 fake 引擎）後，`session` 的全部測試照過。
- `engine-claude` 與 fake 引擎通過同一組一致性測試。
- 出錯的那一輪：fake 引擎先報 `TURN_STARTED` 再報 `TURN_FAILED`（列出來的種類＋平台原話），沒有 `TURN_ENDED`；`error`、`last_message` 經 unix socket 原樣到核心那一端；不認得的種類 `Normalize` 成 `unknown`（ENG-15，`TestConformanceTurnFailedFakeEngine`、`TestScriptedFailReportsTurnFailed`、`TestEngineErrorKindNormalize`）。
- `SetPermission` 的輸入不含任何平台專有欄位名。
- `StartRequest.engine_config` 原樣到達引擎、核心不讀它的內容。
- `Deliver` 收到的文字跟 `session` 給的逐字相同（fake 引擎記錄）。
- `CheckAction` 的請求與回應只有中立欄位（`session`、`class`、`tool`、`summary`、`verdict`、`reason`）。

## 測試計畫
- Q4：`TestUsagesReportsEachAccount`、`TestWarmupOnlyFlaggedAccounts`、`TestLimitWatchNotifiesOnceAfterReset`、`TestLimitWatchQuietHoursAndStale`（REQ-86，REQ-166）、`TestPreflightMergesAccounts`（REQ-165）（`internal/base/engine/usage_test.go`）
- P6-5b：`TestAskThroughAdapter`、一致性案例的 Ask（`TestConformanceFakeEngine`）（REQ-130）
- DEV-46 S3：`TestAnswerApprovalThroughAdapter`（REQ-377，ENG-14）；一致性測試還沒有 AnswerApproval 案例（真的 Claude 要開出原生確認框才測得到）
- `TestNoEngineNamesInCore`、`TestNoVendorTermsInCoreAndProto`（REQ-87、REQ-144，lint）
- `TestRegistryPicksByAccount`、`TestUnknownEngineErrors`（REQ-83）
- `TestSessionSuitePassesWithFakeEngine`（REQ-87，跑 `session` 的測試套件，注入 fake）
- `TestConformanceFakeEngine`、`TestConformanceEngineClaude`（REQ-144；後者是 `integration` tag）
- P5-S5：`TestCheckPermissionsDryRunReportsDrift`（REQ-117、REQ-84）、`TestStatusesAndAttach`（REQ-117）
- `TestEngineConfigPassThrough`（REQ-144）、`TestPermissionPolicyHasNoVendorFields`（REQ-84）

## 設計決定
- **D-6** — 每個 AI 平台做成一個引擎套件，實作核心定義的 `EngineService`；核心不准有依引擎名稱分支的程式（法律第 3 條）。現在只做 Claude；別家等整套架構在 Claude 上做完才接。
- **D-7** — 權限只有一個 `SetPermission(PermissionPolicy)` 介面，用 EverGenie 的詞彙、照 Claude 的需要設計，由引擎產生原生設定（產物不准手改）。執行期確認：引擎把要主人確認的事轉成 `RequestApproval`，核心（看板、手機按鈕）回 `Decision`，引擎再轉回平台格式。不為別家引擎預留相容設計。
- **D-21** — 跟 AI 平台有關的（啟動與關閉、事件與狀態、訊息投遞、權限與確認、問答、對話記錄、遠端存取、用量、帳號設定）一律先在核心用 EverGenie 的詞彙定介面，實作放引擎套件；`internal/`、`sdk/`、資料 schema 不得出現平台專有名詞（lint 擋）。介面的形狀只照 Claude 有的能力切：不拿別家比、不做能力協商、降級、多種投遞模式；但留空格（引擎設定整包不透明地傳、預留能力旗標欄位），之後加平台是並排實作介面、不改寫核心。介面有一致性測試。否決：先把 Claude 寫死在核心再抽象化；一開始就通用化。

## 已知限制
- Mode 目前固定為 ask；可配置 Mode 留待後續定案。
- 只有 Claude 一個引擎（D-6）；介面照 Claude 的能力切，別的平台的差異沒有設計過，到時再擴充（D-21）。
