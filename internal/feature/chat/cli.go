package chat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// Commands 是 `evergenie sessions|status|say …` 的指令表（套件層級函式，不是 Module 的方法）：
// 指令行程只是 client，不建 Module、不啟動任何東西，只用 sdk 的 client 打 API（CLI-1）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"sessions list":    {Usage: "sessions list|opened|open|close|start|stop|restart|rotate|peek|answer|attach\nsession 維運（open <職位> --repo --topic --slug --resume；rotate --all；attach 印出接上畫面的指令）", Group: cmdline.GroupAgents, Run: sessionsList},
		"sessions opened":  {Run: sessionsOpened},
		"sessions open":    {Run: sessionsOpen},
		"sessions close":   {Run: sessionsClose},
		"sessions start":   {Run: sessionsStart},
		"sessions stop":    {Run: sessionsStop},
		"sessions restart": {Run: sessionsRestart},
		"sessions rotate":  {Run: sessionsRotate},
		"sessions peek":    {Run: sessionsPeek},
		"sessions answer":  {Run: sessionsAnswer},
		"sessions attach":  {Run: sessionsAttach},
		"status":           {Usage: "status                            每位秘書的常駐 session 狀態", Group: cmdline.GroupAgents, Run: status},
		"say":              {Usage: "say <職位> <內容>                 傳話給本家的秘書", Group: cmdline.GroupAgents, Run: say},
		"voice answer":     {Usage: "voice answer <V-代號> <一兩句>    回答主人用語音問的問題（捷徑會唸出來）", Group: cmdline.GroupAgents, Run: voiceAnswer},
	}
}

func sessionAPI(e *cmdline.Env) sdk.SessionAPI { return sdk.NewSessionClient(e.Client) }
func busAPI(e *cmdline.Env) sdk.BusAPI         { return sdk.NewBusClient(e.Client) }
func vaultAPI(e *cmdline.Env) sdk.VaultAPI     { return sdk.NewVaultClient(e.Client) }
func actor(e *cmdline.Env) *sdk.Actor          { return &sdk.Actor{Name: e.By} }

func listSessions(e *cmdline.Env, agent string) ([]sdk.Session, error) {
	r, err := sessionAPI(e).List(e.Ctx, &sdk.SessionListRequest{Agent: agent})
	if err != nil {
		return nil, err
	}
	out := r.Sessions
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func printSessions(e *cmdline.Env, list []sdk.Session, empty string) error {
	var b strings.Builder
	if len(list) == 0 {
		b.WriteString(empty + "\n")
	}
	for _, s := range list {
		fmt.Fprintf(&b, "%-22s %-10s %-9s %s", s.Name, s.Agent, s.Kind, StateLabel(s))
		if s.Account != "" {
			fmt.Fprintf(&b, "　帳號 %s", s.Account)
		}
		if (s.Kind == sdk.SessionKindRoom || s.Kind == sdk.SessionKindWork) && s.Cwd != "" {
			fmt.Fprintf(&b, "　%s", s.Cwd)
		}
		b.WriteString("\n")
	}
	_, err := e.Out.Write([]byte(b.String()))
	return err
}

// sessions list [--agent <職位>] [--json]
func sessionsList(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"agent"}, []string{"json"}, "evergenie sessions list [--agent <職位>] [--json]")
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie sessions list [--agent <職位>] [--json]")
	}
	list, err := listSessions(e, o.Vals["agent"])
	if err != nil {
		return err
	}
	if o.Flags["json"] {
		for _, s := range list {
			if err := e.Line(s); err != nil {
				return err
			}
		}
		return nil
	}
	return printSessions(e, list, "（沒有 session）")
}

