# Schema：notify-prefs（通知偏好）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

每個分類怎麼送、安靜時段、沒回多久再問。由 `notify` 統一把關，套件與秘書只回報分類，不能繞過（D-14、REQ-23–27）。

## 路徑
- `settings/notify.toml`（家的根目錄下；看板「設定→通知」改的就是這個檔）

## 欄位
TOML；`schema = 1`：

| 區段.欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema` | int | 是 | `1` | |
| `preset` | enum | 是 | `standard` | `quiet`（安靜）、`standard`（標準）、`active`（積極）。簡單模式只選這個；完整模式在 preset 之上逐類覆寫 |
| `quiet_hours` | string | 否 | 取 `home.toml` 的 `user.quiet_hours` | 安靜時段，只記進看板不推手機（REQ-32） |
| `channels.app` | bool | 否 | `true` | App 推播（Web Push，REQ-28） |
| `channels.line` | bool | 否 | `false` | LINE（選用） |
| `reask.after_minutes` | int | 否 | `30` | 沒回多久再問（REQ-66） |
| `reask.max_asks` | int | 否 | `3` | 最多追幾次，之後擱置 |
| `tasks.nudge_at` | string | 否 | `09:00` | 每天檢查跟進的時間，`HH:MM`，依家的時區（REQ-148） |
| `tasks.stale_days` | int | 否 | `3` | 進行中／等外部的單幾天沒動算「停住的單」，進早上的摘要（D-22、REQ-53） |
| `tasks.nudge_every_days` | int | 否 | `3` | 要你回答／要你驗收的單，幾天沒處理就單獨再推一次（REQ-148） |
| `tasks.nudge_max` | int | 否 | `3` | 同一輪等待最多單獨推幾次，之後只留在摘要（REQ-148） |
| `lanes.system_agents` | list of string | 否 | `["system"]` | 只管**舊檔**（D-47 以前、沒寫 `lane` 的通知）：這些秘書發的 `done`／`digest` 讀出來時算系統那道（D-47 ②、NTF-56）。新的通知不看署名，要進系統那道的由發的地方帶 `lane=system`。核心只預設自己的署名 `system`；要把別位（例 `admin`）以前的例行報告也收進系統，自己在這裡寫。只在檔案裡改（看板不寫，`set_prefs` 原樣保留），改了重開 serve 生效；寫空清單＝舊檔都照秘書那道 |
| `[categories.<分類>]` | 區段 | 否 | 依 preset | 分類：`approve`、`done`、`due`、`digest`、`opening`（見 `message.md`） |
| `categories.<分類>.mode` | enum | 否 | 依 preset | `instant`（即時）、`digest`（合併成摘要）、`board`（只放看板）、`off`（關閉） |
| `categories.<分類>.digest_hours` | int | `mode: digest` 時必填 | `3` | 每 N 小時合併一次 |
| `[agents.<職位 id>.categories.<分類>]` | 區段 | 否 | — | 針對單一秘書另外設定（完整模式，REQ-25）；欄位同上 |

preset 預設值：

| 分類 | `quiet` | `standard` | `active` |
|---|---|---|---|
| `approve` 要你同意 | `instant` | `instant` | `instant` |
| `done` 事情做完 | `board` | `instant` | `instant` |
| `due` 待辦到期 | `instant` | `instant` | `instant` |
| `digest` 摘要 | `board` | `digest`（每 3 小時） | `instant` |
| `opening` 秘書主動開場 | `off` | `board` | `instant` |

## 約束（寫入時驗證）
- `tasks.*` 由看板「設定 → 通知 → 任務單跟進」經 `notify.get_prefs`／`set_prefs`（`NotifyPrefs.tasks`）讀寫：`get_prefs` 沒有 `[tasks]` 就回預設；`set_prefs` 的 `tasks` 是空的就原樣保留 `[tasks]`，有帶才寫四欄。`nudge_at` 要是 `HH:MM`、`stale_days` 與 `nudge_every_days` 1–30、`nudge_max` 0–10，錯了回 `invalid`，`field` 是 `tasks.<欄>`（REQ-148）。`tasks` 模組每次跟進檢查前重讀這四欄，改了不用重開；讀到壞檔沿用上一次的值並寫一筆 WARN。
- `approve` 的 `mode` 最低只能是 `board`：不能 `off`（D-14、REQ-26）；個別秘書的覆寫也一樣。
- 未知的分類或 `mode` 一律拒絕（REQ-4）。
- 安靜時段內，`instant` 一律當作 `board`；只有訊息帶 `urgent: true` 才例外（REQ-32）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `agents.<職位 id>` | 職位 id | 必須存在於名冊 |
| 入 | message 的 `category` | `categories.<分類>` | 決定那則訊息怎麼送 |

## 保存規則
- 不封存、不刪除；改動留在 git 歷史。
- 簡單模式切成完整模式時，把 preset 展開成明確的 `[categories.*]` 寫進檔案，之後不再依賴 preset 的預設值變化。展開由核心做（P6-6）：`notify.set_prefs` 帶 `expand: true`（`SetPrefsExpand`），看板不在前端重算；沒帶 `expand` 時，檔案裡只記「跟目前預設組不一樣」的分類（簡單模式選預設組＝一個 `[categories.*]` 都不寫，舊的逐類覆寫與單一秘書另設跟著清掉）。完整切回簡單不動檔案：逐類覆寫留著，簡單模式的通知頁提醒「你有 N 項自己調過的設定，切到完整模式才看得到；選預設組會清掉它們」。`get_prefs`／`set_prefs` 的回應多一個唯讀的 `presets`（三個預設組每個分類的送法，就是上面的預設表），不存檔。

## 從舊系統遷移
舊系統沒有偏好檔：通知只有兩種（`approve`、`done`）加 `agent.toml` 的 `user.quiet_hours` 和 `[integrations] notify = { line, board_push }`。

| 舊 | 新 | 轉換 |
|---|---|---|
| `user.quiet_hours` | `quiet_hours` | 照抄（不寫也行，預設取 `home.toml`） |
| `integrations.notify.board_push` | `channels.app` | 照抄 |
| `integrations.notify.line` | `channels.line` | 照抄 |
| （無） | `preset` | `standard` |
| 舊的 MAX_ASKS=3、ASK_TIMEOUT_MIN=30（程式常數） | `reask.max_asks`、`reask.after_minutes` | 寫成設定值，預設與舊行為相同 |


## App 推播訂閱：settings/push.toml

每個裝置一段 `[[subscription]]`；沒有檔案或沒有訂閱時，不送 app、不讀秘密檔，也不算投遞失敗。

`push.toml` 由 `notify` 寫（看板「設定 → 裝置推播」開啟、關閉推播時，`SubscribePush`／`UnsubscribePush`）：整檔重寫、原子寫入、權限 0600，不保留註解；手改可以，但下次寫入會整檔重寫。同一個 `endpoint` 只會有一筆（再訂閱就更新兩個 key）。

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `endpoint` | string | 是 | Web Push 訂閱端點 |
| `p256dh` | string | 是 | 訂閱的 base64url 公開金鑰 |
| `auth` | string | 是 | 訂閱的 base64url 驗證資料 |

### 秘密檔的 [webpush] 欄位

`home.toml` 的 `secrets.file` 指向 TOML 秘密檔；只解析 `[webpush]`，忽略其他區段。每次實際送 app 推播時讀取，不快取、不記錄值。

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `vapid_public_key` | string | 是 | webpush-go 使用的 base64url 原始公開金鑰 |
| `vapid_private_key` | string | 是 | webpush-go 使用的 base64url 原始私密金鑰，不是 PEM |
| `vapid_subject` | string | 是 | 以 `mailto:` 或 `https://` 開頭，建議使用看板網址 |

有訂閱但秘密檔未設定、不存在、TOML 錯誤、任一欄缺少／空白，或 subject 開頭不符時，該則通知的 app delivery 記 `failed`；board 照常成功，Send 不因這個錯誤回錯。只寫一筆包含原因與欄位名的 WARN，不含值或檔案內容，不發 `system.alert`（避免通知迴圈）。
