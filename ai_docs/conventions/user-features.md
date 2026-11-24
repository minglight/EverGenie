# 使用者自己的套件（資料夾地圖與命名）

Status: DRAFT — 2026-10-30（SEC-302 / DEV-40 S1；S5 加「分享」；S6 加「壞了會怎樣」；決策 D-41），pending author review.

使用者想要一個自己的新套件（例：`garden`）時讀這份：寫程式的秘書（code、admin）與要幫使用者建套件的秘書都用得到。總規則在 [`packages.md`](packages.md)，manifest 欄位在 [`../schemas/package.md`](../schemas/package.md)；這份只講「使用者的套件放哪、什麼會被分享、什麼永遠不碰、頁面怎麼寫」。這份和它連到的幾份跟著程式發行：秘書用 `evergenie docs show <文件>` 讀（`evergenie docs list` 看有哪幾份），下載版與原始碼版一樣。

## MUST
- **UFE-1** — 新套件一律用 `evergenie packages new <id>` 建，它在 `packages/mine/<id>/` 放好 `package.toml`（`kind = "data"`、`api = "evergenie.v1"`、`version = "0.1.0"`）、`agents/<id>/agent.md`、`personas/`、`pages/index.tsx`、`schemas/`、`README.md`；不手寫整個資料夾，也不用改官方套件來做新套件。
- **UFE-2** — 套件 id 要符合 `^[a-z][a-z0-9-]{1,30}$`（小寫英文開頭，共 2～31 字）；不能是下面的保留字；不能跟任何官方套件、已裝的分享套件、自己已有的套件、或別的套件帶來的職位（職位 id）同名。格式與保留字回 `INVALID`，撞名回 `CONFLICT`，訊息是白話。
- **UFE-3** — 建套件只寫自己這層 `packages/mine/`：先寫在隱藏的暫存資料夾、用 `ParseManifest` 驗過才 rename，失敗什麼都不留；官方層與分享層（`packages/official/`、`packages/installed/`）一個位元組都不動；只有主人或秘書（帶操作者）能呼叫，沒有操作者回 `INVALID`。
- **UFE-4** — 套件的標準網址固定：頁面 `/p/<id>/<頁面 id>`（新套件的第一頁是 `/p/<id>/index`）、公開檔 `/pkg-files/<id>/…`；網址只靠 id，不加使用者前綴、不另開頂層網址。選單放在哪裡由使用者在「設定 → 選單」決定（D-11、PKC-4）；程式與文件不寫死 `/p/…` 字串以外的位置假設，要連到套件頁用選單項目 id 或頁面 id 查路由。
- **UFE-5** — 建好的新套件在這台一定載得起來（`running`、tier `mine`），頁面編得過，路由 `/p/<id>/index` 可解析，選單項目出現在選單池：使用者還沒存過選單時預設放「我的套件」群組，存過就在「未分類」由他自己搬（REQ-349）。頁面標題旁標「我的」。
- **UFE-6** — 這份文件的保留字清單與程式的 `pkg.ReservedIDs` 逐字一致，改一邊要改另一邊；核心看板的路由第一段都在清單裡。
- **UFE-7** — 分享套件一律用 `evergenie packages export <id>`：只帶套件資料夾，絕不帶 `vault/pkg/<id>/` 與 `runtime/`；匯出前的個資掃描擋下時，先跟主人一起改掉，主人看過同意才 `--allow-findings`（見下面「分享」）。
- **UFE-8** — 主人的東西與升級（DEV-154，2026-11-12 定的 8 個原則，改官方套件、核心、升級與部署流程的人都要守；細節在下面「主人的東西與升級」一節）：升級、部署、遷移不能蓋掉、刪掉或搬走家裡主人的東西；官方的新版放旁邊，由秘書整理。

## NEVER
- **UFE-N1** — 不為了新套件改 `packages/official/`、核心（`internal/`、`sdk/`）或 `evergenie` repo；要改官方行為走擴充點；官方套件不能整包複製來改，擴充點做不到的另外做自己的套件（PKC-6）。
- **UFE-N2** — 不動 `packages/installed/`（別人分享、裝進來的）與別的套件的資料夾與 `vault/pkg/<別的 id>/`。
- **UFE-N3** — 套件的資料夾裡不放使用者的資料、金鑰或家的路徑（資料在 `vault/pkg/<id>/`，不會被分享）；鷹架本身不含家的路徑。
- **UFE-N4** — 不用 `cp`、`tar`、`git push` 直接把 `packages/mine/<id>/` 送出去（會帶到 `.git`、暫存、符號連結指到的資料，也沒有個資掃描）；也不替主人 `git init`／`git push`。

