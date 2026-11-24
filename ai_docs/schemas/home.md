# Schema：home（家的設定）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

這個家的全域設定：使用者、時區、安靜時段、帳號、模式，以及信任清單。需求：REQ-84、REQ-118–120。

## 路徑
- `home.toml`（家的根目錄）
- `settings/trust.toml`（信任清單，`SetPermission` 的輸入，D-7、REQ-84）；由看板「設定→權限」或使用者本人改，秘書不能改
- `settings/backup-sweep.toml`（DEV-163，system BSW-3）：每週的備份掃描的例外，主人自己加；只讀 `[[skip]]`：`path`（`~/` 開頭或絕對路徑，可以用 `*` 這類萬用字元）、`reason`（為什麼不算備份，報告「略過的例外」會寫）。沒有這個檔＝只用內建的例外；格式壞了這次只用內建的、報告記成出錯並通知主人
- `settings/migrations.toml`（P8 第二輪 S2、REQ-280）：`applied`（字串陣列）＝已經跑過的家的遷移 id；升級與 `serve` 啟動時由 `update.RunMigrations`（業務塊 `system/update`）維護，不要手改
- `runtime/upgrade/`：升級的狀態（`state.json` 進度、`check.json` 最後一次檢查、`skip.json` 跳過的版本、`decision.json` 今晚更新、`history.jsonl` 升級紀錄、`notified.json` 通知過的版本、`lock`）；不進 git、不備份
- `settings/sessions.toml`（P7-5、D-33、REQ-220）：`work_max`（整數 ≥ 1，預設 `3`）＝每台同時 `doing` 的工作 session 上限；沒有這個檔、沒寫、或寫得不合理（< 1、讀不懂）都用 3；每次開工作 session 前重讀，改了不用重開 `serve`。看板「設定 → 職位與秘書 → 同時進行的對話」可以改（`system.set_work_max`，只有主人、1～10，只動 `work_max` 那一行、先備份，SES-73）。可選 `schema = 1`
- 秘密檔：放在這台，路徑寫在 `home.toml` 的 `[secrets]`，工具只傳路徑不讀內容（REQ-120）
- 指標檔 `evergenie.local.toml`（2026-11-01）：**不在家裡**，在程式資料夾的根目錄（原始碼版是 EverGenie repo 的 checkout，不進 git，repo 的 `.gitignore` 排除；下載版是安裝資料夾 `~/.evergenie/app`，安裝程式寫的）。一個家＝一份程式＋一份資料，指標檔寫這份程式屬於哪個家。見下面「指標檔」

## 佈局（`[layout]`，DEV-145，2026-11-08）

家有兩種佈局，程式一律經 `platform.LayoutFor(home)`（核心）或 `sdk.RuntimeDir`／`MediaDir`／`SharesDir`／`OfficialPersonasDir`（套件、引擎，讀核心給的 `EVERGENIE_STATE`、`EVERGENIE_MEDIA`、`EVERGENIE_OFFICIAL_PERSONAS`）拿路徑，不自己 `Join(home, "runtime", …)`。找官方角色卡經共用入口 `sdk.OfficialCardPath`（套件，讀 `EVERGENIE_BUNDLED_PERSONAS`）／`sdk.OfficialCardIn`（核心）：官方角色資料夾先，沒有才用跟著程式的公開卡（主人在家裡 `personas/mine/` 有同代號的卡時不用它，agents 的 PRS-27）；刻意只看官方資料夾的例外（`recode`、角色包同步與建包、看板頭像）列在 agents 的 PRS-25。

| 東西 | 第 1 版（沒寫 `[layout]`） | 第 2 版（`version = 2`） |
|---|---|---|
| 暫存（sockets、logs、sessions、queue、backups、web…） | `<家>/runtime/` | `<state>/runtime/` |
| 密鑰（`[secrets] file = "secrets/…"`） | `<家>/secrets/` | `<state>/secrets/` |
| 附件 `/share/<名字>` | `<家>/runtime/share/` | `<家>/media/share/` |
| 圖庫等主人的檔案 | `<家>/runtime/art/` | `<家>/media/art/` |
| 官方角色（卡、`avatars/`、`wardrobe/`、`catalog/`、官方衣服目錄的紀錄 `records/`、角色包登記 `pack-registry.toml`） | `<家>/personas/`（紀錄在 `vault/pkg/`、登記在 `settings/personas-pack.toml`） | `<state>/personas/`（`official_personas = "home"` 時跟第 1 版一樣留在家裡）。公開角色的卡另外跟著程式（`<程式>/packages/official/personas/cards/`，唯讀，這裡沒有同代號的卡才用，PRS-25） |
| 別人的套件 | `<家>/packages/installed/` | `<state>/packages/installed/` |
| 主人的角色、套件、vault、settings | 家裡 | 家裡（不變） |

