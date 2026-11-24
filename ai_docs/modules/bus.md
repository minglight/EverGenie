# 模組：bus（`internal/base/bus`）
Status: DRAFT — 設計 2026-10-23（P0-S7）；2026-10-24 依新傳話架構改寫（D-5，P3 放行）。

**目的：** 訊息與事件：封包、佇列、ack、生命週期、訂閱（D-5）。取代舊系統 14 個 tmux 打字入口。
**種類：** 底層模組（`internal/base/`；沒有 HTTP、沒有指令，只給別人呼叫）

## MUST
- **BUS-1** — 每則封包有 id 與生命週期（排隊 → 已送進 session → 收件方開始處理 → 處理中 → 已回覆／收到但沒回／被中斷／送不了，中間可能卡在等確認），每次轉移記一筆 `history`，查得到。
- **BUS-2** — 同一個 `idempotency_key` 重送不會變成兩則；貼上的次數記在 `attempts`。
- **BUS-3** — `route: core` 的封包建立時是 `queued`，什麼時候送進 session 由 `session` 決定；看板看得到「排隊中」。
- **BUS-4** — 秘書之間直接傳的訊息記成 `route: direct` 的封包，建立時就是 `notified`（平台已經送出，核心只記錄）；直接傳話不通時用 `kind: say`、`route: core`，跨帳號也走同一套生命週期。
- **BUS-5** — 封包落在 `runtime/queue/<id>.md`（執行期、不進 git）；`serve` 重啟後還沒結束的封包全部還原。
- **BUS-6** — 生命週期的合法轉移只有 P3 計畫 S2 那張表列的，加上 `received`→`queued`（收件方額度用完、那一輪什麼都沒做，退回排隊等恢復後補送，DEV-47 S4、REQ-378）；其他轉移回 `ERROR_CODE_CONFLICT`。結束狀態：`replied`、`unanswered`、`interrupted`、`failed`。
- **BUS-7** — 並行模型：每個目標 session 有一個「信箱 goroutine」擁有那個 session 的佇列與封包狀態；`Send`、`Transition` 都是投進它的 channel 的訊息，由它依序處理、也由它把每次轉移寫進 `runtime/queue/<id>.md`；同一個 session 的處理順序等於投遞順序。跨 session 共用的只有 `idempotency_key → id` 的索引（一把互斥鎖）。
- **BUS-8** — 事件：每個訂閱者有自己的無上限佇列與一個處理 goroutine；`Publish` 只把事件放進各訂閱者的佇列就回傳，不等處理、不會因為慢的訂閱者而擋住發送者，也不丟事件；佇列長度超過 1000 寫 `WARN`。

## NEVER
- **BUS-N1** — 不碰 session：不送任何東西進 session、不判斷 session 閒不閒；那是 `session`＋引擎套件的事。
- **BUS-N2** — 不知道引擎細節；不 import 任何終端機控制的套件。
- **BUS-N3** — 不丟棄還沒結束的封包（目標 session 不存在也留著，由 `session` 決定退回）。
- **BUS-N4** — 不計時：沒有重通知、沒有逾時；所有等待由 `session` 的 `Tick` 處理。

## 職責
- 封包的建立、查詢、生命週期狀態機、佇列落地與還原、事件的發佈／訂閱（記憶體內，`topic` 結尾 `*` 比對）、串流給看板。

## 不負責
- 判斷 session 是否閒著、什麼時候送、回音 → `session`（派送器）＋引擎套件；通知使用者 → `notify`。

