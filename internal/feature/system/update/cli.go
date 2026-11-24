package update

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// `evergenie upgrade`、`migrate-home`（P8 第二輪 S2、REQ-280）：升級的指令在這裡，升級的做法在 upgrade.go。
// 這兩個是「本機指令」（不用 serve 開著也能跑，不經 API）；失敗時訊息印在 stderr，結束碼 1（升級退不回去是 3）、用法錯誤 2。

const upgradeUsage = "evergenie upgrade --check [--json] | --apply [--to <tag>] [--detach] | --rollback [--rescue] | --accept | --report [--from <版本>] [--only-problems] | --deploy begin|undo|done [--from <版本>] [--from-commit <commit>] | --status [--json] | --decide now|tonight|skip [--version <版本>] [--home <家>]"

// noCoreMessage 是找不到程式（原始碼版的 git repo 或下載版的安裝資料夾，FindInstall）時的白話說明。
const noCoreMessage = "這台不是用安裝程式裝的，沒辦法自動升級（找不到 EverGenie 的程式資料夾；可以在 home.toml 的 [update] core 指定）"

const migrateUsage = "evergenie migrate-home [--apply | --preview] [--home <家>]"

// Commands 登記 `upgrade`、`migrate-home` 兩個本機指令。
func Commands() map[string]app.Command {
	return map[string]app.Command{"upgrade": {Usage: "upgrade --check|--apply|--rollback|--accept|--report|--status|--decide now|tonight|skip   檢查與升級 EverGenie（--apply [--to <tag>] [--detach]；壞版自動換回並跳過；--rollback 退到上一版，資料一起回到升級前（上次升級換不回去時，改成照 rescue.json 再試一次換回；加 --rescue 只做這個）；--accept 記下主人確認升級沒問題、之後不自動退回（不用問主人，更新前的備份只留最新一份））", Group: cmdline.GroupSystem, Run: upgradeCommand}, "migrate-home": {Usage: "migrate-home [--apply|--preview]  家的資料遷移（升級時自動做；預設只列出要做什麼；--preview 在複本上試跑）", Group: cmdline.GroupSystem, Run: migrateHomeCommand}}
}

// say 把錯誤印成一行白話（有 platform 的白話訊息就用它），回「已經印過了」的錯誤。
func say(e *cmdline.Env, err error) error {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		fmt.Fprintln(e.Err, pe.Message)
	} else {
		fmt.Fprintln(e.Err, err.Error())
	}
	return cmdline.ErrReported
}

// resolveHome：--home 優先，其次 $EVERGENIE_HOME；都沒有就說白話。
func resolveHome(vals map[string]string) (string, error) {
	home := vals["home"]
	if home == "" {
		home = strings.TrimSpace(sdk.Getenv("EVERGENIE_HOME"))
	}
	if home == "" {
		return "", platform.Invalid("home", "不知道家在哪裡：用 --home <家的資料夾>，或設定環境變數 EVERGENIE_HOME")
	}
	abs, err := filepath.Abs(home)
	if err != nil {
		return "", platform.Invalid("home", "家的路徑不對")
	}
	return abs, nil
}

