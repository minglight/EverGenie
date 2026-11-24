# Schema：persona-pack（角色包）

Status: 實作 2026-10-29（P8 第二輪 S4，REQ-283、REQ-131、REQ-135）. 共通規則見 `README.md`。

官方公開角色（卡、頭像、公開版衣櫃、目錄卡）的下載包。內容定址：放在任何靜態網址（`https://` 或 `file://`；測試可用本機 `http://127.0.0.1`），核心在官方套件 `personas` 的 `pack.toml` 記「這一版用哪份包」，所以「升級後下載同版號的角色包」＝升級完照新版的指標同步。隱藏版與非公開的內容**不進包**。

## 路徑
- 包（發行者 `evergenie personas pack build --out <資料夾>` 產生，放到網站上）：
  - `<根>/m/<版號>.json`：manifest。
  - `<根>/f/<sha256><副檔名>`：檔案，檔名是內容的 sha256 加邏輯路徑的副檔名；內容一樣的檔案只存一份。
- 指標：`packages/official/personas/pack.toml`（跟著發版；環境變數 `EVERGENIE_PERSONAS_PACK_POINTER=<路徑>` 可以換成別的檔，測試與發版整套測試用）。
- 跟著程式的公開卡（PRS-25、26）：`packages/official/personas/cards/<代號>.md`，發行檔照官方套件帶著（下載版在 `versions/<版本>/packages/official/personas/cards/`，換版跟著換、唯讀）。新家裝好、還沒有角色包也有這幾位；官方角色資料夾有同代號的卡（角色包同步來的）就用那張；主人在 `personas/mine/` 有同代號的舊卡（不是 `my_` 開頭）就用主人的（`modules/agents.md` PRS-27）。卡的文字只有一份正本（來源家的官方卡），repo 裡的是 `personas pack cards --repo <repo>` 產生的副本（`build --set-pointer` 也一起寫），格式跟人設卡一樣。
- 官方角色放家外的家（新家都是，`schemas/home.md`「佈局」）：登記在 `<官方角色資料夾>/pack-registry.toml`、紀錄寫資料層的官方層 `<官方角色資料夾>/records/`（VLT-19），家裡一個檔都不寫；`settings/` 還留著舊登記的（官方角色剛搬出去）讀得到、不動。
- 其他的家（第 1 版、`official_personas = "home"`）：`settings/personas-pack.toml`（登記）、`runtime/personas-pack/{cache/<sha256>,staging/,pending.json,last.json}`、`runtime/backups/personas-pack/<時間>/`（`--force` 蓋掉你改過的檔之前的備份）、`runtime/personas/intros.json`（新角色待送的招呼，見「保存規則」）。

## 欄位
### manifest
JSON：`{"kind":"evergenie.personas","schema":1,"version":"2026.10.29","files":[{"path":"cards/athena.md","sha256":"…","size":123}],"records":[{"kind":"closet.outfit","id":"athena-seaside","fields":{…},"body":"…"}]}`。`records`（選填，DEV-116 S4、PRS-14）是跟著公開角色走的套件紀錄（只有 `by_persona` 的種類；種類宣告了 `pack_only` 時只有可以公開的那幾筆，PRS-29），格式跟角色匯出檔的 `records` 一樣（不含核心管的欄位與 `export_omit` 的欄位）；舊的包沒有這一欄。版號格式 `YYYY.MM.DD[.N]`（跟核心版號脫鉤）；`files` 依 `path` 排序。邏輯路徑只有四種，對到使用者家裡的位置：

| 邏輯路徑 | 家裡 | 收什麼 |
|---|---|---|
| `cards/<代號>.md` | `personas/<代號>.md` | 人設卡 |
| `avatars/<檔名>`、`avatars/full/<檔名>` | `personas/avatars/…` | 名字是 `<代號>`、`<代號>@<衣服>`、`<代號>~N` 開頭的 png／jpg／jpeg／webp／mp4（含全身照）；命名見 `closet.md` |
| `wardrobe/<代號>/<路徑>` | `personas/wardrobe/<代號>/…` | 公開版衣櫃資料夾 |
| `catalog/<檔名>` | `personas/catalog/…` | 目錄卡 |

路徑不能是絕對路徑、不能有 `..`、不能有 `.` 開頭的段、不能有空段；其他位置一律拒絕（整包不換）。

