package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/feature/system/homebackup"
	"github.com/minglight/evergenie/internal/feature/system/host"
	"github.com/minglight/evergenie/sdk"
)

// 出事自救的「修復」入口（DEV-153 A2；規則 HST-10～12 在 ai_docs/modules/system.md）。
//
// 看板打不開時，主人沒有任何東西可以按（秘書也收不到話）。安裝時在 ~/Applications 放一個可以點兩下的
// 「evergenie 修復.command」（rescue install），它只做一件事：叫 `evergenie rescue --ask`。
// rescue：健檢 → 重開服務 → 等看板 → 再健檢 → 把結果開給主人看（看板好了開看板，沒好開結果檔）；
// --ask 時再問要不要重新登入 AI 帳號（引擎在檢查項目上給的 Login 指令，由主人自己在這個視窗登入）。
// 全程不用主人打指令、不用 sudo、不送出任何東西；只重開這個家自己的服務。

const rescueUsage = "evergenie rescue [--ask] | rescue install [--home <家>] [--exe <evergenie>] [--dir <資料夾>]"

// 測試可以換掉的：跑自己的子指令、開檔案或網址、看板有沒有回應、跑登入指令、等看板多久。
var (
	rescueSelf = func(ctx context.Context, home string, args ...string) (string, int) {
		exe, err := os.Executable()
		if err != nil {
			return "找不到 EverGenie 執行檔", 1
		}
		cmd := exec.CommandContext(ctx, exe, args...)
		cmd.Env = append(os.Environ(), "EVERGENIE_HOME="+home)
		out, err := cmd.CombinedOutput()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return string(out), ee.ExitCode()
			}
			return string(out) + err.Error(), 1
		}
		return string(out), 0
	}
	rescueOpen = func(ctx context.Context, args ...string) {
		if sdk.Getenv("EVERGENIE_RESCUE_OPEN") == "0" { // 安裝測試、自動化：不真的開視窗
			return
		}
		_ = exec.CommandContext(ctx, "open", args...).Run()
	}
	rescueBoardUp = func(ctx context.Context, url string) bool {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return false
		}
		res, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
		if err != nil {
			return false
		}
		_ = res.Body.Close()
		return res.StatusCode < 500
	}
	rescueLogin = func(ctx context.Context, argv []string, in io.Reader, out io.Writer) error {
		env := os.Environ()
		for len(argv) > 0 && rescueEnvWord.MatchString(argv[0]) {
			env, argv = append(env, argv[0]), argv[1:]
		}
		if len(argv) == 0 {
			return fmt.Errorf("沒有登入指令")
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, in, out, out
		return cmd.Run()
	}
	// 只重開這個家自己的服務（跟 `evergenie service restart` 同一條路）。
	rescueRestart = func(ctx context.Context, home string) error {
		exe, err := os.Executable()
		if err != nil {
			return platform.Unavailable("找不到 EverGenie 執行檔")
		}
		svc := host.Service{Runner: hostRunner, UID: os.Getuid(), Spec: host.ServiceSpec{Label: host.HomeServiceLabel(home), Exe: exe, Home: home}}
		_, err = svc.Restart(ctx)
		return err
	}
	// 這個家的看板在這台上的網址；讀不到設定回空字串。
	rescueBoard = func(home string) string {
		if h, err := platform.LoadHome(home); err == nil && h.Web.Port > 0 {
			return "http://127.0.0.1:" + strconv.Itoa(h.Web.Port) + "/"
		}
		return ""
	}
	rescueWait   = 60 * time.Second
	rescueResult = func() string {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, "Library", "Logs", platform.ProductName)
	}
)

// 登入指令前面的「名稱=值」才當環境變數（名稱是英數字與底線）；路徑裡剛好有等號的不算。
var rescueEnvWord = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

func rescueCommands() map[string]cmdline.Command {
	engines := func(e *cmdline.Env, r *cmdline.HostRequest) {
		if e.NoSocket {
			return
		}
		r.Engines = func() ([]sdk.PreflightCheck, bool) {
			res, err := sdk.NewSystemClient(e.Client).Preflight(e.Ctx, &sdk.SystemPreflightRequest{})
			if err != nil {
				return nil, false
			}
			return res.Checks, true
		}
	}
	return map[string]cmdline.Command{
		"rescue":         cmdline.HostCommand("rescue", rescueCommand, engines).With("rescue [--ask]   出問題時的修復：健康檢查、重開服務、再檢查，把結果開給你看（--ask 再問要不要重新登入 AI 帳號）；「"+platform.RescueName+"」點兩下跑的就是它\nrescue install [--home <家>] [--exe <evergenie>] [--dir <資料夾>]   在「應用程式」資料夾放可以點兩下的「"+platform.RescueName+"」（安裝程式會叫）", cmdline.GroupSystem),
		"rescue install": cmdline.HostCommand("rescue install", rescueInstall, nil),
	}
}

