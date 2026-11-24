# Schema：session（session 最新狀態）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

每個 AI session 一份，只放**最新狀態**，不逐筆存事件（D-8）；事件另外寫 `runtime/logs/`。需求：REQ-72–80。

## 路徑
- `runtime/sessions/<名稱>.md`（執行期，不進 git，REQ-7）
- 名稱：常駐 `<職位 id>`（例 `code`）；專屬對話 `<職位 id>-<主題>`（例 `code-sec-318`、`life-r-53`）。小寫、`-` 分隔。

## 欄位
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`updated_at` | 共通 | 是 | — | 沒有 `id`／`created_at`／`by`：檔名就是名稱 |
| `name` | string | 是 | — | 等於檔名 |
| `agent` | string | 是 | — | 職位 id |
| `kind` | enum | 是 | — | `resident`、`room`（待辦／主題對話）、`temp`（排程另開的專屬 session，REQ-16）、`work`（工作 session，D-33：綁 `task` 的是任務單的工作，`<職位 id>-sec-<n>`；沒有 `task` 的是秘書傳話等太久時開的同一位秘書的另一個 session，`<職位 id>-c<n>`，REQ-154、REQ-222）；`clone` 是舊值，併進 `work`，讀到舊檔照收，新的程式不再開 |
| `site` | 舊欄位 | 否 | — | 已廢（2026-11-01，法律第 7 條：每台一個家）：sdk 拿掉，新資料不寫；舊檔帶著讀取時忽略 |
| `engine` | string | 是 | 該帳號設定的引擎 | 引擎套件 id（由 `home.toml` 的帳號決定，核心不依它分支） |
| `account` | string | 是 | — | 帳號名稱（對照 `home.toml` 的 `[accounts.*]`） |
| `model` | string | 否 | 職位的設定 | 這段對話現在用的模型（SES-60、D-46）：先記開的時候的設定，引擎回報或主人的 `/model` 做成了就換；引擎認得的代號或別名，核心不解讀 |
| `model_label` | string | 否 | — | `model` 給人看的名字（引擎給的，例「X 9」）；`model` 換了跟著換，沒給就不寫（看板顯示 `model` 原字） |
| `effort` | string | 否 | — | 這段對話現在的思考力度（引擎的字，例 `medium`、`xhigh`）；來源同 `model`，不知道就不寫 |
| `state` | enum | 是 | `stopped` | `stopped`、`starting`、`idle`、`busy`、`waiting`（卡在確認）、`stuck`（已廢，S5-2：舊狀態檔讀到當 `idle`，不再產生；S5-4 sdk 拿掉 `SessionStateStuck`）、`limited`（帳號額度用完，等恢復；不是卡住，REQ-378） |
| `state_since` | datetime | 是 | — | 進入這個狀態的時間 |
| `pending` | object | 否 | — | `state: waiting` 時：`{kind: permission\|question, id, tool, summary, questions[], since}`；`questions` 是中立的 `Question`（`header`、`text`、`options[]`、`multi_select`、`allow_free_text`），回答時 `Decision.answers` 的 key 是 `Question.text` |
| `engine_session_id` | string | 否 | — | 引擎的 session id（`--resume` 用） |
| `cwd` | string | 否 | — | 開在哪個資料夾；接回時（`evergenie sessions open --resume`）沒指定就沿用，引擎才找得到之前的對話（Q2） |
| `attach` | string | 否 | — | 從終端機接上畫面的一行指令（引擎的 `EngineStartResponse.attach`，核心只存不解讀；`evergenie sessions attach` 印出它）（Q2） |
| `remote_access` | object | 否 | — | `{enabled, name, url}`：讓手機直接連進這個 session（引擎有 `remote_access` 能力才有）；名稱是 `<人名>-<機器>`（REQ-73） |
| `last_event` | string | 否 | — | 最近一次引擎事件的類型（`EngineEventType`） |
| `last_event_at` | datetime | 否 | — | |
| `last_turn_end_at` | datetime | 否 | — | 最近一次 `TURN_ENDED`（判斷「閒著」） |
| `last_user_at` | datetime | 否 | — | 使用者最近一次說話（判斷閒置收掉） |
| `now_seen_at` | datetime | 否 | — | 最近一次附上現況快照的時間（REQ-47） |
| `activity` | object | 否 | — | `state: busy` 時正在做什麼：`{tool, since}`（來自 `PreToolUse`；沒有工具就是思考中，`tool` 空）；離開 `busy` 就清空 |
| `background_tasks` | int | 否 | `0` | 最近一次 `TURN_ENDED` 回報還在背景跑的工作數 |
| `seen_uuid` | string | 否 | — | 使用者的已讀位置：看到哪一則訊息（對話記錄的 `uuid`）；存在伺服器，手機與桌機共用（REQ-143） |
| `seen_at` | datetime | 否 | — | 最近一次標已讀的時間 |
| `title` | string | 否 | 依名稱 | 討論串標題（任務單對話取單的標題、待辦對話取待辦內容、常駐取人名；使用者可以改） |
| `turn` | object | 否 | — | 這一輪是誰觸發的：`{origin: owner\|agent, from, envelope, goals[], started_at, adopted}`（REQ-153）；下一輪開始才換。`adopted`＝這一輪是替「已經在她的對話裡」的那句開的、還沒看到開始事件（SES-76），沒有就不寫 |
| `last_owner_at` | datetime | 否 | — | 主人最近一次直接在這個 session 說話（派送器的「主人正在聊就先等」，REQ-154） |
| `owner_talk_at` | datetime | 否 | — | 主人最近一次在這個 session 說話（直接打字、從看板傳、在看板回答選擇題；核心送的第一句、排程、秘書傳話、平台自己接著做不算）；單離開進行中後，這之後閒 `idle_close_after` 才收（SES-28，DEV-147） |
| `task` | string | 否 | — | 工作 session 對應的任務單號（`SEC-<n>`）；有值的工作 session 不看閒置，生命週期跟著那張單（REQ-217、REQ-218） |
| `queued_at` | datetime | 否 | — | 有值＝這個工作 session 在排隊等名額（每台 `doing` 的工作 session 超過 `settings/sessions.toml` 的 `work_max`，REQ-220、REQ-221）；開起來或單離開 `doing` 就清掉。`state` 是 `stopped` |
| `limited_until` | datetime | 否 | — | `state` 是 `limited` 時，引擎說的額度恢復時間（零值＝不知道）；離開 `limited` 就清掉（REQ-378） |
| `last_command` | object | 否 | — | 主人最近一次下的對話指令（SES-59）：`{command, arg, at, by}`；`command` 是 `clear`／`compact`／`model`／`effort`，引擎做成了才寫 |
| `work_ask` | string | 否 | — | 核心用：工作 session 超過上限時在討論串問主人的那則通知編號（沒回之前有值；重開機後回答照樣對得上） |
| `nudged_for` | string | 否 | — | 核心用：已經提醒過的那一輪等待（任務單的 `waiting_since`），同一輪只提醒一次（REQ-218） |
| `task_status` | string | 否 | — | 核心用：最近一次讀到的任務單狀態（算上限用） |
| `suspended` | bool | 否 | `false` | 核心用：主人手動關掉的工作 session，單回到 `doing` 之前不自動接回（REQ-218） |
| `idle_close_after` | duration | 否 | `30m` | `temp`／`room` 閒置多久收掉（REQ-16、REQ-74） |

