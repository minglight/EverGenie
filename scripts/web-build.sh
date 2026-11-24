#!/usr/bin/env bash
# 前端沒改就不打包（T2 第 2 條）：用法 scripts/web-build.sh board；FORCE=1 強制重打包。
#
# 指紋（scripts/web-hash.mjs）跟 dist/.source-hash 一樣、且 dist 在，就印「<名稱>沒改，跳過打包」。
# 要打包時先編到暫存資料夾，再用 `rsync --checksum --delete` 同步進 dist：內容一樣的檔案不會被重寫（mtime 不變），
# 因為 go test 的快取鍵含檔案 mtime（見 scripts/go-build-bin.sh），而 internal/base/app/board 的測試與 depcheck 等會讀 dist；
# vite 的 emptyOutDir 每次都整個清掉重寫，會讓這些測試的快取全失效。
# .source-hash 在同步成功後才寫（內容沒變就不動它）。
set -euo pipefail
cd "$(dirname "$0")/.."
app="${1:?用法：$0 board}"
case "$app" in
  board) out=internal/base/app/board/dist; name=看板 ;;
  *) echo "用法：$0 board" >&2; exit 2 ;;
esac
want="$(node scripts/web-hash.mjs "$app")"
have="$(cat "$out/.source-hash" 2>/dev/null || true)"
if [ "${FORCE:-}" != "1" ] && [ "$want" = "$have" ] && [ -f "$out/index.html" ]; then
  echo "${name}沒改，跳過打包"
  exit 0
fi
tmp="$(mktemp -d "${TMPDIR:-/tmp}/hg-web-$app.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
(cd "web/apps/$app" && pnpm exec vite build --outDir "$tmp/out" --emptyOutDir)
mkdir -p "$out"
rsync -a --checksum --delete --exclude .source-hash "$tmp/out/" "$out/"
if [ "$(cat "$out/.source-hash" 2>/dev/null || true)" != "$want" ]; then
  printf '%s\n' "$want" >"$out/.source-hash"
fi
