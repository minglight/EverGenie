# 開發流程：branch、commit、PR、驗收

Status: DRAFT — 設計 2026-10-23（P0-S8），pending author review.

每次動手前都讀（執行者含 Codex）。計畫與步驟在作者的私人文件庫（不公開），這份講每一步怎麼進 repo。

## MUST
- **DEV-1** — 一步一個 PR（P0 是一步一個 commit）；步驟裡沒列的檔案不動，需要動就停下，寫進進度紀錄等 review。
- **DEV-2** — branch 名稱 `p<階段>-s<步驟>-<簡短英文>`（例 `p1-s3-registry`）；從 `main` 開，不直接推 `main`。
- **DEV-3** — commit 訊息：英文一行摘要（≤72 字元，祈使句），空一行後寫為什麼；一個 commit 做一件事。
- **DEV-4** — PR 描述固定四段：做了什麼、跑過的驗收指令與實際輸出、動到的說明卡與新增或改動的測試（名稱）、沒驗到的部分。
- **DEV-5** — 驗收照步驟列的指令跑；**結束碼或輸出不符就停下**，把指令、輸出、判斷寫進進度紀錄；不自己改需求、不改驗收條件、不「先跳過」。
- **DEV-6** — 文件沒寫到、又不在作者那一層（憲法、宗旨、法律）的決定，做事的人照最佳做法自己定並寫進規則，不等作者（憲法第 1 條，2026-11-01 拿掉架構師）；交回前開一個沒參與實作的 subagent 對照憲法、法律、規則 review，挑出的自己修完。跟 EverGenie 的法律（在作者的私人文件庫，不公開）衝突或要改它的做法一律不做，停下來回報作者。
- **DEV-7** — 每個 PR 由 `code` 對照說明卡與 task 的驗收條件 review；改到 `web`、`@evergenie/ui`、套件頁面的，再過 `admin` 的 UX 關卡（`frontend.md` 的 UXC-n），兩關都過才合併。
- **DEV-8** — CI 全綠才合併：`make ci` 依序執行 gofmt 檢查、build、vet、test（主 module 與 sdk，使用 `-race`，含 API 型別快照）、lint（depcheck、enginecheck、doccheck、tsgen）；測試不再標需求編號，CI 不檢查 Guards（DEV-54）。GitHub Actions 呼叫相同的 `make ci`。
- **DEV-9** — 每完成一步在進度紀錄（計畫指定的位置）追加：步驟編號、commit、跑過的驗收與結果、沒驗到的部分；不改 `plan.md`。
- **DEV-10** — 改程式的同時更新對應的說明卡與 schema；文件與程式不一致算沒做完。
- **DEV-12** — 交件前對一遍「主人的東西與升級」三問（`user-features.md` UFE-8 與同名一節，DEV-154）：①升級時會不會動到主人放在家裡的東西；②改了官方套件的紀錄格式有沒有附 `[[migration]]`、用真的舊資料試過（`make lint` 的格式比對要綠）；③新的資料夾或檔案位置，升級的備份、還原與試跑複本顧不顧得到。有一題答不出來就還沒做完。新加會存角色代號的資料種類（欄位或編號裡有角色代號）時，在 `package.toml` 宣告 `persona_field`／`persona_id`／`persona_prefixed`／`persona_source`（PKG-64），角色改代號時紀錄才會跟著走。
- **DEV-11** — 階段之間要作者點頭（進度紀錄寫「等作者放行 P<n>」並停下）。

## NEVER
- **DEV-N1** — 不建 GitHub repo、不改 repo 的公開設定、不發版、不部署進家、不 force push——除非作者明確同意。`git push` 到公開的 `minglight/EverGenie`（含 `main`）是對外而且收不回來的動作：推之前要 `make ci` 全綠、`scripts/public-check.sh --require-blocklist` 沒有命中（`runbooks/public-release.md`），並且有作者的同意；push 到作者自己的私有 repo 算可退回，不用另外問，但一樣先 `make ci` 全綠（憲法第 8 條）。
- **DEV-N2** — 不碰正式環境：不改正式的家、不動舊系統的 tmux socket `agents`、不重啟常駐秘書、不改帳號的 `settings.json`。
- **DEV-N3** — 不刪除不是自己這一步建立的檔案。
- **DEV-N4** — 不把舊 Python 翻成 Go；只取概念，可以讀舊系統的程式理解行為。
- **DEV-N5** — 不在一個 commit 或一個 PR 裡混不同步驟。

## 範例
```
git switch -c p1-s3-registry
# …做事、跑步驟的驗收指令…
git commit -m "Add module registry with ordered Start/Stop

Registry assembles modules bottom-up per architecture.md (MOD-3)."
```

## 整合測試怎麼跑
`cmd/evergenie/integration_test.go` 與 `conformance_integration_test.go` 帶 `//go:build integration`，會開真的 Claude 和 tmux，**只有 `code` 在本機跑**；執行者（Codex 等）只跑 `go vet -tags integration ./cmd/...`，不執行。

