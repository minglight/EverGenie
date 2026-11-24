# 模組：chat（`internal/feature/chat`）
Status: DRAFT — 2026-11-02 S8-3 合併 chat.md，pending author review.

**目的：** 對話與傳話的入口：sdk 的 `session`（開關、串流、授權與問答、討論串）、`bus`（傳話封包）、`engine_host`（引擎行程回報）三個服務的 HTTP、`web` 的啟動提示兩個方法，以及 `evergenie sessions …`、`status`、`say` 指令。session 執行期與事件佇列是底層，這裡只是它們的門面。
**種類：** 業務塊（7 塊之一；沒有子套件，根目錄就是原本的 chat 模組）

## MUST
- **CHT-1** — `Routes()` 登記 sdk 的 `bus`、`session`、`engine_host` 三個服務（`http.go`），加 `web.get_boot_prompts`、`web.answer_boot_prompt`（`bootprompts.go`，回答只有主人；看板 `BootPromptBanner` 全站上方顯示）。寫入類（`session.resolve`、`mark_seen`、`bus.publish`）都要 `by`，沒帶回 `invalid`（欄位 `by`），底層不會被呼叫；錯誤經 `platform.SDKError` 轉成 API 錯誤。舊的掉包回報 `report_incident` 已廢（登記在 `api.removed`）。
- **CHT-2** — 從單開專屬對話：`session.open` 的 `Topic` 是這個家的任務單號（`platform.ReadIDPrefixes(home)` 判斷）時，只讀任務單決定負責的秘書（`req.Agent` 優先），再交 `session.OpenWork`；其他 topic 直接 `session.Open`。任務單由 `Deps.Tasks`（只用 `Get`）取得，由 registry 接 tasks 塊。
- **CHT-3** — `session.resolve` 把授權或問答的回答送回等待中的引擎；沒有 `decision` 回 `invalid`（欄位 `decision`）。
- **CHT-4** — `session.watch` 推出去的每一筆量「引擎事件 → 推進串流」的延遲（`latency.go`）：第一次看到的現況不算、事件時間往前走才算、每個 session 分開算；樣本記進自己的樣本袋（`latency_log.go`，只在記憶體，最多 5000 筆），讀寫都在 chat。
- **CHT-5** — 指令（`cli.go`）：套件層級 `Commands()` 登記 `sessions list|opened|open|close|start|stop|restart|rotate|peek|answer|attach`、`status`、`say`；只用 `sdk` client 與 `cmdline`；狀態白話 `StateLabel`／`PendingLine` 在 `labels.go`（`agents mv` 也要，registry 把 `chat.StateLabel` 傳進 `agents.Commands`）。`say` 是直接傳話不通時（例如跨帳號）用，經 `bus.send` 送 `say` 封包。
- **CHT-6** — 路由與指令都只看 `cmd/evergenie/registry.go` 有沒有把本模組交給 `app` 與 `featureCommands()`，沒有全域登記。測試 `http_test.go` → `TestRoutesRegisterChatServices`、`TestByRequired`、`TestOpenTaskRoomDelegatesToOpenWork`、`TestResolveWiring`、`TestAdaptersPassThrough`、`TestHostUnavailableWithoutSessions`、`TestStateLatencyFromEngineEventToStream`；`cli_test.go` → `TestCommandsListsAllChatCommands`、`TestSessionsCLIOnFakeEngine`、`TestSayJoinsWordsAndSendsSayKind`。
- **CHT-7** — 健檢（`checks.go`，`Checks()`，doctor 的 `sessions` 群組）：`sessions.resident`（`RunAll`：每位啟用中的常駐秘書卡在引擎登記的提示（信任資料夾、話一直貼不進去等，訊息「<秘書> 卡住了：<提示>」，經 session 的 `BootPrompts` 問引擎，`TestResidentCheckFlagsBootPrompt`）、或家設定自動啟動卻沒在跑，各一條黃字；引擎沒動靜不算問題，舊狀態 `stuck` 不報；都正常一條綠字；名冊經 `Deps.Roster`，沒接就不登記）、`sessions.latency`（今天主人的話送出到收到 p50／p90，綠字；session 有 `ReceiveLatency` 才登記）、`sessions.state_latency`（看板收到對話狀態的延遲，綠字）。測試 `checks_test.go` → `TestResidentCheck`、`TestReceiveLatencyCheck`、`TestStateLatencyCheck`。
- **CHT-8** — 首頁（`board.go`，`Board()`，inbox 塊的 overview 收）：`waiting_threads` 是所有秘書、不含已停的討論串裡狀態 `waiting_user` 的（`[]sdk.ThreadSummary`）；沒接 `Deps.Sessions` 就不登記。測試 `board_test.go` → `TestBoardWaitingThreads`。
- **CHT-9** — 語音入口（`voice.go`，DEV-144）：`web.voice_send {text, to?}` 給手機、手錶的捷徑用（聽寫在手機上做，核心只收文字；看板 `/api/web/voice_send`，tailnet 內不帶 Origin 算同源）。只有主人；空的、超過 2000 字擋下。決定給誰的順序：`to`（職位 id 或名字，找不到回 `invalid`）→ 開頭叫名字（「跟露娜說…」「雅典娜，…」，名字含以前的名字與職位 id，後面要接標點、空白或「說」「幫我」「你」；英文名字只接空白不算，避免「Code review…」被當成叫 code；拿掉稱呼再送）→ 問引擎一次（session 的 `QuickAsk`：家的預設帳號、`[voice]` 的模型與力度（預設 sonnet、low）、45 秒；任務與名冊放在訊息裡、原文框在 `<<< >>>`，只放系統指令時引擎會把內容當成要做的事）回 `{agent,text,sure}`，修正後變長很多就用原文；修正後的內容提到這個家的任務單號（不分大小寫）而且都是同一位負責人就改給她 → 都不行就交給 `[voice] fallback`（預設 admin），用原文、標不確定。話一律以主人身分 `bus.send` chat，內文後面一行註明是語音、有改字附原文並說「意思不同以原文為準」、不確定時說「先交給你」；送出前捷徑已斷線（ctx 取消）就不送、回 `unavailable`，免得主人重講變兩則；回 `reply` 一句給捷徑顯示。測試 `TestVoiceSendOnlyOwner`、`TestVoiceSendAddressedSkipsEngine`、`TestVoiceSendNameNeedsBoundary`、`TestVoiceSendAddressedSpacedAndFormerNames`、`TestVoiceSendGuessCorrectsText`、`TestVoiceSendFallsBackWhenEngineFails`、`TestVoiceSendConfigFallbackAndModel`、`TestVoiceSendFallbackWithoutAdminUsesFirst`、`TestVoiceSendLongCorrectionKeepsRaw`、`TestVoiceSendCanceledDoesNotSend`、`TestVoiceSendExplicitTo`、`TestVoiceSendRoutesTaskToOwner`、`TestVoiceMessageFencesRawText`。
- **CHT-10** — 語音問題的簡短回答（`voice.go`）：引擎在 CHT-9 那一次判斷順便回 `question`（指名或走備援時用字面判斷：問號、嗎、呢、幾點、多少、做到哪…）。是問題而且沒帶 `no_wait`，核心開一個 `V-` 代號記住問誰，傳話內文請秘書先用 `evergenie voice answer <代號> <一兩句>`（`web.voice_answer`）回，再照常在對話裡詳細講；`voice_send` 等她回答，連猜秘書的時間一起最多 50 秒（等回答本身最多 35 秒、至少 8 秒，捷徑約一分鐘放棄），回應 `speak`＝她的回答（`answered`＝true，空白壓成一個、超過 120 字截斷），等不到就說「還在想，等一下到看板看」。只有收到那句話的秘書能回（別人 `forbidden`）；只收第一個；逾時、斷線或答過的代號回 `delivered=false`（不算錯）。代號只在記憶體，服務重開就沒了。捷徑：`reply` 跳通知、`speak` 唸出來（WEB-170）。測試 `TestVoiceQuestionWaitsForAnswer`、`TestVoiceQuestionTimesOutAndNoWait`、`TestVoiceGuessQuestionFromEngine`。
- **CHT-11** — 健檢 `sessions.stuck`（`checks.go` → `stuckCheck`，DEV-162）：session 有 `Stuck` 才登記；每個話送不進去的對話（SES-77）一條紅字（要處理）「〈秘書〉有 N 則話送不進去，已經等了 M 分鐘」（不是主要對話就加對話名；幾分鐘用 session 算好的 `Minutes`），`fix` 是重開對話、不行就到那台 Mac 開「修復」；都沒有一條綠字。測試 `checks_test.go` → `TestStuckCheck`。