- `schema` 跟著佈局走：第 1 版 `schema = 1`，第 2 版 `schema = 2`（舊程式只收 1，碰到第 2 版的家會拒絕啟動，不會把家當第 1 版亂寫）。`schema = 2` 沒有 `[layout] version = 2`、或 `version = 2` 配 `schema = 1`，都回 `Invalid`（`schema`）。`home relayout` 會自動改這一行（退回時改回 1）。
- `[layout] official_personas`：沒寫＝官方角色在狀態資料夾；`"home"`＝留在 `<家>/personas/`。`home relayout` 預設不搬官方角色、寫成 `"home"`：角色包網址還沒設的家，官方角色（含主人自己做的圖與隱藏版）拿不回來，要跟家一起備份；加 `--move-personas` 才搬去狀態資料夾（角色包拿得回來的家才用）。發行角色包的家（`[admin] public_personas` 有寫，例如開發家）官方卡是主人寫的原稿，加了也不搬。其他值回 `Invalid`（`layout.official_personas`）。
- `[layout] version`：1 或 2（沒寫＝1，其他值回 `Invalid`）。`state`：狀態資料夾，可用 `~`；沒寫＝`~/.evergenie/state/<家的資料夾名>`（兩個家資料夾同名會撞，所以 `home relayout` 一律明寫）。
- 第 2 版的家裡只有主人的東西，整個資料夾給雲端同步備份即可。狀態資料夾的 `runtime/` 是暫存，但 `runtime/backups/` 是改設定與升級前自動留的小備份、不是暫存，不能刪；整個家的備份（主人手動做的與還原前留的，`modules/system.md` 的 HBK-2）在狀態資料夾的 `backups/`，也不能刪；`home relayout` 預演會列出 runtime 底下不在已知暫存清單的東西；`secrets/`（推播金鑰，不上雲）要留；`personas/` 只有角色包網址設好、`personas pack sync` 拿得回來時才能當可重建（還沒設網址的家要留著）。
- 搬佈局只用 `evergenie home relayout`（預設預演、`--apply` 搬；`--revert` 預演退回、`--revert --apply` 才真的退回；服務要先停），**不做成自動遷移**：舊程式讀不懂第 2 版（`schema = 2` 會拒絕啟動），退回程式前要先 `--revert`。
  - 搬法一律同一顆磁碟改名；紀錄在 `settings/relayout.json`，中途失敗自動退回。`home.toml` 先備份成 `home.toml.bak-<日期>-relayout`（同一天撞名加 `-HHMM`）。
  - 家裡文字檔（`vault/`、`personas/`、`packages/mine/`、`settings/` 的 `.md .toml .json .jsonl .yaml .txt`）裡 `<家>/runtime/art|share` 的絕對路徑改成 `<家>/media/…`，官方角色有搬時 `<家>/personas/{avatars,wardrobe,catalog,<代號>.md}` 改成狀態資料夾的位置（也認 `~/` 寫法）；原文存 `settings/relayout-rewrites/`，退回時還原。`<家>/runtime/<其他>` 與相對寫法不改，預演列出來。
  - **搬完要重裝服務**：launchd 設定檔的 log 路徑還指家裡，跑 `evergenie service install`（照這個家的 `service_label`；會順便載入、啟動）。退回後也一樣要重裝（log 路徑那時指狀態資料夾）。**搬家前就開著的 session 要重開**（它們帶著舊的環境變數）；重開前 `EVERGENIE_API_SOCKET` 指第 1 版位置時，CLI 與引擎 hook 會照 `sdk.ResolveAPISocket` 改連第 2 版的 socket，不會斷線。
  - 退回時某一步兩邊都有東西（例：第 2 版期間狀態資料夾又長出 `runtime/art`）就停下、已搬回的再搬去第 2 版，家維持第 2 版，不會回報成功。
  - 改角色代號（`home tidy`、`personas promote`，下面）的紀錄還沒收尾時，搬家與退回都擋下（`platform.PendingRecodeJournals`），先退回或 `--forget` 收尾那邊。
