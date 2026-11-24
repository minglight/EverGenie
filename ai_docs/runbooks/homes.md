# 開發家與正式家：誰是正本、怎麼更新、怎麼上線

Status: DRAFT — 2026-11-14（DEV-157）；第 1～5 節的定義作者同日確認，公開前改寫成通用說法，pending review.

這是維護者自己發版用的配置：在開發機（部署目標名 `office`）開發，部署到開發機自己與另一台（目標名 `mini`），兩台各有正式家，另有開發家。一般使用者安裝與升級看 `INSTALL.md`／[install.md](install.md)，不用讀這份。

給要動 EverGenie 本身、做官方角色與衣服、或要部署的人與 AI 看。發版與部署的逐步做法在 [release.md](release.md)；這份只定義「有哪些家、什麼東西的正本在哪、開發家怎麼更新、東西怎麼上線」。職位用 id 寫：`admin`、`code`、`art`（生圖秘書）。

## 1. 有哪些家

| 家 | 在哪台 | 做什麼 | 跑哪一版程式 |
|---|---|---|---|
| office 開發家（原名工坊） | office | 改程式（核心、看板、官方套件）。秘書：`admin`、`code` | 主線最新（repo 主資料夾的 `bin/`） |
| mini 開發家 | mini | 做角色、衣服、圖。秘書：`art` | 跟 mini 正式家同一版（每次部署一起換） |
| 正式家：mini、office | 各自 | 日常使用 | 部署的那一版 |
| 不歸部署工具管的家（例 `family`） | mini | 日常使用 | 它自己的一份程式 |
| 公開版的新家 | 別人的電腦 | 別人日常使用 | 發行檔（下載版）＋公開角色包 |

開發家都寫 `[runtime] env = "dev"`，跑的是那台開發用 repo `~/Workspace/<登入名>/evergenie` 的 `bin/`；mini、office 的正式家跑部署 checkout `~/Workspace/<登入名>/evergenie-deploy` 的 `bin/`；`family` 家跑它自己的一份程式、手動升級，部署工具不碰它，只守著它的服務沒被影響。office 開發家怎麼建、怎麼上線、怎麼退回，見 release.md「工坊」。

## 2. 什麼東西的正本在哪裡

一樣東西只有一個正本。

| 東西 | 正本 | 其他地方怎麼拿到 |
|---|---|---|
| 程式、官方套件、文件 | EverGenie repo 的 main | 部署、發行檔 |
| 官方角色的卡、衣服、圖、衣服紀錄；公開名單 | mini 開發家 | 角色包（各家自己同步） |
| 主人自己的角色、沒升成官方的衣服、隱藏版 | 做的那個家 | 不進角色包、不進公開版（建包的排除規則與測試守著） |
| 每個家的日常資料 | 那個家自己 | 不同步 |

office 開發家不是角色的來源：不設公開名單，跟其他家一樣從角色包同步。（程式與家的設定還沒照這條改，見「已知限制」。）

## 3. 開發家怎麼更新

- **office 開發家**：跑 repo 主資料夾，併進 main 後用部署工具更新（現況不變；指令見 release.md 步驟 6）。
- **mini 開發家**：跟著每次部署一起更新（2026-11-14 定，D-66）。部署工具在部署 mini、office 時，把 mini 開發家換成同一版並重開；檢查沒過自動退回舊版。更新前後各傳一句話給 `art`。工具實際的做法是接在 mini 正式家那一步後面（`scripts/deploy.sh mini` 或 `all`；只部署 office 不會換它），細節見第 6 節。
- **在 mini 開發家改程式**：不在正在跑的那份 repo 上直接改。要改就開分支、commit，跟 `admin` 說；`admin` 審過、檢查通過、主人點頭後併進 main，下一次部署 `art` 就拿到。直接改了沒收好（有沒 commit 的改動、停在別的分支、在 main 上直接 commit），部署工具不會蓋掉，但 mini 開發家這次就不換版（第 6 節）；所以 commit 完要把正在跑的那份切回 `main`。
- 主人自己的套件（那個家的 `packages/mine/`）不受這條限制，照舊在家裡改。

## 4. 上線：三種東西，三條路

### A. 程式
開單 → 在 office 開發家改（分支）→ 自動檢查＋獨立審查 → **主人點頭** → 併進 main、推 → **主人點頭** → 部署（mini、office、mini 開發家；`family` 家另外手動升級）。逐步做法：release.md「一次改動的流程」。

- 退回：部署工具的退回（換回上一版，release.md「退路」）。
- 給別人的正式版：發版測試全過 → **主人按** → 發行檔（release.md「正式發版」）。

