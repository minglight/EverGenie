package claude

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// 一次性對話（ECL-20，P6-5b）：`claude -p` 跑一次，訊息從 stdin 給，回覆是 stdout。
// 不開任何工具（--disallowedTools "*"）、不留對話（--no-session-persistence）、不碰常駐 session，也不回報事件。
// 帳號只靠 CLAUDE_CONFIG_DIR 指定：不寫任何 settings 檔、不帶 --settings／--allowedTools／--resume／--name。

const (
	defaultAskTimeout = 600 * time.Second
	askKillGrace      = 2 * time.Second // 殺掉行程後，等它的輸出管子最多這麼久
)

var pathLike = regexp.MustCompile(`(?:~|/)[^\s"'，。：；]*`)

// askArgv 組出一次性對話的指令（不含 claude 本身）。
func askArgv(req *sdk.EngineAskRequest) []string {
	argv := []string{"-p", "--no-session-persistence"}
	if req.Model != "" {
		argv = append(argv, "--model", req.Model)
	}
	if validEfforts[req.Effort] {
		argv = append(argv, "--effort", req.Effort)
	}
	if req.Instructions != "" {
		argv = append(argv, "--append-system-prompt", req.Instructions)
	}
	return append(argv, "--disallowedTools", "*")
}

// Ask 做一次性對話。逾時殺掉行程回 UNAVAILABLE「回覆逾時」；結束碼不是 0 回 UNAVAILABLE，帶 stderr 的第一行（路徑拿掉）。
func (e *Engine) Ask(ctx context.Context, req *sdk.EngineAskRequest) (*sdk.EngineAskResponse, error) {
	cfg, err := parseEngineConfig(req.EngineConfig)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Message) == "" {
		return nil, invalid("message", "請提供要說的話")
	}
	wait := defaultAskTimeout
	if req.TimeoutSeconds > 0 {
		wait = time.Duration(req.TimeoutSeconds) * time.Second
	}
	call, cancel := context.WithTimeout(ctx, wait)
	defer cancel()

	cmd := exec.Command(e.claudeBin(), askArgv(req)...)
	cmd.Env = withAccountEnv(e.environ(), cfg)
	cmd.Dir = e.askDir(req.Cwd)
	cmd.Stdin = strings.NewReader(req.Message)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // 整組行程一起殺，才不會留下孤兒
	cmd.WaitDelay = askKillGrace
	if err := cmd.Start(); err != nil {
		return nil, unavailable("無法執行 claude：" + stripPaths(firstLine(err.Error())))
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-call.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, unavailable("回覆逾時")
	}
	if waitErr != nil {
		msg := stripPaths(firstLine(stderr.String()))
		if msg == "" {
			var ee *exec.ExitError
			if errors.As(waitErr, &ee) {
				msg = fmt.Sprintf("claude 結束碼 %d", ee.ExitCode())
			} else {
				msg = "claude 沒有正常結束"
			}
		}
		return nil, unavailable("claude 沒有回覆：" + msg)
	}
	reply := strings.TrimSpace(stdout.String())
	if reply == "" {
		return nil, unavailable("claude 沒有回覆")
	}
	return &sdk.EngineAskResponse{Reply: reply, Model: req.Model}, nil
}

// askDir 是 claude 跑的資料夾：秘書的工作資料夾，沒有（或不存在）就家，再沒有就系統暫存資料夾。
func (e *Engine) askDir(cwd string) string {
	for _, d := range []string{cwd, e.d.Home} {
		if d == "" {
			continue
		}
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			return d
		}
	}
	return os.TempDir()
}

// firstLine 取第一個非空白行。
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// stripPaths 把看起來像路徑的字換成「（路徑）」：訊息給使用者看，不該帶家目錄的位置。
func stripPaths(s string) string { return pathLike.ReplaceAllString(s, "（路徑）") }