```
EVERGENIE_IT_CONFIG_DIR=~/.claude-acme EVERGENIE_IT_MODEL=haiku \
  go test -tags integration -timeout 40m ./cmd/evergenie -run 'Integration|Conformance' -v
```
- 需要 `EVERGENIE_IT_CONFIG_DIR`（測試用帳號的設定資料夾）與 `EVERGENIE_IT_MODEL`；任一沒設，測試 `t.Skip`。一定要加 `-timeout`：預設 10 分鐘不夠整個流程。
- 測試自己先 `make build`，再以子行程跑 `bin/evergenie serve --home /tmp/hg-it-<亂數>/home`（環境帶 `EVERGENIE_ENGINE_BIN_DIR=<repo>/bin`，輸出寫 `/tmp/hg-it-<亂數>/serve.log`）；家是 `cmd/evergenie/testdata/home` 的複本，`home.toml` 整份重寫（帳號 `main` 用 `engine = "claude"`、`autostart = true`），`code`、`life` 兩位常駐，`ping` job 在起 `serve` 之前就改成啟用。
- 六段（`1-chat-via-dispatcher` … `6-permission-approval`）各自逾時；②③ 允許整項重試一次（模型可能不照做），兩次都失敗才算不過，重試會印在 `-v` 輸出。
- tmux socket 固定 `hg-test`，不碰 `agents`；結束時送 SIGINT 給 `serve`、`tmux -L hg-test kill-server`，並比對帳號 `settings.json` 前後的 SHA-256。
- 失敗時測試家與 `serve.log` 留在 `/tmp/hg-it-*`（輸出會印最後 200 行與路徑）；成功就刪。
- 只用 `/tmp` 下的測試家，不會動到正式的家；第一次進新資料夾的「信任」對話框由測試經 `AnswerBootPrompt` 自動選信任。

## 看板怎麼建、怎麼跑 e2e
前端在 `web/`（pnpm workspace，需要 Node 與 pnpm 9.15.9）；`make ci` 與 `go build` 不需要 Node，因為建好的 `internal/base/app/board/dist/` 進 git（D-11）。
```
make web      # pnpm install --frozen-lockfile && vite build → internal/base/app/board/dist/
make web-ci   # make web，再只測改到的（biome 整個 web/（含 e2e/）、typecheck、vitest --changed；直接在 main 上 commit 時以 origin/main 為基準），最後 git diff --exit-code -- internal/base/app/board/dist
make web-ci-full  # 同上但 biome、typecheck、vitest 全跑；發版前才跑（TST-12）
make e2e-install # 第一次下載 e2e 用的瀏覽器（Playwright 的 headless shell），只下載一次
make e2e-smoke  # 例外用（TST-12 不再要求）：每個 spec 一個 @smoke 代表測試，桌機＋有手機專屬內容的 spec 的手機版
make e2e SPEC="agent-filter shell"   # 只跑指定的 spec 檔（空白分隔檔名片段；不含截圖）；SPEC 空＝全部 spec；大改某一頁時跑那一個 spec 一次，交回時寫明為什麼
make shots      # 只跑 @shots，輸出 $EVERGENIE_SHOTS_DIR（要看畫面時）
make e2e-full   # 全部 spec＋截圖；發版前才跑（release.md 第 3 步）
```
以上 e2e 目標都先檢查瀏覽器（沒下載就印白話說明叫人跑 `make e2e-install`），再 `e2e-bin`（`make web`、`make build`、假引擎執行檔），再 Playwright（用它內建的輕量無頭 Chromium＝headless shell，不用完整版 Google Chrome；T3）。
- 改了 `web/` 的程式就要跑 `make web` 並把 `internal/base/app/board/dist/` 一起 commit；`make web-ci` 在 dist 沒跟著重建時失敗。
- 改了 `sdk/` 的型別或 `sdk/routes.go`：`go run ./internal/tools/tsgen .` 重新產生 `web/packages/sdk-web/src/gen/`，一起 commit；`make lint` 的 `tsgen -check` 在兩邊不一致時失敗。`gen/` 底下不准手改。
- 前端套件版本固定（不用 `^` `~`），加套件要先有計畫許可（FEN-1）；`pnpm-lock.yaml` 進 git。
- e2e 用的 port、暫存家與截圖位置見計畫 P4 共通定案 14；e2e 不碰 `127.0.0.1:8850`（舊看板）。
- `make test` 裡 `internal/tools/` 不開 `-race`（純原始碼掃描，開了慢 30 倍、抓不到東西）；其他套件照開。新寫「掃整個 repo」的測試：先用 `strings.Contains` 篩掉不可能命中的檔案和行，再跑正規表示式，並且一個測試執行檔只掃一次。
- e2e 的細節（S3、T1）：`e2e/fixtures.ts` 的 worker-scoped fixture `hgServer` 為每個 Playwright worker 把 `cmd/evergenie/testdata/home` 複製到 `/tmp/hg-e2e-<亂數>/home`（`port` 找空的、`engine = "fake"`、`autostart` 用字串取代），用 `<repo>/bin/evergenie serve` 起來，等 `get_health` 與兩位常駐都 `idle`；worker 結束時送 SIGINT、刪暫存家（`EVERGENIE_E2E_KEEP=1` 保留，`serve.log` 在裡面）；`baseURL` 由它提供，沒有 globalSetup／globalTeardown。`workers` 最多 2（T3），`fullyParallel: false`（檔案之間平行、同一檔內照順序）；每個 worker 一個自己的家，所以數字類的檢查不會被別的 worker 干擾，但同一個 worker 裡前面的檔案造的資料還在，測試仍要用獨一無二的資料。造資料用核心的 unix socket（`seed`）。手機 project（`mobile`）只跑 `playwright.config.ts` 的 `mobileSpecs`（有 `isMobile` 分支或手機專屬測試的 spec；`make lint` 的 `scripts/e2e-mobile-specs-check.sh` 檢查沒漏），其餘 spec 只跑桌機（`desktop`）；新 spec 有手機專屬內容就要加進清單。截圖（`@shots`，project `mobile-shots`／`desktop-shots`）有自己的 worker 與乾淨的家，存到 `$EVERGENIE_SHOTS_DIR`（預設 `/tmp/hg-shots`）。等 5 秒「復原」的測試用 `page.clock` 快轉（TST-13）。兩個家（S3 看板連多台）的 spec 用 `twoHomesTest`：worker fixture `twoHomes` 起兩個家，各自前面一個 https proxy（`e2e/serve-proxy.ts`，模擬 tailscale serve）；只有用它的 spec 才起，細節見 TST-14。
- `make e2e` 先 `make build` 才 `make web`，而 `bin/evergenie` 把 `internal/base/app/board/dist` 內嵌在裡面：dist 沒跟著原始碼重建時，e2e 跑的是舊畫面。改了 `web/` 之後先 `make web` 再 `make e2e`（dist 已經進 git、`make web-ci` 把關時不會發生）。
- `e2e/` 不在 `tsc` 的檢查範圍（需要 `@types/node`，不在 FEN-1 的套件清單），只有 Biome 與 Playwright 自己的轉譯。

