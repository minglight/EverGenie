# 測試

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

寫任何測試之前讀。2026-10-30 起不再有「需求」層（DEV-54）：測試不標 REQ，CI 不檢查 Guards；舊測試的 `// Guards: REQ-n` 是歷史編號（舊需求表不在公開版）。

## MUST
- **TST-1** — 測試不標需求編號：名稱寫清楚守的行為（TST-8），名字說不完的在函式正上方用一行註解寫「它守什麼、為什麼」；要有哪些測試由 task 的驗收條件寫。舊測試上的 `// Guards: REQ-n` 留著，不批次刪、不再檢查，新測試也不用寫。
- **TST-2** — 測試只用暫存的家：`homefixture.New(t)` 在 `t.TempDir()` 建一個最小的家（`home.toml`、一位秘書、空的 vault）；需要資料時用 `homefixture.WithTasks(...)` 等建構器，或載入 `testdata/home/` 的 fixture。
- **TST-3** — 時間用 `platform.FakeClock`（可凍結、快轉）；測試裡不用 `time.Sleep` 等真實時間，不用 `time.Now()`。
- **TST-4** — 預期檔比對（golden file）：輸出是文字或檔案的（`agent.Instructions`、遷移結果、通知文字），用 `testdata/*.golden` 加 `-update` 旗標更新；更新預期檔的 PR 要在描述裡說為什麼改。
- **TST-5** — 引擎用 fake（`engine/fake`；引擎套件內部的終端機控制也用自己的 fake）；單元測試不啟動真的 AI session。需要真的 session 的測試加 build tag `integration`，只在 P3 之後的專用 job 跑，用 fixture 家。
- **TST-6** — 引擎事件、對話記錄、確認畫面的 fixture 來自真機錄下的樣本（事件序列、transcript、畫面，例如 `packages/official/engine-claude/testdata/spike/`），不憑想像造。
- **TST-7** — 依賴方向 lint、引擎名稱 grep、`doctor` 檢查本身也是測試，照 TST-8 寫名字。
- **TST-8** — 一個測試只驗一件事、名字說明行為（`TestSendIdempotent`，不是 `TestSend2`）；失敗訊息要能一眼看出哪裡不對。
- **TST-9** — 截圖（`make shots`）固定跑四種畫面：手機寬與桌機寬、亮色與深色，附在 UX review（`frontend.md` 的 UXC-n）；其他 e2e 預設只跑桌機寬，手機寬只跑有手機專屬內容的 spec（TST-12）。
- **TST-11** — 引擎介面有一套一致性測試（`internal/base/engine/conformance`）：fake 引擎與 `engine-claude` 都要通過同一組案例；新增或改動介面方法時，同一個 PR 補案例。
- **TST-10** — 不再開 REQ：要做的事寫在 task（goal → task 把行為講清楚），驗收條件列出要有哪些測試，測試跟程式一起進同一個 PR。

