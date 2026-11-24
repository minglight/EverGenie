package timer

import (
	"fmt"
	"hash/fnv"
	"math/rand"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Window 是時段型排程（舊系統的 windows，主動聊天用）：每天從時段裡挑 PerDay 段，每段裡挑一分鐘觸發。
type Window struct {
	Weekday, Weekend []string // "HH:MM-HH:MM"；Weekend 空表示週末也用 Weekday
	PerDay           int
	IdleMin          int
}

// Slot 是某一次觸發時間與它所屬時段的結束。
type Slot struct{ Start, End time.Time }

func parseClock(s string, day time.Time) (time.Time, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), 0, 0, day.Location()), nil
}

// parseWindow 把 "HH:MM-HH:MM" 換成那一天的開始與結束；結束要晚於開始。
func parseWindow(w string, day time.Time) (time.Time, time.Time, error) {
	a, b, ok := strings.Cut(w, "-")
	if !ok {
		return time.Time{}, time.Time{}, fmt.Errorf("bad window")
	}
	start, err := parseClock(a, day)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := parseClock(b, day)
	if err != nil || !end.After(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("bad window")
	}
	return start, end, nil
}

// ValidWindows 檢查每一段都寫成 HH:MM-HH:MM 而且結束晚於開始。
func ValidWindows(ws []string) error {
	for _, w := range ws {
		if _, _, err := parseWindow(w, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			return platform.Invalid("windows", "時段要寫成 HH:MM-HH:MM，而且結束晚於開始")
		}
	}
	return nil
}

// Slots 是某一天的觸發時間：同一天、同一位、同一個工作結果固定（用日期與 id 當亂數種子），由早到晚。
func (w *Window) Slots(id string, day time.Time) []Slot {
	wins := w.Weekday
	if wd := day.Weekday(); (wd == time.Saturday || wd == time.Sunday) && len(w.Weekend) > 0 {
		wins = w.Weekend
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(day.Format("2006-01-02") + "/" + id))
	rng := rand.New(rand.NewSource(int64(h.Sum64())))
	n := min(max(w.PerDay, 1), len(wins))
	var out []Slot
	for _, i := range rng.Perm(len(wins))[:n] {
		start, end, err := parseWindow(wins[i], day)
		if err != nil {
			continue
		}
		span := int(end.Sub(start) / time.Minute)
		at := start.Add(time.Duration(rng.Intn(max(span-10, 1))) * time.Minute)
		out = append(out, Slot{at, end})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Start.Before(out[j-1].Start); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// Next 是 after 之後最早的一個觸發時間與它的時段結束（最多往後找 8 天）。
func (w *Window) Next(id string, after time.Time, loc *time.Location) (Slot, bool) {
	d := after.In(loc)
	day := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
	for i := 0; i < 8; i++ {
		for _, s := range w.Slots(id, day.AddDate(0, 0, i)) {
			if s.Start.After(after) {
				return s, true
			}
		}
	}
	return Slot{}, false
}
