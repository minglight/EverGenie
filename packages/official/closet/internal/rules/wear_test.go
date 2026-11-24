package rules

import (
	"testing"
	"time"
)

// 現在穿這套（CLO-30）：穿到當天結束（now 所在時區的隔天 00:00）；時間到、沒擁有、沒指定都不算。
func TestWearUntilIsEndOfLocalDay(t *testing.T) {
	taipei := time.FixedZone("UTC+8", 8*3600)
	for _, c := range []struct {
		name      string
		now, want time.Time
	}{
		{"白天按：到今天 24:00", time.Date(2026, 11, 14, 10, 58, 0, 0, taipei), time.Date(2026, 11, 15, 0, 0, 0, 0, taipei)},
		{"23:59 按：到 24:00", time.Date(2026, 11, 14, 23, 59, 59, 0, taipei), time.Date(2026, 11, 15, 0, 0, 0, 0, taipei)},
		{"00:00 整按：到當天 24:00，不是立刻到期", time.Date(2026, 11, 14, 0, 0, 0, 0, taipei), time.Date(2026, 11, 15, 0, 0, 0, 0, taipei)},
		{"月底跨月", time.Date(2026, 11, 30, 12, 0, 0, 0, taipei), time.Date(2026, 12, 1, 0, 0, 0, 0, taipei)},
		{"照 now 的時區算，不是 UTC", time.Date(2026, 11, 14, 20, 0, 0, 0, time.UTC), time.Date(2026, 11, 15, 0, 0, 0, 0, time.UTC)},
	} {
		got := WearUntil(c.now)
		if !got.Equal(c.want) {
			t.Errorf("%s：WearUntil = %s，要 %s", c.name, got, c.want)
		}
		if !got.After(c.now) {
			t.Errorf("%s：到期時間 %s 不在現在 %s 之後", c.name, got, c.now)
		}
	}
}

// 有夏令時間的時區也不會算出過去的時間（每一個小時都試，含調時鐘的那兩天）。
func TestWearUntilNeverInThePast(t *testing.T) {
	for _, zone := range []string{"America/New_York", "America/Sao_Paulo", "Asia/Taipei", "Australia/Lord_Howe"} {
		loc, err := time.LoadLocation(zone)
		if err != nil {
			t.Skipf("這台沒有時區資料 %s：%v", zone, err)
		}
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
		for i := 0; i < 366*24; i++ {
			got := WearUntil(at)
			if !got.After(at) || got.Sub(at) > 25*time.Hour {
				t.Fatalf("%s %s：WearUntil = %s", zone, at, got)
			}
			at = at.Add(time.Hour)
		}
	}
}

func TestPickedNeedsOwnedAndNotExpired(t *testing.T) {
	owned := []Outfit{{Outfit: "base"}, {Outfit: "night"}}
	for _, c := range []struct {
		name       string
		wear       string
		until, now int64
		want       bool
	}{
		{"指定而且還沒到", "night", 7200, 7199, true},
		{"時間剛到就回到輪流", "night", 7200, 7200, false},
		{"過期", "night", 7200, 9000, false},
		{"沒擁有的不能穿", "sea", 7200, 7199, false},
		{"沒指定", "", 7200, 7199, false},
		{"時間寫壞了（0）", "night", 0, 100, false},
	} {
		if got := Picked(c.wear, c.until, c.now, owned); got != c.want {
			t.Errorf("%s：Picked = %v，要 %v", c.name, got, c.want)
		}
	}
}

func TestCheckWearOnlyOwned(t *testing.T) {
	catalog := map[string]Outfit{
		"a-base":  {Record: "a-base", Persona: "a", Outfit: "base", Cost: 0, State: StateActive},
		"a-night": {Record: "a-night", Persona: "a", Outfit: "night", Cost: 1, State: StateActive},
		"a-sea":   {Record: "a-sea", Persona: "a", Outfit: "sea", Cost: 1, State: StateActive},
		"a-draft": {Record: "a-draft", Persona: "a", Outfit: "draft", Cost: 0, State: StateDraft},
		"a-old":   {Record: "a-old", Persona: "a", Outfit: "old", Cost: 1, State: StateRetired},
	}
	unlocks := map[string]bool{"a-night": true, "a-old": true}
	for _, c := range []struct{ outfit, want string }{
		{"base", ""},
		{"night", ""},
		{"old", ""}, // 下架但已擁有的照樣能穿
		{"sea", ReasonNotHers},
		{"draft", ReasonNotHers},
		{"nope", ReasonNotFound},
	} {
		if _, got := CheckWear("a", c.outfit, catalog, unlocks); got != c.want {
			t.Errorf("CheckWear(%s) = %q，要 %q", c.outfit, got, c.want)
		}
	}
	if _, got := CheckWear("b", "base", catalog, unlocks); got != ReasonNotFound {
		t.Errorf("別的角色：%q", got)
	}
}
