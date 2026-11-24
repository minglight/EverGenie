package job

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/minglight/evergenie/internal/base/app/cmdline"
	"github.com/minglight/evergenie/sdk"
)

// 夜班清單（P6-6）：vault 的 note `system/night-queue`（vault/agents/system/notes/night-queue.md），內容是 Markdown。
// 三段：待做、要主人、做完（留最近 14 天）。純文字的解析與改寫在這個檔案，寫入走既有的 vault get／put
// （有 by、變更紀錄、樂觀鎖）；看板的「設定→電腦→夜班清單」讀寫同一筆，格式要一起改（web/apps/board 的 nightQueue.ts）。

const (
	nightKind = "note"
	nightID   = "system/night-queue"
	// nightKeepDays：「做完」留幾天。
	nightKeepDays = 14
)

const nightUsage = `用法：evergenie night <子指令>   夜班清單（每週額度重置前最後兩晚，system 照這份做；沒放就不做）
  list                              印三段（待做、要主人、做完），待做的項目有序號
  add "<做什麼>"                     加在「待做」最下面（一句話；要讀哪份文件、產出放哪裡、做到什麼程度）
  done <n> --result "<一句>"         第 n 項做完：移到「做完」，附結果；順便清掉做完超過 14 天的
  block <n> --reason "<一句>"        第 n 項做不了或卡住：移到「要主人」，附原因
  rm <n>                            刪掉「待做」的第 n 項
<n> 是「待做」裡由上往下的序號（先 list 看）。
done、block、rm 可以加 --expect "<項目文字開頭>"：第 n 項不是這樣開頭就不改（清單可能剛被別處改過，序號跑掉了）。
`

func nightHelp(e *cmdline.Env, a []string) error {
	for _, x := range a {
		if x != "--help" && x != "-h" && x != "help" {
			return cmdline.Usagef("不認得的子指令：night %s（evergenie night --help 看用法）", x)
		}
	}
	_, err := fmt.Fprint(e.Out, nightUsage)
	return err
}

// nightNow 是「今天」的來源；測試換成固定時間。
var nightNow = time.Now

type nightSection struct {
	heading string   // 整行標題（例：## 待做）；前言沒有
	lines   []string // 標題以外的行
}

type nightDoc struct {
	secs []*nightSection // secs[0] 是第一個「## 」之前的前言（heading 空）
}

const (
	nightTodo    = "## 待做"
	nightBlocked = "## 要主人"
	nightDone    = "## 做完（留最近 14 天）"
)

var nightItemStart = regexp.MustCompile(`^- \[( |x|X)\] `)
var nightDoneDate = regexp.MustCompile(`完成 (\d{4}-\d{2}-\d{2})`)

func parseNight(text string) *nightDoc {
	d := &nightDoc{secs: []*nightSection{{}}}
	for _, line := range strings.Split(strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n"), "\n") {
		if strings.HasPrefix(line, "## ") {
			d.secs = append(d.secs, &nightSection{heading: line})
			continue
		}
		cur := d.secs[len(d.secs)-1]
		cur.lines = append(cur.lines, line)
	}
	return d
}

