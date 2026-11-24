package agent

import (
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 附加規則與職責的 `## ` 段落（DEV-161）：兼任的職位讀段落（AGT-43）、`customize add --section` 加進段落（agents AGS-8）
// 用同一套切段：`## ` 開一段，`# `／`## ` 結束上一段，程式碼區塊（``` 或 ~~~）裡的 # 不算標題。

// MaxRuleSection 是附加規則段落名稱的長度上限（字數）。
const MaxRuleSection = 40

// h2Span 是一個 `## ` 段落：head 是標題那一行，end 是下一個 `#`／`##` 標題那一行（沒有就是行數）。
type h2Span struct {
	name      string
	head, end int
}

// h2Spans 照順序列出 lines 裡的 `## ` 段落；`# ` 標題結束上一段但自己不算一段。
func h2Spans(lines []string) []h2Span {
	var out []h2Span
	closeLast := func(i int) {
		if n := len(out); n > 0 && out[n-1].end < 0 {
			out[n-1].end = i
		}
	}
	fence := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if isFence(t) {
			fence = !fence
			continue
		}
		switch {
		case fence:
		case isH2(t):
			closeLast(i)
			out = append(out, h2Span{name: strings.TrimSpace(strings.TrimPrefix(t, "##")), head: i, end: -1})
		case isH1(t):
			closeLast(i)
		}
	}
	closeLast(len(lines))
	return out
}

func isFence(t string) bool { return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") }
func isH1(t string) bool    { return t == "#" || strings.HasPrefix(t, "# ") }

// h2Sections 把 Markdown 照 `## 標題` 切成段（標題 → 底下的內文，不含標題行）；第一個標題之前的文字不算。同名的段落只留第一個。
func h2Sections(text string) map[string]string {
	lines := strings.Split(text, "\n")
	out := map[string]string{}
	for _, s := range h2Spans(lines) {
		if _, seen := out[s.name]; s.name != "" && !seen {
			out[s.name] = strings.Join(lines[s.head+1:s.end], "\n")
		}
	}
	return out
}

// CheckAddendumRule 檢查要加進附加規則的一條（customize add）：規則裡的程式碼區塊要成對（不然後面的標題都會被當成區塊內容）；
// 有段落名稱時，名稱要一行、不以 # 開頭、最多 MaxRuleSection 字，規則在程式碼區塊外不能有 `#`／`##` 標題行（會把那一段切開）。
func CheckAddendumRule(section, rule string) error {
	fence, heading := false, false
	for _, l := range strings.Split(rule, "\n") {
		t := strings.TrimSpace(l)
		if isFence(t) {
			fence = !fence
		} else if !fence && (isH2(t) || isH1(t)) {
			heading = true
		}
	}
	if fence {
		return platform.Invalid("rule", "規則裡的程式碼區塊（``` 或 ~~~）要成對")
	}
	if section == "" {
		return nil
	}
	if strings.ContainsAny(section, "\r\n") || strings.HasPrefix(section, "#") || len([]rune(section)) > MaxRuleSection {
		return platform.Invalid("section", "段落名稱要一行、不以 # 開頭、最多 40 字（例：聊到什麼先看什麼）")
	}
	if heading {
		return platform.Invalid("rule", "加進段落的規則不能有 # 或 ## 開頭的標題行（會把那一段切開）；要分小項用 ### 或 -")
	}
	return nil
}

// AddToAddendum 把一條附加規則 entry 加進附加規則內文 body，回傳新的內文（以換行結尾；條目之間空一行）：
//   - section 空＝一般規則：接在第一個 `## ` 段落之前（接在尾端會被算進最後一段，例如兼任時被當成「聊到什麼先看什麼」）；沒有段落就接尾端。
//   - section 有值：接在第一個同名段落的最後（段尾空行與下一個標題之前），沒有那一段就在尾端開一段。
func AddToAddendum(body, section, entry string) string {
	body = strings.TrimRight(body, "\n")
	lines := strings.Split(body, "\n")
	spans := h2Spans(lines)
	if section == "" {
		if len(spans) == 0 {
			return joinEntry(body, entry)
		}
		return insertBefore(lines, spans[0].head, 0, entry)
	}
	for _, s := range spans {
		if s.name == section {
			return insertBefore(lines, s.end, s.head+1, entry)
		}
	}
	return joinEntry(body, "## "+section+"\n\n"+entry)
}

// joinEntry 把 entry 接在 body 尾端，中間空一行。
func joinEntry(body, entry string) string {
	if body != "" {
		body += "\n\n"
	}
	return body + entry + "\n"
}

// insertBefore 把 entry 插在第 at 行之前：先往回跳過空行（不跳過 floor 之前），前後各空一行。
func insertBefore(lines []string, at, floor int, entry string) string {
	last := at
	for last > floor && strings.TrimSpace(lines[last-1]) == "" {
		last--
	}
	out := append([]string{}, lines[:last]...)
	if last > 0 {
		out = append(out, "")
	}
	out = append(out, entry)
	if at < len(lines) {
		out = append(append(out, ""), lines[at:]...)
	}
	return strings.Join(out, "\n") + "\n"
}
