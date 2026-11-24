# 寫一個套件

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

寫官方或自己的套件（純資料或附程式）之前讀。使用者自己的套件（`packages new`）的資料夾地圖、id 與保留字另見 [`user-features.md`](user-features.md)。manifest 欄位在 `schemas/package.md`，`pkg` 模組的行為在說明卡 packages（開發文件，不跟著發行）。

## MUST
- **PKC-1** — 一個套件是一個資料夾：`package.toml` 加宣告式內容（`agents/`、`personas/`、`jobs/`、`pages/`、`schemas/`）；需要跑程式才附 `cmd/`，並把 `kind` 設成 `program`。
- **PKC-2** — 附程式的套件用 Go SDK：`sdk.Serve(impl)`，像實作一個 Go interface；只透過 SDK 的 client 呼叫核心 API，不 import `internal/`。
- **PKC-3** — `package.toml` 要宣告用到的資料與能力（`capabilities.read`／`write`／`events`）、依賴的套件（`requires`）、協定版本（`api`）；用途是看清模組邊界與相依檢查，**不是安全機制**（D-19）。
- **PKC-4** — 套件對外提供的畫面與選單項目在 manifest 宣告（`contributes.page`、`contributes.menu`）；選單項目只是「可用項目」：放在哪裡由使用者自己的選單樹決定，`group_hint` 只是建議；沒被放進樹的項目會出現在「未分類」。選單項目可以宣告 `section`（跨套件也行）把幾頁合成看板上的一頁分頁；頁面可以宣告 `chat`（旁邊放聊天）與負責的秘書 `chat_agent`（在名冊上就固定跟她聊、頂端放她的橫幅）。同一個 section 的套件在看板上有「顯示設定」開關（停用／啟用那個套件）。核心只照宣告做，不認得特定套件（REQ-235、236）。
- **PKC-5** — 想讓使用者調整的地方，宣告成擴充點（`[[extension]]`：`setting`、`prompt_append`、`page_slot`）；不要要求使用者去改套件的檔案。
- **PKC-12** — 紀錄格式只加欄位（DEV-154，2026-11-12）：發出去的資料種類，欄位只加不必填的；真的要拿掉欄位、改名、換型別、拿掉選項、變必填，就在 `package.toml` 附 `[[migration]]` 讓核心升級時替使用者轉舊紀錄，並用真的舊資料驗過（轉換會先整批預演，一筆不合就整個不動）。官方套件沒附會被 `make lint` 的格式檢查擋下。
- **PKC-6** — 上層只能掛在下層身上、不能蓋掉：官方套件唯讀；使用者要改官方行為走擴充點；擴充點做不到的，另外做自己的套件（`packages/mine/`）。整包複製官方套件（fork）第一版不開放（DEV-154，2026-11-12：複製之後每次升級都要自己合併，是最容易衝突的路）：看板沒有按鈕、`/api/package/fork` 一律回 `FORBIDDEN` 與白話說明；以前複製過的照舊能跑、看得到官方更新差異。
- **PKC-7** — 套件的資料放 `vault/pkg/<id>/`，欄位規格放套件的 `schemas/<種類>.toml`（`[[contributes.data]]` 宣告，D-28；秘書用 `evergenie data`、頁面用 vault API 讀寫，不做套件自己的 RPC）；解除安裝套件時資料保留。
- **PKC-8** — 套件掛了只影響它自己：核心重啟它並發 `system.alert`；套件不要假設永遠在線，呼叫核心時處理 `UNAVAILABLE`。
- **PKC-9** — 發版前通過相容檢查（`admin` 的 fixture 家跑新核心，套件要通過新協定）。
- **PKC-10** — 唯一可以直接寫檔案的地方：附程式的套件只能寫自己的 `<家>/runtime/packages/<id>/public/`，內容是可重建的衍生檔（例如 closet 的圖片與剪影），不是資料（資料仍然只經 `VaultAPI`，PKC-N3）；核心用唯讀的 `GET /pkg-files/<id>/<路徑>` 送出（只限圖片與影片副檔名、不跟符號連結，D-29、REQ-175）。**要不要擋**由套件的程式決定「什麼可以放進公開資料夾」：沒放進去的檔案，網路上就拿不到（例：沒兌換的衣服不放真圖）。
- **PKC-11** — 套件內角色（SEC-302 S2）：套件要帶自己的角色就在 `package.toml` 宣告 `[[contributes.persona]]`（`{slug, path}`），卡檔與頭像都放套件資料夾內（`personas/<slug>.md`、`personas/avatars/<slug>.png`）；有效代號是 `<套件id>.<slug>`，別的套件、官方、我的都不會撞名；不要把卡寫進 `personas/` 或 `personas/mine/`（那是核心與使用者的，PKC-N3）。角色唯讀、跟著套件一起分享；套件停用時角色不能新指派、已指派的秘書不壞（`agent` AGT-24）。