- **TST-12** — 測試分層，e2e 只在發版前跑（作者 2026-10-27：「E2E 除非有重要／大量改動，不然不需要每次都跑」；2026-10-28：「web-ci 跟測試都超級吃我的記憶體太誇張了，尤其是瀏覽器不能重複使用嗎？為什麼一直開開關關的……e2e 是不是以後只需要準備 release 做就好？」，同意下面的規範）：
  1. 改程式中：`make check`（日常用，只測有改的部分，見 development-workflow.md）。
  2. 合進 main 前：`make ci`；動到 `web/` 或 `sdk/` 再加 `make web-ci`（只測改到的前端程式，加 dist 跟 git 一致）。**不跑任何 e2e，包括 `make e2e-smoke`。**
  3. 例外：大改某一頁的流程時，可以只跑那一個 spec 一次（`make e2e SPEC="agent-filter"`，空白分隔檔名片段）；交回時要寫明為什麼跑。`make e2e-smoke` 目標保留給這種例外用，規範不再要求它。
  5. 要看畫面：`make shots`（只跑標 `@shots` 的截圖測試，輸出到 `$EVERGENIE_SHOTS_DIR`，預設 `/tmp/hg-shots`）。
  執行者與 `code` 合併前的驗收都照上面判斷。
  e2e 的資源（T3）：瀏覽器是 Playwright 內建的輕量無頭 Chromium（headless shell，`make e2e-install` 下載一次），不用完整版 Google Chrome；workers 上班時間最多 2、下班時間（平日 18:30～08:00、週末）3，`EVERGENIE_E2E_WORKERS` 可指定（作者 2026-10-28）；不關 GPU 行程（作者 10-28：「不用」）；手機 project 只跑 `playwright.config.ts` 的 `mobileSpecs`（有手機專屬行為或版面斷言的 spec，規則寫在那裡的註解，`make lint` 的 `scripts/e2e-mobile-specs-check.sh` 檢查有 `isMobile` 的 spec 沒漏列），其餘 spec 只跑桌機。新 spec 裡寫了 `isMobile` 分支或手機專屬測試，就要加進 `mobileSpecs`。
  所有重型指令（`make ci`／`check`／`test`／`web`／`web-ci*`／`e2e*`／`shots`）都經過全機共用的鎖（`scripts/heavy-lock.sh`）：同一時間整台機器只跑一個，別人在跑就印等待訊息排隊；並行數上限 `EVERGENIE_CPU`（預設核心數的一半）。
- **TST-13** — e2e 的寫法要讓它能平行、能快轉：每個 Playwright worker 有自己的暫存家與服務（`e2e/fixtures.ts` 的 worker-scoped fixture `hgServer`），所以檔案之間平行、同一檔內照順序；每個測試用自己獨一無二的資料（標題、內容帶亂數），不依賴別的檔案造的資料。要等 5 秒「復原」之類的計時，用 `page.clock.install()`（開頁之前裝）＋`page.clock.runFor(ms)` 快轉，斷言照舊；不准把等待改短來通過。要整檔獨占才寫得出來的，用 `test.describe.configure({ mode: "serial" })` 並寫原因。標 `@smoke`／`@shots` 一律寫在測試標題後面（`test("TestX @smoke", …)`）。同一個 worker 的測試共用一個家，所以「全家共用的上限」會被前面的測試吃掉：要開工作 session 的測試（每台同時 doing 上限 `work_max`，預設 3）開頭與收尾都呼叫 `seed.closeAllWork()`（連排隊中的一起收；只關開著的，排隊的會在下一輪對帳被開起來又佔名額），不要自己寫關閉迴圈。
- **TST-14** — 兩個家的 e2e（S3 看板連多台，2026-11-02）：要兩個家的 spec 用 `e2e/fixtures.ts` 的 `twoHomesTest`（`import { twoHomesTest as test } from "./fixtures"`），只有這種 spec 起第二個家；它的 worker fixture 跟 `test` 不同，Playwright 給它自己的 worker：其他 spec 每個的速度不變，整輪跑時最多多一次 worker 啟動（排在一般 spec 中間時，回到一般 spec 要再起一次 `hgServer`）。兩個家各自前面一個模擬 tailscale serve 的 **https** proxy（`e2e/serve-proxy.ts`，自簽憑證啟動時用 `openssl` 產生在暫存資料夾、`ignoreHTTPSErrors`）：刪掉用戶端帶的 `Tailscale-User-*`、補 `Tailscale-User-Login`（預設 `TEST_OWNER_LOGIN`）、加 `X-Forwarded-*`、Host 原樣轉（照 tailscale 原始碼推的，S3-12 真機補驗）；不同 port＝不同 origin。產品程式不為測試放行 http（PLT-22 只收 https）。設定：A 的 `web.public_url`＝A 的 proxy、`[[web.sites]]`＝B；B 的 `public_url`＝B 的 proxy、`allow_boards`＝A、`owner_login`＝`TEST_OWNER_LOGIN`；`[web]` 的鍵接在 `[web]` 標頭後、`[[web.sites]]` 與 `[runtime] owner = "amy"`、`machine` 是 `e2e-a`／`e2e-b` 接在檔尾（label `amy-e2e-a`／`amy-e2e-b`）。`baseURL` 是 A 的 https 網址、`seed` 造 A 的資料；B 用 `homes.b.seed`。測試可以改 `homes.<a|b>.proxy.login`（`null`＝不補，模擬 tag 身分；或別人的 login）、`proxy.mode`（`"bad-gateway"`＝serve 在但後端掛了回 502；`"reset"`＝整台連不上，連線直接斷；切成這兩種時已經開著的連線——串流、即時更新——也一起斷）、看 `proxy.forwarded`（轉給後端的方法、路徑含查詢字串、標頭；看板自己也會打 API，要挑出測試自己的請求就在網址帶獨一無二的查詢字串，後端只看路徑）；每個測試開始時 `homes`（auto fixture）把兩個 proxy 還原成預設。這類 spec 只跑桌機（不列進 `mobileSpecs`）。

