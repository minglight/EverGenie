# Schema：package（套件 manifest）

Status: DRAFT — 設計 2026-10-23（P0-S5），pending author review. 共通規則見 `README.md`。

一個套件是一個資料夾（D-2）：manifest 加宣告式內容；需要跑程式時附一個獨立行程。

## 路徑
- `packages/official/<id>/package.toml`：官方（唯讀，跟著發版）
- `packages/installed/<id>/package.toml`：別人分享的（唯讀，跟著作者）
- `packages/mine/<id>/package.toml`：自己的（更新永遠不碰；fork 也放這，D-3）
- 同一個資料夾下：`agents/<id>/agent.md`（秘書樣板）、`personas/`、`jobs/`（排程樣板）、`pages/*.tsx`、`schemas/<種類>.toml`（這個套件自己的資料欄位規格，見「資料種類」；寫入一樣經過 `vault`）、`cmd/`（附程式的原始碼）
- 層級（tier）由所在資料夾決定，**不寫在 manifest 裡**（fork 搬過去不必改檔）。核心算出來之後帶在 API 上：`Package.tier`、選單項目 `MenuItem.tier`／`MenuEntry.tier`（核心項目算 `official`）、套件頁 `PageInfo.tier`，值是 `official`／`installed`／`mine`（畫面上 `installed` 叫「分享的」）；`PageInfo.source` 是從網址裝的分享套件的來源（`.source.toml`），沒有就空（SEC-302 S4，REQ-348）。
- 套件內角色的有效代號 `<套件id>.<slug>` 見下面 `[[contributes.persona]]` 與 `persona.md`；使用者套件的資料夾地圖與命名見 `conventions/user-features.md`（D-41）。

## 欄位
`package.toml`（TOML；`schema = 1`）：

