# 模組：fitness（`packages/official/fitness`）
Status: 實作 2026-10-25（P6-3c）；設計 2026-10-23（P0-S7，簡版），P6 設計 review 經作者確認（D-27、D-28）。

**目的：** 運動：運動紀錄、訓練計畫、教練檢討。身體數據 2026-10-27 拆到 `body`；秘書是 `health`（REQ-236）。
**種類：** 官方套件（`kind = "data"`：沒有行程，資料與頁面都是宣告式的，D-28）

## MUST
- **FIT-1** — 運動紀錄、訓練計畫、教練對話。
- **FIT-2** — 身體數據不在這個套件（2026-10-27 拆到 `body`，主人才能單獨關掉，REQ-236）。
- **FIT-3** — 資料有三種，都經 `vault`（YAML 標頭、一筆一檔，放 `vault/pkg/fitness/<種類>/`）：`workout`（訓練，核心發編號 `W-<n>`）、`equipment`（器材，`E-<n>`）、`note`（筆記，`N-<n>`；被釘選的那篇就是訓練計畫）。秘書用 `evergenie data`、頁面用 `api.vault` 讀寫；沒有套件自己的 RPC。
- **FIT-4** — 頁面：`log`（選單「運動」）分「記錄／歷史」兩個分頁（`?view=history`）；「記錄」分重訓與有氧，有氧先挑項目（跑步、划船、游泳、騎車）；「歷史」上面是趨勢圖（見 FIT-11），下面是訓練清單；`plan`（不進選單，從 `log` 連過去，唯讀）；`today`（掛在今天頁的 `core.overview.cards`）顯示本週重訓與有氧次數、進行中的訓練，點了去 `log`；這週還沒練、也沒有進行中的訓練時宣告自己是空的（`EmptyLine`，board WEB-177），手機今天頁縮成一行「運動：這週還沒練　＋記一筆」（體重在 `body` 自己的卡片）。
- **FIT-5** — 教練規則（`pages/lib/coach.ts`，用 TypeScript 重寫舊家 `coach.py` 的判斷與句子）：每記一組，比較這個器材上一組（或上次訓練的最後一組）與感覺，給下一組的建議（`advice`），再補「本週某肌群第幾次、共幾組」（`feedback`）；收操時算檢討（`review`：練了什麼、進步、偏弱、下次）；有氧一次記完時算檢討（`cardio.ts` 的 `reviewCardio`：跟同一項目上一次比距離、配速（騎車比均速）、平均心率；一樣快心率更低時說心肺有進步；跑步步頻低於 160 提醒）。不呼叫 AI；門檻集中在 `RULES`（重訓）與 `CARDIO_RULES`（有氧）。收操寫入 `review`、`review_by = "規則"`，秘書事後可以覆蓋（`review_by` 填自己的 id）。
- **FIT-6** — 記一組：沒有進行中的重訓、或進行中那筆最後一組（沒有組就看 `started`）超過 8 小時沒動 → 舊的那筆標完成（`note` 加「忘了收操」、`review` 用規則算）再開新的 `W-`（`status = 進行中`）；否則把這組接在 `sets` 後面。「最後一組的時間」用 vault 記錄的 `updated_at`（組本身沒有時間戳）。
- **FIT-7** — 新增器材時用關鍵字推肌群（`MUSCLE_RULES`）；推不出來就留空，畫面寫「肌群待教練補」，由教練用 `evergenie data set fitness.equipment` 補。器材管理每台也可以改「座位與注意事項」（見 FIT-20）。器材名稱不重名（頁面保證）；移除＝封存，歷史紀錄不動。
- **FIT-13** — 記一組的輸入（`parseSetInput`）：要填重量的器材，重量空白就擋下來說「重量要填」，不能當成 0 kg 記進去（以前 `Number("")` 是 0，會靜靜記成「徒手」）；次數（徒手器材是秒數等）空白說「次數要填」。刻意填 0 kg 照樣記。（2026-11-03，vitest `TestEmptyWeightIsNotZeroKg`）
- **FIT-14** — 進行中那張卡的每一組：左邊器材名稱可以換行，右邊「重量 × 次數 · 感覺」固定一行（手機 390 寬、名稱很長也不切成「很／硬」）。（2026-11-03，e2e `TestActiveSetDetailStaysOnOneLine`）
- **FIT-12** — 記一組出錯（重量要填、範圍不對、存不進去）時，錯誤寫在那張表單「記這組」的正下方，不放整頁最下面（手機上會被底部列蓋住，按了像沒反應）。（2026-11-03，e2e `TestEmptyWeightErrorShowsByButton`）
- **FIT-15** — 今天頁的運動卡：進行中的重訓最後一次動超過 8 小時（跟 FIT-6 同一把尺，`isStale`）就寫「上次的訓練（N 組）還沒收操，點這裡收操」（一組都沒記：「上次開了訓練還沒收操」），不再一直寫「進行中」；點了去「運動」那頁收操。（2026-11-03，vitest `TestForgottenWorkoutIsStaleOnTodayCard`）
- **FIT-16** — 今天頁的運動卡寫「本週完成重訓 N 次 · 有氧 M 次」：只算完成的，進行中那次不算，字要說清楚。（2026-11-03，e2e `fitness.spec.ts` 今天卡）
- **FIT-17** — 挑器材（`pages/lib/picker.tsx`，DEV-146）：由上往下「剛才在用」（這次訓練用過的，最近的在前）、「常用」（扣掉剛才在用的）、「依部位」（腿臀、背、胸、肩、手臂、核心、其他；照器材的主練肌群推，不用另外填，一台可以在兩個部位，沒有肌群的在「其他」；只有一個部位時不顯示）、搜尋（找不到可以直接新增，REQ-304）。管理器材的抽屜開在重訓區最上面（記一組表單之上），按鈕帶 `aria-expanded`。部位對照表（`PART_OF`）要涵蓋 `MUSCLES`，只有頸部三條與「平衡」刻意歸「其他」（vitest 擋）。（2026-11-09，vitest `TestEquipmentPickerParts`、e2e `TestEquipmentPickerRemembersAndParts`）
- **FIT-18** — 選了哪台器材、打開哪個部位存在瀏覽器（`localStorage`，存不了就當沒存）：切到有氧、歷史、離開頁面或重新整理都還在。回到頁面時：記住的器材還在清單裡且 8 小時內（`STALE_HOURS`）就預選它；不然有進行中（沒忘了收操）的訓練就預選最後一組的器材；收操後清掉。（2026-11-09，vitest `TestEquipmentRemembered`、e2e `TestEquipmentPickerRemembersAndParts`）
- **FIT-19** — 重訓的版面由上往下：這台記一組（重量、次數有 −／＋，重量一次 `RULES.step_kg`；空的欄位按 − 不做事，不會變成 0 kg）→ 教練說 → 挑器材 → 進行中（已經做的組，連續同一台併成一段，組號是這台這次累計的第幾組）與收操，收操、刪最後一組出錯就寫在那兩顆按鈕下面；收操後的檢討在最上面並捲過去。（2026-11-09，e2e `TestEquipmentPickerRemembersAndParts`）
- **FIT-20** — 器材的座位調整與注意事項（2026-11-19，作者「教練說要有座位調整資訊跟注意事項」）：`equipment` 有 `seat`（一句，例「座位 3」）與 `cues`（一條一句、最重要的放第一條；讀的時候去掉空白與重複）。重訓選到一台器材，「教練說」就列出它的座位與注意事項（還沒記第一組也列，當練前提醒），記了組之後建議句在上、座位與注意事項在下。器材管理每台有「座位與注意事項」可以改（注意事項一行一條）；健康秘書照主人說的用 `data set fitness.equipment E-<n> --json '{"seat":…,"cues":[…]}'`（座位與注意事項，FIT-20）、`data set fitness.equipment E-<n> --json '{"seat":…,"cues":[…]}'` 更新（`cues` 整份換掉）。（vitest `TestEquipmentSeatAndCues`；e2e `TestWorkoutLogAndPlan`、`TestEquipmentDrawer`）
- **FIT-9** — 套件不帶秘書樣板：飲食、運動、身體由同一位 `health` 秘書負責（`health` 套件，REQ-236）；頁面旁邊的聊天固定跟她（`chat_agent = "health"`）。
- **FIT-10** — 有氧（2026-11-03，DEV-101）＝`type` 是跑步、划船、游泳、騎車（`cardio.ts` 的 `CARDIO`）。四種共同的欄位是距離、時間、平均／最大心率、熱量；各自的欄位：跑步 步頻、步幅（沒填但有步頻時用距離÷總步數推）、爬升、觸地時間、垂直振幅、有氧訓練效果、室內；划船 槳頻、平均功率；游泳 池長、趟數（有池長沒填距離時距離＝池長×趟數）、泳姿、SWOLF；騎車 均速、踏頻、平均功率、爬升、室內。距離一律存公里（`distance_km`），划船、游泳畫面用公尺；`pace` 的單位跟著項目：跑步每公里、划船每 500 m、游泳每 100 m；騎車不存 `pace`、存 `avg_speed_kmh`。手錶才有的欄位收在「更多指數」。欄位定義與輸入範圍集中在 `cardio.ts` 的 `COMMON_FIELDS`、`FIELDS`、`SPORTS`。
- **FIT-11** — 趨勢圖（歷史分頁上方，`charts.tsx`）：點的橫向位置照真正的日期（時間軸），整張折線圖都是點擊範圍，點哪裡就挑橫向最近的那次，下面那行顯示日期與數值（預設最近一次）；同一天好幾次往右錯開。長條一週一格，手機上比 44px 窄：正式的讀法是下面那行與表格，點長條只是捷徑（UXC-5 的例外），「看數字」展開表格。重訓：沒選器材＝最近 12 週每週總量（重量×次數）長條；選了器材＝每次的最大重量、估算 1RM（Epley：重量×(1+次數/30)，只做一下時就是那個重量）或總量；只記過徒手的器材改看最多次數、總次數，有負重的器材不把徒手組畫進去（單位才一致）。每週總量含進行中那次。有氧：沒選項目＝最近 12 週每週分鐘；選了項目＝距離、時間、配速（騎車均速）、平均心率與該項目自己的指數；配速、SWOLF、觸地時間、垂直振幅越低越好，y 軸反過來（往上＝變好）。顏色只用 token（`var(--accent)` 等）。

