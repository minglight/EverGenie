---
name: request-permission
description: 被權限擋住（平台的自動判斷拒絕、每次都要問、EverGenie 的 hook 說不行）而且之後還會再遇到時，向主人送一張權限變更申請（evergenie policy request），由主人在看板或通知上核准、核心套用。不自己改任何設定，也不叫主人去終端機。只是這一次要放行就直接問主人，不用送申請。
---

# 送權限變更申請

被擋住時，你**不能**自己改權限（改帳號設定、改產生的設定檔、改 `trust.toml` 都不行，平台也會擋），也**不能**叫主人去終端機跑指令（他多半用手機）。正確的路是送一張申請，主人審，核心套用（D-26）。

## 什麼時候用
- 同一類動作被擋，而且之後還會再遇到（例如每次改某個 repo 都被問）。
- 只是這一次要放行：直接在對話裡問主人，不用送申請。
- 對外發訊息、花錢、改設定這類「一律問你」的動作不能開成固定規則；真的常被誤擋才用 `guidance` 說明。

## 步驟
1. **確認真的被擋了**：看擋下時的訊息；需要的話用 `evergenie trust denied` 看核心記下的被擋紀錄，`evergenie trust show` 看目前的權限。不是被擋（例如指令本身錯了）就不要送。
2. **選種類**（預設 `rule`）：
   - `rule`：一條精準規則。動作只能是 `read`、`write`、`commit`、`push`、`delete`；範圍寫到最窄的資料夾（絕對路徑或 `~/` 開頭，不能用萬用字元）。
   - `guidance`：一句話，告訴平台的自動判斷「這類操作是主人要的」。會影響同一個帳號的**所有**秘書，只在一條規則寫不完、同類被誤擋兩次以上時用。
3. **先看有沒有重複**：`evergenie policy list --status pending` 與 `evergenie policy list --status applied`，已經有了就不要再送。
4. **寫清楚為什麼**：`--why` 必填，寫被擋的情境與次數；`--risk` 寫你看到的風險（沒有就寫「只限這個資料夾」之類）。
5. **送出**：
   ```
   evergenie policy request --kind rule --action write --scope ~/Workspace/<登入名>/<repo> --why "改 <repo> 的檔案被擋了 2 次（<什麼情境>）" --risk "只限這個 repo"
   evergenie policy request --kind guidance --text "在 ~/Workspace 底下跑 make ci 是主人要的" --why "make ci 被誤擋 3 次" --risk "只是跑測試"
   ```
   回來的 `P-<n>` 就是申請編號；範圍太寬會標 `broad`，主人會看到紅字警語。
6. **告訴主人**：一句話說你送了哪一張（附白話：誰、要做什麼、哪裡、為什麼），他會在看板「權限申請」頁或通知上按核准或拒絕。不用給他任何指令。
7. **等結果**：核心會把結果送回給你。核准的 `rule` 只在**新開（或重開）的對話**生效；還沒生效前，這一次的動作照樣先問主人。可以用 `evergenie policy show P-<n>` 看狀態。

## 不要做
- 不自己呼叫 `evergenie policy decide` 或 `evergenie policy revoke`（只有主人能用，你呼叫會被拒絕）。
- 不改任何設定檔、不叫主人去終端機、不為了繞過而換一種寫法做同一件事。
- 不一次送一堆：每位秘書同時最多 10 張待審。
