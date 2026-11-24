# 把第二台加進看板

Status: DRAFT — 2026-11-02（S3-11），pending author review.

一個看板（瀏覽器）直接連另一台的家，同一個畫面看到兩台的秘書與資料（計畫在作者的私人文件庫，不公開；法律第 7 條（v1.7 起）：看板可以直接連各家，家與家之間不連線、不同步）。**伺服器之間不會互相連線**，兩台的資料、記憶、排程照舊各自分開；只是瀏覽器多打一台的 API。

下面用「A＝你平常開的看板（例 mini 的 `https://genie-<owner>-mini.example.ts.net`）、B＝要加進來的那台（例 office 的 `https://genie-<owner>-office.example.ts.net`）」。設定欄位的意思見 `schemas/home.md` 的 `[web]`；跨站的規則見 `modules/board.md` WEB-78、WEB-79、WEB-80；畫面規則見 FEN-28。

## 0. 先確認
- 兩台都是 S3 之後的版本（`system.get_mode` 有 `board_api`；比 A 舊的台畫面上寫「版本比這個看板舊」，S3 之前的版本只會寫「沒有允許這個看板，或版本太舊」）。部署照 `release.md`。
- 兩台是同一種家：**正式看板只看正式家、開發看板只看開發家**（`[runtime] env`；法律第 1.1 條的例外「開發與日常分開」、計畫設計決定 3）。不一樣的會被當成被拒（「這是開發家，正式看板不看」），允許了也沒用。開發家互看：dev-mini 的看板加 dev-office。
- 兩台的看板都是固定的 Tailscale 服務網址（D-43，`[web] service`、`public_url`；`evergenie tailscale setup` 會寫 `public_url`）。用手機直接開一次 B 的網址，開得到才往下做（開不到是 tailnet ACL 或服務沒掛上，EverGenie 不碰 ACL）。
- 用來看的裝置要是**用主人的帳號登入 tailnet 的**（手機、筆電）。跑 EverGenie 的 Mac 是 tag 身分，Tailscale 不會補使用者標頭，在 mini 本機開 A 的看板連 B 預設被拒（「這台只接受主人的裝置」）——這是設計，不是壞了。**例外**：在 B 那台**自己**的瀏覽器開 A 的看板連回 B（例如在 office 開 mini 的看板連 office），B 的 `home.toml` `[web]` 加 `trust_self = true`（或 `evergenie config set web.trust_self true`）就能過（WEB-130；要先有 `owner_login` 與 `allow_boards`）。在別台 tag 機器上開仍然被拒。

## 1. B：允許 A 的看板連進來
在 **B 自己的看板**做（這兩個設定別台的看板改不了，WEB-79），或由主人在 B 的終端機跑（家要指對，`EVERGENIE_HOME`）。**秘書做不了**：秘書身分會 `FORBIDDEN`，不要拿掉 `EVERGENIE_AGENT` 冒充主人去繞，也不要叫主人開終端機——請主人在 B 的看板按。
主人的 tailnet 登入＝Tailscale App 上登入的那個帳號（GitHub 登入寫成 `name@github`，`schemas/home.md`）。

| 要設的 | 看板（B 的「設定 → 電腦 → 誰可以連進這台」） | 指令（在 B） |
|---|---|---|
| 主人的 tailnet 登入 `owner_login` | 「主人的 tailnet 登入」填好按「儲存」 | `evergenie web owner set <你的 tailnet 登入>` |
| A 的看板網址 `allow_boards` | 「允許的看板」加 A 的網址按「允許」（要填瀏覽器開 A 用的那個 origin：B 被拒時 A 的「設定 → 電腦」那列會寫「到那台的看板，把這個看板（<origin>）加進…」，照那個抄；手機的 PWA 也要用同一個網址開） | `evergenie web allow add https://genie-<owner>-mini.example.ts.net` |

- 網址寫看板的 origin：`https://主機[:port]`，不帶路徑；大寫、結尾斜線、`:443` 會自動整理掉，http、帶路徑、萬用字元會被擋（伺服器講原因）。
- 只設了 `allow_boards` 沒設 `owner_login`：doctor `config.web_cross_site` 黃字，別的看板一律被擋。
- 改了不用重開，最多 30 秒生效（`home.toml` 修改時間快取）。
- 看現在的：`evergenie web allow list`（同時列主人登入）。

## 2. A：把 B 加進這個看板
在 A 的看板或 A 的終端機做（這個設定別台的看板也可以改）。

| 要設的 | 看板（A 的「設定 → 電腦 → 看板連的電腦」） | 指令（在 A） |
|---|---|---|
| `[[web.sites]]` `url`、`color` | 「加一台」填 B 的網址、挑顏色（預設照那台的電腦代號給；紫、藍、青、綠） | `evergenie web sites add https://genie-<owner>-office.example.ts.net [--color #rrggbb]` |

- 台名、環境、版本不用填，一律向 B 的 `system.get_mode` 拿。
- 加完畫面會立刻試連；那一列的狀態膠囊變「正常」就好了。之後有「立刻試連」可以再按。
- 看現在的：`evergenie web sites list`；拿掉：看板「拿掉」（要確認）或 `evergenie web sites rm <網址>`。