## NEVER
- **FIT-N1** — 不給醫療診斷或治療性處方；紅旗症狀（胸痛、暈眩、呼吸困難、關節突然劇痛腫脹、麻木）請他停下來就醫（寫在 `health` 樣板的「這個職位特有的界線」段）。
- **FIT-N2** — 不匯入舊家的運動資料（D-27，搬家當天另轉）。
- **FIT-N3** — 教練的即時建議與收操檢討不呼叫 AI（純規則）；AI 教練的檢討是秘書的事，寫回 `review`。

## 職責
- 三種資料的欄位規格（`schemas/workout.toml`、`equipment.toml`、`note.toml`）與三個頁面（`pages/log.tsx`、`plan.tsx`、`today.tsx`）；計算放在 `pages/lib/` 的純函式（`coach.ts` 重訓教練規則、`cardio.ts` 有氧欄位與檢討、`workout.ts` 訓練、器材與重訓趨勢、`dates.ts`），圖在 `charts.tsx`，I/O 在 `pages/lib/store.ts`。

## 不負責
- 飲食與熱量 → `nutrition`；身體數據 → `body`；秘書（職責樣板）→ `health`；習慣打卡 → `habits`（可由 `fitness:*` 當自動來源）。
- 欄位檢查、編號、封存 → `vault`；套件探索、註冊資料種類、`packages sync` → `pkg`。