## 介面
API：`sdk/bus.go` 的 `BusAPI`（`Send`、`Get`、`List`、`Watch`、`Publish`、`Subscribe`）。Go：
```go
package bus

type Bus interface {
    Send(ctx context.Context, e Envelope) (Envelope, error)            // 依 idempotency_key 去重；core 建成 queued、direct 建成 notified
    Get(ctx context.Context, id string) (Envelope, error)
    List(ctx context.Context, to string, states ...Lifecycle) ([]Envelope, error)
    Watch(ctx context.Context, to, id string) (<-chan *sdk.BusWatchResponse, error)
    Reroute(ctx context.Context, id, to string) error // 只有 queued 可以改寄；保留 id、attempts、created_at，追加 history 並發布狀態事件
    Transition(ctx context.Context, id string, to Lifecycle, opt TransitionOpt) error   // opt：Reason、LastMessage
    Publish(ctx context.Context, topic string, payload map[string]any) error
    Subscribe(ctx context.Context, topics ...string) (<-chan Event, error)
    Sweep(ctx context.Context, now time.Time) (removed int, err error)
}
```

### P1 骨架對照
- Evidence: `internal/base/bus/module.go` → `Module`、`Deps`；形狀測試 `internal/base/bus/module_test.go` → `TestModuleShape`（REQ-118）。
- P3-S2 已實作信箱 goroutine、生命週期、檔案還原、事件佇列與 vault 轉發；Evidence: `internal/base/bus/mailbox.go` → `Send`、`Transition`；`internal/base/bus/events.go` → `Publish`、`Subscribe`；`internal/base/bus/storage.go` → `restore`。
- API 資料型別直接使用 SDK 的型別別名（實際對照見 module.go）；模組與 API 共用同一份資料，不另做型別轉換（D-5、MOD-N5）。

## 事件
- 轉發：啟動時訂閱 `vault.Watch`，每筆變更發成 `vault.changed`（`{kind, id, type, by}`）。
- 發出：`bus.envelope.state`（`{id, from, to, kind, route, goal, notice, state, reason}`，建立時與每次轉移）。
- 訂閱：None.（`bus` 是事件的中介，自己不訂閱。）

## 儲存
`schemas/message.md` 的 B 類（`runtime/queue/<id>.md`）。

## 依賴
`vault`、`platform`

## 錯誤
`ERROR_CODE_NOT_FOUND`（沒有這個封包）、`ERROR_CODE_CONFLICT`（不合法的狀態轉移，例如 `replied` 之後再 `queued`）、`ERROR_CODE_INVALID`（`to` 或 `kind` 缺）。

## 不變條件
- 狀態只照 S2 的轉移表走；結束狀態之後不能再轉；`history` 的時間單調。
- 同一個 `idempotency_key` 呼叫兩次 `Send` 回同一個 `id`，佇列只有一筆。
- `route: direct` 的封包建立時是 `notified`，`route: core` 是 `queued`。
- 重啟後還沒結束的封包數量與重啟前相同。
- 100 個 goroutine 同時對同一個 session `Send`，所有封包都進佇列、順序等於各自 `Send` 回傳的先後，沒有遺失或重複。
- 一個故意卡住的訂閱者不會讓 `Publish` 變慢，也不會讓其他訂閱者漏事件。
- 目標 session 不存在時封包留在 `queued`，不會消失。
- `internal/base/bus` 不 import 任何終端機控制相關套件。

## 測試計畫
- `TestLifecycleOrder`、`TestHistoryMonotonic`、`TestLifecycleTable`（REQ-11）
- `TestSendIdempotent`（REQ-12）
- `TestQueuedWhenTargetMissing`（REQ-13）
- `TestDirectRouteStartsNotified`、`TestSayAcrossAccounts`（REQ-14，兩個 session 屬不同帳號）
- `TestRestartRestoresQueue`（REQ-11）
- `TestConcurrentSendKeepsOrder`、`TestSlowSubscriberDoesNotBlock`（REQ-12；用 `-race` 跑）
- `TestNoTypedTextInBusPackage`（REQ-89，lint：`internal/base/bus` 不 import 任何終端機控制相關套件）

## 已知限制
- 沒有跨機器的封包傳輸（法律第 7 條：每台一個家、兩家不連線）。
