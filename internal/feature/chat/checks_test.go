package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 狀態延遲：樣本由串流記下（Observe）；健檢「狀態延遲」寫今天的次數、p50／p90、超過 3 秒的次數，是綠字。
// 負的（兩台時鐘差一點點）當 0；昨天的不算進今天。串流怎麼量見 TestStateLatencyFromEngineEventToStream。
// Guards: REQ-390
func TestStateLatencyCheck(t *testing.T) {
	clock := platform.NewFakeClock(time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	c := New(Deps{Clock: clock})
	if got := c.stateLatencyCheck(); got.Status != app.StatusOK || !strings.Contains(got.Message, "還沒") {
		t.Fatalf("還沒有資料：%+v", got)
	}
	for _, took := range []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 5 * time.Second, -50 * time.Millisecond} {
		clock.Advance(time.Minute)
		c.samples.Observe(clock.Now(), took)
	}
	got := c.samples.Since(time.Time{})
	if len(got) != 4 || got[0] != 0 {
		t.Fatalf("負的當 0：%v", got)
	}
	check := c.stateLatencyCheck()
	if check.Status != app.StatusOK || !strings.Contains(check.Message, "4 次") || !strings.Contains(check.Message, "p50 0.2 秒") ||
		!strings.Contains(check.Message, "p90 5.0 秒") || !strings.Contains(check.Message, "超過 3 秒 1 次") {
		t.Fatalf("%+v", check)
	}
	clock.Advance(24 * time.Hour)
	if got := c.stateLatencyCheck(); !strings.Contains(got.Message, "還沒") {
		t.Fatalf("隔天從頭算：%+v", got)
	}
}

type fakeLatency struct {
	count, over int
	p50, p90    time.Duration
}

func (f fakeLatency) ReceiveLatency(context.Context) (int, int, time.Duration, time.Duration, error) {
	return f.count, f.over, f.p50, f.p90, nil
}

// 「送出→收到」：今天幾句、p50／p90、超過 3 秒幾句；只是資訊（綠字），不進健康推播。
// Guards: REQ-387
func TestReceiveLatencyCheck(t *testing.T) {
	c := latencyCheck(context.Background(), fakeLatency{count: 12, over: 2, p50: 1200 * time.Millisecond, p90: 4 * time.Second})
	if c.Status != app.StatusOK || !strings.Contains(c.Message, "12 句") || !strings.Contains(c.Message, "p50 1.2 秒") ||
		!strings.Contains(c.Message, "p90 4.0 秒") || !strings.Contains(c.Message, "超過 3 秒（或沒收到）2 句") {
		t.Fatalf("%+v", c)
	}
	if c := latencyCheck(context.Background(), fakeLatency{}); !strings.Contains(c.Message, "還沒有") {
		t.Fatalf("今天沒說話：%+v", c)
	}
}

type residentSessions struct {
	Sessions
	list []*sdk.Session
	err  error
}

func (s residentSessions) List(context.Context, string, []sdk.SessionState) ([]*sdk.Session, error) {
	return s.list, s.err
}

type residentRoster []*sdk.Record

func (r residentRoster) List(context.Context) ([]*sdk.Record, error) { return r, nil }

func checkNames(cs []app.Check) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// 常駐秘書：沒在跑一條黃字（S5 起不猜卡住，舊狀態 stuck 不報）；停用的與不是常駐的不算；都正常給一條綠字；讀不到 session 清單是黃字。
// 沒接名冊就不列這一項；session 沒有送達統計就不列 sessions.latency。
// Guards: REQ-117
func TestResidentCheck(t *testing.T) {
	roster := residentRoster{
		{ID: "code", Fields: map[string]any{"display_name": "雅典娜", "enabled": true}},
		{ID: "life", Fields: map[string]any{"enabled": true, "mode": "resident"}},
		{ID: "off", Fields: map[string]any{"enabled": false}},
		{ID: "art", Fields: map[string]any{"mode": "on_demand"}},
	}
	m := New(Deps{Sessions: residentSessions{list: []*sdk.Session{{Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionState("stuck")}}}, Roster: roster})
	if got := strings.Join(checkNames(m.Checks()), ","); got != "sessions.resident,sessions.state_latency" {
		t.Fatalf("登記的項目：%s", got)
	}
	got := m.checkResident(context.Background())
	if len(got) != 1 || got[0].Status != app.StatusWarn || !strings.Contains(got[0].Message, "life 沒有在跑") || got[0].Fix == "" {
		t.Fatalf("只有沒在跑的那位；舊狀態 stuck 不再報「可能卡住」：%+v", got)
	}
	ok := New(Deps{Sessions: residentSessions{list: []*sdk.Session{
		{Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle},
		{Agent: "life", Kind: sdk.SessionKindResident, State: sdk.SessionStateBusy},
	}}, Roster: roster})
	if got := ok.checkResident(context.Background()); len(got) != 1 || got[0].Status != app.StatusOK || !strings.Contains(got[0].Message, "2 位常駐秘書都正常") {
		t.Fatalf("都正常：%+v", got)
	}
	bad := New(Deps{Sessions: residentSessions{err: errors.New("x")}, Roster: roster})
	if got := bad.checkResident(context.Background()); len(got) != 1 || got[0].Status != app.StatusWarn || !strings.Contains(got[0].Message, "讀不到 session 清單") {
		t.Fatalf("讀不到清單：%+v", got)
	}
	if got := strings.Join(checkNames(New(Deps{}).Checks()), ","); got != "sessions.state_latency" {
		t.Fatalf("沒接名冊只剩狀態延遲：%s", got)
	}
}

