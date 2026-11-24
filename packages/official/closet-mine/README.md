# 上傳衣服與角色（官方套件 `closet-mine`）

使用者自己的角色、衣服、照片（REQ-126，D-30）：替自己創的角色，或官方角色，加衣服、輪播照片、全身照、動畫。
沒有自己的入口頁（DEV-143，2026-11-08）：上傳衣服在看板一位角色一頁 `/personas/<代號>` 的「衣服」分頁
（`?tab=closet-mine.outfits`，每套衣服缺半身、全身、輪播照或動畫一眼看得到；點一套看每張照片，換一張、加動畫、刪掉、改要幾點、刪掉這套；
總覽最下面「刪掉的」可以拿回來）；新增角色在「角色」清單的「新增角色」→「上傳半身照」。
「衣服」分頁在一般家與開發家長得一樣（DEV-160 審查修正 S1，作者：開發／設計用的只有一頁）：開發用的（看設計中、每張採用、上架／放回開發中、升成官方）
都在開發家角色頁的「修改」分頁（closet 套件，只在衣櫃寫的這個家支援 `edit` 時出現）。
一般家沒有上架／下架（作者 2026-11-15）：秘書放進設計中的我的衣服，衣櫃每一輪自動上架、照片標採用（closet 套件 README；不想要就刪掉那套）；
以前下架的我的衣服在這頁列在「刪掉的」，「拿回來」就改回上架。
展示間的「新增角色」「加一套衣服」也開到這兩個地方；指派（擔任哪個職位）在角色頁的「介紹」。舊網址 `/p/closet-mine/fitting-room` 已拿掉（SEC-6）。
圖由使用者在看板上傳（或聊天附件），程式驗證後複製進 `<家>/personas/mine/`，並登記到衣櫃的目錄 `closet.outfit`。
**免費的（`cost = 0`，頁面「要用點數換」關著＝預設）一加就擁有，出現在衣櫃；要點數的先出現在展示間，兌換後才進衣櫃。**

## 給秘書

- 預設就開著（作者 2026-11-03：上傳是展示間的基本用途，不再是選用的進階套件）。使用者自己關掉過（`evergenie packages list`
  看到 `enabled` 是 false）就照他的意思，要用時問他，他說要才 `evergenie packages enable closet-mine`（要先有 `closet`）。
- 替使用者加圖：圖要先在家的附件資料夾（第 1 版的家 `<家>/runtime/share/`，第 2 版 `<家>/media/share/`；聊天附件就是在那裡，看板上傳也是，`evergenie share <檔案>` 會放進去），再寫一筆請求：

  ```
  evergenie data add closet-mine.request --json '{"type":"outfit","upload":"/share/beach.png","persona":"athena","name":"海邊","desc":"夏天","cost":2}'
  ```

  `type`：`character`（新角色，要 `name`；`persona` 選填＝想要的代號，`my_<名字>`，小寫英數）、`outfit`（替任何一位角色加衣服，
  官方角色也可以；`cost` 是兌換要幾點（0～99，0＝免費、直接擁有）；沒寫 `cost` 時照舊看 `free`，預設 false＝要 1 點兌換；沒兌換前只看得到剪影，兌換後才看得到真圖、`draft` true＝先放進「設計中」，主人在開發家角色頁的「修改」分頁每張採用、整套上架，不滿意的寫評論（見 closet 套件的 README）；頁面從不送 `draft`，
不支援 `edit` 的家收到了，衣櫃下一輪自動上架）、`photo`（替我的衣服加一張輪播照，
  要 `persona` 與 `outfit`）、`full`（放或換全身照）、`video`（放或換某張照片的動畫，只收 MP4，`photo` 是第幾張；`clip` 是這張的第幾支動畫，預設 1、最多 6，要連續放：先有第 1 支才能放第 2 支）。
  沒寫 `draft` 的 `outfit` **直接生效**（上架、照片已採用），正式家、開發家都一樣。
  `state` 寫入時一律 `pending`；程式處理後回填 `done`（`result` 是 `closet.outfit` 編號）或 `rejected`（`reason` 是白話原因）。
