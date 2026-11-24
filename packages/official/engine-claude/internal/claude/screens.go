package claude

import (
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// 畫面辨識只有三個用途（ECL-N3）：啟動時的信任資料夾對話框、送 Enter 前的確認框檢查、
// 逾時後替主人在原生確認框或選擇題上按（ECL-37、ECL-58）。
// 關鍵字來自 testdata/spike/screens；其他畫面（Remote Control 已啟用等）都不算。

// trustYesOptions：信任框第二個選項的寫法。工作資料夾第一次啟動是「Yes, I trust this folder」（trust-folder.txt）；
// 2.1.326 起 --add-dir 帶進沒用過的資料夾另外跳「Adding a new directory」，選項是「Yes, add it」（trust-add-dir.txt）。
// 兩種都是第一項 No, exit、第二項 Yes，答法一樣（↓＋Enter）。
var trustYesOptions = map[string]bool{"Yes, I trust this folder": true, "Yes, add it": true}

// isTrustScreen：啟動時的信任對話框（預設選項是 No, exit）。
// 只認畫面最底下、一行一行長成對話框的樣子：最後幾行裡有「Enter to confirm」那一行，緊接在它上面是 No, exit、Yes 兩項。
// 對話區引用到這些字（秘書在回覆裡說明這個框，底下還有輸入框）不算：以前只要整個畫面有關鍵字就算，
// 秘書引用了「trust this folder」「No, exit」就被當成卡住，主人在看板按了只會把 ↓＋Enter 打進她的輸入框。
// 格式照 2.1.309（trust-folder.txt）與 2.1.326（trust-add-dir.txt）錄的樣本，換版本對不上要重錄。
func isTrustScreen(screen string) bool {
	_, ok := trustCursor(screen)
	return ok
}

// trustCursor：畫面是信任框時回游標停在哪一項（true＝Yes 那一項；主人在終端機按過 ↓ 就會這樣）。
func trustCursor(screen string) (onYes bool, ok bool) {
	var lines []string
	for _, l := range strings.Split(screen, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	n := len(lines)
	// footer 下面可能多一行提示（例如「Press Ctrl-C again to exit」），所以看最後三行。
	for i := n - 1; i >= 2 && i >= n-3; i-- {
		if !strings.HasPrefix(lines[i], "Enter to confirm") {
			continue
		}
		no, yes := lines[i-2], lines[i-1]
		noCur, yesCur := strings.HasPrefix(no, "❯"), strings.HasPrefix(yes, "❯")
		no = strings.TrimSpace(strings.TrimPrefix(no, "❯"))
		yes = strings.TrimSpace(strings.TrimPrefix(yes, "❯"))
		if no != "No, exit" || !trustYesOptions[yes] || noCur == yesCur {
			return false, false
		}
		return yesCur, true
	}
	return false, false
}

// looseTrustScreen：畫面看起來是信任框、但跟 isTrustScreen 認得的格式對不上（平台改了選項的寫法，例如加了編號）：
// 有「No, exit」、有「trust」或「Adding a new directory」、而且沒有輸入框（對話區引用這些字時底下有輸入框）。
// 只用來「不要往裡面貼字」和「讓主人知道」，絕不拿來按鍵。
// 只看畫面最底下那一段（looseTrustTail 行非空行）、而且「No, exit」要自己是一個選項行；畫面是認得的別種框
// （權限確認、選擇題、清單裡的平台選單）就不算——那些框開著時也沒有輸入框，對話區提到信任框的字不能把它們蓋掉。
func looseTrustScreen(screen string) bool {
	if isTrustScreen(screen) || !strings.Contains(screen, "No, exit") {
		return false
	}
	if strings.Contains(screen, "Do you want to proceed?") || findBlocker(screen) != nil {
		return false
	}
	if _, ok := parseQuestionDialog(screen); ok {
		return false
	}
	if _, hasBox := inputBox(screen); hasBox {
		return false
	}
	var tail []string
	for _, l := range strings.Split(screen, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			tail = append(tail, t)
		}
	}
	if len(tail) > looseTrustTail {
		tail = tail[len(tail)-looseTrustTail:]
	}
	option, keyword := false, false
	for _, l := range tail {
		option = option || looseNoExit.MatchString(l)
		keyword = keyword || strings.Contains(strings.ToLower(l), "trust") || strings.Contains(l, "Adding a new directory")
	}
	return option && keyword
}

const looseTrustTail = 14

// looseNoExit：「No, exit」自己一行（前面可以有游標、編號）。
var looseNoExit = regexp.MustCompile(`^(` + dialogCursor + `)?\s*(\d+[.)]\s*)?No, exit$`)

// isConfirmDialog：畫面上有等著人按的確認框。這時送 Enter 可能變成「允許」或「離開」。
func isConfirmDialog(screen string) bool {
	if isTrustScreen(screen) || looseTrustScreen(screen) {
		return true
	}
	return strings.Contains(screen, "Do you want to proceed?") && strings.Contains(screen, "1. Yes")
}

// confirmDialog 是畫面上原生確認框的選項（ECL-37）：options 照畫面上的編號；cursor 是游標（❯）停的那一項（0＝看不到）；
// body 是確認框那一段（從最後一條橫線到選項，去掉游標），按鍵之後拿來判斷還是不是同一個確認框。
type confirmDialog struct {
	options []dialogOption
	cursor  int
	body    string
}

type dialogOption struct {
	n     int
	label string
}

const dialogCursor = "❯"

var optionLine = regexp.MustCompile(`^\s*(` + dialogCursor + `)?\s*(\d+)\.\s+(\S.*?)\s*$`)

// parseConfirmDialog：權限確認框（不含啟動時的信任資料夾）。樣本：testdata/spike/screens/permission-dialog.txt（四選一）、
// hook-ask-dialog.txt（動手前檢查要主人確認的兩選一）。只看「Do you want to proceed?」之後到「Esc to cancel」之間的編號行。
// summaryOnScreen：看板顯示的摘要（指令或檔名的開頭）出現在確認框裡（去掉空白比，畫面會折行）。沒有摘要就不擋。
func summaryOnScreen(body, summary string) bool {
	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	want := []rune(strip(summary))
	if len(want) == 0 {
		return true
	}
	if len(want) > 24 {
		want = want[:24]
	}
	return strings.Contains(strip(body), string(want))
}

func parseConfirmDialog(screen string) (confirmDialog, bool) {
	if isTrustScreen(screen) || !isConfirmDialog(screen) {
		return confirmDialog{}, false
	}
	lines := strings.Split(screen, "\n")
	q := -1
	for i, l := range lines {
		if strings.Contains(l, "Do you want to proceed?") {
			q = i
		}
	}
	if q < 0 {
		return confirmDialog{}, false
	}
	top := 0
	for i := q; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" && strings.Trim(t, "─") == "" {
			top = i
			break
		}
	}
	d := confirmDialog{}
	end := len(lines)
	for i := q + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.Contains(l, "Esc to cancel") {
			end = i
			break
		}
		m := optionLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		d.options = append(d.options, dialogOption{n: n, label: m[3]})
		if m[1] != "" {
			d.cursor = n
		}
	}
	body := make([]string, 0, end-top)
	for _, l := range lines[top:end] {
		body = append(body, strings.TrimRight(strings.ReplaceAll(l, dialogCursor, " "), " "))
	}
	d.body = strings.Join(body, "\n")
	return d, len(d.options) > 0
}

