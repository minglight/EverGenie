# 模組：app（`internal/base/app`）
Status: DRAFT — 設計 2026-10-31（骨架重整第 1 步，計畫 `2026-10-31-evergenie-subtraction`），pending author review.

**目的：** 組裝框架：把業務模組依序啟停，並收集它們登記的 HTTP 路由、指令、健檢、內建定時工作、首頁資料與通知來源。它不認得任何一個業務模組。
**種類：** 底層模組（`internal/base/`，不是套件；組裝框架，depcheck 照路徑檢查）

## MUST
- **APP-1** — 模組介面 `Module`：`Name() string`、`Start(ctx) error`、`Stop(ctx) error`（形狀同 MOD-1）。`Start` 依 `New` 的順序，`Stop` 反序。模組清單的錯誤（nil、空名稱、同名）在 `Start` 時一個都不啟動就回錯。
- **APP-2** — 啟動失敗：任何一個模組 `Start` 回錯，已經成功啟動的反序停掉再回錯（錯誤講出是哪個模組、原因；回滾本身的錯誤一併列出）；失敗的那個不會被 `Stop`；後面的不啟動。不學參考專案的遇錯繼續。`ctx` 在啟動中被取消同樣回滾。
- **APP-3** — `Stop` 反序、每個模組有逾時（預設 10 秒，`WithStopTimeout` 可改，MOD-4）：模組的 `Stop` 在獨立 goroutine 跑，逾時就放棄等它、回「停止 X 逾時」並繼續停下一個；某個失敗不影響其他的，錯誤全部收集後一起回。傳進 `Stop` 的 `ctx` 帶逾時但不繼承呼叫者的取消（關機的 `ctx` 常常已經取消了）。沒啟動過或已停掉再 `Stop` 什麼都不做。
- **APP-4** — 登記介面是選用的：模組實作哪個就登記哪個，不實作就是沒有。`RouteProvider.Routes() []Mount`（`Mount{Service, Impl}`，`Service` 是 sdk 的服務名，`Impl` 實作該服務介面）；`CheckProvider.Checks() []Check`（`Check{Name, Run, RunAll}`，`Run` 與 `RunAll` 剛好給一個：`Run` 回一個結果，`RunAll` 回好幾條（同一項天生好幾條，例如每筆壞資料；回空就是這項沒東西報）；`Run` 回 `CheckResult{Status, Message, Fix}`，`Status` 是 OK／提醒／錯誤三種、`Message` 一行白話、`Fix` 是沒問題以外的下一步；`Name` 寫成「群組.項目」，doctor 靠點前面分群組）；`JobProvider.Jobs() []Job`（`Job{Name, Cron, Every, First, Run}`：`Cron`（五欄位、家的時區）與 `Every`（固定間隔）剛好一個，`First` 只配 `Every`，是啟動後第一次之前等多久；App 只收集，跑是 `timer.JobRunner` 的事）；`BoardProvider.Board() []BoardPart`（`BoardPart{Key, Load}`，首頁要的資料段）。
- **APP-5** — `Mux()` 把所有 `RouteProvider` 的路由用 `sdk.Mount` 掛到同一個 `http.ServeMux`（`Mounts()` 先收集檢查）。服務名重複（錯誤講出兩個模組）、服務名空的、沒有實作、sdk 不認得的服務或實作簽名不符，都回錯，一個都不掛。不掛 `/`：找不到的處理與靜態檔由呼叫的人決定。
- **APP-6** — `Checks()`、`Jobs()`、`Board()` 依模組順序收集；名稱（健檢、定時工作）或 Key（首頁資料）重複、缺名稱／`Load`、健檢的 `Run` 與 `RunAll` 不是剛好一個、定時工作的 `Cron` 與 `Every` 不是剛好一個（或 `First` 配 `Cron`、間隔是負的）都回錯。
- **APP-12** — 消費者（2026-11-01）：收集到的登記要交給某個模組處理時，那個模組實作消費者介面（`JobConsumer.ConsumeJobs` → `timer.JobRunner`、`CheckConsumer.ConsumeChecks` → doctor、`NoticeConsumer.ConsumeNoticeSources` → notify、`BoardConsumer.ConsumeBoard` → overview），`Assemble()` 把合併後的清單交給清單裡每個消費者；登記有錯或消費者回錯就回錯。`Assemble` 只做一次，registry 在 `Build` 時先呼叫（啟動前擋下錯誤），`Start` 也會呼叫（已組過就略過）。
- **APP-7** — 指令：業務模組用**套件層級函式** `func Commands() map[string]app.Command` 提供（指令行程只是 client，不建 `Module`）。`App.Commands(sets...)` 合併，名稱重複或沒有實作回錯。`Command` 的型別與執行環境在 `cmdline`（APP-8）。指令名稱是「指令」或「指令 子指令」（例 `tasks add`），比對時子指令優先。
- **APP-8** — `cmdline`（`internal/base/app/cmdline`）：`Command{Run, Usage, Group}`（`Run func(e *Env, args []string) error` 是實作；`Usage` 是 help 裡這個頂層指令的用法，同一個頂層指令只在其中一筆寫，多行時後面幾行是說明；`Group` 是 help 的分段，標題與順序固定在 `help.go`：任務、提醒與通知／秘書與對話／套件、角色與資料／排程／這台電腦與家／其他；`HostCommand`、`SubHost` 回的殼用 `.With(usage, group)` 補上）；`Env{Ctx, Client, NoSocket, By, In, Out, Err}`（`By` 是 `$EVERGENIE_AGENT`，沒有就是 `owner(cli)`）；`Run(ctx, args, client, stdio, cmds, footer) int`（結束碼：0 成功、1 一般錯誤、2 用法錯誤、或指令用 `ExitCodeError` 指定的碼；`help`／無參數與不認得的指令印 `Help(cmds)`＋`footer`）；`Help(cmds)`（2026-11-01 取代 `cmd/evergenie/run.go` 手寫的 `usageText`：開頭的通用旗標說明、依分段、段內依名稱排序；沒寫用法的頂層指令自動列「名稱 子指令|子指令」）；`Lookup`、`Merge`（同一個頂層指令兩份用法也回錯）、`Env.Message`（錯誤轉一行白話：API 的 `message` 直接顯示，連不到 serve 講白話，被中斷講「已中斷」）、`Env.Line`（印一行 JSON）、`UsageError`／`Usagef`、`ExitCodeError`、`ErrReported`（已印過原因，只回 1）、`SocketPath`、`Dial`。旗標輔助（`Spec`／`Parsed`／`Prepare`／`One`／`Each`／`Send`／`NextCursor`，把命令列旗標對到 sdk 請求型別的泛型輔助）也在這裡；`internal/cli` 已經切過來，只留一份（2026-10-31，reminders 搬進業務模組時）。
- **APP-9** — `HTTPServer`（`httpserver.go`，2026-11-01 從 `internal/api` 搬來）：核心唯一的 HTTP 入口（API-6）。`NewHTTPServer(HTTPDeps{Clock})`、`SetHome`、`SetMounts(mounts)`（registry 把 `(*App).Mounts()` 的結果交進來）、`Start`／`Stop`、`Handler()`；模組名 `api`（log 也叫 `api`）。`Start`：沒設家什麼都不做；已啟動回 `Conflict`；掛載失敗回「API 服務無法掛載」；socket 在 `<家>/runtime/sockets/api.sock`（`SocketPath`），路徑超過 103 位元組回 `Unavailable`（白話：把家放到比較短的資料夾）；殘留的 socket 檔換掉、不是 socket 的檔案絕不刪；只聽 unix socket，沒有 TCP。`Stop` 用 `Close`（長連線的串流隨核心結束）並刪 socket 檔，可重複呼叫。`Handler()` 回同一組路由（看板在行程內直接用），掛載失敗回一律 500 的 handler；找不到的路徑回 `sdk.NotFoundHandler`。
- **APP-10** — `cmdline` 的本機指令殼（`cmdline/host.go`）：`HostRequest`、`HostFunc`、`HostCommand(name, fn, prepare)`、`SubHost`、`EnvHome`。要動這台電腦的指令（service、tailscale、init、preflight…）實作在 `cmd/evergenie`（用 `host`），指令表裡只放這個殼；結束碼 0 成功、2 用法錯誤、其他＝已回報（1）；沒有實作回「這個環境不支援」。
- **APP-11** — 通知來源（2026-11-01，`notice.go`）：自己處理回答的模組實作 `NoticeSourceProvider.NoticeSources() []NoticeSource`（`NoticeSource{Name, Changed, Waiting}`：`Name` 是模組 Send 時標的通知 `source`；`Changed` 是那件事變了時模組發的 bus 事件；`Waiting(ctx)` 回 `waiting`、`known` 兩個通知編號集合）。`App.NoticeSources()` 依模組順序收集，名稱空的或重複回錯。notify 實作 `NoticeConsumer.ConsumeNoticeSources`，`Assemble` 時交進去（2026-11-01 取代 registry 的 `notify.Deps.Sources` 閉包），notify 只認登記過的 source、照 `Waiting` 收掉不用回的通知、在 `notice.answered` 帶 `relay`；session 只看 `relay`。全系統只有這一份「哪些來源自己處理回答」的清單。`Waiting` 會在 notify 拿著自己的鎖時被叫，模組的實作只讀 vault、不拿會回頭叫 notify 的鎖。
- **APP-13** — 業務塊（2026-11-02，S8-3，`group.go`）：一個業務塊有好幾個子模組時，塊根目錄的組裝 `Module` 內嵌 `*app.Group`（`NewGroup(name, mods...)`），registry 只放塊。`Group` 自己就是 `Module`：`Start` 依 `NewGroup` 的順序、失敗就反序停掉已啟動的再回「<子模組>：<原因>」（APP-2 同款，ctx 取消也回滾）、已啟動再 `Start` 回錯、清單錯誤（nil、空名稱、同名）在 `Start` 回；`Stop` 反序、每個子模組各自有逾時、不繼承呼叫者的取消但**保留期限**、錯誤全部收集（APP-3 同款）。放在 App 裡時，App 停它之前把自己的逾時（`WithStopTimeout`）交給塊當每個子模組的逾時，並給整塊「子模組數 × 逾時」：塊裡一個子模組慢或卡住，其他子模組照樣在期限內停完，App 才去停下層（不會底層停了塊還在停）。登記（`Routes`、`Checks`、`Jobs`、`Board`、`NoticeSources`）照子模組順序合併；消費者介面（APP-12）轉給實作它的子模組，錯誤前面加子模組名（`Group` 永遠實作四個消費者介面，所以清單裡有塊時 `Assemble` 一定會收集並檢查四種登記，就算塊裡沒有消費者）；`SetHome` 轉給有 `SetHome(string)` 的子模組。重名照樣在 App 收集時擋（錯誤講塊名）。塊根的存取方法名稱不能跟 `Group` 的方法撞名（例如任務塊用 `JobStore()` 不用 `Jobs()`），也不用匯出欄位蓋掉提升的方法。

