package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/fake"
	"github.com/minglight/evergenie/sdk"
)

type alertBus struct {
	mu     sync.Mutex
	alerts []map[string]any
}

func (b *alertBus) Publish(_ context.Context, topic string, p map[string]any) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if topic == "system.alert" {
		b.alerts = append(b.alerts, p)
	}
	return nil
}
func (b *alertBus) count() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.alerts) }

func usageModule(t *testing.T) (*Module, *fake.Engine, *platform_clock, *alertBus, func()) {
	t.Helper()
	m, api, clock, _, exit := supervised(t)
	bus := &alertBus{}
	m.d.Bus = bus
	return m, api, &platform_clock{clock}, bus, exit
}

// platform_clock 只是為了讓測試簽名短一點。
type platform_clock struct{ c interface{ Now() time.Time } }

func (c *platform_clock) Now() time.Time { return c.c.Now() }

// Guards: REQ-86, REQ-166
func TestUsagesReportsEachAccount(t *testing.T) {
	m, api, clock, _, exit := usageModule(t)
	ctx := context.Background()
	now := clock.Now()

	got, err := m.Usages(ctx, "")
	check(t, err)
	if len(got) != 2 || !strings.Contains(got[0].Note, "還沒有用量資料") {
		t.Fatalf("沒有資料的帳號要說明原因：%+v", got)
	}
	api.Configure(fake.Behavior{Delivered: true, Usage: &sdk.EngineUsageResponse{UsedRatio: 0.4, ResetsAt: now.Add(time.Hour), MeasuredAt: now,
		Windows: []sdk.UsageWindow{{Kind: sdk.UsageWindowKindShort, Hours: 5, UsedRatio: 0.4, ResetsAt: now.Add(time.Hour)}}}})
	got, err = m.Usages(ctx, "two")
	check(t, err)
	if len(got) != 1 || got[0].Account != "two" || got[0].UsedRatio != 0.4 || len(got[0].Windows) != 1 || got[0].Note != "" {
		t.Fatalf("只問 two：%+v", got)
	}
	if _, err := m.Usages(ctx, "nobody"); err == nil {
		t.Fatal("不存在的帳號要回錯誤")
	}
	api.Configure(fake.Behavior{Delivered: true, UsageError: "引擎沒回應"})
	got, _ = m.Usages(ctx, "one")
	if !strings.Contains(got[0].Note, "引擎沒回應") {
		t.Fatalf("引擎回錯誤要放進說明：%+v", got)
	}
	exit()
	m.Tick(ctx, now)
	got, _ = m.Usages(ctx, "one")
	if got[0].Note != NotRunning {
		t.Fatalf("引擎沒在跑：%+v", got)
	}
}

// Guards: REQ-86
func TestWarmupOnlyFlaggedAccounts(t *testing.T) {
	m, api, _, bus, _ := usageModule(t)
	ctx := context.Background()
	lines, err := m.Warmup(ctx)
	check(t, err)
	if len(lines) != 0 {
		t.Fatalf("沒有帳號標暖機，不該做事：%v", lines)
	}
	m.registry.accounts["one"] = account{engine: m.registry.accounts["one"].engine, config: m.registry.accounts["one"].config, warmup: true}
	lines, err = m.Warmup(ctx)
	check(t, err)
	if len(lines) != 1 || lines[0] != "one：OK" || bus.count() != 0 {
		t.Fatalf("暖機：%v alerts=%d", lines, bus.count())
	}
	warm := 0
	for _, c := range api.Calls() {
		if r, ok := c.Request.(*sdk.EngineUsageRequest); ok && r.Warmup {
			warm++
			if r.Account != "one" {
				t.Errorf("只暖機標了的帳號：%+v", r)
			}
		}
	}
	if warm != 1 {
		t.Fatalf("暖機請求 %d 次", warm)
	}
	api.Configure(fake.Behavior{Delivered: true, UsageError: "登入過期"})
	lines, _ = m.Warmup(ctx)
	if !strings.Contains(lines[0], "失敗") || bus.count() != 1 {
		t.Fatalf("失敗要通知：%v alerts=%d", lines, bus.count())
	}
}

