#!/usr/bin/env bash
# scripts/public-check.sh 與 scripts/export-public.sh 的驗收（DEV-158）。結束碼 0＝全過。
# 全程在暫存資料夾：自己建檔案樹與一個暫存的 git repo；HOME 換成暫存的、私人清單用 EVERGENIE_BLOCKLIST 指到暫存區，
# 不讀也不寫真的 ~/.evergenie-release/。假的信箱、網址、token、金鑰、內網位址都在執行時才拼起來，這支本身掃起來是乾淨的。
# 被測的兩支用跑這支的 bash 跑（/bin/bash scripts/public-check-test.sh 就是全程 bash 3.2）。
set -uo pipefail
cd "$(dirname "$0")/.." || exit 1
ROOT="$PWD"
SH="${BASH:-bash}"
CHECK="$ROOT/scripts/public-check.sh"
T="$(mktemp -d "${TMPDIR:-/tmp}/evergenie-public-check-test.XXXXXX")" || exit 1
trap 'rm -rf "$T"' EXIT
T="$(cd "$T" && pwd -P)" || exit 1
export HOME="$T/home"
mkdir -p "$HOME"
unset EVERGENIE_BLOCKLIST XDG_CONFIG_HOME
export GIT_CONFIG_NOSYSTEM=1
pass=0 fail=0 OUT="" RC=0 N=0
ok() { echo "PASS  $1"; pass=$((pass + 1)); }
bad() { echo "FAIL  $1"; fail=$((fail + 1)); }
check() { if eval "$2"; then ok "$1"; else bad "$1"; printf '%s\n' "$OUT" | tail -15 | sed 's/^/      /'; fi; }
# run_check <私人清單（空字串＝不設 EVERGENIE_BLOCKLIST）> <參數…>：結果放 OUT（stdout＋stderr）與 RC
run_check() {
  local bl="$1"
  shift
  OUT="$(EVERGENIE_BLOCKLIST="$bl" "$SH" "$CHECK" "$@" 2>&1)"
  RC=$?
}
gitq() { git -c user.name=test -c user.email=test@example.com -c init.defaultBranch=main "$@" >/dev/null 2>&1; }

# 假資料：拼起來的（原始碼裡不出現完整的字）
AT='@'
EMAIL="alex.chen${AT}corpmail.co"
TSNET="genie-box.tail9f8e7d.ts"".net"
USERS="/Us""ers/alexchen"
TOKEN="gh""p_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8"
KEY="-----BEGIN ""OPENSSH PRIVATE KEY-----"
FIELD_VALUE="k3Yq9Lm2Pz8Rt4Vw6Xy1Ab5Cd7Ef0Gh2Jk"
FIELD="api""_key = \"$FIELD_VALUE\""
IP="192.168"".7.21"
TS6="fd7a"":115c:a1e0::7f01"
TS4="100.101"".7.3"
TEN="10.0"".3.4"
B172="172.20"".1.9"
SK="sk""-proj-Ab3dEf6hIj9kLm2nOp5qRs8t"
XOX="xo""xb-1234567890-abcdefghij"
AKIA="AK""IAZ3Q7W2E5R8T1Y4U6"
JWT="ey""JhbGciOiJIUzI1NiJ9.ey""JzdWIiOiIxMjM0NTY3ODkwIn0.dBjftJeZ4CVPmB92K27uhbUJU1p1r"
TSEX="example.tail9f8e7d.ts"".net"
NOREPLY="noreply${AT}corpmail.co"

BL="$T/blocklist.txt"
cat >"$BL" <<'EOF'
# 測試用的私人清單（全是假的字）
Zorblax Quindle  # 測試人名
re:moon-?castle-[0-9]+  # 測試地名
quuxbarian
EOF

