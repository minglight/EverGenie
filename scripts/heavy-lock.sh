#!/usr/bin/env bash
# 全機一次只跑一個重型測試（T2 第 1 條）。所有 worktree、所有 session 共用同一把鎖：
#   ${XDG_CACHE_HOME:-$HOME/.cache}/evergenie/heavy.lock（用 macOS 內建的 shlock，沒有 flock）
#
# 用法：scripts/heavy-lock.sh <目標名稱> -- <指令...>
#   拿到鎖才執行指令（子行程帶 EVERGENIE_HEAVY_HELD=1，巢狀的 make 不會重拿）；指令結束就放鎖，結束碼原樣回傳。
#   拿不到：每 5 秒印一行「等另一個測試跑完：PID n（<目標>，<worktree>），已等 m 秒」。
#   持有者死掉（PID 不在）shlock 會自動清掉舊鎖，等的人在下一個 5 秒內接手。
#
# 環境變數：
#   EVERGENIE_HEAVY_WAIT=1800   最久等多少秒，超過就失敗（結束碼 75）並說明
#   EVERGENIE_HEAVY_POLL=5      幾秒看一次（測試用，平常不用改）
#   EVERGENIE_HEAVY_LOCK=0      關掉鎖，直接執行（只給特殊情況：例如你確定機器上沒有別的測試、或在排查鎖本身；日常不要用）
#   EVERGENIE_HEAVY_HELD=1      已經在鎖裡（巢狀 make 用，不要手動設）
#
# 限制：持有者被 kill -9 時它的子行程不會被清掉（沒有訊號可以攔），鎖會因為 PID 不在而放掉，
# 但孤兒子行程可能還在跑一小段時間；一般的 INT／TERM／HUP 會連同子行程一起收掉。
set -u

if [ $# -lt 1 ]; then
  echo "用法：$0 <目標名稱> -- <指令...>" >&2
  exit 2
fi
label="$1"
shift
[ "${1:-}" = "--" ] && shift
if [ $# -lt 1 ]; then
  echo "用法：$0 <目標名稱> -- <指令...>" >&2
  exit 2
fi

if [ "${EVERGENIE_HEAVY_LOCK:-1}" = "0" ] || [ -n "${EVERGENIE_HEAVY_HELD:-}" ]; then
  exec "$@"
fi

dir="${XDG_CACHE_HOME:-$HOME/.cache}/evergenie"
mkdir -p "$dir" || { echo "建不了鎖資料夾 $dir" >&2; exit 1; }
lock="$dir/heavy.lock"
info="$dir/heavy.info"
max="${EVERGENIE_HEAVY_WAIT:-1800}"
poll="${EVERGENIE_HEAVY_POLL:-5}"
me=$$
here="$(pwd -P)"

start=$(date +%s)
while ! shlock -f "$lock" -p "$me" 2>/dev/null; do
  waited=$(($(date +%s) - start))
  holder="$(cat "$lock" 2>/dev/null || true)"
  desc="PID ${holder:-?}"
  if [ -n "$holder" ] && [ -f "$info" ]; then
    # info 格式：<pid>\t<目標>\t<worktree>；pid 對得上才用，不然就是舊的
    if [ "$(cut -f1 "$info" 2>/dev/null)" = "$holder" ]; then
      desc="PID ${holder}（$(cut -f2 "$info" 2>/dev/null)，$(cut -f3 "$info" 2>/dev/null)）"
    fi
  fi
  if [ "$waited" -ge "$max" ]; then
    echo "等了 ${waited} 秒（上限 EVERGENIE_HEAVY_WAIT=${max}）還是沒拿到鎖，放棄。持有者：${desc}" >&2
    echo "  確認它還活著：ps -p ${holder:-?}；真的卡死了就殺掉那個 PID（鎖會自動放掉）。" >&2
    echo "  特殊情況可用 EVERGENIE_HEAVY_LOCK=0 跳過鎖（會跟別的測試搶 CPU，日常不要用）。" >&2
    exit 75
  fi
  echo "等另一個測試跑完：${desc}，已等 ${waited} 秒" >&2
  sleep "$poll"
done

printf '%s\t%s\t%s\n' "$me" "$label" "$here" >"$info"

child=""
release() {
  if [ "$(cat "$lock" 2>/dev/null || true)" = "$me" ]; then
    rm -f "$lock" "$info"
  fi
}
killtree() {
  local p="$1" c
  for c in $(pgrep -P "$p" 2>/dev/null); do killtree "$c"; done
  kill -TERM "$p" 2>/dev/null || true
}
on_signal() {
  [ -n "$child" ] && killtree "$child"
  release
  exit 130
}
trap release EXIT
trap on_signal INT TERM HUP

EVERGENIE_HEAVY_HELD=1 "$@" &
child=$!
# wait 會被訊號打斷；被打斷後（沒走 on_signal 的情況）繼續等到子行程真的結束。
rc=0
while :; do
  wait "$child"
  rc=$?
  if kill -0 "$child" 2>/dev/null; then continue; fi
  break
done
exit "$rc"
