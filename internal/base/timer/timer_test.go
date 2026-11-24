package timer

import (
	"reflect"
	"testing"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

var loc = time.FixedZone("test", 8*3600)

func at(day, hour, min int) time.Time { return time.Date(2026, 10, day, hour, min, 0, 0, loc) }

// 多個 cron 取最早的下一次；NextRuns 由早到晚、重複算出的結果與 Cron.Next 一致。
func TestNextRunsMergesSpecsInOrder(t *testing.T) {
	specs := []string{"30 21 * * *", "0 8 * * 1"}
	after := at(23, 12, 0) // 2026-10-23 週五
	got, err := NextRuns(specs, loc, after, 3)
	if err != nil || len(got) != 3 {
		t.Fatalf("NextRuns：%v %v", got, err)
	}
	if !got[0].Equal(at(23, 21, 30)) || !got[1].Equal(at(24, 21, 30)) || !got[2].Equal(at(25, 21, 30)) {
		t.Fatalf("要由早到晚：%v", got)
	}
	c, err := ParseCron(specs, loc)
	if err != nil || !c.Next(after).Equal(got[0]) {
		t.Fatalf("Cron.Next 要跟 NextRuns 一致：%v %v", c.Next(after), err)
	}
}

// 壞掉的 cron 與空的 cron 都回 Invalid，不 panic。
func TestParseCronRejectsBadInput(t *testing.T) {
	for _, specs := range [][]string{nil, {}, {"nope"}, {"* * * *"}, {"0 9 * * *", "bad"}} {
		if _, err := ParseCron(specs, loc); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%q 應該回 Invalid：%v", specs, err)
		}
		if _, err := NextRuns(specs, loc, at(23, 12, 0), 1); err == nil {
			t.Errorf("NextRuns %q 應該回錯誤", specs)
		}
	}
}

// 時區跟著傳進去的 loc 走（每天 09:00 是那個時區的 09:00）。
func TestCronUsesGivenLocation(t *testing.T) {
	c, err := ParseCron([]string{"0 9 * * *"}, loc)
	if err != nil {
		t.Fatal(err)
	}
	next := c.Next(at(23, 10, 0))
	if h := next.In(loc).Hour(); h != 9 || next.In(loc).Day() != 24 {
		t.Fatalf("下一次要是隔天 09:00：%v", next)
	}
}

// 同一天、同一個工作，時段型算出來的時間固定、由早到晚、各落在自己的時段裡。
func TestWindowSlotsAreStable(t *testing.T) {
	w := &Window{Weekday: []string{"12:15-13:15", "15:30-17:00"}, PerDay: 2}
	day := time.Date(2026, 10, 26, 0, 0, 0, 0, loc)
	a, b := w.Slots("life/chat", day), w.Slots("life/chat", day)
	if len(a) != 2 || !reflect.DeepEqual(a, b) || !a[0].Start.Before(a[1].Start) {
		t.Fatalf("per_day=2 要兩段、固定、由早到晚：%v %v", a, b)
	}
	for _, s := range a {
		if !s.End.After(s.Start) {
			t.Errorf("結束要晚於觸發點：%v", s)
		}
	}
	if len(w.Slots("life/other", day)) != 2 {
		t.Fatal("換一個工作 id 也要有兩段")
	}
}

// 週末有自己的時段就用週末的，沒有就沿用平日的。
func TestWindowWeekendOverride(t *testing.T) {
	w := &Window{Weekday: []string{"12:15-13:15"}, Weekend: []string{"10:00-11:30"}, PerDay: 1}
	sat := w.Slots("x", time.Date(2026, 10, 24, 0, 0, 0, 0, loc))
	if len(sat) != 1 || sat[0].End.Format("15:04") != "11:30" {
		t.Fatalf("週六用週末時段：%v", sat)
	}
	mon := w.Slots("x", time.Date(2026, 10, 26, 0, 0, 0, 0, loc))
	if len(mon) != 1 || mon[0].End.Format("15:04") != "13:15" {
		t.Fatalf("週一用平日時段：%v", mon)
	}
	only := &Window{Weekday: []string{"12:15-13:15"}, PerDay: 1}
	if s := only.Slots("x", time.Date(2026, 10, 24, 0, 0, 0, 0, loc)); len(s) != 1 || s[0].End.Format("15:04") != "13:15" {
		t.Fatalf("週末沒設就沿用平日：%v", s)
	}
}

