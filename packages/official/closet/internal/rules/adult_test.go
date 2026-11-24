package rules

import (
	"testing"
	"time"
)

// 開關只認 on（不分大小寫、去空白）；沒設、off、true、1 都是關，免得誤開。
func TestAdultEnabledOnlyOn(t *testing.T) {
	for _, v := range []string{"on", " ON ", "On"} {
		if !AdultEnabled(v) {
			t.Errorf("%q 要算開", v)
		}
	}
	for _, v := range []string{"", "off", "true", "1", "yes", "onn"} {
		if AdultEnabled(v) {
			t.Errorf("%q 要算關", v)
		}
	}
}

// 隱藏版只留一組（DEV-160 S2）：自己的隱藏版開關關著時，主人自己的設定（dir 在 mine/ 底下）不算進那一組；
// 開著＝任一筆 on 或在任一筆的時段裡。
func TestHiddenGroupOneSwitch(t *testing.T) {
	list := []Variant{{ID: "night", Dir: "avatars/hidden", Order: 2, HasOrder: true}, {ID: "adult", Dir: "mine/adult", On: true}, {ID: "a", Dir: "avatars/a", Order: 1, HasOrder: true}}
	off := HiddenGroup(list, false, nil)
	if len(off) != 2 || off[0].ID != "a" || off[1].ID != "night" || len(list) != 3 {
		t.Fatalf("開關關著：%v（原本 %v）", off, list)
	}
	now := time.Date(2026, 11, 15, 12, 30, 0, 0, time.Local)
	if HiddenOpen(off, now) {
		t.Fatal("主人自己的設定開著不算（開關關著）")
	}
	if on := HiddenGroup(list, true, nil); len(on) != 3 || !HiddenOpen(on, now) {
		t.Fatalf("開關開著：%v 要算、而且開著", on)
	}
	w, _ := ParseWindow("12:00-13:00")
	off[1].Windows = []Window{w}
	if !HiddenOpen(off, now) || HiddenOpen(off, now.Add(time.Hour)) {
		t.Fatal("在任一筆的時段裡就開著，時段外關著")
	}
}

// 可以升成官方：整套都是隱藏版的照片不行（DEV-160 S2 起看紀錄，不看名字）。
func TestPromotableNotAllHidden(t *testing.T) {
	o := Outfit{Record: "athena-adultish", Outfit: "adultish", Name: "成人", Source: SourceMine, State: StateActive}
	if !Promotable(true, true, o, false) {
		t.Fatal("名字不再擋，有一般版的照片就可以")
	}
	if Promotable(true, true, o, true) {
		t.Fatal("整套都是隱藏版的照片不行")
	}
	if Promotable(false, true, o, false) || Promotable(true, false, o, false) {
		t.Fatal("要開發家、官方角色")
	}
}

// 升成官方後（from 指向已經不在目錄裡的舊編號、那筆是 closet-mine 寫的），舊編號的兌換要搬到新編號；
// from 指向還在的衣服、別位角色、不是 closet-mine 寫的、好幾筆指同一個舊編號的，一律不搬。
func TestCarryPlanOnlyTrustedUniqueFrom(t *testing.T) {
	catalog := map[string]Outfit{
		"athena-seaside": {Record: "athena-seaside", Persona: "athena", From: "athena-my-12", By: ActorMine},
		"athena-night":   {Record: "athena-night", Persona: "athena", From: "athena-sea", By: ActorMine},
		"athena-sea":     {Record: "athena-sea", Persona: "athena"},
		"athena-steal":   {Record: "athena-steal", Persona: "athena", From: "luna-my-3", By: ActorMine},
		"athena-forged":  {Record: "athena-forged", Persona: "athena", From: "athena-my-5", By: "code"},
		"athena-dup1":    {Record: "athena-dup1", Persona: "athena", From: "athena-my-7", By: ActorMine},
		"athena-dup2":    {Record: "athena-dup2", Persona: "athena", From: "athena-my-7", By: ActorMine},
	}
	var unlocks []Unlock
	for _, id := range []string{"athena-my-12", "athena-sea", "luna-my-3", "athena-my-5", "athena-my-7"} {
		unlocks = append(unlocks, Unlock{Record: id, Cost: 2})
	}
	got := CarryPlan(catalog, unlocks)
	if len(got) != 1 || got[0].From != "athena-my-12" || got[0].To != "athena-seaside" || got[0].Unlock.Cost != 2 {
		t.Fatalf("只該搬 athena-my-12 → athena-seaside：%+v", got)
	}
	// 已經搬過（新編號有兌換）就不再搬；舊的還在要列出來封存。
	unlocks = append(unlocks, Unlock{Record: "athena-seaside", Request: CarryPrefix + "athena-my-12"})
	if got := CarryPlan(catalog, unlocks); len(got) != 0 {
		t.Fatalf("搬過的不再搬：%+v", got)
	}
	if got := CarriedLeftovers(unlocks); len(got) != 1 || got[0] != "athena-my-12" {
		t.Fatalf("舊的要封存：%v", got)
	}
}
