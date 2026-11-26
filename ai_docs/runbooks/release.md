# 發版流程：office 開發、部署到 mini

Status: DRAFT — code 2026-10-28（缺口 1、2、4 已補）。改 D-35 的「開發家在 mini 上」：**程式的開發與 repo 在 office**；2026-11-14（DEV-157）起 mini 另有一個做角色與衣服的開發家，跟 mini 正式家跑同一版（定義見 `homes.md`）。2026-11-01 改成單機說法：兩台各有自己的家、不連線（法律第 7 條），部署只是把同一份程式分別裝到兩台。

這是維護者自己發版用的流程：在開發機開發，用 `scripts/deploy.sh` 部署到開發機自己與另一台。`office`（開發機）、`mini`（另一台）是部署工具的目標名；主機名、要守的服務這類只屬於自己環境的值寫在 repo 外的部署設定（下面「部署工具的細節」）。給別人用的版本看下面「正式發版」；一般使用者安裝與升級看 `INSTALL.md`／[install.md](install.md)。

給要動 EverGenie 本身（核心、看板、官方套件）的人與 AI 看。日常使用不用讀。

## 誰在哪台

| | office（開發機） | mini |
|---|---|---|
| 正式家 | office 自己的家（`~/Workspace/<登入名>/evergenie-office`），跑正式版；看板、推播、vault 都在這台 | mini 自己的家（`~/Workspace/<登入名>/evergenie-<owner>`），跑正式版；看板、推播、vault 都在這台 |
| 開發 | repo `~/Workspace/<登入名>/evergenie`、worktree `~/Workspace/<登入名>/evergenie-wt/<分支>`、**office 開發家**（原名工坊；admin、code 在那裡）| 不改程式；mini 開發家只做角色、衣服、圖（art 在那裡，`homes.md`）|
| 版本 | 正式家跑 main 上最後部署的版本；office 開發家跑 repo 主資料夾（main）編的 | 正式家與 mini 開發家都跑部署過的同一個 commit |

兩個家互不認得（法律第 7 條）：不同步資料、秘書不互相傳話、各自升級。

開發家：兩個都是常駐的家（office 開發家、mini 開發家），各有自己的 port、服務名與狀態資料夾；誰是什麼的正本、怎麼更新見 `homes.md`。

## 一次改動的流程

### 1. 開單（DEV-n）
- 在工坊家的看板開一張任務單，或在工坊家 `evergenie tasks add --title … --criteria … [--body …]`（驗收條件一定要有，法律第 1.4 條）（工坊家 `[ids] tasks = "DEV-"`，單號是 `DEV-n`）。2026-10-31 起開發任務就是工坊家的一般任務單，跟日常的家分開（計畫 `2026-10-31-dev-workshop-split`）；核心沒有 DEV 單、`dev` 服務、`/admin/` 網站了。
- 開發用秘書接手時把單改成進行中；做完：單上有主人的驗收條件就照條件驗過 `evergenie tasks done DEV-n --result … --verify verified --verify_how …` 自己結；沒有就 `evergenie tasks submit DEV-n …` 送驗收，主人結單（法律第 1.4 條）。

### 2. 在 office 開發
- 在哪改照分支規則（作者的私人文件庫，不公開）：預設在主資料夾改 main 或開分支；有別人同時在改才開 worktree：`git -C ~/Workspace/<登入名>/evergenie fetch origin && git -C ~/Workspace/<登入名>/evergenie worktree add -b <單號小寫>-<主題> ~/Workspace/<登入名>/evergenie-wt/<單號小寫>-<主題> origin/main`。分支、worktree 開工就寫進單的 links，合併當天刪。
- 規則照 `AGENTS.md`、`ai_docs/INDEX.md`、EverGenie 的法律；測試名稱寫清楚守的行為（不標 REQ，TST-1）。
- 改程式中跑 `make check`（只測改到的，TST-12）。
- 要看實際畫面：在開發家跑這個 worktree 編的版本（見下面「開發家」），用 office 的 tailnet 網址或本機 port 開；不要拿正式家試。

### 3. 驗收（TST-12）
- 交回或合併前：`make ci`；動了 `web/`、`sdk/` 加 `make web-ci`（dist 有變要 add 進 commit 再跑一次，`.source-hash` 一起）；`git diff --check`。**不跑任何 e2e**（含 `e2e-smoke`）。
- 例外：大改某一頁的流程時，可以只跑那一個 spec 一次（`make e2e SPEC="…"`），交回時寫明為什麼跑；要看畫面 `make shots`。
  - **相容與發版整套**（P8 第二輪 S5，REQ-284、REQ-285）：再加 `make compat`（sdk 型別快照對上一個正式版只加不刪、fixture 家與上一版建的家用這一版都開得起來）與 `make release-e2e`（暫存資料夾裡真的打版號、安裝、升級、壞版自動退回、手動退回、套件、角色包、nightly／stable，約兩分鐘到十幾分鐘，看機器忙不忙；接著跑下載版的幾支，最後的 `package-go-test` 要網路：從 go.dev 下載 Go 一次、從 proxy.golang.org 抓一個小模組）。兩個都不隨 `make ci` 跑，每晚測試的預設步驟裡有；沒過就不發版。`EVERGENIE_E2E_KEEP=1 make release-e2e` 保留暫存資料夾方便看。
  - **使用者有自己的東西時**（DEV-35，`release-e2e` 第 12、13 段）：升級前確認整包複製官方套件的入口已關（DEV-154），再照舊版留下的樣子手動擺一份改過一行的複製品（`mine` 同名、`.fork-base`、`[fork]`）、改一張角色卡、建自己的角色、裝一個分享來的套件（自帶一位秘書，要指定人設擔任、加自己的附加規則）；核心升級後檢查——裝來的套件與資料還在、fork 的原樣不動（`packages updates` 只列官方新版的差異與衝突）、改過的角色卡與自己的角色不被角色包新版蓋掉（`--force` 才蓋並備份）、分享的套件出新版後職責換新、附加規則還在、再 `upgrade --rollback` 這些也都還在。
- **畫面一定自己看過才上線**（2026-11-02）：改到看板、聊天顯示、通知內容、對話記錄怎麼解（engine 的 transcript）這類「主人看得到」的東西，部署正式家前，做事的 AI 要在 office 開發家用瀏覽器實際操作、截圖，對照改動前後確認畫面對了，交回時附截圖或寫清楚看了哪幾頁、怎麼操作；多台的改動要在連了兩台的開發家看。自動測試全綠不能取代這一步；沒看過就不部署，等看過再說。
- **部署前一次跑完：`make release-check`**（D-53，2026-11-03）：依序 `web-ci` → `ci` → `deploy-test` → `compat` → `web-ci-full` → `e2e-full`（`FULL=1 make release-check` 再加 `release-e2e`），一項沒過也跑完其他項，最後印白話摘要：每項過沒過、花多久、沒過的 log 尾巴（log 在 `$TMPDIR/hg-release-check-<時間>/`）。它也檢查**測的是不是要部署的那一版**：HEAD＝origin/main（或只比 origin/main 多、推得上去）而且沒有沒提交的改動才說「可以部署」；origin/main 有 HEAD 沒有的 commit、或工作樹不乾淨，就算全過也不給（結束碼 1）。秘書 session 的 `EVERGENIE_HOME` 等環境變數它自己清掉。只看會跑什麼：`scripts/release-check.sh --dry-run`。畫面那一步它代替不了。大概要多久：`compat` 一分多鐘（有快取十幾秒）、`release-e2e` 兩分鐘左右（最慢的是第 5 段壞版自動退回，等的是 `update` 的 `verifyWait` 60 秒）、`deploy-test` 一分鐘左右（每個「檢查沒過」的情境要等完重試時間）。

