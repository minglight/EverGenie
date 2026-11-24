package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 要動這台電腦的指令（service、preflight、tailscale）：cli 不碰系統，這裡把 internal/feature/system/host 接上去（Q4）。

// requiredTools 是原始碼版（git checkout，升級靠 git）一定要有的工具。下載版（versions/<版本>、current，升級靠下載）不用 git，
// 不檢查（DEV-155 S6：一般使用者的電腦沒有 git 不能顯示 ✘）。引擎要用的終端機工具不列在這裡：引擎套件自己檢查，
// 而且照它真的會用的順序找（引擎旁邊附帶的 ＞ PATH，ECL-63）；這裡只看 PATH 會把附帶的那支當成沒裝。
var requiredTools = []string{"git"}

// preflightExe 是正在跑的 EverGenie（看它是不是下載版）；測試可以換。
var preflightExe = os.Executable

// preflightTools 回這一份 EverGenie 要檢查的必備工具：下載版不用 git。
func preflightTools() []string {
	if exe, err := preflightExe(); err == nil && platform.DownloadInstallOf(exe) != "" {
		return nil
	}
	return requiredTools
}

// hostRunner 讓測試換成假的（不碰真的 launchd、brew、tailscale）。
var hostRunner host.Runner = host.ExecRunner{}

// hostCommands 是要動這台電腦的本機指令（cli 與業務模組都不碰系統）：init、service、tailscale；
// preflight 是業務模組 settings 的指令，實作 preflightCommand 由 registry 傳進去。
func hostCommands() map[string]cmdline.Command {
	cmds := map[string]cmdline.Command{
		"init": cmdline.HostCommand("init", initCommand, nil).With("init --home <資料夾> [--name 名字] [--port N] [--timezone 時區] [--state 狀態資料夾]   在空的資料夾建一個能開看板的新家（一鍵安裝用）", cmdline.GroupSystem),
		// serve 平常在 main 就接走了（不經指令表）；登記在這裡是為了 help 列得到它，從指令表叫也一樣能跑。
		"serve": {Usage: "serve --home <目錄>                核心服務（launchd 服務叫的就是它）", Group: cmdline.GroupSystem, Run: func(e *cmdline.Env, args []string) error {
			if code := serve(args, e.Out, e.Err); code != 0 {
				return cmdline.ExitCodeError{Code: code}
			}
			return nil
		}},
	}
	for _, sub := range []string{"install", "uninstall", "start", "stop", "restart", "status"} {
		cmds["service "+sub] = cmdline.SubHost("service", sub, serviceCommand)
	}
	cmds["service install"] = cmds["service install"].With("service install|uninstall|start|stop|restart|status [--home <家>] [--exe <evergenie>] [--root <試跑資料夾>]\nserve 的 launchd 服務（登入就啟動、掛了重開）", cmdline.GroupSystem)
	for _, sub := range []string{"status", "setup", "down", "funnel-off"} {
		cmds["tailscale "+sub] = cmdline.SubHost("tailscale", sub, tailscaleCommand)
	}
	cmds["tailscale setup"] = cmds["tailscale setup"].With("tailscale status|setup|down|funnel-off [--service <名>] [--https-port N]   讓手機連看板（只把看板用 tailscale serve 露出去，網址寫進 web.public_url；有 web.service 或 --service 就掛到 Tailscale 服務 svc:<名>；絕不開 Funnel，funnel-off 把看板對整個網路的公開關掉）", cmdline.GroupSystem)
	for name, c := range homeCommands() { // home relayout：家的佈局搬家（DEV-145）；home tidy：搬家前整理（DEV-154）
		cmds[name] = c
	}
	for name, c := range rescueCommands() { // rescue：出事時的修復入口（DEV-153）
		cmds[name] = c
	}
	// personas promote：開發家把自己的角色升成官方（DEV-154 第 13 件）；離線做（服務要停），用法寫在角色庫的 personas 那一筆。
	cmds["personas promote"] = personasPromoteCommand()
	return cmds
}

// flags 取出 --name 值（或 --name=值）與開關；其餘留在 rest（實作在 cmdline.LocalFlags，業務模組的本機指令也用它）。
func flags(args []string, withValue, switches []string) (vals map[string]string, on map[string]bool, rest []string, ok bool) {
	return cmdline.LocalFlags(args, withValue, switches)
}

func fail(w io.Writer, err error) int {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		fmt.Fprintln(w, pe.Message)
	} else {
		fmt.Fprintln(w, err.Error())
	}
	return 1
}

