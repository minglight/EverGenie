package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 額度用完的樣本（ECL-38）：testdata/limit/transcript-usage-limit.jsonl 最後一行是 2026-10-30 真實的對話記錄原文；
// 前面幾行裡主人的話換成通用的字；StopFailure-*.json 是平台 2.1.314 StopFailure hook 的輸入格式（共同欄位＋error、last_assistant_message）。
const limitDir = "../../testdata/limit"

// 真實樣本的恢復時間：rate_limit_info.resetsAt = 1793354400（2026-10-30 18:00 Asia/Taipei）。
var sampleReset = time.Unix(1793354400, 0)

func taipei(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Taipei")
	if err != nil {
		t.Skip("沒有時區資料")
	}
	return loc
}

// limitPayload 讀 testdata/limit 的 hook 輸入，transcript_path 換成 path（空字串＝讀不到）。
func limitPayload(t *testing.T, file, path string) []byte {
	t.Helper()
	data := readFixture(t, filepath.Join("..", "limit", file))
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["transcript_path"] = path
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sampleTranscript(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join(limitDir, "transcript-usage-limit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func runAt(e *hookEnv, event string, payload []byte, now time.Time) {
	d := e.deps()
	d.Now = func() time.Time { return now }
	var out bytes.Buffer
	RunHook(context.Background(), event, bytes.NewReader(payload), &out, d)
}

// Guards: REQ-378
func TestLimitTextAndResetFormats(t *testing.T) {
	loc := taipei(t)
	at := time.Date(2026, 10, 30, 16, 48, 0, 0, loc)
	real := "You've hit your 5-hour limit · resets 6pm (Asia/Taipei)"
	if !isLimitText(real) {
		t.Fatalf("認不出真實的那一行：%q", real)
	}
	for _, not := range []string{"", "我快到 5-hour limit 了，先停一下", "The limit is 10 items."} {
		if isLimitText(not) {
			t.Errorf("%q 不是平台的額度訊息", not)
		}
	}
	cases := []struct {
		text string
		want time.Time
	}{
		{real, time.Date(2026, 10, 30, 18, 0, 0, 0, loc)},
		{"You've hit your 5-hour limit · resets 6:30pm (Asia/Taipei)", time.Date(2026, 10, 30, 18, 30, 0, 0, loc)},
		{"You've hit your weekly limit · resets Nov 3, 9am (Asia/Taipei)", time.Date(2026, 11, 3, 9, 0, 0, 0, loc)},
		{"You've hit your 5-hour limit · resets 1am (Asia/Taipei)", time.Date(2026, 10, 31, 1, 0, 0, 0, loc)}, // 已經過了就是明天
		{"Claude AI usage limit reached|1793354400", sampleReset},
		{"You've hit your 5-hour limit", time.Time{}},
	}
	for _, c := range cases {
		if got := parseReset(c.text, at); !got.Equal(c.want) {
			t.Errorf("parseReset(%q) = %v，要 %v", c.text, got, c.want)
		}
	}
}

// Guards: REQ-378
func TestTranscriptLimitRealSample(t *testing.T) {
	seen := transcriptLimit(sampleTranscript(t))
	if !seen.Limited || !seen.Until.Equal(sampleReset) {
		t.Fatalf("真實樣本 = %+v，要額度用完、%v 恢復", seen, sampleReset)
	}
	if got := transcriptLimit(filepath.Join(t.TempDir(), "none.jsonl")); got.Limited {
		t.Fatalf("讀不到的記錄不算：%+v", got)
	}
	// 最後一則是一般回覆（之後恢復了）就不算。
	if got := transcriptLimit(filepath.Join(spikeDir, "transcript.jsonl")); got.Limited {
		t.Fatalf("一般的對話記錄不算：%+v", got)
	}
}

// systemLine 造一行平台的 system／informational（2.1.316 真機格式）。
func systemLine(text string, at time.Time) string {
	return `{"isSidechain":false,"type":"system","subtype":"informational","content":"` + text + `","isMeta":false,"timestamp":"` +
		at.UTC().Format(time.RFC3339Nano) + `","level":"notice","userType":"external","entrypoint":"cli"}`
}

// 平台說「恢復後自己接著做」的那一行（不是每次撞額度都有）與取消的那一行。
const (
	autoResumeText = "Usage limit reached · continuing automatically at 6pm · esc to cancel"
	autoCancelText = "Automatic continue cancelled · Claude Code relaunched during the wait"
)

// writeTranscript 寫一份對話記錄到暫存檔，回傳路徑。
func writeTranscript(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 撞額度之後平台有沒有說會自己接著做（DEV-102）：沒說的（真實樣本）恢復後不會動；說了的才等它；說了又取消的不算；
// 恢復時平台補的 meta 那句（「Your claude.ai usage limit has reset…」）不影響判斷。撞額度的是哪一句也要認得。
func TestTranscriptLimitAutoResumeMarker(t *testing.T) {
	data := string(readFixture(t, filepath.Join("..", "limit", "transcript-usage-limit.jsonl")))
	at := time.Date(2026, 10, 30, 9, 3, 29, 0, time.UTC)
	seen := transcriptLimit(sampleTranscript(t))
	if !seen.Limited || seen.AutoResume || seen.Prompt != "［來自 code］幫我看一下備份" {
		t.Fatalf("沒有那一行就不算會自己接著做，且認得撞額度的那句：%+v", seen)
	}
	resumed := data + systemLine(autoResumeText, at) + "\n" + `{"type":"system","subtype":"turn_duration","durationMs":12}` + "\n"
	if seen := transcriptLimit(writeTranscript(t, resumed)); !seen.Limited || !seen.AutoResume || !seen.AutoResumeAt.Equal(at) || !seen.Until.Equal(sampleReset) {
		t.Fatalf("額度用完之後有 continuing automatically 要認出來：%+v", seen)
	}
	resetting := resumed + systemLine("Usage limit reset · Continuing automatically", at.Add(time.Hour)) + "\n" +
		`{"type":"user","isMeta":true,"timestamp":"2026-10-30T10:03:30Z","message":{"role":"user","content":"Your claude.ai usage limit has reset. Continue the task you were working on when the limit was reached; do not repeat work that is already complete."}}` + "\n"
	if seen := transcriptLimit(writeTranscript(t, resetting)); !seen.Limited || !seen.AutoResume {
		t.Fatalf("平台正要接著做（meta 那句）：照樣算會自己接著做：%+v", seen)
	}
	cancelled := resumed + systemLine(autoCancelText, at.Add(time.Minute)) + "\n"
	if seen := transcriptLimit(writeTranscript(t, cancelled)); !seen.Limited || seen.AutoResume {
		t.Fatalf("取消了就不算會自己接著做：%+v", seen)
	}
	// 只認 informational：別的 system 行剛好含這句不算。
	other := data + `{"type":"system","subtype":"local_command","content":"continuing automatically","timestamp":"2026-10-30T09:03:29Z"}` + "\n"
	if seen := transcriptLimit(writeTranscript(t, other)); seen.AutoResume {
		t.Fatalf("不是 informational 的不算：%+v", seen)
	}
}

// 額度恢復後核心補送同一則（DEV-102）：那句在對話記錄裡、但那一輪停在額度用完而平台沒說會自己接著做 → 照常重貼並送出，
// 不回 already（回了核心會一直當她在忙，2026-11-02 曾經因此卡了 16 小時）；平台說了會自己接著做（在這次啟動之後）→ already、不貼不按。
func TestDeliverRecheckRepastesWhenStrandedByLimit(t *testing.T) {
	sample := bytes.Split(bytes.TrimSpace(readFixture(t, filepath.Join("..", "limit", "transcript-usage-limit.jsonl"))), []byte("\n"))
	limitLine := string(sample[len(sample)-1]) // 平台合成的額度用完那一則
	launchAt := time.Now().Add(-time.Hour)
	for _, c := range []struct {
		name    string
		extra   string // 額度那一則之後的 system 行
		later   bool   // 撞額度的是後來別的一句
		repaste bool
	}{
		{name: "平台不會自己接著做", repaste: true},
		{name: "平台會自己接著做", extra: systemLine(autoResumeText, launchAt.Add(time.Minute))},
		{name: "會自己接著做但被取消了", extra: systemLine(autoResumeText, launchAt.Add(time.Minute)) + "\n" + systemLine(autoCancelText, launchAt.Add(2*time.Minute)), repaste: true},
		{name: "會自己接著做的記號早於這次啟動", extra: systemLine(autoResumeText, launchAt.Add(-time.Minute)), repaste: true},
		{name: "撞額度的是後來別的一句", later: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := newEngEnv(t)
			v.start("code", nil)
			if err := os.Chtimes(launchFilePath(v.home, "code"), launchAt, launchAt); err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			path := v.transcriptFile(t)
			text := "［來自 code］主資料夾收好了"
			deliver := func() *sdk.EngineDeliverResponse {
				t.Helper()
				r, err := v.e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: text, EnvelopeID: "E-1"})
				if err != nil {
					t.Fatal(err)
				}
				return r
			}
			deliver()
			ts := v.clock.Now().UTC().Format(time.RFC3339Nano)
			user := func(uuid, s string) string {
				return `{"type":"user","uuid":"` + uuid + `","timestamp":"` + ts + `","message":{"role":"user","content":"` + s + `"}}` + "\n"
			}
			transcript := user("u1", text)
			if c.later {
				transcript += `{"type":"assistant","timestamp":"` + ts + `","message":{"role":"assistant","content":[{"type":"text","text":"好"}]}}` + "\n" + user("u2", "另一件事")
			}
			transcript += limitLine + "\n"
			if c.extra != "" {
				transcript += c.extra + "\n"
			}
			if err := os.WriteFile(path, []byte(transcript), 0o644); err != nil {
				t.Fatal(err)
			}
			pastes := len(v.tmux.callsOf("paste"))
			r := deliver()
			repasted := len(v.tmux.callsOf("paste")) - pastes
			if !c.repaste {
				if !r.Delivered || r.Reason != sdk.DeliverAlready || repasted != 0 {
					t.Fatalf("要 already、不重貼：%+v 重貼 %d 次", r, repasted)
				}
				return
			}
			if !r.Delivered || r.Reason == sdk.DeliverAlready || repasted != 1 {
				t.Fatalf("停在額度用完、平台不會動：要重貼一次並送出：%+v 重貼 %d 次", r, repasted)
			}
		})
	}
}