func upgradeCommand(e *cmdline.Env, args []string) error {
	vals, on, rest, ok := cmdline.LocalFlags(args, []string{"home", "to", "decide", "version", "from", "deploy", "from-commit"}, []string{"check", "apply", "rollback", "rescue", "accept", "report", "only-problems", "status", "detach", "json"})
	n := 0
	for _, k := range []string{"check", "apply", "rollback", "accept", "report", "status"} {
		if on[k] {
			n++
		}
	}
	if vals["decide"] != "" {
		n++
	}
	if vals["deploy"] != "" {
		n++
	}
	if !ok || len(rest) > 0 || n != 1 || (on["rescue"] && !on["rollback"]) {
		return cmdline.Usagef("用法：%s", upgradeUsage)
	}
	home, err := resolveHome(vals)
	if err != nil {
		return say(e, err)
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return say(e, err)
	}
	exe, _ := os.Executable()
	core := FindInstall(h, exe)

	switch {
	case on["status"]:
		return upgradeStatus(e, h, core != "", on["json"])
	case vals["decide"] != "":
		return upgradeDecide(e, h, core, vals["decide"], vals["version"], exe)
	case on["report"]:
		// 不是走 `upgrade --apply` 換版的家（開發用的部署工具）也能補一份升級報告交給管電腦的秘書（DEV-154）。
		// --only-problems：主人自己的套件都沒有載入問題就不傳（天天部署的家不用每次都吵秘書）。
		from := vals["from"]
		if from == "" {
			from = "上一版"
		}
		// 這一版整理過資料、還在等主人確認：報告要帶上，而且一定要傳。
		base := reportBase(home, from, platform.Version())
		r := surveyHome(e.Ctx, home, exe, base)
		if on["only-problems"] && len(r.Packages) == 0 && len(r.Migrations) == 0 {
			fmt.Fprintln(e.Out, "主人自己的套件都正常、這次也沒整理資料，沒有要交給秘書的")
			return nil
		}
		if !deliverUpgradeReport(e.Ctx, home, r) {
			fmt.Fprintln(e.Err, "升級報告寫好了（"+filepath.Join(UpgradeDir(home), UpgradeReportFile)+"），但傳不到秘書手上（服務沒開，或這個家沒有秘書）")
			return cmdline.ErrReported
		}
		fmt.Fprintln(e.Out, "升級報告已交給 "+ReportAgent(home))
		return nil
	}
	if vals["deploy"] != "" {
		// 部署工具（scripts/deploy.sh）換版時的資料保護（deploy.go）；找不到程式資料夾也能做（只是紀錄裡沒有 commit）。
		u := newUpgrader(h, core, e.Out)
		switch vals["deploy"] {
		case "begin":
			path, err := u.DeployBegin(e.Ctx, vals["from"], vals["from-commit"])
			if err != nil {
				return upgradeExit(e, err)
			}
			fmt.Fprintln(e.Out, "已備份："+path)
		case "undo":
			restored, err := u.DeployUndo()
			if err != nil {
				return upgradeExit(e, err)
			}
			if restored {
				fmt.Fprintln(e.Out, "這次整理過資料：已還原到部署前")
			} else {
				fmt.Fprintln(e.Out, "資料沒有被整理過，不用還原")
			}
		case "done":
			rec, err := u.DeployDone()
			if err != nil {
				return upgradeExit(e, err)
			}
			if rec == nil {
				fmt.Fprintln(e.Out, "這次沒有整理資料；更新前的備份留著最新一份")
			} else {
				fmt.Fprintf(e.Out, "這次整理了資料（%s）：不用請主人確認，更新前的備份留著最新一份；有問題可以 evergenie upgrade --rollback 連資料一起退回 %s\n", strings.Join(rec.Migrations, "、"), rec.From)
			}
		default:
			return cmdline.Usagef("用法：%s", upgradeUsage)
		}
		return nil
	}
	if core == "" {
		fmt.Fprintln(e.Err, noCoreMessage)
		return cmdline.ErrReported
	}
	u := newUpgrader(h, core, e.Out)
	switch {
	case on["check"]:
		chk, err := u.Check(e.Ctx)
		if err != nil {
			return say(e, err)
		}
		if on["json"] {
			return e.Line(chk)
		}
		printCheck(e.Out, chk)
		return nil
	case on["apply"]:
		if on["detach"] {
			return detachUpgrade(e, home, exe, vals["to"])
		}
		return upgradeExit(e, u.Apply(e.Ctx, vals["to"]))
	case on["accept"]:
		last, err := u.Accept()
		if err != nil {
			return upgradeExit(e, err)
		}
		if u.Download {
			fmt.Fprintf(e.Out, "好，%s → %s 這次升級確認沒問題；舊版的程式收掉了，之後不能再退回 %s。升級前的備份留著（只留最新一份，下次更新成功才換掉）。\n", last.From, last.To, last.From)
			return nil
		}
		fmt.Fprintf(e.Out, "好，%s → %s 這次升級確認沒問題，之後不自動退回 %s。升級前的備份留著（只留最新一份，下次更新成功才換掉）。\n", last.From, last.To, last.From)
		return nil
	case on["rescue"]: // 看板首頁的「退回上一版」：只照 rescue.json 換回，沒有就不動
		return upgradeExit(e, u.Rescue(e.Ctx))
	default: // rollback
		return upgradeExit(e, u.Rollback(e.Ctx))
	}
}

func upgradeExit(e *cmdline.Env, err error) error {
	if err == nil {
		return nil
	}
	fmt.Fprintln(e.Err, err.Error())
	return cmdline.ExitCodeError{Code: UpgradeExitCode(err)}
}

func printCheck(w io.Writer, c *UpgradeCheck) {
	fmt.Fprintf(w, "現在的版本：%s（更新頻道：%s）\n", c.Current, channelName(c.Channel))
	if c.Target == "" {
		fmt.Fprintln(w, "沒有新版："+c.Detail)
		return
	}
	fmt.Fprintf(w, "有新版：%s\n", c.Target)
	if c.Notes != "" {
		fmt.Fprintln(w, c.Notes)
	}
	if c.Detail != "" {
		fmt.Fprintln(w, "（"+c.Detail+"）")
	}
	fmt.Fprintln(w, "要更新：evergenie upgrade --apply")
}

// detachUpgrade：`upgrade --apply --detach`，立刻回 0（看板的按鈕用它）。
func detachUpgrade(e *cmdline.Env, home, exe, to string) error {
	logPath, err := startDetached(home, exe, to)
	if err != nil {
		return say(e, err)
	}
	fmt.Fprintf(e.Out, "升級已經在背景開始（日誌：%s）\n", logPath)
	return nil
}