| 區段.欄位 | 型別 | 必填 | 預設 | 說明 |
|---|---|---|---|---|
| `schema` | int | 是 | `1` | |
| `id` | string | 是 | — | 小寫英文加 `-`；等於資料夾名；全家唯一（`mine` 與 `official` 同 id 視為 fork，見 `fork`） |
| `name` | string | 是 | — | 顯示名稱 |
| `version` | string | 是 | — | semver |
| `description` | string | 是 | — | 一句話 |
| `kind` | enum | 是 | — | `data`（純資料）、`program`（附獨立行程，用 Go SDK，D-2） |
| `api` | string | 是 | — | 它講的協定版本，例 `evergenie.v1`；核心不認得就拒載並警告，不影響其他套件 |
| `optional` | bool | 否 | `false` | 選用套件：裝好時**預設不啟用**（`settings/packages.toml` 沒有記錄就是停用），設定精靈的「進階套件（選用）」列出來讓使用者決定；`evergenie packages enable｜disable <id>` 可以開關，秘書要先在對話裡問過使用者。選用套件開啟時一律寫明確的 `enabled = true`（REQ-176） |
| `dev_only` | bool | 否 | `false` | 只給開發用的家（例：開發工坊 `admin`）：看板「設定 → 套件」只在開發家（`[runtime] env = "dev"`）列出來；正式家沒開就不列、開著的照列讓人關掉（PKG-56）。CLI 不擋。比這個欄位舊的核心不認得它、會拒載整個套件，套件寫了它就要用 `core` 寫最低核心版本 |
| `core` | string | 否 | 空（不限制） | 要求的核心版本，寫法同 `requires[].version`（`^0.2.0`、`>=0.2.0`、`0.2.0`）；`platform.Version()` 不符 → `rejected`、白話說明；開發版（`0.2.0-3-gabc1234`）當 `0.2.0`，認不出版本號（短碼、`dev`）不擋只警告（REQ-282） |
| `requires` | list of object | 否 | `[]` | `{id, version}`：依賴別的套件（相依檢查，D-19） |
| `capabilities.read`、`.write` | list of string | 否 | `[]` | 用到哪些資料與能力，例 `vault:task`、`vault:pkg/fitness`、`notify`；用途是看清模組邊界與相依檢查，**不是安全機制**（D-19） |
| `capabilities.events` | list of string | 否 | `[]` | 訂閱或發出的 bus 事件名稱 |
| `program.entry` | string | `program` 必填 | — | Go main package 路徑（相對於套件資料夾），核心偵測改動後重編（REQ-92）。套件資料夾要有 `go.mod`，只要 `module` 與 `go` 兩行：sdk 不用 `require`／`replace`，核心用正在跑的程式旁邊附的 sdk 編（工作區 `go.work` 寫在 runtime，升級後跟著換版；packages PKG-66） |
| `program.restart` | enum | 否 | `always` | `always`、`on-failure`、`never`（REQ-96） |
| `[contributes] avatars` | 字串 | 否 | — | 只給附程式的套件：公開資料夾（`runtime/packages/<id>/public/`）裡放「現在的頭像」的子資料夾，檔名 `<人設>.png|jpg|jpeg|webp|mp4`；看板的 `/avatars/` 先看它（D-29）。名稱要符合套件 id 的格式；資料套件寫了或名稱不合 → `rejected`，`field = "contributes.avatars"` |
| `[[contributes.agent]]` | 陣列 | 否 | — | `{id, template}`：提供職位（例 `health`）；`template` 指到 `agents/<id>/agent.md`（格式見「秘書樣板」）；`id` 同秘書 id 的格式、套件內不重複 |
| `[[contributes.persona]]` | 陣列 | 否 | — | `{slug, path}`：提供的角色卡（格式同 `persona.md`；慣例放 `personas/<slug>.md`）。`slug` 要符合 `^[a-z][a-z0-9]*$`、套件內不重複；`path` 是套件內相對路徑、檔案要存在、不能經符號連結跑出套件資料夾；不合格 → 這個套件 `rejected`，`field = "contributes.persona"`。有效代號是 `<套件id>.<slug>`（`agent` 的角色庫第三個來源，REQ-313，見 `persona.md`）。**頭像位置**：套件內 `personas/avatars/<slug>.png`（動態版 `.mp4`），慣例路徑、不另在 manifest 宣告（REQ-317）；只有套件有效時才送 |
| `[[contributes.job]]` | 陣列 | 否 | — | `{agent, id, path}`：提供的排程工作樣板（見 `job.md`；檔案格式同排程檔：標頭 `name`、`cron` 等、內文是指令；`packages sync` 時才解析） |
| `[[contributes.data]]` | 陣列 | 否 | — | `{kind, schema, ids, prefix?}`：宣告一種資料（見「資料種類」）。`kind` 要符合 `^[a-z][a-z0-9-]*$` 且套件內唯一；`schema` 是套件內的相對路徑（檔案要存在）；`ids` 是 `seq`（核心發編號）或 `given`（呼叫者給編號）；`ids = "seq"` 時 `prefix` 必填、要符合 `^[A-Z]+-$`（例 `M-`），`given` 不能有 `prefix`。選填 `export`（匯出角色時跟著走，PKG-57、D-63）：`by_persona`＋`persona_field`（預設 `persona`；欄位值是被匯出的角色時這一筆跟著走）或 `by_folder`＋`folder_field`（必填；欄位是 `personas/` 底下的資料夾，裡面有被匯出角色的圖〔`<代號>[@…][~N].<ext>`、`full/<代號>…`〕時，這一筆和那些圖跟著走，別的角色的圖不帶）；只給 `ids = "given"`、欄位要在規格裡而且是 `string`、沒寫 `export` 不能寫 `folder_field`；不寫＝不匯出。選填 `export_omit`（字串陣列，只能配 `export`，PKG-58）：匯出（與官方角色包）時拿掉的欄位，像開關這類只屬於這個家當下的狀態；匯入時本機已有那一筆就保留本機的值；每個要是規格裡的欄位、不能是 `export` 用的欄位、不能是必填、不能重複。選填 `pack_only`（表：欄位名 → 可以公開的值的字串陣列，只能配 `export = "by_persona"`，說明卡 packages 的 PKG-68）：官方角色包只帶「每個列出的欄位的值都在清單裡」的紀錄，其他的建包不收、同步時包裡萬一帶了也不寫（少了欄位、值不在清單裡都算不公開）；主人點名的角色匯出不看它。例：`pack_only = { source = ["official"], state = ["active", "retired"] }`。欄位要是規格裡的 `string`、`enum` 或 `bool`（值寫 `"true"`／`"false"`，例 `hidden = ["false"]`）、不能在 `export_omit` 裡，每個欄位至少一個值、不能空白或重複，`enum` 的值要是它的選項。選填 `pack_only_note`（一行、60 個字以內，要配 `pack_only`）：被擋下的紀錄的白話名稱，建包的輸出用（例「還沒升成官方或還在設計中的衣服」）。沒寫 `pack_only`＝公開角色的每一筆都進官方角色包。`pack_only` 不能用角色代號的欄位。舊版核心不認得這兩個鍵、會拒絕整個套件，要分享給別人的套件先確定對方的核心夠新。紀錄指著檔的種類（選填，都只能配 `export = "by_persona"`，PKG-70，DEV-160）：`file_field` 是值為這筆的檔的 `string` 欄位（相對於擁有者的位置：`persona_source` 是 `mine` 的在 `personas/mine/`，其他在官方角色資料夾；例 `avatars/athena@sea.png`），寫了之後官方角色包與角色匯出**照紀錄帶圖**（`file` 指到哪就帶哪個檔，不再照檔名認是誰的、是不是隱藏版），改代號時檔不改名、只有從官方位置收成自己的時搬到 `personas/mine/` 的同一個相對路徑；不能是角色代號或 `persona_source` 的欄位、不能在 `export_omit` 裡。`export_only`（寫法與規則同 `pack_only`）：只有符合的紀錄跟著角色走——官方角色包、主人點名的角色匯出都只帶符合的、匯入與同步時萬一帶了也不收，不符合的紀錄指著的檔也不帶（例衣櫃照片 `export_only = { hidden = ["false"] }`：隱藏版不跟著走）。`pack_parent = { kind, field }`：上層是同一個套件另一種 `by_persona` 的資料，`field`（`string`）是上層的編號；上層那一筆沒進官方角色包（例被 `pack_only` 擋下的設計中的衣服），這一筆與它指著的檔也不進。舊版核心不認得這三個鍵、會拒絕整個套件。角色改代號時紀錄怎麼跟著走（選填，PKG-64；`evergenie home tidy`、`evergenie personas promote` 照這幾個鍵改紀錄，核心不認得個別套件）：`persona_field` 沒寫 `export` 也可以單獨寫（值是角色代號的 `string` 欄位；`export = "by_persona"` 時就是匯出用的那個欄位）；`persona_id` 是 `exact`（編號就是角色代號）或 `prefix`（編號是 `<角色代號>-<其他>`），只給 `ids = "given"`；`persona_prefixed`（字串陣列）是值為 `<角色代號>-<其他>` 的 `string` 欄位（例：指到某套衣服編號的欄位），不能跟 `persona_field` 重複；`persona_source` 是值為 `official`／`mine`（這筆的圖在官方角色資料夾還是 `personas/mine/`）的欄位，`string` 或選項含這兩個值的 `enum`，要配 `persona_field` 或 `persona_id`；角色從官方位置收成自己的（`home tidy --adopt`）時改成 `mine`。欄位不在規格裡、型別不對都算不合格。不合格 → 這個套件 `rejected`，`field = "contributes.data"` |
| `[[contributes.page]]` | 陣列 | 否 | — | `{id, title, route, component, slot?, slot_front?, show_when?, chat?, chat_agent?, merge?}`：頁面；`slot_front = true` 在該 slot 排最前面（今天頁的習慣卡）；`show_when` 只能配 `slot`（沒有 `slot` 或 `slot = "shell.menu"` 寫了 → `rejected`，`field = "contributes.page"`，PKG-69）：條件不成立這頁就不列（寫法同選單的 `show_when`，見下一列）；`component` 指到 `pages/*.tsx`（核心內嵌 esbuild 編譯，D-11）；`slot` 是掛在別的頁面的哪個 slot；`chat = true` 時看板在頁面旁邊放聊天（寬螢幕一欄、窄螢幕「聊天」分頁），`chat_agent` 是負責這頁的秘書（職位 id；在名冊上而且啟用就固定跟她聊、頁面頂端放她的橫幅，REQ-236；名冊沒有就讓主人選；要配 `chat = true`，掛在 slot 的頁不能有 `chat`；不合格 `field = "contributes.page"`）（REQ-235）；`merge = true` 宣告這頁在看板連多台時可以合併各台的資料（S3 只收宣告、原樣放進 `PageDecl.merge`，看板怎麼用見 S3-10；官方套件先都不開） |
| `[[contributes.menu]]` | 陣列 | 否 | — | `{id, title, icon, group_hint?, order, show_when?, page, front?}`：選單項目；`front = true` 表示日常該先看到（習慣與健康），預設選單樹排在待辦後面、訊息前面；`group_hint` 只是「建議放哪個群組」（影響預設選單樹），使用者自己組的選單樹優先（D-11）；`show_when` 是出現條件，認得 `agent:<職位 id>:enabled`（那位秘書在名冊上而且啟用）與 `cap:<套件 id>.<能力>`（那個套件「API 支援」這個能力，跟本檔的 `[capabilities]`〔讀寫哪些資料〕無關：套件啟用、宣告了資料種類 `capabilities`，而且它自己寫的 `<套件>.capabilities/main` 的 `supports` 字串清單裡有這個能力；套件自己算、核心只照這筆讀，說明卡 inbox 的 MNU-11）；不認得的寫法、讀不到都當不成立（不顯示）；`section`（符合 `^[a-z][a-z0-9-]{0,30}$`）＋`section_title`：同一個 section 的項目（可以跨套件）在看板合成一個入口、頁面上方是分頁（例：飲食、運動、身體 → 「健康」），只有 `section_title` 沒有 `section` → `rejected`（REQ-235）。**核心頁也能是分頁**（D-62）：核心的「介紹」`core.personas` 帶 `section = "personas"`、`section_title = "角色"`，套件宣告 `section = "personas"` 就成為「角色」頁的分頁（官方衣櫃的衣櫃、展示間）；分頁順序核心在前、再照 `order`；合成的名稱用同 section 裡第一個寫了的 `section_title`，所以加入核心 section 的套件可以不寫。核心頁不能關，「項目」開關只列套件 |
| `[[extension]]` | 陣列 | 否 | — | 擴充點（D-3）：`{id, kind, description, default?, owner_only?, agent?, until_record?}`；`kind`：`setting`（可改的設定，套件自己讀）、`prompt_append`（接在秘書指令後面的一段；`agent` 寫職位 id 就只給那位，不寫每一位都接；值上限 4000 位元組）、`page_slot`（頁面上留給別的套件掛東西的位置，沒有值）。`agent` 只能用在 `prompt_append`。`until_record`（只能用在 `prompt_append`）寫成「<這個套件宣告的資料種類>/<編號>」（例 `welcome/main`；編號 `^[A-Za-z0-9][A-Za-z0-9_.-]*$`）：那筆紀錄出現（含封存的）之後就不再帶這一段，給「只講一次」的話用（PKG-4）；格式不合、種類不是自己宣告的 → `rejected`，`field = "extension"`。`description` 只能一行、不超過 200 位元組；`prompt_append` 的 `default` 不超過 4000 位元組，而且別人分享的套件的預設值不會自動帶進秘書的指令（主人設了值才帶） |
| `[[migration]]` | 陣列 | 否 | — | 套件自己帶的資料轉換（DEV-154）：`{id, description, kind, rename?, values?, defaults?, remove?}`。`id` 小寫英文數字與 `-`（建議日期開頭）、套件內不重複，發出去之後不改；`description` 寫主人看得懂的白話，只能一行、不超過 200 位元組；`kind` 是自己宣告的資料種類；`rename = { 舊欄位 = "新欄位" }`、`values = { 欄位 = { 舊值 = "新值" } }`、`defaults = { 欄位 = 值 }`（缺才補）、`remove = ["欄位"]`，至少一種；不能動基本欄位（`id`、`kind`、`schema`、`created_at`、`updated_at`、`by`）。官方套件的轉換沒過會擋升級；自己的、分享來的套件沒過只停那個套件。核心升級時替使用者把舊紀錄轉過去（說明卡 packages 的 PKG-61）；官方套件拿掉欄位、換型別、拿掉選項、變必填都要附，不然發版檢查不過（PKG-62） |
| `[fork]` | 區段 | 只有 `mine` 的 fork 會有 | — | `{of, base_version, forked_at}`：fork 自哪個官方套件、基準版本（D-3、REQ-95）；以前的 fork 工具寫的，不手改；2026-11-12 起不再開放新的整包複製（DEV-154） |

