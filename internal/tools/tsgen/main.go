// tsgen 從 sdk/*.go 產生看板用的 TypeScript 型別與 client（D-5）：
// web/packages/sdk-web/src/gen/types.ts 與 client.ts。規則見 P4 共通定案 4。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/sdk"
)

const (
	header   = "// 由 internal/tools/tsgen 產生，不要手改\n"
	genDir   = "web/packages/sdk-web/src/gen"
	typesOut = genDir + "/types.ts"
	enumsOut = "internal/base/app/cmdline/enums_gen.go"
	clientTS = genDir + "/client.ts"
)

// reserved 是產生的程式碼自己會用到的 TypeScript 內建型別名稱；sdk 若有同名的 Go 型別
// （例如 vault 的 Record），會蓋掉內建的而讓 Record<string, T> 壞掉，所以 TypeScript 端改叫 Sdk<名稱>。
var reserved = map[string]bool{"Record": true, "AbortSignal": true, "Promise": true}

func tsName(goName string) string {
	if reserved[goName] {
		return "Sdk" + goName
	}
	return goName
}

// typeSet 是從一個 Go 目錄收集到的型別，輸出時依名稱排序。
type typeSet struct {
	decls map[string]string   // 型別名稱 → TypeScript 宣告
	enums map[string][]string // 字串列舉型別名稱 → 值（照原始碼順序）；也產生給 cmdline 的 --help 用（DEV-89）
}

// parseDir 讀目錄下所有非測試的 .go 檔，轉成 TypeScript 宣告。
func parseDir(dir string) (*typeSet, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	fset := token.NewFileSet()
	var files []*ast.File
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	// 每個 named string type 收集同檔的 const 值。
	known := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				if ts := spec.(*ast.TypeSpec); ts.Name.IsExported() {
					known[ts.Name.Name] = true
				}
			}
		}
	}
	set := &typeSet{decls: map[string]string{}, enums: map[string][]string{}}
	for _, f := range files {
		consts := stringConsts(f)
		for name, values := range consts {
			if known[name] {
				set.enums[name] = append(set.enums[name], values...)
			}
		}
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts := spec.(*ast.TypeSpec)
				if !ts.Name.IsExported() {
					continue
				}
				text, err := declare(ts, consts, known)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", fset.Position(ts.Pos()), err)
				}
				if text != "" {
					set.decls[tsName(ts.Name.Name)] = text
				}
			}
		}
	}
	return set, nil
}

// stringConsts 回傳檔案裡 `const X T = "…"` 依型別 T 分組的值（照原始碼順序）。
func stringConsts(f *ast.File) map[string][]string {
	out := map[string][]string{}
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			id, ok := vs.Type.(*ast.Ident)
			if !ok {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val, err := strconv.Unquote(lit.Value)
				if err != nil {
					continue
				}
				out[id.Name] = append(out[id.Name], val)
			}
		}
	}
	return out
}

// declare 把一個 Go 型別宣告轉成 TypeScript；不是 struct 或 named string 的（例如 interface）回空字串。
func declare(ts *ast.TypeSpec, consts map[string][]string, known map[string]bool) (string, error) {
	name := ts.Name.Name
	out := tsName(name)
	switch t := ts.Type.(type) {
	case *ast.InterfaceType:
		return "", nil
	case *ast.StructType:
		var b strings.Builder
		fmt.Fprintf(&b, "export interface %s {\n", out)
		for _, field := range t.Fields.List {
			if len(field.Names) == 0 {
				return "", fmt.Errorf("%s: 不支援內嵌欄位", name)
			}
			jsonName, omit, skip := jsonTag(field)
			if skip {
				continue
			}
			for _, n := range field.Names {
				if !n.IsExported() {
					continue
				}
				key := jsonName
				if key == "" {
					key = n.Name
				}
				tsType, err := tsOf(field.Type, known)
				if err != nil {
					return "", fmt.Errorf("%s.%s: %w", name, n.Name, err)
				}
				opt := ""
				if omit {
					opt = "?"
				}
				fmt.Fprintf(&b, "  %s%s: %s;\n", tsKey(key), opt, tsType)
			}
		}
		b.WriteString("}\n")
		return b.String(), nil
	default:
		tsType, err := tsOf(ts.Type, known)
		if err != nil {
			return "", fmt.Errorf("%s: %w", name, err)
		}
		if id, ok := ts.Type.(*ast.Ident); ok && id.Name == "string" && !ts.Assign.IsValid() {
			values := consts[name]
			if len(values) == 0 {
				return fmt.Sprintf("export type %s = string;\n", out), nil
			}
			quoted := make([]string, len(values))
			for i, v := range values {
				quoted[i] = strconv.Quote(v)
			}
			return fmt.Sprintf("export type %s = %s;\n", out, strings.Join(quoted, " | ")), nil
		}
		return fmt.Sprintf("export type %s = %s;\n", out, tsType), nil
	}
}