### B. 官方角色與衣服
1. `art` 在 mini 開發家做，放進「設計中」。
2. **主人在角色頁的「修改」分頁採用**（一張一張；每張都採用了才能「上架」，衣櫃卡 CLO-35）。展示間那位角色的一段有一行「N 張照片還沒採用」直接連過去（CLO-37）。
3. **主人在那套衣服的頁面按「升成官方」**（只在開發家出現、只認主人本人，秘書不能代按；隱藏版不能升）。按鈕在 角色 → 那位角色 →「修改」分頁那一套的塊裡（closet-mine CMI-16）；要一次看全部角色有哪些可以升：展示間上面的篩選選「可以升成官方」（只有開發家有這排），點一套就到那一套。可以升的那幾列在列表上標「可以升成官方」，展示間的衣服詳情頁也有連結直接到那一套。開發家沒兌換的衣服也看得到真圖（衣櫃卡 CLO-27），不用先兌換才知道是哪一套。採用只代表「我要穿」，按了升成官方才代表「可以公開」。
4. `admin` 在 mini 開發家建角色包。建包時隱藏版一律排除，結束會列出帶了幾位、幾套、排除了什麼。
5. 先用一個全新的家對這個包同步、檢查過。
6. **主人點頭** → 公開這個包（放上下載位置、把指標寫進 repo）。這一步對外、收不回，每次都問。
7. 各家（維護者自己的家、公開版的家）在升級後或定時自動同步；只新增，主人改過的不蓋。

退回：指標改回上一版的包；已經同步到各家的衣服不會自動消失（需要時另外處理）。

### C. 只有主人自己用的
主人自己的角色、採用了但不想公開的衣服（任何角色身上的都算）、隱藏版：留在做的那個家，不進角色包、不進公開版（建包的排除規則與測試守著）；隱藏版到了哪個家都照樣受隱藏版開關管（預設關、只有主人能開）。要搬到別的家由主人自己的套件做（名詞表「搬到我家」），官方套件不做這件事。

一套衣服採用後的去向：留在開發家，或升成官方（公開；主人的家會從角色包拿到，不用另外搬）。升成官方送出前再確認一次。

## 5. 公開版跟維護者自己的家
同一份程式、同一個公開角色包。維護者的家比公開版多的只有：家裡自己的東西（自己的角色、隱藏版、自己的套件）。所以公開版有的問題，維護者自己的家也會先遇到。

## 6. 部署工具怎麼換開發家

`scripts/deploy.sh`（規則：說明卡 admin 的 ADM-38、設計決定 D-66；正式家那一半見 release.md 步驟 5、6）。開發家不在那台的家清單 `~/.evergenie-homes` 裡（清單裡的家共用部署 checkout 的執行檔），所以另外換。

### 兩個開發家在工具裡的名字與位置
位置只寫在一處：`scripts/deploy.sh` → `devhome_conf`。服務名稱不寫在工具裡，`evergenie service restart` 照那個家 `home.toml` 的 `[runtime] service_label`。

| | office 開發家 | mini 開發家 |
|---|---|---|
| 目標名 | `workshop` | `mini-dev` |
| 什麼時候換 | 加 `--workshop` 才跟著 office 換；或單獨 `scripts/deploy.sh workshop` | **每次** mini 正式家部署成功後自動換；或單獨 `scripts/deploy.sh mini-dev` |
| 在哪台跑 | 本機（office） | ssh 到 mini（`EVERGENIE_DEPLOY_MINI_HOST`，release.md「部署工具的細節」） |
| repo（預設；可用環境變數改） | `~/Workspace/<登入名>/evergenie`（`EVERGENIE_DEPLOY_WORKSHOP_REPO`） | 那台的 `~/Workspace/<登入名>/evergenie`（`EVERGENIE_DEPLOY_MINI_DEV_REPO`） |
| 家（預設；可用環境變數改） | `~/Workspace/<登入名>/evergenie-workshop`（`EVERGENIE_DEPLOY_WORKSHOP_HOME`） | 那台的 `~/Workspace/<登入名>/evergenie-dev`（`EVERGENIE_DEPLOY_MINI_DEV_HOME`） |
| 換成哪個 commit | origin/main | 跟著 mini 部署時：mini 正式家這次部署的同一個 commit；單獨跑時：origin/main |
| 換版前後傳話 | 不傳 | 傳給那個家的 `art`（`EVERGENIE_DEPLOY_MINI_DEV_NOTIFY` 可改職位，空字串＝不傳） |
| 換回 | `scripts/deploy.sh --rollback workshop` | `scripts/deploy.sh --rollback mini-dev` |

