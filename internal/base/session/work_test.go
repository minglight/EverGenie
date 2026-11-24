package session

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/bus"
	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 工作 session（P7-5、D-33）的測試：時間用 FakeClock（TST-3）、引擎用 fake（TST-5），不碰真的引擎或 tailscale。

// memTasks 是工作 session 用的任務單（TaskAccess）：記憶體裡幾張單，記下誰寫了什麼。
type memTasks struct {
	mu      sync.Mutex
	tasks   map[string]*sdk.Task
	updates []string // "<單號>:<by>"
	listErr error    // 節點讀 hub 失敗（ListByAgent）
}

// ListByAgent：節點讀 hub 的單（TaskLister）。
func (t *memTasks) ListByAgent(_ context.Context, agent string) ([]*sdk.Task, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.listErr != nil {
		return nil, t.listErr
	}
	var out []*sdk.Task
	for _, task := range t.tasks {
		if task.Agent == agent {
			cp := *task
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (t *memTasks) Get(_ context.Context, id string) (*sdk.Task, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	task, ok := t.tasks[id]
	if !ok {
		return nil, platform.NotFound("找不到這張任務單")
	}
	cp := *task
	return &cp, nil
}
func (t *memTasks) Update(_ context.Context, id string, p *sdk.TasksUpdateRequest, by string) (*sdk.Task, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	task, ok := t.tasks[id]
	if !ok {
		return nil, platform.NotFound("找不到這張任務單")
	}
	if p.EngineSession != nil {
		task.EngineSession = *p.EngineSession
	}
	t.updates = append(t.updates, id+":"+by)
	cp := *task
	return &cp, nil
}
func (t *memTasks) set(id string, fn func(*sdk.Task)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fn(t.tasks[id])
}

type recNudger struct {
	mu    sync.Mutex
	calls []*sdk.NotifyNudgeRequest
}

func (r *recNudger) Nudge(_ context.Context, req *sdk.NotifyNudgeRequest) (*sdk.Nudge, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return &sdk.Nudge{ID: "nudge-1"}, nil
}
func (r *recNudger) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

type workFixture struct {
	*dispatchFixture
	tasks  *memTasks
	nudger *recNudger
}

// workSetup：work_max 設成 max，單 SEC-1…SEC-n 都是 code 的 doing。
func workSetup(t *testing.T, max, n int) *workFixture {
	t.Helper()
	f := dispatchSetup(t)
	must(t, os.MkdirAll(filepath.Join(f.h.Dir, "settings"), 0700))
	must(t, platform.AtomicWrite(filepath.Join(f.h.Dir, "settings", "sessions.toml"), []byte("schema = 1\nwork_max = "+itoa(max)+"\n"), 0600))
	tasks := &memTasks{tasks: map[string]*sdk.Task{}}
	for i := 1; i <= n; i++ {
		id := "SEC-" + itoa(i)
		tasks.tasks[id] = &sdk.Task{ID: id, Title: "第 " + itoa(i) + " 件事", Agent: "code", Type: sdk.TaskTypeTask, Status: sdk.TaskStatusDoing}
	}
	nudger := &recNudger{}
	f.m.d.Tasks, f.m.d.Nudge = tasks, nudger
	return &workFixture{f, tasks, nudger}
}
func itoa(n int) string { return strconv.Itoa(n) }

func workName(id string) string { return "code-sec-" + strings.TrimPrefix(id, "SEC-") }

// openAs：像 session.open 帶 Topic（單號）那樣開這張單的工作 session。by 空表示沒有人說明身分（當成秘書）。
func (w *workFixture) openAs(t *testing.T, id, by string) Session {
	t.Helper()
	req := &sdk.SessionOpenRequest{Name: workName(id), Agent: "code", Kind: sdk.SessionKindWork, Topic: id}
	if by != "" {
		req.By = &sdk.Actor{Name: by}
	}
	s, err := w.m.Open(context.Background(), req)
	must(t, err)
	w.m.drainEvents(context.Background())
	return s
}

// after：時間走 d，再讓 Tick 對帳一次（tick 之間隔 WorkReconcileEvery 以內的對帳會被略過，所以測試以它為單位前進）。
func (w *workFixture) after(d time.Duration) {
	w.h.Clock.Advance(d)
	w.tick()
	w.m.drainEvents(context.Background())
}

// settle：讓狀態讓出的關閉有時間發生（對帳間隔加上閒置緩衝）。
func (w *workFixture) settle() {
	w.after(WorkReconcileEvery)
	w.after(WorkSettle)
}
func (w *workFixture) state(name string) State {
	s, ok := w.m.State(name)
	if !ok {
		return ""
	}
	return s
}
func (w *workFixture) starts(name string) []*sdk.EngineStartRequest {
	var out []*sdk.EngineStartRequest
	for _, c := range w.e.Calls() {
		if c.Method == "Start" {
			if r := c.Request.(*sdk.EngineStartRequest); r.Name == name {
				out = append(out, r)
			}
		}
	}
	return out
}
func (w *workFixture) stops(name string) int {
	n := 0
	for _, c := range w.e.Calls() {
		if c.Method == "Stop" && c.Request.(*sdk.EngineStopRequest).Name == name {
			n++
		}
	}
	return n
}

// deliveredStatus 是測試用的「交付等驗收」：單是等你（waiting_user）且有 proposed（原本的 review，2026-11-01 併進等你）。
const deliveredStatus sdk.TaskStatus = "delivered"

func (w *workFixture) setStatus(id string, status sdk.TaskStatus) {
	w.tasks.set(id, func(t *sdk.Task) {
		t.Status, t.Proposed = status, ""
		switch status {
		case sdk.TaskStatusWaitingUser:
			t.WaitingSince = w.h.Clock.Now()
			t.Ask = "要用哪個版本？"
		case deliveredStatus:
			t.Status, t.Proposed = sdk.TaskStatusWaitingUser, sdk.TaskStatusDone
			t.WaitingSince = w.h.Clock.Now()
			t.Ask = "請驗收"
		}
	})
	w.m.markWorkDirty()
}
func (w *workFixture) summary(name string) *sdk.ThreadSummary {
	w.m.mu.Lock()
	defer w.m.mu.Unlock()
	return w.m.summary(w.m.entries[name])
}
func (w *workFixture) sessionOf(t *testing.T, name string) Session {
	t.Helper()
	s, err := w.m.Get(context.Background(), name)
	must(t, err)
	return s
}

// Guards: REQ-217
func TestWorkSessionPerTask(t *testing.T) {
	w := workSetup(t, 3, 2)
	w.tasks.tasks["SEC-2"].Agent = "work"
	w.m.config.Accounts["main"].(map[string]any)["remote_access"] = true
	w.m.config.Runtime.Owner, w.m.config.Runtime.Machine = "amy", "home"

	s1 := w.openAs(t, "SEC-1", "code")
	if s1.Name != "code-sec-1" || s1.Kind != sdk.SessionKindWork || s1.Task != "SEC-1" || s1.State != sdk.SessionStateIdle {
		t.Fatalf("一張單一個工作 session：%+v", s1)
	}
	starts := w.starts("code-sec-1")
	if len(starts) != 1 || !starts[0].RemoteAccess || starts[0].RemoteAccessName != "Person·code-amy-home-SEC-1" {
		// fixture 的 work 與 code 用同一個角色，所以人名補「·code」（DEV-90）。
		t.Fatalf("名稱要是 <人名>-<owner>-<machine>-<單號>：%+v", starts)
	}
	// 同一張單再開一次回同一個，不會多開引擎。
	if again := w.openAs(t, "SEC-1", "code"); again.Name != s1.Name || len(w.starts("code-sec-1")) != 1 {
		t.Fatalf("同一張單開兩次要回同一個：%+v", again)
	}
	// 另一張單另一個。
	req := &sdk.SessionOpenRequest{Name: "work-sec-2", Agent: "work", Kind: sdk.SessionKindWork, Topic: "SEC-2"}
	s2, err := w.m.Open(context.Background(), req)
	must(t, err)
	if s2.Name != "work-sec-2" || s2.Task != "SEC-2" {
		t.Fatalf("%+v", s2)
	}
	// 引擎的 session id 記回單上（用負責的秘書的身分寫）。
	got, _ := w.tasks.Get(context.Background(), "SEC-1")
	if got.EngineSession != s1.EngineSessionID || got.EngineSession == "" {
		t.Fatalf("單上要記 engine_session：%+v", got)
	}
	if strings.Join(w.tasks.updates, ",") != "SEC-1:code,SEC-2:work" {
		t.Fatalf("寫回單的身分：%v", w.tasks.updates)
	}
	// 討論串摘要帶種類與單號（看板在秘書底下展開用）。
	if th := w.summary("code-sec-1"); th.Kind != sdk.SessionKindWork || th.Task != "SEC-1" {
		t.Fatalf("%+v", th)
	}
	// 狀態檔記得單號，重開機後還在。
	data, err := os.ReadFile(filepath.Join(w.h.Dir, "runtime", "sessions", "code-sec-1.md"))
	must(t, err)
	if !strings.Contains(string(data), "task: SEC-1") || !strings.Contains(string(data), "kind: work") {
		t.Fatalf("狀態檔：%s", data)
	}
}

// Guards: REQ-218, REQ-268
func TestWorkSessionLifecycleFollowsTask(t *testing.T) {
	// doing：開著，閒置再久也不收（工作 session 不看閒置，看單）。
	t.Run("doing stays open", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "code")
		for i := 0; i < 4; i++ {
			w.after(time.Hour)
		}
		if w.state("code-sec-1") != sdk.SessionStateIdle || w.stops("code-sec-1") != 0 {
			t.Fatalf("doing 的工作 session 要一直開著：%s", w.state("code-sec-1"))
		}
	})

	// waiting_user：保留 50 分鐘 → 提醒一次 → 60 分鐘關。
	t.Run("waiting_user keeps 50 min, nudges once, closes at 60", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "code")
		w.setStatus("SEC-1", sdk.TaskStatusWaitingUser)
		w.after(WorkReconcileEvery)
		w.after(49*time.Minute - WorkReconcileEvery)
		if w.state("code-sec-1") != sdk.SessionStateIdle || w.nudger.count() != 0 {
			t.Fatalf("第 49 分鐘還開著、還沒提醒：%s %d", w.state("code-sec-1"), w.nudger.count())
		}
		w.after(time.Minute) // 第 50 分鐘
		if w.nudger.count() != 1 || w.state("code-sec-1") != sdk.SessionStateIdle {
			t.Fatalf("第 50 分鐘提醒一次、仍開著：%d %s", w.nudger.count(), w.state("code-sec-1"))
		}
		call := w.nudger.calls[0]
		if call.Task != "SEC-1" || call.Session != "code-sec-1" || call.Todo != "要用哪個版本？" || call.URL != "/chat/code-sec-1" ||
			!strings.Contains(call.Title, "SEC-1 還在等你處理") || !strings.Contains(call.Context, "10 分鐘會關掉") {
			t.Fatalf("提醒的內容：%+v", call)
		}
		w.after(5 * time.Minute) // 第 55 分鐘：不重複提醒
		if w.nudger.count() != 1 || w.state("code-sec-1") != sdk.SessionStateIdle {
			t.Fatalf("同一輪等待只提醒一次：%d", w.nudger.count())
		}
		w.after(5 * time.Minute) // 第 60 分鐘
		if w.state("code-sec-1") != sdk.SessionStateStopped || w.stops("code-sec-1") != 1 {
			t.Fatalf("第 60 分鐘要關：%s", w.state("code-sec-1"))
		}
	})

	// 等很久的單（舊系統搬來的）：主人點開、聊過，從最後的動靜重算 60 分鐘，不會一停就被關（REQ-268）。
	t.Run("waiting_user counts from the last activity", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "code")
		w.setStatus("SEC-1", sdk.TaskStatusWaitingUser)
		w.after(WorkReconcileEvery)
		w.after(2 * time.Hour)
		if w.state("code-sec-1") != sdk.SessionStateStopped {
			t.Fatalf("沒人理的照樣 60 分鐘關：%s", w.state("code-sec-1"))
		}
		w.openAs(t, "SEC-1", "owner(web)") // 主人從任務頁點「去聊」接回
		w.settle()
		w.after(30 * time.Minute)
		if w.state("code-sec-1") != sdk.SessionStateIdle {
			t.Fatalf("剛打開的不能馬上關：%s", w.state("code-sec-1"))
		}
		w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "繼續"})
		w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好"})
		w.after(59 * time.Minute)
		if w.state("code-sec-1") != sdk.SessionStateIdle {
			t.Fatalf("聊過之後重算：%s", w.state("code-sec-1"))
		}
		w.after(2 * time.Minute)
		if w.state("code-sec-1") != sdk.SessionStateStopped {
			t.Fatalf("最後一輪之後 60 分鐘關：%s", w.state("code-sec-1"))
		}
	})

	// waiting_external、交付等驗收、完成、不做了：馬上關（等它閒了）。
	for _, status := range []sdk.TaskStatus{sdk.TaskStatusWaitingExternal, deliveredStatus, sdk.TaskStatusDone, sdk.TaskStatusCancelled} {
		t.Run("closes on "+string(status), func(t *testing.T) {
			w := workSetup(t, 3, 1)
			w.openAs(t, "SEC-1", "code")
			w.setStatus("SEC-1", status)
			w.settle()
			if w.state("code-sec-1") != sdk.SessionStateStopped || w.stops("code-sec-1") != 1 {
				t.Fatalf("%s 的單要把工作 session 關掉：%s", status, w.state("code-sec-1"))
			}
			// 關掉不是「手動關」：單不是 doing 就不會自己再開。
			w.after(time.Hour)
			if len(w.starts("code-sec-1")) != 1 {
				t.Fatalf("不該再開：%d", len(w.starts("code-sec-1")))
			}
		})
	}

	// 一輪還在跑就不剪掉，等它閒了才關。
	t.Run("never cuts a running turn", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "code")
		w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceSystem, Input: "做"})
		w.setStatus("SEC-1", deliveredStatus)
		w.settle()
		if w.state("code-sec-1") != sdk.SessionStateBusy {
			t.Fatalf("還在跑的一輪不能被剪掉：%s", w.state("code-sec-1"))
		}
		w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "送驗收了"})
		w.settle()
		if w.state("code-sec-1") != sdk.SessionStateStopped {
			t.Fatalf("一輪結束、閒了就關：%s", w.state("code-sec-1"))
		}
	})

	// 核心送的第一句（From system）貼進去後引擎也說「打字的」：不算主人說過話，送驗收閒了就關（DEV-147）。
	t.Run("core's first message is not the owner talking", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "owner(web)")
		ctx := context.Background()
		brief, err := w.m.d.Bus.Send(ctx, &sdk.Envelope{From: "system", To: "code-sec-1", Kind: sdk.EnvelopeKindRoutine, Route: sdk.EnvelopeRouteCore, Body: "你接了 SEC-1：第 1 件事"})
		must(t, err)
		must(t, w.m.d.Bus.Transition(ctx, brief.ID, sdk.LifecycleNotified, bus.TransitionOpt{Reason: "貼上了"}))
		w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "你接了 SEC-1：第 1 件事"})
		w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "送驗收了"})
		w.setStatus("SEC-1", sdk.TaskStatusDone)
		w.settle()
		if w.state("code-sec-1") != sdk.SessionStateStopped {
			t.Fatalf("只有核心送的第一句，閒了就關：%s", w.state("code-sec-1"))
		}
	})

	// 秘書走核心傳來的話不算；主人在看板回答選擇題（Answer，寄件人 system）算主人說過話。
	for _, c := range []struct {
		name, from string
		kind       sdk.EnvelopeKind
		hold       bool
	}{{"peer via core is not the owner", "life", sdk.EnvelopeKindChat, false}, {"owner's answer counts", "system", sdk.EnvelopeKindAnswer, true}} {
		t.Run(c.name, func(t *testing.T) {
			w := workSetup(t, 3, 1)
			w.openAs(t, "SEC-1", "owner(web)")
			ctx := context.Background()
			env, err := w.m.d.Bus.Send(ctx, &sdk.Envelope{From: c.from, To: "code-sec-1", Kind: c.kind, Route: sdk.EnvelopeRouteCore, Body: "選 A 好了，照這樣做"})
			must(t, err)
			must(t, w.m.d.Bus.Transition(ctx, env.ID, sdk.LifecycleNotified, bus.TransitionOpt{Reason: "貼上了"}))
			w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "選 A 好了，照這樣做"})
			w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好"})
			w.setStatus("SEC-1", sdk.TaskStatusDone)
			w.settle()
			if got := w.state("code-sec-1") != sdk.SessionStateStopped; got != c.hold {
				t.Fatalf("要留著＝%v，狀態 %s", c.hold, w.state("code-sec-1"))
			}
		})
	}

	// 討論到一半秘書把單切成等外部或結掉：主人說過話就等閒 30 分鐘才關，不當場收掉（DEV-147）。
	for _, status := range []sdk.TaskStatus{sdk.TaskStatusWaitingExternal, sdk.TaskStatusDone} {
		t.Run("keeps a conversation with the owner open on "+string(status), func(t *testing.T) {
			w := workSetup(t, 3, 1)
			w.openAs(t, "SEC-1", "code")
			w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "這功能有 deploy 嗎？"})
			w.setStatus("SEC-1", status)
			w.event(t, sdk.EngineEvent{Session: "code-sec-1", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "還沒，先改成等外部"})
			w.settle()
			w.after(20 * time.Minute)
			if w.state("code-sec-1") != sdk.SessionStateIdle || w.stops("code-sec-1") != 0 {
				t.Fatalf("主人剛說過話不能關：%s", w.state("code-sec-1"))
			}
			w.after(10 * time.Minute)
			if w.state("code-sec-1") != sdk.SessionStateStopped {
				t.Fatalf("主人最後一句之後閒 30 分鐘要關：%s", w.state("code-sec-1"))
			}
		})
	}

	// 主人手動關掉的不自動接回；單離開 doing 再回來才接回。
	t.Run("manual close sticks until the task cycles", func(t *testing.T) {
		w := workSetup(t, 3, 1)
		w.openAs(t, "SEC-1", "code")
		must(t, w.m.Close(context.Background(), "code-sec-1"))
		w.after(time.Hour)
		if w.state("code-sec-1") != sdk.SessionStateStopped || len(w.starts("code-sec-1")) != 1 {
			t.Fatalf("手動關掉的不要自己開回來：%s", w.state("code-sec-1"))
		}
		w.setStatus("SEC-1", sdk.TaskStatusWaitingExternal)
		w.after(WorkReconcileEvery)
		w.setStatus("SEC-1", sdk.TaskStatusDoing)
		w.after(WorkReconcileEvery)
		if w.state("code-sec-1") == sdk.SessionStateStopped || len(w.starts("code-sec-1")) != 2 {
			t.Fatalf("單回到 doing 要接回：%s", w.state("code-sec-1"))
		}
	})
}

