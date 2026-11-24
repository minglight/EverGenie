package cmdline

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minglight/evergenie/sdk"
)

const usage = "\n結尾：測試\n"

// run 跑一個指令，回結束碼、stdout、stderr。EVERGENIE_AGENT 固定清掉，避免吃到外面的環境。
func run(t *testing.T, cmds map[string]Command, args ...string) (int, string, string) {
	t.Helper()
	t.Setenv("EVERGENIE_AGENT", "")
	var out, errb bytes.Buffer
	code := Run(context.Background(), args, nil, IO{Out: &out, Err: &errb}, cmds, usage)
	return code, out.String(), errb.String()
}

func TestHelpPrintsUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"--help"}, {"-h"}} {
		code, out, _ := run(t, nil, args...)
		if code != 0 || out != Help(nil)+usage || !strings.HasPrefix(out, "用法：evergenie") {
			t.Fatalf("%v：%d %q", args, code, out)
		}
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, _, errb := run(t, map[string]Command{"a": {Run: func(*Env, []string) error { return nil }}}, "nope", "x", "y")
	if code != 2 || !strings.Contains(errb, "不認得的指令：nope x") || !strings.Contains(errb, usage) {
		t.Fatalf("%d %q", code, errb)
	}
}

// 「指令＋子指令」優先於單一指令：quota check 不會被當成 quota 帶一個參數。
func TestLookupPrefersSubcommand(t *testing.T) {
	var got string
	cmds := map[string]Command{
		"quota":       {Run: func(e *Env, a []string) error { got = "quota" + fmt.Sprint(a); return nil }},
		"quota check": {Run: func(e *Env, a []string) error { got = "check" + fmt.Sprint(a); return nil }},
	}
	if code, _, _ := run(t, cmds, "quota", "check", "acct"); code != 0 || got != "check[acct]" {
		t.Fatalf("%d %s", code, got)
	}
	if code, _, _ := run(t, cmds, "quota", "acct"); code != 0 || got != "quota[acct]" {
		t.Fatalf("%d %s", code, got)
	}
	if fn, rest := Lookup(cmds, nil); fn != nil || rest != nil {
		t.Fatal("沒有參數不該找到指令")
	}
}

func TestExitCodes(t *testing.T) {
	cases := map[string]struct {
		err      error
		code     int
		wantErr  string // stderr 要含的字
		noStderr bool
	}{
		"成功":       {nil, 0, "", true},
		"用法錯誤":     {Usagef("缺少 %s", "編號"), 2, "缺少 編號", false},
		"包著的用法錯誤":  {fmt.Errorf("外層：%w", Usagef("少了旗標")), 2, "少了旗標", false},
		"已回報":      {ErrReported, 1, "", true},
		"自訂結束碼":    {ExitCodeError{Code: 3}, 3, "", true},
		"API 白話錯誤": {&sdk.Error{Code: sdk.ErrorCodeNotFound, Message: "找不到這張單"}, 1, "找不到這張單", false},
		"一般錯誤":     {errors.New("壞了"), 1, "壞了", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			code, out, errb := run(t, map[string]Command{"x": {Run: func(*Env, []string) error { return c.err }}}, "x")
			if code != c.code || out != "" {
				t.Fatalf("%d %q", code, out)
			}
			if c.noStderr && errb != "" {
				t.Fatalf("不該印 stderr：%q", errb)
			}
			if !strings.Contains(errb, c.wantErr) {
				t.Fatalf("stderr %q 要含 %q", errb, c.wantErr)
			}
		})
	}
}

func TestUsageErrorPointsToHelp(t *testing.T) {
	_, _, errb := run(t, map[string]Command{"x": {Run: func(*Env, []string) error { return Usagef("少了編號") }}}, "x")
	if !strings.Contains(errb, "（evergenie help 看用法）") {
		t.Fatal(errb)
	}
}

