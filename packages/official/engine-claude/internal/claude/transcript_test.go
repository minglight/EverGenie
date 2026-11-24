package claude

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

var update = flag.Bool("update", false, "重寫 golden 檔")

func parseFixture(t *testing.T, rel string, stats *Stats) []sdk.ChatMessage {
	t.Helper()
	f, err := os.Open(filepath.Join(spikeDir, rel))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []sdk.ChatMessage
	for msg, err := range ParseStats(f, stats) {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, msg)
	}
	return out
}

// Guards: REQ-81
func TestTranscriptGoldenFromSpike(t *testing.T) {
	msgs := parseFixture(t, "transcript.jsonl", nil)
	got, err := json.MarshalIndent(msgs, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	golden := filepath.Join("..", "..", "testdata", "transcript.golden.json")
	if *update {
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("讀不到 golden（第一次請加 -update）：%v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("解析結果跟 golden 不同（確定是預期的就加 -update）：\n%s", got)
	}
	// 五種 kind 裡 fixture 有的都出現。
	kinds := map[sdk.ChatKind]int{}
	for _, m := range msgs {
		kinds[m.Kind]++
	}
	for _, k := range []sdk.ChatKind{sdk.ChatKindUser, sdk.ChatKindAssistant, sdk.ChatKindToolCall, sdk.ChatKindToolResult, sdk.ChatKindInterrupt} {
		if kinds[k] == 0 {
			t.Errorf("fixture 解析後沒有 %s", k)
		}
	}
	// 工具呼叫與結果用同一個 CallID 配對。
	calls := map[string]bool{}
	for _, m := range msgs {
		if m.Kind == sdk.ChatKindToolCall {
			calls[m.CallID] = true
		}
	}
	for _, m := range msgs {
		if m.Kind == sdk.ChatKindToolResult && !calls[m.CallID] {
			t.Errorf("tool_result %s 找不到對應的 tool_call", m.CallID)
		}
	}
	// 另一份舊 fixture 也解得開。
	if len(parseFixture(t, "transcript-bell.jsonl", nil)) == 0 {
		t.Error("transcript-bell 解析不出任何對話")
	}
}

// 對話記錄裡的工具呼叫帶白話（ECL-53）：跟處理中的狀態字同一套（讀檔、跑指令、用 <服務>），查不到的照原名；工具結果不帶。
func TestTranscriptToolCallCarriesPlainLabel(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-03T01:00:00Z","message":{"content":[` +
			`{"type":"tool_use","id":"t1","name":"Read","input":{"file_path":"/x"}},` +
			`{"type":"tool_use","id":"t2","name":"Bash","input":{"command":"ls"}},` +
			`{"type":"tool_use","id":"t3","name":"mcp__jira__jira_get","input":{}},` +
			`{"type":"tool_use","id":"t4","name":"SomethingNew","input":{}}]}}`,
		`{"type":"user","uuid":"u1","timestamp":"2026-11-03T01:00:01Z","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"內容"}]}}`,
	}, "\n")
	var got []string
	for m, err := range Parse(strings.NewReader(input)) {
		if err != nil {
			t.Fatal(err)
		}
		switch m.Kind {
		case sdk.ChatKindToolCall:
			got = append(got, m.Tool+"="+m.ToolLabel)
		case sdk.ChatKindToolResult:
			if m.ToolLabel != "" {
				t.Errorf("工具結果不該帶白話：%+v", m)
			}
		}
	}
	want := []string{"Read=讀檔", "Bash=跑指令", "mcp__jira__jira_get=用 jira", "SomethingNew=SomethingNew"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("工具呼叫的白話 = %v，要 %v", got, want)
	}
}

