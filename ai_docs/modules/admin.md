# 模組：admin（官方選用套件 `packages/official/admin`）
Status: DRAFT — 設計 2026-10-23（P0-S7，簡版），pending author review.

**目的：** 官方發行者的工具：發版、官方內容製作、相容檢查、深度除錯。判斷規則是「拿掉它，使用者的家照樣完整運作」（D-12）。
**種類：** 官方選用套件（附程式＋看板頁面；開發 EverGenie 的家才啟用。2026-10-31 起 `/admin/` 網站、DEV 單、`dev` 服務、發行者旗標都拿掉，計畫 `2026-10-31-dev-workshop-split`）

## MUST
- **ADM-1** — 發版（nightly／safe／release）、版號、release notes。
- **ADM-2** — 官方內容製作：角色、衣服（設計中的草稿）、圖庫、生圖技能、出角色包。
- **ADM-3** — 廢除（2026-10-31 工坊拆分）：`/admin/` 網站拿掉；admin 只剩看板裡的兩個套件頁面（ADM-27）。
- **ADM-4** — 隱藏版不進角色包（角色包只收 `[admin] public_personas`，PRS-4）。
- **ADM-5** — 發版前的相容檢查：fixture 家跑新核心，官方套件都要通過新協定。
- **ADM-6** — 深度除錯頁：原始佇列、引擎事件 log、分身連線；一般使用者只在設定看簡單的健康狀態。
- **ADM-7** — 廢除（2026-10-31 工坊拆分）：`/admin/` 的外殼與導覽拿掉；開發用的家直接用看板。
- **ADM-8** — 廢除（2026-11-01，法律第 7 條：每台一個家、兩家不連線；restructure-s10）：開發家只看同一台，`[admin] dev_site` 不再讀（舊的 home.toml 留著也沒作用），`admin.devhome` 沒有 `site` 欄位、`state` 沒有 `offline`，「開發家」頁不列「在哪台」。舊的 `admin.devhome` 帶 `offline`／`site` 時頁面當沒設定，程式下一輪就蓋掉。
- **ADM-9** — 部署工具（發版流程缺口 4、D-35；`scripts/deploy.sh`，用法見 `runbooks/release.md` 步驟 5、6）：`<mini|office|all>`（在 office 跑，mini 先、office 後）遠端 `git fetch`＋`merge --ff-only origin/main` → 舊 `bin/` 留成 `bin.prev/` → `make build` → `evergenie service restart` → 檢查（doctor 沒有紅字且核心版本＝這次的 `git describe`、看板首頁 200 且 bundle 跟 dist 一致、`packages list` 有套件且沒有載入失敗的，停用中的套件帶著原因不算；三項一起重試到全過，最多 `EVERGENIE_DEPLOY_TRIES` 秒，免得重開時舊程式還在回應而誤判）→ 成功記下 `bin/.deployed-version`。已是最新版不重做（不蓋 `bin.prev/`）；repo 有沒提交的改動、不能快轉就不動 `bin/`；`all` 在 mini 沒過時不碰 office；`--dry-run` 只印指令、不 ssh、不執行。
- **ADM-10** — 部署的退路：編譯失敗、重開失敗或檢查沒過，自動把 `bin.prev/` 換回 `bin/`（壞的留 `bin.failed/`）、重開服務、印白話原因、結束碼 1，換不回去結束碼 3；`--rollback <機器>` 手動換回並再檢查一次。驗收 `scripts/deploy-test.sh`（`make deploy-test`，假的 ssh／evergenie／make／curl，不碰真的機器）；沒有會真的部署的 make 目標。
- **ADM-11** — 廢除（2026-10-31 工坊拆分）：`/admin/` 的聊天拿掉；看板的聊天本來就有切電腦的分頁。
- **ADM-12** — 廢除（2026-10-31 工坊拆分）：`dev.set_account` 拿掉；開發用帳號就是 admin 秘書的帳號，在看板「設定 → 秘書」改（通用設定，不另做 API）。
- **ADM-13** — 廢除（2026-10-31 工坊拆分）：DEV 單拿掉；開發任務是開發用的家（工坊家）的一般任務單，驗收照一般任務單。
- **ADM-14** — 廢除（2026-10-31 工坊拆分）：DEV 單拿掉；一般任務單的頁面照看板的做法。
- **ADM-15** — 開發家連結：`admin.devhome` 有 `url`（開發家 `home.toml [runtime] url`，只收 http／https）時「開發家」頁多一列「網址」（新分頁開）。
- **ADM-16** — 廢除（2026-10-31 工坊拆分）：DEV 單的表單拿掉。
- **ADM-17** — 廢除（2026-10-31 工坊拆分）：`/admin/` 網站拿掉。
- **ADM-18** — 廢除（2026-10-31 工坊拆分）：`/admin/` 的秘書頁拿掉；用看板的秘書頁。
- **ADM-19** — 廢除（2026-10-31 工坊拆分）：`/admin/` 的除錯頁拿掉；電腦、健康檢查在看板的設定頁。
- **ADM-20** — 廢除（2026-10-31 工坊拆分）：DEV 清單、單頁、總覽拿掉；用看板的任務單。
- **ADM-21** — 發版頁（ADM-27 的「發版」）：版本卡（`admin.release`：這台跑的版本〔`version`；跨台時代的舊資料只有 `sites` 時取第一個有版本的那台〕；main 最新；`detail` 有值時顯示）、還沒部署的改動（`pending_more` 時「還有更多」）、「部署到 mini 和 office」按鈕（有改動沒部署才出現）——先在畫面內確認，再開一張一般任務單給 admin 秘書（`agent = admin`，開工方式見 ADM-27；標題「部署 <short> 到 mini、office」，內文含版本與每個 pending commit）；admin 秘書已有沒結束、標題同開頭的單時換成連到那張。
- **ADM-22** — 正式發版工具（P8 第二輪 S1，REQ-277、REQ-278、REQ-132）：版號從 git tag 來（`scripts/version.sh`＝`git describe --tags --match 'v[0-9]*' --always --dirty` 去掉開頭的 `v`；`make build` 與 `scripts/deploy.sh` 都叫它，safe tag 不算版號）。`scripts/release.sh nightly|prepare|stable|status`：nightly 全新 clone `origin/main`、依 `EVERGENIE_RELEASE_STEPS` 跑全部測試，全過才在發行 repo 打 annotated `safe/<YYYY-MM-DD>`（同一天第二個 `.2`），同一個 commit 測過就不重跑（`--force` 例外）；prepare 依最新 safe 之後的 commit 建議版號（沒有上一個正式版 `0.1.0`、有 `feat` 就 minor+1、否則 patch+1，major 不自動動）並寫發版說明草稿；stable 把最新 safe 升格成 `vX.Y.Z`（版號要比上一版大、tag 還沒有、safe 的 commit 在 `origin/main` 上；不加 commit、不改檔案，tag 說明＝發版說明）；`--push` 才推 tag、`--dry-run` 什麼都不做。`dev.release` 多回上一個正式版、最新 safe、上次每晚測試、下一版草稿（讀 `[admin] release_state`，預設 `~/.evergenie-release`）。維護者自己的家照舊用 `scripts/deploy.sh` 部署 main（`runbooks/release.md`）。
- **ADM-23** — 發版頁的「正式版」區：上一個正式版、最新測試過的版本（和上一個正式版同 commit 寫「同一版」）、上次每晚測試（沒過的步驟紅字）；「跑發版測試」開「發版測試（每晚測試）」的任務單；有比上一個正式版新的測試過版本與 `next` 草稿時顯示版號（x.y.z 而且比上一版大）與發版說明（不能空）的表單，確認後開「發正式版 x.y.z」的任務單（內文含版號、測試過的版本與說明全文）。都開給 admin 秘書並開工（ADM-27），同一種沒結束的單已經有了就連到那張。
- **ADM-24** — 相容檢查與發版整套測試（P8 第二輪 S5，REQ-284、REQ-285、REQ-136）：`make compat`（`scripts/compat.sh`，先 `make build`）——sdk 的 `api.golden` 對上一個正式版只加不刪（沒有正式版 tag 就跳過並說明）、fixture 家與「上一個正式版建的家＋這一版 `migrate-home`」都要 serve 得起來（doctor 無紅字、官方套件全部載入）；`make release-e2e`（`scripts/release-e2e.sh`）——暫存資料夾、真的 git／`make build`／`serve`（`EVERGENIE_SERVICE_MODE=process`），依序走：建角色包、假 origin 三個 tag（`v0.0.3` 編得過但 `serve` 一進來就 `os.Exit(3)`）、一鍵安裝、檢查新版、升到指定版、壞版自動換回、手動退回、角色包沒網路與不退版、套件從網址裝與更新、`nightly`／`prepare`／`stable`（不推）、`make compat`、看板用的 `system.update_check`／`update_apply`；每段印 ✔／✘ 與秒數，沒過停在那一段、結束 1；暫存資料夾要短（unix socket 路徑上限），結束一定收掉起的 serve，不碰 launchd、tmux、正式家、不推任何東西。兩者都不隨 `make ci` 跑（`TestCompatAndReleaseE2EScriptsUsage`、`TestCompatScriptSDKAdditiveOnly` 只檢查語法、`--help` 與 sdk 比對邏輯），發版前與每晚測試的 `EVERGENIE_RELEASE_STEPS` 預設包含。2026-11-13 起（DEV-155）`make release-e2e` 接著跑下載版的 `dist-test`、`upgrade-download-test`、`install-test`、`clean-install-test` 與 `package-go-test`（最後這支**要網路**：真的從 go.dev 下載 Go 一次，packages PKG-66）。

