package rules

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// 習慣的選項值（vault 欄位的列舉 key）。
const (
	Daily      = "daily"
	Weekly     = "weekly"
	Cumulative = "cumulative"
	Streak     = "streak"
	Active     = "active"
	Ended      = "ended"
)

// 子項目的自動來源（讀別的種類的資料即時推算，不寫打卡）。
const (
	SubBodyWeight    = "body:weight"
	SubNutritionMeal = "nutrition:meals"
)

// Sub 是習慣的一個子項目。Source 空＝只能手動打卡。
type Sub struct {
	Name   string
	Source string
	Min    int // nutrition:meals 至少幾筆才算（>=N，預設 1）
}

// Habit 是一筆 habits.habit 讀進來並補好預設值的樣子。
type Habit struct {
	ID        string
	Name      string
	Agent     string
	Cadence   string
	Target    int
	MissDays  int
	WeekSkips int
	Mode      string
	Grace     int
	// BonusWeeks：連續達標幾週給一次獎勵點（0＝不給；vault 沒寫＝DefaultBonusWeeks）。
	BonusWeeks int
	Start      Day
	Status     string
	Ended      Day // HasEnded 才有意義
	HasEnded   bool
	Sources    []string
	Subtasks   []Sub
	AllowMiss  int
}

// IsEnded：狀態是 ended。
func (h Habit) IsEnded() bool { return h.Status == Ended }

// ParseSub 把「名稱」「名稱=來源」「名稱=nutrition:meals>=N」拆開。
func ParseSub(s string) (Sub, error) {
	name, rest, _ := strings.Cut(s, "=")
	src, min, hasMin := strings.Cut(rest, ">=")
	sub := Sub{Name: strings.TrimSpace(name), Source: strings.TrimSpace(src), Min: 1}
	if hasMin && strings.TrimSpace(min) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(min))
		if err != nil {
			return Sub{}, fmt.Errorf("子項目「%s」的最少筆數要是整數（>=N）", s)
		}
		sub.Min = n
	}
	return sub, nil
}

// CheckSubs 檢查子項目與 allow_miss（跟舊系統同一套說法）。
func CheckSubs(subs []Sub, allowMiss int) error {
	seen := map[string]bool{}
	for _, s := range subs {
		if s.Name == "" || seen[s.Name] {
			return fmt.Errorf("子項目的名稱不能是空的，也不能重複")
		}
		seen[s.Name] = true
	}
	for _, s := range subs {
		if s.Source != "" && s.Source != SubBodyWeight && s.Source != SubNutritionMeal {
			return fmt.Errorf("子項目「%s」的自動來源看不懂：%s（%s、%s，或不寫＝手動）", s.Name, s.Source, SubBodyWeight, SubNutritionMeal)
		}
		if s.Min < 1 {
			return fmt.Errorf("子項目「%s」的最少筆數要 ≥ 1", s.Name)
		}
	}
	if len(subs) > 0 && (allowMiss < 0 || allowMiss >= len(subs)) {
		return fmt.Errorf("最多可漏幾項（allow_miss）要在 0–%d（至少要做 1 項）", len(subs)-1)
	}
	if len(subs) == 0 && allowMiss != 0 {
		return fmt.Errorf("沒有子項目，allow_miss 不能大於 0")
	}
	return nil
}

// CheckSource 檢查習慣的自動來源：fitness:<種類>／有氧／*、reminder:R-<n>。
func CheckSource(s string) error {
	kind, arg, _ := strings.Cut(s, ":")
	if (kind != "fitness" && kind != "reminder") || arg == "" {
		return fmt.Errorf("自動打卡來源看不懂：%s（fitness:重訓／fitness:跑步／fitness:有氧／fitness:*／reminder:R-<n>）", s)
	}
	return nil
}

// NeedSubs 是當天要做到幾項才算打卡（沒有子項目時是 1）。
func (h Habit) NeedSubs() int {
	if len(h.Subtasks) == 0 {
		return 1
	}
	return max(len(h.Subtasks)-h.AllowMiss, 1)
}

