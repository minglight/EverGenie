# 模組：secretaries（`packages/official/secretaries`）
Status: 實作 2026-10-28（作者：「setup wizard 應該讓 user 選功能，配合秘書，就可以開始使用了」，REQ-238）。2026-11-14 DEV-157（作者：「可以讓使用者有生圖 art 秘書，怎麼做就使用者跟秘書討論」）加了選用的生圖職位 `art`（SCR-4）；2026-11-15 DEV-160（作者：一般使用者的家沒有生圖秘書，只有開發家有）推翻，`art` 樣板搬到開發工坊 `admin`（admin 卡 ADM-39），SCR-4 已廢。2026-11-15 DEV-161：四個預設職位的職責改成專業背景＋可選知識（SCR-5）；2026-11-16 health、art 也照這個格式（作者 2026-11-16 補談），六份都加 `## 聊到什麼先看什麼`（同日補談第 17–19 條）。

**目的：** 新的家一開始就有職位可選：生活、工作、寫程式、電腦四個職位樣板。
**種類：** 官方套件（`kind = "data"`，只帶四份秘書樣板，沒有資料、頁面、排程）

## MUST
- **SCR-1** — 套件帶四個職位樣板：`life`、`work`、`code`、`system`（`agents/<id>/agent.md`），都不是選用職位；生圖 `art` 不在這裡（一般家沒有生圖秘書，ADM-39）；掃描時不自動建任何秘書（D-28）。主人在歡迎頁（新家第一位，board WEB-176）、設定精靈或「職位與秘書」頁替一個職位選角色，看板送 `package.sync`（`agent` 只動那一個職位、`persona` 是選的角色、`apply`），再把她啟用。CLI：`evergenie packages sync secretaries --agent <職位> --persona <人設> --apply`。
- **SCR-2** — 每份樣板只寫這個職位特有的東西（段落見 SCR-5）；法律第 1 條（所有秘書怎麼做事：叫就做、自己做完、任務單、記憶、先問主人、一律守的界線）寫過的不在樣板重寫（2026-11-01 作者，法律第 1 條開頭）。D-40 的「範圍以外的事」已收進法律 1.1，舊樣板的那一段只留在 `history.txt`。
- **SCR-5** — 職責的固定段落（DEV-161，2026-11-15 作者：職位預先帶著的專業背景，像先告訴 AI 它是哪種專家；誰擔任都一樣，口吻屬角色）：官方的六份職責照這個順序寫：本套件的 `life`、`work`、`system`、`code`，`health` 套件的 `health`（health 卡），開發工坊 `admin` 的生圖 `art`（ADM-39）：`## 她是誰`（專業身分）→ `## 基本知識`（一定帶；分 `### 主題`，每個主題先寫「什麼時候用」再列框架與做法）→ `## 可選知識`（沒有就省略這一段）→ `## 聊到什麼先看什麼`（六份都要有，見下）→ `## 回答時怎麼用` → `## 你主動照顧的事`（提到人或東西先翻主題）→ `## 第一天上班`（用聊的確認可選知識與主人的程度）→ `## 這個職位特有的界線`。可選知識每塊：
  ```
  ### <標題> {#<id>}
  > 預設：勾          （或「> 預設：不勾」）
  > 一句說明（看板勾選畫面顯示這句）
  - 內容…
  ```
  id 小寫英文、數字、減號，英文開頭，同一份職責裡不重複；一塊從它的標題到下一個寫了 `{#id}` 的 `###`、或下一個 `##`／`#`（沒寫 `{#id}` 的 `###` 算上一塊的內容，官方樣板由測試守住）。主人勾了哪幾塊記在家裡（名冊紀錄的 `knowledge`＋看過哪些的 `knowledge_seen`；第一天上班秘書用提問工具問、主人同意後由秘書本人用 `evergenie agents knowledge <自己的職位>` 改，官方職責檔唯讀，法律第 3.1 節）；開場只帶勾了的（標題留、兩行 `>` 去掉，全部沒勾整段拿掉，`agent.md` AGT-41）。勾選入口：「職位與秘書」頁與新家歡迎頁（`agents.knowledge`／`agents.set_knowledge`、派職位的 `package.sync` 加 `knowledge`，`agents.md` AGS-10）、CLI `evergenie agents knowledge`。現在的可選知識：`life` 的 `parenting` 育兒（預設不勾）、`balance` 生活管理與 work/life balance（預設勾）；`work` 的 `leading` 帶人（預設勾）；`system`、`code`、`health`、`art` 沒有（`health` 的運動／飲食／身體三塊知識跟著 `fitness`／`nutrition`／`body` 套件開關走，每個 `###` 的「什麼時候用」寫明哪個套件開著才用）。`system` 刻意不寫安全那一段（作者：交給平台與主人自己決定）。`## 聊到什麼先看什麼`（DEV-161，作者 2026-11-16 補談：聊到某個話題時先載入什麼，每個職位都要）：開頭一句「聽到下面這些時，先看再回答；聊完把新的記回去（那份主題筆記還沒有的，第一次開之前先問主人一句）」，接 3–5 條，每條四件事：聽到什麼 → 先看哪些（主題筆記用 `evergenie notes show`，套件紀錄用那份職責已經寫的資料指令，不另發明指令）→ 回答要帶什麼 → 聊完記回哪裡（主題筆記；數字留在套件紀錄、不抄進筆記）。現在的：`health` 練某個器材或動作、某家店或某種餐、量了體重血壓或拿到健檢報告（血壓寫在 `body.entry` 的 `note`）；`life` 某個人、某個地方、某個固定活動；`work` 某位同事、某張票、某個專案；`system` 某台電腦、某個工具、某個帳號；`code` 某個 repo、某個服務、某個錯誤；`art` 某位角色、某套衣服、某個生圖工具或模型。節尾空一行接固定一句（一字不差，正本是 `internal/base/agent` 的 `TopicCuesClosing`）：主人說「以後聊到 X 先看 Y」時用 `customize add <職位> --section 聊到什麼先看什麼` 加進自己附加規則同名的一段、不改官方檔，兩邊都照做、衝突聽主人加的（主人自己定的寫在家裡，升級不動；`--section` 見 `agents.md` AGS-8；兼任的職位也帶這一節與附加規則同名的一段，`agent.md` AGT-43）。測試 `cmd/evergenie` `TestOfficialDutiesKnowledgeFormat`（六份、每份對到自己的套件；段落順序、每塊解析得出 id、預設、說明；「聊到什麼先看什麼」3–5 條、最後一行是固定的那一句）。
