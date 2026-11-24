package notes

import (
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/base/app"
	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// 種類名要跟底層 agent 的一樣（schemas/note.md）；指令行程只是 client，不 import 底層模組（CLI-1）。
const (
	topicKind    = "topic"
	categoryKind = "topic_category"
	ownerTopicID = "人/主人"
	quipTopic    = "梗本" // 梗本（DEV-164）跟「主人」一樣全部職位跟進；名字或別名是它的那一份
	notesUsage   = "evergenie notes list [--category <分類>] [--follow <職位>] | show <名字> | find <字> | add <名字> <一行> [--category … --summary … --follow a,b --aliases a,b --links a,b] | set <名字> [--file <路徑>|- --revision <show 看到的版本>] [--summary … --follow a,b --aliases a,b --links a,b --clear aliases,follow,links] | archive <名字> [--into <要留的主題>] | categories list|add <名字> --summary … | quips [used <那條梗裡的字>]"
)

// Commands 是 `evergenie notes …` 的指令表（套件層級函式）：讀走 vault 的 list，寫走 sdk 的 notes 服務。
// 寫的人是 $EVERGENIE_AGENT（秘書）；沒有就是主人（EverGenie 指令預設的操作者）。
func Commands() map[string]app.Command {
	return map[string]app.Command{
		"notes list":       {Usage: "notes list|show|find|add|set|archive|categories\n全家共用的主題筆記（vault/notes/<分類>/<主題>.md）：list [--category 人] [--follow life]；show <名字>；find <字>；add <名字> <一行>（行尾自動標你和日期；新主題要 --category 與 --summary）；set <名字> --file <路徑>|- --revision <版本> 整份取代（要先 show 拿版本；沒標名的行拒絕、版本對不上拒絕）；archive <名字> [--into <要留的主題>] 封存重複的主題（有目標沒有的行就拒絕，先 add 併過去）；categories list|add <名字> --summary …；quips 看開場帶的「梗本」那一節（全家共用的老梗），quips used <那條梗裡的字> 記下剛提過這條（一週內不再帶進開場）", Group: cmdline.GroupAgents, Run: list},
		"notes show":       {Run: show},
		"notes find":       {Run: find},
		"notes add":        {Run: add},
		"notes set":        {Run: set},
		"notes archive":    {Run: archive},
		"notes categories": {Run: categories},
		"notes quips":      {Run: quips},
	}
}

func vaultAPI(e *cmdline.Env) sdk.VaultAPI { return sdk.NewVaultClient(e.Client) }
func api(e *cmdline.Env) sdk.NotesAPI      { return sdk.NewNotesClient(e.Client) }
func actor(e *cmdline.Env) *sdk.Actor      { return &sdk.Actor{Name: e.By} }

func records(e *cmdline.Env, kind string) ([]sdk.Record, error) {
	r, err := vaultAPI(e).List(e.Ctx, &sdk.VaultListRequest{Kind: kind})
	if err != nil {
		return nil, err
	}
	return r.Records, nil
}

func str(r sdk.Record, key string) string { s, _ := r.Fields[key].(string); return s }

func strs(r sdk.Record, key string) []string {
	out := []string{}
	switch xs := r.Fields[key].(type) {
	case []string:
		out = append(out, xs...)
	case []any:
		for _, x := range xs {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func name(r sdk.Record) string {
	if n := str(r, "name"); n != "" {
		return n
	}
	_, n, _ := strings.Cut(r.ID, "/")
	return n
}

func category(r sdk.Record) string { c, _, _ := strings.Cut(r.ID, "/"); return c }

func follows(r sdk.Record, agent string) bool {
	return followedByAll(r) || slices.Contains(strs(r, "follow"), agent)
}

func followedByAll(r sdk.Record) bool {
	return r.ID == ownerTopicID || name(r) == quipTopic || slices.Contains(strs(r, "aliases"), quipTopic)
}

// line 是列表與找字時一個主題的一行（跟索引同樣的寫法）。
func line(r sdk.Record) string {
	s := "[[" + name(r) + "]]"
	if a := strs(r, "aliases"); len(a) > 0 {
		s += "（別名：" + strings.Join(a, "、") + "）"
	}
	follow := "全部職位"
	if !followedByAll(r) {
		follow = strings.Join(strs(r, "follow"), "、")
	}
	return s + " — " + str(r, "summary") + " · 跟進：" + follow + " · 分類：" + category(r)
}

func sorted(rows []sdk.Record) []sdk.Record {
	sort.Slice(rows, func(i, j int) bool {
		if category(rows[i]) != category(rows[j]) {
			return category(rows[i]) < category(rows[j])
		}
		return name(rows[i]) < name(rows[j])
	})
	return rows
}

func list(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"category", "follow"}, nil, notesUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie notes list [--category <分類>] [--follow <職位>]")
	}
	rows, err := records(e, topicKind)
	if err != nil {
		return err
	}
	n := 0
	for _, r := range sorted(rows) {
		if c := o.Vals["category"]; c != "" && category(r) != c {
			continue
		}
		if f := o.Vals["follow"]; f != "" && !follows(r, f) {
			continue
		}
		if _, err := fmt.Fprintln(e.Out, "- "+line(r)); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		_, err = fmt.Fprintln(e.Out, "沒有符合的主題")
	}
	return err
}

// lookup 用名字或別名找主題（跟核心同樣的規則：先比名字，再比別名）。
func lookup(rows []sdk.Record, want string) (sdk.Record, error) {
	for _, r := range rows {
		if name(r) == want {
			return r, nil
		}
	}
	var hits []sdk.Record
	for _, r := range rows {
		if slices.Contains(strs(r, "aliases"), want) {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return sdk.Record{}, fmt.Errorf("沒有主題「%s」；用 evergenie notes find <字> 找找看", want)
	}
	names := []string{}
	for _, r := range hits {
		names = append(names, name(r))
	}
	return sdk.Record{}, fmt.Errorf("「%s」是好幾份主題的別名（%s），請用名字", want, strings.Join(names, "、"))
}

func show(e *cmdline.Env, args []string) error {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		if len(args) == 1 && args[0] == "--help" {
			return &cmdline.HelpText{Text: "用法：evergenie notes show <名字>\n"}
		}
		return cmdline.Usagef("用法：evergenie notes show <名字>")
	}
	rows, err := records(e, topicKind)
	if err != nil {
		return err
	}
	r, err := lookup(rows, args[0])
	if err != nil {
		return err
	}
	head := []string{"# " + name(r) + "（分類：" + category(r) + "，vault/notes/" + r.ID + ".md）", "一句說明：" + str(r, "summary")}
	for _, f := range []struct{ key, label string }{{"aliases", "別名"}, {"follow", "跟進"}, {"links", "連結"}} {
		if xs := strs(r, f.key); len(xs) > 0 {
			head = append(head, f.label+"："+strings.Join(xs, "、"))
		}
	}
	head = append(head, "最後寫的："+str(r, "by")+" "+str(r, "updated_at"), "版本："+r.Revision+"（要整份 set 就帶 --revision 這個版本）")
	body := strings.TrimRight(r.Body, "\n")
	if strings.TrimSpace(body) == "" {
		body = "（還沒有內容）"
	}
	_, err = fmt.Fprint(e.Out, strings.Join(head, "\n")+"\n\n"+body+"\n")
	return err
}

func find(e *cmdline.Env, args []string) error {
	word := strings.TrimSpace(strings.Join(args, " "))
	if word == "" || strings.HasPrefix(word, "--") {
		return cmdline.Usagef("用法：evergenie notes find <字>（比對名字、別名、一句說明、內文）")
	}
	rows, err := records(e, topicKind)
	if err != nil {
		return err
	}
	low := strings.ToLower(word)
	has := func(s string) bool { return strings.Contains(strings.ToLower(s), low) }
	n := 0
	for _, r := range sorted(rows) {
		hit := has(name(r)) || has(str(r, "summary")) || slices.ContainsFunc(strs(r, "aliases"), has)
		var lines []string
		for _, l := range strings.Split(r.Body, "\n") {
			if has(l) {
				lines = append(lines, "    "+strings.TrimSpace(l))
			}
		}
		if !hit && len(lines) == 0 {
			continue
		}
		out := "- " + line(r)
		if len(lines) > 0 {
			out += "\n" + strings.Join(lines, "\n")
		}
		if _, err := fmt.Fprintln(e.Out, out); err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		_, err = fmt.Fprintf(e.Out, "沒有主題提到「%s」；總索引在 vault/notes/索引.md\n", word)
	}
	return err
}

// splitList 把「a,b」或「a、b」拆開。
func splitList(s string) []string {
	out := []string{}
	for _, x := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '、' }) {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

func add(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"category", "summary", "follow", "aliases", "links"}, nil, notesUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) < 2 {
		return cmdline.Usagef("用法：evergenie notes add <名字> <一行> [--category <分類> --summary <一句說明>（新主題才要）]")
	}
	req := &sdk.NotesAddRequest{Name: o.Pos[0], Line: strings.Join(o.Pos[1:], " "), Category: o.Vals["category"], Summary: o.Vals["summary"],
		Follow: splitList(o.Vals["follow"]), Aliases: splitList(o.Vals["aliases"]), Links: splitList(o.Vals["links"]), By: actor(e)}
	out, err := api(e).Add(e.Ctx, req)
	if err != nil {
		return err
	}
	verb := "已寫進"
	if out.Created {
		verb = "已開新主題並寫進"
	}
	_, err = fmt.Fprintf(e.Out, "%s「%s」：%s\n", verb, name(*out.Topic), out.Line)
	return err
}

