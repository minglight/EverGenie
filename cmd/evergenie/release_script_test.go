package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 發版工具 scripts/release.sh、版本腳本 scripts/version.sh 的檢查（P8 第二輪 S1）：語法正確，整套情境（scripts/release-test.sh，`make release-test`）
// 全過——暫存 bare origin＋clone 上真的跑 nightly／prepare／stable／status，測試步驟用 true／false 代替 make；沒有 --push 時 origin 不會多出任何 tag。
// Guards: REQ-278
func TestReleaseScript(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("沒有 git")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("沒有 python3")
	}
	for _, name := range []string{"release.sh", "release-test.sh", "version.sh"} {
		script, err := filepath.Abs(filepath.Join("..", "..", "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
			t.Fatalf("%s 語法不對：%v\n%s", name, err, out)
		}
	}
	script, _ := filepath.Abs("../../scripts/release-test.sh")
	cmd := exec.Command("bash", script)
	cmd.Env = append(os.Environ(), "TMPDIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "失敗 0") {
		t.Fatalf("release-test.sh 沒有全過：%v\n%s", err, out)
	}
}

// 用法錯誤、不認得的選項是結束碼 2；--dry-run 的 nightly 不建 state 資料夾、不 clone。
// Guards: REQ-278
func TestReleaseScriptUsage(t *testing.T) {
	script, err := filepath.Abs("../../scripts/release.sh")
	if err != nil {
		t.Fatal(err)
	}
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init 失敗：%v\n%s", err, out)
	}
	state := filepath.Join(t.TempDir(), "state")
	run := func(args ...string) (string, int) {
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		cmd.Env = append(os.Environ(), "EVERGENIE_RELEASE_REPO="+repo, "EVERGENIE_RELEASE_STATE="+state)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code
	}
	for _, args := range [][]string{{}, {"bogus"}, {"nightly", "--bogus"}} {
		if _, code := run(args...); code != 2 {
			t.Fatalf("%v 該是用法錯誤（2）：%d", args, code)
		}
	}
	out, code := run("nightly", "--dry-run")
	if code != 0 || !strings.Contains(out, "dry-run") {
		t.Fatalf("nightly --dry-run：%d\n%s", code, out)
	}
	if _, err := os.Stat(state); err == nil {
		t.Fatal("--dry-run 不能建 state 資料夾")
	}
}
