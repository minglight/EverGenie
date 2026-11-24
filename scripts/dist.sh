#!/usr/bin/env bash
# 把發行檔編好（DEV-155 S1）：每種晶片一個包，加一份版本清單。`make dist` 叫它。格式見 ai_docs/runbooks/release.md 的「發行檔」。
#
#   scripts/dist.sh [--version <版本>] [--out <資料夾>] [--arch "arm64 amd64"] [--channel release|safe]
#                   [--notes <說明> | --notes-file <檔>] [--base-url <網址>] [--source <版本清單網址>] [--name <名稱>] [--released-at <時間>]
#                   [--tmux <資料夾> | --no-tmux] [--test-go-pin <檔>]
#
# 包：<out>/<名稱>-<版本>-darwin-<晶片>.tar.gz，解開是一個資料夾 <名稱>-<版本>/：bin/（六支執行檔）、fixed/（附帶的 tmux 與啟動程式）、
#   packages/official/、sdk/（套件工具組的原始碼：go.mod 與不是測試的 .go，主人的附程式套件在本機編譯時用，packages PKG-66）、
#   ai_docs/（跟著程式的說明文件：ai_docs/bundled.txt 上的那幾份與清單本身，秘書用 evergenie docs 讀，platform PLT-28）、VERSION（一行）、
#   LICENSE（這個專案本身的授權，涵蓋程式與官方角色；＝repo 最上層那一份）、
#   LICENSES/（Go 相依與 Go 本身的授權、看板網頁的第三方授權聲明 web-third-party.txt＝repo 的 LICENSES/web-third-party.txt）。
# 附帶的 tmux（engine-claude ECL-63、ECL-65）：從 scripts/build-tmux.sh 的產物拿（--tmux，預設 dist/tmux；amd64 的包拿 x86_64 那份）：
#   <tmux>/<晶片>/tmux 放成包的 fixed/<名稱>（檔名取產品名：macOS 權限視窗上顯示的是檔名）、<tmux>/<晶片>/disclaim 放成 fixed/disclaim，
#   兩支都原封不動（不重簽：內容跟上一版一樣，安裝的固定位置就不換、主人按過的允許還算數，system 卡 UPD-15）；
#   <tmux>/LICENSES/ 放進包的 LICENSES/tmux/；兩支都只能連 /usr/lib/ 的系統函式庫。還沒編就停（結束碼 3），請先跑 scripts/build-tmux.sh。
#   引擎編進同一個名稱（-ldflags -X …claude.bundledTmuxName），才知道 fixed/ 裡要找哪個檔。
#   --no-tmux 不帶 tmux（只給測試升級流程用；這樣的包裝起來秘書開不了對話，不要拿去發）。
# --test-go-pin <檔>（只給測試用，不要拿去發）：核心執行檔下載 Go 時改讀這份 JSON（版本、下載點、檢查碼，packages PKG-65），
#   用 -ldflags -X 編進去；正式的版本不讀任何測試開關。要是絕對路徑、只有英數與 . _ - + /。
# 清單：<out>/manifest.json（schema 1；每個包的 url 預設是相對於清單的檔名，給 --base-url 就是 <base-url>/<檔名>）。
# --source 編進核心執行檔，當「家裡沒寫 [update] source 時找新版的地方」（platform.DefaultUpdateSource）；沒給就用 scripts/install.sh 的
#   DEFAULT_SOURCE（安裝與升級才會是同一個下載點），那是佔位的 .invalid 網址時就是空的（沒有下載來源）。
#   照下載來源的規則（platform 卡 PLT-25）：https://…、只收本機的 http://（127.0.0.1、localhost、[::1]）或 file:///…。包裡不會有符號連結（有就算失敗）。
# 預設：--version 是 scripts/version.sh、--out 是 dist/、--arch 兩種都編、--channel release、--name 是 scripts/install.sh 的 PRODUCT（產品名，改名只改那一行）。
# 全部 CGO_ENABLED=0 交叉編譯（不用 C 工具鏈）；amd64 補 ad-hoc 簽名（codesign -s -；arm64 的 go 自己會簽）。
# 不碰 bin/、不碰任何家、不連網（Go 的相依模組要已經在本機的快取裡）；不刪 --out 裡原本的東西（預設的 dist/ 裡有 dist/tmux/）。
# 結束碼：0 成功；1 編譯或打包失敗；2 用法錯誤；3 還沒編附帶的 tmux（或那份不對）。
set -uo pipefail
# macOS 內建的是 bash 3.2：空陣列要寫成 ${a[@]+"${a[@]}"}，變數後面緊接中文要加大括號。
cd "$(dirname "$0")/.." || exit 2
ROOT="$PWD"