- **換、刪、拿回、改點數**（DEV-160 S3；每張照片的編號在 `closet.photo`，`evergenie data list closet.photo` 看 `outfit` 欄位找）：
  - 換一張：`photo`／`full`／`video` 加 `"photo_id": "<照片編號>"`（同一筆紀錄換新檔，舊檔留成備份）。種類不能換（半身照換半身照、全身照換全身照、動畫換動畫）。
    **開發家（衣櫃寫的這個家支援 `edit`）換了就自動回到還沒採用**（作者 2026-11-15）：主人在「修改」分頁重新看過、按「採用」；秘書不用、也不要自己改 `adopted`。
    一般家（確定不支援 `edit`）照衣服的狀態直接生效；衣櫃還沒寫這個家支援什麼時當成要重新採用，讀不到就等下一輪（請求維持 `pending`）。
  - 換官方的照片（只在開發家，CMI-25）：一樣 `photo`／`full`／`video` 加 `photo_id`（官方照片的編號，`source = official`）。新檔寫在官方位置
    `personas/avatars/photos/<照片編號>.<ext>`（角色本身的頭像還是 `avatars/<角色>.<ext>`），舊檔原地改名成備份，紀錄一樣回到還沒採用。
    只換不加（不能替官方的照片多配一支動畫）；只換一般版、檔在官方 `avatars/` 底下的（官方衣服的隱藏版照片、舊資料檔在別處的會被拒）；
    要核心說這是開發家、衣櫃的能力也有 `edit`；一般家、官方角色跟著角色包放在家外面（第 2 版）時會被拒，`reason` 寫原因。
  - 刪一張：`{"type":"remove_photo","photo_id":"<照片編號>","state":"pending"}`；拿回：`"type":"restore_photo"`（同一個 `photo_id`）。
    半身照連它的動畫一起；最後一張半身照不能刪。檔改名成備份（30 天），拿回時改回來。
  - 刪一整套：`{"type":"remove_outfit","persona":"athena","outfit":"my-12","state":"pending"}`（連照片；基本款不能刪）；拿回：`"type":"restore_outfit"`（整套刪掉的照片要這樣一起拿回；下架的我的衣服送這個就改回上架）。
    角色改代號後，之前刪掉的衣服與照片就拿不回來了（改代號只改還在的紀錄）。
  - 改要幾點：`{"type":"set_cost","persona":"athena","outfit":"my-12","cost":0,"state":"pending"}`（0＝免費、直接擁有；1 以上沒兌換前只看剪影；基本款一定免費）。
- **自己的隱藏版**：`photo`／`full`／`video` 加 `"adult": true`，圖一樣放 `personas/mine/photos/`、照片紀錄記 `hidden = true`；不看衣服名或角色代號
  （叫「成人禮」也是一般版）。只有這套現在的照片全是隱藏版時，沒寫 `adult` 的也算隱藏版（要放一般版寫 `"adult": false`）。第一次會建好
  `closet.variant/adult`（隱藏版，預設關）。只能替**已經有的衣服**放（`character`、`outfit` 不收）；官方衣服也可以放自己的隱藏版的圖。
  **主人沒在看板「設定 → 套件」把衣櫃的 `adult` 設成 `on` 時，請求會被拒**（「自己的隱藏版沒有打開…」）；這個開關只有主人本人能改，秘書改會被拒。
  `closet.variant/adult` 被封存或 `dir` 被改掉時也會被拒，`reason` 寫怎麼恢復。
- **不要**直接寫 `personas/mine/`，也不要自己寫 `source = mine` 的 `closet.outfit`：一律走請求，程式才會驗證檔案類型與大小。
- 官方的衣服改不了（請求會被拒）；官方的照片只有開發家能換（上面）。刪是封存＋改名成備份（上面的 `remove_photo`／`remove_outfit`），真的刪檔請找管理者。
- 這個套件沒有秘書樣板。生圖秘書（`art` 職位）只有開發家有：樣板在開發工坊 `admin` 套件（選用的職位，DEV-160 S5a），一般家沒有；她出好的圖照上面的請求放進「設計中」，主人在角色頁的「修改」分頁採用（closet 套件 README）；重做一張就用換圖請求（`photo_id`），系統自動回到還沒採用。角色庫的 new／check／test／live 在後面的 P6-5。

## 檔案放哪裡