// 引擎接不回原本的對話、開了新的：核心要記新的 id，秘書要再拿到一次單（DEV-133）。
func TestWorkSessionLostConversationGetsBriefAgain(t *testing.T) {
	w := workSetup(t, 3, 1)
	first := w.openAs(t, "SEC-1", "code")
	briefs := func() int {
		rows, err := w.b.List(context.Background(), "code-sec-1")
		must(t, err)
		n := 0
		for _, e := range rows {
			if strings.HasPrefix(e.Body, "SEC-1：") {
				n++
			}
		}
		return n
	}
	before := briefs()
	w.setStatus("SEC-1", deliveredStatus)
	w.settle()
	if w.state("code-sec-1") != sdk.SessionStateStopped {
		t.Fatalf("送驗收要關：%s", w.state("code-sec-1"))
	}
	w.m.mu.Lock()
	w.m.entries["code-sec-1"].seen = "舊對話讀到的位置"
	w.m.mu.Unlock()
	w.e.Configure(fake.Behavior{Delivered: true, ResumeLost: true})
	w.setStatus("SEC-1", sdk.TaskStatusDoing)
	w.after(WorkReconcileEvery)
	starts := w.starts("code-sec-1")
	if len(starts) != 2 || starts[1].ResumeSessionID != first.EngineSessionID {
		t.Fatalf("要先試著接回原本的對話：%+v", starts)
	}
	now := w.sessionOf(t, "code-sec-1")
	if now.EngineSessionID == first.EngineSessionID || now.EngineSessionID == "" {
		t.Fatalf("要記下引擎回的新 id：%+v", now)
	}
	if got := briefs(); got != before+1 {
		t.Fatalf("開了新對話要再送一次單：前 %d 後 %d", before, got)
	}
	w.m.mu.Lock()
	seen := w.m.entries["code-sec-1"].seen
	w.m.mu.Unlock()
	if seen != "" {
		t.Fatalf("新對話的已讀位置要從頭算：%q", seen)
	}
}

