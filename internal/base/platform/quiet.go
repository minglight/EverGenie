package platform

import (
	"strings"
	"time"
)

type Quiet struct {
	Start, End, DeferTo string
	Loc                 *time.Location
}

func clockMinutes(s string) (int, bool) {
	t, err := time.Parse("15:04", s)
	if err != nil || len(s) != 5 {
		return 0, false
	}
	return t.Hour()*60 + t.Minute(), true
}
func (q Quiet) location() *time.Location {
	if q.Loc != nil {
		return q.Loc
	}
	return time.UTC
}
func (q Quiet) Contains(t time.Time) bool {
	start, a := clockMinutes(q.Start)
	end, b := clockMinutes(q.End)
	if !a || !b || start == end {
		return false
	}
	t = t.In(q.location())
	minute := t.Hour()*60 + t.Minute()
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}
func (q Quiet) Defer(t time.Time) time.Time {
	if !q.Contains(t) {
		return t
	}
	target, ok := clockMinutes(q.DeferTo)
	if !ok {
		return t
	}
	local := t.In(q.location())
	next := time.Date(local.Year(), local.Month(), local.Day(), target/60, target%60, 0, 0, q.location())
	if !next.After(t) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
func validateQuiet(hours, deferTo string) bool {
	parts := strings.Split(hours, "-")
	if len(parts) != 2 {
		return false
	}
	_, a := clockMinutes(parts[0])
	_, b := clockMinutes(parts[1])
	_, c := clockMinutes(deferTo)
	return a && b && c
}
