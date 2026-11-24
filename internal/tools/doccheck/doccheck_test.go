package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir("testdata/good", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("testdata/good", path)
		if err != nil {
			return err
		}
		dest := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func replace(t *testing.T, root, path, old, new string) {
	t.Helper()
	dest := filepath.Join(root, "ai_docs", path)
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), old) {
		t.Fatalf("fixture lacks %q", old)
	}
	if err := os.WriteFile(dest, []byte(strings.ReplaceAll(string(data), old, new)), 0644); err != nil {
		t.Fatal(err)
	}
}

// Guards: REQ-147
func TestDocumentRules(t *testing.T) {
	cases := []struct{ name, path, old, new, diagnostic string }{
		{"decision dangling", "conventions/testing.md", "- **D-1** — one", "see D-3", "DOC-1"},
		{"decision duplicate", "modules/platform.md", "- **D-2**", "- **D-1**", "DOC-1"},
		{"index missing", "INDEX.md", "modules/platform.md", "modules/absent.md", "DOC-2"},
		{"module heading", "modules/platform.md", "## NEVER", "## Missing", "DOC-3"},
		{"schema heading", "schemas/task.md", "## 欄位", "## Missing", "DOC-4"},
		{"convention heading", "conventions/testing.md", "## Handoff", "## Missing", "DOC-5"},
		{"dependency", "modules/platform.md", "## 依賴\nNone.", "## 依賴\n`feature/tasks`", "DOC-6"},
		{"convention direction", "conventions/testing.md", "None.", "[card](modules/platform.md)", "DOC-7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			var good bytes.Buffer
			if check(root, &good) != 0 {
				t.Fatalf("valid fixture: %s", &good)
			}
			replace(t, root, tc.path, tc.old, tc.new)
			var bad bytes.Buffer
			if check(root, &bad) != 1 || !strings.Contains(bad.String(), tc.diagnostic) {
				t.Fatalf("invalid fixture: %s", &bad)
			}
		})
	}
}

// Guards: REQ-147
func TestRepoPasses(t *testing.T) {
	var out bytes.Buffer
	if check("../../..", &out) != 0 {
		t.Fatal(&out)
	}
}

// Guards: REQ-147
func TestGenericModuleReferenceAllowed(t *testing.T) {
	root := fixture(t)
	replace(t, root, "conventions/testing.md", "None.", "`modules/<模組>.md`")
	var out bytes.Buffer
	if check(root, &out) != 0 {
		t.Fatal(&out)
	}
}

// Guards: REQ-147
func TestMissingPolicyFails(t *testing.T) {
	var out bytes.Buffer
	if check(t.TempDir(), &out) != 1 {
		t.Fatal("empty repo passed")
	}
}

// addCard 在 fixture 的 modules/ 底下新增一張說明卡：用 platform.md 當樣板，換掉「種類」行與「依賴」段。
func addCard(t *testing.T, root, name, kind, deps string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "ai_docs", "modules", "platform.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "# platform", "# "+name, 1)
	text = strings.Replace(text, "**種類：** 底層模組（`internal/base/`）", "**種類：** "+kind, 1)
	text = strings.Replace(text, "## 依賴\nNone.", "## 依賴\n"+deps, 1)
	text = strings.Replace(text, "- **D-2** — two (D-1)", "None.", 1) // 設計決定編號只能定義一次
	if err := os.WriteFile(filepath.Join(root, "ai_docs", "modules", name+".md"), []byte(text), 0644); err != nil {
		t.Fatal(err)
	}
}

