.PHONY: build vet test lint ci check cover web web-ci web-ci-full e2e-install e2e-bin e2e e2e-smoke shots e2e-full deploy-test release-check release-test install-test workshop-test compat release-e2e \
	public-check public-check-test \
	dist dist-test upgrade-download-test clean-install-test package-go-test \
	_ci _check _cover _test _web _web-ci _web-ci-full _e2e-bin _e2e-browser _e2e _e2e-smoke _shots _e2e-full _compat _release-e2e _dist _dist-test _upgrade-download-test _install-test _clean-install-test _package-go-test

# 核心的版本字串（P7-7、REQ-277）：scripts/version.sh（最近的 vX.Y.Z tag 去掉 v；tag 之後有 N 個 commit 就是 X.Y.Z-N-g<短碼>；還沒有 tag 就是短碼），deploy.sh 也叫它。`system.doctor` 的「核心版本」讀它（admin 套件探測開發家也讀這一條）；沒有 git 就留空（核心退回 Go 內嵌的版本資訊或 dev）。
VERSION := $(shell scripts/version.sh 2>/dev/null)

# ---- 全機一次只跑一個重型測試＋限制並行數（T2 第 1 條）-------------------------------------------
# 重型目標（ci、check、test、web、web-ci、web-ci-full、e2e-bin、e2e、e2e-smoke、shots、e2e-full）都經過 scripts/heavy-lock.sh：
# 所有 worktree、所有 session 共用同一把鎖（~/.cache/evergenie/heavy.lock），別人跑著就排隊、每 5 秒印一行等待訊息。
# 巢狀的 make 不重拿（EVERGENIE_HEAVY_HELD=1）；EVERGENIE_HEAVY_LOCK=0 可以關鎖（只給特殊情況）；EVERGENIE_HEAVY_WAIT 是最久等多少秒（預設 1800）。
# 每個重型目標的真正內容在同名加底線的 `_目標`，公開目標只是「拿鎖再跑它」。
# EVERGENIE_CPU：並行數上限，預設核心數的一半（至少 2）；go test／build／vet 的 -p、vitest 的 --maxWorkers、
# pnpm -r 的 --workspace-concurrency、Playwright 的 workers（取它跟原本設定的較小者）都吃它。
EVERGENIE_CPU ?= $(shell n=$$(sysctl -n hw.ncpu 2>/dev/null || nproc 2>/dev/null || echo 4); c=$$((n / 2)); if [ $$c -lt 2 ]; then c=2; fi; echo $$c)
export EVERGENIE_CPU

# go 工具本身預設開「核心數」個執行緒（這台 14 個），載入套件、算快取鍵時這些執行緒大量空轉搶 sys 時間：
# 實測一次什麼都不用編的 `go vet`，GOMAXPROCS=14 約 4.8 CPU 秒、=4 約 1.7 CPU 秒（牆鐘一樣快）。
# 測試程式繼承同一個值（CI 的機器本來就只有 2–4 核）。要改自己設 GOMAXPROCS 或 EVERGENIE_GOPROCS；
# 環境裡已經有 GOMAXPROCS 就用它。
EVERGENIE_GOPROCS ?= 4
export GOMAXPROCS ?= $(EVERGENIE_GOPROCS)

LOCK := scripts/heavy-lock.sh
# 例：$(call locked,ci) → 拿鎖後執行 `make _ci`
define locked
	@$(LOCK) $(1) -- $(MAKE) --no-print-directory _$(1)
endef

# 「改了什麼」的比較基準（make check 的只測有改的部分用）
EVERGENIE_BASE ?= main
export EVERGENIE_BASE

