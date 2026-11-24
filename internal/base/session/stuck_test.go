package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

// 一位秘書那一側（引擎）卡住，不能拖住其他秘書（DEV-159）。這些測試裡 code 是卡住的那位、work 是好好的那位；
// 「別人不受影響」用真的時間量：otherWithin 內要完成（正常是幾毫秒，卡住時是永遠或一整個時限）。
const otherWithin = 2 * time.Second

// within 在背景跑 fn，回它有沒有在 d 內做完。
func within(d time.Duration, fn func()) bool {
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

// eventually 在 d 內反覆看 ok，回有沒有等到。
func eventually(d time.Duration, ok func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return ok()
}

// stuckFixture：兩位秘書都開著、閒著；ctx 在測試結束時取消，卡在假引擎裡的呼叫才放得掉。
func stuckFixture(t *testing.T) (*dispatchFixture, context.Context) {
	t.Helper()
	f := dispatchSetup(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return f, ctx
}

func (f *dispatchFixture) deliveredTo(session string) int {
	n := 0
	for _, d := range f.deliveries() {
		if d.Session == session {
			n++
		}
	}
	return n
}

func chatTo(to, body string) *sdk.Envelope {
	return &sdk.Envelope{From: "amy", To: to, Kind: sdk.EnvelopeKindChat, Body: body}
}

// 送話：code 的引擎不回，同一輪排在後面、給 work 的話照樣馬上送到；定時那一輪（Tick）也不被拖住。
func TestStuckDeliverDoesNotHoldOtherSecretary(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.e.StickSession("code", true)
	f.send(t, chatTo("code", "給卡住的那位"))
	f.h.Clock.Advance(time.Nanosecond)
	b := f.send(t, chatTo("work", "給好好的那位"))
	ticked := within(otherWithin, func() { f.m.Tick(ctx, f.h.Clock.Now()) })
	if !eventually(otherWithin, func() bool { return f.deliveredTo("work") == 1 }) {
		t.Fatalf("code 卡住時，給 work 的話 %s 內沒送出去", otherWithin)
	}
	if !ticked {
		t.Fatalf("code 卡住時，定時那一輪 %s 內沒跑完", otherWithin)
	}
	f.want(t, b.ID, sdk.LifecycleNotified)
}

// 送話：code 的引擎每次拖 1.5 秒才回失敗，給 work 的話不用跟著等。
func TestSlowFailingDeliverDoesNotDelayOtherSecretary(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.e.SlowSession("code", 1500*time.Millisecond)
	f.send(t, chatTo("code", "給慢的那位"))
	f.h.Clock.Advance(time.Nanosecond)
	f.send(t, chatTo("work", "給好好的那位"))
	start := time.Now()
	go f.m.Tick(ctx, f.h.Clock.Now())
	if !eventually(otherWithin, func() bool { return f.deliveredTo("work") == 1 }) {
		t.Fatalf("給 work 的話 %s 內沒送出去", otherWithin)
	}
	if took := time.Since(start); took > 700*time.Millisecond {
		t.Fatalf("給 work 的話等了 %s 才送：被慢的那位拖住了", took)
	}
}

// 叫醒：code 停著、有話要給她，開她的引擎不回；派送器不能抓著鎖等，給 work 的話照樣送到。
func TestStuckStartDoesNotHoldDispatcher(t *testing.T) {
	f, ctx := stuckFixture(t)
	must(t, f.m.Close(ctx, "code"))
	f.m.drainEvents(ctx)
	f.e.StickSession("code", true)
	f.send(t, chatTo("code", "叫醒卡住的那位"))
	f.h.Clock.Advance(time.Nanosecond)
	f.send(t, chatTo("work", "給好好的那位"))
	ticked := within(otherWithin, func() { f.m.Tick(ctx, f.h.Clock.Now()) })
	if !eventually(otherWithin, func() bool { return f.deliveredTo("work") == 1 }) {
		t.Fatalf("code 開不起來時，給 work 的話 %s 內沒送出去", otherWithin)
	}
	if !ticked {
		t.Fatalf("code 開不起來時，定時那一輪 %s 內沒跑完", otherWithin)
	}
}

// 開關：code 的關閉卡在引擎，work 的關、開專屬對話、重開都照常。
func TestStuckCloseDoesNotHoldOtherSecretaryOpenClose(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.e.StickSession("code", true)
	go func() { _ = f.m.Close(ctx, "code") }()
	if !eventually(otherWithin, func() bool {
		for _, c := range f.e.Calls() {
			if r, ok := c.Request.(*sdk.EngineStopRequest); ok && r.Name == "code" {
				return true
			}
		}
		return false
	}) {
		t.Fatal("code 的關閉沒有叫到引擎")
	}
	if !within(otherWithin, func() {
		if _, err := f.m.Open(ctx, &sdk.SessionOpenRequest{Name: "work-topic", Agent: "work", Kind: sdk.SessionKindRoom}); err != nil {
			t.Errorf("開 work 的專屬對話：%v", err)
		}
	}) {
		t.Fatalf("code 關不掉時，work 的專屬對話 %s 內沒開起來", otherWithin)
	}
	if !within(otherWithin, func() {
		if err := f.m.Rotate(ctx, "work"); err != nil {
			t.Errorf("重開 work：%v", err)
		}
	}) {
		t.Fatalf("code 關不掉時，work %s 內沒重開", otherWithin)
	}
	if !within(otherWithin, func() {
		if err := f.m.Close(ctx, "work-topic"); err != nil {
			t.Errorf("關 work 的專屬對話：%v", err)
		}
	}) {
		t.Fatalf("code 關不掉時，work 的專屬對話 %s 內沒關掉", otherWithin)
	}
}

// 定時那一輪要收 code 閒置的專屬對話，引擎不回：這一輪不被拖住，下一輪照樣把話送給 work。
func TestStuckIdleCloseDoesNotHoldTick(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.open(t, "code-topic", "code", sdk.SessionKindRoom)
	f.e.StickSession("code-topic", true)
	f.h.Clock.Advance(31 * time.Minute)
	if !within(otherWithin, func() { f.m.Tick(ctx, f.h.Clock.Now()) }) {
		t.Fatalf("收 code 的閒置對話卡在引擎時，定時那一輪 %s 內沒跑完", otherWithin)
	}
	f.send(t, chatTo("work", "給好好的那位"))
	go f.m.Tick(ctx, f.h.Clock.Now())
	if !eventually(otherWithin, func() bool { return f.deliveredTo("work") == 1 }) {
		t.Fatalf("給 work 的話 %s 內沒送出去", otherWithin)
	}
}

func (f *dispatchFixture) unread(t *testing.T, name string) int32 {
	t.Helper()
	rows, _, err := f.m.Threads(context.Background(), "", true)
	must(t, err)
	for _, r := range rows {
		if r.Name == name {
			return r.Unread
		}
	}
	t.Fatalf("找不到討論串 %s", name)
	return 0
}

// 讓 name 的對話多一則秘書的訊息（假引擎貼上一則就附一則回覆），並標成要重讀。
func (f *dispatchFixture) assistantSays(t *testing.T, name, text string) {
	t.Helper()
	f.e.Configure(fake.Behavior{Delivered: true, Messages: []sdk.ChatMessage{{Kind: sdk.ChatKindAssistant, Text: text}}})
	if _, err := f.e.Deliver(context.Background(), &sdk.EngineDeliverRequest{Session: name, Text: "問"}); err != nil {
		t.Fatal(err)
	}
	f.e.Configure(fake.Behavior{Delivered: true})
	f.m.mu.Lock()
	f.m.transcriptDirty[name] = true
	f.m.mu.Unlock()
}

// 未讀：code 的對話記錄讀不回來，work 的未讀數照常更新。
func TestStuckTranscriptDoesNotHoldOtherUnread(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.e.StickSession("code", true)
	f.m.mu.Lock()
	f.m.transcriptDirty["code"] = true
	f.m.mu.Unlock()
	go f.m.refreshUnread(ctx) // 這一次會卡在 code
	if !eventually(otherWithin, func() bool {
		for _, c := range f.e.Calls() {
			if r, ok := c.Request.(*sdk.EngineTranscriptRequest); ok && r.Session == "code" {
				return true
			}
		}
		return false
	}) {
		t.Fatal("沒有去讀 code 的對話記錄")
	}
	f.assistantSays(t, "work", "work 的回覆")
	go f.m.refreshUnread(ctx)
	if !eventually(otherWithin, func() bool { return f.unread(t, "work") == 1 }) {
		t.Fatalf("code 的對話記錄讀不回來時，work 的未讀數 %s 內沒更新", otherWithin)
	}
}

const quietWords = "秘書那邊沒回應"

func saysQuiet(t *testing.T, what string, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), quietWords) || strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("%s：時限到了要回白話（含「%s」），得到：%v", what, quietWords, err)
	}
}

