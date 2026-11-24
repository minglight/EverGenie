# 模組：nutrition（`packages/official/nutrition`）
Status: 實作 2026-10-25（P6-3b）；設計 2026-10-23（P0-S7，簡版），P6 設計 review 經作者確認（D-27、D-28）。

**目的：** 飲食：餐點照片估算、飲食紀錄、趨勢、目標。秘書是 `health`（2026-10-27，REQ-236）。
**種類：** 官方套件（`kind = "data"`：沒有行程，資料與頁面都是宣告式的，D-28）

## MUST
- **NUT-1** — 飲食紀錄（照片估算）、趨勢、目標。
- **NUT-2** — 資料只有三種，都經 `vault`（YAML 標頭、一筆一檔，放 `vault/pkg/nutrition/<種類>/`）：`meal`（餐點，核心發編號 `M-<n>`）、`goal`（目標，只用編號 `main`）、`fav`（常吃餐點，`F-<n>`）。秘書用 `evergenie data`、頁面用 `api.vault` 讀寫；沒有套件自己的 RPC。
- **NUT-3** — 一餐的熱量、蛋白質、碳水、脂肪是整餐合計（數字）；品項放 `items`（list，每項 `name`、`qty`、`kcal`、`protein`、`carb`、`fat`）；照片放 `photos`（`/share/<名字>` 連結，來自聊天附件）。把握度 `conf` 只有高、中、低。
- **NUT-4** — 頁面：`log`（選單「飲食」）看某一天的餐點與對目標的進度、記常吃、跟記錄秘書說話；`trend`（不進選單）看近 14 天熱量、近 7 天平均並編輯目標；`today`（掛在今天頁的 `core.overview.cards`）顯示今天對目標的兩條進度，點了去 `log`；今天一餐都還沒記時宣告自己是空的（`EmptyLine`，board WEB-177），手機今天頁縮成一行「飲食：今天還沒記　＋記一餐」。
- **NUT-5** — 趨勢裡沒有記錄的日子留空（不畫 0、不算進平均）；超過目標用警示色、不用責備的字眼；沒設目標（`kcal`、`protein` 為 0 或沒有 `goal/main`）顯示「還沒設目標」加設定入口。頁面讀目標用 `vault.list`（`pages/lib/store.ts` 的 `loadGoal`，含封存的，挑 `main`），不用 `vault.get`：還沒設目標是常態，`get` 會回 404、瀏覽器每開一次頁就在主控台記一筆錯（2026-11-15 DEV-157 新家第一天走查⑤）。e2e `nutrition.spec.ts` → `TestGoalLoadNeverAsks404`。
- **NUT-6** — 刪除餐點是封存（檔案還在），刪之前在畫面內確認；「記一筆」常吃餐點是複製成新的一餐，日期與時間用現在、餐別依時間猜（5–10 早餐、10–14 午餐、14–17 點心、17–21 晚餐、其他宵夜）。
- **NUT-8** — `log` 的換日只走到今天：在今天時「後一天」按不下去（`canGoNext`，跟日期選擇器的 `max` 一致），不會按到明天的空白頁以為記錄不見了。日期只寫一次：就是中間那個日期選擇器（`nutrition-datepick`，至少 44px 高、字比較大），上面不再另寫一行日期，底下只說「今天」或給「回到今天」（`pages/log.tsx` 的 `DateNav`；DEV-156 U-29）。（2026-11-03，vitest `TestNextDayStopsAtToday`、e2e `TestFavLogsMeal`、`TestMealLogWithPhoto`）
- **NUT-9** — `log` 的字跟著看的那天走：沒設目標時看別天寫「這天累計」不寫「今天累計」（`noGoalText`）；這天沒有記錄的提示，沒有常吃餐點就不叫人「點上面的常吃餐點」（`emptyDayHint`）。（2026-11-03，vitest `TestDayTextsFollowTheShownDay`）
- **NUT-7** — 套件不帶秘書樣板：飲食、運動、身體由同一位 `health` 秘書負責（`health` 套件，看照片估算、用 `evergenie data` 記錄、不給醫療建議都寫在那份樣板）；頁面旁邊的聊天固定跟她（`chat_agent = "health"`）。

## NEVER
- **NUT-N1** — 不做照片自動辨識或營養資料庫：看圖估算是秘書（AI）的事，頁面只存與顯示。
- **NUT-N2** — 不給醫療診斷或治療性的飲食處方，不鼓勵極端節食（寫在 `health` 樣板的「這個職位特有的界線」段）。
- **NUT-N3** — 不匯入舊家的飲食資料（D-27，搬家當天另轉）。

## 職責
- 三種資料的欄位規格（`schemas/meal.toml`、`goal.toml`、`fav.toml`）與三個頁面（`pages/log.tsx`、`trend.tsx`、`today.tsx`）；計算（加總、猜餐別、趨勢、平均、日期）放在 `pages/lib/nutrition.ts` 的純函式，I/O 在 `pages/lib/store.ts`。

