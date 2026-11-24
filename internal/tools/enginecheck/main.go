package main

import (
	_ "embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed vendor_terms.txt
var terms string

var (
	lowerUpper   = regexp.MustCompile(`([a-z0-9])([A-Z])`)
	acronymUpper = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
)

// splitIdentifiers turns claude_session_id, tmuxSocket and MCPServer into
// separate words, because \b treats underscores and camelCase as one word.
func splitIdentifiers(line string) string {
	line = strings.ReplaceAll(line, "_", " ")
	line = acronymUpper.ReplaceAllString(line, "$1 $2")
	return lowerUpper.ReplaceAllString(line, "$1 $2")
}

// mentionsAny is a cheap prefilter before the regexp: any regexp match (on the
// line or on splitIdentifiers of it) is also a case-insensitive substring of
// the original text, because terms have no spaces and splitting only inserts
// spaces or turns underscores into spaces. Under -race the regexp is ~30x
// slower, so skipping clean files and lines keeps make ci fast.
func mentionsAny(lower string, lowerTerms []string) bool {
	for _, term := range lowerTerms {
		if strings.Contains(lower, term) {
			return true
		}
	}
	return false
}

func check(root string, out io.Writer) int {
	var words, lowerTerms []string
	for _, term := range strings.Fields(terms) {
		words = append(words, regexp.QuoteMeta(term))
		lowerTerms = append(lowerTerms, strings.ToLower(term))
	}
	if len(words) == 0 {
		fmt.Fprintln(out, "詞表為空")
		return 1
	}
	pattern := regexp.MustCompile(`(?i)\b(` + strings.Join(words, "|") + `)\b`)
	bad := false
	// cmd/ 是核心的執行檔（主人看得到的訊息也在這裡）：一樣不出現平台專有名詞；它的測試檔會接上真的引擎跑整合測試，不掃。
	for _, folder := range []string{"internal", "sdk", "cmd"} {
		if _, err := os.Stat(filepath.Join(root, folder)); folder == "cmd" && os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, folder), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if d.IsDir() {
				if d.Name() == "testdata" || rel == "internal/base/engine/registry" || rel == "internal/tools/enginecheck" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if ext != ".go" || (folder == "cmd" && strings.HasSuffix(path, "_test.go")) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !mentionsAny(strings.ToLower(string(data)), lowerTerms) {
				return nil
			}
			for i, line := range strings.Split(string(data), "\n") {
				if !mentionsAny(strings.ToLower(line), lowerTerms) {
					continue
				}
				// Whole-line matches keep multi-word terms such as transcript_path; split matches catch identifiers.
				seen := map[string]bool{}
				for _, term := range append(pattern.FindAllString(line, -1), pattern.FindAllString(splitIdentifiers(line), -1)...) {
					if seen[strings.ToLower(term)] {
						continue
					}
					seen[strings.ToLower(term)] = true
					fmt.Fprintf(out, "%s:%d 出現平台專有名詞 %s（D-21）\n", rel, i+1, term)
					bad = true
				}
			}
			return nil
		})
		if err != nil {
			fmt.Fprintln(out, err)
			bad = true
		}
	}
	if bad {
		return 1
	}
	return 0
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "用法：enginecheck <專案根目錄>")
		os.Exit(2)
	}
	os.Exit(check(os.Args[1], os.Stdout))
}
