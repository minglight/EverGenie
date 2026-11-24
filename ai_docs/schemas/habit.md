# Schema：habits（習慣套件的資料）

Status: 實作 2026-10-25（P6-4a），取代 2026-10-23 的舊設計（打卡月表格、`wallet.md`、`H-<n>.md` 單檔：**作廢，見 P6-4a**）。共通規則見 `README.md`；套件資料的註冊方式見 `package.md`「資料種類」。

官方套件 `habits`（`kind = "program"`）宣告六種資料，vault 種類名稱是 `habits.<種類>`，一筆一檔，放 `vault/pkg/habits/<種類>/<編號>.md`（YAML 標頭，D-27、D-28）。欄位規格的正本是套件資料夾的 `schemas/<種類>.toml`；規則見 `modules/habits.md`。

## 路徑
- 習慣：`vault/pkg/habits/habit/H-<n>.md`（內文＝約定時聊出來的補充）
- 打卡：`vault/pkg/habits/checkin/C-<n>.md`
- 狀態：`vault/pkg/habits/status/h-<n>.md`（編號是習慣編號的小寫）
- 錢包：`vault/pkg/habits/wallet/main.md`
- 待辦累積換點數：`vault/pkg/habits/exchange/X-<n>.md`（HAB-18）
- 第一次介紹：`vault/pkg/habits/welcome/main.md`（HAB-21）

## 欄位
共通欄位（`schema`、`id`、`created_at`、`updated_at`、`by`、`kind`、`archived_at`）由核心管；套件的欄位不能跟它們同名。

`habit`（`seq`，`H-`；`date_field = "start"`）：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `name` | string | 是 | 習慣名稱 |
| `agent` | string | 是 | 跟誰約的（職位 id） |
| `cadence` | enum | 是 | `daily`（每天）、`weekly`（每週） |
| `target` | int | `weekly` 時必填（程式與頁面檢查） | 每週幾次，1–7 |
| `miss_days` | int | 否（沒寫＝1） | 每天型最多可以連續漏幾天，0–6 |
| `week_skips` | int | 否（沒寫＝2） | 每天型一週最多跳過幾天，0–6；超過那週算沒達標，2026-11-09 之前的週不算（HAB-14） |
| `mode` | enum | 是 | `cumulative`（達標週數只增不減）、`streak`（連漏超過 `grace` 週這一輪歸零，賺到的點不收回） |
| `grace` | int | 否（沒寫＝1） | `streak` 的寬容週數，0–6 |
| `start` | date | 是 | 從哪天開始算 |
| `status` | enum | 是 | `active`、`ended`（可以改回） |
| `ended` | date | 否 | 結束那天（頁面把 `status` 改成 `ended` 時寫，改回 `active` 時拿掉；沒寫就用記錄最後修改的那天） |
| `identity`、`cue`、`two_min` | string | 否 | 想成為什麼樣的人、提示、兩分鐘版本 |
| `sources` | list of string | 否 | 自動來源：`fitness:重訓`、`fitness:跑步`（或划船、游泳、騎車）、`fitness:有氧`（四種有氧任一）、`fitness:*`、`reminder:R-<n>` |
| `subtasks` | list of string | 否 | 子項目：「名稱」「名稱=body:weight」「名稱=nutrition:meals>=N」 |
| `allow_miss` | int | 否（沒寫＝0） | 子項目最多可以漏幾項還算當天打卡 |

`checkin`（`seq`，`C-`；`date_field = "date"`）：`habit`（string，必填，`H-<n>`）、`date`（date，必填）、`sub`（string，子項目名稱；空＝整個習慣）、`source`（string，沒寫＝`manual`）、`key`（string，去重鍵）、`undo`（bool，`true`＝撤銷）。

