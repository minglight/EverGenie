package roster

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/internal/base/platform"
	"github.com/minglight/evergenie/sdk"
)

const (
	memoryUsage  = "evergenie memory show [<職位>] | memory add <一行> | memory write --file <路徑>|- | --body <文字>"
	noteUsage    = "evergenie memory note list [--agent <職位>] | memory note show <名字> [--agent <職位>] | memory note write <名字> --file <路徑>|- [--revision <show 看到的版本>]"
	journalUsage = "evergenie journal add <一行> | journal show [<職位>] [--date YYYY-MM-DD]"
)

// self 是寫記憶、日誌的人：要在秘書的 session 裡（$EVERGENIE_AGENT），主人與一般 session 不寫秘書記憶。
func self(e *cmdline.Env) (string, error) {
	if e.By == "" || e.By == cmdline.DefaultBy || platform.IsOwner(e.By) {
		return "", cmdline.Usagef("記憶和日誌只有秘書本人寫：要在秘書的 session 裡用（環境變數 EVERGENIE_AGENT 是她的職位 id）")
	}
	return e.By, nil
}

// who 是要看誰的：有給職位 id 用它，沒給就是自己。
func who(e *cmdline.Env, pos []string, usage string) (string, error) {
	switch {
	case len(pos) > 1:
		return "", cmdline.Usagef("用法：%s", usage)
	case len(pos) == 1:
		return pos[0], nil
	}
	id, err := self(e)
	if err != nil {
		return "", cmdline.Usagef("要看哪一位？用法：%s", usage)
	}
	return id, nil
}

// body 讀一筆 vault 紀錄的內文；還沒有這筆回空字串。
func body(e *cmdline.Env, kind, id string) (string, error) {
	r, err := vaultAPI(e).Get(e.Ctx, &sdk.VaultGetRequest{Kind: kind, ID: id})
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound {
			return "", nil
		}
		return "", err
	}
	if r.Record == nil {
		return "", nil
	}
	return r.Record.Body, nil
}

func memoryShow(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, nil, nil, memoryUsage)
	if err != nil {
		return err
	}
	id, err := who(e, o.Pos, "evergenie memory show [<職位>]")
	if err != nil {
		return err
	}
	text, err := body(e, "memory", id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		_, err = fmt.Fprintf(e.Out, "%s 還沒有記憶\n", id)
		return err
	}
	_, err = fmt.Fprint(e.Out, strings.TrimRight(text, "\n")+"\n")
	return err
}

// memory add <一行>：接在記憶尾端（讀出整份、加一行、整份寫回）。
func memoryAdd(e *cmdline.Env, args []string) error {
	id, err := self(e)
	if err != nil {
		return err
	}
	line := strings.TrimSpace(strings.Join(args, " "))
	if line == "" || strings.HasPrefix(line, "--") {
		return cmdline.Usagef("用法：evergenie memory add <一行>")
	}
	if strings.ContainsAny(line, "\r\n") {
		return cmdline.Usagef("memory add 一次一行；整份改寫用 evergenie memory write --file <路徑>")
	}
	text, err := body(e, "memory", id)
	if err != nil {
		return err
	}
	if text = strings.TrimRight(text, "\n"); text != "" {
		text += "\n"
	}
	if _, err := api(e).WriteMemory(e.Ctx, &sdk.AgentsWriteMemoryRequest{ID: id, Body: text + line + "\n", By: actor(e)}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已記進 %s 的記憶\n", id)
	return err
}

// memory write --file <路徑>|- 或 --body <文字>：整份取代自己的記憶。
func memoryWrite(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"file", "body"}, nil, memoryUsage)
	if err != nil {
		return err
	}
	file, hasFile := o.Vals["file"]
	text, hasBody := o.Vals["body"]
	if len(o.Pos) > 0 || hasFile == hasBody {
		return cmdline.Usagef("用法：evergenie memory write --file <路徑>|- 或 --body <文字>（兩個給一個）")
	}
	id, err := self(e)
	if err != nil {
		return err
	}
	if hasFile {
		var data []byte
		if file == "-" {
			data, err = io.ReadAll(e.In)
		} else {
			data, err = os.ReadFile(cmdline.ExpandHome(file))
		}
		if err != nil {
			return fmt.Errorf("讀不到 %s：%w", file, err)
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
		return cmdline.Usagef("新的記憶是空的，沒有寫入（真的要清空請 --body 寫一行說明）")
	}
	if _, err := api(e).WriteMemory(e.Ctx, &sdk.AgentsWriteMemoryRequest{ID: id, Body: text, By: actor(e)}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已更新 %s 的記憶（%d 行）\n", id, strings.Count(strings.TrimRight(text, "\n"), "\n")+1)
	return err
}

// memory note list|show|write：秘書自己的做事筆記（vault/agents/<職位>/notes/<名字>.md，DEV-161）。
// 讀誰的都行（--agent），寫只有本人；檔頭由核心補，秘書只給內文。
func memoryNote(e *cmdline.Env, args []string) error {
	if len(args) == 0 {
		return cmdline.Usagef("用法：%s", noteUsage)
	}
	switch args[0] {
	case "list":
		return noteList(e, args[1:])
	case "show":
		return noteShow(e, args[1:])
	case "write":
		return noteWrite(e, args[1:])
	}
	return cmdline.Usagef("用法：%s", noteUsage)
}

// noteOwner 是要看誰的筆記：--agent 有給用它，沒給就是自己。
func noteOwner(e *cmdline.Env, o cmdline.Opts) (string, error) {
	if a := o.Vals["agent"]; a != "" {
		return a, nil
	}
	id, err := self(e)
	if err != nil {
		return "", cmdline.Usagef("要看哪一位的筆記？加 --agent <職位>（用法：%s）", noteUsage)
	}
	return id, nil
}

func noteList(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"agent"}, nil, noteUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) > 0 {
		return cmdline.Usagef("用法：evergenie memory note list [--agent <職位>]")
	}
	id, err := noteOwner(e, o)
	if err != nil {
		return err
	}
	r, err := vaultAPI(e).List(e.Ctx, &sdk.VaultListRequest{Kind: "note"})
	if err != nil {
		return err
	}
	rows := []sdk.Record{}
	for _, rec := range r.Records {
		if strings.HasPrefix(rec.ID, id+"/") {
			rows = append(rows, rec)
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for _, rec := range rows {
		by, _ := rec.Fields["by"].(string)
		at, _ := rec.Fields["updated_at"].(string)
		if _, err := fmt.Fprintf(e.Out, "- %s · 最後寫的：%s %s · %d 行\n", strings.TrimPrefix(rec.ID, id+"/"), by, at, strings.Count(strings.TrimRight(rec.Body, "\n"), "\n")+1); err != nil {
			return err
		}
	}
	if len(rows) == 0 {
		_, err = fmt.Fprintf(e.Out, "%s 還沒有做事筆記\n", id)
	}
	return err
}

func noteShow(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"agent"}, nil, noteUsage)
	if err != nil {
		return err
	}
	if len(o.Pos) != 1 {
		return cmdline.Usagef("用法：evergenie memory note show <名字> [--agent <職位>]")
	}
	id, err := noteOwner(e, o)
	if err != nil {
		return err
	}
	name := strings.TrimSuffix(strings.TrimSpace(o.Pos[0]), ".md")
	got, err := vaultAPI(e).Get(e.Ctx, &sdk.VaultGetRequest{Kind: "note", ID: id + "/" + name})
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound {
			return fmt.Errorf("%s 沒有做事筆記「%s」；用 evergenie memory note list 看有哪些", id, name)
		}
		return err
	}
	rec := got.Record
	if rec == nil {
		return fmt.Errorf("%s 沒有做事筆記「%s」", id, name)
	}
	by, _ := rec.Fields["by"].(string)
	at, _ := rec.Fields["updated_at"].(string)
	body := strings.TrimRight(rec.Body, "\n")
	if strings.TrimSpace(body) == "" {
		body = "（還沒有內容）"
	}
	_, err = fmt.Fprintf(e.Out, "# %s（vault/agents/%s/notes/%s.md）\n最後寫的：%s %s\n版本：%s（要整份 write 就帶 --revision 這個版本）\n\n%s\n", name, id, name, by, at, rec.Revision, body)
	return err
}