- 新家（`evergenie init`）一開始就是第 2 版、官方角色放家外（DEV-154 第 1 件，2026-11-13）：`schema = 2`、`[layout] version = 2`，不寫 `official_personas`；家裡只建 `home.toml`、`settings/`、`vault/`、`personas/mine/`。升級與角色包同步不寫家裡的檔（例外只有兩種：資料格式轉換，與寄給主人的通知 `vault/messages/`）。服務在跑時套件自己重算的檢視（例：衣櫃的點數檢視 `vault/pkg/closet/state/`，裡面有計算時間）照常會變，那是套件在寫自己的資料、不是升級寫的；發版整套測試的比對不算它。`init --state <資料夾>` 可以指定狀態資料夾（寫進 `[layout] state`）；沒給用預設位置、`home.toml` 不寫死。狀態資料夾已經有東西（另一個同名的家在用）或在家裡面就拒絕，家與狀態資料夾都不留半套。測試：`cmd/evergenie/initcmd_layout_test.go`；發版整套測試第 2、4 段（家裡的檔案清單與雜湊在升級、同步前後相同）。
- 搬家前整理用 `evergenie home tidy`（DEV-154 第 10 件，PRS-22；預設預演、`--apply` 才做、`--revert [--apply]` 照紀錄退回；服務要先停）：自己的角色改成 `my_` 開頭（卡、圖、名冊、套件紀錄、下面幾個設定一起改）、官方位置不是官方的卡列出來（`--adopt` 收成自己的）、第 2 版 `official_personas = "home"` 的家 `--move-official` 把官方的卡與圖搬到 `<state>/personas/` 並拿掉 `official_personas` 那一行、列出散著的 `.bak-`。開發家把自己的角色升成官方用 `evergenie personas promote`（PRS-23）。兩個都會改的設定：`[web] app_icon`、`[defaults] persona`、`settings/packages.toml` 的 `[claims.<職位>] persona`、`settings/persona-import.toml` 的鍵；只改那一行的值，註解與排列原樣。紀錄檔：`settings/home-tidy.json`＋原文 `settings/home-tidy-originals/`（0700）、`settings/persona-promote/<my_名字>.json`＋`<my_名字>-originals/`；一次只能有一份（還在就不做新的），全部退回後拿掉，家跟做之前逐檔相同；確定不退了用 `--forget --apply` 收尾（紀錄檔與原文改名成 `.bak-<日期>-done`，照備份規則留 30 天）。**順序**：紀錄還沒收尾（退回或 `--forget`）之前，`home relayout` 搬家與 `--revert` 都會擋下——中間換佈局（例：`--move-official` 搬去狀態資料夾的官方卡，退回第 1 版的家看不到；搬家改寫的文字檔對不上改完時的 sha256），改代號就退不回去。要把整理過又搬過家的家整個退回，先 `evergenie home tidy --revert --apply`，再 `evergenie home relayout --revert --apply`。

## 欄位

`[incidents] owner = "職位 id"` **已廢**（2026-11-02 S5）：以前是本家事故負責人；S5-2 刪掉 session 自動開的事故單之後沒有人讀它；寫著也不影響啟動，doctor 不再檢查。舊版的 `dispatcher`（收單分診）由家的遷移 `2026-11-01-incidents-owner` 抄成 `owner`（只在沒寫 owner、那位秘書在名冊時；`dispatcher`、`reviewer` 留著不讀）。舊 `[backup]`、`runtime.backup_dir`、servers 設定不再使用，保留不影響啟動。

`home.toml`（TOML；`schema = 1`，佈局第 2 版是 `schema = 2`）：

