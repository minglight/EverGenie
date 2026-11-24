package settings

import (
	"errors"
	"fmt"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

// 看板連多台的指令（S3-3）：web sites|allow|owner。都走 web 服務的 get_sites／set_*（跟看板同一套），
// add／rm 是「讀整份 → 改一筆 → 整份寫回」；比對重複用 platform.NormalizeOrigin（跟伺服器同一套寫法）。

const (
	sitesHelp = "  evergenie web sites list [--json]                 列出這個看板還要連的別台\n" +
		"  evergenie web sites add <網址> [--color #rrggbb]   加一台（https://主機[:port]，不帶路徑；顏色不寫用預設色）\n" +
		"  evergenie web sites rm <網址>                      拿掉一台\n"
	allowHelp = "  evergenie web allow list [--json]   列出可以連進來的別台看板，和主人的 tailnet 登入\n" +
		"  evergenie web allow add <網址>      允許一台（https://主機[:port]，不帶路徑）\n" +
		"  evergenie web allow rm <網址>       拿掉一台\n"
	ownerHelp = "  evergenie web owner set <login>   主人的 tailnet 登入（例 amy@example.com）；別台的看板連進來要對得上\n" +
		"  evergenie web owner clear         拿掉（不開放別台的看板連進來）\n"
)

func webCommands() map[string]app.Command {
	return map[string]app.Command{
		"web sites": {Usage: "web sites|allow|owner\n看板連多台：sites 這個看板還要連的別台、allow 哪些別台的看板可以連進來、owner 主人的 tailnet 登入（evergenie web sites --help 看細節）", Group: cmdline.GroupSystem, Run: func(e *cmdline.Env, args []string) error {
			return sub(e, args, sitesHelp, map[string]func(*cmdline.Env, []string) error{"list": sitesList, "add": sitesAdd, "rm": sitesRm})
		}},
		"web allow": {Run: func(e *cmdline.Env, args []string) error {
			return sub(e, args, allowHelp, map[string]func(*cmdline.Env, []string) error{"list": allowList, "add": allowAdd, "rm": allowRm})
		}},
		"web owner": {Run: func(e *cmdline.Env, args []string) error {
			return sub(e, args, ownerHelp, map[string]func(*cmdline.Env, []string) error{"set": ownerSet, "clear": ownerClear})
		}},
	}
}

// sub 依第一個參數找子指令；--help 印用法（結束 0），沒給或不認得是用法錯誤。
func sub(e *cmdline.Env, args []string, help string, subs map[string]func(*cmdline.Env, []string) error) error {
	if len(args) > 0 {
		if args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
			return &cmdline.HelpText{Text: "用法：\n" + help}
		}
		if run, ok := subs[args[0]]; ok {
			return run(e, args[1:])
		}
	}
	return cmdline.Usagef("用法：\n%s", strings.TrimRight(help, "\n"))
}

func web(e *cmdline.Env) sdk.WebAPI { return sdk.NewWebClient(e.Client) }

// sameOrigin 照伺服器的寫法比兩個網址；整理不了的照原字串比（交給伺服器回白話錯誤）。
func sameOrigin(a, b string) bool {
	if na, ok := platform.NormalizeOrigin(a); ok {
		a = na
	}
	if nb, ok := platform.NormalizeOrigin(b); ok {
		b = nb
	}
	return a == b
}

// one 解析「一個位置參數＋旗標」的子指令。
func one(args []string, withValue []string, usage string) (string, cmdline.Opts, error) {
	o, err := cmdline.ParseOpts(args, withValue, nil, usage)
	if err != nil {
		return "", o, err
	}
	if len(o.Pos) != 1 || strings.TrimSpace(o.Pos[0]) == "" {
		return "", o, cmdline.Usagef("用法：%s", usage)
	}
	return o.Pos[0], o, nil
}

// ---- web sites ----

func sitesList(e *cmdline.Env, args []string) error {
	const usage = "evergenie web sites list [--json]"
	o, err := cmdline.ParseOpts(args, nil, []string{"json"}, usage)
	if err != nil || len(o.Pos) > 0 {
		return orUsage(err, usage)
	}
	r, err := web(e).GetSites(e.Ctx, &sdk.WebGetSitesRequest{})
	if err != nil {
		return err
	}
	if o.Flags["json"] {
		return e.Line(r.Sites)
	}
	if len(r.Sites) == 0 {
		_, err = fmt.Fprintln(e.Out, "這個看板沒有要連的別台（evergenie web sites add <網址> 加一台）")
		return err
	}
	for _, s := range r.Sites {
		color := s.Color
		if color == "" {
			color = "（預設色）"
		}
		if _, err := fmt.Fprintf(e.Out, "%s  %s\n", s.URL, color); err != nil {
			return err
		}
	}
	return nil
}