## 資料夾地圖
`<家>` 是 EverGenie 的家（不是 repo）。每一列說：是什麼、會不會被分享、能不能碰。

| 位置 | 是什麼 | 分享時 | 誰能改 |
|---|---|---|---|
| `<家>/packages/mine/<id>/` | **程式與宣告**：這個套件自己的資料夾（下面各項都在它裡面） | `packages export` 帶整個資料夾（排除 `.git`、`.fork-base`、`.source.toml`、`node_modules`、隱藏暫存、編譯產物、符號連結） | 使用者與他的秘書 |
| `package.toml` | 套件說明書：名字、版本、頁面、選單、資料種類、要用的能力 | 會 | 使用者與秘書 |
| `agents/<id>/agent.md` | 這個套件附帶的職位的職責樣板（`packages sync` 預演、主人同意才建秘書） | 會 | 使用者與秘書 |
| `personas/` | 這個套件自己帶的角色卡（`<slug>.md`，在 `package.toml` 用 `[[contributes.persona]]` 宣告）與頭像（`avatars/<slug>.png`／`.mp4`）；角色代號 `<id>.<slug>` | 會（角色跟著套件走，裝好就在對方的角色庫） | 使用者與秘書 |
| `pages/` | 頁面（`.tsx`，核心內嵌編譯；只能 import react、`@evergenie/ui`、`@evergenie/sdk-web`、`@evergenie/host` 與自己資料夾裡的檔案） | 會 | 使用者與秘書 |
| `schemas/` | 資料種類的欄位規格（`<種類>.toml`，用 `[[contributes.data]]` 宣告） | 會 | 使用者與秘書 |
| `README.md` | 給人看的說明 | 會 | 使用者與秘書 |
| `<家>/vault/pkg/<id>/` | **使用者的資料**：用這個套件記下來的東西 | 分享套件（`packages export`）**永遠不帶**；解除安裝也保留。唯一的例外：主人用 `personas export` 點名匯出角色時，套件宣告 `export` 的種類裡屬於那位角色的紀錄跟著走（PKG-57、D-63） | 只經 `evergenie data` 與 vault API |
| `<家>/runtime/packages/<id>/` | 執行時的衍生檔（`public/` 是對外公開檔、`out.log`） | 不分享（可重建） | 只有附程式的套件自己的 `public/` |
| `<家>/settings/packages.toml` | 使用者對這個套件的設定（啟用、擴充點值） | 不分享 | 設定頁 |
| `<家>/personas/mine/` | 使用者個人的角色卡（跨套件，角色包與分享的套件都不帶，PRS-N1） | 分享套件**不帶**（要跟著套件走就複製進套件的 `personas/`）；主人可以用 `personas export` 點名匯出私下給別的家（PRS-8、D-63） | 角色庫 |
| `<家>/packages/installed/` | 別人分享、裝進來的套件 | — | **永遠不碰**（只能 `packages upgrade`／`uninstall`） |
| `<家>/packages/official/`、repo 的 `packages/official/` | 官方套件，跟著發版 | — | **永遠不碰**（改行為走擴充點） |
| `evergenie` repo（`internal/`、`sdk/`、`web/`） | 核心程式 | — | **永遠不為了做一個使用者套件去改** |

## 命名與網址
| 東西 | 規則 |
|---|---|
| 套件 id | `^[a-z][a-z0-9-]{1,30}$`；同時是套件 id、資料夾名、預設職位 id、資料種類前綴（`<id>.<種類>`）、`vault/pkg/<id>/` |
| 頁面 | `/p/<id>/<頁面 id>`；新套件的第一頁 `index`；`[[contributes.page]]` 的 `id` 就是網址最後一段 |
| 公開檔 | `/pkg-files/<id>/<路徑>`（只限圖片與影片，PKC-10） |
| 選單 | `[[contributes.menu]]` 只是「可用項目」，放哪由使用者的選單樹決定；沒放進樹的在「未分類」；`group_hint` 只是建議 |
| 角色頭像 | `/avatars/<職位或角色代號>`；套件自帶的角色代號是 `<id>.<slug>`（例 `garden.fairy`），頭像放套件裡的 `personas/avatars/<slug>.png` |

