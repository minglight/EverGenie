#!/usr/bin/env bash
# 乾淨環境的整體驗收（DEV-155 S9）：模擬「一台什麼都沒裝的 Mac 上，不懂電腦的人從安裝用到升級」整條路，一次跑完。
# `make clean-install-test` 叫它；`make release-e2e` 也跑（不進 `make ci`）。
#
#   scripts/clean-install-test.sh [--help]
#
#   1. 環境：空的假 HOME、env -i；PATH 只有系統內建工具（/usr/bin、/bin、/usr/sbin、/sbin 的替身：沒有 go、brew、tmux；git、make、
#      python3、編譯器換成一叫就失敗、記下來的絆線，像乾淨的 Mac 上「要先裝開發者工具」的空殼）加 ~/.local/bin 的假 claude（已登入）。
#      本機 http 當下載點（安裝程式、版本清單、各版的包）。
#   2. 照 INSTALL.md 第 3 步：curl 下載安裝程式、一行裝 0.0.1 → 看板 200、doctor 沒有要處理的、角色庫列出公開角色、
#      秘書做套件要讀的文件照裝好的技能 create-feature 寫的方式（evergenie docs show <文件>）讀得到、跟 repo 的一樣（platform PLT-28）、
#      名冊是空的（看板會開新家的歡迎頁，WEB-176）、照歡迎頁走的 API 請第一位秘書、開她的 session（附帶的 tmux、經啟動程式 disclaim 開 server、假的 claude 在裡面）。
#      2d. 角色包帶衣服（DEV-157）：假的來源家（服務在跑、官方角色留在家裡、兩位公開角色各一套送的與一套要兌換的、一組隱藏版、兩套主人自己還沒升成官方的衣服、一套官方設計中的）
#      等衣櫃替圖登記好照片紀錄（DEV-160）再建帶紀錄的角色包，
#      新的家同步 → 衣櫃馬上有每位角色的衣服、各擁有一套、頭像 200；隱藏版的檔案與紀錄不在包裡也不在新的家。
#   3. 看板 API 升到 0.0.2（檢查更新 → 現在更新）→ 版本變了、看板 200、資料還在、秘書的 session 還在、固定位置的 tmux 沒換（inode 一樣）。
#   4. 故意失敗三種（都走看板 API，看看板拿到的結果；白話原因與下一步不含英文字母）：檢查碼不符 → 舊版照常；0.0.3 的服務一開就掛 →
#      自動換回、資料沒動；0.0.4 一開就讓舊版的資料夾讀不到、檢查也沒過 → 換不回去、要處理，排除後按「退回上一版」救回來。
#   5. 主人確認新版（upgrade --accept）→ 舊版清掉；照 ai_docs/runbooks/install.md「移除」一步一步做 → 假 HOME 裡不留程式、服務、行程。
#   最後看絆線：整條路沒叫到 git、make、python3、編譯器。
#
# 跟主人那台不一樣的只有測試要的：EVERGENIE_SERVICE_MODE=process（服務是背景行程，不碰 launchd）、TMUX_TMPDIR 在暫存資料夾（碰不到各家
# 真的 tmux socket）、安裝時多給 --source（本機 http 的版本清單）與 --port（空的 port；預設 8765 往上找可能搶到這台真的家要用的）。
# 全程在 mktemp -d 的暫存資料夾（預設 /tmp：socket 路徑有長度上限），不碰任何家、不連外網。要先有 dist/tmux（scripts/build-tmux.sh；
# 或用 EVERGENIE_DIST_TMUX 指到別處）。結束碼 0＝全部通過；1＝有沒過的（印 ✘ 與最近一次的輸出）；2＝用法錯誤。
# 環境變數：EVERGENIE_E2E_KEEP=1 保留暫存資料夾；EVERGENIE_E2E_TMP 暫存資料夾放哪（預設 /tmp）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
# macOS 內建的是 bash 3.2：變數後面緊接中文要加大括號（${x}）。

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  sed -n '2,26p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi
if [ $# -gt 0 ]; then
  echo "不認得的選項：${1}（--help 看用法）" >&2
  exit 2
fi
for tool in go python3 curl git shasum tar; do command -v "$tool" >/dev/null 2>&1 || { echo "這台沒有 ${tool}，跑不了（編發行檔與檢查要用）" >&2; exit 1; }; done

cd "$(dirname "$0")/.." || exit 2
ROOT="$PWD"
. "$ROOT/scripts/lib/download-e2e.sh"
case "$(uname -m)" in arm64) ARCH=arm64 TARCH=arm64 ;; x86_64) ARCH=amd64 TARCH=x86_64 ;; *) echo "不認得這台的晶片：$(uname -m)" >&2; exit 1 ;; esac
TMUXD="${EVERGENIE_DIST_TMUX:-$ROOT/dist/tmux}"
PRODUCT="$(sed -n 's/^PRODUCT="\([^"]*\)"$/\1/p' "$ROOT/scripts/install.sh")" # 產品名＝附帶的 tmux 在 fixed/ 裡的檔名（ECL-65）
if [ ! -x "$TMUXD/$TARCH/tmux" ] || [ ! -x "$TMUXD/$TARCH/disclaim" ]; then
  echo "還沒編附帶的 tmux 與啟動程式（找不到 $TMUXD/$TARCH/tmux 或 disclaim）：先跑 scripts/build-tmux.sh，再跑一次 make clean-install-test。" >&2
  exit 1
fi

B="$(dl_mktemp "${EVERGENIE_E2E_TMP:-/tmp}" hgc)" || { echo "建不出暫存資料夾（在 ${EVERGENIE_E2E_TMP:-/tmp} 底下）" >&2; exit 1; }
ME="$(id -un)"
FH="$B/h" TMPD="$B/t" WWW="$B/www" DIST="$B/dist" SAFE="$B/safe" TRIP="$B/trip.log" TT="$B/tmux"
APP="$FH/.evergenie/app" HOMEDIR="$FH/evergenie-$ME" STATE="$FH/.evergenie/state/evergenie-$ME"
TBIN="$APP/fixed/$PRODUCT" SOCK="$TT/tmux-$(id -u)/eg" # 秘書的 tmux：固定位置那支、預設 socket 名稱 eg（在暫存資料夾裡）
PUB="$B/pub" PSTATE="$B/pubstate" # 2d 的假角色包來源家（不在假 HOME 裡：它不是主人的家）
mkdir -p "$FH" "$TMPD" "$WWW/scripts" "$DIST" "$TT"
WWW_PID=""

# 收掉這次開的行程：只照 pid 檔（服務、跑到一半的升級行程的鎖）與自己的 tmux socket 收，不用名稱比對。pid 檔可能過期（行程早就
# 停了、pid 給了別人），所以先確認那個 pid 的指令列帶著這個暫存資料夾的路徑才收。服務自成一個行程群組（引擎與套件行程在裡面），
# 整組送 TERM，10 秒還在就整組 KILL。
own() { ps -o command= -p "$1" 2>/dev/null | grep -qF "$B/"; }
cleanup() {
  local p i pids=""
  for f in "$STATE/runtime/upgrade/lock" "$STATE/runtime/serve.pid" "$PSTATE/runtime/serve.pid"; do
    p="$(tr -d ' \n' 2>/dev/null <"$f")"
    case "$p" in '' | *[!0-9]*) continue ;; esac
    own "$p" || continue
    kill -TERM -- "-$p" 2>/dev/null || kill -TERM "$p" 2>/dev/null
    pids="$pids $p"
  done
  [ -S "$SOCK" ] && env -i PATH=/usr/bin:/bin "$TMUXD/$TARCH/tmux" -S "$SOCK" kill-server 2>/dev/null
  [ -n "$WWW_PID" ] && kill "$WWW_PID" 2>/dev/null
  for p in $pids; do
    for ((i = 0; i < 40; i++)); do kill -0 "$p" 2>/dev/null || break; sleep 0.25; done
    own "$p" && { kill -KILL -- "-$p" 2>/dev/null || kill -KILL "$p" 2>/dev/null; }
  done
  chmod -R u+rwx "$B" 2>/dev/null # 0.0.4 那段把舊版的資料夾改成讀不到；中途停掉時要改回來才刪得掉
  if [ "${EVERGENIE_E2E_KEEP:-0}" = 1 ]; then echo "（保留暫存資料夾：${B}）"; else rm -rf "$B"; fi
}
trap cleanup EXIT
trap 'echo; echo "中斷了，收掉暫存的服務。"; exit 130' INT TERM

say() { printf '%s\n' "$*"; }
FAILS=0 OUT="" RC=0
chk() {
  if eval "$2"; then
    say "  ✔ $1"
  else
    say "  ✘ $1"
    FAILS=$((FAILS + 1))
    if [ -n "$OUT" ]; then
      say "      --- 最近一次的輸出（結束碼 ${RC}）---"
      printf '%s\n' "$OUT" | tail -25 | sed 's/^/      /'
    fi
  fi
}
stop_here() { say "  （$1，後面不測了）"; say "乾淨環境整體驗收：沒過（$((SECONDS - T0)) 秒）"; exit 1; }

# 主人那台乾淨 Mac 的環境：env -i、只有系統內建工具的 PATH 加 ~/.local/bin（假的 claude、裝好之後的 evergenie 都在那裡），在家目錄跑。
USER_ENV=(env -i HOME="$FH" USER="$ME" LOGNAME="$ME" SHELL=/bin/zsh PATH="$SAFE:$FH/.local/bin" TMPDIR="$TMPD" LANG=en_US.UTF-8
  TERM=xterm-256color TMUX_TMPDIR="$TT" EVERGENIE_SERVICE_MODE=process)
as_user() { OUT="$(cd "$FH" && "${USER_ENV[@]}" "$@" 2>&1)"; RC=$?; }
hgu() { as_user evergenie "$@"; } # 裝好之後的 evergenie（~/.local/bin，不給 --home，靠指標檔找家）
tmx() { env -i PATH=/usr/bin:/bin "$TBIN" -S "$SOCK" "$@"; } # 看秘書的 tmux（固定位置那支、同一個 socket）

PORT="$(dl_freeport)" WPORT="$(dl_freeport)"
SRC="http://127.0.0.1:$WPORT"
BOARD="http://127.0.0.1:$PORT/"
board_code() { curl -s -o /dev/null -m 5 -w '%{http_code}' "$BOARD" 2>/dev/null; }
wait_board() { # wait_board [秒]：等看板回 200
  local i
  for ((i = 0; i < ${1:-90}; i++)); do
    [ "$(board_code)" = 200 ] && return 0
    sleep 1
  done
  return 1
}
# api <服務/方法> <JSON>：跟看板的網頁一樣打 /api/<服務>/<方法>（本機、沒有 Origin；看板把身分換成主人）。回應在 OUT，RC 0＝HTTP 200。
api() {
  local code
  code="$(curl -sS -m 30 -o "$B/api.out" -w '%{http_code}' -X POST -H 'Content-Type: application/json' --data "$2" "${BOARD}api/$1" 2>"$B/api.err")"
  RC=$?
  OUT="$(cat "$B/api.out" 2>/dev/null)"
  if [ $RC != 0 ]; then
    OUT="$OUT$(cat "$B/api.err" 2>/dev/null)"
  elif [ "$code" != 200 ]; then
    RC=22 OUT="HTTP $code $OUT"
  fi
}
upd() { api system/update_status '{}'; } # 看板的「版本與更新」現況（UpdateInfo）
upd_field() { upd && dl_pyj "$1" <<<"$OUT" 2>/dev/null; }
state_of() { dl_pyj '(lambda s: " ".join([s.get("kind") or "upgrade", s.get("started_at") or "-", s.get("phase") or "-"]))(d.get("state") or {})' <<<"$1" 2>/dev/null; }
LOCK="$STATE/runtime/upgrade/lock"
# wait_update <種類 upgrade|rescue> <按之前的 started_at>：輪詢看板的 update_status，等這一次（started_at 變了、種類對）
# 跑完（done、failed、rolled_back），再等升級行程收尾放掉鎖。看板重開時連不上照樣等（最多四分鐘）。最後一次的現況在 OUT。
wait_update() {
  local i k s p info=""
  for ((i = 0; i < 240; i++)); do
    upd
    if [ $RC = 0 ]; then
      info="$OUT"
      read -r k s p <<<"$(state_of "$info")"
      if [ "$k" = "$1" ] && [ "$s" != "$2" ]; then
        case "$p" in done | failed | rolled_back)
          for ((i = 0; i < 60; i++)); do [ -e "$LOCK" ] || break; sleep 0.5; done
          upd
          return 0
          ;;
        esac
      fi
    fi
    sleep 1
  done
  OUT="${info:-$OUT}"
  return 1
}
started_at() { upd && read -r _ s _ <<<"$(state_of "$OUT")" && echo "$s"; }
# press_update <版本>：看板「版本與更新」卡：按「檢查更新」看到新版是 <版本>，再按「現在更新」，等它跑完。結果（UpdateInfo）在 OUT。
press_update() {
  local before
  api system/update_check '{}'
  [ $RC = 0 ] && [ "$(dl_pyj 'd.get("target", "")' <<<"$OUT")" = "$1" ] || return 1
  before="$(started_at)"
  api system/update_apply "{\"version\":\"$1\"}"
  [ $RC = 0 ] || return 1
  wait_update upgrade "$before"
}
# 看板拿到的結果：outcome、restored、原因與下一步
result_of() { dl_pyj '(lambda s: " ".join([s.get("phase") or "-", s.get("outcome") or "-", str(bool(s.get("restored")))]))(d.get("state") or {})' <<<"$1"; }
reason_of() { dl_pyj '(d.get("state") or {}).get("reason", "")' <<<"$1"; }
next_of() { dl_pyj '(d.get("state") or {}).get("next", "")' <<<"$1"; }
# plain_ok <說明> <UpdateInfo>：看板拿到的白話原因與下一步都有、都不含英文字母（連一個字母都不行）
plain_ok() {
  local r n
  r="$(reason_of "$2")" n="$(next_of "$2")"
  chk "${1}：看板拿到的原因不含英文字母、有下一步（原因：${r}｜下一步：${n}）" '[ -n "$r" ] && [ -n "$n" ] && ! grep -q "[A-Za-z]" <<<"$r$n"'
}
current() { readlink "$APP/current"; }
versions_list() { (cd "$APP/versions" && /bin/ls -A | LC_ALL=C sort | tr '\n' ' '); }
serve_pid() { tr -d ' \n' <"$STATE/runtime/serve.pid" 2>/dev/null; }
skipped() { grep -q "\"$1\"" "$STATE/runtime/upgrade/skip.json" 2>/dev/null; }
inode() { stat -f %i "$1" 2>/dev/null; }

