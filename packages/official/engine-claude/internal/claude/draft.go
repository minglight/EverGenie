package claude

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 還在打的那段（ECL-61，DEV-150）：平台要等整則訊息（連同後面的工具參數）生成完才寫進對話記錄，
// 秘書「先回一句再叫工具」的那句在畫面上 2～4 秒就出現、進記錄卻要 10 秒以上。跟隨對話時從畫面抓出
// 最後一段還沒進記錄的話，當成 draft 先送給看板；正式那則寫進記錄後看板換掉。

const draftBullet = "⏺ "

// 工具的狀態字（新版畫面圓點後面、或縮兩格接在話後面）：不是秘書說的話。
// 後面一定接數字（「Ran 2 shell commands」「Searched for 3 patterns」），英文的話剛好用 Read、Did 開頭不會誤判。
const draftStatusWords = `(Ran|Running|Read|Reading|Wrote|Writing|Edited|Editing|Updated|Updating|Searched|Searching|Listed|Listing|Fetched|Fetching|Called|Calling|Created|Creating|Did|Doing) (for )?\d`

// 畫面上縮兩格、但不是秘書說的話：工具的狀態與結果。
var draftStatusLine = regexp.MustCompile(`^  (⎿|… |` + draftStatusWords + `)`)

// 「⏺ Bash(…)」（舊版畫面）、「⏺ Running 1 shell command…」（新版）是工具，不是話。
var draftToolCall = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_.:-]*\(|` + draftStatusWords + `)`)

// 轉圈那行（「✽ Ideating… (3s · ↓ 20 tokens)」；圓點閃爍時可能只剩縮排）：有它才是秘書正在忙。
// 做完的那行（「✻ Crunched for 17s」）沒有「…」。
var draftSpinner = regexp.MustCompile(`^(\S |  )?[A-Z][a-z]+…`)

// 畫面上跟話擠在同一行、但不是話的字（WEB-171 雜訊）：話後面的「 · summary」標記，
// 和隔一大段空白靠右的狀態字（「No changes this session」「+50 files edited before this session (show)」）。
// 只切認得的狀態字：表格、對齊的程式碼也會有一大段空白，不能切。「 · summary」剛好折到下一行時切不掉（少見，接受）。
var draftTrailer = regexp.MustCompile(`\S\s{3,}(\S.*(this session|\(show\)))\s*$`)

func cleanDraftLine(line string) string {
	if loc := draftTrailer.FindStringIndex(line); loc != nil {
		_, size := utf8.DecodeRuneInString(line[loc[0]:])
		line = line[:loc[0]+size]
	}
	return strings.TrimSuffix(strings.TrimRight(line, " "), " · summary")
}

// 整行靠右的狀態字：前面空了一大段。
var draftRightAligned = regexp.MustCompile(`^\s{20,}\S`)

// draftFromScreen 從畫面（tmux capture-pane 的純文字）找最後一個主人輸入（❯ 開頭）之後、最後一段秘書說的話；
// 沒有、或秘書沒在忙（沒有轉圈那行：做完、被中斷、等主人回答）就回空字串。輸入框（最後一條分隔線以下）不算。新版畫面的工具標題（「⏺ Adding … to the journal」）
// 看起來跟話一樣，靠下一行接著「⎿」認出來。
func draftFromScreen(screen string) string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r", ""), "\n")
	end := len(lines)
	// 輸入框上面那條分隔線：從下往上找第二條（輸入框上下各一條）。
	seps := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "────") {
			seps++
			if seps == 2 {
				end = i
				break
			}
		}
	}
	lines = lines[:end]
	start := 0
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "❯ ") {
			start = i + 1
			break
		}
	}
	busy := false
	for _, line := range lines[start:] {
		if draftSpinner.MatchString(line) {
			busy = true
		}
	}
	if !busy {
		return ""
	}
	var last, cur []string // last：上一段確定是話的；cur：正在看的這段（nil＝不在話裡）
	keep := func() {
		if cur != nil {
			last = cur
		}
		cur = nil
	}
	for _, line := range lines[start:] {
		if draftRightAligned.MatchString(line) {
			continue // 靠右的狀態字（「+50 files edited before this session (show)」）：不是話，也不打斷這段
		}
		line = cleanDraftLine(line)
		switch {
		case strings.HasPrefix(line, draftBullet):
			keep()
			rest := strings.TrimRight(strings.TrimPrefix(line, draftBullet), " ")
			if !draftToolCall.MatchString(rest) {
				cur = []string{rest}
			}
		case cur == nil:
		case strings.HasPrefix(line, "  ⎿"):
			cur = nil // 圓點那段下面直接接結果（不管折了幾行）：是工具標題
		case strings.TrimSpace(line) == "":
			cur = append(cur, "")
		case strings.HasPrefix(line, "  ") && !draftStatusLine.MatchString(line):
			cur = append(cur, strings.TrimRight(strings.TrimPrefix(line, "  "), " "))
		default:
			// 工具狀態、轉圈、其他第一格開頭的行：這段話到這裡結束。
			keep()
		}
	}
	keep()
	return joinWrapped(last)
}

// joinWrapped 把畫面折行接回來：空行是分段、清單項目各自一行；其他折行兩邊都是中文或碰到全形標點就直接黏，否則補一個空白。
func joinWrapped(lines []string) string {
	var b strings.Builder
	prevBlank := true
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
			if !prevBlank {
				b.WriteString("\n\n")
			}
			prevBlank = true
			continue
		case prevBlank:
		case draftListItem.MatchString(t):
			b.WriteByte('\n')
		case needsSpace(b.String(), t):
			b.WriteByte(' ')
		}
		b.WriteString(t)
		prevBlank = false
	}
	return strings.TrimSpace(b.String())
}

var draftListItem = regexp.MustCompile(`^([-*•]|\d+[.)]) `)

func needsSpace(before, next string) bool {
	r := []rune(strings.TrimRight(before, " "))
	n := []rune(next)
	if len(r) == 0 || len(n) == 0 {
		return false
	}
	a, z := r[len(r)-1], n[0]
	if isWidePunct(a) || isWidePunct(z) {
		return false
	}
	return !(isWide(a) && isWide(z))
}

func isWidePunct(r rune) bool { return (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF) }

func isWide(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || (r >= 0x3000 && r <= 0x303F) || (r >= 0xFF00 && r <= 0xFFEF)
}

// draftKey 只留字母、數字與漢字，用來比對畫面上的話跟記錄裡的是不是同一段（畫面會拿掉 markdown 符號、重新折行）。
func draftKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// draftCovered：這段話已經在記錄裡（是這一輪某則秘書文字的一部分）。
func draftCovered(draft string, said []string) bool {
	k := draftKey(draft)
	if k == "" {
		return true
	}
	for _, s := range said {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}