func jsonTag(field *ast.Field) (name string, omitempty, skip bool) {
	if field.Tag == nil {
		return "", false, false
	}
	raw, err := strconv.Unquote(field.Tag.Value)
	if err != nil {
		return "", false, false
	}
	tag := reflect.StructTag(raw).Get("json")
	if tag == "-" {
		return "", false, true
	}
	parts := strings.Split(tag, ",")
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return parts[0], omitempty, false
}

func tsKey(key string) string {
	for i, r := range key {
		ok := r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9')
		if !ok {
			return strconv.Quote(key)
		}
	}
	return key
}

var scalars = map[string]string{
	"string": "string", "bool": "boolean",
	"int": "number", "int8": "number", "int16": "number", "int32": "number", "int64": "number",
	"uint": "number", "uint8": "number", "uint16": "number", "uint32": "number", "uint64": "number",
	"float32": "number", "float64": "number",
	"any": "unknown",
}

// tsOf 把 Go 型別運算式轉成 TypeScript；不認得的型別回錯誤，不默默變 any。
func tsOf(expr ast.Expr, known map[string]bool) (string, error) {
	switch t := expr.(type) {
	case *ast.Ident:
		if s, ok := scalars[t.Name]; ok {
			return s, nil
		}
		if known[t.Name] {
			return tsName(t.Name), nil
		}
		return "", fmt.Errorf("不認得的型別 %s", t.Name)
	case *ast.StarExpr:
		return tsOf(t.X, known)
	case *ast.ArrayType:
		if t.Len != nil {
			return "", fmt.Errorf("不支援固定長度陣列")
		}
		// encoding/json 把 []byte 編成 base64 字串。
		if id, ok := t.Elt.(*ast.Ident); ok && id.Name == "byte" {
			return "string", nil
		}
		elem, err := tsOf(t.Elt, known)
		if err != nil {
			return "", err
		}
		if strings.ContainsAny(elem, " |") {
			elem = "(" + elem + ")"
		}
		return elem + "[]", nil
	case *ast.MapType:
		if id, ok := t.Key.(*ast.Ident); !ok || id.Name != "string" {
			return "", fmt.Errorf("map 的 key 只支援 string")
		}
		val, err := tsOf(t.Value, known)
		if err != nil {
			return "", err
		}
		return "Record<string, " + val + ">", nil
	case *ast.InterfaceType:
		if len(t.Methods.List) == 0 {
			return "unknown", nil
		}
		return "", fmt.Errorf("不支援有方法的 interface 欄位")
	case *ast.SelectorExpr:
		if pkg, ok := t.X.(*ast.Ident); ok {
			switch pkg.Name + "." + t.Sel.Name {
			case "time.Time":
				return "string", nil
			case "time.Duration":
				return "number", nil
			case "json.RawMessage":
				return "unknown", nil
			}
			return "", fmt.Errorf("不認得的型別 %s.%s", pkg.Name, t.Sel.Name)
		}
	}
	return "", fmt.Errorf("不認得的型別 %T", expr)
}

