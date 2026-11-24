// Package rules 是 habits 套件的規則引擎：全部是純函式（沒有 I/O、不取系統時間），
// 規則的唯一正本是舊系統 habits.py 的檔頭說明，這裡用 Go 重寫。
package rules

import "time"

// Day 是一個日曆日（自 1970-01-01 起的天數，沒有時區）。週一到週日算一週。
type Day int

// ParseDay 收 "YYYY-MM-DD"（後面接時間也行，只看前 10 個字）。
func ParseDay(s string) (Day, bool) {
	if len(s) < 10 {
		return 0, false
	}
	t, err := time.Parse("2006-01-02", s[:10])
	if err != nil {
		return 0, false
	}
	return Day(t.Unix() / 86400), true
}

// DayOf 取這個時間（它自己的時區）的年月日。
func DayOf(t time.Time) Day {
	return Day(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

// String 是 "YYYY-MM-DD"。
func (d Day) String() string {
	return time.Unix(int64(d)*86400, 0).UTC().Format("2006-01-02")
}

// Add 加減天數。
func (d Day) Add(n int) Day { return d + Day(n) }

// Monday 是這一天那一週的週一。1970-01-01 是週四。
func (d Day) Monday() Day {
	off := (int(d)%7 + 7 + 3) % 7 // 週一 = 0
	return d - Day(off)
}
