package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
)

// 兼任測試用的 life 職責：SCR-5 的每一段都有，可選知識兩塊（只勾 balance）。
const sideDuties = `# life 的職責

## 她是誰
生活教練。

## 基本知識
### 陪伴
- 先聽。

## 可選知識

### 育兒 {#parenting}
> 預設：不勾
> 依孩子年齡給建議。
- 每個孩子一份主題。

### 生活管理 {#balance}
> 預設：勾
> 留意工作有沒有吃掉生活。
- 有跡象提醒一句。

## 聊到什麼先看什麼
- **提到某個人**：先看那個人的主題。
` + TopicCuesClosing + `

## 回答時怎麼用
- 建議要具體。

## 你主動照顧的事
- 提醒與待辦。

## 第一天上班
- 用聊的認識他。

## 這個職位特有的界線
- 不替他做大決定。`

func sideFixture(t *testing.T) (*Module, context.Context) {
	m, _, ctx := sideFixtureHome(t)
	return m, ctx
}

func sideFixtureHome(t *testing.T) (*Module, *homefixture.Home, context.Context) {
	t.Helper()
	m, h, _ := fixture(t)
	ctx := context.Background()
	must(t, m.SetBody(ctx, "agent", "life", sideDuties, "system"))
	_, err := m.PatchAgent(ctx, "life", map[string]any{"knowledge": []string{"balance"}}, nil, "system")
	must(t, err)
	must(t, m.WriteAddendum(ctx, "life", "<!-- customize add 2026-11-16 by life -->\n總規則。\n\n## 聊到什麼先看什麼\n\n<!-- customize add 2026-11-16 by life -->\n聊到跑步先看鞋子的主題。\n\n## 別的小節\n不該帶。", "life"))
	must(t, m.WriteMemory(ctx, "life", "life 的記憶", "life"))
	return m, h, ctx
}

// 同一位角色兼任的職位：開場帶那個職位的她是誰、基本知識、勾了的可選知識、聊到什麼先看什麼（含附加規則同名一段）、回答時怎麼用；
// 不帶你主動照顧的事、第一天上班、界線、記憶與附加規則的其他段。位置在附加規則之後、共用規則之前。
func TestInstructionsBringSidePositionKnowledge(t *testing.T) {
	m, ctx := sideFixture(t)
	must(t, m.WriteAddendum(ctx, "code", "code 自己的附加", "code"))
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	side := sectionOf(t, got, "兼任的職位")
	for _, want := range []string{
		"### life\n\n你同時擔任 life（測試職責），下面是那個職位的知識，回答時一起用；那個職位的記憶與日常照顧仍在它自己的對話。" +
			"下面提到那個職位的記憶或沒帶過來的段落時，用 `evergenie memory show life`、`evergenie customize show life` 看。",
		"#### 她是誰\n\n生活教練。",
		"#### 基本知識\n\n##### 陪伴\n- 先聽。",
		"#### 可選知識\n\n##### 生活管理\n- 有跡象提醒一句。",
		"#### 聊到什麼先看什麼\n\n- **提到某個人**：先看那個人的主題。\n\n主人加的（跟上面衝突時照這幾條）：\n<!-- customize add 2026-11-16 by life -->\n聊到跑步先看鞋子的主題。",
		"#### 回答時怎麼用\n\n- 建議要具體。",
	} {
		if !strings.Contains(side, want) {
			t.Fatalf("兼任的職位少了 %q：\n%s", want, side)
		}
	}
	for _, not := range []string{"育兒", "預設：", "你主動照顧的事", "提醒與待辦", "第一天上班", "界線", "life 的記憶", "總規則", "別的小節", "不該帶", "# life 的職責", TopicCuesClosing} {
		if strings.Contains(side, not) {
			t.Fatalf("兼任的職位不該有 %q：\n%s", not, side)
		}
	}
	addendum, shared := strings.Index(got, "## 附加規則\n\ncode 自己的附加"), strings.Index(got, "## 共用規則")
	if at := strings.Index(got, "## 兼任的職位"); addendum < 0 || !(addendum < at && at < shared) {
		t.Fatalf("兼任的職位要在附加規則之後、共用規則之前：%d %d %d", addendum, at, shared)
	}
	// 反過來 life 的開場也帶 code，但 code 的職責沒照 SCR-5 寫（只有「官方職責」一行），帶不出東西就不出這一節。
	life, err := m.Instructions(ctx, "life", "life")
	must(t, err)
	if strings.Contains(life, "## 兼任的職位") {
		t.Fatalf("沒照 SCR-5 寫的職責不帶：\n%s", life)
	}
}

// 停用的職位不算兼任。
func TestInstructionsSkipDisabledSidePosition(t *testing.T) {
	m, ctx := sideFixture(t)
	_, err := m.PatchAgent(ctx, "life", map[string]any{"enabled": false}, nil, "system")
	must(t, err)
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if strings.Contains(got, "## 兼任的職位") || strings.Contains(got, "生活教練") {
		t.Fatalf("停用的職位不帶：\n%s", got)
	}
}

// 不同角色擔任的職位不帶。
func TestInstructionsSkipOtherPersonaPosition(t *testing.T) {
	m, ctx := sideFixture(t)
	_, err := m.PatchAgent(ctx, "life", map[string]any{"persona": "new"}, nil, "system")
	must(t, err)
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	if strings.Contains(got, "## 兼任的職位") || strings.Contains(got, "生活教練") {
		t.Fatalf("別的角色的職位不帶：\n%s", got)
	}
}

// 兼任好幾個職位時照名冊順序各一段；測口吻時照座位原本坐的角色算（換了被測的卡也照樣帶）。
func TestInstructionsSideOrderAndTrial(t *testing.T) {
	m, h, ctx := sideFixtureHome(t)
	for _, id := range []string{"art", "work"} {
		h.WithRecords(vault.Record{Kind: "agent", ID: id, Fields: map[string]any{"duty": id + " 職責", "persona": "athena"}, Body: "## 她是誰\n" + id + " 的她是誰。"})
	}
	got, err := m.Instructions(ctx, "code", "code")
	must(t, err)
	side := sectionOf(t, got, "兼任的職位")
	art, life, work := strings.Index(side, "### art\n"), strings.Index(side, "### life\n"), strings.Index(side, "### work\n")
	if art < 0 || !(art < life && life < work) {
		t.Fatalf("照名冊順序：%d %d %d\n%s", art, life, work, side)
	}
	trial, err := m.TestInstructions(ctx, "code", "new", "", "", nil)
	must(t, err)
	if sectionOf(t, trial, "兼任的職位") != side {
		t.Fatalf("測口吻照座位原本的角色算兼任：\n%s", trial)
	}
}

// 程式碼區塊裡的 # 不當標題：不切段、不降級。
func TestSideKnowledgeIgnoresHeadingsInCode(t *testing.T) {
	duties := "## 基本知識\n```\n## 不是標題\n```\n#標籤 不是標題\n## 你主動照顧的事\n- 不帶"
	got := sideKnowledge(duties, "")
	want := "#### 基本知識\n\n```\n## 不是標題\n```\n#標籤 不是標題"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// sectionOf 取開場裡 `## <名字>` 那一節（到下一個 `---` 分隔為止）。
func sectionOf(t *testing.T, text, name string) string {
	t.Helper()
	_, rest, ok := strings.Cut(text, "## "+name+"\n\n")
	if !ok {
		t.Fatalf("開場沒有「%s」：\n%s", name, text)
	}
	body, _, _ := strings.Cut(rest, "\n\n---\n\n")
	return body
}