usage() { sed -n '2,28p' "$0" | sed 's/^# \{0,1\}//'; }
die() { echo "dist：$*" >&2; exit 1; }
bad() { echo "dist：$*（scripts/dist.sh --help 看用法）" >&2; exit 2; }

NAME="" VERSION="" OUT="$ROOT/dist" ARCHS="arm64 amd64" CHANNEL=release NOTES="" BASE_URL="" SOURCE="" RELEASED_AT=""
TMUX_DIR="$ROOT/dist/tmux" WITH_TMUX=1 TEST_GO_PIN=""
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help) usage; exit 0 ;;
    --no-tmux) WITH_TMUX=0; shift ;;
    --version | --out | --arch | --channel | --notes | --notes-file | --base-url | --source | --name | --released-at | --tmux | --test-go-pin)
      [ $# -ge 2 ] || bad "$1 後面要接值"
      case "$1" in
        --version) VERSION="$2" ;;
        --out) OUT="$2" ;;
        --arch) ARCHS="$2" ;;
        --channel) CHANNEL="$2" ;;
        --notes) NOTES="$2" ;;
        --notes-file) [ -f "$2" ] || bad "找不到說明檔 $2"; NOTES="$(cat "$2")" ;;
        --base-url) BASE_URL="${2%/}" ;;
        --source) SOURCE="$2" ;;
        --name) NAME="$2" ;;
        --released-at) RELEASED_AT="$2" ;;
        --tmux) TMUX_DIR="$2" ;;
        --test-go-pin) TEST_GO_PIN="$2" ;;
      esac
      shift 2 ;;
    *) bad "不認得的參數：$1" ;;
  esac
done

if [ -n "$VERSION" ]; then
  # 指定了版本又有沒提交的改動：包裡的官方套件會帶這些改動，跟那個版本的 tag 不一樣（照樣編，只提醒）
  if [ -n "$(git -C "$ROOT" status --porcelain 2>/dev/null)" ]; then
    echo "（注意：工作樹有沒提交的改動，包裡的內容跟 $VERSION 的 tag 不會一樣）" >&2
  fi
else
  VERSION="$(scripts/version.sh)"
