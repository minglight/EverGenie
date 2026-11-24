package platform

import (
	"regexp"
	"strings"
)

// 家的編號字首（home.toml [ids]，GOAL-2 W3）：同一台電腦跑兩個家（日常、工坊）時，兩邊的單號不要長得一樣。
// 任務單預設 SEC-、Goal 預設 GOAL-；寫了就照家的設定發新編號、認單號。字首只能是大寫英文加一個 `-`。

const (
	// DefaultTaskPrefix 是任務單編號的預設字首。
	DefaultTaskPrefix = "SEC-"
	// DefaultGoalPrefix 是 Goal 編號的預設字首。
	DefaultGoalPrefix = "GOAL-"
)

// reservedPrefixes 是核心其他種類已經用掉的字首：任務單、Goal 不能跟它們一樣（否則認不出是哪一種）。
var reservedPrefixes = map[string]string{"R-": "提醒", "H-": "習慣", "M-": "訊息", "P-": "權限申請"}

var idPrefixPattern = regexp.MustCompile(`^[A-Z]+-$`)

// IDPrefixes 是 home.toml 的 [ids]。空字串＝用預設（測試直接組 Home 時也一樣）。
type IDPrefixes struct {
	Tasks string `toml:"tasks"`
	Goals string `toml:"goals"`
}

// Task 回任務單字首（沒設＝SEC-）。
func (p IDPrefixes) Task() string {
	if p.Tasks == "" {
		return DefaultTaskPrefix
	}
	return p.Tasks
}

// Goal 回 Goal 字首（沒設＝GOAL-）。
func (p IDPrefixes) Goal() string {
	if p.Goals == "" {
		return DefaultGoalPrefix
	}
	return p.Goals
}

var positiveNumber = regexp.MustCompile(`^[1-9][0-9]*$`)

// IsTask 回報 id 是不是這個家的任務單號（字首加正整數）。
func (p IDPrefixes) IsTask(id string) bool { return hasNumber(id, p.Task()) }

// IsGoal 回報 id 是不是這個家的 Goal 編號。
func (p IDPrefixes) IsGoal(id string) bool { return hasNumber(id, p.Goal()) }

// GoalPattern 是在一段文字裡找 Goal 編號用的正規式（前後不是英數字）。
func (p IDPrefixes) GoalPattern() *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(p.Goal()) + `[0-9]+\b`)
}

func hasNumber(id, prefix string) bool {
	return strings.HasPrefix(id, prefix) && positiveNumber.MatchString(strings.TrimPrefix(id, prefix))
}

// validate 檢查 [ids]：大寫英文加 `-`、兩個不一樣、不撞核心其他種類。
func (p IDPrefixes) validate() error {
	for field, v := range map[string]string{"ids.tasks": p.Tasks, "ids.goals": p.Goals} {
		if v == "" {
			continue
		}
		if !idPrefixPattern.MatchString(v) {
			return Invalid(field, "編號字首只能是大寫英文加一個 -（例：SEC-、DEV-）")
		}
		if what, taken := reservedPrefixes[v]; taken {
			return Invalid(field, "編號字首 "+v+" 已經給"+what+"用了，請換一個")
		}
	}
	if p.Task() == p.Goal() {
		return Invalid("ids.goals", "任務單跟 Goal 的編號字首不能一樣")
	}
	return nil
}

// ReadIDPrefixes 讀家的 [ids]；讀不到或格式不對就回預設（不擋啟動，格式錯誤由 LoadHome 回報）。
func ReadIDPrefixes(home string) IDPrefixes {
	if home == "" {
		return IDPrefixes{}
	}
	h, err := LoadHome(home)
	if err != nil {
		return IDPrefixes{}
	}
	return h.IDs
}
