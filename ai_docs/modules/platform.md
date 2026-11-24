# 模組：platform（`internal/base/platform`）
Status: DRAFT — 設計 2026-10-23（P0-S7），pending author review.

**目的：** L0 的共用基礎：時鐘、id、原子寫檔、設定讀取、log、錯誤型別、安靜時段的純計算。不知道任何業務。
**種類：** 底層模組（`internal/base/`；沒有 HTTP、沒有指令，只給別人呼叫）

## MUST
- **PLT-1** — 每台一個家，各自獨立運作，不連接其他家。
- **PLT-2** — 讀 `home.toml` 與 `settings/trust.toml`，缺欄位套預設，欄位不合格回帶欄位名的錯誤（`ERROR_CODE_INVALID`）。
- **PLT-3** — 秘密只放在本家的 `secrets.file`，只回傳路徑；log、錯誤、vault 都不得出現秘密內容。
- **PLT-4** — 原子寫檔：寫暫存檔、`fsync`、`rename`；中途中斷不留下半個檔。
- **PLT-5** — 時間一律帶時區（家的 `user.timezone`），透過可注入的 `Clock`，測試可以凍結與快轉。
- **PLT-7** — 信任設定的型別、解析與欄位驗證留在 platform；等級規則、政策、編輯、快照與拒絕紀錄由 `internal/feature/trust/rules` 擁有。
- **PLT-8** — 家的設定讀寫（Q3）：`GetHomeValue(home, key)` 讀 `home.toml` 的一個值（key 用點分隔，空＝整份）；`SetHomeValue` 改一個值：先備份到 `runtime/backups/home/<時間>.toml`、寫完用 `LoadHome` 重讀驗證、不合格把舊檔寫回並回帶欄位名的錯誤（註解不保留）；`schema` 不能改。只有主人能呼叫（`system.set_config`）。業務模組要讀寫自己那一段（例如 `[accounts]`）用 `ReadHomeTable`（讀成表格）與 `SetHomeTable`（同一套備份、驗證、還原）；AI 帳號的清單與編輯規則在 `internal/feature/system/accounts`，platform 不再有帳號編輯。
- **PLT-9** — （2026-11-01 搬走）備份整理（`ListBackups`、`PlanPrune`、`DeleteBackups`）只有 settings 用，已搬到業務模組 `settings`（`backupfiles.go`，見 `modules/system.md` 的 SET-3）。
- **PLT-10** — 已廢：2026-10-31 法律第 7 條改，兩家不連線。
- **PLT-11** — 本家 runtime 設定核心自己讀的有 board_port、env、owner、machine、sandbox、service_label（舊的 dev 換算成 sandbox＋env，PLT-20；remote_suffix 2026-11-01 退休、讀到忽略）；這些都不傳給引擎。API socket 在本家的 runtime/sockets。舊 backup_dir 忽略，不傳給引擎。
- **PLT-17** — 編號字首（GOAL-2 W3）：`home.toml` 的 `[ids]`：`tasks`（預設 `SEC-`）、`goals`（預設 `GOAL-`），`Home.IDs`（`IDPrefixes`：`Task()`、`Goal()`、`IsTask`、`IsGoal`、`GoalPattern`）；`ReadIDPrefixes(home)` 讀不到設定回預設。格式 `^[A-Z]+-$`，不能撞 `R-`、`H-`、`M-`、`P-`、不能兩個一樣，不合法回 `Invalid`（欄位 `ids.tasks`／`ids.goals`）。核心沒有另外的「開發任務」：單號只認家的任務單字首（工坊設 `DEV-`，`DEV-<n>` 就是它的任務單；日常家的 `DEV-<n>` 不是單號）。寫死 `SEC-`／`GOAL-` 的地方一律改用它（vault 發號與連結檢查、session 工作 session 認單號、api `session.open`、goals 在文字裡找 Goal）。
- **PLT-18** — 任務單的家設定（GOAL-2 工坊）：`home.toml` 的 `[tasks] keep_review_open`（bool，預設 `false`），`Home.Tasks.KeepReviewOpen`。`true`＝要你驗收（review）的單，工作 session 不關、被關掉的自動接回、接回不占進行中的名額，驗收或取消才關（session 模組 SES-29 照它判斷，不看單號字首）；工坊設 `true`。
- **PLT-12** — 廢除（2026-10-31 admin 搬出核心）：核心只讀 `[admin] public_personas`（角色包）；`agents`、`dev_home`、`repo`、`release_state` 由 admin 套件自己讀 home.toml（ADM-25），核心不認得；`dev_site` 2026-11-01 廢除（法律第 7 條：家與家不連線；ADM-8）。`PeekHome`、`DevAgent` 拿掉。
- **PLT-13** — 已廢：2026-10-31 法律第 7 條改，兩家不連線。
- **PLT-23** — session 照引擎回報發的事實事件，名字與 payload 的正本在 `session_events.go`（S5，2026-11-02）：`TopicSessionTurnFailed`（`session.turn_failed`，`{name, agent, account, error, last_message}`）、`TopicSessionEnded`（`session.ended`，`{name, agent, account, reason}`）、`TopicSessionRecovered`（`session.recovered`，`{name, agent, account, after}`：報過 TURN_FAILED 或帳號登出之後第一次正常結束，S5-2 加，notify 用它收提醒 NTF-55）；發的是 `session`、收的是 `notify`（NTF-54、NTF-55），兩邊都只用這裡的常數。派送器算出來的「話送不進去」也在這裡（DEV-162）：`TopicSessionStuck`（`session.stuck`，`{name, agent, agent_name, count, minutes, dialog}`）、`TopicSessionUnstuck`（`session.unstuck`，`{name, agent, agent_stuck}`；核心啟動後第一輪 `{reset, agents}`）與查詢回傳的 `SessionStuck`（SES-77、NTF-68）。帳號登出的 reason 是 sdk 引擎契約的 `sdk.SessionEndReasonLoggedOut`（`logged_out`，ENG-16），這裡不另定。
- **PLT-14** — LoadHome 將 sessions.workdir、secrets.file、update.core 的 ~ 展開；其餘有效路徑照原語意。舊 [backup]、servers 鍵忽略；[incidents] owner **已廢**（2026-11-02 S5：doctor 不再檢查；S5-2 刪掉 session 的事故單之後就沒有人讀）：照讀進 `Home.Incidents.Owner`、舊家的 home.toml 不會因此壞；舊的 dispatcher 由 `MigrateIncidentsOwner`（家的遷移 `2026-11-01-incidents-owner`）抄成 owner 的遷移照留（已經跑過的家不會再跑）。
- **PLT-15** — 核心的版號（P8 第二輪 S1，REQ-277）：版本字串從 git tag `vX.Y.Z` 來（`scripts/version.sh`：在 tag 上＝`0.2.0`、tag 之後 N 個 commit＝`0.2.0-N-g<短碼>`、有未提交改動加 `-dirty`、還沒有 tag＝短碼）。`ParseVersion(s)` 回 `CoreVersion{Major, Minor, Patch, Pre, Ahead, Commit, Dirty}`（認得開頭的 `v`；短碼、`dev`、空字串回 false），`Compare` 先比數字、預發布比正式版小、tag 之後的 commit 越多越新（`Commit` 與 `Dirty` 不參與），`Release()`＝沒有 `Pre`、`Ahead == 0`、不 dirty，`String()` 還原成同樣的字串。型別叫 `CoreVersion`（不是 `Version`）因為 `Version()` 函式早就存在。`internal/feature/packages/version.go` 的 semver 是套件的版本限制，另一回事、不合併。
- **PLT-16** — 版本與更新的設定與狀態檔、家的遷移（P8 第二輪 S2，REQ-280）：`home.toml` 的 `[update]`（`channel`＝`release`｜`safe`｜`off`，沒寫是 `release`；用部署工具更新的家自己寫 `off`（2026-10-31 拿掉發行者旗標，計畫 `2026-10-31-dev-workshop-split`）；`check_at` 預設 `10:00`、`tonight_at` 預設 `03:30`、`core` 選填；`source` 選填，是下載版找新版的版本清單網址（照下載來源的規則 PLT-25，DEV-155；`Home.UpdateSource()`＝`source` ＞ 編進執行檔的 `DefaultUpdateSource`〔`scripts/dist.sh --source`，現在是空的〕）；寫錯回 `Invalid`。Evidence: `update_test.go` → `TestHomeUpdateSource`）。升級的狀態檔（`<家>/runtime/upgrade/`）的格式與讀寫已搬到業務模組 `update`（`modules/system.md` 的 UPD-3）；這裡只留設定（頻道常數、`Home.UpdateChannel`）與通用的 `ReadJSONFile`／`WriteJSONFile`。家的遷移（`HomeMigration`、`RunMigrations`）2026-11-01 搬到 `update`（UPD-8）；`DecideTonight` 隨升級狀態搬到 `update`。
- **PLT-19** — 「只有主人能改」的判斷只有一份：`platform.IsOwner(by string) bool`（`by` 是已過 `ActorName` 的操作者名稱；主人＝`owner(` 開頭，例 `owner(web)`、`owner(cli)`）。所有主人限定都呼叫它（agent、notify、tasks、goals、policy、packages、reminders、personas、agents、session、vault 篩選、trust、accounts、update、settings），不要自己寫 `strings.HasPrefix(by, "owner(")`。另有 `platform.IsOwnerOrSystem(by)`：主人或 `system`（名冊、角色庫的寫入）；「主人或某位秘書本人」寫 `by == id || platform.IsOwner(by)`。例外：`internal/base/session` 的 `owner(from)` 判斷的是訊息寄件人（bus 的 `From`，不一定過 `ActorName`），多檢查結尾是 `)`，內部呼叫 `IsOwner`。
- **PLT-20** — 家的身分（2026-11-01 作者定）：`[runtime]` 的 `env`（`prod`｜`dev`，沒寫＝`prod`）、`owner`（沒寫＝`user.name` 轉小寫、空白換 `-`）、`machine`（沒寫＝這台的短主機名）；`Home.IsDev()`、`Home.Label()`（`<owner>-<machine>`，dev 加 `@dev`）。身分主要拿來標記（依 `env` 改變行為的例外，這份清單是正本，加一項就補這裡與 `schemas/home.md`：AGT-35 秘書的共用規則、PKG-56 設定頁列不列 `dev_only` 套件、closet-mine 的 CMI-14 衣服「升成官方」只在開發家能做、closet 的 CLO-27／CLO-29 開發家不遮沒兌換的衣服並標哪些可以升成官方、CLO-32 開發家多一個能力 `edit`（別的套件的頁面用 `show_when = "cap:closet.edit"` 跟著它，核心只看能力、不看 `env`）——套件都是問 `system.get_mode` 的 `env`，讀不到當正式家），其他行為看 `Runtime.Sandbox`（不推播、不自動升級、套件檢查放寬）。舊的 `dev = true` 讀成 `Sandbox = true` 加 `Env = "dev"`（明寫 `env` 優先）；`Runtime.LegacyDev` 只在 `LoadHome` 換算，別處不讀。格式不合回 `Invalid`（`runtime.env`／`runtime.owner`／`runtime.machine`）。Evidence: `internal/base/platform/identity.go` → `Home.resolveIdentity`、`Home.Label`；`identity_test.go` → `TestHomeIdentityDefaults`、`TestHomeIdentityDevLabel`、`TestHomeLegacyDevMeansSandboxAndDevEnv`、`TestHomeRemoteSuffixIgnored`、`TestHomeIdentityRejectsBadValues`。
- **PLT-22** — 看板連多台的 `[web]` 設定（S3，2026-11-01）：`owner_login`（有值要像 email，`ValidOwnerLogin`）、`allow_boards`（看板的 https origin 清單）、`[[web.sites]]`（`url` 同 origin 規則、`color` 空或 `#rgb`／`#rrggbb`）。`LoadHome` 把每個網址經 `NormalizeOrigin` 整理成瀏覽器 `Origin` 標頭的寫法（host 小寫、IPv6 標準寫法加中括號、去 `:443`、port 前導 0 與結尾斜線），之後別的模組直接拿字串跟 `Origin` 比；不是 https、帶路徑／查詢／片段／帳密、host 不是 ASCII 網域或合法 IP（萬用字元、中文網域、IPv6 zone）、重複的都回 `Invalid`（`web.allow_boards`、`web.sites.url`、`web.sites.color`、`web.owner_login`）。檢查與整理只寫在 `NormalizeOwnerLogin`、`NormalizeAllowBoards`、`NormalizeSites`（`validateWeb` 與 settings 的 `web.set_*` 寫檔前都呼叫它們，SET-8）。這些網址只給瀏覽器用，伺服器之間不連線。只收 https、不為測試放行 http：兩個家的 e2e 在每個家前面放 https proxy（TST-14）。`PublicBase()` 是 `web.public_url` 去掉結尾斜線的唯一寫法（推播連結、`get_mode` 都用它）。Evidence: `internal/base/platform/web.go` → `validateWeb`、`NormalizeOrigin`、`Home.PublicBase`；`web_test.go` → `TestWebMultiBoardSettingsLoadAndNormalize`、`TestWebMultiBoardSettingsAreOptional`、`TestWebMultiBoardSettingsRejectBadValues`、`TestOwnerLoginAcceptsGitHubStyleLogin`、`TestNormalizeOriginMatchesBrowserSerialization`。
- **PLT-21** — 指標檔（2026-11-01）：程式資料夾根目錄的 `evergenie.local.toml`（`PointerFile`）寫 `home = "<家>"`；`ReadPointer(checkout)` 讀（`~` 展開、相對路徑以 checkout 為準、沒有檔回空字串、壞了回 `Invalid`），`PointerHome(exe)` 讀這個資料夾的指標檔：執行檔位置（符號連結先解開）`bin/` 的上一層；那一層是下載版的版本資料夾（`DownloadInstallOf(exe)`：`<app>/versions/<版本>/` 有 `VERSION`、沒有 `.git`，`<app>` 是下載版的安裝資料夾，UPD-11）時改成安裝資料夾 `<app>`，升級換了 `current` 也還找得到（安裝程式把指標檔寫在那裡；版本資料夾裡的指標檔不算）。順序與指令端的接法見 `schemas/home.md`「指標檔」。「是不是下載版」只在 `install_layout.go` 判斷一次（`IsDownloadInstall`、`DownloadCurrentVersion`、`DownloadInstallOf`），指標檔與升級（`update` 模組）共用。Evidence: `internal/base/platform/pointer.go` → `ReadPointer`、`PointerHome`；`install_layout.go`；`pointer_test.go` → `TestReadPointer`、`TestPointerHomeFromExecutable`、`TestPointerHomeForDownloadInstall`；`cmd/evergenie/pointer_test.go` → `TestPointerFileSetsHomeWhenNoEnv`、`TestPointerFileLosesToEnv`、`TestPointerFileMissingOrBroken`、`TestServeHomeFlagBeatsEnv`。
- **PLT-24** — 執行檔旁邊的資料夾（DEV-155，2026-11-13）：`ExecutableDirs()` 回 `os.Executable()` 所在的資料夾，經過符號連結叫起來的再加解開之後的資料夾（不一樣才加；照叫起來的路徑在前）。找官方套件資料夾（PKG 的「位置」）、發版附的套件執行檔、引擎執行檔都試這幾個，所以下載版經 `current`、或經 `~/.local/bin` 的連結叫起來都找得到。Evidence: `pointer.go` → `ExecutableDirs`；`pointer_test.go` → `TestExecutableDirs`。
- **PLT-25** — 下載來源的規則（DEV-155 審查，2026-11-13）：從網路拿程式或資料的網址只有一條規則，`DownloadURLPattern`／`DownloadURLAllowed`：`https://<主機>[:port][/…]`；`http://` 只收本機（`127.0.0.1`、`localhost`、`[::1]`，測試用；別台的 http 在路上就能連檢查碼一起被換掉）；`file:///<絕對路徑>`；主機只收英數、`.`、`-`，路徑不收空白與控制字元，不收帳號密碼。下載用的 client 走 `DownloadClient`：轉址照 `DownloadRedirectCheck`（轉過去的也要合規則；一路上出現過 https 之後只能是 https；最多 10 次；擋下回 `ErrUnsafeRedirect`）。打開下載來源只有一支 `OpenDownload(ctx, client, 網址, timeout)`：先照 `DownloadURLAllowed`，`file://` 直接讀（要是一般檔），https 與本機 http 走 `DownloadClient`；失敗回 `*DownloadError`（`Fault`：`DownloadNotAllowed`、`DownloadUnsafeRedirect`、`DownloadUnreachable`、`DownloadStatus`＋`Status`；`Err` 是英文細節、不含查詢字串），白話由呼叫的人照種類挑。用到的地方：家的 `[update] source`（`home.go`，訊息只建議 https）、下載版升級的清單與包（UPD-12，`update.open`）、角色包同步與 `--url-base`（PRS，`fetcher.open`）、主人同意後下載的 Go（packages PKG-65；下載點也要照規則，不照的當成「下載 Go 的設定讀不到」）、`scripts/install.sh` 的 `--source` 與清單裡的包、`scripts/dist.sh` 的 `--source` 與 `--base-url`；兩支腳本的 `DOWNLOAD_URL_RE` 跟 `DownloadURLPattern` 一字不差，curl 的轉址只收 https。Evidence: `download_url.go`；`download_url_test.go` → `TestDownloadURLAllowed`、`TestDownloadClientRefusesRedirectAwayFromHTTPS`、`TestOpenDownload`；`packages/pkg/toolchain_test.go` → `TestGoToolsInstallFailuresLeaveNothing`（下載點不照規則、https 被轉成 http）；`update_test.go` → `TestHomeUpdateSource`；`cmd/evergenie/install_script_test.go` → `TestDownloadSourceRuleSameEverywhere`；`update/download_test.go` → `TestDownloadRefusesRedirectAwayFromHTTPS`；`personapack/pack_test.go` → `TestPackSyncRefusesRedirectAwayFromHTTPS`。
- **PLT-26** — 產品名只有一個來源（DEV-155 審查，2026-11-13；改名 DEV-158 用）：`platform.ProductName`（正本 `sdk.ProductName`，sdk 讀家的佈局也要用）。核心執行檔 `bin/<名>`（`CoreExe`）、指標檔 `<名>.local.toml`（`PointerFile`）、預設的狀態資料夾 `~/.<名>/state/<家>`（`DefaultStateDir`）、服務名稱 `com.<名>.serve`（`host.DefaultLabel`）、引擎執行檔 `<名>-engine-<id>`（`registry.Executables`）都從它推，不在別處另寫（官方套件用 `sdk.ProductName`）；Go 的非測試檔裡整個字串就是產品名組成的檔名、路徑、名稱，`TestNoHardCodedProductName` 會擋（例外只有格式識別字，例如看板請求的 `X-Requested-With`，列在測試裡並寫原因）。程式外面的（`scripts/install.sh` 的 `PRODUCT`、`Makefile` 的 build 與 `scripts/dist.sh` 的 `BINS`、`cmd/<名>/`、`INSTALL.md` 的表）做不到共用常數，靠測試綁住：`TestProductNameSameEverywhere`、`TestInstallScriptNames`；`scripts/dist.sh` 認核心與引擎用模組資料夾，不認執行檔名。改名要改的全部地方列在 `scripts/install.sh` 開頭「名稱與網址」那段。
- **PLT-27** — 下載的包怎麼收、怎麼解開只有一套（DEV-155，2026-11-13；憲法第 2、4 條）：`CopyVerified(dst, src, size, sha256)` 最多讀 size+1 位元組、邊寫邊算 sha256，長度與檢查碼都要對，失敗回 `*CopyError`（`Fault`：`CopyWrite` 寫不進去、`CopyCut` 斷掉或比寫的短、`CopyTooLong`、`CopyMismatch`）；`ExtractTarGz(archive, dest, UnpackOptions{Top, KeepTop, MaxBytes})` 只收一個最上層資料夾底下的資料夾與一般檔（絕對路徑、`..`、符號連結、硬連結、重複的檔、同一個名字是檔也是資料夾、其他種類、超過 `MaxBytes` 一律拒絕；pax 全域標頭略過；檔案權限最多 0755），失敗回 `*UnpackError`（`Bad`＝包的內容不對，其中超過 `MaxBytes` 那一種另外標 `TooBig`；不是 `Bad` 是寫不進去）。只回種類與英文細節，給人看的白話由用的模組自己定。每個用的地方都要給 `MaxBytes`（壓縮得特別小、解開特別大的包會塞滿硬碟；沒給〔≤ 0〕`ExtractTarGz` 直接拒絕、什麼都不碰，回不是 `Bad` 的錯，`TestExtractTarGz`）：`system/update` 換版下載與解開發行檔（UPD-11、UPD-12；解開後最多 2 GiB）、`packages` 下載 Go（PKG-65；1 GiB）。
- **PLT-28** — 程式資料夾與跟著程式的說明文件只有一套找法（DEV-155，2026-11-14）：程式資料夾＝執行檔解開符號連結後的上上層（`ProgramDir`）——下載版是 `<app>/versions/<版本>`（經 `current`、`~/.local/bin` 的連結都解開到這裡），原始碼版是 repo；解不開回空字串。附的 sdk（packages PKG-66）與說明文件都從這裡找。說明文件放 `<程式資料夾>/ai_docs/`，帶哪幾份只看清單 `ai_docs/bundled.txt`（`ProgramDocsDir`、`ProgramDocsList`）：`scripts/dist.sh` 照它抄進發行檔、`evergenie docs`（packages PKG-67）照它給、doccheck 照它檢查（doc-style DOC-12）。清單一行一份，相對 `ai_docs/` 的 `.md` 路徑，空白後接 `# 一句話說明`；`#` 開頭與空行略過；絕對路徑、`..`、不乾淨的路徑、有空白或反斜線、不是 `.md`、重複、整份是空的都回錯並說第幾行（`ParseProgramDocs`）。找一份（`FindProgramDoc`）收完整路徑、前面多 `ai_docs/`、少 `.md`，或只寫檔名（清單上只有一份叫這個名字時）；不在清單上的一律找不到（原始碼版也不給沒跟著發行的內部文件）。
- **PLT-29** — 日誌不會把硬碟寫滿（DEV-153，`logguard.go`）：①`NewLogger` 外面包一層收斂——同一句警告或錯誤（等級、標題、欄位、模組都一樣）在視窗內只寫第一次，視窗過了再發生時寫一行帶 `repeated=<沒寫的次數>`，視窗加倍（1 分鐘起、最多 1 小時），安靜超過 1 小時重新算；一般紀錄（INFO）不收斂；記住的種類上限 512。②每日日誌 `runtime/logs/<日期>.jsonl` 超過 `LogMaxBytes`（20 MiB）就改名成 `<日期>.1.jsonl`（只留一份）再開新的；清道夫照日期一起清。③服務的 `serve.err.log`／`serve.out.log`（launchd 接的，不照日期分檔）：`WatchServiceLogs`（serve 啟動時與之後每小時）超過上限就把最後 `LogMaxBytes` 複製到 `<檔名>.1`（不整份複製）、清空原檔；只動真的接在自己輸出上的檔（比對 inode）。`<檔名>.1` 留不下來時（硬碟滿了、沒權限）不能就這樣放著讓它繼續長：改成**原地只留尾巴**（`truncateToTail`，留最後 `logTailKeep()`＝256 KB 與上限四分之一取小的那個，其餘清掉），不需要另外的空間。測試 `TestRepeatGuardCollapsesFlood`、`TestRepeatGuardBoundsKeys`、`TestDailyLogRotatesAtSizeCap`、`TestGuardOwnLog`、`TestGuardOwnLogTruncatesInPlaceWhenPreviousCopyCannotBeKept`。
- **PLT-6** — 安靜時段的判斷與「延到 `quiet_defer_to`」是純函式，`jobs` 與 `notify` 共用。

