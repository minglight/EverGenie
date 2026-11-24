package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 程式資料夾：經 current、經另一個連結、直接叫，都解開到 versions/<版本>；原始碼版是 repo；空的、不存在的回空字串。
func TestProgramDir(t *testing.T) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	ver := filepath.Join(root, "app", "versions", "1.0.0")
	if err := os.MkdirAll(filepath.Join(ver, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(ver, "bin", "evergenie")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("versions", "1.0.0"), filepath.Join(root, "app", "current")); err != nil {
		t.Fatal(err)
	}
	viaCurrent := filepath.Join(root, "app", "current", "bin", "evergenie")
	link := filepath.Join(root, "localbin", "evergenie")
	_ = os.MkdirAll(filepath.Dir(link), 0o755)
	if err := os.Symlink(viaCurrent, link); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{exe, viaCurrent, link} {
		if got := ProgramDir(p); got != ver {
			t.Fatalf("ProgramDir(%s) = %q，要 %q", p, got, ver)
		}
	}
	repo := filepath.Join(root, "repo")
	_ = os.MkdirAll(filepath.Join(repo, "bin"), 0o755)
	if err := os.WriteFile(filepath.Join(repo, "bin", "evergenie"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := ProgramDir(filepath.Join(repo, "bin", "evergenie")); got != repo {
		t.Fatalf("原始碼版：%q", got)
	}
	if ProgramDir("") != "" || ProgramDir(filepath.Join(root, "none", "bin", "x")) != "" {
		t.Fatal("空的或不存在的要回空字串")
	}
}

// 清單：註解與空行略過、說明是 # 後面的字；壞的路徑（絕對、..、不乾淨、不是 .md、重複、有空白）與空清單回錯並說第幾行。
func TestParseProgramDocs(t *testing.T) {
	docs, err := ParseProgramDocs("# 標題\n\nconventions/a.md   # 講 a\nschemas/b.md\n")
	if err != nil || len(docs) != 2 || docs[0] != (ProgramDoc{"conventions/a.md", "講 a"}) || docs[1] != (ProgramDoc{"schemas/b.md", ""}) {
		t.Fatalf("%+v %v", docs, err)
	}
	for text, want := range map[string]string{
		"/abs.md":                "第 1 行",
		"a.md\n../x.md":          "第 2 行",
		"a/./b.md":               "第 1 行",
		"a//b.md":                "第 1 行",
		"a.txt":                  ".md",
		"a.md\na.md":             "兩次",
		"a b.md":                 "空白",
		"a\\b.md":                "反斜線",
		"# 只有註解\n":               "空的",
		"conventions/../../x.md": "第 1 行",
	} {
		if _, err := ParseProgramDocs(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q：%v（要有「%s」）", text, err, want)
		}
	}
}

// 讀清單：<程式資料夾>/ai_docs/bundled.txt；沒有就回錯。
func TestReadProgramDocs(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadProgramDocs(dir); err == nil {
		t.Fatal("沒有清單要回錯")
	}
	_ = os.MkdirAll(filepath.Join(dir, ProgramDocsDir), 0o755)
	if err := os.WriteFile(filepath.Join(dir, ProgramDocsDir, ProgramDocsList), []byte("schemas/package.md # 欄位\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if docs, err := ReadProgramDocs(dir); err != nil || len(docs) != 1 || docs[0].About != "欄位" {
		t.Fatalf("%+v %v", docs, err)
	}
}

// 找一份：完整路徑、前面多 ai_docs/、少 .md、只寫檔名（只有一份叫這個名字時）都找得到；同名兩份、不在清單上的找不到。
func TestFindProgramDoc(t *testing.T) {
	docs := []ProgramDoc{{Path: "conventions/user-features.md"}, {Path: "schemas/README.md"}, {Path: "conventions/README.md"}}
	for _, name := range []string{"conventions/user-features.md", "ai_docs/conventions/user-features.md", "conventions/user-features", "user-features", "user-features.md", " user-features "} {
		if d, ok := FindProgramDoc(docs, name); !ok || d.Path != "conventions/user-features.md" {
			t.Errorf("%q 要找到 user-features：%+v %v", name, d, ok)
		}
	}
	if d, ok := FindProgramDoc(docs, "schemas/README"); !ok || d.Path != "schemas/README.md" {
		t.Errorf("schemas/README：%+v %v", d, ok)
	}
	for _, name := range []string{"README", "", "terms", "../conventions/user-features.md", "ai_docs/", "modules/packages.md"} {
		if d, ok := FindProgramDoc(docs, name); ok {
			t.Errorf("%q 不該找到：%+v", name, d)
		}
	}
}
