package timer

import (
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/robfig/cron/v3"
)

// Cron 是一組五欄位 cron（一個工作可以有多個，取最早的下一次）。
type Cron struct{ specs []cron.Schedule }

// ParseCron 把五欄位 cron 文字解析成排程（時區用 loc）。排程服務、遷移報告、驗證共用這一份，算出來的時間一定一致。
func ParseCron(specs []string, loc *time.Location) (*Cron, error) {
	if len(specs) == 0 {
		return nil, platform.Invalid("cron", "請填寫排程")
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	out := make([]cron.Schedule, 0, len(specs))
	for _, spec := range specs {
		parsed, err := parser.Parse(spec)
		if err != nil {
			return nil, platform.Invalid("cron", "排程格式不正確，請使用五欄位格式")
		}
		if s, ok := parsed.(*cron.SpecSchedule); ok {
			s.Location = loc
		}
		out = append(out, parsed)
	}
	return &Cron{specs: out}, nil
}

// Next 回 after 之後最早的一次觸發時間；排不出來回零值。
func (c *Cron) Next(after time.Time) time.Time {
	var next time.Time
	for _, spec := range c.specs {
		n := spec.Next(after)
		if !n.IsZero() && (next.IsZero() || n.Before(next)) {
			next = n
		}
	}
	return next
}

// NextRuns 算 specs 在 after 之後的下 n 次觸發時間（多個 cron 時合併、由早到晚）。
// 遷移工具用它在報告列出新排程的下次觸發時間（REQ-19），也用它對照舊系統的 cron 語意（SCH-7）。
func NextRuns(specs []string, loc *time.Location, after time.Time, n int) ([]time.Time, error) {
	c, err := ParseCron(specs, loc)
	if err != nil {
		return nil, err
	}
	var out []time.Time
	for at := after; len(out) < n; {
		next := c.Next(at)
		if next.IsZero() {
			break
		}
		out = append(out, next)
		at = next
	}
	return out, nil
}
