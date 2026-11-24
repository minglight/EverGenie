#!/usr/bin/env bash
# 公開前的檢查（DEV-158）：掃一個要公開的資料夾，找主人的私人字與通用的洩漏樣式，有命中就結束碼 1，一行一筆「檔案:行號: 規則」。
# 規則、兩份清單的格式、命中了怎麼處理：ai_docs/runbooks/public-release.md（只寫在那裡）。
#
#   scripts/public-check.sh [--require-blocklist] [<資料夾>]
#
# <資料夾> 預設是這個 repo；是 git 的最上層就只掃 git 追蹤的檔，不是就掃全部的檔（.git 不算）。
# 私人清單：環境變數 EVERGENIE_BLOCKLIST，預設 ~/.evergenie-release/blocklist.txt。放行清單：<資料夾>/ai_docs/public-allow.txt。
# --require-blocklist：沒有私人清單（或清單是空的）就失敗；正式公開一律加。
# 只用 macOS 內建的工具加 git，bash 3.2 跑得動。
# 結束碼：0 沒有命中；1 有命中；2 用法錯誤、清單格式錯或有檔讀不了；3 加了 --require-blocklist 但沒有私人清單（或清單是空的）。
set -uo pipefail
# 位元組比對：-i 只摺 ASCII 大小寫、[A-Za-z] 不含其他字母，中文照位元組比；排序固定
export LC_ALL=C
HERE="$(cd "$(dirname "$0")" && pwd -P)" || exit 2
GLOB_AWK="$HERE/lib/public-glob.awk"

