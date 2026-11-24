# Schema：closet、closet-mine（衣櫃套件的資料）

Status: 實作 2026-10-26（P6-4b）。共通規則見 `README.md`；套件資料的註冊方式見 `package.md`「資料種類」；規則見 `modules/closet.md`；使用者自己的角色與衣服由套件 `closet-mine` 加（資料在本檔最後一段「closet-mine 的資料」；2026-11-03 起預設啟用，DEV-112）。

官方套件 `closet`（`kind = "program"`）宣告十一種資料，vault 種類名稱是 `closet.<種類>`，一筆一檔，放 `vault/pkg/closet/<種類>/<編號>.md`（YAML 標頭，D-27、D-28）。欄位規格的正本是套件資料夾的 `schemas/<種類>.toml`。**舊家的資料與圖片不匯入**（D-27）：搬家當天把舊 `personas/avatars/` 整個複製進新家、再由一次性腳本轉成下面的資料。

## 路徑
- 衣服（目錄）：`vault/pkg/closet/outfit/<人設>-<衣服>.md`（例 `athena-seaside`）
- 兌換紀錄：`vault/pkg/closet/unlock/<人設>-<衣服>.md`（編號＝同一套衣服的 outfit 編號，所以一套只會兌換一次）
- 輪播設定：`vault/pkg/closet/rotation/<人設>.md`
- 兌換請求：`vault/pkg/closet/request/Q-<n>.md`
- 角色檢視：`vault/pkg/closet/person/<人設>.md`
- 點數檢視：`vault/pkg/closet/state/main.md`
- 隱藏版：`vault/pkg/closet/variant/<隱藏版 id>.md`
- 衣服意見：`vault/pkg/closet/note/CN-<n>.md`
- 照片：`vault/pkg/closet/photo/<衣服編號>--<類型><第幾張>[-c<第幾支>][-h].md`（例 `athena-seaside--half2`，DEV-160；closet-mine 新上傳的是 `<衣服編號>--u<請求編號>`，S3）
- 這個家支援什麼：`vault/pkg/closet/capabilities/main.md`
- 別的套件登記的動作：`vault/pkg/closet/action/<套件 id>-<動作名>.md`

## 欄位
共通欄位（`schema`、`id`、`created_at`、`updated_at`、`by`、`kind`、`archived_at`）由核心管；套件的欄位不能跟它們同名。人設代號 `^[a-z][a-z0-9]*$`、衣服 id `^[a-z0-9][a-z0-9-]*$`；不合的程式不處理並記一次 log。

`outfit`（`given`；誰寫：搬家腳本、art 秘書 `evergenie data add closet.outfit --id <人設>-<衣服> --json …`、套件 `closet-mine`（`source = mine` 的，含刪一套、上架等請求）；頁面只在開發家角色頁的「修改」分頁改 `state`〔上架、下架〕，DEV-160 S5b 以前展示間的單張「採用」與「不要了」已拿掉）：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `persona` | string | 是 | 人設代號 |
| `outfit` | string | 是 | 衣服 id；`base` 是每位的第一套。編號必須等於 `persona` ＋ `-` ＋ `outfit`，不合的程式略過 |
| `name` | string | 是 | 名稱 |
| `desc` | string | 否 | 一句話說明 |
| `cost` | int | 是 | 兌換要幾點；0＝送的或免費 |
| `state` | enum | 是 | `active` 上架；`draft` 設計中的草稿（不能兌換、真圖照樣公開給使用者看；在開發家角色頁的「修改」分頁每張照片採用〔記在 `closet.photo.adopted`，CLO-34〕，每張都採用了才能按「上架」改 `active`，CLO-35；一般家的衣櫃與展示間不列，CLO-37）；`retired` 下架（已擁有的人照樣有，沒擁有的看不到） |
| `source` | enum | 是 | `official`（圖在 `personas/avatars/`）、`mine`（圖在 `personas/mine/avatars/`） |
| `order` | int | 否 | 同一位照 `order` 再照編號排；沒寫排最後 |
| `adopted` | list of string | 否 | 舊的單張採用清單（SEC-10，CLO-22 已廢）：`half-<n>`（第 n 張半身）、`full`（全身）、`video-<n>-<k>`（第 n 張的第 k 支影片）。DEV-160 S2 起採用記在 `closet.photo.adopted`（CLO-34），這欄不再寫，只在照片紀錄轉換時讀一次；以前展示間「設計中」按「採用」加進來、按「修改」拿掉。這個家當下的審核進度，匯出不帶（`export_omit`） |
| `from` | string | 否 | 升成官方前的舊編號（`closet-mine` 的升成官方寫，DEV-141）：只認 `by = closet-mine` 的那筆、同一位角色、舊的那筆已不在目錄裡、只有這一筆指著它；舊編號的兌換會搬到這一筆（新的 `unlock` 的 `request = carry:<舊編號>`，舊的封存，CLO-19）。匯出不帶（`export_omit`） |