func upgradeStatus(e *cmdline.Env, h *platform.Home, coreFound, asJSON bool) error {
	st, _ := ReadUpgradeState(h.Dir)
	chk, _ := ReadUpgradeCheck(h.Dir)
	dec, _ := ReadUpgradeDecision(h.Dir)
	skipped := ReadUpgradeSkipped(h.Dir)
	waiting := PendingAcceptance(h.Dir)
	if asJSON {
		return e.Line(map[string]any{"current": platform.Version(), "channel": h.UpdateChannel(), "core_found": coreFound, "state": st, "check": chk, "skipped": skipped, "decision": dec, "awaiting_accept": waiting})
	}
	fmt.Fprintf(e.Out, "現在的版本：%s（更新頻道：%s）\n", platform.Version(), channelName(h.UpdateChannel()))
	if !coreFound {
		fmt.Fprintln(e.Out, noCoreMessage)
	}
	if st != nil {
		line := fmt.Sprintf("升級進度：%s（%s → %s）", st.Phase, st.From, st.To)
		if st.Reason != "" {
			line += "，" + st.Reason
		}
		fmt.Fprintln(e.Out, line)
	}
	if chk != nil {
		if chk.Target != "" {
			fmt.Fprintf(e.Out, "最近一次檢查（%s）：有新版 %s\n", chk.CheckedAt.Format("2006-01-02 15:04"), chk.Target)
		} else {
			fmt.Fprintf(e.Out, "最近一次檢查（%s）：沒有新版（%s）\n", chk.CheckedAt.Format("2006-01-02 15:04"), chk.Detail)
		}
	}
	if waiting != nil && len(waiting.Migrations) > 0 {
		fmt.Fprintf(e.Out, "最近一次整理過資料的升級：%s → %s；升級前的備份留著（只留最新一份），有問題可以退回（--rollback，資料一起回到升級前）。不用請主人確認\n", waiting.From, waiting.To)
	}
	if len(skipped) > 0 {
		fmt.Fprintln(e.Out, "跳過的版本："+strings.Join(skipped, "、"))
	}
	if dec != nil {
		fmt.Fprintf(e.Out, "今晚更新：%s 會在 %s 更新\n", dec.Version, dec.At.Format("2006-01-02 15:04"))
	}
	return nil
}

func upgradeDecide(e *cmdline.Env, h *platform.Home, core, decision, version, exe string) error {
	chk, _ := ReadUpgradeCheck(h.Dir)
	if version == "" && chk != nil {
		version = chk.Target
	}
	if version == "" {
		fmt.Fprintln(e.Err, "現在沒有新版可以決定；先 evergenie upgrade --check")
		return cmdline.ErrReported
	}
	switch decision {
	case "now":
		if core == "" {
			fmt.Fprintln(e.Err, noCoreMessage)
			return cmdline.ErrReported
		}
		tag := ""
		if chk != nil && chk.Target == version {
			tag = chk.Tag
		}
		if tag == "" {
			tag = "v" + version
		}
		return detachUpgrade(e, h.Dir, exe, tag)
	case "tonight":
		at, err := DecideTonight(h, version, time.Now())
		if err != nil {
			return say(e, err)
		}
		fmt.Fprintf(e.Out, "好，%s 會在 %s 更新。\n", version, at.Format("2006-01-02 15:04"))
		return nil
	case "skip":
		if err := AddUpgradeSkipped(h.Dir, version); err != nil {
			return say(e, err)
		}
		_ = ClearUpgradeDecision(h.Dir)
		fmt.Fprintf(e.Out, "好，%s 這版先跳過。\n", version)
		return nil
	}
	return cmdline.Usagef("用法：%s", upgradeUsage)
}

// migrateHomeCommand 列出（預設）或依序跑（--apply）還沒跑過的家的遷移（HomeMigrations）。每個都能重跑；serve 啟動時也會先跑。
func migrateHomeCommand(e *cmdline.Env, args []string) error {
	vals, on, rest, ok := cmdline.LocalFlags(args, []string{"home"}, []string{"apply", "preview"})
	if !ok || len(rest) > 0 || (on["apply"] && on["preview"]) {
		return cmdline.Usagef("用法：%s", migrateUsage)
	}
	home, err := resolveHome(vals)
	if err != nil {
		return say(e, err)
	}
	pending, err := PendingMigrations(home, AllMigrations(home))
	if err != nil {
		return say(e, err)
	}
	if len(pending) == 0 {
		fmt.Fprintln(e.Out, "家的資料都是新的，沒有要整理的")
		return nil
	}
	if on["preview"] {
		// 在家的複本上整個走一遍，真的家不動（升級會先做這一步，DEV-154）。
		ran, err := PreviewMigrations(home)
		for _, id := range ran {
			fmt.Fprintln(e.Out, "預演通過："+id)
		}
		if err != nil {
			fmt.Fprintln(e.Err, "預演沒過，家裡的資料沒有動。")
			return say(e, err)
		}
		fmt.Fprintln(e.Out, "預演全部通過（只在複本上試，家裡的資料沒有動）")
		return nil
	}
	if !on["apply"] {
		for _, m := range pending {
			fmt.Fprintf(e.Out, "要做：%s（%s）\n", m.Description, m.ID)
		}
		fmt.Fprintln(e.Out, "加 --apply 才會真的做")
		return nil
	}
	ran, err := RunHomeMigrations(home)
	for _, id := range ran {
		fmt.Fprintln(e.Out, "已整理："+id)
	}
	if err != nil {
		return say(e, err)
	}
	return nil
}