- **ADM-25** — admin 官方套件（計畫 `2026-10-31-admin-out-of-core` A4／S4，2026-10-31 併進 計畫 `2026-10-31-dev-workshop-split`；`packages/official/admin`）：附程式（`kind = "program"`、`optional = true`，預設不啟用），程式只 import `sdk`。資料種類 `admin.release`（`ids = given`，只用 `current`：本機版本 `version`（2026-11-01 取代跨台時代每台一列的 `sites`，法律第 7 條；舊資料帶 `sites` 照樣讀，下一輪整筆換掉）、`main`、`pending`／`pending_more`、`latest`、`safe`、`nightly`、`next`、`detail`、`computed_at`，其餘形狀同舊的 `dev.release`）、`admin.devhome`（`ids = given`，只用 `current`：`state` unset／stopped／running、`path`、`version`、`board_port`、`detail`、`url`、`computed_at`，同舊的 `dev.status` 的 `home`）、`admin.request`（`ids = seq`、`R-`：`type = refresh`、`target` all／release／devhome、`state` pending → done／rejected）。
- **ADM-26** — admin 程式什麼時候算、算什麼：開機先處理積著的請求再算一輪，之後每 5 分鐘算一輪（git fetch 最多這麼頻繁，fetch 時限 15 秒、其他 git 10 秒）並封存處理完超過 7 天的請求；`admin.request` 有變就去抖 1 秒、合併 pending 請求的 `target` 算一次，寫好資料後才把請求標 done，看不懂的 `type` 標 rejected 並寫 `reason`。設定讀家的 `home.toml [admin]`（`repo`、`release_state`、`dev_home`；路徑 `~` 展開；讀不到用預設）。發版狀態算這台；開發家只探測本機（`<dev_home>/runtime/sockets/api.sock` 的 `system.doctor` 1 秒內回應＝running，網址只收 http／https）。
- **ADM-27** — admin 的看板頁面（計畫 `2026-10-31-dev-workshop-split` S2，`packages/official/admin/pages/`，做法同衣櫃）：`[[contributes.page]]` 兩頁「發版」（`release.tsx`，ADM-21、ADM-23）與「開發家」（`devhome.tsx`，ADM-8、ADM-15），`[[contributes.menu]]` 放「工作」群組。頁面只讀 `admin.release`／`admin.devhome` 的 `current`（15 秒重讀），「重新檢查」寫一張 `admin.request`（`refresh`）等程式回填 done；開單並開工（DEV-124）：確認後 `tasks.create` 開成 `idea` 再 `tasks.update` 改 `doing`，才會開工作 session 把單送給秘書（SES-27）；改狀態失敗時單已開好，畫面改連到那張單並說「還沒開工」，不留確認框（免得開出重複單）。找沒結束的單用 `tasks.list`、admin 秘書在不在用 `web.get_roster`（沒有這位就說去「設定 → 秘書」開 admin 職位）。頁面不跑 git、不讀 repo、不新增 API。畫面上的時間（算好的時間、正式版、測試過的版本、每晚測試）寫成「11 月 3 日 02:10」（`whenText`，不是今年的多寫年），不直接印程式寫的 RFC3339 字串（2026-11-03 補）。讀不到 main 時「還沒部署的改動」寫「算不出…原因在上面的說明」（`pendingNote`），不重複上一列的「讀不到 main 的最新版本」（2026-11-03 補）。開單前的確認句是白話：英文開頭的秘書名字前補空白（「給 Helper，」，`nameAfterCJK`）、不放程式碼的反引號（2026-11-03 補）。頁名只由外殼頁首寫，頁面不再放同名的 h1；開發家開著時不多一列「說明：開發家開著」（`homeDetail`，2026-11-03 補）。
- **ADM-28** — 發版頁「還沒有測過的版本」的提示指向按鈕的實際位置與按鈕上的字（「先按上面的『跑發版測試』」），不寫「下面的按鈕」（按鈕排在提示上面）。
- **ADM-29** — 「重新檢查」等 30 秒程式都沒把請求標 done／rejected 時，鈕下方說「admin 的程式 30 秒內沒有回應…先顯示上次算好的」（`waitRequest` 回 `timeout`），不默默當成檢查完。
- **ADM-30** — 開單鈕（部署、發版測試、發正式版）按下開成單後，在「沒結束的單」清單列到它之前照樣顯示「已經有一張在跑了」（不讓按鈕閃回來被按第二次）；清單列過之後就以清單為準，那張單結束了（不在清單、或狀態不再是沒結束的）按鈕就回來，不用重新整理頁面（`trackOpened`）；一直沒列到（單在列到前就結束、或沒拿到單號）時重讀 3 次就放掉，不會永遠卡在「在跑」。
- **ADM-31** — 讀不到這台跑的版本（本機健康檢查問不到）時，`admin.release` 的 `detail` 寫「讀不到這台跑的版本…沒辦法算還沒部署的 commit」，不拿空字串去 repo 找、也不寫成「本機的版本  在 repo 裡找不到」；main 最新照樣讀。
- **ADM-32** — 「發版」「開發家」頁的一列（`Row`）：值短時靠右跟標籤同一行；換到下一行或長到多行時整段靠左（`justify-between`＋值 `text-left`，不用 `ml-auto`：那會把換到第二行的短值又推到右邊），不一行行靠右對齊。（2026-11-03，e2e `admin.spec.ts` 發版頁）
- **ADM-33** — 開發家的卡片標題寫「這台的開發家」，不寫「狀態」（第一列就是「狀態」，重複）。（2026-11-03，vitest `TestDevHomeCardTitleIsNotState`、e2e `admin.spec.ts`）
- **ADM-35** — `scripts/deploy.sh --dry-run` 說清楚會部署什麼：本機看到的 origin/main 是哪個 commit、多久前更新過（看 origin/main 的 reflog，各 worktree 共用；不看每個 worktree 各一份、fetch 別的分支也會變的 FETCH_HEAD；沒有 reflog 就不說）；本機那台（office）上次部署的版本（`bin/.deployed-version`）到 origin/main 差幾個 commit、列出最新 15 個；上次部署的就是 origin/main 時說「一樣、不會重做」，本機 git 找不到那個版本就照實說算不出來，讀不到檔就說不知道；上次部署的比本機的 origin/main 還新就警告本機可能沒 fetch。repo 位置 `$HOME/` 或 `~/` 開頭都認得。別台（mini）要 ssh 才看得到，dry-run 只說看不到；開發家（workshop、mini-dev）不記上次部署的版本（ADM-38），不比；`--rollback` 不說。全部只讀本機 git 與檔案，照樣不 ssh、不連網路、不 fetch。（2026-11-03 設計、2026-11-16 DEV-153 照現在的部署工具重寫；`scripts/deploy-test.sh` 第 20 段）
- **ADM-36** — `scripts/release.sh prepare` 的版號與草稿看得懂這個 repo 的 commit（多半寫中文、不寫 `feat:`／`fix:`）：只認標準類型前綴（`feat`、`fix`、`docs`、`chore`…，大小寫不拘；「`notify: …`」「`dev-111: …`」照沒寫前綴算）；沒寫前綴的，以「修正／修好／修」（不含「修改」）開頭（含去掉開頭的單號或模組名後）、或寫了「review 修」的算修正，其餘算改動。0.x 有新增或改動就 minor+1，只有修正與維護才 patch+1；1.0 起只有 `feat` 才 minor+1。草稿涵蓋上一個正式版到最新 safe 的全部 commit（不限條數、不含 merge）；**只**重新打包看板 dist 的整條不列，其他 commit 結尾的「；重建看板 dist」只剪掉那段、那條照列；開頭寫幾個 commit、請改寫成白話，`next.json` 記 `draft = true`。nightly 過了才串 prepare，prepare 失敗不讓已打 safe 的 nightly 變失敗。（DEV-113，2026-11-04，`make release-test` 的 prepare 段）
- **ADM-37** — `scripts/release.sh notes --notes <檔>` 只換 `next.json` 的發版說明（`draft` 改 false），版號、safe 不動；草稿的 `safe_tag` 不是最新 safe、沒有草稿、檔案不在、空的或只有空白、帶了 `--version`／`--push`／`--force` 都拒絕（結束 1）。`stable` 用還沒改寫的草稿發時，拿掉開頭「（草稿：…）」那行，不進 tag 說明；`status` 說草稿是原樣還是已改寫。「跑發版測試」的單請 admin 秘書過了之後把草稿改寫成白話並用它寫回；發版頁的表單在草稿換了（`safe_tag`、版號或說明變了）時重新帶入，草稿還是原樣時提示「admin 會改寫成白話」。發版頁的提示只講存在的東西：上面有「跑發版測試」鈕（有 admin 秘書）才說「按上面的」，名冊還沒讀到或沒有秘書時不說；也不說「每晚會自己跑」（排程沒裝）。（DEV-113，`make release-test` 的 notes、stable 段，vitest `TestAdminHintsOnlyPromiseWhatExists`、`TestAdminReleaseReadsNextDraft`、`TestAdminNightlyBodyAsksPlainNotes`）
- **ADM-38** — 部署工具換開發家（DEV-157，D-66；`scripts/deploy.sh` → `devhome_one`，位置集中在 `devhome_conf`；做法與每個開發家的差別見 `runbooks/homes.md` 第 6 節）：開發家跑開發用 repo 的 `bin/`、不在家清單裡，另外換，換版用跟正式家同一套（留 `bin.prev/`、`make build`、試跑資料整理、備份、重開、三項檢查、沒過自動換回；不寫 `bin/.deployed-version`）。mini 開發家（目標名 `mini-dev`）在 mini 正式家部署成功、要守的服務都在之後**每次**接著換成同一個 commit；它的 repo 不在 `main`、有沒 commit 的改動（含沒追蹤的新檔）、或 `main` 上有 origin 沒有的 commit 時不動它、白話說原因、結束碼 1，不擋 office；repo、家不在或服務沒在跑就跳過（不算失敗）；換版前後各用那個家的 `evergenie say` 傳一句話給 `art`（開頭「部署工具：」），傳不到不算失敗。開發家的服務管的是另一個家（沒設自己的 `service_label`）、或讀不到正式家部署的 commit 時不換、結束碼 1。office 開發家（`workshop`）照 D-53 預設不跟著換。守它的測試：`scripts/deploy-test.sh` 第 19 段（19a～19ai，`make deploy-test`）；dry-run 不執行另有 `cmd/evergenie` → `TestDeployScriptDryRun`。
- **ADM-39** — 生圖職位 `art` 的樣板在這個套件（`agents/art/agent.md`、`[[contributes.agent]] id = "art"`；DEV-160，作者 2026-11-15：一般使用者的家沒有生圖秘書，只有開發家有，D-67）：開發工坊只在開發家開著，所以一般家「職位與秘書」的可加清單、設定精靈、歡迎頁都沒有 `art`（看板只列啟用中套件帶的職位，WEB-47、WEB-176），`evergenie packages sync secretaries --agent art` 回 `NOT_FOUND`、`packages sync admin`／`ClaimNotify` 回「已停用」（PKG-12、PKG-40）；沒有在核心或頁面另寫「是不是開發家」的判斷。樣板標頭照舊 `optional: true`（選用職位，PKG-12）：開發家的設定精靈、歡迎頁、整包同步也不帶出它，主人在「職位與秘書」頁「選用的職位」自己加。一般家主人自己建一個叫 `art` 的職位（`evergenie agents new art …`）不套這份官方職責（`TemplateDutiesRef` 只看啟用中的官方套件，PKG-63）：那是他自己的職位，職責寫在家裡。**已經有 `art` 的家照舊**：名冊、職責、記憶都不動——同步 `secretaries` 不再碰她，開發家同步 `admin` 時家裡已經有就回 `same`、不寫職責（PKG-63）；家的遷移 `2026-11-12-official-duties` 只轉那次就有的職位（agent 卡 AGT-38）；DEV-157 之後從看板認領、名冊記著 `duties_from: secretaries/art` 的，讀職責時照樣對到 `admin/art`（`duties.go` 的 `movedDutyRefs`，PKG-63）。原本「你主動照顧的事」「界線」的內容不變（DEV-157 寫的，加上 DEV-160 的換圖請求與「不要自己改 `adopted`」）；2026-11-16 起照 secretaries 卡 SCR-5 的固定段落寫（DEV-161 補談，作者：角色設計師兼美術指導，八項基本知識，沒有可選知識；版權只提醒一句、不擋、不用先問主人）。測試見下面「測試計畫」。

