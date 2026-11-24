package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 選擇題、權限確認逾時不消失（DEV-46 S3、SES-48）：逾時後確認回到她自己的畫面上等，
// Pending 留著（標 native）、狀態 waiting、推播不收、不變卡住；看板回答交給引擎代按；畫面上有人答了（下一個引擎事件）才收掉。

// timedOut 開一個確認、讓它逾時（Tick 收），回傳那個 Pending。
func (f *fixture) timedOut(t *testing.T, req *sdk.EngineHostRequestApprovalRequest) *sdk.Pending {
	t.Helper()
	out, _ := f.beginApproval(t, req)
	if req.Kind == sdk.ApprovalKindQuestion {
		f.h.Clock.Advance(QuestionTimeout) // 選擇題等得比較久（SES-66）
	} else {
		f.h.Clock.Advance(ApprovalTimeout)
	}
	f.tick()
	if r := result(t, out); r.decided {
		t.Fatal("逾時不能算有決定")
	}
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if s.Pending == nil || !s.Pending.Native {
		t.Fatalf("逾時後 Pending 要留著、標 native：%+v", s.Pending)
	}
	return s.Pending
}

func (f *fixture) answerCalls() []*sdk.EngineAnswerApprovalRequest {
	var out []*sdk.EngineAnswerApprovalRequest
	for _, c := range f.e.Calls() {
		if c.Method == "AnswerApproval" {
			out = append(out, c.Request.(*sdk.EngineAnswerApprovalRequest))
		}
	}
	return out
}

// Guards: REQ-376
func TestTimeoutKeepsPendingWaitingNotStuck(t *testing.T) {
	f, w := waitSetup(t)
	p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "git push"})
	f.state(t, sdk.SessionStateWaiting)
	if th := f.thread(t); th.Status != sdk.ThreadStatusWaitingUser {
		t.Fatalf("對話列照樣亮等你：%+v", th)
	}
	// 一小時沒人答：還是 waiting、不變卡住，推播不收回（那件事還在）。
	for i := 0; i < 6; i++ {
		f.h.Clock.Advance(10 * time.Minute)
		f.tick()
		f.m.syncWaits(context.Background(), f.h.Clock.Now())
	}
	f.state(t, sdk.SessionStateWaiting)
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if s.Pending == nil || s.Pending.ID != p.ID {
		t.Fatalf("Pending 要一直留著：%+v", s.Pending)
	}
	sends, resolves := w.calls()
	if len(sends) != 1 || sends[0].Key != "pending:"+p.ID || len(resolves) != 0 {
		t.Fatalf("推播推一次、不收回：%+v %+v", sends, resolves)
	}
}

// Guards: REQ-376
func TestEngineGivingUpKeepsPending(t *testing.T) {
	// 引擎那邊不等了（連線斷了）：一樣留在畫面上等，不清掉。
	f := setup(t)
	f.open(t, "code")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, decided, _ := f.m.RequestApproval(ctx, &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
		if decided {
			t.Error("斷線不能算有決定")
		}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := f.m.Get(context.Background(), "code"); s.Pending != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if s.Pending == nil || !s.Pending.Native || s.State != sdk.SessionStateWaiting {
		t.Fatalf("引擎不等了，確認留在畫面上：%+v %s", s.Pending, s.State)
	}
}

// Guards: REQ-377
func TestBoardAnswerGoesToEngineAfterTimeout(t *testing.T) {
	for _, allow := range []bool{true, false} {
		f, w := waitSetup(t)
		p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "git push"})
		f.m.syncWaits(context.Background(), f.h.Clock.Now())
		f.answer(t, p, &sdk.Decision{Allow: allow})
		calls := f.answerCalls()
		if len(calls) != 1 || calls[0].Session != "code" || calls[0].Pending.ID != p.ID || calls[0].Decision.Allow != allow || calls[0].By.Name != "owner(web)" {
			t.Fatalf("看板回答要交給引擎在畫面上按：%+v", calls)
		}
		s, err := f.m.Get(context.Background(), "code")
		must(t, err)
		if s.Pending != nil || s.State != sdk.SessionStateBusy {
			t.Fatalf("答完清掉 Pending、回到忙：%+v %s", s.Pending, s.State)
		}
		if len(f.receipts()) != 1 {
			t.Fatal("答完要有回執")
		}
		at := f.h.Clock.Now()
		f.h.Clock.Advance(time.Second)
		f.tick()
		if _, resolves := w.calls(); len(resolves) != 1 || f.h.Clock.Now().Sub(at) > 3*time.Second {
			t.Fatalf("答完 3 秒內收回提醒：%+v", resolves)
		}
	}
}

// Guards: REQ-377
func TestNodeResolveGoesToEngineAfterTimeout(t *testing.T) {
	// 節點：hub 經 session_resolve 轉來，走 ResolveNotices。
	f := setup(t)
	f.open(t, "code")
	p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	applied, notices, err := f.m.ResolveNotices(context.Background(), "code", p.ID, &sdk.Decision{Allow: true}, "owner(web)")
	if err != nil || !applied || len(notices) != 0 || len(f.answerCalls()) != 1 {
		t.Fatalf("節點上一樣交給引擎：%v %v %v %d", applied, notices, err, len(f.answerCalls()))
	}
}

