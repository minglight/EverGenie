package cmdline

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// Spec 描述一個指令怎麼把命令列對到 Request 型別。旗標名＝Request 的 JSON 欄位名。
type Spec struct {
	ID     bool              // 第一個位置參數是 id
	Free   bool              // 位置參數交給指令自己處理
	Skip   []string          // 不開旗標的 JSON 欄位名
	Rename map[string]string // 旗標名 → JSON 欄位名（例外：--grant → grants）
	Split  map[string]string // JSON 欄位名 → 一個值裡的分隔字元（例：options 以 | 切開）
}

// Parsed 是解析完的請求與命令列上有給的旗標（JSON 欄位名）。
type Parsed[Req any] struct {
	Req *Req
	Pos []string
	Set map[string]bool
}

func jsonName(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
	if name == "-" {
		return ""
	}
	return name
}

var timeType = reflect.TypeFor[time.Time]()

// timeLayouts 是時間旗標收的寫法；沒有時區的照本機時間。
var timeLayouts = []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"}

// scalar 回傳型別能不能用旗標填：字串（含別名）、整數、布林、時間、它們的指標、字串切片。
func scalar(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int32, reflect.Int64:
		return true
	case reflect.Slice:
		return t.Elem().Kind() == reflect.String
	}
	return false
}

// flagFields 是請求型別能用旗標填的欄位（JSON 欄位名 → 欄位），照結構的順序另回一份名稱；by 與 Skip 的不算。
func flagFields(v reflect.Value, s Spec) (map[string]reflect.Value, []string, map[string]reflect.Value) {
	fields := map[string]reflect.Value{}
	byName := map[string]reflect.Value{}
	var order []string
	for i := range v.NumField() {
		f := v.Type().Field(i)
		name := jsonName(f)
		if name == "" {
			continue
		}
		byName[name] = v.Field(i)
		if name == "by" || !scalar(f.Type) || slices.Contains(s.Skip, name) {
			continue
		}
		fields[name] = v.Field(i)
		order = append(order, name)
	}
	return fields, order, byName
}