| 區段.欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema` | int | 是 | `1` | 第 1 版佈局 `1`、第 2 版佈局 `2`（見上面「佈局」） |
| `mode` | enum | 是 | `simple` | `simple`、`full`（通知偏好的簡單／完整）。**只影響看板怎麼顯示**（P6-6）：簡單模式的通知頁只有三張預設組卡片；其他畫面只拿它決定內部代號（職位代號、清單列的編號）露不露、任務單交付卡的「詳細」預設開不開（board WEB-26）；資料、API、CLI 不變。由看板「設定→通知」最上面的切換改（`system.set_mode`，備份後原子寫入，改完不用重開），也能用 `evergenie config set mode '"full"'` |
| `chat_display` | enum | 否 | `default` | `default`、`engineer`（對話的顯示方式）。**只影響看板怎麼顯示**：`default` 不露工具細節，`engineer` 顯示；由看板「設定→顯示」改（`system.set_chat_display`，備份後原子寫入，改完不用重開） |
| `publisher` | — | — | — | **廢除**（2026-10-31，計畫 `2026-10-31-dev-workshop-split` W6）：核心不讀，舊檔留著也沒作用（toml 不認得的欄位略過）。是不是開發 EverGenie 的家＝有沒有啟用 admin 套件；角色包的來源看 `[admin] public_personas`。`[admin]` 的 `agents`、`dev_home`、`repo`、`release_state` 由 admin 套件自己讀（ADM-25）；`dev_site`（跨台看開發家）2026-11-01 廢除（法律第 7 條：家與家不連線），沒人讀，舊檔留著也沒作用 |
| `user.name` | string | 是 | — | 使用者稱呼（秘書對他的稱呼、看板標題） |
| `user.language` | string | 是 | `zh-TW` | |
| `user.timezone` | string | 是 | — | IANA 時區，例 `Asia/Taipei`；所有 datetime 的預設時區 |
| `user.quiet_hours` | string | 是 | `22:00-07:01` | 不推播的時段（REQ-32） |
| `user.quiet_defer_to` | string | 是 | `08:30` | 提醒落在安靜時段時延到這個時間（REQ-62） |
| `voice.model`、`voice.effort` | string | 否 | `sonnet`、`low` | 語音入口（`web.voice_send`，CHT-9）猜給誰時用的模型與思考力度；實測 sonnet 一次 4～6 秒 |
| `voice.fallback` | string | 否 | `admin`，沒有就名冊第一位 | 語音入口猜不出來（引擎沒回、逾時、回的不是 JSON 或猜到名冊外的人）時交給誰（職位 id） |
| `slots.default` | string | 是 | — | 預設用哪一組提醒時間 |
| `slots.<組名>.label`、`.remind`、`.check` | string | 是 | — | 提醒預設時間（`remind`＝「提醒」，`check`＝確認／條件檢查／討論）；例 work：`09:00`／`15:00`，life：`08:30`／`18:00` |
| `accounts.default` | string | 是 | — | 預設帳號名稱 |
| `accounts.<名>.engine` | string | 是 | — | 這個帳號用哪個引擎套件（id）；不同帳號可以用不同引擎並排運作（REQ-83、REQ-144） |
| `accounts.<名>.engine_config` | table | 否 | `{}` | 該引擎自己的帳號設定；**核心不解讀、整包原樣傳給引擎**（`StartRequest.engine_config`），欄位由各引擎的說明卡定義（例：claude 引擎的 `config_dir`、`default_login`——用 Claude 自己的預設登入，ECL-64、`env_file`——開 session 前載入的 shell 檔，放 token 用，見 engine-claude 卡 ECL-23）。`init` 建的第一個帳號由引擎填（ENG-19） |
| `accounts.<名>.remote_access` | bool | 否 | `false` | 這個帳號的常駐 session 開遠端存取（引擎有這個能力才生效） |
| `accounts.<名>.usage_warmup` | bool | 否 | `false` | 標了的帳號，每天由 `usage-warmup` 排程送一則極小的訊息，讓額度視窗提早起算（REQ-86，Q4） |
| `sessions.autostart` | bool | 否 | `true` | 自動開啟並維持 enabled 且 mode=resident 的常駐對話 |
| `sessions.workdir` | string | 否 | 家的根目錄 | 啟動對話的預設工作目錄；Open 的 cwd 可指定另一個目錄 |
| `web.port` | int | 是 | — | 看板 port（切換時沿用舊看板的 `8850`） |
| `web.public_url` | string | 否 | — | 推播附的連結根（tailnet 的 https 網址）；`evergenie tailscale setup` 會寫（有 `web.service` 時是 `https://<服務名>.<tailnet 網域>`） |
| `web.service` | string | 否 | （沒寫＝用這台機器自己的 `tailscale serve`） | 看板用的 Tailscale 服務名稱，**不含 `svc:`**（D-43，見 `modules/system.md`）：命名 `genie[-dev]-<owner>-<machine>`（`<owner>-<machine>` 同家的標籤，開發家加 `-dev`；例 `genie-<owner>-mini`、`genie-dev-<owner>-office`）。`tailscale setup|status|down` 改用 `svc:<名>`，doctor 檢查這台有沒有掛上、後台有沒有核准（`config.tailscale`，黃字）。只能用小寫英數字與 `-`（一段 DNS 名稱，最長 63 字）；寫成 `svc:<名>` 讀進來會拿掉前綴，其他格式錯**不擋整個家**（doctor 黃字、`tailscale` 指令回錯）。服務要主人先在 Tailscale 後台建好，這台要是 tag 身分 |
| `web.app_icon` | string | 否 | — | 手機主畫面圖示用誰的頭像（人設代號，例 `artemis`）：伺服器從那位人設的頭像（`personas/avatars/<代號>.png`，或管這位的套件給的）裁成正方形產生圖示。沒寫：用 life 秘書的人設，沒有 life 就用第一位啟用的秘書；都沒有頭像就用內建的靜態圖示。見 `modules/board.md` |
| `web.owner_login` | string | 否 | — | 看板連多台（S3）：主人的 tailnet 登入（例 `amy@example.com`，GitHub 登入是 `name@github`）。別的看板跨站呼叫這個家時，請求的 `Tailscale-User-Login` 要等於它；沒寫＝不開放跨站。要像 email（`x@y`、不含空白），不合格回 `INVALID`（`field = "web.owner_login"`） |
| `web.trust_self` | bool | 否 | `false` | 這台是 tag 身分的機器（Tailscale 不帶 `Tailscale-User-Login`）時，讓「從這台自己的瀏覽器開別台看板、再連回這個家」的跨站請求過：沒帶登入、來源位址（`X-Forwarded-For`）是這台自己的 tailnet 位址就視為主人（WEB-130）。要先有 `owner_login` 與 `allow_boards`（缺了 doctor 黃字）；看板沒有設定入口，改 `home.toml` 或 `evergenie config set web.trust_self true`（只有主人） |
| `web.allow_boards` | list | 否 | `[]` | 哪些看板可以從瀏覽器呼叫這個家：每個寫 `https://主機[:port]`（瀏覽器 `Origin` 的寫法），不帶路徑、查詢、片段、帳密；主機要是 ASCII 網域（中文網域寫 punycode，不收萬用字元）或 IP（IPv6 加中括號、不帶 zone）；結尾斜線、大寫主機、`:443`、port 前導 0 讀進來時整理掉；重複或不合格回 `INVALID`（`field = "web.allow_boards"`）。`platform.NormalizeOrigin`（PLT-22） |
| `[[web.sites]]` `url`、`color` | 陣列 | 否 | — | 這個看板還要看哪些家（只給瀏覽器連，伺服器之間不連線）：`url` 是那台看板的 https origin（規則同 `allow_boards`，`field = "web.sites.url"`）；`color` 選填 `#rgb`／`#rrggbb`（`field = "web.sites.color"`），沒寫照那台的電腦代號給預設色。台名、環境、版本一律向那台的 `system.get_mode` 拿，不寫在這裡。這三個欄位改法：`evergenie web sites|allow|owner`，或看板經 `web.set_sites`／`set_allow_boards`／`set_owner_login`（只有主人，SET-8；`owner_login`、`allow_boards` 只能在這台自己的看板或 CLI 改，別台的看板跨站來改一律 403，WEB-79）。`owner_login`、`allow_boards` 改了不用重開，最多 30 秒生效（看板每個請求看 `home.toml` 的修改時間，WEB-78）。兩台都要設什麼見 `runbooks/add-board-site.md` |
| `secrets.file` | string | 否 | — | 秘密檔路徑；推播金鑰格式見 [notify-prefs.md](notify-prefs.md) |
| `sense.interval_min`、`.max_lines`、`.disabled`、`.exclude_repos` | int／list | 否 | `30`／`40`／`[]`／`[]` | 現況快照（REQ-47） |
| `integrations.<名>` | map | 否 | — | 外部整合開關（jira、confluence、calendar、chrome…）；屬於各套件的設定，上線前先放這裡原樣保留 |
| `content.*` | map | 否 | — | 內容分級設定（隱藏版，REQ-135）；只有角色包的來源家會用 |
| `defaults.persona` | string | 否 | — | 新建東西時的預設人設 slug：`packages sync --apply` 替套件新增秘書時用；沒寫就回 `INVALID` 請主人先指定（P6-3a，D-28） |
| `persona.style` | enum | 否 | （沒寫） | 說話層強度（P6-5a、D-31、REQ-130）：`a`（現況）、`b`（頭尾一句）、`c`（用說的）、`d`（全開）用核心內建文字；`custom` 讀 `personas/_style.md`。沒寫時：`_style.md` 存在＝`custom`、不存在＝`c`。設了 `custom` 但檔案不存在：退回 `c`，`doctor` 報一條。其他值回 Invalid。由看板「角色」頁（介紹分頁）或 `evergenie personas style` 改（只有主人）。 |
| `[[rules.docs]]` `id`、`title`、`path`、`section` | 陣列 | 否 | （沒寫 `[rules]`＝憲法 `docs/constitution.md`、宗旨與法律 `docs/projects/evergenie.md` 的「宗旨」「法律」段） | 規矩頁（D-44、`modules/system.md`）要呈現的文件，**順序就是位階**（第一筆最高）。`id` 小寫英文開頭、只用小寫英數字與 `-`，不能重複；`title` 頁面上的名字；`path` 相對家的資料夾（不能 `..`）或 `~/`、絕對路徑，要是 `.md`；`section` 選填，檔裡某個標題的文字（不含 `#`），只取那一段。寫了 `[rules]` 但 `docs` 是空的＝沒有規矩文件。寫錯或寫的檔不在：`doctor` 黃字 `config.rules`（不會紅字），頁面顯示原因 |
| `runtime.board_port` | int | 否 | （沒寫＝用 `web.port`） | 這個家的看板 port；有寫就蓋過 `web.port`（一台兩個家各用各的，D-35）。1–65535，超出回 Invalid |
| `runtime.url` | string | 否 | — | 開發家才用：從手機或別台打開這個家看板的網址（只收 `http://`／`https://`）；admin 套件的「開發家」頁當成連結（REQ-266）。核心不解讀其他用途 |
| `runtime.env` | enum | 否 | `prod` | 家的身分（2026-11-01）：`prod`＝正式家（跑正式版）、`dev`＝開發家（跑主線）。**主要拿來標記**：家的標籤加 `@dev`、看板最上面一條橘色橫條與分頁標題前加「開發｜」、手機主畫面名稱「開發 <machine>」與圖示的 DEV 帶、推播標題前加「開發｜」。不改推播、升級等行為（那是 `sandbox`）。**會跟著 `env` 改變的行為只有這幾項**（正本是 platform 卡 PLT-20 的清單）：秘書的共用規則（AGT-35）、設定頁列不列 `dev_only` 套件（PKG-56）、衣服「升成官方」只在開發家能做（closet-mine 卡 CMI-14）、衣櫃在開發家多一個能力 `edit`（closet 卡 CLO-32；別的套件的頁面用 `show_when` 跟著它）、**衣櫃在開發家不遮沒兌換的衣服**（closet 卡 CLO-27、CLO-29：`dev` 的家會把沒兌換衣服的真圖放進公開資料夾，所以 `dev` 只該寫在做角色與衣服的家，正式家不要寫）。其他值回 `Invalid`（`runtime.env`） |
| `runtime.owner` | string | 否 | `user.name` 轉小寫（空白換成 `-`） | 家的標籤的前半：主人代號（例 `amy`）。只能用字母、數字、`.`、`_`、`-`，不合法回 `Invalid`（`runtime.owner`） |
| `runtime.machine` | string | 否 | 這台的短主機名（第一個點之前、小寫；拿不到是 `local`） | 家的標籤的後半：電腦代號（例 `home`、`office`）。格式同 `owner`（`runtime.machine`）。同一台的正式家與開發家靠 `env` 分開（開發家標籤多 `@dev`），machine 寫一樣 |
| `runtime.sandbox` | bool | 否 | `false` | 沙盒家（開發家用 fixture 資料、假引擎時）：不推播手機（REQ-180）、不自動檢查與升級、`doctor` 不報官方套件被改過。跟 `env` 分開：`env = "dev"` 不代表沙盒 |
| `runtime.dev` | bool | 否 | `false` | **舊寫法**（2026-11-01 拆成 `sandbox`＋`env`）：`dev = true` 讀成 `sandbox = true` 加 `env = "dev"`（有明寫 `env` 時以明寫的為準）。新的家不要寫 |
| `runtime.service_label` | string | 否 | （沒寫＝`com.evergenie.serve`） | 這個家的 launchd 服務名稱：`evergenie service`（install、start、stop、restart、status）沒給 `--label` 時用它，plist 是 `~/Library/LaunchAgents/<名稱>.plist`；doctor 的「服務」那一項也看它。同一台跑兩個家（日常＋工坊，GOAL-2 W2）時，第二個家一定要寫（例 `com.evergenie.workshop`），不然兩個家搶同一個服務。只能用英數字、`.`、`_`、`-`，不合法回 `Invalid`（`runtime.service_label`）。不交給引擎 |
| `runtime.remote_suffix` | — | — | — | **退休**（2026-11-01）：讀到忽略，也不交給引擎。遠端存取名稱改成 `<角色名>-<家的標籤>`（見下面「家的標籤」） |
| `runtime.<其他欄位>` | any | 否 | — | 核心不解讀（ENG-4），原樣蓋到每個帳號的 `engine_config` 上（家的設定優先於帳號自己寫的）。例：`tmux_socket`——Claude 引擎用的終端機 socket 名稱（引擎預設 `eg`；開發家寫 `hg-dev`，不碰正式家的 session）；`cards = false`——Claude 引擎不寫帳號的 `agents/<id>.md` 秘書卡（同一台第二個家秘書同名時寫，ECL-16）。`[runtime]` 沒寫的欄位，帳號的 `engine_config` 不動 |
| `ids.tasks` | string | 否 | `SEC-` | 任務單的編號字首（GOAL-2 W3）：開新單照它編號（計數器不分字首，接著原本的數字），工作 session 認單號（`<秘書>-<字首小寫><n>`，例 `code-dev-73`）、看板任務單連結都照它。大寫英文加一個 `-`（`^[A-Z]+-$`）；不能跟提醒 `R-`、習慣 `H-`、訊息 `M-`、權限申請 `P-` 或 `ids.goals` 一樣，不合法回 `Invalid`（`ids.tasks`）。核心沒有另外的開發任務：單號只認這個字首（工坊設 `DEV-`）。改了字首，舊單的檔名與連結不會跟著改 |
| `ids.goals` | string | 否 | `GOAL-` | Goal 的編號字首（GOAL-2 W3）：開新 Goal 照它編號；任務單、提醒掛 Goal（`[[<字首><n>]]`）與秘書訊息裡找 Goal 編號都照它認。格式同 `ids.tasks`；不合法回 `Invalid`（`ids.goals`） |
| `tasks.keep_review_open` | bool | 否 | `false` | 要你驗收（`waiting_user` 且有 `proposed`，原本的 `review`）的單，工作 session 要不要開著（PLT-18、SES-28）：`true`＝不關、被關掉的（不是主人手動關的）自動接回、接回不占同時進行中的名額，驗收或改成不做了才關，退回時直接在同一個對話送退回意見；`false`＝照舊，送驗收、閒了就關。工坊家設 `true`（主人要看過程、秘書可能還沒做完） |
| `admin.public_personas` | list of string | 否 | `[]` | 公開角色的代號（`^[a-z][a-z0-9]*$`）：`personas pack build` 只收這些人的卡、頭像、公開版衣櫃、目錄卡；沒寫＝不建角色包（REQ-283） |
| `update.channel` | enum | 否 | `release` | 更新頻道（P8 第二輪 S2，REQ-280）：`release` 只升正式版、`safe` 也升每晚測過的版本、`off` 不自動檢查。背景的自動檢查與自動升級只在**明確寫了這個欄位**的家做（`evergenie init` 建的家都有寫） |
| `update.check_at` | string | 否 | `10:00` | 每天幾點檢查一次新版（HH:MM，家的時區） |
| `update.tonight_at` | string | 否 | `03:30` | 「今晚更新」幾點動手（HH:MM） |
| `update.core` | string | 否 | （執行檔所在的 repo） | EverGenie 程式（git repo，或下載版的程式資料夾，DEV-155）的位置，`~` 會展開；沒找到程式就顯示「這台不是用安裝程式裝的，沒辦法自動升級」 |
| `update.source` | string | 否 | （編進執行檔的，現在是空的） | 下載版（沒有 git 的程式資料夾，DEV-155）找新版的版本清單 `manifest.json` 的網址，照下載來源的規則（platform 卡 PLT-25）：`https://<主機>…`；`http://` 只收本機（`127.0.0.1`、`localhost`、`[::1]`，測試用）；測試也可以用 `file:///…`；沒寫用 `scripts/dist.sh --source` 編進執行檔的。寫錯回 `Invalid`（`update.source`）。原始碼版不看它 |
| `upgrade.channel` | enum | 否 | `stable` | `stable`、`nightly` |

