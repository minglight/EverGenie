package claude

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Tmux 是引擎碰終端機的唯一介面（真的實作呼叫 `tmux -L <socket>`；測試用假的）。
// 畫面只讀兩處：啟動時的對話框、送 Enter 前的確認框檢查（ECL-N3）。
type Tmux interface {
	// Start 在 socket 上開一個背景 session；env 是 session 裡的完整環境（已清過），argv 是要執行的指令。
	Start(socket, name, cwd string, env []string, argv []string) error
	Paste(name, text string) error         // load-buffer + paste-buffer -p
	SendKey(name string, key string) error // Enter、Down…
	Alive(name string) bool
	// Remember 記下這個 session 在哪個 socket：開之前先記（才不會查到別的家同名的 session），
	// 引擎重開後從核心的紀錄（attach）補回。同一台跑兩個家時，各家的 socket 不同（GOAL-2）。
	Remember(name, socket string)
	Capture(name string) (string, error) // 啟動對話框、確認框、輸入框（純文字）
	// CaptureStyled 同 Capture 但帶顏色與樣式控制碼（capture-pane -e）：只用來分辨輸入框裡的淡色提示字（ECL-55）。
	CaptureStyled(name string) (string, error)
	Kill(name string) error // session 不在不算錯
	// LaunchedHere：這個名稱的 session 是不是這支引擎（這個家）開的。Start 遇到同名的舊視窗時，只有是自己開的才收掉（ECL-68）；
	// 查不出來一律回 false。
	LaunchedHere(name string) bool
	// Path 是實際會執行的那支 tmux（完整路徑；固定位置、引擎旁邊與 PATH 都沒有時是 "tmux"）：給人貼的 attach 指令用（ECL-15、ECL-63、ECL-65）。
	Path() string
}

const (
	defaultTmuxSocket = "eg"
	tmuxWidth         = "160"
	tmuxHeight        = "50"
	tmuxCallTimeout   = 10 * time.Second
)

// tmuxRunner 執行一次 tmux；stdin 非空才餵給它。回傳合併後的輸出。
type tmuxRunner func(ctx context.Context, stdin string, args ...string) (string, error)

// ExecTmux 是真的實作。每個 session 記得自己是在哪個 socket 開的；不認得的 session 找預設的 socket。
type ExecTmux struct {
	Bin           string // 指定的 tmux（不經小啟動程式）；空＝照 ECL-65／ECL-63 找（固定位置的 ＞ Self 旁邊的 ＞ PATH 上的）
	Self          string // 引擎自己的執行檔：從它的位置找跟著程式一起裝的 tmux（ECL-63、ECL-65）
	DefaultSocket string // 空＝defaultTmuxSocket（eg）
	ScriptDir     string // 啟動指令稿放的資料夾（指令稿執行時自己刪掉）
	// Warn 收「沒照預期做、但不擋事」的一行話（小啟動程式沒能讓 tmux 自己當負責的程式時，ECL-65）。
	// serve 用的（defaultTmux）寫進 <狀態>/engine-claude/out.log；空＝印到 stderr（核心開引擎時不收 stderr，只給測試與手動跑用）。
	Warn func(string)

	run      tmuxRunner // 測試注入；空＝真的執行 tmux
	rnd      func() string
	lookPath func(string) (string, error) // 測試注入；空＝exec.LookPath

	mu      sync.Mutex
	sockets map[string]string
}

// choice 每次呼叫都重找（stat 幾個檔，比起執行 tmux 本身可以不計）：引擎跑著時才裝好的 tmux 也找得到。
// 哪裡都沒有時 bin 是裸的 "tmux"（錯誤訊息照 tmux 原本的）。
func (t *ExecTmux) choice() tmuxChoice {
	if t.Bin != "" {
		return tmuxChoice{bin: t.Bin}
	}
	look := t.lookPath
	if look == nil {
		look = exec.LookPath
	}
	if c := chooseTmux(t.Self, look); c.bin != "" {
		return c
	}
	return tmuxChoice{bin: tmuxExe}
}

func (t *ExecTmux) bin() string { return t.choice().bin }

func (t *ExecTmux) Path() string { return t.bin() }