func (f *dispatchFixture) userSaid(t *testing.T, name, text string) int {
	t.Helper()
	all, err := f.m.readChat(context.Background(), name, "")
	must(t, err)
	n := 0
	for _, msg := range all {
		if msg.Kind == sdk.ChatKindUser && msg.Text == text {
			n++
		}
	}
	return n
}

// 貼上有時限：時限到了當成這次沒送到、照退避再試；引擎那邊其實後來送進去了，重試時查到就收下，不貼成兩則。
func TestDeliverTimeoutRetriesAndLateLandingIsNotPastedTwice(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.m.limits.Deliver = 150 * time.Millisecond
	f.e.LandLate("code", true)
	a := f.send(t, chatTo("code", "晚到的那句"))
	f.m.Tick(ctx, f.h.Clock.Now())
	if !eventually(otherWithin, func() bool { return f.get(t, a.ID).Lifecycle == sdk.LifecycleQueued && f.m.retrying(a.ID) }) {
		t.Fatalf("時限到了要放回排隊、記成正在重試：%+v", f.get(t, a.ID))
	}
	f.m.settle()
	h := f.get(t, a.ID).History
	if why := h[len(h)-1].Detail; !strings.Contains(why, quietWords) {
		t.Fatalf("歷程要寫白話的原因：%q", why)
	}
	// 還在退避裡：不重送
	f.tick()
	if n := f.deliveredTo("code"); n != 1 {
		t.Fatalf("退避還沒到就重送了：%d 次", n)
	}
	// 引擎那邊其實已經送進去了（LandLate）：下一次用同一個編號交給引擎，它查到就說已經在了
	f.e.LandLate("code", false)
	f.e.Configure(fake.Behavior{Delivered: true, Recheck: true})
	f.h.Clock.Advance(time.Second)
	f.tick()
	f.want(t, a.ID, sdk.LifecycleReceived)
	d := f.deliveries()
	if len(d) != 2 || d[0].EnvelopeID != a.ID || d[1].EnvelopeID != a.ID {
		t.Fatalf("重試要用同一個封包編號：%+v", d)
	}
	if n := f.userSaid(t, "code", d[0].Text); n != 1 {
		t.Fatalf("她的對話裡這句要剛好一則，得到 %d 則", n)
	}
}

