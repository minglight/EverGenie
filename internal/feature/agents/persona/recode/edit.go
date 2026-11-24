package recode

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/minglight/evergenie/internal/base/vault"
)

// 改檔只動要改的那段字：紀錄檔改檔頭某一行的值，TOML 改某一行的值或鍵；註解、空行、排列、其他位元組都不動。
// 改完一定再解析一次對過（欄位除了要改的都一樣），對不上就不改、回錯——寧可列出來請人看，也不要改壞主人的檔。

// change 是一個值的舊、新。
type change struct{ old, new string }

func splitLines(s string) []string { return strings.SplitAfter(s, "\n") }

func trimEOL(s string) (string, string) {
	switch {
	case strings.HasSuffix(s, "\r\n"):
		return s[:len(s)-2], "\r\n"
	case strings.HasSuffix(s, "\n"):
		return s[:len(s)-1], "\n"
	}
	return s, ""
}

// replaceScalar 在 v（冒號或等號後面那段，含前面的空白）裡找開頭的值：雙引號、單引號或（YAML）不加引號；
// 值剛好是 old 就換成 new（同樣的引號），回新的那段。plain＝允許不加引號的寫法（YAML 才有）。
func replaceScalar(v, old, new string, plain bool) (string, bool) {
	ws := len(v) - len(strings.TrimLeft(v, " \t"))
	lead, rest := v[:ws], v[ws:]
	switch {
	case strings.HasPrefix(rest, `"`):
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return v, false
		}
		inner := rest[1 : 1+end]
		if strings.ContainsRune(inner, '\\') || inner != old {
			return v, false
		}
		return lead + `"` + new + `"` + rest[2+end:], true
	case strings.HasPrefix(rest, `'`):
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 || strings.HasPrefix(rest[2+end:], "'") {
			return v, false
		}
		if rest[1:1+end] != old {
			return v, false
		}
		return lead + "'" + new + "'" + rest[2+end:], true
	case plain:
		val, tail := rest, ""
		if i := strings.Index(rest, " #"); i >= 0 {
			val, tail = rest[:i], rest[i:]
		}
		trimmed := strings.TrimRight(val, " \t")
		if trimmed != old {
			return v, false
		}
		return lead + new + val[len(trimmed):] + tail, true
	}
	return v, false
}

// editFrontmatter 改紀錄檔檔頭最上層幾個欄位的值（key → 舊值、新值），回新內容與改了哪幾行。
// 檔頭找不到「key: 舊值」這一行（例如值寫成多行），或改完解析出來不對，回錯。
func editFrontmatter(data []byte, changes map[string]change) ([]byte, []Edit, error) {
	lines := splitLines(string(data))
	if first, _ := trimEOL(lines[0]); first != "---" {
		return nil, nil, fmt.Errorf("檔頭不是 --- 開頭")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if l, _ := trimEOL(lines[i]); l == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return nil, nil, fmt.Errorf("檔頭沒有結束的 ---")
	}
	done := map[string]bool{}
	var edits []Edit
	for i := 1; i < end; i++ {
		body, eol := trimEOL(lines[i])
		for key, c := range changes {
			if done[key] || !strings.HasPrefix(body, key+":") {
				continue
			}
			v := body[len(key)+1:]
			if v != "" && v[0] != ' ' && v[0] != '\t' {
				continue
			}
			nv, ok := replaceScalar(v, c.old, c.new, true)
			if !ok {
				continue
			}
			nb := key + ":" + nv
			edits = append(edits, Edit{Line: i + 1, Before: body, After: nb})
			lines[i] = nb + eol
			done[key] = true
		}
	}
	for key, c := range changes {
		if !done[key] {
			return nil, nil, fmt.Errorf("檔頭找不到「%s: %s」這一行（值可能寫成好幾行），沒改", key, c.old)
		}
	}
	out := []byte(strings.Join(lines, ""))
	before, _, err := vault.Decode(data)
	if err != nil {
		return nil, nil, fmt.Errorf("檔頭讀不懂：%s", err.Error())
	}
	after, _, err := vault.Decode(out)
	if err != nil {
		return nil, nil, fmt.Errorf("改完讀不懂，沒改")
	}
	want := map[string]any{}
	for k, v := range before.Fields {
		want[k] = v
	}
	for k, c := range changes {
		want[k] = c.new
	}
	if !reflect.DeepEqual(want, after.Fields) || before.Body != after.Body {
		return nil, nil, fmt.Errorf("改完的欄位跟預期不一樣，沒改")
	}
	return out, edits, nil
}

var (
	tomlHeader = regexp.MustCompile(`^\s*\[([^\[\]]+)\]\s*(#.*)?$`)
	tomlArray  = regexp.MustCompile(`^\s*\[\[`)
	// 一行一個鍵＝單行字串：鍵是不加引號的、雙引號或單引號的（不認得 a.b = 這種點起來的鍵）。
	tomlKV = regexp.MustCompile(`^(\s*)("(?:[^"\\]|\\.)*"|'[^']*'|[A-Za-z0-9_-]+)(\s*=)(.*)$`)
)

