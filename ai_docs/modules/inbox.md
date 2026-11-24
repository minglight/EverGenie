# 模組：inbox（`internal/feature/inbox`）
Status: DRAFT — 2026-11-02 S8-3 合併 notify.md、overview.md、menu.md、push.md，pending author review.

**目的：** 主人收到什麼、從哪裡看：給主人的通知（分類、偏好、摘要、推播、按鈕回答、不吵的提醒，D-14、D-34）、Web Push 傳送通道、看板首頁「今天」的彙整、看板的選單與頁面清單。
**種類：** 業務塊（7 塊之一；子套件 `inbox/notify`（原 notify）、`inbox/push`（原底層 push，只給 notify 用的函式庫）、`inbox/overview`、`inbox/menu`）

## MUST
### notify（通知）
- **NTF-1** — 每則通知帶一個分類：`approve`（要你同意）、`done`、`due`、`digest`、`opening`；沒分類的呼叫被拒絕。
- **NTF-2** — 每個分類可選送法 `instant`、`digest`（每 N 小時合併）、`board`（只放看板）、`off`；偏好在 `settings/notify.toml`，可針對單一秘書覆寫。
- **NTF-3** — 簡單模式只選預設組（`quiet`／`standard`／`active`）；完整模式逐類調，另可對單一秘書另設（`[agents.<職位 id>.categories.<分類>]`）。`SetPrefs` 的 `[categories.*]` 只記跟預設組不一樣的（選預設組＝沒有逐類設定，舊的逐類與單一秘書另設一併清掉）；帶 `expand: true`（`SetPrefsExpand`，簡單切完整時用）五類都明確寫出（展開由核心做，`schemas/notify-prefs.md`）。回應附 `presets`（三組預設，跟核心同一份表）。
- **NTF-3a** — 偏好也帶任務單跟進四欄（`NotifyPrefs.tasks`，`[tasks]`）：`SetPrefs` 收到才寫並驗範圍，沒帶原樣保留。
- **NTF-4** — `approve` 的送法最低只到 `board`（含單一秘書的覆寫），設成 `off` 在寫入時被拒絕。
- **NTF-5** — 呼叫者只傳分類；送不送、何時、哪個管道由 notify 依偏好決定，沒有繞過的參數（`urgent` 只影響安靜時段）。
- **NTF-6** — 通知同時進看板訊息列表（`M-<n>`）與 App 推播；LINE 依設定。例外：提醒（NTF-33）與帶 `key` 的對話等待（NTF-50）只推手機、不建 `M-<n>`。
- **NTF-7** — 通知文字固定 `[職位｜機器｜帳號] 類型｜標題`＋`背景：…`＋`請你：…`；標籤由 notify 帶入，呼叫者不能改格式。
- **NTF-62** — 通知文字（NTF-7）空的部分不寫：沒有帳號就是 `[code] 類型｜標題`（不寫成 `[code｜]`），背景沒內容就不寫那一行（`noticeBody`；請你在 `send` 時已補成「無」，所以新通知一定有那行，`noticeBody` 本身空的也不寫）。開頭寫職位 id 不寫秘書的名字：notify 拿不到名冊，看板顯示時另外換成頭像與名字。（2026-11-03，`TestMessageFormatSkipsEmptyParts`）
- **NTF-63** — 看板「要你回答」的卡片在通知沒有背景、請你時退回用 body，但拿掉 notify 組的第一行（`[職位｜帳號] 類型｜標題`，跟卡片的標題、頭像重複；舊的存檔還是 `[code｜]`）與空的「背景：」「請你：」（`bodyDetail`）。（2026-11-03，vitest `TestWaitingDetailDropsNoticeHeader`）
- **NTF-8** — `approve` 一定有 `todo`、可以帶選項；回答（按鈕或打字）後發 `notice.answered`，由訂閱者送回發問的 session 或執行動作。
- **NTF-9** — 通知可附頭像圖（`image`）與看板連結（`url`）。
- **NTF-10** — 安靜時段（`platform.Quiet`）只寫看板、不推手機；`urgent` 例外。
- **NTF-11** — 回傳未讀數；事情在別處處理掉就標已回：訂閱 `reminder.changed`／`task.changed`，看事件帶的 `from` 判斷離開了待回答狀態（不讀 `task`／`reminder` 資料、不在記憶體記狀態）；任務結束（done／cancelled）不管從哪來都收，並每分鐘經 `SetTasks` 接上的 tasks `Get` 照現在狀態補收（事件漏掉、重啟、搬來的單也收；單檔被直接刪掉、`Get` 連兩輪回找不到也當結束收掉（只一輪不收，防外部工具先刪後寫），2026-11-03 DEV-74／DEV-86 的訊息因此一直掛著、看板刪不掉）。反方向（答完最後一則，任務回到進行中）由 tasks 做，經 notify 的 `List` 問還有沒有別則在等。
- **NTF-12** — `Share` 把檔案複製到 `runtime/share/` 回傳連結；API `notify.share`（CLI `evergenie share` 用，看板不開）；安全限制見不變條件。
- **NTF-13** — `approve` 的 `url` 預設開到那一則訊息（`/messages/<id>`），落地就看得到選項；不准預設成沒按鈕的頁面。
- **NTF-14** — 打字回答與按鈕等價：`Answer(via: chat)` 一樣標已回、發 `notice.answered`；答案還沒送進發問的 session（`delivered_at` 空）時每 30 秒重送、最多 2 小時，之後標 `failed` 並再通知主人重答；發問的秘書查得到「還沒收到回覆」。
- **NTF-15** — 推播裝置由主人在看板管：`PushKey` 回公開金鑰與訂閱數（金鑰沒設回 `unavailable`）；`SubscribePush`、`UnsubscribePush`、`PushTest` 的 `by` 必須是 `owner(…)`，否則 `forbidden`；檔案、格式與金鑰在 push（PSH-4、PSH-5），這裡只管誰能管。`PushTest` 只送給同 endpoint 那一台，內容是不存檔的測試通知（編號 `test`、分類 `done`），不進看板、不發事件。
- **NTF-16** — 沙盒家（`[runtime] sandbox = true`，舊寫法 `dev = true`，D-35）永遠不推播：`Send` 不論偏好、`urgent` 只寫看板，`PushTest` 回 `unavailable`。只寫 `env = "dev"` 的開發家照常推（NTF-52）。
- **NTF-30** — 通知屬於本家，標籤 [職位｜帳號]，不注入機器來源；`site` 欄位已拿掉（法律第 7 條：每台一個家、家與家不連線），舊檔帶著照樣讀、回得了（`TestMigratedNoticeWithoutDeliveryCanBeAnswered`）。
- **NTF-31** — 已廢：兩家不連線（法律第 7 條）。
- **NTF-32** — 討論串內提問（`inline: true`，D-33）只進看板與 `room` 的討論串、不推播（不論偏好、安靜時段、`urgent`），投遞紀錄只有 `board`。只會更安靜，不能用來繞過偏好。
- **NTF-33** — 提醒 `Nudge`（D-34）：工作 session 在 `waiting_user` 第 50 分鐘的「還在等你」，由 session 經 `Deps.Nudge` 呼叫。提醒不是通知：不建 `M-<n>`、不進列表、不發 `notice.sent`，只在記憶體；內容有單號、標題、要主人做什麼，`url` 預設該 session 的聊天頁（只收站內路徑）。真的顯示才算一次：任務單 `nudges`＋1（經 `TaskNudges`，TSK-17）。同 session 已有沒收掉的提醒時回既有那則，不重複計數。
- **NTF-34** — 提醒的推播條件跟一般通知一樣由 notify 把關（沒有 `urgent`）：沙盒家、`channel_app` 關、`approve` 對該秘書的送法不是 `instant`、安靜時段、沒有訂閱，一律不推，回 `surface: none`、不計次數。
- **NTF-35** — 主人在看哪裡：看板在前景時每 30 秒回報 `Presence{client, path, thread, away}`（`notify.presence`，只有主人；`client` 是分頁編號，上限 64 筆）；只存記憶體，90 秒（`PresenceTTL`）沒更新過期、`away` 馬上清。`Nudge` 依在場決定：有分頁在那個 session 的討論串（`thread` 或 `/chat/<session>`）或那張單（`/tasks/<單號>`）→ `inline`；看板還開著 → `toast`；都沒有 → `push`。安靜時段內 `inline`、`toast` 照出。`Presence` 回目前要顯示的提醒。
- **NTF-36** — 升級與收掉：頁內卡片滿 2 分鐘（`InlineEscalateAfter`）沒回、或主人離開那頁 → 改推播（遵守 NTF-34）；站內提示只有看板整個沒人看才改推播；提醒最長 15 分鐘（`NudgeLifetime`）。任一處回答其他地方一起收：主人按「知道了」（`notify.resolve_nudge`）、單離開 `waiting_user`（`task.changed`）、主人在那個 session 回話（`bus.envelope.state`：`queued`、`kind: chat`、`from` 是 `owner(…)`、`to` 是該 session）；收掉時發 `notice.nudge.resolved`、經 `Watch` 通知看板；不補推「已處理」（法律第 5 條白名單沒有這種，手機上跳出來又看不出是哪件事，作者 2026-11-04，DEV-123），手機上那則等看板打開或回到前景時收（WEB-36 ⑥），在那之前留著，點了還是開那個對話。測試 `TestAnswerRetractsEverywhere`。
- **NTF-40** — 主人刪訊息：`Archive(ids, by)` 只有 `owner(…)`（否則 `forbidden`）；刪除＝封存（`vault.ArchiveNow`，不刪檔）；找不到或已封存的略過；封存後 `Watch` 送新的 `unread`。看板代理 `notify.archive`（`by` 換成 `owner(web)`）。
- **NTF-41** — 主人說刪就刪：`Archive` 不略過還沒回答的「要你同意」（系統提醒、秘書、Goal、權限申請的都封存）；`skipped` 只為 API 相容，一律空的。Goal、權限申請本身的狀態不跟著改。
- **NTF-42** — `save` 一律寫回 `delivery`（沒有就是空清單），舊系統搬來的通知 `delivery` 是空的也能回答、已讀。
- **NTF-43** — 系統提醒不洗版：收到 `system.alert` 時，同標題的系統提醒還沒處理（沒回答、沒封存）就不發新的。引擎回報的那兩種（NTF-54）標題是固定的，去重另外看通知的 `account`：同標題、同帳號還開著才不發（`openAlert`）。
- **NTF-44** — `Deps.Push` 沒接時啟動失敗，不當成沒有訂閱（`TestStartFailsWithoutPushChannel`）。
- **NTF-48** — 業務塊自己處理的通知：`source` 只由擁有它的模組在 `Send` 時標（`goals`、`policy`、`package`），算不算看各業務塊登記的通知來源（`app.NoticeSourceProvider`，APP-11；app 組裝時經 `ConsumeNoticeSources` 交進來），沒登記的 `source` 清掉；這類回答不追蹤送達、不轉進秘書的對話。`notice.answered` 帶 `relay`（`relaysAnswer`：不是 auto、不是登記的來源、不是 `system.alert`，而且綁了對話、或不是任務／待辦的通知、或是只收打字回答的任務問句〔`typed`，TSK-27：tasks 只把單送回進行中、不轉話，回答的內容要讓秘書讀到，D-57；DEV-156 U-05；等不等她讀到照舊看 NTF-59，這種不追蹤〕），session 只照它決定要不要轉回發問的對話；`claim`（職位 id）只在 `source = package` 時留著；payload 帶回答的人 `by`，擁有者用它只認主人的回答。測試 `TestRegisteredSourcesDecideOwnershipAndRelay`。
- **NTF-49** — 通知跟狀態只有一份正本：綁的那件事不在了就 `Retire`（記回答 `auto`「已經不用回了」＝`RetireReason`、發 `notice.answered`（via auto）、封存；已回答、封存、找不到的略過）。走事件、3 秒內：登記的來源發它的 `Changed`（`goal.changed`、`policy.changed`）→ 問那個來源的 `Waiting`，有人指到但不在等了就收（`retireSettled`；不讀 `goal`／`policy_request` 資料）。安全網每 `SweepEvery`（3 秒，`Tick`）對一次：指到的事不在等、或沒人指到且發出超過 1 分鐘就收（讀不到這次不判斷）；系統提醒與一般通知不動（例外：NTF-55）。綁在對話上的由 session 判斷、呼叫 `Retire`（SES-47）；綁單的同一句問句由 tasks 收（催的時候換成新的一則、主人回了其中一則，TSK-30）。未讀數不算 `inline`。
- **NTF-50** — 對話等待：`Nudge` 帶 `key`（例 `pending:<確認編號>`）＝綁在對話上的「要你回答」，不建 `M-<n>`、跟提醒一樣依在場決定去哪（NTF-34～36），但：同 session 可以同時有不同 `key` 的（去重看 session＋key）、推播 tag `nudge-<session>-<key>`、不記 `nudges`、最長留 `WaitLifetime`（24 小時，由發問的 session 收回）、主人在那個對話打字不收。`resolve: true` 收回同 session＋key 那則（不補推，NTF-36），沒有也算成功。看板的 `NudgeCard` 不畫帶 key 的，站內提示照畫。
- **NTF-51** — 自己登記 `Routes()`（notify）、`Commands()`（notify、share、push init）、`Checks()`（`push.status`）、`Board()`（`approvals`、`unread`）、`Jobs()`（`notify.tick`：每秒一次 `Tick`，由 `timer.JobRunner` 跑，沒啟動時是空操作）。測試 `TestHTTPRoundTripAndBoard`、`TestHTTPWritesRequireActor`、`TestHTTPUnavailableAndChecks`、`TestNotifyOptionsSplitOnBar`。
- **NTF-52** — 開發家（`[runtime] env = "dev"`）推播的標題前加「開發｜」：一般通知、提醒、測試推播都一樣；只改推出去的 payload，存檔、看板、sdk 回傳不變（`pushTitle`，`TestDevEnvPushTitleHasPrefix`）。
- **NTF-53** — 推播內容帶 `home`＝這台的標籤（`platform.Home.Label()`）：一般通知與測試推播的 payload 是 `sdk.Notification`，提醒（含對話等待）是 `nudgePayload`，都帶；只填推出去的那份。送 Web Push 只有三條路（`service.go` 的 `Send`、`push.go` 的 `PushTest`、`nudge.go` 的 `pushNudge`），新增的路也要帶。後端不改寫 `url`、不換成完整網址（service worker 照 `home` 換，WEB-36 ⑥）；看板連多台時每台各自收在場回報、不轉送。測試 `TestPushPayloadCarriesHomeLabel`、`TestEveryPushPayloadCarriesHomeLabelAndRelativeURL`。
- **NTF-54** — 引擎回報、只有主人親手能解的錯誤才推（法律第 5 條，`engine_alert.go`）：訂閱 `session.turn_failed`、`session.ended`（欄位正本在 `platform/session_events.go`）。種類先 `sdk.EngineErrorKind.Normalize()`；`auth`、`reason = logged_out`（ENG-16）→ 標題「AI 帳號要重新登入」，`billing` →「AI 帳號的帳單或方案有問題」（標題只說哪一種事，DEV-156 U-08）。內文第一句寫出是哪位秘書、發生什麼（法律第 5 條：誰、哪件事）：「〈秘書名〉剛剛沒辦法回你，因為 AI 帳號登入過期了。」（登出：「〈秘書名〉的對話停了，因為 AI 帳號登出了。」；秘書名是事件帶的現任角色名 `agent_name`，沒帶就說「秘書」，不拿職位代號頂替），接著一句下一步「要你〔在 <台名> 〕重新登入〔 <帳號> 帳號｜這個帳號〕；登入好之前，用這個帳號的秘書都沒辦法做事。」。**台名與帳號名只在需要分辨時才寫**（`Module.alertScope`，只有一個家、一個帳號時是雜訊，U-07）：這個家有 `[[web.sites]]`、`allow_boards` 或是開發家才寫台名（`platform.Home.Label()`）；`[accounts]` 有兩個以上才寫帳號名。白話、同一件事只講一遍：不放平台原話（多半是英文，原話在那個對話那一句的「詳細」）、不寫「這一輪」這種內部話、不露對話名。轉成系統提醒（`source = system.alert`、`approve`、選項「知道了」、`account` 填出事的帳號），照 NTF-43 以「標題＋帳號」去重：同一個帳號好幾位秘書一起出錯只有一則（寫的是第一位）。要做的事已經在內文，`todo` 填「無」（`noTodo`）：推播的字（`sw.js` 的 `bodyFor`：有「請你」用請你，沒有用內文）就是內文開頭這一句。其他種類與結束原因都不推（錯誤在那個對話裡顯示）。測試 `TestOwnerFixableEngineErrorsBecomeOneAlertPerAccount`、`TestEngineErrorsOwnerCannotFixAreNotPushed`、`TestEngineAlertNamesSecretaryAndScopesMachineAccount`、`TestAlertScopeFromHomeConfig`；`cmd/evergenie` 的 `TestTurnFailedAuthNotifiesOnceOverloadedNotAtAll`（內文開頭是角色名）。
- **NTF-55** — 問題沒了通知跟著消失：訂閱 `session.recovered`（`{name, agent, account, after}`，報過失敗的對話又正常做完一輪時發一次；登出也記成 auth 失敗）。`after` 是 `auth` 或 `billing`（`recoveredFromOwnerFixable`）→ 這個帳號（通知的 `account` 相同）的「AI 帳號要重新登入」與「AI 帳號的帳單或方案有問題」兩則一起收（`retireAlerts`，收法同 `Retire`）；其他種類不動；別的帳號、主人已經按過的不動。升級前發的提醒把台名與帳號寫在標題（「<台名> 的 <帳號> 帳號要重新登入」），恢復時那種舊標題也照收（`legacyTitles`）。測試 `TestRecoveredRetiresThatAccountsAlertOnly`、`TestRecoveredRetiresAlertsSentBeforeUpgrade`、`TestReloginAlertRetiredWhenAccountWorksAgain`（`cmd/evergenie`）。
- **NTF-56** — 訊息分兩道（D-47）：每則通知有 `lane`（`agent`＝秘書那道、`system`＝系統那道），規則只在 `lane.go`。`Send` 時定、存進檔案（`sendLane`）：①`source` 是 `system.alert` 或 `package` → `system`（發的人寫什麼都一樣）；②發的人明講（`NotifySendRequest.lane`，CLI `--lane system|agent`）照它，其他值 `invalid`；③其他 `agent`——**不看是哪位秘書發的**：system 坐的 Goal 達成請主人確認、單做完等主人結、規矩提案都是秘書在說話（法律第 5 條要傳）。真的自動的報告由發的地方自己帶 `system`：`scripts/release.sh` 的每晚測試、`update` 的升級結果、`trust/requests` 的每週權限摘要。舊檔（D-47 以前、沒有 `lane`）`Get`／`List` 讀出來時補上（`laneOf`，下次存檔就寫進去）：先照①③，再加一條搬家規則——`settings/notify.toml` `[lanes] system_agents`（沒寫＝`["system"]`，核心自己發通知的署名；各職位秘書的 id 核心不認得，法律第 3 條）裡的秘書發的 `done`、`digest` → `system`；`[lanes]` 只在檔案裡改，`SetPrefs` 原樣保留、改了重開 serve 生效。測試 `TestNoticeLaneRule`、`TestOldNoticeWithoutLaneGetsDerived`、`TestNotifyLaneFlag`。
- **NTF-57** — 未讀數（`Unread()`、`List`／`Watch` 的 `unread`、總覽的 `unread`、選單「訊息」徽章）只算秘書那道的未讀，加上系統那道**照樣推的**：還要主人回答的（未讀的 `approve`，例：帳號要重新登入、套件認領）與標了 `urgent` 的——推了手機就要在徽章上看得到；討論串內提問不算（`countsInBadge`）。每則讀出來時帶 `badge`（這則現在算不算，`decorate`；不存檔），看板照它數、不自己算（FEN-N3）。系統那道其他的只在看板「系統」分頁自己的數字上（WEB-86）。測試 `TestBadgeCountsAgentLaneAndPendingSystem`、`TestSystemAlertIsSystemLaneButCounted`。
- **NTF-58** — 系統那道不推手機（法律第 5 條「自動事件不傳」）：`lane = system`、不是 `approve`、沒標 `urgent` 的，送法一律當 `board`（不推、不進摘要桶），投遞紀錄只有 `board`；要主人回答的與 `urgent` 照偏好與安靜時段（NTF-10）。摘要桶裡都是可以推的，送出的「通知摘要」明講 `agent`。測試 `TestSystemLaneDoesNotPushUnlessAnswerOrUrgent`、`TestDigestSummaryStaysInAgentLane`。
- **NTF-59** — 回答之後等不等秘書讀到，只有一份規則（`deliversAnswer`）：沒被業務塊認走的來源，而且綁在對話上（`room`），或沒綁單／待辦、不是系統提醒、有署名秘書的——這些才記 `answer.delivered_at`、2 小時沒讀到再問一次（`tracksAnswer` 就是它加「有回答、不是自動收」）。讀出來時每則帶 `delivers_answer`（`decorate`，不存檔；跟 `badge` 一樣不加 omitempty），看板照它決定已回答那一列要不要說「秘書還沒讀到…超過會再通知你重答」：系統提醒、套件通知按了「知道了」不會被說成在等秘書（`AnswerPanel` 的 `AnsweredRow`；舊版的台沒給照以前）。測試 `TestDeliversAnswerOnlyForSecretaryNotices`；vitest `NotificationView.spec`「回答過之後那一行小字」。
- **NTF-60** — 只收打字回答的通知（`typed`，D-57）：`Send` 時清掉 `options`、`approve` 也不補預設的「好／不要」；存檔與讀回帶 `typed`；「回答尚未送達」重問那則照抄。看板 `optionsOf` 回空、`AnswerButtons` 不畫按鈕，只留打字回答（今天頁的通知卡片留「打字回答」連結，WEB-112；綁在等你單上的那則不另列，在單的卡片上就地回答，TSK-26、board WEB-178）。目前只有 tasks 的要你回答問句用（TSK-27），它的回答會轉回秘書（NTF-48）；秘書的 API 不開放這個欄位。測試 `TestTypedNoticeGetsNoDefaultOptions`。
- **NTF-61** — 給主人看的通知文字寫秘書的**名字**，不寫 id（2026-11-03 QA：權限申請寫「code 送的，code 想要…」，看板別處都寫 Builder）。權限申請（trust 的 requests）的通知標題、影響說明（`impact`，權限申請頁也顯示）、每週摘要、回給秘書的結果，經 `Deps.Names`（底層 agent 的 `PersonaName`）把秘書 id 換成名字；主人、查不到的照寫原字；申請裡存的 `agent`／`from` 與預覽指紋照舊用 id。測試 `TestRequestNoticeUsesAgentNames`（trust/requests）。
- **NTF-64** — 看板通知設定的自動存（`pages/settings/useAutosavedPrefs.ts`，FEN-22）：離開頁面（換設定分頁、按返回）時還在等 600ms 的那幾格**立刻存**，不清掉計時器就算了——以前改完安靜時段馬上換分頁，回來值還是舊的、也沒有任何提示。測試 `pages/settings/useAutosavedPrefs.spec.tsx`「離開頁面時還沒存的那一格立刻存」。
- **NTF-65** — 接 NTF-64：離開時補存的那次還沒回來，人就回到通知設定又改別格，**不會把剛補存的那格蓋回舊值**。所有存檔排成一條隊（跟著 QueryClient，整個看板共用），補存回來才送下一份；回到頁面時畫面與下一次存檔以補存那份為底（不是快取裡的舊值），補存回來、這頁又沒改過才重抓伺服器上的值（改過就靠自己的存檔帶回，免得晚回來的讀取蓋掉剛存的）；存好時先取消還在路上的讀取再寫快取。已知限制：補存失敗（網路不通）時那幾格沒有交給新頁面重試。以前新頁面拿快取的舊值當底整份送出，剛補存的安靜時段被蓋回去。測試 `pages/settings/useAutosavedPrefs.spec.tsx`「補存還沒回來就回頁面改別格，不會把剛補存的那格蓋回去」。
- **NTF-66** — 訊息的連結按鈕與「說話的人」（DEV-115 新角色來打招呼，PRS-13）：通知多兩個選填欄位 `link{label, url}`、`speaker`，存檔、讀回都帶。`speaker` 只有核心模組在 `Send` 時填；`link` 核心模組與 API 的 `notify.send`（`NotifySendRequest.Link`，套件的程式用，2026-11-15 DEV-157 新家第一天走查②：習慣的「到展示間」「到習慣頁」，HAB-9、HAB-20）都可以帶，`evergenie notify` 指令列沒有開這個旗標，所以秘書用指令發的通知不帶按鈕。舊的 `url` 欄位只給指令輸出看（預設 `/messages/<id>`），看板與推播都不拿它當按鈕。`Send` 時 `cleanLink`：字與網址都空＝沒有；只有一樣、或網址不是站內路徑回 `invalid`，免得一顆按鈕把主人帶到外面的網站——站內路徑＝`/` 開頭、沒有空白與反斜線、路徑部分（`?`、`#` 之前）沒有空段（`//`）也沒有 `.`、`..` 段（瀏覽器會把 `/.//evil.com` 正規化成 `//evil.com`）；`speaker` 去前後空白。**有 `speaker` 的通知一律即時推**（不管主人的分類偏好是只放看板或摘要；偏好「關閉」照舊不推、安靜時段照舊不推）：新角色來打招呼在法律第 5 條的推播白名單（v1.12，2026-11-04 作者）。開發家（`env = "dev"`）例外只放看板，免得同一位新角色正式家推過、開發家再推一次。推出去的內文是自我介紹（`context`）：`sw.js` 的 `bodyFor` 把 `todo` 是「無」當作沒有。`speaker` 只保留給這種招呼，別的地方要用它得先改法律第 5 條。連結按鈕不是回答選項（不進 `options`、按了不算回答）。看板怎麼畫見 WEB-150（指回這則自己的不畫）。測試 `TestSpeakerNoticePushedWithSiteLocalLinkOnly`、`TestHTTPSendCarriesSiteLocalLink`（API 帶的站內按鈕存得進去、外站與只有一樣的回 `invalid`）。
- **NTF-67** — 「AI 帳號要重新登入」「帳單或方案有問題」兩則的內文最後接一句做法（DEV-153，`engine_alert.go` 的 `reloginSteps`、`billingSteps`）：重新登入＝到那台 Mac 前面搜尋「evergenie 修復」（`platform.RescueName`，HST-12）照畫面選重新登入；帳單＝用瀏覽器到他訂閱那個 AI 帳號的官網、訂閱與付款的地方處理（核心不寫是哪一家，D-21）。不叫他找電腦秘書、不要他打指令、不寫金額與方案名。推播的字還是內文開頭那一句。額度用完維持不推播。測試 `TestOwnerFixableEngineErrorsBecomeOneAlertPerAccount`、`TestEngineAlertNamesSecretaryAndScopesMachineAccount`。
- **NTF-68** — 給某位秘書的話送不進去時通知主人一次（DEV-162，`stuck_alert.go`）：訂閱 `session.stuck`、`session.unstuck`（SES-77）。標題「有話送不進去」，內文「〈秘書名〉有 N 則話送不進去，已經等了 M 分鐘。」（秘書名是事件的 `agent_name`，沒帶說「秘書」；不是她的主要對話寫「〈秘書名〉的對話「〈對話名〉」」，主人才不會重開錯；看板還連別的家時前面加「〈這台〉 的」，同 NTF-54 的 `alertScope`）接做法：到看板那個對話右上「⋯」選「重開對話」，還是不行到那台 Mac 開「evergenie 修復」（`platform.RescueName`）；事件帶 `dialog` 時先請他看畫面上有沒有在等他選的確認框、有就回答，再說重開。不叫他打指令。送法同 NTF-54：`system.alert` 來源的 `approve`、只有「知道了」、不標 `urgent`（法律第 5 條白名單「要主人介入的事故：自動處理停下來」，照偏好與安靜時段推）。同一位秘書（通知的 `agent`）還開著一則就不再發；`session.unstuck` 且 `agent_stuck` 不是 true 時收掉（`retireAlerts`），之後再卡才再發；帶 `reset` 的（核心啟動後第一輪）把不在 `agents` 名單上的都收掉。測試 `stuck_alert_test.go` → `TestStuckSecretaryAlertsOwnerOncePerSecretary`、`TestStuckAlertRetiredWhenQueueMovesAgain`、`TestStuckAlertPointsAtTheRightConversationAndFix`、`TestStuckAlertsFromBeforeRestartAreRetiredUnlessStillStuck`。

