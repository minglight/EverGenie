package claude

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 用量與額度（ECL-10，Q4）：資料來源是各帳號的狀態列寫的快照（<monitor_dir>/<名>.json，官方 rate_limits，只有最新值）。
// 快照只在有 session 時才更新，所以過期的視窗要換算：
//   - 五小時是滾動的：重置時間過了就當 0，要等下一則訊息才開新的（Expired）。
//   - 每週是固定的：重置時間過了，用量當 0，下一次重置往後推整數週（Rolled）。

const weekSeconds = 7 * 24 * 3600

// defaultMonitorDir 是沒寫 engine_config.monitor_dir 時快照所在的資料夾（~/.claude-monitor/snapshots）。
func monitorDir(cfg map[string]any) string {
	if d, _ := cfg["monitor_dir"].(string); strings.TrimSpace(d) != "" {
		return expandHome(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude-monitor", "snapshots")
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

type snapshot struct {
	UpdatedAt string `json:"updatedAt"`
	Profile   struct {
		ConfigDir string `json:"configDir"`
	} `json:"profile"`
	RateLimits struct {
		FiveHour *rateWindow `json:"fiveHour"`
		SevenDay *rateWindow `json:"sevenDay"`
	} `json:"rateLimits"`
}

type rateWindow struct {
	UsedPercentage float64 `json:"usedPercentage"`
	ResetsAt       float64 `json:"resetsAt"` // epoch 秒
}

// findSnapshot 找這個帳號資料夾的快照：先比對快照裡的 profile.configDir，再試 <帳號名>.json。
// <帳號名>.json 只在快照沒寫 configDir 時才算：寫了別的資料夾就是別的帳號（例如同一台電腦上另一個家剛好也有叫 personal 的帳號），不拿來用（SEC-373）。
func findSnapshot(dir, account, configDir string) (*snapshot, bool) {
	entries, _ := os.ReadDir(dir)
	var named *snapshot
	for _, en := range entries {
		if en.IsDir() || !strings.HasSuffix(en.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, en.Name()))
		if err != nil {
			continue
		}
		var sn snapshot
		if json.Unmarshal(data, &sn) != nil {
			continue // 寫到一半、格式不對就當沒有
		}
		if configDir != "" && filepath.Clean(sn.Profile.ConfigDir) == filepath.Clean(configDir) {
			return &sn, true
		}
		if strings.TrimSuffix(en.Name(), ".json") == account && strings.TrimSpace(sn.Profile.ConfigDir) == "" {
			c := sn
			named = &c
		}
	}
	return named, named != nil
}

// usageWindows 把快照換算成視窗（now 是現在）。
func usageWindows(sn *snapshot, now time.Time) []sdk.UsageWindow {
	var out []sdk.UsageWindow
	add := func(w *rateWindow, kind sdk.UsageWindowKind, hours int32) {
		if w == nil {
			return
		}
		uw := sdk.UsageWindow{Kind: kind, Hours: hours, UsedRatio: clamp01(w.UsedPercentage / 100)}
		reset := time.Unix(int64(w.ResetsAt), 0)
		if !now.Before(reset) {
			uw.UsedRatio = 0
			if kind == sdk.UsageWindowKindWeekly {
				n := int64(math.Floor(now.Sub(reset).Seconds()/weekSeconds)) + 1
				reset = reset.Add(time.Duration(n*weekSeconds) * time.Second)
				uw.Rolled = true
			} else {
				uw.Expired = true
			}
		}
		uw.ResetsAt = reset
		out = append(out, uw)
	}
	add(sn.RateLimits.FiveHour, sdk.UsageWindowKindShort, 5)
	add(sn.RateLimits.SevenDay, sdk.UsageWindowKindWeekly, 168)
	return out
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

// Usage 回這個帳號的用量。沒有快照（還沒跑過 session、狀態列沒寫）回空的 windows 與 MeasuredAt 零值，不當成錯誤。
// Warmup=true 先送一則極小的請求讓額度視窗從現在起算，再讀快照。
func (e *Engine) Usage(ctx context.Context, req *sdk.EngineUsageRequest) (*sdk.EngineUsageResponse, error) {
	cfg, err := parseEngineConfig(req.EngineConfig)
	if err != nil {
		return nil, err
	}
	out := &sdk.EngineUsageResponse{Account: req.Account}
	if req.Warmup {
		if err := e.warmup(ctx, cfg); err != nil {
			return nil, err
		}
		out.Warmed = true
	}
	sn, ok := findSnapshot(monitorDir(req.EngineConfig), req.Account, cfg.ConfigDir)
	if !ok {
		return out, nil
	}
	now := e.now()
	if t, err := time.Parse(time.RFC3339, sn.UpdatedAt); err == nil {
		out.MeasuredAt = t
	}
	out.Windows = usageWindows(sn, now)
	for _, w := range out.Windows {
		if w.UsedRatio > out.UsedRatio || out.ResetsAt.IsZero() {
			out.UsedRatio, out.ResetsAt = math.Max(out.UsedRatio, w.UsedRatio), w.ResetsAt
		}
	}
	return out, nil
}

// warmup 用這個帳號送一則極小的訊息（不留對話、不給工具）。
func (e *Engine) warmup(ctx context.Context, cfg engineConfig) error {
	call, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	env := withAccountEnv(e.environ(), cfg)
	out, err := e.run(call, env, e.claudeBin(), "-p", "ping", "--model", "haiku", "--no-session-persistence", "--disallowedTools", "*")
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return unavailable("額度暖機沒有成功：這個帳號的 claude 沒有回應（是不是還沒登入？）")
	}
	return nil
}

func (e *Engine) run(ctx context.Context, env []string, name string, args ...string) ([]byte, error) {
	if e.d.Run != nil {
		return e.d.Run(ctx, env, name, args...)
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = env
	cmd.Dir = os.TempDir()
	return cmd.Output()
}

func (e *Engine) lookPath(name string) (string, error) {
	if e.d.LookPath != nil {
		return e.d.LookPath(name)
	}
	return exec.LookPath(name)
}

// loginFix：沒登入時的下一步，寫成不用懂終端機也能照做的（DEV-153 B3，ECL-70）。「evergenie 修復」是安裝時放在「應用程式」的小工具，
// 它照 Login（loginCommand）替主人把 claude 開在對的帳號資料夾上，主人只要照畫面登入。名字跟 platform.RescueName 一致由測試守。
const loginFix = "到這台 Mac 前面，按 ⌘＋空白鍵搜尋「evergenie 修復」打開它，照畫面選「重新登入 AI 帳號」，再用瀏覽器登入你的 Claude 帳號"

// loginHint：修復小工具開 claude 之前給主人看的步驟。
var loginHint = []string{
	"等一下會自動打開瀏覽器，照畫面登入你的 Claude 帳號。",
	"登入好之後這裡會自己繼續，不用打任何字。",
}

// loginCommand 是主人親手登入這個帳號要跑的指令：`claude auth login`（直接開瀏覽器登入、登完自己結束，主人不用打字），
// 跑在這個帳號的設定資料夾上（預設登入就不設 CLAUDE_CONFIG_DIR）。
func (e *Engine) loginCommand(cfg engineConfig) []string {
	bin := e.claudeBin()
	if p, err := e.lookPath(bin); err == nil {
		bin = p
	}
	if cfg.usesDefaultLogin() {
		return []string{bin, "auth", "login"}
	}
	return []string{"CLAUDE_CONFIG_DIR=" + cfg.ConfigDir, bin, "auth", "login"}
}

// Preflight 檢查這個帳號要跑起來需要的東西：claude 程式、tmux、帳號有登入。只檢查，不安裝（Install 只是建議的指令）。
func (e *Engine) Preflight(_ context.Context, req *sdk.EnginePreflightRequest) (*sdk.EnginePreflightResponse, error) {
	out := &sdk.EnginePreflightResponse{}
	bin := e.claudeBin()
	if p, err := e.lookPath(bin); err == nil {
		out.Checks = append(out.Checks, sdk.PreflightCheck{ID: "claude", Name: "claude 程式", Ok: true, Detail: p})
	} else {
		out.Checks = append(out.Checks, sdk.PreflightCheck{ID: "claude", Name: "claude 程式", Detail: "找不到 " + bin, Fix: "依 Claude Code 官方文件安裝，再登入一次"})
	}
	out.Checks = append(out.Checks, e.tmuxCheck())
	cfg, err := parseEngineConfig(req.EngineConfig)
	name := "帳號 " + req.Account + " 已登入"
	if err != nil {
		out.Checks = append(out.Checks, sdk.PreflightCheck{ID: "login:" + req.Account, Name: name, Detail: "帳號設定不完整：" + messageOf(err), Fix: "在 home.toml 的這個帳號補上 engine_config.config_dir"})
		return out, nil
	}
	if loggedIn(cfg.ConfigDir) {
		out.Checks = append(out.Checks, sdk.PreflightCheck{ID: "login:" + req.Account, Name: name, Ok: true, Detail: cfg.ConfigDir, Login: e.loginCommand(cfg), LoginHint: loginHint})
	} else {
		out.Checks = append(out.Checks, sdk.PreflightCheck{ID: "login:" + req.Account, Name: name, Detail: "這個帳號還沒有登入（登入資料夾 " + cfg.ConfigDir + "）", Fix: loginFix, Login: e.loginCommand(cfg), LoginHint: loginHint})
	}
	return out, nil
}

// tmuxCheck：找的順序跟真的開 session 一樣（ECL-65、ECL-63：固定位置的 ＞ 引擎旁邊附帶的 ＞ PATH 上的），都沒有才算沒裝。
// 修法分兩種裝法說：下載版升級時補上（發行檔附了 tmux；安裝程式不重裝已經有家的機器）或先 brew install、原始碼版 brew install；
// 有 Homebrew 的話 preflight --fix 會幫忙 brew install。
func (e *Engine) tmuxCheck() sdk.PreflightCheck {
	if c := chooseTmux(e.d.HookBin, e.lookPath); c.bin != "" {
		detail := c.bin
		switch {
		case c.launcher != "" && c.stable:
			detail += "（跟著程式一起裝的；系統問資料夾權限時記在這一支名下）"
		case c.launcher != "":
			// 固定位置還沒放好（ECL-65）：一樣記在這一支名下，但路徑帶版本號，升級後系統會再問一次
			detail += "（跟著程式一起裝的；系統問資料夾權限時記在這一支名下，但它還不在固定位置，升級後可能要再按一次允許）"
		case c.bundled:
			detail += "（跟著程式一起裝的）"
		}
		return sdk.PreflightCheck{ID: "tmux", Name: "tmux", Ok: true, Detail: detail}
	}
	where := "EverGenie 的程式資料夾"
	if e.d.HookBin != "" {
		if real, err := filepath.EvalSymlinks(e.d.HookBin); err == nil {
			where += "（" + filepath.Dir(real) + "）"
		}
	}
	return sdk.PreflightCheck{
		ID: "tmux", Name: "tmux",
		Detail:  "找不到（秘書的對話跑在 tmux 裡）：" + where + "和 PATH 上都沒有",
		Fix:     "下載安裝的：tmux 跟著 EverGenie 一起裝，下次升級（evergenie upgrade --apply）就會補上，急的話先 brew install tmux；從原始碼裝的：brew install tmux",
		Install: []string{"brew", "install", "tmux"},
	}
}

func messageOf(err error) string {
	var se *sdk.Error
	if errors.As(err, &se) {
		return se.Message
	}
	return err.Error()
}

// loggedIn：帳號資料夾的 .claude.json 有 oauthAccount。預設的 ~/.claude 把檔案放在家目錄的 ~/.claude.json。
func loggedIn(configDir string) bool {
	cands := []string{filepath.Join(configDir, ".claude.json")}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(configDir) == filepath.Join(home, ".claude") {
		cands = append(cands, filepath.Join(home, ".claude.json"))
	}
	for _, f := range cands {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var v map[string]any
		if json.Unmarshal(data, &v) == nil {
			if _, ok := v["oauthAccount"]; ok {
				return true
			}
		}
	}
	return false
}
