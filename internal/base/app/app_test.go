package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// 假模組：把啟停順序記進共用的 log，可指定 Start／Stop 的行為。
type fake struct {
	name     string
	log      *eventLog
	startErr error
	stopErr  error
	stopHang bool // Stop 不理 ctx，卡住直到 release
	release  chan struct{}
}

type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, s)
}

func (l *eventLog) get() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.events, ",")
}

func (f *fake) Name() string { return f.name }
func (f *fake) Start(context.Context) error {
	f.log.add("start:" + f.name)
	return f.startErr
}
func (f *fake) Stop(ctx context.Context) error {
	f.log.add("stop:" + f.name)
	if f.stopHang {
		<-f.release
	}
	return f.stopErr
}

func mods(l *eventLog, names ...string) []Module {
	var ms []Module
	for _, n := range names {
		ms = append(ms, &fake{name: n, log: l})
	}
	return ms
}

// 啟動依 New 的順序、停止反序。
func TestStartInOrderStopInReverse(t *testing.T) {
	l := &eventLog{}
	a := New(mods(l, "a", "b", "c")...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := l.get(), "start:a,start:b,start:c,stop:c,stop:b,stop:a"; got != want {
		t.Fatalf("順序 %s，要 %s", got, want)
	}
}

// 啟動到一半失敗：已啟動的反序停掉，失敗的那個不停，回的錯誤講出是誰、原因。
func TestStartFailureRollsBackStartedModules(t *testing.T) {
	l := &eventLog{}
	boom := errors.New("埠被占用")
	ms := mods(l, "a", "b", "c", "d")
	ms[2].(*fake).startErr = boom
	a := New(ms...)
	err := a.Start(context.Background())
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "啟動 c 失敗") {
		t.Fatalf("錯誤不對：%v", err)
	}
	if got, want := l.get(), "start:a,start:b,start:c,stop:b,stop:a"; got != want {
		t.Fatalf("順序 %s，要 %s（d 不該啟動、c 不該被停）", got, want)
	}
	// 回滾後再 Stop 什麼都不做。
	if err := a.Stop(context.Background()); err != nil || strings.Contains(l.get(), "stop:b,stop:a,stop:") {
		t.Fatalf("回滾後 Stop 不該再停任何東西：%v %s", err, l.get())
	}
}

// 回滾時某個模組停不掉，錯誤要連原因一起回。
func TestStartFailureReportsRollbackErrorsToo(t *testing.T) {
	l := &eventLog{}
	ms := mods(l, "a", "b")
	ms[0].(*fake).stopErr = errors.New("停不掉")
	ms[1].(*fake).startErr = errors.New("起不來")
	err := New(ms...).Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "起不來") || !strings.Contains(err.Error(), "停不掉") {
		t.Fatalf("要同時帶出啟動失敗與回滾失敗：%v", err)
	}
}

// Stop 某個失敗不影響其他的，錯誤全部回。
func TestStopContinuesAfterFailure(t *testing.T) {
	l := &eventLog{}
	ms := mods(l, "a", "b", "c")
	ms[1].(*fake).stopErr = errors.New("b 壞了")
	a := New(ms...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := a.Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "停止 b 失敗") {
		t.Fatalf("錯誤不對：%v", err)
	}
	if !strings.HasSuffix(l.get(), "stop:c,stop:b,stop:a") {
		t.Fatalf("b 失敗後 a 還是要停：%s", l.get())
	}
}

// 某個模組 Stop 不理 ctx、卡住：逾時後放棄等它，繼續停其他的。
func TestStopTimeoutMovesOn(t *testing.T) {
	l := &eventLog{}
	ms := mods(l, "a", "b")
	hang := ms[1].(*fake)
	hang.stopHang, hang.release = true, make(chan struct{})
	defer close(hang.release)
	a := New(ms...).WithStopTimeout(50 * time.Millisecond)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	err := a.Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "停止 b 逾時") {
		t.Fatalf("要回逾時：%v", err)
	}
	if time.Since(begin) > 2*time.Second {
		t.Fatalf("逾時沒有生效：%s", time.Since(begin))
	}
	if !strings.HasSuffix(l.get(), "stop:b,stop:a") {
		t.Fatalf("b 逾時後 a 還是要停：%s", l.get())
	}
}

