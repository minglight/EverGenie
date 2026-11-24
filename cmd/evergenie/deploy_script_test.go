package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 部署工具 scripts/deploy.sh 的輕量檢查（整套情境在 scripts/deploy-test.sh，`make deploy-test`）：語法正確、--dry-run 不 ssh 也不執行任何東西、
// mini 排在 office 前面、用法錯誤結束碼 2。ssh 換成「一呼叫就留記號」的假指令，證明 dry-run 沒有碰它。
// Guards: REQ-249, REQ-250
func TestDeployScriptDryRun(t *testing.T) {
	script, err := filepath.Abs("../../scripts/deploy.sh")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("deploy.sh 語法不對：%v\n%s", err, out)
	}
	dir := t.TempDir()
	mark := filepath.Join(dir, "ssh-called")
	fake := "#!/bin/sh\necho \"$@\" >> " + mark + "\nexit 255\n"
	if err := os.WriteFile(filepath.Join(dir, "ssh"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (string, int) {
		cmd := exec.Command("bash", append([]string{script}, args...)...)
		// mini 的 ssh 名稱給假的、不讀 repo 外面的部署設定檔：結果不看這台電腦有沒有放設定。
		cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "EVERGENIE_DEPLOY_MINI_HOST=my-mini", "EVERGENIE_DEPLOY_ENV=")
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code
	}

	out, code := run("--dry-run", "all")
	if code != 0 {
		t.Fatalf("dry-run all 該成功：%d\n%s", code, out)
	}
	for _, want := range []string{"git merge --ff-only origin/main", "make build", "evergenie service restart", "bin.prev", "evergenie doctor"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dry-run 該印出 %q：\n%s", want, out)
		}
	}
	if m, o := strings.Index(out, "== mini"), strings.Index(out, "== office"); m < 0 || o < 0 || m > o {
		t.Fatalf("mini 要在 office 前面：\n%s", out)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("dry-run 不能呼叫 ssh")
	}
	if out, code = run("--dry-run", "--rollback", "mini"); code != 0 || !strings.Contains(out, "bin.prev") {
		t.Fatalf("--rollback --dry-run：%d\n%s", code, out)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("--rollback --dry-run 不能呼叫 ssh")
	}
	for _, args := range [][]string{{}, {"staging"}, {"--bogus", "mini"}, {"--rollback", "all"}} {
		if _, code := run(args...); code != 2 {
			t.Fatalf("%v 該是用法錯誤（2）：%d", args, code)
		}
	}
}
