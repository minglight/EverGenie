#!/usr/bin/env bash
# 下載版真的編得出主人自己的附程式套件（DEV-155，packages PKG-65、PKG-66）：用 scripts/dist.sh 編 0.0.1、0.0.2 兩版、本機 http 當下載點，
# 手動裝成下載版（<程式>/versions/<版本>/＋current），在假 HOME、只有系統內建工具的 PATH（沒有 go、git、make、python3；git、make、
# python3、編譯器換成一叫就失敗並記下來的絆線）跑背景服務，放一個最小的附程式套件（kind = "program"，go.mod 只有 module 與 go 兩行）：
#   1. 缺 Go → 套件停下來問（failed、補救代號 install_go、白話問一句）
#   2. 主人同意 → install-go --confirmed：**真的從 Go 官方（go.dev）下載寫死的那一版**、對過檢查碼放進 <程式>/tools/go
#   3. 用程式旁邊附的 sdk 編譯成功（工作區 go.work 指到 versions/0.0.1/sdk）→ 套件 running；家目錄沒多 go 資料夾，快取在 <程式>/tools/
#   4. 升到 0.0.2 → 用 0.0.2 附的 sdk 重編、還是 running
#   5. 套件的 go.mod 改成從官方套件 fork 來的寫法（require sdk v0.0.0＋replace 到不存在的路徑）、再用一個外部模組 → 照樣編好
#      （工作區對那個版本加 replace 指到附的 sdk；外部模組從公開的模組站抓到 <程式>/tools/gopath），套件資料夾一個檔都沒多
#   6. 失敗兩種是白話、不夾 go 的英文原文：附的 sdk 不見了；套件的 go.mod 要的 Go 比這台的新。改回來又 running。
# 家資料夾有 .git（家放進 git 的人）：go 不能去叫 git（-buildvcs=false）。最後看絆線：整條路沒叫到 git、make、python3、編譯器。
#
#   scripts/package-go-test.sh [--help]
#
# **要網路**：連 go.dev 下載 Go 一次（約 70MB，放在暫存資料夾的程式資料夾裡、測完刪），第 5 步連 proxy.golang.org 抓一個小模組；
# 其他步驟編譯不連網。
# 不進 `make ci`；`make package-go-test` 叫它，`make release-e2e` 也跑。全程在 mktemp -d 的暫存資料夾、EVERGENIE_SERVICE_MODE=process
# （背景行程，不碰 launchd），不碰任何家。結束碼 0＝全部通過；1＝有沒過的（印 ✘ 與最近一次指令的輸出）；2＝用法錯誤。
# 環境變數：EVERGENIE_E2E_KEEP=1 保留暫存資料夾；EVERGENIE_E2E_TMP 暫存資料夾放哪（預設 /tmp，要短：socket 路徑上限約 104 字元）；EVERGENIE_E2E_WAIT 等看板的秒數（預設 180）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
# macOS 內建的是 bash 3.2：變數後面緊接中文要加大括號（${x}）。

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi
if [ $# -gt 0 ]; then
  echo "不認得的選項：${1}（--help 看用法）" >&2
  exit 2
fi
for tool in go python3 curl shasum tar; do command -v "$tool" >/dev/null 2>&1 || { echo "這台沒有 ${tool}，跑不了（編發行檔與檢查要用）" >&2; exit 1; }; done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
. "$ROOT/scripts/lib/download-e2e.sh"
B="$(dl_mktemp "${EVERGENIE_E2E_TMP:-/tmp}" hgg)" || { echo "建不出暫存資料夾（在 ${EVERGENIE_E2E_TMP:-/tmp} 底下）" >&2; exit 1; }
WAIT="${EVERGENIE_E2E_WAIT:-180}"
case "$(uname -m)" in arm64) ARCH=arm64 ;; x86_64) ARCH=amd64 ;; *) echo "不認得這台的晶片：$(uname -m)" >&2; exit 1 ;; esac
ME="$(id -un)"
FH="$B/h" APP="$B/h/app" HOMEDIR="$B/h/home" STATE="$B/h/state" WWW="$B/www" SAFE="$B/safe" TRIP="$B/trip.log"
mkdir -p "$FH" "$B/t" "$WWW"
SDKMOD="$(sed -n 's/^module //p' "$ROOT/sdk/go.mod")" # sdk 的模組路徑（改名 DEV-158 時跟著 sdk/go.mod）
PKG="$HOMEDIR/packages/mine/gotest"
WWW_PID=""

