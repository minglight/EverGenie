package host

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// DefaultLabel 是 serve 的 launchd 服務名稱。
const DefaultLabel = "com." + platform.ProductName + ".serve"

// ServiceSpec 是 serve 的 launchd 設定（`service install` 產生 plist 用）。
type ServiceSpec struct {
	Label string
	Exe   string // EverGenie 執行檔的絕對路徑
	Home  string // 家（--home）
	Path  string // 服務的 PATH；空＝常見的 Homebrew 與系統位置
	Lang  string
}

// HomeServiceLabel 是這個家的 launchd 服務名稱：home.toml [runtime] service_label（同一台跑兩個家時各設各的，GOAL-2 W2）；
// 沒寫或讀不到設定＝空字串（用預設 DefaultLabel）。`service` 指令、doctor 與升級重開服務都用它。
func HomeServiceLabel(home string) string {
	if home == "" {
		return ""
	}
	h, err := platform.LoadHome(home)
	if err != nil {
		return ""
	}
	return h.Runtime.ServiceLabel
}

func (s ServiceSpec) label() string {
	if s.Label == "" {
		return DefaultLabel
	}
	return s.Label
}

func (s ServiceSpec) path() string {
	if s.Path != "" {
		return s.Path
	}
	home, _ := os.UserHomeDir()
	return strings.Join([]string{filepath.Join(home, ".local", "bin"), "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}, ":")
}

// RenderPlist 產生 launchd 的 plist：開機（登入）就跑、掛了重開；日誌在家的 runtime/logs/。
// 引擎套件的執行檔預設放在 EverGenie 旁邊（EVERGENIE_ENGINE_BIN_DIR）。
func RenderPlist(s ServiceSpec) string {
	lang := s.Lang
	if lang == "" {
		lang = "en_US.UTF-8"
	}
	e := html.EscapeString
	logs := platform.RuntimeDir(s.Home, "logs")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <!-- 自動產生（evergenie service install），要改請重跑那個指令 -->
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
    <string>--home</string>
    <string>%s</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>WorkingDirectory</key><string>%s</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key><string>%s</string>
    <key>LANG</key><string>%s</string>
    <key>EVERGENIE_ENGINE_BIN_DIR</key><string>%s</string>
  </dict>
  <key>StandardOutPath</key><string>%s</string>
  <key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, e(s.label()), e(s.Exe), e(s.Home), e(s.Home), e(s.path()), e(lang), e(filepath.Dir(s.Exe)), e(filepath.Join(logs, "serve.out.log")), e(filepath.Join(logs, "serve.err.log")))
}

// Service 管 serve 的 launchd 服務。Root 不是空的（--root，測試與試跑用）時，plist 放 <Root>/Library/LaunchAgents，而且不呼叫 launchctl。
type Service struct {
	Runner Runner
	Spec   ServiceSpec
	Dir    string // LaunchAgents 資料夾；空＝~/Library/LaunchAgents
	Root   string
	UID    int
}

// LaunchAgentsDir 是放 plist 的資料夾：環境變數 EVERGENIE_LAUNCH_AGENTS_DIR（測試用）、其次 ~/Library/LaunchAgents。
func LaunchAgentsDir() string {
	if d := sdk.Getenv("EVERGENIE_LAUNCH_AGENTS_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents")
}

func (s Service) dir() string {
	switch {
	case s.Root != "":
		return filepath.Join(s.Root, "Library", "LaunchAgents")
	case s.Dir != "":
		return s.Dir
	}
	return LaunchAgentsDir()
}

// PlistPath 是這個服務的 plist 位置。
func (s Service) PlistPath() string { return filepath.Join(s.dir(), s.Spec.label()+".plist") }

func (s Service) domain() string { return "gui/" + strconv.Itoa(s.UID) }
func (s Service) target() string { return s.domain() + "/" + s.Spec.label() }
func (s Service) dry() bool      { return s.Root != "" }

func (s Service) launchctl(ctx context.Context, args ...string) (string, int) {
	if s.dry() {
		return "", 0
	}
	return s.Runner.Run(ctx, "launchctl", args...)
}

// Loaded 問 launchd 這個服務載入了沒有、有沒有在跑（pid）。
func (s Service) Loaded(ctx context.Context) (loaded, running bool, pid int) {
	if ProcessMode() {
		st := s.processStatus()
		return st.Loaded, st.Running, st.PID
	}
	if s.dry() {
		return false, false, 0
	}
	out, code := s.Runner.Run(ctx, "launchctl", "print", s.target())
	if code != 0 {
		return false, false, 0
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "pid = "); ok {
			pid, _ = strconv.Atoi(strings.TrimSpace(v))
		}
		if v, ok := strings.CutPrefix(line, "state = "); ok && strings.TrimSpace(v) == "running" {
			running = true
		}
	}
	return true, running, pid
}

