# 安裝、升級與退版（給新使用者）

Status: DRAFT — code 2026-10-29（P8 第二輪 S2，REQ-279、REQ-280）；2026-11-13 改成下載版（DEV-155 S3）。

這份是給**安裝 EverGenie 的人**（與幫他裝的 AI）看的；發版的人看 `release.md`。維護者從開發機部署自己的家用 `scripts/deploy.sh`（`release.md`），不走這條路。

## 先要有的
- 一台 Mac（macOS 11 以上，Apple 晶片或 Intel 都可以）。
- **Claude Code**：裝好並登入（秘書靠它工作）。安裝程式會檢查（`claude auth status`），沒裝或沒登入就停下來請你先處理，不會幫你裝。
- 不用 git、Go、make、Homebrew、tmux：程式是編好的，tmux 跟著程式一起帶（engine-claude ECL-63；放在固定位置、檔名是產品名，系統問資料夾權限時記在它名下，升級不重問，ECL-65）。

## 安裝：在 Claude Code 裡貼連結
在 Claude Code 裡貼上 repo 的連結，說「幫我安裝」。Claude 照 repo 根目錄的 `INSTALL.md` 做：確認是 Mac → 先問你怎麼稱呼、資料放哪（用預設就好），等你回答 → 下載並執行 `scripts/install.sh` → 照結束碼跟你白話說結果 → 告訴你看板網址。第一次打開看板會先到歡迎頁（board WEB-176），照它選好第一位秘書就能用。

`scripts/install.sh`（不互動，參數給齊就跑完）做的事：看晶片（Apple 晶片一律裝 arm64，就算終端機是 Rosetta 開的）→ 檢查 Claude 有沒有裝、有沒有登入 → 家、狀態資料夾、程式資料夾都要是空的 → 讀版本清單（`manifest.json`）→ 下載這台晶片的包、比大小與 sha256 → 檢查包的內容（一個最上層資料夾、只有資料夾與一般檔）→ 解到 `~/.evergenie/app/versions/<版本>/`、建 `current` 連結、確認 `bin/evergenie version` 是這一版 → 把包裡 `fixed/` 的檔（附帶的 tmux 與啟動程式）放到 `~/.evergenie/app/fixed/`（先寫暫存檔再改名，跟升級的 UPD-15 一樣），確認那支 tmux 跑得動（不行是結束碼 24）→ `evergenie init` 建家（預設 `~/evergenie-<登入名>`）→ 在程式資料夾寫指標檔 → 裝成服務（跑 `current/bin/evergenie`）→ 建 `~/.local/bin/evergenie` 連結（那裡本來有別的東西就不動，最後說一聲）→ 等看板回 200 → 印「裝好了」與看板網址。只用 macOS 內建的工具（curl、tar、shasum、osascript 讀清單）。

選項：`--from-backup <備份檔>`（從備份建家：換電腦用，轉給 `evergenie init --from-backup`，建好的家是備份的內容、綁機器的設定用這台的，還要主人做的事印成「注意：」，`modules/system.md` 的 HBK-8）、`--name`、`--home`（必須不存在或是空的）、`--port`（預設 8765，被占用就往上找）、`--timezone`、`--source <版本清單網址>`（預設是腳本開頭的 `DEFAULT_SOURCE`＝公開 repo 的 GitHub Releases 的 `latest/download/manifest.json`（DEV-158）；改成 `.invalid` 結尾的佔位網址時安裝會停在結束碼 20；照下載來源的規則 platform 卡 PLT-25：要 `https://`，測試可以用本機的 `http://127.0.0.1…` 或 `file:///…`；清單裡包的網址也照這條，curl 的轉址只收 https）、`--dir`（程式放哪，預設 `~/.evergenie/app`）、`--state`、`--no-service`、`--skip-claude-check`（測試用）、`--from-source`（開發用，見下面）。

