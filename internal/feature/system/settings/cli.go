package settings

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/feature/system/doctor"
	"github.com/minglight/evergenie/sdk"
)

// Commands 登記 settings 的指令（套件層級函式，指令行程只是 client，不建 Module）：
// config show|get|set、backup list|prune、preflight，以及看板連多台的 web sites|allow|owner（cli_web.go）。
// preflight 要動這台電腦（brew、工具有沒有裝），業務模組不碰系統，實作由 registry 傳進來（用 host）；
// 傳 nil（例如單元測試）時 preflight 回「這個環境不支援」。
// `config set` 改完跑的 doctor 結果用同塊 doctor 的 Print 印。
func Commands(preflight cmdline.HostFunc) map[string]app.Command {
	cmds := map[string]app.Command{
		"config show":  {Usage: "config show|get <名稱>|set <名稱> <值>   讀、改 home.toml（改之前備份、改完跑 doctor）", Group: cmdline.GroupSystem, Run: configShow},
		"config get":   {Run: configGet},
		"config set":   {Run: configSet},
		"backup list":  {Usage: "backup create|status|show <檔>|restore <檔> --yes   整個家的備份與還原（一個 .egbackup 檔；主人手動做，只放在這台電腦；每次更新前另外自動留最新一份）\nbackup list|prune [--keep N] [--days N] [--apply]   改設定前留的小備份整理（prune 預設只預演，每組最新 N 個一定留，其餘只留 N 天內）\nbackup sweep [--run] [--json]   每週的備份掃描報告（工作資料夾、這個家、AI 帳號資料夾裡名字像備份的東西；--run 現在掃一次，只出報告不刪）", Group: cmdline.GroupSystem, Run: backupList},
		"backup prune": {Run: backupPrune},
		"preflight": cmdline.HostCommand("preflight", preflight, func(e *cmdline.Env, r *cmdline.HostRequest) {
			if e.NoSocket {
				return
			}
			// serve 在跑：問各帳號的引擎（終端機工具、程式、登入），一起檢查。沒回應就只做這台電腦的部分。
			r.Engines = func() ([]sdk.PreflightCheck, bool) {
				res, err := sdk.NewSystemClient(e.Client).Preflight(e.Ctx, &sdk.SystemPreflightRequest{})
				if err != nil {
					return nil, false
				}
				return res.Checks, true
			}
		}).With("preflight [--need tailscale] [--fix]   這台電腦夠不夠格（系統、Homebrew、必備工具、帳號程式與登入）；--fix 只幫你 brew install 缺的", cmdline.GroupSystem),
	}
	for name, c := range webCommands() {
		cmds[name] = c
	}
	return cmds
}

func actor(e *cmdline.Env) *sdk.Actor { return &sdk.Actor{Name: e.By} }

func system(e *cmdline.Env) sdk.SystemAPI { return sdk.NewSystemClient(e.Client) }

// ---- config ----

// config show|get <key>|set <key> <值>
func configShow(e *cmdline.Env, args []string) error {
	if len(args) != 0 {
		return cmdline.Usagef("用法：evergenie config show")
	}
	r, err := system(e).GetConfig(e.Ctx, &sdk.SystemGetConfigRequest{})
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(e.Out, r.Text)
	return err
}

func configGet(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("用法：evergenie config get <設定名稱>（例：user.timezone）")
	}
	r, err := system(e).GetConfig(e.Ctx, &sdk.SystemGetConfigRequest{Key: args[0]})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(e.Out, r.Value)
	return err
}

func configSet(e *cmdline.Env, args []string) error {
	if len(args) != 2 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("用法：evergenie config set <設定名稱> <值>（值寫 TOML：\"文字\"、123、true、[…]；不合法的當成文字）")
	}
	r, err := system(e).SetConfig(e.Ctx, &sdk.SystemSetConfigRequest{Key: args[0], Value: args[1], By: actor(e)})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "已設定 %s = %s（舊檔備份在 %s；要生效的部分請 evergenie sessions rotate --all 或重啟 serve）\n", r.Key, r.Value, r.Backup)
	// 改完經 API 跑一次 doctor，馬上看到有沒有弄壞什麼。
	res, err := system(e).Doctor(e.Ctx, &sdk.SystemDoctorRequest{})
	if err != nil {
		return err
	}
	if err := doctor.Print(e, res.Checks); err != nil {
		return err
	}
	for _, c := range res.Checks {
		if c.Severity == sdk.DoctorSeverityError {
			// 設定已經寫好了；doctor 的問題不一定是這次改的，不讓指令看起來像失敗。
			_, err := fmt.Fprintln(e.Out, "注意：設定已寫入，但上面有要處理的項目。如果是這次改壞的，舊檔在上面那個備份路徑。")
			return err
		}
	}
	return nil
}

// ---- backup ----

const backupUsage = "evergenie backup list [--json] | prune [--keep N] [--days N] [--apply] [--json]"

func printBackups(e *cmdline.Env, r *sdk.SystemBackupsResponse, asJSON bool) error {
	if asJSON {
		return e.Line(r)
	}
	var b strings.Builder
	if len(r.Files) == 0 {
		b.WriteString("沒有備份\n")
	}
	for _, f := range r.Files {
		mark := " "
		if f.Prune {
			mark = "刪"
		}
		fmt.Fprintf(&b, "%s %-8s %8s  %s  %s\n", mark, f.Group, humanSize(f.Size), f.ModTime.Local().Format("2006-01-02 15:04"), f.Path)
	}
	fmt.Fprintf(&b, "共 %d 個，%s\n", len(r.Files), humanSize(r.Total))
	_, err := e.Out.Write([]byte(b.String()))
	return err
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func backupList(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"json"}, backupUsage)
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：%s", backupUsage)
	}
	r, err := system(e).Backups(e.Ctx, &sdk.SystemBackupsRequest{})
	if err != nil {
		return err
	}
	return printBackups(e, r, o.Flags["json"])
}

// backup prune：預設只預演（標出「刪」的）；--apply 才真的刪。每一組最新的 --keep 個一定留，其餘只留 --days 天內的。
func backupPrune(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"keep", "days"}, []string{"apply", "json"}, backupUsage)
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：%s", backupUsage)
	}
	req := &sdk.SystemBackupsRequest{Prune: true, Apply: o.Flags["apply"], By: actor(e)}
	for name, dst := range map[string]*int32{"keep": &req.Keep, "days": &req.Days} {
		if v, ok := o.Vals[name]; ok {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return cmdline.Usagef("--%s 要是正整數", name)
			}
			*dst = int32(n)
		}
	}
	r, err := system(e).Backups(e.Ctx, req)
	if err != nil {
		return err
	}
	if err := printBackups(e, r, o.Flags["json"]); err != nil || o.Flags["json"] {
		return err
	}
	switch {
	case r.Applied:
		_, err = fmt.Fprintf(e.Out, "已刪 %d 個，騰出 %s\n", r.Pruned, humanSize(r.Freed))
	case r.Pruned > 0:
		_, err = fmt.Fprintf(e.Out, "預演：會刪 %d 個（標「刪」的），騰出 %s。要真的刪：加 --apply\n", r.Pruned, humanSize(r.Freed))
	default:
		_, err = fmt.Fprintln(e.Out, "沒有需要刪的")
	}
	return err
}