## NEVER
- **PKC-N1** — 不 import `internal/` 的任何東西；也不在套件裡複製核心的邏輯。
- **PKC-N2** — 不自己帶 React 或 `@evergenie/ui`；外殼提供（前端規範 FEN-2；頁面怎麼寫見 `user-features.md`「頁面怎麼寫」）。
- **PKC-N3** — 不直接寫家裡的檔案（經 `VaultAPI`；例外有兩個：PKC-10 的自己的公開資料夾，以及官方套件 `closet-mine` 的 `<家>/personas/mine/`——只限新角色卡與圖，D-30，它也讀 `runtime/share/`；兩處都不碰官方內容）、不改帳號的 `settings.json`。
- **PKC-N4** — 不繞過 `notify` 直接推播（只回報分類，D-14）。
- **PKC-N5** — 每個引擎套件只含自己那一家的程式；不在 `engine-claude` 裡寫別家的程式（D-7）。

## 附程式的套件怎麼寫（P6-4a，範例：`packages/official/habits`）
需要在背景算東西、發通知的套件才附程式（`kind = "program"`）；只有資料與頁面的套件不要附（D-28）。
下面是官方套件（在 `evergenie` repo 裡）的寫法。主人自己的附程式套件（`packages/mine/<id>/`）差在第 1 步與第 8 步：`go.mod` 只寫 `module mine/<id>` 與 `go` 兩行，不寫 sdk 的 `require`、`replace`，也不改 `go.work`、`Makefile`——核心編譯時用正在跑的程式旁邊附的 sdk（說明卡 packages 的 PKG-66），改完 `evergenie packages reload --id <id>` 讓核心編；測試照第 8 步的「純函式測」做，`cmd/evergenie` 的整合測試只有 repo 有。細節照技能 `create-feature`。
1. **獨立 Go module**：套件資料夾放自己的 `go.mod`（`module github.com/minglight/evergenie/packages/official/<id>`），`require github.com/minglight/evergenie/sdk v0.0.0` 並 `replace … => ../../../sdk`；在 repo 根目錄的 `go.work` 加一行 `use`，`Makefile` 的 `build`、`vet`、`test` 各加一行 `cd packages/official/<id> && go …`（`build` 輸出 `bin/evergenie-<id>`，官方行程跟著發版附在核心旁邊）。只 import `sdk`，不 import `internal/`（PKC-N1）。
2. **manifest**：`kind = "program"`、`[program] entry = "cmd/evergenie-<id>"`、`restart = "always"`；資料種類、頁面、選單照純資料套件宣告。
3. **入口**：`main` 只做 `sdk.ServePackage(ctx, impl)`；`impl` 實作 `Run(ctx, host)`，報到成功才被呼叫，跑到 `ctx` 結束為止（SIGINT／SIGTERM 取消 `ctx`），乾淨結束回 `nil`。核心啟動行程時不帶參數，資訊都在環境變數。
4. **只寫 vault**：資料一律經 `sdk.NewVaultClient(host.Client)`，操作者填套件 id；通知經 `sdk.NewNotifyClient`（只回報分類）。**不做 RPC**：核心不把呼叫轉給套件行程，秘書與頁面只讀寫 vault 資料；套件算出來的東西寫成自己的資料種類（例如 `habits.status`），頁面只讀。算過的結果「內容沒變就不寫」，避免觸發自己訂閱的變更。
5. **訂閱與去抖**：`vault.watch` 要重算的種類，有變就等一小段時間（habits 是 2 秒）再重算，連續多次變更只算一次；另外開機先算一次、需要時加每日定時；訂閱斷線要重連並補算一次。
6. **讀不到就當沒有**：別的套件的種類可能沒裝，`list` 回空就當沒有；讀失敗記 log、不中斷。
7. **錯誤寫 stderr**：核心把行程的 stderr 收進 `runtime/packages/<id>/out.log`（接在後面寫，重開不清掉）；一筆資料有問題就記 log 略過那一筆，不讓整個行程退出（退出會被重啟並發 `system.alert`，PKC-8）。
8. **測試**：規則做成純函式測；行程那層用假的 vault 與 notify（`interface` 包住 SDK client）；再加一個 `cmd/evergenie` 的測試：把官方套件複製進 fixture 家、編出行程、用 `EVERGENIE_PACKAGE_BIN_DIR` 指給核心，真的 serve 一次（範例：`TestHabitsPackageRunsOnFixtureHome`；兩個套件一起跑的例子：`TestClosetPublicRouteHidesLocked`，`buildPackageBinaries` 可以把多個套件的行程編到同一個資料夾）。
9. **公開檔案**（P6-4b，範例：`packages/official/closet`）：要讓看板顯示圖片或影片，行程把檔案放進自己的 `runtime/packages/<id>/public/`（先寫暫存檔再 rename、不在清單裡的刪掉、`.` 開頭的檔不碰，來源檔一律先確認是一般檔案），網址是 `/pkg-files/<id>/<路徑>`（PKC-10）。「什麼可以放進去」就是存取控制：不該被看到的東西不要放，不要放了再靠頁面藏。測試要證明沒放的東西網路上拿不到（`TestReconcilePublishesOnlyOwned`、`TestPkgFilesServeOnlyPublicDir`）。
10. **選用套件**（範例：`packages/official/admin`，開發 EverGenie 的家才用）：不是每個人都要的套件，manifest 寫 `optional = true`：裝好預設不啟用，設定精靈的「進階套件（選用）」問使用者要不要開，秘書要先在對話裡問過使用者才能 `evergenie packages enable <id>`（REQ-176）。需要別的套件就寫 `[[requires]]`（沒開依賴時開它會被拒並說明原因）。只給開發 EverGenie 的家用的（例：`admin`）再加 `dev_only = true`，正式家的設定頁就不列（PKG-56）。一般人都會用到的套件不要做成選用（例：`closet-mine` 上傳角色與衣服，2026-11-03 改成預設啟用）。要寫 `personas/mine/` 這類使用者資料夾必須先有決策（D-30，範例 `closet-mine`），只寫驗證過的路徑、不跟符號連結、先寫暫存檔再 rename、不覆蓋既有內容。