## 保留字
下面這些字不能當新套件 id（程式在 `internal/feature/packages/pkg/newpkg.go` 的 `ReservedIDs`；改 EverGenie 的人兩邊一起改，UFE-6）。官方套件的名字另外由「撞官方名」擋（也列在這裡是因為官方套件資料夾可能不在這台）。只擋會撞系統的字，常見生活詞（例：`todo`、`notes`）不擋。
<!-- reserved-ids:start -->
- 看板網址與選單：`admin`、`api`、`assets`、`avatars`、`chat`、`core`、`goals`、`home`、`login`、`messages`、`overview`、`p`、`personas`、`pkg-bundles`、`pkg-files`、`policy`、`reminders`、`settings`、`share`、`static`、`tasks`、`upload`
- 核心與家：`agents`、`bus`、`evergenie`、`node`、`notify`、`runtime`、`scheduler`、`sdk`、`session`、`vault`
- 套件層與套件資料夾：`installed`、`mine`、`official`、`package`、`packages`、`persona`
- 預設的職位（秘書名冊）：`art`、`code`、`life`、`system`、`work`
- 內建官方套件：`body`、`closet`、`closet-mine`、`fitness`、`habits`、`health`、`nutrition`、`secretaries`
<!-- reserved-ids:end -->

## 頁面怎麼寫
套件的頁面是 `pages/` 裡的 `.tsx`，由核心內嵌編譯（主人不用裝 Node）。在 `package.toml` 用 `[[contributes.page]]` 宣告（`id` 是網址最後一段，`component` 指到檔案），要出現在選單再加 `[[contributes.menu]]`；欄位見 [`../schemas/package.md`](../schemas/package.md)。
1. **只 import 外殼給的**：`react`、`@evergenie/ui`（按鈕、讀取中、空狀態這類元件）、`@evergenie/sdk-web`、`@evergenie/host`，與自己資料夾裡的檔；不自己帶 React 或別的 UI 套件（PKC-N2）。
2. **元件**：檔案 `export default` 一個 React 元件；拿到的 props 有 `pkg`（套件 id）、`page`（頁面 id）、`search`（網址的查詢字串），掛在核心頁面上的另外有那個位置給的（第 5 點的表）。
3. **讀寫資料**：用 `@evergenie/sdk-web` 的 `api.vault`（`list`、`get`、`put`、`patch`、`archive`、`nextId`）。看板只開放套件的資料種類（`<套件>.<種類>`），核心的種類讀不到；自己的頁面讀寫自己宣告的種類（`<id>.<種類>`）。欄位不合由核心擋下、回白話原因（說明卡 board 的 WEB-23）。秘書在對話裡改了資料，頁面要跟著重讀：`onLiveChange("<id>.", reload)`（呼叫前先確認它存在；不要自己開串流，說明卡 packages 的 PKG-49）。
4. **換頁**：用 `@evergenie/host` 的 `navigate(href, {replace?})`，不要改 `window.location`（說明卡 board 的 WEB-165）；連到自己的頁面用 `/p/<id>/<頁面 id>`（UFE-4）。「上一頁」由看板的外殼畫，頁面不自己畫「回到…」的返回連結；自己呼叫 `history.replaceState` 時要帶 `history.state`，不能傳 `null`（會洗掉歷史位置，前端規範 FEN-29）。
5. **掛到核心頁面上**：`[[contributes.page]]` 加 `slot = "<位置>"`（不用 `[[contributes.menu]]`）。核心只留位置、不知道誰會掛；每個掛的頁面各自包一層保護（一個壞了不影響別的），套件關掉就消失。同一個位置的排序：`slot_front = true` 的在前，其他照頁面 id（`<套件>.<頁面>`）。這張表是正本：

   | 位置（`slot`） | 在哪 | 元件多拿到的 props | 怎麼顯示 |
   |---|---|---|---|
   | `core.overview.cards` | 今天頁 | — | 每個掛的頁面一張卡；卡片宣告「現在是空的」時手機縮成一行（見下面） |
   | `core.reminders.top` | 待辦頁最上面 | — | 依序疊 |
   | `core.persona.tab` | 一位角色一頁 `/personas/<代號>` | `persona`（角色代號）、`item`（網址 `?item=` 的值，沒有是空字串）、`onItem(id)`（把分頁裡現在打開的那一項寫進網址，空字串＝關掉） | 一個頁面一個分頁，標題＝頁面 `title`，網址 `?tab=<套件>.<頁面>`；分頁裡點開某一項要記在網址（重新整理回同一個畫面）就用 `item`／`onItem`，值由套件自己定；「介紹」是核心的、永遠在最前面 |
   | `core.personas.new` | 角色清單的「新增角色」對話框 | `onDone(代號?)`、`onCancel()` | 一個頁面一種新增方式（分段按鈕，標題＝`title`），最後是核心的「進階：只寫角色卡（沒有圖）」 |

   照官方套件 `habits`（今天頁的卡、待辦頁最上面）、`closet-mine`（角色分頁、新增角色）的 `package.toml` 與頁面寫最快。

   **只在某些家才出現**（DEV-160）：掛的頁面加 `show_when`，條件不成立核心就不列（分頁、卡片都不出現）。`cap:<套件>.<能力>`＝那個套件說這個家支援才列（例 `show_when = "cap:closet.edit"`：衣櫃說這個家能「修改」衣服才有這個分頁）；`agent:<職位>:enabled`＝那位秘書啟用中才列。頁面自己不要判斷是哪種家。自己的套件也想給別人用 `cap:`：宣告一個資料種類 `capabilities`（`ids = "given"`），由你的程式或秘書寫 `<套件>.capabilities/main`，欄位 `supports` 是能力名稱的清單（說明卡 inbox 的 MNU-11）。

   **今天頁的卡片沒有資料時**（board WEB-177）：手機的今天頁很擠，還沒有資料的卡片不要佔一整張。卡片自己知道「現在是空的」時，用 `@evergenie/ui` 的 `EmptyLine` 把整張卡包起來，給一行版的字與下一步；手機的今天頁就只畫一行「體重：還沒記　＋記一筆」，桌機與別的位置照舊畫你的整張卡。有資料時不要包（直接畫卡片）；讀取中、讀不到也不要包（讀不到用 `LoadFailed`）。不包也不會壞，只是手機上永遠是整張卡。

   ```tsx
   import { EmptyLine } from "@evergenie/ui";

   const card = <a href="/p/body/log" className="…">…整張卡…</a>;
   if (loaded && latest === null) {
     return (
       <EmptyLine label="體重" text="還沒記" action={{ label: "＋記一筆", href: "/p/body/log" }}>
         {card}
       </EmptyLine>
     );
   }
   return card;
   ```

   `label`＝這張卡是什麼、`text`＝現在的狀況（一句短話）、`action`（選填）＝一個連結 `{ label, href }` 或一顆按鈕 `{ label, onClick }`（例：開一個抽屜）；連結也可以帶 `onClick`（`{ label, href, onClick }`：按下去先做一件小事再照連結換頁，例：習慣只有一位秘書時「約一個」先在聊天起好頭再開她的聊天）。字用白話，不放代號。
