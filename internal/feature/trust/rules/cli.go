package rules

import (
	"bufio"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie trust …`、`evergenie setup permissions` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，只用 sdk 的 client 打 system 服務的 trust、set_permission_levels（CLI-1）。
// printDoctor 印 `trust diff` 的健檢結果（system 塊 doctor 的 Print，經 trust 塊的 Commands 傳進來；塊之間不互相 import）。
func Commands(printDoctor DoctorPrinter) map[string]app.Command {
	return map[string]app.Command{
		"trust show":        {Usage: "trust show|scan|diff|denied|snapshot [--json]   權限現況（只讀；snapshot 存一份快照到 runtime/trust/）", Group: cmdline.GroupSystem, Run: trustShow},
		"trust scan":        {Run: trustScan},
		"trust diff":        {Run: func(e *cmdline.Env, args []string) error { return trustDiff(e, args, printDoctor) }},
		"trust denied":      {Run: trustDenied},
		"trust snapshot":    {Run: trustSnapshot},
		"setup permissions": {Usage: "setup permissions [--choice all|ask|read] [--agent <職位> --level full|standard|readonly|clear]", Group: cmdline.GroupSystem, Run: setupPermissions},
	}
}

// DoctorPrinter 照 doctor 的格式印健檢結果。
type DoctorPrinter func(e *cmdline.Env, checks []sdk.DoctorCheck) error

func api(e *cmdline.Env) sdk.SystemAPI { return sdk.NewSystemClient(e.Client) }

// trustFlags 是 trust 子指令共用的旗標：--json、--days N、--scope 關鍵字。
type trustFlags struct {
	json  bool
	days  int32
	scope string
}

func parseTrustFlags(args []string, usage string, allowDays, allowScope bool) (trustFlags, error) {
	var f trustFlags
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--json":
			f.json = true
		case allowDays && a == "--days" && i+1 < len(args):
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return f, cmdline.Usagef("--days 要是正整數")
			}
			f.days = int32(n)
		case allowScope && a == "--scope" && i+1 < len(args):
			i++
			f.scope = args[i]
		default:
			return f, cmdline.Usagef("用法：%s", usage)
		}
	}
	return f, nil
}

var levelNames = map[sdk.PermissionLevel]string{
	sdk.PermissionLevelFull:     "最高（全部讓 AI 做）",
	sdk.PermissionLevelStandard: "一般（做大事先問你）",
	sdk.PermissionLevelReadOnly: "唯讀（只看不改）",
}

var levelSources = map[string]string{"agent": "單獨設定", "default_level": "整個家的預設", "builtin": "內建預設"}

// trust show：每位秘書的等級、可寫的 repo、額外放行與不開放的動作。
func trustShow(e *cmdline.Env, args []string) error {
	f, err := parseTrustFlags(args, "evergenie trust show [--json]", false, false)
	if err != nil {
		return err
	}
	r, err := api(e).Trust(e.Ctx, &sdk.SystemTrustRequest{})
	if err != nil {
		return err
	}
	if f.json {
		return e.Line(r)
	}
	var b strings.Builder
	b.WriteString("【權限等級】\n")
	for _, a := range r.Agents {
		fmt.Fprintf(&b, "  %-10s %s　（%s）\n", a.Agent, levelNames[a.Level], levelSources[a.Source])
	}
	fmt.Fprintf(&b, "  其他秘書：%s\n", levelNames[r.DefaultLevel])
	if p := r.Policy; p != nil {
		b.WriteString("\n【可以寫的 repo】\n")
		if len(p.Repos) == 0 {
			b.WriteString("  （沒有）\n")
		}
		for _, repo := range p.Repos {
			push := "不能 push"
			if repo.Push {
				push = "可以 push"
			}
			fmt.Fprintf(&b, "  %s　%s；可寫：%s\n", repo.Path, push, strings.Join(repo.Writers, "、"))
		}
		if len(p.Allow) > 0 {
			fmt.Fprintf(&b, "\n【額外放行】%s\n", strings.Join(p.Allow, "、"))
		}
		if len(p.Deny) > 0 {
			fmt.Fprintf(&b, "\n【不開放】%s\n", strings.Join(p.Deny, "、"))
		}
	}
	_, err = e.Out.Write([]byte(b.String()))
	return err
}

// trust scan：把權限來源攤成一張表（--scope 只看範圍含關鍵字的）。
func trustScan(e *cmdline.Env, args []string) error {
	f, err := parseTrustFlags(args, "evergenie trust scan [--json] [--scope 關鍵字]", false, true)
	if err != nil {
		return err
	}
	r, err := api(e).Trust(e.Ctx, &sdk.SystemTrustRequest{})
	if err != nil {
		return err
	}
	rows := []sdk.PermissionSource{}
	for _, s := range r.Sources {
		if f.scope == "" || strings.Contains(s.Scope, f.scope) {
			rows = append(rows, s)
		}
	}
	if f.json {
		return e.Line(rows)
	}
	var b strings.Builder
	last := ""
	for _, s := range rows {
		if s.Scope != last {
			fmt.Fprintf(&b, "\n## %s　%s\n", s.Scope, s.Source)
			last = s.Scope
		}
		fmt.Fprintf(&b, "  %-14s %s\n", s.Kind, s.Value)
	}
	fmt.Fprintf(&b, "\n共 %d 條\n", len(rows))
	_, err = e.Out.Write([]byte(b.String()))
	return err
}

// trust diff：權限設定與引擎產生的設定一不一致（等於 doctor 的「權限」那一段）。
func trustDiff(e *cmdline.Env, args []string, printDoctor DoctorPrinter) error {
	f, err := parseTrustFlags(args, "evergenie trust diff [--json]", false, false)
	if err != nil {
		return err
	}
	r, err := api(e).Doctor(e.Ctx, &sdk.SystemDoctorRequest{Groups: []sdk.DoctorGroup{sdk.DoctorGroupPermissions}})
	if err != nil {
		return err
	}
	if f.json {
		return e.Line(r)
	}
	return printDoctor(e, r.Checks)
}

// trust denied：最近被核心擋下或改成問你的動作，依秘書與動作類型統計。
func trustDenied(e *cmdline.Env, args []string) error {
	f, err := parseTrustFlags(args, "evergenie trust denied [--days N] [--json]", true, false)
	if err != nil {
		return err
	}
	r, err := api(e).Trust(e.Ctx, &sdk.SystemTrustRequest{DeniedDays: f.days})
	if err != nil {
		return err
	}
	if f.json {
		return e.Line(r.Denied)
	}
	return printDenied(e, r.Denied, f.days)
}

func printDenied(e *cmdline.Env, denied []sdk.DeniedAction, days int32) error {
	if days <= 0 {
		days = 7
	}
	var b strings.Builder
	if len(denied) == 0 {
		fmt.Fprintf(&b, "最近 %d 天沒有被擋的動作\n", days)
		_, err := e.Out.Write([]byte(b.String()))
		return err
	}
	counts := map[string]int{}
	for _, d := range denied {
		counts[fmt.Sprintf("%s　%s　%s", d.Agent, d.Class, d.Verdict)]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(&b, "最近 %d 天被擋 %d 次（deny＝擋下、ask＝改成問你）\n", days, len(denied))
	for _, k := range keys {
		fmt.Fprintf(&b, "  %s：%d 次\n", k, counts[k])
	}
	b.WriteString("\n最近幾筆：\n")
	for _, d := range denied[max(0, len(denied)-10):] {
		fmt.Fprintf(&b, "  %s　%s（%s 委派）%s %s：%s\n", d.At.Local().Format("01-02 15:04"), d.Agent, d.From, d.Class, d.Tool, d.Reason)
	}
	_, err := e.Out.Write([]byte(b.String()))
	return err
}

// trust snapshot：存一份權限現況的快照，印出跟上一份的差異與被擋統計（不改任何設定）。
func trustSnapshot(e *cmdline.Env, args []string) error {
	f, err := parseTrustFlags(args, "evergenie trust snapshot [--json]", false, false)
	if err != nil {
		return err
	}
	r, err := api(e).Trust(e.Ctx, &sdk.SystemTrustRequest{Snapshot: true})
	if err != nil {
		return err
	}
	if f.json {
		return e.Line(r)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "快照：%s\n", r.SnapshotPath)
	if len(r.Changes) == 0 {
		b.WriteString("跟上一份比沒有變化（或這是第一份）\n")
	} else {
		b.WriteString("跟上一份比：\n")
		for _, c := range r.Changes {
			fmt.Fprintf(&b, "  %s\n", c)
		}
	}
	b.WriteString("\n")
	if _, err := e.Out.Write([]byte(b.String())); err != nil {
		return err
	}
	return printDenied(e, r.Denied, 7)
}

// wizardChoices 是安裝時的權限選項（給不懂電腦的人看的白話），順序就是畫面上的 1、2、3。
var wizardChoices = []struct {
	key, title, explain string
	level               sdk.PermissionLevel
}{
	{"all", "全部讓 AI 幫我做", "秘書可以自己改檔案、跑指令、改系統設定，不會先問你。最省事，但做錯事時你事後才會知道。", sdk.PermissionLevelFull},
	{"ask", "做大事先問我", "秘書可以自己改檔案；要跑指令、刪東西、推送或對外送訊息之前會先問你。比較安全，但你會常收到確認。", sdk.PermissionLevelStandard},
	{"read", "只讓 AI 看不讓它改", "秘書只能看資料、回答問題，不能改任何檔案或設定。最安全，但很多事它做不了，要你自己動手。", sdk.PermissionLevelReadOnly},
}

// WizardLevel 把精靈的選項（1／2／3 或 all／ask／read）換成權限等級；不認得回 false。
func WizardLevel(choice string) (sdk.PermissionLevel, bool) {
	c := strings.ToLower(strings.TrimSpace(choice))
	for i, w := range wizardChoices {
		if c == w.key || c == strconv.Itoa(i+1) {
			return w.level, true
		}
	}
	return "", false
}

// setup permissions：安裝時設定權限。沒給 --choice 就印三個選項讓人選；--agent <id> --level <等級> 只改一位。
func setupPermissions(e *cmdline.Env, args []string) error {
	const usage = "evergenie setup permissions [--choice all|ask|read] [--agent <職位> --level full|standard|readonly|clear]"
	choice, agent, level := "", "", ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--choice" && i+1 < len(args):
			i++
			choice = args[i]
		case a == "--agent" && i+1 < len(args):
			i++
			agent = args[i]
		case a == "--level" && i+1 < len(args):
			i++
			level = args[i]
		default:
			return cmdline.Usagef("用法：%s", usage)
		}
	}
	req := &sdk.SystemSetPermissionLevelsRequest{By: &sdk.Actor{Name: e.By}}
	switch {
	case agent != "" || level != "":
		if agent == "" || level == "" || choice != "" {
			return cmdline.Usagef("用法：%s", usage)
		}
		l := sdk.PermissionLevel(strings.ToLower(strings.TrimSpace(level)))
		if l == "clear" {
			l = ""
		}
		req.Levels = []sdk.AgentPermission{{Agent: agent, Level: l}}
	default:
		if choice == "" {
			var b strings.Builder
			b.WriteString("要讓秘書做到什麼程度？\n\n")
			for i, w := range wizardChoices {
				fmt.Fprintf(&b, "  %d. %s\n     %s\n\n", i+1, w.title, w.explain)
			}
			b.WriteString("請輸入 1、2 或 3：")
			if _, err := e.Out.Write([]byte(b.String())); err != nil {
				return err
			}
			line, _ := bufio.NewReader(e.In).ReadString('\n')
			choice = line
			if _, err := e.Out.Write([]byte("\n")); err != nil {
				return err
			}
		}
		l, ok := WizardLevel(choice)
		if !ok {
			return cmdline.Usagef("請選 1、2、3（或 all、ask、read）")
		}
		req.DefaultLevel = l
	}
	r, err := api(e).SetPermissionLevels(e.Ctx, req)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("已更新權限（秘書下次開 session 時生效）：\n")
	others := []string{}
	for _, a := range r.Agents {
		fmt.Fprintf(&b, "  %-10s %s\n", a.Agent, levelNames[a.Level])
		if req.DefaultLevel != "" && a.Source == "agent" && a.Level != req.DefaultLevel {
			others = append(others, a.Agent)
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(&b, "\n%s 另外單獨設定過，維持原本的等級（要改用 --agent <職位> --level …）。\n", strings.Join(others, "、"))
	}
	if r.Backup != "" {
		fmt.Fprintf(&b, "舊的設定備份在 %s\n", r.Backup)
	}
	_, err = e.Out.Write([]byte(b.String()))
	return err
}
