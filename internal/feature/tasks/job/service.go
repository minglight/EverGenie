package job

import (
	"context"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/timer"
	"github.com/minglight/evergenie/internal/base/vault"
	"github.com/minglight/evergenie/sdk"
)

// reload 把 vault 的 job 資料整批載入計時迴圈（vault.changed 之後、啟動時）。
// 先做會失敗的檢查（內文、秘書存不存在），再用 timer.Sync 一次換掉所有 job 項目，一筆壞設定不會讓整批換到一半。
func (m *Module) reload(ctx context.Context, now time.Time) error {
	records, err := m.d.Vault.List(ctx, "job", vault.Query{})
	if err != nil {
		return err
	}
	type candidate struct {
		r     vault.Record
		label string
		index int
	}
	problems := make([]string, len(records)) // 一筆一格，最後依 vault 的順序串起來
	var cands []candidate
	for i, r := range records {
		if r.Fields["enabled"] == false || r.Archived {
			continue
		}
		label := jobLabel(r)
		// 單一工作有問題只略過那一筆並記下來（doctor 讀 Problems），不讓一筆壞設定擋住整個排程服務。
		if strings.TrimSpace(r.Body) == "" {
			problems[i] = label + "：指令內文是空的，這個排程不會執行"
			continue
		}
		if agent := text(r, "agent"); agent != "" {
			if _, err := m.d.Vault.Get(ctx, "agent", agent); platform.CodeOf(err) == sdk.ErrorCodeNotFound {
				problems[i] = label + "：找不到秘書 " + agent + "，這個排程不會執行"
				continue
			}
		}
		cands = append(cands, candidate{r, label, i})
	}
	loc := m.d.Timer.Location()
	m.d.Timer.Sync(jobKind, func(prev map[string]*timer.Item) map[string]*timer.Item {
		next := map[string]*timer.Item{}
		for _, c := range cands {
			r, old := c.r, prev[c.r.ID]
			if r.Fields["windows"] != nil {
				it, err := windowItem(r, now, loc)
				if err != nil {
					problems[c.index] = c.label + "：時段要寫成 HH:MM-HH:MM，而且結束晚於開始"
					if old != nil {
						next[r.ID] = old
					}
					continue
				}
				carry(it, old)
				next[r.ID] = it
				continue
			}
			specs, ok := stringList(r.Fields["cron"])
			if !ok {
				problems[c.index] = c.label + "：排程必須是文字陣列"
				continue
			}
			it, err := timer.NewCronItem(r.ID, specs, loc, timer.CronOpt{Catchup: text(r, "catchup"), Agent: text(r, "agent")}, now)
			if err != nil {
				problems[c.index] = c.label + "：排程格式不正確，請使用五欄位格式"
				// 改壞的 cron 不要讓原本在跑的排程悄悄消失。
				if old != nil {
					next[r.ID] = old
				}
				continue
			}
			it.Data = jobData{action: text(r, "action"), body: r.Body}
			carry(it, old)
			next[r.ID] = it
		}
		return next
	})
	ids := make(map[string]bool, len(records))
	for _, r := range records {
		if !r.Archived {
			ids[r.ID] = true
		}
	}
	m.pruneRuns(ids)
	m.problems = []string{}
	for _, p := range problems {
		if p != "" {
			m.problems = append(m.problems, p)
		}
	}
	return nil
}

// carry 把舊項目的「上一次觸發」帶過來（重載後同一分鐘不重複觸發）。
func carry(it, old *timer.Item) {
	if old != nil {
		it.T.CarryLast(old.T)
	}
}

// windowItem 排一個時段型工作（REQ-22：主動聊天）：windows／weekend_windows 是時段，per_day 是一天最多幾次。
func windowItem(r vault.Record, now time.Time, loc *time.Location) (*timer.Item, error) {
	wd, ok1 := stringList(r.Fields["windows"])
	we, ok2 := stringList(r.Fields["weekend_windows"])
	if !ok1 || !ok2 || len(wd) == 0 {
		return nil, platform.Invalid("windows", "請填寫時段")
	}
	if err := timer.ValidWindows(append(append([]string{}, wd...), we...)); err != nil {
		return nil, err
	}
	win := &timer.Window{Weekday: wd, Weekend: we, PerDay: intField(r.Fields["per_day"], 1), IdleMin: intField(r.Fields["idle_min"], 0)}
	it := timer.NewWindowItem(r.ID, win, loc, text(r, "agent"), now)
	it.Data = jobData{action: text(r, "action"), body: r.Body}
	return it, nil
}

// jobLabel 是問題訊息裡稱呼這個工作的方式：名稱優先，沒有就用 id。
func jobLabel(r vault.Record) string {
	if name := text(r, "name"); name != "" {
		return "排程「" + name + "」（" + r.ID + "）"
	}
	return "排程 " + r.ID
}

// Problems 回傳最近一次載入時，沒辦法排上的工作，一條一句白話（cron 解析不了、指令內文是空的、秘書不存在）。
func (m *Module) Problems() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.problems...)
}

// Checks 登記 jobs 的健檢項目（app.CheckProvider）：最近一次載入有沒有工作排不上；判斷只有 Problems 這一份。
// 有問題時所有問題併成一條（用「；」分開），修法都一樣。
func (m *Module) Checks() []app.Check {
	return []app.Check{{Name: "schedule.jobs", Run: func(context.Context) app.CheckResult {
		problems := m.Problems()
		if len(problems) == 0 {
			return app.CheckResult{Status: app.StatusOK, Message: "排程都載入了"}
		}
		return app.CheckResult{Status: app.StatusError, Message: strings.Join(problems, "；"),
			Fix: "修正 vault/agents/<秘書>/jobs/ 裡的那份排程檔（cron 要五個欄位、內文不能空、秘書要存在）"}
	}}}
}
