package session

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 通知跟狀態只有一份正本（DEV-49 S2、SES-47）。時間用假時鐘：「3 秒內」＝那件事不在了之後，
// 時鐘不往前超過 3 秒（Tick 每秒、session 一有變化就叫醒）就已經收回。

// waitNotify 是有 Nudge、Retire 的假通知（記下每一次呼叫）。
type waitNotify struct {
	*testNotify
	mu      sync.Mutex
	nudges  []sdk.NotifyNudgeRequest
	retired []string
}

func (w *waitNotify) Nudge(_ context.Context, req *sdk.NotifyNudgeRequest) (*sdk.Nudge, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.nudges = append(w.nudges, *req)
	return &sdk.Nudge{ID: "nudge-x", Session: req.Session, Key: req.Key, Resolved: req.Resolve}, nil
}
func (w *waitNotify) Retire(_ context.Context, ids []string, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.retired = append(w.retired, ids...)
	w.testNotify.mu.Lock()
	defer w.testNotify.mu.Unlock()
	for _, id := range ids {
		if n := w.testNotify.notices[id]; n != nil {
			n.State = sdk.NotificationStateAnswered
		}
	}
	return nil
}
func (w *waitNotify) calls() (sends, resolves []sdk.NotifyNudgeRequest) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, n := range w.nudges {
		if n.Resolve {
			resolves = append(resolves, n)
		} else {
			sends = append(sends, n)
		}
	}
	return
}

func waitSetup(t *testing.T) (*fixture, *waitNotify) {
	t.Helper()
	f := setup(t)
	w := &waitNotify{testNotify: f.n}
	f.m.mu.Lock()
	f.m.d.Notify = w
	f.m.mu.Unlock()
	f.open(t, "code")
	return f, w
}

// Guards: REQ-373
func TestPendingPushesOnlyAndRetractsWhenGone(t *testing.T) {
	gone := map[string]func(f *fixture, p *sdk.Pending){
		"看板回答": func(f *fixture, p *sdk.Pending) { f.answer(t, p, &sdk.Decision{Allow: true}) },
		// 在終端機、另一台、Remote Control 回答：引擎的下一個事件把 Pending 清掉。
		"在別處回答": func(f *fixture, p *sdk.Pending) { f.event(t, sdk.EngineEventTypeActivityEnded) },
		// 逾時不算不在了：確認留在她的畫面上等（SES-48，native_test.go）。
		"對話關了": func(f *fixture, p *sdk.Pending) { must(t, f.m.Close(context.Background(), "code")) },
	}
	names := make([]string, 0, len(gone))
	for k := range gone {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f, w := waitSetup(t)
			out, p := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "git push"})
			f.m.syncWaits(context.Background(), f.h.Clock.Now())
			sends, _ := w.calls()
			if len(sends) != 1 || sends[0].Key != "pending:"+p.ID || sends[0].Session != "code" || sends[0].URL != "/chat/code" || !strings.Contains(sends[0].Context, "git push") {
				t.Fatalf("確認要推播、點了開那個對話：%+v", sends)
			}
			// 不建訊息頁的通知。
			if rows, _ := f.n.List(context.Background(), nil, "", false); len(rows) != 0 {
				t.Fatalf("綁在對話上的等待不進訊息頁：%+v", rows)
			}
			// 對話列的橘點照舊。
			if th := f.thread(t); th.Status != sdk.ThreadStatusWaitingUser {
				t.Fatalf("對話列要亮等你：%+v", th)
			}
			// 再對一次不重推。
			f.m.syncWaits(context.Background(), f.h.Clock.Now())
			if sends, _ = w.calls(); len(sends) != 1 {
				t.Fatal("同一題不重推")
			}
			gone[name](f, p)
			result(t, out)
			at := f.h.Clock.Now()
			f.h.Clock.Advance(time.Second) // 下一次 Tick（每秒）
			f.tick()
			_, resolves := w.calls()
			if len(resolves) != 1 || resolves[0].Key != "pending:"+p.ID || resolves[0].Session != "code" || f.h.Clock.Now().Sub(at) > 3*time.Second {
				t.Fatalf("那件事不在了，3 秒內收回提醒：%+v", resolves)
			}
		})
	}
}

// Guards: REQ-373
func TestNewQuestionReplacesPush(t *testing.T) {
	f, w := waitSetup(t)
	out, p1 := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindQuestion, Questions: []sdk.Question{{Text: "要哪個？", Header: "選版本", Options: []sdk.QuestionOption{{Label: "A"}}}}})
	f.m.syncWaits(context.Background(), f.h.Clock.Now())
	f.answer(t, p1, &sdk.Decision{Answers: map[string]string{"要哪個？": "A"}})
	result(t, out)
	out2, p2 := f.beginApproval(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Summary: "刪檔"})
	f.m.syncWaits(context.Background(), f.h.Clock.Now())
	sends, resolves := w.calls()
	if len(sends) != 2 || sends[0].Key != "pending:"+p1.ID || sends[1].Key != "pending:"+p2.ID || !strings.Contains(sends[0].Context, "選版本") {
		t.Fatalf("每一題各推一則：%+v", sends)
	}
	if len(resolves) != 1 || resolves[0].Key != "pending:"+p1.ID {
		t.Fatalf("上一題收回：%+v", resolves)
	}
	f.answer(t, p2, &sdk.Decision{Allow: false})
	result(t, out2)
}

// 名額詢問（「SEC-n 要現在開嗎？」）在排隊被取消的當下就收掉：單離開 doing、主人手動關（排到了開起來的由 afterQueuedStart 回答）。
// 不再每 3 秒對帳一次綁在對話上的舊通知（S5-2）。
func TestWorkAskRetiredWhenQueueCancelled(t *testing.T) {
	cases := map[string]func(w *workFixture){
		"單離開 doing": func(w *workFixture) { w.setStatus("SEC-2", sdk.TaskStatusDone); w.after(WorkReconcileEvery) },
		"主人手動關":     func(w *workFixture) { must(t, w.m.Close(context.Background(), "code-sec-2")) },
	}
	names := make([]string, 0, len(cases))
	for k := range cases {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			w := workSetup(t, 1, 2)
			wn := &waitNotify{testNotify: w.m.d.Notify.(*testNotify)}
			w.m.mu.Lock()
			w.m.d.Notify = wn
			w.m.mu.Unlock()
			w.openAs(t, "SEC-1", "code")
			s := w.openAs(t, "SEC-2", "owner(web)")
			if s.QueuedAt.IsZero() {
				t.Fatalf("超過上限要排隊：%+v", s)
			}
			w.m.mu.Lock()
			ask := w.m.entries["code-sec-2"].workAsk
			w.m.mu.Unlock()
			if ask == "" {
				t.Fatal("排隊要問主人")
			}
			cases[name](w)
			// 背景寫檔的那一圈也會收名額詢問（retireAsks）：被它先拿走時，這裡要等它的 Retire 做完。
			var got []string
			for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
				wn.mu.Lock()
				got = append([]string(nil), wn.retired...)
				wn.mu.Unlock()
				if len(got) > 0 || time.Now().After(deadline) {
					break
				}
			}
			if strings.Join(got, ",") != ask {
				t.Fatalf("要當下收掉名額詢問 %s：%v", ask, got)
			}
		})
	}
}

func (f *fixture) published(topic string) []map[string]any {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	var out []map[string]any
	for _, ev := range f.b.events {
		if ev.Topic == topic {
			out = append(out, ev.Payload)
		}
	}
	return out
}
