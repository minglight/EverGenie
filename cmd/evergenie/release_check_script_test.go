package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// 部署前檢查 scripts/release-check.sh（D-53，`make release-check`）：在暫存 repo（本機 bare origin＋clone）裡跑，make 換成假的
// （指定的目標失敗，其他成功）。證明：照順序跑每一項、一項沒過也跑完其他項、摘要列出每項結果與沒過的 log 尾巴、結論說
// 「先不要部署」並結束碼 1；全過而且 HEAD＝origin/main、沒有沒提交的改動才說可以部署（結束碼 0）；有沒提交的改動、
// 或 origin/main 有 HEAD 沒有的 commit（部署會帶上沒測過的）就算全過也不給部署；--dry-run 不叫 make；不認得的參數結束碼 2；
// 秘書 session 的 EVERGENIE_HOME 不會傳給 make。
func TestReleaseCheckScript(t *testing.T) {
	src, err := filepath.Abs("../../scripts/release-check.sh")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", "-n", src).CombinedOutput(); err != nil {
		t.Fatalf("release-check.sh 語法不對：%v\n%s", err, out)
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("沒有 git")
	}
	dir := t.TempDir()
	gitEnv := append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	git := func(in string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir, cmd.Env = in, gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v：%v\n%s", args, err, out)
		}
	}
	origin, repo, other := filepath.Join(dir, "origin.git"), filepath.Join(dir, "repo"), filepath.Join(dir, "other")
	git(dir, "init", "-q", "--bare", "-b", "main", origin)
	git(dir, "clone", "-q", origin, repo)
	body, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "release-check.sh"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	git(repo, "add", "-A")
	git(repo, "commit", "-q", "-m", "one")
	git(repo, "push", "-q", "origin", "HEAD:main")
	git(repo, "fetch", "-q", "origin")

	calls := filepath.Join(dir, "calls.log")
	fake := "#!/bin/sh\necho \"$1 EVERGENIE_HOME=${EVERGENIE_HOME:-}\" >> " + calls + "\n" +
		"if [ \"$1\" = \"$FAKE_FAIL\" ]; then echo \"$1 的最後一行：壞掉了\"; exit 2; fi\necho ok\n"
	fakeMake := filepath.Join(dir, "fake-make")
	if err := os.WriteFile(fakeMake, []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	run := func(fail string, args ...string) (string, int) {
		_ = os.Remove(calls)
		cmd := exec.Command("bash", append([]string{filepath.Join(repo, "scripts", "release-check.sh")}, args...)...)
		cmd.Env = append(gitEnv,
			"EVERGENIE_RELEASE_CHECK_MAKE="+fakeMake,
			"EVERGENIE_RELEASE_CHECK_STEPS=web-ci ci e2e-full",
			"EVERGENIE_RELEASE_CHECK_LOGS="+filepath.Join(dir, "logs"),
			"EVERGENIE_HOME=/should/not/leak",
			"FAKE_FAIL="+fail,
		)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code
	}
	readCalls := func() string { b, _ := os.ReadFile(calls); return string(b) }

	out, code := run("ci")
	if code != 1 {
		t.Fatalf("ci 沒過時結束碼該是 1：%d\n%s", code, out)
	}
	if got, want := readCalls(), "web-ci EVERGENIE_HOME=\nci EVERGENIE_HOME=\ne2e-full EVERGENIE_HOME=\n"; got != want {
		t.Fatalf("該照順序跑完三項（一項沒過也跑下一項）、而且不帶 EVERGENIE_HOME：\n得到 %q\n要 %q", got, want)
	}
	for _, want := range []string{"✓ make web-ci", "✗ make ci", "✓ make e2e-full", "ci 的最後一行：壞掉了", "先不要部署", "開發家"} {
		if !strings.Contains(out, want) {
			t.Fatalf("摘要該有 %q：\n%s", want, out)
		}
	}

	if out, code = run(""); code != 0 || !strings.Contains(out, "可以部署") || !strings.Contains(out, "測的就是 origin/main") || strings.Contains(out, "✗") {
		t.Fatalf("全過、HEAD＝origin/main 時該結束碼 0、說可以部署：%d\n%s", code, out)
	}

	// 沒提交的改動：測到的不會被部署
	if err := os.WriteFile(filepath.Join(repo, "scripts", "release-check.sh"), append(body, []byte("\n# 改了\n")...), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, code = run(""); code != 1 || !strings.Contains(out, "沒提交的改動") || strings.Contains(out, "可以部署") {
		t.Fatalf("有沒提交的改動時不該給部署：%d\n%s", code, out)
	}
	git(repo, "checkout", "--", ".")

	// origin/main 多了一個 HEAD 沒有的 commit：部署會帶上沒測過的
	git(dir, "clone", "-q", origin, other)
	if err := os.WriteFile(filepath.Join(other, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(other, "add", "-A")
	git(other, "commit", "-q", "-m", "two")
	git(other, "push", "-q", "origin", "HEAD:main")
	if out, code = run(""); code != 1 || !strings.Contains(out, "1 個 commit 是 HEAD") || strings.Contains(out, "可以部署") {
		t.Fatalf("origin/main 比 HEAD 新時不該給部署：%d\n%s", code, out)
	}

	if out, code = run("", "--dry-run"); code != 0 || !strings.Contains(out, "e2e-full") || readCalls() != "" {
		t.Fatalf("--dry-run 只印、不叫 make：%d calls=%q\n%s", code, readCalls(), out)
	}
	if _, code = run("", "--bogus"); code != 2 {
		t.Fatalf("不認得的參數該是 2：%d", code)
	}
}
