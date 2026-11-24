package job

import (
	"embed"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie jobs …`、`evergenie night …` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module、不啟動任何東西，只用 sdk 的 client 打 API（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"night":       {Usage: "night list|add|done|block|rm       夜班清單（每週額度重置前最後兩晚 system 照做；add \"<做什麼>\"、done <n> --result、block <n> --reason、rm <n>；evergenie night --help 看細節）", Group: cmdline.GroupSchedule, Run: nightHelp},
		"night list":  {Run: nightList},
		"night add":   {Run: nightAdd},
		"night done":  {Run: nightDoneCmd},
		"night block": {Run: nightBlock},
		"night rm":    {Run: nightRm},
		"jobs list":   {Usage: "jobs list|add|set|rm|run           排程工作（add <職位>/<id> --cron …（可重複）或 --template ai-topics|reflect|chat；set 改 --enabled、--cron）", Group: cmdline.GroupSchedule, Run: jobsList},
		"jobs add":    {Run: jobsAdd},
		"jobs set":    {Run: jobsSet},
		"jobs rm":     {Run: jobsRm},
		"jobs run":    {Run: jobsRun},
	}
}

func api(e *cmdline.Env) sdk.SchedulerAPI  { return sdk.NewSchedulerClient(e.Client) }
func vaultAPI(e *cmdline.Env) sdk.VaultAPI { return sdk.NewVaultClient(e.Client) }
func actor(e *cmdline.Env) *sdk.Actor      { return &sdk.Actor{Name: e.By} }

// jobs：排程工作的 CLI（Q1，等價舊的 `agentctl job …`、`run`）。資料是 vault 的 `job`（schemas/job.md），
// id 是 `<職位>/<id>`（系統級沒有 `/`）。讀寫都經排程服務（`scheduler.*`，API-1）；驗證（cron、時段、秘書、內文）在排程模組。

//go:embed jobtemplates/*.md
var jobTemplateFiles embed.FS

// jobTemplate 是能力模組做成的排程範本（舊系統的 extras：ai-topics、reflect、chat）。內文用 `{{slot …}}` 標出 addendum 可以換掉的地方。
type jobTemplate struct {
	name string
	job  sdk.Job
}

var jobTemplates = map[string]jobTemplate{
	"ai-topics":    {"AI 新知", sdk.Job{Cron: []string{"0 15 * * 1,3,5,0"}, Catchup: "once"}},                                   // 週一三五日 15:00（舊 cron 的星期 7 改成 0）
	"reflect":      {"每日回顧", sdk.Job{Cron: []string{"40 20 * * *"}, Catchup: "skip", Silent: true}},                           // 每天 20:40，不推播
	"usage-warmup": {"每天早上額度暖機", sdk.Job{Cron: []string{"0 6 * * *"}, Catchup: "skip", Silent: true, Action: "usage_warmup"}}, // 系統級（id 不用寫職位）：每天 6:00
	"limit-watch":  {"額度開通通知", sdk.Job{Cron: []string{"*/5 * * * *"}, Catchup: "skip", Silent: true, Action: "limit_watch"}},  // 系統級：每 5 分鐘看一次
	"backup-sweep": {"每週備份掃描", sdk.Job{Cron: []string{"30 4 * * 0"}, Catchup: "once", Silent: true, Action: "backup_sweep"}},  // 系統級：每週日 04:30（DEV-163；遷移會自動放一份）
	"chat": {"主動聊天", sdk.Job{Windows: []string{"12:15-13:15", "15:30-17:00"}, WeekendWindows: []string{"10:00-11:30", "15:00-17:00"},
		PerDay: 1, IdleMin: 15, Catchup: "skip"}}, // 每天從時段裡挑一段，主動開個話題
}

const (
	jobsAddUsage = "evergenie jobs add <職位>/<id> (--cron \"五欄位\"…|--template ai-topics|reflect|chat|usage-warmup|limit-watch|backup-sweep) [--file 指令檔] [--name 名稱] [--catchup once|skip] [--action none|rotate] [--silent] [--disabled]"
	jobsSetUsage = "evergenie jobs set <id> [--enabled true|false] [--cron \"五欄位\"]… [--name 名稱] [--body 內文|-]"
)

// takeRepeated 把可以重複的 `--name 值` 全部取出來，回傳值與剩下的參數。
func takeRepeated(args []string, name string) ([]string, []string, error) {
	var vals, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--"+name:
			if i+1 >= len(args) {
				return nil, nil, cmdline.Usagef("--%s 後面要有值", name)
			}
			i++
			vals = append(vals, args[i])
		case strings.HasPrefix(a, "--"+name+"="):
			vals = append(vals, strings.TrimPrefix(a, "--"+name+"="))
		default:
			rest = append(rest, a)
		}
	}
	return vals, rest, nil
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// jobs list [--agent <職位>] [--all] [--json]
func jobsList(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"agent"}, []string{"all", "json"}, "evergenie jobs list [--agent <職位>] [--all] [--json]")
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie jobs list [--agent <職位>] [--all] [--json]")
	}
	r, err := api(e).ListJobs(e.Ctx, &sdk.SchedulerListJobsRequest{Agent: o.Vals["agent"], IncludeArchived: o.Flags["all"]})
	if err != nil {
		return err
	}
	if o.Flags["json"] {
		for _, j := range r.Jobs {
			if err := e.Line(j); err != nil {
				return err
			}
		}
		return nil
	}
	var b strings.Builder
	if len(r.Jobs) == 0 {
		b.WriteString("沒有排程工作\n")
	}
	for _, j := range r.Jobs {
		state := "開"
		if !j.Enabled {
			state = "關"
		}
		if j.Archived {
			state = "已移除"
		}
		fmt.Fprintf(&b, "%-24s %-4s %s　%s", j.ID, state, j.Name, when(j))

		b.WriteString("\n")
	}
	_, err = e.Out.Write([]byte(b.String()))
	return err
}

// when 是排程時間的白話：cron 原樣列出，時段型列出時段。
func when(j sdk.Job) string {
	if len(j.Windows) > 0 {
		s := "時段 " + strings.Join(j.Windows, "、")
		if len(j.WeekendWindows) > 0 {
			s += "（週末 " + strings.Join(j.WeekendWindows, "、") + "）"
		}
		return s
	}
	return strings.Join(j.Cron, "；")
}

// jobs add <職位>/<id> …：用法錯誤（缺時間、缺內文、範本不存在、id 格式）在這裡擋；cron、時段、秘書存不存在由排程模組驗。
func jobsAdd(e *cmdline.Env, args []string) error {
	crons, args, err := takeRepeated(args, "cron")
	if err != nil {
		return err
	}
	o, err := cmdline.ParseOpts(args, []string{"template", "file", "name", "catchup", "action"}, []string{"silent", "disabled"}, jobsAddUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：%s", jobsAddUsage)
	}
	id := o.Pos[0]
	agent, local, scoped := strings.Cut(id, "/")
	if !scoped {
		local = agent
	}
	if local == "" || strings.Contains(local, "/") || (scoped && agent == "") {
		return cmdline.Usagef("排程 id 要寫成 <職位>/<id>，例如 code/nightly")
	}
	var j sdk.Job
	if t := o.Vals["template"]; t != "" {
		tpl, ok := jobTemplates[t]
		if !ok {
			return cmdline.Usagef("沒有這個範本：%s（可用：ai-topics、reflect、chat、usage-warmup、limit-watch、backup-sweep）", t)
		}
		data, err := jobTemplateFiles.ReadFile("jobtemplates/" + t + ".md")
		if err != nil {
			return err
		}
		j = tpl.job
		j.Name, j.Body = tpl.name, string(data)
	}
	j.ID, j.Enabled, j.Source = id, !o.Flags["disabled"], "user"
	if len(crons) > 0 {
		j.Cron, j.Windows, j.WeekendWindows, j.PerDay, j.IdleMin = crons, nil, nil, 0, 0
	}
	if len(j.Cron) == 0 && len(j.Windows) == 0 {
		return cmdline.Usagef("請用 --cron 給排程時間（可以重複），或用 --template 選範本")
	}
	if f := o.Vals["file"]; f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return cmdline.Usagef("讀不到指令檔 %s", f)
		}
		j.Body = string(data)
	}
	if strings.TrimSpace(j.Body) == "" {
		return cmdline.Usagef("請用 --file 給指令內文（要秘書做什麼），或用 --template 選範本")
	}
	if n := o.Vals["name"]; n != "" {
		j.Name = n
	}
	if v := o.Vals["catchup"]; v != "" {
		j.Catchup = v
	}
	if v := o.Vals["action"]; v != "" {
		j.Action = v
	}

	if o.Flags["silent"] {
		j.Silent = true
	}
	r, err := api(e).SaveJob(e.Ctx, &sdk.SchedulerSaveJobRequest{Job: &j, By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已新增排程 %s「%s」：%s\n", r.Job.ID, r.Job.Name, when(*r.Job))
	return err
}

// jobs set <id> [--enabled true|false] [--cron …]… [--name 名稱] [--body 內文|-]（`--body -` 從標準輸入讀）
func jobsSet(e *cmdline.Env, args []string) error {
	crons, args, err := takeRepeated(args, "cron")
	if err != nil {
		return err
	}
	o, err := cmdline.ParseOpts(args, []string{"enabled", "name", "body"}, nil, jobsSetUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：%s", jobsSetUsage)
	}
	req := &sdk.SchedulerSetJobRequest{ID: o.Pos[0], Cron: crons, By: &sdk.Actor{Name: e.By}}
	if v, ok := o.Vals["enabled"]; ok {
		var on bool
		switch strings.ToLower(v) {
		case "true", "on", "yes":
			on = true
		case "false", "off", "no":
		default:
			return cmdline.Usagef("--enabled 只能是 true 或 false")
		}
		req.Enabled = &on
	}

	if v, ok := o.Vals["name"]; ok {
		req.Name = &v
	}
	if v, ok := o.Vals["body"]; ok {
		if v == "-" {
			data, err := io.ReadAll(e.In)
			if err != nil {
				return cmdline.Usagef("讀不到標準輸入")
			}
			v = string(data)
		}
		req.Body = &v
	}
	if req.Enabled == nil && len(req.Cron) == 0 && req.Name == nil && req.Body == nil {
		return cmdline.Usagef("用法：%s（至少改一項）", jobsSetUsage)
	}
	r, err := api(e).SetJob(e.Ctx, req)
	if err != nil {
		return err
	}
	state := "開"
	if !r.Job.Enabled {
		state = "關"
	}
	_, err = fmt.Fprintf(e.Out, "已更新排程 %s（%s）：%s\n", r.Job.ID, state, when(*r.Job))
	return err
}

// jobs rm <id>：自己加的（source: user）封存；遷移或套件帶來的只關掉，之後升級也不會被重新打開（規則在排程模組）。
func jobsRm(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		return cmdline.Usagef("用法：evergenie jobs rm <id>")
	}
	r, err := api(e).RemoveJob(e.Ctx, &sdk.SchedulerRemoveJobRequest{ID: args[0], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if !r.Archived {
		_, err = fmt.Fprintf(e.Out, "%s 不是你自己加的排程：已關掉（enabled: false）。要再開：evergenie jobs set %s --enabled true\n", r.Job.ID, r.Job.ID)
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已移除排程 %s（封存在 vault 的 archive，沒有刪除）\n", r.Job.ID)
	return err
}

// jobs run <id>：立刻跑一次，跟排程時間到了一樣經派送器送給秘書。
func jobsRun(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		return cmdline.Usagef("用法：evergenie jobs run <id>")
	}
	r, err := api(e).RunJob(e.Ctx, &sdk.SchedulerRunJobRequest{ID: args[0], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已排進 %s 的派送佇列：%s「%s」\n", r.Job.Agent, r.Job.ID, r.Job.Name)
	return err
}
