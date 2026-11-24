# 架構總覽

Status: DRAFT — 設計 2026-10-23（P0-S2），2026-11-01 改寫成兩層（骨架重整第 8 步），pending author review.

EverGenie 是一個 Go 寫的常駐服務（`evergenie serve`），每台電腦跑一個。它管一組 AI 秘書（常駐的 AI session，由引擎套件啟動）、
任務、待辦、通知、排程與看板；其他的都做成套件。為什麼這樣切見各卡與規範的「設計決定」（D-n）與 EverGenie 的法律。

## 兩層

法律第 8 條（2026-10-31）：執行檔裡面照參考專案（另一個 Go 服務）的做法分兩種模組。

```
internal/
  base/<名>/      底層模組：沒有 HTTP、沒有指令，只給別人呼叫（下面「底層模組」表）
  feature/<塊>/<名>/  業務模組：7 塊（tasks、chat、agents、trust、inbox、packages、system），塊裡一件業務一個子套件，自己的 HTTP、指令、健檢、定時工作、首頁資料都在裡面登記
  tools/<名>/     開發與一次性工具（depcheck、doccheck、enginecheck、tsgen）：不是模組，不在 serve 裡
cmd/evergenie/    唯一的組裝點（registry.go）
sdk/              對外契約（路由、型別、client），套件與看板也用
```

**判斷法：有自己的 HTTP 或指令 → 業務模組；只被別人叫 → 底層模組。** 例外：只有同一個業務塊在用的函式庫放在那塊裡（`inbox/push`、`system/host`，S8-2）。 `session`、`agent` 放底層，是因為每個業務塊都要用到它們；它們的管理畫面與指令是業務模組（`chat`、`agents`）。
`internal/` 底下只有 `base`、`feature`、`tools` 三個資料夾（depcheck 擋）。資料夾形狀、登記方式、怎麼開一個新模組見 `conventions/module-pattern.md`。

## 底層模組

由下往上排。第二欄是**它可以 import 的底層**（自己的子套件不用列；測試檔也算）；depcheck 照這張表擋，表上沒有的 `internal/base/<名>` 也擋。新增底層模組先在這裡加一列。

| 模組 | 可以 import 的底層 | 做什麼 |
|---|---|---|
| `platform` | —（只用 sdk 與標準庫） | 時鐘、id、原子寫檔、家的設定讀取、log、錯誤型別。不知道任何業務 |
| `vault` | `platform` | 檔案資料庫：Obsidian 格式（YAML frontmatter＋`[[連結]]`）、schema 驗證、索引、單一寫入者、清道夫（D-8）；測試用的假家 `vault/homefixture` |
| `bus` | `vault`、`platform` | 訊息與事件：封包、佇列、ack、生命週期、訂閱（D-5） |
| `app` | `vault`、`platform` | 組裝框架：模組清單與啟停、五種登記介面與收集、HTTP 伺服器（`HTTPServer`）；子套件 `app/cmdline` 指令執行器、`app/board` 看板外殼（D-11） |
| `timer` | `app`、`vault`、`platform` | cron、時段、到期判斷、補跑；共用的計時迴圈 `Scheduler`，跑各模組 `Jobs()` 的 `JobRunner` |
| `engine` | `bus`、`platform` | **只有介面**：`Engine`（啟動、送達、狀態、用量、`SetPermission`）＋把呼叫轉給引擎套件的轉接（D-6、D-7、D-21）；子套件 `registry`、`fake`、`conformance` |
| `agent` | `bus`、`vault`、`platform` | 秘書名冊的資料：單筆紀錄的讀、寫、列、搬、封存，人設綁定、大腦／分身、組裝給 session 的指令；名冊管理的規則在業務塊 `agents`；測試角色庫 `agent/personafixture`；子套件 `agent/personaref`：角色在名冊、角色庫、packages 之間共用的名詞（代號格式、來源與狀態、套件角色型別、`CardInfo`，只有型別、常數與純函式；S8-2 從底層 `personaref` 併進來） |
| `session` | `engine`、`agent`、`bus`、`vault`、`platform` | 常駐 session 的執行期：開關、派送器、閒置／忙碌／等確認／額度用完（只照引擎事件，S5 起不猜卡住）、引擎事件、回合結算、討論串狀態、Goal 授權入口 |