### 4. 合進 main、推
- rebase 到最新的 `origin/main` → 再跑一次步驟 3 合併前那幾項（`make ci` 等；e2e 留到發版前）→ `git -C ~/Workspace/<登入名>/evergenie merge --ff-only <分支>` → `git push origin main`（作者同意的範圍內）。
- `sdk/testdata/api.golden` 是 sdk 給套件用的契約，只准「加」：刪或改既有欄位會讓還沒跟著改的套件（官方的、使用者裝的、自己 fork 的）讀不到或解錯（D-35「相容靠 sdk 的型別快照」，`make compat` 對上一個正式版比）。真的要刪（例如 2026-11-01 拿掉跨台時代的 `site` 欄位）：先確認官方套件（`packages/official/*`）都不再用那些欄位，再在發版說明寫清楚刪了什麼、為什麼；舊資料帶著被刪的欄位要照樣讀得進來（加一條測試守住）；被刪的每一行登記進 `sdk/testdata/api.removed`（後面寫原因），`make compat` 才不會把它算成「少了」。

### 5. 部署到 mini
> 改名（DEV-158）後第一次上新版不要用 `scripts/deploy.sh`：它找的是新名字的家清單（`~/.evergenie-homes`）、資料夾、服務名稱，改名前的家還是舊的，找不到家清單時會當成只有一個家。第一次由搬家工具把家、狀態資料夾、服務名稱一起換成新名字，之後才照這一節部署。

在 office 的任何一份 EverGenie checkout 跑都可以（主資料夾、在哪個分支都行；mini 要在 `~/.ssh/config` 裡、ssh 連得上，主機名見下面「部署工具的細節」）：工具一開始先 `git fetch origin main`，手上的 `scripts/deploy.sh` 跟 origin/main 的不一樣就換成 origin/main 那一份跑（同樣參數；只動 remote-tracking，不切分支、不改工作資料夾；`--rollback` 例外，用手上這份，因為壞的可能正是剛推的 deploy.sh；D-53）。**不用再開暫存 worktree 手動 fetch＋checkout。**
```
scripts/deploy.sh --dry-run all      # 先看會做什麼（不 ssh、不執行）：會部署的 origin/main、本機的 origin/main 多久前更新過、office 上次部署到現在差哪些 commit（ADM-35）
scripts/deploy.sh mini
```
`scripts/deploy.sh` 替你做（REQ-249）：
0. （只 mini，D-53）只讀 `launchctl list`（和 `launchctl print`，用來認定時型），記下名稱符合要守的樣式的服務（同一台上不歸部署工具管的家或程式，例另一個家 `family` 的 `com.<產品名>.family`；樣式見下面「部署工具的細節」）和各自的狀態：在跑、定時待命（定時型，沒 PID、結束碼 0）、定時異常（定時型，結束碼不是 0）、停著（常駐型沒 PID）。讀不到、或一個都沒看到（多半是讀錯地方，例如 ssh 看不到登入畫面那邊的服務），就先不部署、什麼都不動；確定那台不用守才用 `EVERGENIE_DEPLOY_MINI_GUARD=`（空字串）跳過。
1. 遠端 repo `git fetch origin && git merge --ff-only origin/main`（repo 有沒提交的改動、不能快轉就停，不動 `bin/`）。
2. 舊的 `bin/` 留成 `bin.prev/`（退路），然後 `make build`（版本是 `scripts/version.sh` 印的：最近的 `vX.Y.Z` tag 去掉 `v`，tag 之後多了 N 個 commit 就是 `X.Y.Z-N-g<短碼>`，還沒有 tag 就是短碼，REQ-277；`scripts/go-build-bin.sh` 內容沒變就不覆蓋）。
3. 每個家先用新版在複本上試跑資料整理（`evergenie migrate-home --preview`，DEV-154）：沒過就把 `bin.prev/` 換回、不重開，家裡的資料沒動。過了每個家先備份（`evergenie upgrade --deploy begin`），才 `evergenie service restart`（LaunchAgent；真的整理在 serve 啟動時跑）。檢查沒過要換回時，整理過資料的家先用備份還原（`--deploy undo`，在換回 `bin/` 之前）；檢查過了收尾（`--deploy done`）：備份留著當最新一份、更早的更新前備份收掉（UPD-16）；有整理資料就記一筆升級紀錄、通知主人（不用請他確認，不問他要不要刪備份），`evergenie upgrade --rollback` 會連資料一起退回（`modules/system.md` UPD-9）。上次更新整理過資料、還沒確認時（或查不到有沒有整理過），`scripts/deploy.sh --rollback` 會拒絕（只換程式的話舊版讀不懂新格式），要用 `evergenie upgrade --rollback`。
4. 檢查，每一項都過才算完成：
   - `evergenie doctor` 沒有紅字，而且「核心版本」＝這次的 `scripts/version.sh`。
   - 看板首頁 200，頁面引用的 `assets/index-*.js` 跟 `internal/base/app/board/dist/index.html` 的一致、檔案在 dist 裡。
   - `evergenie packages list` 有套件、官方套件沒有載入失敗的（停用中的套件帶著原因，例如「需要套件 X，但它已停用」，不算；主人自己的、別人分享的套件壞了不算部署失敗，輸出列「注意」並用 `evergenie upgrade --report --only-problems` 交給管電腦的秘書，DEV-154）。

   三項一起重試到全部過，最多 `EVERGENIE_DEPLOY_TRIES` 秒（預設 30），印最後一輪：剛重開時舊程式可能還在回看板、新程式還沒起來，只看第一次回應會把好好的新版誤判成壞的、白白換回。
5. 全過：記下 `bin/.deployed-version`、印「部署完成：舊版 → 新版」。**任何一步沒過**：自動把 `bin.prev/` 換回 `bin/`（壞的留 `bin.failed/`）、重開服務、印白話原因、結束碼 1（換不回去是 3，要人處理）。已經是最新版（版本＝`bin/.deployed-version`）會直接說不用重做，不會蓋掉 `bin.prev/`；要照做加 `--force`。
6. （只 mini，D-53）部署（或自動換回）之後再比一次第 0 步的服務：少了、或本來正常的壞了（常駐型停了、定時型上一次結束碼不是 0；等 10 秒還沒回來），就印一大段 `!!!` 標出來、結束碼 4。工具不碰那些服務，也不因此換回 EverGenie（換回救不了），要人馬上看。**以前部署完手動數一次要守的服務，不用再做**；結束碼 4 或看到 `✗✗` 才要處理。定時型（tick 每分鐘跑一次）跑完沒有 PID 是正常的，不會誤報（DEV-119）。

結束碼：0 成功或已是最新；1 沒過、已換回；2 用法錯；3 沒過而且換不回（要人處理）；4 要守的服務少了（3 比 4 優先；`all` 時 mini 是 4 就不碰 office）。

