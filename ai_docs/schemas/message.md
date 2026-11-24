# Schema：message（訊息：通知與 bus 封包）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

兩種檔：**A. 通知**（給使用者的，正本，進 git）、**B. 佇列封包**（`bus` 的執行期資料，不進 git）。需求：REQ-11–14、23–34、63、138、139。

## 路徑
- **A. 通知**：`vault/messages/M-<n>.md`（不分月份）
- **B. 佇列封包**：`runtime/queue/<id>.md`（`id` 是 ULID，例 `01JBX…`；跟 A 的 `M-<n>` 分開）
- 封存：A 滿期後搬到 `vault/messages/archive/<YYYY-MM>/`；B 直接刪（見保存規則）。

## 欄位
### A. 通知（給使用者）
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 是 `M-<n>`；`by` 是發通知的職位 id |
| `source` | string | 否 | 空字串 | 空值為一般通知；`system.alert` 為警示轉成的通知，只由 notify 內部寫入；`goals`（確認主人的 Goal）、`policy`（權限申請）、`package`（認領職位，SEC-302 S9）由那三個模組在 Send 時標（API 的 Send 不帶 source，秘書標不了），其他值 Send 一律清掉；不隨 `by` 改變 |
| `category` | enum | 是 | — | `approve`（要你同意）、`done`（事情做完）、`due`（待辦到期）、`digest`（摘要）、`opening`（秘書主動開場）（D-14、REQ-23） |
| `title` | string | 是 | — | 任務標題 |
| `context` | string | 否 | — | 背景，一兩句 |
| `todo` | string | `approve` 時必填 | `無` | 請你做什麼（REQ-30）；`done` 沒有要他做的事就寫 `無` |
| `options` | list of string | 否 | `[]` | 按鈕選項；`approve` 一定要有（沒有就預設「好／不要」） |
| `url` | string | 否 | `/messages/<id>` | 點通知開哪個畫面；`approve` 的預設要直接開到這則訊息，不能是沒有按鈕的頁面（REQ-138） |
| `image` | string | 否 | — | 推播圖示與看板用的圖：站內網址（例 `/avatars/code.png`、`/avatars/persona/<角色代號>.png`）；`/share/` 開頭的看板當附圖畫，`/avatars/` 開頭而且有 `speaker` 的當說話者頭像（WEB-150） |
| `speaker` | string | 否 | — | 說話的是角色本人、不是署名的秘書時寫她的名字（看板顯示用；新角色來打招呼，PRS-13、NTF-66）。只有核心模組在 Send 時填，秘書的 API 不開放；有這欄的通知一律即時推（法律第 5 條白名單；主人關掉、安靜時段照舊不推，NTF-66）；看板寄件人寫她、不給「跟秘書聊」 |
| `link` | object | 否 | — | 訊息上的連結按鈕 `{label, url}`（NTF-66）：兩樣都要有，`url` 只收站內路徑（`/` 開頭、沒有空白與反斜線、路徑部分沒有空段或 `.`／`..` 段，NTF-66），否則 Send 回 `invalid`；兩樣都空＝沒有。不是回答選項，按了不算回答。核心模組與 API 的 `notify.send`（套件的程式，DEV-157）在 Send 時填，`evergenie notify` 指令不開放（例：新角色打招呼的「去角色頁看看」→ `/personas/<代號>`（DEV-143；舊的 `/personas?card=<代號>` 會轉到這裡）；習慣的「到展示間」→ `/p/closet/showroom`）。看板不畫指回這則自己（`/messages/<id>`）的 |
| `agent` | string | 是 | `by` | 顯示在標籤裡的職位 id；系統通知允許 `system` |
| `site` | 舊欄位 | 否 | — | 已廢（2026-11-01，法律第 7 條：每台一個家）：sdk 拿掉，新資料不寫；舊檔帶著讀取時忽略 |
| `account` | string | 否 | — | AI 帳號名稱（顯示用；引擎回報的系統提醒另外照「標題＋帳號」去重與收掉，NTF-43、NTF-55） |
| `room` | string | 否 | — | 答案要送回哪個對話（session 名稱） |
| `task` | link | 否 | — | 指向 `tasks/SEC-<n>` |
| `reminder` | link | 否 | — | 指向 `reminders/R-<n>` |
| `urgent` | bool | 否 | `false` | 安靜時段也推（REQ-32） |
| `inline` | bool | 否 | `false` | 討論串內提問（P7-5、D-33）：只進看板與 `room` 的討論串，不推播；`urgent` 也不推（REQ-220） |
| `typed` | bool | 否 | `false` | 只收打字回答（D-57、NTF-60）：notify 不補預設的「好／不要」、`options` 清空；看板只給打字回答與「去聊」；回答的內容轉回秘書（綁單的也轉，NTF-48、SES-11）。要你回答的任務問句（TSK-27）用它 |
| `claim` | string | 否 | — | 認領通知對應的職位 id（SEC-302 S9，REQ-343）：只有 `source` 是 `package` 時由 `pkg` 寫，其他通知 Send 一律清掉；看板用它開到「設定→職位與秘書」那張認領卡 |
| `lane` | enum | 否 | 照規則算 | `agent`（秘書那道）、`system`（系統那道：系統提醒、套件、發的地方明講的自動報告）；notify 在 Send 時寫（D-47、NTF-56），舊檔沒有的讀出來時補上。決定徽章算不算（NTF-57）、推不推（NTF-58）與看板哪個分頁 |
| `badge` | bool | — | 讀出來時算 | 這則現在算不算進「訊息」徽章與未讀數（NTF-57）；只在 API 回應裡，**不存檔** |
| `delivers_answer` | bool | — | 讀出來時算 | 主人回答之後要不要等秘書讀到（記 `answer.delivered_at`、超過 2 小時再問，NTF-59）；系統提醒、套件、模組自己處理的、綁單或待辦的是 `false`，看板就不說「秘書還沒讀到」。只在 API 回應裡，**不存檔** |
| `delivery` | list of object | 是 | `[]` | 實際送了哪些管道：`{channel: board\|app\|line, at, result}`；`notify` 依偏好決定（D-14） |
| `state` | enum | 是 | `unread` | `unread`、`read`、`answered`、`expired`、`failed` |
| `answer` | object | 否 | — | `{text, at, via: button\|chat\|api\|auto, delivered_at?}`；`delivered_at` 只對需送回 session 的人工回答追蹤；空代表尚未送達，封包 `received` 時補上（REQ-139）。有 `room`，或無 task／reminder、`source` 不是 `system.alert` 且有 agent 才需送回；auto 與 `source` 是 `goals`／`policy`／`package` 的不追蹤（回答由那個模組自己處理，不送回秘書的對話） |