usage() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; }
bad() { echo "public-check：$*" >&2; exit 2; }
# 印路徑時家目錄寫成 ~（log 裡不要有 /Users/<名字>）
tilde() { local h="${HOME:-}"; if [ -n "$h" ] && [ "${1#"$h"/}" != "$1" ]; then printf '~/%s' "${1#"$h"/}"; else printf '%s' "$1"; fi; }
trim() { local s="$1"; s="${s#"${s%%[![:space:]]*}"}"; printf '%s' "${s%"${s##*[![:space:]]}"}"; }
# 跟著符號連結解到真正的位置（macOS 舊版的 readlink 沒有 -f）
real_path() {
  local p="$1" t i=0 d
  while [ -L "$p" ] && [ "$i" -lt 40 ]; do
    t="$(readlink "$p")" || return 1
    case "$t" in /*) p="$t" ;; *) p="$(dirname "$p")/$t" ;; esac
    i=$((i + 1))
  done
  d="$(cd "$(dirname "$p")" && pwd -P)" || return 1
  printf '%s/%s' "$d" "$(basename "$p")"
}

REQUIRE=0 DIR=""
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help) usage; exit 0 ;;
    --require-blocklist) REQUIRE=1 ;;
    -*) bad "不認得的參數：$1（scripts/public-check.sh --help 看用法）" ;;
    *) [ -z "$DIR" ] || bad "只能給一個資料夾：$1"; DIR="$1" ;;
  esac
  shift
done
[ -f "$GLOB_AWK" ] || bad "少了 $GLOB_AWK"
[ -n "$DIR" ] || DIR="$HERE/.."
[ -d "$DIR" ] || bad "找不到資料夾 $DIR"
ROOT="$(cd "$DIR" && pwd -P)" || bad "進不去 $DIR"
HOME_REAL="$(cd "${HOME:-/nonexistent}" 2>/dev/null && pwd -P)"
case "$ROOT" in / | "${HOME_REAL:-/nonexistent}") bad "不掃整個硬碟或整個家目錄，請給要公開的那個資料夾" ;; esac

W="$(mktemp -d "${TMPDIR:-/tmp}/evergenie-public-check.XXXXXX")" || bad "建不了暫存資料夾"
trap 'rm -rf "$W"' EXIT

# ---- 規則 -------------------------------------------------------------------------------------
# 每條：名字、種類（gen 通用／priv 私人清單）、F 固定字串／E 擴充正規表示式、i 不分大小寫／s 分、樣式、錨點。
# 錨點：每個命中一定含有其中一個的固定字串（不分大小寫），先用它挑出要掃的檔（正規表示式掃整個 repo 很慢）；空的＝跟同種的私人規則一起挑。
# 通用規則的「示範寫法放過」在下面 report.awk 的 real()（example.com、/Users/you…），一條規則的樣式與放過的寫法只在這兩處。
R_NAME=() R_KIND=() R_MODE=() R_CASE=() R_PAT=() R_ANCHOR=()
add_rule() { R_NAME+=("$1"); R_KIND+=("$2"); R_MODE+=("$3"); R_CASE+=("$4"); R_PAT+=("$5"); R_ANCHOR+=("${6:-}"); }
add_rule email gen E s '[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+' '@'
add_rule ts-net gen E i '[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)*\.ts\.net' '.ts.net'
add_rule users-path gen E i '/Users/[A-Za-z0-9._-]+' '/users/'
# 前面接英數或 _ - 的不算（task-list-… 這種名字裡的 sk-）
add_rule secret-token gen E s '(^|[^A-Za-z0-9_-])(sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|xox[abposr]-[A-Za-z0-9-]{10,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|eyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,})' \
  'sk- ghp_ gho_ ghu_ ghs_ ghr_ github_pat_ xox akia aiza eyj'
add_rule private-key gen E s '-----BEGIN [A-Z0-9 ]*PRIVATE KEY[A-Z ]*-----' '-----begin'
add_rule secret-field gen E i "(secret|token|passwd|password|api[_-]?key|private[_-]?key|access[_-]?key|auth[_-]?key)[A-Za-z0-9_-]*[\"']?[[:space:]]*[:=]{1,2}[[:space:]]*[\"']?[A-Za-z0-9+/_=-]{32,}" \
  'secret token passw apikey api_key api-key privatekey private_key private-key accesskey access_key access-key authkey auth_key auth-key'
# 內網：10.、172.16～31.、192.168.、Tailscale 的 100.64～100.127 與它的 IPv6 段；前面接數字、字母或點的不算（版本號）。
# 冒號寫成 [:]：這支自己也會被掃，原始碼裡不能出現完整的位址開頭
add_rule private-ip gen E i '(^|[^0-9A-Za-z.])(10\.[0-9]{1,3}|172\.(1[6-9]|2[0-9]|3[01])|192\.168|100\.(6[4-9]|[7-9][0-9]|1[01][0-9]|12[0-7]))\.[0-9]{1,3}\.[0-9]{1,3}|fd7a[:]115c[:]a1e0[:][0-9a-f:]*' \
  '10. 172. 192.168. 100. fd7a:'
GENERIC_NAMES=" ${R_NAME[*]} image-metadata odd-filename "

# ---- 私人清單 ---------------------------------------------------------------------------------
BL="${EVERGENIE_BLOCKLIST:-${HOME:-/nonexistent}/.evergenie-release/blocklist.txt}"
NPRIV=0
if [ -e "$BL" ]; then
  [ -f "$BL" ] && [ -r "$BL" ] || bad "讀不了私人清單 $(tilde "$BL")"
  BL_AT="$(cd "$(dirname "$BL")" && pwd -P)/$(basename "$BL")"
  BL_REAL="$(real_path "$BL")" || bad "找不到私人清單真正的位置 $(tilde "$BL")"
  case "$BL_AT" in "$ROOT"/*) bad "私人清單放在要掃的資料夾裡面（$(tilde "$BL")），會跟著公開；請搬到 repo 外面" ;; esac
  case "$BL_REAL" in "$ROOT"/*) bad "私人清單是指進要掃的資料夾裡面的符號連結（$(tilde "$BL")），那份會跟著公開；請搬到 repo 外面" ;; esac
  n=0 errs=0
  while IFS= read -r line || [ -n "$line" ]; do
    n=$((n + 1))
    line="${line%$'\r'}"
    [ "$n" != 1 ] || line="${line#$'\xef\xbb\xbf'}"
    line="$(trim "$line")"
    case "$line" in '' | '#'*) continue ;; esac
    name=""
    case "$line" in *[[:space:]]'#'*) name="$(trim "${line#*[[:space:]]#}")"; line="$(trim "${line%%[[:space:]]#*}")" ;; esac
    mode=F pat="$line"
    case "$line" in re:*) mode=E pat="$(trim "${line#re:}")" ;; esac
    # 錯誤訊息只寫行號，不寫內容（清單本身就是私人的）
    if [ -z "$pat" ]; then
      echo "public-check：私人清單第 $n 行是空的規則" >&2; errs=$((errs + 1)); continue
    fi
    if [ "$mode" = E ]; then
      grep -q -E -i -e "$pat" /dev/null 2>/dev/null
      if [ $? -eq 2 ]; then echo "public-check：私人清單第 $n 行的正規表示式寫錯了" >&2; errs=$((errs + 1)); continue; fi
      if printf '\n' | grep -q -E -i -e "$pat"; then
        echo "public-check：私人清單第 $n 行會比對到空字串（每一行都算命中），請改寫" >&2; errs=$((errs + 1)); continue
      fi
    fi
    name="${name//[[:cntrl:]]/ }"
    [ -n "$name" ] || name="私人清單第 ${n} 行"
    case "$GENERIC_NAMES" in *" $name "*) echo "public-check：私人清單第 $n 行的名字跟通用規則撞名，請換一個" >&2; errs=$((errs + 1)); continue ;; esac
    add_rule "$name" priv "$mode" i "$pat"
    NPRIV=$((NPRIV + 1))
  done <"$BL"
  [ "$errs" = 0 ] || exit 2
fi
if [ "$NPRIV" = 0 ]; then
  if [ "$REQUIRE" = 1 ]; then
    echo "public-check：找不到私人清單或清單是空的（$(tilde "$BL")）；正式公開前要先放好，格式見 ai_docs/runbooks/public-release.md" >&2
    exit 3
  fi
  echo "注意：沒有私人清單，只跑通用規則（找的是 $(tilde "$BL")）" >&2
fi

# ---- 放行清單 ---------------------------------------------------------------------------------
ALLOW="$ROOT/ai_docs/public-allow.txt"
: >"$W/allow.tsv"
if [ -f "$ALLOW" ]; then
  n=0 errs=0
  while IFS= read -r line || [ -n "$line" ]; do
    n=$((n + 1))
    line="${line%$'\r'}"
    case "$(trim "$line")" in '' | '#'*) continue ;; esac
    reason="" body="$line"
    case "$line" in *[[:space:]]'#'*) reason="$(trim "${line#*[[:space:]]#}")"; body="${line%%[[:space:]]#*}" ;; esac
    p="" r="" extra=""
    read -r p r extra <<<"$body"
    if [ -z "$reason" ]; then
      echo "public-check：放行清單第 $n 行沒寫原因（格式：<路徑樣式>  <規則名或 *>  # 原因）" >&2; errs=$((errs + 1)); continue
    fi
    if [ -z "$r" ] || [ -n "$extra" ]; then
      echo "public-check：放行清單第 $n 行要剛好兩欄：路徑樣式、規則名或 *（然後是 # 原因）" >&2; errs=$((errs + 1)); continue
    fi
    if [ "$r" = odd-filename ]; then
      echo "public-check：放行清單第 $n 行：odd-filename 不能放行（那種檔的內容沒掃），請把檔案改名" >&2; errs=$((errs + 1)); continue
    fi
    printf '%s\t%s\t%s\n' "$n" "$p" "$r" >>"$W/allow.tsv"
  done <"$ALLOW"
  [ "$errs" = 0 ] || exit 2
fi

# ---- 要掃的檔 ---------------------------------------------------------------------------------
# 讀不了的檔：錯誤訊息收進 $W/err，最後一起印；裡面的檔名可能帶私人字，印之前換成 [私人清單]
: >"$W/err"
: >"$W/priv.F"
: >"$W/priv.E"
i=0
while [ "$i" -lt "${#R_NAME[@]}" ]; do
  [ "${R_KIND[$i]}" != priv ] || printf '%s\n' "${R_PAT[$i]}" >>"$W/priv.${R_MODE[$i]}"
  i=$((i + 1))
done
masked() { # <檔>：印出來，私人清單命中的字換成 [私人清單]
  : >"$W/mask.words"
  [ ! -s "$W/priv.F" ] || grep -o -i -F -f "$W/priv.F" -- "$1" >>"$W/mask.words" 2>/dev/null
  [ ! -s "$W/priv.E" ] || grep -o -i -E -f "$W/priv.E" -- "$1" >>"$W/mask.words" 2>/dev/null
  awk -v F_W="$W/mask.words" '
    FILENAME == F_W { if ($0 != "") WORD[++n] = $0; next }
    { for (k = 1; k <= n; k++) { s = $0; out = ""
        while ((p = index(s, WORD[k])) > 0) { out = out substr(s, 1, p - 1) "[私人清單]"; s = substr(s, p + length(WORD[k])) }
        $0 = out s }
      print }' "$W/mask.words" "$1"
}
unreadable() { # 有錯誤就印（遮過）並結束
  [ -s "$W/err" ] || return 0
  echo "public-check：有檔讀不了（沒掃完就不算過）：" >&2
  head -5 "$W/err" >"$W/err.head"
  masked "$W/err.head" >&2
  exit 2
}
if [ "$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null)" = "$ROOT" ]; then
  SRC="所有 git 追蹤的檔"
  list_files() { git -C "$ROOT" ls-files -z 2>>"$W/err"; }
else
  SRC="資料夾裡全部的檔"
  list_files() { (cd "$ROOT" && find . -name .git -prune -o \( -type f -o -type l \) -print0) 2>>"$W/err"; }
fi
# 檔名有控制字元（換行、tab…）的另外記：一行一個的清單放不下它們，直接算命中（odd-filename），內容不掃
{
  list_files | while IFS= read -r -d '' f; do
    f="${f#./}"
    case "$f" in
      *[[:cntrl:]]*) printf '%s\n' "${f//[[:cntrl:]]/?}" >&4 ;;
      *) if [ -L "$ROOT/$f" ]; then printf '%s\n' "$f" >&5; elif [ -f "$ROOT/$f" ]; then printf '%s\n' "$f" >&3; elif [ -d "$ROOT/$f" ]; then printf '%s\n' "$f" >&6; fi ;;
    esac
  done
  echo "${PIPESTATUS[0]}" >"$W/list.rc"
} 3>"$W/files" 4>"$W/odd" 5>"$W/links" 6>"$W/subs"
unreadable
[ "$(cat "$W/list.rc")" = 0 ] || bad "列不出要掃的檔"
[ -s "$W/files" ] || [ -s "$W/links" ] || [ -s "$W/odd" ] || [ -s "$W/subs" ] || bad "資料夾裡沒有檔可以掃"
cat "$W/files" "$W/links" "$W/odd" "$W/subs" >"$W/paths"

# 文字檔＝grep 認得是文字、至少一行；其他不是空的就是二進位檔
# 給 grep 的路徑前面加 ./（叫 - 的檔才不會被當成標準輸入）；grep 印出來的再拿掉
nul_list() { sed 's|^|./|' "$1" | tr '\n' '\0'; }
unprefix() { sed 's|^\./||'; }
in_root() { (cd "$ROOT" && "$@"); }
if [ -s "$W/files" ]; then nul_list "$W/files" | in_root xargs -0 -r grep -l -I -e '' -- 2>>"$W/err" | unprefix >"$W/text"; else : >"$W/text"; fi
sort -o "$W/text" "$W/text"
sort "$W/files" | comm -23 - "$W/text" >"$W/nontext"

# ---- 一條一條掃 -------------------------------------------------------------------------------
# 先挑出「可能有命中」的檔，再一條一條只掃那些：通用規則用自己的錨點；私人清單的固定字串、正規表示式各一起挑一次（-i 是超集）
candidates() { # <輸出> <grep 參數…>
  local out="$1"
  shift
  : >"$out"
  [ -s "$W/text" ] || return 0
  nul_list "$W/text" | in_root xargs -0 -r grep -l -I -i "$@" -- 2>>"$W/err" | unprefix >"$out"
}
[ ! -s "$W/priv.F" ] || candidates "$W/cand.F" -F -f "$W/priv.F"
[ ! -s "$W/priv.E" ] || candidates "$W/cand.E" -E -f "$W/priv.E"

# 符號連結不跟過去（可能指到 repo 外面）：掃它指向的路徑字串，當作第 1 行
: >"$W/targets"
while IFS= read -r f; do
  t="$(readlink "$ROOT/$f")"
  printf '%s\n' "${t//[[:cntrl:]]/?}" >>"$W/targets"
done <"$W/links"

# 紀錄（給 report.awk）：C 內容、P 路徑、L 連結目標、M 中繼資料、O 怪檔名、B 二進位檔清單、X 二進位檔裡的私人字；欄位用 tab 分
: >"$W/rec"
to_rec() { # <C|P|L> <規則序號>：把 grep 的輸出轉成紀錄
  awk -v t="$1" -v r="$2" '
    t == "C" { p = index($0, "\t"); if (!p) next; path = substr($0, 1, p - 1); sub(/^\.\//, "", path); $0 = substr($0, p + 1) }
    { c = index($0, ":"); if (!c) next
      if (t == "C") printf "C\t%s\t%s\t%s\t%s\n", r, path, substr($0, 1, c - 1), substr($0, c + 1)
      else printf "%s\t%s\t%s\t%s\n", t, r, substr($0, 1, c - 1), substr($0, c + 1) }'
}
: >"$W/rules.tsv"
i=0
while [ "$i" -lt "${#R_NAME[@]}" ]; do
  printf '%s\t%s\t%s\n' "$i" "${R_KIND[$i]}" "${R_NAME[$i]}" >>"$W/rules.tsv"
  fl=("-${R_MODE[$i]}")
  [ "${R_CASE[$i]}" = s ] || fl+=(-i)
  c="$W/cand.${R_MODE[$i]}"
  if [ -n "${R_ANCHOR[$i]}" ]; then
    c="$W/cand.$i" anchors=()
    for a in ${R_ANCHOR[$i]}; do anchors+=(-e "$a"); done
    candidates "$c" -F "${anchors[@]}"
  fi
  if [ -s "$c" ]; then
    # --null：檔名後面接 NUL（檔名有冒號也拆得開），再換成 tab（檔名沒有控制字元）
    nul_list "$c" | in_root xargs -0 -r grep --null -H -n -o -I "${fl[@]}" -e "${R_PAT[$i]}" -- 2>>"$W/err" | tr '\000' '\t' | to_rec C "$i" >>"$W/rec"
  fi
  grep -n -o "${fl[@]}" -e "${R_PAT[$i]}" -- "$W/paths" 2>>"$W/err" | to_rec P "$i" >>"$W/rec"
  [ ! -s "$W/targets" ] || grep -n -o "${fl[@]}" -e "${R_PAT[$i]}" -- "$W/targets" 2>>"$W/err" | to_rec L "$i" >>"$W/rec"
  i=$((i + 1))
done
unreadable

# 二進位檔：列清單；圖與影片找中繼資料（直接找檔案裡的標記：EXIF、XMP、QuickTime 的位置）
media_meta() { # <相對路徑>：印找到的中繼資料，沒有就不印
  local f="./$1" ext found=""
  ext="$(printf '%s' "${1##*.}" | tr 'A-Z' 'a-z')"
  case "$ext" in
    jpg | jpeg | png | webp | gif | heic | heif | avif | mp4 | mov | m4v | 3gp) ;;
    tif | tiff | dng) printf 'TIFF 的中繼資料這支看不出來，請換成 PNG'; return ;;
    *) return ;;
  esac
  (cd "$ROOT" && grep -a -q -i -F -e exif -- "$f") && found="EXIF"
  # PNG 的文字段（生圖工具常把提示詞放這裡）；JPEG 的註解段與 IPTC（FF FE 在 PNG、影片裡會隨機出現，只看 JPEG）
  if [ "$ext" = png ] && (cd "$ROOT" && grep -a -q -F -e tEXt -e iTXt -e zTXt -- "$f"); then found="${found:+$found、}PNG 文字段"; fi
  case "$ext" in jpg | jpeg)
    (cd "$ROOT" && grep -a -q -F -e "$(printf '\377\376')" -- "$f") && found="${found:+$found、}JPEG 註解"
    (cd "$ROOT" && grep -a -q -F -e 'Photoshop 3.0' -e 8BIM -- "$f") && found="${found:+$found、}IPTC" ;;
  esac
  (cd "$ROOT" && grep -a -q -F -e '<x:xmpmeta' -e 'XML:com.adobe.xmp' -e 'http://ns.adobe.com/xap/1.0/' -- "$f") && found="${found:+$found、}XMP"
  (cd "$ROOT" && grep -a -q -F -e "$(printf '\251xyz')" -e 'location.ISO6709' -- "$f") && found="${found:+$found、}位置"
  # EXIF 裡有 GPS 那一段（標籤 0x8825，大端或小端）；grep -q 先結束時 od 會收到 SIGPIPE，這段不用 pipefail
  if [ "${found#EXIF}" != "$found" ] && (set +o pipefail; cd "$ROOT" && od -An -v -tx1 "$f" | tr -s ' \n' '  ' | grep -q -e ' 88 25 00 04 ' -e ' 25 88 04 00 '); then
    found="$found（含 GPS）"
  fi
  printf '%s' "$found"
}
NBIN=0
while IFS= read -r f; do
  size="$(wc -c <"$ROOT/$f" | tr -d ' ')"
  [ "$size" != 0 ] || continue
  NBIN=$((NBIN + 1))
  printf 'B\t%s\t%s\n' "$f" "$size" >>"$W/rec"
  meta="$(media_meta "$f")"
  [ -z "$meta" ] || printf 'M\t%s\t%s\n' "$f" "$meta" >>"$W/rec"
done <"$W/nontext"
while IFS= read -r f; do printf 'O\t%s\n' "$f" >>"$W/rec"; done <"$W/odd"
# 二進位檔的內容也找私人字（圖的中繼資料、帶 NUL 的紀錄檔）；先一起挑，再一條一條認是哪條
if [ -s "$W/nontext" ] && [ "$NPRIV" -gt 0 ]; then
  : >"$W/bcand"
  [ ! -s "$W/priv.F" ] || nul_list "$W/nontext" | in_root xargs -0 -r grep -a -l -i -F -f "$W/priv.F" -- 2>>"$W/err" | unprefix >>"$W/bcand"
  [ ! -s "$W/priv.E" ] || nul_list "$W/nontext" | in_root xargs -0 -r grep -a -l -i -E -f "$W/priv.E" -- 2>>"$W/err" | unprefix >>"$W/bcand"
  if [ -s "$W/bcand" ]; then
    sort -u -o "$W/bcand" "$W/bcand"
    i=0
    while [ "$i" -lt "${#R_NAME[@]}" ]; do
      if [ "${R_KIND[$i]}" = priv ]; then
        nul_list "$W/bcand" | in_root xargs -0 -r grep -a -l -i "-${R_MODE[$i]}" -e "${R_PAT[$i]}" -- 2>>"$W/err" | unprefix |
          awk -v r="$i" '{ printf "X\t%s\t%s\n", r, $0 }' >>"$W/rec"
      fi
      i=$((i + 1))
    done
  fi
fi
unreadable
NSUB="$(wc -l <"$W/subs" | tr -d ' ')"
[ "$NSUB" = 0 ] || echo "注意：子模組 $NSUB 個的內容沒掃（不在這個 repo 裡；.gitmodules 與子模組的路徑有掃）" >&2

# ---- 整理：放過示範寫法、套放行清單、遮字、去重複 ------------------------------------------------
cat >"$W/report.awk" <<'AWK'
BEGIN {
  FS = "\t"
  split("you yourname your-name example someone me user username shared test tester", a, " "); for (i in a) DEMO_USER[a[i]] = 1
  split("example invalid test localhost", a, " "); for (i in a) RESERVED_TLD[a[i]] = 1
  # 「名字@2x.png」這種檔名不是信箱
  split("png jpg jpeg gif webp svg ico heic avif bmp tif tiff mp4 mov webm mp3 m4a wav pdf txt md json toml yaml yml go ts tsx js mjs css html sh bak zip gz log", a, " "); for (i in a) FILE_EXT[a[i]] = 1
  KNOWN["image-metadata"] = 1; KNOWN["odd-filename"] = 1
}
FILENAME == F_RULES { KIND[$1] = $2; NAME[$1] = $3; KNOWN[$3] = 1; if ($2 == "priv") PRIV[$3] = 1; next }
FILENAME == F_ALLOW { NA++; ALN[NA] = $1; ARE[NA] = glob2re($2); ARULE[NA] = $3; next }
FILENAME == F_PATHS { NP++; P_PATH[NP] = $0; P_MASK[NP] = $0; next }
FILENAME == F_LINKS { NL++; LINK[NL] = $0; next }
FILENAME == F_REC { NR2++; REC[NR2] = $0; next }

function after(s, n,    i, p) { for (i = 0; i < n; i++) { p = index(s, "\t"); if (!p) return ""; s = substr(s, p + 1) } return s }
function field(s, n,    a) { split(s, a, "\t"); return a[n] }
function placeholder(s) { return index(s, "xxxxxxxx") || index(s, "XXXXXXXX") || index(s, "********") || index(s, "00000000") }
# 通用規則的示範寫法放過；CLEAN 是去掉前後文字的命中字。放過哪些寫在 runbook 的「內建放過的寫法」，改這裡要一起改
function real(r, m,    n, p, local, dom, tld, v, a, i) {
  CLEAN = m
  if (KIND[r] != "gen") return 1
  n = NAME[r]
  if (n == "email") {
    p = index(m, "@"); local = tolower(substr(m, 1, p - 1)); dom = tolower(substr(m, p + 1))
    sub(/-+$/, "", dom); tld = dom; sub(/.*\./, "", tld)
    if (tld !~ /^[a-z][a-z]+$/ || tld in FILE_EXT || tld in RESERVED_TLD) return 0
    if (dom ~ /(^|\.)example\.(com|net|org)$/) return 0
    if (local ~ /^(no-?reply|do-?not-?reply|git)$/ && dom ~ /^(github\.com|gitlab\.com|bitbucket\.org|anthropic\.com)$/) return 0
    return 1
  }
  # 只有 .ts.net 前面那一段就是 example 的才放過（example.<真的 tailnet>.ts.net 照樣擋）
  if (n == "ts-net") return tolower(m) !~ /(^|\.)example\.ts\.net$/
  if (n == "users-path") { v = tolower(substr(m, 8)); sub(/\.+$/, "", v); return !(length(v) <= 1 || v in DEMO_USER) }
  if (n == "secret-token") { sub(/^[^A-Za-z0-9_-]/, "", CLEAN); return !placeholder(CLEAN) }
  if (n == "secret-field") {
    match(m, /[:=]/); v = substr(m, RSTART); sub(/^[:=]+[ \t]*["']?/, "", v)
    return v ~ /[0-9]/ && v ~ /[A-Za-z]/ && !placeholder(v)
  }
  if (n == "private-ip") {
    sub(/^[^0-9A-Fa-f]/, "", CLEAN)
    if (tolower(substr(CLEAN, 1, 5)) == "fd7a:") return 1
    split(CLEAN, a, "."); for (i = 1; i <= 4; i++) if (a[i] + 0 > 255) return 0
    return 1
  }
  return 1
}
function mask(s,    n) { n = length(s); return n <= 6 ? substr(s, 1, 1) "…" : substr(s, 1, 2) "…" substr(s, n - 1, 2) }
function label(r, m) { return KIND[r] == "priv" ? NAME[r] : NAME[r] "（" mask(m) "）" }
function allowed(path, rule,    k) {
  # * 只放通用規則：私人清單的字要放行就得寫出那條的名字（免得日後真名進了同一個資料夾也被放過）
  for (k = 1; k <= NA; k++) if (((ARULE[k] == "*" && !(rule in PRIV)) || ARULE[k] == rule) && path ~ ARE[k]) { USED[k]++; return 1 }
  return 0
}
# where：同一個檔的第 0 行可能是檔名命中、也可能是二進位檔內容命中，分開算
function emit(path, line, rule, text, where,    key) {
  key = path SUBSEP line SUBSEP rule SUBSEP where
  if (key in SEEN) return
  SEEN[key] = 1
  if (rule != "odd-filename" && allowed(path, rule)) { NALLOWED++; return }
  printf "%s\t%d\t%s\n", (path in DISP ? DISP[path] : path), line, text > OUT_HITS
  COUNT[rule]++
  if (!(rule in ORDER)) { ORDER[rule] = ++NORDER; RULE_AT[NORDER] = rule }
}
function replace_all(s, from, to,    p, out) {
  out = ""
  while (from != "" && (p = index(s, from)) > 0) { out = out substr(s, 1, p - 1) to; s = substr(s, p + length(from)) }
  return out s
}
END {
  # 先遮路徑（私人字一律遮；通用規則只遮真的命中），印任何一行之前都要遮好
  for (k = 1; k <= NR2; k++) {
    if (field(REC[k], 1) != "P") continue
    r = field(REC[k], 2); idx = field(REC[k], 3); m = after(REC[k], 3)
    if (KIND[r] == "priv" || real(r, m)) P_MASK[idx] = replace_all(P_MASK[idx], m, "[" NAME[r] "]")
  }
  for (idx = 1; idx <= NP; idx++) DISP[P_PATH[idx]] = P_MASK[idx]
  for (k = 1; k <= NR2; k++) {
    t = field(REC[k], 1)
    if (t == "P") {
      r = field(REC[k], 2); idx = field(REC[k], 3); m = after(REC[k], 3)
      if (real(r, m)) emit(P_PATH[idx], 0, NAME[r], NAME[r] "（檔名）", "name")
    } else if (t == "C" || t == "L") {
      r = field(REC[k], 2)
      if (t == "C") { path = field(REC[k], 3); line = field(REC[k], 4); m = after(REC[k], 4) }
      else { path = LINK[field(REC[k], 3)]; line = 1; m = after(REC[k], 3) }
      if (real(r, m)) emit(path, line, NAME[r], label(r, CLEAN))
    } else if (t == "X") {
      r = field(REC[k], 2)
      emit(field(REC[k], 3), 0, NAME[r], NAME[r] "（二進位檔內容）", "bin")
    } else if (t == "M") {
      emit(field(REC[k], 2), 0, "image-metadata", "image-metadata（" after(REC[k], 2) "）")
    } else if (t == "O") {
      path = field(REC[k], 2)
      emit(path, 0, "odd-filename", "odd-filename（檔名有換行、tab 這類控制字元，內容沒掃；請改名）")
    } else if (t == "B") {
      path = field(REC[k], 2)
      printf "  %s  %s bytes\n", (path in DISP ? DISP[path] : path), field(REC[k], 3) > OUT_BIN
    }
  }
  for (k = 1; k <= NORDER; k++) printf "%s\t%d\n", RULE_AT[k], COUNT[RULE_AT[k]] > OUT_COUNT
  printf "%d\n", NALLOWED > OUT_ALLOWED
  for (k = 1; k <= NA; k++) {
    if (ARULE[k] != "*" && !(ARULE[k] in KNOWN) && HAVE_PRIV) printf "注意：放行清單第 %s 行的規則「%s」不認得（通用規則與私人清單都沒有）\n", ALN[k], ARULE[k] > "/dev/stderr"
    # 沒有私人清單時，寫私人規則名字的行本來就用不到，不提醒
    else if (!USED[k] && (HAVE_PRIV || ARULE[k] == "*" || ARULE[k] in KNOWN)) printf "注意：放行清單第 %s 行沒有用到，可以拿掉\n", ALN[k] > "/dev/stderr"
  }
}
AWK
: >"$W/hits" && : >"$W/bin" && : >"$W/count" && : >"$W/allowed"
awk -v F_RULES="$W/rules.tsv" -v F_ALLOW="$W/allow.tsv" -v F_PATHS="$W/paths" -v F_LINKS="$W/links" -v F_REC="$W/rec" \
  -v OUT_HITS="$W/hits" -v OUT_BIN="$W/bin" -v OUT_COUNT="$W/count" -v OUT_ALLOWED="$W/allowed" -v HAVE_PRIV="$NPRIV" \
  -f "$GLOB_AWK" -f "$W/report.awk" "$W/rules.tsv" "$W/allow.tsv" "$W/paths" "$W/links" "$W/rec" || bad "整理結果失敗"

# ---- 印結果 -----------------------------------------------------------------------------------
TAB="$(printf '\t')"
sort -t "$TAB" -k1,1 -k2,2n -k3,3 "$W/hits" | awk -F '\t' '{ printf "%s:%s: %s\n", $1, $2, $3 }'
NHIT="$(wc -l <"$W/hits" | tr -d ' ')"
NTEXT="$(wc -l <"$W/text" | tr -d ' ')"
if [ "$NBIN" -gt 0 ]; then
  echo "二進位檔 $NBIN 個（不掃內容，公開前請逐一看過）："
  sort "$W/bin"
fi
allowed="$(cat "$W/allowed")"
info="掃的是${SRC}（文字檔 $NTEXT 個、二進位檔 $NBIN 個）；私人清單 $NPRIV 條；放行 $allowed 筆"
if [ "$NHIT" -gt 0 ]; then
  counts="$(sort -t "$TAB" -k2,2nr -k1,1 "$W/count" | awk -F '\t' '{ printf "%s%s %s", (NR > 1 ? "、" : ""), $1, $2 }')"
  echo "public-check：命中 $NHIT 筆（${counts}）。$info"
  exit 1
fi
echo "public-check：沒有命中。$info"