// sessions opened：用 open 開的專屬對話（不含常駐、臨時、分身）。
func sessionsOpened(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"json"}, "evergenie sessions opened [--json]")
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie sessions opened [--json]")
	}
	list, err := listSessions(e, "")
	if err != nil {
		return err
	}
	rooms := []sdk.Session{}
	for _, s := range list {
		if s.Kind == sdk.SessionKindRoom || (s.Kind == sdk.SessionKindWork && s.Task != "") {
			rooms = append(rooms, s)
		}
	}
	if o.Flags["json"] {
		for _, s := range rooms {
			if err := e.Line(s); err != nil {
				return err
			}
		}
		return nil
	}
	return printSessions(e, rooms, "（沒有用 open 開的 session）")
}

var slugJunk = regexp.MustCompile(`[^a-z0-9]+`)

// slug 把主題變成名稱的一段：小寫英數，其他字元換成 -；中文主題會變空字串。
func slug(s string) string {
	return strings.Trim(slugJunk.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

const openUsage = "evergenie sessions open <職位> [--repo <資料夾>] [--topic <主題>] [--slug <英文名>] [--resume <session 名稱>] [--first <第一句>]"

// sessions open <職位>：開一個專屬對話，名稱是 <職位>-<slug 或主題>；--resume 接回之前收掉的那個（沿用原本的資料夾與對話）。
func sessionsOpen(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"repo", "topic", "slug", "resume", "first"}, nil, openUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：%s", openUsage)
	}
	agent := o.Pos[0]

	req := &sdk.SessionOpenRequest{Agent: agent, Kind: sdk.SessionKindRoom, Topic: o.Vals["topic"], FirstMessage: o.Vals["first"], By: actor(e)}
	if repo := o.Vals["repo"]; repo != "" {
		dir, err := filepath.Abs(cmdline.ExpandHome(repo))
		if err != nil {
			return cmdline.Usagef("資料夾路徑不正確：%s", repo)
		}
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return fmt.Errorf("資料夾不存在：%s", dir)
		}
		req.Cwd = dir
	}
	if name := o.Vals["resume"]; name != "" {
		req.Name = name
	} else {
		part := slug(o.Vals["slug"])
		if o.Vals["slug"] == "" {
			part = slug(o.Vals["topic"])
			if o.Vals["topic"] == "" {
				part = "chat"
			}
		}
		if part == "" {
			return cmdline.Usagef("主題沒有英數字，請用 --slug 給一個英文名稱（例：--slug fitness）")
		}
		req.Name = agent + "-" + part
	}
	r, err := sessionAPI(e).Open(e.Ctx, req)
	if err != nil {
		return err
	}
	s := r.Session
	msg := fmt.Sprintf("已開 %s（%s，%s）\n", s.Name, s.Agent, StateLabel(*s))
	if !s.QueuedAt.IsZero() {
		msg = fmt.Sprintf("%s 先排隊：這台同時進行的工作 session 已到上限，有名額會自動開起來\n", s.Name)
	}
	if s.Attach != "" {
		msg += "接上畫面：" + s.Attach + "\n"
	}
	_, err = e.Out.Write([]byte(msg))
	return err
}

func oneName(args []string, usage string) (string, error) {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return "", cmdline.Usagef("用法：%s", usage)
	}
	return args[0], nil
}

// sessions close <名稱>：收掉一個 session（常駐也可以，等於 stop）。
func sessionsClose(e *cmdline.Env, args []string) error {
	name, err := oneName(args, "evergenie sessions close <名稱>")
	if err != nil {
		return err
	}
	if _, err := sessionAPI(e).Close(e.Ctx, &sdk.SessionCloseRequest{Name: name}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已收掉 %s\n", name)
	return err
}

// sessions start <職位>：開常駐 session（已經在跑就回現況）。
func sessionsStart(e *cmdline.Env, args []string) error {
	agent, err := oneName(args, "evergenie sessions start <職位>")
	if err != nil {
		return err
	}
	r, err := sessionAPI(e).Open(e.Ctx, &sdk.SessionOpenRequest{Agent: agent, Kind: sdk.SessionKindResident})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "%s：%s\n", r.Session.Name, StateLabel(*r.Session))
	return err
}

