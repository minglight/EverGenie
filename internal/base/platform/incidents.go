package platform

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// MigrateIncidentsOwner 是家的遷移（update 的 HomeMigration，由 cmd/evergenie 組裝；2026-11-01 預演三個家升級時補的）：
// 舊版 home.toml 的事故設定是 `[incidents] dispatcher`（收單分診的秘書）＋`reviewer`（代驗收），新版只讀 `owner`（PLT-14）。
// 舊的家只寫了 dispatcher 時，升級後會變成「沒設事故負責人」、事故單沒人接；這裡把 dispatcher 抄成 owner。
//
// 只在 owner 沒寫、dispatcher 有寫、而且那位秘書在這個家的名冊裡（vault/agents/<id>/agent.md）才動；其餘不動（doctor 照舊提醒主人自己定）。
// 只在 [incidents] 區段加一行 owner，dispatcher、reviewer 與註解原樣留著：舊版不認得 owner 會略過，照樣讀得懂。能重跑：抄過就有 owner，不會再動。
func MigrateIncidentsOwner(home string) error {
	path := filepath.Join(home, "home.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return Unavailable("無法讀取家的設定，請檢查存取權限")
	}
	var cfg struct {
		Incidents struct {
			Owner      string `toml:"owner"`
			Dispatcher string `toml:"dispatcher"`
		} `toml:"incidents"`
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return Invalid("home", "設定格式或欄位型別不正確，請檢查設定檔")
	}
	owner, dispatcher := strings.TrimSpace(cfg.Incidents.Owner), strings.TrimSpace(cfg.Incidents.Dispatcher)
	if owner != "" || !incidentAgentID.MatchString(dispatcher) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(home, "vault", "agents", dispatcher, "agent.md")); err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	section := ""
	for i, line := range lines {
		if m := tomlHeader.FindStringSubmatch(line); m != nil {
			section = strings.TrimSpace(m[1])
			continue
		}
		if section != "incidents" {
			continue
		}
		if m := dispatcherLine.FindStringSubmatch(line); m != nil {
			out := append(append(append([]string{}, lines[:i]...), m[1]+`owner = "`+dispatcher+`"`), lines[i:]...)
			info, err := os.Stat(path)
			if err != nil {
				return Unavailable("無法讀取家的設定，請檢查存取權限")
			}
			return AtomicWrite(path, []byte(strings.Join(out, "\n")), info.Mode().Perm())
		}
	}
	return nil // dispatcher 寫成 incidents.dispatcher 之類的少見寫法：不動，doctor 會提醒
}

var (
	incidentAgentID = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	tomlHeader      = regexp.MustCompile(`^\s*\[\[?\s*([^\[\]]+?)\s*\]\]?\s*(#.*)?$`) // [區段] 與 [[陣列區段]]
	dispatcherLine  = regexp.MustCompile(`^(\s*)dispatcher\s*=`)
)
