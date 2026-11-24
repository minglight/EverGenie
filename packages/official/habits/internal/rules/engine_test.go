package rules

import (
	"reflect"
	"strings"
	"testing"
)

// 2026-10-05 是週一。
var mon = mustDay("2026-10-05")

func mustDay(s string) Day {
	d, ok := ParseDay(s)
	if !ok {
		panic(s)
	}
	return d
}

func days(start Day, offsets ...int) []Day {
	out := make([]Day, len(offsets))
	for i, o := range offsets {
		out[i] = start.Add(o)
	}
	return out
}

func rng(a, b int) []int {
	var out []int
	for i := a; i < b; i++ {
		out = append(out, i)
	}
	return out
}

func weekly(target int, mode string) Habit {
	return Habit{ID: "H-1", Name: "重訓", Agent: "fitness", Cadence: Weekly, Target: target, Mode: mode, Grace: 1, MissDays: 1, Start: mon, Status: Active}
}

func daily() Habit {
	return Habit{ID: "H-1", Name: "拉筋", Agent: "fitness", Cadence: Daily, Target: 7, Mode: Cumulative, Grace: 1, MissDays: 1, Start: mon, Status: Active}
}

func results(ws []Week) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Result)
	}
	return out
}

// hitWeeks：前 n 週每週打一次卡。
func hitWeeks(n int) []Day {
	var out []Day
	for i := 0; i < n; i++ {
		out = append(out, mon.Add(i*7))
	}
	return out
}

func at(weeks int) Day { return mon.Add(7 * weeks) }

func earn(h Habit, dates []Day, today Day) Earned {
	return Earn(h, WeekResults(h, dates, today))
}

// TestWeeklyTargetAccumulates：每週型打卡數 ≥ target 就達標（可以週中就達標）；第一次累積 2 週 +1 點，之後每 4 週 +1 點。
// Guards: REQ-124
func TestWeeklyTargetAccumulates(t *testing.T) {
	h := weekly(2, Cumulative)
	if got := results(WeekResults(h, nil, at(1))); got[0] != Miss {
		t.Fatalf("整週沒做：%v", got)
	}
	if got := results(WeekResults(h, days(mon, 0, 2), mon.Add(2))); got[0] != Hit {
		t.Fatalf("週中就達標：%v", got)
	}
	// 第二週還沒過完是 open，過完沒達標是 miss。
	if got := results(WeekResults(h, days(mon, 7), mon.Add(9))); got[1] != Open {
		t.Fatalf("open：%v", got)
	}
	if got := results(WeekResults(h, days(mon, 7), at(2))); got[1] != Miss {
		t.Fatalf("miss：%v", got)
	}
	// 約定日之前的打卡不算。
	h2 := weekly(1, Cumulative)
	h2.Start = mon.Add(3)
	if ws := WeekResults(h2, days(mon, 0, 1, 3), mon.Add(6)); ws[0].Count != 1 {
		t.Fatalf("約定日前的打卡不算：%+v", ws)
	}
	// 點數：2 週 1 點，之後每 4 週 1 點。
	one := weekly(1, Cumulative)
	for _, c := range []struct{ n, pts, cycle, need int }{{0, 0, 0, 2}, {1, 0, 1, 2}, {2, 1, 0, 4}, {5, 1, 3, 4}, {6, 2, 0, 4}, {10, 3, 0, 4}} {
		e := earn(one, hitWeeks(c.n), at(c.n))
		if e.Points != c.pts || e.Cycle != c.cycle || e.NeedWeeks != c.need {
			t.Errorf("%d 週：%+v", c.n, e)
		}
	}
	if e := earn(one, hitWeeks(5), at(5)); e.Left() != 1 {
		t.Fatalf("離下一點還差：%+v", e)
	}
	// 連續週數：達標、達標、漏、達標 → 漏一週就歸零（grace 不影響連續），連續 1 週、累計 3 週（DEV-142）。
	e := earn(weekly(2, Cumulative), days(mon, 0, 1, 7, 8, 21, 22), mon.Add(28))
	if e.Streak != 1 || e.Hits != 3 {
		t.Fatalf("連續週數：%+v", e)
	}
}