// 開對話有時限：時限到了回白話、記回停了；引擎後來真的開起來，照它回報的算。
func TestStartTimeoutSaysSoAndLateStartStillCounts(t *testing.T) {
	f, ctx := stuckFixture(t)
	must(t, f.m.Close(ctx, "code"))
	f.m.drainEvents(ctx)
	f.m.limits.Start = 150 * time.Millisecond
	f.e.StickSession("code", true)
	_, err := f.m.Open(ctx, &sdk.SessionOpenRequest{Name: "code", Agent: "code", Kind: sdk.SessionKindResident})
	saysQuiet(t, "開對話", err)
	if s := f.sessionState(t, "code"); s != sdk.SessionStateStopped {
		t.Fatalf("沒開起來要記回停了：%s", s)
	}
	f.e.StickSession("code", false)
	f.event(t, sdk.EngineEvent{Session: "code", Type: sdk.EngineEventTypeSessionStarted})
	if s := f.sessionState(t, "code"); s != sdk.SessionStateIdle {
		t.Fatalf("引擎後來回報開好了要算數：%s", s)
	}
}

// 關對話有時限：時限到了回白話，狀態不亂改（引擎沒說關掉就還是原本的）。
func TestStopTimeoutSaysSoAndKeepsState(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.m.limits.Stop = 150 * time.Millisecond
	f.e.StickSession("code", true)
	saysQuiet(t, "關對話", f.m.Close(ctx, "code"))
	if s := f.sessionState(t, "code"); s == sdk.SessionStateStopped {
		t.Fatal("引擎沒回就不能記成停了")
	}
	// 重開（先關再開）也一樣回白話
	saysQuiet(t, "重開", f.m.Rotate(ctx, "code"))
}

func (f *dispatchFixture) callsOf(method, session string) int {
	n := 0
	for _, c := range f.e.Calls() {
		if c.Method != method {
			continue
		}
		switch r := c.Request.(type) {
		case *sdk.EngineStartRequest:
			if r.Name == session {
				n++
			}
		case *sdk.EngineStopRequest:
			if r.Name == session {
				n++
			}
		}
	}
	return n
}

