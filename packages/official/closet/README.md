# closet：衣櫃（給秘書看的用法）

衣櫃不屬於某一位秘書（沒有秘書樣板）。每位角色（人設）有一組衣服。看板上兩頁（作者 2026-11-03）：
- 兩頁上面都是一排角色頭像，一次只看一位（網址 `?persona=<代號>`，例 `/p/closet/showroom?persona=athena`；記得上次看誰）。
- **衣櫃**：只有已經擁有的衣服——現在穿哪套、輪流穿哪幾套；每套一顆「現在穿這套」，按了馬上換、今天都穿這套（那天不輪），明天起照輪流穿的繼續（輪流穿每小時輪一次），可以「復原」。沒擁有的不出現在這裡，也不能穿。
- **展示間**：所有上架的衣服（沒擁有的是剪影）、用點數兌換，還有「新增角色」「加一套衣服」——開到看板的角色頁（一位角色一頁的「衣服」分頁），上傳由套件 `closet-mine`「上傳衣服與角色」處理，見它的 README。
  設計中的衣服不在展示間（DEV-160 S5b，CLO-37）：一般家看不到；開發家在那位角色的一段裡有一行「N 張照片還沒採用：到角色頁的「修改」看」，採用、評論都在角色頁的「修改」分頁（見下面）。
- 展示間的點數卡會說「再 N 點可以換下一套」（夠了寫「可以換一套了」），並連到習慣頁（「習慣」套件開著才連）。
- 家裡一套衣服都沒有時，兩頁會照官方角色包的狀態說明：這一版沒有官方衣服包／正在下載或上次沒成功（會自動再試，另有「現在再試」按鈕）／已經同步但這位沒有衣服；
  秘書要看同一份狀態用 `evergenie personas pack status`，要再試用 `evergenie personas pack sync`。
- **審核**只在開發家（DEV-160，作者 2026-11-15）：新衣服先是「設計中」（`state = draft`，只用這個叫法），主人在角色頁的「修改」分頁對**每一張照片**按「採用」，
  每一張都採用了才能按「上架」（整套 `state` 改 `active`）；有意見按「評論」（見下面「角色頁的「修改」分頁」一節）。一般家的畫面沒有設計中、採用、評論。
  - **一般家沒有上架／下架**（作者 2026-11-15）：衣櫃寫的這個家確定不支援 `edit` 時，主人自己做的（`source = mine`）設計中衣服每一輪**自動上架**、它的照片標採用
    （秘書照開發家的做法送了 `draft = true`、以前留下的設計中都會這樣；衣櫃還沒寫、讀不到這個家支援什麼時不動）。不想要就在「衣服」分頁「刪掉這套」（拿得回來）。
    下架的我的衣服在一般家的「衣服」分頁列在「刪掉的」，「拿回來」就改回上架。一般家的畫面也沒有「上架」「下架」這些字。
  - 不要自己改 `adopted` 或 `state` 替主人採用、上架：採用要主人明說。
  - 生圖秘書（`art` 職位）只有開發家能有：樣板在開發工坊 `admin` 套件（選用的職位），一般家沒有（DEV-160 S5a，作者 2026-11-15）。開發家有啟用中的 `art` 才傳話給她；
    沒有（或停用中）時「修改」分頁不傳話，評論照樣記成 `closet.note`，分頁上說一句並連到「設定 → 職位與秘書」。所以生圖秘書上班時要看一下有沒有 `state = open` 還沒處理的 `closet.note`。
  - **舊的意見**：DEV-160 以前展示間「設計中」的「留言」「修改」寫的 `closet.note`（沒有 `photo`、也不是 `scope = outfit`）還沒辦完的照舊辦，頁面不再寫這種意見：
    `intent = comment` 只照留言內容辦（沒說要重做就不要重做、不要動「已採用」、不要新增衣服），辦完 `state` 改 `dismissed`；
    其他（`redo`）只重做那一張：用換圖請求換那張照片紀錄（closet-mine 的 `closet-mine.request` 帶 `photo_id`，見它的 README；`piece` 有 `h:` 前綴或 `level = 隱藏版` 的是隱藏版那張，不要動一般版的圖），
    系統自動把那張回到還沒採用，做完 `state` 改 `redone`。`result` 都寫一句話做了什麼（`evergenie data set closet.note <CN-編號> --json '{"state":"redone","result":"…"}'`），再告訴主人。
