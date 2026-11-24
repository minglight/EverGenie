#!/usr/bin/env bash
# 下載版升級的整套測試（DEV-155 S2）：用 scripts/dist.sh 編三個版本、本機 http 提供版本清單，手動裝成「下載版」（<程式>/versions/<版本>/＋current），
# 再用真的指令走：檢查新版、檢查碼不符、下載中斷、清單讀不到、沒有這台晶片的包（四種都不升級、舊版照常在跑、白話中文、英文細節只在升級日誌）、
# 升到 0.0.2、壞掉的 0.0.3 自動換回、手動退回、指定版本再升、主人確認後舊版的資料夾刪掉而且不能再退回；
# 公開角色的卡跟著換版與退回（0.0.2 的 kaguya 卡多一行，PRS-25）；主人自己的附程式套件要 Go：沒有就停下來問、同意才從本機 http 的假 Go 包
# 下載到 <程式>/tools/go（檢查碼不符不留東西）、重編用的是那一支、升級與確認之後工具還在（packages PKG-65）；編的時候工作區 go.work 指到
# 正在跑的那一版附的 sdk、升級之後跟著換成新版的（PKG-66；真的 Go 與真的編譯在 scripts/package-go-test.sh）。`make upgrade-download-test` 叫它。
#
#   scripts/upgrade-download-test.sh [--help]
#
# 結束碼 0＝全部通過；1＝有沒過的（印 ✘ 與最近一次指令的輸出）；2＝用法錯誤。
# 跑 evergenie 的 PATH 是挑過的：沒有 go、gofmt、make、gmake、git（下載版升級不能用到它們）；只有編發行檔那一步用正常的 PATH。
# 全程在 mktemp -d 的暫存資料夾、EVERGENIE_SERVICE_MODE=process（背景行程，不碰 launchd），不碰任何家、不連外網、不推任何東西。
# 環境變數：EVERGENIE_E2E_KEEP=1 保留暫存資料夾；EVERGENIE_E2E_TMP 暫存資料夾的位置（預設 /tmp，要短：socket 路徑上限約 104 字元）；EVERGENIE_E2E_WAIT 等看板的秒數（預設 180）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
# macOS 內建的是 bash 3.2：變數後面緊接中文要加大括號（${x}）。

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
  exit 0
fi
if [ $# -gt 0 ]; then
  echo "不認得的選項：${1}（--help 看用法）" >&2
  exit 2
fi
for tool in go python3 curl shasum tar; do command -v "$tool" >/dev/null 2>&1 || { echo "這台沒有 ${tool}，跑不了" >&2; exit 1; }; done

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
. "$ROOT/scripts/lib/download-e2e.sh"
B="$(dl_mktemp "${EVERGENIE_E2E_TMP:-/tmp}" hgd)" || { echo "建不出暫存資料夾（在 ${EVERGENIE_E2E_TMP:-/tmp} 底下）" >&2; exit 1; }
WAIT="${EVERGENIE_E2E_WAIT:-180}"
case "$(uname -m)" in arm64) ARCH=arm64 ;; x86_64) ARCH=amd64 ;; *) echo "不認得這台的晶片：$(uname -m)" >&2; exit 1 ;; esac

APP="$B/app" HOMEDIR="$B/home" STATE="$B/state" WWW="$B/www" SAFE="$B/safebin" GOPIN="$B/gopin.json"
# GOPIN：下載 Go 的版本、下載點與檢查碼換成本機 http 上的假包（packages PKG-65）。正式程式不讀測試開關：用 dist.sh --test-go-pin
# 編進這裡編的三個版本（換版之後也一樣）；不靠環境變數。
# CLAUDE_CONFIG_DIR 明確指到暫存區（DEV-157）：init 會把「現在登入的帳號資料夾」寫進 home.toml（ENG-19），
# 從 AI 的 session 跑時環境裡是這台電腦真的 Claude 設定，不換掉測試家的服務就會讀寫它。
CLEAN=(env -u EVERGENIE_HOME -u EVERGENIE_ENV_FILE -u EVERGENIE_AGENT -u EVERGENIE_LAUNCH -u EVERGENIE_ENGINE_SOCKET -u EVERGENIE_ENGINE_BIN_DIR -u EVERGENIE_API_SOCKET -u EVERGENIE_SESSION
  -u EVERGENIE_PERSONAS_PACK_POINTER -u EVERGENIE_OFFICIAL_PACKAGES_DIR -u EVERGENIE_PACKAGE_BIN_DIR -u EVERGENIE_RELEASE_REPO -u EVERGENIE_RELEASE_STATE -u EVERGENIE_RELEASE_STEPS
  EVERGENIE_SITE=e2e EVERGENIE_SERVICE_MODE=process EVERGENIE_BACKUP_DIR="$B/backup" GIT_TERMINAL_PROMPT=0 CLAUDE_CONFIG_DIR="$B/claude-config")
