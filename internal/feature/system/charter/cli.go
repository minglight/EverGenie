package charter

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie rules …` 的指令表（套件層級函式）：給秘書讀規矩、看版本、送修改提案。
// 改文件、還原、採用或退回提案只有主人能做，在看板「設定 › 規矩」按（核心也會擋秘書）。指令行程只是 client（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"rules":           {Usage: "rules list|show|history|diff|propose|proposals   規矩（憲法、宗旨、法律…）：讀、看版本、送修改提案（evergenie rules --help 看細節）", Group: cmdline.GroupSystem, Run: cmdHelp},
		"rules list":      {Run: cmdList},
		"rules show":      {Run: cmdShow},
		"rules history":   {Run: cmdHistory},
		"rules diff":      {Run: cmdDiff},
		"rules propose":   {Run: cmdPropose},
		"rules proposals": {Run: cmdProposals},
	}
}

func api(e *cmdline.Env) sdk.RulesAPI { return sdk.NewRulesClient(e.Client) }

const usage = `用法：evergenie rules <子指令>
  list                              列出規矩文件（照位階，最高的在前）
  show <文件> [--version RV-n]      印出全文（例：rules show law）；--version 印那一版
  history <文件>                    列出版本（新的在前）
  diff <RV-n> [--against RV-m|current]
                                    這一版跟前一版（或指定的那一版、現在的文件）差在哪（- 刪掉、+ 加上）
  propose <文件> --why <為什麼要改> [--section <標題>] (--text <內容> | --file <檔案> | --file -)
                                    送修改提案給主人（不改文件）：沒有 --section 是整份的新全文；
                                    有 --section 是文件裡那個標題底下的新內容（標題文字，不含 #）。--file - 從標準輸入讀
  proposals [<文件>] [--status pending|accepted|rejected]
                                    列出提案
