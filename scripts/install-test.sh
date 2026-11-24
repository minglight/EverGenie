#!/usr/bin/env bash
# scripts/install.sh 下載版（預設的裝法）的驗收（DEV-155 S3）。結束碼 0＝全過。`make install-test` 跑它，再跑從原始碼裝的 install-source-test.sh。
# 真的編一包（scripts/dist.sh：這台的晶片、帶 scripts/build-tmux.sh 編的 tmux 與啟動程式 disclaim）放在本機 http；每個情境一個假 HOME；
# 跑安裝程式的環境是 env -i（沒有 CLAUDE_CONFIG_DIR、EVERGENIE_* 這些），PATH 只有系統內建工具的替身資料夾（沒有 git、go、make、brew、tmux、python3）
# 加假 HOME 的 ~/.local/bin（假的 claude 在那裡）。服務是背景行程（EVERGENIE_SERVICE_MODE=process），不碰 launchd、不碰任何家、不碰各家的 tmux socket。
# 情境 1 另外驗公開角色（DEV-155 S7）：裝好就有 repo 的 packages/official/personas/cards/ 那幾位、卡在那一版的程式資料夾裡；圖走本機 http 的測試角色包（檢查碼不對整包不換）、放狀態資料夾，家裡一個檔都不寫。
# 要先有 dist/tmux（scripts/build-tmux.sh；或用 EVERGENIE_DIST_TMUX 指到別處）。Go 的相依模組要已經在本機快取（dist.sh 不連網）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
cd "$(dirname "$0")/.." || exit 2
ROOT="$PWD"
. "$ROOT/scripts/lib/download-e2e.sh"

case "$(uname -m)" in arm64) ARCH=arm64 TARCH=arm64 OTHER=amd64 ;; *) ARCH=amd64 TARCH=x86_64 OTHER=arm64 ;; esac
TMUXD="${EVERGENIE_DIST_TMUX:-$ROOT/dist/tmux}"
PRODUCT="$(sed -n 's/^PRODUCT="\([^"]*\)"$/\1/p' "$ROOT/scripts/install.sh")" # 產品名＝附帶的 tmux 在 fixed/ 裡的檔名（ECL-65）
if [ ! -x "$TMUXD/$TARCH/tmux" ] || [ ! -x "$TMUXD/$TARCH/disclaim" ]; then
  echo "還沒編附帶的 tmux 與啟動程式（找不到 $TMUXD/$TARCH/tmux 或 disclaim）：先跑 scripts/build-tmux.sh，再跑一次 make install-test。" >&2
  exit 1
fi

# 放在 /tmp：家的 API socket 與 tmux 的 socket 路徑有長度上限（約 103 字元），暫存資料夾的預設位置常常太長
B="$(dl_mktemp "${EVERGENIE_INSTALL_TEST_TMP:-/tmp}" hgi)" || { echo "建不出暫存資料夾（在 ${EVERGENIE_INSTALL_TEST_TMP:-/tmp} 底下）" >&2; exit 1; }
SOCKD="$B/s"
mkdir -p "$SOCKD" "$B/tmux"
WWW_PID=""
cleanup() {
  local f
  for f in "$B"/h*/.evergenie/app/current/bin/evergenie; do
    [ -x "$f" ] || continue
    for h in "$B"/h*/evergenie-*; do
      [ -d "$h" ] && env -i HOME="$(dirname "$h")" PATH=/usr/bin:/bin EVERGENIE_SERVICE_MODE=process "$f" service uninstall --home "$h" >/dev/null 2>&1
    done
  done
  pkill -f "serve --?home $B/" 2>/dev/null # 背景行程模式是 serve -home <家>
  [ -n "$WWW_PID" ] && kill "$WWW_PID" 2>/dev/null
  [ -S "$SOCKD/t.sock" ] && env -i PATH=/usr/bin:/bin "$TMUXD/$TARCH/tmux" -S "$SOCKD/t.sock" kill-server 2>/dev/null
  # 萬一服務開了秘書的 session（假 claude 在 tmux 裡會待著不走）：收掉暫存資料夾裡那個 tmux（預設 socket 名稱 hg）
  [ -S "$B/tmux/tmux-$(id -u)/hg" ] && env -i PATH=/usr/bin:/bin "$TMUXD/$TARCH/tmux" -S "$B/tmux/tmux-$(id -u)/hg" kill-server 2>/dev/null
  rm -rf "$B"
}
trap cleanup EXIT

pass=0 fail=0
say() { printf '%s\n' "$*"; }
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; echo "      --- 輸出（結束碼 ${RC:-?}）---"; printf '%s\n' "${OUT:-}" | sed 's/^/      /'; fi; }
freeport() { dl_freeport; }

WPORT="$(freeport)"
SRC="http://127.0.0.1:$WPORT"

say "── 準備：編 0.0.1（darwin-${ARCH}，帶附帶的 tmux；找新版的地方編進執行檔）"
OUT="$(env -u EVERGENIE_HOME -u EVERGENIE_ENGINE_BIN_DIR "$ROOT/scripts/dist.sh" --version 0.0.1 --arch "$ARCH" --out "$B/www" --source "$SRC/manifest.json" --tmux "$TMUXD" --notes "測試版" 2>&1)"
RC=$?
check "dist.sh 編好 0.0.1" '[ $RC = 0 ] && [ -s "$B/www/manifest.json" ]'
[ $RC = 0 ] || exit 1

# 清單的幾種壞法：檢查碼不符、看不懂、沒有這台晶片、說的大小比檔案大（像下載到一半斷掉）
python3 -I - "$B/www" "$ARCH" "$OTHER" <<'PY'
import copy, json, os, sys
www, arch, other = sys.argv[1:4]
m = json.load(open(os.path.join(www, "manifest.json"), encoding="utf-8"))
def put(name, mm):
    json.dump(mm, open(os.path.join(www, name), "w", encoding="utf-8"), ensure_ascii=False)
x = copy.deepcopy(m); x["assets"]["darwin-" + arch]["sha256"] = "0" * 64; put("badsha.json", x)
x = copy.deepcopy(m); x["assets"] = {"darwin-" + other: m["assets"]["darwin-" + arch]}; put("nochip.json", x)
x = copy.deepcopy(m); x["assets"]["darwin-" + arch]["size"] += 1000; put("cut.json", x)
open(os.path.join(www, "garbage.json"), "w").write("<html>502 Bad Gateway</html>\n")
PY
python3 -I -m http.server "$WPORT" --bind 127.0.0.1 --directory "$B/www" >"$B/www.log" 2>&1 &
WWW_PID=$!
disown "$WWW_PID" 2>/dev/null
for _ in $(seq 1 50); do curl -s -o /dev/null "$SRC/manifest.json" && break; sleep 0.2; done
OUT="$(curl -s "$SRC/manifest.json")"
check "本機 http 提供版本清單" 'grep -q "\"version\": *\"0.0.1\"" <<<"$OUT"'

# 系統內建工具的替身：/usr/bin、/bin、/usr/sbin、/sbin 都在，但拿掉 git、go、make、brew、tmux、python3（乾淨的 Mac 上那幾個不是沒有就是要另外裝）
SAFE="$B/safe"
dl_safebin "$SAFE"
OUT="$(env -i PATH="$SAFE" /bin/bash -c 'command -v git go make brew tmux python3 claude' 2>&1)"
check "安裝程式的 PATH 沒有 git、go、make、brew、tmux、python3、claude" '[ -z "$OUT" ]'

# 每個情境一個假 HOME（${FH}）：~/.local/bin/claude 是假的 Claude（auth status 看 ~/.claude.json 有沒有 oauthAccount），預設登入了。
N=0
newhome() {
  N=$((N + 1))
  FH="$B/h$N" TMPD="$B/tmp$N"
  mkdir -p "$FH/.local/bin" "$TMPD"
  dl_fake_claude "$FH"
  APP="$FH/.evergenie/app" HOMEDIR="$FH/evergenie-$(id -un)"
}
# TMUX_TMPDIR：萬一有秘書的 session 開起來，tmux 的 socket 也在暫存資料夾，碰不到各家真的 tmux（預設 socket 名字 hg 跟正式家一樣）。
# INST_EXTRA：這一次多給的環境變數（空白分開、值裡不能有空白），例如 EVERGENIE_INSTALL_TEST_FAULT=fail-board。
INST_EXTRA=""
inst() { # inst <install.sh 的參數…>：在 $FH 跑，結果在 OUT、RC
  # shellcheck disable=SC2086
  OUT="$(cd "$FH" && env -i HOME="$FH" USER="$(id -un)" LOGNAME="$(id -un)" PATH="$SAFE:$FH/.local/bin" TMPDIR="$TMPD" TMUX_TMPDIR="$B/tmux" LANG=en_US.UTF-8 \
    EVERGENIE_SERVICE_MODE=process EVERGENIE_INSTALL_BOARD_WAIT=90 $INST_EXTRA /bin/bash "$ROOT/scripts/install.sh" "$@" 2>&1)"
  RC=$?
}
serve_running() { # 這個假 HOME 的 serve 還在跑（剛收掉的給 5 秒停）
  local i
  for i in 1 2 3 4 5 6 7 8 9 10; do
    pgrep -f "serve --?home $FH/" >/dev/null 2>&1 || return 1
    sleep 0.5
  done
  return 0
}
hgx() { # hgx <指令…>：跟安裝後的使用者一樣，從 ~/.local/bin/evergenie 跑（不給 --home，靠指標檔找家）
  OUT="$(cd "$FH" && env -i HOME="$FH" USER="$(id -un)" PATH="$SAFE:$FH/.local/bin" TMUX_TMPDIR="$B/tmux" LANG=en_US.UTF-8 EVERGENIE_SERVICE_MODE=process "$FH/.local/bin/evergenie" "$@" 2>&1)"
  RC=$?
}
# 失敗時刻意留下的日誌（~/Library/Logs/evergenie-install-*）不算「留了半套」，另外檢查
snap() { (cd "$FH" && find . | grep -Ev '^\./Library(/Logs(/.*)?)?$' | LC_ALL=C sort); }
# kept_logs：訊息裡「日誌留在 <路徑>/。」的路徑真的在假 HOME 的 ~/Library/Logs/evergenie-install-* 底下、裡面有日誌檔
kept_logs() {
  local p
  p="$(sed -n 's/.*日誌留在 \(.*\)\/。.*/\1/p' <<<"$OUT" | tail -1)"
  case "$p" in "$FH/Library/Logs/evergenie-install-"*) ;; *) return 1 ;; esac
  [ -d "$p" ] && [ -n "$(find "$p" -type f -name '*.log' 2>/dev/null)" ]
}
tmp_empty() { [ -z "$(ls -A "$TMPD")" ]; }
# 服務起來過的家不刪、整個搬到旁邊（install.sh 開頭；DEV-155 最後審查）：kept_at 是訊息裡「整個搬到 <路徑>」的路徑
kept_at() { sed -n 's/.*整個搬到 \([^（。]*\).*/\1/p' <<<"$OUT" | tail -1; }
BAKRE='\.bak-[0-9]{8}(-[0-9]{4})?-install-unfinished(-[0-9]+)?$'
kept_ok() { # kept_ok <搬去的家>：照備份規則的名字、在原本的家旁邊、是那個家（有 home.toml）；狀態資料夾搬到同一個尾巴；訊息說了怎麼拿回來
  local k="$1" st
  [[ "$k" == "$HOMEDIR".bak-* ]] && [[ "$k" =~ $BAKRE ]] && [ -f "$k/home.toml" ] || return 1
  st="$FH/.evergenie/state/$(basename "$k")"
  [ -d "$st/runtime" ] && grep -qF "狀態資料夾搬到 $st" <<<"$OUT" && grep -qF "$k 改回 ${HOMEDIR}、$st 改回 $FH/.evergenie/state/$(basename "$HOMEDIR")" <<<"$OUT"
}
drop_kept() { # drop_kept <搬去的家>：測試自己收掉這份備份（與狀態資料夾那份），空了的上層一起收，好比對「除了備份，沒留半套」
  case "$1" in "$HOMEDIR".bak-*) ;; *) return 0 ;; esac
  rm -rf "$1" "$FH/.evergenie/state/$(basename "$1")"
  rmdir "$FH/.evergenie/state" "$FH/.evergenie" 2>/dev/null
  return 0
}