# ---- Go ---------------------------------------------------------------------------------------
# bin/ 的執行檔只在內容變了才覆蓋（scripts/go-build-bin.sh），不然 go test 的快取會被 mtime 打掉。
build:
	go build -p $(EVERGENIE_CPU) ./...
	cd sdk && go build -p $(EVERGENIE_CPU) ./...
	scripts/go-build-bin.sh . bin/evergenie ./cmd/evergenie -ldflags "-X github.com/minglight/evergenie/internal/base/platform.BuildVersion=$(VERSION)"
	scripts/go-build-bin.sh packages/official/engine-claude bin/evergenie-engine-claude ./cmd/evergenie-engine-claude
	scripts/go-build-bin.sh packages/official/habits bin/evergenie-habits ./cmd/evergenie-habits
	scripts/go-build-bin.sh packages/official/closet bin/evergenie-closet ./cmd/evergenie-closet
	scripts/go-build-bin.sh packages/official/closet-mine bin/evergenie-closet-mine ./cmd/evergenie-closet-mine
	scripts/go-build-bin.sh packages/official/admin bin/evergenie-admin ./cmd/evergenie-admin

vet:
	go vet -p $(EVERGENIE_CPU) ./...
	cd sdk && go vet -p $(EVERGENIE_CPU) ./...
	cd packages/official/engine-claude && go vet -p $(EVERGENIE_CPU) ./...
	cd packages/official/habits && go vet -p $(EVERGENIE_CPU) ./...
	cd packages/official/closet && go vet -p $(EVERGENIE_CPU) ./...
	cd packages/official/closet-mine && go vet -p $(EVERGENIE_CPU) ./...
	cd packages/official/admin && go vet -p $(EVERGENIE_CPU) ./...

# internal/tools/ 是單執行緒的原始碼掃描（depcheck、enginecheck、doccheck…），-race 抓不到東西、只會慢 30 倍，所以不開。
test:
	$(call locked,test)
_test:
	go test -p $(EVERGENIE_CPU) -race $$(go list ./... | grep -v /internal/tools/)
	go test -p $(EVERGENIE_CPU) ./internal/tools/...
	cd sdk && go test -p $(EVERGENIE_CPU) -race ./...
	cd packages/official/engine-claude && go test -p $(EVERGENIE_CPU) -race ./...
	cd packages/official/habits && go test -p $(EVERGENIE_CPU) -race ./...
	cd packages/official/closet && go test -p $(EVERGENIE_CPU) -race ./...
	cd packages/official/closet-mine && go test -p $(EVERGENIE_CPU) -race ./...
	cd packages/official/admin && go test -p $(EVERGENIE_CPU) -race ./...

lint:
	go run ./internal/tools/depcheck .
	go run ./internal/tools/enginecheck .
	go run ./internal/tools/doccheck .
	go run ./internal/tools/tsgen -check .
	scripts/e2e-mobile-specs-check.sh
	scripts/compat.sh --only schema

# 合 main 前的完整關卡：不管改了什麼，build、vet、go test（含 -race）、lint 全跑（靠 go 的快取省時間）。
ci:
	$(call locked,ci)
_ci:
	test -z "$$(gofmt -l cmd internal sdk packages)"
	$(MAKE) --no-print-directory build
	$(MAKE) --no-print-directory vet
	$(MAKE) --no-print-directory _test
	$(MAKE) --no-print-directory lint

# 覆蓋率報告（DEV-54）：只報告、不設門檻、不進 make ci（整輪 -cover 比整個 make ci 還慢）。
# 看哪些套件測得少，給 review 參考；要看單一套件的哪幾行沒測到：go test -coverprofile=c.out ./internal/<套件> && go tool cover -html=c.out
cover:
	$(call locked,cover)
_cover:
	go test -p $(EVERGENIE_CPU) -cover ./cmd/... ./internal/... | grep 'coverage:' | sed 's|github.com/minglight/evergenie/||' | sort -t: -k2 -n

