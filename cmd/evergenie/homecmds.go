package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
)

// `evergenie home relayout`（DEV-145）：把第 1 版的家搬成第 2 版（暫存、密鑰、官方角色、別人的套件搬到家外面的狀態資料夾，
// 附件與圖庫搬到 <家>/media/），或照紀錄退回。預設只預演；不會自己停服務。

const homeRelayoutUsage = "home relayout|tidy …   relayout [--state <資料夾>] [--move-personas] [--revert] [--apply] 把家搬成第 2 版佈局；" +
	"tidy [--rename 舊=新]… [--adopt 代號]… [--move-official] [--revert|--forget] [--apply] [--json] 搬家前整理（自己的角色改成 my_ 開頭、放錯位置的角色收成自己的、官方的卡與圖搬出家、列出舊備份）" +
	"（都是預設只預演、--apply 才做、--revert 照紀錄退回；tidy 的 --forget 確定不退了、收尾；要先停服務；evergenie home tidy --help 看細節）"

const homeRelayoutHelp = `用法：evergenie home relayout [--home <家>] [--state <資料夾>] [--move-personas] [--revert] [--apply]

把第 1 版的家搬成第 2 版：
  runtime/share、runtime/art      → 家/media/（主人的檔案）
  runtime/ 其餘、secrets/          → 狀態資料夾
  packages/installed/              → 狀態資料夾
  官方角色（卡、頭像、衣櫃、目錄）  預設留在家裡（跟家一起備份）；加 --move-personas 才搬去狀態資料夾（角色包網址設好、拿得回來的家才用；發行角色包的家一律留）
  personas/mine/ 等主人的角色      留在家裡
  家裡文字檔裡的 runtime/art、runtime/share 絕對路徑一起改寫（加 --move-personas 時官方角色的也改）（原文存 settings/relayout-rewrites/）

  --state <資料夾>   狀態資料夾（絕對路徑或 ~ 開頭；沒寫＝~/.evergenie/state/<家的資料夾名>）
  --apply            真的搬（沒加就只預演，什麼都不改）
  --move-personas    官方角色也搬去狀態資料夾（預設留在家裡）
  --revert           照 settings/relayout.json 搬回第 1 版（一樣先預演，加 --apply 才真的搬回）
搬之前要先停服務（evergenie service stop）；只做同一顆磁碟的改名，跨磁碟會停下不搬。
搬完要重裝服務（evergenie service install）再啟動，還開著的對話要重開。
`

func homeCommands() map[string]cmdline.Command {
	return map[string]cmdline.Command{
		"home": {Run: func(e *cmdline.Env, args []string) error {
			if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
				return &cmdline.HelpText{Text: homeRelayoutHelp + "\n" + homeTidyHelp}
			}
			return cmdline.Usagef("不認得的子指令：home %s（現在有 home relayout、home tidy）", args[0])
		}},
		"home tidy": homeTidyCommand(),
		"home relayout": cmdline.Command{Run: func(e *cmdline.Env, args []string) error {
			if code := homeRelayout(cmdline.EnvHome(), args, e.Out, e.Err, time.Now()); code != 0 {
				return cmdline.ExitCodeError{Code: code}
			}
			return nil
		}}.With(homeRelayoutUsage, cmdline.GroupSystem),
	}
}

