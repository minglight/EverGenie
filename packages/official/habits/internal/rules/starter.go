package rules

// 起手禮（HAB-20）：家裡第一個習慣約好送 1 點，每個家只一次。錢包的 starter 欄位記走到哪一步，
// 送過（given）或不送（none）就不再變：習慣結束、刪掉、重約都不會再送；點數只增不減，送了就一直算在 earned 裡。
const (
	StarterWaiting = "waiting" // 還沒約過習慣：第一個習慣約好就送
	StarterGiven   = "given"   // 送過了
	StarterNone    = "none"    // 起手禮上線前就約過習慣的家：不補送
	StarterPoints  = 1
)

// StarterBook 是決定起手禮要的帳。
type StarterBook struct {
	Old       string // 錢包舊帳的 starter；空＝沒有這個欄位（新家第一次算，或起手禮上線前的錢包）
	OldHabit  string // 錢包舊帳的 starter_habit
	OldWallet bool   // 有沒有舊錢包
	HadHabits bool   // 舊錢包沒有 starter 時才看：以前有沒有過習慣（含封存的習慣、點數帳、打卡）
	First     string // 現在編號最小的習慣（沒封存、設定合格；結束的也算）；空＝沒有
}

// Starter 回這次之後的 starter 與 starter_habit；gift＝這次剛送（要通知一次）。
// 沒有 starter 欄位時：沒有舊錢包＝新家，照「還沒送」算；有舊錢包而且以前有過習慣＝升級前就在用的家，不補送。
func Starter(b StarterBook) (state, habit string, gift bool) {
	switch b.Old {
	case StarterGiven, StarterNone:
		return b.Old, b.OldHabit, false
	case StarterWaiting:
	default:
		if b.OldWallet && b.HadHabits {
			return StarterNone, "", false
		}
	}
	if b.First == "" {
		return StarterWaiting, "", false
	}
	return StarterGiven, b.First, true
}

// WithStarter 把起手禮算進錢包：送過了 earned 多 1 點，points 照 max(earned − spent, 0) 重算。
func (w Wallet) WithStarter(state, habit string) Wallet {
	w.Starter, w.StarterHabit = state, habit
	if state == StarterGiven {
		w.Earned += StarterPoints
		w.Points = max(w.Earned-w.Spent, 0)
	}
	return w
}