// tomlPath 把表頭或鍵的「a."b.c".d」拆成幾段。
func tomlPath(s string) []string {
	var out []string
	var b strings.Builder
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
				continue
			}
			b.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			out = append(out, strings.TrimSpace(b.String()))
			b.Reset()
		case c == ' ' || c == '\t':
		default:
			b.WriteByte(c)
		}
	}
	return append(out, strings.TrimSpace(b.String()))
}

func unquoteKey(k string) string {
	if strings.HasPrefix(k, `"`) {
		if s, err := strconv.Unquote(k); err == nil {
			return s
		}
		return k
	}
	return strings.Trim(k, "'")
}

// tomlEdit 是對一個 TOML 檔要做的事：把某個表的某個鍵的字串值換掉、把某個表裡的鍵改名、拿掉某個表的某個鍵（整行）。
// match 決定是哪個表（表頭拆成幾段；最上層是空的）。
type tomlEdit struct {
	match  func(table []string) bool
	key    string // 鍵（解開引號後）
	old    string // set：舊值；rename：（不用）
	new    string // set：新值；rename：新的鍵
	op     string // set｜rename｜delete
	expect func(m map[string]any)
}

// editTOML 照 edits 改，回新內容與改了哪幾行；改完解析出來要等於「原本的解析＋每個 edit 的 expect」，不然回錯。
// 一個 edit 一處都沒改到不算錯（呼叫的人先確定要改才給）；verify 會抓到「值在、但寫法認不得」的情形。
func editTOML(data []byte, edits []tomlEdit) ([]byte, []Edit, error) {
	lines := splitLines(string(data))
	var table []string
	inMulti := ""
	var out []Edit
	for i := range lines {
		body, eol := trimEOL(lines[i])
		if inMulti != "" {
			if strings.Contains(body, inMulti) {
				inMulti = ""
			}
			continue
		}
		if tomlArray.MatchString(body) {
			table = []string{"\x00array"}
			continue
		}
		if m := tomlHeader.FindStringSubmatch(body); m != nil {
			table = tomlPath(m[1])
			continue
		}
		m := tomlKV.FindStringSubmatch(body)
		if m == nil {
			continue
		}
		val := strings.TrimLeft(m[4], " \t")
		for _, q := range []string{`"""`, `'''`} {
			if strings.HasPrefix(val, q) && !strings.Contains(val[3:], q) {
				inMulti = q
			}
		}
		key := unquoteKey(m[2])
		for _, e := range edits {
			if e.key != key || !e.match(table) {
				continue
			}
			switch e.op {
			case "set":
				nv, ok := replaceScalar(m[4], e.old, e.new, false)
				if !ok {
					continue
				}
				nb := m[1] + m[2] + m[3] + nv
				out = append(out, Edit{Line: i + 1, Before: body, After: nb})
				lines[i] = nb + eol
			case "rename":
				q := strconv.Quote(e.new)
				if strings.HasPrefix(m[2], "'") {
					q = "'" + e.new + "'"
				}
				nb := m[1] + q + m[3] + m[4]
				out = append(out, Edit{Line: i + 1, Before: body, After: nb})
				lines[i] = nb + eol
			case "delete":
				out = append(out, Edit{Line: i + 1, Before: body, After: ""})
				lines[i] = ""
			}
		}
	}
	result := []byte(strings.Join(lines, ""))
	want := map[string]any{}
	if _, err := toml.Decode(string(data), &want); err != nil {
		return nil, nil, fmt.Errorf("讀不懂（TOML 格式不對）")
	}
	for _, e := range edits {
		e.expect(want)
	}
	got := map[string]any{}
	if _, err := toml.Decode(string(result), &got); err != nil {
		return nil, nil, fmt.Errorf("改完讀不懂，沒改")
	}
	if !reflect.DeepEqual(want, got) {
		return nil, nil, fmt.Errorf("有地方的寫法認不得（例如寫成 a.b = 或放在一行的表裡），沒改")
	}
	return result, out, nil
}

// tableAt 回 m 裡 path 那個表（沒有回 nil）。
func tableAt(m map[string]any, path []string) map[string]any {
	cur := m
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func samePath(a []string) func([]string) bool {
	return func(b []string) bool { return reflect.DeepEqual(a, b) }
}

// setString：path 表的 key 等於 old 就換成 new。
func setString(path []string, key, old, new string) tomlEdit {
	return tomlEdit{match: samePath(path), key: key, old: old, new: new, op: "set", expect: func(m map[string]any) {
		if t := tableAt(m, path); t != nil && t[key] == old {
			t[key] = new
		}
	}}
}

// renameKey：path 表的鍵 old 改名成 new（值不變）。
func renameKey(path []string, old, new string) tomlEdit {
	return tomlEdit{match: samePath(path), key: old, new: new, op: "rename", expect: func(m map[string]any) {
		if t := tableAt(m, path); t != nil {
			if v, ok := t[old]; ok {
				delete(t, old)
				t[new] = v
			}
		}
	}}
}

// deleteKey：拿掉 path 表的鍵（整行）。
func deleteKey(path []string, key string) tomlEdit {
	return tomlEdit{match: samePath(path), key: key, op: "delete", expect: func(m map[string]any) {
		if t := tableAt(m, path); t != nil {
			delete(t, key)
		}
	}}
}