// TestDailyMissDays：每天型沒有連續超過 miss_days 天沒做就達標；今天還沒做不算漏；週日當天做了算過完；漏的連續天數可以跨週。
// Guards: REQ-124
func TestDailyMissDays(t *testing.T) {
	d := daily()
	if got := results(WeekResults(d, days(mon, 0, 1, 3, 4, 5, 6), at(1))); got[0] != Hit {
		t.Fatalf("漏一天可以：%v", got)
	}
	if got := results(WeekResults(d, days(mon, 0, 1, 4, 5, 6), at(1))); got[0] != Miss {
		t.Fatalf("連漏兩天：%v", got)
	}
	// 週四：週三沒做、今天還沒做，還在進行。
	if got := results(WeekResults(d, days(mon, 0, 1), mon.Add(3))); got[0] != Open {
		t.Fatalf("今天沒做不算漏：%v", got)
	}
	if got := results(WeekResults(d, days(mon, rng(0, 7)...), mon.Add(6))); got[0] != Hit {
		t.Fatalf("週日當天做了就過完：%v", got)
	}
	// 週日沒做＋下週一沒做＝連續兩天。
	got := results(WeekResults(d, days(mon, 0, 1, 2, 3, 4, 5, 9, 10, 11, 12, 13), at(2)))
	if got[0] != Hit || got[1] != Miss {
		t.Fatalf("漏的天數跨週：%v", got)
	}
	// miss_days = 0：漏一天就斷。
	d0 := daily()
	d0.MissDays = 0
	if got := results(WeekResults(d0, days(mon, 0, 1, 3, 4, 5, 6), at(1))); got[0] != Miss {
		t.Fatalf("miss_days=0：%v", got)
	}
}

// TestMidWeekStartNotPenalized：週中才約定的那週沒達標不算漏；每天型那週也不給分，下個週一才開始算。
// Guards: REQ-124
func TestMidWeekStartNotPenalized(t *testing.T) {
	h := weekly(1, Cumulative)
	h.Start = mon.Add(4) // 週五才約
	if got := results(WeekResults(h, nil, mon.Add(8))); got[0] != Skip {
		t.Fatalf("週中約定沒做：%v", got)
	}
	if got := results(WeekResults(h, days(mon, 4), mon.Add(8))); got[0] != Hit {
		t.Fatalf("每週型達標照給：%v", got)
	}
	d := daily()
	d.Start = mon.Add(6) // 週日才約、當天做了
	if got := results(WeekResults(d, days(mon, 6), mon.Add(6))); got[0] != Skip {
		t.Fatalf("每天型只有一天不算完整一週：%v", got)
	}
	if got := results(WeekResults(d, days(mon, 6, 7, 8), mon.Add(9))); !reflect.DeepEqual(got, []string{Skip, Open}) {
		t.Fatalf("下個週一才開始算：%v", got)
	}
	// 週一約定的是完整一週。
	if got := results(WeekResults(weekly(1, Cumulative), nil, at(1))); got[0] != Miss {
		t.Fatalf("週一約定：%v", got)
	}
	// 已結束的習慣只算到結束那天。
	end := weekly(1, Cumulative)
	end.Status, end.Ended, end.HasEnded = Ended, mon.Add(8), true
	if ws := WeekResults(end, nil, at(10)); len(ws) != 2 {
		t.Fatalf("結束後不再往下算：%v", results(ws))
	}
}

// TestStreakResetKeepsPoints：連續模式連漏超過寬容週數，這一輪累積的週數歸零，賺到的點還在；累計模式漏了不歸零。
// Guards: REQ-124
func TestStreakResetKeepsPoints(t *testing.T) {
	dates := hitWeeks(3) // 兩週 1 點，第三週累積 1，之後兩週沒做
	cum := earn(weekly(1, Cumulative), dates, at(5))
	if cum.Points != 1 || cum.Cycle != 1 {
		t.Fatalf("累計：%+v", cum)
	}
	con := earn(weekly(1, Streak), dates, at(5))
	if con.Points != 1 || con.Cycle != 0 {
		t.Fatalf("連續：%+v", con)
	}
	if con := earn(weekly(1, Streak), dates, at(4)); con.Cycle != 1 {
		t.Fatalf("只漏一週還在寬容裡：%+v", con)
	}
}

// withBonus 測試用：連續獎勵每 n 週一次。
func withBonus(h Habit, n int) Habit {
	h.BonusWeeks = n
	return h
}

