package pkg

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/platform"
)

// fakeProgram 建一個假的程式資料夾（bin/evergenie＋ai_docs/），讓 docs 指令以為自己從那裡跑；回程式資料夾。
// files 是相對 ai_docs/ 的檔與內容；清單 bundled.txt 也放在 files 裡（不給就沒有清單）。
func fakeProgram(t *testing.T, files map[string]string) string {
	t.Helper()
	program, _ := filepath.EvalSymlinks(t.TempDir())
	exe := filepath.Join(program, "bin", "evergenie")
	_ = os.MkdirAll(filepath.Dir(exe), 0o755)
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range files {
		p := filepath.Join(program, platform.ProgramDocsDir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := docsExecutable
	docsExecutable = func() (string, error) { return exe, nil }
	t.Cleanup(func() { docsExecutable = old })
	return program
}

// docs list（與不帶子指令的 docs）列出清單上每一份、一句話說明與文件資料夾；show 照完整路徑或只寫檔名印全文。
func TestDocsListAndShow(t *testing.T) {
	program := fakeProgram(t, map[string]string{
		"bundled.txt":                  "# 註解\nconventions/user-features.md  # 資料夾地圖\nschemas/package.md # 欄位\n",
		"conventions/user-features.md": "# 使用者自己的套件\n內容",
		"schemas/package.md":           "# Schema：package\n",
	})
	for _, args := range [][]string{{"docs"}, {"docs", "list"}} {
		code, out, errOut := run(t, nil, "", args...)
		if code != 0 || !strings.Contains(out, filepath.Join(program, "ai_docs")) || !strings.HasPrefix(out, "程式資料夾："+program+"（") || !regexp.MustCompile(`(?m)^  conventions/user-features\.md  資料夾地圖$`).MatchString(out) ||
			!regexp.MustCompile(`(?m)^  schemas/package\.md {12}欄位$`).MatchString(out) || strings.Contains(out, "註解") {
			t.Fatalf("%v → %d\n%s%s", args, code, out, errOut)
		}
	}
	for _, name := range []string{"conventions/user-features.md", "ai_docs/conventions/user-features.md", "user-features", "conventions/user-features"} {
		code, out, errOut := run(t, nil, "", "docs", "show", name)
		if code != 0 || out != "# 使用者自己的套件\n內容\n" {
			t.Fatalf("show %s → %d %q %s", name, code, out, errOut)
		}
	}
	code, out, _ := run(t, nil, "", "docs", "--help")
	if code != 0 || !strings.Contains(out, "docs show <文件>") {
		t.Fatalf("--help → %d %s", code, out)
	}
}

// 只給清單上的：不在清單上的（就算檔案在）、跑出資料夾的、沒給名字的都拒絕；清單上有但檔案不在、沒有清單都說白話。
func TestDocsRefusesWhatIsNotListed(t *testing.T) {
	fakeProgram(t, map[string]string{
		"bundled.txt":                  "conventions/user-features.md\nschemas/gone.md\n",
		"conventions/user-features.md": "x",
		"modules/packages.md":          "內部的說明卡",
	})
	for _, name := range []string{"modules/packages.md", "packages", "../bin/evergenie", "bundled.txt", "/etc/hosts"} {
		code, out, errOut := run(t, nil, "", "docs", "show", name)
		if code != 1 || out != "" || !strings.Contains(errOut, "沒有這份說明文件") || !strings.Contains(errOut, "evergenie docs list") {
			t.Fatalf("show %s → %d %q %q", name, code, out, errOut)
		}
	}
	if code, _, errOut := run(t, nil, "", "docs", "show", "gone"); code != 1 || !strings.Contains(errOut, "讀不到這個檔") {
		t.Fatalf("清單上有、檔案不在 → %d %s", code, errOut)
	}
	for _, args := range [][]string{{"docs", "show"}, {"docs", "show", "a", "b"}, {"docs", "list", "x"}} {
		if code, _, _ := run(t, nil, "", args...); code != 2 {
			t.Fatalf("%v 要是用法錯誤，得到 %d", args, code)
		}
	}
	fakeProgram(t, map[string]string{"conventions/user-features.md": "x"})
	if code, _, errOut := run(t, nil, "", "docs", "show", "user-features"); code != 1 || !strings.Contains(errOut, "讀不到跟著程式的說明文件清單") {
		t.Fatalf("沒有清單 → %d %s", code, errOut)
	}
	docsExecutable = func() (string, error) { return "", os.ErrNotExist }
	if code, _, errOut := run(t, nil, "", "docs"); code != 1 || !strings.Contains(errOut, "找不到程式資料夾") {
		t.Fatalf("找不到執行檔 → %d %s", code, errOut)
	}
}

// 原始碼版同一套找法：程式資料夾的 ai_docs 就是 repo 的 ai_docs，清單上每一份都讀得到、內容跟 repo 的一樣。
func TestDocsShowsEveryListedRepoDoc(t *testing.T) {
	repoDocs, err := filepath.Abs(filepath.Join("..", "..", "..", "..", "ai_docs"))
	if err != nil {
		t.Fatal(err)
	}
	program := fakeProgram(t, nil)
	if err := os.Symlink(repoDocs, filepath.Join(program, platform.ProgramDocsDir)); err != nil {
		t.Fatal(err)
	}
	docs, err := platform.ReadProgramDocs(filepath.Dir(repoDocs))
	if err != nil || len(docs) < 3 {
		t.Fatalf("repo 的清單：%v %v", docs, err)
	}
	for _, d := range docs {
		if d.About == "" {
			t.Errorf("%s 要寫一句話說明", d.Path)
		}
		want, err := os.ReadFile(filepath.Join(repoDocs, filepath.FromSlash(d.Path)))
		if err != nil {
			t.Fatal(err)
		}
		code, out, errOut := run(t, nil, "", "docs", "show", d.Path)
		if code != 0 || strings.TrimSuffix(out, "\n") != strings.TrimSuffix(string(want), "\n") {
			t.Fatalf("show %s → %d %s", d.Path, code, errOut)
		}
	}
}