## 介面
資料種類（`[[contributes.data]]`，完整 vault 種類名稱 `fitness.<種類>`）：

| 種類 | 編號 | 欄位 |
|---|---|---|
| `workout` | `seq`，`W-` | `type`（重訓／跑步／划船／游泳／騎車，必填）、`date`（必填）、`started`、`finished`（`YYYY-MM-DD HH:MM`）、`status`（進行中／完成，必填）、`feeling`（輕鬆／剛好／吃力）、`note`、`sets`（list，每項 map：`equipment`、`weight`、`reps`、`feel`）、`distance_km`、`duration_min`（number）、`pace`（`m:ss`，單位跟著項目）、有氧指數（number：`avg_hr`、`max_hr`、`calories`、`cadence_spm`、`stride_m`、`gct_ms`、`vertical_osc_cm`、`training_effect`、`elevation_m`、`avg_power_w`、`stroke_rate_spm`、`pool_m`、`laps`、`swolf`、`avg_speed_kmh`、`cadence_rpm`；`stroke_style` enum；`indoor` bool，見 FIT-10）、`source`、`review`、`review_by`；`date_field = "date"` |
| `equipment` | `seq`，`E-` | `name`（必填）、`primary`、`secondary`（list of string）、`type`（例「徒手」）、`unit`（次／秒）、`seat`（座位調整）、`cues`（注意事項，list of string；FIT-20） |
| `note` | `seq`，`N-` | `title`（必填）、`date`、`pinned`（bool）；內文 Markdown；`date_field = "date"` |

頁面（`[[contributes.page]]`／`[[contributes.menu]]`）：`log`（選單「運動」、圖示 `dumbbell`、`group_hint = "daily"`、`order = 20`，網址 `/p/fitness/log?view=history`）、`plan`（`/p/fitness/plan`）、`today`（slot `core.overview.cards`）。 `log` 另宣告 `section = "health"`、`section_title = "健康"`（看板跟飲食、身體合成一頁「健康」的分頁）與 `chat = true`、`chat_agent = "health"`（頁面旁邊就是跟健康秘書的聊天）（REQ-235、236）。

