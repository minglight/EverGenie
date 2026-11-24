# Schema：goal（主人的 Goal 與 Planbook）

Status: DRAFT — 設計 2026-10-24（P3-S0b），作者 2026-10-24 放行 P3. 共通規則見 `README.md`。

主人確認一次的目的，秘書們為它接力（D-23）。frontmatter 是 Goal 原文與狀態，內文是 Planbook。需求：REQ-152、REQ-153、REQ-202（主人在看板直接開）。

## 路徑
- 字首 `GOAL-` 是預設，家的 `home.toml [ids] goals` 可以換（例：工坊 `DG-`，GOAL-2 W3）；下面的 `GOAL-` 都是「這個家的 Goal 字首」。
- `vault/goals/GOAL-<n>.md`（計數器 `goal`）
- 已封存：`vault/goals/archive/<YYYY-MM>/GOAL-<n>.md`

## 欄位
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 是 `GOAL-<n>`；`by` 是提案的職位 id |
| `title` | string | 是 | — | 一行標題；`draft` 之後不能改 |
| `text` | string | 是 | — | 主人確認的原話；`draft` 之後不能改（REQ-152） |
| `criteria` | string | 新開的必填（service 檢查；2026-11-01 之前的舊 Goal 沒有） | — | 驗收條件：怎樣算達成。跟 `text` 一樣開了不能改；主人確認過（進行中或已達成）以後，掛在這個 Goal 的任務單算有主人的驗收條件（法律第 4 條、第 1.4 條） |
| `agent` | string | 是 | `by` | 負責的秘書（回報完成、通知主人）；主人自己開的 Goal 可以在表單指定負責秘書（2026-11-01）；沒指定是 `owner(…)`（由接力的秘書自己判斷） |
| `state` | enum | 是 | `draft` | `draft`（等主人確認）、`active`、`done`、`withdrawn`；主人在看板開的 Goal 一開始就是 `active`（D-39） |
| `notice` | link | `draft` 必填 | — | 請主人確認的通知 `M-<n>`；主人自己在看板開的 Goal（D-39）沒有 |
| `confirmed_at` | datetime | `active` 起必填 | — | 主人按「確認」的時間 |
| `confirmed_by` | string | `active` 起必填 | — | 確認的來源（`owner(…)`） |
| `ended_at` | datetime | `done`／`withdrawn` 必填 | — | 結束的時間 |
| `ended_by` | string | `done`／`withdrawn` 必填 | — | 誰結束的；`withdrawn` 只能是 `owner(…)`（含主人在確認通知上按「不要」） |
| `summary` | string | `done` 時必填 | — | 負責的秘書寫的完成說明 |
| `planbook_log` | list of object | 是 | `[]` | 每次改 Planbook：`{at, by, note}`（REQ-152） |

內文：Planbook（Markdown，自由格式）。建議寫每一棒做什麼、誰負責、完成的樣子、棒子現在在誰手上；Goal 本身不寫誰做什麼（D-23）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `notice` | `messages/M-<n>` | `draft` 時必須存在；主人自己開的沒有 |
| 出 | `agent`、`by` | 職位 id | 必須存在於名冊 |
| 入 | 封包的 `goal` | 這個 Goal | 選填，只是記錄 |
| 入 | task／reminder 的 `goal` | 這個 Goal | 選填，一筆資料最多一個、不巢狀、可以跨秘書（REQ-35）；只要 Goal 存在就能掛（`draft`、`active`、`done`、`withdrawn`、已封存都算），掛上去不需要主人確認（REQ-36）。成員是反查，Goal 檔沒有成員清單 |

## 保存規則
- `done`／`withdrawn` 滿 30 天 → 封存（REQ-37）。正本類，只封存、不刪除；封存後任務與待辦的 `goal` 連結照樣解析。
- `draft` 不自動過期：確認通知是 `approve`，留在看板等主人處理（`message` 的保存規則）。

## 從舊系統遷移
舊系統沒有 Goal，也沒有群組。None.（上線時 `vault/goals/` 是空的，計數器從 `GOAL-1` 開始。）
