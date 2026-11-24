# 模組：health 與 body（`packages/official/health`、`packages/official/body`）
Status: 實作 2026-10-27（作者：「我想要讓這三個都同一人把關」，REQ-236；body 從 `fitness` 拆出來）。2026-11-02 把 `body.md` 併進來。

**目的：** `health`：健康秘書，飲食、運動、身體由同一位秘書把關（記錄、估算、檢討、看趨勢）。`body`：身體數據（體重、體脂等），一天一筆，看 7 天平均與趨勢。
**種類：** 官方套件（兩個都是 `kind = "data"`：`health` 只帶一份秘書樣板；`body` 沒有行程，資料與頁面都是宣告式的，D-28）

## MUST
health：
- **HLT-1** — 套件只帶一位秘書樣板 `health`（`agents/health/agent.md`）；`nutrition`、`fitness`、`body` 不再各帶秘書。樣板不在掃描時自動套用，要 `evergenie packages sync health` 預演、主人同意後 `--apply`（D-28）。
- **HLT-2** — 樣板寫明：開始前用 `evergenie packages list` 看飲食、運動、身體哪幾項開著；**關掉的那項不記、不追問、不主動提**（主人問起可以聊，但不寫資料）；關掉的種類 `evergenie data` 會擋，不要繞過；主人要開關某項先在對話確認，再用 `evergenie packages enable|disable`。
- **HLT-3** — 三個套件的頁面都宣告 `chat_agent = "health"`：看板的「健康」頁頂端是她的橫幅（`PageAgentBanner`），聊天固定跟她（寬螢幕右邊一欄、窄螢幕「聊天」分頁），還沒有對話時就地開一個（`session.open`）。她不在名冊或停用時，照 REQ-235 讓主人選人。

body：
- **BOD-1** — 身體數據（體重等）。
- **BOD-2** — 資料只有一種 `entry`（vault 種類 `body.entry`，放 `vault/pkg/body/entry/<日期>.md`），編號就是日期 `YYYY-MM-DD`，一天一筆。秘書用 `evergenie data`、頁面用 `api.vault` 讀寫；沒有套件自己的 RPC。**誰聽到體重都可以寫進同一份**（平常是 `health` 秘書），不會有兩份數字。
- **BOD-3** — 頁面：`log`（選單「身體」）記體重（必填）與其他欄位（選填）；同一天再記是覆蓋（`vault.patch` 只改有給的欄位），不存在才 `vault.put`（id＝日期）；7 天平均、趨勢圖只算有記錄的日子，沒記錄的不補 0。`today`（掛在今天頁的 `core.overview.cards`）顯示最近體重與 7 天平均，點了去 `log`；一筆體重都還沒有時宣告自己是空的（`EmptyLine`，board WEB-177），手機今天頁縮成一行「體重：還沒記　＋記一筆」。
- **BOD-5** — 今天頁的「今天的身體」卡片：最近一筆體重不是今天量的，就在數字旁標「昨天量的」或「10/13 週二量的」（`measuredLabel`）；今天量的不標。（2026-11-03，vitest `TestTodayCardSaysWhenWeighed`、e2e `TestFitnessTodayCard`）
- **BOD-4** — 是獨立的套件，主人可以單獨停用「身體」（設定 → 套件，或「健康」頁的「顯示設定」開關，board WEB-137）；停用時分頁與卡片不見、`evergenie data` 寫不進去，資料留著，重新啟用就回來。

## NEVER
- **HLT-N1** — 不給醫療診斷或治療性的飲食、用藥處方；不鼓勵過度訓練、極端節食；紅旗症狀請他停下來就醫（寫在樣板「這個職位特有的界線」段）。
- **BOD-N1** — 不匯入舊家的身體資料（D-27，搬家當天另轉）。

## 職責
- `health` 秘書的職責樣板，照 secretaries 卡 SCR-5 的固定段落（2026-11-16 DEV-161，作者：體能教練＋運動營養師＋健康管理三合一，不診斷、不開藥）：基本知識分運動、飲食、身體三塊，跟著 `fitness`、`nutrition`、`body` 開關走（關掉的那塊不用），沒有可選知識；「聊到什麼先看什麼」三條（器材或動作、店或餐、體重血壓或健檢，只用開著的那幾項；血壓沒有欄位，寫在 `body.entry` 那天的 `note`）；回答時先看資料、給完整分析附趨勢與理由、收在下一步、安全優先；三項資料的 `evergenie data` 用法（`nutrition.*`、`fitness.*`、`body.entry`）與怎麼看哪幾項開著放在「你主動照顧的事」底下；「範圍以外的事」照 D-40：不主動去管，但主人叫就做。
- `body`：欄位規格 `schemas/entry.toml`、頁面 `pages/log.tsx`、`pages/today.tsx`；計算放在 `pages/lib/body.ts` 的純函式（輸入檢查、7 天平均、趨勢線），日期在 `pages/lib/dates.ts`，I/O 在 `pages/lib/store.ts`。

