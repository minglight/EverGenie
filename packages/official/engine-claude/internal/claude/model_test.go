package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 這段對話現在用的模型與力度（ECL-49、D-46）：Claude Code 說了才報，說不出就空著。
// 對話記錄的行照 2.1.316 真的對話記錄的樣子寫（「Set model to `Opus 5.5` …」、assistant 的 message.model）。

func TestModelLabelFromIDsAndAliases(t *testing.T) {
	for in, want := range map[string]string{
		"claude-opus-5-5":            "Opus 5.5",
		"claude-sonnet-5":            "Sonnet 5",
		"claude-haiku-4-5-20251001":  "Haiku 4.5",
		"claude-3-5-sonnet-20241022": "Sonnet 3.5",
		"opus":                       "Opus",
		"opus[1m]":                   "Opus",
		"Sonnet":                     "Sonnet",
		"fable":                      "Fable",
		"":                           "",
		"<synthetic>":                "",
		"gpt-9":                      "",
		"claude-":                    "",
	} {
		if got := modelLabel(in); got != want {
			t.Errorf("modelLabel(%q)=%q，要 %q", in, got, want)
		}
	}
}

func assistantLine(ts time.Time, model string, sidechain bool) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "timestamp": ts, "isSidechain": sidechain, "message": map[string]any{"model": model, "content": []any{map[string]any{"type": "text", "text": "好"}}}})
	return string(b)
}

func stdoutLine(ts time.Time, text string) string {
	b, _ := json.Marshal(map[string]any{"type": "user", "timestamp": ts, "message": map[string]any{"role": "user", "content": "<local-command-stdout>" + text + "</local-command-stdout>"}})
	return string(b)
}

func TestTranscriptModelTakesLastReplySinceLaunch(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	data := strings.Join([]string{
		assistantLine(t0, "claude-haiku-5-5", false),                    // 這一次開之前（接回的舊對話）：不算
		assistantLine(t0.Add(2*time.Minute), "claude-opus-5-5", false),  // 這一次的
		assistantLine(t0.Add(3*time.Minute), "claude-sonnet-5-5", true), // subagent：不算
		assistantLine(t0.Add(4*time.Minute), "<synthetic>", false),      // 平台合成的（額度用完那種）：不算
		`{"type":"assistant","message":{"model":`,                       // 寫到一半的行
	}, "\n") + "\n"
	got := transcriptModel([]byte(data), t0.Add(time.Minute))
	if got.Model != "claude-opus-5-5" || got.Label != "Opus 5.5" {
		t.Fatalf("要最後一則她自己的回話：%+v", got)
	}
	if got := transcriptModel(nil, time.Time{}); got != (modelSeen{}) {
		t.Fatalf("沒有記錄就空著：%+v", got)
	}
}

func TestTranscriptModelSwitchReplyGivesLabelUntilNextReply(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	lines := []string{
		assistantLine(t0, "claude-sonnet-5-5", false),
		stdoutLine(t0.Add(time.Minute), "Set model to `Opus 5.5` and saved as your default for new sessions"),
	}
	got := transcriptModel([]byte(strings.Join(lines, "\n")+"\n"), time.Time{})
	// 換了模型、她還沒回話：代號還不知道，名字照平台的回覆。
	if got.Model != "" || got.Label != "Opus 5.5" {
		t.Fatalf("換模型的回覆：%+v", got)
	}
	got = transcriptModel([]byte(stdoutLine(t0, "Set model to `claude-sonnet-5`")+"\n"), time.Time{})
	if got.Label != "Sonnet 5" {
		t.Fatalf("回覆寫的是代號要換成名字：%+v", got)
	}
}

func TestHookEffortReadsLevelObjectOrString(t *testing.T) {
	for raw, want := range map[string]string{
		`{"level":"medium"}`: "medium",
		`"XHigh"`:            "xhigh",
		`{"level":""}`:       "",
		`{"level":"a b"}`:    "",
		``:                   "",
		`42`:                 "",
	} {
		if got := hookEffort(json.RawMessage(raw)); got != want {
			t.Errorf("hookEffort(%s)=%q，要 %q", raw, got, want)
		}
	}
}