// Guards: REQ-86
func TestLimitWatchNotifiesOnceAfterReset(t *testing.T) {
	m, api, clock, bus, _ := usageModule(t)
	ctx := context.Background()
	now := clock.Now() // 2026-10-24 10:00 UTC＝台北 18:00，不在安靜時段
	blocked := func(resets time.Time) fake.Behavior {
		return fake.Behavior{Delivered: true, Usage: &sdk.EngineUsageResponse{MeasuredAt: now,
			Windows: []sdk.UsageWindow{{Kind: sdk.UsageWindowKindShort, Hours: 5, UsedRatio: 0.99, ResetsAt: resets}}}}
	}
	api.Configure(blocked(now.Add(2 * time.Hour)))
	if _, err := m.LimitWatch(ctx, now); err != nil || bus.count() != 0 {
		t.Fatalf("還沒重置不通知：%v alerts=%d", err, bus.count())
	}
	// 被擋之後帳號沒有 session，快照不再更新（這裡用「沒有用量資料」模擬）：只靠記下來的重置時間。
	api.Configure(fake.Behavior{Delivered: true})
	if _, err := m.LimitWatch(ctx, now.Add(time.Hour)); err != nil || bus.count() != 0 {
		t.Fatalf("重置前不通知：%v alerts=%d", err, bus.count())
	}
	log, err := m.LimitWatch(ctx, now.Add(2*time.Hour+time.Minute))
	check(t, err)
	if bus.count() != 2 || !strings.Contains(strings.Join(log, ";"), "已通知") { // 兩個帳號都被擋
		t.Fatalf("重置後通知：%v alerts=%d", log, bus.count())
	}
	if _, err := m.LimitWatch(ctx, now.Add(3*time.Hour)); err != nil || bus.count() != 2 {
		t.Fatalf("只通知一次：%v alerts=%d", err, bus.count())
	}
}

// Guards: REQ-86
func TestLimitWatchQuietHoursAndStale(t *testing.T) {
	m, api, clock, bus, _ := usageModule(t)
	ctx := context.Background()
	now := clock.Now()
	api.Configure(fake.Behavior{Delivered: true, Usage: &sdk.EngineUsageResponse{MeasuredAt: now,
		Windows: []sdk.UsageWindow{{Kind: sdk.UsageWindowKindWeekly, Hours: 168, UsedRatio: 1, ResetsAt: now.Add(time.Hour)}}}})
	_, err := m.LimitWatch(ctx, now)
	check(t, err)
	api.Configure(fake.Behavior{Delivered: true})
	// 台北 23:30（UTC 15:30）：已過重置，但在安靜時段，先留著。
	quietAt := time.Date(2026, 10, 24, 15, 30, 0, 0, time.UTC)
	log, err := m.LimitWatch(ctx, quietAt)
	check(t, err)
	if bus.count() != 0 || !strings.Contains(strings.Join(log, ";"), "安靜時段") {
		t.Fatalf("安靜時段不通知：%v alerts=%d", log, bus.count())
	}
	// 重置超過 12 小時才處理：過期，不通知，記錄清掉。
	log, err = m.LimitWatch(ctx, now.Add(14*time.Hour))
	check(t, err)
	if bus.count() != 0 || !strings.Contains(strings.Join(log, ";"), "不通知") {
		t.Fatalf("太久以前的重置不通知：%v alerts=%d", log, bus.count())
	}
	if left := m.loadLimitState(); len(left) != 0 {
		t.Fatalf("過期的記錄要清掉：%v", left)
	}
}

// Guards: REQ-165
func TestPreflightMergesAccounts(t *testing.T) {
	m, api, _, _, _ := usageModule(t)
	api.Configure(fake.Behavior{Delivered: true, Preflight: []sdk.PreflightCheck{{ID: "tool-a", Name: "工具 A", Ok: true}, {ID: "login", Name: "已登入", Detail: "沒登入", Fix: "登入"}}})
	got, err := m.Preflight(context.Background())
	check(t, err)
	if len(got) != 2 {
		t.Fatalf("兩個帳號共用同一個引擎，同一個 id 只列一次：%+v", got)
	}
}