6. **範例**：`packages new` 建的 `pages/index.tsx` 是最小的一頁；完整的例子看官方套件（例：`habits`、`nutrition`）的 `pages/`，在 `evergenie docs list` 第一行印的程式資料夾裡的 `packages/official/<套件>/pages/`。
7. **看大圖**：要讓人把圖片或影片放大看，用 `@evergenie/ui` 的 `MediaViewer`，**不可以用新分頁開圖片或影片**（`<a target="_blank">`、`window.open`）——主人把看板加到手機主畫面後沒有分頁列，開了就退不回來（前端規範 FEN-30）。站外連結除外。寫法：`const [at, setAt] = useState<number | null>(null)`；縮圖 `<button type="button" onClick={() => setAt(i)}><img src={url} alt="" /></button>`；旁邊放 `<MediaViewer items={[{ src: url, name: "名字" }]} index={at} onIndex={setAt} />`（`items` 放多張就可以左右換，影片寫 `type: "video"` 或用 `.mp4` 這類副檔名）。右上角 ✕、點背景、Esc、手機的上一頁都會關掉並回到原來的位置，不用自己做。
8. **驗收**：存檔後 `evergenie packages reload --id <id>`，`evergenie packages get <id>` 是 `running`；再真的打開 `/p/<id>/<頁面 id>` 看一次（技能 `create-feature` 的驗收步驟）。