// Guards: REQ-81
func TestUnknownEntrySkipped(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"brand-new-thing","uuid":"x1","payload":{"a":1}}`,
		`{"type":"brand-new-thing","uuid":"x2"}`,
		`{"type":"another-new-thing"}`,
		`{"type":"mode","mode":"normal"}`,
		`{"type":"user","uuid":"u1","timestamp":"2026-10-24T01:00:00Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-24T01:00:01Z","message":{"content":[{"type":"text","text":"yo"}]}}`,
		`{這不是 json`,
		`{"type":"user","uuid":"u2","message":{"content":42}}`,
	}, "\n")
	var stats Stats
	var msgs []sdk.ChatMessage
	for m, err := range ParseStats(strings.NewReader(input), &stats) {
		if err != nil {
			t.Fatalf("未知與壞行不該報錯：%v", err)
		}
		msgs = append(msgs, m)
	}
	if len(msgs) != 2 || msgs[0].Text != "hi" || msgs[1].Text != "yo" {
		t.Fatalf("msgs = %+v", msgs)
	}
	if stats.Unknown["brand-new-thing"] != 2 || stats.Unknown["another-new-thing"] != 1 || len(stats.Unknown) != 2 {
		t.Errorf("Unknown = %v（認得的非對話類型不該算未知）", stats.Unknown)
	}
	if stats.Malformed != 1 {
		t.Errorf("Malformed = %d", stats.Malformed)
	}
	// 沒有最後換行、提早停止都可以。
	n := 0
	for range Parse(strings.NewReader(`{"type":"user","uuid":"u","message":{"content":"a"}}`)) {
		n++
	}
	if n != 1 {
		t.Errorf("最後一行沒有換行也要解析：%d", n)
	}
	for range Parse(strings.NewReader(input)) {
		break
	}
}

// 她工作到一半時主人送的話，Claude Code 只寫成 queued_command attachment；看板要看得到（ECL-45）。
// 背景工作通知與其他 attachment 不算對話；別的 session 傳話照樣解外框。
func TestMidTurnMessageShownFromQueuedCommand(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-11-02T02:25:24.271Z","content":"/compact"}`,
		`{"type":"attachment","uuid":"a1","timestamp":"2026-11-02T02:25:24.270Z","attachment":{"type":"queued_command","prompt":"好了嗎？","commandMode":"prompt","origin":{"kind":"human"}}}`,
		`{"type":"attachment","uuid":"a2","timestamp":"2026-11-02T02:25:25Z","attachment":{"type":"queued_command","prompt":"<task-notification>x</task-notification>","origin":{"kind":"task-notification"}}}`,
		`{"type":"attachment","uuid":"a3","timestamp":"2026-11-02T02:25:26Z","attachment":{"type":"queued_command","prompt":[{"type":"text","text":"看這張"},{"type":"image","source":{}}],"origin":{"kind":"human"}}}`,
		`{"type":"attachment","uuid":"a4","timestamp":"2026-11-02T02:25:27Z","attachment":{"type":"queued_command","prompt":"<cross-session-message from=\"uds:/x\" from-name=\"work\" from-mode=\"prompting\">\nhello\n</cross-session-message>","origin":{"kind":"peer"}}}`,
		`{"type":"attachment","uuid":"a5","timestamp":"2026-11-02T02:25:28Z","attachment":{"type":"skill_listing","content":"x"}}`,
		// subagent 交回的報告（ECL-51）：不是主人的話，不畫。
		`{"type":"attachment","uuid":"a6","timestamp":"2026-11-02T02:25:29Z","attachment":{"type":"queued_command","prompt":"<agent-message from=\"af82\">\n[Subagent hand-back] report\n</agent-message>","commandMode":"prompt","origin":{"kind":"peer","from":"af82"}}}`,
		`{"type":"user","uuid":"u7","timestamp":"2026-11-02T02:25:30Z","message":{"content":"<agent-message from=\"af82\">\nreport\n</agent-message>"}}`,
	}, "\n")
	var got []sdk.ChatMessage
	for m, err := range Parse(strings.NewReader(in)) {
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind != sdk.ChatKindNotice || m.Text != "…" { // subagent 的報告只是一個「…」（ECL-51）
			got = append(got, m)
		}
	}
	if len(got) != 3 {
		t.Fatalf("要 3 則（主人兩則、傳話一則），拿到 %d：%+v", len(got), got)
	}
	if got[0].Kind != sdk.ChatKindUser || got[0].Text != "好了嗎？" || got[0].Uuid != "a1" || got[0].Ts.IsZero() {
		t.Errorf("中途的話：%+v", got[0])
	}
	if got[1].Text != "看這張" {
		t.Errorf("帶圖的中途話只留文字：%+v", got[1])
	}
	if got[2].FromSession != "work" || got[2].Text != "hello" {
		t.Errorf("中途的傳話要解外框：%+v", got[2])
	}
}

