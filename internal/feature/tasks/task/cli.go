package task

import (
	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie tasks …` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module、不啟動任何東西，只用 sdk 的 client 打 API（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"tasks add": {Usage: "tasks add|set|submit|done|list|get|stale|waiting", Group: cmdline.GroupWork, Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{}, api(e).Create, func(r *sdk.TasksCreateResponse) any { return r.Task })
		}},
		"tasks set": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Update, func(r *sdk.TasksUpdateResponse) any { return r.Task })
		}},
		"tasks submit": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Submit, func(r *sdk.TasksSubmitResponse) any { return r.Task })
		}},
		"tasks done": {Run: func(e *cmdline.Env, a []string) error {
			p, err := cmdline.Prepare[sdk.TasksCloseRequest](e, a, cmdline.Spec{ID: true})
			if err != nil {
				return err
			}
			if !p.Set["status"] {
				p.Req.Status = sdk.TaskStatusDone
			}
			return cmdline.Send(e, p, api(e).Close, func(r *sdk.TasksCloseResponse) any { return r.Task })
		}},
		"tasks get": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Get, func(r *sdk.TasksGetResponse) any { return r.Task })
		}},
		"tasks list": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.Each(e, a, cmdline.Spec{}, api(e).List,
				func(r *sdk.TasksListRequest, cursor string) { r.Page = &sdk.PageRequest{Cursor: cursor} },
				func(r *sdk.TasksListResponse) ([]sdk.Task, string) { return r.Tasks, cmdline.NextCursor(r.Page) })
		}},
		"tasks stale": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.Each(e, a, cmdline.Spec{}, api(e).Stale, nil,
				func(r *sdk.TasksStaleResponse) ([]sdk.Task, string) { return r.Tasks, "" })
		}},
		"tasks waiting": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.Each(e, a, cmdline.Spec{}, api(e).Waiting, nil,
				func(r *sdk.TasksWaitingResponse) ([]sdk.WaitingTask, string) { return r.Tasks, "" })
		}},
	}
}

func api(e *cmdline.Env) sdk.TasksAPI { return sdk.NewTasksClient(e.Client) }
