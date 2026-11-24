---
name: create-feature
description: 主人想要新東西（他常說「新功能」：新頁面、新的資料紀錄、新角色、新指令），或想把做好的套件分享給別人時，用 evergenie packages new 在他自己的套件資料夾（packages/mine/）建一個新套件。正式家不改官方套件、核心、EverGenie repo 或官方角色卡（要改就說「這要到開發家改」）；開發家（共用規則會寫「這是開發家」）要改的本來就是官方套件時，直接在 EverGenie repo 開分支改官方，不另建套件。
---

# 幫主人建一個新套件

主人說「我想要一個 XX 功能／頁面／角色」「幫我做個記帳的」「這個我想分享給朋友」時用這份：主人口中的「新功能」，在這裡就是建一個**套件**（一包程式，可以裝、關）。新套件一律長在他自己的 `packages/mine/<id>/`，不是改別人的程式。先看共用規則寫的是「這是開發家」還是「這是正式家」（法律第 1.1 條）：**開發家**要改的若本來就是官方套件（例：叫你在官方的健康頁加喝水紀錄），就直接在 `evergenie` repo 開分支改官方套件，不另建 `packages/mine/` 的新套件；**正式家**只做客製，要改官方或核心就說「這要到開發家改」。以下步驟是「建新套件／客製」的做法，兩種家都適用。資料夾地圖、命名與保留字、頁面怎麼寫的完整版用 `evergenie docs show conventions/user-features.md` 讀；名詞以 `evergenie docs show conventions/terms.md` 為準（跟著程式的說明文件：下載版、原始碼版都用這個指令讀，不要自己去找 repo 的檔；`evergenie docs list` 看還有哪幾份）；這裡只列你要做的事。

## 什麼時候用
- 要新頁面、新角色、新資料種類（例：習慣、藥單、園藝紀錄）、新指令，或要把做好的套件分享出去。
- 只是想**改官方套件的行為**：開發家直接改官方（見上）；正式家先看官方套件有沒有擴充點（`evergenie packages extensions <套件>` 看有哪些與現在的值，`evergenie packages set-extension <套件> <擴充點> --value …` 改：`setting` 是設定值、`prompt_append` 是接在秘書指令後面的一段、`page_slot` 是頁面上可以掛自己頁面的位置）；官方套件不能整包複製來改（複製之後每次升級都要自己合併）；擴充點做不到的，另外做一個自己的新套件補上，或告訴主人「這要到開發家改」。

