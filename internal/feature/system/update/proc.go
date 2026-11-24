package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 升級行程要用的輔助：組 Upgrader、脫離終端機叫起升級、重開之後的檢查、發通知。
// 從 cmd/evergenie/upgradecmd.go 搬來（2026-10-31，業務模組 update）。

func channelName(c string) string {
	switch c {
	case platform.UpdateChannelSafe:
		return "也升測試過的版本"
	case platform.UpdateChannelOff:
		return "不自動檢查"
	}
	return "只升正式版"
}

// newUpgrader 組出升級用的物件：重開用 Service、檢查用 verifyUpgrade、通知與收尾接到這個家。
// core 是 FindInstall 找到的地方：原始碼版的 repo，或下載版的安裝資料夾（服務跑 current/bin/evergenie）。
func newUpgrader(h *platform.Home, core string, out io.Writer) *Upgrader {
	download := IsDownloadInstall(core)
	exe := filepath.Join(core, "bin", platform.CoreExe)
	if download {
		exe = filepath.Join(core, currentLinkName, "bin", platform.CoreExe)
	}
	var known map[string]bool // 升級前就紅的健康檢查項目（Baseline 填）
	return &Upgrader{
		Baseline: func(ctx context.Context) { known = doctorReds(ctx, h.Dir, exe) },
		Home:     h.Dir, Core: core, From: platform.Version(), Channel: h.UpdateChannel(), Out: out,
		Download: download, Source: h.UpdateSource(),
		Verify: func(ctx context.Context, want string) error { return verifyUpgrade(ctx, h.Dir, exe, want, known) },
		Notify: func(title, detail string) { notifyUpgrade(h.Dir, title, detail) },
		Report: func(ctx context.Context, r UpgradeReport) { sendUpgradeReport(ctx, h.Dir, exe, r) },
		AfterSuccess: func(ctx context.Context) {
			// 同步這一版的角色包（S4 的 personas pack sync）；失敗或這個版本還沒有這個指令都不算升級失敗。
			out, code := runIn(ctx, 3*time.Minute, h.Dir, exe, "personas", "pack", "sync")
			switch {
			case code == 0:
			case strings.Contains(out, "不認得的指令"):
			default:
				fmt.Fprintf(os.Stderr, "（角色包還沒同步：%s；之後會再試）\n", firstLine(out))
			}
		},
	}
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return first
}

func runIn(ctx context.Context, timeout time.Duration, home, name string, args ...string) (string, int) {
	call, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(call, name, args...)
	cmd.Env = append(os.Environ(), "EVERGENIE_HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		return string(out) + err.Error(), 127
	}
	return string(out), 0
}

// notifyUpgrade 發一則資訊類通知（done），連到「設定 → 系統健康」；serve 不通就算了（升級的結果以 state.json 為準）。
// notifyUpgrade 發升級的通知。點下去：沒成功的（state 有 outcome）到看板首頁（升級結果橫幅有「再試一次」「退回上一版」，WEB-173）；
// 成功的到系統健康的版本與更新卡。
func notifyUpgrade(home, title, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := sdk.Dial(platform.RuntimeDir(home, "sockets", "api.sock"))
	_, _ = sdk.NewNotifyClient(c).Send(ctx, &sdk.NotifySendRequest{Category: sdk.CategoryDone, Title: title, Context: detail, URL: notifyURL(home), Lane: sdk.NotifyLaneSystem}) // 自動報告，系統那道（D-47）
}

func notifyURL(home string) string {
	if st, _ := ReadUpgradeState(home); st != nil && st.Outcome != "" {
		return "/"
	}
	return "/settings/health"
}

// startDetached 另起一個脫離終端機的升級行程（Setsid），日誌附加在 runtime/logs/upgrade.log，不等它做完。
func startDetached(home, exe, to string) (logPath string, err error) {
	args := []string{"upgrade", "--apply", "--home", home}
	if to != "" {
		args = append(args, "--to", to)
	}
	return startDetachedArgs(home, exe, args...)
}

// startDetachedRollback 另起一個脫離終端機的 `upgrade --rollback --rescue`（換不回去之後再試一次換回，rescue.go）：
// 只照 rescue.json 做，紀錄不在了就不動（不改做一般的退回）。
func startDetachedRollback(home, exe string) (string, error) {
	return startDetachedArgs(home, exe, "upgrade", "--rollback", "--rescue", "--home", home)
}

func startDetachedArgs(home, exe string, args ...string) (logPath string, err error) {
	logDir := platform.RuntimeDir(home, "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		return "", platform.Unavailable("建不了日誌資料夾 " + logDir)
	}
	logPath = filepath.Join(logDir, "upgrade.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", platform.Unavailable("開不了升級日誌")
	}
	defer logf.Close()
	cmd := exec.Command(exe, args...)
	cmd.Env = os.Environ()
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(logf, "%s [detail] start %s: %v\n", time.Now().Format(time.RFC3339), exe, err)
		return "", platform.Unavailable("叫不起升級的程式（細節記在升級日誌）")
	}
	_ = cmd.Process.Release()
	return logPath, nil
}

// ---- 重開之後的檢查 ----------------------------------------------------------------------------------------

// verifyWait 是重開之後最多等多久讓服務起來；測試可以縮短。
var verifyWait = 60 * time.Second