// Guards: REQ-81
func TestTaskNotificationFiltered(t *testing.T) {
	msgs := parseFixture(t, "transcript.jsonl", nil)
	for _, m := range msgs {
		if strings.Contains(m.Text, "<task-notification>") || strings.Contains(m.Text, "<task-id>") {
			t.Fatalf("背景工作通知不該出現在對話裡：%+v", m)
		}
		if strings.Contains(m.Text, "<local-command-") || strings.Contains(m.Text, "<command-name>") {
			t.Fatalf("平台內部的指令記錄不該出現在對話裡：%+v", m)
		}
	}
	// 前後的回合還在。
	var sawStarted, sawFinished bool
	for _, m := range msgs {
		sawStarted = sawStarted || (m.Kind == sdk.ChatKindAssistant && m.Text == "STARTED")
		sawFinished = sawFinished || strings.Contains(m.Text, "finished with exit code 0")
	}
	if !sawStarted || !sawFinished {
		t.Errorf("通知前後的 assistant 回合要留著（started=%v finished=%v）", sawStarted, sawFinished)
	}
}

// Guards: REQ-81, REQ-399
func TestCrossSessionUnwrap(t *testing.T) {
	var found *sdk.ChatMessage
	msgs := parseFixture(t, "transcript.jsonl", nil)
	for i := range msgs {
		if msgs[i].FromSession != "" {
			found = &msgs[i]
		}
	}
	if found == nil {
		t.Fatal("找不到跨 session 的訊息")
	}
	if found.Kind != sdk.ChatKindUser || found.FromSession != "m2-2b" || found.Text != "PING-CROSS from m2" {
		t.Errorf("跨 session 解封裝不對：%+v", found)
	}
	// 只有外框（hook 形式）也要解。
	in := `{"type":"user","uuid":"u","message":{"content":"<cross-session-message from=\"uds:/x\" from-name=\"work\" from-mode=\"prompting\">\nhello\n</cross-session-message>"}}`
	for m := range Parse(strings.NewReader(in)) {
		if m.FromSession != "work" || m.Text != "hello" {
			t.Errorf("外框形式：%+v", m)
		}
	}
	// pasted_content 外框去掉。
	in = `{"type":"user","uuid":"u","message":{"content":"<pasted_content id=\"1\">\nline1\nline2\n</pasted_content>"}}`
	for m := range Parse(strings.NewReader(in)) {
		if m.Text != "line1\nline2" || m.FromSession != "" {
			t.Errorf("pasted_content：%+v", m)
		}
	}
	// 結尾標籤也帶 id（Claude Code 2026-10 起），前後有空行（REQ-399）。
	in = `{"type":"user","uuid":"u","message":{"content":"\n\n<pasted_content id=\"cb09\">\nline1\nline2\n</pasted_content id=\"cb09\">\n"}}`
	for m := range Parse(strings.NewReader(in)) {
		if m.Text != "line1\nline2" {
			t.Errorf("pasted_content 結尾帶 id：%+v", m)
		}
	}
	// 中斷。
	in = `{"type":"user","uuid":"u","message":{"content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]}}`
	for m := range Parse(strings.NewReader(in)) {
		if m.Kind != sdk.ChatKindInterrupt {
			t.Errorf("中斷要是 interrupt：%+v", m)
		}
	}
}

// subagent 交回的報告是系統輸入，不是主人打的字（ECL-51）：看板不畫成主人的話、不算主人最後說話的時間。
func TestAgentMessageIsSystemInput(t *testing.T) {
	in := NormalizeInput("<agent-message from=\"af82\">\n[Subagent hand-back] report\n</agent-message>")
	if in.Source != sdk.InputSourceSystem {
		t.Fatalf("要是 system：%+v", in)
	}
}

// 新版 Claude Code 把她說給主人聽的旁白存成 thinking 區塊（thinkingDisplay = "updates"、有字）；
// 要變成一則助理訊息。空的思考、不是 updates 模式的 thinking 不算（ECL-54）。
func TestNarrationThinkingBecomesAssistantMessage(t *testing.T) {
	in := strings.Join([]string{
		`{"type":"assistant","uuid":"a1","timestamp":"2026-11-03T01:00:01Z","thinkingDisplay":"updates","message":{"content":[{"type":"thinking","thinking":"","signature":"x"}]}}`,
		`{"type":"assistant","uuid":"a2","timestamp":"2026-11-03T01:00:02Z","thinkingDisplay":"updates","message":{"content":[{"type":"thinking","thinking":"我先查一下。\n\n","signature":"x"}]}}`,
		`{"type":"assistant","uuid":"a3","timestamp":"2026-11-03T01:00:03Z","thinkingDisplay":"summarized","message":{"content":[{"type":"thinking","thinking":"自己的思考","signature":"x"}]}}`,
		`{"type":"assistant","uuid":"a4","timestamp":"2026-11-03T01:00:04Z","message":{"content":[{"type":"thinking","thinking":"沒有顯示模式","signature":"x"}]}}`,
		`{"type":"assistant","uuid":"a5","timestamp":"2026-11-03T01:00:05Z","message":{"content":[{"type":"text","text":"弄好了。"}]}}`,
	}, "\n") + "\n"
	var got []string
	for m, err := range Parse(strings.NewReader(in)) {
		if err != nil {
			t.Fatal(err)
		}
		if m.Kind == sdk.ChatKindAssistant {
			got = append(got, m.Text)
		}
	}
	if len(got) != 2 || got[0] != "我先查一下。" || got[1] != "弄好了。" {
		t.Fatalf("只有 updates 有字的 thinking 與 text 變成助理訊息：%q", got)
	}
}