## 不負責
- 飲食、運動的資料與頁面 → `nutrition`、`fitness`；「量體重」習慣的自動打卡 → `habits`（子項目 `body:weight` 讀 `body.entry`）。
- 開關套件、探索、註冊資料種類 → `packages` 塊（`package.set_enabled`）；欄位檢查、編號、封存 → `vault`；看板的橫幅、聊天、「顯示設定」開關 → `board`（WEB-45）。

## 介面
- `health`：`[[contributes.agent]]` `id = "health"`、`template = "agents/health/agent.md"`（`duty`：健康：飲食、運動、身體一起照顧）。
- `body` 資料種類：`entry`（`given`，編號＝日期）：`date`（必填）、`weight`、`fat`、`bmi`、`bmr`、`visceral`、`score`（number）、`note`；`date_field = "date"`。
- `body` 頁面：`log`（選單「身體」、圖示 `scale`、`group_hint = "daily"`、`order = 21`，網址 `/p/body/log`；`section = "health"`、`section_title = "健康"`、`chat = true`、`chat_agent = "health"`，REQ-235、236）、`today`（slot `core.overview.cards`）。
- 秘書的指令（`health` 樣板內說明）：`evergenie data list body.entry --since <日期>`、`data add body.entry --id <日期> --json '{…}'`、`data set body.entry <日期> --json '{…}'`。

## 事件
None.

## 儲存
- `health`：None（秘書樣板套用後在家的 `agents/health/`，由 `agent` 管）。
- `body`：`vault/pkg/body/entry/<日期>.md`（YAML 標頭）。

## 依賴
`sdk`

## 錯誤
- 名冊上沒有 `health`（還沒 `packages sync health --apply`，或停用）：健康頁沒有橫幅，聊天退回「跟誰聊」選人（REQ-235）。
- 身體欄位不符 → vault 回 `INVALID` 並說明哪個欄位；頁面自己先檢查（體重 20–300 kg、體脂 3–60% 等），說白話。
- 刪掉（封存）的那一天可以再記一次：新增一筆同日期的，舊的留在封存裡不動（vault VLT-15，DEV-151）。頁面新增時 vault 回 `CONFLICT`「這個編號已經有資料了」（畫面還沒跟上，例如秘書剛記了同一天）就改成修改，照「同一天再記就是覆蓋」。
- `body` 停用時資料暫時看不到，頁面顯示讀取失敗的原因（REQ-173）。

## 不變條件
- 一位秘書管飲食、運動、身體；關掉的項目不記。
- 身體可以單獨停用，停用時寫不進去、資料留著。

## 測試計畫
- 純函式（vitest，`body/pages/lib/body.spec.ts`）：`TestBodyWeeklyAverage`、`TestBodyTrendAndInput`（REQ-129）。
- Go：`cmd/evergenie` `TestHealthOneAgentEachPartCanBeOff`（REQ-236：`packages sync health` 預演、三項預設啟用、停用身體後寫不進去而飲食照常、重新啟用就好）、`TestBodyKindRegistered`（REQ-129、236：`body.entry` 註冊、不給 id 被擋、同一天不能再記、其他秘書也能改、刪除＝封存）；`internal/feature/packages/pkg` `TestOfficialPackagesDiscovered` 涵蓋 manifest 與樣板合格；`cmd/evergenie` `TestOfficialDutiesKnowledgeFormat` 守 `health` 職責照 secretaries 卡 SCR-5 的段落順序、沒有可選知識。
- e2e：`fitness.spec.ts` 的 `TestHealthTabsAndSideChat`、`TestHealthPartsCanBeTurnedOff`（REQ-235、236）、`TestBodyMetrics`、`TestFitnessTodayCard`（體重卡片）；截圖 `shots.spec.ts` 的 `TestShotsFitnessBody`（REQ-115）。e2e 的家沒有 `health` 秘書，固定秘書的橫幅與就地開對話只在試用站看過。

## 已知限制
- 刪掉的那筆沒有還原的入口（`vault` 還不支援封存記錄的還原）；同一天再記是新的一筆，舊的只留在封存檔。