## 業務塊

`internal/feature/<塊>/<名>`：照使用者看到的頁面與資料擁有者分成 7 塊（計畫 `2026-11-02-module-merge` 設計決定 1），塊裡一件業務一個子套件（法律第 8 條）。**塊之間不 import**（同一塊裡的子套件可以），要對方的能力就在自己的 `Deps` 定義小 interface，由 registry 注入；不急的事走 bus 事件。每塊根目錄一個組裝用的 `Module`（內嵌 `app.Group`，APP-13），registry 只放塊；每塊剛好一張說明卡 `modules/<塊>.md`（doccheck 擋）。這張表一塊一列，是 depcheck 的設定：表上沒有的 `internal/feature/<塊>` 也擋。新的業務先放進現有的塊；真的要開新塊先在這裡加一列。

| 塊 | 子套件（原名） | 做什麼 |
|---|---|---|
| `tasks` | `tasks/task`（原 `tasks`）、`tasks/reminder`（原 `reminders`）、`tasks/goal`（原 `goals`）、`tasks/job`（原 `jobs`） | 主人要追的事與時間：任務單（D-9，工作 session 照它開關）、主人待辦（提醒、延後、到期）、Goal 與 Planbook（D-23、D-24）、使用者排程與夜班（計時本身在底層 `timer`） |
| `chat` | —（根目錄就是模組） | 對話與傳話入口：sdk 的 `session`、`bus`、`engine_host` 服務與 `sessions`、`status`、`say` 指令（底層 `session`、`bus` 的門面） |
| `agents` | `agents/roster`（原 `agents`）、`agents/persona`（原 `personas`） | 秘書名冊管理的規則與 HTTP、指令（資料經底層 `agent`）；角色庫、說話層強度、套件角色、角色包（D-31） |
| `trust` | `trust/rules`（原 `trust`）、`trust/requests`（原 `policy`） | 信任等級與規則（實作 engine／session 定義的信任介面）；權限變更申請：秘書送、主人審、核心套用（D-26） |
| `inbox` | `inbox/notify`、`inbox/push`（原底層 `push`，只給 notify 用的函式庫）、`inbox/overview`、`inbox/menu` | 給主人的通知（分類、偏好、摘要、推播、按鈕、催促，D-14）；看板首頁「今天」的彙整；看板的選單與頁面清單（D-11，外殼在底層 `app/board`） |
| `packages` | `packages/pkg`（原根目錄的套件模組，S8-5）、`packages/data`（原 `data`） | 套件：探索、安裝、重編、行程監督、能力宣告、擴充點、fork 與更新整理（D-2、D-3）；套件與看板用的通用資料讀寫（sdk 的 `vault` 服務） |
| `system` | `system/settings`、`system/update`、`system/doctor`、`system/accounts`、`system/charter`（原 `rules`）、`system/homebackup`、`system/report`、`system/backupsweep`、`system/host`（原底層 `host`，函式庫） | 家的設定與模式、備份（含每週的備份掃描）、安裝前檢查；EverGenie 自己的升級與家的遷移；健康檢查（收各模組的 `Checks()`）；AI 帳號與用量；規矩頁（D-44）；這台電腦的事（launchd、Tailscale；`host` 只給命令列與 `system/update` 用，不在 `serve` 裡執行；`packages` 的裝 Go 不走 host，是 pkg 自己下載到程式的工具資料夾，PKG-65） |

塊下那一層的 Go 套件名照資料夾名（`task`、`reminder`、`goal`、`job`、`roster`、`persona`、`rules`、`requests`、`charter`、`pkg`），模組名（`Name()`）也一樣；塊根目錄的 `Module` 名稱是塊名（S8-4、S8-5）。

