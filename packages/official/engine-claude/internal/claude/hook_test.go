package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// Guards: REQ-77, REQ-378
func TestHookCommandSilent(t *testing.T) {
	// 狀態類 hook 不輸出任何東西，即使核心回了內容。
	e := newHookEnv(t)
	e.host.context = "不該被印出來"
	e.host.check = &sdk.EngineHostCheckActionResponse{Verdict: sdk.ActionVerdictDeny, Reason: "x"}
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true}}
	for _, tc := range []struct{ event, file string }{
		{"SessionStart", "hooks/SessionStart.json"},
		{"UserPromptSubmit", "hooks/UserPromptSubmit.json"},
		{"PreToolUse", "hooks/PreToolUse-Bash.json"},
		{"PostToolUse", "hooks/PostToolUse-Bash.json"},
		{"Stop", "hooks/Stop.json"},
		{"StopFailure", "../limit/StopFailure-rate_limit.json"},
		{"StopFailure", "../limit/StopFailure-overloaded.json"},
		{"SessionEnd", "hooks/SessionEnd.json"},
		{"Notification", "hooks/Notification-idle_prompt.json"},
		{"PreCompact", "../compact/PreCompact-auto.json"},
	} {
		out, code := e.runFixture(t, tc.event, tc.file)
		if out != "" || code != 0 {
			t.Errorf("%s：stdout=%q exit=%d，要空且 0", tc.event, out, code)
		}
	}
}

// Guards: REQ-77
func TestHookExitsZeroWhenCoreDown(t *testing.T) {
	payloads := map[string]string{
		"SessionStart":      "hooks/SessionStart.json",
		"UserPromptSubmit":  "hooks/UserPromptSubmit.json",
		"PreToolUse":        "hooks/PreToolUse-Bash.json",
		"PostToolUse":       "hooks/PostToolUse-Bash.json",
		"Stop":              "hooks/Stop.json",
		"StopFailure":       "../limit/StopFailure-rate_limit.json",
		"PreCompact":        "../compact/PreCompact-auto.json",
		"SessionEnd":        "hooks/SessionEnd.json",
		"Notification":      "hooks/Notification-idle_prompt.json",
		"PermissionRequest": "hooks/PermissionRequest-Bash.json",
		"context":           "hooks/UserPromptSubmit.json",
		"check":             "hooks/PreToolUse-Bash.json",
	}
	// 核心回錯。
	e := newHookEnv(t)
	e.host.err = errCoreDown
	for event, file := range payloads {
		out, code := e.runFixture(t, event, file)
		if code != 0 || out != "" {
			t.Errorf("核心回錯 %s：stdout=%q exit=%d", event, out, code)
		}
	}
	// 真的連一個不存在的 socket（不開 socket，沙盒也能跑）。
	dir := t.TempDir()
	vars := map[string]string{"EVERGENIE_HOME": dir, "EVERGENIE_API_SOCKET": filepath.Join(dir, "none.sock"), "EVERGENIE_SESSION": "code"}
	real := HookDeps{Getenv: func(k string) string { return vars[k] }, Sleep: func(time.Duration) {}}
	for event, file := range payloads {
		var out strings.Builder
		code := RunHook(context.Background(), event, strings.NewReader(string(readFixture(t, file))), &out, real)
		if code != 0 || out.Len() != 0 {
			t.Errorf("socket 不存在 %s：stdout=%q exit=%d", event, out.String(), code)
		}
	}
	// payload 不是 JSON、空的。
	for _, bad := range []string{"", "not json", "{"} {
		if out, code := e.run("Stop", []byte(bad)); out != "" || code != 0 {
			t.Errorf("壞 payload %q：stdout=%q exit=%d", bad, out, code)
		}
	}
	// 缺任何一個環境變數 → 不印、exit 0、也沒有呼叫核心。
	for _, missing := range []string{"EVERGENIE_API_SOCKET", "EVERGENIE_SESSION", "EVERGENIE_HOME"} {
		e2 := newHookEnv(t)
		d := e2.deps()
		base := d.Getenv
		d.Getenv = func(k string) string {
			if k == missing {
				return ""
			}
			return base(k)
		}
		var out strings.Builder
		code := RunHook(context.Background(), "UserPromptSubmit", strings.NewReader(string(readFixture(t, "hooks/UserPromptSubmit.json"))), &out, d)
		if code != 0 || out.Len() != 0 || len(e2.host.events) != 0 {
			t.Errorf("缺 %s：stdout=%q exit=%d events=%d", missing, out.String(), code, len(e2.host.events))
		}
	}
	// 不認得的事件。
	if out, code := e.run("Whatever", []byte("{}")); out != "" || code != 0 {
		t.Errorf("不認得的事件：stdout=%q exit=%d", out, code)
	}
}