- **SCR-3** — 任務單什麼時候開照法律 1.3（主人要追蹤的、等主人回覆的、等外部的）；樣板不寫開單規則。
- **SCR-4** — 已廢（2026-11-15，DEV-160，作者：一般使用者的家沒有生圖秘書，只有開發家有）：生圖職位 `art` 的樣板搬到開發工坊 `admin`，規則改寫在 admin 卡 ADM-39。選用職位這個通用機制（樣板標頭 `optional: true`）照舊，見 PKG-12、PKG-40、WEB-47 與名詞表「選用職位」。

## NEVER
- **SCR-N1** — 樣板不寫任何真實人名或某個家的私人設定（用「主人」）。

## 職責
- 四個職位的官方職責樣板；官方職責的固定段落（SCR-5，也管 `health`、`art`）與四個預設職位的專業背景、可選知識。

## 不負責
- 建秘書、套職責 → `pkg`（`Sync`）＋ `agent`；看板的認領卡與精靈 → `web`（WEB-47）。

## 介面
`[[contributes.agent]]` × 4（`life`、`work`、`code`、`system`）；`package.list` 的 `agents` 列出它們（id、一句話職責、顏色、`optional`；四個都不是選用）。

## 事件
None.

## 儲存
None（套用後在家的 `agents/<id>/`，由 `agent` 管）。

## 依賴
`sdk`

## 錯誤
- 名冊上已經有同 id 的秘書（例：從舊系統搬來的 `life`）：這個職位算「已經有人負責」，看板不出認領卡；`packages sync` 會顯示職責差異，主人同意才更新。
- 封存過同 id：`packages sync` 回 conflict（換 id 或先還原）。

## 不變條件
- 新的家選一個職位＋一個角色就有一位上班的秘書。

## 測試計畫
- Go：`cmd/evergenie` `TestOfficialDutiesKnowledgeFormat`（SCR-5）；`internal/feature/packages` `TestSyncOneFunctionWithPersona`（清單帶樣板、只同步一個職位、用指定角色）、`TestOfficialPackagesDiscovered`（樣板合格、四個都不是選用、`art` 在 `admin`）、`TestSyncSkipsOptionalUnlessNamed`、`TestClaimNotifySkipsOptional`、`TestClaimNotifyOnlyOptional`、`TestOptionalTemplateHeader`（選用職位不自動帶出來，通用機制）、`TestSyncOfficialDutiesStayInProgram`（已經有的職位同步不動）；一般家沒有 `art`、開發家有、已經有 `art` 的家同步後職責不變見 admin 卡 ADM-39 的測試。
- e2e：`settings-tabs.spec.ts` `TestClaimFunctionFromTemplate`（認領卡選角色 → 名冊多一位啟用的秘書）；vitest `functions.spec.ts`、`ClaimFunctions.spec.tsx`（選用職位的標記、精靈不列、設定頁另一組）。

## 已知限制
- 樣板是通用的起點；名冊上已經有這些職位的家不會出認領卡，職責要不要換成樣板，由 `packages sync` 預演後主人決定。