`unlock`（`given`；**只有套件的程式寫**，`by` 是 `closet` 或搬家腳本 `migrate`；別的來源寫的會被封存並記 log）：`persona`（必填）、`outfit`（必填）、`cost`（int，必填；**habits 讀它算 `wallet.spent`，沒寫當 1**）、`name`（兌換當時的衣服名稱）、`at`（datetime，必填）、`request`（string，`Q-<n>`；升成官方搬過來的是 `carry:<舊編號>`，CLO-19）。只永久保存、不收回；唯一的例外是升成官方：舊編號那筆搬到新編號後封存（點數不變）。

`rotation`（`given`，編號＝人設代號；頁面或秘書寫）：`outfits`（list of string，要輪流穿的衣服 id）。有效清單＝勾的裡面還擁有的（照目錄順序），沒有就是第一套擁有的。另有 `wear`（string，「現在穿這套」指定的衣服 id）與 `wear_until`（string，RFC 3339，指定穿到什麼時候＝當天結束，服務所在機器時區的隔天 00:00）：**這兩欄只有程式寫**（要換請寫一筆 `request`，`type = wear`）；時間沒到而且還擁有才作用，不影響 `outfits`（CLO-30）。

`request`（`seq`，`Q-`；頁面或秘書寫新的，**程式只回填 `state`、`reason`、`unlock`**）：`type`（enum，必填：`unlock` 用點數兌換〔只認主人本人〕、`wear` 現在穿這套〔只有已擁有的能穿〕、`unwear` 復原；CLO-30）、`persona`、`outfit`（必填）、`state`（enum `pending`／`done`／`rejected`，必填，寫入時一律 `pending`）、`reason`（string，拒絕的白話原因）、`unlock`（string，成功時的兌換紀錄編號）。已處理的請求超過 7 天，程式在整點那輪封存。

`person`（`given`，編號＝人設代號；**只有套件的程式寫**，頁面只讀）：`persona`、`name`（取人設檔檔頭的 `name`，沒有就是代號）、`agent`（現在坐這個人設的職位 id；沒人坐＝空；多位坐同一人設取編號最小的啟用中那位）、`worn`（現在穿的衣服 id）、`picked`（bool，現在這套是「現在穿這套」指定的；false＝照輪流穿）、`worn_until`（string，RFC 3339，指定穿到什麼時候；沒指定是空字串）、`worn_photo`（int，`photos` 清單裡的第幾項，1 起算，含隱藏版開著時接在後面的那些；0＝沒有圖）、`rotation`（list，有效清單）、`outfits`（list of map，已排序；沒擁有的下架衣服不列），每項：`record`（outfit 編號）、`adopted`（list of string，只有 `state = draft` 的衣服有，其他沒有這個欄位；SEC-10）、`outfit`、`name`、`desc`、`cost`、`state`、`owned`（bool）、`rotating`（bool）、`source`（`official`｜`mine`，給 closet-mine 頁面分辨）、`full`（string，一般版全身照網址；**只有擁有的與草稿的衣服有**，沒擁有的是空字串，也不給剪影）、`full_hidden`／`full_hidden_variant`（隱藏版開著時隱藏版全身照的網址與隱藏版編號，跟 `full` 並存；沒有是空字串）、`photos`（list of map：`n`（那一版的第幾張，1 起算；擁有的與草稿的才有）、`variant`（隱藏版編號，一般版是空字串；隱藏版開著時一般版在前、隱藏版接在後面，兩版都列）、`url`、`real`（bool）、`video`（第 1 支動態網址或空字串）、`videos`（list of string，所有做出來的動態網址，第 1 支在前；沒有是空 list））、`status`（map，只有擁有的與草稿的衣服有，CLO-21：`photos` list of `{n, std:{img,videos}, hidden:{<隱藏版編號>:{img,videos}}}`、`full` `{std, hidden:{<編號>:bool}}`）、`hidden`（頂層 list of `{id, name}`：這個家能用的隱藏版，自己的隱藏版開關關著時不含它）、`photo_urls`（頂層 map，照片編號 → 公開網址：這一輪真的放進公開資料夾的每一張半身、影片、全身照，只列有顯示的衣服；隱藏版的公開檔名是 `h-<n>`、網址帶 `?v=h`；角色頁「修改」分頁照編號找縮圖，CLO-35）、`owner_hidden`（頂層 list of string：這位的照片裡自己的隱藏版的 `closet.photo` 編號，`hidden` 而且 `source = mine`〔或舊家放在官方位置、路徑看起來是自己的隱藏版的〕，不管開關開沒開都列；「修改」分頁不給改成一般版，CLO-18、CLO-35）、`computed_at`（string，RFC 3339）。