## NEVER
- **TST-N1** — 不碰正式的家（這台真的程式與狀態資料夾 `~/.evergenie`、正式的家）、不動 tmux socket `agents`（舊系統）、不改任何帳號的 `settings.json`；`homefixture` 在偵測到路徑落在 `~/.evergenie` 底下時直接 `t.Fatal`。
- **TST-N6** — 測試起新家不寫進這台電腦真的 AI 帳號設定（2026-11-15 DEV-157，新家第一天走查⑩）：`evergenie init` 會把「現在登入的帳號資料夾」（環境的 `CLAUDE_CONFIG_DIR`，沒有就是 `~/.claude`，ENG-19）寫進 `home.toml`，從 AI 的 session 跑測試時那就是真的設定。起新家的測試工具一律明確給假的：e2e（`e2e/fixtures.ts` 寫 `[accounts.main.engine_config] config_dir` 並把 serve 的 `CLAUDE_CONFIG_DIR` 換成暫存區）、`scripts/compat.sh`、`scripts/upgrade-download-test.sh`（`CLEAN` 帶暫存區的 `CLAUDE_CONFIG_DIR`，後者並檢查 home.toml 寫的是它）；模擬乾淨 Mac 的 `scripts/clean-install-test.sh`、`package-go-test.sh`、`install-test.sh` 用 `env -i`＋假 HOME，`~/.claude` 就在假家目錄裡（clean-install-test 檢查 home.toml 寫的是預設登入的 `~/.claude`）。
- **TST-N2** — 不寫有隨機性或時間依賴的測試（flaky）；真的需要並行就用同步點，不用 sleep。
- **TST-N3** — 不為了讓測試過而放寬驗收條件或略過測試（`t.Skip` 要附原因，且 PR 描述要列出）。
- **TST-N4** — 不在測試裡讀秘密或呼叫外部服務；需要網路的部分用 fake。
- **TST-N5** — 不把 spike 的程式當測試工具直接用；要用的邏輯搬進 `internal/…/testing` 並補測試。

## 範例
```go
// 同一個 idempotency_key 重送不會變成兩則訊息
func TestSendIdempotent(t *testing.T) {
    h := homefixture.New(t)
    b := bus.New(bus.Deps{Vault: h.Vault, Clock: h.Clock})
    a, _ := b.Send(ctx, bus.Envelope{From: "owner(web)", To: "code", Body: "x", IdempotencyKey: "k1"})
    c, _ := b.Send(ctx, bus.Envelope{From: "owner(web)", To: "code", Body: "x", IdempotencyKey: "k1"})
    if a.ID != c.ID { t.Fatalf("同一個 idempotency_key 應該回同一個 id：%s ≠ %s", a.ID, c.ID) }
}
```

## 已知限制
- 沒有自動檢查「這個行為有沒有測試」；看覆蓋率報告（`make cover`，只報告不設門檻）找沒被測到的地方，由 review 判斷；e2e 測試案例也沒有這種檢查。
- 還沒有 Playwright 的 CI 環境（P4 建）；P1–P3 只跑 Go 測試。目前 e2e 只在本機跑（Playwright 的 headless shell，要先 `make e2e-install`），worker 數最多 2。

## Handoff
接著讀 `development-workflow.md`（測試與驗收怎麼進 PR）。

## 待作者決定
None.
