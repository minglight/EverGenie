package rules

import "sort"

// 週結果。
const (
	Hit  = "hit"  // 達標
	Miss = "miss" // 沒達標
	Open = "open" // 還在進行
	Skip = "skip" // 不算（週中才約定的那週沒達標）
)

// weekSkipsFrom 是「每週跳過上限」開始算的週一（2026-11-09）：之前的週照舊規則，不回頭改已經給的結果。
var weekSkipsFrom, _ = ParseDay("2026-11-09")

const (
	// DefaultWeekSkips 是每天型每週最多跳過幾天（作者 2026-11-07 定）。
	DefaultWeekSkips = 2
	// TaskPointsPerWalletPoint：待辦累積幾點換一個錢包點（HAB-13；DEV-149 作者 2026-11-12 從 20 改 30）。
	TaskPointsPerWalletPoint = 30
	// OldTaskPointsPerWalletPoint：改 30 之前按的兌換、舊版自動換的，一律照 20 算，不回頭改已經換到的。
	OldTaskPointsPerWalletPoint = 20
	daysKept                    = 400 // status 留幾天給月曆

	firstPointWeeks = 2 // 第一次累積 2 週達標 → 1 點
	nextPointWeeks  = 4 // 之後每累積 4 週達標 → 再 1 點

	// DefaultBonusWeeks：沒寫 bonus_weeks 時，連續達標幾週給一次獎勵（DEV-142，作者 2026-11-07 定）。
	DefaultBonusWeeks = 4
)

// Week 是一週的結果。Week 是那週的週一。
type Week struct {
	Week   Day
	Count  int
	Result string
}

// WeekResults 是一個習慣從約定那週到這週，每週的結果。dates 是打卡日期（同一天可以多筆）。
// 規則見舊系統 habits.py 檔頭：每週型 打卡數 ≥ target；每天型 沒有連續超過 miss_days 天沒做（今天還沒過完不算漏）；
// 週中才約定的那週沒達標不算漏，每天型那週也不給分。
func WeekResults(h Habit, dates []Day, today Day) []Week {
	start := h.Start
	end := today
	if h.HasEnded && h.Ended < end {
		end = h.Ended
	}
	if end < start {
		return nil
	}
	n := map[Day]int{}
	for _, d := range dates {
		if d >= start && d <= end {
			n[d]++
		}
	}
	daily := h.Cadence == Daily
	broken := map[Day]bool{}
	if daily {
		broken = dailyMarks(h, n, start, end).brokenWeeks
	}
	var out []Week
	firstWeek := start.Monday()
	for w := firstWeek; w <= end; w += 7 {
		sun := w + 6
		lo := max(w, start)
		hi := min(sun, end)
		finished := sun < end || (sun == end && end < today)
		count := 0
		for d := lo; d <= hi; d++ {
			count += n[d]
		}
		var res string
		if daily { // 週日當天做了也算過完（不用等到週一才給分）
			switch {
			case broken[w]:
				res = Miss
			case finished || (sun == end && n[sun] > 0):
				res = Hit
			default:
				res = Open
			}
		} else {
			switch {
			case count >= h.Target:
				res = Hit
			case finished:
				res = Miss
			default:
				res = Open
			}
		}
		if w == firstWeek && start != w && (res == Miss || (res == Hit && daily)) {
			res = Skip
		}
		out = append(out, Week{Week: w, Count: count, Result: res})
	}
	return out
}

// marks 是每天型每一天沒做的結果：還在容許內（skipped）或算斷（broken）。
type marks struct {
	skipped, broken []Day
	brokenWeeks     map[Day]bool
}

// dailyMarks：連續漏超過 miss_days 天、或同一週漏超過 week_skips 天（只算 weekSkipsFrom 之後的週）就斷。今天還沒過完：沒做不算漏。
func dailyMarks(h Habit, n map[Day]int, start, end Day) marks {
	m := marks{brokenWeeks: map[Day]bool{}}
	run := 0
	week := map[Day]int{}
	for d := start; d < end || (d == end && n[d] > 0); d++ {
		if n[d] > 0 {
			run = 0
			continue
		}
		run++
		mon := d.Monday()
		week[mon]++
		if run > h.MissDays || (mon >= weekSkipsFrom && week[mon] > h.WeekSkips) {
			m.brokenWeeks[mon] = true
			m.broken = append(m.broken, d)
		} else {
			m.skipped = append(m.skipped, d)
		}
	}
	return m
}

