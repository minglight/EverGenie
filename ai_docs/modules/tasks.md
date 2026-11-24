# 模組：tasks（`internal/feature/tasks`）
Status: DRAFT — 2026-11-02 S8-3 合併 tasks.md、reminders.md、goals.md、jobs.md，pending author review.

**目的：** 主人要追的事與時間：任務單（要驗收才結，D-22）、主人待辦（時間到一定送到）、Goal 與 Planbook（原文由核心保管，D-23）、使用者排程與夜班。計時迴圈在底層 `timer`，這塊決定到期之後做什麼。
**種類：** 業務塊（7 塊之一；子套件 `tasks/task`（原 tasks）、`tasks/reminder`（原 reminders）、`tasks/goal`（原 goals）、`tasks/job`（原 jobs，含範本 `jobtemplates/`）、`tasks/attach`（任務單與待辦共用的附件規則，沒有 Module，D-61））

## MUST
### task（任務單）
- **TSK-1** — 開單、改狀態、送驗收、通過、退回、結單、查單（`Create`、`Update`、`Submit`、`Accept`、`Reject`、`Close`、`Get`、`List`），CLI 與看板走同一組 API。
- **TSK-2** — 狀態六種：待做 `idea`、進行中 `doing`、等你 `waiting_user`、等外部 `waiting_external`、完成 `done`、不做了 `cancelled`；類型 `task`、`ask`；其他值拒絕；轉移照 `schemas/task.md`。交付等驗收＝等你＋`proposed` 有值。
- **TSK-3** — `Submit`（一般任務送驗收）必填 `result`、`verify`、`verify_how`、`proposed`，可附 `links`。`Close` 只給問答類（秘書可用，要 `result`）與使用者。
- **TSK-4** — 待做收件匣：`List(ideas_only)` 回全部職位的 `idea` 單，開單日舊的在前。
- **TSK-5** — `Stale(now)` 回 `doing`／`waiting_external` 超過 `stale_days` 天沒更新的單（舊的在前）；`Reconcile(agent, now)` 回這位秘書超過 24 小時沒更新的 `doing`；兩者都不改單。`cancelled` 只有使用者能設（問答類例外）。
- **TSK-6** — 進入 `waiting_user`（要主人決定時必填 `ask`；送驗收也算）填 `waiting_since`、`nudges` 歸零，發事件讓 notify 推一則 `approve`；離開清掉 `waiting_since`。
- **TSK-12** — 跟進：`Waiting(now)` 回所有等你的單與等了幾天（最久在前）；`DueNudges(now)` 回距 `waiting_since`（或 `last_nudged_at`）超過 `nudge_every_days`、`nudges` 未到 `nudge_max` 的單；`MarkNudged` 加一。`[tasks]` 四欄每次檢查前重讀 `settings/notify.toml`；壞檔沿用上次的值並記 WARN（`Start` 時壞檔回錯）。
- **TSK-13** — `Submit` 把單改成 `waiting_user`、寫 `proposed`（`done`／`cancelled`）、`ask` 補「請驗收」；問答類不能送（`CONFLICT`）。`Accept`／`Reject` 只收等你＋`proposed` 的單；`Reject(id, note, by)` 只有使用者，單回 `doing`、寫 `review_note`，下次 `Submit` 清掉。離開等你、或不是送驗收而進入等你時拿掉 `proposed` 與自動補的「請驗收」。
- **TSK-14** — 秘書把一般任務設成結束預設 `FORBIDDEN`（`Update` 一律不行）。例外（法律第 1.4 條）：單上 `agent` 本人 `Close` 成 `done`、單在 `doing`／`waiting_external`（其他 `CONFLICT`）、有主人的驗收條件（`criteria_owner`，或掛在主人確認過、進行中或已達成的 Goal）、`verify` 是 `verified`、`result` 與 `verify_how` 有寫 → 直接結，`accepted_by` 記她的職位 id；不符就 `FORBIDDEN`（叫她送驗收）或 `INVALID`（沒驗過、缺欄位）；秘書不能結成 `cancelled`。問答類秘書可 `Close` 成 `done` 或 `cancelled`（`result` 寫原因）。`Accept` 只有使用者，結局取 `proposed`，填 `accepted_by`、`accepted_at`、`closed_at`。
- **TSK-24** — 驗收條件（法律第 1.4 條）：`criteria`＋`criteria_owner`。秘書開一般任務沒寫 `criteria` 回 `INVALID`；主人開單可不寫，秘書 `Submit` 前要補。主人寫的 `criteria_owner` 是 true；秘書寫的是 false，除非帶 `criteria_from_owner`（照抄主人的話）；秘書不能把條件改空、不能改主人的條件（`FORBIDDEN`）；秘書改了 `agent`，`criteria_owner` 降成 false。問答類不用。核心開的事件單（`owner(system)`）的條件算主人的。
- **TSK-25** — 交付等驗收的那則通知（TSK-6 推的 `approve`，`todo` 是「請驗收」）按鈕是「驗收／退回」。主人回答它（`notice.answered`，不是 `via=auto`）時不走「最後一則答完就回進行中」：「驗收」（舊通知的「好」）→ `Accept`；「退回」（舊的「不要」）→ `Reject`，意見寫「主人在通知按了「退回」…」；打字回答 → 那句話當退回意見 `Reject`。只認 `notice.answered` 帶的 `todo` 是「請驗收」的那則：送驗收前就開著的舊問句（等你→等你送驗收不會收掉它）回答了不動單（`TestAnsweringOldQuestionDoesNotAcceptReview`）。以前按「好」會把單送回 `doing`、清掉 `proposed`，等於沒驗收也沒退回。測試 `TestAnsweringReviewNoticeAcceptsOrRejects`。
- **TSK-26** — 首頁「等你處理」一件事一張卡：TSK-6／TSK-12 推的、掛在等你單上的 `approve` 通知（`task` 有值、那張單現在是 `waiting_user`，而且 `todo` 是「請驗收」或就是單上的 `ask`；秘書另外問、內容不同的照列）不進總覽的 `approvals`（notify 的 `Board()` 經 `SetTasks` 接上的 tasks 讀單的狀態），只留 `waiting_tasks` 那張單（交付等驗收的有驗收／退回；秘書在問你的在問句下面就地回答、另有次要的「去聊」，board WEB-178），`waiting_total` 與徽章也就不重算。訊息頁照樣有那則通知。測試 `TestBoardApprovalsSkipWaitingTaskNotices`、e2e `TestOverviewWaitingTaskShownOnce`。
- **TSK-27** — 要你回答（`waiting_user`、有 `ask`、沒有 `proposed`）時 TSK-6 推的那則通知標 `typed`（只收打字回答，D-57、NTF-60）：不給預設的「好／不要」——問句是秘書寫的，可能是「要日式還是義式？」，按「好」說不通又會把單送回進行中。主人打字回答（或在對話裡跟她說）照舊走「最後一則答完就回進行中」；打字回答的內容也轉回秘書（notify 的 `relaysAnswer` 對綁單的 `typed` 通知例外地轉，NTF-48；送到那張單的工作 session，沒有就給秘書本人，SES-11）——以前只把單送回進行中，秘書讀不到主人答了什麼（DEV-156 U-05）。看板在單的卡片與詳情頁就地回答（board WEB-178）。請驗收那則（TSK-25）不標，照舊「驗收／退回」。測試 `TestWaitingDecisionNoticeIsTypedOnly`。
- **TSK-28** — 任務單可以放附件（DEV-104，作者 2026-11-03，資料模型 D-61）：欄位 `attachments`（`/share/<名字>` 連結的清單）。開單 `attachments`、之後 `tasks.update` 的 `add_attachments`／`remove_attachments`（CLI `tasks add --attachments`、`tasks set --add_attachments／--remove_attachments`，可重複）；主人與秘書都能加。每個連結都要是這個家 `runtime/share/` 裡真的有的一般檔（完整 `http(s)://…/share/…` 收成路徑、去頭尾空白、重複的只留一個），不對的整筆不存、`INVALID` 指到欄位；結束的單 `CONFLICT`；拿掉只從單上拿掉、不刪檔（同一個檔可能也在訊息裡）。交給秘書的那一段（`BriefText`、工作 session 的 `readWork`）在內文後列「附件：」一行一個，秘書照 AGT-14 直接讀。看板：開單表單（任務單頁、手機的開單面板）在標題或說明貼上（⌘V／Ctrl+V）或按「附件」選檔，選了就上傳到要開在的那台，列在輸入框下（上傳中／失敗可重試／拿掉），還在上傳不能送；開在別台就在那台再傳一次（FEN-28）。快速輸入的「交代 …」把貼的圖帶進單的附件（不再只送標題）。詳情頁（含今天頁右欄）「附件」段：圖畫縮圖、點了照 WEB-132 在這頁看大圖，其他檔是下載連結，可以「加附件」（選檔，或在頁面上直接貼上；右欄只收點過右欄之後的貼上）、拿掉（toast 可復原）。測試 `task/attachments_test.go`（`TestAttachmentsOnCreateAndUpdate`、`TestAttachmentMustBeSharedFile`、`TestEndedTaskRejectsAttachments`、`TestBriefListsAttachments`）、`TestTaskCommandsOnFakes`、session 的 `TestWorkBriefListsAttachments`；vitest `attachments.spec`、`taskCreate.spec`「附件有才帶 attachments」、`QuickInput.spec` `TestQuickInputAssignCarriesImages`；e2e `TestBoardCreateTaskWithAttachments`、`TestTaskAddAttachment`、`TestQuickInputAssignCarriesImage`。
- **TSK-29** — 任務單內容可以改（DEV-129，作者 2026-11-05「現在任務單不能修改耶」）：`tasks.update` 收 `title`、`body`（CLI `tasks set --title／--body`）；沒給＝不動；標題去頭尾空白、空白回 `INVALID`，需求給空字串＝清掉；只有主人能改，秘書 `FORBIDDEN`（條件可能就寫在標題或需求裡，秘書改了等於繞過法律第 1.4 條）；結束的單 `CONFLICT`。看板詳情頁（含今天頁右欄）沒結束的單，標題、需求、驗收條件旁有「改標題」「改需求」「改驗收條件」，就地編輯（標題 Enter 或失焦存；需求與驗收條件有「存」「取消」鈕、⌘／Ctrl+Enter 也能存、失焦不存，寫到一半切走不會存半句；Esc 還原；沒改過不送，不會把別人剛改的內容蓋回去）；主人在這裡改的驗收條件算主人的（TSK-24）。結束的單只顯示。工作 session 已開著時改內容不另外通知秘書，要她知道就在專屬對話說一聲。測試 `TestUpdateTitleBodyAndCriteria`、`TestTaskCommandsOnFakes`；e2e `TestTaskDetailEditContent`。
- **TSK-30** — 同一張單、同一句問句只留一則在等主人（DEV-156 U-05，2026-11-15）：催（`tasks.nudge` 的事件迴圈，不拿著 tasks 的鎖）先送新的一則（`sendWaiting` 回它的編號），送出了再把這張單上 TSK-6 自己推過、還沒回的舊那則收掉（`staleOwn`：`ownNotice` 是沒綁對話、沒有來源、只收打字或「請驗收」的；要你回答時全部換——問句一樣的、秘書在等你期間改了問句留下的舊問句；交付等驗收時只換「請驗收」，送驗收前的舊問句照 TSK-25 留著；notify `Retire`，via `auto`，理由「又提醒了一次，看新的那則」）；先送後收，送不出去時舊的還在。秘書自己綁單發的（有對話、帶選項）不換。主人回了其中一則（`notice.answered`，不是 auto，帶 `todo`），而且回的就是單上那句（`todo == ask`）、答案會轉回秘書（`relay`，NTF-48）：同一句的其他則一起收掉（理由「已在另一則回答」），再看還有沒有別則決定回不回進行中（舊資料以前催一次就多一則）。問別的事的那則不動（秘書另外問、問句剛好一樣是「請回覆」也不動）；答案不會轉回秘書的那則（例：沒綁對話、帶選項）被回了，不收只收打字的那則，秘書才讀得到主人答了什麼。以前任務單上一個問題會出現好幾個回答框，回了一個單還卡在等你、再回一次秘書收到兩次。看板的回答框只拿單上那句（`task.ask`）一則：先挑只收打字的、再挑最新的（`useTaskAsks`，WEB-178）；主人送出時那則剛好被收掉，看板說「沒送出去」（`answer.retired`）、不假裝已回。測試 `TestNudgeRetiresOlderSameQuestion`、`TestAnsweringOneOfDuplicateAsksRetiresTheRest`，e2e `tasks.spec.ts` → `TestWaitingTaskDuplicateAskOneBox`；notify 的 `notice.answered` 帶 `todo`（`TestAnswerPublishesEvent`，以前漏帶，通知上按「驗收」不會驗收，TSK-25）。
- **TSK-7** — 提供任務單頁的查詢（依職位、狀態、Goal）與頁面註冊 `core.tasks`。
- **TSK-8** — `Brief(id)`：專屬對話（`<職位>-sec-<n>`）的第一句，帶單號、需求，有 `review_note` 一併帶；格式由 `BriefText(task)` 產生。
- **TSK-9** — 每張單記 `by`（職位 id、`owner(web)`、`owner(cli)`）。
- **TSK-10** — 掛 Goal：選填 `goal`（`GOAL-<n>`），一單最多一個；只驗證 Goal 存在（經同塊的 `goal.CanAttach`，任何狀態），秘書或使用者掛都不用主人確認，不存在回 `FORBIDDEN`；空字串拿掉；舊 `group` 讀成 `goal`（D-25）。
- **TSK-11** — 不讀簡單／完整模式（模式只影響看板怎麼顯示，WEB-26）：`goal` 欄位、`List`／`Get`、CLI 一律照常。
- **TSK-15** — 廢除：DEV 單併進工坊家的一般任務單（單號字首 `DEV-`），同一套狀態機。
- **TSK-16** — 工作 session 的記錄（D-33）：選填 `engine_session`，`Update` 可帶、空字串拿掉；負責的秘書與使用者都能寫，不改狀態、不動跟進計數；何時寫是 session 的事。`site` 已拿掉（法律第 7 條：每台一個家、家與家不連線）。
- **TSK-17** — `AddNudge(id)`（D-34）：「還在等你」提醒真的顯示或推出時，notify 經 `TaskNudges` 把 `nudges` 加 1：不看每日上限、不動 `last_nudged_at`、單不在等待也照記；沒這張單 `NOT_FOUND`。
- **TSK-18** — 主人刪單：`Archive(ids, by)` 只有 `owner(…)`，秘書 `FORBIDDEN`；沒結束的先記成 `cancelled`，再 `vault.ArchiveNow` 搬進 `archive`（不刪檔）；找不到或已封存的略過。看板代理 `tasks.archive`（`by` 換成 `owner(web)`）。
- **TSK-19** — 看板手動改狀態：沒結束的單可改成待做／進行中／等外部（`tasks.update`），或結成完成／不做了（`tasks.close`，按兩下確認）；等你不給選。完成時沒寫結果就記「主人在看板手動結單」、`unverifiable`。
- **TSK-20** — 廢除：同 TSK-15（工坊家 `[ids] tasks = "DEV-"`，驗收照一般任務單）。
- **TSK-21** — 自己登記對外面（`conventions/module-pattern.md`；四個子套件都一樣：`http.go` 驗操作者後直接呼叫模組、錯誤經 `platform.SDKError`，`cli.go` 只用 `sdk` client 與 `cmdline`）：`Routes()` 登記 sdk `tasks` 服務；`Commands()` 登記 `tasks add|set|submit|done|get|list|stale|waiting`；`Board()` 登記首頁 `waiting_tasks`（跟 `tasks.waiting` 同一份）；沒有健檢，每天的跟進是自己的計時。測試 `TestRoutesRegisterTasksService`、`TestHTTPWritesRequireActor`、`TestHTTPRoundTripAndErrors`、`TestCommandsListsAllTaskCommands`、`TestTaskCommandsOnFakes`、`TestUsageErrorsExitTwo`、`TestBoardWaitingTasks`。
- **TSK-22** — 單從別的狀態改成 `doing`（新開的不算）時，經 `Deps.Work.AutoOpenWork(id, agent, by)` 請 session 開工作 session；每次 `task.changed` 都經 `Deps.Work.RefreshWork()` 請 session 對帳（SES-27、SES-28）；在事件迴圈裡做，不在寫入當下叫。測試 `TestDoingOpensWorkSession`。
- **TSK-23** — 舊狀態：vault 照收 `review`、`failed`、`shelved`，`decode` 與改單前的 `load` 經 `upgradeLegacy` 轉成新狀態（對照表在 `schemas/task.md`）、拿掉 `site`；API 不回舊值，`List` 用舊值查對到新狀態；寫入收到 `failed` 當 `cancelled`、`review` 回 `CONFLICT`、`shelved` 回 `INVALID`。家的遷移 `2026-11-01-task-statuses`（`MigrateLegacyStatuses`，經 `update.HomeMigrations`）同一轉法改寫 `vault/tasks/`（含 `archive/`），能重跑、退回舊版讀得懂。測試 `TestLegacyStatusesReadAsNew`、`TestDeliveryEndsWhenLeavingOrAsking`、`TestAgentCancelsAskWithReason`、`TestMigrateLegacyStatusesRewritesFiles`。

