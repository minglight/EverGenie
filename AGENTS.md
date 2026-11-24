# 給 AI 的入口（Claude、Codex 共用；`CLAUDE.md` 指到這份）

1. 先讀 [`ai_docs/INDEX.md`](ai_docs/INDEX.md)，依你的任務只讀它指到的那幾份，不要全讀。名詞（職位、角色、套件、衣櫃、展示間、兌換…）以 [`ai_docs/conventions/terms.md`](ai_docs/conventions/terms.md) 為準。會碰到家裡資料的改動（官方套件的紀錄格式、遷移、升級與部署流程、`vault/` 的資料夾）先讀 [`ai_docs/conventions/user-features.md`](ai_docs/conventions/user-features.md) 的「主人的東西與升級」：8 個原則與交件前三問（DEV-154）。
2. 不能違反：憲法、EverGenie 的宗旨與法律（在作者的私人文件庫，不公開、不在這個 repo）、規則檔裡的設計決定 D-n（在負責那件事的規範或說明卡的「設計決定」段，`git grep -n '\*\*D-<n>\*\*' ai_docs` 找得到）。覺得哪條錯了，照上層改下層（規則以下由做事的 AI 改）；要改憲法、宗旨或法律，停下來回報作者。
   **條號與單號**：文件裡的「憲法第 n 條」「法律第 n 條」是作者自己的上層規則（不公開），這裡只當出處；要遵守的內容都已經寫在這個 repo 的規則裡（常被引用的幾條各一句話，見 `ai_docs/INDEX.md`「上層規則與單號」）。文件裡的 DEV-n、REQ-n、SEC-n 這類是作者追蹤系統的單號，「計畫 `<日期>-<名字>`」是作者自己的工作計畫（不公開），都只當出處（開發流程規範自己的 `DEV-` 規則例外，見 INDEX）。
3. 照計畫執行時：只動步驟列出的檔案；驗收沒過就停；文件沒寫到、又不是憲法／宗旨／法律層級的決定，照最佳做法自己定、寫進規則，不等作者（憲法第 1 條）；交回前開一個沒參與實作的 subagent 對照憲法、法律、規則 review。
   **進度紀錄**：每一步完成後，在計畫指定的進度紀錄尾端追加：步驟編號、commit、跑過的驗收與結果、沒驗到的部分、待決。進度紀錄是每一步都可以動的例外；計畫本身（`plan.md`）不改。
4. 測試不標需求編號（2026-10-30 起廢掉 REQ 層；舊需求表與 spike 結果不在公開版）：測試名稱寫清楚它守的行為，必要時正上方一行註解說明；task 的驗收條件寫要有哪些測試。舊測試上的 `// Guards: REQ-n` 是歷史編號，留著，不用刪也不用補。
5. 不碰正式環境：不改正式的家與舊系統的資料夾、不動 tmux socket `agents`、不改帳號的 `settings.json`。
6. 這個 repo 是公開的：`git push` 到 `minglight/EverGenie`（含 `main`）是對外而且收不回來的動作，推之前要 `make ci` 全綠、`scripts/public-check.sh --require-blocklist` 沒有命中（`ai_docs/runbooks/public-release.md`），並且有作者的同意（憲法第 8 條）；push 到作者自己的私有 repo 算可退回，不用另外問。建 GitHub repo、改公開設定、發版、部署、force push 照舊要作者明確同意。