## NEVER
- **CHT-N1** — 不 import 別的業務塊（depcheck 照路徑擋）；要別塊的能力就在自己的 `Deps` 定義小 interface，由 registry 注入。
- **CHT-N2** — 不自己管 session 的狀態與派送（底層 session）、不自己存封包（底層 bus）。
- **CHT-N3** — 不在 `internal/api`、`internal/cli` 留第二份。

## 職責
- 三個服務的 HTTP adapter、操作者驗證、單號 topic 轉 `OpenWork`、語音入口（CHT-9、CHT-10）、狀態延遲的量測、`sessions.*` 健檢、首頁的 `waiting_threads`；指令的旗標與白話輸出。

## 不負責
- session 的生命週期、派送器、回音、討論串狀態 → 底層 session；封包佇列與歷程 → 底層 bus；引擎行程怎麼跑 → 底層 engine；健檢的收集與顯示、看板的健康頁（`web.get_health`）→ system 塊的 doctor；首頁的彙整 → inbox 塊的 overview。

## 組裝（塊的 Module，S8-3）
chat 塊只有一個子模組，就是 `chat/module.go` 的 `Module`，不另包（S8-3）。Deps 都是塊外的（S8-5 看過，沒有多餘的）：Sessions（session 的對話，含 `EngineHost()`）、Bus（傳話）、Tasks（只用 `Get`：單號 topic 決定負責的秘書）、Roster（只用 `List`：`sessions.resident` 健檢，可以是 nil）、Clock（可以是 nil）。Start 只開 log。測試 `module_test.go`。