### reminder（主人待辦）
- **RMD-1** — 新增、修改、延後、取消、完成、改成進行中（`Create`、`Update`、`Snooze`、`Cancel`、`Done`、`Doing`），另有 `Get`、`List`。
- **RMD-2** — 跟進方式 `none`、`remind`、`confirm`、`check`、`discuss`；`none` 時間到只標逾期，不找人。
- **RMD-3** — 週期 `once`、`daily`、`weekly`、`biweekly`、`monthly`；有週期的一定要有 `at`。
- **RMD-4** — `at` 留空套預設：`remind` 事件前 30 分或當天早上（依 `reminder_slot`），`confirm`、`check`、`discuss` 取該 slot 的 `check` 時間；落在安靜時段延到 `quiet_defer_to`；使用者明講的照講的。
- **RMD-5** — 到期通知有三顆一鍵回覆（2026-11-07 DEV-137，不用打字）：「做了」＝完成；「晚點」＝ 3 小時後（落在安靜時段就延到結束）；「今天不做」＝單次的改到明天同一時間、週期的跳到下一次，不給點。按鈕在訊息頁那一則（看板 `canAnswer`；不算「等你回答」，沒回不亮橘點、不進要你同意；手機推播本身沒有按鈕，點開就到那一則）。測試 `TestDueNoticeHasThreeOptions`、`TestLaterAnswer`、`TestNotTodayAnswer`。
- **RMD-6** — `check`：到時間經 `Deps.Sessions.Open` 開專屬對話（`room`，預設 `<owner>-r-<編號>`），送 `routine`「［待辦 R-n］<查證內容>」（冪等鍵 `R-n#check#<到期時間>`）；10 分鐘內沒有對應通知就把原文通知使用者。
- **RMD-7** — `discuss`：通知附「去聊」連結，開專屬對話、秘書先開口。
- **RMD-8** — 沒回不念、不自動擱置（2026-11-07 DEV-137）：等 `reask_minutes`（預設 30 分）沒回，`asks + 1`、狀態回 `todo`、下一次一次性待辦改到隔天同時間、週期待辦改到今天之後的下一次；達 `max_asks`（預設 3）後不再推，留在逾期清單，主人一碰（延後、還在做、改內容或時間、完成）`asks` 歸零。測試 `TestNoReplyNeverShelvesAndStopsPushing`。
- **RMD-9** — 待辦可指定送到哪個對話（`room`）。
- **RMD-10** — 排程工作要使用者回覆時開一則 `confirm` 待辦（`source: routine:<job id>`），1 分鐘內依 RMD-5 送出。
- **RMD-11** — 使用者完成待辦時發 `reminder.changed`（`by` 標來源），並經 `Deps.Journal.AppendJournalOnce` 在負責秘書今天的日誌記「主人完成：<內容>（R-n）」（同天同行一次；寫不進去只記 log）。
- **RMD-12** — 查詢：今天、逾期、這週、之後、要你回答、沒時間、昨天做完，可依負責職位過濾；不給 `query` 時只看 `statuses`（看板的「未完成」「已完成」就是這樣讀，分組在畫面做，WEB-133）。
- **RMD-13** — 主人待辦頁的查詢與頁面註冊 `core.reminders`：看、改、勾掉。
- **RMD-14** — 掛 Goal：跟 TSK-10 同一套規則。測試 `TestReminderGoalLink`、`TestLegacyGroupFieldReadsAsGoal`。
- **RMD-15** — 不讀簡單／完整模式（同 TSK-11），看板一律顯示 Goal 欄（WEB-46）。
- **RMD-16** — 自己登記對外面（同 TSK-21）：`Routes()` 登記 sdk `reminders` 服務；`Commands()` 登記 `reminders add|set|snooze|done|doing|cancel|get|list`；`Board()` 登記首頁 `today_reminders`。測試 `TestRoutesRegisterRemindersService`、`TestHTTPWritesRequireActor`、`TestHTTPRoundTrip`、`TestBoardTodayReminders`、`TestCommandsListsAllReminderCommands`、`TestFlagsFillRequestFields`、`TestTimeFlagsParse`。
- **RMD-17** — 有 `room` 且沒結束的待辦，經 `Deps.Sessions.SetRoomWait(待辦編號, room, reminder)` 讓那個對話顯示「等待辦」（SES-15）；狀態變就重登，`Start` 整批登記。
- **RMD-18** — 首頁的「今天的待辦」（`Board()` 的 `today_reminders`）：前幾天就到期、還沒做完的（`overdue` 裡不在今天的）排在最前面、舊的在前，後面是今天的、依時間；做完、取消的不列（舊看板就是這樣，2026-11-02 稽核補回）。看板首頁的圓圈就是「做完」鈕，逾期的時間標紅；待辦頁（DEV-107 起）「未完成」第一組就是逾期，不再另有「還有 N 件之前沒做的」提示。測試 `TestBoardTodayReminders`、e2e `TestTodayRemindersShowOverdueAndMarkDone`。
- **RMD-19** — 看板上把待辦取消的按鈕（清單「⋯」選單、詳細頁動作列）寫「不做了」，不寫「取消」：手機底部抽屜最下面的「取消」大家會當成「關掉選單」。按了照舊先藏起來、5 秒內可復原（toast「取消了：…」）。e2e `TestReminderCancelAndMenu`。
- **RMD-20** — 快速輸入抓時間（`lib/when.ts`，FEN-15）也認說的是過去的日子：昨天／昨日／昨晚／昨早、前天、大前天。「前天」前面是漢字不算（「查目前天氣」不是時間）。「昨天 3 點 繳費」是昨天 03:00、內容是「繳費」（以前「昨天」不認得，只抓到「3 點」→ 今天或明天 03:00，「昨天」還留在內容裡）；記下來就是逾期的待辦，切到「逾期」那段。vitest `when.spec`「昨天、前天、昨晚：照說的那天」。
- **RMD-21** — （2026-11-03 DEV-107 改版）待辦頁「未完成」依時間分組（逾期、今天、這週、之後、未定時間，WEB-133）；只有「今天」組每列右邊只寫幾點（「08:30」），其他組與平清單照舊寫完整時間（逾期的寫日期與幾點）。以前逾期那段標題與列各寫一次日期。e2e `TestReminderLateGroupShowsDate`。
- **RMD-22** — 快速輸入（今天頁、待辦頁同一個框，`lib/intent.ts`）開頭是「交代」加空白或冒號（「交代 整理照片」「交代：整理照片」）就是交代任務，不是待辦：預覽寫「交代任務」「交給：<秘書>」（秘書照「改開任務單」那顆的選法），Enter 開任務單，標題拿掉「交代」、不抓時間，開好跟「改開任務單」一樣 toast「已開任務單 SEC-n 給…」、可按「看這張單」；不新增待辦。「交代事情的方法」這種沒有空白的照舊是待辦。指令面板不把「交代 …」新增成待辦。vitest `intent.spec`「交代 整理照片」、e2e `TestQuickInputAssignOpensTask`。
- **RMD-23** — 待辦有描述與附件（DEV-104，作者 2026-11-03「關於待辦，我也想要加上描述，然後可以附件」）：描述是 `body`（Markdown），開的時候帶、之後 `reminders.update` 的 `body` 改（給空字串＝清掉、沒給＝不動；CLI `reminders add --body`、`reminders set --body`）。附件跟任務單同一套（TSK-28、D-61，共用 `tasks/attach`）：欄位 `attachments`，開的時候 `attachments`、之後 `add_attachments`／`remove_attachments`，同樣的檢查、結束的待辦 `CONFLICT`。要查證的待辦（`follow: check`）到時間送給秘書的那一則也帶「描述：」與「附件：」。看板：快速輸入（待辦頁）的「更多選項」有「描述」（跟進方式是「到時間找我聊」時叫「想聊什麼」，同一個欄位）與「選檔案」，在輸入框或描述貼上的圖、選的檔都進 `attachments`，不再把圖片行塞進描述；待辦詳情的「描述」照 Markdown 顯示、按「改描述」就地編輯（⌘／Ctrl+Enter 存），舊資料塞在描述裡的話與 `![…](/share/…)` 照樣畫成圖；下面是跟任務單同一個「附件」段。結束的待辦描述唯讀、不能加附件。測試 `reminder/attachments_test.go`（`TestReminderBodyAndAttachments`、`TestReminderAttachmentMustBeSharedFile`、`TestCheckRoutineListsAttachments`）、`TestFlagsFillRequestFields`；vitest `QuickInput.spec` `TestQuickInputTodoDescAndAttachments`；e2e `TestReminderCreateWithDescAndAttachments`、`TestReminderDetailDescAndAttachments`。
- **RMD-24** — 待辦難度與點數（2026-11-07 DEV-136）：`difficulty` 簡單 1、普通 2（預設）、困難 3；完成時核心把點數累加到 `done_points`、`done_at` 記時間；同一筆同一天只給一次（週期待辦每次完成都給），取消、今天不做都不給；沒有 `done_points` 的舊待辦算 0，不追溯。習慣套件讀它換衣櫃點數（HAB-13）。測試 `TestDonePoints`。
- **RMD-26** — 只有兩種跟進集點、過期加點（2026-11-12 DEV-149 作者定）：只有「問我做了沒」（`confirm`）、「秘書幫我確認」（`check`）完成時給點，其他跟進方式 0 點；給的點＝難度點數＋這一次過期每滿一週 1 點（從 `due_at` 算，沒有就從 `at`）。延後、還在做、沒回自動改明天都不重算過期；週期做了或今天不做跳到下一次、主人自己改時間或週期才從頭算（秘書改時間跟延後一樣不重算）。新增或改時間時填了以前的時間（補記忘了的事）照存，但過期從寫下去那一刻算，不能往回填刷點（作者 2026-11-12）。核心在每筆待辦回 `points`（現在按「做了」會拿幾點，結束了或今天給過是 0），看板清單在難度膠囊裡寫「+N 點」（WEB-162）；新增、編輯、詳情頁只有這兩種跟進才出現難度。測試 `TestPointsOnlyConfirmAndOverdue`。
- **RMD-25** — 待辦頁最上面留 slot `core.reminders.top`（2026-11-07）：別的套件可以掛卡片（習慣套件放待辦點數，HAB-18）；沒有套件就什麼都不畫，核心不知道點數怎麼換。

