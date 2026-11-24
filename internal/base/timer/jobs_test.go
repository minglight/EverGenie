package timer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/vault/homefixture"
)

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("等不到：%s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// JobRunner：Every 照間隔一直跑（First 決定第一次）；回錯、panic 都不影響下一次；上一次沒跑完不疊加；Stop 等全部結束。
func TestJobRunnerRunsEveryJob(t *testing.T) {
	var ticks, failing, overlap, running, maxRunning atomic.Int32
	r := NewJobRunner()
	r.SetHome(homefixture.New(t).Dir)
	err := r.ConsumeJobs([]app.Job{
		{Name: "a.tick", Every: 10 * time.Millisecond, First: time.Millisecond, Run: func(context.Context) error { ticks.Add(1); return nil }},
		{Name: "b.fail", Every: 10 * time.Millisecond, Run: func(context.Context) error {
			if failing.Add(1)%2 == 0 {
				panic("壞")
			}
			return errors.New("失敗")
		}},
		{Name: "c.slow", Every: time.Millisecond, Run: func(ctx context.Context) error {
			n := running.Add(1)
			if n > maxRunning.Load() {
				maxRunning.Store(n)
			}
			overlap.Add(1)
			select {
			case <-time.After(20 * time.Millisecond):
			case <-ctx.Done():
			}
			running.Add(-1)
			return nil
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "每項都跑了好幾次", func() bool { return ticks.Load() >= 3 && failing.Load() >= 3 && overlap.Load() >= 2 })
	if err := r.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if maxRunning.Load() != 1 {
		t.Fatalf("同一項不疊加：最多同時 %d", maxRunning.Load())
	}
	after := ticks.Load()
	time.Sleep(30 * time.Millisecond)
	if ticks.Load() != after {
		t.Fatal("Stop 之後不再跑")
	}
}

// Cron 寫錯在組裝時擋下；沒設家不啟動。
func TestJobRunnerRejectsBadCronAndNeedsHome(t *testing.T) {
	r := NewJobRunner()
	if err := r.ConsumeJobs([]app.Job{{Name: "x", Cron: "壞掉", Run: func(context.Context) error { return nil }}}); err == nil {
		t.Fatal("cron 寫錯要回錯")
	}
	var n atomic.Int32
	if err := r.ConsumeJobs([]app.Job{{Name: "x", Every: time.Millisecond, Run: func(context.Context) error { n.Add(1); return nil }}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	if n.Load() != 0 {
		t.Fatal("沒設家不啟動")
	}
	_ = r.Stop(context.Background())
}

// Cron 的下一次照家的時區算；First 只用在第一次。
func TestJobRunnerNext(t *testing.T) {
	next, err := nextFunc(app.Job{Cron: "30 * * * *"}, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if got := next(time.Date(2026, 11, 1, 9, 10, 0, 0, time.UTC), true); got != 20*time.Minute {
		t.Fatalf("cron：%s", got)
	}
	every, _ := nextFunc(app.Job{Every: time.Hour, First: time.Minute}, time.UTC)
	if every(time.Time{}, true) != time.Minute || every(time.Time{}, false) != time.Hour {
		t.Fatal("First 只用在第一次")
	}
	plain, _ := nextFunc(app.Job{Every: time.Hour}, time.UTC)
	if plain(time.Time{}, true) != time.Hour {
		t.Fatal("沒有 First 第一次也等一個 Every")
	}
}
