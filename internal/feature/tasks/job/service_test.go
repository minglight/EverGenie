package job

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
	"github.com/minglight/evergenie/sdk"
)

type fakeBus struct {
	mu     sync.Mutex
	events []*sdk.BusSubscribeResponse
	calls  *[]string
	sent   []*sdk.Envelope // Send 收到的封包（同一個冪等鍵只留一則，跟 bus 一樣）
}

func (b *fakeBus) Send(_ context.Context, e *sdk.Envelope) (*sdk.Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, old := range b.sent {
		if e.IdempotencyKey != "" && old.IdempotencyKey == e.IdempotencyKey {
			return old, nil
		}
	}
	c := *e
	c.ID = fmt.Sprintf("E-%d", len(b.sent)+1)
	b.sent = append(b.sent, &c)
	return &c, nil
}
func (b *fakeBus) Get(_ context.Context, id string) (*sdk.Envelope, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.sent {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, platform.NotFound("找不到封包")
}
func (b *fakeBus) envelopes() []*sdk.Envelope {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]*sdk.Envelope(nil), b.sent...)
}

func (b *fakeBus) Publish(ctx context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, &sdk.BusSubscribeResponse{Topic: topic, Payload: p})
	return nil
}
func (b *fakeBus) Subscribe(ctx context.Context, topics ...string) (<-chan *sdk.BusSubscribeResponse, error) {
	return make(chan *sdk.BusSubscribeResponse), nil
}
func (b *fakeBus) Sweep(context.Context, time.Time) (int, error) {
	*b.calls = append(*b.calls, "bus")
	return 0, nil
}
func (b *fakeBus) of(topic string) []*sdk.BusSubscribeResponse {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*sdk.BusSubscribeResponse
	for _, e := range b.events {
		if e.Topic == topic {
			out = append(out, e)
		}
	}
	return out
}

type fakeVault struct {
	records []vault.Record
	budget  int
	calls   *[]string
	fail    bool
	noAgent string // 這位秘書「不存在」（Get 回 NotFound）
	down    bool   // 節點讀 hub 的 vault，hub 連不上：Get、List 都回 Unavailable
}

func (v *fakeVault) RegisterSchema(vault.Spec) {}
func (v *fakeVault) Get(_ context.Context, kind, id string) (vault.Record, error) {
	if v.down {
		return vault.Record{}, platform.Unavailable("主機連不上")
	}
	if kind == "agent" && id == v.noAgent {
		return vault.Record{}, platform.NotFound("找不到這位秘書")
	}
	if kind == "job" {
		for _, r := range v.records {
			if r.ID == id {
				return r, nil
			}
		}
		return vault.Record{}, platform.NotFound("找不到這個排程")
	}
	return vault.Record{Fields: map[string]any{"budget": map[string]any{"max_runs_per_day": v.budget}}}, nil
}
func (v *fakeVault) List(context.Context, string, vault.Query) ([]vault.Record, error) {
	if v.down {
		return nil, platform.Unavailable("主機連不上")
	}
	return v.records, nil
}
func (v *fakeVault) Put(context.Context, vault.Record, string, string) (vault.Record, error) {
	return vault.Record{}, fmt.Errorf("not used")
}
func (v *fakeVault) Patch(context.Context, string, string, map[string]any, []string, string) (vault.Record, error) {
	return vault.Record{}, fmt.Errorf("not used")
}
func (v *fakeVault) Archive(context.Context, string, string, string) error {
	return fmt.Errorf("not used")
}
func (v *fakeVault) ArchiveNow(context.Context, string, string, string) error {
	return fmt.Errorf("not used")
}
func (v *fakeVault) Move(context.Context, string, string, string, map[string]any, string) (vault.Record, error) {
	return vault.Record{}, fmt.Errorf("not used")
}
func (v *fakeVault) Sweep(context.Context, time.Time, bool) (vault.Report, error) {
	*v.calls = append(*v.calls, "vault")
	if v.fail {
		return vault.Report{}, fmt.Errorf("test failure")
	}
	return vault.Report{}, nil
}

// rig 是測試用的組合：jobs 模組加上真的計時迴圈（假時鐘、不開每秒 tick，測試自己呼叫 Tick）。
type rig struct {
	*Module
	*timer.Scheduler
}