### goal（Goal 與 Planbook）
- **GOL-1** — 秘書用 `Propose` 提 Goal（標題、主人原話、驗收條件），建成 `draft` 並發 `approve` 通知（「確認」「不要」）；確認變 `active`、不要變 `withdrawn`。
- **GOL-2** — `title`、`text`、`criteria` 建好之後誰都不能改；要改就另開。
- **GOL-3** — 秘書只能改 `active` Goal 的 Planbook，每次在 `planbook_log` 追加 `{at, by, note}`。
- **GOL-4** — 只有主人能收回；負責的秘書用 `Done` 回報完成，同時發 `done` 通知給主人。
- **GOL-5** — 已廢：傳話不掛 Goal、核心不附 Goal 原文；`Mentioned`、`Annotation` 拿掉。
- **GOL-6** — `Decide(origin, from, class)` 是純函式：主人的回合 `allow`；秘書傳來的 `allow`，「一律問你」的 `ask`；認不出觸發者只能讀、其他 `deny`，理由只針對這一則。清單是 `settings/goals.toml` 的 `always_ask`（預設 `push`、`delete`、`send_external`、`spend`、`settings`），Start 時載入。
- **GOL-7** — 任務與待辦用 `goal` 掛 Goal（規則在 TSK-10、RMD-14、D-25；含已封存的 Goal）；成員是反查，Goal 檔沒有成員清單。
- **GOL-8** — `done`／`withdrawn` 結束滿 30 天由清道夫封存（schema 的 `ArchiveAt`），封存後 `Get` 與 `goal` 連結仍可解析。
- **GOL-9** — 主人直接開 Goal（D-39）：`Create`（`title`、`text`、`criteria`、選填 `agent`〔名字或職位 id，經 `Deps.Agents` 對成職位 id，名冊沒有 `INVALID`，空的記成 `by`〕）只有 `owner(…)` 能用，其他 `FORBIDDEN` 且什麼都不寫；建好直接 `active`，不發確認通知。舊檔的 `grants` 讀得開、略過。
- **GOL-10** — 自己登記對外面（同 TSK-21）：`Routes()` 登記 sdk `goals` 服務；`Commands()` 登記 `goal propose|show|list|planbook|done|withdraw`；沒有首頁段、健檢、定時工作。測試 `TestRoutesRegisterGoalsService`、`TestHTTPWritesRequireActor`、`TestHTTPWithdrawNeedsOwner`、`TestCommandsListsAllGoalCommands`、`TestGoalCommandsOnFakes`、`TestPlanbookBodySources`、`TestUsageErrorsExitTwo`。
- **GOL-11** — 登記通知來源（`notice.go`，APP-11）：`NoticeSources()` 回 `{goals, goal.changed, waitingNotices}`，列每個 Goal 指到的確認通知、草稿算在等；notify 拿著自己的鎖時叫它，所以只讀 vault、不拿 `m.mu`。測試 `TestNoticeSourceWaitingFollowsDraft`。
- **GOL-12** — 主人開 Goal 指定了負責秘書時，存好後經 `Deps.Mail` 送她一則 `routine`（`from: system`、冪等鍵 `GOAL-n#assigned`）：標題、驗收條件、先寫 Planbook、做完 `goal done`。沒指定不傳；傳不出去只記 WARN。測試 `TestOwnerCreateGoalWithAgent`。
- **GOL-13** — 一定要有驗收條件（法律第 1.4 條）：`Create`、`Propose` 的 `criteria` 空白回 `INVALID`；確認通知與傳給負責秘書的話都附上，並請她自己做到、不派單給別的秘書（法律第 1.2 條）。測試 `TestGoalNeedsCriteriaAndKeepsIt`。
- **GOL-14** — 看板「新增 Goal」（`pages/goals/NewGoal.tsx`）必填沒填就按「建立」：錯誤畫出來後焦點移到第一個沒填的欄並捲進畫面（`lib/focusInvalid.ts`）。手機抽屜表單會捲動，以前人在最下面按建立、錯誤寫在捲出去的標題欄，看起來像按了沒反應。測試 `lib/focusInvalid.spec.ts`。