## NEVER
None.（詳細卡在 P6–P8 開工時補。）

## 職責
發版流程、官方內容製作工具、UX review 工具化（D-13）、相容檢查、除錯頁；只給開發家的職位樣板（生圖 `art`，ADM-39）。

## 不負責
使用者的家的其他業務 → 其他模組；沒啟用 admin 套件的家沒有這些頁面與程式。

## 介面
`admin` 套件（附程式＋看板頁面，ADM-25～27）；沒有自己的 API，資料走 `api.vault` 的 `admin.*` 種類，開單走 `tasks`。`[[contributes.agent]]` × 1（`art`，選用職位，ADM-39）。

## 事件
None.（開工時補。）

## 儲存
None.（見各自引用的 `schemas/`；開工時補。）

## 依賴
`sdk`

## 錯誤
None.（開工時補。）

## 不變條件
- 發版（nightly／safe／release）、版號、release notes（ADM-22）；發版整套測試（ADM-24）。
- 官方內容製作：角色、衣服（設計中的草稿）、圖庫、生圖技能、出角色包。
- 核心不認得 admin：沒有 `/admin/`、`dev` 服務、DEV 單、發行者旗標；拿掉 `packages/official/admin`，`go build ./...` 照樣過。
- 隱藏版不進角色包。
- 發版前的相容檢查：fixture 家跑新核心，官方套件都要通過新協定（ADM-24）。
- 深度除錯頁：原始佇列、引擎事件 log、分身連線；一般使用者只在設定看簡單的健康狀態。
- 部署：先 mini 後 office、mini 沒過不碰 office；檢查沒過自動換回舊版並以非 0 結束；`--dry-run` 不執行任何東西；`--rollback` 換回上一版並再檢查。
- 部署工具先換成 origin/main 的 `scripts/deploy.sh`（只動 remote-tracking、不改工作資料夾；`--dry-run` 不 fetch 不換）；mini 部署前後只讀 `launchctl list` 比對要守的服務（名稱樣式寫在 repo 外的部署設定，`runbooks/release.md`「部署工具的細節」），少了、常駐型停了、或定時型上次結束碼不是 0 就大聲報錯、結束碼 4，不碰那些服務；`--workshop` 只在 office 成功後才動工坊，工坊的 repo 不在 main 或有沒提交的改動就不動（D-53）。
- mini 正式家部署成功後接著換 mini 開發家：換成同一個 commit；repo 不乾淨或不在 main 時一個檔都不蓋、結束碼 1、office 照樣部署；檢查沒過自動換回舊版；開發家不在或沒在跑就跳過；傳話失敗不影響結果；mini 正式家沒過或要守的服務少了時不碰開發家（ADM-38）。
- `make release-check` 只跑檢查、不部署不推；HEAD 不是 origin/main（或推得上去的快轉）或工作樹不乾淨時，就算全過也不說可以部署（D-53）。
- `make web-ci`／`make check` 的 biome 看整個 `web/`（含 `e2e/`）（D-53）。
- 開發家的 `[runtime] url` 只有 http／https 會變成連結，`javascript:` 之類丟掉。
- 一般家（開發工坊沒開）可加的職位沒有生圖 `art`；開發家有、標選用；已經有 `art` 的家升級、同步後職責一個字都不變（ADM-39）。
- 發版頁：還沒部署的改動列出來，部署按鈕先確認再開部署單（body 含版本與每個 pending commit）；admin 秘書已有同一種沒結束的單時換成連到那張；沒有 admin 秘書時說去哪裡開，不顯示按鈕。
- 發版頁的正式版區：列出上一個正式版、最新測試過的版本、上次每晚測試（沒過的步驟紅字）；版號必須是 x.y.z 且比上一個正式版大、發版說明不能空，不合就停用按鈕；按了先在畫面內確認才開「發正式版 x.y.z」的單（body 含版號、測試過的版本與發版說明全文）；已有沒結束的發版單時換成看那一張；沒有測過的版本時說先跑每晚測試；總覽版本卡多一行正式版（ADM-23）。

