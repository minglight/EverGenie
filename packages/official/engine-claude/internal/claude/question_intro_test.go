package claude

import (
	"strings"
	"testing"
)

// 確認卡要帶「問之前她說的那段話」：取這一輪最後一段她的話；上一輪的不算；太長截斷。
func TestQuestionIntroTakesThisTurnsLastAssistantText(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-02T01:00:00Z","message":{"content":"上一輪的問題"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-02T01:00:01Z","message":{"content":[{"type":"text","text":"上一輪的回答"}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-11-02T01:01:00Z","message":{"content":"做一個新角色"}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-11-02T01:01:01Z","message":{"content":[{"type":"text","text":"我先看看現有的"}]}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-02T01:01:02Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"ls"}}]}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-11-02T01:01:03Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"a b"}]}}`,
		`{"type":"assistant","uuid":"a4","timestamp":"2026-11-02T01:01:04Z","message":{"content":[{"type":"text","text":"希臘神話裡有三位已經在用，我推薦瑟西。"},{"type":"tool_use","id":"t2","name":"AskUserQuestion","input":{}}]}}`,
	}
	got := questionIntro([]byte(strings.Join(lines, "\n") + "\n"))
	if got != "希臘神話裡有三位已經在用，我推薦瑟西。" {
		t.Fatalf("要這一輪最後一段她的話：%q", got)
	}
	// 這一輪還沒說話就問：空的（不拿上一輪的）。
	if got := questionIntro([]byte(strings.Join(lines[:3], "\n") + "\n")); got != "" {
		t.Fatalf("這一輪沒說話要空的：%q", got)
	}
	long := `{"type":"user","uuid":"u","timestamp":"2026-11-02T01:00:00Z","message":{"content":"問"}}` + "\n" +
		`{"type":"assistant","uuid":"a","timestamp":"2026-11-02T01:00:01Z","message":{"content":[{"type":"text","text":"` + strings.Repeat("字", 700) + `"}]}}` + "\n"
	if got := []rune(questionIntro([]byte(long))); len(got) != questionIntroMax+1 || string(got[len(got)-1]) != "…" {
		t.Fatalf("太長要截斷：%d 字", len(got))
	}
	if questionIntro(nil) != "" {
		t.Fatal("沒有對話記錄要空的")
	}
}

// 新版 Claude Code 把她問問題前說的話存成 thinking 區塊（thinkingDisplay = "updates"，畫面標 · summary），不寫 text 區塊
// （作者 2026-11-03：看板的問題卡漏掉最後一段，只顯示更早的 text）。要拿到這一輪最後一段旁白；其他模式的思考、空的思考不算。
func TestQuestionIntroTakesNarrationThinkingBlocks(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-03T01:00:00Z","message":{"content":"確認一下"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-03T01:00:01Z","message":{"content":[{"type":"text","text":"我來確認，只讀不動。"}]}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-11-03T01:00:02Z","message":{"content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-11-03T01:00:03Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"ok"}]}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-03T01:00:04Z","thinkingDisplay":"updates","message":{"content":[{"type":"thinking","thinking":"","signature":"x"}]}}`,
		`{"type":"assistant","uuid":"a4","timestamp":"2026-11-03T01:00:05Z","thinkingDisplay":"updates","message":{"content":[{"type":"thinking","thinking":"沒有程式在聽，所以可以直接退役。\n\n","signature":"x"}]}}`,
		`{"type":"assistant","uuid":"a5","timestamp":"2026-11-03T01:00:06Z","message":{"content":[{"type":"tool_use","id":"t2","name":"AskUserQuestion","input":{}}]}}`,
	}
	got := questionIntro([]byte(strings.Join(lines, "\n") + "\n"))
	if got != "沒有程式在聽，所以可以直接退役。" {
		t.Fatalf("要取到最後一段旁白（thinking／updates），不是更早的 text：%q", got)
	}
	// 不是 updates 模式的 thinking 是她自己的思考，不能當成說給主人的話。
	other := strings.ReplaceAll(strings.Join(lines, "\n"), `"thinkingDisplay":"updates","message":{"content":[{"type":"thinking","thinking":"沒有`, `"thinkingDisplay":"summarized","message":{"content":[{"type":"thinking","thinking":"沒有`)
	if got := questionIntro([]byte(other + "\n")); got != "我來確認，只讀不動。" {
		t.Fatalf("非 updates 的 thinking 不算旁白：%q", got)
	}
}

// 連問兩題、第二題前沒有新的話：第二題的卡片不能掛第一題的開場白（DEV-153 新家第一天實測）；第二題前有新的話就用新的。
func TestQuestionIntroResetsAfterAnsweredQuestion(t *testing.T) {
	lines := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-20T06:00:00Z","message":{"content":"想養成早睡的習慣"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-20T06:00:01Z","message":{"content":[{"type":"text","text":"先讓我知道你現在的起點。"},{"type":"tool_use","id":"q1","name":"AskUserQuestion","input":{}}]}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-11-20T06:00:02Z","message":{"content":[{"type":"tool_result","tool_use_id":"q1","content":"Your questions have been answered"}]}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-11-20T06:00:03Z","message":{"content":[{"type":"tool_use","id":"q2","name":"AskUserQuestion","input":{}}]}}`,
	}
	if got := questionIntro([]byte(strings.Join(lines, "\n") + "\n")); got != "" {
		t.Fatalf("上一題答完後沒說新的話，不能沿用上一題的開場白：%q", got)
	}
	withNew := append(append([]string{}, lines[:3]...),
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-20T06:00:03Z","message":{"content":[{"type":"text","text":"接著找一個每晚固定會做的事。"},{"type":"tool_use","id":"q2","name":"AskUserQuestion","input":{}}]}}`)
	if got := questionIntro([]byte(strings.Join(withNew, "\n") + "\n")); got != "接著找一個每晚固定會做的事。" {
		t.Fatalf("第二題前說的新話要帶上：%q", got)
	}
	// 一般工具的結果不算新的一段（照舊）。
	if got := questionIntro([]byte(strings.Join(lines[:2], "\n") + "\n")); got != "先讓我知道你現在的起點。" {
		t.Fatalf("第一題照舊帶開場白：%q", got)
	}
}