# 收掉這次開的行程：照 pid 檔收（確認那個 pid 的指令列帶著這個暫存資料夾的路徑才收，不用名稱比對）；服務自成一個行程群組，整組送 TERM，
# 10 秒還在就整組 KILL。
own() { ps -o command= -p "$1" 2>/dev/null | grep -qF "$B/"; }
cleanup() {
  local p i
  p="$(tr -d ' \n' 2>/dev/null <"$STATE/runtime/serve.pid")"
  case "$p" in '' | *[!0-9]*) p="" ;; esac
  if [ -n "$p" ] && own "$p"; then
    kill -TERM -- "-$p" 2>/dev/null || kill -TERM "$p" 2>/dev/null
    for ((i = 0; i < 40; i++)); do kill -0 "$p" 2>/dev/null || break; sleep 0.25; done
    own "$p" && { kill -KILL -- "-$p" 2>/dev/null || kill -KILL "$p" 2>/dev/null; }
  fi
  [ -n "$WWW_PID" ] && kill "$WWW_PID" 2>/dev/null
  if [ "${EVERGENIE_E2E_KEEP:-0}" = 1 ]; then echo "（保留暫存資料夾：${B}）"; else chmod -R u+rwx "$B" 2>/dev/null; rm -rf "$B"; fi
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
      say "      --- 最近一次指令的輸出（結束碼 ${RC}）---"
      printf '%s\n' "$OUT" | tail -25 | sed 's/^/      /'
    fi
  fi
}
stop_here() { say "  （$1，後面不測了）"; say "下載版編主人的附程式套件：沒過（$((SECONDS - T0)) 秒）"; exit 1; }
run() { OUT="$("$@" 2>&1)"; RC=$?; }
pyj() { dl_pyj "$1"; }

# 主人那台的環境：env -i、假 HOME、只有系統內建工具的 PATH（外面設的 GOFLAGS、GOPROXY、GOPATH 都帶不進來）。
# EVERGENIE_GO_DOWNLOAD_PIN 指到不存在的檔：以前的測試開關，正式程式要不理它（照樣從 Go 官方下載寫死的那一版，packages PKG-65）
USER_ENV=(env -i HOME="$FH" USER="$ME" LOGNAME="$ME" SHELL=/bin/zsh PATH="$SAFE" TMPDIR="$B/t" LANG=en_US.UTF-8
  EVERGENIE_SERVICE_MODE=process EVERGENIE_SITE=e2e EVERGENIE_BACKUP_DIR="$B/backup" EVERGENIE_GO_DOWNLOAD_PIN="$B/沒有這個檔.json")