## 測試省時間、省資源（T2，2026-10-27）
- **一次只跑一個重型指令**：`make ci`、`check`、`test`、`web*`、`e2e*`、`shots` 先拿全機共用的鎖（`scripts/heavy-lock.sh`，macOS `shlock`）；別人在跑就每 5 秒印在等誰，持有者死掉自動接手，最多等 `EVERGENIE_HEAVY_WAIT` 秒（預設 1800）；`EVERGENIE_HEAVY_LOCK=0` 只給排查用。
- **並行數上限** `EVERGENIE_CPU`（預設核心數的一半、至少 2）管 go `-p`、pnpm、Playwright workers；vitest 最多 4；`GOMAXPROCS` 預設 4（`EVERGENIE_GOPROCS`）。
- **沒改就不重做**：`make web` 比看板輸入指紋（`scripts/web-hash.mjs`、`dist/.source-hash`），一樣就跳過、`FORCE=1` 強制，打包先進暫存再 `rsync --checksum`（不重寫內容一樣的檔，免得 go test 快取失效）；`make web-ci-full` 的 biome、typecheck、vitest 各記通過紀錄（`scripts/web-stamp.sh`）；typecheck 增量；`make build` 用 `scripts/go-build-bin.sh`（內容一樣不覆蓋）。各自的驗收腳本 `scripts/*-test.sh`。
- **`make check`**（日常）：gofmt、全部編譯、vet 與 go test 只跑受影響的套件（`scripts/go-affected.py`；算不出來就全跑靠快取；`internal/tools/` 的掃描測試每次都跑）、lint、有改才打包、前端只測改到的（`EVERGENIE_BASE` 預設 `main`）。`make ci` 仍是完整關卡。
- 量測：`scripts/ci-bench.sh`。

## 舊系統搬家工具
舊系統的搬家工具（`cmd/` 底下的搬家指令、`scripts/legacy-data-import.py`、`scripts/legacy-rollback.sh`）2026-11-02 已從 repo 刪掉；還有家要從舊系統搬時，用刪掉前最後一版的 tag（不公開）編出當時的工具，照那個版本這份文件的「怎麼在家的複本上試跑遷移」在複本上跑，搬進新家後再用最新版 `evergenie migrate-home --apply` 升級。

## 已知限制
- remote 是 GitHub `minglight/EverGenie`（D-17，公開）；push 規則見 DEV-N1。

## Handoff
接著讀 `module-pattern.md`（開模組）與 `testing.md`（寫測試）。

## 待作者決定
None.