T0=$SECONDS
say "── 準備：編四個版本（scripts/dist.sh，darwin-${ARCH}，帶附帶的 tmux；找新版的地方編進執行檔），本機 http 當下載點"
for v in 0.0.1 0.0.2 0.0.3 0.0.4; do
  OUT="$(env -u EVERGENIE_HOME -u EVERGENIE_ENGINE_BIN_DIR "$ROOT/scripts/dist.sh" --version "$v" --arch "$ARCH" --out "$DIST/$v" --source "$SRC/manifest.json" --tmux "$TMUXD" --notes "第 $v 版的說明" 2>&1)"
  RC=$?
  chk "dist.sh 編 $v" '[ $RC = 0 ] && [ -s "$DIST/$v/manifest.json" ]'
  [ $RC = 0 ] || stop_here "編不出發行檔"
done
# 0.0.3：服務一開就掛（印一段英文，確定它不會跑進看板的原因）；其他指令照常交給真的執行檔。
cat >"$B/broken-0.0.3" <<'SH'
#!/bin/sh
if [ "${1:-}" = serve ]; then
  echo "panic: e2e broken build on purpose (exit status 3)" >&2
  exit 3
fi
exec "$(dirname "$0")/evergenie.real" "$@"
SH
# 0.0.4：服務一開就把舊版（0.0.2）的 bin/ 改成讀不到（像權限或硬碟出問題），而且讀不到套件清單（升級後的檢查沒過）→ 自動換回時
# 換不回舊版的資料夾。測試之後把權限改回來（排除原因），再按「退回上一版」。
cat >"$B/broken-0.0.4" <<SH
#!/bin/sh
case "\${1:-}" in
serve) chmod 000 "$APP/versions/0.0.2/bin" ;;
packages) echo "error: e2e broken build on purpose: cannot list packages" >&2; exit 1 ;;
esac
exec "\$(dirname "\$0")/evergenie.real" "\$@"
SH
dl_wrap_bin "$DIST/0.0.3" 0.0.3 "$ARCH" "$B/broken-0.0.3" && dl_wrap_bin "$DIST/0.0.4" 0.0.4 "$ARCH" "$B/broken-0.0.4"
RC=$?
chk "做好壞掉的 0.0.3（服務一開就掛）與 0.0.4（讓舊版讀不到、套件清單讀不到）" '[ $RC = 0 ]'
cp "$ROOT/scripts/install.sh" "$WWW/scripts/install.sh"
dl_publish "$WWW" "$DIST/0.0.1" "$ARCH"
python3 -I -m http.server "$WPORT" --bind 127.0.0.1 --directory "$WWW" >"$B/www.log" 2>&1 &
WWW_PID=$!
for _ in $(seq 1 50); do curl -s -o /dev/null "$SRC/manifest.json" && break; sleep 0.2; done
OUT="$(curl -s "$SRC/manifest.json")"
chk "本機 http 提供版本清單（0.0.1）" 'grep -q "\"version\": *\"0.0.1\"" <<<"$OUT"'