## 範例
```toml
# packages/mine/my-habit/package.toml
schema = 1
id = "my-habit"
name = "我的習慣"
version = "0.1.0"
description = "自己用的習慣小工具"
kind = "data"
api = "evergenie.v1"
[capabilities]
read = ["vault:task"]
[[contributes.menu]]
id = "main"
title = "習慣"
icon = "check"
group_hint = "packages"
order = 10
page = "main"
```

## 設計決定
- **D-2** — 一個套件一個資料夾：manifest＋宣告式內容（角色、prompt、排程、頁面）；要跑程式就附一個獨立行程，用核心定義、帶版本號的協定溝通（Go SDK `sdk.Serve`）。分「純資料」與「附程式」兩種；官方提供的也是套件。安裝無感：`packages/` 的資料夾改了，核心自己偵測、重編、重啟。理由：改核心介面不會讓使用者的套件編不過；一個套件壞掉只壞它自己。否決：編進同一個執行檔、Go plugin（`.so`）。
- **D-3** — 四層：核心執行檔 → 官方套件（唯讀，跟著發版）→ 分享的套件（唯讀，跟著作者）→ `packages/mine/`（更新永遠不碰）；上層只能掛、不能蓋。改官方套件兩條路：①擴充點：官方 manifest 宣告哪些設定、prompt 附加段、頁面 slot 可以改，擴充點被移除時看板警告、不會壞；②fork：整包複製進 `mine/`、記下基準版本，官方更新時由 AI 整理差異逐項問（2026-11-12 DEV-154 起入口關閉，只剩擴充點與「自己做新套件」，見 PKC-6）。只有某個家用的放那個家的 `packages/mine/`；admin 只裝在發行者的家。否決：整檔覆寫（舊的 `overrides/`）。
- **D-19** — 套件不做安全把關：分享是少數互信的人之間的事；裝別人的套件要主人看過預覽、同意才裝，裝了就是信任它。manifest 宣告用到哪些資料和能力，是為了看清模組邊界、做相依檢查，不是安全機制。不做作業系統層級的隔離、套件市集、陌生人套件的權限審核。
- **D-28** — 套件的資料是宣告式的：manifest 用 `[[contributes.data]]` 宣告種類、`schemas/<種類>.toml` 寫欄位，核心註冊成 vault 種類 `<套件 id>.<種類>`，資料放 `vault/pkg/<套件 id>/<種類>/<id>.md`。秘書用 `evergenie data`、套件頁面用同一組 vault API 讀寫；**不做套件自己的 RPC**。加總、趨勢放在套件頁面或由秘書算。套件帶來的職責與排程樣板用 `evergenie packages sync <套件>` 先預演、主人同意才套用，不在掃描時自動改。否決：每個套件自帶 API 行程；掃描時自動覆蓋職責。
- **D-29** — 附程式的套件可以在 `<家>/runtime/packages/<id>/public/` 放公開檔案（圖片、影片），核心只用唯讀的 `GET /pkg-files/<id>/<路徑>` 送出（只限圖片與影片副檔名、一般檔案、不跟符號連結、啟用中的 `kind = "program"` 套件）；核心不寫、不看內容、不轉呼叫（是靜態檔，不是 RPC）。要不要擋（例如沒兌換的衣服）由套件的程式決定放什麼進去。頭像：附程式的套件可以在 manifest 宣告 `[contributes] avatars = "<子資料夾>"`，頭像路由 `/avatars/<職位>.png|.mp4` 先看那個套件公開資料夾的 `<子資料夾>/<人設>.*`，沒有才用人設預設圖；核心不認得任何一個套件（2026-10-30 改：原本寫死 `closet`）。官方套件裡宣告的只有衣櫃（`worn`）；同時有兩個宣告時取 id 排最前面的。否決：核心認識衣櫃規則；寫死套件 id；每次請求問套件行程；頁面自己判斷（可繞過）。

## 已知限制
- 套件市集、陌生人套件的權限審核都不做（D-19）。
- SDK 的介面在 P6 才定稿。

## Handoff
接著讀 `user-features.md`「頁面怎麼寫」（寫套件頁面）、`schemas/package.md`。