fi
[ -n "$VERSION" ] || die "算不出版本（沒有 git？請用 --version 指定）"
# 產品名只有一個來源：install.sh 的 PRODUCT（TestInstallScriptNames 檢查格式）
[ -n "$NAME" ] || NAME="$(sed -n 's/^PRODUCT="\([^"]*\)"$/\1/p' "$ROOT/scripts/install.sh")"
[ -n "$NAME" ] || die "讀不到 scripts/install.sh 的 PRODUCT（產品名）"
# 版本與名稱會變成資料夾名與網址的一部分：只收英數與 . - + _
[[ "$VERSION" =~ ^[0-9A-Za-z][0-9A-Za-z.+_-]*$ ]] || bad "版本只能有英數與 . - + _：$VERSION"
[[ "$NAME" =~ ^[0-9A-Za-z][0-9A-Za-z._-]*$ ]] || bad "名稱只能有英數與 . - _：$NAME"
[ "$NAME" != disclaim ] || bad "名稱不能是 disclaim（跟附帶的啟動程式撞名）"
case "$CHANNEL" in release | safe) ;; *) bad "--channel 只能是 release 或 safe" ;; esac
# --source 照下載來源的規則（PLT-25；跟家裡的 [update] source、install.sh 同一條）：https://<主機>…；http:// 只收本機
# （127.0.0.1、localhost、[::1]，測試用）；file:///…；不收空白與控制字元（會直接放進 -ldflags）。
# 跟程式的 platform.DownloadURLPattern、scripts/install.sh 的 DOWNLOAD_URL_RE 一字不差（TestDownloadSourceRuleSameEverywhere 比對）。
DOWNLOAD_URL_RE='^(https://[A-Za-z0-9.-]+(:[0-9]+)?(/[^[:space:][:cntrl:]]*)?|http://(127\.0\.0\.1|localhost|\[::1\])(:[0-9]+)?(/[^[:space:][:cntrl:]]*)?|file:///[^[:space:][:cntrl:]]+)$'
valid_source() {
  local LC_ALL=C
  [[ "$1" =~ $DOWNLOAD_URL_RE ]]
}
if [ -z "$SOURCE" ]; then
  # 跟安裝程式同一個下載點：讀 install.sh 那一行（格式見那裡的註解；TestInstallScriptNames 檢查只有一行、格式對）
  SOURCE="$(sed -n 's/^DEFAULT_SOURCE="\([^"]*\)"$/\1/p' "$ROOT/scripts/install.sh")"
  case "$SOURCE" in *.invalid | *.invalid/* | *.invalid:*) SOURCE="" ;; esac
fi
[ -z "$SOURCE" ] || valid_source "$SOURCE" || bad "--source 要是 https:// 開頭的網址：$SOURCE"
# --base-url 會變成清單裡每個包的網址：同一條規則（升級下載包時也照它擋）
[ -z "$BASE_URL" ] || valid_source "$BASE_URL/x" || bad "--base-url 要是 https:// 開頭的網址：$BASE_URL"
# 會直接放進 -ldflags：不收空白與其他字元
[ -z "$TEST_GO_PIN" ] || [[ "$TEST_GO_PIN" =~ ^/[A-Za-z0-9._+/-]+$ ]] || bad "--test-go-pin 要是絕對路徑、只有英數與 . _ - + /：$TEST_GO_PIN"
[ -n "$ARCHS" ] || bad "--arch 是空的"
for a in $ARCHS; do case "$a" in arm64 | amd64) ;; *) bad "--arch 只能是 arm64、amd64：$a" ;; esac; done
for tool in go python3 shasum tar; do command -v "$tool" >/dev/null 2>&1 || die "這台沒有 $tool"; done
[ -n "$RELEASED_AT" ] || RELEASED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
case "$OUT" in /*) ;; *) OUT="$ROOT/$OUT" ;; esac
case "$TMUX_DIR" in /*) ;; *) TMUX_DIR="$ROOT/$TMUX_DIR" ;; esac

# 附帶的 tmux 與啟動程式：先確定每種晶片都有、只連系統函式庫，才開始編（不然編了半天才發現少一樣）。
tmux_arch() { case "$1" in amd64) echo x86_64 ;; *) echo "$1" ;; esac; }
if [ "$WITH_TMUX" = 1 ]; then
  for a in $ARCHS; do
    for t in "$TMUX_DIR/$(tmux_arch "$a")/tmux" "$TMUX_DIR/$(tmux_arch "$a")/disclaim"; do
      if [ ! -f "$t" ] || [ ! -x "$t" ]; then
        echo "dist：還沒編附帶的 tmux 或它的啟動程式（找不到 ${t}）。先跑 scripts/build-tmux.sh（要 Xcode 指令列工具，幾分鐘），再重跑這支；只測升級流程、不要 tmux 才加 --no-tmux。" >&2
        exit 3
      fi
      if command -v otool >/dev/null 2>&1; then
        libs="$(otool -L "$t" | tail -n +2 | awk '{print $1}' | grep -v '^/usr/lib/' || true)"
        if [ -n "$libs" ]; then
          echo "dist：$t 連到了系統以外的函式庫（${libs}），一般人的 Mac 跑不起來；請用 scripts/build-tmux.sh 重編。" >&2
          exit 3
        fi
      fi
    done
  done
  [ -f "$TMUX_DIR/LICENSES/tmux.txt" ] || { echo "dist：找不到 tmux 的授權文字（$TMUX_DIR/LICENSES/tmux.txt）；請重跑 scripts/build-tmux.sh。" >&2; exit 3; }
fi
mkdir -p "$OUT" || die "建不了 $OUT"

STAGE="$(mktemp -d "${TMPDIR:-/tmp}/hg-dist.XXXXXX")" || die "建不了暫存資料夾"
trap 'rm -rf "$STAGE"' EXIT
export GOMAXPROCS="${GOMAXPROCS:-${EVERGENIE_GOPROCS:-4}}"
CPU="${EVERGENIE_CPU:-4}"

# 六支執行檔：<模組資料夾>|<執行檔名>|<套件>。跟 Makefile 的 build 同一份清單（scripts/dist-test.sh 會比對兩邊一樣）。
BINS=(
  ".|evergenie|./cmd/evergenie"
  "packages/official/engine-claude|evergenie-engine-claude|./cmd/evergenie-engine-claude"
  "packages/official/habits|evergenie-habits|./cmd/evergenie-habits"
  "packages/official/closet|evergenie-closet|./cmd/evergenie-closet"
  "packages/official/closet-mine|evergenie-closet-mine|./cmd/evergenie-closet-mine"
  "packages/official/admin|evergenie-admin|./cmd/evergenie-admin"
)
CORE_LDFLAGS="-X github.com/minglight/evergenie/internal/base/platform.BuildVersion=$VERSION"
# 引擎：附帶的 tmux 在 fixed/ 裡的檔名（ECL-65）
ENGINE_LDFLAGS="-X github.com/minglight/evergenie/packages/official/engine-claude/internal/claude.bundledTmuxName=$NAME"
[ -z "$SOURCE" ] || CORE_LDFLAGS="$CORE_LDFLAGS -X github.com/minglight/evergenie/internal/base/platform.DefaultUpdateSource=$SOURCE"
if [ -n "$TEST_GO_PIN" ]; then
  echo "（注意：--test-go-pin 只給測試用，這樣編的包下載 Go 時讀 ${TEST_GO_PIN}，不要拿去發）" >&2
  CORE_LDFLAGS="$CORE_LDFLAGS -X github.com/minglight/evergenie/internal/feature/packages/pkg.testGoPinFile=$TEST_GO_PIN"
fi

# 官方套件資料夾：git 追蹤的與還沒追蹤（沒被忽略）的檔都收（工作樹裡的改動也算），不是 git 就整份複製。
# 測試檔與測試資料不帶：使用者用不到，而且測試資料裡有開發機的路徑。哪些算（packages/official 底下的相對路徑）：
# 有 testdata、__tests__ 這一層，或名字含 _test.go、.test.、.spec.。scripts/dist-test.sh 讀這一行檢查包裡沒有（同一條規則），這行的格式不要改。
TEST_PATH_RE='(^|/)(testdata|__tests__)(/|$)|_test\.go$|\.(test|spec)\.'
is_test_path() { [[ "$1" =~ $TEST_PATH_RE ]]; }
copy_official() {
  local dest="$1/packages/official"
  mkdir -p "$dest" || return 1
  if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    (cd "$ROOT/packages/official" && git ls-files -z -co --exclude-standard . | while IFS= read -r -d '' f; do
      [ -e "$f" ] || [ -L "$f" ] || continue # 工作樹裡刪掉的
      ! is_test_path "$f" || continue        # 測試與測試資料不出門（裡面有開發機的路徑）
      mkdir -p "$dest/$(dirname "$f")" && cp -P "$f" "$dest/$f" || exit 1
    done) || return 1
  else
    cp -R "$ROOT/packages/official/." "$dest/" || return 1
    # 先列完再刪（一邊列一邊刪，find 會讀到剛刪掉的資料夾）
    local list
    list="$(cd "$dest" && find . -mindepth 1 -depth | sed 's|^\./||')" || return 1
    (cd "$dest" && while IFS= read -r f; do
      [ -n "$f" ] && is_test_path "$f" || continue
      rm -rf "$f" || exit 1
    done <<<"$list") || return 1
  fi
}

# 套件工具組 sdk 的原始碼（packages PKG-66）：主人自己的、分享來的附程式套件在使用者的電腦上編譯時 import 它（工作區 go.work 指過來，
# 不連私有 repo、不用網路）。只帶 go.mod（有 go.sum 也帶）與不是測試的 .go：git 追蹤的與還沒追蹤（沒被忽略）的，子資料夾（testdata）不帶。
# sdk 只能用標準庫：用到外部模組就停（那要一起帶、補授權文字，現在沒有這個需要）。
copy_sdk() {
  local dest="$1/sdk" f extra
  extra="$(cd "$ROOT/sdk" && GOWORK=off GOFLAGS=-mod=mod GOPROXY=off go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' . 2>&1)" ||
    { echo "dist：列不出 sdk 用到的模組：$extra" >&2; return 1; }
  extra="$(grep -v -e '^$' -e "^$(sed -n 's/^module //p' "$ROOT/sdk/go.mod")\(/\|\$\)" <<<"$extra" || true)"
  [ -z "$extra" ] || { echo "dist：sdk 用到了標準庫以外的東西（${extra}），要先決定怎麼一起帶" >&2; return 1; }
  mkdir -p "$dest" || return 1
  while IFS= read -r f; do
    case "$f" in */* | *_test.go) continue ;; go.mod | go.sum | *.go) cp "$ROOT/sdk/$f" "$dest/$f" || return 1 ;; esac
  done < <(if git -C "$ROOT" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    (cd "$ROOT/sdk" && git ls-files -co --exclude-standard . | while IFS= read -r f; do [ -f "$f" ] && echo "$f"; done)
  else
    (cd "$ROOT/sdk" && ls)
  fi)
  [ -f "$dest/go.mod" ] || { echo "dist：sdk 少了 go.mod" >&2; return 1; }
}