# ---- 前端 -------------------------------------------------------------------------------------
# make web：看板算輸入指紋（scripts/web-hash.mjs），跟 internal/base/app/board/dist/.source-hash 一樣就跳過打包；
# make web FORCE=1 強制重打包（也會讓 web-ci-full 的 biome／typecheck／vitest 通過紀錄失效重跑）。
web:
	$(call locked,web)
_web:
	scripts/web-deps.sh
	FORCE=$(FORCE) scripts/web-build.sh board

# 合併前的關卡，只測改到的（作者 2026-10-27：「前端 web-ci 應該只測那些有被改到的功能」）：
# 打包（有改才打）、scripts/web-check-changed.sh（頁面資料夾不互相 import〔FEN-27，全掃〕、不用新分頁開圖片影片〔FEN-30，全掃〕、
# 第三方授權聲明 LICENSES/web-third-party.txt 跟相依一致〔scripts/web-licenses.mjs --check〕、biome 整個 web/（含 e2e/，約 1 秒，D-53）、typecheck 整個專案（增量）、
# vitest --changed $(EVERGENIE_BASE)）、dist 跟 git 一致。間接受影響、沒 import 到改動的測試會漏，留給 web-ci-full。
# dist 比對排除 .source-hash：它只是「這份 dist 是用哪組輸入打的」，內容沒變的改動（例如只改註解）
# 會讓它變，但 dist 本身沒變；它過期的代價只是下次多打包一次，不會讓 bundle 錯。
DIST_DIFF = git diff --exit-code -- internal/base/app/board/dist ':(exclude)internal/base/app/board/dist/.source-hash'
web-ci:
	$(call locked,web-ci)
_web-ci: _web
	scripts/web-check-changed.sh
	$(DIST_DIFF)

# 完整版：biome、typecheck、vitest 全跑；發版前才跑（跟 e2e-full 同時，TST-12）。
# 三項各自記「這組輸入通過過」（scripts/web-stamp.sh），輸入沒變就沿用，改了就整項重跑；FORCE=1 全部重跑。
web-ci-full:
	$(call locked,web-ci-full)
_web-ci-full: _web
	node scripts/web-page-imports.mjs
	node scripts/web-media-newtab.mjs
	node scripts/web-licenses.mjs --check
	FORCE=$(FORCE) scripts/web-stamp.sh biome -- sh -c 'cd web && pnpm check:ci'
	FORCE=$(FORCE) scripts/web-stamp.sh typecheck -- sh -c 'cd web && pnpm typecheck'
	FORCE=$(FORCE) scripts/web-stamp.sh vitest -- sh -c 'cd web && pnpm test'
	$(DIST_DIFF)

# ---- 日常用的快速檢查（T2 第 5 條）-------------------------------------------------------------
# 迭代時跑 make check；交回或合併前跑一次 make ci（有動 web/、sdk/ 加 make web-ci）。make check 不取代 make ci。
# 前端部分跟 make web-ci 一樣只測改到的；web-ci 多一步 dist 跟 git 一致。
# Go：gofmt、全部編譯（不連結執行檔）、vet 與 go test 只跑「有改的套件跟依賴它的」（scripts/go-affected.py 用 go list -deps -test 算；
#     改到套件以外的檔案或沒把握就全跑，靠快取）、lint；
# 前端：有改才打包、第三方授權聲明跟相依一致、biome 整個 web/、typecheck（增量）、vitest --changed $(EVERGENIE_BASE)。
check:
	$(call locked,check)
_check:
	test -z "$$(gofmt -l cmd internal sdk packages)"
	scripts/go-check.sh
	$(MAKE) --no-print-directory lint
	$(MAKE) --no-print-directory _web
	scripts/web-check-changed.sh

