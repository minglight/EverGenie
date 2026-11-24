package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/minglight/evergenie/internal/base/platform"
)

// 跟著發行檔帶的說明文件（doc-style DOC-12，DEV-155）：清單只有 ai_docs/bundled.txt 一份（platform PLT-28，
// scripts/dist.sh 照它帶、`evergenie docs` 照它給）。這裡守三件事：
//  1. 清單讀得懂，上面的檔都在 ai_docs/。
//  2. 帶的文件提到的 ai_docs 文件也在清單上（連結或寫出檔名都算）：沒帶的就是下載版的死連結，要嘛一起帶、要嘛改寫那一句。
//     寫成 ai_docs/… 或 conventions|schemas|modules|runbooks/ 開頭（含 ../）的，不管 repo 裡有沒有那個檔都算。
//  3. 秘書會讀到的文字不寫 repo 的路徑 ai_docs/…（下載版沒有 repo），也不寫沒帶的 conventions|schemas|modules|runbooks/*.md；
//     指文件一律用 `evergenie docs show <文件>`，而且那份要在清單上。秘書會讀到的文字＝跟著發行檔的原始碼與文字全文
//     （官方套件——開發用的 dev_only 套件除外——的每個檔、sdk 的 .go，含註解）、核心內嵌的 .md、核心 Go 正式程式碼的字串、
//     看板原始碼不是註解的行（核心與看板只發執行檔，註解是給開發的人看的）。測試檔與 testdata 不算。
func bundledProblems(root string) []string {
	docsRoot := filepath.Join(root, platform.ProgramDocsDir)
	data, err := os.ReadFile(filepath.Join(docsRoot, platform.ProgramDocsList))
	if err != nil {
		return []string{"沒有跟著發行檔帶的文件清單 ai_docs/" + platform.ProgramDocsList}
	}
	docs, err := platform.ParseProgramDocs(string(data))
	if err != nil {
		return []string{"ai_docs/" + platform.ProgramDocsList + "：" + err.Error()}
	}
	var problems []string
	listed := map[string]bool{}
	isFile := func(rel string) bool {
		info, err := os.Stat(filepath.Join(docsRoot, filepath.FromSlash(rel)))
		return err == nil && info.Mode().IsRegular()
	}
	for _, d := range docs {
		listed[d.Path] = true
		if !isFile(d.Path) {
			problems = append(problems, "清單上的 "+d.Path+" 不在 ai_docs/")
		}
	}
	for _, d := range docs {
		text, err := os.ReadFile(filepath.Join(docsRoot, filepath.FromSlash(d.Path)))
		if err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, ref := range docRefs(string(text)) {
			cands := refCandidates(d.Path, ref)
			ok := false
			for _, c := range cands {
				ok = ok || listed[c]
			}
			if ok {
				continue
			}
			if p := docsRootRef(d.Path, ref); p != "" {
				// 寫明是 ai_docs 的文件（不管檔在不在：不在的更是死連結）
				if !seen[p] {
					seen[p] = true
					problems = append(problems, "帶的文件 "+d.Path+" 提到沒帶的 "+p+"（一起加進 ai_docs/bundled.txt，或改寫成不寫路徑的說法，例：說明卡 packages 的 PKG-30）")
				}
				continue
			}
			for _, c := range cands {
				if isFile(c) && !seen[c] {
					seen[c] = true
					problems = append(problems, "帶的文件 "+d.Path+" 提到沒帶的 "+c+"（一起加進 ai_docs/bundled.txt，或改寫成不寫路徑的說法，例：說明卡 packages 的 PKG-30）")
				}
			}
		}
	}
	check := func(where, text string) {
		for _, m := range aiDocsPath.FindAllString(text, -1) {
			problems = append(problems, where+"：秘書會讀到的文字寫了 repo 的路徑 "+m+"（下載版沒有 repo；改成 evergenie docs show <文件>，那份要在 ai_docs/bundled.txt）")
		}
		for _, m := range docsShow.FindAllStringSubmatch(text, -1) {
			if strings.HasPrefix(m[1], "<") {
				continue
			}
			if _, ok := platform.FindProgramDoc(docs, m[1]); !ok {
				problems = append(problems, where+"：evergenie docs show "+m[1]+" 不在 ai_docs/bundled.txt（下載版讀不到）")
			}
		}
		for _, loc := range docsDirRef.FindAllStringSubmatchIndex(text, -1) {
			start, ref := loc[2], text[loc[2]:loc[3]]
			if strings.HasSuffix(text[:start], "docs show ") || strings.HasSuffix(text[:start], platform.ProgramDocsDir+"/") || listed[ref] {
				continue // docs show 與 ai_docs/… 上面報過
			}
			problems = append(problems, where+"：秘書會讀到的文字提到沒帶的文件 "+ref+"（下載版找不到；帶的寫 evergenie docs show <文件>，沒帶的改寫成不寫路徑的說法，例：schema task、說明卡 packages）")
		}
	}
	for _, f := range secretaryTexts(root) {
		check(f.where, f.text)
	}
	sort.Strings(problems)
	return problems
}