**兌換、點數、公開的圖都由套件的程式管，不要自己寫 `closet.unlock`、`closet.person`、`closet.state`、`closet.capabilities`**（寫了會被封存或覆蓋）。
**秘書不能代使用者兌換**：兌換請求只有使用者本人（看板或他自己的命令列）送的才算，秘書送的會被拒絕。
所有操作都用 `evergenie data`（種類名稱 `closet.<種類>`）。

## 加一套衣服（目錄）
**圖要先放好，再加資料。** 官方角色的圖放在官方角色的 `avatars/`（第 1 版的家是 `personas/avatars/`，第 2 版在家外面狀態資料夾的 `personas/avatars/`；使用者自己的角色一律放家裡的 `personas/mine/avatars/`），命名：

- 第一套 `base`：`<人設>.png`（例 `athena.png`）；其他套：`<人設>@<衣服>.png`（例 `athena@seaside.png`）
- 輪播照片從 `~2` 起連續編號：`athena@seaside~2.png`、`~3`…（第 1 張不帶 `~1`），一套最多 12 張
- 靜態圖副檔名 `png`／`jpg`／`jpeg`／`webp`；動態是同代號的 `.mp4`（只有兌換了的才會公開）
- 人設代號 `^[a-z][a-z0-9]*$`、衣服 id `^[a-z0-9][a-z0-9-]*$`；其他檔（`full/`、`*.locked.png`、`*.live.json`、`*.voice.mp4`）不會被讀

```
evergenie data add closet.outfit --id athena-seaside --json '{
  "persona": "athena", "outfit": "seaside", "name": "海邊", "desc": "夏天的海邊洋裝",
  "cost": 1, "state": "active", "source": "official", "order": 2
}'
```

- 編號一定是 `<人設>-<衣服 id>`（對不上的會被略過並記 log）。
- 每位的第一套是 `"outfit": "base"`、`"cost": 0`（免費、一開始就擁有）。`cost` 是兌換要幾點，0＝送的或免費。
- 主人自己的衣服（`source = mine`）也一樣能設 `cost`：沒擁有時公開資料夾只有剪影，兌換後才公開真圖、進衣櫃。
- 開發家（`home.toml [runtime] env = "dev"`，做角色與衣服的家）不遮：沒兌換的衣服也給真圖，畫面標「未兌換（開發家看得到全部）」；擁有、點數、兌換照舊，`closet.person` 那套多 `preview = true`。正式家不會這樣。
- 開發家的展示間多一排篩選：「全部」「可以升成官方」「已經是官方」「只在開發家（我自己的）」，選了就跨所有角色列出；網址 `?filter=promotable|official|mine`。可以升成官方的衣服在 `closet.person` 多 `promotable = true`（官方角色、`source = mine`、已上架、不是自己的隱藏版）。
- `state`：`active` 上架；`draft` 設計中（只有開發家用：不能兌換、展示間不列，在角色頁「修改」分頁每一張都採用了才能上架；我的衣服在那裡也能「放回開發中」；一般家的我的衣服自動上架，見上面）；`retired` 下架（已擁有的人照樣有，沒擁有的看不到；開發家「修改」分頁照樣有圖）。

## 隱藏版（另一組圖，開著時跟一般版一起列）
寫一筆 `closet.variant`、把圖照上面的命名放進它的資料夾（`personas/` 底下）就生效，不用改程式：
```
evergenie data add closet.variant --id night --json '{"name":"隱藏版","dir":"avatars/hidden","windows":["22:00-02:00"],"button":true}'
evergenie data set closet.variant night --json '{"on":true}'   # 時段外也打開；false 關掉
```
- 開著＝`on` 或在 `windows` 的時段裡；`button: true` 時衣櫃與展示間頂端有開關。
- 只加在擁有的與設計中的衣服上：開著時一般版的圖照樣在，隱藏版的圖（照片、影片、全身照）接在後面一起列，不是二選一（2026-11-13 作者）。設計中的在「修改」分頁每張各自採用、評論（隱藏版的照片紀錄 `hidden = true`；舊的意見的 `piece` 隱藏版前面加 `h:`，例 `h:half-2`、`h:full`）。沒擁有的照舊只有剪影。
- 隱藏版只有一組（DEV-160）：幾筆 `closet.variant` 的開關、時段合起來管同一組（任一筆開著就開著，頁面的開關一次改全部）；哪些圖是隱藏版看照片紀錄的 `hidden`，`dir` 只是衣櫃登記舊圖時認的地方。