內文：None.（沒有 Markdown 內文；說明放 `README.md`，選用。）

## 資料種類（D-28、REQ-173）
套件的資料是宣告式的：核心把 `contributes.data` 的每一種註冊成 vault 的種類 `<套件 id>.<種類>`，資料放 `vault/pkg/<套件 id>/<種類>/<id>.md`（YAML 標頭，一筆一檔）。秘書用 `evergenie data …` 讀寫，看板的套件頁面用同一組 vault API（`list`、`get`、`put`、`patch`、`archive`、`next_id`）讀寫；**沒有套件自己的 RPC**，加總、趨勢這類計算放在套件頁面或由秘書算。套件停用、解除安裝或被拒時資料留在原地、暫時看不到，重新啟用就回來。內文一律允許自由 Markdown。

欄位規格檔 `schemas/<種類>.toml`：
```toml
schema = 1
title = "餐點"            # 白話名稱（必填）
date_field = "date"       # 選填：data list --since/--until 用哪個欄位（型別要是 date）；沒寫就用建立日
[[field]]
name = "kcal"             # ^[a-z][a-z0-9_]*$；不能是 schema、id、created_at、updated_at、by、kind、archived_at
type = "number"           # string｜int｜number｜bool｜date｜datetime｜enum｜list｜map
required = false
enum = []                 # type = enum 時必填，其他型別不能寫
description = "大卡"
```
`number` 收整數與浮點數；`int` 收整數值；`date` 是 `YYYY-MM-DD`；`datetime` 是 RFC 3339。編號：`seq` 種類是 `<prefix><n>`（`NextId` 發）；`given` 種類的 id 要符合 `^[a-z0-9][a-z0-9-]*$`（例 `2026-10-25`、`main`）。核心管的欄位（`schema`、`id`、`created_at`、`updated_at`、`by`）自動寫入。

