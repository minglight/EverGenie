# 前端：外殼、選單、頁面、slot

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

寫看板外殼、選單、頁面或套件頁面之前讀。選單的算法在 `inbox` 說明卡的 `inbox/menu`，偏好格式在 `schemas/menu-prefs.md`；畫面改動合併前必過的 UX 檢查（UXC-n，2026-11-02 從 `ux.md` 併進來）在下面。

## MUST
- **FEN-1** — 技術（D-11，版本照參考專案的前端，全部固定、不用 `^` `~`）：React 19.2.4、TypeScript 5.9.3、TanStack Router 1.163.2（程式碼定義路由，不用檔案路由）、TanStack Query 5.99.0、Vite 7.3.1、Tailwind 4.2.1、`@base-ui/react` 1.3.0、`lucide-react` 1.6.0、`sonner` 2.0.7、Biome 2.4.8、Vitest 4.1.2、Playwright 1.59.1、pnpm 9.15.9（`web/package.json` 的 `packageManager`）；沒列在 P4 定案的套件不加。元件與設計 token 共用 `@evergenie/ui`；前端放 `web/`（pnpm workspace：`apps/board`、`apps/admin`、`packages/ui`、`packages/sdk-web`），建置輸出 `internal/base/app/board/dist/`（看板）進 git（2026-11-01 從 `internal/web/dist/` 搬來；admin 的畫面已改由 `admin` 套件自己提供）。
- **FEN-2** — 核心內嵌 esbuild（Go）編譯套件的 `.tsx`，使用者不用裝 Node；React 與 `@evergenie/ui` 由外殼提供，套件不准自己帶。
- **FEN-3** — 選單的可用項目與使用者的樹由伺服器一次算好（`WebAPI.GetMenu`），前端拿到才開始畫；沒拿到之前顯示載入畫面，不畫預設選單再修正。例外：選單等了 3 秒還沒到或讀失敗，先用基本選單把外殼畫出來（WEB-175，作者 2026-11-14「SPA 不應該因為一個功能卡住就不能進去」）。
- **FEN-12** — 選單的呈現元件可以被使用者的 `.tsx` 取代（`shell.menu`，約定見 `web` 模組的說明卡）：外殼用 error boundary 包住它，失敗就退回預設元件；`?menu=default` 強制預設。
- **FEN-4** — 每個畫面都有網址（Router 的路由）；上一頁、重新整理、書籤回到同一個畫面。狀態放網址或伺服器，不放只存在於記憶體的 UI 狀態。
- **FEN-5** — 需要持久的使用者偏好存成檔案（`settings/*.toml`，經 API），不存 `localStorage`（換裝置就沒了、AI 也看不到）；`localStorage` 只放「最後選的分頁」「秘書篩選的選擇（`eg.agentFilter.v2`）」「桌機討論串旁邊是角色欄還是清單（`eg.chatPane.v1`）」這類無關緊要的方便。鍵一律 `eg.` 開頭（套件頁面也是）；改名前存的 `hg.` 開頭的鍵由 `src/lib/storageRename.ts` 在開看板時搬一次（新鍵已有值以新的為準，DEV-158）。
- **FEN-6** — 資料用 HTTP＋JSON 的 TypeScript client（型別從 `sdk/` 產生），不手寫 fetch；串流用 `Watch`／`Chat` 並處理重連。
- **FEN-7** — 手機優先：最小寬度 320（WCAG 1.4.10；DEV-156 U-25 從 360 降下來，e2e `TestNoHorizontalScrollAt320`；新家的歡迎頁在名冊空的家另外驗：`welcome.spec.ts` 的 `TestWelcomeFitsAt320WithOneH1`）、桌機 1280 兩個版面都要用得了；沒有水平捲動；可點的目標至少 44px——視覺上要小的（膠囊、文字鈕）用看不見的熱區補到 44：`apps/board/src/lib/hit.ts` 的 `HIT_44`，`Segmented` 非 compact 一律高 44（DEV-156 U-11）；亮色與深色都用 token，不寫死顏色；字的對比至少 4.5:1（不用半透明把字變淡；`lib/tokens.spec.ts` 守）。
- **FEN-8** — 「要你同意」的訊息，選項按鈕在落地畫面的第一屏就可見（REQ-138）；回答後畫面立刻更新成「已回」。聊天裡的卡片比一屏長時例外，見 UXC-1。
- **FEN-9** — 頁面可以留 slot（`<Slot id="…"/>`）給別的套件掛東西；slot 空了頁面照常運作。
- **FEN-10** — 給人看的字串走 i18n 資源檔（預設繁體中文）；識別碼（`SEC-12`）旁邊要有白話說明（使用者規則）。
- **FEN-11** — 每個畫面改動在合併前過 UX review（下面的 UXC-n，D-13）。
- **FEN-13** — 先看再改：詳細頁是閱讀版面，不是表單；點哪個欄位就地編輯（點文字變輸入框，Enter 或失焦存、Esc 還原），存完欄位旁顯示「已儲存」1.5 秒。內容是 Markdown 這類裡面有連結、圖的，`InlineEdit` 給 `editLabel`：內容不包在按鈕裡，改按標籤旁的編輯鈕（例：待辦的「改描述」）。待辦與任務單另有「編輯」鈕開整張表單一次改（WEB-161），兩者並存。
- **FEN-14** — 狀態改變是動作，不是下拉：依目前狀態只顯示能做的動作按鈕（任務單交付等驗收：「驗收」「退回」，放在詳情頁最上面的交付卡裡，board WEB-179；待辦：「做了」「還在做」「延後」「取消」），主要動作一個、用實心按鈕。
- **FEN-15** — 清單頂端一行輸入就能新增（待辦打「明天 9 點 打給牙醫」按 Enter 就建立）；解析出的時間、負責秘書用輸入框下方的 chip 顯示，可點 chip 改；其他欄位收在「更多選項」，不展開也能送出。時間欄位 `WhenField` 收自然語言，下方即時顯示解析結果，旁邊有三個快捷 chip 與「用日曆選」，留空交給伺服器套預設時間（REQ-62）並寫一行小字說明；解析器在 `apps/board/src/lib/when.ts`，用瀏覽器的時區。
- **FEN-16** — 漸進揭露：只顯示跟目前選擇有關的欄位（例：跟進方式選「秘書幫我確認」才出現「要確認什麼」；選「到時間找我聊」才出現「想聊什麼」）。
- **FEN-17** — 選項 ≤ 5 個用分段按鈕或 chip（`Segmented`），秘書用頭像選擇器（`AgentPicker`），清單資料用 chip 輸入（`ChipsInput`，Enter 或逗號成一顆，退格刪最後一顆）；選項多於 5 個才用 `Select`。
- **FEN-18** — 每個欄位有可見的標籤；說明寫在欄位下方一行小字；placeholder 只放範例。
- **FEN-19** — 必填與格式在失焦後就檢查，錯誤寫在欄位正下方；伺服器回的 `sdk.Error.field` 對到同名欄位就顯示在那個欄位下，對不到才用 toast；送出時捲到並聚焦第一個錯誤欄位。前端只檢查必填、長度、格式，業務規則交給伺服器（FEN-N3）。
- **FEN-20** — 送出中按鈕顯示轉圈並停用，不會重複送；成功後清單立刻更新（樂觀更新），失敗就還原並顯示原因。
- **FEN-21** — 可逆的動作（待辦的「做了」「取消」）先在畫面上生效，toast 帶「復原」（5 秒），5 秒內沒按才真的呼叫 API（離開畫面或 `pagehide` 時立刻送出）；按復原就不送、畫面還原。不可逆或會通知別人的動作（退回任務單要寫理由、拒絕權限）在畫面內展開一個小確認區。
- **FEN-22** — 設定頁沒有「儲存」按鈕：改了 600ms 後自動存，頁首顯示「已儲存／儲存中／沒存成功（重試）」；伺服器拒絕就還原那個欄位並把原因寫在欄位下。例外：帳號表單，與「誰可以連進這台」（`owner_login`、`allow_boards`，WEB-80）用明確的送出鈕——改誰能連進來不該存打到一半的值。
- **FEN-23** — 聊天輸入框與新增表單的未送內容存 `localStorage`（key 帶畫面網址，FEN-5 允許的方便類資料），回來還在，送出成功就清掉；有未存的就地編輯時離開畫面，畫面內提示「還沒存，要放棄嗎」。
- **FEN-24** — 手機上新增與編輯用底部抽屜（`Sheet`；右上角一定有關閉鈕，手機的搜尋面板也是，不能只靠點外面或往下拉，DEV-156 U-27），動作列固定在抽屜底部、鍵盤彈出時跟著上移；欄位設對 `inputmode`、`enterkeyhint`、`autocomplete`；桌機 `⌘/Ctrl+Enter` 送出、`Esc` 關閉、開啟時聚焦第一個欄位、關閉後焦點回到觸發的按鈕。
- **FEN-25** — 手機頁首固定的元件（標題列、篩選列等）一律放在外殼（`Shell`）、高度寫死；頁面裡不准畫會隨切頁重掛的頁首，否則每次換分頁都會閃一下、位置差幾個 px。要不要畫由路由的 `staticData` 旗標決定（例：`agentFilter`）。圖片頭像要記住載入過的網址，重掛不從透明淡入。（2026-10-25 作者用 iPhone 回報切換分頁畫面閃；REQ-155） 篩選列往下捲時收起、往上捲或回到頂端（24px 內）再出現，換頁一律先出現（`shell/hideOnScroll.ts`，2026-11-02 稽核：手機一個畫面少了快兩成）；標題列不收。e2e `TestMobileFilterBarHidesOnScrollDown`、vitest `hideOnScroll.spec`。
- **FEN-26** — 頁面的查詢放在自己的頁面資料夾（2026-10-31，法律第 8 條，照業務分資料夾）：只有一個頁面資料夾用的查詢、動作與純函式（`useQuery` 的 `queryOptions`、`jobs.ts`、`goals.ts`、`chatPane.ts`…）寫在 `pages/<頁面>/queries.ts`（或該頁面自己的檔）；兩個以上的頁面資料夾、`shell/`、`components/` 都要用的（`menuQuery`、`threadsQuery`、`taskQuery`、`liveKeys`、`retryUnlessMissing`…）才留在 `data/queries.ts`，`data/` 只放這種共用的東西（client 之外的通用 hook、key、store）。頁面之間不互相 import 對方的 `queries.ts`：別的頁面也開始要用時，把那個查詢移到 `data/queries.ts`。搬檔只改 import，畫面與行為不變。
- **FEN-27** — 頁面資料夾之間不互相 import（2026-11-01，法律第 8 條，一個業務一個資料夾、業務之間不互相依賴，跟 Go 端的業務模組同一個原則）：`pages/<A>/` 底下的檔（含測試）不 import `pages/<B>/`。兩個以上頁面要用的東西搬出來：元件放 `components/`、純函式放 `lib/`、查詢放 `data/`（FEN-26）；搬檔只改 import，測試跟著搬，畫面與行為不變。共用層 `components/`、`lib/`、`data/`（含測試）也不 import `pages/`（共用的不能反過來依賴某個頁面；測試要驗某頁的東西就寫在那頁資料夾的 spec）。只有掛頁面的 `router.tsx`、外殼 `shell/`、套件頁的宿主 `pkg/` 可以 import 頁面（例：`pkg/PackagePage` 用聊天頁的 `pages/chat/PageChat`）。自動檢查 `scripts/web-page-imports.mjs`（看相對路徑的 `import`／`export … from`／`import()`／`vi.mock()` 解析後落在哪個資料夾），`make web-ci`、`make check`、`make web-ci-full` 都跑，違規就列出檔案與行號並擋下。
- **FEN-28** — 看板的多台（2026-11-02，計畫 `2026-11-01-board-multi-home` S3-5，S3-6～S3-9 補）：一個看板直接呼叫各台的 API，每台的資料分開快取、網址補對那台。共用的工具在 `apps/board/src/data/sites.ts`：
  - 核心頁取資料：查詢用 `data/queries.ts` 的 `xxxQuery(…, site)`（不給＝主伺服器），自己呼叫 API 用 `useSiteApi(key)` 或 `apiFor(site)`，不直接 import `api` 去打別台；找不到那台時不可以退回主伺服器（會寫到錯的家）。
  - query key 帶台名：一律經 `siteKey(site, …)` 產生（別台是 `["site", 台名, …]`；主伺服器不加前綴，跟 S3 之前一樣，所以現有的 `invalidateQueries({ queryKey: ["menu"] })` 只動主伺服器）。
  - 伺服器給的相對網址（頭像 `/avatars/…`、附件 `/share/…`）顯示前經 `siteHref(site, rel)`；主伺服器維持相對網址。上傳用 `upload(file, site.base)`。
  - 台清單 `useSites()`：別台來自主伺服器的 `web.get_sites`（讀不到＝只有主伺服器）；主伺服器一定在第一個（key `""`、base `""`，不用等 get_mode）；別台的 key 是它的台名（斷線時用這個分頁上次讀到的台名；從沒連上過才是網址），`state` 分 `ok`／`old`（`board_api` 比主伺服器自己的小，單一正本是主伺服器的 get_mode）／`denied`（到得了但被拒：伺服器回 403，或正式／開發不同，原因在 `reason`）／`down`（到不了、伺服器出錯）／`checking`（還在試連，或主伺服器自己的 get_mode 還沒讀到），只有 `ok` 的才抓資料；不是 `ok` 的每 30 秒自己再試。
  - 寫入別台前一定看 `state === "ok"`：`useSiteApi(key)` 只對 ok 的台回 api，其他回 undefined；自己拿 `apiFor(site)` 的要自己檢查（`data/sites.ts` 的 `siteUsable`：主伺服器或 ok 的別台）。動作做完重抓用 `refreshAll(client, site)`／`invalidateLive(client, site)`（`data/live.ts`），只動那台的 key。
  - 某台不能用時的提示一律用 `components/SiteNotice.tsx`：`<SiteNotice site={site} />` 放在那台的那一段（能用的台什麼都不畫，可以無條件放）、清單上方用 `SiteProblems`；只要文字用 `siteNoticeText(site)`，台名用 `components/SiteTag.tsx` 的 `siteName`。不另寫一套「連不上／被拒／太舊」的字，也不用串流閒置去猜連不上（只看真的請求失敗與 get_mode）。
  - 秘書跨台用 `agentKey(台, id)`（主伺服器就是職位 id，別台是「台名/職位 id」），篩選、計數、React key 都用它；別台的名冊用 `useSiteAgents()`（只有能用的台）；要列出不能用的台（寫 `SiteNotice`）時，台清單另外用 `useSites()`（秘書篩選的 `data/filterSites.ts`：兩台同一個職位合成一個頭像、不能用的台只放一格小標記「有一台連不上」——整句話在頁面內文講一次，同一句提示一頁只出現一次、不放會被切掉的長字，DEV-156 U-23；聊天換秘書的 `useChatAgents().sites`：照電腦分段）；只拿到一個 agentKey 要知道她在哪台、那台能不能用，用 `useAgentSite(key)`，不能用時寫 `SiteNotice`、不退回主伺服器。
  - 即時、在場、推播（S3-8）：每台能用的台（`useOkSites`）各開一組即時串流、各報一次在場（只有正在看的那台拿真的路徑），提醒記著台（`SiteNudge.site`），回答只送那台；推播同一個訂閱寫進每台、金鑰不同的台不寫。
  - 網址：聊天與詳情頁（`/chat/$session`、`/tasks/$id`、`/reminders/$id`、`/goals/$id`、`/messages/$id`）用 `?home=<台名>` 指出那筆在哪一台，沒寫＝主伺服器；讀用 `useHomeParam()`／`useHomeSite()`（找不到時 `pending` 分「還在試連」與「真的沒有這台」），連過去用 `homeSearch(site)`。**只有一台時畫面與網址跟 S3 之前完全一樣。**
  - 合併好幾台的清單（S3-7b）：`data/acrossSites.ts` 的 `useAcrossSites(site => xxxQuery(…, site))`（主伺服器在第一個、別台只抓 ok 的）＋`flattenParts` 接成一份、每筆記得自己的台；跨台的 React key、延後動作、「已回」標記用 `itemKey(site, id)`（`data/waiting.ts` 那一個，兩台的編號可能一樣）；別台的詳情連結先問 `data/siteLinks.ts` 的 `canLinkDetail`。每筆標台名用 `components/SiteTag.tsx` 的 `ItemSiteTag`（只有一台時不畫；裡面是 S3-6 的 `SiteTag`，顏色 `siteColor`：設定的 `color` 優先，沒設照 machine 給 `--site-n`），別台用不了（或那台資料讀不到）時清單上方 `components/SiteNotice.tsx` 的 `SiteProblems`（`failed={failedSites(parts)}`）只說那台（每台一行 `SiteNotice`，字一套 `site.notice.*`）；詳情頁用 `components/SiteGate.tsx` 的 `SiteGate` 照 `?home=` 找台、把 site 往下傳給所有元件與動作。新增東西用 `components/SitePick.tsx` 的 `useSiteTarget`（秘書用 agentKey，建在秘書那台；沒選秘書＝主伺服器）＋`SiteAgentPicker`＋`SitePickChip`（畫面上看得出建在哪台）；秘書那台現在用不了（試連中、連不上、被拒）時目標還是那台、`ready` 是 false，送出要擋下，不退回主伺服器。這些在只有一台時都不畫。別台的版本可能比較新、回看板不認得的狀態：字用 `lib/knownText.ts` 照原字顯示，`Pill` 對不認得的 tone 畫灰（muted）。