## 照片紀錄（DEV-160，2026-11-15 起）
每個圖檔（每張半身、全身照、每支影片）一筆 `closet.photo`：屬於哪位角色哪一套（`persona`、`outfit`＝衣服編號）、類型（`type` half／full／video）、
第幾張（`order`）、影片配哪張第幾支（`of`、`clip`）、隱藏版（`hidden`）、檔案在哪（`source` official／mine＋`file` 相對路徑）、採用了沒（`adopted`）。
編號 `<衣服編號>--<類型><第幾張>[-c<第幾支>][-h]`，例 `athena-seaside--half2`、`athena-seaside--video1-c2`、`athena-seaside--full1-h`；編號只是名字，看欄位就好。
- **衣櫃、展示間、角色包、角色匯出、改代號都只讀這份紀錄，檔名不再帶意思**：改代號（`home tidy`、`personas promote`）、升成官方都不改檔名，只改紀錄（升成官方、收成自己的時檔搬到另一邊的同一個相對路徑）。
- **衣櫃自己補登記**：每一輪把還沒有紀錄指著的圖照上面的舊命名登記起來（上架的整套每張算採用、設計中的照舊的 `adopted` 清單；不認得的檔不碰、只列出）。
  只新增紀錄、不動任何檔，跑幾次都一樣；新複製進來的圖下一輪就登記。官方角色放在家外的家，官方的照片紀錄跟著角色包來，不寫進家裡。
  這套衣服已經有照片紀錄之後才補登記的圖（之後才複製進來的舊命名檔），開發家一律算還沒採用（主人看過再採用）；第一次轉換（這套一筆都沒有）照上面的規則。
  預演只讀：`cd packages/official/closet && go run ./cmd/closet-photos --dry-run --home <家的複本>`。秘書不要自己新增 `closet.photo`，也不要自己改 `adopted`（換圖用換圖請求，見下面「修改」分頁一節）。
- **退路**（登記錯了）：把登記錯的那幾筆封存（`evergenie data rm closet.photo <編號>`，只封存紀錄、不刪檔），衣櫃下一輪會用新的編號（`<編號>-r2`）重新登記；檔一直沒動過。
- **S3 起**新上傳的檔存成 `personas/mine/photos/<照片編號>.<ext>`。

## 看狀況
```
evergenie data get closet.state main          # 點數：賺到、花掉、現在可以用的
evergenie data get closet.person athena       # 這位角色現在穿什麼、每套有沒有擁有
```
沒兌換的衣服，公開資料夾裡根本沒有真圖，只有模糊的剪影（霧面，看不出臉）；使用者兌換後才會公開。

## 自己的隱藏版（主人自己放的，預設關）
自己的隱藏版就是一筆隱藏版：圖放 `<家>/personas/mine/adult/`，命名跟正常版一樣（`<人設>[@<衣服>][~N].<ext>`、同代號 `.mp4`、
`full/<代號>.<ext>`）；只換**已經擁有的**衣服（跟上面的隱藏版一樣），兌換共用，不另做一套衣服。
- **家設定沒打開就完全不讀、不顯示**：開關是衣櫃的擴充點 `closet.adult`，主人在看板「設定 → 套件」把它寫成 `on` 才算開
  （其他值、沒設都當關）。這個擴充點宣告了 `owner_only`：**只有主人本人能改**，秘書寫會被拒（「只有主人」看的是請求或設定的署名 `by = owner(…)`，是防秘書弄錯、不是安全機制：家裡的程式都互信，D-19。）；主人沒打開時被問到，就說「要你在設定裡打開自己的隱藏版」。