# mktree <資料夾>：一份乾淨的樹：各種示範寫法（都該放過）＋一張沒有中繼資料的圖
mktree() {
  local d="$1"
  mkdir -p "$d/docs" "$d/ai_docs" "$d/img"
  cat >"$d/README.md" <<'EOF'
# 範例專案
信寄 someone@example.com，機器人是 noreply@github.com，假的 dev@e2e.test
看板 https://board.example.ts.net
路徑 /Users/you/Workspace/x 與 /Users/example/y
本機 127.0.0.1:8080、文件範例 192.0.2.10、版本 v10.4.0 與 1.10.2.3
圖 icon@2x.png、outfit@sea.png
token := os.Getenv("TOKEN")
GH_TOKEN=ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
EOF
  printf '\211PNG\r\n\032\n\000\000\000\rIHDR\000\000\000\001\000\000\000\001\010\006\000\000\000' >"$d/img/logo.png"
  printf '# 放行清單（測試）\n' >"$d/ai_docs/public-allow.txt"
}
# newtree：新的乾淨樹，路徑放 D
newtree() { N=$((N + 1)); D="$T/tree-$N"; mktree "$D"; }

# ---- 乾淨的樹 ----------------------------------------------------------------------------------
newtree
run_check "$BL" "$D"
check "乾淨的樹（示範寫法 example.com、noreply@github.com、board.example.ts.net、/Users/you、127.0.0.1、版本號、@2x.png、填空字的 token 都放過）：結束碼 0、印「沒有命中」" '[ $RC = 0 ] && grep -q "沒有命中" <<<"$OUT"'
size="$(wc -c <"$D/img/logo.png" | tr -d ' ')"
check "乾淨的樹：二進位檔列出路徑與大小（img/logo.png  $size bytes）" 'grep -q "^二進位檔 1 個" <<<"$OUT" && grep -qx "  img/logo.png  $size bytes" <<<"$OUT"'
check "乾淨的樹：有私人清單就不印「沒有私人清單」、算得出 3 條" '! grep -q "沒有私人清單" <<<"$OUT" && grep -q "私人清單 3 條" <<<"$OUT"'

# ---- 每一種命中：第 3 行放一個，要失敗、檔案與行號對、輸出裡沒有完整的字 ------------------------------------
case_hit() { # <說明> <輸出裡的規則> <第 3 行的內容> <輸出裡不能出現的字>
  local what="$1" rule="$2" text="$3" secret="$4"
  newtree
  printf '%s\n' "第一行" "第二行" "$text" "第四行" >"$D/docs/case.md"
  run_check "$BL" "$D"
  check "${what}：結束碼 1、列出 docs/case.md:3: $rule" '[ $RC = 1 ] && grep -qF "docs/case.md:3: $rule" <<<"$OUT"'
  check "${what}：輸出裡沒有完整的命中字" '! grep -qiF -- "$secret" <<<"$OUT"'
}
case_hit "信箱" email "聯絡 $EMAIL 謝謝" "$EMAIL"
case_hit "ts.net 網址" ts-net "看板 https://$TSNET/" "$TSNET"
case_hit "/Users/真名/" users-path "路徑 $USERS/Projects/x" "alexchen"
case_hit "假 token" secret-token "export GH=$TOKEN" "$TOKEN"
case_hit "私鑰開頭" private-key "$KEY" "$KEY"
case_hit "長的密鑰欄位" secret-field "$FIELD" "$FIELD_VALUE"
case_hit "內網位址" private-ip "內網 $IP:8080" "$IP"
case_hit "Tailscale 的 IPv6 位址" private-ip "位址 [$TS6]:80" "$TS6"
case_hit "Tailscale 的 100.64～127 段" private-ip "ssh $TS4" "$TS4"
case_hit "10.x 位址" private-ip "內網 $TEN" "$TEN"
case_hit "172.16～31 段" private-ip "內網 $B172" "$B172"
case_hit "sk- 開頭的 token" secret-token "KEY=$SK" "$SK"
case_hit "xox 開頭的 token" secret-token "SLACK=$XOX" "$XOX"
case_hit "AKIA 開頭的金鑰" secret-token "AWS=$AKIA" "$AKIA"
case_hit "JWT" secret-token "Bearer $JWT" "$JWT"
case_hit "example.<真的 tailnet>.ts.net 照樣擋" ts-net "看板 https://$TSEX/" "tail9f8e7d"
case_hit "noreply 但網域不是知名服務" email "寄件 $NOREPLY" "corpmail"
case_hit "私人清單的固定字串（大小寫不同）" "測試人名" "我是 zORBLAX qUINDLE 啦" "zorblax"
case_hit "私人清單的 re: 規則" "測試地名" "住在 Moon-Castle-42 號" "moon-castle"

