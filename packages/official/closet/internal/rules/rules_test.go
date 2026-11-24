package rules

import (
	"strings"
	"testing"
)

func outfit(persona, name string, cost int, state string) Outfit {
	return Outfit{Record: RecordID(persona, name), Persona: persona, Outfit: name, Name: name, Cost: cost, State: state, Source: SourceOfficial}
}

func fields(persona, name string, cost int, state string) map[string]any {
	return map[string]any{"persona": persona, "outfit": name, "name": "名", "cost": cost, "state": state, "source": "official"}
}

// Guards: REQ-125
func TestOwnedFollowsCostAndUnlock(t *testing.T) {
	free := outfit("athena", "base", 0, StateActive)
	paid := outfit("athena", "seaside", 2, StateActive)
	draftFree := outfit("athena", "wip", 0, StateDraft)
	retiredPaid := outfit("athena", "old", 3, StateRetired)
	retiredFree := outfit("athena", "gift", 0, StateRetired)
	unlocked := map[string]bool{}
	cases := []struct {
		o    Outfit
		want bool
	}{{free, true}, {paid, false}, {draftFree, false}, {retiredPaid, false}, {retiredFree, true}}
	for _, c := range cases {
		if got := Owned(c.o, unlocked); got != c.want {
			t.Errorf("%s owned = %v, 要 %v", c.o.Record, got, c.want)
		}
	}
	unlocked["athena-seaside"], unlocked["athena-old"], unlocked["athena-wip"] = true, true, true
	if !Owned(paid, unlocked) {
		t.Error("有 unlock 要擁有")
	}
	if !Owned(retiredPaid, unlocked) {
		t.Error("下架但兌換過的人照樣有")
	}
	if Owned(draftFree, unlocked) {
		t.Error("草稿不算擁有，就算有 unlock")
	}
}

// Guards: REQ-125
func TestOutfitFromFieldsValidates(t *testing.T) {
	if _, err := OutfitFromFields("athena-seaside", fields("athena", "seaside", 1, "active")); err != nil {
		t.Fatal(err)
	}
	bad := map[string]map[string]any{
		"athena-x":       fields("athena", "seaside", 1, "active"), // 編號對不上
		"Athena-seaside": fields("Athena", "seaside", 1, "active"),
		"athena-Sea":     fields("athena", "Sea", 1, "active"),
		"athena-sea":     fields("athena", "sea", -1, "active"),
	}
	for id, f := range bad {
		if _, err := OutfitFromFields(id, f); err == nil {
			t.Errorf("%s 要被略過", id)
		}
	}
	f := fields("athena", "sea", 1, "weird")
	if _, err := OutfitFromFields("athena-sea", f); err == nil {
		t.Error("state 不認得要被略過")
	}
}

// Guards: REQ-125
func TestSortOutfitsByOrderThenID(t *testing.T) {
	a := Outfit{Record: "p-b", Order: 2, HasOrder: true}
	b := Outfit{Record: "p-a", Order: 2, HasOrder: true}
	c := Outfit{Record: "p-c", Order: 1, HasOrder: true}
	d := Outfit{Record: "p-0"}
	list := []Outfit{d, a, b, c}
	SortOutfits(list)
	var got []string
	for _, o := range list {
		got = append(got, o.Record)
	}
	if strings.Join(got, ",") != "p-c,p-a,p-b,p-0" {
		t.Fatalf("順序：%v", got)
	}
}

// Guards: REQ-113, REQ-125
func TestWearingRotatesByHour(t *testing.T) {
	list := []string{"base", "night", "sea"}
	for hour, want := range map[int64]int{0: 0, 1: 1, 2: 2, 3: 0, 100: 1} {
		if got := WearingIndex(hour, len(list)); got != want {
			t.Errorf("hour %d → %d，要 %d", hour, got, want)
		}
	}
	if WearingIndex(5, 0) != -1 {
		t.Error("清單空要 -1")
	}
	if Hour(7200) != 2 || Hour(7199) != 1 {
		t.Error("Hour")
	}
	owned := []Outfit{outfit("a", "base", 0, StateActive), outfit("a", "night", 1, StateActive), outfit("a", "sea", 1, StateActive)}
	// 勾的裡面還擁有的，照目錄順序。
	if got := EffectiveRotation([]string{"sea", "night", "gone"}, owned); strings.Join(got, ",") != "night,sea" {
		t.Errorf("有效清單：%v", got)
	}
	// 沒有勾（或勾的都不在了）→ 第一套擁有的。
	if got := EffectiveRotation(nil, owned); strings.Join(got, ",") != "base" {
		t.Errorf("預設：%v", got)
	}
	if got := EffectiveRotation([]string{"gone"}, owned); strings.Join(got, ",") != "base" {
		t.Errorf("勾的都不在：%v", got)
	}
	if got := EffectiveRotation([]string{"base"}, nil); len(got) != 0 {
		t.Errorf("沒有任何擁有的：%v", got)
	}
}

// Guards: REQ-113
func TestPhotoIndexRotates(t *testing.T) {
	// 兩套輪流、一套有 3 張：每過 2 小時換一張。
	want := []int{0, 0, 1, 1, 2, 2, 0, 0}
	for hour, w := range want {
		if got := PhotoIndex(int64(hour), 2, 3); got != w {
			t.Errorf("hour %d → %d，要 %d", hour, got, w)
		}
	}
	if PhotoIndex(3, 1, 0) != -1 || PhotoIndex(3, 0, 2) != -1 {
		t.Error("沒有照片／沒有衣服要 -1")
	}
}