## 介面
sdk 服務 `BusAPI`、`SessionAPI`、`EngineHostAPI`（見 `sdk`）。Go：
```go
func New(d Deps) *Module
func (m *Module) Name() string
func (m *Module) SetHome(dir string)
func (m *Module) Start(ctx context.Context) error // 只開 logger
func (m *Module) Stop(ctx context.Context) error
func (m *Module) Routes() []app.Mount             // app.RouteProvider
func (m *Module) Checks() []app.Check             // app.CheckProvider
func (m *Module) Board() []app.BoardPart          // app.BoardProvider
func Commands() map[string]app.Command            // 套件層級函式
```
`Sessions` 之外，session「額外有」的 `WithMessages`（把還沒結束的話填進 `Session.Messages`）用型別斷言，假物件可以沒有。

| 檔案 | 內容 |
|---|---|
| `module.go` | `Module`、`Deps`（小 interface）、`New`／`Start`／`Stop` |
| `http.go` | `Routes()`＋實作 `BusAPI`、`SessionAPI`、`EngineHostAPI` 的三個 service |
| `bootprompts.go` | 看板列出與回答秘書卡住的啟動提示（`web` 服務的兩個方法） |
| `latency.go`、`latency_log.go` | `session.watch` 串流的狀態延遲量測與樣本袋 |
| `checks.go` | `Checks()`：`sessions.*` 健檢 |
| `board.go` | `Board()`：首頁的 `waiting_threads` |
| `cli.go`、`labels.go` | `Commands()`：`sessions …`、`status`、`say`；狀態白話 |
| `doc.go` | 套件說明 |


## 事件
沒有自己發或收的事件（`bus.publish`、`bus.subscribe` 只是把呼叫轉給底層 bus）。

## 儲存
`schemas/session.md`、`schemas/message.md`（資料由底層 session、bus 寫）。

## 依賴
`platform`、`app`（登記介面與 `cmdline`，import）。對話、訊息匯流排、任務單、名冊都不 import：在自己的 `Deps` 定義小 interface（`Sessions`、`Bus`、`Tasks`、`Roster`），由 `cmd/evergenie/registry.go` 接上底層 session、bus 與 tasks、agents 塊。

## 錯誤
照底層 session、bus 回的錯誤；本模組自己回：`invalid`（寫入沒帶 `by`、授權沒有 `decision`）、`unavailable`（session 沒接上時的引擎回報）。

## 不變條件
- 沒帶 `by` 的寫入一定被擋下，底層不會被呼叫。
- 單號 topic 一定走 `OpenWork`，不直接 `Open`、也不自己組第一句、不送封包；找不到單就不開 session。
- 同一個事件的後續推送、連上時的現況，不重複算進狀態延遲。
- 指令表跟其他業務塊的不重名（同名會在 `featureCommands()` 合併時被擋）。

## 測試計畫
- `http_test.go`、`cli_test.go`（見 CHT-6）、`checks_test.go`（CHT-7）、`board_test.go`（CHT-8）；`cmd/evergenie` 的整合測試（`hg` 帶 `featureCommands()`、真的 session 與假引擎）；`cmd/evergenie/api_test.go` 的 `TestEverySDKRouteIsMounted` 守這三個服務的每個方法都有人掛。

## 已知限制
- 樣本袋只在記憶體，serve 重開就從頭算。