func rescueCommand(ctx context.Context, r cmdline.HostRequest) int {
	_, on, rest, ok := flags(r.Args, nil, []string{"ask"})
	if !ok || len(rest) > 0 {
		fmt.Fprintln(r.Err, "用法："+rescueUsage)
		return 2
	}
	home, err := resolveHome(nil, r.Home)
	if err != nil {
		return fail(r.Err, err)
	}
	say := func(format string, a ...any) { fmt.Fprintf(r.Out, format+"\n", a...) }
	say("%s：檢查 → 重開服務 → 再檢查。大約一分鐘，這個視窗先不要關。", platform.RescueName)
	say("")
	say("1/3 檢查現在的狀況…")
	before, _ := rescueSelf(ctx, home, "doctor")
	say("    %s", rescueSummary(before))

	say("2/3 重開 %s 的服務…", platform.ProductName)
	board := rescueBoard(home)
	restartNote := ""
	if err := rescueRestart(ctx, home); err != nil {
		restartNote = errText(err)
	}
	if restartNote != "" {
		say("    重開沒成功：%s", restartNote)
	}
	up := false
	if board != "" {
		deadline := time.Now().Add(rescueWait)
		for {
			if up = rescueBoardUp(ctx, board); up || !time.Now().Before(deadline) || ctx.Err() != nil {
				break
			}
			time.Sleep(time.Second)
		}
	}
	if up {
		say("    看板開得起來了。")
	} else {
		say("    看板還是沒有回應。")
	}

	say("3/3 再檢查一次…")
	after, code := rescueSelf(ctx, home, "doctor")
	say("    %s", rescueSummary(after))
	say("")

	// 還原卡住了（服務因為 .restore/ 的狀態拒絕啟動，HBK-7）：重開幾次都一樣，而且這時候動家裡的資料夾會讓它換不回去。
	stuck := !up && homebackup.Stuck(home)
	file, share := rescueWriteResult(ctx, home, up, stuck, restartNote, after, time.Now())
	switch {
	case stuck:
		say("還原卡住了：上次從備份還原換到一半，沒辦法自己換回去，所以服務先不啟動。")
		say("先不要動家裡的資料夾（不要搬、不要刪、不要改名），也不用一直重開；你的資料都還在這台電腦上。")
		if share != "" {
			say("把這個檔傳給提供程式的人，請他帶你處理（幫你打開了）：")
			say("    %s", share)
			say("（裡面已盡量拿掉私人內容，送出前請自己看一遍；系統不會自己送出。）")
			rescueOpen(ctx, "-t", share)
		} else {
			say("請告訴提供程式的人「還原卡住了，%s說先不要動家裡的資料夾」，請他帶你處理。", platform.RescueName)
		}
	case up && code == 0:
		say("都正常了。幫你打開看板。")
		rescueOpen(ctx, board)
	case up:
		say("看板可以用了，還有幾項要看：到看板「設定 → 系統健康」，或跟任何一位秘書說「幫我看系統健康的問題」。幫你打開看板。")
		rescueOpen(ctx, board)
	default:
		say("還是開不起來。先等一兩分鐘再點一次「%s」。", platform.RescueName)
		// 只叫他傳遮過的那一份（另一個檔）；健檢全文有這台的路徑與帳號，留在這台，不開給他、也不叫他傳。
		if share != "" {
			say("還是不行，把這個檔傳給提供程式的人（幫你打開了）：")
			say("    %s", share)
			say("（裡面已盡量拿掉私人內容，送出前請自己看一遍；要不要傳由你決定，系統不會自己送出。）")
			rescueOpen(ctx, "-t", share)
		} else {
			say("還是不行，請告訴提供程式的人「%s跑過了，看板還是打不開」。", platform.RescueName)
		}
	}
	if file != "" {
		say("這次的檢查結果留在這台電腦上（給秘書看的，不用傳出去）：%s", file)
	}
	if on["ask"] && up && r.Engines != nil {
		rescueAskLogin(ctx, r)
	}
	say("")
	say("可以關掉這個視窗了。")
	if !up {
		return 1
	}
	return 0
}

// rescueSummary 取 doctor 輸出的最後一行（「N 項正常、N 項注意、N 項要處理」）；沒有就說沒查到。
func rescueSummary(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if strings.Contains(last, "項正常") {
		return last
	}
	return "沒能完成檢查"
}