newtree
mkdir -p "$D/notes/Quuxbarian-dir" && echo "內容是乾淨的" >"$D/notes/Quuxbarian-dir/QUUXBARIAN-plan.md"
run_check "$BL" "$D"
check "檔名命中（沒名字的規則叫「私人清單第 4 行」）：結束碼 1、行號 0、路徑裡命中的部分遮成 [規則名]" \
  '[ $RC = 1 ] && grep -qF "notes/[私人清單第 4 行]-dir/[私人清單第 4 行]-plan.md:0: 私人清單第 4 行（檔名）" <<<"$OUT"'
check "檔名命中：輸出裡沒有完整的私人字" '! grep -qi "quuxbarian" <<<"$OUT"'

newtree
mkdir -p "$D/photos"
# 最小的 JPEG：APP1 段放 Exif，IFD0 只有一個 GPS 指標（標籤 0x8825）
printf '\377\330\377\341\000\036Exif\000\000MM\000*\000\000\000\010\000\001\210\045\000\004\000\000\000\001\000\000\000\032\000\000\000\000\377\331' >"$D/photos/trip.jpg"
run_check "$BL" "$D"
check "帶 EXIF（含 GPS）的圖：結束碼 1、列出 photos/trip.jpg:0: image-metadata（EXIF（含 GPS））" '[ $RC = 1 ] && grep -qF "photos/trip.jpg:0: image-metadata（EXIF（含 GPS））" <<<"$OUT"'
check "帶 EXIF 的圖也列在二進位檔清單" 'grep -qx "  photos/trip.jpg  40 bytes" <<<"$OUT"'

newtree
mkdir -p "$D/photos"
printf '\377\330\377\341\000\010Exif\000\000\377\331' >"$D/photos/plain.jpg"
printf '\377\330\377\341\000\036Exif\000\000MM\000*\000\000\000\010\000\001\210\045\000\004\000\000\000\001\000\000\000\032\000\000\000\000\377\331' >"$D/photos/IMG.JPG"
printf '\377\330\377\376\000\007hello\377\331' >"$D/photos/note.jpeg"
printf '\211PNG\r\n\032\n\000\000\000\010eXIfMM\000*\000\000\000\010' >"$D/photos/exif.png"
printf 'RIFF\000\000\000\000WEBP<x:xmpmeta>' >"$D/photos/xmp.webp"
printf 'MM\000*\000\000\000\010' >"$D/photos/scan.tiff"
run_check "$BL" "$D"
check "有 EXIF 但沒有 GPS 的 JPEG：image-metadata（EXIF）" 'grep -qF "photos/plain.jpg:0: image-metadata（EXIF）" <<<"$OUT"'
check "副檔名大寫的 .JPG 也看" 'grep -qF "photos/IMG.JPG:0: image-metadata（EXIF（含 GPS））" <<<"$OUT"'
check "JPEG 的註解段" 'grep -qF "photos/note.jpeg:0: image-metadata（JPEG 註解）" <<<"$OUT"'
check "PNG 的 eXIf 段" 'grep -qF "photos/exif.png:0: image-metadata（EXIF）" <<<"$OUT"'
check "XMP" 'grep -qF "photos/xmp.webp:0: image-metadata（XMP）" <<<"$OUT"'
check "TIFF 一律命中" 'grep -qF "photos/scan.tiff:0: image-metadata（TIFF" <<<"$OUT"'
check "img/logo.png 沒有中繼資料：不命中" '[ $RC = 1 ] && ! grep -q "^img/logo.png:" <<<"$OUT"'

newtree
printf '\211PNG\r\n\032\n\000\000\000\030tEXtComment\000Zorblax Quindle' >"$D/img/Zorblax Quindle.png"
printf 'log\000 quuxbarian here\n' >"$D/docs/app.log"
run_check "$BL" "$D"
check "PNG 的文字段：image-metadata（PNG 文字段）" 'grep -qF "img/[測試人名].png:0: image-metadata（PNG 文字段）" <<<"$OUT"'
check "二進位檔的內容也找私人字：圖的文字段、帶 NUL 的紀錄檔" \
  'grep -qF "img/[測試人名].png:0: 測試人名（二進位檔內容）" <<<"$OUT" && grep -qF "docs/app.log:0: 私人清單第 4 行（二進位檔內容）" <<<"$OUT"'