say "── 1. 乾淨的 Mac：空的家目錄、只有系統內建工具、假的 claude（已登入）"
dl_safebin "$SAFE" "$TRIP"
dl_fake_claude "$FH"
OUT="$(ls -A "$FH" | LC_ALL=C sort | tr '\n' ' ')"
chk "家目錄只有 Claude Code 留下的（.claude.json、.local/bin/claude）" '[ "$OUT" = ".claude.json .local " ] && [ "$(ls -A "$FH/.local/bin")" = claude ]'
as_user /bin/sh -c 'command -v go brew tmux evergenie'
chk "PATH 裡沒有 go、brew、tmux（也還沒有 evergenie）" '[ -z "$OUT" ]'
as_user /bin/sh -c 'for t in git make python3; do p="$(command -v "$t")" || exit 1; case "$p" in "$0"/*) ;; *) exit 1 ;; esac; done; git --version || make --version || python3 --version || exit 0; exit 1' "$SAFE"
chk "git、make、python3 只有「要先裝開發者工具」的空殼：一叫就失敗" '[ $RC = 0 ] && grep -q "No developer tools" <<<"$OUT"'
: >"$TRIP" # 上面那幾下是故意叫的；之後整條路都不該再叫到
as_user uname -s
chk "INSTALL.md 第 1 步：這台是 Mac（uname -s）" '[ "$OUT" = Darwin ]'