### job（使用者排程與夜班）
- **SCH-1** — cron 排程，一個工作可有多個 cron；同一分鐘同一工作只觸發一次。
- **SCH-2** — 工作來源是 vault 的 `job` 資料與 reminder 註冊的到期時間；`vault.changed` 後立刻重算。
- **SCH-3** — 觸發時發 `schedule.fire`；有秘書的 `job` 另外送一則 `routine` 封包（`deliver`）：內文讀 `job` 本身（已刪不送）、`{{slot}}` 經 `Deps.Agents.RenderJob` 套好、前加「［排程 <id>］」，冪等鍵 `job:<id>#<觸發時間 RFC3339>`；何時送進對話是 session 的事（SES-10）。`RunJob` 一樣送。
- **SCH-4** — 做完沒有只看封包結局（`envelopeDone`：訂閱 `bus.envelope.state`，只看自己送的、冪等鍵 `job:` 開頭）：`replied` → 發 `schedule.done`；`failed`／`interrupted` → 只記一行 WARN，不發事件、不推主人（法律第 5 條）、不重開對話；還在路上的不說。不呼叫 notify、不發 `system.alert`。
- **SCH-5** — 錯過的時間點：`catchup: once` 在 6 小時內補跑最近一次，其餘略過。
- **SCH-6** — 使用者可以開關工作、改 cron；寫回 `job` 資料，不重啟生效。
- **SCH-7** — 各職位現有的工作（早安、晚安、夜間整理、night-shift…）遷移後都照常觸發。
- **SCH-8** — 夜間整理：每位秘書每晚一次；封包回覆後讀 `job` 的 `action`，是 `rotate` 就經 `Deps.Sessions.Rotate` 請 session 重開收件的那個對話（SES-9）。
- **SCH-9** — 時段型排程：`windows`／`weekend_windows`（`HH:MM-HH:MM`）、`per_day`、`idle_min`；每天從時段挑 `per_day` 段、每段挑一分鐘，同一天結果固定；錯過不補。舊 `sites` 忽略。
- **SCH-10** — 能力模組（`extras`：主動聊天、反思、AI 議題）用同一套機制排。
- **SCH-11** — 只讀本家 vault；`ScheduledOn` 判斷當天有沒有排程（隨選秘書當天視為常駐）。清道夫不快照、不同步。
- **SCH-12** — 自己登記對外面（同 TSK-21）：`Routes()` 登記 sdk `scheduler` 服務（`ListJobs`／`SaveJob`／`SetJob`／`RemoveJob`／`RunJob`）；`Commands()` 登記 `jobs list|add|set|rm|run` 與 `night list|add|done|block|rm`（`cli.go`、`night.go`，範本在 `jobtemplates/*.md`）；沒有 `Board()`。
- **SCH-13** — `timer.Scheduler.Sync("job", …)` 整批換自己的項目，不碰 task／reminder 的 `Cron`／`At`；`system.maintenance`（每晚 03:30，vault 與 bus 清道夫）在 `Start` 登記；每日額度、`schedule.fire`／`schedule.missed` 在交給 `timer` 的 `Handler`。
- **SCH-14** — 管理 API：`job` 的寫入只走這裡、跟載入同一套檢查（`checkJob`），有問題回 `INVALID` 不寫；`SetJob` 改開關、cron、名稱、內文（沒給的不動、空名稱或內文 `INVALID`、版本過期 `CONFLICT`）；`ListJobs` 每筆帶 `next_run` 與最近一次（SCH-16）；`RemoveJob` 自己加的（`source: user`）停用後封存，遷移／套件帶來的只停用；`RunJob` 發 `schedule.fire`、不算每日上限。
- **SCH-15** — 系統級額度工作：`job.action` 可以是 `usage_warmup`（每天暖機）或 `limit_watch`（每 5 分鐘看額度）；沒有 `agent` 的工作觸發時 `schedule.fire` 帶 `action`，由 engine 處理（範本 `usage-warmup`、`limit-watch`）。`backup_sweep`（每週的備份掃描）由 system 塊的 backupsweep 處理（範本 `backup-sweep`，家的遷移 `2026-11-20-backup-sweep-job` 自動放一份，system BSW-1）。
- **SCH-16** — 每個排程記最近一次的結果（`runs.go`，D-48），`ListJobs` 每筆帶 `last_run`、`last_result`、`last_reason`、`runs_today`（從沒跑過的都不帶）：到期送給秘書 → `running`；封包 `replied` → `ok`；`failed`／`interrupted` → `failed`，原因是 session 記在封包上的白話（`interrupted` 寫成「做到一半停了（<原因>）」；沒有原因就「沒送進秘書的對話」／「做到一半停了」）；送不出去（讀不到內文、`bus.Send` 失敗）→ `failed`；錯過不補、過了時段、今天次數用完 → `skipped`＋原因（補跑的那次算跑了）；系統級（沒有 `agent`）只知道觸發 → `fired`。封包的結局只改同一次觸發（冪等鍵裡的時間，比到秒），舊的那次晚到不蓋掉之後那一次。送給秘書時先記 `running` 再送（封包很快結束時結局才對得上這一次）；同一次觸發早就送過、`bus.Send` 回的封包已經結束（冪等鍵相同），直接記那個結局。`runs_today` 是家的時區當天真的送出幾次（手動跑算、略過不算），過午夜歸零。重載時丟掉已經不在的排程的紀錄。存 `runtime/jobs/last-runs.json`（暫存檔＋rename），`Start` 讀回，serve 重開不丟；讀壞了記 WARN 從空的開始。

