package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

// writeCoreSession 模擬核心記的 runtime/sessions/<名>.md（只放引擎會讀的欄位）。
func writeCoreSession(t *testing.T, home, name, state string) {
	t.Helper()
	dir := filepath.Join(home, "runtime", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\nstate: " + state + "\n---\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 電腦重開後：核心記得「待命」的 session，tmux 已經不在 → 回報 SESSION_ENDED（gone）；還在的納入之後的檢查；停了的不管。
// Guards: REQ-300
func TestCheckAliveReportsOrphansAfterRestart(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	writeCoreSession(t, v.home, "health", "idle")
	writeCoreSession(t, v.home, "life", "busy")
	writeCoreSession(t, v.home, "art", "stopped")
	v.tmux.setAlive("life", true)

	v.e.CheckAlive(ctx)
	if len(v.host.events) != 1 || v.host.events[0].Session != "health" || v.host.events[0].Type != sdk.EngineEventTypeSessionEnded || v.host.events[0].Reason != "gone" {
		t.Fatalf("只有不在的 health 要回報 gone：%+v", v.host.events)
	}
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 1 {
		t.Fatalf("報過的不重報：%+v", v.host.events)
	}
	// 重開前就在跑的 life 之後掛了，也要看得到。
	v.tmux.setAlive("life", false)
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 2 || v.host.events[1].Session != "life" {
		t.Fatalf("life 掛了要回報：%+v", v.host.events)
	}
}

// 核心還沒好（回報失敗）就留著，下一輪再報。
// Guards: REQ-300
func TestOrphanRetriedWhenCoreNotReady(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	writeCoreSession(t, v.home, "health", "idle")
	v.host.err = &sdk.Error{Code: sdk.ErrorCodeUnavailable, Message: "還沒好"}
	v.e.CheckAlive(ctx)
	v.host.err = nil
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 2 || v.host.events[1].Session != "health" {
		t.Fatalf("第一次失敗要再報一次：%+v", v.host.events)
	}
	v.e.CheckAlive(ctx)
	if len(v.host.events) != 2 {
		t.Fatalf("成功之後不再報：%+v", v.host.events)
	}
}

// 只傳照片時看板送的是 `![檔名](/share/…)`：開頭的 ! 會被 Claude 當成終端機指令，要補空白。
// Guards: REQ-303
func TestDeliverEscapesLeadingBang(t *testing.T) {
	v := newEngEnv(t)
	v.tmux.setAlive("health", true)
	text := "![IMG_8613.jpeg](/share/x.jpeg)"
	if r, err := v.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: "health", Text: text}); err != nil || !r.Delivered {
		t.Fatalf("要送出：%+v %v", r, err)
	}
	got := v.tmux.actions()[0]
	if got.Op != "paste" || got.Arg != " "+text {
		t.Fatalf("開頭的 ! 前面要補空白：%+v", got)
	}
	if safeLeading("你好 !") != "你好 !" {
		t.Fatal("不是開頭的 ! 不動")
	}
}

// 讀照片時平台自己加的「[Image: original …]」（isMeta）不是主人說的話。
// Guards: REQ-303
func TestTranscriptSkipsMetaText(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"user","uuid":"u1","timestamp":"2026-10-30T00:14:11Z","message":{"role":"user","content":"這台要注意什麼"}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-10-30T00:14:13Z","isMeta":true,"message":{"role":"user","content":"[Image: original 3024x4032, displayed at 1500x2000.]"}}`,
		`{"type":"assistant","uuid":"a1","timestamp":"2026-10-30T00:14:22Z","message":{"role":"assistant","content":[{"type":"text","text":"這台不是卷腹機喔"}]}}`,
	}, "\n") + "\n"
	msgs, err := parseAll([]byte(lines))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range msgs {
		texts = append(texts, string(m.Kind)+":"+m.Text)
	}
	if len(msgs) != 2 || strings.Contains(strings.Join(texts, "|"), "[Image:") {
		t.Fatalf("isMeta 的文字要略過：%v", texts)
	}
}