mini 開發家跟著這一步換版：見 [homes.md](homes.md)「部署工具怎麼換開發家」。

工具不管的檢查（人看）：啟用 admin 套件的家，選單的「發版」「開發家」頁開得了。

### 6. 部署到 office
```
scripts/deploy.sh --dry-run --workshop office
scripts/deploy.sh --workshop office  # office 是本機，不經 ssh；office 過了再更新工坊。或 scripts/deploy.sh --workshop all（mini 先，沒過就不碰 office）
```
- **工坊（D-53）**：工坊跑的是開發用主資料夾 `~/Workspace/<登入名>/evergenie` 的 `bin/`（不是部署 checkout），所以 `scripts/deploy.sh office` 不會動它。加 `--workshop`：office 成功後，主資料夾**在 main 而且沒有沒提交的改動**時才 `git fetch`＋快轉到 origin/main → 留 `bin.prev/` → `make build` → `EVERGENIE_HOME=<工坊> evergenie service restart` → 同樣三項檢查，沒過自動換回；在別的分支或有別人做到一半的改動就不動（工坊照舊跑原本的），印出原因與 `git status`，結束碼 1（office 已經部署好了）。工坊已經跑著這一版就不重開（`--force` 照做）。**預設不做**：工坊是秘書正在工作的家，重開會打斷它們，而且主資料夾常有別人的改動，要人決定要不要順便更新。只更新工坊：`scripts/deploy.sh workshop`；換回：`scripts/deploy.sh --rollback workshop`。取代以前手動的 `make build && EVERGENIE_HOME=~/Workspace/<登入名>/evergenie-workshop bin/evergenie service restart`。
- 做的事跟 mini 一樣。兩台的正式版都用**專用的部署 checkout** `~/Workspace/<登入名>/evergenie-deploy`（只拉、只編，不在裡面開發；`EVERGENIE_DEPLOY_MINI_REPO`、`EVERGENIE_DEPLOY_OFFICE_REPO` 可改）。兩台的 `~/Workspace/<登入名>/evergenie` 是開發家跑的開發程式（2026-11-01 起），不拿來部署（DEV-32）。LaunchAgent、`~/.local/bin/evergenie`、`EVERGENIE_ENGINE_BIN_DIR` 都指向 deploy checkout 的 `bin/`。程式在 repo、office 自己的內容在家（`home.toml`、vault、runtime），不在 git。
- 順序照慣例先 mini 再 office（`all` 也是：mini 沒過就不碰 office）：兩個家不連線，不用同一次升級；先在不改程式的那台確認新版沒問題，再動開發機（office 開發家也在 office，壞了開發就停）。mini 部署成功後工具接著把 mini 開發家換成同一個 commit（ADM-38；只部署 office 時不動它）。

### 部署工具的細節
- 機器對應：`mini` 經 ssh，`office` 是本機。主機名用環境變數 `EVERGENIE_DEPLOY_MINI_HOST`、`EVERGENIE_DEPLOY_OFFICE_HOST`（`local` 或 ssh 名稱）；這類只屬於自己環境的值（主機名、要守的服務、家的清單）寫在 repo 外的部署設定 `~/.evergenie-release/deploy.env`（有就讀；`EVERGENIE_DEPLOY_ENV` 可以換位置，設成空字串就不讀，指的檔找不到或語法錯結束碼 2；環境變數優先，設定只補沒設的），沒寫時的預設看 `scripts/deploy.sh` 開頭的說明；沒有 mini 的 ssh 名稱時，真的部署 mini 會停下來（結束碼 2，什麼都還沒做），`--dry-run` 照樣印計畫並先說真的部署會停在這裡；兩台 repo 的位置一起改用 `EVERGENIE_DEPLOY_REPO`（測試用；沒設時各用上面的 deploy checkout）；那台的登入殼層沒設 `EVERGENIE_HOME` 時用 `EVERGENIE_DEPLOY_HOME` 帶正式家的位置。
- `--dry-run`：只印每一步會跑的指令與檢查項目，不 ssh、不執行（`all`、`--rollback`、`workshop` 也可以）；也印出那台的 repo 位置、本機看到的 origin/main（會部署的 commit）與它多久前更新過、office（本機）上次部署的版本到 origin/main 差幾個 commit 並列出最新 15 個（mini 要 ssh 才看得到，只說看不到；ADM-35），手上的部署工具跟 origin/main 不一樣時會說「真的部署時會換」。
- D-53 的開關：`EVERGENIE_DEPLOY_SELF_UPDATE=0`（不換成 origin/main 的部署工具：測試、或正在改 deploy.sh 本身時）；`EVERGENIE_DEPLOY_MINI_GUARD`／`EVERGENIE_DEPLOY_OFFICE_GUARD`（要守的 launchd 名稱，awk 正規式；空字串＝不守；預設看 `scripts/deploy.sh` 開頭的說明，自己的值寫在部署設定）；`EVERGENIE_DEPLOY_GUARD_TRIES`（部署後等它們回來幾秒，預設 10）；`EVERGENIE_DEPLOY_WORKSHOP_REPO`／`EVERGENIE_DEPLOY_WORKSHOP_HOME`（office 開發家跑的 repo 與家）；`EVERGENIE_DEPLOY_MINI_DEV_*`（mini 開發家的 repo、家、要傳話的秘書，見 `scripts/deploy.sh` 的 `devhome_conf`）。目標 `mini-dev` 只換 mini 開發家（補換與 `--rollback mini-dev`）。
- 手動換回：`scripts/deploy.sh --rollback mini`（或 office）：`bin.prev/` 換回 `bin/`、重開、再檢查一次（版本對 `bin/.deployed-version`）。repo 還停在新的 commit，下次部署會重新編。只能換回一版。
- 工具壞了或不能用時，手動做同樣的事（在那台的 repo 裡）：`git fetch origin && git merge --ff-only origin/main` → `rm -rf bin.prev && cp -R bin bin.prev` → `make build` → `evergenie service restart` → 手動查上面三項；沒過就 `mv bin bin.failed && mv bin.prev bin && evergenie service restart`。
- 一台跑多個家（GOAL-2 W2：日常＋工坊）：在那台放 `~/.evergenie-homes`，一行一個家的資料夾（`#` 之後是註解、空行略過、可以 `~/` 開頭）。有這個檔時同一個執行檔只編一次，每個家各自 `EVERGENIE_HOME=<家> evergenie service restart`（服務名稱照那個家的 `[runtime] service_label`）、各自跑上面三項檢查；任何一個家沒過就整台換回、每個家都重開回舊版。`--rollback` 也照這份清單。沒有這個檔＝照舊只有一個家。第二個家要各自設好 `[runtime] board_port`、`service_label`、`tmux_socket`、`cards = false`（不蓋帳號的秘書卡）。見 `ai_docs/schemas/home.md`。清單裡的家共用部署 checkout 的執行檔；**工坊不能放進來**（它跑主資料夾的 `bin/`，放進來會用舊執行檔重開、版本對不上、整台 office 被換回），工坊用 `--workshop`。
- 驗收：`make deploy-test`（`scripts/deploy-test.sh`，假的 ssh／evergenie／make／curl／launchctl＋暫存 repo，REQ-249、REQ-250；第 16～18 段是 D-53：守服務、工坊、換成 origin/main 的工具；第 19 段是 ADM-38：mini 開發家）。**沒有會真的部署的 make 目標**，部署只能人手打 `scripts/deploy.sh`。