WWW_PID=""

cleanup() {
  local p
  p="$(cat "$STATE/runtime/serve.pid" 2>/dev/null)"
  [ -n "$p" ] && kill "$p" 2>/dev/null
  [ -n "$WWW_PID" ] && kill "$WWW_PID" 2>/dev/null
  pkill -f "serve -home $HOMEDIR" 2>/dev/null
  sleep 1
  pkill -9 -f "serve -home $HOMEDIR" 2>/dev/null
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
      say "      --- 最近一次指令的輸出（結束碼 ${RC}）---"
      printf '%s\n' "$OUT" | tail -25 | sed 's/^/      /'
    fi
  fi
}
run() { OUT="$("$@" 2>&1)"; RC=$?; }
freeport() { dl_freeport; }
pyj() { dl_pyj "$1"; }

# 跑 evergenie：乾淨的 EVERGENIE_* 環境、挑過的 PATH（沒有 go／make／git）、經過 current 這個連結。
hgx() { "${CLEAN[@]}" PATH="$SAFE" "$APP/current/bin/evergenie" "$@"; }
hg() { "${CLEAN[@]}" PATH="$SAFE" EVERGENIE_HOME="$HOMEDIR" "$APP/current/bin/evergenie" "$@"; }
board() { curl -s -o /dev/null -m 5 -w '%{http_code}' "http://127.0.0.1:$PORT/" 2>/dev/null; }
wait_board() {
  local i
  for ((i = 0; i < WAIT; i++)); do
    [ "$(board)" = 200 ] && return 0
    sleep 1
  done
  return 1
}
core_version() {
  hg doctor --json 2>/dev/null | python3 -I -c 'import json, sys
try:
    d = json.loads(sys.stdin.read())
    print([c["detail"] for c in d["checks"] if c["detail"].startswith("核心版本：")][0].split("：", 1)[1])
except Exception:
    print("")'
}
current() { readlink "$APP/current"; }
versions_list() { (cd "$APP/versions" && /bin/ls -A | LC_ALL=C sort | tr '\n' ' '); }
serve_pid() { cat "$STATE/runtime/serve.pid" 2>/dev/null | tr -d ' \n'; }
serve_cmd() { ps -o command= -p "$(serve_pid)" 2>/dev/null; }
english_in() { dl_english_in; }
log_lines() { if [ -f "$STATE/runtime/logs/upgrade.log" ]; then wc -l <"$STATE/runtime/logs/upgrade.log" | tr -d ' '; else echo 0; fi; }

# 把 <版本> 的包放上 http，寫版本清單（<花樣> 見 dl_publish）。
publish() { dl_publish "$WWW" "$B/dist/$1" "$ARCH" "${2:-}"; }

T0=$SECONDS
PORT="$(freeport)" WPORT="$(freeport)"
SRC="http://127.0.0.1:$WPORT/manifest.json"

say "── 準備：編三個版本（scripts/dist.sh，darwin-${ARCH}；找新版的地方編進執行檔，家裡不寫）"
# --no-tmux：這裡測的是換版與退回，不開秘書的對話；附帶的 tmux 由 dist-test 與 install-test 驗（這樣沒編 tmux 的 worktree 也跑得了）。
# 升級時放固定位置的檔（<app>/fixed/，system 卡 UPD-15：內容沒變不換、變了才換）由 Go 的 TestDownloadApplySyncsFixedFiles 驗；這裡只看沒有 fixed/ 的包不會建它。
for v in 0.0.1 0.0.2 0.0.3; do
  run "${CLEAN[@]}" "$ROOT/scripts/dist.sh" --version "$v" --arch "$ARCH" --out "$B/dist/$v" --source "$SRC" --no-tmux --test-go-pin "$GOPIN" --notes "第 $v 版的說明"
  chk "dist.sh 編 $v" '[ $RC = 0 ] && [ -s "$B/dist/$v/manifest.json" ]'
  [ $RC = 0 ] || exit 1