`photos` 的規則：擁有的與草稿的＝每張一項 `{url: /pkg-files/closet/o/…, real: true, video: 同張第 1 支 mp4 網址或空字串, videos: 同張所有 mp4 網址}`；沒擁有的（非草稿）＝只有一項 `{url: 剪影網址或空字串, real: false, video: "", videos: []}`（不透露照片張數；剪影做不出來——WEBP、壞檔——時 `url` 是空字串，頁面畫灰色人形）；沒有圖的衣服＝空 list。

`state`（`given`，只用 `main`；**只有套件的程式寫**，頁面只讀）：`earned`（int，習慣錢包賺到過的點數）、`spent`（int，認得的兌換花掉的）、`points`（int，`max(earned − spent, 0)`）、`wallet`（bool，找得到 `habits.wallet/main` 才是 true）、`variants`（list of map，每個合格的隱藏版照順序一項：`id`、`name`、`button`、`on`、`in_window`（現在在時段裡）；開著＝`on` 或 `in_window`）、`computed_at`。頁面頂端的點數與隱藏版開關讀這個，不自己算。

`capabilities`（`given`，只用 `main`；**只有套件的程式寫**，頁面與核心只讀；CLO-32、CLO-33，正本 `schemas/capabilities.toml`）：`supports`（list of string，必填：這個家支援的能力；`mine`＝每個家都有，`edit`＝只有開發家）、`actions`（list of map：別的套件登記、程式驗過、這個家撐得起〔`needs` 在 `supports` 裡〕的動作，照 `scope`、`order`、編號排；每項欄位同下面的 `action` 再多 `id`，`request_kind`、`status_kind` 是完整名稱，有 `filter_label` 就一定有 `filter_values`）、`computed_at`。核心的 `show_when = "cap:closet.<能力>"` 讀 `supports`（說明卡 inbox 的 MNU-11）。

`action`（`given`，編號 `<套件 id>-<動作名>`；**登記的套件的秘書寫**〔`evergenie data add closet.action --id … --json …`〕，不要了就封存；程式只讀，驗過才放進 `capabilities` 的 `actions`；CLO-33，正本 `schemas/action.toml`）：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `package` | string | 是 | 登記的套件 id；編號要以 `<package>-` 開頭；這個套件要啟用、沒被拒、不是官方套件 |
| `label` | string | 是 | 鈕上的字（一行、不超過 20 個字） |
| `scope` | enum | 是 | `outfit` 對整套、`photo` 對單張 |
| `needs` | enum | 是 | `mine`／`edit`：這個家要有這個能力才列 |
| `request_kind` | string | 是 | 按下去寫哪個種類：`<package>.<種類>`，登記的套件自己宣告的；衣櫃不處理這筆請求 |
| `request_fields` | list of string | 是 | 請求帶哪些欄位：`persona`（角色代號）、`outfit`（`closet.outfit` 編號）、`photo`（照片編號，只有 `scope = photo` 能用）；都要是 `request_kind` 規格裡的欄位（有 `status_kind` 時也要在它的規格裡） |
| `request_set` | map | 否 | 請求另外固定帶的欄位（例 `{"type": "move"}`）；值只能是字串、數字、true/false；不能是 `by` 或 `request_fields` 裡的；要是 `request_kind` 規格裡的欄位 |
| `order` | int | 否 | 同一個 scope 裡的順序（小的在前，一樣再照編號）；沒寫是 0 |
| `status_kind` | string | 否 | 狀態看哪個種類（`<package>.<種類>`）；頁面在這個種類裡找 `request_fields` 都對得上目標的那幾筆（不含封存），取 `updated_at` 最新的 |
| `status_field` | string | 有 `status_kind` 時是 | 狀態看那個種類的哪個欄位（要在它的規格裡） |
| `status_labels` | map | 有 `status_kind` 時是 | 狀態值 → 白話（一行、不超過 20 個字）；沒列的值不顯示 |
| `filter_label` | string | 否 | 篩選鈕的名字（例「已處理」）；要配 `status_kind` |
| `filter_values` | list of string | 否 | 篩選鈕留下哪些狀態值（都要在 `status_labels` 裡）；要配 `filter_label`；沒寫＝`status_labels` 的全部值 |

`variant`（`given`，編號＝隱藏版 id `^[a-z0-9][a-z0-9-]*$`；主人、秘書、頁面、使用者自己的套件都可以寫，程式只讀；規則見 `modules/closet.md` CLO-17）：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `name` | string | 是 | 給人看的名稱（開關上的字） |
| `dir` | string | 是 | 圖的資料夾，相對於 `<家>/personas/`（用 `/`，不能有 `.` 開頭的段）；裡面照「圖片的位置與命名」放，第 N 張不用連續 |
| `windows` | list of string | 否 | 每天自動開著的時段 `HH:MM-HH:MM`（本機時間，結尾可寫 `24:00`，可跨午夜；頭尾一樣不收） |
| `button` | bool | 否 | true＝衣櫃與展示間頂端畫開關 |
| `on` | bool | 否 | 開關；true＝時段外也開著 |
| `order` | int | 否 | 好幾個同時開著時先用小的（沒寫排最後，再照編號） |

