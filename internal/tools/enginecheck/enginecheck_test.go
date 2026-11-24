package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"internal", "sdk"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range files {
		dest := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// Guards: REQ-87, REQ-144
func TestCatchesVendorTerm(t *testing.T) {
	data, err := os.ReadFile("testdata/violation.go")
	if err != nil {
		t.Fatal(err)
	}
	root := fixture(t, map[string]string{"internal/example/bad.go": string(data)})
	var out bytes.Buffer
	if check(root, &out) != 1 || !strings.Contains(out.String(), "tmux") || !strings.Contains(out.String(), "claude") {
		t.Fatal(&out)
	}
}

// Guards: REQ-87, REQ-144
func TestRepoIsClean(t *testing.T) {
	var out bytes.Buffer
	if check("../../..", &out) != 0 {
		t.Fatal(&out)
	}
}

// Guards: REQ-87, REQ-144
func TestRegistryExempt(t *testing.T) {
	root := fixture(t, map[string]string{"internal/base/engine/registry/r.go": "// Claude", "internal/tools/enginecheck/x.go": "// tmux", "internal/example/testdata/x.go": "// MCP"})
	var out bytes.Buffer
	if check(root, &out) != 0 {
		t.Fatal(&out)
	}
}

// cmd/ 的程式（主人看得到的訊息）也掃；cmd/ 的測試檔不掃（整合測試接真的引擎）。
func TestCmdScannedButNotItsTests(t *testing.T) {
	root := fixture(t, map[string]string{"cmd/evergenie/host.go": `var detail = "沒檢查（tmux、Claude 程式）"` + "\n", "cmd/evergenie/host_test.go": "// claude\n"})
	var out bytes.Buffer
	if check(root, &out) != 1 || !strings.Contains(out.String(), "cmd/evergenie/host.go:1") || strings.Contains(out.String(), "host_test.go") {
		t.Fatal(&out)
	}
}

// Guards: REQ-87, REQ-144
func TestSDKAndWordBoundaries(t *testing.T) {
	root := fixture(t, map[string]string{"sdk/x.go": "// OPENAI\n// hooked tmuxlike\n", "internal/base/engine/registry_extra/x.go": "// Hooks"})
	var out bytes.Buffer
	if check(root, &out) != 1 || strings.Count(out.String(), "出現平台專有名詞") != 2 {
		t.Fatal(&out)
	}
}

// Guards: REQ-87, REQ-144
func TestCatchesVendorTermInIdentifiers(t *testing.T) {
	for _, line := range []string{
		`var id = "claude_session_id"`,
		`var tmuxSocket = 1`,
		`type ClaudeEngine struct{}`,
		`var MCPServer, mcpServer = 1, 2`,
		`func onPreToolUseHook() {}`,
		`var anthropicKey = ""`,
	} {
		t.Run(line, func(t *testing.T) {
			root := fixture(t, map[string]string{"internal/example/bad.go": "package example\n" + line + "\n"})
			var out bytes.Buffer
			if check(root, &out) != 1 {
				t.Fatal("identifier passed:", line)
			}
		})
	}
}

// Guards: REQ-87, REQ-144
func TestIdentifierSplitKeepsNeutralWords(t *testing.T) {
	root := fixture(t, map[string]string{"internal/example/ok.go": "package example\nvar webhookURL, engineSessionID, hookedUp, MCPXServer = 1, 2, 3, 4\n"})
	var out bytes.Buffer
	if check(root, &out) != 0 {
		t.Fatal(&out)
	}
}

// Guards: REQ-87, REQ-144
func TestSDKVendorTermCaught(t *testing.T) {
	root := fixture(t, map[string]string{"sdk/bad.go": "package sdk\ntype ClaudeSession struct{}\n"})
	var out bytes.Buffer
	if check(root, &out) != 1 || !strings.Contains(out.String(), "Claude") {
		t.Fatal(&out)
	}
}