done
fix_manifest() { dl_fix_manifest "$B/dist/$1" "$ARCH"; }
# 0.0.2 的 kaguya 卡多一行：換版時角色庫讀的公開卡跟著 current 換（PRS-25），退回也跟著退
CARD_LINE="第 0.0.2 版的卡多的一行"
mkdir -p "$B/re2" && tar xzf "$B/dist/0.0.2/evergenie-0.0.2-darwin-$ARCH.tar.gz" -C "$B/re2" &&
  printf '%s\n' "$CARD_LINE" >>"$B/re2/evergenie-0.0.2/packages/official/personas/cards/kaguya.md" &&
  (cd "$B/re2" && COPYFILE_DISABLE=1 tar -czf "$B/dist/0.0.2/evergenie-0.0.2-darwin-$ARCH.tar.gz" evergenie-0.0.2) &&
  fix_manifest 0.0.2
RC=$?
chk "做好 0.0.2（kaguya 的卡多一行）" '[ $RC = 0 ]'
card_has_line() { run hg personas show kaguya; [ $RC = 0 ] && grep -q "輝夜" <<<"$OUT" && grep -qF "$CARD_LINE" <<<"$OUT"; }
card_no_line() { run hg personas show kaguya; [ $RC = 0 ] && grep -q "輝夜" <<<"$OUT" && ! grep -qF "$CARD_LINE" <<<"$OUT"; }
# 0.0.3 故意壞掉：bin/evergenie 換成一支小腳本，serve 一開就掛（印一段英文，確定它不會跑進通知），其他指令照常交給真的執行檔。
cat >"$B/broken-0.0.3" <<'SH'
#!/bin/sh
if [ "${1:-}" = serve ]; then
  echo "panic: e2e broken build on purpose (exit status 3)" >&2
  exit 3
fi
exec "$(dirname "$0")/evergenie.real" "$@"
SH
dl_wrap_bin "$B/dist/0.0.3" 0.0.3 "$ARCH" "$B/broken-0.0.3"
RC=$?
chk "做好壞掉的 0.0.3（serve 一開就掛）" '[ $RC = 0 ] && [ -x "$B/dist/0.0.3/re/evergenie-0.0.3/bin/evergenie.real" ]'

# 主人自己的附程式套件（packages PKG-65）：先用正常的 PATH 編一支會向核心報到的小程式；假的 Go 包裡的 go 收到 build 就把它複製過去，
# 並記下被叫的是哪一支 go（證明重編用的是程式工具資料夾裡那支）與帶的工作區 GOWORK、模組站 GOPROXY（PKG-66）。
GOVER=go1.27.4 GOLOG="$B/fakego.log"
mkdir -p "$B/gopkg" && cat >"$B/gopkg/go.mod" <<EOF &&
module e2e/gopkg

go 1.24

require github.com/minglight/evergenie/sdk v0.0.0

replace github.com/minglight/evergenie/sdk => $ROOT/sdk
EOF
  cat >"$B/gopkg/main.go" <<'EOF' &&
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/minglight/evergenie/sdk"
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
  (cd "$B/gopkg" && "${CLEAN[@]}" GOWORK=off GOFLAGS=-mod=mod go build -o "$B/gopkg-bin" .)
RC=$?
chk "編好會報到的小套件程式（給假的 go 複製）" '[ $RC = 0 ] && [ -x "$B/gopkg-bin" ]'
mkdir -p "$B/fakego/go/bin" && printf '%s\ntime 2026-11-01T00:00:00Z\n' "$GOVER" >"$B/fakego/go/VERSION" &&
  cat >"$B/fakego/go/bin/go" <<EOF &&
#!/bin/sh
if [ "\$1" = env ] && [ "\$2" = GOVERSION ]; then echo ${GOVER}; exit 0; fi
if [ "\$1" = build ] && [ "\$2" = -buildvcs=false ] && [ "\$3" = -o ]; then
  echo "\$0 \$* GOWORK=\$GOWORK GOPROXY=\$GOPROXY" >>"$GOLOG"
  cp "$B/gopkg-bin" "\$4" && chmod +x "\$4"
  exit \$?
fi
echo "fake go: \$*" >&2
exit 1
EOF
  chmod +x "$B/fakego/go/bin/go" && mkdir -p "$WWW/godl" &&
  (cd "$B/fakego" && COPYFILE_DISABLE=1 tar -czf "$WWW/godl/go-fake.darwin-$ARCH.tar.gz" go)