## 測試計畫
- admin 套件（`packages/official/admin/internal/run/run_test.go`）：`TestComputeReleaseFromFakeRepo`、`TestComputeReleaseMissingRepo`、`TestDevHomeUnsetStoppedRunning`、`TestDevHomeURLOnlyHTTP`、`TestLoadConfig`、`TestServiceRoundAndRequests`、`TestServiceRoundReplacesLegacySites`；頁面 vitest `pages/lib/release.spec.ts`（「按鈕開單並開工」、`TestAdminToReleaseLegacySites`、`TestAdminToDevHomeUnknownStateAndBadURL`、內文與版號檢查、`TestAdminWaitRequestTimesOut`、`TestAdminOpenedTaskReleasedAfterItEnds`、`TestAdminWhenTextPlain`、`TestAdminPendingNoteNoRepeat`、`TestAdminConfirmNameSpacingAndPlain`、`TestAdminHomeDetailHiddenWhenRunning`）；e2e `web/apps/board/e2e/admin.spec.ts` 的 `TestAdminPagesDeployButtonComesBackAfterTaskEnds`（啟用套件、灌 `admin.release`、開部署單→結單後按鈕回來、頁名只有一個、確認句白話；ADM-30、ADM-27 補）。
- 發版工具：`scripts/release-test.sh`（`make release-test`；Go `cmd/evergenie` → `TestReleaseScript`、`TestReleaseScriptUsage`）；部署：`scripts/deploy-test.sh`（`make deploy-test`，第 16～18 段是 D-53 的守服務、工坊、換成 origin/main 的工具，第 19 段是 ADM-38 的 mini 開發家，第 20 段是 ADM-35 的 dry-run 列出差哪些 commit；`TestDeployScriptDryRun`）；部署前檢查：`TestReleaseCheckScript`；相容與整套：`TestCompatAndReleaseE2EScriptsUsage`、`TestCompatScriptSDKAdditiveOnly`。
- 生圖職位只在開發家（ADM-39）：Go `internal/feature/packages/pkg/devart_test.go` → `TestArtNotAddableWithoutDevPackage`（一般家每個入口都加不了）、`TestArtOptionalWhenDevPackageEnabled`（開發家列出、標選用、指名才建）、`TestExistingArtKeepsDutiesAfterTemplateMoved`（主人自己建的 art 同步後一個位元都不變）、`TestOldSecretariesArtRefStillReadsDuties`（記著舊 ref 的照樣讀得到職責），`manifest_test.go` → `TestOfficialPackagesDiscovered`，`cmd/evergenie` → `TestOfficialDutiesMigrationLeavesOwnArt`、`TestOfficialDutiesKnowledgeFormat`（`art` 的職責照 SCR-5 段落順序、沒有可選知識）；vitest `functions.spec.ts`「開發工坊沒開的家（一般家），可加的職位沒有生圖 art」；e2e `closet.spec.ts` → `TestNoArtOnOrdinaryHome`、`admin.spec.ts` → `TestArtListedOnDevHome`；`scripts/clean-install-test.sh` 2b 段（新家候選與選用的職位都沒有 `art`）。