## 步驟
1. **問清楚**：套件叫什麼、要記什麼、要不要自己的頁面、要不要專屬角色。套件 id 是小寫英文開頭、小寫英文數字與 `-`、共 2～31 字（例 `garden`），不能跟官方、已裝、已有的套件同名。
2. **建鷹架**：`evergenie packages new <id> --name <顯示名稱>`。它會在 `packages/mine/<id>/` 放好 `package.toml`、`agents/<id>/agent.md`（套件附帶的職位）、`personas/`、`pages/index.tsx`、`schemas/`、`README.md`，頁面網址是 `/p/<id>/index`。被拒（撞名、格式不對）就照白話原因換一個 id。
3. **加頁面與資料**（只改 `packages/mine/<id>/` 裡面）：
   - 頁面：`pages/<頁面>.tsx`，在 `package.toml` 加 `[[contributes.page]]`（和對應的 `[[contributes.menu]]`）；只能 import `react`、`@evergenie/ui`、`@evergenie/sdk-web`、`@evergenie/host` 與自己資料夾的檔案。
   - 資料種類：`schemas/<種類>.toml` 寫欄位規格，在 `package.toml` 加 `[[contributes.data]]`。資料存在 `vault/pkg/<id>/`，用 `evergenie data …` 讀寫；不要把使用者的資料、金鑰或家的路徑寫進套件資料夾（它會被分享）。
   - 欄位格式（`package.toml` 與資料種類的欄位規格）用 `evergenie docs show schemas/package.md` 讀。改完用 `evergenie packages reload --id <id>` 重新載入，再看 `evergenie packages get <id>` 是不是 `running`。
   - **要編譯 Go 時**（`kind = "program"`、帶 Go 程式的套件）：`evergenie packages get <id>` 的 `remedy` 是 `install_go`（或 `evergenie packages go-status` 結束碼 3）＝這台沒有能用的編譯工具。**不要自己下載或安裝**（不用 brew、curl，也不先跑 `install-go`）：用提問工具問主人一句：照 `go-status` 的 `ask` 原文問（它會說大約多大、裝在程式自己的資料夾裡，結尾是「要我裝嗎？」；選項「好，裝」「先不要」）。主人選「好」才跑 `evergenie packages install-go --confirmed`（從 Go 官方下載、對過檢查碼才放進去；裝好自己重新編譯等著 Go 的套件），再看 `packages get <id>` 是不是 `running`。沒成功會印白話原因與下一步，照著轉述，不要自己改用別的方法裝。主人說先不要就停在這裡、記下來，不要一直問。`go-status` 的 `installable` 是 false（原始碼版的開發家、這台晶片沒有可下載的包；這時 `packages get` 不帶 `remedy`）時不問下載：照 `detail` 跟主人說（原始碼版是 Go 要自己裝，裝好 `evergenie packages reload` 再編）。套件的 `error` 說 go.mod 要的 Go 比這台新：不是要裝 Go，照它說的改 go.mod 的 go 那一行。
   - **寫 Go 程式的套件**（`kind = "program"`）：套件資料夾放 `go.mod`，只寫兩行 `module mine/<套件 id>`（加 `mine/` 才不會跟 Go 內建的套件同名）與 `go 1.24`（要更新的語法才往上加）；程式照常 import sdk（模組路徑跟官方套件的 import 一樣），**不要寫 sdk 的 `require`、`replace`**：核心編譯時用正在跑的程式旁邊附的 sdk（升級後自動換成新版的），寫了也不算（舊的、從官方套件 fork 來的有寫也沒關係，不用刪）。sdk 有哪些東西就讀那一份，不要照記憶寫：它在 `evergenie docs list` 第一行印的程式資料夾裡的 `sdk/`（下載版是正在用的那一版的程式資料夾，原始碼版是 repo；兩種都用這個指令找，不要自己猜路徑）。不要自己跑 `go build`、`go mod tidy`、`go get`（下載版的 PATH 本來就沒有 go；`go mod tidy` 不認附的 sdk 會失敗）：改完 `packages reload --id <id>` 讓核心編。真的要用外部模組（盡量不要）：在 `go.mod` 手寫 `require <模組> <版本>`，核心編譯時從 Go 的公開模組站下載（要網路；私有的程式庫連不到）。編不過時 `packages get <id>` 的 `error` 先是一句白話，要改程式的後面接「編譯工具的原文：」與 go 的原文最後幾行（主人收到的通知只有白話）：「工具組（sdk）找不到」→ 照它說的處理（重新安裝這一版；你自己就能做，不用轉給別人）；「跟這一版的工具組對不上」（升級換了 sdk 之後）或「用到了…sdk 裡沒有的套件」→ 讀這一版的 sdk、照原文改程式；「外部模組…下載不到／找不到」→ 等網路或換掉那個模組；「外部模組 X 要 Go N 以上」→ 把那個模組換成舊一點的版本；「套件的 go.mod 要 Go N 以上」→ 照它說的改 go 那一行；「go.mod 把工具組（sdk）指到找不到的資料夾」→ 把 `go.mod` 裡跟 sdk 有關的 `require`、`replace` 拿掉，只留兩行；「編譯超過 5 分鐘還沒好」→ 多半是網路慢，等一下再 reload；「程式編譯沒有過」→ 程式寫錯了，照原文改。跑著的套件重編失敗會繼續跑舊的（`error` 寫「仍在跑舊版」；舊的後來也停了就只寫編譯失敗，不會自己重啟，改好再 reload）。
   - **驗收（不能省）**：`running` 只代表載入了，不代表頁面能用。①用 `evergenie data` 真的存一筆、再讀回來；②打開頁面看一次：網址是 `<看板網址>/p/<id>/<頁面>`（看板網址是 `web.public_url`；`route` 只是會轉過去的別名，告訴主人時用 `/p/…`），有瀏覽器工具就自己開來看剛存的那筆有沒有出現、按鈕能不能存；沒有就把完整網址給主人，請他開來看一眼。頁面沒人看過就不要說做好了。
