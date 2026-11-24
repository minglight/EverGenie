package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

const knowledgeDuties = `# life 的職責

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

## 回答時怎麼用
- 先翻主題。`

// 可選知識的區塊從職責文字解析出來：id、標題、預設、一句說明；基本知識的 ### 不算。
func TestParseKnowledgeBlocks(t *testing.T) {
	got := ParseKnowledge(knowledgeDuties)
	want := []KnowledgeBlock{
		{ID: "parenting", Title: "育兒", Summary: "依孩子年齡給建議。", Default: false},
		{ID: "balance", Title: "生活管理", Summary: "留意工作有沒有吃掉生活。", Default: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("解析：%+v", got)
	}
	if got := ParseKnowledge("## 基本知識\n### 陪伴 {#x}\n> 預設：勾\n"); len(got) != 0 {
		t.Fatalf("不在「可選知識」那一節的不算：%+v", got)
	}
}

// 開場只帶勾了的可選知識：標題留著（拿掉 {#id}）、預設與說明兩行拿掉；全部沒勾整節拿掉；其他段不動。
func TestKeepKnowledgeOnlyEnabled(t *testing.T) {
	out := KeepKnowledge(knowledgeDuties, map[string]bool{"balance": true})
	for _, s := range []string{"## 可選知識", "### 生活管理\n- 有跡象提醒一句。", "## 回答時怎麼用\n- 先翻主題。", "### 陪伴"} {
		if !strings.Contains(out, s) {
			t.Fatalf("少了 %q：\n%s", s, out)
		}
	}
	for _, s := range []string{"育兒", "每個孩子一份主題", "預設：", "{#", "留意工作有沒有吃掉生活"} {
		if strings.Contains(out, s) {
			t.Fatalf("不該有 %q：\n%s", s, out)
		}
	}
	none := KeepKnowledge(knowledgeDuties, map[string]bool{})
	if strings.Contains(none, "可選知識") || strings.Contains(none, "生活管理") || !strings.Contains(none, "## 基本知識") || !strings.Contains(none, "## 回答時怎麼用") {
		t.Fatalf("全部沒勾要整節拿掉：\n%s", none)
	}
	if plain := "## 她是誰\n普通職責\n"; KeepKnowledge(plain, nil) != plain {
		t.Fatal("沒有可選知識的職責要原樣回")
	}
}

// 名冊紀錄沒有 knowledge 欄位照預設；有欄位照主人勾的（空清單＝全部不勾）；開場（DutiesOf／Instructions）跟著變。
func TestDutiesOfKnowledgeChoice(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	m.SetOfficialDuties(func(ref string) (string, bool) { return knowledgeDuties, ref == "secretaries/life" })
	_, err := m.PatchAgent(ctx, "life", map[string]any{"duties_from": "secretaries/life"}, nil, "system")
	must(t, err)

	text, err := m.Instructions(ctx, "life", "life")
	must(t, err)
	if !strings.Contains(text, "### 生活管理") || strings.Contains(text, "育兒") {
		t.Fatalf("沒有欄位要照預設（balance 勾、parenting 不勾）：\n%s", text)
	}
	r, err := m.Record(ctx, "agent", "life")
	must(t, err)
	blocks := m.KnowledgeOf(r)
	if len(blocks) != 2 || blocks[0].Enabled || !blocks[1].Enabled || blocks[0].Summary == "" {
		t.Fatalf("KnowledgeOf 照預設：%+v", blocks)
	}

	_, err = m.PatchAgent(ctx, "life", map[string]any{"knowledge": []string{"parenting"}}, nil, "owner(cli)")
	must(t, err)
	text, err = m.Instructions(ctx, "life", "life")
	must(t, err)
	if !strings.Contains(text, "### 育兒") || strings.Contains(text, "生活管理") {
		t.Fatalf("勾了才出現：\n%s", text)
	}

	_, err = m.PatchAgent(ctx, "life", map[string]any{"knowledge": []string{}}, nil, "owner(cli)")
	must(t, err)
	r, err = m.Record(ctx, "agent", "life")
	must(t, err)
	if _, ok := KnowledgeChoice(r.Fields); !ok {
		t.Fatalf("空清單要留著（全部不勾，不是回到預設）：%+v", r.Fields)
	}
	if d := m.DutiesOf(r); strings.Contains(d, "可選知識") {
		t.Fatalf("全部不勾整節拿掉：\n%s", d)
	}
}

// 主人看過的塊（knowledge_seen）照他勾的；沒看過的（官方之後新加的）照預設；沒有 knowledge_seen 的舊紀錄當成都看過。
func TestEnabledKnowledgeNewBlocksUseDefault(t *testing.T) {
	blocks := ParseKnowledge(knowledgeDuties + "\n\n## 可選知識\n### 新的 {#fresh}\n> 預設：勾\n> 新加的。\n")
	if len(blocks) != 3 {
		t.Fatalf("解析：%+v", blocks)
	}
	got := EnabledKnowledge(blocks, map[string]any{"knowledge": []string{}, "knowledge_seen": []string{"parenting", "balance"}})
	if got["parenting"] || got["balance"] || !got["fresh"] {
		t.Fatalf("看過的照勾的、沒看過的照預設：%v", got)
	}
	got = EnabledKnowledge(blocks, map[string]any{"knowledge": []string{"parenting"}})
	if !got["parenting"] || got["balance"] || got["fresh"] {
		t.Fatalf("沒有 knowledge_seen 當成都看過：%v", got)
	}
}