## 3. 推播：兩台金鑰要一樣
一支手機只在 A 的看板（PWA）訂閱一次，同一個訂閱寫進每一台；兩台各自用自己的秘密檔送，所以**兩台秘密檔的 `[webpush]` `vapid_public_key`、`vapid_private_key` 要一樣**（`vapid_subject` 一起抄也沒關係）（`schemas/notify-prefs.md`、`modules/inbox.md`「多台」）。
- 金鑰一樣之後，在手機上開 A 的看板「設定 → 裝置推播」：推播開著時每次進這頁都會替別台補寫訂閱；「其他電腦的推播」每台一列寫狀態。
- 「推播金鑰不一樣，收不到它的推播」＝兩台的公開金鑰不同（看板比 `notify.push_key`），不會替那台寫。只改了 B 的金鑰：手機再進一次這頁就會補寫；改了 A 的金鑰：瀏覽器的訂閱綁在 A 的公開金鑰上，手機要把推播關掉再開一次。
- 點 B 送的推播會開到 `…?home=<B 的台名>`。

## 4. 檢查
- A 的「設定 → 電腦」B 那一列「正常」，有版本；底下看得到 B 的主人登入與允許清單（只顯示，旁邊「到那台的看板改」）。
- 聊天清單多一段 B（台名用那台的顏色），對 B 的秘書送一句、她回得到。
- 今天、訊息、任務、待辦、Goal、權限申請混著列兩台，每筆標台名；點 B 的那筆網址帶 `?home=<B>`，重新整理還在。
- 在 B 改一張單，A 的清單 3 秒內跟上。
- B 那台關機或斷網時，只有 B 那一段寫「現在連不到」，A 自己的照常；判成連不上最多要等約 40 秒（30 秒一次試連＋8 秒逾時）。只停掉 B 的 EverGenie、Tailscale 服務還在時，畫面寫的是「沒有允許這個看板，或版本太舊」（502 被判成被拒，見已知限制）。

## 5. 常見錯誤（畫面上的字 → 原因 → 怎麼修）
| 看到 | 原因 | 修法 |
|---|---|---|
| 「B 不讓這個看板連：這台還沒設定主人的 tailnet 登入（web.owner_login）…」 | B 沒設 `owner_login` | 步驟 1 第一列 |
| 「B 不讓這個看板連：這台只接受主人的裝置」 | 用來看的裝置不是主人的帳號（tag 身分的 Mac、別人的手機），或 `owner_login` 寫錯 | 換用主人登入的手機／筆電；在 B `evergenie web allow list` 對一下登入；要在 B 那台自己的瀏覽器看，B 開 `web.trust_self`（WEB-130） |
| 「B 沒有允許這個看板，或版本太舊。」（沒有原因） | A 的網址不在 B 的 `allow_boards`（B 回不帶 CORS 的 403，瀏覽器讀不到原因）；或 B 是 S3 之前的版本；或 B 的 Tailscale 服務在、EverGenie 沒在跑（tailscaled 回 502，現在也會被判成被拒，見 board.md「已知限制」的「多台」） | 步驟 1 第二列；網址照 A 設定頁那句提示裡的 origin 填（瀏覽器開 A 用的網址）；更新 B；確認 B 的 serve 在跑 |
| 「這是開發家，正式看板不看」／「這是正式家，開發看板不看」 | 兩台 env 不同 | 拿掉這一列；開發家請開發看板去看 |
| 「B 的版本比這個看板舊（x），先更新那台…」 | B 的 `board_api` 比 A 小 | 部署 B |
| 「B 現在連不到，等它開機或網路恢復會自己再試。」 | 網路到不了 B（關機、斷網、ACL、服務沒掛上） | 手機直接開 B 的網址看看；每 30 秒自動再試 |
| 「這個設定只能在那台自己的看板改」 | 從別台的看板直接打 API 改 B 的允許清單或主人登入（A 的設定頁對 B 只顯示、沒有改的鈕，一般碰不到） | 到 B 自己的看板或 B 的終端機改 |
| 看板「設定 → 電腦」B 那列一直不見 | 重複的列、或寫成 A 自己的網址會被略過，畫面看不到 | `evergenie web sites list` 找出來 `rm` |

## 6. 退回
- A：看板「設定 → 電腦」拿掉，或 `evergenie web sites rm <B 的網址>`。只有在看板拿掉時會清掉**這個瀏覽器**秘書篩選裡 B 的選擇（從沒連上過的台清不到）；用 CLI 拿掉不清，篩選裡留著「B/秘書」，要是只選了 B 的秘書，清單可能整個被篩空——到秘書篩選列取消那幾位（見 `board.md`「已知限制」的「多台」）。
- B：`evergenie web allow rm <A 的網址>`；不再讓任何看板連進來就 `evergenie web owner clear`。
- 三個指令與看板的寫入都會留 `home.toml` 備份（`platform.SetHomeTable`）。
