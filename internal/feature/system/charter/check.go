package charter

import (
	"context"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
)

// Checks 登記健檢 config.rules（app.CheckProvider）：只會是 OK 或黃字，從來不是紅字——
// 規矩文件不在不影響秘書做事，紅字會讓部署退回（D-44）。
// 黃字只在 home.toml 明寫了 [rules] 但設定寫錯、或寫的檔案不在；沒寫 [rules]（用預設位置）時檔案不在就是頁面空著，不提醒。
func (m *Module) Checks() []app.Check {
	return []app.Check{{Name: "config.rules", Run: m.checkDocs}}
}

func (m *Module) checkDocs(ctx context.Context) app.CheckResult {
	m.mu.Lock()
	home := m.home
	m.mu.Unlock()
	if home == "" {
		return app.CheckResult{Status: app.StatusOK, Message: "規矩頁：沒有家，略過"}
	}
	cfg := loadConfig(home)
	if cfg.problem != "" {
		return app.CheckResult{Status: app.StatusWarn, Message: "規矩頁的設定寫錯了：" + cfg.problem, Fix: "照上面說的地方改 home.toml 的 [[rules.docs]]（一份文件一筆）"}
	}
	var missing []string
	found := 0
	for _, d := range cfg.docs {
		l, err := m.load(d)
		switch {
		case err != nil:
			missing = append(missing, d.Title+"（"+d.Path+" 讀不到）")
		case l.exists:
			found++
		case cfg.explicit:
			where := d.Path
			if d.Section != "" {
				where += " 的「" + d.Section + "」"
			}
			missing = append(missing, d.Title+"（"+where+" 不在）")
		}
	}
	if len(missing) > 0 {
		return app.CheckResult{Status: app.StatusWarn, Message: "規矩頁設定的文件找不到：" + strings.Join(missing, "、"),
			Fix: "把檔案放到那個位置，或改 home.toml 的 [[rules.docs]] path／section"}
	}
	if len(cfg.docs) == 0 || found == 0 {
		return app.CheckResult{Status: app.StatusOK, Message: "規矩頁：還沒有規矩文件（頁面是空的）"}
	}
	return app.CheckResult{Status: app.StatusOK, Message: "規矩頁：找到 " + strconv.Itoa(found) + " 份規矩文件"}
}