### 7. 結案
- `evergenie tasks submit DEV-n …` 送驗收；進度寫進計畫指定的進度紀錄（AGENTS.md 第 3 條）。

## 正式發版（給別的使用者的版本）
上面是維護者自己的兩台（`office`、`mini`）：`scripts/deploy.sh` 把 main 部署過去，**照舊**。給別的使用者的版本是另一條線（P8 第二輪，REQ-277、REQ-278），兩層、沒有 beta：`safe/YYYY-MM-DD[.N]`＝每晚測過全部通過的 commit；`vX.Y.Z`＝主人按了才把最新的 safe 升格成的正式版。版號只從 git tag 來（`scripts/version.sh`，現在是 `0.x.y`，正式 release 才 `1.0.0`），不放 VERSION 檔。

工具是 `scripts/release.sh`（在 office 的 repo 跑；`EVERGENIE_RELEASE_REPO` 預設 `~/Workspace/<登入名>/evergenie`、`EVERGENIE_RELEASE_STATE` 預設 `~/.evergenie-release`）：
畫面在**工坊**的「發版」頁（admin 套件；按鈕開單給 admin 秘書，所以那個家要有 admin 秘書；開了套件但沒有 admin 秘書的家只看得到狀態、沒有按鈕）。

1. **跑發版測試**：「發版」頁正式版區按「跑發版測試」→ 開一張「發版測試（每晚測試）」的單給 admin 秘書，她跑 `scripts/release.sh nightly`（不加 `--push`，DEV-27）：全新 clone origin/main、跑 `EVERGENIE_RELEASE_STEPS`，全過打 `safe/<日期>`（只在本機）並自動跑第 2 步。每晚自動跑的排程**沒裝**，現在只有按了才跑。
2. **準備發版**：`scripts/release.sh prepare`（nightly 過了會自己跑）。上一個正式版到最新 safe 之間的全部 commit → 建議版號＋發版說明草稿，寫 `next.json`（ADM-36）：
   - 版號：沒有上一版＝`0.1.0`；0.x 有新增或沒寫前綴的改動＝minor+1，只有修正與維護＝patch+1；1.0 起只有 `feat` 才 minor+1；major 不自動動，`1.0.0` 要主人自己填。這個 repo 的 commit 多半寫中文、不寫 `feat:`，所以「改動」也算數（DEV-113）。
   - 說明草稿是原樣的 commit 標題（新增／改動／修正／其他，只重新打包看板的不列）。admin 秘書接著把它改寫成白話（分新增、修正，合併同一件事，拿掉純內部的），存成檔跑 `scripts/release.sh notes --notes <檔>` 只換說明（ADM-37）；主人在「發版」頁看到的就是白話版，版號與說明都還可以改。
3. **主人按「發正式版 x.y.z」**：「發版」頁開一張任務單給 admin 秘書，單上帶版號與說明全文，她照第 4 步做。
4. **升格**：把說明存成檔，`scripts/release.sh stable --version 0.2.0 --notes <檔> --push`。版號要比上一個正式版大、tag 還沒有、safe 的 commit 在 `origin/main` 上；在 safe 的那個 commit 打 annotated `v0.2.0`（說明＝發版說明）；**不加 commit、不改任何檔案**。沒有 `--push` 只打在本機。
5. `scripts/release.sh status [--json]` 看現況；任何指令加 `--dry-run` 只印會做什麼。

**推 tag 是對外動作**：只有 `--push` 才推；裝每晚自動跑的排程、推 safe tag 都要先經作者同意。驗收：`make release-test`（`scripts/release-test.sh`，暫存 bare origin＋clone，測試步驟用 `true`／`false`，REQ-277、REQ-278）。整條線的真跑驗收是 `make release-e2e`（`scripts/release-e2e.sh`，REQ-285）；相容檢查是 `make compat`（`scripts/compat.sh`，REQ-284）。

其他人的安裝與升級（`scripts/install.sh`、`evergenie upgrade`）見 `runbooks/install.md`。

## 發行檔（下載版用，DEV-155）
給「不用 git、Go、make 也能裝」的下載版（`modules/system.md` UPD-11～13）。`scripts/dist.sh`（`make dist`，參數用 `DIST_ARGS` 傳）把一個版本編成每種晶片一個包＋一份版本清單，放 `dist/`（不進 git）。不碰 `bin/`、不碰任何家、不推任何東西；讓人下載的網址是 `scripts/install.sh` 的 `DEFAULT_SOURCE`（DEV-158 定成 GitHub Releases 的 `latest/download/manifest.json`）。安裝程式（`scripts/install.sh`，`runbooks/install.md`）裝的就是這種包。
- **包**：`<名稱>-<版本>-darwin-<arm64|amd64>.tar.gz`（名稱預設 `evergenie`），解開是一個資料夾 `<名稱>-<版本>/`：`bin/`（`make build` 那六支：`evergenie`、`evergenie-engine-claude`、`evergenie-habits`、`evergenie-closet`、`evergenie-closet-mine`、`evergenie-admin`）、`fixed/`（附帶的 tmux，檔名是產品名 `<名稱>`，與它的啟動程式 `disclaim`；安裝與升級放到程式資料夾的 `fixed/`，見下面「附帶的 tmux」）、`packages/official/`（git 追蹤的與還沒追蹤、沒被忽略的檔；含公開角色的卡片文字 `packages/official/personas/cards/`，PRS-25，圖不在包裡、走角色包）、`sdk/`（repo 的 `sdk/go.mod`、`go.sum` 與不是測試的 `.go`：主人自己的附程式套件在使用者的電腦上編譯時 import 的就是這份，packages PKG-66；sdk 只靠標準函式庫，用了外部模組 `dist.sh` 會停，那時要另外決定怎麼附並補授權）、`ai_docs/`（跟著程式的說明文件：清單 `ai_docs/bundled.txt` 上的那幾份，路徑不變，連清單一起；秘書替主人做套件時用 `evergenie docs list|show` 讀，platform PLT-28、packages PKG-67。帶哪幾份只改清單，帶的標準、「帶的文件不提到沒帶的」與內容的規矩都在 `conventions/doc-style.md` DOC-12，doccheck 擋）、`VERSION`（一行版本）、`LICENSES/`（Go 本身與用到的外部 Go 模組的授權文字，`README.txt` 列模組與版本；`LICENSES/tmux/` 是 tmux、libevent、ncurses、utf8proc 的）。
- **編法**：`CGO_ENABLED=0 GOOS=darwin GOARCH=<晶片> go build -trimpath`；核心帶 `-X …/platform.BuildVersion=<版本>`，引擎帶 `-X …/engine-claude/internal/claude.bundledTmuxName=<名稱>`（它在 `fixed/` 裡找的檔名，engine-claude ECL-65），給了 `--source` 再帶 `-X …/platform.DefaultUpdateSource=<清單網址>`（家裡沒寫 `[update] source` 時找新版的地方；照下載來源的規則 platform 卡 PLT-25：`https://`，`http://` 只收本機，或 `file:///`，跟家裡的設定同一套）；amd64 補 ad-hoc 簽名（`codesign --force --sign -`；arm64 的 go 自己會簽）；`fixed/` 的兩支不重簽（`build-tmux.sh` 簽過，內容要跟 `dist/tmux` 那份一模一樣，下一版沒改就不換）。包裡只放資料夾與一般檔：有符號連結就算失敗（下載版的安裝不收連結）。`--version` 指定了版本而工作樹有沒提交的改動時會提醒（包裡會帶這些改動）。
- **版本清單** `manifest.json`：
  ```json
  {"schema": 1, "version": "0.3.0", "channel": "release", "released_at": "2026-11-13T00:00:00Z", "notes": "…",
   "assets": {"darwin-arm64": {"url": "evergenie-0.3.0-darwin-arm64.tar.gz", "sha256": "…", "size": 123},
              "darwin-amd64": {"url": "…", "sha256": "…", "size": 456}}}
  ```
  `channel` 是 `release` 或 `safe`；`url` 預設是相對於清單的檔名（`--base-url <網址>` 給 `<網址>/<檔名>`）；`version` 不帶 `v`。
