package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/agent/personaref"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/agents/persona/recode"
	"github.com/minglight/evergenie/internal/feature/packages/pkg"
)

// `evergenie home tidy`（DEV-154 第 10 件）與 `evergenie personas promote`（第 13 件）：離線改角色代號（PRS-22～PRS-24）。
// 改代號的工具在角色庫（recode），它不認得任何套件；這裡是組裝點：讀各層 package.toml 的宣告（PKG-64）轉給它。

const homeTidyHelp = `用法：evergenie home tidy [--home <家>] [--rename <舊>=<新>]… [--adopt <代號>]… [--official <代號>]… [--move-official] [--apply] [--json]
      evergenie home tidy --revert|--forget [--apply] [--json]

搬家前把家整理好（預設只預演，什麼都不改；要先停服務）：
  一、自己的角色（personas/mine/）代號改成 my_ 開頭：卡、圖的檔名、名冊、套件紀錄、設定一起改
      --rename <舊>=<新>   指定新代號（要 my_ 開頭；可以寫好幾次）；名字不合規則、跟官方同代號的不自動改，要用這個
  二、放在官方位置、但不是官方的角色：列出來
      --adopt <代號>       收進 personas/mine/、改成 my_<代號>（圖從官方的 avatars/ 搬進 personas/mine/avatars/；可以寫好幾次）
      --official <代號>    主人確認這張是官方的（沒有角色包登記檔的舊家判斷不出來；只影響這一次，可以寫好幾次）
  三、官方的卡與圖還在家裡（第 2 版佈局、official_personas = "home"、不是發行角色包的家）
      --move-official      搬到狀態資料夾，拿掉 home.toml 的 official_personas = "home"
  四、家裡散著的舊備份：只列出
套件紀錄怎麼跟著改，看各套件 package.toml 的 persona_field、persona_id、persona_prefixed、persona_source；
沒宣告、改不了的地方列出「檔名:行號」請人看。
套用時整份計畫寫進 settings/home-tidy.json、改過的檔原文存 settings/home-tidy-originals/；中途失敗自動退回。
  --revert             照紀錄退回到整理前（一樣先預演，加 --apply 才退；退完紀錄檔與原文拿掉）
  --forget             確定不退了：紀錄檔與原文改名成備份（.bak-<日期>-done）；收尾後才能再整理、再做 home relayout
  --json               印 JSON（每一步、每一處）
`

const personasPromoteHelp = `用法：evergenie personas promote <my_名字> [--as <官方代號>] [--home <家>] [--apply] [--json]
      evergenie personas promote <my_名字> --revert|--forget [--apply] [--json]

開發家把自己的角色升成官方（預設只預演；要先停服務；只有開發家能做；秘書代跑前要主人點頭）：
  卡從 personas/mine/<my_名字>.md 搬到官方角色資料夾、代號去掉 my_（或 --as 指定），內容原樣
  圖留在 personas/mine/，只改檔名（衣服之後用展示間的衣服「升成官方」一套一套升；自己的隱藏版不搬）
  名冊、套件紀錄、設定跟著新代號
紀錄在 settings/persona-promote/<my_名字>.json；--revert 照紀錄退回、--forget 確定不退了把紀錄改名成備份（都要加 --apply 才做）。
`

// homeTidyCommand 掛在 home 底下（home relayout 旁邊；用法寫在 home relayout 那一筆）。
func homeTidyCommand() cmdline.Command {
	return cmdline.Command{Run: func(e *cmdline.Env, args []string) error {
		if code := homeTidy(cmdline.EnvHome(), args, e.Out, e.Err, time.Now()); code != 0 {
			return cmdline.ExitCodeError{Code: code}
		}
		return nil
	}}
}

// personasPromoteCommand 是 personas promote：離線指令（服務要停著），所以登記在組裝點、不經角色庫的 API。
func personasPromoteCommand() cmdline.Command {
	return cmdline.Command{Run: func(e *cmdline.Env, args []string) error {
		if code := personasPromote(cmdline.EnvHome(), e.By, args, e.Out, e.Err, time.Now()); code != 0 {
			return cmdline.ExitCodeError{Code: code}
		}
		return nil
	}}
}