// Guards: REQ-376
func TestTerminalAnswerClearsNativePending(t *testing.T) {
	for _, kind := range []sdk.EngineEventType{sdk.EngineEventTypeActivityStarted, sdk.EngineEventTypeActivityEnded, sdk.EngineEventTypeTurnStarted, sdk.EngineEventTypeTurnEnded, sdk.EngineEventTypeIdleHint} {
		t.Run(string(kind), func(t *testing.T) {
			f, w := waitSetup(t)
			p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "要哪個？", Options: []sdk.QuestionOption{{Label: "A"}}}}})
			f.m.syncWaits(context.Background(), f.h.Clock.Now())
			f.event(t, kind) // 有人在終端機或遠端連線上直接答了
			s, err := f.m.Get(context.Background(), "code")
			must(t, err)
			if s.Pending != nil || s.State == sdk.SessionStateWaiting {
				t.Fatalf("引擎的下一個事件要清掉：%+v %s", s.Pending, s.State)
			}
			f.h.Clock.Advance(time.Second)
			f.tick()
			if _, resolves := w.calls(); len(resolves) != 1 || resolves[0].Key != "pending:"+p.ID {
				t.Fatalf("推播要收回：%+v", resolves)
			}
			if len(f.answerCalls()) != 0 {
				t.Fatal("沒人在看板答，不叫引擎")
			}
		})
	}
}

// Guards: REQ-376
func TestIdleHintKeepsCorePending(t *testing.T) {
	// 還在等核心回答的（沒逾時）不受閒著提示影響（REQ-143）。
	f := setup(t)
	f.open(t, "code")
	out, _ := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	f.event(t, sdk.EngineEventTypeIdleHint)
	f.event(t, sdk.EngineEventTypeActivityStarted)
	if s, _ := f.m.Get(context.Background(), "code"); s.Pending == nil {
		t.Fatal("核心還在等的確認不能被收掉")
	}
	f.event(t, sdk.EngineEventTypeActivityEnded)
	result(t, out)
}

// Guards: REQ-377
func TestEngineAnswerReasons(t *testing.T) {
	t.Run("畫面上已經沒有確認框", func(t *testing.T) {
		f := setup(t)
		f.e.Configure(fake.Behavior{Delivered: true, AnswerReason: sdk.AnswerNone})
		f.open(t, "code")
		p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
		applied, err := f.m.Resolve(context.Background(), "code", p.ID, &sdk.Decision{Allow: true}, "owner(web)")
		if err != nil || applied {
			t.Fatalf("不算答成（看板顯示已經在別處回答了）：%v %v", applied, err)
		}
		s, _ := f.m.Get(context.Background(), "code")
		if s.Pending != nil || s.State != sdk.SessionStateIdle || len(f.receipts()) != 0 {
			t.Fatalf("收掉、回到閒著、不發回執：%+v %s", s.Pending, s.State)
		}
	})
	t.Run("選擇題引擎還不會代答", func(t *testing.T) {
		f := setup(t)
		f.e.Configure(fake.Behavior{Delivered: true, AnswerReason: sdk.AnswerUnsupported})
		f.open(t, "code")
		p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "要哪個？"}}})
		_, err := f.m.Resolve(context.Background(), "code", p.ID, &sdk.Decision{Answers: map[string]string{"要哪個？": "A"}}, "owner(web)")
		if platform.CodeOf(err) != sdk.ErrorCodeUnavailable {
			t.Fatalf("要說請在畫面上回答：%v", err)
		}
		if s, _ := f.m.Get(context.Background(), "code"); s.Pending == nil || s.State != sdk.SessionStateWaiting {
			t.Fatal("還在等，不能收掉")
		}
	})
	t.Run("沒答完全部題目不叫引擎", func(t *testing.T) {
		f := setup(t)
		f.open(t, "code")
		p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "one"}, {Text: "two"}}})
		if _, err := f.m.Resolve(context.Background(), "code", p.ID, &sdk.Decision{Answers: map[string]string{"one": "A"}}, "owner(web)"); platform.CodeOf(err) != sdk.ErrorCodeInvalid || len(f.answerCalls()) != 0 {
			t.Fatalf("只答一題要擋：%v", err)
		}
	})
}

// Guards: REQ-376
func TestNewApprovalReplacesNative(t *testing.T) {
	// 畫面上等的那個過去了（例如被中斷、事件沒來）才會有新的確認：新的照常接手，不回「已在等待確認」。
	f := setup(t)
	f.open(t, "code")
	old := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "舊的"})
	out := make(chan approvalResult, 1)
	go func() {
		d, decided, err := f.m.RequestApproval(context.Background(), &sdk.EngineHostRequestApprovalRequest{Session: "code", Kind: sdk.ApprovalKindPermission, Summary: "新的"})
		if err != nil {
			t.Error(err)
		}
		out <- approvalResult{d, decided}
	}()
	var p *sdk.Pending
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := f.m.Get(context.Background(), "code"); s.Pending != nil && s.Pending.ID != old.ID {
			p = s.Pending
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if p == nil || p.Native || p.Summary != "新的" {
		t.Fatalf("新的確認要接手：%+v", p)
	}
	f.answer(t, p, &sdk.Decision{Allow: true})
	if r := result(t, out); !r.decided || len(f.answerCalls()) != 0 {
		t.Fatal("新的照一般方式回答")
	}
}

// Guards: REQ-376
func TestRestartRestoresNativePending(t *testing.T) {
	// serve 重開：讀回來的 Pending 一定是在畫面上等的，看板照樣能答（交給引擎）。
	f := setup(t)
	f.open(t, "code")
	f.m.mu.Lock()
	e := f.m.entries["code"]
	e.session.Pending = &sdk.Pending{ID: "p-old", Kind: sdk.PendingKindPermission, Summary: "寫入"}
	e.session.State = sdk.SessionStateWaiting
	f.m.restoreNative(e)
	f.m.mu.Unlock()
	f.answer(t, &sdk.Pending{ID: "p-old"}, &sdk.Decision{Allow: true})
	if calls := f.answerCalls(); len(calls) != 1 || calls[0].Pending.ID != "p-old" {
		t.Fatalf("重開後一樣交給引擎：%+v", calls)
	}
}
