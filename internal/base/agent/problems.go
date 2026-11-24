package agent

import (
	"context"
	"fmt"

	"github.com/minglight/evergenie/internal/base/agent/personaref"

	"github.com/minglight/evergenie/internal/base/platform"
)

// Problems 檢查每位啟用中的秘書設定得通不通（doctor 的 config 檢查用，紅字）：人設卡 personas/<slug>.md 要讀得到，
// 綁的帳號要在 home.toml 裡。一條一句白話；沒有問題回空串列。只讀，不改任何東西。
// 坐的角色來自停用、被拒或移除的套件不算在這裡（秘書照樣起得來），在 Warnings。
func (m *Module) Problems(ctx context.Context) []string {
	problems, _ := m.check(ctx)
	return problems
}

// Warnings 是 doctor 的黃字（SEC-302 S6、REQ-359）：秘書坐的角色來自停用、被拒或移除的套件。
// 使用者的套件壞了只影響它自己（PKC-8），不能讓 doctor 變紅字擋住部署。
func (m *Module) Warnings(ctx context.Context) []string {
	_, warnings := m.check(ctx)
	return warnings
}

func (m *Module) check(ctx context.Context) (problems, warnings []string) {
	problems, warnings = []string{}, []string{}
	rows, err := m.List(ctx)
	if err != nil {
		return append(problems, "讀不到秘書名冊"), warnings
	}
	if level, _, exists, err := m.src.Style(); err == nil && level == "custom" && !exists {
		problems = append(problems, "說話層設成 custom，但 personas/_style.md 不存在（目前用強度 C）")
	}
	// 家的設定讀不起來是 doctor 離線檢查（config.home）報告的事；這裡就只檢查人設卡。
	h, homeErr := platform.LoadHome(m.home)
	for _, row := range rows {
		if enabled, ok := row.Fields["enabled"].(bool); ok && !enabled {
			continue
		}
		name := row.ID
		if display, _ := row.Fields["display_name"].(string); display != "" {
			name = display + "（" + row.ID + "）"
		}
		slug, _ := row.Fields["persona"].(string)
		if info, err := m.src.Card(slug); err != nil {
			if personaref.IsPackageCode(slug) {
				warnings = append(warnings, fmt.Sprintf("秘書 %s：坐的人設 %s 來自的套件不在了或卡讀不了，目前用暫代口吻，請換一位角色或把套件裝回來", name, slug))
			} else {
				problems = append(problems, fmt.Sprintf("秘書 %s：人設卡 personas/%s.md 不存在或讀不了", name, slug))
			}
		} else if info.Source == personaref.SourcePackage && info.Inactive != "" {
			warnings = append(warnings, fmt.Sprintf("秘書 %s：坐的人設 %s 來自的套件%s目前不能用（%s），仍沿用原卡；要換角色在設定→職位與秘書，或把套件開起來", name, slug, info.PackageID, info.Inactive))
		} else if info.Status == personaref.StatusDesign {
			problems = append(problems, fmt.Sprintf("秘書 %s：坐的人設 %s 在設計中（不能指派），請換一位或讓它上線", name, slug))
		}
		if from := DutiesFrom(row.Fields); from != "" && m.duties != nil { // 官方職責的樣板讀不到：不擋（黃字），秘書照附加規則做
			if _, ok := m.duties(from); !ok {
				warnings = append(warnings, fmt.Sprintf("秘書 %s：用的官方職責「%s」讀不到（帶它來的套件不在了），目前開場沒有職責內容；把套件裝回來，或請秘書幫你把職責寫進附加規則", name, from))
			}
		}
		if homeErr != nil {
			continue
		}
		account, _ := row.Fields["account"].(string)
		if account == "" {
			account, _ = h.Accounts["default"].(string)
		}
		if _, ok := h.Accounts[account].(map[string]any); account == "" || account == "default" || !ok {
			problems = append(problems, fmt.Sprintf("秘書 %s：綁的帳號「%s」不在 home.toml 的 accounts 裡", name, account))
		}
	}
	return problems, warnings
}