- **選項**：`--version`（預設 `scripts/version.sh`）、`--out`、`--arch "arm64 amd64"`、`--channel`、`--notes`／`--notes-file`、`--base-url`、`--source`、`--name`（預設是 `scripts/install.sh` 的 `PRODUCT`，也是附帶的 tmux 的檔名；不能叫 `disclaim`）、`--released-at`、`--tmux <資料夾>`（附帶的 tmux 從哪拿，預設 `dist/tmux`）、`--no-tmux`（不帶 tmux、沒有 `fixed/`，只給測試升級流程用，這種包不要拿去發）；用法錯誤結束碼 2，編譯或打包失敗 1，還沒編附帶的 tmux 或啟動程式（或連了 `/usr/lib/` 以外的函式庫、少了授權）3。不刪 `--out` 裡原本的東西（預設的 `dist/` 裡有 `dist/tmux/`）。
- **公開角色的卡**（PRS-25、26）：repo 的 `packages/official/personas/cards/` 是開發家（角色包的來源家）官方卡的副本。卡的文字改了、或 `[admin] public_personas` 改了，發版前在開發家跑 `EVERGENIE_HOME=<開發家> evergenie personas pack cards --repo <repo>`（只收公開的、不收標了隱藏版的、卡裡有主人的名字就整個不寫）再 commit；有了角色包網址之後 `personas pack build --set-pointer <repo> --url-base <網址>` 會連卡一起寫。`make dist-test` 會確認包裡剛好那幾張、沒有圖、沒有標隱藏版的。
- **發正式版時**：`--source` 不給就用 `scripts/install.sh` 開頭的 `DEFAULT_SOURCE`（安裝從那裡下載、升級從編進程式的那裡找新版，兩邊自然一樣）；那是佔位的 `.invalid` 網址時編進去的是空的。DEV-158 已把它定成 `https://github.com/minglight/EverGenie/releases/latest/download/manifest.json`；要換下載點只改 `install.sh` 那一行。
- **驗收**：`make dist-test`（`scripts/dist-test.sh`：真的編兩種晶片，對包的內容〔`bin/` 六支、`fixed/` 的產品名 tmux 與 `disclaim`：晶片、簽名、只連 `/usr/lib/`、跟 `dist/tmux` 一模一樣、經 `disclaim` 跑得動；tmux 的授權、公開角色的卡、`sdk/` 跟 repo 的一字不差、只靠它與標準函式庫不連網編得出一個附程式套件、`ai_docs/` 剛好是清單上的加清單本身且一字不差、解開後 `bin/evergenie docs list|show` 讀得到每一份〕、清單的檢查碼與大小、解開後 `bin/evergenie version` 印的版本、晶片與簽名、`--no-tmux`、沒編 tmux 或少了 `disclaim` 時結束碼 3、跑完 `dist/tmux/` 沒被動；要先有 `dist/tmux`）；安裝的整套 `make install-test`（`scripts/install-test.sh`：真的編一包、本機 http、假 HOME、只有系統內建工具，裝好與每種失敗）；下載版升級的整套 `make upgrade-download-test`（`scripts/upgrade-download-test.sh`：三個版本、本機 http、檢查碼不符、下載中斷、清單讀不到、沒有這台晶片的包、壞版自動換回、手動退回、主人確認、主人自己的附程式套件缺 Go 時問了才從本機 http 的假 Go 包下載到 `tools/go` 且升級後還在〔packages PKG-65〕、編的時候工作區指到那一版附的 sdk、升級後跟著換〔PKG-66〕，跑 EverGenie 的 PATH 沒有 go／make／git）。三支都隨 `make release-e2e` 跑，不隨 `make ci`（dist-test 與 install-test 要先有 `dist/tmux`）。最後是乾淨環境的整體驗收 `make clean-install-test`（`scripts/clean-install-test.sh`，DEV-155 S9：沒有開發者工具的假 Mac 照 `INSTALL.md` 裝、照裝好的技能 create-feature 寫的方式用 `evergenie docs show` 讀到三份文件、設定精靈選秘書、開 session、經看板升級、三種失敗與〔退回上一版〕、主人確認、照 `install.md`「移除」移乾淨，約兩分鐘），也隨 `make release-e2e` 跑。下載版真的編得出主人的附程式套件 `make package-go-test`（`scripts/package-go-test.sh`：假 HOME、PATH 沒有 go／git，缺 Go → 問 → `install-go --confirmed` **真的從 go.dev 下載**寫死的那一版 → 用附的 sdk 編好、套件 running → 升一版用新的 sdk 重編 → 從官方套件 fork 來的 `go.mod`＋一個外部模組照樣編好 → 缺 sdk、`go.mod` 要的 Go 太新是白話；家資料夾有 `.git` 也不叫 git；**要網路**：go.dev 約七十 MB、proxy.golang.org 一個小模組，測完刪），隨 `make release-e2e` 跑。
- 官方套件的測試檔與測試資料不進包：路徑有 `testdata/`、`__tests__/` 這一層，或檔名含 `_test.go`、`.test.`、`.spec.` 的都算（規則只寫在 `scripts/dist.sh` 的 `TEST_PATH_RE` 一行，`make dist-test` 讀同一行查包裡沒有）；使用者用不到，而且測試資料裡有開發機的路徑。`make dist-test` 也查包裡的文字檔與 `bin/`、`fixed/` 的執行檔（`strings`）沒有家目錄、`/Users/<名>` 這種路徑（Go 用 `-trimpath` 編；唯一放過的是 BurntSushi/toml 錯誤說明裡寫死的 Windows 範例路徑）。
- `scripts/dist.sh --test-go-pin <檔>` 只給測試用（下載 Go 改讀那份 JSON，packages PKG-65），發版不要加。
- **還沒做**（DEV-155 之後的步驟）：簽名與公證、把發行檔放上 `DEFAULT_SOURCE` 那個網址的流程（DEV-158 定了網址）。看板網頁用到的 npm 套件授權聲明在 `LICENSES/web-third-party.txt`（`scripts/web-licenses.mjs` 產生，`make web-ci` 會檢查有沒有跟上相依的變動），發行檔一起帶；這個專案自己的授權 `LICENSE`（涵蓋程式與官方角色，不是開放原始碼授權）放在包的最上層。