// questionDialog 是畫面上原生的選擇題（AskUserQuestion，ECL-58）。樣本：testdata/spike/screens/question-dialog.txt、
// question-dialog-sidepane.txt（右邊有她開著的檔案，每行「│」後面是別的東西）。
// 「☐ 小標」那一行開始、到「Enter to select · ↑/↓ to navigate」那一行；選項是編號行（說明在下一行縮排），
// 「N. Type something.」是自己打字、「N. Chat about this」不是答案。tabs＝小標那一行有好幾題（多題一起問，要一題一題切）。
type questionDialog struct {
	options []dialogOption
	cursor  int
	typeN   int
	body    string
	tabs    bool
}

const (
	questionFooter = "Enter to select"
	questionType   = "Type something."
	questionChat   = "Chat about this"
)

// beforeSidePane：一行裡「│」以後是右邊別的窗格（選項行、小標行用）。行首就是「│」的（題目框）不切。
func beforeSidePane(l string) string {
	if i := strings.Index(l, "│"); i > 0 {
		return l[:i]
	}
	return l
}

func parseQuestionDialog(screen string) (questionDialog, bool) {
	lines := strings.Split(screen, "\n")
	end := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if l := beforeSidePane(lines[i]); strings.Contains(l, questionFooter) && strings.Contains(l, "↑/↓") {
			end = i
			break
		}
	}
	if end < 0 {
		return questionDialog{}, false
	}
	// 選項從尾行往上找，只收連續遞減到 1 的編號行（N、N-1…1）：題目或說明裡的「1. …」清單不會被當成選項。
	var opts []dialogOption
	cursor, first := 0, -1
	for i := end - 1; i >= 0; i-- {
		m := optionLine.FindStringSubmatch(beforeSidePane(lines[i]))
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil || (len(opts) > 0 && n != opts[0].n-1) {
			continue
		}
		opts = append([]dialogOption{{n: n, label: strings.TrimSpace(m[3])}}, opts...)
		if m[1] != "" {
			cursor = n
		}
		if n == 1 {
			first = i
			break
		}
	}
	if first < 0 {
		return questionDialog{}, false
	}
	top := -1
	for i := first; i >= 0; i-- {
		if strings.ContainsAny(beforeSidePane(lines[i]), "☐☒✔") {
			top = i
			break
		}
	}
	if top < 0 {
		return questionDialog{}, false
	}
	d := questionDialog{options: opts, cursor: cursor}
	for _, o := range opts {
		if o.label == questionType {
			d.typeN = o.n
		}
	}
	head := beforeSidePane(lines[top])
	d.tabs = strings.Count(head, "☐")+strings.Count(head, "☒")+strings.Count(head, "✔") > 1 || strings.Contains(head, "Submit")
	// body：小標到第一個選項之間（題目），比對看板那一題用。
	body := make([]string, 0, first-top)
	for _, l := range lines[top:first] {
		body = append(body, strings.TrimRight(l, " "))
	}
	d.body = strings.Join(body, "\n")
	return d, true
}