內文：完整通知文字（`[職位｜機器｜帳號] 類型｜標題` 加背景加請你，REQ-29 的固定格式由 `notify` 產生，不手寫）。

### B. 佇列封包（`bus`）
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at` | 共通 | 是 | — | 沒有 `by`，改用 `from` |
| `from` | string | 是 | — | 發送者（職位 id、`owner(web)`、`system`） |
| `to` | string | 是 | — | 目標 session 名稱（例 `code`、`code-sec-318`） |
| `site` | 舊欄位 | 否 | — | 已廢（2026-11-01，法律第 7 條：每台一個家）：sdk 拿掉，新資料不寫；舊檔帶著讀取時忽略；封包一律由這台的派送器處理 |
| `kind` | enum | 是 | — | `chat`（使用者說話）、`say`（秘書傳話）、`delegate`（大腦交辦）、`report`（分身回報）、`answer`（按鈕答案）、`routine`（排程指令）、`echo`（回音：派送器通知寄件人結果，REQ-151） |
| `route` | enum | 是 | `core` | `core`（派送器送進 session）、`direct`（秘書之間用平台內建的直接傳話，核心只記錄，REQ-14） |
| `body` | string | 是 | — | 內容（長文放內文，這裡放摘要前 200 字） |
| `lifecycle` | enum | 是 | `core`：`queued`；`direct`：`notified` | `queued`（排隊）、`notified`（已送進 session）、`received`（收件方這一輪開始處理）、`working`（這一輪有動作）、`blocked`（卡在等確認）、`replied`（處理完：已回覆或本身就是回覆）、`unanswered`（直接傳的訊息收到但沒回）、`interrupted`（處理到一半被中斷：主人按中斷、這一輪出錯（`TURN_FAILED`，原因附平台原話）、對話結束；S5 起核心不再判「卡住」）、`failed`（送不了）；後四個是結束狀態。`received` 可以退回 `queued`（收件方額度用完、那一輪什麼都沒做，恢復後補送，REQ-378） |
| `history` | list of object | 是 | `[]` | 每次狀態變化：`{state, at}` |
| `attempts` | int | 是 | `0` | 貼上的次數（REQ-12） |
| `goal` | 舊欄位 | 否 | — | 已廢（2026-11-01 作者：傳話不掛 Goal）：sdk 拿掉，新資料不寫；舊檔帶著讀取時忽略 |
| `reason` | string | 否 | — | `failed`／`interrupted` 的原因（白話） |
| `last_message` | string | 否 | — | 收件方這一輪最後說的話（前 200 字；回音與回送用） |
| `reply_to` | link | 否 | — | 回覆哪一個封包；有值的封包結束時不再要求回覆 |
| `notice` | link | 否 | — | 對應的 A 類通知 `M-<n>` |

內文：完整訊息文字。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| A 出 | `task`、`reminder` | 任務、待辦 | 選填；存在才寫入 |
| A 出 | `agent`、`by` | 職位 id／操作身分 | agent 必須存在於名冊或為 `system`；by 依共通身分規則 |
| B 出 | `to` | session 名稱 | session 不存在時封包留在 `queued`，不丟棄 |
| B 出 | `reply_to`、`notice` | 封包、通知 | 選填 |

## 保存規則
- A. 通知：`answered`／`expired` 且滿 30 天、或 `read` 的 `digest`／`done` 滿 14 天 → 封存。`approve` 且 `unread`：不封存、不過期（使用者還沒處理）。
- B. 封包：`replied`、`unanswered`、`interrupted` 滿 7 天、`failed` 滿 30 天 → 刪除（執行期資料，REQ-7）；`queued`／`notified`／`received`／`working`／`blocked` 不動。
- 高頻事件（引擎事件）不逐筆存成封包：只更新 `session` 的最新狀態（D-8）。

## 從舊系統遷移
**A. 通知**：舊路徑 `state/messages/log.jsonl`（一行一則，欄位 `id`（`N-<n>`）、`at`、`ts`、`agent`、`type`、`title`、`context`、`todo`、`site`、`account`、`options`、`sent`、`url`、`room`、`image`、`reminder`）。

| 舊 | 新 | 轉換 |
|---|---|---|
| `id`（`N-78`） | `id`（`M-78`） | 數字接續；計數器取最大值 |
| `at`／`ts` | `created_at` | 補時區 |
| `agent` | `by`、`agent` | 照抄 |
| `type`：`approve`／`done` | `category` | 照抄；舊系統只有這兩種，其他 `due`、`digest`、`opening` 切換後才會出現 |
| `title`、`context`、`todo`、`options`、`account`、`url`、`room`、`image` | 同名 | 照抄；`site` 不搬成欄位，只留在內文標籤 `[職位｜機器｜帳號]`（2026-11-01，法律第 7 條：每台一個家）；舊 `url` 格式 `/?tab=…` 不遷移（REQ-108 拿掉）→ 改成 `/messages/<id>` |
| `reminder`（`R-53`） | `reminder` | `"[[R-53]]"` |
| `sent`（`["app"]`、`["app","line"]`） | `delivery` | 每個管道一筆，`at` 用 `created_at`，`result: ok` |
| 已讀／已回（舊系統存在 `api/messages.py` 管的另一份狀態，含 `reply`、`read`、`failed`） | `state`、`answer` | 逐則比對；對不上的標 `read`；`reply.pending` 的轉成 `answer.delivered_at` 空 |

**B. 佇列封包**：舊系統沒有佇列（直接 tmux 打字）。None.