check "二進位檔的檔名命中：命中與二進位檔清單裡都遮掉" \
  'grep -qF "img/[測試人名].png:0: 測試人名（檔名）" <<<"$OUT" && grep -q "^  img/\[測試人名\].png  [0-9]* bytes$" <<<"$OUT" && ! grep -qi "zorblax\|quuxbarian" <<<"$OUT"'

newtree
ln -s "/opt/moon-castle-7/x" "$D/docs/quuxbarian-link"
run_check "$BL" "$D"
check "符號連結的名字與指向的路徑都照私人清單掃、都遮掉" \
  'grep -qF "docs/[私人清單第 4 行]-link:0: 私人清單第 4 行（檔名）" <<<"$OUT" && grep -qF "docs/[私人清單第 4 行]-link:1: 測試地名" <<<"$OUT" && ! grep -qi "quuxbarian\|moon-castle" <<<"$OUT"'

newtree
printf 'zorblax quindle\n' >"$D/-"
run_check "$BL" "$D"
check "檔名就叫 - 的檔照樣掃內容" '[ $RC = 1 ] && grep -qF -- "-:1: 測試人名" <<<"$OUT"'

newtree
echo "內容" >"$D/docs/quuxbarian-notes.md" && chmod 000 "$D/docs/quuxbarian-notes.md"
run_check "$BL" "$D"
check "讀不了的檔：結束碼 2、錯誤訊息裡的私人字換成 [私人清單]" '[ $RC = 2 ] && grep -q "有檔讀不了" <<<"$OUT" && grep -qF "[私人清單]" <<<"$OUT" && ! grep -qi quuxbarian <<<"$OUT"'
chmod 644 "$D/docs/quuxbarian-notes.md"
mkdir -p "$D/zorblax-dir" && echo x >"$D/zorblax-dir/a.md" && chmod 000 "$D/zorblax-dir"
printf 'zorblax\n' >"$T/bl-one.txt"
run_check "$T/bl-one.txt" "$D"
check "讀不了的資料夾：結束碼 2、私人字遮掉" '[ $RC = 2 ] && ! grep -qi zorblax <<<"$OUT"'
chmod 755 "$D/zorblax-dir"

newtree
ln -s "$USERS/secret" "$D/docs/link"
run_check "$BL" "$D"
check "符號連結不跟過去、掃它指向的路徑：docs/link:1: users-path" '[ $RC = 1 ] && grep -qF "docs/link:1: users-path" <<<"$OUT"'

newtree
printf 'x\n' >"$D/docs/$(printf 'tab\tname').md"
run_check "$BL" "$D"
check "檔名有控制字元：odd-filename、內容沒掃" '[ $RC = 1 ] && grep -qF "docs/tab?name.md:0: odd-filename" <<<"$OUT"'

newtree
printf '%s\n' "a $EMAIL b $EMAIL" "c" "d $EMAIL" >"$D/docs/many.md"
run_check "$BL" "$D"
check "同一行同一條規則只算一筆，依行號排：命中 2 筆" '[ $RC = 1 ] && grep -q "命中 2 筆（email 2）" <<<"$OUT" && [ "$(grep -c "^docs/many.md:" <<<"$OUT")" = 2 ]'