## NEVER
- **APP-N1** — 不用全域變數或 `init()` 登記任何東西（MOD-N1）：誰被組進來，完全看 `cmd/evergenie/registry.go` 傳給 `New` 的清單。
- **APP-N2** — 不 import 任何業務模組；`internal/` 只准 `platform`（`httpserver.go` 用家的設定、log、錯誤型別，跟 `push`、`timer` 一樣）；不認得任何業務模組的名字。
- **APP-N3** — 不引入 DI 容器；依賴照 MOD-2 由 registry 注入。
- **APP-N4** — `New` 不做事、不開 goroutine（MOD-N4）。
- **APP-N5** — `Start` 遇錯不繼續啟動後面的模組。

## 沿用的規則（原 api、cli 說明卡）
2026-11-01 `internal/api`、`internal/cli` 刪掉，程式註解還引用的規則編號留在這裡（刪除前的卡不在公開版）。
- **API-1** — 所有對 vault 的寫入都經過 HTTP 這一個入口（再進 `vault`）；引擎、指令、套件不直接碰檔案。
- **API-3** — 錯誤一律回對應 HTTP 狀態，JSON body 是 `sdk.Error`（`code`、白話 `message`、`field`、`details`）；指令與看板原樣顯示 `message`。
- **API-6** — 只在 tailnet 的 HTTPS 與本機 unix socket 上監聽；套件走 unix socket，帶 `Hello` 發的 token（D-19：宣告能力是邊界，不是安全機制）。
- **API-N1～N3** — 路由處理只做驗證輸入、呼叫模組、編解碼 JSON；不繞過 `vault` 直接寫檔；不回傳堆疊或內部路徑給使用者，細節進 log。
- **CLI-1** — 指令是 API 的薄 client（只用 `sdk/` 的 client，跟套件一樣從外面打核心）；錯誤訊息直接顯示 API 回的白話 `message`。
- **CLI-2** — 指定另一個家就連另一個家（DEV-88）：資料指令都吃全域的 `--home <家>`（寫在哪個位置都行，`--home=<家>` 也可以；`serve`、`init`、`service`、`tailscale` 自己處理，不吃全域的），主程式把它換成 `$EVERGENIE_HOME`；`cmdline.SocketPath` 的順序：`$EVERGENIE_API_SOCKET` 是標準位置（`<家>/runtime/sockets/api.sock`）且屬於別的家、又有明講 `$EVERGENIE_HOME`，就連 `$EVERGENIE_HOME` 的 socket（秘書 session 預設有 `EVERGENIE_API_SOCKET`，以前會悄悄連回自己的家）；socket 屬於同一個家或不是標準位置，照 `$EVERGENIE_API_SOCKET`。連的是另一個家時，指令結束後在 stderr 印一行「（這次連的是另一個家：<標籤>，<路徑>；不是這個對話自己的家）」，stdout 的 JSON 不變。順序也寫在 `platform/pointer.go` 的註解。測試 `cmdline_test.go` → `TestSocketPathFollowsExplicitHome`；`cmd/evergenie/homeflag_test.go` → `TestExtractHomeFlag`。
- **CLI-3** — 子指令自己說明白（DEV-89）：`Prepare` 遇到 `--help` 或 `-h` 不解析，回 `HelpText`，`Run` 印到標準輸出、結束 0；內容照請求型別產生（用法一行＋一個旗標一行：型別、sdk 字串列舉的全部值〔`enums_gen.go`，tsgen 從 sdk 產生，`tsgen -check` 守住〕、能不能重複、`Rename` 的別名；`Spec.ID` 時編號是位置參數、不列 `--id`），不手寫。不認得的旗標列出能用的旗標；API 錯誤有 `field` 而訊息沒提到時補「（欄位：x，命令列是 --x）」；手寫旗標的 `ParseOpts` 遇到 `--help` 印自己的用法、結束 0。`Env.Cmd` 是這次的指令名稱。Evidence: `cmdline/flags.go` → `flagHelp`、`flagList`；`cmdline/cmdline.go` → `Run`、`Message`；`flags_test.go` → `TestPrepareHelpListsFlags`、`TestPrepareUnknownFlagListsValid`、`TestMessageNamesField`、`TestParseOptsHelp`。
- **CLI-4** — 環境變數一律叫 `EVERGENIE_…`（DEV-158 改名）：程式只寫新名字（服務設定、hook、開給 session 與套件的環境），讀一律經 `sdk.Getenv`（收 getenv 函式的引擎套件用 `sdk.Getenv` 傳進去），新名字沒設（設成空字串也算有設）才讀改名前的舊名，舊名只寫在 `sdk/env.go`。拿掉「屬於別的家、別的 session」的變數時新舊一起拿（`sdk.EnvPrefixes`、`sdk.CurrentEnvName`、`sdk.IsProductEnv`；腳本在 `set` 那行後面清舊名）。已經裝好的家都搬完就拿掉舊名的分支。
- **CLI-N1** — 指令只 import `sdk/` 與 `internal/base/app/…`（`cmdline` 等），不 import 別的模組；`evergenie serve` 的組裝在 `cmd/evergenie`。
- **CLI-N3** — 遷移不刪除舊家的任何檔案；切換不刪舊系統。　**CLI-N4** — 不讀秘密檔內容，只傳路徑。　**CLI-N5** — 核心命令列不放任何平台專屬的指令（事件回報、取信入口之類）；那些是引擎套件自己的入口。

