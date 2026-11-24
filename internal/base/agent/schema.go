package agent

import (
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/internal/base/vault"
)

// Specs 回傳這個模組註冊的全部資料種類宣告，給遷移工具直接註冊用（只新增匯出，模組行為不變）。
func Specs(h *platform.Home) []vault.Spec { return specs(h) }

func specs(h *platform.Home) []vault.Spec {
	common := []vault.Field{{Name: "schema", Type: vault.Int, Required: true}, {Name: "updated_at", Type: vault.DateTime, Required: true}, {Name: "by", Type: vault.String, Required: true}}
	agentFields := []vault.Field{{Name: "id", Type: vault.String, Required: true}, {Name: "created_at", Type: vault.DateTime, Required: true}, {Name: "duty", Type: vault.String, Required: true}, {Name: "persona", Type: vault.String, Required: true}, {Name: "former_names", Type: vault.List, Default: []string{}}, {Name: "display_name", Type: vault.String}, {Name: "enabled", Type: vault.Bool, Required: true, Default: true}, {Name: "mode", Type: vault.Enum, Required: true, Default: "resident", Enum: []string{"resident", "ondemand", "none"}}, {Name: "account", Type: vault.String, Required: true, Default: h.Accounts["default"]}, {Name: "model", Type: vault.String, Default: "sonnet"}, {Name: "effort", Type: vault.String}, {Name: "remote_access", Type: vault.Bool}, {Name: "color", Type: vault.String}, {Name: "color_dark", Type: vault.String}, {Name: "reminder_slot", Type: vault.String, Default: h.Slots["default"]}, {Name: "budget", Type: vault.Map}, {Name: "extras", Type: vault.Map, Default: map[string]any{}}, {Name: "shares_conversation_with", Type: vault.String}, {Name: "duties_from", Type: vault.String}, {Name: "knowledge", Type: vault.List}, {Name: "knowledge_seen", Type: vault.List}, {Name: "first_day", Type: vault.Bool}}
	out := []vault.Spec{}
	for _, kind := range []string{"agent", "addendum", "memory", "profile", "journal", "note", "changelog"} {
		k := kind
		sc := vault.Spec{Kind: k, Fields: append([]vault.Field{}, common...), Path: func(id string) string {
			if k == "profile" {
				return "vault/profile.md"
			}
			if k == "journal" || k == "note" || k == "changelog" {
				parts := strings.SplitN(id, "/", 2)
				if len(parts) != 2 {
					return ""
				}
				dir := k
				if k == "note" {
					dir = "notes"
				}
				return "vault/agents/" + parts[0] + "/" + dir + "/" + parts[1] + ".md"
			}
			return "vault/agents/" + id + "/" + k + ".md"
		}}
		switch k {
		case "agent":
			sc.Fields = append(sc.Fields, agentFields...)
		case "addendum", "memory", "journal":
			sc.Fields = append(sc.Fields, vault.Field{Name: "agent", Type: vault.String, Required: true})
		}
		if k == "note" {
			// 秘書自己的做事筆記（DEV-161）：核心寫入時補 agent 與 created_at；沒有這兩欄的舊檔（或沒有檔頭的檔）照樣讀得到。
			sc.Fields = append(sc.Fields, vault.Field{Name: "agent", Type: vault.String}, vault.Field{Name: "created_at", Type: vault.DateTime})
		}
		if k == "memory" || k == "profile" {
			max := MemoryCoreLines // 記憶核心（DEV-161）；以前是 200
			if k == "profile" {
				max = 150
			}
			sc.Fields = append(sc.Fields, vault.Field{Name: "max_lines", Type: vault.Int, Default: max})
		}
		if k == "journal" {
			sc.Fields = append(sc.Fields, vault.Field{Name: "date", Type: vault.Date, Required: true})
			sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
				date, _ := r.Fields["date"].(string)
				at, err := time.ParseInLocation("2006-01-02", date, loc)
				return at.AddDate(0, 0, 90), err == nil
			}
		}
		if k == "changelog" {
			sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
				parts := strings.SplitN(r.ID, "/", 2)
				if len(parts) != 2 {
					return time.Time{}, false
				}
				at, err := time.ParseInLocation("2006-01-02", parts[1], loc)
				return at.AddDate(2, 0, 0), err == nil
			}
		}
		out = append(out, sc)
	}
	return append(out, noteSpecs()...)
}