### push（Web Push 通道，只給 notify 用）
- **PSH-1** — `Send(ctx, sub, payload)` 用家的秘密檔 `[webpush]` 的 `vapid_public_key`、`vapid_private_key`、`vapid_subject` 把 `payload` 加密送到 `sub.Endpoint`（TTL 12 小時、逾時 15 秒）；`vapid_subject` 必須 `mailto:` 或 `https://` 開頭，憑證裡的 `sub` 就是它（webpush-go 會自己補 `mailto:`，交給它前先拿掉，不然變成 `mailto:mailto:`、Apple 回 403 BadJwtToken；`TestSendJWTSubjectMatchesConfig`）；推播服務回非 2xx 算失敗，錯誤帶狀態碼與服務給的原因代碼（Apple 的 JSON `reason`、FCM 的一行文字；只留英數與 `_ . : -`、最多 60 字、有網址就整段不要），404／410 加「關閉再重新開啟」、401／403 加「不認金鑰或聯絡方式」（`TestSendRejectionCarriesStatusAndReason`）。
- **PSH-2** — 金鑰只在 `Send`、`PublicKey` 時讀，每次重讀秘密檔、不快取；秘密檔路徑每次重讀 `home.toml` `[secrets]`（`WithSecretsPath` 可換來源）。金鑰讀不到就不打網路。
- **PSH-3** — `Send`、`PublicKey` 的錯誤一律是整理過的 `platform` 錯誤（`unavailable`／`invalid`，白話）：只帶欄位名，或推播服務的狀態碼與過濾後的原因代碼（PSH-1）；不帶金鑰值、檔案內容、端點網址或原始錯誤，可以放心寫進 log。
- **PSH-4** — 訂閱端點存在 `settings/push.toml`（`[[subscription]]`：`endpoint`、`p256dh`、`auth`）：`Subscriptions()` 讀（不存在回空；壞檔或缺欄位回 `invalid`）；`Subscribe` 照 `endpoint` 新增或更新（不重複、保留順序）回訂閱數；`Unsubscribe` 刪除回剩幾筆，沒有也算成功（不寫檔），刪光寫空檔。原子寫入、權限 0600，讀改寫有鎖。
- **PSH-5** — `Validate(sub)`：`endpoint` 以 `https://` 開頭、`p256dh` 與 `auth` 非空且只含 base64url 字元（可帶結尾 `=`），不合格回 `invalid` 並指出欄位名；`Subscribe` 先 `Validate`，不合格不寫檔。