### 家的標籤
`<owner>-<machine>`，`env = "dev"` 時加 `@dev`（例 `amy-home`、`amy-home@dev`）；`platform.Home.Label()`。用在：常駐秘書的遠端存取名稱 `<角色名>-<標籤>`（工作 session 再接 `-<單號>`）、看板標題旁的標籤（`system.get_mode` 回 `env`、`label`）。

### 指標檔
程式資料夾根目錄的 `evergenie.local.toml`（TOML；下載版是安裝資料夾 `<app>`，不是 `<app>/versions/<版本>/`，PLT-21）：

```toml
home = "~/evergenie-dev"   # 這份程式屬於哪個家；可 ~/ 開頭，相對路徑以程式資料夾為準
```

找家的順序：`--home` 旗標 > 環境變數 `EVERGENIE_HOME` > 執行檔所在程式資料夾（`bin/` 的上一層，符號連結先解開；下載版是安裝資料夾）的指標檔 > 各指令原本的行為（大多是「不知道家在哪裡」）。指令行程啟動時（`cmd/evergenie/pointer.go` → `applyPointerHome`）沒設 `EVERGENIE_HOME` 就把指標檔的家設成 `EVERGENIE_HOME`，所以連 API socket 也跟著走；`serve` 沒給 `--home` 時也用它。`init` 照舊不吃 `EVERGENIE_HOME`、也不看指標檔。檔案壞了或沒寫 `home`：只印一行警告，當作沒有指標檔。