// Install 寫 plist 並載入（已經載入的先卸掉再載入，所以可以重跑來更新）。家必須有 home.toml，執行檔必須存在。
func (s Service) Install(ctx context.Context) ([]string, error) {
	if ProcessMode() {
		return s.processInstall(ctx)
	}
	if s.Spec.Home == "" || !filepath.IsAbs(s.Spec.Home) {
		return nil, platform.Invalid("home", "請給家的絕對路徑（--home）")
	}
	if _, err := platform.LoadHome(s.Spec.Home); err != nil {
		return nil, err
	}
	if info, err := os.Stat(s.Spec.Exe); err != nil || info.IsDir() {
		return nil, platform.Invalid("exe", "找不到 EverGenie 執行檔 "+s.Spec.Exe+"（用 --exe 指定）")
	}
	if err := os.MkdirAll(platform.RuntimeDir(s.Spec.Home, "logs"), 0o700); err != nil {
		return nil, platform.Unavailable("建不了日誌資料夾")
	}
	if err := os.MkdirAll(s.dir(), 0o755); err != nil {
		return nil, platform.Unavailable("建不了 " + s.dir())
	}
	lines := []string{}
	plist := s.PlistPath()
	if err := platform.AtomicWrite(plist, []byte(RenderPlist(s.Spec)), 0o644); err != nil {
		return nil, platform.Unavailable("寫不了 " + plist)
	}
	lines = append(lines, "已寫入 "+plist)
	if s.dry() {
		return append(lines, "（--root 模式：沒有呼叫 launchctl）"), nil
	}
	if loaded, _, _ := s.Loaded(ctx); loaded {
		if out, code := s.launchctl(ctx, "bootout", s.target()); code != 0 {
			return lines, platform.Unavailable("卸不掉舊的服務：" + oneLine(out))
		}
	}
	if out, code := s.launchctl(ctx, "bootstrap", s.domain(), plist); code != 0 {
		return lines, platform.Unavailable("載入服務失敗：" + oneLine(out))
	}
	return append(lines, "已載入 "+s.Spec.label()+"（登入就會啟動，掛了會自動重開）"), nil
}

// Uninstall 卸載並移除 plist（家的資料不動）。
func (s Service) Uninstall(ctx context.Context) ([]string, error) {
	if ProcessMode() {
		return s.processStop()
	}
	var lines []string
	if loaded, _, _ := s.Loaded(ctx); loaded {
		if out, code := s.launchctl(ctx, "bootout", s.target()); code != 0 {
			return nil, platform.Unavailable("卸不掉服務：" + oneLine(out))
		}
		lines = append(lines, "已卸載 "+s.Spec.label())
	}
	switch err := os.Remove(s.PlistPath()); {
	case err == nil:
		lines = append(lines, "已移除 "+s.PlistPath())
	case os.IsNotExist(err):
		lines = append(lines, "本來就沒有安裝")
	default:
		return lines, platform.Unavailable("移不掉 " + s.PlistPath())
	}
	return lines, nil
}