## NEVER
- **TSK-N1** — 不為排程工作（早安、夜間整理、反思）開任務單。
- **TSK-N2** — 不自己決定通知的送法（只發事件、只傳 `approve`）。
- **TSK-N3** — 不刪檔；結束的由清道夫封存（vault）。主人的「刪除」是封存（TSK-18）。
- **TSK-N4** — 不自動改成不做了、不自動結單；時間到只會列出來。
- **TSK-N5** — 廢除：同 TSK-15。
- **TSK-N6** — 不 import 別的業務塊（depcheck 照路徑擋）；要別塊的能力就在 `Deps` 定義小 interface，由 registry 注入。整塊適用（RMD-N4、GOL-N4 同義）。
- **RMD-N1** — 提醒、確認、討論的到期通知不經過任何秘書（秘書忙或沒開機都不影響）。
- **RMD-N2** — 不自己跳問題框；要使用者回覆一律開 `confirm` 待辦。
- **RMD-N3** — 不 import `session`、`engine`；開對話、登記等待用 `Deps.Sessions`（`Open`、`SetRoomWait`）。
- **RMD-N4** — 見 TSK-N6。
- **GOL-N1** — 秘書的寫入碰不到 Goal 原文：`Propose`、`Create` 以外沒有寫 `title`、`text` 的路。
- **GOL-N2** — `Decide` 不讀檔、不呼叫其他模組、不看時間：同樣輸入永遠同樣結果。
- **GOL-N3** — 不判斷「這一輪是誰觸發的」（session），也不分類動作（引擎套件）。
- **GOL-N4** — 見 TSK-N6。
- **SCH-N1** — 不 import `session`、`engine`（其餘同 TSK-N6）；套 slot、重開對話用 `Deps.Agents`、`Deps.Sessions`。
- **SCH-N2** — 不自己判斷安靜時段（用 `platform.Quiet`）。
- **SCH-N3** — 不自己解析 cron、算時段挑點或補跑：一律用底層 `timer`；這裡只決定到期之後發什麼、受不受額度限制。