RC=$?
chk "做好假的 Go 包（go/VERSION、go/bin/go）" '[ $RC = 0 ] && [ -s "$WWW/godl/go-fake.darwin-$ARCH.tar.gz" ]'
# gopin [bad]：寫下載 Go 的設定（版本、下載點、這台晶片的包）；bad＝檢查碼故意寫錯
gopin() {
  python3 -I - "$WWW/godl/go-fake.darwin-$ARCH.tar.gz" "$GOPIN" "$GOVER" "http://127.0.0.1:$WPORT/godl/" "$ARCH" "${1:-}" <<'PY'
import hashlib, json, os, sys
pkg, out, ver, base, arch, mode = sys.argv[1:7]
data = open(pkg, "rb").read()
sha = "0" * 64 if mode == "bad" else hashlib.sha256(data).hexdigest()
json.dump({"version": ver, "base": base, "files": {"darwin-" + arch: {"name": os.path.basename(pkg), "sha256": sha, "size": len(data)}}}, open(out, "w"))
PY
}
# 工具資料夾裡除了 go 以外的東西（下載或解開到一半的暫存）
tools_extra() { (cd "$APP/tools" 2>/dev/null && /bin/ls -A | grep -v '^go$' | tr '\n' ' '); }
pkg_field() { hg packages get gotest 2>/dev/null | pyj "d.get('$1', '')" 2>/dev/null; }
# 文字裡除了 Go、MB 這兩個名字以外的英文字
english_but_go() { english_in | tr ' ' '\n' | awk '$0 != "" && $0 != "Go" && $0 != "MB"' | tr '\n' ' '; }

# 挑過的 PATH（dl_safebin）：系統的指令都在，但沒有 go、gofmt、make、gmake、git（也沒有 brew、tmux、python3、claude）
dl_safebin "$SAFE"
OUT="$(PATH="$SAFE" command -v go gofmt make gmake git 2>&1)"
chk "跑 evergenie 的 PATH 裡沒有 go、gofmt、make、gmake、git" '[ -z "$OUT" ]'

mkdir -p "$WWW"
publish 0.0.1
python3 -I -m http.server "$WPORT" --bind 127.0.0.1 --directory "$WWW" >"$B/www.log" 2>&1 &
WWW_PID=$!
for _ in $(seq 1 50); do curl -s -o /dev/null "$SRC" && break; sleep 0.2; done
OUT="$(curl -s "$SRC")"
chk "本機 http 提供版本清單" 'grep -q "\"version\": *\"0.0.1\"" <<<"$OUT"'

say "── 安裝：手動裝成下載版 0.0.1（$APP/versions/0.0.1＋current），建家、背景服務"
mkdir -p "$APP/versions/0.0.1" &&
  tar xzf "$B/dist/0.0.1/evergenie-0.0.1-darwin-$ARCH.tar.gz" -C "$APP/versions/0.0.1" --strip-components 1 &&
  ln -s versions/0.0.1 "$APP/current"
chk "versions/0.0.1 與 current 連結" '[ "$(current)" = versions/0.0.1 ] && [ -x "$APP/current/bin/evergenie" ]'
run hgx version
chk "current/bin/evergenie version 是 0.0.1" '[ "$OUT" = 0.0.1 ]'
run hgx init --home "$HOMEDIR" --name 測試 --port "$PORT" --timezone Asia/Taipei --state "$STATE"
chk "init 建家" '[ $RC = 0 ]'
[ $RC = 0 ] || exit 1
chk "帳號資料夾是暫存區的假資料夾（不是這台電腦真的 Claude 設定，DEV-157）" 'grep -qxF "config_dir = \"$B/claude-config\"" "$HOMEDIR/home.toml"'
chk "家裡沒寫 [update] source（用編進執行檔的）" '! grep -q "^source" "$HOMEDIR/home.toml"'
run hgx service install --home "$HOMEDIR" --exe "$APP/current/bin/evergenie"
chk "service install（背景行程）" '[ $RC = 0 ]'
chk "看板 200" 'wait_board'
OUT="$(core_version)"
chk "doctor 的核心版本是 0.0.1" '[ "$OUT" = 0.0.1 ]'
chk "0.0.1 的公開卡：kaguya 是 0.0.1 帶的那張" 'card_no_line'
OUT="$(serve_cmd)"
chk "服務跑的是 current/bin/evergenie（不是 versions/ 底下）" '[[ "$OUT" == "$APP/current/bin/evergenie serve "* ]]'
# launchd 的設定（--root 試跑：只寫 plist、不呼叫 launchctl）也指向 current，升級不用改它
run "${CLEAN[@]}" PATH="$SAFE" EVERGENIE_SERVICE_MODE= "$APP/current/bin/evergenie" service install --home "$HOMEDIR" --exe "$APP/current/bin/evergenie" --root "$B/la"
PLIST="$(ls "$B"/la/Library/LaunchAgents/*.plist 2>/dev/null | head -1)"
chk "launchd 的設定指向 current/bin/evergenie（試跑，沒呼叫 launchctl）" '[ -n "$PLIST" ] && grep -q "<string>$APP/current/bin/evergenie</string>" "$PLIST" && ! grep -q versions/ "$PLIST"'
PLIST_SUM="$(shasum -a 256 "$PLIST" 2>/dev/null | cut -d' ' -f1)"

