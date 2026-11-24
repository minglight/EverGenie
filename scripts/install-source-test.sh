#!/usr/bin/env bash
# scripts/install.sh --from-source（從原始碼裝，開發用）的驗收（P8 第二輪 S2、REQ-279；DEV-155 S3 改名，原本叫 install-test.sh）。結束碼 0＝全過。
# 全程在暫存資料夾與假 HOME：本機 bare origin（file:// 網址）＋假的 Makefile／evergenie／go；不編譯、不碰真的服務（假的 `service install` 起一個 python 小網站當看板）。
# 隨 `make ci` 跑（TestInstallScript）；下載版（預設的裝法）的驗收是 scripts/install-test.sh（make install-test）。真的編譯、真的 serve 的整套走 make release-e2e。
set -uo pipefail
cd "$(dirname "$0")/.."
ROOT="$PWD"
BASE="$(mktemp -d "${TMPDIR:-/tmp}/hg-install-test.XXXXXX")"
PORTS=()
cleanup() {
  for f in "$BASE"/*/home*/pid "$BASE"/*/pid; do [ -f "$f" ] && kill "$(cat "$f")" 2>/dev/null; done
  rm -rf "$BASE"
}
trap cleanup EXIT
pass=0; fail=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; echo "      --- 輸出 ---"; printf '%s\n' "${OUT:-}" | sed 's/^/      /'; fi; }
PORT=19700
nextport() { PORT=$((PORT + 3)); }

# 假的 evergenie：version／init／service install|uninstall
FAKE_BIN='#!/usr/bin/env bash
case "$1" in
  version) cat "$(dirname "$0")/../ver.txt" ;;
  init)
    shift; home=""; port=8765
    while [ $# -gt 0 ]; do case "$1" in --home) home="$2"; shift ;; --port) port="$2"; shift ;; --name) shift ;; esac; shift; done
    if [ -e "$home" ] && [ -n "$(ls -A "$home")" ]; then echo "這裡已經有東西了：$home"; exit 1; fi
    [ -f "$(dirname "$0")/../init-fail" ] && { echo "假的 init 失敗" >&2; exit 1; }
    mkdir -p "$home"; printf "[web]\nport = %s\n" "$port" >"$home/home.toml" ;;
  service)
    shift; act="$1"; shift; home=""
    while [ $# -gt 0 ]; do case "$1" in --home) home="$2"; shift ;; --exe) shift ;; esac; shift; done
    case "$act" in
      install)
        [ -f "$(dirname "$0")/../service-fail" ] && { echo "假的服務裝不起來" >&2; exit 1; }
        port=$(sed -n "s/^port *= *//p" "$home/home.toml")
        nohup python3 -m http.server "$port" --bind 127.0.0.1 --directory "$home" </dev/null >/dev/null 2>&1 &
        echo $! >"$home/pid" ;;
      uninstall) [ -f "$home/pid" ] && kill "$(cat "$home/pid")" 2>/dev/null; rm -f "$home/pid" ;;
    esac ;;
  rescue)
    [ "$2" = install ] || exit 0
    [ -f "$(dirname "$0")/../rescue-fail" ] && exit 1
    mkdir -p "$HOME/Applications"; f="$HOME/Applications/evergenie 修復.command"
    printf "#!/bin/bash\n# %s\n" "$*" >"$f"; chmod +x "$f"; echo "$f" ;;
esac
'