// 重開回來時舊程式可能還在回 doctor（核心版本還是舊的），等到版本對上或時間到才下結論；
// 只看第一次回應會把好好的新版誤判成壞的、白白退回（2026-11-03 部署工具踩過，scripts/deploy.sh 同一條規則）。
// redKey 是一條紅字的身分：代號加內容。內容變了（例如壞檔從 1 個變成 50 個）就算新的問題。
func redKey(c sdk.DoctorCheck) string { return c.ID + "\x00" + c.Detail }

// doctorReds 跑一次健康檢查，回紅字項目（redKey；服務沒起來回 nil）。升級前記下來當基準。
func doctorReds(ctx context.Context, home, exe string) map[string]bool {
	out, _ := runIn(ctx, 60*time.Second, home, exe, "doctor", "--json", "--home", home)
	var res sdk.SystemDoctorResponse
	if json.Unmarshal([]byte(strings.TrimSpace(lastJSONLine(out))), &res) != nil {
		return nil
	}
	reds := map[string]bool{}
	for _, c := range res.Checks {
		if c.Severity == sdk.DoctorSeverityError && c.ID != "serve.down" && c.ID != "serve.doctor" {
			reds[redKey(c)] = true
		}
	}
	return reds
}

// verifyUpgrade：known 是升級前就紅的項目（redKey：代號加內容都一樣才算同一條）；它們不算這次升級的錯（主人的資料本來就有的問題不擋升級）。
func verifyUpgrade(ctx context.Context, home, exe, want string, known map[string]bool) error {
	deadline := time.Now().Add(verifyWait)
	var checks []sdk.DoctorCheck
	got := ""
	for {
		out, _ := runIn(ctx, 60*time.Second, home, exe, "doctor", "--json", "--home", home)
		var res sdk.SystemDoctorResponse
		up := json.Unmarshal([]byte(strings.TrimSpace(lastJSONLine(out))), &res) == nil && len(res.Checks) > 0
		if up {
			for _, c := range res.Checks {
				if c.ID == "serve.down" || c.ID == "serve.doctor" {
					up = false
				}
			}
		}
		if up {
			checks, got = res.Checks, doctorVersion(res.Checks)
			if want == "" || got == want {
				break
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if checks != nil {
				break // 起來了但版本一直不對：下面照版本不符回報
			}
			logs := platform.RuntimeDir(home, "logs")
			return plainf("重開之後服務一直沒有起來", "service did not answer doctor within %s; see %s and %s", verifyWait, filepath.Join(logs, "serve.log"), filepath.Join(logs, "serve.err.log"))
		}
		time.Sleep(time.Second)
	}
	var reds []string
	for _, c := range checks {
		if c.Severity == sdk.DoctorSeverityError && !known[redKey(c)] {
			reds = append(reds, c.Detail)
		}
	}
	if len(reds) > 0 {
		// 健康檢查的說明是給主人看的白話（doctor），但有的夾著路徑或英文原文：只接不夾英文的那段（plainPart），整段進升級日誌。
		var plain []string
		for _, r := range reds {
			if p := plainPart(r); p != "" {
				plain = append(plain, p)
			}
		}
		msg := "健康檢查有要處理的項目"
		if len(plain) > 0 {
			msg += "：" + strings.Join(plain, "；")
		}
		return plainf(msg, "doctor reds: %s", strings.Join(reds, " | "))
	}
	if want != "" && got != want {
		return &ErrUpgrade{Message: fmt.Sprintf("跑著的核心版本是「%s」，應該是「%s」（服務可能沒重開成功）", got, want)}
	}
	if h, err := platform.LoadHome(home); err == nil {
		if err := waitBoard(ctx, h.Web.Port, 30*time.Second); err != nil {
			return err
		}
	}
	if out, code := runIn(ctx, 60*time.Second, home, exe, "packages", "list"); code != 0 || !strings.Contains(out, `"id"`) {
		return plainf("重開之後讀不到套件清單", "packages list exit %d: %s", code, strings.TrimSpace(out))
	} else {
		for _, line := range strings.Split(out, "\n") {
			// 停用中的套件帶著原因（例如「需要套件 X，但它已停用」）不算載入失敗。
			// 主人自己的、別人分享的套件壞了不是這一版的錯，不能因此把整個升級退回（DEV-154）；那些寫進升級報告
			// 交給電腦秘書處理（report.go）。只有明確標成 mine／installed 的才放過：層級看不出來的照樣算。
			var p sdk.Package
			if json.Unmarshal([]byte(strings.TrimSpace(line)), &p) != nil || p.Error == "" || !p.Enabled {
				continue
			}
			if p.Tier != sdk.TierMine && p.Tier != sdk.TierInstalled {
				name := "一個官方套件"
				if hasHan(p.Name) {
					name = "官方套件「" + p.Name + "」"
				}
				return plainf("有套件載入失敗："+name, "package %s (%s): %s", p.ID, p.Tier, p.Error)
			}
		}
	}
	return nil
}

// doctorVersion 從 doctor 的檢查裡找「核心版本：…」那一項；沒有就是空字串。
func doctorVersion(checks []sdk.DoctorCheck) string {
	for _, c := range checks {
		if after, ok := strings.CutPrefix(c.Detail, "核心版本："); ok {
			return strings.TrimSpace(after)
		}
	}
	return ""
}

func lastJSONLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "{") {
			return lines[i]
		}
	}
	return ""
}

func waitBoard(ctx context.Context, port int, wait time.Duration) error {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/"
	deadline := time.Now().Add(wait)
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		client := http.Client{Timeout: 5 * time.Second}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return plainf("重開之後看板打不開", "GET %s did not return 200 within %s", url, wait)
		}
		time.Sleep(time.Second)
	}
}