# ---- 放行清單 ---------------------------------------------------------------------------------
newtree
mkdir -p "$D/LICENSES"
printf '%s\n' "Copyright (c) 作者 <$EMAIL>" >"$D/LICENSES/thirdparty.txt"
printf '%s\n' "LICENSES/  email  # 第三方授權裡原作者的信箱" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "放行清單生效：LICENSES/ 的信箱放過、結束碼 0、算「放行 1 筆」" '[ $RC = 0 ] && grep -q "放行 1 筆" <<<"$OUT"'
echo "Zorblax Quindle" >>"$D/LICENSES/thirdparty.txt"
run_check "$BL" "$D"
check "放行只放那一條規則：同一個檔的私人字照樣擋" '[ $RC = 1 ] && grep -qF "LICENSES/thirdparty.txt:2: 測試人名" <<<"$OUT"'
printf '%s\n' "LICENSES/*.txt  *  # 整個檔都是第三方的原文" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "放行清單的 * 只放通用規則：私人字照樣擋" '[ $RC = 1 ] && grep -qF "LICENSES/thirdparty.txt:2: 測試人名" <<<"$OUT"'
printf '%s\r\n' "LICENSES/thirdparty.txt  測試人名  # 原文裡的作者名（CRLF 結尾）" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "私人規則寫出名字才放行（CRLF 結尾的行也認得）：結束碼 0" '[ $RC = 0 ]'
run_check "$T/no-such-blocklist.txt" "$D"
check "沒有私人清單時，寫私人規則名字的放行行不提醒「沒有用到」" '[ $RC = 0 ] && ! grep -q "放行清單第 4 行沒有用到" <<<"$OUT"'
printf '%s\n' "LICENSES/  no-such-rule  # 寫錯名字" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "放行清單寫了不認得的規則：提醒" 'grep -q "放行清單第 5 行的規則「no-such-rule」不認得" <<<"$OUT"'
newtree
printf '%s\n' "vendor/  *  # 這個樹裡沒有 vendor/" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "沒用到的放行行會提醒（不算失敗）" '[ $RC = 0 ] && grep -q "注意：放行清單第 2 行沒有用到" <<<"$OUT"'

newtree
printf '%s\n' "LICENSES/  email" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "放行行沒寫原因：格式錯（結束碼 2），說第 2 行沒寫原因" '[ $RC = 2 ] && grep -q "放行清單第 2 行沒寫原因" <<<"$OUT"'
newtree
printf '%s\n' "LICENSES/  email  extra  # 多一欄" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "放行行多一欄：格式錯（結束碼 2）" '[ $RC = 2 ] && grep -q "放行清單第 2 行要剛好兩欄" <<<"$OUT"'
newtree
printf '%s\n' "docs/  odd-filename  # 想放過" >>"$D/ai_docs/public-allow.txt"
run_check "$BL" "$D"
check "odd-filename 不能放行（結束碼 2）" '[ $RC = 2 ] && grep -q "odd-filename 不能放行" <<<"$OUT"'

# ---- 沒有私人清單 -----------------------------------------------------------------------------
newtree
run_check "$T/no-such-blocklist.txt" "$D"
check "沒有私人清單：預設只提醒（結束碼 0），印「注意：沒有私人清單，只跑通用規則」" '[ $RC = 0 ] && grep -q "^注意：沒有私人清單，只跑通用規則" <<<"$OUT"'
run_check "$T/no-such-blocklist.txt" --require-blocklist "$D"
check "沒有私人清單：加 --require-blocklist 就失敗（結束碼 3）" '[ $RC = 3 ] && grep -q "找不到私人清單" <<<"$OUT"'
run_check "" "$D"
check "沒設 EVERGENIE_BLOCKLIST 時找 ~/.evergenie-release/blocklist.txt（印成 ~ 開頭）" '[ $RC = 0 ] && grep -qF "找的是 ~/.evergenie-release/blocklist.txt" <<<"$OUT"'
mkdir -p "$HOME/.evergenie-release" && printf 'Zorblax Quindle\n' >"$HOME/.evergenie-release/blocklist.txt"
echo "zorblax quindle" >"$D/docs/x.md"
run_check "" "$D"
check "預設位置有清單就用它（假的 HOME 裡）：命中" '[ $RC = 1 ] && grep -qF "docs/x.md:1: 私人清單第 1 行" <<<"$OUT"'
rm -rf "$HOME/.evergenie-release"
printf '# 只有註解\n\n' >"$T/empty-blocklist.txt"
run_check "$T/empty-blocklist.txt" --require-blocklist "$D"
check "私人清單是空的（只有註解）：--require-blocklist 也失敗（結束碼 3）" '[ $RC = 3 ]'