# 每個情境一個新環境：T（origin、假指令、程式與家的位置）。第一個參數是要打的 tag（空白分隔）。
newenv() {
  T="$(mktemp -d "$BASE/env.XXXXXX")"
  mkdir -p "$T/fakebin"
  printf '#!/bin/sh\nexit 0\n' >"$T/fakebin/go"; chmod +x "$T/fakebin/go"
  git init -q --bare -b main "$T/origin.git"
  git clone -q "$T/origin.git" "$T/dev" 2>/dev/null
  (
    cd "$T/dev" || exit 1
    git config user.email t@example.com; git config user.name t
    printf '%s' "$FAKE_BIN" >fake-evergenie.sh
    printf 'build:\n\t@if [ -f build-fail ]; then echo "編不過" >&2; exit 2; fi\n\t@mkdir -p bin && cp fake-evergenie.sh bin/evergenie && chmod +x bin/evergenie\n' >Makefile
    printf 'bin/\n' >.gitignore
    echo main-dev >ver.txt
    git add -A && git commit -q -m first && git push -q origin HEAD:main
    for tag in $1; do
      echo "${tag#v}" >ver.txt; git commit -q -am "release $tag"; git tag -a "$tag" -m "$tag"; git push -q origin HEAD:main "$tag"
    done
  )
  nextport
  HOMEDIR="$T/home"
  mkdir -p "$T/fakehome"
}
inst() { # inst <其他參數…>（假 HOME：狀態資料夾的預設位置也在暫存資料夾裡）
  OUT="$(cd "$T" && HOME="$T/fakehome" EVERGENIE_SERVICE_MODE=process PATH="$T/fakebin:$PATH" EVERGENIE_INSTALL_BOARD_WAIT=10 "$ROOT/scripts/install.sh" --from-source --skip-claude-check --repo "file://$T/origin.git" --dir "$T/core" --home "$HOMEDIR" --port "$PORT" "$@" 2>&1)"
  RC=$?
}

# 1. 預設：最大的正式版（v0.10.0 比 v0.9.0 新、預發布不算）、建家、裝服務、看板 200、印網址
newenv "v0.9.0 v0.10.0 v0.11.0-rc1"
inst --name 測試
check "1a 成功：結束碼 0、印「裝好了」與看板網址" "[ $RC = 0 ] && grep -q '裝好了' <<<\"\$OUT\" && grep -q 'http://127.0.0.1:$PORT/' <<<\"\$OUT\""
check "1b 裝的是最大的正式版（0.10.0，不是預發布）" "[ \"\$(git -C '$T/core' describe --tags --exact-match)\" = v0.10.0 ] && grep -q '版本 0.10.0' <<<\"\$OUT\""
check "1c 家建好了、看板真的有回應" "[ -f '$HOMEDIR/home.toml' ] && curl -fsS --max-time 3 -o /dev/null http://127.0.0.1:$PORT/"
check "1d 放了可以點兩下的「evergenie 修復」（假 HOME 的 Applications，寫著這個家與程式），輸出白話告訴主人怎麼找" "[ -x '$T/fakehome/Applications/evergenie 修復.command' ] && grep -q -- '--home $HOMEDIR --exe $T/core/bin/evergenie' '$T/fakehome/Applications/evergenie 修復.command' && grep -q '出問題時：按 ⌘＋空白鍵搜尋「evergenie 修復」' <<<\"\$OUT\""
kill "$(cat "$HOMEDIR/pid")" 2>/dev/null

# 1e. 「修復」放不成：照樣算裝好了（結束碼 0），只多一行「注意：」
newenv "v0.9.0"
(cd "$T/dev" && touch rescue-fail && git add -A && git commit -q -m rescue-fail && git tag -f -a v0.9.0 -m v0.9.0 >/dev/null && git push -q -f origin HEAD:main v0.9.0)
inst --name 測試
check "1e 修復入口放不成不算失敗：結束碼 0、有「注意：沒能在「應用程式」資料夾放好」、沒有「出問題時」那句" "[ $RC = 0 ] && grep -q '^注意：沒能在「應用程式」資料夾放好「evergenie 修復」' <<<\"\$OUT\" && ! grep -q '出問題時：' <<<\"\$OUT\""
kill "$(cat "$HOMEDIR/pid")" 2>/dev/null

# 2. --ref 指定版本；再跑一次（程式已經在、家已有東西）：家有東西就拒絕，不動
newenv "v0.1.0 v0.2.0"
inst --ref v0.1.0 --no-service
check "2a --ref v0.1.0 --no-service：裝 0.1.0、沒有服務、說怎麼啟動" "[ $RC = 0 ] && [ \"\$(git -C '$T/core' describe --tags --exact-match)\" = v0.1.0 ] && [ ! -f '$HOMEDIR/pid' ] && grep -q 'serve --home' <<<\"\$OUT\""
inst --ref v0.2.0
check "2b 家已經有東西：拒絕、結束 13、家沒被動" "[ $RC = 13 ] && grep -q '已經有東西' <<<\"\$OUT\" && [ -f '$HOMEDIR/home.toml' ]"
HOMEDIR="$T/home2"; nextport
inst --ref v0.2.0
check "2c 同一份程式換新家：fetch 後切到 0.2.0" "[ $RC = 0 ] && [ \"\$(git -C '$T/core' describe --tags --exact-match)\" = v0.2.0 ]"
kill "$(cat "$HOMEDIR/pid")" 2>/dev/null