func lastEvent(t *testing.T, h *fakeHost) *sdk.EngineEvent {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.events) == 0 {
		t.Fatal("沒有回報事件")
	}
	return h.events[len(h.events)-1]
}

// 真的 SessionStart 掛勾帶 model（testdata/spike/hooks/SessionStart.json，2.1.309 實錄）：開起來就回報代號與名字。
func TestHookSessionStartReportsModel(t *testing.T) {
	e := newHookEnv(t)
	e.runFixture(t, "SessionStart", "hooks/SessionStart.json")
	ev := lastEvent(t, e.host)
	if ev.Type != sdk.EngineEventTypeSessionStarted || ev.Model != "claude-haiku-5-5" || ev.ModelLabel != "Haiku 5.5" {
		t.Fatalf("SessionStart 要帶模型：%+v", ev)
	}
}

// 真的 Stop 掛勾帶 effort.level（Stop.json 實錄）；這一輪的模型從對話記錄最後一則她的回話讀。
func TestHookStopReportsModelFromTranscriptAndEffort(t *testing.T) {
	e := newHookEnv(t)
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(transcript, []byte(assistantLine(time.Now().UTC(), "claude-opus-5-5", false)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(readFixture(t, "hooks/Stop.json"), &p); err != nil {
		t.Fatal(err)
	}
	p["transcript_path"] = transcript
	raw, _ := json.Marshal(p)
	e.run("Stop", raw)
	ev := lastEvent(t, e.host)
	if ev.Type != sdk.EngineEventTypeTurnEnded || ev.Model != "claude-opus-5-5" || ev.ModelLabel != "Opus 5.5" || ev.Effort != "medium" {
		t.Fatalf("Stop 要帶這一輪的模型與力度：%+v", ev)
	}
	// 工具的掛勾也帶力度（PreToolUse-Bash.json 實錄），沒有模型。
	e.runFixture(t, "PreToolUse", "hooks/PreToolUse-Bash.json")
	ev = lastEvent(t, e.host)
	if ev.Effort != "medium" || ev.Model != "" {
		t.Fatalf("工具掛勾只帶力度：%+v", ev)
	}
	// 沒帶的（UserPromptSubmit 實錄）：都空著，核心不改。
	e.runFixture(t, "UserPromptSubmit", "hooks/UserPromptSubmit.json")
	ev = lastEvent(t, e.host)
	if ev.Effort != "" || ev.Model != "" || ev.ModelLabel != "" {
		t.Fatalf("說不出就空著：%+v", ev)
	}
}

// 她忙著時主人換了模型：這一輪 /model 之前的回話還是舊模型，Stop 不拿它蓋掉主人剛換的；之後的回話才算。
func TestHookStopIgnoresRepliesBeforeModelSwitch(t *testing.T) {
	e := newHookEnv(t)
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	markModelSwitch(e.home, "code", t0.Add(time.Minute))
	transcript := filepath.Join(t.TempDir(), "t.jsonl")
	stop := func(lines ...string) *sdk.EngineEvent {
		t.Helper()
		if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"hook_event_name": "Stop", "transcript_path": transcript})
		e.run("Stop", raw)
		return lastEvent(t, e.host)
	}
	old := assistantLine(t0, "claude-sonnet-5-5", false)
	if ev := stop(old); ev.Type != sdk.EngineEventTypeTurnEnded || ev.Model != "" || ev.ModelLabel != "" {
		t.Fatalf("換模型之前的回話不報：%+v", ev)
	}
	if ev := stop(old, assistantLine(t0.Add(2*time.Minute), "claude-opus-5-5", false)); ev.Model != "claude-opus-5-5" {
		t.Fatalf("換模型之後的回話要報：%+v", ev)
	}
}

