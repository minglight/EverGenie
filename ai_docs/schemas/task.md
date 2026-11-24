# Schema：task（任務單）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

主人要追蹤的事、等主人的事、等外部的事（2026-11-01 起秘書自己的多步驟工作不開單）：需求、結果、驗證；一般任務要使用者驗收才結（D-22）。執行過程留在 session，不寫進單（舊 `rules/tasks.md`）。需求：REQ-6、REQ-49–57、REQ-148–150。

## 路徑
- 字首 `SEC-` 是預設，家的 `home.toml [ids] tasks` 可以換（例：工坊 `DEV-`，GOAL-2 W3）；下面的 `SEC-` 都是「這個家的任務單字首」。
- 進行中：`vault/tasks/SEC-<n>.md`
- 已封存：`vault/tasks/archive/<YYYY-MM>/SEC-<n>.md`（月份取實際封存的時間）
- 檔名等於 `id`。

## 欄位
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `by` 例：`code`、`owner(web)` |
| `title` | string | 是 | — | 一句話需求（開單者理解的版本） |
| `type` | enum | 是 | `task` | `task`（一般任務，要使用者驗收）、`ask`（問答，秘書回答完自己結）（D-22） |
| `agent` | string | 否 | 開單的職位 id | 負責的職位 id；`status: idea`（待做）時可以空（使用者還沒指派） |
| `status` | enum | 是 | `doing` | 見下表 |
| `ask` | string | `waiting_user` 且沒有 `proposed` 時必填 | — | 要使用者做什麼或決定什麼（一到兩行）；不再塞在 `result` 裡。送驗收時 `tasks` 補「請驗收」（舊版的 `waiting_user` 一定要有 `ask`），離開等你時拿掉 |
| `waiting_since` | datetime | `waiting_user` 時必填 | — | 開始等使用者的時間；進入等你（含送驗收）時由 `tasks` 填，離開時清掉；跟進提醒照它算（REQ-148） |
| `nudges` | int | 是 | `0` | 這一輪等待已經單獨推過幾次（上限見 `settings/notify.toml` 的 `tasks.nudge_max`）；離開等待狀態時歸零 |
| `last_nudged_at` | datetime | 否 | — | 最近一次單獨推的時間 |
| `result` | string | 交付等驗收、`done` 時必填 | — | 結果一到三行；`cancelled`（不做了）把原因寫在這裡（秘書結問答類時必填） |
| `verify` | enum | `type: task` 且交付等驗收／`done` 時必填 | — | `verified`、`unverifiable` |
| `verify_how` | string | `type: task` 且交付等驗收／`done` 時必填 | — | 怎麼驗的（資料來源、指令） |
| `proposed` | enum | 交付等驗收時必填 | — | 秘書建議的結局：`done`、`cancelled`（舊值 `failed` 讀成 `cancelled`）。**`waiting_user` 且有 `proposed`＝交付等驗收**（原本的 `review`）；離開等你、或不是送驗收而進入等你時拿掉 |
| `review_note` | string | 否 | — | 使用者退回時的意見；退回後留著給秘書看，下一次送驗收時清掉 |
| `accepted_by` | string | `type: task` 且結束（`done`／`cancelled`）時必填 | — | 誰結的：主人是 `owner(…)`；有主人驗收條件、秘書自己結的是她的職位 id（TSK-14，2026-11-01）；遷移進來的舊單、擱置轉來的 `migrate` 例外，見文末 |
| `criteria` | string | 一般任務：秘書開單、送驗收、秘書自己結單時要有（service 檢查，TSK-24） | — | 驗收條件：怎樣算完（能用指令或畫面確認）。2026-11-01 之前的舊單沒有 |
| `criteria_owner` | bool | 否 | `false` | 驗收條件是主人給的（主人寫的，或秘書照抄主人的話並標明）。true、或單掛在主人確認過的 Goal，負責的秘書照條件驗過就能自己結；秘書改了負責人就變 false |
| `accepted_at` | datetime | 同上 | — | 通過的時間 |
| `closed_at` | datetime | 結束時必填 | — | `done`／`cancelled` 時填 |
| `links` | list of string | 否 | `[]` | PR、commit、artifact 連結 |
| `attachments` | list of string | 否 | `[]` | 附件：`/share/<名字>` 連結（檔案在 `<家>/runtime/share/`，要真的存在；D-61、TSK-28）；拿掉只改這裡、不刪檔 |
| `engine_session` | string | 否 | — | 這張單的工作 session 在引擎那邊的 session id（D-33）：關掉後用引擎的接續指令接回；第一次開工作 session 時由核心記，狀態檔丟了也靠它接回（REQ-217、REQ-219） |
| `goal` | link | 否 | — | 最多一個，指向 `goals/GOAL-<n>`（D-25；原本的 `group`，2026-10-25 改名）。只要求那個 Goal 存在，不必是 `active`、不需主人確認 |

`status` 的值（2026-11-01 從九種收成六種，D-42）：

| key | 畫面上叫 | 說明 |
|---|---|---|
| `idea` | 待做 | 還沒開始（含原本的「想法」：使用者的收件匣，可以還沒指派）；不跟進、不算停住 |
| `doing` | 進行中 | 超過 `tasks.stale_days`（預設 3）天沒更新算「停住的單」，進使用者早上的摘要（REQ-53） |
| `waiting_user` | 等你處理 | 要使用者決定或動手（必填 `ask`，畫面叫「要你回答」），或秘書交付等驗收（有 `proposed`，只有 `type: task`，畫面叫「要你驗收」）；照 REQ-148 跟進 |
| `waiting_external` | 等外部 | 等別人、CI 在跑、額度用完；跟 `doing` 一樣會算停住 |
| `done` | 完成 | |
| `cancelled` | 不做了 | 原本的無法完成、擱置、取消；原因寫在 `result`。一般任務只有使用者能設（秘書送驗收時可以建議），問答類秘書可以自己結 |

