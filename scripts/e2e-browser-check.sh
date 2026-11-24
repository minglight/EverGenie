#!/usr/bin/env bash
# 檢查 e2e 用的瀏覽器（Playwright 的 chromium headless shell）下載了沒；沒有就用白話說明怎麼辦並以結束碼 1 結束（T3）。
# 不下載任何東西——下載由 `make e2e-install` 做。版本跟 @playwright/test 綁定：升級 Playwright 後要重新 `make e2e-install`。
set -euo pipefail
cd "$(dirname "$0")/../web/apps/board"
loc="$(pnpm exec playwright install --only-shell --dry-run chromium 2>/dev/null | awk '/Install location:/ {print $3; exit}')"
if [ -n "$loc" ] && [ -f "$loc/INSTALLATION_COMPLETE" ]; then
  exit 0
fi
cat >&2 <<MSG
e2e 用的瀏覽器（Playwright 的輕量無頭 Chromium）還沒下載。
請先跑一次：make e2e-install（只下載一次，約 100 MB，存在 ~/Library/Caches/ms-playwright；Playwright 升級後要再跑一次）。
MSG
exit 1
