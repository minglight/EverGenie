package accounts

import (
	"context"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/engine/registry"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// Info 是 home.toml 的一個 AI 帳號（[accounts.<名>]）給看板看的樣子。
type Info struct {
	Name         string
	Engine       string
	ConfigDir    string
	EnvFile      string
	RemoteAccess bool
	UsageWarmup  bool
	Default      bool
}

// List 列出 home.toml 的 AI 帳號（依名稱排序）。只讀。
func List(home string) ([]Info, error) {
	raw, err := platform.ReadHomeTable(home)
	if err != nil {
		return nil, err
	}
	accounts, _ := raw["accounts"].(map[string]any)
	def, _ := accounts["default"].(string)
	var out []Info
	for name, v := range accounts {
		t, ok := v.(map[string]any)
		if !ok {
			continue
		}
		a := Info{Name: name, Default: name == def}
		a.Engine, _ = t["engine"].(string)
		a.RemoteAccess, _ = t["remote_access"].(bool)
		a.UsageWarmup, _ = t["usage_warmup"].(bool)
		if ec, ok := t["engine_config"].(map[string]any); ok {
			a.ConfigDir, _ = ec["config_dir"].(string)
			a.EnvFile, _ = ec["env_file"].(string)
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

var accountName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// Save 新增或更新一個 AI 帳號（走 platform.SetHomeTable：先備份、寫完驗證、不合格還原）。
// 其他 engine_config 的欄位（沒在畫面上的）原樣保留；makeDefault 時順便設成預設帳號。回傳 home.toml 的備份路徑。
func Save(home string, a Info, makeDefault bool, now time.Time) (backup string, err error) {
	a.Name = strings.TrimSpace(a.Name)
	if !accountName.MatchString(a.Name) || a.Name == "default" {
		return "", platform.Invalid("name", "帳號名稱只能用小寫英數字、底線與減號，而且不能叫 default")
	}
	a.Engine = strings.TrimSpace(a.Engine)
	if a.Engine == "" {
		return "", platform.Invalid("engine", "請選引擎")
	}
	a.ConfigDir = platform.ExpandHome(strings.TrimSpace(a.ConfigDir))
	if a.ConfigDir == "" || !filepath.IsAbs(a.ConfigDir) {
		return "", platform.Invalid("config_dir", "請填這個帳號的設定資料夾（完整路徑，例如 ~/.acct-work）")
	}
	raw, err := platform.ReadHomeTable(home)
	if err != nil {
		return "", err
	}
	ec := map[string]any{}
	if accounts, ok := raw["accounts"].(map[string]any); ok {
		if old, ok := accounts[a.Name].(map[string]any); ok {
			if oec, ok := old["engine_config"].(map[string]any); ok {
				for k, v := range oec {
					ec[k] = v
				}
			}
		}
	}
	ec["config_dir"] = a.ConfigDir
	if env := strings.TrimSpace(a.EnvFile); env != "" {
		ec["env_file"] = env
	} else {
		delete(ec, "env_file")
	}
	table := map[string]any{"engine": a.Engine, "remote_access": a.RemoteAccess, "usage_warmup": a.UsageWarmup, "engine_config": ec}
	backup, err = platform.SetHomeTable(home, "accounts."+a.Name, table, now)
	if err != nil {
		return backup, err
	}
	if makeDefault {
		if _, err = platform.SetHomeTable(home, "accounts.default", a.Name, now.Add(time.Nanosecond)); err != nil {
			return backup, err
		}
	}
	return backup, nil
}

// Show 回帳號清單與可選的引擎。
func (m *Module) Show(ctx context.Context) (*sdk.SystemAccountsResponse, error) {
	home := m.homeDir()
	if home == "" {
		return nil, platform.Unavailable("還不知道家在哪裡")
	}
	rows, err := List(home)
	if err != nil {
		return nil, err
	}
	out := &sdk.SystemAccountsResponse{Engines: registry.Engines()}
	for _, a := range rows {
		out.Accounts = append(out.Accounts, sdk.Account{Name: a.Name, Engine: a.Engine, ConfigDir: a.ConfigDir, EnvFile: a.EnvFile, RemoteAccess: a.RemoteAccess, UsageWarmup: a.UsageWarmup, Default: a.Default})
	}
	return out, nil
}

// Upsert 新增或更新一個帳號；只有主人（owner(…)）能做，秘書不能改電腦與帳號。
func (m *Module) Upsert(ctx context.Context, actor *sdk.Actor, a sdk.Account, makeDefault bool) (*sdk.SystemSaveAccountResponse, error) {
	who, err := platform.ActorName(actor)
	if err != nil {
		return nil, err
	}
	if !platform.IsOwner(who) {
		return nil, platform.Forbidden("只有主人能改 AI 帳號")
	}
	home := m.homeDir()
	if home == "" {
		return nil, platform.Unavailable("還不知道家在哪裡")
	}
	backup, err := Save(home, Info{Name: a.Name, Engine: a.Engine, ConfigDir: a.ConfigDir, EnvFile: a.EnvFile, RemoteAccess: a.RemoteAccess, UsageWarmup: a.UsageWarmup}, makeDefault, m.now())
	if err != nil {
		return nil, err
	}
	if log := m.logger(); log != nil {
		log.Info("AI 帳號已儲存", "by", who, "account", a.Name, "backup", backup)
	}
	return &sdk.SystemSaveAccountResponse{Backup: backup}, nil
}

// Usages 回各帳號的用量與額度（account 空＝全部）。
func (m *Module) Usages(ctx context.Context, account string) (*sdk.SystemUsageResponse, error) {
	if m.d.Usage == nil {
		return nil, platform.Unavailable("引擎服務還沒接上")
	}
	rows, err := m.d.Usage.Usages(ctx, strings.TrimSpace(account))
	if err != nil {
		return nil, err
	}
	return &sdk.SystemUsageResponse{Accounts: rows}, nil
}