### `pack.toml`（指標）
| 欄位 | 型別 | 說明 |
|---|---|---|
| `version` | string | 包的版號 |
| `url` | string | manifest 的完整網址；必須是 `<包的位置>/m/<版號>.json`。**空＝這一版沒有角色包**（同步什麼都不做、結束碼 0，跟主人說「還沒有公開的角色包：角色的頭像與衣服圖還沒開放下載（角色卡已經跟著程式裝好…）」；角色包還沒有公開網址） |
| `sha256` | string | manifest 檔案內容的 sha256（下載後比對，對不上整包不換） |

### `settings/personas-pack.toml`（登記；官方角色放家外的家是 `<官方角色資料夾>/pack-registry.toml`，格式相同）
`schema = 1`、`version`（上次同步到的版號）、`synced_at`、`[files]`（邏輯路徑 → 裝進去時的 sha256；用來分辨「你改過」）、`[records]`（`<種類>/<編號>` → 寫進去時欄位＋內文的雜湊，不含核心管的欄位與 `export_omit` 的欄位）。manifest 不再列的檔與紀錄，登記也保留。

## 連結
| 方向 | 欄位 | 指向 | 規則 |
|---|---|---|---|
| 出 | `files[].path` 的 `cards/<代號>.md` | 人設卡 `personas/<代號>.md`（`persona.md`） | 同步只寫官方位置，不碰 `personas/mine/` |
| 出 | `avatars/`、`wardrobe/` | 頭像與衣櫃圖（`closet.md`） | 命名照 `closet.md`；兌換與公開資料夾由 `closet` 套件重建 |
| 入 | `home.toml [admin] public_personas` | 公開角色代號（`home.md`） | 建包只收這些人 |
| 出 | `records[].kind` | 套件資料種類（`package.md` 的 `contributes.data`，宣告 `export`） | 建包照宣告收；同步時這個家沒裝、停用或沒宣告的略過 |

## 不收什麼（REQ-135）
建包時一律不收：路徑有 `level/`、`old_school/`、`mine/`、`src/` 的；`*.locked.*`、`*.live.json`、`*.voice.*`；名稱含 `adult`／`nsfw`／`r18`／`成人` 的；文字檔自己標了隱藏版（開頭 4KB 有 `adult: true`／`nsfw = true`、`level: adult`）的；`personas/mine/`；製作用的 `personas/wardrobe/<代號>.toml`（含提示詞）；不在 `home.toml [admin] public_personas` 的角色。公開角色的卡裡寫了來源家主人的名字（`[user] name`）整包不建、說是哪一位（PRS-26；卡裡稱呼主人寫「主人」）。**跟著資料夾走的紀錄（`by_folder`，例如衣櫃隱藏版 `closet.variant` 的 `dir`，含封存的）指的資料夾整個不收**，不管名字，是符號連結的連它指到的真正位置一起（DEV-157，PRS-4）；建包要連得到這個家的服務才知道是哪些資料夾，連不到就不建，`--no-records` 才建只有圖與卡的包、`wardrobe/` 整個不收；宣告了跟著角色走的資料的套件停用或被拒（讀不到它的紀錄與資料夾）也不建，`--no-records` 時 `wardrobe/` 整個不收。**套件宣告不公開的紀錄不收**（`pack_only`，PRS-29）：例如衣櫃的衣服只帶官方來源、已採用或下架的，主人自己的（`source = mine`，還沒升成官方）與設計中的不進 `records`，它們照命名放在官方位置的圖（`<代號>@<衣服>…`、`wardrobe/<代號>/<衣服>/`）也不收，建包的輸出說幾筆沒帶、列編號與沒收的圖（`--json` 的 `records_held`、`held_files`）。**照紀錄帶圖**（DEV-160，PRS-30）：這個家的套件宣告了 `file_field`（衣櫃的照片 `closet.photo`）時，頭像資料夾只收跟著角色走的照片紀錄指著的檔（加角色本身的頭像 `avatars/<代號>.<副檔名>`），沒有紀錄指著的不收；隱藏版的照片（`export_only`，放在 `wardrobe/<代號>/` 裡也一樣）、還沒採用的、主人自己的（`pack_only`）、衣服沒進包的（`pack_parent`）照片連紀錄帶圖不收；紀錄指著的檔不套上面的檔名規則（`level/`、`mine/`、`src/`、`*.locked.*` 與名字是隱藏版的資料夾照擋），但要在 `avatars/` 或公開角色的 `wardrobe/<代號>/` 底下、是圖或影片、路上沒有符號連結。**紀錄只能讓檔更不帶**（DEV-160 審查）：全家（所有角色、含封存）只要有一筆不公開、沒寫角色、或別的角色的紀錄指著同一個檔（大小寫不同算同一個），或檔名是這個家另一位角色的，就不收；紀錄指著的檔沒收，那筆紀錄也不帶。`--no-records` 只是不帶紀錄，照樣照紀錄挑檔；這個家的套件宣告了照紀錄帶圖的種類時，連不到服務不建（PRS-4）。包格式不變：舊版的家不認得 `closet.photo` 只是略過那幾筆、列原因，圖與其他紀錄照常。