### overview（首頁彙整）
- **OVW-1** — `Overview(ctx)` 在伺服器算總覽：`WaitingTotal` ＝ `Approvals`（notify 的 `Board()`：未回答、不是 inline 的 approve 通知，兩道都算；掛在等你的任務單上的不列，TSK-26）筆數 ＋ `WaitingTasks`（tasks 塊的 `Board()`）筆數；另有 `Unread`（notify，NTF-57：秘書那道＋系統那道要你回答的與 urgent 的）、`WaitingThreads`（chat 塊）。測試 `TestOverviewCounts`。
- **OVW-2** — 首頁資料段由各業務塊的 `Board()`（`app.BoardPart{Key, Load}`）登記，overview 實作 `app.BoardConsumer`，app 組裝時經 `ConsumeBoard` 交進來。`Key` 就是 `WebGetOverviewResponse` 欄位的 json 名稱（反射讀），不認得的 `Key` 或自己算的 `waiting_total` 在組裝時就回錯；`Load` 回的型別放不進欄位就那次總覽回錯。目前有 `approvals`、`unread`、`today_reminders`、`waiting_tasks`、`waiting_threads`。測試 `TestOverviewFillsBoardParts`。
- **OVW-3** — 任何一段讀取失敗，整個總覽失敗；HTTP 用 `platform.UpstreamError` 轉白話。還沒組裝回 `unavailable`「核心服務尚未啟動」。測試 `TestOverviewFailsWhenAnyPartFails`。
- **OVW-4** — `Routes()` 用 `Mount.Methods` 只登記 sdk `web` 服務的 `get_overview`（同服務的其他方法由 menu、trust、agents、system 塊各自登記）；沒有指令、不提供首頁段。測試 `TestModuleRegistersOnlyGetOverview`、`TestGetOverviewRoute`。
- **OVW-5** — 選單徽章的數字也從 `Overview` 來（塊的 `New` 把同一個 `*overview.Module` 交給 `menu.Deps.Overview`），`WaitingTotal` 只算這一份。