- **FEN-29** — 上一頁（2026-11-04，DEV-122，作者：「很多地方都加上」「不能造成 loop」）：除了今天頁，每個畫面都有一顆，只有一個元件 `components/BackLink.tsx`：手機在頁首左邊（討論串在自己的標題欄），桌機在內容區頂端一條。有站內歷史就退，跳過跟現在同一個網址的筆（`lib/backTrail.ts`，否則刪除後回清單會多一下按了沒反應）；退到頭（通知、書籤、新分頁進來）才往上一層（`lib/backTarget.ts`），用 `replace`。所以按到底一定停在今天、不繞圈，也不多疊歷史；例外是還沒有秘書的新家，設定精靈就是最上層、不畫上一頁（今天頁會轉回精靈）。討論串當掉時外殼補一條。為了守住這點：① 頁面（含套件頁）不自己畫「回到…」的返回連結；② 自動跳頁、看完就失效的頁面（找不到的提示、刪除後、轉址）一律 `replace`；③ 內容區的一般 `<a href>` 由外殼改走前端路由（`shell/internalLinks.ts`），不整頁重載，否則站內歷史歸零；④ 套件自己 `history.replaceState` 要帶 `history.state`，不能傳 `null`（會洗掉歷史位置）。套件頁每筆歷史重掛一次，換查詢等於以前的整頁重載。vitest `backTarget.spec`、`internalLinks.spec`，e2e `back.spec`。
- **FEN-30** — 看大圖（2026-11-14，DEV-157，作者：「圖案關閉應該是大家都要有」；前身是 2026-11-03 的 WEB-132）：看板與套件頁面要讓人把圖片或影片放大看，**一律用 `@evergenie/ui` 的 `MediaViewer`**（`web/packages/ui/src/components/MediaViewer.tsx`；一張縮圖配一張大圖的用包好的 `ImageViewer`），**不可以用新分頁開圖片或影片（`target="_blank"`、`window.open`）**：加到主畫面的看板沒有分頁列，開了就退不回來。站外連結、下載用的檔案連結除外（照舊 `<a target="_blank" rel="noopener noreferrer">`）。元件的行為：在這頁疊一層（`data-testid="media-viewer"`）；右上角明顯的 ✕（44px）；點背景、Esc 可關；**開著時手機的上一頁／返回手勢是關掉大圖、不是離開頁面**（開的時候多疊一筆同網址的歷史，帶著原本的 `history.state`、路由的鍵不變所以套件頁不重掛，FEN-29；用 ✕／背景／Esc 關的事後把那一筆退掉，歷史不多一筆）；關掉後回到同一頁同一個位置；多於一項可以左右換（兩側按鈕、鍵盤左右鍵、「第幾張 / 共幾張」；**沒有做手指左右滑**）；影片用 `<video controls>` 播。用法：`const [at, setAt] = useState<number | null>(null)`，縮圖 `onClick={() => setAt(i)}`，旁邊放 `<MediaViewer items={[{ src, name }]} index={at} onIndex={setAt} />`（`type` 不寫看副檔名）。守門：`scripts/web-media-newtab.mjs`（`make check`、`make web-ci`、`make web-ci-full` 都跑）掃看板、`@evergenie/ui` 與官方套件的頁面，`window.open`、`<a target="_blank">` 包著 `<img>`／`<video>` 或網址是圖片影片（副檔名、`/pkg-files/`、`/avatars/`）就紅；真的有理由的例外在同一行或上一行寫 `hg-allow-new-tab: <原因>`。主人自己的套件（`packages/mine/`）沒有自動檢查（`evergenie packages` 沒有檢查頁面寫法的地方），靠 `user-features.md`「頁面怎麼寫」第 7 點；要查可以手動跑 `node scripts/web-media-newtab.mjs <套件資料夾>`。已知的邊角：角色頁「介紹」有還沒存的編輯時關大圖會先問要不要放棄（現在那個分頁沒有看大圖）。vitest `MediaViewer.spec.tsx`、`Markdown.spec.tsx`；e2e `closet.spec.ts` → `TestOutfitImageOpensViewerAndClosesInPlace`（手機大小）、`TestShareImageAndFile`、`nutrition.spec.ts`。
- **FEN-31** — 每頁只有一個第一層標題（h1；WCAG 1.3.1，DEV-156 U-31，e2e `TestOneH1PerPage`；歡迎頁四步 `TestWelcomeFitsAt320WithOneH1`）：手機的頁首（`shell/MobileHeader.tsx`）本來就是 h1；內文在手機也會畫出自己的標題時（詳情頁的那一筆、找不到的說明）用 `components/PageTitle.tsx` 的 `<PageTitle>`（畫出來是 h1），頁首會自動退成一般文字。只有桌機才畫的標題（桌機沒有頁首）照舊寫 `<h1>`；一頁其他段落用 h2 以下。套件頁面的第一層標題是外殼給的頁名，套件自己的內容從 h2 開始。頁內的底線分頁（`PageTabs`）不折行，放不下時那一排自己橫向捲、頁面不捲。
- **FEN-32** — 頭像一律用 `@evergenie/ui` 的 `Avatar`（2026-11-15，DEV-157 新家第一天走查③⑦；`web/packages/ui/src/components/Avatar.tsx`）：沒有圖時圓裡寫名字的字（`initialsOf`：中日韓名取前兩個字，同一個字開頭的角色也分得出來；英文名兩個字以上取各字首字母；直徑小於 32px 一律一個字）；圖載入成功後字就不畫（透明背景的圖不會透出字）；同一個網址載入失敗後 5 分鐘內重新掛上不再抓（不一直 404 洗主控台，過了再試一次）。**伺服器確定沒有圖時就不給網址**（名冊、角色庫的 `avatar_url`，agents AGS-6、board WEB-150），前端不要自己拼 `/avatars/…` 去試。vitest `Avatar.spec.tsx`。
UX 檢查（任何畫面改動合併前必過，D-13、REQ-115）：截手機寬（390）與桌機寬（1280）、亮色與深色共四張，逐條對照，產生附圖的報告；**作者每糾正一次就多加一條**（附日期與來源）。
- **UXC-1** — 「要你同意」的訊息，選項按鈕在落地畫面（點通知進來的第一屏）可見、可點，不需要捲動或再點一層。（2026-10-23 作者回報手機沒看到按鈕；REQ-138）例外：聊天裡的等你回答卡片跟著訊息一起滑（WEB-157，作者 2026-11-04「他不應該置頂」），點通知進來時定位到卡片開頭，放得下就整張可見；內容比訊息區長時從題目看起，選項往下滑一次才到。
- **UXC-2** — 畫面上出現的編號（`SEC-120`、`R-53`、`H-3`、Jira 票號、PR 號）旁邊都有白話說明，不能只放代號。（作者 2026-10-05）
- **UXC-3** — 每個畫面有自己的網址，重新整理與上一頁回到同一個畫面。除了今天頁，每個畫面都有「上一頁」（FEN-29）。（REQ-107）
- **UXC-4** — 選單載入前不顯示選單內容；載入後不再跳動。（REQ-105）例外：選單等了 3 秒還沒到或讀失敗，外殼先用基本選單畫出來、標明是基本選單，讀到了換回來（WEB-175；作者 2026-11-14「SPA 不應該因為一個功能卡住就不能進去」）。
- **UXC-5** — 手機寬度沒有水平捲動；可點的目標至少 44px；主要動作放在拇指容易碰到的位置。
- **UXC-6** — 亮色與深色都清楚：文字對比足夠、沒有寫死的顏色、圖片在深色下不刺眼。
- **UXC-7** — 空狀態要告訴使用者「現在沒有東西、下一步可以做什麼」，不是空白。找不到、讀不到這類不是好消息的不用綠色打勾：`EmptyState` 給 `tone="missing"`（灰色問號）；找不到某一筆用 `NotFoundBlock`（一定帶「回到…」的按鈕）；資料讀不到時不要同時畫空狀態（會以為資料不見了）（DEV-156 U-06、U-18、U-28）。
- **UXC-8** — 錯誤訊息是白話（說發生什麼、怎麼辦），不出現堆疊、內部路徑或錯誤碼。（REQ-116）套件的卡片與頁面讀不到資料、套件頁的程式載不進來，都用 `@evergenie/ui` 的 `LoadFailed`（「{什麼}暫時載不進來」＋「再試一次」，跟核心頁同一種說法）；英文原文只寫 console，不上畫面；今天頁同一個 slot 多張卡失敗由 `LoadFailedGroup` 合成一條（DEV-156 U-06）。內部代號（職位 id、單號、指令）不上一般畫面：職位代號只在完整模式顯示（`data/queries.ts` 的 `useShowCodes`，DEV-156 U-07）；清單列標題前的編號（單號、待辦與通知的編號）也是（`components/RowCode.tsx` 的 `RowCode`，DEV-156 U-22：今天頁的等你處理與桌機清單、任務單頁、訊息頁的通知列、聊天角色欄與她首頁的任務、工作對話頂端的任務列；待辦清單那一行的編號；聊天清單的工作對話列沒有標題時才在簡單模式寫單號）。詳情頁與搜尋照舊顯示編號——在那裡編號是用來對的。vitest `RowCode.spec.tsx` → `TestRowCodeHiddenInSimpleMode` 守簡單模式不露、完整模式才露；`MessagesPage.spec.tsx` 用標題認列；`TaskBanner.spec.tsx`、`ThreadList.spec.tsx` 照完整模式測。
- **UXC-9** — 使用者按了按鈕之後，畫面立刻有回饋（已回／處理中），不讓人不知道有沒有按成功。（REQ-139）
- **UXC-10** — 報告對照時，每一條檢查標「過／不過／不適用」，不過的附截圖與修改建議。
- **UXC-11** — 每個討論串的狀態在清單與標題欄一眼看得出來：等你處理有橘點、寫「要你同意」或「要你回答」（清單與標題欄同一個字，DEV-156 U-10）（原本寫黃點，2026-10-30 起照 UXC-12）、處理中有進度（工程師模式才寫正在做什麼，WEB-160）、完成了沒看有完成標記；手機寬度下黃點與完成標記不被截掉。（2026-10-23 作者要求；REQ-142、REQ-143）
- **UXC-12** — 狀態（清單、標題欄、秘書卡、訊息頁、徽章）只用五種顏色，查 `web/apps/board/src/lib/statusTone.ts` 的對照表，不在畫面裡自己挑：橘＝等你處理（要你同意、要你回答、要你驗收三種，名詞表；**狀態裡只有這個用橘色**；過期的待辦、離線、排隊、叫醒中、快取快過期都不是橘）、藍＝處理中、紅＝出錯（送不了、這一輪沒做完）（「卡住」「掉包」已廢（S5，2026-11-02）：核心不再判，看板的那兩種紅字 S5-4 拿掉；看板遇到不認得的狀態畫灰）、綠＝完成與有新回覆、灰＝待命／停了／離線／額度用完（灰色一定配文字）。亮色與深色都要分得出橘與紅。（2026-10-30 作者「需要他回覆的用橘色點點」；REQ-382、REQ-383）