func sitesAdd(e *cmdline.Env, args []string) error {
	url, o, err := one(args, []string{"color"}, "evergenie web sites add <網址> [--color #rrggbb]")
	if err != nil {
		return err
	}
	r, err := web(e).GetSites(e.Ctx, &sdk.WebGetSitesRequest{})
	if err != nil {
		return err
	}
	for _, s := range r.Sites {
		if sameOrigin(s.URL, url) {
			_, err := fmt.Fprintf(e.Out, "已經在清單裡：%s（沒有改動；要換顏色先 rm 再 add）\n", s.URL)
			return err
		}
	}
	sites := append(r.Sites, sdk.WebSite{URL: url, Color: o.Vals["color"]})
	res, err := web(e).SetSites(e.Ctx, &sdk.WebSetSitesRequest{Sites: sites, By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已加入 %s（現在 %d 台；舊檔備份在 %s；看板重新整理就看得到）\n", res.Sites[len(res.Sites)-1].URL, len(res.Sites), res.Backup)
	return err
}

func sitesRm(e *cmdline.Env, args []string) error {
	url, _, err := one(args, nil, "evergenie web sites rm <網址>")
	if err != nil {
		return err
	}
	r, err := web(e).GetSites(e.Ctx, &sdk.WebGetSitesRequest{})
	if err != nil {
		return err
	}
	kept := make([]sdk.WebSite, 0, len(r.Sites))
	for _, s := range r.Sites {
		if !sameOrigin(s.URL, url) {
			kept = append(kept, s)
		}
	}
	if len(kept) == len(r.Sites) {
		return notListed(url, "evergenie web sites list")
	}
	res, err := web(e).SetSites(e.Ctx, &sdk.WebSetSitesRequest{Sites: kept, By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已拿掉 %s（還有 %d 台；舊檔備份在 %s）\n", url, len(res.Sites), res.Backup)
	return err
}

// ---- web allow ----

func allowList(e *cmdline.Env, args []string) error {
	const usage = "evergenie web allow list [--json]"
	o, err := cmdline.ParseOpts(args, nil, []string{"json"}, usage)
	if err != nil || len(o.Pos) > 0 {
		return orUsage(err, usage)
	}
	r, err := web(e).GetSites(e.Ctx, &sdk.WebGetSitesRequest{})
	if err != nil {
		return err
	}
	if o.Flags["json"] {
		return e.Line(struct {
			OwnerLogin  string   `json:"owner_login"`
			AllowBoards []string `json:"allow_boards"`
		}{r.OwnerLogin, r.AllowBoards})
	}
	var b strings.Builder
	if len(r.AllowBoards) == 0 {
		b.WriteString("沒有允許別台的看板連進來（evergenie web allow add <網址> 加一台）\n")
	}
	for _, origin := range r.AllowBoards {
		b.WriteString(origin + "\n")
	}
	if r.OwnerLogin == "" {
		b.WriteString("主人的 tailnet 登入：還沒設，別台的看板連不進來（evergenie web owner set <login>）\n")
	} else {
		b.WriteString("主人的 tailnet 登入：" + r.OwnerLogin + "\n")
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

func allowAdd(e *cmdline.Env, args []string) error {
	url, _, err := one(args, nil, "evergenie web allow add <網址>")
	if err != nil {
		return err
	}
	r, err := web(e).GetSites(e.Ctx, &sdk.WebGetSitesRequest{})
	if err != nil {
		return err
	}
	for _, origin := range r.AllowBoards {
		if sameOrigin(origin, url) {
			_, err := fmt.Fprintf(e.Out, "已經在清單裡：%s（沒有改動）\n", origin)
			return err
		}
	}
	res, err := web(e).SetAllowBoards(e.Ctx, &sdk.WebSetAllowBoardsRequest{AllowBoards: append(r.AllowBoards, url), By: actor(e)})
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Out, "已允許 %s（現在 %d 台；舊檔備份在 %s；不用重開）\n", res.AllowBoards[len(res.AllowBoards)-1], len(res.AllowBoards), res.Backup)
	if r.OwnerLogin == "" {
		fmt.Fprintln(e.Out, "注意：還沒設主人的 tailnet 登入，別台的看板還是連不進來（evergenie web owner set <login>）")
	}
	return nil
}

func allowRm(e *cmdline.Env, args []string) error {
	url, _, err := one(args, nil, "evergenie web allow rm <網址>")
	if err != nil {
		return err
	}
	r, err := web(e).GetSites(e.Ctx, &sdk.WebGetSitesRequest{})
	if err != nil {
		return err
	}
	kept := make([]string, 0, len(r.AllowBoards))
	for _, origin := range r.AllowBoards {
		if !sameOrigin(origin, url) {
			kept = append(kept, origin)
		}
	}
	if len(kept) == len(r.AllowBoards) {
		return notListed(url, "evergenie web allow list")
	}
	res, err := web(e).SetAllowBoards(e.Ctx, &sdk.WebSetAllowBoardsRequest{AllowBoards: kept, By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已拿掉 %s（還有 %d 台；舊檔備份在 %s）\n", url, len(res.AllowBoards), res.Backup)
	return err
}

// ---- web owner ----

func ownerSet(e *cmdline.Env, args []string) error {
	login, _, err := one(args, nil, "evergenie web owner set <login>")
	if err != nil {
		return err
	}
	res, err := web(e).SetOwnerLogin(e.Ctx, &sdk.WebSetOwnerLoginRequest{OwnerLogin: login, By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "主人的 tailnet 登入改成 %s（舊檔備份在 %s；不用重開）\n", res.OwnerLogin, res.Backup)
	return err
}

func ownerClear(e *cmdline.Env, args []string) error {
	const usage = "evergenie web owner clear"
	o, err := cmdline.ParseOpts(args, nil, nil, usage)
	if err != nil || len(o.Pos) > 0 {
		return orUsage(err, usage)
	}
	res, err := web(e).SetOwnerLogin(e.Ctx, &sdk.WebSetOwnerLoginRequest{By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已拿掉主人的 tailnet 登入：別台的看板連不進來了（舊檔備份在 %s）\n", res.Backup)
	return err
}

func notListed(url, listCmd string) error {
	return &sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "清單裡沒有 " + url + "（" + listCmd + " 看現有的）"}
}

// orUsage：--help 照樣往上傳（印用法、結束 0），其他解析錯誤一律回用法。
func orUsage(err error, usage string) error {
	var he *cmdline.HelpText
	if errors.As(err, &he) {
		return err
	}
	return cmdline.Usagef("用法：%s", usage)
}
