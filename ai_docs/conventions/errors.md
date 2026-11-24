# 錯誤與 log

Status: DRAFT — 設計 2026-10-23（P0-S8），2026-11-02 把 `logging.md` 併進來，pending author review.

會失敗的函式怎麼回錯誤、錯誤怎麼穿過模組與 API 到使用者眼前；寫 log 怎麼寫。寫任何會失敗的函式或 log 之前讀。log 是給除錯用的，不是資料：資料放 `vault`（D-8）。

## MUST
錯誤：
- **ERR-1** — 失敗用 `error` 回傳；跨模組邊界的錯誤用 `platform.Err`（帶 `ErrorCode`，對應 `sdk/common.go`），內部用 `fmt.Errorf("…: %w", err)` 包裝。
- **ERR-2** — 錯誤碼只有 `sdk.ErrorCode` 的六個有效值（未指定用零值）；選最接近的，不新增。`INVALID`＝輸入不合 schema，`NOT_FOUND`，`CONFLICT`＝狀態不允許或衝突，`FORBIDDEN`＝規則不允許，`UNAVAILABLE`＝暫時不在、可以重試，`INTERNAL`。
- **ERR-3** — 每個給人看的錯誤帶白話 `message`（繁體中文，說發生什麼、怎麼辦），驗證類錯誤帶 `field`（欄位名）。程式識別字、路徑只放 `details` 或 log，不放 `message`。
- **ERR-4** — HTTP 層透過 `sdk.HTTPStatus` 把 `ErrorCode` 對應到 HTTP 狀態，回應 body 是 `sdk.Error`；業務模組的 HTTP 用 `platform.SDKError(err)` 轉；CLI 與看板原樣顯示 `message`。
- **ERR-5** — 只有 `UNAVAILABLE` 可以自動重試，有上限與退避；其他碼重試沒有意義，直接回報。
- **ERR-6** — 比對錯誤用 `errors.Is`／`errors.As` 加上 `platform.CodeOf(err)`，不比對錯誤字串。
- **ERR-7** — 在 goroutine 邊界與引擎事件的入口 `recover`，轉成 `INTERNAL` 並寫 log；引擎的事件回報入口無論如何都不能讓平台那一側卡住（見 `engine` 與 `session` 的說明卡）。
- **ERR-8** — 日誌不寫主人說的話（DEV-153）：`log` 的欄位只放「哪裡、什麼狀況」（對話或職位代號、錯誤、原因、狀態、次數、長度），不放訊息內文、標題、附件名、帳密；要對得上是哪一句就記長度或封包編號。回報問題的詳細版另有欄位白名單（說明卡 system 的 RPT-3），但源頭就不該寫。同一句會一直重複的警告，呼叫的地方自己要有節流或退避（例：SES-74）；平台的收斂（PLT-29）只是最後一道。

log：
- **LOG-1** — 用標準庫 `log/slog`，輸出 JSON 一行一筆到 `runtime/logs/<日期>.jsonl`（每天一份）；`serve` 同時印到 stderr（launchd 收）。
- **LOG-2** — 每筆有 `module`；跟某個東西有關就帶 `id`（`SEC-12`、`R-53`、封包 id）、`session`、`agent`，讓人用 `grep`／`jq` 就能串起一條線。
- **LOG-3** — 層級：`ERROR`（需要人看）、`WARN`（降級但繼續）、`INFO`（重要狀態轉移）、`DEBUG`（預設關）。引擎事件與 session 狀態轉移是 `INFO`，單筆事件的內容是 `DEBUG`。
- **LOG-4** — 高頻事件（引擎事件、事件匯流排）不逐筆存成 vault 檔案，只更新 session 的最新狀態，另外寫這個每日 JSONL（D-8）。
- **LOG-5** — 清道夫依保存規則刪除舊的每日 log（預設保留 30 天），先出報告。
- **LOG-6** — 會讓人去做事的 `ERROR`，同時發 `system.alert`（`notify` 轉成通知），但同一個原因 10 分鐘內只發一次。

## NEVER
- **ERR-N1** — 不 `panic` 來表示可預期的失敗；`panic` 只給程式本身的錯誤（不可能的狀態）。
- **ERR-N2** — 不把堆疊、內部路徑、秘密放進回給使用者的 `message`（REQ-116、REQ-120）。
- **ERR-N3** — 不吞錯誤（`_ = f()`）；真的可以忽略的，註解寫為什麼。
- **ERR-N4** — 不用同一個錯誤碼表示兩種要不同處理的情況（例：「找不到單」與「找不到 Goal」可以同碼，但「沒權限」不可以用 `NOT_FOUND` 偽裝）。
- **LOG-N1** — 不寫秘密、token、秘密檔內容、環境變數整包（REQ-120）。
- **LOG-N2** — 不把訊息內文、通知內文、聊天內容寫進 log（只寫 id 與長度）；那些資料在 `vault` 與引擎的對話記錄。
- **LOG-N3** — 不用 `fmt.Println` 當 log；不在迴圈裡每圈一筆 `INFO`。
- **LOG-N4** — 引擎的事件回報入口不得把任何東西寫進會被平台當成模型內容的輸出；錯誤只進 stderr 與 log 檔。

## 範例
```go
if t.Status == StatusDone && (c.Result == "" || c.Verify == "" || c.VerifyHow == "") {
    return platform.Invalid("result", "結單要寫結果、驗證、怎麼驗的，缺一個不行") // ERROR_CODE_INVALID，field=result
}
log := slog.With("module", "session", "session", name, "agent", agent)
log.Info("state", "from", old, "to", new, "event", ev)
```

## 已知限制
- log 不集中收集：每台、每家各自一份。

## Handoff
接著讀 `testing.md`（測試怎麼檢查 log 不含秘密）。

## 待作者決定
None.