## NEVER
- **PLT-N1** — 不 import 任何其他 `internal/` 模組，也不出現任何業務名詞（任務、待辦、秘書）。
- **PLT-N2** — 不讀秘密檔內容、不把環境變數整包寫進 log。

## 職責
- `Clock`、`NewID`（ULID）、`AtomicWrite`、`Log`（結構化 log）、`Config`（`home.toml` 載入）、`Quiet`（安靜時段）、`Err`（錯誤型別，對應 `ErrorCode`）。

## 不負責
- 檔案資料的 schema 與驗證 → `vault`；訊息 → `bus`；排程時間 → `timer`（計時）與 `jobs`（使用者排程）；家的格式升級 → `update`（`migrate-home`）。

## 介面
```go
package platform

type Clock interface{ Now() time.Time }
func NewID() string                                   // ULID
func AtomicWrite(path string, data []byte, mode os.FileMode) error
func ShareName(base string) string                    // 分享檔名清理：只留 [A-Za-z0-9._-] 與中日韓文字，其他換 `_`，最長 80 字元（notify.Share 與看板上傳共用，P6-3b）
type Quiet struct{ Start, End, DeferTo string; Loc *time.Location }
func (q Quiet) Contains(t time.Time) bool
func (q Quiet) Defer(t time.Time) time.Time           // 落在安靜時段就回 DeferTo
type Home struct{ /* home.toml 對應 */ }
func LoadHome(dir string) (*Home, error)
func (h *Home) SecretsPath() string                   // 只給路徑
func (h *Home) IsDev() bool                           // [runtime] env = "dev"（主要是標記，例外見 PLT-20）
func (h *Home) Label() string                         // <owner>-<machine>[@dev]
func (h *Home) PublicBase() string                    // web.public_url 去掉結尾斜線（PLT-22）
type WebSite struct{ URL, Color string }              // [[web.sites]] 的一台
func NormalizeOrigin(raw string) (string, bool)       // 整理成瀏覽器 Origin 的寫法；不合格回 false（PLT-22）
func ValidOwnerLogin(login string) bool               // web.owner_login 像 email（含 name@github）
func ValidSiteColor(color string) bool                // 空或 #rgb／#rrggbb
func NormalizeOwnerLogin(login string) (string, error)          // 去空白、檢查（LoadHome 與 settings 的 web.set_* 共用，PLT-22）
func NormalizeAllowBoards(raws []string) ([]string, error)      // 整理成 Origin、擋重複（回新的一份）
func NormalizeSites(sites []WebSite) ([]WebSite, error)         // url 同上、color 去空白並檢查
const PointerFile = "evergenie.local.toml"
func ReadPointer(checkout string) (string, error)    // 指標檔的家（PLT-21）
func PointerHome(exe string) (string, error)
func IsDownloadInstall(dir string) bool             // dir 是下載版的安裝資料夾（versions/、current、VERSION，不是 git repo；UPD-11）
func DownloadCurrentVersion(app string) string      // current 指到的版本資料夾名稱
func DownloadInstallOf(exe string) string           // exe 是下載版某一版的執行檔時回安裝資料夾 <app>
const DownloadVersionsDir, DownloadCurrentLink, DownloadVersionFile = "versions", "current", "VERSION"
const DownloadFixedDir = sdk.DownloadFixedDir       // "fixed"：<app>/fixed/，發行檔 fixed/ 裡、要一直待在同一個路徑的檔（UPD-15）；名字只定義在 sdk（引擎也用）
const DownloadToolsDir = "tools"                    // <app>/tools/：主人同意後才下載的編譯工具（tools/go，packages PKG-65）；升級、退回不碰
func CopyVerified(dst io.Writer, src io.Reader, size int64, wantSHA256 string) (int64, error) // 照大小與 sha256 收下載的內容（PLT-27）
func ProgramDir(exe string) string                  // 程式資料夾：執行檔解開符號連結的上上層；解不開回空字串（PLT-28）
const ProgramDocsDir, ProgramDocsList = "ai_docs", "bundled.txt" // 跟著程式的說明文件與清單（PLT-28）
type ProgramDoc struct{ Path, About string }        // 清單的一行：相對 ai_docs/ 的路徑、一句話說明
func ParseProgramDocs(text string) ([]ProgramDoc, error) // 讀清單的文字（PLT-28）
func ReadProgramDocs(program string) ([]ProgramDoc, error) // 讀 <程式資料夾>/ai_docs/bundled.txt
func FindProgramDoc(docs []ProgramDoc, name string) (ProgramDoc, bool) // 照名字找清單上的一份（PLT-28）
func ExtractTarGz(archive, dest string, opt UnpackOptions) error                          // 安全解開 tar.gz（PLT-27）
func OpenDownload(ctx context.Context, client *http.Client, raw string, timeout time.Duration) (io.ReadCloser, int64, error) // 打開下載來源（PLT-25；失敗回 *DownloadError）
```
API：`sdk/common.go`（`ErrorCode`、`Error`）。