# 3. 沒有任何正式版：裝 main 並說明
newenv ""
inst --no-service
check "3 沒有正式版：裝 main、說「還沒有正式版」" "[ $RC = 0 ] && grep -q '還沒有正式版' <<<\"\$OUT\""

# 4. 不認得的版本
newenv "v0.1.0"
inst --ref v9.9.9 --no-service
check "4 找不到版本：結束 41、白話、不建家" "[ $RC = 41 ] && grep -q '找不到版本 v9.9.9' <<<\"\$OUT\" && [ ! -e '$HOMEDIR' ]"

# 5. 編譯失敗 / init 失敗 / 服務失敗：結束 1、不留家
newenv "v0.1.0 v0.2.0"
(cd "$T/dev" && echo x >build-fail && git add -A && git commit -q -m bad && git tag -a v0.3.0 -m x && git push -q origin HEAD:main v0.3.0)
inst --no-service
check "5a 編譯失敗：結束 41、說編譯失敗、不建家" "[ $RC = 41 ] && grep -q '編譯失敗' <<<\"\$OUT\" && [ ! -e '$HOMEDIR' ]"
newenv "v0.1.0"
(cd "$T/dev" && echo x >init-fail && git add -A && git commit -q -m bad && git tag -a v0.2.0 -m x && git push -q origin HEAD:main v0.2.0)
# init-fail 是放在 repo 根目錄、bin 的上一層：假 init 讀得到
inst --no-service
check "5b init 失敗：結束 30、不留家" "[ $RC = 30 ] && grep -q '建立你的資料夾失敗' <<<\"\$OUT\" && [ ! -e '$HOMEDIR' ]"
newenv "v0.1.0"
(cd "$T/dev" && echo x >service-fail && git add -A && git commit -q -m bad && git tag -a v0.2.0 -m x && git push -q origin HEAD:main v0.2.0)
inst
check "5c 服務裝失敗：結束 31、這次建的家刪掉" "[ $RC = 31 ] && grep -q '裝成服務失敗' <<<\"\$OUT\" && [ ! -e '$HOMEDIR' ]"

# 6. 沒有 Go：白話說要先裝、不自己裝
newenv "v0.1.0"
OUT="$(cd "$T" && HOME="$T/fakehome" EVERGENIE_SERVICE_MODE=process PATH="/usr/bin:/bin" "$ROOT/scripts/install.sh" --from-source --skip-claude-check --repo "file://$T/origin.git" --dir "$T/core" --home "$HOMEDIR" --no-service 2>&1)"; RC=$?
if command -v go >/dev/null 2>&1 && [ "$(PATH=/usr/bin:/bin command -v go)" != "" ]; then
  ok "6 （這台的 /usr/bin 就有 go，略過沒有 Go 的情境）"
else
  check "6 沒有 Go：結束 40、說要先裝 Go、不建家" "[ $RC = 40 ] && grep -q '要先裝 Go' <<<\"\$OUT\" && [ ! -e '$HOMEDIR' ] && [ ! -e '$T/core' ]"
fi

# 7. 用法
OUT="$("$ROOT/scripts/install.sh" --bogus 2>&1)"; RC=$?
check "7 不認得的選項：結束 2" "[ $RC = 2 ]"
OUT="$("$ROOT/scripts/install.sh" --repo x 2>&1)"; RC=$?
check "7b --repo 沒有 --from-source：結束 2" "[ $RC = 2 ] && grep -q 'from-source' <<<\"\$OUT\""

echo
echo "通過 ${pass}，失敗 $fail"
[ "$fail" = 0 ]