## 職責
- 任務單的狀態與身分規則、主人待辦的到期送達與追問、Goal 的確認與 Planbook、授權判斷表、使用者排程的到期處理與每日額度（`budget.max_runs_per_day`）、夜間整理重開對話。

## 不負責
封存時間點 → vault；通知送法與摘要排版 → notify；開 session、這一輪算誰的、封包何時送進對話 → session；cron 與補跑的計算 → `timer`；動作分類 → 引擎套件；畫面 → web；排程與夜間交代的內容 → 秘書。

## 組裝（塊的 Module，S8-3）
`internal/feature/tasks/module.go`（內嵌 `*app.Group`，APP-13）。Deps（塊外）：Vault、Bus、Timer、Agents（名冊、日誌、排程樣板；也當 reminder 的 Journal）、Notify、Clock。塊內沒有注入：task、reminder 掛 Goal 直接用 `goal.CanAttach`（S8-5）。Start 順序 job → task → reminder → goal（都只要底層 vault、bus、timer；reminder 的 Start 叫 `Sessions.SetRoomWait`，session 要建好）。session 建得比較晚：`SetSessions`。存取 `TaskStore()`／`ReminderStore()`／`GoalStore()`／`JobStore()`；指令 `tasks.Commands()`。測試 `module_test.go`。

## 介面
型別直接用 sdk 的 struct（MOD-N5）；每個子套件有 `module.go`、`service.go`、`schema.go`、`events.go`、`http.go`、`cli.go`。
- task：sdk `TasksAPI`；Go 另有 `Reconcile`、`DueNudges`、`MarkNudged`、`AddNudge`、`Archive`、`Brief`、`SetWork`、`MigrateLegacyStatuses`。`Deps`：`Scheduler`（`Cron(ctx, id, specs, timer.CronOpt)`，`timer` 直接實作）、`Agents`、`Notify`（`Send`、`List`、`Retire`：收掉同一句問句的其他則，TSK-30）、`Bus`、`Vault`、`Clock`、`Work`（`AutoOpenWork`、`RefreshWork`）。
- reminder：sdk `RemindersAPI`（`Snooze` 收 `3h`／`tomorrow`／`tomorrow-same`／`30m`／絕對時間）。`Deps`：`Agents`、`Notify`、`Scheduler`、`Bus`、`Vault`、`Clock`、`Sessions`（`Open`、`SetRoomWait`）、`Journal`。
- goal：sdk `GoalsAPI`（`Propose`、`Create`、`Get`、`List`、`UpdatePlanbook`、`Done`、`Withdraw`）；Go 另有 `Decide`（`policy.go`）、`NoticeSources()`；套件函式 `Read`、`CanAttach`（task、reminder 用）。`Deps`：`Vault`、`Notify`、`Bus`、`Clock`、`Agents`、`Mail`。
- job：sdk `SchedulerAPI`；Go `Spec()`、`Problems()`、`ScheduledOn(ctx, agent, day)`、`Checks()`（`schedule.jobs`：有排不上的就 error 附修法）、`AgentJobIDs`／`ArchiveAgentJob`／`MoveAgentJob`（agents 塊經自己的 `Deps.Jobs` 呼叫）。`Deps`：`Bus`、`Vault`、`Timer`（`Cron`、`Next`、`Location`、`Sync`、`SetHandler`）、`Clock`、`Agents`（`RenderJob`）、`Sessions`（`Rotate`）。

## 事件
- 發出：`task.changed`（`{id, status, from, agent, by}`）、`task.waiting_user`（`{id, agent, ask}`）、`task.review`（`{id, agent, proposed}`，送驗收時）、`task.rejected`（`{id, agent}`）；`reminder.changed`（`{id, status, from, owner, room, by}`）；`goal.changed`（`{id, state, by}`）；`schedule.fire`（`{id, agent, kind: job|cron|at, at, action}`）、`schedule.missed`（`{id, at, caught_up}`）、`schedule.done`（`{id}`，只有整合測試在聽）。
- 送出封包（`bus.Send`）：RMD-6、GOL-12、SCH-3。
- 訂閱：`schedule.fire`（task 的 `tasks.nudge`、reminder 的 `kind: at`）、`notice.answered`（task：最後一則答完回進行中、驗收／退回、收掉同一句的其他則，DEV-26、TSK-25、TSK-30；reminder；goal 只認主人對 draft 的確認；auto 都忽略）、`notice.sent`（reminder 追問）、`task.changed`（TSK-22）、`vault.changed`（job）、`bus.envelope.state`（SCH-4）。

