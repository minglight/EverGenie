package session

import (
	"context"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 主人打字蓋過選擇題（SES-70）的測試共用：開一個確認、等 session 上有 Pending。
func (f *dispatchFixture) ask(t *testing.T, req *sdk.EngineHostRequestApprovalRequest) <-chan approvalResult {
	t.Helper()
	req.Session = "code"
	out := make(chan approvalResult, 1)
	go func() {
		d, decided, _ := f.m.RequestApproval(context.Background(), req) // 測試結束核心關機時還在等的會回 unavailable，不算失敗
		out <- approvalResult{d, decided}
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if s, _ := f.m.Get(context.Background(), "code"); s != nil && s.Pending != nil {
			f.m.drainEvents(context.Background())
			return out
		}
		if time.Now().After(deadline) {
			t.Fatal("no pending")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func oneQuestion() *sdk.EngineHostRequestApprovalRequest {
	return &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "選哪個", Options: []sdk.QuestionOption{{Label: "A"}, {Label: "B"}}}}}
}

// 選擇題在看板等，主人沒選、直接打一句話：那題當沒答收掉（她拿到「他打的話緊接著到」），這句排著，她閒下來就送進去。
func TestOwnerTypingDismissesQuestion(t *testing.T) {
	f := dispatchSetup(t)
	out := f.ask(t, oneQuestion())
	e := f.send(t, ownerChat("都不要，改做別的"))
	f.tick()
	r := result(t, out)
	if !r.decided || r.decision == nil || r.decision.Allow || r.decision.Message != TypedOverQuestion || len(r.decision.Answers) != 0 {
		t.Fatalf("打字蓋過＝這題沒答、附上說明：%+v", r)
	}
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending != nil {
		t.Fatalf("題目要收掉：%+v %v", s.Pending, err)
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnEnded})
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	if got := f.deliveries(); len(got) == 0 || got[len(got)-1].Text == "" {
		t.Fatalf("她閒下來要把主人那句送進去：%+v", got)
	}
}

// 權限確認不收：主人打字照舊排著，等他按允許／拒絕（確認框開著時貼字可能被當成允許，D-5）。
func TestOwnerTypingKeepsPermissionPending(t *testing.T) {
	f := dispatchSetup(t)
	f.ask(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "寫入"})
	e := f.send(t, ownerChat("等一下"))
	f.tick()
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil {
		t.Fatalf("權限確認要留著：%v", err)
	}
	f.want(t, e.ID, sdk.LifecycleQueued)
}

// 別的秘書傳話不算主人打字：選擇題留著。
func TestPeerSayKeepsQuestion(t *testing.T) {
	f := dispatchSetup(t)
	f.ask(t, oneQuestion())
	f.send(t, &sdk.Envelope{From: "work", Body: "幫我查一下"})
	f.tick()
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil {
		t.Fatalf("秘書傳話不能收掉主人的題目：%v", err)
	}
}

// 已經交回她畫面上的選擇題（native，只有一題）：引擎把說明當成自己打的答案填進去；同一題只做一次。
func TestOwnerTypingAnswersNativeQuestionOnce(t *testing.T) {
	f := dispatchSetup(t)
	f.ask(t, oneQuestion())
	f.h.Clock.Advance(QuestionTimeout + time.Minute)
	f.tick()
	if s, _ := f.m.Get(context.Background(), "code"); s == nil || s.Pending == nil || !s.Pending.Native {
		t.Fatal("要先交回畫面")
	}
	f.send(t, ownerChat("算了，先做這個"))
	f.tick()
	f.tick()
	var calls []*sdk.EngineAnswerApprovalRequest
	for _, c := range f.e.Calls() {
		if c.Method == "AnswerApproval" {
			calls = append(calls, c.Request.(*sdk.EngineAnswerApprovalRequest))
		}
	}
	if len(calls) != 1 || calls[0].Decision == nil || calls[0].Decision.Answers["選哪個"] != TypedOverQuestion {
		t.Fatalf("引擎代填一次說明：%+v", calls)
	}
	if s, _ := f.m.Get(context.Background(), "code"); s == nil || s.Pending != nil {
		t.Fatal("代填成功要收掉題目")
	}
}

// 題目出現前就在排隊的主人訊息（例如她忙時連送兩句，第二句排著）：主人送的時候還沒看到題目，不能拿來收掉後來的題目。
func TestQueuedBeforeQuestionDoesNotDismiss(t *testing.T) {
	f := dispatchSetup(t)
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeTurnStarted})
	f.send(t, ownerChat("第一句"))
	f.send(t, ownerChat("第二句"))
	f.h.Clock.Advance(time.Second)
	f.ask(t, oneQuestion())
	f.tick()
	if s, err := f.m.Get(context.Background(), "code"); err != nil || s.Pending == nil {
		t.Fatalf("題目之前就排著的話不能收掉題目：%v", err)
	}
}

// 過了期限、Tick 還沒交回畫面時主人打字：照畫面上的那題做（引擎代填說明），不卡在「請回答全部問題」。
func TestOwnerTypingAfterDeadlineBeforeTick(t *testing.T) {
	f := dispatchSetup(t)
	f.ask(t, oneQuestion())
	f.h.Clock.Advance(QuestionTimeout + time.Minute)
	f.send(t, ownerChat("改做這個"))
	f.tick()
	if s, _ := f.m.Get(context.Background(), "code"); s == nil || s.Pending != nil {
		t.Fatalf("要交回畫面並代填收掉：%+v", s)
	}
}
