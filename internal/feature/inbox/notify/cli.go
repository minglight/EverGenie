package notify

import (
	"path/filepath"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie notify`、`evergenie share` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module，只用 sdk 的 client 打 API（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"notify":    {Usage: "notify --agent <職位> --category approve|done|due|digest|opening --title … [--options \"好|不要\"] [--lane system（自動報告：不推、不算徽章）]", Group: cmdline.GroupWork, Run: sendCmd},
		"push init": {Usage: "push init [--subject mailto:信箱] [--force]   做手機推播要用的金鑰、接到 home.toml（不用開 serve；已經有就不動）", Group: cmdline.GroupSystem, Run: pushCommand},
		"share":     {Usage: "share <路徑> [--agent <職位>]", Group: cmdline.GroupWork, Run: shareCmd},
	}
}

// sendCmd 發一則通知；在秘書的 session 裡沒給 --agent 就算那位秘書發的。
func sendCmd(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.NotifySendRequest](e, a, cmdline.Spec{Split: map[string]string{"options": "|"}})
	if err != nil {
		return err
	}
	if !p.Set["agent"] && e.By != cmdline.DefaultBy {
		p.Req.Agent = e.By
	}
	return cmdline.Send(e, p, api(e).Send, func(r *sdk.NotifySendResponse) any { return r.Notification })
}

// shareCmd 把一個本機檔案交給 notify.share，只印一行網址：伺服器回了 web.public_url（PublicBase）就印完整網址，
// 沒有就印 /share/<名字>。--agent 沒給就用 $EVERGENIE_AGENT。
func shareCmd(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.ShareRequest](e, a, cmdline.Spec{Free: true, Skip: []string{"path"}})
	if err != nil {
		return err
	}
	if len(p.Pos) != 1 {
		return cmdline.Usagef("用法：evergenie share <路徑> [--agent <職位>]")
	}
	path, err := filepath.Abs(p.Pos[0])
	if err != nil {
		return cmdline.Usagef("路徑不正確：%s", p.Pos[0])
	}
	p.Req.Path = path
	if !p.Set["agent"] && e.By != cmdline.DefaultBy {
		p.Req.Agent = e.By
	}
	resp, err := api(e).Share(e.Ctx, p.Req)
	if err != nil {
		return err
	}
	_, err = e.Out.Write([]byte(resp.PublicBase + resp.URL + "\n"))
	return err
}

func api(e *cmdline.Env) sdk.NotifyAPI { return sdk.NewNotifyClient(e.Client) }