func set(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"file", "revision", "summary", "follow", "aliases", "links", "clear"}, nil, notesUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：evergenie notes set <名字> [--file <路徑>|- --revision <版本>] [--summary …] [--follow a,b] [--aliases a,b] [--links a,b] [--clear aliases,follow,links]")
	}
	req := &sdk.NotesSetRequest{Name: o.Pos[0], Summary: o.Vals["summary"], Clear: splitList(o.Vals["clear"]), Revision: o.Vals["revision"], By: actor(e)}
	for key, dst := range map[string]*[]string{"follow": &req.Follow, "aliases": &req.Aliases, "links": &req.Links} {
		if v, ok := o.Vals[key]; ok {
			*dst = splitList(v)
		}
	}
	file, hasFile := o.Vals["file"]
	if !hasFile {
		if req.Summary == "" && req.Follow == nil && req.Aliases == nil && req.Links == nil && len(req.Clear) == 0 {
			return cmdline.Usagef("要給 --file（新的整份內文）或要改的檔頭（--summary、--follow、--aliases、--links、--clear）")
		}
		req.KeepBody = true
	} else {
		var data []byte
		if file == "-" {
			data, err = io.ReadAll(e.In)
		} else {
			data, err = os.ReadFile(cmdline.ExpandHome(file))
		}
		if err != nil {
			return fmt.Errorf("讀不到 %s：%w", file, err)
		}
		req.Body = string(data)
	}
	out, err := api(e).Set(e.Ctx, req)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已更新主題「%s」\n", name(*out.Topic))
	return err
}