// Guards: REQ-125
func TestAvailablePointsFromEarnedMinusUnlocks(t *testing.T) {
	// 剛兌換一套（花 2 點）；habits 的 wallet.spent 還沒更新，但這裡用 earned 自己減，不會重複花。
	unlocks := []Unlock{{Record: "a-x", Cost: 2}}
	if got := Available(3, unlocks); got != 1 {
		t.Fatalf("3 - 2 = %d", got)
	}
	unlocks = append(unlocks, Unlock{Record: "a-y", Cost: 1})
	if got := Available(3, unlocks); got != 0 {
		t.Fatalf("%d", got)
	}
	if got := Available(1, unlocks); got != 0 {
		t.Fatalf("最少 0：%d", got)
	}
	// cost 沒寫當 1（跟 habits 一致）。
	if u := UnlockFromFields("a-z", map[string]any{}); u.Cost != 1 {
		t.Fatalf("沒寫 cost：%d", u.Cost)
	}
	if u := UnlockFromFields("a-z", map[string]any{"cost": 4.0}); u.Cost != 4 {
		t.Fatalf("cost：%d", u.Cost)
	}
}

func baseInput() RequestInput {
	cat := map[string]Outfit{}
	for _, o := range []Outfit{
		outfit("athena", "base", 0, StateActive),
		outfit("athena", "sea", 2, StateActive),
		outfit("athena", "wip", 1, StateDraft),
		outfit("athena", "old", 1, StateRetired),
		outfit("athena", "gift", 0, StateRetired),
	} {
		cat[o.Record] = o
	}
	return RequestInput{By: "owner(web)", Persona: "athena", Outfit: "sea", Catalog: cat, Unlocks: map[string]bool{}, Wallet: true, Points: 2}
}

// Guards: REQ-125
func TestUnlockRequestRules(t *testing.T) {
	cases := []struct {
		name   string
		mod    func(*RequestInput)
		reason string // 空＝通過；用 Contains 比對
	}{
		{"通過", func(*RequestInput) {}, ""},
		{"cli 的主人也行", func(in *RequestInput) { in.By = "owner(cli)" }, ""},
		{"秘書不行", func(in *RequestInput) { in.By = "code" }, ReasonNotOwner},
		{"別人偽裝", func(in *RequestInput) { in.By = "closet" }, ReasonNotOwner},
		{"沒有這套", func(in *RequestInput) { in.Outfit = "nope" }, ReasonNotFound},
		{"人設對不上", func(in *RequestInput) { in.Persona = "zeus" }, ReasonNotFound},
		{"草稿", func(in *RequestInput) { in.Outfit = "wip" }, ReasonNotYet},
		{"下架沒擁有", func(in *RequestInput) { in.Outfit = "old" }, ReasonNotYet},
		{"下架但免費（已擁有）", func(in *RequestInput) { in.Outfit = "gift" }, ReasonHaveIt},
		{"免費", func(in *RequestInput) { in.Outfit = "base" }, ReasonHaveIt},
		{"已擁有", func(in *RequestInput) { in.Unlocks["athena-sea"] = true }, ReasonHaveIt},
		{"沒有錢包", func(in *RequestInput) { in.Wallet = false }, ReasonNoWallet},
		{"點數不夠", func(in *RequestInput) { in.Points = 1 }, "點數不夠：要 2 點，現在 1 點"},
	}
	for _, c := range cases {
		in := baseInput()
		c.mod(&in)
		_, reason := CheckUnlock(in)
		if c.reason == "" && reason != "" || c.reason != "" && !strings.Contains(reason, c.reason) {
			t.Errorf("%s：reason = %q，要 %q", c.name, reason, c.reason)
		}
	}
}

// 偽造的兌換（不是 closet／migrate 寫的）不被承認。
// Guards: REQ-125
func TestForgedUnlockNotTrusted(t *testing.T) {
	for by, want := range map[string]bool{"closet": true, "migrate": true, "owner(web)": false, "owner(cli)": false, "code": false, "habits": false, "": false} {
		if TrustedActor(by) != want {
			t.Errorf("TrustedActor(%q) = %v", by, !want)
		}
	}
	// 沒被認得的兌換不進 unlocked，所以衣服照樣沒擁有、點數照樣沒少。
	sea := outfit("athena", "sea", 2, StateActive)
	if Owned(sea, map[string]bool{}) {
		t.Error("沒有認得的兌換不該擁有")
	}
	if Available(2, nil) != 2 {
		t.Error("偽造的兌換不該影響點數")
	}
}

// 設計中每張圖各自採用（SEC-10）：adopted 是字串 list；沒寫是空。
func TestOutfitAdoptedField(t *testing.T) {
	base := map[string]any{"persona": "athena", "outfit": "sea", "name": "海邊", "cost": 1, "state": "draft", "source": "official"}
	o, err := OutfitFromFields("athena-sea", base)
	if err != nil || len(o.Adopted) != 0 {
		t.Fatalf("沒寫 adopted：%v %v", o.Adopted, err)
	}
	base["adopted"] = []any{"half-1", "", "full"}
	o, err = OutfitFromFields("athena-sea", base)
	if err != nil || len(o.Adopted) != 2 || o.Adopted[0] != "half-1" || o.Adopted[1] != "full" {
		t.Fatalf("adopted：%v %v", o.Adopted, err)
	}
}