### P1 骨架對照
- P2 已實作可並行的 SystemClock／FakeClock、ULID、AtomicWrite（暫存檔與目錄同步）、Quiet、Home／Trust 設定驗證、共用錯誤型別與每日 JSON 日誌。
- Evidence: `internal/base/platform/platform_test.go` → `TestLoadHomeDefaults`、`TestLoadHomeRejectsBadField`、`TestTrustConfiguration`、`TestAtomicWriteCrash`、`TestQuietCrossMidnight`、`TestQuietDeferDefaultSlots`、`TestFakeClockAdvance`、`TestSecretsNeverLogged`、`TestErrorCodesAndIDs`。
- 生命週期維持無副作用；`NewLogger(home *Home, module string)` 使用家的 Clock 與時區，每次寫入開啟並關閉當日檔案，同時輸出 stderr。Home 的日誌值僅含模式與時區；不讀秘密內容。不實作 P3 的 system.alert。

## 事件
None.

## 儲存
見 `schemas/home.md`（`home.toml`、`settings/trust.toml`）。

## 依賴
`sdk`（錯誤碼與型別）、標準庫、第三方的 TOML（BurntSushi/toml）與 ULID（oklog/ulid）函式庫。不依賴任何 internal 套件。

## 錯誤
`ERROR_CODE_INVALID`（設定欄位不合格）、`ERROR_CODE_NOT_FOUND`（沒有 `home.toml`）。