## NEVER
- **FEN-N1** — 不先畫完整選單再依權限收掉項目（會閃現不該看到的項目）。選單讀不到時的基本選單只放不看權限的核心頁（WEB-175），不違反這條。
- **FEN-N2** — 不含 admin 的程式（D-12）；2026-10-31 起 `/admin/` 網站與 `apps/admin` 拿掉（計畫 `2026-10-31-dev-workshop-split`），admin 只用套件頁面（`packages/official/admin/pages/`，跟其他套件一樣只 import react、`@evergenie/ui`、`@evergenie/sdk-web`）。
- **FEN-N3** — 不在前端算業務規則（例如待辦的預設時間、通知該不該送）；前端只顯示伺服器給的結果。
- **FEN-N4** — 不依賴舊格式的推播連結（`/?tab=…&room=…`，REQ-108）。
- **FEN-N5** — 不為了套件而在外殼寫套件專屬的程式（外殼只認 manifest 的宣告）。
- **FEN-N6** — 不用瀏覽器的 `confirm`／`alert`／`prompt`；確認寫在畫面內（FEN-21），草稿提示除了 `beforeunload` 也不開瀏覽器對話框（FEN-23）。
- **UXC-N1** — 不用「之後再補」放過檢查不過的項目；要放過，在報告寫明原因並由作者同意。
- **UXC-N2** — 不用同一個畫面在手機與桌機各做一套互相矛盾的流程；差別只在版面。
- **UXC-N3** — 不以截圖好看代替檢查表（報告必須逐條對照）。