### menu（選單與頁面）
- **MNU-1** — 選單由伺服器依「已安裝＋已啟用＋有權限」算出完整的一份才回（WEB-2）；樹最多兩層、沒放進樹的進「未分類」、`core.settings` 永遠可達、已不存在的項目略過（WEB-3）。測試 `TestMenuComputedOnceComplete`、`TestMenuTreeTwoLevelsOnly`、`TestMenuUnplacedGroup`、`TestSettingsAlwaysReachable`、`TestStaleItemsIgnored`。
- **MNU-2** — 徽章數字向 `Deps.Overview` 拿，不另算；拿不到時 `get_menu` 用 `platform.UpstreamError` 回白話錯誤。測試 `TestMenuBadgesComeFromOverview`。
- **MNU-3** — 套件項目與頁面（`pages.go`）：只列看得到的套件（`Deps.Frontend.VisiblePackages`）；`show_when` 照 MNU-11；套件頁網址只在 `pageRoute` 組，一律 `/p/<套件>/<頁面>`；`package.toml` 的 `route` 整理後放 `PageInfo.alias`（跟正式網址一樣或掛 slot 的不給），看板 404 頁遇到別名轉到正式網址（`TestPageRouteAlias`、e2e `TestPageRouteAliasRedirects`）；項目與頁面帶 `tier`（核心 `official`），沒存過 `menu.toml` 時自己的與分享的套件預設放「我的套件」；頁面不能蓋住核心路由（WEB-65）。核心頁的 slot 由 `pages.go` 的 `hosts` 登記（哪個 slot 在哪個核心頁）、`GetPages` 列在核心頁的 `Slots`：今天頁 `core.overview.cards`、待辦 `core.reminders.top`、角色頁 `core.persona.tab` 與 `core.personas.new`（DEV-143；清單與用法見 `packages.md`「核心頁面留給套件的 slot」）。測試 `TestPackageMenuItemsInPool`、`TestShowWhenAgentEnabled`、`TestSlotPagesListed`、`TestSlotMissingHostNotFatal`、`TestMenuTier`、`TestMenuUserTreeWins`、`TestPageCannotShadowCoreRoute`。
- **MNU-4** — 自訂選單元件（`shell.menu`，WEB-15）只能來自自己的套件（`packages/mine/`）；編不過回錯誤字串，前端退回預設元件。測試 `TestRendererMustBeMine`、`TestRendererCompileErrorReported`。
- **MNU-5** — `set_menu_prefs` 只有主人（`platform.IsOwner(by)`），被拒時不寫檔；驗證失敗回 `invalid` 並指出欄位。測試 `TestSetMenuPrefsOwnerOnly`。
- **MNU-6** — `Routes()` 用 `Mount.Methods` 只登記 sdk `web` 服務的 `get_menu`、`get_pages`、`get_menu_prefs`、`set_menu_prefs`；沒有指令、健檢、定時工作、首頁段。測試 `TestRoutesOnlyMenuMethods`。
- **MNU-7** — 同一個 section 的項目在樹上放在一起（`buildTree` 的 `leaves`，D-62）：第一次遇到某個 section 的項目（使用者的樹或「未分類」），就在那個位置依 pool 的順序（核心在前、再照套件 `order`）放下這個 section 全部可用的項目，含樹裡沒寫的；後面再寫到的略過（看板的入口才是一個、連到第一個分頁）。存過 `menu.toml`、衣櫃在「日常」的家多出的「介紹」跟著衣櫃放，不進「未分類」；`menu.toml` 不改寫；樹上一個同 section 的都沒有（衣櫃停用、樹裡沒寫介紹）就照 WEB-3 進「未分類」。測試 `TestSavedMenuGathersSectionIntoOneEntry`、`TestSavedMenuWithoutSectionPeersLeavesCoreItemUnplaced`。
- **MNU-8** — 預設樹裡帶 section 的核心項目（`placeCoreSections`）：放在同 section 第一個套件項目的前面（「角色」入口在衣櫃原本的位置），沒有就接在「日常」最後；核心只看 section、不寫套件 id（法律第 3 條）。測試 `TestDefaultTreePlacesCoreSectionBeforePackageItems`、`TestMenuComputedOnceComplete`。
- **MNU-9** — 核心項目「介紹」`core.personas`（DEV-117；DEV-156 U-15 以前叫「角色介紹」，頁首已是 section 的「角色」）：網址 `/personas`、圖示 `contact-round`、`section = "personas"`、`section_title = "角色"`、`tier = official`，在 pool 的核心項目裡排在 `core.settings` 前面；`get_pages` 也列它。`personas` 是核心網址的第一段（`CoreRouteSegments`，套件頁的 route 與新套件 id 都不能用，PKG-45）。測試 `TestCorePersonasItemCarriesSection`、`cmd/evergenie` 的 `TestPackagesNewReservedCoversCoreRoutes`（每個核心選單項目的網址第一段都在 `CoreRouteSegments` 與保留字）。
- **MNU-11** — `show_when`（DEV-160 S4，`showwhen.go`）：選單項目與**掛在 slot 的頁面**共用同一個判斷，條件不成立的項目不進 pool、頁面不進 `get_pages`（看板拿不到，角色頁的分頁、今天頁的卡都不出現；網址 `?tab=` 指著它就照 WEB-163 回介紹）。認得兩種寫法：`agent:<職位 id>:enabled`（名冊上有而且啟用）、`cap:<套件 id>.<能力>`（那個套件「API 支援」這個能力；這裡的「能力」跟 manifest 的 `[capabilities]`〔讀寫哪些資料，PKC-3〕無關）。`cap:` 的約定：套件看得到（`VisiblePackages`）、宣告了資料種類 `capabilities`，而且紀錄 `<套件>.capabilities/main`（`Deps.Records`，接塊的 Vault）沒封存、`supports`（字串清單）裡有這個能力；能力由套件自己算、寫進那筆紀錄，核心不認得哪個套件有什麼能力、不判斷是哪種家（官方衣櫃的 `mine`／`edit` 見 closet 卡 CLO-32）。一次請求每個套件的能力紀錄只讀一次、名冊只讀一次；沒接 Records／Roster、找不到、讀不到、封存、套件關掉都當不成立（fail closed，讀錯只警告一次）；不認得的寫法也不顯示並警告一次（新核心多認得的寫法在舊核心只會藏起來，不會讓套件被拒）。能力改了，看板下一次重抓頁面清單才跟著變（`pagesQuery` 30 秒後才算過期，之後重新整理、切回視窗或重新進頁面時重抓）。核心只看紀錄、不看套件的程式在不在跑：程式停了，紀錄停在最後寫的那份。`show_when` 只能寫在有 `slot` 的頁面（PKG-69）。測試 `TestShowWhenAgentEnabled`、`TestSlotPageShowWhenCapability`、`TestSlotPageShowWhenFailsClosed`、`TestMenuShowWhenCapability`、`TestParseCap`，看板 `personaTabs.spec.ts` 的 `TestPersonaTabsOnlyFromListedPages`。