`settings/trust.toml`（TOML；`schema = 1`）：

| 區段.欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `[[repo]]` | 陣列 | 否 | 每個可寫的 repo 一段 |
| `repo.path` | string | 是 | 路徑（`~/Workspace` 這種資料夾代表底下全部） |
| `repo.remote` | string | 否 | 遠端（`github.com-<帳號>:<帳號>/xxx`） |
| `repo.push` | bool | 是 | 可不可以 `git push` |
| `repo.writers` | list of string | 是 | 哪些職位 id 可以寫這個 repo |
| `[approve]`、`[deny]` | 區段 | 否 | 核准與不開放的動作設定（REQ-84） |
| `approve.actions` | list of string | 否 | 預設空陣列，原樣對應 PermissionPolicy.Allow |
| `deny.actions` | list of string | 否 | 預設空陣列，原樣對應 PermissionPolicy.Deny |
| `default_level` | enum | 否 | 沒有單獨設定的秘書用的權限等級：`full`（最高）、`standard`（做大事先問）、`readonly`（只看）；沒寫＝內建預設：每一位都是 `full`。Q5，2026-10-25 |
| `[agent.<職位 id>]` `level` | enum | 是（有這段時） | 這位秘書的等級，優先於 `default_level`；值同上，大小寫與前後空白不計，其他值回 Invalid |

