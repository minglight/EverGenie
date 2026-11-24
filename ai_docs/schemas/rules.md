# Schema：rules_version、rules_proposal（規矩的版本與修改提案）

Status: DRAFT — 設計 2026-11-02（減法計畫 S7），D-44. 共通規則見 `README.md`；行為見 `modules/system.md`。

規矩文件本身（憲法、宗旨、法律…）在家的資料夾，位置由 `home.toml [[rules.docs]]` 決定（`home.md`）；這裡只存改動的紀錄。

## 路徑
- 版本：`vault/rules/versions/RV-<n>.md`（計數器 `rules_version`，前綴 `RV-`）。
- 提案：`vault/rules/proposals/RP-<n>.md`（計數器 `rules_proposal`，前綴 `RP-`）。
- 兩種檔頭的 `kind` 都一定要寫（`vault/rules/` 不在 vault 照資料夾認種類的清單裡，重新掃描時靠它）。

## 欄位

### rules_version（版本）
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 是 `RV-<n>`；`by` 是改的人（`owner(…)`；「改之前的樣子」是 `system`） |
| `kind` | enum | 是 | — | 固定 `rules_version` |
| `doc` | string | 是 | — | 哪一份文件（`[[rules.docs]]` 的 `id`，例 `law`） |
| `title` | string | 是 | — | 當時文件的標題 |
| `source` | enum | 是 | — | `save`（主人存檔）、`restore`（還原）、`proposal`（採用提案）、`outside`（改之前的樣子：第一次經看板改，或在看板外改過） |
| `note` | string | 否 | — | 改了什麼（主人寫的，最多 500 字；還原預設「還原成 RV-n」、採用預設「採用 <秘書> 的提案 RP-n」） |
| `proposal` | string | 否 | — | 採用的提案 `RP-<n>` |
| `restored_from` | string | 否 | — | 還原自哪一版 `RV-<n>` |

內文：那個時間點這份文件的全文（一段的文件就是那一段的內容，不含標題）；結尾空行去掉、換行一律 `\n`。

### rules_proposal（修改提案）
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 是 `RP-<n>` |
| `kind` | enum | 是 | — | 固定 `rules_proposal` |
| `doc` | string | 是 | — | 哪一份文件 |
| `section` | string | 否 | 空＝整份換掉 | 改文件裡哪個標題底下的內容（標題文字，不含 `#`） |
| `from` | string | 是 | — | 提案的秘書（職位 id） |
| `why` | string | 是 | — | 為什麼要改（最多 2000 字） |
| `status` | enum | 是 | `pending` | `pending`、`accepted`、`rejected` |
| `base` | string | 否 | — | 提案時文件的 revision（文件不在時是空的）；跟現在的不同＝過期，不能採用 |
| `decided_at`、`decided_by` | datetime、string | 離開 `pending` 起必填 | — | 主人決定的時間與身分 |
| `note` | string | 否 | — | 主人採用或退回時寫的話（最多 500 字，會一起回給秘書） |
| `notice` | string | 否 | — | 給主人的通知 `M-<n>`（決定後收掉） |
| `version` | string | 否 | — | 採用後留下的版本 `RV-<n>` |

內文：提案的新內容：整份的新全文，或 `section` 那一段的新內容（最多 200 KB）。

## 連結
- `rules_version.proposal` → `rules_proposal.id`；`rules_proposal.version` → `rules_version.id`（採用後兩邊互指）。
- `rules_version.restored_from` → 同一份文件的另一個 `rules_version.id`。
- `rules_proposal.notice` → `notify` 的通知 `M-<n>`。
- `doc` → `home.toml [[rules.docs]]` 的 `id`（設定拿掉那份文件後，舊版本與提案留著，提案標過期）。

## 保存規則
- 都不封存、不自動清：版本是規矩的歷史；提案量很少（每位秘書同時最多 10 個待決）。
- 改檔頭請經 `evergenie rules` 或看板，不要手改（`kind` 寫錯重開後就認不得）。

## 從舊系統遷移
- 無：舊系統沒有規矩頁；舊的憲法、法律文字本來就在家的 `docs/`，不用搬。第一次在看板改時會自動留一版「改之前的樣子」。
