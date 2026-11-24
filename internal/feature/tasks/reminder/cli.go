package reminder

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie reminders …` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module、不啟動任何東西，只用 sdk 的 client 打 API（CLI-1）。
// 旗標名就是請求的欄位名，由 cmdline 的 Spec 對進 sdk 請求型別。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"reminders add": {Usage: "reminders add|set|snooze|done|doing|cancel|list|get", Group: cmdline.GroupWork, Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{}, api(e).Create, func(r *sdk.RemindersCreateResponse) any { return r.Reminder })
		}},
		"reminders set": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Update, func(r *sdk.RemindersUpdateResponse) any { return r.Reminder })
		}},
		"reminders snooze": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Snooze, func(r *sdk.RemindersSnoozeResponse) any { return r.Reminder })
		}},
		"reminders done": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Done, func(r *sdk.RemindersDoneResponse) any { return r.Reminder })
		}},
		"reminders doing": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Doing, func(r *sdk.RemindersDoingResponse) any { return r.Reminder })
		}},
		"reminders cancel": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Cancel, func(r *sdk.RemindersCancelResponse) any { return r.Reminder })
		}},
		"reminders get": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Get, func(r *sdk.RemindersGetResponse) any { return r.Reminder })
		}},
		"reminders list": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.Each(e, a, cmdline.Spec{}, api(e).List,
				func(r *sdk.RemindersListRequest, cursor string) { r.Page = &sdk.PageRequest{Cursor: cursor} },
				func(r *sdk.RemindersListResponse) ([]sdk.Reminder, string) {
					return r.Reminders, cmdline.NextCursor(r.Page)
				})
		}},
	}
}

func api(e *cmdline.Env) sdk.RemindersAPI { return sdk.NewRemindersClient(e.Client) }