func resolveHome(vals map[string]string, def string) (string, error) {
	home := vals["home"]
	if home == "" {
		home = def
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

// serviceUsage：--label 沒給時用家的 home.toml [runtime] service_label，再沒有就是 com.evergenie.serve。
const serviceUsage = "evergenie service install|uninstall|start|stop|restart|status [--home <家>] [--exe <EverGenie 執行檔>] [--label <名稱>] [--root <試跑資料夾>]"

func serviceCommand(ctx context.Context, r cmdline.HostRequest) int {
	if len(r.Args) == 0 {
		fmt.Fprintln(r.Err, "用法："+serviceUsage)
		return 2
	}
	action := r.Args[0]
	vals, _, rest, ok := flags(r.Args[1:], []string{"home", "exe", "label", "root"}, nil)
	if !ok || len(rest) > 0 {
		fmt.Fprintln(r.Err, "用法："+serviceUsage)
		return 2
	}
	home, err := resolveHome(vals, r.Home)
	if err != nil {
		return fail(r.Err, err)
	}
	exe := vals["exe"]
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return fail(r.Err, platform.Invalid("exe", "找不到 EverGenie 執行檔，請用 --exe 指定"))
		}
	}
	if exe, err = filepath.Abs(exe); err != nil {
		return fail(r.Err, platform.Invalid("exe", "執行檔路徑不對"))
	}
	label := vals["label"]
	if label == "" {
		label = host.HomeServiceLabel(home)
	}
	svc := host.Service{Runner: hostRunner, Root: vals["root"], UID: os.Getuid(), Spec: host.ServiceSpec{Label: label, Exe: exe, Home: home}}
	var lines []string
	switch action {
	case "install":
		lines, err = svc.Install(ctx)
	case "uninstall":
		lines, err = svc.Uninstall(ctx)
	case "start":
		lines, err = svc.Start(ctx)
	case "stop":
		lines, err = svc.Stop(ctx)
	case "restart":
		lines, err = svc.Restart(ctx)
	case "status":
		st := svc.Status(ctx)
		lines = describeService(st, home)
	default:
		fmt.Fprintln(r.Err, "用法："+serviceUsage)
		return 2
	}
	for _, l := range lines {
		fmt.Fprintln(r.Out, l)
	}
	if err != nil {
		return fail(r.Err, err)
	}
	if action == "install" || action == "restart" {
		rescueEnsure(svc, home, exe) // 升級、部署都會走到這裡：補上修復入口（HST-11）
	}
	return 0
}

func describeService(st host.ServiceStatus, home string) []string {
	switch {
	case !st.Installed:
		return []string{st.Label + "：還沒安裝（evergenie service install）"}
	case !st.Loaded:
		return []string{st.Label + "：已安裝但沒有載入（evergenie service start）", "plist：" + st.Plist}
	}
	state := "已載入，但現在沒在跑"
	if st.Running {
		state = "在跑，pid " + strconv.Itoa(st.PID)
	}
	lines := []string{st.Label + "：" + state, "plist：" + st.Plist, "家：" + st.PlistHome}
	if st.PlistHome != "" && home != "" && filepath.Clean(st.PlistHome) != filepath.Clean(home) {
		lines = append(lines, "注意：服務管的家（"+st.PlistHome+"）跟你現在指定的家（"+home+"）不同")
	}
	return lines
}

const preflightUsage = "evergenie preflight [--need tailscale] [--fix]"

func preflightCommand(ctx context.Context, r cmdline.HostRequest) int {
	vals, on, rest, ok := flags(r.Args, []string{"need"}, []string{"fix"})
	if !ok || len(rest) > 0 {
		fmt.Fprintln(r.Err, "用法："+preflightUsage)
		return 2
	}
	need := map[string]bool{}
	if n := vals["need"]; n != "" {
		for _, x := range strings.Split(n, ",") {
			if x = strings.TrimSpace(x); x != "tailscale" {
				fmt.Fprintln(r.Err, "--need 目前只有 tailscale")
				return 2
			}
			need[x] = true
		}
	}
	// 每次都重問 serve：--fix 裝完（例如用 Homebrew 裝了引擎要的終端機工具）重查時，引擎那幾項才是新的。
	run := func() []host.Check {
		checks := host.Preflight(hostRunner, host.PreflightOptions{Need: need, Tools: preflightTools()})
		var extra []sdk.PreflightCheck
		ok := false
		if r.Engines != nil {
			extra, ok = r.Engines()
		}
		if !ok {
			return append(checks, host.Check{ID: "engines", Name: "帳號程式", Level: host.LevelWarn,
				Detail: "沒檢查：EverGenie 服務沒在跑（終端機工具、AI 程式、登入狀態由它回報）",
				Fix:    "裝好、服務開起來之後再跑一次 evergenie preflight"})
		}
		return append(checks, host.FromEngine(extra, checks)...)
	}
	checks := run()
	if on["fix"] {
		ran, err := host.Fix(ctx, hostRunner, r.Out, checks, need)
		if err != nil {
			fmt.Fprintln(r.Out, err.Error())
		}
		if ran != "" && err == nil {
			checks = run()
		}
	}
	if host.Print(r.Out, checks) > 0 {
		return 1
	}
	return 0
}