// 額度用完報 RATE_LIMITED（ECL-38）；過載報 TURN_FAILED（ECL-41）。
// Guards: REQ-378
func TestStopFailureReportsRateLimited(t *testing.T) {
	// 有對話記錄：恢復時間取 rate_limit_info.resetsAt。
	e := newHookEnv(t)
	e.run("StopFailure", limitPayload(t, "StopFailure-rate_limit.json", sampleTranscript(t)))
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeRateLimited || !e.host.events[0].Until.Equal(sampleReset) {
		t.Fatalf("事件 = %+v，要一筆 RATE_LIMITED、until %v", e.host.events, sampleReset)
	}
	// 讀不到對話記錄：從那一行認（fixedNow 是台北 10/24 20:00，下一個 6pm 是 10/25 18:00）。
	e = newHookEnv(t)
	e.run("StopFailure", limitPayload(t, "StopFailure-rate_limit.json", ""))
	want := time.Date(2026, 10, 25, 18, 0, 0, 0, taipei(t))
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeRateLimited || !e.host.events[0].Until.Equal(want) {
		t.Fatalf("事件 = %+v，要 RATE_LIMITED、until %v", e.host.events, want)
	}
	// 過載（真機格式的樣本）報 TURN_FAILED、種類 overloaded、原話是平台那一句；不是額度用完（ECL-41）。
	e = newHookEnv(t)
	out, code := e.run("StopFailure", limitPayload(t, "StopFailure-overloaded.json", ""))
	if out != "" || code != 0 || len(e.host.events) != 1 {
		t.Fatalf("過載要報一筆事件、不印：out=%q code=%d events=%+v", out, code, e.host.events)
	}
	if ev := e.host.events[0]; ev.Type != sdk.EngineEventTypeTurnFailed || ev.Error != sdk.EngineErrorKindOverloaded || ev.LastMessage != "API Error: Repeated 529 Overloaded errors" || !ev.Until.IsZero() {
		t.Fatalf("過載要報 TURN_FAILED{overloaded, 平台原話}：%+v", ev)
	}
}

