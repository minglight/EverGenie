# Schema：agent（職位與人設綁定）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

一個「職位」（`code`、`life`…）是固定的位置；坐在上面的人設（人名）可以換，記憶與職責不變（REQ-39–41）。需求：REQ-39–48、72、83。

## 路徑
- `vault/agents/<職位 id>/agent.md`：職位的設定與職責（內文；官方套件帶來的職位內文留空，frontmatter 選填 `duties_from: <套件>/<樣板的職位>`，職責讀程式那邊的樣板，AGT-10）
- `vault/agents/<職位 id>/addendum.md`：使用者自己加的規則（只有使用者與該秘書可以寫；官方更新永遠不碰，REQ-46、REQ-48）
- 同一個資料夾下還有 `memory.md`、`journal/`、`jobs/`（見 `memory.md`、`journal.md`、`job.md`）

## 欄位
`agent.md` frontmatter（`id` 就是職位 id）：

| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 例：`code`；小寫英文，建立後不改（人名會換，id 不會） |
| `duty` | string | 是 | — | 一句話職責（名冊用） |
| `persona` | string | 是 | — | 現任人設 slug，指向 `personas/<slug>.md` |
| `former_names` | list of string | 否 | `[]` | 以前的人名（主人用舊名時對照用，REQ-40） |
| `display_name` | string | 否 | 人設的 `name` | 只有一個人設坐多個職位、要區分時才用 |
| `enabled` | bool | 是 | `true` | |
| `mode` | enum | 是 | `resident` | `resident`（常駐 session）、`ondemand`（第一句話來時才開，閒置 60 分鐘關；當天有排程當天視為常駐，P7-4）、`none`（只有排程／沒有 session） |
| `account` | string | 是 | `home.toml` 的預設帳號 | 對照 `[accounts.<名>]` |
| `model` | string | 否 | `sonnet` | 傳給引擎 |
| `effort` | string | 否 | — | 傳給引擎 |
| `remote_access` | bool | 否 | 帳號的設定 | 手機可以直接連（REQ-73；引擎沒有這個能力就忽略） |
| `color`、`color_dark` | string | 否 | — | 看板主題色（`#rrggbb`），亮色與深色 |
| `reminder_slot` | string | 否 | `home.toml` 的 `slots.default` | 提醒預設時間組（REQ-62） |
| `sites` | 歷史欄位 | 否 | — | 舊資料保留讀取，新資料不再填入；不限制本機秘書 |
| `budget.max_runs_per_day` | int | 否 | `4` | 排程工作（含擴充）每天最多執行次數 |
| `extras` | map | 否 | `{}` | 能力模組的設定，例 `extras.ai-topics.enabled: true`（REQ-22，上線後） |
| `shares_conversation_with` | string | 否 | — | 代管：共用另一個職位的對話（REQ-42），值是職位 id |
| `duties_from` | string | 否 | — | 官方職責樣板 `<套件>/<樣板的職位>`（AGT-10） |
| `knowledge` | list of string | 否 | 沒有欄位＝照職責裡每塊的預設 | 主人勾的可選知識 id（職責 `## 可選知識` 底下的 `{#id}`，DEV-161）；空清單＝全部不勾。開場只帶勾了的那幾塊（`secretaries.md` SCR-5）。用 `agents knowledge` 改（主人或這個職位的秘書本人） |
| `knowledge_seen` | list of string | 否 | — | 改勾選當下職責全部的可選 id（DEV-161）：不在裡面的塊（官方之後新加的）照它的預設算；沒有這個欄位但有 `knowledge`＝當成每塊都看過 |
| `first_day` | bool | 否 | — | 新家第一位秘書、還沒第一次啟用（DEV-153，agents AGS-11）：名冊一筆都沒有時建的那位才有；第一次設成啟用時核心拿掉（同時排上班招呼）。不開放 `agents set` |

內文：這個職位的職責（取代舊 `role.md`）。**官方給的職責只讀**（來源是套件，D-3）；使用者要補充寫在 `addendum.md`，組裝 session 指令時固定附在職責之後（REQ-43、REQ-48）。

`addendum.md`：frontmatter 只有 `schema`、`agent`、`updated_at`、`by`；內文自由 Markdown。核心認得兩種 `## ` 段落：`## slot: <名稱>`（排程內文的個人化，說明卡 agent AGT-17）、`## 聊到什麼先看什麼`（主人定的「聊到 X 先看 Y」，`customize add --section` 寫入；兼任這個職位的開場也帶，AGT-43）。

### 可以用 `agents set` 改的欄位（Q3）
`duty`、`display_name`、`enabled`、`mode`、`model`、`effort`、`remote_access`、`color`、`color_dark`、`reminder_slot`、`budget.max_runs_per_day`；值一律寫文字，布林寫 `true`／`false`，選填欄位給空字串就拿掉。其他欄位有自己的指令：`persona` 用 `agents assign`、`account` 用 `agents account`、`id` 用 `agents mv`、`knowledge` 用 `agents knowledge <id> --enabled a,b`（看板「職位與秘書」頁勾選）；`former_names`、`sites`、`extras`、`shares_conversation_with`、`first_day` 不開放。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `persona` | `personas/<slug>.md` | 必須存在 |
| 出 | `account` | `home.toml` 的帳號 | 必須存在 |
| 出 | `shares_conversation_with` | 另一個職位 id | 必須存在，不可循環 |
| 入 | task 的 `agent`、reminder 的 `owner`、message 的 `agent`、habit 的 `agent` | 這個職位 | 由索引算 |

## 保存規則
- 職位預設不封存、不刪除；停用只改 `enabled: false`。主人用 `agents rm` 移除時，整包（設定、職責、附加規則、記憶、日誌、筆記、排程）封存到各自的 `archive/`，不刪除，id 不能重建。
- `addendum.md` 與 `memory.md` 永遠保留。

## 從舊系統遷移
舊路徑 `features/<id>/feature.toml`（TOML）、`features/<id>/role.md`、`features/<id>/slots/append/*.md`、`overrides/slots/*.md`。

| 舊 | 新 | 轉換 |
|---|---|---|
| `feature.toml` 的 `name` | `id` | 照抄 |
| `duty`、`persona`、`former_names`、`display_name`、`enabled`、`model`、`effort`、`color`、`color_dark`、`reminder_slot` | 同名 | 照抄 |
| `remote_control` | `remote_access` | 改名（舊名是 Claude 那項能力的名稱） |
| `mode`（`resident` 等） | `mode` | 照抄；沒寫的 `resident` |
| `account`（`"acme"`）或 `config_dir`（`"~/.claude-acme"`） | `account` | 兩種都轉成 `home.toml` 的帳號名稱（`config_dir` 對照 `[accounts.*].config_dir`） |
| `sites` | `sites` | 名稱照抄 |
| `[budget] max_runs_per_day` | `budget.max_runs_per_day` | 照抄 |
| `[extras.*]` | `extras` | 原樣放進 map |
| `[[jobs]]` | `jobs/<id>.md` | 見 `job.md` |
| `role.md` | `agent.md` 內文 | 照抄（官方職責） |
| `slots/append/*.md`、`overrides/slots/*.md` 裡屬於這個職位的內容 | `addendum.md` | 依檔名排序串接，每段前加一行 `<!-- from slot <檔名> -->`；舊的 slot 整檔覆寫（`overrides/`）不遷移，列進遷移報告請使用者逐項決定 |
| `features/<id>/CLAUDE.md`（由 render 產生） | 不遷移 | 是產物，新系統自己組（REQ-43） |
| `framework/`（render 的產物） | 不遷移 | 同上 |