// rescueWriteResult 把這次的結果寫成兩個檔（都在 ~/Library/Logs/<產品名>/），回兩個路徑；寫不了的那個回空字串：
//   - 修復結果-<時間>.txt：留在這台看的（健檢全文，會有這台的路徑與帳號）；不叫主人傳出去。
//   - 可以傳的回報-<時間>.txt：只有 `evergenie report --detail --out` 遮過的那一份（由 report 自己寫檔，
//     子行程印在畫面上的其他字不會混進來）。主人要傳給別人的只有這個。
//
// 順手刪掉 30 天前的舊檔（只認這兩種檔名）。
func rescueWriteResult(ctx context.Context, home string, up, stuck bool, restartNote, doctor string, now time.Time) (local, share string) {
	dir := rescueResult()
	if dir == "" || os.MkdirAll(dir, 0o700) != nil {
		return "", ""
	}
	for _, pat := range []string{"修復結果-*.txt", "可以傳的回報-*.txt"} {
		old, _ := filepath.Glob(filepath.Join(dir, pat))
		for _, f := range old {
			if st, err := os.Stat(f); err == nil && now.Sub(st.ModTime()) > 30*24*time.Hour {
				_ = os.Remove(f)
			}
		}
	}
	stamp := now.Format("20060102-150405")
	var b strings.Builder
	fmt.Fprintf(&b, "%s 的結果（%s）\n\n這個檔只留在這台電腦上看，裡面有這台的路徑與帳號名稱，不要傳出去。\n\n", platform.RescueName, now.Format("2006-01-02 15:04"))
	if up {
		b.WriteString("看板：開得起來\n")
	} else {
		b.WriteString("看板：沒有回應\n")
	}
	if restartNote != "" {
		b.WriteString("重開服務：沒成功（" + restartNote + "）\n")
	} else {
		b.WriteString("重開服務：做了\n")
	}
	if stuck {
		b.WriteString("還原：卡住了（換到一半、換不回去；服務因此不啟動。不要動家裡的資料夾）\n")
	}
	b.WriteString("\n===== 健康檢查 =====\n\n" + strings.TrimSpace(doctor) + "\n")
	local = filepath.Join(dir, "修復結果-"+stamp+".txt")
	if os.WriteFile(local, []byte(b.String()), 0o600) != nil {
		local = ""
	}
	share = filepath.Join(dir, "可以傳的回報-"+stamp+".txt")
	_, code := rescueSelf(ctx, home, "report", "--detail", "--out", share)
	if st, err := os.Stat(share); code != 0 || err != nil || st.Size() == 0 {
		_ = os.Remove(share)
		share = ""
	} else if stuck {
		// 收到的人要知道是這個狀況（這一句沒有私人內容）。
		if f, err := os.OpenFile(share, os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
			_, _ = f.WriteString("\n## 還原\n還原卡住了：換到一半、換不回去，服務因此不啟動（家裡的 .restore/ 還在）。\n")
			_ = f.Close()
		}
	}
	return local, share
}

// rescueAskLogin：列出引擎說「可以在這裡登入」的帳號（PreflightCheck.Login），主人選了才跑；跑的是引擎自己的登入畫面，
// 帳密只在那個畫面與瀏覽器裡，EverGenie 不經手。
func rescueAskLogin(ctx context.Context, r cmdline.HostRequest) {
	checks, ok := r.Engines()
	if !ok {
		return
	}
	type acct struct {
		name  string
		ok    bool
		login []string
		hint  []string
	}
	var accts []acct
	for _, c := range checks {
		if id, found := strings.CutPrefix(c.ID, "login:"); found && len(c.Login) > 0 {
			accts = append(accts, acct{id, c.Ok, c.Login, c.LoginHint})
		}
	}
	if len(accts) == 0 || r.In == nil {
		return
	}
	in := bufio.NewReader(r.In)
	fmt.Fprintln(r.Out)
	fmt.Fprintln(r.Out, "要重新登入 AI 帳號嗎？（收到「AI 帳號要重新登入」的通知才需要）")
	for i, a := range accts {
		state := "看起來有登入"
		if !a.ok {
			state = "沒有登入"
		}
		fmt.Fprintf(r.Out, "  %d）重新登入 AI 帳號 %s（%s）\n", i+1, a.name, state)
	}
	fmt.Fprint(r.Out, "要的話打數字再按 Enter；不用就直接按 Enter：")
	line, _ := in.ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(accts) {
		return
	}
	fmt.Fprintln(r.Out)
	fmt.Fprintln(r.Out, "等一下會出現這個 AI 帳號自己的畫面：")
	for _, l := range accts[n-1].hint {
		fmt.Fprintln(r.Out, "  "+l)
	}
	if len(accts[n-1].hint) == 0 {
		fmt.Fprintln(r.Out, "  照畫面登入；登入好之後離開那個畫面，就會回到這裡。")
	}
	fmt.Fprint(r.Out, "準備好就按 Enter：")
	_, _ = in.ReadString('\n')
	if err := rescueLogin(ctx, accts[n-1].login, r.In, r.Out); err != nil {
		fmt.Fprintln(r.Out, "登入的畫面沒有正常結束；再點一次「"+platform.RescueName+"」重試。")
		return
	}
	fmt.Fprintln(r.Out, "好了。回看板再說一次話，秘書就會接著做。")
}