// homeRelayout 回結束碼：0 成功、1 不能搬或失敗、2 用法錯誤。
func homeRelayout(envHome string, args []string, out, errw io.Writer, now time.Time) int {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(out, homeRelayoutHelp)
			return 0
		}
	}
	vals, on, rest, ok := flags(args, []string{"home", "state"}, []string{"apply", "revert", "move-personas"})
	if !ok || len(rest) > 0 {
		fmt.Fprint(errw, homeRelayoutHelp)
		return 2
	}
	home, err := resolveHome(vals, envHome)
	if err != nil {
		return fail(errw, err)
	}
	home = filepath.Clean(platform.ExpandHome(home))
	if on["revert"] {
		if vals["state"] != "" {
			fmt.Fprintln(errw, "--revert 不用寫 --state（照紀錄搬回）")
			return 2
		}
		return homeRevert(home, on["apply"], out, errw, now)
	}
	if s := vals["state"]; s != "" && !filepath.IsAbs(s) && !strings.HasPrefix(s, "~") {
		fmt.Fprintln(errw, "--state 要寫絕對路徑或 ~ 開頭")
		return 2
	}
	plan, err := platform.PlanRelayoutWith(home, vals["state"], on["move-personas"])
	if err != nil {
		return fail(errw, err)
	}
	printRelayoutPlan(out, plan)
	if len(plan.Problems) > 0 {
		fmt.Fprintln(errw, "不能搬：")
		for _, p := range plan.Problems {
			fmt.Fprintln(errw, "  - "+p)
		}
		return 1
	}
	label := host.HomeServiceLabel(home) // 搬之前讀：照這個家的 service_label
	if !on["apply"] {
		printNextSteps(out, home, label, false)
		fmt.Fprintln(out, "\n這是預演，什麼都沒改。要真的搬請加 --apply")
		return 0
	}
	if err := platform.ApplyRelayout(plan, now); err != nil {
		return fail(errw, err)
	}
	fmt.Fprintln(out, "\n搬好了：這個家現在是第 2 版（home.toml 加了 [layout]、schema 改成 2）。")
	fmt.Fprintln(out, "home.toml 的備份："+plan.Backup)
	printNextSteps(out, home, label, true)
	return 0
}

// printNextSteps 是搬完要做的事：launchd 設定檔的 log 路徑還指家裡，要重裝服務再啟動；還開著的 session 要重開。
func printNextSteps(out io.Writer, home, label string, done bool) {
	lbl := ""
	if label != "" {
		lbl = " --label " + label
	}
	if done {
		fmt.Fprintln(out, "\n接下來：")
	} else {
		fmt.Fprintln(out, "\n接下來（--apply 搬完後）：")
	}
	fmt.Fprintf(out, "  1. evergenie service install --home %s%s（服務設定檔裡的 log 路徑還指家裡，要重裝）\n", home, lbl)
	fmt.Fprintf(out, "  2. evergenie service start --home %s%s\n", home, lbl)
	fmt.Fprintln(out, "  3. 搬家前就開著的對話（session）要重開，才會照新位置走")
	fmt.Fprintln(out, "  要退回：先停服務，再 evergenie home relayout --revert --apply")
}

func homeRevert(home string, apply bool, out, errw io.Writer, now time.Time) int {
	plan, err := platform.PlanRevert(home)
	if err != nil {
		return fail(errw, err)
	}
	fmt.Fprintf(out, "家：%s\n狀態資料夾：%s\n照紀錄搬回第 1 版（「家/」「狀態/」指這兩個資料夾）：\n", home, plan.State)
	for _, m := range plan.Moves {
		fmt.Fprintf(out, "  [%s] %s → %s（%s）\n", m.Kind, shortPath(m.From, home, plan.State), shortPath(m.To, home, plan.State), platform.HumanSize(m.Size))
	}
	printRewrites(out, plan.Rewrites, "還原路徑")
	if len(plan.Problems) > 0 {
		fmt.Fprintln(errw, "不能退回：")
		for _, p := range plan.Problems {
			fmt.Fprintln(errw, "  - "+p)
		}
		return 1
	}
	if !apply {
		fmt.Fprintln(out, "\n這是預演，什麼都沒改。要真的搬回請加 --apply（先停服務）")
		return 0
	}
	if err := platform.RevertRelayout(home, now); err != nil {
		return fail(errw, err)
	}
	fmt.Fprintln(out, "\n退回好了：這個家回到第 1 版（home.toml 的 [layout] 拿掉了、schema 改回 1，紀錄改名成 relayout.json.bak-"+now.Format("20060102")+"-reverted）。")
	fmt.Fprintf(out, "接下來：evergenie service install --home %s（服務設定檔的 log 路徑還指狀態資料夾，要重裝；重裝會順便啟動），還開著的對話要重開\n", home)
	return 0
}

