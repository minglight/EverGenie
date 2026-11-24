package charter

import (
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// 程式碼區塊裡的 # 開頭行不算標題。
func TestSectionIgnoresHeadingsInCodeBlocks(t *testing.T) {
	file := "## 法律\n\n```\n## 不是標題\n```\n\n內容\n\n## 下一段\n"
	got, ok := docText(file, "法律")
	if !ok || got != "```\n## 不是標題\n```\n\n內容" {
		t.Fatalf("%q %v", got, ok)
	}
	if _, ok := docText(file, "不是標題"); ok {
		t.Fatal("程式碼區塊裡的不算")
	}
}

// 標題結尾的 # 要前面有空白才是收尾記號；四個 ` 開頭的區塊不會被三個 ` 收掉，``` 後面接說明文字的不是收尾。
func TestHeadingAndFenceFollowMarkdownRules(t *testing.T) {
	if l, h := heading("## C#"); l != 2 || h != "C#" {
		t.Fatalf("## C# → %d %q", l, h)
	}
	if l, h := heading("### 第 1 條 ###"); l != 3 || h != "第 1 條" {
		t.Fatalf("收尾記號 → %d %q", l, h)
	}
	file := "## A\n\n````\n```\n## 還在區塊裡\n```python\n````\n\n## B\n"
	if got, ok := docText(file, "A"); !ok || !strings.Contains(got, "## 還在區塊裡") {
		t.Fatalf("%q %v", got, ok)
	}
	if _, ok := docText(file, "B"); !ok {
		t.Fatal("區塊收尾之後的標題認得")
	}
}

// 改一段只換內容：標題、前後段都留著。
func TestReplaceSectionKeepsHeadingAndOtherSections(t *testing.T) {
	file := "# 標題\n\n## A\n\n舊的\n\n### A-1\n\n細節\n\n## B\n\nB 的內容\n"
	got := withDoc(file, "A", "新的")
	want := "# 標題\n\n## A\n\n新的\n\n## B\n\nB 的內容\n"
	if got != want {
		t.Fatalf("got %q", got)
	}
	if got := withDoc("# 標題\n", "C", "加上"); got != "# 標題\n\n## C\n\n加上\n" {
		t.Fatalf("沒有那一段就加在檔尾：%q", got)
	}
}

// 逐行差異：改一行是「刪一行＋加一行」，其他行一樣。
func TestLineDiffMarksChangedLines(t *testing.T) {
	d := lineDiff("a\nb\nc", "a\nB\nc\nd")
	want := []sdk.RulesDiffLine{{Op: "same", Text: "a"}, {Op: "del", Text: "b"}, {Op: "add", Text: "B"}, {Op: "same", Text: "c"}, {Op: "add", Text: "d"}}
	if len(d) != len(want) {
		t.Fatalf("%+v", d)
	}
	for i := range want {
		if d[i] != want[i] {
			t.Fatalf("第 %d 行 %+v，要 %+v", i, d[i], want[i])
		}
	}
}
