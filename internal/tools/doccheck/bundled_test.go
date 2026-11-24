package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// DOC-12：清單讀得懂、清單上的檔都在；帶的文件不提到沒帶的；秘書會讀到的文字不寫 ai_docs/…，evergenie docs show 的那份要在清單上。
// 開發用的（dev_only 套件、程式碼註解、測試檔、internal/tools）不算。
func TestBundledDocs(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string // 相對 fixture 根目錄
		bad   string            // 空字串＝要通過
	}{
		{name: "fixture"},
		{name: "list missing", files: map[string]string{"ai_docs/bundled.txt": "# 空的\n"}, bad: "DOC-12: ai_docs/bundled.txt：清單是空的"},
		{name: "list bad path", files: map[string]string{"ai_docs/bundled.txt": "../x.md\n"}, bad: "DOC-12: ai_docs/bundled.txt：第 1 行"},
		{name: "listed file absent", files: map[string]string{"ai_docs/bundled.txt": "conventions/testing.md\nconventions/absent.md\n"}, bad: "DOC-12: 清單上的 conventions/absent.md 不在 ai_docs/"},
		{name: "bundled links unbundled", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("見 [task](../schemas/task.md)")}, bad: "DOC-12: 帶的文件 conventions/testing.md 提到沒帶的 schemas/task.md"},
		{name: "bundled names unbundled", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("見 `schemas/task.md`")}, bad: "提到沒帶的 schemas/task.md"},
		{name: "bundled names unbundled with prefix", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("見 ai_docs/schemas/task.md")}, bad: "提到沒帶的 schemas/task.md"},
		{name: "bundled names bundled", files: map[string]string{
			"ai_docs/bundled.txt":            "conventions/testing.md\nschemas/task.md\n",
			"ai_docs/conventions/testing.md": testingWith("見 [task](../schemas/task.md)、`schemas/task.md`"),
		}},
		{name: "bundled names missing file", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("見 `ai_docs/modules/gone.md`、`schemas/gone.md`")}, bad: "提到沒帶的 modules/gone.md"},
		{name: "bundled names missing file without prefix", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("見 `schemas/gone.md`")}, bad: "提到沒帶的 schemas/gone.md"},
		{name: "bundled names relative text", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("見 `../schemas/task.md`")}, bad: "提到沒帶的 schemas/task.md"},
		{name: "placeholder and unknown names", files: map[string]string{"ai_docs/conventions/testing.md": testingWith("`agents/<id>/agent.md`、`SKILL.md`、`modules/<模組>.md`、`task.md`")}},
		{name: "skill writes repo path", files: map[string]string{
			"packages/official/demo/package.toml":      "id = \"demo\"\n",
			"packages/official/demo/skills/x/SKILL.md": "讀 `ai_docs/conventions/testing.md`",
		}, bad: "DOC-12: packages/official/demo/skills/x/SKILL.md：秘書會讀到的文字寫了 repo 的路徑 ai_docs/conventions/testing.md"},
		{name: "skill uses docs show", files: map[string]string{
			"packages/official/demo/package.toml":      "id = \"demo\"\n",
			"packages/official/demo/skills/x/SKILL.md": "讀 `evergenie docs show conventions/testing.md`、`evergenie docs show testing`、`evergenie docs show <文件>`",
		}},
		{name: "skill shows unlisted doc", files: map[string]string{
			"packages/official/demo/package.toml":      "id = \"demo\"\n",
			"packages/official/demo/skills/x/SKILL.md": "讀 `evergenie docs show schemas/task.md`",
		}, bad: "DOC-12: packages/official/demo/skills/x/SKILL.md：evergenie docs show schemas/task.md 不在 ai_docs/bundled.txt"},
		{name: "dev only package", files: map[string]string{
			"packages/official/admin/package.toml": "id = \"admin\"\ndev_only = true\n",
			"packages/official/admin/pages/x.tsx":  "const s = \"照 ai_docs/runbooks/release.md\";\n",
			"packages/official/admin/README.md":    "ai_docs/runbooks/release.md",
		}},
		{name: "go string", files: map[string]string{"internal/feature/demo/x.go": "package demo\n\nconst s = \"見 ai_docs/schemas/task.md\"\n"}, bad: "DOC-12: internal/feature/demo/x.go：秘書會讀到的文字寫了 repo 的路徑 ai_docs/schemas/task.md"},
		{name: "go raw string docs show", files: map[string]string{"cmd/demo/x.go": "package main\n\nconst s = `用 evergenie docs show schemas/task.md 讀`\n"}, bad: "DOC-12: cmd/demo/x.go：evergenie docs show schemas/task.md 不在"},
		{name: "go comment, test, tools", files: map[string]string{
			"cmd/demo/x.go":            "package main\n\n// 見 ai_docs/schemas/task.md\nconst s = \"ok\"\n",
			"cmd/demo/x_test.go":       "package main\n\nconst t = \"ai_docs/schemas/task.md\"\n",
			"internal/tools/demo/x.go": "package demo\n\nconst s = \"ai_docs/schemas/task.md\"\n",
		}},
		{name: "sdk comment", files: map[string]string{"sdk/x.go": "package sdk\n\n// 欄位見 ai_docs/schemas/task.md\nconst s = 1\n"}, bad: "DOC-12: sdk/x.go：秘書會讀到的文字寫了 repo 的路徑 ai_docs/schemas/task.md"},
		{name: "sdk comment bare", files: map[string]string{"sdk/x.go": "package sdk\n\n// 欄位見 schemas/task.md\nconst s = 1\n"}, bad: "DOC-12: sdk/x.go：秘書會讀到的文字提到沒帶的文件 schemas/task.md"},
		{name: "sdk comment listed and test", files: map[string]string{
			"sdk/x.go":      "package sdk\n\n// 欄位見 evergenie docs show conventions/testing.md、conventions/testing.md、schema task\nconst s = 1\n",
			"sdk/x_test.go": "package sdk\n\n// ai_docs/schemas/task.md\n",
		}},
		{name: "package go comment", files: map[string]string{
			"packages/official/demo/package.toml":  "id = \"demo\"\n",
			"packages/official/demo/internal/x.go": "package x\n\n// 規則見 ai_docs/modules/demo.md\n",
		}, bad: "DOC-12: packages/official/demo/internal/x.go：秘書會讀到的文字寫了 repo 的路徑 ai_docs/modules/demo.md"},
		{name: "package page comment", files: map[string]string{
			"packages/official/demo/package.toml":    "id = \"demo\"\n",
			"packages/official/demo/pages/home.tsx":  "// 照 conventions/frontend.md\nexport default function Home() { return null; }\n",
			"packages/official/demo/pages/x.spec.ts": "// ai_docs/x.md\n",
		}, bad: "DOC-12: packages/official/demo/pages/home.tsx：秘書會讀到的文字提到沒帶的文件 conventions/frontend.md"},
		{name: "embedded md", files: map[string]string{"internal/base/agent/rules/shared.md": "先讀 ai_docs/conventions/testing.md"}, bad: "DOC-12: internal/base/agent/rules/shared.md：秘書會讀到的文字寫了 repo 的路徑"},
		{name: "web string", files: map[string]string{"web/apps/board/src/x.ts": "export const s = `做法：ai_docs/runbooks/x.md`;\n"}, bad: "DOC-12: web/apps/board/src/x.ts：秘書會讀到的文字寫了 repo 的路徑 ai_docs/runbooks/x.md"},
		{name: "web comment and spec", files: map[string]string{
			"web/apps/board/src/x.ts":      "// 見 ai_docs/conventions/frontend.md\n/* ai_docs/x.md\n * ai_docs/y.md */\nexport const s = 1;\n",
			"web/apps/board/src/x.spec.ts": "const s = \"ai_docs/x.md\";\n",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			for rel, body := range tc.files {
				writeFile(t, root, rel, body)
			}
			var out bytes.Buffer
			code := check(root, &out)
			if tc.bad == "" {
				if code != 0 {
					t.Fatalf("要通過：%s", &out)
				}
				return
			}
			if code != 1 || !strings.Contains(out.String(), tc.bad) {
				t.Fatalf("要有 %q：%d %s", tc.bad, code, &out)
			}
		})
	}
	root := fixture(t)
	if err := os.Remove(filepath.Join(root, "ai_docs", "bundled.txt")); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if check(root, &out) != 1 || !strings.Contains(out.String(), "DOC-12: 沒有跟著發行檔帶的文件清單 ai_docs/bundled.txt") {
		t.Fatalf("沒有清單要擋：%s", &out)
	}
}

// testingWith 是 fixture 的 conventions/testing.md，MUST 段換成 body。
func testingWith(body string) string {
	return "## MUST\n" + body + "\n## NEVER\nNone.\n## 設計決定\n- **D-1** — one\n## Handoff\nNone.\n"
}
