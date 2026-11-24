package goal

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie goal …` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module、不啟動任何東西，只用 sdk 的 client 打 API（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"goal propose": {Usage: "goal propose|show|list|planbook|done|withdraw", Group: cmdline.GroupWork, Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{}, api(e).Propose,
				func(r *sdk.GoalsProposeResponse) any { return r.Goal })
		}},
		"goal show":     {Run: show},
		"goal planbook": {Run: planbook},
		"goal list": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.Each(e, a, cmdline.Spec{Rename: map[string]string{"state": "states"}}, api(e).List, nil,
				func(r *sdk.GoalsListResponse) ([]sdk.Goal, string) { return r.Goals, "" })
		}},
		"goal done": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Done, func(r *sdk.GoalsDoneResponse) any { return r.Goal })
		}},
		"goal withdraw": {Run: func(e *cmdline.Env, a []string) error {
			return cmdline.One(e, a, cmdline.Spec{ID: true}, api(e).Withdraw, func(r *sdk.GoalsWithdrawResponse) any { return r.Goal })
		}},
	}
}

func api(e *cmdline.Env) sdk.GoalsAPI { return sdk.NewGoalsClient(e.Client) }

// show 印 Goal 的原文、狀態與 Planbook 三段純文字。
func show(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.GoalsGetRequest](e, a, cmdline.Spec{ID: true})
	if err != nil {
		return err
	}
	resp, err := api(e).Get(e.Ctx, p.Req)
	if err != nil {
		return err
	}
	g := resp.Goal
	if g == nil {
		return fmt.Errorf("沒有找到這個 Goal")
	}
	_, err = fmt.Fprintf(e.Out, "【原文】%s　%s\n%s\n\n【狀態】%s\n\n【Planbook】\n%s\n", g.ID, g.Title, g.Text, g.State, g.Planbook)
	return err
}

// planbook 改 Planbook 內文：--body 直接給，或 --file 讀檔（- 表示標準輸入）。
func planbook(e *cmdline.Env, a []string) error {
	a, file, hasFile := takeFlag(a, "file")
	p, err := cmdline.Prepare[sdk.GoalsUpdatePlanbookRequest](e, a, cmdline.Spec{ID: true})
	if err != nil {
		return err
	}
	switch {
	case hasFile && p.Set["body"]:
		return cmdline.Usagef("--file 和 --body 只能給一個")
	case !hasFile && !p.Set["body"]:
		return cmdline.Usagef("用法：evergenie goal planbook <GOAL-n> --file <路徑>|--body <文字> --note <一句話>")
	case hasFile:
		var data []byte
		if file == "-" {
			data, err = io.ReadAll(e.In)
		} else {
			data, err = os.ReadFile(file)
		}
		if err != nil {
			return fmt.Errorf("讀不到 %s：%w", file, err)
		}
		p.Req.Body = string(data)
	}
	return cmdline.Send(e, p, api(e).UpdatePlanbook, func(r *sdk.GoalsUpdatePlanbookResponse) any { return r.Goal })
}

// takeFlag 把 --name 值（或 --name=值）從參數中取出；最後一個值為準。
func takeFlag(args []string, name string) (rest []string, val string, found bool) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--":
			return append(rest, args[i:]...), val, found
		case args[i] == "--"+name && i+1 < len(args):
			val, found = args[i+1], true
			i++
		case strings.HasPrefix(args[i], "--"+name+"="):
			val, found = strings.TrimPrefix(args[i], "--"+name+"="), true
		default:
			rest = append(rest, args[i])
		}
	}
	return rest, val, found
}