say "── 1. 裝好：預設位置、本機 http 的清單"
newhome
# 以前安裝失敗留下的日誌：這個名字、超過 30 天的，裝好時順手刪掉；最近的、別的名字的不動
LOGS="$FH/Library/Logs"
mkdir -p "$LOGS/evergenie-install-20200101-000000" "$LOGS/evergenie-install-$(date +%Y%m%d)-000000" "$LOGS/別的程式-install-20200101"
for d in "$LOGS"/*; do echo 日誌 >"$d/serve.log"; done
touch -t 202001010000 "$LOGS/evergenie-install-20200101-000000" "$LOGS/別的程式-install-20200101"
PORT="$(freeport)"
inst --name 測試 --port "$PORT" --source "$SRC/manifest.json"
check "1a 結束碼 0、印「裝好了」、版本與看板網址" '[ $RC = 0 ] && grep -q "裝好了（版本 0.0.1）" <<<"$OUT" && grep -q "看板網址：http://127.0.0.1:$PORT/" <<<"$OUT"'
check "1a 服務起來之後請它同步了一次角色包（這一版沒有角色包：不算失敗、沒有「還沒弄好」的注意）" 'grep -q "^== 下載角色的頭像與衣服" <<<"$OUT" && ! grep -q "角色的頭像與衣服這次還沒弄好" <<<"$OUT"'
[ $RC = 0 ] || { echo "裝不起來，後面不測了"; echo "通過 ${pass}，失敗 $((fail + 1))"; exit 1; }
check "1a2 超過 30 天的舊安裝日誌刪了；最近的、別的名字的還在" '[ ! -e "$LOGS/evergenie-install-20200101-000000" ] && [ -f "$LOGS/evergenie-install-$(date +%Y%m%d)-000000/serve.log" ] && [ -f "$LOGS/別的程式-install-20200101/serve.log" ]'
check "1b 程式在 ~/.evergenie/app/versions/0.0.1、current 指過去、包裡的 tmux 在 fixed/、bin/ 裡沒有" '[ "$(readlink "$APP/current")" = versions/0.0.1 ] && [ -x "$APP/versions/0.0.1/fixed/${PRODUCT}" ] && [ ! -e "$APP/versions/0.0.1/bin/tmux" ] && [ "$("$APP/current/bin/evergenie" version)" = 0.0.1 ]'
check "1b2 固定位置 ~/.evergenie/app/fixed/ 有產品名的 tmux 與啟動程式（ECL-65、UPD-15），跟編好的一模一樣、沒留暫存檔" '[ -x "$APP/fixed/${PRODUCT}" ] && [ -x "$APP/fixed/disclaim" ] && cmp -s "$APP/fixed/${PRODUCT}" "$TMUXD/$TARCH/tmux" && cmp -s "$APP/fixed/disclaim" "$TMUXD/$TARCH/disclaim" && [ "$(ls -A "$APP/fixed" | LC_ALL=C sort | tr "\n" " ")" = "$(printf "%s\n" disclaim "${PRODUCT}" | LC_ALL=C sort | tr "\n" " ")" ]'
RESCUE_F="$FH/Applications/evergenie 修復.command"
check "1b3 放了可以點兩下的「evergenie 修復」：可執行、寫著這個家、叫的是 current/bin 的程式（升級後還是對的）；輸出告訴主人怎麼找" '[ -x "$RESCUE_F" ] && grep -q "^export EVERGENIE_HOME=.$HOMEDIR.\$" "$RESCUE_F" && grep -q "^EXE=.$APP/current/bin/evergenie.\$" "$RESCUE_F" && grep -q "rescue --ask" "$RESCUE_F" && grep -q "出問題時：按 ⌘＋空白鍵搜尋「evergenie 修復」" <<<"$OUT"'
check "1c ~/.local/bin/evergenie 連到 current/bin/evergenie" '[ "$(readlink "$FH/.local/bin/evergenie")" = "$APP/current/bin/evergenie" ]'
check "1d 指標檔在程式資料夾、寫著這個家" 'grep -qx "home = \"$HOMEDIR\"" "$APP/evergenie.local.toml"'
check "1e 看板首頁 200" '[ "$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/")" = 200 ]'
SERVE_PID="$(tr -d ' \n' <"$FH/.evergenie/state/evergenie-$(id -un)/runtime/serve.pid" 2>/dev/null)"
OUT="$(ps -ww -o command= -p "${SERVE_PID:-0}" 2>/dev/null)"
check "1f 服務跑的是 current/bin/evergenie" '[[ "$OUT" == "$APP/current/bin/evergenie serve "*"home $HOMEDIR" ]]'
check "1g home.toml 的第一個帳號帶了帳號設定（預設登入的 ~/.claude）" 'grep -q "^\[accounts.main.engine_config\]" "$HOMEDIR/home.toml" && grep -q "^config_dir = \"~/.claude\"" "$HOMEDIR/home.toml" && grep -q "^default_login = true" "$HOMEDIR/home.toml"'
hgx doctor --json
DOC="$OUT"
check "1h doctor（不給 --home，靠指標檔）跑得起來" '[ $RC = 0 ] || [ $RC = 1 ]'
OUT="$(dl_doctor_reds <<<"$DOC" 2>&1)"
check "1i doctor 沒有紅字" '[ -z "$OUT" ]'
# 看板回 200 時引擎可能還在起來（serve 起好才開引擎）：「引擎沒有在跑」就再等一下，最多 30 秒
for _ in $(seq 1 30); do
  hgx preflight
  grep -q "引擎沒有在跑" <<<"$OUT" || break
  sleep 1
done
PRE="$OUT"
grep -q "引擎沒有在跑" <<<"$PRE" && tail -20 "$FH/.evergenie/state/evergenie-$(id -un)/runtime/logs/serve.log" 2>/dev/null | sed 's/^/      serve.log｜/'
check "1j preflight 的 tmux 是固定位置那份、權限視窗記在它名下（ECL-65）" 'grep -qF "✔ tmux（$APP/fixed/${PRODUCT}（跟著程式一起裝的；系統問資料夾權限時記在這一支名下））" <<<"$PRE"'
check "1k preflight 的帳號項：登入了、不是「帳號設定不完整」" 'grep -q "✔ 帳號 main 已登入（$FH/.claude）" <<<"$PRE" && ! grep -q "帳號設定不完整" <<<"$PRE"'
check "1n preflight 不查 git（下載版不用 git，沒裝也不會 ✘；DEV-155 S6）" '! grep -qE "(✔|⚠|✘) git" <<<"$PRE"'
# 跟引擎一樣：開 server 的那一次經啟動程式（設得起來就什麼都不印），之後直接叫 tmux
OUT="$(env -i PATH=/usr/bin:/bin TERM=xterm-256color "$APP/fixed/disclaim" "$APP/fixed/${PRODUCT}" -S "$SOCKD/t.sock" new-session -d -s t "sleep 30" 2>&1 &&
  env -i PATH=/usr/bin:/bin "$APP/fixed/${PRODUCT}" -S "$SOCKD/t.sock" has-session -t t 2>&1)"
RC=$?
env -i PATH=/usr/bin:/bin "$APP/fixed/${PRODUCT}" -S "$SOCKD/t.sock" kill-server 2>/dev/null
check "1l 固定位置的 tmux 經啟動程式開得起 session、沒印照平常方式開的那一行（乾淨環境、自己的 socket）" '[ $RC = 0 ] && [ -z "$OUT" ]'
OUT="$(ls -A "$TMPD")"
check "1m 暫存資料夾收乾淨了" '[ -z "$OUT" ]'

say "── 1n. 公開角色：卡跟著程式（PRS-25），圖走角色包（本機 http 的測試包），家裡一個檔都不寫"
STATE="$FH/.evergenie/state/evergenie-$(id -un)"
CARDS="$APP/versions/0.0.1/packages/official/personas/cards"
# 公開角色的名單不寫死：repo 的 cards/ 有哪幾張，新家就該列出哪幾位（PRS-25、PRS-26）
WANT_CARDS="$(dl_official_cards "$ROOT")" # 跟 dist.sh 收檔一樣（被忽略的不算）
# 跟 release-e2e 的 home_list 一樣不比服務自己會寫的訊息、編號計數與衣櫃狀態（那些是服務照常在寫，跟角色包無關）
home_files() { (cd "$HOMEDIR" && find . -type f ! -path './vault/messages/*' ! -path './vault/.counters.toml' ! -path './vault/pkg/closet/state/*' -print0 | LC_ALL=C sort -z | xargs -0 shasum -a 256); }
official_list() { # 角色庫列出的官方角色代號（空白隔開、排序）
  hgx personas list --json
  OUT="$(python3 -I -c 'import json,sys; print(" ".join(sorted(d["slug"] for d in (json.loads(l) for l in sys.stdin if l.strip()) if d.get("source") == "official")))' <<<"$OUT" 2>&1)"
}
avatar_code() { curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/avatars/persona/$1.png"; }
official_list
check "1n 角色庫（personas.list）裝好就有 repo 的 cards/ 那幾位公開角色（${OUT}）" '[ -n "$WANT_CARDS" ] && [ "$OUT" = "$WANT_CARDS" ]'
hgx personas show kaguya
check "1o 卡的內容是程式資料夾裡那張（versions/0.0.1/…/personas/cards/）" '[ $RC = 0 ] && grep -q "輝夜" <<<"$OUT" && [ -f "$CARDS/kaguya.md" ] && [ "$OUT" = "$(cat "$CARDS/kaguya.md")" ]'
hgx personas style
check "1o 說話層是程式內建的（強度 c；家裡沒有主人自己的 personas/_style.md）" '[ $RC = 0 ] && grep -q "目前的說話層強度：c" <<<"$OUT" && grep -q "沒有 personas/_style.md" <<<"$OUT" && [ ! -e "$HOMEDIR/personas/_style.md" ]'
check "1p 家裡與狀態資料夾都沒有官方卡（只在程式資料夾）" '[ -z "$(ls "$HOMEDIR/personas" 2>/dev/null | grep -v "^mine$")" ] && [ -z "$(ls "$STATE/personas/"*.md 2>/dev/null)" ]'
OUT="$(avatar_code kaguya)"
check "1q 還沒有圖：頭像回 404（看板改用名字的第一個字，不會是破圖）" '[ "$OUT" = 404 ]'
hgx personas pack sync
check "1r 這一版的指標 url 空：sync 結束 0、白話說還沒有公開的角色包" '[ $RC = 0 ] && grep -q "還沒有公開的角色包" <<<"$OUT"'
# 測試用的角色包：假的來源家（testdata 的家＋公開 kaguya），用裝好的 evergenie 建包，放在本機 http。
# 來源家沒有服務在跑：要加 --no-records 才建（只有圖與卡）；不知道哪些資料夾是隱藏版，衣服資料夾整個不收（PRS-4）。
# 裝好的程式旁邊有官方衣櫃（照片紀錄 closet.photo 宣告 file_field）：連不到服務時 --no-records 也不建（DEV-160 審查 M2）；
# 要建只有圖與卡的測試包，就讓這一次的建包看一個沒有套件的官方資料夾（EVERGENIE_OFFICIAL_PACKAGES_DIR）。
# 帶紀錄、衣服、隱藏版的完整流程在 clean-install-test.sh。
PUB="$B/pub"
cp -R "$ROOT/cmd/evergenie/testdata/home" "$PUB"
printf '\n[admin]\npublic_personas = ["kaguya"]\n' >>"$PUB/home.toml"
mkdir -p "$PUB/personas/avatars" "$PUB/personas/wardrobe/kaguya"
{ cat "$ROOT/packages/official/personas/cards/kaguya.md"; echo "角色包版本的一句"; } >"$PUB/personas/kaguya.md"
printf 'png-kaguya' >"$PUB/personas/avatars/kaguya.png"
printf 'png-kaguya-adult' >"$PUB/personas/avatars/kaguya@adult.png"
printf 'png-kaguya-day' >"$PUB/personas/wardrobe/kaguya/day.png"
OUT="$(env -i HOME="$FH" PATH="$SAFE" LANG=en_US.UTF-8 "$APP/current/bin/evergenie" personas pack build --home "$PUB" --out "$B/www/pack" --version 2026.11.13 --json 2>&1)"; RC=$?
check "1s 來源家的服務沒在跑：不加 --no-records 不建包，說要先啟動服務" '[ $RC != 0 ] && grep -q "連不到這個家的 EverGenie 服務" <<<"$OUT" && grep -q -- "--no-records" <<<"$OUT" && [ ! -e "$B/www/pack" ]'
OUT="$(env -i HOME="$FH" PATH="$SAFE" LANG=en_US.UTF-8 "$APP/current/bin/evergenie" personas pack build --home "$PUB" --out "$B/www/pack" --version 2026.11.13 --no-records --json 2>&1)"; RC=$?
check "1s 有照片紀錄的衣櫃時服務沒在跑：加 --no-records 也不建包，說要先啟動服務" '[ $RC != 0 ] && grep -q "closet.photo" <<<"$OUT" && grep -q "evergenie service start" <<<"$OUT" && [ ! -e "$B/www/pack" ]'
mkdir -p "$B/no-official"
OUT="$(env -i HOME="$FH" PATH="$SAFE" LANG=en_US.UTF-8 EVERGENIE_OFFICIAL_PACKAGES_DIR="$B/no-official" "$APP/current/bin/evergenie" personas pack build --home "$PUB" --out "$B/www/pack" --version 2026.11.13 --no-records --json 2>&1)"; RC=$?
PACK_SHA="$(python3 -I -c 'import json,sys; print(json.load(sys.stdin)["manifest_sha256"])' <<<"$OUT" 2>/dev/null)"
check "1s 加 --no-records 建出測試用的角色包（衣服資料夾整個不收、有一行總結）" '[ $RC = 0 ] && [ -n "$PACK_SHA" ] && grep -q "衣服資料夾整個不收" <<<"$OUT" && grep -q "角色包 2026.11.13：1 位角色" <<<"$OUT"'
printf 'version = "2026.11.13"\nurl = "%s/pack/m/2026.11.13.json"\nsha256 = "%s"\n' "$SRC" "$(printf '0%.0s' $(seq 1 64))" >"$B/ptr-bad.toml"
printf 'version = "2026.11.13"\nurl = "%s/pack/m/2026.11.13.json"\nsha256 = "%s"\n' "$SRC" "$PACK_SHA" >"$B/ptr-ok.toml"
hgsync() { # hgsync <指標檔>：跟 hgx 一樣從 ~/.local/bin/evergenie 跑，換掉角色包指標（服務在跑：指令把指標交給服務、由服務同步）
  OUT="$(cd "$FH" && env -i HOME="$FH" USER="$(id -un)" PATH="$SAFE:$FH/.local/bin" LANG=en_US.UTF-8 EVERGENIE_SERVICE_MODE=process EVERGENIE_PERSONAS_PACK_POINTER="$1" "$FH/.local/bin/evergenie" personas pack sync --json 2>&1)"
  RC=$?
}
HOME_BEFORE="$(home_files)"
hgsync "$B/ptr-bad.toml"
check "1t 檢查碼對不上：sync 結束 1、狀態 failed、什麼都沒換" '[ $RC = 1 ] && grep -q "\"status\":\"failed\"" <<<"$OUT" && [ ! -e "$STATE/personas/avatars" ] && [ ! -e "$STATE/personas/kaguya.md" ]'
hgsync "$B/ptr-ok.toml"
check "1u 本機 http 的角色包：sync 結束 0、狀態 synced" '[ $RC = 0 ] && grep -q "\"status\":\"synced\"" <<<"$OUT"'
check "1v 圖在狀態資料夾（家外面），名字帶成人字樣的隱藏版與衣服資料夾（--no-records 不收）沒進來；登記在狀態資料夾" '[ "$(cat "$STATE/personas/avatars/kaguya.png" 2>/dev/null)" = png-kaguya ] && [ ! -e "$STATE/personas/wardrobe" ] && [ ! -e "$STATE/personas/avatars/kaguya@adult.png" ] && grep -q "^version = \"2026.11.13\"" "$STATE/personas/pack-registry.toml"'
check "1w 第一次同步不算新角色（不會來打招呼）" '! grep -q "new_cards" <<<"$OUT"'
OUT="$(diff <(printf '%s\n' "$HOME_BEFORE") <(home_files))"
check "1x 同步前後（含失敗那次）家裡的檔案清單與內容完全一樣" '[ -n "$HOME_BEFORE" ] && grep -q "home.toml" <<<"$HOME_BEFORE" && [ -z "$OUT" ]'
OUT="$(avatar_code kaguya)"
check "1y 有圖了：頭像回 200" '[ "$OUT" = 200 ]'
hgx personas show kaguya
check "1z 卡換成角色包那張（狀態資料夾的優先）" '[ $RC = 0 ] && grep -q "角色包版本的一句" <<<"$OUT" && [ -f "$STATE/personas/kaguya.md" ]'
hgx personas show athena
check "1z 其他幾位還是程式帶的卡（狀態資料夾沒有她們的卡）" '[ $RC = 0 ] && [ "$OUT" = "$(cat "$CARDS/athena.md")" ] && (for s in $WANT_CARDS; do [ "$s" = kaguya ] || [ ! -e "$STATE/personas/$s.md" ] || exit 1; done)'
official_list
check "1z 同步後角色庫照樣列同樣那幾位、不重複（${OUT}）" '[ "$OUT" = "$WANT_CARDS" ]'

say "── 2. 同一台再裝一次（已經裝過）：結束 14、什麼都不動"
BEFORE="$(cd "$APP" && find . | LC_ALL=C sort)"
inst --home "$FH/另一個家" --source "$SRC/manifest.json" --port "$(freeport)"
check "2a 結束 14、說已經裝過、要升級用 upgrade" '[ $RC = 14 ] && grep -q "已經裝過" <<<"$OUT" && grep -q "upgrade" <<<"$OUT"'
check "2b 程式資料夾沒變、沒有建另一個家" '[ "$(cd "$APP" && find . | LC_ALL=C sort)" = "$BEFORE" ] && [ ! -e "$FH/另一個家" ]'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1
sleep 1
check "2c 收掉測試的服務" '! kill -0 "${SERVE_PID:-0}" 2>/dev/null'

# 失敗的情境：每個都在新的假 HOME，跑完之後假 HOME 跟跑之前一模一樣（沒留半套）、暫存資料夾也收乾淨
fails() { # fails <名稱> <結束碼> <訊息要有的字> <install.sh 的參數…>
  local name="$1" code="$2" want="$3" before after
  shift 3
  before="$(snap)"
  inst "$@"
  after="$(snap)"
  check "${name}：結束 ${code}、說「${want}」" '[ $RC = '"$code"' ] && grep -q "沒裝成功（結束碼 '"$code"'）" <<<"$OUT" && grep -q "$want" <<<"$OUT"'
  check "${name}：沒留半套" '[ "$before" = "$after" ] && tmp_empty'
}
fails_kept() { # fails_kept <名稱> <結束碼> <訊息要有的字> <install.sh 的參數…>：服務起來之後才失敗：家不刪、整個搬到旁邊；除了這份備份，沒留半套
  local name="$1" code="$2" want="$3" before after k
  shift 3
  before="$(snap)"
  inst "$@"
  k="$(kept_at)"
  check "${name}：結束 ${code}、說「${want}」" '[ $RC = '"$code"' ] && grep -q "沒裝成功（結束碼 '"$code"'）" <<<"$OUT" && grep -q "$want" <<<"$OUT"'
  check "${name}：服務起來過的家沒刪，整個搬到旁邊（<家>.bak-<日期>-install-unfinished）、狀態資料夾跟著搬、訊息說了在哪與怎麼拿回來" 'kept_ok "$k" && grep -qF "注意：$HOMEDIR 這個家開過服務" <<<"$OUT"'
  drop_kept "$k"
  after="$(snap)"
  check "${name}：除了搬到旁邊的家，沒留半套" '[ "$before" = "$after" ] && tmp_empty'
}
say "── 3. 失敗：每一種都白話、有結束碼、沒留半套"
newhome
fails "3a 檢查碼不符" 23 "檢查碼對不上" --source "$SRC/badsha.json" --port "$(freeport)"
fails "3b 清單讀不到（404）" 20 "讀不到版本清單" --source "$SRC/nope.json"
fails "3c 清單看不懂" 20 "版本清單看不懂" --source "$SRC/garbage.json"
fails "3d 清單沒有這台的晶片" 21 "darwin-$ARCH" --source "$SRC/nochip.json"
fails "3e 下載到一半斷掉" 22 "斷掉" --source "$SRC/cut.json"
fails "3f 下載點還是佔位網址（.invalid）：不連網就停" 20 "還沒有公開的下載點" --source "https://example.invalid/evergenie/releases/latest/manifest.json"
fails "3g 建家失敗（下載、解開之後才失敗，程式也收掉）" 30 "建立你的資料夾失敗" --source "$SRC/manifest.json" --timezone 火星/奧林帕斯
mkdir -p "$HOMEDIR" && echo 別動 >"$HOMEDIR/我的東西.txt"
fails "3h 家已經有東西" 13 "已經有東西了" --source "$SRC/manifest.json"
check "3h 家裡原本的東西還在" '[ "$(cat "$HOMEDIR/我的東西.txt")" = 別動 ]'
rm -rf "$HOMEDIR"
rm -f "$FH/.claude.json"
fails "3i Claude 沒登入" 12 "還沒登入" --source "$SRC/manifest.json"
rm -f "$FH/.local/bin/claude"
fails "3j 沒有 Claude 的指令工具" 11 "找不到 Claude 的指令工具" --source "$SRC/manifest.json"
# 用法不對：跟這台電腦的狀態無關，一律結束 2（一樣在假 HOME 跑，不碰真的 ~/Library/LaunchAgents）
inst --bogus
check "3k 不認得的選項：結束 2、先印「沒裝成功（結束碼 2）」" '[ $RC = 2 ] && grep -q "沒裝成功（結束碼 2）：不認得的選項" <<<"$OUT"'
before="$(snap)"
inst --source ftp://x/m.json --skip-claude-check
check "3l --source 不是 http(s)／file：結束 2、什麼都沒建" '[ $RC = 2 ] && grep -q "沒裝成功（結束碼 2）：--source" <<<"$OUT" && [ "$before" = "$(snap)" ]'
inst --source http://example.com/m.json --skip-claude-check
check "3l1 --source 是別台電腦的 http（只收本機的 http）：結束 2、訊息只建議 https、什麼都沒建" '[ $RC = 2 ] && grep -q "沒裝成功（結束碼 2）：--source 要是 https:// 開頭的網址" <<<"$OUT" && [ "$before" = "$(snap)" ]'
inst --port 99999
check "3l2 port 太大：結束 2" '[ $RC = 2 ] && grep -q "沒裝成功（結束碼 2）：--port" <<<"$OUT"'
inst --home "$FH/a/../b"
check "3l3 路徑有 .. 這一層：結束 2、說要寫完整的路徑" '[ $RC = 2 ] && grep -q "不能有 . 或 .. 這一層" <<<"$OUT" && [ ! -e "$FH/b" ] && [ "$before" = "$(snap)" ]'
# 家是捷徑（符號連結）：收拾時看不準裡面有沒有東西（DEV-155 最後審查），一開始就擋；家放在程式資料夾裡：收程式時會連家一起刪，也擋
mkdir -p "$FH/真的家" && ln -s "$FH/真的家" "$FH/捷徑家"
before="$(snap)"
inst --home "$FH/捷徑家" --skip-claude-check --source "$SRC/manifest.json"
check "3l4 --home 是捷徑：結束 2、說不能是捷徑、什麼都沒建" '[ $RC = 2 ] && grep -q "沒裝成功（結束碼 2）：--home 不能是捷徑" <<<"$OUT" && [ "$before" = "$(snap)" ] && [ -z "$(ls -A "$FH/真的家")" ]'
inst --home "$FH/.evergenie/app/家" --skip-claude-check --source "$SRC/manifest.json"
check "3l5 家放在程式資料夾裡：結束 2、說要分開放、什麼都沒建" '[ $RC = 2 ] && grep -q "沒裝成功（結束碼 2）：程式資料夾.*跟家.*一個在另一個裡面" <<<"$OUT" && [ "$before" = "$(snap)" ]'
rm -rf "$FH/真的家" "$FH/捷徑家"

say "── 3b. 後面幾步才失敗：主人原本的 ~/.local/bin 不動（只收這次建的連結）、服務收掉、服務起來過的家搬到旁邊"
newhome
echo 我的工具 >"$FH/.local/bin/我的工具"
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=fail-board"
fails_kept "3m 看板沒起來" 32 "看板一直沒有起來" --source "$SRC/manifest.json" --port "$(freeport)"
check "3m ~/.local/bin 原本的 claude 與工具都在、服務收掉了" '[ -x "$FH/.local/bin/claude" ] && [ "$(cat "$FH/.local/bin/我的工具")" = 我的工具 ] && ! serve_running'
check "3m 訊息裡的日誌位置真的在、裡面有日誌（收拾之前複製出來的）" 'kept_logs'
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=fail-service"
fails_kept "3n 裝成服務失敗（服務其實起來了，也要收）" 31 "裝成服務失敗" --source "$SRC/manifest.json" --port "$(freeport)"
check "3n 服務收掉了" '! serve_running'
mkdir -p "$FH/Library/LaunchAgents" && echo '<plist>別的家</plist>' >"$FH/Library/LaunchAgents/com.evergenie.serve.plist"
INST_EXTRA="EVERGENIE_LAUNCH_AGENTS_DIR=$FH/Library/LaunchAgents"
fails "3o 這台已經有一個家用了服務名稱" 14 "已經有一個 evergenie 的家裝成服務了" --source "$SRC/manifest.json" --port "$(freeport)"
check "3o 原本的服務設定沒被動" '[ "$(cat "$FH/Library/LaunchAgents/com.evergenie.serve.plist")" = "<plist>別的家</plist>" ]'
rm -rf "$FH/Library"

# 被中斷（TERM）：等到「等看板」那一步（服務、連結都建好了）才送
before="$(snap)"
(cd "$FH" && exec env -i HOME="$FH" USER="$(id -un)" LOGNAME="$(id -un)" PATH="$SAFE:$FH/.local/bin" TMPDIR="$TMPD" TMUX_TMPDIR="$B/tmux" LANG=en_US.UTF-8 \
  EVERGENIE_SERVICE_MODE=process EVERGENIE_INSTALL_BOARD_WAIT=90 EVERGENIE_INSTALL_TEST_FAULT=hang-board /bin/bash "$ROOT/scripts/install.sh" --source "$SRC/manifest.json" --port "$(freeport)" >"$B/term.out" 2>&1) &
TPID=$!
for _ in $(seq 1 600); do grep -q "== 6/6" "$B/term.out" 2>/dev/null && break; sleep 0.2; done
kill -TERM "$TPID" 2>/dev/null
wait "$TPID"; RC=$?
OUT="$(cat "$B/term.out")"
K="$(kept_at)"
check "3p 被中斷（TERM）：結束 1、說被中斷了" '[ $RC = 1 ] && grep -q "沒裝成功（結束碼 1）：被中斷了" <<<"$OUT"'
check "3p 被中斷：服務起來過的家沒刪，整個搬到旁邊、訊息說了在哪" 'kept_ok "$K"'
drop_kept "$K"
after="$(snap)"
check "3p 被中斷：除了搬到旁邊的家，沒留半套、服務收掉了" '[ "$before" = "$after" ] && tmp_empty && ! serve_running'
check "3p 被中斷：訊息裡的日誌位置真的在、裡面有日誌" 'kept_logs'

# 被強制停掉（kill -9，trap 來不及跑）：留下 .install-unfinished；同一行再跑一次，先收掉上次的再裝好
P3="$(freeport)"
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=kill-after-service"
inst --source "$SRC/manifest.json" --port "$P3"
OLD_PID="$(cat "$FH/.evergenie/state/evergenie-$(id -un)/runtime/serve.pid" 2>/dev/null)"
check "3q 被 kill -9：留下 .install-unfinished（記了服務、這次建的資料夾、建家當時家的指紋）" '[ $RC = 137 ] && grep -q "^service" "$APP/.install-unfinished" && grep -q "created" "$APP/.install-unfinished" && grep -Eq "^home$(printf "\t")$HOMEDIR$(printf "\t")[0-9a-f]{64}\$" "$APP/.install-unfinished"'
INST_EXTRA=""
inst --source "$SRC/manifest.json" --port "$P3"
check "3q 同一行再跑：先收掉上次的、再裝好、上次的服務停了" '[ $RC = 0 ] && grep -q "上次的安裝沒有跑完" <<<"$OUT" && grep -q "裝好了" <<<"$OUT" && [ ! -e "$APP/.install-unfinished" ] && ! kill -0 "${OLD_PID:-0}" 2>/dev/null'
check "3q 上次的家開過服務：沒刪，整個搬到旁邊；最後的「注意：」也有說" 'kept_ok "$(kept_at)" && [ "$(sed -n "/^裝好了/,\$p" <<<"$OUT" | grep -c "注意：上次沒裝完的家 $HOMEDIR 開過服務")" = 1 ]'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 3q 的變形（DEV-155 最後審查）：服務起來後被 kill -9 → 主人只在套件頁記了東西（vault/pkg/<套件>/，家的指紋不算它，看不出被用過）
# → 同一行重跑：家不能刪，整個搬到旁邊、訊息說在哪；旁邊已經有同名的備份就換一個名字，不蓋掉
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=kill-after-service"
inst --source "$SRC/manifest.json" --port "$(freeport)"
OLD_PID="$(cat "$FH/.evergenie/state/evergenie-$(id -un)/runtime/serve.pid" 2>/dev/null)"
mkdir -p "$HOMEDIR/vault/pkg/memo" && echo 只在套件頁記的 >"$HOMEDIR/vault/pkg/memo/主人記的.md"
OLDBAK="$HOMEDIR.bak-$(date +%Y%m%d)-install-unfinished"
mkdir -p "$OLDBAK" && echo 之前的備份 >"$OLDBAK/之前的備份.txt"
INST_EXTRA=""
inst --source "$SRC/manifest.json" --port "$(freeport)"
K="$(kept_at)"
check "3q2 只在套件頁記了東西再重跑：裝好；家整個搬到旁邊、套件頁記的還在、訊息說了在哪與怎麼拿回來" '[ $RC = 0 ] && grep -q "上次的安裝沒有跑完" <<<"$OUT" && kept_ok "$K" && [ "$(cat "$K/vault/pkg/memo/主人記的.md" 2>/dev/null)" = 只在套件頁記的 ] && grep -qF "注意：上次沒裝完的家 $HOMEDIR 開過服務，可能已經記了東西，所以沒有刪，整個搬到 $K" <<<"$OUT"'
check "3q2 旁邊已經有同名的備份：換一個名字，原本那份沒被蓋" '[ -n "$K" ] && [ "$K" != "$OLDBAK" ] && [ "$(cat "$OLDBAK/之前的備份.txt")" = 之前的備份 ] && [ ! -e "$OLDBAK/home.toml" ]'
check "3q2 新的家是新建的、上次的服務停了、紀錄收掉" '[ -f "$HOMEDIR/home.toml" ] && [ ! -e "$HOMEDIR/vault/pkg/memo/主人記的.md" ] && ! kill -0 "${OLD_PID:-0}" 2>/dev/null && [ ! -e "$APP/.install-unfinished" ]'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 同上，但搬不開（測試：假裝搬不開）：結束 14、什麼都不動（家、套件頁記的、程式、紀錄都在；停掉的服務重開）
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=kill-after-service"
inst --source "$SRC/manifest.json" --port "$(freeport)"
mkdir -p "$HOMEDIR/vault/pkg/memo" && echo 只在套件頁記的 >"$HOMEDIR/vault/pkg/memo/主人記的.md"
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=aside-fail"
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3q3 搬不開：結束 14、說搬不開、什麼都沒動、服務照樣開著" '[ $RC = 14 ] && grep -q "沒裝成功（結束碼 14）：上次沒裝完的家 $HOMEDIR 開過服務.*搬不開" <<<"$OUT" && [ "$(cat "$HOMEDIR/vault/pkg/memo/主人記的.md")" = 只在套件頁記的 ] && [ -x "$APP/current/bin/evergenie" ] && grep -q "^service" "$APP/.install-unfinished" && [ -z "$(ls -d "$HOMEDIR".bak-* 2>/dev/null)" ] && serve_running'
INST_EXTRA=""
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 這次裝到服務之後才失敗，家又搬不開（測試：看板沒起來＋假裝搬不開）：家與狀態資料夾留在原處、不刪；程式與服務照樣收掉
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=fail-board,aside-fail"
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3m2 看板沒起來、家又搬不開：結束 32、說留在原處；家與狀態資料夾都在、沒有備份、程式與服務收掉" '[ $RC = 32 ] && grep -qF "注意：$HOMEDIR 這個家開過服務，可能已經記了東西，所以沒有刪；也沒辦法搬到旁邊" <<<"$OUT" && [ -f "$HOMEDIR/home.toml" ] && [ -d "$FH/.evergenie/state/$(basename "$HOMEDIR")/runtime" ] && [ -z "$(ls -d "$HOMEDIR".bak-* 2>/dev/null)" ] && [ ! -e "$APP" ] && ! serve_running'
INST_EXTRA=""
rm -rf "$HOMEDIR" "$FH/.evergenie"

# 這次裝到服務之後才失敗，服務又停不掉（測試：看板沒起來＋假裝停不掉）：什麼都不收（程式、家、紀錄都留著，服務照樣在跑）；
# 同一行重跑：照紀錄停掉上次的服務、家搬到旁邊、裝好
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=fail-board,svc-stop-fail"
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3m3 看板沒起來、服務又停不掉：結束 32、說服務停不掉都留著；程式、家、紀錄都在、服務還在跑" '[ $RC = 32 ] && grep -q "注意：這次裝的服務停不掉" <<<"$OUT" && [ -x "$APP/current/bin/evergenie" ] && [ -f "$HOMEDIR/home.toml" ] && grep -q "^service" "$APP/.install-unfinished" && serve_running'
INST_EXTRA=""
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3m3 同一行再跑：停掉上次的服務、家搬到旁邊、裝好、紀錄收掉" '[ $RC = 0 ] && grep -q "上次的安裝沒有跑完" <<<"$OUT" && kept_ok "$(kept_at)" && [ ! -e "$APP/.install-unfinished" ]'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 連結建好後才被 kill -9，~/.local/bin 原本不存在：重跑時只收連結與這次的資料夾；兩次之間別人放進 ~/.local、~/.evergenie 的東西留著
newhome
rm -rf "$FH/.local"
P4="$(freeport)"
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=kill-after-link"
inst --skip-claude-check --source "$SRC/manifest.json" --port "$P4"
check "3r 連結建好後被 kill -9：紀錄裡有連結與 ~/.local/bin" '[ $RC = 137 ] && [ -L "$FH/.local/bin/evergenie" ] && grep -q "^link" "$APP/.install-unfinished" && grep -q "bindir" "$APP/.install-unfinished"'
mkdir -p "$FH/.local/share/別的工具" "$FH/.evergenie/別的"
echo 資料 >"$FH/.local/share/別的工具/資料" && echo 留著 >"$FH/.evergenie/別的/留著.txt"
INST_EXTRA=""
inst --skip-claude-check --source "$SRC/manifest.json" --port "$P4"
check "3r 同一行再跑：裝好，兩次之間別人放的東西都在" '[ $RC = 0 ] && grep -q "上次的安裝沒有跑完" <<<"$OUT" && [ "$(cat "$FH/.local/share/別的工具/資料")" = 資料 ] && [ "$(cat "$FH/.evergenie/別的/留著.txt")" = 留著 ] && [ "$(readlink "$FH/.local/bin/evergenie")" = "$APP/current/bin/evergenie" ]'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 重跑時換了 --home：上次的家不是這次要用的位置，不動它、當場說一聲；上次的服務照樣收掉
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=kill-after-link"
inst --source "$SRC/manifest.json" --home "$FH/家A" --port "$(freeport)"
INST_EXTRA=""
inst --source "$SRC/manifest.json" --home "$FH/家B" --port "$(freeport)"
check "3s 換了 --home 再跑：裝好、說上次的家沒有動、家A 還在、家A 的服務停了" '[ $RC = 0 ] && grep -q "注意：上次沒裝完時建的 $FH/家A 跟這次的位置不一樣" <<<"$OUT" && [ -f "$FH/家A/home.toml" ] && [ -f "$FH/家B/home.toml" ] && ! pgrep -f "serve --?home $FH/家A" >/dev/null'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$FH/家B" >/dev/null 2>&1

# 上次那個安裝還在跑（例如背景重試、前一次還沒結束）：不收它的東西，結束 14
newhome
(exec -a hg-install-fake-running sleep 60) &
RUNNING=$!
mkdir -p "$APP" && printf 'pid\t%s\nplace\t%s\tcreated\t%s\n' "$RUNNING" "$APP" "$FH/.evergenie" >"$APP/.install-unfinished"
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3t 另一個安裝正在跑：結束 14、說另一個安裝正在跑、它的東西沒動" '[ $RC = 14 ] && grep -q "另一個安裝正在跑" <<<"$OUT" && [ -f "$APP/.install-unfinished" ]'
kill "$RUNNING" 2>/dev/null; wait "$RUNNING" 2>/dev/null

# 上次被 kill -9 之後，那個家已經被用過（只多了一筆紀錄，home.toml 沒動）：重跑不收任何東西，結束 14；家、服務、程式、紀錄都還在
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=kill-after-service"
inst --source "$SRC/manifest.json" --port "$(freeport)"
home_toml_before="$(shasum -a 256 "$HOMEDIR/home.toml")"
mkdir -p "$HOMEDIR/vault/tasks" && echo 主人的資料 >"$HOMEDIR/vault/tasks/主人的資料.md"
INST_EXTRA=""
inst --source "$SRC/manifest.json" --port "$(freeport)"
# 服務還在跑、會一直寫狀態資料夾，所以不比整個假 HOME，逐項看
check "3u 上次的家被用過：結束 14、說不是剛建好的、什麼都沒動（家、程式、連結、紀錄、服務）" '[ $RC = 14 ] && grep -q "沒裝成功（結束碼 14）：上次沒裝完時建的家 $HOMEDIR 已經不是剛建好的樣子" <<<"$OUT" && [ "$(shasum -a 256 "$HOMEDIR/home.toml")" = "$home_toml_before" ] && [ "$(cat "$HOMEDIR/vault/tasks/主人的資料.md")" = 主人的資料 ] && [ -x "$APP/current/bin/evergenie" ] && [ -f "$APP/evergenie.local.toml" ] && grep -q "^service" "$APP/.install-unfinished" && serve_running'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 上次在建家的時候被停掉（紀錄裡有這次建的家，還沒記下家的指紋）、家裡已經有檔：認不出來，不收，結束 14
newhome
mkdir -p "$APP" "$HOMEDIR" && echo 'schema = 1' >"$HOMEDIR/home.toml"
printf 'pid\t999999\nplace\t%s\tcreated\t%s\nplace\t%s\tcreated\t%s\n' "$HOMEDIR" "$HOMEDIR" "$APP" "$FH/.evergenie" >"$APP/.install-unfinished"
before="$(snap)"
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3v 上次建的家認不出來：結束 14、什麼都沒動" '[ $RC = 14 ] && grep -q "認不出是不是剛建好的" <<<"$OUT" && [ "$before" = "$(snap)" ]'

# 裝好了卻刪不掉 .install-unfinished（測試：假裝刪不掉）：改寫成 done、說一聲；重跑當成已經裝好（照一般的檢查：家已經有東西，結束 13），家沒被收
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=mark-stuck"
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3w 刪不掉紀錄：照樣裝好、紀錄改成 done、有「注意：」" '[ $RC = 0 ] && grep -q "裝好了" <<<"$OUT" && [ "$(cat "$APP/.install-unfinished")" = done ] && grep -q "注意：收不掉 $APP/.install-unfinished" <<<"$OUT"'
INST_EXTRA=""
inst --source "$SRC/manifest.json" --port "$(freeport)"
check "3w 重跑：說上次其實裝好了（不說沒跑完）、照一般的檢查停在家已經有東西（13）、家與程式都在、紀錄收掉" '[ $RC = 13 ] && grep -q "上次的安裝其實已經裝好了" <<<"$OUT" && ! grep -q "上次的安裝沒有跑完" <<<"$OUT" && grep -q "已經有東西了" <<<"$OUT" && [ -f "$HOMEDIR/home.toml" ] && [ -x "$APP/current/bin/evergenie" ] && [ ! -e "$APP/.install-unfinished" ]'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

# 裝好之後等角色包同步有上限（DEV-157）：同步一直不回來，等到上限只印一行注意，照樣算裝好了（結束碼 0），沒有被收掉
newhome
INST_EXTRA="EVERGENIE_INSTALL_TEST_FAULT=hang-pack EVERGENIE_INSTALL_PACK_WAIT=2"
inst --source "$SRC/manifest.json" --port "$(freeport)"
INST_EXTRA=""
check "3z 等角色包同步超時：照樣裝好（結束碼 0）、只印一行注意" '[ $RC = 0 ] && grep -q "裝好了" <<<"$OUT" && [ "$(grep -c "注意：角色的頭像與衣服等了 2 秒還沒弄好，先不等了；服務之後會自己補上。" <<<"$OUT")" = 1 ] && ! grep -q "沒裝成功" <<<"$OUT"'
check "3z 超時後家與服務都在、沒留 .install-unfinished" '[ -f "$HOMEDIR/home.toml" ] && [ ! -e "$APP/.install-unfinished" ] && serve_running'
env -i HOME="$FH" PATH="$SAFE" EVERGENIE_SERVICE_MODE=process "$APP/current/bin/evergenie" service uninstall --home "$HOMEDIR" >/dev/null 2>&1

say "── 4. ~/.local/bin/evergenie 本來是別人的：不動它、照樣裝好（--no-service、指定位置）"
newhome
echo "別人的" >"$FH/.local/bin/evergenie"
inst --no-service --home "$FH/家" --dir "$FH/程式" --state "$FH/狀態" --source "$SRC/manifest.json" --port "$(freeport)"
check "4a 結束 0、說那裡本來有別的、還沒啟動" '[ $RC = 0 ] && grep -q "注意：$FH/.local/bin/evergenie 本來就有別的東西，沒有動它" <<<"$OUT" && grep -q "還沒有啟動" <<<"$OUT"'
check "4b 別人的檔案沒被動、程式與家在指定的位置" '[ "$(cat "$FH/.local/bin/evergenie")" = 別人的 ] && [ -x "$FH/程式/current/bin/evergenie" ] && [ -f "$FH/家/home.toml" ] && [ -d "$FH/狀態" ] && grep -q "home = \"$FH/家\"" "$FH/程式/evergenie.local.toml"'
check "4c --no-service 沒有起服務" '! serve_running && [ ! -e "$FH/狀態/runtime/serve.pid" ]'

echo
echo "通過 ${pass}，失敗 $fail"
[ "$fail" = 0 ]