// section 找一段（標題以 prefix 開頭，例 "## 待做"）；沒有就照順序補在最後。
func (d *nightDoc) section(canonical string) *nightSection {
	prefix := strings.TrimSuffix(canonical, "（留最近 14 天）")
	for _, s := range d.secs {
		if s.heading == prefix || strings.HasPrefix(s.heading, prefix+"（") || strings.HasPrefix(s.heading, prefix+" ") {
			return s
		}
	}
	s := &nightSection{heading: canonical}
	d.secs = append(d.secs, s)
	return s
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func (d *nightDoc) render() string {
	var out []string
	for _, s := range d.secs {
		body := trimBlank(s.lines)
		if s.heading != "" {
			out = append(out, s.heading)
		}
		if len(body) > 0 {
			out = append(out, body...)
		}
		out = append(out, "")
	}
	return strings.Join(out, "\n")
}

// nightItem 是一個項目在 lines 裡的範圍 [start, end)：第一行是「- [ ] …」，後面縮排的行是它的進度說明。
type nightItem struct{ start, end int }

func (s *nightSection) items() []nightItem {
	var out []nightItem
	for i := 0; i < len(s.lines); i++ {
		if !nightItemStart.MatchString(s.lines[i]) {
			continue
		}
		end := i + 1
		for end < len(s.lines) && !nightItemStart.MatchString(s.lines[end]) && s.lines[end] != "" && (s.lines[end][0] == ' ' || s.lines[end][0] == '\t') {
			end++
		}
		out = append(out, nightItem{i, end})
		i = end - 1
	}
	return out
}

func (s *nightSection) cut(it nightItem) []string {
	taken := append([]string(nil), s.lines[it.start:it.end]...)
	s.lines = append(append([]string(nil), s.lines[:it.start]...), s.lines[it.end:]...)
	return taken
}

func (s *nightSection) appendItem(lines ...string) {
	body := trimBlank(s.lines)
	s.lines = append(append([]string(nil), body...), lines...)
}

// nightTemplate 是檔案不存在時第一次寫入的內容。
const nightTemplate = `# 夜班清單

主人排好、要在「帳號每週額度重置前最後兩晚」做的事。放進來的才會做，沒放就不做。
做法與停止條件寫在 system 的夜班排程（排程 night-shift 的內文）；這個家有那個排程才會自動做，沒有就請電腦秘書（system）設。

## 怎麼放
一項一行，由上往下做。跟任何一位秘書說「放進夜班清單」，對方就會用 ` + "`evergenie night add \"<做什麼>\"`" + ` 加在「待做」：
` + "`- [ ] 做什麼（一句話；要讀哪份文件、產出放哪裡、做到什麼程度）｜加入 YYYY-MM-DD`" + `
適合：吃額度但不急、做完可以事後檢查的事（整理、review、補測試、寫文件、研究）。
不適合（夜班會跳過並移到「要主人」）：要 sudo／密碼、要主人決定、花點數或錢、對外寫入（push、寄信）。

` + nightTodo + `

` + nightBlocked + `

` + nightDone + `
`

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(s, "｜", "|")), " ")
}

func (d *nightDoc) add(what string, today time.Time) error {
	what = oneLine(what)
	if what == "" {
		return cmdline.Usagef("請寫要做什麼：evergenie night add \"<做什麼>\"")
	}
	d.section(nightTodo).appendItem(fmt.Sprintf("- [ ] %s｜加入 %s", what, today.Format("2006-01-02")))
	return nil
}

func (d *nightDoc) pick(n int) (*nightSection, nightItem, error) {
	todo := d.section(nightTodo)
	items := todo.items()
	if n < 1 || n > len(items) {
		if len(items) == 0 {
			return nil, nightItem{}, fmt.Errorf("「待做」是空的，沒有第 %d 項", n)
		}
		return nil, nightItem{}, fmt.Errorf("「待做」只有 %d 項，沒有第 %d 項（evergenie night list 看序號）", len(items), n)
	}
	return todo, items[n-1], nil
}

func (d *nightDoc) done(n int, result string, today time.Time) error {
	result = oneLine(result)
	if result == "" {
		return cmdline.Usagef("做完要附一句結果：evergenie night done <n> --result \"<一句>\"")
	}
	todo, it, err := d.pick(n)
	if err != nil {
		return err
	}
	lines := todo.cut(it)
	lines[0] = "- [x] " + strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(lines[0], "- [ ] "), "- [x] "), "- [X] ")
	lines[0] += fmt.Sprintf("｜結果：%s｜完成 %s", result, today.Format("2006-01-02"))
	done := d.section(nightDone)
	done.appendItem(lines...)
	d.pruneDone(today)
	return nil
}

// pruneDone 清掉「做完」裡完成日期超過 14 天的項目（連同它的進度說明）；沒有日期的留著。
func (d *nightDoc) pruneDone(today time.Time) {
	done := d.section(nightDone)
	cutoff := today.AddDate(0, 0, -nightKeepDays).Format("2006-01-02")
	items := done.items()
	for i := len(items) - 1; i >= 0; i-- {
		m := nightDoneDate.FindStringSubmatch(done.lines[items[i].start])
		if m != nil && m[1] < cutoff {
			done.cut(items[i])
		}
	}
}