## 保存規則
- 同步只新增與替換，**不刪**任何檔；你改過的檔（本機 sha256 跟登記的不一樣、或原本就有但沒登記過且內容不同）不蓋，列在「你改過的，沒換」，`--force` 才蓋並先備份。
- 指標的版號比登記的舊＝不退版，什麼都不動；一樣＝已是最新。
- 失敗（沒網路、下載中斷、sha256 對不上、manifest 不合格、紀錄不合格）什麼都不換、登記不變，寫 `runtime/personas-pack/pending.json`（`{version, reason, at}`）；成功或不需要時清掉。
- 紀錄（`records`，PRS-14～16、29）：建包只收套件宣告 `export = "by_persona"` 的公開角色紀錄，宣告了 `pack_only` 的種類只收可以公開的；`by_folder` 的（使用者自己定義的圖組，例如衣櫃隱藏版）連同資料夾裡的圖一律不進角色包，只有主人點名的角色匯出才帶；連不到這個家的 EverGenie 就不建包（`--no-records` 才建只有圖與卡的包並說明；這個家有照紀錄帶圖的種類時 `--no-records` 也要連得到，PRS-4）。同步規則跟角色匯入一樣：全部先驗（屬於包裡的角色、這個家的規格）再寫；只新增；本機的雜湊等於登記的才換；你改過的不蓋（`--force` 才蓋，先備份到 `runtime/backups/personas-pack/<時間>/records/<種類>/<編號>.json`）；沒裝、停用、沒宣告 `export` 的種類略過並列出；`by_folder` 的紀錄（舊包萬一帶了）略過並列出；不在這個家宣告的 `pack_only` 裡的紀錄（舊包、別人做的包萬一帶了）略過並列出；已封存的編號不寫；`export_omit` 的欄位保留本機的值。照片紀錄（`file_field`）指著的檔是「你改過的，沒換」時，包裡新指著它的那筆也不寫、列在略過（DEV-160）。紀錄經 vault 的正常寫入，在檔案換進來之後寫；萬一某筆寫不進去，檔案與已寫的紀錄照樣登記、版號不登記、記下待補。指令 `personas pack sync` 先請服務做（`personas.pack_sync`，PRS-28）；包裡有紀錄而連不到核心（serve 沒在跑或還沒準備好）＝檔案照樣換、紀錄這次不寫、版號不登記、記下待補（結果 `records_later = true`），`new_cards` 照樣回報；serve 幾十秒內再試、接著寫（PRS-6）。官方層寫進來的紀錄會發變更事件（VLT-19），衣櫃馬上重算。登記跟角色匯入的 `settings/persona-import.toml` 分開：匯入來的紀錄之後出現在包裡，算你改過的、不蓋。
- 角色包的來源家（寫了 `[admin] public_personas`）不同步，角色庫以那裡的檔案為準。
- 同步結果的 `new_cards`（`--json`）：這次換進來、家裡原本沒有這個檔的 `cards/<代號>.md` 的代號；全新安裝的第一次同步（登記沒有版號也沒有檔案，而且 `personas/` 頂層還沒有任何官方卡）、只是更新內容的卡、非卡片檔都不算。失敗時空。
- 新角色待送的招呼 `runtime/personas/intros.json`（PRS-13）：`{"intros":[{"slug","at","queued_at"}]}`，`at` 是預定送出時間（家的時區 10:00–21:00 的隨機一分鐘，同步過 20:00 排隔天）。同步把 `new_cards` 寫進來（同代號不重排），serve 的 `personas.intros` 到時間送出、送成功才拿掉。跨行程用旁邊的 `intros.json.lock`（flock）保護，原子寫入；壞掉的檔當空的；排進來超過 7 天還送不出去的放棄。執行期資料，不備份。