舊值（2026-11-01 前寫的檔）：vault 照樣收，`tasks` 讀進來就轉、家的遷移 `2026-11-01-task-statuses` 改寫檔案（TSK-23）：
`review`→`waiting_user`（沒有 `ask` 補「請驗收」）、`failed`→`cancelled`（`result` 前加「無法完成：」）、`shelved`→`cancelled`（`result` 前加「擱置：」、補 `closed_at`＝最後更新時間，一般任務補 `accepted_by: migrate`、`accepted_at`）、`proposed: failed`→`cancelled`、拿掉 `site`。

狀態轉移（`tasks` 模組執行，不合法回 `ERROR_CODE_CONFLICT`；身分不對回 `ERROR_CODE_FORBIDDEN`）：

| 誰 | 可以做的轉移 |
|---|---|
| 秘書（`by` 是職位 id） | `idea`→`doing`；`doing`⇄`waiting_user`／`waiting_external`；送驗收（`Submit`，只有 `type: task`）＝`doing`／`waiting_user`→`waiting_user`＋`proposed`；`type: ask` 可以 `doing`→`done`／`cancelled`（`cancelled` 要寫 `result`）。**不能**把 `type: task` 設成 `cancelled`；設成 `done` 只有一種：自己負責、單在 `doing`／`waiting_external`、有主人的驗收條件、照條件驗過，用 `Close`（TSK-14） |
| 使用者（`by` 是 `owner(…)`） | 任何非結束狀態之間；交付等驗收→「通過」（設成 `proposed` 的結局，填 `accepted_by`、`accepted_at`、`closed_at`）或「退回」（回 `doing`，寫 `review_note`）；停住的單→`cancelled`、`done`、`doing` |
| 任何人 | 結束的單（`done`／`cancelled`）不能再改狀態 |

內文：自由 Markdown（需求補充）。附件不放內文，記在 `attachments`（D-61；舊單內文裡的圖片連結照樣顯示）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `goal` | `GOAL-<n>` | 選填；指向的 Goal 必須存在（`draft`、`active`、`done`、`withdrawn`、已封存都算） |
| 入 | （不存） | 訊息的 `task`、待辦的 `source`、habit 的 `sources` | 由索引算，不寫回這個檔 |
| 衍生 | （不存） | 工作 session `<agent>-sec-<n>` | 名稱由規則推得（REQ-56）；生命週期跟著 `status`（D-33，REQ-218） |

## 保存規則
- `done`、`cancelled`：`closed_at` 的隔天凌晨封存（REQ-6）。（遷移之前的舊值：`failed` 同上，`shelved` 最後更新 30 天後。）
- `idea`、`doing`、`waiting_*`：不封存。不會自動改成不做了（D-22）；停住的單由使用者在摘要上處理。
- 只封存，不刪除。

## 從舊系統遷移
舊路徑 `board/tasks/SEC-<n>.md`、`board/tasks/archive/…`；格式：`+++` 包住的 TOML 檔頭＋內文（D-8：改 `---` YAML）。

| 舊欄位 | 新欄位 | 轉換 |
|---|---|---|
| （檔名） | `id` | 照抄；計數器接續最大單號 |
| `title` | `title` | 照抄 |
| `type` = `TASK`／`ASK` | `type` | 小寫 |
| `role` | `agent` | 照抄（舊的 `""` → 不寫） |
| `status` | `status` | 進行中→`doing`、等主人→`waiting_user`、等外部→`waiting_external`、完成→`done`、無法完成→`failed`、擱置→`shelved`、取消→`cancelled`、想法→`idea`（2026-10 的一次性匯入；`failed`／`shelved` 之後由家的遷移轉成 `cancelled`） |
| `opened` | `created_at` | `YYYY-MM-DD` → 當天 00:00 加家的時區 |
| `closed` | `closed_at` | 同上 |
| `updated` | `updated_at` | `YYYY-MM-DD HH:MM` 補時區 |
| `result`、`verify`、`verify_how` | 同名 | `verify`：已驗證→`verified`、無法驗證→`unverifiable` |
| `link` | `links` | 單一字串 → 一個元素的陣列 |
| `by` | `by` | 照抄 |
| 內文 | 內文 | 照抄 |

驗證：遷移後筆數（含 archive）與欄位逐筆比對（P5 的驗證腳本）。

**`group` → `goal`（D-25，2026-10-25）：**舊系統沒有群組，遷移工具也沒搬過 `group`，所以不需要資料遷移。保險起見，讀取時把殘留的 `group` 欄位當 `goal`（值是合法的 `[[GOAL-<n>]]` 才接；已有 `goal` 以 `goal` 為準；其他值如舊設計的 `[[G-<n>]]` 丟掉），下一次寫回時 `group` 就被清掉（`vault.UpgradeLegacyGoal`，）。

D-22 之後的新欄位怎麼補：P5-S2 採用的預設在舊系統搬家工具刪掉前最後一版的 tag（不公開）裡的 `rules.go`（工具已刪）。
