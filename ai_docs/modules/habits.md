# 模組：habits（`packages/official/habits`）
Status: 實作 2026-10-25（P6-4a）；設計 2026-10-23（P0-S7，簡版），P6 設計 review 經作者確認（D-27、D-28）。

**目的：** 原子習慣：約定、打卡、自動來源、子項目、累計或連續模式、點數錢包。
**種類：** 官方套件（`kind = "program"`：背景行程讀 vault、算規則、把結果寫回 vault、賺到點通知；仍然**沒有 RPC**，D-28）

## MUST
- **HAB-1** — 原子習慣：約定、打卡、自動來源、子項目、累計或連續模式、點數錢包。規則的正本是 `internal/rules`（純函式，用 Go 重寫舊系統 `habits.py` 的概念）。
- **HAB-2** — 資料有六種，都經 `vault`（YAML 標頭、一筆一檔，放 `vault/pkg/habits/<種類>/`）：`habit`（習慣，`H-<n>`）、`checkin`（打卡，`C-<n>`）、`status`（每個習慣一筆，編號是習慣編號的小寫 `h-<n>`）、`wallet`（只有 `main`）、`exchange`（待辦點數兌換，`X-<n>`，HAB-18）、`welcome`（第一次介紹習慣，只有 `main`，HAB-21）。**`status` 與 `wallet` 只有套件的程式寫，別人不寫**；秘書與頁面寫 `habit`、`checkin`，頁面寫 `exchange`，`welcome` 由介紹過的秘書寫 `done`、不用介紹的家由程式寫 `skipped`。
- **HAB-3** — 達標一律以週（週一到週日）算：每週型打卡數 ≥ `target`；每天型沒有連續超過 `miss_days` 天沒做（今天還沒過完不算漏，週日當天做了算過完）。第一次累積 2 週達標 +1 點，之後每累積 4 週達標再 +1 點；累計模式達標週數只增不減，連續模式連漏超過 `grace` 週這一輪累積的週數歸零（已賺到的點不收回）。週中才約定的那週沒達標不算漏（`skip`），每天型那週也不給分；約定日之前的打卡不算。
- **HAB-4** — 點數只增不減：`wallet.ledger` 記每個習慣賺到過的最高點數，每次重算取 max(舊, 新)，習慣被刪或封存、規則改嚴都不收回；`wallet.earned`＝ledger 合計＋待辦點數換到的 `task_wallet`＋起手禮（HAB-20，送過就 1 點）；`wallet.points = max(earned − spent, 0)`，`spent` 讀 `closet.unlock`（每筆花 `cost`，沒寫就是 1 點；沒有 closet 套件就是 0）。
- **HAB-5** — 打卡是 `habits.checkin` 一筆（頁面寫 `source = manual`、`key = manual:<習慣>:<日期>:<子項目>`）；同一習慣同一個 `key` 在有效期間只算一次；撤銷是再寫一筆 `undo = true`（取消同一習慣、同一天、同一子項目最後一筆還有效的打卡，依編號先後處理），舊資料不改。
- **HAB-6** — 自動來源即時推算、不寫打卡：`fitness:<種類>`＝那天有那種 `fitness.workout`（欄位 `type`，`*`＝任何，`有氧`＝跑步、划船、游泳、騎車任一）且 `status = 完成`，每筆運動算一次；`reminder:R-<n>`＝那筆待辦 `status = done` 的 `closed_at` 那天（本機時區）；子項目 `body:weight`＝那天有 `body.entry` 而且有 `weight`；`nutrition:meals>=N`＝那天 `nutrition.meal` 至少 N 筆。讀不到某個種類（套件沒裝）就當沒有，不報錯。
- **HAB-7** — 子項目：當天做到的項數 ≥ 全部 − `allow_miss`（至少 1 項）才算當天打卡，之後走一樣的每天或每週規則；格式「名稱」「名稱=body:weight」「名稱=nutrition:meals>=N」。
- **HAB-8** — 行程：開機先算一次，之後 `vault.watch` 種類 `habits.habit`、`habits.checkin`、`habits.exchange`、`fitness.workout`、`body.entry`、`nutrition.meal`、`closet.unlock`、`closet.state`、`closet.outfit`（後兩個：衣櫃開關、衣服上下架會改錢包的 `outfits_ready`，HAB-23）、`reminder`（`internal/run` 的 `WatchedKinds`），有變就去抖 2 秒重算；另外每天本機時間 00:05 重算一次（週次推進）；訂閱斷線 3 秒後重連並補算一次。`status`、`wallet` 只有內容變了才寫（`computed_at` 不算內容）。
- **HAB-9** — 新賺到點（ledger 合計變多）時用 `notify.send` 發一則（分類 `done`、標題「賺到 N 點」、內文「<習慣名> 達標累積 N 週，現在有 P 點可以換衣服」、agent＝習慣的 `agent`、通知上一顆站內按鈕「到展示間」（`link`＝`/p/closet/showroom`，點數在那裡換衣服；2026-11-15 DEV-157 新家第一天走查②：以前寫在 `url`，看板不畫，改成 `NotifySendRequest.Link`，inbox NTF-66）；**點數還換不到衣服的家**（錢包的 `outfits_ready` 是 false，HAB-23：衣櫃沒在用、或還沒有上架要點數的衣服）按鈕改成「到習慣頁」（`/p/habits/list`）、內文只說「現在有 P 點」，不提換衣服（起手禮 HAB-20 同一個判斷）。測試 `TestNotifyLinksHabitsWhenNoOutfits`、e2e `TestPointNotificationLinksHabitsWhenClosetOff`）；先寫錢包再通知，同一個 (習慣, 點數) 只發一次（以 ledger 為準，重開行程不重發）。
- **HAB-10** — 頁面（`list`、`detail`、`today`）**不算點數**，只讀 `status`、`wallet`；打卡後約 2 秒程式才算完，頁面每 3 秒重新讀一次；這段空檔清單頁先把看得到的進度改掉（`pages/lib/habits.ts` 的 `withPendingMark`：今天做了沒、本週幾次、子項目、能不能撤銷；點數與連續週數不猜），程式重算好（`computedAt` 變了）就換回真的，小字「記下了」2 秒就收，不留「正在更新進度…」（DEV-156 U-20）。存檔前用 `pages/lib/habits.ts` 的 `validateHabit` 檢查，規則跟 `internal/rules` 的 `Habit.Validate` 一致，錯誤用白話。
- **HAB-11** — 套件不提供秘書樣板（習慣是跟各位秘書約的，不屬於某一位）；給秘書的完整用法寫在 `packages/official/habits/README.md`：約定＝`evergenie data add habits.habit`、打卡＝`data add habits.checkin`、看狀態＝`data get habits.status h-<n>`、錢包＝`data get habits.wallet main`。每位秘書的指令另外帶一段短的（2026-11-14 DEV-157：`[[extension]] guide`，`prompt_append`、不指定職位，接進「套件補充」，PKG-4；套件停用就不帶）：怎麼約（從兩分鐘的小版本開始、綁在已經會做的事後面、一句身分認同）、怎麼記（兩個指令，其他讀 README：下載版沒有 repo，指「`evergenie docs` 第一行的程式資料夾裡的 `packages/official/habits/README.md`」，官方套件跟著發行檔帶，DOC-12 不擋）、點數怎麼來（含起手禮；提不提「可以到展示間換衣服」先看錢包的 `outfits_ready`，HAB-23，不是 true 就只說點數先存著、之後有衣服可以換時用得到）、不念不催（沒做到不提醒、不責備、不主動問進度）、戒壞習慣與回顧沒有專門頁面，只在主人提起時用聊的帶。
- **HAB-12** — 今天頁的習慣卡：習慣或打卡狀態讀不到時整張卡換成共用的 `LoadFailed`（「今天的習慣暫時載不進來」＋「再試一次」，只重讀、不跳頁；英文原因只寫 console，DEV-156 U-06），不能一直轉圈；錢包讀不到只是不顯示點數。（2026-11-03，vitest `TestTodayCardShowsErrorWhenStatusFails`）
- **HAB-13** — 待辦點數（2026-11-07 DEV-136）：程式每次重算讀全部 `reminder`（含封存）的 `done_points` 加總，寫 `wallet.task_points`（取 max，只增不減）；換成錢包點改成主人按兌換（HAB-18）。不推播（法律第 5 條）。跟主人自己做的點數套件無關。
- **HAB-14** — 月曆打卡與每週跳過上限（2026-11-07 DEV-135）：每天型同一週沒做超過 `week_skips` 天（預設 2）那週算沒達標，2026-11-09 之前的週不算，連續漏超過 `miss_days` 的規則照舊（連跳一天不斷、連兩天斷）。`status` 帶 `done_days`、`skipped_days`、`broken_days`、`manual_days`（最近 400 天），詳情頁月曆只畫、不算；點格子打卡（無子項目的 checkin）、再點撤銷；自動來源的、未來、開始前、結束後不能點。測試 `TestWeekSkips`、`TestDayMarks`。
- **HAB-15** — 今天只有一件事（2026-11-07 DEV-135）：今天頁的習慣卡（排在套件卡片那一區的最前面；桌機那一區在輸入框下面，手機排在今天的待辦下面，見 board WEB-177）只顯示一個習慣（`pickOneThing`）：快連兩次漏的（每天型昨天沒做）先；今天還沒做過才挑一般的；都沒有就「今天這樣就夠了」。名字＋兩分鐘版本＋提示，一鍵「做了」。不新增提醒推播。卡片標題固定「今天的習慣」；「今天只有一件事」「今天這樣就夠了。」是內文（DEV-156 U-13）。
- **HAB-16** — 原子習慣四原則（2026-11-07，寫在套件 README 與 HAB-11 的補充；不放核心的共用規則，法律第 3 條）：很小（兩分鐘內）、很明顯（綁在一件已經會做的事後面）、不連兩次漏（漏一次沒關係）、不念（沒做不催、不講道理）。
- **HAB-17** — 清單卡片的本週七格（2026-11-07 DEV-135 補）：習慣清單每張卡在進度條下面畫本週週一到週日七格（`weekCells`，跟月曆同一套長相 `lib/dayLook.ts`：做了綠底 ✓、斷了淡紅 ✕、跳過「－」第一次淡灰、同一週第二次起淡紫虛線框（`skipNo`；不用橘，橘只給等你處理）、還沒做空白、今天加框；2026-11-08 作者改色），只看不點；打卡鈕還沒打是實心主色（子項目用淡紫底），打過變淡綠底「已打卡 ✓」，不能跟白色格子混在一起；名字後面加「›」表示點進去有月曆可以補打、撤銷。
- **HAB-18** — 待辦點數手動兌換（2026-11-07 作者定）：`task_points` 是總數，完成待辦就往上加、可以超過一份；待辦頁最上面（slot `core.reminders.top`，頁面 `todo-points`）顯示累積到哪（DEV-156 U-14：進度條＋一句「再累積 N 就多 1 點」，完整規則收在點開的「怎麼算」；畫面上只有一種「點數」，待辦攢的叫「累積」不叫點），還沒換的滿一份（現在 30，HAB-19）出現「換成 1 點」，按一次寫一筆 `habits.exchange`，程式照筆數用一份點數換 1 個錢包點（`task_exchanged`，只增不減，不夠的那次不換、兌換紀錄被刪也不收回）。舊版自動換過的記在 `task_legacy`，升級時從舊帳算一次。測試 `TestTaskPointsToWallet`。
- **HAB-19** — 兌換改 30 點（2026-11-12 DEV-149 作者定）：一份從 20 改 30（`TaskPointsPerWalletPoint`、頁面 `TASK_POINTS`）。改價前按的兌換照 20：升級後第一次重算把當時的兌換筆數記在 `wallet.task_old_requests`，前面這幾筆 20、之後 30，照順序換；換到的錢包點另記 `task_wallet`（只增不減，舊錢包第一次從 `task_exchanged ÷ 20` 算），不再用 `task_exchanged` 除回去，已經換到的不會變少。測試 `TestTaskExchangePriceChange`。
- **HAB-20** — 起手禮（2026-11-14 DEV-157，作者定）：家裡第一個習慣約好送 1 點，每個家只一次。錢包的 `starter` 記走到哪一步：`waiting`（還沒約過，第一個沒封存、設定合格的習慣出現就送）→ `given`（送過，`starter_habit` 記是哪個習慣；之後結束、刪掉、重約都不再送，算進 `earned` 永遠不收回）；`none`＝起手禮上線前就有過習慣的家，不補送。分辨新舊：錢包不在＝新家；錢包在卻沒有 `starter`＝升級前的錢包，以前有過習慣（含封存的習慣、點數帳、打卡）就 `none`，沒有就照新家等第一個（升級前沒約過習慣的家約第一個時一樣送）。剛送出時通知一次（分類 `done`、標題「送你 1 點」、agent＝那個習慣的 `agent`）：點數換得到衣服（`outfits_ready`，HAB-23）時內文「約好第一個習慣「<名稱>」，送你 1 點，可以到展示間換一套。」、按鈕「到展示間」；換不到時內文「約好第一個習慣「<名稱>」，送你 1 點，之後有衣服可以換時用得到。」、按鈕「到習慣頁」（HAB-9 同一套按鈕；DEV-157 新家第一天走查①：以前只看衣櫃套件開沒開，沒有衣服包的新家也說能換）；先寫錢包再通知，錢包寫成 `given` 之後不會再送。測試 `TestStarterOncePerHome`、`TestStarterGiftOnce`、`TestStarterAndWelcomeSkipOldHomes`、`TestStarterWalletWriteFailureRetries`。
- **HAB-21** — 第一次介紹（2026-11-14 DEV-157，作者定「秘書第一次開場用她的口吻帶一次」）：套件宣告第二段補充 `[[extension]] welcome`（`prompt_append`、不指定職位、`until_record = "welcome/main"`，PKG-4），內容叫秘書在主人本人跟她說話時照常回完，再 `evergenie data add habits.welcome --id main --json '{"status":"done","agent":"<職位>"}'`：寫成功才用自己的口吻補一兩句（可以約一個很小的習慣、約好送 1 點、做到有點數；錢包的 `outfits_ready` 是 true 才加「點數可以到展示間換衣服」，HAB-23），寫不成（別的秘書講過＝已經有資料、或出錯）什麼都不提；只講一次，主人沒接話也不再提。`habits.welcome main` 一出現（含封存的），核心之後開的對話就不帶這段（已經開著的對話靠「寫不成就不提」擋第二次）。現有的家不會被介紹：程式每次重算時，還沒有這筆紀錄而且（錢包在卻沒有 `starter`＝升級前就在用，或已經有習慣）就寫 `{"status":"skipped"}`；要在寫錢包之前寫好，寫不成這一輪就不寫錢包、下次再試（錢包一寫上 `starter` 就分不出新舊）。核心不知道「習慣」：只照 `until_record` 看那筆紀錄在不在。測試 `TestStarterAndWelcomeSkipOldHomes`、`TestWelcomeWriteFailureKeepsOldWallet`、`TestHabitsGuideReachesSecretaries`（真 serve：新家兩段都有、寫了紀錄就只剩 guide、第二位秘書寫被擋、停用都不帶、升級前的家沒有介紹），核心 `TestPromptAppendUntilRecord`。
- **HAB-22** — 第一天的入口（2026-11-14 DEV-157）：今天頁的習慣卡沒有進行中的習慣時，除了「還沒有約定習慣」再放「約一個新習慣」，跟習慣頁用同一個元件（`pages/lib/parts.tsx` 的 `NewHabit`：選一位秘書、聊天起好頭「我想約一個新習慣：」、打開聊天；名單還沒讀到寫「載入中…」，讀到了卻一位秘書都沒有就寫「還沒有秘書」並連到 `/settings/agents`；**只有一位秘書時不問跟誰**（`pages/lib/habits.ts` 的 `soleAgent`，DEV-157 新家第一天走查④）：按鈕直接是連到 `/chat/<她的職位>` 的連結、按下去先起好頭再換頁；說明文字不寫「她」，寫「選一位秘書，聊天會打開並幫你起好頭」）；習慣頁的錢包卡放連到 `/p/closet/showroom` 的連結，衣櫃套件沒開或被拒就不放（`loadClosetAvailable`）；字看點數真的換不換得到衣服（錢包的 `outfits_ready`，HAB-23；頁面不自己判斷——2026-11-15 起原本頁面的 `loadOutfitsReady` 拿掉，跟通知、秘書補充用同一份），換得到寫「到展示間換衣服」、還換不到寫「到展示間看看」（展示間會說衣服包怎麼裝，closet CLO-25）；今天頁「還沒有約定習慣」那句同樣，換不到時不寫「會有點數換衣服」、只說「做到就有點數」（DEV-156 U-09：沒有衣服包時不承諾換衣服）。e2e「今天頁還沒有習慣時按得到「約一個新習慣」」「習慣頁的錢包卡點得到展示間」「只有一位秘書時「約一個新習慣」直接開她的聊天」。還沒有進行中的習慣時卡片宣告自己是空的（`EmptyLine`，board WEB-177）：手機今天頁縮成一行「今天的習慣：還沒約　約一個」，「約一個」開同一個「跟誰約？」抽屜（`NewHabitSheet`）；只有一位秘書時「約一個」直接連到她的聊天、先起好頭（`soleAgent`，同上；所以這張卡空的時候就讀秘書名單）；桌機照舊整張卡。
- **HAB-23** — 點數換不換得到衣服只有一個判斷（2026-11-15 DEV-157 新家第一天走查①）：`internal/run` 的 `outfitsReady`——衣櫃在用（讀得到沒封存的 `closet.state/main`，衣櫃的程式每輪都寫它；套件停用時這個種類沒註冊，`closetOn`），而且這個家有上架、要點數的衣服（`closet.outfit`：沒封存、`state = active`、`cost > 0`；官方衣服包裝好的、主人自己加、設了點數的都算，closet CLO-2）。只看衣服本身，不看角色包狀態：這一版有帶衣服包不等於已經裝好（下載中、上次沒成功時展示間還是空的，CLO-25）；讀不到當沒有，寧可少說。程式每次重算把結果寫進錢包的 `outfits_ready`（`schemas/wallet.toml`），`closet.state`、`closet.outfit` 有變就重算（HAB-8 的訂閱種類）；用它的地方：賺到點與起手禮的通知（HAB-9、HAB-20）、給秘書的兩段補充（`guide`、`welcome`，叫秘書先 `data get habits.wallet main` 看這欄，HAB-11、HAB-21）、習慣頁的錢包卡與今天頁「還沒有約定習慣」那句（HAB-22）。衣櫃剛停用時錢包要等下一次重算才變 false（每天的重算或任何一筆習慣資料變動）；這段時間習慣頁的展示間連結照 `loadClosetAvailable` 收掉。測試 `TestNotifyLinksHabitsWhenNoOutfits`（衣櫃停用、沒有衣服、只有免費的／草稿／下架／封存的）、`TestOutfitsReadyFollowsCloset`；vitest `habits.spec.ts` 的 `toWallet`。
- **HAB-24** — 習慣卡的點數只寫下一步（2026-11-15 DEV-156 U-14）：卡片「點數」那一行是「再達標 N 週就拿到 1 點」（N＝status 的 `next_need_weeks`，0 時「這週達標就拿到 1 點」，`pointLine`），詳情頁同一句；完整規則（累計 2 週、之後每 4 週、連續加分、點數不會少）收在卡片右上「怎麼算」（以前叫「規則」）。規則本身不變。測試 `habits.spec.ts`「點數那一行只寫下一步」、e2e `TestHabitCheckinUpdatesCard`。

