package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"strconv"
	"strings"
)

// 兩層結構（法律第 8 條）：internal/base/<名> 是底層模組，internal/feature/<塊>/<名> 是業務塊（7 塊；塊之間不 import，同一塊的子套件可以），internal/tools 是工具（不檢查）。
const (
	layerBase    = "base"
	layerFeature = "feature"
	layerTools   = "tools"
	// appModule 是組裝框架，登記介面（Routes／Commands）的定義與合併在它那裡，不受「底層不准有 Routes／Commands」限制。
	appModule = "app"
)

// checkPathRules 檢查 internal/base/** 與 internal/feature/** 底下的一個 .go 檔（parts 是相對 internal/ 的路徑段），
// 測試檔也算：
//  1. 底層不能 import 任何業務塊；
//  2. 業務塊不能 import 別的業務塊（同一塊的子套件可以）；
//  3. 底層 import 別的底層，要是 architecture.md「底層模組」表上那一列允許的（自己的子套件可以）；
//  4. 底層與業務塊都不 import internal/tools（工具不是模組），也不 import internal/ 底下 base、feature 以外的路徑；
//  5. 底層不准實作 Routes() 或 Commands()（app 例外，測試檔不算）：有 HTTP 或指令就是業務塊。
//
// order 是「底層模組」表：模組 → 可以 import 的底層。回 true 表示有違規。
func checkPathRules(path string, parts []string, order map[string]map[string]bool, out io.Writer) bool {
	if len(parts) < 3 {
		fmt.Fprintf(out, "%s: 檔案要放在 internal/%s/<名稱>/ 底下\n", path, parts[0])
		return true
	}
	layer, name := parts[0], parts[1]
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		fmt.Fprintln(out, err)
		return true
	}
	bad := false
	for _, imp := range f.Imports {
		target, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			fmt.Fprintln(out, err)
			bad = true
			continue
		}
		rest, ok := strings.CutPrefix(target, modulePath+"/internal/")
		if !ok {
			continue
		}
		first, tail, _ := strings.Cut(rest, "/")
		to := strings.Split(tail, "/")[0]
		pos := fset.Position(imp.Pos())
		switch {
		case first == layerFeature && layer == layerBase:
			fmt.Fprintf(out, "%s: base/%s -> feature/%s 不允許（底層不能 import 業務塊，測試檔也算；要業務塊的行為就用假物件，整合測試放 cmd/evergenie）\n", pos, name, to)
			bad = true
		case first == layerFeature && to != name:
			fmt.Fprintf(out, "%s: feature/%s -> feature/%s 不允許（業務塊之間不互相 import〔同一塊的子套件可以〕，要對方的能力就在自己的 Deps 定義小 interface，由 registry 注入）\n", pos, name, to)
			bad = true
		case first == layerBase && layer == layerBase && to != name && !order[name][to]:
			fmt.Fprintf(out, "%s: base/%s -> base/%s 不允許（architecture.md「底層模組」表沒有列；底層之間只能往表上允許的方向 import）\n", pos, name, to)
			bad = true
		case first == layerTools:
			fmt.Fprintf(out, "%s: %s/%s -> tools/%s 不允許（工具不是模組，底層與業務塊不 import 它）\n", pos, layer, name, to)
			bad = true
		case first != layerBase && first != layerFeature:
			fmt.Fprintf(out, "%s: %s/%s -> internal/%s 不允許（internal/ 底下只有 base、feature、tools）\n", pos, layer, name, first)
			bad = true
		}
	}
	if layer == layerBase && name != appModule && !strings.HasSuffix(path, "_test.go") {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || (fn.Name.Name != "Routes" && fn.Name.Name != "Commands") {
				continue
			}
			fmt.Fprintf(out, "%s: base/%s 不准實作 %s()（有 HTTP 或指令就是業務塊，放 internal/feature/）\n", fset.Position(fn.Pos()), name, fn.Name.Name)
			bad = true
		}
	}
	return bad
}