say "── 主人自己的附程式套件要 Go（packages PKG-65）：沒有就停下來問、同意才下載到程式的工具資料夾"
gopin
run hg packages go-status
chk "go-status：沒有能用的 Go、結束碼 3、可以幫忙下載 ${GOVER}、有問主人的那一句" '[ $RC = 3 ] && [ "$(pyj '"'"'str(d["usable"]) + " " + str(d["installable"]) + " " + d.get("offer", "")'"'"' <<<"$OUT")" = "False True $GOVER" ] && grep -q "要我裝嗎" <<<"$OUT"'
eng="$(pyj 'd.get("detail", "") + d.get("ask", "")' <<<"$OUT" | english_but_go)"
chk "go-status 的 detail、ask 沒有夾英文（Go、MB 不算；${eng:-沒有}）" '[ -z "$eng" ]'
mkdir -p "$HOMEDIR/packages/mine/gotest/cmd/gotest" &&
  printf '%s\n' 'schema = 1' 'id = "gotest"' 'name = "測試套件"' 'version = "1.0.0"' 'description = "要編譯 Go 的測試套件"' 'kind = "program"' \
    'api = "evergenie.v1"' '[capabilities]' 'read = ["vault:pkg/gotest"]' 'write = ["vault:pkg/gotest"]' '[program]' 'entry = "cmd/gotest"' \
    >"$HOMEDIR/packages/mine/gotest/package.toml" &&
  cp "$B/gopkg/main.go" "$HOMEDIR/packages/mine/gotest/cmd/gotest/main.go" &&
  printf 'module e2e/gotest\n\ngo 1.24\n' >"$HOMEDIR/packages/mine/gotest/go.mod"