## 儲存
`schemas/task.md`、`schemas/reminder.md`、`schemas/goal.md`、`schemas/job.md`；跟進天數讀 `settings/notify.toml` 的 `[tasks]`（`schemas/notify-prefs.md`）；`settings/goals.toml`（`always_ask`）；排程最近一次的結果 `runtime/jobs/last-runs.json`（SCH-16，執行期狀態、不進 vault）。

## 依賴
`timer`、`vault`、`platform`、`app`（登記介面與 `cmdline`，import）。同塊：task、reminder import `tasks/goal`（只用 `CanAttach`、`Read`）與 `tasks/attach`（附件的檢查與增減，D-61；它只 import `vault`、`platform`），其他子套件彼此不 import。名冊、通知、訊息匯流排、session、引擎都不 import：各子套件在自己的 `Deps` 定義小 interface，由 `cmd/evergenie/registry.go` 接上。

## 錯誤
都以 `ERROR_CODE_*` 回，各條 MUST 寫了什麼時候回：`INVALID`（缺必填、未知值、不合格的 cron／時段／內文、想改 Goal 原文）、`NOT_FOUND`、`CONFLICT`（不合法轉移、已結束再改、狀態不對的驗收／送驗收／Planbook、版本過期）、`FORBIDDEN`（秘書做只有主人能做的事、掛到不存在的 Goal）。

## 不變條件
- 結束的 `type: task` 都有 `accepted_by`、`accepted_at`、`closed_at`；`done` 的還有 `result`、`verify`、`verify_how`。
- `Stale`、`Reconcile` 前後檔案位元組不變；`doing` 2 天 23 時 59 分不在 `Stale`、3 天 1 分在（`DueNudges` 同界線）。
- 進入 `waiting_user` 恰好一則事件；離開後 `waiting_since`、`proposed` 為空；已結束不能改回 `doing`；遷移再跑一次位元組不變。
- 待辦預設時間：work slot `confirm` 15:00、life 18:00，落在 22:00–07:01 一律 08:30；`weekly` 完成後 `at` 前進一週、回 `todo`；「之後」是沒結束、有時間、不早於明天 00:00 的。
- `Decide` 對「三種觸發 × 八類動作」每格跟 D-24 一致；Goal 結束第 29 天不封存、滿 30 天封存。
- `* * * * *` 快轉 10 分鐘恰好 10 次；睡 3 小時 `catchup: once` 補一次、`skip` 不補，超過 6 小時不補；壞的 `job` 只略過那筆（cron 改壞時維持舊排程）。
- job 的鎖與 `timer.Scheduler` 的鎖只有 job → timer 一個方向（`Handler` 在 Scheduler 鎖外被呼叫）。

## 測試計畫
- task（`task/*_test.go`，名稱就是守的行為）：例 `TestTransitionTable`、`TestSubmitRequiresResultVerifyHowProposed`、`TestStaleAndReconcileDoNotWrite`、`TestDueNudgesEveryThreeDaysMaxThree`、`TestAgentClosesTaskWithOwnerCriteria`、`TestAgentSelfCloseOnlyFromDoingOrWaitingExternal`、`TestTaskGoalLink`、`TestTaskWorkSessionFields`、`TestAddNudgeCountsWithoutFollowCadence`、`TestAttachmentsOnCreateAndUpdate`、`TestBriefListsAttachments`。
- 整塊：`module_test.go`（假物件組整塊、子模組順序、Start／Stop 一輪、指令合併）。
- reminder：`TestCreateUpdateSnoozeDoneCancelDoing`、`TestFollowTypes`、`TestCycleRequiresAt`、`TestDefaultTimeRules`、`TestDueSendsWithoutSession`、`TestButtonAnswersApply`、`TestCheckOpensRoomThenFallsBackAfter10Min`、`TestDiscussHasChatLink`、`TestNoReplyDelaysThenShelvesAfterThree`、`TestRoomRouting`、`TestRoutineConfirmSource`、`TestDoneEventCarriesActor`、`TestQueries`、`TestReminderPageQueries`、`TestReminderBodyAndAttachments`、`TestCheckRoutineListsAttachments`。
- goal：`TestProposeSendsApprove`、`TestConfirmActivates`、`TestDeclineWithdraws`、`TestTextImmutable`、`TestPlanbookLogged`、`TestOnlyOwnerWithdraws`、`TestOwnerCreatesGoalActive`、`TestAgentCannotCreateGoal`、`TestDecideTable`、`TestConfiguredAlwaysAskDrivesDecide`、`TestGoalArchivedAfter30DaysEnded`、`TestLegacyGoalWithGrantsReads`；vault 的 `TestGoalPathRecognized`；sdk 的 `TestLegacyGoalGrantsDecode`；`internal/base/app/board` 的 `TestBoardAllowsGoalCreate`。
- job：`TestCronFiresOncePerMinute`、`TestMultipleCrons`、`TestFireNeverCallsSession`、`TestCatchupOnce`、`TestCatchupSkip`、`TestCatchupTooOld`、`TestDisableJobStopsFiring`、`TestChangeCronNoRestart`、`TestNightlyRotateAction`、`TestLegacySitesNeverFilterLocalJobs`、`TestWindowJobFires`、`TestWindowJobMissedAfterWindowEnds`、`TestWindowJobBadWindowIsProblem`、`TestBudgetMaxRunsPerDay`、`TestProblemsListsUnloadableJobs`、`TestChecksReportUnloadableJobs`、`TestNextRunsMatchesService`；`deliver_test.go` → `TestDueJobSendsRoutineToAgentOnce`、`TestRemovedJobIsNotSent`、`TestSystemJobSendsNoEnvelope`、`TestRepliedJobPublishesDoneAndRotates`、`TestUnfinishedJobOnlyLogsWarning`、`TestOtherEnvelopesIgnored`；`TestJobWithoutOutcomeNeverAlerts`；`runs_test.go` → `TestListJobsShowsLastRunAndRunsToday`、`TestFailedJobRunCarriesReason`、`TestOlderEnvelopeOutcomeDoesNotOverwriteLatestRun`、`TestSkippedJobRunsAreRecordedNotCounted`、`TestCaughtUpRunIsNotSkipped`、`TestSystemJobFiredAndUndeliverableJobFailed`、`TestRunJobIsRecorded`、`TestLastRunSurvivesRestartAndRunsTodayResetsAtMidnight`；管理 API `TestJobsCLIRoundTrip`、`TestHTTPRoundTripAndErrors`；`cmd/evergenie` 的 `TestJobsCLIOnFixtureHome` 與整合測試（觸發 → 回覆 → `schedule.done`）。

