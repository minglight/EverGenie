#!/usr/bin/env bash
# 新的 tmux client 連得上舊的 tmux server 嗎（升附帶的 tmux 版本時用；ai_docs/runbooks/release.md「附帶的 tmux」）。
#
# 為什麼：升級會換掉程式資料夾 fixed/ 裡的 tmux（system 卡 UPD-15），但秘書的對話還開在舊版開的 tmux server 裡
# （升級只重開服務、不關對話）；新版的引擎接著用新的 tmux 送字、看畫面。client 跟 server 的協定對不上時，
# tmux 會說 protocol version mismatch：送不進去、也看不到畫面，秘書全部失聯。
#
# 用法：scripts/tmux-compat.sh <舊的 tmux> <新的 tmux> [<disclaim>]
#   有給 disclaim 就經它開舊的 server（跟引擎一樣，engine-claude ECL-65）；兩支一樣（同版本）也照跑，驗這個流程本身是通的。
# 做法：乾淨環境（env -i，PATH 只有 /usr/bin:/bin、空的 HOME），自己的 socket（hg-compat-<pid>），不碰任何家正在用的 socket：
#   ①用舊的開 server 與一個 session；②換新的 client 做引擎會對已經開著的對話做的事：has-session、send-keys、
#   load-buffer＋paste-buffer -p＋delete-buffer、capture-pane -p／-p -e、在同一個 server 開新的 session（new-session -d）、kill-session；
#   ③畫面上要看得到新的 client 打進去的指令跑出來的結果；④用新的 kill-server 收掉，socket 檔也刪。
# 結束碼：0 連得上；1 有一步不行（印是哪一步、tmux 說了什麼）；2 用法錯。
set -euo pipefail

die() { echo "tmux-compat：$*" >&2; exit 1; }
[ $# -ge 2 ] && [ $# -le 3 ] || { sed -n '8,9p' "$0" | sed 's/^# \{0,1\}//' >&2; exit 2; }
OLD="$1" NEW="$2" LAUNCHER="${3:-}"
for f in "$OLD" "$NEW" ${LAUNCHER:+"$LAUNCHER"}; do
  [ -x "$f" ] || { echo "tmux-compat：$f 不是能執行的檔" >&2; exit 2; }
done

WORK="$(mktemp -d "${TMPDIR:-/tmp}/hg-tmux-compat.XXXXXX")"
H="$WORK/home"
mkdir -p "$H"
SOCK="hg-compat-$$"
SOCK_PATH=""
cleanup() {
  env -i HOME="$H" PATH=/usr/bin:/bin "$NEW" -L "$SOCK" kill-server >/dev/null 2>&1 || true
  env -i HOME="$H" PATH=/usr/bin:/bin "$OLD" -L "$SOCK" kill-server >/dev/null 2>&1 || true
  if [ -n "$SOCK_PATH" ]; then rm -f "$SOCK_PATH"; fi
  rm -rf "$WORK"
}
trap cleanup EXIT

old() { env -i HOME="$H" PATH=/usr/bin:/bin "$OLD" -L "$SOCK" "$@"; }
new() { env -i HOME="$H" PATH=/usr/bin:/bin "$NEW" -L "$SOCK" "$@"; }
# step <說明> <指令…>：跑一步，失敗就停、把 tmux 說的話印出來
step() {
  local what="$1" out
  shift
  out="$("$@" 2>&1)" || die "${what}失敗：${out}"
  printf '%s' "$out"
}

echo "    舊：$(env -i HOME="$H" PATH=/usr/bin:/bin "$OLD" -V)；新：$(env -i HOME="$H" PATH=/usr/bin:/bin "$NEW" -V)"
if [ -n "$LAUNCHER" ]; then
  step "用舊的 tmux（經 disclaim）開 server " env -i HOME="$H" PATH=/usr/bin:/bin "$LAUNCHER" "$OLD" -L "$SOCK" new-session -d -s old -x 160 -y 50 /bin/sh >/dev/null
else
  step "用舊的 tmux 開 server " old new-session -d -s old -x 160 -y 50 /bin/sh >/dev/null
fi
SOCK_PATH="$(step "用舊的 tmux 讀 socket 位置" old display-message -p '#{socket_path}')"
case "$SOCK_PATH" in */"$SOCK") ;; *) die "socket 位置不是自己的（${SOCK_PATH}）" ;; esac

marker="compat-$$"
step "新的 client：has-session " new has-session -t '=old' >/dev/null
step "新的 client：send-keys " new send-keys -t '=old:' "echo \$((6*7))-$marker" Enter >/dev/null
printf '%s' "echo \$((2+3))-$marker-paste" | new load-buffer -b hg-compat - 2>"$WORK/err" || die "新的 client：load-buffer 失敗：$(cat "$WORK/err")"
step "新的 client：paste-buffer -p " new paste-buffer -p -d -b hg-compat -t '=old:' >/dev/null
step "新的 client：送 Enter " new send-keys -t '=old:' Enter >/dev/null
printf '%s' "x" | new load-buffer -b hg-compat-left - 2>"$WORK/err" || die "新的 client：load-buffer 失敗：$(cat "$WORK/err")"
step "新的 client：delete-buffer " new delete-buffer -b hg-compat-left >/dev/null
out=""
for _ in $(seq 1 50); do
  out="$(step "新的 client：capture-pane -p " new capture-pane -p -t '=old:')"
  case "$out" in *"42-$marker"*"5-$marker-paste"*) break ;; esac
  sleep 0.1
done
case "$out" in
  *"42-$marker"*"5-$marker-paste"*) ;;
  *) printf '%s\n' "$out" >&2; die "新的 client 打進舊的 server 的指令，畫面上看不到結果" ;;
esac
step "新的 client：capture-pane -p -e " new capture-pane -p -e -t '=old:' >/dev/null
step "新的 client：在舊的 server 開新的 session " new new-session -d -s fresh -x 80 -y 24 /bin/sleep 600 >/dev/null
step "新的 client：has-session（新開的） " new has-session -t '=fresh' >/dev/null
step "新的 client：kill-session " new kill-session -t '=fresh' >/dev/null
step "新的 client：kill-server " new kill-server >/dev/null
echo "    新的 client 對舊的 server：has-session、send-keys、paste-buffer -p、delete-buffer、capture-pane -p／-e、new-session、kill-session、kill-server 都正常"