// 說明卡的「依賴」照路徑規則：底層不列業務塊、業務塊不列別的業務塊、套件只列 sdk、工具不受限。
func TestCardDependencyRules(t *testing.T) {
	cases := []struct {
		name, kind, deps string
		extra            map[string][2]string // 另外要有的卡：名稱 → {種類, 依賴}
		bad              string               // 空字串＝要通過
	}{
		{name: "base-plain", kind: "底層模組（`internal/base/`）", deps: "None."},
		{name: "feature-plain", kind: "業務塊（`internal/feature/`）", deps: "None."},
		{name: "base-not-package", kind: "底層模組（`internal/base/`，不是套件）", deps: "標準庫與 `sdk`。"},
		{name: "base-lists-base", kind: "底層模組", deps: "`vault`、`base/platform`、`timer`、`sdk`", extra: map[string][2]string{"timer": {"底層模組", "None."}}},
		{name: "feature-lists-base", kind: "業務塊", deps: "`app`、`internal/base/push`", extra: map[string][2]string{"app": {"底層模組", "None."}}},
		{name: "feature-lists-itself", kind: "業務塊", deps: "`feature/feature-lists-itself`"},
		{name: "tool-lists-feature", kind: "工具函式庫（`internal/tools/`）", deps: "`reminders`、`vault`", extra: map[string][2]string{"reminders": {"業務塊", "None."}}},
		{name: "package-lists-sdk", kind: "官方套件（附程式）", deps: "只用 `sdk`。"},
		{name: "base-lists-feature-path", kind: "底層模組", deps: "`feature/tasks`", bad: "底層模組不能依賴業務塊"},
		{name: "base-lists-feature-card", kind: "底層模組", deps: "`reminders`", extra: map[string][2]string{"reminders": {"業務塊", "None."}}, bad: "底層模組不能依賴業務塊"},
		{name: "feature-lists-feature-path", kind: "業務塊", deps: "`internal/feature/reminders`", bad: "業務塊不能依賴別的業務塊"},
		{name: "feature-lists-feature-card", kind: "業務塊", deps: "`reminders`", extra: map[string][2]string{"reminders": {"業務塊", "None."}}, bad: "業務塊不能依賴別的業務塊"},
		{name: "package-lists-internal", kind: "官方套件", deps: "`sdk`、`vault`", bad: "套件只用 `sdk`"},
		{name: "unknown-kind", kind: "核心模組（不是套件）", deps: "None.", bad: "「種類」行要以「底層模組」「業務塊」「工具」或「官方…套件」開頭"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			addCard(t, root, tc.name, tc.kind, tc.deps)
			for n, kd := range tc.extra {
				addCard(t, root, n, kd[0], kd[1])
			}
			var out bytes.Buffer
			code := check(root, &out)
			if tc.bad == "" {
				if code != 0 {
					t.Fatalf("要通過：%d %s", code, &out)
				}
				return
			}
			if code != 1 || !strings.Contains(out.String(), "DOC-6") || !strings.Contains(out.String(), tc.bad) {
				t.Fatalf("要擋 %q：%d %s", tc.bad, code, &out)
			}
		})
	}
}