`status`（`given`；**只有套件的程式寫**）：`habit`（string）、`streak_weeks`、`achieved_weeks`、`earned_points`（只增不減）、`next_need_weeks`（離下一點還差幾週）、`today_manual`（今天還有效的手動打卡筆數）（int）；`this_week`（map：`done`、`need`、`days`（list of 日期））；`weeks`（list of map：`week`（週一日期）、`count`、`result`（`hit`／`miss`／`open`／`skip`），最近 8 週）；`today_subs`（list of map：`name`、`done`、`how`（`manual`／`auto`／空））；`computed_at`（string，RFC 3339）。

`wallet`（`given`，只用 `main`；**只有套件的程式寫**）：`earned`（int，全部習慣含已刪的點數帳合計＋`task_wallet`＋起手禮）、`spent`（int，衣櫃花掉的）、`points`（int，`max(earned − spent, 0)`）、`ledger`（list of map：`habit`、`points`，每個習慣賺到過的最高點數）；待辦點數的 `task_points`、`task_exchanged`、`task_legacy`、`task_wallet`、`task_old_requests`（int，HAB-13、18、19）；起手禮的 `starter`（enum：`waiting`／`given`／`none`）與 `starter_habit`（string，送給哪個習慣，`given` 才有；HAB-20）；`outfits_ready`（bool，點數現在換不換得到衣服：衣櫃在用而且有上架、要點數的衣服；通知、頁面、給秘書的補充都照它，HAB-23）。

`exchange`（`seq`，`X-`；頁面寫）：`points`（int，只是記錄）、`note`（string）。HAB-18。

`welcome`（`given`，只用 `main`）：`status`（enum，必填：`done`＝秘書介紹過、`skipped`＝這個家不用介紹，程式寫）、`agent`（string，介紹的秘書）。這筆一出現（含封存的），套件給秘書的「第一次介紹」就不再帶（HAB-21、PKG-4 的 `until_record`）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `habit.agent` | 職位 id | 純字串；通知送給這位，名冊沒有時通知失敗只記 log |
| 出 | `habit.sources: reminder:R-<n>` | 待辦 | 純字串；指到的待辦 `status = done` 的那天算打卡 |
| 出 | `habit.sources: fitness:…`、子項目 `body:weight`、`nutrition:meals` | `fitness.workout`、`body.entry`、`nutrition.meal` | 即時推算，不寫打卡；沒裝那個套件就當沒有 |
| 出 | `checkin.habit`、`status.habit` | `habit` | 純字串 |
| 入 | `closet.unlock`（欄位見 `closet.md`） | — | 程式讀它算 `wallet.spent`（每筆花 `cost`，沒寫就是 1 點）；closet 只認自己（`by = closet`）與搬家腳本（`migrate`）寫的，別的來源寫的會被封存 |
| 入 | `closet.state`、`closet.outfit`（欄位見 `closet.md`） | — | 程式讀它們算 `wallet.outfits_ready`（HAB-23）：讀得到沒封存的 `closet.state/main`＝衣櫃在用；有沒封存、`state = active`、`cost > 0` 的 `closet.outfit`＝有衣服可換 |

## 保存規則
- 只封存，不刪除（`evergenie data rm` 是封存）；封存習慣後它的打卡、`status` 留在原地，`wallet.ledger` 的那一列保留（點數只增不減）。
- `status`、`wallet` 是可重算的快取，但點數帳（`ledger`）不是：丟掉它會讓已賺到的點數縮水，不要手動刪。
- 沒有自動封存（套件資料沒有 `ArchiveAt`）。

## 從舊系統遷移
不處理（D-27）：搬家當天由 AI 手動轉一次。舊的 `board/habits/` 對照：`H-<n>.md` 欄位 `role`→`agent`、`cadence` 每天／每週→`daily`／`weekly`、`mode` 累計／連續→`cumulative`／`streak`、`status` 進行中／結束→`active`／`ended`、`subtasks` 與 `sources` 格式不變；`checkins.jsonl` 每列→一筆 `checkin`；`points.json` 每個習慣的 `points`→`wallet.ledger`。