- 打開後才作用：算自己的隱藏版的資料夾（`dir` 有一段叫 `level`，或任一段名字含 `adult`／`nsfw`／`r18`／`成人`，這幾個是程式認資料夾的關鍵字；例 `mine/adult`、舊家的 `avatars/level/hidden`，跟角色包同一份規則）沒打開時整筆不作用、不列在開關裡，公開資料夾也不會有它的圖；
  衣櫃程式每 3 秒看一次開關，一變就重算：關掉後幾秒內換回正常版、圖從公開資料夾刪掉。
- 圖一律走 `closet-mine` 的請求放（加 `"adult": true`），第一次放時它會建好 `closet.variant/adult`
  （`dir = mine/adult`、`button = true`、預設關著）；要自動時段就改那筆的 `windows`。那筆被封存或 `dir` 被改掉時，
  上傳自己的隱藏版的圖的請求會被拒並說怎麼恢復（改回 `dir`，或另建一筆 `dir = mine/adult` 的隱藏版）。
- **不匯出、不進角色包、不能升官方**：`evergenie personas export` 不帶任何 `adult` 資料夾的圖與紀錄，`personas pack build` 也不收。

## 升成官方（只在開發家）
一套採用了的主人自己的衣服要變成官方的（放進這個家的官方位置；要帶到別的家，還要在 `home.toml [admin] public_personas` 列這位角色、再建角色包），由套件 `closet-mine` 處理，用法見它的 README「升成官方」。
主人在看板按的「升成官方（公開給所有人）」在角色頁「修改」分頁那一套的塊裡（預演→確認→送 `closet-mine.request`）；那套有沒採用的照片會被拒、說哪幾張。
升官方換了 id 時，新的那筆帶 `from = <舊編號>`；衣櫃只認 closet-mine 寫的、而且只有一筆指著那個舊編號的，把舊編號的兌換紀錄搬到新編號（舊的封存，點數不變），所以兌換過的照樣擁有。

## 這個家支援什麼（能力）
衣櫃的程式每一輪寫 `closet.capabilities/main`：`supports` 是這個家支援的能力——每個家都有 `mine`（自己的衣服：新增／刪一套、上傳／刪單張、要不要用點數換），開發家多 `edit`（整套與單張的狀態、評論、篩選）；有 `edit` 而且開關 `closet.edit-tab` 沒寫 `off` 再多 `edit-tab`（角色頁掛「修改」分頁、連過去的入口才出現；主人改用別的套件管設計時可以關，SEC-12）。
頁面與核心都只看這一筆、不自己判斷是哪種家；別的套件的分頁要「這個家能修改衣服才出現」，在 `package.toml` 那個頁面寫 `show_when = "cap:closet.edit"`。
```
evergenie data get closet.capabilities main   # supports：能力；actions：登記好、這個家能用的動作
```

## 在衣櫃的頁面上加一顆鈕（登記動作）
使用者自己的套件要在衣櫃的頁面上對整套或單張加一顆鈕，由**那個套件的秘書**寫一筆 `closet.action`（一筆一個動作，編號 `<套件 id>-<動作名>`）；不要了就封存。
```
evergenie data add closet.action --id studio-move --json '{
  "package": "studio", "label": "搬到別處", "scope": "outfit", "needs": "edit",
  "request_kind": "studio.request", "request_fields": ["persona", "outfit"], "request_set": {"type": "move"},
  "status_kind": "studio.review", "status_field": "state",
  "status_labels": {"done": "搬好了", "stale": "改過要再搬"}, "filter_label": "搬過的"}'
evergenie data rm closet.action studio-move        # 不要了（rm 是封存）
```
- `scope`：`outfit` 對整套、`photo` 對單張；`needs`：這個家要有哪個能力才列（`mine` 或 `edit`）。
- 按下去，頁面寫一筆 `request_kind`（**你的套件自己宣告的種類**）：`request_set` 的固定欄位，加上 `request_fields` 照那一套（或那一張）填的 `persona`（角色代號）、`outfit`（`closet.outfit` 的編號）、`photo`（照片編號，只有 `scope = photo`）。這些欄位都要寫在那個種類的規格裡（`schemas/<種類>.toml`）。衣櫃不處理這筆請求，你的套件的秘書自己做。
- 狀態（選填）：頁面在 `status_kind`（也是你的種類）裡找 `request_fields` 都對得上的那幾筆、取最新的，把 `status_field` 的值照 `status_labels` 換成白話顯示；有 `filter_label` 就多一顆篩選鈕，只留狀態是 `filter_values`（沒寫＝`status_labels` 的全部值）的。
- 寫完約一秒，衣櫃驗過才放進 `closet.capabilities` 的 `actions`；不在裡面就是不合格（編號不是 `<套件 id>-` 開頭、種類不是你的套件宣告的、欄位不在規格裡或組合不對、你的套件沒啟用、是官方套件），原因在衣櫃的 log。欄位的完整說明在 `schemas/action.toml`。