# ---- e2e --------------------------------------------------------------------------------------
# 測試分層（TST-12，ai_docs/conventions/testing.md、development-workflow.md；作者 2026-10-28「e2e 是不是以後只需要準備 release 做就好？」）：
#   改程式中：make check；合進 main 前：make ci（動到 web/、sdk/ 加 make web-ci）。**不跑任何 e2e**（包括 e2e-smoke）。
#   例外：大改某一頁的流程時，只跑那一個 spec 一次（make e2e SPEC="agent-filter"），交回時寫明為什麼跑。
#   發版前（部署到 mini 之前，release.md 第 3／5 步之間）：make e2e-full ＋ make web-ci-full 各跑一次；
#   要看畫面：make shots（只跑 @shots，輸出 $(EVERGENIE_SHOTS_DIR)，預設 /tmp/hg-shots）。
#   make e2e-smoke 保留給上面的例外用（每個 spec 一個 @smoke 代表測試），規範不再要求它。
# 瀏覽器：Playwright 的輕量無頭 Chromium（headless shell），不用完整版 Google Chrome；第一次先 make e2e-install（只下載一次）。
# 手機 project 只跑 playwright.config.ts 的 mobileSpecs（有手機專屬行為或版面斷言的 spec），其餘 spec 只跑桌機；workers 上班時間 2、下班時間與週末 3（EVERGENIE_E2E_WORKERS 可指定）。

# 下載 e2e 用的瀏覽器（只下載一次，存在 ~/Library/Caches/ms-playwright；Playwright 升級後再跑一次）。不碰重型鎖。
e2e-install:
	scripts/web-deps.sh
	cd web/apps/board && pnpm exec playwright install --only-shell chromium

# 找不到瀏覽器就用白話說明叫人跑 make e2e-install（在建置之前檢查，省得白建一輪）。
_e2e-browser:
	@scripts/e2e-browser-check.sh

# 先建 web 再建 Go：bin/evergenie 內嵌 internal/base/app/board/dist，順序反了會測到舊畫面。
e2e-bin:
	$(call locked,e2e-bin)
_e2e-bin: _web
	$(MAKE) --no-print-directory build
	scripts/go-build-bin.sh . bin/evergenie-engine-fake ./internal/base/engine/fake/cmd/evergenie-engine-fake

# 全部 spec（不含截圖）；SPEC 可以指定檔名片段，空白分隔（例：SPEC="agent-filter shell"），空＝全部。
e2e:
	$(call locked,e2e)
_e2e: _e2e-browser _e2e-bin
	cd web/apps/board && pnpm exec playwright test --grep-invert @shots $(SPEC)

# 每個 spec 檔一個最能代表「這頁沒壞」的測試（標 @smoke），桌機一遍、有手機專屬內容的 spec 再加手機一遍。
# 規範（TST-12）不再要求跑它；留給「大改某一頁」的例外。
e2e-smoke:
	$(call locked,e2e-smoke)
_e2e-smoke: _e2e-browser _e2e-bin
	cd web/apps/board && pnpm exec playwright test --grep @smoke --project=mobile --project=desktop

shots:
	$(call locked,shots)
_shots: _e2e-browser _e2e-bin
	cd web/apps/board && pnpm exec playwright test --grep @shots

# 發版前跑一次：全部 spec（桌機＋有手機專屬內容的手機 spec）＋截圖。
e2e-full:
	$(call locked,e2e-full)
_e2e-full: _e2e-browser _e2e-bin
	cd web/apps/board && pnpm exec playwright test

# 部署工具的驗收（發版流程缺口 4）：假的 ssh／evergenie／make／curl＋暫存 repo，不 ssh、不碰真的服務。
# 注意：沒有會真的部署的目標——部署只能人手打 `scripts/deploy.sh <機器>`（release.md 步驟 5、6）。
deploy-test:
	scripts/deploy-test.sh

# 部署前檢查一次跑完（D-53，release.md 步驟 3～5）：web-ci → ci → deploy-test → compat → web-ci-full → e2e-full，
# FULL=1 再加 release-e2e；一項沒過也跑完其他項，最後印白話摘要（含「測的是不是 origin/main」）。本身不拿重型鎖（每一項自己拿）。
# 不部署：過了之後還是人手打 scripts/deploy.sh。只看會跑什麼：scripts/release-check.sh --dry-run。
release-check:
	scripts/release-check.sh $(if $(filter-out 0,$(FULL)),--full)

