# Schema：job（排程工作）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

一個排程工作 = 什麼時候、做什麼（內文就是指令）。需求：REQ-15–22。

## 路徑
- `vault/agents/<職位 id>/jobs/<job id>.md`（內文是給秘書的指令）
- 系統級（不屬於某個職位，例每天早上的額度暖機 `usage-warmup`）：`vault/jobs/<job id>.md`

## 欄位
| 欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema`、`id`、`created_at`、`updated_at`、`by` | 共通 | 是 | — | `id` 在同一個職位內唯一，例 `nightly`、`goodnight` |
| `agent` | string | 職位級必填 | — | 職位 id；系統級不寫 |
| `name` | string | 是 | — | 畫面顯示名稱，例「重整記憶」 |
| `cron` | list of string | 是 | — | 一個或多個五欄位 cron（REQ-15）；時區用 `home.toml` 的 `user.timezone` |
| `enabled` | bool | 是 | `true` | 使用者可以開關、改時間（REQ-18） |
| `catchup` | enum | 否 | `skip` | `once`（錯過後醒來補最近一次，6 小時內）、`skip`（REQ-17） |
| `action` | enum | 否 | `none` | 做完之後的動作：`rotate`（重開 session，清 context，REQ-80）、`none`；系統級（沒有 `agent`）的工作可以是 `usage_warmup`（額度暖機）、`limit_watch`（額度開通通知），由 `engine` 模組處理（REQ-86）；`backup_sweep`（每週的備份掃描，由 system 塊的 backupsweep 處理，BSW-1；家的遷移會放一份 `backup-sweep`） |
| `sites` | 舊欄位 | 否 | — | 已廢：2026-10-31 起家與家不連線。舊值讀取時忽略，排程在本家執行 |
| `windows` | list of string | 否 | — | 時段型排程（Q1，舊 `windows`）：`HH:MM-HH:MM`，跟 `cron` 擇一；每天從時段裡挑 `per_day` 段、每段挑一分鐘觸發 |
| `weekend_windows` | list of string | 否 | — | 週末用的時段，沒寫就週末也用 `windows` |
| `per_day` | int | 否 | `1` | 時段型：一天觸發幾次 |
| `idle_min` | int | 否 | `0` | 時段型：主人閒置幾分鐘以上才開場 |
| `removed_at` | time | 否 | — | `jobs rm` 移除時記下（Q1）；之後即使升級也不會被重新打開 |
| `silent` | bool | 否 | `false` | `true` 代表不推播（夜間整理等系統工作，REQ-20） |
| `source` | string | 否 | — | 來源：`migrate`、套件 id（P6 起）、`user` |

內文：給秘書的指令（取代舊 `prompt = "jobs/xxx.md"` 指向的檔案）。

工作的執行規則（不存在這個檔裡，屬 `jobs` 業務模組）：同一分鐘只跑一次；秘書忙、2 分鐘送不進去就另開專屬 session，閒置 30 分鐘收掉，同時最多 4 個（REQ-16）；做完沒有只看那則話的結局，沒做完只記 log、不通知（SCH-4；「20 分鐘還沒跑成就通知」已廢，S5，2026-11-02）。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `agent` | 職位 id | 必須存在 |
| 入 | reminder 的 `source: routine:<id>` | 這個 job | 純字串，不強制存在 |

## 保存規則
- 遷移與套件帶來的排程不封存、不刪除，停用改 `enabled: false`。使用者自己加的（`source: user`）可以用 `jobs rm` 移除：先停用、記 `removed_at`、再封存（不刪除，REQ-5）。
- 執行紀錄不存在這裡：高頻事件進 `runtime/logs/`，每個職位每天最多執行次數看 agent 的 `budget`。每個排程最近一次的結果與今天的次數是執行期狀態，存 `runtime/jobs/last-runs.json`（SCH-16、D-48），`ListJobs` 帶出 `last_run`、`last_result`、`last_reason`、`runs_today`。

## 從舊系統遷移
None.