// 接不回對話時單常常還排著沒送出（對話檔只有標題就是這樣）：不重複排，秘書只收到一份。
func TestWorkSessionLostConversationKeepsQueuedBrief(t *testing.T) {
	w := workSetup(t, 3, 1)
	w.openAs(t, "SEC-1", "code")
	w.setStatus("SEC-1", deliveredStatus)
	w.settle()
	_, brief, err := w.m.readWork(context.Background(), "SEC-1")
	must(t, err)
	_, err = w.b.Send(context.Background(), &sdk.Envelope{From: "system", To: "code-sec-1", Kind: sdk.EnvelopeKindRoutine, Body: brief})
	must(t, err)
	w.e.Configure(fake.Behavior{Delivered: true, ResumeLost: true})
	w.setStatus("SEC-1", sdk.TaskStatusDoing)
	w.after(WorkReconcileEvery)
	rows, err := w.b.List(context.Background(), "code-sec-1")
	must(t, err)
	n := 0
	for _, e := range rows {
		if e.Body == brief {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("單只能有一份：%d", n)
	}
}

// 順利接回時單已經在對話裡，不重送。
func TestWorkSessionResumeDoesNotResendBrief(t *testing.T) {
	w := workSetup(t, 3, 1)
	w.openAs(t, "SEC-1", "code")
	count := func() int {
		rows, err := w.b.List(context.Background(), "code-sec-1")
		must(t, err)
		n := 0
		for _, e := range rows {
			if strings.HasPrefix(e.Body, "SEC-1：") {
				n++
			}
		}
		return n
	}
	before := count()
	w.setStatus("SEC-1", deliveredStatus)
	w.settle()
	w.setStatus("SEC-1", sdk.TaskStatusDoing)
	w.after(WorkReconcileEvery)
	if got := count(); got != before {
		t.Fatalf("接回成功不該重送單：前 %d 後 %d", before, got)
	}
}

// Guards: REQ-219
func TestWorkSessionResume(t *testing.T) {
	w := workSetup(t, 3, 1)
	first := w.openAs(t, "SEC-1", "code")
	id := first.EngineSessionID
	if id == "" {
		t.Fatal("引擎要回 session id")
	}
	w.setStatus("SEC-1", deliveredStatus)
	w.settle()
	if w.state("code-sec-1") != sdk.SessionStateStopped {
		t.Fatalf("送驗收要關：%s", w.state("code-sec-1"))
	}
	// 被退回：接回原本的對話（Start 帶同一個 session id），並跟秘書說一聲與退回意見。
	w.tasks.set("SEC-1", func(task *sdk.Task) { task.ReviewNote = "表格少一欄" })
	w.setStatus("SEC-1", sdk.TaskStatusDoing)
	w.after(WorkReconcileEvery)
	starts := w.starts("code-sec-1")
	if len(starts) != 2 || starts[1].ResumeSessionID != id {
		t.Fatalf("接回要帶原本的引擎 session：%+v", starts)
	}
	if now := w.sessionOf(t, "code-sec-1"); now.EngineSessionID != id || now.State == sdk.SessionStateStopped {
		t.Fatalf("接回後仍是同一個引擎 session：%+v", now)
	}
	rows, err := w.b.List(context.Background(), "code-sec-1")
	must(t, err)
	found := false
	for _, e := range rows {
		if e.Kind == sdk.EnvelopeKindRoutine && e.From == "system" && strings.Contains(e.Body, "SEC-1") && strings.Contains(e.Body, "表格少一欄") {
			found = true
		}
	}
	if !found {
		t.Fatalf("接回要附上退回意見：%+v", rows)
	}

	// 狀態檔不見了：用單上記的 engine_session 接回。
	must(t, w.m.Close(context.Background(), "code-sec-1"))
	w.m.mu.Lock()
	delete(w.m.entries, "code-sec-1")
	w.m.mu.Unlock()
	must(t, os.Remove(filepath.Join(w.h.Dir, "runtime", "sessions", "code-sec-1.md")))
	w.openAs(t, "SEC-1", "code")
	starts = w.starts("code-sec-1")
	if last := starts[len(starts)-1]; last.ResumeSessionID != id {
		t.Fatalf("狀態檔丟了，要靠單上的 engine_session 接回：%+v", last)
	}

	// 記錄丟了：用單上記的引擎 session 接回（每台一個家，單上的就是這台的，法律第 7 條）。
	w.tasks.set("SEC-1", func(task *sdk.Task) { task.EngineSession = "elsewhere-1" })
	// （上面那次已經開起來，這裡直接驗 workStarting 的規則）
	var resume string
	w.m.mu.Lock()
	w.m.workStarting(&entry{}, &sdk.Task{ID: "SEC-9", EngineSession: "elsewhere-9"}, &resume)
	w.m.mu.Unlock()
	if resume != "elsewhere-9" {
		t.Fatalf("單上記的引擎 session 要拿來接回：%q", resume)
	}
}

func (w *workFixture) notices() []*sdk.Notification {
	n := w.m.d.Notify.(*testNotify)
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []*sdk.Notification
	for i := 1; i <= len(n.notices); i++ {
		out = append(out, n.notices["M-"+itoa(i)])
	}
	return out
}
func (w *workFixture) answer(t *testing.T, id, text string) {
	t.Helper()
	must(t, w.m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "notice.answered", Payload: map[string]any{"id": id, "text": text, "via": "button", "room": "x"}}))
	w.m.drainEvents(context.Background())
}