// Validate 檢查整個習慣（頁面存檔前用 pages/lib/habits.ts 做同樣的檢查）。
func (h Habit) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("習慣要有名字")
	}
	if h.Agent == "" {
		return fmt.Errorf("習慣要指定跟哪位秘書約（agent）")
	}
	if h.Cadence != Daily && h.Cadence != Weekly {
		return fmt.Errorf("頻率只能是 daily 或 weekly")
	}
	if h.Mode != Cumulative && h.Mode != Streak {
		return fmt.Errorf("模式只能是 cumulative 或 streak")
	}
	if h.Status != Active && h.Status != Ended {
		return fmt.Errorf("狀態只能是 active 或 ended")
	}
	if h.Cadence == Weekly && (h.Target < 1 || h.Target > 7) {
		return fmt.Errorf("每週幾次（target）要在 1–7")
	}
	if h.MissDays < 0 || h.MissDays > 6 {
		return fmt.Errorf("miss_days 要在 0–6")
	}
	if h.WeekSkips < 0 || h.WeekSkips > 6 {
		return fmt.Errorf("week_skips 要在 0–6")
	}
	if h.Grace < 0 || h.Grace > 6 {
		return fmt.Errorf("grace 要在 0–6")
	}
	if h.BonusWeeks < 0 || h.BonusWeeks == 1 || h.BonusWeeks > 12 {
		return fmt.Errorf("bonus_weeks 要是 0 或 2–12（0＝沒有連續獎勵；1 等於每週加點，不收）")
	}
	if err := CheckSubs(h.Subtasks, h.AllowMiss); err != nil {
		return err
	}
	for _, s := range h.Sources {
		if err := CheckSource(s); err != nil {
			return err
		}
	}
	return nil
}

// HabitFromFields 把 vault 的欄位轉成 Habit（補預設值）並驗證。endFallback 是「已結束但沒寫結束日」時用的日子
// （通常是記錄最後修改的那天）。
func HabitFromFields(id string, f map[string]any, endFallback Day) (Habit, error) {
	h := Habit{ID: id, MissDays: 1, WeekSkips: DefaultWeekSkips, Grace: 1, BonusWeeks: DefaultBonusWeeks, Cadence: str(f["cadence"]), Mode: str(f["mode"]), Status: str(f["status"])}
	h.Name, h.Agent = str(f["name"]), str(f["agent"])
	for _, p := range []struct {
		key string
		dst *int
	}{{"target", &h.Target}, {"miss_days", &h.MissDays}, {"week_skips", &h.WeekSkips}, {"grace", &h.Grace}, {"bonus_weeks", &h.BonusWeeks}, {"allow_miss", &h.AllowMiss}} {
		if v, ok := f[p.key]; ok && v != nil && v != "" {
			n, ok := asInt(v)
			if !ok {
				return h, fmt.Errorf("%s 要是整數", p.key)
			}
			*p.dst = n
		}
	}
	if h.Status == "" {
		h.Status = Active
	}
	if h.Cadence == Daily && h.Target == 0 {
		h.Target = 7
	}
	start, ok := ParseDay(str(f["start"]))
	if !ok {
		return h, fmt.Errorf("start 要是日期（YYYY-MM-DD）")
	}
	h.Start = start
	if h.Status == Ended {
		if e, ok := ParseDay(str(f["ended"])); ok {
			h.Ended, h.HasEnded = e, true
		} else {
			h.Ended, h.HasEnded = max(endFallback, h.Start), true
		}
	}
	h.Sources = strList(f["sources"])
	for _, s := range strList(f["subtasks"]) {
		sub, err := ParseSub(s)
		if err != nil {
			return h, err
		}
		h.Subtasks = append(h.Subtasks, sub)
	}
	if err := h.Validate(); err != nil {
		return h, err
	}
	return h, nil
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		if n == math.Trunc(n) {
			return int(n), true
		}
	}
	return 0, false
}

func strList(v any) []string {
	var out []string
	switch l := v.(type) {
	case []string:
		out = append(out, l...)
	case []any:
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

// sortedDays 把集合變成由小到大的清單。
func sortedDays(set map[Day]bool) []Day {
	out := make([]Day, 0, len(set))
	for d := range set {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