// TestStreakIsConsecutive：連續週數漏一週就歸零（寬容週數不影響）；skip、open 不加也不斷。
func TestStreakIsConsecutive(t *testing.T) {
	h := weekly(1, Cumulative)
	h.Grace = 6
	// 達標、漏、達標、漏、還在進行 → 0。
	if e := earn(h, days(mon, 0, 14), mon.Add(29)); e.Streak != 0 || e.Hits != 2 {
		t.Fatalf("hit,miss,hit,miss,open：%+v", e)
	}
	// 達標、漏、達標 → 1。
	if e := earn(h, days(mon, 0, 14), at(3)); e.Streak != 1 {
		t.Fatalf("hit,miss,hit：%+v", e)
	}
	// open 不斷：兩週達標、這週還沒做 → 2。
	if e := earn(h, hitWeeks(2), mon.Add(15)); e.Streak != 2 {
		t.Fatalf("open 不斷：%+v", e)
	}
	// skip 不加也不斷：直接看週結果。
	ws := []Week{{Result: Hit}, {Result: Skip}, {Result: Hit}, {Result: Open}}
	if e := Earn(withBonus(h, 4), ws); e.Streak != 2 || e.Bonus != 0 {
		t.Fatalf("skip 不斷：%+v", e)
	}
	// 週中才約、那週沒做（skip），之後三週達標 → 3。
	mid := weekly(1, Cumulative)
	mid.Start = mon.Add(4)
	if e := earn(mid, days(mon, 7, 14, 21), at(4)); e.Streak != 3 {
		t.Fatalf("週中約定：%+v", e)
	}
}

// TestStreakBonus：每連續達標 bonus_weeks 週 +1 點（第 4、8 週）；斷了不收回、跨段累加；0＝不給；總點數＝累積＋獎勵。
func TestStreakBonus(t *testing.T) {
	h := withBonus(weekly(1, Cumulative), 4)
	for _, c := range []struct{ n, base, bonus, left int }{{3, 1, 0, 1}, {4, 1, 1, 4}, {5, 1, 1, 3}, {8, 2, 2, 4}} {
		e := earn(h, hitWeeks(c.n), at(c.n))
		if e.Base != c.base || e.Bonus != c.bonus || e.Points != c.base+c.bonus || e.BonusLeft() != c.left {
			t.Errorf("%d 週：%+v left=%d", c.n, e, e.BonusLeft())
		}
	}
	if e := earn(h, hitWeeks(4), at(4)); !e.JustBonused() {
		t.Fatalf("第 4 週剛拿到獎勵：%+v", e)
	}
	if e := earn(h, hitWeeks(5), at(5)); e.JustBonused() {
		t.Fatalf("第 5 週沒有獎勵：%+v", e)
	}
	// 連 4 週、漏 1 週、再連 4 週：獎勵 2 點（斷了不收回、跨段累加）；斷掉那週之後連續從 0 開始。
	run := append(hitWeeks(4), days(mon, 35, 42, 49, 56)...)
	e := earn(h, run, at(9))
	if e.Bonus != 2 || e.Streak != 4 || e.Hits != 8 {
		t.Fatalf("跨段累加：%+v", e)
	}
	if e := earn(h, hitWeeks(4), at(5)); e.Bonus != 1 || e.Streak != 0 || e.BonusLeft() != 4 {
		t.Fatalf("斷了不收回：%+v", e)
	}
	// 連 3 週、漏、連 3 週：每段都沒滿 4 週 → 沒有獎勵。
	if e := earn(h, append(hitWeeks(3), days(mon, 28, 35, 42)...), at(7)); e.Bonus != 0 {
		t.Fatalf("沒連滿不給：%+v", e)
	}
	// bonus_weeks = 0：不給、離下一次是 0。
	if e := earn(withBonus(weekly(1, Cumulative), 0), hitWeeks(8), at(8)); e.Bonus != 0 || e.Points != 2 || e.BonusLeft() != 0 || e.JustBonused() {
		t.Fatalf("bonus_weeks=0：%+v", e)
	}
	// 實際資料 H-1：每週 2 次、5 週都達標 → 連續 5、累積 1 點、獎勵 1 點、共 2 點、離下一次獎勵 3 週。
	var dates []Day
	for i := 0; i < 5; i++ {
		dates = append(dates, mon.Add(i*7), mon.Add(i*7+1))
	}
	if e := earn(withBonus(weekly(2, Cumulative), 4), dates, at(5)); e.Streak != 5 || e.Base != 1 || e.Bonus != 1 || e.Points != 2 || e.BonusLeft() != 3 {
		t.Fatalf("H-1：%+v", e)
	}
	// 錢包用總點數。
	if w := Bank([]State{stateOf(h, hitWeeks(4), at(4))}, nil, 0, TaskBook{HasLegacy: true}); w.Banked["H-1"] != 2 {
		t.Fatalf("錢包：%+v", w)
	}
}