var (
	// aiDocsPath：寫死的 repo 文件路徑（ai_docs/ 後面接檔名或資料夾）。
	aiDocsPath = regexp.MustCompile(`ai_docs/[A-Za-z0-9_./-]+`)
	// docsShow：`evergenie docs show <文件>` 的文件名（到空白、引號、反引號、括號或中文標點為止）。
	docsShow = regexp.MustCompile("evergenie docs show ([^\\s`'\"()（）「」，。、；：]+)")
	// mdLink：Markdown 連結的目標；mdName：前後不是路徑字元的 *.md（`agents/<id>/agent.md` 這種有佔位的整個不算）。
	mdLink = regexp.MustCompile(`\]\(([^)\s#]+\.md)(?:#[^)]*)?\)`)
	mdName = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./<>*-])((?:\.\.?/)*[A-Za-z0-9_][A-Za-z0-9_./-]*\.md)\b`)
	// docsDirRef：ai_docs 底下四個資料夾的 .md（秘書會讀到的文字裡不帶 ai_docs/ 也算指 repo 的文件）。
	docsDirRef = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./<>*-])((?:conventions|schemas|modules|runbooks)/[A-Za-z0-9_-]+\.md)\b`)
)

// docsRootRef：帶的文件 doc 裡寫的 ref 明白指到 ai_docs 的文件時回相對 ai_docs 的路徑，否則空字串。明白指到＝寫成 ai_docs/…，
// 或是（./、../ 開頭的照 doc 的資料夾解開之後）conventions|schemas|modules|runbooks/ 開頭；只寫檔名的不算（照檔在不在判斷）。
func docsRootRef(doc, ref string) string {
	if rest, ok := strings.CutPrefix(ref, platform.ProgramDocsDir+"/"); ok {
		return path.Clean(rest)
	}
	c := path.Clean(ref)
	if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
		c = path.Clean(path.Join(path.Dir(doc), ref))
	}
	for _, dir := range []string{"conventions/", "schemas/", "modules/", "runbooks/"} {
		if strings.HasPrefix(c, dir) {
			return c
		}
	}
	return ""
}

// docRefs 回文件裡提到的 .md：連結目標與寫出來的檔名（原樣，還沒解析）。
func docRefs(text string) []string {
	var out []string
	for _, m := range mdLink.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	for _, m := range mdName.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

// refCandidates：doc（相對 ai_docs）裡寫的 ref 可能指到 ai_docs 裡的哪幾個檔——相對 doc 的資料夾、相對 ai_docs；
// 寫成 ai_docs/… 的只算相對 ai_docs。跑出 ai_docs 的不算。
func refCandidates(doc, ref string) []string {
	var raw []string
	if rest, ok := strings.CutPrefix(ref, platform.ProgramDocsDir+"/"); ok {
		raw = []string{rest}
	} else {
		raw = []string{path.Join(path.Dir(doc), ref), ref}
	}
	var out []string
	for _, r := range raw {
		c := path.Clean(r)
		if c == ".." || strings.HasPrefix(c, "../") || path.IsAbs(c) {
			continue
		}
		out = append(out, c)
	}
	return out
}

type secretaryText struct{ where, text string }

// isTestFile：Go 的 _test.go、前端的 .spec.／.test.（測試不是秘書讀的）。
func isTestFile(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".spec.") || strings.Contains(base, ".test.")
}