// launcherFallbackNote 是小啟動程式沒能讓 tmux 自己當負責的程式、照平常方式開 tmux 時，印在 stderr 那一行的開頭（後面接原因）。
// 跟 scripts/build-tmux.sh 裡啟動程式原始碼的 FALLBACK_NOTE 一字不差（cmd/evergenie 的 TestLauncherNoteMatchesEngine 檢查）。
const launcherFallbackNote = "disclaim：權限視窗沒辦法改記在 tmux 自己名下，照平常的方式開："

// command 是真的要執行的整行指令（ECL-65）：只有開 session 的 new-session（tmux server 還沒開時由它開起來）
// 經小啟動程式 `<啟動程式> <tmux> …`，tmux 自己當系統權限視窗的負責程式；其他指令（send-keys 等）直接叫 tmux。
// 沒有啟動程式（從原始碼跑的、舊的發行檔、指定了 Bin）就直接叫 tmux，跟以前一樣。
func (t *ExecTmux) command(args []string) []string {
	c := t.choice()
	argv := make([]string, 0, len(args)+2)
	if c.launcher != "" && len(args) > 2 && args[0] == "-L" && args[2] == "new-session" {
		argv = append(argv, c.launcher)
	}
	argv = append(argv, c.bin)
	return append(argv, args...)
}

