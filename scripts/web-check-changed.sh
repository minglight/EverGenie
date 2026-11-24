#!/usr/bin/env bash
# make check 與 make web-ci 的前端部分（只測改到的）：頁面資料夾互相 import 的檢查（全掃）、第三方授權聲明跟相依一致（全看）、biome（全看，約 1 秒）、
# typecheck（整個專案，tsc 增量，加通過紀錄）、vitest --changed 的基準（EVERGENIE_BASE）。全跑用 make web-ci-full。
# 打包（有改才打）在 Makefile 的 _web 先做過了。
# 頁面資料夾之間不互相 import（FEN-27，scripts/web-page-imports.mjs）：全掃，不到一秒，不看改了什麼。
set -euo pipefail
cd "$(dirname "$0")/.."
node scripts/web-page-imports.mjs
# 頁面不用新分頁開圖片或影片（FEN-30，scripts/web-media-newtab.mjs）：全掃看板、共用元件與官方套件的頁面。
node scripts/web-media-newtab.mjs
# 第三方授權聲明 LICENSES/web-third-party.txt 跟現在的相依一致（scripts/web-licenses.mjs --check；只讀 node_modules 的 package.json，不到一秒）。
node scripts/web-licenses.mjs --check
# 比較基準：跟 EVERGENIE_BASE（預設 main）的 merge-base。直接在 main 上 commit 時 merge-base 就是 HEAD，「改到的」會是空的
# （2026-11-03 踩過：e2e 檔格式錯，web-ci 全過、到發版前的 web-ci-full 才抓到）；這時改用 origin/<基準>，算「還沒推上去的」。
base="${EVERGENIE_BASE:-main}"
mb="$(git merge-base HEAD "$base")"
if [ "$mb" = "$(git rev-parse HEAD)" ] && git rev-parse --verify -q "origin/$base" >/dev/null; then
  mb="$(git merge-base HEAD "origin/$base")"
  export EVERGENIE_BASE="$mb"
fi
# biome：整個 web/ 都看（含 e2e/、scripts/；393 個檔約 1 秒，不值得只挑改到的而漏掉）。D-53。
(cd web && pnpm exec biome ci .)
scripts/web-stamp.sh typecheck -- sh -c 'cd web && pnpm typecheck'
# vitest：輸入跟上次「全跑通過」一樣就沿用；不一樣只跑相關的（vitest --changed；
# 動到 package.json／vitest.config.ts 時 vitest 自己會判斷要全跑）。
scripts/web-stamp.sh --probe vitest || (cd web && pnpm test:changed)