# 跟著程式的說明文件（platform PLT-28、doc-style DOC-12）：照 ai_docs/bundled.txt 帶那幾份（相對 ai_docs/ 的路徑不變），連清單一起；
# 秘書替主人做套件時用 evergenie docs 讀。清單上的檔不在、路徑不對、清單是空的都停（doccheck 也擋，這裡是最後一關）。
copy_docs() {
  local src="$ROOT/ai_docs" dest="$1/ai_docs" line f n=0 seen=" "
  [ -f "$src/bundled.txt" ] || { echo "dist：沒有文件清單 ai_docs/bundled.txt" >&2; return 1; }
  mkdir -p "$dest" && cp "$src/bundled.txt" "$dest/bundled.txt" || return 1
  while IFS= read -r line || [ -n "$line" ]; do
    # 跟 platform.ParseProgramDocs 一樣嚴：# 後面是說明；路徑不能有空白、反斜線、..、./、//，要是 .md，不能重複
    f="$(printf '%s' "${line%%#*}" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')"
    [ -n "$f" ] || continue
    case "$f" in /* | *..* | *\\* | *[[:space:]]* | ./* | */./* | *//* | */) echo "dist：清單上的路徑不對：$f" >&2; return 1 ;; *.md) ;; *) echo "dist：清單上的 $f 不是 .md" >&2; return 1 ;; esac
    case "$seen" in *" $f "*) echo "dist：清單上的 $f 寫了兩次" >&2; return 1 ;; esac
    seen="$seen$f "
    [ -f "$src/$f" ] || { echo "dist：清單上的 $f 不在 ai_docs/" >&2; return 1; }
    mkdir -p "$dest/$(dirname "$f")" && cp "$src/$f" "$dest/$f" || return 1
    n=$((n + 1))
  done <"$src/bundled.txt"
  [ "$n" -gt 0 ] || { echo "dist：文件清單是空的" >&2; return 1; }
}