// TestBonusWeeksField：bonus_weeks 沒寫＝4、可以寫 0、範圍 0–12。
func TestBonusWeeksField(t *testing.T) {
	f := func(extra map[string]any) map[string]any {
		m := map[string]any{"name": "重訓", "agent": "fitness", "cadence": "weekly", "target": 1.0, "mode": "cumulative", "start": "2026-10-05", "status": "active"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	if h, err := HabitFromFields("H-1", f(nil), mon); err != nil || h.BonusWeeks != DefaultBonusWeeks || DefaultBonusWeeks != 4 {
		t.Fatalf("預設：%+v %v", h, err)
	}
	for _, c := range []struct {
		v  any
		ok bool
		n  int
	}{{0.0, true, 0}, {12.0, true, 12}, {13.0, false, 0}, {-1.0, false, 0}, {"", true, 4}, {1.5, false, 0}, {1.0, false, 0}, {2.0, true, 2}} {
		h, err := HabitFromFields("H-1", f(map[string]any{"bonus_weeks": c.v}), mon)
		if (err == nil) != c.ok || (c.ok && h.BonusWeeks != c.n) {
			t.Errorf("bonus_weeks=%v：%+v %v", c.v, h.BonusWeeks, err)
		}
	}
}

// TestPointsOnlyGrow：點數帳每個習慣取最高值；規則改嚴、習慣被刪都不縮水；錢包＝賺到的全部 − 花掉的，不會是負的。
// Guards: REQ-124
func TestPointsOnlyGrow(t *testing.T) {
	st := stateOf
	// 賺到 2 點、花掉 1 點。
	w := Bank([]State{st(weekly(1, Cumulative), hitWeeks(6), at(6))}, nil, 1, TaskBook{Read: 0, Old: 0, HasLegacy: true})
	if w.Earned != 2 || w.Points != 1 {
		t.Fatalf("錢包：%+v", w)
	}
	// 規則改嚴了、算出來 0 點：帳上的 2 點不縮水；已刪習慣 H-9 的 1 點還在。
	w = Bank([]State{st(weekly(7, Cumulative), hitWeeks(6), at(6))}, map[string]int{"H-1": 2, "H-9": 1}, 0, TaskBook{Read: 0, Old: 0, HasLegacy: true})
	if w.Banked["H-1"] != 2 || w.Earned != 3 || w.Points != 3 {
		t.Fatalf("改嚴之後：%+v", w)
	}
	// 習慣全刪光：帳上 3 點，花掉 1 點照扣；花得比賺的多也不會是負的。
	if w := Bank(nil, map[string]int{"H-1": 2, "H-9": 1}, 1, TaskBook{Read: 0, Old: 0, HasLegacy: true}); w.Points != 2 {
		t.Fatalf("全刪光：%+v", w)
	}
	if w := Bank(nil, map[string]int{"H-1": 1}, 5, TaskBook{Read: 0, Old: 0, HasLegacy: true}); w.Points != 0 {
		t.Fatalf("不會是負的：%+v", w)
	}
	// 多個習慣湊成同一個錢包，跟誰約的無關。
	a := st(weekly(1, Cumulative), hitWeeks(2), at(6))
	b := st(weekly(1, Cumulative), hitWeeks(6), at(6))
	b.Habit.ID = "H-2"
	b.Habit.Agent = "life"
	if w := Bank([]State{a, b}, nil, 0, TaskBook{Read: 0, Old: 0, HasLegacy: true}); w.Points != 3 {
		t.Fatalf("同一個錢包：%+v", w)
	}
}

// stateOf 測試用：用給定的日期（而不是打卡紀錄）算狀態。
func stateOf(h Habit, dates []Day, today Day) State {
	weeks := WeekResults(h, dates, today)
	return State{Habit: h, Weeks: weeks, Earned: Earn(h, weeks)}
}

func subHabit(allow int, subs ...string) Habit {
	h := daily()
	h.Start = mustDay("2026-01-01")
	h.AllowMiss = allow
	for _, s := range subs {
		sub, err := ParseSub(s)
		if err != nil {
			panic(err)
		}
		h.Subtasks = append(h.Subtasks, sub)
	}
	return h
}

func subRow(seq int, date Day, sub string) Checkin {
	return Checkin{Seq: seq, Habit: "H-1", Date: date, Sub: sub, Source: "manual", Key: "manual:H-1:" + date.String() + ":" + sub}
}

// TestSubtasksAllowMiss：子項目逐項打卡，做到的項數 ≥ 全部 − allow_miss 才算當天打卡；自動來源（體重、飲食）即時推算；格式與檢查。
// Guards: REQ-124
func TestSubtasksAllowMiss(t *testing.T) {
	today := mustDay("2026-10-09")
	y := today.Add(-1)
	// 預設全部都要做。
	h := subHabit(0, "量體重=body:weight", "記錄吃飯=nutrition:meals>=2")
	in := Inputs{WeightDays: map[Day]bool{y: true}, MealCounts: map[Day]int{y: 1}}
	if got := Dates(h, nil, in); len(got) != 0 {
		t.Fatalf("只有一項不算：%v", got)
	}
	in.MealCounts[y] = 2
	if got := Dates(h, nil, in); !reflect.DeepEqual(got, []Day{y}) {
		t.Fatalf("兩項都有算一天：%v", got)
	}
	// allow_miss = 1：五項做到四項就算。
	five := subHabit(1, "s0", "s1", "s2", "s3", "s4")
	var rows []Checkin
	for i := 0; i < 3; i++ {
		rows = append(rows, subRow(i+1, y, five.Subtasks[i].Name))
	}
	if got := Dates(five, ActiveCheckins(rows), Inputs{}); len(got) != 0 {
		t.Fatalf("3/5 不夠：%v", got)
	}
	rows = append(rows, subRow(4, y, "s3"))
	if got := Dates(five, ActiveCheckins(rows), Inputs{}); !reflect.DeepEqual(got, []Day{y}) {
		t.Fatalf("4/5 最多漏一項，算：%v", got)
	}
	// 手動補一項、自動一項：今天各項的狀態，自動與手動都有時算手動。
	mixed := subHabit(0, "量體重=body:weight", "記錄吃飯=nutrition:meals>=2")
	act := ActiveCheckins([]Checkin{subRow(1, today, "記錄吃飯"), subRow(2, today, "量體重")})
	st := Compute(mixed, act, Inputs{WeightDays: map[Day]bool{today: true}}, today)
	if len(st.TodaySubs) != 2 || st.TodaySubs[0].How != "manual" || st.TodaySubs[1].How != "manual" {
		t.Fatalf("今天的子項目：%+v", st.TodaySubs)
	}
	st = Compute(mixed, nil, Inputs{WeightDays: map[Day]bool{today: true}}, today)
	if !st.TodaySubs[0].Done || st.TodaySubs[0].How != "auto" || st.TodaySubs[1].Done {
		t.Fatalf("自動來源：%+v", st.TodaySubs)
	}
	// 不是子項目的手動打卡不算。
	if got := Dates(five, ActiveCheckins([]Checkin{subRow(1, y, "不存在")}), Inputs{}); len(got) != 0 {
		t.Fatalf("不存在的子項目：%v", got)
	}
	// 格式。
	if s, err := ParseSub("吃飯=nutrition:meals>=3"); err != nil || s.Min != 3 || s.Source != SubNutritionMeal {
		t.Fatalf("ParseSub：%+v %v", s, err)
	}
	if s, _ := ParseSub("拉筋"); s.Name != "拉筋" || s.Source != "" || s.Min != 1 {
		t.Fatalf("ParseSub 純名稱：%+v", s)
	}
	for _, bad := range [][]string{{"a", "a"}, {"a=gmail:x"}, {"a=nutrition:meals>=0"}, {"=body:weight"}} {
		var subs []Sub
		for _, s := range bad {
			sub, _ := ParseSub(s)
			subs = append(subs, sub)
		}
		if err := CheckSubs(subs, 0); err == nil {
			t.Errorf("%v 要被擋", bad)
		}
	}
	if err := CheckSubs([]Sub{{Name: "a", Min: 1}, {Name: "b", Min: 1}}, 2); err == nil || !strings.Contains(err.Error(), "allow_miss") {
		t.Errorf("allow_miss 太大要被擋：%v", err)
	}
	if err := CheckSubs(nil, 1); err == nil {
		t.Error("沒有子項目 allow_miss 不能大於 0")
	}
}

// TestAutoSourceCheckin：自動來源即時推算、不寫打卡：fitness（種類與萬用字元、只算完成的）、reminder（完成那天）、體重、飲食。
// Guards: REQ-124
func TestAutoSourceCheckin(t *testing.T) {
	d1, d2, d3 := mon, mon.Add(1), mon.Add(2)
	in := Inputs{
		Workouts: []Workout{
			{Type: "重訓", Date: d1, Done: true},
			{Type: "跑步", Date: d2, Done: true},
			{Type: "重訓", Date: d3, Done: false}, // 進行中不算
			{Type: "重訓", Date: d1, Done: true},  // 同一天第二次，各算一次
			{Type: "游泳", Date: d3, Done: true},
		},
		Reminders:  map[string]Day{"R-7": d3},
		WeightDays: map[Day]bool{d1: true, d2: true},
		MealCounts: map[Day]int{d1: 3, d2: 1},
	}
	h := weekly(1, Cumulative)
	h.Sources = []string{"fitness:重訓"}
	if got := Dates(h, nil, in); !reflect.DeepEqual(got, []Day{d1, d1}) {
		t.Fatalf("fitness:重訓：%v", got)
	}
	h.Sources = []string{"fitness:*"}
	if got := Dates(h, nil, in); len(got) != 4 {
		t.Fatalf("fitness:*：%v", got)
	}
	// fitness:有氧＝跑步、划船、游泳、騎車都算，重訓不算。
	h.Sources = []string{"fitness:有氧"}
	if got := Dates(h, nil, in); !reflect.DeepEqual(got, []Day{d2, d3}) {
		t.Fatalf("fitness:有氧：%v", got)
	}
	h.Sources = []string{"fitness:跑步"}
	if got := Dates(h, nil, in); !reflect.DeepEqual(got, []Day{d2}) {
		t.Fatalf("fitness:跑步：%v", got)
	}
	h.Sources = []string{"reminder:R-7"}
	if got := Dates(h, nil, in); !reflect.DeepEqual(got, []Day{d3}) {
		t.Fatalf("reminder：%v", got)
	}
	h.Sources = []string{"reminder:R-8"}
	if got := Dates(h, nil, in); len(got) != 0 {
		t.Fatalf("沒完成的待辦不算：%v", got)
	}
	// 體重、飲食當子項目的來源。
	sub := subHabit(0, "量體重=body:weight", "吃飯=nutrition:meals>=2")
	if got := Dates(sub, nil, in); !reflect.DeepEqual(got, []Day{d1}) {
		t.Fatalf("體重＋飲食：%v", got)
	}
	// 自動來源跟手動打卡加在一起；習慣的來源檢查。
	h.Sources = []string{"fitness:跑步"}
	got := Dates(h, ActiveCheckins([]Checkin{{Seq: 1, Habit: "H-1", Date: d3, Source: "manual", Key: "k"}}), in)
	if !reflect.DeepEqual(got, []Day{d3, d2}) {
		t.Fatalf("手動加自動：%v", got)
	}
	for _, bad := range []string{"gmail:1", "fitness:", "reminder"} {
		if CheckSource(bad) == nil {
			t.Errorf("來源 %q 要被擋", bad)
		}
	}
	for _, ok := range []string{"fitness:重訓", "fitness:有氧", "fitness:*", "reminder:R-3"} {
		if err := CheckSource(ok); err != nil {
			t.Errorf("來源 %q：%v", ok, err)
		}
	}
}

// TestUndoCheckin：撤銷只對還有效的最後一筆；同一個 key 只算一次；撤銷之後同一天可以再打。
// Guards: REQ-124
func TestUndoCheckin(t *testing.T) {
	d := mon
	ck := func(seq int, key string, undo bool) Checkin {
		return Checkin{Seq: seq, Habit: "H-1", Date: d, Source: "manual", Key: key, Undo: undo}
	}
	if got := ActiveCheckins([]Checkin{ck(1, "k", false), ck(2, "k", false)}); len(got) != 1 {
		t.Fatalf("同一個 key 只算一次：%v", got)
	}
	if got := ActiveCheckins([]Checkin{ck(1, "k", false), ck(2, "", true)}); len(got) != 0 {
		t.Fatalf("撤銷：%v", got)
	}
	if got := ActiveCheckins([]Checkin{ck(1, "k", false), ck(2, "", true), ck(3, "k", false)}); len(got) != 1 || got[0].Seq != 3 {
		t.Fatalf("撤銷後再打：%v", got)
	}
	if got := ActiveCheckins([]Checkin{ck(1, "", true)}); len(got) != 0 {
		t.Fatalf("沒有可撤的：%v", got)
	}
	// 亂序進來也照編號處理；撤銷只影響同習慣同一天同子項目。
	other := Checkin{Seq: 2, Habit: "H-1", Date: d.Add(1), Source: "manual", Key: "k2"}
	got := ActiveCheckins([]Checkin{ck(3, "", true), ck(1, "k", false), other})
	if len(got) != 1 || got[0].Seq != 2 {
		t.Fatalf("亂序與其他天：%v", got)
	}
	sub := Checkin{Seq: 1, Habit: "H-1", Date: d, Sub: "a", Key: "ka"}
	undoOther := Checkin{Seq: 2, Habit: "H-1", Date: d, Sub: "b", Undo: true}
	if got := ActiveCheckins([]Checkin{sub, undoOther}); len(got) != 1 {
		t.Fatalf("撤銷別的子項目不影響：%v", got)
	}
	// 打卡真的影響本週進度（今天的手動筆數）。
	h := weekly(2, Cumulative)
	st := Compute(h, ActiveCheckins([]Checkin{ck(1, "k", false)}), Inputs{}, d)
	if st.ThisWeek.Done != 1 || st.ThisWeek.Need != 2 || st.TodayManual != 1 {
		t.Fatalf("本週進度：%+v", st)
	}
	st = Compute(h, ActiveCheckins([]Checkin{ck(1, "k", false), ck(2, "", true)}), Inputs{}, d)
	if st.ThisWeek.Done != 0 || st.TodayManual != 0 {
		t.Fatalf("撤銷之後：%+v", st)
	}
}

// TestHabitFromFields：vault 欄位轉成習慣：預設值、檢查、結束日。
// Guards: REQ-124
func TestHabitFromFields(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"name": "重訓", "agent": "fitness", "cadence": "weekly", "target": 3.0, "mode": "cumulative", "start": "2026-10-05", "status": "active"}
	}
	h, err := HabitFromFields("H-1", base(), 0)
	if err != nil || h.MissDays != 1 || h.Grace != 1 || h.Target != 3 || h.AllowMiss != 0 {
		t.Fatalf("預設值：%+v %v", h, err)
	}
	f := base()
	delete(f, "target")
	if _, err := HabitFromFields("H-1", f, 0); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("weekly 要有 target：%v", err)
	}
	f = base()
	f["cadence"] = "daily"
	delete(f, "target")
	if h, err := HabitFromFields("H-1", f, 0); err != nil || h.Cadence != Daily {
		t.Fatalf("daily 不需要 target：%v", err)
	}
	f = base()
	f["start"] = "不是日期"
	if _, err := HabitFromFields("H-1", f, 0); err == nil {
		t.Fatal("start 壞了要擋")
	}
	f = base()
	f["status"] = "ended"
	f["ended"] = "2026-10-20"
	if h, _ := HabitFromFields("H-1", f, 0); !h.HasEnded || h.Ended != mustDay("2026-10-20") {
		t.Fatalf("結束日：%+v", h)
	}
	delete(f, "ended")
	if h, _ := HabitFromFields("H-1", f, mustDay("2026-10-22")); h.Ended != mustDay("2026-10-22") {
		t.Fatalf("沒寫結束日用修改日：%+v", h)
	}
	f = base()
	f["sources"] = []any{"gmail:1"}
	if _, err := HabitFromFields("H-1", f, 0); err == nil {
		t.Fatal("來源壞了要擋")
	}
	f = base()
	f["target"] = 3.5
	if _, err := HabitFromFields("H-1", f, 0); err == nil {
		t.Fatal("target 不是整數要擋")
	}
}