缺欄位使用空陣列；已知 actions 欄位型別錯誤回 Invalid，不忽略權限設定。這兩個區段內其他未知欄位忽略，合寫一筆 WARN，只記欄位名、不記值。S7 的 SetPermission.Mode 有無 trust.toml 都固定 ask；與 S11 的原生 session engine_config.permission_mode 無關。權限等級在每次開 session 時重讀（`EngineStartRequest.permission_level`），由引擎對應到平台的原生設定；`engine_config.permission_mode` 有寫時以它為準。等級由 `evergenie setup permissions`（只有主人）改：先備份到 `runtime/backups/trust/`，其他欄位保留、註解不保留。核心擋下的動作記在 `runtime/trust/denied.jsonl`（一行一筆 `DeniedAction`），`evergenie trust snapshot` 的快照在 `runtime/trust/snapshots/`；兩者都在 runtime，不進 git。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 入 | agent 的 `account` | `accounts.<名>` | 必須存在 |
| 入 | agent 的 `reminder_slot` | `slots.<組名>` | 必須存在 |
| 出 | `trust.repo.writers` | 職位 id | 必須存在於名冊 |
| 出 | `secrets.file` | 檔案路徑 | 只存路徑；不存在只警告（`doctor`） |

## 保存規則
- 不封存、不刪除；改動留在 git 歷史。
- 秘密內容不進 vault、不進 git、不進 log（REQ-120）。