// recodeKinds 讀這個家各層套件的宣告，轉成改代號工具認得的樣子。
func recodeKinds(home string) ([]recode.Kind, []recode.PackageIssue) {
	pks, problems := pkg.PersonaKinds(home)
	issues := make([]recode.PackageIssue, 0, len(problems))
	for _, p := range problems {
		issues = append(issues, recode.PackageIssue{Package: p.Package, Message: p.Message})
	}
	out := make([]recode.Kind, 0, len(pks))
	for _, k := range pks {
		out = append(out, recode.Kind{Kind: k.Kind, Tier: string(k.Tier), Field: k.Decl.PersonaField, ID: k.Decl.PersonaID,
			Prefixed: k.Decl.PersonaPrefixed, Source: k.Decl.PersonaSource, File: k.Decl.FileField})
	}
	return out, issues
}

// pullRepeated 把可以寫好幾次的 --name 值（--name v 或 --name=v）拿出來，其餘原樣留給 flags。
func pullRepeated(args []string, names ...string) (map[string][]string, []string, bool) {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	got := map[string][]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, has := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !strings.HasPrefix(a, "--") || !want[name] {
			rest = append(rest, a)
			continue
		}
		if !has {
			if i+1 >= len(args) {
				return nil, nil, false
			}
			i++
			value = args[i]
		}
		got[name] = append(got[name], value)
	}
	return got, rest, true
}

func printResult(out io.Writer, asJSON, applied bool, v any, text string) {
	if !asJSON {
		fmt.Fprintln(out, text)
		return
	}
	data, _ := json.MarshalIndent(struct {
		Applied bool `json:"applied"`
		Result  any  `json:"result"`
	}{applied, v}, "", "  ")
	fmt.Fprintln(out, string(data))
}

// homeTidy 回結束碼：0 成功（含預演）、1 不能做或失敗、2 用法錯誤。
func homeTidy(envHome string, args []string, out, errw io.Writer, now time.Time) int {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(out, homeTidyHelp)
			return 0
		}
	}
	rep, args, ok := pullRepeated(args, "rename", "adopt", "official")
	if !ok {
		fmt.Fprint(errw, homeTidyHelp)
		return 2
	}
	vals, on, rest, ok := flags(args, []string{"home"}, []string{"apply", "revert", "forget", "move-official", "json"})
	if !ok || len(rest) > 0 {
		fmt.Fprint(errw, homeTidyHelp)
		return 2
	}
	home, err := resolveHome(vals, envHome)
	if err != nil {
		return fail(errw, err)
	}
	home = filepath.Clean(platform.ExpandHome(home))
	if on["revert"] || on["forget"] {
		if on["revert"] && on["forget"] {
			fmt.Fprintln(errw, "--revert（退回）與 --forget（收尾、不退了）只能選一個")
			return 2
		}
		if len(rep) > 0 || on["move-official"] {
			fmt.Fprintln(errw, "--revert、--forget 照紀錄做，不用寫 --rename、--adopt、--official、--move-official")
			return 2
		}
		if on["forget"] {
			return recodeForget(recode.TidyJournal(home), on["apply"], on["json"], out, errw, now)
		}
		return recodeRevert(home, recode.TidyJournal(home), on["apply"], on["json"], out, errw)
	}
	opt := recode.TidyOptions{Rename: map[string]string{}, Adopt: rep["adopt"], Official: rep["official"], MoveOfficial: on["move-official"]}
	for _, r := range rep["rename"] {
		old, nw, ok := strings.Cut(r, "=")
		if !ok || old == "" || nw == "" {
			fmt.Fprintln(errw, "--rename 要寫成 <舊代號>=<新代號>（例：--rename mika=my_mika）")
			return 2
		}
		if _, dup := opt.Rename[old]; dup {
			fmt.Fprintln(errw, "--rename "+old+" 寫了兩次")
			return 2
		}
		opt.Rename[old] = nw
	}
	kinds, issues := recodeKinds(home)
	t, err := recode.PlanTidy(home, kinds, issues, opt)
	if err != nil {
		return fail(errw, err)
	}
	if !on["apply"] || len(t.Problems) > 0 || len(t.Steps) == 0 {
		printResult(out, on["json"], false, t, t.Text(false))
		if len(t.Problems) > 0 {
			return 1
		}
		return 0
	}
	if err := t.Apply("home tidy", recode.TidyJournal(home), recode.TidyOriginals(home), now); err != nil {
		return fail(errw, err)
	}
	printResult(out, on["json"], true, t, t.Text(true))
	return 0
}

