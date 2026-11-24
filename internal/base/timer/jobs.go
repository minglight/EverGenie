package timer

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
)

// JobRunner 跑各業務模組用 Jobs() 登記的定時工作（app.Job）：app 組裝時經 ConsumeJobs 交進來，Start 時每項開一條
// goroutine，Stop 等全部結束。它放在 app 清單的最後面（所有業務模組之後），所以業務模組都啟動好了才開始跑、關機時最先停。
// 同一項上一次還沒跑完就略過這一次（不疊加）；Run 回錯或 panic 只寫 log，下一次照跑。
// 這跟 Scheduler 分開：Scheduler 是使用者資料（排程、提醒、任務單跟催）的到期判斷，JobRunner 是程式內建的週期工作。
type JobRunner struct {
	mu     sync.Mutex
	home   string
	jobs   []app.Job
	log    *slog.Logger
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewJobRunner 建一個還沒啟動的定時工作執行器。
func NewJobRunner() *JobRunner { return &JobRunner{} }

// Name 讓 app 的啟停清單認得它。
func (r *JobRunner) Name() string { return "jobs-runner" }

// SetHome 指定家（Cron 的時區、log）；沒指定就不啟動。
func (r *JobRunner) SetHome(dir string) { r.mu.Lock(); defer r.mu.Unlock(); r.home = dir }

// ConsumeJobs 收下合併後的定時工作（app.JobConsumer）；Cron 寫錯在這裡就擋下。
func (r *JobRunner) ConsumeJobs(jobs []app.Job) error {
	for _, j := range jobs {
		if j.Cron != "" {
			if _, err := ParseCron([]string{j.Cron}, time.UTC); err != nil {
				return err
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append([]app.Job(nil), jobs...)
	return nil
}

var _ app.JobConsumer = (*JobRunner)(nil)

// Start 每項工作開一條 goroutine 後立刻回傳（MOD-4）。
func (r *JobRunner) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.home == "" || r.cancel != nil {
		return nil
	}
	h, err := platform.LoadHome(r.home)
	if err != nil {
		return err
	}
	loc := h.Location()
	r.log = platform.NewLogger(h, "jobs-runner")
	run, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	for _, j := range r.jobs {
		next, err := nextFunc(j, loc)
		if err != nil {
			cancel()
			r.cancel = nil
			return err
		}
		r.wg.Add(1)
		go r.loop(run, j, next)
	}
	return nil
}

// nextFunc 回「從 now 起，下一次在多久之後」：第一次（first 為真）用 First（零值＝Every），之後每 Every；Cron 照家的時區算。
func nextFunc(j app.Job, loc *time.Location) (func(now time.Time, first bool) time.Duration, error) {
	if j.Cron != "" {
		c, err := ParseCron([]string{j.Cron}, loc)
		if err != nil {
			return nil, err
		}
		return func(now time.Time, _ bool) time.Duration {
			at := c.Next(now)
			if at.IsZero() {
				return -1
			}
			return at.Sub(now)
		}, nil
	}
	return func(_ time.Time, first bool) time.Duration {
		if first && j.First > 0 {
			return j.First
		}
		return j.Every
	}, nil
}

// loop 跑一項工作：等到下一次、跑完（同步，所以不會疊加）、再算下一次。
func (r *JobRunner) loop(ctx context.Context, j app.Job, next func(time.Time, bool) time.Duration) {
	defer r.wg.Done()
	first := true
	for {
		wait := next(time.Now(), first)
		first = false
		if wait < 0 {
			return // cron 排不出下一次
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		r.runOnce(ctx, j)
	}
}

func (r *JobRunner) runOnce(ctx context.Context, j app.Job) {
	defer func() {
		if recover() != nil {
			r.warn("定時工作中斷", j.Name)
		}
	}()
	if err := j.Run(ctx); err != nil && ctx.Err() == nil {
		r.warn("定時工作失敗", j.Name)
	}
}

func (r *JobRunner) warn(msg, name string) {
	r.mu.Lock()
	log := r.log
	r.mu.Unlock()
	if log != nil {
		log.Warn(msg, "job", name)
	}
}

// Stop 停掉所有工作並等它們結束（ctx 逾時就放手）。
func (r *JobRunner) Stop(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