## 職責
- 模組的啟動、回滾與停止順序，Stop 逾時；路由、健檢、定時工作、首頁資料、通知來源、指令的收集與重名檢查；指令的執行環境與結束碼。

## 不負責
- 各業務的規則、資料型別（會出現在 API 上的在 `sdk/`）→ 各業務模組；執行健檢並顯示 → doctor 模組；把定時工作排進計時、執行 → `timer`（底層）；首頁彙整的組合與呼叫 → overview 模組；看板的連接埠、靜態檔、`/api/` 轉送、頭像、上傳與套件頁面編譯 → 子套件 `board`（`internal/base/app/board`，自己一張卡）；選單 → menu 模組。
- 要掛哪些模組、順序 → `cmd/evergenie/registry.go`（唯一的組裝點）。

## 介面
```go
package app

type Module interface { Name() string; Start(ctx context.Context) error; Stop(ctx context.Context) error }

type RouteProvider interface{ Routes() []Mount }
type CheckProvider interface{ Checks() []Check }
type JobProvider   interface{ Jobs() []Job }
type JobConsumer   interface{ ConsumeJobs(jobs []Job) error }
type CheckConsumer interface{ ConsumeChecks(checks []Check) error }
type BoardConsumer interface{ ConsumeBoard(parts []BoardPart) error }
type BoardProvider interface{ Board() []BoardPart }
type NoticeSourceProvider interface{ NoticeSources() []NoticeSource }
type NoticeConsumer interface{ ConsumeNoticeSources(sources []NoticeSource) error }

type Mount     struct{ Service string; Impl any }
type Check     struct{ Name string; Run func(ctx context.Context) CheckResult; RunAll func(ctx context.Context) []CheckResult }
type CheckResult struct{ Status Status; Message, Fix string } // StatusOK | StatusWarn | StatusError
type Job       struct{ Name, Cron string; Run func(ctx context.Context) error }
type BoardPart struct{ Key string; Load func(ctx context.Context) (any, error) }
type NoticeSource struct{ Name, Changed string; Waiting func(ctx context.Context) (waiting, known map[string]bool, err error) }
type Command = cmdline.Command

type Group struct{ /* 子模組與已啟動的清單 */ } // APP-13；實作 Module、所有登記介面與消費者介面
func NewGroup(name string, mods ...Module) *Group
func (g *Group) Modules() []Module
func (g *Group) SetHome(dir string)

func New(mods ...Module) *App
func (a *App) WithStopTimeout(d time.Duration) *App
func (a *App) Start(ctx context.Context) error
func (a *App) Stop(ctx context.Context) error
func (a *App) Mounts() ([]Mount, error)
func (a *App) Mux() (*http.ServeMux, error)
func (a *App) Checks() ([]Check, error)
func (a *App) Jobs() ([]Job, error)
func (a *App) Assemble() error // 把登記交給消費者；只做一次
func (a *App) Board() ([]BoardPart, error)
func (a *App) NoticeSources() ([]NoticeSource, error)
func (a *App) Commands(sets ...map[string]Command) (map[string]Command, error)
```
業務模組怎麼用這些介面、資料夾長什麼樣：`conventions/module-pattern.md` 的「業務模組與塊」一節。