// Prepare 把命令列旗標解析進 Req：旗標名是 Req 的 JSON 欄位名，`by` 欄位填 e.By。
// 有 --help 或 -h 就不解析，回 HelpText（Run 印出這個子指令的旗標、結束 0，DEV-89）。
func Prepare[Req any](e *Env, args []string, s Spec) (*Parsed[Req], error) {
	p := &Parsed[Req]{Req: new(Req), Set: map[string]bool{}}
	v := reflect.ValueOf(p.Req).Elem()
	fields, order, byName := flagFields(v, s)
	for _, a := range args {
		if a == "--" {
			break
		}
		if a == "--help" || a == "-h" {
			return nil, &HelpText{Text: flagHelp(e.Cmd, v, fields, order, s)}
		}
	}
	for flag, name := range s.Rename {
		if f, ok := fields[name]; ok {
			fields[flag] = f
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			p.Pos = append(p.Pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			p.Pos = append(p.Pos, a)
			continue
		}
		name, val, hasVal := strings.Cut(a[2:], "=")
		f, ok := fields[name]
		if !ok {
			return nil, Usagef("不認得的旗標 --%s；能用的旗標：%s（加 --help 看說明）", name, flagList(order, s))
		}
		if !isBool(f.Type()) && !hasVal {
			if i+1 >= len(args) {
				return nil, Usagef("旗標 --%s 後面要接一個值", name)
			}
			i++
			val = args[i]
		}
		field := name
		if n, ok := s.Rename[name]; ok {
			field = n
		}
		if err := assign(f, name, val, s.Split[field]); err != nil {
			return nil, err
		}
		p.Set[field] = true
	}
	if by, ok := byName["by"]; ok && by.Type() == reflect.TypeFor[*sdk.Actor]() {
		by.Set(reflect.ValueOf(&sdk.Actor{Name: e.By}))
	}
	if s.ID {
		if len(p.Pos) != 1 {
			return nil, Usagef("需要一個編號（例：SEC-1）")
		}
		if p.Set["id"] {
			return nil, Usagef("編號只要給一次（位置參數或 --id）")
		}
		byName["id"].SetString(p.Pos[0])
		p.Pos = nil
	} else if !s.Free && len(p.Pos) > 0 {
		return nil, Usagef("多餘的參數：%s", strings.Join(p.Pos, " "))
	}
	return p, nil
}

func isBool(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Bool
}

func assign(f reflect.Value, flag, val, sep string) error {
	t := f.Type()
	if t.Kind() == reflect.Pointer {
		if f.IsNil() {
			f.Set(reflect.New(t.Elem()))
		}
		f, t = f.Elem(), t.Elem()
	}
	if t == timeType {
		for _, layout := range timeLayouts {
			if at, err := time.ParseInLocation(layout, val, time.Local); err == nil {
				f.Set(reflect.ValueOf(at))
				return nil
			}
		}
		return Usagef("旗標 --%s 要填時間（例：2026-10-24T09:30 或 2026-10-24T09:30:00+08:00）", flag)
	}
	switch t.Kind() {
	case reflect.String:
		f.SetString(val)
	case reflect.Bool:
		b := true
		if val != "" {
			var err error
			if b, err = strconv.ParseBool(val); err != nil {
				return Usagef("旗標 --%s 要填 true 或 false", flag)
			}
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(val, 10, t.Bits())
		if err != nil {
			return Usagef("旗標 --%s 要填整數", flag)
		}
		f.SetInt(n)
	case reflect.Slice:
		parts := []string{val}
		if sep != "" {
			parts = strings.Split(val, sep)
		}
		for _, part := range parts {
			if part = strings.TrimSpace(part); part != "" || sep == "" {
				f.Set(reflect.Append(f, reflect.ValueOf(part).Convert(t.Elem())))
			}
		}
	}
	return nil
}

// Send 呼叫 API 並印出回應的主要物件。
func Send[Req, Resp any](e *Env, p *Parsed[Req], call func(context.Context, *Req) (*Resp, error), pick func(*Resp) any) error {
	resp, err := call(e.Ctx, p.Req)
	if err != nil {
		return err
	}
	return e.Line(pick(resp))
}

// One 解析旗標、呼叫 API、印出回應的主要物件。
func One[Req, Resp any](e *Env, args []string, s Spec, call func(context.Context, *Req) (*Resp, error), pick func(*Resp) any) error {
	p, err := Prepare[Req](e, args, s)
	if err != nil {
		return err
	}
	return Send(e, p, call, pick)
}

// Each 跟 One 一樣，但回應是一串項目：一行一筆，有下一頁就接著取。
func Each[Req, Resp, Item any](e *Env, args []string, s Spec, call func(context.Context, *Req) (*Resp, error), page func(*Req, string), items func(*Resp) ([]Item, string)) error {
	p, err := Prepare[Req](e, args, s)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for {
		resp, err := call(e.Ctx, p.Req)
		if err != nil {
			return err
		}
		list, next := items(resp)
		for _, it := range list {
			if err := e.Line(it); err != nil {
				return err
			}
		}
		if next == "" || seen[next] {
			return nil
		}
		seen[next] = true
		page(p.Req, next)
	}
}

// NextCursor 取分頁回應的下一頁游標；沒有分頁就是空字串。
func NextCursor(p *sdk.PageResponse) string {
	if p == nil {
		return ""
	}
	return p.NextCursor
}

// flagList 是能用的旗標，空白隔開（不認得的旗標時列給人看）。
func flagList(order []string, s Spec) string {
	out := make([]string, 0, len(order)+len(s.Rename))
	for _, n := range order {
		out = append(out, "--"+n)
	}
	for flag := range s.Rename {
		out = append(out, "--"+flag)
	}
	slices.Sort(out[len(order):])
	return strings.Join(out, " ")
}

// flagHelp 是一個子指令的說明：用法一行，再一個旗標一行（型別、列舉值、能不能重複），照請求型別產生，不用手寫（DEV-89）。
func flagHelp(cmd string, v reflect.Value, fields map[string]reflect.Value, order []string, s Spec) string {
	if s.ID {
		order = slices.DeleteFunc(slices.Clone(order), func(n string) bool { return n == "id" }) // 編號是位置參數
	}
	var b strings.Builder
	if cmd == "" {
		cmd = "<指令>"
	}
	b.WriteString("用法：evergenie " + cmd)
	if s.ID {
		b.WriteString(" <編號>")
	} else if s.Free {
		b.WriteString(" [參數…]")
	}
	if len(order) > 0 {
		b.WriteString(" [--旗標 值]\n旗標（名稱就是請求的欄位名）：\n")
	} else {
		b.WriteString("\n沒有旗標。\n")
	}
	width := 0
	for _, n := range order {
		width = max(width, len(n))
	}
	alias := map[string][]string{}
	for flag, field := range s.Rename {
		alias[field] = append(alias[field], "--"+flag)
	}
	for _, n := range order {
		fmt.Fprintf(&b, "  --%-*s  %s", width, n, flagKind(fields[n].Type(), s.Split[n]))
		if a := alias[n]; len(a) > 0 {
			slices.Sort(a)
			b.WriteString("（也可以寫 " + strings.Join(a, "、") + "）")
		}
		b.WriteString("\n")
	}
	b.WriteString("必填的欄位由服務檢查，缺了會說是哪一個。\n")
	return b.String()
}

// flagKind 是旗標要填什麼的白話：列舉型別列出全部的值（enums_gen.go，由 tsgen 從 sdk 產生）。
func flagKind(t reflect.Type, sep string) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		return "時間（例 2026-10-24T09:30）"
	}
	switch t.Kind() {
	case reflect.Bool:
		return "開關（不用接值）"
	case reflect.Int, reflect.Int32, reflect.Int64:
		return "整數"
	case reflect.Slice:
		kind := flagKind(t.Elem(), "")
		if sep != "" {
			return kind + "，一個值裡用 " + sep + " 分隔多個"
		}
		return kind + "，可重複給"
	}
	if values := enumValues[t.Name()]; t.PkgPath() == sdkPkg && len(values) > 0 {
		return strings.Join(values, "|")
	}
	return "文字"
}

var sdkPkg = reflect.TypeFor[sdk.Actor]().PkgPath()