// 同一位秘書自己的開、關、重開仍然一次一件：前一件還在等引擎，後一件排在後面，不會同時叫引擎。
func TestSameSecretaryOperationsStayExclusive(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.e.StickSession("code", true)
	first, release := context.WithCancel(ctx)
	defer release()
	go func() { _ = f.m.Close(first, "code") }()
	if !eventually(otherWithin, func() bool { return f.callsOf("Stop", "code") == 1 }) {
		t.Fatal("第一個關閉沒有叫到引擎")
	}
	rotated := make(chan error, 1)
	go func() { rotated <- f.m.Rotate(ctx, "code") }()
	time.Sleep(200 * time.Millisecond)
	if n := f.callsOf("Stop", "code"); n != 1 {
		t.Fatalf("前一件還沒做完，後一件就叫了引擎：Stop %d 次", n)
	}
	f.e.StickSession("code", false)
	release() // 第一個關閉放手
	select {
	case err := <-rotated:
		must(t, err)
	case <-time.After(otherWithin):
		t.Fatal("前一件結束後，重開沒有接著做")
	}
	if f.callsOf("Stop", "code") != 2 || f.callsOf("Start", "code") != 2 {
		t.Fatalf("重開要關一次、開一次：Stop %d、Start %d", f.callsOf("Stop", "code"), f.callsOf("Start", "code"))
	}
}

// 已讀：讀她的對話記錄有時限，時限到了回白話；別位照常。
func TestMarkSeenTimeoutSaysSo(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.m.limits.Transcript = 150 * time.Millisecond
	f.e.StickSession("code", true)
	_, err := f.m.MarkSeen(ctx, "code", "", "owner(web)")
	saysQuiet(t, "記已讀", err)
	if _, err = f.m.MarkSeen(ctx, "work", "", "owner(web)"); err != nil {
		t.Fatalf("別位的已讀不受影響：%v", err)
	}
}

// 未讀：讀不回來的那位沿用上次的值，引擎好了之後下一次補上。
func TestStuckTranscriptKeepsLastUnreadAndCatchesUp(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.m.limits.Transcript = 150 * time.Millisecond
	f.assistantSays(t, "code", "第一則")
	f.m.refreshUnread(ctx)
	f.m.settle()
	if n := f.unread(t, "code"); n != 1 {
		t.Fatalf("未讀要是 1：%d", n)
	}
	f.assistantSays(t, "code", "第二則")
	f.e.StickSession("code", true)
	f.m.refreshUnread(ctx)
	f.m.settle()
	if n := f.unread(t, "code"); n != 1 {
		t.Fatalf("讀不到時沿用上次的值：%d", n)
	}
	f.e.StickSession("code", false)
	f.m.refreshUnread(ctx)
	f.m.settle()
	if n := f.unread(t, "code"); n != 2 {
		t.Fatalf("引擎好了之後要補上：%d", n)
	}
}

// 對話指令：時限到了回白話，不是原樣的程式錯誤；別位的指令照常。
func TestCommandTimeoutSaysSo(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.m.limits.Command = 150 * time.Millisecond
	f.e.StickSession("code", true)
	_, err := f.m.Command(ctx, "code", sdk.SessionCommandModel, "opus", "owner(web)")
	saysQuiet(t, "對話指令", err)
	if _, err = f.m.Command(ctx, "work", sdk.SessionCommandModel, "opus", "owner(web)"); err != nil {
		t.Fatalf("別位的指令不受影響：%v", err)
	}
	// 指令放手之後位置要還回去：她好了之後照常收話
	f.e.StickSession("code", false)
	a := f.send(t, chatTo("code", "指令之後的話"))
	f.tick()
	f.want(t, a.ID, sdk.LifecycleNotified)
}

// 代答：替主人在她畫面上回答有時限，時限到了回白話、這一題留著可以再答。
func TestNativeAnswerTimeoutSaysSoAndKeepsQuestion(t *testing.T) {
	f, _ := waitSetup(t)
	p := f.timedOut(t, &sdk.EngineHostRequestApprovalRequest{Kind: sdk.ApprovalKindPermission, Tool: "Bash", Summary: "git push"})
	f.m.limits.Answer = 150 * time.Millisecond
	f.e.StickSession("code", true)
	_, err := f.m.Resolve(context.Background(), "code", p.ID, &sdk.Decision{Allow: true}, "owner(web)")
	saysQuiet(t, "代答", err)
	s, err := f.m.Get(context.Background(), "code")
	must(t, err)
	if s.Pending == nil || s.Pending.ID != p.ID {
		t.Fatalf("這一題要留著：%+v", s.Pending)
	}
	f.e.StickSession("code", false)
	ok, err := f.m.Resolve(context.Background(), "code", p.ID, &sdk.Decision{Allow: true}, "owner(web)")
	must(t, err)
	if !ok {
		t.Fatal("引擎好了之後要答得進去")
	}
}