## NEVER
- **HAB-N1** — 不 import `internal/`，只用 `sdk`（PKC-N1）；寫 vault 一律經 `VaultAPI`（PKC-N3）；通知一律經 `notify`（PKC-N4）。
- **HAB-N2** — 不匯入舊家的習慣資料（D-27，搬家當天另轉）。
- **HAB-N3** — 錯誤不中斷行程：一筆設定不合格的習慣記 log（stderr，核心收進 `runtime/packages/habits/out.log`）後略過，別的習慣照算。

## 職責
- 六種資料的欄位規格（`schemas/*.toml`）、規則引擎（`internal/rules`）、vault 讀寫與通知（`internal/run`）、行程入口（`cmd/evergenie-habits`）、四個頁面（`pages/list.tsx`、`detail.tsx`、`today.tsx`、`todo-points.tsx`）與純函式（`pages/lib/habits.ts`）、給秘書的兩段補充（`package.toml` 的 `[[extension]]`，HAB-11、HAB-21）。

## 不負責
- 衣櫃的兌換、輪播、剪影 → `closet`（P6-4b；它把花掉的點數寫進 `closet.unlock`，這裡只讀）。
- 運動紀錄、身體數據 → `fitness`；飲食 → `nutrition`；待辦 → `reminders`（這裡只讀它們當自動來源）。
- 欄位檢查、編號、封存 → `vault`；套件探索、行程監督 → `pkg`。