const tailscaleUsage = "evergenie tailscale status|setup|down|funnel-off [--home <家>] [--service <名>] [--https-port N]"

func tailscaleCommand(ctx context.Context, r cmdline.HostRequest) int {
	if len(r.Args) == 0 {
		fmt.Fprintln(r.Err, "用法："+tailscaleUsage)
		return 2
	}
	action := r.Args[0]
	vals, _, rest, ok := flags(r.Args[1:], []string{"home", "https-port", "service"}, nil)
	if !ok || len(rest) > 0 {
		fmt.Fprintln(r.Err, "用法："+tailscaleUsage)
		return 2
	}
	home, err := resolveHome(vals, r.Home)
	if err != nil {
		return fail(r.Err, err)
	}
	ts := host.Tailscale{Runner: hostRunner, Home: home, Service: vals["service"]}
	if v := vals["https-port"]; v != "" {
		if ts.Port, err = strconv.Atoi(v); err != nil || ts.Port < 1 || ts.Port > 65535 {
			fmt.Fprintln(r.Err, "--https-port 要是 1–65535 的數字")
			return 2
		}
	}
	var lines []string
	switch action {
	case "status":
		lines = ts.Status(ctx)
	case "setup":
		lines, err = ts.Setup(ctx)
	case "down":
		lines, err = ts.Down(ctx)
	case "funnel-off": // 看板被 Funnel 開到整個網路時關掉它（DEV-153）；手機連線不受影響
		lines, err = ts.CloseFunnel(ctx)
	default:
		fmt.Fprintln(r.Err, "用法："+tailscaleUsage)
		return 2
	}
	for _, l := range lines {
		fmt.Fprintln(r.Out, l)
	}
	if err != nil {
		return fail(r.Err, err)
	}
	return 0
}

// launchdCheck 把 serviceCheck 交給 doctor 業務模組當離線檢查（registry 傳進 doctor.Commands）。
func launchdCheck(ctx context.Context, home string) []sdk.DoctorCheck {
	return []sdk.DoctorCheck{serviceCheck(ctx, home)}
}

// tailscaleDoctorTimeout 是 doctor 叫 tailscale 每支指令最多等多久。
const tailscaleDoctorTimeout = 10 * time.Second