// Guards: REQ-75
func TestHookToEngineEventMapping(t *testing.T) {
	wantType := map[string]sdk.EngineEventType{
		"SessionStart":     sdk.EngineEventTypeSessionStarted,
		"UserPromptSubmit": sdk.EngineEventTypeTurnStarted,
		"PreToolUse":       sdk.EngineEventTypeActivityStarted,
		"PostToolUse":      sdk.EngineEventTypeActivityEnded,
		"Stop":             sdk.EngineEventTypeTurnEnded,
		"SessionEnd":       sdk.EngineEventTypeSessionEnded,
		"Notification":     sdk.EngineEventTypeIdleHint,
	}
	files, err := filepath.Glob(filepath.Join(spikeDir, "hooks", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("找不到 hook fixture：%v", err)
	}
	handled, skipped := 0, 0
	for _, f := range files {
		rel, _ := filepath.Rel(spikeDir, f)
		name := filepath.Base(f)
		// 明確略過：錄下的「輸出」不是 hook 的輸入。
		if strings.HasSuffix(name, ".out.json") || strings.HasSuffix(name, ".answer.json") {
			skipped++
			continue
		}
		event := fixtureField(t, rel, "hook_event_name")
		e := newHookEnv(t)
		out, code := e.runFixture(t, event, rel)
		if code != 0 {
			t.Errorf("%s：exit %d", name, code)
		}
		switch {
		case event == "PermissionRequest":
			if len(e.host.approvals) != 1 || len(e.host.events) != 0 {
				t.Errorf("%s：PermissionRequest 要呼叫 RequestApproval，不報事件（approvals=%d events=%d）", name, len(e.host.approvals), len(e.host.events))
			}
			if out != "" {
				t.Errorf("%s：核心沒決定時要什麼都不印，卻印了 %q", name, out)
			}
		case event == "Notification" && fixtureField(t, rel, "notification_type") != "idle_prompt":
			// 明確略過：只有 idle_prompt 對應 IDLE_HINT。
			if len(e.host.events) != 0 {
				t.Errorf("%s：不該報事件", name)
			}
			skipped++
			continue
		default:
			want, ok := wantType[event]
			if !ok {
				t.Errorf("%s：事件 %q 沒有對照", name, event)
				continue
			}
			if len(e.host.events) != 1 || e.host.events[0].Type != want {
				t.Errorf("%s：事件 = %+v，要 %s", name, e.host.events, want)
				continue
			}
			ev := e.host.events[0]
			if ev.Session != "code" || !ev.At.Equal(fixedNow) {
				t.Errorf("%s：Session/At 不對：%+v", name, ev)
			}
		}
		handled++
	}
	if handled+skipped != len(files) {
		t.Fatalf("有 fixture 沒被對到：handled=%d skipped=%d files=%d", handled, skipped, len(files))
	}
}

// Guards: REQ-75
func TestStopCarriesBackgroundTasks(t *testing.T) {
	e := newHookEnv(t)
	e.runFixture(t, "Stop", "hooks/Stop-background_tasks.json")
	e.runFixture(t, "Stop", "hooks/Stop.json")
	if len(e.host.events) != 2 {
		t.Fatalf("events = %d", len(e.host.events))
	}
	if ev := e.host.events[0]; ev.BackgroundTasks != 1 || ev.LastMessage != "STARTED" {
		t.Errorf("background_tasks 要原樣帶進 TURN_ENDED：%+v", ev)
	}
	if ev := e.host.events[1]; ev.BackgroundTasks != 0 || ev.LastMessage != "hello" {
		t.Errorf("%+v", ev)
	}
	e.runFixture(t, "SessionEnd", "hooks/SessionEnd.json")
	if ev := e.host.events[2]; ev.Reason != "prompt_input_exit" {
		t.Errorf("SessionEnd reason = %q", ev.Reason)
	}
}

// Guards: REQ-153, REQ-399
func TestInputNormalize(t *testing.T) {
	prompt := func(rel string) string { return fixtureField(t, rel, "prompt") }
	cases := []struct {
		name   string
		prompt string
		want   Input
	}{
		{"跨 session", prompt("hooks/UserPromptSubmit-cross-session.json"),
			Input{Text: "PING-CROSS from m2", Source: sdk.InputSourcePeer, From: "m2-2b"}},
		{"跨 session（有名字的對方）", prompt("hooks/UserPromptSubmit-named-peer.json"),
			Input{Text: "主人的 Goal GOAL-1，這一棒交給你：請照 Goal 原文做，做完用 SendMessage 回報給 s0c-q。", Source: sdk.InputSourcePeer, From: "s0c-q"}},
		{"背景工作完成", prompt("hooks/UserPromptSubmit-task-notification.json"),
			Input{Text: prompt("hooks/UserPromptSubmit-task-notification.json"), Source: sdk.InputSourceSystem}},
		{"一般", prompt("hooks/UserPromptSubmit.json"),
			Input{Text: "Reply with exactly: hello", Source: sdk.InputSourceTyped}},
		{"中斷後接著打字", "[Request interrupted by user for tool use]\n請繼續",
			Input{Text: "請繼續", Source: sdk.InputSourceTyped, Interrupted: true}},
		{"只有中斷那一行", "[Request interrupted by user]",
			Input{Text: "", Source: sdk.InputSourceTyped, Interrupted: true}},
		{"中斷後接跨 session", "[Request interrupted by user]\n<cross-session-message from=\"uds:/x\" from-name=\"code\" from-mode=\"prompting\">\n幫我查\n</cross-session-message>",
			Input{Text: "幫我查", Source: sdk.InputSourcePeer, From: "code", Interrupted: true}},
		{"pasted_content 外框", "<pasted_content id=\"abc\">\n第一行\n第二行\n</pasted_content>",
			Input{Text: "第一行\n第二行", Source: sdk.InputSourceTyped}},
		{"pasted_content 外框（不換行）", "<pasted_content>hi</pasted_content>",
			Input{Text: "hi", Source: sdk.InputSourceTyped}},
		{"pasted_content 結尾標籤帶 id（REQ-399）", "\n\n<pasted_content id=\"cb09\">\n第一行\n第二行\n</pasted_content id=\"cb09\">\n",
			Input{Text: "第一行\n第二行", Source: sdk.InputSourceTyped}},
		{"只有一半的外框不動", "<pasted_content>hi",
			Input{Text: "<pasted_content>hi", Source: sdk.InputSourceTyped}},
		{"transcript 形式的跨 session", "Another Claude session sent a message:\n<cross-session-message from=\"uds:/x\" from-name=\"work\" from-mode=\"prompting\">\nhello\n</cross-session-message>\n\n平台附的說明",
			Input{Text: "hello", Source: sdk.InputSourcePeer, From: "work"}},
		{"一般文字提到標籤不算", "請看 <cross-session-message> 這個標籤",
			Input{Text: "請看 <cross-session-message> 這個標籤", Source: sdk.InputSourceTyped}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeInput(tc.prompt); got != tc.want {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// Guards: REQ-153
func TestInputNormalizeReportedAndRecorded(t *testing.T) {
	e := newHookEnv(t)
	e.runFixture(t, "UserPromptSubmit", "hooks/UserPromptSubmit-cross-session.json")
	if len(e.host.events) != 1 {
		t.Fatalf("events = %d", len(e.host.events))
	}
	ev := e.host.events[0]
	if ev.Type != sdk.EngineEventTypeTurnStarted || ev.Source != sdk.InputSourcePeer || ev.From != "m2-2b" || ev.Input != "PING-CROSS from m2" || ev.Interrupted {
		t.Fatalf("TURN_STARTED 不對：%+v", ev)
	}
	data, err := os.ReadFile(filepath.Join(e.home, "runtime", "engine-claude", "turns", "code.json"))
	if err != nil {
		t.Fatalf("回合來源沒寫到 turns：%v", err)
	}
	var rec turnRecord
	if err := jsonUnmarshal(data, &rec); err != nil || rec.Source != sdk.InputSourcePeer || rec.From != "m2-2b" || !rec.At.Equal(fixedNow) {
		t.Fatalf("turns 內容不對：%s（%v）", data, err)
	}
	// 輸入的開頭也記下來（正規化後的），Deliver 拿來確認貼上的那句送出了（ECL-42）。
	if rec.Head != "PING-CROSS from m2" {
		t.Fatalf("turns 要記輸入開頭：%q", rec.Head)
	}
	// 核心不在也要寫。
	e2 := newHookEnv(t)
	e2.host.err = errCoreDown
	e2.runFixture(t, "UserPromptSubmit", "hooks/UserPromptSubmit-task-notification.json")
	if _, err := os.Stat(filepath.Join(e2.home, "runtime", "engine-claude", "turns", "code.json")); err != nil {
		t.Fatalf("核心不在也要寫 turns：%v", err)
	}
}

// 自動整理對話（PreCompact trigger=auto）報 ACTIVITY_STARTED「整理對話」，畫面才知道她在整理、不是當機；
// 主人在終端機打的 /compact（manual）不在一輪裡面，不報（報了核心會把她記成處理中、等不到結束）（ECL-43）。
// testdata/compact/ 的樣本是照平台 hook 文件的欄位（共同欄位＋trigger、custom_instructions）造的，不是錄下來的。
func TestHookPreCompactReportsTidyingOnlyWhenAuto(t *testing.T) {
	// transcript_path 換成讀不到的：樣本裡是真機的路徑，這台電腦上剛好有那個檔、而且那一輪已結束，測試就會看機器而變。
	e := newHookEnv(t)
	if out, code := e.run("PreCompact", limitPayload(t, "../compact/PreCompact-auto.json", "")); out != "" || code != 0 {
		t.Fatalf("不印、exit 0：%q %d", out, code)
	}
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeActivityStarted || e.host.events[0].Activity != "整理對話" || e.host.events[0].Session != "code" {
		t.Fatalf("自動整理要報 ACTIVITY_STARTED「整理對話」：%+v", e.host.events)
	}
	e = newHookEnv(t)
	e.run("PreCompact", limitPayload(t, "../compact/PreCompact-manual.json", ""))
	if len(e.host.events) != 0 {
		t.Fatalf("手動 /compact 不報：%+v", e.host.events)
	}
	// 被換掉的那一次 Claude（啟動記號不同）送來的也不報（ECL-24）。
	e = newHookEnv(t)
	if err := writeFileAtomic(launchFilePath(e.home, "code"), []byte(`"new-launch"`), 0o644); err != nil {
		t.Fatal(err)
	}
	e.runFixture(t, "PreCompact", "../compact/PreCompact-auto.json")
	if len(e.host.events) != 0 {
		t.Fatalf("舊的那一次不報：%+v", e.host.events)
	}
}

// 閒著時平台自己做的自動壓縮（trigger 也是 auto）不報，不然核心把她記成處理中、一直等不到 Stop；
// 一輪中間的自動壓縮照報。對話記錄尾巴的樣子照 2026-11-08 那次的記錄縮寫。
func TestHookPreCompactSkipsIdleAutoCompact(t *testing.T) {
	ended := `{"type":"assistant","message":{"stop_reason":"end_turn","content":[{"type":"text","text":"好了"}]}}
{"type":"system","subtype":"stop_hook_summary"}
{"type":"system","subtype":"turn_duration"}
{"type":"last-prompt"}
{"type":"custom-title"}
`
	cases := []struct {
		name, tail string
		report     bool
	}{
		{"一輪結束之後", ended, false},
		{"最後一則是 end_turn", `{"type":"assistant","message":{"stop_reason":"end_turn","content":[]}}` + "\n", false},
		{"主人按了停止", `{"type":"user","message":{"content":[{"type":"text","text":"[Request interrupted by user]"}]}}` + "\n", false},
		{"API 錯誤結束", `{"type":"assistant","isApiErrorMessage":true,"message":{"stop_reason":"stop_sequence","content":[{"type":"text","text":"API Error"}]}}` + "\n", false},
		{"背景小幫手的記錄不算", ended + `{"type":"assistant","isSidechain":true,"message":{"stop_reason":"tool_use","content":[]}}` + "\n", false},
		{"等工具結果", `{"type":"assistant","message":{"stop_reason":"tool_use","content":[]}}` + "\n", true},
		{"工具剛回來", ended + `{"type":"user","message":{"content":"再查一下"}}` + "\n" + `{"type":"assistant","message":{"stop_reason":"tool_use","content":[]}}` + "\n" + `{"type":"user","message":{"content":[{"type":"tool_result","content":"ok"}]}}` + "\n", true},
		{"主人剛說話", ended + `{"type":"user","message":{"content":"在嗎"}}` + "\n", true},
	}
	for _, c := range cases {
		e := newHookEnv(t)
		path := filepath.Join(e.home, "t.jsonl")
		if err := os.WriteFile(path, []byte(c.tail), 0o644); err != nil {
			t.Fatal(err)
		}
		payload, _ := json.Marshal(map[string]string{"session_id": "s", "transcript_path": path, "hook_event_name": "PreCompact", "trigger": "auto"})
		e.run("PreCompact", payload)
		if got := len(e.host.events) == 1; got != c.report {
			t.Errorf("%s：要報 %v，收到 %+v", c.name, c.report, e.host.events)
		}
	}
}

// Guards: REQ-14
func TestDirectMessageFromPreAndPostToolUse(t *testing.T) {
	cases := []struct {
		file   string
		to     string
		text   string
		ok     bool
		isPost bool
	}{
		{"hooks/PreToolUse-SendMessage.json", "m1-e1 [069cbd]", "PING-CROSS from m2", false, false},
		{"hooks/PostToolUse-SendMessage.json", "m1-e1 [069cbd]", "PING-CROSS from m2", true, true},
		{"hooks/PostToolUse-SendMessage-named.json", "s0c-r", "PING-C", true, true},
		{"hooks/PostToolUse-SendMessage-unreachable.json", "s0c-c", "PING-D", false, true},
	}
	for _, tc := range cases {
		e := newHookEnv(t)
		event := "PreToolUse"
		want := sdk.EngineEventTypeActivityStarted
		if tc.isPost {
			event, want = "PostToolUse", sdk.EngineEventTypeActivityEnded
		}
		e.runFixture(t, event, tc.file)
		if len(e.host.events) != 1 {
			t.Fatalf("%s：events = %d", tc.file, len(e.host.events))
		}
		ev := e.host.events[0]
		if ev.Type != want || ev.Activity != "傳話" || ev.DirectMessage == nil {
			t.Fatalf("%s：%+v", tc.file, ev)
		}
		if dm := *ev.DirectMessage; dm.To != tc.to || dm.Text != tc.text || dm.OK != tc.ok {
			t.Errorf("%s：DirectMessage = %+v，要 To=%q Text=%q OK=%v", tc.file, dm, tc.to, tc.text, tc.ok)
		}
	}
	// 其他工具不帶直接傳話。
	e := newHookEnv(t)
	e.runFixture(t, "PreToolUse", "hooks/PreToolUse-Bash.json")
	if e.host.events[0].DirectMessage != nil || e.host.events[0].Activity != "跑指令" {
		t.Errorf("Bash 不該帶 DirectMessage：%+v", e.host.events[0])
	}
}

// 處理中後面的字是白話（SES-63）：常見工具翻成讀檔、改檔、跑指令…；外掛工具寫「用 <服務>」；查不到的照原名。
func TestActivityLabelIsPlainWords(t *testing.T) {
	for tool, want := range map[string]string{
		"Read": "讀檔", "Edit": "改檔", "Bash": "跑指令", "WebSearch": "上網查",
		"mcp__jira__jira_get": "用 jira", "mcp__": "mcp__", "SomethingNew": "SomethingNew",
	} {
		if got := activityLabel(tool); got != want {
			t.Errorf("%s → %q，要 %q", tool, got, want)
		}
	}
}

// Guards: REQ-14
func TestSenderAddress(t *testing.T) {
	for _, tc := range []struct{ name, prompt, addr, from string }{
		{"跨 session", fixtureField(t, "hooks/UserPromptSubmit-cross-session.json", "prompt"), "uds:/tmp/cc-socks/11566.sock", "m2-2b"},
		{"transcript 形式", "Another Claude session sent a message:\n<cross-session-message from=\"uds:/x.sock\" from-name=\"work\" from-mode=\"prompting\">\nhi\n</cross-session-message>", "uds:/x.sock", "work"},
		{"一般輸入", "hello", "", ""},
		{"文字裡提到標籤", "請看 <cross-session-message from=\"uds:/x\"> 這個", "", ""},
	} {
		if addr, from := SenderAddress(tc.prompt); addr != tc.addr || from != tc.from {
			t.Errorf("%s：addr=%q from=%q，要 %q %q", tc.name, addr, from, tc.addr, tc.from)
		}
	}
}

// Guards: REQ-14
func TestDirectReplyToSenderAddressResolvesToName(t *testing.T) {
	// 收件方回覆時，SendMessage 的 to 常是寄件人外框裡的 uds: 位址；要還原成名字，核心才對得上「回給寄件人」。
	e := newHookEnv(t)
	e.runFixture(t, "UserPromptSubmit", "hooks/UserPromptSubmit-cross-session.json")
	payload := []byte(`{"hook_event_name":"PostToolUse","tool_name":"SendMessage","tool_input":{"to":"uds:/tmp/cc-socks/11566.sock","message":"答案","recipient_kind":"session"},"tool_response":{"success":true}}`)
	e.run("PostToolUse", payload)
	// 沒見過的位址、名字、帶 [id] 尾巴的都原樣。
	e.run("PostToolUse", []byte(`{"hook_event_name":"PostToolUse","tool_name":"SendMessage","tool_input":{"to":"uds:/tmp/cc-socks/999.sock","message":"x"},"tool_response":{"success":true}}`))
	e.run("PostToolUse", []byte(`{"hook_event_name":"PostToolUse","tool_name":"SendMessage","tool_input":{"to":"code","message":"x"},"tool_response":{"success":true}}`))
	var got []string
	for _, ev := range e.host.events {
		if ev.DirectMessage != nil {
			got = append(got, ev.DirectMessage.To)
		}
	}
	want := []string{"m2-2b", "uds:/tmp/cc-socks/999.sock", "code"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("To = %q，要 %q", got, want)
	}
}

// Guards: REQ-79
func TestQuestionRoundTrip(t *testing.T) {
	e := newHookEnv(t)
	answers := map[string]string{
		"Which color do you like?":  "Red",
		"Which fruits do you want?": "Apple, Banana",
		"Anything else to tell me?": "free text typed by the owner",
	}
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true, Answers: answers}}
	out, code := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-AskUserQuestion.json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !jsonEqual(t, []byte(out), readFixture(t, "hooks/PermissionRequest-AskUserQuestion.answer.json")) {
		t.Fatalf("輸出跟錄下的 .answer.json 不同：\n%s", out)
	}
	req := e.host.approvals[0]
	if req.Kind != sdk.ApprovalKindQuestion || req.Session != "code" || len(req.Questions) != 3 || req.MaxWaitSeconds < 600 {
		t.Fatalf("RequestApproval 不對：%+v", req)
	}
	q := req.Questions[1]
	if q.Header != "Fruits" || q.Text != "Which fruits do you want?" || !q.MultiSelect || len(q.Options) != 3 || q.Options[1].Label != "Banana" || q.Options[1].Description != "Banana" {
		t.Errorf("題目沒有原樣轉成 Question：%+v", q)
	}
	if req.Questions[0].MultiSelect || req.Questions[0].Text != "Which color do you like?" {
		t.Errorf("第一題：%+v", req.Questions[0])
	}
	// 拒絕問題（沒有答案）→ deny。
	e2 := newHookEnv(t)
	e2.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: false, Message: "主人不想回答"}}
	out2, _ := e2.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-AskUserQuestion.json")
	if !jsonEqual(t, []byte(out2), []byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"主人不想回答"}}}`)) {
		t.Errorf("拒絕問題的輸出：%s", out2)
	}
}

// Guards: REQ-78
func TestPermissionRequestDecisions(t *testing.T) {
	e := newHookEnv(t)
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true}}
	out, _ := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-Bash.json")
	if !jsonEqual(t, []byte(out), readFixture(t, "hooks/PermissionRequest-Bash.answer.json")) {
		t.Fatalf("允許的輸出跟 .answer.json 不同：%s", out)
	}
	req := e.host.approvals[0]
	if req.Kind != sdk.ApprovalKindPermission || req.Tool != "Bash" || req.Summary != "touch ALLOWED-1 && echo done" || req.MaxWaitSeconds != 600 {
		t.Errorf("RequestApproval 不對：%+v", req)
	}
	// 拒絕帶理由。
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: false, Message: "不行"}}
	out, _ = e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-Bash.json")
	if !jsonEqual(t, []byte(out), []byte(`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"不行"}}}`)) {
		t.Errorf("拒絕的輸出：%s", out)
	}
	// 逾時或核心不接手（Decided=false）、核心回錯、Decision 空 → 什麼都不印。
	for name, resp := range map[string]*sdk.EngineHostRequestApprovalResponse{
		"未決定": {Decided: false}, "沒有 Decision": {Decided: true}, "預設": nil,
	} {
		e.host.approval = resp
		if out, _ := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-Bash.json"); out != "" {
			t.Errorf("%s：要什麼都不印，卻印了 %q", name, out)
		}
	}
}

// Guards: REQ-78
func TestPermissionSummary(t *testing.T) {
	cases := map[string]string{
		`{"command":"ls","file_path":"/a"}`: "ls",
		`{"file_path":"/a/b.go"}`:           "/a/b.go",
		`{"url":"https://x.test"}`:          "https://x.test",
		`{"pattern":"foo"}`:                 `{"pattern":"foo"}`,
		`{}`:                                `{}`,
	}
	for in, want := range cases {
		if got := summarizeInput([]byte(in)); got != want {
			t.Errorf("summarize(%s) = %q，要 %q", in, got, want)
		}
	}
	long := `{"command":"` + strings.Repeat("字", 300) + `"}`
	if got := summarizeInput([]byte(long)); len([]rune(got)) != 200 {
		t.Errorf("要截成 200 字，實際 %d", len([]rune(got)))
	}
	if got := summarizeInput([]byte(`{"x":"` + strings.Repeat("a", 300) + `"}`)); len([]rune(got)) != 200 {
		t.Errorf("JSON 也要截成 200 字，實際 %d", len([]rune(got)))
	}
}

// Guards: REQ-47, REQ-152
func TestContextHookOnlyWhenNonEmpty(t *testing.T) {
	e := newHookEnv(t)
	if out, code := e.runFixture(t, "context", "hooks/UserPromptSubmit.json"); out != "" || code != 0 {
		t.Fatalf("核心回空字串要什麼都不印：%q exit=%d", out, code)
	}
	e.host.context = "現況：12:00\n主人的 Goal GOAL-1：…"
	out, code := e.runFixture(t, "context", "hooks/UserPromptSubmit-cross-session.json")
	if code != 0 || strings.TrimSpace(out) != e.host.context {
		t.Fatalf("非空要印出來：%q", out)
	}
	last := e.host.contexts[len(e.host.contexts)-1]
	if last.Session != "code" || last.Input != "PING-CROSS from m2" {
		t.Errorf("GetContext 的輸入要是正規化後的文字：%+v", last)
	}
	// 核心回錯 → 不印。
	e.host.err = errCoreDown
	if out, _ := e.runFixture(t, "context", "hooks/UserPromptSubmit.json"); out != "" {
		t.Errorf("核心回錯要什麼都不印：%q", out)
	}
}

// Guards: REQ-256
func TestHookFromReplacedLaunchIsDropped(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	first := envMap(v.start("code", nil).Env)[launchVar]
	if first == "" {
		t.Fatal("Start 要給 Claude 啟動記號")
	}
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	second := envMap(v.start("code", nil).Env)[launchVar]
	if second == "" || second == first {
		t.Fatalf("每次啟動的記號要不一樣：%q %q", first, second)
	}
	run := func(launch string) []*sdk.EngineEvent {
		host := &fakeHost{}
		vars := map[string]string{"EVERGENIE_HOME": v.home, "EVERGENIE_API_SOCKET": filepath.Join(v.home, "api.sock"), "EVERGENIE_SESSION": "code", launchVar: launch}
		d := HookDeps{Host: host, Getenv: func(k string) string { return vars[k] }, Now: func() time.Time { return fixedNow }, Sleep: func(time.Duration) {}}
		for _, ev := range []string{"SessionEnd", "Stop", "SessionStart"} {
			RunHook(ctx, ev, strings.NewReader(string(readFixture(t, "hooks/"+ev+".json"))), &bytes.Buffer{}, d)
		}
		return host.events
	}
	// 被砍掉的那一次（重開前的 Claude）之後才觸發的 hook：丟掉，不能把新的記成停了。
	if got := run(first); len(got) != 0 {
		t.Fatalf("舊的那一次的事件要丟掉：%+v", got)
	}
	// 舊版開的 Claude 沒有記號；有記號檔就表示它已經被換掉。
	if got := run(""); len(got) != 0 {
		t.Fatalf("沒有記號的舊 Claude 要丟掉：%+v", got)
	}
	if got := run(second); len(got) != 3 || got[0].Type != sdk.EngineEventTypeSessionEnded {
		t.Fatalf("現在這一次的事件要照送：%+v", got)
	}
	// Stop 之後（還沒重開）被砍的 Claude 觸發的 SessionEnd 也算舊的；Stop 自己會回報結束。
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	if got := run(second); len(got) != 0 {
		t.Fatalf("Stop 之後被砍的 Claude 的事件要丟掉：%+v", got)
	}
}

// Guards: REQ-256
func TestCheckAliveKeepsSessionRestartedMidCheck(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.start("code", nil)
	old := v.e.sessions["code"]
	if _, err := v.e.Stop(ctx, &sdk.EngineStopRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	v.start("code", nil)
	// 存活檢查拿到的是重開前那一筆：不能把重開後的這筆當成掛了忘掉。
	if v.e.forget("code", old) {
		t.Fatal("記憶裡已經換成新的，不該忘掉")
	}
	if v.e.sessions["code"] == nil {
		t.Fatal("重開後的 session 不見了")
	}
}

// 自動壓縮對話（SessionStart source=compact）不是新 session：不回報 SessionStarted，
// 不然核心會把正在做事的秘書翻成閒著、派送器把那則記成中斷（DEV-94）。startup／resume／clear 照舊。
func TestSessionStartCompactKeepsState(t *testing.T) {
	for _, tc := range []struct {
		source string
		want   int
	}{{"compact", 0}, {"startup", 1}, {"resume", 1}, {"clear", 1}, {"", 1}} {
		e := newHookEnv(t)
		if _, code := e.run("SessionStart", []byte(`{"hook_event_name":"SessionStart","source":"`+tc.source+`"}`)); code != 0 {
			t.Fatalf("%s：exit %d", tc.source, code)
		}
		if len(e.host.events) != tc.want {
			t.Errorf("source=%q：事件 %d 個，要 %d 個（%+v）", tc.source, len(e.host.events), tc.want, e.host.events)
		}
	}
}

// 在她畫面上 /clear：SessionStarted 帶新的對話編號給核心（之後接回用它，SES-61）；startup 不帶。
func TestSessionStartClearReportsNewEngineSessionID(t *testing.T) {
	e := newHookEnv(t)
	if _, code := e.run("SessionStart", []byte(`{"hook_event_name":"SessionStart","source":"clear","session_id":"abc-123"}`)); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(e.host.events) != 1 || e.host.events[0].EngineSessionID != "abc-123" {
		t.Fatalf("要帶新的編號：%+v", e.host.events)
	}
	e = newHookEnv(t)
	e.run("SessionStart", []byte(`{"hook_event_name":"SessionStart","source":"startup","session_id":"abc-123"}`))
	if len(e.host.events) != 1 || e.host.events[0].EngineSessionID != "" {
		t.Fatalf("startup 不帶：%+v", e.host.events)
	}
}

// SessionEnd 的 reason：Claude 的 logout 換成契約的 sdk.SessionEndReasonLoggedOut（核心靠它提醒主人重新登入），
// 其他原因照原樣交給核心。
func TestSessionEndLogoutBecomesLoggedOut(t *testing.T) {
	for platform, want := range map[string]string{
		"logout":            sdk.SessionEndReasonLoggedOut,
		"prompt_input_exit": "prompt_input_exit",
		"other":             "other",
		"":                  "",
	} {
		e := newHookEnv(t)
		payload, _ := json.Marshal(map[string]string{"hook_event_name": "SessionEnd", "reason": platform})
		if out, code := e.run("SessionEnd", payload); out != "" || code != 0 {
			t.Fatalf("reason=%q：不印、exit 0：out=%q code=%d", platform, out, code)
		}
		if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeSessionEnded || e.host.events[0].Reason != want {
			t.Errorf("reason=%q：事件 = %+v，要 SESSION_ENDED{%q}", platform, e.host.events, want)
		}
	}
}

// 選擇題等 12 小時（帶給核心的 MaxWaitSeconds），權限確認照舊 600 秒；平台那邊的 hook 逾時比 12 小時多一點（ECL-56）。
func TestQuestionApprovalWaitsTwelveHours(t *testing.T) {
	e := newHookEnv(t)
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true, Answers: map[string]string{}}}
	e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-AskUserQuestion.json")
	if len(e.host.approvals) != 1 || e.host.approvals[0].MaxWaitSeconds != 12*60*60 {
		t.Fatalf("選擇題要等 12 小時：%+v", e.host.approvals)
	}
	if approvalHookTimeout <= 12*60*60 {
		t.Fatalf("平台的 hook 逾時 %d 秒要比 12 小時長，hook 才能自己收手", approvalHookTimeout)
	}
}

// 連不上核心（serve 重開、部署）時每 2 秒再問一次、帶剩下的時間，核心回來就照常拿到答案；不交回她的畫面（ECL-56）。
func TestApprovalRetriesWhileCoreUnreachable(t *testing.T) {
	e := newHookEnv(t)
	e.host.approvalErrs = []error{errCoreDown, &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "還沒好"}}
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true}}
	out, _ := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-Bash.json")
	if !jsonEqual(t, []byte(out), readFixture(t, "hooks/PermissionRequest-Bash.answer.json")) {
		t.Fatalf("核心回來後要照常允許：%s", out)
	}
	if len(e.host.approvals) != 3 {
		t.Fatalf("要問三次（兩次連不上），實際 %d", len(e.host.approvals))
	}
}

// 核心一直連不上：最多再問 3 分鐘（夠蓋過部署）就交回她的畫面，不等到 12 小時；每次帶的剩下時間越來越少（ECL-56）。
func TestApprovalReconnectGivesUpAfterThreeMinutes(t *testing.T) {
	e := newHookEnv(t)
	e.moving = true
	for i := 0; i < 500; i++ {
		e.host.approvalErrs = append(e.host.approvalErrs, errCoreDown)
	}
	if out, _ := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-AskUserQuestion.json"); out != "" {
		t.Fatalf("連不上就交回她的畫面（什麼都不印）：%q", out)
	}
	if got := len(e.host.approvals); got != 91 {
		t.Fatalf("3 分鐘每 2 秒一次＝91 次，實際 %d", got)
	}
	if e.now.Sub(fixedNow) > 3*time.Minute {
		t.Fatalf("重試超過 3 分鐘：%s", e.now.Sub(fixedNow))
	}
	first, last := e.host.approvals[0].MaxWaitSeconds, e.host.approvals[len(e.host.approvals)-1].MaxWaitSeconds
	if first != 12*60*60 || last != first-180 {
		t.Fatalf("剩下時間要跟著時鐘減少：第一次 %d、最後 %d", first, last)
	}
}

// 接上核心、等了一陣子之後核心又重開（一天部署兩次）：新的一段斷線重新算 3 分鐘，不被前一段用掉的次數扣掉（ECL-56）。
func TestApprovalReconnectWindowRestartsAfterLongWait(t *testing.T) {
	e := newHookEnv(t)
	e.moving = true
	for i := 0; i < 300; i++ {
		e.host.approvalErrs = append(e.host.approvalErrs, errCoreDown)
	}
	e.host.during = func(n int) {
		if n == 51 {
			e.now = e.now.Add(time.Hour) // 第 51 次接上了、等了一小時才斷
		}
	}
	e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-AskUserQuestion.json")
	if got := len(e.host.approvals); got != 50+91 {
		t.Fatalf("第一段 50 次、第二段重新算 91 次，實際 %d", got)
	}
}

// 請求被取消或逾時（不是連不上）不重試。
func TestApprovalDoesNotRetryWhenCanceled(t *testing.T) {
	for _, err := range []error{context.Canceled, context.DeadlineExceeded} {
		e := newHookEnv(t)
		e.host.approvalErrs = []error{err, err}
		e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true}}
		if out, _ := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-Bash.json"); out != "" {
			t.Fatalf("%v 要交回畫面：%q", err, out)
		}
		if len(e.host.approvals) != 1 {
			t.Fatalf("%v 不能重試，實際問了 %d 次", err, len(e.host.approvals))
		}
	}
}

// 核心回的其他錯（例如已經在等別的確認）不重試，照舊交回她的畫面（什麼都不印）。
func TestApprovalDoesNotRetryOnCoreRefusal(t *testing.T) {
	e := newHookEnv(t)
	e.host.approvalErrs = []error{&sdk.Error{Code: sdk.ErrorCodeConflict, Message: "這個對話已在等待確認"}}
	e.host.approval = &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: &sdk.Decision{Allow: true}}
	if out, _ := e.runFixture(t, "PermissionRequest", "hooks/PermissionRequest-Bash.json"); out != "" {
		t.Fatalf("核心拒絕要什麼都不印：%q", out)
	}
	if len(e.host.approvals) != 1 {
		t.Fatalf("不能重試，實際問了 %d 次", len(e.host.approvals))
	}
}

// subagent 裡觸發的工具事件（帶 agent_id）不回報，免得把主對話正在等的選擇題收掉（ECL-60）。
func TestRunHookSubagentToolEventNotReported(t *testing.T) {
	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		e := newHookEnv(t)
		e.run(event, []byte(`{"hook_event_name":"`+event+`","tool_name":"Bash","tool_input":{"command":"ls"},"agent_id":"a07cb4a92289896df","agent_type":"general-purpose"}`))
		if len(e.host.events) != 0 {
			t.Fatalf("%s：subagent 的事件不該回報：%+v", event, e.host.events)
		}
		main := newHookEnv(t)
		main.run(event, []byte(`{"hook_event_name":"`+event+`","tool_name":"Bash","tool_input":{"command":"ls"}}`))
		if len(main.host.events) != 1 {
			t.Fatalf("%s：主對話的事件要回報：%d", event, len(main.host.events))
		}
		dm := newHookEnv(t)
		dm.run(event, []byte(`{"hook_event_name":"`+event+`","tool_name":"SendMessage","tool_input":{"to":"code","message":"hi"},"agent_id":"a1"}`))
		if len(dm.host.events) != 1 || dm.host.events[0].DirectMessage == nil {
			t.Fatalf("%s：subagent 的傳話照樣回報（回執要記）：%+v", event, dm.host.events)
		}
	}
}