// TestWeekSkips（HAB-14）：2026-11-09 之後，每天型一週跳 2 天達標、跳 3 天沒達標；之前的週不受影響。
func TestWeekSkips(t *testing.T) {
	from, _ := ParseDay("2026-11-09")
	d := daily()
	d.Start, d.WeekSkips = from, DefaultWeekSkips
	if got := results(WeekResults(d, days(from, 0, 2, 4, 5, 6), from.Add(7))); got[0] != Hit {
		t.Fatalf("跳 2 天（不連續）要達標：%v", got)
	}
	if got := results(WeekResults(d, days(from, 0, 2, 4, 6), from.Add(7))); got[0] != Miss {
		t.Fatalf("跳 3 天要沒達標：%v", got)
	}
	old := from.Add(-7)
	o := daily()
	o.Start, o.WeekSkips = old, DefaultWeekSkips
	if got := results(WeekResults(o, days(old, 0, 2, 4, 6), from)); got[0] != Hit {
		t.Fatalf("上線前的週不受影響：%v", got)
	}
}

// TestDayMarks（HAB-14）：月曆的做了、跳過、斷了；今天沒做不算。
func TestDayMarks(t *testing.T) {
	from, _ := ParseDay("2026-11-09")
	d := daily()
	d.Start, d.WeekSkips = from, DefaultWeekSkips
	done, skipped, broken := DayMarks(d, days(from, 0, 2), from.Add(5))
	if len(done) != 2 || len(skipped) != 2 || len(broken) != 1 || skipped[0] != from.Add(1) || broken[0] != from.Add(4) {
		t.Fatalf("done=%v skipped=%v broken=%v", done, skipped, broken)
	}
}

