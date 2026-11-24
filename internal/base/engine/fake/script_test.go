package fake

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// recordingHost 記下引擎回報的事件，並用 decide 回答確認。
type recordingHost struct {
	mu     sync.Mutex
	events []sdk.EngineEvent
	ended  chan struct{}
	decide func(*sdk.EngineHostRequestApprovalRequest) *sdk.Decision
	asked  []*sdk.EngineHostRequestApprovalRequest
}

func newRecordingHost() *recordingHost { return &recordingHost{ended: make(chan struct{}, 8)} }

func (h *recordingHost) CheckAction(context.Context, *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	return &sdk.EngineHostCheckActionResponse{Verdict: sdk.ActionVerdictAllow}, nil
}
func (h *recordingHost) ReportEvent(_ context.Context, r *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	h.mu.Lock()
	h.events = append(h.events, *r.Event)
	h.mu.Unlock()
	if r.Event.Type == sdk.EngineEventTypeTurnEnded {
		h.ended <- struct{}{}
	}
	return &sdk.EngineHostReportEventResponse{}, nil
}
func (h *recordingHost) RequestApproval(_ context.Context, r *sdk.EngineHostRequestApprovalRequest) (*sdk.EngineHostRequestApprovalResponse, error) {
	h.mu.Lock()
	h.asked = append(h.asked, r)
	h.mu.Unlock()
	return &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: h.decide(r)}, nil
}
func (h *recordingHost) GetContext(context.Context, *sdk.EngineHostGetContextRequest) (*sdk.EngineHostGetContextResponse, error) {
	return &sdk.EngineHostGetContextResponse{}, nil
}

func (h *recordingHost) types() []sdk.EngineEventType {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []sdk.EngineEventType
	for _, e := range h.events {
		out = append(out, e.Type)
	}
	return out
}

func runScript(t *testing.T, h *recordingHost, text string) []sdk.ChatMessage {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	e := NewScriptedContext(ctx, h)
	if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	out, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: text})
	if err != nil || !out.Delivered {
		t.Fatalf("Deliver = %+v, %v", out, err)
	}
	select {
	case <-h.ended:
	case <-time.After(10 * time.Second):
		t.Fatalf("劇情沒有跑完：%v", h.types())
	}
	var msgs []sdk.ChatMessage
	if err := e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "code"}, func(r *sdk.EngineTranscriptResponse) error {
		msgs = append(msgs, *r.Message)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return msgs
}

// Guards: REQ-144
func TestScriptedTurnEchoes(t *testing.T) {
	h := newRecordingHost()
	msgs := runScript(t, h, "你好 e2e-1")
	want := []sdk.EngineEventType{sdk.EngineEventTypeSessionStarted, sdk.EngineEventTypeTurnStarted, sdk.EngineEventTypeActivityStarted, sdk.EngineEventTypeActivityEnded, sdk.EngineEventTypeTurnEnded}
	got := h.types()
	if len(got) != len(want) {
		t.Fatalf("事件 = %v，要 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("事件 = %v，要 %v", got, want)
		}
	}
	if h.events[1].Input != "你好 e2e-1" || h.events[2].Activity != "Read" || h.events[4].LastMessage != "收到：你好 e2e-1" {
		t.Fatalf("事件內容不對：%+v", h.events)
	}
	kinds := []sdk.ChatKind{sdk.ChatKindUser, sdk.ChatKindToolCall, sdk.ChatKindToolResult, sdk.ChatKindAssistant}
	if len(msgs) != len(kinds) || msgs[3].Text != "收到：你好 e2e-1" || msgs[1].Tool != "Read" || msgs[1].CallID == "" || msgs[1].CallID != msgs[2].CallID {
		t.Fatalf("對話記錄 = %+v", msgs)
	}
	for i, k := range kinds {
		if msgs[i].Kind != k {
			t.Fatalf("對話記錄 = %+v", msgs)
		}
	}
}

// 假引擎的工具呼叫也帶白話（SES-65）；【多工具】多用幾種、有的很多次，還有一個沒有白話的（看板退回原名，WEB-126）。
func TestScriptedToolCallsCarryPlainLabel(t *testing.T) {
	msgs := runScript(t, newRecordingHost(), "你好")
	if msgs[1].Kind != sdk.ChatKindToolCall || msgs[1].ToolLabel != "讀檔" || msgs[2].ToolLabel != "" {
		t.Fatalf("工具呼叫要帶白話、結果不帶：%+v", msgs[1:3])
	}
	msgs = runScript(t, newRecordingHost(), "做很多事 【多工具】")
	count := map[string]int{}
	for _, m := range msgs {
		if m.Kind == sdk.ChatKindToolCall {
			count[m.Tool+"="+m.ToolLabel]++
		}
	}
	want := map[string]int{"Read=讀檔": 2, "Bash=跑指令": 12, "Grep=找內容": 2, "OldTool=": 1}
	if fmt.Sprint(count) != fmt.Sprint(want) {
		t.Fatalf("工具呼叫 = %v，要 %v", count, want)
	}
	if got := msgs[len(msgs)-1]; got.Kind != sdk.ChatKindAssistant {
		t.Fatalf("最後要有回覆：%+v", got)
	}
}

