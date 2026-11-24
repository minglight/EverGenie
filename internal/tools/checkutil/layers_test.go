package checkutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 說明卡「種類」行只看開頭：底層模組、業務塊、工具、官方…套件；「不是套件」之類夾在後面的字不影響。
func TestCardKind(t *testing.T) {
	cases := map[string]string{
		"# x\n**種類：** 底層模組（`internal/base/`，不是套件）\n": KindBase,
		"**種類：** 業務塊\n":                      KindFeature,
		"**種類：** 工具函式庫（`internal/tools/`）\n": KindTool,
		"**種類：** 官方套件（附程式）\n":                KindPackage,
		"**種類：** 官方**選用**套件\n":               KindPackage,
		"**種類：** 核心模組\n":                     "",
		"沒有種類行":                              "",
	}
	for text, want := range cases {
		if got := CardKind(text); got != want {
			t.Errorf("%q: got %q want %q", text, got, want)
		}
	}
}

// 卡的「依賴」：底層不能列業務塊、業務塊不能列別的業務塊、套件只能列 sdk、工具不受限。
func TestCardDeps(t *testing.T) {
	kinds := map[string]string{"reminders": KindFeature, "vault": KindBase}
	cases := []struct{ kind, name, deps, bad string }{
		{KindBase, "session", "`vault`、`sdk`", ""},
		{KindBase, "session", "`feature/goals`", "底層模組不能依賴業務塊"},
		{KindBase, "session", "`reminders`", "底層模組不能依賴業務塊"},
		{KindFeature, "tasks", "`internal/base/vault`、`feature/tasks`", ""},
		{KindFeature, "tasks", "`reminders`", "業務塊不能依賴別的業務塊"},
		{KindTool, "migrate", "`reminders`、`vault`", ""},
		{KindPackage, "habits", "`sdk`", ""},
		{KindPackage, "habits", "`vault`", "套件只用 `sdk`"},
	}
	for _, c := range cases {
		got := strings.Join(CardDeps(c.kind, c.name, c.deps, kinds), "\n")
		if (c.bad == "") != (got == "") || !strings.Contains(got, c.bad) {
			t.Errorf("%s %s %s：%q（要 %q）", c.kind, c.name, c.deps, got, c.bad)
		}
	}
}

// 「底層模組」表：第一欄模組、第二欄可以 import 的底層；重複的列要擋，沒有表要擋。
// 業務塊表一塊一列：只認第一欄是單一個反引號名稱的列，只讀「業務塊」段；重複、沒有表都回錯。
func TestFeatureBlocks(t *testing.T) {
	write := func(t *testing.T, text string) string {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "ai_docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "ai_docs", "architecture.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	blocks, err := FeatureBlocks(write(t, "## 業務塊\n| 塊 | 子套件 | 做什麼 |\n|---|---|---|\n| `tasks` | `tasks/task` | x |\n| `chat` | — | y |\n## 下一段\n| `extra` | — | z |\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 2 || !blocks["tasks"] || !blocks["chat"] {
		t.Fatalf("%v", blocks)
	}
	if _, err := FeatureBlocks(write(t, "## 業務塊\n| `tasks` | a | x |\n| `tasks` | b | y |\n")); err == nil {
		t.Fatal("重複的列要擋")
	}
	if _, err := FeatureBlocks(write(t, "# 沒有表\n")); err == nil {
		t.Fatal("沒有表要擋")
	}
}

func TestBaseOrder(t *testing.T) {
	write := func(t *testing.T, text string) string {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "ai_docs"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "ai_docs", "architecture.md"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	order, err := BaseOrder(write(t, "## 底層模組\n| 模組 | 可以 import 的底層 | 做什麼 |\n|---|---|---|\n| `platform` | — | x |\n| `vault` | `platform` | y |\n## 下一段\n| `bus` | `vault` | z |\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || !order["vault"]["platform"] || len(order["platform"]) != 0 {
		t.Fatalf("%v", order)
	}
	if _, err := BaseOrder(write(t, "## 底層模組\n| `vault` | — | x |\n| `vault` | — | y |\n")); err == nil {
		t.Fatal("重複的列要擋")
	}
	if _, err := BaseOrder(write(t, "# 沒有表\n")); err == nil {
		t.Fatal("沒有表要擋")
	}
}
