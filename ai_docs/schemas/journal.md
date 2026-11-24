# Schema：journal（日誌）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

每位秘書每天一份，只追加（REQ-44）。夜間整理把值得長期記住的升級進 `memory`。

## 路徑
- `vault/agents/<職位 id>/journal/<YYYY-MM-DD>.md`
- 已封存：`vault/agents/<職位 id>/journal/archive/<YYYY-MM>/<YYYY-MM-DD>.md`

## 欄位
frontmatter（沒有 `id`：路徑就是身分）：

| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema` | int | 是 | `1` | |
| `agent` | string | 是 | — | 職位 id |
| `date` | date | 是 | — | 等於檔名 |
| `updated_at` | datetime | 是 | — | 最後一次追加的時間 |

內文：一行一件事，格式 `- HH:MM <內容>`（例 `- 15:03 AI 議題：他在忙，略過`）。**只能追加到檔尾**；`vault` 的寫入介面只提供 `Append`，沒有覆寫。使用者在頁面上勾掉待辦時，夜間整理補寫 `- 主人完成：<內容>`（REQ-69）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | 內文裡的 `[[SEC-n]]`、`[[R-n]]` | 任務、待辦 | 選用，不強制 |

## 保存規則
- 滿 90 天的日誌封存到 `archive/<YYYY-MM>/`；不刪除。
- 當天以前的日誌不再被修改（夜間整理讀完之後也不動）。

## 從舊系統遷移
| 舊 | 新 | 轉換 |
|---|---|---|
| `features/<職位>/memory/journal/<日期>.md`（純文字，一行一條 `- HH:MM …`） | `vault/agents/<職位>/journal/<日期>.md` | 內文照抄；補 frontmatter（`agent`、`date`、`updated_at` 取檔案修改時間） |
| 行尾帶「（home）」的行（來自 `from-home` 合併） | 同上 | 照抄，保留 |
