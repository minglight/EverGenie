# 模組怎麼開

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

開一個新的模組（底層或業務）、改 registry 或生命週期之前讀。兩層、依賴規則、組裝在 `architecture.md`，這份只講「怎麼開」。

## MUST
- **MOD-1** — 每個模組一個資料夾：底層 `internal/base/<名>/`、業務 `internal/feature/<塊>/<名>/`（7 塊，2026-11-02 S8-2；判斷法見 `architecture.md`「兩層」）；入口檔 `module.go`，形狀固定：`type Module struct`、`func New(deps Deps) *Module`、`Name() string`、`Start(ctx) error`、`Stop(ctx) error`。
- **MOD-2** — 跨塊、跨底層的依賴一律是**使用方在自己這個套件裡定義的小 interface**，由 `cmd/evergenie/registry.go` 注入具體實作；不 import 對方的具體型別。例外：同一塊的子套件彼此直接 import（只能單向、不能成環），不為了塊內或「以前是兩個模組」另定介面或函式參數，建構順序要反過來的在塊根目錄的 `New` 接；只有測試要換掉的才留介面（例 notify 的 `PushChannel`）（2026-11-02 S8-5）。`Deps` 的欄位名寫接進來的是什麼（`Vault`、`Goals`、`Sessions`…）；時鐘直接用 `platform.Clock`（system、trust、packages 已改；底層與其他塊還有自己定義的 `Clock` interface，碰到時順手改）；依賴方向只照路徑規則擋 import。
- **MOD-3** — `registry.go` 把底層模組與 7 個業務塊（塊根目錄的 `Module`，子模組由塊的 `app.Group` 依序啟停，APP-13）放進一份 `app.New(…)` 清單，被依賴的放前面；這份清單就是順序：依序 `Start`、關閉時反向 `Stop`、遇錯回滾。
- **MOD-4** — `Start` 只開背景工作並立刻回傳；做不完的事放 goroutine，用 `ctx` 取消；`Stop` 等 goroutine 結束並有逾時（預設 10 秒）。
- **MOD-5** — 新增業務模組先放進現有的業務塊（`internal/feature/<塊>/<名>`）：在塊根目錄的 `Module` 加這個子模組（`app.NewGroup` 的清單）、`Commands` 合進去，說明卡是塊那張加一段（DOC-11），`architecture.md`「業務塊」表那一列補子套件。真的要開新塊或新的底層模組，同步做三件事：`ai_docs/modules/<名>.md` 說明卡（「種類」行寫「底層模組」或「業務塊」）、`INDEX.md` 的路由、`architecture.md` 的「底層模組」或「業務塊」表加一列。兩張表都是 depcheck 的設定（底層模組表第二欄是它可以 import 的底層），沒登記 depcheck 擋；卡的種類、依賴、每塊一張 doccheck 擋。
- **MOD-6** — `architecture.md`「依賴規則」允許的方向（業務塊 → 底層、底層照「底層模組」表往下、經 `Deps` 小 interface 注入的）直接呼叫 interface；其他方向發 `bus` 事件（見 `messaging.md`）。
- **MOD-7** — 每個模組在 `module.go` 的頂端註解寫一行對應的說明卡路徑（舊的 REQ 範圍留著，不用補）。
- **MOD-8** — 模組啟動時向 `vault` 註冊自己的資料種類 schema（`RegisterSchema`），不在別處硬編欄位。
- **MOD-9** — 凡是跟 AI provider／平台有關的能力（啟動、事件、訊息投遞、權限與問答、對話記錄、遠端存取、用量、帳號設定…），一律先在核心用 EverGenie 自己的詞彙定義 interface，實作放進引擎套件（D-21）。介面的形狀只照 Claude 有的能力來切，並留空格：引擎自己的設定整包不透明地傳過去（`engine_config`）、預留能力旗標（`features`）。

