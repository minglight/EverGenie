package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/minglight/evergenie/internal/tools/checkutil"
)

func check(root string, out io.Writer) int {
	bad := 0
	issue := func(rule int, format string, args ...any) {
		bad++
		fmt.Fprintf(out, "DOC-%d: %s\n", rule, fmt.Sprintf(format, args...))
	}
	read := func(rel string) string {
		data, err := os.ReadFile(filepath.Join(root, "ai_docs", filepath.FromSlash(rel)))
		if err != nil {
			issue(0, "%s: %v", rel, err)
		}
		return string(data)
	}
	// 設計決定 D-n 放在負責的規範或說明卡的「## 設計決定」段：每個編號只定義一次，
	// ai_docs 裡引用的編號都要找得到。
	defined := map[string]string{}
	refs := map[string]string{}
	defRe := regexp.MustCompile(`(?m)^- \*\*(D-[0-9]+)\*\*`)
	refRe := regexp.MustCompile(`\bD-[0-9]+\b`)
	err := filepath.WalkDir(filepath.Join(root, "ai_docs"), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(root, "ai_docs"), path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() || !strings.HasSuffix(rel, ".md") {
			return nil
		}
		text := read(rel)
		for _, m := range defRe.FindAllStringSubmatch(text, -1) {
			if first, ok := defined[m[1]]; ok {
				issue(1, "設計決定 %s 定義了兩次：%s、%s", m[1], first, rel)
				continue
			}
			defined[m[1]] = rel
		}
		for _, id := range refRe.FindAllString(text, -1) {
			if _, ok := refs[id]; !ok {
				refs[id] = rel
			}
		}
		return nil
	})
	if err != nil {
		issue(0, "%v", err)
	}
	var dangling []string
	for id, rel := range refs {
		if _, ok := defined[id]; !ok {
			dangling = append(dangling, id+"（"+rel+"）")
		}
	}
	sort.Strings(dangling)
	for _, d := range dangling {
		issue(1, "引用了不存在的設計決定 %s", d)
	}
	index := read("INDEX.md")
	links := regexp.MustCompile(`\]\(((?:modules|conventions|schemas)/[^)]+\.md)\)`).FindAllStringSubmatch(index, -1)
	for _, m := range links {
		clean := filepath.Clean(filepath.FromSlash(m[1]))
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			issue(2, "INDEX 路徑超出 ai_docs：%s", m[1])
			continue
		}
		info, err := os.Stat(filepath.Join(root, "ai_docs", clean))
		if err != nil || info.IsDir() {
			issue(2, "INDEX 連結不存在：%s", m[1])
		}
	}
	// INDEX 裡用反引號寫的卡路徑（不是連結）也要存在。
	for _, path := range regexp.MustCompile(`modules/[a-z-]+\.md`).FindAllString(index, -1) {
		if _, err := os.Stat(filepath.Join(root, "ai_docs", path)); err != nil {
			issue(6, "INDEX 卡不存在：%s", path)
		}
	}
	style := read("conventions/doc-style.md")
	start := strings.Index(style, "## 樣板 B")
	var required []string
	if start >= 0 {
		tail := style[start:]
		parts := strings.SplitN(tail, "```", 3)
		if len(parts) >= 3 {
			for _, line := range strings.Split(parts[1], "\n") {
				if strings.HasPrefix(line, "## ") {
					fields := strings.Fields(strings.TrimPrefix(line, "## "))
					if len(fields) > 0 {
						required = append(required, fields[0])
					}
				}
			}
		}
	}
	if len(required) == 0 {
		issue(3, "doc-style.md 缺少樣板 B 的段落")
	}
	// 說明卡的 名稱 → 種類，檢查「依賴」時用來認出沒寫路徑的名稱。
	newKinds := map[string]string{}
	if cards, err := filepath.Glob(filepath.Join(root, "ai_docs", "modules", "*.md")); err == nil {
		for _, path := range cards {
			if data, err := os.ReadFile(path); err == nil {
				if k := checkutil.CardKind(string(data)); k != "" {
					newKinds[strings.TrimSuffix(filepath.Base(path), ".md")] = k
				}
			}
		}
	}
	for _, kind := range []string{"modules", "schemas", "conventions"} {
		paths, err := filepath.Glob(filepath.Join(root, "ai_docs", kind, "*.md"))
		if err != nil {
			issue(0, "%v", err)
			continue
		}
		if len(paths) == 0 {
			issue(0, "%s 沒有文件", kind)
		}
		for _, path := range paths {
			name := strings.TrimSuffix(filepath.Base(path), ".md")
			text := read(kind + "/" + name + ".md")
			has := func(heading string) bool {
				for _, line := range strings.Split(text, "\n") {
					if line == "## "+heading {
						return true
					}
				}
				return false
			}
			switch kind {
			case "modules":
				for _, h := range required {
					if !has(h) {
						issue(3, "%s 缺少 ## %s", name, h)
					}
				}
				seenRule := map[string]bool{}
				for _, m := range regexp.MustCompile(`(?m)^- \*\*([A-Z]+-N?[0-9]+)\*\*`).FindAllStringSubmatch(text, -1) {
					if seenRule[m[1]] {
						issue(7, "%s: 規則編號 %s 定義了兩次（編號不重用，新規則用下一個沒用過的號碼）", name, m[1])
					}
					seenRule[m[1]] = true
				}
				for _, p := range cardImportProblems(root, name, text) {
					issue(6, "%s", p)
				}
				// 依賴照路徑規則查（architecture.md「依賴規則」）：種類行要寫得出是哪一種。
				kind := checkutil.CardKind(text)
				if kind == "" {
					issue(6, "%s 的「種類」行要以「底層模組」「業務塊」「工具」或「官方…套件」開頭", name)
					continue
				}
				for _, p := range checkutil.CardDeps(kind, name, checkutil.Section(text, "依賴"), newKinds) {
					issue(6, "%s", p)
				}
			case "schemas":
				if name == "README" {
					continue
				}
				for _, h := range []string{"路徑", "欄位", "連結", "保存規則", "從舊系統遷移"} {
					if !has(h) {
						issue(4, "%s 缺少 ## %s", name, h)
					}
				}
			case "conventions":
				if name != "doc-style" {
					for _, h := range []string{"MUST", "NEVER", "Handoff"} {
						if !has(h) {
							issue(5, "%s 缺少 ## %s", name, h)
						}
					}
				}
				if regexp.MustCompile(`modules/[a-z][a-z-]*\.md`).MatchString(text) {
					issue(7, "%s 不可引用個別模組卡", name)
				}
			}
		}
	}
	for _, p := range blockCardProblems(root) {
		issue(11, "%s", p)
	}
	for _, p := range bundledProblems(root) {
		issue(12, "%s", p)
	}
	fmt.Fprintf(out, "docs bad=%d\n", bad)
	if bad > 0 {
		return 1
	}
	return 0
}

