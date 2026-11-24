#!/usr/bin/env bash
# 前端檢查的「通過紀錄」（T2 第 2、3 條的延伸，作用跟 go test 的 (cached) 一樣）：
#   scripts/web-stamp.sh <biome|typecheck|vitest> -- <指令...>
# 這項檢查的輸入指紋（scripts/web-hash.mjs stamp <名稱>）跟上次「通過」時一樣，就不重跑、重印上次的摘要；
# 不一樣（或 FORCE=1）才真的跑，通過才記錄，失敗不記錄。輸入含 pnpm-lock.yaml 與 node 版本，換套件／換 node 一定重跑。
# 紀錄在 web/node_modules/.cache/hg-stamps/（每個 worktree 各自一份，不進 git）。
set -uo pipefail
cd "$(dirname "$0")/.."
# --probe：只看有沒有通過紀錄（有：重印摘要、結束碼 0；沒有：結束碼 1），不跑指令。make check 用它跳過 vitest。
probe=0
if [ "${1:-}" = "--probe" ]; then probe=1; shift; fi
name="${1:?用法：$0 [--probe] <biome|typecheck|vitest> -- 指令...}"
shift
[ "${1:-}" = "--" ] && shift
dir=web/node_modules/.cache/hg-stamps
mkdir -p "$dir"
file="$dir/$name"
fp="$(node scripts/web-hash.mjs stamp "$name")" || exit 1
if [ "${FORCE:-}" != "1" ] && [ -f "$file" ] && [ "$(head -1 "$file")" = "$fp" ]; then
  echo "${name}：輸入沒變，沿用上次通過的結果"
  tail -n +2 "$file"
  exit 0
fi
if [ "$probe" = 1 ]; then exit 1; fi
log="$(mktemp "${TMPDIR:-/tmp}/hg-stamp.XXXXXX")"
trap 'rm -f "$log"' EXIT
"$@" 2>&1 | tee "$log"
rc=${PIPESTATUS[0]}
if [ "$rc" = 0 ]; then
  {
    echo "$fp"
    # 摘要行（vitest 的 Test Files／Tests）原樣保存，下次命中時重印，測試總數才看得到
    sed $'s/\x1b\\[[0-9;]*m//g' "$log" | grep -E '^ *(Test Files|Tests) ' || true
  } >"$file"
else
  rm -f "$file"
fi
exit "$rc"