## NEVER
- **MOD-N1** — 不用全域變數或 `init()` 註冊東西（registry 是唯一的組裝點）；防止依賴方向被藏起來。
- **MOD-N2** — 不違反 `architecture.md`「依賴規則」：底層不 import 業務塊、業務塊不 import 別的塊、底層之間只照「底層模組」表；要別塊的能力，用自己 `Deps` 的小 interface 由 registry 注入。import 由 depcheck 擋，不靠 review。
- **MOD-N3** — 不在核心裡寫引擎或套件的名稱分支（`if engine == "…"`）。
- **MOD-N4** — 不在 `New` 裡做 I/O 或開 goroutine。
- **MOD-N6** — 不拿其他平台跟 Claude 比較來設計介面，也不為別家可能長得不一樣而加能力協商、降級、多種模式；別家的能力真的不同時，到時再擴充介面（D-21）。
- **MOD-N7** — 核心的 `internal/`、核心的 `sdk/` 與核心執行檔 `cmd/`（主人看得到的訊息也在這裡；`cmd/` 的測試檔接真的引擎跑整合測試，不算）不出現任何平台專有名詞（hook、tmux、MCP、各家名稱與設定檔名…）；給主人看的字改說「終端機工具」「AI 程式」這類。詞表與檢查在 `internal/tools/enginecheck`（`TestCmdScannedButNotItsTests`）。
- **MOD-N5** — 不為同一份資料寫兩套型別再互相轉換：會出現在 API 上的資料（任務、待辦、Goal、秘書、訊息…）直接用 `sdk/` 的 struct，模組與 `api` 都用它，不另做一份；只在模組內部用的型別才放模組自己（D-5，2026-10-23 拿掉 proto 後改寫）。

## 範例
```go
// internal/feature/tasks/reminder/module.go — 說明卡：ai_docs/modules/<塊>.md（REQ-59–71）
package reminder

type Deps struct {
    Vault     VaultStore      // 在本檔定義的 interface
    Notify    Notifier
    Scheduler Scheduler
    Bus       Bus
    Agents    AgentLookup
    Clock     platform.Clock
}

func New(d Deps) *Module                       { /* 只存欄位 */ }
func (m *Module) Name() string                 { return "reminder" }
func (m *Module) Start(ctx context.Context) error { /* 註冊 schema、訂閱事件、排程到期時間 */ }
func (m *Module) Stop(ctx context.Context) error  { /* 取消訂閱、等 goroutine */ }
```

## 業務模組與塊
兩層的判斷法、7 塊的清單、依賴規則、資料擁有者在 `architecture.md`；這一節只講資料夾形狀與登記方式。

- **業務模組** `internal/feature/<塊>/<名>/`：塊裡一件業務一個子套件（Go 套件名照資料夾名）；只給同一塊用、沒有 HTTP 與指令的函式庫也放在那塊裡（`inbox/push`、`system/host`），不用 MOD-1 的形狀。自己的 HTTP、指令、健檢、內建定時工作、首頁資料都在自己資料夾登記。
- **塊的組裝**：塊根目錄 `internal/feature/<塊>/module.go` 只組裝、不放規則：`Deps`（只列塊外的東西）、`New(Deps) *Module`（內嵌 `*app.Group`，APP-13：子模組的 Start 順序、Stop 反序、登記合併）、塊外建得比較晚的依賴用 setter（tasks 的 `SetSessions`、inbox 的 `SetTasks`）、套件層級 `Commands(…)` 用 `cmdline.Merge` 合子套件的指令（要別塊的東西當參數收，例 `trust.Commands(system.PrintDoctor)`）、`module_test.go` 用假物件組起整塊跑一輪 Start／Stop。只有一個子模組的塊（chat）就是那個 Module，不另包。

資料夾形狀（沒有的就不建）：
```
internal/feature/<塊>/<名>/
  module.go   Module、Deps（小 interface）、New／Start／Stop，以及選用的登記方法
  service.go  規則與資料操作
  schema.go   資料種類（MOD-8）
  http.go     實作 sdk 的服務介面；Routes() 交給 app 掛
  cli.go      套件層級函式 Commands()：「<名> add|set|done|list」
  events.go   發／收的 bus 事件
  doc.go
```

登記介面（定義在 `internal/base/app`）。業務模組**實作哪個就登記哪個**；不用全域變數或 `init()`（MOD-N1），是否被收集只看它在不在 `app.New` 的清單（塊裡的子模組經塊的 `app.Group`）：
| 介面 | 方法 | 用途 |
|---|---|---|
| `RouteProvider` | `Routes() []app.Mount` | 要掛的 sdk 服務（`Mount{Service, Impl}`）；一個服務被好幾個業務模組分著掛時用 `Mount.Methods` 只登記自己的方法（`system`、`web`），同一方法只能有一個登記者 |
| `CheckProvider` | `Checks() []app.Check` | 健檢（`Check{Name: "群組.項目", Run 或 RunAll}`，結果有 `Fix`）；doctor 收集。底層的檢查由它的門面業務模組登記；`app/board` 例外直接實作 |
| `JobProvider` | `Jobs() []app.Job` | 內建定時工作（`timer.JobRunner` 跑）。不在 `Start` 自己開固定週期的 ticker；開機要立刻做一次的在 `Start` 先做一次 |
| `BoardProvider` | `Board() []app.BoardPart` | 首頁資料段（`Key` 是 `WebGetOverviewResponse` 欄位的 json 名，全域唯一）；選單徽章向 overview 拿同一份 |
| `NoticeSourceProvider` | `NoticeSources() []app.NoticeSource` | 自己處理回答的通知來源（APP-11、NTF-48） |
| （套件層級函式） | `func Commands() map[string]app.Command` | 指令。**不是 Module 的方法**：指令行程只是 client，不建 Module；合併時名稱重複回錯 |