## 分享
把自己做的套件給別人用（SEC-302 S5，指令與模組行為見 `cli` 卡 CLI-27、`pkg` 卡 PKG-30～PKG-33）：

1. **匯出**：`evergenie packages export <id> --git --out <資料夾>`（要一個檔就 `--out <名字>.tar.gz`；不給 `--out` 放 `<家>/runtime/exports/<id>-<版本>/`）。只有 `mine`（自己的）與 `installed`（分享來的，可以再分享）能匯出；官方套件不用匯出（別人升級就有），fork 的官方套件也不行（對方有同名官方套件，裝不起來；要分享請 `packages new` 做成自己的套件）。
2. **帶什麼**：只帶套件資料夾——`package.toml`、`agents/`、`pages/`、`schemas/`、`personas/`（角色跟著走）、`README.md` 與你自己放的檔。**不帶**：你的資料 `vault/pkg/<id>/`、`runtime/`、`settings/packages.toml`（啟用與擴充點的值）、`.git`、`.fork-base`、`.source.toml`、`node_modules`、隱藏暫存、編譯產物；符號連結不跟（指到資料夾外面的會整個擋下並列出，請換成真的檔）。沒帶的東西會列在輸出的「沒有帶的」。
3. **個資掃描**：匯出前掃一遍，看起來像金鑰／token、家的絕對路徑（`/Users/<名>`）、email、`.env`／`secrets` 之類的檔，就停下列出「檔案:行號　說明」、什麼都不寫（結束碼 1）。秘書把清單念給主人、一起改掉（金鑰改放設定或擴充點、路徑改成相對的）；主人看過、確定沒問題才加 `--allow-findings`。掃描寧可多報；它不自動改檔。
4. **放上 git**：`--git` 會多一份 `.gitignore`，但不會幫你 `git init`／`git push`——主人自己（或他同意後）`git init && git add -A && git commit && git tag v<版本> && git push`。之後改版就改 `package.toml` 的 `version`、打新的版本標籤，對方用 `packages outdated`／`packages upgrade` 更新。
5. **別人怎麼裝**：`evergenie packages install <網址>`（`https://`、`ssh://`、`git@`、`file://`）先印預覽，列出帶來的秘書、頁面、資料種類與**帶來的角色**（`<id>.<slug>`、名字、有沒有頭像），對方同意才加 `--yes`；本機資料夾直接 `packages install <完整路徑>`。裝好角色馬上在角色庫（可以指派），頁面在 `/p/<id>/…`；對方的資料從空的開始。更新（`packages upgrade`）不動對方的 `settings/packages.toml` 與 `vault/pkg/<id>/`。

## 壞了會怎樣
使用者的套件壞了只影響它自己（D-41 ⑧，PKC-8），不用怕試：
- `package.toml` 寫錯：只有這個套件載不起來（`rejected`），官方與別的套件、選單、頁面照常；`doctor` 一條黃字說原因，存檔修好就自動重新載入。
- 附程式的套件一直掛：核心依設定重啟它並發系統提醒（同一個套件 10 分鐘最多一則），連續失敗太多次就停止重啟；別的照常。
- 核心升級或重開：`packages/mine/`、`personas/mine/`、`vault/pkg/<id>/` 一個位元組都不動。
- 移除套件（刪掉 `packages/mine/<id>/`）：它的角色從角色庫消失；坐那位角色的秘書照樣上班（暫用代號當名字），`doctor` 黃字提醒換一位角色。
- 以上 `doctor` 都只有黃字，不會因為使用者的套件擋住部署或升級。資料只透過 `evergenie data` 寫進自己的 `vault/pkg/<id>/`；這是慣例不是沙盒（D-19），不要繞過它去寫別的套件的資料夾（UFE-N2）。