- Evidence: `internal/base/app/app.go` → `Start`、`stopWithTimeout`、`Mux`、`Mounts`；`provider.go` → 登記介面；`group.go` → `Group`；`cmdline/cmdline.go` → `Run`、`Lookup`、`Merge`；`httpserver.go` → `HTTPServer`；`cmdline/host.go` → `HostCommand`；`cmdline/flags.go` → `Spec`、`Prepare`、`One`、`Each`、`Send`。

## 事件
None（組裝框架不發也不收 bus 事件）。

## 儲存
None（不存任何資料）。

## 依賴
標準庫與 sdk（路由用 sdk.Mount 掛載，指令的執行環境用 sdk 的 client）；`HTTPServer` 用 `platform`；不依賴任何業務模組。

## 錯誤
都是 Go `error`，訊息是白話：「啟動 X 失敗：…」「停止 X 逾時（10s）」「模組名稱重複：X」「服務 X 重複登記：A、B」「指令 "X" 重複登記」。`cmdline` 的錯誤轉結束碼見 APP-8。

## 不變條件
- 啟停順序永遠是 `New` 的順序與它的反序；啟動失敗後，每個已成功啟動的模組恰好被 `Stop` 一次，失敗的與沒輪到的一次都沒有。
- 一個 `Stop` 卡住不會讓整個關機卡住：每個模組最多等逾時那麼久。
- 任何重名（服務、健檢、定時工作、首頁 Key、指令）都在收集時就被擋下，不會拖到執行期才互相蓋掉。
- 沒實作任何登記介面的模組不會讓任何收集函式多出東西。