# ---- 私人清單格式 -----------------------------------------------------------------------------
newtree
printf 're:(unclosedzz\n' >"$T/bad-re.txt"
run_check "$T/bad-re.txt" "$D"
check "私人清單的正規表示式寫錯：結束碼 2、只說行號、不印那一條" '[ $RC = 2 ] && grep -q "私人清單第 1 行的正規表示式寫錯了" <<<"$OUT" && ! grep -q unclosedzz <<<"$OUT"'
printf 're:x*\n' >"$T/empty-re.txt"
run_check "$T/empty-re.txt" "$D"
check "私人清單的規則會比對到空字串：結束碼 2" '[ $RC = 2 ] && grep -q "會比對到空字串" <<<"$OUT"'
cp "$BL" "$D/blocklist.txt"
run_check "$D/blocklist.txt" "$D"
check "私人清單放在要掃的資料夾裡面：結束碼 2（會跟著公開）" '[ $RC = 2 ] && grep -q "放在要掃的資料夾裡面" <<<"$OUT"'
ln -s "$D/blocklist.txt" "$T/bl-link.txt"
run_check "$T/bl-link.txt" "$D"
check "私人清單是指進要掃的資料夾的符號連結：結束碼 2" '[ $RC = 2 ] && grep -q "符號連結" <<<"$OUT"'
printf 'x  # email\n' >"$T/bl-clash.txt"
run_check "$T/bl-clash.txt" "$D"
check "私人清單的名字跟通用規則撞名：結束碼 2" '[ $RC = 2 ] && grep -q "撞名" <<<"$OUT"'
newtree
printf '\357\273\277Zorblax Quindle  # 測試人名\r\nre: moon-?castle-[0-9]+  # 測試地名\r\n' >"$T/bl-crlf.txt"
printf '%s\n' "zorblax quindle" "moon-castle-12 在行首" >"$D/docs/x.md"
run_check "$T/bl-crlf.txt" "$D"
check "私人清單有 BOM、CRLF、re: 後面有空白：照樣認得" '[ $RC = 1 ] && grep -qF "docs/x.md:1: 測試人名" <<<"$OUT" && grep -qF "docs/x.md:2: 測試地名" <<<"$OUT"'

# ---- git：只掃追蹤的檔 ------------------------------------------------------------------------
newtree
gitq -C "$D" init && gitq -C "$D" add -A && gitq -C "$D" commit -m init
echo "聯絡 $EMAIL" >"$D/docs/untracked.md"
run_check "$BL" "$D"
check "git 的最上層：只掃追蹤的檔（沒追蹤的檔有信箱也過）" '[ $RC = 0 ] && grep -q "掃的是所有 git 追蹤的檔" <<<"$OUT"'
gitq -C "$D" add docs/untracked.md
run_check "$BL" "$D"
check "git 的最上層：加進追蹤（還沒提交）就擋" '[ $RC = 1 ] && grep -qF "docs/untracked.md:1: email" <<<"$OUT"'

# ---- 用法 -------------------------------------------------------------------------------------
run_check "$BL" --help
check "--help：結束碼 0、印用法" '[ $RC = 0 ] && grep -q "scripts/public-check.sh \[--require-blocklist\]" <<<"$OUT"'
run_check "$BL" --bogus
check "不認得的參數：結束碼 2" '[ $RC = 2 ]'
run_check "$BL" "$T/no-such-dir"
check "資料夾不在：結束碼 2" '[ $RC = 2 ]'
run_check "$BL" "$HOME"
check "不掃整個家目錄：結束碼 2" '[ $RC = 2 ] && grep -q "不掃整個硬碟或整個家目錄" <<<"$OUT"'
mkdir -p "$T/empty-dir"
run_check "$BL" "$T/empty-dir"
check "空資料夾：結束碼 2" '[ $RC = 2 ] && grep -q "沒有檔可以掃" <<<"$OUT"'
newtree
gitq -C "$D" init && gitq -C "$D" add -A && gitq -C "$D" commit -m init
echo "聯絡 $EMAIL" >"$D/docs/untracked.md"
run_check "$BL" "$D/docs"
check "git 底下但不是最上層的資料夾：掃全部的檔（沒追蹤的也掃）" '[ $RC = 1 ] && grep -q "掃的是資料夾裡全部的檔" <<<"$OUT" && grep -qF "untracked.md:1: email" <<<"$OUT"'
mkdir -p "$T/subrepo" && echo sub >"$T/subrepo/a.md"
gitq -C "$T/subrepo" init && gitq -C "$T/subrepo" add -A && gitq -C "$T/subrepo" commit -m init
rm -f "$D/docs/untracked.md"
gitq -C "$D" -c protocol.file.allow=always submodule add "$T/subrepo" vendor/sub && gitq -C "$D" commit -m sub
run_check "$BL" "$D"
check "子模組：提醒內容沒掃（不算命中）" '[ $RC = 0 ] && grep -q "注意：子模組 1 個的內容沒掃" <<<"$OUT"'