官方套件（獨立行程，透過 SDK 經 HTTP 溝通，不 import 任何 `internal/`）：
`engine-claude`、`personas`（只剩角色包下載，P8；角色庫在 agents 塊的 `agents/persona`，D-31）、`habits`、`closet`、`fitness`、`nutrition`、`body`、`health`（飲食、運動、身體共用的一位秘書，REQ-236）、`secretaries`（生活、工作、寫程式、電腦四個職位樣板，REQ-238）、`closet-mine`（上傳衣服與角色，可寫 `personas/mine/`，D-30；2026-11-03 起預設啟用）。選用的 `admin`（開發 EverGenie 的家才開：發版、開發家頁面）。
開發工坊（2026-10-31，法律第 1.1 條的例外「開發只在工坊做」，計畫 `2026-10-31-dev-workshop-split`）：EverGenie 的開發在另一個家（工坊家）用一般的看板、任務單、秘書；`admin` 官方套件（選用、預設不啟用）只帶程式與兩個看板頁面（發版、開發家），狀態由程式寫成套件資料（ADM-25）。核心不認得 admin。

## 依賴規則

第 1–7 條由 `go run ./internal/tools/depcheck .` 照路徑擋（`make ci` 會跑），不靠 review；`_test.go` 也算。

1. `internal/` 底下只有 `base`、`feature`、`tools`；`internal/base/<名>` 要在上面「底層模組」表有一列，`internal/feature/<塊>` 要在「業務塊」表有一列。
2. **底層不 import 業務塊**。底層要業務塊的行為，就定義 interface 讓業務塊實作（session 問 `tasks/goal`「這一輪授權了嗎」、engine 讀 `trust/requests` 的有效規則），由 registry 接；測試用假物件，要真的業務塊一起跑的整合測試放 `cmd/evergenie`。
3. **業務塊之間不 import**（同一塊裡的子套件、自己的子套件可以；depcheck 照 `internal/feature/<塊>` 比）。要對方的能力：在自己的 `Deps` 定義小 interface（MOD-2），registry 注入；不急的事走 bus 事件。
4. **底層之間照「底層模組」表**：只能 import 表上那一列列出的底層。要往上通知（例如 vault 的變更）用 `Watch`／bus 事件，不反向 import。
5. 底層不准實作 `Routes()` 或 `Commands()`（`app` 本身除外）：長出 HTTP 或指令，就該是業務模組。
6. 底層與業務塊都不 import `internal/tools`；`tools` 不受限（可以 import 任何底層與業務塊）。`cmd/` 是組裝點，不受限。
7. 每個 vault 種類只有擁有者直接讀寫（下面「資料種類的擁有者」）。
8. 官方套件、admin 套件只用 `sdk`（經 HTTP），不 import 任何 `internal/`。

說明卡也照同一套規則查（doccheck）：卡的「種類」行寫「底層模組」「業務塊」「工具…」或「官方…套件」；「依賴」段用反引號列的內部模組要跟實際 import 一致，底層不列業務塊、業務塊不列別的業務塊、套件只列 `sdk`。

## 組裝

`cmd/evergenie/registry.go` 是唯一的組裝點（MOD-N1：不用全域變數或 `init()` 登記）：

- **一份清單**：底層模組與 7 個業務塊放進一個 `app.New(…)`，一塊一格；這份清單就是啟停順序：依序 `Start`、反序 `Stop`、遇錯回滾（MOD-3、MOD-4）。塊裡子模組的先後由塊自己的 `app.Group` 管（APP-13）。`Start` 只開背景工作、立刻回傳。
  現在的順序：`platform` → `vault` → `bus` → `timer` → `agent` → `inbox` → `agents` → `tasks` → `trust` → `system` → `engine` → `session` → `chat` → `packages` → `api`（gateway）→ `board` → `jobs-runner`。限制：`trust` 在 `engine` 前（引擎握手要讀到已核准的 guidance）、`tasks` 在 `session` 前（session 的 Start 讀今天的排程）、`packages` 在 `engine` 與 `session` 後（它的 Start 會叫起套件行程）、gateway 在所有登記路由的模組後、看板在 gateway 後、定時工作的執行器最後。
  建構順序跟啟動順序不一樣：塊要別塊的東西就後建（`packages` → 看板的前端資源 → `inbox` → `tasks` → `trust` → `agents` → `session` → `system` → `chat`），互相需要的用 setter 接（`tasks.SetSessions`、`inbox.SetTasks`、`agents.SetTester`、`packages` 的 `SetSyncHost` 等）；不傳有型別的 nil 進介面。
