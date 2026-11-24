package timer

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Clock 是計時迴圈的時間來源（platform.Clock 的子集；測試用 platform.FakeClock）。
type Clock interface{ Now() time.Time }

// CronOpt 是 Cron 的選項。
type CronOpt struct {
	Catchup string // "once"｜"skip"｜空（等於 skip）
	Agent   string
}

// Item 是計時迴圈裡的一個項目：計時狀態在 T，其餘是擁有者（呼叫 Cron／At 的人，或 jobs）的資料。
// timer 不看 Data。
type Item struct {
	ID      string
	Kind    string // "cron"｜"at"｜擁有者自訂（jobs 用 "job"）
	Agent   string
	Catchup string
	Data    any
	T       *Entry
}

// Handler 是到期之後要做的事：timer 只決定「哪個項目到期了」，發事件、額度都是擁有者的事。
// 四個方法都在沒有 Scheduler 鎖的情況下被呼叫，可以放心呼叫 Scheduler 的方法。
type Handler interface {
	// Missed：項目錯過了（d.CaughtUp 說明有沒有補跑）。
	Missed(ctx context.Context, it *Item, d Due)
	// Allow：該觸發的項目在觸發前先問一次（例如每日額度）；回 false 就不觸發、也不記為已觸發。
	Allow(ctx context.Context, now time.Time, it *Item) bool
	// Fire：項目觸發了。
	Fire(ctx context.Context, now time.Time, it *Item, d Due)
	// AfterTick：每次 tick 的最後，給擁有者做每秒一次的收尾（jobs 現在不做事；計時不能改狀態、發通知，S5）。
	AfterTick(ctx context.Context, now time.Time)
}

// Scheduler 是單一計時迴圈：每秒一次，照 Cron／At 登記的項目（以及擁有者用 Sync 放進來的項目）判斷到期，
// 到期就交給 Handler。它不發事件、不讀 vault，所以 tasks（Cron）、reminders（At）、jobs（使用者排程）共用同一個。
// 說明卡 ai_docs/modules/timer.md。
type Scheduler struct {
	clock Clock

	mu       sync.Mutex
	home     string
	loc      *time.Location
	log      *slog.Logger
	items    map[string]*Item
	handler  Handler
	lastTick time.Time
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// NewScheduler 建一個還沒啟動的計時迴圈；clock 空的用系統時間。
func NewScheduler(clock Clock) *Scheduler {
	if clock == nil {
		clock = platform.SystemClock{}
	}
	return &Scheduler{clock: clock, loc: time.UTC, items: map[string]*Item{}, lastTick: clock.Now()}
}

// Name 讓 registry 的啟停清單認得它。
func (s *Scheduler) Name() string { return "timer" }

// SetHome 指定家的位置：Start 從 home.toml 讀時區。沒指定就不啟動（測試直接呼叫 Tick）。
func (s *Scheduler) SetHome(dir string) { s.mu.Lock(); defer s.mu.Unlock(); s.home = dir }

// SetHandler 接上到期後的處理；沒接之前到期的項目留著，不會被吃掉。
func (s *Scheduler) SetHandler(h Handler) { s.mu.Lock(); defer s.mu.Unlock(); s.handler = h }

// SetLocation 直接指定時區：正常由 Start 從家設定讀；測試不啟動迴圈、用假時鐘時用這個。
func (s *Scheduler) SetLocation(loc *time.Location) { s.mu.Lock(); defer s.mu.Unlock(); s.loc = loc }

// Location 是家的時區（Start 之前是 UTC）。
func (s *Scheduler) Location() *time.Location {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loc
}

// Start 讀時區、開每秒一次的 tick 迴圈後立刻回傳（MOD-4）。
func (s *Scheduler) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.home == "" {
		return nil
	}
	if s.cancel != nil {
		return platform.Conflict("排程服務已啟動")
	}
	h, err := platform.LoadHome(s.home)
	if err != nil {
		return err
	}
	h.Clock = s.clock
	s.loc = h.Location()
	s.log = platform.NewLogger(h, "timer")
	s.lastTick = s.clock.Now()
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.recoverWorker()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-run.Done():
				return
			case <-t.C:
				s.Tick(run, s.clock.Now())
			}
		}
	}()
	return nil
}

func (s *Scheduler) recoverWorker() {
	if recover() != nil {
		s.mu.Lock()
		log := s.log
		s.mu.Unlock()
		if log != nil {
			log.Error("timer worker failed")
		}
	}
}

// Stop 停掉 tick 迴圈並等它結束。
func (s *Scheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	s.wg.Wait()
	s.mu.Lock()
	s.cancel = nil
	s.mu.Unlock()
	return nil
}