每一種失敗都印一句「沒裝成功（結束碼 N）：<白話原因>」，並收掉這次建的東西：程式、家、狀態這三個資料夾是這次才建的就刪掉，順便建出來的上層（例如 `~/.evergenie`）空的才刪；本來就在、但是空的就清空；`~/.local/bin` 只收這次放進去的連結（還指著這次的程式才刪），裡面別的東西絕不清。收掉之前，這次建的家或狀態資料夾裡已經有日誌（`runtime/logs/`，例如服務起來了、看板沒起來的結束碼 32）就先複製到 `~/Library/Logs/evergenie-install-<時間>/`，訊息最後寫「日誌留在 <那裡>/。」（被中斷的結束碼 1 也一樣）；裝好時順手刪掉這個名字、超過 30 天的舊日誌資料夾（別的名字不碰）。被 `kill -9` 這類來不及收拾的停掉時，程式資料夾留著 `.install-unfinished`（記著建了什麼、當時的 pid、建家當時家的指紋〔每個檔的路徑、大小、修改時間〕、有沒有裝過服務、建的指令連結），同一行再跑會先照它收掉再重裝：只收這次也要用的位置（換了 `--home` 的話上次的家不動、當場說一聲），服務要確認還是那個家的才卸；上次那個還在跑就結束碼 14、不動它；上次建的家已經不是剛建好的樣子（家的指紋＝每個檔的路徑、大小、修改時間跟建家當時記下的不一樣，例如多了一筆紀錄或改過設定；服務一開就會自己寫的 `settings/migrations.toml`、`.atomic-*` 暫存檔、套件的起始資料 `vault/pkg/` 不算，所以只在套件裡記了東西認不出來；或建了家還沒記下指紋就被停掉）也是結束碼 14、什麼都不動。**服務起來過的家一律不刪**（DEV-155 最後審查；指紋看不出套件頁記的東西）：重跑時收上次的、或這次裝到服務之後才失敗、被中斷，都先停掉服務，再把家與狀態資料夾整個改名搬到原本的旁邊——照備份規則（system 卡 BSW-4）叫 `<家>.bak-<日期>-install-unfinished`、`<狀態資料夾>.bak-<日期>-install-unfinished`（同一個尾巴，搬過去的家照預設還找得到自己的狀態資料夾；同一天已經有一份就用 `.bak-<日期>-<時分>-install-unfinished`，再有就在最後加 `-2`、`-3`，不蓋掉），輸出一行「注意：…整個搬到 <位置>…要拿回來：<位置> 改回 <家>…」（怎麼換回見 `INSTALL.md`「搬到旁邊的家」）。重跑時搬不開（上層寫不進去、名字用完、停不掉服務、改名失敗）是結束碼 14、什麼都不動（停掉的服務重開，重開不了會說）；這次的失敗時搬不開就留在原處、不刪。搬的那一下不理 Ctrl-C；家搬過去了、狀態資料夾搬不過去也搬不回來時，訊息照實說家在哪、狀態資料夾留在原處（重跑時是結束碼 14，下次再搬狀態資料夾）。這次的失敗時服務停不掉：這次裝的東西全部留著（`.install-unfinished` 也留著，訊息「注意：這次裝的服務停不掉…」），同一行重跑時照它停掉、收好（還是停不掉就結束碼 14、什麼都不動）。`--home`、`--state` 是捷徑（符號連結），或程式資料夾跟家、狀態資料夾互相包含：一開始就結束碼 2（收拾時會認錯裡面有沒有東西、或收程式時連家一起刪）。搬到旁邊的家不會自動清（安裝程式只在當下說一次；過 30 天要再問主人，見 `INSTALL.md`「搬到旁邊的家」）。服務從沒起來過的家照上面的指紋規則收掉。`.install-unfinished` 只有這次寫的才收（還沒寫就失敗的，例如結束碼 10～12，不動別人的紀錄）。裝好了卻刪不掉 `.install-unfinished` 時改寫成 `done`，下次重跑當成已經裝好、照一般的檢查停下（家已經有東西是結束碼 13，程式資料夾已經有東西是 14），不會把用過的家收掉。結束碼只寫在兩個地方：完整的表在 `scripts/install.sh` 開頭，`INSTALL.md` 有每一碼怎麼跟主人說（`TestInstallScriptExitCodes` 比對兩張表與腳本裡真的會用的碼）。

