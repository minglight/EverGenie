# 公開前的兩份路徑清單共用的「路徑樣式」：scripts/public-check.sh 的放行清單、scripts/export-public.sh 的排除清單。
# 兩支都用這一份，規則才不會漂掉；寫法說明只在 ai_docs/runbooks/public-release.md。
# * → 任何字（可以跨資料夾）；? → 一個位元組（LC_ALL=C 下跑，中文字要用 *）；結尾 / → 這個資料夾底下全部；其他照字面。
function glob2re(g,    re, i, c, n, dir) {
  n = length(g)
  dir = (substr(g, n, 1) == "/")
  re = "^"
  for (i = 1; i <= n; i++) {
    c = substr(g, i, 1)
    if (c == "*") re = re ".*"
    else if (c == "?") re = re "."
    else if (index("\\^$.[]|()+{}", c)) re = re "\\" c
    else re = re c
  }
  return dir ? re : re "$"
}