say "── 2. 照 INSTALL.md 裝 0.0.1：下載安裝程式、一行裝好"
as_user curl -fsSL "$SRC/scripts/install.sh" -o "$TMPD/evergenie-install.sh"
chk "第 3 步：curl 下載安裝程式（跟 repo 的 scripts/install.sh 一樣）" '[ $RC = 0 ] && cmp -s "$TMPD/evergenie-install.sh" "$ROOT/scripts/install.sh"'
as_user bash "$TMPD/evergenie-install.sh" --name 測試 --source "$SRC/manifest.json" --port "$PORT"
INSTALL_OUT="$OUT"
URL="$(sed -n 's/^看板網址：\(http[^ ]*\)$/\1/p' <<<"$INSTALL_OUT" | tail -1)"
chk "一行裝好：結束碼 0、印「裝好了（版本 0.0.1）」與看板網址" '[ $RC = 0 ] && grep -q "裝好了（版本 0.0.1）" <<<"$INSTALL_OUT" && [ "$URL" = "$BOARD" ]'
chk "服務起來之後請它同步了一次角色包（這一版沒有角色包：不算失敗）" 'grep -q "^== 下載角色的頭像與衣服" <<<"$INSTALL_OUT" && ! grep -q "角色的頭像與衣服這次還沒弄好" <<<"$INSTALL_OUT"'
[ $RC = 0 ] || stop_here "裝不起來"
# 帳號資料夾在假 HOME 裡（DEV-157）：env -i 帶不進這台電腦真的 CLAUDE_CONFIG_DIR，init 照乾淨 Mac 的預設登入寫 ~/.claude，
# 服務跑在 HOME=假家目錄 底下，~ 就是假的；寫成別的（例：真的 Claude 設定的絕對路徑）就是測試環境漏了。
chk "帳號資料夾是假 HOME 的 ~/.claude（預設登入），不是這台電腦真的 Claude 設定" 'grep -qx "config_dir = \"~/.claude\"" "$HOMEDIR/home.toml" && grep -qx "default_login = true" "$HOMEDIR/home.toml"'
chk "看板首頁 200" 'wait_board 30'
hgu version
chk "打 evergenie 就找得到（~/.local/bin），版本 0.0.1" '[ $RC = 0 ] && [ "$OUT" = 0.0.1 ]'
chk "程式在 ~/.evergenie/app/versions/0.0.1、固定位置有產品名的 tmux 與啟動程式" '[ "$(current)" = versions/0.0.1 ] && [ -x "$TBIN" ] && [ -x "$APP/fixed/disclaim" ] && cmp -s "$TBIN" "$TMUXD/$TARCH/tmux"'
TMUX_INODE="$(inode "$TBIN")" DISCLAIM_INODE="$(inode "$APP/fixed/disclaim")"
hgu doctor --json
DOC="$OUT"
OUT="$(dl_doctor_reds <<<"$DOC" 2>&1)"
chk "doctor 沒有要處理的（沒有紅字）" '[ -n "$DOC" ] && [ -z "$OUT" ]'
WANT_CARDS="$(dl_official_cards "$ROOT")"
api personas/list '{}'
OUT2="$OUT"
OUT="$(dl_pyj '" ".join(sorted(c["slug"] for c in d.get("personas") or [] if c.get("source") == "official"))' <<<"$OUT2" 2>&1)"
chk "角色庫（看板的 personas.list）列出公開角色（${OUT}）" '[ -n "$WANT_CARDS" ] && [ "$OUT" = "$WANT_CARDS" ]'
# 秘書替主人做套件要讀的文件（DEV-155、platform PLT-28）：照裝好的技能 create-feature 寫的方式（evergenie docs show <文件>）一份一份讀。
# 下載版沒有 repo：讀到的是這一版程式資料夾裡跟著帶的 ai_docs/，內容要跟 repo 的一樣
SKILL="$APP/current/packages/official/engine-claude/skills/create-feature/SKILL.md"
DOCS_WANT="$(grep -o 'evergenie docs show [A-Za-z0-9_./-]*' "$SKILL" 2>/dev/null | sed 's/^evergenie docs show //' | LC_ALL=C sort -u | tr '\n' ' ')"
OUT="$DOCS_WANT"
chk "裝好的技能 create-feature 用 evergenie docs show 指名詞、資料夾地圖、欄位那三份（${DOCS_WANT}）" '(for d in conventions/user-features.md conventions/terms.md schemas/package.md; do case " $DOCS_WANT " in *" $d "*) ;; *) exit 1 ;; esac; done)'
hgu docs list
PROGDIR="$(sed -n 's/^程式資料夾：\(.*\)（.*/\1/p' <<<"$OUT" | head -1)"
chk "evergenie docs list 找的是這一版程式資料夾裡的 ai_docs/" '[ $RC = 0 ] && grep -q "/\.evergenie/app/versions/0\.0\.1/ai_docs" <<<"$OUT"'
chk "evergenie docs list 第一行印程式資料夾（這一版），技能照它找的 sdk/ 在那裡（${PROGDIR}）" '[ -n "$PROGDIR" ] && case "$PROGDIR" in */.evergenie/app/versions/0.0.1) true ;; *) false ;; esac && [ -f "$PROGDIR/sdk/go.mod" ]'
DOCS_BAD=""
for d in $DOCS_WANT; do
  as_user /bin/sh -c 'evergenie docs show "$1" >"$2"' sh "$d" "$TMPD/doc.txt"
  { [ $RC = 0 ] && cmp -s "$TMPD/doc.txt" "$ROOT/ai_docs/$d"; } || DOCS_BAD="${DOCS_BAD} ${d}（結束碼 ${RC}：$(head -c 300 <<<"$OUT")）"
done
OUT="$DOCS_BAD"
chk "照技能寫的一份一份讀（evergenie docs show）：每份都讀得到、跟 repo 的 ai_docs/ 一樣" '[ -n "$DOCS_WANT" ] && [ -z "$DOCS_BAD" ]'

say "── 2b. 新家的歡迎頁（看板的 API）：請第一位秘書"
api web/get_agents '{}'
OUT="$(dl_pyj '"名冊 %d 位：%s" % (len(d.get("agents") or []), " ".join(a.get("id", "?") for a in d.get("agents") or []))' <<<"$OUT" 2>&1)"
chk "剛裝好名冊是空的（一位秘書都沒有，看板才會開歡迎頁，WEB-176）" '[ "$OUT" = "名冊 0 位：" ]'
# 歡迎頁（與「職位與秘書」頁的認領卡共用 hireSecretary）：啟用中的套件帶來、名冊上還沒有的職位（選用職位不列，PKG-12、WEB-47），選第一個；角色照看板預設選的（職位自己帶的、還沒坐位置的優先）
api package/list '{}' && cp "$B/api.out" "$B/pkgs.json"
api web/get_agents '{}' && cp "$B/api.out" "$B/agents.json"
api personas/list '{}' && cp "$B/api.out" "$B/personas.json"
CLAIM="$(python3 -I - "$B/pkgs.json" "$B/agents.json" "$B/personas.json" "$B/fns.txt" <<'PY'
import json, sys
pk, ag, pe = (json.load(open(p, encoding="utf-8")) for p in sys.argv[1:4])
seen = {a.get("id") for a in ag.get("agents") or []}
fns, optional = [], []
for p in pk.get("packages") or []:
    if not p.get("enabled") or p.get("state") == "rejected" or not p.get("id"):
        continue
    for a in p.get("agents") or []:
        if a.get("id") and a["id"] not in seen:
            seen.add(a["id"])
            (optional if a.get("optional") else fns).append((p["id"], a["id"]))
with open(sys.argv[4], "w", encoding="utf-8") as f:
    f.write("candidates=" + ",".join(x[1] for x in fns) + " optional=" + ",".join(x[1] for x in optional) + "\n")
live = [c for c in pe.get("personas") or [] if c.get("status") == "live" and not c.get("shadowed") and not c.get("unassignable")]
if fns and live:
    pkg, agent = fns[0]
    own = [c for c in live if c.get("source") == "package" and c.get("package") == pkg]
    pool = own or live
    pick = next((c for c in pool if not c.get("seats")), pool[0])
    print(pkg, agent, pick["slug"])
PY
)"
read -r CPKG AGENT PERSONA <<<"$CLAIM"
OUT="$(cat "$B/fns.txt" 2>&1)"
chk "一般家沒有生圖秘書：生圖 art 不在歡迎頁的候選裡，也不在選用的職位裡（它只在開發家開著的開發工坊，ADM-39）" '[[ "$OUT" == "candidates="*" optional="* ]] && ! grep -Eq "(=|,)art( |,|$)" <<<"$OUT"'
OUT="$CLAIM"
chk "歡迎頁列出還沒人負責的職位與可以選的角色（選 ${AGENT:-?}，角色 ${PERSONA:-?}）" '[ -n "${PERSONA:-}" ]'
[ -n "${PERSONA:-}" ] || stop_here "歡迎頁沒有可以選的職位"
api package/sync "{\"id\":\"$CPKG\",\"agent\":\"$AGENT\",\"persona\":\"$PERSONA\",\"apply\":true}"
chk "按「請她上班」：建好秘書（package.sync）" '[ $RC = 0 ]'
api agents/set_field "{\"id\":\"$AGENT\",\"field\":\"enabled\",\"value\":\"true\"}"
chk "按「請她上班」：啟用（agents.set_field）" '[ $RC = 0 ]'
roster() { api web/get_agents '{}' && dl_pyj '" ".join(sorted(a["id"] + ":" + str(a.get("persona") or "") for a in d.get("agents") or [] if a.get("enabled")))' <<<"$OUT"; }
ROSTER="$(roster)"
OUT="$ROSTER"
chk "請好了：${AGENT} 上班了（看板的名冊）" 'grep -q "^$AGENT:" <<<"$ROSTER"'