- **小 interface 注入**：跨塊、跨底層的 `Deps` 欄位是**使用方自己定義的小 interface**（MOD-2），registry 傳具體的模組進去；同一塊的子套件例外，直接用、在塊的 `New` 接。底層定義、業務塊實作的 interface 也在這裡接（`engine.SetRuleSource`／`SetGuidanceSource` 接 trust 塊的 `Requests().ActiveRules`／`ActiveGuidance`、`agent.SetPersonaSource` 接 agents 塊的 `Library()`、`session` 的 `Goals` 接 tasks 塊的 `GoalStore()`）。
- **五種登記**（介面定義在 `internal/base/app`；模組實作哪個就登記哪個，不實作就是沒有）：

| 登記 | 方法 | 誰消費 |
|---|---|---|
| 路由 | `Routes() []app.Mount` | `all.Mounts()` 收集，交給 `app.HTTPServer`（`gateway.SetMounts`）掛到同一個 mux；同一服務同一方法只能有一個登記者 |
| 健檢 | `Checks() []app.Check` | system 塊的 `doctor`（`CheckConsumer.ConsumeChecks`，經 `app.Group` 轉） |
| 定時工作 | `Jobs() []app.Job` | `timer.JobRunner`（`JobConsumer.ConsumeJobs`） |
| 首頁資料 | `Board() []app.BoardPart` | inbox 塊的 `overview`（`BoardConsumer.ConsumeBoard`）；選單徽章（`inbox/menu`）向 overview 拿同一份 |
| 通知來源 | `NoticeSources() []app.NoticeSource` | inbox 塊的 `notify`（`NoticeConsumer.ConsumeNoticeSources`） |

  `all.Assemble()` 把收集到的交給各消費者；重名、缺欄位、名稱或鍵不合格在啟動前擋下。
- **指令**：每個子套件一個套件層級函式 `Commands()`（不是 Module 的方法：指令行程只是 client，不建 Module），每塊根目錄的 `Commands` 用 `cmdline.Merge` 合起來；`featureCommands()` 合併 7 塊與本機指令（名稱重複回錯），交給 `cmdline` 執行。塊之間共用的指令輔助用 `Commands(…)` 的參數注入（`trust.Commands(system.PrintDoctor)`、`agents.Commands(chat.StateLabel, pkg.PersonasPackPointer)`）；同一塊裡的子套件直接 import，不用參數（MOD-2）。

## 資料種類的擁有者

每個 vault 種類只有一個擁有的模組，只有它直接讀寫；別的模組要那份資料就在自己的 `Deps` 定義小 interface 問擁有者，或聽它發的事件（2026-11-01，骨架重整第 7 步）。「也可以讀」是暫時允許直接讀的例外，只能讀不能寫。depcheck 照下表檢查：`internal/` 底下（`tools`、`base/vault` 自己、測試檔除外）叫在名字含 `vault` 的東西上的 vault 方法（`Get`、`List`、`Watch` 算讀；`Patch`、`Append`、`Archive`、`ArchiveNow`、`Move`、`NextID`、`EnsureCounter` 算寫）與 `vault.Record{Kind: …}`（`Put`），種類是字串字面值或同套件的字串常數時，不是擁有者（讀的話也不在「也可以讀」）就報錯；表上沒有的種類也報錯。模組名：`internal/feature/<塊>/…` 是 `<塊>`（S8-2 起擁有者是塊：同一塊的子套件共用那塊的資料），`internal/base/<名>/…` 是 `<名>`；擁有者或「也可以讀」寫 `<塊>/<子套件>` 時只有那個子套件（含它底下的套件）算，同塊別的子套件直接呼叫它的方法（S8-5）。什麼時候交給子套件：塊裡只有一個子套件該讀寫、別的子套件本來就經它拿（例 `message` 只有 notify，overview、menu 經它的 `Board()` 段）；同塊幾個子套件都要讀的（例 task、reminder 讀 `goal`）就留在塊。

