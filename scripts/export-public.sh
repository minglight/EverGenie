#!/usr/bin/env bash
# 匯出要公開的檔案樹（DEV-158）：把這個 repo 的 git 追蹤的檔，照 ai_docs/public-exclude.txt 排除之後抄到 <目的資料夾>（不帶 .git），
# 再對那份跑 scripts/public-check.sh --require-blocklist。清單格式、命中了怎麼處理：ai_docs/runbooks/public-release.md（只寫在那裡）。
#
#   scripts/export-public.sh [--no-check] <目的資料夾>
#
# 目的資料夾要不存在或是空的，不能在這個 repo 裡面，也不要寫 ..。抄的是工作樹現在的內容（有沒提交的改動會提醒）。
# --no-check：抄完不檢查（只給測試用；這樣匯出的不能拿去公開）。
# 只用 macOS 內建的工具加 git，bash 3.2 跑得動。
# 結束碼：0 抄完而且檢查過了；1 檢查有命中（抄好的檔留在目的資料夾）；
#   2 用法錯誤、清單格式錯、目的資料夾不能用或抄失敗；3 沒有私人清單（public-check.sh 的 --require-blocklist）。
set -uo pipefail
export LC_ALL=C
CALLER_PWD="$PWD"
HERE="$(cd "$(dirname "$0")" && pwd -P)" || exit 2
ROOT="$(cd "$HERE/.." && pwd -P)" || exit 2
GLOB_AWK="$HERE/lib/public-glob.awk"

usage() { awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } NR > 1 { exit }' "$0"; }
bad() { echo "export-public：$*" >&2; exit 2; }
tilde() { local h="${HOME:-}"; if [ -n "$h" ] && [ "${1#"$h"/}" != "$1" ]; then printf '~/%s' "${1#"$h"/}"; else printf '%s' "$1"; fi; }
trim() { local s="$1"; s="${s#"${s%%[![:space:]]*}"}"; printf '%s' "${s%"${s##*[![:space:]]}"}"; }

CHECK=1 DEST=""
while [ $# -gt 0 ]; do
  case "$1" in
    -h | --help) usage; exit 0 ;;
    --no-check) CHECK=0 ;;
    -*) bad "不認得的參數：$1（scripts/export-public.sh --help 看用法）" ;;
    *) [ -z "$DEST" ] || bad "只能給一個目的資料夾：$1"; DEST="$1" ;;
  esac
  shift
