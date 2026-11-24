package roster

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie agents|customize|memory|journal|roster …` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module、不啟動任何東西，只用 sdk 的 client 打 API（CLI-1）。
// 名冊與設定的寫入規則（只有主人、先備份、失敗還原）在底層 agent；這裡只負責旗標與要給人看的白話。
// stateLabel 把 session 狀態換成白話（registry 傳 chat 業務模組的 StateLabel，業務模組之間不互相 import；nil 就印原始狀態）。
func Commands(stateLabel func(sdk.Session) string) map[string]app.Command {
	if stateLabel == nil {
		stateLabel = func(s sdk.Session) string { return string(s.State) }
	}
	return map[string]app.Command{
		"agents list":      {Usage: "agents list|new|rm|mv|assign|swap|account|set|link|knowledge\n秘書名冊（new <id> --persona …；rm 封存不刪除；mv 連記憶排程一起搬；assign／swap 換角色但記憶不動；account 改帳號；link 預演各帳號的 agents/<id>.md，--apply 才寫；knowledge <id> [--enabled a,b] 看或換勾的可選知識）", Group: cmdline.GroupAgents, Run: agentsList},
		"agents new":       {Run: agentsNew},
		"agents rm":        {Run: agentsRm},
		"agents mv":        {Run: func(e *cmdline.Env, args []string) error { return agentsMv(e, args, stateLabel) }},
		"agents assign":    {Run: agentsAssign},
		"agents swap":      {Run: agentsSwap},
		"agents account":   {Run: agentsAccount},
		"agents set":       {Run: agentsSet},
		"agents link":      {Run: agentsLink},
		"agents knowledge": {Run: agentsKnowledge},
		"customize add":    {Usage: "customize add|show <id> [--section <小節>] [規則]    使用者交代的新規則（附加規則，官方更新不會蓋掉；--section 加進附加規則裡同名的一段）", Group: cmdline.GroupAgents, Run: customizeAdd},
		"customize show":   {Run: customizeShow},
		"memory show":      {Usage: "memory show [<職位>] | add <一行> | write --file <路徑>|- | --body <文字> | note list|show|write\n秘書自己的長期記憶（只有本人寫，身分是 $EVERGENIE_AGENT；add 接在尾端，write 整份取代）；note 是自己的做事筆記（只有這個職位用的長文件：計畫、清單、草稿）：note list [--agent <職位>]、note show <名字> [--agent <職位>]、note write <名字> --file <路徑>|- [--revision <show 看到的版本>]（核心補檔頭；新檔不用版本，改舊檔要版本）", Group: cmdline.GroupAgents, Run: memoryShow},
		"memory add":       {Run: memoryAdd},
		"memory write":     {Run: memoryWrite},
		"memory note":      {Run: memoryNote},
		"journal add":      {Usage: "journal add <一行> | show [<職位>] [--date YYYY-MM-DD]   秘書自己的日誌（只能追加，時間由核心加）", Group: cmdline.GroupAgents, Run: journalAdd},
		"journal show":     {Run: journalShow},
		"roster":           {Usage: "roster                            秘書名冊（一位一行）", Group: cmdline.GroupAgents, Run: roster},
	}
}

func api(e *cmdline.Env) sdk.AgentsAPI         { return sdk.NewAgentsClient(e.Client) }
func vaultAPI(e *cmdline.Env) sdk.VaultAPI     { return sdk.NewVaultClient(e.Client) }
func sessionAPI(e *cmdline.Env) sdk.SessionAPI { return sdk.NewSessionClient(e.Client) }
func actor(e *cmdline.Env) *sdk.Actor          { return &sdk.Actor{Name: e.By} }

const (
	agentsNewUsage     = "evergenie agents new <id> --persona <人設> [--display <名稱>] [--duty <一句話職責>]"
	agentsMvUsage      = "evergenie agents mv <舊 id> <新 id> [--display <名稱>]"
	agentsAccountUsage = "evergenie agents account <id> <帳號> [--model …] [--effort …]"
)

// stopResident 停掉這位的常駐 session；沒在跑回 false。
func stopResident(e *cmdline.Env, id string) (bool, error) {
	r, err := sessionAPI(e).List(e.Ctx, &sdk.SessionListRequest{Agent: id})
	if err != nil {
		return false, err
	}
	list := r.Sessions
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	for _, s := range list {
		if s.Kind != sdk.SessionKindResident || s.State == sdk.SessionStateStopped {
			continue
		}
		if _, err := sessionAPI(e).Close(e.Ctx, &sdk.SessionCloseRequest{Name: s.Name}); err != nil {
			var se *sdk.Error
			if !errors.As(err, &se) || se.Code != sdk.ErrorCodeNotFound {
				return false, err
			}
		}
		return true, nil
	}
	return false, nil
}

func recordLine(r *sdk.Record) string {
	if r == nil {
		return ""
	}
	name, _ := r.Fields["display_name"].(string)
	persona, _ := r.Fields["persona"].(string)
	return fmt.Sprintf("%s（人設 %s%s）", r.ID, persona, map[bool]string{true: "，顯示名稱 " + name, false: ""}[name != ""])
}

// roster：名冊一行一筆 JSON（含停用的）。
func roster(e *cmdline.Env, a []string) error {
	if len(a) > 0 {
		return cmdline.Usagef("roster 不需要參數")
	}
	req := &sdk.VaultListRequest{Kind: "agent"}
	seen := map[string]bool{}
	for {
		resp, err := vaultAPI(e).List(e.Ctx, req)
		if err != nil {
			return err
		}
		for _, r := range resp.Records {
			if err := e.Line(r); err != nil {
				return err
			}
		}
		next := cmdline.NextCursor(resp.Page)
		if next == "" || seen[next] {
			return nil
		}
		seen[next] = true
		req.Page = &sdk.PageRequest{Cursor: next}
	}
}

// agents list [--json]：名冊（含停用的）。
func agentsList(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"json"}, "evergenie agents list [--json]")
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie agents list [--json]")
	}
	req := &sdk.VaultListRequest{Kind: "agent"}
	var rows []sdk.Record
	for seen := map[string]bool{}; ; {
		r, err := vaultAPI(e).List(e.Ctx, req)
		if err != nil {
			return err
		}
		rows = append(rows, r.Records...)
		next := cmdline.NextCursor(r.Page)
		if next == "" || seen[next] {
			break
		}
		seen[next] = true
		req.Page = &sdk.PageRequest{Cursor: next}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	var b strings.Builder
	for i := range rows {
		if o.Flags["json"] {
			if err := e.Line(rows[i]); err != nil {
				return err
			}
			continue
		}
		state := "開"
		if on, _ := rows[i].Fields["enabled"].(bool); !on {
			state = "關"
		}
		account, _ := rows[i].Fields["account"].(string)
		fmt.Fprintf(&b, "%-12s %s　%s　帳號 %s\n", rows[i].ID, state, recordLine(&rows[i]), account)
	}
	if o.Flags["json"] {
		return nil
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// agents new <id> --persona …
func agentsNew(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"persona", "display", "duty"}, nil, agentsNewUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 || o.Vals["persona"] == "" {
		return cmdline.Usagef("用法：%s", agentsNewUsage)
	}
	r, err := api(e).Create(e.Ctx, &sdk.AgentsCreateRequest{ID: o.Pos[0], Persona: o.Vals["persona"], DisplayName: o.Vals["display"], Duty: o.Vals["duty"], By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已建立 %s（還沒啟用：要上線請 evergenie agents set %s enabled true；職責用 evergenie customize add %s …補）\n", recordLine(r.Agent), o.Pos[0], o.Pos[0])
	return err
}

// agents rm <id>：先停 session，再封存（不刪除）。
func agentsRm(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("用法：evergenie agents rm <id>")
	}
	if _, err := stopResident(e, args[0]); err != nil {
		return err
	}
	r, err := api(e).Archive(e.Ctx, &sdk.AgentsArchiveRequest{ID: args[0], By: actor(e)})
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已封存 %s（%d 筆資料，沒有刪除）\n", args[0], len(r.Archived))
	for _, a := range r.Archived {
		b.WriteString("  " + a + "\n")
	}
	if r.Restore != "" {
		b.WriteString("要還原：" + r.Restore + "\n")
	}
	b.WriteString("提醒：各帳號的 agents/" + args[0] + ".md 要等 evergenie agents link --apply 才會移除。\n")
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// agents mv <舊> <新> [--display …]：停 → 搬 → 原本在跑就用新名字重開。
func agentsMv(e *cmdline.Env, args []string, stateLabel func(sdk.Session) string) error {
	o, err := cmdline.ParseOpts(args, []string{"display"}, nil, agentsMvUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 2 {
		return cmdline.Usagef("用法：%s", agentsMvUsage)
	}
	old, next := o.Pos[0], o.Pos[1]
	wasRunning, err := stopResident(e, old)
	if err != nil {
		return err
	}
	r, err := api(e).Rename(e.Ctx, &sdk.AgentsRenameRequest{ID: old, NewID: next, DisplayName: o.Vals["display"], By: actor(e)})
	if err != nil {
		if wasRunning { // 沒改成，把原本在跑的開回來
			_, _ = sessionAPI(e).Open(e.Ctx, &sdk.SessionOpenRequest{Agent: old, Kind: sdk.SessionKindResident})
		}
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "已把 %s 改成 %s（搬了 %d 筆資料：記憶、附加規則、排程等）\n", old, next, len(r.Moved))
	if len(r.Mentions) > 0 {
		b.WriteString("這些檔案還提到舊的 id，請自己看一下要不要改（不會自動改內文）：\n")
		for _, m := range r.Mentions {
			b.WriteString("  " + m + "\n")
		}
	}
	if wasRunning {
		s, err := sessionAPI(e).Open(e.Ctx, &sdk.SessionOpenRequest{Agent: next, Kind: sdk.SessionKindResident})
		if err != nil {
			fmt.Fprintf(&b, "原本在跑，但用新名字重開失敗：%s（請手動 evergenie sessions start %s）\n", e.Message(err), next)
		} else {
			fmt.Fprintf(&b, "已用新名字重開：%s\n", stateLabel(*s.Session))
		}
	}
	b.WriteString("各帳號的 agents/ 檔案要等 evergenie agents link --apply 才會更新。\n")
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// agents assign <id> <人設>
func agentsAssign(e *cmdline.Env, args []string) error {
	if len(args) != 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return cmdline.Usagef("用法：evergenie agents assign <id> <人設>")
	}
	r, err := api(e).Assign(e.Ctx, &sdk.AgentsAssignRequest{ID: args[0], Persona: args[1], By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已換角色：%s（記憶與職責沒動）。常駐對話重開後才會用新角色：evergenie sessions restart %s\n", recordLine(r.Agent), args[0])
	return err
}

// agents swap <a> <b>
func agentsSwap(e *cmdline.Env, args []string) error {
	if len(args) != 2 || strings.HasPrefix(args[0], "-") || strings.HasPrefix(args[1], "-") {
		return cmdline.Usagef("用法：evergenie agents swap <a> <b>")
	}
	r, err := api(e).Swap(e.Ctx, &sdk.AgentsSwapRequest{A: args[0], B: args[1], By: actor(e)})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("已互換角色（記憶與職責沒動）：\n")
	for i := range r.Agents {
		b.WriteString("  " + recordLine(&r.Agents[i]) + "\n")
	}
	b.WriteString("常駐對話重開後才會用新角色。\n")
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// agents account <id> <帳號> [--model …] [--effort …]
func agentsAccount(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"model", "effort"}, nil, agentsAccountUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 2 {
		return cmdline.Usagef("用法：%s", agentsAccountUsage)
	}
	r, err := api(e).SetAccount(e.Ctx, &sdk.AgentsSetAccountRequest{ID: o.Pos[0], Account: o.Pos[1], Model: o.Vals["model"], Effort: o.Vals["effort"], By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已把 %s 改用帳號 %s（舊設定已備份在 vault 的變更紀錄）。常駐對話重開後才生效：evergenie sessions restart %s\n", r.Agent.ID, o.Pos[1], o.Pos[0])
	return err
}

// agents set <id> <欄位> <值>
func agentsSet(e *cmdline.Env, args []string) error {
	if len(args) != 3 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("用法：evergenie agents set <id> <欄位> <值>")
	}
	r, err := api(e).SetField(e.Ctx, &sdk.AgentsSetFieldRequest{ID: args[0], Field: args[1], Value: args[2], By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已更新 %s 的 %s\n", r.Agent.ID, args[1])
	return err
}

const agentsKnowledgeUsage = "evergenie agents knowledge <職位> [--enabled <id>,<id>]（--enabled= 全部不勾）"

// agents knowledge <職位> [--enabled a,b]：沒給 --enabled 只列出；給了整份換掉（只有主人）。
func agentsKnowledge(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"enabled"}, nil, agentsKnowledgeUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：%s", agentsKnowledgeUsage)
	}
	var r *sdk.AgentsKnowledgeResponse
	if v, set := o.Vals["enabled"]; set {
		ids := []string{}
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		r, err = api(e).SetKnowledge(e.Ctx, &sdk.AgentsSetKnowledgeRequest{Agent: o.Pos[0], Enabled: ids, By: actor(e)})
	} else {
		r, err = api(e).Knowledge(e.Ctx, &sdk.AgentsKnowledgeRequest{Agent: o.Pos[0]})
	}
	if err != nil {
		return err
	}
	if len(r.Blocks) == 0 {
		_, err = fmt.Fprintf(e.Out, "%s 的職責沒有可選知識\n", o.Pos[0])
		return err
	}
	var b strings.Builder
	for _, k := range r.Blocks {
		mark := "[ ]"
		if k.Enabled {
			mark = "[x]"
		}
		def := "預設不勾"
		if k.Default {
			def = "預設勾"
		}
		fmt.Fprintf(&b, "%s %-12s %s（%s）%s\n", mark, k.ID, k.Title, def, k.Summary)
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

// agents link [--apply]：預設只預演。
func agentsLink(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"apply"}, "evergenie agents link [--apply]")
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie agents link [--apply]")
	}
	r, err := api(e).Link(e.Ctx, &sdk.AgentsLinkRequest{Apply: o.Flags["apply"], By: actor(e)})
	if err != nil {
		return err
	}
	var b strings.Builder
	if r.Applied {
		b.WriteString("已寫入：\n")
	} else {
		b.WriteString("預演（沒有寫任何檔），加 --apply 才會寫：\n")
	}
	for _, a := range r.Accounts {
		fmt.Fprintf(&b, "【帳號 %s】\n", a.Account)
		if a.Note != "" {
			b.WriteString("  " + a.Note + "\n")
		}
		for _, f := range a.Files {
			fmt.Fprintf(&b, "  %-7s %s　%s\n", linkActions[f.Action], f.Path, f.Summary)
		}
	}
	if len(r.Accounts) == 0 {
		b.WriteString("  （沒有設定任何帳號）\n")
	}
	_, err = fmt.Fprint(e.Out, b.String())
	return err
}

var linkActions = map[string]string{"create": "新增", "update": "更新", "same": "沒變", "skip": "略過", "remove": "移除"}

// customize add <id> [--section <小節>] <規則…> / customize show <id>
func customizeAdd(e *cmdline.Env, args []string) error {
	usage := "用法：evergenie customize add <id> [--section <小節>] <規則原文>"
	section, args, err := cutSection(args, usage)
	if err != nil {
		return err
	}
	if len(args) < 2 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("%s", usage)
	}
	r, err := api(e).AddRule(e.Ctx, &sdk.AgentsAddRuleRequest{ID: args[0], Section: section, Rule: strings.Join(args[1:], " "), By: actor(e)})
	if err != nil {
		return err
	}
	where := "附加規則"
	if section != "" {
		where += "「" + section + "」那一段"
	}
	_, err = fmt.Fprintf(e.Out, "已加進 %s 的%s（核心更新不會蓋掉）。目前共 %d 行。常駐對話重開後才讀到：evergenie sessions restart %s\n", args[0], where, strings.Count(strings.TrimRight(r.Addendum, "\n"), "\n")+1, args[0])
	return err
}

// cutSection 從 customize add 的參數拿出 `--section <小節>`（或 `--section=<小節>`）；其他參數原樣留著當規則原文。
func cutSection(args []string, usage string) (string, []string, error) {
	section, rest := "", make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--section":
			if i+1 >= len(args) {
				return "", nil, cmdline.Usagef("%s", usage)
			}
			i++
			section = args[i]
		case strings.HasPrefix(a, "--section="):
			section = strings.TrimPrefix(a, "--section=")
		default:
			rest = append(rest, a)
		}
	}
	return section, rest, nil
}

func customizeShow(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return cmdline.Usagef("用法：evergenie customize show <id>")
	}
	r, err := api(e).Customizations(e.Ctx, &sdk.AgentsCustomizationsRequest{ID: args[0]})
	if err != nil {
		return err
	}
	add := strings.TrimRight(r.Addendum, "\n")
	if add == "" {
		add = "（沒有）"
	}
	_, err = fmt.Fprintf(e.Out, "【官方職責（只讀）】\n%s\n\n【你加的規則】\n%s\n", strings.TrimRight(r.Duties, "\n"), add)
	return err
}