RC=$?
chk "放好主人自己的附程式套件 packages/mine/gotest" '[ $RC = 0 ]'
run hg packages reload
for _ in $(seq 1 40); do [ "$(pkg_field remedy)" = install_go ] && break; sleep 0.5; done
run hg packages get gotest
chk "編套件：停下來（failed）、補救代號 install_go、白話問一句" '[ "$(pyj '"'"'d.get("state", "") + " " + d.get("remedy", "")'"'"' <<<"$OUT")" = "failed install_go" ] && grep -q "這個套件要先裝編譯工具（Go，約 1MB，裝在程式自己的資料夾裡），要我裝嗎？" <<<"$OUT"'
eng="$(pyj 'd.get("error", "")' <<<"$OUT" | english_but_go)"
chk "套件的白話原因沒有夾英文（${eng:-沒有}）" '[ -z "$eng" ]'
chk "沒問之前什麼都沒下載（沒有 tools/go、http 沒有人抓 Go 包）" '[ ! -e "$APP/tools/go" ] && ! grep -q "godl/" "$B/www.log"'
run hg packages install-go
chk "install-go 沒有 --confirmed：結束碼 2、說要先問主人、不下載" '[ $RC = 2 ] && grep -q "先問主人" <<<"$OUT" && [ ! -e "$APP/tools/go" ] && ! grep -q "godl/" "$B/www.log"'
gopin bad
run hg packages install-go --confirmed
chk "檢查碼不符：結束碼 1、說檢查碼對不上、沒有裝" '[ $RC = 1 ] && grep -q "檢查碼對不上" <<<"$OUT" && grep -q "沒有裝" <<<"$OUT"'
eng="$(english_but_go <<<"$OUT")"
chk "檢查碼不符的訊息沒有夾英文（${eng:-沒有}）" '[ -z "$eng" ]'
OUT2="$(tools_extra)"
chk "檢查碼不符：工具資料夾裡什麼都沒留（${OUT2:-空的}）" '[ ! -e "$APP/tools/go" ] && [ -z "$OUT2" ]'
chk "套件還在等 Go" '[ "$(pkg_field remedy)" = install_go ]'
gopin
run hg packages install-go --confirmed
chk "主人同意後 install-go：成功、下載了 $GOVER" '[ $RC = 0 ] && [ "$(pyj '"'"'d.get("version", "")'"'"' <<<"$OUT")" = "$GOVER" ] && grep -q "下載 Go" <<<"$OUT"'
chk "Go 放在程式的工具資料夾（tools/go）、沒留暫存" '[ -x "$APP/tools/go/bin/go" ] && [ -z "$(tools_extra)" ]'
run hg packages go-status
chk "go-status：能用（結束碼 0）、用的是工具資料夾那支" '[ $RC = 0 ] && [ "$(pyj '"'"'d.get("source", "") + " " + d.get("path", "")'"'"' <<<"$OUT")" = "tools $APP/tools/go/bin/go" ]'
for _ in $(seq 1 60); do [ "$(pkg_field state)" = running ] && break; sleep 0.5; done
chk "套件自己重編、跑起來（running、沒有補救代號）" '[ "$(pkg_field state)" = running ] && [ -z "$(pkg_field remedy)" ]'
chk "重編用的是工具資料夾那支 go" 'grep -q "^$APP/tools/go/bin/go build -buildvcs=false -o " "$GOLOG" 2>/dev/null'
GOWORKF="$STATE/runtime/packages/gotest/go.work"
# sdk_of <版本>：那一版附的 sdk 的真實路徑（/tmp 是連結，核心記的是解開後的）
sdk_of() { (cd "$APP/versions/$1/sdk" 2>/dev/null && pwd -P); }
chk "0.0.1 的包附了 sdk 原始碼（versions/0.0.1/sdk/go.mod）" '[ -f "$APP/versions/0.0.1/sdk/go.mod" ] && [ -n "$(sdk_of 0.0.1)" ]'
chk "編的時候帶工作區（GOWORK 指到 runtime/packages/gotest/go.work）、外部模組只走公開的模組站" 'tail -1 "$GOLOG" | grep -qF " GOWORK=$GOWORKF GOPROXY=https://proxy.golang.org"'
chk "工作區用 0.0.1 附的 sdk 與套件資料夾、套件的 go.mod 沒被改" 'grep -qF "\"$(sdk_of 0.0.1)\"" "$GOWORKF" && grep -qF "\"$HOMEDIR/packages/mine/gotest\"" "$GOWORKF" && [ "$(cat "$HOMEDIR/packages/mine/gotest/go.mod")" = "$(printf "module e2e/gotest\n\ngo 1.24")" ] && [ ! -e "$HOMEDIR/packages/mine/gotest/go.work" ]'
GOINODE="$(stat -f %i "$APP/tools/go/bin/go" 2>/dev/null)"

say "── 檢查新版"
publish 0.0.2
run hg upgrade --check --json
chk "upgrade --check：目標 0.0.2、現在 0.0.1" '[ $RC = 0 ] && [ "$(pyj '"'"'d["target"] + " " + d["current"]'"'"' <<<"$OUT")" = "0.0.2 0.0.1" ]'

say "── 下載出問題：不升級、舊版照常在跑、白話中文、英文細節在升級日誌"
PID0="$(serve_pid)"
bad_case() { # bad_case <說明> <花樣> <訊息要有的字>（chk 的判斷式看得到這裡的 local：bash 的變數是動態範圍）
  local what="$1" want="$3" before eng
  before="$(log_lines)"
  publish 0.0.2 "$2"
  run hg upgrade --apply
  chk "${what}：結束碼 1、說「${want}」與「舊版照常在跑」" '[ $RC = 1 ] && grep -q "$want" <<<"$OUT" && grep -q "舊版照常在跑" <<<"$OUT"'
  eng="$(english_in <<<"$OUT")"
  chk "${what}：訊息沒有夾英文（${eng:-沒有}）" '[ -z "$eng" ]'
  chk "${what}：英文細節寫進升級日誌" '[ "$(log_lines)" -gt "$before" ] && tail -n +$((before + 1)) "$STATE/runtime/logs/upgrade.log" | grep -q "\[detail\]"'
  chk "${what}：還是 0.0.1（current、服務、同一個行程）、versions 只有 0.0.1、沒留下載到一半的檔" '[ "$(current)" = versions/0.0.1 ] && [ "$(core_version)" = 0.0.1 ] && [ "$(serve_pid)" = "$PID0" ] && [ "$(versions_list)" = "0.0.1 " ]'
  chk "${what}：沒有把 0.0.2 記成跳過" '! grep -q 0.0.2 "$STATE/runtime/upgrade/skip.json" 2>/dev/null'
}
bad_case "檢查碼不符" badsha "下載的檔案不完整"
bad_case "下載中斷" cut "下載到一半斷掉了"
bad_case "清單讀不到" garbage "讀不到版本清單"
bad_case "沒有這台晶片的包" nochip "沒有這台電腦的晶片能用的版本"
chk "這幾次都沒有寫升級紀錄" '[ ! -s "$STATE/runtime/upgrade/history.jsonl" ]'