func newRig(t *testing.T, h *homefixture.Home, b BusSender, v VaultStore) *rig {
	t.Helper()
	home, err := platform.LoadHome(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	tm := timer.NewScheduler(h.Clock)
	tm.SetLocation(home.Location())
	m := New(Deps{Bus: b, Vault: v, Timer: tm, Clock: h.Clock})
	m.SetHome(h.Dir)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("單一工作有問題不能讓排程服務起不來：%v", err)
	}
	t.Cleanup(func() {
		if err := m.Stop(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return &rig{m, tm}
}

func setup(t *testing.T, records ...vault.Record) (*rig, *platform.FakeClock, *fakeBus, *fakeVault) {
	t.Helper()
	h := homefixture.New(t)
	calls := []string{}
	b := &fakeBus{calls: &calls}
	v := &fakeVault{records: records, calls: &calls}
	m := newRig(t, h, b, v)
	return m, h.Clock, b, v
}
func job(spec, catchup string) vault.Record {
	return vault.Record{Kind: "job", ID: "code/ping", Body: "測試用的指令內文", Fields: map[string]any{"name": "測試", "cron": []string{spec}, "agent": "code", "enabled": true, "catchup": catchup, "action": "none"}}
}
func tick(m *rig, c *platform.FakeClock, d time.Duration) {
	c.Advance(d)
	m.Tick(context.Background(), c.Now())
}

// Guards: REQ-15
func TestCronFiresOncePerMinute(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	for i := 0; i < 10; i++ {
		tick(m, c, time.Minute)
		m.Tick(context.Background(), c.Now())
	}
	if n := len(b.of("schedule.fire")); n != 10 {
		t.Fatal(n)
	}
}

// Guards: REQ-15
func TestMultipleCrons(t *testing.T) {
	m, c, b, _ := setup(t)
	if err := m.Cron(context.Background(), "multi", []string{"* * * * *", "* * * * *"}, timer.CronOpt{}); err != nil {
		t.Fatal(err)
	}
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 1 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-16
func TestFireNeverCallsSession(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, i := range f.Imports {
			p, err := strconv.Unquote(i.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, module := range []string{"session", "engine", "tasks", "reminders"} {
				if p == "github.com/minglight/evergenie/internal/"+module {
					t.Fatal(p)
				}
			}
		}
	}
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 1 {
		t.Fatal(b.events)
	}
}

// 排程送出去之後，引擎沒回報（封包沒結束）就不猜「還沒完成」：一天都沒有 system.alert（S5、D7）。
func TestJobWithoutOutcomeNeverAlerts(t *testing.T) {
	m, c, b, _ := setup(t, job("1 * * * *", "skip"))
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 1 {
		t.Fatal(b.events)
	}
	for range 24 * 6 {
		tick(m, c, 10*time.Minute)
	}
	if n := len(b.of("system.alert")); n != 0 {
		t.Fatalf("沒有引擎的事實就不能發提醒：%d", n)
	}
}

// Guards: REQ-17
func TestCatchupOnce(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "once"))
	tick(m, c, 3*time.Hour)
	events := b.of("schedule.fire")
	if len(events) != 1 || !events[0].Payload["at"].(time.Time).Equal(c.Now()) {
		t.Fatal(events)
	}
	missed := b.of("schedule.missed")
	if len(missed) != 1 || missed[0].Payload["caught_up"] != true {
		t.Fatal(missed)
	}
}

// Guards: REQ-17
func TestCatchupSkip(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "skip"))
	tick(m, c, 3*time.Hour)
	if len(b.of("schedule.fire")) != 0 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-17
func TestCatchupTooOld(t *testing.T) {
	m, c, b, _ := setup(t, job("* * * * *", "once"))
	tick(m, c, 7*time.Hour)
	if len(b.of("schedule.fire")) != 0 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-18
func TestDisableJobStopsFiring(t *testing.T) {
	m, c, b, v := setup(t, job("* * * * *", "skip"))
	v.records[0].Fields["enabled"] = false
	m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "vault.changed", Payload: map[string]any{"kind": "job"}})
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 0 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-18
func TestChangeCronNoRestart(t *testing.T) {
	m, c, b, v := setup(t, job("* * * * *", "skip"))
	v.records[0].Fields["cron"] = []string{"2 * * * *"}
	m.handle(context.Background(), &sdk.BusSubscribeResponse{Topic: "vault.changed", Payload: map[string]any{"kind": "job"}})
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 0 {
		t.Fatal("old cron fired")
	}
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 1 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-20
func TestNightlyRotateAction(t *testing.T) {
	r := job("* * * * *", "skip")
	r.Fields["action"] = "rotate"
	m, c, b, _ := setup(t, r)
	tick(m, c, time.Minute)
	e := b.of("schedule.fire")
	if len(e) != 1 || e[0].Payload["action"] != "rotate" {
		t.Fatal(e)
	}
}

// Guards: REQ-16
func TestBudgetMaxRunsPerDay(t *testing.T) {
	m, c, b, v := setup(t, job("* * * * *", "skip"))
	v.budget = 2
	for i := 0; i < 3; i++ {
		tick(m, c, time.Minute)
	}
	if len(b.of("schedule.fire")) != 2 {
		t.Fatal(b.events)
	}
	tick(m, c, 24*time.Hour)
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 3 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-15
func TestAtFiresOnceThenRemoved(t *testing.T) {
	m, c, b, _ := setup(t)
	if err := m.At(context.Background(), "once", c.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	tick(m, c, time.Minute)
	tick(m, c, time.Minute)
	if len(b.of("schedule.fire")) != 1 {
		t.Fatal(b.events)
	}
	if _, ok := m.Next("once"); ok {
		t.Fatal("single event remains")
	}
	if platform.CodeOf(m.Cancel(context.Background(), "once")) != sdk.ErrorCodeNotFound {
		t.Fatal("missing cancel")
	}
}

// Guards: REQ-15
func TestAtInPastFiresNextTick(t *testing.T) {
	m, c, b, _ := setup(t)
	if err := m.At(context.Background(), "past", c.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	m.Tick(context.Background(), c.Now())
	if len(b.of("schedule.fire")) != 1 {
		t.Fatal(b.events)
	}
}

// Guards: REQ-15
func TestCronForModuleHasKindCron(t *testing.T) {
	m, c, b, _ := setup(t)
	if err := m.Cron(context.Background(), "module", []string{"* * * * *"}, timer.CronOpt{}); err != nil {
		t.Fatal(err)
	}
	tick(m, c, time.Minute)
	e := b.of("schedule.fire")
	if len(e) != 1 || e[0].Payload["kind"] != "cron" {
		t.Fatal(e)
	}
}

// Guards: REQ-5, REQ-10, REQ-216
func TestMaintenanceRunsNightly(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(strconv.FormatBool(configured), func(t *testing.T) {
			m, c, b, v := setup(t)

			v.fail = true
			next, ok := m.Next("system.maintenance")
			if !ok {
				t.Fatal("missing maintenance")
			}
			c.Set(next)
			m.Tick(context.Background(), c.Now())
			m.Tick(context.Background(), c.Now())
			want := []string{"vault", "bus"}

			if !reflect.DeepEqual(*v.calls, want) {
				t.Fatalf("calls=%v want=%v", *v.calls, want)
			}
			e := b.of("schedule.fire")
			if len(e) != 1 || e[0].Payload["id"] != "system.maintenance" || e[0].Payload["kind"] != "cron" {
				t.Fatal(e)
			}
		})
	}
}

// Guards: REQ-117, REQ-18
func TestProblemsListsUnloadableJobs(t *testing.T) {
	good := job("* * * * *", "skip")
	good.ID = "code/good"
	badCron := job("not a cron", "skip")
	badCron.ID = "code/badcron"
	noBody := job("* * * * *", "skip")
	noBody.ID, noBody.Body = "code/nobody", "  \n"
	ghost := job("* * * * *", "skip")
	ghost.ID, ghost.Fields["agent"] = "ghost/ping", "ghost"
	off := job("not a cron", "skip") // 停用的工作不檢查
	off.ID, off.Fields["enabled"] = "code/off", false
	h := homefixture.New(t)
	calls := []string{}
	v := &fakeVault{records: []vault.Record{good, badCron, noBody, ghost, off}, calls: &calls, noAgent: "ghost"}
	m := newRig(t, h, &fakeBus{calls: &calls}, v)

	problems := m.Problems()
	if len(problems) != 3 {
		t.Fatalf("要 3 條問題（壞 cron、空內文、秘書不存在），實際 %q", problems)
	}
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"code/badcron", "code/nobody", "ghost"} {
		if !strings.Contains(joined, want) {
			t.Errorf("問題裡要提到 %s：%q", want, problems)
		}
	}
	if _, ok := m.Next("code/good"); !ok {
		t.Error("正常的工作照常排上")
	}
	for _, id := range []string{"code/badcron", "code/nobody", "ghost/ping"} {
		if _, ok := m.Next(id); ok {
			t.Errorf("%s 有問題，不該被排上", id)
		}
	}
	// 修好之後重載，問題清掉。
	v.records = []vault.Record{good}
	m.mu.Lock()
	err := m.reload(context.Background(), h.Clock.Now())
	m.mu.Unlock()
	if err != nil || len(m.Problems()) != 0 {
		t.Fatalf("修好後不該再有問題：%q %v", m.Problems(), err)
	}
}

// 健檢項目 schedule.jobs：有排不上的工作是錯誤（所有問題併成一條、附修法），修好之後回到 OK。
// Guards: REQ-117
func TestChecksReportUnloadableJobs(t *testing.T) {
	good := job("* * * * *", "skip")
	good.ID = "code/good"
	bad := job("not a cron", "skip")
	bad.ID = "code/badcron"
	h := homefixture.New(t)
	calls := []string{}
	v := &fakeVault{records: []vault.Record{good, bad}, calls: &calls}
	m := newRig(t, h, &fakeBus{calls: &calls}, v)
	checks, err := app.New(m.Module).Checks()
	if err != nil || len(checks) != 1 || checks[0].Name != "schedule.jobs" {
		t.Fatalf("要登記一項 schedule.jobs：%+v %v", checks, err)
	}
	got := checks[0].Run(context.Background())
	if got.Status != app.StatusError || !strings.Contains(got.Message, "code/badcron") || got.Fix == "" {
		t.Fatalf("壞的 cron 要報錯並附修法：%+v", got)
	}
	v.records = []vault.Record{good}
	m.mu.Lock()
	err = m.reload(context.Background(), h.Clock.Now())
	m.mu.Unlock()
	if got := checks[0].Run(context.Background()); err != nil || got.Status != app.StatusOK || got.Message != "排程都載入了" {
		t.Fatalf("修好後回到 OK：%+v %v", got, err)
	}
}

// Guards: REQ-19
func TestNextRunsMatchesService(t *testing.T) {
	// 遷移報告用 NextRuns 算下次觸發時間，一定要跟排程服務自己排出來的一樣（同一份解析）。
	specs := []string{"30 21 * * *", "0 8 * * 1"}
	m, c, _, _ := setup(t)
	if err := m.Cron(context.Background(), "x", specs, timer.CronOpt{}); err != nil {
		t.Fatal(err)
	}
	got, err := timer.NextRuns(specs, m.Location(), c.Now(), 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("NextRuns：%v %v", got, err)
	}
	if next, ok := m.Next("x"); !ok || !next.Equal(got[0]) {
		t.Fatalf("第一次觸發：服務 %v，NextRuns %v", next, got)
	}
	if !got[0].Before(got[1]) || !got[1].Before(got[2]) {
		t.Fatalf("要由早到晚：%v", got)
	}
	if _, err := timer.NextRuns([]string{"nope"}, m.Location(), c.Now(), 1); err == nil {
		t.Fatal("壞掉的 cron 應該回錯誤")
	}
	if _, err := timer.NextRuns(nil, m.Location(), c.Now(), 1); err == nil {
		t.Fatal("沒有 cron 應該回錯誤")
	}
}

// setupWith 跟 setup 一樣，但先讓 prep 改家裡的檔案（例如放 servers/*.toml）再啟動。
func setupWith(t *testing.T, prep func(dir string), records ...vault.Record) (*rig, *platform.FakeClock, *fakeBus) {
	t.Helper()
	h := homefixture.New(t)
	prep(h.Dir)
	calls := []string{}
	b := &fakeBus{calls: &calls}
	m := newRig(t, h, b, &fakeVault{records: records, calls: &calls})
	return m, h.Clock, b
}

func sitedJob(id string, sites []any) vault.Record {
	r := job("* * * * *", "skip")
	r.ID = id
	if sites != nil {
		r.Fields["sites"] = sites
	}
	return r
}

// Guards: REQ-21

// Guards: REQ-177

func windowJobRecord(windows, weekend []any, perDay int) vault.Record {
	f := map[string]any{"name": "主動聊天", "agent": "life", "enabled": true, "action": "none", "windows": windows, "per_day": perDay}
	if weekend != nil {
		f["weekend_windows"] = weekend
	}
	return vault.Record{Kind: "job", ID: "life/chat", Body: "找他聊一個話題", Fields: f}
}

// Guards: REQ-22
func TestWindowJobFires(t *testing.T) {
	m, c, b := setupWith(t, func(string) {}, windowJobRecord([]any{"12:15-13:15", "15:30-17:00"}, []any{"10:00-11:30"}, 1))
	// 從 2026-10-23（五）12:00 跑三天，每分鐘一次 tick。
	perDay := map[string]int{}
	for i := 0; i < 3*24*60; i++ {
		tick(m, c, time.Minute)
	}
	for _, e := range b.of("schedule.fire") {
		if e.Payload["id"] != "life/chat" {
			continue
		}
		at := e.Payload["at"].(time.Time).In(c.Now().Location())
		end := e.Payload["window_end"].(time.Time).In(c.Now().Location())
		perDay[at.Format("2006-01-02")]++
		hm := at.Format("15:04")
		weekend := at.Weekday() == time.Saturday || at.Weekday() == time.Sunday
		inside := (!weekend && ((hm >= "12:15" && hm < "13:15") || (hm >= "15:30" && hm < "17:00"))) || (weekend && hm >= "10:00" && hm < "11:30")
		if !inside || !end.After(at) {
			t.Errorf("觸發時間 %s 不在時段裡（週末=%v，結束 %s）", at, weekend, end)
		}
		if e.Payload["kind"] != "job" || e.Payload["agent"] != "life" {
			t.Errorf("payload：%v", e.Payload)
		}
	}
	for _, day := range []string{"2026-10-24", "2026-10-25"} {
		if perDay[day] != 1 {
			t.Errorf("%s 要剛好一次（per_day=1）：%v", day, perDay)
		}
	}
}

// Guards: REQ-22
func TestWindowJobMissedAfterWindowEnds(t *testing.T) {
	m, c, b := setupWith(t, func(string) {}, windowJobRecord([]any{"12:15-13:15"}, nil, 1))
	// 電腦睡到 14:00 才醒：時段過了，不補，發 schedule.missed。
	c.Advance(2 * time.Hour)
	m.Tick(context.Background(), c.Now())
	if n := len(b.of("schedule.fire")); n != 0 { // 12:00→14:00 之間沒有其他排程
		t.Fatalf("時段過了不該觸發：%d", n)
	}
	if n := len(b.of("schedule.missed")); n != 1 {
		t.Fatalf("要記一筆 missed：%d", n)
	}
	if next, ok := m.Next("life/chat"); !ok || next.In(c.Now().Location()).Format("2006-01-02") != "2026-10-24" {
		t.Fatalf("下一次要排到隔天：%v %v", next, ok)
	}
}

// Guards: REQ-22
func TestWindowJobBadWindowIsProblem(t *testing.T) {
	m, _, _ := setupWith(t, func(string) {}, windowJobRecord([]any{"13:15-12:15"}, nil, 1))
	if p := m.Problems(); len(p) != 1 || !strings.Contains(p[0], "時段") {
		t.Fatalf("時段寫反要列成問題：%v", p)
	}
}

// Guards: REQ-198

// Guards: REQ-101, REQ-198

// Guards: REQ-198

// Guards: REQ-194
func TestScheduledOnCountsTodayRuns(t *testing.T) {
	h := homefixture.New(t)
	calls := []string{}
	v := &fakeVault{records: []vault.Record{job("0 15 * * *", "skip")}, calls: &calls}
	m := newRig(t, h, &fakeBus{calls: &calls}, v)
	if !m.ScheduledOn(context.Background(), "code", h.Clock.Now()) {
		t.Fatal("每天 15:00 的排程，今天有")
	}
	weekly := job("0 15 * * 1", "skip") // 週一
	v.records = []vault.Record{weekly}
	today := h.Clock.Now()
	got := m.ScheduledOn(context.Background(), "code", today)
	want := today.In(m.Location()).Weekday() == time.Monday
	if got != want {
		t.Fatalf("週一的排程：今天是 %s，got %v want %v", today.In(m.Location()).Weekday(), got, want)
	}
}

func TestLegacySitesNeverFilterLocalJobs(t *testing.T) {
	m, c, _, v := setup(t)
	for _, tc := range []struct {
		id    string
		sites any
	}{{"remote", []any{"office"}}, {"malformed", 42}, {"empty", []any{}}} {
		r := sitedJob(tc.id, nil)
		r.Fields["sites"] = tc.sites
		v.records = append(v.records, r)
	}
	if err := m.reload(context.Background(), c.Now()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"remote", "malformed", "empty"} {
		if next, ok := m.Next(id); !ok || next.Before(c.Now()) {
			t.Fatalf("舊 sites 不應擋住 %s", id)
		}
	}
}
