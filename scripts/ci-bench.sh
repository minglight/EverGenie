#!/usr/bin/env bash
# 量測 make ci / make web-ci / make e2e-smoke 的 CPU 秒數（user+sys）與牆鐘（T2 第 0 條、第 6 條）。
#
# 用法：scripts/ci-bench.sh [-o 輸出資料夾] [情境...]
#   情境：warm（先跑一輪暖機，讓快取熱起來；量但標暖機）、A（沒改檔）、B（只改一個 Go 測試檔）、C（只改一個看板 .tsx）
#   不給情境＝warm A B C。每個情境、每個目標只跑 1 次（量測本身也耗資源）。
#   B、C 的「加一行註解」量完一定還原（內容從備份拷回，不用 git 寫入指令）；dist 資料夾也還原。
#
# 規則（計畫 T2 第 0 條）：每個情境開始前看 1 分鐘 load average，超過核心數就等 5 分鐘再看，最多重等 2 次，
# 仍超過照實記。若 scripts/heavy-lock.sh 存在，整段量測先拿機器鎖（量的時候別人排隊）。
# 輸出：<輸出資料夾>/result.md（Markdown 表）、每個目標的原始 log 與 /usr/bin/time -l 輸出。
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# 有鎖就整段在鎖裡跑（EVERGENIE_HEAVY_HELD=1 讓裡面的 make 不重拿）。
if [ -x scripts/heavy-lock.sh ] && [ -z "${EVERGENIE_HEAVY_HELD:-}" ] && [ "${EVERGENIE_HEAVY_LOCK:-1}" != "0" ]; then
  exec scripts/heavy-lock.sh ci-bench -- "$0" "$@"
fi

export GOCACHE="${GOCACHE:-/tmp/evergenie-gocache}"

OUT=""
while getopts "o:" opt; do
  case "$opt" in
    o) OUT="$OPTARG" ;;
    *) echo "用法：$0 [-o 輸出資料夾] [warm|A|B|C ...]" >&2; exit 2 ;;
  esac
done
shift $((OPTIND - 1))
SCENARIOS=("$@")
[ ${#SCENARIOS[@]} -eq 0 ] && SCENARIOS=(warm A B C)
[ -z "$OUT" ] && OUT="$(mktemp -d "${TMPDIR:-/tmp}/hg-ci-bench.XXXXXX")"
mkdir -p "$OUT"
RESULT="$OUT/result.md"
NCPU="$(sysctl -n hw.ncpu)"
BK="$OUT/backup"
mkdir -p "$BK"

GO_FILE="$(ls internal/tasks/*_test.go | head -1)"
TSX_FILE="web/apps/board/src/pages/policy/PolicyPage.tsx"

# EVERGENIE_BENCH_TARGETS="ci web-ci" 可以只量其中幾個；EVERGENIE_BENCH_NOWAIT=1 不等 load 降下來（照實記開始時的 load）
read -r -a TARGETS <<<"${EVERGENIE_BENCH_TARGETS:-ci web-ci e2e-smoke}"

{
  echo "# ci-bench $(date '+%F %T')  核心數 ${NCPU}  worktree ${ROOT}"
  echo "Go 檔：${GO_FILE}  tsx 檔：${TSX_FILE}  EVERGENIE_BENCH_LABEL=${EVERGENIE_BENCH_LABEL:-}"
  echo
  echo "| 情境 | 目標 | 結束碼 | 牆鐘 s | user s | sys s | CPU s | 最大記憶體 MB | 開始時 load(1m) | 測試數 |"
  echo "|---|---|---|---|---|---|---|---|---|---|"
} >"$RESULT"

load1() { sysctl -n vm.loadavg | awk '{print $2}'; }

# 開始時 load 超過核心數就等 5 分鐘，最多重等 2 次。
wait_for_load() {
  [ "${EVERGENIE_BENCH_NOWAIT:-}" = "1" ] && return 0
  local tries=0 l
  while :; do
    l="$(load1)"
    if awk -v l="$l" -v n="$NCPU" 'BEGIN{exit !(l<=n)}'; then return 0; fi
    if [ "$tries" -ge 2 ]; then
      echo "load $l 仍超過核心數 ${NCPU}，照實記錄後開始" >&2
      return 0
    fi
    tries=$((tries + 1))
    echo "load $l > ${NCPU}，等 5 分鐘（第 $tries 次）" >&2
    sleep 300
  done
}

count_tests() { # $1=目標 $2=log
  case "$1" in
    ci)
      local ok cached
      ok="$(grep -c '^ok' "$2" || true)"
      cached="$(grep -c '^ok.*(cached)' "$2" || true)"
      echo "go ok 包數 ${ok}（cached ${cached}）"
      ;;
    web-ci)
      sed -n 's/^ *Tests  *\([0-9]*\) passed.*/vitest \1 測試/p' "$2" | tail -1
      ;;
    e2e-smoke)
      sed -n 's/^ *\([0-9][0-9]*\) passed.*/e2e-smoke \1 測試/p' "$2" | tail -1
      ;;
  esac
}

measure() { # $1=情境 $2=目標
  local sc="$1" tg="$2" log="$OUT/$1-$2.log" tm="$OUT/$1-$2.time" l0 rc
  l0="$(load1)"
  /usr/bin/time -l -o "$tm" make "$tg" >"$log" 2>&1
  rc=$?
  local real user sys rss cpu
  read -r real user sys < <(awk '/ real /{print $1, $3, $5; exit}' "$tm")
  rss="$(awk '/maximum resident set size/{printf "%.0f", $1/1048576}' "$tm")"
  cpu="$(awk -v u="$user" -v s="$sys" 'BEGIN{printf "%.1f", u+s}')"
  echo "| $sc | make $tg | $rc | $real | $user | $sys | $cpu | $rss | $l0 | $(count_tests "$tg" "$log") |" >>"$RESULT"
  echo "[$sc] make $tg → exit $rc, CPU ${cpu}s, real ${real}s" >&2
}

backup_dist() { rm -rf "$BK/dist"; mkdir -p "$BK/dist"; rsync -a internal/base/app/board/dist/ "$BK/dist/dist/" 2>/dev/null; }
restore_dist() {
  mkdir -p internal/base/app/board/dist
  rsync -a --delete "$BK/dist/dist/" internal/base/app/board/dist/
}

touched=""
restore_touched() {
  if [ -n "$touched" ]; then
    cp "$BK/touched.orig" "$touched" && echo "已還原 $touched" >&2
    touched=""
  fi
  [ -d "$BK/dist" ] && restore_dist
}
trap restore_touched EXIT

touch_file() { # 備份後在檔尾加一行註解
  touched="$1"
  cp "$1" "$BK/touched.orig"
  printf '\n// ci-bench touch\n' >>"$1"
}

for sc in "${SCENARIOS[@]}"; do
  wait_for_load
  backup_dist
  case "$sc" in
    warm) echo "| warm（暖機，含 pnpm install／首次編譯，不列入驗收） | | | | | | | | | |" >>"$RESULT" ;;
    A) ;;
    B) touch_file "$GO_FILE" ;;
    C) touch_file "$TSX_FILE" ;;
    *) echo "不認得的情境：$sc" >&2; exit 2 ;;
  esac
  for tg in "${TARGETS[@]}"; do measure "$sc" "$tg"; done
  restore_touched
done

echo >>"$RESULT"
echo "結束 $(date '+%T')，原始 log 在 $OUT" >>"$RESULT"
cat "$RESULT"