func printRelayoutPlan(out io.Writer, p *platform.RelayoutPlan) {
	fmt.Fprintf(out, "家：%s\n狀態資料夾：%s\n（下面的「家/」「狀態/」指這兩個資料夾）\n", p.Home, p.State)
	if len(p.Moves) == 0 {
		fmt.Fprintln(out, "沒有要搬的東西。")
	} else {
		fmt.Fprintln(out, "要搬的：")
		var total int64
		for _, m := range p.Moves {
			total += m.Size
			fmt.Fprintf(out, "  [%s] %s → %s（%s）\n", m.Kind, shortPath(m.From, p.Home, p.State), shortPath(m.To, p.Home, p.State), platform.HumanSize(m.Size))
		}
		fmt.Fprintf(out, "  共 %d 項、%s（同一顆磁碟改名，不複製）\n", len(p.Moves), platform.HumanSize(total))
	}
	if p.PersonasAtHome {
		if p.Publishes {
			fmt.Fprintln(out, "這是發行角色包的家（[admin] public_personas 有寫）：官方角色是主人寫的原稿，留在家裡（[layout] 會寫 official_personas = \"home\"）。")
		} else {
			fmt.Fprintln(out, "官方角色留在家裡、跟家一起備份（[layout] 會寫 official_personas = \"home\"）；角色包拿得回來、想搬去狀態資料夾才加 --move-personas。")
		}
	}
	rewriteTitle := "家裡 runtime/art、runtime/share 改成 media/"
	if !p.PersonasAtHome {
		rewriteTitle += "，官方角色改成狀態資料夾"
	}
	printRewrites(out, p.Rewrites, rewriteTitle)
	if len(p.RuntimeRefs) > 0 {
		n := 0
		for _, r := range p.RuntimeRefs {
			n += r.Count
		}
		fmt.Fprintf(out, "還有 %d 處寫了家裡 runtime 的路徑（%d 個檔），不會改（搬完那些路徑會找不到，要的話自己改）：\n", n, len(p.RuntimeRefs))
		for i, r := range p.RuntimeRefs {
			if i == 5 {
				fmt.Fprintf(out, "  …還有 %d 個檔\n", len(p.RuntimeRefs)-5)
				break
			}
			fmt.Fprintf(out, "  %s（%d 處）\n", r.Path, r.Count)
		}
	}
	if len(p.NotTemp) > 0 {
		fmt.Fprintln(out, "runtime/ 底下這些不是暫存，搬到狀態資料夾後不要刪：")
		for _, n := range p.NotTemp {
			fmt.Fprintf(out, "  狀態/runtime/%s\n", n)
		}
	}
	if len(p.Stay) > 0 {
		fmt.Fprintln(out, "留在家裡（不動）：")
		for _, s := range p.Stay {
			rel, err := filepath.Rel(p.Home, s.From)
			if err != nil {
				rel = s.From
			}
			fmt.Fprintf(out, "  [%s] %s（%s）\n", s.Kind, rel, platform.HumanSize(s.Size))
		}
	}
}

// printRewrites 列出要改寫路徑的文字檔：幾個檔、幾處（原文會先存進 settings/relayout-rewrites/）。
func printRewrites(out io.Writer, list []platform.RelayoutRewrite, what string) {
	if len(list) == 0 {
		return
	}
	n := 0
	for _, r := range list {
		n += r.Count
	}
	fmt.Fprintf(out, "文字檔裡的路徑（%s）：%d 個檔、%d 處（原文在 settings/relayout-rewrites/）\n", what, len(list), n)
	for i, r := range list {
		if i == 10 {
			fmt.Fprintf(out, "  …還有 %d 個檔\n", len(list)-10)
			break
		}
		fmt.Fprintf(out, "  %s（%d 處）\n", r.Path, r.Count)
	}
}

// shortPath 把路徑寫成「家/…」或「狀態/…」，比較好讀。
func shortPath(p, home, state string) string {
	for _, b := range []struct{ dir, name string }{{state, "狀態"}, {home, "家"}} {
		if b.dir == "" {
			continue
		}
		if rel, err := filepath.Rel(b.dir, p); err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
			return b.name + "/" + rel
		}
	}
	return p
}
