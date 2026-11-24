#!/usr/bin/env bash
# make check 的 Go 部分（T2 第 5 條）：全部編譯（不連結執行檔）→ vet 與 go test 只跑受影響的套件。
# 受影響 = 改到的套件＋依賴它們的套件（scripts/go-affected.py）；算不出來（ALL）就全跑，靠 go 的快取。
# internal/tools/ 的原始碼掃描測試每次都跑（它們讀整個 repo，快取沒變就是 (cached)），而且不開 -race（同 make test）。
set -euo pipefail
export GOMAXPROCS="${GOMAXPROCS:-${EVERGENIE_GOPROCS:-4}}"
cd "$(dirname "$0")/.."
P="${EVERGENIE_CPU:-4}"

go build -p "$P" ./...
(cd sdk && go build -p "$P" ./...)

aff="$(python3 scripts/go-affected.py)"
if [ "$aff" = "ALL" ]; then
  echo "go-check：改動不只在 Go 套件裡（或算不出來），vet 與 go test 全跑（靠快取）"
  make --no-print-directory vet
  make --no-print-directory _test
  exit 0
fi

go test -p "$P" ./internal/tools/...
if [ -z "$aff" ]; then
  echo "go-check：沒有受影響的 Go 套件"
  exit 0
fi
while IFS=$'\t' read -r mod pkgs; do
  [ -z "$mod" ] && continue
  echo "go-check：$mod 受影響的套件：$pkgs"
  # shellcheck disable=SC2086
  (cd "$mod" && go vet -p "$P" $pkgs)
  race=""; norace=""
  for p in $pkgs; do
    case "$p" in
      */internal/tools/*) norace="$norace $p" ;; # 上面已經跑過
      *) race="$race $p" ;;
    esac
  done
  if [ -n "$race" ]; then
    # shellcheck disable=SC2086
    (cd "$mod" && go test -p "$P" -race $race)
  fi
done <<<"$aff"