// renderEnums 產生 cmdline 的 enumValues：子指令 --help 列出列舉旗標能填哪些值。
func (s *typeSet) renderEnums() ([]byte, error) {
	names := make([]string, 0, len(s.enums))
	for n := range s.enums {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	b.WriteString(header + "\npackage cmdline\n\n// enumValues 是 sdk 的字串列舉型別 → 可以填的值（照 sdk 原始碼順序）。\nvar enumValues = map[string][]string{\n")
	for _, n := range names {
		quoted := make([]string, len(s.enums[n]))
		for i, v := range s.enums[n] {
			quoted[i] = strconv.Quote(v)
		}
		fmt.Fprintf(&b, "%q: {%s},\n", n, strings.Join(quoted, ", "))
	}
	b.WriteString("}\n")
	return format.Source([]byte(b.String()))
}

func (s *typeSet) render() []byte {
	names := make([]string, 0, len(s.decls))
	for n := range s.decls {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.WriteString(header)
	for _, n := range names {
		b.WriteString("\n")
		b.WriteString(s.decls[n])
	}
	return b.Bytes()
}

// camel 把 get_menu 轉成 getMenu。
func camel(s string) string {
	parts := strings.Split(s, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	return strings.Join(parts, "")
}

// renderClient 照 routes 產生 client.ts；Request／Response 必須是 set 裡的型別。
func renderClient(routes []sdk.Route, set *typeSet) ([]byte, error) {
	used := map[string]bool{}
	type entry struct{ method, text string }
	services := map[string][]entry{}
	var order []string
	for _, r := range routes {
		req, resp := tsName(r.Request), tsName(r.Response)
		for _, n := range []string{req, resp} {
			if _, ok := set.decls[n]; !ok {
				return nil, fmt.Errorf("route %s 用到 sdk 沒有的型別 %s", r.Path, n)
			}
			used[n] = true
		}
		var text string
		if r.Stream {
			text = fmt.Sprintf("(req: %[1]s, onItem: (item: %[2]s) => void, signal?: AbortSignal, onReconnect?: () => void) =>\n        stream<%[1]s, %[2]s>(base + %[3]q, req, onItem, signal, onReconnect)", req, resp, r.Path)
		} else {
			text = fmt.Sprintf("(req: %[1]s) => call<%[1]s, %[2]s>(base + %[3]q, req)", req, resp, r.Path)
		}
		if _, ok := services[r.Service]; !ok {
			order = append(order, r.Service)
		}
		services[r.Service] = append(services[r.Service], entry{camel(r.Method), text})
	}
	sort.Strings(order)
	names := make([]string, 0, len(used))
	for n := range used {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("\nimport { call, stream } from \"../transport\";\nimport type {\n")
	for _, n := range names {
		fmt.Fprintf(&b, "  %s,\n", n)
	}
	// makeApi(base)：每個方法的路徑前面接 base（另一台看板的 https origin，S3 看板連多台）；
	// api 是 base 為空的那一份，跟以前一樣打同源的相對路徑，現有呼叫端不用改。
	b.WriteString("} from \"./types\";\n\n")
	b.WriteString("/** 照 base（另一台看板的 https origin，例 https://genie-amy-office.example.ts.net；空字串＝同源）產生一份 API 用戶端。\n * base 要是整理過的 origin（不帶結尾斜線，home.toml 讀進來的 web.sites 已經是）。 */\n")
	b.WriteString("export function makeApi(base: string) {\n  return {\n")
	for _, svc := range order {
		fmt.Fprintf(&b, "    %s: {\n", tsKey(svc))
		entries := services[svc]
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].method < entries[j].method })
		for _, e := range entries {
			fmt.Fprintf(&b, "      %s: %s,\n", e.method, e.text)
		}
		b.WriteString("    },\n")
	}
	b.WriteString("  };\n}\n\nexport type Api = ReturnType<typeof makeApi>;\n\n/** 同源的 API 用戶端（這個看板自己的家）。 */\nexport const api: Api = makeApi(\"\");\n")
	return b.Bytes(), nil
}

// generate 回傳 路徑（相對 root）→ 內容。
func generate(root string) (map[string][]byte, error) {
	set, err := parseDir(filepath.Join(root, "sdk"))
	if err != nil {
		return nil, err
	}
	client, err := renderClient(sdk.Routes, set)
	if err != nil {
		return nil, err
	}
	enums, err := set.renderEnums()
	if err != nil {
		return nil, err
	}
	return map[string][]byte{typesOut: set.render(), clientTS: client, enumsOut: enums}, nil
}

func run(root string, check bool, out io.Writer) int {
	files, err := generate(root)
	if err != nil {
		fmt.Fprintln(out, "tsgen:", err)
		return 1
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	bad := false
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(p))
		if check {
			have, err := os.ReadFile(full)
			if err != nil || !bytes.Equal(have, files[p]) {
				fmt.Fprintf(out, "%s 跟 sdk/ 產生的不一樣，請執行 go run ./internal/tools/tsgen .\n", p)
				bad = true
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			fmt.Fprintln(out, "tsgen:", err)
			return 1
		}
		if err := os.WriteFile(full, files[p], 0o644); err != nil {
			fmt.Fprintln(out, "tsgen:", err)
			return 1
		}
	}
	if bad {
		return 1
	}
	return 0
}

func main() {
	check := flag.Bool("check", false, "只比對，不寫檔；跟 git 裡的不一樣就失敗")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "用法：tsgen [-check] <專案根目錄>")
		os.Exit(2)
	}
	os.Exit(run(flag.Arg(0), *check, os.Stderr))
}