// personasPromote 回結束碼：0 成功（含預演）、1 不能做或失敗、2 用法錯誤。by 是操作者（$EVERGENIE_AGENT，沒有就是主人 owner(cli)）。
func personasPromote(envHome, by string, args []string, out, errw io.Writer, now time.Time) int {
	for _, a := range args {
		if a == "--help" || a == "-h" {
			fmt.Fprint(out, personasPromoteHelp)
			return 0
		}
	}
	vals, on, rest, ok := flags(args, []string{"home", "as"}, []string{"apply", "revert", "forget", "json"})
	if !ok || len(rest) != 1 {
		fmt.Fprint(errw, personasPromoteHelp)
		return 2
	}
	home, err := resolveHome(vals, envHome)
	if err != nil {
		return fail(errw, err)
	}
	home = filepath.Clean(platform.ExpandHome(home))
	mine := rest[0]
	// 只有開發家能做（PlanPromote 檢查）。操作者不另外擋：這是離線指令（服務要停），主人平常只透過秘書做事，
	// 擋秘書的 session 等於沒有人能用；秘書代跑前要主人點頭，由共用規則管（主人沒交代的改動先問），不靠環境變數假裝驗身分。
	_ = by
	if on["revert"] || on["forget"] {
		if on["revert"] && on["forget"] {
			fmt.Fprintln(errw, "--revert（退回）與 --forget（收尾、不退了）只能選一個")
			return 2
		}
		if vals["as"] != "" {
			fmt.Fprintln(errw, "--revert、--forget 照紀錄做，不用寫 --as")
			return 2
		}
		if h, err := platform.LoadHome(home); err != nil {
			return fail(errw, err)
		} else if !h.IsDev() {
			return fail(errw, platform.Forbidden("升成官方（與退回、收尾）只能在開發家做；這個家不是開發家，什麼都沒動"))
		}
		// 紀錄檔名就是舊代號：跟升的時候一樣先檢查，免得 ../ 之類的指到別處。
		if !personaref.MineSlugPattern.MatchString(mine) {
			return fail(errw, platform.Invalid("persona", "要退回或收尾的是升過的自己的角色，寫升之前的代號（my_ 開頭，例：my_mika）"))
		}
		if on["forget"] {
			return recodeForget(recode.PromoteJournal(home, mine), on["apply"], on["json"], out, errw, now)
		}
		return recodeRevert(home, recode.PromoteJournal(home, mine), on["apply"], on["json"], out, errw)
	}
	kinds, issues := recodeKinds(home)
	p, err := recode.PlanPromote(home, mine, vals["as"], kinds, issues)
	if err != nil {
		return fail(errw, err)
	}
	if !on["apply"] || len(p.Problems) > 0 {
		printResult(out, on["json"], false, p, p.Text(false))
		if len(p.Problems) > 0 {
			return 1
		}
		return 0
	}
	if err := p.Apply("personas promote", recode.PromoteJournal(home, mine), recode.PromoteOriginals(home, mine), now); err != nil {
		return fail(errw, err)
	}
	printResult(out, on["json"], true, p, p.Text(true))
	return 0
}

// recodeRevert 照紀錄退回（home tidy、personas promote 共用）：預設只預演；服務在跑不退。
func recodeRevert(home, journal string, apply, asJSON bool, out, errw io.Writer) int {
	rp, err := recode.PlanRevert(journal)
	if err != nil {
		return fail(errw, err)
	}
	if msg := platform.ServiceRunning(home); msg != "" {
		rp.Problems = append(rp.Problems, strings.Replace(msg, "再搬", "再退回", 1))
	}
	if !apply || len(rp.Problems) > 0 {
		printResult(out, asJSON, false, rp, rp.Text(false))
		if len(rp.Problems) > 0 {
			return 1
		}
		return 0
	}
	if err := recode.Revert(journal); err != nil {
		return fail(errw, err)
	}
	printResult(out, asJSON, true, rp, rp.Text(true))
	return 0
}

// recodeForget 收尾（home tidy、personas promote 共用）：確定不退了，紀錄檔與原文改名成備份；預設只預演。
// 只改 settings/ 裡的兩個名字，服務在跑也可以做。
func recodeForget(journal string, apply, asJSON bool, out, errw io.Writer, now time.Time) int {
	fp, err := recode.PlanForget(journal, now)
	if err != nil {
		return fail(errw, err)
	}
	if !apply || len(fp.Problems) > 0 {
		printResult(out, asJSON, false, fp, fp.Text(false))
		if len(fp.Problems) > 0 {
			return 1
		}
		return 0
	}
	if fp, err = recode.Forget(journal, now); err != nil {
		return fail(errw, err)
	}
	printResult(out, asJSON, true, fp, fp.Text(true))
	return 0
}
