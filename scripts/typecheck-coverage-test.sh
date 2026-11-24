#!/usr/bin/env bash
# typecheck 去重／增量之後還抓得到錯（T2 第 3 條）：在 ui、sdk-web、看板、一個官方套件頁各放一個型別錯誤，
# `make web-ci` 都要失敗（結束碼非 0）並指出那個檔案；對照組（沒放錯誤）要過。跑完一定還原（原始檔 mtime 不變、dist 與通過紀錄也還原）。
# 結束碼 0＝全過。會跑好幾次 `make web-ci`（經過機器鎖），約 5 分鐘。
set -uo pipefail
cd "$(dirname "$0")/.."
T="$(mktemp -d "${TMPDIR:-/tmp}/hg-tc-test.XXXXXX")"
touched=""
restore() {
  if [ -n "$touched" ] && [ -f "$T/orig" ]; then cp -p "$T/orig" "$touched"; touched=""; fi
  rsync -a --checksum --delete "$T/dist/" internal/base/app/board/dist/ 2>/dev/null
  rsync -a --delete "$T/stamps/" web/node_modules/.cache/hg-stamps/ 2>/dev/null
}
cleanup() { restore; rm -rf "$T"; }
trap cleanup EXIT
mkdir -p "$T/dist" "$T/stamps"

pass=0; fail=0

echo "== 對照組：沒放錯誤，make web-ci 要過"
make web-ci >"$T/control.log" 2>&1; rc=$?
if [ "$rc" = 0 ]; then echo "PASS  對照組 make web-ci 結束碼 0"; pass=$((pass + 1)); else echo "FAIL  對照組結束碼 ${rc}（看 $T/control.log）"; tail -20 "$T/control.log"; fail=$((fail + 1)); fi

# 備份 dist 與通過紀錄（放錯誤的那幾次可能改寫它們）
rsync -a internal/base/app/board/dist/ "$T/dist/"
rsync -a web/node_modules/.cache/hg-stamps/ "$T/stamps/"

official="$(ls packages/official/habits/pages/*.tsx | head -1)"
for f in web/packages/ui/src/index.ts web/packages/sdk-web/src/index.ts web/apps/board/src/lib/format.ts "$official"; do
  touched="$f"
  cp -p "$f" "$T/orig"
  printf '\nexport const __typeErrorProbe: number = "not a number";\n' >>"$f"
  make web-ci >"$T/case.log" 2>&1; rc=$?
  base="$(basename "$f")"
  if [ "$rc" != 0 ] && grep -q "error TS" "$T/case.log" && grep -q "$base" "$T/case.log"; then
    echo "PASS  ${f}：make web-ci 失敗（結束碼 ${rc}）並指出 $base"; pass=$((pass + 1))
  else
    echo "FAIL  ${f}：結束碼 ${rc}，輸出裡$(grep -q 'error TS' "$T/case.log" && echo 有 || echo 沒有) error TS（看 $T/case.log）"; fail=$((fail + 1))
    tail -15 "$T/case.log"
  fi
  restore
done

echo "通過 ${pass}，失敗 ${fail}"
[ "$fail" -eq 0 ]
