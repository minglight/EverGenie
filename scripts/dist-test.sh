#!/usr/bin/env bash
# scripts/dist.sh 的驗收（DEV-155 S1）：真的交叉編兩種晶片、打包、寫版本清單，再一項一項對。`make dist-test` 叫它。結束碼 0＝全過。
# 對的東西：兩個包都在；bin/ 剛好是 Makefile 的 build 那六支；fixed/ 剛好兩支：附帶的 tmux（檔名是產品名＝install.sh 的 PRODUCT）與啟動程式 disclaim
# （ECL-65：都只連 /usr/lib/、晶片對、有臨時簽名、跟 dist/tmux 那份一模一樣、tmux 的授權文字也帶了）、
# packages/official/secretaries、跟著程式的公開卡（跟 repo 的 packages/official/personas/cards/ 一樣、沒有圖、沒有標隱藏版的）、
# sdk/（repo 的 sdk 的 go.mod 與不是測試的 .go、一字不差，只靠它與標準庫、不連網就編得出一個附程式套件，packages PKG-66）、
# ai_docs/（剛好是 ai_docs/bundled.txt 上的那幾份加清單本身、一字不差；解開這台的包，bin/evergenie docs list 列得出每一份、docs show 印的跟 repo 的一樣，platform PLT-28）、
# VERSION、最上層的 LICENSE（跟 repo 一字不差）、LICENSES/（含 Go 本身的授權、跟 repo 一字不差的看板網頁第三方授權聲明 web-third-party.txt）；清單的 sha256、大小跟檔案一樣；解開這台的包，bin/evergenie version 印的就是 VERSION、
# 經 disclaim 跑 fixed/<產品名> -V 印得出版本、沒印「照平常的方式開」；兩種晶片都對、都有簽名；--source 編進執行檔；--base-url、--channel 寫進清單；
# --no-tmux 只有六支、沒有 fixed/；--test-go-pin 編得進核心（正式編的沒有）；沒編 tmux 或少了啟動程式結束碼 3 並叫人先跑 scripts/build-tmux.sh；用法錯誤結束碼 2。
# 官方套件的測試檔與測試資料不在包裡（規則讀 dist.sh 的 TEST_PATH_RE，同一條）；文字檔與 bin/、fixed/ 的執行檔裡沒有開發機的路徑（家目錄、/Users/<名>）。
# 全程在暫存資料夾，不碰 bin/、不碰任何家、不動 dist/tmux/。
# 要先編好附帶的 tmux（scripts/build-tmux.sh，產物在 dist/tmux/；EVERGENIE_DIST_TMUX 可以指到別的地方）。
set -uo pipefail
for _legacy_env in $(compgen -e | grep '^HG_'); do unset "$_legacy_env"; done  # 讀舊名（legacy）：改名前的環境變數（屬於舊的家、舊的 session）也清掉，程式新名字沒設時會讀它
cd "$(dirname "$0")/.." || exit 1
ROOT="$PWD"
T="$(mktemp -d "${TMPDIR:-/tmp}/hg-dist-test.XXXXXX")" || exit 1
trap 'rm -rf "$T"' EXIT
# 解開符號連結、去掉多的斜線（TMPDIR 常以 / 結尾）：下面編附程式套件時 go.work 寫的路徑要跟 cd 過去的目前資料夾逐字一樣
T="$(cd "$T" && pwd -P)" || exit 1
pass=0 fail=0 OUT=""
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; printf '%s\n' "${OUT:-}" | tail -15 | sed 's/^/      /'; fi; }

VER=9.8.7
NAME="$(sed -n 's/^PRODUCT="\([^"]*\)"$/\1/p' "$ROOT/scripts/install.sh")" # 產品名（dist.sh 的 --name 預設）
SRC="https://example.invalid/hg/manifest.json"
case "$(uname -m)" in arm64) NATIVE=arm64 ;; x86_64) NATIVE=amd64 ;; *) NATIVE="" ;; esac
TMUXD="${EVERGENIE_DIST_TMUX:-$ROOT/dist/tmux}"
for f in arm64/tmux x86_64/tmux arm64/disclaim x86_64/disclaim; do
  if [ ! -x "$TMUXD/$f" ]; then
    echo "還沒編附帶的 tmux 與啟動程式（找不到 ${TMUXD}/${f}）：先跑 scripts/build-tmux.sh 再跑這支。" >&2
    exit 1
  fi
