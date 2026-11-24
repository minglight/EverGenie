# Schema：reminder（主人待辦）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

主人自己要做的事，加上（選填）秘書怎麼跟進（舊 `rules/reminders.md`）。需求：REQ-59–71。

## 路徑
- 進行中：`vault/reminders/R-<n>.md`
- 已封存：`vault/reminders/archive/<YYYY-MM>/R-<n>.md`
- 檔名等於 `id`。

## 欄位
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | |
| `content` | string | 是 | — | 待辦內容 |
| `follow` | enum | 是 | `none` | 跟進方式，見下表 |
| `cycle` | enum | 是 | `once` | `once`、`daily`、`weekly`、`biweekly`、`monthly`；非 `once` 一定要有 `at` |
| `at` | datetime | `follow` 不是 `none` 時必填 | 依預設時間規則（REQ-62） | 下一次到期的時間 |
| `owner` | string | 是 | 記下它的職位 id | 負責的職位 id |
| `room` | string | 否 | — | 送到負責秘書的哪個對話；空＝常駐對話 |
| `site` | 舊欄位 | 否 | — | 已廢（2026-11-01，法律第 7 條：每台一個家）：sdk 拿掉，新資料不寫；舊檔帶著讀取時忽略 |
| `status` | enum | 是 | `todo` | 見下表 |
| `check` | string | `follow: check` 時必填 | — | 條件檢查要查什麼 |
| `source` | string | 否 | — | 來源，例 `routine:<job id>`（REQ-68）、`chat` |
| `cycle_at` | datetime | 否 | — | 週期待辦的基準時間：只延後這一次（晚點、還在做）時記下原本的 `at`，下一次從它算；做了、今天不做、主人改時間後清掉 |
| `asks` | int | 是 | `0` | 連續幾次沒回（上限 3 後不再推，主人一碰歸零，RMD-8） |
| `difficulty` | enum | 否 | `normal` | `easy`、`normal`、`hard`；完成給 1、2、3 點（RMD-24）；只有 `follow` 是 `confirm`、`check` 才算（RMD-26） |
| `done_points` | int | 否 | `0` | 累積的點數，只增不減；由核心在完成時加 |
| `done_at` | datetime | 否 | — | 最近一次完成的時間 |
| `due_at` | datetime | 否 | — | 過期從哪一刻算（RMD-26）：這一次第一次被延後（晚點、還在做、沒回改明天）前記下原本的 `at`；週期做了、今天不做、主人改時間或週期後清掉。API 另回算出來的 `points`（現在做完能拿幾點，不存） |
| `sent_at` | datetime | 否 | — | 最近一次送出的時間 |
| `closed_at` | datetime | 結案時必填 | — | `done`／`cancelled` |
| `goal` | link | 否 | — | 最多一個，指向 `goals/GOAL-<n>`（D-25；原本的 `group`）；只要求存在，不必是 `active` |
| `attachments` | list of string | 否 | `[]` | 附件：`/share/<名字>` 連結，規則同任務單（D-61、RMD-23） |

`follow`：

| key | 舊中文 | 意思 |
|---|---|---|
| `none` | 無（不用跟進） | 只列清單，過時間只標逾期，不主動找他 |
| `remind` | 提醒 | 時間到了告訴他 |
| `confirm` | 確認 | 問他做了沒 |
| `check` | 條件檢查 | 到時間去查某個系統；查到就結、查不到改問他 |
| `discuss` | 討論 | 時間到，秘書在聊天室主動開場 |

`status`：

| key | 舊中文 | 說明 |
|---|---|---|
| `todo` | 待辦 | 時間到就送（`follow` 不是 `none`） |
| `doing` | 進行中 | 使用者說「還在做」，明天同一時間再問 |
| `asking` | 要你回答 | 已送出，等他回；30 分鐘沒回回到 `todo`，一次性改到隔天同時間、週期改到今天之後的下一次（RMD-8） |
| `shelved` | 擱置 | 舊資料或秘書手動擱置；沒回不會自動進這個狀態（RMD-8） |
| `done` | 完成 | |
| `cancelled` | 取消 | |

內文：描述（Markdown，例如「預估 0.3 小時」、要帶的東西、討論想聊什麼；看板與 `reminders set --body` 可以改，RMD-23）。附件不放內文，記在 `attachments`（舊資料內文裡的圖片連結照樣顯示）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `goal` | `GOAL-<n>` | 選填；指向的 Goal 必須存在 |
| 出 | `source: routine:<id>` | job 的 `id` | 純字串，不是 `[[ ]]` 連結（job 可能已移除） |
| 入 | （不存） | 訊息的 `reminder` | 由索引算 |
| 衍生 | （不存） | 專屬對話 `<owner>-r-<n>` | 條件檢查、討論（REQ-64、REQ-65） |

## 保存規則
- `done`、`cancelled`：`closed_at` 的隔天封存。
- `shelved`：30 天後封存。
- `todo`、`doing`、`asking`：不封存。
- 只封存，不刪除。

## 從舊系統遷移
舊路徑 `board/reminders/R-<n>.md`（含 `archive/`）；`+++` TOML 檔頭＋內文。

| 舊欄位 | 新欄位 | 轉換 |
|---|---|---|
| `content` | `content` | 照抄 |
| `type` | `follow` | 無→`none`、提醒→`remind`、確認→`confirm`、條件檢查→`check`、討論→`discuss` |
| `cycle` | `cycle` | 單次→`once`、每天→`daily`、每週→`weekly`、每兩週→`biweekly`、每月→`monthly` |
| `at` | `at` | `YYYY-MM-DD HH:MM` 補時區；空就不寫 |
| `owner` | `owner` | 照抄 |
| `room`、`check`、`source`、`asks` | 同名 | 照抄；`asks` 空→`0`；`site` 不搬（2026-11-01，法律第 7 條：每台一個家） |
| `status` | `status` | 待辦→`todo`、進行中→`doing`、要你回答→`asking`、擱置→`shelved`、完成→`done`、取消→`cancelled`；更舊的「待提醒」→`todo` |
| `sent_at` | `sent_at` | 補時區 |
| `opened`／`closed`／`updated` | `created_at`／`closed_at`／`updated_at` | 補時區 |
| `by` | `by` | 照抄 |
| 內文 | 內文 | 照抄 |

**`group` → `goal`（D-25，2026-10-25）：**舊系統沒有群組，遷移工具也沒搬過 `group`，所以不需要資料遷移。保險起見，讀取時把殘留的 `group` 欄位當 `goal`（值是合法的 `[[GOAL-<n>]]` 才接；已有 `goal` 以 `goal` 為準；其他值如舊設計的 `[[G-<n>]]` 丟掉），下一次寫回時 `group` 就被清掉（`vault.UpgradeLegacyGoal`，）。
