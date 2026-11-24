package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pathRoot 把 testdata/pathrules/<案例> 複製到暫存資料夾，再補上真正的 architecture.md（check 要讀「底層模組」表）。
func pathRoot(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", "pathrules", name)
	root := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	arch, err := os.ReadFile("../../../ai_docs/architecture.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "ai_docs"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ai_docs", "architecture.md"), arch, 0644); err != nil {
		t.Fatal(err)
	}
	addBlockDirs(t, root)
	return root
}

// 合法的結構：業務塊可以 import 底層與自己的子套件；底層照表 import 別的底層；
// app 與業務塊可以有 Routes／Commands；底層的測試檔不算；tools 不受限。
func TestPathRulesAcceptLegalLayout(t *testing.T) {
	var out bytes.Buffer
	if code := check(pathRoot(t, "ok"), &out); code != 0 {
		t.Fatalf("%d: %s", code, &out)
	}
}

func TestPathRulesCatchViolations(t *testing.T) {
	cases := map[string]string{
		"base-imports-feature":    "base/platform -> feature/tasks 不允許（底層不能 import 業務塊",
		"feature-imports-feature": "feature/tasks -> feature/inbox 不允許",
		"base-routes-method":      "base/platform 不准實作 Routes()",
		"base-commands-func":      "base/platform 不准實作 Commands()",
		"loose-file":              "檔案要放在 internal/base/<名稱>/ 底下",
		"base-imports-base-up":    "base/vault -> base/session 不允許（architecture.md「底層模組」表沒有列",
		"base-imports-tools":      "base/platform -> tools/tsgen 不允許",
		"feature-imports-tools":   "feature/tasks -> tools/tsgen 不允許",
		"top-level-module":        "internal/ 底下只能有 base、feature、tools",
		// 中央資料夾 api、cli、web 與業務塊的舊路徑（internal/tasks…）都已刪：internal/ 底下 base、feature 以外的路徑一律不准 import。
		"base-imports-api":              "base/platform -> internal/api 不允許",
		"base-imports-cli":              "base/platform -> internal/cli 不允許",
		"base-imports-web":              "base/platform -> internal/web 不允許",
		"base-imports-legacy-feature":   "base/platform -> internal/tasks 不允許",
		"base-imports-legacy-scheduler": "base/platform -> internal/scheduler 不允許",
		"feature-imports-api":           "feature/tasks -> internal/api 不允許",
		"feature-imports-cli":           "feature/tasks -> internal/cli 不允許",
		"feature-imports-web":           "feature/tasks -> internal/web 不允許",
		"feature-imports-legacy-other":  "feature/tasks -> internal/reminders 不允許",
		"feature-imports-legacy-notify": "feature/tasks -> internal/notify 不允許",
		// 底層 import 業務塊，測試檔也算（要業務塊的行為就用假物件）。
		"base-session-imports-feature": "base/session -> feature/goals 不允許（底層不能 import 業務塊",
		"base-test-imports-feature":    "base/engine -> feature/trust 不允許（底層不能 import 業務塊",
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if code := check(pathRoot(t, name), &out); code != 1 || !strings.Contains(out.String(), want) {
				t.Fatalf("%d，要含 %q：%s", code, want, &out)
			}
		})
	}
}

// 每個違規只報它該報的：業務塊互相 import 不會同時被報成別的錯。
func TestPathRulesReportOnlyTheViolation(t *testing.T) {
	var out bytes.Buffer
	check(pathRoot(t, "feature-imports-feature"), &out)
	if n := strings.Count(out.String(), "\n"); n != 1 {
		t.Fatalf("要剛好一行：%s", &out)
	}
}