// sessions stop <職位>：關常駐 session。
func sessionsStop(e *cmdline.Env, args []string) error {
	agent, err := oneName(args, "evergenie sessions stop <職位>")
	if err != nil {
		return err
	}
	if _, err := sessionAPI(e).Close(e.Ctx, &sdk.SessionCloseRequest{Name: agent}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已停 %s\n", agent)
	return err
}

// sessions restart <職位>：先關再開（沒在跑就直接開）。
func sessionsRestart(e *cmdline.Env, args []string) error {
	agent, err := oneName(args, "evergenie sessions restart <職位>")
	if err != nil {
		return err
	}
	if _, err := sessionAPI(e).Close(e.Ctx, &sdk.SessionCloseRequest{Name: agent}); err != nil {
		var se *sdk.Error
		if !errors.As(err, &se) || se.Code != sdk.ErrorCodeNotFound {
			return err
		}
	}
	return sessionsStart(e, args)
}

// sessions rotate [<名稱>…] [--all]：重開清掉 context（核心換版之後、夜間整理之後）；--all＝所有在跑的常駐。
func sessionsRotate(e *cmdline.Env, args []string) error {
	const usage = "evergenie sessions rotate <名稱>… | --all"
	o, err := cmdline.ParseOpts(args, nil, []string{"all"}, usage)
	if err != nil {
		return err
	}
	names := o.Pos
	if o.Flags["all"] {
		if len(names) > 0 {
			return cmdline.Usagef("用法：%s", usage)
		}
		list, err := listSessions(e, "")
		if err != nil {
			return err
		}
		for _, s := range list {
			if s.Kind == sdk.SessionKindResident && s.State != sdk.SessionStateStopped {
				names = append(names, s.Name)
			}
		}
	}
	if len(names) == 0 {
		if o.Flags["all"] {
			_, err := fmt.Fprintln(e.Out, "沒有在跑的常駐 session")
			return err
		}
		return cmdline.Usagef("用法：%s", usage)
	}
	failed := false
	for _, n := range names {
		r, err := sessionAPI(e).Rotate(e.Ctx, &sdk.SessionRotateRequest{Name: n})
		if err != nil {
			failed = true
			fmt.Fprintf(e.Err, "%s：%s\n", n, e.Message(err))
			continue
		}
		fmt.Fprintf(e.Out, "已重開 %s（%s）\n", n, StateLabel(*r.Session))
	}
	if failed {
		return cmdline.ErrReported
	}
	return nil
}

// sessions peek <名稱> [--lines N]：看最近幾則對話（從引擎的對話記錄讀，不讀畫面）。
func sessionsPeek(e *cmdline.Env, args []string) error {
	const usage = "evergenie sessions peek <名稱> [--lines N]"
	o, err := cmdline.ParseOpts(args, []string{"lines"}, nil, usage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	n := 10
	if v := o.Vals["lines"]; v != "" {
		if n, err = strconv.Atoi(v); err != nil || n <= 0 {
			return cmdline.Usagef("--lines 要是正整數")
		}
	}
	var msgs []*sdk.ChatMessage
	err = sessionAPI(e).Chat(e.Ctx, &sdk.SessionChatRequest{Session: o.Pos[0]}, func(r *sdk.SessionChatResponse) error {
		if r.Message != nil {
			msgs = append(msgs, r.Message)
		}
		return nil
	})
	if err != nil {
		return err
	}
	var b strings.Builder
	if len(msgs) == 0 {
		b.WriteString("（還沒有對話）\n")
	}
	for _, m := range msgs[max(0, len(msgs)-n):] {
		who := map[sdk.ChatKind]string{sdk.ChatKindUser: "你", sdk.ChatKindAssistant: "秘書", sdk.ChatKindToolCall: "工具", sdk.ChatKindToolResult: "結果", sdk.ChatKindInterrupt: "中斷", sdk.ChatKindNotice: "通知"}[m.Kind]
		text := strings.TrimSpace(m.Text)
		if m.Kind == sdk.ChatKindToolCall && m.Tool != "" {
			text = m.Tool + " " + text
		}
		if r := []rune(text); len(r) > 300 {
			text = string(r[:300]) + "…"
		}
		fmt.Fprintf(&b, "[%s] %s：%s\n", m.Ts.Local().Format("15:04"), who, strings.ReplaceAll(text, "\n", "\n    "))
	}
	_, err = e.Out.Write([]byte(b.String()))
	return err
}

// sessions answer <名稱> (--allow | --deny [--message 理由] | --answer <答案>…)：代答正在等的確認或問題。
func sessionsAnswer(e *cmdline.Env, args []string) error {
	const usage = "evergenie sessions answer <名稱> (--allow | --deny [--message 理由] | --answer <答案>)"
	var answers []string
	rest := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "--answer" && i+1 < len(args) {
			answers = append(answers, args[i+1])
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	o, err := cmdline.ParseOpts(rest, []string{"message"}, []string{"allow", "deny"}, usage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	picked := 0
	for _, on := range []bool{o.Flags["allow"], o.Flags["deny"], len(answers) > 0} {
		if on {
			picked++
		}
	}
	if picked != 1 {
		return cmdline.Usagef("用法：%s", usage)
	}
	g, err := sessionAPI(e).Get(e.Ctx, &sdk.SessionGetRequest{Name: o.Pos[0]})
	if err != nil {
		return err
	}
	p := g.Session.Pending
	if p == nil {
		return fmt.Errorf("%s 現在沒有在等人回答", o.Pos[0])
	}
	d := &sdk.Decision{}
	switch {
	case len(answers) > 0:
		if p.Kind != sdk.PendingKindQuestion || len(p.Questions) != len(answers) {
			return fmt.Errorf("%s 在等的是：%s；答案數要跟題目數一樣（%d 題）", o.Pos[0], PendingLine(p), len(p.Questions))
		}
		d.Answers = map[string]string{}
		for i, q := range p.Questions {
			d.Answers[q.Text] = answers[i]
		}
	case p.Kind != sdk.PendingKindPermission:
		return fmt.Errorf("%s 在等的是問題（%s），請用 --answer", o.Pos[0], PendingLine(p))
	default:
		d.Allow = o.Flags["allow"]
		d.Message = o.Vals["message"]
	}
	r, err := sessionAPI(e).Resolve(e.Ctx, &sdk.SessionResolveRequest{Session: o.Pos[0], PendingID: p.ID, Decision: d, By: &sdk.Actor{Name: e.By}})
	if err != nil {
		return err
	}
	if !r.Applied {
		_, err = fmt.Fprintf(e.Out, "%s 已經有人先回答了（或已不在等）\n", o.Pos[0])
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已回答 %s：%s\n", o.Pos[0], PendingLine(p))
	return err
}

// sessions attach <名稱>：印出從終端機接上這個 session 畫面的指令（自己貼上執行；Ctrl-b d 離開）。
func sessionsAttach(e *cmdline.Env, args []string) error {
	name, err := oneName(args, "evergenie sessions attach <名稱>")
	if err != nil {
		return err
	}
	g, err := sessionAPI(e).Get(e.Ctx, &sdk.SessionGetRequest{Name: name})
	if err != nil {
		return err
	}
	if g.Session.Attach == "" {
		return fmt.Errorf("%s 的引擎沒有提供接上畫面的方式（或還沒開過）", name)
	}
	_, err = fmt.Fprintln(e.Out, g.Session.Attach)
	return err
}

// status：每位秘書的常駐 session 狀態、在等你的事，加上其他開著的 session 數。
func status(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, []string{"json"}, "evergenie status [--json]")
	if err != nil || len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie status [--json]")
	}
	list, err := listSessions(e, "")
	if err != nil {
		return err
	}
	agents, err := rosterIDs(e)
	if err != nil {
		return err
	}
	if o.Flags["json"] {
		return e.Line(map[string]any{"agents": agents, "sessions": list})
	}
	byName := map[string]sdk.Session{}
	others := 0
	for _, s := range list {
		if s.Kind == sdk.SessionKindResident {
			byName[s.Name] = s
		} else if s.State != sdk.SessionStateStopped {
			others++
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s %-14s %s\n", "秘書", "帳號", "狀態")
	for _, a := range agents {
		s, ok := byName[a]
		if !ok {
			fmt.Fprintf(&b, "%-10s %-14s %s\n", a, "", "沒開過")
			continue
		}
		since := ""
		if !s.StateSince.IsZero() {
			since = "（" + s.StateSince.Local().Format("01-02 15:04") + " 起）"
		}
		fmt.Fprintf(&b, "%-10s %-14s %s%s\n", a, s.Account, StateLabel(s), since)
	}
	fmt.Fprintf(&b, "\n其他開著的 session：%d 個（evergenie sessions list 看全部）\n", others)
	fmt.Fprintf(&b, "現在時間：%s\n", time.Now().Format("2006-01-02 15:04"))
	_, err = e.Out.Write([]byte(b.String()))
	return err
}

// rosterIDs 是名冊上啟用的秘書 id（排序）。
func rosterIDs(e *cmdline.Env) ([]string, error) {
	req := &sdk.VaultListRequest{Kind: "agent"}
	seen := map[string]bool{}
	var ids []string
	for {
		resp, err := vaultAPI(e).List(e.Ctx, req)
		if err != nil {
			return nil, err
		}
		for _, r := range resp.Records {
			if enabled, ok := r.Fields["enabled"].(bool); ok && !enabled {
				continue
			}
			ids = append(ids, r.ID)
		}
		next := cmdline.NextCursor(resp.Page)
		if next == "" || seen[next] {
			break
		}
		seen[next] = true
		req.Page = &sdk.PageRequest{Cursor: next}
	}
	sort.Strings(ids)
	return ids, nil
}

// say 直接傳話不通時（例如跨帳號）用：經 bus 送一則 say 封包，路徑由 bus 定成 core。
func say(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.BusSendRequest](e, a, cmdline.Spec{Free: true, Skip: []string{"from", "to", "kind", "body"}})
	if err != nil {
		return err
	}
	if len(p.Pos) < 2 {
		return cmdline.Usagef("用法：evergenie say <職位> <內容>")
	}
	p.Req.From, p.Req.To, p.Req.Kind = e.By, p.Pos[0], sdk.EnvelopeKindSay
	p.Req.Body = strings.Join(p.Pos[1:], " ")
	return cmdline.Send(e, p, busAPI(e).Send, func(r *sdk.BusSendResponse) any { return r.Envelope })
}

// voice answer <V-代號> <一兩句>：秘書回主人的語音問題（CHT-10）。
func voiceAnswer(e *cmdline.Env, a []string) error {
	p, err := cmdline.Prepare[sdk.WebVoiceAnswerRequest](e, a, cmdline.Spec{Free: true, Skip: []string{"ask", "text"}})
	if err != nil {
		return err
	}
	if len(p.Pos) < 2 {
		return cmdline.Usagef("用法：evergenie voice answer <V-代號> <一兩句口語回答>")
	}
	p.Req.Ask, p.Req.Text = p.Pos[0], strings.Join(p.Pos[1:], " ")
	p.Req.By = actor(e)
	return cmdline.Send(e, p, sdk.NewWebClient(e.Client).VoiceAnswer, func(r *sdk.WebVoiceAnswerResponse) any { return r.Reply })
}