## 秘書樣板與 `packages sync`（D-28、REQ-174）
`agents/<id>/agent.md`：YAML 標頭 `duty`（必填，一句話職責）、`color`、`color_dark`（`#rrggbb`）、`mode`（`resident`｜`ondemand`｜`none`）、`persona`（選填，預設角色）、`optional`（選填，`true`＝選用職位：不自動帶出來，見下）＋內文＝職責全文。套件帶來的秘書職責與排程樣板**不在掃描時自動套用**：`evergenie packages sync <套件>` 先預演（會新增哪些秘書、職責差異的增刪行數、會新增哪些排程），主人同意後 `--apply` 才套用；新秘書用 `--persona` 指定的人設，沒指定就用樣板標頭的 `persona`（選填，人設代號或套件內角色 `<套件id>.<slug>`，REQ-324；現在不能用時預演就擋），再沒有就用 `home.toml` 的 `[defaults] persona`（都沒有就請主人先指定）、停用狀態建立；`--agent <職位>` 只同步那一個職位（REQ-238）；沒給 `--agent` 時略過名冊上還沒有的選用職位與它的排程（封存過的也算還沒有；回應的 `optional` 列出略過的；已經在名冊上的照一般職位同步，DEV-157），職責用 `WriteDuties`（不碰 addendum），排程的 `source` 是套件 id，已存在的排程不改。

