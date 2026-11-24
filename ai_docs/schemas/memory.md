# Schema：memory（記憶核心；使用者 profile 已退休）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

每位秘書自己的記憶核心：最常用、跟自己職位有關的事，上限 120 行（DEV-161，2026-11-15 定 60、同日作者改 120）。跟人、東西、地方、專案有關的長期記憶放全家共用的主題筆記（`note.md`）。需求：REQ-44、REQ-45。

> 2026-11-15 DEV-161：共用的使用者 profile（`vault/profile.md`）退休，內文搬進固定主題「主人」（`vault/notes/人/主人.md`，`note.md`「從舊資料轉」）；舊檔留著，核心不再讀、開場不帶。底下關於 `profile.md` 的欄位只描述舊檔。

## 路徑
- 記憶核心：`vault/agents/<職位 id>/memory.md`
- 使用者 profile（已退休，留著不讀）：`vault/profile.md`
- 職位索引（核心產生，不是紀錄）：`vault/agents/<職位 id>/索引.md`（`note.md`「索引」）
- 秘書自己的做事筆記（選用，自由格式；種類 `note`，跟全家共用的主題筆記不同）：`vault/agents/<職位 id>/notes/<名稱>.md`（名稱就是檔名，中文可、不含 `/`；子資料夾裡的 `.md` 照舊認、id 是 `<職位>/<子資料夾>/<名稱>`；`.py` 這類非 `.md`、備份 `.bak-…`、隱藏檔不掃不碰，VLT-18）
- 記憶的變更紀錄（歷史，不動、不摘要）：`vault/agents/<職位 id>/changelog/<YYYY-MM-DD>.md`

## 欄位
`memory.md`、`profile.md`（沒有 `id`、`created_at`：路徑就是身分）：

| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema` | int | 是 | `1` | |
| `agent` | string | `memory.md` 必填 | — | 職位 id；`profile.md` 不寫 |
| `updated_at` | datetime | 是 | — | |
| `by` | string | 是 | — | 誰最後寫的 |
| `max_lines` | int | 否 | `120`（DEV-161 前是 `200`；`profile.md` 為 `150`） | 超過就把跟人、東西有關的搬進主題筆記、其他摘要（夜間整理）；`doctor` 的 `config.notes` 報黃字。沒寫或還是舊的 `200` 一律當 `120` 看 |

內文：Markdown。一行一件事；有時效的事附日期；互相矛盾時只留最新那條，改寫成現況並註明推翻了什麼。

`notes/<名稱>.md`（做事筆記，DEV-161）：frontmatter `schema`、`updated_at`、`by` 必填，另有選填的 `agent`（職位 id）、`created_at`（第一次寫的時間，之後不變）；內文自由。檔頭一律由核心補，寫的人只給內文。

做事筆記只放「只有這個職位自己用的長文件」（計畫、清單、草稿）：做事規則放記憶核心，人、東西、地方、專案、喜好放全家共用的主題筆記（`note.md`）；程式腳本（`.py` 這類）不放 `vault/`。

指令（只有本人寫，身分是 `$EVERGENIE_AGENT`）：
- `evergenie memory note list [--agent <職位>]`：列出名稱、最後寫的人與時間、行數。
- `evergenie memory note show <名稱> [--agent <職位>]`：看內容與「版本」（內容雜湊）。讀別人的可以（`--agent`），寫只寫自己的。
- `evergenie memory note write <名稱> --file <路徑>|- [--revision <版本>]`：整份寫入內文（`--file -` 讀標準輸入）。新檔不用版本；已經有的要帶 `show` 看到的版本，沒帶或對不上就拒絕（別的對話剛改過，重新 show 再整理）；內文自己帶 `---` 開頭的檔頭也拒絕。空內容、不合規的名稱（含 `/`、`.` 開頭、含 `.bak-`、叫 `archive` 或 `索引`、超過 80 字）拒絕。
- 對應 API：`agents.write_note`（`AgentsWriteNoteRequest`），讀走 `vault` 的 `get`／`list`（種類 `note`、id「<職位>/<名稱>」）。

沒有檔頭也收：`notes/` 底下沒有檔頭（第一行不是 `---`，例如用編輯器直接寫的）的 `.md` 不是壞檔、不擋部署——整份當內文，`updated_at`／`created_at` 取檔案修改時間，`by` 是 `external`，`agent` 取職位；讀得到、搜得到。`doctor` 的 `config.notes` 只報一行注意（不是要處理）；下次經 `memory note write`（帶讀到的版本）寫回，核心才補上檔頭。檔頭有但壞掉（YAML 讀不懂、必填欄位缺）照舊是壞檔；第一行剛好是 `---`（水平線、貼來的 YAML）的檔會被當成有檔頭，寫壞了一樣是壞檔，所以內文不要用 `---` 開頭。`agents mv` 只搬認得的筆記（一層 `.md`），子資料夾、`.py`、備份留在舊資料夾。

寫入權限：`memory.md` 只有擔任這個職位的秘書和夜間整理可以寫。`profile.md`（已退休）以前只有 `life` 可以寫，`WriteProfile` 還在、沒有人用；主人的事改寫進主題「主人」，誰都能寫、每行留名（`note.md`）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | 內文裡的 `[[SEC-n]]`、`[[R-n]]` | 任務、待辦 | 選用；失效的連結不擋寫入，由 `doctor` 報告 |

## 保存規則
- 不封存、不刪除（記憶是正本）；超過 `max_lines` 由夜間整理搬進主題筆記或摘要較舊的項目。
- `changelog/`：滿 24 個月封存。

## 從舊系統遷移
| 舊 | 新 | 轉換 |
|---|---|---|
| `features/<職位>/memory/memory.md` | `vault/agents/<職位>/memory.md` | 內文照抄；補 frontmatter（`agent`、`updated_at` 取檔案修改時間、`by: migrate`） |
| `memory/profile.md` | `vault/profile.md` | 同上 |
| `features/<職位>/memory/changelog/` | `vault/agents/<職位>/changelog/` | 整個搬 |
| `features/<職位>/memory/notes/`、`interests.md`、`watch.md`、`reflections/`、`briefings/`（life 等有的） | `vault/agents/<職位>/notes/` | 逐檔搬，補最小 frontmatter |
| `features/<職位>/memory/from-home/` | 不遷移 | 舊的 home→office 記憶回寫通道；新系統走 API（REQ-103），遷移前先確認 `merged/` 以外沒有未處理的檔 |
