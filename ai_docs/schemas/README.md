# Schema 共通規則

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review.

每種存成檔案的資料一份 `schemas/<種類>.md`。這份是所有 schema 共用的規則；各份只寫自己的差異。P2 起 `vault` 用這些 schema 驗證寫入（REQ-4）。

## 檔案格式
- **資料**（會一直增加的東西）：一筆一個 `.md`，`---` 包住的 YAML frontmatter，後面是內文（Markdown）。關聯用 `"[[識別碼]]"`（D-8、REQ-1）。
- **設定**（人會手改、數量固定的東西）：`.toml`（`home.toml`、`servers/<名稱>.toml`、`settings/*.toml`、套件 manifest）。
- **高頻事件**：不逐筆存成檔案，只更新 session 的最新狀態，另外寫每天一份 `runtime/logs/<日期>.jsonl`（D-8）。

## 共通欄位（每個 `.md` 資料檔都有）
| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `schema` | int | 是 | 這種資料的 schema 版本，現在都是 `1`；改格式時加一，遷移工具認得它 |
| `id` | string | 是 | 識別碼（各種類的前綴見各份），檔名等於 `id`，建立後不改、不重用 |
| `created_at` | datetime | 是 | 建立時間 |
| `updated_at` | datetime | 是 | 最後一次寫入的時間，寫入時由 `vault` 填 |
| `by` | string | 是 | 誰寫的：職位 id（例 `code`）、`owner(web)`、`owner(cli)`、`system`、`migrate` |
| `origin` | string | 否 | 已廢（2026-11-01 起每台一個家、家與家不連線）：跨台時代記「最後一次寫入是哪台電腦來的」。現在不寫；舊資料留著這個欄位不算錯，讀取時忽略。不由各 schema 重複列出 |

例外：`memory`、`journal`、`persona`、`note`（主題筆記與分類說明）沒有 `id`（路徑就是身分），各份自己寫。

## 型別
| 型別 | 寫法 |
|---|---|
| string | YAML 字串；多行放內文，不放 frontmatter |
| int／bool | YAML 原生型別 |
| date | `2026-10-23` |
| datetime | RFC 3339 含時區：`2026-10-23T15:19:00+08:00` |
| enum | 英文小寫 key（列在各份的欄位表）；畫面上的中文標籤由 `web` 對照，不存進資料 |
| link | `"[[SEC-12]]"`（一定加引號，YAML 才不會當成巢狀陣列）；指向的檔案必須存在，`vault` 寫入時檢查 |
| list | YAML 陣列 |

## 保存規則（共通）
- 每種資料在自己的「保存規則」寫清楚：什麼條件下封存、封存到哪、保留多久。清道夫每晚跑一次，**先產生報告再動手**（D-8、REQ-5）。
- **正本類的資料只封存、不刪除**：封存是搬到同一個資料夾下的 `archive/<YYYY-MM>/`，仍然查得到，連結照樣解析（以 `id` 找，不看路徑）。例外：套件自己給編號的種類封存後可以用同編號再記一筆，這時以編號查到的是新的那筆，舊的留在 `archive/`（見說明卡 vault 的 VLT-15）。
- **執行期資料**（`runtime/`）可以刪，不進 git（REQ-7）。
- 每次寫入 `vault` 追加一行變更紀錄到 `runtime/logs/changes-<日期>.jsonl`：誰、哪一筆、什麼時候（REQ-8）。

## 識別碼前綴
| 種類 | 前綴 | 例 | 計數 |
|---|---|---|---|
| task | `SEC-` | `SEC-318` | 全家一個計數器，只增不減 |
| reminder | `R-` | `R-53` | 同上 |
| habit | `H-` | `H-1` | 同上 |
| goal | `GOAL-` | `GOAL-1` | 全家一個計數器，只增不減 |
| message | `M-` | `M-78` | 同上（舊 `N-78` → `M-78`） |

計數器存在 `vault/.counters.toml`（每種一行），由 `vault` 在寫入時原子遞增。
