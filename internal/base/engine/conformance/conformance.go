// Package conformance supplies the shared engine/host contract scenario.
package conformance

import (
	"context"
	"github.com/minglight/evergenie/sdk"
	"sync"
	"testing"
	"time"
)

// Text is deliberately multiline: transports must preserve it exactly.
const Text = "一致性測試：第一行\n第二行，保留空白  與符號。"

// Host is mounted by the process-engine harness, or passed directly to the fake.
// NewHost registers assertions for the reverse calls that EngineAPI cannot expose.
type Host struct {
	Events    chan sdk.EngineEvent
	mu        sync.Mutex
	approvals map[sdk.ApprovalKind]int
	actions   int
}

func NewHost(t *testing.T) *Host {
	t.Helper()
	h := &Host{Events: make(chan sdk.EngineEvent, 64), approvals: map[sdk.ApprovalKind]int{}}
	t.Cleanup(func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.approvals[sdk.ApprovalKindPermission] == 0 || h.approvals[sdk.ApprovalKindQuestion] == 0 || h.actions == 0 {
			t.Error("engine did not exercise permission, question and action checks")
		}
	})
	return h
}

// NewEventHost 只收事件、不檢查反向呼叫（給 TurnFailed 這種單一事件的案例用）。
func NewEventHost() *Host {
	return &Host{Events: make(chan sdk.EngineEvent, 64), approvals: map[sdk.ApprovalKind]int{}}
}

func (h *Host) ReportEvent(ctx context.Context, r *sdk.EngineHostReportEventRequest) (*sdk.EngineHostReportEventResponse, error) {
	select {
	case h.Events <- *r.Event:
		return &sdk.EngineHostReportEventResponse{}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (h *Host) RequestApproval(ctx context.Context, r *sdk.EngineHostRequestApprovalRequest) (*sdk.EngineHostRequestApprovalResponse, error) {
	h.mu.Lock()
	h.approvals[r.Kind]++
	h.mu.Unlock()
	d := &sdk.Decision{Allow: true, Answers: map[string]string{}}
	for _, q := range r.Questions {
		d.Answers[q.Text] = "測試答案"
	}
	return &sdk.EngineHostRequestApprovalResponse{Decided: true, Decision: d}, ctx.Err()
}
func (h *Host) GetContext(ctx context.Context, r *sdk.EngineHostGetContextRequest) (*sdk.EngineHostGetContextResponse, error) {
	return &sdk.EngineHostGetContextResponse{Text: "測試現況"}, ctx.Err()
}
func (h *Host) CheckAction(ctx context.Context, r *sdk.EngineHostCheckActionRequest) (*sdk.EngineHostCheckActionResponse, error) {
	h.mu.Lock()
	h.actions++
	h.mu.Unlock()
	return &sdk.EngineHostCheckActionResponse{Verdict: sdk.ActionVerdictAsk, Reason: "測試確認理由"}, ctx.Err()
}

// Run checks the forward calls and event stream. The harness supplies configured
// account/start defaults and a scenario that triggers NewHost's reverse calls.
func Run(t *testing.T, e sdk.EngineAPI, events <-chan sdk.EngineEvent) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	caps, err := e.Capabilities(ctx, &sdk.EngineCapabilitiesRequest{})
	if err != nil || caps == nil {
		t.Fatalf("capabilities: %v", err)
	}
	// DryRun 不寫任何東西：不能回報「寫了哪些檔」（Artifacts 要空），Drift 是白話字串（有沒有內容看引擎的狀態，不限定）。
	dry, err := e.SetPermission(ctx, &sdk.EngineSetPermissionRequest{Account: "conformance-dry-run", Policy: &sdk.PermissionPolicy{}, DryRun: true})
	if err != nil || dry == nil || len(dry.Artifacts) != 0 {
		t.Fatalf("set_permission dry_run must not write artifacts: %v %v", dry, err)
	}
	stoppedSessions := map[string]bool{}
	for _, name := range []string{"conformance-peer", "conformance"} {
		instructions := "收到別的對話傳話後回覆收到。"
		if name == "conformance" {
			instructions = "收到一致性測試文字時：先嘗試在目前資料夾寫入 probe.txt，讓主人確認；再用提問工具問主人『選哪個？』；然後直接傳話給 conformance-peer，請它回覆收到。不要只用文字模擬操作。"
		}
		started, err := e.Start(ctx, &sdk.EngineStartRequest{Name: name, Agent: "test", Cwd: t.TempDir(), Instructions: instructions})
		if err != nil || started == nil || started.EngineSessionID == "" {
			t.Fatalf("start: %v", err)
		}
		session := name
		t.Cleanup(func() {
			if !stoppedSessions[session] {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if _, err := e.Stop(ctx, &sdk.EngineStopRequest{Name: session}); err != nil {
					t.Errorf("cleanup session: %v", err)
				}
			}
		})
	}
	r, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: "conformance", EnvelopeID: "envelope-1", Text: Text})
	if err != nil || r == nil || !r.Delivered {
		t.Fatalf("deliver: %v %v", r, err)
	}
	typed, peer, direct, ended := false, false, false, false
	for !typed || !peer || !direct || !ended {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.Type == sdk.EngineEventTypeTurnStarted && ev.Source == sdk.InputSourceTyped && ev.Input == Text {
				typed = true
			}
			if ev.Type == sdk.EngineEventTypeTurnStarted && ev.Source == sdk.InputSourcePeer && ev.From != "" && ev.Input != "" {
				peer = true
			}
			if ev.Type == sdk.EngineEventTypeActivityStarted && ev.DirectMessage != nil && ev.DirectMessage.To != "" && ev.DirectMessage.Text != "" {
				direct = true
			}
			if ev.Type == sdk.EngineEventTypeActivityEnded && ev.DirectMessage != nil {
				ended = true
			}
		case <-ctx.Done():
			t.Fatal("missing typed/peer/direct-message events")
		}
	}
	found := false
	err = e.Transcript(ctx, &sdk.EngineTranscriptRequest{Session: "conformance"}, func(r *sdk.EngineTranscriptResponse) error {
		if r.Message != nil && r.Message.Text == Text {
			found = true
		}
		return nil
	})
	if err != nil || !found {
		t.Fatalf("transcript must preserve delivered text: %v", err)
	}
	for _, name := range []string{"conformance", "conformance-peer"} {
		if _, err = e.Stop(ctx, &sdk.EngineStopRequest{Name: name}); err != nil {
			t.Fatal(err)
		}
		stoppedSessions[name] = true
	}
	stopped := map[string]bool{}
	for len(stopped) < 2 {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.Type == sdk.EngineEventTypeSessionEnded {
				stopped[ev.Session] = true
			}
		case <-ctx.Done():
			t.Fatal("stop events timed out")
		}
	}
	if caps.Features["ask"] {
		askCase(t, ctx, e, events)
	}
}