# 發版工具的驗收（P8 第二輪 S1、REQ-277、REQ-278）：暫存 bare origin＋clone，真的跑 nightly／prepare／stable／status（測試步驟用 true／false），不編譯、不推任何東西。
# 隨 `make ci` 跑（cmd/evergenie 的 TestReleaseScript 會呼叫它）；單獨跑也可以。
release-test:
	scripts/release-test.sh

# 公開前檢查（DEV-158，ai_docs/runbooks/public-release.md）：public-check 掃整個 repo 有沒有私人的字與通用規則的命中
# （私人清單在 repo 外面，預設 ~/.evergenie-release/blocklist.txt；沒有清單時只跑通用規則並提醒）。正式公開前改跑
# scripts/public-check.sh --require-blocklist 或 scripts/export-public.sh。不改任何檔。
public-check:
	scripts/public-check.sh

# public-check.sh 與 export-public.sh 的驗收：暫存資料夾、假的私人清單與假 HOME，不讀真的 ~/.evergenie-release/。
# 隨 `make ci` 跑（cmd/evergenie 的 TestPublicCheckScript 會呼叫它）；單獨跑也可以。
public-check-test:
	scripts/public-check-test.sh

# 安裝程式的驗收（DEV-155 S3、REQ-279）：先測下載版（真的編一包放本機 http、假 HOME、PATH 只有系統內建工具、背景行程的服務；
# 要先有 dist/tmux，沒有就說先跑 scripts/build-tmux.sh），再測從原始碼裝（假的 Makefile／evergenie／go，這段也隨 `make ci` 跑：TestInstallScript）。
# 下載版要真的編一包，經過重型鎖；隨 `make release-e2e` 跑，不隨 `make ci`。
install-test:
	$(call locked,install-test)
_install-test:
	scripts/install-test.sh
	scripts/install-source-test.sh

# 建工坊家與搬家腳本的驗收（GOAL-2 S3、S4）：假的日常家＋假的 evergenie，不編譯、不碰真的家；隨 `make ci` 跑（TestWorkshopScripts）。
workshop-test:
	scripts/workshop-test.sh

# 相容檢查（P8 第二輪 S5、REQ-284、REQ-136）：這一版對「上一個正式版」留下來的東西還認不認得——sdk 型別快照只加不刪、
# fixture 家開得起來且官方套件全部載入、上一版建出來的家遷移得過（細節在 scripts/compat.sh 的檔頭）。沒有正式版 tag 時跳過需要它的兩項並說明。
# 先 make build（要用這一版的 bin/evergenie）；經過重型鎖。**不隨 `make ci` 跑**（要編兩個版本），發版前跑（release.md、每晚測試的 EVERGENIE_RELEASE_STEPS 也有）。
compat:
	$(call locked,compat)
_compat:
	$(MAKE) --no-print-directory build
	scripts/compat.sh

# 發版整套測試（P8 第二輪 S5、REQ-285）：在暫存資料夾用真的 git／make build／serve 把「打版號、一鍵安裝、升級、壞版自動退回、手動退回、
# 套件從網址裝與更新、角色包同步與沒網路、nightly／stable、make compat」走一遍（scripts/release-e2e.sh，不碰 launchd、不推任何東西）。
# 約十幾分鐘（要編很多次）；**不隨 `make ci` 跑**，發版前（部署 mini 之前）與每晚測試跑。EVERGENIE_E2E_KEEP=1 保留暫存資料夾。
# 2026-11-13 起（DEV-155）也跑下載版的三支：dist-test（發行檔編得出來、格式對）、upgrade-download-test（下載版的升級、壞版換回、退回）
# 與 install-test 的下載版那段（安裝程式裝下載版、每種失敗的結束碼），clean-install-test（乾淨的 Mac 從安裝用到升級、移除，DEV-155 S9），
# 最後 package-go-test（下載版真的下載 Go、用附的 sdk 編主人的附程式套件，**要網路**：從 go.dev 下載 Go 一次、proxy.golang.org 一個小模組）。
# dist-test、install-test、clean-install-test 要先有 dist/tmux（scripts/build-tmux.sh）。
release-e2e:
	$(call locked,release-e2e)