say "── 2c. 開她的 session：附帶的 tmux、經啟動程式開 server、假的 claude 在裡面"
api session/open "{\"agent\":\"$AGENT\"}"
# 剛請她上班的頭一兩秒引擎可能還在起來（回 503「引擎尚未就緒，請稍後重試」）：照訊息說的等一下再按，最多 10 秒。
for _ in 1 2 3 4 5 6 7 8 9 10; do
  { [ "$RC" = 0 ] || ! grep -q '"unavailable"' <<<"$OUT"; } && break
  sleep 1
  api session/open "{\"agent\":\"$AGENT\"}"
done
SNAME="$(dl_pyj '(d.get("session") or {}).get("name", "")' <<<"$OUT" 2>/dev/null)"
chk "看板按「開始聊天」（session.open）" '[ $RC = 0 ] && [ -n "$SNAME" ]'
TNAME="$(printf '%s' "$SNAME" | tr '.:' '__')"
for _ in $(seq 1 30); do
  tmx has-session -t "=$TNAME" 2>/dev/null && tmx capture-pane -p -t "=$TNAME:" 2>/dev/null | grep -q "假的 claude 在 tmux 裡" && break
  sleep 1
done
OUT="$(tmx capture-pane -p -t "=$TNAME:" 2>&1)"
chk "tmux 裡有她的 session（socket 在暫存資料夾），裡面跑的是 claude" '[ -S "$SOCK" ] && grep -q "假的 claude 在 tmux 裡：--" <<<"$OUT"'
TSERVER="$(tmx display-message -p -t "=$TNAME:" '#{pid}' 2>/dev/null)"
PANE="$(tmx display-message -p -t "=$TNAME:" '#{pane_pid}' 2>/dev/null)"
OUT="$(ps -o comm= -p "${TSERVER:-0}" 2>&1)"
chk "tmux server 是固定位置那支附帶的 tmux（${TBIN}）" '[ "$OUT" = "$TBIN" ]'
# 啟動程式設不起來時會印「照平常的方式開」那一行，引擎記進 <狀態>/runtime/engine-claude/out.log（只有出事才有這個檔，ECL-65）
ENGDIR="$STATE/runtime/engine-claude"
OUT="$(cat "$ENGDIR/out.log" 2>/dev/null)"
chk "開 server 經過啟動程式 disclaim：引擎開過 session、沒記下「照平常的方式開」" '[ -d "$ENGDIR/launch" ] && ! grep -qs "權限視窗沒辦法改記在 tmux 自己名下" "$ENGDIR/out.log" "$STATE/runtime/logs/serve.log"'
hgu preflight
chk "安裝前檢查：tmux 是固定位置那份、權限視窗記在它名下" 'grep -qF "✔ tmux（${TBIN}（跟著程式一起裝的；系統問資料夾權限時記在這一支名下））" <<<"$OUT"'
HOMETOML="$(shasum -a 256 "$HOMEDIR/home.toml" | cut -d' ' -f1)"
data_same() { [ "$(roster)" = "$ROSTER" ] && [ "$(shasum -a 256 "$HOMEDIR/home.toml" | cut -d' ' -f1)" = "$HOMETOML" ]; }
session_alive() { [ "$(tmx display-message -p -t "=$TNAME:" '#{pane_pid}' 2>/dev/null)" = "$PANE" ] && kill -0 "$PANE" 2>/dev/null; }
fixed_same() { [ "$(inode "$TBIN")" = "$TMUX_INODE" ] && [ "$(inode "$APP/fixed/disclaim")" = "$DISCLAIM_INODE" ] && cmp -s "$TBIN" "$TMUXD/$TARCH/tmux"; }

say "── 2d. 角色包帶衣服（DEV-157）：假的來源家建帶紀錄的角色包，新的家同步後衣櫃馬上有衣服"
# 來源家：用裝好的 evergenie 建（第 2 版、狀態資料夾在旁邊）、公開 kaguya 與 athena，服務在跑（背景行程模式）。
# 官方角色留在家裡（[layout] official_personas = "home"）：發行角色包的家都這樣（官方卡與圖是主人的原稿，home relayout 也不搬，schemas/home.md），
# 衣櫃也只在官方角色在家裡的家替官方的圖寫照片紀錄（CLO-34）；建包照照片紀錄帶圖（DEV-160），所以等衣櫃登記好再建。
# 卡照抄 repo 的（跟程式帶的一樣）；圖是假的小 PNG（剪影要解得開），每位一套送的 base（cost 0）與一套要兌換的；
# 隱藏版兩組（名字都沒有任何「成人」字眼）：跟著資料夾走的紀錄 closet.variant night 指著 avatars/night/、moonlit 指著
# wardrobe/kaguya/moonlit/（衣服資料夾本來會整個收：拿掉排除，這組就會進包）；旁邊的 wardrobe/kaguya/day.png 不是隱藏版、照收。
PPORT="$(dl_freeport)"
hgu init --home "$PUB" --name 來源 --port "$PPORT" --state "$PSTATE"
chk "建出假的來源家" '[ $RC = 0 ] && [ -f "$PUB/home.toml" ] && ! grep -q "^\[admin\]" "$PUB/home.toml"'
OUT="$(python3 -I - "$PUB/home.toml" <<'PY' 2>&1
import re, sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
s, n = re.subn(r"(?m)^\[layout\]\n", '[layout]\nofficial_personas = "home"\n', s, count=1)
assert n == 1, "home.toml 沒有 [layout]"
open(p, "w", encoding="utf-8").write(s)
PY
)"
RC=$?
printf '\n[admin]\npublic_personas = ["kaguya", "athena"]\n' >>"$PUB/home.toml"
chk "來源家的官方角色留在家裡（[layout] official_personas = \"home\"，發行角色包的家都這樣）" '[ $RC = 0 ] && grep -q "^official_personas = \"home\"" "$PUB/home.toml"'
POFF="$PUB/personas" # 來源家的官方角色資料夾（卡、avatars/、wardrobe/）
mkdir -p "$POFF/avatars/night" "$POFF/wardrobe/kaguya/moonlit"
cp "$ROOT/packages/official/personas/cards/kaguya.md" "$ROOT/packages/official/personas/cards/athena.md" "$POFF/"
python3 -I - "$POFF/avatars" <<'PY'
import os, struct, sys, zlib
def png(path, rgb):
    raw = b"".join(b"\x00" + bytes(rgb) * 8 for _ in range(8))
    chunk = lambda t, d: struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xFFFFFFFF)
    data = b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", 8, 8, 8, 2, 0, 0, 0)) + chunk(b"IDAT", zlib.compress(raw)) + chunk(b"IEND", b"")
    open(os.path.join(sys.argv[1], path), "wb").write(data)