## 角色匯出檔（`.hgpersona`，D-63、PRS-8～12）
主人點名的角色私下給別的家（`personas export --persona <代號>… --out <檔>` → 對方 `personas import <檔>`），家與家不連線。看板也做得到：「角色」頁（`/personas`）介紹分頁的「匯出」（勾角色、下載檔）與「匯入」（選檔、先預覽、確定才寫；有你改過的才給「蓋掉我改過的（先備份）」），走 `personas.export`／`import`，跟指令同一份程式、同一份規則（PRS-17～19、WEB-151～155）；看板的暫存放 `runtime/persona-export/<資料夾>/` 與 `runtime/persona-import/uploads/`，用完就刪；沒用完的（看了預覽就關掉、下載沒成功）放超過一小時由定時工作 `personas.transfer_sweep`（每 10 分鐘）清。跟角色包不同：不看 `public_personas`、不套上面「不收什麼」的過濾、不看套件的 `pack_only`（主人自己決定給誰），也收使用者自己的卡與圖（`personas/mine/`）。
- 檔案：zip，`files/<邏輯路徑>`（檔案在前）＋最後的 `manifest.json`。圖與影片不再壓縮。
- manifest：`{"kind":"evergenie.persona-export","schema":1,"exported_at":"<RFC3339>","personas":["mika"],"files":[{"path","sha256","size"}],"records":[{"kind":"closet.outfit","id":"mika-sea","fields":{…},"body":"…"}],"file_records":{"extra/mine/photos/mika-my-3--u1.png":"closet.photo/mika-my-3--u1"}}`。`file_records`（選填，DEV-160 S3，PRS-30）：檔名看不出是誰的檔 → 帶著它的那筆紀錄（`<種類>/<編號>`，要在 `records` 裡）；只列檔名認不出來的，而且只能是頭像、照片或衣服資料夾的檔（`avatars/…`、`wardrobe/<代號>/…`、`extra/mine/avatars/…`、`extra/mine/photos/<檔>`、`extra/mine/wardrobe/<代號>/…`；改過代號的角色的舊檔名也走這條）；舊的匯出檔沒有這一欄。`records[].fields` 不含核心管的欄位（`schema`、`id`、`created_at`、`updated_at`、`by`、`kind`、`archived_at`），也不含那種資料宣告 `export_omit` 的欄位（PKG-58）。
- 邏輯路徑：角色包的四種，加上 `extra/<personas 底下的路徑>`，只收兩種：使用者自己的卡 `extra/mine/<代號>.md`，和圖或影片（png／jpg／jpeg／webp／mp4，例 `extra/mine/avatars/…`、隱藏版資料夾 `extra/avatars/hidden/…`）；不能有 `_` 開頭的段（全家共用的設定不會被塞進來）。每個檔都要屬於 `personas` 列的某一位（卡、目錄卡、衣櫃資料夾看代號；圖看檔名 `<代號>[@衣服][~N].<副檔名>`；檔名看不出來的要在 `file_records` 指著檔案裡的一筆紀錄，匯入時照這個家的宣告驗那筆紀錄屬於列出的角色、指著這個檔）。
- 收什麼：卡（官方位置 `personas/<代號>.md`，沒有才找 `personas/mine/<代號>.md`）、`personas/avatars/`、`avatars/full/`、`mine/avatars/`、`mine/avatars/full/` 裡她的圖、`personas/wardrobe/<代號>/`、`personas/catalog/<代號>.md`；套件宣告 `export` 的紀錄（PKG-57）：`by_persona` 收角色欄位是她的，`by_folder` 收資料夾（`personas/<資料夾>/`、`<資料夾>/full/`）裡有她的圖的紀錄，連那些圖一起帶、別的角色的圖不帶。不收：`src/`、`*.locked.*`、`.` 開頭的、符號連結（檔案本身或 `personas/` 底下路上的資料夾）、製作用的 `personas/wardrobe/<代號>.toml`、沒宣告 `export` 的種類（例如兌換紀錄）。**照紀錄帶圖**（DEV-160，PRS-30）：這個家有宣告 `file_field` 的種類時，圖改成只帶跟著角色走的紀錄指著的檔（`source = mine` 的在 `extra/mine/<file>`）加角色本身的頭像；隱藏版的照片（`export_only`）連紀錄帶圖不帶，全家有一筆不跟著走、沒寫角色或別的角色的紀錄指著的檔、檔名是這個家別的角色的檔都不帶，名字是隱藏版的資料夾與 `by_folder` 紀錄指的資料夾照擋，`by_folder` 的紀錄（隱藏版設定）不匯出；紀錄指著的檔沒帶，那筆紀錄也不匯出；這位角色一筆照片紀錄都沒有時照檔名找圖；檔名看不出是誰的檔（衣櫃新上傳的 `photos/<照片編號>.<ext>`、升官方後的 `avatars/photos/…`）照紀錄認、記在 `file_records`（DEV-160 S3）；匯入時那筆紀錄沒收（套件沒裝、這個家說不跟著走、編號已封存）它的檔也不收、寫在略過的原因後面，紀錄對不上那個檔整個不收；檔名是收件家另一位角色的檔連同紀錄略過；沒帶檔的照片紀錄指著收件家已經有的檔或別筆紀錄的檔，那筆略過（不能把收件家的圖指走）；收件家隱藏版、別的角色的紀錄指著的位置，帶來的檔不寫（`--force` 也不蓋）、紀錄不收；帶來的檔因為本機內容不同沒換時，新指著它的紀錄也不收；靠 `file_records` 認的檔只能是圖或影片；舊版核心匯入帶了這種檔的匯出檔會整個不收（要新版）。匯入時 `export_only` 不允許的紀錄不收。套件的角色（`<套件>.<代號>`）跟著套件走，不能從這裡匯出。
- 登記：`settings/persona-import.toml`：`schema = 1`、`imported_at`、`[files]`（家底下的相對路徑 → 寫進去時的 sha256）、`[records]`（`<種類>/<編號>` → 欄位＋內文的雜湊）；用來分辨「上次匯入的、之後沒人改過」。
- 暫存與備份：`runtime/persona-import/staging/<時間>/`（驗過的檔先解到這裡，做完刪掉）、`runtime/backups/persona-import/<時間>/`（`--force` 蓋掉的檔照家裡的相對路徑放，紀錄放 `records/<種類>/<編號>.json`）。
- 匯入規則：①全部先驗：manifest 的種類與版本、代號、路徑、屬於誰、每位剛好一張卡、sha256 格式、大小、不重複（大小寫不同算同一個）、上限（解開 2 GB、2 萬個檔）；同代號的卡不能一邊在官方位置一邊在 `mine/`（這個家已經有另一個位置的同代號，整個不收，請先改代號）；寫入路上不能有符號連結；`by_folder` 紀錄的資料夾裡要有這個檔帶來的她的圖；zip 裡只能有 manifest 與它列的檔；每個檔的大小與 sha256；紀錄的種類要在這個家註冊中而且也宣告了 `export`（沒裝、停用、沒宣告的略過並列出），屬於誰、欄位照這個家的規格先驗。任何一項不對整個檔不收、什麼都沒寫。②檔案只新增與替換、不刪；本機有而內容不同、又不是上次匯入的原樣的不蓋（列在「你改過的，沒換」），`--force` 才蓋並先備份。紀錄同理；這個家已封存的編號不重用（`--force` 也不寫）。這個家宣告 `export_omit` 的欄位：檔案裡帶了也不收；換掉已有的那一筆時保留本機的值，只差這些欄位不算你改過的（登記的雜湊不含它們）。③檔案經角色包同一套換檔（全部換進去或全部倒回），紀錄經 vault 的正常寫入（檔頭、驗證、事件）；萬一某筆紀錄寫不進去，照實說已寫了哪些。④`--dry-run` 驗得一樣完整（含 sha256），算出會怎麼做，什麼都不寫。
- 卡照原樣：官方位置的卡照角色庫的規則一律算上線，`mine` 的卡保留檔頭的 `status`（D-63 否決④）。

## 從舊系統遷移
不處理。舊系統沒有角色包；舊家的 `personas/` 搬進新家後，第一次同步會把「內容跟包不同、沒登記過」的檔當成你改過的而不蓋（`--force` 才蓋，先備份）。
