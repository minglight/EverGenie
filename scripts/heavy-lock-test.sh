#!/usr/bin/env bash
# scripts/heavy-lock.sh 與 Makefile 掛鎖的驗收（T2 第 1 條）。結束碼 0＝全過。
# 全程用自己的 XDG_CACHE_HOME（暫存），不碰真正的機器鎖，也不跑真的測試（用 sleep／echo 代替）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
cd "$(dirname "$0")/.."
LOCKSH="$PWD/scripts/heavy-lock.sh"
T="$(mktemp -d "${TMPDIR:-/tmp}/hg-lock-test.XXXXXX")"
export XDG_CACHE_HOME="$T/cache"
export EVERGENIE_HEAVY_POLL=1
unset EVERGENIE_HEAVY_HELD EVERGENIE_HEAVY_LOCK
pass=0; fail=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; fi; }
cleanup() { pkill -f "$T" 2>/dev/null; rm -rf "$T"; }
trap cleanup EXIT

mkdir -p "$T/wtA" "$T/wtB"
TAG="1$RANDOM$RANDOM" # 只用來認出本測試開的 sleep，收尾時 pkill

# 1. 兩個 worktree 同時要鎖：第二個印等待訊息，第一個結束後才開始
(cd "$T/wtA" && "$LOCKSH" web-ci -- bash -c 'date +%s > "$0"; sleep 6; date +%s > "$1"' "$T/a.start" "$T/a.end") >"$T/a.out" 2>&1 &
apid=$!
sleep 1
holder="$(cat "$T/cache/evergenie/heavy.lock" 2>/dev/null)"
(cd "$T/wtB" && "$LOCKSH" web-ci -- bash -c 'date +%s > "$0"' "$T/b.start") >"$T/b.out" 2>&1
brc=$?
wait "$apid"
check "1a 第二個印出等待訊息（含持有者 PID ${holder}、目標、worktree、已等秒數）" \
  "[ -n '$holder' ] && grep -qE '等另一個測試跑完：PID ${holder}（web-ci，.*wtA），已等 [0-9]+ 秒' '$T/b.out'"
check "1b 第一個結束後第二個才開始" "[ \"\$(cat '$T/b.start')\" -ge \"\$(cat '$T/a.end')\" ]"
check "1c 兩個都成功（結束碼 0）" "[ $brc -eq 0 ] && [ ! -s '$T/a.out' ]"
check "1d 結束後鎖檔已清掉" "[ ! -e '$T/cache/evergenie/heavy.lock' ]"

# 2. kill -9 第一個，第二個 10 秒內接手（用預設的 5 秒輪詢，不是測試用的 1 秒）
(cd "$T/wtA" && EVERGENIE_HEAVY_POLL=5 "$LOCKSH" web-ci -- sleep "$TAG") >"$T/a2.out" 2>&1 &
apid=$!
sleep 1
holder="$(cat "$T/cache/evergenie/heavy.lock" 2>/dev/null)"
(cd "$T/wtB" && EVERGENIE_HEAVY_POLL=5 "$LOCKSH" web-ci -- echo got-it "$T") >"$T/b2.out" 2>&1 &
bpid=$!
sleep 2
kill -0 "$bpid" 2>/dev/null || bad "2 第二個不該在第一個還活著時就結束"
tk=$(date +%s)
kill -9 "$holder" 2>/dev/null
wait "$apid" 2>/dev/null
wait "$bpid"
b2rc=$?
t1=$(date +%s)
check "2a kill -9 之後第二個 10 秒內接手並成功（實際 $((t1 - tk)) 秒）" \
  "[ $b2rc -eq 0 ] && [ $((t1 - tk)) -le 10 ] && grep -q got-it '$T/b2.out'"
pkill -f "sleep $TAG" 2>/dev/null

# 3. 巢狀：鎖裡面再呼叫（EVERGENIE_HEAVY_HELD=1）不會自己等自己
(cd "$T/wtA" && "$LOCKSH" outer -- "$LOCKSH" inner -- echo nested-ok) >"$T/n.out" 2>&1 &
npid=$!
for _ in 1 2 3 4 5 6 7 8; do kill -0 "$npid" 2>/dev/null || break; sleep 1; done
if kill -0 "$npid" 2>/dev/null; then kill -9 "$npid"; bad "3 巢狀呼叫卡住"; else
  check "3 巢狀呼叫直接執行" "grep -q nested-ok '$T/n.out' && ! grep -q 等另一個 '$T/n.out'"; fi

# 4. 等太久就失敗：EVERGENIE_HEAVY_WAIT=3
(cd "$T/wtA" && "$LOCKSH" holder -- sleep "$TAG") >/dev/null 2>&1 &
hpid=$!
sleep 1
(cd "$T/wtB" && EVERGENIE_HEAVY_WAIT=3 "$LOCKSH" waiter -- echo never) >"$T/w.out" 2>&1
wrc=$?
kill "$hpid" 2>/dev/null; wait "$hpid" 2>/dev/null; pkill -f "sleep $TAG" 2>/dev/null
check "4 等超過 EVERGENIE_HEAVY_WAIT 就失敗（結束碼 75）並說明" "[ $wrc -eq 75 ] && grep -q 放棄 '$T/w.out' && ! grep -q '^never' '$T/w.out'"

# 5. EVERGENIE_HEAVY_LOCK=0 直接執行；子指令的結束碼原樣回傳
(cd "$T/wtA" && "$LOCKSH" holder -- sleep "$TAG") >/dev/null 2>&1 &
hpid=$!
sleep 1
EVERGENIE_HEAVY_LOCK=0 "$LOCKSH" x -- bash -c 'exit 7'; r0=$?
kill "$hpid" 2>/dev/null; wait "$hpid" 2>/dev/null; pkill -f "sleep $TAG" 2>/dev/null
check "5a EVERGENIE_HEAVY_LOCK=0 不等鎖、結束碼原樣（7）" "[ $r0 -eq 7 ]"
"$LOCKSH" x -- bash -c 'exit 9'; r1=$?
check "5b 有鎖時子指令結束碼原樣回傳（9）" "[ $r1 -eq 9 ]"

# 6. Makefile：每個重型目標的配方都經過 heavy-lock.sh（make -n 只印不跑）
for tgt in ci check test web web-ci e2e-bin e2e e2e-smoke shots e2e-full; do
  check "6 make $tgt 經過鎖" "make -n $tgt 2>/dev/null | grep -q 'scripts/heavy-lock.sh $tgt --'"
done

echo "通過 ${pass}，失敗 ${fail}"
[ "$fail" -eq 0 ]