hgx() { "${USER_ENV[@]}" "$APP/current/bin/evergenie" "$@"; }
hg() { "${USER_ENV[@]}" EVERGENIE_HOME="$HOMEDIR" "$APP/current/bin/evergenie" "$@"; }
board() { curl -s -o /dev/null -m 5 -w '%{http_code}' "http://127.0.0.1:$PORT/" 2>/dev/null; }
wait_board() {
  local i
  for ((i = 0; i < WAIT; i++)); do
    [ "$(board)" = 200 ] && return 0
    sleep 1
  done
  return 1
}
current() { readlink "$APP/current"; }
pkg_field() { hg packages get gotest 2>/dev/null | pyj "d.get('$1', '')" 2>/dev/null; }
# wait_pkg <state> [秒]：等套件到那個狀態（編譯第一次要編標準函式庫，給久一點）
wait_pkg() {
  local i
  for ((i = 0; i < ${2:-300}; i++)); do
    [ "$(pkg_field state)" = "$1" ] && return 0
    sleep 1
  done
  return 1
}
# sdk_of <版本>：那一版附的 sdk 的真實路徑
sdk_of() { (cd "$APP/versions/$1/sdk" 2>/dev/null && pwd -P); }
GOWORKF="$STATE/runtime/packages/gotest/go.work"
STAMP="$STATE/runtime/packages/gotest/build.stamp"
# 套件錯誤的白話裡除了工具名（Go、MB、sdk）與 go.mod 的寫法（go、mod）以外的英文字；go 的英文原文（requires go、running go……）
plain_extra() { dl_english_in | tr ' ' '\n' | awk '$0 != "" && $0 != "Go" && $0 != "MB" && $0 != "sdk" && $0 != "go" && $0 != "mod"' | tr '\n' ' '; }
GO_RAW='requires go|running go|GOTOOLCHAIN|no required module|cannot find|undefined:|go: |\.go:[0-9]'
# 家目錄裡 Go 會建的東西（GOPATH 預設的 ~/go、編譯快取 ~/Library/Caches/go-build）
home_go() { /bin/ls -d "$FH/go" "$FH/Library/Caches/go-build" 2>/dev/null | tr '\n' ' '; }
# wait_rebuilt <上次的指紋>：等到用新的內容編好（指紋換了）；這一次編不過（「編譯失敗」）就不等了
wait_rebuilt() {
  local i
  for ((i = 0; i < 300; i++)); do
    [ -n "$(cat "$STAMP" 2>/dev/null)" ] && [ "$(cat "$STAMP" 2>/dev/null)" != "$1" ] && [ "$(pkg_field state)" = running ] && return 0
    pkg_field error | grep -q 編譯失敗 && return 1
    sleep 1
  done
  return 1
}

T0=$SECONDS
PORT="$(dl_freeport)" WPORT="$(dl_freeport)"
SRC="http://127.0.0.1:$WPORT/manifest.json"

say "── 準備：編兩個版本（scripts/dist.sh，darwin-${ARCH}；--no-tmux：這裡不開秘書的對話）"
for v in 0.0.1 0.0.2; do
  run env -u EVERGENIE_HOME -u EVERGENIE_API_SOCKET -u EVERGENIE_ENGINE_SOCKET -u EVERGENIE_AGENT -u EVERGENIE_LAUNCH -u EVERGENIE_SESSION \
    "$ROOT/scripts/dist.sh" --version "$v" --arch "$ARCH" --out "$B/dist/$v" --source "$SRC" --no-tmux
  chk "dist.sh 編 $v" '[ $RC = 0 ] && [ -s "$B/dist/$v/manifest.json" ]'
  [ $RC = 0 ] || exit 1
done

dl_safebin "$SAFE" "$TRIP"
OUT="$(PATH="$SAFE" command -v go gofmt brew 2>&1)"
chk "主人的 PATH 裡沒有 go、gofmt、brew（git、make、python3、編譯器是絆線）" '[ -z "$OUT" ]'

dl_publish "$WWW" "$B/dist/0.0.1" "$ARCH"
python3 -I -m http.server "$WPORT" --bind 127.0.0.1 --directory "$WWW" >"$B/www.log" 2>&1 &
WWW_PID=$!
for _ in $(seq 1 50); do curl -s -o /dev/null "$SRC" && break; sleep 0.2; done
OUT="$(curl -s "$SRC")"
chk "本機 http 提供版本清單" 'grep -q "\"version\": *\"0.0.1\"" <<<"$OUT"'

say "── 安裝：手動裝成下載版 0.0.1、建家、背景服務"
mkdir -p "$APP/versions/0.0.1" &&
  tar xzf "$B/dist/0.0.1/evergenie-0.0.1-darwin-$ARCH.tar.gz" -C "$APP/versions/0.0.1" --strip-components 1 &&
  ln -s versions/0.0.1 "$APP/current"