### mini 開發家：工具做的事
`scripts/deploy.sh mini`（或 `all`）在 mini 正式家部署成功（含「已經是最新版」）、而且 mini 上要守的服務都在之後，接著：

1. **確認開發家在**：repo 有 `.git`、家有 `home.toml`、`evergenie service status` 是在跑。任何一個不是 → 說一聲「這次跳過」，不算失敗（不把沒在跑的開發家開起來）。`service status` 說這個服務管的是另一個家（開發家的 `home.toml` 沒設自己的 `[runtime] service_label`）→ 不換、結束碼 1：那時重開的會是別的家。
2. **確認 repo 可以動**：在 `main`、`git status --porcelain` 是空的（沒追蹤的新檔也算改動，`make build` 會把它編進去）、`main` 上沒有 origin 沒有的 commit。任何一個不是 → **不動它**（不切分支、不重設、不蓋掉任何檔），白話說原因（哪個分支、幾個檔、前 10 個檔名或 commit），開發家照舊跑原本的版本，並傳一句話給 `art` 說這次沒換版與該怎麼收。收好之後下一次部署 mini 會自己換，或單獨跑 `scripts/deploy.sh mini-dev`。
3. **快轉**到要換的 commit（只接受快轉）。repo 已經比這次部署的 commit 新（有人手動拉過）就不往回換、也不重編，不算失敗；要讓它跑 repo 現在那一版就單獨跑 `scripts/deploy.sh mini-dev`。讀不到正式家這次部署的是哪個 commit 時不換、結束碼 1。
4. 已經跑著這一版（`evergenie doctor` 的核心版本＝`scripts/version.sh`）就不重開、不傳話；`--force` 照做。
5. 傳一句話給 `art`：「部署工具：mini 開發家要從 <舊版> 換成 <新版>，等一下會重開服務…」。
6. 跟正式家**同一套**換版：舊 `bin/` 留成 `bin.prev/` → `make build` → 在複本上試跑資料整理 → 備份家 → `evergenie service restart` → 三項檢查（doctor、看板首頁、套件）。任何一步沒過：自動把 `bin.prev/` 換回、重開回舊版（整理過資料的先用備份還原）。家裡的資料不動。開發家的 repo 不寫 `bin/.deployed-version`。
7. 再傳一句話給 `art`：成功（從哪一版換成哪一版），或沒過的原因與已自動退回哪一版。

傳話用那個家的 `evergenie say <職位> <內容>`；那個家沒有這個職位、或傳不到，只印一句「傳不到，不算部署失敗」。

### 結果與結束碼
- mini 開發家沒換成（第 2 步停下、或第 6 步退回舊版）**不影響其他家**：mini 正式家已經部署好，`all` 照樣接著部署 office。最後的結束碼是 1，輸出寫「mini 正式家已經部署好了；mini 開發家這次沒換成」。
- 正式家的結束碼優先：mini 正式家沒過（1）、要守的服務少了（4）時不碰開發家。開發家換回也失敗是 3（要人處理）。
- `--rollback mini` 只換回 mini 正式家，不碰開發家；開發家要換回用 `--rollback mini-dev`。
- `--dry-run` 一樣印出開發家那一段的計畫（要換的 commit 寫 origin/main，真的跑時是正式家那個 commit），不 ssh、不執行。

### 驗收
`make deploy-test`（`scripts/deploy-test.sh`）第 19 段：正常換版與兩句傳話、跟正式家同一個 commit、repo 有沒 commit 的改動／沒追蹤的新檔／不在 main／main 上有 origin 沒有的 commit 時不蓋掉、檢查沒過退回舊版、開發家不在或沒在跑跳過、傳話失敗不影響、正式家沒過或要守的服務少了時不碰開發家、`mini-dev` 單獨跑與換回、`--dry-run`、位置寫成字面 `$HOME/…` 到那台才展開、服務管的是另一個家時不換。第 17 段是 office 開發家（`workshop`）。

## 已知限制
- 第 2 節「office 開發家不是角色的來源」只寫了定義，程式與 office 開發家的設定還沒改（角色包那一段的工作）。
- `family` 家不跟著部署工具升級，要手動升級；部署工具只守著它的服務沒被影響。
- mini 開發家的服務沒在跑時部署會跳過它，之後手動開起來跑的是舊版：開起來後跑一次 `scripts/deploy.sh mini-dev`。
- 傳話給 `art` 沒有設等待上限：那個家的核心卡住時，部署工具會停在傳話那一步，要人中斷。
- 部署工具改了 mini 開發家的 repo 之後，手動 `--rollback mini-dev` 只換回 `bin/`，repo 留在新的 commit（跟正式家一樣）。