for i, name in enumerate(["kaguya.png", "kaguya@sea.png", "athena.png", "athena@work.png", "night/kaguya.png", "night/athena@work.png",
                          "../wardrobe/kaguya/day.png", "../wardrobe/kaguya/moonlit/kaguya@moon.png"]):
    png(name, ((40 * i + 20) % 256, (200 - 30 * i) % 256, 90))
PY
# 主人自己的衣服（還沒升成官方，PKG-68）：圖在來源家的 personas/mine/avatars/，紀錄在下面跟官方的一起寫（一套已採用、一套設計中）。
mkdir -p "$PUB/personas/mine/avatars"
cp "$POFF/avatars/kaguya@sea.png" "$PUB/personas/mine/avatars/kaguya@my-24.png"
cp "$POFF/avatars/kaguya@sea.png" "$PUB/personas/mine/avatars/kaguya@my-38.png"
# 官方來源、還在設計中的一套：圖已經放在官方位置（kaguya@next.png），紀錄不帶、圖也不能進包（PRS-29）。
cp "$POFF/avatars/kaguya@sea.png" "$POFF/avatars/kaguya@next.png"
chk "放好假的小圖（四張衣服、一張衣服資料夾的圖、三張隱藏版）" '[ $RC = 0 ] && [ -s "$POFF/avatars/kaguya@sea.png" ] && [ -s "$POFF/avatars/night/kaguya.png" ] && [ -s "$POFF/wardrobe/kaguya/moonlit/kaguya@moon.png" ]'
hgu service install --home "$PUB" --exe "$APP/current/bin/evergenie"
for _ in $(seq 1 60); do [ "$(curl -s -o /dev/null -m 2 -w '%{http_code}' "http://127.0.0.1:$PPORT/" 2>/dev/null)" = 200 ] && break; sleep 1; done
chk "來源家的服務起來了（看板 200）" '[ "$(curl -s -o /dev/null -m 2 -w "%{http_code}" "http://127.0.0.1:$PPORT/")" = 200 ]'
pubhg() { as_user env EVERGENIE_HOME="$PUB" evergenie "$@"; } # 對來源家下指令
PUB_BAD=""
for rec in 'kaguya-base|{"persona":"kaguya","outfit":"base","name":"基本","cost":0,"state":"active","source":"official"}' \
  'kaguya-sea|{"persona":"kaguya","outfit":"sea","name":"海邊","cost":5,"state":"active","source":"official"}' \
  'athena-base|{"persona":"athena","outfit":"base","name":"基本","cost":0,"state":"active","source":"official"}' \
  'athena-work|{"persona":"athena","outfit":"work","name":"工作服","cost":3,"state":"active","source":"official"}' \
  'kaguya-my-24|{"persona":"kaguya","outfit":"my-24","name":"主人的私服","cost":0,"state":"active","source":"mine"}' \
  'kaguya-my-38|{"persona":"kaguya","outfit":"my-38","name":"主人設計中的","cost":0,"state":"draft","source":"mine"}' \
  'kaguya-next|{"persona":"kaguya","outfit":"next","name":"官方設計中的","cost":0,"state":"draft","source":"official"}'; do
  pubhg data add closet.outfit --id "${rec%%|*}" --json "${rec#*|}"
  [ $RC = 0 ] || PUB_BAD="$PUB_BAD ${rec%%|*}：$OUT"
done
for rec in 'night|{"name":"夜","dir":"avatars/night","button":true}' 'moonlit|{"name":"月","dir":"wardrobe/kaguya/moonlit","button":true}'; do
  pubhg data add closet.variant --id "${rec%%|*}" --json "${rec#*|}"
  [ $RC = 0 ] || PUB_BAD="$PUB_BAD ${rec%%|*}：$OUT"
done
OUT="$PUB_BAD"
chk "來源家寫好衣服目錄（官方四套、主人自己的兩套、官方設計中的一套）與隱藏版（closet.variant night、moonlit）" '[ -z "$PUB_BAD" ]'
# 建包只照照片紀錄（closet.photo）帶圖（DEV-160）：衣櫃看到衣服目錄有變就替圖補登記，等每一套都登記好（官方四套與設計中那套各有照片紀錄）再建。
# 還沒登記就建，包裡會有衣服、沒有圖：建包會拒絕、說是哪幾套（PKG-70），不會靜靜少帶。
photo_outfits() { pubhg data list closet.photo && awk '{print $1}' <<<"$OUT" | sed -n 's/--.*$//p' | sort -u | tr '\n' ' '; }
for _ in $(seq 1 60); do [ "$(photo_outfits)" = "athena-base athena-work kaguya-base kaguya-my-24 kaguya-my-38 kaguya-next kaguya-sea " ] && break; sleep 0.5; done
PHOTO_OUTFITS="$(photo_outfits)"
OUT="$PHOTO_OUTFITS"
chk "衣櫃替每一套的圖登記好照片紀錄（官方四套、主人自己的兩套、官方設計中的一套）" '[ "$PHOTO_OUTFITS" = "athena-base athena-work kaguya-base kaguya-my-24 kaguya-my-38 kaguya-next kaguya-sea " ]'
pubhg personas pack build --out "$WWW/pack" --version 2026.11.14 --json
BUILD_OUT="$OUT"
PACK_SHA="$(dl_pyj 'd["manifest_sha256"]' <<<"$BUILD_OUT" 2>/dev/null)"
chk "帶紀錄建包：2 位角色、8 筆紀錄（四套衣服、四張照片）、說 3 筆還沒升成官方或設計中的衣服與 3 張主人自己的或沒採用的照片沒帶、它們的圖、排除隱藏版的資料夾 avatars/night、wardrobe/kaguya/moonlit（總結那一行）" '[ $RC = 0 ] && [ -n "$PACK_SHA" ] && grep -q "角色包 2026.11.14：2 位角色、[0-9]* 個檔案、8 筆紀錄（3 筆還沒升成官方或還在設計中的衣服沒帶：kaguya-my-24、kaguya-my-38、kaguya-next）（3 筆主人自己的、隱藏版或還沒採用的照片沒帶：kaguya-my-24--half1、kaguya-my-38--half1、kaguya-next--half1）（它們的圖 1 個檔也沒收）；排除的資料夾（跟著資料夾走的紀錄指的，例如隱藏版）：avatars/night、wardrobe/kaguya/moonlit" <<<"$BUILD_OUT"'
OUT="$(python3 -I - "$WWW/pack/m/2026.11.14.json" <<'PY'
import json, sys
m = json.load(open(sys.argv[1], encoding="utf-8"))
print(" ".join(sorted([f["path"] for f in m["files"]] + [r["kind"] + "/" + r["id"] for r in m.get("records") or []])))
PY
)"
PACK_LIST="$OUT"
chk "包裡有兩張卡、四張圖、衣服資料夾的那張、四筆衣服與它們的四筆照片紀錄；沒有隱藏版的圖與紀錄，也沒有主人自己的與設計中的衣服（紀錄與圖）" '[ "$PACK_LIST" = "avatars/athena.png avatars/athena@work.png avatars/kaguya.png avatars/kaguya@sea.png cards/athena.md cards/kaguya.md closet.outfit/athena-base closet.outfit/athena-work closet.outfit/kaguya-base closet.outfit/kaguya-sea closet.photo/athena-base--half1 closet.photo/athena-work--half1 closet.photo/kaguya-base--half1 closet.photo/kaguya-sea--half1 wardrobe/kaguya/day.png" ]'
OUT="$(grep -oE 'my-(24|38)|kaguya-next|@next|主人的私服|主人設計中的|官方設計中的' "$WWW/pack/m/2026.11.14.json" | sort -u | tr '\n' ' ')"
chk "包的清單裡連主人自己的與設計中的衣服的編號、名稱、圖都沒有" '[ -z "$OUT" ]'
hgu service uninstall --home "$PUB"
chk "建完收掉來源家的服務" '[ $RC = 0 ]'
printf 'version = "2026.11.14"\nurl = "%s/pack/m/2026.11.14.json"\nsha256 = "%s"\n' "$SRC" "$PACK_SHA" >"$B/ptr-pack.toml"
as_user env EVERGENIE_PERSONAS_PACK_POINTER="$B/ptr-pack.toml" evergenie personas pack sync --json
SYNC_OUT="$OUT"
chk "新的家同步（交給服務做）：synced、紀錄新增 8 筆（四套衣服、四張照片）、沒有「紀錄晚點補」" '[ $RC = 0 ] && [ "$(dl_pyj "d.get(\"status\")" <<<"$SYNC_OUT")" = synced ] && [ "$(dl_pyj "len(d.get(\"records_added\") or [])" <<<"$SYNC_OUT")" = 8 ] && [ "$(dl_pyj "bool(d.get(\"records_later\"))" <<<"$SYNC_OUT")" = False ]'
# 衣櫃的程式看到官方層寫進來的衣服（變更事件）就重算：不用等整點或五分鐘那一輪
owned_of() { api vault/list '{"kind":"closet.person"}' && dl_pyj '" ".join(sorted("%s:%d/%d" % (r["id"], sum(1 for o in r["fields"].get("outfits") or [] if o.get("owned")), len(r["fields"].get("outfits") or [])) for r in d.get("records") or [] if r["id"] in ("kaguya", "athena")))' <<<"$OUT"; }
for _ in $(seq 1 30); do [ "$(owned_of)" = "athena:1/2 kaguya:1/2" ] && break; sleep 0.5; done
OWNED="$(owned_of)"
OUT="$OWNED"
chk "衣櫃馬上有衣服：每位角色兩套、各擁有一套（送的 base）（${OWNED}）" '[ "$OWNED" = "athena:1/2 kaguya:1/2" ]'
CPUB="$STATE/runtime/packages/closet/public"
chk "展示間：擁有的有真圖、沒兌換的只有剪影（公開資料夾裡沒有它的真圖）" '[ -n "$(ls "$CPUB/o/kaguya/base/" 2>/dev/null)" ] && [ -s "$CPUB/s/kaguya/sea/1.png" ] && [ ! -e "$CPUB/o/kaguya/sea" ] && [ -s "$CPUB/s/athena/work/1.png" ] && [ ! -e "$CPUB/o/athena/work" ]'
OUT="$(curl -s -o /dev/null -m 5 -w '%{http_code}' "${BOARD}avatars/persona/kaguya.png")"
chk "kaguya 的頭像 200" '[ "$OUT" = 200 ]'
api vault/list '{"kind":"closet.outfit"}'
NEW_RECS="$OUT"
api vault/list '{"kind":"closet.photo"}'
OUT="$(grep -oE 'kaguya-(my-[0-9]+|next)' <<<"$NEW_RECS$OUT" | sort -u | tr '\n' ' ')$(find "$STATE/personas" "$HOMEDIR/personas" \( -name '*@my-*' -o -name '*@next*' \) 2>/dev/null)"
chk "新的家沒有來源家還沒公開的衣服：沒有 kaguya-my-24、kaguya-my-38、kaguya-next 的衣服紀錄、照片紀錄與圖" '[ -z "$OUT" ]'
api vault/list '{"kind":"closet.variant"}'
VARIANTS="$OUT"
api vault/list '{"kind":"closet.person"}'
PERSONS="$OUT"
OUT="$(find "$STATE/personas" "$HOMEDIR/personas" \( -path '*night*' -o -path '*moonlit*' \) 2>/dev/null) $(grep -oE '"(night|moonlit)"' <<<"$VARIANTS$PERSONS")"
chk "新的家沒有隱藏版：沒有 night、moonlit 的圖與 closet.variant，衣櫃也沒列這兩組；衣服資料夾的那張有" '[ "$OUT" = " " ] && [ -s "$STATE/personas/wardrobe/kaguya/day.png" ]'

