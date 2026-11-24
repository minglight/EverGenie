package claude

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minglight/evergenie/sdk"
)

// fakeClock 讓「等 1 秒」「20 秒內每秒看一次」不真的等：Sleep 只把時間往前推並記錄。
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock { return &fakeClock{now: fixedNow} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.sleeps = append(c.sleeps, d)
}

func (c *fakeClock) sleepCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sleeps)
}

type tmuxCall struct {
	Op     string // start、paste、key、alive、capture、kill
	Name   string
	Arg    string // paste 的文字、key 的名稱
	At     time.Time
	Socket string
	Cwd    string
	Env    []string
	Argv   []string
}

// fakeTmux 記錄每個呼叫與時間，不碰真的 tmux。
type fakeTmux struct {
	mu    sync.Mutex
	clock *fakeClock
	calls []tmuxCall
	alive map[string]bool

	screens    map[string]string // 每個 session 目前的畫面
	styled     map[string]string // 帶樣式的畫面（CaptureStyled）；沒設就回 screens 那份（純文字沒有淡色字）
	styledErr  error
	startErr   error
	pasteErr   error
	captureErr error
	keyErr     error
	// onKey：按鍵之後改畫面（模擬確認框收掉；回傳新畫面，ok=false 表示畫面不變）。
	onKey func(name, key string) (string, bool)
	// onPaste：貼上之後改畫面（選擇題的「Type something.」那一行；回空字串表示畫面不變）。
	onPaste func(name, text string) string

	// 像真的 Claude 的輸入框（ECL-42）：typing 時貼上的字進輸入框（最後兩條橫線中間），Enter 把它送出、輸入框清空。
	// dropPaste：貼上丟了（輸入框不變）；eatEnter：Enter 被吃掉（字還留在輸入框）。
	typing    bool
	dropPaste bool
	eatEnter  bool
	// echo：Enter 送出後，那句出現在輸入框上面（對話區的回顯，或她忙時排隊等著的那句）。
	echo bool
	// path：Path() 回的 tmux 路徑（attach 指令用）；空＝"tmux"。
	path string
	// foreign：這些名稱的 session 不是這個家開的（LaunchedHere 回 false）。
	foreign map[string]bool
}

func newFakeTmux(clock *fakeClock) *fakeTmux {
	return &fakeTmux{clock: clock, alive: map[string]bool{}, screens: map[string]string{}, typing: true}
}

const fakeRule = "────────────────────"

// withBox 把畫面上輸入框的內容換成 content（沒有輸入框就在最後補一個）。
func withBox(screen, content string) string {
	lines := strings.Split(screen, "\n")
	bottom, top := -1, -1
	for i := len(lines) - 1; i >= 0; i-- {
		if isRule(lines[i]) {
			if bottom < 0 {
				bottom = i
				continue
			}
			top = i
			break
		}
	}
	box := "❯ " + content
	if top < 0 {
		if screen != "" && !strings.HasSuffix(screen, "\n") {
			screen += "\n"
		}
		return screen + fakeRule + "\n" + box + "\n" + fakeRule + "\n"
	}
	out := append([]string{}, lines[:top+1]...)
	out = append(out, box)
	return strings.Join(append(out, lines[bottom:]...), "\n")
}

// record 只在持有 f.mu 時呼叫（每個假方法都先鎖）；watchBoot 等背景 goroutine 也會寫，測試讀紀錄要經 snapshot／callsOf／actions。
func (f *fakeTmux) record(c tmuxCall) {
	c.At = f.clock.Now()
	f.calls = append(f.calls, c)
}

func (f *fakeTmux) Start(socket, name, cwd string, env, argv []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "start", Name: name, Socket: socket, Cwd: cwd, Env: env, Argv: argv})
	if f.startErr != nil {
		return f.startErr
	}
	f.alive[name] = true
	return nil
}

func (f *fakeTmux) Paste(name, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "paste", Name: name, Arg: text})
	if f.pasteErr == nil && f.typing && !f.dropPaste {
		f.screens[name] = withBox(f.screens[name], text)
	}
	if f.pasteErr == nil && f.onPaste != nil {
		if screen := f.onPaste(name, text); screen != "" {
			f.screens[name] = screen
		}
	}
	return f.pasteErr
}

func (f *fakeTmux) SendKey(name, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "key", Name: name, Arg: key})
	if f.keyErr == nil && f.typing && !f.eatEnter && key == "Enter" {
		if box, ok := inputBox(f.screens[name]); ok {
			sent := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(box), "❯"))
			f.screens[name] = withBox(f.screens[name], "")
			if f.echo && sent != "" {
				f.screens[name] = "> " + sent + "\n" + f.screens[name]
			}
		}
	}
	if f.keyErr == nil && f.onKey != nil {
		if screen, ok := f.onKey(name, key); ok {
			f.screens[name] = screen
		}
	}
	return f.keyErr
}

func (f *fakeTmux) Remember(name, socket string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "remember", Name: name, Arg: socket})
}

func (f *fakeTmux) Alive(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "alive", Name: name})
	return f.alive[name]
}

func (f *fakeTmux) Capture(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "capture", Name: name})
	if f.captureErr != nil {
		return "", f.captureErr
	}
	return f.screens[name], nil
}