// Guards: REQ-220
func TestWorkLimitAsksInThread(t *testing.T) {
	setup := func(t *testing.T) (*workFixture, *sdk.Notification) {
		w := workSetup(t, 2, 3)
		w.openAs(t, "SEC-1", "owner(web)")
		w.openAs(t, "SEC-2", "owner(web)")
		if len(w.notices()) != 0 {
			t.Fatal("沒超過上限不該問")
		}
		third := w.openAs(t, "SEC-3", "owner(web)")
		if third.State != sdk.SessionStateStopped || third.QueuedAt.IsZero() || len(w.starts("code-sec-3")) != 0 {
			t.Fatalf("超過上限：先不開，排隊：%+v", third)
		}
		notices := w.notices()
		if len(notices) != 1 {
			t.Fatalf("只在那張新單的討論串問一則：%d", len(notices))
		}
		return w, notices[0]
	}

	t.Run("question in the thread, no push", func(t *testing.T) {
		w, n := setup(t)
		if n.Category != sdk.CategoryApprove || n.Room != "code-sec-3" || !n.Inline || n.Agent != "code" {
			t.Fatalf("討論串內提問（不推播）：%+v", n)
		}
		if got := strings.Join(n.Options, "|"); got != "照開|先排隊|關掉 SEC-1|關掉 SEC-2" {
			t.Fatalf("選項：%s", got)
		}
		if !strings.Contains(n.Context, "SEC-1") || !strings.Contains(n.Context, "SEC-2") || strings.Contains(n.Context, "SEC-3 ") {
			t.Fatalf("要附目前開著的清單：%q", n.Context)
		}
		if th := w.summary("code-sec-3"); th.Status != sdk.ThreadStatusWaitingUser || !hasReason(th.Waiting, sdk.WaitingReasonApproval) {
			t.Fatalf("那個討論串要黃點：%+v", th)
		}
		// 再開一次（或主人再傳一句話）不重複問。
		w.openAs(t, "SEC-3", "owner(web)")
		if len(w.notices()) != 1 {
			t.Fatal("同一張單不重複問")
		}
	})

	t.Run("close one", func(t *testing.T) {
		w, n := setup(t)
		w.answer(t, n.ID, "關掉 SEC-1")
		if w.state("code-sec-1") != sdk.SessionStateStopped || w.sessionOf(t, "code-sec-1").QueuedAt.IsZero() {
			t.Fatalf("SEC-1 關掉並排回佇列：%+v", w.sessionOf(t, "code-sec-1"))
		}
		if s := w.sessionOf(t, "code-sec-3"); s.State == sdk.SessionStateStopped || !s.QueuedAt.IsZero() {
			t.Fatalf("SEC-3 開起來：%+v", s)
		}
		if w.state("code-sec-2") == sdk.SessionStateStopped {
			t.Fatal("只關被點名的那個")
		}
		w.noAnswerEnvelope(t)
		// SEC-2 送驗收 → 空出名額，排隊的 SEC-1 自動接回。
		w.setStatus("SEC-2", deliveredStatus)
		w.settle()
		if s := w.sessionOf(t, "code-sec-1"); s.State == sdk.SessionStateStopped || !s.QueuedAt.IsZero() {
			t.Fatalf("有名額就接回排隊的：%+v", s)
		}
	})

	t.Run("open anyway", func(t *testing.T) {
		w, n := setup(t)
		w.answer(t, n.ID, "照開")
		if w.state("code-sec-3") == sdk.SessionStateStopped || w.state("code-sec-1") == sdk.SessionStateStopped || w.state("code-sec-2") == sdk.SessionStateStopped {
			t.Fatal("照開：三個都開著")
		}
		w.noAnswerEnvelope(t)
		w.after(time.Hour) // 超過上限的不會被自己關掉
		if w.state("code-sec-3") == sdk.SessionStateStopped {
			t.Fatal("照開之後不自己關")
		}
	})

	t.Run("queue it", func(t *testing.T) {
		w, n := setup(t)
		w.answer(t, n.ID, "先排隊")
		if w.state("code-sec-3") != sdk.SessionStateStopped || w.sessionOf(t, "code-sec-3").QueuedAt.IsZero() {
			t.Fatal("先排隊：不開")
		}
		w.noAnswerEnvelope(t)
		if th := w.summary("code-sec-3"); hasReason(th.Waiting, sdk.WaitingReasonApproval) {
			t.Fatalf("回答之後提問的黃點要消失：%+v", th)
		}
		w.setStatus("SEC-1", sdk.TaskStatusDone)
		w.settle()
		if w.state("code-sec-3") == sdk.SessionStateStopped {
			t.Fatal("有名額就自動開")
		}
	})
}

