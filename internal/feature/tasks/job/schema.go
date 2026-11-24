package job

import (
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
)

// Spec 是 job 這個資料種類的宣告（schemas/job.md）。遷移工具用它註冊資料種類，不必啟動排程服務。
func Spec() vault.Spec { return jobSpec() }

func jobSpec() vault.Spec {
	sc := vault.Spec{Kind: "job", Fields: []vault.Field{
		{Name: "schema", Type: vault.Int, Required: true}, {Name: "id", Type: vault.String, Required: true}, {Name: "created_at", Type: vault.DateTime, Required: true}, {Name: "updated_at", Type: vault.DateTime, Required: true}, {Name: "by", Type: vault.String, Required: true},
		{Name: "agent", Type: vault.String, RequiredIf: func(r vault.Record) bool { return strings.Contains(r.ID, "/") }},
		{Name: "name", Type: vault.String, Required: true}, {Name: "cron", Type: vault.List, RequiredIf: func(r vault.Record) bool { return r.Fields["windows"] == nil }}, {Name: "enabled", Type: vault.Bool, Required: true, Default: true},
		{Name: "windows", Type: vault.List}, {Name: "weekend_windows", Type: vault.List}, {Name: "per_day", Type: vault.Int}, {Name: "idle_min", Type: vault.Int},
		{Name: "catchup", Type: vault.Enum, Enum: []string{"once", "skip"}, Default: "skip"}, {Name: "action", Type: vault.Enum, Enum: []string{"none", "rotate", "usage_warmup", "limit_watch", "backup_sweep"}, Default: "none"},
		{Name: "silent", Type: vault.Bool, Default: false}, {Name: "source", Type: vault.String},
		{Name: "removed_at", Type: vault.DateTime},
	}}
	// 只有 `jobs rm` 移除的（source: user，記了 removed_at）可以封存；其他排程不封存、只停用（schemas/job.md）。
	sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
		at, err := time.Parse(time.RFC3339Nano, text(r, "removed_at"))
		return at, err == nil
	}
	return sc
}
func text(r vault.Record, key string) string { v, _ := r.Fields[key].(string); return v }

// job 資料欄位的讀取輔助（vault 回來的型別不固定）。

func stringList(v any) ([]string, bool) {
	switch values := v.(type) {
	case nil:
		return nil, true
	case []string:
		return values, true
	case []any:
		out := make([]string, 0, len(values))
		for _, x := range values {
			s, ok := x.(string)
			if !ok {
				return nil, false
			}
			out = append(out, s)
		}
		return out, true
	}
	return nil, false
}

func intField(v any, def int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return def
}
