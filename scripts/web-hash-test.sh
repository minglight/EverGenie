#!/usr/bin/env bash
# scripts/web-hash.mjs 的驗收（T2 第 2 條）：每個輸入目錄各改一個檔，看板的指紋要變；改 Go 檔、只改 spec／e2e 不能變。結束碼 0＝全過。
# 在工作目錄的暫存複本上改（只複製 web/ 與 packages/official/*/pages/、不含 node_modules），不動 worktree。
set -uo pipefail
cd "$(dirname "$0")/.."
SRC="$PWD"
T="$(mktemp -d "${TMPDIR:-/tmp}/hg-webhash-test.XXXXXX")"
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/scripts" "$T/packages/official" "$T/internal/foo"
cp scripts/web-hash.mjs "$T/scripts/"
rsync -a --exclude node_modules --exclude dist --exclude .cache web/ "$T/web/"
for d in packages/official/*/pages; do mkdir -p "$T/$d" && rsync -a "$d/" "$T/$d/"; done
echo 'package foo' >"$T/internal/foo/foo.go"
cd "$T"

h() { node scripts/web-hash.mjs "$1"; }
base_b="$(h board)"
pass=0; fail=0
# 名稱|檔案|動作|看板要變？   動作：comment＝檔尾加整行註解；token＝檔尾加一個新字串；text＝檔尾加一般文字
case_run() {
  local name="$1" file="$2" act="$3" wb="$4" bak
  bak="$(mktemp "$T/bak.XXXXXX")"
  cp -p "$file" "$bak"
  case "$act" in
    comment) printf '\n// hash-test comment\n' >>"$file" ;;
    token) printf '\nexport const __hashTest = "hash-test-newclass-%s";\n' "$RANDOM" >>"$file" ;;
    text) printf '\nhash-test text %s\n' "$RANDOM" >>"$file" ;;
  esac
  local nb rb
  nb="$(h board)"
  [ "$nb" != "$base_b" ] && rb=1 || rb=0
  cp -p "$bak" "$file"
  if [ "$rb" = "$wb" ]; then
    echo "PASS  ${name}（看板 $([ $rb = 1 ] && echo 變 || echo 不變)）"; pass=$((pass + 1))
  else
    echo "FAIL  ${name}：看板變=${rb}（要 ${wb}）"; fail=$((fail + 1))
  fi
}

official="$(ls packages/official/*/pages/*.tsx 2>/dev/null | head -1)"
[ -z "$official" ] && official="$(find packages/official -name '*.tsx' -not -name '*.spec.*' | head -1)"

# 各個輸入目錄各改一個檔 → 對應的指紋要變
case_run "看板 src（非 spec）"                 web/apps/board/src/pages/policy/PolicyPage.tsx comment 1
case_run "看板 index.html"                     web/apps/board/index.html text 1
case_run "看板 public/"                        web/apps/board/public/sw.js comment 1
case_run "看板 vite.config.ts"                 web/apps/board/vite.config.ts comment 1
case_run "看板 tsconfig.json"                  web/apps/board/tsconfig.json text 1
case_run "看板 package.json"                   web/apps/board/package.json text 1
case_run "packages/ui"                         web/packages/ui/src/index.ts comment 1
case_run "packages/sdk-web"                    web/packages/sdk-web/src/index.ts comment 1
case_run "web/package.json"                    web/package.json text 1
case_run "pnpm-lock.yaml"                      web/pnpm-lock.yaml text 1
case_run "tsconfig.base.json"                  web/tsconfig.base.json text 1
case_run "官方套件頁面新增 class 字串（Tailwind 掃描）" "$official" token 1
# 不該變的
case_run "只改官方套件頁面的註解"              "$official" comment 0
case_run "改 e2e 檔"                           web/apps/board/e2e/shell.spec.ts text 0
case_run "改 Go 檔"                            internal/foo/foo.go text 0
case_run "改看板的 playwright.config.ts"       web/apps/board/playwright.config.ts text 0

# stamp 指紋：改 spec 要變（vitest 會讀），改 e2e 不影響 vitest／typecheck 但影響 biome
sb() { node scripts/web-hash.mjs stamp "$1"; }
v0="$(sb vitest)"; t0="$(sb typecheck)"; b0="$(sb biome)"
f=web/apps/board/src/lib/cron.spec.ts; cp -p "$f" "$T/bak.spec"; echo '// x' >>"$f"
[ "$(sb vitest)" != "$v0" ] && [ "$(sb typecheck)" != "$t0" ] && [ "$(sb biome)" != "$b0" ] \
  && { echo "PASS  改 spec：vitest、typecheck、biome 的通過紀錄都失效"; pass=$((pass + 1)); } \
  || { echo "FAIL  改 spec 應讓三項通過紀錄失效"; fail=$((fail + 1)); }
cp -p "$T/bak.spec" "$f"
f=web/apps/board/e2e/shell.spec.ts; cp -p "$f" "$T/bak.e2e"; echo '// x' >>"$f"
[ "$(sb vitest)" = "$v0" ] && [ "$(sb typecheck)" = "$t0" ] && [ "$(sb biome)" != "$b0" ] \
  && { echo "PASS  改 e2e：只有 biome 的通過紀錄失效"; pass=$((pass + 1)); } \
  || { echo "FAIL  改 e2e 應只讓 biome 失效"; fail=$((fail + 1)); }
cp -p "$T/bak.e2e" "$f"

echo "通過 ${pass}，失敗 ${fail}"
[ "$fail" -eq 0 ]