done
tmux_sum_before="$(shasum -a 256 "$TMUXD"/*/tmux "$TMUXD"/*/disclaim)"
# 測試檔不出門的規則：讀 dist.sh 那一行（同一條），先確定它抓得到該抓的、不誤抓一般的檔
TEST_PATH_RE="$(sed -n "s/^TEST_PATH_RE='\(.*\)'$/\1/p" "$ROOT/scripts/dist.sh")"
OUT="$(for f in a_test.go x/a_test.go web/a.test.ts web/a.test.tsx web/a.test.js web/a.spec.ts web/a.spec.mjs web/__tests__/a.ts __tests__/a.js \
  testdata/x.json x/testdata/y/z.txt x/testdata; do [[ "$f" =~ $TEST_PATH_RE ]] || echo "沒抓到 $f"; done
  for f in x/latest.go web/contest.ts x/attestation.md x/testdata.go x/test/a.go x/spec.md x/tests.go; do [[ "$f" =~ $TEST_PATH_RE ]] && echo "誤抓 $f"; done)"
check "dist.sh 的測試檔規則（TEST_PATH_RE）抓得到 testdata/、__tests__/、_test.go、.test.、.spec.，不誤抓一般的檔" '[ -n "$TEST_PATH_RE" ] && [ -z "$OUT" ]'
# 執行檔裡唯一允許的 /Users/：BurntSushi/toml 錯誤說明裡寫死的 Windows 路徑範例（第三方的文字，不是開發機的路徑；路徑拆開寫，這支本身給 scripts/public-check.sh 掃起來才是乾淨的）
DEVPATH_OK='instead of '"'"'\'"'"' will usually also work: "C:/Us''ers/martin".'
before="$(git status --porcelain 2>/dev/null)"

OUT="$(scripts/dist.sh --version "$VER" --out "$T/out" --tmux "$TMUXD" --notes "測試用的更新說明" --source "$SRC" --released-at 2026-11-13T00:00:00Z 2>&1)"; RC=$?
check "dist.sh 結束碼 0" '[ $RC = 0 ]'

# Makefile 的 build 目標編哪幾支（那段裡的 scripts/go-build-bin.sh … bin/<名稱> …；e2e 用的假引擎不在裡面）
MAKE_BINS="$(awk '/^build:/ { f = 1; next } f && !/^\t/ { f = 0 } f' Makefile | sed -n 's|^	scripts/go-build-bin.sh [^ ]* bin/\([^ ]*\) .*|\1|p' | LC_ALL=C sort | tr '\n' ' ')"
# 跟著程式的公開卡不寫死幾位：包裡要跟 repo 的 packages/official/personas/cards/ 一模一樣（PRS-25、PRS-26）；
# 清單跟 dist.sh 收檔一樣用 git ls-files（被忽略的 .DS_Store 這類不算）
want_cards="$(cd packages/official/personas/cards 2>/dev/null && git ls-files -co --exclude-standard . | LC_ALL=C sort | tr '\n' ' ')"
OUT="$MAKE_BINS"
check "Makefile 的 build 是六支執行檔" '[ "$(wc -w <<<"$MAKE_BINS" | tr -d " ")" = 6 ]'