## 介面
資料種類（`[[contributes.data]]`，完整 vault 種類名稱 `habits.<種類>`；欄位見 `schemas/habit.md`）：

| 種類 | 編號 | 誰寫 |
|---|---|---|
| `habit` | `seq`，`H-` | 秘書（約定時）、頁面（修改約定） |
| `checkin` | `seq`，`C-` | 頁面與秘書 |
| `status` | `given`，習慣編號小寫（`h-3`） | 只有套件的程式 |
| `wallet` | `given`，`main` | 只有套件的程式 |
| `exchange` | `seq`，`X-` | 頁面（待辦頁按「換成 1 點」，HAB-18） |
| `welcome` | `given`，`main` | 介紹過的秘書（`done`）、套件的程式（`skipped`，HAB-21） |

頁面：`list`（選單「習慣」、圖示 `calendar-check`、`group_hint = "daily"`、`front = true`、`order = 5`，網址 `/p/habits/list`）、`detail`（`/p/habits/detail?id=H-<n>`，不進選單）、`today`（slot `core.overview.cards`）、`todo-points`（slot `core.reminders.top`，HAB-18）。

擴充點（`[[extension]]`，PKG-4）：`guide`（給每位秘書的習慣用法，HAB-11）、`welcome`（新家第一次介紹，`until_record = "welcome/main"`，HAB-21），都是 `prompt_append`。 `list` 另宣告 `chat = true`（寬螢幕旁邊放聊天；習慣各自跟不同秘書約，所以不給預設，主人選一位）（REQ-235）。