| 內容 | 位置 |
|---|---|
| 新角色卡 | `personas/mine/<代號>.md`（`---` YAML 檔頭：`schema: 1`、`name`；內文＝一句話介紹） |
| 角色本身的頭像（新角色基本款的第一張） | `personas/mine/avatars/<代號>.<png｜jpg｜webp>`（角色卡的慣例路徑：把角色複製進套件時只認這裡；換圖還是寫這裡） |
| 其他照片、全身照、動畫（一般版與自己的隱藏版） | `personas/mine/photos/<照片編號>.<png｜jpg｜webp｜mp4>`（DEV-160 S3 起；照片編號 `<衣服編號>--u<請求編號>`，名字沒有意思：哪一種、第幾張、是不是隱藏版只看 `closet.photo`；隱藏版不匯出、不進角色包） |
| 開發家換官方的照片 | 官方位置 `personas/avatars/photos/<照片編號>.<ext>`（角色本身的頭像 `personas/avatars/<角色>.<ext>`；只在官方角色放在家裡時） |
| 換掉、刪掉的舊檔 | 原地改名 `<原名>.bak-<YYYYMMDD>-replace-u<n>`／`-delete-u<n>`（30 天） |
| 以前放的 | `personas/mine/avatars/<代號>@<衣服>[~N].<ext>`、`avatars/<代號>~N.<ext>`、`avatars/full/…`、`adult/…`：不搬，照片紀錄照舊指著 |

衣服編號：第一套 `<代號>-base`；替角色加的衣服 id 是 `my-<請求編號>`。

## 升成官方（只在開發家）
採用了的我的衣服要變成官方的（放進這個家的官方位置：第 1 版的家 `personas/avatars/`，第 2 版在家外面狀態資料夾的 `personas/avatars/`；要帶到別的家，還要在 `home.toml [admin] public_personas` 列這位角色、再 `evergenie personas pack build`），寫一筆 `type = promote` 的請求：

```
evergenie data add closet-mine.request --json '{"type":"promote","outfit":"athena-my-12","new_id":"seaside","dry_run":true,"state":"pending"}'
```

主人也可以在看板按：開發家角色頁的「修改」分頁，那套衣服的塊裡有「升成官方（公開給所有人）」——先填新編號、預演看會怎麼做，
確認「可以公開給所有人、下一次建角色包時會帶出去（角色要在公開名單裡）、收不回」才正式送（送的就是上面這種請求；元件在 closet 套件的頁面）。
只在開發家、官方角色、上架了而且照片不全是隱藏版的自己的衣服出現（衣櫃算的 `promotable`）；正式家沒有「修改」分頁。

- **只認主人本人送的**（`by` 是 `owner(…)`，看板或主人自己的命令列）；秘書送的會被拒（「只有主人」看的是請求或設定的署名 `by = owner(…)`，是防秘書弄錯、不是安全機制：家裡的程式都互信，D-19。）。秘書被要求時，把上面這行給主人看、請他自己送。
- **只在開發家**（`home.toml` `[runtime] env = "dev"`）；正式家回 rejected「這要到開發家做」。
- `outfit` 是那套的編號 `<人設>-<衣服 id>`；`new_id` 是官方的新衣服 id（小寫英數與 `-`）。上傳時自動給的 `my-<n>` 不能當官方 id，
  這時一定要給 `new_id`。`dry_run: true` 只預演：`detail` 列出會搬哪些檔、會改哪些紀錄、怎麼退回，什麼都不動。
- 只收官方角色（官方角色卡 `<人設>.md`，第 1 版在 `personas/`，第 2 版在狀態資料夾的 `personas/`）、`source = mine`、`state = active` 的衣服；
  要公開的每一張（一般版）都要採用了，有沒採用的（例如秘書剛換過的）會被拒、列出哪幾張。搬哪些圖照照片紀錄：
  一般版的照片從 `personas/mine/` 複製到官方位置（舊的 `avatars/…` 到同一個相對路徑、`photos/<照片編號>.<ext>` 到官方的 `avatars/photos/`，檔名不改；不蓋內容不同的檔），
  舊的圖改名成 `<原名>.bak-<日期>-promote`，照片紀錄改成官方的、`file` 跟著換。
- 換了 id 會新增一筆 `source = official`、`from = <舊編號>`（兌換過的照樣擁有），舊的那筆封存，輪播設定跟著換。
- 退回：`detail` 會寫步驟（都是現成指令與改檔名）。舊編號在封存裡拿不回來，所以退回是把新的那筆改回 `source = mine`、
  圖搬回原位（`detail` 列出每一步）。紀錄改到一半失敗時請求維持 `pending`，下一輪接著做。
- 自己的隱藏版不能升：隱藏版的照片（照紀錄的 `hidden`）留在 `personas/mine/`，紀錄跟著換到新編號，`detail` 會列出來。