# 包裡的 sdk 原始碼要跟 repo 的 sdk/ 一樣：go.mod（與 go.sum）加不是測試的 .go，子資料夾（testdata）不帶
want_sdk="$(cd sdk 2>/dev/null && git ls-files -co --exclude-standard . | grep -v / | grep -v '_test\.go$' | grep -E '^(go\.mod|go\.sum|.*\.go)$' | LC_ALL=C sort | tr '\n' ' ')"
# 跟著程式的說明文件（platform PLT-28、doc-style DOC-12）：包裡的 ai_docs/ 剛好是清單上的那幾份加清單本身（清單照 dist.sh 的讀法：# 後面是說明）
want_docs="$( { [ -f ai_docs/bundled.txt ] && echo bundled.txt; sed 's/#.*//' ai_docs/bundled.txt 2>/dev/null | tr -d ' \t\r' | grep -v '^$'; } | LC_ALL=C sort | tr '\n' ' ')"
want_listed="$(tr ' ' '\n' <<<"$want_docs" | grep -v -e '^$' -e '^bundled\.txt$' | LC_ALL=C sort | tr '\n' ' ')"
for arch in arm64 amd64; do
  f="$T/out/$NAME-$VER-darwin-$arch.tar.gz"
  OUT="$(ls -l "$T/out" 2>&1)"
  check "有 darwin-$arch 的包" '[ -s "$f" ]'
  [ -s "$f" ] || continue
  LIST="$(tar tzf "$f" 2>&1)"
  OUT="$LIST"
  got="$(sed -n "s|^$NAME-$VER/bin/\([^/][^/]*\)$|\1|p" <<<"$LIST" | LC_ALL=C sort | tr '\n' ' ')"
  check "darwin-${arch}：bin/ 剛好是 Makefile 那六支（${got}）" '[ "$got" = "$MAKE_BINS" ]'
  got="$(sed -n "s|^$NAME-$VER/fixed/\([^/][^/]*\)$|\1|p" <<<"$LIST" | LC_ALL=C sort | tr '\n' ' ')"
  want_fixed="$(printf '%s\n' "$NAME" disclaim | LC_ALL=C sort | tr '\n' ' ')"
  check "darwin-${arch}：fixed/ 剛好兩支：產品名的 tmux＋啟動程式 disclaim（${got}）" '[ "$got" = "$want_fixed" ]'
  check "darwin-${arch}：tmux 的授權文字在 LICENSES/tmux/（tmux、libevent、ncurses、utf8proc、各檔聲明）" '(for n in tmux libevent ncurses utf8proc tmux-notices; do grep -qx "$NAME-$VER/LICENSES/tmux/$n.txt" <<<"$LIST" || exit 1; done)'
  check "darwin-${arch}：有 packages/official/secretaries" 'grep -q "^$NAME-$VER/packages/official/secretaries/package.toml$" <<<"$LIST"'
  got="$(sed -n "s|^$NAME-$VER/packages/official/personas/cards/\([^/][^/]*\)$|\1|p" <<<"$LIST" | LC_ALL=C sort | tr '\n' ' ')"
  check "darwin-${arch}：跟著程式的公開卡＝repo 的 cards/（PRS-25：${got}）、角色包的圖不在包裡" '[ -n "$want_cards" ] && [ "$got" = "$want_cards" ] && [ -z "$(grep -E "^$NAME-$VER/packages/official/personas/(avatars|wardrobe|catalog)/" <<<"$LIST")" ]'
  check "darwin-${arch}：有 VERSION 與 LICENSES/（含 Go 本身的授權）" 'grep -qx "$NAME-$VER/VERSION" <<<"$LIST" && grep -qx "$NAME-$VER/LICENSES/go/LICENSE" <<<"$LIST" && grep -qx "$NAME-$VER/LICENSES/README.txt" <<<"$LIST"'
  check "darwin-${arch}：只有一個最上層資料夾 $NAME-$VER/" '[ -z "$(grep -v "^$NAME-$VER/" <<<"$LIST")" ]'
  check "darwin-${arch}：包裡只有資料夾與一般檔（沒有符號連結，下載版的安裝不收）" '[ -z "$(tar tvzf "$f" | grep -v "^[d-]")" ]'
  mkdir -p "$T/x-$arch" && tar xzf "$f" -C "$T/x-$arch"
  d="$T/x-$arch/$NAME-$VER"
  OUT="$(file -b "$d/bin/evergenie" 2>&1)"
  want=arm64
  [ "$arch" = amd64 ] && want=x86_64
  check "darwin-${arch}：執行檔是 $want" 'grep -q "$want" <<<"$OUT"'
  OUT="$(for b in "$d"/bin/* "$d"/fixed/*; do codesign -v "$b" 2>&1 || echo "沒簽名：$b"; sig="$(codesign -dv "$b" 2>&1)"; [[ "$sig" == *Signature=adhoc* ]] || echo "不是 ad-hoc 簽名：$b"; done)"
  check "darwin-${arch}：bin/ 六支與 fixed/ 兩支都有簽名、都是 ad-hoc" '[ -z "$OUT" ]'
  tarch=$arch
  [ "$arch" = amd64 ] && tarch=x86_64
  for fx in "$NAME:tmux" disclaim:disclaim; do
    fn="${fx%%:*}" src="${fx#*:}"
    OUT="$(otool -L "$d/fixed/$fn" 2>&1)"
    check "darwin-${arch}：fixed/$fn 只連 /usr/lib/ 的系統函式庫" '[ -n "$OUT" ] && [ -z "$(tail -n +2 <<<"$OUT" | awk "{print \$1}" | grep -v "^/usr/lib/")" ]'
    OUT="$(lipo -archs "$d/fixed/$fn" 2>&1)"
    check "darwin-${arch}：fixed/$fn 是 ${want}（只有這一種）" '[ "$OUT" = "$want" ]'
    # 簽名在 build-tmux.sh 就做了，打包不重簽（amd64 也不）：要跟 dist/tmux 那份一模一樣，下一版內容沒變，裝好的固定位置才不換（UPD-15）
    check "darwin-${arch}：fixed/$fn 跟 dist/tmux/$tarch/$src 一模一樣" 'cmp -s "$d/fixed/$fn" "$TMUXD/$tarch/$src"'
  done
  OUT="$(grep -lE '^[[:space:]]*(adult|nsfw|r18)[[:space:]]*[:=][[:space:]]*"?true|^[[:space:]]*level[[:space:]]*[:=][[:space:]]*"?(adult|成人)' "$d"/packages/official/personas/cards/*.md 2>&1)"
  check "darwin-${arch}：公開卡沒有標隱藏版的" '[ -z "$OUT" ]'
  check "darwin-${arch}：包裡的公開卡內容跟 repo 的 cards/ 一字不差" '[ -n "$want_cards" ] && (for c in $want_cards; do cmp -s "$d/packages/official/personas/cards/$c" "packages/official/personas/cards/$c" || exit 1; done)'
  OUT="$(sed -n "s|^$NAME-$VER/packages/official/||p" <<<"$LIST" | while IFS= read -r f; do [[ "$f" =~ $TEST_PATH_RE ]] && echo "$f"; done | head -5)"
  check "darwin-${arch}：官方套件的測試檔與測試資料（testdata/、__tests__/、_test.go、.test.、.spec.）不在包裡" '[ -n "$TEST_PATH_RE" ] && [ -z "$OUT" ]'
  OUT="$(grep -rlI -e "$HOME" -e "/Users/[A-Za-z0-9_]" "$d/packages/official" "$d/sdk" "$d/ai_docs" 2>/dev/null | head -5)"
  check "darwin-${arch}：包裡的文字檔沒有開發機的路徑（家目錄、/Users/）" '[ -z "$OUT" ]'
  # 執行檔也看（bin/ 六支、fixed/ 的 tmux 與啟動程式）：編譯時的原始碼路徑、模組快取的路徑會寫進去（Go 要 -trimpath）
  OUT="$(for f in "$d"/bin/* "$d"/fixed/*; do [ -f "$f" ] || continue; strings -a "$f" | grep -E -e "$HOME" -e "/Users/[A-Za-z0-9_]" | grep -vxF "$DEVPATH_OK" | sed "s|^|${f#"$d"/}: |" | head -3; done)"
  check "darwin-${arch}：bin/、fixed/ 的執行檔裡沒有開發機的路徑（家目錄、/Users/<名>）" '[ -n "$(ls "$d"/bin)" ] && [ -z "$OUT" ]'
  got="$(sed -n "s|^$NAME-$VER/sdk/\([^/][^/]*\)$|\1|p" <<<"$LIST" | LC_ALL=C sort | tr '\n' ' ')"
  check "darwin-${arch}：sdk/ 是 repo 的 sdk 的 go.mod 與不是測試的 .go（PKG-66：${got}）、沒有子資料夾" '[ -n "$want_sdk" ] && [ "$got" = "$want_sdk" ] && [ -z "$(grep "^$NAME-$VER/sdk/[^/]*/" <<<"$LIST")" ]'
  check "darwin-${arch}：包裡的 sdk 跟 repo 的一字不差" '[ -n "$want_sdk" ] && (for f in $want_sdk; do cmp -s "$d/sdk/$f" "sdk/$f" || exit 1; done)'
  check "darwin-${arch}：LICENSES/web-third-party.txt（看板網頁的第三方授權聲明）跟 repo 的一字不差" '[ -s LICENSES/web-third-party.txt ] && cmp -s "$d/LICENSES/web-third-party.txt" LICENSES/web-third-party.txt'
  check "darwin-${arch}：最上層有這個專案的授權 LICENSE（涵蓋程式與官方角色），跟 repo 的一字不差，沒有已經不用的 CHARACTERS-LICENSE.md" '[ -s LICENSE ] && cmp -s "$d/LICENSE" LICENSE && [ ! -e "$d/CHARACTERS-LICENSE.md" ]'
  got="$(sed -n "s|^$NAME-$VER/ai_docs/\(.*[^/]\)$|\1|p" <<<"$LIST" | LC_ALL=C sort | tr '\n' ' ')"
  check "darwin-${arch}：ai_docs/ 剛好是 ai_docs/bundled.txt 上的那幾份加清單（PLT-28：${got}）" '[ "$(wc -w <<<"$want_listed" | tr -d " ")" -ge 3 ] && [ "$got" = "$want_docs" ]'
  check "darwin-${arch}：包裡的說明文件跟 repo 的 ai_docs/ 一字不差" '[ -n "$want_listed" ] && (for f in $want_docs; do cmp -s "$d/ai_docs/$f" "ai_docs/$f" || exit 1; done)'
  OUT="$(cat "$d/VERSION")"
  check "darwin-${arch}：VERSION 是一行 $VER" '[ "$OUT" = "$VER" ] && [ "$(wc -l <"$d/VERSION" | tr -d " ")" = 1 ]'
  check "darwin-${arch}：--source 編進核心執行檔" 'LC_ALL=C grep -aq "$SRC" "$d/bin/evergenie"'
  if [ "$arch" = "$NATIVE" ]; then
    OUT="$("$d/bin/evergenie" version 2>&1)"
    check "解開這台的包（darwin-${arch}），bin/evergenie version 印 VERSION 的內容" '[ "$OUT" = "$(cat "$d/VERSION")" ]'
    # 秘書讀文件的方式（技能 create-feature）：evergenie docs list／show，不連 serve、不用家；找的是包裡的 ai_docs/
    OUT="$(env -u EVERGENIE_HOME "$d/bin/evergenie" docs list 2>&1)"
    got="$(sed -n 's/^  \([^ ][^ ]*\).*/\1/p' <<<"$OUT" | LC_ALL=C sort | tr '\n' ' ')"
    check "解開這台的包（darwin-${arch}），bin/evergenie docs list 列出清單上每一份、指到包裡的 ai_docs/" '[ -n "$want_listed" ] && [ "$got" = "$want_listed" ] && grep -qF "$d/ai_docs" <<<"$OUT"'
    OUT="$(for f in $want_listed; do env -u EVERGENIE_HOME "$d/bin/evergenie" docs show "$f" >"$T/show.txt" 2>&1 || { echo "docs show $f 失敗：$(cat "$T/show.txt")"; continue; }; cmp -s "$T/show.txt" "ai_docs/$f" || echo "docs show $f 跟 repo 的不一樣"; done)"
    check "解開這台的包（darwin-${arch}），bin/evergenie docs show 每一份印的都跟 repo 的 ai_docs/ 一樣" '[ -n "$want_listed" ] && [ -z "$OUT" ]'
    # 只靠包裡的 sdk 與標準庫、不連網（GOPROXY=off）編得出一個附程式套件：工作區 go.work 指到包裡的 sdk（跟核心編主人套件同一個做法）
    mkdir -p "$T/prog" && printf 'module e2e/prog\n\ngo 1.24\n' >"$T/prog/go.mod" &&
      printf 'package main\n\nimport (\n\t"context"\n\n\t"%s"\n)\n\ntype prog struct{}\n\nfunc (prog) Run(ctx context.Context, _ *sdk.PackageHost) error { return nil }\n\nfunc main() { _ = sdk.ServePackage(context.Background(), prog{}) }\n' \
        "$(sed -n 's/^module //p' sdk/go.mod)" >"$T/prog/main.go" &&
      printf 'go %s\n\nuse (\n\t"%s"\n\t"%s"\n)\n' "$(sed -n 's/^go //p' "$d/sdk/go.mod")" "$T/prog" "$d/sdk" >"$T/prog.work"
    OUT="$(cd "$T/prog" && env -u GOFLAGS GOWORK="$T/prog.work" GOPROXY=off GOTOOLCHAIN=local go build -o "$T/prog.bin" . 2>&1)"
    check "只靠包裡的 sdk、不連網編得出一個附程式套件（darwin-${arch}）" '[ -x "$T/prog.bin" ]'
    OUT="$(env -i PATH=/usr/bin:/bin "$d/fixed/disclaim" "$d/fixed/$NAME" -V 2>&1)"
    check "解開這台的包（darwin-${arch}），經 disclaim 跑 fixed/$NAME -V 印得出版本、沒印別的（${OUT}）" '[ "$(wc -l <<<"$OUT" | tr -d " ")" = 1 ] && grep -q "^tmux [0-9]" <<<"$OUT"'
  fi
done

OUT="$(python3 - "$T/out" "$VER" "$NAME" <<'PY' 2>&1
import hashlib, json, os, sys
out, ver, name = sys.argv[1:4]
m = json.load(open(os.path.join(out, "manifest.json"), encoding="utf-8"))
problems = []
if m.get("schema") != 1: problems.append("schema 不是 1")
if m.get("version") != ver: problems.append("version 不對：%r" % m.get("version"))
if m.get("channel") != "release": problems.append("channel 不是 release")
if m.get("notes") != "測試用的更新說明": problems.append("notes 不對")
if m.get("released_at") != "2026-11-13T00:00:00Z": problems.append("released_at 不對")
for arch in ("arm64", "amd64"):
    a = m.get("assets", {}).get("darwin-" + arch)
    fn = "%s-%s-darwin-%s.tar.gz" % (name, ver, arch)
    if not a:
        problems.append("沒有 darwin-" + arch); continue
    data = open(os.path.join(out, fn), "rb").read()
    if a.get("url") != fn: problems.append("darwin-%s 的 url 不是相對的檔名：%r" % (arch, a.get("url")))
    if a.get("sha256") != hashlib.sha256(data).hexdigest(): problems.append("darwin-%s 的 sha256 跟檔案不一樣" % arch)
    if a.get("size") != len(data): problems.append("darwin-%s 的 size 跟檔案不一樣" % arch)
print("\n".join(problems))
PY
)"
check "manifest.json：schema、版本、頻道、說明、兩個包的 url／sha256／大小都跟實際的檔案一樣" '[ -z "$OUT" ]'