// 關機常常傳已經取消的 ctx：Stop 不能因此一開始就逾時。
func TestStopWorksWithCancelledContext(t *testing.T) {
	l := &eventLog{}
	a := New(mods(l, "a")...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Stop(ctx); err != nil {
		t.Fatal(err)
	}
}

// Stop 收到的 ctx 要有逾時（MOD-4）：模組看得到 deadline。
func TestStopGivesModuleADeadline(t *testing.T) {
	var hasDeadline bool
	m := &deadlineMod{seen: &hasDeadline}
	a := New(m)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !hasDeadline {
		t.Fatal("Stop 的 ctx 沒有 deadline")
	}
}

type deadlineMod struct{ seen *bool }

func (deadlineMod) Name() string                { return "d" }
func (deadlineMod) Start(context.Context) error { return nil }
func (m *deadlineMod) Stop(ctx context.Context) error {
	_, *m.seen = ctx.Deadline()
	return nil
}

func TestDefaultStopTimeoutIsTenSeconds(t *testing.T) {
	if New().stopTimeout != 10*time.Second {
		t.Fatal("預設逾時要 10 秒")
	}
}

func TestStartTwiceAndBadModuleList(t *testing.T) {
	l := &eventLog{}
	a := New(mods(l, "a")...)
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err == nil {
		t.Fatal("重複 Start 要回錯")
	}
	for name, ms := range map[string][]Module{
		"同名":  mods(l, "x", "x"),
		"nil": {nil},
		"空名":  mods(l, ""),
	} {
		l2 := &eventLog{}
		if err := New(ms...).Start(context.Background()); err == nil {
			t.Fatalf("%s：要回錯", name)
		}
		if l2.get() != "" {
			t.Fatalf("%s：清單有問題時不該啟動任何模組", name)
		}
	}
	if got := l.get(); got != "start:a" {
		t.Fatalf("清單有問題時不該啟動任何模組：%s", got)
	}
}

func TestStartCancelledContextRollsBack(t *testing.T) {
	l := &eventLog{}
	ctx, cancel := context.WithCancel(context.Background())
	ms := mods(l, "a", "b")
	a := New(&cancelAfter{Module: ms[0], cancel: cancel}, ms[1])
	err := a.Start(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("要回取消：%v", err)
	}
	if got := l.get(); got != "start:a,stop:a" {
		t.Fatalf("取消後 b 不該啟動、a 要停：%s", got)
	}
}

type cancelAfter struct {
	Module
	cancel func()
}

func (c *cancelAfter) Start(ctx context.Context) error {
	err := c.Module.Start(ctx)
	c.cancel()
	return err
}

// ---- 登記的收集 ----

// 一個模組同時實作四種登記介面。
type rich struct {
	fake
	mounts []Mount
	checks []Check
	jobs   []Job
	board  []BoardPart
}

func (r *rich) Routes() []Mount    { return r.mounts }
func (r *rich) Checks() []Check    { return r.checks }
func (r *rich) Jobs() []Job        { return r.jobs }
func (r *rich) Board() []BoardPart { return r.board }

// 只實作 Module 的模組不會被當成任何 Provider。
func TestPlainModuleRegistersNothing(t *testing.T) {
	a := New(mods(&eventLog{}, "plain")...)
	if mounts, err := a.Mounts(); err != nil || len(mounts) != 0 {
		t.Fatal(mounts, err)
	}
	if c, err := a.Checks(); err != nil || len(c) != 0 {
		t.Fatal(c, err)
	}
	if j, err := a.Jobs(); err != nil || len(j) != 0 {
		t.Fatal(j, err)
	}
	if b, err := a.Board(); err != nil || len(b) != 0 {
		t.Fatal(b, err)
	}
	if mux, err := a.Mux(); err != nil || mux == nil {
		t.Fatal(mux, err)
	}
}

// 實作 sdk.RemindersAPI 的假實作（嵌入介面，只為了方法簽名符合 sdk.Mount 的檢查）。
type fakeReminders struct{ sdk.RemindersAPI }

func TestMuxMountsRoutesOnOneServeMux(t *testing.T) {
	m := &rich{fake: fake{name: "reminders", log: &eventLog{}}, mounts: []Mount{{Service: "reminders", Impl: &fakeReminders{}}}}
	mux, err := New(m).Mux()
	if err != nil {
		t.Fatal(err)
	}
	var path string
	for _, r := range sdk.Routes {
		if r.Service == "reminders" {
			path = r.Path
			break
		}
	}
	req := httptest.NewRequest(http.MethodPost, path, nil)
	if _, pattern := mux.Handler(req); pattern != path {
		t.Fatalf("路由 %s 沒有掛上（pattern=%q）", path, pattern)
	}
}

func TestMuxRejectsDuplicateService(t *testing.T) {
	l := &eventLog{}
	a := &rich{fake: fake{name: "a", log: l}, mounts: []Mount{{Service: "reminders", Impl: &fakeReminders{}}}}
	b := &rich{fake: fake{name: "b", log: l}, mounts: []Mount{{Service: "reminders", Impl: &fakeReminders{}}}}
	_, err := New(a, b).Mux()
	if err == nil || !strings.Contains(err.Error(), "reminders") || !strings.Contains(err.Error(), "a、b") {
		t.Fatalf("要講出服務與兩個模組：%v", err)
	}
}

// system 服務由好幾個業務模組分著實作：各登記自己的方法，同一個方法或整個服務重複就回錯。
type fakeTrust struct{}

func (fakeTrust) Trust(context.Context, *sdk.SystemTrustRequest) (*sdk.SystemTrustResponse, error) {
	return &sdk.SystemTrustResponse{}, nil
}

type fakeUsage struct{}

func (fakeUsage) Usage(context.Context, *sdk.SystemUsageRequest) (*sdk.SystemUsageResponse, error) {
	return &sdk.SystemUsageResponse{}, nil
}

func TestPartialMountsShareOneService(t *testing.T) {
	l := &eventLog{}
	a := &rich{fake: fake{name: "a", log: l}, mounts: []Mount{{Service: "system", Impl: fakeTrust{}, Methods: []string{"trust"}}}}
	b := &rich{fake: fake{name: "b", log: l}, mounts: []Mount{{Service: "system", Impl: fakeUsage{}, Methods: []string{"usage"}}}}
	mux, err := New(a, b).Mux()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/system/trust", "/api/system/usage"} {
		if _, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, path, nil)); pattern != path {
			t.Errorf("%s 沒有掛上", path)
		}
	}
	if _, pattern := mux.Handler(httptest.NewRequest(http.MethodPost, "/api/system/doctor", nil)); pattern == "/api/system/doctor" {
		t.Error("沒人登記的方法不該被掛上")
	}
}

