package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/tools/checkutil"
)

// fixture 是一個只有真正的 architecture.md 與一個 internal/<mod>/bad.go 的暫存 repo。
func fixture(t *testing.T, mod, source string) string {
	t.Helper()
	root := t.TempDir()
	data, err := os.ReadFile("../../../ai_docs/architecture.md")
	if err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{"ai_docs/architecture.md": string(data), "internal/" + mod + "/bad.go": source} {
		dest := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	addBlockDirs(t, root)
	return root
}

// addBlockDirs 在有 internal/feature 的樣本裡補上表上每一塊的空資料夾（表上登記的塊都要有資料夾，見 TestRegisteredBlockNeedsFolder）。
func addBlockDirs(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, "internal", "feature")); err != nil {
		return
	}
	blocks, err := checkutil.FeatureBlocks(root)
	if err != nil {
		t.Fatal(err)
	}
	for name := range blocks {
		if err := os.MkdirAll(filepath.Join(root, "internal", "feature", name), 0755); err != nil {
			t.Fatal(err)
		}
	}
}

// 「業務塊」表上登記的塊沒有資料夾要擋（塊合併或刪掉時忘了拿掉那一列）。
func TestRegisteredBlockNeedsFolder(t *testing.T) {
	root := fixture(t, "feature/tasks/task", "package task\n")
	if err := os.Remove(filepath.Join(root, "internal", "feature", "chat")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := check(root, &out); code != 1 || !strings.Contains(out.String(), "登記了業務塊 chat，但沒有 internal/feature/chat") {
		t.Fatalf("%d: %s", code, &out)
	}
}

// Guards: REQ-145
func TestRepoIsClean(t *testing.T) {
	var out bytes.Buffer
	if code := check("../../..", &out); code != 0 {
		t.Fatalf("%d: %s", code, &out)
	}
}

// Guards: REQ-145
// 故意違規的樣本：底層 vault import 業務塊 tasks。
func TestCatchesViolation(t *testing.T) {
	var out bytes.Buffer
	if code := check("testdata/violation", &out); code != 1 || !strings.Contains(out.String(), "base/vault -> feature/tasks") {
		t.Fatalf("%d: %s", code, &out)
	}
}

// Guards: REQ-145
// 業務塊之間不互相 import（以前是「同層 jobs -> notify」，S8 起是 tasks 塊 -> inbox 塊）。
func TestCatchesFeatureToFeature(t *testing.T) {
	root := fixture(t, "feature/tasks/job", "package job\nimport _ \"github.com/minglight/evergenie/internal/feature/inbox/notify\"\n")
	var out bytes.Buffer
	if code := check(root, &out); code != 1 || !strings.Contains(out.String(), "feature/tasks -> feature/inbox") {
		t.Fatalf("%d: %s", code, &out)
	}
}

// Guards: REQ-145
// 底層之間照「底層模組」表：往下可以、往上或表上沒列的不行；自己的子套件可以。
func TestBaseOrder(t *testing.T) {
	cases := map[string]struct {
		mod, imp string
		pass     bool
	}{
		"vault uses platform":       {"base/vault", "base/platform", true},
		"vault uses own subpackage": {"base/vault", "base/vault/codec", true},
		"session uses engine":       {"base/session", "base/engine", true},
		"platform uses vault":       {"base/platform", "base/vault", false},
		"vault uses session":        {"base/vault", "base/session", false},
		"engine uses session":       {"base/engine", "base/session", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pkg := c.mod[strings.LastIndex(c.mod, "/")+1:]
			root := fixture(t, c.mod, "package "+pkg+"\nimport _ \"github.com/minglight/evergenie/internal/"+c.imp+"\"\n")
			var out bytes.Buffer
			code := check(root, &out)
			if c.pass && code != 0 {
				t.Fatalf("要通過：%s", &out)
			}
			if !c.pass && (code != 1 || !strings.Contains(out.String(), c.mod+" -> base/") || !strings.Contains(out.String(), "底層模組」表沒有列")) {
				t.Fatalf("要擋：%d %s", code, &out)
			}
		})
	}
}

// Guards: REQ-145
// internal/ 底下只能有 base、feature、tools；底層模組與業務塊都要登記在 architecture.md 的表上（業務塊不會再長回一個業務一塊）。
func TestLayoutRules(t *testing.T) {
	cases := map[string]struct{ mod, want string }{
		"old top-level module": {"cli", "internal/ 底下只能有 base、feature、tools"},
		"unregistered base":    {"base/clock", "底層模組 clock 沒有登記"},
		"unregistered block":   {"feature/widgets/widget", "業務塊 widgets 沒有登記在 architecture.md 的「業務塊」表"},
		"old feature path":     {"feature/reminders", "業務塊 reminders 沒有登記"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			pkg := c.mod[strings.LastIndex(c.mod, "/")+1:]
			var out bytes.Buffer
			if code := check(fixture(t, c.mod, "package "+pkg+"\n"), &out); code != 1 || !strings.Contains(out.String(), c.want) {
				t.Fatalf("%d: %s", code, &out)
			}
		})
	}
}

// Guards: REQ-145
func TestMissingArchitectureFails(t *testing.T) {
	var out bytes.Buffer
	if check(t.TempDir(), &out) != 1 {
		t.Fatal("missing policy passed")
	}
}