| 種類 | 擁有者 | 也可以讀 |
|---|---|---|
| `agent` | `agent` | `session`、`agents`、`system`、`tasks`、`trust`（秘書設定：名冊、模型、權限、名額；之後改經 `Deps.Agents`） |
| `addendum` | `agent` | |
| `memory` | `agent` | |
| `journal` | `agent` | |
| `changelog` | `agent` | |
| `note` | `agent` | |
| `profile` | `agent` | |
| `topic` | `agent` | |
| `topic_category` | `agent` | |
| `task` | `tasks` | |
| `reminder` | `tasks` | |
| `goal` | `tasks` | |
| `job` | `tasks` | |
| `message` | `inbox/notify` | |
| `policy_request` | `trust` | |
| `rules_version` | `system` | |
| `rules_proposal` | `system` | |

檢查不到的（照規則做，lint 不擋）：種類是算出來的（變數、參數；例如 `agent` 的 `childKinds`、`packages` 與 `chat` 的套件資料種類、`agents/roster` 的子資料）；經 `sdk` client 走 HTTP 的（`cmd/`、各業務塊的 `cli.go`、套件：那是公開的資料 API，例如 `tasks/job` 的夜間整理寫 `note`）；把 `Kind` 寫在賦值裡而不是組合字面值（`r.Kind = "goal"`）。套件自己宣告的資料種類（`habit` 等）由 `packages` 管，不在表上。

## 行程

| 行程 | 在哪 | 做什麼 |
|---|---|---|
| `evergenie serve` | 每家一個 | 本家唯一的 vault 寫入者、bus 佇列、排程、session、看板與推播；兩家不連線 |
| 套件行程 | 跟著 `serve`，由 `packages` 啟動與監督 | 每個「附程式」的套件一個；用 unix socket 上的 HTTP＋JSON 跟 `serve` 講話；掛了 `packages` 重啟並通知 |
| AI session | 由引擎套件啟動與管理（`engine-claude`：tmux，socket `eg`） | 遠端存取照常可連（D-5）；引擎把平台上發生的事轉成正規化事件，經 `EngineHostAPI` 回報給 `serve` |
| 引擎自己的入口程式 | 由該引擎的平台呼叫（`engine-claude`：`hook`） | 屬於引擎套件，不在核心的命令列；細節見該套件的說明卡 |
| `evergenie <指令>` | 秘書或使用者在 shell 跑 | `tasks add`、`reminders snooze`、`notify` 等；全部打本機 `serve` |

舊核心沒有常駐服務，是 launchd 每 60 秒叫一次 `agentctl tick`；新系統的 tick 在 `serve` 裡面（底層 `timer` 的 `Scheduler`），狀態不用每次從檔案重建。

**上線前**（開發與測試）：新系統只在暫存的家（fixture）或家的**複本**裡跑，不碰正式的家（plan 規則 5）；沒有新舊同時在正式環境運作的機制。tmux socket 用 `eg`。
**換掉舊系統**（憲法第 2、8 條）：不並跑。先完整備份家 → 遷移 → 停用舊系統（程式與設定保留當退路）→ 啟動新系統。

## 家的目錄