func TestPartialMountsRejectOverlap(t *testing.T) {
	l := &eventLog{}
	mk := func(name string, mt Mount) *rich {
		return &rich{fake: fake{name: name, log: l}, mounts: []Mount{mt}}
	}
	part := func(m ...string) Mount { return Mount{Service: "system", Impl: fakeTrust{}, Methods: m} }
	whole := Mount{Service: "system", Impl: fakeTrust{}}
	cases := map[string][2]*rich{
		"同一個方法":  {mk("a", part("trust")), mk("b", part("trust", "usage"))},
		"先部分後整個": {mk("a", part("trust")), mk("b", whole)},
		"先整個後部分": {mk("a", whole), mk("b", part("trust"))},
	}
	for name, c := range cases {
		_, err := New(c[0], c[1]).Mounts()
		if err == nil || !strings.Contains(err.Error(), "a、b") {
			t.Errorf("%s：要講出兩個模組：%v", name, err)
		}
	}
}

func TestMuxRejectsBadMounts(t *testing.T) {
	l := &eventLog{}
	cases := map[string]Mount{
		"沒服務名":    {Impl: &fakeReminders{}},
		"沒實作":     {Service: "reminders"},
		"sdk 不認得": {Service: "no_such_service", Impl: &fakeReminders{}},
		"實作簽名不符":  {Service: "reminders", Impl: struct{}{}},
	}
	for name, mt := range cases {
		m := &rich{fake: fake{name: "m", log: l}, mounts: []Mount{mt}}
		if _, err := New(m).Mux(); err == nil {
			t.Fatalf("%s：要回錯", name)
		}
	}
}

