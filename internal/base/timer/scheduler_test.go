package timer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// recorder 記下計時迴圈交給 Handler 的每一件事。
type recorder struct {
	mu     sync.Mutex
	fired  []string
	missed []string
	deny   map[string]bool
	after  int
}

func (r *recorder) Missed(_ context.Context, it *Item, _ Due) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.missed = append(r.missed, it.ID)
}
func (r *recorder) Allow(_ context.Context, _ time.Time, it *Item) bool { return !r.deny[it.ID] }
func (r *recorder) Fire(_ context.Context, _ time.Time, it *Item, _ Due) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fired = append(r.fired, it.ID+":"+it.Kind)
}
func (r *recorder) AfterTick(context.Context, time.Time) { r.mu.Lock(); r.after++; r.mu.Unlock() }

func newRig() (*Scheduler, *platform.FakeClock, *recorder) {
	clock := platform.NewFakeClock(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	s := NewScheduler(clock)
	r := &recorder{deny: map[string]bool{}}
	s.SetHandler(r)
	return s, clock, r
}

func step(s *Scheduler, c *platform.FakeClock, d time.Duration) {
	c.Advance(d)
	s.Tick(context.Background(), c.Now())
}

// 一個 * * * * * 的 cron，快轉 10 分鐘恰好觸發 10 次；同一分鐘 tick 兩次只算一次。
func TestSchedulerCronFiresOncePerMinute(t *testing.T) {
	s, c, r := newRig()
	if err := s.Cron(context.Background(), "tick", []string{"* * * * *"}, CronOpt{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		step(s, c, time.Minute)
		s.Tick(context.Background(), c.Now())
	}
	if len(r.fired) != 10 || r.fired[0] != "tick:cron" {
		t.Fatalf("fired=%v", r.fired)
	}
}

// 單次項目觸發一次就被移除；過去的時間下一次 tick 就觸發；取消不存在的回 NotFound。
func TestSchedulerAtFiresOnceThenRemoved(t *testing.T) {
	s, c, r := newRig()
	ctx := context.Background()
	if err := s.At(ctx, "once", c.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.At(ctx, "past", c.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	s.Tick(ctx, c.Now())
	step(s, c, time.Minute)
	step(s, c, time.Minute)
	if len(r.fired) != 2 || r.fired[0] != "past:at" || r.fired[1] != "once:at" {
		t.Fatalf("fired=%v", r.fired)
	}
	if _, ok := s.Next("once"); ok {
		t.Fatal("單次項目觸發後還在")
	}
	if platform.CodeOf(s.Cancel(ctx, "once")) != sdk.ErrorCodeNotFound {
		t.Fatal("取消不存在的項目要回 NotFound")
	}
}

// Allow 回 false 的項目不觸發、不記為已觸發（下一輪還會問）。
func TestSchedulerAllowDenyDoesNotFire(t *testing.T) {
	s, c, r := newRig()
	if err := s.Cron(context.Background(), "x", []string{"* * * * *"}, CronOpt{}); err != nil {
		t.Fatal(err)
	}
	r.deny["x"] = true
	step(s, c, time.Minute)
	if len(r.fired) != 0 {
		t.Fatalf("被擋下的項目不該觸發：%v", r.fired)
	}
	r.deny["x"] = false
	step(s, c, time.Minute)
	if len(r.fired) != 1 || r.after != 2 {
		t.Fatalf("fired=%v after=%d", r.fired, r.after)
	}
}

// 錯過的項目：catchup once 在六小時內補跑一次並回報 Missed，skip 只回報錯過。
func TestSchedulerCatchup(t *testing.T) {
	for _, tc := range []struct {
		catchup string
		fired   int
	}{{"once", 1}, {"skip", 0}} {
		s, c, r := newRig()
		if err := s.Cron(context.Background(), "job", []string{"0 13 * * *"}, CronOpt{Catchup: tc.catchup}); err != nil {
			t.Fatal(err)
		}
		c.Advance(3 * time.Hour) // 睡過頭：整點已經過了兩小時
		s.Tick(context.Background(), c.Now())
		if len(r.fired) != tc.fired || len(r.missed) != 1 {
			t.Fatalf("%s: fired=%v missed=%v", tc.catchup, r.fired, r.missed)
		}
	}
}

// Sync 只換掉同一種類的項目，其他種類（Cron／At 登記的）不動；沒有 Handler 時到期的項目留著。
func TestSchedulerSyncReplacesOnlyOneKind(t *testing.T) {
	s, c, r := newRig()
	ctx := context.Background()
	if err := s.Cron(ctx, "other", []string{"* * * * *"}, CronOpt{}); err != nil {
		t.Fatal(err)
	}
	mk := func(id string) *Item {
		it, err := NewCronItem(id, []string{"* * * * *"}, time.UTC, CronOpt{}, c.Now())
		if err != nil {
			t.Fatal(err)
		}
		return it
	}
	s.Sync("job", func(prev map[string]*Item) map[string]*Item {
		if len(prev) != 0 {
			t.Fatalf("prev=%v", prev)
		}
		return map[string]*Item{"a": mk("a"), "b": mk("b")}
	})
	s.Sync("job", func(prev map[string]*Item) map[string]*Item {
		if len(prev) != 2 || prev["a"] == nil {
			t.Fatalf("prev=%v", prev)
		}
		return map[string]*Item{"a": prev["a"]}
	})
	step(s, c, time.Minute)
	if len(r.fired) != 2 || r.fired[0] != "a:job" || r.fired[1] != "other:cron" {
		t.Fatalf("fired=%v", r.fired)
	}
	if _, ok := s.Next("b"); ok {
		t.Fatal("b 應該被換掉了")
	}

	bare := NewScheduler(c)
	if err := bare.At(ctx, "x", c.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	bare.Tick(ctx, c.Now())
	if _, ok := bare.Next("x"); !ok {
		t.Fatal("沒接 Handler 時到期的項目不該被吃掉")
	}
}

// 登記時的錯誤：沒名稱、cron 解析不了、catchup 不認得都回 Invalid。
func TestSchedulerCronRejectsBadInput(t *testing.T) {
	s, _, _ := newRig()
	for _, tc := range []struct {
		id    string
		specs []string
		opt   CronOpt
	}{{"", []string{"* * * * *"}, CronOpt{}}, {"x", nil, CronOpt{}}, {"x", []string{"bad"}, CronOpt{}}, {"x", []string{"* * * * *"}, CronOpt{Catchup: "always"}}} {
		if platform.CodeOf(s.Cron(context.Background(), tc.id, tc.specs, tc.opt)) != sdk.ErrorCodeInvalid {
			t.Errorf("%+v 應該回 Invalid", tc)
		}
	}
	if platform.CodeOf(s.At(context.Background(), "", time.Now())) != sdk.ErrorCodeInvalid {
		t.Error("At 沒有名稱要回 Invalid")
	}
}

// 沒設家就不啟動（Start／Stop 都是空動作）。
func TestSchedulerStartWithoutHomeIsNoop(t *testing.T) {
	s, _, _ := newRig()
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
}
