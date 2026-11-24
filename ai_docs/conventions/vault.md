# 讀寫家裡的檔案（vault）

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

讀或寫家裡任何資料檔之前讀。欄位與保存規則看 `schemas/`，模組的行為看 `vault` 模組的說明卡；這份講「程式怎麼碰檔案」。

## MUST
- **VFS-1** — 讀寫資料一律經過 `vault.Store`（同一個行程）或 `VaultAPI`（套件、CLI）；不用 `os.ReadFile`／`os.WriteFile` 直接碰 `vault/`、`settings/`。
- **VFS-2** — 寫入前先 `Get` 取 `revision`，寫入時帶 `expected_revision`；`ERROR_CODE_CONFLICT` 就重讀、重算、重試，不覆蓋別人的修改。
- **VFS-3** — 序列化是決定性的：frontmatter 欄位照 schema 的順序、LF 換行、檔尾一個換行、`[[連結]]` 一律加引號；相同資料寫出位元組相同（git diff 才乾淨）。
- **VFS-4** — 讀回再寫出要保留未知欄位與內文（round-trip）：使用者或別的工具加的欄位不能被吃掉。
- **VFS-5** — 識別碼只用 `vault.NextID` 取得，建立後不改、不重用；檔名等於 `id`。
- **VFS-6** — 時間欄位寫 RFC 3339 含時區（家的 `user.timezone`）；讀舊格式（不含時區）時補家的時區，寫出時一律新格式。
- **VFS-7** — `journal` 只用 `Append`；其他需要「只加不改」的資料同樣不提供覆寫路徑。
- **VFS-8** — 使用者在 Obsidian 或編輯器手改檔案是允許的：`vault` 監看檔案變動、重建索引；格式不合就標成無效並讓 `doctor` 報告，不當機、不自動「修好」。主人放在 `vault/` 裡、不屬於核心資料夾的東西（自己的筆記、備份、`.obsidian`）不算無效，核心不掃也不碰（`vault` 說明卡 VLT-18）。
- **VFS-9** — 要刪除的只有 `runtime/` 的資料；`vault/` 的資料用 `Archive`。
- **VFS-10** — 套件自己的資料放 `vault/pkg/<套件 id>/`，schema 由套件宣告，寫入一樣經過 `VaultAPI`。

## NEVER
- **VFS-N1** — 不在 `internal/base/platform` 以外呼叫 `os.WriteFile`、`os.Rename` 寫 `vault/`；lint 擋。
- **VFS-N2** — 不把執行期資料（session 狀態、佇列、log）放進 `vault/`（會進 git，D-8）。
- **VFS-N3** — 不用路徑當連結的唯一依據（封存會搬檔）；連結以 `id` 解析。
- **VFS-N4** — 不逐筆把高頻事件存成檔案。
- **VFS-N5** — 不把秘密寫進任何 vault 檔案（REQ-120）。

## 範例
```go
r, err := store.Get(ctx, "task", "SEC-318")
if err != nil { return err }
r.Fields["status"] = "waiting_user"
_, err = store.Put(ctx, r, "code", r.Revision)   // 衝突時回 ERROR_CODE_CONFLICT，重讀重試
```

## 設計決定
- **D-8** — 法律第 2 條的做法：一筆資料一個 `.md`、欄位放 YAML 標頭、關聯用 `[[連結]]`、每種資料一個資料夾，各配一份 schema。所有寫入只經本家的核心（單一寫入者；hook 走 API），用暫存檔＋rename 原子寫入。高頻事件不逐筆存，只更新最新狀態，另外每天一份 JSONL 除錯 log。`vault/`、`runtime/` 不進 git。清道夫：每種資料在 schema 寫保存規則，每晚先出報告再動手；正本類只封存、不刪除。否決：執行期狀態放 SQLite。

## 已知限制
- 索引放記憶體、啟動時掃一次；資料量到幾萬筆再考慮落盤（`vault` 模組的說明卡）。

## Handoff
接著讀 `schemas/README.md`；寫套件的資料讀 `packages.md`。

## 待作者決定
None.