// 其他 API 錯誤（ECL-41）：照對照表報 TURN_FAILED 的種類，平台原話放 last_message，空的填白話。
// testdata/failure/ 的樣本是照 StopFailure-overloaded.json（平台 2.1.314 真機格式）的欄位造的，error 與原話換成各種類。
func TestStopFailureReportsTurnFailedKinds(t *testing.T) {
	cases := []struct {
		file string
		kind sdk.EngineErrorKind
		msg  string
	}{
		{"../failure/StopFailure-authentication_failed.json", sdk.EngineErrorKindAuth, "Invalid API key · Please run /login"},
		{"../failure/StopFailure-billing_error.json", sdk.EngineErrorKindBilling, "Credit balance is too low"},
		{"../failure/StopFailure-server_error.json", sdk.EngineErrorKindOverloaded, "API Error: 500 Internal server error"},
		{"../failure/StopFailure-invalid_request.json", sdk.EngineErrorKindRejected, "Prompt is too long"},
		// 回覆太長被截斷：再說一次通常能接著做，不歸 rejected。
		{"../failure/StopFailure-max_output_tokens.json", sdk.EngineErrorKindUnknown, "API Error: Claude's response exceeded the 32000 output token maximum."},
		{"../failure/StopFailure-unknown-empty.json", sdk.EngineErrorKindUnknown, sdk.EngineNoPlatformMessage},
	}
	for _, c := range cases {
		e := newHookEnv(t)
		// transcript_path 換成讀不到的（樣本裡是真機的路徑，這台電腦上剛好有那個檔、最後一則是額度用完，測試就會看機器而變）。
		payload := limitPayload(t, filepath.Join("..", "failure", filepath.Base(c.file)), "")
		if out, code := e.run("StopFailure", payload); out != "" || code != 0 {
			t.Fatalf("%s：不印、exit 0：out=%q code=%d", c.file, out, code)
		}
		if len(e.host.events) != 1 {
			t.Fatalf("%s：要一筆事件：%+v", c.file, e.host.events)
		}
		if ev := e.host.events[0]; ev.Type != sdk.EngineEventTypeTurnFailed || ev.Error != c.kind || ev.LastMessage != c.msg || ev.Session != "code" {
			t.Errorf("%s：事件 = %+v，要 TURN_FAILED{%s, %q}", c.file, ev, c.kind, c.msg)
		}
	}
	// 對照表的每一個平台值；沒看過的值當 unknown；空的原話依種類填白話。
	table := map[string]sdk.EngineErrorKind{
		"authentication_failed": sdk.EngineErrorKindAuth, "oauth_org_not_allowed": sdk.EngineErrorKindAuth, "account_on_hold": sdk.EngineErrorKindAuth,
		"cloud_credential_error": sdk.EngineErrorKindAuth, "verification_required": sdk.EngineErrorKindAuth, "billing_error": sdk.EngineErrorKindBilling,
		"overloaded": sdk.EngineErrorKindOverloaded, "server_error": sdk.EngineErrorKindOverloaded, "invalid_request": sdk.EngineErrorKindRejected,
		"max_output_tokens": sdk.EngineErrorKindUnknown, "unknown": sdk.EngineErrorKindUnknown, "": sdk.EngineErrorKindUnknown, "brand_new_error": sdk.EngineErrorKindUnknown,
	}
	for platform, want := range table {
		e := newHookEnv(t)
		payload, _ := json.Marshal(map[string]string{"hook_event_name": "StopFailure", "error": platform})
		e.run("StopFailure", payload)
		if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeTurnFailed || e.host.events[0].Error != want {
			t.Errorf("error=%q：事件 = %+v，要 TURN_FAILED{%s}", platform, e.host.events, want)
			continue
		}
		if msg := e.host.events[0].LastMessage; msg == "" {
			t.Errorf("error=%q：沒有原話要填一句白話", platform)
		}
	}
	// 額度用完的那一行（error 不是 rate_limit 也一樣）照舊報 RATE_LIMITED，不報 TURN_FAILED。
	e := newHookEnv(t)
	payload, _ := json.Marshal(map[string]string{"hook_event_name": "StopFailure", "error": "unknown", "last_assistant_message": "You've hit your 5-hour limit · resets 6pm (Asia/Taipei)"})
	e.run("StopFailure", payload)
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeRateLimited {
		t.Fatalf("額度用完那一行要報 RATE_LIMITED：%+v", e.host.events)
	}
	// error 與最後一句都看不出來，但對話記錄最後一則是還沒恢復的額度用完 → RATE_LIMITED，不先報一則「平台出錯」。
	e = newHookEnv(t)
	payload, _ = json.Marshal(map[string]string{"hook_event_name": "StopFailure", "error": "unknown", "transcript_path": sampleTranscript(t)})
	e.run("StopFailure", payload)
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeRateLimited || !e.host.events[0].Until.Equal(sampleReset) {
		t.Fatalf("對話記錄是額度用完要報 RATE_LIMITED：%+v", e.host.events)
	}
}