`note`（`seq`，`CN-`，SEC-10、CLO-22；頁面在開發家角色頁「修改」分頁的「評論」寫新的〔CLO-35；DEV-160 S5b 以前是展示間「設計中」某張的「修改」「留言」〕，秘書重做完回填 `state`、`result`；欄位刻意跟角色工坊的 `character-studio.note` 同名同意思，正本是套件資料夾的 `schemas/note.toml`）：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `persona` | string | 是 | 人設代號 |
| `outfit` | string | 是 | 衣服 id |
| `record` | string | 否 | `closet.outfit` 編號（`<人設>-<衣服 id>`） |
| `piece` | string | 是 | 哪一張：`half-<n>`、`full`、`video-<n>-<k>`；隱藏版的前面加 `h:`（例 `h:half-2`；隱藏版只有一組，DEV-160 S2），舊的 `<隱藏版編號>:`（例 `h1:half-2`，2026-11-13）照讀、當成 `h:`；DEV-160 起有 `photo` 就看 `photo`；整套的意見（`scope = outfit`）固定寫 `outfit` |
| `type` | enum | 是 | `half`｜`full`｜`video`；整套的意見固定寫 `half`（只是填必填，看 `scope`） |
| `slot` | int | 否 | 第幾張照片（全身照沒有） |
| `clip` | int | 否 | 只給影片：這張的第幾支 |
| `level` | string | 否 | 那張圖的版本：`標準`（一般版）或 `隱藏版`；展示間一律寫，是分辨版本的主要依據（角色工坊寫的意見 piece 不加前綴，只靠這欄） |
| `intent` | enum | 否 | `redo`（沒寫也當 redo）＝按「修改」要重做；`comment`＝按「留言」，照內容辦、不自動重做。有 `photo` 或 `scope = outfit` 的 `comment` 是角色頁「修改」分頁的評論：真的換了新圖要把那張 `closet.photo` 的 `adopted` 改回 false、`state` 填 `redone`（CLO-35） |
| `comment` | string | 是 | 主人的意見（白話：哪裡不喜歡、想怎麼改） |
| `state` | enum | 是 | `open` 等秘書重做；`redone` 已重做（新圖已換在同一張）；`dismissed` 不用重做了 |
| `result` | string | 否 | 重做結果的一句話（秘書回填） |
| `photo` | string | 否 | 哪一張：`closet.photo` 的編號（DEV-160）；整套的意見不用寫 |
| `scope` | enum | 否 | `photo`（沒寫也當 photo，舊的意見都是；只加不必填的欄位，不附資料轉換，PKG-62）｜`outfit`（整套；`piece = outfit`、`type = half` 是固定寫法，不寫 `photo`、`slot`、`clip`、`level`）。角色頁「修改」分頁的「評論」整套與單張都寫 `intent = comment`（CLO-35） |

`photo`（`given`，DEV-160 S1，modules/closet.md CLO-31；一筆＝一個檔：每張半身、全身照、每支影片；正本是套件資料夾的 `schemas/photo.toml`）。照片屬於誰、狀態都記在這裡：**S2 起衣櫃、角色包、匯出、改代號都只讀這份紀錄，檔名與資料夾不再帶意思**；存放位置只表示歸誰、能不能改（官方在家外唯讀、主人的在家裡）。**S3 起**新上傳的檔存成 `personas/mine/photos/<照片編號>.<ext>`，名字沒有意思（新角色的第一張是角色本身的頭像 `personas/mine/avatars/<代號>.<ext>`，角色卡的慣例路徑）；舊檔不改名，紀錄指過去。

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `persona` | string | 是 | 人設代號 |
| `outfit` | string | 是 | `closet.outfit` 編號 `<人設>-<衣服 id>` |
| `type` | enum | 是 | `half` 半身｜`full` 全身｜`video` 影片 |
| `order` | int | 否 | 同一套同類型的排序（1 起算；半身第 1 張＝1，取代舊檔名 `~N`；全身照 1；影片跟它配的照片同一個數字） |
| `of` | string | 否 | 只給影片：配哪一張照片的 `closet.photo` 編號 |
| `clip` | int | 否 | 只給影片：那張照片的第幾支（1 起算，就是公開的編號；取代舊檔名 `+k`） |
| `hidden` | bool | 否 | 隱藏版的圖（隱藏版只有一組，不記是哪一組）；轉換器一律明寫 |
| `source` | enum | 是 | `official`（檔在官方角色資料夾）｜`mine`（在 `<家>/personas/mine/`）；角色從官方收成自己的時改成 `mine` |
| `file` | string | 是 | 相對於 `source` 那個資料夾的路徑（用 `/`），例 `avatars/athena@sea~2.png`、`adult/full/athena@sea.png` |
| `adopted` | bool | 否 | 設計中那張已採用；上架、下架的衣服每張都是 true；轉換器一律明寫 |