行程：`evergenie-habits`（`program.entry = "cmd/evergenie-habits"`、`restart = "always"`）；官方行程跟著發版附在核心執行檔旁（`bin/evergenie-habits`，`make build` 產生），找不到才用 `go build`。

## 事件
- 發出：經 `notify.send` 的通知（HAB-9 賺到點、HAB-20 起手禮）。
- 訂閱：`vault.watch`（HAB-8）。

## 儲存
資料在 `vault/pkg/habits/{habit,checkin,status,wallet,exchange,welcome}/<編號>.md`（YAML 標頭、內文自由 Markdown；習慣的內文是約定時聊出來的補充）。`runtime/packages/habits/out.log` 是行程的 log。

## 依賴
`sdk`

## 錯誤
- 欄位不符（缺必填、種類不在列舉、型別錯）→ vault 回 `INVALID` 並說明哪個欄位；頁面把原因顯示在欄位旁，`evergenie data` 印在標準錯誤。每週型沒寫 `target`、`target` 不在 1–7、`miss_days`／`grace` 不在 0–6、子項目格式錯等 vault 看不出來的規則，由頁面存檔前與程式算的時候檢查：頁面擋下並說白話，程式略過那個習慣並寫 log。
- 讀 `habits.habit` 或 `habits.checkin` 失敗 → 這一輪不寫任何東西、記 log，下一次變更或每日重算再試；讀自動來源的種類失敗 → 當作沒有。
- 錢包寫不成 → 不通知（下次重算會再發現新點數；起手禮也一樣，`starter` 還不是 `given` 就下次再送）。
- 「不用介紹」記不下來、或升級前的錢包讀不到全部習慣（含封存）→ 這一輪不寫錢包、記 log，下次再試（HAB-20、HAB-21）。
- 套件停用時六種資料暫時看不到；重新啟用資料回來（REQ-173）。

