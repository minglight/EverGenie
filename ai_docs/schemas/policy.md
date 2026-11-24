# Schema：policy_request（權限變更申請）

Status: DRAFT — 設計 2026-10-25（Q7），D-26. 共通規則見 `README.md`。

秘書被擋住時送的申請單；主人審，核心套用（D-26）。需求：REQ-167、REQ-168、REQ-169。

## 路徑
- `vault/policy/P-<n>.md`（計數器 `policy_request`，前綴 `P-`）

## 欄位
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 是 `P-<n>`；`by` 是最後寫入的人 |
| `from` | string | 是 | — | 送件人（職位 id；主人自己送的是 `owner(…)`） |
| `kind` | enum | 是 | — | `rule`、`guidance` |
| `agent` | string | `rule` 必填 | `from` | 規則給哪位秘書；必須存在於名冊 |
| `action` | enum | `rule` 必填 | — | `read`、`write`、`commit`、`push`、`delete`（`send_external`、`spend`、`settings` 不能開成固定規則） |
| `scope` | string | 否 | 空＝任何地方 | 絕對路徑或 `~/` 開頭的資料夾；不能含 `..` 或萬用字元 |
| `text` | string | `guidance` 必填 | — | 一行、最多 300 字 |
| `why` | string | 是 | — | 被擋的情境（REQ-167） |
| `risk` | string | 否 | — | 送件人自述的風險 |
| `impact` | string | 是 | — | 核心算的白話影響預覽：誰多了什麼 |
| `broad` | bool | 是 | `false` | 範圍過寬（沒寫範圍、或從根算不到四層）：審核畫面紅字警語，不拒收 |
| `status` | enum | 是 | `pending` | `pending`、`applied`、`rejected`、`failed`（套用失敗已還原）、`revoked`、`expired` |
| `permanent` | bool | 是 | `false` | Q7b 起核准一律 `true`；欄位留著相容舊單（舊單可能是 `false`＋`expires_at`） |
| `expires_at` | datetime | 否 | — | Q7b 起核准不設到期，新單一律沒有；只有 Q7b 之前核准的舊單會有（由 `Tick` 處理） |
| `decided_at`、`decided_by` | datetime、string | 離開 `pending` 起必填 | — | 最後一次審、撤銷或到期的時間與人（`owner(…)` 或到期時的 `system`） |
| `note` | string | 否 | — | 主人的備註；套用失敗的原因、到期也接在這裡 |
| `notice` | link | 否 | — | 發給主人的「要你同意」通知 `M-<n>` |

沒有內文。預覽指紋（`fingerprint`）不存檔：每次讀取時由內容版本與有效規則的世代算出來。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `notice` | `messages/M-<n>` | 必須存在 |
| 出 | `agent` | 職位 id | 送件時必須存在於名冊且沒封存 |

## 保存規則
- 正本類，不自動封存、不刪除（稽核用）。

## 從舊系統遷移
None.（舊系統沒有權限申請：秘書被擋住時只能請主人自己改 `overrides/trust.toml` 或帳號設定。）