chk "versions/0.0.1、current，包裡附了 sdk 原始碼" '[ "$(current)" = versions/0.0.1 ] && [ -x "$APP/current/bin/evergenie" ] && [ "$(sed -n "s/^module //p" "$APP/versions/0.0.1/sdk/go.mod")" = "$SDKMOD" ]'
run hgx init --home "$HOMEDIR" --name 測試 --port "$PORT" --timezone Asia/Taipei --state "$STATE"
chk "init 建家" '[ $RC = 0 ]'
[ $RC = 0 ] || stop_here "家建不起來"
run hgx service install --home "$HOMEDIR" --exe "$APP/current/bin/evergenie"
chk "service install（背景行程）" '[ $RC = 0 ]'
chk "看板 200" 'wait_board'

say "── 主人自己的附程式套件（go.mod 只有 module 與 go 兩行，不寫 require、replace）"
mkdir -p "$PKG/cmd/gotest" &&
  printf '%s\n' 'schema = 1' 'id = "gotest"' 'name = "測試套件"' 'version = "1.0.0"' 'description = "要編譯 Go 的測試套件"' 'kind = "program"' \
    'api = "evergenie.v1"' '[capabilities]' 'read = ["vault:pkg/gotest"]' 'write = ["vault:pkg/gotest"]' '[program]' 'entry = "cmd/gotest"' \
    >"$PKG/package.toml" &&
  printf 'module e2e/gotest\n\ngo 1.24\n' >"$PKG/go.mod" &&
  cat >"$PKG/cmd/gotest/main.go" <<EOF
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"$SDKMOD"
)

type prog struct{}

func (prog) Run(ctx context.Context, _ *sdk.PackageHost) error { <-ctx.Done(); return nil }

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := sdk.ServePackage(ctx, prog{}); err != nil {
		os.Exit(1)
	}
}
EOF
RC=$?
chk "放好 packages/mine/gotest" '[ $RC = 0 ]'
mkdir -p "$HOMEDIR/.git" # 家放進 git 的人：go 預設會叫 git 記版本資訊（乾淨的 Mac 上 git 是一叫就跳安裝視窗的空殼，這裡是絆線）
GOMOD0="$(cat "$PKG/go.mod")"
run hg packages reload --id gotest
for _ in $(seq 1 60); do [ "$(pkg_field remedy)" = install_go ] && break; sleep 0.5; done
run hg packages get gotest
chk "缺 Go：套件停下來（failed、install_go）、白話問一句要不要裝" '[ "$(pyj '"'"'d.get("state", "") + " " + d.get("remedy", "")'"'"' <<<"$OUT")" = "failed install_go" ] && grep -q "要我裝嗎？" <<<"$OUT"'
run hg packages go-status
GOVER="$(pyj 'd.get("offer", "")' <<<"$OUT" 2>/dev/null)"
chk "go-status：結束碼 3、可以幫忙下載（${GOVER:-沒有}）" '[ $RC = 3 ] && [ "$(pyj '"'"'str(d["installable"])'"'"' <<<"$OUT")" = True ] && [ -n "$GOVER" ]'
chk "沒問之前什麼都沒下載" '[ ! -e "$APP/tools/go" ]'

say "── 主人同意：從 Go 官方真的下載 ${GOVER}（要網路，約 70MB）"
run hg packages install-go --confirmed
chk "install-go --confirmed：成功、裝的是 ${GOVER}" '[ $RC = 0 ] && [ "$(pyj '"'"'d.get("version", "")'"'"' <<<"$OUT")" = "$GOVER" ]'
if [ $RC != 0 ]; then
  say "      （下載 Go 要連得到 go.dev；上面的白話原因說了卡在哪）"
  stop_here "Go 沒裝好"