const modulePath = "github.com/minglight/evergenie"

// blockCardProblems 檢查每個業務塊（internal/feature/<塊>）剛好一張說明卡（doc-style DOC-11，計畫 2026-11-02-module-merge
// 設計決定 11）：卡的標題寫 `internal/feature/<塊>`；塊裡的子套件不另開卡（寫進塊那張卡的分段）。沒有 internal/feature 就不查。
func blockCardProblems(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, "internal", checkutil.KindFeature))
	if err != nil {
		return nil
	}
	blocks := map[string][]string{}
	for _, e := range entries {
		if e.IsDir() {
			blocks[e.Name()] = nil
		}
	}
	cards, _ := filepath.Glob(filepath.Join(root, "ai_docs", "modules", "*.md"))
	var problems []string
	prefix := "internal/" + checkutil.KindFeature + "/"
	for _, path := range cards {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		dir, ok := strings.CutPrefix(cardDir(string(data)), prefix)
		if !ok {
			continue
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		block, sub, _ := strings.Cut(dir, "/")
		if sub != "" {
			problems = append(problems, name+": 業務塊 "+block+" 的子套件 "+dir+" 不另開卡，寫進塊的卡 modules/"+block+".md")
			continue
		}
		if _, known := blocks[block]; known {
			blocks[block] = append(blocks[block], name)
		}
	}
	for block, names := range blocks {
		switch len(names) {
		case 1:
			if names[0] != block {
				problems = append(problems, names[0]+": 業務塊 "+block+" 的說明卡要叫 modules/"+block+".md")
			}
		case 0:
			problems = append(problems, "業務塊 "+block+" 沒有說明卡（標題寫 `internal/feature/"+block+"` 的 modules/"+block+".md）")
		default:
			sort.Strings(names)
			problems = append(problems, "業務塊 "+block+" 有不只一張說明卡："+strings.Join(names, "、"))
		}
	}
	sort.Strings(problems)
	return problems
}