// noAnswerEnvelope：上限的提問由 session 自己處理，不會變成送進模型的 answer 封包。
func (w *workFixture) noAnswerEnvelope(t *testing.T) {
	t.Helper()
	rows, err := w.b.List(context.Background(), "")
	must(t, err)
	for _, e := range rows {
		if e.Kind == sdk.EnvelopeKindAnswer {
			t.Fatalf("上限的回答不該送進模型：%+v", e)
		}
	}
}

// Guards: REQ-221
func TestWorkLimitQueuesWhenNotOwner(t *testing.T) {
	w := workSetup(t, 2, 4)
	w.openAs(t, "SEC-1", "code")
	w.openAs(t, "SEC-2", "code")
	for _, by := range []string{"code", ""} {
		s := w.openAs(t, "SEC-3", by)
		if s.State != sdk.SessionStateStopped || s.QueuedAt.IsZero() || len(w.starts("code-sec-3")) != 0 {
			t.Fatalf("不是主人開的：直接排隊（by=%q）：%+v", by, s)
		}
	}
	if len(w.notices()) != 0 {
		t.Fatalf("不是主人開的不問、不通知：%+v", w.notices())
	}
	th := w.summary("code-sec-3")
	// 排隊等名額不是在等主人（S5 D6）：討論串是 queued、原因寫 work_queue，不算進要你回覆的數字。
	if th.Status != sdk.ThreadStatusQueued || !hasReason(th.Waiting, sdk.WaitingReasonWorkQueue) {
		t.Fatalf("排隊中的討論串是排隊（灰色），不是要你回覆：%+v", th)
	}
	if _, n, err := w.m.Threads(context.Background(), "", true); err != nil || n != 0 {
		t.Fatalf("排隊等名額不算進要你回覆：%d %v", n, err)
	}
	// 只有 doing 算進上限：SEC-1 等主人時不佔名額。
	w.setStatus("SEC-1", sdk.TaskStatusWaitingUser)
	w.after(WorkReconcileEvery)
	if w.state("code-sec-3") == sdk.SessionStateStopped {
		t.Fatal("waiting_user 的不算進上限，排隊的該開了")
	}
	if th := w.summary("code-sec-3"); len(th.Waiting) != 0 {
		t.Fatalf("開起來之後排隊原因消失：%+v", th)
	}
	// 排隊的依先後順序開：SEC-3、SEC-4 都在等，名額一次只空一個。
	w.openAs(t, "SEC-4", "code")
	w.setStatus("SEC-2", deliveredStatus)
	w.settle()
	if w.state("code-sec-4") == sdk.SessionStateStopped {
		t.Fatal("空出名額，SEC-4 該開")
	}
}