// tailscaleCheck 是 doctor 的「看板的 Tailscale 服務」（只讀）：home.toml 寫了 web.service 才檢查——
// 這台沒掛上或後台還沒核准給黃字；沒寫 web.service 這一項不出現（用機器 serve 的家照舊）。
func tailscaleCheck(ctx context.Context, home string) []sdk.DoctorCheck {
	if home == "" {
		return nil
	}
	h, err := platform.LoadHome(home)
	if err != nil || h.Web.Service == "" {
		return nil
	}
	check := func(sev sdk.DoctorSeverity, detail, fix string) []sdk.DoctorCheck {
		return []sdk.DoctorCheck{{ID: "config.tailscale", Group: sdk.DoctorGroupConfig, Ok: sev == sdk.DoctorSeverityOk, Severity: sev, Detail: detail, Fix: fix}}
	}
	svc := "svc:" + h.Web.Service
	if !platform.ValidTailscaleService(h.Web.Service) {
		return check(sdk.DoctorSeverityWarn, "web.service 的格式不對（"+h.Web.Service+"）：只能用小寫英文、數字與連字號", "evergenie config set web.service '\"genie-<owner>-<machine>\"'")
	}
	// doctor 不能被 tailscale 卡住：兩支指令（status、serve get-config）各最多等 10 秒。
	runner := hostRunner
	if er, ok := runner.(host.ExecRunner); ok && er.Timeout == 0 {
		er.Timeout = tailscaleDoctorTimeout
		runner = er
	}
	s := host.Tailscale{Runner: runner, Home: home}.CheckService(ctx)
	switch {
	case s.Bin == "":
		return check(sdk.DoctorSeverityWarn, "web.service 寫了 "+svc+"，但這台找不到 Tailscale", "到 "+host.TailscaleDownload+" 下載 Tailscale App，裝好並登入")
	case s.Backend != "Running":
		return check(sdk.DoctorSeverityWarn, "web.service 寫了 "+svc+"，但這台的 Tailscale 沒連上", "打開 Tailscale App 登入，再看 evergenie tailscale status")
	case !s.Tagged:
		return check(sdk.DoctorSeverityWarn, "看板的 Tailscale 服務 "+svc+"：這台不是 tag 身分，當不了服務主機", "到 Tailscale 後台把這台改成 tag 身分，再 evergenie tailscale setup")
	case s.Elsewhere:
		return check(sdk.DoctorSeverityWarn, "看板的 Tailscale 服務 "+svc+"：掛的是別的 port（指到 "+s.Target+"，這個家的看板是 127.0.0.1:"+strconv.Itoa(s.Port)+"）", "換一個服務名稱：evergenie tailscale setup --service <名>")
	case !s.Mounted && s.Problem != "":
		return check(sdk.DoctorSeverityWarn, "看板的 Tailscale 服務 "+svc+"："+s.Problem, "evergenie tailscale status")
	case !s.Mounted:
		return check(sdk.DoctorSeverityWarn, "看板的 Tailscale 服務 "+svc+"：這台沒掛上", "evergenie tailscale setup")
	case !s.Approved:
		return check(sdk.DoctorSeverityWarn, "看板的 Tailscale 服務 "+svc+"：這台掛上了，但後台還沒核准（網址打不開）", "到 Tailscale 後台 Services 頁核准這台，或設 autoApprovers")
	}
	return check(sdk.DoctorSeverityOk, "看板掛在 Tailscale 服務 "+svc+"（"+s.URL+"），後台已核准", "")
}

// serviceCheck 是 doctor 的「服務有沒有裝」：讀 plist（和 launchctl 的載入狀況），只讀。
func serviceCheck(ctx context.Context, home string) sdk.DoctorCheck {
	check := func(sev sdk.DoctorSeverity, detail, fix string) sdk.DoctorCheck {
		return sdk.DoctorCheck{ID: "config.launchd", Group: sdk.DoctorGroupConfig, Ok: sev == sdk.DoctorSeverityOk, Severity: sev, Detail: detail, Fix: fix}
	}
	svc := host.Service{Runner: hostRunner, UID: os.Getuid(), Spec: host.ServiceSpec{Label: host.HomeServiceLabel(home), Home: home}}
	st := svc.Status(ctx)
	if st.Process { // EVERGENIE_SERVICE_MODE=process：測試與 release-e2e 用，沒有 launchd
		if st.Running {
			return check(sdk.DoctorSeverityOk, "serve 以背景行程在跑（測試模式，沒有 launchd），pid "+strconv.Itoa(st.PID), "")
		}
		return check(sdk.DoctorSeverityOk, "serve 沒有以背景行程在跑（測試模式，沒有 launchd）", "")
	}
	switch {
	case !st.Installed:
		return check(sdk.DoctorSeverityOk, "serve 還沒安裝成 launchd 服務（切換前正常；要裝：evergenie service install）", "")
	case !st.Loaded:
		return check(sdk.DoctorSeverityWarn, "serve 的 launchd 服務裝了但沒有載入", "執行 evergenie service start")
	case !st.Running:
		errLog := "runtime/logs/serve.err.log"
		if l := platform.LayoutFor(home); home != "" && l.V2 { // 第 2 版的暫存在家外面，寫完整路徑
			errLog = l.Runtime("logs", "serve.err.log")
		}
		return check(sdk.DoctorSeverityWarn, "serve 的 launchd 服務已載入，但現在沒有在跑", "看 "+errLog+"，或 evergenie service restart")
	case home != "" && st.PlistHome != "" && filepath.Clean(st.PlistHome) != filepath.Clean(home):
		return check(sdk.DoctorSeverityWarn, "serve 的 launchd 服務管的是另一個家："+st.PlistHome, "要換成這個家：evergenie service install --home "+home)
	}
	return check(sdk.DoctorSeverityOk, "serve 已安裝成 launchd 服務，在跑（pid "+strconv.Itoa(st.PID)+"）", "")
}