// /model 做成了：回別名當代號（真的代號等她下一次回話），名字用平台的回覆；/effort 回這次的等級。
func TestCommandModelReturnsLabelFromPlatformReply(t *testing.T) {
	v := newEngEnv(t)
	v.start("code", nil)
	v.tmux.setScreen("code", readScreen(t, "labeled-rule.txt"))
	s := v.e.transcriptSource("code")
	path := filepath.Join(s.configDir, "projects", projectDirName(s.cwd), s.id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	v.tmux.onKey = func(_, key string) (string, bool) {
		if key == "Enter" {
			line := stdoutLine(time.Now().UTC(), "Set model to `Opus 5.5` and saved as your default for new sessions")
			if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
				t.Error(err)
			}
		}
		return "", false
	}
	r := runCommand(t, v, "code", sdk.SessionCommandModel, "opus")
	if !r.Done || r.Model != "opus" || r.ModelLabel != "Opus 5.5" || r.Effort != "" {
		t.Fatalf("換模型的結果：%+v", r)
	}
	r = runCommand(t, v, "code", sdk.SessionCommandEffort, "high")
	if !r.Done || r.Effort != "high" || r.Model != "" {
		t.Fatalf("換力度的結果：%+v", r)
	}
}

// 開的時候帶了 --model、--effort：啟動結果說出來；沒帶的不說（帳號的預設，開起來才知道）。
func TestStartReportsPassedModelAndEffortOnly(t *testing.T) {
	v := newEngEnv(t)
	r, err := v.e.Start(t.Context(), func() *sdk.EngineStartRequest {
		req := v.req("a")
		req.Model, req.Effort = "sonnet", "high"
		return req
	}())
	if err != nil {
		t.Fatal(err)
	}
	if r.Model != "sonnet" || r.ModelLabel != "Sonnet" || r.Effort != "high" {
		t.Fatalf("帶了的要說：%+v", r)
	}
	req := v.req("b")
	req.Model, req.Effort = "", "turbo"
	r, err = v.e.Start(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model != "" || r.ModelLabel != "" || r.Effort != "" {
		t.Fatalf("沒帶的不說：%+v", r)
	}
}

// Stop 比回話寫進對話記錄還早（真機開起來第一輪會這樣）：等一下再讀，讀到才回；一直沒有就回空的、不會一直等。
func TestStopModelWaitsForReplyToBeWritten(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(assistantLine(t0, "claude-haiku-5-5", false)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	waits := 0
	got := stopModel(path, t0.Add(time.Minute), func(time.Duration) {
		waits++
		if waits == 2 {
			f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
			_, _ = f.WriteString(assistantLine(t0.Add(2*time.Minute), "claude-sonnet-5-5", false) + "\n")
			_ = f.Close()
		}
	})
	if got.Model != "claude-sonnet-5-5" || waits != 2 {
		t.Fatalf("要等到這一輪的回話：%+v（等了 %d 次）", got, waits)
	}
	// 第一次就讀到：不等。
	waits = 0
	if got := stopModel(path, t0.Add(time.Minute), func(time.Duration) { waits++ }); got.Model != "claude-sonnet-5-5" || waits != 0 {
		t.Fatalf("第一次就有回話不用等：%+v（等了 %d 次）", got, waits)
	}
	// 一直沒有這一輪的回話：回空的、等有限次。
	waits = 0
	if got := stopModel(path, t0.Add(time.Hour), func(time.Duration) { waits++ }); got.Model != "" || waits != stopModelTries {
		t.Fatalf("讀不到就回空的、等有限次：%+v（等了 %d 次）", got, waits)
	}
	// 沒有對話記錄檔：不等。
	waits = 0
	if got := stopModel(filepath.Join(t.TempDir(), "none.jsonl"), t0, func(time.Duration) { waits++ }); got.Model != "" || waits != 0 {
		t.Fatalf("沒有檔案直接回空的：%+v（等了 %d 次）", got, waits)
	}
}