## NEVER
- **NTF-N1** — 不讓任何呼叫者指定管道或繞過偏好。
- **NTF-N2** — 通知內容只存在 `vault/messages/`，不寫進 log；不存秘密。
- **NTF-N3** — 不 import `session`、別的業務塊；用事件或注入的小 interface。
- **NTF-N4** — 提醒與在場資料不落盤、不寫 log、不建訊息；在場只用來決定提醒去哪。
- **NTF-N5** — 不自己讀 VAPID 金鑰、加密、打推播服務或讀寫 `settings/push.toml`：一律經 `Deps.Push`（`PushChannel`；怎麼接見「組裝」）。通道回的錯誤只有 `platform` 錯誤能寫進 log 或回給呼叫者，其他一律換成「推播送出失敗」。
- **PSH-N1** — push 不實作 `Routes()`、`Commands()`，不開 goroutine，不發 bus 事件。
- **PSH-N2** — push 不知道通知規則：不看分類、偏好、安靜時段、開發家、誰能管裝置。
- **PSH-N3** — push 不 import 任何業務模組（含同塊的 notify），只用標準庫、`platform`、`webpush-go`、`toml`。
- **PSH-N4** — push 不快取、不記錄、不印金鑰、`payload` 或訂閱網址。
- **OVW-N1** — overview 不 import 別的業務塊；首頁要別的業務塊的資料，那個業務塊自己登記 `Board()`。
- **OVW-N2** — overview 不寫業務塊的業務、不列業務塊的鍵：鍵對欄位只看 sdk 回應的 json 名稱。
- **MNU-10** — 選單項目可以收到設定（DEV-140，2026-11-07）：`settings/menu.toml` 的 `hidden`；沒寫＝預設（`defaultHidden`，權限申請），空清單＝全部放回。收起來的項目不進 `tree` 與「未分類」，回在 `menu.tucked`，設定頁列入口（桌機在分頁清單下面，手機在設定首頁的「進階」裡，WEB-184）；網址照常能開，樹上的位置留著，放回就回原位。今天與設定不能收（`tuckable`）。測試 `TestTuckedItems`。
- **MNU-N1** — menu 不 import 別的業務塊；套件清單、編譯、名冊經 `Deps` 的小 interface 注入，總覽是同塊的 `*overview.Module`（也在 `Deps`）。
- **MNU-N2** — menu 不自己編譯套件頁面或送檔案：編譯與 `/pkg-bundles/` 在看板外殼 `base/app/board` 的 `Assets`。