// Guards: REQ-144
func TestScriptedPermissionAndQuestion(t *testing.T) {
	h := newRecordingHost()
	h.decide = func(*sdk.EngineHostRequestApprovalRequest) *sdk.Decision {
		return &sdk.Decision{Allow: false, Message: "不要"}
	}
	msgs := runScript(t, h, "請做 【權限】")
	if got := msgs[len(msgs)-1].Text; got != "已拒絕：不要" {
		t.Fatalf("回覆 = %q", got)
	}
	if a := h.asked[0]; a.Kind != sdk.ApprovalKindPermission || a.Tool != "Bash" || a.Summary != "建立檔案 e2e.txt" {
		t.Fatalf("權限請求 = %+v", a)
	}

	h = newRecordingHost()
	h.decide = func(r *sdk.EngineHostRequestApprovalRequest) *sdk.Decision {
		return &sdk.Decision{Answers: map[string]string{r.Questions[0].Text: "B"}}
	}
	msgs = runScript(t, h, "請問 【選擇】")
	if got := msgs[len(msgs)-1].Text; got != "你選了 B" {
		t.Fatalf("回覆 = %q", got)
	}
	if q := h.asked[0].Questions[0]; q.Text != "要哪一個？" || len(q.Options) != 2 {
		t.Fatalf("問題 = %+v", q)
	}
}

// 文字含「【出錯】」：這一輪報 TURN_FAILED（overloaded＋假的平台原話），不做事、沒有 TURN_ENDED；
// 「【出錯:auth】」可以指定種類（S5-0）。
func TestScriptedFailReportsTurnFailed(t *testing.T) {
	for text, want := range map[string]sdk.EngineErrorKind{"請做 【出錯】": sdk.EngineErrorKindOverloaded, "請做 【出錯:auth】": sdk.EngineErrorKindAuth, "請做 【出錯：billing】": sdk.EngineErrorKindBilling} {
		h := newRecordingHost()
		ctx, cancel := context.WithCancel(context.Background())
		e := NewScriptedContext(ctx, h)
		if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: "code"}); err != nil {
			t.Fatal(err)
		}
		if out, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: text}); err != nil || !out.Delivered {
			t.Fatalf("Deliver = %+v, %v", out, err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for len(h.types()) < 3 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(fastActivity + 100*time.Millisecond) // 確定後面沒有別的事件（正常一輪 300ms 內就會有 ACTIVITY／TURN_ENDED）
		cancel()
		got := h.types()
		if len(got) != 3 || got[1] != sdk.EngineEventTypeTurnStarted || got[2] != sdk.EngineEventTypeTurnFailed {
			t.Fatalf("%s：事件 = %v，要 session_started、turn_started、turn_failed", text, got)
		}
		h.mu.Lock()
		ev := h.events[2]
		h.mu.Unlock()
		if ev.Error != want || ev.LastMessage == "" || ev.Session != "code" {
			t.Fatalf("%s：TURN_FAILED = %+v", text, ev)
		}
	}
}

// 文字含「【登出】」：這一輪開始後報 SESSION_ENDED（reason＝帳號登出），session 跟著不在，之後的話回 gone。
func TestScriptedLogoutEndsSession(t *testing.T) {
	h := newRecordingHost()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := NewScriptedContext(ctx, h)
	if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	if out, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "請做 【登出】"}); err != nil || !out.Delivered {
		t.Fatalf("Deliver = %+v, %v", out, err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(h.types()) < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := h.types()
	if len(got) != 3 || got[2] != sdk.EngineEventTypeSessionEnded {
		t.Fatalf("事件 = %v，要 session_started、turn_started、session_ended", got)
	}
	h.mu.Lock()
	ev := h.events[2]
	h.mu.Unlock()
	if ev.Reason != sdk.SessionEndReasonLoggedOut {
		t.Fatalf("SESSION_ENDED = %+v", ev)
	}
	if out, _ := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "還在嗎"}); out == nil || out.Reason != "gone" {
		t.Fatalf("登出之後 session 不在：%+v", out)
	}
}

// 假引擎也有 resume：接回時回同一個 session id，Capabilities 說有（P7-5、D-33）。
// Guards: REQ-219
func TestFakeResumeKeepsSessionID(t *testing.T) {
	e := New(nil)
	ctx := context.Background()
	caps, err := e.Capabilities(ctx, &sdk.EngineCapabilitiesRequest{})
	if err != nil || !caps.Features["resume"] {
		t.Fatalf("%+v %v", caps, err)
	}
	fresh, err := e.Start(ctx, &sdk.EngineStartRequest{Name: "code-sec-1"})
	if err != nil || fresh.EngineSessionID != "fake-code-sec-1" {
		t.Fatalf("%+v %v", fresh, err)
	}
	back, err := e.Start(ctx, &sdk.EngineStartRequest{Name: "code-sec-1", ResumeSessionID: "11111111-2222-4333-8444-555555555555"})
	if err != nil || back.EngineSessionID != "11111111-2222-4333-8444-555555555555" {
		t.Fatalf("接回要回同一個 id：%+v %v", back, err)
	}
}