say "── 3. 升到 0.0.2（看板：檢查更新 → 現在更新）"
dl_publish "$WWW" "$DIST/0.0.2" "$ARCH"
press_update 0.0.2
INFO="$OUT"
chk "檢查更新看到 0.0.2、按現在更新、跑完是「已經更新」" '[ "$(result_of "$INFO")" = "done - False" ]'
chk "版本變了：看板說現在是 0.0.2、current 指向 versions/0.0.2" '[ "$(upd_field "d.get(\"current\")")" = 0.0.2 ] && [ "$(current)" = versions/0.0.2 ]'
chk "看板 200" 'wait_board 30'
chk "資料還在：名冊（${AGENT} 上班中）、home.toml 一個字都沒變" 'data_same'
chk "秘書的 session 還在（同一個 tmux、同一個 claude）" 'session_alive'
chk "固定位置的 tmux 與啟動程式沒被換（inode 一樣、內容一樣）" 'fixed_same'
chk "舊版的資料夾留著（還能退回），兩版都在" '[ "$(versions_list)" = "0.0.1 0.0.2 " ]'
OUT="$(ls "$STATE/runtime/backups/" 2>&1)"
chk "升級前備份了你的資料（runtime/backups/upgrade-0.0.1-0.0.2.tgz，下次更新成功才換掉）" '[ -s "$STATE/runtime/backups/upgrade-0.0.1-0.0.2.tgz" ]'

say "── 4a. 下載到壞檔（檢查碼不符）：舊版照常"
PID0="$(serve_pid)"
dl_publish "$WWW" "$DIST/0.0.3" "$ARCH" badsha
press_update 0.0.3
INFO="$OUT"
chk "結果是「舊版照常在跑」（outcome untouched）" '[ "$(result_of "$INFO")" = "failed untouched False" ]'
plain_ok "檢查碼不符" "$INFO"
chk "還是 0.0.2、服務沒重開（同一個行程）、沒把 0.0.3 記成跳過" '[ "$(current)" = versions/0.0.2 ] && [ "$(serve_pid)" = "$PID0" ] && ! skipped 0.0.3 && [ "$(versions_list)" = "0.0.1 0.0.2 " ]'