// rescueInstall 在 --dir（預設 ~/Applications）放「<產品名> 修復.command」：點兩下會開終端機視窗跑 `evergenie rescue --ask`。
// 放「應用程式」不放桌面：寫桌面會跳 macOS 的權限視窗。同名的檔是別的家的，就在檔名加上這個家的資料夾名，不蓋掉別人的。
func rescueInstall(_ context.Context, r cmdline.HostRequest) int {
	vals, _, rest, ok := flags(r.Args, []string{"home", "exe", "dir"}, nil)
	if !ok || len(rest) > 0 {
		fmt.Fprintln(r.Err, "用法："+rescueUsage)
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
	path, err := rescuePlace(home, exe, vals["dir"])
	if err != nil {
		return fail(r.Err, err)
	}
	fmt.Fprintln(r.Out, path)
	return 0
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func errText(err error) string {
	var pe *platform.Err
	if errors.As(err, &pe) && pe.Message != "" {
		return pe.Message
	}
	return err.Error()
}

// rescuePlace 放（或更新）這個家的修復入口，回檔案路徑。dir 空的就是 EVERGENIE_RESCUE_DIR 或 ~/Applications。
// 檔名：沒人用就是「<產品名> 修復.command」；被別的家用了就加上這個家的資料夾名；再撞就加編號。同一個家重放會覆寫自己那個。
func rescuePlace(home, exe, dir string) (string, error) {
	if dir == "" {
		dir = sdk.Getenv("EVERGENIE_RESCUE_DIR")
	}
	if dir == "" {
		uh, err := os.UserHomeDir()
		if err != nil {
			return "", platform.Unavailable("找不到使用者的家目錄")
		}
		dir = filepath.Join(uh, "Applications")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", platform.Unavailable("建不了資料夾 " + dir)
	}
	homeLine := "export EVERGENIE_HOME=" + shQuote(home)
	names := []string{platform.RescueFile, platform.RescueName + "（" + filepath.Base(home) + "）.command"}
	for i := 2; i <= 9; i++ {
		names = append(names, platform.RescueName+"（"+filepath.Base(home)+" "+strconv.Itoa(i)+"）.command")
	}
	path := ""
	for _, name := range names {
		candidate := filepath.Join(dir, name)
		old, err := os.ReadFile(candidate)
		if os.IsNotExist(err) || (err == nil && strings.Contains(string(old), homeLine+"\n")) {
			path = candidate
			break
		}
	}
	if path == "" {
		return "", platform.Unavailable("「" + platform.RescueName + "」的名字都被別的家用了，放不進 " + dir)
	}
	body := "#!/bin/bash\n" +
		"# " + platform.RescueName + "：點兩下就會檢查、重開服務、把結果開給你看。安裝時放的；刪掉不影響使用。\n" +
		homeLine + "\n" +
		"EXE=" + shQuote(exe) + "\n" +
		"if [ ! -x \"$EXE\" ]; then\n" +
		"  echo \"找不到 " + platform.ProductName + " 的程式（$EXE）。可能被搬走或刪掉了；請重新安裝一次（你的資料會留著）。\"\n" +
		"  exit 1\n" +
		"fi\n" +
		"exec \"$EXE\" rescue --ask\n"
	if err := platform.AtomicWrite(path, []byte(body), 0o755); err != nil {
		return "", platform.Unavailable("寫不了 " + path)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", platform.Unavailable("設不了 " + path + " 的權限")
	}
	return path, nil
}

// rescueEnsure：服務裝好或重開成功後順手補放修復入口（升級上來的家當初安裝時還沒有這個東西）。
// 只有真的裝成服務的家才放（plist 在）；測試模式（背景行程）、--root 的試裝不放。放不了不算錯。
func rescueEnsure(svc host.Service, home, exe string) {
	if host.ProcessMode() || svc.Root != "" {
		return
	}
	if _, err := os.Stat(svc.PlistPath()); err != nil {
		return
	}
	_, _ = rescuePlace(home, exe, "")
}