func (f *fakeTmux) CaptureStyled(name string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "capture-styled", Name: name})
	if f.styledErr != nil {
		return "", f.styledErr
	}
	if s, ok := f.styled[name]; ok {
		return s, nil
	}
	return f.screens[name], nil
}

func (f *fakeTmux) Kill(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.record(tmuxCall{Op: "kill", Name: name})
	delete(f.alive, name)
	return nil
}

func (f *fakeTmux) LaunchedHere(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.foreign[name]
}

func (f *fakeTmux) Path() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.path == "" {
		return "tmux"
	}
	return f.path
}

func (f *fakeTmux) setScreen(name, screen string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.screens[name] = screen
}

func (f *fakeTmux) setAlive(name string, v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[name] = v
}

// snapshot 回到目前為止所有呼叫的複本（鎖住才讀，背景 goroutine 之後再寫也不影響）。
func (f *fakeTmux) snapshot() []tmuxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tmuxCall(nil), f.calls...)
}

func (f *fakeTmux) callsOf(op string) []tmuxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tmuxCall
	for _, c := range f.calls {
		if c.Op == op {
			out = append(out, c)
		}
	}
	return out
}

// 只有會改變 session 的呼叫（排除 alive、capture 這類查詢）。
func (f *fakeTmux) actions() []tmuxCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tmuxCall
	for _, c := range f.calls {
		if c.Op != "alive" && c.Op != "capture" {
			out = append(out, c)
		}
	}
	return out
}

var hostEnviron = []string{
	"HOME=/Users/tester", "PATH=/usr/local/bin:/usr/bin", "TERM=xterm-256color",
	"CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_CODE_ENTRYPOINT=cli", "ANTHROPIC_API_KEY=host-secret", "ANTHROPIC_BASE_URL=http://x",
	"EVERGENIE_ENGINE_SOCKET=/tmp/host/engine-claude.sock", "EVERGENIE_SESSION=host-session", "EVERGENIE_HOME=/wrong/home",
}

type engEnv struct {
	t      *testing.T
	home   string
	cfgDir string
	clock  *fakeClock
	tmux   *fakeTmux
	host   *fakeHost
	e      *Engine
	ids    int
}

func newEngEnv(t *testing.T) *engEnv {
	t.Helper()
	v := &engEnv{t: t, home: t.TempDir(), cfgDir: t.TempDir(), clock: newFakeClock(), host: &fakeHost{}}
	v.tmux = newFakeTmux(v.clock)
	v.e = NewEngine(EngineDeps{
		Home: v.home, HookBin: "/opt/hg/evergenie-engine-claude", APISocket: filepath.Join(v.home, "runtime", "sockets", "api.sock"),
		Tmux: v.tmux, Host: v.host, Now: v.clock.Now, Sleeper: v.clock.Sleep,
		Environ: func() []string { return append([]string(nil), hostEnviron...) },
		NewID: func() string {
			v.ids++
			return "00000000-0000-4000-8000-00000000000" + string(rune('0'+v.ids))
		},
	})
	t.Cleanup(v.e.Close)
	return v
}

func (v *engEnv) req(name string) *sdk.EngineStartRequest {
	return &sdk.EngineStartRequest{
		Name: name, Agent: "code", Cwd: "/tmp/hg-work", Account: "acct-a",
		EngineConfig: map[string]any{"config_dir": v.cfgDir},
		Instructions: "你是 code。\n第二行 \"引號\" 與 $HOME。",
	}
}

func (v *engEnv) start(name string, mod func(*sdk.EngineStartRequest)) *tmuxCall {
	v.t.Helper()
	r := v.req(name)
	if mod != nil {
		mod(r)
	}
	if _, err := v.e.Start(context.Background(), r); err != nil {
		v.t.Fatalf("Start：%v", err)
	}
	v.e.Wait() // 啟動對話框偵測用假時鐘，很快結束；等它結束，之後的檢查才不會跟它搶時鐘
	starts := v.tmux.callsOf("start")
	c := starts[len(starts)-1]
	return &c
}

// transcriptFile 建 start 之後那個 session 的對話記錄檔（空的），回傳路徑。
func (v *engEnv) transcriptFile(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(v.cfgDir, "projects", projectDirName("/tmp/hg-work"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "00000000-0000-4000-8000-000000000001.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeTurn 模擬 UserPromptSubmit hook 寫的 turns 檔（ECL-5、ECL-42）。
func (v *engEnv) writeTurn(t *testing.T, head string, at time.Time) {
	t.Helper()
	data, err := json.Marshal(turnRecord{Source: sdk.InputSourceTyped, At: at, Head: head})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(turnFilePath(v.home, "code"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func argvValue(argv []string, flag string) (string, bool) {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1], true
		}
	}
	return "", false
}

func hasFlag(argv []string, flag string) bool {
	for _, a := range argv {
		if a == flag {
			return true
		}
	}
	return false
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

func isCode(err error, code sdk.ErrorCode) bool {
	var e *sdk.Error
	return errors.As(err, &e) && e.Code == code
}

func readScreen(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(spikeDir, "screens", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