// TestTaskPointsToWallet（HAB-13、HAB-18）：待辦點數一直往上加；改價前按的兌換用 20 點換 1 個錢包點，不夠不換；舊版錢包自動換過的照留。
func TestTaskPointsToWallet(t *testing.T) {
	if w := Bank(nil, nil, 0, TaskBook{Read: 39, HasLegacy: true}); w.Earned != 0 || w.Tasks != 39 || w.TaskExchanged != 0 {
		t.Fatalf("沒按兌換不換：%+v", w)
	}
	if w := Bank(nil, nil, 0, TaskBook{Read: 45, HasLegacy: true, Requested: 3}); w.Earned != 2 || w.TaskExchanged != 40 {
		t.Fatalf("按三次只夠換兩次：%+v", w)
	}
	if w := Bank(nil, nil, 0, TaskBook{Read: 0, Old: 40, OldExchanged: 20, HasLegacy: true}); w.Earned != 1 || w.Tasks != 40 || w.TaskExchanged != 20 {
		t.Fatalf("待辦被刪、兌換紀錄被刪也不收回：%+v", w)
	}
	if w := Bank(nil, map[string]int{"H-1": 3}, 1, TaskBook{Read: 20, HasLegacy: true, Requested: 1}); w.Earned != 4 || w.Points != 3 {
		t.Fatalf("%+v", w)
	}
	w := Bank(nil, nil, 0, TaskBook{Read: 47, Old: 45})
	if w.TaskLegacy != 40 || w.TaskExchanged != 40 || w.Earned != 2 {
		t.Fatalf("舊版錢包每滿 20 自動換過的照留：%+v", w)
	}
	if w := Bank(nil, nil, 0, TaskBook{Read: 47, Old: 47, Legacy: 40, OldExchanged: 40, HasLegacy: true, Requested: 1}); w.TaskExchanged != 40 {
		t.Fatalf("剩 7 點不夠換：%+v", w)
	}
}