編號規則：`<衣服編號>--<類型><第幾張>[-c<第幾支>][-h]`——半身 `athena-sea--half2`、全身 `athena-sea--full1`、影片 `athena-sea--video2-c1`（配第 2 張半身的第 1 支）、隱藏版接 `-h`（`athena-sea--half2-h`、`athena-sea--video2-c1-h`）。同一個檔重算一定同號（冪等）；編號只是名字，讀的人看欄位，不要拆編號。
宣告：`export = "by_persona"`；`pack_only = { source = ["official"], hidden = ["false"], adopted = ["true"] }`（官方角色包只帶官方位置、一般版、已採用的；設計中衣服裡已採用的那幾張 S2 由建包端照衣服連帶擋）；`adopted` 跟著匯出（照片本身的狀態，不像 `outfit.adopted` 是這個家的審核進度）；這套編號只給轉換器登記舊圖，之後改類型或一般／隱藏不改編號，S3 起新上傳另用沒有意思的編號；改代號 `persona_id = "prefix"`、`persona_prefixed = ["outfit", "of"]`、`persona_source = "source"`。

舊資料轉換（衣櫃的照片轉換器 `internal/photomigrate`，全 repo 唯一保留舊檔名解析的地方）：掃「圖片的位置與命名」的檔，一個檔一筆；半身 `order`＝掃到的位置，影片 `of`＝那張半身、`clip`＝掃到的順序（第 1 支缺時往前補），全身 `order = 1`，隱藏版資料夾的圖 `hidden = true`（隱藏版只留一組：兩組有同一張時，舊 `adopted` 提到的那組先、再來開關開著的、其餘照順序，排前面的登記、其他列出來）；上架、下架的整套每張 `adopted = true`，設計中的照 `outfit.adopted`（`half-<n>`、`full`、`video-<n>-<k>`、`<隱藏版編號>:` 前綴；沒前綴而一般版沒那張的算隱藏版）。已經有的不重寫；不認得的檔不碰、只列出。預演（只讀，紀錄讀家裡的 vault 與官方層 `<官方角色資料夾>/records/`）：`cd packages/official/closet && go run ./cmd/closet-photos --dry-run --home <家的複本>`。真的寫入（`photomigrate.Apply`）還沒接到任何流程：S2 要定程式型轉換怎麼接升級的試跑與退回（UPD-9；PKG-61 的宣告只能改欄位，做不到掃檔產生紀錄），以及官方照片的紀錄寫在哪一層（官方角色放家外的家，寫進家裡會蓋住官方層，VLT-19）。

例：`evergenie data add closet.variant --id night --json '{"name":"隱藏版","dir":"avatars/hidden","windows":["22:00-02:00"],"button":true}'`，再把 `athena@seaside.png` 這類圖放進 `<家>/personas/avatars/hidden/`。

## 圖片的位置與命名
官方角色 `<家>/personas/avatars/<人設>[@<衣服>][~<N>].<副檔名>`；使用者自己的 `<家>/personas/mine/avatars/` 同樣的命名。
第 2 版的家（`[layout] version = 2`，DEV-145）：官方角色卡與 `avatars/` 在家外面的狀態資料夾（`sdk.OfficialPersonasDir`；`official_personas = "home"`〔relayout 預設〕時仍在家裡），使用者自己的仍在 `<家>/personas/mine/`。

- 第一套 `base` 的代號就是人設本身（`athena.png`），其他是 `<人設>@<衣服>`（`athena@seaside.png`）。
- 輪播照片從 `~2` 起連續編號（第 1 張不帶 `~1`），一套最多 12 張；缺一張就停。
- 靜態圖副檔名 `png`／`jpg`／`jpeg`／`webp`（同一張只認第一個存在的，順序同這行）；動態是同代號的 `.mp4`（只有擁有的與草稿的才公開）。**同一張可以有多支動態**（DEV-125，看板每次隨機挑一支播）：第 1 支是 `<名>.mp4`，第 k 支（k ≥ 2）是 `<名>+k.mp4`（例 `athena@sea+2.mp4`、`athena@sea~2+3.mp4`），從 `+2` 起連續、缺一支就停，最多 6 支；`+` 不會出現在人設與衣服 id，所以不會和別的檔名撞。只有第 1 支缺、有 `+2` 時，公開時往前補（公開的編號永遠從 1 起連續）。
- **全身照**：`<圖資料夾>/full/<代號>.<png|jpg|jpeg|webp>`（官方 `personas/avatars/full/`、我的 `personas/mine/avatars/full/`；代號規則同上，同一張只認第一個存在的，順序同上）。舊家 `personas/avatars/full/` 的命名就是這個，搬家複製即可。
- 只認一般檔案（不跟符號連結）、大小 ≤ 30 MB。**其他檔（`*.locked.png`、`*.live.json`、`*.voice.mp4`、`full/` 以外的子資料夾）一律不讀**；隱藏版宣告的資料夾例外（`variant.dir`）。