一台只裝一個家：程式資料夾已經有東西、或已經有一個家用了服務名稱 `com.evergenie.serve`（`~/Library/LaunchAgents/` 有它的 plist 或 launchd 有它），都是結束碼 14，要升級用 `evergenie upgrade`。`--no-service` 才能在同一台另外裝一份（給另一個 `--dir`、`--home`）。

### 裝出來的樣子
- `~/.evergenie/app/`：`versions/<版本>/`（`bin/` 六支執行檔、`fixed/`、`packages/official/`、`sdk/`、`ai_docs/`〔跟著程式的說明文件，秘書用 `evergenie docs` 讀〕、`VERSION`、`LICENSES/`）、`current` → `versions/<版本>`、`fixed/`（固定位置：附帶的 tmux，檔名 `evergenie`〔產品名〕，與開 tmux server 用的啟動程式 `disclaim`；升級時內容變了才換，system 卡 UPD-15、engine-claude ECL-65）、指標檔 `evergenie.local.toml`（這份程式屬於哪個家，`schemas/home.md`「指標檔」）、`tools/go/`（只有主人自己的附程式套件要編譯、他同意下載 Go 之後才有；升級不刪，packages 卡 PKG-65）。
- `~/.local/bin/evergenie` → `~/.evergenie/app/current/bin/evergenie`：在終端機打 `evergenie` 不用給 `--home`（靠指標檔找家）。`~/.local/bin` 不在 PATH 時安裝程式會提醒；它不改 shell 的設定檔。
- 家（預設 `~/evergenie-<登入名>`）與狀態資料夾（預設 `~/.evergenie/state/<家的資料夾名>`，日誌在它的 `runtime/logs/`；安裝失敗時日誌複製到 `~/Library/Logs/evergenie-install-<時間>/`）。
- `home.toml` 的第一個帳號（`accounts.main`）已經帶了帳號設定：`init` 問引擎套件「用這台現在登入的那個帳號」（ENG-19）。Claude 引擎：有 `CLAUDE_CONFIG_DIR` 就照它；沒有就是 `~/.claude` 加 `default_login = true`（開 Claude 時不設 `CLAUDE_CONFIG_DIR`，才看得到原本的登入；engine-claude ECL-64）。問不到就不寫，最後提醒到看板「設定 → 電腦」的「AI 帳號」填。
- 服務：launchd 的 `com.evergenie.serve`（HST-1）；升級只換 `current`，不改服務的設定。

### 從原始碼裝（開發用）
`scripts/install.sh --from-source [--repo <git 網址>] [--ref <vX.Y.Z|latest|main>] [--dir <資料夾>]`：要先有 git、Go、make、tmux（缺了結束碼 40，不幫忙裝）→ 抓程式到 `~/.evergenie/core`（已經是同一個 repo 就 fetch）→ 切到最新的正式版（還沒有正式版就裝 main 並說明）→ `make build` → 建家 → 裝服務 → 等看板。不建 `~/.local/bin` 的連結、不寫指標檔（跟以前一樣）。`--repo` 預設是腳本開頭的 `DEFAULT_REPO`（`EVERGENIE_INSTALL_REPO` 蓋過；DEV-158 起是公開 repo 的 https 網址）。