// 時段寫反、格式錯都要被 ValidWindows 擋下。
func TestValidWindows(t *testing.T) {
	if err := ValidWindows([]string{"12:15-13:15", "09:00-09:30"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"13:15-12:15", "12:15", "aa-bb", "12:15-12:15", ""} {
		if err := ValidWindows([]string{bad}); platform.CodeOf(err) != sdk.ErrorCodeInvalid {
			t.Errorf("%q 應該回 Invalid：%v", bad, err)
		}
	}
}

// Next 一定落在 after 之後，而且找不到時段（空清單）時回 false。
func TestWindowNextIsAfter(t *testing.T) {
	w := &Window{Weekday: []string{"12:15-13:15"}, PerDay: 1}
	after := at(23, 12, 0)
	s, ok := w.Next("x", after, loc)
	if !ok || !s.Start.After(after) {
		t.Fatalf("Next：%v %v", s, ok)
	}
	if _, ok := (&Window{}).Next("x", after, loc); ok {
		t.Fatal("沒有時段不該有下一次")
	}
}

func cronEntry(t *testing.T, spec string, now time.Time) *Entry {
	t.Helper()
	c, err := ParseCron([]string{spec}, loc)
	if err != nil {
		t.Fatal(err)
	}
	return NewCron("x", c, now)
}

// 還沒到期不改任何狀態；到期的那一分鐘觸發一次、推進到下一次，同一分鐘再問不會重複觸發。
func TestDueFiresOncePerMinute(t *testing.T) {
	e := cronEntry(t, "* * * * *", at(23, 12, 0))
	if _, ok := e.Due(at(23, 12, 0), at(23, 12, 0), false, loc); ok {
		t.Fatal("還沒到期")
	}
	now := at(23, 12, 1)
	d, ok := e.Due(now, at(23, 12, 0), false, loc)
	if !ok || !d.Fire || d.Missed || !d.At.Equal(now) {
		t.Fatalf("到期要觸發：%+v %v", d, ok)
	}
	e.MarkFired(d.At)
	if !e.Next().Equal(at(23, 12, 2)) {
		t.Fatalf("要推進到下一分鐘：%v", e.Next())
	}
	if _, ok := e.Due(now.Add(30*time.Second), now, false, loc); ok {
		t.Fatal("同一分鐘不該再到期")
	}
}

// 已經觸發過的那一分鐘（CarryLast 帶過來的）不重複觸發，但仍然推進到下一次。
func TestDueSkipsMinuteAlreadyFired(t *testing.T) {
	prev := cronEntry(t, "0 9 * * *", at(23, 8, 0))
	prev.MarkFired(at(23, 9, 0))
	e := cronEntry(t, "0 9 * * *", at(23, 8, 0))
	e.CarryLast(prev)
	d, ok := e.Due(at(23, 9, 0), at(23, 8, 59), false, loc)
	if !ok || d.Fire {
		t.Fatalf("同一分鐘不重複觸發：%+v %v", d, ok)
	}
	if !e.Next().Equal(at(24, 9, 0)) {
		t.Fatalf("仍要推進：%v", e.Next())
	}
}

// 錯過的時間：catchup=once 且六小時內補最近一次；skip 或超過六小時不補。
func TestDueCatchup(t *testing.T) {
	cases := []struct {
		name         string
		catchup      bool
		sleep        time.Duration
		fire, caught bool
	}{
		{"once 三小時內補跑", true, 3 * time.Hour, true, true},
		{"skip 不補", false, 3 * time.Hour, false, false},
		{"once 超過六小時不補", true, 7 * time.Hour, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := at(23, 12, 0)
			e := cronEntry(t, "*/30 * * * *", start) // 12:30 到期
			now := start.Add(tc.sleep)
			d, ok := e.Due(now, start, tc.catchup, loc)
			if !ok || !d.Missed || d.Fire != tc.fire || d.CaughtUp != tc.caught {
				t.Fatalf("%+v %v", d, ok)
			}
			if tc.fire && (d.At.After(now) || now.Sub(d.At) > 30*time.Minute) {
				t.Fatalf("補跑要是最近一次：%v（現在 %v）", d.At, now)
			}
			if !e.Next().After(now) {
				t.Fatalf("下一次要在現在之後：%v", e.Next())
			}
		})
	}
}

// 單次項目到期就觸發，並標示要移除；補跑與重複判斷不適用。
func TestDueOnce(t *testing.T) {
	e := NewAt("r", at(23, 12, 1))
	if _, ok := e.Due(at(23, 12, 0), at(23, 12, 0), false, loc); ok {
		t.Fatal("還沒到期")
	}
	d, ok := e.Due(at(23, 13, 0), at(23, 12, 0), false, loc)
	if !ok || !d.Fire || !d.Once || d.Missed || !d.At.Equal(at(23, 12, 1)) {
		t.Fatalf("%+v %v", d, ok)
	}
}

// 時段型：時段內到期就觸發並排到下一個；時段過了才醒就略過、記錯過，不補。
func TestDueWindow(t *testing.T) {
	w := &Window{Weekday: []string{"12:15-13:15"}, Weekend: []string{"12:15-13:15"}, PerDay: 1, IdleMin: 7}
	start := at(23, 12, 0)
	e := NewWindow("life/chat", w, start, loc)
	first := e.Next()
	if !first.After(start) || e.Window() != w {
		t.Fatalf("NewWindow：%v", first)
	}
	d, ok := e.Due(first, start, false, loc)
	if !ok || !d.Fire || d.Missed || !d.WindowEnd.After(first) {
		t.Fatalf("時段內要觸發：%+v %v", d, ok)
	}
	if !e.Next().After(first) {
		t.Fatalf("要排到下一個時段：%v", e.Next())
	}

	late := NewWindow("life/chat", w, start, loc)
	d, ok = late.Due(at(23, 14, 0), start, false, loc)
	if !ok || d.Fire || !d.Missed || d.CaughtUp {
		t.Fatalf("時段過了不補：%+v %v", d, ok)
	}
	if late.Next().Day() != 24 {
		t.Fatalf("要排到隔天：%v", late.Next())
	}
}
