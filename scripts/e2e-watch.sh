#!/usr/bin/env bash
# 量 e2e 跑的時候瀏覽器吃多少記憶體（T3）：每 N 秒（預設 30）用 ps 記一次瀏覽器行程（只算 Playwright 的 chromium headless shell，不算你自己開的 Chrome）的
# 數量與 RSS 總和，另外記 evergenie 行程（serve＋假引擎）的數量與 RSS；被監看的 PID 結束就自己停，最後印最大值。
# 用法：scripts/e2e-watch.sh <被監看的 PID> <輸出檔.tsv> [間隔秒]
# 例：/usr/bin/time -l make e2e-full 2> time.txt & pid=$!; scripts/e2e-watch.sh $pid watch.tsv; wait $pid
set -u
pid="${1:?用法：$0 <PID> <輸出檔> [間隔秒]}"
out="${2:?用法：$0 <PID> <輸出檔> [間隔秒]}"
every="${3:-30}"
printf 'time\tbrowser_procs\tbrowser_rss_mb\tevergenie_procs\tevergenie_rss_mb\n' >"$out"
while :; do
  ps -axo rss=,command= | awk -v t="$(date +%H:%M:%S)" '
    /ms-playwright\/chromium_headless_shell/ && !/awk/ { b++; br += $1; next }
    /bin\/evergenie/ && !/awk/ { h++; hr += $1 }
    END { printf "%s\t%d\t%.1f\t%d\t%.1f\n", t, b, br / 1024, h, hr / 1024 }' >>"$out"
  kill -0 "$pid" 2>/dev/null || break
  sleep "$every"
done
awk -F'\t' 'NR > 1 { if ($2 > bp) bp = $2; if ($3 > br) br = $3; if ($4 > hp) hp = $4; if ($5 > hr) hr = $5 }
  END { printf "最大值：瀏覽器行程 %d 個、RSS 總和 %.1f MB；evergenie 行程 %d 個、RSS 總和 %.1f MB\n", bp, br, hp, hr }' "$out"