## 職責
- notify：偏好、分類到送法的決策、摘要計時、管道送出、通知檔與狀態、回答的收件與重送、未讀數、分享檔案、`system.alert` 與引擎錯誤轉成系統提醒、提醒與在場、通知回收。
- push：VAPID 金鑰讀取、Web Push 加密與送出、訂閱端點檔案的讀寫與格式檢查。
- overview：「等你處理」的彙整與首頁資料段的收集填入。
- menu：選單（pool、樹、徽章、自訂元件）、頁面清單（核心頁、套件頁、slot 頁）、`settings/menu.toml` 的讀寫與驗證、預設樹。

## 不負責
- 答案怎麼送回 session → 底層 session；待辦的完成與延後 → tasks 塊；通知文案 → 呼叫者；頁面、手機端訂閱與 service worker → web；聽連接埠、靜態檔、編譯與送 bundle → `base/app/board`；套件的安裝與狀態 → packages 塊。

## 組裝（塊的 Module，S8-3）
`internal/feature/inbox/module.go`（內嵌 `*app.Group`，APP-13）。Deps（塊外）：Bus、Vault、Clock、Frontend（看板 assets）、Roster；塊內 push→notify（`push.New("")` 符合 `PushChannel`，塊的 `SetHome` 再交家給 push）、overview→menu（`*overview.Module`）；Vault 也交給 menu 當 `Records`（MNU-11）。Start 順序 notify → overview → menu（overview、menu 只開 log）。任務單建得比較晚：`SetTasks`。存取 `Notices()`／`Overview()`／`Menu()`；指令 `inbox.Commands()`。測試 `module_test.go`。

