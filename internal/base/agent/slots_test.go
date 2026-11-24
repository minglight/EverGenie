package agent

import (
	"context"
	"testing"
)

// Guards: REQ-22, REQ-46, REQ-160
func TestRenderJobSlots(t *testing.T) {
	addendum := "## 我的規則\n先講結論。\n\n## slot: chat-priorities\n   - 優先聊健身與日本旅遊。\n\n## slot: chat-avoid\n   - 不聊股票。\n## 其他\n不相干"
	slots := ParseSlots(addendum)
	if slots["chat-priorities"] != "   - 優先聊健身與日本旅遊。" || slots["chat-avoid"] != "   - 不聊股票。" || len(slots) != 2 {
		t.Fatalf("ParseSlots：%q", slots)
	}
	body := "挑話題：\n{{slot chat-priorities}}\n   - 預設：可以馬上用上的。\n{{/slot}}\n{{slot chat-avoid}}\n   - 預設：不聊工作。\n{{/slot}}\n{{slot ai-topics-focus}}\n   - 預設：跟工作有關的。\n{{/slot}}"
	want := "挑話題：\n   - 優先聊健身與日本旅遊。\n   - 不聊股票。\n   - 預設：跟工作有關的。"
	if got := RenderSlots(body, slots); got != want {
		t.Fatalf("RenderSlots：\n%s\n要：\n%s", got, want)
	}

	m, _, _ := fixture(t)
	ctx := context.Background()
	if got, err := m.RenderJob(ctx, "life", body); err != nil || got != "挑話題：\n   - 預設：可以馬上用上的。\n   - 預設：不聊工作。\n   - 預設：跟工作有關的。" {
		t.Fatalf("沒有 addendum 時用預設內容：%q %v", got, err)
	}
	if err := m.WriteAddendum(ctx, "life", addendum, "owner(web)"); err != nil {
		t.Fatal(err)
	}
	if got, err := m.RenderJob(ctx, "life", body); err != nil || got != want {
		t.Fatalf("RenderJob：%q %v", got, err)
	}
}