func TestCollectChecksJobsBoardInModuleOrder(t *testing.T) {
	l := &eventLog{}
	ok := func(context.Context) CheckResult { return CheckResult{Status: StatusOK, Message: "好"} }
	run := func(context.Context) error { return nil }
	load := func(context.Context) (any, error) { return 1, nil }
	a := &rich{fake: fake{name: "a", log: l},
		checks: []Check{{Name: "a.one", Run: ok}, {Name: "a.two", Run: ok}},
		jobs:   []Job{{Name: "a.job", Cron: "0 * * * *", Run: run}},
		board:  []BoardPart{{Key: "a", Load: load}}}
	b := &rich{fake: fake{name: "b", log: l},
		checks: []Check{{Name: "b.one", Run: ok}},
		jobs:   []Job{{Name: "b.job", Cron: "*/5 * * * *", Run: run}},
		board:  []BoardPart{{Key: "b", Load: load}}}
	app := New(a, b)

	checks, err := app.Checks()
	if err != nil || len(checks) != 3 || checks[0].Name != "a.one" || checks[2].Name != "b.one" {
		t.Fatalf("checks：%v %v", checks, err)
	}
	if r := checks[0].Run(context.Background()); r.Status != StatusOK || r.Message != "好" {
		t.Fatalf("結果不對：%+v", r)
	}
	jobs, err := app.Jobs()
	if err != nil || len(jobs) != 2 || jobs[0].Name != "a.job" || jobs[1].Cron != "*/5 * * * *" {
		t.Fatalf("jobs：%v %v", jobs, err)
	}
	board, err := app.Board()
	if err != nil || len(board) != 2 || board[0].Key != "a" || board[1].Key != "b" {
		t.Fatalf("board：%v %v", board, err)
	}
	if v, err := board[1].Load(context.Background()); err != nil || v != 1 {
		t.Fatalf("Load：%v %v", v, err)
	}
}

func TestCollectRejectsDuplicatesAndIncomplete(t *testing.T) {
	l := &eventLog{}
	ok := func(context.Context) CheckResult { return CheckResult{} }
	run := func(context.Context) error { return nil }
	load := func(context.Context) (any, error) { return nil, nil }
	mk := func(name string, f func(*rich)) *rich {
		r := &rich{fake: fake{name: name, log: l}}
		f(r)
		return r
	}
	t.Run("健檢重名", func(t *testing.T) {
		a := mk("a", func(r *rich) { r.checks = []Check{{Name: "x", Run: ok}} })
		b := mk("b", func(r *rich) { r.checks = []Check{{Name: "x", Run: ok}} })
		if _, err := New(a, b).Checks(); err == nil || !strings.Contains(err.Error(), "x") {
			t.Fatal(err)
		}
	})
	t.Run("定時工作重名", func(t *testing.T) {
		a := mk("a", func(r *rich) { r.jobs = []Job{{Name: "x", Cron: "* * * * *", Run: run}} })
		b := mk("b", func(r *rich) { r.jobs = []Job{{Name: "x", Cron: "* * * * *", Run: run}} })
		if _, err := New(a, b).Jobs(); err == nil {
			t.Fatal("要回錯")
		}
	})
	t.Run("首頁 Key 重複", func(t *testing.T) {
		a := mk("a", func(r *rich) { r.board = []BoardPart{{Key: "x", Load: load}} })
		b := mk("b", func(r *rich) { r.board = []BoardPart{{Key: "x", Load: load}} })
		if _, err := New(a, b).Board(); err == nil {
			t.Fatal("要回錯")
		}
	})
	t.Run("不完整", func(t *testing.T) {
		if _, err := New(mk("a", func(r *rich) { r.checks = []Check{{Name: "x"}} })).Checks(); err == nil {
			t.Fatal("健檢沒有 Run 要回錯")
		}
		both := Check{Name: "x", Run: func(context.Context) CheckResult { return CheckResult{} }, RunAll: func(context.Context) []CheckResult { return nil }}
		if _, err := New(mk("a", func(r *rich) { r.checks = []Check{both} })).Checks(); err == nil {
			t.Fatal("健檢同時有 Run 與 RunAll 要回錯")
		}
		if _, err := New(mk("a", func(r *rich) {
			r.checks = []Check{{Name: "x", RunAll: func(context.Context) []CheckResult { return nil }}}
		})).Checks(); err != nil {
			t.Fatalf("只有 RunAll 的健檢要收：%v", err)
		}
		for name, j := range map[string]Job{
			"沒有 Cron 也沒有 Every": {Name: "x", Run: run},
			"Cron 與 Every 都有":   {Name: "x", Cron: "* * * * *", Every: time.Second, Run: run},
			"First 配 Cron":      {Name: "x", Cron: "* * * * *", First: time.Second, Run: run},
			"負的間隔":              {Name: "x", Every: -time.Second, Run: run},
		} {
			if _, err := New(mk("a", func(r *rich) { r.jobs = []Job{j} })).Jobs(); err == nil {
				t.Fatalf("定時工作%s要回錯", name)
			}
		}
		if _, err := New(mk("a", func(r *rich) { r.jobs = []Job{{Name: "x", Every: time.Second, First: time.Millisecond, Run: run}} })).Jobs(); err != nil {
			t.Fatalf("Every＋First 要收：%v", err)
		}
		if _, err := New(mk("a", func(r *rich) { r.board = []BoardPart{{Load: load}} })).Board(); err == nil {
			t.Fatal("首頁資料沒有 Key 要回錯")
		}
	})
}