// DayMarks 是月曆要畫的每一天：做了、跳過（還在容許內）、斷了；每種最多留最近 daysKept 天。規則只在這裡，頁面只畫。
func DayMarks(h Habit, dates []Day, today Day) (done, skipped, broken []Day) {
	end := today
	if h.HasEnded && h.Ended < end {
		end = h.Ended
	}
	n := map[Day]int{}
	set := map[Day]bool{}
	for _, d := range dates {
		if d >= h.Start && d <= end {
			n[d]++
			set[d] = true
		}
	}
	done = lastDays(sortedDays(set))
	if h.Cadence == Daily && end >= h.Start {
		m := dailyMarks(h, n, h.Start, end)
		skipped, broken = lastDays(m.skipped), lastDays(m.broken)
	}
	return done, skipped, broken
}

func lastDays(days []Day) []Day {
	if len(days) > daysKept {
		return days[len(days)-daysKept:]
	}
	return days
}

// Earned 是每週結果算出來的成績。
type Earned struct {
	Hits       int // 達標週數（累計）
	Points     int // 這個規則算出來的點數＝Base＋Bonus
	Base       int // 累積達標週數換到的點數（第一次 2 週、之後每 4 週）
	Bonus      int // 連續獎勵的點數（每連續達標 BonusWeeks 週 +1，斷了不收回，整段歷史累加）
	BonusWeeks int // 連續幾週給一次獎勵（0＝不給）
	Streak     int // 連續達標週數：漏一週（miss）就歸零；skip、open 不加也不斷
	Cycle      int // 這一輪累積的週數
	NeedWeeks  int // 這一輪總共要累積幾週才有下一點（2 或 4）
}

// Left 是離下一點（累積）還差幾週。
func (e Earned) Left() int { return e.NeedWeeks - e.Cycle }

// BonusLeft 是離下一次連續獎勵還差幾週（不給獎勵時是 0）。
func (e Earned) BonusLeft() int {
	if e.BonusWeeks <= 0 {
		return 0
	}
	return e.BonusWeeks - e.Streak%e.BonusWeeks
}

// JustBonused：最近一個達標週剛好湊滿連續獎勵（通知用來說「連續達標 N 週」）。
func (e Earned) JustBonused() bool {
	return e.BonusWeeks > 0 && e.Streak > 0 && e.Streak%e.BonusWeeks == 0
}

// Earn：第一次累積 2 週達標 +1 點，之後每累積 4 週再 +1 點。累計＝達標週數只增不減；
// 連續模式（mode = streak）：連續超過 grace 週沒達標，這一輪累積的週數歸零（賺到的點不收回）。
// 連續週數另外算：漏一週就歸零；每連續達標 bonus_weeks 週再 +1 點（斷了不收回）。
func Earn(h Habit, weeks []Week) Earned {
	var base, bonus, cycle, hits, streak, misses int
	for _, wk := range weeks {
		switch wk.Result {
		case Hit:
			hits++
			streak++
			misses = 0
			cycle++
			need := nextPointWeeks
			if base == 0 {
				need = firstPointWeeks
			}
			if cycle >= need {
				base++
				cycle = 0
			}
			if h.BonusWeeks > 0 && streak%h.BonusWeeks == 0 {
				bonus++
			}
		case Miss:
			streak = 0
			misses++
			if misses > h.Grace && h.Mode == Streak {
				cycle = 0
			}
		}
	}
	need := nextPointWeeks
	if base == 0 {
		need = firstPointWeeks
	}
	return Earned{Hits: hits, Points: base + bonus, Base: base, Bonus: bonus, BonusWeeks: max(h.BonusWeeks, 0), Streak: streak, Cycle: cycle, NeedWeeks: need}
}

