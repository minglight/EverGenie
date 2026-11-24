#!/usr/bin/env bash
# 只在內容真的變了才覆蓋執行檔（T2 第 4 條）。
# `go build -o x` 每次都會重寫 x（mtime 變）；go test 的快取鍵包含「測試讀過的檔案與資料夾條目」的 mtime，
# 而 doccheck 之類的測試會讀整個 repo（含 bin/），於是每次 `make build` 之後它們都不吃快取。
# 先編到暫存檔，內容一樣就丟掉、不動 x。
#
# 用法：scripts/go-build-bin.sh <模組資料夾> <輸出檔（相對 repo 根目錄）> <套件> [go build 的其他參數...]
set -euo pipefail
export GOMAXPROCS="${GOMAXPROCS:-${EVERGENIE_GOPROCS:-4}}"
root="$(cd "$(dirname "$0")/.." && pwd)"
mod="$1"; out="$2"; pkg="$3"; shift 3
dest="$root/$out"
mkdir -p "$(dirname "$dest")"
# 暫存檔放在 bin/ 外面：在 bin/ 裡建立再刪掉會改到資料夾本身的 mtime，同樣會讓讀 bin/ 的測試快取失效。
tmp="$(mktemp "${TMPDIR:-/tmp}/hg-bin.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
(cd "$root/$mod" && go build -p "${EVERGENIE_CPU:-4}" "$@" -o "$tmp" "$pkg")
if [ -f "$dest" ] && cmp -s "$tmp" "$dest"; then
  exit 0
fi
chmod 755 "$tmp"
mv -f "$tmp" "$dest"
