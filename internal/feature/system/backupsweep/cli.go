package backupsweep

import (
	"fmt"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

const cliUsage = "evergenie backup sweep [--run] [--json]"

// Commands 登記 `evergenie backup sweep`（套件層級；指令行程只是 client）。頂層 backup 的用法說明寫在 settings 的 backup list 那一筆。
func Commands() map[string]app.Command {
	return map[string]app.Command{"backup sweep": {Run: cliSweep}}
}

// backup sweep：看上一次的備份掃描報告；--run 現在掃一次（主人跑的會順便照保留規則清 runtime/backups/）。
func cliSweep(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"run", "json"}, cliUsage)
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：%s", cliUsage)
	}
	r, err := sdk.NewSystemClient(e.Client).BackupSweep(e.Ctx, &sdk.SystemBackupSweepRequest{Run: o.Flags["run"], By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if o.Flags["json"] {
		if r.Report == nil {
			return e.Line(map[string]any{})
		}
		return e.Line(r.Report)
	}
	if r.Report == nil {
		_, err := fmt.Fprintln(e.Out, "還沒掃過。現在掃一次：evergenie backup sweep --run")
		return err
	}
	if r.Running {
		if _, err := fmt.Fprintln(e.Out, "另一次掃描還在跑，下面是上一次的結果。"); err != nil {
			return err
		}
	}
	_, err = fmt.Fprint(e.Out, Markdown(r.Report, nil))
	return err
}