say "── 4b. 新版起不來（0.0.3 的服務一開就掛）：自動換回、資料沒動（要等新版的檢查逾時，約一分鐘）"
dl_publish "$WWW" "$DIST/0.0.3" "$ARCH"
press_update 0.0.3
INFO="$OUT"
chk "結果是「已換回、你的資料沒動」（rolled_back、沒有還原資料）" '[ "$(result_of "$INFO")" = "rolled_back rolled_back False" ]'
plain_ok "新版起不來" "$INFO"
chk "換回 0.0.2：看板說現在是 0.0.2、current 指回 versions/0.0.2、看板 200" '[ "$(upd_field "d.get(\"current\")")" = 0.0.2 ] && [ "$(current)" = versions/0.0.2 ] && wait_board 30'
chk "0.0.3 記成跳過；資料沒動；秘書的 session 還在；固定位置的 tmux 沒換" 'skipped 0.0.3 && data_same && session_alive && fixed_same'
chk "壞版印的英文只在服務日誌、沒進看板的原因" 'grep -q "panic: e2e broken build" "$STATE/runtime/logs/serve.log" && ! grep -q panic <<<"$INFO"'

say "── 4c. 換不回去（0.0.4 讓舊版讀不到）：要處理 → 排除後按「退回上一版」救回來"
BINMODE="$(stat -f %Lp "$APP/versions/0.0.2/bin")"
dl_publish "$WWW" "$DIST/0.0.4" "$ARCH"
press_update 0.0.4
INFO="$OUT"
chk "結果是「也沒能換回，要處理」（stranded）、看板可以按「退回上一版」" '[ "$(result_of "$INFO")" = "failed stranded False" ] && [ "$(dl_pyj "d.get(\"rescue\", False)" <<<"$INFO")" = True ]'
plain_ok "換不回去" "$INFO"
chk "下一步是按「退回上一版」" 'grep -q "退回上一版" <<<"$(next_of "$INFO")"'
chk "這時看板還開得起來（0.0.4 在跑）" 'wait_board 30 && [ "$(current)" = versions/0.0.4 ]'
chmod "$BINMODE" "$APP/versions/0.0.2/bin" # 排除原因（秘書把權限改回來）
BEFORE="$(started_at)"
api system/update_rollback '{}'
chk "按「退回上一版」（update_rollback）" '[ $RC = 0 ]'
wait_update rescue "$BEFORE"
INFO="$OUT"
chk "救回來了：結果是「已換回」（rescue、rolled_back）、不用再處理" '[ "$(result_of "$INFO")" = "rolled_back rolled_back False" ] && [ "$(dl_pyj "d.get(\"rescue\", False)" <<<"$INFO")" = False ]'
chk "回到 0.0.2：看板說現在是 0.0.2、current 指回 versions/0.0.2、看板 200" '[ "$(upd_field "d.get(\"current\")")" = 0.0.2 ] && [ "$(current)" = versions/0.0.2 ] && wait_board 30'
chk "0.0.4 記成跳過；資料沒動；秘書的 session 還在；固定位置的 tmux 沒換" 'skipped 0.0.4 && data_same && session_alive && fixed_same'

say "── 5. 確認新版（upgrade --accept，不用問主人）：舊版清掉、升級前的備份留著（只留最新一份）"
hgu upgrade --accept
chk "upgrade --accept 成功" '[ $RC = 0 ]'
OUT="$OUT
（versions：$(versions_list)；backups：$(ls "$STATE/runtime/backups/" 2>/dev/null | tr '\n' ' ')）"
chk "versions 只剩 0.0.2（0.0.1 與壞掉的 0.0.3、0.0.4 都收掉了）、這次升級前的備份還在" '[ "$(versions_list)" = "0.0.2 " ] && [ -e "$STATE/runtime/backups/upgrade-0.0.1-0.0.2.tgz" ]'
chk "看板 200、還是 0.0.2" 'wait_board 30 && [ "$(upd_field "d.get(\"current\")")" = 0.0.2 ]'

say "── 6. 移除（照 ai_docs/runbooks/install.md「移除」）"
SPID="$(serve_pid)"
hgu service uninstall
chk "① evergenie service uninstall：服務停了、看板關了" '[ $RC = 0 ] && ! kill -0 "${SPID:-0}" 2>/dev/null && [ "$(board_code)" != 200 ]'
as_user "$APP/fixed/$PRODUCT" -L eg kill-server
sleep 1
chk "② 關掉秘書的 tmux（固定位置那支 -L eg kill-server）：server 與裡面的 claude 都停了" '[ $RC = 0 ] && ! kill -0 "${TSERVER:-0}" 2>/dev/null && ! kill -0 "${PANE:-0}" 2>/dev/null && ! tmx list-sessions >/dev/null 2>&1'
LINK="$(readlink "$FH/.local/bin/evergenie")"
as_user /bin/rm -f "$FH/.local/bin/evergenie"
chk "③ ~/.local/bin/evergenie 是指到程式資料夾的連結，刪掉" '[ "$LINK" = "$APP/current/bin/evergenie" ] && [ ! -e "$FH/.local/bin/evergenie" ] && [ ! -L "$FH/.local/bin/evergenie" ]'
as_user /bin/sh -c 'rm -rf "$HOME/.evergenie/app" "$HOME/.evergenie/state/evergenie-$USER" && rmdir "$HOME/.evergenie/state" "$HOME/.evergenie"'
chk "④ 刪程式資料夾與狀態資料夾，~/.evergenie 空了也刪" '[ $RC = 0 ] && [ ! -e "$FH/.evergenie" ]'
OUT="$(cd "$FH" && find . -mindepth 1 ! -path "./evergenie-$ME" ! -path "./evergenie-$ME/*" | LC_ALL=C sort | tr '\n' ' ')"
chk "假 HOME 只剩原本的（Claude 的檔）與主人的資料（家留著）" '[ "$OUT" = "./.claude.json ./.local ./.local/bin ./.local/bin/claude " ] && [ -f "$HOMEDIR/home.toml" ]'
OUT="$(pgrep -fl -- "$FH/")"
# 服務是背景行程（EVERGENIE_SERVICE_MODE=process），launchd 那一半（bootout、刪 plist）測不到，靠 host 模組的單元測試（HST-1）
chk "沒有留下任何行程（服務、引擎、tmux、claude）、假 HOME 裡沒有服務設定（Library/LaunchAgents）" '[ -z "$OUT" ] && [ ! -e "$FH/Library/LaunchAgents" ]'
OUT="$(cat "$TRIP" 2>/dev/null)"
chk "整條路沒叫到 git、make、python3、編譯器（絆線紀錄是空的）" '[ -z "$OUT" ]'

say ""
# 交辦要求整支五分鐘內跑完（含編四個版本）：超過就提醒（不算沒過；機器很忙時會慢）
[ "$SECONDS" -gt 300 ] && say "（提醒：整支跑了 ${SECONDS} 秒，超過五分鐘；機器忙的話正常，不忙就要看哪一段變慢了）"
if [ "$FAILS" = 0 ]; then
  say "乾淨環境整體驗收：全部通過（$((SECONDS - T0)) 秒）"
  exit 0
fi
say "乾淨環境整體驗收：${FAILS} 項沒過（$((SECONDS - T0)) 秒）"
exit 1