## 用手機看（手機連線）
裝好之後想在手機上用看板：在**這台 Mac** 用瀏覽器打開看板的「設定 → 手機連線」（`http://127.0.0.1:<port>/settings/phone`，安裝完成的最後幾行有印），照四步做，每一步做好會打勾（board WEB-187、system SET-10～13）：
1. 在這台 Mac 裝 Tailscale 並登入。沒裝的話頁面上有官網下載連結（https://tailscale.com/download/mac ；下載 App，不用 Homebrew）。
2. 按「讓手機連得到」（等於 `evergenie tailscale setup`，通知要用的金鑰也一起做好）。全新的 Tailscale 帳號第一次會被擋：頁面會說「Tailscale 後台還沒打開 HTTPS」並給一個連結，打開它按「Enable」，回來再按一次。這顆按鈕只有在這台 Mac 自己的瀏覽器開的看板上才有；從手機或別台電腦打開只看得到狀態。
3. 在手機裝 Tailscale（App Store），用**同一個帳號**登入。
4. 用手機的相機掃頁面上的 QR code 打開看板；再照「裝置推播」那一頁把看板加到主畫面、開通知。

要知道的事：
- **跟你在同一個 Tailscale 網路裡的人，都能打開你的看板；不要把別人加進你的 Tailscale 網路。** 看板沒有密碼。
- EverGenie 不會開 Tailscale 的「Funnel」（對整個網路公開）。如果它被打開了，看板每一頁上面和「系統健康」都會出現紅字「你的看板現在整個網路都連得到」；在這台 Mac 的看板按「關掉對外公開」（或 `evergenie tailscale funnel-off`）就關掉，手機照樣連得到。

## 升級
- 每天 10:00（`[update] check_at`）會檢查一次，有新版就發一則通知；「設定 → 系統健康」的「版本與更新」卡可以現在更新、今晚更新（`tonight_at`，預設 03:30）、跳過這版。
- 命令列：`evergenie upgrade --check` 看有沒有新版，`evergenie upgrade --apply` 升級（可加 `--to <版本>`）。更新頻道在 `home.toml` 的 `[update] channel`：`release`（預設，只升正式版）、`safe`（也升每晚測過的版本）、`off`。
- 下載版：新版從版本清單 `manifest.json` 下載（`home.toml` 的 `[update] source`，沒寫用編進程式的；安裝程式不寫這一項，所以之後換下載點只要新版程式改內建的就好）。下載完先對檢查碼、解到 `versions/<新版>/`、備份你的資料（`runtime/backups/upgrade-<舊版>-<新版>.tgz`）、整理資料（`migrate-home`；先在資料的複本上試跑，過了才動真的）、把 `current` 換過去、重開、檢查。**任何一步沒過就自動換回舊版**、跳過那一版、發通知；壞的那版資料夾留在 `versions/` 給人查，下次升級成功時收掉。進度與原因在 `evergenie upgrade --status`，日誌在 `runtime/logs/upgrade.log`。
- 下載出問題（網路不通、檔案不完整、下載到一半斷掉、新版沒有這台電腦的晶片）：不升級，舊版照常在跑，畫面上是白話的原因；細節記在升級日誌。
- 沒成功時看板首頁最上面有一條橫幅，分三種講：「舊版照常在跑」（可以按〔再試一次〕）、「已換回，你的資料沒動」（那一版先跳過）、「也沒能換回，要處理」（按〔退回上一版〕再試一次換回）；每種都帶原因與下一步，〔看細節〕有升級日誌的位置（system 卡 UPD-14、board 卡 WEB-173）。
- 原始碼版（`--from-source` 裝的）：從 git tag 找新版、編譯、留下舊版（`bin.prev/`），壞的那份留在 `bin.failed/`；其他同上。
- 兩台電腦：每台各有自己的家、各自照上面的方式升級（兩個家不連線，法律第 7 條）。

## 退版
`evergenie upgrade --rollback`：回到最後一次升級之前的版本（下載版把 `current` 換回上一版；原始碼版 `bin.prev/` 還在就直接換回，不在就重新編），重開並檢查，那一版之後不會再自動升級。那次升級整理過資料格式的話，資料也一起回到升級前（升級之後新增、改過的東西另外留一份 `backups/rollback-*.tgz`，要找回來請秘書幫忙）。

升級好之後不用做什麼、也不用確認：升級前的備份會留著最新一份（下次更新成功才換掉），用起來有問題就跟秘書說要退回（程式與資料一起回到升級前）。秘書不會問你要不要刪備份。升級後哪裡要整理（你自己的套件、以前複製來改的官方套件…）會自動寫成一份升級報告交給管電腦的秘書，由她處理到能用為止。