// askCase：一次性對話回非空的文字；不開 session、不回報任何事件（P6-5b）。
func askCase(t *testing.T, ctx context.Context, e sdk.EngineAPI, events <-chan sdk.EngineEvent) {
	t.Helper()
	got, err := e.Ask(ctx, &sdk.EngineAskRequest{Agent: "test", Cwd: t.TempDir(), Instructions: "你叫測試員，回覆只說『收到』兩個字。", Message: "哈囉", TimeoutSeconds: 120})
	if err != nil || got == nil || got.Reply == "" {
		t.Fatalf("ask 要回非空的文字：%+v %v", got, err)
	}
	select {
	case ev := <-events:
		t.Fatalf("ask 不能回報事件：%+v", ev)
	case <-time.After(300 * time.Millisecond):
	}
}

// TurnFailed：一輪因為平台的錯誤停下來時，引擎先報 TURN_STARTED、再報 TURN_FAILED（error 是列出來的種類、
// last_message 是平台原話、session 對得上），而且這一輪不會再有 TURN_ENDED（S5）。trigger 是讓這個引擎出錯的文字
// （fake 引擎用 fake.MarkFail；真的平台沒辦法隨時造出錯誤，不跑這個案例）。回傳收到的 TURN_FAILED，呼叫端可以再對值。
func TurnFailed(t *testing.T, e sdk.EngineAPI, events <-chan sdk.EngineEvent, trigger string) sdk.EngineEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	const name = "conformance-failed"
	if _, err := e.Start(ctx, &sdk.EngineStartRequest{Name: name, Agent: "test", Cwd: t.TempDir()}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if r, err := e.Deliver(ctx, &sdk.EngineDeliverRequest{Session: name, EnvelopeID: "envelope-failed", Text: trigger}); err != nil || r == nil || !r.Delivered {
		t.Fatalf("deliver: %v %v", r, err)
	}
	started := false
	var failed *sdk.EngineEvent
	for failed == nil {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.Session != name {
				continue
			}
			switch ev.Type {
			case sdk.EngineEventTypeTurnStarted:
				started = true
			case sdk.EngineEventTypeTurnEnded:
				t.Fatalf("出錯的那一輪不能報 TURN_ENDED：%+v", ev)
			case sdk.EngineEventTypeTurnFailed:
				if !started {
					t.Fatalf("TURN_FAILED 之前要先有 TURN_STARTED：%+v", ev)
				}
				failed = &ev
			}
		case <-ctx.Done():
			t.Fatal("沒有等到 TURN_FAILED")
		}
	}
	if failed.Error == "" || failed.Error.Normalize() != failed.Error || failed.LastMessage == "" {
		t.Fatalf("TURN_FAILED 要帶列出來的錯誤種類與平台原話：%+v", *failed)
	}
	if _, err := e.Stop(ctx, &sdk.EngineStopRequest{Name: name}); err != nil {
		t.Fatal(err)
	}
	return drainUntilEnded(t, ctx, events, name, *failed)
}

func drainUntilEnded(t *testing.T, ctx context.Context, events <-chan sdk.EngineEvent, name string, failed sdk.EngineEvent) sdk.EngineEvent {
	t.Helper()
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatal("event stream closed")
			}
			if ev.Session == name && ev.Type == sdk.EngineEventTypeTurnEnded {
				t.Fatalf("出錯的那一輪不能報 TURN_ENDED：%+v", ev)
			}
			if ev.Session == name && ev.Type == sdk.EngineEventTypeSessionEnded {
				return failed
			}
		case <-ctx.Done():
			t.Fatal("stop events timed out")
		}
	}
}