// 「【不回】」之後引擎整個不回應：那一句的 Deliver 與之後的呼叫都卡著，呼叫的人放棄（ctx 到期）才回錯誤。
func TestScriptedStuckHoldsEveryCall(t *testing.T) {
	h := newRecordingHost()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := NewScriptedContext(ctx, h)
	if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: "code"}); err != nil {
		t.Fatal(err)
	}
	short := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, 50*time.Millisecond)
	}
	call, done := short()
	if out, err := e.Deliver(call, &sdk.EngineDeliverRequest{Session: "code", Text: "【不回】"}); err == nil {
		t.Fatalf("卡住的那一句不該回：%+v", out)
	}
	done()
	call, done = short()
	if out, err := e.ListBootPrompts(call, &sdk.EngineListBootPromptsRequest{Session: "code"}); err == nil {
		t.Fatalf("卡住之後 ListBootPrompts 不該回：%+v", out)
	}
	done()
	call, done = short()
	err := e.Transcript(call, &sdk.EngineTranscriptRequest{Session: "code"}, func(*sdk.EngineTranscriptResponse) error { return nil })
	done()
	if err == nil {
		t.Fatal("卡住之後 Transcript 不該回")
	}
	call, done = short()
	if out, err := e.Command(call, &sdk.EngineCommandRequest{Session: "code", Command: sdk.SessionCommandInterrupt}); err == nil {
		t.Fatalf("卡住之後 Command 不該回：%+v", out)
	}
	done()
}

// 「【這位不回】」只卡收到這句的那個 session：她的 Deliver 與之後的呼叫都卡著，別的 session 照常收話。
func TestScriptedStuckOneHoldsOnlyThatSession(t *testing.T) {
	h := newRecordingHost()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e := NewScriptedContext(ctx, h)
	for _, name := range []string{"code", "life"} {
		if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	short := func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(ctx, 50*time.Millisecond)
	}
	call, done := short()
	if out, err := e.Deliver(call, &sdk.EngineDeliverRequest{Session: "code", Text: "【這位不回】"}); err == nil {
		t.Fatalf("卡住的那一句不該回：%+v", out)
	}
	done()
	call, done = short()
	if _, err := e.Stop(call, &sdk.EngineStopRequest{Name: "code"}); err == nil {
		t.Fatal("卡住之後她的 Stop 不該回")
	}
	done()
	call, done = short()
	err := e.Transcript(call, &sdk.EngineTranscriptRequest{Session: "code"}, func(*sdk.EngineTranscriptResponse) error { return nil })
	done()
	if err == nil {
		t.Fatal("卡住之後她的 Transcript 不該回")
	}
	call, done = short()
	defer done()
	out, err := e.Deliver(call, &sdk.EngineDeliverRequest{Session: "life", Text: "給另一位"})
	if err != nil || !out.Delivered {
		t.Fatalf("別的 session 要照常收話：%+v %v", out, err)
	}
}

// SlowSession：那個 session 的呼叫拖一段時間才回失敗；LandLate：Deliver 等呼叫的人放棄之後才真的送進去，
// 同一個封包編號再交來時（Recheck）回「已經在了」，對話裡只有一則。
func TestSlowSessionAndLateLanding(t *testing.T) {
	ctx := context.Background()
	e := New(newRecordingHost())
	for _, name := range []string{"code", "work"} {
		if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
	}
	e.SlowSession("code", 80*time.Millisecond)
	start := time.Now()
	if _, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", Text: "慢"}); err == nil || time.Since(start) < 80*time.Millisecond {
		t.Fatalf("要拖過了才回失敗：%v（%s）", err, time.Since(start))
	}
	start = time.Now()
	if _, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "work", Text: "快"}); err != nil || time.Since(start) > 50*time.Millisecond {
		t.Fatalf("別的 session 不受影響：%v（%s）", err, time.Since(start))
	}
	e.SlowSession("code", 0)

	e.LandLate("code", true)
	call, done := context.WithTimeout(ctx, 50*time.Millisecond)
	_, err := e.Deliver(call, &sdk.EngineDeliverRequest{Session: "code", EnvelopeID: "env-1", Text: "晚到"})
	done()
	if err == nil {
		t.Fatal("呼叫的人放棄時要回錯誤")
	}
	e.LandLate("code", false)
	e.Configure(Behavior{Delivered: true, Recheck: true})
	out, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "code", EnvelopeID: "env-1", Text: "晚到"})
	if err != nil || !out.Delivered || out.Reason != sdk.DeliverAlready {
		t.Fatalf("再交一次要說已經在了：%+v %v", out, err)
	}
	n := 0
	err = e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "code"}, func(r *sdk.EngineTranscriptResponse) error {
		if r.Message != nil && r.Message.Text == "晚到" {
			n++
		}
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("對話裡要剛好一則：%d %v", n, err)
	}
}
