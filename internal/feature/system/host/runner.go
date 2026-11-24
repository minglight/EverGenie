package host

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Runner 執行外部指令，回輸出（stdout＋stderr）與結束碼；指令跑不起來（找不到程式）結束碼是 127。
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (out string, code int)
	// LookPath 找程式的位置；找不到回空字串。
	LookPath(name string) string
}

// ExecRunner 是真的執行外部指令的 Runner。
type ExecRunner struct{ Timeout time.Duration }

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (string, int) {
	d := r.Timeout
	if d == 0 {
		d = 60 * time.Second
	}
	call, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	cmd := exec.CommandContext(call, name, args...)
	cmd.Env = withTerm(os.Environ())
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	switch e := err.(type) {
	case nil:
		return buf.String(), 0
	case *exec.ExitError:
		return buf.String(), e.ExitCode()
	default:
		if call.Err() != nil {
			return buf.String() + "逾時", 124
		}
		return err.Error(), 127
	}
}

// LookPath 除了 PATH，也找 Homebrew 與 /usr/local 常見的位置（launchd 與遠端環境的 PATH 常常很短）。
func (ExecRunner) LookPath(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, dir := range []string{"/opt/homebrew/bin", "/usr/local/bin", "/Applications/Tailscale.app/Contents/MacOS", filepath.Join(os.Getenv("HOME"), ".local", "bin")} {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// timeNow 讓測試可以換成固定時間。
var timeNow = time.Now

// withTerm 在環境沒有 TERM 時補一個（DEV-153）：服務由 launchd 起來時環境裡沒有 TERM，
// Tailscale App 內附的那支程式會以為要開視窗（"The Tailscale GUI failed to start"）而不當指令跑，
// 看板的手機連線就把開著的 Tailscale 說成沒開。已經有 TERM 就不動。
func withTerm(env []string) []string {
	for _, kv := range env {
		if len(kv) > 5 && kv[:5] == "TERM=" {
			return env
		}
	}
	return append(env, "TERM=dumb")
}