// cardDir 從說明卡標題「# 模組：名（`internal/…`）」取出套件資料夾；標題沒寫 internal 路徑（套件、測試樣本）回空字串。
func cardDir(text string) string {
	title, _, _ := strings.Cut(text, "\n")
	if m := regexp.MustCompile("`(internal/[a-z0-9_/-]+)`").FindStringSubmatch(title); m != nil {
		return m[1]
	}
	return ""
}

// moduleOf 把 internal 底下的路徑（不含 internal/）換成模組名：base／feature／tools 底下取第二段，其他取第一段；
// cmdline 是 app 的子套件。
func moduleOf(rel string) string {
	parts := strings.Split(rel, "/")
	if len(parts) > 1 && (parts[0] == checkutil.KindBase || parts[0] == checkutil.KindFeature || parts[0] == "tools") {
		return parts[1]
	}
	if parts[0] == "cmdline" {
		return "app"
	}
	return parts[0]
}

// depModule 把依賴段裡的一個 `記號` 換成模組名；不是 internal 底下的套件（sdk、cmd/evergenie、型別名）回空字串。
func depModule(root, token string) string {
	p := strings.TrimPrefix(token, "internal/")
	mod := moduleOf(p)
	if mod == "app" {
		return mod
	}
	for _, dir := range []string{mod, checkutil.KindBase + "/" + mod, checkutil.KindFeature + "/" + mod, "tools/" + mod} {
		if info, err := os.Stat(filepath.Join(root, "internal", filepath.FromSlash(dir))); err == nil && info.IsDir() {
			return mod
		}
	}
	return ""
}

// cardImportProblems 比對說明卡「依賴」段用反引號列的內部模組，與標題那個套件（含子套件）正式程式碼（不含 _test.go、testdata）
// 實際 import 的 EverGenie 內部模組：多列、漏列都回報。經 Deps 小 interface 注入、不 import 的寫白話，不加反引號。
func cardImportProblems(root, name, text string) []string {
	dir := cardDir(text)
	if dir == "" {
		return nil
	}
	abs := filepath.Join(root, filepath.FromSlash(dir))
	if info, err := os.Stat(abs); err != nil || !info.IsDir() {
		return []string{name + ": 標題寫的套件 " + dir + " 不存在"}
	}
	self := moduleOf(strings.TrimPrefix(dir, "internal/"))
	actual := map[string][]string{}
	err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			target := strings.Trim(imp.Path.Value, "\"")
			if !strings.HasPrefix(target, modulePath+"/internal/") {
				continue
			}
			rel := strings.TrimPrefix(target, modulePath+"/internal/")
			if mod := moduleOf(rel); mod != self && !slices.Contains(actual[mod], rel) {
				actual[mod] = append(actual[mod], rel)
			}
		}
		return nil
	})
	if err != nil {
		return []string{name + ": " + err.Error()}
	}
	listed := map[string]bool{}
	for _, m := range regexp.MustCompile("`([a-z/-]+)`").FindAllStringSubmatch(checkutil.Section(text, "依賴"), -1) {
		if mod := depModule(root, m[1]); mod != "" && mod != self {
			listed[mod] = true
		}
	}
	var problems []string
	for mod := range listed {
		if actual[mod] == nil {
			problems = append(problems, name+": 依賴段列了 `"+mod+"`，但 "+dir+" 的正式程式碼沒有 import 它（經 Deps 注入的寫白話，不加反引號）")
		}
	}
	for mod, paths := range actual {
		if !listed[mod] {
			sort.Strings(paths)
			problems = append(problems, name+": 依賴段漏了 `"+mod+"`（"+dir+" 的正式程式碼 import 了 internal/"+strings.Join(paths, "、internal/")+"）")
		}
	}
	sort.Strings(problems)
	return problems
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "用法：doccheck <專案根目錄>")
		os.Exit(2)
	}
	os.Exit(check(os.Args[1], os.Stdout))
}
