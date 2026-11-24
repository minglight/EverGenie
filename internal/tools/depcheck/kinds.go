package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/tools/checkutil"
)

// vault 的方法：第幾個參數是種類，以及是不是只讀。Watch 從第 1 個參數之後全部都是種類。
var vaultMethods = map[string]struct {
	arg  int
	read bool
}{
	"Get": {1, true}, "List": {1, true}, "Watch": {1, true},
	"Patch": {1, false}, "Append": {1, false}, "Archive": {1, false}, "ArchiveNow": {1, false},
	"Move": {1, false}, "NextID": {1, false}, "EnsureCounter": {1, false},
}

// checkKinds 檢查「每個 vault 種類只有擁有的模組直接讀寫」（architecture.md「資料種類的擁有者」）：
// 掃 internal/ 底下（tools、base/vault 自己、testdata、測試檔除外）每個套件，找
//   - 叫在名字含 vault 的東西上的 vault 方法（m.vault.Get(ctx, "task", …)、m.deps.Vault.List(ctx, kind, …)），
//   - `vault.Record{Kind: …}` 的組合字面值（Put 寫入），
//
// 種類是字串字面值或同一個套件裡的字串常數才判斷；算出來的（變數、參數）看不出來，不檢查。
// 模組名：internal/feature/<塊>/… 是 <塊>（S8-2 起同一塊的子套件共用那塊的資料；表上寫 `<塊>/<子套件>` 的種類只有那個子套件算），
// internal/base/<名>/… 是 <名>。
// 經 sdk client 走 HTTP 的（指令、套件）不在這裡管：那是公開的資料 API。
func checkKinds(root string, out io.Writer) bool {
	owners, err := checkutil.KindOwners(root)
	if err != nil {
		fmt.Fprintln(out, err)
		return true
	}
	internal := filepath.Join(root, "internal")
	pkgs := map[string][]string{}
	err = filepath.WalkDir(internal, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(internal, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if d.IsDir() {
			if d.Name() == "testdata" || parts[0] == layerTools || (parts[0] == layerBase && len(parts) > 1 && parts[1] == "vault") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			pkgs[filepath.Dir(path)] = append(pkgs[filepath.Dir(path)], path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(out, err)
		return true
	}
	dirs := make([]string, 0, len(pkgs))
	for dir := range pkgs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	bad := false
	for _, dir := range dirs {
		rel, _ := filepath.Rel(internal, dir)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if checkKindPackage(pkgs[dir], moduleOf(parts), subModuleOf(parts), owners, out) {
			bad = true
		}
	}
	return bad
}

func moduleOf(parts []string) string {
	if (parts[0] == layerBase || parts[0] == layerFeature) && len(parts) > 1 {
		return parts[1]
	}
	return parts[0]
}

// subModuleOf 是業務塊裡的子套件名（internal/feature/inbox/notify/… → inbox/notify）；不是業務塊的子套件就跟 moduleOf 一樣。
// 擁有者表可以把種類交給一個子套件（S8-5：message 只有 inbox/notify），這時同塊別的子套件也不能直接讀寫。
func subModuleOf(parts []string) string {
	if parts[0] == layerFeature && len(parts) > 2 {
		return parts[1] + "/" + parts[2]
	}
	return moduleOf(parts)
}

func checkKindPackage(files []string, mod, sub string, owners map[string]checkutil.KindOwner, out io.Writer) bool {
	fset := token.NewFileSet()
	var parsed []*ast.File
	consts := map[string]string{}
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			fmt.Fprintln(out, err)
			return true
		}
		parsed = append(parsed, f)
		ast.Inspect(f, func(n ast.Node) bool {
			g, ok := n.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				return true
			}
			for _, spec := range g.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) {
						if s, ok := stringLit(vs.Values[i]); ok {
							consts[name.Name] = s
						}
					}
				}
			}
			return false
		})
	}
	kindOf := func(e ast.Expr) (string, bool) {
		if s, ok := stringLit(e); ok {
			return s, true
		}
		if id, ok := e.(*ast.Ident); ok {
			s, ok := consts[id.Name]
			return s, ok
		}
		return "", false
	}
	bad := false
	report := func(pos token.Pos, kind string, read bool) {
		o, ok := owners[kind]
		switch {
		case !ok:
			fmt.Fprintf(out, "%s: vault 種類 %q 沒有登記擁有者（architecture.md 資料種類的擁有者）\n", fset.Position(pos), kind)
			bad = true
		case o.Owner == mod || o.Owner == sub:
		case read && (o.Readers[mod] || o.Readers[sub]):
		default:
			verb := "寫"
			if read {
				verb = "讀"
			}
			who, how := mod, "經 Deps 小介面或事件問"
			if strings.HasPrefix(o.Owner, mod+"/") {
				who, how = sub, "直接呼叫同塊的" // 同一塊、但種類只給塊裡的一個子套件（MOD-2：同塊直接 import）
			}
			fmt.Fprintf(out, "%s: %s 直接%s %s 的資料（%q）不允許，%s %s（architecture.md 資料種類的擁有者）\n", fset.Position(pos), who, verb, o.Owner, kind, how, o.Owner)
			bad = true
		}
	}
	for _, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				sel, ok := x.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				m, ok := vaultMethods[sel.Sel.Name]
				if !ok || !strings.Contains(strings.ToLower(lastName(sel.X)), "vault") || len(x.Args) <= m.arg {
					return true
				}
				args := x.Args[m.arg : m.arg+1]
				if sel.Sel.Name == "Watch" {
					args = x.Args[m.arg:]
				}
				for _, a := range args {
					if kind, ok := kindOf(a); ok {
						report(a.Pos(), kind, m.read)
					}
				}
			case *ast.CompositeLit:
				t, ok := x.Type.(*ast.SelectorExpr)
				if !ok || t.Sel.Name != "Record" || lastName(t.X) != "vault" {
					return true
				}
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if k, isID := kv.Key.(*ast.Ident); !ok || !isID || k.Name != "Kind" {
						continue
					}
					if kind, ok := kindOf(kv.Value); ok {
						report(kv.Value.Pos(), kind, false)
					}
				}
			}
			return true
		})
	}
	return bad
}

// lastName 回呼叫對象最後一段的名字：m.deps.Vault → Vault，vault → vault，m.vaultFor(x) → vaultFor。
func lastName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	case *ast.CallExpr:
		return lastName(x.Fun)
	}
	return ""
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