type promptSessions struct {
	residentSessions
	prompts []*sdk.BootPrompt
	err     error
}

func (p promptSessions) BootPrompts(context.Context) ([]*sdk.BootPrompt, error) {
	return p.prompts, p.err
}

// 卡在啟動對話框（信任資料夾）的秘書：狀態是「待命」也要列成黃字，而且不能再說「都正常」；
// 查不到對話框（引擎沒回）就當沒有，不擋其他項目。
// Guards: DEV-87
func TestResidentCheckFlagsBootPrompt(t *testing.T) {
	roster := residentRoster{
		{ID: "code", Fields: map[string]any{"display_name": "雅典娜", "enabled": true}},
		{ID: "life", Fields: map[string]any{"enabled": true}},
	}
	base := residentSessions{list: []*sdk.Session{
		{Name: "code", Agent: "code", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle},
		{Name: "life", Agent: "life", Kind: sdk.SessionKindResident, State: sdk.SessionStateIdle},
	}}
	stuck := New(Deps{Sessions: promptSessions{residentSessions: base, prompts: []*sdk.BootPrompt{
		{ID: "code#trust", Session: "code", Title: "第一次在這個資料夾啟動：要信任它嗎？"},
	}}, Roster: roster})
	got := stuck.checkResident(context.Background())
	if len(got) != 1 || got[0].Status != app.StatusWarn || !strings.Contains(got[0].Message, "雅典娜（code） 卡住了：") ||
		!strings.Contains(got[0].Message, "要信任它嗎") || got[0].Fix == "" {
		t.Fatalf("卡在信任框要列黃字：%+v", got)
	}
	failing := New(Deps{Sessions: promptSessions{residentSessions: base, err: errors.New("引擎沒回")}, Roster: roster})
	if got := failing.checkResident(context.Background()); len(got) != 1 || got[0].Status != app.StatusOK || !strings.Contains(got[0].Message, "2 位常駐秘書都正常") {
		t.Fatalf("查不到對話框不擋其他項目：%+v", got)
	}
}

type fakeStuck struct {
	residentSessions
	stuck []platform.SessionStuck
}

func (f fakeStuck) Stuck(context.Context) []platform.SessionStuck { return f.stuck }

// 話送不進去（DEV-162）：每個卡住的對話一條紅字（要處理）——哪位秘書、幾則、多久，帶下一步；都沒有是綠字。
// session 沒有這個查詢就不列這一項。
func TestStuckCheck(t *testing.T) {
	m := New(Deps{Sessions: fakeStuck{stuck: []platform.SessionStuck{
		{Name: "admin", Agent: "admin", AgentName: "輝夜", Count: 3, Minutes: 25},
		{Name: "code-sec-1", Agent: "code", Count: 1, Minutes: 25},
	}}})
	var check *app.Check
	for _, c := range m.Checks() {
		if c.Name == "sessions.stuck" {
			check = &c
		}
	}
	if check == nil {
		t.Fatalf("要登記 sessions.stuck：%v", checkNames(m.Checks()))
	}
	got := check.RunAll(context.Background())
	if len(got) != 2 || got[0].Status != app.StatusError || got[0].Message != "輝夜有 3 則話送不進去，已經等了 25 分鐘" ||
		!strings.Contains(got[0].Fix, "重開對話") || !strings.Contains(got[0].Fix, platform.RescueName) {
		t.Fatalf("卡住是紅字，寫出誰、幾則、多久與下一步：%+v", got)
	}
	if got[1].Status != app.StatusError || got[1].Message != "code（code-sec-1）有 1 則話送不進去，已經等了 25 分鐘" {
		t.Fatalf("不是主要對話要寫出是哪個對話：%+v", got[1])
	}
	if got := stuckCheck(context.Background(), fakeStuck{}); len(got) != 1 || got[0].Status != app.StatusOK {
		t.Fatalf("沒有卡住是綠字：%+v", got)
	}
	if names := strings.Join(checkNames(New(Deps{Sessions: residentSessions{}}).Checks()), ","); strings.Contains(names, "sessions.stuck") {
		t.Fatalf("session 沒有這個查詢就不列：%s", names)
	}
}
