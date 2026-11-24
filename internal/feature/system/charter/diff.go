package charter

import (
	"strings"

	"github.com/minglight/evergenie/sdk"
)

// maxDiffCells：中間不同的部分超過這麼多「舊行數×新行數」就不逐行比，整段當成刪掉再加上（避免大檔吃記憶體）。
const maxDiffCells = 4_000_000

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lineDiff 逐行比較 from → to（最長共同子序列）；先拿掉頭尾一樣的行。
func lineDiff(from, to string) []sdk.RulesDiffLine {
	a, b := splitLines(from), splitLines(to)
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	out := make([]sdk.RulesDiffLine, 0, len(a)+len(b)-pre-suf)
	for _, l := range a[:pre] {
		out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpSame, Text: l})
	}
	out = append(out, middle(a[pre:len(a)-suf], b[pre:len(b)-suf])...)
	for _, l := range a[len(a)-suf:] {
		out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpSame, Text: l})
	}
	return out
}

func middle(a, b []string) []sdk.RulesDiffLine {
	var out []sdk.RulesDiffLine
	if len(a)*len(b) > maxDiffCells {
		for _, l := range a {
			out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpDel, Text: l})
		}
		for _, l := range b {
			out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpAdd, Text: l})
		}
		return out
	}
	// lcs[i][j]：a[i:] 與 b[j:] 的最長共同子序列長度。
	n, m := len(a), len(b)
	lcs := make([][]int32, n+1)
	for i := range lcs {
		lcs[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpSame, Text: a[i]})
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpDel, Text: a[i]})
			i++
		default:
			out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpAdd, Text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpDel, Text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, sdk.RulesDiffLine{Op: sdk.RulesDiffOpAdd, Text: b[j]})
	}
	return out
}