## 不變條件
- 原子習慣：約定、打卡、自動來源、子項目、累計或連續模式、點數錢包。

## 測試計畫
- Go（`packages/official/habits/internal/rules`，`make ci` 的 `make test` 會跑）：`TestWeeklyTargetAccumulates`、`TestDailyMissDays`、`TestMidWeekStartNotPenalized`、`TestStreakResetKeepsPoints`、`TestPointsOnlyGrow`、`TestSubtasksAllowMiss`、`TestAutoSourceCheckin`（fitness、reminder、body、nutrition 四種）、`TestUndoCheckin`、`TestHabitFromFields`（REQ-124）。
- Go（`internal/rules`）：`TestStarterOncePerHome`（HAB-20）。
- Go（`internal/run`，假的 vault 與 notify）：`TestNewPointNotifiesOnce`、`TestHabitsAutoSourcesFromVault`、`TestHabitsRunRecomputesOnChange`、`TestNextDailyIsFiveAfterMidnight`（REQ-124）；`TestStarterGiftOnce`、`TestStarterAndWelcomeSkipOldHomes`、`TestWelcomeWriteFailureKeepsOldWallet`、`TestStarterWalletWriteFailureRetries`（HAB-20、HAB-21）。
- Go（`cmd/evergenie`）：`TestHabitsPackageRunsOnFixtureHome`（REQ-124：真 serve＋fixture 家，官方 habits 被編譯好的行程啟動、報到、`running`；`data add` 種習慣與兩週打卡 → `status.earned_points = 1`、`wallet` 2 點（含起手禮）、賺到點與起手禮各通知一則——這個測試家沒裝衣櫃套件，兩則都連回習慣頁、不提展示間（HAB-9；衣櫃開著連展示間由 e2e `closet-habits.spec.ts` 的 `TestFirstHabitGiftRedeemsOutfit` 驗）；打卡與撤銷後本週進度跟著變）、`TestHabitsGuideReachesSecretaries`（HAB-11、HAB-21）。
- 純函式（vitest，`pages/lib/habits.spec.ts`）：`TestHabitInputValidation`、`TestSubtaskRules`、`TestHabitRecordsAndFields`、`TestStatusAndProgressText`、`TestNotDoneCount`（REQ-124）。
- e2e（`habits.spec.ts`，mobile／desktop，真的行程在跑）：`TestHabitCheckinUpdatesCard`（REQ-124：打卡後卡片的本週進度變了、撤銷、子項目）、`TestHabitEditValidation`（REQ-124）、`TestHabitsTodayCard`（REQ-114、124）、「今天頁還沒有習慣時按得到「約一個新習慣」」「習慣頁的錢包卡點得到展示間」「只有一位秘書時「約一個新習慣」直接開她的聊天」（HAB-22）。截圖：`TestShotsHabits`、`TestShotsHabitDetail`（REQ-115）。