### 附帶的 tmux（engine-claude ECL-63、ECL-65）
發行檔裡的 tmux 與啟動程式 `disclaim` 用 `scripts/build-tmux.sh` 編（產物 `dist/tmux/<晶片>/tmux`、`dist/tmux/<晶片>/disclaim` 與 `dist/tmux/LICENSES/`；打包時 tmux 改名成產品名、兩支放進包的 `fixed/`；下載包的 `amd64` 對到 `x86_64` 那份）。兩支都是可重現的：同一份原始碼、同一台、同一套編譯工具編出來的 sha256 一樣（同一台編兩次比過），所以下一版沒升 tmux 時 `fixed/` 的內容不變，裝好的不會被換、主人不會被再問一次。權限記在 tmux 那支名下，所以**升 tmux 或函式庫、或換了編譯工具（Xcode／Command Line Tools 升級可能讓產物不一樣）而 tmux 的 sha256 變了＝下一版之後主人要再按一次「允許」**，發版說明要寫；只改 `disclaim` 不會讓主人重按，但 `build-tmux.sh` 變了 `release.sh` 會整份重編（下面），重編完要比 `dist/tmux/<晶片>/tmux` 的 sha256 跟上一版發出去的一不一樣（還沒有自動比）。升 tmux 或函式庫的版本：
1. 改腳本開頭的版本與 sha256（從官方發佈頁或 Homebrew 的 formula 對過）。
2. 跑 `scripts/build-tmux.sh`：授權文字變了會停，把原始碼裡新的授權檔換進 `LICENSES/` 再跑。
3. 本機晶片那份會在 `env -i` 乾淨環境下用自己的 socket（`hg-s4test-<pid>`）做經 `disclaim` 開 server 的 `new-session -d`（要什麼都不印）、`send-keys`、`paste-buffer -p`、`capture-pane -p`／`-e`、以 `xterm-256color` 接上畫面的煙霧測試，做完 `kill-server`；`disclaim` 另外試參數與環境原樣帶到、結束碼照傳、沒給參數 2、tmux 不能執行 127。另一種晶片只檢查晶片、連結、簽名與 terminfo 路徑格式。
4. 新的 client 要連得上舊的 server：升級換了 `fixed/` 的 tmux，秘書的對話還開在舊版開的 server 裡，新版的引擎用新的 tmux 送字、看畫面；協定對不上（tmux 說 protocol version mismatch）秘書就全部失聯。`build-tmux.sh` 煙霧測試之後接著跑 `scripts/tmux-compat.sh <舊> <新> <disclaim>`：乾淨環境、自己的 socket（`hg-compat-<pid>`），用舊的經 `disclaim` 開 server 與 session，換新的做 `has-session`、`send-keys`、`load-buffer`＋`paste-buffer -p`、`delete-buffer`、`capture-pane -p`／`-e`、在同一個 server `new-session -d`、`kill-session`、`kill-server`，畫面上要看得到新的打進去的指令跑出來。「舊的」預設是編之前 `--out` 裡本機晶片那份（所以升版時先不要刪 `dist/tmux`），也可以 `--old <上一版發出去的包裡的 fixed/<產品名>>` 指定；沒有就用剛編好的（同版本，驗流程通）。連不上就停、這一版的 tmux 不能發：要嘛換一個協定相容的版本，要嘛升級流程得先關掉舊的 server（會中斷所有秘書的對話，要另外設計）。單獨跑也可以（結束碼 0 連得上、1 不行、2 用法錯；用 `dist/tmux/arm64` 同版本驗過，假的不相容 client 會停在 `has-session`）。

**確認權限視窗記在哪一支名下（不跳視窗）**：系統的權限服務每次判斷都在系統紀錄寫一行 `AUTHREQ_ATTRIBUTION`，裡面的 `responsible_path` 就是視窗上會顯示的那支。做法：在暫存資料夾放一支只呼叫 `TCCAccessPreflight(kTCCServiceListenEvent, NULL)` 的小程式（只問「現在准不准」、不會跳視窗；`ListenEvent` 是「輸入監控」，不碰任何資料夾），背景跑 `/usr/bin/log stream --style compact --predicate 'subsystem == "com.apple.TCC" AND eventMessage CONTAINS "AUTHREQ_ATTRIBUTION"'`（`log show` 會漏），再 `TMUX_TMPDIR=<短的暫存資料夾，例如 mktemp -d /tmp/hgs8.XXXXXX；socket 路徑上限約 104 字元> <fixed>/disclaim <fixed>/evergenie -L <自己的 socket> new-session -d '<小程式>; sleep 5'`；紀錄裡那一行的 `responsible_path` 要是 `<fixed>/evergenie`。之後在同一個 server 不經 `disclaim` 開的 window 也一樣；不經 `disclaim` 開的另一個 server（對照組）是叫起 tmux 的那支（終端機、核心之類）。用 `dist/tmux/arm64` 的成品實測：前兩種都是 `<fixed>/evergenie`，對照組是 `…/evergenie/bin/evergenie`（叫起這串的核心）。做完 `kill-server` 那個 socket、停掉 `log stream`。**不要用碰下載、文件、桌面的指令來試**：會在主人螢幕跳視窗。

**要主人按的那一步（留給電腦秘書跟主人約時間做，主人要在螢幕前）**：權限是記在登入的那個人身上，不看 `HOME`，所以用暫存資料夾就能試，不用動任何家。①在暫存資料夾 `<T>` 放 `<T>/fixed/evergenie`（`dist/tmux/arm64/tmux` 複製過來改名）與 `<T>/fixed/disclaim`；②`TMUX_TMPDIR=<短的暫存資料夾> <T>/fixed/disclaim <T>/fixed/evergenie -L hg-tcc new-session -d -s t`；③（同樣的 `TMUX_TMPDIR`）`<T>/fixed/evergenie -L hg-tcc send-keys -t t 'ls ~/Downloads' Enter`：預期跳出大意是「“evergenie”想要取用你“下載項目”檔案夾中的檔案」的視窗（顯示產品名，不是終端機或 `evergenie-engine-claude`），按「好」；④`kill-server` 後用同一個路徑、沒改過的檔再做一次②③：不能再跳；⑤把 `<T>/fixed/evergenie` 換成內容不同的一份（重編出來是一樣的，改用 `codesign --force --sign - -i evergenie.tcctest <T>/fixed/evergenie` 換簽名的識別碼，cdhash 就不同）再做一次：預期會再問（這就是升級時內容沒變不換的原因）；⑥做完 `kill-server`、刪 `<T>`，到「系統設定 → 隱私權與安全性 → 檔案與檔案夾」把試的那一項移掉。同時開著上面的 `log stream`，`responsible_path` 要是 `<T>/fixed/evergenie`。真的家改用下載版之後，同樣的事在第一次讀這些資料夾時發生一次，之後升級（`fixed/` 沒變）不再問。