## 介面
- notify：API `NotifyAPI`（`sdk/notify.go`：`Send`、`Get`、`List`、`MarkRead`、`Answer`、`Archive`、`Share`、`GetPrefs`、`SetPrefs`、`Watch`、`PushKey`、`SubscribePush`、`UnsubscribePush`、`PushTest`、`Nudge`、`Presence`、`ResolveNudge`）。Go 另有 `SetPrefsExpand`、`ExpandPreset`（純函式）、`Tick`、`Retire`、`PushStatus`（金鑰設好且至少一台裝置才 Ok，doctor 與健康頁共用）、`SetTasks(TaskNudges)`、`ConsumeNoticeSources`（`app.NoticeConsumer`）。`Deps`：`Bus`、`Vault`、`Push`（`PushChannel`：`Send`、`PublicKey`、`Subscriptions`、`Subscribe`、`Unsubscribe`）、`Clock`。`pushcmd.go` 離線產生推播金鑰（`push init`，`TestPushInitOfflineAndPreservesExistingKeys`）。型別直接用 sdk 的（MOD-N5）。
- push：`Subscription{Endpoint, P256dh, Auth}`、`New(dir, opts...) *Service`（不做事、不讀檔；`dir` 空的時不讀不寫）、`SetHome`、`WithSecretsPath`、`(*Service).Send`／`PublicKey`／`Subscriptions`／`Subscribe`／`Unsubscribe`、`Validate`（`push.go`、`webpush.go`）。
- overview：API `web.get_overview`；Go `Overview`、`ConsumeBoard`；`Deps{}` 是空的。
- menu：API `web.get_menu`、`get_pages`、`get_menu_prefs`、`set_menu_prefs`。`Deps`（都可以是 nil）：`Frontend`（由 `board.Assets` 提供：`VisiblePackages`、`FindPage`、`Bundle`）、`Roster`（`List`）、`Records`（`Get`，`show_when` 的 `cap:` 讀套件的能力紀錄，MNU-11；塊的 `New` 接 `Deps.Vault`）、`Overview`（同塊的 `*overview.Module`）。

## 事件
- notify 發出（`lane` 不放事件裡，要的人讀通知）：`notice.sent`（`{id, category, agent, room, task, reminder, delivery}`）、`notice.answered`（`{id, text, via, agent, room, task, todo, reminder, source, by, relay}`；`todo` 讓 tasks 認出「請驗收」那則，TSK-25）、`notice.nudged`（`{id, agent, session, task, surface, key}`）、`notice.nudge.resolved`（`{id, agent, session, task}`）。
- notify 訂閱：`system.alert`（`{title, context, agent}`）、`session.turn_failed`、`session.ended`、`session.recovered`、`session.stuck`、`session.unstuck`（NTF-68）、各通知來源的 `Changed`（`goal.changed`、`policy.changed`）、`reminder.changed`、`task.changed`、`bus.envelope.state`（對應 notice 的封包 `received` 時補 `answer.delivered_at`；NTF-36 的主人回話）、`session.approval.resolved`（`{name, pending_id, notices}`：替全部通知補送達時間，重複回執不重寫）。
- push、overview、menu 沒有事件。

## 儲存
- notify：`schemas/message.md`（`vault/messages/`）、`schemas/notify-prefs.md`；分享檔 `runtime/share/`；提醒與在場只在記憶體。`message` 只有 notify 讀寫（擁有者表寫 `inbox/notify`，depcheck 連同塊的 overview、menu 也擋）；別的子模組經 notify 的 `Board()` 段拿。
- push：`settings/push.toml`（0600）；金鑰在 `home.toml` `[secrets]` 指的秘密檔（唯讀）。
- overview：None。
- menu：`settings/menu.toml`（`schemas/menu-prefs.md`），整檔寫入；只讀套件的 `<套件>.capabilities/main`（MNU-11），不寫 vault。

## 依賴
`platform`、`vault`、`app`（登記介面與 `cmdline`，import）。同塊的子套件：`inbox/notify` import `inbox/push`（只用 `Subscription` 型別與 `Validate`；傳送與端點管理經 `Deps.Push`，測試要換掉真的送出）；`inbox/menu` import `inbox/overview`（徽章的數字）；overview 不 import 同塊別的子套件。另用第三方 webpush-go、BurntSushi/toml。匯流排、任務單、名冊、看板的 `Assets` 都不 import，經塊的 Deps 與 setter 注入；首頁段與通知來源由 app 組裝時交進來。

## 錯誤
- notify：`INVALID`（沒分類、`approve` 沒 `todo`、`approve` 設成 `off`、不能分享的檔案）、`FORBIDDEN`（秘書管推播裝置、刪訊息、回報在場；分享秘密檔）、`NOT_FOUND`（回答不存在的通知）、`UNAVAILABLE`（所有管道都失敗，通知仍在看板；沒有推播金鑰；沙盒家測試推播）。
- push：`UNAVAILABLE`（沒設秘密檔、讀不到、送出失敗）、`INVALID`（金鑰檔、訂閱格式、壞的訂閱檔）、`INTERNAL`（訂閱無法編碼）；訊息不帶輸入。
- overview：原錯誤、段型別不對的內部錯誤、`UNAVAILABLE`（還沒組裝）；`ConsumeBoard` 對不認得的鍵回 Go `error`（擋下啟動）。
- menu：非主人 `forbidden`（欄位 `by`）、樹不合法 `invalid`（指出哪一格）、總覽拿不到經 `platform.UpstreamError`；套件頁編不過寫在該頁的 `error`，不算錯誤。

## 不變條件
- 成功的 `Send` 至少有 `board` 這筆 `delivery`；`instant` 且非安靜時段時另有 `app`；安靜時段只有 `board`，`urgent` 才有 `app`；`line` 只在設定開啟時有。
- 通知文字第一行符合 `^\[.+｜.+\] .+｜.+$`；呼叫者傳的 `title` 已含標籤不重複加。
- `digest` 模式下 N 小時內多則只出一則摘要，列出每一則標題。
- 沙盒家、`inline` 通知推播呼叫 0 次（含 `urgent`）；提醒不建 `M-<n>`、未讀數不變；頁內卡片 2 分鐘沒回只推一次；回答或收回後不再推任何東西（DEV-123）。
- `source` 是 `goals`／`policy` 的通知回答後過 2 小時也不變 `failed`、不轉進秘書的對話。
- `Share` 先 `EvalSymlinks` 再判斷：只收一般檔案；超過 25 MiB `invalid`（先 `Stat`）；家的秘密檔、`settings/`、`~/.ssh`、`~/.gnupg`、`~/.aws`、`~/.config/gh` 底下、`.env`／`.env.*` 一律 `forbidden`、不複製；錯誤不帶路徑；讀檔不握模組的鎖。存檔名 `<ULID>-<原檔名>`（`platform.ShareName`，看板上傳共用，WEB-24），網址 `/share/<名字>`，回應帶 `public_base`。
- push：秘密檔換掉後下一次就用新的；金鑰有問題時錯誤不含金鑰、不送任何請求；多台訂閱不互相蓋掉。
- overview：`WaitingTotal` ＝ `len(Approvals)` ＋ `len(WaitingTasks)`；沒有哪一段被默默吞掉。
- menu：選單一次回完整的一份，壞掉的使用者套件只有它自己不見。