## 已知限制
- 舊資料搬家當天另轉（D-27），這個套件不匯入。
- 打卡到頁面進度更新有約 2～5 秒延遲（程式去抖 2 秒加頁面 3 秒輪詢）；程式沒在跑時 `status` 不會更新，頁面顯示最後一次算的結果。
- `reminder:R-<n>` 用待辦的 `closed_at`；週期性待辦完成後會滾到下一次，`closed_at` 只留最近一次，更早的完成日不會被算到。
- 打卡去重鍵是「日期＋子項目」，所以同一個習慣同一天頁面只能打一次卡；要同一天算多次（每週型）請用自動來源或讓秘書寫不同的 `key`。
- `closet.unlock` 的欄位見 `schemas/closet.md`：一筆＝花 `cost` 點（沒寫或不是正整數就當 1 點）；closet 的程式只認 `by` 是 `closet` 或 `migrate` 的兌換紀錄，別的來源寫的會被封存，所以這裡讀到的都是真的。
- 已刪除（封存）習慣的 `status` 記錄留在原地不更新；錢包的 ledger 保留它賺到的點。
- 時區用行程的本機時區（`time.Local`），不是 `home.toml` 的設定。
- 起手禮與第一次介紹分辨新舊家只看錢包（HAB-20、HAB-21）：一直停用習慣套件、從來沒有錢包的舊家，之後啟用會被當成新家（介紹一次、第一個習慣送 1 點）。退回到沒有起手禮的舊版程式會重寫錢包、拿掉 `starter` 與那 1 點；再升級回來時被當成升級前就有習慣的家，不會重送。
- 第一次介紹靠秘書照補充做（寫紀錄、寫成功才講）：是給 AI 的指示，不是程式擋的；已經開著的對話要等寫成功才講，所以兩位秘書同時第一次被搭話時最多一位講。
- 舊家升級當天有一段空窗：常駐秘書的對話開機就開，可能早於習慣程式第一次重算（記 `skipped`），那些對話到夜間重開前都帶著「第一次介紹」那段；擋住重講的是「寫成功才講」——程式記好之後寫一定不成，秘書第一次被搭話時多跑一次失敗的 `data add`、不會開口。程式還沒記好之前就被搭話的那一次，舊家也可能被介紹一次（程式沒在跑、或開機到第一次重算那幾秒）。
- 賺到點、起手禮的通知連哪裡，看發通知那一刻衣櫃的 `closet.state/main` 讀不讀得到（HAB-9）：衣櫃剛啟用、它的程式還沒寫第一筆 state 的那幾秒內發的通知會連回習慣頁；發出去的通知不會回頭改連結（之後才停用衣櫃，舊通知仍連展示間）。