// Checkin 是一筆 habits.checkin 讀進來的樣子。Seq 是編號的數字（C-12 → 12），決定先後。
type Checkin struct {
	Seq    int
	Habit  string
	Date   Day
	Sub    string
	Source string
	Key    string
	Undo   bool
}

// ActiveCheckins 依編號順序處理，回還有效的打卡：同一個 key 有效期間只算一次；
// undo 撤銷同一習慣、同一天、同一子項目最後一筆還有效的打卡。
func ActiveCheckins(rows []Checkin) []Checkin {
	sorted := append([]Checkin(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Seq < sorted[j].Seq })
	type slot struct {
		habit string
		date  Day
		sub   string
	}
	stacks := map[slot][]Checkin{}
	activeKey := map[string]bool{}
	for _, r := range sorted {
		s := slot{r.Habit, r.Date, r.Sub}
		if r.Undo {
			st := stacks[s]
			if len(st) == 0 {
				continue
			}
			last := st[len(st)-1]
			stacks[s] = st[:len(st)-1]
			if last.Key != "" {
				delete(activeKey, r.Habit+"\x00"+last.Key)
			}
			continue
		}
		if r.Key != "" {
			k := r.Habit + "\x00" + r.Key
			if activeKey[k] {
				continue
			}
			activeKey[k] = true
		}
		stacks[s] = append(stacks[s], r)
	}
	var out []Checkin
	for _, st := range stacks {
		out = append(out, st...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out
}

// cardioTypes 是 fitness.workout 裡算「有氧」的種類（來源 fitness:有氧）；跟 fitness 套件 pages/lib/cardio.ts 的 CARDIO、schemas/workout.toml 的 type 列舉一起改。
var cardioTypes = map[string]bool{"跑步": true, "划船": true, "游泳": true, "騎車": true}

// Workout 是 fitness.workout 讀進來要用的部分。
type Workout struct {
	Type string // 重訓／跑步／划船／游泳／騎車
	Date Day
	Done bool // status = 完成
}

// Inputs 是算習慣要讀的別的種類的資料；讀不到的種類就是空的（套件沒裝不報錯）。
type Inputs struct {
	Checkins   []Checkin      // habits.checkin 全部（含 undo）
	Workouts   []Workout      // fitness.workout
	Reminders  map[string]Day // 待辦 id → 完成那天
	WeightDays map[Day]bool   // 有 body.entry 而且有 weight 的日子
	MealCounts map[Day]int    // nutrition.meal 每天幾筆
}

// SubDone 是某天某個子項目怎麼做到的：manual 或 auto。
type SubDone map[string]string

// SubDays 每天做到了哪幾項（手動打卡的加自動來源推算的；兩者都有算 manual）。active 是 ActiveCheckins 的結果。
func SubDays(h Habit, active []Checkin, in Inputs) map[Day]SubDone {
	out := map[Day]SubDone{}
	put := func(d Day, name, how string) {
		if out[d] == nil {
			out[d] = SubDone{}
		}
		out[d][name] = how
	}
	names := map[string]bool{}
	for _, s := range h.Subtasks {
		names[s.Name] = true
		switch s.Source {
		case SubBodyWeight:
			for d := range in.WeightDays {
				put(d, s.Name, "auto")
			}
		case SubNutritionMeal:
			for d, c := range in.MealCounts {
				if c >= s.Min {
					put(d, s.Name, "auto")
				}
			}
		}
	}
	for _, r := range active {
		if r.Habit == h.ID && r.Sub != "" && names[r.Sub] {
			put(r.Date, r.Sub, "manual")
		}
	}
	return out
}

// Dates 是這個習慣所有算數的打卡日（同一天可以多筆）。沒有子項目：手動打卡＋自動來源（每筆運動、每個完成的待辦各一次）；
// 有子項目：做到的項數夠的那些天各算一次（沒指定子項目的手動打卡與自動來源也算那天完成）。
func Dates(h Habit, active []Checkin, in Inputs) []Day {
	var dates []Day
	for _, r := range active {
		if r.Habit == h.ID && r.Sub == "" {
			dates = append(dates, r.Date)
		}
	}
	for _, src := range h.Sources {
		switch kind, arg := cut(src); kind {
		case "fitness":
			for _, w := range in.Workouts {
				if w.Done && (arg == "*" || arg == w.Type || (arg == "有氧" && cardioTypes[w.Type])) {
					dates = append(dates, w.Date)
				}
			}
		case "reminder":
			if d, ok := in.Reminders[arg]; ok {
				dates = append(dates, d)
			}
		}
	}
	if len(h.Subtasks) == 0 {
		return dates
	}
	set := map[Day]bool{}
	for _, d := range dates {
		set[d] = true
	}
	for d, got := range SubDays(h, active, in) {
		if len(got) >= h.NeedSubs() {
			set[d] = true
		}
	}
	return sortedDays(set)
}

func cut(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// ThisWeek 是本週進度：done 次（每週型是打卡數、每天型是做了幾天）、need（每週型 target、每天型 7）、做了的日子。
type ThisWeek struct {
	Done int
	Need int
	Days []Day
}

// State 是一個習慣現在的狀態（規則算出來的，還沒跟點數帳合併）。
type State struct {
	Habit       Habit
	Weeks       []Week
	Earned      Earned
	ThisWeek    ThisWeek
	TodaySubs   []TodaySub
	TodayManual int // 今天還有效的手動打卡（沒指定子項目）筆數
	DoneDays    []Day
	SkippedDays []Day
	BrokenDays  []Day
	ManualDays  []Day // 有手動打卡（沒指定子項目）的日子：月曆只有這些能撤銷
}

// TodaySub 是有子項目的習慣今天各項做了沒。
type TodaySub struct {
	Name string
	Done bool
	How  string // manual、auto 或空
}

// Compute 算一個習慣。active 是 ActiveCheckins(in.Checkins)。
func Compute(h Habit, active []Checkin, in Inputs, today Day) State {
	dates := Dates(h, active, in)
	weeks := WeekResults(h, dates, today)
	st := State{Habit: h, Weeks: weeks, Earned: Earn(h, weeks)}
	st.DoneDays, st.SkippedDays, st.BrokenDays = DayMarks(h, dates, today)
	manual := map[Day]bool{}
	for _, r := range active {
		if r.Habit == h.ID && r.Sub == "" && r.Source == "manual" {
			manual[r.Date] = true
		}
	}
	st.ManualDays = lastDays(sortedDays(manual))
	mon := today.Monday()
	lo := max(mon, h.Start)
	hi := today
	if h.HasEnded && h.Ended < hi {
		hi = h.Ended
	}
	days := map[Day]bool{}
	count := 0
	for _, d := range dates {
		if d >= lo && d <= hi && d <= mon+6 {
			days[d] = true
			count++
		}
	}
	st.ThisWeek = ThisWeek{Days: sortedDays(days)}
	if h.Cadence == Weekly {
		st.ThisWeek.Done, st.ThisWeek.Need = count, h.Target
	} else {
		st.ThisWeek.Done, st.ThisWeek.Need = len(days), 7
	}
	if len(h.Subtasks) > 0 {
		got := SubDays(h, active, in)[today]
		for _, s := range h.Subtasks {
			how, ok := got[s.Name]
			st.TodaySubs = append(st.TodaySubs, TodaySub{Name: s.Name, Done: ok, How: how})
		}
	}
	for _, r := range active {
		if r.Habit == h.ID && r.Date == today && r.Sub == "" && r.Source == "manual" {
			st.TodayManual++
		}
	}
	return st
}

// Wallet 是全域錢包的結果。
type Wallet struct {
	Banked map[string]int // 習慣 id → 賺到過的最高點數（只增不減，包含已刪的習慣）
	Tasks  int            // 待辦累積的點數（只增不減）
	// TaskExchanged：已經換成錢包點的待辦點數（只增不減；含 TaskLegacy）。Tasks − TaskExchanged 是還沒換的（HAB-18）。
	TaskExchanged int
	// TaskLegacy：HAB-18 之前自動換掉的待辦點數（第一次升級時從舊帳算一次，之後照抄）。
	TaskLegacy int
	// TaskWallet：待辦點數換到的錢包點（只增不減）。兌換價錢改過（20→30，DEV-149），不能再用 TaskExchanged 除回去。
	TaskWallet int
	// TaskOldRequests：照舊價 20 算的兌換筆數（改價後第一次重算時記下當時的筆數，之後照抄）。
	TaskOldRequests int
	// TaskOldUnset：改價前的筆數這次還定不了（舊錢包又讀不到兌換紀錄），不寫進錢包。
	TaskOldUnset bool
	// Starter、StarterHabit：起手禮走到哪一步、送給哪個習慣（HAB-20，WithStarter 填）。
	Starter      string
	StarterHabit string
	Earned       int
	Spent        int
	Points       int
}

// TaskBook 是算待辦點數要的帳（HAB-13、HAB-18）。
type TaskBook struct {
	Read         int  // 這次讀到的待辦累積點數（全部待辦的 done_points 加總）
	Old          int  // 錢包舊帳的 task_points
	OldExchanged int  // 錢包舊帳的 task_exchanged
	Legacy       int  // 錢包舊帳的 task_legacy
	HasLegacy    bool // 舊帳有沒有 task_legacy（沒有＝HAB-18 之前的錢包，從 Old 算一次）
	Requested    int  // 主人按了幾次「兌換」（habits.exchange 筆數）
	OldWallet    int  // 錢包舊帳的 task_wallet
	HasWallet    bool // 舊帳有沒有 task_wallet（沒有＝改價前的錢包，從 OldExchanged 照 20 算一次）
	OldRequests  int  // 錢包舊帳的 task_old_requests
	HasOld       bool // 舊帳有沒有 task_old_requests（沒有＝改價前的錢包，現在的筆數都照舊價）
	Unread       bool // 這次讀不到兌換紀錄（Requested 是 0 不可信）
}

// Bank 把這次算出來的點數併進舊的點數帳（ledger）：每個習慣取 max(舊, 新)，帳上有但這次沒有的習慣（已刪、已封存）照留。
// 待辦點數跟舊帳取 max 一直往上加；主人按一次兌換用掉一份點數換一個錢包點，不夠就不換（HAB-18）。
// 一份的價錢：改價前按的那幾筆 20、之後的 30（DEV-149）；照順序換，前面一筆不夠，後面的也先不換。
func Bank(states []State, ledger map[string]int, spent int, t TaskBook) Wallet {
	w := Wallet{Banked: map[string]int{}, Spent: spent, Tasks: max(t.Read, t.Old)}
	w.TaskLegacy = t.Legacy
	if !t.HasLegacy {
		w.TaskLegacy = t.Old / OldTaskPointsPerWalletPoint * OldTaskPointsPerWalletPoint // 舊版每滿 20 自動換過
	}
	w.TaskOldRequests = t.OldRequests
	if !t.HasOld {
		w.TaskOldRequests, w.TaskOldUnset = t.Requested, t.Unread
	}
	left, used, got := w.Tasks-w.TaskLegacy, 0, 0
	for i := range t.Requested {
		cost := TaskPointsPerWalletPoint
		if i < w.TaskOldRequests {
			cost = OldTaskPointsPerWalletPoint
		}
		if used+cost > left {
			break
		}
		used += cost
		got++
	}
	w.TaskExchanged = max(t.OldExchanged, w.TaskLegacy+used)
	oldWallet := t.OldWallet
	if !t.HasWallet {
		oldWallet = t.OldExchanged / OldTaskPointsPerWalletPoint
	}
	w.TaskWallet = max(oldWallet, w.TaskLegacy/OldTaskPointsPerWalletPoint+got)
	for id, p := range ledger {
		w.Banked[id] = p
	}
	for _, s := range states {
		if s.Earned.Points > w.Banked[s.Habit.ID] {
			w.Banked[s.Habit.ID] = s.Earned.Points
		}
	}
	for _, p := range w.Banked {
		w.Earned += p
	}
	w.Earned += w.TaskWallet
	w.Points = max(w.Earned-spent, 0)
	return w
}