// Guards: REQ-378
func TestIdlePromptAfterLimit(t *testing.T) {
	payload := func(path string) []byte {
		data := readFixture(t, "hooks/Notification-idle_prompt.json")
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		m["transcript_path"] = path
		out, _ := json.Marshal(m)
		return out
	}
	// 還沒到恢復時間：平台照樣提示閒著，回報的是額度用完（StopFailure 沒接上時的備援）。
	e := newHookEnv(t)
	runAt(e, "Notification", payload(sampleTranscript(t)), sampleReset.Add(-time.Hour))
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeRateLimited || !e.host.events[0].Until.Equal(sampleReset) {
		t.Fatalf("恢復前 = %+v，要 RATE_LIMITED", e.host.events)
	}
	// 過了恢復時間：照常是閒著。
	e = newHookEnv(t)
	runAt(e, "Notification", payload(sampleTranscript(t)), sampleReset.Add(time.Minute))
	if len(e.host.events) != 1 || e.host.events[0].Type != sdk.EngineEventTypeIdleHint {
		t.Fatalf("恢復後 = %+v，要 IDLE_HINT", e.host.events)
	}
}

// 額度用完前剛好壓縮過上下文：補貼用的「主人最後那句話」要是主人真的說的話，不是壓縮接續摘要（OF-16）。
func TestPromptBeforeSkipsCompactSummary(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"user","uuid":"u1","message":{"role":"user","content":"幫我整理看板"}}`),
		[]byte(`{"type":"user","uuid":"u2","isCompactSummary":true,"isVisibleInTranscriptOnly":true,"message":{"role":"user","content":"This session is being continued from a previous conversation..."}}`),
		[]byte(`{"type":"assistant","uuid":"a1","message":{"role":"assistant","content":[{"type":"text","text":"做到一半"}]}}`),
	}
	if got := promptBefore(lines); got != "幫我整理看板" {
		t.Fatalf("promptBefore = %q，要是主人那句話", got)
	}
}
