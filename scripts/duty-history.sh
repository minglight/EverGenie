#!/bin/sh
# 產生官方職位樣板的「以前出現過的行」（agents/<id>/history.txt，DEV-154）。
# 家的遷移 2026-11-12-official-duties 用它分辨「家裡職責內文的這一行是舊版樣板的，還是主人自己加的」：
# 樣板每一版（git 紀錄，含改名前）出現過的非空白行，去掉行尾空白、去重、排序。樣板改版後不用重跑：
# 遷移只跑一次，之後職責直接讀樣板，不再存進家裡。要重產生：scripts/duty-history.sh（在 repo 根目錄）。
# 【不要再跑】history.txt 以 repo 裡這份為準：改名時直接改過裡面的指令名（那幾行沒在任何一版樣板出現過），
# 公開的 repo 又只有一個 commit（不含以前各版的樣板），在哪裡重跑都會把歷史行弄丟。
# 保險：重產生的結果少了現有 history.txt 的任何一行就停下來說明、不覆寫。
# 只重產生已經有 history.txt 的樣板（那次遷移認得的職位，cmd/evergenie/migrations.go 的 officialDutiesMigrated）；
# 之後才加的樣板（例：art，DEV-157）從來沒抄進家裡，不需要也不產生。
set -eu
cd "$(dirname "$0")/.."
for t in packages/official/*/agents/*/agent.md; do
  out="$(dirname "$t")/history.txt"
  [ -f "$out" ] || continue
  tmp="$(mktemp)"
  git log --follow --format=%H --name-only -- "$t" | awk 'NF==1 && length($0)==40 {h=$0; next} NF {print h" "$0}' |
  while read -r h p; do
    git show "$h:$p" 2>/dev/null | awk 'BEGIN{n=0} /^---[ \t]*$/ && n<2 {n++; next} n>=2 {print}'
  done | sed -e 's/[[:space:]]*$//' | grep -v '^$' | LC_ALL=C sort -u >"$tmp" || true
  lost="$(LC_ALL=C comm -23 "$out" "$tmp" | wc -l | tr -d ' ')"
  if [ "$lost" != 0 ]; then
    rm -f "$tmp"
    echo "duty-history：${out} 有 ${lost} 行在這份 git 紀錄裡找不到（紀錄不完整，例如公開版只有一個 commit；或 history.txt 被直接改過，例如改名時）；沒有覆寫，history.txt 以 repo 裡這份為準。" >&2
    exit 1
  fi
  mv "$tmp" "$out"
  echo "${out}: $(wc -l <"${out}" | tr -d ' ') lines"
done