// importRoot 是 fixture 加上一個真的 Go 套件與它的說明卡：卡的標題寫 internal 路徑才會被拿來比對 import。
func importRoot(t *testing.T, deps string) string {
	t.Helper()
	root := fixture(t)
	addCard(t, root, "widget", "業務塊（`internal/feature/widget`）", deps)
	replace(t, root, "modules/widget.md", "# widget", "# 模組：widget（`internal/feature/widget`）")
	addCard(t, root, "gadget", "業務塊（`internal/feature/gadget`）", "None.") // 每個業務塊剛好一張卡（DOC-11）
	replace(t, root, "modules/gadget.md", "# gadget", "# 模組：gadget（`internal/feature/gadget`）")
	files := map[string]string{
		"internal/base/vault/vault.go":           "package vault\n",
		"internal/base/app/cmdline/cmdline.go":   "package cmdline\n",
		"internal/feature/gadget/gadget.go":      "package gadget\n",
		"internal/feature/widget/widget.go":      "package widget\nimport (\n\t_ \"github.com/minglight/evergenie/internal/base/vault\"\n\t_ \"github.com/minglight/evergenie/sdk\"\n)\n",
		"internal/feature/widget/sub/sub.go":     "package sub\nimport _ \"github.com/minglight/evergenie/internal/base/app/cmdline\"\n",
		"internal/feature/widget/widget_test.go": "package widget\nimport _ \"github.com/minglight/evergenie/internal/feature/gadget\"\n",
		"internal/feature/widget/testdata/x.go":  "package x\nimport _ \"github.com/minglight/evergenie/internal/feature/gadget\"\n",
	}
	for path, src := range files {
		dest := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(src), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// 「依賴」段的反引號模組要跟套件（含子套件）正式程式碼的 import 一致：多列、漏列都擋；
// 測試檔、testdata、sdk、經 Deps 注入寫白話的都不算；cmdline 算 app。
func TestCardDependenciesMatchImports(t *testing.T) {
	cases := []struct{ name, deps, bad string }{
		{"match", "`vault`、`app`／`cmdline`、`sdk`。名冊 gadget 經 `Deps` 注入，不 import。", ""},
		{"internal-path", "`internal/base/vault`、`internal/base/app`", ""},
		{"missing", "`vault`", "widget: 依賴段漏了 `app`"},
		{"extra", "`vault`、`app`、`gadget`", "widget: 依賴段列了 `gadget`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := importRoot(t, tc.deps)
			var out bytes.Buffer
			code := check(root, &out)
			if tc.bad == "" {
				if code != 0 {
					t.Fatalf("要通過：%s", &out)
				}
				return
			}
			if code != 1 || !strings.Contains(out.String(), tc.bad) {
				t.Fatalf("要擋 %q：%d %s", tc.bad, code, &out)
			}
		})
	}
}

// 標題寫的套件不存在也擋（卡跟程式脫鉤）。
func TestCardPackageMustExist(t *testing.T) {
	root := fixture(t)
	addCard(t, root, "ghost", "業務塊", "None.")
	replace(t, root, "modules/ghost.md", "# ghost", "# 模組：ghost（`internal/feature/ghost`）")
	var out bytes.Buffer
	if code := check(root, &out); code != 1 || !strings.Contains(out.String(), "ghost: 標題寫的套件 internal/feature/ghost 不存在") {
		t.Fatalf("%d: %s", code, &out)
	}
}

// 每個業務塊（internal/feature/<塊>）剛好一張說明卡：沒有卡、兩張卡、替塊裡的子套件另開卡都擋（DOC-11，卡不會再長回一個業務一張）。
func TestEachFeatureBlockHasExactlyOneCard(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T, root string)
		bad   string // 空字串＝要通過
	}{
		{"one card per block", func(*testing.T, string) {}, ""},
		{"block without card", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "ai_docs", "modules", "gadget.md")); err != nil {
				t.Fatal(err)
			}
		}, "DOC-11: 業務塊 gadget 沒有說明卡"},
		{"two cards for one block", func(t *testing.T, root string) {
			addCard(t, root, "gadget2", "業務塊（`internal/feature/gadget`）", "None.")
			replace(t, root, "modules/gadget2.md", "# gadget2", "# 模組：gadget2（`internal/feature/gadget`）")
		}, "DOC-11: 業務塊 gadget 有不只一張說明卡：gadget、gadget2"},
		{"card file not named after block", func(t *testing.T, root string) {
			dir := filepath.Join(root, "ai_docs", "modules")
			if err := os.Rename(filepath.Join(dir, "gadget.md"), filepath.Join(dir, "todo.md")); err != nil {
				t.Fatal(err)
			}
		}, "DOC-11: todo: 業務塊 gadget 的說明卡要叫 modules/gadget.md"},
		{"sub-package card", func(t *testing.T, root string) {
			addCard(t, root, "sub", "業務塊（`internal/feature/widget/sub`）", "`app`")
			replace(t, root, "modules/sub.md", "# sub", "# 模組：widget/sub（`internal/feature/widget/sub`）")
		}, "DOC-11: sub: 業務塊 widget 的子套件 widget/sub 不另開卡"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := importRoot(t, "`vault`、`app`／`cmdline`、`sdk`。")
			tc.setup(t, root)
			var out bytes.Buffer
			code := check(root, &out)
			if tc.bad == "" {
				if code != 0 {
					t.Fatalf("要通過：%s", &out)
				}
				return
			}
			if code != 1 || !strings.Contains(out.String(), tc.bad) {
				t.Fatalf("要擋 %q：%d %s", tc.bad, code, &out)
			}
		})
	}
}

// 同一張卡裡同一個規則編號定義兩次要擋（DOC-7：編號不重用）。
func TestDuplicateRuleIDRejected(t *testing.T) {
	root := fixture(t)
	replace(t, root, "modules/platform.md", "## MUST\nNone.", "## MUST\n- **PLT-1** — a\n- **PLT-1** — b")
	var out bytes.Buffer
	if code := check(root, &out); code != 1 || !strings.Contains(out.String(), "platform: 規則編號 PLT-1 定義了兩次") {
		t.Fatalf("%d: %s", code, &out)
	}
}

// INDEX 裡用反引號寫、不是連結的卡路徑也要存在（卡改名或刪掉時 INDEX 的文字跟著改）。
func TestIndexCodeFormattedCardMustExist(t *testing.T) {
	root := fixture(t)
	replace(t, root, "INDEX.md", "[task](schemas/task.md)", "[task](schemas/task.md)\n舊的在 `modules/absent.md`")
	var out bytes.Buffer
	if code := check(root, &out); code != 1 || !strings.Contains(out.String(), "DOC-6: INDEX 卡不存在：modules/absent.md") {
		t.Fatalf("%d: %s", code, &out)
	}
}