## 不變條件
- `SetHomeValue` 前後：備份檔內容等於改之前；改成不合格的值（例 `web.port = "文字"`）之後 `home.toml` 與改之前逐字相同、回錯誤；`schema` 與不存在的路徑往字串底下設被拒絕。
- 文件正本由 CI 檢查，七類規則各有合規與違規測試。Evidence: `internal/tools/doccheck/doccheck_test.go` → `TestDocumentRules`、`TestRepoPasses`。
- 依賴檢查照路徑規則（`architecture.md`「依賴規則」，2026-11-01 舊的依賴表與 Deps 欄位對照退休）：底層不 import 業務塊、業務塊不互相 import、底層之間照「底層模組」表；工具測試覆蓋合規與刻意違規。Evidence: `internal/tools/depcheck/depcheck_test.go` → `TestRepoIsClean`、`TestCatchesViolation`、`TestCatchesFeatureToFeature`、`TestBaseOrder`、`TestLayoutRules`；`pathrules_test.go`。
- `LoadHome` 對相同輸入結果相同，缺欄位的預設與 `schemas/home.md` 的預設欄一致。
- `AtomicWrite` 在寫到一半被殺時，目標檔保持舊內容或新內容，不會是半個。
- `Quiet.Contains` 在跨午夜的時段（`22:00-07:01`）正確。
- `LoadHome` 對不認得的信任等級回 `Invalid`；等級的預設與優先順序由 `feature/trust/rules` 判定。
- `LoadHome` 與日誌都不含秘密檔的內容；環境裡塞進秘密後整輪 log 搜不到。
- `[runtime]` 的 `board_port` 蓋過 `web.port`、超出 1–65535 回 `Invalid`；`service_label` 只收英數字、`.`、`_`、`-`（不合法回 `Invalid`），`backup_dir` 展開 `~`，這兩個核心自己用、不交給引擎；其餘欄位覆蓋每個帳號的 `engine_config`，沒寫就不動帳號原本的值。
- `[ids]` 沒寫＝`SEC-`、`GOAL-`；寫了照寫的發號、認單號（`TestHomeIDPrefixesDefaultAndCustom`、`TestHomeIDPrefixesRejectBadValues`、`TestHomeRuntimeServiceLabelAndBackupDir`）。
- `[tasks] keep_review_open` 沒寫＝`false`：送驗收的工作 session 照舊關；`true` 時不關、被關掉的接回且不占名額（`TestKeepReviewOpenDecidesWhetherReviewCloses`、`TestKeepReviewOpenResumesClosedReviewWithoutUsingSlot`，在 `internal/base/session`）。
- `home.toml` 的 `secrets.file = "~/.secret.sh"`、`backup.snapshot_dir = "~/bk"`、`sessions.workdir = "~"` 讀進來都是使用者家目錄下的絕對路徑；`/abs`、相對路徑、`~user/x` 不變。