秘書的指令（`health` 樣板內說明）：`evergenie data list fitness.workout --since <日期>`、`data add fitness.workout --json '{…}'`（補記有氧，欄位見 FIT-10）、`data set fitness.workout W-<n> --json '{"review":…,"review_by":"health"}'`、`data set fitness.equipment E-<n> --json '{"primary":[…]}'`、`data add fitness.note … --body-file`（訓練計畫＝`pinned`）。

## 事件
None.

## 儲存
資料在 `vault/pkg/fitness/{workout,equipment,note}/<編號>.md`（YAML 標頭、內文自由 Markdown）；欄位規格在套件資料夾的 `schemas/`，格式見 `schemas/package.md`「資料種類」。

## 依賴
`sdk`

## 錯誤
- 欄位不符（缺必填、種類不在列舉、型別錯）→ vault 回 `INVALID` 並說明哪個欄位；頁面把原因顯示在出錯的區塊旁，`evergenie data` 印在標準錯誤。頁面自己先檢查輸入（重量 0–500 kg、次數 1–200、有氧距離 跑步 0.1–100 km、划船 100–50000 m、游泳 25–20000 m、騎車 0.5–400 km／時間 1–720 分、心率 30–230 且最大不低於平均等，範圍在 `cardio.ts`），說白話。
- 套件停用時三種資料暫時看不到，頁面顯示讀取失敗的原因；重新啟用資料回來（REQ-173）。

## 不變條件
- 運動紀錄、訓練計畫、教練對話。
- 秘書在 `health`，身體數據在 `body`，這個套件可以單獨停用。
- 記重訓的器材搜尋找不到時有「新增「X」」，新增後直接選好。

## 測試計畫
- 純函式（vitest，`pages/lib/*.spec.ts`）：`cardio.spec.ts` 的 `TestCardioPaceUnits`、`TestCardioInputChecks`、`TestReviewCardio`、`TestCardioTrendsAndDetails`；`coach.spec.ts` 的 `TestAdviceRules`、`TestAdviceFeedbackAddsWeekMuscles`、`TestReviewSentences`、`TestReviewRun`、`TestInferMuscles`；`workout.spec.ts` 的 `TestEquipmentSeatAndCues`、`TestStaleWorkoutStartsNew`、`TestEquipmentPickerRules`、`TestInputChecks`（REQ-127）、`TestStrengthTimeline`、`TestCardioSummaryInHistory`。
- Go：`cmd/evergenie` `TestFitnessKindsRegistered`（REQ-127：三種 kind 註冊、缺 `type` 被擋、`sets` 收得下、沒有 `fitness.body`）；`internal/feature/packages` `TestOfficialPackagesDiscovered` 涵蓋 manifest、樣板與規格檔都合格。
- e2e（`fitness.spec.ts`，mobile／desktop）：`TestWorkoutLogAndPlan`（REQ-127：忘了收操、記兩組看到建議句、徒手、刪最後一組、收操有檢討、歷史與折線、`plan` 頁顯示釘選筆記）、`TestEquipmentDrawer`（REQ-127）、`TestCardioLog`（四種有氧各記一次、心率與各自欄位存對、檢討、歷史摘要與趨勢圖）、`TestFitnessTodayCard`（REQ-114、127、236）、`TestHealthTabsAndSideChat`、`TestHealthPartsCanBeTurnedOff`（REQ-235、236，健康頁）。截圖：`TestShotsFitnessLog`、`TestShotsFitnessHistory`（REQ-115）、`TestShotsFitnessCardio`。

## 已知限制
- 舊資料搬家當天另轉（D-27），這個套件不匯入。
- 有氧的指數靠手動填（或秘書從截圖補記）；不接手錶、Strava 的 API。
- 舊家的欄位 `kind`（重訓／跑步）在新系統叫 `type`：vault 檔案標頭的 `kind` 已經是資料種類，套件欄位不能同名（`reservedDataFields`）。搬家時要改名。
- 舊家的「習慣自動打卡」（`habits` 讀運動紀錄）不在這個套件；`habits` 套件用 `fitness.workout`（與 `body.entry`）當來源。
- 頁面一次讀完全部訓練、器材再在前端算（一個人用、資料量小）；量大到變慢再加伺服器端篩選。
- 「忘了收操」用記錄的修改時間判斷最後一組；教練或手動改了那筆（例如補 `review`）也會更新時間，8 小時的計時重新開始。
- 器材肌群沒有 `src`（規則推的、教練查的）這個欄位；舊家有。
- 官方套件頁面的 Tailwind class 靠看板建置時掃描 `packages/official/**/pages`；`packages/mine/` 的頁面不會被掃到。
- 頁面裡原本的「跟 <人名> 說」按鈕拿掉了（2026-10-27）：旁邊就是聊天（REQ-236）。
