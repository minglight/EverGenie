# 訊息與事件（bus、RPC）

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

模組之間、跟套件傳訊息之前讀。兩層與依賴規則見 `architecture.md`；每個事件與 RPC 的清單只在各模組的說明卡（DOC-3）。

## MUST
- **MSG-1** — 呼叫 `architecture.md`「依賴規則」允許的方向（業務塊 → 底層、底層照「底層模組」表往下、經 `Deps` 小 interface 注入的）直接呼叫 interface；其他方向**發事件**（`bus.Publish`），不 import。
- **MSG-2** — 事件 topic 命名 `<模組>.<名詞>.<動詞過去式或狀態>`，全小寫，例 `task.changed`、`notice.answered`、`session.state`；訂閱可以用結尾 `*`。
- **MSG-3** — 事件是「發生了什麼」的通知，不是命令：payload 放識別碼與必要欄位，訂閱者要資料就用識別碼去查；訂閱者的處理必須**冪等**（同一事件收到兩次結果相同）。
- **MSG-4** — payload 用 `map[string]any`，鍵名穩定、只增不改；改鍵名等於改協定，要走 API 型別的相容規則（見下）。
- **MSG-5** — 要有回應的互動用 HTTP＋JSON 請求，不用事件模擬 request／response。
- **MSG-6** — 要送進 session 的內容用 `bus.Send`（封包、有生命週期、一定有回音），由 `session` 的派送器決定什麼時候經引擎送進去；模組不自己打字、也不用事件。
- **MSG-7** — 需要「至少送達一次」的寫入（例如套件回報、會重試的通知）帶 `idempotency_key`。
- **MSG-8** — API 型別的 JSON 欄位名與型別保持相容；enum 用穩定的小寫字串；新增欄位一律 optional 語意（預設值要能讓舊程式照常運作）；SDK 型別快照在 CI 擋刪除、改名或改型別（D-5）。
- **MSG-9** — RPC 的請求與回應型別命名為 `<服務名><RPC 名>Request／Response`（例 `TasksCreateRequest`）；SDK 型別相容性測試通過。
- **MSG-10** — 串流 RPC（`Watch`、`Chat`）要處理斷線重連：用 `since`／`cursor` 續傳，不要求伺服器記住每個客戶端的位置。

## NEVER
- **MSG-N1** — 不在事件 handler 裡同步呼叫「發事件者」（避免循環）；需要的話再發一個事件。`bus` 是中介、`Publish` 不擋，在事件處理裡呼叫 `bus` 的方法不算違反這條。
- **MSG-N2** — 不用事件傳大型資料或檔案（放 `vault` 或 `runtime/`，事件只帶 id）。
- **MSG-N3** — 不假設事件順序跨 topic 一致；需要順序的狀態轉移用封包的 `lifecycle` 或 `revision`。
- **MSG-N4** — 不為了方便在 SDK 的 API 型別裡塞某個引擎或套件專屬的欄位到核心服務（`sdk/engine.go` 的 `SetPermission` 用 EverGenie 的詞彙，D-7）。
- **MSG-N5** — 不在引擎事件回報的路徑上做同步的跨模組呼叫（`ReportEvent` 只改狀態，見 `session` 模組的說明卡）。

## 範例
```go
// 發：reminders 完成一筆待辦
bus.Publish(ctx, "reminder.changed", map[string]any{"id": "R-53", "status": "done", "owner": "life", "by": "owner(web)"})
// 訂：agent 寫「主人完成」日誌（冪等：同一個 id＋狀態只寫一次）
bus.Subscribe(ctx, "reminder.changed")
```

## 設計決定
- **D-5** — 程式之間（核心↔套件、核心↔看板與 CLI）一律 **HTTP＋JSON**：路由 `POST /api/<服務>/<方法>`，持續推送用 SSE。介面的正本是 `sdk/` 的 Go 型別（JSON 欄位 snake_case），看板的 TypeScript 型別由它自動產生；CI 用型別快照擋破壞性改動（刪欄位、改名、改型別；只加欄位不算）。核心有訊息佇列，每則訊息看得到生命週期（排隊 → 送進 session → 開始處理 → 已回覆／收到沒回／被中斷／送不了），寄件人一定收到結果。傳話分兩條路：秘書之間用平台內建的直接傳話，核心旁觀並記錄；核心要送進 session 的（主人的話、排程、待辦、按鈕答案、回音）由派送器貼上——主人的話秘書忙時也送，秘書之間、排程、回音等她閒著；確認框開著、等主人回答、額度用完時一律不貼（S5 起沒有「卡住」這個狀態）（確認框開著時 Enter 會被當成允許）。秘書是原生 session（法律第 6 條），怎麼送、狀態怎麼偵測都是引擎的事（D-21）。否決：Connect-RPC＋protobuf（開發與上線卡手）；由核心擁有 session（接不上 Remote Control）；用專用工具讓 session 自己收信（秘書變得很奇怪）。

## 已知限制
- 事件只在同一個行程內投遞。

## Handoff
接著讀 `vault.md`（資料怎麼讀寫）、各模組卡的「事件」段。

## 待作者決定
None.