// sameQuestion：兩次抓的畫面選項都一樣（游標不算；題目另外用 questionOnScreen 比，右邊窗格會變不能整段比）。
func sameQuestion(a, b questionDialog) bool {
	if len(a.options) != len(b.options) {
		return false
	}
	for i := range a.options {
		if a.options[i] != b.options[i] {
			return false
		}
	}
	return true
}

// labelOf：編號 n 那一項現在畫面上寫的字（沒有回空字串）。
func (d questionDialog) labelOf(n int) string {
	for _, o := range d.options {
		if o.n == n {
			return o.label
		}
	}
	return ""
}

// pick：答案剛好是哪個選項的標籤（「Type something.」「Chat about this」不算）。找不到回 0。
func (d questionDialog) pick(answer string) int {
	answer = strings.TrimSpace(answer)
	for _, o := range d.options {
		if o.label == questionType || o.label == questionChat {
			continue
		}
		if o.label == answer {
			return o.n
		}
	}
	return 0
}

// questionOnScreen：看板那一題的題目開頭出現在畫面上的選擇題裡（去掉空白與框線比，畫面會折行、右邊可能有別的窗格，只比前 16 個字）。
func questionOnScreen(body, text string) bool {
	strip := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || r == '│' {
				return -1
			}
			return r
		}, s)
	}
	want := []rune(strip(text))
	if len(want) == 0 {
		return false
	}
	if len(want) > 16 {
		want = want[:16]
	}
	return strings.Contains(strip(body), string(want))
}

// choice：允許＝標籤剛好是「Yes」的那一項（不選「Yes, and always allow…」「switch to auto mode」這種會改設定的）；
// 拒絕＝標籤是「No」或以「No,」「No 」開頭的那一項。找不到回 0。
func (d confirmDialog) choice(allow bool) int {
	for _, o := range d.options {
		if allow && o.label == "Yes" {
			return o.n
		}
		if !allow && (o.label == "No" || strings.HasPrefix(o.label, "No,") || strings.HasPrefix(o.label, "No ")) {
			return o.n
		}
	}
	return 0
}

// trustHeaders：信任框第一行的標題，下一行就是問的資料夾（trust-folder.txt、trust-add-dir.txt）。
var trustHeaders = []string{"Accessing workspace:", "Adding a new directory:"}

// trustDirNext：信任框路徑下一行的開頭（2.1.309 trust-folder.txt、2.1.326 trust-add-dir.txt）。
// 路徑太長被折成兩行時，下一行是路徑的後半，對不上這些就當認不出來。
var trustDirNext = []string{"Quick safety check", "This session hasn't"}

// trustDir：信任框問的是哪個資料夾（標題下一個非空行）；認不出來回空字串（之後一律交給主人回答，不自動按）。
// 只在 isTrustScreen 為真時用。半截路徑可能剛好切在列過的資料夾結尾（列 /a/b、實際 /a/bcd），所以這些都當認不出來：
// 下一行不是說明句（被折行）、有省略號（被截斷）、長度碰到框的寬度（最上面那條橫線，可能被切）。
func trustDir(screen string) string {
	var lines []string
	width := 0
	for _, l := range strings.Split(screen, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Trim(t, "─") == "" {
			width = utf8.RuneCountInString(t)
		}
		lines = append(lines, t)
	}
	for i := len(lines) - 3; i >= 0; i-- {
		if !slices.Contains(trustHeaders, lines[i]) {
			continue
		}
		dir, next := lines[i+1], lines[i+2]
		ok := filepath.IsAbs(dir) && !strings.Contains(dir, "…") && !strings.Contains(dir, "...") &&
			(width == 0 || utf8.RuneCountInString(dir) < width-2)
		ok = ok && slices.ContainsFunc(trustDirNext, func(p string) bool { return strings.HasPrefix(next, p) })
		if !ok {
			return ""
		}
		return filepath.Clean(dir)
	}
	return ""
}

// dirListed：dir 是主人設定裡本來就列的資料夾（或在它底下）。listed 要是乾淨的絕對路徑。
func dirListed(dir string, listed []string) bool {
	if dir == "" {
		return false
	}
	for _, l := range listed {
		if dir == l || strings.HasPrefix(dir, l+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