發版測試（`scripts/release.sh nightly`）的步驟裡有 `release-e2e`、`dist-test` 或 `install-test` 時，全新 clone 裡沒有 `dist/tmux`：release.sh 先備好一份放在 `$EVERGENIE_RELEASE_STATE/tmux`（預設 `~/.evergenie-release/tmux`，原始碼快取在旁邊的 `tmux-src/`），用 `EVERGENIE_DIST_TMUX` 交給步驟。那份記著編它的 `build-tmux.sh` 的 sha256：腳本沒變就直接用（`--force` 也不重編），變了才重編，並記成第一步「編附帶的 tmux」（第一次或升版時要幾分鐘、要連網抓原始碼）；編不起來就跟其他步驟失敗一樣，不打 safe。驗收：`scripts/release-test.sh` 的「nightly 附帶的 tmux」幾項（假的 build-tmux.sh；快取要兩種晶片的 tmux 與 `disclaim` 都在才算數）。

### 主人同意後下載的 Go（packages PKG-65）
主人自己的附程式套件要編譯、這台沒有能用的 Go 時，下載版會（主人同意後）從 Go 官方下載寫死的那一版到 `<app>/tools/go`。版本、檔名、sha256、大小只寫在 `internal/feature/packages/pkg/toolchain.go` 的 `officialGo`（darwin-arm64、darwin-amd64 兩個包）。換版本（例如 Go 出了安全修正）：
1. 從 `https://go.dev/dl/?mode=json` 抄新版本兩個 darwin 包的檔名、sha256、大小，改 `officialGo`（問主人那一句的「約多少 MB」由大小自己算；規則卡與技能裡寫的「約 70MB」只是例子，大小差很多時順手改 packages PKG-3）；版本要 ≥ `minGoVersion`（跟 `sdk/go.mod` 的 go 那一行一樣，`TestMinGoVersionMatchesSDK`）。
2. 真的下載一次對檢查碼（約 140MB，裝到測試的暫存資料夾，不碰任何家）：`go test ./internal/feature/packages/pkg/ -run TestOfficialGoDownload -count=1 -v -args -real-go-download`（兩個包都對大小與 sha256，這台的晶片再完整解開、問到的版本要對）。
3. `make ci`、`make upgrade-download-test`。

### 角色包（建包、驗收、什麼時候寫指標；DEV-157）
官方角色的頭像與衣服不跟發行檔走，放在角色包（格式 `ai_docs/schemas/persona-pack.md`，規則 `modules/agents.md` PRS-4～7、14～16、28、29）。repo 的 `packages/official/personas/pack.toml` 指到公開的角色包（2026-11-26 起，`https://raw.githubusercontent.com/minglight/EverGenie-personas/main`；格式 `TestOfficialPersonasPointerWellFormed` 守著；url 空＝這一版沒有角色包，行為 `TestPersonasPackEmptyPointerIsNoPack` 守著）。
1. **在哪建**：角色包的來源家（`home.toml [admin] public_personas` 列公開角色）。哪一個家當來源家由維護者定（現在是 mini 開發家，`homes.md` 第 2 節）。來源家的官方角色要留在家裡（第 1 版，或第 2 版寫 `[layout] official_personas = "home"`；`home relayout` 對發行角色包的家本來就這樣寫）：衣櫃只在這種家替官方的圖寫照片紀錄（closet 的 CLO-34），官方角色放家外的家建包會因為官方衣服沒有照片紀錄而擋下，而且等多久都一樣：換一個官方角色留在家裡的家建，不要只在那個家的 `home.toml` 加 `official_personas = "home"`（官方的檔還在狀態資料夾，加了反而找不到官方角色）。
2. **建**：來源家的服務要在跑（建包經服務讀衣服目錄與隱藏版的資料夾；沒在跑會擋下並說先 `evergenie service start --home <來源家>`；衣櫃套件停用或被拒也會擋下，先在 `evergenie packages list` 看原因、啟用或修好）。`evergenie personas pack build --home <來源家> --out <資料夾> --version <YYYY.MM.DD[.N]>`。看最後那行總結：幾位角色、幾個檔案、幾筆紀錄、排除了哪些資料夾——**每一組隱藏版（`closet.variant` 的 `dir`）都要在排除清單裡**，紀錄數要跟公開角色**已經升成官方**的衣服對得上（每位至少一套 `cost = 0` 的 `<代號>-base`，新家才會一裝好就擁有一套）。只有主人按過「升成官方」的衣服會帶（PRS-29）：主人自己的（`source = mine`）與設計中的不帶，總結那行會寫「N 筆還沒升成官方或還在設計中的衣服沒帶：<編號>…（它們的圖 M 個檔也沒收）」，上面的「沒收：」也一筆一行列出——對一下這份編號：該公開的不在裡面（在的話先到角色頁按「升成官方」再重建），不該公開的不在包裡。`--no-records`（只有圖與卡、衣服資料夾整個不收）是給沒有服務的測試與只要圖的特殊情況，正式發的包不要加（新家會拿不到衣服目錄）；裝了衣櫃（有照片紀錄）的家加了它也要服務在跑，照樣照照片紀錄挑圖（DEV-160）。建包只照照片紀錄帶圖：剛放好的衣服與圖要等衣櫃登記好照片紀錄（服務在跑時衣服目錄一改就補，直接複製進來的圖等五分鐘一次的重掃或重開服務；`EVERGENIE_HOME=<來源家> evergenie data list closet.photo` 看），會進包的官方衣服一筆照片紀錄都沒有時建包擋下、列出是哪幾套（PKG-70 ③），等登記好再建；摘要說「N 個圖沒有照片紀錄指著、沒收」時也一樣先等登記好再重建。
3. **驗收（本機，不對外）**：自動的是 `make clean-install-test` 的 2d 段（假的來源家〔官方角色留在家裡〕等衣櫃登記好照片紀錄、建帶紀錄的包（衣服與它們的照片紀錄）、新的家同步後衣櫃每位角色各擁有一套、頭像 200、隱藏版的檔與紀錄不在包裡也不在新的家；來源家主人自己的兩套衣服〔一套已採用、一套設計中〕紀錄、圖、名稱都不在包裡也不在新的家）。真的包再手動看一次：manifest（`<資料夾>/m/<版號>.json`）的 `files` 沒有任何隱藏版的路徑（含名字帶成人字樣的）、`records` 沒有 `closet.variant`、每一筆 `closet.outfit` 的 `source` 都是 `official`、`state` 不是 `draft`（沒有 `<代號>-my-<數字>` 這種編號），而且每一筆都有對應的圖（`files` 裡的 `avatars/<代號>@<衣服>…`；`base` 是 `avatars/<代號>.…`）；反過來 `files` 裡每套衣服的圖也都有紀錄（沒帶的紀錄照命名的圖建包會排除；放在官方位置卻完全沒有紀錄、或不照命名的圖核心認不出來，`modules/agents.md`「已知限制」）；在暫存的測試家（不是任何真的家）用 `EVERGENIE_PERSONAS_PACK_POINTER=<暫存指標檔> evergenie personas pack sync --home <測試家>` 同步（指標的 `url` 可以是 `file://<資料夾>/m/<版號>.json`），看衣櫃與展示間：每位都有衣服、送的那套擁有、沒兌換的是剪影。
4. **上傳**：放到公開網址是無法退回的對外動作，先問作者（憲法第 8 條）。公開網址是 GitHub repo `minglight/EverGenie-personas`（`m/`、`f/` 放根目錄，另有 `README.md`、`LICENSE`、`ai_docs/public-allow.txt`）：新版號的檔加進去、舊版的留著（舊版程式還指著），推之前在那個 repo 跑 `scripts/public-check.sh --require-blocklist .`（圖片資訊要先在來源家的原檔清掉再重建，不要只改包裡的檔：檔名就是 sha256）。
5. **什麼時候寫指標**：包已經在正式網址、而且從那個網址再同步驗過一次之後，才 `evergenie personas pack build … --set-pointer <repo> --url-base <網址>`（同一個版號重建一次，把 `pack.toml` 與公開卡寫進 repo），commit 後跟著下一版發。指標不能指 `file://` 或本機路徑。新家裝好時安裝程式會請服務同步一次，舊家升級後也同步一次（PRS-6）。