# 授權文字：Go 本身，加上六支執行檔真的用到的外部模組（go list -deps），各自的 LICENSE／COPYING／NOTICE。
collect_licenses() {
  local dest="$1/LICENSES" arch="$2"
  local mods="$STAGE/modules-$arch.txt" b mod pkg path dir ver f n=0
  mkdir -p "$dest" || return 1
  : >"$mods"
  for b in "${BINS[@]}"; do
    IFS='|' read -r mod _ pkg <<<"$b"
    (cd "$ROOT/$mod" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go list -deps -f '{{with .Module}}{{.Path}}|{{.Dir}}|{{.Version}}{{end}}' "$pkg") >>"$mods" || return 1
  done
  {
    echo "這個資料夾是 $NAME $VERSION 用到的別人寫的程式的授權文字（各自的資料夾裡），以及 Go 本身的授權（go/）。"
    echo "web-third-party.txt：看板網頁（編進 bin/${NAME}）打包的第三方套件與各自的授權全文。"
    echo "這個專案本身的授權在上一層的 LICENSE（涵蓋程式與官方角色）。"
    echo
  } >"$dest/README.txt"
  # 這個專案本身的授權（涵蓋程式與官方角色）：放在包的最上層，跟 repo 一樣；安裝與升級整包解開，會在 <程式>/versions/<版本>/ 最上層
  for f in LICENSE; do
    [ -s "$ROOT/$f" ] || { echo "dist：找不到這個專案的授權（repo 最上層的 ${f}）" >&2; return 1; }
    cp "$ROOT/$f" "$1/$f" || return 1
  done
  # 看板網頁的第三方授權聲明：repo 的 LICENSES/web-third-party.txt 是正本（scripts/web-licenses.mjs 產生，make web-ci 守著跟相依一致）
  [ -s "$ROOT/LICENSES/web-third-party.txt" ] || { echo "dist：找不到看板網頁的第三方授權聲明（LICENSES/web-third-party.txt）；請跑 node scripts/web-licenses.mjs" >&2; return 1; }
  cp "$ROOT/LICENSES/web-third-party.txt" "$dest/web-third-party.txt" || return 1
  # Go 本身的授權：官方安裝在 GOROOT/LICENSE；Homebrew 的 Go 放在 GOROOT 的上一層。PATENTS 有就一起帶。
  local goroot lic=""
  goroot="$(go env GOROOT)"
  for f in "$goroot/LICENSE" "$goroot/../LICENSE"; do [ -f "$f" ] && { lic="$f"; break; }; done
  [ -n "$lic" ] || { echo "dist：找不到 Go 本身的授權文字（$goroot/LICENSE）" >&2; return 1; }
  mkdir -p "$dest/go" && cp "$lic" "$dest/go/LICENSE" || return 1
  [ ! -f "$goroot/PATENTS" ] || cp "$goroot/PATENTS" "$dest/go/PATENTS" || return 1
  echo "go $(go env GOVERSION)" >>"$dest/README.txt"
  while IFS='|' read -r path dir ver; do
    case "$path" in "" | github.com/minglight/evergenie | github.com/minglight/evergenie/*) continue ;; esac
    [ -d "$dir" ] || { echo "dist：找不到模組 $path 的資料夾（${dir}）" >&2; return 1; }
    mkdir -p "$dest/$path" || return 1
    for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE* "$dir"/PATENTS*; do
      [ -f "$f" ] && cp "$f" "$dest/$path/" && n=$((n + 1))
    done
    echo "$path $ver" >>"$dest/README.txt"
  done < <(LC_ALL=C sort -u "$mods")
  [ "$n" -gt 0 ] || { echo "dist：一份外部模組的授權文字都沒找到" >&2; return 1; }
}

ROWS="$STAGE/rows.txt"
: >"$ROWS"
for arch in $ARCHS; do
  top="$NAME-$VERSION"
  dir="$STAGE/$arch/$top"
  mkdir -p "$dir/bin" || die "建不了 $dir"
  echo "== darwin-${arch}：編譯"
  for b in "${BINS[@]}"; do
    IFS='|' read -r mod name pkg <<<"$b"
    flags=()
    # 認模組資料夾、不認執行檔名（改名或 --name 都不影響）：核心是根模組，引擎是 engine-claude 套件
    [ "$mod" = . ] && flags=(-ldflags "$CORE_LDFLAGS")
    [ "$mod" = packages/official/engine-claude ] && flags=(-ldflags "$ENGINE_LDFLAGS")
    (cd "$ROOT/$mod" && CGO_ENABLED=0 GOOS=darwin GOARCH="$arch" go build -p "$CPU" -trimpath ${flags[@]+"${flags[@]}"} -o "$dir/bin/$name" "$pkg") || die "編不過 ${name}（darwin-${arch}）"
  done
  if [ "$WITH_TMUX" = 1 ]; then
    # 原封不動地放（cp 不改內容；fixed/ 不在下面 amd64 重簽的範圍）：內容沒變，裝好的固定位置就不換（UPD-15）
    mkdir -p "$dir/fixed" &&
      cp "$TMUX_DIR/$(tmux_arch "$arch")/tmux" "$dir/fixed/$NAME" && chmod 755 "$dir/fixed/$NAME" &&
      cp "$TMUX_DIR/$(tmux_arch "$arch")/disclaim" "$dir/fixed/disclaim" && chmod 755 "$dir/fixed/disclaim" ||
      die "放不進附帶的 tmux 或它的啟動程式（darwin-${arch}）"
  fi
  if [ "$arch" = amd64 ]; then
    if command -v codesign >/dev/null 2>&1; then
      for f in "$dir"/bin/*; do codesign --force --sign - "$f" >/dev/null 2>&1 || die "簽不了 $(basename "$f")（darwin-amd64）"; done
    else
      echo "（這台沒有 codesign：darwin-amd64 的執行檔沒有補簽名）" >&2
    fi
  fi
  printf '%s\n' "$VERSION" >"$dir/VERSION"
  copy_official "$dir" || die "複製不了官方套件"
  copy_sdk "$dir" || die "放不進 sdk 的原始碼"
  copy_docs "$dir" || die "放不進跟著程式的說明文件"
  [ -d "$dir/packages/official/secretaries" ] || die "官方套件裡沒有 secretaries"
  collect_licenses "$dir" "$arch" || die "收集不了授權文字"
  if [ "$WITH_TMUX" = 1 ]; then
    mkdir -p "$dir/LICENSES/tmux" && cp "$TMUX_DIR"/LICENSES/*.txt "$dir/LICENSES/tmux/" || die "放不進 tmux 的授權文字"
    {
      echo
      echo "tmux/：fixed/${NAME}（附帶的 tmux，檔名取產品名；tmux 與它靜態連進去的 libevent、ncurses、utf8proc）的授權文字；tmux 各原始檔的著作權聲明在 tmux/tmux-notices.txt。"
    } >>"$dir/LICENSES/README.txt"
  fi
  # 下載版的安裝一律不收符號連結（update.extractRelease），包裡有就是做錯了
  links="$(find "$dir" -type l | head -3)"
  [ -z "$links" ] || die "包裡不能有符號連結（下載版的安裝不收）：$links"
  asset="$NAME-$VERSION-darwin-$arch.tar.gz"
  (cd "$STAGE/$arch" && COPYFILE_DISABLE=1 tar -czf "$STAGE/$asset" "$top") || die "打包失敗（darwin-${arch}）"
  mv -f "$STAGE/$asset" "$OUT/$asset" || die "放不進 $OUT"
  sha="$(shasum -a 256 "$OUT/$asset" | cut -d' ' -f1)"
  size="$(wc -c <"$OUT/$asset" | tr -d ' ')"
  url="$asset"
  [ -z "$BASE_URL" ] || url="$BASE_URL/$asset"
  printf 'darwin-%s\t%s\t%s\t%s\n' "$arch" "$url" "$sha" "$size" >>"$ROWS"
  echo "   $OUT/${asset}（$size bytes）"
done

python3 - "$ROWS" "$OUT/manifest.json" "$VERSION" "$CHANNEL" "$RELEASED_AT" "$NOTES" <<'PY' || die "寫不了版本清單"
import json, os, sys
rows, out, version, channel, released, notes = sys.argv[1:7]
assets = {}
for line in open(rows, encoding="utf-8"):
    key, url, sha, size = line.rstrip("\n").split("\t")
    assets[key] = {"url": url, "sha256": sha, "size": int(size)}
m = {"schema": 1, "version": version, "channel": channel, "released_at": released, "notes": notes, "assets": assets}
tmp = out + ".tmp"
with open(tmp, "w", encoding="utf-8") as f:
    json.dump(m, f, ensure_ascii=False, indent=2)
    f.write("\n")
os.replace(tmp, out)
PY
echo "== 版本清單：$OUT/manifest.json（${VERSION}，頻道 ${CHANNEL}）"
