package rules

import "testing"

// TestStarterOncePerHome：起手禮（HAB-20）每個家只送一次——新家第一個習慣約好送、送過不再送；
// 升級前就有過習慣的家不補送，升級前沒約過習慣的家照新家等第一個；送了算進 earned、花掉也不收回。
func TestStarterOncePerHome(t *testing.T) {
	for _, c := range []struct {
		name       string
		in         StarterBook
		state, hab string
		gift       bool
	}{
		{"新家、還沒約", StarterBook{}, StarterWaiting, "", false},
		{"新家、第一次算就有習慣", StarterBook{First: "H-1"}, StarterGiven, "H-1", true},
		{"等著、約好了", StarterBook{Old: StarterWaiting, OldWallet: true, First: "H-2"}, StarterGiven, "H-2", true},
		{"等著、還沒約", StarterBook{Old: StarterWaiting, OldWallet: true}, StarterWaiting, "", false},
		{"送過、習慣刪光後重約", StarterBook{Old: StarterGiven, OldHabit: "H-1", OldWallet: true, First: "H-5"}, StarterGiven, "H-1", false},
		{"送過、習慣都刪了", StarterBook{Old: StarterGiven, OldHabit: "H-1", OldWallet: true}, StarterGiven, "H-1", false},
		{"不送的家、約了新的", StarterBook{Old: StarterNone, OldWallet: true, First: "H-9"}, StarterNone, "", false},
		{"升級前有過習慣", StarterBook{OldWallet: true, HadHabits: true, First: "H-3"}, StarterNone, "", false},
		{"升級前有過、現在都封存了", StarterBook{OldWallet: true, HadHabits: true}, StarterNone, "", false},
		{"升級前沒約過", StarterBook{OldWallet: true}, StarterWaiting, "", false},
	} {
		state, hab, gift := Starter(c.in)
		if state != c.state || hab != c.hab || gift != c.gift {
			t.Errorf("%s：%s %s %v，要 %s %s %v", c.name, state, hab, gift, c.state, c.hab, c.gift)
		}
	}

	w := Bank(nil, map[string]int{"H-1": 2}, 3, TaskBook{HasLegacy: true}).WithStarter(StarterGiven, "H-1")
	if w.Earned != 3 || w.Points != 0 || w.Starter != StarterGiven || w.StarterHabit != "H-1" {
		t.Fatalf("送了算進 earned，花掉照扣、不會是負的：%+v", w)
	}
	if w := Bank(nil, nil, 0, TaskBook{HasLegacy: true}).WithStarter(StarterNone, ""); w.Earned != 0 || w.Points != 0 || w.Starter != StarterNone {
		t.Fatalf("不送就不加：%+v", w)
	}
}