## 公開資料夾
`<家>/runtime/packages/closet/public/`（第 2 版的家在狀態資料夾的 `runtime/`，`sdk.RuntimeDir`；只有 closet 的行程寫；可重建的衍生檔，不是資料，PKC-10；核心唯讀送出，D-29）：

| 路徑 | 內容 |
|---|---|
| `o/<人設>/<衣服>/<n>.<ext>`、`<n>.mp4`、`<n>+k.mp4` | 真圖與同張的動態（第 2 支起帶 `+k`，連續編號），**只放擁有的與草稿的** |
| `f/<人設>/<衣服>/1.<ext>` | 全身照，**只放擁有的與草稿的**；沒擁有的不放，也不給剪影 |
| `s/<人設>/<衣服>/1.png` | 剪影（128×128 灰階，只由 16×16 的格子平均色決定），只給沒擁有的、非草稿、非下架的衣服的第 1 張 |
| `worn/<人設>.<ext>`、`worn/<人設>.mp4`、`worn/<人設>+k.mp4` | 現穿的那張與它的動態（沒有 mp4 就沒有這個檔；有幾支就有幾個，換衣服時一起換、舊的 `+k` 一起刪） |
| `.state.json` | 每個檔案的來源簽章（來源沒變就不重寫） |

隱藏版開著時（CLO-17），擁有的與草稿的衣服**多放**隱藏版的圖，一般版的照樣在（兩版都列，2026-11-13 作者）：`o/<人設>/<衣服>/<編號>-<n>.<ext>`（同張動態 `<編號>-<n>.mp4`、`<編號>-<n>+k.mp4`）、`f/<人設>/<衣服>/<編號>-1.<ext>`；`person` 的隱藏版網址帶 `?v=<編號>`；`worn/` 在兩版的照片裡輪。沒擁有的照舊。

不變條件：沒擁有的衣服，真圖、全身照與影片的檔案在公開資料夾裡不存在。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `outfit.persona`、`person.persona` | 人設 | 純字串；人設檔 `personas/<人設>.md`（或 `personas/mine/`）提供顯示名 |
| 出 | `person.agent` | `agent` | 純字串；讀名冊裡啟用中、`persona` 相符的職位 |
| 出 | `state.earned` | `habits.wallet/main` | 讀 `earned`；沒有錢包 `state.wallet = false` |
| 入 | `habits` 的 `wallet.spent` | `unlock.cost` | 每筆花 `cost`，沒寫就是 1 點 |

## 保存規則
- 只封存，不刪除。`outfit` 被封存（`closet-mine` 的刪一套，或 DEV-160 S5b 以前展示間的「不要了」）後編號不能重用；`unlock` 永久保存、不收回。
- `person`、`state`、`capabilities` 是可重算的快取（`capabilities` 被封存就寫不進去，核心與頁面當這個家什麼都不支援，程式記一次 log）；角色在目錄裡沒有衣服了，`person` 不封存而是清空（`outfits` 為空），因為封存過的紀錄之後寫不進去。
- 沒有自動封存（套件資料沒有 `ArchiveAt`），已處理的請求由程式封存。
- 官方角色包（`evergenie personas pack build`）：`outfit` 宣告 `pack_only = { source = ["official"], state = ["active", "retired"] }`——只帶官方來源、已採用或下架的衣服；主人自己的（`source = mine`）與設計中的（`state = draft`）不帶，同步時包裡萬一帶了也不寫（CLO-26）。下面的匯出角色不看這條。
- 匯出角色（`evergenie personas export`，PRS-8、D-63）：`package.toml` 宣告 `outfit` 是 `export = "by_persona"`（她的衣服目錄跟著她走）、`variant` 是 `by_folder`＋`folder_field = "dir"`（資料夾裡有她的圖才跟著走，只帶她的圖）＋`export_omit = ["on"]`（開關是這個家當下的狀態不跟著走；匯入時本機已有這個隱藏版就保留本機的開關；時段 `windows` 照帶，PKG-58）。`unlock`、`person`、`state`、`rotation`、`request`、`capabilities`、`action` 不匯出：兌換紀錄只有本套件的程式能寫，對方的家照 `cost` 自己兌換。

## 從舊系統遷移
不處理（D-27）：搬家當天由 AI 手動轉一次。圖片整個複製 `personas/avatars/`；目錄由一次性腳本寫 `closet.outfit`（第一套 `cost: 0`、其他 `cost: 1`、`source: official`、`state: active`）；舊系統「已兌換」對應 `closet.unlock`（`by: migrate`、`cost` 照舊、`at` 填搬家時間）；舊的輪播設定轉 `closet.rotation`。

## 使用者自己建的東西放哪裡（DEV-112，作者 2026-11-03）
看板上「新增角色」「加一套衣服」（入口在展示間，由 `closet-mine` 處理）建的東西，跟官方的放在不同地方、用同一套命名：