## 主人的東西與升級
這一節給改 EverGenie 本身（官方套件、核心、遷移、升級與部署工具）的人看；替主人做自己的套件不用管它。
2026-11-12 定案（DEV-154「使用者自己的東西模組化」）。寫任何會碰到家的程式（官方套件、核心、遷移、升級與部署工具）之前先對一遍：

| # | 原則 | 現在怎麼守（條文） |
|---|---|---|
| 1 | 家裡都是主人個人的；官方的東西唯讀、不放家裡 | 官方套件在程式資料夾（PKG-9 四層）；官方職責跟著程式走、家裡只留附加規則（說明卡 agent 的 AGT-10、AGT-38，packages 的 PKG-63）；新家（`evergenie init`）一開始就是官方角色放家外的佈局：官方卡、圖、衣服目錄紀錄（資料層的官方層，說明卡 vault 的 VLT-19）、角色包登記都在狀態資料夾；公開角色的卡跟著程式（官方套件 `personas` 的 `cards/`，下載版在 `versions/<版本>/` 裡，換版跟著換，agents 的 PRS-25），升級與角色包同步不寫家裡的檔（agents 的 PRS-20；發版整套測試第 4 段比對家裡每個檔的雜湊）；舊的家照 `[layout] official_personas = "home"` 留在家裡，由搬家工具 `evergenie home tidy --move-official` 搬（agents 的 PRS-22；沒有角色包登記檔的舊家要主人用 `--official` 確認哪些卡是官方的）；官方不再提供的衣服，目錄、圖與兌換紀錄都留著（PRS-21）；別人分享的套件在狀態資料夾 |
| 2 | 主人改過的一律留著；官方新版放旁邊，秘書整理 | 升級不動 `packages/mine/`、`personas/mine/`；程式帶的公開卡排在主人同代號的卡後面（說明卡 agents 的 PRS-27）；退回時的資料還原只動核心管的資料夾（說明卡 system 的 UPD-9 ⑤） |
| 3 | 官方套件的紀錄格式只能加欄位；真的要改就附自動轉換，而且先驗證好 | PKC-12、`[[migration]]`（PKG-61）；沒附轉換發版檢查不過（PKG-62，`make lint`）；升級先在複本上試跑（UPD-9 ①） |
| 4 | 升級後的問題統一交給電腦秘書（`system` 職位；沒有就名冊第一位） | 升級報告（UPD-10） |
| 5 | 第一版不開放整包複製官方套件；只留擴充點與自己做套件 | PKC-6；複製的入口已關（`pkg.ForkClosed`）；擴充點三種都生效、別人分享的套件不能靠預設值往秘書的指令塞字（PKG-4） |
| 6 | 主人自己的角色代號 `my_<名字>` | 新建的已經是（PRF-1、AGT-20）；舊代號由搬家工具 `evergenie home tidy` 改（卡、圖檔名、名冊、套件紀錄一起，可逐檔退回，PRS-22、PRS-24）；套件的紀錄怎麼跟著代號走看套件自己的宣告（packages 的 PKG-64），沒宣告的只列出來；開發家把自己的角色升成官方用 `evergenie personas promote`（PRS-23） |
| 7 | 系統不認得的檔案不掃、不碰、不擋升級 | `vault/` 只認核心資料夾（說明卡 vault 的 VLT-18）；主人自己的、分享來的套件壞了（含資料轉換沒過）只停它自己、不擋升級與啟動（PKG-61、UPD-1）；升級前就有的健康檢查紅字不算這次升級的錯（UPD-1） |
| 8 | 轉換失敗：先在複本試跑；真的家上失敗自動退回；秘書修到升級通過、都能用；不用請主人點頭、不問刪備份（更新前的備份只留最新一份）；可以退回、資料一起還原 | UPD-9（試跑、`--rollback` 連資料）；部署工具走同一套（UPD-9 ⑥，發版手冊） |

什麼時候搬（2026-11-13）：發版準備期間新佈局只在開發家上跑；正式家等規則與工具都定了，最後一次搬完（佈局、密鑰搬出設定檔、改名一起），期間照常升級程式。搬家工具每次有改，就拿每個正式家的複本預演一次（預演、套用、退回、逐檔比對）。做完確定不退了要 `--forget` 收尾，不然不能再整理、也不能做 `home relayout`。