改文件、還原、採用或退回提案只有主人能做（看板「設定 › 規矩」）。
`

func cmdHelp(e *cmdline.Env, a []string) error {
	for _, x := range a {
		if x != "--help" && x != "-h" && x != "help" {
			return cmdline.Usagef("不認得的子指令：rules %s（evergenie rules --help 看用法）", x)
		}
	}
	_, err := fmt.Fprint(e.Out, usage)
	return err
}

// opts 解析一個子指令的參數（cmdline.ParseOpts：不認得的旗標回用法錯誤，--help 印用法）；want 是要幾個位置參數（-1＝0 或 1 個）。
func opts(a []string, withValue []string, want int, what string) (cmdline.Opts, error) {
	o, err := cmdline.ParseOpts(a, withValue, nil, strings.TrimPrefix(usage, "用法："))
	if err != nil {
		return o, err
	}
	switch {
	case want == -1 && len(o.Pos) > 1:
		return o, cmdline.Usagef("最多一個%s（evergenie rules --help 看用法）", what)
	case want >= 0 && len(o.Pos) != want:
		if want == 0 {
			return o, cmdline.Usagef("這個指令不用參數（evergenie rules --help 看用法）")
		}
		return o, cmdline.Usagef("需要一個%s（evergenie rules --help 看用法）", what)
	}
	return o, nil
}

func cmdList(e *cmdline.Env, a []string) error {
	if _, err := opts(a, nil, 0, ""); err != nil {
		return err
	}
	resp, err := api(e).List(e.Ctx, &sdk.RulesListRequest{})
	if err != nil {
		return err
	}
	if resp.Problem != "" {
		fmt.Fprintf(e.Err, "設定有問題：%s\n", resp.Problem)
	}
	if len(resp.Docs) == 0 {
		_, err := fmt.Fprintln(e.Out, "還沒有規矩文件")
		return err
	}
	for _, d := range resp.Docs {
		state := ""
		if !d.Exists {
			state = "（還沒有內容）"
		}
		if d.Pending > 0 {
			state += fmt.Sprintf("（%d 個提案待決）", d.Pending)
		}
		where := d.Path
		if d.Section != "" {
			where += " #" + d.Section
		}
		if _, err := fmt.Fprintf(e.Out, "%d. %s　%s　%s%s\n", d.Rank, d.ID, d.Title, where, state); err != nil {
			return err
		}
	}
	return nil
}

func cmdShow(e *cmdline.Env, a []string) error {
	o, err := opts(a, []string{"version"}, 1, "文件（例：law）")
	if err != nil {
		return err
	}
	doc := o.Pos[0]
	if version, ok := o.Vals["version"]; ok {
		resp, err := api(e).Version(e.Ctx, &sdk.RulesVersionRequest{ID: version})
		if err != nil {
			return err
		}
		if resp.Version == nil || resp.Version.Doc != doc {
			return cmdline.Usagef("%s 不是 %s 的版本", version, doc)
		}
		_, err = fmt.Fprintln(e.Out, resp.Version.Text)
		return err
	}
	resp, err := api(e).Get(e.Ctx, &sdk.RulesGetRequest{Doc: doc})
	if err != nil {
		return err
	}
	if resp.Doc != nil && !resp.Doc.Exists {
		_, err := fmt.Fprintf(e.Out, "（%s 還沒有內容：%s）\n", resp.Doc.Title, resp.Doc.Path)
		return err
	}
	_, err = fmt.Fprintln(e.Out, resp.Text)
	return err
}

func cmdHistory(e *cmdline.Env, a []string) error {
	o, err := opts(a, nil, 1, "文件（例：law）")
	if err != nil {
		return err
	}
	resp, err := api(e).History(e.Ctx, &sdk.RulesHistoryRequest{Doc: o.Pos[0]})
	if err != nil {
		return err
	}
	if len(resp.Versions) == 0 {
		_, err := fmt.Fprintln(e.Out, "還沒有版本（第一次經看板改的時候會先留一版原本的樣子）")
		return err
	}
	for _, v := range resp.Versions {
		line := fmt.Sprintf("%s　%s　%s　%s", v.ID, v.CreatedAt.Format("2006-01-02 15:04"), sourceLabel(v.Source), v.By)
		if v.Note != "" {
			line += "　" + v.Note
		}
		if _, err := fmt.Fprintln(e.Out, line); err != nil {
			return err
		}
	}
	return nil
}

func sourceLabel(s sdk.RulesVersionSource) string {
	switch s {
	case sdk.RulesVersionSourceSave:
		return "主人改的"
	case sdk.RulesVersionSourceRestore:
		return "還原"
	case sdk.RulesVersionSourceProposal:
		return "採用提案"
	case sdk.RulesVersionSourceOutside:
		return "改之前的樣子"
	}
	return string(s)
}

func cmdDiff(e *cmdline.Env, a []string) error {
	o, err := opts(a, []string{"against"}, 1, "版本（例：RV-3）")
	if err != nil {
		return err
	}
	id := o.Pos[0]
	resp, err := api(e).Version(e.Ctx, &sdk.RulesVersionRequest{ID: id, Against: o.Vals["against"]})
	if err != nil {
		return err
	}
	base := resp.Against
	if base == "" {
		base = "（空的，這是第一版）"
	}
	fmt.Fprintf(e.Out, "%s → %s\n", base, id)
	return printDiff(e.Out, resp.Diff)
}

func printDiff(w io.Writer, diff []sdk.RulesDiffLine) error {
	for _, l := range diff {
		mark := "  "
		switch l.Op {
		case sdk.RulesDiffOpAdd:
			mark = "+ "
		case sdk.RulesDiffOpDel:
			mark = "- "
		}
		if _, err := fmt.Fprintln(w, mark+l.Text); err != nil {
			return err
		}
	}
	return nil
}

func cmdPropose(e *cmdline.Env, a []string) error {
	o, err := opts(a, []string{"why", "section", "text", "file"}, 1, "文件（例：law）")
	if err != nil {
		return err
	}
	doc, why, section := o.Pos[0], o.Vals["why"], o.Vals["section"]
	text, hasText := o.Vals["text"]
	file, hasFile := o.Vals["file"]
	if hasText == hasFile {
		return cmdline.Usagef("新內容用 --text 或 --file 給一個（--file - 從標準輸入讀）")
	}
	if hasFile {
		var data []byte
		if file == "-" {
			data, err = io.ReadAll(io.LimitReader(e.In, MaxTextBytes+1))
		} else {
			data, err = os.ReadFile(file)
		}
		if err != nil {
			return fmt.Errorf("讀不到 %s：%w", file, err)
		}
		text = string(data)
	}
	resp, err := api(e).Propose(e.Ctx, &sdk.RulesProposeRequest{Doc: doc, Section: section, Text: text, Why: why, By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	return e.Line(resp.Proposal)
}

func cmdProposals(e *cmdline.Env, a []string) error {
	o, err := opts(a, []string{"status"}, -1, "文件（例：rules proposals law）")
	if err != nil {
		return err
	}
	req := &sdk.RulesProposalsRequest{}
	if status, ok := o.Vals["status"]; ok {
		req.Statuses = []sdk.RulesProposalStatus{sdk.RulesProposalStatus(status)}
	}
	if len(o.Pos) == 1 {
		req.Doc = o.Pos[0]
	}
	resp, err := api(e).Proposals(e.Ctx, req)
	if err != nil {
		return err
	}
	for _, p := range resp.Proposals {
		p.Diff = nil
		if err := e.Line(p); err != nil {
			return err
		}
	}
	return nil
}