## 測試計畫
- Q3：`TestConfigSetBacksUpAndRestores`（REQ-163，`internal/base/platform/config_test.go`）
- `TestLoadHomeDefaults`（REQ-119）、`TestLoadHomeRejectsBadField`（REQ-119）
- `TestAtomicWriteCrash`（REQ-3，用子行程在 rename 前被殺）
- `TestQuietCrossMidnight`、`TestQuietDeferDefaultSlots`（REQ-32、REQ-62）
- Q5：`TestTrustLevelValidation`（REQ-84；等級的先後在 trust 塊的 `TestLevelDefaultsAndPrecedence`）
- `TestSecretsNeverLogged`（REQ-120，把秘密塞進環境後跑一輪 log，grep 不得出現）
- P8 第二輪 S2（`update_test.go`）：`TestHomeUpdateSection`（遷移的兩項搬到 `update` 的 `migrations_test.go`）（REQ-280；`TestDecideTonight` 搬到 `update` 的 `state_test.go`）
- P8 S1：`TestParseVersion`、`TestVersionCompare`（REQ-277，`internal/base/platform/version_test.go`）
- PLT-27：`TestCopyVerified`、`TestExtractTarGz`（`archive_test.go`；升級解發行檔的那幾種壞包在 update 的 `download_test.go` 也照跑）
- PLT-28：`TestProgramDir`、`TestParseProgramDocs`、`TestReadProgramDocs`、`TestFindProgramDoc`（`program_docs_test.go`）；清單上的檔都在、帶的文件不提到沒帶的在 doccheck（DOC-12）；包裡剛好帶清單上的、解開後 `evergenie docs` 讀得到在 `make dist-test`；照技能 create-feature 的寫法讀三份在 `make clean-install-test`

## 已知限制
None.