# --base-url、--channel、只編一種晶片、--no-tmux、--test-go-pin（編譯吃快取，很快）
OUT="$(scripts/dist.sh --version "$VER" --out "$T/out2" --arch arm64 --channel safe --base-url "https://example.invalid/r/" --no-tmux --test-go-pin "$T/gopin.json" 2>&1)"; RC=$?
OUT2="$OUT" # dist.sh 印的（下面 --test-go-pin 那條看它有沒有說「只給測試用」）
check "只編 arm64、--channel safe、--base-url：結束碼 0、只有一個包" '[ $RC = 0 ] && [ -s "$T/out2/$NAME-$VER-darwin-arm64.tar.gz" ] && [ ! -e "$T/out2/$NAME-$VER-darwin-amd64.tar.gz" ]'
OUT="$(python3 -c 'import json, sys; m = json.load(open(sys.argv[1])); print(m["channel"], m["assets"]["darwin-arm64"]["url"], sorted(m["assets"]))' "$T/out2/manifest.json" 2>&1)"
check "清單的頻道是 safe、url 是 <base-url>/<檔名>、只有 darwin-arm64" '[ "$OUT" = "safe https://example.invalid/r/$NAME-$VER-darwin-arm64.tar.gz ['"'"'darwin-arm64'"'"']" ]'
mkdir -p "$T/x2" && tar xzf "$T/out2/$NAME-$VER-darwin-arm64.tar.gz" -C "$T/x2"
# 沒給 --source 時讀 install.sh 的 DEFAULT_SOURCE（公開 repo 的 releases 網址）編進去；只有佔位的 .invalid 網址才濾掉（編進去的是空的）
DEFSRC="$(sed -n 's/^DEFAULT_SOURCE="\([^"]*\)"$/\1/p' "$ROOT/scripts/install.sh")"
check "沒給 --source：前面 --source 給的沒編進來；編進去的是 install.sh 的 DEFAULT_SOURCE（佔位的 .invalid 網址才不編）" '[ -x "$T/x2/$NAME-$VER/bin/evergenie" ] && ! LC_ALL=C grep -aq "$SRC" "$T/x2/$NAME-$VER/bin/evergenie" && case "$DEFSRC" in *.invalid/*) ! LC_ALL=C grep -aqF "$DEFSRC" "$T/x2/$NAME-$VER/bin/evergenie" ;; *) LC_ALL=C grep -aqF "$DEFSRC" "$T/x2/$NAME-$VER/bin/evergenie" ;; esac'
# --test-go-pin 真的編進核心（-X 的名字寫錯 go 不會報錯，只會沒效果）；正式編的那份沒有，也不再認以前的環境變數
check "--test-go-pin：核心執行檔裡有那份 JSON 的路徑、說了只給測試用；正式編的沒有、也沒有 EVERGENIE_GO_DOWNLOAD_PIN" 'LC_ALL=C grep -aqF "$T/gopin.json" "$T/x2/$NAME-$VER/bin/evergenie" && grep -q "只給測試用" <<<"$OUT2" && [ -f "$T/x-arm64/$NAME-$VER/bin/evergenie" ] && ! LC_ALL=C grep -aqF "gopin.json" "$T/x-arm64/$NAME-$VER/bin/evergenie" && ! LC_ALL=C grep -aqF EVERGENIE_GO_DOWNLOAD_PIN "$T/x2/$NAME-$VER/bin/evergenie"'
got="$(cd "$T/x2/$NAME-$VER/bin" && /bin/ls | LC_ALL=C sort | tr '\n' ' ')"
check "--no-tmux：bin/ 只有 Makefile 那六支、沒有 fixed/、沒有 tmux 的授權資料夾（${got}）" '[ "$got" = "$MAKE_BINS" ] && [ ! -e "$T/x2/$NAME-$VER/fixed" ] && [ ! -e "$T/x2/$NAME-$VER/LICENSES/tmux" ]'

# 還沒編 tmux：結束碼 3、白話叫人先跑 build-tmux.sh、什麼都沒編
OUT="$(scripts/dist.sh --version "$VER" --out "$T/out3" --arch arm64 --tmux "$T/no-tmux-here" 2>&1)"; RC=$?
check "找不到附帶的 tmux：結束碼 3、說要先跑 scripts/build-tmux.sh、沒有產物" '[ $RC = 3 ] && grep -q "scripts/build-tmux.sh" <<<"$OUT" && [ ! -e "$T/out3/manifest.json" ]'
# 舊的 build-tmux.sh 產物（只有 tmux、沒有啟動程式）：一樣結束碼 3
mkdir -p "$T/old-tmux/arm64" && cp "$TMUXD/arm64/tmux" "$T/old-tmux/arm64/tmux" && cp -R "$TMUXD/LICENSES" "$T/old-tmux/LICENSES"
OUT="$(scripts/dist.sh --version "$VER" --out "$T/out4" --arch arm64 --tmux "$T/old-tmux" 2>&1)"; RC=$?
check "少了啟動程式 disclaim：結束碼 3、說要先跑 scripts/build-tmux.sh、沒有產物" '[ $RC = 3 ] && grep -q "disclaim" <<<"$OUT" && grep -q "scripts/build-tmux.sh" <<<"$OUT" && [ ! -e "$T/out4/manifest.json" ]'

for args in "--arch sparc" "--version ../x" "--channel nightly" "--name disclaim" "--source ftp://x/m.json" "--source /tmp/m.json" "--source https://" "--base-url http://example.com/dl" "--base-url ftp://x" "--test-go-pin rel/pin.json" "--bogus" "--out"; do
  # shellcheck disable=SC2086
  OUT="$(scripts/dist.sh $args --out "$T/never" 2>&1)"; RC=$?
  check "用法錯誤（${args}）：結束碼 2、什麼都沒編" '[ $RC = 2 ] && [ ! -e "$T/never/manifest.json" ]'
done

OUT="$(git status --porcelain 2>/dev/null)"
check "repo 的檔案沒有被動到（不碰 bin/）" '[ "$OUT" = "$before" ]'
OUT="$(shasum -a 256 "$TMUXD"/*/tmux "$TMUXD"/*/disclaim 2>&1)"
check "dist/tmux/ 沒被動到" '[ "$OUT" = "$tmux_sum_before" ]'

echo "通過 ${pass}，失敗 $fail"
[ "$fail" = 0 ]