type jobSink struct {
	fake
	got  []Job
	err  error
	seen int
}

func (j *jobSink) ConsumeJobs(jobs []Job) error { j.seen++; j.got = jobs; return j.err }

// Assemble 把合併後的定時工作交給 JobConsumer（只交一次，Start 再呼叫也不重交）；登記錯誤或消費者不收都回錯、不啟動。
func TestAssembleHandsJobsToConsumer(t *testing.T) {
	l := &eventLog{}
	run := func(context.Context) error { return nil }
	feat := &rich{fake: fake{name: "feat", log: l}, jobs: []Job{{Name: "feat.tick", Every: time.Minute, Run: run}}}
	sink := &jobSink{fake: fake{name: "runner", log: l}}
	a := New(feat, sink)
	if err := a.Assemble(); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	if sink.seen != 1 || len(sink.got) != 1 || sink.got[0].Name != "feat.tick" {
		t.Fatalf("交一次、內容對：%d %+v", sink.seen, sink.got)
	}

	bad := &jobSink{fake: fake{name: "runner", log: l}, err: errors.New("壞")}
	if err := New(feat, bad).Start(context.Background()); err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("消費者不收要擋下啟動：%v", err)
	}
	dup := &rich{fake: fake{name: "dup", log: l}, jobs: []Job{{Name: "feat.tick", Every: time.Minute, Run: run}}}
	if err := New(feat, dup, &jobSink{fake: fake{name: "runner", log: l}}).Assemble(); err == nil {
		t.Fatal("重名要在組裝時擋下")
	}
}

type collector struct {
	fake
	checks []Check
	parts  []BoardPart
	err    error
}

func (c *collector) ConsumeChecks(cs []Check) error    { c.checks = cs; return c.err }
func (c *collector) ConsumeBoard(ps []BoardPart) error { c.parts = ps; return c.err }

// Assemble 也把健檢交給 CheckConsumer、首頁資料段交給 BoardConsumer；消費者回錯就不啟動。
func TestAssembleHandsChecksAndBoard(t *testing.T) {
	l := &eventLog{}
	feat := &rich{fake: fake{name: "feat", log: l},
		checks: []Check{{Name: "a.one", Run: func(context.Context) CheckResult { return CheckResult{} }}},
		board:  []BoardPart{{Key: "k", Load: func(context.Context) (any, error) { return 1, nil }}}}
	c := &collector{fake: fake{name: "c", log: l}}
	if err := New(feat, c).Assemble(); err != nil || len(c.checks) != 1 || len(c.parts) != 1 || c.parts[0].Key != "k" {
		t.Fatalf("%v %+v %+v", err, c.checks, c.parts)
	}
	bad := &collector{fake: fake{name: "c", log: l}, err: errors.New("不認得")}
	if err := New(feat, bad).Assemble(); err == nil || !strings.Contains(err.Error(), "不認得") {
		t.Fatalf("消費者不收要回錯：%v", err)
	}
}

func TestStatusString(t *testing.T) {
	if StatusOK.String() != "OK" || StatusWarn.String() != "提醒" || StatusError.String() != "錯誤" {
		t.Fatal("三種狀態的白話名稱不對")
	}
}

// ---- 指令 ----

func TestCommandsMergeAndRejectDuplicate(t *testing.T) {
	a := New()
	noop := func(e *cmdline.Env, args []string) error { return nil }
	one := map[string]Command{"tasks add": {Run: noop}, "tasks list": {Run: noop}}
	two := map[string]Command{"reminders add": {Run: noop}}
	all, err := a.Commands(one, two)
	if err != nil || len(all) != 3 {
		t.Fatalf("合併：%v %v", all, err)
	}
	if _, err := a.Commands(one, map[string]Command{"tasks add": {Run: noop}}); err == nil || !strings.Contains(err.Error(), "tasks add") {
		t.Fatalf("重名要回錯並講出名稱：%v", err)
	}
	if _, err := a.Commands(map[string]Command{"x": {}}); err == nil {
		t.Fatal("沒有實作的指令要回錯")
	}
	if all, err := a.Commands(); err != nil || len(all) != 0 {
		t.Fatalf("沒有任何指令表：%v %v", all, err)
	}
}