`state` 的轉移（由引擎回報的正規化事件決定，核心不知道事件在平台上叫什麼）：`TURN_STARTED`→`busy`；要使用者確認的事（`RequestApproval`）或 `ATTENTION_NEEDED`→`waiting`；`TURN_ENDED`、`TURN_FAILED`→`idle`；`IDLE_HINT`→`idle`（`busy` 時；等確認、`limited` 不蓋）；`RATE_LIMITED`→`limited`（帶恢復時間；`IDLE_HINT` 不會把它蓋成 `idle`，過了恢復時間沒有要送的話才回 `idle`，REQ-378、REQ-379）；沒有事件就不變（S5-2 起不再猜 `stuck`）。**只有 `idle` 才能通知 session**（`Deliver`）：平台的確認畫面開著時，送進去的輸入可能被當成對確認的回答（P0 spike 實測過）。

內文：None.（只用 frontmatter。）

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `agent` | 職位 id | 必須存在 |
| 出 | `turn.envelope` | 佇列封包 `runtime/queue/<id>` | 選填 |
| 入 | 封包的 `to` | 這個 session 的名稱 | 由索引算 |

## 保存規則
- `resident`：永遠保留（`stopped` 也留，`serve` 重啟時據此還原）。
- `room`、`temp`、不綁單的 `work`：`stopped` 滿 7 天刪除（執行期資料）；綁單的 `work` 留著（單上的 `engine_session` 另外記一份，狀態檔丟了也接得回）；之後可以用 `engine_session_id` 重新接回的窗口在刪除時一起消失。
- 不封存、不進 git。

## 從舊系統遷移
舊路徑 `state/<session>.sid`（session id）、`.last_stop`、`.last_user`、`.now_seen`、`.runtime.json`（`engine`、`provider`、`config_dir`、`status`）、`.model-picker.lock`。這些是**執行期資料，不遷移**：切換時舊 session 全關，新系統開新的 `runtime/sessions/<名稱>.md`。

| 舊 | 新 | 轉換 |
|---|---|---|
| `state/<名>.sid` | `engine_session_id` | 只有要「接回舊對話」時才讀；預設不接，新 session 從乾淨的 context 開始（夜間整理後重開本來就是這個行為，REQ-80） |
| `state/<名>.last_user`、`.last_stop`、`.now_seen` | `last_user_at`、`last_turn_end_at`、`now_seen_at` | 不遷移，初始為空 |
| session 名稱（`<職位>-<主題>`） | `name` | 規則不變 |