## 移除（下載版）
主人說「不用了、幫我移除」時照這幾步（`make clean-install-test` 最後一段照這份做、檢查做完不留程式、服務與行程；那裡的服務是背景行程，launchd 那一半〔bootout、刪 plist〕靠 host 的單元測試，HST-1）。
**要在秘書的 tmux 外面做**：看板上的秘書自己就跑在第 2 步要關的 tmux 裡（第 1 步停掉服務後她的權限確認也會卡住），由她來跑會做到一半就把自己關掉、留下半套。秘書收到這個要求時說明這點、不要自己跑；請主人在「終端機」裡開 Claude Code（跟安裝時一樣）照下面做。
1. `~/.local/bin/evergenie service uninstall`：停掉服務並移除 launchd 的設定（`~/Library/LaunchAgents/<服務名稱>.plist`，服務名稱預設 `com.evergenie.serve`，`home.toml` 的 `[runtime] service_label` 改過就是那個；指令自己會找）。
2. `~/.evergenie/app/fixed/evergenie -L eg kill-server`：關掉秘書們的 tmux（裡面的 Claude 一起停）。socket 名稱預設 `eg`；`home.toml` 的 `[runtime] tmux_socket` 或帳號的 `engine_config.tmux_socket` 改過就用那個名字。
3. `~/.local/bin/evergenie` 是指到 `~/.evergenie/app/…` 的連結才刪（`readlink` 看一下；別的東西不動）。
4. 刪程式資料夾 `~/.evergenie/app`（主人同意後下載的 Go 與編套件的快取 `tools/` 也在裡面）與這個家的狀態資料夾 `~/.evergenie/state/<家的資料夾名>`（日誌、備份都在裡面）；`~/.evergenie/state`、`~/.evergenie` 空了才刪。
5. 家（預設 `~/evergenie-<登入名>`）是主人的資料：預設留著，主人明講要刪才刪（刪前提醒裡面有什麼）。安裝沒裝完時搬到旁邊的家（`<家>.bak-<日期>-install-unfinished` 與 `~/.evergenie/state/<家的資料夾名>.bak-<日期>-install-unfinished`）也是主人的資料，一樣問過才刪。
6. 安裝失敗時留下的日誌 `~/Library/Logs/evergenie-install-*`（只有失敗過才有；裝好時會自己刪掉超過 30 天的）：給人查原因用的，可以刪。
7. 選做：系統設定 →「隱私權與安全性」裡記在「evergenie」名下的資料夾權限可以拿掉（附帶的 tmux 的名字）。下載過 Go、編過主人自己的套件的話，Go 在 `~/Library/Application Support/go` 留了它自己的使用統計設定（很小），這台沒有別人在用 Go 才刪（packages PKG-66）。

Claude Code 與它的登入（`~/.claude*`、`~/.claude.json`）不是我們裝的，不動。原始碼版（`--from-source`；沒有指標檔、沒有 `~/.local/bin` 的連結）：第 1 步是 `~/.evergenie/core/bin/evergenie service uninstall --home <家>`、第 2 步用 PATH 上的 `tmux -L eg kill-server`、沒有第 3 步、第 4 步的程式資料夾是 `~/.evergenie/core`（原始碼版這段沒有自動測試）。