// secretaryTexts 收秘書會讀到的文字（見 bundledProblems 的第 3 點）。資料夾不在就略過（測試樣本只有 ai_docs）。
func secretaryTexts(root string) []secretaryText {
	var out []secretaryText
	add := func(path, text string) {
		rel, _ := filepath.Rel(root, path)
		out = append(out, secretaryText{filepath.ToSlash(rel), text})
	}
	walk := func(dir string, fn func(path string)) {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				switch name := d.Name(); {
				case name == "testdata" || name == "node_modules" || name == "dist" || (strings.HasPrefix(name, ".") && path != dir):
					return filepath.SkipDir
				case path == filepath.Join(root, "internal", "tools"):
					return filepath.SkipDir
				}
				return nil
			}
			fn(path)
			return nil
		})
	}
	read := func(path string) string {
		data, _ := os.ReadFile(path)
		return string(data)
	}
	goStrings := func(path string) {
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return
		}
		var lits []string
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil {
					lits = append(lits, s)
				}
			}
			return true
		})
		add(path, strings.Join(lits, "\n"))
	}
	// 官方套件：開發用的（package.toml 寫 dev_only = true）不算，那只裝在開發 EverGenie 的家。
	devOnly := regexp.MustCompile(`(?m)^dev_only\s*=\s*true\b`)
	pkgs, _ := filepath.Glob(filepath.Join(root, "packages", "official", "*", "package.toml"))
	for _, manifest := range pkgs {
		if devOnly.MatchString(read(manifest)) {
			continue
		}
		walk(filepath.Dir(manifest), func(path string) {
			if isTestFile(path) {
				return
			}
			switch filepath.Ext(path) {
			case ".md", ".toml", ".txt", ".go", ".ts", ".tsx":
				add(path, read(path)) // 原始碼跟著發行檔走，秘書會照著寫：註解也算
			}
		})
	}
	// sdk：發行檔帶原始碼（packages PKG-66），技能叫秘書去讀：全文。
	walk(filepath.Join(root, "sdk"), func(path string) {
		if filepath.Ext(path) == ".go" && !isTestFile(path) {
			add(path, read(path))
		}
	})
	// 核心：Go 的字串、內嵌的 .md（共用規則、說話層、排程樣板）。internal/tools 是開發工具，不算。
	for _, dir := range []string{"cmd", "internal"} {
		walk(filepath.Join(root, dir), func(path string) {
			switch filepath.Ext(path) {
			case ".md":
				add(path, read(path))
			case ".go":
				goStrings(path)
			}
		})
	}
	// 看板：原始碼裡不是註解的行（夜班清單的樣板這類會寫進主人的資料、被秘書讀到）。
	webDirs, _ := filepath.Glob(filepath.Join(root, "web", "packages", "*", "src"))
	for _, dir := range append([]string{filepath.Join(root, "web", "apps", "board", "src")}, webDirs...) {
		walk(dir, func(path string) {
			ext := filepath.Ext(path)
			if (ext != ".ts" && ext != ".tsx") || isTestFile(path) {
				return
			}
			var keep []string
			for _, line := range strings.Split(read(path), "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "/*") || strings.HasPrefix(t, "*") {
					continue
				}
				keep = append(keep, line)
			}
			add(path, strings.Join(keep, "\n"))
		})
	}
	return out
}
