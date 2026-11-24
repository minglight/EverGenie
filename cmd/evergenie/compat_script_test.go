package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 相容檢查 scripts/compat.sh（`make compat`）與發版整套測試 scripts/release-e2e.sh（`make release-e2e`）的輕量檢查：
// 兩支腳本語法正確、--help 結束 0 並印出用法、不認得的選項結束 2。真正的整套（編譯、安裝、升級、退版）太慢，不隨 `make ci` 跑，發版前才跑。
// Guards: REQ-284, REQ-285
func TestCompatAndReleaseE2EScriptsUsage(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"compat.sh", "--only sdk|schema|fixture|prev"},
		{"release-e2e.sh", "EVERGENIE_E2E_KEEP"},
	} {
		script, err := filepath.Abs(filepath.Join("..", "..", "scripts", tc.name))
		if err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("%s 語法不對：%v\n%s", tc.name, err, out)
		}
		out, err := exec.Command("bash", script, "--help").CombinedOutput()
		if err != nil || !strings.Contains(string(out), tc.want) {
			t.Fatalf("%s --help 該結束 0 並印出用法（含 %q）：%v\n%s", tc.name, tc.want, err, out)
		}
		err = exec.Command("bash", script, "--bogus").Run()
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
			t.Fatalf("%s 不認得的選項該結束 2：%v", tc.name, err)
		}
	}
}

// compat.sh 的 sdk 項目：上一個正式版的 api.golden 每一行現在都要還在。暫存 git repo 上試：加一行過、刪一行抓到（列出少了哪行、結束 1）、
// 沒有正式版 tag 就跳過並說明；好幾個 tag 拿最大的；EVERGENIE_COMPAT_PREV 可以指定比對對象。
// Guards: REQ-284
func TestCompatScriptSDKAdditiveOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("沒有 git")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("沒有 python3")
	}
	src, err := filepath.Abs("../../scripts/compat.sh")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v：%v\n%s", args, err, out)
		}
	}
	golden := filepath.Join(repo, "sdk", "testdata", "api.golden")
	write := func(lines ...string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "compat.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(env ...string) (string, int) {
		t.Helper()
		cmd := exec.Command("bash", filepath.Join(repo, "scripts", "compat.sh"), "--only", "sdk")
		cmd.Env = append(os.Environ(), append([]string{"EVERGENIE_COMPAT_TMP=" + t.TempDir()}, env...)...)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}

	git("init", "-q", "-b", "main")
	write("A struct", "A.X string x", "A.Y int y")
	git("add", "-A")
	git("commit", "-q", "-m", "first")

	// 還沒有正式版：跳過並說明，結束 0。
	if out, code := run(); code != 0 || !strings.Contains(out, "還沒有正式版") || !strings.Contains(out, "跳過") {
		t.Fatalf("沒有正式版 tag 該跳過：%d\n%s", code, out)
	}

	git("tag", "-a", "v0.1.0", "-m", "v0.1.0")
	// 只加不刪：過。
	write("A struct", "A.X string x", "A.Y int y", "A.Z bool z")
	if out, code := run(); code != 0 || !strings.Contains(out, "新增 1 行") {
		t.Fatalf("只加不刪該過：%d\n%s", code, out)
	}
	// 刪一行：抓到、列出少了哪一行。
	write("A struct", "A.X string x")
	out, code := run()
	if code != 1 || !strings.Contains(out, "A.Y int y") || !strings.Contains(out, "相容檢查：有 1 項沒過") {
		t.Fatalf("少了一行該結束 1 並列出它：%d\n%s", code, out)
	}
	// 刻意刪的登記在 api.removed（tab 後面是原因）：不算少了；沒登記的照樣抓到。
	removed := filepath.Join(repo, "sdk", "testdata", "api.removed")
	if err := os.WriteFile(removed, []byte("# 註解\nA.Y int y\t# DEV-1 拿掉\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run(); code != 0 || !strings.Contains(out, "api.removed 1 行") {
		t.Fatalf("登記過的刪除該過：%d\n%s", code, out)
	}
	write("A struct")
	if out, code := run(); code != 1 || !strings.Contains(out, "A.X string x") || strings.Contains(out, "A.Y int y") {
		t.Fatalf("沒登記的刪除該抓到、登記過的不列：%d\n%s", code, out)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatal(err)
	}
	// 改了欄位的型別也算少了舊的那一行。
	write("A struct", "A.X int x", "A.Y int y")
	if out, code := run(); code != 1 || !strings.Contains(out, "A.X string x") {
		t.Fatalf("改型別該抓到：%d\n%s", code, out)
	}
	// 有好幾個正式版：拿最大的（v0.10.0 比 v0.9.0 大）比。
	write("A struct", "A.X string x", "A.Y int y")
	git("add", "-A")
	git("commit", "-q", "--allow-empty", "-m", "same")
	git("tag", "-a", "v0.9.0", "-m", "v0.9.0")
	git("tag", "-a", "v0.10.0", "-m", "v0.10.0")
	if out, code := run(); code != 0 || !strings.Contains(out, "上一個正式版：v0.10.0") {
		t.Fatalf("該拿最大的 v0.10.0 比：%d\n%s", code, out)
	}
	// EVERGENIE_COMPAT_PREV 指定比對對象。
	if out, code := run("EVERGENIE_COMPAT_PREV=v0.9.0"); code != 0 || !strings.Contains(out, "上一個正式版：v0.9.0") {
		t.Fatalf("EVERGENIE_COMPAT_PREV：%d\n%s", code, out)
	}
}
