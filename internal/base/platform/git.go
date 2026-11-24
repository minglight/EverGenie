package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// 唯讀的 git 小工具（REQ-272）：api 模組不直接用 os／os/exec（REQ-2 的 lint），發版狀態要看 repo 就走這裡。
// 一律 exec.CommandContext、不經 shell。

// HasGit 回這台找不找得到 git 指令。
func HasGit() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// DirExists 回 path 是不是一個存在的資料夾。
func DirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// GitOutput 在 repo 裡跑 `git -C <repo> <args…>`，timeout 內沒做完就中止。
// 成功回標準輸出；失敗回只有一行白話的錯（取 stderr 第一行，逾時說等了幾秒）。
func GitOutput(ctx context.Context, timeout time.Duration, repo string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", fmt.Errorf("等了 %d 秒沒有回應", int(timeout.Seconds()))
		}
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		first, _, _ := strings.Cut(msg, "\n")
		return "", errors.New(strings.TrimSpace(first))
	}
	return out.String(), nil
}