// NewCronItem 建一個 cron 項目（還沒放進迴圈）；jobs 重載時用它組新的清單。
func NewCronItem(id string, specs []string, loc *time.Location, opt CronOpt, now time.Time) (*Item, error) {
	if id == "" || len(specs) == 0 {
		return nil, platform.Invalid("cron", "請填寫工作名稱與排程")
	}
	if opt.Catchup != "" && opt.Catchup != "skip" && opt.Catchup != "once" {
		return nil, platform.Invalid("catchup", "請選擇補跑一次或略過")
	}
	parsed, err := ParseCron(specs, loc)
	if err != nil {
		return nil, err
	}
	return &Item{ID: id, Kind: "cron", Agent: opt.Agent, Catchup: opt.Catchup, T: NewCron(id, parsed, now)}, nil
}

// NewWindowItem 建一個時段型項目（還沒放進迴圈）。
func NewWindowItem(id string, w *Window, loc *time.Location, agent string, now time.Time) *Item {
	return &Item{ID: id, Kind: "cron", Agent: agent, T: NewWindow(id, w, now, loc)}
}

// Cron 登記（或換掉）一個重複項目；同一個 id 再登記時帶走上一次觸發的記錄。
func (s *Scheduler) Cron(ctx context.Context, id string, specs []string, opt CronOpt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, err := NewCronItem(id, specs, s.loc, opt, s.clock.Now())
	if err != nil {
		return err
	}
	it.T.CarryLast(s.itemEntry(id))
	s.items[id] = it
	return nil
}

func (s *Scheduler) itemEntry(id string) *Entry {
	if old := s.items[id]; old != nil {
		return old.T
	}
	return nil
}

// At 登記一個單次項目（例如待辦到期）；觸發一次就移除。
func (s *Scheduler) At(ctx context.Context, id string, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if id == "" {
		return platform.Invalid("id", "請填寫排程名稱")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[id] = &Item{ID: id, Kind: "at", T: NewAt(id, t)}
	return nil
}

// Cancel 取消一個項目；沒有這個項目回 NotFound。
func (s *Scheduler) Cancel(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return platform.NotFound("找不到這個排程")
	}
	delete(s.items, id)
	return nil
}

// Next 是項目的下一次觸發時間；沒有這個項目或排不出來回 false。
func (s *Scheduler) Next(id string) (time.Time, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok || it.T.Next().IsZero() {
		return time.Time{}, false
	}
	return it.T.Next(), true
}

// Sync 整批換掉某一種（Kind）的項目：fn 拿到目前這一種的項目（id → 項目），回新的清單；其他種類不動。
// 擁有者先把會失敗的檢查做完再呼叫，fn 只做計算（它在鎖裡跑，不要呼叫 Scheduler 的方法）。
// 回傳的項目 Kind 會被強制設成 kind。
func (s *Scheduler) Sync(kind string, fn func(prev map[string]*Item) map[string]*Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := map[string]*Item{}
	for id, it := range s.items {
		if it.Kind == kind {
			prev[id] = it
		}
	}
	next := fn(prev)
	for id := range prev {
		delete(s.items, id)
	}
	for id, it := range next {
		it.Kind = kind
		s.items[id] = it
	}
}

// Tick 判斷 now 時哪些項目到期（id 排序），交給 Handler。測試用假時鐘直接呼叫；Start 之後每秒由迴圈呼叫。
func (s *Scheduler) Tick(ctx context.Context, now time.Time) {
	if ctx.Err() != nil {
		return
	}
	type fired struct {
		it *Item
		d  Due
	}
	s.mu.Lock()
	handler := s.handler
	if handler == nil {
		s.mu.Unlock()
		return
	}
	ids := make([]string, 0, len(s.items))
	for id := range s.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var dues []fired
	for _, id := range ids {
		it := s.items[id]
		d, ok := it.T.Due(now, s.lastTick, it.Catchup == "once", s.loc)
		if !ok {
			continue
		}
		if d.Once {
			delete(s.items, id)
		}
		dues = append(dues, fired{it, d})
	}
	s.mu.Unlock()

	for _, f := range dues {
		if f.d.Missed {
			handler.Missed(ctx, f.it, f.d)
		}
		if !f.d.Fire || !handler.Allow(ctx, now, f.it) {
			continue
		}
		s.mu.Lock()
		f.it.T.MarkFired(f.d.At)
		s.mu.Unlock()
		handler.Fire(ctx, now, f.it, f.d)
	}
	handler.AfterTick(ctx, now)

	s.mu.Lock()
	if now.After(s.lastTick) {
		s.lastTick = now
	}
	s.mu.Unlock()
}