say "── 升到 0.0.2"
publish 0.0.2
run hg upgrade --apply
chk "upgrade --apply 成功（結束碼 0）" '[ $RC = 0 ]'
OUT2="$OUT"
OUT="$(core_version)"
chk "doctor 的核心版本是 0.0.2" '[ "$OUT" = 0.0.2 ]'
chk "current 指向 versions/0.0.2、舊的 versions/0.0.1 還在" '[ "$(current)" = versions/0.0.2 ] && [ "$(versions_list)" = "0.0.1 0.0.2 " ]'
OUT="$(serve_cmd)"
chk "服務重開了、跑的還是 current/bin/evergenie" '[ "$(serve_pid)" != "$PID0" ] && [[ "$OUT" == "$APP/current/bin/evergenie serve "* ]]'
chk "看板 200" 'wait_board'
OUT="$(tail -1 "$STATE/runtime/upgrade/history.jsonl" 2>&1)"
chk "升級紀錄：0.0.1 → 0.0.2、成功、下載版" '[ "$(pyj '"'"'d["kind"] + " " + d["from"] + " " + d["to"] + " " + str(d["ok"]) + " " + d.get("install", "")'"'"' <<<"$OUT")" = "upgrade 0.0.1 0.0.2 True download" ]'
chk "升級前做了備份" 'ls "$STATE"/runtime/backups/upgrade-0.0.1-0.0.2.tgz >/dev/null 2>&1'
chk "包裡沒有 fixed/：程式資料夾也不會多出 fixed/（UPD-15）" '[ ! -e "$APP/fixed" ]'
chk "公開卡跟著換成 0.0.2 帶的（多那一行）、家裡沒有官方卡" 'card_has_line && [ -z "$(ls "$HOMEDIR/personas" 2>/dev/null | grep -v "^mine$")" ]'
OUT="$OUT2"
chk "升級的輸出沒有提到 git、go、make" '! grep -qiE "\bgit\b|go build|\bmake\b" <<<"$OUT"'
chk "launchd 的設定沒被動到" '[ "$(shasum -a 256 "$PLIST" | cut -d" " -f1)" = "$PLIST_SUM" ]'
run hg packages go-status
chk "升級之後工具資料夾的 Go 還在（同一個檔）、go-status 用它" '[ $RC = 0 ] && [ -n "$GOINODE" ] && [ "$(stat -f %i "$APP/tools/go/bin/go" 2>/dev/null)" = "$GOINODE" ] && [ "$(pyj '"'"'d.get("source", "")'"'"' <<<"$OUT")" = tools ]'
for _ in $(seq 1 60); do [ "$(pkg_field state)" = running ] && break; sleep 0.5; done
chk "升級之後主人的套件照常跑（running）" '[ "$(pkg_field state)" = running ]'
chk "升級之後用 0.0.2 附的 sdk 重編（工作區跟著換版）" 'grep -qF "\"$(sdk_of 0.0.2)\"" "$GOWORKF" && ! grep -qF "/versions/0.0.1/" "$GOWORKF" && [ "$(grep -c " build -buildvcs=false -o " "$GOLOG")" -ge 2 ]'

say "── 壞掉的 0.0.3：自動換回 0.0.2"
publish 0.0.3
MSGS_BEFORE="$(find "$HOMEDIR/vault/messages" -type f 2>/dev/null | LC_ALL=C sort)"
run hg upgrade --apply
chk "upgrade --apply（0.0.3）失敗、結束碼 1、說已換回 0.0.2" '[ $RC = 1 ] && grep -q "已換回 0.0.2" <<<"$OUT"'
chk "current 換回 versions/0.0.2、服務是 0.0.2" '[ "$(current)" = versions/0.0.2 ] && [ "$(core_version)" = 0.0.2 ]'
chk "看板 200" 'wait_board'
chk "壞掉的 versions/0.0.3 留著給人查" '[ -x "$APP/versions/0.0.3/bin/evergenie.real" ]'
chk "0.0.3 記成跳過、狀態是 rolled_back" 'grep -q 0.0.3 "$STATE/runtime/upgrade/skip.json" && [ "$(pyj '"'"'d["phase"]'"'"' <"$STATE/runtime/upgrade/state.json")" = rolled_back ]'
NEWMSG=""
for _ in $(seq 1 20); do
  NEWMSG="$(comm -13 <(printf '%s\n' "$MSGS_BEFORE") <(find "$HOMEDIR/vault/messages" -type f 2>/dev/null | LC_ALL=C sort) | while read -r f; do [ -n "$f" ] && grep -l "沒成功" "$f"; done | head -1)"
  [ -n "$NEWMSG" ] && break
  sleep 0.5