// TestTaskExchangePriceChange（DEV-149）：兌換從 20 點改 30 點；改價前按的照 20，之後的照 30，已經換到的錢包點不變少。
func TestTaskExchangePriceChange(t *testing.T) {
	// 改價前的錢包：兩筆兌換已經照 20 換好，第一次重算記下 2 筆舊價、錢包點 2。
	w := Bank(nil, nil, 0, TaskBook{Read: 45, Old: 45, OldExchanged: 40, HasLegacy: true, Requested: 2})
	if w.TaskOldRequests != 2 || w.TaskExchanged != 40 || w.TaskWallet != 2 || w.Earned != 2 {
		t.Fatalf("改價前換的照 20：%+v", w)
	}
	// 之後又按一次：要 30 點，還沒換的只有 5 點，不換。
	book := TaskBook{Read: 45, Old: 45, OldExchanged: 40, HasLegacy: true, Requested: 3, OldWallet: 2, HasWallet: true, OldRequests: 2, HasOld: true}
	if w := Bank(nil, nil, 0, book); w.TaskExchanged != 40 || w.Earned != 2 {
		t.Fatalf("新的一次要 30 點，5 點不夠：%+v", w)
	}
	// 累積到 70：還沒換的 30 點，夠換一次。
	book.Read = 70
	if w := Bank(nil, nil, 0, book); w.TaskExchanged != 70 || w.TaskWallet != 3 || w.Earned != 3 {
		t.Fatalf("滿 30 換一次：%+v", w)
	}
	// 舊錢包又讀不到兌換紀錄：改價前的筆數這次不定，錢包點不變少。
	if w := Bank(nil, nil, 0, TaskBook{Read: 45, Old: 45, OldExchanged: 40, HasLegacy: true, Unread: true}); !w.TaskOldUnset || w.TaskWallet != 2 {
		t.Fatalf("讀不到不定舊價筆數：%+v", w)
	}
	// 新家：一開始就是 30。
	if w := Bank(nil, nil, 0, TaskBook{Read: 29, HasLegacy: true, HasOld: true, HasWallet: true, Requested: 1}); w.TaskExchanged != 0 || w.Earned != 0 {
		t.Fatalf("29 點不夠：%+v", w)
	}
	if w := Bank(nil, nil, 0, TaskBook{Read: 30, HasLegacy: true, HasOld: true, HasWallet: true, Requested: 1}); w.TaskExchanged != 30 || w.Earned != 1 {
		t.Fatalf("30 點換 1：%+v", w)
	}
}