fi
OUT="$("$APP/tools/go/bin/go" version 2>&1)"
chk "tools/go 是真的 Go ${GOVER}（darwin/${ARCH}）" 'grep -q "^go version $GOVER darwin/$ARCH" <<<"$OUT"'
wait_pkg running
OUT="$(hg packages get gotest 2>&1)"
chk "用程式旁邊附的 sdk 編好、套件 running" '[ "$(pkg_field state)" = running ] && [ -x "$STATE/runtime/packages/gotest/bin/gotest" ]'
OUT="$(cat "$GOWORKF" 2>&1)"
chk "工作區 go.work 用 0.0.1 附的 sdk 與套件資料夾" 'grep -qF "\"$(sdk_of 0.0.1)\"" "$GOWORKF" && grep -qF "/packages/mine/gotest\"" "$GOWORKF"'
chk "套件資料夾沒被動（go.mod 一樣、沒多 go.sum、go.work）" '[ "$(cat "$PKG/go.mod")" = "$GOMOD0" ] && [ ! -e "$PKG/go.sum" ] && [ ! -e "$PKG/go.work" ]'
OUT="$(home_go)"
chk "家目錄沒多 go 資料夾、沒留編譯快取（在 <程式>/tools/gocache）" '[ -z "$OUT" ] && [ -d "$APP/tools/gocache" ]'
STAMP1="$(cat "$STAMP" 2>/dev/null)"

say "── 升到 0.0.2：用 0.0.2 附的 sdk 重編"
dl_publish "$WWW" "$B/dist/0.0.2" "$ARCH"
run hg upgrade --apply
chk "upgrade --apply 成功、current 是 0.0.2" '[ $RC = 0 ] && [ "$(current)" = versions/0.0.2 ]'
chk "看板 200" 'wait_board'
wait_pkg running
OUT="$(hg packages get gotest 2>&1)"
chk "升級之後套件 running" '[ "$(pkg_field state)" = running ]'
OUT="$(cat "$GOWORKF" 2>&1)"
chk "重編過（指紋換了）、工作區換成 0.0.2 附的 sdk" '[ -n "$STAMP1" ] && [ "$(cat "$STAMP" 2>/dev/null)" != "$STAMP1" ] && grep -qF "\"$(sdk_of 0.0.2)\"" "$GOWORKF" && ! grep -qF /versions/0.0.1/ "$GOWORKF"'
chk "Go 還是工具資料夾那一支（升級沒動它）" '[ "$(hg packages go-status 2>/dev/null | pyj '"'"'d.get("source", "")'"'"')" = tools ]'

say "── 套件 go.mod 是從官方套件 fork 來的寫法（require sdk v0.0.0＋replace 到不存在的路徑）、再用一個外部模組（要網路：proxy.golang.org）"
STAMP2="$(cat "$STAMP" 2>/dev/null)"
printf 'module e2e/gotest\n\ngo 1.24\n\nrequire (\n\t%s v0.0.0\n\tgolang.org/x/text v0.3.0\n)\n\nreplace %s => ../../../sdk\n' "$SDKMOD" "$SDKMOD" >"$PKG/go.mod"
printf 'package main\n\nimport _ "golang.org/x/text/language"\n' >"$PKG/cmd/gotest/text.go"
GOMOD1="$(cat "$PKG/go.mod")"
run hg packages reload --id gotest
wait_rebuilt "$STAMP2"
OUT="$(hg packages get gotest 2>&1)"
chk "fork 來的 go.mod＋外部模組：編好、running" '[ "$(pkg_field state)" = running ] && [ "$(cat "$STAMP" 2>/dev/null)" != "$STAMP2" ] && [ -z "$(pkg_field error)" ]'
OUT="$(cat "$GOWORKF" 2>&1)"
chk "工作區對 go.mod 寫的 sdk v0.0.0 加 replace 指到 0.0.2 附的 sdk" 'grep -qF "$SDKMOD v0.0.0 => \"$(sdk_of 0.0.2)\"" "$GOWORKF"'
OUT="$(/bin/ls "$APP/tools/gopath/pkg/mod/golang.org/x" 2>&1; /bin/ls "$STATE/runtime/packages/gotest" 2>&1)"
chk "外部模組抓到 <程式>/tools/gopath、檢查碼記在 runtime 的 go.work.sum" '[ -d "$APP/tools/gopath/pkg/mod/golang.org/x/text@v0.3.0" ] && [ -s "$STATE/runtime/packages/gotest/go.work.sum" ]'
chk "套件資料夾沒被動（go.mod 一樣、沒多 go.sum、go.work）" '[ "$(cat "$PKG/go.mod")" = "$GOMOD1" ] && [ ! -e "$PKG/go.sum" ] && [ ! -e "$PKG/go.work" ]'
STAMP2="$(cat "$STAMP" 2>/dev/null)" # 改檔之前記（核心偵測到存檔也會自己重編）
rm -f "$PKG/cmd/gotest/text.go"
printf '%s\n' "$GOMOD0" >"$PKG/go.mod"
run hg packages reload --id gotest
wait_rebuilt "$STAMP2"
chk "改回只有 module 與 go 兩行：又編好、running" '[ "$(pkg_field state)" = running ] && [ "$(cat "$STAMP" 2>/dev/null)" != "$STAMP2" ]'