## 來源記錄 `.source.toml`（REQ-282）
從 git 網址裝的套件（`packages/installed/<id>/` 或 `mine/`）資料夾裡多一個隱藏檔 `.source.toml`（核心寫，不手改；本機資料夾裝的沒有）：`url`（安裝時給的網址，不含帳號密碼）、`ref`（裝的 tag 或分支）、`commit`（實際抓到的）、`installed_at`（RFC 3339）。`package.outdated` 用 `url` 問來源有沒有比 `version` 新的 semver tag；`package.upgrade` 換新版時重寫它。複製到別處（fork、本機資料夾安裝）不帶這個檔。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `requires[].id` | 另一個套件 | 必須已安裝且版本符合；缺少就不載入這個套件並警告 |
| 出 | `contributes.menu[].page` | 本套件的 `contributes.page[].id` | 必須存在 |
| 出 | `contributes.agent[].template` | 套件資料夾內的檔 | 必須存在 |
| 出 | `fork.of` | 官方套件 id | 官方套件被移除時，fork 仍可用，只是不再提示更新 |
| 入 | server 的 `packages` | 這個套件 id | 由索引算 |
| 入 | 擴充點被使用者設定引用 | `<套件 id>.<extension id>` | 擴充點被移除時，看板警告但不壞（REQ-94） |

## 保存規則
- `official`、`installed`：解除安裝就移走（它們是可重新取得的）。
- `mine`：永遠不被更新或清道夫碰；解除安裝要使用者明確確認。
- 套件在 `vault/pkg/<id>/` 下的資料屬於使用者：解除安裝套件時**資料保留**，只是暫時看不到。
- 每個套件的資料一律放 `vault/pkg/<id>/`（D-27）：一筆一個 `.md`、YAML 標頭（`vault` 的共通規則），欄位規格在套件自己的 `schemas/<種類>.toml`（D-28）；附程式的套件從 `Hello` 的 `data_dir` 拿到這個路徑，寫入一律經 `VaultAPI`。

## 從舊系統遷移
None.