## 退路
- 部署後檢查沒過：`scripts/deploy.sh` 已自動把 `bin.prev/` 換回 `bin/` 並重開（看輸出的 FAIL 行與 `bin.failed/`），再查原因；已經部署完才發現有問題用 `scripts/deploy.sh --rollback <機器>`。main 不用 revert（下一版修好再部署）。
- 資料出問題：EverGenie 不做跨台備份（法律第 7 條：每台一個家、兩家不連線），家的資料由主人自己備份；`config set` 改壞的設定看 `runtime/backups/home/`。

## 開發家
現在有兩個開發家：office 開發家（原名工坊，見下面「工坊」）與 mini 開發家。各自做什麼、什麼東西的正本在哪、怎麼更新、東西怎麼上線、部署工具怎麼換它們，都在 [homes.md](homes.md)，這裡不重複。

- 試分支、請作者驗收：用 office 開發家。
- 網址（Tailscale 服務，D-43）：每台的開發家是 `svc:genie-dev-<owner>-<machine>`——mini 開發家 `svc:genie-dev-<owner>-mini`（→8860）、office 開發家 `svc:genie-dev-<owner>-office`（→8880）。
- admin 套件的「開發家」頁只看**同一台**的開發家（開了沒、版本、看板 port、網址）：那台正式家的 `home.toml` 寫 `[admin]` 的 `dev_home = "<開發家資料夾的完整路徑>"`。
- 已退役（2026-11-03，開發家與測試家合一）：office 的沙盒 `~/evergenie-dev`（18870）、`scripts/dev-home.sh`、worktree `devhome` 的流程都不再用。

## admin 套件現在有什麼
- 2026-10-31 起 `/admin/` 網站拿掉（計畫 `2026-10-31-dev-workshop-split` W5）：聊天、任務、秘書直接用看板；admin 套件（選用、`packages enable admin`）只帶程式和兩個看板頁面：
- **發版**：每台版本、main 最新、還沒部署的改動（部署鈕）、正式版（跑發版測試、發正式版）。按鈕只開一般任務單給 admin 秘書，不新增遠端執行的通道。
- **開發家**：開了沒、在哪台、版本、看板 port、網址。開發用帳號就是 admin 秘書在「設定 → 秘書」選的帳號。
- 資料：admin 的程式定時寫 `admin.release`／`admin.devhome`（`current` 一筆）；頁面要馬上重算就寫一張 `admin.request`（`type = "refresh"`）。

## 工坊（office 開發家）
工坊＝office 開發家（「工坊」是原名，部署工具的目標名還是 `workshop`）。它是哪一種家、跑哪一版、怎麼跟著部署更新，見 [homes.md](homes.md) 第 1、3、6 節；這一節只寫它的設定、怎麼建、怎麼上線、怎麼退回。

2026-11-02 從 `runbooks/workshop.md` 併進來；一次性的搬家腳本是 `scripts/split-workshop.py`。秘書 admin、code。家跟家之間的界線照法律第 7 條：每台一個家、家與家不連線。設定欄位的意思見 `schemas/home.md`。

| 項目 | 日常 | 工坊 |
|---|---|---|
| 家 | `~/Workspace/<登入名>/evergenie-<owner>`（mini；office 的家是 `~/Workspace/<登入名>/evergenie-office`） | `~/Workspace/<登入名>/evergenie-workshop`（只在 office） |
| 任務單／Goal 字首 | `SEC-`／`GOAL-` | `DEV-`／`DG-`（`[ids]`） |
| launchd 服務 | `com.evergenie.serve` | `com.evergenie.workshop`（`[runtime] service_label`） |
| 看板 | 8850，`svc:genie-<owner>-mini` https://genie-<owner>-mini.example.ts.net （office 的家 `svc:genie-<owner>-office`） | 8880，`svc:genie-dev-<owner>-office` https://genie-dev-<owner>-office.example.ts.net |
| tmux socket | `eg`（預設） | `hg-ws`（`[runtime] tmux_socket`） |
| 遠端存取名稱 | `<人名>-<owner>-office`（office 的家；`<角色名>-<owner>-<machine>`） | `<人名>-<owner>-office@dev`（`[runtime] env = "dev"`、`machine = "office"`；`remote_suffix` 2026-11-01 退休） |
| 秘書卡（帳號 `agents/<id>.md`） | 不寫（`[runtime] cards = false`） | 寫 |
| 備份 | 主人自己處理 | 主人自己處理（法律第 7 條：EverGenie 不做跨台備份） |
| 更新頻道 | `off` | `off` |

- **建家**：`scripts/workshop-init.sh <工坊> --daily <日常的家> --public-url <網址>`：`evergenie init`（看板 8880）後改寫 `home.toml` 的 `[ids]`、`[runtime]`、`[tasks] keep_review_open = true`、`[update] channel = "off"`，`[user]`、`[slots]`、`[accounts]`、`[admin]`、`[secrets]` 照日常抄（帳號的 `usage_warmup` 關掉）；`settings/packages.toml` 只開 engine-claude、admin 與帶過來的 mine 套件；可以重跑（已有的不蓋，`home.toml` 不一樣的只列出來）。檢查：`EVERGENIE_HOME=<工坊> evergenie doctor` 的【設定】沒有紅字。
- **上線**：裝服務要在本機登入的終端機（**不能用 SSH**，launchd 要在登入的工作階段裡裝）：`evergenie service install --home <工坊>`；工坊**不放** `~/.evergenie-homes`（它跑主資料夾的 `bin/`），部署時用 `scripts/deploy.sh --workshop office` 或 `scripts/deploy.sh workshop` 更新（D-53）；手機用 Tailscale 服務 `svc:genie-dev-<owner>-office`（先在後台建，再 `evergenie tailscale setup --home <工坊> --service genie-dev-<owner>-office`，`evergenie tailscale status` 看「掛上」「核准」）。
- **退回**：`evergenie service uninstall --home <工坊>`、`evergenie tailscale down --home <工坊>`（`~/.evergenie-homes` 有那行的話拿掉），資料夾改名留著（`evergenie-workshop.bak-<YYYYMMDD>-<原因>`）。
- 遠端存取名稱：工坊寫 `[runtime] env = "dev"`、`machine = "office"`，手機 App 裡是 `<人名>-<owner>-office@dev`，跟正式家的 `<人名>-<owner>-office` 分開。