say "── 失敗的兩種是白話"
# fail_case <說明> <錯誤要有的字> <狀態>：等到這一次的錯，看狀態、白話、沒有補救代號、沒有 go 的英文原文。
# 跑著的套件重編失敗會繼續跑舊的（running＋「仍在跑舊版」）；沒在跑的編不過是 failed。
fail_case() { # chk 的判斷式看得到這裡的 local（bash 的變數是動態範圍），但 $1、$2 是 chk 自己的，所以先存成 local
  local what="$1" want="$2" state="$3" extra err st i
  for ((i = 0; i < 120; i++)); do
    run hg packages get gotest
    err="$(pyj 'd.get("error", "")' <<<"$OUT" 2>/dev/null)" st="$(pyj 'd.get("state", "")' <<<"$OUT" 2>/dev/null)"
    [ "$st" = "$state" ] && grep -qF "$want" <<<"$err" && break
    sleep 1
  done
  extra="$(plain_extra <<<"$err")"
  chk "${what}：${state}、說「${want}」、沒有補救代號" '[ "$st" = "$state" ] && grep -qF "$want" <<<"$err" && [ -z "$(pyj '"'"'d.get("remedy", "")'"'"' <<<"$OUT")" ]'
  chk "${what}：沒有 go 的英文原文、沒夾別的英文（${extra:-沒有}）" '[ -z "$extra" ] && ! grep -qE "$GO_RAW" <<<"$err"'
}
SDK2="$APP/versions/0.0.2/sdk"
mv "$SDK2" "$B/sdk.off"
run hg packages reload --id gotest
fail_case "附的 sdk 不見了（套件跑著）" "程式資料夾裡的 sdk）找不到" running
chk "附的 sdk 不見了：說重新安裝這一版會補回來、舊的照跑" 'grep -q "重新安裝這一版" <<<"$OUT" && grep -q "仍在跑舊版" <<<"$OUT"'
mv "$B/sdk.off" "$SDK2"
run hg packages disable gotest
chk "停用套件（下一個失敗看沒在跑的）" '[ $RC = 0 ]'
printf 'module e2e/gotest\n\ngo 1.99\n' >"$PKG/go.mod"
run hg packages enable gotest
fail_case "go.mod 要的 Go 比這台的新（套件沒在跑）" "要 Go 1.99 以上" failed
chk "go.mod 要的 Go 比這台的新：說這台是 ${GOVER#go}" 'grep -qF "是 ${GOVER#go}" <<<"$OUT"'
printf '%s\n' "$GOMOD0" >"$PKG/go.mod"
run hg packages reload --id gotest
wait_pkg running
OUT="$(hg packages get gotest 2>&1)"
chk "改回來：又 running" '[ "$(pkg_field state)" = running ]'

OUT="$(cat "$TRIP" 2>/dev/null)"
chk "整條路沒叫到 git、make、python3、編譯器（絆線紀錄是空的）" '[ -z "$OUT" ]'
OUT="$(home_go)"
chk "家目錄最後還是沒有 go 資料夾、編譯快取" '[ -z "$OUT" ]'

if [ "$FAILS" = 0 ]; then
  say "下載版編主人的附程式套件：全部通過（$((SECONDS - T0)) 秒）"
  exit 0
fi
say "下載版編主人的附程式套件：${FAILS} 項沒過（$((SECONDS - T0)) 秒）"
exit 1