// 壓縮上下文後 Claude Code 寫的「This session is being continued…」接續摘要是 user entry 但不是人說的話，看板不畫（ECL-57，OF-16）。
// 壓縮前後主人真的說的話照常顯示。
func TestTranscriptSkipsCompactSummary(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"user","uuid":"u1","timestamp":"2026-11-03T09:40:00Z","message":{"role":"user","content":"整理一下看板"}}`,
		`{"type":"system","subtype":"compact_boundary","uuid":"s1","timestamp":"2026-11-03T09:53:29Z","content":"Conversation compacted"}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-11-03T09:53:30Z","isVisibleInTranscriptOnly":true,"isCompactSummary":true,"message":{"role":"user","content":"This session is being continued from a previous conversation that ran out of context.\nSummary:\n1. Primary Request and Intent: ..."}}`,
		`{"type":"user","uuid":"u3","timestamp":"2026-11-03T10:00:00Z","message":{"role":"user","content":"繼續"}}`,
	}, "\n") + "\n"
	msgs, err := parseAll([]byte(lines))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, m := range msgs {
		texts = append(texts, string(m.Kind)+":"+m.Text)
	}
	if len(msgs) != 2 || strings.Contains(strings.Join(texts, "|"), "This session is being continued") {
		t.Fatalf("壓縮接續摘要要略過、主人的話要留：%v", texts)
	}
}

// 引擎（或 serve）重開後，記憶裡沒有信任框，但接手的既有 session 畫面還停在那裡：ListBootPrompts 現場看一次、重新登記；
// 主人在終端機按掉了，下一次看到畫面不是信任框就撤掉（DEV-87）。
// Guards: DEV-87
func TestAdoptedSessionStuckOnTrustIsRegistered(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	writeCoreSession(t, v.home, "code", "starting")
	writeCoreSession(t, v.home, "life", "idle")
	v.tmux.setAlive("code", true)
	v.tmux.setAlive("life", true)
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v.tmux.setScreen("life", readScreen(t, "remote-control-active.txt"))

	list, err := v.e.ListBootPrompts(ctx, nil)
	if err != nil || len(list.Prompts) != 1 || list.Prompts[0].Session != "code" || len(list.Prompts[0].Choices) < 2 {
		t.Fatalf("接手的 code 卡在信任框，要重新登記（life 不是）：%+v %v", list, err)
	}
	if one, _ := v.e.ListBootPrompts(ctx, &sdk.EngineListBootPromptsRequest{Session: "life"}); len(one.Prompts) != 0 {
		t.Fatalf("依 session 篩選：%+v", one)
	}
	// 登記之後可以照常回答。
	if _, err := v.e.AnswerBootPrompt(ctx, &sdk.EngineAnswerBootPromptRequest{ID: list.Prompts[0].ID, Choice: list.Prompts[0].Choices[0]}); err != nil {
		t.Fatal(err)
	}
	// 主人自己在終端機按掉了：畫面不再是信任框，下一次檢查要撤掉。
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	if l, _ := v.e.ListBootPrompts(ctx, nil); len(l.Prompts) != 1 {
		t.Fatalf("還卡著要再登記：%+v", l)
	}
	v.tmux.setScreen("code", readScreen(t, "remote-control-active.txt"))
	if l, _ := v.e.ListBootPrompts(ctx, nil); len(l.Prompts) != 0 {
		t.Fatalf("畫面已經不是信任框，要撤掉：%+v", l)
	}
}

// 定期檢查（CheckAlive）也看畫面：自己開的 session 過了 20 秒才出現信任框，或畫面上的信任框已被按掉。
// Guards: DEV-87
func TestCheckAliveRescansBootPrompts(t *testing.T) {
	v := newEngEnv(t)
	ctx := context.Background()
	v.start("code", nil)
	v.e.Wait()
	if l, _ := v.e.ListBootPrompts(ctx, nil); len(l.Prompts) != 0 {
		t.Fatalf("一開始沒有：%+v", l)
	}
	v.tmux.setScreen("code", readScreen(t, "trust-folder.txt"))
	v.e.CheckAlive(ctx)
	if l, _ := v.e.ListBootPrompts(ctx, nil); len(l.Prompts) != 1 {
		t.Fatalf("定期檢查要發現信任框：%+v", l)
	}
	v.tmux.setScreen("code", readScreen(t, "remote-control-active.txt"))
	v.e.CheckAlive(ctx)
	if l, _ := v.e.ListBootPrompts(ctx, nil); len(l.Prompts) != 0 {
		t.Fatalf("信任框被按掉要撤掉：%+v", l)
	}
	// 不在的 session 不看畫面。
	v.tmux.setAlive("code", false)
	before := len(v.tmux.callsOf("capture"))
	v.e.CheckAlive(ctx)
	if got := len(v.tmux.callsOf("capture")); got != before {
		t.Fatalf("session 不在不該抓畫面：%d → %d", before, got)
	}
}
