package charter

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// 文件與「一段」（D-44）：section 是 Markdown 標題（# 到 ######，不在 ``` 程式碼區塊裡）的文字，
// 那一段從標題下一行到下一個同級或更高級的標題為止。讀的時候去掉頭尾空行；改的時候只換內容、標題留著。

// 標題：最多 3 個空白縮排、1～6 個 #、空白、文字；結尾的「空白＋一串 #」是收尾記號，不算文字（「## C#」的文字是「C#」）。
var (
	headingPattern = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*$`)
	closingHashes  = regexp.MustCompile(`(?:^|[ \t]+)#+$`)
)

// heading 回這一行的標題級數與文字；不是標題回 0。
func heading(line string) (int, string) {
	m := headingPattern.FindStringSubmatch(line)
	if m == nil {
		return 0, ""
	}
	return len(m[1]), strings.TrimSpace(closingHashes.ReplaceAllString(m[2], ""))
}

// fence 追蹤 ``` 與 ~~~ 程式碼區塊（CommonMark）：開頭是 3 個以上同一種符號（最多 3 個空白縮排），
// 收尾要同一種符號、至少一樣長、後面只能有空白；``` 的說明文字不能有 `。
type fence struct {
	char byte
	n    int
}

func (f *fence) open() bool { return f.n > 0 }

// step 讀一行：在程式碼區塊裡或這行是區塊的開頭／收尾就回 true（這行不能當標題）。
func (f *fence) step(line string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return f.open()
	}
	if f.open() {
		run := len(trimmed) - len(strings.TrimLeft(trimmed, string(f.char)))
		if run >= f.n && strings.TrimSpace(trimmed[run:]) == "" {
			f.n = 0
		}
		return true
	}
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return false
	}
	c := trimmed[0]
	run := len(trimmed) - len(strings.TrimLeft(trimmed, string(c)))
	if run < 3 || (c == '`' && strings.ContainsRune(trimmed[run:], '`')) {
		return false
	}
	f.char, f.n = c, run
	return true
}

// normalize：換行統一成 \n、去掉結尾的空行（跟 vault 存內文的方式一樣，版本比較才不會因為結尾換行而不同）。
func normalize(s string) string {
	return strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// trimSection：一段的內容去掉頭尾空行。
func trimSection(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
}

// revisionOf 是內容的短雜湊；文件不存在時是空字串。
func revisionOf(text string, exists bool) string {
	if !exists {
		return ""
	}
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])[:16]
}

type span struct {
	found              bool
	level              int // 標題的級數（## 是 2）
	bodyStart, bodyEnd int // 這一段內容在檔案文字裡的位置（bodyStart 是標題那行的下一行開頭）
}

// findSection 找標題文字等於 name 的第一段。file 已經換成 \n。
func findSection(file, name string) span {
	var s span
	var f fence
	pos := 0
	for pos <= len(file) {
		end := strings.IndexByte(file[pos:], '\n')
		next := len(file) + 1
		line := file[pos:]
		if end >= 0 {
			line = file[pos : pos+end]
			next = pos + end + 1
		}
		if !f.step(line) {
			if l, text := heading(line); l > 0 {
				if s.found {
					if l <= s.level {
						s.bodyEnd = pos
						return s
					}
				} else if text == name {
					s.found, s.level = true, l
					s.bodyStart = min(next, len(file))
				}
			}
		}
		pos = next
	}
	if s.found {
		s.bodyEnd = len(file)
	}
	return s
}

// checkBody 檢查要放進一段（標題級數 level；0＝整個檔）的新內容不會弄亂段落邊界：
// 不能有同級或更高級的標題（會把後面的內容切到段外），程式碼區塊要收尾（不然後面的標題都會被當成程式碼）。
func checkBody(text string, level int) string {
	var f fence
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if f.step(line) {
			continue
		}
		if l, h := heading(line); l > 0 && l <= level {
			return fmt.Sprintf("這一段的標題是 %s 級，內容裡不能有同級或更高級的標題（「%s %s」）；要加新的一段請改整份文件", strings.Repeat("#", level), strings.Repeat("#", l), h)
		}
	}
	if f.open() {
		return "內容裡有沒收尾的程式碼區塊（" + strings.Repeat(string(f.char), f.n) + "），請補上收尾那一行"
	}
	return ""
}

// docText 從檔案內容取出這份文件的文字；section 不在時 found=false。
func docText(file, section string) (string, bool) {
	file = strings.ReplaceAll(file, "\r\n", "\n")
	if section == "" {
		return normalize(file), true
	}
	s := findSection(file, section)
	if !s.found {
		return "", false
	}
	return trimSection(file[s.bodyStart:s.bodyEnd]), true
}

// withDoc 回把這份文件換成 text 之後的整個檔案內容（file 不存在時傳空字串）。
// 整個檔：直接換掉。一段：只換那一段的內容，標題與其他段都不動；那一段不在就加在檔尾（## 標題）。
func withDoc(file, section, text string) string {
	file = strings.ReplaceAll(file, "\r\n", "\n")
	if section == "" {
		if body := normalize(text); body != "" {
			return body + "\n"
		}
		return ""
	}
	body := trimSection(text)
	s := findSection(file, section)
	if !s.found {
		head := normalize(file)
		if head != "" {
			head += "\n\n"
		}
		out := head + "## " + section + "\n"
		if body != "" {
			out += "\n" + body + "\n"
		}
		return out
	}
	head := file[:s.bodyStart]
	if !strings.HasSuffix(head, "\n") {
		head += "\n"
	}
	tail := file[s.bodyEnd:]
	out := head
	if body != "" {
		out += "\n" + body + "\n"
	}
	if tail != "" {
		out += "\n" + tail
	}
	return out
}

// cleanText 是存進文件與版本的樣子（比較「有沒有改」也用它）。
func cleanText(section, text string) string {
	if section == "" {
		return normalize(text)
	}
	return trimSection(text)
}