| 東西 | 放在哪 |
|---|---|
| 角色卡 | `<家>/personas/mine/<代號>.md` |
| 半身圖、輪播照、全身照、動畫（一般版與自己的隱藏版） | DEV-160 S3 起：`<家>/personas/mine/photos/<照片編號>.<ext>`（新角色的第一張是角色本身的頭像 `avatars/<代號>.<ext>`；照片編號 `<衣服編號>--u<請求編號>`，名字沒有意思，是誰的、哪一種、一般或隱藏版只看 `closet.photo`）；以前放的 `avatars/<代號>[@<衣服>][~N].<ext>`、`avatars/full/…`、`adult/…` 不搬，紀錄照舊指著 |
| 衣服目錄 | `closet.outfit`，`source = mine` |
| 上傳暫存 | 家的附件資料夾：第 1 版 `<家>/runtime/share/`、第 2 版 `<家>/media/share/`（`sdk.SharesDir`；核心的上傳路由寫，處理完複製出去） |

看板上看到哪裡，只看「擁有」（CLO-4）：免費的（`cost = 0`，頁面預設）一加就擁有，出現在**衣櫃**；要點數的出現在**展示間**（剪影），兌換後才進衣櫃；設計中（`state = draft`）不在衣櫃與展示間，只在開發家角色頁的「修改」分頁（CLO-35、CLO-37）。路徑與資料沿用 P6-4c 的，不搬家。

## closet-mine 的資料
2026-11-02 從 `schemas/closet-mine.md` 併進來；規則在 `closet-mine` 套件的說明卡。

套件 `closet-mine`（`kind = "program"`，2026-11-03 起預設啟用）宣告一種資料，vault 種類名稱是 `closet-mine.request`，一筆一檔，放 `vault/pkg/closet-mine/request/U-<n>.md`（YAML 標頭，D-27、D-28）。欄位規格的正本是套件資料夾的 `schemas/request.toml`。**舊家的資料與圖片不匯入**（D-27）。

### 路徑
- 請求：`vault/pkg/closet-mine/request/U-<n>.md`
- 檔案（D-30）：`<家>/personas/mine/<代號>.md`（角色卡）、`<家>/personas/mine/avatars/<代號>.<ext>`（角色本身的頭像＝新角色基本款的第一張）、`<家>/personas/mine/photos/<照片編號>.<ext>`（DEV-160 S3 起其他新上傳的圖與動畫，一般版與隱藏版都在這；換掉、刪掉的改名成 `<原名>.bak-<YYYYMMDD>-replace-u<n>`／`-delete-u<n>`）；以前的 `avatars/…`、`avatars/full/…`、`adult/…` 不搬。升成官方時圖搬到官方位置（舊的 `avatars/…` 同一個相對路徑、`photos/<照片編號>.<ext>` 到 `<家>/personas/avatars/photos/`）、舊的改名 `<原名>.bak-<日期>-promote`。上傳暫存 `<家>/runtime/share/<檔名>`（核心的上傳路由寫，這個套件只讀）。

### 欄位
`request`（`seq`，`U-`；**誰寫：主人與秘書都可以**，不花點數，`promote` 只認主人本人的；程式只回填 `state`、`reason`、`result`、`detail`）：