// ECL-59：平台自己加的 isMeta 文字不是主人說的話：工具帶進來的、讀圖、系統提醒不畫，其他變成一行通知；跨 session 傳話照舊。
func TestMetaTextIsNotOwnerSpeech(t *testing.T) {
	entry := func(extra, content string) string {
		b, _ := json.Marshal(content)
		return `{"type":"user","uuid":"u","timestamp":"2026-11-04T01:00:00Z","isMeta":true` + extra + `,"message":{"content":` + string(b) + `}}` + "\n"
	}
	skill := "Base directory for this skill: /Users/someone/.claude/skills/ci-report\n\n# ci-report\n很長的說明"
	cases := []struct {
		name, line string
		kind       sdk.ChatKind
		text       string
	}{
		{"Skill 工具帶進來的", entry(`,"sourceToolUseID":"toolu_1"`, skill), "", ""},
		{"自己載入的技能", entry("", skill), sdk.ChatKindNotice, "載入技能 ci-report"},
		{"讀圖", entry("", "[Image: original 10x10]"), "", ""},
		{"系統提醒", entry("", "<system-reminder>\n<tools-used-before-summary>Read</tools-used-before-summary>\n</system-reminder>"), "", ""},
		{"指令外框", entry("", "<local-command-caveat>x</local-command-caveat>"), "", ""},
		{"額度快用完", entry("", "[Usage limit reached; a short grace allowance remains]"), sdk.ChatKindNotice, "額度快用完了"},
		{"額度恢復", entry("", "Your claude.ai usage limit has reset. Continue."), sdk.ChatKindNotice, "額度恢復了"},
		{"閒置通知", entry("", `[Cross-session idle notice] "uds:/tmp/a.sock", which you asked`), sdk.ChatKindNotice, "對方的對話閒下來了"},
		{"自己排的喚醒", entry("", "檢查 c1 還在不在\n第二行"), sdk.ChatKindNotice, "自動喚醒：檢查 c1 還在不在\n第二行"},
		{"區塊格式的技能", `{"type":"user","uuid":"u","timestamp":"2026-11-04T01:00:00Z","isMeta":true,"message":{"content":[{"type":"text","text":"Base directory for this skill: /x/pdf"}]}}` + "\n", sdk.ChatKindNotice, "載入技能 pdf"},
		{"跨 session 傳話", entry("", "Another Claude session sent a message:\n<cross-session-message from-name=\"life\">嗨</cross-session-message>"), sdk.ChatKindUser, "嗨"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got []sdk.ChatMessage
			for m, err := range Parse(strings.NewReader(c.line)) {
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, m)
			}
			if c.kind == "" {
				if len(got) != 0 {
					t.Fatalf("不該畫：%+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Kind != c.kind || got[0].Text != c.text {
				t.Fatalf("got %+v，want %s %q", got, c.kind, c.text)
			}
		})
	}
}

// subagent 交回的報告不是主人的話：有沒有平台那行開頭，都只畫一個「…」，不露內容（ECL-51）。
func TestAgentMessageShownAsEllipsis(t *testing.T) {
	for _, text := range []string{
		"<agent-message from=\"af82\">\n[Subagent hand-back] 報告內容\n</agent-message>",
		"Another Claude session sent a message:\n<agent-message from=\"af82\">\n[Subagent hand-back] 報告內容\n</agent-message>",
	} {
		for _, meta := range []bool{false, true} {
			var m sdk.ChatMessage
			var ok bool
			if meta {
				m, ok = metaText(text, false)
			} else {
				m, ok = userText(text)
			}
			if !ok || m.Kind != sdk.ChatKindNotice || m.Text != "…" {
				t.Fatalf("meta=%v：%+v", meta, m)
			}
		}
	}
}