# ---- export-public.sh -------------------------------------------------------------------------
# 暫存的 repo：乾淨的樹＋兩支腳本與共用的 awk、排除清單、進度紀錄（有私人字，要被排除）、一個沒追蹤的檔
SRC="$T/src"
mktree "$SRC"
mkdir -p "$SRC/scripts/lib" "$SRC/progress"
cp "$ROOT/scripts/public-check.sh" "$ROOT/scripts/export-public.sh" "$SRC/scripts/"
cp "$ROOT/scripts/lib/public-glob.awk" "$SRC/scripts/lib/"
printf '%s\n' "# 測試的排除清單" "progress/  # 進度紀錄是內部的" >"$SRC/ai_docs/public-exclude.txt"
echo "今天跟 Zorblax Quindle 開會" >"$SRC/progress/p1.md"
ln -s README.md "$SRC/docs/readme-link"
gitq -C "$SRC" init && gitq -C "$SRC" add -A && gitq -C "$SRC" commit -m init
echo "聯絡 $EMAIL" >"$SRC/notes.txt"
EXPORT="$SRC/scripts/export-public.sh"
run_export() { # <私人清單（空字串＝不設）> <參數…>
  local bl="$1"
  shift
  OUT="$(EVERGENIE_BLOCKLIST="$bl" "$SH" "$EXPORT" "$@" 2>&1)"
  RC=$?
}

run_export "$BL" --no-check "$T/out1"
check "export --no-check：結束碼 0、印抄了 8 個、排除了 1 個" '[ $RC = 0 ] && grep -q "抄了 8 個檔" <<<"$OUT" && grep -q "排除了 1 個" <<<"$OUT"'
check "export：排除清單生效（沒有 progress/）、不帶 .git、沒追蹤的檔不抄" '[ -f "$T/out1/README.md" ] && [ ! -e "$T/out1/progress" ] && [ ! -e "$T/out1/.git" ] && [ ! -e "$T/out1/notes.txt" ]'
check "export：內容一樣、執行權限留著、符號連結抄成連結" 'cmp -s "$SRC/README.md" "$T/out1/README.md" && cmp -s "$SRC/img/logo.png" "$T/out1/img/logo.png" && [ -x "$T/out1/scripts/public-check.sh" ] && [ -L "$T/out1/docs/readme-link" ] && [ "$(readlink "$T/out1/docs/readme-link")" = README.md ]'
rm "$SRC/img/logo.png"
run_export "$BL" --no-check "$T/out1b"
check "export：工作樹裡刪掉的追蹤檔不抄、會說" '[ $RC = 0 ] && grep -q "工作樹裡刪掉的 1 個沒抄" <<<"$OUT" && [ ! -e "$T/out1b/img/logo.png" ]'
gitq -C "$SRC" checkout -- img/logo.png
(cd "$T" && EVERGENIE_BLOCKLIST="$BL" "$SH" "$EXPORT" --no-check out-rel >/dev/null 2>&1)
check "export：相對路徑的目的地照呼叫的地方算" '[ -f "$T/out-rel/README.md" ]'

mkdir -p "$T/out2" && echo keep >"$T/out2/keep.txt"
run_export "$BL" --no-check "$T/out2"
check "export：目的地不是空的就拒絕（結束碼 2）、裡面原本的東西沒動、什麼都沒抄" '[ $RC = 2 ] && grep -q "不是空的" <<<"$OUT" && [ "$(ls -A "$T/out2")" = keep.txt ]'
run_export "$BL" --no-check "$SRC/out"
check "export：目的地在 repo 裡面就拒絕（結束碼 2）、沒建資料夾" '[ $RC = 2 ] && grep -q "不能在 repo 裡面" <<<"$OUT" && [ ! -e "$SRC/out" ]'
run_export "$BL" --no-check "$T/nowhere/../src/out"
check "export：目的地寫 .. 就拒絕（結束碼 2）、repo 裡沒多出東西" '[ $RC = 2 ] && grep -q "不要寫 \.\." <<<"$OUT" && [ ! -e "$SRC/out" ]'
echo x >"$T/out-file"
run_export "$BL" --no-check "$T/out-file"
check "export：目的地是檔就拒絕（結束碼 2）" '[ $RC = 2 ] && grep -q "不是資料夾" <<<"$OUT"'
mkdir -p "$T/out-real" && ln -s "$T/out-real" "$T/out-link"
run_export "$BL" --no-check "$T/out-link"
check "export：目的地是符號連結就拒絕（結束碼 2）" '[ $RC = 2 ] && [ -z "$(ls -A "$T/out-real")" ]'
mkdir -p "$T/out-locked" && echo keep >"$T/out-locked/keep.txt" && chmod 300 "$T/out-locked"
run_export "$BL" --no-check "$T/out-locked"
check "export：目的地讀不了就拒絕（不當成空的）" '[ $RC = 2 ] && grep -q "讀不了目的資料夾" <<<"$OUT"'
chmod 755 "$T/out-locked"