func (d *nightDoc) block(n int, reason string) error {
	reason = oneLine(reason)
	if reason == "" {
		return cmdline.Usagef("要附一句原因：evergenie night block <n> --reason \"<一句>\"")
	}
	todo, it, err := d.pick(n)
	if err != nil {
		return err
	}
	lines := todo.cut(it)
	lines[0] += "｜卡住：" + reason
	d.section(nightBlocked).appendItem(lines...)
	return nil
}

// expect 確認「待做」第 n 項的文字（去掉「- [ ] 」）以 prefix 開頭；比較時連續空白當一個。
// prefix 空的不檢查（沒給 --expect）。
func (d *nightDoc) expect(n int, prefix string) error {
	prefix = strings.Join(strings.Fields(prefix), " ")
	if prefix == "" {
		return nil
	}
	todo, it, err := d.pick(n)
	if err != nil {
		return err
	}
	text := strings.Join(strings.Fields(nightItemStart.ReplaceAllString(todo.lines[it.start], "")), " ")
	if !strings.HasPrefix(text, prefix) {
		return fmt.Errorf("沒有改：「待做」第 %d 項是「%s」，不是「%s」開頭；清單可能剛被改過，先 evergenie night list 看序號再試", n, text, prefix)
	}
	return nil
}

func (d *nightDoc) rm(n int) error {
	todo, it, err := d.pick(n)
	if err != nil {
		return err
	}
	todo.cut(it)
	return nil
}

// list 印三段；只有「待做」有序號（done／block／rm 用的就是它）。
func (d *nightDoc) list() string {
	var b strings.Builder
	for _, c := range []struct{ canonical, title string }{{nightTodo, "待做"}, {nightBlocked, "要主人"}, {nightDone, "做完"}} {
		s := d.section(c.canonical)
		items := s.items()
		fmt.Fprintf(&b, "%s（%d）\n", c.title, len(items))
		if len(items) == 0 {
			b.WriteString("  （沒有）\n")
		}
		for i, it := range items {
			first := nightItemStart.ReplaceAllString(s.lines[it.start], "")
			if c.canonical == nightTodo {
				fmt.Fprintf(&b, "  %d. %s\n", i+1, first)
			} else {
				fmt.Fprintf(&b, "  - %s\n", first)
			}
			for _, extra := range s.lines[it.start+1 : it.end] {
				fmt.Fprintf(&b, "      %s\n", strings.TrimSpace(extra))
			}
		}
	}
	return b.String()
}

// nightEdit 讀、改、寫夜班清單這一筆 note；寫入衝突（別人剛改過）重讀再試一次。
func nightEdit(e *cmdline.Env, change func(*nightDoc) error) error {
	for attempt := 0; ; attempt++ {
		doc, rec, err := nightLoad(e)
		if err != nil {
			return err
		}
		if err := change(doc); err != nil {
			return err
		}
		next := sdk.Record{Kind: nightKind, ID: nightID, Fields: map[string]any{}, Body: doc.render()}
		expected := ""
		if rec != nil {
			next.Fields, expected = rec.Fields, rec.Revision
		}
		_, err = vaultAPI(e).Put(e.Ctx, &sdk.VaultPutRequest{Record: &next, By: actor(e), ExpectedRevision: expected})
		var se *sdk.Error
		if err != nil && errors.As(err, &se) && se.Code == sdk.ErrorCodeConflict && attempt == 0 {
			continue
		}
		return err
	}
}

// nightLoad 讀夜班清單；還沒有這個檔案回範本（rec 為 nil）。
func nightLoad(e *cmdline.Env) (*nightDoc, *sdk.Record, error) {
	r, err := vaultAPI(e).Get(e.Ctx, &sdk.VaultGetRequest{Kind: nightKind, ID: nightID})
	if err != nil {
		var se *sdk.Error
		if errors.As(err, &se) && se.Code == sdk.ErrorCodeNotFound {
			return parseNight(nightTemplate), nil, nil
		}
		return nil, nil, err
	}
	return parseNight(r.Record.Body), r.Record, nil
}