## 範例
```tsx
// 選單：拿到才畫（等太久或讀不到時先用基本選單，WEB-175）
const { data: menu } = useQuery(webClient.getMenu, {});
if (!menu) return <Loading />;
return <Nav firstRow={menu.menu.firstRow} more={menu.menu.more} />;
```
UX 報告格式：每個改動一頁——改了什麼、四張截圖、UXC 逐條「過／不過／不適用」、不過項目的修改建議。

## 設計決定
- **D-11** — 跟參考專案同一套技術（React 19、TypeScript、TanStack Router），共用 `@evergenie/ui`。核心內嵌 esbuild 編譯套件的 `.tsx`，使用者不用裝 Node；React 與 ui 由外殼提供，套件不准自己帶。選單項目由套件在 manifest 宣告，伺服器依「已安裝＋已啟用＋有權限」算出可用項目一次給前端，前端拿到才畫（例外：選單等了 3 秒還沒到或讀失敗，先用基本選單把外殼畫出來（WEB-175，作者 2026-11-14「SPA 不應該因為一個功能卡住就不能進去」）；基本選單只有不看權限與套件的核心頁）。使用者自己組最多兩層的選單樹（`settings/menu.toml`），沒放進樹的出現在「未分類」，設定頁永遠可達；呈現可以換成 `packages/mine/` 的選單元件，壞了自動退回預設。頁面可以留 slot 給別的套件掛。
- **D-13** — 畫面改動合併前要過 UX 檢查（UXC-n，做法見 MUST 那段）；作者每糾正一次就多加一條。不另開角色。
- **D-59** — 手機鍵盤的處理（2026-11-03 QA）：viewport **不加** `interactive-widget=resizes-content`，版面照常用 `dvh`（鍵盤蓋上來時瀏覽器自己把有焦點的輸入框捲進畫面）；鍵盤打開時「一定要整個看得到」的東西改看 `visualViewport`——高度上限用 `--hg-visible-h`（`lib/media.ts` 的 `useVisibleHeightVar`，寫 `max-h-[calc(var(--hg-visible-h)*0.N)]`，不寫 `max-h-[Ndvh]`），貼頂的浮層加上 `useVisibleTop`（WEB-128）。理由：iOS Safari 不認 `interactive-widget`，加了只有 Android 會把整個版面縮小、底部選單騎到鍵盤上，兩個平台行為不同、要測兩套；`visualViewport` 兩邊都有。否決：①加 `interactive-widget`（只修到 Android）；②整個外殼高度改跟 `visualViewport`（每次開關鍵盤整頁重排、iOS 還是會捲，跳動更明顯）。

## 已知限制
- 看板的已知限制（iOS 真機、真的推播…）記在 `board` 說明卡的「已知限制」。
- 截圖：`make shots`（TST-9），輸出在 `$EVERGENIE_SHOTS_DIR`（預設 `/tmp/hg-shots`）。

## Handoff
接著讀 `development-workflow.md`（UX 關卡在 PR 的哪個位置）。

## 待作者決定
None.