| 欄位 | 型別 | 必填 | 說明 |
|---|---|---|---|
| `type` | enum | 是 | `character`（新角色）、`outfit`（替任何一位角色加衣服，官方角色也可以）、`photo`（替我的衣服加一張輪播照；寫 `photo_id` 是換那一張）、`full`（放或換我的衣服的全身照）、`video`（放或換我的衣服某張照片的動畫）、`promote`（只在開發家：把採用了的我的衣服升成官方）；DEV-160 S3：`remove_photo`／`restore_photo`（刪／拿回一張我上傳的照片，照 `photo_id`）、`remove_outfit`／`restore_outfit`（刪／拿回一整套我的衣服，連照片）、`set_cost`（改要幾點） |
| `upload` | string | `character`、`outfit`、`photo`、`full`、`video` | `/share/<名>` 或 `<名>`；只能是單一檔名 |
| `persona` | string | 依種類 | `character`：想要的代號（選填，`my_<名字>`，以前的 `my<名字>` 也收；沒給＝`my_<請求編號>`）；其他：哪位角色（必填） |
| `outfit` | string | `photo`／`full`／`video`／`promote`／`remove_outfit`／`restore_outfit`／`set_cost` | 衣服 id（只能是 `source = mine` 的衣服；`adult` 的照片官方衣服也可以）；`promote` 是那套的編號 `<人設>-<衣服 id>` |
| `name` | string | `character`、`outfit` | 角色名／衣服名，最多 40 字 |
| `desc` | string | 否 | `character`＝一句話介紹（角色卡內文）；`outfit`＝衣服說明 |
| `free` | bool | 否 | 只給 `outfit`：true＝`cost = 0`（直接擁有）；預設 false＝`cost = 1`，跟官方衣服一樣要 1 點兌換（兌換前只看剪影） |
| `cost` | int | `set_cost` | `outfit`：兌換要幾點（0～99，0＝免費）；有寫就照它，沒寫照 `free`。`set_cost`：改成幾點（基本款不能改） |
| `draft` | bool | 否 | 只給 `outfit`：true＝`state = draft`（放進「設計中」，照片 `adopted = false`）；預設直接生效（`state = active`、照片 `adopted = true`），不看是不是開發家（closet-mine CMI-22） |
| `adult` | bool | 否 | 只給 `photo`、`full`、`video`：true＝自己的隱藏版的圖（檔一樣在 `personas/mine/photos/`，照片紀錄 `hidden = true`）；不看衣服名或角色代號。沒寫 `adult` 而這套現在的照片全是隱藏版的也算隱藏版，寫 `false` 才是一般版（closet-mine CMI-13）；衣櫃的「自己的隱藏版」開關沒打開就拒絕 |
| `photo_id` | string | `remove_photo`／`restore_photo` | 照片紀錄（`closet.photo`）的編號：`photo`／`full` 是換那一張；`video` 寫半身照的編號＝配那一張、寫動畫的編號＝換那一支（DEV-160 S3） |
| `new_id` | string | 否 | 只給 `promote`：官方的新衣服 id（小寫英數與 `-`，不能 `my-` 開頭、不能 `base`）；舊的是 `my-` 開頭時必填 |
| `dry_run` | bool | 否 | 只給 `promote`：true＝只預演，`detail` 列出會做什麼，什麼都不動 |
| `photo` | int | 否 | 只給 `video`：配第幾張照片（照衣櫃排的順序數），預設 1；寫了 `photo_id` 就不看 |
| `clip` | int | 否 | 只給 `video`：這張照片的第幾支動畫（DEV-125），預設 1、最多 6；放新的一支要連續（先有第 k−1 支才能放第 k 支），給已有的號碼就是換掉它 |
| `state` | enum | 是 | `pending`、`done`、`rejected`；寫入時一律 `pending` |
| `reason` | string | 否 | 被拒絕的白話原因（只有程式回填） |
| `result` | string | 否 | 成功時影響的 `closet.outfit` 編號（只有程式回填；`promote` 是升官方後的編號） |
| `detail` | string | 否 | `promote` 的說明（只有程式回填）：搬了哪些檔、改了哪些紀錄、怎麼退回 |
| `photos` | list | 否 | `remove_outfit` 這次一起封存的照片紀錄編號（只有程式回填；`restore_outfit` 照它拿回） |

### 檔案格式
- 角色卡 `personas/mine/<代號>.md`：`---` YAML 檔頭（`schema: 1`、`name: "<名>"`，雙引號字串，跳脫 `"`、`\`、換行；格式同 `persona.md`）＋內文（一句話介紹）。讀的時候 `---` YAML 與 `+++` TOML 兩種都認。
- 圖：副檔名由檔案開頭決定（`png`／`jpg`／`webp`；動畫 `mp4`）；一般檔案、≤ 20 MB。
- 照片編號（DEV-160 S3）：新上傳的每一張 `<衣服編號>--u<請求編號>`（例 `athena-my-12--u31`），檔 `personas/mine/photos/<照片編號>.<ext>`（新角色的第一張是頭像 `avatars/<代號>.<ext>`）；換圖不換編號。
- 衣服編號：`character` → `<代號>-base`（`outfit = base`、`name = 基本款`、`cost = 0`、`state = active`、`source = mine`、`order = 0`）；`outfit` → `<人設>-my-<請求編號>`（`outfit = my-<請求編號>`）。這兩種編號都可以由請求編號重算，重播時結果相同。

### 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `request.persona`／`outfit` | `closet.outfit`、`closet.person`、人設檔 | 純字串；程式檢查存在 |
| 出 | `request.result` | `closet.outfit` 編號 | 成功時回填 |
| 出 | `request.photo_id`／`photos` | `closet.photo` 編號 | 純字串；程式檢查存在、屬於這套衣服 |
| 出 | `request.upload` | `runtime/share/<名>` | 純字串 |

### 保存規則
- 請求已處理超過 7 天由程式封存；`personas/mine/` 的檔案永遠不被更新或清道夫碰（使用者的資料，D-3）。
- 刪我的衣服與圖＝封存紀錄＋檔改名成備份（`remove_outfit`／`remove_photo`，可以拿回；closet-mine 卡 CMI-19、CMI-20），不真的刪檔。

### 從舊系統遷移
不處理（D-27）：舊系統裡主人自己的衣服資料夾，搬家當天由 AI 手動轉（複製進 `personas/mine/`，寫 `closet.outfit`＋角色卡）。