_release-e2e:
	scripts/release-e2e.sh
	scripts/dist-test.sh
	scripts/upgrade-download-test.sh
	scripts/install-test.sh
	scripts/clean-install-test.sh
	scripts/package-go-test.sh

# 發行檔（DEV-155 S1）：交叉編 darwin-arm64、darwin-amd64（CGO_ENABLED=0；amd64 補 ad-hoc 簽名）、打包、寫版本清單 manifest.json，
# 放 dist/（scripts/dist.sh；參數用 DIST_ARGS 傳，例：make dist DIST_ARGS="--version 0.3.0 --base-url https://…"）。不碰 bin/、不碰任何家。經過重型鎖。
dist:
	$(call locked,dist)
_dist:
	scripts/dist.sh $(DIST_ARGS)

# dist.sh 的驗收：真的編兩種晶片，對包的內容、清單的檢查碼與大小、解開後 version 印的版本、簽名（scripts/dist-test.sh）。不隨 `make ci` 跑。
dist-test:
	$(call locked,dist-test)
_dist-test:
	scripts/dist-test.sh

# 下載版升級的整套（DEV-155 S2）：用 dist.sh 編三個版本、本機 http 提供、手動裝成 versions/＋current，再升級、檢查碼不符、下載中斷、
# 清單讀不到、沒有這台晶片的包、新版開不起來自動換回、手動退回、主人確認（scripts/upgrade-download-test.sh；PATH 裡沒有 go、make、git）。不隨 `make ci` 跑。
upgrade-download-test:
	$(call locked,upgrade-download-test)
_upgrade-download-test:
	scripts/upgrade-download-test.sh

# 乾淨環境的整體驗收（DEV-155 S9）：一台什麼都沒裝的 Mac（空的假 HOME、只有系統內建工具、git／make／python3 一叫就失敗、假的已登入 claude），
# 照 INSTALL.md 那一行裝 0.0.1 → 設定精靈選一位秘書、用附帶的 tmux 開她的 session → 經看板升到 0.0.2 → 檢查碼不符、新版起不來、
# 換不回去再按「退回上一版」三種失敗 → 主人確認 → 照 runbooks/install.md 移除（scripts/clean-install-test.sh，約兩分鐘）。
# 要先有 dist/tmux；隨 `make release-e2e` 跑，不隨 `make ci`。EVERGENIE_E2E_KEEP=1 保留暫存資料夾。
clean-install-test:
	$(call locked,clean-install-test)
_clean-install-test:
	scripts/clean-install-test.sh

# 下載版真的編得出主人自己的附程式套件（DEV-155，packages PKG-65、PKG-66）：dist.sh 編兩版、假 HOME、PATH 沒有 go／git，缺 Go → 問 →
# install-go 真的從 go.dev 下載 → 用程式旁邊附的 sdk 編好、套件 running → 升一版後用新版的 sdk 重編 → fork 來的 go.mod＋外部模組照樣編好 →
# 缺 sdk、go.mod 要的 Go 太新是白話（scripts/package-go-test.sh，約兩三分鐘）。**要網路**（下載 Go 一次約 70MB、proxy.golang.org 一個
# 小模組，測完刪）；隨 `make release-e2e` 跑，不隨 `make ci`。
package-go-test:
	$(call locked,package-go-test)
_package-go-test:
	scripts/package-go-test.sh