交件前三問（開發流程規範的 DEV-12）：這次改動會不會在升級時動到主人放在家裡的東西？改了官方套件的紀錄格式有沒有附 `[[migration]]` 並用真的舊資料試過？新加的資料夾、檔案位置，升級備份與還原、試跑複本顧得到嗎（`vault/` 新的最上層資料夾要加進 `vaultRoots`；家的遷移只能改 `home.toml` 與 `vault/` 的核心資料夾）？

## 給秘書
- 主人要新套件、新角色、新頁面或要分享套件時，**先讀技能 `create-feature`**（`packages/official/engine-claude/skills/create-feature/SKILL.md`；引擎開秘書 session 時以外掛載入，對話裡叫 `evergenie:create-feature`，不裝進帳號 `skills/`，ECL-36）：它是步驟與禁止事項的完整版，這份文件是它指向的資料夾地圖與命名規則，不在這裡重寫。
- 提醒這件事的條文在共用規則（核心內建，組進每位秘書的指令，包括新建的與停用的）；開發的人改的正本是 repo 的 `internal/base/agent/rules/shared.md`，發版就同步。
- 這份、[`packages.md`](packages.md)、[`../schemas/package.md`](../schemas/package.md) 等跟著程式發行的文件，一律用 `evergenie docs show <文件>` 讀（例：`evergenie docs show conventions/user-features.md`；`evergenie docs list` 看有哪幾份）。下載版沒有 `evergenie` repo，不要自己去找 repo 的檔，也不要叫主人去找。
- 秘書做新套件一律 `packages new`。正式家不改官方套件、核心、`evergenie` repo、官方角色卡（UFE-N1），要改就說「這要到開發家改」；開發家（`home.toml` `[runtime] env = "dev"`）的秘書可以在 `evergenie` repo 開分支直接改官方與核心（DEV-93；共用規則依 env 只放對應那一句，`context.go` 的 `codeRule`）。被擋走 `request-permission`，不繞過。
- 改了官方套件資料夾會被 `doctor` 的套件組點名（`packages.official_edited`，只在從 repo 跑的非沙盒家）。

## 設計決定
- **D-41** — 使用者自己的套件，骨架沿用 D-3 的四層：①一個使用者套件＝`packages/mine/<id>/` 一個套件資料夾，帶自己的頁面、資料種類、秘書樣板與角色（`personas/<slug>.md`＋`personas/avatars/`），跟著資料夾分享（`evergenie packages new`／`export`）。②「我的角色」（`personas/mine/`）永遠是個人的；要分享就複製進套件的 `personas/`，來源不動。③套件內角色的代號是 `<套件id>.<slug>`；官方與我的代號不含點，不會撞。④網址固定 `/p/<id>/<頁>`，前端只在 `routes.ts` 一處組；選單放哪由使用者決定（D-11），官方／分享的／我的靠 `tier` 標示。⑤套件 id 規則與保留字見本檔。⑥分享只帶套件資料夾，不帶 `vault/pkg/<id>/`、`runtime/`；分享前掃疑似個資，有就停下來給主人看。⑦秘書都知道主人要新東西就做套件：共用規則一條＋官方技能 `create-feature`，技能由 engine-claude 開秘書 session 時用 `--plugin-dir` 載入，不裝進帳號的 `skills/`；建好後發認領通知，主人按「好」才由核心以主人身分套用。⑧使用者的套件壞了只影響它自己，doctor 一律黃字、不擋部署。否決：`personas/mine/` 跟套件綁死；技能裝進帳號 `skills/`（帳號的所有 session 都看得到）；作業系統隔離（D-19）。

## Handoff
寫套件的頁面看上面「頁面怎麼寫」（核心看板外殼本身的規則在開發用的前端規範，不跟著發行）；寫附程式的套件讀 [`packages.md`](packages.md)；`packages new` 的指令與模組行為（CLI-16、PKG-20、PKG-N7）、`packages export`（CLI-27、PKG-30～PKG-33、PKG-N8）在說明卡 packages（開發文件，不跟著發行）。