## 測試計畫
測試在 `internal/base/app/*_test.go` 與 `cmdline/*_test.go`，名稱就是守的行為；代表：
- 啟停與組裝（`app_test.go`，假模組）：順序、回滾、`Stop` 失敗或逾時後繼續、`Mux` 與服務重名、登記重名／缺欄位、`TestAssembleHandsJobsToConsumer`、`TestAssembleHandsChecksAndBoard`、`Commands` 合併與重名。
- 塊（APP-13，`group_test.go`）：`TestGroupStartsInOrderStopsInReverseInsideApp`、`TestGroupStartFailureRollsBackStartedSubmodules`、`TestGroupInsideAppStopsAllSubmodulesBeforeLowerModules`、`TestGroupMergesRegistrationsAndForwardsSetHome`、`TestGroupForwardsConsumersDuringAssemble`；各塊的 `module_test.go` 組起整塊跑一輪。
- `httpserver_test.go`：只聽 unix socket、殘留 socket、掛載失敗；`cmdline`：`flags_test.go`（旗標填請求、`Spec`、`TestPrepareHelpListsFlags`）、`cmdline_test.go`（`TestHelpFromCommands`、結束碼、`SocketPath`、`Merge`）、`host_test.go`。
- 路徑規則：`internal/tools/depcheck/pathrules_test.go`。

## 已知限制
- `Mux()` 只掛 sdk 定義的服務（`sdk.Routes` 有的）；不是 sdk 服務的路由（看板靜態檔、頭像、分享檔、`/pkg-bundles/`、`/pkg-files/`、上傳）沒有登記介面，由看板外殼 `board` 自己的 handler 處理（2026-11-01 拆 `internal/web` 時決定不另開登記介面：這些都是看板外殼的事，沒有別的模組要加）。
- `Jobs()` 用 app 自己的 `Job`（設計稿寫的是 `timer.Spec`）：2026-11-01 定案，`timer.JobRunner` 吃 `app.Job`，不另定型別。
- `Stop` 逾時只是不再等：卡住的 goroutine 會留到模組自己回來為止。
