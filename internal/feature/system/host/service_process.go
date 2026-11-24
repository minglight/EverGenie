package host

import (
	"context"
	"fmt"
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

// 服務的「背景行程」模式（P8 第二輪 S2、REQ-280）：環境變數 EVERGENIE_SERVICE_MODE=process 時不碰 launchd，
// serve 以脫離終端機的背景行程跑，pid 寫在 <家>/runtime/serve.pid、日誌附加在 <家>/runtime/logs/serve.log。
// **只給測試與 `make release-e2e` 用**（在暫存資料夾真的安裝、升級、退版，卻不碰這台電腦的 launchd）；正式安裝一律走 launchd。

// ServiceModeEnv 是切換服務模式的環境變數；值 "process" 才是背景行程模式，其他（含沒設）都是 launchd。
const ServiceModeEnv = "EVERGENIE_SERVICE_MODE"

// ProcessMode 回現在是不是背景行程模式。
func ProcessMode() bool { return sdk.Getenv(ServiceModeEnv) == "process" }

// processStopWait：SIGTERM 之後最多等多久才改用 SIGKILL。測試可以縮短。
var processStopWait = 10 * time.Second

func (s Service) pidPath() string { return platform.RuntimeDir(s.Spec.Home, "serve.pid") }
func (s Service) serveLogPath() string {
	return platform.RuntimeDir(s.Spec.Home, "logs", "serve.log")
}

// PidAlive 回這個 pid 的行程還在不在（升級的鎖檔用）。
func PidAlive(pid int) bool { return pidAlive(pid) }

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// readPID 讀 pid 檔；沒有或讀不懂回 0。
func (s Service) readPID() int {
	data, err := os.ReadFile(s.pidPath())
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	return pid
}

// processEnv 是背景 serve 的環境：繼承目前的環境，但拿掉屬於「別的家／別的 session」的變數（EVERGENIE_HOME 等），避免 serve 連到不對的家。
// EVERGENIE_ENGINE_BIN_DIR 沒設就用執行檔旁邊（跟 launchd 的 plist 一樣）。EVERGENIE_OFFICIAL_PACKAGES_DIR 等照原樣帶過去。
func (s Service) processEnv() []string {
	drop := map[string]bool{"EVERGENIE_HOME": true, "EVERGENIE_API_SOCKET": true, "EVERGENIE_AGENT": true, "EVERGENIE_SESSION": true, "EVERGENIE_LAUNCH": true, "EVERGENIE_ENV_FILE": true, "EVERGENIE_ENGINE_SOCKET": true,
		"EVERGENIE_STATE": true, "EVERGENIE_MEDIA": true, "EVERGENIE_OFFICIAL_PERSONAS": true, // 佈局（DEV-145）由 serve 自己照 home.toml 算
		"EVERGENIE_BUNDLED_PERSONAS": true} // 跟著程式的公開卡資料夾由 serve 開套件時自己帶（PRS-25）
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		name := sdk.CurrentEnvName(k) // 改名前的舊名跟新名字一樣處理
		if drop[name] || name == "EVERGENIE_ENGINE_BIN_DIR" {
			continue
		}
		env = append(env, kv)
	}
	// 引擎資料夾只用新名帶一筆：照 sdk.Getenv 的規則取值（新名設了就用新名，空的也算），空的就用執行檔旁邊
	bin := sdk.Getenv("EVERGENIE_ENGINE_BIN_DIR")
	if bin == "" {
		bin = filepath.Dir(s.Spec.Exe)
	}
	return append(env, "EVERGENIE_ENGINE_BIN_DIR="+bin)
}

func (s Service) processValidate() error {
	if s.Spec.Home == "" || !filepath.IsAbs(s.Spec.Home) {
		return platform.Invalid("home", "請給家的絕對路徑（--home）")
	}
	if _, err := platform.LoadHome(s.Spec.Home); err != nil {
		return err
	}
	if info, err := os.Stat(s.Spec.Exe); err != nil || info.IsDir() {
		return platform.Invalid("exe", "找不到 EverGenie 執行檔 "+s.Spec.Exe+"（用 --exe 指定）")
	}
	return nil
}

func (s Service) processStart() ([]string, error) {
	if pid := s.readPID(); pidAlive(pid) {
		return []string{"已經在跑了（pid " + strconv.Itoa(pid) + "）"}, nil
	}
	if err := s.processValidate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.serveLogPath()), 0o700); err != nil {
		return nil, platform.Unavailable("建不了日誌資料夾")
	}
	logf, err := os.OpenFile(s.serveLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, platform.Unavailable("開不了日誌 " + s.serveLogPath())
	}
	defer logf.Close()
	cmd := exec.Command(s.Spec.Exe, "serve", "-home", s.Spec.Home)
	cmd.Dir = s.Spec.Home
	cmd.Env = s.processEnv()
	cmd.Stdin = nil
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, platform.Unavailable("啟動 serve 失敗：" + oneLine(err.Error()))
	}
	// 在背景收屍：同一個行程（測試）接著問「還活著嗎」時，結束的 serve 才不會變成殭屍一直算活著。
	go func() { _ = cmd.Wait() }()
	if err := platform.AtomicWrite(s.pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o600); err != nil {
		_ = cmd.Process.Kill()
		return nil, platform.Unavailable("寫不了 " + s.pidPath())
	}
	return []string{fmt.Sprintf("已在背景啟動 serve（pid %d，日誌 %s）", cmd.Process.Pid, s.serveLogPath())}, nil
}

func (s Service) processStop() ([]string, error) {
	pid := s.readPID()
	if !pidAlive(pid) {
		_ = os.Remove(s.pidPath())
		return []string{"本來就沒有在跑"}, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return nil, platform.Unavailable("停止失敗：" + oneLine(err.Error()))
	}
	deadline := time.Now().Add(processStopWait)
	for pidAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if pidAlive(pid) {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		for i := 0; i < 40 && pidAlive(pid); i++ {
			time.Sleep(50 * time.Millisecond)
		}
		if pidAlive(pid) {
			return nil, platform.Unavailable("serve 停不下來（pid " + strconv.Itoa(pid) + "）")
		}
	}
	_ = os.Remove(s.pidPath())
	return []string{"已停止"}, nil
}

func (s Service) processStatus() ServiceStatus {
	st := ServiceStatus{Label: s.Spec.label() + "（背景行程模式）", Plist: s.pidPath(), PlistHome: s.Spec.Home, Process: true}
	pid := s.readPID()
	if pid == 0 {
		return st
	}
	st.Installed = true
	if pidAlive(pid) {
		st.Loaded, st.Running, st.PID = true, true, pid
	}
	return st
}

// processInstall：背景行程模式的「安裝」＝（重新）啟動。
func (s Service) processInstall(ctx context.Context) ([]string, error) {
	if err := s.processValidate(); err != nil {
		return nil, err
	}
	lines, err := s.processStop()
	if err != nil {
		return lines, err
	}
	out, err := s.processStart()
	return append(lines, out...), err
}