func TestEnvDefaultsAndBy(t *testing.T) {
	var by string
	var noSocket bool
	var stdin string
	cmd := map[string]Command{"x": {Run: func(e *Env, _ []string) error {
		by, noSocket = e.By, e.NoSocket
		b := make([]byte, 4)
		n, _ := e.In.Read(b)
		stdin = string(b[:n])
		return nil
	}}}
	// 沒有 stdin、沒有 client、沒有 $EVERGENIE_AGENT：身分是 owner(cli)，NoSocket 為真，stdin 讀到空。
	if code, _, _ := run(t, cmd, "x"); code != 0 || by != DefaultBy || !noSocket || stdin != "" {
		t.Fatal(code, by, noSocket, stdin)
	}
	// 有 $EVERGENIE_AGENT 就用它；有給 stdin 就讀得到。
	t.Setenv("EVERGENIE_AGENT", "code")
	code := Run(context.Background(), []string{"x"}, nil, IO{In: strings.NewReader("hi"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, cmd, usage)
	if code != 0 || by != "code" || stdin != "hi" {
		t.Fatal(code, by, stdin)
	}
}

func TestMessage(t *testing.T) {
	e := &Env{Ctx: context.Background()}
	if got := e.Message(&sdk.Error{Message: "白話"}); got != "白話" {
		t.Fatal(got)
	}
	if got := e.Message(errors.New("其他")); got != "其他" {
		t.Fatal(got)
	}
	conn := &url.Error{Op: "Post", URL: "http://x", Err: errors.New("refused")}
	if got := e.Message(conn); got != "連不到 EverGenie，請確認 serve 在跑" {
		t.Fatal(got)
	}
	e.NoSocket = true
	if got := e.Message(conn); !strings.Contains(got, "不知道它的位置") {
		t.Fatal(got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := (&Env{Ctx: ctx}).Message(errors.New("x")); got != "已中斷" {
		t.Fatal(got)
	}
}

func TestLinePrintsOneJSONLineWithoutEscapingHTML(t *testing.T) {
	var out bytes.Buffer
	e := &Env{Out: &out}
	if err := e.Line(map[string]string{"a": "<b>"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"a\":\"<b>\"}\n" {
		t.Fatalf("%q", out.String())
	}
}

func TestSocketPath(t *testing.T) {
	t.Setenv("EVERGENIE_API_SOCKET", "")
	t.Setenv("EVERGENIE_HOME", "")
	if SocketPath() != "" || Dial() != nil {
		t.Fatal("沒有線索時要回空字串與 nil")
	}
	t.Setenv("EVERGENIE_HOME", "/h")
	if SocketPath() != "/h/runtime/sockets/api.sock" || Dial() == nil {
		t.Fatal(SocketPath())
	}
	t.Setenv("EVERGENIE_API_SOCKET", "/s.sock")
	if SocketPath() != "/s.sock" {
		t.Fatal(SocketPath())
	}
}

// DEV-88：$EVERGENIE_API_SOCKET 是標準位置但屬於別的家，又明講了 $EVERGENIE_HOME，就連 $EVERGENIE_HOME 的家；
// 同一個家、非標準位置的 socket、只有 socket 都照 socket。
func TestSocketPathFollowsExplicitHome(t *testing.T) {
	dev, prod := t.TempDir(), t.TempDir()
	devSock := filepath.Join(dev, "runtime", "sockets", "api.sock")
	prodSock := filepath.Join(prod, "runtime", "sockets", "api.sock")

	t.Setenv("EVERGENIE_API_SOCKET", devSock)
	t.Setenv("EVERGENIE_HOME", prod)
	if got := SocketPath(); got != prodSock {
		t.Fatalf("socket 屬於別的家，要照 EVERGENIE_HOME：%q", got)
	}
	if home, other := OtherHome(); !other || home != filepath.Clean(prod) {
		t.Fatalf("要認出是別的家：%q %v", home, other)
	}

	t.Setenv("EVERGENIE_HOME", dev)
	if got := SocketPath(); got != devSock {
		t.Fatalf("同一個家照 socket：%q", got)
	}
	if _, other := OtherHome(); other {
		t.Fatal("同一個家不是別的家")
	}

	t.Setenv("EVERGENIE_API_SOCKET", "/s.sock")
	t.Setenv("EVERGENIE_HOME", prod)
	if got := SocketPath(); got != "/s.sock" {
		t.Fatalf("非標準位置的 socket 不猜：%q", got)
	}

	t.Setenv("EVERGENIE_HOME", "")
	t.Setenv("EVERGENIE_API_SOCKET", devSock)
	if got := SocketPath(); got != devSock {
		t.Fatalf("只有 socket：%q", got)
	}
}

func TestMerge(t *testing.T) {
	noop := func(*Env, []string) error { return nil }
	all, err := Merge(map[string]Command{"a": {Run: noop}}, nil, map[string]Command{"b": {Run: noop}})
	if err != nil || len(all) != 2 {
		t.Fatal(all, err)
	}
	if _, err := Merge(map[string]Command{"a": {Run: noop}}, map[string]Command{"a": {Run: noop}}); err == nil || !strings.Contains(err.Error(), `"a"`) {
		t.Fatal(err)
	}
	if _, err := Merge(map[string]Command{"": {Run: noop}}); err == nil {
		t.Fatal("空名稱要回錯")
	}
	if _, err := Merge(map[string]Command{"a x": {Run: noop, Usage: "a x"}}, map[string]Command{"a y": {Run: noop, Usage: "a y"}}); err == nil || !strings.Contains(err.Error(), "用法寫了兩份") {
		t.Fatalf("同一個頂層指令兩份用法要回錯：%v", err)
	}
}

// help 從指令表組出來：開頭的通用說明、依 Group 的固定順序分段（空的段不印）、段內依名稱排序；多行用法後面幾行縮排；
// 沒寫用法的頂層指令自動列「名稱 子指令|子指令」。
func TestHelpFromCommands(t *testing.T) {
	noop := func(*Env, []string) error { return nil }
	got := Help(map[string]Command{
		"tasks add":  {Run: noop, Usage: "tasks add|list   任務單", Group: GroupWork},
		"tasks list": {Run: noop},
		"doctor":     {Run: noop, Usage: "doctor   健康檢查\n第二行", Group: GroupSystem},
		"say":        {Run: noop, Usage: "say <職位> <內容>", Group: GroupAgents},
		"zzz b":      {Run: noop},
		"zzz a":      {Run: noop},
	})
	want := helpHeader + "\n任務、提醒與通知\n  tasks add|list   任務單\n" +
		"\n秘書與對話\n  say <職位> <內容>\n" +
		"\n這台電腦與家\n  doctor   健康檢查\n" + usageIndent + "第二行\n" +
		"\n其他\n  zzz a|b\n"
	if got != want {
		t.Fatalf("help 不對：\n%s\n---\n%s", got, want)
	}
}

// DEV-145：第 2 版的家，API socket 在狀態資料夾；$EVERGENIE_API_SOCKET 指到它也算同一個家。
func TestSocketPathFollowsLayoutV2(t *testing.T) {
	home, state := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("[layout]\nversion = 2\nstate = \""+state+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(state, "runtime", "sockets", "api.sock")
	t.Setenv("EVERGENIE_API_SOCKET", "")
	t.Setenv("EVERGENIE_HOME", home)
	if got := SocketPath(); got != sock {
		t.Fatalf("第 2 版要用狀態資料夾的 socket：%q", got)
	}
	t.Setenv("EVERGENIE_API_SOCKET", sock)
	if got := SocketPath(); got != sock {
		t.Fatalf("同一個家照 socket：%q", got)
	}
	if _, other := OtherHome(); other {
		t.Fatal("狀態資料夾的 socket 屬於同一個家")
	}
	other := t.TempDir()
	t.Setenv("EVERGENIE_API_SOCKET", filepath.Join(other, "runtime", "sockets", "api.sock"))
	if got := SocketPath(); got != sock {
		t.Fatalf("socket 屬於別的家，要照 EVERGENIE_HOME：%q", got)
	}
}

// 搬家前就開著的 session 帶著第 1 版的 socket：家已是第 2 版就改連狀態資料夾的 socket（DEV-145）。
func TestSocketPathOldV1SocketAfterRelayout(t *testing.T) {
	home, state := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "home.toml"), []byte("schema = 2\n[layout]\nversion = 2\nstate = \""+state+"\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(home, "runtime", "sockets", "api.sock")
	want := filepath.Join(state, "runtime", "sockets", "api.sock")
	for _, envHome := range []string{home, ""} {
		t.Setenv("EVERGENIE_HOME", envHome)
		t.Setenv("EVERGENIE_API_SOCKET", old)
		if got := SocketPath(); got != want {
			t.Fatalf("EVERGENIE_HOME=%q：舊 socket 要換成第 2 版的位置：%q", envHome, got)
		}
	}
	v1 := t.TempDir()
	v1sock := filepath.Join(v1, "runtime", "sockets", "api.sock")
	t.Setenv("EVERGENIE_HOME", v1)
	t.Setenv("EVERGENIE_API_SOCKET", v1sock)
	if got := SocketPath(); got != v1sock {
		t.Fatalf("第 1 版照原本的 socket：%q", got)
	}
}