done
[ -n "$DEST" ] || bad "要給目的資料夾（scripts/export-public.sh --help 看用法）"
case "$DEST" in /*) ;; *) DEST="$CALLER_PWD/$DEST" ;; esac
# 有 .. 的話，下面「往上找現存的資料夾」會算錯位置
case "/$DEST/" in */../*) bad "目的資料夾不要寫 ..：$(tilde "$DEST")" ;; esac
[ -f "$GLOB_AWK" ] || bad "少了 $GLOB_AWK"
[ "$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null)" = "$ROOT" ] || bad "$(tilde "$ROOT") 不是 git 的最上層，不知道哪些檔是追蹤的"

W="$(mktemp -d "${TMPDIR:-/tmp}/evergenie-export.XXXXXX")" || bad "建不了暫存資料夾"
trap 'rm -rf "$W"' EXIT

# 排除清單：先整份檢查格式，有錯就什麼都不抄
EXCLUDE="$ROOT/ai_docs/public-exclude.txt"
[ -f "$EXCLUDE" ] || bad "沒有排除清單 ai_docs/public-exclude.txt"
: >"$W/exclude.tsv"
n=0 errs=0
while IFS= read -r line || [ -n "$line" ]; do
  n=$((n + 1))
  line="${line%$'\r'}"
  case "$(trim "$line")" in '' | '#'*) continue ;; esac
  reason="" body="$line"
  case "$line" in *[[:space:]]'#'*) reason="$(trim "${line#*[[:space:]]#}")"; body="${line%%[[:space:]]#*}" ;; esac
  p="" extra=""
  read -r p extra <<<"$body"
  if [ -z "$reason" ]; then
    echo "export-public：排除清單第 $n 行沒寫原因（格式：<路徑樣式>  # 原因）" >&2; errs=$((errs + 1)); continue
  fi
  if [ -z "$p" ] || [ -n "$extra" ]; then
    echo "export-public：排除清單第 $n 行要剛好一個路徑樣式（然後是 # 原因）" >&2; errs=$((errs + 1)); continue
  fi
  printf '%s\t%s\n' "$n" "$p" >>"$W/exclude.tsv"
done <"$EXCLUDE"
[ "$errs" = 0 ] || exit 2

# 目的資料夾：不存在或是空的、不在 repo 裡面（在裡面的話它自己會變成工作樹裡的新檔）
if [ -e "$DEST" ] || [ -L "$DEST" ]; then
  [ -d "$DEST" ] && [ ! -L "$DEST" ] || bad "目的地已經有東西、不是資料夾：$(tilde "$DEST")"
  listing="$(ls -A "$DEST" 2>/dev/null)" || bad "讀不了目的資料夾：$(tilde "$DEST")"
  [ -z "$listing" ] || bad "目的資料夾不是空的：$(tilde "$DEST")（請給一個新的或空的資料夾）"
fi
d="$DEST"
while [ ! -d "$d" ]; do d="$(dirname "$d")"; done
d="$(cd "$d" && pwd -P)" || bad "進不去 $(tilde "$d")"
case "$d/" in "$ROOT"/*) bad "目的資料夾不能在 repo 裡面：$(tilde "$DEST")" ;; esac

# 要抄的檔：git 追蹤的，照排除清單分兩堆
git -C "$ROOT" ls-files -z >"$W/tracked.z" || bad "列不出 git 追蹤的檔"
while IFS= read -r -d '' f; do
  case "$f" in *[[:cntrl:]]*) bad "有檔名帶換行、tab 這類控制字元；跑 scripts/public-check.sh 會列出是哪個（odd-filename），請先改名" ;; esac
  printf '%s\n' "$f"
done <"$W/tracked.z" >"$W/tracked"
cat >"$W/split.awk" <<'AWK'
FILENAME == F_EX { N++; LN[N] = $1; RE[N] = glob2re($2); next }
{ for (k = 1; k <= N; k++) if ($0 ~ RE[k]) { USED[k]++; print > OUT_EXCLUDED; next } print > OUT_KEPT }
# 沒排除到任何檔的行算格式錯：多半是樣式寫錯（例如照 .gitignore 的習慣在開頭加 /），本來要排除的檔就會流出去
END {
  for (k = 1; k <= N; k++) if (!USED[k]) { printf "export-public：排除清單第 %s 行沒有排除到任何檔（樣式寫錯了？開頭不要加 /；用不到就拿掉）\n", LN[k] > "/dev/stderr"; bad = 1 }
  if (bad) exit 3
}
AWK
: >"$W/kept" && : >"$W/excluded"
awk -F '\t' -v F_EX="$W/exclude.tsv" -v OUT_KEPT="$W/kept" -v OUT_EXCLUDED="$W/excluded" \
  -f "$GLOB_AWK" -f "$W/split.awk" "$W/exclude.tsv" "$W/tracked"
rc=$?
[ "$rc" = 3 ] && exit 2
[ "$rc" = 0 ] || bad "套不了排除清單"

if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=no 2>/dev/null)" ]; then
  echo "注意：工作樹有沒提交的改動，匯出的是工作樹現在的內容" >&2
fi
mkdir -p "$DEST" || bad "建不了目的資料夾 $(tilde "$DEST")"
real="$(cd "$DEST" && pwd -P)" || bad "進不去 $(tilde "$DEST")"
case "$real/" in "$ROOT"/*) bad "目的資料夾不能在 repo 裡面：$(tilde "$DEST")" ;; esac
copied=0 gone=0 skipped=0 last=""
while IFS= read -r f; do
  if [ ! -e "$ROOT/$f" ] && [ ! -L "$ROOT/$f" ]; then gone=$((gone + 1)); continue; fi
  # 子模組（git 追蹤的是一個資料夾）不抄
  if [ -d "$ROOT/$f" ] && [ ! -L "$ROOT/$f" ]; then skipped=$((skipped + 1)); continue; fi
  dir="${f%/*}"
  if [ "$dir" != "$f" ] && [ "$dir" != "$last" ]; then
    mkdir -p "$DEST/$dir" 2>/dev/null || bad "目的資料夾裡有資料夾建不了（不印名字：檔名可能帶私人字）"
    last="$dir"
  fi
  # -P：符號連結照原樣抄（不跟過去），檢查會掃它指向的路徑；-X：不帶延伸屬性（下載來源、標籤這類）
  cp -P -X "$ROOT/$f" "$DEST/$f" 2>/dev/null || bad "有檔抄不了（不印名字：檔名可能帶私人字；跑 scripts/public-check.sh 會列出讀不了的檔）"
  copied=$((copied + 1))
done <"$W/kept"
excluded="$(wc -l <"$W/excluded" | tr -d ' ')"
msg="export-public：抄了 $copied 個檔到 $(tilde "$DEST")，排除了 $excluded 個（ai_docs/public-exclude.txt）"
[ "$gone" = 0 ] || msg="$msg；工作樹裡刪掉的 $gone 個沒抄"
[ "$skipped" = 0 ] || msg="$msg；子模組 $skipped 個沒抄"
echo "$msg"

if [ "$CHECK" = 0 ]; then
  echo "注意：--no-check 沒有檢查，這份不能拿去公開" >&2
  exit 0
fi
"${BASH:-bash}" "$HERE/public-check.sh" --require-blocklist "$DEST"
rc=$?
if [ "$rc" != 0 ]; then
  echo "export-public：檢查沒過（結束碼 $rc）。匯出的檔留在 $(tilde "$DEST")；照上面清乾淨 repo 之後，刪掉那個資料夾重匯" >&2
  exit "$rc"
fi
echo "export-public：檢查過了，$(tilde "$DEST") 可以拿去公開"
