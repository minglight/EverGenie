package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 每個排程最近一次的結果（SCH-16、D-48）：到期、略過、手動跑、封包結局都記在這裡，ListJobs 帶出去給看板。
// 排程只知道自己看得到的事實：送出了沒、封包回覆了沒、為什麼略過；秘書做得好不好不知道。
// 存 runtime/jobs/last-runs.json（D-8：高頻狀態只留最新一筆、不進 vault），serve 重開後今天的資訊還在。

// runState 是一個排程最近一次的狀態。
type runState struct {
	At     time.Time     `json:"at"`               // 最近一次到期（或手動跑）的時間
	Result sdk.JobResult `json:"result"`           // 那一次的結果
	Reason string        `json:"reason,omitempty"` // 白話原因（略過、沒跑成）
	Day    string        `json:"day,omitempty"`    // Runs 算的是哪一天（家的時區，YYYY-MM-DD）
	Runs   int           `json:"runs,omitempty"`   // Day 那天真的送出（觸發）幾次
}

// reasonMax 是原因最多留幾個字：看板一行放得下，太長的（例如她最後說的話）截掉。
const reasonMax = 80

func runsPath(home string) string { return platform.RuntimeDir(home, "jobs", "last-runs.json") }

// loadRuns 在 Start 讀回上次存的狀態；沒有檔案就是空的，讀壞了記 WARN 從空的開始（只是顯示用的資料，不擋排程）。
func (m *Module) loadRuns() {
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	m.runs = map[string]runState{}
	data, err := os.ReadFile(runsPath(m.home))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err == nil {
		err = json.Unmarshal(data, &m.runs)
	}
	if err != nil {
		m.runs = map[string]runState{}
		m.warn("讀取排程最近結果失敗，從空的開始", err)
	}
}

// saveRuns 整份寫回（暫存檔＋rename）。呼叫者拿著 runsMu。
func (m *Module) saveRuns() {
	if m.home == "" {
		return
	}
	data, err := json.MarshalIndent(m.runs, "", "  ")
	if err == nil {
		path := runsPath(m.home)
		if err = os.MkdirAll(filepath.Dir(path), 0o700); err == nil {
			err = platform.AtomicWrite(path, data, 0o600)
		}
	}
	if err != nil {
		m.warn("寫入排程最近結果失敗", err)
	}
}

func (m *Module) dayOf(t time.Time) string { return t.In(m.d.Timer.Location()).Format("2006-01-02") }

// noteRun 記一次到期（或手動跑）的結果。fired 為真表示真的送出（觸發）了，算進當天次數；略過的不算。
func (m *Module) noteRun(id string, at time.Time, result sdk.JobResult, reason string, fired bool) {
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	if m.runs == nil {
		m.runs = map[string]runState{}
	}
	st := m.runs[id]
	if fired {
		if day := m.dayOf(at); st.Day != day {
			st.Day, st.Runs = day, 0
		}
		st.Runs++
	}
	// 比記著的那一次還舊的（不該發生，例如時鐘往回調）只算次數，不蓋掉最近一次。
	if !at.Before(st.At) {
		st.At, st.Result, st.Reason = at, result, shorten(reason)
	}
	m.runs[id] = st
	m.saveRuns()
}

// noteOutcome 記封包的結局：只改「同一次觸發」的結果（冪等鍵裡的時間跟最近一次相同，比到秒）；
// 舊的那次很晚才結束，不能蓋掉之後那一次。
func (m *Module) noteOutcome(id string, at time.Time, result sdk.JobResult, reason string) {
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	st, ok := m.runs[id]
	if !ok || st.At.Unix() != at.Unix() {
		return
	}
	st.Result, st.Reason = result, shorten(reason)
	m.runs[id] = st
	m.saveRuns()
}

// pruneRuns 丟掉已經不在的排程（刪掉、封存、改名）的紀錄；reload 時順手做，檔案不會一直長。
func (m *Module) pruneRuns(live map[string]bool) {
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	changed := false
	for id := range m.runs {
		if !live[id] {
			delete(m.runs, id)
			changed = true
		}
	}
	if changed {
		m.saveRuns()
	}
}

// lastRun 把最近一次的狀態填進 ListJobs 的那一筆；今天的次數只算今天（家的時區）。
func (m *Module) lastRun(j *sdk.Job, now time.Time) {
	m.runsMu.Lock()
	st, ok := m.runs[j.ID]
	m.runsMu.Unlock()
	if !ok || st.At.IsZero() {
		return
	}
	at := st.At
	j.LastRun, j.LastResult, j.LastReason = &at, st.Result, st.Reason
	if st.Day == m.dayOf(now) {
		j.RunsToday = st.Runs
	}
}

// envelopeTime 從排程封包的冪等鍵「job:<id>#<觸發時間>」取出觸發時間。
func envelopeTime(e *sdk.Envelope) (time.Time, bool) {
	_, at, ok := strings.Cut(e.IdempotencyKey, "#")
	if !ok {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, at)
	return t, err == nil
}

// unfinishedReason 是封包沒做完時給主人看的原因：failed 是沒送進對話，interrupted 是做到一半停了（附 session 記的原因）。
func unfinishedReason(state string, e *sdk.Envelope) string {
	why := e.Reason
	if why == "" {
		why = e.LastMessage
	}
	if state == string(sdk.LifecycleFailed) {
		if why == "" {
			return "沒送進秘書的對話"
		}
		return why
	}
	if why == "" {
		return "做到一半停了"
	}
	return "做到一半停了（" + why + "）"
}

// missedReason 說這次為什麼略過（timer 的 Due 不帶原因，從項目的設定推）。
func missedReason(catchup string, window bool) string {
	switch {
	case window:
		return "醒來時已經過了時段，這次不跑"
	case catchup == "once":
		return "錯過超過 6 小時，不補跑"
	default:
		return "錯過時間（電腦關著、在睡或太忙），不補跑"
	}
}

func shorten(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > reasonMax {
		return string(r[:reasonMax]) + "…"
	}
	return s
}