## 不負責
- 運動 → `fitness`；習慣 → `habits`。
- 聊天附件的上傳與 `/share/` 服務 → `web`（`POST /api/web/upload`）；欄位檢查、編號、封存 → `vault`；套件探索、註冊資料種類、`packages sync` → `pkg`。
- 體重、體脂等身體數據 → `body` 套件；秘書（職責樣板）→ `health`。

## 介面
資料種類（`[[contributes.data]]`，完整 vault 種類名稱 `nutrition.<種類>`）：

| 種類 | 編號 | 欄位 |
|---|---|---|
| `meal` | `seq`，`M-` | `date`（date，必填）、`at`（`YYYY-MM-DD HH:MM`，必填）、`meal`（早餐／午餐／晚餐／點心／宵夜，必填）、`kcal`、`protein`、`carb`、`fat`（number，必填，整餐合計）、`items`（list）、`photos`（list）、`conf`（高／中／低）、`note`、`fav`（來源常吃餐點）；`date_field = "date"` |
| `goal` | `given`，只用 `main` | `mode`（減脂／增肌／維持／自訂）、`kcal`、`protein`、`carb`、`fat`（number，必填；`carb`、`fat` 為 0＝不設目標） |
| `fav` | `seq`，`F-` | `name`、`kcal`、`protein`、`carb`、`fat`（必填）、`items`（list） |

頁面（`[[contributes.page]]`／`[[contributes.menu]]`）：`log`（選單「飲食」、圖示 `utensils`、`group_hint = "daily"`、`order = 10`，網址 `/p/nutrition/log?date=YYYY-MM-DD`）、`trend`（`/p/nutrition/trend`，目標編輯在 `#goal`）、`today`（slot `core.overview.cards`）。 `log` 另宣告 `section = "health"`、`section_title = "健康"`（看板跟運動、身體合成一頁「健康」的分頁）與 `chat = true`、`chat_agent = "health"`（頁面旁邊就是跟健康秘書的聊天）（REQ-235、236）。

秘書的指令（`health` 樣板內說明）：`evergenie data add nutrition.meal --json '{…}'`、`data set`、`data rm`、`data list nutrition.meal --since <日期>`、`data get nutrition.goal main`、`data list nutrition.fav`。

## 事件
None.

## 儲存
資料在 `vault/pkg/nutrition/{meal,goal,fav}/<編號>.md`（YAML 標頭、內文自由 Markdown）；欄位規格在套件資料夾的 `schemas/`，格式見 `schemas/package.md`「資料種類」。照片檔在 `runtime/share/`（聊天附件上傳，`web` 負責）。

## 依賴
`sdk`

## 錯誤
- 欄位不符（缺必填、餐別不在列舉、型別錯）→ vault 回 `INVALID` 並說明哪個欄位；頁面把原因顯示在出錯的區塊旁，`evergenie data` 印在標準錯誤。
- 套件停用時三種資料暫時看不到（`vault` 回「沒有這種資料」），頁面顯示讀取失敗的原因；重新啟用資料回來（REQ-173）。

## 不變條件
- 飲食紀錄（照片估算）、趨勢、目標。

## 測試計畫
- 純函式（vitest，`pages/lib/nutrition.spec.ts`，REQ-128）：`TestDayTotals`、`TestGuessMeal`、`TestTrendSkipsEmptyDays`。
- Go：`cmd/evergenie` `TestNutritionKindsRegistered`（REQ-128：三種 kind 註冊、缺 `date` 被擋、`items` 收得下、`goal` 要自己給 id）；`internal/feature/packages` `TestOfficialPackagesDiscovered` 涵蓋 manifest、樣板與規格檔都合格。
- e2e（`nutrition.spec.ts`，mobile／desktop）：`TestMealLogWithPhoto`（REQ-128：合計對目標、縮圖與大圖、換日、刪除）、`TestFavLogsMeal`（REQ-128：常吃餐點記一筆、存成常吃）、`TestTrendAndGoal`（REQ-128：14 格、留空、改目標存檔重新整理還在）、`TestNutritionTodayCard`（REQ-114、REQ-128：今天頁的卡片）。截圖：`TestShotsNutritionLog`、`TestShotsNutritionTrend`（REQ-115）。

## 已知限制
- 舊資料搬家當天另轉（D-27），這個套件不匯入。
- 不做照片自動辨識：由秘書看圖估算，估算誤差兩三成是預期的（秘書樣板有寫）。
- 頁面一次讀完全部餐點再在前端篩日期與加總（一個人用、一天幾筆，資料量小）；量大到變慢再加伺服器端的日期篩選。
- 日期與「現在」用瀏覽器的本地時區；網址 `?date=` 用 `replaceState` 更新，上一頁不會逐日回退。
- 官方套件頁面的 Tailwind class 靠看板建置時掃描 `packages/official/**/pages`；`packages/mine/` 的頁面不會被掃到，只能用建置時已經有的 class。
- 秘書樣板（在 `health`）建立的新秘書是停用的；沒啟用並開 session 前，頁面的「跟 <人名> 說」不顯示。