// Guards: REQ-222
func TestCloneMergedIntoWork(t *testing.T) {
	f := workSetup(t, 1, 1)
	// 一個 doing 的工作 session 把唯一的名額佔滿；秘書傳話的分身不算進上限。
	f.openAs(t, "SEC-1", "code")
	f.event(t, sdk.EngineEvent{Type: sdk.EngineEventTypeActivityStarted})
	e := f.send(t, &sdk.Envelope{})
	f.h.Clock.Advance(5 * time.Minute)
	f.tick()
	if got := f.get(t, e.ID).To; got != "code-c1" {
		t.Fatalf("傳話排 5 分鐘要改送工作 session：%s", got)
	}
	s, err := f.m.Get(context.Background(), "code-c1")
	must(t, err)
	if s.Kind != sdk.SessionKindWork || s.Task != "" {
		t.Fatalf("clone 併進 work：種類是 work、不綁單：%+v", s)
	}
	f.tick()
	f.want(t, e.ID, sdk.LifecycleNotified)
	// 不綁單的工作 session 閒置就收掉（跟過去的分身一樣），不受單的對帳管。
	f.event(t, sdk.EngineEvent{Session: "code-c1", Type: sdk.EngineEventTypeTurnStarted, Source: sdk.InputSourceTyped, Input: "請處理這則訊息"})
	f.event(t, sdk.EngineEvent{Session: "code-c1", Type: sdk.EngineEventTypeTurnEnded, LastMessage: "好"})
	f.want(t, e.ID, sdk.LifecycleReplied)
	f.h.Clock.Advance(31 * time.Minute)
	f.tick()
	if state, _ := f.m.State("code-c1"); state != sdk.SessionStateStopped {
		t.Fatalf("閒置 30 分鐘收掉：%s", state)
	}
	if state, _ := f.m.State("code-sec-1"); state == sdk.SessionStateStopped {
		t.Fatal("綁單的工作 session 不受閒置影響")
	}
	// 新的程式不再開 clone。
	list, err := f.m.List(context.Background(), "", nil)
	must(t, err)
	for _, s := range list {
		if s.Kind == sdk.SessionKindClone {
			t.Fatalf("不該再開 clone：%+v", s)
		}
	}
}

