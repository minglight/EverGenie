#!/usr/bin/env bash
# 檢查 playwright.config.ts 的 mobileSpecs 沒漏（T3）：有 `isMobile` 的 spec 都要在清單裡，除非在下面的例外（附原因）。
# 手機 project 只跑 mobileSpecs；漏列的 spec 裡手機專屬的測試會在桌機被跳過、等於消失。
set -euo pipefail
cd "$(dirname "$0")/../web/apps/board"
# 例外：isMobile 只用來幫截圖檔命名（policy）；或只用來換打開指令面板的方法，手機的開法已在 palette.spec 驗過（reminders）。
allow=" policy reminders "
list="$(awk '/^const mobileSpecs = \[/,/^\];/' playwright.config.ts | sed -n 's|.*"\*\*/\(.*\)\.spec\.ts".*|\1|p')"
[ -n "$list" ] || { echo "e2e-mobile-specs-check：讀不到 playwright.config.ts 的 mobileSpecs" >&2; exit 1; }
bad=0
for f in e2e/*.spec.ts; do
  name="$(basename "$f" .spec.ts)"
  case "$name" in shots*|two-*) continue ;; esac
  grep -q isMobile "$f" || continue
  case "$allow" in *" $name "*) continue ;; esac
  if ! printf '%s\n' "$list" | grep -qx "$name"; then
    echo "e2e-mobile-specs-check：$f 有 isMobile 但不在 playwright.config.ts 的 mobileSpecs（手機的斷言不會跑）" >&2
    bad=1
  fi
done
for n in $list; do
  [ -f "e2e/$n.spec.ts" ] || { echo "e2e-mobile-specs-check：mobileSpecs 列了不存在的 $n.spec.ts" >&2; bad=1; }
done
exit $bad
