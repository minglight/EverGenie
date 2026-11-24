package backupsweep

import (
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

//go:embed defaults.toml
var defaultsTOML string

// SettingsFile 是主人加例外的設定檔（相對家）；只讀裡面的 [[skip]]（BSW-3）。
const SettingsFile = "settings/backup-sweep.toml"

// Skip 是一條例外：path 照檔名萬用字元比對（不跨 /），比對到的檔案或資料夾整個略過。
type Skip struct {
	Path   string `toml:"path"`
	Reason string `toml:"reason"`
}

// Check 是「在用」的一種檢查：files 是一組檔案（每個檔的內容算一筆），command 是一個指令（輸出的每一行算一筆）。
// Name 是程式內部用的名字；Label 是給主人看的白話（報告、看板、通知只寫這個，不寫工具名稱，MOD-N7）。
type Check struct {
	Name    string   `toml:"name"`
	Label   string   `toml:"label"`
	Files   string   `toml:"files"`
	Command []string `toml:"command"`
}

type fileConfig struct {
	Roots  []string `toml:"roots"`
	Skip   []Skip   `toml:"skip"`
	Checks []Check  `toml:"check"`
}

// Config 是一次掃描要的全部東西，路徑都已經展開成絕對路徑。
type Config struct {
	UserHome string   // 使用者家目錄
	Home     string   // 這個家
	Roots    []string // 掃描範圍（可以有 * 萬用字元，掃之前展開）
	Skips    []Skip   // 內建的＋主人加的例外
	Checks   []Check
	// Probe 跑一種檢查；nil＝照 Check 讀檔、跑指令（runCheck）。測試換掉它。
	Probe func(Check) ([]Ref, error)
	// GitFiles 回一個 git 工作目錄裡 git 管的檔（相對路徑）；nil＝跑 git ls-files。測試換掉它。
	GitFiles func(root string) ([]string, error)
	// Visit 每讀一個資料夾之前叫一次（測試拿來證明沒有碰掃描範圍外面）；可以是 nil。
	Visit func(dir string)
}

// loadDefaults 讀跟著程式走的內建設定（defaults.toml）。
func loadDefaults() (fileConfig, error) {
	var c fileConfig
	_, err := toml.Decode(defaultsTOML, &c)
	return c, err
}

// launchAgentsDir 是放開機服務設定的資料夾：EVERGENIE_LAUNCH_AGENTS_DIR（測試用，跟 system/host 同一個變數）優先。
func launchAgentsDir(userHome string) string {
	if d := sdk.Getenv("EVERGENIE_LAUNCH_AGENTS_DIR"); d != "" {
		return d
	}
	return filepath.Join(userHome, "Library", "LaunchAgents")
}

// expand 把 ~、{home}、{launch_agents} 換成絕對路徑。
func expand(p, userHome, home string) string {
	p = strings.TrimSpace(p)
	switch {
	case p == "~":
		p = userHome
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(userHome, p[2:])
	}
	p = strings.ReplaceAll(p, "{home}", home)
	p = strings.ReplaceAll(p, "{launch_agents}", launchAgentsDir(userHome))
	return filepath.Clean(p)
}

// LoadConfig 組這個家的掃描設定：內建的範圍、例外、檢查，加上這個家自己的備份資料夾（程式算位置）與主人設定檔裡的例外。
// 主人的設定檔讀不懂時照樣回內建的設定，錯誤另外回（報告寫「出錯」並通知主人）。
func LoadConfig(home, userHome string) (Config, error) {
	d, err := loadDefaults()
	if err != nil {
		return Config{}, platform.Unavailable("內建的備份掃描設定讀不懂")
	}
	cfg := Config{UserHome: userHome, Home: home, Checks: d.Checks}
	for _, r := range d.Roots {
		cfg.Roots = append(cfg.Roots, expand(r, userHome, home))
	}
	// 平台相關的範圍、例外、檢查由 engine/registry 給（核心不寫平台名稱，MOD-N7）。
	for _, r := range registry.SweepAccountDirs() {
		cfg.Roots = append(cfg.Roots, expand(r, userHome, home))
	}
	for _, s := range d.Skip {
		cfg.Skips = append(cfg.Skips, Skip{Path: expand(s.Path, userHome, home), Reason: s.Reason})
	}
	for _, s := range registry.SweepSkips() {
		cfg.Skips = append(cfg.Skips, Skip{Path: expand(s.Path, userHome, home), Reason: s.Reason})
	}
	name, command := registry.SweepTerminalCheck()
	cfg.Checks = append(cfg.Checks, Check{Name: name, Label: "終端機視窗", Command: command})
	for i, c := range cfg.Checks {
		if c.Label == "" {
			cfg.Checks[i].Label = c.Name
		}
		if c.Files != "" {
			cfg.Checks[i].Files = expand(c.Files, userHome, home)
		}
	}
	cfg.Skips = append(cfg.Skips, ownSkips(home)...)
	user, uerr := userSkips(home, userHome)
	cfg.Skips = append(cfg.Skips, user...)
	return cfg, uerr
}

// ownSkips 是這個家自己管的備份資料夾：它們有自己的清理規則，不是這裡的備份。
func ownSkips(home string) []Skip {
	out := []Skip{
		{Path: platform.RuntimeDir(home, "backups"), Reason: "這個家改設定前留的舊檔：照保留規則自動清（每週掃描時順便清）"},
		{Path: filepath.Dir(platform.HomeBackupDir(home, "daily")), Reason: "整個家的備份：自己照保留規則清（HBK-3）"},
		{Path: platform.RuntimeDir(home, "home-backup"), Reason: "整個家備份的狀態與上傳暫存"},
		{Path: reportDir(home), Reason: "備份掃描自己的報告"},
		{Path: filepath.Join(home, "vault"), Reason: "這個家的資料（任務、排程、筆記…）：有自己的封存規則，名字像備份的是紀錄不是備份"},
		{Path: filepath.Join(home, filepath.FromSlash(SettingsFile)), Reason: "備份掃描的例外設定檔"},
	}
	// 主人的附件與圖庫（檔名是主人取的，例如分享的「…backup-sweep 規格」文件）。
	if l := platform.LayoutFor(home); l.V2 {
		out = append(out, Skip{Path: l.Media(), Reason: "這個家的附件與圖庫：主人的檔案，不是備份"})
	} else {
		out = append(out, Skip{Path: l.Media("share"), Reason: "這個家的附件：主人的檔案，不是備份"},
			Skip{Path: l.Media("art"), Reason: "這個家的圖庫：主人的檔案，不是備份"})
	}
	return out
}

// userSkips 讀主人設定檔的 [[skip]]；沒有檔案是空的。
func userSkips(home, userHome string) ([]Skip, error) {
	data, err := os.ReadFile(filepath.Join(home, filepath.FromSlash(SettingsFile)))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, platform.Unavailable("讀不了 " + SettingsFile)
	}
	var c struct {
		Skip []Skip `toml:"skip"`
	}
	if _, err := toml.Decode(string(data), &c); err != nil {
		return nil, platform.Invalid("backup-sweep", SettingsFile+" 格式不對，這次只用內建的例外")
	}
	var out []Skip
	for _, s := range c.Skip {
		if strings.TrimSpace(s.Path) == "" {
			continue
		}
		reason := strings.TrimSpace(s.Reason)
		if reason == "" {
			reason = "主人加的例外"
		}
		out = append(out, Skip{Path: expand(s.Path, userHome, home), Reason: reason + "（" + SettingsFile + "）"})
	}
	return out, nil
}

// label 是給主人看的檢查名稱；沒寫 label 就用 name（主人或測試自己加的檢查）。
func (c Check) label() string {
	if c.Label != "" {
		return c.Label
	}
	return c.Name
}