// Guards: REQ-221
func TestWorkQueueSurvivesRestart(t *testing.T) {
	w := workSetup(t, 1, 2)
	w.openAs(t, "SEC-1", "code")
	w.openAs(t, "SEC-2", "owner(web)")
	n := w.notices()[0]
	must(t, w.m.flushDirty())
	// 重新讀狀態檔：排隊、提問的對應都還在。
	m := w.m
	m.mu.Lock()
	must(t, m.restore())
	m.mu.Unlock()
	if e := m.entries["code-sec-2"]; e == nil || e.session.QueuedAt.IsZero() || e.workAsk != n.ID || m.workAsks[n.ID] != "code-sec-2" || e.session.Task != "SEC-2" {
		t.Fatalf("重開機後排隊與提問要還在：%+v", e)
	}
	w.answer(t, n.ID, "照開")
	if w.state("code-sec-2") == sdk.SessionStateStopped {
		t.Fatal("重開機後回答照樣有效")
	}
}

// startDoing 模擬 tasks 業務模組在單從別的狀態改成 doing 時叫 AutoOpenWork（什麼時候叫由 tasks 決定，見 tasks 的 TestDoingOpensWorkSession）。
func (w *workFixture) startDoing(t *testing.T, id, by string) {
	t.Helper()
	task, err := w.tasks.Get(context.Background(), id)
	must(t, err)
	w.m.RefreshWork()
	w.m.AutoOpenWork(context.Background(), id, task.Agent, by)
	w.m.drainEvents(context.Background())
}

// Guards: REQ-217, REQ-221, REQ-186
func TestWorkSessionAutoOpensWhenTaskStartsDoing(t *testing.T) {
	w := workSetup(t, 1, 4)
	// 已經是 doing 的舊單：沒有事件就不開（一上線不會開滿上限）。
	w.after(WorkReconcileEvery)
	if len(w.starts("code-sec-1")) != 0 || w.state("code-sec-1") != "" {
		t.Fatal("已經是 doing 的舊單不自動開")
	}
	// 單剛變成 doing：開它的工作 session，第一句是單的內容。
	w.tasks.set("SEC-2", func(task *sdk.Task) { task.Body = "把報告寫完" })
	w.startDoing(t, "SEC-2", "code")
	if s := w.sessionOf(t, "code-sec-2"); s.Kind != sdk.SessionKindWork || s.Task != "SEC-2" || len(w.starts("code-sec-2")) != 1 {
		t.Fatalf("剛變成 doing 要開工作 session：%+v", s)
	}
	rows, err := w.b.List(context.Background(), "code-sec-2")
	must(t, err)
	if len(rows) != 1 || rows[0].Kind != sdk.EnvelopeKindRoutine || !strings.Contains(rows[0].Body, "SEC-2：第 2 件事\n把報告寫完") {
		t.Fatalf("第一句要是單的內容：%+v", rows)
	}
	// 已經開了：再叫一次不重開、不重送（新開的單、doing → doing 不叫，由 tasks 判斷）。
	w.startDoing(t, "SEC-2", "code")
	if len(w.starts("code-sec-2")) != 1 {
		t.Fatal("已經開了不重開")
	}
	// 超過上限照 workGate：秘書改的直接排隊，不問。
	w.startDoing(t, "SEC-3", "code")
	if s := w.sessionOf(t, "code-sec-3"); s.QueuedAt.IsZero() || len(w.starts("code-sec-3")) != 0 || len(w.notices()) != 0 {
		t.Fatalf("超過上限、秘書改的要直接排隊：%+v", s)
	}
	// 主人改的在討論串裡問。
	w.startDoing(t, "SEC-4", "owner(web)")
	if s := w.sessionOf(t, "code-sec-4"); s.QueuedAt.IsZero() || len(w.notices()) != 1 {
		t.Fatalf("超過上限、主人改的要在討論串問：%+v %d", s, len(w.notices()))
	}
	// 單不是 doing（事件晚到、單已經又改了）或不是這位秘書的：不開。
	w.tasks.set("SEC-1", func(task *sdk.Task) { task.Status, task.Proposed = sdk.TaskStatusWaitingUser, sdk.TaskStatusDone })
	w.startDoing(t, "SEC-1", "code")
	if w.state("code-sec-1") != "" {
		t.Fatal("單現在不是 doing 就不開")
	}
}

// Guards: REQ-229, REQ-217, REQ-221, REQ-186, REQ-205

// Guards: REQ-229, REQ-186
func TestHubDoesNotPollTasksForStarts(t *testing.T) {
	// hub（不是節點）靠 tasks 在單改成 doing 時叫 AutoOpenWork，不輪詢：就算 Tasks 有實作 ListByAgent，hub 也不會因為單變成 doing 就自己開。
	w := workSetup(t, 3, 2)
	w.m.mu.Lock()
	w.m.mu.Unlock()
	w.after(WorkReconcileEvery)
	w.setStatus("SEC-1", sdk.TaskStatusIdea)
	w.after(WorkReconcileEvery)
	w.setStatus("SEC-1", sdk.TaskStatusDoing)
	w.after(WorkReconcileEvery)
	if w.state("code-sec-1") != "" {
		t.Fatal("hub 不靠輪詢開工作 session（tasks 沒叫就不開）")
	}
}

