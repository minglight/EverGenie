package reminder

import (
	"time"

	"github.com/minglight/evergenie/internal/base/vault"
)

func text(r vault.Record, k string) string { s, _ := r.Fields[k].(string); return s }
func stamp(r vault.Record, k string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, text(r, k))
	return t
}
func ended(s string) bool { return s == "done" || s == "cancelled" }

// Spec 回傳待辦的資料種類宣告，給遷移工具直接註冊用（只新增匯出，模組行為不變）。
func Spec() vault.Spec { return reminderSpec() }

func reminderSpec() vault.Spec {
	sc := vault.Spec{Kind: "reminder", Path: func(id string) string { return "vault/reminders/" + id + ".md" }, Fields: []vault.Field{
		{Name: "content", Type: vault.String, Required: true}, {Name: "follow", Type: vault.Enum, Required: true, Default: "none", Enum: []string{"none", "remind", "confirm", "check", "discuss"}}, {Name: "cycle", Type: vault.Enum, Required: true, Default: "once", Enum: []string{"once", "daily", "weekly", "biweekly", "monthly"}}, {Name: "at", Type: vault.DateTime, RequiredIf: func(r vault.Record) bool { return text(r, "follow") != "none" || text(r, "cycle") != "once" }}, {Name: "cycle_at", Type: vault.DateTime}, {Name: "owner", Type: vault.String, Required: true}, {Name: "room", Type: vault.String}, {Name: "status", Type: vault.Enum, Required: true, Default: "todo", Enum: []string{"todo", "doing", "asking", "shelved", "done", "cancelled"}}, {Name: "check", Type: vault.String, RequiredIf: func(r vault.Record) bool { return text(r, "follow") == "check" }}, {Name: "source", Type: vault.String}, {Name: "asks", Type: vault.Int, Required: true, Default: 0}, {Name: "sent_at", Type: vault.DateTime}, {Name: "closed_at", Type: vault.DateTime, RequiredIf: func(r vault.Record) bool { return ended(text(r, "status")) }}, {Name: "goal", Type: vault.Link, LinkKind: "goal"}, {Name: "attachments", Type: vault.List}, {Name: "difficulty", Type: vault.Enum, Enum: []string{"easy", "normal", "hard"}}, {Name: "done_points", Type: vault.Int}, {Name: "done_at", Type: vault.DateTime}, {Name: "due_at", Type: vault.DateTime}}}
	sc.ClosedFunc = func(r vault.Record) (bool, time.Time) {
		if ended(text(r, "status")) {
			return true, stamp(r, "closed_at")
		}
		if text(r, "status") == "shelved" {
			return true, r.UpdatedAt
		}
		return false, time.Time{}
	}
	sc.ArchiveAt = func(r vault.Record, loc *time.Location) (time.Time, bool) {
		if text(r, "status") == "shelved" {
			return r.UpdatedAt.AddDate(0, 0, 30), true
		}
		if ended(text(r, "status")) {
			at := stamp(r, "closed_at").In(loc)
			return time.Date(at.Year(), at.Month(), at.Day()+1, 0, 0, 0, 0, loc), true
		}
		return time.Time{}, false
	}
	return sc
}