```
<家>/
  home.toml                   使用者、時區、模式（簡單／完整）
  settings/                   使用者偏好（看板設定頁改的）：notify.toml（D-14）、menu.toml（D-11）
  vault/                      本家的正本；備份由主人自行處理
    tasks/SEC-<n>.md
    reminders/R-<n>.md
    goals/GOAL-<n>.md                    主人的 Goal（任務、待辦用 `goal` 掛上來，D-25）
    agents/<職位 id>/agent.md            職位＋人設綁定
    agents/<職位 id>/memory.md           記憶核心（120 行內）
    agents/<職位 id>/索引.md             職位索引（核心產生）
    notes/<分類>/<主題>.md               全家共用的主題筆記（DEV-161）；notes/索引.md 是總索引
    agents/<職位 id>/journal/<日期>.md
    messages/<YYYY-MM>/M-<id>.md         給使用者的訊息（通知、按鈕、回覆）
    pkg/<套件 id>/…                      套件自己的資料（schema 由套件宣告，寫入一樣經過 vault）
  packages/
    official/<id>/            官方套件（唯讀，跟著發版）
    installed/<id>/           別人分享的（唯讀，跟著作者）
    mine/<id>/                自己的（更新永遠不碰；fork 也放這，記基準版本）
  runtime/                    不進 git（D-8）
    sessions/<名稱>.md        每個 session 的最新狀態
    queue/<id>.md             bus 佇列（含生命週期）
    logs/<日期>.jsonl         高頻事件的除錯 log
    jobs/last-runs.json       每個排程最近一次的結果與今天的次數（SCH-16）
    quips-used.json           梗本裡最近提過的梗（一週內不再帶進開場，DEV-164）
    sockets/                  serve 與套件的 unix socket
```

第 2 版佈局（DEV-145）把 `runtime/`、`secrets/`、`packages/installed/`（加 `--move-personas` 時還有官方角色）移到家外面的狀態資料夾，附件與圖庫放家裡的 `media/`，見 `schemas/home.md`「佈局」。

每種資料的欄位與保存規則在 `schemas/<種類>.md`（P0-S5）。

## 主要流程

### 1. 主人在看板跟秘書說話
1. 看板 → `bus.Send(chat, to=code, text)` → 一則 `route: core` 的封包，狀態 **排隊**。
2. `session` 的派送器看目標 session 的狀態（引擎回報的事件）：閒著、沒有確認框 → 叫 `engine.Deliver(text)`；忙 → 等。
3. 引擎把文字原樣送進 session（`engine-claude`：tmux 貼上，1 秒後 Enter），狀態 **已送進 session**。
4. 引擎回報 `TURN_STARTED` → **收件方開始處理**；這一輪算主人的。
5. 引擎回報 `TURN_ENDED` → **已回覆**。看板從 `engine.Transcript` 讀對話記錄顯示回覆（不讀畫面）。
6. 從手機遠端存取直接打字不經過 bus；看板的聊天記錄一樣從引擎的對話記錄來，所以兩邊看得到同一段對話。

### 2. 秘書通知主人、主人按按鈕
1. 秘書跑 `evergenie notify --category approve --title … --options "好|不要"`。
2. `inbox/notify` 依偏好（D-14）決定：即時推、併進摘要、只放看板；寫一則 `vault/messages/…/M-<id>.md`。
3. 主人按「好」→ `api.Answer(M-<id>, "好")` → 依訊息帶的動作：封成 `answer` 封包由派送器送回原 session，或直接執行（例如 `reminders done`）。訊息標成已回。

### 3. 秘書找秘書、主人的 Goal
1. work 用平台內建的直接傳話找 code（`engine-claude`：SendMessage）；引擎回報「直接傳話給 code」，`session` 記一則 `route: direct` 的封包。
2. code 那一輪開始 → **收件方開始處理**，這一輪算 work 的。訊息提到 `GOAL-<n>` 時，引擎的附加內容 hook 從 `tasks/goal` 拿原文附在旁邊（D-23）。
3. code 每個動作之前，引擎分類動作問 `session.CheckAction` → `tasks/goal` 的 `Decide`：秘書傳話照常，「一律問你」的動作要問主人（D-24；2026-11-01 起傳話不掛 Goal）。
4. code 那一輪結束：有回給 work → **已回覆**；沒回 → **收到但沒回**，派送器貼一則回音給 work。