// 主人手動關掉（或取消排隊）的工作 session，名下還有沒送出去的話（例：開單時送的第一句、別的秘書傳的話）也不自己開回來：
// 話留著，等主人再開、對它說話，或單離開 doing 再回來（REQ-218）。以前派送看到停了的工作 session 有話就直接開，
// 把「手動關掉」洗掉，e2e 的 seed.closeAllWork 關完又被開回來（TestShotsWorkSessions 偶爾 20 秒收不完）。
// Guards: REQ-218
func TestWorkManualCloseNotReopenedByPendingMessage(t *testing.T) {
	ctx := context.Background()
	w := workSetup(t, 1, 2)
	w.openAs(t, "SEC-1", "code")
	if s := w.openAs(t, "SEC-2", "code"); s.QueuedAt.IsZero() {
		t.Fatalf("超過上限要排隊：%+v", s)
	}
	brief := w.send(t, &sdk.Envelope{From: "system", To: "code-sec-2", Kind: sdk.EnvelopeKindRoutine, Body: "SEC-2 的第一句"})
	other := w.send(t, &sdk.Envelope{From: "work", To: "code-sec-1"})
	must(t, w.m.Close(ctx, "code-sec-2")) // 取消排隊
	must(t, w.m.Close(ctx, "code-sec-1")) // 關掉開著的
	for i := 0; i < 5; i++ {
		w.after(WorkReconcileEvery)
	}
	if w.state("code-sec-2") != sdk.SessionStateStopped || len(w.starts("code-sec-2")) != 0 {
		t.Fatalf("取消排隊的不要因為有話就自己開：%s，開了 %d 次", w.state("code-sec-2"), len(w.starts("code-sec-2")))
	}
	if w.state("code-sec-1") != sdk.SessionStateStopped || len(w.starts("code-sec-1")) != 1 {
		t.Fatalf("手動關掉的不要因為有話就自己開回來：%s，開了 %d 次", w.state("code-sec-1"), len(w.starts("code-sec-1")))
	}
	w.want(t, brief.ID, sdk.LifecycleQueued)
	w.want(t, other.ID, sdk.LifecycleQueued)

	// 主人再開它：照常開起來，留著的話接著送。
	w.openAs(t, "SEC-2", "owner(web)")
	if w.state("code-sec-2") == sdk.SessionStateStopped {
		t.Fatal("主人再開要開起來")
	}
}

// TSK-28：工作 session 的第一句（交單）列出單上的附件連結，跟 tasks.BriefText 同一個格式。
func TestWorkBriefListsAttachments(t *testing.T) {
	w := workSetup(t, 3, 1)
	w.tasks.set("SEC-1", func(task *sdk.Task) { task.Attachments = []string{"/share/a.png", "/share/b.pdf"} })
	_, err := w.m.OpenWork(context.Background(), "SEC-1", "code", &sdk.Actor{Name: "owner(web)"})
	must(t, err)
	rows, err := w.b.List(context.Background(), "code-sec-1")
	must(t, err)
	for _, e := range rows {
		if strings.Contains(e.Body, "SEC-1") && strings.Contains(e.Body, "\n附件：\n- /share/a.png\n- /share/b.pdf") {
			return
		}
	}
	t.Fatalf("交單那句要列附件：%+v", rows)
}

// 單還沒結、先收起來的任務對話留在對話清單（不含已停的那份也列），結單或取消才不列（DEV-147）。
func TestParkedWorkSessionStaysListed(t *testing.T) {
	w := workSetup(t, 3, 1)
	w.openAs(t, "SEC-1", "code")
	w.setStatus("SEC-1", sdk.TaskStatusWaitingExternal)
	w.settle()
	if w.state("code-sec-1") != sdk.SessionStateStopped {
		t.Fatalf("等外部、沒人說話的要收起來：%s", w.state("code-sec-1"))
	}
	listed := func() *sdk.ThreadSummary {
		rows, _, err := w.m.Threads(context.Background(), "", false)
		must(t, err)
		for _, r := range rows {
			if r.Name == "code-sec-1" {
				return r
			}
		}
		return nil
	}
	w.m.mu.Lock()
	w.m.entries["code-sec-1"].ended = true
	w.m.entries["code-sec-1"].messages = append(w.m.entries["code-sec-1"].messages, &sdk.ChatMessage{}) // 收起來前的回覆還沒讀：也寫已收起，不寫完成
	w.m.mu.Unlock()
	if r := listed(); r == nil || r.Status != sdk.ThreadStatusStopped || r.Task != "SEC-1" {
		t.Fatalf("單還沒結的對話收起來也要留在清單：%+v", r)
	}
	w.setStatus("SEC-1", sdk.TaskStatusDone)
	w.after(WorkReconcileEvery)
	if r := listed(); r != nil {
		t.Fatalf("結單之後不列：%+v", r)
	}
}

// 整個家還原前把開著的對話全部收掉（SES-75）：每一個都叫引擎關；跟主人手動關不一樣，不記「不要自動接回」，
// 進行中的單的工作對話之後照平常的規則再開。
func TestCloseAllStopsEverySessionAndLetsWorkComeBack(t *testing.T) {
	ctx := context.Background()
	w := workSetup(t, 3, 2)
	w.openAs(t, "SEC-1", "code")
	w.openAs(t, "SEC-2", "code")
	n, err := w.m.CloseAll(ctx)
	must(t, err)
	if n < 2 || w.state("code-sec-1") != sdk.SessionStateStopped || w.state("code-sec-2") != sdk.SessionStateStopped {
		t.Fatalf("開著的都要關掉：關了 %d，%s／%s", n, w.state("code-sec-1"), w.state("code-sec-2"))
	}
	for i := 0; i < 3; i++ {
		w.after(WorkReconcileEvery)
	}
	if w.state("code-sec-1") == sdk.SessionStateStopped || len(w.starts("code-sec-1")) != 2 {
		t.Fatalf("單還在進行中，收掉的工作對話要照規則再開：%s，開了 %d 次", w.state("code-sec-1"), len(w.starts("code-sec-1")))
	}
}