run_export "$BL" "$T/out3"
check "export 之後自動檢查：被排除的 progress/ 有私人字也不影響，結束碼 0、印「檢查過了」" '[ $RC = 0 ] && grep -q "檢查過了" <<<"$OUT"'
echo "zorblax quindle 的筆記" >"$SRC/docs/leak.md"
gitq -C "$SRC" add docs/leak.md
run_export "$BL" "$T/out4"
check "export 之後自動檢查會擋住髒的樹：結束碼 1、列出 docs/leak.md:1: 測試人名、提醒有沒提交的改動" \
  '[ $RC = 1 ] && grep -qF "docs/leak.md:1: 測試人名" <<<"$OUT" && grep -q "檢查沒過" <<<"$OUT" && grep -q "工作樹有沒提交的改動" <<<"$OUT"'
check "export 擋住時輸出裡沒有完整的私人字" '! grep -qi "zorblax" <<<"$OUT"'
gitq -C "$SRC" rm -f docs/leak.md

run_export "$T/no-such-blocklist.txt" "$T/out5"
check "export 不加 --no-check 又沒有私人清單：失敗（結束碼 3）" '[ $RC = 3 ]'

printf '%s\n' "/progress/  # 照 .gitignore 的習慣寫" >"$SRC/ai_docs/public-exclude.txt"
run_export "$BL" --no-check "$T/out7"
check "排除清單的行沒排除到任何檔：格式錯（結束碼 2）、什麼都沒抄" '[ $RC = 2 ] && grep -q "排除清單第 1 行沒有排除到任何檔" <<<"$OUT" && [ ! -e "$T/out7" ]'
printf '%s\n' "progress/  # 進度紀錄是內部的" >"$SRC/ai_docs/public-exclude.txt"
printf 'x\n' >"$SRC/docs/$(printf 'zorblax\tx').md"
gitq -C "$SRC" add -A
run_export "$BL" --no-check "$T/out8"
check "export：追蹤的檔名有控制字元就拒絕（結束碼 2）、訊息不印檔名" '[ $RC = 2 ] && grep -q "控制字元" <<<"$OUT" && ! grep -qi zorblax <<<"$OUT" && [ ! -e "$T/out8" ]'
gitq -C "$SRC" rm -f --cached "docs/$(printf 'zorblax\tx').md" && rm -f "$SRC/docs/$(printf 'zorblax\tx').md"
printf '%s\n' "progress/" >"$SRC/ai_docs/public-exclude.txt"
run_export "$BL" --no-check "$T/out6"
check "排除清單沒寫原因：格式錯（結束碼 2）、什麼都沒抄" '[ $RC = 2 ] && grep -q "排除清單第 1 行沒寫原因" <<<"$OUT" && [ ! -e "$T/out6" ]'
run_export "$BL" --help
check "export --help：結束碼 0" '[ $RC = 0 ] && grep -q "scripts/export-public.sh \[--no-check\]" <<<"$OUT"'
run_export "$BL"
check "export 沒給目的資料夾：結束碼 2" '[ $RC = 2 ]'

check "假 HOME 裡的 .evergenie-release 跑完有清掉（真的家目錄從頭到尾沒用到）" '[ ! -e "$HOME/.evergenie-release" ]'
echo "通過 ${pass}，失敗 ${fail}"
[ "$fail" -eq 0 ]