// memory note write <名字> --file <路徑>|- [--revision <版本>]：新檔不用版本，改舊檔要版本（先 show 拿）。
func noteWrite(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"file", "revision"}, nil, noteUsage)
	if err != nil {
		return err
	}
	file, ok := o.Vals["file"]
	if len(o.Pos) != 1 || !ok {
		return cmdline.Usagef("用法：evergenie memory note write <名字> --file <路徑>|- [--revision <show 看到的版本>]（只給內文，檔頭核心會補）")
	}
	id, err := self(e)
	if err != nil {
		return err
	}
	var data []byte
	if file == "-" {
		data, err = io.ReadAll(e.In)
	} else {
		data, err = os.ReadFile(cmdline.ExpandHome(file))
	}
	if err != nil {
		return fmt.Errorf("讀不到 %s：%w", file, err)
	}
	if strings.HasPrefix(strings.ReplaceAll(string(data), "\r\n", "\n"), "---\n") {
		return cmdline.Usagef("內文不用自己寫檔頭（--- 開頭那段）：檔頭由核心補，只給內文")
	}
	out, err := api(e).WriteNote(e.Ctx, &sdk.AgentsWriteNoteRequest{ID: id, Name: o.Pos[0], Body: string(data), Revision: o.Vals["revision"], By: actor(e)})
	if err != nil {
		return err
	}
	verb := "已更新"
	if out.Created {
		verb = "已新增"
	}
	_, err = fmt.Fprintf(e.Out, "%s %s 的做事筆記「%s」（vault/agents/%s/notes/%s.md，版本 %s）\n", verb, id, out.Name, id, out.Name, out.Revision)
	return err
}

// journal add <一行>：今天日誌的尾端追加一行，時間由核心加。
func journalAdd(e *cmdline.Env, args []string) error {
	id, err := self(e)
	if err != nil {
		return err
	}
	line := strings.TrimSpace(strings.Join(args, " "))
	if line == "" || strings.HasPrefix(line, "--") {
		return cmdline.Usagef("用法：evergenie journal add <一行>")
	}
	if _, err := api(e).AppendJournal(e.Ctx, &sdk.AgentsAppendJournalRequest{ID: id, Line: line, By: actor(e)}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已寫進 %s 今天的日誌\n", id)
	return err
}

func journalShow(e *cmdline.Env, args []string) error {
	o, err := cmdline.ParseOpts(args, []string{"date"}, nil, journalUsage)
	if err != nil {
		return err
	}
	id, err := who(e, o.Pos, "evergenie journal show [<職位>] [--date YYYY-MM-DD]")
	if err != nil {
		return err
	}
	day := o.Vals["date"]
	if day == "" {
		day = time.Now().Format("2006-01-02")
	} else if _, err := time.Parse("2006-01-02", day); err != nil {
		return cmdline.Usagef("--date 要寫成 YYYY-MM-DD")
	}
	text, err := body(e, "journal", id+"/"+day)
	if err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		_, err = fmt.Fprintf(e.Out, "%s 在 %s 沒有日誌\n", id, day)
		return err
	}
	_, err = fmt.Fprint(e.Out, strings.TrimRight(text, "\n")+"\n")
	return err
}