### 4. 權限確認
1. session 卡在「要不要允許」→ 引擎呼叫 `EngineHostAPI.RequestApproval`（問題也走這條）。
2. `session` → `inbox/notify`（分類：要你同意）→ 看板／手機出現按鈕。
3. 主人按了 → `Decision` 回給還在等的引擎，session 繼續；逾時就留給引擎原本的確認畫面。

## repo 目錄

```
evergenie/
  cmd/evergenie/              main.go、registry.go
  internal/base/<名>/         底層模組
  internal/feature/<塊>/<名>/ 業務模組（7 塊，見「業務塊」表）
  internal/tools/<名>/        開發與一次性工具（depcheck、doccheck、enginecheck、tsgen）
  sdk/                        共用的 SDK（自己的 go.mod）：API 型別正本、client、Serve()
  packages/official/<id>/     官方套件的原始碼
  web/                        pnpm workspace：apps/board、packages/ui（@evergenie/ui）、packages/sdk-web
  ai_docs/                    這份文件所在
  evergenie.local.toml        指標檔（不進 git）：這份程式屬於哪個家（一個家＝一份程式＋一份資料，schemas/home.md「指標檔」）
```

admin 的官方內容（角色、技能）另外放，不公開；admin 套件（發版、開發家頁面）是官方選用套件，開發 EverGenie 的家才啟用（D-12，2026-10-31 改，計畫 `2026-10-31-dev-workshop-split`）。

## 設計決定
- **D-32** — 已廢：2026-10-31 法律第 7 條改，兩家不連線。每家只讀寫自己的資料。
- **D-36** — 已廢：2026-10-31 法律第 7 條改，兩家不連線。移除跨台 listener 與路由。
- **D-38** — 已廢：2026-10-31 法律第 7 條改，兩家不連線。EverGenie 不做跨台備份，備份由主人處理。

- **D-9** — 核心模組只放每個家都需要的骨架（法律第 3 條）；任務單、主人待辦、Goal 算核心。習慣、衣櫃、健身、飲食、身體、角色包下載、秘書樣板、引擎都是官方套件。模組清單見上面「底層模組」「業務塊」。
- **D-17** — 產品與 repo 叫 EverGenie（DEV-158 改名；指令、資料夾、服務名稱小寫 `evergenie`）：GitHub `minglight/EverGenie`、Go module `github.com/minglight/evergenie`，SDK 在 `/sdk`、有自己的 `go.mod`。不用舊系統的名字（用舊名建新 repo 會讓 GitHub 轉址失效）。核心與 SDK 照公開 repo 寫（`scripts/install.sh` 的 `DEFAULT_REPO` 是公開的 https 網址，不用存取權、不設 `GOPRIVATE`）；admin 的官方內容另外放，不公開。下載版（DEV-155）不用：發行檔附 sdk 的原始碼，核心編主人的附程式套件時用工作區 `go.work` 指過去，不連 repo（packages PKG-66）。

## 已知限制
P0-S4 spike 的結果（原始紀錄不在公開版；調整寫在 D-5，`conventions/messaging.md`）：
- 只能在 session「閒著」時送東西進去：確認畫面開著時，送進去的輸入會被當成對確認的回答（實測：門鈴的 `Enter` 誤核准了權限）。`session` 的狀態來自引擎事件，沒有 `TURN_ENDED` 就不送。
- 權限確認與問題都可以由核心決定後回送，不需要按鍵；使用者在別處先回答則以使用者為準，晚到的決定會被丟掉。
- 核心送進 session 用貼上（spike 07）；秘書之間用平台內建的直接傳話（spike 08，同帳號）。跨帳號的直接傳話 P3-S0c 才驗，不通就用 `evergenie say` 經派送器。
- 使用者在引擎自己的畫面中斷一個回合，不一定有 `TURN_ENDED`：狀態機要有逾時補救。
- 引擎介面只照 Claude 有的能力切、留了空格（D-21）；別的平台的差異沒有設計過。