func (t *ExecTmux) warn(msg string) {
	if t.Warn != nil {
		t.Warn(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

func (t *ExecTmux) socketFor(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if s := t.sockets[name]; s != "" {
		return s
	}
	if t.DefaultSocket != "" {
		return t.DefaultSocket
	}
	return defaultTmuxSocket
}

func (t *ExecTmux) random() string {
	if t.rnd != nil {
		return t.rnd()
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func (t *ExecTmux) exec(ctx context.Context, stdin string, args ...string) (string, error) {
	if t.run != nil {
		return t.run(ctx, stdin, args...)
	}
	argv := t.command(args)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// call 對某個 session 的 socket 執行一個 tmux 指令。
func (t *ExecTmux) call(socket, stdin string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tmuxCallTimeout)
	defer cancel()
	out, err := t.exec(ctx, stdin, append([]string{"-L", socket}, args...)...)
	if err != nil {
		return out, fmt.Errorf("tmux %s：%w：%s", args[0], err, strings.TrimSpace(out))
	}
	return out, nil
}

// tmuxName：tmux 會把 session 名稱裡的 . 和 : 換成 _，這裡先換，之後的目標才對得上。
func tmuxName(name string) string {
	return strings.NewReplacer(".", "_", ":", "_").Replace(name)
}

// 目標前面加 = 才是「名稱完全相同」；不加的話 code 不在時會對到 code-c1。
func sessionTarget(name string) string { return "=" + tmuxName(name) }
func paneTarget(name string) string    { return "=" + tmuxName(name) + ":" }

func (t *ExecTmux) Start(socket, name, cwd string, env []string, argv []string) error {
	if len(argv) == 0 {
		return errors.New("沒有要執行的指令")
	}
	if socket == "" {
		socket = t.socketFor("")
	}
	script, err := t.writeLaunchScript(env, argv)
	if err != nil {
		return err
	}
	args := []string{"new-session", "-d", "-s", tmuxName(name), "-x", tmuxWidth, "-y", tmuxHeight}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, script)
	out, err := t.call(socket, "", args...)
	if err != nil {
		os.Remove(script)
		return err
	}
	// 小啟動程式設不起來時照平常的方式開了（ECL-65）：session 沒事，但權限視窗又會記在開它的程式名下，留一行紀錄。
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, launcherFallbackNote) {
			t.warn(strings.TrimSpace(line))
		}
	}
	t.mu.Lock()
	if t.sockets == nil {
		t.sockets = map[string]string{}
	}
	t.sockets[name] = socket
	t.mu.Unlock()
	return nil
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// envFileVar 是帳號設定 env_file 的路徑：啟動腳本看到它就先載入那個 shell 檔（DEV-5）。
const envFileVar = "EVERGENIE_ENV_FILE"

// writeLaunchScript 把環境與指令寫成一個指令稿交給 tmux：tmux 的命令列有大小上限（指令很長時會被拒），
// 而且 tmux server 的環境可能帶著別人的變數，所以指令稿先把 CLAUDE*／ANTHROPIC_* 全部清掉，再設環境、exec。
// 有 EVERGENIE_ENV_FILE（帳號的 env_file）時最先載入它：先在子 shell 試一次，檔案壞了就略過（不讓 session 開不起來）；
// 載入後才清 CLAUDE*／ANTHROPIC_*、設這個 session 自己的變數，所以那個檔蓋不掉帳號與 session 的設定。
func (t *ExecTmux) writeLaunchScript(env, argv []string) (string, error) {
	dir := t.ScriptDir
	if dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("#!/bin/sh\n")
	b.WriteString("rm -f -- \"$0\"\n")
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == envFileVar && v != "" {
			f := shellQuote(v)
			b.WriteString("if [ -r " + f + " ] && ( set -a; . " + f + " ) >/dev/null 2>&1; then set -a; . " + f + " >/dev/null 2>&1; set +a; fi\n")
		}
	}
	b.WriteString("for v in $(env | sed -n -E 's/^(CLAUDE[A-Za-z0-9_]*|ANTHROPIC_[A-Za-z0-9_]*)=.*/\\1/p'); do unset \"$v\"; done\n")
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || !envName.MatchString(k) {
			continue
		}
		b.WriteString("export " + k + "=" + shellQuote(v) + "\n")
	}
	b.WriteString("exec")
	for _, a := range argv {
		b.WriteString(" " + shellQuote(a))
	}
	b.WriteString("\n")
	path := filepath.Join(dir, "launch-"+t.random()+".sh")
	if err := os.WriteFile(path, []byte(b.String()), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func (t *ExecTmux) Paste(name, text string) error {
	socket := t.socketFor(name)
	buf := "hg-" + t.random()
	if _, err := t.call(socket, text, "load-buffer", "-b", buf, "-"); err != nil {
		return err
	}
	if _, err := t.call(socket, "", "paste-buffer", "-p", "-d", "-b", buf, "-t", paneTarget(name)); err != nil {
		_, _ = t.call(socket, "", "delete-buffer", "-b", buf)
		return err
	}
	return nil
}

func (t *ExecTmux) SendKey(name, key string) error {
	_, err := t.call(t.socketFor(name), "", "send-keys", "-t", paneTarget(name), key)
	return err
}

func (t *ExecTmux) Remember(name, socket string) {
	if socket == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sockets == nil {
		t.sockets = map[string]string{}
	}
	t.sockets[name] = socket
}

func (t *ExecTmux) Alive(name string) bool {
	_, err := t.call(t.socketFor(name), "", "has-session", "-t", sessionTarget(name))
	return err == nil
}

// LaunchedHere 看那個 session 第一個窗格當初執行的指令：Start 交給 tmux 的是啟動指令稿 <ScriptDir>/launch-<亂數>.sh，
// ScriptDir 在這個家的狀態資料夾底下，所以路徑對得上就是這個家開的（別的家用同一個 socket 開的同名 session 對不上）。
// 沒設 ScriptDir（指令稿放在系統暫存資料夾，分不出是誰的）或查不到都回 false。
func (t *ExecTmux) LaunchedHere(name string) bool {
	if t.ScriptDir == "" {
		return false
	}
	out, err := t.call(t.socketFor(name), "", "display-message", "-p", "-t", paneTarget(name), "#{pane_start_command}")
	if err != nil {
		return false
	}
	script := strings.Trim(strings.TrimSpace(out), `"'`)
	return filepath.Dir(script) == filepath.Clean(t.ScriptDir) && strings.HasPrefix(filepath.Base(script), "launch-")
}

func (t *ExecTmux) Capture(name string) (string, error) {
	return t.call(t.socketFor(name), "", "capture-pane", "-p", "-t", paneTarget(name))
}

func (t *ExecTmux) CaptureStyled(name string) (string, error) {
	return t.call(t.socketFor(name), "", "capture-pane", "-p", "-e", "-t", paneTarget(name))
}

func (t *ExecTmux) Kill(name string) error {
	_, err := t.call(t.socketFor(name), "", "kill-session", "-t", sessionTarget(name))
	if err != nil && sessionMissing(err.Error()) {
		err = nil
	}
	t.mu.Lock()
	delete(t.sockets, name)
	t.mu.Unlock()
	return err
}

func sessionMissing(msg string) bool {
	for _, s := range []string{"can't find session", "no server running", "no sessions", "error connecting"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