4. **角色放進套件**（要專屬角色才做）：新角色用 `evergenie personas new --to-package <id> <slug> [--name <名字>]`；已經有「我的角色」要放進去用 `evergenie personas move <我的角色> --to-package <id> [--slug <代號>]`（複製，原本的不動，頭像一起帶）。指令會自己寫 `packages/mine/<id>/personas/<slug>.md` 並在 `package.toml` 加 `[[contributes.persona]]`，不用手寫；放進去是設計中，填好個性後 `evergenie personas check <id>.<slug>`、`evergenie personas live <id>.<slug>` 才能指派。只限 `packages/mine/` 的套件，官方與分享來的套件會被拒（`FORBIDDEN`）。角色代號是 `<id>.<slug>`。
5. **預演秘書**：`evergenie packages sync <id>` 只預演（列出會新增的秘書、排程、預設角色）。把預演結果用白話告訴主人。真的套用只有主人能做（`--apply` 你跑會被擋 `FORBIDDEN`）；不要自己 apply，走第 7 步讓他按一下就好。
6. **選單**：套件的選單項目會出現在「未分類」，由主人在看板「設定→選單」自己放到想要的地方；你不改他的選單樹。
7. **提醒主人選角色（一鍵設好）**：建好、預演過之後：
   - 先在對話白話說：「這個套件帶的職位還沒有角色擔任，建議派 X（理由），要這樣設嗎？我發一則通知給你，按一下就設好。」建議誰：套件自己帶的上線角色優先，其次還沒擔任任何職位的上線角色（核心也照這個順序挑；你有更好的理由可以用 `--persona <角色>` 指定，那位要已經上線）。
   - 再發通知：`evergenie packages claim-notify <id>`（職位 id 跟套件 id 不同時加 `--agent <職位>`）。它會印出通知編號、建議的角色與理由。
   - 主人按了之後會發生什麼（照實跟他說）：
     - 「好，用 X」：核心派 X 擔任這個職位、建好這位秘書並讓她上班（等於主人在設定頁按「請她上班」）；你不用、也不能自己 `--apply`。之後用 `evergenie agents list` 看得到她。送出前核心會再檢查一次（職位還在、還沒有人擔任、X 還能指派），不行就不動並發系統提醒，請主人到設定頁那張卡重選。
     - 「換一個」：看板直接開到「設定→職位與秘書」那張認領卡，他自己挑角色、按「請她上班」。
     - 「之後再說」：記下來，不再問；那張認領卡一直留在設定頁，他想好了自己去按。
   - **不要重複追問**：同一個職位只發一次。之前問過、主人說之後再說或換一個，`claim-notify` 都不會再發（印「沒有發新的通知」與原因）；你也不要在對話裡一直催。
   - 主人在對話裡直接說「好」：還是請他按那則通知（或設定頁那張卡），你不能替他套用。
   - 沒有任何上線的角色時指令會說清楚：先陪主人在看板的「角色」頁做一位並上線（或第 4 步的套件內角色 `personas live`），再發。
8. **分享**（主人說要給別人用才做）：`evergenie packages export <id> --git --out <資料夾>` 產生可以直接放上 git 的資料夾（只帶套件資料夾，不帶他的資料 `vault/pkg/<id>/`；你不要自己 `git init`／`git push`，把印出來的下一步交給主人）；要一個檔就 `--out <名字>.tar.gz`。匯出前會掃疑似個資：被擋（結束碼 1）就把列出的檔案與行號念給主人、一起改掉；主人看過、確定沒問題才加 `--allow-findings`。官方套件不用匯出。別人用 `evergenie packages install <網址>` 安裝：預覽會列出帶來的角色（`<id>.<slug>`），對方同意才加 `--yes`。

## 不要做
- **正式家**不改 `packages/official/`（家裡或 repo 裡的官方套件）、核心（`internal/`、`sdk/`）、`evergenie` repo、官方角色卡 `personas/<代號>.md`；做客製不需要改它們。開發家可以改（開分支、跑 `make ci`、push 先問主人），但主人要的新東西仍用 `packages new` 建在 `packages/mine/`，不要為了新套件去改官方。
- 不動 `packages/installed/`（別人分享、裝進來的）、別的套件的資料夾與 `vault/pkg/<別的 id>/`。
- 不手寫整個套件資料夾來代替 `packages new`；不在套件資料夾放使用者資料、金鑰、家的路徑。
- 不自己 `--apply`、不替主人回答通知、不替主人調選單；被權限擋住就走 `request-permission` 技能，不繞過。
- 沒問過主人不下載 Go，也不自己用 Homebrew 或別的方法裝編譯工具；主人同意才 `packages install-go --confirmed`。
