package timer

import "time"

// catchupWindow：睡太久不補跑；六小時內只補最近一次（SCH-5）。
const catchupWindow = 6 * time.Hour

// Entry 是一個排程項目的計時狀態：下一次觸發、上一次觸發、（時段型）這次所屬時段的結束。
// 三種：單次（NewAt）、cron（NewCron）、時段型（NewWindow）。Entry 不是並行安全的，呼叫者自己加鎖。
type Entry struct {
	id         string
	cron       *Cron
	win        *Window
	once       bool
	next, last time.Time
	end        time.Time
}

// NewAt 是單次項目：t 到了觸發一次。
func NewAt(id string, t time.Time) *Entry { return &Entry{id: id, once: true, next: t} }

// NewCron 是重複項目：下一次是 now 之後最早的 cron 時間。
func NewCron(id string, c *Cron, now time.Time) *Entry {
	return &Entry{id: id, cron: c, next: c.Next(now)}
}

// NewWindow 是時段型項目：下一次是 now 之後最早的時段觸發點（找不到就沒有下一次）。
func NewWindow(id string, w *Window, now time.Time, loc *time.Location) *Entry {
	e := &Entry{id: id, win: w}
	if s, ok := w.Next(id, now, loc); ok {
		e.next, e.end = s.Start, s.End
	}
	return e
}

// Next 是下一次觸發時間；沒有下一次回零值。
func (e *Entry) Next() time.Time { return e.next }

// Window 回時段型項目的設定；不是時段型回 nil。
func (e *Entry) Window() *Window { return e.win }

// CarryLast 把舊項目的「上一次觸發」帶過來（重載後同一分鐘不重複觸發）。prev 可以是 nil。
func (e *Entry) CarryLast(prev *Entry) {
	if prev != nil {
		e.last = prev.last
	}
}

// MarkFired 記下這一次真的觸發了（at 是 Due 回的 At）。被額度擋下就不要呼叫。
func (e *Entry) MarkFired(at time.Time) { e.last = at }

// Due 是一次到期判斷的結果。
type Due struct {
	At        time.Time // 這次的觸發時間（補跑時是最近一次）
	Fire      bool      // 該不該觸發
	Missed    bool      // 錯過了（呼叫者要發「錯過」事件，CaughtUp 說明有沒有補）
	CaughtUp  bool
	WindowEnd time.Time // 時段型：這次所屬時段的結束
	Once      bool      // 單次項目：呼叫者要把它移除
}

// Due 判斷 now 時這個項目有沒有到期；沒到期回 false 且不改任何狀態。
// 到期就同時把項目推進到下一次。lastTick 是上一次 tick 的時間（判斷睡了多久）；
// catchup 為 true 時，cron 項目錯過的時間在六小時內補跑最近一次，否則略過；
// loc 是排下一次時段用的時區。
func (e *Entry) Due(now, lastTick time.Time, catchup bool, loc *time.Location) (Due, bool) {
	if e.next.IsZero() || e.next.After(now) {
		return Due{}, false
	}
	at := e.next
	d := Due{At: at, Fire: true}
	switch {
	case e.once:
		d.Once = true
	case e.win != nil:
		// 時段型：時段內晚了幾分鐘照樣送，時段過了就略過今天這一次。
		end := e.end
		if now.After(end) {
			d.Fire, d.Missed = false, true
		}
		if s, ok := e.win.Next(e.id, now, loc); ok {
			e.next, e.end = s.Start, s.End
		} else {
			e.next = time.Time{}
		}
		d.WindowEnd = end
	default:
		if now.Truncate(time.Minute).After(at.Truncate(time.Minute)) {
			// 睡太久不補；六小時內只補最近一次。
			d.Fire = catchup && now.Sub(lastTick) <= catchupWindow
			if d.Fire {
				for next := e.cron.Next(at); !next.IsZero() && !next.After(now); next = e.cron.Next(at) {
					at = next
				}
			}
			d.Missed, d.CaughtUp = true, d.Fire
		}
		e.next = e.cron.Next(now)
		if !e.last.IsZero() && e.last.Truncate(time.Minute).Equal(at.Truncate(time.Minute)) {
			d.Fire = false
		}
		d.At = at
	}
	return d, true
}
