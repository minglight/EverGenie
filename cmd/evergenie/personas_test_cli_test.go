package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/internal/base/engine/fake"
)

// TestPersonasTestCLI：真的 serve＋fixture 家＋假引擎。`personas test` 預設等跑完，印結果檔路徑；第二次多印「上一次」；
// --no-wait 只印路徑；results 列最近幾次；一次性對話不開 session。
// Guards: REQ-130
func TestPersonasTestCLI(t *testing.T) {
	dir := doctorHome(t)
	engineAPI, sock := startDoctorAPI(t, dir)
	engineAPI.Configure(fake.Behavior{Delivered: true})
	t.Setenv("EVERGENIE_HOME", dir)
	t.Setenv("EVERGENIE_AGENT", "")
	if err := os.MkdirAll(filepath.Join(dir, "personas", "_test"), 0o700); err != nil {
		t.Fatal(err)
	}
	scenarios := "## one｜第一則\n{name}，今天好嗎\n\n## two｜第二則\n晚餐吃什麼\n"
	if err := os.WriteFile(filepath.Join(dir, "personas", "_test", "scenarios.md"), []byte(scenarios), 0o600); err != nil {
		t.Fatal(err)
	}

	help := hgOK(t, sock, "personas", "--help")
	for _, sub := range []string{"test <角色>", "results <角色>", "--no-wait"} {
		if !strings.Contains(help, sub) {
			t.Errorf("personas --help 少了 %s", sub)
		}
	}
	if code, _, errOut := hg(t, sock, "", "personas", "test"); code != 2 {
		t.Fatalf("沒給角色是用法錯誤：%d %q", code, errOut)
	}
	if code, _, errOut := hg(t, sock, "", "personas", "test", "nobody"); code != 1 || !strings.Contains(errOut, "角色庫裡沒有 nobody") {
		t.Fatalf("角色不存在：%d %q", code, errOut)
	}

	// 第一次：等跑完，印結果路徑，沒有「上一次」。
	out := hgOK(t, sock, "personas", "test", "builder")
	first := strings.TrimSpace(strings.TrimPrefix(strings.SplitN(out, "\n", 2)[0], "結果："))
	if !strings.HasPrefix(out, "結果：") || strings.Contains(out, "上一次") || !strings.HasPrefix(first, filepath.Join(dir, "runtime", "persona-tests", "builder")) {
		t.Fatalf("第一次的輸出：%q", out)
	}
	data, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"status: done", "# Builder 說話測試", "## one｜第一則", "> Builder，今天好嗎", "[fake] Builder，今天好嗎", "## two｜第二則", "[fake] 晚餐吃什麼"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("結果檔少了 %q：\n%s", want, data)
		}
	}

	// 第二次：多印上一次。
	out = hgOK(t, sock, "personas", "test", "Builder", "--only", "two", "--style", "d")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "結果：") || lines[1] != "上一次："+first {
		t.Fatalf("第二次的輸出：%q", out)
	}
	second := strings.TrimPrefix(lines[0], "結果：")
	if data, _ = os.ReadFile(second); strings.Contains(string(data), "## one") || !strings.Contains(string(data), "等級 D") {
		t.Fatalf("--only／--style：\n%s", data)
	}

	// --no-wait：只印路徑。
	out = hgOK(t, sock, "personas", "test", "builder", "--no-wait", "--model", "haiku")
	if !strings.Contains(out, "結果：") || !strings.Contains(out, "上一次："+second) {
		t.Fatalf("--no-wait：%q", out)
	}
	if code, _, errOut := hg(t, sock, "", "personas", "test", "builder", "--style", "z"); code != 1 || !strings.Contains(errOut, "說話層只能是") {
		t.Fatalf("說話層不認得：%d %q", code, errOut)
	}

	// results：列最近幾次（新到舊）。
	res := hgOK(t, sock, "personas", "results", "builder")
	if rows := strings.Split(strings.TrimSpace(res), "\n"); len(rows) != 3 || !strings.Contains(rows[2], first) || !strings.Contains(rows[2], "完成") || !strings.Contains(rows[2], "座位 code") {
		t.Fatalf("results：\n%s", res)
	}
	if res = hgOK(t, sock, "personas", "results", "builder", "--limit", "1"); strings.Count(res, "\n") != 1 {
		t.Fatalf("--limit：\n%s", res)
	}
	if res = hgOK(t, sock, "personas", "results", "helper"); !strings.Contains(res, "還沒測過口吻") {
		t.Fatalf("沒測過：%q", res)
	}

	// 不留 session：引擎只收到 Ask。
	for _, c := range engineAPI.Calls() {
		if c.Method == "Start" || c.Method == "Deliver" {
			t.Errorf("測口吻不能開 session：%s", c.Method)
		}
	}
}