消費別人登記的（doctor 的 `ConsumeChecks`、overview 的 `ConsumeBoard`、notify 的 `ConsumeNoticeSources`）在 `App.Assemble` 時交進來、收到就驗，不合格擋下啟動。指令的型別與執行環境（`Env`）在 `internal/base/app/cmdline`；指令只用 `sdk` 的 client 打 API（CLI-1），本機指令（不連 API）用 `cmdline.LocalFlags`、`ExitCodeError`；要動這台電腦的（launchd、brew）業務模組不碰系統，由 `cmd/evergenie` 傳 `cmdline.HostFunc` 進 `Commands(…)`。

### 新增一個子模組
1. 放進對的塊（MOD-5），照上面的資料夾形狀寫；要別塊的能力就在 `Deps` 定義小 interface，同塊的直接 import（MOD-2）。
2. 在塊根目錄 `New` 的 `app.NewGroup` 清單加它（位置＝啟動順序）、`Commands` 合進去；塊外的依賴加進塊的 `Deps` 或 setter，registry 跟著接。
3. 測試：`http_test.go`（`app.New(m).Mux()` 打 HTTP：沒帶 `by`、一輪來回、錯誤對照）、`cli_test.go`（`sdk.Serve` 開假的 API，`cmdline.Run(…, Commands(), usage)`）、塊的 `module_test.go` 補子模組順序；要真的 vault 用 `internal/base/vault/homefixture`；跨塊的整條路放 `cmd/evergenie`。
4. 文件同一個 commit：塊的卡加一段（DOC-11）、`architecture.md`「業務塊」表那一列補子套件。
5. 驗收：`go build ./... && go vet ./...`、`go run ./internal/tools/depcheck .`、`go run ./internal/tools/doccheck .`、`make ci`；碰到畫面的跑對應 e2e。

### 做法備忘
- 底層不 import 任何業務塊，**`_test.go` 也算**：用假物件（`session` 的 `fakes_feature_test.go`、`internal/base/agent/personafixture`），要真業務模組的整合測試放 `cmd/evergenie`。
- 每個 vault 種類只有擁有的模組直接讀寫（`architecture.md`「資料種類的擁有者」表）：要別塊的資料，在自己的 `Deps` 定義小 interface 問它（chat 讀單用 `Deps.Tasks.Get`），或聽它的事件（notify 用 `task.changed` 的 `from`）；同一塊的子套件共用那塊的資料，除非表上把種類交給一個子套件（`message` 只有 `inbox/notify`）；新增種類要在表上登記擁有者。
- 底層可以定義 interface 讓業務模組實作（session 問 goal「這一輪授權了嗎」），在 registry 接；底層不准實作 `Routes()` 或 `Commands()`（`internal/base/app` 除外）。
- 帶業務的指令輔助（`doctor.Print`、`chat.StateLabel`）留在擁有它的業務模組，別塊用 `Commands(…)` 的參數收；跟業務無關、兩個以上業務模組都要的才進 `cmdline`。
- 業務模組要改 `home.toml` 自己那一段：platform 只給通用的 `ReadHomeTable`／`SetHomeTable`，欄位規則在業務模組裡；只有一個業務模組用的資料與檔案格式放業務模組，跟 `Home` 設定有關的留 platform。
- 不要為了測試新增匯出的 Deps：測試縫用 `Module` 的不匯出欄位，在同套件測試裡換掉。
- 被好幾個模組共用的迴圈放底層（`timer.Scheduler`），用 `Handler` interface 把到期後的事交回擁有者；底層在自己的鎖外呼叫 `Handler`。
- 不為了搬模組改 sdk 的路由（網址是對外契約）；sdk 服務名不一定等於業務模組名（tasks 塊的 job 掛 `scheduler`）。
- 搬檔後相對路徑（`../../…`）每深一層補一個 `../`。

## 已知限制
- 套件（官方與使用者的）不走這個模式，它們用 SDK（見 `packages.md`）。

## Handoff
接著讀 `errors.md`、`testing.md`。

## 待作者決定
None.
