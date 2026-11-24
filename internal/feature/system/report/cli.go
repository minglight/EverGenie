package report

import (
	"fmt"
	"os"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Collect 是「跑一次健檢」（doctor.Collect 加上 registry 給的離線檢查）：由組裝點傳進來，report 不 import doctor。
type Collect func(e *cmdline.Env, home string) []sdk.DoctorCheck

const usage = "用法：evergenie report [--detail] [--out <檔>] [--json] [--home <家>]"

// Commands 登記 `evergenie report`（套件層級函式）。在指令行程裡直接讀家與日誌，服務沒起來也能產生（RPT-1）。
func Commands(collect Collect) map[string]app.Command {
	return map[string]app.Command{"report": {
		Usage: "report [--detail] [--out <檔>] [--json] [--home <目錄>]    產生問題回報的內容（版本、系統、健檢、錯誤的標題與次數；--detail 多帶遮過的日誌尾段）。只印出來或存檔，不會送出任何東西",
		Group: cmdline.GroupSystem,
		Run:   func(e *cmdline.Env, args []string) error { return run(e, args, collect) },
	}}
}

func run(e *cmdline.Env, args []string, collect Collect) error {
	detail, asJSON, home, out := false, false, "", ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--detail":
			detail = true
		case a == "--json":
			asJSON = true
		case a == "--home" && i+1 < len(args):
			i++
			home = args[i]
		case strings.HasPrefix(a, "--home="):
			home = strings.TrimPrefix(a, "--home=")
		case a == "--out" && i+1 < len(args):
			i++
			out = args[i]
		case strings.HasPrefix(a, "--out="):
			out = strings.TrimPrefix(a, "--out=")
		default:
			return cmdline.Usagef(usage)
		}
	}
	if home == "" {
		home = cmdline.EnvHome()
	}
	var checks []sdk.DoctorCheck
	if collect != nil {
		checks = collect(e, home)
	}
	b := Build(e.Ctx, Options{Home: home, Detail: detail, Checks: checks})
	if asJSON {
		return e.Line(&sdk.SystemReportResponse{Title: b.Title, Body: b.Body, IssueLink: b.Link})
	}
	text := "# " + b.Title + "\n\n" + b.Body
	if out != "" {
		if err := os.WriteFile(out, []byte(text), 0o600); err != nil {
			return fmt.Errorf("存不了回報檔（%s）：%w", out, err)
		}
		_, err := fmt.Fprintf(e.Out, "回報的內容存在 %s。沒有送出任何東西；要回報，把這個檔傳給提供程式的人。\n", out)
		return err
	}
	_, err := fmt.Fprint(e.Out, text+"\n（上面只是印出來，沒有送出任何東西；要回報，把這段內容傳給提供程式的人。）\n")
	return err
}