## 出問題怎麼辦
- 安裝失敗：照 `INSTALL.md` 的結束碼表；每次失敗都收乾淨了，修好原因重跑即可。
- 「這台不是用安裝程式裝的，沒辦法自動升級」：找不到程式資料夾。在 `home.toml` 的 `[update]` 寫 `core = "<程式資料夾>"`（下載版是 `~/.evergenie/app`）。
- 下載版：`current` 指的是哪一版看 `ls -l ~/.evergenie/app/current`；舊版的資料夾還在的話，`evergenie upgrade --rollback` 就能換回去。
- 升級結束碼 3（換不回去）：先在看板首頁按〔退回上一版〕（等於 `evergenie upgrade --rollback --rescue`，照 `runtime/upgrade/rescue.json` 再試一次換回；要還原資料時先把現在的家留一份 `backups/rollback-*.tgz`，資料已經還原過的不再還原）。還是不行：原始碼版到程式資料夾看 `bin/` 與 `bin.prev/`，必要時 `git checkout --detach <上一個版本的 tag>`、`make build`、`evergenie service restart`；下載版把 `versions/<舊版>/fixed/` 裡的檔放回 `~/.evergenie/app/fixed/`（內容一樣的不用動，動了主人要再按一次權限視窗的允許；system 卡 UPD-15）、`current` 指回 `versions/<舊版>`、`evergenie service restart`。
- 服務沒起來：狀態資料夾的 `runtime/logs/serve.err.log`；`evergenie doctor` 會說哪裡不對。家的路徑太長（API socket 超過約 103 字元）時服務起不來，日誌會說，把家放到短一點的地方。

## 給測試用的
- `EVERGENIE_SERVICE_MODE=process`：服務改成背景行程（pid 在狀態資料夾的 `runtime/serve.pid`），不碰 launchd。正式安裝不要設。
- `make install-test`：先跑下載版（`scripts/install-test.sh`：真的編一包放本機 http、每個情境一個假 HOME、`env -i`、PATH 只有系統內建工具的替身〔沒有 git、go、make、brew、tmux、python3〕、假的 `claude`；裝好、各種失敗的結束碼與「沒留半套」〔含看板沒起來、裝服務失敗時主人原本的 `~/.local/bin` 不動、已經有一個家用了服務名稱、被 TERM 中斷、被 `kill -9` 後同一行重跑會先收掉上次的；服務起來過的家搬到旁邊不刪（3m、3n、3p、3q；3q2 是只在套件頁 `vault/pkg/` 記了東西、旁邊已經有同名備份；3q3 是搬不開停在 14；3m2 是這次失敗時搬不開、留在原處；3m3 是服務停不掉、全部留著、重跑再收）；`--home` 是捷徑、家放在程式資料夾裡結束碼 2（3l4、3l5）；裝好時刪掉超過 30 天的舊安裝日誌（1a2）；故意失敗靠只給測試用的 `EVERGENIE_INSTALL_TEST_FAULT`（可以用逗號給好幾個）〕、固定位置有產品名的 tmux 與啟動程式、跟編好的一模一樣、安裝前檢查用的是它、經啟動程式開得起 session；要先有 `dist/tmux`），再跑從原始碼裝（`scripts/install-source-test.sh`，假的 Makefile／go；這段也隨 `make ci` 跑）。
- `make upgrade-download-test`（下載版的升級與退版）、`make release-e2e`（原始碼版從安裝到升級、退版的整套，也跑下載版的幾支）。暫存資料夾放 `/tmp`：socket 路徑有長度上限。
- `make clean-install-test`（`scripts/clean-install-test.sh`，DEV-155 S9，約兩分鐘）：一台什麼都沒裝的 Mac 從頭用到移除——空的假 HOME、只有系統內建工具（git、make、python3、編譯器換成一叫就失敗並記下來的絆線）、假的已登入 `claude`；照 `INSTALL.md` curl 安裝程式裝 0.0.1 → 看板 200、doctor 沒紅字、角色庫有公開角色 → 設定精靈的 API 選一位秘書、用附帶的 tmux 開她的 session → 看板 API 升到 0.0.2（資料、session、固定位置的 tmux 都沒變）→ 檢查碼不符、新版起不來、換不回去再按〔退回上一版〕三種失敗（看板拿到的原因與下一步不含英文字母）→ `upgrade --accept` → 照上面「移除」做完不留東西 → 絆線紀錄是空的。要先有 `dist/tmux`；隨 `make release-e2e` 跑，不隨 `make ci`。共用的函式在 `scripts/lib/download-e2e.sh`（install-test、upgrade-download-test 也用）。