## 設計決定
- **D-22** — 法律第 1.4 條的做法：①一般任務秘書做完只能送「要你驗收」（附結果、驗證、驗證方式、建議結局），只有主人能通過或退回；問答類秘書可以自己結；有主人驗收條件的單，負責的秘書照條件驗過可以自己結（2026-11-01，TSK-14）。②不自動擱置：秘書每晚交代手上的進行中；超過 3 天沒動的進主人早上的「停住的單」；不做了只由主人決定（問答類除外）。③等你進入時推一次，之後每天早上摘要列出，超過 3 天再推、每 3 天一次、最多 3 次（預設 3／3／3）。否決：秘書自己勾已驗證就結；問答也要驗收。
- **D-23** — 法律第 4 條的做法：主人說出目的，秘書提成 Goal，主人確認一次，之後秘書為它接力、不再問。原文由核心保管、確認後誰都不能改，只有主人能收回；秘書只能改 Planbook（每次留紀錄）；Goal 不寫誰做什麼。否決：Goal 由秘書轉述、寫在 Planbook（實測會漏掉主人授權的 push）。
- **D-24** — 「誰觸發的」的做法：每一輪開始，核心判斷這一輪算主人的（打字、看板、按鈕、排程、待辦）還是某位秘書的（傳話）。秘書每個動作前，引擎把動作分類（讀、寫、commit、push、刪除、對外發訊息、花錢、改設定）問核心：主人的回合與秘書傳來的話照常（傳話等於打字），但「一律問你」的 push、刪除、對外發訊息、花錢、改設定要問主人；放行時引擎不輸出東西。否決：只靠角色說明預先授權；秘書 session 不開 auto 模式。
- **D-25** — 任務與待辦的選填 `goal`：一筆最多一個、不巢狀、可跨秘書；只要求 Goal 存在（任何狀態），不用主人確認；task、reminder 經同塊的 `goal.CanAttach` 直接讀。看板一律顯示 Goal。否決：任務群組與 Goal 並存；掛 Goal 要主人確認。
- **D-39** — 主人在看板直接開的 Goal 直接 `active`，不跑確認通知（主人打的就是原話）；秘書提案流程（`draft` → 確認）不變；只有 `owner(…)` 能用 `goals.create`。
- **D-42** — 任務單狀態收成六種（作者 2026-11-01）：要你驗收併進等你（`proposed` 有值）、想法併進待做（值沿用 `idea`）、無法完成／擱置／取消併進不做了（原因寫 `result`）；舊資料讀取時轉、家的遷移改寫（TSK-23）。否決：新開 `todo`、`review` 等新值（退回舊版讀不懂）；保留 `review` 當第七種。
- **D-48** — 排程最近一次的結果（2026-11-02，作者在手機上看不出早安簡報今天跑了沒）：排程只報自己看得到的事實——送出了（`running`）、封包回覆了（`ok`）、封包沒做完或送不出去（`failed`＋原因）、這次略過（`skipped`＋原因）、系統級只知道觸發了（`fired`），不猜秘書做得好不好。只留每個排程最新一筆＋當天次數，存 `runtime/jobs/last-runs.json`（D-8：高頻狀態只留最新、不進 vault），serve 重開不丟。沒跑成只在排程頁（清單那一列、詳情「最近一次」）用紅字寫原因，**不**進今天的「等你處理」、不推播：SCH-4 與法律第 5 條（自己處理掉的錯誤、自動事件不傳），主人打開排程頁就看得到。否決：寫回 vault 的 job 檔（每次觸發都改正本、又觸發 `vault.changed` 重載）；每次從 bus 佇列的封包現算（清道夫會清掉封包，算不出「今天幾次」）；沒跑成發通知或進「等你處理」（違反 SCH-4；要改得先改法律第 5 條的解讀，由作者定）。
- **D-57** — 要你回答的任務問句只收打字回答（2026-11-03 QA）：單上秘書在問你時，通知不放按鈕，只給打字回答與「去聊」（`Notification.typed`，notify 不補「好／不要」）。理由：問句是秘書自由寫的，按鈕只對是非題有意義；答案要送回秘書，打字最不會誤會。否決：①讓秘書開單時給選項（要改 `tasks set` 的參數、共用規則與每位秘書的習慣，秘書忘了給又回到「好／不要」）；②看板看 `task` 有值就不畫按鈕（伺服器已經補了「好／不要」，看板分不出秘書是不是真的要是非題，也分不出舊的「請驗收」通知）。秘書要是非題就自己 `evergenie notify` 發帶選項的那則。
- **D-61** — 任務單與待辦的附件是 `/share/<名字>` 連結的清單（DEV-104，2026-11-03，TSK-28、RMD-23）：檔案留在家的 `runtime/share/`（看板上傳、`evergenie share` 本來就放這裡、`/share/` 路由本來就會送、秘書 session 本來就讀得到〔ECL-8、AGT-14〕），單上只記連結；檢查只看檔案在不在，不搬、不複製、不刪。完整網址只取路徑、不比對主機：存的一律是這台的相對連結，而且這台的 share 資料夾裡要真的有那個檔，別台的網址對不到檔就被擋（`web.public_url` 可能沒設、tailnet 名字也會換，比主機反而會擋掉對的網址）。理由：聊天、訊息、待辦、任務單同一種附件，秘書不用學新的路徑，看板與秘書都不用新的上傳或下載路由；別台照 FEN-28 在那台再傳一次。附件放在家的附件資料夾：第 1 版佈局是 `runtime/share/`，第 2 版是 `media/share/`（schema home「佈局」）；第 1 版的家附件還在 `runtime/` 底下，照法律第 2 條（`runtime/` 放執行期的東西、不算資料、可以刪）算可以刪的東西。兩版的連結格式一樣。否決：①複製進 `vault/tasks/_files/SEC-<n>/`（舊 schema 寫的，從沒實作：要新的上傳 API、下載路由、秘書讀取規則，同一個檔在聊天與單上存兩份）；②附件只寫進內文 Markdown（秘書與看板都得從自由文字裡解析，拿掉要改內文；待辦的快速輸入以前就是這樣）；③每個附件存名稱、大小、型別的物件（檔名已經在連結裡，大小與型別看檔案就知道，多一份會跟檔案不一致）。

## 已知限制
- 本機 API 沒有身分驗證：秘書可以冒用 `owner(…)` 確認或收回 Goal；擋的是模型判斷錯，不是惡意冒用（D-24）。
- 主人在看板開的 Goal 沒有確認通知，所以 schema 的 `notice` 只在 `draft` 必填。
- 排程的「結果」只到封包為止：秘書回覆了算 `ok`，回覆裡說「做不到」也一樣是 `ok`；系統級排程（額度暖機、額度開通通知）的成敗在 engine，這裡只記 `fired`（SCH-16）。
- `running` 沒有對帳：封包一直沒有結局（引擎沒回報）就一直寫「送出，還在做」；看板每 30 秒重讀，封包結束後最多 30 秒才變（SCH-16）。
- 看板判斷「今天」用瀏覽器的時區，`runs_today` 用家的時區；主人的手機與家同時區，不同時區時可能差一天。
- 「最近一次」只留一筆：之前幾次的結果、哪幾次略過，要看 `runtime/logs` 的 `jobs` log。
- SCH-7 的遷移工具已刪（留在舊系統搬家工具刪掉前最後一版的 tag，不公開）。
- 附件（TSK-28、RMD-23）：加、拿掉附件也會更新 `updated_at`（停住的單會算有動靜）；工作 session 已經開著時加的附件不另外通知秘書，要她看就在專屬對話說一聲或她自己 `tasks get`；`runtime/share/` 不進家的版本庫、也沒有自動清理，檔案跟聊天附件一樣靠機器備份；別台還是舊版時，開在那台的單或待辦帶的附件會被忽略（舊版不認這個欄位）。