## 角色頁的「修改」分頁（只在開發家）
這個家支援 `edit`（開發家）時，角色頁（`/personas/<代號>`）最後一個分頁是「修改」：一位角色所有衣服的每一張照片（`closet.photo`）與狀態，主人在這裡改：
整套「上架」「下架」（`closet.outfit` 的 `state`；開發中的要每張照片都採用了才能上架）、「放回開發中」（我的衣服、不是基本款，`state = draft`）、
「升成官方（公開給所有人）」（可以升的我的衣服）、單張「採用」「取消採用」（`closet.photo` 的 `adopted`；畫面上那張剛被換過就不寫，請主人再看一次）、
一般版／隱藏版（`hidden`，只是狀態、不搬檔）、半身／全身（`type`；影片不能改類型）。這些都是主人按的，**秘書不要替主人採用或上架**。
隱藏版開關關著時，隱藏版的照片整張不列、沒有「顯示隱藏版」（`closet.person` 的 `hidden_off` 列這一輪因為開關關著不公開的編號）。

主人按「評論」（整套或某一張；官方的衣服也能評），頁面寫一筆 `closet.note`（`intent = comment`、`state = open`；某一張的有 `scope = photo`、`photo`＝照片編號，
整套的是 `scope = outfit`、`piece = outfit`），**評論本身不改採用**，再用 `bus.send` 把評論轉給 `art` 秘書（家裡沒有啟用中的 `art` 就不傳，評論照樣記下）。
**怎麼分**：`closet.note` 有 `photo`、或 `scope = outfit` 的，是這個分頁的評論，照這一節辦；兩個都沒有的是 DEV-160 以前展示間寫的，照上面「舊的意見」辦（留言辦完只填 `dismissed`、不動採用）。
**秘書的做法**：
- 照評論的內容辦：評論沒說要重做就不要重做、不要新增衣服；看得懂要做什麼才做，看不懂就問主人。
- 圖在哪：`evergenie data get closet.photo <照片編號>` 看 `source`（`official`＝角色資料夾 `personas/`，`mine`＝`personas/mine/`）＋`file`（相對於它的路徑）。
- **真的換了新圖**：用換圖請求（`closet-mine.request` 的 `photo`／`full`／`video` 帶 `"photo_id": "<照片編號>"`，寫法見 closet-mine 的 README；
  我的照片、官方的照片在開發家都走這條；官方衣服的隱藏版照片換圖請求會拒，要換先跟主人說），程式換檔、舊檔留備份，**系統自動把那張回到還沒採用**，主人重新看過再採用。不要自己覆蓋檔、也不要自己改 `adopted`。
  整套的評論換了哪幾張，就送那幾張的換圖請求。
- 處理完回填那筆 `closet.note`：換了圖寫 `state = redone`，沒換圖（只是回話、改了別的）寫 `dismissed`；`result` 寫一句話做了什麼：
  `evergenie data set closet.note <CN-編號> --json '{"state":"redone","result":"…"}'`，再告訴主人。主人在那張（或那套）底下的評論紀錄看得到時間、內容、狀態與你寫的 `result`。
- 生圖秘書上班時看一下有沒有 `state = open` 還沒處理的 `closet.note`（家裡沒有 `art` 時頁面不傳話）。