done
OUT="$(cat "$NEWMSG" 2>/dev/null)"
chk "主人收到「沒成功，已換回 0.0.2」的通知" '[ -n "$NEWMSG" ] && grep -q "已換回 0.0.2" <<<"$OUT"'
# 通知給主人看的是標題與內容（訊息檔的 title、context；其他欄位與內文的「[system] 事情做完」是訊息本身的格式）
BODY="$(python3 -I -c 'import re, sys
for line in open(sys.argv[1], encoding="utf-8"):
    m = re.match(r"^(title|context):\s*(.*)$", line.rstrip("\n"))
    if m:
        print(m.group(2))' "$NEWMSG" 2>/dev/null)"
eng="$(english_in <<<"$BODY")"
chk "通知的標題與內容沒有英文（檔名不算；${eng:-沒有}）、沒有壞版印的英文輸出" '[ -n "$BODY" ] && [ -z "$eng" ] && ! grep -qE "panic|exit status|goroutine|broken build" "$NEWMSG"'
chk "壞版印的英文只在服務日誌" 'grep -q "panic: e2e broken build" "$STATE/runtime/logs/serve.log"'

say "── 手動退回：0.0.2 → 0.0.1"
run hg upgrade --rollback
chk "upgrade --rollback 成功（結束碼 0）" '[ $RC = 0 ]'
chk "current 指向 versions/0.0.1、服務是 0.0.1" '[ "$(current)" = versions/0.0.1 ] && [ "$(core_version)" = 0.0.1 ]'
chk "公開卡跟著退回 0.0.1 帶的（沒有那一行）" 'card_no_line'
chk "看板 200" 'wait_board'
OUT="$(tail -1 "$STATE/runtime/upgrade/history.jsonl" 2>&1)"
chk "升級紀錄最後一筆是退回 0.0.1" '[ "$(pyj '"'"'d["kind"] + " " + d["to"]'"'"' <<<"$OUT")" = "rollback 0.0.1" ]'

say "── 指定版本再升（--to 0.0.2），主人確認後舊版的資料夾刪掉、不能再退回"
publish 0.0.2
run hg upgrade --apply --to 0.0.9
chk "--to 跟清單上的版本不一樣：拒絕、沒動" '[ $RC != 0 ] && [ "$(current)" = versions/0.0.1 ]'
run hg upgrade --apply --to 0.0.2
chk "upgrade --apply --to 0.0.2 成功" '[ $RC = 0 ] && [ "$(current)" = versions/0.0.2 ] && [ "$(core_version)" = 0.0.2 ]'
chk "看板 200" 'wait_board'
chk "versions 只留新舊兩版（壞掉的 0.0.3 收掉了）" '[ "$(versions_list)" = "0.0.1 0.0.2 " ]'
run hg upgrade --accept
chk "upgrade --accept 成功，說舊版的程式收掉了、備份留著" '[ $RC = 0 ] && grep -q "舊版的程式收掉了" <<<"$OUT" && grep -q "只留最新一份" <<<"$OUT"'
chk "versions 只剩 0.0.2" '[ "$(versions_list)" = "0.0.2 " ]'
chk "確認之後工具資料夾的 Go 還在（同一個檔）" '[ -n "$GOINODE" ] && [ "$(stat -f %i "$APP/tools/go/bin/go" 2>/dev/null)" = "$GOINODE" ]'
run hg upgrade --rollback
chk "確認之後 --rollback 拒絕（舊版的資料夾已經不在）、沒動" '[ $RC != 0 ] && grep -q "程式資料夾已經不在" <<<"$OUT" && [ "$(current)" = versions/0.0.2 ] && [ "$(core_version)" = 0.0.2 ]'

say ""
if [ "$FAILS" = 0 ]; then
  say "下載版升級整套：全部通過（$((SECONDS - T0)) 秒）"
  exit 0
fi
say "下載版升級整套：${FAILS} 項沒過（$((SECONDS - T0)) 秒）"
exit 1