// 快問：整個引擎不回時，核心在「給引擎的秒數＋寬限」之後放手、回白話。
func TestQuickAskTimeoutSaysSo(t *testing.T) {
	f := setup(t)
	f.m.limits.AskGrace = 100 * time.Millisecond
	f.e.Stick()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var err error
	if !within(3*time.Second, func() { _, err = f.m.QuickAsk(ctx, "", "", "", "給誰", 1) }) {
		t.Fatal("快問沒有在時限內放手")
	}
	if err == nil || !strings.Contains(err.Error(), "引擎沒回應") || strings.Contains(err.Error(), "context deadline") {
		t.Fatalf("要回白話：%v", err)
	}
}

// slowBoot：code 的啟動提示問不回來，其他 session 照常回一個提示。
type slowBoot struct{ EngineAccess }

func (b slowBoot) BootPrompts(ctx context.Context, session string) ([]*sdk.BootPrompt, error) {
	if session == "code" {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return []*sdk.BootPrompt{{ID: session + "#trust", Session: session}}, nil
}
func (b slowBoot) AnswerBootPrompt(ctx context.Context, id, _, _ string) error {
	if strings.HasPrefix(id, "code#") {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

type slowBootRegistry struct{ EngineRegistry }

func (r slowBootRegistry) For(account string) (EngineAccess, error) {
	real, err := r.EngineRegistry.For(account)
	if err != nil {
		return nil, err
	}
	return slowBoot{real}, nil
}

// 啟動提示：每位各自問、各自有時限；一位不回，別位的提示照樣在時限內交回來。
func TestBootPromptsReturnsOthersWhenOneIsStuck(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.m.d.Engine = slowBootRegistry{f.m.d.Engine}
	f.m.limits.BootPrompt = 150 * time.Millisecond
	f.m.limits.Answer = 150 * time.Millisecond
	var got []*sdk.BootPrompt
	var err error
	start := time.Now()
	if !within(otherWithin, func() { got, err = f.m.BootPrompts(ctx) }) {
		t.Fatalf("一位不回時，啟動提示 %s 內沒交回來", otherWithin)
	}
	must(t, err)
	if len(got) != 1 || got[0].Session != "work" {
		t.Fatalf("要拿到 work 的提示、略過 code：%+v", got)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("等了 %s：要在每位的時限內回來", took)
	}
	must(t, f.m.AnswerBootPrompt(ctx, "work", "work#trust", "信任", "owner(web)"))
}

// 工作 session：排在前面的那個引擎開不起來（不回），定時那一輪不被拖住，後面排隊的照樣輪得到名額。
func TestStuckQueuedWorkDoesNotStarveNext(t *testing.T) {
	w := workSetup(t, 2, 4)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	for i := 1; i <= 4; i++ {
		w.openAs(t, "SEC-"+itoa(i), "")
		w.h.Clock.Advance(time.Second)
	}
	if w.state(workName("SEC-3")) != sdk.SessionStateStopped || w.state(workName("SEC-4")) != sdk.SessionStateStopped {
		t.Fatal("第 3、4 個要在排隊")
	}
	w.e.StickSession(workName("SEC-3"), true)
	w.setStatus("SEC-1", sdk.TaskStatusDone)
	w.setStatus("SEC-2", sdk.TaskStatusDone)
	ok := eventually(5*time.Second, func() bool {
		w.h.Clock.Advance(WorkReconcileEvery + WorkSettle)
		if !within(otherWithin, func() { w.m.Tick(ctx, w.h.Clock.Now()) }) {
			t.Fatalf("排隊的工作 session 開不起來時，定時那一輪 %s 內沒跑完", otherWithin)
		}
		w.m.drainEvents(ctx)
		return len(w.starts(workName("SEC-4"))) == 1
	})
	if !ok {
		t.Fatalf("第 3 個卡在引擎時，第 4 個沒輪到：3=%s 4=%s", w.state(workName("SEC-3")), w.state(workName("SEC-4")))
	}
	if len(w.starts(workName("SEC-3"))) == 0 {
		t.Fatal("第 3 個要先試著開")
	}
}

// 停對話服務：背景裡還有等引擎的事（她不回），停的時候不等到引擎的時限。
func TestStopDoesNotWaitForStuckEngine(t *testing.T) {
	f := dispatchSetup(t)
	f.e.StickSession("code", true)
	f.send(t, chatTo("code", "給卡住的那位"))
	f.m.Tick(context.Background(), f.h.Clock.Now())
	if !eventually(otherWithin, func() bool { return f.deliveredTo("code") == 1 }) {
		t.Fatal("沒有叫到引擎")
	}
	var err error
	if !within(otherWithin, func() { err = f.m.Stop(context.Background()) }) {
		t.Fatalf("對話服務 %s 內沒停下來", otherWithin)
	}
	must(t, err)
}

// 未讀：引擎回失敗（不是不回）時也沿用上次的值、留著要重讀的記號，不能當成「讀完了、沒有新的」把未讀歸零。
func TestFailingTranscriptKeepsLastUnread(t *testing.T) {
	f, ctx := stuckFixture(t)
	f.assistantSays(t, "code", "第一則")
	f.m.refreshUnread(ctx)
	f.m.settle()
	if n := f.unread(t, "code"); n != 1 {
		t.Fatalf("未讀要是 1：%d", n)
	}
	f.e.SlowSession("code", 20*time.Millisecond)
	f.m.mu.Lock()
	f.m.transcriptDirty["code"] = true
	f.m.mu.Unlock()
	f.m.refreshUnread(ctx)
	f.m.settle()
	if n := f.unread(t, "code"); n != 1 {
		t.Fatalf("引擎回失敗時要沿用上次的值：%d", n)
	}
	f.m.mu.Lock()
	dirty := f.m.transcriptDirty["code"]
	f.m.mu.Unlock()
	if !dirty {
		t.Fatal("沒讀到要留著記號，下次再讀")
	}
	if _, err := f.m.MarkSeen(ctx, "code", "", "owner(web)"); err == nil {
		t.Fatal("讀不到對話記錄時，記已讀要回錯誤，不能記成空的")
	}
}

func (f *dispatchFixture) callsOfTranscript(session string) int {
	n := 0
	for _, c := range f.e.Calls() {
		if r, ok := c.Request.(*sdk.EngineTranscriptRequest); ok && r.Session == session {
			n++
		}
	}
	return n
}

// 背景裡要收閒置的對話時，派送器剛好正要貼話給她（位置被佔著）：這一輪不收，位置放掉後才收。
func TestBackgroundCloseSkipsWhenDeliveryInFlight(t *testing.T) {
	f, _ := stuckFixture(t)
	f.open(t, "code-topic", "code", sdk.SessionKindRoom)
	f.h.Clock.Advance(31 * time.Minute)
	f.m.dispatchMu.Lock()
	f.m.inFlight["code-topic"] = true
	f.m.dispatchMu.Unlock()
	f.tick()
	if n := f.callsOf("Stop", "code-topic"); n != 0 {
		t.Fatalf("正要貼話給她的時候不能收：Stop %d 次", n)
	}
	f.m.dispatchMu.Lock()
	delete(f.m.inFlight, "code-topic")
	f.m.dispatchMu.Unlock()
	f.tick()
	if n := f.callsOf("Stop", "code-topic"); n != 1 {
		t.Fatalf("位置放掉後要收：Stop %d 次", n)
	}
}

// 主人說「照開」時那個工作 session 剛好正在開關：等它做完再開，不能當沒這回事。
func TestForcedStartWaitsForBusySession(t *testing.T) {
	w := workSetup(t, 1, 2)
	w.openAs(t, "SEC-1", "")
	w.openAs(t, "SEC-2", "")
	name := workName("SEC-2")
	if w.state(name) != sdk.SessionStateStopped {
		t.Fatal("第 2 個要在排隊")
	}
	l := w.m.sessionLock(name)
	l.Lock()
	w.m.startForced(context.Background(), name, "code")
	if n := len(w.starts(name)); n != 0 {
		l.Unlock()
		t.Fatalf("別的操作還沒做完就開了：%d 次", n)
	}
	l.Unlock()
	w.m.settle()
	if n := len(w.starts(name)); n != 1 {
		t.Fatalf("前一件做完後要開起來：Start %d 次", n)
	}
}