func (s Service) installed() error {
	if _, err := os.Stat(s.PlistPath()); err != nil {
		return platform.NotFound("還沒安裝服務：先 evergenie service install")
	}
	return nil
}

// Start 載入（沒載入的）並啟動。
func (s Service) Start(ctx context.Context) ([]string, error) {
	if ProcessMode() {
		return s.processStart()
	}
	if err := s.installed(); err != nil {
		return nil, err
	}
	if loaded, running, _ := s.Loaded(ctx); loaded {
		if running {
			return []string{"已經在跑了"}, nil
		}
		if out, code := s.launchctl(ctx, "kickstart", s.target()); code != 0 {
			return nil, platform.Unavailable("啟動失敗：" + oneLine(out))
		}
		return []string{"已啟動"}, nil
	}
	if out, code := s.launchctl(ctx, "bootstrap", s.domain(), s.PlistPath()); code != 0 {
		return nil, platform.Unavailable("載入服務失敗：" + oneLine(out))
	}
	return []string{"已載入並啟動"}, nil
}

// Stop 卸載（plist 留著；要再開用 start）。launchd 的 KeepAlive 會把只是「殺掉」的服務重開，所以停止就是卸載。
func (s Service) Stop(ctx context.Context) ([]string, error) {
	if ProcessMode() {
		return s.processStop()
	}
	if err := s.installed(); err != nil {
		return nil, err
	}
	if loaded, _, _ := s.Loaded(ctx); !loaded {
		return []string{"本來就沒有在跑"}, nil
	}
	if out, code := s.launchctl(ctx, "bootout", s.target()); code != 0 {
		return nil, platform.Unavailable("停止失敗：" + oneLine(out))
	}
	return []string{"已停止（要再開：evergenie service start）"}, nil
}

// Restart 重開（沒載入就等於 start）。
func (s Service) Restart(ctx context.Context) ([]string, error) {
	if ProcessMode() {
		if _, err := s.processStop(); err != nil {
			return nil, err
		}
		if _, err := s.processStart(); err != nil {
			return nil, err
		}
		return []string{"已重開"}, nil
	}
	if err := s.installed(); err != nil {
		return nil, err
	}
	if loaded, _, _ := s.Loaded(ctx); loaded {
		if out, code := s.launchctl(ctx, "kickstart", "-k", s.target()); code != 0 {
			return nil, platform.Unavailable("重開失敗：" + oneLine(out))
		}
		return []string{"已重開"}, nil
	}
	return s.Start(ctx)
}

// ServiceStatus 是 `service status` 的結果。
type ServiceStatus struct {
	Label     string
	Plist     string
	Installed bool
	Loaded    bool
	Running   bool
	PID       int
	// PlistHome 是 plist 裡 --home 指的家；跟你現在要管的家不同時，doctor 會提醒。
	PlistHome string
	// Process：背景行程模式（EVERGENIE_SERVICE_MODE=process，只給測試用）；Plist 那欄放的是 pid 檔。
	Process bool
}

// Status 讀現況（只讀）。
func (s Service) Status(ctx context.Context) ServiceStatus {
	if ProcessMode() {
		return s.processStatus()
	}
	st := ServiceStatus{Label: s.Spec.label(), Plist: s.PlistPath()}
	data, err := os.ReadFile(st.Plist)
	if err != nil {
		return st
	}
	st.Installed = true
	st.PlistHome = homeOf(string(data))
	st.Loaded, st.Running, st.PID = s.Loaded(ctx)
	return st
}

// homeOf 從 plist 文字找出 --home 後面那個字串。
func homeOf(plist string) string {
	i := strings.Index(plist, "<string>--home</string>")
	if i < 0 {
		return ""
	}
	rest := plist[i+len("<string>--home</string>"):]
	a, b := strings.Index(rest, "<string>"), strings.Index(rest, "</string>")
	if a < 0 || b < a {
		return ""
	}
	return html.UnescapeString(rest[a+len("<string>") : b])
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