func archive(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"into"}, nil, notesUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：evergenie notes archive <名字> [--into <要留的主題>]（封存重複的主題：先把它有、目標沒有的行 notes add 到目標）")
	}
	out, err := api(e).Archive(e.Ctx, &sdk.NotesArchiveRequest{Name: o.Pos[0], Into: o.Vals["into"], By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(e.Out, out.Message)
	return err
}

// suggested 取分類說明檔裡「建議段落：…」那一句（到第一個句號）；沒有回空字串。
func suggested(body string) string {
	for _, l := range strings.Split(body, "\n") {
		if l = strings.TrimSpace(l); strings.HasPrefix(l, "建議段落：") {
			head, _, _ := strings.Cut(l, "。")
			return head + "。"
		}
	}
	return ""
}

func categories(e *cmdline.Env, args []string) error {
	usage := "用法：evergenie notes categories list | add <名字> --summary <這個分類放什麼> [--body <說明>]"
	if len(args) == 0 || args[0] == "list" {
		if len(args) > 1 {
			return cmdline.Usagef("%s", usage)
		}
		rows, err := records(e, categoryKind)
		if err != nil {
			return err
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
		for _, r := range rows {
			if _, err := fmt.Fprintf(e.Out, "- %s：%s\n", r.ID, str(r, "summary")); err != nil {
				return err
			}
			if s := suggested(r.Body); s != "" { // 說明檔有建議段落就多印一行（DEV-161；舊家的說明檔沒有這一段）
				if _, err := fmt.Fprintf(e.Out, "  %s\n", s); err != nil {
					return err
				}
			}
		}
		if len(rows) == 0 {
			_, err = fmt.Fprintln(e.Out, "還沒有分類")
		}
		return err
	}
	if args[0] != "add" {
		return cmdline.Usagef("%s", usage)
	}
	o, err := cmdline.ParseOpts(args[1:], []string{"summary", "body"}, nil, usage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("%s", usage)
	}
	out, err := api(e).AddCategory(e.Ctx, &sdk.NotesAddCategoryRequest{Name: o.Pos[0], Summary: o.Vals["summary"], Body: o.Vals["body"], By: actor(e)})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已加分類「%s」（vault/notes/%s/）\n", out.Category.ID, out.Category.ID)
	return err
}

// quips：`notes quips` 印開場「梗本」那一節（每位秘書都一樣，DEV-164）；`notes quips used <字>` 記下剛提過哪一條。
func quips(e *cmdline.Env, args []string) error {
	usage := "用法：evergenie notes quips | quips used <那條梗裡的字>"
	if len(args) > 0 && args[0] == "used" {
		text := strings.TrimSpace(strings.Join(args[1:], " "))
		if text == "" || strings.HasPrefix(text, "--") {
			return cmdline.Usagef("%s", usage)
		}
		out, err := api(e).QuipUsed(e.Ctx, &sdk.NotesQuipUsedRequest{Text: text, By: actor(e)})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(e.Out, out.Message)
		return err
	}
	if len(args) > 0 {
		return cmdline.Usagef("%s", usage)
	}
	out, err := api(e).Quips(e.Ctx, &sdk.NotesQuipsRequest{By: actor(e)})
	if err != nil {
		return err
	}
	head := fmt.Sprintf("開場的「梗本」那一節（每位秘書開場都帶；梗本一共 %d 條，帶了 %d 條", out.Total, out.Shown)
	if len(out.Resting) > 0 {
		head += "；這週提過、先不帶：" + strings.Join(out.Resting, "、")
	}
	_, err = fmt.Fprint(e.Out, head+"）：\n\n"+strings.TrimRight(out.Text, "\n")+"\n")
	return err
}