## 設計決定
- **D-12** — admin 是發行者的工具，判斷「拿掉它，使用者的家照樣完整運作」：發版、官方內容製作（角色、衣服、圖庫、生圖技能）、發版前的相容檢查、深度除錯。特權：admin repo 私有＋家的 `publisher = true`，不另做權限系統。畫面是獨立 SPA `/admin/`（自己的 bundle 與 router，可另外裝成 PWA），使用者的看板不含 admin 的程式。**2026-10-31 改（計畫 `2026-10-31-dev-workshop-split` W5、W6）**：發行者旗標與 `/admin/` 拿掉；是不是開發 EverGenie 的家＝有沒有啟用 admin 套件，頁面掛在看板裡；開發另開一個家（工坊家）跟日常分開。
- **D-35** — 一台可以有**正式家**（跑發佈過的版本）與**開發家**（開發時才開：fixture 資料、自己的 tmux socket、自己的 port、不推播、不連其他台）；家的位置、tmux socket、看板 port、RC 名稱都能依家覆寫。開發與修 bug 在 office（開發家、repo、開發用秘書在 office）；mini 只跑正式版；部署先 mini 再 office，相容靠 `sdk` 型別快照（流程 `runbooks/release.md`）。開發用 `/admin/`：開發用秘書的聊天、開發任務 `DEV-<n>`（存正式家 `vault/admin/`，跟 `SEC-<n>` 分開、日常看板看不到）、開發家狀態、發版與除錯頁。 **2026-10-31 改（計畫 `2026-10-31-dev-workshop-split`）**：`/admin/` 與 DEV 單拿掉，開發另開工坊家；開發家狀態由 admin 套件的程式與頁面看（ADM-26、ADM-27）。
- **D-53** — 部署流程少靠人記（2026-11-03）：①部署工具自己換成 origin/main 那一份（部署的一定是 origin/main，工具也該是；以前要開暫存 worktree 手動 checkout；`--rollback` 例外用手上這份，壞的可能正是新推的工具）。②部署目標那台上不歸部署工具管的家或程式（例另一個家 `family`）的服務與網址不能斷，由工具守：部署前後只讀比對 launchd 名稱（樣式寫在 repo 外的部署設定，repo 裡不寫死；產品名不比，改名前建的服務也守得到），少了就大聲報錯、結束碼 4（`all` 時不再碰 office）；部署前一個都沒看到當成讀錯地方，先不部署；不自動換回（部署本來就不碰它們，換回救不了）、不重開它們。③工坊跑開發用主資料夾的 `bin/`，不在 `~/.evergenie-homes`；`--workshop` 讓 office 成功後順便更新，**預設不做**：工坊是有秘書在工作的家，主資料夾常有別人做到一半的改動，只有在 main 且乾淨時才快轉＋重開，不然說明原因跳過。④`make release-check` 把部署前要跑的自動檢查串起來、跑完全部再給摘要，並檢查測的就是要部署的版本；release-e2e 只在 `FULL=1`（給別的使用者的發版線，維護者自己的部署不經 `upgrade`）。畫面檢查仍是人做。⑤biome 整個 `web/` 只要約 1 秒，web-ci 不再只挑改到的檔（在 main 上 commit 時「改到的」是空的，e2e 檔格式錯會漏到發版前）。
- **D-66** — mini 開發家跟著每次部署換版（2026-11-14，DEV-157）：它是 `art` 做角色與衣服的家，要跟正式家同一版，不能靠人記得去更新。①掛在 `mini` 目標後面自動做、另外給一個只換它的目標 `mini-dev`（補換、換回用），不做成像 `--workshop` 那樣的選項：決定是「每次都換」，要人記得加選項就會漏；也不跟 `workshop` 一樣預設不換，因為工坊的主資料夾常有別人做到一半的改動、重開會打斷正在開發的秘書，mini 開發家的 repo 照規定不直接改。②它沒換成不擋 office、但結束碼 1：其他家不該被一個開發家卡住，可是「沒換成」要有人看到。③沒追蹤的新檔也算改動（工坊只看追蹤中的檔）：`make build` 會把新檔編進去，而且那多半是有人直接在這份上做事，不能蓋。④換成正式家部署的那個 commit、不是當下的 origin/main：兩個家要同一版。⑤服務沒在跑就跳過、不順手開起來：停著多半是有人刻意停的。
- **D-67** — 一般家沒有生圖秘書，`art` 只在開發家（作者 2026-11-15，DEV-160；推翻 DEV-157「`art` 是選用職位、一般家可以加一位」）：做法是把 `art` 樣板從公開的 `secretaries` 搬到開發工坊 `admin`（ADM-39），不在核心或頁面寫「是不是開發家」。理由：①D-12 已經定了「開發家＝開著開發工坊的家」，跟著套件開關走就不用新的判斷，一般家的設定精靈、歡迎頁、「職位與秘書」、認領通知、`packages sync` 全部靠既有的「只列啟用中套件的職位」自動不出現；②樣板照舊標選用，開發家也不會被主動問；③已經有 `art` 的家都是主人自己建的職位（沒有 `duties_from`），樣板從哪來對她們沒有影響，所以不用遷移；DEV-157 部署後從看板認領的會記 `secretaries/art`，讀職責時照樣對到新位置（ADM-39），也不用遷移。開著開發工坊的開發家升級後「選用的職位」就有 `art`。另一條路「`secretaries` 留著 `art`、看板與核心看 `env = "dev"` 決定列不列」沒選：要在精靈、歡迎頁、認領卡、認領通知、同步各加一處判斷，還要再登記一條 env 例外（PLT-20）。代價：開發工坊（含 `art` 樣板）照樣跟著發行的檔案出去（`dev_only` 只管設定頁列不列，PKG-56），一般家 `evergenie packages get admin` 看得到它帶 `art`；一般家主人硬要開開發工坊就會有，這跟其他開發工具一樣由主人自己決定。

## 已知限制
- 部署工具守服務（D-53）：定時型（plist 有 `StartInterval`／`StartCalendarInterval`，例 `com.<產品名>.<家>.tick`）跑完沒有 PID 是正常待命，不算停；曾因此誤報 ✗✗，已修（DEV-119）：看最後結束碼與有沒有在跑，定時型結束碼不是 0 或服務不見了才報。定時型的認法是 `launchctl print` 有 `run interval`／`calendarinterval`，或 `~/Library/LaunchAgents/<名稱>.plist` 有那兩個鍵（任一個成立）；部署前就不正常的（停著、定時異常）不怪部署；事件觸發型（`WatchPaths` 等）仍歸「停著」（跟以前一樣）；`launchd` 的 `KeepAlive`＋定時混用沒測過。