## 從舊系統遷移
舊路徑：`agent.toml`（家的根）、`overrides/trust.toml`。

| 舊 | 新 | 轉換 |
|---|---|---|
| `[user]` `name`、`language`、`timezone`、`quiet_hours`、`quiet_defer_to` | `user.*` | 照抄 |
| `[slots]` | `slots` | 照抄 |
| `[accounts.<名>]` `config_dir`、`remote_control`、`usage_warmup` | `accounts.<名>` | `engine = "claude"`（舊系統只有這一家）；`config_dir` → `engine_config.config_dir`；`remote_control` → `remote_access`；`usage_warmup` 照抄；`computer_use` 不遷移（屬瀏覽器整合）；`type = "codex"` 的帳號不遷移（D-6：Codex 之後再做，REQ-88） |
| `[board]` `port`、`public_url`、`app_icon` | `web.*` | 照抄 |
| `[machines]` | 不遷移（法律第 7 條：每台一個家，新系統沒有機器表；只列進報告「不遷移」） | — |
| `[platform]`（`launchd_label`、`bin_paths`、`python`、`service_manager`、`codex_bin`） | 不遷移 | 舊 Python 的環境；新系統另有 `launchd` 設定，由安裝程式產生 |
| 舊 trust.toml 的 `[[approve]]`／分類器規則（id/text） | 待 P5 定義轉換 | 遷移在 P5 處理，S7 不轉換 |
| `[permissions]` `default_mode`、`deny` | `settings/trust.toml` 的 `[deny]`、以及 `agent.md` 的引擎設定 | 逐項對照，列進遷移報告 |
| `[integrations]`、`[secrets]`、`[sense]`、`[upgrade]`、`[content]`、`[admin]` | 同名區段 | 照抄 |
| `[residents]`、`[codex]` | 不遷移 | 舊 Python 常駐／Codex 的設定 |
| `overrides/trust.toml` 的 `[[repo]]` | `settings/trust.toml` 的 `[[repo]]` | 照抄 |
| `mode`、`publisher` | （新） | 預設 `full`（舊系統不分簡單／完整，等於全開）；`publisher` 2026-10-31 廢除，搬家不再寫 |