func nightList(e *cmdline.Env, a []string) error {
	if len(a) > 0 {
		return cmdline.Usagef("用法：evergenie night list")
	}
	doc, _, err := nightLoad(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(e.Out, doc.list())
	return err
}

func nightAdd(e *cmdline.Env, a []string) error {
	if len(a) == 0 {
		return cmdline.Usagef("用法：evergenie night add \"<做什麼>\"")
	}
	what := strings.Join(a, " ")
	if err := nightEdit(e, func(d *nightDoc) error { return d.add(what, nightNow()) }); err != nil {
		return err
	}
	_, err := fmt.Fprintln(e.Out, "已放進夜班清單的「待做」")
	return err
}

// nightExpect 取 --expect 的值；有給但是空的要擋（不然等於沒檢查）。
func nightExpect(o cmdline.Opts) (string, error) {
	v, ok := o.Vals["expect"]
	if ok && strings.TrimSpace(v) == "" {
		return "", cmdline.Usagef("--expect 要寫第 n 項文字的開頭（例：--expect \"整理\"），不能是空的")
	}
	return v, nil
}

func nightN(pos []string, usage string) (int, error) {
	if len(pos) != 1 {
		return 0, cmdline.Usagef("用法：%s", usage)
	}
	var n int
	if _, err := fmt.Sscanf(pos[0], "%d", &n); err != nil || fmt.Sprint(n) != pos[0] || n < 1 {
		return 0, cmdline.Usagef("<n> 要是「待做」的序號（1、2、3…；先 evergenie night list）")
	}
	return n, nil
}

func nightDoneCmd(e *cmdline.Env, a []string) error {
	const usage = "evergenie night done <n> --result \"<一句>\" [--expect \"<項目文字開頭>\"]"
	o, err := cmdline.ParseOpts(a, []string{"result", "expect"}, nil, usage)
	if err != nil {
		return cmdline.Usagef("用法：%s", usage)
	}
	n, err := nightN(o.Pos, usage)
	if err != nil {
		return err
	}
	want, err := nightExpect(o)
	if err != nil {
		return err
	}
	if err := nightEdit(e, func(d *nightDoc) error {
		if err := d.expect(n, want); err != nil {
			return err
		}
		return d.done(n, o.Vals["result"], nightNow())
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "第 %d 項已移到「做完」\n", n)
	return err
}

func nightBlock(e *cmdline.Env, a []string) error {
	const usage = "evergenie night block <n> --reason \"<一句>\" [--expect \"<項目文字開頭>\"]"
	o, err := cmdline.ParseOpts(a, []string{"reason", "expect"}, nil, usage)
	if err != nil {
		return cmdline.Usagef("用法：%s", usage)
	}
	n, err := nightN(o.Pos, usage)
	if err != nil {
		return err
	}
	want, err := nightExpect(o)
	if err != nil {
		return err
	}
	if err := nightEdit(e, func(d *nightDoc) error {
		if err := d.expect(n, want); err != nil {
			return err
		}
		return d.block(n, o.Vals["reason"])
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "第 %d 項已移到「要主人」\n", n)
	return err
}

func nightRm(e *cmdline.Env, a []string) error {
	const usage = "evergenie night rm <n> [--expect \"<項目文字開頭>\"]"
	o, err := cmdline.ParseOpts(a, []string{"expect"}, nil, usage)
	if err != nil {
		return cmdline.Usagef("用法：%s", usage)
	}
	n, err := nightN(o.Pos, usage)
	if err != nil {
		return err
	}
	want, err := nightExpect(o)
	if err != nil {
		return err
	}
	if err := nightEdit(e, func(d *nightDoc) error {
		if err := d.expect(n, want); err != nil {
			return err
		}
		return d.rm(n)
	}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(e.Out, "已刪掉第 %d 項\n", n)
	return err
}