## 測試計畫
- notify（`notify/*_test.go`）：`service_test.go`（分類、送法、格式、安靜時段、分享、沙盒、送達重送；例 `TestShareRefusesSecrets`）、`nudge_test.go`（提醒與在場，例 `TestPresenceSuppressesPush`）、`retire_test.go`、`engine_alert_test.go`、`push_test.go`（真的 `push.Service`）、`http_test.go`、`cli_test.go`、`module_test.go`；跨模組 `TestAddNudgeCountsWithoutFollowCadence`（tasks 塊）、`TestWorkSessionLifecycleFollowsTask`（`cmd/evergenie`）、`TestModuleOwnedAnswersStayOutOfAgentThreads`（session）。
- push：`TestSubscribeAddsAndUpdates`、`TestSubscribeConcurrent`、`TestKeyFileProblemsDoNotLeak`、`TestSendEncryptsAndChecksStatus`（`httptest` 假推播服務）、`TestSendWithoutKeyNeverCallsNetwork`、`TestEmptyHomeUntilSetHome`（塊先用空的家建）。
- 整塊：`internal/feature/inbox/module_test.go`（假物件組整塊、子模組順序、Start／Stop 一輪）。
- overview：見 OVW-1～4；登記那邊 `TestBoardWaitingTasks`、`TestBoardWaitingThreads`。
- menu：見 MNU 各條，另 `TestDefaultTreeKeepsTasksInBar`；整條路 `cmd/evergenie/board_menu_test.go` 的 `TestBrokenUserFeatureIsolated`。

## 設計決定
- **D-14** — 每則通知帶一個分類（`approve` 要你同意／`done` 做完／`due` 到期／`digest` 摘要／`opening` 主動開場）；每類可選即時、合併成摘要、只放看板、關閉。簡單模式挑預設組，完整模式逐類、逐秘書調；安靜時段、沒回多久再問、最多追幾次在同一份設定。由 notify 統一把關，套件只回報分類、不能繞過；`approve` 最低只到「只放看板」。
- **D-47** — 訊息分「秘書｜系統」兩道（2026-11-02：訊息頁與徽章曾被幾十則「每晚測試沒過」洗版）。秘書跟主人說的話、問的事是一道；系統提醒、套件通知、排程與測試、部署的自動報告是另一道。決定：①哪一道由核心算、寫在通知上（`lane`，NTF-56），看板只照欄位分頁，不在前端認秘書 id；徽章算不算也由核心算好放在 `badge`（NTF-57）；②新的通知**不看署名分道**：只有系統提醒、套件通知一律在系統那道，其他要發的地方明講 `lane=system`（每晚測試、升級結果、每週權限摘要）。起初想讓 `system`／`admin` 的 `done`／`digest` 都算系統，review 抓到會把法律第 5 條要傳的（system 坐的 Goal 達成請確認、規矩提案、單做完等主人結）藏起來，而且核心寫死 `admin` 違反法律第 3 條——所以署名規則只留給舊檔搬家（`[lanes] system_agents`，預設只有核心自己的署名 `system`；想把 `admin` 以前那批也收進系統的家自己寫）；③徽章只算秘書那道，系統那道還要你回答的與 `urgent` 的也算進去，並在「秘書」分頁上面一行「系統有 N 則要你處理」帶過去（NTF-57、WEB-86）——推了手機、要你處理的事不能因為分頁而看不到；④系統那道不推，例外同③（NTF-58，跟法律第 5 條的白名單一致：帳號登出這類事故是 `approve`，照推）；⑤連在一起、同一種的系統通知在看板收成一列「<標題> ×N」，同一種＝同一位、同一分類、標題拿掉 commit 編號與數字後一樣；要你處理的不收；刪那一列＝`notify.archive` 一次送整組編號（WEB-86）；⑥推播、今天頁這些連結只帶編號、不帶 `?lane=`：看板照選的那一則所在的那道開分頁（不在核心的網址裡塞分頁參數）。
- **D-34** — 法律第 5 條的做法：①提醒推播只推手機、不建 `M-<n>`，內容寫單號、標題、要主人做什麼，點下去開那個 session 的聊天；次數記在任務單 `nudges`；安靜時段不推。②主人正在看就不推：看那張單或那個討論串 → 頁內卡片；看板開著但在別頁 → 站內提示；都不是 → 推播；頁內卡片 2 分鐘沒按或離開 → 改推播；任一處回答，其他地方一起收掉。③session 旁顯示 prompt cache 冷卻倒數（只做在前端）。
- **D-62** — 「角色」頁：核心頁也能加入 section（DEV-117，作者 2026-11-04「你把角色庫跟衣櫃放一起在同一個page」，選「上面三個分頁：角色介紹｜衣櫃｜展示間」；分頁 2026-11-15 改叫「介紹」，DEV-156 U-15）。做法：沿用 REQ-235 的 section（同 section 合成一個入口、上方分頁），核心多一個選單項目 `core.personas`（介紹，`/personas`）帶 `section = "personas"`、`section_title = "角色"`；角色庫從「設定」搬到這頁，`/settings/personas` 轉址（保留 `?card=`）。衣櫃的兩個選單項目宣告 `section = "personas"` 加入——核心不寫衣櫃的名字（法律第 3 條）；關掉衣櫃只剩介紹，不畫只有一格的分頁列。分頁與入口的順序由伺服器的 pool 決定（核心在前、再照 `order`），選單樹裡同 section 的項目在第一次出現的位置放在一起（MNU-7），存過選單的家不會多一個掉進「未分類」的入口。「顯示設定」開關（DEV-156 U-15 前叫「項目」）只列套件（核心分頁不能關）；關掉正在看的套件頁、又沒有別的套件分頁時回到核心頁。看板的角色頁程式暫時留在 `pages/settings/`（角色卡的純函式、查詢跟「職位與秘書」共用）；照 FEN-26／27 該開 `pages/personas/`、共用的搬到 `data/`／`lib/`，記成待辦，S0 先不搬（只換外框與網址，降低這張單的風險）。與法律的落差：角色庫在核心（D-31；法律 v1.13 第 3 條 2026-11-04 作者改成列在核心），所以核心可以有自己的選單項目與網址。否決：①衣櫃自己做一個角色分頁（角色庫照 D-31 由核心做〔`modules/agent.md`〕，套件不能內嵌核心頁）；②核心寫死「角色」入口去找 closet 的項目（違反法律第 3 條）；③把核心項目寫進使用者的 `menu.toml`（改使用者的檔，衣櫃停用時又是個空殼）；④衣櫃也寫自己的 `section_title`（兩邊各寫一份名字會不一致，改成用第一個寫了的）。

## 已知限制
- LINE 還沒做。
- 摘要桶在記憶體，重啟後不補推先前桶內通知（原通知仍在看板）；`Watch` 慢讀者會斷線，重新 `List`／`Watch` 取得現況。
- 「要重新登入／帳單」自動收掉靠 session 記在記憶體的「上一次失敗」：serve 重開過就不會發 `session.recovered`，那則留著等主人按「知道了」。
- 一支手機收多台推播，前提是各台共用同一組 VAPID 金鑰（`runbooks/add-board-site.md` 第 3 步；看板比公開金鑰一樣才替那台訂閱，WEB-36 ⑥）；push 不比對金鑰、不同步訂閱檔。
- 送出失敗（含 410）不會自動刪過期訂閱，要主人在看板取消。
- overview 的段型別在執行期才檢查（`Load` 回 `any`）。
- 選單編輯器不能挑自訂選單元件，只能看目前是哪一個、改回預設。
