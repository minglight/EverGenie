package accounts

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie quota`、`quota check` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，只用 sdk 的 client 打 system 服務的 usage（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"quota":       {Usage: "quota [<帳號>] [--json] | quota check [<帳號>] [--5h-max N] [--week-max N] [--reset-within H]   帳號額度（check 結束碼 0 可繼續／1 該停／2 沒資料／3 不在範圍）", Group: cmdline.GroupSystem, Run: quota},
		"quota check": {Run: quotaCheck},
	}
}

func api(e *cmdline.Env) sdk.SystemAPI { return sdk.NewSystemClient(e.Client) }

const quotaUsage = "evergenie quota [<帳號>] [--json] | quota check [<帳號>] [--5h-max N] [--week-max N] [--reset-within H]"

// quotaOpts 是 quota 兩個指令的旗標：最多一個帳號（位置參數）、一個開關、其餘要帶值。
type quotaOpts struct {
	account string
	json    bool
	vals    map[string]string
}

func parseQuota(args []string, withValue []string, withJSON bool) (quotaOpts, error) {
	o := quotaOpts{vals: map[string]string{}}
	isVal := map[string]bool{}
	for _, n := range withValue {
		isVal[n] = true
	}
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		switch {
		case withJSON && name == "json" && !hasValue:
			o.json = true
		case isVal[name] && hasValue:
			o.vals[name] = value
		case isVal[name] && i+1 < len(args):
			i++
			o.vals[name] = args[i]
		default:
			return o, cmdline.Usagef("用法：%s", quotaUsage)
		}
	}
	if len(pos) > 1 {
		return o, cmdline.Usagef("用法：%s", quotaUsage)
	}
	if len(pos) == 1 {
		o.account = pos[0]
	}
	return o, nil
}

func windowLabel(w sdk.UsageWindow) string {
	if w.Kind == sdk.UsageWindowKindWeekly {
		return "每週"
	}
	return fmt.Sprintf("%d 小時", w.Hours)
}

func resetText(t, now time.Time) string {
	if t.IsZero() {
		return "—"
	}
	t = t.Local()
	if t.YearDay() == now.Local().YearDay() && t.Year() == now.Local().Year() {
		return t.Format("15:04")
	}
	return t.Format("01/02(Mon) 15:04")
}

func usageLine(u sdk.AccountUsage, now time.Time) string {
	if u.Note != "" {
		return fmt.Sprintf("%-8s %s", u.Account, u.Note)
	}
	var parts []string
	for _, w := range u.Windows {
		s := fmt.Sprintf("%s %.0f%%，%s 重置", windowLabel(w), w.UsedRatio*100, resetText(w.ResetsAt, now))
		switch {
		case w.Expired:
			s += "（視窗已過期，下一則訊息才開新的）"
		case w.Rolled:
			s += "（資料較舊，重置時間是推算的）"
		}
		parts = append(parts, s)
	}
	age := ""
	if !u.MeasuredAt.IsZero() {
		age = fmt.Sprintf("　（資料 %d 分鐘前）", int(now.Sub(u.MeasuredAt).Minutes()))
	}
	return fmt.Sprintf("%-8s %s%s", u.Account, strings.Join(parts, "；"), age)
}

func quota(e *cmdline.Env, args []string) error {
	o, err := parseQuota(args, nil, true)
	if err != nil {
		return err
	}
	r, err := api(e).Usage(e.Ctx, &sdk.SystemUsageRequest{Account: o.account})
	if err != nil {
		return err
	}
	if o.json {
		for _, u := range r.Accounts {
			if err := e.Line(u); err != nil {
				return err
			}
		}
		return nil
	}
	now := time.Now()
	lines := make([]string, 0, len(r.Accounts))
	for _, u := range r.Accounts {
		lines = append(lines, usageLine(u, now))
	}
	_, err = fmt.Fprintln(e.Out, strings.Join(lines, "\n"))
	return err
}

// quota check：給夜班判斷「還能不能做」。結束碼 0 可以繼續；1 超過上限該停；2 沒有資料；3 不在 --reset-within 的範圍內。
func quotaCheck(e *cmdline.Env, args []string) error {
	o, err := parseQuota(args, []string{"5h-max", "week-max", "reset-within"}, false)
	if err != nil {
		return err
	}
	num := func(name string) (float64, bool, error) {
		v, ok := o.vals[name]
		if !ok {
			return 0, false, nil
		}
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f < 0 {
			return 0, false, cmdline.Usagef("--%s 要是不小於 0 的數字", name)
		}
		return f, true, nil
	}
	five, hasFive, err := num("5h-max")
	if err != nil {
		return err
	}
	week, hasWeek, err := num("week-max")
	if err != nil {
		return err
	}
	within, hasWithin, err := num("reset-within")
	if err != nil {
		return err
	}
	r, err := api(e).Usage(e.Ctx, &sdk.SystemUsageRequest{Account: o.account})
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound {
			fmt.Fprintln(e.Err, se.Message)
			return cmdline.ExitCodeError{Code: 2}
		}
		return err
	}
	now := time.Now()
	verdict := 0
	for _, u := range r.Accounts {
		if u.MeasuredAt.IsZero() || len(u.Windows) == 0 {
			fmt.Fprintf(e.Out, "%s：沒有用量資料\n", u.Account)
			verdict = maxCode(verdict, 2)
			continue
		}
		fmt.Fprintln(e.Out, usageLine(u, now))
		for _, w := range u.Windows {
			limit, has := five, hasFive
			if w.Kind == sdk.UsageWindowKindWeekly {
				limit, has = week, hasWeek
			}
			if has && w.UsedRatio*100 > limit {
				fmt.Fprintf(e.Out, "  → %s %s用量 %.0f%% 超過上限 %.0f%%，該停\n", u.Account, windowLabel(w), w.UsedRatio*100, limit)
				verdict = maxCode(verdict, 1)
			}
		}
		if hasWithin {
			soon := false
			for _, w := range u.Windows {
				if !w.ResetsAt.IsZero() && w.ResetsAt.Sub(now) <= time.Duration(within*float64(time.Hour)) {
					soon = true
				}
			}
			if !soon {
				fmt.Fprintf(e.Out, "  → %s 最近一次重置不在 %.0f 小時內\n", u.Account, within)
				verdict = maxCode(verdict, 3)
			}
		}
	}
	if verdict == 0 {
		return nil
	}
	return cmdline.ExitCodeError{Code: verdict}
}

// maxCode 決定多個帳號的結果取哪個：該停(1)最優先，其次沒資料(2)、不在範圍(3)。
func maxCode(cur, next int) int {
	rank := map[int]int{0: 0, 3: 1, 2: 2, 1: 3}
	if rank[next] > rank[cur] {
		return next
	}
	return cur
}
